package shots

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNilServiceIsSafe(t *testing.T) {
	var s *Service
	if _, err := s.Capture(context.Background(), "http://x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Capture: %v", err)
	}
	if _, err := s.Refresh(context.Background(), uuid.New(), "http://x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Refresh: %v", err)
	}
	if _, ok := s.Stat(uuid.New()); ok {
		t.Fatal("Stat on nil should be false")
	}
	if err := s.Remove(uuid.New()); err != nil {
		t.Fatal(err)
	}
}

func TestUnreachableBrowser(t *testing.T) {
	s := New(t.TempDir(), "127.0.0.1:1", 2*time.Second)
	if _, err := s.Capture(context.Background(), "http://x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestStatAndRemove(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "0.0.0.0:9222", time.Second)
	if s.base != "http://127.0.0.1:9222" {
		t.Fatalf("base: %s", s.base)
	}
	id := uuid.New()
	if _, ok := s.Stat(id); ok {
		t.Fatal("unexpected preview")
	}
	if err := os.WriteFile(filepath.Join(dir, id.String()+".jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Stat(id); !ok {
		t.Fatal("expected preview")
	}
	if err := s.Remove(id); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(id); err != nil {
		t.Fatal("second remove should be nil")
	}
}
