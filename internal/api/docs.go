package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed static/openapi/*
var staticFS embed.FS

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
