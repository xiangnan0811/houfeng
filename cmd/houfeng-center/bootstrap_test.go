package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	centerapp "houfeng/internal/center/app"
	"houfeng/internal/center/config"
	centerhttp "houfeng/internal/center/http"
)

func TestBootstrapCenterReturnsOpenPostgresError(t *testing.T) {
	cfg := config.CenterConfig{HTTPAddr: ":8080", WebDistDir: "web/dist", DatabaseURL: "postgres://center"}
	wantErr := errors.New("open boom")
	calledMigrate := false
	calledRouter := false
	calledApp := false

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return nil, wantErr
		},
		applyMigrations: func(context.Context, postgresDB) error {
			calledMigrate = true
			return nil
		},
		newRouter: func(centerhttp.RouterOptions) http.Handler {
			calledRouter = true
			return http.NewServeMux()
		},
		newApp: func(string, http.Handler, centerapp.Worker) appRunner {
			calledApp = true
			return fakeApp{}
		},
	})
	if err == nil {
		t.Fatal("bootstrapCenter() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "open postgres") {
		t.Fatalf("bootstrapCenter() error = %q, want wrapped open postgres error", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("bootstrapCenter() error = %v, want wrapped %v", err, wantErr)
	}
	if app != nil {
		t.Fatal("bootstrapCenter() app != nil, want nil")
	}
	if cleanup != nil {
		t.Fatal("bootstrapCenter() cleanup != nil, want nil")
	}
	if calledMigrate || calledRouter || calledApp {
		t.Fatalf("downstream steps executed: migrate=%t router=%t app=%t", calledMigrate, calledRouter, calledApp)
	}
}

func TestBootstrapCenterClosesDBOnMigrationFailure(t *testing.T) {
	cfg := config.CenterConfig{HTTPAddr: ":8080", WebDistDir: "web/dist", DatabaseURL: "postgres://center"}
	db := &fakePostgresDB{}
	wantErr := errors.New("migrate boom")
	calledRouter := false
	calledApp := false

	app, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			return wantErr
		},
		newRouter: func(centerhttp.RouterOptions) http.Handler {
			calledRouter = true
			return http.NewServeMux()
		},
		newApp: func(string, http.Handler, centerapp.Worker) appRunner {
			calledApp = true
			return fakeApp{}
		},
	})
	if err == nil {
		t.Fatal("bootstrapCenter() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "apply migrations") {
		t.Fatalf("bootstrapCenter() error = %q, want wrapped apply migrations error", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("bootstrapCenter() error = %v, want wrapped %v", err, wantErr)
	}
	if app != nil {
		t.Fatal("bootstrapCenter() app != nil, want nil")
	}
	if cleanup != nil {
		t.Fatal("bootstrapCenter() cleanup != nil, want nil")
	}
	if !db.closed {
		t.Fatal("bootstrapCenter() did not close DB on migration failure")
	}
	if calledRouter || calledApp {
		t.Fatalf("downstream steps executed: router=%t app=%t", calledRouter, calledApp)
	}
}

func TestBootstrapCenterBuildsAppOnSuccess(t *testing.T) {
	cfg := config.CenterConfig{HTTPAddr: ":8080", WebDistDir: "web/dist", DatabaseURL: "postgres://center"}
	db := &fakePostgresDB{}
	app := fakeApp{}
	var gotOpts centerhttp.RouterOptions
	var gotAddr string
	var gotHandler http.Handler

	builtApp, cleanup, err := bootstrapCenter(context.Background(), cfg, "dev", bootstrapDeps{
		openPostgres: func(context.Context, string) (postgresDB, error) {
			return db, nil
		},
		applyMigrations: func(context.Context, postgresDB) error {
			return nil
		},
		newRouter: func(opts centerhttp.RouterOptions) http.Handler {
			gotOpts = opts
			return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		},
		newApp: func(addr string, handler http.Handler, worker centerapp.Worker) appRunner {
			gotAddr = addr
			gotHandler = handler
			if worker == nil {
				t.Fatal("worker = nil, want incident background worker")
			}
			return app
		},
	})
	if err != nil {
		t.Fatalf("bootstrapCenter() error = %v, want nil", err)
	}
	if builtApp != app {
		t.Fatal("bootstrapCenter() did not return constructed app")
	}
	if cleanup == nil {
		t.Fatal("bootstrapCenter() cleanup = nil, want non-nil")
	}
	if gotOpts.Version != "dev" {
		t.Fatalf("router version = %q, want %q", gotOpts.Version, "dev")
	}
	if gotOpts.WebDistDir != cfg.WebDistDir {
		t.Fatalf("router webDistDir = %q, want %q", gotOpts.WebDistDir, cfg.WebDistDir)
	}
	if gotOpts.NodesCollectionHandler == nil {
		t.Fatal("router nodes collection handler = nil, want non-nil")
	}
	if gotOpts.NodeItemHandler == nil {
		t.Fatal("router node item handler = nil, want non-nil")
	}
	if gotOpts.TargetsCollectionHandler == nil {
		t.Fatal("router targets collection handler = nil, want non-nil")
	}
	if gotOpts.TargetItemHandler == nil {
		t.Fatal("router target item handler = nil, want non-nil")
	}
	if gotOpts.TargetProbeItemsHandler == nil {
		t.Fatal("router target probe items handler = nil, want non-nil")
	}
	if gotOpts.AgentEnrollHandler == nil {
		t.Fatal("router agent enroll handler = nil, want non-nil")
	}
	if gotOpts.AgentSyncHandler == nil {
		t.Fatal("router agent sync handler = nil, want non-nil")
	}
	if gotAddr != cfg.HTTPAddr {
		t.Fatalf("app addr = %q, want %q", gotAddr, cfg.HTTPAddr)
	}
	if gotHandler == nil {
		t.Fatal("app handler = nil, want non-nil")
	}
	if db.closed {
		t.Fatal("DB closed before cleanup")
	}

	cleanup()
	if !db.closed {
		t.Fatal("cleanup() did not close DB")
	}
}

type fakePostgresDB struct {
	closed bool
}

func (f *fakePostgresDB) Close() {
	f.closed = true
}

func (f *fakePostgresDB) Pool() *pgxpool.Pool {
	return nil
}

type fakeApp struct{}

func (fakeApp) Run(context.Context) error {
	return nil
}
