package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"houfeng/internal/center/config"
)

var version = "dev"

func main() {
	cfg, err := config.LoadCenterConfig()
	if err != nil {
		fatal("load center config", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, cleanup, err := bootstrapCenter(ctx, cfg, version, bootstrapDeps{})
	if err != nil {
		fatal("bootstrap center", err)
	}
	defer cleanup()

	if err := app.Run(ctx); err != nil {
		fatal("run center app", err)
	}
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
