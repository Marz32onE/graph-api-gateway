// Package main is the entrypoint for graph-api-gateway.
//
//	@title			graph-api-gateway
//	@version		v1
//	@description	HTTP gateway that runs a sequential pipeline: (1) build the kube-state-graph graph in-process from the inbound query (an embedded engine querying VictoriaMetrics directly), (2) extract `data.ipaddress` from every `node`-type entry, (3) if any IPs were collected, POST those IPs to the switch backend batched as a single `[{"ip":…}]` JSON body, (4) re-anchor switch shadow nodes onto kube node IDs via IP match, (5) merge into a single Cytoscape.js envelope. Emits structured logs via slog. Any backend failure returns `502`.
//	@description
//	@description	**Authentication.** When the gateway is started with API keys configured (`API_KEYS` or `API_KEYS_FILE`), every request to `/v1/*` MUST carry an `X-API-Key: <key>` header. Missing or invalid keys yield `401 Unauthorized`. Health probes (`/livez`, `/readyz`), the OpenAPI spec (`/openapi.*`), and the Swagger UI (`/docs/*`) are exempt and require no key.
//	@BasePath		/
//
//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						X-API-Key
//	@description				API key presented in the `X-API-Key` header. Required on `/v1/*` when the gateway is started with keys configured. Health and docs routes are exempt.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marz32one/graph-api-gateway/internal/api"
	"github.com/marz32one/graph-api-gateway/internal/auth"
	"github.com/marz32one/graph-api-gateway/internal/build"
	"github.com/marz32one/graph-api-gateway/internal/client"
	"github.com/marz32one/graph-api-gateway/internal/config"
	"github.com/marz32one/graph-api-gateway/internal/observability"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "graph-api-gateway: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := observability.NewLogger(cfg)
	logger.Info("starting",
		"version", build.Version,
		"commit", build.Commit,
		"listen_addr", cfg.ListenAddr,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	keys, err := loadAPIKeys(cfg, logger)
	if err != nil {
		return fmt.Errorf("api keys: %w", err)
	}
	if cfg.APIKeysFile != "" && cfg.APIKeysReloadInterval > 0 {
		go reloadAPIKeys(ctx, keys, cfg.APIKeysFile, cfg.APIKeysReloadInterval, logger)
	}

	ksg, err := client.NewKubeStateGraphClient(cfg.KSG.VictoriaMetricsURL, cfg.KSG.MetricPrefix, cfg.KSG.BuildTimeout)
	if err != nil {
		return fmt.Errorf("kube-state-graph engine: %w", err)
	}
	switchClient := client.NewSwitchGraphClient(cfg.Switch.BaseURL, cfg.Switch.APIKey, cfg.Switch.Timeout)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.New(cfg, logger, ksg, switchClient, keys).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	return nil
}

// loadAPIKeys returns a populated KeySet (file or CSV) or an empty one when
// neither source is configured. File loading is fail-fast at startup; the
// optional background reload (see reloadAPIKeys) is wired by the caller.
func loadAPIKeys(cfg *config.Config, logger *slog.Logger) (*auth.KeySet, error) {
	ks := auth.NewKeySet()
	switch {
	case cfg.APIKeysFile != "":
		if err := ks.LoadFile(cfg.APIKeysFile); err != nil {
			return nil, err
		}
		logger.Info("api key auth enabled (file)",
			"path", cfg.APIKeysFile,
			"keys", ks.Snapshot(),
			"reload_interval", cfg.APIKeysReloadInterval,
		)
	case cfg.APIKeys != "":
		ks.LoadCSV(cfg.APIKeys)
		logger.Info("api key auth enabled (env)", "keys", ks.Snapshot())
	default:
		logger.Warn("api key auth DISABLED — no API_KEYS_FILE or API_KEYS configured")
	}
	return ks, nil
}

// reloadAPIKeys re-reads the key file every interval until ctx is cancelled, so
// a Kubernetes Secret rotation is picked up without a process restart.
func reloadAPIKeys(ctx context.Context, ks *auth.KeySet, path string, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := ks.LoadFile(path); err != nil {
				logger.Error("api keys reload failed", "path", path, "err", err.Error())
			}
		}
	}
}
