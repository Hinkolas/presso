package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// LoopbackTransport cannot be redirected to arbitrary container destinations.
func LoopbackTransport(d Client, container string, port int) *http.Transport {
	return &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return d.DialLoopback(ctx, container, port)
	}, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second}
}

func (m *Moby) DialLoopback(ctx context.Context, container string, port int) (net.Conn, error) {
	if port != 2019 && port != 2020 && port != 9223 {
		return nil, fmt.Errorf("docker: unsupported management port %d", port)
	}
	created, err := m.cli.ExecCreate(ctx, container, client.ExecCreateOptions{
		Cmd:         []string{"socat", "STDIO", fmt.Sprintf("TCP4:127.0.0.1:%d,connect-timeout=5", port)},
		AttachStdin: true, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return nil, wrap(err)
	}
	att, err := m.cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, wrap(err)
	}
	local, relay := net.Pipe()
	var once sync.Once
	done := make(chan struct{})
	closeAll := func() { once.Do(func() { close(done); _ = att.CloseWrite(); att.Close(); _ = relay.Close() }) }
	go func() {
		select {
		case <-ctx.Done():
			closeAll()
		case <-done:
		}
	}()
	go func() { defer closeAll(); _, _ = io.Copy(att.Conn, relay) }()
	go func() {
		defer closeAll()
		// Docker multiplexing must never leak stderr into HTTP/WebSocket bytes.
		_, _ = stdcopy.StdCopy(relay, io.Discard, att.Reader)
	}()
	return local, nil
}
