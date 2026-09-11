// Command runtime-api is the OpenHands Agent Sandbox Runtime adapter.
//
// It translates the OpenHands Remote Runtime HTTP API into Kubernetes
// agent-sandbox lifecycle operations.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/agentsandbox"
	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/config"
	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/httpapi"
	"github.com/openhands-agent-sandbox/openhands-agent-sandbox/internal/proxy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Set up structured logging
	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	var logger *slog.Logger
	if cfg.LogFormat == "json" {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	}
	slog.SetDefault(logger)

	logger.Info("starting openhands-agent-sandbox-runtime",
		"namespace", cfg.Namespace,
		"public_base_url", cfg.PublicBaseURL,
		"listen_addr", cfg.ListenAddr)

	// Create backend
	backend, err := agentsandbox.NewBackend(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create backend: %w", err)
	}

	// Create HTTP handler
	handler := httpapi.NewHandler(
		backend,
		cfg.APIKey,
		cfg.RegistryPrefix,
		cfg.KnownImages(),
	)

	// Create proxy
	proxyHandler := proxy.New(backend)

	// Set up HTTP mux
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	handler.RegisterProxy(mux, proxyHandler)

	// Register Prometheus metrics endpoint
	mux.Handle("GET /metrics", promhttp.Handler())

	// Create server
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	logger.Info("shutdown complete")
	return nil
}
