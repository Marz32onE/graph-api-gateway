package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

// KubeStateGraphClient wraps the upstream kube-state-graph /v1/graph contract.
type KubeStateGraphClient struct {
	resty   *resty.Client
	baseURL string
}

var _ GraphBackend = (*KubeStateGraphClient)(nil)

// NewKubeStateGraphClient constructs the kube-state-graph wrapper.
func NewKubeStateGraphClient(baseURL, apiKey string, timeout time.Duration) *KubeStateGraphClient {
	return &KubeStateGraphClient{
		baseURL: baseURL,
		resty:   newRestyClient(apiKey, timeout),
	}
}

// FetchGraph issues GET {baseURL}/v1/graph?{rawQuery}.
func (c *KubeStateGraphClient) FetchGraph(ctx context.Context, q GraphQuery) (*CytoscapeGraph, error) {
	return fetchGraph(ctx, c.resty, c.baseURL, q)
}

func newRestyClient(apiKey string, timeout time.Duration) *resty.Client {
	// Timeout is set on the underlying http.Client (see newHTTPClient); resty
	// inherits it. The handler additionally wraps each call in
	// context.WithTimeout for finer per-stage budgets.
	r := resty.NewWithClient(newHTTPClient(timeout))
	if apiKey != "" {
		r.SetHeader("X-API-Key", apiKey)
	}
	return r
}

// fetchGraph is shared by both built-in clients.
func fetchGraph(ctx context.Context, r *resty.Client, baseURL string, q GraphQuery) (*CytoscapeGraph, error) {
	target := baseURL + "/v1/graph"
	if q.RawQuery != "" {
		target += "?" + q.RawQuery
	}
	resp, err := r.R().
		SetContext(ctx).
		Get(target)
	if err != nil {
		return nil, fmt.Errorf("client: GET %s: %w", target, err)
	}
	if resp.IsError() {
		body := truncate(resp.String(), 256)
		return nil, fmt.Errorf("client: %s returned %d: %s", target, resp.StatusCode(), body)
	}
	if len(resp.Body()) == 0 {
		return nil, errors.New("client: empty response body")
	}
	out := &CytoscapeGraph{}
	if err := json.Unmarshal(resp.Body(), out); err != nil {
		return nil, fmt.Errorf("client: decode %s: %w (body: %s)", target, err, truncate(resp.String(), 256))
	}
	if out.Elements.Nodes == nil {
		out.Elements.Nodes = []Node{}
	}
	if out.Elements.Edges == nil {
		out.Elements.Edges = []Edge{}
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
