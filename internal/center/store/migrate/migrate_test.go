package migrate

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/db/migrations"
)

const frozenR1RootSourceCount = 52
const currentRootSourceCount = frozenR1RootSourceCount + 9

func TestNamesIncludesBaselineAndFollowupMigrations(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}

	if len(names) < 10 {
		t.Fatalf("len(Names()) = %d, want at least 10", len(names))
	}

	if names[0] != "0001_initial_schema.sql" {
		t.Fatalf("first migration = %q, want %q", names[0], "0001_initial_schema.sql")
	}
	if names[1] != "0002_normalize_status_defaults.sql" {
		t.Fatalf("second migration = %q, want %q", names[1], "0002_normalize_status_defaults.sql")
	}
	if names[2] != "0003_add_sync_token_hash.sql" {
		t.Fatalf("third migration = %q, want %q", names[2], "0003_add_sync_token_hash.sql")
	}
	if names[3] != "0004_add_node_onboarding_binding_state.sql" {
		t.Fatalf("fourth migration = %q, want %q", names[3], "0004_add_node_onboarding_binding_state.sql")
	}
	if names[4] != "0004_add_observation_provenance.sql" {
		t.Fatalf("fifth migration = %q, want %q", names[4], "0004_add_observation_provenance.sql")
	}
	if names[5] != "0005_add_node_binding_epoch.sql" {
		t.Fatalf("sixth migration = %q, want %q", names[5], "0005_add_node_binding_epoch.sql")
	}
	if names[6] != "0006_add_center_settings.sql" {
		t.Fatalf("seventh migration = %q, want %q", names[6], "0006_add_center_settings.sql")
	}
	if names[7] != "0007_add_telegram_runtime_managed.sql" {
		t.Fatalf("eighth migration = %q, want %q", names[7], "0007_add_telegram_runtime_managed.sql")
	}
	if names[8] != "0008_add_retention_aggregates.sql" {
		t.Fatalf("ninth migration = %q, want %q", names[8], "0008_add_retention_aggregates.sql")
	}
	if names[9] != "0009_add_observability_filter_indexes.sql" {
		t.Fatalf("tenth migration = %q, want %q", names[9], "0009_add_observability_filter_indexes.sql")
	}

	expectedTail := []string{
		"0017_add_vps_assets.sql",
		"0018_add_subscriptions.sql",
		"0019_create_vps_node_links.sql",
		"0020_create_renewal_decisions.sql",
		"0021_create_asset_histories.sql",
		"0022_create_experience_logs.sql",
		"0023_create_asset_services.sql",
		"0024_create_asset_domains.sql",
		"0025_add_enrollment_token_consumption.sql",
		"0026_tune_observability_cadence.sql",
		"0027_add_host_sample_capacity_bytes.sql",
		"0028_create_asset_lifecycle_actions.sql",
		"0029_rename_nodes_to_monitoring_instances.sql",
		"0030_vps_first_status_semantics.sql",
		"0031_subscription_periods_and_validity_extension.sql",
		"0032_extend_raw_retention_for_monitoring_windows.sql",
		"0033_subscription_cost_center.sql",
		"0034_subscription_monthly_budgets.sql",
		"0035_create_asset_decision_records.sql",
		"0036_add_asset_decision_member_followups.sql",
		"0037_create_asset_decision_manual_groups.sql",
		"0038_create_asset_decision_scenario_templates.sql",
		"0039_add_ip_quality_settings.sql",
		"0040_create_ip_quality_reports.sql",
		"0041_filter_ip_quality_read_models.sql",
		"0042_extend_ip_quality_source_details.sql",
	}
	offset := 0
	for _, name := range names {
		if offset < len(expectedTail) && name == expectedTail[offset] {
			offset++
		}
	}
	if offset != len(expectedTail) {
		t.Fatalf("migration names missing expected asset ledger tail order %#v in %#v", expectedTail, names)
	}
}

func TestRootMigrationsExcludeObsoleteAppExtensionDraft(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir embedded migrations: %v", err)
	}

	obsoleteRootName := strings.Join([]string{"0052", "add", "app", "extension", "hardening", "receipt.sql"}, "_")
	sqlFiles := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sqlFiles++
		if entry.Name() == obsoleteRootName {
			t.Fatalf("root migrations expose obsolete draft %q", obsoleteRootName)
		}
	}
	if got, want := sqlFiles, currentRootSourceCount; got != want {
		t.Fatalf("root migration count = %d, want current count %d", got, want)
	}
}

func TestFrozenR1RootSourcesRemainExactPrefix(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	if got, want := len(names), currentRootSourceCount; got != want {
		t.Fatalf("root migration name count = %d, want current count %d", got, want)
	}
	if got, want := names[frozenR1RootSourceCount-1], "0051_create_record_platform_foundation.sql"; got != want {
		t.Fatalf("final frozen r1 migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount], "0052_create_records_core.sql"; got != want {
		t.Fatalf("first current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+1], "0053_create_record_attachments.sql"; got != want {
		t.Fatalf("second current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+2], "0054_create_record_evidence.sql"; got != want {
		t.Fatalf("third current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+3], "0055_create_record_collaboration.sql"; got != want {
		t.Fatalf("fourth current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+4], "0056_create_record_search.sql"; got != want {
		t.Fatalf("fifth current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+5], "0057_create_record_activity.sql"; got != want {
		t.Fatalf("sixth current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+6], "0058_create_record_portability.sql"; got != want {
		t.Fatalf("seventh current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+7], "0059_relax_portability_blob_key_regex.sql"; got != want {
		t.Fatalf("eighth current extension migration = %q, want %q", got, want)
	}
	if got, want := names[frozenR1RootSourceCount+8], "0060_create_records_authority_heartbeat.sql"; got != want {
		t.Fatalf("ninth current extension migration = %q, want %q", got, want)
	}

	snapshot, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatalf("snapshotMigrationSources(root) error = %v", err)
	}
	if got, want := len(snapshot.names), currentRootSourceCount; got != want {
		t.Fatalf("root snapshot count = %d, want current count %d", got, want)
	}
	if !reflect.DeepEqual(snapshot.names, names) {
		t.Fatalf("root snapshot names = %#v, want Names() %#v", snapshot.names, names)
	}
}

func TestSnapshotMigrationSourcesCapturesExactLexicalEmbeddedSet(t *testing.T) {
	embeddedEntries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir embedded migrations: %v", err)
	}
	wantNames := make([]string, 0, len(embeddedEntries))
	for _, entry := range embeddedEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		wantNames = append(wantNames, entry.Name())
	}
	sort.Strings(wantNames)

	snapshot, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}
	if len(snapshot.names) != currentRootSourceCount {
		t.Fatalf("snapshot migration name count = %d, want %d", len(snapshot.names), currentRootSourceCount)
	}
	if !reflect.DeepEqual(snapshot.names, wantNames) {
		t.Fatalf("snapshot migration names = %#v, want embedded lexical names %#v", snapshot.names, wantNames)
	}
	for index := 1; index < len(snapshot.names); index++ {
		if snapshot.names[index-1] >= snapshot.names[index] {
			t.Fatalf("snapshot migration names are not lexical at %d: %q then %q", index, snapshot.names[index-1], snapshot.names[index])
		}
	}
	if snapshot.names[3] != "0004_add_node_onboarding_binding_state.sql" || snapshot.names[4] != "0004_add_observation_provenance.sql" {
		t.Fatalf("snapshot duplicate 0004 lexical order = %q, %q", snapshot.names[3], snapshot.names[4])
	}
	entries, err := ParseCanonicalMigrationSetBodyV1(snapshot.canonicalSet)
	if err != nil {
		t.Fatalf("ParseCanonicalMigrationSetBodyV1() error = %v", err)
	}
	if len(entries) != len(snapshot.names) {
		t.Fatalf("canonical snapshot entries = %d, want %d", len(entries), len(snapshot.names))
	}
	for index, entry := range entries {
		if entry.Filename != snapshot.names[index] {
			t.Fatalf("canonical snapshot entry %d = %q, want %q", index, entry.Filename, snapshot.names[index])
		}
		if snapshot.sources[entry.Filename].checksum == "" {
			t.Fatalf("snapshot source %q has no checksum", entry.Filename)
		}
	}
}

func TestIPQualitySourceDetailsMigrationExtendsReadModel(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0042_extend_ip_quality_source_details.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"coverage_json jsonb",
		"diagnostics_json jsonb",
		"status text not null default 'success'",
		"source_type text not null default 'default'",
		"probe_status text not null default 'success'",
		"idx_ip_quality_service_unlocks_report_service_source",
		"assigned_reports.coverage_json",
		"r.status in ('success', 'partial')",
		"r.ip_address <> '0.0.0.0'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0042 migration missing %q", want)
		}
	}
	if !strings.Contains(sql, "drop index if exists idx_ip_quality_service_unlocks_report_service") {
		t.Fatalf("0042 migration must replace legacy service unique index")
	}
}

func TestSessionHashMigrationRemovesPlaintextSessionIDs(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0044_hash_session_ids.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"delete from sessions",
		"rename column session_id to session_id_hash",
		"column_name = 'session_id'",
		"column_name = 'session_id_hash'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0044 migration missing %q", want)
		}
	}
}

func TestAgentSyncBatchReplayMigrationCreatesIdempotencyTable(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0045_create_agent_sync_batches.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"create table if not exists agent_sync_batches",
		"monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade",
		"sync_batch_id text not null",
		"primary key (monitoring_instance_id, sync_batch_id)",
		"create index if not exists idx_agent_sync_batches_received_at",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0045 migration missing %q", want)
		}
	}
}

func TestCommandActionAuditMigrationCreatesMetadataOnlyAuditTable(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0046_create_command_action_audit.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"create table if not exists monitoring_instance_command_action_audit",
		"audit_id text primary key",
		"action_id text not null",
		"monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade",
		"command_id text not null",
		"sensitivity text not null",
		"event_type text not null",
		"actor_user_id text references users(user_id) on delete set null",
		"source text not null",
		"exit_code integer",
		"occurred_at timestamptz not null default now()",
		"details jsonb not null default '{}'::jsonb",
		"sensitivity in ('standard', 'sensitive')",
		"event_type in ('queued', 'dispatched', 'completed')",
		"source in ('web', 'agent_sync')",
		"idx_monitoring_instance_command_action_audit_instance_time",
		"idx_monitoring_instance_command_action_audit_action_time",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0046 migration missing %q", want)
		}
	}
	if strings.Contains(sql, "stdout") || strings.Contains(sql, "stderr") {
		t.Fatalf("0046 migration must not create stdout/stderr audit columns")
	}
}

func TestCommandActionAuditExtensionMigrationPreservesPermanentMetadata(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0050_extend_command_action_audit.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"add column if not exists monitoring_instance_name_snapshot text not null default ''",
		"add column if not exists actor_username_snapshot text not null default ''",
		"add column if not exists actor_display_name_snapshot text not null default ''",
		"monitoring_instance_name_snapshot = mi.display_name",
		"then u.username",
		"then u.display_name",
		"alter column action_id drop not null",
		"command_action_audit_event_type_allowed",
		"event_type in ('queued', 'dispatched', 'completed', 'rejected')",
		"command_action_audit_action_identity_valid",
		"command_action_audit_rejected_source_valid",
		"command_action_audit_rejected_reason_valid",
		"sensitive_confirmation_required",
		"command_action_audit_details_metadata_only",
		"not jsonb_path_exists",
		"exists(@.stdout) || exists(@.stderr)",
		"idx_monitoring_instance_command_action_audit_global_time",
		"on monitoring_instance_command_action_audit(occurred_at desc, audit_id desc)",
		"from pg_constraint",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0050 migration missing %q", want)
		}
	}
	if strings.Contains(sql, "delete from monitoring_instance_command_action_audit") {
		t.Fatal("0050 migration must preserve command audit rows")
	}
}

func TestIPQualityStaleAfterSettingsMigrationRebuildsReadModel(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0047_ip_quality_stale_after_settings.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"stale_after_seconds",
		"604800",
		"drop view if exists ip_quality_latest_vps_summaries",
		"drop view if exists ip_quality_assigned_vps_reports",
		"center_settings",
		"ip_quality_settings",
		"make_interval(secs =>",
		"assigned_reports.observed_at < now() - make_interval",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0047 migration missing %q", want)
		}
	}
	if strings.Contains(sql, "interval '7 days'") {
		t.Fatalf("0047 migration must not keep the hardcoded 7-day stale window")
	}
}

func TestSubscriptionGiftRenewalModeMigrationRelaxesConstraints(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0048_subscription_gift_renewal_mode.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"subscriptions_renewal_mode_allowed",
		"price_histories_renewal_mode_allowed",
		"renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'gift', 'bonus', 'other')",
		"from_renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'gift', 'bonus', 'other')",
		"to_renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'gift', 'bonus', 'other')",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0048 migration missing %q", want)
		}
	}
}

func TestVPSAssetStateCombinationMigrationAddsCrossColumnConstraint(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0049_vps_asset_state_combination_constraint.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"vps_assets_state_combination_valid",
		"lifecycle_status <> 'cancelled'",
		"renewal_decision in ('cancel', 'auto_renew_cancelled')",
		"usage_status <> 'in_use'",
		"lifecycle_status <> 'to_cancel'",
		"lifecycle_status <> 'to_migrate'",
		"renewal_decision = 'migrate'",
		"renewal_decision <> 'replaced'",
		"lifecycle_status <> 'active'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0049 migration missing %q", want)
		}
	}
	if strings.Contains(sql, "not valid") {
		t.Fatalf("0049 migration must validate existing rows instead of adding a not valid constraint")
	}
}

func TestVPSAssetStateCombinationMigrationNormalizesExistingRowsBeforeConstraint(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0049_vps_asset_state_combination_constraint.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	const addConstraint = "add constraint vps_assets_state_combination_valid"
	const updateAssets = "update vps_assets"
	addConstraintIndex := strings.Index(sql, addConstraint)
	if addConstraintIndex < 0 {
		t.Fatalf("0049 migration missing %q", addConstraint)
	}
	updateIndex := strings.Index(sql, updateAssets)
	if updateIndex < 0 {
		t.Fatalf("0049 migration must normalize existing vps_assets rows before adding constraint")
	}
	if updateIndex > addConstraintIndex {
		t.Fatalf("0049 migration normalizes vps_assets after adding constraint")
	}
	for _, want := range []string{
		"lifecycle_status = 'cancelled'",
		"renewal_decision not in ('cancel', 'auto_renew_cancelled')",
		"usage_status = 'in_use'",
		"lifecycle_status = 'to_cancel'",
		"lifecycle_status = 'to_migrate'",
		"renewal_decision <> 'migrate'",
		"renewal_decision = 'replaced'",
		"lifecycle_status = 'active'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0049 migration normalization missing %q", want)
		}
	}
}

func TestIPQualityReadModelFilterMigrationHidesFailurePlaceholders(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0041_filter_ip_quality_read_models.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, viewName := range []string{"ip_quality_latest_vps_summaries", "ip_quality_assigned_vps_reports"} {
		dropIndex := strings.Index(sql, "drop view if exists "+viewName)
		createIndex := strings.Index(sql, "create or replace view "+viewName)
		if dropIndex < 0 {
			t.Fatalf("0041 migration must drop %s before changing read model", viewName)
		}
		if createIndex < 0 {
			t.Fatalf("0041 migration must recreate %s with create or replace", viewName)
		}
		if dropIndex > createIndex {
			t.Fatalf("0041 migration drops %s after recreating it", viewName)
		}
	}
	for _, want := range []string{
		"r.status in ('success', 'partial')",
		"r.ip_address <> '0.0.0.0'",
		"r.ip_version in (4, 6)",
		"from vps_monitoring_instance_links l\n    where l.vps_id = v.vps_id\n      and l.unlinked_at is null",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0041 migration missing IP quality read filter %q", want)
		}
	}
	if strings.Contains(sql, "from active_link_reports linked") {
		t.Fatalf("0041 migration must not let invalid linked reports fall back to IP matching")
	}
}

func TestIPQualityMigrationViewUsesAssignedReportsAlias(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0040_create_ip_quality_reports.sql")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	sql := string(payload)
	if strings.Contains(sql, "pr.report_id = ranked.report_id") {
		t.Fatalf("ip quality migration contains stale ranked alias in assigned view")
	}
	if !strings.Contains(sql, "pr.report_id = assigned_reports.report_id") {
		t.Fatalf("ip quality migration missing provider join on assigned_reports")
	}
}

func TestApplyFSExecutesPendingMigrationsInSortedOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql":         {Data: []byte("select 2;")},
		"0001_initial_schema.sql": {Data: []byte("select 1;")},
		"README.txt":              {Data: []byte("ignore me")},
	}
	store := &fakeMigrationStore{}

	if err := applyFS(context.Background(), store, fsys); err != nil {
		t.Fatalf("applyFS() error = %v", err)
	}

	if store.ensureCalls != 1 {
		t.Fatalf("ensureCalls = %d, want 1", store.ensureCalls)
	}

	wantExec := []string{"select 1;", "select 2;"}
	if !reflect.DeepEqual(store.execSQL, wantExec) {
		t.Fatalf("execSQL = %#v, want %#v", store.execSQL, wantExec)
	}

	wantRecorded := []string{"0001_initial_schema.sql", "0002_second.sql"}
	if !reflect.DeepEqual(store.recorded, wantRecorded) {
		t.Fatalf("recorded = %#v, want %#v", store.recorded, wantRecorded)
	}
}

func TestApplyFSSkipsAlreadyAppliedMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql":         {Data: []byte("select 2;")},
		"0001_initial_schema.sql": {Data: []byte("select 1;")},
	}
	store := &fakeMigrationStore{
		applied: map[string]bool{"0001_initial_schema.sql": true},
	}

	if err := applyFS(context.Background(), store, fsys); err != nil {
		t.Fatalf("applyFS() error = %v", err)
	}

	wantExec := []string{"select 2;"}
	if !reflect.DeepEqual(store.execSQL, wantExec) {
		t.Fatalf("execSQL = %#v, want %#v", store.execSQL, wantExec)
	}

	wantRecorded := []string{"0002_second.sql"}
	if !reflect.DeepEqual(store.recorded, wantRecorded) {
		t.Fatalf("recorded = %#v, want %#v", store.recorded, wantRecorded)
	}
}

func TestApplyFSRejectsChecksumMismatchForAppliedMigration(t *testing.T) {
	const migrationName = "0001_initial_schema.sql"
	fsys := fstest.MapFS{
		migrationName: {Data: []byte("select 1;")},
	}
	store := &fakeMigrationStore{
		applied: map[string]bool{migrationName: true},
		checksums: map[string]string{
			migrationName: strings.Repeat("0", 64),
		},
	}

	err := applyFS(context.Background(), store, fsys)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("applyFS() error = %v, want checksum mismatch", err)
	}
}

func TestApplyFSRejectsUnknownAppliedMigration(t *testing.T) {
	store := &fakeMigrationStore{
		applied: map[string]bool{"0000_unknown.sql": true},
		checksums: map[string]string{
			"0000_unknown.sql": strings.Repeat("0", 64),
		},
	}

	err := applyFS(context.Background(), store, fstest.MapFS{
		"0001_initial_schema.sql": {Data: []byte("select 1;")},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown applied migration") {
		t.Fatalf("applyFS() error = %v, want unknown applied migration", err)
	}
}

func TestApplyFSIncludesMigrationNameOnExecFailure(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql": {Data: []byte("select 2;")},
	}
	store := &fakeMigrationStore{
		execErr: map[string]error{"select 2;": errors.New("boom")},
	}

	err := applyFS(context.Background(), store, fsys)
	if err == nil {
		t.Fatal("applyFS() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "0002_second.sql") {
		t.Fatalf("error %q does not include migration name", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %q does not include underlying exec error", err)
	}
}

func TestEnsureMigrationLedgerInTxBackfillsAndLocksOnCallerTransaction(t *testing.T) {
	ctx := context.Background()
	checksum := strings.Repeat("a", 64)
	tx := &fakeMigrationLedgerTx{
		rows: &fakeMigrationLedgerRows{
			name:     "0001_initial_schema.sql",
			checksum: nil,
		},
	}
	sources := map[string]migrationSource{
		"0001_initial_schema.sql": {checksum: checksum, sql: "create table example();"},
	}

	if err := ensureMigrationLedgerInTx(ctx, tx, sources); err != nil {
		t.Fatalf("ensureMigrationLedgerInTx() error = %v", err)
	}
	if len(tx.execSQL) < 6 {
		t.Fatalf("caller transaction Exec calls = %#v, want ledger create, lock, backfill, and constraints", tx.execSQL)
	}
	joined := strings.Join(tx.execSQL, "\n")
	for _, want := range []string{
		"create table if not exists public.schema_migrations",
		"alter table public.schema_migrations add column if not exists checksum text",
		"lock table public.schema_migrations in share row exclusive mode",
		"update public.schema_migrations set checksum = $2 where name = $1",
		"alter table public.schema_migrations alter column checksum set not null",
		"conrelid = 'public.schema_migrations'::regclass",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("caller transaction SQL = %q, want %q", joined, want)
		}
	}
	if got := strings.Join(tx.querySQL, "\n"); !strings.Contains(got, `from public.schema_migrations order by name::text COLLATE "C"`) {
		t.Fatalf("caller transaction ledger query = %q, want public-qualified C-collated ledger read", got)
	}
	if tx.beginCalled {
		t.Fatal("ensureMigrationLedgerInTx() started a nested transaction")
	}
	if tx.updatedName != "0001_initial_schema.sql" || tx.updatedChecksum != checksum {
		t.Fatalf("ledger checksum backfill = (%q, %q), want (%q, %q)", tx.updatedName, tx.updatedChecksum, "0001_initial_schema.sql", checksum)
	}
}

func TestEnsureLegacyMigrationLedgerInTxKeepsAmbientLedgerSQLUnqualified(t *testing.T) {
	ctx := context.Background()
	tx := &fakeMigrationLedgerTx{
		rows: &fakeMigrationLedgerRows{
			name:     "0001_initial_schema.sql",
			checksum: nil,
		},
	}
	sources := map[string]migrationSource{
		"0001_initial_schema.sql": {checksum: strings.Repeat("a", 64), sql: "create table example();"},
	}

	if err := ensureLegacyMigrationLedgerInTx(ctx, tx, sources); err != nil {
		t.Fatalf("ensureLegacyMigrationLedgerInTx() error = %v", err)
	}
	joined := strings.Join(tx.execSQL, "\n")
	if strings.Contains(joined, "public.schema_migrations") {
		t.Fatalf("legacy caller transaction SQL = %q, must not public-qualify the ambient ledger", joined)
	}
	for _, want := range []string{
		"create table if not exists schema_migrations",
		"alter table schema_migrations add column if not exists checksum text",
		"lock table schema_migrations in share row exclusive mode",
		"update schema_migrations set checksum = $2 where name = $1",
		"alter table schema_migrations alter column checksum set not null",
		"conrelid = 'schema_migrations'::regclass",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("legacy caller transaction SQL = %q, want %q", joined, want)
		}
	}
	if got := strings.Join(tx.querySQL, "\n"); !strings.Contains(got, `from schema_migrations order by name::text COLLATE "C"`) || strings.Contains(got, "public.schema_migrations") {
		t.Fatalf("legacy caller transaction ledger query = %q, want unqualified C-collated ledger read", got)
	}
}

func TestAssetDecisionFollowupMigrationDropsRecordsViewBeforeChangingShape(t *testing.T) {
	sqlBytes, err := fs.ReadFile(migrations.FS, "0036_add_asset_decision_member_followups.sql")
	if err != nil {
		t.Fatalf("read 0036 migration: %v", err)
	}
	sql := strings.ToLower(string(sqlBytes))
	dropIndex := strings.Index(sql, "drop view if exists asset_decision_records_with_counts")
	createIndex := strings.Index(sql, "create or replace view asset_decision_records_with_counts")
	if dropIndex < 0 {
		t.Fatal("0036 migration must drop asset_decision_records_with_counts before changing its column shape")
	}
	if createIndex < 0 {
		t.Fatal("0036 migration must recreate asset_decision_records_with_counts")
	}
	if dropIndex > createIndex {
		t.Fatal("0036 migration drops asset_decision_records_with_counts after recreating it; drop must come first")
	}
	if !strings.Contains(sql[createIndex:], "followup_todo_count") || !strings.Contains(sql[createIndex:], "evidence_snapshot") {
		t.Fatal("0036 recreated view must include followup counts and preserve evidence_snapshot")
	}
}

type fakeMigrationStore struct {
	ensureCalls int
	applied     map[string]bool
	checksums   map[string]string
	execSQL     []string
	recorded    []string
	execErr     map[string]error
}

type fakeMigrationLedgerTx struct {
	pgx.Tx
	rows            pgx.Rows
	execSQL         []string
	querySQL        []string
	beginCalled     bool
	updatedName     string
	updatedChecksum string
}

func (tx *fakeMigrationLedgerTx) Begin(context.Context) (pgx.Tx, error) {
	tx.beginCalled = true
	return tx, nil
}

func (tx *fakeMigrationLedgerTx) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tx.execSQL = append(tx.execSQL, sql)
	if strings.Contains(sql, "update schema_migrations set checksum") || strings.Contains(sql, "update public.schema_migrations set checksum") {
		if len(arguments) != 2 {
			return pgconn.CommandTag{}, errors.New("unexpected checksum backfill arguments")
		}
		name, ok := arguments[0].(string)
		if !ok {
			return pgconn.CommandTag{}, errors.New("checksum backfill name is not a string")
		}
		checksum, ok := arguments[1].(string)
		if !ok {
			return pgconn.CommandTag{}, errors.New("checksum backfill checksum is not a string")
		}
		tx.updatedName = name
		tx.updatedChecksum = checksum
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeMigrationLedgerTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	tx.querySQL = append(tx.querySQL, sql)
	return tx.rows, nil
}

type fakeMigrationLedgerRows struct {
	name     string
	checksum *string
	returned bool
}

func (rows *fakeMigrationLedgerRows) Close()                                       {}
func (rows *fakeMigrationLedgerRows) Err() error                                   { return nil }
func (rows *fakeMigrationLedgerRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *fakeMigrationLedgerRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *fakeMigrationLedgerRows) Values() ([]any, error)                       { return nil, nil }
func (rows *fakeMigrationLedgerRows) RawValues() [][]byte                          { return nil }
func (rows *fakeMigrationLedgerRows) Conn() *pgx.Conn                              { return nil }
func (rows *fakeMigrationLedgerRows) Next() bool {
	if rows.returned {
		return false
	}
	rows.returned = true
	return true
}
func (rows *fakeMigrationLedgerRows) Scan(dest ...any) error {
	if len(dest) != 2 {
		return errors.New("unexpected ledger row scan destination count")
	}
	name, ok := dest[0].(*string)
	if !ok {
		return errors.New("ledger row name destination is not *string")
	}
	checksum, ok := dest[1].(**string)
	if !ok {
		return errors.New("ledger row checksum destination is not **string")
	}
	*name = rows.name
	*checksum = rows.checksum
	return nil
}

func (f *fakeMigrationStore) EnsureLedger(_ context.Context, sources map[string]migrationSource) error {
	f.ensureCalls++
	if f.applied == nil {
		f.applied = make(map[string]bool)
	}
	if f.checksums == nil {
		f.checksums = make(map[string]string)
	}
	for name, applied := range f.applied {
		if !applied {
			continue
		}
		if _, alreadyPinned := f.checksums[name]; alreadyPinned {
			continue
		}
		if source, ok := sources[name]; ok {
			f.checksums[name] = source.checksum
		}
	}
	return nil
}

func (f *fakeMigrationStore) Applied(context.Context) (map[string]string, error) {
	applied := make(map[string]string, len(f.applied))
	for name, isApplied := range f.applied {
		if isApplied {
			applied[name] = f.checksums[name]
		}
	}
	return applied, nil
}

func (f *fakeMigrationStore) Apply(_ context.Context, name, checksum, sql string) error {
	f.execSQL = append(f.execSQL, sql)
	if err := f.execErr[sql]; err != nil {
		return err
	}
	f.applied[name] = true
	f.checksums[name] = checksum
	f.recorded = append(f.recorded, name)
	return nil
}
