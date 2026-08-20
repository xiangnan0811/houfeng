package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
)

// seedEvidenceActivityFixture writes one snapshot per subject kind plus one whose
// source kind is schema headroom rather than a subject.
func seedEvidenceActivityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	statements := []struct {
		sql       string
		arguments []any
	}{
		{sql: `insert into public.records (record_id) values ('rec_evidencesrc')`},
		{sql: `insert into public.evidence_payloads (
			payload_digest, canonical_size_bytes, compressed_size_bytes, compressed_payload
		) values (decode(repeat('7a', 32), 'hex'), 1024, 1, '\x00'::bytea)`},
		{
			// The columns not listed here take their defaults; the ones listed are the
			// ones the projection actually reads or that constraints require.
			sql: `insert into public.evidence_snapshots (
				snapshot_id, record_id, kind, schema_version, source_kind, source_id,
				subject_identity_snapshot, source_identity_snapshot,
				capture_authorization, capture_authorization_digest,
				requested_started_at, requested_ended_at,
				actual_started_at, actual_ended_at,
				observed_at, captured_at, referenced_at,
				source_revision, source_digest, producer_version, calculation_version,
				actual_precision, bucket_width, unit_semantics, quality, quota_outcome,
				retention, sensitivity_level, redaction, canonical_hash,
				logical_size_bytes, payload_digest, created_at
			) values
			  ('evs_vpswindow01', 'rec_evidencesrc', 'monitoring.host.v1', 2, 'vps', $1,
			   '{"display_name": "hk-edge-01"}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			   decode(repeat('61', 32), 'hex'),
			   $2, $3, $2, $3, $3, $4, $4, 'rev-1', decode(repeat('62', 32), 'hex'),
			   'evidence/v1', 'calc/v1', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			   '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, 'normal', '[]'::jsonb,
			   decode(repeat('7a', 32), 'hex'), 1024, decode(repeat('7a', 32), 'hex'), $4),
			  ('evs_instwindow01', 'rec_evidencesrc', 'monitoring.probe.v1', 1, 'monitoring_instance', $5,
			   '{"display_name": "hk-edge-01-instance"}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			   decode(repeat('63', 32), 'hex'),
			   $2, $3, $2, $3, $3, $4, $4, 'rev-1', decode(repeat('64', 32), 'hex'),
			   'evidence/v1', 'calc/v1', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			   '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, 'normal', '[]'::jsonb,
			   decode(repeat('7a', 32), 'hex'), 1024, decode(repeat('7a', 32), 'hex'), $6),
			  ('evs_headroom001', 'rec_evidencesrc', 'billing.subscription.v1', 1, 'subscription', 'sub_headroom',
			   '{"display_name": "a subscription"}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			   decode(repeat('65', 32), 'hex'),
			   $2, $3, $2, $3, $3, $4, $4, 'rev-1', decode(repeat('66', 32), 'hex'),
			   'evidence/v1', 'calc/v1', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb,
			   '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, 'normal', '[]'::jsonb,
			   decode(repeat('7a', 32), 'hex'), 1024, decode(repeat('7a', 32), 'hex'), $7)`,
			arguments: []any{
				testEvidenceVPSSourceID,
				base.Add(-time.Hour),  // requested/actual window start
				base,                  // window end
				base.Add(time.Minute), // captured/referenced/created
				testMonitoringInstanceSourceID,
				base.Add(2 * time.Minute),
				base.Add(3 * time.Minute),
			},
		},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.arguments...); err != nil {
			t.Fatalf("seed evidence activity fixture: %v", err)
		}
	}
}

func newEvidenceActivitySourceFixture(
	t *testing.T,
	ctx context.Context,
	base time.Time,
) (*EvidenceActivitySource, *pgxpool.Pool) {
	t.Helper()
	pool := openActivityTestPool(t, ctx)
	seedEvidenceActivityFixture(t, ctx, pool, base)
	source, err := NewEvidenceActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new evidence activity source: %v", err)
	}
	return source, pool
}

func TestPostgresIntegrationEvidenceSourceProjectsSubjectScopedCaptures(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newEvidenceActivitySourceFixture(t, ctx, base)

	head, err := source.IncrementalHead(ctx)
	if err != nil {
		t.Fatalf("incremental head: %v", err)
	}
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: head.RecordedThrough}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("scanned %d candidates, want the 2 with subject sources: %+v", len(candidates), candidates)
	}

	bySnapshotID := make(map[string]activity.CandidateEvent, len(candidates))
	for _, candidate := range candidates {
		bySnapshotID[candidate.Source.EventID] = candidate
	}
	if _, projected := bySnapshotID["evs_headroom001"]; projected {
		t.Fatal("a snapshot whose source is not a subject has no timeline to appear on")
	}

	vpsCapture := bySnapshotID["evs_vpswindow01"]
	// The observed window end is the event time, and the row's write time is
	// separately the recorded time.
	if !vpsCapture.EventAt.Equal(base) {
		t.Fatalf("event time = %s, want the observed window end %s", vpsCapture.EventAt, base)
	}
	if !vpsCapture.RecordedAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("recorded time = %s, want the write time", vpsCapture.RecordedAt)
	}
	if vpsCapture.Source.Version != 2 {
		t.Fatalf("source version = %d, want the snapshot's schema version", vpsCapture.Source.Version)
	}
	if vpsCapture.Subjects[0].Kind != records.SubjectKindVPS {
		t.Fatalf("subject kind = %q, want vps", vpsCapture.Subjects[0].Kind)
	}
	if got := vpsCapture.Subjects[0].Identity["display_name"]; got != "hk-edge-01" {
		t.Fatalf("identity = %q, want the captured identity", got)
	}
	if vpsCapture.Presentation.Summary != "monitoring.host.v1" {
		t.Fatalf("summary = %q, want the evidence kind", vpsCapture.Presentation.Summary)
	}

	instanceCapture := bySnapshotID["evs_instwindow01"]
	if instanceCapture.Subjects[0].Kind != records.SubjectKindMonitoringInstance {
		t.Fatalf("subject kind = %q, want monitoring_instance", instanceCapture.Subjects[0].Kind)
	}
}

// Headroom source kinds are excluded, and an export has to be able to say so
// rather than quietly claiming it covered everything.
func TestPostgresIntegrationEvidenceSourceReportsExcludedHeadroomRows(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newEvidenceActivitySourceFixture(t, ctx, base)

	readiness, err := source.Readiness(ctx, activity.ExportScope{}, activity.NewSettledSourceHead(
		activity.SourceKindEvidenceSnapshot, base.Add(time.Hour), 9000,
	))
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if readiness.ExcludedRows != 1 {
		t.Fatalf("readiness reports %d excluded rows, want the 1 headroom snapshot", readiness.ExcludedRows)
	}
	if !readiness.CaughtUp {
		t.Fatal("an excluded row must not leave the source looking behind")
	}
}

func TestPostgresIntegrationEvidenceSourceHonoursBothWindowBounds(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newEvidenceActivitySourceFixture(t, ctx, base)

	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(90 * time.Second)}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != "evs_vpswindow01" {
		t.Fatalf("upper bound leaked %d rows: %+v", len(candidates), candidates)
	}

	candidates, err = source.ScanAfter(ctx, activity.ScanWindow{
		From:    base.Add(90 * time.Second),
		Through: base.Add(time.Hour),
	}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != "evs_instwindow01" {
		t.Fatalf("lower bound returned %d rows: %+v", len(candidates), candidates)
	}
}

// Every existing index on the table leads with source or record, and the
// projection scan filters on neither, so it needs one keyed on write time.
func TestPostgresIntegrationEvidenceSourceScanUsesTheCreatedTimeIndex(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, pool := newEvidenceActivitySourceFixture(t, ctx, base)

	var indexed bool
	if err := pool.QueryRow(ctx, `
		select exists (
		  select 1 from pg_indexes
		  where schemaname = 'public'
		    and tablename = 'evidence_snapshots'
		    and indexname = 'idx_evidence_snapshots_created'
		)`).Scan(&indexed); err != nil {
		t.Fatalf("inspect indexes: %v", err)
	}
	if !indexed {
		t.Fatal("evidence_snapshots needs an index on created_at for the projection scan")
	}
}

func TestPostgresIntegrationEvidenceSourceProjectsThroughTheProjector(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, pool := newEvidenceActivitySourceFixture(t, ctx, base)

	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{source},
		Checkpoints: repository,
		Publisher:   repository,
		BatchSize:   50,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}

	report, err := projector.ProjectOnce(ctx, 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}
	if err := report.Err(); err != nil {
		t.Fatalf("pass failed: %v", err)
	}
	outcome, _ := report.Source(activity.SourceKindEvidenceSnapshot)
	if outcome.Inserted != 2 {
		t.Fatalf("inserted %d rows, want 2", outcome.Inserted)
	}
	if !outcome.CaughtUp {
		t.Fatalf("an excluded row must not stop the pass short: %+v", outcome)
	}

	projected := readProjectedRows(t, ctx, repository)
	if len(projected) != 2 {
		t.Fatalf("projected %d rows, want 2", len(projected))
	}
	assertContiguousFromOne(t, projected)
}
