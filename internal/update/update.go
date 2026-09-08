// Package update keeps presso current. A release is one version string that
// names everything: the pressod binary on GitHub Releases, the edge image and
// every runtime image on ghcr.io all carry the release tag, and the engine
// derives image references from its own version. Updating the engine is
// therefore what updates the runtimes: after the new binary boots it
// computes different image references for every site and recreates them.
//
// Three pieces live here: the checker (polls GitHub for the latest release),
// the engine.update job (downloads and verifies the new binary, swaps it in
// place and asks the process to re-exec itself) and the engine.rollout job
// (moves every site to the images of the running release; enqueued on the
// first boot after a version change).
package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/version"
)

// Job kinds registered with the runner; both run without a site and so are
// serialized under the runner's global lock.
const (
	JobUpdate  = "engine.update"
	JobRollout = "engine.rollout"
)

// platform_kv keys.
const (
	kvEngineVersion = "engine.version"
	kvLatest        = "update.latest"
)

// createdBySystem marks jobs the engine enqueues on its own.
const createdBySystem = "system"

var (
	// ErrUnavailable: this build or host cannot self-update (dev build, not
	// Linux, inside a container, binary not writable, checks disabled).
	ErrUnavailable = errors.New("update: self-update is not available on this host")
	// ErrCurrent: no newer release is known.
	ErrCurrent = errors.New("update: already running the latest release")
	// ErrBusy: an update or rollout job is already queued or running.
	ErrBusy = errors.New("update: an engine job is already in progress")
	// ErrNoRelease: the feed has no published release yet.
	ErrNoRelease = errors.New("update: no release has been published")
)

// Sites is what the rollout needs from internal/site.
type Sites interface {
	List(ctx context.Context) ([]site.Site, error)
	ApplyRuntime(ctx context.Context, id uuid.UUID, res compat.Resolution, by string) (*store.Job, error)
	Catalog() *compat.Catalog
}

// Deps bundles the collaborators.
type Deps struct {
	Config *config.API
	Store  *store.Store
	Docker docker.Client
	Hub    *events.Hub
	Jobs   *jobs.Runner
	Sites  Sites
	// HTTP is used for the release feed and downloads; nil uses a default
	// client with sane timeouts.
	HTTP *http.Client
	// Executable overrides os.Executable (tests).
	Executable string
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Status is the update state the API reports.
type Status struct {
	Current      string `json:"current"`
	ReleaseBuild bool   `json:"release_build"`
	// Supported reports whether this process can update itself; Reason says
	// why not when it cannot.
	Supported bool   `json:"supported"`
	Reason    string `json:"unsupported_reason,omitempty"`
	// Latest is the newest published release known ("" before the first
	// successful check); Available is true when it is newer than Current.
	Latest      string     `json:"latest"`
	Available   bool       `json:"available"`
	URL         string     `json:"url"`
	PublishedAt *time.Time `json:"published_at"`
	CheckedAt   *time.Time `json:"checked_at"`
	Error       string     `json:"error"`
	// JobID is the queued or running engine.update / engine.rollout job.
	JobID   *uuid.UUID `json:"job_id"`
	JobKind string     `json:"job_kind,omitempty"`
}

type Service struct {
	cfg   *config.API
	st    *store.Store
	dk    docker.Client
	hub   *events.Hub
	jobs  *jobs.Runner
	sites Sites
	http  *http.Client
	exe   string
	now   func() time.Time

	mu        sync.Mutex
	latest    *release
	checkedAt time.Time
	checkErr  string
	lastCheck time.Time

	restart chan struct{}
}

func New(d Deps) *Service {
	s := &Service{
		cfg: d.Config, st: d.Store, dk: d.Docker, hub: d.Hub, jobs: d.Jobs, sites: d.Sites,
		http: d.HTTP, exe: d.Executable, now: d.Now, restart: make(chan struct{}, 1),
	}
	if s.http == nil {
		s.http = &http.Client{Timeout: 10 * time.Minute}
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.jobs != nil {
		s.jobs.Register(JobUpdate, s.runUpdate,
			jobs.Step{Name: "check", Label: "Release check"},
			jobs.Step{Name: "drain", Label: "Wait for site jobs"},
			jobs.Step{Name: "download", Label: "Download and verify"},
			jobs.Step{Name: "images", Label: "Pre-pull images"},
			jobs.Step{Name: "install", Label: "Install binary"},
			jobs.Step{Name: "restart", Label: "Restart"},
		)
		s.jobs.Register(JobRollout, s.runRollout,
			jobs.Step{Name: "resolve", Label: "Resolve runtime images"},
			jobs.Step{Name: "recreate", Label: "Recreate sites"},
		)
	}
	return s
}

// RestartRequests fires once after a successful engine.update job has been
// recorded; cmd/pressod shuts down gracefully and re-execs the new binary.
func (s *Service) RestartRequests() <-chan struct{} { return s.restart }

// supported reports whether this process can replace its own binary.
func (s *Service) supported() (string, error) {
	if version.Release() == "" {
		return "", fmt.Errorf("%w: %s is a development build", ErrUnavailable, version.Version)
	}
	if os.Getenv("PRESSO_IN_CONTAINER") != "" {
		return "", fmt.Errorf("%w: pressod runs in a container; pull a newer image instead", ErrUnavailable)
	}
	if s.cfg != nil && !s.cfg.Update.Check {
		return "", fmt.Errorf("%w: UPDATE_CHECK is disabled", ErrUnavailable)
	}
	exe, err := s.executable()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	probe := filepath.Join(filepath.Dir(exe), ".presso-write-check")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("%w: %s is not writable (%v)", ErrUnavailable, filepath.Dir(exe), err)
	}
	f.Close()
	os.Remove(probe)
	return exe, nil
}

func (s *Service) executable() (string, error) {
	exe := s.exe
	if exe == "" {
		p, err := os.Executable()
		if err != nil {
			return "", err
		}
		exe = p
	}
	return filepath.EvalSymlinks(exe)
}

// Status assembles the API view.
func (s *Service) Status(ctx context.Context) Status {
	out := Status{Current: version.Version, ReleaseBuild: version.Release() != ""}
	if _, err := s.supported(); err != nil {
		out.Reason = reasonOf(err)
	} else {
		out.Supported = true
	}
	s.mu.Lock()
	if s.latest != nil {
		out.Latest, out.URL = s.latest.Tag, s.latest.URL
		if !s.latest.PublishedAt.IsZero() {
			t := s.latest.PublishedAt
			out.PublishedAt = &t
		}
		out.Available = newer(s.latest.Tag, version.Version)
	}
	if !s.checkedAt.IsZero() {
		t := s.checkedAt
		out.CheckedAt = &t
	}
	out.Error = s.checkErr
	s.mu.Unlock()
	if job := s.activeJob(ctx); job != nil {
		id := job.ID
		out.JobID, out.JobKind = &id, job.Kind
	}
	return out
}

// reasonOf strips the sentinel prefix ("update: self-update is not available
// on this host: ") from a wrapped ErrUnavailable.
func reasonOf(err error) string {
	msg := err.Error()
	prefix := ErrUnavailable.Error() + ": "
	if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
		return msg[len(prefix):]
	}
	return msg
}

// activeJob returns the queued or running engine job, if any.
func (s *Service) activeJob(ctx context.Context) *store.Job {
	for _, kind := range []string{JobUpdate, JobRollout} {
		if j, err := s.st.GetActiveJobByKind(ctx, kind); err == nil {
			return &j
		}
	}
	return nil
}

// StartUpdate enqueues engine.update towards the latest known release (a
// fresh check runs first when none is known).
func (s *Service) StartUpdate(ctx context.Context, by string) (store.Job, error) {
	if _, err := s.supported(); err != nil {
		return store.Job{}, err
	}
	s.mu.Lock()
	known := s.latest
	s.mu.Unlock()
	if known == nil {
		if _, err := s.Check(ctx, true); err != nil {
			return store.Job{}, err
		}
		s.mu.Lock()
		known = s.latest
		s.mu.Unlock()
	}
	if known == nil {
		return store.Job{}, ErrNoRelease
	}
	if !newer(known.Tag, version.Version) {
		return store.Job{}, ErrCurrent
	}
	if s.activeJob(ctx) != nil {
		return store.Job{}, ErrBusy
	}
	return s.jobs.Enqueue(ctx, JobUpdate, uuid.NullUUID{}, map[string]string{"version": known.Tag}, by)
}

// StartRollout enqueues engine.rollout by hand.
func (s *Service) StartRollout(ctx context.Context, by string) (store.Job, error) {
	if s.activeJob(ctx) != nil {
		return store.Job{}, ErrBusy
	}
	return s.jobs.Enqueue(ctx, JobRollout, uuid.NullUUID{}, nil, by)
}

// OnBoot records the running version and, when it differs from the one that
// ran before, enqueues a rollout so every site moves to this release's
// runtime images. Call after the runner has started. It returns the previous
// version ("" on a first boot) and whether a rollout was enqueued.
func (s *Service) OnBoot(ctx context.Context) (previous string, rollout bool) {
	prev, _ := s.st.GetKV(ctx, kvEngineVersion)
	previous = string(prev)
	if previous == version.Version {
		return previous, false
	}
	if err := s.st.SetKV(ctx, store.SetKVParams{Key: kvEngineVersion, Value: []byte(version.Version), UpdatedAt: s.now()}); err != nil {
		slog.WarnContext(ctx, "update: record engine version", "err", err)
	}
	if previous == "" {
		slog.InfoContext(ctx, "update: first boot", "version", version.Version)
		return previous, false
	}
	slog.InfoContext(ctx, "update: engine version changed, rolling runtime images", "from", previous, "to", version.Version)
	if _, err := s.StartRollout(ctx, createdBySystem); err != nil {
		slog.WarnContext(ctx, "update: enqueue rollout", "err", err)
		return previous, false
	}
	return previous, true
}
