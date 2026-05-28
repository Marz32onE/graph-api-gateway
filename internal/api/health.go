package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// probeTimeout caps each backend reachability probe on /readyz.
const probeTimeout = 1500 * time.Millisecond

// handleLivez is the liveness probe.
//
//	@Summary	Liveness probe
//	@Tags		health
//	@Produce	plain
//	@Success	200	{string}	string	"ok"
//	@Router		/livez [get]
func (s *Server) handleLivez(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

// handleReadyz probes both backends sequentially and returns 200 only when
// both are reachable. Either failure returns 503 with the failed backend in
// the response body.
//
//	@Summary	Readiness probe
//	@Tags		health
//	@Produce	plain
//	@Success	200	{string}	string	"ok"
//	@Failure	503	{string}	string	"not ready"
//	@Router		/readyz [get]
func (s *Server) handleReadyz(c *gin.Context) {
	if err := s.probe(c.Request.Context(), "ksg", s.ksg.Probe); err != nil {
		s.logger.WarnContext(c.Request.Context(), "readyz ksg probe failed", "err", err.Error())
		c.String(http.StatusServiceUnavailable, "not ready: ksg")
		return
	}
	if err := s.probe(c.Request.Context(), "switch", s.switchClient.Probe); err != nil {
		s.logger.WarnContext(c.Request.Context(), "readyz switch probe failed", "err", err.Error())
		c.String(http.StatusServiceUnavailable, "not ready: switch")
		return
	}
	c.String(http.StatusOK, "ok")
}

func (s *Server) probe(ctx context.Context, _ string, fn func(context.Context) error) error {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return fn(pctx)
}
