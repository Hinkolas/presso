package config

import (
	"context"
	"strings"
	"testing"

	"github.com/Hinkolas/presso/internal/version"
)

func TestLoadAPIDevBuildNeedsLocalImages(t *testing.T) {
	t.Setenv("AUTH_SECRET", strings.Repeat("x", 32))
	t.Setenv("DATA_DIR", t.TempDir())
	// Defaults name release images; a dev build must be told otherwise.
	if _, err := Load[API](context.Background()); err == nil || !strings.Contains(err.Error(), "RUNTIME_IMAGE_TEMPLATE") {
		t.Fatalf("expected the {release} template to be rejected on a dev build, got %v", err)
	}
	t.Setenv("RUNTIME_IMAGE_TEMPLATE", "presso-runtime:php{php}-wp{wp}")
	for _, key := range []string{"MARIADB_IMAGE", "VALKEY_IMAGE", "EDGE_IMAGE", "SHOTS_IMAGE"} {
		if _, err := Load[API](context.Background()); err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("expected the {release} %s to be rejected on a dev build, got %v", key, err)
		}
		t.Setenv(key, "presso-"+strings.ToLower(strings.TrimSuffix(key, "_IMAGE"))+":dev")
	}
	t.Setenv("PANEL_DOMAIN", " Panel.Example.com ")
	cfg, err := Load[API](context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Panel.Domain != "panel.example.com" || cfg.Edge.RawImage != "presso-edge:dev" || !cfg.Update.Check {
		t.Fatalf("cfg: %+v", cfg)
	}
	t.Setenv("PANEL_DOMAIN", "bad host")
	if _, err := Load[API](context.Background()); err == nil {
		t.Fatal("invalid panel domain accepted")
	}
}

func TestLoadAPIReleaseBuildResolvesTemplates(t *testing.T) {
	old := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = old })
	t.Setenv("AUTH_SECRET", strings.Repeat("x", 32))
	t.Setenv("DATA_DIR", t.TempDir())
	cfg, err := Load[API](context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Edge.Image != "ghcr.io/hinkolas/presso-edge:v1.2.3" || cfg.Edge.RawImage != "ghcr.io/hinkolas/presso-edge:{release}" {
		t.Fatalf("edge image: %q raw %q", cfg.Edge.Image, cfg.Edge.RawImage)
	}
	if cfg.MariaDBImage != "ghcr.io/hinkolas/presso-mariadb:v1.2.3" || cfg.ValkeyImage != "ghcr.io/hinkolas/presso-valkey:v1.2.3" || cfg.Shots.Image != "ghcr.io/hinkolas/presso-browser:v1.2.3" {
		t.Fatalf("platform images: %q %q %q", cfg.MariaDBImage, cfg.ValkeyImage, cfg.Shots.Image)
	}
	want := "ghcr.io/hinkolas/presso-mariadb:v1.3.0 ghcr.io/hinkolas/presso-valkey:v1.3.0 ghcr.io/hinkolas/presso-edge:v1.3.0 ghcr.io/hinkolas/presso-browser:v1.3.0"
	if got := strings.Join(cfg.ReleaseImages("v1.3.0"), " "); got != want {
		t.Fatalf("release images: %s", got)
	}
	// Pinned images do not follow releases; a disabled browser is not pulled.
	t.Setenv("MARIADB_IMAGE", "mariadb:11.8")
	t.Setenv("SHOTS_ENABLED", "false")
	if cfg, err = Load[API](context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.ReleaseImages("v1.3.0"), " "); got != "ghcr.io/hinkolas/presso-valkey:v1.3.0 ghcr.io/hinkolas/presso-edge:v1.3.0" || cfg.MariaDBImage != "mariadb:11.8" {
		t.Fatalf("release images with overrides: %s", got)
	}
	if !strings.Contains(cfg.ImageTemplate, "{release}") {
		t.Fatalf("runtime template must stay a template (compat fills it): %q", cfg.ImageTemplate)
	}
}

func TestValkeyConfiguration(t *testing.T) {
	t.Setenv("AUTH_SECRET", strings.Repeat("x", 32))
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("RUNTIME_IMAGE_TEMPLATE", "presso-runtime:php{php}-wp{wp}")
	t.Setenv("EDGE_IMAGE", "presso-edge:dev")
	t.Setenv("MARIADB_IMAGE", "presso-mariadb:dev")
	t.Setenv("VALKEY_IMAGE", "presso-valkey:dev")
	t.Setenv("SHOTS_IMAGE", "presso-browser:dev")
	cfg, err := Load[API](context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ValkeyImage != "presso-valkey:dev" || cfg.ValkeyListen != "127.0.0.1:6379" || cfg.ValkeyMaxMemory != "256mb" {
		t.Fatalf("unexpected cache defaults: %+v", cfg.Platform)
	}
	t.Setenv("VALKEY_IMAGE", "example/valkey:custom")
	t.Setenv("VALKEY_LISTEN", "127.0.0.1:16379")
	t.Setenv("VALKEY_MAXMEMORY", "128mb")
	cfg, err = Load[API](context.Background())
	if err != nil || cfg.ValkeyImage != "example/valkey:custom" || cfg.ValkeyListen != "127.0.0.1:16379" || cfg.ValkeyMaxMemory != "128mb" {
		t.Fatalf("cache overrides: %+v %v", cfg, err)
	}
	for _, suffix := range []string{"IMAGE", "LISTEN", "MAXMEMORY"} {
		t.Run("obsolete_"+suffix, func(t *testing.T) {
			t.Setenv("REDIS_"+suffix, "old-setting")
			if _, err := Load[API](context.Background()); err == nil || !strings.Contains(err.Error(), "VALKEY_"+suffix) {
				t.Fatalf("obsolete Redis setting must fail with replacement: %v", err)
			}
		})
	}
	for _, addr := range []string{"0.0.0.0:6379", ":6379", "192.168.1.2:6379"} {
		t.Run(addr, func(t *testing.T) {
			t.Setenv("VALKEY_LISTEN", addr)
			if _, err := Load[API](context.Background()); err == nil || !strings.Contains(err.Error(), "VALKEY_LISTEN") {
				t.Fatalf("non-loopback cache accepted: %v", err)
			}
		})
	}
}
