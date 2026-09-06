// Package obs installs the process-wide structured logger (slog). It is the
// one place that decides log format and level; everything else just calls
// slog.InfoContext(ctx, "message", "key", value).
//
// Tracing and metrics are deliberately absent for now. When they arrive they
// slot in here (OpenTelemetry via autoexport, off unless an OTLP endpoint is
// configured) without touching call sites.
package obs

import (
	"log/slog"
	"os"
	"strings"
)

// Options configures logging for a binary.
type Options struct {
	ServiceName string
	Version     string
	LogLevel    string // debug|info|warn|error
	LogFormat   string // text|json
}

// Init installs the global slog logger.
func Init(opt Options) {
	h := &slog.HandlerOptions{Level: parseLevel(opt.LogLevel)}
	var base slog.Handler
	if strings.EqualFold(opt.LogFormat, "json") {
		base = slog.NewJSONHandler(os.Stdout, h)
	} else {
		base = slog.NewTextHandler(os.Stdout, h)
	}
	logger := slog.New(base).With("service", opt.ServiceName, "version", opt.Version)
	slog.SetDefault(logger)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
