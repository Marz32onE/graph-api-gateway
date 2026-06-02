package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A per-stage deadline being exceeded (context.DeadlineExceeded from the primary
// build) must surface as 504 with the {"error":"upstream_timeout"} envelope —
// distinct from a generic 502 upstream failure.
func TestGraphHandler_PrimaryDeadlineExceeded_504(t *testing.T) {
	swSrv := newStub(t, switchResponse, 200, nil)
	s := newTestServer(t, &fakeKSG{err: context.DeadlineExceeded}, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status: want 504, got %d, body=%s", w.Code, w.Body.String())
	}
	var er errorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &er); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	if er.Error != "upstream_timeout" {
		t.Errorf("error: want upstream_timeout, got %q", er.Error)
	}
}

// When the inbound request context is already cancelled, writeFetchError checks
// errors.Is(ctx.Err(), context.Canceled) before the deadline/502 branches and
// aborts with 499 — there is no peer to respond to, so no body is written.
func TestGraphHandler_InboundClientCancel_499(t *testing.T) {
	swSrv := newStub(t, switchResponse, 200, nil)
	s := newTestServer(t, &fakeKSG{err: context.Canceled}, swSrv.URL, io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // inbound request already gone before the handler runs

	req := httptest.NewRequest("GET", "/v1/graph", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 499 {
		t.Fatalf("status: want 499, got %d, body=%s", w.Code, w.Body.String())
	}
}
