package migrate

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"houfeng/db/migrations"
)

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

	wantChecks := []string{"0001_initial_schema.sql", "0002_second.sql"}
	if !reflect.DeepEqual(store.appliedChecks, wantChecks) {
		t.Fatalf("appliedChecks = %#v, want %#v", store.appliedChecks, wantChecks)
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
	ensureCalls   int
	applied       map[string]bool
	appliedChecks []string
	execSQL       []string
	recorded      []string
	execErr       map[string]error
}

func (f *fakeMigrationStore) EnsureLedger(context.Context) error {
	f.ensureCalls++
	return nil
}

func (f *fakeMigrationStore) HasMigration(_ context.Context, name string) (bool, error) {
	f.appliedChecks = append(f.appliedChecks, name)
	return f.applied[name], nil
}

func (f *fakeMigrationStore) ExecMigration(_ context.Context, sql string) error {
	f.execSQL = append(f.execSQL, sql)
	if err := f.execErr[sql]; err != nil {
		return err
	}
	return nil
}

func (f *fakeMigrationStore) RecordMigration(_ context.Context, name string) error {
	f.recorded = append(f.recorded, name)
	return nil
}
