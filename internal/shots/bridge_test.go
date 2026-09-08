package shots

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/docker/fake"
)

func TestBridgeCapabilityAndCancellation(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:9223/devtools/browser/test"}`)
			return
		}
		w.WriteHeader(418)
	}))
	defer backend.Close()
	dk := fake.New()
	dk.DialFn = func(ctx context.Context, _ string, port int) (net.Conn, error) {
		if port != 9223 {
			t.Errorf("unexpected port %d", port)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(backend.URL, "http://"))
	}
	s := NewDocker(t.TempDir(), dk, "browser", time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint, closeBridge, err := s.bridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridge()
	u, _ := url.Parse(endpoint)
	for _, path := range []string{"/json/version", "/devtools/browser/test", u.Path + "/extra"} {
		req, _ := http.NewRequest("GET", "http://"+u.Host+path, nil)
		req.Header.Set("Upgrade", "websocket")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("unprotected path %s status %d", path, res.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", "http://"+u.Host+u.Path, nil)
	req.Header.Set("Upgrade", "websocket")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 418 {
		t.Fatalf("capability not forwarded: %d", res.StatusCode)
	}
	closeBridge()
	if res, err := http.Get("http://" + u.Host + u.Path); err == nil {
		res.Body.Close()
		t.Fatal("bridge remained reachable after capture")
	}
}
