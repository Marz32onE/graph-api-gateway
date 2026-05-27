package api

import "github.com/gin-gonic/gin"

// handleLivez is the liveness probe.
//
//	@Summary	Liveness probe
//	@Tags		health
//	@Produce	plain
//	@Success	200	{string}	string	"ok"
//	@Router		/livez [get]
func (s *Server) handleLivez(c *gin.Context) {
	c.String(200, "ok")
}

// handleReadyz is the readiness probe.
//
//	@Summary	Readiness probe
//	@Tags		health
//	@Produce	plain
//	@Success	200	{string}	string	"ok"
//	@Router		/readyz [get]
func (s *Server) handleReadyz(c *gin.Context) {
	c.String(200, "ok")
}
