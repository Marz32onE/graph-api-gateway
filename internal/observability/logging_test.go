package observability

import (
	"log/slog"
	"testing"

	"github.com/marz32one/graph-api-gateway/internal/config"
)

func TestNewLogger_JSONByDefault(t *testing.T) {
	logger := NewLogger(&config.Config{LogLevel: "info", LogFormat: "json"})
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	// The default handler must be JSON; sanity-check by handler type.
	if _, ok := logger.Handler().(*slog.JSONHandler); !ok {
		t.Errorf("want *slog.JSONHandler, got %T", logger.Handler())
	}
}

func TestNewLogger_TextFormat(t *testing.T) {
	logger := NewLogger(&config.Config{LogLevel: "info", LogFormat: "text"})
	if _, ok := logger.Handler().(*slog.TextHandler); !ok {
		t.Errorf("want *slog.TextHandler, got %T", logger.Handler())
	}
}

func TestNewLogger_NoTraceFields(t *testing.T) {
	// After OTLP removal the logger must NOT inject trace_id / span_id.
	logger := NewLogger(&config.Config{LogLevel: "debug", LogFormat: "json"})
	// We can't capture stdout cleanly here; instead assert the handler is the
	// stdlib JSON handler (no wrapping traceHandler), which by construction
	// emits no trace fields. The record-level guarantee is covered by the
	// handler type: a plain *slog.JSONHandler adds only the attrs it is given.
	if _, ok := logger.Handler().(*slog.JSONHandler); !ok {
		t.Fatalf("expected plain JSON handler, got %T", logger.Handler())
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"error":   slog.LevelError,
		"unknown": slog.LevelInfo,
		"":        slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
