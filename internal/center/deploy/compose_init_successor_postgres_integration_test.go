//go:build appacl_release_fixture

package deploy

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationComposeInitializeUpgradesExactV0794Predecessor(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 through the strict PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("Compose successor integration requires the ownership-checked ephemeral PostgreSQL runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	baseURL := os.Getenv("HOUFENG_DATABASE_URL")
	baseConfig, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Fatalf("parse strict PostgreSQL fixture URL: %v", err)
	}
	bootstrapPool, err := store.OpenPostgres(ctx, baseURL)
	if err != nil {
		t.Fatalf("open strict PostgreSQL fixture: %v", err)
	}
	t.Cleanup(bootstrapPool.Close)
	assertComposeIntegrationFixtureHasNoProductionState(t, ctx, bootstrapPool)

	config := exactComposeIntegrationConfig(t, baseConfig.ConnConfig.Password)
	createDatabaseDDL := formatComposeIntegrationDDL(t, ctx, bootstrapPool, `CREATE DATABASE %I`, config.DatabaseName)
	if _, err := bootstrapPool.Exec(ctx, createDatabaseDDL); err != nil {
		t.Fatalf("create exact Compose integration database: %v", err)
	}
	t.Cleanup(func() { cleanupComposeIntegrationDatabase(t, context.Background(), bootstrapPool, config) })
	openPostgres := composeIntegrationPostgresOpener(baseConfig)

	seedReleasedV0794ComposePredecessor(t, ctx, openPostgres, config)
	passwordsBefore := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, config)
	authorityBefore, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("load exact predecessor authority state: %v", err)
	}

	if err := initializeCompose(ctx, config, openPostgres); err != nil {
		t.Fatalf("upgrade exact v0.79.4 Compose predecessor through production wiring: %v", err)
	}
	passwordsAfter := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, config)
	if !reflect.DeepEqual(passwordsAfter, passwordsBefore) {
		t.Fatal("Compose successor upgrade rewrote unchanged role password verifiers")
	}
	authorityAfter, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("load upgraded authority state: %v", err)
	}
	if authorityAfter.DeploymentID != authorityBefore.DeploymentID || authorityAfter.DatabasePassword() != authorityBefore.DatabasePassword() {
		t.Fatal("Compose successor upgrade replaced Records authority state")
	}
	assertExactComposeSuccessorState(t, ctx, openPostgres, config)
	assertComposeSuccessorRecordsAndAttachments(t, ctx, openPostgres, config)
	assertComposeIntegrationCatalog(t, ctx, bootstrapPool, openPostgres, config)
	assertComposeIntegrationAuthorityProof(t, ctx, openPostgres, config)

	beforeRepeat := readComposeIntegrationDurableCounts(t, ctx, openPostgres, config)
	if err := initializeCompose(ctx, config, openPostgres); err != nil {
		t.Fatalf("repeat exact Compose successor initialization through production wiring: %v", err)
	}
	afterRepeat := readComposeIntegrationDurableCounts(t, ctx, openPostgres, config)
	if afterRepeat != beforeRepeat {
		t.Fatalf("Compose successor repeat counts = %#v, want unchanged %#v", afterRepeat, beforeRepeat)
	}
	if got := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, config); !reflect.DeepEqual(got, passwordsAfter) {
		t.Fatal("Compose successor repeat rewrote role password verifiers")
	}
}

func exactComposeIntegrationConfig(t *testing.T, bootstrapPassword string) ComposeInitConfig {
	t.Helper()
	config, err := NewComposeInitConfig(ComposeInitPasswords{
		Bootstrap:     bootstrapPassword,
		Runtime:       "exact-runtime-secret",
		PlatformAdmin: "exact-admin-secret",
		Migrator:      "exact-migrator-secret",
	})
	if err != nil {
		t.Fatalf("build exact Compose integration config: %v", err)
	}
	config.AuthorityStateRoot = filepath.Join(t.TempDir(), "records-authority")
	centerConfigDirectory := filepath.Join(t.TempDir(), "center-config")
	if err := os.Mkdir(centerConfigDirectory, 0o755); err != nil {
		t.Fatalf("create exact Compose Center config directory: %v", err)
	}
	config.CenterDeploymentIDPath = filepath.Join(centerConfigDirectory, "deployment-id")
	return config
}

func composeIntegrationPostgresOpener(baseConfig *pgxpool.Config) func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error) {
	return func(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
		config := baseConfig.Copy()
		config.ConnConfig.Database = endpoint.Database
		config.ConnConfig.User = endpoint.Role
		config.ConnConfig.Password = endpoint.Password
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
	}
}

func seedReleasedV0794ComposePredecessor(
	t *testing.T,
	ctx context.Context,
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
	config ComposeInitConfig,
) {
	t.Helper()
	bootstrapPool, err := openPostgres(ctx, composePostgresEndpoint{
		Database: config.DatabaseName,
		Role:     config.BootstrapRole,
		Password: config.Passwords.Bootstrap,
	})
	if err != nil {
		t.Fatalf("open Compose predecessor bootstrap: %v", err)
	}
	authorityState, err := prepareComposeAuthorityState(ctx, bootstrapPool, config.AuthorityStateRoot)
	if err != nil {
		bootstrapPool.Close()
		t.Fatalf("prepare Compose predecessor authority: %v", err)
	}
	if err := platformmigrate.ProvisionComposeBootstrap(ctx, bootstrapPool, platformmigrate.ComposeBootstrapConfig{
		DatabaseName:  config.DatabaseName,
		BootstrapRole: config.BootstrapRole,
		AuthorityRole: config.AuthorityRole,
		Roles:         config.Roles,
		Passwords: platformmigrate.ComposeRolePasswords{
			Runtime:       config.Passwords.Runtime,
			PlatformAdmin: config.Passwords.PlatformAdmin,
			Migrator:      config.Passwords.Migrator,
			Authority:     authorityState.DatabasePassword(),
		},
	}); err != nil {
		bootstrapPool.Close()
		t.Fatalf("provision Compose predecessor bootstrap: %v", err)
	}
	bootstrapPool.Close()

	migratorPool, err := openPostgres(ctx, composePostgresEndpoint{
		Database: config.DatabaseName,
		Role:     config.Roles.Migrator,
		Password: config.Passwords.Migrator,
	})
	if err != nil {
		t.Fatalf("open released predecessor migrator: %v", err)
	}
	predecessor, err := migrate.ConvergeAppACLCurrentV0794ReleaseFixture(
		ctx,
		migratorPool,
		config.Roles.CenterRuntime,
		config.Roles.PlatformAdmin,
	)
	if err != nil {
		migratorPool.Close()
		t.Fatalf("materialize released v0.79.4 predecessor: %v", err)
	}
	if got, want := hex.EncodeToString(predecessor.ManifestDigest[:]), "88220d78d73ce4ba7e3b5647372a27ff749aca1b36854960824a6940059f34cc"; got != want || predecessor.ManifestRevision != 1 {
		migratorPool.Close()
		t.Fatalf("released predecessor = revision %d digest %s, want revision 1 digest %s", predecessor.ManifestRevision, got, want)
	}
	if _, err := migratorPool.Exec(ctx, `
		insert into public.center_settings (settings_id, incident_defaults, override_rules, updated_at)
		values (
		  'center',
		  '{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}'::jsonb,
		  '{"monitoring_instance_labels":[{"label":"edge","overrides":{"incident_defaults":{"stale_threshold_intervals":3}}}],"target_types":[],"target_labels":[]}'::jsonb,
		  '2025-01-02 03:04:05+00'::timestamptz
		)
		on conflict (settings_id) do update
		set incident_defaults = excluded.incident_defaults,
		    override_rules = excluded.override_rules,
		    updated_at = excluded.updated_at;
		insert into public.monitoring_instances (
		  monitoring_instance_id, display_name, region, city, provider, lifecycle_status
		) values ('mi_compose_predecessor', 'Compose predecessor', 'eu', 'berlin', 'fixture', 'active');
		insert into public.monitoring_instance_heartbeats (
		  monitoring_instance_id, observed_at, received_at, agent_version, fingerprint, sync_batch_id, is_backfilled
		) values
		  ('mi_compose_predecessor', '2025-01-02 03:04:06+00', '2025-01-02 03:04:07+00', 'v0.79.4', 'fixture-live', 'batch-live', false),
		  ('mi_compose_predecessor', '2025-01-02 03:04:08+00', '2025-01-02 03:04:09+00', 'v0.79.4', 'fixture-backfill', 'batch-backfill', true)
	`); err != nil {
		migratorPool.Close()
		t.Fatalf("seed released predecessor settings and heartbeat rows: %v", err)
	}
	if err := activateComposeAuthorityState(ctx, migratorPool, config.AuthorityStateRoot, authorityState); err != nil {
		migratorPool.Close()
		t.Fatalf("activate released predecessor authority: %v", err)
	}
	if err := publishComposeAuthorityDeploymentID(config.AuthorityStateRoot, authorityState, config.CenterDeploymentIDPath); err != nil {
		migratorPool.Close()
		t.Fatalf("publish released predecessor authority: %v", err)
	}
	migratorPool.Close()

	authorityPool, err := openPostgres(ctx, composePostgresEndpoint{
		Database: config.DatabaseName,
		Role:     config.AuthorityRole,
		Password: authorityState.DatabasePassword(),
	})
	if err != nil {
		t.Fatalf("open released predecessor authority: %v", err)
	}
	if err := heartbeatComposeAuthority(ctx, authorityPool, authorityState); err != nil {
		authorityPool.Close()
		t.Fatalf("establish released predecessor authority heartbeat: %v", err)
	}
	authorityPool.Close()
}

func assertExactComposeSuccessorState(
	t *testing.T,
	ctx context.Context,
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
	config ComposeInitConfig,
) {
	t.Helper()
	pool, err := openPostgres(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.Migrator, Password: config.Passwords.Migrator})
	if err != nil {
		t.Fatalf("open Compose successor verifier: %v", err)
	}
	defer pool.Close()
	var migrationsCount, revisionCount, headRevision, globalThreshold, overrideThreshold, heartbeatCount int
	var indexDefinition string
	if err := pool.QueryRow(ctx, `
		select (select count(*)::int from public.schema_migrations),
		       (select count(*)::int from public.app_acl_manifest_revisions),
		       (select manifest_revision::int from public.app_acl_manifest_head where singleton),
		       (select (incident_defaults->>'stale_threshold_intervals')::int from public.center_settings where settings_id='center'),
		       (select (override_rules#>>'{monitoring_instance_labels,0,overrides,incident_defaults,stale_threshold_intervals}')::int from public.center_settings where settings_id='center'),
		       (select count(*)::int from public.monitoring_instance_heartbeats where monitoring_instance_id='mi_compose_predecessor'),
		       pg_get_indexdef('public.idx_monitoring_instance_heartbeats_live_received'::regclass)
	`).Scan(&migrationsCount, &revisionCount, &headRevision, &globalThreshold, &overrideThreshold, &heartbeatCount, &indexDefinition); err != nil {
		t.Fatalf("read exact Compose successor state: %v", err)
	}
	const expectedIndex = "CREATE INDEX idx_monitoring_instance_heartbeats_live_received ON public.monitoring_instance_heartbeats USING btree (monitoring_instance_id, received_at DESC, id DESC) INCLUDE (sync_batch_id) WHERE (is_backfilled = false)"
	if migrationsCount != 64 || revisionCount != 2 || headRevision != 2 || globalThreshold != 12 || overrideThreshold != 3 || heartbeatCount != 2 || indexDefinition != expectedIndex {
		t.Fatalf("exact Compose successor = migrations:%d revisions:%d head:%d thresholds:%d/%d heartbeats:%d index:%q", migrationsCount, revisionCount, headRevision, globalThreshold, overrideThreshold, heartbeatCount, indexDefinition)
	}
}

func assertComposeSuccessorRecordsAndAttachments(
	t *testing.T,
	ctx context.Context,
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
	config ComposeInitConfig,
) {
	t.Helper()
	runtimePool, err := openPostgres(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.CenterRuntime, Password: config.Passwords.Runtime})
	if err != nil {
		t.Fatalf("open Compose runtime Records verifier: %v", err)
	}
	defer runtimePool.Close()
	if err := migrate.AdmitAppACLCurrentRuntime(ctx, runtimePool); err != nil {
		t.Fatalf("admit exact Compose successor runtime: %v", err)
	}
	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Compose successor Records transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`insert into public.records (record_id) values ('rec_composesuccessor')`,
		`insert into public.record_revisions (revision_id, record_id, revision_no, title, body_markdown, markdown_dialect_version, record_type, impact_level, visibility_scope, visibility_digest, author_id, canonical_hash) values ('rrv_composesuccessor', 'rec_composesuccessor', 1, 'Compose successor', '# Compose successor', 1, 'note', 'informational', '{}'::jsonb, decode(repeat('51', 32), 'hex'), 'usr_compose', decode(repeat('52', 32), 'hex'))`,
		`insert into public.record_revision_subjects (revision_id, ordinal, registry_version, subject_kind, relation_role, source_id, is_primary, identity_snapshot, capture_authorization, capture_authorization_digest) values ('rrv_composesuccessor', 0, 1, 'vps', 'affected', 'vps_compose_successor', true, '{}'::jsonb, '{}'::jsonb, decode(repeat('53', 32), 'hex'))`,
		`insert into public.blob_objects (blob_key, sha256_digest, object_version, size_bytes, backend_kind) values ('sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', decode(repeat('bb', 32), 'hex'), 'compose-v1', 9, 'local')`,
		`insert into public.attachment_quota_accounts (project_id, logical_bytes, physical_bytes) values ('default', 9, 9)`,
		`insert into public.record_attachments (attachment_id, record_id, attachment_state, display_name, media_type, logical_size_bytes, blob_key, blob_object_version, created_by) values ('att_composesuccessor', 'rec_composesuccessor', 'available', 'compose.txt', 'text/plain', 9, 'sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'compose-v1', 'usr_compose')`,
		`insert into public.record_revision_attachments (record_id, revision_id, ordinal, attachment_id) values ('rec_composesuccessor', 'rrv_composesuccessor', 0, 'att_composesuccessor')`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("execute Compose successor Records/attachment statement %q: %v", statement, err)
		}
	}
	var title, displayName, objectVersion string
	if err := tx.QueryRow(ctx, `
		select revisions.title, attachments.display_name, blobs.object_version
		from public.record_revisions revisions
		join public.record_revision_attachments refs on refs.revision_id = revisions.revision_id
		join public.record_attachments attachments on attachments.attachment_id = refs.attachment_id
		join public.blob_objects blobs on blobs.blob_key = attachments.blob_key and blobs.object_version = attachments.blob_object_version
		where revisions.revision_id = 'rrv_composesuccessor'
	`).Scan(&title, &displayName, &objectVersion); err != nil {
		t.Fatalf("read back Compose successor Records/attachment row: %v", err)
	}
	if title != "Compose successor" || displayName != "compose.txt" || objectVersion != "compose-v1" {
		t.Fatalf("Compose successor readback = %q/%q/%q", title, displayName, objectVersion)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback Compose successor Records fixture: %v", err)
	}
}
