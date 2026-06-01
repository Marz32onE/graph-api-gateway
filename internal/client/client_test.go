package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// newSwitchTo wires a SwitchGraphClient at an httptest server with the given
// handler. The switch backend is the only HTTP path now, so it also exercises
// the shared decode logic in transport.go.
func newSwitchTo(t *testing.T, apiKey string, timeout time.Duration, h http.HandlerFunc) *SwitchGraphClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewSwitchGraphClient(srv.URL, apiKey, timeout)
}

func respond(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

func TestSwitchClient_PostsIPBody(t *testing.T) {
	var (
		gotMethod, gotPath, gotCT, gotAPIKey string
		gotBody                              []map[string]string
	)
	c := newSwitchTo(t, "switch-key", 5*time.Second, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotAPIKey = r.Header.Get("X-API-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"apiVersion":"v1","elements":{"nodes":[],"edges":[]}}`)
	})

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

func TestSwitchClient_NoAPIKeyWhenEmpty(t *testing.T) {
	var gotKey string
	c := newSwitchTo(t, "", 5*time.Second, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		_, _ = io.WriteString(w, `{"apiVersion":"v1","elements":{"nodes":[],"edges":[]}}`)
	})
	if _, err := c.FetchGraphByIPs(context.Background(), nil); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotKey != "" {
		t.Errorf("X-API-Key should be absent, got %q", gotKey)
	}
}

func TestSwitchClient_MalformedJSON(t *testing.T) {
	c := newSwitchTo(t, "", 5*time.Second, respond(`{"apiVersion":`))
	if _, err := c.FetchGraphByIPs(context.Background(), nil); err == nil {
		t.Fatal("want error on malformed JSON, got nil")
	}
}

func TestSwitchClient_MissingElementsBecomeEmpty(t *testing.T) {
	c := newSwitchTo(t, "", 5*time.Second, respond(`{"apiVersion":"v1","elements":{}}`))
	g, err := c.FetchGraphByIPs(context.Background(), nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if g.Elements.Nodes == nil || g.Elements.Edges == nil {
		t.Error("missing slices must decode as non-nil empty")
	}
}

func TestSwitchClient_NonOK(t *testing.T) {
	c := newSwitchTo(t, "", 5*time.Second, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.FetchGraphByIPs(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want 500 in error, got %v", err)
	}
}

func TestSwitchClient_Timeout(t *testing.T) {
	c := newSwitchTo(t, "", 20*time.Millisecond, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `{"apiVersion":"v1","elements":{"nodes":[],"edges":[]}}`)
	})
	start := time.Now()
	_, err := c.FetchGraphByIPs(context.Background(), nil)
	if err == nil {
		t.Fatal("want timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("timeout took too long: %s", elapsed)
	}
}

// Compound nodes (kube-state-graph design.md D31): the upstream emits synthetic
// `cluster/<name>` group nodes (type "cluster", no ipaddress) plus a data.parent
// reference on real nodes. The decode must retain both, and a re-serialise must
// round-trip parent while omitting empty ones.
func TestSwitchClient_DecodesCompoundAndIPAddress(t *testing.T) {
	c := newSwitchTo(t, "", 5*time.Second, respond(`{
		"apiVersion":"v1",
		"elements":{
			"nodes":[
				{"data":{"id":"cluster/prod","name":"prod","type":"cluster"}},
				{"data":{"id":"node-1","type":"node","parent":"cluster/prod","ipaddress":["10.0.0.1","10.0.0.2"]}},
				{"data":{"id":"pod-1","type":"pod","parent":"node-1"}}
			],
			"edges":[]
		}
	}`))
	g, err := c.FetchGraphByIPs(context.Background(), nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	nodes := g.Elements.Nodes
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}
	if nodes[0].Data.Type != "cluster" || nodes[0].Data.Parent != "" || nodes[0].Data.IPAddress != nil {
		t.Errorf("cluster group node decoded wrong: %+v", nodes[0].Data)
	}
	if got := nodes[1].Data.IPAddress; len(got) != 2 || got[0] != "10.0.0.1" || got[1] != "10.0.0.2" {
		t.Errorf("ipaddress: want [10.0.0.1 10.0.0.2], got %v", got)
	}
	if nodes[1].Data.Parent != "cluster/prod" || nodes[2].Data.Parent != "node-1" {
		t.Errorf("parents decoded wrong: %q, %q", nodes[1].Data.Parent, nodes[2].Data.Parent)
	}

	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"parent":"cluster/prod"`) || !strings.Contains(s, `"parent":"node-1"`) {
		t.Errorf("re-serialised JSON dropped parent: %s", s)
	}
	if strings.Contains(s, `"parent":""`) {
		t.Errorf("empty parent should be omitted, got: %s", s)
	}
}

// The in-process kube-state-graph adapter rejects an invalid request via the
// shared kubegraph.ParseValues before any upstream query is attempted.
func TestKubeStateGraphClient_ParseErrorShortCircuits(t *testing.T) {
	c, err := NewKubeStateGraphClient("http://localhost:8428", "", time.Second)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	// Empty query → missing start/end → parse error, no upstream call.
	if _, err := c.FetchGraph(context.Background(), GraphQuery{}); err == nil {
		t.Fatal("want parse error for empty query, got nil")
	}
}
