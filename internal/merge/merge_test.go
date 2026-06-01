package merge

import (
	"reflect"
	"testing"

	"github.com/marz32one/kube-state-graph/pkg/cytoscape"
)

func n(id, typ string) cytoscape.Node {
	return cytoscape.Node{Data: cytoscape.NodeData{ID: id, Type: typ}}
}
func e(typ, src, tgt string) cytoscape.Edge { //nolint:unparam // typ is varied in future cases
	return cytoscape.Edge{Data: cytoscape.EdgeData{Type: typ, Source: src, Target: tgt}}
}
func g(nodes []cytoscape.Node, edges []cytoscape.Edge, clusters ...string) *cytoscape.Body {
	return &cytoscape.Body{
		APIVersion: "v1",
		Clusters:   append([]string(nil), clusters...),
		Elements:   cytoscape.Elements{Nodes: nodes, Edges: edges},
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name      string
		inputs    []*cytoscape.Body
		wantNodes []cytoscape.Node
		wantEdges []cytoscape.Edge
	}{
		{
			name: "disjoint union",
			inputs: []*cytoscape.Body{
				g([]cytoscape.Node{n("a", "pod")}, []cytoscape.Edge{e("t", "a", "b")}),
				g([]cytoscape.Node{n("b", "node")}, []cytoscape.Edge{e("t", "b", "c")}),
			},
			wantNodes: []cytoscape.Node{n("a", "pod"), n("b", "node")},
			wantEdges: []cytoscape.Edge{e("t", "a", "b"), e("t", "b", "c")},
		},
		{
			name: "node id collision resolves to first writer",
			inputs: []*cytoscape.Body{
				g([]cytoscape.Node{n("x", "pod")}, nil),
				g([]cytoscape.Node{n("x", "node")}, nil),
			},
			wantNodes: []cytoscape.Node{n("x", "pod")},
			wantEdges: []cytoscape.Edge{},
		},
		{
			name: "edge triple collision collapses",
			inputs: []*cytoscape.Body{
				g(nil, []cytoscape.Edge{e("t", "a", "b")}),
				g(nil, []cytoscape.Edge{e("t", "a", "b")}),
			},
			wantNodes: []cytoscape.Node{},
			wantEdges: []cytoscape.Edge{e("t", "a", "b")},
		},
		{
			name: "distinct edge triples preserved",
			inputs: []*cytoscape.Body{
				g(nil, []cytoscape.Edge{e("t", "a", "b"), e("t", "b", "c")}),
			},
			wantNodes: []cytoscape.Node{},
			wantEdges: []cytoscape.Edge{e("t", "a", "b"), e("t", "b", "c")},
		},
		{
			name:      "nil inputs are skipped",
			inputs:    []*cytoscape.Body{nil, g([]cytoscape.Node{n("a", "pod")}, nil), nil},
			wantNodes: []cytoscape.Node{n("a", "pod")},
			wantEdges: []cytoscape.Edge{},
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
	a := g([]cytoscape.Node{n("x", "pod")}, []cytoscape.Edge{e("t", "x", "y")})
	b := g([]cytoscape.Node{n("x", "node")}, []cytoscape.Edge{e("t", "x", "y")})

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
