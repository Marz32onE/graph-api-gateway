// Package client wraps the upstream graph backends: the in-process
// kube-state-graph engine (primary) and the HTTP switch backend. Both speak the
// shared pkg/cytoscape DTO.
package client

import (
	"context"

	"github.com/marz32one/kube-state-graph/pkg/cytoscape"
)

// GraphBackend is the primary (kube-state-graph) contract: build the kube graph
// for an inbound query and report upstream reachability. It is satisfied by
// KubeStateGraphClient, the in-process engine adapter. The switch backend is
// queried differently (a POST/IP body via SwitchGraphClient.FetchGraphByIPs) and
// is deliberately not a GraphBackend.
type GraphBackend interface {
	FetchGraph(ctx context.Context, q GraphQuery) (*cytoscape.Body, error)
	Probe(ctx context.Context) error
}

// GraphQuery is the inbound URL-encoded query string forwarded to the primary
// (no leading '?'). The engine parses it via kubegraph.ParseValues.
type GraphQuery struct {
	RawQuery string
}
