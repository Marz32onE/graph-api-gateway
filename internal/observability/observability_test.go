package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestTraceHandler_InjectsIDsInsideSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	var buf bytes.Buffer
	logger := slog.New(&traceHandler{
		inner: slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "op")
	logger.InfoContext(ctx, "inside")
	span.End()

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log line not JSON: %v\n%s", err, buf.String())
	}
	traceID, _ := rec["trace_id"].(string)
	spanID, _ := rec["span_id"].(string)
	if len(traceID) != 32 {
		t.Errorf("trace_id length: want 32, got %d (%q)", len(traceID), traceID)
	}
	if len(spanID) != 16 {
		t.Errorf("span_id length: want 16, got %d (%q)", len(spanID), spanID)
	}
}

func TestTraceHandler_NoIDsOutsideSpan(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(&traceHandler{
		inner: slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	})
	logger.Info("outside")

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log line not JSON: %v\n%s", err, buf.String())
	}
	if _, ok := rec["trace_id"]; ok {
		t.Errorf("trace_id should be absent outside span; got %v", rec["trace_id"])
	}
	if _, ok := rec["span_id"]; ok {
		t.Errorf("span_id should be absent outside span; got %v", rec["span_id"])
	}
}

func TestSetupTracing_NoopWhenEndpointUnset(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	shutdown, err := SetupTracing(context.Background())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	tpName := strings.ToLower(reflect.TypeOf(otel.GetTracerProvider()).String())
	if !strings.Contains(tpName, "noop") {
		t.Errorf("want noop TracerProvider, got %s", tpName)
	}
}
