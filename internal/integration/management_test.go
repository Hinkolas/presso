//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/control"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/shots"
	"github.com/Hinkolas/presso/internal/testimages"
	"github.com/google/uuid"
)

// Every Docker object is private to this test. Never use platform production
// names or remove shared resources. These tests require a Docker daemon.
func TestManagementIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dk, err := docker.New("")
	if err != nil {
		t.Fatal(err)
	}
	defer dk.Close()
	name := "presso-test-" + uuid.NewString()
	network := name + "-net"
	if _, err = dk.EnsureNetwork(ctx, network, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		out, err := exec.Command("docker", "network", "rm", network).CombinedOutput()
		if err != nil {
			t.Errorf("cleanup network: %s %v", out, err)
		}
	})
	dir := t.TempDir()
	trust, err := control.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A real host listener exercises host.docker.internal on Linux and Desktop.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !control.IsEdge(r.TLS) {
			http.Error(w, "certificate required", 403)
			return
		}
		if r.Header.Get("X-Forwarded-For") == "spoof" {
			http.Error(w, "spoof survived", 500)
			return
		}
		fmt.Fprint(w, "trusted edge")
	})}
	go server.Serve(tls.NewListener(listener, trust.ServerTLS()))
	defer server.Close()
	image := testimages.Edge()
	create := func(spec docker.Spec) string {
		t.Helper()
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
		return cid
	}
	edgeID := create(docker.Spec{Name: name + "-edge", Image: image, Network: network, ExtraHosts: []string{"host.docker.internal:host-gateway"}, Mounts: []docker.Mount{{Type: docker.MountBind, Source: filepath.Join(dir, "control", "edge"), Target: "/etc/presso/control", ReadOnly: true}}})
	cli := edge.NewDockerClient(dk, edgeID, dir)
	route := edge.SiteRoute{ID: "test", Slug: "test", Hosts: []string{"test.localhost", "alias.localhost"}, TLSMode: "off", Upstream: "127.0.0.1:9999", PageCache: true, Unavailable: true}
	render, err := edge.Render([]edge.SiteRoute{route}, edge.Options{PanelHost: "panel.localhost", PanelUpstream: fmt.Sprintf("host.docker.internal:%d", listener.Addr().(*net.TCPAddr).Port)}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var loadErr error
	for i := 0; i < 30; i++ {
		loadErr = cli.Load(ctx, render)
		if loadErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if loadErr != nil {
		out, _ := exec.Command("docker", "logs", edgeID).CombinedOutput()
		t.Fatalf("load: %v\n%s\n%s", loadErr, out, render)
	}
	if ok, err := cli.Matches(ctx, render); err != nil || !ok {
		t.Fatalf("generation: %t %v", ok, err)
	}
	if ok, err := cli.Matches(ctx, render+"\n# different generation"); err != nil || ok {
		t.Fatalf("stale generation accepted: %t %v", ok, err)
	}
	if err := cli.PurgeHosts(ctx, route.Hosts); err != nil {
		t.Fatal(err)
	}
	probe := create(docker.Spec{Name: name + "-probe", Image: image, Network: network, Entrypoint: []string{"/bin/sh", "-c", "sleep 300"}, ExtraHosts: []string{"host.docker.internal:host-gateway"}})
	run := func(cmd ...string) docker.ExecResult {
		t.Helper()
		res, err := dk.Exec(ctx, probe, docker.ExecOptions{Cmd: cmd})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, host := range []string{name + "-edge", "host.docker.internal"} {
		for _, port := range []int{2019, 2020} {
			if host == "host.docker.internal" {
				existing, err := exec.Command("docker", "ps", "--filter", fmt.Sprintf("publish=%d", port), "-q").Output()
				if err != nil {
					t.Fatal(err)
				}
				if len(strings.TrimSpace(string(existing))) > 0 {
					t.Logf("host port %d belongs to an existing installation; checking test container publications instead", port)
					ports, err := exec.Command("docker", "port", edgeID).Output()
					if err != nil || len(ports) != 0 {
						t.Fatalf("test management ports published: %s %v", ports, err)
					}
					continue
				}
			}

			result := run("socat", "-T1", "-", fmt.Sprintf("TCP:%s:%d,connect-timeout=1", host, port))
			if result.ExitCode == 0 {
				t.Fatalf("management port accessible: %s:%d", host, port)
			}
		}
	}
	for _, host := range route.Hosts {
		result, err := dk.Exec(ctx, probe, docker.ExecOptions{Cmd: []string{"socat", "-T2", "-", "TCP:" + name + "-edge:80"}, Stdin: strings.NewReader("GET / HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n")})
		if err != nil {
			t.Fatal(err)
		}
		text := string(result.Stdout) + string(result.Stderr)
		if !strings.Contains(text, "503") || !strings.Contains(strings.ToLower(text), "cache-control: no-store") {
			t.Fatalf("unavailable %s: %s", host, text)
		}
	}
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", network, "curlimages/curl:latest", "-ksS", "--max-time", "15", "--connect-to", "panel.localhost:443:"+name+"-edge:443", "-H", "X-Forwarded-For: spoof", "https://panel.localhost/").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "trusted edge") {
		logs, _ := exec.Command("docker", "logs", edgeID).CombinedOutput()
		t.Fatalf("panel TLS: %s %v\n%s", out, err, logs)
	}

	// Restart must serve the safe bootstrap until the engine pushes routes.
	if err := dk.RestartContainer(ctx, edgeID, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	if ok, err := cli.Matches(ctx, render); err == nil && ok {
		t.Fatal("restart resumed persisted routes")
	}
	if err := cli.Load(ctx, render); err != nil {
		t.Fatal(err)
	}
	browser := create(docker.Spec{Name: name + "-browser", Image: testimages.Browser(), Network: network, Cmd: []string{"--no-sandbox", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=9223", "--disable-dev-shm-usage"}})
	for _, port := range []int{9222, 9223} {
		result := run("socat", "-T1", "-", fmt.Sprintf("TCP:%s:%d,connect-timeout=1", name+"-browser", port))
		if result.ExitCode == 0 {
			t.Fatalf("DevTools exposed on %d", port)
		}
	}
	shot := shots.NewDocker(dir, dk, browser, 30*time.Second)
	var jpeg []byte
	for i := 0; i < 5; i++ {
		jpeg, err = shot.Capture(ctx, "data:text/html,<body>isolated preview</body>")
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil || len(jpeg) < 100 {
		t.Fatalf("capture: %d bytes %v", len(jpeg), err)
	}
	// A cancelled exec stream closes promptly, including a live protocol socket.
	tunnelCtx, stop := context.WithCancel(ctx)
	conn, err := dk.DialLoopback(tunnelCtx, edgeID, 2019)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stop()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = io.ReadAll(conn)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("cancelled tunnel remained open")
	}
}
