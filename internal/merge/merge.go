// Package merge unions multiple Cytoscape graph responses into one.
package merge

import (
	"sort"

	"github.com/marz32one/graph-api-gateway/internal/client"
)

// Merge unions any number of Cytoscape graphs into a single envelope.
//
// Nodes are unioned by data.id; on collision the first writer wins.
// Edges are deduplicated by the (type, source, target) triple; on collision
// the first writer wins. Inputs are not mutated; the returned graph is fresh.
// edgeKey deduplicates edges by the (type, source, target) triple without
// risking string-concat collisions when any field contains a separator char.
type edgeKey struct {
	Type, Source, Target string
}

func Merge(graphs ...*client.CytoscapeGraph) *client.CytoscapeGraph {
	nodeIdx := map[string]int{}
	edgeIdx := map[edgeKey]int{}
	out := &client.CytoscapeGraph{
		APIVersion: "v1",
		Elements: client.Elements{
			Nodes: []client.Node{},
			Edges: []client.Edge{},
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
			if _, dup := nodeIdx[id]; dup {
				continue
			}
			nodeIdx[id] = len(out.Elements.Nodes)
			out.Elements.Nodes = append(out.Elements.Nodes, n)
		}
		for _, e := range g.Elements.Edges {
			key := edgeKey{Type: e.Data.Type, Source: e.Data.Source, Target: e.Data.Target}
			if _, dup := edgeIdx[key]; dup {
				continue
			}
			edgeIdx[key] = len(out.Elements.Edges)
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
