package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationMonitoringInstanceSparklinesUseNullableBuckets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openTemporaryMonitoringSparklinesDatabase(t, ctx)
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	const monitoringInstanceID = "mi_sparkline_regression"
	if _, err := db.Exec(ctx, `
		insert into monitoring_instances (
			monitoring_instance_id, display_name, "group", region, city, provider, lifecycle_status
		) values ($1, 'Sparkline Regression', 'test', 'test-region', 'test-city', 'test-provider', '在用')
	`, monitoringInstanceID); err != nil {
		t.Fatalf("insert monitoring instance: %v", err)
	}

	referenceNow := time.Now().UTC()
	for _, sample := range []struct {
		observedAt time.Time
		cpuUsage   float64
	}{
		{observedAt: referenceNow.Add(-21 * time.Hour), cpuUsage: 0},
		{observedAt: referenceNow.Add(-9 * time.Hour), cpuUsage: 10},
		{observedAt: referenceNow.Add(-9*time.Hour + time.Minute), cpuUsage: 20},
	} {
		if _, err := db.Exec(ctx, `
			insert into host_samples (
				monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
				cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
				swap_used_pct, disk_used_pct, inode_used_pct, net_in_bytes_per_sec,
				net_out_bytes_per_sec, cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec,
				disk_write_bytes_per_sec, disk_busy_pct, uptime_seconds, sync_batch_id
			) values (
				$1, $2, $2, 'test-agent', 'test-fingerprint', $3,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 'sparkline-regression'
			)
		`, monitoringInstanceID, sample.observedAt, sample.cpuUsage); err != nil {
			t.Fatalf("insert host sample at %s: %v", sample.observedAt.Format(time.RFC3339), err)
		}
	}

	repository := store.NewPostgresMonitoringInstanceSparklinesRepository(db)
	since := referenceNow.Add(-24 * time.Hour)
	result, err := repository.GetMonitoringInstanceSparklines(ctx, []string{"cpu_usage_pct"}, since, 4)
	if err != nil {
		t.Fatalf("GetMonitoringInstanceSparklines: %v", err)
	}
	assertNullableSparklineBuckets(t, result[monitoringInstanceID]["cpu_usage_pct"])

	handler := MonitoringInstanceSparklines(repository)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet,
		"/api/monitoring-instances/sparklines?metrics=cpu_usage_pct&window=24h&downsample=4",
		nil,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("handler status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response sparklinesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode handler response: %v", err)
	}
	assertNullableSparklineBuckets(t, response.MonitoringInstances[monitoringInstanceID]["cpu_usage_pct"])
	if !strings.Contains(recorder.Body.String(), `"cpu_usage_pct":[0,null,15,null]`) {
		t.Fatalf("handler response did not preserve zero, average, and null buckets: %s", recorder.Body.String())
	}

	// Set HOUFENG_SPARKLINES_ARTIFACT to persist this sanitized response for browser replay.
	if artifactPath := strings.TrimSpace(os.Getenv("HOUFENG_SPARKLINES_ARTIFACT")); artifactPath != "" {
		if !filepath.IsAbs(artifactPath) {
			artifactPath = filepath.Join(sparklinesRepositoryRoot(t), artifactPath)
		}
		if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
			t.Fatalf("create response output directory: %v", err)
		}
		if err := os.WriteFile(artifactPath, recorder.Body.Bytes(), 0o644); err != nil {
			t.Fatalf("write sanitized handler response: %v", err)
		}
	}
}

func assertNullableSparklineBuckets(t *testing.T, buckets []*float64) {
	t.Helper()
	if len(buckets) != 4 {
		t.Fatalf("bucket count = %d, want 4", len(buckets))
	}
	if buckets[0] == nil || *buckets[0] != 0 {
		t.Fatalf("zero bucket = %#v, want non-nil pointer to 0", buckets[0])
	}
	if buckets[1] != nil {
		t.Fatalf("internal empty bucket = %v, want nil", *buckets[1])
	}
	if buckets[2] == nil || *buckets[2] != 15 {
		t.Fatalf("average bucket = %#v, want 15", buckets[2])
	}
	if buckets[3] != nil {
		t.Fatalf("trailing empty bucket = %v, want nil", *buckets[3])
	}
}

func openTemporaryMonitoringSparklinesDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for monitoring sparkline PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for monitoring sparkline PostgreSQL integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_sparklines_%d_%d", time.Now().UnixNano(), os.Getpid())
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
	return testPool
}

func sparklinesRepositoryRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			return cwd
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			t.Fatal("could not locate repository root")
		}
		cwd = parent
	}
}
