// Package jobs runs the engine's long operations (provision, delete,
// recreate, ...) in the background with a durable record: every job is a row
// in SQLite with a status and an append-only event log, so the UI can show
// progress live (through internal/events) and after the fact. Handlers are
// registered by kind; the runner serializes jobs per site so two operations
// never touch one site concurrently, while different sites proceed in
// parallel.
//
// Jobs are in-process only. After an engine restart, unfinished jobs are
// marked failed and the owning service reconciles the site's state.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/store"
)

// Status values stored in jobs.status.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// Event levels stored in job_events.level.
const (
	LevelStep  = "step"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

var (
	ErrUnknownKind = errors.New("jobs: unknown job kind")
	ErrQueueFull   = errors.New("jobs: queue full")
	ErrNotFound    = errors.New("jobs: not found")
)

// Handler executes one job. Progress goes through the Reporter; the returned
// error becomes the job's failure message.
type Handler func(ctx context.Context, job store.Job, r *Reporter) error

// Step is one planned phase of a job kind: the name the handler passes to
// Reporter.Step and a short label for the UI, which uses the plan to show
// what is still to come and how far along a job is. Steps a handler skips
// (a conditional phase) simply never fire.
type Step struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// plans is the kind → planned steps registry. Kinds are static metadata
// shared by every runner, so it lives at package level and JobPayload can
// attach it without a runner in hand.
var (
	plansMu sync.RWMutex
	plans   = map[string][]Step{}
)

// planFor returns a copy of the plan for kind, or nil when none was declared.
func planFor(kind string) []Step {
	plansMu.RLock()
	defer plansMu.RUnlock()
	p := plans[kind]
	if len(p) == 0 {
		return nil
	}
	return append([]Step(nil), p...)
}

type Runner struct {
	st       *store.Store
	hub      *events.Hub
	handlers map[string]Handler
	workers  int
	queue    chan uuid.UUID
	locks    keyedMutex
	now      func() time.Time
	wg       sync.WaitGroup
}

func New(st *store.Store, hub *events.Hub, workers int) *Runner {
	if workers <= 0 {
		workers = 4
	}
	return &Runner{
		st: st, hub: hub, handlers: map[string]Handler{}, workers: workers,
		queue: make(chan uuid.UUID, 1024), now: func() time.Time { return time.Now().UTC() },
	}
}

// Register binds a handler to a job kind, with the steps it is expected to
// report in order. Must happen before Start.
func (r *Runner) Register(kind string, h Handler, plan ...Step) {
	r.handlers[kind] = h
	plansMu.Lock()
	plans[kind] = append([]Step(nil), plan...)
	plansMu.Unlock()
}

// Start fails whatever a previous process left unfinished, then launches the
// workers. It returns immediately; workers stop when ctx is cancelled and
// Wait blocks until they have.
func (r *Runner) Start(ctx context.Context) error {
	now := r.now()
	n, err := r.st.FailUnfinishedJobs(ctx, store.FailUnfinishedJobsParams{
		Error: "engine restarted while the job was in progress", FinishedAt: sqlTime(now), UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("jobs: fail unfinished: %w", err)
	}
	if n > 0 {
		slog.WarnContext(ctx, "jobs: marked unfinished jobs from a previous run as failed", "count", n)
	}
	for i := 0; i < r.workers; i++ {
		r.wg.Add(1)
		go r.worker(ctx)
	}
	return nil
}

// Wait blocks until all workers have exited (after ctx cancellation).
func (r *Runner) Wait() { r.wg.Wait() }

// Enqueue records a job and schedules it. args is JSON-encoded into the row.
func (r *Runner) Enqueue(ctx context.Context, kind string, siteID uuid.NullUUID, args any, createdBy string) (store.Job, error) {
	if _, ok := r.handlers[kind]; !ok {
		return store.Job{}, fmt.Errorf("%w: %s", ErrUnknownKind, kind)
	}
	raw := []byte("{}")
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return store.Job{}, fmt.Errorf("jobs: encode args: %w", err)
		}
		raw = b
	}
	id, err := uuid.NewV7()
	if err != nil {
		return store.Job{}, fmt.Errorf("jobs: id: %w", err)
	}
	now := r.now()
	job, err := r.st.CreateJob(ctx, store.CreateJobParams{
		ID: id, Kind: kind, SiteID: siteID, Status: StatusQueued, Args: string(raw),
		CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return store.Job{}, fmt.Errorf("jobs: create: %w", err)
	}
	select {
	case r.queue <- job.ID:
	default:
		_ = r.st.FinishJob(ctx, store.FinishJobParams{Status: StatusFailed, Error: ErrQueueFull.Error(), FinishedAt: sqlTime(now), UpdatedAt: now, ID: job.ID})
		return store.Job{}, ErrQueueFull
	}
	r.publishJob(job)
	return job, nil
}

func (r *Runner) worker(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-r.queue:
			r.run(ctx, id)
		}
	}
}

func (r *Runner) run(ctx context.Context, id uuid.UUID) {
	job, err := r.st.GetJob(ctx, id)
	if err != nil {
		slog.ErrorContext(ctx, "jobs: load", "job_id", id, "err", err)
		return
	}
	if job.Status != StatusQueued {
		return
	}
	// Per-site serialization: hold the site's lock for the whole run.
	lockKey := "global"
	if job.SiteID.Valid {
		lockKey = job.SiteID.UUID.String()
	}
	unlock := r.locks.lock(lockKey)
	defer unlock()

	now := r.now()
	if err := r.st.StartJob(ctx, store.StartJobParams{StartedAt: sqlTime(now), UpdatedAt: now, ID: id}); err != nil {
		slog.ErrorContext(ctx, "jobs: start", "job_id", id, "err", err)
		return
	}
	job.Status, job.StartedAt, job.UpdatedAt = StatusRunning, sqlTime(now), now
	r.publishJob(job)

	rep := &Reporter{runner: r, jobID: id}
	handler := r.handlers[job.Kind]
	var runErr error
	func() {
		defer func() {
			if p := recover(); p != nil {
				runErr = fmt.Errorf("panic: %v", p)
			}
		}()
		runErr = handler(ctx, job, rep)
	}()

	status, msg := StatusSucceeded, ""
	if runErr != nil {
		status, msg = StatusFailed, runErr.Error()
		rep.Error(ctx, msg)
	}
	end := r.now()
	// The job outcome must be recorded even when the engine is shutting down,
	// so use a detached context for the final write.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := r.st.FinishJob(wctx, store.FinishJobParams{Status: status, Error: msg, FinishedAt: sqlTime(end), UpdatedAt: end, ID: id}); err != nil {
		slog.ErrorContext(ctx, "jobs: finish", "job_id", id, "err", err)
	}
	job.Status, job.Error, job.FinishedAt, job.UpdatedAt = status, msg, sqlTime(end), end
	r.publishJob(job)
	slog.InfoContext(ctx, "jobs: finished", "job_id", id, "kind", job.Kind, "status", status, "duration_ms", end.Sub(now).Milliseconds())
}

func (r *Runner) publishJob(job store.Job) {
	if r.hub != nil {
		r.hub.Publish(events.TypeJobUpdated, JobPayload(job))
	}
}

// Reporter appends to a job's event log.
type Reporter struct {
	runner *Runner
	jobID  uuid.UUID
	step   string
}

// Step starts a named phase; subsequent Info/Warn lines carry its name.
func (rp *Reporter) Step(ctx context.Context, name, message string) {
	rp.step = name
	rp.emit(ctx, LevelStep, message)
}
func (rp *Reporter) Info(ctx context.Context, message string)  { rp.emit(ctx, LevelInfo, message) }
func (rp *Reporter) Warn(ctx context.Context, message string)  { rp.emit(ctx, LevelWarn, message) }
func (rp *Reporter) Error(ctx context.Context, message string) { rp.emit(ctx, LevelError, message) }

// Infof is Info with formatting.
func (rp *Reporter) Infof(ctx context.Context, format string, args ...any) {
	rp.Info(ctx, fmt.Sprintf(format, args...))
}

func (rp *Reporter) emit(ctx context.Context, level, message string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	ev, err := rp.runner.st.CreateJobEvent(wctx, store.CreateJobEventParams{
		JobID: rp.jobID, Ts: rp.runner.now(), Level: level, Step: rp.step, Message: message,
	})
	if err != nil {
		slog.ErrorContext(ctx, "jobs: record event", "job_id", rp.jobID, "err", err)
		return
	}
	if rp.runner.hub != nil {
		rp.runner.hub.Publish(events.TypeJobEvent, EventPayload(ev))
	}
}

// --- payloads shared with the API ---

type Job struct {
	ID         uuid.UUID       `json:"id"`
	Kind       string          `json:"kind"`
	SiteID     *uuid.UUID      `json:"site_id"`
	Status     string          `json:"status"`
	Args       json.RawMessage `json:"args"`
	Error      string          `json:"error"`
	CreatedBy  string          `json:"created_by"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	// Plan lists the steps this kind reports, in order; empty when the kind
	// declared none.
	Plan []Step `json:"plan"`
}

type Event struct {
	ID      int64     `json:"id"`
	JobID   uuid.UUID `json:"job_id"`
	Ts      time.Time `json:"ts"`
	Level   string    `json:"level"`
	Step    string    `json:"step"`
	Message string    `json:"message"`
}

func JobPayload(j store.Job) Job {
	out := Job{
		ID: j.ID, Kind: j.Kind, Status: j.Status, Args: json.RawMessage(j.Args), Error: j.Error,
		CreatedBy: j.CreatedBy, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
		Plan: planFor(j.Kind),
	}
	if out.Plan == nil {
		out.Plan = []Step{}
	}
	if !json.Valid(out.Args) {
		out.Args = json.RawMessage("{}")
	}
	if j.SiteID.Valid {
		id := j.SiteID.UUID
		out.SiteID = &id
	}
	if j.StartedAt.Valid {
		t := j.StartedAt.Time
		out.StartedAt = &t
	}
	if j.FinishedAt.Valid {
		t := j.FinishedAt.Time
		out.FinishedAt = &t
	}
	return out
}

func EventPayload(e store.JobEvent) Event {
	return Event{ID: e.ID, JobID: e.JobID, Ts: e.Ts, Level: e.Level, Step: e.Step, Message: e.Message}
}

// --- helpers ---

type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = map[string]*sync.Mutex{}
	}
	m, ok := k.locks[key]
	if !ok {
		m = &sync.Mutex{}
		k.locks[key] = m
	}
	k.mu.Unlock()
	m.Lock()
	return m.Unlock
}
