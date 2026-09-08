package api

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/store"
)

type systemHandlers struct {
	version  string
	cfg      *config.API
	catalog  *compat.Catalog
	dk       docker.Client
	platform *platform.Service
	st       *store.Store
	started  time.Time
}

// GET /v1/system/meta
func (h *systemHandlers) meta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Version string `json:"version"`
	}{h.version})
}

// GET /v1/system/runtime: what this server can run (internal/compat), the
// contract the UI's version picker is built on.
func (h *systemHandlers) runtime(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeJSON(w, http.StatusOK, compat.Capabilities{Runtimes: []compat.RuntimeInfo{}, WordPress: []compat.WordPressInfo{}})
		return
	}
	writeJSON(w, http.StatusOK, h.catalog.Capabilities())
}

type dockerStatus struct {
	OK         bool   `json:"ok"`
	Version    string `json:"version"`
	APIVersion string `json:"api_version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Error      string `json:"error,omitempty"`
}

type edgeStatus struct {
	LastAppliedAt *time.Time `json:"last_applied_at"`
	LastError     string     `json:"last_error"`
	Sites         int64      `json:"sites"`
}

type diskStatus struct {
	DataDirBytes    int64 `json:"data_dir_bytes"`
	ImagesBytes     int64 `json:"images_bytes"`
	ContainersBytes int64 `json:"containers_bytes"`
	VolumesBytes    int64 `json:"volumes_bytes"`
}

// GET /v1/system/status
func (h *systemHandlers) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := struct {
		Version       string               `json:"version"`
		Docker        dockerStatus         `json:"docker"`
		Components    []platform.Component `json:"components"`
		Edge          edgeStatus           `json:"edge"`
		Sites         map[string]int64     `json:"sites"`
		Disk          diskStatus           `json:"disk"`
		UptimeSeconds int64                `json:"uptime_seconds"`
	}{Version: h.version, Components: []platform.Component{}, Sites: map[string]int64{}, UptimeSeconds: int64(time.Since(h.started).Seconds())}

	if h.dk != nil {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := h.dk.Ping(pctx)
		cancel()
		if err != nil {
			out.Docker.Error = err.Error()
		} else {
			out.Docker = dockerStatus{OK: true, Version: info.Version, APIVersion: info.APIVersion, OS: info.OS, Arch: info.Arch}
		}
		if out.Docker.OK {
			dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if du, err := h.dk.DiskUsage(dctx); err == nil {
				out.Disk.ImagesBytes, out.Disk.ContainersBytes, out.Disk.VolumesBytes = du.ImagesBytes, du.ContainersBytes, du.VolumesBytes
			}
			cancel()
		}
	}
	if h.platform != nil {
		out.Components = h.platform.Components()
		out.Edge.LastAppliedAt, out.Edge.LastError = h.platform.EdgeStatus(ctx)
	}
	if h.st != nil {
		if counts, err := h.st.CountSitesByStatus(ctx); err == nil {
			for _, c := range counts {
				out.Sites[c.Status] = c.N
				if c.Status != "deleting" {
					out.Edge.Sites += c.N
				}
			}
		}
	}
	if h.cfg != nil {
		out.Disk.DataDirBytes = dirSize(h.cfg.DataDir)
	}
	writeJSON(w, http.StatusOK, out)
}

// dirSize sums file sizes under dir (the engine's SQLite and rendered
// configs; small enough to walk on every status call).
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	if _, err := os.Stat(dir); err != nil {
		return 0
	}
	return total
}
