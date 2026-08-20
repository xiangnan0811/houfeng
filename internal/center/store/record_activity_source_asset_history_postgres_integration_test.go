package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
)

// seedAssetHistoryFixture writes one row in each of the four unioned tables, all
// for the same VPS, so the scan has to interleave them by write time.
func seedAssetHistoryFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	statements := []struct {
		sql       string
		arguments []any
	}{
		{
			sql: `insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status)
				values ($1, 'hk-edge-01', 'active', 'in_use')`,
			arguments: []any{testEvidenceVPSSourceID},
		},
		{
			sql: `insert into subscriptions (
				subscription_id, vps_id, price, currency, billing_cycle, billing_months,
				monthly_price, billing_period_unit, billing_period_length, started_at,
				renew_at, status, created_at, updated_at
			) values ('sub_assethist', $1, 20, 'USD', 'monthly', 1, 20, 'month', 1,
				$2::date, $2::date + interval '2 months', 'active', $2, $2)`,
			arguments: []any{testEvidenceVPSSourceID, base},
		},
		{
			sql: `insert into renewal_decisions (
				decision_id, vps_id, from_decision, to_decision, reason, decided_at, created_at
			) values ('dec_assethist', $1, 'unreviewed', 'keep', '涨价后仍保留', $2, $3)`,
			arguments: []any{testEvidenceVPSSourceID, base, base.Add(time.Second)},
		},
		{
			sql: `insert into price_histories (
				price_history_id, subscription_id, vps_id,
				from_price, to_price, from_currency, to_currency,
				from_billing_months, to_billing_months,
				from_monthly_price, to_monthly_price,
				from_auto_renew, to_auto_renew,
				from_auto_renew_cancelled, to_auto_renew_cancelled,
				from_status, to_status,
				changed_at, created_at
			) values ('ph_assethist', 'sub_assethist', $1,
				20, 24, 'USD', 'USD', 1, 1, 20, 24, true, true, false, false,
				'active', 'active', $2, $3)`,
			arguments: []any{testEvidenceVPSSourceID, base.Add(time.Minute), base.Add(time.Minute)},
		},
		{
			sql: `insert into ip_histories (
				ip_history_id, vps_id, from_ipv4, to_ipv4, changed_at, created_at
			) values ('iph_assethist', $1, '203.0.113.7', '203.0.113.9', $2, $3)`,
			arguments: []any{testEvidenceVPSSourceID, base.Add(2 * time.Minute), base.Add(2 * time.Minute)},
		},
		{
			sql: `insert into vps_spec_snapshots (
				snapshot_id, vps_id, product_name, ssh_host, ssh_port, ssh_user,
				os_name, virtualization, captured_at, created_at
			) values ('snap_assethist', $1, 'KVM 2C2G', 'edge.example.com', 22, 'root',
				'Debian 12', 'kvm', $2, $3)`,
			arguments: []any{testEvidenceVPSSourceID, base.Add(3 * time.Minute), base.Add(3 * time.Minute)},
		},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.arguments...); err != nil {
			t.Fatalf("seed asset history fixture: %v", err)
		}
	}
}

func newAssetHistoryActivitySourceFixture(
	t *testing.T,
	ctx context.Context,
	base time.Time,
) (*AssetHistoryActivitySource, *pgxpool.Pool) {
	t.Helper()
	pool := openActivityTestPool(t, ctx)
	seedAssetHistoryFixture(t, ctx, pool, base)
	source, err := NewAssetHistoryActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new asset history activity source: %v", err)
	}
	return source, pool
}

func TestPostgresIntegrationAssetHistorySourceUnionsAllFourTables(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newAssetHistoryActivitySourceFixture(t, ctx, base)

	head, err := source.IncrementalHead(ctx)
	if err != nil {
		t.Fatalf("incremental head: %v", err)
	}
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: head.RecordedThrough}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 4 {
		t.Fatalf("scanned %d candidates, want one per unioned table: %+v", len(candidates), candidates)
	}

	// One ordered page across four tables is the point of unioning them: a
	// per-table position could disagree with the others.
	for index := 1; index < len(candidates); index++ {
		if candidates[index-1].RecordedAt.After(candidates[index].RecordedAt) {
			t.Fatalf("page is not ordered by write time: %s then %s",
				candidates[index-1].RecordedAt, candidates[index].RecordedAt)
		}
	}

	byEventID := make(map[string]activity.CandidateEvent, len(candidates))
	for _, candidate := range candidates {
		byEventID[candidate.Source.EventID] = candidate
		if candidate.Subjects[0].Kind != records.SubjectKindVPS {
			t.Fatalf("%s subject kind = %q, want vps", candidate.Source.EventID, candidate.Subjects[0].Kind)
		}
		if candidate.Subjects[0].Identity["display_name"] != "hk-edge-01" {
			t.Fatalf("%s identity = %v", candidate.Source.EventID, candidate.Subjects[0].Identity)
		}
	}

	renewal := byEventID[assetHistoryEventID(assetFactRenewalDecision, "dec_assethist")]
	if renewal.Presentation.Summary != "keep" {
		t.Fatalf("renewal summary = %q, want the decision enum", renewal.Presentation.Summary)
	}
	// The decision was made before the row was written, and the timeline sorts by
	// when it was decided.
	if !renewal.EventAt.Equal(base) {
		t.Fatalf("renewal event time = %s, want the decided time %s", renewal.EventAt, base)
	}

	// The seeded rows carry a real IP change, an SSH host, a price change, and a
	// free-text reason. None of it may appear anywhere in what gets projected.
	for _, candidate := range candidates {
		for _, leaked := range []string{
			"203.0.113.7", "203.0.113.9", "edge.example.com", "root",
			"KVM 2C2G", "Debian 12", "24", "涨价后仍保留",
		} {
			if candidate.Presentation.Title == leaked || candidate.Presentation.Summary == leaked {
				t.Fatalf("%s projected the value %q", candidate.Source.EventID, leaked)
			}
		}
	}
}

func TestPostgresIntegrationAssetHistorySourceHonoursBothWindowBounds(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newAssetHistoryActivitySourceFixture(t, ctx, base)

	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(90 * time.Second)}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("upper bound returned %d rows, want the 2 written by then: %+v", len(candidates), candidates)
	}

	candidates, err = source.ScanAfter(ctx, activity.ScanWindow{
		From:    base.Add(150 * time.Second),
		Through: base.Add(time.Hour),
	}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("lower bound returned %d rows, want 1: %+v", len(candidates), candidates)
	}
	if candidates[0].Source.EventID != assetHistoryEventID(assetFactSpecSnapshot, "snap_assethist") {
		t.Fatalf("lower bound returned %q", candidates[0].Source.EventID)
	}
}

// Every index on the four tables leads with vps_id, and a scan ordered by write
// time across all four filters on none of them.
func TestPostgresIntegrationAssetHistorySourceScanUsesTheCreatedTimeIndexes(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, pool := newAssetHistoryActivitySourceFixture(t, ctx, base)

	for _, expected := range []struct {
		table string
		index string
	}{
		{table: "renewal_decisions", index: "idx_renewal_decisions_created"},
		{table: "price_histories", index: "idx_price_histories_created"},
		{table: "ip_histories", index: "idx_ip_histories_created"},
		{table: "vps_spec_snapshots", index: "idx_vps_spec_snapshots_created"},
	} {
		var indexed bool
		if err := pool.QueryRow(ctx, `
			select exists (
			  select 1 from pg_indexes
			  where schemaname = 'public' and tablename = $1 and indexname = $2
			)`, expected.table, expected.index).Scan(&indexed); err != nil {
			t.Fatalf("inspect %s indexes: %v", expected.table, err)
		}
		if !indexed {
			t.Errorf("%s needs %s for the projection scan", expected.table, expected.index)
		}
	}
}

func TestPostgresIntegrationAssetHistorySourceProjectsThroughTheProjector(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, pool := newAssetHistoryActivitySourceFixture(t, ctx, base)

	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{source},
		Checkpoints: repository,
		Publisher:   repository,
		BatchSize:   3,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}

	inserted := 0
	for pass := 0; pass < 3; pass++ {
		report, err := projector.ProjectOnce(ctx, 1)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if err := report.Err(); err != nil {
			t.Fatalf("pass %d failed: %v", pass, err)
		}
		outcome, _ := report.Source(activity.SourceKindAssetHistory)
		inserted += outcome.Inserted
	}
	if inserted != 4 {
		t.Fatalf("inserted %d rows across passes, want 4", inserted)
	}

	projected := readProjectedRows(t, ctx, repository)
	if len(projected) != 4 {
		t.Fatalf("projected %d rows, want 4", len(projected))
	}
	assertContiguousFromOne(t, projected)
}
