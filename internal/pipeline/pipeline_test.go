package pipeline

import (
	"reflect"
	"testing"

	"github.com/marz32one/graph-api-gateway/internal/client"
)

func node(id, typ string, ips ...string) client.Node {
	return client.Node{Data: client.NodeData{ID: id, Type: typ, IPAddress: ips}}
}
func edge(typ, src, tgt string) client.Edge {
	return client.Edge{Data: client.EdgeData{Type: typ, Source: src, Target: tgt}}
}
func graph(nodes []client.Node, edges []client.Edge) *client.CytoscapeGraph {
	return &client.CytoscapeGraph{
		APIVersion: "v1",
		Elements:   client.Elements{Nodes: nodes, Edges: edges},
	}
}

func TestExtractIPs(t *testing.T) {
	tests := []struct {
		name string
		in   *client.CytoscapeGraph
		want []string
	}{
		{
			name: "nil input returns empty",
			in:   nil,
			want: []string{},
		},
		{
			name: "node-only collection",
			in: graph([]client.Node{
				node("n1", "node", "10.0.0.1", "10.0.0.2"),
				node("p1", "pod", "10.1.0.5"),
				node("v1", "pvc", "192.168.0.1"),
				node("e1", "external", "8.8.8.8"),
			}, nil),
			want: []string{"10.0.0.1", "10.0.0.2"},
		},
		{
			name: "duplicates across multiple nodes collapse",
			in: graph([]client.Node{
				node("n1", "node", "10.0.0.1"),
				node("n2", "node", "10.0.0.1", "10.0.0.2"),
			}, nil),
			want: []string{"10.0.0.1", "10.0.0.2"},
		},
		{
			name: "empty/nil ipaddress skipped",
			in: graph([]client.Node{
				node("n1", "node"),
				node("n2", "node", ""),
				node("n3", "node", "10.0.0.1"),
			}, nil),
			want: []string{"10.0.0.1"},
		},
		{
			name: "insertion order preserved across entries",
			in: graph([]client.Node{
				node("nA", "node", "10.0.0.3"),
				node("nB", "node", "10.0.0.1", "10.0.0.2"),
			}, nil),
			want: []string{"10.0.0.3", "10.0.0.1", "10.0.0.2"},
		},
		{
			name: "no eligible entries returns empty",
			in: graph([]client.Node{
				node("p1", "pod", "10.1.0.5"),
			}, nil),
			want: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractIPs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestExtractIPs_DoesNotMutate(t *testing.T) {
	g := graph([]client.Node{node("n1", "node", "10.0.0.1")}, nil)
	before := *g
	beforeElements := g.Elements
	beforeIPs := append([]string(nil), g.Elements.Nodes[0].Data.IPAddress...)

	_ = ExtractIPs(g)

	if !reflect.DeepEqual(*g, before) || !reflect.DeepEqual(g.Elements, beforeElements) {
		t.Error("graph mutated")
	}
	if !reflect.DeepEqual(g.Elements.Nodes[0].Data.IPAddress, beforeIPs) {
		t.Error("ipaddress slice mutated")
	}
}

func TestReconcileSwitch(t *testing.T) {
	primary := graph([]client.Node{
		node("prod/abc", "node", "10.0.0.1", "10.0.0.2"),
		node("prod/def", "node", "10.0.0.5"),
	}, nil)

	t.Run("shadow collapses onto kube node and edges rewrite", func(t *testing.T) {
		sw := graph(
			[]client.Node{
				{Data: client.NodeData{ID: "sw-host:xyz", Type: "host", IPAddress: []string{"10.0.0.1"}}},
				{Data: client.NodeData{ID: "switch:tor-1", Type: "switch"}},
			},
			[]client.Edge{
				edge("host-attached", "sw-host:xyz", "switch:tor-1"),
			},
		)
		out := ReconcileSwitch(primary, sw)
		if got := len(out.Elements.Nodes); got != 1 {
			t.Errorf("want 1 surviving node (switch chassis), got %d: %+v", got, out.Elements.Nodes)
		}
		if out.Elements.Nodes[0].Data.ID != "switch:tor-1" {
			t.Errorf("want switch:tor-1, got %q", out.Elements.Nodes[0].Data.ID)
		}
		if len(out.Elements.Edges) != 1 {
			t.Fatalf("want 1 edge, got %d", len(out.Elements.Edges))
		}
		e := out.Elements.Edges[0].Data
		if e.Source != "prod/abc" || e.Target != "switch:tor-1" {
			t.Errorf("edge: want prod/abc → switch:tor-1, got %s → %s", e.Source, e.Target)
		}
	})

	t.Run("switch-only nodes preserved", func(t *testing.T) {
		sw := graph(
			[]client.Node{{Data: client.NodeData{ID: "switch:tor-1", Type: "switch"}}},
			nil,
		)
		out := ReconcileSwitch(primary, sw)
		if len(out.Elements.Nodes) != 1 || out.Elements.Nodes[0].Data.ID != "switch:tor-1" {
			t.Errorf("switch-only node should pass through; got %+v", out.Elements.Nodes)
		}
	})

	t.Run("foreign-IP shadow stays as orphan with original ID", func(t *testing.T) {
		sw := graph(
			[]client.Node{
				{Data: client.NodeData{ID: "sw-host:foreign", Type: "host", IPAddress: []string{"172.31.99.99"}}},
			},
			[]client.Edge{edge("host-attached", "sw-host:foreign", "switch:tor-9")},
		)
		out := ReconcileSwitch(primary, sw)
		if len(out.Elements.Nodes) != 1 || out.Elements.Nodes[0].Data.ID != "sw-host:foreign" {
			t.Errorf("orphan should keep original id; got %+v", out.Elements.Nodes)
		}
		if out.Elements.Edges[0].Data.Source != "sw-host:foreign" {
			t.Errorf("edge endpoint should not be rewritten; got %s", out.Elements.Edges[0].Data.Source)
		}
	})

	t.Run("multi-IP kube node matches via any of its IPs", func(t *testing.T) {
		sw := graph(
			[]client.Node{
				{Data: client.NodeData{ID: "sw-host:xyz", IPAddress: []string{"10.0.0.2"}}},
			},
			[]client.Edge{edge("host-attached", "sw-host:xyz", "switch:tor-1")},
		)
		out := ReconcileSwitch(primary, sw)
		if out.Elements.Edges[0].Data.Source != "prod/abc" {
			t.Errorf("want source=prod/abc, got %s", out.Elements.Edges[0].Data.Source)
		}
	})

	t.Run("nil switch returns nil", func(t *testing.T) {
		if got := ReconcileSwitch(primary, nil); got != nil {
			t.Errorf("want nil, got %+v", got)
		}
	})

	t.Run("inputs not mutated", func(t *testing.T) {
		p := graph([]client.Node{node("prod/abc", "node", "10.0.0.1")}, nil)
		s := graph(
			[]client.Node{{Data: client.NodeData{ID: "sw-host:x", IPAddress: []string{"10.0.0.1"}}}},
			[]client.Edge{edge("e", "sw-host:x", "switch:1")},
		)
		pBefore, sBefore := *p, *s
		pElemBefore, sElemBefore := p.Elements, s.Elements
		_ = ReconcileSwitch(p, s)
		if !reflect.DeepEqual(*p, pBefore) || !reflect.DeepEqual(p.Elements, pElemBefore) {
			t.Error("primary mutated")
		}
		if !reflect.DeepEqual(*s, sBefore) || !reflect.DeepEqual(s.Elements, sElemBefore) {
			t.Error("switch mutated")
		}
	})

	t.Run("self-loop introduced by rewrite collision is dropped", func(t *testing.T) {
		// Kube node N has IPs [.1, .2]; switch shadows A(.1) and B(.2) both
		// rewrite to N. Edge A→B (e.g., intra-fabric LAG link) MUST NOT become
		// a kube→kube self-loop in the output.
		sw := graph(
			[]client.Node{
				{Data: client.NodeData{ID: "sw-A", IPAddress: []string{"10.0.0.1"}}},
				{Data: client.NodeData{ID: "sw-B", IPAddress: []string{"10.0.0.2"}}},
			},
			[]client.Edge{edge("lag", "sw-A", "sw-B")},
		)
		out := ReconcileSwitch(primary, sw)
		for _, e := range out.Elements.Edges {
			if e.Data.Source == e.Data.Target {
				t.Errorf("self-loop introduced by rewrite collision: %+v", e)
			}
		}
	})

	t.Run("genuine self-loop in switch input is preserved", func(t *testing.T) {
		// If the switch graph genuinely contains a self-loop (e.g., port mirror),
		// reconcile should preserve it — only rewrite-induced collisions are dropped.
		sw := graph(
			[]client.Node{{Data: client.NodeData{ID: "switch:tor-1", Type: "switch"}}},
			[]client.Edge{edge("port-mirror", "switch:tor-1", "switch:tor-1")},
		)
		out := ReconcileSwitch(primary, sw)
		if len(out.Elements.Edges) != 1 {
			t.Errorf("genuine self-loop should be preserved, got %d edges", len(out.Elements.Edges))
		}
	})

	t.Run("rewrite target endpoint too", func(t *testing.T) {
		sw := graph(
			[]client.Node{
				{Data: client.NodeData{ID: "sw-host:xyz", IPAddress: []string{"10.0.0.1"}}},
			},
			[]client.Edge{edge("inbound", "switch:tor-1", "sw-host:xyz")},
		)
		out := ReconcileSwitch(primary, sw)
		if out.Elements.Edges[0].Data.Target != "prod/abc" {
			t.Errorf("want target=prod/abc, got %s", out.Elements.Edges[0].Data.Target)
		}
	})
}
