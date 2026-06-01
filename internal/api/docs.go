package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/marz32one/graph-api-gateway/docs"
)

// openAPISpec is the generated OpenAPI 3.1 document, rendered once at startup
// from the swag-generated docs package. The spec is compiled into the binary
// as a Go string (the swaggo convention) — no file embedding is needed.
// ReadDoc mutates SwaggerInfo and re-parses the template, so it is rendered
// exactly once here rather than per request.
var openAPISpec = renderOpenAPISpec()

// renderOpenAPISpec renders the swag doc template and strips the legacy
// Swagger-2.0 "schemes" field that swag v2's template always emits (as null
// under OpenAPI 3.1, where it is not a valid root field). Falls back to the
// raw document if it ever fails to parse.
func renderOpenAPISpec() []byte {
	raw := docs.SwaggerInfo.ReadDoc()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return []byte(raw)
	}
	delete(doc, "schemes")
	out, err := json.Marshal(doc)
	if err != nil {
		return []byte(raw)
	}
	return out
}

// handleOpenAPIJSON serves the generated OpenAPI 3.1 spec consumed by the
// Swagger UI at /docs/.
//
//	@Summary	OpenAPI spec (JSON)
//	@Tags		docs
//	@Produce	application/json
//	@Success	200	{string}	string	"OpenAPI 3.1 JSON"
//	@Router		/openapi.json [get]
func (s *Server) handleOpenAPIJSON(c *gin.Context) {
	c.Data(http.StatusOK, "application/json; charset=utf-8", openAPISpec)
}
