// Package client wraps the upstream Cytoscape graph backends.
package client

import "context"

// GraphBackend is the contract implemented by every concrete client.
type GraphBackend interface {
	FetchGraph(ctx context.Context, q GraphQuery) (*CytoscapeGraph, error)
}

// GraphQuery is the raw, already-encoded query string to forward to the backend.
// Callers MUST pass the URL-encoded form (no leading `?`).
type GraphQuery struct {
	RawQuery string
}

// CytoscapeGraph is the standard Cytoscape.js envelope returned by the kube-state-graph
// /v1/graph contract.
type CytoscapeGraph struct {
	APIVersion string   `json:"apiVersion"`
	Clusters   []string `json:"clusters,omitempty"`
	Elements   Elements `json:"elements"`
}

// Elements holds the typed node and edge slices.
type Elements struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Node is a Cytoscape.js node.
type Node struct {
	Data NodeData `json:"data"`
}

// NodeData carries the addressable node attributes.
//
// IPAddress is the cross-graph join key shared between kube-state-graph (which
// populates it on node/pod entries) and the switch backend (which populates it
// on endpoint-shadow entries). The gateway reconciles switch IDs onto kube IDs
// by matching this field.
type NodeData struct {
	ID        string            `json:"id"`
	Name      string            `json:"name,omitempty"`
	Type      string            `json:"type,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	IPAddress []string          `json:"ipaddress,omitempty"`
}

// Edge is a Cytoscape.js edge.
type Edge struct {
	Data EdgeData `json:"data"`
}

// EdgeData carries the addressable edge attributes.
type EdgeData struct {
	ID     string            `json:"id,omitempty"`
	Type   string            `json:"type"`
	Source string            `json:"source"`
	Target string            `json:"target"`
	Labels map[string]string `json:"labels,omitempty"`
}
