package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/marz32one/kube-state-graph/pkg/cytoscape"
	"github.com/marz32one/kube-state-graph/pkg/kubegraph"

	"github.com/marz32one/graph-api-gateway/internal/client"
	"github.com/marz32one/graph-api-gateway/internal/merge"
	"github.com/marz32one/graph-api-gateway/internal/pipeline"
)

// handleGraph serves GET /v1/graph by running the sequential pipeline:
// primary fetch → IP extraction → conditional switch fetch → reconciliation → merge.
//
//	@Summary		Merged kube + switch graph (Cytoscape.js)
//	@Description	Runs a sequential pipeline: (1) build the kube-state-graph graph in-process from the inbound query (an embedded engine querying VictoriaMetrics directly — no HTTP hop); (2) collect `data.ipaddress` from every `node`-type entry; (3) if any IPs were collected, call the switch backend as `POST /v1/graph` with a JSON body `[{"ip":"<a>"},{"ip":"<b>"}]` (all IPs batched into one request); (4) re-anchor the switch graph onto kube node IDs by matching `data.ipaddress` (switch shadow nodes are dropped, their edge references rewritten); (5) merge primary + reconciled switch (union nodes by `data.id` with kube winning, dedup edges by `(type,source,target)`). A malformed request returns `400` (kube-state-graph reason code); a primary build timeout `504`; any other stage failure `502`.
//	@Tags			graph
//	@Produce		json
//	@Param			start		query		string		true	"Window start. RFC 3339 (`2026-05-05T11:00:00Z`) or Unix seconds (`1746442800`). Forwarded to kube-state-graph."	example(2026-05-05T11:00:00Z)
//	@Param			end			query		string		true	"Window end. RFC 3339 or Unix seconds. Must be > start. Forwarded to kube-state-graph."	example(2026-05-05T12:00:00Z)
//	@Param			cluster		query		[]string	false	"Restrict to listed clusters (repeatable, OR-combined). Forwarded to kube-state-graph."	collectionFormat(multi)	example(prod-eu)
//	@Param			namespace	query		[]string	false	"Restrict to listed Kubernetes namespaces (repeatable, OR-combined). Forwarded to kube-state-graph."	collectionFormat(multi)	example(payments)
//	@Param			edge_type	query		[]string	false	"Restrict to listed edge types (repeatable, OR-combined). Forwarded to kube-state-graph."	collectionFormat(multi)	Enums(pod-mounts-pvc,pod-calls-pod,service-selects-pod)	example(pod-calls-pod)
//	@Param			name		query		[]string	false	"Restrict to nodes whose name matches exactly across every node type (repeatable, OR-combined). Forwarded to kube-state-graph."	collectionFormat(multi)	example(checkout-7d9f6c8b8-abcde)
//	@Param			root		query		string		false	"Cluster-scoped node ID anchoring a traversal (e.g. `<cluster>/<uid>`). Forwarded to kube-state-graph."	example(prod-eu/8f8d4f1a-1234-4abc-9def-0123456789ab)
//	@Param			depth		query		int			false	"BFS traversal depth in hops. Range `0..6`. Defaults to `2` when `root` is set, ignored otherwise. Forwarded to kube-state-graph."	minimum(0)	maximum(6)	default(2)	example(2)
//	@Param			direction	query		string		false	"Traversal direction relative to `root`. `out` = downstream, `in` = upstream, `both` = undirected. Forwarded to kube-state-graph."	Enums(in,out,both)	default(both)	example(both)
//	@Param			X-API-Key	header		string		false	"API key. Required when the gateway is started with API keys configured."
//	@Success		200			{object}	cytoscape.Body
//	@Failure		400			{object}	errorResponse	"invalid request parameters (e.g. missing/invalid start|end, invalid_range, depth_too_large, invalid_scope); `error` carries kube-state-graph's reason code"
//	@Failure		401			{object}	errorResponse	"missing or invalid X-API-Key (when auth enabled)"
//	@Failure		502			{object}	errorResponse
//	@Failure		504			{object}	errorResponse
//	@Security		ApiKeyAuth
//	@Router			/v1/graph [get]
func (s *Server) handleGraph(c *gin.Context) {
	ctx := c.Request.Context()

	// withTimeout runs one pipeline stage under a per-stage deadline derived
	// from the inbound request context, so a slow backend cannot stall the
	// whole request beyond its configured budget.
	withTimeout := func(timeout time.Duration, fn func(context.Context) (*cytoscape.Body, error)) (*cytoscape.Body, error) {
		stageCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return fn(stageCtx)
	}

	ksgGraph, err := withTimeout(s.cfg.KSG.BuildTimeout, func(stageCtx context.Context) (*cytoscape.Body, error) {
		return s.ksg.FetchGraph(stageCtx, client.GraphQuery{RawQuery: c.Request.URL.RawQuery})
	})
	if err != nil {
		s.writeFetchError(c, "ksg fetch failed", err)
		return
	}

	ips := pipeline.ExtractIPs(ksgGraph)

	var switchGraph *cytoscape.Body
	if len(ips) > 0 {
		switchGraph, err = withTimeout(s.cfg.Switch.Timeout, func(stageCtx context.Context) (*cytoscape.Body, error) {
			return s.switchClient.FetchGraphByIPs(stageCtx, ips)
		})
		if err != nil {
			s.writeFetchError(c, "switch fetch failed", err)
			return
		}
	}

	reconciled := pipeline.ReconcileSwitch(ksgGraph, switchGraph)
	merged := merge.Merge(ksgGraph, reconciled)
	c.JSON(http.StatusOK, merged)
}

// writeFetchError maps a backend fetch error to the right status code and
// emits one structured log line. Client disconnects (context.Canceled on the
// inbound request) are dropped silently — there is no peer to respond to and
// counting them as upstream failures pollutes SLOs. Per-stage timeouts surface
// as 504; everything else as 502.
func (s *Server) writeFetchError(c *gin.Context, msg string, err error) {
	ctx := c.Request.Context()
	// A *kubegraph.ParseError is a malformed inbound request (the embedded
	// engine parses the inbound query before any upstream I/O), not an upstream
	// failure. Surface it as 400 carrying kube-state-graph's stable reason code
	// so the gateway's /v1/graph input contract matches kube-state-graph's.
	var pe *kubegraph.ParseError
	if errors.As(err, &pe) {
		s.logger.InfoContext(ctx, msg+" (bad request)", "reason", pe.Reason, "err", err.Error())
		c.JSON(http.StatusBadRequest, errorResponse{Error: pe.Reason})
		return
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		s.logger.InfoContext(ctx, msg+" (client cancelled)", "err", err.Error())
		// Best-effort marker for any middleware reading the writer status.
		c.AbortWithStatus(499)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.logger.ErrorContext(ctx, msg+" (deadline exceeded)", "err", err.Error())
		c.JSON(http.StatusGatewayTimeout, errorResponse{Error: "upstream_timeout"})
		return
	}
	s.logger.ErrorContext(ctx, msg, "err", err.Error())
	c.JSON(http.StatusBadGateway, errorResponse{Error: "upstream_unavailable"})
}

// errorResponse is the gateway's error envelope.
type errorResponse struct {
	Error string `json:"error"`
}
