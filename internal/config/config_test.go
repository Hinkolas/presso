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
