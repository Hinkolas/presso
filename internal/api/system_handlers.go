package api

import "net/http"

type systemHandlers struct {
	version string
}

// GET /v1/system/meta
func (h *systemHandlers) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Version string `json:"version"`
	}{h.version})
}
