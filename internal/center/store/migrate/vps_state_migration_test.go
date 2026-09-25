package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/db/migrations"
)

func TestVPSStateRepair0065BackfillsArchivedSnapshotsAndExpandsLifecycleAudit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := openTemporaryPostgresDatabase(t, ctx)
	applyPostgresMigrationsThrough(t, ctx, db, "0064_add_network_rates_valid.sql")

	if _, err := db.Exec(ctx, `
		insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status, renewal_decision, archived_at)
		values
			('vps_state_repair_archived_in_use', 'Legacy archived VPS in use', 'archived', 'in_use', 'keep', '2025-01-02T03:04:05Z'),
			('vps_state_repair_archived_standby', 'Legacy archived VPS standby', 'archived', 'standby', 'observe', '2025-02-03T04:05:06Z'),
			('vps_state_repair_active_in_use', 'Active VPS in use', 'active', 'in_use', 'keep', null)
	`); err != nil {
		t.Fatalf("seed pre-0065 VPS states: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months, monthly_price,
			renew_at, auto_renew, status, billing_period_unit, billing_period_length, renewal_mode
		) values (
			'sub_state_repair_validity_history', 'vps_state_repair_active_in_use', 120, 'USD', 'annual', 12, 10,
			'2025-12-01', true, 'active', 'year', 1, 'auto'
		)
	`); err != nil {
		t.Fatalf("seed pre-0065 active subscription: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into asset_lifecycle_actions (action_id, vps_id, action_type, status, reason, effective_date, summary)
		values (
			'action_legacy_extend_validity',
			'vps_state_repair_active_in_use',
			'extend_validity',
			'completed',
			'legacy validity extension',
			'2025-12-01',
			jsonb_build_object('subscription_id', 'sub_state_repair_validity_history', 'old_renew_at', '2025-11-01', 'new_renew_at', '2025-12-01')
		)
	`); err != nil {
		t.Fatalf("seed released validity-extension action before 0065: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into asset_lifecycle_action_steps (
			step_id, action_id, object_type, object_id, step_type, status, before_state, after_state, message
		) values (
			'step_legacy_subscription_renewal',
			'action_legacy_extend_validity',
			'subscription',
			'sub_state_repair_validity_history',
			'subscription_renew_at',
			'completed',
			'{"renew_at":"2025-11-01"}',
			'{"renew_at":"2025-12-01"}',
			'legacy subscription validity extended'
		)
	`); err != nil {
		t.Fatalf("seed released subscription-renewal step before 0065: %v", err)
	}

	preflightSQL, err := osReadVPSStatePreflight()
	if err != nil {
		t.Fatalf("read VPS state preflight SQL: %v", err)
	}
	if _, err := db.Exec(ctx, preflightSQL); err != nil {
		t.Fatalf("run read-only VPS state preflight against the pre-migration schema: %v", err)
	}
	var observedUsage string
	if err := db.QueryRow(ctx, `select usage_status from vps_assets where vps_id = 'vps_state_repair_archived_in_use'`).Scan(&observedUsage); err != nil {
		t.Fatalf("read preflighted archived usage: %v", err)
	}
	if observedUsage != "in_use" {
		t.Fatalf("read-only preflight changed archived usage to %q", observedUsage)
	}

	applyPostgresMigrationsThrough(t, ctx, db, "0065_extend_vps_lifecycle_audit_and_snapshot.sql")
	var preservedActionType, preservedStepType string
	if err := db.QueryRow(ctx, `
		select a.action_type, s.step_type
		from asset_lifecycle_actions a
		join asset_lifecycle_action_steps s on s.action_id = a.action_id
		where a.action_id = 'action_legacy_extend_validity' and s.step_id = 'step_legacy_subscription_renewal'
	`).Scan(&preservedActionType, &preservedStepType); err != nil {
		t.Fatalf("read preserved pre-0065 validity-extension history: %v", err)
	}
	if preservedActionType != "extend_validity" || preservedStepType != "subscription_renew_at" {
		t.Fatalf("pre-0065 validity-extension history after migration = action:%q step:%q", preservedActionType, preservedStepType)
	}

	type snapshot struct {
		LifecycleStatus string `json:"lifecycle_status"`
		UsageStatus     string `json:"usage_status"`
		RenewalDecision string `json:"renewal_decision"`
		CapturedAt      string `json:"captured_at"`
		Source          string `json:"source"`
	}
	wantSnapshots := map[string]snapshot{
		"vps_state_repair_archived_in_use": {
			LifecycleStatus: "archived",
			UsageStatus:     "in_use",
			RenewalDecision: "keep",
			Source:          "migration_observation",
		},
		"vps_state_repair_archived_standby": {
			LifecycleStatus: "archived",
			UsageStatus:     "standby",
			RenewalDecision: "observe",
			Source:          "migration_observation",
		},
	}
	rows, err := db.Query(ctx, `select vps_id, usage_status, archived_state_snapshot from vps_assets where vps_id like 'vps_state_repair_%' order by vps_id`)
	if err != nil {
		t.Fatalf("query migrated VPS state snapshots: %v", err)
	}
	seen := make(map[string]bool, len(wantSnapshots))
	for rows.Next() {
		var id, currentUsage string
		var raw []byte
		if err := rows.Scan(&id, &currentUsage, &raw); err != nil {
			rows.Close()
			t.Fatalf("scan migrated VPS state snapshot: %v", err)
		}
		want, isArchived := wantSnapshots[id]
		if !isArchived {
			if id != "vps_state_repair_active_in_use" || currentUsage != "in_use" || raw != nil {
				rows.Close()
				t.Fatalf("non-archived state after migration = (%q, %q, %s), want active/in_use/no snapshot", id, currentUsage, raw)
			}
			continue
		}
		var got snapshot
		if err := json.Unmarshal(raw, &got); err != nil {
			rows.Close()
			t.Fatalf("decode archived state snapshot for %s: %v", id, err)
		}
		if got.LifecycleStatus != want.LifecycleStatus || got.UsageStatus != want.UsageStatus || got.RenewalDecision != want.RenewalDecision || got.Source != want.Source || got.CapturedAt == "" {
			rows.Close()
			t.Fatalf("archived snapshot for %s = %#v, want original axes, migration source, and capture time", id, got)
		}
		if _, err := time.Parse(time.RFC3339Nano, got.CapturedAt); err != nil {
			rows.Close()
			t.Fatalf("archived snapshot capture time for %s = %q: %v", id, got.CapturedAt, err)
		}
		if currentUsage != "unknown" {
			rows.Close()
			t.Fatalf("current usage for archived VPS %s = %q, want unknown", id, currentUsage)
		}
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate migrated VPS state snapshots: %v", err)
	}
	rows.Close()
	for id := range wantSnapshots {
		if !seen[id] {
			t.Errorf("missing migrated archived VPS snapshot for %s", id)
		}
	}

	_, err = db.Exec(ctx, `update vps_assets set usage_status = 'in_use' where vps_id = 'vps_state_repair_archived_in_use'`)
	var constraintError *pgconn.PgError
	if !errors.As(err, &constraintError) || constraintError.Code != "23514" || constraintError.ConstraintName != "vps_assets_state_combination_valid" {
		t.Fatalf("setting archived VPS usage to in_use error = %v, want the archived-state check violation", err)
	}

	var correctionActionID string
	for index, actionType := range []string{"cancel_vps", "archive_vps", "restore_vps", "start_migration", "correct_dependency_status"} {
		actionID := fmt.Sprintf("action_state_repair_%02d", index)
		if _, err := db.Exec(ctx, `insert into asset_lifecycle_actions (action_id, vps_id, action_type, reason) values ($1, 'vps_state_repair_active_in_use', $2, 'migration contract test')`, actionID, actionType); err != nil {
			t.Fatalf("insert lifecycle action %q: %v", actionType, err)
		}
		if actionType == "correct_dependency_status" {
			correctionActionID = actionID
		}
	}
	for _, step := range []struct {
		id         string
		objectType string
		objectID   string
	}{
		{id: "step_state_repair_service", objectType: "service", objectID: "service_state_repair"},
		{id: "step_state_repair_domain", objectType: "domain", objectID: "domain_state_repair"},
	} {
		if _, err := db.Exec(ctx, `insert into asset_lifecycle_action_steps (step_id, action_id, object_type, object_id, step_type, status) values ($1, $2, $3, $4, 'dependency_status', 'completed')`, step.id, correctionActionID, step.objectType, step.objectID); err != nil {
			t.Fatalf("insert %s dependency-status audit step: %v", step.objectType, err)
		}
	}
	var actionCount, stepCount int
	if err := db.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where action_id like 'action_state_repair_%'`).Scan(&actionCount); err != nil {
		t.Fatalf("count accepted lifecycle actions: %v", err)
	}
	if err := db.QueryRow(ctx, `select count(*) from asset_lifecycle_action_steps where step_id like 'step_state_repair_%' and step_type = 'dependency_status' and object_type in ('service', 'domain')`).Scan(&stepCount); err != nil {
		t.Fatalf("count accepted dependency-status steps: %v", err)
	}
	if actionCount != 5 || stepCount != 2 {
		t.Fatalf("accepted lifecycle audit rows = %d actions, %d steps; want 5 actions and 2 steps", actionCount, stepCount)
	}
}

func TestVPSStateRepair0066RejectsUnknownEnumsWithoutGuessingAndAcceptsDomainEdges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db := openTemporaryPostgresDatabase(t, ctx)
	applyPostgresMigrationsThrough(t, ctx, db, "0065_extend_vps_lifecycle_audit_and_snapshot.sql")

	if _, err := db.Exec(ctx, `
		insert into monitoring_instances (monitoring_instance_id, display_name, region, city, provider, lifecycle_status, monitoring_status, binding_status, binding_fingerprint)
		values
			('mi_state_repair_pending', 'Pending instance', 'Tokyo', 'Tokyo', 'Test provider', '待接入', '启用', '未绑定', null),
			('mi_state_repair_in_use', 'In-use instance', 'Tokyo', 'Tokyo', 'Test provider', '在用', '启用', '已绑定', 'fingerprint-in-use'),
			('mi_state_repair_observing', 'Observing instance', 'Tokyo', 'Tokyo', 'Test provider', '观察中', '维护中', '指纹变更待确认', 'fingerprint-observing'),
			('mi_state_repair_no_renewal', 'No-renewal instance', 'Tokyo', 'Tokyo', 'Test provider', '不续费', '暂停', '未绑定', null),
			('mi_state_repair_retired_enabled', 'Retired enabled instance', 'Tokyo', 'Tokyo', 'Test provider', '已退役', '启用', '已绑定', 'fingerprint-retired')
	`); err != nil {
		t.Fatalf("seed valid MonitoringInstance domain values: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into targets (target_id, name, target_type, host, run_status)
		values
			('target_state_repair_enabled', 'Enabled', 'service', 'enabled.example', '启用'),
			('target_state_repair_maintenance', 'Maintenance', 'service', 'maintenance.example', '维护中'),
			('target_state_repair_paused', 'Paused', 'service', 'paused.example', '暂停'),
			('target_state_repair_archived', 'Archived', 'service', 'archived.example', '已归档')
	`); err != nil {
		t.Fatalf("seed valid Target domain values: %v", err)
	}

	type invalidState struct {
		table     string
		idColumn  string
		column    string
		rowID     string
		badValue  string
		goodValue string
	}
	invalidStates := []invalidState{
		{table: "monitoring_instances", idColumn: "monitoring_instance_id", column: "lifecycle_status", rowID: "mi_state_repair_no_renewal", badValue: "legacy-lifecycle", goodValue: "不续费"},
		{table: "monitoring_instances", idColumn: "monitoring_instance_id", column: "monitoring_status", rowID: "mi_state_repair_retired_enabled", badValue: "legacy-monitoring", goodValue: "启用"},
		{table: "monitoring_instances", idColumn: "monitoring_instance_id", column: "binding_status", rowID: "mi_state_repair_pending", badValue: "legacy-binding", goodValue: "未绑定"},
		{table: "targets", idColumn: "target_id", column: "run_status", rowID: "target_state_repair_archived", badValue: "legacy-run-status", goodValue: "已归档"},
	}
	migrationFS := vpsStateRepairMigrationFSThrough(t, "0066_constrain_monitoring_and_target_state_values.sql")
	for _, state := range invalidStates {
		updateSQL := fmt.Sprintf("update %s set %s = $1 where %s = $2", state.table, state.column, state.idColumn)
		if _, err := db.Exec(ctx, updateSQL, state.badValue, state.rowID); err != nil {
			t.Fatalf("seed unknown %s.%s value: %v", state.table, state.column, err)
		}
		if err := applyFS(ctx, poolStore{db: db}, migrationFS); err == nil {
			t.Fatalf("apply 0066 with unknown %s.%s value succeeded", state.table, state.column)
		} else {
			for _, detail := range []string{state.table + "." + state.column, "id=" + state.rowID, "value=" + state.badValue} {
				if !strings.Contains(err.Error(), detail) {
					t.Fatalf("apply 0066 error = %v, want diagnostic %q", err, detail)
				}
			}
		}
		var appliedCount, constraintCount int
		if err := db.QueryRow(ctx, `select count(*) from schema_migrations where name = '0066_constrain_monitoring_and_target_state_values.sql'`).Scan(&appliedCount); err != nil {
			t.Fatalf("read failed migration ledger state: %v", err)
		}
		if err := db.QueryRow(ctx, `
			select count(*)
			from pg_constraint
			where conname in (
				'monitoring_instances_lifecycle_status_allowed',
				'monitoring_instances_monitoring_status_allowed',
				'monitoring_instances_binding_status_allowed',
				'targets_run_status_allowed'
			)
		`).Scan(&constraintCount); err != nil {
			t.Fatalf("read failed migration constraints: %v", err)
		}
		if appliedCount != 0 || constraintCount != 0 {
			t.Fatalf("failed 0066 state = %d ledger rows, %d enum constraints; want transaction rollback", appliedCount, constraintCount)
		}
		var unchangedValue string
		if err := db.QueryRow(ctx, "select "+state.column+" from "+state.table+" where "+state.idColumn+" = $1", state.rowID).Scan(&unchangedValue); err != nil {
			t.Fatalf("read unknown value after failed migration: %v", err)
		}
		if unchangedValue != state.badValue {
			t.Fatalf("failed migration changed unknown %s.%s from %q to %q", state.table, state.column, state.badValue, unchangedValue)
		}
		if _, err := db.Exec(ctx, updateSQL, state.goodValue, state.rowID); err != nil {
			t.Fatalf("restore verified %s.%s value: %v", state.table, state.column, err)
		}
	}

	if err := applyFS(ctx, poolStore{db: db}, migrationFS); err != nil {
		t.Fatalf("apply 0066 after explicit enum corrections: %v", err)
	}
	var constraintCount int
	if err := db.QueryRow(ctx, `
		select count(*)
		from pg_constraint
		where conname in (
			'monitoring_instances_lifecycle_status_allowed',
			'monitoring_instances_monitoring_status_allowed',
			'monitoring_instances_binding_status_allowed',
			'targets_run_status_allowed'
		)
	`).Scan(&constraintCount); err != nil {
		t.Fatalf("count installed enum constraints: %v", err)
	}
	if constraintCount != 4 {
		t.Fatalf("installed enum constraints = %d, want 4", constraintCount)
	}

	var lifecycleStatus, monitoringStatus, bindingStatus string
	if err := db.QueryRow(ctx, `
		select lifecycle_status, monitoring_status, binding_status
		from monitoring_instances
		where monitoring_instance_id = 'mi_state_repair_retired_enabled'
	`).Scan(&lifecycleStatus, &monitoringStatus, &bindingStatus); err != nil {
		t.Fatalf("read legal but repairable retired-instance edge: %v", err)
	}
	if lifecycleStatus != "已退役" || monitoringStatus != "启用" || bindingStatus != "已绑定" {
		t.Fatalf("0066 rewrote or rejected legal enum edge = (%q, %q, %q)", lifecycleStatus, monitoringStatus, bindingStatus)
	}

	for _, state := range []struct {
		table    string
		idColumn string
		column   string
		rowID    string
	}{
		{table: "monitoring_instances", idColumn: "monitoring_instance_id", column: "lifecycle_status", rowID: "mi_state_repair_no_renewal"},
		{table: "monitoring_instances", idColumn: "monitoring_instance_id", column: "monitoring_status", rowID: "mi_state_repair_retired_enabled"},
		{table: "monitoring_instances", idColumn: "monitoring_instance_id", column: "binding_status", rowID: "mi_state_repair_pending"},
		{table: "targets", idColumn: "target_id", column: "run_status", rowID: "target_state_repair_archived"},
	} {
		updateSQL := fmt.Sprintf("update %s set %s = 'invalid-after-0066' where %s = $1", state.table, state.column, state.idColumn)
		if _, err := db.Exec(ctx, updateSQL, state.rowID); err == nil {
			t.Errorf("0066 accepted an invalid %s.%s value", state.table, state.column)
		}
	}
}

func vpsStateRepairMigrationFSThrough(t *testing.T, throughName string) fstest.MapFS {
	t.Helper()
	names, err := Names()
	if err != nil {
		t.Fatalf("list migrations through %s: %v", throughName, err)
	}
	result := make(fstest.MapFS)
	for _, name := range names {
		payload, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		result[name] = &fstest.MapFile{Data: append([]byte(nil), payload...)}
		if name == throughName {
			return result
		}
	}
	t.Fatalf("migration %q not found", throughName)
	return nil
}

func osReadVPSStatePreflight() (string, error) {
	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "scripts", "preflight-vps-state.sql"))
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
