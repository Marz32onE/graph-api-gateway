package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/marz32one/graph-api-gateway/internal/client"
	"github.com/marz32one/graph-api-gateway/internal/merge"
	"github.com/marz32one/graph-api-gateway/internal/pipeline"
)

// handleGraph serves GET /v1/graph by running the sequential pipeline:
// primary fetch → IP extraction → conditional switch fetch → reconciliation → merge.
//
//	@Summary		Merged kube + switch graph (Cytoscape.js)
//	@Description	Runs a sequential pipeline: (1) forward the inbound query to kube-state-graph and parse the Cytoscape response; (2) collect `data.ipaddress` from every `node`-type entry; (3) if any IPs were collected, call the switch backend as `POST /v1/graph` with a JSON body `[{"ip":"<a>"},{"ip":"<b>"}]` (all IPs batched into one request); (4) re-anchor the switch graph onto kube node IDs by matching `data.ipaddress` (switch shadow nodes are dropped, their edge references rewritten); (5) merge primary + reconciled switch (union nodes by `data.id` with kube winning, dedup edges by `(type,source,target)`). Any stage failure returns `502`.
//	@Tags			graph
//	@Produce		json
//	@Param			start		query		string	false	"Window start (RFC 3339 or Unix seconds) — forwarded to kube-state-graph"
//	@Param			end			query		string	false	"Window end (RFC 3339 or Unix seconds) — forwarded to kube-state-graph"
//	@Param			cluster		query		string	false	"Restrict to a cluster (repeatable) — forwarded to kube-state-graph"
//	@Param			namespace	query		string	false	"Restrict to a namespace (repeatable) — forwarded to kube-state-graph"
//	@Param			edge_type	query		string	false	"Restrict edges by type (repeatable) — forwarded to kube-state-graph"
//	@Param			name		query		string	false	"Restrict to nodes whose name matches exactly (repeatable) — forwarded to kube-state-graph"
//	@Param			root		query		string	false	"Anchor a traversal at a node id — forwarded to kube-state-graph"
//	@Param			depth		query		int		false	"Traversal depth — forwarded to kube-state-graph"
//	@Param			direction	query		string	false	"Traversal direction (`in`|`out`|`both`) — forwarded to kube-state-graph"
//	@Success		200			{object}	client.CytoscapeGraph
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
	withTimeout := func(timeout time.Duration, fn func(context.Context) (*client.CytoscapeGraph, error)) (*client.CytoscapeGraph, error) {
		stageCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return fn(stageCtx)
	}

	ksgGraph, err := withTimeout(s.cfg.KSG.Timeout, func(stageCtx context.Context) (*client.CytoscapeGraph, error) {
		return s.ksg.FetchGraph(stageCtx, client.GraphQuery{RawQuery: c.Request.URL.RawQuery})
	})
	if err != nil {
		s.writeFetchError(c, "ksg fetch failed", err)
		return
	}

	ips := pipeline.ExtractIPs(ksgGraph)

	var switchGraph *client.CytoscapeGraph
	if len(ips) > 0 {
		switchGraph, err = withTimeout(s.cfg.Switch.Timeout, func(stageCtx context.Context) (*client.CytoscapeGraph, error) {
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
