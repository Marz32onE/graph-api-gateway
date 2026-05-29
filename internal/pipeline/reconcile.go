package pipeline

import (
	"slices"

	"github.com/marz32one/graph-api-gateway/internal/client"
)

// ReconcileSwitch re-anchors the switch graph onto primary's K8s node IDs by
// matching shared data.ipaddress values. See design.md D9.
//
// Steps:
//  1. Index every IP on primary node-type entries → kube node ID.
//  2. Walk switch nodes; any with at least one matching IP is a "shadow" of
//     the corresponding kube node — record a switchID → kubeID rewrite.
//  3. Return a new graph that (a) drops the collapsed shadow nodes, (b) keeps
//     all other switch nodes as-is, (c) rewrites edge source/target via the
//     map.
//
// Pure: inputs are never mutated; the returned graph holds value copies.
// Returns nil when switchGraph is nil.
func ReconcileSwitch(primary, switchGraph *client.CytoscapeGraph) *client.CytoscapeGraph {
	if switchGraph == nil {
		return nil
	}

	primaryNodes := 0
	if primary != nil {
		primaryNodes = len(primary.Elements.Nodes)
	}
	ipToNodeID := make(map[string]string, primaryNodes)
	iterNodeIPs(primary, func(nodeID, ip string) {
		if _, dup := ipToNodeID[ip]; !dup {
			ipToNodeID[ip] = nodeID
		}
	})

	rewrite := make(map[string]string, len(switchGraph.Elements.Nodes))
	for _, n := range switchGraph.Elements.Nodes {
		for _, ip := range n.Data.IPAddress {
			if kid, ok := ipToNodeID[ip]; ok {
				rewrite[n.Data.ID] = kid
				break
			}
		}
	}

	// Clusters are passed through so Merge can union switch-only clusters with
	// the kube side. slices.Clone keeps the "inputs are not mutated" contract.
	clusters := slices.Clone(switchGraph.Clusters)
	out := &client.CytoscapeGraph{
		APIVersion: switchGraph.APIVersion,
		Clusters:   clusters,
		Elements: client.Elements{
			Nodes: make([]client.Node, 0, len(switchGraph.Elements.Nodes)),
			Edges: make([]client.Edge, 0, len(switchGraph.Elements.Edges)),
		},
	}
	for _, n := range switchGraph.Elements.Nodes {
		if _, dropped := rewrite[n.Data.ID]; dropped {
			continue
		}
		out.Elements.Nodes = append(out.Elements.Nodes, n)
	}
	for _, e := range switchGraph.Elements.Edges {
		copyE := e
		origSrc, origTgt := copyE.Data.Source, copyE.Data.Target
		if kid, ok := rewrite[origSrc]; ok {
			copyE.Data.Source = kid
		}
		if kid, ok := rewrite[origTgt]; ok {
			copyE.Data.Target = kid
		}
		// Skip self-loops introduced by rewrite collisions: two switch shadows
		// that both map to the same kube node would otherwise produce a bogus
		// kube→kube self-edge that did not exist in either input.
		if copyE.Data.Source == copyE.Data.Target && origSrc != origTgt {
			continue
		}
		out.Elements.Edges = append(out.Elements.Edges, copyE)
	}
	return out
}
