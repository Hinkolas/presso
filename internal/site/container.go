package site

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/store"
)

// decrypted holds a site's plaintext secrets for the duration of one
// operation (container creation, login link minting).
type decrypted struct {
	dbPassword    string
	redisPassword string
	salts         map[string]string
	loginSecret   string
}

func (s *Service) decrypt(row store.Site) (decrypted, error) {
	var d decrypted
	var err error
	if d.dbPassword, err = s.box.DecryptString(row.DbPasswordEnc); err != nil {
		return d, fmt.Errorf("site: decrypt db password: %w", err)
	}
	if d.redisPassword, err = s.box.DecryptString(row.RedisPasswordEnc); err != nil {
		return d, fmt.Errorf("site: decrypt redis password: %w", err)
	}
	saltsJSON, err := s.box.Decrypt(row.SaltsEnc)
	if err != nil {
		return d, fmt.Errorf("site: decrypt salts: %w", err)
	}
	if err := json.Unmarshal(saltsJSON, &d.salts); err != nil {
		return d, fmt.Errorf("site: salts: %w", err)
	}
	if d.loginSecret, err = s.box.DecryptString(row.LoginSecretEnc); err != nil {
		return d, fmt.Errorf("site: decrypt login secret: %w", err)
	}
	return d, nil
}

// purgeTokenFor derives the system plugin's purge bearer token from the
// site's login secret, so only its hash needs storing.
func purgeTokenFor(loginSecret string) string {
	mac := hmac.New(sha256.New, []byte(loginSecret))
	mac.Write([]byte("presso-purge-token"))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// ContainerName and NetworkAlias are the docker-side identities of a site.
func ContainerName(slug string) string { return "presso-site-" + slug }
func NetworkAlias(id string) string    { return "site-" + id }

// containerSpec renders the runtime image's environment contract from a site
// row. Every variable here is read by runtime/wp-config.php, the Caddyfile
// or the system plugin; keep the names in sync.
func (s *Service) containerSpec(row store.Site, d decrypted) docker.Spec {
	set := parseSettings(row.Settings)
	id := row.ID.String()
	env := []string{
		"PRESSO_SITE_ID=" + id,
		"PRESSO_SITE_URL=" + siteURL(row.PrimaryDomain, row.TlsMode),
		"PRESSO_PROFILE=" + row.Profile,
		"PRESSO_FILE_MODS=" + row.FileMods,
		"PRESSO_DB_HOST=" + platform.NameMariaDB,
		"PRESSO_DB_NAME=" + row.DbName,
		"PRESSO_DB_USER=" + row.DbUser,
		"PRESSO_DB_PASSWORD=" + d.dbPassword,
		"PRESSO_DB_PREFIX=wp_",
		"PRESSO_REDIS_HOST=" + platform.NameRedis,
		"PRESSO_REDIS_PORT=6379",
		"PRESSO_REDIS_USER=" + row.RedisUser,
		"PRESSO_REDIS_PASSWORD=" + d.redisPassword,
		"PRESSO_REDIS_PREFIX=" + platform.RedisKeyPrefix(id),
		"PRESSO_ENGINE_URL=" + s.cfg.EngineInternalURL,
		"PRESSO_PURGE_TOKEN=" + purgeTokenFor(d.loginSecret),
		"PRESSO_LOGIN_SECRET=" + d.loginSecret,
		"PRESSO_CACHE=" + onOff(row.PageCache),
		"PRESSO_CACHE_TTL=" + strconv.Itoa(set.CacheTTL),
		"PRESSO_TELEMETRY_SAMPLE=0.1",
		"PRESSO_WP_MEMORY_LIMIT=" + set.PHPMemoryLimit,
		"PRESSO_WP_MAX_MEMORY_LIMIT=" + set.PHPMemoryLimit,
		"PHP_MEMORY_LIMIT=" + set.PHPMemoryLimit,
		"PHP_UPLOAD_MAX_FILESIZE=" + set.UploadMaxFilesize,
		"PHP_POST_MAX_SIZE=" + set.PostMaxSize,
		"PHP_MAX_EXECUTION_TIME=" + strconv.Itoa(set.MaxExecutionTime),
		"PHP_MAX_INPUT_VARS=" + strconv.Itoa(set.MaxInputVars),
		"PHP_OPCACHE_MEMORY=" + strconv.Itoa(set.OpcacheMemoryMB),
	}
	if row.Profile == "dev" {
		env = append(env, "PHP_OPCACHE_REVALIDATE_FREQ=0")
	} else {
		env = append(env, "PHP_OPCACHE_REVALIDATE_FREQ=60")
	}
	for _, n := range saltNames {
		env = append(env, "PRESSO_SALT_"+n+"="+d.salts[n])
	}
	return docker.Spec{
		Name:           ContainerName(row.Slug),
		Image:          row.Image,
		Env:            env,
		Labels:         docker.Labels("site", map[string]string{docker.LabelSiteID: id, docker.LabelSlug: row.Slug}),
		Network:        s.cfg.Network,
		NetworkAliases: []string{NetworkAlias(id)},
		Mounts: []docker.Mount{
			{Type: docker.MountVolume, Source: row.VolumeName, Target: wpPath + "/wp-content"},
			{Type: docker.MountTmpfs, Target: "/tmp"},
		},
		ExtraHosts:    []string{"host.docker.internal:host-gateway"},
		MemoryBytes:   int64(set.ContainerMemoryMB) << 20,
		NanoCPUs:      int64(set.ContainerCPUs * 1e9),
		RestartPolicy: "unless-stopped",
		StopTimeout:   10 * time.Second,
		LogMaxSize:    "10m",
		LogMaxFile:    3,
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// ensureContainer creates the site container if missing, replacing one whose
// image or environment no longer matches the row. Returns the container id.
func (s *Service) ensureContainer(ctx context.Context, row store.Site, d decrypted, replace bool) (string, error) {
	spec := s.containerSpec(row, d)
	info, err := s.dk.InspectContainer(ctx, spec.Name)
	if err == nil {
		if !replace && info.Image == spec.Image && sameEnv(info.Env, spec.Env) {
			return info.ID, nil
		}
		if err := s.dk.RemoveContainer(ctx, info.ID, true); err != nil {
			return "", err
		}
	} else if !isNotFound(err) {
		return "", err
	}
	id, err := s.dk.CreateContainer(ctx, spec)
	if err != nil {
		return "", err
	}
	if err := s.st.SetSiteContainerID(ctx, store.SetSiteContainerIDParams{ContainerID: id, UpdatedAt: s.now(), ID: row.ID}); err != nil {
		return "", err
	}
	return id, nil
}

func sameEnv(have, want []string) bool {
	set := map[string]bool{}
	for _, e := range have {
		set[e] = true
	}
	for _, e := range want {
		if !set[e] {
			return false
		}
	}
	return true
}

// waitHealthy polls the container until Docker reports it healthy. A
// container that exits is a hard failure with its last log lines attached.
func (s *Service) waitHealthy(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		info, err := s.dk.InspectContainer(ctx, containerID)
		if err != nil {
			return err
		}
		switch {
		case info.State == "exited" || info.State == "dead":
			return fmt.Errorf("container exited with code %d: %s", info.ExitCode, s.lastLogs(ctx, containerID, 15))
		case info.Health == "healthy", info.Health == "none" && info.Running():
			return nil
		case info.Health == "unhealthy" && time.Now().After(deadline):
			return fmt.Errorf("container did not become healthy within %s: %s", timeout, s.lastLogs(ctx, containerID, 15))
		case time.Now().After(deadline):
			return fmt.Errorf("container did not become healthy within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// lastLogs returns the tail of a container's output for error messages.
func (s *Service) lastLogs(ctx context.Context, containerID string, n int) string {
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lines, _ := s.dk.Logs(lctx, containerID, docker.LogOptions{Tail: n})
	var out string
	for l := range lines {
		out += "\n  " + l.Text
	}
	return out
}
