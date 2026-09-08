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
	if _, err := Load[API](context.Background()); err == nil || !strings.Contains(err.Error(), "EDGE_IMAGE") {
		t.Fatalf("expected the {release} edge image to be rejected on a dev build, got %v", err)
	}
	t.Setenv("EDGE_IMAGE", "presso-edge:dev")
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
	if !strings.Contains(cfg.ImageTemplate, "{release}") {
		t.Fatalf("runtime template must stay a template (compat fills it): %q", cfg.ImageTemplate)
	}
}

func TestValkeyConfiguration(t *testing.T) {
	t.Setenv("AUTH_SECRET", strings.Repeat("x", 32))
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("RUNTIME_IMAGE_TEMPLATE", "presso-runtime:php{php}-wp{wp}")
	t.Setenv("EDGE_IMAGE", "presso-edge:dev")
	cfg, err := Load[API](context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ValkeyImage != DefaultValkeyImage || cfg.ValkeyListen != "127.0.0.1:6379" || cfg.ValkeyMaxMemory != "256mb" {
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
