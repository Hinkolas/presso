package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hinkolas/presso/internal/auth"
)

func TestPrivatePeer(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:5000", "[::1]:80", "10.1.2.3:1", "172.18.0.2:41234", "192.168.1.9:2", "169.254.1.1:1", "[fd00::1]:1", "[::]:1"} {
		if !privatePeer(ok) {
			t.Errorf("%q should be private", ok)
		}
	}
	for _, bad := range []string{"203.0.113.7:443", "8.8.8.8:1", "[2001:db8::1]:1", "not-an-ip", ""} {
		if privatePeer(bad) {
			t.Errorf("%q should be public", bad)
		}
	}
}

func TestPanelGuard(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	serve := func(domain, path, peer string, headers map[string]string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.7:1" // what realIP would leave when no proxy headers are present
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if peer != "" {
			req = req.WithContext(context.WithValue(req.Context(), peerKey{}, peer))
		}
		rec := httptest.NewRecorder()
		panelGuard(domain)(next).ServeHTTP(rec, req)
		return rec.Code
	}
	// Disabled: nothing is filtered.
	if c := serve("", "/v1/sites", "203.0.113.7:1", nil); c != http.StatusNoContent {
		t.Fatalf("no domain: %d", c)
	}
	// Public peer is refused; proxy headers cannot promote it.
	if c := serve("panel.example.com", "/v1/sites", "203.0.113.7:1", map[string]string{"X-Real-IP": "127.0.0.1", "X-Forwarded-For": "10.0.0.1"}); c != http.StatusForbidden {
		t.Fatalf("public peer: %d", c)
	}
	// The edge (docker bridge), a tunnel and the LAN pass.
	for _, peer := range []string{"172.18.0.5:5123", "127.0.0.1:9", "192.168.0.7:1"} {
		if c := serve("panel.example.com", "/v1/sites", peer, nil); c != http.StatusNoContent {
			t.Fatalf("peer %s: %d", peer, c)
		}
	}
	// Health and the system-plugin callbacks stay reachable from anywhere.
	for _, path := range []string{"/healthz", "/v1/internal/sites/abc/purge"} {
		if c := serve("panel.example.com", path, "203.0.113.7:1", nil); c != http.StatusNoContent {
			t.Fatalf("exempt %s: %d", path, c)
		}
	}
}

func TestSystemActionsWithoutUpdater(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, a.st, "admin@example.com", "A", "hunter2hunter2", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateUser(ctx, a.st, "member@example.com", "M", "hunter2hunter2", auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	admin, _ := a.login("admin@example.com", "hunter2hunter2")
	member, _ := a.login("member@example.com", "hunter2hunter2")
	// Admin only.
	if res, _ := a.do(http.MethodPost, "/v1/system/actions/update", member, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("member: %d", res.StatusCode)
	}
	// No update service wired in the test router → unknown action.
	if res, _ := a.do(http.MethodPost, "/v1/system/actions/update", admin, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d", res.StatusCode)
	}
	// Status carries no update block then.
	res, body := a.do(http.MethodGet, "/v1/system/status", admin, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", res.StatusCode)
	}
	if _, ok := body["update"]; ok {
		t.Fatal("update block present without an updater")
	}
}
