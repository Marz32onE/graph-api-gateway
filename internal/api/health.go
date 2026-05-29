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
	ctx := c.Request.Context()
	backends := []struct {
		name  string
		probe func(context.Context) error
	}{
		{"ksg", s.ksg.Probe},
		{"switch", s.switchClient.Probe},
	}
	for _, b := range backends {
		if err := s.probe(ctx, b.probe); err != nil {
			s.logger.WarnContext(ctx, "readyz probe failed", "backend", b.name, "err", err.Error())
			c.String(http.StatusServiceUnavailable, "not ready: "+b.name)
			return
		}
	}
	c.String(http.StatusOK, "ok")
}

func (s *Server) probe(ctx context.Context, fn func(context.Context) error) error {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return fn(pctx)
}
