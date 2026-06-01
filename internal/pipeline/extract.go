// Package pipeline contains pure orchestration helpers used by the /v1/graph
// handler to bridge kube-state-graph and the switch backend.
package pipeline

import "github.com/marz32one/kube-state-graph/pkg/cytoscape"

// nodeTypeK8sNode is the data.type value the gateway treats as a K8s node
// for IP extraction and reconciliation. Centralised so extract.go and
// reconcile.go cannot drift.
const nodeTypeK8sNode = "node"

// iterNodeIPs invokes fn(nodeID, ip) for every (id, ip) pair on K8s-node
// entries in g, skipping empty IPs. Pure walk — caller decides what to collect.
func iterNodeIPs(g *cytoscape.Body, fn func(nodeID, ip string)) {
	if g == nil {
		return
	}
	for _, n := range g.Elements.Nodes {
		if n.Data.Type != nodeTypeK8sNode {
			continue
		}
		for _, ip := range n.Data.IPAddress {
			if ip == "" {
				continue
			}
			fn(n.Data.ID, ip)
		}
	}
}

// ExtractIPs collects every value from data.ipaddress on node-type entries in g.
//
// Only node-type entries are considered — pod / pvc / external entries are
// excluded by design because the switch backend is queried with K8s node IPs
// only. Returned IPs are deduplicated and preserve insertion order. Returns a
// non-nil empty slice when g is nil or contains no eligible entries. Does not
// mutate the input.
func ExtractIPs(g *cytoscape.Body) []string {
	n := 0
	if g != nil {
		n = len(g.Elements.Nodes)
	}
	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	iterNodeIPs(g, func(_, ip string) {
		if _, dup := seen[ip]; dup {
			return
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	})
	return out
}
