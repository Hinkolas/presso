package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/store"
)

type jobHandlers struct {
	st  *store.Store
	hub *events.Hub
}

// GET /v1/jobs?limit=50&site_id=
func (h *jobHandlers) list(w http.ResponseWriter, r *http.Request) {
	limit := queryLimit(r, 50, 500)
	var rows []store.Job
	var err error
	if sid := r.URL.Query().Get("site_id"); sid != "" {
		id, perr := uuid.Parse(sid)
		if perr != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "site_id must be a uuid")
			return
		}
		rows, err = h.st.ListJobsBySite(r.Context(), store.ListJobsBySiteParams{SiteID: uuid.NullUUID{UUID: id, Valid: true}, Limit: limit})
	} else {
		rows, err = h.st.ListJobs(r.Context(), limit)
	}
	if err != nil {
		writeInternalError(r.Context(), w, "list jobs", err)
		return
	}
	out := make([]jobs.Job, 0, len(rows))
	for _, j := range rows {
		out = append(out, jobs.JobPayload(j))
	}
	writeJSON(w, http.StatusOK, struct {
		Jobs []jobs.Job `json:"jobs"`
	}{out})
}

// GET /v1/jobs/{id}
func (h *jobHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	job, err := h.st.GetJob(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	if err != nil {
		writeInternalError(r.Context(), w, "get job", err)
		return
	}
	evs, err := h.st.ListJobEvents(r.Context(), id)
	if err != nil {
		writeInternalError(r.Context(), w, "job events", err)
		return
	}
	out := make([]jobs.Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, jobs.EventPayload(e))
	}
	writeJSON(w, http.StatusOK, struct {
		Job    jobs.Job     `json:"job"`
		Events []jobs.Event `json:"events"`
	}{jobs.JobPayload(job), out})
}

// GET /v1/events  (SSE stream of hub events)
func (h *jobHandlers) events(w http.ResponseWriter, r *http.Request) {
	ch, cancel := h.hub.Subscribe(256)
	defer cancel()
	sse, err := newSSE(w)
	if err != nil {
		return
	}
	hb := time.NewTicker(sseHeartbeat)
	defer hb.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-hb.C:
			if sse.Heartbeat() != nil {
				return
			}
		case ev := <-ch:
			if sse.Send(ev.Type, ev) != nil {
				return
			}
		}
	}
}

// context_with_cancel derives a cancellable context from the request so a
// streaming handler can stop its producer on exit.
func context_with_cancel(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithCancel(r.Context())
}
