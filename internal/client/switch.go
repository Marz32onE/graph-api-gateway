package client

import (
	"context"
	"time"

	"github.com/go-resty/resty/v2"
)

// SwitchGraphClient targets the switch backend (node-to-switch L2/L3 lookup).
// It speaks the same /v1/graph Cytoscape contract as kube-state-graph; the
// gateway is responsible for synthesising the `ip=...` query string the switch
// backend expects.
type SwitchGraphClient struct {
	resty   *resty.Client
	baseURL string
}

var _ GraphBackend = (*SwitchGraphClient)(nil)

// NewSwitchGraphClient constructs the switch backend wrapper.
func NewSwitchGraphClient(baseURL, apiKey string, timeout time.Duration) *SwitchGraphClient {
	return &SwitchGraphClient{
		baseURL: baseURL,
		resty:   newRestyClient(apiKey, timeout),
	}
}

// FetchGraph issues GET {baseURL}/v1/graph?{rawQuery}. The caller is expected
// to set q.RawQuery to a deduped `ip=...&ip=...` string built from kube IPs
// (see pipeline.ExtractIPs + api.buildIPQuery).
func (c *SwitchGraphClient) FetchGraph(ctx context.Context, q GraphQuery) (*CytoscapeGraph, error) {
	return fetchGraph(ctx, c.resty, c.baseURL, q)
}
