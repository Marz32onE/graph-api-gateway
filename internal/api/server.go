// Package api implements the Gin HTTP server.
package api

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/marz32one/graph-api-gateway/internal/auth"
	"github.com/marz32one/graph-api-gateway/internal/client"
	"github.com/marz32one/graph-api-gateway/internal/config"
)

// Server holds the wired Gin engine, logger, backend clients, and the inbound
// API-key validator.
type Server struct {
	engine       *gin.Engine
	logger       *slog.Logger
	cfg          *config.Config
	ksg          *client.KubeStateGraphClient
	switchClient *client.SwitchGraphClient
	keys         auth.Validator
}

// New constructs a Server with all routes and middleware wired. keys may be nil
// to run with API-key authentication disabled (an empty KeySet is substituted).
func New(cfg *config.Config, logger *slog.Logger, ksg *client.KubeStateGraphClient, switchClient *client.SwitchGraphClient, keys auth.Validator) *Server {
	if keys == nil {
		keys = auth.NewKeySet()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	s := &Server{
		engine:       r,
		logger:       logger,
		cfg:          cfg,
		ksg:          ksg,
		switchClient: switchClient,
		keys:         keys,
	}

	r.Use(gin.Recovery())
	r.Use(requestIDMiddleware())
	r.Use(loggingMiddleware(logger))
	r.Use(s.apiKeyMiddleware())

	r.GET("/livez", s.handleLivez)
	r.GET("/readyz", s.handleReadyz)
	r.GET("/v1/graph", s.handleGraph)

	r.GET("/openapi.yaml", s.handleOpenAPIYAML)
	r.GET("/openapi.json", s.handleOpenAPIJSON)
	r.GET("/docs/*filepath", s.handleDocsUI)

	return s
}

// Handler returns the underlying http.Handler.
func (s *Server) Handler() *gin.Engine { return s.engine }
