package client

import "time"

// NewSwitchGraphClient constructs a client for the switch backend
// (node-to-switch L2/L3 lookup). It speaks the same /v1/graph Cytoscape
// contract as kube-state-graph; the gateway is responsible for synthesising
// the deduped `ip=...&ip=...` query string the switch backend expects (see
// pipeline.ExtractIPs + api.buildIPQuery).
func NewSwitchGraphClient(baseURL, apiKey string, timeout time.Duration) *GraphClient {
	return newGraphClient(baseURL, apiKey, timeout)
}
