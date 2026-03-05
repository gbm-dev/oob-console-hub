package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gbm-dev/oob-console-hub/internal/auth"
	"github.com/gbm-dev/oob-console-hub/internal/config"
	"github.com/gbm-dev/oob-console-hub/internal/modem"
	"github.com/gbm-dev/oob-console-hub/internal/sshserver"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	cfg := config.LoadFromEnv()

	// Init user store
	store, err := auth.NewFileStore(cfg.UserDataDir)
	if err != nil {
		slog.Error("initializing user store", "err", err)
		os.Exit(1)
	}

	// Run legacy migration if old files exist
	if err := auth.MigrateFromLegacy(cfg.UserDataDir, store); err != nil {
		slog.Warn("legacy migration failed", "err", err)
	}

	// Parse site config
	sites, err := config.ParseSitesFile(cfg.SitesPath)
	if err != nil {
		slog.Error("loading sites config", "err", err)
		os.Exit(1)
	}
	slog.Info("sites loaded", "count", len(sites), "path", cfg.SitesPath)

	// Create modem device lock
	lock := modem.NewDeviceLock(cfg.DevicePath)
	slog.Info("modem device configured", "device", cfg.DevicePath)

	// Start SSH server
	srv, err := sshserver.New(cfg, store, lock, sites)
	if err != nil {
		slog.Error("creating SSH server", "err", err)
		os.Exit(1)
	}

	// Signal handling for graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			slog.Error("SSH server error", "err", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
	}

	slog.Info("shutdown complete")
}
