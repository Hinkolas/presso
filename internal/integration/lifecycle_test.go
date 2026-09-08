//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/api"
	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/control"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
	"github.com/google/uuid"
)

type lifecyclePlatform struct {
	dk        *docker.Moby
	db, redis string
	edge      *edge.Client
	render    platform.EdgeRenderer
	mu        sync.Mutex
}

func (p *lifecyclePlatform) SetEdgeRenderer(r platform.EdgeRenderer) { p.render = r }
func (p *lifecyclePlatform) ApplyEdge(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, err := p.render(ctx)
	if err != nil {
		return err
	}
	return p.edge.Load(ctx, s)
}
func (p *lifecyclePlatform) sql(ctx context.Context, sql string) error {
	r, err := p.dk.Exec(ctx, p.db, docker.ExecOptions{Cmd: []string{"mariadb", "--protocol=tcp", "-h127.0.0.1", "-uroot"}, Stdin: strings.NewReader(sql)})
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("sql exited %d: %s", r.ExitCode, r.Stderr)
	}
	return nil
}
func (p *lifecyclePlatform) CreateSiteDatabase(ctx context.Context, db, user, pw string) error {
	return p.sql(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`; CREATE USER IF NOT EXISTS '%s'@'%%' IDENTIFIED BY '%s'; GRANT ALL ON `%s`.* TO '%s'@'%%';", db, user, pw, db, user))
}
func (p *lifecyclePlatform) DropSiteDatabase(ctx context.Context, db, user string) error {
	return p.sql(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`; DROP USER IF EXISTS '%s'@'%%';", db, user))
}
func (p *lifecyclePlatform) ApplyRedisACL(ctx context.Context, user, pw, prefix string) error {
	r, err := p.dk.Exec(ctx, p.redis, docker.ExecOptions{Cmd: []string{"redis-cli", "ACL", "SETUSER", user, "on", ">" + pw, "~" + prefix + "*", "+@all"}})
	if err != nil {
		return err
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("redis: %s", r.Stderr)
	}
	return nil
}
func (p *lifecyclePlatform) DeleteRedisACL(ctx context.Context, user string) error {
	_, err := p.dk.Exec(ctx, p.redis, docker.ExecOptions{Cmd: []string{"redis-cli", "ACL", "DELUSER", user}})
	return err
}

type pausedInstall struct {
	*docker.Moby
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *pausedInstall) Exec(ctx context.Context, id string, o docker.ExecOptions) (docker.ExecResult, error) {
	if strings.Contains(strings.Join(o.Cmd, " "), "core install") {
		d.once.Do(func() { close(d.entered) })
		select {
		case <-d.release:
		case <-ctx.Done():
			return docker.ExecResult{}, ctx.Err()
		}
	}
	return d.Moby.Exec(ctx, id, o)
}

func TestFreshSiteLifecycleAndHTTPSCallbacks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	dk, err := docker.New("")
	if err != nil {
		t.Fatal(err)
	}
	defer dk.Close()
	name := "presso-test-" + uuid.NewString()
	network := name + "-net"
	if _, err = dk.EnsureNetwork(ctx, network, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		out, err := exec.Command("docker", "network", "rm", network).CombinedOutput()
		if err != nil {
			t.Errorf("cleanup: %s %v", out, err)
		}
	})
	create := func(spec docker.Spec) string {
		t.Helper()
		if err := dk.EnsureImage(ctx, spec.Image, nil); err != nil {
			t.Fatal(err)
		}
		cid, err := dk.CreateContainer(ctx, spec)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = dk.RemoveContainer(context.Background(), cid, true) })
		if err := dk.StartContainer(ctx, cid); err != nil {
			t.Fatal(err)
		}
		return cid
	}
	dir := t.TempDir()
	trust, err := control.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	database := create(docker.Spec{Name: name + "-db", Image: "mariadb:11.4", Network: network, NetworkAliases: []string{platform.NameMariaDB}, Env: []string{"MARIADB_ALLOW_EMPTY_ROOT_PASSWORD=1"}, Mounts: []docker.Mount{{Type: docker.MountTmpfs, Target: "/var/lib/mysql"}}})
	redis := create(docker.Spec{Name: name + "-redis", Image: "redis:7-alpine", Network: network, NetworkAliases: []string{platform.NameRedis}})
	edgeID := create(docker.Spec{Name: name + "-edge", Image: "presso-edge:release-verification", Network: network, NetworkAliases: []string{platform.NameEdge}, Mounts: []docker.Mount{{Type: docker.MountBind, Source: filepath.Join(dir, "control/edge"), Target: "/etc/presso/control", ReadOnly: true}}, Ports: []docker.PortBinding{{HostIP: "127.0.0.1", HostPort: 0, ContainerPort: 80}}})
	portBytes, err := exec.Command("docker", "port", edgeID, "80/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	address := strings.TrimSpace(string(portBytes))
	p := &lifecyclePlatform{dk: dk, db: database, redis: redis, edge: edge.NewDockerClient(dk, edgeID, dir)}
	for {
		if err = p.sql(ctx, "SELECT 1;"); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(err)
		case <-time.After(time.Second):
		}
	}
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := &config.API{Base: config.Base{DataDir: dir}, Platform: config.Platform{Network: network, EngineInternalURL: fmt.Sprintf("https://host.docker.internal:%d", listener.Addr().(*net.TCPAddr).Port)}}
	cat, err := compat.New(compat.Default, compat.Options{ImageTemplate: "presso-runtime:" + name + "-wp{wp}-php{php}"})
	if err != nil {
		t.Fatal(err)
	}
	// The same test tag contains the catalog's default pair; a harmless suffix
	// satisfies the template contract while avoiding any release-tag writes.
	pair, err := cat.Resolve("", "")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("docker", "tag", "presso-runtime:release-verification", pair.Image).CombinedOutput(); err != nil {
		t.Fatalf("runtime image: %s %v", out, err)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", pair.Image).Run() })
	st := testdb.New(t)
	runner := jobs.New(st, nil, 1)
	box, _ := secrets.New(strings.Repeat("x", 32))
	paused := &pausedInstall{Moby: dk, entered: make(chan struct{}), release: make(chan struct{})}
	svc := site.New(site.Deps{Config: cfg, Catalog: cat, Store: st, Docker: paused, Secrets: box, Jobs: runner, Platform: p, Edge: p.edge})
	server := &http.Server{Handler: api.InternalOnly(api.NewRouter(api.Deps{Store: st, DB: st.DB, Config: cfg, Sites: svc}))}
	go server.Serve(tls.NewListener(listener, trust.ServerTLS()))
	defer server.Close()
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); runner.Wait() })
	wait := func(j store.Job) {
		t.Helper()
		for {
			row, err := st.GetJob(ctx, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if row.Status == jobs.StatusFailed {
				t.Fatal(row.Error)
			}
			if row.Status == jobs.StatusSucceeded {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	slug := "test-" + uuid.NewString()[:8]
	host := slug + ".localhost"
	row, j, err := svc.Create(ctx, site.CreateInput{Slug: slug, Name: "Integration", PrimaryDomain: host, AdminUser: "admin", AdminEmail: "admin@example.com", AdminPassword: "isolated-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		runner.Wait()
		_ = dk.RemoveContainer(context.Background(), site.ContainerName(slug), true)
		record, err := st.GetSiteByID(context.Background(), row.ID)
		if err == nil {
			_ = dk.RemoveVolume(context.Background(), record.VolumeName)
		}
	})
	fetch := func(want int) http.Header {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/", nil)
		req.Host = host
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("site HTTP %d want %d: %.200s", res.StatusCode, want, body)
		}
		return res.Header
	}
installationWait:
	for {
		select {
		case <-paused.entered:
			break installationWait
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
			state, err := st.GetJob(ctx, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Status == jobs.StatusFailed {
				t.Fatal(state.Error)
			}
		}
	}

	if err := p.ApplyEdge(ctx); err != nil {
		t.Fatal(err)
	}
	if h := fetch(503); h.Get("Cache-Control") != "no-store" {
		t.Fatal(h)
	}
	if err := dk.RestartContainer(ctx, edgeID, time.Second); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	portBytes, err = exec.Command("docker", "port", edgeID, "80/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	address = strings.TrimSpace(string(portBytes))
	fetch(503)
	if err := p.ApplyEdge(ctx); err != nil {
		t.Fatal(err)
	}
	fetch(503)
	close(paused.release)
	wait(j)
	fetch(200)
	if h := fetch(200); !strings.Contains(h.Get("Cache-Status"), "hit") {
		t.Fatalf("page was not cached before stop: %v", h)
	}
	// Execute WordPress's HTTP client with the exact mounted CA and callback
	// credentials. This verifies TLS from the actual runtime namespace.
	script := `$r=wp_remote_post(getenv('PRESSO_ENGINE_URL').'/v1/internal/sites/'.getenv('PRESSO_SITE_ID').'/purge',['sslcertificates'=>'/opt/presso/control-ca.pem','headers'=>['Authorization'=>'Bearer '.getenv('PRESSO_PURGE_TOKEN'),'Content-Type'=>'application/json'],'body'=>'{"all":true}']); if(is_wp_error($r)){WP_CLI::error($r->get_error_message());} if(wp_remote_retrieve_response_code($r)!==200){WP_CLI::error('callback status '.wp_remote_retrieve_response_code($r));}`
	res, err := svc.RunWP(ctx, row.ID, []string{"eval", script}, "test")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("HTTPS callback: %+v %v", res, err)
	}
	j, err = svc.Stop(ctx, row.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	wait(j)
	fetch(503)
	updated, child, err := svc.Update(ctx, row.ID, site.UpdateInput{Settings: map[string]json.RawMessage{"container_memory_mb": json.RawMessage("384"), "php_memory_limit": json.RawMessage(`"192M"`)}})
	if err != nil || child != nil || !updated.ConfigurationPending {
		t.Fatalf("stopped settings: %+v %v %v", updated, child, err)
	}
	j, err = svc.Start(ctx, row.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	wait(j)
	fetch(200)
	res, err = svc.RunWP(ctx, row.ID, []string{"eval", "echo WP_MEMORY_LIMIT;"}, "test")
	if err != nil || !strings.Contains(res.Stdout, "192M") {
		t.Fatalf("settings not applied: %+v %v", res, err)
	}
	mem, err := exec.Command("docker", "inspect", "--format", "{{.HostConfig.Memory}}", site.ContainerName(slug)).Output()
	if err != nil || strings.TrimSpace(string(mem)) != "402653184" {
		t.Fatalf("resource limit not applied: %s %v", mem, err)
	}
	j, err = svc.Delete(ctx, row.ID, slug, false, "test")
	if err != nil {
		t.Fatal(err)
	}
	wait(j)
	fetch(404)
}
