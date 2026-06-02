package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/marz32one/kube-state-graph/pkg/cytoscape"
)

// graphPath is the relative path appended to the switch backend's baseURL.
const graphPath = "/v1/graph"

// maxErrBodyExcerpt caps how many bytes of an upstream response body are
// included in error messages, keeping logs bounded on huge payloads.
const maxErrBodyExcerpt = 256

// probePath is the conventional liveness endpoint hit by /readyz when probing
// the HTTP switch backend for reachability.
const probePath = "/livez"

// graphClient is the resty-backed transport core for the HTTP switch backend
// (optional X-API-Key, per-call timeout, /livez probe). The base URL lives on
// the resty client (SetBaseURL); call sites read it back via resty.Client.BaseURL.
type graphClient struct {
	resty *resty.Client
}

func newGraphClient(baseURL, apiKey string, timeout time.Duration) *graphClient {
	return &graphClient{resty: newRestyClient(baseURL, apiKey, timeout)}
}

// Probe checks that the backend is reachable, used by /readyz. Any HTTP
// response (including 404 or 405) is treated as "reachable"; only transport
// errors and 5xx are considered failures.
func (c *graphClient) Probe(ctx context.Context) error {
	return probeBackend(ctx, c.resty)
}

func newRestyClient(baseURL, apiKey string, timeout time.Duration) *resty.Client {
	// Timeout is set on the underlying http.Client; resty inherits it. The
	// handler additionally wraps each call in context.WithTimeout for finer
	// per-stage budgets, which also caps total retry time. SetBaseURL lets call
	// sites issue relative paths (graphPath/probePath); resty prepends baseURL.
	r := resty.NewWithClient(&http.Client{Timeout: timeout}).
		SetBaseURL(baseURL).
		SetHeader("Accept", "application/json").
		SetRetryCount(2).
		SetRetryWaitTime(100 * time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Second).
		AddRetryCondition(retryOnTransient)

	if apiKey != "" {
		r.SetHeader("X-API-Key", apiKey)
	}
	return r
}

// retryOnTransient retries connection-level failures and 5xx responses, but
// never retries timeouts or caller cancellation — those must fail fast so the
// handler can map them to 504 (per-stage timeout) / 499 (client cancel) within
// the stage budget instead of burning it on backoff sleeps.
func retryOnTransient(resp *resty.Response, err error) bool {
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return false
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return false
		}
		return true // dial/reset/EOF and other transient transport errors
	}
	return resp.StatusCode() >= http.StatusInternalServerError
}

// postGraph issues POST {baseURL}{graphPath} with a JSON-encoded body and
// decodes the Cytoscape response. Used by the switch backend, which is queried
// with a batched IP list (see switch.go).
func postGraph(ctx context.Context, r *resty.Client, body any) (*cytoscape.Body, error) {
	target := r.BaseURL + graphPath
	resp, err := r.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(graphPath) // relative; SetBaseURL prepends baseURL
	if err != nil {
		return nil, fmt.Errorf("client: POST %s: %w", target, err)
	}
	return decodeGraphResponse(resp, "POST "+target)
}

// decodeGraphResponse validates an upstream response and decodes it into a
// cytoscape.Body, normalising nil node/edge slices to empty so callers never
// panic on access. reqDesc is the "<METHOD> <url>" label used in error messages.
func decodeGraphResponse(resp *resty.Response, reqDesc string) (*cytoscape.Body, error) {
	if resp.IsError() {
		body := truncate(resp.String(), maxErrBodyExcerpt)
		return nil, fmt.Errorf("client: %s returned %d: %s", reqDesc, resp.StatusCode(), body)
	}
	if len(resp.Body()) == 0 {
		return nil, errors.New("client: empty response body")
	}
	out := &cytoscape.Body{}
	if err := json.Unmarshal(resp.Body(), out); err != nil {
		return nil, fmt.Errorf("client: decode %s: %w (body: %s)", reqDesc, err, truncate(resp.String(), maxErrBodyExcerpt))
	}
	if out.Elements.Nodes == nil {
		out.Elements.Nodes = []cytoscape.Node{}
	}
	if out.Elements.Edges == nil {
		out.Elements.Edges = []cytoscape.Edge{}
	}
	return out, nil
}

// probeBackend issues a short GET against {baseURL}/livez. Transport errors and
// 5xx responses are reported as failure; any 1xx/2xx/3xx/4xx is reachable.
func probeBackend(ctx context.Context, r *resty.Client) error {
	target := r.BaseURL + probePath
	resp, err := r.R().SetContext(ctx).Get(probePath) // relative; SetBaseURL prepends baseURL
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
