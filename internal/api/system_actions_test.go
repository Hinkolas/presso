package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/control"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/testdb"
	"github.com/Hinkolas/presso/internal/update"
)

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
	// No configured domain still requires a local peer.
	if c := serve("", "/v1/sites", "203.0.113.7:1", nil); c != http.StatusForbidden {
		t.Fatalf("no domain: %d", c)
	}
	// Public peer is refused; proxy headers cannot promote it.
	if c := serve("panel.example.com", "/v1/sites", "203.0.113.7:1", map[string]string{"X-Real-IP": "127.0.0.1", "X-Forwarded-For": "10.0.0.1"}); c != http.StatusForbidden {
		t.Fatalf("public peer: %d", c)
	}
	// Only loopback peers pass without an edge certificate.
	for _, peer := range []string{"127.0.0.1:9", "[::1]:9"} {
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
	if res, _ := a.do(http.MethodPut, "/v1/system/update/settings", member, map[string]any{"channel": "beta"}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("member settings: %d", res.StatusCode)
	}
	if res, _ := a.do(http.MethodPut, "/v1/system/update/settings", admin, map[string]any{"channel": "beta"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("settings without updater: %d", res.StatusCode)
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

func TestInternalListenerRequiresEdgeIdentity(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, path := range []string{"/v1/sites", "/api/v1/sites", "/v1/auth/login", "/healthz", "/"} {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "127.0.0.1:1"
		req.Header.Set("X-Real-IP", "127.0.0.1")
		rec := httptest.NewRecorder()
		InternalOnly(next).ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("internal %s accepted without certificate", path)
		}
	}
	req := httptest.NewRequest("POST", "/v1/internal/sites/test/purge", nil)
	rec := httptest.NewRecorder()
	InternalOnly(next).ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatal("callback must reach bearer-token validation")
	}
}

func TestForwardedHeadersRequireVerifiedEdge(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		req := httptest.NewRequest("GET", "http://panel.localhost/", nil)
		req.RemoteAddr = "172.18.0.4:1000"
		req.Header.Set("X-Real-IP", "198.51.100.9")
		req.Header.Set("X-Forwarded-Proto", "https")
		if trusted {
			cert := &x509.Certificate{Subject: pkix.Name{CommonName: control.EdgeIdentity}}
			req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
		}
		var remote string
		realIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { remote = r.RemoteAddr })).ServeHTTP(httptest.NewRecorder(), req)
		if trusted && remote != "198.51.100.9" {
			t.Fatalf("edge forwarding lost: %s", remote)
		}
		if !trusted && (remote != "172.18.0.4:1000" || isHTTPS(req)) {
			t.Fatalf("spoofed forwarding trusted: %s", remote)
		}
	}
}

func TestUpdateSettingsEndpoint(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"v0.3.0-rc.1","prerelease":true},{"tag_name":"v0.2.0"}]`))
	}))
	defer feed.Close()
	st := testdb.New(t)
	authSvc := auth.New(st, auth.Config{})
	cfg := &config.API{Base: config.Base{DataDir: t.TempDir()}, Update: config.Update{Check: true, Interval: time.Hour, Repo: "acme/presso", APIBase: feed.URL}}
	upd := update.New(update.Deps{Config: cfg, Store: st, Jobs: jobs.New(st, nil, 1)})
	srv := httptest.NewServer(StripAPIPrefix(NewRouter(Deps{Auth: authSvc, Store: st, DB: st.DB, Version: "test", Config: cfg, Update: upd})))
	defer srv.Close()
	a := &testAPI{t: t, srv: srv, st: st}
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, st, "admin@example.com", "A", "hunter2hunter2", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	admin, _ := a.login("admin@example.com", "hunter2hunter2")

	res, body := a.do(http.MethodPut, "/v1/system/update/settings", admin, map[string]any{"channel": "beta", "auto_update": true})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %v", res.StatusCode, body)
	}
	st1, _ := body["update"].(map[string]any)
	if st1["channel"] != "beta" || st1["auto_update"] != true || st1["latest"] != "v0.3.0-rc.1" || st1["prerelease"] != true {
		t.Fatalf("status after beta: %v", st1)
	}
	// Partial update keeps the other field.
	_, body = a.do(http.MethodPut, "/v1/system/update/settings", admin, map[string]any{"auto_update": false})
	if st2, _ := body["update"].(map[string]any); st2["channel"] != "beta" || st2["auto_update"] != false {
		t.Fatalf("partial update: %v", st2)
	}
	for _, bad := range []map[string]any{{"channel": "nightly"}, {"channel": "beta", "extra": 1}} {
		if res, body := a.do(http.MethodPut, "/v1/system/update/settings", admin, bad); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%v: %d %v", bad, res.StatusCode, body)
		}
	}
}
