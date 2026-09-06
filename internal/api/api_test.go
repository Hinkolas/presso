package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hinkolas/presso/internal/auth"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
)

// testAPI runs the real router over a real migrated SQLite database.
type testAPI struct {
	t   *testing.T
	srv *httptest.Server
	st  *store.Store
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	st := testdb.New(t)
	authSvc := auth.New(st, auth.Config{})
	h := StripAPIPrefix(NewRouter(Deps{Auth: authSvc, Store: st, DB: st.DB, Version: "test"}))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &testAPI{t: t, srv: srv, st: st}
}

func (a *testAPI) do(method, path, token string, body any) (*http.Response, map[string]any) {
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
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
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

	res, body = a.do("POST", "/v1/auth/login", "", map[string]string{"email": "nick@example.com", "password": "hunter2hunter2"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %v", res.StatusCode, body)
	}
	if res.Header.Get(VersionHeader) != "test" {
		t.Fatalf("version header missing")
	}
	session := body["session"].(map[string]any)
	token := session["token"].(string)

	// The /api prefix reaches the same routes.
	res, body = a.do("GET", "/api/v1/auth/session", token, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("session via /api: %d %v", res.StatusCode, body)
	}
	if body["user"].(map[string]any)["email"] != "nick@example.com" {
		t.Fatalf("wrong user: %v", body["user"])
	}

	res, body = a.do("GET", "/v1/auth/sessions", token, nil)
	if res.StatusCode != http.StatusOK || len(body["sessions"].([]any)) != 1 {
		t.Fatalf("sessions: %d %v", res.StatusCode, body)
	}

	res, body = a.do("GET", "/v1/system/meta", token, nil)
	if res.StatusCode != http.StatusOK || body["version"] != "test" {
		t.Fatalf("meta: %d %v", res.StatusCode, body)
	}

	res, _ = a.do("POST", "/v1/auth/logout", token, nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	res, body = a.do("GET", "/v1/auth/session", token, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout: %d %v", res.StatusCode, body)
	}
}

func TestStrictJSON(t *testing.T) {
	a := newTestAPI(t)
	res, body := a.do("POST", "/v1/auth/login", "", map[string]string{"email": "x@example.com", "password": "p", "extra": "field"})
	if res.StatusCode != http.StatusBadRequest || errorCode(body) != "bad_request" {
		t.Fatalf("unknown field accepted: %d %v", res.StatusCode, body)
	}
}

func TestHealthz(t *testing.T) {
	a := newTestAPI(t)
	res, body := a.do("GET", "/healthz", "", nil)
	if res.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("healthz: %d %v", res.StatusCode, body)
	}
}
