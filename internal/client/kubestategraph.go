package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// graphPath is the relative path appended to every backend's baseURL.
// Both KubeStateGraphClient and SwitchGraphClient hit this single endpoint
// shape; centralising avoids drift if either upstream renames its route.
const graphPath = "/v1/graph"

// maxErrBodyExcerpt caps how many bytes of an upstream response body are
// included in error messages, keeping logs bounded on huge payloads.
const maxErrBodyExcerpt = 256

// probePath is the conventional liveness endpoint hit by /readyz when probing
// upstream backends for reachability.
const probePath = "/livez"

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

// FetchGraph issues GET {baseURL}{graphPath}?{rawQuery}.
func (c *KubeStateGraphClient) FetchGraph(ctx context.Context, q GraphQuery) (*CytoscapeGraph, error) {
	return fetchGraph(ctx, c.resty, c.baseURL, q)
}

// Probe checks that the backend is reachable, used by /readyz. Any HTTP
// response (including 404 or 405) is treated as "reachable"; only transport
// errors and 5xx are considered failures.
func (c *KubeStateGraphClient) Probe(ctx context.Context) error {
	return probeBackend(ctx, c.resty, c.baseURL)
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
	target := baseURL + graphPath
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
		body := truncate(resp.String(), maxErrBodyExcerpt)
		return nil, fmt.Errorf("client: %s returned %d: %s", target, resp.StatusCode(), body)
	}
	if len(resp.Body()) == 0 {
		return nil, errors.New("client: empty response body")
	}
	out := &CytoscapeGraph{}
	if err := json.Unmarshal(resp.Body(), out); err != nil {
		return nil, fmt.Errorf("client: decode %s: %w (body: %s)", target, err, truncate(resp.String(), maxErrBodyExcerpt))
	}
	if out.Elements.Nodes == nil {
		out.Elements.Nodes = []Node{}
	}
	if out.Elements.Edges == nil {
		out.Elements.Edges = []Edge{}
	}
	return out, nil
}

// probeBackend issues a short GET against {baseURL}/livez. Transport errors
// and 5xx responses are reported as failure; any 1xx/2xx/3xx/4xx is treated
// as reachable.
func probeBackend(ctx context.Context, r *resty.Client, baseURL string) error {
	target := baseURL + probePath
	resp, err := r.R().SetContext(ctx).Get(target)
	if err != nil {
		return fmt.Errorf("probe: GET %s: %w", target, err)
	}
	if resp.StatusCode() >= 500 {
		return fmt.Errorf("probe: %s returned %d", target, resp.StatusCode())
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Replace any invalid UTF-8 introduced by a mid-rune cut so the result is
	// safe for slog/JSON/span attribute exporters.
	return strings.ToValidUTF8(s[:n], "�") + "..."
}
