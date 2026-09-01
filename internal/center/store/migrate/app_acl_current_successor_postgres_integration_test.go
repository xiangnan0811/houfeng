package migrate

import (
	"context"
	"crypto/sha256"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

func testPostgresIntegrationAppACLCurrentRegisteredSuccessor(t *testing.T) {
	for _, tc := range []struct {
		name           string
		globalBefore   int
		globalAfter    int
		updatedAtMoves bool
	}{
		{name: "default_three_becomes_twelve", globalBefore: 3, globalAfter: 12, updatedAtMoves: true},
		{name: "custom_twenty_is_preserved", globalBefore: 20, globalAfter: 20, updatedAtMoves: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
			migratorDB := fixture.openRolePool(t, ctx, appACLCurrentTransitionMigrator)
			oldFS := appACLCurrentTransitionTestFS(t)
			delete(oldFS, "0063_tune_heartbeat_incident_policy.sql")
			oldFragments := append([]AppACLCurrentMigrationFragment(nil), appACLCurrentMigrationFragments[:len(appACLCurrentMigrationFragments)-1]...)
			oldSource, err := compileAppACLCurrentSourceContract(oldFS, oldFragments)
			if err != nil {
				t.Fatalf("compile exact v0.79.4 source: %v", err)
			}
			if !reflect.DeepEqual(oldSource.sources.canonicalSet, appACLCurrentV0794MigrationGolden) {
				t.Fatal("exact predecessor fixture differs from independent v0.79.4 migration golden")
			}
			oldDependencies := defaultAppACLCurrentConvergenceDependencies()
			oldDependencies.transitionDefinitions = nil
			predecessor, err := convergeAppACLCurrentWithDependencies(
				ctx,
				func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					return migratorDB.BeginTx(ctx, options)
				},
				appACLCurrentTransitionBindings[0].CatalogRole,
				appACLCurrentTransitionBindings[1].CatalogRole,
				oldFS,
				oldFragments,
				oldDependencies,
			)
			if err != nil {
				t.Fatalf("converge exact v0.79.4 predecessor: %v", err)
			}
			if predecessor.ManifestDigest != appACLCurrentV0794ManifestDigestGolden || predecessor.ManifestRevision != 1 {
				t.Fatalf("predecessor manifest = revision %d digest %x, want frozen revision 1 %x", predecessor.ManifestRevision, predecessor.ManifestDigest, appACLCurrentV0794ManifestDigestGolden)
			}

			oldUpdatedAt := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
			if _, err := migratorDB.Exec(ctx, `
					insert into public.center_settings (
					  settings_id, telegram_bot_token, telegram_chat_id, telegram_runtime_managed,
					  host_sample_frequency_tier, probe_frequency_defaults, incident_defaults,
					  override_rules, retention_policy, feishu_enabled, feishu_webhook_url,
					  created_at, updated_at
					)
					values (
					  'center',
					  'successor-fixture-telegram-token',
					  'successor-fixture-chat',
					  true,
					  '15s',
					  '{"tcp":"11s","http":"12s","tls":"13m"}'::jsonb,
					  jsonb_build_object(
				    'heartbeat_interval_seconds', 5,
				    'stale_threshold_intervals', $1::int,
				    'sweep_interval_seconds', 5,
				    'notify_on_started', true,
				    'notify_on_escalated', true,
				    'notify_on_recovered', true
					  ),
					  '{"monitoring_instance_labels":[{"label":"edge","overrides":{"incident_defaults":{"stale_threshold_intervals":3}}}],"target_types":[],"target_labels":[]}'::jsonb,
					  '{"raw_layer_days":31,"aggregate_layer_days":32,"event_layer_days":91,"notification_layer_days":181}'::jsonb,
					  true,
					  'https://successor-fixture.invalid/webhook',
					  '2024-12-01 01:02:03+00'::timestamptz,
					  $2
					)
				`, tc.globalBefore, oldUpdatedAt); err != nil {
				t.Fatalf("seed predecessor settings: %v", err)
			}
			seedAppACLCurrentSuccessorHeartbeatRows(t, ctx, migratorDB)
			settingsBefore := readAppACLCurrentSettingsExceptTransitionDigest(t, ctx, migratorDB)
			heartbeatsBefore := readAppACLCurrentSuccessorHeartbeatDigest(t, ctx, migratorDB)

			successor, err := ConvergeAppACLCurrent(
				ctx,
				migratorDB,
				appACLCurrentTransitionBindings[0].CatalogRole,
				appACLCurrentTransitionBindings[1].CatalogRole,
			)
			if err != nil {
				t.Fatalf("ConvergeAppACLCurrent() registered successor: %v", err)
			}
			if successor.ManifestRevision != 2 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
				t.Fatalf("successor manifest = %#v, want revision 2 linked to frozen predecessor", successor)
			}
			assertSingleIntValue(t, ctx, migratorDB, `select count(*)::int from public.schema_migrations`, 64)
			assertSingleIntValue(t, ctx, migratorDB, `select count(*)::int from public.schema_migrations where name = '0063_tune_heartbeat_incident_policy.sql'`, 1)
			assertSingleIntValue(t, ctx, migratorDB, `select count(*)::int from public.app_acl_manifest_revisions`, 2)
			assertSingleIntValue(t, ctx, migratorDB, `select manifest_revision::int from public.app_acl_manifest_head where singleton`, 2)

			var globalThreshold, overrideThreshold int
			var updatedAt time.Time
			if err := migratorDB.QueryRow(ctx, `
				select (incident_defaults->>'stale_threshold_intervals')::int,
				       (override_rules#>>'{monitoring_instance_labels,0,overrides,incident_defaults,stale_threshold_intervals}')::int,
				       updated_at
		from public.center_settings settings where settings_id = 'center'
			`).Scan(&globalThreshold, &overrideThreshold, &updatedAt); err != nil {
				t.Fatalf("read successor settings: %v", err)
			}
			if globalThreshold != tc.globalAfter || overrideThreshold != 3 {
				t.Fatalf("successor thresholds = %d/%d, want %d/3", globalThreshold, overrideThreshold, tc.globalAfter)
			}
			if tc.updatedAtMoves && !updatedAt.After(oldUpdatedAt) {
				t.Fatalf("default migration updated_at = %s, want after %s", updatedAt, oldUpdatedAt)
			}
			if !tc.updatedAtMoves && !updatedAt.Equal(oldUpdatedAt) {
				t.Fatalf("custom migration updated_at = %s, want preserved %s", updatedAt, oldUpdatedAt)
			}
			if settingsAfter := readAppACLCurrentSettingsExceptTransitionDigest(t, ctx, migratorDB); settingsAfter != settingsBefore {
				t.Fatal("registered successor changed a center_settings field outside incident_defaults/updated_at")
			}
			if heartbeatsAfter := readAppACLCurrentSuccessorHeartbeatDigest(t, ctx, migratorDB); heartbeatsAfter != heartbeatsBefore {
				t.Fatal("registered successor changed existing heartbeat rows")
			}
			assertAppACLCurrentSuccessorHeartbeatIndexShape(t, ctx, migratorDB)

			runtimeDB := fixture.openRolePool(t, ctx, appACLCurrentTransitionBindings[0].CatalogRole)
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("AdmitAppACLCurrentRuntime() registered successor: %v", err)
			}
			_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			beforeRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, appACLCurrentTransitionBindings[0].CatalogRole, appACLCurrentTransitionBindings[1].CatalogRole)
			if err != nil {
				t.Fatalf("ConvergeAppACLCurrent() registered successor repeat: %v", err)
			}
			afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
			if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
				t.Fatalf("registered successor repeat changed durable state\nbefore: %#v\nafter: %#v", beforeRepeat, afterRepeat)
			}
			convergenceFixture := fixture.asConvergenceFixture()
			assertRecordsCoreAppACLCurrentRolePrivileges(t, ctx, &convergenceFixture, runtimeDB)
		})
	}
}

func testPostgresIntegrationAppACLCurrentRegisteredSuccessorRejectsInvalidPredecessor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(context.Context, *pgxpool.Pool) error
		wantError string
	}{
		{
			name: "preexisting_wrong_named_index",
			mutate: func(ctx context.Context, db *pgxpool.Pool) error {
				_, err := db.Exec(ctx, `
					create index idx_monitoring_instance_heartbeats_live_received
					on public.monitoring_instance_heartbeats (received_at)
				`)
				return err
			},
			wantError: "already has reserved heartbeat index",
		},
		{
			name: "wrong_released_default",
			mutate: func(ctx context.Context, db *pgxpool.Pool) error {
				_, err := db.Exec(ctx, `
					alter table public.center_settings
					alter column incident_defaults set default '{}'::jsonb
				`)
				return err
			},
			wantError: "predecessor default",
		},
		{
			name: "partial_0063_default_and_data_without_index",
			mutate: func(ctx context.Context, db *pgxpool.Pool) error {
				_, err := db.Exec(ctx, `
					alter table public.center_settings
					alter column incident_defaults set default '{"heartbeat_interval_seconds": 5, "stale_threshold_intervals": 12, "sweep_interval_seconds": 5, "notify_on_started": true, "notify_on_escalated": true, "notify_on_recovered": true}'::jsonb;
					update public.center_settings
					set incident_defaults = jsonb_set(incident_defaults, '{stale_threshold_intervals}', '12'::jsonb, false),
					    updated_at = clock_timestamp()
					where settings_id = 'center'
				`)
				return err
			},
			wantError: "predecessor default",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			state := seedExactAppACLCurrentPredecessor(t, ctx, 3)
			if err := tc.mutate(ctx, state.migratorDB); err != nil {
				t.Fatalf("seed invalid predecessor shape: %v", err)
			}
			before := readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, state.input)

			_, err := ConvergeAppACLCurrent(
				ctx,
				state.migratorDB,
				appACLCurrentTransitionBindings[0].CatalogRole,
				appACLCurrentTransitionBindings[1].CatalogRole,
			)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("ConvergeAppACLCurrent() error = %v, want %q", err, tc.wantError)
			}
			after := readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, state.input)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected predecessor changed durable state\nbefore: %#v\nafter:  %#v", before, after)
			}
			assertSingleIntValue(t, ctx, state.migratorDB, `select count(*)::int from public.schema_migrations`, 63)
			assertSingleIntValue(t, ctx, state.migratorDB, `select count(*)::int from public.app_acl_manifest_revisions`, 1)
			assertSingleIntValue(t, ctx, state.migratorDB, `select manifest_revision::int from public.app_acl_manifest_head where singleton`, 1)
		})
	}

	t.Run("unexpected_other_settings_drift_rolls_back", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		state := seedExactAppACLCurrentPredecessor(t, ctx, 3)
		before := readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, state.input)
		dependencies := defaultAppACLCurrentConvergenceDependencies()
		applyPending := dependencies.applyPending
		const secretMarker = "unexpected-settings-secret-marker"
		dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, source migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
			if err := applyPending(ctx, tx, source, applied); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				update public.center_settings
				set telegram_bot_token = $1
				where settings_id = 'center'
			`, secretMarker)
			return err
		}
		_, err := convergeAppACLCurrentWithDependencies(
			ctx,
			func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				return state.migratorDB.BeginTx(ctx, options)
			},
			appACLCurrentTransitionBindings[0].CatalogRole,
			appACLCurrentTransitionBindings[1].CatalogRole,
			migrations.FS,
			appACLCurrentMigrationFragments,
			dependencies,
		)
		if err == nil || !strings.Contains(err.Error(), "changed non-incident settings") {
			t.Fatalf("ConvergeAppACLCurrent() drift error = %v, want non-incident settings rejection", err)
		}
		if strings.Contains(err.Error(), secretMarker) {
			t.Fatal("registered successor drift error leaked a settings value")
		}
		after := readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, state.input)
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("settings-drift rejection did not roll back the full successor transaction\nbefore: %#v\nafter:  %#v", before, after)
		}
	})
}

type exactAppACLCurrentPredecessorState struct {
	migratorDB *pgxpool.Pool
	input      appACLEffectiveCatalogVerifierInput
}

func seedExactAppACLCurrentPredecessor(t *testing.T, ctx context.Context, globalThreshold int) exactAppACLCurrentPredecessorState {
	t.Helper()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, appACLCurrentTransitionMigrator)
	oldFS := appACLCurrentTransitionTestFS(t)
	delete(oldFS, "0063_tune_heartbeat_incident_policy.sql")
	oldFragments := append([]AppACLCurrentMigrationFragment(nil), appACLCurrentMigrationFragments[:len(appACLCurrentMigrationFragments)-1]...)
	oldSource, err := compileAppACLCurrentSourceContract(oldFS, oldFragments)
	if err != nil {
		t.Fatalf("compile exact v0.79.4 source: %v", err)
	}
	if !reflect.DeepEqual(oldSource.sources.canonicalSet, appACLCurrentV0794MigrationGolden) {
		t.Fatal("exact predecessor fixture differs from independent v0.79.4 migration golden")
	}
	oldDependencies := defaultAppACLCurrentConvergenceDependencies()
	oldDependencies.transitionDefinitions = nil
	predecessor, err := convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return migratorDB.BeginTx(ctx, options)
		},
		appACLCurrentTransitionBindings[0].CatalogRole,
		appACLCurrentTransitionBindings[1].CatalogRole,
		oldFS,
		oldFragments,
		oldDependencies,
	)
	if err != nil {
		t.Fatalf("converge exact v0.79.4 predecessor: %v", err)
	}
	if predecessor.ManifestDigest != appACLCurrentV0794ManifestDigestGolden || predecessor.ManifestRevision != 1 {
		t.Fatalf("predecessor manifest = revision %d digest %x, want frozen revision 1 %x", predecessor.ManifestRevision, predecessor.ManifestDigest, appACLCurrentV0794ManifestDigestGolden)
	}
	if _, err := migratorDB.Exec(ctx, `
		insert into public.center_settings (settings_id, incident_defaults, override_rules, updated_at)
		values (
		  'center',
		  jsonb_build_object(
		    'heartbeat_interval_seconds', 5,
		    'stale_threshold_intervals', $1::int,
		    'sweep_interval_seconds', 5,
		    'notify_on_started', true,
		    'notify_on_escalated', true,
		    'notify_on_recovered', true
		  ),
		  '{"monitoring_instance_labels":[{"label":"edge","overrides":{"incident_defaults":{"stale_threshold_intervals":3}}}],"target_types":[],"target_labels":[]}'::jsonb,
		  '2025-01-02 03:04:05+00'::timestamptz
		)
	`, globalThreshold); err != nil {
		t.Fatalf("seed predecessor settings: %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	return exactAppACLCurrentPredecessorState{migratorDB: migratorDB, input: input}
}

type appACLCurrentTransitionDurableState struct {
	Base                           appACLCurrentPostgresDurableSnapshot
	IncidentDefaults               []byte
	SettingsExceptTransitionDigest [32]byte
	SettingsUpdated                time.Time
	ColumnDefault                  string
	IndexDefinitions               []string
	HeartbeatRowsDigest            [32]byte
}

func readAppACLCurrentTransitionDurableState(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	input appACLEffectiveCatalogVerifierInput,
) appACLCurrentTransitionDurableState {
	t.Helper()
	state := appACLCurrentTransitionDurableState{Base: readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)}
	var settingsExceptTransition []byte
	if err := db.QueryRow(ctx, `
			select incident_defaults,
			       to_jsonb(settings) - array['incident_defaults', 'updated_at']::text[],
			       updated_at
			from public.center_settings settings where settings_id = 'center'
		`).Scan(&state.IncidentDefaults, &settingsExceptTransition, &state.SettingsUpdated); err != nil {
		t.Fatalf("read transition settings state: %v", err)
	}
	state.SettingsExceptTransitionDigest = sha256.Sum256(settingsExceptTransition)
	state.HeartbeatRowsDigest = readAppACLCurrentSuccessorHeartbeatDigest(t, ctx, db)
	if err := db.QueryRow(ctx, `
		select pg_get_expr(defaults.adbin, defaults.adrelid)
		from pg_catalog.pg_attrdef defaults
		join pg_catalog.pg_class relations on relations.oid = defaults.adrelid
		join pg_catalog.pg_namespace namespaces on namespaces.oid = relations.relnamespace
		join pg_catalog.pg_attribute attributes
		  on attributes.attrelid = relations.oid and attributes.attnum = defaults.adnum
		where namespaces.nspname = 'public'
		  and relations.relname = 'center_settings'
		  and attributes.attname = 'incident_defaults'
	`).Scan(&state.ColumnDefault); err != nil {
		t.Fatalf("read transition column default state: %v", err)
	}
	rows, err := db.Query(ctx, `
		select pg_get_indexdef(indexes.indexrelid)
		from pg_catalog.pg_index indexes
		join pg_catalog.pg_class relations on relations.oid = indexes.indrelid
		join pg_catalog.pg_namespace namespaces on namespaces.oid = relations.relnamespace
		where namespaces.nspname = 'public'
		  and relations.relname = 'monitoring_instance_heartbeats'
		order by indexes.indexrelid::regclass::text collate "C"
	`)
	if err != nil {
		t.Fatalf("read transition index state: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil {
			t.Fatalf("scan transition index state: %v", err)
		}
		state.IndexDefinitions = append(state.IndexDefinitions, definition)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate transition index state: %v", err)
	}
	return state
}

func seedAppACLCurrentSuccessorHeartbeatRows(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into public.monitoring_instances (
		  monitoring_instance_id, display_name, region, city, provider, lifecycle_status
		) values ('mi_successor', 'Successor fixture', 'HK', 'Hong Kong', 'Test Provider', '在用');
		insert into public.monitoring_instance_heartbeats (
		  monitoring_instance_id, observed_at, received_at, agent_version,
		  fingerprint, sync_batch_id, is_backfilled
		) values
		  ('mi_successor', '2026-08-30 01:02:03+00', '2026-08-30 01:02:04+00', 'v0.79.4', 'successor-a', 'batch-a', false),
		  ('mi_successor', '2026-08-30 01:03:03+00', '2026-08-30 01:03:04+00', 'v0.79.4', 'successor-b', 'batch-b', true)
	`); err != nil {
		t.Fatalf("seed registered successor heartbeat rows: %v", err)
	}
}

func readAppACLCurrentSettingsExceptTransitionDigest(t *testing.T, ctx context.Context, db *pgxpool.Pool) [32]byte {
	t.Helper()
	var body []byte
	if err := db.QueryRow(ctx, `
		select to_jsonb(settings) - array['incident_defaults', 'updated_at']::text[]
		from public.center_settings settings
		where settings_id = 'center'
	`).Scan(&body); err != nil {
		t.Fatalf("read center_settings non-transition fields: %v", err)
	}
	return sha256.Sum256(body)
}

func readAppACLCurrentSuccessorHeartbeatDigest(t *testing.T, ctx context.Context, db *pgxpool.Pool) [32]byte {
	t.Helper()
	var body []byte
	if err := db.QueryRow(ctx, `
		select coalesce(jsonb_agg(to_jsonb(heartbeats) order by id), '[]'::jsonb)
		from public.monitoring_instance_heartbeats heartbeats
	`).Scan(&body); err != nil {
		t.Fatalf("read registered successor heartbeat rows: %v", err)
	}
	return sha256.Sum256(body)
}

func assertAppACLCurrentSuccessorHeartbeatIndexShape(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var valid, ready, unique bool
	var accessMethod, predicate string
	var keyCount, attributeCount int16
	var attributes []string
	var options []int16
	if err := db.QueryRow(ctx, `
		select indexes.indisvalid,
		       indexes.indisready,
		       indexes.indisunique,
		       methods.amname,
		       indexes.indnkeyatts,
		       indexes.indnatts,
		       array(
		         select attributes.attname
		         from unnest(indexes.indkey::smallint[]) with ordinality keys(attnum, ordinal)
		         join pg_catalog.pg_attribute attributes
		           on attributes.attrelid = indexes.indrelid and attributes.attnum = keys.attnum
		         order by keys.ordinal
		       ),
		       indexes.indoption::smallint[],
		       pg_get_expr(indexes.indpred, indexes.indrelid)
		from pg_catalog.pg_index indexes
		join pg_catalog.pg_class index_rel on index_rel.oid = indexes.indexrelid
		join pg_catalog.pg_class table_rel on table_rel.oid = indexes.indrelid
		join pg_catalog.pg_namespace namespaces on namespaces.oid = table_rel.relnamespace
		join pg_catalog.pg_am methods on methods.oid = index_rel.relam
		where namespaces.nspname = 'public'
		  and table_rel.relname = 'monitoring_instance_heartbeats'
		  and index_rel.relname = 'idx_monitoring_instance_heartbeats_live_received'
	`).Scan(&valid, &ready, &unique, &accessMethod, &keyCount, &attributeCount, &attributes, &options, &predicate); err != nil {
		t.Fatalf("read independent registered successor index shape: %v", err)
	}
	if !valid || !ready || unique || accessMethod != "btree" || keyCount != 3 || attributeCount != 4 ||
		!reflect.DeepEqual(attributes, []string{"monitoring_instance_id", "received_at", "id", "sync_batch_id"}) ||
		!reflect.DeepEqual(options, []int16{0, 3, 3}) || strings.Join(strings.Fields(predicate), " ") != "(is_backfilled = false)" {
		t.Fatalf("independent registered successor index shape = valid:%t ready:%t unique:%t method:%q keys:%d attrs:%d columns:%#v options:%#v predicate:%q",
			valid, ready, unique, accessMethod, keyCount, attributeCount, attributes, options, predicate)
	}
}

type exactAppACLCurrentSuccessorPostgresFixture struct {
	db             *pgxpool.Pool
	databaseName   string
	bootstrapOwner string
	passwords      map[string]string
}

func newExactAppACLCurrentSuccessorPostgresFixture(t *testing.T, ctx context.Context) exactAppACLCurrentSuccessorPostgresFixture {
	t.Helper()
	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for postgres integration tests", postgresIntegrationFlag)
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for postgres integration tests")
	}
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL integration URL: %v", err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open exact-successor admin PostgreSQL pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	fixture := exactAppACLCurrentSuccessorPostgresFixture{
		databaseName: appACLCurrentTransitionDatabase,
		passwords:    make(map[string]string, 3),
	}
	if err := adminPool.QueryRow(ctx, `select current_user`).Scan(&fixture.bootstrapOwner); err != nil {
		t.Fatalf("read exact-successor bootstrap owner: %v", err)
	}
	for _, role := range []string{
		appACLCurrentTransitionBindings[0].CatalogRole,
		appACLCurrentTransitionBindings[1].CatalogRole,
		appACLCurrentTransitionMigrator,
	} {
		password := appACLEffectiveCatalogTemporaryPassword(t)
		fixture.passwords[role] = password
		if _, err := adminPool.Exec(ctx, `create role `+quotePostgresIdentifier(role)+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+password+`'`); err != nil {
			t.Fatalf("create exact-successor role %q: %v", role, err)
		}
	}
	if _, err := adminPool.Exec(ctx, `create database `+quotePostgresIdentifier(fixture.databaseName)+` owner `+quotePostgresIdentifier(appACLCurrentTransitionMigrator)); err != nil {
		t.Fatalf("create exact-successor database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, `drop database if exists `+quotePostgresIdentifier(fixture.databaseName)+` with (force)`); err != nil {
			t.Errorf("drop exact-successor database: %v", err)
		}
		for _, role := range []string{
			appACLCurrentTransitionBindings[0].CatalogRole,
			appACLCurrentTransitionBindings[1].CatalogRole,
			appACLCurrentTransitionMigrator,
		} {
			if _, err := adminPool.Exec(cleanupCtx, `drop role if exists `+quotePostgresIdentifier(role)); err != nil {
				t.Errorf("drop exact-successor role %q: %v", role, err)
			}
		}
	})
	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = fixture.databaseName
	fixture.db, err = pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open exact-successor database: %v", err)
	}
	t.Cleanup(fixture.db.Close)
	return fixture
}

func (fixture exactAppACLCurrentSuccessorPostgresFixture) openRolePool(t *testing.T, ctx context.Context, role string) *pgxpool.Pool {
	t.Helper()
	password, ok := fixture.passwords[role]
	if !ok {
		t.Fatalf("no exact-successor password for role %q", role)
	}
	config := fixture.db.Config().Copy()
	config.MinConns = 0
	config.MaxConns = 1
	config.ConnConfig.User = role
	config.ConnConfig.Password = password
	config.AfterConnect = nil
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open exact-successor role pool %q: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func (fixture exactAppACLCurrentSuccessorPostgresFixture) asConvergenceFixture() appACLConvergencePostgresFixture {
	return appACLConvergencePostgresFixture{
		db:             fixture.db,
		databaseName:   fixture.databaseName,
		bootstrapOwner: fixture.bootstrapOwner,
		runtime:        appACLCurrentTransitionBindings[0].CatalogRole,
		admin:          appACLCurrentTransitionBindings[1].CatalogRole,
		migrator:       appACLCurrentTransitionMigrator,
		rolePasswords:  fixture.passwords,
	}
}
