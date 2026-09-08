package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/version"
)

const (
	// drainTimeout bounds how long the update waits for site jobs to finish
	// before giving up (the restart would fail them).
	drainTimeout = 10 * time.Minute
	// maxBinaryBytes bounds the extracted binary (the real one is ~40 MB).
	maxBinaryBytes = 512 << 20
)

// assetNames are the release assets goreleaser produces (.goreleaser.yaml).
func assetNames(tag string) (archive, checksums string) {
	return fmt.Sprintf("pressod_%s_%s_%s.tar.gz", tag, runtime.GOOS, runtime.GOARCH), fmt.Sprintf("pressod_%s_checksums.txt", tag)
}

// runUpdate is the engine.update handler. Every step before "install" leaves
// the host untouched; "install" is one atomic rename; "restart" hands over to
// cmd/pressod once the job row is final.
func (s *Service) runUpdate(ctx context.Context, job store.Job, r *jobs.Reporter) (err error) {
	var args struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal([]byte(job.Args), &args)

	r.Step(ctx, "check", "Checking the release")
	exe, err := s.supported()
	if err != nil {
		return err
	}
	if _, err := s.Check(ctx, true); err != nil && !errors.Is(err, ErrNoRelease) {
		r.Warn(ctx, "release check failed, using the last known release: "+err.Error())
	}
	s.mu.Lock()
	rel := s.latest
	s.mu.Unlock()
	if rel == nil {
		return ErrNoRelease
	}
	if args.Version != "" && args.Version != rel.Tag {
		return fmt.Errorf("update: requested %s but the latest published release is %s; start the update again", args.Version, rel.Tag)
	}
	if !newer(rel.Tag, version.Version) {
		return ErrCurrent
	}
	r.Infof(ctx, "Updating %s → %s (%s)", version.Version, rel.Tag, rel.URL)
	archive, sums := assetNames(rel.Tag)
	archiveURL, sumsURL := rel.Assets[archive], rel.Assets[sums]
	if archiveURL == "" || sumsURL == "" {
		return fmt.Errorf("update: release %s has no asset %s or %s", rel.Tag, archive, sums)
	}

	r.Step(ctx, "drain", "Waiting for running site jobs to finish")
	s.jobs.Pause()
	restarting := false
	defer func() {
		if !restarting {
			s.jobs.Resume()
		}
	}()
	if err := s.drain(ctx, job.ID, r); err != nil {
		return err
	}

	r.Step(ctx, "download", "Downloading and verifying the binary")
	dir := filepath.Join(s.cfg.DataDir, "updates", rel.Tag)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	staged := filepath.Join(dir, "pressod")
	if err := s.download(ctx, archiveURL, sumsURL, archive, staged); err != nil {
		return err
	}
	if err := verifyVersion(ctx, staged, rel.Tag); err != nil {
		return err
	}
	r.Info(ctx, "Checksum verified and the binary reports "+rel.Tag)

	r.Step(ctx, "images", "Pre-pulling the release's images")
	s.prepull(ctx, rel.Tag, r)

	r.Step(ctx, "install", "Installing the binary")
	if err := install(staged, exe); err != nil {
		return err
	}
	r.Infof(ctx, "Installed %s; the previous binary is kept at %s.previous", exe, exe)

	r.Step(ctx, "restart", "Restarting into "+rel.Tag)
	r.Info(ctx, "The engine restarts now; sites are moved to the new runtime images by the rollout job that follows.")
	restarting = true
	go s.requestRestart(job.ID)
	return nil
}

// drain waits until no other job is queued or running.
func (s *Service) drain(ctx context.Context, self uuid.UUID, r *jobs.Reporter) error {
	deadline := s.now().Add(drainTimeout)
	lastReport := time.Time{}
	for {
		active, err := s.st.ListActiveJobs(ctx)
		if err != nil {
			return err
		}
		n := 0
		for _, j := range active {
			if j.ID != self {
				n++
			}
		}
		if n == 0 {
			return nil
		}
		if s.now().Sub(lastReport) >= 30*time.Second {
			r.Infof(ctx, "Waiting for %d running job(s)", n)
			lastReport = s.now()
		}
		if s.now().After(deadline) {
			return fmt.Errorf("update: %d site job(s) still running after %s; try again later", n, drainTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// download fetches the checksum list and the archive, verifies the SHA-256
// and extracts the pressod binary to dst (mode 0755).
func (s *Service) download(ctx context.Context, archiveURL, sumsURL, archiveName, dst string) error {
	sums, err := s.get(ctx, sumsURL, 1<<20)
	if err != nil {
		return fmt.Errorf("checksums: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == archiveName {
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return fmt.Errorf("checksums: no entry for %s", archiveName)
	}
	body, err := s.get(ctx, archiveURL, maxBinaryBytes)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("archive: checksum mismatch (want %s, got %s)", want, got)
	}
	return extractBinary(body, dst)
}

func (s *Service) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pressod/"+version.Version)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", url, limit)
	}
	return b, nil
}

// extractBinary writes the archive's "pressod" entry to dst.
func extractBinary(archive []byte, dst string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return errors.New("archive: no pressod binary inside")
		}
		if err != nil {
			return fmt.Errorf("archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != "pressod" {
			continue
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(tr, maxBinaryBytes)); err != nil {
			f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
}

// verifyVersion runs the staged binary and requires it to report tag.
func verifyVersion(ctx context.Context, path, tag string) error {
	vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, path, "version").Output()
	if err != nil {
		return fmt.Errorf("staged binary does not run: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != tag {
		return fmt.Errorf("staged binary reports %q, expected %s", got, tag)
	}
	return nil
}

// install puts staged in place of exe: a copy into the executable's own
// directory (a rename across file systems fails), a copy of the current
// binary to exe.previous for a manual rollback, then one atomic rename. The
// running process keeps its inode and is unaffected until it re-execs.
func install(staged, exe string) error {
	newPath := exe + ".new"
	if err := copyFile(staged, newPath, 0o755); err != nil {
		return fmt.Errorf("stage next to binary: %w", err)
	}
	if err := copyFile(exe, exe+".previous", 0o755); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("keep previous binary: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("replace binary: %w", err)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// prepull fetches the images the new release will want, so the rollout after
// the restart is quick and sites are down for seconds rather than for a pull.
// Best effort: the rollout pulls whatever is missing.
func (s *Service) prepull(ctx context.Context, tag string, r *jobs.Reporter) {
	if s.dk == nil {
		return
	}
	pull := func(ref string) {
		if err := s.dk.EnsureImage(ctx, ref, func(line string) { r.Info(ctx, line) }); err != nil {
			r.Warn(ctx, "could not pre-pull "+ref+": "+err.Error())
		} else {
			r.Info(ctx, "Image ready: "+ref)
		}
	}
	if strings.Contains(s.cfg.Edge.RawImage, "{release}") {
		pull(strings.ReplaceAll(s.cfg.Edge.RawImage, "{release}", tag))
	}
	if !strings.Contains(s.cfg.ImageTemplate, "{release}") || s.sites == nil {
		r.Info(ctx, "Runtime images are not versioned by release on this host; nothing to pre-pull")
		return
	}
	cat, err := compat.New(compat.Default, compat.Options{
		ImageTemplate: s.cfg.ImageTemplate, Release: tag, LegacyEnabled: s.cfg.LegacyEnabled, DefaultWP: s.cfg.DefaultWP, PHPVersions: s.cfg.PHPVersions,
	})
	if err != nil {
		r.Warn(ctx, "cannot resolve runtime images for "+tag+": "+err.Error())
		return
	}
	sites, err := s.sites.List(ctx)
	if err != nil {
		r.Warn(ctx, "cannot list sites: "+err.Error())
		return
	}
	seen := map[string]bool{}
	for _, site := range sites {
		if site.WPVersion == "" {
			continue
		}
		res, err := cat.Resolve(site.WPVersion, site.PHPVersion)
		if err != nil || seen[res.Image] {
			continue
		}
		seen[res.Image] = true
		pull(res.Image)
	}
}

// requestRestart waits until the runner has recorded the job's final status
// (so the UI sees "succeeded" and the next boot does not fail it as
// interrupted), then signals cmd/pressod.
func (s *Service) requestRestart(id uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		if j, err := s.st.GetJob(ctx, id); err == nil && j.Status != jobs.StatusRunning && j.Status != jobs.StatusQueued {
			break
		}
		select {
		case <-ctx.Done():
			// Restart anyway; the row is written with a detached context and
			// will almost certainly land before the process exits.
			goto signal
		case <-time.After(100 * time.Millisecond):
		}
	}
signal:
	select {
	case s.restart <- struct{}{}:
	default:
	}
}
