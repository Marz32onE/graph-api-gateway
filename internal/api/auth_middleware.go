package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIKeyHeader is the HTTP header callers must use to present their API key.
const APIKeyHeader = "X-API-Key" //nolint:gosec // G101 false positive — header name, not a credential

// openPaths are exempt from API-key authentication. Health probes must answer
// kubelet without credentials, and the OpenAPI spec + Swagger UI must load in
// any browser without a key. Keys are gin route patterns (c.FullPath()), so the
// Swagger UI wildcard is the registered pattern, not a concrete asset URL.
var openPaths = map[string]struct{}{
	"/livez":          {},
	"/readyz":         {},
	"/openapi.yaml":   {},
	"/openapi.json":   {},
	"/docs/*filepath": {},
}

// apiKeyMiddleware enforces X-API-Key on protected routes when at least one
// key is loaded. With no keys configured, the middleware is a no-op so dev
// rigs and existing tests run unchanged.
func (s *Server) apiKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.keys == nil || s.keys.Empty() {
			c.Next()
			return
		}
		path := c.FullPath()
		if _, open := openPaths[path]; open {
			c.Next()
			return
		}
		presented := c.GetHeader(APIKeyHeader)
		if presented == "" {
			s.logger.WarnContext(c.Request.Context(), "auth rejected",
				"reason", "missing", "path", path, "request_id", c.GetString(requestIDKey))
			c.JSON(http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
			c.Abort()
			return
		}
		if !s.keys.Validate(presented) {
			s.logger.WarnContext(c.Request.Context(), "auth rejected",
				"reason", "invalid", "path", path, "request_id", c.GetString(requestIDKey))
			c.JSON(http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}
