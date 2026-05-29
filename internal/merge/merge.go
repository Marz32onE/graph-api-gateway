// Package merge unions multiple Cytoscape graph responses into one.
package merge

import (
	"sort"

	"github.com/marz32one/graph-api-gateway/internal/client"
)

// edgeKey deduplicates edges by the (type, source, target) triple without
// risking string-concat collisions when any field contains a separator char.
type edgeKey struct {
	Type, Source, Target string
}

// Merge unions any number of Cytoscape graphs into a single envelope.
//
// Nodes are unioned by data.id; on collision the first writer wins.
// Edges are deduplicated by the (type, source, target) triple; on collision
// the first writer wins. Inputs are not mutated; the returned graph is fresh.
func Merge(graphs ...*client.CytoscapeGraph) *client.CytoscapeGraph {
	// Output is bounded by the total input size (dedup only shrinks it), so
	// size the dedup sets and output slices up front to avoid regrowth.
	var totalNodes, totalEdges int
	for _, g := range graphs {
		if g != nil {
			totalNodes += len(g.Elements.Nodes)
			totalEdges += len(g.Elements.Edges)
		}
	}

	nodeSeen := make(map[string]struct{}, totalNodes)
	edgeSeen := make(map[edgeKey]struct{}, totalEdges)
	out := &client.CytoscapeGraph{
		APIVersion: "v1",
		Elements: client.Elements{
			Nodes: make([]client.Node, 0, totalNodes),
			Edges: make([]client.Edge, 0, totalEdges),
		},
	}
	clusters := map[string]struct{}{}

	for _, g := range graphs {
		if g == nil {
			continue
		}
		for _, c := range g.Clusters {
			clusters[c] = struct{}{}
		}
		for _, n := range g.Elements.Nodes {
			id := n.Data.ID
			if _, dup := nodeSeen[id]; dup {
				continue
			}
			nodeSeen[id] = struct{}{}
			out.Elements.Nodes = append(out.Elements.Nodes, n)
		}
		for _, e := range g.Elements.Edges {
			key := edgeKey{Type: e.Data.Type, Source: e.Data.Source, Target: e.Data.Target}
			if _, dup := edgeSeen[key]; dup {
				continue
			}
			edgeSeen[key] = struct{}{}
			out.Elements.Edges = append(out.Elements.Edges, e)
		}
	}

	if len(clusters) > 0 {
		out.Clusters = make([]string, 0, len(clusters))
		for c := range clusters {
			out.Clusters = append(out.Clusters, c)
		}
		sort.Strings(out.Clusters)
	}
	return out
}
