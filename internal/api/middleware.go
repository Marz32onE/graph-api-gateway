package api

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"
const requestIDKey = "request_id"

// quietLogPaths are skipped from access logs on 2xx (healthchecks are noisy).
var quietLogPaths = map[string]struct{}{
	"/livez":  {},
	"/readyz": {},
}

// requestIDMiddleware honours an inbound X-Request-ID header or generates a UUID,
// stores it in gin context under "request_id", and echoes it back to the caller.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(requestIDKey, id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

// loggingMiddleware emits one structured access log line per request.
// 2xx responses to /livez and /readyz are skipped to avoid drowning the log.
func loggingMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		if _, quiet := quietLogPaths[path]; quiet && status < 400 {
			return
		}
		logger.InfoContext(c.Request.Context(), "http",
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", c.GetString(requestIDKey),
		)
	}
}
