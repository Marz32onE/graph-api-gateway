package api

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed static/openapi/* static/scalar/*
var staticFS embed.FS

const scalarHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>graph-api-gateway · API Reference</title>
</head>
<body>
  <script id="api-reference" data-url="/openapi.json"></script>
  <script src="/docs/assets/scalar.js"></script>
</body>
</html>
`

// handleOpenAPIYAML serves the embedded OpenAPI YAML spec.
//
//	@Summary	OpenAPI spec (YAML)
//	@Tags		docs
//	@Produce	application/yaml
//	@Success	200	{string}	string	"OpenAPI 3.1 YAML"
//	@Router		/openapi.yaml [get]
func (s *Server) handleOpenAPIYAML(c *gin.Context) {
	b, err := fs.ReadFile(staticFS, "static/openapi/openapi.yaml")
	if err != nil {
		c.String(http.StatusNotFound, "openapi.yaml not built — run `make docs`")
		return
	}
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", b)
}

// handleOpenAPIJSON serves the embedded OpenAPI JSON spec.
//
//	@Summary	OpenAPI spec (JSON)
//	@Tags		docs
//	@Produce	application/json
//	@Success	200	{string}	string	"OpenAPI 3.1 JSON"
//	@Router		/openapi.json [get]
func (s *Server) handleOpenAPIJSON(c *gin.Context) {
	b, err := fs.ReadFile(staticFS, "static/openapi/openapi.json")
	if err != nil {
		c.String(http.StatusNotFound, "openapi.json not built — run `make docs`")
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", b)
}

// handleDocs serves the Scalar API Reference HTML shell.
//
//	@Summary	API reference UI (Scalar)
//	@Tags		docs
//	@Produce	text/html
//	@Success	200	{string}	string	"HTML"
//	@Router		/docs [get]
func (s *Server) handleDocs(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(scalarHTML))
}

// handleDocsAsset serves vendored Scalar assets.
//
//	@Summary	API reference asset (vendored)
//	@Tags		docs
//	@Param		path	path		string	true	"Asset path"
//	@Success	200		{file}		file
//	@Failure	404		{string}	string	"asset not found"
//	@Router		/docs/assets/{path} [get]
func (s *Server) handleDocsAsset(c *gin.Context) {
	requested := strings.TrimPrefix(c.Param("path"), "/")
	// Disallow path traversal: clean and ensure no parent refs.
	clean := path.Clean(requested)
	if clean == "." || strings.HasPrefix(clean, "..") || strings.Contains(clean, "../") {
		c.String(http.StatusNotFound, "not found")
		return
	}
	b, err := fs.ReadFile(staticFS, "static/scalar/"+clean)
	if err != nil {
		c.String(http.StatusNotFound, "not found")
		return
	}
	c.Data(http.StatusOK, mimeFor(clean), b)
}

func mimeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".map"):
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
