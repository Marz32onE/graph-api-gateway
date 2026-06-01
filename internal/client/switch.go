package client

import (
	"context"
	"time"

	"github.com/marz32one/kube-state-graph/pkg/cytoscape"
)

// ipRequest is one element of the switch backend's batched request body. The
// gateway POSTs a JSON array of these — `[{"ip":"10.0.0.1"},{"ip":"10.0.0.2"}]`
// — so every collected K8s node IP is sent in a single call (see design.md D9).
type ipRequest struct {
	IP string `json:"ip"`
}

// SwitchGraphClient queries the switch backend (node-to-switch L2/L3 lookup).
// It returns the same /v1/graph Cytoscape contract as kube-state-graph, but is
// queried differently: instead of a forwarded query string, every collected
// K8s node IP is sent at once via FetchGraphByIPs (POST /v1/graph with a
// `[{"ip":…}]` JSON body).
type SwitchGraphClient struct {
	*graphClient
}

// NewSwitchGraphClient constructs a client for the switch backend.
func NewSwitchGraphClient(baseURL, apiKey string, timeout time.Duration) *SwitchGraphClient {
	return &SwitchGraphClient{newGraphClient(baseURL, apiKey, timeout)}
}

// FetchGraphByIPs queries the switch backend with all K8s node IPs in a single
// POST {baseURL}/v1/graph call whose body is `[{"ip":"<addr>"},…]`. The IP
// order is preserved from ips. Callers skip this stage entirely when no IPs
// were collected; invoking it with an empty slice still posts an empty array.
func (c *SwitchGraphClient) FetchGraphByIPs(ctx context.Context, ips []string) (*cytoscape.Body, error) {
	body := make([]ipRequest, len(ips))
	for i, ip := range ips {
		body[i] = ipRequest{IP: ip}
	}
	return postGraph(ctx, c.resty, c.baseURL, body)
}
