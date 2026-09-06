package api

import (
	"net/http"

	"github.com/Hinkolas/presso/internal/site"
)

// internalHandlers serve the system plugin running inside site containers.
// They authenticate with per-site tokens, not operator sessions.
type internalHandlers struct {
	sites *site.Service
}

// POST /v1/internal/sites/{id}/purge   Authorization: Bearer <purge token>
// Body: {"all": true} or {"tags": ["s<id>:p12", ...]}
func (h *internalHandlers) purge(w http.ResponseWriter, r *http.Request) {
	id, ok := siteID(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
		return
	}
	token, _ := bearerToken(r)
	if err := h.sites.VerifyPurgeToken(r.Context(), id, token); err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	var req struct {
		All  bool     `json:"all"`
		Tags []string `json:"tags"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	tags := req.Tags
	if req.All {
		tags = nil
	} else if len(tags) == 0 {
		writeError(w, http.StatusBadRequest, codeBadRequest, "tags or all is required")
		return
	}
	if err := h.sites.PurgeCache(r.Context(), id, tags); err != nil {
		writeSiteError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
