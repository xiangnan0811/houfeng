package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/config"
	"houfeng/internal/center/enrollment"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	incidentservice "houfeng/internal/center/incidents"
	"houfeng/internal/center/notify"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
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
	newApp          func(string, http.Handler, centerapp.Worker) appRunner
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
	targetRepo := store.NewPostgresTargetRepository(db.Pool())
	runtimeFactsRepo := store.NewPostgresRuntimeFactsRepository(db.Pool())
	incidentRepo := store.NewPostgresIncidentRepository(db.Pool())
	dashboardRepo := store.NewPostgresDashboardRepository(db.Pool())
	snapshotReader := incidentservice.NewPostgresSnapshotReader(db.Pool())
	enrollmentSvc := enrollment.NewService(nodeRepo)
	syncRepo := store.NewPostgresSyncRepository(db.Pool())
	var notifier incidentservice.Notifier
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		notifier = notify.NewTelegramNotifier(cfg.TelegramBotToken, cfg.TelegramChatID)
	}
	incidentSvc := incidentservice.NewService(
		nodeRepo,
		targetRepo,
		snapshotReader,
		incidentRepo,
		notifier,
		slog.Default(),
		30*time.Second,
		cfg.IncidentSweepInterval,
	)
	syncSvc := syncing.NewService(syncRepo, incidentSvc)
	router := deps.newRouter(centerhttp.RouterOptions{
		Version:                   version,
		WebDistDir:                cfg.WebDistDir,
		DashboardHandler:          handlers.Dashboard(dashboardRepo),
		EventsHandler:             handlers.Events(dashboardRepo),
		IncidentsHandler:          handlers.Incidents(incidentRepo),
		NodesCollectionHandler:    handlers.NodesCollection(nodeRepo),
		NodeItemHandler:           handlers.NodeItem(nodeRepo),
		NodeRuntimeFactsHandler:   handlers.NodeRuntimeFacts(runtimeFactsRepo),
		TargetsCollectionHandler:  handlers.TargetsCollection(targetRepo),
		TargetItemHandler:         handlers.TargetItem(targetRepo),
		TargetProbeItemsHandler:   handlers.TargetProbeItems(targetRepo),
		TargetRuntimeFactsHandler: handlers.TargetRuntimeFacts(runtimeFactsRepo),
		AgentEnrollHandler:        handlers.AgentEnroll(enrollmentSvc),
		AgentSyncHandler:          handlers.AgentSync(syncSvc),
	})

	return deps.newApp(cfg.HTTPAddr, router, incidentSvc), db.Close, nil
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
		d.newApp = func(addr string, handler http.Handler, worker centerapp.Worker) appRunner {
			return centerapp.New(addr, handler, worker)
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
