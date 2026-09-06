package site

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/docker/fake"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
)

// fakePlatform records provisioning calls.
type fakePlatform struct {
	dbs, acls, dropped, deletedACLs []string
	edgeApplies                     int
	render                          platform.EdgeRenderer
}

func (f *fakePlatform) CreateSiteDatabase(_ context.Context, db, user, pw string) error {
	f.dbs = append(f.dbs, db+"/"+user)
	return nil
}
func (f *fakePlatform) DropSiteDatabase(_ context.Context, db, _ string) error {
	f.dropped = append(f.dropped, db)
	return nil
}
func (f *fakePlatform) ApplyRedisACL(_ context.Context, user, _, prefix string) error {
	f.acls = append(f.acls, user+" "+prefix)
	return nil
}
func (f *fakePlatform) DeleteRedisACL(_ context.Context, user string) error {
	f.deletedACLs = append(f.deletedACLs, user)
	return nil
}
func (f *fakePlatform) ApplyEdge(context.Context) error         { f.edgeApplies++; return nil }
func (f *fakePlatform) SetEdgeRenderer(r platform.EdgeRenderer) { f.render = r }

type harness struct {
	svc  *Service
	st   *store.Store
	dk   *fake.Client
	pf   *fakePlatform
	jobs *jobs.Runner
	hub  *events.Hub
}

func newHarness(t *testing.T) (*harness, context.Context) {
	t.Helper()
	st := testdb.New(t)
	dk := fake.New()
	hub := events.New()
	box, _ := secrets.New(strings.Repeat("k", 32))
	cfg := &config.API{
		Platform: config.Platform{Network: "presso", EngineInternalURL: "http://host.docker.internal:7080"},
		Runtime:  config.Runtime{ImageTemplate: "presso-runtime:php{php}", PHPVersions: []string{"8.3", "8.4"}, DefaultPHP: "8.3"},
		Edge:     config.Edge{HTTPListen: "127.0.0.1:1", CacheAdminListen: "127.0.0.1:2020", DefaultCacheTTL: 12 * time.Hour},
	}
	runner := jobs.New(st, hub, 2)
	pf := &fakePlatform{}
	svc := New(Deps{Config: cfg, Store: st, Docker: dk, Secrets: box, Hub: hub, Jobs: runner, Platform: pf})
	// wp core is-installed → not installed (exit 1) on the first call, then
	// every command succeeds.
	installed := false
	dk.ExecFn = func(id string, opts docker.ExecOptions) (docker.ExecResult, error) {
		if len(opts.Cmd) > 3 && opts.Cmd[3] == "core" && opts.Cmd[4] == "is-installed" {
			if installed {
				return docker.ExecResult{ExitCode: 0}, nil
			}
			return docker.ExecResult{ExitCode: 1}, nil
		}
		if len(opts.Cmd) > 3 && opts.Cmd[3] == "core" && opts.Cmd[4] == "install" {
			installed = true
			return docker.ExecResult{Stdout: []byte("Success: WordPress installed successfully.")}, nil
		}
		return docker.ExecResult{Stdout: []byte("ok")}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return &harness{svc: svc, st: st, dk: dk, pf: pf, jobs: runner, hub: hub}, ctx
}

func (h *harness) waitJob(t *testing.T, ctx context.Context, id uuid.UUID) store.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, err := h.st.GetJob(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == jobs.StatusSucceeded || j.Status == jobs.StatusFailed {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return store.Job{}
}

func TestCreateProvisionAndDelete(t *testing.T) {
	h, ctx := newHarness(t)
	site, job, err := h.svc.Create(ctx, CreateInput{
		Name: "Demo Shop", AdminEmail: "a@example.com", AdminPassword: "supersecret1", CreatedBy: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if site.Slug != "demo-shop" || site.PrimaryDomain != "demo-shop.localhost" || site.TLSMode != "off" || site.URL != "http://demo-shop.localhost" {
		t.Fatalf("defaults: %+v", site)
	}
	if site.Status != StatusProvisioning || site.PHPVersion != "8.3" || site.Image != "presso-runtime:php8.3" || site.AdminUser != "admin" {
		t.Fatalf("site: %+v", site)
	}
	if len(site.Domains) != 1 || site.Domains[0].Kind != "primary" {
		t.Fatalf("domains: %+v", site.Domains)
	}

	done := h.waitJob(t, ctx, job.ID)
	if done.Status != jobs.StatusSucceeded {
		evs, _ := h.st.ListJobEvents(ctx, job.ID)
		t.Fatalf("provision failed: %s\n%+v", done.Error, evs)
	}
	row, _ := h.svc.Get(ctx, site.ID)
	if row.Status != StatusRunning {
		t.Fatalf("status: %s %s", row.Status, row.StatusMessage)
	}
	if len(h.pf.dbs) != 1 || !strings.HasPrefix(h.pf.dbs[0], "wp_demo_shop_") || len(h.pf.acls) != 1 || h.pf.edgeApplies != 1 {
		t.Fatalf("platform calls: %+v", h.pf)
	}
	if len(h.dk.Created) != 1 {
		t.Fatalf("containers: %d", len(h.dk.Created))
	}
	spec := h.dk.Created[0]
	if spec.Name != "presso-site-demo-shop" || spec.Labels[docker.LabelSiteID] != site.ID.String() || spec.NetworkAliases[0] != "site-"+site.ID.String() {
		t.Fatalf("spec: %+v", spec)
	}
	env := strings.Join(spec.Env, "\n")
	for _, want := range []string{"PRESSO_SITE_URL=http://demo-shop.localhost", "PRESSO_DB_HOST=presso-mariadb", "PRESSO_REDIS_PREFIX=site-" + site.ID.String() + ":", "PRESSO_CACHE=on", "PHP_MEMORY_LIMIT=256M", "PRESSO_SALT_NONCE_SALT="} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %s", want)
		}
	}
	if spec.MemoryBytes != 512<<20 || spec.NanoCPUs != 1e9 {
		t.Fatalf("limits: %d %d", spec.MemoryBytes, spec.NanoCPUs)
	}
	// Duplicate slug and domain are rejected.
	if _, _, err := h.svc.Create(ctx, CreateInput{Name: "Demo Shop", AdminEmail: "a@example.com", AdminPassword: "supersecret1"}); !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("slug: %v", err)
	}
	if _, _, err := h.svc.Create(ctx, CreateInput{Name: "Other", PrimaryDomain: "demo-shop.localhost", AdminEmail: "a@example.com", AdminPassword: "supersecret1"}); !errors.Is(err, ErrDomainTaken) {
		t.Fatalf("domain: %v", err)
	}

	// Login link carries a signed token for the admin user.
	url, exp, err := h.svc.LoginLink(ctx, site.ID, "test")
	if err != nil || !strings.HasPrefix(url, "http://demo-shop.localhost/wp-login.php?presso_login=") || exp.Before(time.Now()) {
		t.Fatalf("login link: %s %v", url, err)
	}

	// Edge render includes the site.
	caddyfile, err := h.svc.RenderEdge(ctx)
	if err != nil || !strings.Contains(caddyfile, "http://demo-shop.localhost {") {
		t.Fatalf("render: %v\n%s", err, caddyfile)
	}

	// Stop then start.
	j, err := h.svc.Stop(ctx, site.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.waitJob(t, ctx, j.ID)
	row, _ = h.svc.Get(ctx, site.ID)
	if row.Status != StatusStopped {
		t.Fatalf("after stop: %s", row.Status)
	}
	if _, err := h.svc.Stop(ctx, site.ID, "test"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("stop stopped: %v", err)
	}
	j, _ = h.svc.Start(ctx, site.ID, "test")
	h.waitJob(t, ctx, j.ID)
	row, _ = h.svc.Get(ctx, site.ID)
	if row.Status != StatusRunning {
		t.Fatalf("after start: %s %s", row.Status, row.StatusMessage)
	}

	// Update: PHP version change recreates the container.
	php := "8.4"
	updated, rj, err := h.svc.Update(ctx, site.ID, UpdateInput{PHPVersion: &php, UpdatedBy: "test"})
	if err != nil || rj == nil || updated.Image != "presso-runtime:php8.4" {
		t.Fatalf("update: %v %v %+v", err, rj, updated)
	}
	h.waitJob(t, ctx, rj.ID)
	if len(h.dk.Created) != 2 || h.dk.Created[1].Image != "presso-runtime:php8.4" {
		t.Fatalf("recreate: %d", len(h.dk.Created))
	}
	// Name-only update: no job.
	name := "Renamed"
	if _, rj, err := h.svc.Update(ctx, site.ID, UpdateInput{Name: &name}); err != nil || rj != nil {
		t.Fatalf("rename: %v %v", err, rj)
	}
	// Bad setting rejected.
	if _, _, err := h.svc.Update(ctx, site.ID, UpdateInput{Settings: map[string]jsonRaw{"max_input_vars": jsonRaw("5")}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad setting: %v", err)
	}

	// WP-CLI passthrough.
	res, err := h.svc.RunWP(ctx, site.ID, []string{"plugin", "list"}, "test")
	if err != nil || res.Stdout != "ok" {
		t.Fatalf("wp: %v %+v", err, res)
	}
	if _, err := h.svc.RunWP(ctx, site.ID, []string{"--path=/etc"}, "test"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("path arg: %v", err)
	}

	// Delete requires the slug.
	if _, err := h.svc.Delete(ctx, site.ID, "wrong", false, "test"); !errors.Is(err, ErrWrongConfirm) {
		t.Fatalf("confirm: %v", err)
	}
	dj, err := h.svc.Delete(ctx, site.ID, "demo-shop", false, "test")
	if err != nil {
		t.Fatal(err)
	}
	if d := h.waitJob(t, ctx, dj.ID); d.Status != jobs.StatusSucceeded {
		t.Fatalf("delete: %s", d.Error)
	}
	if _, err := h.svc.Get(ctx, site.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("still exists: %v", err)
	}
	if len(h.dk.Containers) != 0 || h.dk.Volumes["presso-site-demo-shop-content"] || len(h.pf.dropped) != 1 || len(h.pf.deletedACLs) != 1 {
		t.Fatalf("teardown incomplete: %+v %+v", h.dk.Volumes, h.pf)
	}
}

func TestProvisionFailureAndRetry(t *testing.T) {
	h, ctx := newHarness(t)
	h.dk.FailCreate = errors.New("boom")
	site, job, err := h.svc.Create(ctx, CreateInput{Name: "x1", AdminEmail: "a@example.com", AdminPassword: "supersecret1"})
	if err != nil {
		t.Fatal(err)
	}
	if d := h.waitJob(t, ctx, job.ID); d.Status != jobs.StatusFailed || !strings.Contains(d.Error, "container: ") {
		t.Fatalf("expected container failure: %+v", d)
	}
	row, _ := h.svc.Get(ctx, site.ID)
	if row.Status != StatusError {
		t.Fatalf("status: %s", row.Status)
	}
	rj, err := h.svc.Retry(ctx, site.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if d := h.waitJob(t, ctx, rj.ID); d.Status != jobs.StatusSucceeded {
		t.Fatalf("retry: %s", d.Error)
	}
	row, _ = h.svc.Get(ctx, site.ID)
	if row.Status != StatusRunning {
		t.Fatalf("after retry: %s %s", row.Status, row.StatusMessage)
	}
}

func TestObserveContainer(t *testing.T) {
	h, ctx := newHarness(t)
	site, job, _ := h.svc.Create(ctx, CreateInput{Name: "obs", AdminEmail: "a@example.com", AdminPassword: "supersecret1"})
	h.waitJob(t, ctx, job.ID)
	info, _ := h.dk.InspectContainer(ctx, "presso-site-obs")
	info.State, info.ExitCode = "exited", 137
	h.svc.ObserveContainer(ctx, site.ID, info)
	row, _ := h.svc.Get(ctx, site.ID)
	if row.Status != StatusError || !strings.Contains(row.StatusMessage, "137") {
		t.Fatalf("crash not observed: %s %s", row.Status, row.StatusMessage)
	}
	info.State, info.ExitCode, info.Health = "running", 0, "healthy"
	h.svc.ObserveContainer(ctx, site.ID, info)
	row, _ = h.svc.Get(ctx, site.ID)
	p, _ := h.svc.Payload(ctx, row)
	if row.Status != StatusRunning || p.Health != "healthy" {
		t.Fatalf("recovery not observed: %s %s", row.Status, p.Health)
	}
	h.svc.ObserveContainer(ctx, site.ID, nil)
	row, _ = h.svc.Get(ctx, site.ID)
	if row.Status != StatusError {
		t.Fatalf("missing container not observed: %s", row.Status)
	}
}

func TestValidation(t *testing.T) {
	cfg := &config.API{Runtime: config.Runtime{PHPVersions: []string{"8.3"}, DefaultPHP: "8.3"}}
	cases := []CreateInput{
		{Name: "", AdminEmail: "a@b.c", AdminPassword: "12345678"},
		{Name: "x", Slug: "-bad", AdminEmail: "a@b.c", AdminPassword: "12345678"},
		{Name: "x", Slug: "ok", PrimaryDomain: "bad host", AdminEmail: "a@b.c", AdminPassword: "12345678"},
		{Name: "x", Slug: "ok", PHPVersion: "7.4", AdminEmail: "a@b.c", AdminPassword: "12345678"},
		{Name: "x", Slug: "ok", AdminEmail: "nope", AdminPassword: "12345678"},
		{Name: "x", Slug: "ok", AdminEmail: "a@b.c", AdminPassword: "short"},
		{Name: "x", Slug: "ok", AdminEmail: "a@b.c", AdminPassword: "12345678", TLSMode: "maybe"},
	}
	for i, c := range cases {
		if err := c.validate(cfg); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("case %d: %v", i, err)
		}
	}
	good := CreateInput{Name: "My Site!", PrimaryDomain: "Example.COM", AdminEmail: "a@b.c", AdminPassword: "12345678"}
	if err := good.validate(cfg); err != nil || good.Slug != "my-site" || good.PrimaryDomain != "example.com" || good.TLSMode != "auto" || good.Title != "My Site!" {
		t.Fatalf("good: %v %+v", err, good)
	}
	if Slugify("  Héllo  World -- 2 ") != "h-llo-world-2" {
		t.Fatalf("slugify: %q", Slugify("  Héllo  World -- 2 "))
	}
}

func TestPurgePrefixEnforcement(t *testing.T) {
	// Without an edge client PurgeCache is a no-op; the filter is exercised
	// through the exported helper semantics instead.
	id := uuid.New()
	prefix := "s" + id.String()
	tags := filterTags(prefix, []string{prefix, prefix + ":p1", "sOTHER:p1", "junk"})
	if len(tags) != 2 {
		t.Fatalf("filter: %v", tags)
	}
}
