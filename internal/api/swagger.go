package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files/v2"
)

// swaggerInitializerJS overrides the upstream swagger-initializer.js (which
// points at petstore.swagger.io) so the embedded Swagger UI loads THIS
// service's spec from /openapi.json. The whole bundle ships inside the binary
// (swaggo/files/v2 embeds Swagger UI 5.x) — no CDN, fully offline.
const swaggerInitializerJS = `window.onload = function () {
  window.ui = SwaggerUIBundle({
    url: "/openapi.json",
    dom_id: "#swagger-ui",
    deepLinking: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    plugins: [SwaggerUIBundle.plugins.DownloadUrl],
    layout: "StandaloneLayout"
  });
};
`

// docsFileServer serves the embedded Swagger UI bundle (swaggo/files/v2).
// http.FileServer cleans the request path and blocks traversal outside the
// embedded FS, and serves index.html for the bundle root.
var docsFileServer = http.FileServer(http.FS(swaggerFiles.FS))

// handleDocsUI serves the offline Swagger UI under /docs/*filepath. One wildcard
// handler special-cases swagger-initializer.js (repointing the UI at our own
// /openapi.json) and serves every other asset from the embedded bundle. A
// separate static route alongside the catch-all would panic Gin's router on the
// shared tree node, so the special-case lives inside this single handler.
func (s *Server) handleDocsUI(c *gin.Context) {
	rel := c.Param("filepath")
	if rel == "/swagger-initializer.js" {
		c.Data(http.StatusOK, "application/javascript; charset=utf-8", []byte(swaggerInitializerJS))
		return
	}
	// Rewrite to the bundle-relative path; "/" resolves to index.html.
	c.Request.URL.Path = rel
	docsFileServer.ServeHTTP(c.Writer, c.Request)
}
