//go:build linux

package deploy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegrationMigrationManagementConnectionCannotReconnect(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires the isolated PostgreSQL integration runner")
	}
	url := os.Getenv("HOUFENG_RECOVERY_CONTROL_DATABASE_URL")
	if url == "" || os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" {
		t.Fatal("missing isolated recovery-control fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := ledgerParsePool(url)
	if err != nil {
		t.Fatal("parse isolated management connection", err)
	}
	ledgerLimitManagementConnections(config)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	managed, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pid int32
	if err := managed.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&pid); err != nil || pid <= 0 {
		managed.Release()
		t.Fatal("read management backend", err)
	}
	control, err := pgx.Connect(ctx, url)
	if err != nil {
		managed.Release()
		t.Fatal(err)
	}
	defer control.Close(context.Background())
	var terminated bool
	if err := control.QueryRow(ctx, `select pg_catalog.pg_terminate_backend($1)`, pid).Scan(&terminated); err != nil || !terminated {
		managed.Release()
		t.Fatal("terminate original management backend", err)
	}
	_ = managed.Conn().Close(ctx)
	managed.Release()
	attempt, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	var replacement int32
	if err := pool.QueryRow(attempt, `select pg_catalog.pg_backend_pid()`).Scan(&replacement); err == nil {
		t.Fatalf("management pool accepted replacement backend %d", replacement)
	}
}
