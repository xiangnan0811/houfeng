package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/config"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

type appRunner interface {
	Run(context.Context) error
}

type postgresDB interface {
	Close()
	Pool() *pgxpool.Pool
}

type bootstrapDeps struct {
	openPostgres    func(context.Context, string) (postgresDB, error)
	applyMigrations func(context.Context, postgresDB) error
	newRouter       func(centerhttp.RouterOptions) http.Handler
	newApp          func(string, http.Handler) appRunner
}

type pgxPostgresDB struct {
	pool *pgxpool.Pool
}

func bootstrapCenter(ctx context.Context, cfg config.CenterConfig, version string, deps bootstrapDeps) (appRunner, func(), error) {
	deps = deps.withDefaults()

	db, err := deps.openPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}

	if err := deps.applyMigrations(ctx, db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("apply migrations: %w", err)
	}

	nodeRepo := store.NewPostgresNodeRepository(db.Pool())
	router := deps.newRouter(centerhttp.RouterOptions{
		Version:    version,
		WebDistDir: cfg.WebDistDir,
		Nodes:      nodeRepo,
	})

	return deps.newApp(cfg.HTTPAddr, router), db.Close, nil
}

func (d bootstrapDeps) withDefaults() bootstrapDeps {
	if d.openPostgres == nil {
		d.openPostgres = func(ctx context.Context, databaseURL string) (postgresDB, error) {
			pool, err := store.OpenPostgres(ctx, databaseURL)
			if err != nil {
				return nil, err
			}
			return pgxPostgresDB{pool: pool}, nil
		}
	}
	if d.applyMigrations == nil {
		d.applyMigrations = func(ctx context.Context, db postgresDB) error {
			return migrate.Apply(ctx, db.Pool())
		}
	}
	if d.newRouter == nil {
		d.newRouter = centerhttp.New
	}
	if d.newApp == nil {
		d.newApp = func(addr string, handler http.Handler) appRunner {
			return centerapp.New(addr, handler)
		}
	}
	return d
}

func (p pgxPostgresDB) Close() {
	p.pool.Close()
}

func (p pgxPostgresDB) Pool() *pgxpool.Pool {
	return p.pool
}
