package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/marz32one/graph-api-gateway/internal/client"
	"github.com/marz32one/graph-api-gateway/internal/config"
)

type stubCapture struct {
	traceparent string
	rawQuery    string
	called      bool
}

func newStub(t *testing.T, body string, status int, capture *stubCapture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture.called = true
			capture.traceparent = r.Header.Get("Traceparent")
			capture.rawQuery = r.URL.RawQuery
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestServer(t *testing.T, primaryURL, switchURL string, logBuf io.Writer) *Server {
	t.Helper()
	cfg := &config.Config{
		ListenAddr: ":0",
		LogLevel:   "debug",
		LogFormat:  "json",
		KSG:        config.Backend{BaseURL: primaryURL, Timeout: 2 * time.Second},
		Switch:     config.Backend{BaseURL: switchURL, Timeout: 2 * time.Second},
	}
	logger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	primary := client.NewKubeStateGraphClient(primaryURL, "", cfg.KSG.Timeout)
	switchClient := client.NewSwitchGraphClient(switchURL, "", cfg.Switch.Timeout)
	return New(cfg, logger, primary, switchClient)
}

// Primary response with a kube node that owns an IP, plus a pod (whose IP must NOT be forwarded).
const primaryWithIPs = `{
	"apiVersion": "v1",
	"elements": {
		"nodes": [
			{"data": {"id": "prod/abc", "type": "node", "ipaddress": ["10.0.0.1"]}},
			{"data": {"id": "prod/def", "type": "node", "ipaddress": ["10.0.0.2"]}},
			{"data": {"id": "prod/pod1", "type": "pod", "ipaddress": ["10.1.0.5"]}}
		],
		"edges": [
			{"data": {"id": "e-pod-node", "type": "pod-runs-on-node", "source": "prod/pod1", "target": "prod/abc"}}
		]
	}
}`

// Primary response with no IPs at all → switch should be skipped.
const primaryNoIPs = `{
	"apiVersion": "v1",
	"elements": {
		"nodes": [{"data": {"id": "prod/pvc1", "type": "pvc"}}],
		"edges": []
	}
}`

// Switch response: a shadow keyed on 10.0.0.1 plus a switch chassis and connecting edge.
const switchResponse = `{
	"apiVersion": "v1",
	"elements": {
		"nodes": [
			{"data": {"id": "sw-host:xyz", "type": "host", "ipaddress": ["10.0.0.1"]}},
			{"data": {"id": "switch:tor-1", "type": "switch"}}
		],
		"edges": [
			{"data": {"id": "e-sw", "type": "host-attached", "source": "sw-host:xyz", "target": "switch:tor-1"}}
		]
	}
}`

func TestGraphHandler_PrimaryAndSwitchSucceed_Reconciled(t *testing.T) {
	pCap, sCap := &stubCapture{}, &stubCapture{}
	primary := newStub(t, primaryWithIPs, 200, pCap)
	swSrv := newStub(t, switchResponse, 200, sCap)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph?start=t1&end=t2", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: want 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var got client.CytoscapeGraph
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}

	// Nodes: prod/abc, prod/def, prod/pod1 from primary; switch:tor-1 from switch (sw-host:xyz dropped via reconcile).
	ids := map[string]bool{}
	for _, n := range got.Elements.Nodes {
		ids[n.Data.ID] = true
	}
	for _, want := range []string{"prod/abc", "prod/def", "prod/pod1", "switch:tor-1"} {
		if !ids[want] {
			t.Errorf("missing node %s in merged output (got %v)", want, ids)
		}
	}
	if ids["sw-host:xyz"] {
		t.Errorf("switch shadow sw-host:xyz should have been collapsed, but appears in output")
	}

	// Edge from switch should point at kube node id, not the shadow id.
	var found bool
	for _, e := range got.Elements.Edges {
		if e.Data.Type == "host-attached" && e.Data.Source == "prod/abc" && e.Data.Target == "switch:tor-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("reconciled edge prod/abc → switch:tor-1 missing in output: %+v", got.Elements.Edges)
	}
}

func TestGraphHandler_SwitchReceivesDedupedIPQuery(t *testing.T) {
	sCap := &stubCapture{}
	primary := newStub(t, primaryWithIPs, 200, nil)
	swSrv := newStub(t, switchResponse, 200, sCap)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	if !sCap.called {
		t.Fatal("switch backend was not called")
	}
	parsed, err := url.ParseQuery(sCap.rawQuery)
	if err != nil {
		t.Fatalf("parse switch query: %v", err)
	}
	gotIPs := parsed["ip"]
	wantIPs := []string{"10.0.0.1", "10.0.0.2"}
	if len(gotIPs) != len(wantIPs) {
		t.Fatalf("switch ip count: want %d, got %d (%v)", len(wantIPs), len(gotIPs), gotIPs)
	}
	for i, ip := range wantIPs {
		if gotIPs[i] != ip {
			t.Errorf("ip[%d]: want %s, got %s", i, ip, gotIPs[i])
		}
	}
}

func TestGraphHandler_NoIPs_SwitchSkipped(t *testing.T) {
	sCap := &stubCapture{}
	primary := newStub(t, primaryNoIPs, 200, nil)
	swSrv := newStub(t, switchResponse, 200, sCap)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	if sCap.called {
		t.Errorf("switch backend SHOULD NOT have been called when no IPs are present")
	}
}

func TestGraphHandler_PrimaryFails_NoSwitchCall_502(t *testing.T) {
	sCap := &stubCapture{}
	primary := newStub(t, "boom", 500, nil)
	swSrv := newStub(t, switchResponse, 200, sCap)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 502 {
		t.Fatalf("status: want 502, got %d", w.Code)
	}
	if sCap.called {
		t.Errorf("switch backend SHOULD NOT have been called when primary fails")
	}
}

func TestGraphHandler_SwitchFails_502(t *testing.T) {
	primary := newStub(t, primaryWithIPs, 200, nil)
	swSrv := newStub(t, "boom", 500, nil)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 502 {
		t.Fatalf("status: want 502, got %d, body=%s", w.Code, w.Body.String())
	}
	var er errorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error != "upstream_unavailable" {
		t.Errorf("error: want upstream_unavailable, got %q", er.Error)
	}
}

func TestGraphHandler_InboundQueryForwardedToPrimaryOnly(t *testing.T) {
	pCap, sCap := &stubCapture{}, &stubCapture{}
	primary := newStub(t, primaryWithIPs, 200, pCap)
	swSrv := newStub(t, switchResponse, 200, sCap)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	const inboundQ = "cluster=prod&namespace=ns1&edge_type=pod-runs-on-node"
	req := httptest.NewRequest("GET", "/v1/graph?"+inboundQ, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	if pCap.rawQuery != inboundQ {
		t.Errorf("primary query: want %q, got %q", inboundQ, pCap.rawQuery)
	}
	if strings.Contains(sCap.rawQuery, "cluster=") || strings.Contains(sCap.rawQuery, "namespace=") || strings.Contains(sCap.rawQuery, "edge_type=") {
		t.Errorf("switch query should contain only ip= params, got %q", sCap.rawQuery)
	}
}

func TestGraphHandler_TraceparentChainedThroughBothBackends(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	pCap, sCap := &stubCapture{}, &stubCapture{}
	primary := newStub(t, primaryWithIPs, 200, pCap)
	swSrv := newStub(t, switchResponse, 200, sCap)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	const inboundTrace = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	req := httptest.NewRequest("GET", "/v1/graph", nil)
	req.Header.Set("Traceparent", inboundTrace)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	const wantTraceID = "0af7651916cd43dd8448eb211c80319c"
	if !strings.Contains(pCap.traceparent, wantTraceID) {
		t.Errorf("primary traceparent %q missing trace id %s", pCap.traceparent, wantTraceID)
	}
	if !strings.Contains(sCap.traceparent, wantTraceID) {
		t.Errorf("switch traceparent %q missing trace id %s", sCap.traceparent, wantTraceID)
	}
}

func TestGraphHandler_RequestIDEchoedAndLogged(t *testing.T) {
	primary := newStub(t, primaryWithIPs, 200, nil)
	swSrv := newStub(t, switchResponse, 200, nil)
	var logBuf bytes.Buffer
	s := newTestServer(t, primary.URL, swSrv.URL, &logBuf)

	const inboundID = "abc-123"
	req := httptest.NewRequest("GET", "/v1/graph", nil)
	req.Header.Set("X-Request-ID", inboundID)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != inboundID {
		t.Errorf("X-Request-ID echo: want %q, got %q", inboundID, got)
	}
	if !strings.Contains(logBuf.String(), inboundID) {
		t.Errorf("log missing request_id %q: %s", inboundID, logBuf.String())
	}
}

func TestRequestIDMiddleware_GeneratesWhenMissing(t *testing.T) {
	primary := newStub(t, primaryWithIPs, 200, nil)
	swSrv := newStub(t, switchResponse, 200, nil)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	got := w.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("X-Request-ID header missing on response")
	}
	if len(got) < 16 {
		t.Errorf("generated id too short: %q", got)
	}
}

func TestValidRequestID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"empty", "", false},
		{"simple uuid", "550e8400-e29b-41d4-a716-446655440000", true},
		{"safe chars", "abc_DEF.123+xy:zz-", true},
		{"with space", "abc def", false},
		{"with newline", "abc\ndef", false},
		{"with CR", "abc\rdef", false},
		{"with NUL", "abc\x00def", false},
		{"with slash", "abc/def", false},
		{"at limit", strings.Repeat("a", 128), true},
		{"over limit", strings.Repeat("a", 129), false},
	}
	for _, tc := range cases {
		if got := validRequestID(tc.in); got != tc.ok {
			t.Errorf("%s: validRequestID(%q) = %v, want %v", tc.name, tc.in, got, tc.ok)
		}
	}
}

func TestRequestIDMiddleware_RejectsMalformedInbound(t *testing.T) {
	primary := newStub(t, primaryWithIPs, 200, nil)
	swSrv := newStub(t, switchResponse, 200, nil)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	req := httptest.NewRequest("GET", "/v1/graph", nil)
	req.Header.Set("X-Request-ID", "bad id with space")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	got := w.Header().Get("X-Request-ID")
	if got == "bad id with space" {
		t.Fatalf("invalid inbound id should not be echoed, got %q", got)
	}
	if len(got) < 16 {
		t.Errorf("generated fallback id too short: %q", got)
	}
}

func TestReadyz_BackendUnreachable(t *testing.T) {
	// primary stub is up; switch URL points at an unroutable address so its
	// probe must fail within the 1.5s probe budget.
	primary := newStub(t, primaryWithIPs, 200, nil)
	s := newTestServer(t, primary.URL, "http://127.0.0.1:1", io.Discard)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "switch") {
		t.Errorf("body should name the failed backend, got %q", w.Body.String())
	}
}

func TestHealth(t *testing.T) {
	primary := newStub(t, primaryWithIPs, 200, nil)
	swSrv := newStub(t, switchResponse, 200, nil)
	s := newTestServer(t, primary.URL, swSrv.URL, io.Discard)

	for _, path := range []string{"/livez", "/readyz"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "ok" {
			t.Errorf("%s: want 200/ok, got %d/%q", path, w.Code, w.Body.String())
		}
	}
}

func TestBuildIPQuery(t *testing.T) {
	got := buildIPQuery([]string{"10.0.0.1", "10.0.0.2"})
	parsed, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"10.0.0.1", "10.0.0.2"}
	if len(parsed["ip"]) != 2 || parsed["ip"][0] != want[0] || parsed["ip"][1] != want[1] {
		t.Errorf("want ip=%v, got %v (raw %q)", want, parsed["ip"], got)
	}
}
