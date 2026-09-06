// Package shots captures front-page previews of sites with a shared headless
// Chrome (the presso-browser platform container) over the DevTools protocol
// and keeps the latest JPEG per site under DATA_DIR/screenshots. The browser
// lives on the presso network and resolves every hostname to the edge, so a
// preview shows exactly what a visitor gets.
//
// Captures are serialized: one headless Chrome with a tight memory limit
// renders one page at a time comfortably and several poorly.
package shots

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/google/uuid"
)

// ErrUnavailable means the browser could not be reached (container down,
// still starting, or previews disabled).
var ErrUnavailable = errors.New("shots: preview browser unavailable")

const (
	viewportWidth  = 1280
	viewportHeight = 800
	jpegQuality    = 80
	// settle gives web fonts and above-the-fold images a moment after load.
	settle = 1500 * time.Millisecond
)

// Service captures and stores previews. A nil *Service is safe to use: every
// method reports ErrUnavailable or a zero value.
type Service struct {
	dir     string
	base    string // DevTools HTTP endpoint, e.g. http://127.0.0.1:9222
	timeout time.Duration
	mu      sync.Mutex
}

// New returns a service storing previews in dir and talking to the DevTools
// endpoint published at listen (host:port).
func New(dir, listen string, timeout time.Duration) *Service {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		host, port = "127.0.0.1", "9222"
	}
	if host == "0.0.0.0" || host == "" {
		host = "127.0.0.1"
	}
	return &Service{dir: dir, base: "http://" + net.JoinHostPort(host, port), timeout: timeout}
}

// Capture renders url in a fresh tab and returns a JPEG of the viewport.
func (s *Service) Capture(ctx context.Context, url string) ([]byte, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if err := s.reachable(ctx); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, s.base)
	defer cancelAlloc()
	tabCtx, cancelTab := chromedp.NewContext(allocCtx)
	// Cancel closes the target; a plain context cancel would leave the tab
	// open in the shared browser.
	defer func() { _ = chromedp.Cancel(tabCtx) }()
	defer cancelTab()

	var buf []byte
	err := chromedp.Run(tabCtx,
		emulation.SetDeviceMetricsOverride(viewportWidth, viewportHeight, 1, false),
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(settle),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, err = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatJpeg).
				WithQuality(jpegQuality).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("shots: capture %s: %w", url, ctx.Err())
		}
		return nil, fmt.Errorf("shots: capture %s: %w", url, err)
	}
	return buf, nil
}

// reachable fails fast when the DevTools endpoint does not answer, so a
// missing browser reads as ErrUnavailable rather than a websocket timeout.
func (s *Service) reachable(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/json/version", nil)
	if err != nil {
		return err
	}
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("devtools answered %d", res.StatusCode)
	}
	return nil
}

// Refresh captures url and stores it as the preview for id, returning the
// stored file's modification time.
func (s *Service) Refresh(ctx context.Context, id uuid.UUID, url string) (time.Time, error) {
	if s == nil {
		return time.Time{}, ErrUnavailable
	}
	img, err := s.Capture(ctx, url)
	if err != nil {
		return time.Time{}, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return time.Time{}, err
	}
	final := s.Path(id)
	tmp, err := os.CreateTemp(s.dir, id.String()+".*.tmp")
	if err != nil {
		return time.Time{}, err
	}
	if _, err := tmp.Write(img); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return time.Time{}, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return time.Time{}, err
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		os.Remove(tmp.Name())
		return time.Time{}, err
	}
	at, _ := s.Stat(id)
	return at, nil
}

// Path is where the preview for id lives (whether or not it exists).
func (s *Service) Path(id uuid.UUID) string {
	if s == nil {
		return ""
	}
	return filepath.Join(s.dir, id.String()+".jpg")
}

// Stat reports when the preview for id was captured, if one exists.
func (s *Service) Stat(id uuid.UUID) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	fi, err := os.Stat(s.Path(id))
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime().UTC(), true
}

// Remove deletes the preview for id; a missing file is not an error.
func (s *Service) Remove(id uuid.UUID) error {
	if s == nil {
		return nil
	}
	if err := os.Remove(s.Path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
