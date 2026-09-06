package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// sseWriter streams server-sent events. Each Send is one event; a comment
// heartbeat keeps proxies from closing idle connections.
type sseWriter struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newSSE(w http.ResponseWriter) (*sseWriter, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &sseWriter{w: w, rc: http.NewResponseController(w)}
	return s, s.rc.Flush()
}

func (s *sseWriter) Send(event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	return s.rc.Flush()
}

func (s *sseWriter) Heartbeat() error {
	if _, err := fmt.Fprint(s.w, ": ping\n\n"); err != nil {
		return err
	}
	return s.rc.Flush()
}

const sseHeartbeat = 20 * time.Second
