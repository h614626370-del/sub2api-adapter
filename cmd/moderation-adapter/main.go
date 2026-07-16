package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sub2api-adapter/internal/adapter"
)

func main() {
	cfgPath := os.Getenv("ADAPTER_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.json"
	}

	cfg, err := adapter.LoadConfig(cfgPath)
	if err != nil {
		slog.Error("load_config_failed", "path", cfgPath, "error", err)
		os.Exit(1)
	}

	app, err := adapter.NewApp(cfg)
	if err != nil {
		slog.Error("create_app_failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = app.Close() }()

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("moderation_adapter_start", "addr", cfg.ListenAddr, "provider", cfg.Provider.Type)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		slog.Info("shutdown_signal", "signal", sig.String())
	case err := <-errCh:
		slog.Error("server_failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown_failed", "error", err)
		os.Exit(1)
	}
}
