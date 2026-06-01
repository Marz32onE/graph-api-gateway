// Package observability wires structured logging via log/slog.
package observability

import (
	"log/slog"
	"os"

	"github.com/marz32one/graph-api-gateway/internal/config"
)

// NewLogger builds a slog.Logger writing to stdout, using a JSON handler by
// default and a text handler when cfg.LogFormat == "text".
func NewLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}
	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
