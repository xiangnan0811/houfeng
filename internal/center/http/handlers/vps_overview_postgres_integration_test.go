package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	centerrecords "houfeng/internal/center/records"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
	overviewperftest "houfeng/internal/center/testsupport/vpsoverviewperf"
	"houfeng/internal/center/vpsoverview"
)

const (
	vpsOverviewPerfSubjectID = "vps_7c2a4e18b09d5f31"
	vpsOverviewPerfRuns      = 3
	vpsOverviewPerfBudget    = 750 * time.Millisecond
)

// TestPostgresIntegrationVPSOverviewPerformance times GET /api/vps/:id/overview
// against a large subject activity projection. Scale with HOUFENG_ACTIVITY_PERF_SCALE.
func TestPostgresIntegrationVPSOverviewPerformance(t *testing.T) {
	ctx := context.Background()
	pool := openVPSOverviewPerfPool(t, ctx)
	count := overviewPerfRowCount(t)

	t.Logf("seeding %d activity rows for overview HTTP performance", count)
	if err := seedOverviewPerfActivities(ctx, pool, count); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := overviewperftest.SeedAuthority(ctx, pool, vpsOverviewPerfSubjectID); err != nil {
		t.Fatalf("seed overview authority: %v", err)
	}
	if err := overviewperftest.PrepareMeasurement(ctx, pool); err != nil {
		t.Fatalf("prepare measurement: %v", err)
	}

	trace := &overviewperftest.QueryTrace{}
	runtimePool := overviewperftest.OpenTracedPool(t, ctx, pool, trace)
	vpsRepository := store.NewPostgresVPSAssetRepository(runtimePool)
	sources, err := store.NewVPSOverviewRepository(
		vpsRepository,
		store.NewPostgresVPSMonitoringInstanceLinkRepository(runtimePool),
		store.NewPostgresIPQualityRepository(runtimePool),
		store.NewPostgresSubscriptionRepository(runtimePool),
		store.NewPostgresAssetServiceRepository(runtimePool),
		store.NewPostgresAssetDomainRepository(runtimePool),
	)
	if err != nil {
		t.Fatalf("overview sources: %v", err)
	}
	subjects, err := centerrecords.NewSubjectAdapterRegistry([]centerrecords.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(vpsRepository),
		store.NewMonitoringInstanceRecordSubjectAdapter(store.NewPostgresMonitoringInstanceRepository(runtimePool)),
		store.NewTargetRecordSubjectAdapter(store.NewPostgresTargetRepository(runtimePool)),
	})
	if err != nil {
		t.Fatalf("subject registry: %v", err)
	}
	activityRepository, err := store.NewActivityProjectionRepository(runtimePool)
	if err != nil {
		t.Fatalf("repository: %v", err)
	}
	codec, err := activity.NewCursorCodec([]byte("houfeng-overview-http-perf-hmac-key!!"))
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	activityService, err := activity.NewService(
		activityRepository, activityRepository, store.NewActivityLiveSubjectResolver(subjects), codec,
	)
	if err != nil {
		t.Fatalf("activity service: %v", err)
	}
	overviewService, err := vpsoverview.NewService(sources, activityService)
	if err != nil {
		t.Fatalf("overview service: %v", err)
	}

	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("actor: %v", err)
	}
	handler := VPSOverview(overviewService)

	// Warmup
	warmup := serveOverview(t, handler, actor)
	if warmup.Code != http.StatusOK {
		t.Fatalf("warmup status=%d body=%s", warmup.Code, warmup.Body.String())
	}
	var warmupOverview vpsoverview.Overview
	if err := json.Unmarshal(warmup.Body.Bytes(), &warmupOverview); err != nil {
		t.Fatalf("warmup decode: %v", err)
	}
	if err := overviewperftest.ValidateHealthyOverview(warmupOverview, vpsOverviewPerfSubjectID); err != nil {
		t.Fatalf("warmup unhealthy overview: %v", err)
	}
	if err := trace.Snapshot().Validate(); err != nil {
		t.Fatalf("warmup query contract: %v", err)
	}
	trace.Reset()

	durations := make([]time.Duration, 0, vpsOverviewPerfRuns)
	for run := 1; run <= vpsOverviewPerfRuns; run++ {
		trace.Reset()
		started := time.Now()
		recorder := serveOverview(t, handler, actor)
		elapsed := time.Since(started)
		if recorder.Code != http.StatusOK {
			t.Fatalf("run %d status=%d body=%s", run, recorder.Code, recorder.Body.String())
		}
		var overview vpsoverview.Overview
		if err := json.Unmarshal(recorder.Body.Bytes(), &overview); err != nil {
			t.Fatalf("run %d decode: %v", run, err)
		}
		if err := overviewperftest.ValidateHealthyOverview(overview, vpsOverviewPerfSubjectID); err != nil {
			t.Fatalf("run %d unhealthy overview: %v", run, err)
		}
		queryStats := trace.Snapshot()
		if err := queryStats.Validate(); err != nil {
			t.Fatalf("run %d query contract: %v", run, err)
		}
		durations = append(durations, elapsed)
		t.Logf("run %d GET overview = %s (queries=%d query_errors=%d query_error_rate=%.2f%%)",
			run, elapsed.Round(time.Millisecond), queryStats.Count, queryStats.Errors, queryStats.ErrorRatePercent())
	}

	p95 := overviewPerfP95(durations)
	t.Logf("overview HTTP p95=%s budget=%s", p95.Round(time.Millisecond), vpsOverviewPerfBudget)
	if p95 > vpsOverviewPerfBudget {
		t.Fatalf("overview p95 = %s, want ≤ %s", p95, vpsOverviewPerfBudget)
	}
}

func serveOverview(t *testing.T, handler http.Handler, actor recordauth.ActorScope) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/vps/"+vpsOverviewPerfSubjectID+"/overview", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func overviewPerfRowCount(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("HOUFENG_ACTIVITY_PERF_SCALE"))
	const full = 1_000_000
	if raw == "" {
		return full
	}
	var scale float64
	if _, err := fmt.Sscanf(raw, "%f", &scale); err != nil || scale <= 0 {
		t.Fatalf("HOUFENG_ACTIVITY_PERF_SCALE=%q invalid", raw)
	}
	count := int(float64(full) * scale)
	if count < 1000 {
		count = 1000
	}
	return count
}

func overviewPerfP95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	// Use the nearest-rank definition so a small sample never rounds down to a
	// value below the observed 95th percentile.
	rank := (95*len(sorted) + 99) / 100
	return sorted[rank-1]
}

func TestOverviewPerfP95UsesConservativeNearestRank(t *testing.T) {
	values := []time.Duration{507 * time.Millisecond, 508 * time.Millisecond, 522 * time.Millisecond}

	if got, want := overviewPerfP95(values), 522*time.Millisecond; got != want {
		t.Fatalf("overviewPerfP95() = %s, want %s", got, want)
	}
}

func openVPSOverviewPerfPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for VPS overview performance tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for VPS overview performance tests")
	}
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	databaseName := fmt.Sprintf("houfeng_overview_perf_%d_%d", time.Now().UnixNano(), os.Getpid())
	if _, err := adminPool.Exec(ctx, `create database `+pgx.Identifier{databaseName}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = adminPool.Exec(cleanupCtx, `drop database if exists `+pgx.Identifier{databaseName}.Sanitize()+` with (force)`)
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	pool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := storemigrate.Apply(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ('default', 1, 0, 0)`); err != nil {
		t.Fatalf("seed head: %v", err)
	}
	return pool
}

func seedOverviewPerfActivities(ctx context.Context, pool *pgxpool.Pool, count int) error {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = 0xab
	}
	const batchSize = 50_000
	for from := 1; from <= count; from += batchSize {
		to := from + batchSize - 1
		if to > count {
			to = count
		}
		if _, err := pool.Exec(ctx, `
			insert into public.record_activity_projection (
			  activity_id, project_id, projection_generation, ingest_sequence,
			  event_kind, event_at, recorded_at, source_kind, source_event_id, source_version,
			  backfilled, severity, presentation_version, presentation_json,
			  auth_scope_digest, canonical_hash
			)
			select
			  'act_' || lpad(to_hex(g), 16, '0'),
			  'default', 1, g, 'record_revised',
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  'record_domain', 'evt_' || lpad(to_hex(g), 16, '0'), 1,
			  false, 'info', 1, '{"version":1,"title":"perf"}'::jsonb,
			  $3::bytea, $3::bytea
			from generate_series($1::bigint, $2::bigint) as g`, from, to, digest); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `
			insert into public.record_activity_subjects (
			  activity_id, subject_kind, subject_source_id, relation_role, is_primary,
			  relation_order, identity_snapshot, live_route, tombstoned,
			  projection_generation, ingest_sequence, event_kind, source_kind,
			  event_at, recorded_at, auth_scope_digest, relation_hash
			)
			select
			  'act_' || lpad(to_hex(g), 16, '0'),
			  'vps', $3, 'affected', true, 0,
			  '{"display_name":"perf-vps"}'::jsonb, '/vps/' || $3, false,
			  1, g, 'record_revised', 'record_domain',
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  $4::bytea, $4::bytea
			from generate_series($1::bigint, $2::bigint) as g`,
			from, to, vpsOverviewPerfSubjectID, digest); err != nil {
			return err
		}
	}
	_, err := pool.Exec(ctx, `
		update public.record_activity_projection_heads
		set published_ingest_sequence = $1, allocated_ingest_sequence = $1, updated_at = now()
		where project_id = 'default' and head_state = 'active'`, count)
	return err
}
