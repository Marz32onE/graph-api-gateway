package client

import (
	"context"
	"strings"
	"testing"
	"time"
)

// NewKubeStateGraphClient with a reachable-looking vmURL constructs a usable
// engine adapter offline: promql.New only parses the address, it does not dial,
// so a syntactically valid URL yields a non-nil client and no error. The
// returned *KubeStateGraphClient also satisfies the GraphBackend contract
// (asserted at compile time in the source via the package-level var _).
func TestNewKubeStateGraphClient_ValidURLConstructsBackend(t *testing.T) {
	c, err := NewKubeStateGraphClient("http://localhost:8428", "", time.Second)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if c == nil {
		t.Fatal("want non-nil client, got nil")
	}
	// Pin the GraphBackend contract for an instance (the source asserts the
	// type statically; this proves the constructed value is usable as one).
	var _ GraphBackend = c
}

// FetchGraph rejects an un-decodable RawQuery before any upstream work: an
// invalid percent-escape makes url.ParseQuery fail, so FetchGraph returns the
// wrapped "parse query" error without ever building the graph or dialing
// VictoriaMetrics (no VM is running in this test).
func TestFetchGraph_InvalidEscapingReturnsParseQueryError(t *testing.T) {
	c, err := NewKubeStateGraphClient("http://localhost:8428", "", time.Second)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	_, err = c.FetchGraph(context.Background(), GraphQuery{RawQuery: "%zz"})
	if err == nil {
		t.Fatal("want parse error for invalid escaping, got nil")
	}
	if !strings.Contains(err.Error(), "parse query") {
		t.Fatalf("want error to mention \"parse query\", got %v", err)
	}
}

// NewKubeStateGraphClient surfaces an engine-construction failure: a vmURL that
// promql.New cannot parse ("://bad" has no scheme) is wrapped with the
// "kube-state-graph engine" context, and no client is returned. This is fully
// offline — promql.New parses the address eagerly without dialing.
func TestNewKubeStateGraphClient_InvalidURLFailsEngine(t *testing.T) {
	c, err := NewKubeStateGraphClient("://bad", "", time.Second)
	if err == nil {
		t.Fatal("want engine error for unparseable vmURL, got nil")
	}
	if c != nil {
		t.Errorf("want nil client on error, got %+v", c)
	}
	if !strings.Contains(err.Error(), "kube-state-graph engine") {
		t.Fatalf("want error to mention \"kube-state-graph engine\", got %v", err)
	}
}
