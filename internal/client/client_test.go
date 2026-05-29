package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestKubeStateGraphClient_HappyPath(t *testing.T) {
	var gotAPIKey, gotTraceparent, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/graph" {
			t.Errorf("path: want /v1/graph, got %s", r.URL.Path)
		}
		gotAPIKey = r.Header.Get("X-API-Key")
		gotTraceparent = r.Header.Get("Traceparent")
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"apiVersion": "v1",
			"elements": {
				"nodes": [{"data": {"id": "n1", "type": "pod"}}],
				"edges": []
			}
		}`))
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "secret-1", 5*time.Second)
	g, err := c.FetchGraph(context.Background(), GraphQuery{RawQuery: "start=a&end=b"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if g.APIVersion != "v1" || len(g.Elements.Nodes) != 1 || g.Elements.Nodes[0].Data.ID != "n1" {
		t.Errorf("decoded payload mismatch: %+v", g)
	}
	if gotAPIKey != "secret-1" {
		t.Errorf("X-API-Key: want secret-1, got %q", gotAPIKey)
	}
	if gotQuery != "start=a&end=b" {
		t.Errorf("query: want start=a&end=b, got %q", gotQuery)
	}
	_ = gotTraceparent // tested in TraceContext test
}

func TestClient_NoAPIKeyWhenEmpty(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","elements":{"nodes":[],"edges":[]}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "", 5*time.Second)
	if _, err := c.FetchGraph(context.Background(), GraphQuery{}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotKey != "" {
		t.Errorf("X-API-Key should be absent, got %q", gotKey)
	}
}

func TestClient_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"apiVersion":`))
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "", 5*time.Second)
	_, err := c.FetchGraph(context.Background(), GraphQuery{})
	if err == nil {
		t.Fatal("want error on malformed JSON, got nil")
	}
}

func TestClient_MissingElementsBecomesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"apiVersion":"v1","elements":{}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "", 5*time.Second)
	g, err := c.FetchGraph(context.Background(), GraphQuery{})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(g.Elements.Nodes) != 0 || len(g.Elements.Edges) != 0 {
		t.Errorf("want empty slices, got %+v", g.Elements)
	}
	if g.Elements.Nodes == nil || g.Elements.Edges == nil {
		t.Error("slices must be non-nil")
	}
}

func TestClient_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "", 5*time.Second)
	_, err := c.FetchGraph(context.Background(), GraphQuery{})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want 500 in error, got %v", err)
	}
}

func TestClient_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"apiVersion":"v1","elements":{"nodes":[],"edges":[]}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "", 20*time.Millisecond)
	start := time.Now()
	_, err := c.FetchGraph(context.Background(), GraphQuery{})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("timeout took too long: %s", elapsed)
	}
}

func TestClient_PropagatesTraceparent(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	var gotTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("Traceparent")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","elements":{"nodes":[],"edges":[]}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "", 5*time.Second)
	ctx, span := tp.Tracer("test").Start(context.Background(), "parent")
	defer span.End()

	if _, err := c.FetchGraph(ctx, GraphQuery{}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotTraceparent == "" {
		t.Fatal("want traceparent header on outbound request")
	}
	parentTrace := span.SpanContext().TraceID().String()
	if !strings.Contains(gotTraceparent, parentTrace) {
		t.Errorf("traceparent %q does not contain trace id %s", gotTraceparent, parentTrace)
	}
}

func TestSwitchClient_PostsIPBody(t *testing.T) {
	var (
		gotMethod, gotPath, gotCT, gotAPIKey string
		gotBody                              []map[string]string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotAPIKey = r.Header.Get("X-API-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","elements":{"nodes":[],"edges":[]}}`))
	}))
	t.Cleanup(srv.Close)

	c := NewSwitchGraphClient(srv.URL, "switch-key", 5*time.Second)
	if _, err := c.FetchGraphByIPs(context.Background(), []string{"10.0.0.1", "10.0.0.2"}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method: want POST, got %s", gotMethod)
	}
	if gotPath != "/v1/graph" {
		t.Errorf("path: want /v1/graph, got %s", gotPath)
	}
	if !strings.Contains(gotCT, "application/json") {
		t.Errorf("content-type: want application/json, got %q", gotCT)
	}
	if gotAPIKey != "switch-key" {
		t.Errorf("X-API-Key: want switch-key, got %q", gotAPIKey)
	}
	want := []map[string]string{{"ip": "10.0.0.1"}, {"ip": "10.0.0.2"}}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body: want %v, got %v", want, gotBody)
	}
}

func TestNodeData_IPAddressDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"apiVersion":"v1",
			"elements":{
				"nodes":[
					{"data":{"id":"a","type":"node","ipaddress":["10.0.0.1","10.0.0.2"]}},
					{"data":{"id":"b","type":"node"}}
				],
				"edges":[]
			}
		}`))
	}))
	t.Cleanup(srv.Close)

	c := NewKubeStateGraphClient(srv.URL, "", 5*time.Second)
	g, err := c.FetchGraph(context.Background(), GraphQuery{})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := g.Elements.Nodes[0].Data.IPAddress; len(got) != 2 || got[0] != "10.0.0.1" || got[1] != "10.0.0.2" {
		t.Errorf("ipaddress: want [10.0.0.1 10.0.0.2], got %v", got)
	}
	if g.Elements.Nodes[1].Data.IPAddress != nil {
		t.Errorf("missing ipaddress should decode as nil, got %v", g.Elements.Nodes[1].Data.IPAddress)
	}
}

// Compile-time guard: make sure CytoscapeGraph round-trips through json.
func TestCytoscapeGraph_Roundtrip(t *testing.T) {
	in := &CytoscapeGraph{
		APIVersion: "v1",
		Elements: Elements{
			Nodes: []Node{{Data: NodeData{ID: "a", Type: "pod"}}},
			Edges: []Edge{{Data: EdgeData{Type: "t", Source: "a", Target: "b"}}},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out := &CytoscapeGraph{}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
	if out.Elements.Nodes[0].Data.ID != "a" || out.Elements.Edges[0].Data.Source != "a" {
		t.Errorf("roundtrip mismatch: %+v", out)
	}
}
