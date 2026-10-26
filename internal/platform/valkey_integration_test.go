//go:build integration

package platform

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
	"github.com/Hinkolas/presso/internal/testimages"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Exercise the production spec, admin client, ACL policy and reconciliation
// against the pinned server. Only test-specific containers/ports/state are used.
func TestValkeyCacheIsolationAndRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dk, err := docker.New("")
	if err != nil {
		t.Fatal(err)
	}
	defer dk.Close()
	st := testdb.New(t)
	box, _ := secrets.New(strings.Repeat("v", 32))
	cfg := testConfig()
	cfg.ValkeyImage = testimages.Valkey()
	cfg.Network = "" // Only a random host-loopback test port is needed.
	svc := New(cfg, dk, st, box, nil, nil)
	defer svc.Close()
	spec, err := svc.valkeySpec(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != NameValkey || spec.NetworkAliases[0] != "valkey" || spec.Labels[docker.LabelKind] != "valkey" {
		t.Fatalf("wrong cache identity: %+v", spec)
	}
	if len(spec.Mounts) != 0 || spec.Ports[0].HostIP != "127.0.0.1" {
		t.Fatal("cache must not reuse persisted Redis data or expose a public port")
	}
	spec.Name = "presso-test-valkey-" + uuid.NewString()
	spec.NetworkAliases = nil
	spec.Ports[0].HostPort = 0
	spec.RestartPolicy = "no"
	if err := dk.EnsureImage(ctx, spec.Image, nil); err != nil {
		t.Fatal(err)
	}
	cid, err := dk.CreateContainer(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dk.RemoveContainer(context.Background(), cid, true); err != nil {
			t.Error(err)
		}
	})
	if err := dk.StartContainer(ctx, cid); err != nil {
		t.Fatal(err)
	}
	setAddress := func() {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", "port", cid, "6379/tcp").Output()
		if err != nil {
			t.Fatal(err)
		}
		cfg.ValkeyListen = strings.TrimSpace(string(out))
	}
	setAddress()
	admin, err := svc.Valkey(ctx, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	info, err := admin.Info(ctx, "server").Result()
	if err != nil || !strings.Contains(info, "valkey_version:9.0.") {
		t.Fatalf("unexpected cache server: %s %v", info, err)
	}
	for key, want := range map[string]string{"maxmemory": "67108864", "maxmemory-policy": "volatile-lru", "save": "", "appendonly": "no"} {
		values, err := admin.ConfigGet(ctx, key).Result()
		if err != nil || values[key] != want {
			t.Fatalf("cache config %s = %v, want %s: %v", key, values, want, err)
		}
	}
	// Wait for Docker's actual configured health check, not just our client.
	for {
		state, err := dk.InspectContainer(ctx, cid)
		if err != nil {
			t.Fatal(err)
		}
		if state.Health == "healthy" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Valkey health check never passed: %s", state.Health)
		case <-time.After(200 * time.Millisecond):
		}
	}
	client := func(user, password string) *redis.Client {
		c := redis.NewClient(&redis.Options{Addr: cfg.ValkeyListen, Username: user, Password: password, MaxRetries: -1})
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	if err := client("", "").Ping(ctx).Err(); err == nil || !strings.Contains(err.Error(), "NOAUTH") {
		t.Fatalf("unauthenticated access: %v", err)
	}
	id := uuid.New()
	prefix := CacheKeyPrefix(id.String())
	pw, _ := box.EncryptString("site-test-password")
	_, err = st.CreateSite(ctx, store.CreateSiteParams{
		ID: id, Slug: "cache-test", Name: "Cache test", Status: "running", PrimaryDomain: "cache.localhost",
		TlsMode: "off", PhpVersion: "8.5", Image: "test:1", Profile: "default", FileMods: "locked",
		DbName: "cache", DbUser: "cache", DbPasswordEnc: pw, RedisUser: "site-test", RedisPasswordEnc: pw,
		SaltsEnc: pw, PurgeTokenHash: pw, LoginSecretEnc: pw, AdminUser: "admin", AdminEmail: "a@example.com",
		Settings: "{}", CreatedAt: time.Now(), UpdatedAt: time.Now(), Runtime: "frankenphp", WpVersion: "7.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.applyAllCacheACLs(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyCacheACL(ctx, "other-site", "other-password", "site-other:"); err != nil {
		t.Fatal(err)
	}
	one, other := client("site-test", "site-test-password"), client("other-site", "other-password")
	if err := client("site-test", "wrong-password").Ping(ctx).Err(); err == nil {
		t.Fatal("wrong site password accepted")
	}
	if err := one.Set(ctx, prefix+"object", "cached", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if got, err := one.Get(ctx, prefix+"object").Result(); err != nil || got != "cached" {
		t.Fatalf("cache read: %q %v", got, err)
	}
	if err := other.Set(ctx, "site-other:object", "other", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]any{
		{"GET", "site-other:object"}, {"SET", "site-other:object", "stolen"}, {"DEL", "site-other:object"},
		{"FLUSHDB"}, {"FLUSHALL"}, {"ACL", "LIST"}, {"CONFIG", "GET", "*"}, {"KEYS", "*"},
		{"EVAL", "return redis.call('GET', 'site-other:object')", 0},
	} {
		if err := one.Do(ctx, args...).Err(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "permission") && !strings.Contains(err.Error(), "NOPERM") {
			t.Fatalf("cross-site/admin command %v was not denied: %v", args, err)
		}
	}
	if err := dk.RestartContainer(ctx, cid, time.Second); err != nil {
		t.Fatal(err)
	}
	// Docker can allocate another host port on restart. Reconnect the engine
	// as it would after a process restart, with the same encrypted state.
	svc.Close()
	setAddress()
	svc = New(cfg, dk, st, box, nil, nil)
	defer svc.Close()
	admin, err = svc.Valkey(ctx, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Get(ctx, prefix+"object").Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("cache unexpectedly persisted: %v", err)
	}
	if err := svc.applyAllCacheACLs(ctx); err != nil {
		t.Fatal(err)
	}
	one = client("site-test", "site-test-password")
	if err := one.Set(ctx, prefix+"recovered", "ok", time.Minute).Err(); err != nil {
		t.Fatalf("persisted site ACL not restored after restart: %v", err)
	}
	if err := svc.DeleteCacheACL(ctx, "site-test"); err != nil {
		t.Fatal(err)
	}
	if err := client("site-test", "site-test-password").Ping(ctx).Err(); err == nil {
		t.Fatal("deleted site still authenticates")
	}
}
