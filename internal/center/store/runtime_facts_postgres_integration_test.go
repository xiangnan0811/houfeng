package store

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/runtimefacts"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationRuntimeFactsUsesReadAtReceiptCutoffForAllHostSections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	db := openTemporaryRuntimeFactsPostgresDatabase(t, ctx)
	monitoringInstanceID := "mi_runtime_receipt_cutoff"
	base := time.Now().UTC().Truncate(time.Microsecond)
	bindingEpoch := base.Add(-time.Hour)
	historyObservedAt := base.Add(-25 * time.Minute)
	delayedObservedAt := base.Add(-20 * time.Minute)
	liveObservedAt := base.Add(-5 * time.Minute)
	insertRuntimeFactsMonitoringInstance(t, ctx, db, monitoringInstanceID, "fp-current", bindingEpoch)
	insertRuntimeFactsHostSample(t, ctx, db, monitoringInstanceID, historyObservedAt, historyObservedAt.Add(time.Second), "fp-old", 7, 7, false, "receipt-cutoff-old-binding")
	insertRuntimeFactsHostSample(t, ctx, db, monitoringInstanceID, liveObservedAt, liveObservedAt.Add(time.Second), "fp-current", 11, 11, false, "receipt-cutoff-live")

	insertedDelayed := false
	queryer := &runtimeFactsReceiptCutoffQueryer{
		db: db,
		afterLatestScan: func() error {
			insertedDelayed = true
			return insertRuntimeFactsHostSampleValue(ctx, db, monitoringInstanceID, delayedObservedAt, time.Now().UTC().Add(time.Hour), "fp-current", 99, 99, true, "receipt-cutoff-delayed")
		},
	}
	repository := &PostgresRuntimeFactsRepository{db: queryer}

	facts, err := repository.GetMonitoringInstanceRuntimeFacts(ctx, monitoringInstanceID, runtimefacts.WindowRequest{
		Key:         "realtime",
		StartedAt:   base.Add(-30 * time.Minute),
		EndedAt:     base.Add(10 * time.Minute),
		BucketCount: 4,
	})
	if err != nil {
		t.Fatalf("GetMonitoringInstanceRuntimeFacts: %v", err)
	}
	if !insertedDelayed {
		t.Fatal("interleaving delayed sample was not committed after the authoritative latest query")
	}
	if facts.LatestHostSample == nil || facts.LatestHostSample.SyncBatchID != "receipt-cutoff-live" || facts.LatestHostSample.IsBackfilled {
		t.Fatalf("latest host sample = %#v, want pre-read live sample", facts.LatestHostSample)
	}
	if facts.Window.SampleCount != 2 {
		t.Fatalf("window sample count = %d, want two pre-read instance-scoped samples", facts.Window.SampleCount)
	}
	if facts.Window.AvailableStartedAt == nil || !facts.Window.AvailableStartedAt.Equal(historyObservedAt) ||
		facts.Window.AvailableEndedAt == nil || !facts.Window.AvailableEndedAt.Equal(liveObservedAt) {
		t.Fatalf("window availability = %v..%v, want instance history %v..%v", facts.Window.AvailableStartedAt, facts.Window.AvailableEndedAt, historyObservedAt, liveObservedAt)
	}
	if len(facts.HostMetricPoints) != 4 {
		t.Fatalf("len(host metric points) = %d, want all four buckets", len(facts.HostMetricPoints))
	}
	for index, point := range facts.HostMetricPoints {
		wantCount := 0
		if index == 0 || index == 2 {
			wantCount = 1
		}
		if point.SampleCount != wantCount {
			t.Fatalf("host metric point %d sample count = %d, want %d", index, point.SampleCount, wantCount)
		}
		if wantCount == 0 && (point.CPUUsagePct != nil || point.NetInBytesPerSec != nil || point.NetOutBytesPerSec != nil) {
			t.Fatalf("host metric point %d = %#v, want nullable gap", index, point)
		}
	}
	historyPoint := facts.HostMetricPoints[0]
	if historyPoint.CPUUsagePct == nil || *historyPoint.CPUUsagePct != 7 || historyPoint.NetInBytesPerSec == nil || *historyPoint.NetInBytesPerSec != 7 {
		t.Fatalf("historical host metric point = %#v, want retained cross-binding history", historyPoint)
	}
	livePoint := facts.HostMetricPoints[2]
	if livePoint.CPUUsagePct == nil || *livePoint.CPUUsagePct != 11 || livePoint.NetInBytesPerSec == nil || *livePoint.NetInBytesPerSec != 11 {
		t.Fatalf("live host metric point = %#v, want only pre-read live values", livePoint)
	}
	if len(facts.RecentHostSamples) != 2 ||
		facts.RecentHostSamples[0].SyncBatchID != "receipt-cutoff-old-binding" ||
		facts.RecentHostSamples[0].Fingerprint != "fp-old" ||
		facts.RecentHostSamples[1].SyncBatchID != "receipt-cutoff-live" {
		t.Fatalf("recent host samples = %#v, want retained cross-binding history plus live sample", facts.RecentHostSamples)
	}
}

type runtimeFactsReceiptCutoffQueryer struct {
	db              *pgxpool.Pool
	afterLatestScan func() error
}

func (q *runtimeFactsReceiptCutoffQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := q.db.QueryRow(ctx, sql, args...)
	if sql != runtimeFactsLatestHostSampleSQL {
		return row
	}
	return runtimeFactsReceiptCutoffRow{row: row, afterScan: q.afterLatestScan}
}

func (q *runtimeFactsReceiptCutoffQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return q.db.Query(ctx, sql, args...)
}

type runtimeFactsReceiptCutoffRow struct {
	row       pgx.Row
	afterScan func() error
}

func (r runtimeFactsReceiptCutoffRow) Scan(dest ...any) error {
	if err := r.row.Scan(dest...); err != nil {
		return err
	}
	if r.afterScan != nil {
		return r.afterScan()
	}
	return nil
}

func insertRuntimeFactsMonitoringInstance(t *testing.T, ctx context.Context, db *pgxpool.Pool, id, fingerprint string, bindingEpoch time.Time) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into monitoring_instances (
			monitoring_instance_id, display_name, "group", region, city, provider,
			lifecycle_status, monitoring_status, binding_status, binding_fingerprint,
			binding_epoch_started_at
		) values ($1, $2, 'test', 'test-region', 'test-city', 'test-provider',
			'在用', '启用', '已绑定', $3, $4)
	`, id, id, fingerprint, bindingEpoch); err != nil {
		t.Fatalf("insert monitoring instance %q: %v", id, err)
	}
}

func insertRuntimeFactsHostSample(t *testing.T, ctx context.Context, db *pgxpool.Pool, id string, observedAt, receivedAt time.Time, fingerprint string, cpuUsagePct, networkRate int64, backfilled bool, syncBatchID string) {
	t.Helper()
	if err := insertRuntimeFactsHostSampleValue(ctx, db, id, observedAt, receivedAt, fingerprint, cpuUsagePct, networkRate, backfilled, syncBatchID); err != nil {
		t.Fatalf("insert host sample %q/%q: %v", id, syncBatchID, err)
	}
}

func insertRuntimeFactsHostSampleValue(ctx context.Context, db *pgxpool.Pool, id string, observedAt, receivedAt time.Time, fingerprint string, cpuUsagePct, networkRate int64, backfilled bool, syncBatchID string) error {
	_, err := db.Exec(ctx, `
		insert into host_samples (
			monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
			mem_total_bytes, swap_used_pct, disk_used_pct, disk_total_bytes, inode_used_pct,
			net_in_bytes_per_sec, net_out_bytes_per_sec, network_rates_valid,
			cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec,
			disk_busy_pct, uptime_seconds, maintenance_context, is_backfilled, sync_batch_id
		) values (
			$1, $2, $3, 'test-agent', $4,
			$5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			$6, $6, true, 0, 0, 0, 0, 0, $7, false, $8, $9
		)
	`, id, observedAt, receivedAt, fingerprint, float64(cpuUsagePct), networkRate, cpuUsagePct, backfilled, syncBatchID)
	return err
}

func openTemporaryRuntimeFactsPostgresDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for runtime facts PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for runtime facts PostgreSQL integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_runtime_facts_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres database %q: %v", databaseName, err)
	}
	if err := storemigrate.Apply(ctx, testPool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return testPool
}
