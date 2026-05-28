package api

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const requestIDHeader = "X-Request-ID"
const requestIDKey = "request_id"

// maxRequestIDLen caps inbound X-Request-ID values to bound log/header memory
// and stop attackers from forcing unbounded strings through structured logs.
const maxRequestIDLen = 128

// quietLogPaths are skipped from access logs on 2xx (healthchecks are noisy).
var quietLogPaths = map[string]struct{}{
	"/livez":  {},
	"/readyz": {},
}

// requestIDMiddleware honours an inbound X-Request-ID header when it is a
// safe, bounded string, or generates a UUID otherwise. The value is stored
// in gin context under "request_id" and echoed back to the caller.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		c.Set(requestIDKey, id)
		c.Header(requestIDHeader, id)
		// Decorate the otelgin-created span so the trace backend carries the
		// same correlation key as the response header and access log.
		if span := trace.SpanFromContext(c.Request.Context()); span.SpanContext().IsValid() {
			span.SetAttributes(attribute.String("http.request_id", id))
		}
		c.Next()
	}
}

// validRequestID accepts non-empty values up to maxRequestIDLen made of
// [A-Za-z0-9_.+:-]. The set covers UUIDs, ULIDs, and W3C traceparent IDs
// while rejecting whitespace / control chars that enable log injection.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := range len(id) {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '.' || c == '+' || c == ':' || c == '-':
		default:
			return false
		}
	}
	return true
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
