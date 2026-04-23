package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"houfeng/internal/center/config"
)

var version = "dev"

func main() {
	cfg, err := config.LoadCenterConfig()
	if err != nil {
		log.Fatalf("load center config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, cleanup, err := bootstrapCenter(ctx, cfg, version, bootstrapDeps{})
	if err != nil {
		log.Fatalf("bootstrap center: %v", err)
	}
	defer cleanup()

	if err := app.Run(ctx); err != nil {
		log.Fatalf("run center app: %v", err)
	}
}
