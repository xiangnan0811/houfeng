//go:build linux

package deploy

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// This fixture uses only its two own sessions and an advisory lock. It requires
// an isolated superuser PostgreSQL URL because pg_control_system is privileged.
func TestPostgresIntegrationOrdinarySettlementWaitsBeforeLock(t *testing.T) {
	url := os.Getenv("HOUFENG_ORDINARY_CONTROL_DATABASE_URL")
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" || url == "" {
		t.Skip("requires isolated PostgreSQL ordinary control fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	observer, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close(context.Background())
	old, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close(context.Background())
	const application = "hf-cold-settlement-fixture"
	if _, err := old.Exec(ctx, `select set_config('application_name',$1,false)`, application); err != nil {
		t.Fatal(err)
	}
	var physical ordinaryPhysicalDatabase
	var address string
	if err := observer.QueryRow(ctx, `select (select system_identifier::text from pg_catalog.pg_control_system()), (select oid from pg_catalog.pg_database where datname=current_database()),host(inet_server_addr()),pg_postmaster_start_time()`).Scan(&physical.SystemIdentifier, &physical.DatabaseOID, &address, &physical.PostmasterStarted); err != nil {
		t.Fatal(err)
	}
	var record ordinaryBackendRecord
	if err := old.QueryRow(ctx, `select a.pid,a.backend_start,current_user::text from pg_catalog.pg_stat_activity a where a.pid=pg_backend_pid()`).Scan(&record.PID, &record.BackendStarted, &record.Role); err != nil {
		t.Fatal(err)
	}
	record.Version, record.OperationID, record.ApplicationName, record.Physical = 1, "fixture", application, physical
	record.ServerAddress = address
	// A blocked backend has not yet acquired the authoritative lock. It must
	// still be observed and joined before the next filesystem transition.
	const lockKey int64 = 873140290153
	if _, err := observer.Exec(ctx, `select pg_advisory_lock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	defer observer.Exec(context.Background(), `select pg_advisory_unlock($1)`, lockKey)
	done := make(chan error, 1)
	go func() {
		_, err := old.Exec(ctx, `select pg_advisory_lock($1)`, lockKey)
		if err == nil {
			_, err = old.Exec(ctx, `select pg_advisory_unlock($1)`, lockKey)
		}
		done <- err
	}()
	short, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	pending, err := ordinaryWaitForBackendSettlementSnapshot(short, observer, physical, []ordinaryBackendRecord{record}, false)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("live backend was not awaited: %v", err)
	}
	if len(pending) != 1 || pending[0].PID != record.PID || !pending[0].BackendStart.Equal(record.BackendStarted) || pending[0].UserName != record.Role || pending[0].Application != application {
		t.Fatalf("missing exact blocked backend identity: %+v", pending)
	}
	if _, err := observer.Exec(ctx, `select pg_advisory_unlock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryWaitForBackendSettlement(ctx, observer, physical, []ordinaryBackendRecord{record}, false); err != nil {
		t.Fatalf("closed backend did not settle: %v", err)
	}
}
