package observability

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/marz32one/graph-api-gateway/internal/config"
)

// NewLogger builds a slog.Logger whose handler injects trace_id and span_id
// when the record context carries a valid OpenTelemetry span.
func NewLogger(cfg *config.Config) *slog.Logger {
	return slog.New(newTraceHandler(os.Stdout, cfg.LogFormat, parseLevel(cfg.LogLevel)))
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

func newTraceHandler(w io.Writer, format string, level slog.Level) slog.Handler {
	opts := &slog.HandlerOptions{Level: level}
	var inner slog.Handler
	if format == "text" {
		inner = slog.NewTextHandler(w, opts)
	} else {
		inner = slog.NewJSONHandler(w, opts)
	}
	return &traceHandler{inner: inner}
}

// traceHandler wraps another slog.Handler and adds trace_id / span_id attrs
// when the record's context carries a valid span context.
type traceHandler struct {
	inner slog.Handler
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{inner: h.inner.WithGroup(name)}
}
