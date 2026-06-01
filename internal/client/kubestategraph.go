package client

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/marz32one/kube-state-graph/pkg/cytoscape"
	"github.com/marz32one/kube-state-graph/pkg/kubegraph"
	"github.com/marz32one/kube-state-graph/pkg/promql"
)

// KubeStateGraphClient is the primary backend. It builds the kube-state-graph
// response in-process via an embedded kubegraph.Engine querying VictoriaMetrics
// directly, instead of calling a kube-state-graph HTTP service — the same graph
// logic with no HTTP hop and no JSON round-trip. It satisfies GraphBackend.
type KubeStateGraphClient struct {
	engine *kubegraph.Engine
}

var _ GraphBackend = (*KubeStateGraphClient)(nil)

// NewKubeStateGraphClient builds a client that queries VictoriaMetrics at vmURL.
// metricPrefix is the upstream metric-name prefix (kube-state-graph design.md
// D26); buildTimeout bounds the internal retention probe. Build self-metrics are
// disabled (nil Metrics) so the gateway does not register kube_state_graph_*
// series in its own registry.
func NewKubeStateGraphClient(vmURL, metricPrefix string, buildTimeout time.Duration) (*KubeStateGraphClient, error) {
	q, err := promql.New(vmURL, nil)
	if err != nil {
		return nil, fmt.Errorf("client: kube-state-graph engine: %w", err)
	}
	eng := kubegraph.New(q, kubegraph.Options{
		MetricPrefix: metricPrefix,
		APITimeout:   buildTimeout,
	})
	return &KubeStateGraphClient{engine: eng}, nil
}

// FetchGraph parses the inbound query and builds the graph in-process, returning
// the identical Cytoscape body the kube-state-graph HTTP API would have served.
// The caller bounds the build via ctx (per-stage timeout).
func (c *KubeStateGraphClient) FetchGraph(ctx context.Context, q GraphQuery) (*cytoscape.Body, error) {
	vals, err := url.ParseQuery(q.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("client: parse query %q: %w", q.RawQuery, err)
	}
	body, err := c.engine.BuildFromValues(ctx, vals)
	if err != nil {
		return nil, err
	}
	return &body, nil
}

// Probe reports VictoriaMetrics reachability for /readyz, delegating to the
// engine's up{} health primitive (no bare querier, no out-of-clock time).
func (c *KubeStateGraphClient) Probe(ctx context.Context) error {
	return c.engine.Probe(ctx)
}
