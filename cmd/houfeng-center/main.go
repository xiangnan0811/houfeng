package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	centerapp "houfeng/internal/center/app"
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

	if err := centerapp.New(cfg, version).Run(ctx); err != nil {
		log.Fatalf("run center app: %v", err)
	}
}
