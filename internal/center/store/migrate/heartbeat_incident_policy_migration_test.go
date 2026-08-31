package migrate

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/syncing"
)

func TestHeartbeatIncidentPolicyMigrationSourceContract(t *testing.T) {
	t.Parallel()

	payload, err := migrations.FS.ReadFile("0063_tune_heartbeat_incident_policy.sql")
	if err != nil {
		t.Fatalf("read 0063 migration: %v", err)
	}
	source := strings.ToLower(strings.Join(strings.Fields(string(payload)), " "))
	for _, fragment := range []string{
		"alter column incident_defaults set default",
		`"stale_threshold_intervals":12`,
		"incident_defaults->>'stale_threshold_intervals' = '3'",
		`incident_defaults || '{"stale_threshold_intervals":12}'::jsonb`,
		"updated_at = now()",
		"on monitoring_instance_heartbeats (monitoring_instance_id, received_at desc, id desc)",
		"include (sync_batch_id)",
		"where is_backfilled = false",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("0063 source = %q, want fragment %q", source, fragment)
		}
	}
	if strings.Contains(source, "override_rules") {
		t.Fatal("0063 must not rewrite explicit override_rules thresholds")
	}
}

func TestHeartbeatIncidentPolicyMigrationRegistersExplicitEmptyAppACLFragment(t *testing.T) {
	t.Parallel()

	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile production current APP ACL source contract: %v", err)
	}
	if got, want := len(source.fragments), 12; got != want {
		t.Fatalf("current APP ACL fragment count = %d, want %d", got, want)
	}
	fragment := source.fragments[11]
	if fragment.Migration != "0063_tune_heartbeat_incident_policy.sql" {
		t.Fatalf("twelfth fragment migration = %q", fragment.Migration)
	}
	if len(fragment.Objects) != 0 || len(fragment.Privileges) != 0 || len(fragment.AuxiliaryPrivileges) != 0 || len(fragment.Functions) != 0 {
		t.Fatalf("0063 fragment = %#v, want explicit empty APP ACL delta", fragment)
	}
	if appACLCurrentMigrationFragments[11].Privileges == nil {
		t.Fatal("0063 Privileges callback must be non-nil")
	}
	if got := appACLCurrentMigrationFragments[11].Privileges("houfeng"); got != nil {
		t.Fatalf("0063 Privileges() = %#v, want nil", got)
	}
}

func TestPostgresIntegrationHeartbeatIncidentPolicyMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := openTemporaryPostgresDatabase(t, ctx)
	applyPostgresMigrationsThrough(t, ctx, db, "0062_create_vps_create_idempotency.sql")

	oldUpdatedAt := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	if _, err := db.Exec(ctx, `
		insert into center_settings (settings_id, incident_defaults, override_rules, updated_at)
		values (
			'center',
			'{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}'::jsonb,
				'{"monitoring_instance_labels":[{"label":"edge","overrides":{"incident_defaults":{"stale_threshold_intervals":3}}}],"target_types":[],"target_labels":[]}'::jsonb,
			$1
		)`, oldUpdatedAt); err != nil {
		t.Fatalf("seed pre-0063 center settings: %v", err)
	}

	payload, err := migrations.FS.ReadFile("0063_tune_heartbeat_incident_policy.sql")
	if err != nil {
		t.Fatalf("read 0063 migration: %v", err)
	}
	if _, err := db.Exec(ctx, string(payload)); err != nil {
		t.Fatalf("apply 0063 migration: %v", err)
	}

	var globalThreshold, overrideThreshold int
	var migratedUpdatedAt time.Time
	if err := db.QueryRow(ctx, `
		select
			(incident_defaults->>'stale_threshold_intervals')::int,
				(override_rules#>>'{monitoring_instance_labels,0,overrides,incident_defaults,stale_threshold_intervals}')::int,
			updated_at
		from center_settings where settings_id = 'center'`).Scan(&globalThreshold, &overrideThreshold, &migratedUpdatedAt); err != nil {
		t.Fatalf("read migrated center settings: %v", err)
	}
	if globalThreshold != 12 || overrideThreshold != 3 || !migratedUpdatedAt.After(oldUpdatedAt) {
		t.Fatalf("migrated settings = global %d override %d updated_at %s, want 12/3 and advanced timestamp", globalThreshold, overrideThreshold, migratedUpdatedAt)
	}

	customUpdatedAt := time.Date(2025, time.February, 3, 4, 5, 6, 0, time.UTC)
	if _, err := db.Exec(ctx, `
		update center_settings
		set incident_defaults = jsonb_set(incident_defaults, '{stale_threshold_intervals}', '20'::jsonb),
			updated_at = $1
		where settings_id = 'center'`, customUpdatedAt); err != nil {
		t.Fatalf("seed custom threshold 20: %v", err)
	}
	if _, err := db.Exec(ctx, string(payload)); err != nil {
		t.Fatalf("reapply idempotent 0063 migration: %v", err)
	}
	if err := db.QueryRow(ctx, `
		select (incident_defaults->>'stale_threshold_intervals')::int, updated_at
		from center_settings where settings_id = 'center'`).Scan(&globalThreshold, &migratedUpdatedAt); err != nil {
		t.Fatalf("read custom settings after idempotent migration: %v", err)
	}
	if globalThreshold != 20 || !migratedUpdatedAt.Equal(customUpdatedAt) {
		t.Fatalf("custom settings = threshold %d updated_at %s, want preserved 20/%s", globalThreshold, migratedUpdatedAt, customUpdatedAt)
	}

	if _, err := db.Exec(ctx, `delete from center_settings where settings_id = 'center'`); err != nil {
		t.Fatalf("delete settings before default probe: %v", err)
	}
	if _, err := db.Exec(ctx, `insert into center_settings (settings_id) values ('center')`); err != nil {
		t.Fatalf("insert fresh default settings row: %v", err)
	}
	if err := db.QueryRow(ctx, `select (incident_defaults->>'stale_threshold_intervals')::int from center_settings where settings_id = 'center'`).Scan(&globalThreshold); err != nil {
		t.Fatalf("read fresh incident default: %v", err)
	}
	if globalThreshold != 12 {
		t.Fatalf("fresh stale threshold = %d, want 12", globalThreshold)
	}

	var indexDefinition string
	if err := db.QueryRow(ctx, `
		select indexdef from pg_indexes
		where schemaname = 'public' and indexname = 'idx_monitoring_instance_heartbeats_live_received'`).Scan(&indexDefinition); err != nil {
		t.Fatalf("read heartbeat recovery index: %v", err)
	}
	normalizedIndex := strings.ToLower(strings.Join(strings.Fields(indexDefinition), " "))
	for _, fragment := range []string{"(monitoring_instance_id, received_at desc, id desc)", "include (sync_batch_id)", "where (is_backfilled = false)"} {
		if !strings.Contains(normalizedIndex, fragment) {
			t.Fatalf("index definition = %q, want %q", normalizedIndex, fragment)
		}
	}
}

func TestPostgresIntegrationHeartbeatRecoveryReceipts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLCurrent() for heartbeat recovery: %v", err)
	}

	monitoringInstanceID := "mi_heartbeat_recovery"
	if _, err := migrator.Exec(ctx, `
		insert into monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status
		) values ($1, 'Heartbeat Recovery', 'HK', 'Hong Kong', 'Test Provider', '在用')`, monitoringInstanceID); err != nil {
		t.Fatalf("seed monitoring instance: %v", err)
	}
	startedAt := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	candidateLimit := 3 * syncing.MaxBatchItems
	historyRows := candidateLimit * 4
	if _, err := migrator.Exec(ctx, `
			insert into monitoring_instance_heartbeats (
			monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled
		)
		select $1, $2, $2, 'test-agent', 'test-fingerprint', 'duplicate-history', false
		from generate_series(1, $3::int)`, monitoringInstanceID, startedAt.Add(time.Second), historyRows); err != nil {
		t.Fatalf("seed duplicate heartbeat history: %v", err)
	}
	latestReceivedAt := startedAt.Add(10 * time.Second)
	if _, err := migrator.Exec(ctx, `
		insert into monitoring_instance_heartbeats (
			monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled
		) values
			($1, $2, $2, 'test-agent', 'test-fingerprint', 'batch-1', false),
			($1, $2, $2, 'test-agent', 'test-fingerprint', 'batch-2', false),
			($1, $2, $2, 'test-agent', 'test-fingerprint', 'batch-3', false),
			($1, $3, $3, 'test-agent', 'test-fingerprint', 'backfill', true),
			($1, $4, $4, 'test-agent', 'test-fingerprint', 'pre-incident', false)`,
		monitoringInstanceID, latestReceivedAt, latestReceivedAt.Add(time.Second), startedAt.Add(-time.Second)); err != nil {
		t.Fatalf("seed newest, backfilled, and pre-incident heartbeat receipts: %v", err)
	}
	if _, err := migrator.Exec(ctx, `analyze monitoring_instance_heartbeats`); err != nil {
		t.Fatalf("analyze heartbeat recovery history: %v", err)
	}

	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	trace := &heartbeatRecoveryQueryTrace{}
	tracedRuntime := openHeartbeatRecoveryTracedPool(t, ctx, runtime, trace)
	got, err := incidents.NewPostgresSnapshotReader(tracedRuntime).ListRecentLiveHeartbeatReceipts(ctx, monitoringInstanceID, startedAt)
	if err != nil {
		t.Fatalf("runtime ListRecentLiveHeartbeatReceipts() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("receipts = %#v, want exactly three distinct live batches", got)
	}
	wantBatches := []string{"batch-3", "batch-2", "batch-1"}
	for i := range wantBatches {
		if got[i].SyncBatchID != wantBatches[i] || !got[i].ReceivedAt.Equal(latestReceivedAt) {
			t.Fatalf("receipt[%d] = %#v, want %q at tied received_at %s", i, got[i], wantBatches[i], latestReceivedAt)
		}
	}

	query, args := trace.snapshot()
	if query == "" {
		t.Fatal("production heartbeat recovery query was not captured")
	}
	var liveRows int
	if err := migrator.QueryRow(ctx, `
		select count(*) from monitoring_instance_heartbeats
		where monitoring_instance_id = $1 and received_at > $2 and is_backfilled = false`, monitoringInstanceID, startedAt).Scan(&liveRows); err != nil {
		t.Fatalf("count post-incident live heartbeat history: %v", err)
	}
	if liveRows <= candidateLimit {
		t.Fatalf("post-incident live rows = %d, want more than candidate limit %d", liveRows, candidateLimit)
	}
	explainRecoveryQuery := func(historyLabel string) heartbeatRecoveryScanMetrics {
		var rawPlan []byte
		if err := runtime.QueryRow(ctx, "explain (analyze, buffers, format json) "+query, args...).Scan(&rawPlan); err != nil {
			t.Fatalf("EXPLAIN production heartbeat recovery query (%s): %v", historyLabel, err)
		}
		metrics := assertHeartbeatRecoveryPlanBounded(t, rawPlan, candidateLimit)
		t.Logf("bounded heartbeat recovery scan (%s): rows=%.0f removed=%.0f shared_blocks=%.0f candidate_limit=%d", historyLabel, metrics.actualRows, metrics.rowsRemovedByFilter, metrics.sharedBlocks, candidateLimit)
		return metrics
	}
	baselineMetrics := explainRecoveryQuery("baseline history")

	expandedHistoryRows := candidateLimit * 16
	if _, err := migrator.Exec(ctx, `
			insert into monitoring_instance_heartbeats (
				monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled
			)
			select $1, $2, $2, 'test-agent', 'test-fingerprint', 'expanded-duplicate-history', false
			from generate_series(1, $3::int)`, monitoringInstanceID, startedAt.Add(2*time.Second), expandedHistoryRows); err != nil {
		t.Fatalf("seed expanded duplicate heartbeat history: %v", err)
	}
	if _, err := migrator.Exec(ctx, `analyze monitoring_instance_heartbeats`); err != nil {
		t.Fatalf("analyze expanded heartbeat recovery history: %v", err)
	}
	var expandedLiveRows int
	if err := migrator.QueryRow(ctx, `
			select count(*) from monitoring_instance_heartbeats
			where monitoring_instance_id = $1 and received_at > $2 and is_backfilled = false`, monitoringInstanceID, startedAt).Scan(&expandedLiveRows); err != nil {
		t.Fatalf("count expanded post-incident live heartbeat history: %v", err)
	}
	if expandedLiveRows != liveRows+expandedHistoryRows {
		t.Fatalf("expanded post-incident live rows = %d, want %d", expandedLiveRows, liveRows+expandedHistoryRows)
	}
	expandedMetrics := explainRecoveryQuery("expanded history")
	maxBlockGrowth := float64(candidateLimit / 16)
	if expandedMetrics.sharedBlocks > baselineMetrics.sharedBlocks+maxBlockGrowth {
		t.Fatalf("expanded history shared blocks = %.0f, baseline %.0f + allowed growth %.0f; old history must not cause linear reads", expandedMetrics.sharedBlocks, baselineMetrics.sharedBlocks, maxBlockGrowth)
	}
}

type heartbeatRecoveryQueryTrace struct {
	mu    sync.Mutex
	query string
	args  []any
}

var _ pgx.QueryTracer = (*heartbeatRecoveryQueryTrace)(nil)

func (trace *heartbeatRecoveryQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.query = data.SQL
	trace.args = append([]any(nil), data.Args...)
	return ctx
}

func (*heartbeatRecoveryQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (trace *heartbeatRecoveryQueryTrace) snapshot() (string, []any) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.query, append([]any(nil), trace.args...)
}

func openHeartbeatRecoveryTracedPool(t *testing.T, ctx context.Context, source *pgxpool.Pool, trace *heartbeatRecoveryQueryTrace) *pgxpool.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.MinConns = 0
	config.MaxConns = 1
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open traced heartbeat recovery pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type heartbeatRecoveryExplainDocument struct {
	Plan heartbeatRecoveryExplainNode `json:"Plan"`
}

type heartbeatRecoveryExplainNode struct {
	NodeType            string                         `json:"Node Type"`
	RelationName        string                         `json:"Relation Name"`
	IndexName           string                         `json:"Index Name"`
	ActualRows          float64                        `json:"Actual Rows"`
	ActualLoops         float64                        `json:"Actual Loops"`
	RowsRemovedByFilter float64                        `json:"Rows Removed by Filter"`
	SharedHitBlocks     *float64                       `json:"Shared Hit Blocks"`
	SharedReadBlocks    *float64                       `json:"Shared Read Blocks"`
	Plans               []heartbeatRecoveryExplainNode `json:"Plans"`
}

type heartbeatRecoveryScanMetrics struct {
	actualRows          float64
	rowsRemovedByFilter float64
	sharedBlocks        float64
}

func assertHeartbeatRecoveryPlanBounded(t *testing.T, rawPlan []byte, candidateLimit int) heartbeatRecoveryScanMetrics {
	t.Helper()
	var documents []heartbeatRecoveryExplainDocument
	if err := json.Unmarshal(rawPlan, &documents); err != nil {
		t.Fatalf("decode heartbeat recovery EXPLAIN JSON: %v\n%s", err, rawPlan)
	}
	if len(documents) != 1 {
		t.Fatalf("heartbeat recovery EXPLAIN documents = %d, want 1", len(documents))
	}
	const (
		heartbeatRelation = "monitoring_instance_heartbeats"
		heartbeatIndex    = "idx_monitoring_instance_heartbeats_live_received"
	)
	maxScanRows := float64(candidateLimit)
	maxFilterRows := float64(candidateLimit)
	maxSharedBlocks := float64(candidateLimit/2 + 64)
	foundWindow, foundSort, foundWindowInput, foundOrderedIndexScan := false, false, false, false
	var scanMetrics heartbeatRecoveryScanMetrics
	var walk func(heartbeatRecoveryExplainNode)
	walk = func(node heartbeatRecoveryExplainNode) {
		if node.NodeType == "WindowAgg" || node.NodeType == "Sort" {
			if node.ActualRows > float64(candidateLimit) {
				t.Errorf("%s actual rows = %.0f, want <= candidate limit %d", node.NodeType, node.ActualRows, candidateLimit)
			}
		}
		if node.NodeType == "WindowAgg" {
			foundWindow = true
			for _, input := range node.Plans {
				foundWindowInput = true
				if input.ActualRows > float64(candidateLimit) {
					t.Errorf("WindowAgg input %s actual rows = %.0f, want <= candidate limit %d", input.NodeType, input.ActualRows, candidateLimit)
				}
			}
		}
		if node.NodeType == "Sort" {
			foundSort = true
		}
		if strings.Contains(node.NodeType, "Bitmap") {
			t.Errorf("heartbeat recovery plan uses unordered %s path", node.NodeType)
		}
		if node.RelationName == heartbeatRelation {
			if node.NodeType != "Index Scan" && node.NodeType != "Index Only Scan" {
				t.Errorf("heartbeat relation scan = %s, want ordered Index Scan or Index Only Scan", node.NodeType)
			} else if node.IndexName != heartbeatIndex {
				t.Errorf("heartbeat relation index = %q, want %q", node.IndexName, heartbeatIndex)
			} else {
				foundOrderedIndexScan = true
			}
			loops := node.ActualLoops
			if loops <= 0 {
				t.Errorf("heartbeat relation scan actual loops = %.0f, want positive", loops)
				loops = 1
			}
			scanMetrics.actualRows += node.ActualRows * loops
			scanMetrics.rowsRemovedByFilter += node.RowsRemovedByFilter * loops
			if scanMetrics.actualRows > maxScanRows {
				t.Errorf("heartbeat relation scan actual rows*loops = %.0f, want <= %.0f", scanMetrics.actualRows, maxScanRows)
			}
			if scanMetrics.rowsRemovedByFilter > maxFilterRows {
				t.Errorf("heartbeat relation rows removed by filter*loops = %.0f, want <= %.0f", scanMetrics.rowsRemovedByFilter, maxFilterRows)
			}
			if node.SharedHitBlocks == nil || node.SharedReadBlocks == nil {
				t.Errorf("heartbeat relation scan lacks BUFFERS evidence: hit=%v read=%v", node.SharedHitBlocks, node.SharedReadBlocks)
			} else {
				scanMetrics.sharedBlocks += *node.SharedHitBlocks + *node.SharedReadBlocks
				if scanMetrics.sharedBlocks <= 0 {
					t.Error("heartbeat relation scan shared hit/read blocks = 0, want positive execution evidence")
				}
				if scanMetrics.sharedBlocks > maxSharedBlocks {
					t.Errorf("heartbeat relation scan shared hit/read blocks = %.0f, want <= %.0f", scanMetrics.sharedBlocks, maxSharedBlocks)
				}
			}
		}
		for _, child := range node.Plans {
			walk(child)
		}
	}
	walk(documents[0].Plan)
	if !foundWindow || !foundSort || !foundWindowInput {
		t.Fatalf("heartbeat recovery plan missing bounded WindowAgg/Sort evidence: window=%v sort=%v input=%v", foundWindow, foundSort, foundWindowInput)
	}
	if !foundOrderedIndexScan {
		t.Fatalf("heartbeat recovery plan does not use exact ordered index %q", heartbeatIndex)
	}
	return scanMetrics
}
