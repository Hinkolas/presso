package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/docker/fake"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
)

// fakePlatform satisfies site.Platform without MariaDB/Redis/Caddy.
type fakePlatform struct{}

func (fakePlatform) CreateSiteDatabase(context.Context, string, string, string) error { return nil }
func (fakePlatform) DropSiteDatabase(context.Context, string, string) error           { return nil }
func (fakePlatform) ApplyRedisACL(context.Context, string, string, string) error      { return nil }
func (fakePlatform) DeleteRedisACL(context.Context, string) error                     { return nil }
func (fakePlatform) ApplyEdge(context.Context) error                                  { return nil }
func (fakePlatform) SetEdgeRenderer(platform.EdgeRenderer)                            {}

// testAPI runs the real router over a real migrated SQLite database and the
// in-memory docker client.
type testAPI struct {
	t   *testing.T
	srv *httptest.Server
	st  *store.Store
	dk  *fake.Client
	hub *events.Hub
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	st := testdb.New(t)
	authSvc := auth.New(st, auth.Config{})
	dk := fake.New()
	hub := events.New()
	box, _ := secrets.New(strings.Repeat("k", 32))
	cfg := &config.API{
		Platform: config.Platform{Network: "presso", EngineInternalURL: "http://host.docker.internal:7080"},
		Runtime:  config.Runtime{ImageTemplate: "presso-runtime:php{php}-wp{wp}", LegacyEnabled: true},
		Edge:     config.Edge{HTTPListen: "127.0.0.1:1", DefaultCacheTTL: 12 * time.Hour},
	}
	// Full compatibility matrix (no build policy) so the lifecycle test can
	// move a site to another PHP version; the policy is covered in compat.
	matrix := *compat.Default
	matrix.Build = compat.Build{}
	catalog, err := compat.New(&matrix, compat.Options{ImageTemplate: cfg.ImageTemplate, LegacyEnabled: cfg.LegacyEnabled})
	if err != nil {
		t.Fatal(err)
	}
	runner := jobs.New(st, hub, 2)
	sites := site.New(site.Deps{Config: cfg, Catalog: catalog, Store: st, Docker: dk, Secrets: box, Hub: hub, Jobs: runner, Platform: fakePlatform{}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>ui</html>")) })
	h := StripAPIPrefix(NewRouter(Deps{Auth: authSvc, Store: st, DB: st.DB, Version: "test", Config: cfg, Catalog: catalog, Sites: sites, Hub: hub, Docker: dk, WebUI: ui}))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &testAPI{t: t, srv: srv, st: st, dk: dk, hub: hub}
}

type reqOpt func(*http.Request)

func withCookie(v string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: SessionCookie, Value: v}) }
}
func withCSRF() reqOpt { return func(r *http.Request) { r.Header.Set(CSRFHeader, CSRFValue) } }

func (a *testAPI) doRaw(method, path, token string, body any, opts ...reqOpt) *http.Response {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, a.srv.URL+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, o := range opts {
		o(req)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func (a *testAPI) do(method, path, token string, body any, opts ...reqOpt) (*http.Response, map[string]any) {
	a.t.Helper()
	res := a.doRaw(method, path, token, body, opts...)
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func (a *testAPI) login(email, password string) (string, *http.Cookie) {
	a.t.Helper()
	res := a.doRaw("POST", "/v1/auth/login", "", map[string]string{"email": email, "password": password})
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != http.StatusOK {
		a.t.Fatalf("login: %d %v", res.StatusCode, body)
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == SessionCookie {
			cookie = c
		}
	}
	return body["session"].(map[string]any)["token"].(string), cookie
}

func TestAuthFlow(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, a.st, "nick@example.com", "Nick", "hunter2hunter2", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	res, body := a.do("GET", "/v1/auth/session", "", nil)
	if res.StatusCode != http.StatusUnauthorized || errorCode(body) != "invalid_token" {
		t.Fatalf("unauthenticated: %d %v", res.StatusCode, body)
	}

	res, body = a.do("POST", "/v1/auth/login", "", map[string]string{"email": "nick@example.com", "password": "nope-nope-nope"})
	if res.StatusCode != http.StatusUnauthorized || errorCode(body) != "invalid_credentials" {
		t.Fatalf("bad login: %d %v", res.StatusCode, body)
	}

	token, cookie := a.login("nick@example.com", "hunter2hunter2")
	if cookie == nil || !cookie.HttpOnly || cookie.Value != token {
		t.Fatalf("session cookie: %+v", cookie)
	}

	// The /api prefix reaches the same routes; bearer works.
	res, body = a.do("GET", "/api/v1/auth/session", token, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get(VersionHeader) != "test" {
		t.Fatalf("session via /api: %d %v", res.StatusCode, body)
	}
	if body["user"].(map[string]any)["email"] != "nick@example.com" {
		t.Fatalf("wrong user: %v", body["user"])
	}

	// Cookie auth works for reads and needs the CSRF header for writes.
	res, _ = a.do("GET", "/v1/auth/session", "", nil, withCookie(token))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cookie read: %d", res.StatusCode)
	}
	res, body = a.do("POST", "/v1/auth/password", "", map[string]string{"current_password": "x", "new_password": "y"}, withCookie(token))
	if res.StatusCode != http.StatusForbidden || errorCode(body) != "forbidden" {
		t.Fatalf("cookie write without csrf header: %d %v", res.StatusCode, body)
	}
	res, body = a.do("POST", "/v1/auth/password", "", map[string]string{"current_password": "x", "new_password": "y"}, withCookie(token), withCSRF())
	if res.StatusCode == http.StatusForbidden {
		t.Fatalf("cookie write with csrf header rejected: %d %v", res.StatusCode, body)
	}

	res, body = a.do("GET", "/v1/system/meta", token, nil)
	if res.StatusCode != http.StatusOK || body["version"] != "test" {
		t.Fatalf("meta: %d %v", res.StatusCode, body)
	}
	res, body = a.do("GET", "/v1/system/runtime", token, nil)
	if res.StatusCode != http.StatusOK || body["default_wp"] != "7.1" || body["legacy_enabled"] != true || len(body["wordpress"].([]any)) != 16 || len(body["runtimes"].([]any)) != 2 {
		t.Fatalf("runtime: %d %v", res.StatusCode, body)
	}

	// Logout through the cookie clears it.
	res = a.doRaw("POST", "/v1/auth/logout", "", nil, withCookie(token), withCSRF())
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	cleared := false
	for _, c := range res.Cookies() {
		if c.Name == SessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not clear the cookie")
	}
	res, body = a.do("GET", "/v1/auth/session", token, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout: %d %v", res.StatusCode, body)
	}
}

func TestSiteLifecycle(t *testing.T) {
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

	sub, cancelSub := a.hub.Subscribe(128)
	defer cancelSub()

	create := map[string]any{"name": "Demo", "admin_email": "a@example.com", "admin_password": "supersecret1"}
	res, body := a.do("POST", "/v1/sites", member, create)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("member create: %d %v", res.StatusCode, body)
	}
	res, body = a.do("POST", "/v1/sites", admin, create)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("create: %d %v", res.StatusCode, body)
	}
	siteObj := body["site"].(map[string]any)
	id := siteObj["id"].(string)
	if siteObj["slug"] != "demo" || siteObj["status"] != "provisioning" || siteObj["url"] != "http://demo.localhost" ||
		siteObj["wp_version"] != "7.1" || siteObj["php_version"] != "8.5" || siteObj["runtime"] != "frankenphp" {
		t.Fatalf("site: %v", siteObj)
	}
	jobID := body["job"].(map[string]any)["id"].(string)

	// Wait for the provisioning job to finish.
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, body = a.do("GET", "/v1/jobs/"+jobID, member, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("job: %d %v", res.StatusCode, body)
		}
		status := body["job"].(map[string]any)["status"]
		if status == "succeeded" {
			break
		}
		if status == "failed" || time.Now().After(deadline) {
			t.Fatalf("provision: %v", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if evs := body["events"].([]any); len(evs) == 0 {
		t.Fatal("no job events")
	}

	res, body = a.do("GET", "/v1/sites/"+id, member, nil)
	if res.StatusCode != http.StatusOK || body["site"].(map[string]any)["status"] != "running" || body["active_job"] != nil {
		t.Fatalf("get: %d %v", res.StatusCode, body)
	}
	res, body = a.do("GET", "/v1/sites", member, nil)
	if res.StatusCode != http.StatusOK || len(body["sites"].([]any)) != 1 {
		t.Fatalf("list: %d %v", res.StatusCode, body)
	}

	// Duplicate slug → conflict code.
	res, body = a.do("POST", "/v1/sites", admin, create)
	if res.StatusCode != http.StatusConflict || errorCode(body) != "slug_taken" {
		t.Fatalf("dup: %d %v", res.StatusCode, body)
	}
	// Validation error.
	res, body = a.do("POST", "/v1/sites", admin, map[string]any{"name": "x", "slug": "y", "admin_email": "bad", "admin_password": "supersecret1"})
	if res.StatusCode != http.StatusBadRequest || errorCode(body) != "bad_request" {
		t.Fatalf("validation: %d %v", res.StatusCode, body)
	}
	// Version choices outside the matrix get their own codes.
	res, body = a.do("POST", "/v1/sites", admin, map[string]any{"name": "x", "slug": "yy", "wp_version": "5.9", "php_version": "8.4", "admin_email": "a@example.com", "admin_password": "supersecret1"})
	if res.StatusCode != http.StatusBadRequest || errorCode(body) != "php_incompatible" {
		t.Fatalf("incompatible: %d %v", res.StatusCode, body)
	}
	res, body = a.do("POST", "/v1/sites", admin, map[string]any{"name": "x", "slug": "yy", "wp_version": "4.9", "admin_email": "a@example.com", "admin_password": "supersecret1"})
	if res.StatusCode != http.StatusBadRequest || errorCode(body) != "wp_unsupported" {
		t.Fatalf("unsupported: %d %v", res.StatusCode, body)
	}

	// Login link, wp, stats, metrics, activity.
	res, body = a.do("POST", "/v1/sites/"+id+"/login-link", admin, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body["url"].(string), "presso_login=") {
		t.Fatalf("login link: %d %v", res.StatusCode, body)
	}
	res, body = a.do("POST", "/v1/sites/"+id+"/wp", admin, map[string]any{"args": []string{"option", "get", "home"}})
	if res.StatusCode != http.StatusOK || body["exit_code"] != float64(0) {
		t.Fatalf("wp: %d %v", res.StatusCode, body)
	}
	res, body = a.do("POST", "/v1/sites/"+id+"/wp", member, map[string]any{"args": []string{"option", "get", "home"}})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("member wp: %d", res.StatusCode)
	}
	res, body = a.do("GET", "/v1/sites/"+id+"/stats", member, nil)
	if res.StatusCode != http.StatusOK || body["mem_limit"] == nil {
		t.Fatalf("stats: %d %v", res.StatusCode, body)
	}
	res, body = a.do("GET", "/v1/sites/"+id+"/metrics?range=24h", member, nil)
	if res.StatusCode != http.StatusOK || body["step_seconds"] != float64(300) {
		t.Fatalf("metrics: %d %v", res.StatusCode, body)
	}
	res, body = a.do("GET", "/v1/sites/"+id+"/activity", member, nil)
	if res.StatusCode != http.StatusOK || len(body["activity"].([]any)) < 2 {
		t.Fatalf("activity: %d %v", res.StatusCode, body)
	}

	// Internal purge: wrong token rejected, right token accepted.
	res, body = a.do("POST", "/v1/internal/sites/"+id+"/purge", "wrong", map[string]any{"all": true})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("purge bad token: %d %v", res.StatusCode, body)
	}
	var purgeToken string
	for _, e := range a.dk.Created[0].Env {
		if strings.HasPrefix(e, "PRESSO_PURGE_TOKEN=") {
			purgeToken = strings.TrimPrefix(e, "PRESSO_PURGE_TOKEN=")
		}
	}
	res, body = a.do("POST", "/v1/internal/sites/"+id+"/purge", purgeToken, map[string]any{"tags": []string{"s" + id + ":p1"}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("purge: %d %v", res.StatusCode, body)
	}

	// PATCH: settings validation and a recreate job for a PHP change.
	res, body = a.do("PATCH", "/v1/sites/"+id, admin, map[string]any{"settings": map[string]any{"max_input_vars": 5}})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad settings: %d %v", res.StatusCode, body)
	}
	res, body = a.do("PATCH", "/v1/sites/"+id, admin, map[string]any{"php_version": "8.4"})
	if res.StatusCode != http.StatusOK || body["job"] == nil {
		t.Fatalf("patch: %d %v", res.StatusCode, body)
	}
	waitJob(t, a, admin, body["job"].(map[string]any)["id"].(string))
	// Cache TTL / SWR reach the plugin through the container env: recreate.
	res, body = a.do("PATCH", "/v1/sites/"+id, admin, map[string]any{"settings": map[string]any{"cache_ttl": 600, "cache_swr": true}})
	if res.StatusCode != http.StatusOK || body["job"] == nil {
		t.Fatalf("patch cache: %d %v", res.StatusCode, body)
	}
	waitJob(t, a, admin, body["job"].(map[string]any)["id"].(string))
	if env := strings.Join(a.dk.Created[len(a.dk.Created)-1].Env, "\n"); !strings.Contains(env, "PRESSO_CACHE_SWR=on\n") || !strings.Contains(env, "PRESSO_CACHE_TTL=600\n") {
		t.Fatalf("env after cache patch: %s", env)
	}
	// The warmer opt-in is engine-side: no job.
	res, body = a.do("PATCH", "/v1/sites/"+id, admin, map[string]any{"settings": map[string]any{"cache_warm": true}})
	if res.StatusCode != http.StatusOK || body["job"] != nil || body["site"].(map[string]any)["settings"].(map[string]any)["cache_warm"] != true {
		t.Fatalf("patch cache_warm: %d %v", res.StatusCode, body)
	}

	// Delete requires the slug.
	res, body = a.do("DELETE", "/v1/sites/"+id, admin, map[string]any{"confirm": "nope"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete confirm: %d %v", res.StatusCode, body)
	}
	res, body = a.do("DELETE", "/v1/sites/"+id, admin, map[string]any{"confirm": "demo"})
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("delete: %d %v", res.StatusCode, body)
	}
	waitJob(t, a, admin, body["job"].(map[string]any)["id"].(string))
	res, _ = a.do("GET", "/v1/sites/"+id, admin, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete: %d", res.StatusCode)
	}

	// The hub carried site and job updates for the SSE stream.
	seen := map[string]bool{}
	timeout := time.After(time.Second)
	for !(seen[events.TypeSiteStatus] && seen[events.TypeJobUpdated] && seen[events.TypeSiteDeleted]) {
		select {
		case ev := <-sub:
			seen[ev.Type] = true
		case <-timeout:
			t.Fatalf("hub events: %v", seen)
		}
	}

	// Docker side is clean.
	if len(a.dk.Containers) != 0 {
		t.Fatalf("containers left: %d", len(a.dk.Containers))
	}
}

func waitJob(t *testing.T, a *testAPI, token, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, body := a.do("GET", "/v1/jobs/"+id, token, nil)
		switch body["job"].(map[string]any)["status"] {
		case "succeeded":
			return
		case "failed":
			t.Fatalf("job failed: %v", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job timeout")
}

func TestEventsStream(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, a.st, "nick@example.com", "Nick", "hunter2hunter2", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	token, _ := a.login("nick@example.com", "hunter2hunter2")
	req, _ := http.NewRequest("GET", a.srv.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := http.DefaultClient.Do(req.WithContext(rctx))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		a.hub.Publish(events.TypePlatformStatus, []string{"x"})
	}()
	// The stream opens with a comment so proxies release it; keep reading
	// until the published event shows up.
	var got strings.Builder
	buf := make([]byte, 4096)
	for !strings.Contains(got.String(), "event: platform.status") {
		n, err := res.Body.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			t.Fatalf("stream ended: %q (%v)", got.String(), err)
		}
	}
}

func TestStrictJSON(t *testing.T) {
	a := newTestAPI(t)
	res, body := a.do("POST", "/v1/auth/login", "", map[string]string{"email": "x@example.com", "password": "p", "extra": "field"})
	if res.StatusCode != http.StatusBadRequest || errorCode(body) != "bad_request" {
		t.Fatalf("unknown field accepted: %d %v", res.StatusCode, body)
	}
}

func TestHealthzAndUI(t *testing.T) {
	a := newTestAPI(t)
	res, body := a.do("GET", "/healthz", "", nil)
	if res.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("healthz: %d %v", res.StatusCode, body)
	}
	res = a.doRaw("GET", "/sites/abc", "", nil)
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(b), "ui") {
		t.Fatalf("spa fallback: %d %s", res.StatusCode, b)
	}
	res, body = a.do("GET", "/v1/nope", "", nil)
	if res.StatusCode != http.StatusNotFound || errorCode(body) != "not_found" {
		t.Fatalf("api 404: %d %v", res.StatusCode, body)
	}
}

var _ docker.Client = (*fake.Client)(nil)
