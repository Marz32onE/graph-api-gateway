// Package api implements the Gin HTTP server.
package api

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/marz32one/graph-api-gateway/internal/build"
	"github.com/marz32one/graph-api-gateway/internal/client"
	"github.com/marz32one/graph-api-gateway/internal/config"
)

// Server holds the wired Gin engine, logger, and backend clients.
type Server struct {
	engine       *gin.Engine
	logger       *slog.Logger
	cfg          *config.Config
	ksg          *client.KubeStateGraphClient
	switchClient *client.SwitchGraphClient
}

// New constructs a Server with all routes and middleware wired.
func New(cfg *config.Config, logger *slog.Logger, ksg *client.KubeStateGraphClient, switchClient *client.SwitchGraphClient) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	s := &Server{
		engine:       r,
		logger:       logger,
		cfg:          cfg,
		ksg:          ksg,
		switchClient: switchClient,
	}

	r.Use(gin.Recovery())
	r.Use(requestIDMiddleware())
	r.Use(otelgin.Middleware(build.ServiceName))
	r.Use(loggingMiddleware(logger))

	r.GET("/livez", s.handleLivez)
	r.GET("/readyz", s.handleReadyz)
	r.GET("/v1/graph", s.handleGraph)

	r.GET("/openapi.yaml", s.handleOpenAPIYAML)
	r.GET("/openapi.json", s.handleOpenAPIJSON)
	r.GET("/docs", s.handleDocs)
	r.GET("/docs/assets/*path", s.handleDocsAsset)

	return s
}

// Handler returns the underlying http.Handler.
func (s *Server) Handler() *gin.Engine { return s.engine }
