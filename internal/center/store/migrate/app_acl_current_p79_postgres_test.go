package migrate

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const appACLCurrentP80Migration = "0080_bound_agent_sync_replay.sql"

func TestPostgresIntegrationAgentReplayMigration(t *testing.T) {
	t.Run("fresh_c80", testPostgresIntegrationAgentReplayFresh)
	for _, chained := range []bool{false, true} {
		name := "independent_c79_genesis"
		if chained {
			name = "released_c76_c77_c78_c79_history"
		}
		t.Run(name, func(t *testing.T) { testPostgresIntegrationAgentReplayUpgrade(t, chained) })
	}
	for _, cutpoint := range []string{"ddl", "dcl", "manifest"} {
		t.Run("rollback_"+cutpoint, func(t *testing.T) { testPostgresIntegrationAgentReplayRollback(t, cutpoint) })
	}
	for _, current := range []bool{false, true} {
		phase := "predecessor_"
		if current {
			phase = "current_"
		}
		for _, drift := range []string{"unrelated_add", "unrelated_remove", "public", "admin", "grant_option"} {
			t.Run(phase+drift, func(t *testing.T) { testPostgresIntegrationAgentReplayDrift(t, current, drift) })
		}
	}
	for _, drift := range []string{"replay_state_delete", "receipt_update", "missing_state_select", "missing_receipt_delete", "replay_public", "replay_admin", "function_execute"} {
		t.Run("current_"+drift, func(t *testing.T) { testPostgresIntegrationAgentReplayDrift(t, true, drift) })
	}
	t.Run("unknown_drop_dependency_rolls_back", testPostgresIntegrationAgentReplayDropDependency)
}

func seedAppACLCurrentC79(t *testing.T, ctx context.Context, fixture exactAppACLCurrentSuccessorPostgresFixture, db *pgxpool.Pool, chained bool) AppACLManifestPersistedV1 {
	t.Helper()
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP79Migration)
	if !chained {
		manifest, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, db, profile)
		return manifest
	}
	previous := seedAppACLCurrentP78Predecessor(t, ctx, fixture, db, true)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	manifest, err := appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, db, profile)
	if err != nil {
		t.Fatalf("materialize frozen C79 successor: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger, appACLCurrentP79Migration)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if manifest.ManifestRevision != previous.ManifestRevision+1 || manifest.PreviousManifestDigest != previous.ManifestDigest || !reflect.DeepEqual(before.Catalog, after.Catalog) {
		t.Fatal("historical C78 to C79 fixture changed authorization or manifest linkage")
	}
	return manifest
}

func seedAppACLCurrentReplayFacts(t *testing.T, ctx context.Context, db *pgxpool.Pool, legacy bool) {
	t.Helper()
	seedAppACLCurrentP77Rows(t, ctx, db)
	seedAppACLCurrentP68LegacyCPURows(t, ctx, db)
	if _, err := db.Exec(ctx, `
		update public.monitoring_instances set ever_connected = true, last_trusted_online_at = '2026-01-02 03:04:06+00'
		where monitoring_instance_id = 'mi_p76_existing';
		insert into public.monitoring_agent_sessions
		(session_id, monitoring_instance_id, token_hash, fingerprint_hash, started_at, last_trusted_online_at, ever_connected)
		values ('session_c79', 'mi_p76_existing', 'token-c79', 'fingerprint-c79', '2026-01-01 00:00:00+00', '2026-01-02 03:04:06+00', true);
		insert into public.monitoring_instance_heartbeats
		(monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled)
		values ('mi_p76_existing', '2026-01-02 03:04:05+00', '2026-01-02 03:04:06+00', 'c79-agent', 'fingerprint-c79', 'legacy-batch', true);
		insert into public.state_change_events (event_id, object_type, object_id, event_type, summary)
		values ('event_c79', 'monitoring_instance', 'mi_p76_existing', 'online', 'preserve business history');
	`); err != nil {
		t.Fatalf("seed C79 credentials/online/raw/business facts: %v", err)
	}
	if legacy {
		if _, err := db.Exec(ctx, `
			insert into public.agent_sync_batches (monitoring_instance_id, sync_batch_id) values ('mi_p76_existing', 'legacy-batch');
			insert into public.agent_live_signals (session_id, signal_id) values ('session_c79', 'legacy-live');
		`); err != nil {
			t.Fatalf("seed old exact replay identities: %v", err)
		}
	}
}

func readAppACLCurrentReplayFacts(t *testing.T, ctx context.Context, db *pgxpool.Pool) string {
	t.Helper()
	var body string
	if err := db.QueryRow(ctx, `select jsonb_build_object(
		'vps', (select jsonb_agg(to_jsonb(r) order by vps_id) from public.vps_assets r),
		'instances', (select jsonb_agg(to_jsonb(r) order by monitoring_instance_id) from public.monitoring_instances r),
		'sessions', (select jsonb_agg(to_jsonb(r) order by session_id) from public.monitoring_agent_sessions r),
		'heartbeats', (select jsonb_agg(to_jsonb(r) order by id) from public.monitoring_instance_heartbeats r),
		'host', (select jsonb_agg(to_jsonb(r) order by id) from public.host_samples r),
		'aggregates', (select jsonb_agg(to_jsonb(r) order by monitoring_instance_id, bucket_date) from public.monitoring_instance_host_sample_daily_aggregates r),
		'events', (select jsonb_agg(to_jsonb(r) order by event_id) from public.state_change_events r),
		'settings', (select jsonb_agg(to_jsonb(r) order by settings_id) from public.center_settings r),
		'reports', (select jsonb_agg(to_jsonb(r) order by report_id) from public.ip_quality_reports r),
		'providers', (select jsonb_agg(to_jsonb(r) order by result_id) from public.ip_quality_provider_results r),
		'services', (select jsonb_agg(to_jsonb(r) order by unlock_id) from public.ip_quality_service_unlocks r),
		'intelligence', (select jsonb_agg(to_jsonb(r) order by report_id) from public.ip_quality_intelligence r),
		'cache', (select jsonb_agg(to_jsonb(r) order by source_id, resource, credential_fingerprint) from public.ip_quality_source_cache r)
	)::text`).Scan(&body); err != nil {
		t.Fatalf("read preserved C79 facts: %v", err)
	}
	return body
}

func assertAppACLCurrentReplayTables(t *testing.T, ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, current bool) {
	t.Helper()
	var oldBatch, oldLive, state, receipt bool
	if err := db.QueryRow(ctx, `select to_regclass('public.agent_sync_batches') is not null,
		to_regclass('public.agent_live_signals') is not null, to_regclass('public.agent_replay_states') is not null,
		to_regclass('public.agent_batch_receipts') is not null`).Scan(&oldBatch, &oldLive, &state, &receipt); err != nil {
		t.Fatal(err)
	}
	if oldBatch == current || oldLive == current || state != current || receipt != current {
		t.Fatalf("replay tables old=%t/%t new=%t/%t, current=%t", oldBatch, oldLive, state, receipt, current)
	}
	if !current {
		return
	}
	var configured bool
	if err := db.QueryRow(ctx, `select bool_and(reloptions @> array['autovacuum_enabled=true','autovacuum_vacuum_threshold=1000','autovacuum_vacuum_scale_factor=0.05'])
		and bool_and(case when relname = 'agent_replay_states' then reloptions @> array['fillfactor=80'] else true end)
		from pg_catalog.pg_class where oid in ('public.agent_replay_states'::regclass,'public.agent_batch_receipts'::regclass)`).Scan(&configured); err != nil || !configured {
		t.Fatalf("bounded replay storage options = %t/%v", configured, err)
	}
	var exactColumns, onlyPrimaryIndexes bool
	if err := db.QueryRow(ctx, `select
		(select jsonb_object_agg(attname, pg_catalog.format_type(atttypid, atttypmod))
			from pg_catalog.pg_attribute where attrelid='public.agent_replay_states'::regclass and attnum>0 and not attisdropped)
			= '{"session_id":"text","producer_id":"uuid","retired_through":"bigint","allocated_through":"bigint","live_sequence_high":"bigint"}'::jsonb
		and (select bool_and(attnotnull) from pg_catalog.pg_attribute where attrelid='public.agent_replay_states'::regclass and attnum>0 and not attisdropped)
		and (select jsonb_object_agg(attname, pg_catalog.format_type(atttypid, atttypmod))
			from pg_catalog.pg_attribute where attrelid='public.agent_batch_receipts'::regclass and attnum>0 and not attisdropped)
			= '{"session_id":"text","batch_sequence":"bigint","payload_digest":"bytea"}'::jsonb
		and (select bool_and(attnotnull) from pg_catalog.pg_attribute where attrelid='public.agent_batch_receipts'::regclass and attnum>0 and not attisdropped),
		(select count(*)=2 and bool_and(indisprimary and indisunique) from pg_catalog.pg_index
			where indrelid in ('public.agent_replay_states'::regclass,'public.agent_batch_receipts'::regclass))
	`).Scan(&exactColumns, &onlyPrimaryIndexes); err != nil || !exactColumns || !onlyPrimaryIndexes {
		t.Fatalf("replay schema must contain only exact bounded-state/receipt columns and two primary indexes: %t/%t/%v", exactColumns, onlyPrimaryIndexes, err)
	}
}

func testPostgresIntegrationAgentReplayFresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	manifest, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil || manifest.ManifestRevision != 1 {
		t.Fatalf("fresh C80 convergence = %#v/%v", manifest, err)
	}
	assertAppACLCurrentReplayTables(t, ctx, db, true)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatal(err)
	}
	seedAppACLCurrentReplayFacts(t, ctx, db, false)
	assertAppACLCurrentReplayRuntime(t, ctx, runtimeDB)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	if len(before.Ledger) != 81 || before.Ledger[80].Name != appACLCurrentP80Migration {
		t.Fatalf("fresh C80 ledger must contain 81 sources ending 0080: %#v", before.Ledger)
	}
	facts := readAppACLCurrentReplayFacts(t, ctx, db)
	repeated, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil || repeated.ManifestDigest != manifest.ManifestDigest || !reflect.DeepEqual(before, readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)) || readAppACLCurrentReplayFacts(t, ctx, db) != facts {
		t.Fatalf("fresh C80 repeat mutated durable state: %v", err)
	}
}

func testPostgresIntegrationAgentReplayUpgrade(t *testing.T, chained bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	previous := seedAppACLCurrentC79(t, ctx, fixture, db, chained)
	seedAppACLCurrentReplayFacts(t, ctx, db, true)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	facts := readAppACLCurrentReplayFacts(t, ctx, db)
	if len(before.Ledger) != 80 || before.Ledger[79].Name != appACLCurrentP79Migration {
		t.Fatal("upgrade fixture is not independently frozen C79")
	}
	assertAppACLCurrentReplayTables(t, ctx, db, false)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	manifest, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil || manifest.ManifestRevision != previous.ManifestRevision+1 || manifest.PreviousManifestDigest != previous.ManifestDigest {
		t.Fatalf("C79 to C80 exact successor = %#v/%v", manifest, err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger, appACLCurrentP80Migration)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatal("C80 did not append exactly one manifest")
	}
	if !reflect.DeepEqual(appACLCurrentCatalogBeforeReplay(t, before.Catalog, after.Catalog), before.Catalog) || readAppACLCurrentReplayFacts(t, ctx, db) != facts {
		t.Fatal("C80 changed anything beyond exact replay retirement/additions or rewrote persisted facts")
	}
	assertAppACLCurrentReplayTables(t, ctx, db, true)
	var states, receipts int
	if err := db.QueryRow(ctx, `select (select count(*) from public.agent_replay_states), (select count(*) from public.agent_batch_receipts)`).Scan(&states, &receipts); err != nil || states != 0 || receipts != 0 {
		t.Fatalf("migration invented v2 producer/receipt state: %d/%d/%v", states, receipts, err)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatal(err)
	}
	assertAppACLCurrentReplayRuntime(t, ctx, runtimeDB)
	repeated, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil || repeated.ManifestDigest != manifest.ManifestDigest || !reflect.DeepEqual(after, readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)) || readAppACLCurrentReplayFacts(t, ctx, db) != facts {
		t.Fatalf("C80 repeat changed durable state: %v", err)
	}
}

func assertAppACLCurrentReplayRuntime(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `insert into public.agent_replay_states (session_id, producer_id, allocated_through)
		values ('session_c79', '00000000-0000-4000-8000-000000000079', 131073);
		insert into public.agent_batch_receipts (session_id, batch_sequence, payload_digest)
		values ('session_c79', 1, decode(repeat('ab',32),'hex')), ('session_c79',131072,decode(repeat('cd',32),'hex'));`); err != nil {
		t.Fatalf("runtime INSERT replay state and window receipts: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx, `select count(*) from public.agent_batch_receipts where session_id='session_c79'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("runtime SELECT receipts = %d/%v", count, err)
	}
	for _, tc := range []struct{ sql, state string }{
		{`update public.agent_batch_receipts set payload_digest=decode(repeat('ef',32),'hex') where session_id='session_c79'`, "42501"},
		{`delete from public.agent_replay_states where session_id='session_c79'`, "42501"},
		{`delete from public.agent_batch_receipts where session_id='session_c79' and batch_sequence=1`, "23514"},
		{`insert into public.agent_batch_receipts values ('session_c79',131073,decode(repeat('ab',32),'hex'))`, "23514"},
		{`insert into public.agent_batch_receipts values ('session_c79',2,decode('ab','hex'))`, "23514"},
		{`update public.agent_replay_states set producer_id='00000000-0000-4000-8000-000000000080' where session_id='session_c79'`, "23514"},
	} {
		if _, err := tx.Exec(ctx, "savepoint denied_replay_operation"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, tc.sql)
		requirePostgresSQLState(t, err, tc.state)
		if _, err := tx.Exec(ctx, "rollback to savepoint denied_replay_operation"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `update public.agent_replay_states set retired_through=1, live_sequence_high=1024 where session_id='session_c79'`); err != nil {
		t.Fatalf("runtime state UPDATE and invoker receipt retirement: %v", err)
	}
	var floor, high int64
	if err := tx.QueryRow(ctx, `select retired_through, live_sequence_high, (select count(*) from public.agent_batch_receipts where session_id='session_c79') from public.agent_replay_states where session_id='session_c79'`).Scan(&floor, &high, &count); err != nil || floor != 1 || high != 1024 || count != 1 {
		t.Fatalf("floor advance did not atomically clean receipt: %d/%d/%d/%v", floor, high, count, err)
	}
	for _, sql := range []string{
		`update public.agent_replay_states set retired_through=0 where session_id='session_c79'`,
		`update public.agent_replay_states set allocated_through=131072 where session_id='session_c79'`,
		`update public.agent_replay_states set live_sequence_high=1023 where session_id='session_c79'`,
		`insert into public.agent_batch_receipts values ('session_c79',1,decode(repeat('ab',32),'hex'))`,
	} {
		if _, err := tx.Exec(ctx, "savepoint monotonic_replay_operation"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, sql)
		requirePostgresSQLState(t, err, "23514")
		if _, err := tx.Exec(ctx, "rollback to savepoint monotonic_replay_operation"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, `update public.agent_replay_states set retired_through=131073 where session_id='session_c79';
		delete from public.agent_batch_receipts where session_id='session_c79'`); err != nil {
		t.Fatalf("runtime permitted DELETE after retirement: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func testPostgresIntegrationAgentReplayRollback(t *testing.T, cutpoint string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentC79(t, ctx, fixture, db, false)
	seedAppACLCurrentReplayFacts(t, ctx, db, true)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	facts := readAppACLCurrentReplayFacts(t, ctx, db)
	injected := errors.New("C80 transactional failure")
	reached := false
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	switch cutpoint {
	case "ddl":
		apply := dependencies.applyPending
		dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, sources migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
			if err := apply(ctx, tx, sources, applied); err != nil {
				return err
			}
			assertAppACLCurrentReplayTables(t, ctx, tx, true)
			reached = true
			return injected
		}
	case "dcl":
		apply := dependencies.applyDCL
		dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract appACLEffectiveCatalogContract) error {
			if err := apply(ctx, tx, contract); err != nil {
				return err
			}
			var granted bool
			if err := tx.QueryRow(ctx, `select has_table_privilege($1,'public.agent_batch_receipts','DELETE')`, fixture.runtimeRole).Scan(&granted); err != nil || !granted {
				t.Fatalf("DCL cutpoint before real runtime grant: %t/%v", granted, err)
			}
			reached = true
			return injected
		}
	case "manifest":
		insert := dependencies.insertSuccessor
		dependencies.insertSuccessor = func(ctx context.Context, tx pgx.Tx, previous AppACLManifestPersistedV1, sources, privileges []byte) (AppACLManifestPersistedV1, error) {
			manifest, err := insert(ctx, tx, previous, sources, privileges)
			if err != nil {
				return manifest, err
			}
			reached = manifest.ManifestRevision == previous.ManifestRevision+1
			return manifest, injected
		}
	}
	_, err := convergeAppACLCurrentWithDependencies(ctx, db.BeginTx, fixture.runtimeRole, fixture.adminRole, migrations.FS, appACLCurrentMigrationFragments, dependencies)
	if !reached || !errors.Is(err, injected) {
		t.Fatalf("C80 %s cutpoint not reached: %t/%v", cutpoint, reached, err)
	}
	assertAppACLCurrentReplayTables(t, ctx, db, false)
	if !reflect.DeepEqual(before, readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)) || facts != readAppACLCurrentReplayFacts(t, ctx, db) {
		t.Fatal("failed C80 transaction changed predecessor DDL/DCL/ledger/manifest/facts")
	}
	var oldBatch, oldLive int
	if err := db.QueryRow(ctx, `select (select count(*) from public.agent_sync_batches),(select count(*) from public.agent_live_signals)`).Scan(&oldBatch, &oldLive); err != nil || oldBatch != 1 || oldLive != 1 {
		t.Fatalf("rollback lost old replay identities: %d/%d/%v", oldBatch, oldLive, err)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err != nil {
		t.Fatal(err)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatal(err)
	}
}

func testPostgresIntegrationAgentReplayDrift(t *testing.T, current bool, drift string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentC79(t, ctx, fixture, db, false)
	seedAppACLCurrentReplayFacts(t, ctx, db, true)
	if current {
		if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err != nil {
			t.Fatal(err)
		}
	}
	runtimeRole := pgx.Identifier{fixture.runtimeRole}.Sanitize()
	var sql string
	switch drift {
	case "unrelated_add":
		sql = "grant delete on public.ip_quality_intelligence to " + runtimeRole
	case "unrelated_remove":
		sql = "revoke select on public.ip_quality_intelligence from " + runtimeRole
	case "public":
		sql = "grant select on public.ip_quality_intelligence to public"
	case "admin":
		sql = "grant select on public.ip_quality_intelligence to " + pgx.Identifier{fixture.adminRole}.Sanitize()
	case "grant_option":
		sql = "grant select on public.ip_quality_intelligence to " + runtimeRole + " with grant option"
	case "replay_state_delete":
		sql = "grant delete on public.agent_replay_states to " + runtimeRole
	case "receipt_update":
		sql = "grant update on public.agent_batch_receipts to " + runtimeRole
	case "missing_state_select":
		sql = "revoke select on public.agent_replay_states from " + runtimeRole
	case "missing_receipt_delete":
		sql = "revoke delete on public.agent_batch_receipts from " + runtimeRole
	case "replay_public":
		sql = "grant select on public.agent_batch_receipts to public"
	case "replay_admin":
		sql = "grant select on public.agent_replay_states to " + pgx.Identifier{fixture.adminRole}.Sanitize()
	case "function_execute":
		sql = "grant execute on function public.enforce_agent_replay_state() to " + runtimeRole
	default:
		t.Fatalf("unknown ACL drift %q", drift)
	}
	if _, err := db.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	facts := readAppACLCurrentReplayFacts(t, ctx, db)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err == nil {
		t.Fatal("admitted predecessor or drifted current ACL")
	}
	if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err == nil {
		t.Fatal("convergence accepted/repaired unapproved ACL drift")
	}
	if !reflect.DeepEqual(before, readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)) || facts != readAppACLCurrentReplayFacts(t, ctx, db) {
		t.Fatal("rejected ACL drift mutated durable state")
	}
	assertAppACLCurrentReplayTables(t, ctx, db, current)
}

func testPostgresIntegrationAgentReplayDropDependency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentC79(t, ctx, fixture, db, false)
	if _, err := db.Exec(ctx, `create schema replay_dependency; create view replay_dependency.old_receipts as select sync_batch_id from public.agent_sync_batches`); err != nil {
		t.Fatal(err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	_, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	requirePostgresSQLState(t, err, "2BP01")
	assertAppACLCurrentReplayTables(t, ctx, db, false)
	if !reflect.DeepEqual(before, readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)) {
		t.Fatal("unknown dependent view caused partial replay migration")
	}
}

// Historical successor tests keep their existing delta assertions after this
// independently checked C80 delta is projected away. Historical-only snapshots
// have no replay-state table and are returned untouched.
func appACLCurrentCatalogBeforeReplay(t *testing.T, before, after AppACLEffectiveCatalogSnapshotR1) AppACLEffectiveCatalogSnapshotR1 {
	t.Helper()
	isNew := func(class AppACLObjectClass, schema, identity string) bool {
		return schema == "public" && ((class == AppACLObjectClassTable && (identity == "agent_replay_states" || identity == "agent_batch_receipts")) ||
			(class == AppACLObjectClassFunction && (identity == "enforce_agent_replay_state()" || identity == "enforce_agent_batch_receipt()")))
	}
	isOld := func(class AppACLObjectClass, schema, identity string) bool {
		return class == AppACLObjectClassTable && schema == "public" && (identity == "agent_sync_batches" || identity == "agent_live_signals")
	}
	present := false
	for _, owner := range after.Owners {
		present = present || owner.ObjectIdentity == "agent_replay_states" && owner.SchemaName == "public"
	}
	if !present {
		return after
	}
	var migrator string
	for _, owner := range before.Owners {
		if owner.ObjectClass == AppACLObjectClassTable && owner.SchemaName == "public" {
			migrator = owner.OwnerRole
			break
		}
	}
	result := after
	result.Owners = nil
	newOwners := make(map[string]bool)
	for _, owner := range after.Owners {
		if isOld(owner.ObjectClass, owner.SchemaName, owner.ObjectIdentity) {
			t.Fatalf("C80 retained retired object %#v", owner)
		}
		if isNew(owner.ObjectClass, owner.SchemaName, owner.ObjectIdentity) {
			if owner.OwnerRole != migrator || newOwners[owner.ObjectIdentity] {
				t.Fatalf("C80 owner mismatch %#v", owner)
			}
			newOwners[owner.ObjectIdentity] = true
		} else {
			result.Owners = append(result.Owners, owner)
		}
	}
	if len(newOwners) != 4 {
		t.Fatalf("C80 owners = %#v, want exactly two tables/two functions", newOwners)
	}
	for _, owner := range before.Owners {
		if isOld(owner.ObjectClass, owner.SchemaName, owner.ObjectIdentity) {
			result.Owners = append(result.Owners, owner)
		}
	}
	sort.Slice(result.Owners, func(i, j int) bool {
		a, b := result.Owners[i], result.Owners[j]
		return string(a.ObjectClass)+"\x00"+a.SchemaName+"\x00"+a.ObjectIdentity+"\x00"+a.OwnerRole < string(b.ObjectClass)+"\x00"+b.SchemaName+"\x00"+b.ObjectIdentity+"\x00"+b.OwnerRole
	})
	projectPrivileges := func(old, current []AppACLEffectiveCatalogPrivilegeObservationR1, effective bool) []AppACLEffectiveCatalogPrivilegeObservationR1 {
		var projected []AppACLEffectiveCatalogPrivilegeObservationR1
		var runtimeRole string
		for _, privilege := range current {
			if privilege.ObjectIdentity == "agent_replay_states" && privilege.Privilege == AppACLPrivilegeInsert {
				runtimeRole = privilege.Grantee
			}
		}
		want := make(map[AppACLEffectiveCatalogPrivilegeObservationR1]bool)
		for _, privilege := range appACLCurrentReplayExpectedPrivileges() {
			want[AppACLEffectiveCatalogPrivilegeObservationR1{Grantee: runtimeRole, ObjectClass: privilege.ObjectClass, SchemaName: privilege.SchemaName, ObjectIdentity: privilege.ObjectIdentity, Privilege: privilege.Privilege}] = true
		}
		for _, privilege := range current {
			if isOld(privilege.ObjectClass, privilege.SchemaName, privilege.ObjectIdentity) {
				t.Fatalf("C80 retained retired privilege %#v", privilege)
			}
			if isNew(privilege.ObjectClass, privilege.SchemaName, privilege.ObjectIdentity) {
				if !want[privilege] {
					t.Fatalf("unexpected C80 privilege %#v", privilege)
				}
				delete(want, privilege)
			} else {
				projected = append(projected, privilege)
			}
		}
		if len(want) != 0 {
			t.Fatalf("missing C80 runtime grants %#v", want)
		}
		for _, privilege := range old {
			if isOld(privilege.ObjectClass, privilege.SchemaName, privilege.ObjectIdentity) {
				projected = append(projected, privilege)
			}
		}
		sort.Slice(projected, func(i, j int) bool {
			a, b := projected[i], projected[j]
			key := func(p AppACLEffectiveCatalogPrivilegeObservationR1) string {
				if effective {
					return p.Grantee + "\x00" + string(p.ObjectClass) + "\x00" + p.SchemaName + "\x00" + p.ObjectIdentity + "\x00" + p.ColumnName + "\x00" + string(p.Privilege)
				}
				return string(p.ObjectClass) + "\x00" + p.SchemaName + "\x00" + p.ObjectIdentity + "\x00" + p.ColumnName + "\x00" + p.Grantee + "\x00" + string(p.Privilege)
			}
			return key(a) < key(b)
		})
		return projected
	}
	result.DirectPrivileges = projectPrivileges(before.DirectPrivileges, after.DirectPrivileges, false)
	result.EffectivePrivileges = projectPrivileges(before.EffectivePrivileges, after.EffectivePrivileges, true)
	result.Functions = nil
	newFunctions := make(map[string]bool)
	for _, function := range after.Functions {
		if isNew(AppACLObjectClassFunction, function.SchemaName, function.Name+"("+function.IdentityArguments+")") {
			if function.OwnerRole != migrator || function.Kind != "f" || function.SecurityDefiner || function.IdentityArguments != "" || !reflect.DeepEqual(function.Config, []string{"search_path=pg_catalog"}) || newFunctions[function.Identity] {
				t.Fatalf("C80 function hardening mismatch %#v", function)
			}
			newFunctions[function.Identity] = true
		} else {
			result.Functions = append(result.Functions, function)
		}
	}
	if len(newFunctions) != 2 {
		t.Fatalf("C80 function inventory %#v, want exact two invokers", newFunctions)
	}
	return result
}
