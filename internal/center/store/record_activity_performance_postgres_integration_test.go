package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
)

const (
	activityPerfSubjectID   = "vps_7c2a4e18b09d5f31"
	activityPerfDefaultRows = 1_000_000
	activityPerfPageLimit   = 50
	activityPerfRuns        = 3
	activityPerfBudget      = time.Second
)

// TestPostgresIntegrationRecordActivityPerformance seeds a large subject
// projection and times the first-page ListSubjectPage path across three clean
// runs. Full scale is one million activities; set HOUFENG_ACTIVITY_PERF_SCALE
// (e.g. 0.01) to shrink for local iteration.
func TestPostgresIntegrationRecordActivityPerformance(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	count := activityPerfRowCount(t)

	t.Logf("seeding %d activity rows for subject %s (may take minutes at full scale)", count, activityPerfSubjectID)
	seedStarted := time.Now()
	if err := seedActivityPerformanceProjection(ctx, pool, count); err != nil {
		t.Fatalf("seed activity performance projection: %v", err)
	}
	t.Logf("seed completed in %s", time.Since(seedStarted).Round(time.Millisecond))
	if _, err := pool.Exec(ctx, `analyze public.record_activity_subjects; analyze public.record_activity_projection`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	head, err := repository.LoadPublishedHead(ctx)
	if err != nil {
		t.Fatalf("load published head: %v", err)
	}
	query, err := activity.NormalizeQuery(activity.Query{
		Subject: activity.SubjectRef{Kind: records.SubjectKindVPS, SourceID: activityPerfSubjectID},
		View:    activity.ViewActivity,
		Limit:   activityPerfPageLimit,
	})
	if err != nil {
		t.Fatalf("normalize query: %v", err)
	}
	pageRequest := activity.SubjectPageRequest{
		Query:            query,
		Generation:       head.Generation,
		AsOf:             head.PublishedIngestSequence,
		Limit:            activityPerfPageLimit,
		AuthUnrestricted: true,
	}

	if _, err := repository.ListSubjectPage(ctx, pageRequest); err != nil {
		t.Fatalf("warmup ListSubjectPage: %v", err)
	}

	durations := make([]time.Duration, 0, activityPerfRuns)
	for run := 1; run <= activityPerfRuns; run++ {
		started := time.Now()
		page, err := repository.ListSubjectPage(ctx, pageRequest)
		elapsed := time.Since(started)
		if err != nil {
			t.Fatalf("run %d ListSubjectPage: %v", run, err)
		}
		if !page.SubjectKnown || len(page.Events) != activityPerfPageLimit {
			t.Fatalf("run %d: known=%v events=%d, want known with %d",
				run, page.SubjectKnown, len(page.Events), activityPerfPageLimit)
		}
		durations = append(durations, elapsed)
		t.Logf("run %d ListSubjectPage first %d = %s", run, activityPerfPageLimit, elapsed.Round(time.Millisecond))
	}

	p50, p95, p99 := durationPercentiles(durations)
	t.Logf("ListSubjectPage timings n=%d p50=%s p95=%s p99=%s budget=%s query_path=ListSubjectPage",
		len(durations), p50.Round(time.Millisecond), p95.Round(time.Millisecond),
		p99.Round(time.Millisecond), activityPerfBudget)
	if p95 > activityPerfBudget {
		t.Fatalf("timeline p95 = %s, want ≤ %s", p95, activityPerfBudget)
	}

	plan, err := explainSubjectTimeline(ctx, pool, head.PublishedIngestSequence)
	if err != nil {
		t.Fatalf("explain subject timeline: %v", err)
	}
	t.Logf("EXPLAIN (ANALYZE, BUFFERS):\n%s", plan)
}

func activityPerfRowCount(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("HOUFENG_ACTIVITY_PERF_SCALE"))
	if raw == "" {
		return activityPerfDefaultRows
	}
	scale, err := strconv.ParseFloat(raw, 64)
	if err != nil || scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		t.Fatalf("HOUFENG_ACTIVITY_PERF_SCALE=%q must be a positive float", raw)
	}
	count := int(math.Round(float64(activityPerfDefaultRows) * scale))
	if count < 1000 {
		count = 1000
	}
	return count
}

func seedActivityPerformanceProjection(ctx context.Context, pool *pgxpool.Pool, count int) error {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = 0xab
	}

	// Insert projection rows in batches to keep memory bounded while still using
	// set-based generate_series inside each batch.
	const batchSize = 50_000
	for from := 1; from <= count; from += batchSize {
		to := from + batchSize - 1
		if to > count {
			to = count
		}
		_, err := pool.Exec(ctx, `
			insert into public.record_activity_projection (
			  activity_id, project_id, projection_generation, ingest_sequence,
			  event_kind, event_at, recorded_at, source_kind, source_event_id, source_version,
			  backfilled, severity, presentation_version, presentation_json,
			  auth_scope_digest, canonical_hash
			)
			select
			  'act_' || lpad(to_hex(g), 16, '0'),
			  'default',
			  1,
			  g,
			  'record_revised',
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  'record_domain',
			  'evt_' || lpad(to_hex(g), 16, '0'),
			  1,
			  false,
			  'info',
			  1,
			  '{"version":1,"title":"perf"}'::jsonb,
			  $3::bytea,
			  $3::bytea
			from generate_series($1::bigint, $2::bigint) as g`,
			from, to, digest,
		)
		if err != nil {
			return fmt.Errorf("insert projection [%d,%d]: %w", from, to, err)
		}

		_, err = pool.Exec(ctx, `
			insert into public.record_activity_subjects (
			  activity_id, subject_kind, subject_source_id, relation_role, is_primary,
			  relation_order, identity_snapshot, live_route, tombstoned,
			  projection_generation, ingest_sequence, event_kind, source_kind,
			  event_at, recorded_at, auth_scope_digest, relation_hash
			)
			select
			  'act_' || lpad(to_hex(g), 16, '0'),
			  'vps',
			  $3,
			  'affected',
			  true,
			  0,
			  '{"display_name":"perf-vps"}'::jsonb,
			  '/vps/' || $3,
			  false,
			  1,
			  g,
			  'record_revised',
			  'record_domain',
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  timestamptz '2026-01-01 00:00:00+00' + make_interval(secs => g),
			  $4::bytea,
			  $4::bytea
			from generate_series($1::bigint, $2::bigint) as g`,
			from, to, activityPerfSubjectID, digest,
		)
		if err != nil {
			return fmt.Errorf("insert subjects [%d,%d]: %w", from, to, err)
		}
	}

	_, err := pool.Exec(ctx, `
		update public.record_activity_projection_heads
		set published_ingest_sequence = $1,
		    allocated_ingest_sequence = $1,
		    updated_at = now()
		where project_id = 'default' and head_state = 'active'`,
		count,
	)
	if err != nil {
		return fmt.Errorf("advance published head: %w", err)
	}

	sourceKinds := []string{
		"record_domain", "evidence_snapshot", "asset_history", "monitoring_event", "command_audit",
	}
	for _, kind := range sourceKinds {
		_, err := pool.Exec(ctx, `
			insert into public.record_activity_projection_checkpoints (
			  project_id, projection_generation, source_kind,
			  recorded_through, caught_up, last_success_at, last_error_code, attempt
			) values ('default', 1, $1, now(), true, now(), '', 1)
			on conflict (project_id, projection_generation, source_kind) do update
			set recorded_through = excluded.recorded_through,
			    caught_up = true,
			    last_success_at = excluded.last_success_at,
			    last_error_code = '',
			    updated_at = now()`,
			kind,
		)
		if err != nil {
			return fmt.Errorf("seed checkpoint %s: %w", kind, err)
		}
	}
	return nil
}

func explainSubjectTimeline(ctx context.Context, pool *pgxpool.Pool, asOf uint64) (string, error) {
	rows, err := pool.Query(ctx, `
		explain (analyze, buffers, format text)
		select s.activity_id
		from public.record_activity_subjects s
		where s.subject_kind = 'vps'
		  and s.subject_source_id = $1
		  and s.ingest_sequence <= $2
		order by s.event_at desc, s.recorded_at desc, s.source_kind asc, s.activity_id asc
		limit $3`,
		activityPerfSubjectID, asOf, activityPerfPageLimit+1,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), rows.Err()
}

func durationPercentiles(values []time.Duration) (p50, p95, p99 time.Duration) {
	if len(values) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	percentile := func(p float64) time.Duration {
		if len(sorted) == 1 {
			return sorted[0]
		}
		rank := p * float64(len(sorted)-1)
		lo := int(math.Floor(rank))
		hi := int(math.Ceil(rank))
		if lo == hi {
			return sorted[lo]
		}
		weight := rank - float64(lo)
		return time.Duration(float64(sorted[lo])*(1-weight) + float64(sorted[hi])*weight)
	}
	return percentile(0.50), percentile(0.95), percentile(0.99)
}
