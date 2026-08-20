package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
)

const commandAuditFixtureActorID = "usr_000000000000000000000001"

// seedCommandAuditFixture writes one action's three phases plus a second action
// that failed, which is the case an operator scans a timeline to find.
func seedCommandAuditFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, base time.Time) {
	t.Helper()
	statements := []struct {
		sql       string
		arguments []any
	}{
		{
			sql: `insert into monitoring_instances (
				monitoring_instance_id, display_name, region, city, provider, lifecycle_status
			) values ($1, 'hk-edge-01-renamed', 'HK', 'Hong Kong', 'Test Provider', '在用')`,
			arguments: []any{monitoringEventFixtureInstanceID},
		},
		{
			sql: `insert into users (user_id, username, password_hash, display_name)
				values ($1, 'alan', 'hash', 'Alan')`,
			arguments: []any{commandAuditFixtureActorID},
		},
		{
			sql: `insert into monitoring_instance_command_action_audit (
				audit_id, action_id, monitoring_instance_id, command_id, sensitivity,
				event_type, actor_user_id, source, exit_code, occurred_at,
				monitoring_instance_name_snapshot, actor_username_snapshot, actor_display_name_snapshot
			) values
			  ('cmda_queued', 'act_one', $1, 'df_h', 'standard', 'queued', $2, 'web',
			   null, $3, 'hk-edge-01', 'alan', 'Alan'),
			  ('cmda_dispatched', 'act_one', $1, 'df_h', 'standard', 'dispatched', null, 'agent_sync',
			   null, $4, 'hk-edge-01', '', ''),
			  ('cmda_completed', 'act_one', $1, 'df_h', 'standard', 'completed', null, 'agent_sync',
			   0, $5, 'hk-edge-01', '', ''),
			  ('cmda_failed', 'act_two', $1, 'journalctl_u', 'sensitive', 'completed', null, 'agent_sync',
			   1, $6, 'hk-edge-01', '', '')`,
			arguments: []any{
				monitoringEventFixtureInstanceID,
				commandAuditFixtureActorID,
				base,
				base.Add(time.Second),
				base.Add(5 * time.Second),
				base.Add(time.Minute),
			},
		},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.arguments...); err != nil {
			t.Fatalf("seed command audit fixture: %v", err)
		}
	}
}

func newCommandAuditActivitySourceFixture(
	t *testing.T,
	ctx context.Context,
	base time.Time,
) (*CommandAuditActivitySource, *pgxpool.Pool) {
	t.Helper()
	pool := openActivityTestPool(t, ctx)
	seedCommandAuditFixture(t, ctx, pool, base)
	source, err := NewCommandAuditActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new command audit activity source: %v", err)
	}
	return source, pool
}

func TestPostgresIntegrationCommandAuditSourceProjectsEveryPhase(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newCommandAuditActivitySourceFixture(t, ctx, base)

	head, err := source.IncrementalHead(ctx)
	if err != nil {
		t.Fatalf("incremental head: %v", err)
	}
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: head.RecordedThrough}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 4 {
		t.Fatalf("scanned %d candidates, want all 4 audit rows: %+v", len(candidates), candidates)
	}

	byAuditID := make(map[string]activity.CandidateEvent, len(candidates))
	for _, candidate := range candidates {
		byAuditID[candidate.Source.EventID] = candidate
	}

	// The operator who queued the command is attributed; the agent phases that
	// followed have no user behind them and must stay unattributed.
	queued := byAuditID["cmda_queued"]
	if queued.Actor == nil || queued.Actor.ActorID != commandAuditFixtureActorID {
		t.Fatalf("queued actor = %+v, want the operator", queued.Actor)
	}
	if queued.Actor.DisplayName != "Alan" {
		t.Fatalf("queued actor display name = %q, want the captured snapshot", queued.Actor.DisplayName)
	}
	if dispatched := byAuditID["cmda_dispatched"]; dispatched.Actor != nil {
		t.Fatalf("dispatched actor = %+v, want none for an agent phase", dispatched.Actor)
	}

	// The instance has since been renamed, and the snapshot is what keeps the
	// timeline showing the name it had when the command ran.
	if got := queued.Subjects[0].Identity["display_name"]; got != "hk-edge-01" {
		t.Fatalf("identity = %q, want the captured name rather than the current one", got)
	}

	if severity := byAuditID["cmda_failed"].Severity; severity != "warning" {
		t.Fatalf("failed command severity = %q, want warning", severity)
	}
	if severity := byAuditID["cmda_completed"].Severity; severity != "info" {
		t.Fatalf("clean command severity = %q, want info", severity)
	}
	if summary := byAuditID["cmda_failed"].Presentation.Summary; summary != "journalctl_u" {
		t.Fatalf("summary = %q, want the catalog command id", summary)
	}
}

func TestPostgresIntegrationCommandAuditSourceHonoursBothWindowBounds(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, _ := newCommandAuditActivitySourceFixture(t, ctx, base)

	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(2 * time.Second)}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("upper bound returned %d rows, want the 2 recorded by then", len(candidates))
	}

	candidates, err = source.ScanAfter(ctx, activity.ScanWindow{
		From:    base.Add(30 * time.Second),
		Through: base.Add(10 * time.Minute),
	}, 50)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Source.EventID != "cmda_failed" {
		t.Fatalf("lower bound returned %d rows: %+v", len(candidates), candidates)
	}
}

func TestPostgresIntegrationCommandAuditSourceScanUsesTheGlobalTimeIndex(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	_, pool := newCommandAuditActivitySourceFixture(t, ctx, base)

	var indexed bool
	if err := pool.QueryRow(ctx, `
		select exists (
		  select 1 from pg_indexes
		  where schemaname = 'public'
		    and tablename = 'monitoring_instance_command_action_audit'
		    and indexdef like '%occurred_at%'
		    and indexdef not like '%monitoring_instance_id%'
		    and indexdef not like '%action_id%'
		)`).Scan(&indexed); err != nil {
		t.Fatalf("inspect indexes: %v", err)
	}
	// The per-instance and per-action indexes lead with a column the projection
	// scan does not filter on, so neither can serve a scan ordered by time alone.
	if !indexed {
		t.Fatal("the audit needs a global time index for the projection scan")
	}
}

func TestPostgresIntegrationCommandAuditSourceProjectsThroughTheProjector(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	source, pool := newCommandAuditActivitySourceFixture(t, ctx, base)

	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:   activityTestNamespace(),
		Adapters:    []activity.SourceAdapter{source},
		Checkpoints: repository,
		Publisher:   repository,
		BatchSize:   2,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}

	// Two passes at a page size of two: the backlog has to drain rather than stall
	// on the first page.
	inserted := 0
	for pass := 0; pass < 3; pass++ {
		report, err := projector.ProjectOnce(ctx, 1)
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if err := report.Err(); err != nil {
			t.Fatalf("pass %d failed: %v", pass, err)
		}
		outcome, _ := report.Source(activity.SourceKindCommandAudit)
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
