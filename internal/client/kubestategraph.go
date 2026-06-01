package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

// graphPath is the relative path appended to every backend's baseURL.
// Both backends hit this single endpoint shape; centralising avoids drift if
// either upstream renames its route.
const graphPath = "/v1/graph"

// maxErrBodyExcerpt caps how many bytes of an upstream response body are
// included in error messages, keeping logs bounded on huge payloads.
const maxErrBodyExcerpt = 256

// probePath is the conventional liveness endpoint hit by /readyz when probing
// upstream backends for reachability.
const probePath = "/livez"

// graphClient is the shared resty-backed transport core for both upstream
// backends. kube-state-graph and the switch backend speak the same /v1/graph
// Cytoscape contract and share identical transport behaviour (optional
// X-API-Key, per-call timeout, /livez probe); they differ only in request
// shape, so each is exposed as its own thin type (KubeStateGraphClient,
// SwitchGraphClient) embedding this core.
type graphClient struct {
	resty   *resty.Client
	baseURL string
}

func newGraphClient(baseURL, apiKey string, timeout time.Duration) *graphClient {
	return &graphClient{
		baseURL: baseURL,
		resty:   newRestyClient(apiKey, timeout),
	}
}

// Probe checks that the backend is reachable, used by /readyz. Any HTTP
// response (including 404 or 405) is treated as "reachable"; only transport
// errors and 5xx are considered failures. Shared by both backends.
func (c *graphClient) Probe(ctx context.Context) error {
	return probeBackend(ctx, c.resty, c.baseURL)
}

// KubeStateGraphClient queries the kube-state-graph primary, which receives the
// inbound query verbatim over GET via FetchGraph.
type KubeStateGraphClient struct {
	*graphClient
}

var _ GraphBackend = (*KubeStateGraphClient)(nil)

// NewKubeStateGraphClient constructs a client for the kube-state-graph
// /v1/graph contract.
func NewKubeStateGraphClient(baseURL, apiKey string, timeout time.Duration) *KubeStateGraphClient {
	return &KubeStateGraphClient{newGraphClient(baseURL, apiKey, timeout)}
}

// FetchGraph issues GET {baseURL}{graphPath}?{rawQuery}.
func (c *KubeStateGraphClient) FetchGraph(ctx context.Context, q GraphQuery) (*CytoscapeGraph, error) {
	return fetchGraph(ctx, c.resty, c.baseURL, q)
}

func newRestyClient(apiKey string, timeout time.Duration) *resty.Client {
	// Timeout is set on the underlying http.Client; resty inherits it. The
	// handler additionally wraps each call in context.WithTimeout for finer
	// per-stage budgets.
	r := resty.NewWithClient(&http.Client{Timeout: timeout})
	if apiKey != "" {
		r.SetHeader("X-API-Key", apiKey)
	}
	return r
}

// fetchGraph issues GET {baseURL}{graphPath}?{rawQuery} and decodes the
// Cytoscape response. Used by kube-state-graph, which receives the inbound
// query verbatim.
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
	return decodeGraphResponse(resp, "GET "+target)
}

// postGraph issues POST {baseURL}{graphPath} with a JSON-encoded body and
// decodes the Cytoscape response. Used by the switch backend, which is queried
// with a batched IP list (see switch.go) rather than a query string.
func postGraph(ctx context.Context, r *resty.Client, baseURL string, body any) (*CytoscapeGraph, error) {
	target := baseURL + graphPath
	resp, err := r.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(target)
	if err != nil {
		return nil, fmt.Errorf("client: POST %s: %w", target, err)
	}
	return decodeGraphResponse(resp, "POST "+target)
}

// decodeGraphResponse validates an upstream response and decodes it into a
// CytoscapeGraph, normalising nil node/edge slices to empty so callers never
// panic on access. reqDesc is the "<METHOD> <url>" label used in error messages.
func decodeGraphResponse(resp *resty.Response, reqDesc string) (*CytoscapeGraph, error) {
	if resp.IsError() {
		body := truncate(resp.String(), maxErrBodyExcerpt)
		return nil, fmt.Errorf("client: %s returned %d: %s", reqDesc, resp.StatusCode(), body)
	}
	if len(resp.Body()) == 0 {
		return nil, errors.New("client: empty response body")
	}
	out := &CytoscapeGraph{}
	if err := json.Unmarshal(resp.Body(), out); err != nil {
		return nil, fmt.Errorf("client: decode %s: %w (body: %s)", reqDesc, err, truncate(resp.String(), maxErrBodyExcerpt))
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
