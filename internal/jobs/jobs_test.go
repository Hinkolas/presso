package jobs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/testdb"
)

func waitStatus(t *testing.T, st *store.Store, id uuid.UUID, want string) store.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := st.GetJob(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %s", id, want)
	return store.Job{}
}

func TestRunnerLifecycle(t *testing.T) {
	st := testdb.New(t)
	hub := events.New()
	sub, cancelSub := hub.Subscribe(64)
	defer cancelSub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := New(st, hub, 2)
	r.Register("ok", func(ctx context.Context, job store.Job, rep *Reporter) error {
		rep.Step(ctx, "one", "doing one")
		rep.Info(ctx, "detail")
		return nil
	})
	r.Register("boom", func(context.Context, store.Job, *Reporter) error { return errors.New("kaboom") })
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}

	j, err := r.Enqueue(ctx, "ok", uuid.NullUUID{}, map[string]string{"a": "b"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	done := waitStatus(t, st, j.ID, StatusSucceeded)
	if !done.StartedAt.Valid || !done.FinishedAt.Valid {
		t.Fatalf("timestamps missing: %+v", done)
	}
	evs, _ := st.ListJobEvents(ctx, j.ID)
	if len(evs) != 2 || evs[0].Level != LevelStep || evs[1].Step != "one" {
		t.Fatalf("events: %+v", evs)
	}

	f, _ := r.Enqueue(ctx, "boom", uuid.NullUUID{}, nil, "test")
	failed := waitStatus(t, st, f.ID, StatusFailed)
	if failed.Error != "kaboom" {
		t.Fatalf("error: %q", failed.Error)
	}

	if _, err := r.Enqueue(ctx, "nope", uuid.NullUUID{}, nil, "test"); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind: %v", err)
	}

	// Hub saw queued/running/succeeded updates plus events.
	types := map[string]int{}
	timeout := time.After(time.Second)
	for len(types) < 2 {
		select {
		case ev := <-sub:
			types[ev.Type]++
		case <-timeout:
			t.Fatalf("hub events: %v", types)
		}
	}
}

func TestPerSiteSerialization(t *testing.T) {
	st := testdb.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := New(st, nil, 4)

	var active, maxActive int32
	var mu sync.Mutex
	r.Register("slow", func(ctx context.Context, job store.Job, rep *Reporter) error {
		n := atomic.AddInt32(&active, 1)
		mu.Lock()
		if n > maxActive {
			maxActive = n
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return nil
	})
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	siteRow, err := st.CreateSite(ctx, store.CreateSiteParams{
		ID: uuid.New(), Slug: "s", Name: "s", Status: "running", PrimaryDomain: "s.localhost", TlsMode: "off",
		PhpVersion: "8.3", Image: "img", Profile: "prod", FileMods: "locked", PageCache: true,
		DbName: "d", DbUser: "u", DbPasswordEnc: []byte("x"), RedisUser: "r", RedisPasswordEnc: []byte("x"),
		SaltsEnc: []byte("x"), PurgeTokenHash: []byte("x"), LoginSecretEnc: []byte("x"), AdminUser: "a",
		AdminEmail: "a@b.c", Settings: "{}", VolumeName: "v", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	site := uuid.NullUUID{UUID: siteRow.ID, Valid: true}
	var ids []uuid.UUID
	for i := 0; i < 4; i++ {
		j, err := r.Enqueue(ctx, "slow", site, nil, "test")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	for _, id := range ids {
		waitStatus(t, st, id, StatusSucceeded)
	}
	if maxActive != 1 {
		t.Fatalf("same-site jobs overlapped: max active %d", maxActive)
	}
}

func TestStartFailsLeftovers(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := st.CreateJob(ctx, store.CreateJobParams{ID: uuid.New(), Kind: "x", Status: StatusRunning, Args: "{}", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	r := New(st, nil, 1)
	r.Register("x", func(context.Context, store.Job, *Reporter) error { return nil })
	cctx, cancel := context.WithCancel(ctx)
	if err := r.Start(cctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	r.Wait()
	jobs, _ := st.ListJobsByStatus(ctx, StatusFailed)
	if len(jobs) != 1 {
		t.Fatalf("expected leftover marked failed, got %+v", jobs)
	}
}

func TestGlobalJobCanWaitWithOneSiteWorker(t *testing.T) {
	st := testdb.New(t)
	r := New(st, nil, 1)
	siteRow, err := st.CreateSite(context.Background(), store.CreateSiteParams{
		ID: uuid.New(), Slug: "s", Name: "s", Status: "running", PrimaryDomain: "s.localhost", TlsMode: "off",
		PhpVersion: "8.3", Image: "img", Profile: "prod", FileMods: "locked", PageCache: true,
		DbName: "d", DbUser: "u", DbPasswordEnc: []byte("x"), RedisUser: "r", RedisPasswordEnc: []byte("x"),
		SaltsEnc: []byte("x"), PurgeTokenHash: []byte("x"), LoginSecretEnc: []byte("x"), AdminUser: "a",
		AdminEmail: "a@b.c", Settings: "{}", VolumeName: "v", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.Register("child", func(context.Context, store.Job, *Reporter) error { return nil })
	r.Register("parent", func(ctx context.Context, _ store.Job, report *Reporter) error {
		child, err := r.Enqueue(ctx, "child", uuid.NullUUID{UUID: siteRow.ID, Valid: true}, nil, "test")
		if err != nil {
			return err
		}
		for {
			j, err := st.GetJob(ctx, child.ID)
			if err != nil {
				return err
			}
			if j.Status == StatusSucceeded {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond * 10):
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	j, err := r.Enqueue(ctx, "parent", uuid.NullUUID{}, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, st, j.ID, StatusSucceeded)
	cancel()
	r.Wait()
}
