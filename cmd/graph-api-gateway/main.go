// Package main is the entrypoint for graph-api-gateway.
//
//	@title			graph-api-gateway
//	@version		v1
//	@description	HTTP gateway that runs a sequential pipeline: (1) fetch the kube-state-graph response with the inbound query, (2) extract `data.ipaddress` from every `node`-type entry, (3) if any IPs were collected, POST those IPs to the switch backend batched as a single `[{"ip":…}]` JSON body, (4) re-anchor switch shadow nodes onto kube node IDs via IP match, (5) merge into a single Cytoscape.js envelope. Propagates W3C trace context end-to-end and emits structured logs via slog. Any backend failure returns `502`.
//	@BasePath		/
//	@schemes		http https
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/marz32one/graph-api-gateway/internal/api"
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

	shutdownTracing, err := observability.SetupTracing(ctx)
	if err != nil {
		return fmt.Errorf("setup tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Error("tracing shutdown", "err", err.Error())
		}
	}()

	ksg := client.NewKubeStateGraphClient(cfg.KSG.BaseURL, cfg.KSG.APIKey, cfg.KSG.Timeout)
	switchClient := client.NewSwitchGraphClient(cfg.Switch.BaseURL, cfg.Switch.APIKey, cfg.Switch.Timeout)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.New(cfg, logger, ksg, switchClient).Handler(),
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
