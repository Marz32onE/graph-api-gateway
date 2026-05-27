package merge

import (
	"reflect"
	"testing"

	"github.com/marz32one/graph-api-gateway/internal/client"
)

func n(id, typ string) client.Node {
	return client.Node{Data: client.NodeData{ID: id, Type: typ}}
}
func e(typ, src, tgt string) client.Edge { //nolint:unparam // typ is varied in future cases
	return client.Edge{Data: client.EdgeData{Type: typ, Source: src, Target: tgt}}
}
func g(nodes []client.Node, edges []client.Edge, clusters ...string) *client.CytoscapeGraph {
	return &client.CytoscapeGraph{
		APIVersion: "v1",
		Clusters:   append([]string(nil), clusters...),
		Elements:   client.Elements{Nodes: nodes, Edges: edges},
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name      string
		inputs    []*client.CytoscapeGraph
		wantNodes []client.Node
		wantEdges []client.Edge
	}{
		{
			name: "disjoint union",
			inputs: []*client.CytoscapeGraph{
				g([]client.Node{n("a", "pod")}, []client.Edge{e("t", "a", "b")}),
				g([]client.Node{n("b", "node")}, []client.Edge{e("t", "b", "c")}),
			},
			wantNodes: []client.Node{n("a", "pod"), n("b", "node")},
			wantEdges: []client.Edge{e("t", "a", "b"), e("t", "b", "c")},
		},
		{
			name: "node id collision resolves to first writer",
			inputs: []*client.CytoscapeGraph{
				g([]client.Node{n("x", "pod")}, nil),
				g([]client.Node{n("x", "node")}, nil),
			},
			wantNodes: []client.Node{n("x", "pod")},
			wantEdges: []client.Edge{},
		},
		{
			name: "edge triple collision collapses",
			inputs: []*client.CytoscapeGraph{
				g(nil, []client.Edge{e("t", "a", "b")}),
				g(nil, []client.Edge{e("t", "a", "b")}),
			},
			wantNodes: []client.Node{},
			wantEdges: []client.Edge{e("t", "a", "b")},
		},
		{
			name: "distinct edge triples preserved",
			inputs: []*client.CytoscapeGraph{
				g(nil, []client.Edge{e("t", "a", "b"), e("t", "b", "c")}),
			},
			wantNodes: []client.Node{},
			wantEdges: []client.Edge{e("t", "a", "b"), e("t", "b", "c")},
		},
		{
			name:      "nil inputs are skipped",
			inputs:    []*client.CytoscapeGraph{nil, g([]client.Node{n("a", "pod")}, nil), nil},
			wantNodes: []client.Node{n("a", "pod")},
			wantEdges: []client.Edge{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := Merge(tc.inputs...)
			if out.APIVersion != "v1" {
				t.Errorf("apiVersion: want v1, got %q", out.APIVersion)
			}
			if !reflect.DeepEqual(out.Elements.Nodes, tc.wantNodes) {
				t.Errorf("nodes: want %+v, got %+v", tc.wantNodes, out.Elements.Nodes)
			}
			if !reflect.DeepEqual(out.Elements.Edges, tc.wantEdges) {
				t.Errorf("edges: want %+v, got %+v", tc.wantEdges, out.Elements.Edges)
			}
		})
	}
}

func TestMerge_InputsNotMutated(t *testing.T) {
	a := g([]client.Node{n("x", "pod")}, []client.Edge{e("t", "x", "y")})
	b := g([]client.Node{n("x", "node")}, []client.Edge{e("t", "x", "y")})

	beforeA := *a
	beforeAElements := a.Elements
	beforeB := *b
	beforeBElements := b.Elements

	_ = Merge(a, b)

	if !reflect.DeepEqual(*a, beforeA) || !reflect.DeepEqual(a.Elements, beforeAElements) {
		t.Errorf("input a mutated")
	}
	if !reflect.DeepEqual(*b, beforeB) || !reflect.DeepEqual(b.Elements, beforeBElements) {
		t.Errorf("input b mutated")
	}
}

func TestMerge_ClustersUnioned(t *testing.T) {
	out := Merge(
		g(nil, nil, "alpha", "beta"),
		g(nil, nil, "beta", "gamma"),
	)
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(out.Clusters, want) {
		t.Errorf("clusters: want %v, got %v", want, out.Clusters)
	}
}

func TestMerge_NoInputs(t *testing.T) {
	out := Merge()
	if out.APIVersion != "v1" || len(out.Elements.Nodes) != 0 || len(out.Elements.Edges) != 0 {
		t.Errorf("empty merge: %+v", out)
	}
}
