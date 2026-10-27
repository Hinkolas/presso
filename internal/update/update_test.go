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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker/fake"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
	"github.com/Hinkolas/presso/internal/version"
)

func setVersion(t *testing.T, v string) {
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

// fakeSites is the rollout's view of internal/site.
type fakeSites struct {
	st        *store.Store
	runner    *jobs.Runner
	list      []site.Site
	recreated []uuid.UUID
	cat       *compat.Catalog
}

func (f *fakeSites) List(context.Context) ([]site.Site, error) { return f.list, nil }
func (f *fakeSites) ApplyRuntime(ctx context.Context, id uuid.UUID, res compat.Resolution, _ string) (*store.Job, error) {
	row, err := f.st.GetSiteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Image == res.Image {
		return nil, nil
	}
	err = f.st.SetSiteImage(ctx, store.SetSiteImageParams{Image: res.Image, Runtime: res.Runtime, PhpVersion: res.PHP, ID: id, UpdatedAt: time.Now()})
	if err != nil {
		return nil, err
	}
	if row.Status == site.StatusStopped {
		return nil, nil
	}
	f.recreated = append(f.recreated, id)
	j, err := f.runner.Enqueue(ctx, site.JobRecreate, uuid.NullUUID{UUID: id, Valid: true}, nil, "test")
	if err != nil {
		return nil, err
	}
	err = f.st.FinishJob(ctx, store.FinishJobParams{ID: j.ID, Status: jobs.StatusSucceeded, UpdatedAt: time.Now()})
	return &j, err
}
func (f *fakeSites) Catalog() *compat.Catalog { return f.cat }

func TestNewer(t *testing.T) {
	cases := []struct {
		tag, cur string
		want     bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v1.0.0", "v0.9.9", true},
		{"v0.2.0", "v0.0.0-dev", false},        // dev builds never update
		{"v0.2.0", "v0.1.0-3-gabc1234", false}, // between-tag builds neither
		{"v0.2.0-rc.1", "v0.1.0", true},
		{"v0.2.0", "v0.2.0-rc.1", true},
	}
	for _, c := range cases {
		if got := newer(c.tag, c.cur); got != c.want {
			t.Errorf("newer(%q,%q) = %v, want %v", c.tag, c.cur, got, c.want)
		}
	}
}

// feed serves a GitHub-shaped release list plus the assets of one release
// (tag). extra adds more entries to the list, newest first as GitHub orders
// them; status and header replace the answer to simulate feed errors.
type feed struct {
	srv     *httptest.Server
	tag     string
	archive []byte
	sums    string
	draft   bool
	extra   []map[string]any
	status  int
	header  map[string]string
	hits    int
}

func newFeed(t *testing.T, tag string, binary []byte) *feed {
	t.Helper()
	f := &feed{tag: tag}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "pressod", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(binary)
	_ = tw.Close()
	_ = gz.Close()
	f.archive = buf.Bytes()
	archiveName, sumsName := assetNames(tag)
	sum := sha256.Sum256(f.archive)
	f.sums = hex.EncodeToString(sum[:]) + "  " + archiveName + "\n" + strings.Repeat("0", 64) + "  other.tar.gz\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/presso/releases", func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		if r.URL.Query().Get("per_page") == "" {
			http.Error(w, "per_page missing", http.StatusBadRequest)
			return
		}
		for k, v := range f.header {
			w.Header().Set(k, v)
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"message":"simulated"}`))
			return
		}
		base := f.srv.URL + "/dl/"
		list := append([]map[string]any{}, f.extra...)
		list = append(list, map[string]any{
			"tag_name": f.tag, "html_url": "https://example.com/" + f.tag, "published_at": "2026-09-08T10:00:00Z",
			"draft": f.draft, "prerelease": version.IsPrerelease(f.tag),
			"assets": []map[string]string{
				{"name": archiveName, "browser_download_url": base + archiveName},
				{"name": sumsName, "browser_download_url": base + sumsName},
			},
		})
		_ = json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("/dl/"+archiveName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(f.archive) })
	mux.HandleFunc("/dl/"+sumsName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(f.sums)) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// ghRelease is one extra entry of the fake list.
func ghRelease(tag string, draft, prerelease bool) map[string]any {
	return map[string]any{"tag_name": tag, "html_url": "https://example.com/" + tag, "draft": draft, "prerelease": prerelease, "published_at": "2026-09-09T10:00:00Z"}
}

func testConfig(t *testing.T, feedURL string) *config.API {
	return &config.API{
		Base:    config.Base{DataDir: t.TempDir()},
		Runtime: config.Runtime{ImageTemplate: "reg/presso-runtime:{release}-php{php}-wp{wp}", LegacyEnabled: true},
		Edge:    config.Edge{RawImage: "reg/presso-edge:{release}"},
		Update:  config.Update{Check: true, Interval: time.Hour, Repo: "acme/presso", APIBase: feedURL},
	}
}

func TestCheckAndStatus(t *testing.T) {
	setVersion(t, "v0.1.0")
	f := newFeed(t, "v0.2.0", []byte("bin"))
	st := testdb.New(t)
	hub := events.New()
	sub, cancel := hub.Subscribe(8)
	defer cancel()
	svc := New(Deps{Config: testConfig(t, f.srv.URL), Store: st, Hub: hub, Jobs: jobs.New(st, hub, 1)})
	ctx := context.Background()

	status, err := svc.Check(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.Latest != "v0.2.0" || status.Current != "v0.1.0" || status.URL == "" || status.PublishedAt == nil {
		t.Fatalf("status: %+v", status)
	}
	select {
	case ev := <-sub:
		if ev.Type != events.TypeSystemUpdate {
			t.Fatalf("event %s", ev.Type)
		}
	default:
		t.Fatal("no system.update event published")
	}
	// Rate limit: a second non-forced check does not hit the feed.
	if _, _ = svc.Check(ctx, false); f.hits != 1 {
		t.Fatalf("rate limit: %d hits", f.hits)
	}
	if _, _ = svc.Check(ctx, true); f.hits != 2 {
		t.Fatalf("forced check: %d hits", f.hits)
	}
	// Persisted: a fresh service loads the result without a network call.
	svc2 := New(Deps{Config: testConfig(t, f.srv.URL), Store: st, Jobs: jobs.New(st, hub, 1)})
	svc2.load(ctx)
	if s2 := svc2.Status(ctx); s2.Latest != "v0.2.0" || s2.CheckedAt == nil {
		t.Fatalf("loaded status: %+v", s2)
	}
	// Drafts / nothing published → ErrNoRelease, previous result kept.
	f.draft = true
	if _, err := svc.Check(ctx, true); err != ErrNoRelease {
		t.Fatalf("draft: %v", err)
	}
	if s := svc.Status(ctx); s.Latest != "v0.2.0" || s.Error == "" {
		t.Fatalf("after draft: %+v", s)
	}
}

func TestStatusDevBuildUnsupported(t *testing.T) {
	setVersion(t, "v0.0.0-dev")
	st := testdb.New(t)
	svc := New(Deps{Config: testConfig(t, "http://127.0.0.1:1"), Store: st, Jobs: jobs.New(st, nil, 1)})
	s := svc.Status(context.Background())
	if s.Supported || s.ReleaseBuild || !strings.Contains(s.Reason, "development build") {
		t.Fatalf("status: %+v", s)
	}
	if _, err := svc.StartUpdate(context.Background(), "u"); err == nil || !strings.Contains(err.Error(), ErrUnavailable.Error()) {
		t.Fatalf("StartUpdate on a dev build: %v", err)
	}
}

func TestDownloadVerifyInstall(t *testing.T) {
	setVersion(t, "v0.1.0")
	// A "binary" that is a shell script printing its version stands in for
	// the real pressod (verifyVersion runs `<file> version`).
	script := []byte("#!/bin/sh\necho v0.2.0\n")
	f := newFeed(t, "v0.2.0", script)
	st := testdb.New(t)
	svc := New(Deps{Config: testConfig(t, f.srv.URL), Store: st, Jobs: jobs.New(st, nil, 1)})
	ctx := context.Background()
	if _, err := svc.Check(ctx, true); err != nil {
		t.Fatal(err)
	}
	archiveName, sumsName := assetNames("v0.2.0")
	dir := t.TempDir()
	staged := filepath.Join(dir, "pressod")
	rel := svc.latest
	if err := svc.download(ctx, rel.Assets[archiveName], rel.Assets[sumsName], archiveName, staged); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(staged); !bytes.Equal(b, script) {
		t.Fatalf("staged content: %q", b)
	}
	if err := verifyVersion(ctx, staged, "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	if err := verifyVersion(ctx, staged, "v0.3.0"); err == nil {
		t.Fatal("version mismatch accepted")
	}
	// Tampered archive → checksum mismatch.
	f.archive = append(f.archive, 0)
	if err := svc.download(ctx, rel.Assets[archiveName], rel.Assets[sumsName], archiveName, staged); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered archive: %v", err)
	}

	// install: atomic replace with a .previous copy, in the binary's own dir.
	exeDir := t.TempDir()
	exe := filepath.Join(exeDir, "pressod")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := install(staged, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, script) {
		t.Fatal("binary not replaced")
	}
	if b, _ := os.ReadFile(exe + ".previous"); string(b) != "old" {
		t.Fatal("previous binary not kept")
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal("staging file left behind")
	}
	if fi, _ := os.Stat(exe); fi.Mode()&0o111 == 0 {
		t.Fatal("binary not executable")
	}
}

func TestRolloutMovesSites(t *testing.T) {
	setVersion(t, "v0.2.0")
	st := testdb.New(t)
	hub := events.New()
	runner := jobs.New(st, hub, 1)
	cat, err := compat.New(compat.Default, compat.Options{ImageTemplate: "reg/presso-runtime:{release}-php{php}-wp{wp}", Release: "v0.2.0", LegacyEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	newest := compat.Default.Latest()
	res, err := cat.Resolve(newest.Branch, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mk := func(slug, status, image, wp, php string) store.Site {
		row, err := st.CreateSite(ctx, store.CreateSiteParams{
			ID: uuid.New(), Slug: slug, Name: slug, Status: status, PrimaryDomain: slug + ".localhost", TlsMode: "off",
			PhpVersion: php, Image: image, Profile: "prod", FileMods: "locked", PageCache: true, VolumeName: "v-" + slug,
			DbName: "d", DbUser: "u", DbPasswordEnc: []byte("x"), RedisUser: "r", RedisPasswordEnc: []byte("x"), SaltsEnc: []byte("x"),
			LoginSecretEnc: []byte("x"), PurgeTokenHash: []byte("x"), AdminUser: "a", AdminEmail: "a@b.c", Settings: "{}",
			CreatedAt: now, UpdatedAt: now, Runtime: "frankenphp", WpVersion: wp,
		})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	running := mk("running", site.StatusRunning, "reg/presso-runtime:v0.1.0-php"+res.PHP+"-wp"+res.WPVersion, res.WPVersion, res.PHP)
	stopped := mk("stopped", site.StatusStopped, "reg/presso-runtime:v0.1.0-php"+res.PHP+"-wp"+res.WPVersion, res.WPVersion, res.PHP)
	current := mk("current", site.StatusRunning, res.Image, res.WPVersion, res.PHP)
	undetected := mk("fresh", site.StatusRunning, "reg/presso-runtime:v0.1.0-x", "", res.PHP)
	view := func(r store.Site) site.Site {
		return site.Site{ID: r.ID, Slug: r.Slug, Status: r.Status, DesiredState: r.Status, Image: r.Image, WPVersion: r.WpVersion, PHPVersion: r.PhpVersion}
	}
	fs := &fakeSites{st: st, runner: runner, cat: cat, list: []site.Site{view(running), view(stopped), view(current), view(undetected)}}
	svc := New(Deps{Config: testConfig(t, "http://127.0.0.1:1"), Store: st, Hub: hub, Jobs: runner, Sites: fs})

	// OnBoot: first boot records the version without a rollout; a changed
	// version enqueues one.
	if prev, rolled := svc.OnBoot(ctx); prev != "" || rolled {
		t.Fatalf("first boot: prev=%q rolled=%v", prev, rolled)
	}
	_ = st.SetKV(ctx, store.SetKVParams{Key: kvEngineVersion, Value: []byte("v0.1.0"), UpdatedAt: now})
	if prev, rolled := svc.OnBoot(ctx); prev != "v0.1.0" || !rolled {
		t.Fatalf("upgrade boot: prev=%q rolled=%v", prev, rolled)
	}
	job := svc.activeJob(ctx)
	if job == nil || job.Kind != JobRollout {
		t.Fatalf("no rollout queued: %+v", job)
	}
	if _, err := svc.StartRollout(ctx, "u"); err != ErrBusy {
		t.Fatalf("second rollout: %v", err)
	}

	// Run the handler directly (a started runner would first fail the
	// queued row as "left over from a previous process").
	if err := svc.runRollout(ctx, *job, jobs.NewReporter(runner, job.ID)); err == nil {
		t.Fatal("undetected site must fail rollout")
	}
	if len(fs.recreated) != 1 || fs.recreated[0] != running.ID {
		t.Fatalf("recreated: %v (want only %s)", fs.recreated, running.ID)
	}
	for _, id := range []uuid.UUID{running.ID, stopped.ID} {
		row, _ := st.GetSiteByID(ctx, id)
		if row.Image != res.Image {
			t.Fatalf("%s image %s, want %s", row.Slug, row.Image, res.Image)
		}
	}
	row, _ := st.GetSiteByID(ctx, undetected.ID)
	if row.Image != "reg/presso-runtime:v0.1.0-x" {
		t.Fatal("undetected site must not be touched")
	}
	evs, _ := st.ListJobEvents(ctx, job.ID)
	var text []string
	for _, e := range evs {
		text = append(text, e.Level+": "+e.Message)
	}
	joined := strings.Join(text, "\n")
	if !strings.Contains(joined, "1 deferred until Start") || !strings.Contains(joined, "warn: fresh:") {
		t.Fatalf("events:\n%s", joined)
	}
	_ = current
}

func TestPrepullAndDrain(t *testing.T) {
	setVersion(t, "v0.1.0")
	st := testdb.New(t)
	dk := fake.New()
	runner := jobs.New(st, nil, 1)
	cat, _ := compat.New(compat.Default, compat.Options{ImageTemplate: "reg/presso-runtime:{release}-php{php}-wp{wp}", Release: "v0.1.0", LegacyEnabled: true})
	newest := compat.Default.Latest()
	res, _ := cat.Resolve(newest.Branch, "")
	fs := &fakeSites{st: st, runner: runner, cat: cat, list: []site.Site{
		{ID: uuid.New(), Slug: "a", Status: site.StatusRunning, WPVersion: res.WPVersion, PHPVersion: res.PHP, Image: res.Image},
		{ID: uuid.New(), Slug: "b", Status: site.StatusStopped, WPVersion: res.WPVersion, PHPVersion: res.PHP, Image: res.Image},
	}}
	svc := New(Deps{Config: testConfig(t, "http://127.0.0.1:1"), Store: st, Docker: dk, Jobs: runner, Sites: fs})
	ctx := context.Background()
	job, _ := runner.Enqueue(ctx, JobUpdate, uuid.NullUUID{}, nil, "t")
	rep := reporterFor(t, runner, job.ID)
	svc.prepull(ctx, "v0.2.0", rep)
	want := fmt.Sprintf("reg/presso-runtime:v0.2.0-php%s-wp%s", res.PHP, res.WPVersion)
	if !dk.Images[want] || !dk.Images["reg/presso-edge:v0.2.0"] {
		t.Fatalf("pre-pulled: %v", dk.Images)
	}
	if len(dk.Images) != 2 {
		t.Fatalf("expected one runtime image for two sites on the same pair: %v", dk.Images)
	}

	// drain ignores the update job itself and waits for others.
	other, _ := runner.Enqueue(ctx, JobUpdate, uuid.NullUUID{}, nil, "t")
	// Every clock read jumps a full drain timeout ahead, so the deadline is
	// passed on the first iteration instead of after ten real minutes.
	base, calls := time.Now().UTC(), 0
	svc.now = func() time.Time { calls++; return base.Add(time.Duration(calls) * drainTimeout) }
	if err := svc.drain(ctx, job.ID, rep); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("drain with another job: %v", err)
	}
	_ = st.FinishJob(ctx, store.FinishJobParams{Status: jobs.StatusFailed, Error: "x", UpdatedAt: time.Now(), ID: other.ID})
	if err := svc.drain(ctx, job.ID, rep); err != nil {
		t.Fatalf("drain alone: %v", err)
	}
	if runtime.GOOS == "linux" {
		t.Log("supported() would also probe the executable directory here")
	}
}

// reporterFor builds a Reporter bound to a job without running it.
func reporterFor(t *testing.T, r *jobs.Runner, id uuid.UUID) *jobs.Reporter {
	t.Helper()
	return jobs.NewReporter(r, id)
}

type rolloutStub struct {
	list  []site.Site
	cat   *compat.Catalog
	apply func(context.Context, uuid.UUID, compat.Resolution, string) (*store.Job, error)
}

func (f rolloutStub) List(context.Context) ([]site.Site, error) { return f.list, nil }
func (f rolloutStub) Catalog() *compat.Catalog                  { return f.cat }
func (f rolloutStub) ApplyRuntime(ctx context.Context, id uuid.UUID, r compat.Resolution, by string) (*store.Job, error) {
	return f.apply(ctx, id, r, by)
}

func TestRolloutWaitsForFailuresAndReportsQueueRejection(t *testing.T) {
	for _, mode := range []string{"child failure", "queue rejection", "success"} {
		t.Run(mode, func(t *testing.T) {
			st := testdb.New(t)
			runner := jobs.New(st, nil, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runner.Register("test.child", func(context.Context, store.Job, *jobs.Reporter) error {
				time.Sleep(20 * time.Millisecond)
				if mode == "child failure" {
					return errors.New("injected database failure")
				}
				return nil
			})
			cat, err := compat.New(compat.Default, compat.Options{ImageTemplate: "runtime:php{php}-wp{wp}"})
			if err != nil {
				t.Fatal(err)
			}
			res, _ := cat.Resolve("", "")
			var child *store.Job
			stub := rolloutStub{cat: cat, list: []site.Site{{ID: uuid.New(), Slug: "test", Status: site.StatusRunning, DesiredState: site.StatusRunning, WPVersion: res.WPVersion, PHPVersion: res.PHP}}, apply: func(ctx context.Context, _ uuid.UUID, _ compat.Resolution, _ string) (*store.Job, error) {
				if mode == "queue rejection" {
					return nil, jobs.ErrQueueFull
				}
				j, err := runner.Enqueue(ctx, "test.child", uuid.NullUUID{}, nil, "test")
				child = &j
				return child, err
			}}
			svc := New(Deps{Config: testConfig(t, "http://127.0.0.1:1"), Store: st, Jobs: runner, Sites: stub})
			if err := runner.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); runner.Wait() }()
			parent, err := st.CreateJob(ctx, store.CreateJobParams{ID: uuid.New(), Kind: JobRollout, Status: jobs.StatusRunning, Args: "{}", CreatedAt: time.Now(), UpdatedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			err = svc.runRollout(ctx, parent, jobs.NewReporter(runner, parent.ID))
			if mode == "success" && err != nil {
				t.Fatal(err)
			}
			if mode != "success" && err == nil {
				t.Fatal("rollout reported success despite failure")
			}
			if child != nil {
				state, _ := st.GetJob(ctx, child.ID)
				if state.Status == jobs.StatusQueued || state.Status == jobs.StatusRunning {
					t.Fatal("rollout returned before child completed")
				}
				events, _ := st.ListJobEvents(ctx, parent.ID)
				found := false
				for _, e := range events {
					if strings.Contains(e.Message, child.ID.String()) {
						found = true
					}
				}
				if !found {
					t.Fatal("child ID not recorded")
				}
			}
		})
	}
}

// writableExe is a stand-in executable in a writable directory, so
// supported() passes.
func writableExe(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "pressod")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestChannels(t *testing.T) {
	setVersion(t, "v0.1.0")
	f := newFeed(t, "v0.2.0", []byte("bin"))
	// GitHub lists newest first: an unpublished draft, the release candidate
	// with its separate source release, then the stable release.
	f.extra = []map[string]any{
		ghRelease("v0.4.0", true, false),
		ghRelease("sources-v0.3.0-rc.1", false, true),
		ghRelease("v0.3.0-rc.1", false, true),
		ghRelease("v0.2.1-beta.1", false, false), // suffix wins over a missing flag
		ghRelease("not-a-release", false, false),
	}
	st := testdb.New(t)
	hub := events.New()
	svc := New(Deps{Config: testConfig(t, f.srv.URL), Store: st, Hub: hub, Jobs: jobs.New(st, hub, 1), Executable: writableExe(t)})
	ctx := context.Background()

	status, err := svc.Check(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if status.Channel != ChannelStable || status.Latest != "v0.2.0" || status.Prerelease || !status.Available || status.AutoUpdate {
		t.Fatalf("stable: %+v", status)
	}

	beta := ChannelBeta
	status, err = svc.UpdateSettings(ctx, SettingsPatch{Channel: &beta})
	if err != nil {
		t.Fatal(err)
	}
	if status.Channel != ChannelBeta || status.Latest != "v0.3.0-rc.1" || !status.Prerelease || !status.Available {
		t.Fatalf("beta: %+v", status)
	}
	// Persisted with the channel: a fresh service reads beta and the beta
	// result, and ignores nothing else.
	svc2 := New(Deps{Config: testConfig(t, f.srv.URL), Store: st, Jobs: jobs.New(st, hub, 1)})
	svc2.load(ctx)
	if s2 := svc2.Status(ctx); s2.Channel != ChannelBeta || s2.Latest != "v0.3.0-rc.1" {
		t.Fatalf("reloaded: %+v", s2)
	}

	// Back to stable while running the release candidate: nothing to offer,
	// presso never downgrades, and the status says it is ahead.
	setVersion(t, "v0.3.0-rc.1")
	stable := ChannelStable
	status, err = svc.UpdateSettings(ctx, SettingsPatch{Channel: &stable})
	if err != nil {
		t.Fatal(err)
	}
	if status.Latest != "v0.2.0" || status.Available || !status.Ahead {
		t.Fatalf("stable after beta: %+v", status)
	}
	if _, err := svc.StartUpdate(ctx, "u"); !errors.Is(err, ErrCurrent) {
		t.Fatalf("downgrade offered: %v", err)
	}
	// A stored stable result is not loaded while the channel is beta.
	_, _ = svc.UpdateSettings(ctx, SettingsPatch{Channel: &beta})
	_ = st.SetKV(ctx, store.SetKVParams{Key: kvLatest, Value: []byte(`{"channel":"stable","latest":{"tag":"v9.9.9"}}`), UpdatedAt: time.Now()})
	svc3 := New(Deps{Config: testConfig(t, f.srv.URL), Store: st, Jobs: jobs.New(st, hub, 1)})
	svc3.load(ctx)
	if s3 := svc3.Status(ctx); s3.Latest != "" {
		t.Fatalf("result of another channel loaded: %+v", s3)
	}

	bogus := "nightly"
	if _, err := svc.UpdateSettings(ctx, SettingsPatch{Channel: &bogus}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("unknown channel: %v", err)
	}
}

func TestDefaultChannelFollowsBuild(t *testing.T) {
	for v, want := range map[string]string{"v0.2.0": ChannelStable, "v0.3.0-beta.2": ChannelBeta, "v0.0.0-dev": ChannelStable} {
		setVersion(t, v)
		st := testdb.New(t)
		svc := New(Deps{Config: testConfig(t, "http://127.0.0.1:1"), Store: st, Jobs: jobs.New(st, nil, 1)})
		if got := svc.Status(context.Background()).Channel; got != want {
			t.Errorf("%s: channel %s, want %s", v, got, want)
		}
	}
	// UPDATE_CHANNEL (install.sh --channel) wins over the build, and a saved
	// setting wins over both.
	setVersion(t, "v0.2.0")
	st := testdb.New(t)
	cfg := testConfig(t, "http://127.0.0.1:1")
	cfg.Update.Channel = ChannelBeta
	svc := New(Deps{Config: cfg, Store: st, Jobs: jobs.New(st, nil, 1)})
	if got := svc.Status(context.Background()).Channel; got != ChannelBeta {
		t.Fatalf("UPDATE_CHANNEL ignored: %s", got)
	}
	stable := ChannelStable
	if _, err := svc.UpdateSettings(context.Background(), SettingsPatch{Channel: &stable}); err != nil {
		t.Fatal(err)
	}
	svc = New(Deps{Config: cfg, Store: st, Jobs: jobs.New(st, nil, 1)})
	if got := svc.Status(context.Background()).Channel; got != ChannelStable {
		t.Fatalf("saved setting lost to UPDATE_CHANNEL: %s", got)
	}
}

func TestCheckErrorKinds(t *testing.T) {
	setVersion(t, "v0.1.0")
	cases := []struct {
		name   string
		setup  func(f *feed)
		feedTo string
		kind   string
	}{
		{"not found", func(f *feed) { f.status = http.StatusNotFound }, "", kindNotFound},
		{"rate limited", func(f *feed) {
			f.status, f.header = http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}
		}, "", kindRateLimited},
		{"too many requests", func(f *feed) { f.status = http.StatusTooManyRequests }, "", kindRateLimited},
		{"server error", func(f *feed) { f.status = http.StatusBadGateway }, "", kindUnavailable},
		{"only drafts", func(f *feed) { f.draft = true }, "", kindNoRelease},
		{"offline", nil, "http://127.0.0.1:1", kindOffline},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFeed(t, "v0.2.0", []byte("bin"))
			if c.setup != nil {
				c.setup(f)
			}
			url := f.srv.URL
			if c.feedTo != "" {
				url = c.feedTo
			}
			st := testdb.New(t)
			svc := New(Deps{Config: testConfig(t, url), Store: st, Jobs: jobs.New(st, nil, 1)})
			status, err := svc.Check(context.Background(), true)
			if err == nil || status.ErrorKind != c.kind || status.Error == "" {
				t.Fatalf("err %v, status %+v", err, status)
			}
		})
	}
	// Invalid JSON.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }))
	defer srv.Close()
	st := testdb.New(t)
	svc := New(Deps{Config: testConfig(t, srv.URL), Store: st, Jobs: jobs.New(st, nil, 1)})
	if status, _ := svc.Check(context.Background(), true); status.ErrorKind != kindInvalid {
		t.Fatalf("invalid feed: %+v", status)
	}
}

func TestManualUpdatesWithoutBackgroundChecks(t *testing.T) {
	setVersion(t, "v0.1.0")
	f := newFeed(t, "v0.2.0", []byte("bin"))
	st := testdb.New(t)
	cfg := testConfig(t, f.srv.URL)
	cfg.Update.Check = false
	svc := New(Deps{Config: cfg, Store: st, Jobs: jobs.New(st, nil, 1), Executable: writableExe(t)})
	ctx := context.Background()
	svc.Run(ctx) // returns at once: no background loop
	if f.hits != 0 {
		t.Fatalf("background check ran: %d hits", f.hits)
	}
	status := svc.Status(ctx)
	if !status.Supported || status.Checks {
		t.Fatalf("status: %+v", status)
	}
	if _, err := svc.StartUpdate(ctx, "u"); err != nil {
		t.Fatalf("manual update: %v", err)
	}
}

func TestAutoUpdate(t *testing.T) {
	setVersion(t, "v0.1.0")
	f := newFeed(t, "v0.2.0", []byte("bin"))
	st := testdb.New(t)
	svc := New(Deps{Config: testConfig(t, f.srv.URL), Store: st, Jobs: jobs.New(st, nil, 1), Executable: writableExe(t)})
	ctx := context.Background()
	status, err := svc.Check(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	// Off by default.
	svc.autoUpdate(ctx, status)
	if svc.activeJob(ctx) != nil {
		t.Fatal("automatic update ran while off")
	}
	on := true
	if status, err = svc.UpdateSettings(ctx, SettingsPatch{AutoUpdate: &on}); err != nil || !status.AutoUpdate {
		t.Fatalf("enable: %+v %v", status, err)
	}
	svc.autoUpdate(ctx, status)
	job := svc.activeJob(ctx)
	if job == nil || job.Kind != JobUpdate || job.CreatedBy != createdBySystem || !strings.Contains(job.Args, "v0.2.0") {
		t.Fatalf("automatic update job: %+v", job)
	}
	// Once per release: after that job failed, the same release is not
	// started again on its own.
	_ = st.FinishJob(ctx, store.FinishJobParams{ID: job.ID, Status: jobs.StatusFailed, Error: "x", UpdatedAt: time.Now()})
	svc.autoUpdate(ctx, svc.Status(ctx))
	if svc.activeJob(ctx) != nil {
		t.Fatal("failed automatic update retried for the same release")
	}
	// A newer release is.
	f.tag = "v0.3.0"
	if status, err = svc.Check(ctx, true); err != nil || status.Latest != "v0.3.0" {
		t.Fatalf("recheck: %+v %v", status, err)
	}
	svc.autoUpdate(ctx, status)
	if j := svc.activeJob(ctx); j == nil || !strings.Contains(j.Args, "v0.3.0") {
		t.Fatalf("next release: %+v", j)
	}
}
