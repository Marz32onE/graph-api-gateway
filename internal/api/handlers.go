package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
//	@Description	Runs a sequential pipeline: (1) forward the inbound query to kube-state-graph and parse the Cytoscape response; (2) collect `data.ipaddress` from every `node`-type entry; (3) if any IPs were collected, call the switch backend as `GET /v1/graph?ip=<a>&ip=<b>...`; (4) re-anchor the switch graph onto kube node IDs by matching `data.ipaddress` (switch shadow nodes are dropped, their edge references rewritten); (5) merge primary + reconciled switch (union nodes by `data.id` with kube winning, dedup edges by `(type,source,target)`). Any stage failure returns `502`.
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
//	@Failure		502			{object}	errorResponse
//	@Failure		504			{object}	errorResponse
//	@Router			/v1/graph [get]
func (s *Server) handleGraph(c *gin.Context) {
	ctx := c.Request.Context()

	fetch := func(timeout time.Duration, b client.GraphBackend, q client.GraphQuery) (*client.CytoscapeGraph, error) {
		stageCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return b.FetchGraph(stageCtx, q)
	}

	ksgGraph, err := fetch(s.cfg.KSG.Timeout, s.ksg, client.GraphQuery{RawQuery: c.Request.URL.RawQuery})
	if err != nil {
		s.writeFetchError(c, "ksg fetch failed", err)
		return
	}

	ips := pipeline.ExtractIPs(ksgGraph)

	var switchGraph *client.CytoscapeGraph
	if len(ips) > 0 {
		switchGraph, err = fetch(s.cfg.Switch.Timeout, s.switchClient, client.GraphQuery{RawQuery: buildIPQuery(ips)})
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

// buildIPQuery encodes ips as `ip=<a>&ip=<b>&...` with proper URL escaping.
func buildIPQuery(ips []string) string {
	return url.Values{"ip": ips}.Encode()
}

// errorResponse is the gateway's error envelope.
type errorResponse struct {
	Error string `json:"error"`
}
