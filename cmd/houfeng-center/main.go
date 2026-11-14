package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"houfeng/internal/center/config"
)

var version = "dev"

func main() {
	if err := runCenterCommand(); err != nil {
		fatal("center stopped", err)
	}
}

func runCenterCommand() error {
	cfg, err := config.LoadCenterConfig()
	if err != nil {
		return fmt.Errorf("load center config: %w", err)
	}

	cleanupLogging, err := setupLogging(cfg)
	if err != nil {
		return fmt.Errorf("setup logging: %w", err)
	}
	defer cleanupLogging()
	slog.Info("center starting", "version", safeLogVersion(version), "http_addr", cfg.HTTPAddr)
	defer slog.Info("center stopped")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, cleanup, err := bootstrapCenter(ctx, cfg, version, bootstrapDeps{})
	if err != nil {
		return fmt.Errorf("bootstrap center: %w", err)
	}
	defer cleanup()

	return app.Run(ctx)
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

func safeLogVersion(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
