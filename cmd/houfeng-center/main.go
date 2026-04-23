package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/config"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

var version = "dev"

func main() {
	cfg, err := config.LoadCenterConfig()
	if err != nil {
		log.Fatalf("load center config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer db.Close()

	if err := migrate.Apply(ctx, db); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	router := centerhttp.New(centerhttp.RouterOptions{
		Version:    version,
		WebDistDir: cfg.WebDistDir,
		DB:         db,
	})

	if err := centerapp.New(cfg.HTTPAddr, router).Run(ctx); err != nil {
		log.Fatalf("run center app: %v", err)
	}
}
