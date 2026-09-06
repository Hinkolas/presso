package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/observe"
	"github.com/Hinkolas/presso/internal/site"
	"github.com/Hinkolas/presso/internal/store"
)

type siteHandlers struct {
	sites *site.Service
	st    *store.Store
	// shutdown closes when the server stops; nil for non-streaming routes.
	shutdown <-chan struct{}
}

// sitePayload is the API view plus the last-hour traffic summary.
type sitePayload struct {
	site.Site
	Metrics1h *metricsSummary `json:"metrics_1h,omitempty"`
}

type metricsSummary struct {
	Requests    int64 `json:"requests"`
	CacheHit    int64 `json:"cache_hit"`
	CacheMiss   int64 `json:"cache_miss"`
	CacheBypass int64 `json:"cache_bypass"`
	CacheStale  int64 `json:"cache_stale"`
	Status5xx   int64 `json:"status_5xx"`
}

func siteID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	return id, err == nil
}

// GET /v1/sites
func (h *siteHandlers) list(w http.ResponseWriter, r *http.Request) {
	sites, err := h.sites.List(r.Context())
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	// The list summary is visitor traffic: what the public site is doing.
	sums, _ := h.st.SumRequestMetricsSince(r.Context(), store.SumRequestMetricsSinceParams{Bucket: time.Now().UTC().Add(-time.Hour), Scope: observe.ScopeVisitor})
	byID := map[uuid.UUID]*metricsSummary{}
	for _, s := range sums {
		byID[s.SiteID] = &metricsSummary{Requests: s.Requests, CacheHit: s.CacheHit, CacheMiss: s.CacheMiss, CacheBypass: s.CacheBypass, CacheStale: s.CacheStale, Status5xx: s.Status5xx}
	}
	out := make([]sitePayload, 0, len(sites))
	for _, s := range sites {
		out = append(out, sitePayload{Site: s, Metrics1h: byID[s.ID]})
	}
	writeJSON(w, http.StatusOK, struct {
		Sites []sitePayload `json:"sites"`
	}{out})
}

// POST /v1/sites
func (h *siteHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		Slug          string `json:"slug"`
		PrimaryDomain string `json:"primary_domain"`
		TLSMode       string `json:"tls_mode"`
		PHPVersion    string `json:"php_version"`
		PageCache     *bool  `json:"page_cache"`
		Profile       string `json:"profile"`
		AdminUser     string `json:"admin_user"`
		AdminEmail    string `json:"admin_email"`
		AdminPassword string `json:"admin_password"`
		Title         string `json:"title"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	s, job, err := h.sites.Create(r.Context(), site.CreateInput{
		Name: req.Name, Slug: req.Slug, PrimaryDomain: req.PrimaryDomain, TLSMode: req.TLSMode, PHPVersion: req.PHPVersion,
		PageCache: req.PageCache, Profile: req.Profile, AdminUser: req.AdminUser, AdminEmail: req.AdminEmail,
		AdminPassword: req.AdminPassword, Title: req.Title, CreatedBy: actorFrom(r.Context()),
	})
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		Site site.Site `json:"site"`
		Job  jobs.Job  `json:"job"`
	}{s, jobs.JobPayload(job)})
}

// GET /v1/sites/{id}
func (h *siteHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	row, err := h.sites.Get(r.Context(), id)
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	payload, err := h.sites.Payload(r.Context(), row)
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	var active *jobs.Job
	if j, err := h.sites.ActiveJob(r.Context(), id); err == nil && j != nil {
		p := jobs.JobPayload(*j)
		active = &p
	}
	writeJSON(w, http.StatusOK, struct {
		Site      site.Site `json:"site"`
		ActiveJob *jobs.Job `json:"active_job"`
	}{payload, active})
}

// PATCH /v1/sites/{id}
func (h *siteHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	var req struct {
		Name       *string                    `json:"name"`
		PHPVersion *string                    `json:"php_version"`
		PageCache  *bool                      `json:"page_cache"`
		FileMods   *string                    `json:"file_mods"`
		TLSMode    *string                    `json:"tls_mode"`
		Profile    *string                    `json:"profile"`
		Settings   map[string]json.RawMessage `json:"settings"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	s, job, err := h.sites.Update(r.Context(), id, site.UpdateInput{
		Name: req.Name, PHPVersion: req.PHPVersion, PageCache: req.PageCache, FileMods: req.FileMods,
		TLSMode: req.TLSMode, Profile: req.Profile, Settings: req.Settings, UpdatedBy: actorFrom(r.Context()),
	})
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	var jp *jobs.Job
	if job != nil {
		p := jobs.JobPayload(*job)
		jp = &p
	}
	writeJSON(w, http.StatusOK, struct {
		Site site.Site `json:"site"`
		Job  *jobs.Job `json:"job"`
	}{s, jp})
}

// DELETE /v1/sites/{id}
func (h *siteHandlers) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	var req struct {
		Confirm    string `json:"confirm"`
		KeepVolume bool   `json:"keep_volume"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	job, err := h.sites.Delete(r.Context(), id, req.Confirm, req.KeepVolume, actorFrom(r.Context()))
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		Job jobs.Job `json:"job"`
	}{jobs.JobPayload(job)})
}

// POST /v1/sites/{id}/actions/{action}
func (h *siteHandlers) action(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	ctx := r.Context()
	by := actorFrom(ctx)
	var (
		job store.Job
		err error
	)
	switch chi.URLParam(r, "action") {
	case "start":
		job, err = h.sites.Start(ctx, id, by)
	case "stop":
		job, err = h.sites.Stop(ctx, id, by)
	case "restart":
		job, err = h.sites.Restart(ctx, id, by)
	case "recreate":
		job, err = h.sites.Recreate(ctx, id, by)
	case "retry":
		job, err = h.sites.Retry(ctx, id, by)
	case "purge-cache":
		if err := h.sites.PurgeCache(ctx, id, nil); err != nil {
			writeSiteError(ctx, w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	case "flush-object-cache":
		if err := h.sites.FlushObjectCache(ctx, id, by); err != nil {
			writeSiteError(ctx, w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	case "screenshot":
		at, err := h.sites.Screenshot(ctx, id, by)
		if err != nil {
			writeSiteError(ctx, w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ScreenshotAt time.Time `json:"screenshot_at"`
		}{at})
		return
	default:
		writeError(w, http.StatusNotFound, codeNotFound, "unknown action")
		return
	}
	if err != nil {
		writeSiteError(ctx, w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, struct {
		Job jobs.Job `json:"job"`
	}{jobs.JobPayload(job)})
}

// GET /v1/sites/{id}/screenshot serves the stored front-page preview. The
// ETag is the capture time, so the SPA can revalidate cheaply; the response
// is private because it is behind the session.
func (h *siteHandlers) screenshot(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	path, at, ok := h.sites.ScreenshotFile(id)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "no preview captured yet")
		return
	}
	etag := `"` + strconv.FormatInt(at.UnixNano(), 36) + `"`
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no preview captured yet")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Last-Modified", at.UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// POST /v1/sites/{id}/wp
func (h *siteHandlers) wp(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	var req struct {
		Args []string `json:"args"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	res, err := h.sites.RunWP(r.Context(), id, req.Args, actorFrom(r.Context()))
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /v1/sites/{id}/login-link
func (h *siteHandlers) loginLink(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	url, exp, err := h.sites.LoginLink(r.Context(), id, actorFrom(r.Context()))
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}{url, exp})
}

// GET /v1/sites/{id}/activity
func (h *siteHandlers) activity(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	if _, err := h.sites.Get(r.Context(), id); err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	rows, err := h.sites.Activity(r.Context(), id, queryLimit(r, 50, 500))
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Activity []activityPayload `json:"activity"`
	}{activityPayloads(rows)})
}

type activityPayload struct {
	ID      uuid.UUID       `json:"id"`
	Ts      time.Time       `json:"ts"`
	UserID  *uuid.UUID      `json:"user_id"`
	SiteID  *uuid.UUID      `json:"site_id"`
	Kind    string          `json:"kind"`
	Message string          `json:"message"`
	Meta    json.RawMessage `json:"meta"`
}

func activityPayloads(rows []store.Activity) []activityPayload {
	out := make([]activityPayload, 0, len(rows))
	for _, a := range rows {
		p := activityPayload{ID: a.ID, Ts: a.Ts, Kind: a.Kind, Message: a.Message, Meta: json.RawMessage(a.Meta)}
		if !json.Valid(p.Meta) {
			p.Meta = json.RawMessage("{}")
		}
		if a.UserID.Valid {
			u := a.UserID.UUID
			p.UserID = &u
		}
		if a.SiteID.Valid {
			s := a.SiteID.UUID
			p.SiteID = &s
		}
		out = append(out, p)
	}
	return out
}

// GET /v1/sites/{id}/stats
func (h *siteHandlers) stats(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	st, err := h.sites.Stats(r.Context(), id)
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Read       time.Time `json:"read"`
		CPUPercent float64   `json:"cpu_percent"`
		MemUsage   uint64    `json:"mem_usage"`
		MemLimit   uint64    `json:"mem_limit"`
	}{st.Read, st.CPUPercent, st.MemUsage, st.MemLimit})
}

// GET /v1/sites/{id}/logs?tail=200&follow=1  (SSE)
func (h *siteHandlers) logs(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	tail := int(queryLimit(r, 200, 5000))
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 5000 {
			tail = n
		}
	}
	follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"
	ctx, cancel := context_with_cancel(r)
	defer cancel()
	lines, errc, err := h.sites.Logs(ctx, id, docker.LogOptions{Follow: follow, Tail: tail})
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	sse, err := newSSE(w)
	if err != nil {
		return
	}
	hb := time.NewTicker(sseHeartbeat)
	defer hb.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.shutdown:
			_ = sse.Send("end", map[string]string{"reason": "engine shutting down"})
			return
		case <-hb.C:
			if sse.Heartbeat() != nil {
				return
			}
		case l, ok := <-lines:
			if !ok {
				_ = sse.Send("end", map[string]string{"reason": "stream closed"})
				return
			}
			if sse.Send("line", l) != nil {
				return
			}
		case err := <-errc:
			if err != nil {
				_ = sse.Send("end", map[string]string{"reason": err.Error()})
				return
			}
		}
	}
}

// GET /v1/sites/{id}/metrics?range=1h|24h|7d&scope=visitor|admin|all
//
// scope defaults to visitor: public traffic without wp-admin, wp-login, the
// REST API, wp-cron and XML-RPC, which are counted under admin.
func (h *siteHandlers) metrics(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	if _, err := h.sites.Get(r.Context(), id); err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	rng := r.URL.Query().Get("range")
	var span, step time.Duration
	switch rng {
	case "", "1h":
		rng, span, step = "1h", time.Hour, time.Minute
	case "24h":
		span, step = 24*time.Hour, 5*time.Minute
	case "7d":
		span, step = 7*24*time.Hour, time.Hour
	default:
		writeError(w, http.StatusBadRequest, codeBadRequest, "range must be 1h, 24h or 7d")
		return
	}
	scope := r.URL.Query().Get("scope")
	switch scope {
	case "":
		scope = observe.ScopeVisitor
	case observe.ScopeVisitor, observe.ScopeAdmin, "all":
	default:
		writeError(w, http.StatusBadRequest, codeBadRequest, "scope must be visitor, admin or all")
		return
	}
	since := time.Now().UTC().Add(-span).Truncate(step)
	var reqRows []store.SiteRequestMetric
	var err error
	if scope == "all" {
		reqRows, err = h.st.ListRequestMetrics(r.Context(), store.ListRequestMetricsParams{SiteID: id, Bucket: since})
	} else {
		reqRows, err = h.st.ListRequestMetricsByScope(r.Context(), store.ListRequestMetricsByScopeParams{SiteID: id, Bucket: since, Scope: scope})
	}
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	ctrRows, err := h.st.ListContainerMetrics(r.Context(), store.ListContainerMetricsParams{SiteID: id, Bucket: since})
	if err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Range       string            `json:"range"`
		Scope       string            `json:"scope"`
		StepSeconds int               `json:"step_seconds"`
		Requests    []requestBucket   `json:"requests"`
		Container   []containerBucket `json:"container"`
	}{rng, scope, int(step / time.Second), downsampleRequests(reqRows, step), downsampleContainer(ctrRows, step)})
}

type requestBucket struct {
	Bucket      time.Time `json:"bucket"`
	Requests    int64     `json:"requests"`
	CacheHit    int64     `json:"cache_hit"`
	CacheMiss   int64     `json:"cache_miss"`
	CacheBypass int64     `json:"cache_bypass"`
	CacheStale  int64     `json:"cache_stale"`
	Status2xx   int64     `json:"status_2xx"`
	Status3xx   int64     `json:"status_3xx"`
	Status4xx   int64     `json:"status_4xx"`
	Status5xx   int64     `json:"status_5xx"`
	Bytes       int64     `json:"bytes"`
	DurP50Ms    float64   `json:"dur_p50_ms"`
	DurP95Ms    float64   `json:"dur_p95_ms"`
}

type containerBucket struct {
	Bucket   time.Time `json:"bucket"`
	CPUPct   float64   `json:"cpu_pct"`
	MemBytes int64     `json:"mem_bytes"`
	MemLimit int64     `json:"mem_limit"`
}

// downsampleRequests merges minute rows into step-sized buckets: counts add
// up, percentiles take the worst minute (a conservative approximation).
func downsampleRequests(rows []store.SiteRequestMetric, step time.Duration) []requestBucket {
	out := []requestBucket{}
	for _, r := range rows {
		b := r.Bucket.Truncate(step)
		if n := len(out); n == 0 || !out[n-1].Bucket.Equal(b) {
			out = append(out, requestBucket{Bucket: b})
		}
		cur := &out[len(out)-1]
		cur.Requests += r.Requests
		cur.CacheHit += r.CacheHit
		cur.CacheMiss += r.CacheMiss
		cur.CacheBypass += r.CacheBypass
		cur.CacheStale += r.CacheStale
		cur.Status2xx += r.Status2xx
		cur.Status3xx += r.Status3xx
		cur.Status4xx += r.Status4xx
		cur.Status5xx += r.Status5xx
		cur.Bytes += r.Bytes
		cur.DurP50Ms = max(cur.DurP50Ms, r.DurP50Ms)
		cur.DurP95Ms = max(cur.DurP95Ms, r.DurP95Ms)
	}
	return out
}

func downsampleContainer(rows []store.SiteContainerMetric, step time.Duration) []containerBucket {
	out := []containerBucket{}
	counts := []int{}
	for _, r := range rows {
		b := r.Bucket.Truncate(step)
		if n := len(out); n == 0 || !out[n-1].Bucket.Equal(b) {
			out = append(out, containerBucket{Bucket: b})
			counts = append(counts, 0)
		}
		i := len(out) - 1
		counts[i]++
		out[i].CPUPct += r.CpuPct
		out[i].MemBytes = max(out[i].MemBytes, r.MemBytes)
		out[i].MemLimit = max(out[i].MemLimit, r.MemLimit)
	}
	for i := range out {
		out[i].CPUPct /= float64(counts[i])
	}
	return out
}

func queryLimit(r *http.Request, def, maxN int64) int64 {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 && n <= maxN {
			return n
		}
	}
	return def
}
