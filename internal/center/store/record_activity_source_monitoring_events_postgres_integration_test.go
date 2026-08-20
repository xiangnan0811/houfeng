package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/records"
)

const (
	monitoringEventFixtureInstanceID = "mi_3b7d1e9a04c6f285"
	monitoringEventFixtureTargetID   = "tg_9f8e7d6c5b4a3210"
)

// seedMonitoringEventFixture writes one row of each shape the projector has to
// cope with: a contract-complete incident, a target runtime change, a correction
// pointing at the incident, and a pre-contract row with no metadata at all.
func seedMonitoringEventFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	statements := []struct {
		sql       string
		arguments []any
	}{
		{
			sql: `insert into monitoring_instances (
				monitoring_instance_id, display_name, region, city, provider, lifecycle_status
			) values ($1, 'hk-edge-01', 'HK', 'Hong Kong', 'Test Provider', '在用')`,
			arguments: []any{monitoringEventFixtureInstanceID},
		},
		{
			sql: `insert into targets (target_id, name, target_type, host, run_status)
				values ($1, 'edge-https', 'hostname', 'edge.example.com', '启用')`,
			arguments: []any{monitoringEventFixtureTargetID},
		},
		{
			sql: `insert into state_change_events (
				event_id, object_type, object_id, event_type, severity, summary, payload, created_at
			) values (
				'evt_mon_incident', 'monitoring_instance', $1, 'incident_started', '告警',
				'hk-edge-01 CPU 使用率持续超过阈值', jsonb_build_object(
					'event_at', $2::text, 'is_backfilled', false, 'provenance', 'center',
					'producer_version', $4::text, 'rule_version', $5::text,
					'prior_state', 'normal', 'resulting_state', 'alert'
				), $3
			)`,
			arguments: []any{
				monitoringEventFixtureInstanceID,
				base.Format(time.RFC3339Nano),
				base.Add(2 * time.Second),
				incidents.MonitoringEventProducerVersion,
				incidents.MonitoringEventIncidentRuleVersion,
			},
		},
		{
			sql: `insert into state_change_events (
				event_id, object_type, object_id, event_type, severity, summary, payload, created_at
			) values (
				'evt_mon_target', 'target', $1, 'target_paused', '',
				'edge-https 已暂停', jsonb_build_object(
					'event_at', $2::text, 'is_backfilled', false, 'provenance', 'web',
					'producer_version', $4::text, 'rule_version', $5::text,
					'prior_state', '启用', 'resulting_state', '暂停'
				), $3
			)`,
			arguments: []any{
				monitoringEventFixtureTargetID,
				base.Add(time.Minute).Format(time.RFC3339Nano),
				base.Add(time.Minute),
				incidents.MonitoringEventProducerVersion,
				incidents.MonitoringEventTargetRuleVersion,
			},
		},
		{
			// A correction, backfilled, recorded well after it occurred.
			sql: `insert into state_change_events (
				event_id, object_type, object_id, event_type, severity, summary, payload, created_at
			) values (
				'evt_mon_correction', 'monitoring_instance', $1, 'event_corrected', '告警',
				'更正先前事件', jsonb_build_object(
					'event_at', $2::text, 'is_backfilled', true, 'provenance', 'manual_correction',
					'producer_version', $4::text, 'rule_version', $5::text,
					'prior_state', 'normal', 'resulting_state', 'alert',
					'correction_of_event_id', 'evt_mon_incident'
				), $3
			)`,
			arguments: []any{
				monitoringEventFixtureInstanceID,
				base.Add(30 * time.Second).Format(time.RFC3339Nano),
				base.Add(2 * time.Minute),
				incidents.MonitoringEventProducerVersion,
				incidents.MonitoringEventIncidentRuleVersion,
			},
		},
		{
			// Written before the metadata contract existed. It carries no provenance,
			// so nothing can vouch for it.
			sql: `insert into state_change_events (
				event_id, object_type, object_id, event_type, severity, summary, payload, created_at
			) values (
				'evt_mon_legacy', 'monitoring_instance', $1, 'incident_started', '告警',
				'legacy event', jsonb_build_object('incident_id', 'inc_legacy'), $2
			)`,
			arguments: []any{monitoringEventFixtureInstanceID, base.Add(3 * time.Minute)},
		},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.arguments...); err != nil {
			t.Fatalf("seed monitoring event fixture: %v", err)
		}
	}
}

func newMonitoringEventActivitySourceFixture(
	t *testing.T,
	ctx context.Context,
	base time.Time,
) (*MonitoringEventActivitySource, *pgxpool.Pool) {
	t.Helper()
	pool := openActivityTestPool(t, ctx)
	seedMonitoringEventFixture(t, ctx, pool, base)
	source, err := NewMonitoringEventActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new monitoring event activity source: %v", err)
	}
	return source, pool
}

func TestPostgresIntegrationMonitoringEventSourceReadsPayloadSemantics(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newMonitoringEventActivitySourceFixture(t, ctx, base)

	head, err := source.IncrementalHead(ctx)
	if err != nil {
		t.Fatalf("incremental head: %v", err)
	}
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: head.RecordedThrough}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	// Three contract-complete rows; the pre-contract row is excluded.
	if len(candidates) != 3 {
		t.Fatalf("scanned %d candidates, want 3 contract-complete rows: %+v", len(candidates), candidates)
	}

	byEventID := make(map[string]activity.CandidateEvent, len(candidates))
	for _, candidate := range candidates {
		byEventID[candidate.Source.EventID] = candidate
	}
	if _, projected := byEventID["evt_mon_legacy"]; projected {
		t.Fatal("a row with no provable provenance must not reach the timeline")
	}

	incident := byEventID["evt_mon_incident"]
	// The occurrence time lives in the payload while created_at is when the row
	// was written; reading the wrong one puts the event at the wrong place.
	if !incident.EventAt.Equal(base) {
		t.Fatalf("incident event time = %s, want the payload's %s", incident.EventAt, base)
	}
	if !incident.RecordedAt.Equal(base.Add(2 * time.Second)) {
		t.Fatalf("incident recorded time = %s, want created_at %s", incident.RecordedAt, base.Add(2*time.Second))
	}
	if incident.Severity != "warning" {
		t.Fatalf("incident severity = %q, want the projected form of 告警", incident.Severity)
	}
	if incident.Subjects[0].Identity["display_name"] != "hk-edge-01" {
		t.Fatalf("identity snapshot = %v, want the joined display name", incident.Subjects[0].Identity)
	}
	if incident.Presentation.Summary != "" {
		t.Fatalf("the writer's summary must stay out of the projection, got %q", incident.Presentation.Summary)
	}

	// The target event resolves against the target table, not the instance one.
	target := byEventID["evt_mon_target"]
	if target.Subjects[0].Kind != records.SubjectKindTarget || target.Subjects[0].SourceID != monitoringEventFixtureTargetID {
		t.Fatalf("target subject = %+v", target.Subjects[0])
	}
	if target.Subjects[0].Identity["display_name"] != "edge-https" {
		t.Fatalf("target identity = %v, want the target's own name", target.Subjects[0].Identity)
	}

	correction := byEventID["evt_mon_correction"]
	if correction.Corrects != incident.ActivityID {
		t.Fatalf("corrects = %q, want the incident's projected id %q", correction.Corrects, incident.ActivityID)
	}
	if !correction.Backfilled {
		t.Fatalf("a backfilled correction must stay marked late")
	}
	if !correction.EventAt.Equal(base.Add(30 * time.Second)) {
		t.Fatalf("correction event time = %s, want its real occurrence", correction.EventAt)
	}
}

// A row that predates the contract is history and can never improve. Failing on
// it would park the source below it forever, so the scan must step over it and
// keep making progress.
func TestPostgresIntegrationMonitoringEventSourceAdvancesPastPreContractRows(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, pool := newMonitoringEventActivitySourceFixture(t, ctx, base)

	// The pre-contract row is the newest, so a window ending after it must still
	// return the rows recorded before it rather than erroring on the way past.
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(10 * time.Minute)}, 50)
	if err != nil {
		t.Fatalf("scan must step over a pre-contract row, got: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("scanned %d candidates, want 3", len(candidates))
	}

	readiness, err := source.Readiness(ctx, activity.ExportScope{}, activity.NewSettledSourceHead(
		activity.SourceKindMonitoringEvent, base.Add(10*time.Minute), 9000,
	))
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	// The exclusion has to be stated, not inferred: an export that called itself
	// complete while a row was skipped would be making a false claim.
	if readiness.ExcludedRows != 1 {
		t.Fatalf("readiness reports %d excluded rows, want the 1 pre-contract row", readiness.ExcludedRows)
	}

	// A row that claims the contract but breaks it is a live writer bug, and must
	// be loud rather than skipped like old data.
	if _, err := pool.Exec(ctx, `
		insert into state_change_events (
			event_id, object_type, object_id, event_type, severity, summary, payload, created_at
		) values (
			'evt_mon_impossible', 'monitoring_instance', $1, 'incident_started', '告警',
			'impossible transition', jsonb_build_object(
				'event_at', $2::text, 'is_backfilled', false, 'provenance', 'center',
				'producer_version', $4::text, 'rule_version', $5::text,
				'prior_state', 'alert', 'resulting_state', 'alert'
			), $3
		)`,
		monitoringEventFixtureInstanceID,
		base.Add(4*time.Minute).Format(time.RFC3339Nano),
		base.Add(4*time.Minute),
		incidents.MonitoringEventProducerVersion,
		incidents.MonitoringEventIncidentRuleVersion,
	); err != nil {
		t.Fatalf("seed contract-violating row: %v", err)
	}
	if _, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(10 * time.Minute)}, 50); !errors.Is(err, activity.ErrInvalidEventKind) {
		t.Fatalf("error = %v, want a contract violation to fail the scan", err)
	}
}

func TestPostgresIntegrationMonitoringEventSourceHonoursBothWindowBounds(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newMonitoringEventActivitySourceFixture(t, ctx, base)

	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(30 * time.Second)}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != "evt_mon_incident" {
		t.Fatalf("upper bound leaked %d rows: %+v", len(candidates), candidates)
	}

	candidates, err = source.ScanAfter(ctx, activity.ScanWindow{
		From:    base.Add(90 * time.Second),
		Through: base.Add(10 * time.Minute),
	}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != "evt_mon_correction" {
		t.Fatalf("lower bound returned %d rows: %+v", len(candidates), candidates)
	}
}

// The projector scans this table on every pass, and it is the largest log in the
// deployment.
func TestPostgresIntegrationMonitoringEventSourceScanUsesTheRecordedTimeIndex(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, pool := newMonitoringEventActivitySourceFixture(t, ctx, base)

	var indexed bool
	if err := pool.QueryRow(ctx, `
		select exists (
		  select 1 from pg_indexes
		  where schemaname = 'public'
		    and tablename = 'state_change_events'
		    and indexdef like '%created_at%'
		)`).Scan(&indexed); err != nil {
		t.Fatalf("inspect indexes: %v", err)
	}
	if !indexed {
		t.Fatal("state_change_events needs an index on created_at for the projection scan")
	}
}

func TestPostgresIntegrationMonitoringEventSourceProjectsThroughTheProjector(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, pool := newMonitoringEventActivitySourceFixture(t, ctx, base)

	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new activity projection repository: %v", err)
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
	outcome, _ := report.Source(activity.SourceKindMonitoringEvent)
	if outcome.Inserted != 3 {
		t.Fatalf("inserted %d rows, want the 3 contract-complete events", outcome.Inserted)
	}
	if !outcome.CaughtUp {
		t.Fatalf("a pre-contract row must not stop the pass short: %+v", outcome)
	}

	projected := readProjectedRows(t, ctx, repository)
	if len(projected) != 3 {
		t.Fatalf("projected %d rows, want 3", len(projected))
	}
	assertContiguousFromOne(t, projected)

	// A second pass must find nothing new: source identity is what makes a re-read
	// harmless, and the trailing window is re-read on every pass by design.
	second, err := projector.ProjectOnce(ctx, 1)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if secondOutcome, _ := second.Source(activity.SourceKindMonitoringEvent); secondOutcome.Inserted != 0 {
		t.Fatalf("second pass inserted %d rows, want 0", secondOutcome.Inserted)
	}
}
