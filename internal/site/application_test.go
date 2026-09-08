package site

import (
	"context"
	"errors"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/jobs"
)

func provisioned(t *testing.T) (*harness, context.Context, Site) {
	t.Helper()
	h, ctx := newHarness(t)
	st, j, err := h.svc.Create(ctx, CreateInput{Slug: "recovery", Name: "Recovery", PrimaryDomain: "recovery.localhost", AdminUser: "admin", AdminEmail: "admin@example.com", AdminPassword: "a-long-test-password", CreatedBy: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if j = h.waitJob(t, ctx, j.ID); j.Status != jobs.StatusSucceeded {
		t.Fatal(j.Error)
	}
	row, err := h.svc.Get(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	st, err = h.svc.Payload(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	return h, ctx, st
}

func TestStoppedSettingsApplyOnlyOnStart(t *testing.T) {
	h, ctx, st := provisioned(t)
	j, err := h.svc.Stop(ctx, st.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if j = h.waitJob(t, ctx, j.ID); j.Status != jobs.StatusSucceeded {
		t.Fatal(j.Error)
	}
	before := len(h.dk.Created)
	php := "8.4"
	updated, job, err := h.svc.Update(ctx, st.ID, UpdateInput{PHPVersion: &php})
	if err != nil {
		t.Fatal(err)
	}
	if job != nil || len(h.dk.Created) != before || !updated.ConfigurationPending || updated.TrafficReady || updated.DesiredState != StatusStopped {
		t.Fatalf("stopped update: %+v job=%v", updated, job)
	}
	render, err := h.svc.RenderEdge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(render, "reverse_proxy site-") || !strings.Contains(render, "Site unavailable") {
		t.Fatal(render)
	}
	j, err = h.svc.Start(ctx, st.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if j = h.waitJob(t, ctx, j.ID); j.Status != jobs.StatusSucceeded {
		t.Fatal(j.Error)
	}
	row, _ := h.svc.Get(ctx, st.ID)
	if !row.TrafficReady || row.AppliedRevision != row.ConfigRevision || row.AppliedImage != row.Image || len(h.dk.Created) != before+1 {
		t.Fatalf("start: %+v", row)
	}
}

func TestApplicationFailureAndExplicitRetry(t *testing.T) {
	for _, failure := range []string{"edge", "container", "database", "bootstrap"} {
		t.Run(failure, func(t *testing.T) {
			h, ctx, st := provisioned(t)
			originalExec := h.dk.ExecFn
			before := len(h.dk.Created)
			switch failure {
			case "edge":
				h.pf.edgeError = errors.New("injected edge failure")
			case "container":
				h.dk.FailCreate = errors.New("injected container failure")
			default:
				h.dk.ExecFn = func(id string, o docker.ExecOptions) (docker.ExecResult, error) {
					if (failure == "database" && strings.Contains(strings.Join(o.Cmd, " "), "core update-db")) || (failure == "bootstrap" && strings.Contains(strings.Join(o.Cmd, " "), "is_blog_installed")) {
						return docker.ExecResult{ExitCode: 1, Stderr: []byte("injected")}, nil
					}
					return originalExec(id, o)
				}
			}
			j, err := h.svc.Recreate(ctx, st.ID, "test")
			if err != nil {
				t.Fatal(err)
			}
			j = h.waitJob(t, ctx, j.ID)
			row, _ := h.svc.Get(ctx, st.ID)
			if j.Status != jobs.StatusFailed || row.TrafficReady || row.Status != StatusError {
				t.Fatalf("job=%+v row=%+v", j, row)
			}
			if failure == "edge" && len(h.dk.Created) != before {
				t.Fatal("replaced runtime despite unavailable-route failure")
			}
			info, _ := h.dk.InspectContainer(ctx, ContainerName(st.Slug))
			h.svc.ObserveContainer(ctx, st.ID, info)
			row, _ = h.svc.Get(ctx, st.ID)
			if row.Status != StatusError {
				t.Fatal("observer cleared failed application")
			}
			h.pf.edgeError = nil
			h.dk.ExecFn = originalExec
			j, err = h.svc.Retry(ctx, st.ID, "test")
			if err != nil {
				t.Fatal(err)
			}
			j = h.waitJob(t, ctx, j.ID)
			row, _ = h.svc.Get(ctx, st.ID)
			if j.Status != jobs.StatusSucceeded || !row.TrafficReady || row.AppliedRevision != row.ConfigRevision {
				t.Fatalf("retry: %s %+v", j.Error, row)
			}
		})
	}
}

func TestRolloutVerifiesApplicationAndContainer(t *testing.T) {
	h, ctx, st := provisioned(t)
	res, err := h.svc.Catalog().Resolve(st.WPVersion, st.PHPVersion)
	if err != nil {
		t.Fatal(err)
	}
	if j, err := h.svc.ApplyRuntime(ctx, st.ID, res, "test"); err != nil || j != nil {
		t.Fatalf("verified success not skipped: %v %v", j, err)
	}
	info, err := h.dk.InspectContainer(ctx, ContainerName(st.Slug))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dk.RemoveContainer(ctx, info.ID, true); err != nil {
		t.Fatal(err)
	}
	j, err := h.svc.ApplyRuntime(ctx, st.ID, res, "test")
	if err != nil || j == nil {
		t.Fatalf("missing container skipped: %v %v", j, err)
	}
	if out := h.waitJob(t, ctx, j.ID); out.Status != jobs.StatusSucceeded {
		t.Fatal(out.Error)
	}
	if err := h.st.InvalidateSiteApplication(ctx, store.InvalidateSiteApplicationParams{ID: st.ID, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	j, err = h.svc.ApplyRuntime(ctx, st.ID, res, "test")
	if err != nil || j == nil {
		t.Fatalf("incomplete matching image skipped: %v %v", j, err)
	}
	if out := h.waitJob(t, ctx, j.ID); out.Status != jobs.StatusSucceeded {
		t.Fatal(out.Error)
	}
	h.jobs.Pause()
	if _, err := h.svc.ApplyRuntime(ctx, st.ID, res, "test"); !errors.Is(err, jobs.ErrPaused) {
		t.Fatalf("queue rejection lost: %v", err)
	}
}

func TestProvisioningCannotBePublishedByAnotherRefresh(t *testing.T) {
	h, ctx := newHarness(t)
	blocked := make(chan struct{})
	release := make(chan struct{})
	original := h.dk.ExecFn
	h.dk.ExecFn = func(id string, o docker.ExecOptions) (docker.ExecResult, error) {
		if strings.Contains(strings.Join(o.Cmd, " "), "core install") {
			close(blocked)
			select {
			case <-release:
			case <-ctx.Done():
				return docker.ExecResult{}, ctx.Err()
			}
		}
		return original(id, o)
	}
	st, j, err := h.svc.Create(ctx, CreateInput{Slug: "unfinished", Name: "Unfinished", PrimaryDomain: "unfinished.localhost", AdminUser: "admin", AdminEmail: "admin@example.com", AdminPassword: "a-long-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	<-blocked
	render, err := h.svc.RenderEdge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(render, "Site unavailable") || strings.Contains(render, "reverse_proxy site-") {
		t.Fatal("installer exposed by full refresh")
	}
	row, _ := h.svc.Get(ctx, st.ID)
	if row.TrafficReady {
		t.Fatal("provisioning ready before installation")
	}
	close(release)
	if out := h.waitJob(t, ctx, j.ID); out.Status != jobs.StatusSucceeded {
		t.Fatal(out.Error)
	}
}

// faultDocker injects failures at real service boundaries without waiting for
// production health timeouts or modifying shared Docker state.
type faultDocker struct {
	docker.Client
	failure string
}

func (f faultDocker) EnsureImage(ctx context.Context, image string, progress func(string)) error {
	if f.failure == "image" {
		return errors.New("injected pull failure")
	}
	return f.Client.EnsureImage(ctx, image, progress)
}
func (f faultDocker) InspectContainer(ctx context.Context, id string) (*docker.ContainerInfo, error) {
	info, err := f.Client.InspectContainer(ctx, id)
	if err == nil && f.failure == "health" && info.Running() {
		info.State = "dead"
		info.ExitCode = 17
	}
	return info, err
}

func TestImageHealthAndPurgeFailures(t *testing.T) {
	for _, failure := range []string{"image", "health", "purge"} {
		t.Run(failure, func(t *testing.T) {
			h, ctx, st := provisioned(t)
			before := len(h.dk.Created)
			if failure == "purge" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "injected", 503) }))
				defer server.Close()
				h.svc.edge = edge.NewClient(strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(server.URL, "http://"), "")
			} else {
				h.svc.dk = faultDocker{Client: h.dk, failure: failure}
			}
			j, err := h.svc.Recreate(ctx, st.ID, "test")
			if err != nil {
				t.Fatal(err)
			}
			out := h.waitJob(t, ctx, j.ID)
			if out.Status != jobs.StatusFailed {
				t.Fatal("injected failure reported success")
			}
			row, _ := h.svc.Get(ctx, st.ID)
			if failure == "image" {
				if !row.TrafficReady || len(h.dk.Created) != before {
					t.Fatal("pull failure disturbed previous validated runtime")
				}
			} else if row.TrafficReady {
				t.Fatal("incomplete application serving traffic")
			}
			if failure == "purge" && len(h.dk.Created) != before {
				t.Fatal("purge failure did not prevent replacement")
			}
			h.svc.edge = nil
			h.svc.dk = h.dk
			j, err = h.svc.Retry(ctx, st.ID, "test")
			if err != nil {
				t.Fatal(err)
			}
			if out = h.waitJob(t, ctx, j.ID); out.Status != jobs.StatusSucceeded {
				t.Fatal(out.Error)
			}
		})
	}
}

func TestInterruptedApplicationStaysPending(t *testing.T) {
	h, ctx, st := provisioned(t)
	// Model process interruption with an unfinished persisted job; recovery is
	// deliberately performed before any observer or edge reconciliation.
	_, err := h.st.CreateJob(ctx, store.CreateJobParams{ID: uuid.New(), Kind: JobRecreate, SiteID: nullID(st.ID), Status: jobs.StatusRunning, Args: "{}", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	runner := jobs.New(h.st, nil, 1)
	restartCtx, cancel := context.WithCancel(ctx)
	if err := runner.Start(restartCtx); err != nil {
		t.Fatal(err)
	}
	cancel()
	runner.Wait()
	if err := h.svc.ReconcileAll(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ := h.svc.Get(ctx, st.ID)
	if row.Status != StatusError || row.TrafficReady || row.AppliedRevision == row.ConfigRevision {
		t.Fatalf("interrupted application republished: %+v", row)
	}
}

func TestQueuedStartIntentSurvivesInterruption(t *testing.T) {
	h, ctx, st := provisioned(t)
	stop, err := h.svc.Stop(ctx, st.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.waitJob(t, ctx, stop.ID)
	// This runner deliberately has no workers: interrupt after acceptance but
	// before execution to exercise persisted intent rather than container state.
	pending := jobs.New(h.st, nil, 1)
	pending.Register(JobStart, h.svc.runStart)
	pending.Register(JobRecreate, h.svc.runRecreate)
	h.svc.jobs = pending
	queued, err := h.svc.Start(ctx, st.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	row, _ := h.svc.Get(ctx, st.ID)
	if row.DesiredState != StatusRunning {
		t.Fatal("accepted Start did not persist running intent")
	}
	restart := jobs.New(h.st, nil, 1)
	restartCtx, cancel := context.WithCancel(ctx)
	if err := restart.Start(restartCtx); err != nil {
		t.Fatal(err)
	}
	cancel()
	restart.Wait()
	interrupted, _ := h.st.GetJob(ctx, queued.ID)
	if interrupted.Status != jobs.StatusFailed {
		t.Fatal("queued Start not failed on restart")
	}
	retry, err := h.svc.Retry(ctx, st.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if retry.Kind != JobRecreate {
		t.Fatalf("retry lost Start intent: %s", retry.Kind)
	}
}
