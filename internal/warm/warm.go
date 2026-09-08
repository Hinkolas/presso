// Package warm keeps the edge page cache of opted-in sites warm. It walks a
// site's sitemap through the edge on a schedule and shortly after every
// purge, so pages are rendered before a visitor asks for them. A request
// for a page that is still cached is an edge hit and costs the site nothing,
// so a walk only spends origin renders on pages that actually expired or
// were purged.
//
// The warmer is a background service, not a job: it must not hold the
// per-site job lock for minutes at a time or show up in the activity list
// every hour. Sites opt in with their cache_warm setting; the operator
// bounds every run through config.Warm.
package warm

import (
	"context"
	"hash/fnv"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/observe"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
)

const (
	// debounce coalesces triggers: the system plugin fires one purge per
	// saved post, and an editor saves often.
	debounce = 20 * time.Second
	// startupGrace lets the platform reconcile the edge and the status
	// watcher settle site states before the first walk.
	startupGrace = 2 * time.Minute
	// siteParallel bounds sites walked at once; urlParallel requests in
	// flight per site. Together they cap the warmer at four requests.
	siteParallel = 2
	urlParallel  = 2
	perReqPause  = 50 * time.Millisecond
	runTimeout   = 10 * time.Minute
	// maxErrors consecutive transport failures abort a run: the edge is down
	// or the site's certificate is not issued yet, and retrying every URL
	// would only fill the log.
	maxErrors = 10

	ReasonPeriodic = "periodic"
	ReasonTrigger  = "trigger"
)

// Deps are the warmer's collaborators.
type Deps struct {
	Store  *store.Store
	Config *config.API
	// Now is the clock; nil means time.Now in UTC.
	Now func() time.Time
}

// Service is the warmer. Construct with New, run with Start.
type Service struct {
	st      *store.Store
	fetch   *Fetcher
	now     func() time.Time
	maxURLs int

	// Timing knobs live on the struct so tests can shorten them.
	interval time.Duration
	debounce time.Duration
	grace    time.Duration

	mu      sync.Mutex
	started time.Time
	state   map[uuid.UUID]*siteState
	last    map[uuid.UUID]Summary
	wake    chan struct{} // capacity 1: a nudge for the loop
	sem     chan struct{} // capacity siteParallel
	wg      sync.WaitGroup
}

type siteState struct {
	due     time.Time
	running bool
	rerun   bool // triggered while running: go again after the debounce
	reason  string
}

// Summary describes one completed run.
type Summary struct {
	SiteID   uuid.UUID
	Slug     string
	Reason   string
	Sitemap  string // document the URLs came from; empty means front page only
	Started  time.Time
	Duration time.Duration
	URLs     int
	Hit      int
	Miss     int
	Stale    int
	Bypass   int
	Errors   int
	Aborted  bool
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		st:       d.Store,
		fetch:    NewFetcher(d.Config.Edge.HTTPListen, d.Config.Edge.HTTPSListen, d.Config.Warm.Timeout),
		now:      now,
		maxURLs:  d.Config.Warm.MaxURLs,
		interval: d.Config.Warm.Interval,
		debounce: debounce,
		grace:    startupGrace,
		state:    map[uuid.UUID]*siteState{},
		last:     map[uuid.UUID]Summary{},
		wake:     make(chan struct{}, 1),
		sem:      make(chan struct{}, siteParallel),
	}
}

// Fetch requests one URL through the edge (site.Warmer).
func (s *Service) Fetch(ctx context.Context, rawURL string) (int, string, error) {
	r, err := s.fetch.Get(ctx, rawURL)
	return r.Status, r.CacheStatus, err
}

// Trigger asks for a walk of the site soon (site.Warmer). Triggers within
// the debounce window coalesce; a trigger during a run schedules exactly one
// follow-up. Eligibility is checked when the run is due, so callers need not
// know whether the site opted in.
func (s *Service) Trigger(id uuid.UUID) {
	due := s.now().Add(s.debounce)
	s.mu.Lock()
	st := s.state[id]
	switch {
	case st == nil:
		s.state[id] = &siteState{due: due, reason: ReasonTrigger}
	case st.running:
		st.rerun = true
	default:
		if due.Before(st.due) {
			st.due = due
		}
		st.reason = ReasonTrigger
	}
	s.mu.Unlock()
	s.nudge()
}

// LastRun reports the most recent completed run for a site, if any.
func (s *Service) LastRun(id uuid.UUID) (Summary, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum, ok := s.last[id]
	return sum, ok
}

func (s *Service) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Start runs the scheduler until ctx is cancelled, then waits for runs in
// flight (each request carries ctx, so they stop promptly).
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	s.started = s.now()
	s.mu.Unlock()
	if !sleep(ctx, s.grace) {
		return
	}
	for {
		s.refresh(ctx)
		s.dispatch(ctx)
		timer := time.NewTimer(s.untilNext())
		select {
		case <-ctx.Done():
			timer.Stop()
			s.wg.Wait()
			return
		case <-timer.C:
		case <-s.wake:
			timer.Stop()
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// eligible is the opt-in test: running, page cache on, warmer on.
func eligible(row store.Site) bool {
	return row.Status == site.StatusRunning && row.PageCache && site.ParseSettings(row.Settings).CacheWarm
}

// refresh seeds newly eligible sites into the schedule (staggered so a fleet
// does not walk in lockstep) and forgets sites that were deleted or opted
// out while idle.
func (s *Service) refresh(ctx context.Context) {
	rows, err := s.st.ListSites(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "warm: list sites", "err", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	present := map[uuid.UUID]bool{}
	for _, row := range rows {
		present[row.ID] = true
		st := s.state[row.ID]
		switch {
		case eligible(row) && st == nil:
			s.state[row.ID] = &siteState{due: s.firstDue(row.ID), reason: ReasonPeriodic}
		case !eligible(row) && st != nil && !st.running:
			delete(s.state, row.ID)
		}
	}
	for id, st := range s.state {
		if !present[id] && !st.running {
			delete(s.state, id)
		}
	}
}

// firstDue spreads sites over the first interval by a stable hash of their
// id, so restarts keep the same rhythm.
func (s *Service) firstDue(id uuid.UUID) time.Time {
	h := fnv.New32a()
	h.Write(id[:])
	secs := int64(s.interval / time.Second)
	if secs <= 0 {
		secs = 1
	}
	offset := time.Duration(int64(h.Sum32())%secs) * time.Second
	return s.started.Add(s.grace + offset)
}

func (s *Service) untilNext() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	next := now.Add(time.Minute)
	for _, st := range s.state {
		if !st.running && st.due.Before(next) {
			next = st.due
		}
	}
	if d := next.Sub(now); d > 0 {
		return d
	}
	return time.Millisecond
}

// dispatch starts a run for every due site that is still eligible and not
// busy with a job (the recreate that just purged it, say: then the walk
// waits one more debounce).
func (s *Service) dispatch(ctx context.Context) {
	now := s.now()
	s.mu.Lock()
	var due []uuid.UUID
	for id, st := range s.state {
		if !st.running && !st.due.After(now) {
			due = append(due, id)
		}
	}
	s.mu.Unlock()

	for _, id := range due {
		if ctx.Err() != nil {
			return
		}
		select {
		case s.sem <- struct{}{}:
		default:
			return // every slot busy; the finishing run nudges the loop
		}
		row, err := s.st.GetSiteByID(ctx, id)
		if err != nil || !eligible(row) {
			s.mu.Lock()
			delete(s.state, id)
			s.mu.Unlock()
			<-s.sem
			continue
		}
		if active, err := s.st.ListActiveJobsBySite(ctx, uuid.NullUUID{UUID: id, Valid: true}); err != nil || len(active) > 0 {
			s.mu.Lock()
			if st := s.state[id]; st != nil {
				st.due = s.now().Add(s.debounce)
			}
			s.mu.Unlock()
			<-s.sem
			continue
		}
		s.mu.Lock()
		st := s.state[id]
		if st == nil || st.running {
			s.mu.Unlock()
			<-s.sem
			continue
		}
		st.running = true
		reason := st.reason
		s.mu.Unlock()

		s.wg.Add(1)
		go func(row store.Site, reason string) {
			defer s.wg.Done()
			defer func() { <-s.sem }()
			sum := s.Run(ctx, row, reason)
			s.finish(row.ID, sum)
		}(row, reason)
	}
}

func (s *Service) finish(id uuid.UUID, sum Summary) {
	s.mu.Lock()
	s.last[id] = sum
	if st := s.state[id]; st != nil {
		st.running = false
		if st.rerun {
			st.rerun = false
			st.due = s.now().Add(s.debounce)
			st.reason = ReasonTrigger
		} else {
			st.due = s.now().Add(s.interval)
			st.reason = ReasonPeriodic
		}
	}
	s.mu.Unlock()
	s.nudge()
}

// Run walks one site now and reports what happened. It is safe to call
// directly (tests, a future operator action); the scheduler calls it from
// dispatch.
func (s *Service) Run(ctx context.Context, row store.Site, reason string) Summary {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	sum := Summary{SiteID: row.ID, Slug: row.Slug, Reason: reason, Started: s.now()}
	base, err := url.Parse(site.SiteURL(row.PrimaryDomain, row.TlsMode))
	if err != nil {
		sum.Errors++
		return sum
	}
	urls, source := Discover(ctx, s.fetch, base, s.maxURLs)
	sum.Sitemap = source
	sum.URLs = len(urls)

	var (
		mu          sync.Mutex
		consecutive atomic.Int32
		queue       = make(chan string)
		wg          sync.WaitGroup
	)
	for i := 0; i < urlParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range queue {
				r, err := s.fetch.Get(ctx, u)
				mu.Lock()
				if err != nil {
					sum.Errors++
					if consecutive.Add(1) >= maxErrors {
						sum.Aborted = true
						cancel()
					}
				} else {
					consecutive.Store(0)
					switch r.Outcome {
					case observe.OutcomeHit:
						sum.Hit++
					case observe.OutcomeMiss:
						sum.Miss++
					case observe.OutcomeStale:
						sum.Stale++
					default:
						sum.Bypass++
					}
				}
				mu.Unlock()
				if !sleep(ctx, perReqPause) {
					return
				}
			}
		}()
	}
feed:
	for _, u := range urls {
		select {
		case queue <- u:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	wg.Wait()
	sum.Duration = s.now().Sub(sum.Started)

	attrs := []any{"site", row.Slug, "reason", reason, "sitemap", source, "urls", sum.URLs,
		"hit", sum.Hit, "miss", sum.Miss, "stale", sum.Stale, "bypass", sum.Bypass, "errors", sum.Errors,
		"duration", sum.Duration.Round(time.Millisecond)}
	switch {
	case sum.Aborted:
		slog.Warn("warm: run aborted after repeated request failures", attrs...)
	default:
		slog.Info("warm: run", attrs...)
	}
	return sum
}
