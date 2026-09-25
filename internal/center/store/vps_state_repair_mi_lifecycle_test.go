package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
)

func TestVPSStateRepairMIUnlinkedCreateRejectsRetiredLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	input := monitoringinstances.CreateInput{
		DisplayName:     "Retired unlinked MI",
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleRetired,
		Labels:          []string{},
	}
	if _, err := repo.CreateMonitoringInstance(ctx, input); !errors.Is(err, monitoringinstances.ErrInvalidCreateInput) {
		t.Fatalf("unlinked retired create error = %v, want invalid create input", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from monitoring_instances where display_name = $1`, input.DisplayName).Scan(&count); err != nil {
		t.Fatalf("count monitoring instances: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected retired create persisted %d monitoring instances", count)
	}
	input.LifecycleStatus = monitoringinstances.LifecyclePendingEnrollment
	if _, err := repo.CreateMonitoringInstance(ctx, input); err != nil {
		t.Fatalf("unlinked pending-enrollment create: %v", err)
	}
}

func TestVPSStateRepairMIRetirementReconcilesAndRestores(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)

	record, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "MI lifecycle repair",
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set binding_status = '指纹变更待确认',
			binding_fingerprint = 'fingerprint-current',
			binding_epoch_started_at = now(),
			enrollment_token_hash = $2,
			enrollment_token_issued_at = now(),
			enrollment_token_consumed_at = now(),
			sync_token_hash = $3,
			pending_binding_fingerprint = 'fingerprint-pending',
			pending_binding_first_seen_at = now(),
			pending_binding_last_seen_at = now(),
			pending_binding_attempt_count = 2,
			pending_action_id = 'act_retirement_repair',
			pending_action_command_id = 'cmd_retirement_repair',
			last_action = '{"action_id":"act_retirement_repair","command_id":"cmd_retirement_repair","status":"pending"}'::jsonb
		where monitoring_instance_id = $1`, record.MonitoringInstanceID, hashEnrollmentToken("retirement-repair-token"), hashSyncToken("retirement-repair-sync-token")); err != nil {
		t.Fatalf("seed retirement state: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, exit_code, occurred_at
		) values ($1, $2, $3, 'uptime', 'standard', 'completed', 'agent_sync', 0, now())`,
		"cmd_aud_retirement_"+record.MonitoringInstanceID,
		"act_retirement_"+record.MonitoringInstanceID,
		record.MonitoringInstanceID); err != nil {
		t.Fatalf("seed completed command audit: %v", err)
	}

	retired, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "retirement repair"})
	if err != nil {
		t.Fatalf("RetireMonitoringInstance: %v", err)
	}
	if retired.LifecycleStatus != monitoringinstances.LifecycleRetired ||
		retired.MonitoringStatus != monitoringinstances.MonitoringPaused ||
		retired.BindingStatus != monitoringinstances.BindingBound ||
		retired.EnrollmentTokenHash != "" || retired.EnrollmentTokenIssuedAt != nil || retired.EnrollmentTokenConsumedAt != nil ||
		retired.SyncTokenHash != "" || retired.PendingBindingFingerprint != "" || retired.PendingBindingFirstSeenAt != nil ||
		retired.PendingBindingLastSeenAt != nil || retired.PendingBindingAttemptCount != 0 ||
		retired.PendingActionID != "" || retired.PendingActionCommandID != "" || retired.LastAction != nil {
		t.Fatalf("retired record did not satisfy lifecycle invariants: %#v", retired)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 1)
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetirementReconciled, 0)
	assertVPSStateRepairMICommandAuditCount(t, ctx, pool, record.MonitoringInstanceID, 1)

	if _, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "clean retry"}); err != nil {
		t.Fatalf("clean same-state retirement: %v", err)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 1)
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetirementReconciled, 0)

	for name, call := range map[string]func() error{
		"maintenance": func() error {
			_, err := repo.SetMonitoringInstanceMonitoringMaintenance(ctx, record.MonitoringInstanceID)
			return err
		},
		"resume": func() error {
			_, err := repo.ResumeMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID)
			return err
		},
		"enrollment token": func() error {
			_, err := repo.IssueMonitoringInstanceEnrollmentToken(ctx, record.MonitoringInstanceID)
			return err
		},
		"sync token": func() error {
			_, err := repo.IssueSyncToken(ctx, record.MonitoringInstanceID)
			return err
		},
		"confirm binding": func() error {
			_, err := repo.ConfirmMonitoringInstanceRebind(ctx, record.MonitoringInstanceID)
			return err
		},
		"reject binding": func() error { _, err := repo.RejectPendingFingerprint(ctx, record.MonitoringInstanceID); return err },
		"reset binding": func() error {
			_, err := repo.ResetMonitoringInstanceBinding(ctx, record.MonitoringInstanceID)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, monitoringinstances.ErrRetiredMonitoringInstance) {
				t.Fatalf("retired MI action error = %v, want ErrRetiredMonitoringInstance", err)
			}
		})
	}

	restored, err := repo.RestoreMonitoringInstanceLifecycle(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "controlled restore"})
	if err != nil {
		t.Fatalf("RestoreMonitoringInstanceLifecycle: %v", err)
	}
	if restored.LifecycleStatus != monitoringinstances.LifecycleObserving || restored.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("restored state = (%q, %q), want observing and paused", restored.LifecycleStatus, restored.MonitoringStatus)
	}
	if _, err := repo.IssueMonitoringInstanceEnrollmentToken(ctx, record.MonitoringInstanceID); err != nil {
		t.Fatalf("issue enrollment token after controlled restore: %v", err)
	}
	if _, err := repo.IssueSyncToken(ctx, record.MonitoringInstanceID); err != nil {
		t.Fatalf("issue sync token after controlled restore: %v", err)
	}
	pausedAfterCredentialIssue, err := repo.GetMonitoringInstance(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("GetMonitoringInstance after credential issue: %v", err)
	}
	if pausedAfterCredentialIssue.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("monitoring after credential issue = %q, want paused until explicit resume", pausedAfterCredentialIssue.MonitoringStatus)
	}
	resumed, err := repo.ResumeMonitoringInstanceMonitoring(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("explicit resume after restore: %v", err)
	}
	if resumed.MonitoringStatus != monitoringinstances.MonitoringEnabled {
		t.Fatalf("explicit resume status = %q, want enabled", resumed.MonitoringStatus)
	}

	if _, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "retire after restore"}); err != nil {
		t.Fatalf("retire after controlled restore: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set monitoring_status = '启用',
			binding_status = '指纹变更待确认',
			enrollment_token_hash = $2,
			enrollment_token_issued_at = now(),
			enrollment_token_consumed_at = now(),
			sync_token_hash = $3,
			pending_binding_fingerprint = 'fingerprint-pending-again',
			pending_binding_first_seen_at = now(),
			pending_binding_last_seen_at = now(),
			pending_binding_attempt_count = 3,
			pending_action_id = 'act_pending_again',
			pending_action_command_id = 'cmd_pending_again',
			last_action = '{"action_id":"act_pending_again","command_id":"cmd_pending_again","status":"pending"}'::jsonb
		where monitoring_instance_id = $1`, record.MonitoringInstanceID, hashEnrollmentToken("retirement-repair-token-2"), hashSyncToken("retirement-repair-sync-token-2")); err != nil {
		t.Fatalf("seed dirty already-retired state: %v", err)
	}
	cleaned, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "reconcile retired state"})
	if err != nil {
		t.Fatalf("reconcile already-retired MI: %v", err)
	}
	if cleaned.LifecycleStatus != monitoringinstances.LifecycleRetired || cleaned.MonitoringStatus != monitoringinstances.MonitoringPaused || cleaned.BindingStatus != monitoringinstances.BindingBound || cleaned.EnrollmentTokenHash != "" || cleaned.SyncTokenHash != "" || cleaned.PendingActionID != "" || cleaned.LastAction != nil {
		t.Fatalf("already-retired state was not reconciled: %#v", cleaned)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 2)
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRetirementReconciled, 1)
	var reconciliationPayload map[string]any
	if err := pool.QueryRow(ctx, `
		select payload from state_change_events
		where object_type = 'monitoring_instance' and object_id = $1 and event_type = $2`,
		record.MonitoringInstanceID, string(incidents.EventMonitoringInstanceRetirementReconciled)).Scan(&reconciliationPayload); err != nil {
		t.Fatalf("read retirement reconciliation event: %v", err)
	}
	if reconciliationPayload["prior_state"] != monitoringinstances.LifecycleRetired ||
		reconciliationPayload["resulting_state"] != monitoringinstances.LifecycleRetired ||
		reconciliationPayload["retirement_monitoring_status_reconciled"] != true ||
		reconciliationPayload["retirement_binding_status_reconciled"] != true ||
		reconciliationPayload["retirement_enrollment_credentials_revoked"] != true ||
		reconciliationPayload["retirement_sync_credential_revoked"] != true ||
		reconciliationPayload["retirement_pending_binding_cleared"] != true ||
		reconciliationPayload["retirement_pending_action_cleared"] != true {
		t.Fatalf("retirement reconciliation event = %#v, want safe state-change booleans", reconciliationPayload)
	}
	encodedPayload, err := json.Marshal(reconciliationPayload)
	if err != nil {
		t.Fatalf("marshal reconciliation payload: %v", err)
	}
	if strings.Contains(string(encodedPayload), "retirement-repair-token") || strings.Contains(string(encodedPayload), "fingerprint-pending") {
		t.Fatalf("retirement event disclosed credentials or pending fingerprint: %s", encodedPayload)
	}
	assertVPSStateRepairMICommandAuditCount(t, ctx, pool, record.MonitoringInstanceID, 1)

	archived, err := repo.ArchiveMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.ArchiveInput{
		Reason:           "retired instance archive",
		ConfirmationName: record.DisplayName,
	})
	if err != nil {
		t.Fatalf("ArchiveMonitoringInstance: %v", err)
	}
	if archived.ArchivedAt == nil || archived.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("archived state = %#v, want archived and paused", archived)
	}
	if _, err := repo.RetireMonitoringInstance(ctx, record.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "must not modify archive"}); !errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
		t.Fatalf("retire archived MI = %v, want ErrArchivedMonitoringInstance", err)
	}
	if _, err := repo.RetireMonitoringInstance(ctx, "mi_repair_missing", monitoringinstances.LifecycleActionInput{Reason: "missing"}); !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("retire missing MI = %v, want ErrMonitoringInstanceNotFound", err)
	}

	fromArchive, err := repo.RestoreMonitoringInstanceFromArchive(ctx, record.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("RestoreMonitoringInstanceFromArchive: %v", err)
	}
	if fromArchive.ArchivedAt != nil || fromArchive.LifecycleStatus != monitoringinstances.LifecycleObserving || fromArchive.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("archive restore state = %#v, want observing and paused", fromArchive)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, record.MonitoringInstanceID, incidents.EventMonitoringInstanceRestoredFromArchive, 1)
	var restorePrior, restoreResult, restoreLifecycle string
	if err := pool.QueryRow(ctx, `
		select payload->>'prior_state', payload->>'resulting_state', payload->>'lifecycle_status'
		from state_change_events
		where object_type = 'monitoring_instance' and object_id = $1 and event_type = $2`,
		record.MonitoringInstanceID, string(incidents.EventMonitoringInstanceRestoredFromArchive)).Scan(&restorePrior, &restoreResult, &restoreLifecycle); err != nil {
		t.Fatalf("read archive restore event: %v", err)
	}
	if restorePrior != "archived" || restoreResult != "unarchived" || restoreLifecycle != monitoringinstances.LifecycleObserving {
		t.Fatalf("archive restore event = (%q, %q, %q), want archived -> unarchived with observing lifecycle", restorePrior, restoreResult, restoreLifecycle)
	}
	assertVPSStateRepairMICommandAuditCount(t, ctx, pool, record.MonitoringInstanceID, 1)
}

func TestVPSStateRepairMILinkedCreateRejectsRetiredLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresMonitoringInstanceRepository(pool)
	const vpsID = "vps_mi_retired_create"
	if _, err := pool.Exec(ctx, `
		insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status)
		values ($1, 'Retired MI create guard', 'active', 'idle')`, vpsID); err != nil {
		t.Fatalf("insert parent VPS: %v", err)
	}

	_, _, err := repo.CreateLinkedMonitoringInstance(ctx, vpsID, monitoringinstances.CreateInput{
		DisplayName:     "Retired linked MI",
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleRetired,
		Labels:          []string{},
	}, "retired link")
	if !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
		t.Fatalf("CreateLinkedMonitoringInstance with retired lifecycle = %v, want link conflict", err)
	}
	var instanceCount, linkCount int
	if err := pool.QueryRow(ctx, `select count(*) from monitoring_instances where display_name = 'Retired linked MI'`).Scan(&instanceCount); err != nil {
		t.Fatalf("count retired MI rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from vps_monitoring_instance_links where vps_id = $1 and unlinked_at is null`, vpsID).Scan(&linkCount); err != nil {
		t.Fatalf("count new MI links: %v", err)
	}
	if instanceCount != 0 || linkCount != 0 {
		t.Fatalf("rejected linked create left %d MI rows and %d current links", instanceCount, linkCount)
	}
}

func assertVPSStateRepairMIEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoringInstanceID string, eventType incidents.EventType, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
		select count(*)::int from state_change_events
		where object_type = 'monitoring_instance' and object_id = $1 and event_type = $2`,
		monitoringInstanceID, string(eventType)).Scan(&got); err != nil {
		t.Fatalf("count MI event %q: %v", eventType, err)
	}
	if got != want {
		t.Fatalf("MI event count for %q = %d, want %d", eventType, got, want)
	}
}

func assertVPSStateRepairMICommandAuditCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoringInstanceID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
		select count(*)::int from monitoring_instance_command_action_audit
		where monitoring_instance_id = $1`, monitoringInstanceID).Scan(&got); err != nil {
		t.Fatalf("count MI command audit: %v", err)
	}
	if got != want {
		t.Fatalf("MI command audit count = %d, want %d", got, want)
	}
}
