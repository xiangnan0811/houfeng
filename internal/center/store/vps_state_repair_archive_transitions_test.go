package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairArchiveSnapshotSurvivesRestoreWithoutRestoringRelationsOrTokens(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepository := NewPostgresVPSAssetRepository(pool)
	lifecycleRepository := NewPostgresAssetLifecycleRepository(pool)

	vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Archive snapshot and restore", vpsassets.LifecycleActive, vpsassets.UsageInUse, vpsassets.RenewalKeep)
	miRepository := NewPostgresMonitoringInstanceRepository(pool)
	mi, err := miRepository.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "Archived relationship remains archived",
		Region:          "Tokyo",
		City:            "Tokyo",
		Provider:        "State repair test",
		LifecycleStatus: monitoringinstances.LifecycleObserving,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("create monitoring instance: %v", err)
	}
	link, err := NewPostgresVPSMonitoringInstanceLinkRepository(pool).LinkMonitoringInstance(ctx, vps.VPSID, assetlinks.LinkInput{
		MonitoringInstanceID: mi.MonitoringInstanceID,
		Note:                 "keep this historical relation intact",
	})
	if err != nil {
		t.Fatalf("link monitoring instance: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set lifecycle_status = $2,
		    monitoring_status = $3,
		    archived_reason = '',
		    enrollment_token_hash = null,
		    enrollment_token_issued_at = null,
		    enrollment_token_consumed_at = null,
		    sync_token_hash = null
		where monitoring_instance_id = $1`,
		mi.MonitoringInstanceID,
		monitoringinstances.LifecycleRetired,
		monitoringinstances.MonitoringPaused,
	); err != nil {
		t.Fatalf("prepare retired, credential-free related monitoring instance: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set lifecycle_status = $2, usage_status = $3, renewal_decision = $4
		where vps_id = $1`,
		vps.VPSID,
		vpsassets.LifecycleCancelled,
		vpsassets.UsageStandby,
		vpsassets.RenewalAutoRenewCancelled,
	); err != nil {
		t.Fatalf("prepare cancelled VPS: %v", err)
	}

	const archiveReason = "archive after cancellation cleanup"
	archivedReview, err := lifecycleRepository.ApplyVPSArchive(ctx, vps.VPSID, assetlifecycle.ApplyArchiveInput{
		ConfirmationName: vps.DisplayName,
		Reason:           archiveReason,
	})
	if err != nil {
		t.Fatalf("ApplyVPSArchive: %v", err)
	}
	if archivedReview.VPS.LifecycleStatus != vpsassets.LifecycleArchived || archivedReview.VPS.UsageStatus != vpsassets.UsageUnknown {
		t.Fatalf("archived VPS state = (%q, %q), want (archived, unknown)", archivedReview.VPS.LifecycleStatus, archivedReview.VPS.UsageStatus)
	}
	assertVPSStateRepairSnapshot(t, archivedReview.VPS.ArchivedStateSnapshot, vpsassets.LifecycleCancelled, vpsassets.UsageStandby, vpsassets.RenewalAutoRenewCancelled, "archive")
	if archivedReview.VPS.RenewalDecision != vpsassets.RenewalAutoRenewCancelled {
		t.Fatalf("archived VPS renewal decision = %q, want unchanged auto_renew_cancelled", archivedReview.VPS.RenewalDecision)
	}
	archiveSnapshot := *archivedReview.VPS.ArchivedStateSnapshot
	assertVPSStateRepairLifecycleAudit(t, ctx, pool, vps.VPSID, assetlifecycle.ActionTypeArchiveVPS, archiveReason,
		vpsStateRepairExpectedAuditState{lifecycle: vpsassets.LifecycleCancelled, usage: vpsassets.UsageStandby, renewal: vpsassets.RenewalAutoRenewCancelled},
		vpsStateRepairExpectedAuditState{lifecycle: vpsassets.LifecycleArchived, usage: vpsassets.UsageUnknown, renewal: vpsassets.RenewalAutoRenewCancelled, hasArchivedAt: true, snapshot: &vpsStateRepairSnapshotExpectation{lifecycle: vpsassets.LifecycleCancelled, usage: vpsassets.UsageStandby, renewal: vpsassets.RenewalAutoRenewCancelled, source: "archive"}},
	)

	archivedReadback, err := vpsRepository.GetVPSAsset(ctx, vps.VPSID)
	if err != nil {
		t.Fatalf("read archived VPS: %v", err)
	}
	assertVPSStateRepairSnapshot(t, archivedReadback.ArchivedStateSnapshot, vpsassets.LifecycleCancelled, vpsassets.UsageStandby, vpsassets.RenewalAutoRenewCancelled, "archive")
	if *archivedReadback.ArchivedStateSnapshot != archiveSnapshot {
		t.Fatalf("archived snapshot readback = %#v, want preserved snapshot %#v", archivedReadback.ArchivedStateSnapshot, archiveSnapshot)
	}

	const restoreReason = "restore for reassessment"
	restored, err := lifecycleRepository.RestoreVPSFromArchive(ctx, vps.VPSID, assetlifecycle.RestoreArchiveInput{Reason: restoreReason})
	if err != nil {
		t.Fatalf("RestoreVPSFromArchive: %v", err)
	}
	if restored.LifecycleStatus != vpsassets.LifecycleIdle || restored.UsageStatus != vpsassets.UsageUnknown || restored.RenewalDecision != vpsassets.RenewalAutoRenewCancelled || restored.ArchivedAt != nil {
		t.Fatalf("restored VPS = lifecycle:%q usage:%q renewal:%q archived_at:%v; want idle/unknown/unchanged renewal/unarchived", restored.LifecycleStatus, restored.UsageStatus, restored.RenewalDecision, restored.ArchivedAt)
	}
	assertVPSStateRepairSnapshot(t, restored.ArchivedStateSnapshot, vpsassets.LifecycleCancelled, vpsassets.UsageStandby, vpsassets.RenewalAutoRenewCancelled, "archive")
	if *restored.ArchivedStateSnapshot != archiveSnapshot {
		t.Fatalf("restored snapshot = %#v, want original archive snapshot %#v", restored.ArchivedStateSnapshot, archiveSnapshot)
	}
	assertVPSStateRepairLifecycleAudit(t, ctx, pool, vps.VPSID, assetlifecycle.ActionTypeRestoreVPS, restoreReason,
		vpsStateRepairExpectedAuditState{lifecycle: vpsassets.LifecycleArchived, usage: vpsassets.UsageUnknown, renewal: vpsassets.RenewalAutoRenewCancelled, hasArchivedAt: true, snapshot: &vpsStateRepairSnapshotExpectation{lifecycle: vpsassets.LifecycleCancelled, usage: vpsassets.UsageStandby, renewal: vpsassets.RenewalAutoRenewCancelled, source: "archive"}},
		vpsStateRepairExpectedAuditState{lifecycle: vpsassets.LifecycleIdle, usage: vpsassets.UsageUnknown, renewal: vpsassets.RenewalAutoRenewCancelled, snapshot: &vpsStateRepairSnapshotExpectation{lifecycle: vpsassets.LifecycleCancelled, usage: vpsassets.UsageStandby, renewal: vpsassets.RenewalAutoRenewCancelled, source: "archive"}},
	)

	restoredReadback, err := vpsRepository.GetVPSAsset(ctx, vps.VPSID)
	if err != nil {
		t.Fatalf("read restored VPS: %v", err)
	}
	if restoredReadback.LifecycleStatus != vpsassets.LifecycleIdle || restoredReadback.UsageStatus != vpsassets.UsageUnknown || restoredReadback.RenewalDecision != vpsassets.RenewalAutoRenewCancelled {
		t.Fatalf("restored readback state = lifecycle:%q usage:%q renewal:%q", restoredReadback.LifecycleStatus, restoredReadback.UsageStatus, restoredReadback.RenewalDecision)
	}
	assertVPSStateRepairSnapshot(t, restoredReadback.ArchivedStateSnapshot, vpsassets.LifecycleCancelled, vpsassets.UsageStandby, vpsassets.RenewalAutoRenewCancelled, "archive")
	if *restoredReadback.ArchivedStateSnapshot != archiveSnapshot {
		t.Fatalf("restored readback snapshot = %#v, want original archive snapshot %#v", restoredReadback.ArchivedStateSnapshot, archiveSnapshot)
	}

	var gotLinkID, miLifecycle, miMonitoring, enrollmentHash, syncHash string
	var linkActive, miArchivedAtPresent bool
	if err := pool.QueryRow(ctx, `
		select l.link_id, l.unlinked_at is null, mi.lifecycle_status, mi.monitoring_status,
		       mi.archived_at is not null, coalesce(mi.enrollment_token_hash, ''), coalesce(mi.sync_token_hash, '')
		from vps_monitoring_instance_links l
		join monitoring_instances mi using (monitoring_instance_id)
		where l.vps_id = $1 and mi.monitoring_instance_id = $2`, vps.VPSID, mi.MonitoringInstanceID).Scan(
		&gotLinkID, &linkActive, &miLifecycle, &miMonitoring, &miArchivedAtPresent, &enrollmentHash, &syncHash,
	); err != nil {
		t.Fatalf("read related link and monitoring-instance credential state after VPS restore: %v", err)
	}
	if gotLinkID != link.LinkID || !linkActive {
		t.Fatalf("restored VPS relation = (%q, active:%t), want original active link %q unchanged", gotLinkID, linkActive, link.LinkID)
	}
	if miLifecycle != monitoringinstances.LifecycleRetired || miMonitoring != monitoringinstances.MonitoringPaused || miArchivedAtPresent {
		t.Fatalf("related monitoring instance after VPS restore = (%q, %q, archived:%t), want retired/paused/unarchived", miLifecycle, miMonitoring, miArchivedAtPresent)
	}
	if enrollmentHash != "" || syncHash != "" {
		t.Fatalf("related retired monitoring instance credentials after VPS restore = enrollment:%q sync:%q, want no restored tokens", enrollmentHash, syncHash)
	}
}

func TestVPSStateRepairArchiveReviewReportsUnknownAndOnlyEffectiveRunningTargets(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Archive dependency blockers", vpsassets.LifecycleActive, vpsassets.UsageIdle, vpsassets.RenewalKeep)
	targetRepository := NewPostgresTargetRepository(pool)
	serviceRepository := NewPostgresAssetServiceRepository(pool)

	createTarget := func(name string, status string) targets.TargetRecord {
		t.Helper()
		target, err := targetRepository.CreateTarget(ctx, targets.CreateTargetInput{
			Name:                              name,
			TargetType:                        targets.TargetTypeService,
			Host:                              strings.ToLower(strings.ReplaceAll(name, " ", "-")) + ".example.test",
			ExecutionMonitoringInstanceLabels: []string{},
			RunStatus:                         status,
			Labels:                            []string{},
		})
		if err != nil {
			t.Fatalf("create target %q: %v", name, err)
		}
		return target
	}
	createService := func(name string, status assetservices.ServiceStatus, target targets.TargetRecord) assetservices.Record {
		t.Helper()
		targetID := target.TargetID
		service, err := serviceRepository.CreateAssetService(ctx, assetservices.CreateInput{
			VPSID:       vps.VPSID,
			TargetID:    &targetID,
			Name:        name,
			ServiceType: assetservices.ServiceTypeWeb,
			Status:      status,
			Labels:      []string{},
		})
		if err != nil {
			t.Fatalf("create %s service: %v", status, err)
		}
		return service
	}

	activeTarget := createTarget("Active target with effective service", targets.RunStatusEnabled)
	pausedTarget := createTarget("Paused target with active service", targets.RunStatusPaused)
	retiredReferenceTarget := createTarget("Enabled target with retired service", targets.RunStatusEnabled)
	pausedReferenceTarget := createTarget("Enabled target with paused service", targets.RunStatusEnabled)
	unknownReferenceTarget := createTarget("Enabled target with unknown service", targets.RunStatusEnabled)
	activeService := createService("Effective active service", assetservices.ServiceStatusActive, activeTarget)
	createService("Service on paused target", assetservices.ServiceStatusActive, pausedTarget)
	createService("Retired service reference", assetservices.ServiceStatusRetired, retiredReferenceTarget)
	createService("Paused service reference", assetservices.ServiceStatusPaused, pausedReferenceTarget)
	unknownService := createService("Unknown service requiring confirmation", assetservices.ServiceStatusUnknown, unknownReferenceTarget)
	if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status = $2, renewal_decision = $3 where vps_id = $1`, vps.VPSID, vpsassets.LifecycleCancelled, vpsassets.RenewalCancel); err != nil {
		t.Fatalf("prepare cancelled VPS with dependencies: %v", err)
	}

	_, err := NewPostgresAssetLifecycleRepository(pool).ApplyVPSArchive(ctx, vps.VPSID, assetlifecycle.ApplyArchiveInput{
		ConfirmationName: vps.DisplayName,
		Reason:           "archive review must classify each dependency",
	})
	var blocked *assetlifecycle.ArchiveBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("ApplyVPSArchive error = %v, want object-level archive blocker review", err)
	}
	details := make(map[string]assetlifecycle.BlockerDetail, len(blocked.Review.BlockerDetails))
	for _, detail := range blocked.Review.BlockerDetails {
		details[detail.ObjectID] = detail
	}
	unknownDetail, ok := details[unknownService.ServiceID]
	if !ok || unknownDetail.Code != "dependency_needs_confirmation" || unknownDetail.ObjectType != "service" || unknownDetail.CurrentState != string(assetservices.ServiceStatusUnknown) {
		t.Fatalf("unknown service blocker = %#v, want object-level needs-confirmation blocker", unknownDetail)
	}
	activeDetail, ok := details[activeTarget.TargetID]
	if !ok || activeDetail.Code != "target_running" || activeDetail.ObjectType != "target" {
		t.Fatalf("effective active target blocker = %#v, want target_running", activeDetail)
	}
	for _, nonBlockingTarget := range []targets.TargetRecord{pausedTarget, retiredReferenceTarget, pausedReferenceTarget, unknownReferenceTarget} {
		if detail, blocked := details[nonBlockingTarget.TargetID]; blocked {
			t.Errorf("target %q with paused, retired, or unknown-only reference unexpectedly blocks archive: %#v", nonBlockingTarget.TargetID, detail)
		}
	}
	if detail, blocked := details[activeService.ServiceID]; blocked {
		t.Errorf("known active service itself unexpectedly blocks archive instead of its running target: %#v", detail)
	}
	if !containsVPSStateRepairString(blocked.Review.Blockers, "unknown service") && !containsVPSStateRepairString(blocked.Review.Blockers, unknownService.ServiceID) {
		t.Errorf("archive blocker messages %q do not identify the unknown service %q", blocked.Review.Blockers, unknownService.ServiceID)
	}
}

func TestVPSStateRepairArchiveFailureRollsBackAndPersistsFailedAudit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Archive failure audit survives", vpsassets.LifecycleActive, vpsassets.UsageStandby, vpsassets.RenewalKeep)
	prepareVPSStateRepairCancelledVPS(t, ctx, pool, vps.VPSID, vpsassets.UsageStandby, vpsassets.RenewalCancel)
	if _, err := pool.Exec(ctx, `
		create function reject_completed_vps_archive_audit() returns trigger
		language plpgsql as $$
		begin
			if new.action_type = 'archive_vps' and new.status = 'completed' then
				raise exception 'injected completed archive audit failure';
			end if;
			return new;
		end $$`); err != nil {
		t.Fatalf("create audit failure function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		create trigger reject_completed_vps_archive_audit
		before insert on asset_lifecycle_actions
		for each row execute function reject_completed_vps_archive_audit()`); err != nil {
		t.Fatalf("create completed-audit failure trigger: %v", err)
	}

	const reason = "archive audit rollback case"
	_, err := NewPostgresAssetLifecycleRepository(pool).ApplyVPSArchive(ctx, vps.VPSID, assetlifecycle.ApplyArchiveInput{ConfirmationName: vps.DisplayName, Reason: reason})
	if err == nil || !strings.Contains(err.Error(), "injected completed archive audit failure") {
		t.Fatalf("ApplyVPSArchive error = %v, want the injected completed-audit failure", err)
	}
	assertVPSStateRepairStoredVPSState(t, ctx, pool, vps.VPSID, vpsassets.LifecycleCancelled, vpsassets.UsageStandby, vpsassets.RenewalCancel, false)

	var actionStatus, actionReason, stepStatus, stepType string
	var beforeJSON, afterJSON []byte
	if err := pool.QueryRow(ctx, `
		select a.status, a.reason, s.status, s.step_type, s.before_state, s.after_state
		from asset_lifecycle_actions a
		join asset_lifecycle_action_steps s using (action_id)
		where a.vps_id = $1 and a.action_type = $2
		order by a.created_at desc limit 1`, vps.VPSID, assetlifecycle.ActionTypeArchiveVPS).Scan(
		&actionStatus, &actionReason, &stepStatus, &stepType, &beforeJSON, &afterJSON,
	); err != nil {
		t.Fatalf("read failed archive action and step: %v", err)
	}
	if actionStatus != assetlifecycle.ActionStatusFailed || actionReason != reason || stepStatus != assetlifecycle.StepStatusFailed || stepType != assetlifecycle.StepTypeVPSLifecycle {
		t.Fatalf("failed archive audit = action(%q,%q) step(%q,%q), want failed action/step preserving reason", actionStatus, actionReason, stepStatus, stepType)
	}
	before := decodeVPSStateRepairAuditJSON(t, beforeJSON)
	after := decodeVPSStateRepairAuditJSON(t, afterJSON)
	assertVPSStateRepairAuditState(t, before, vpsStateRepairExpectedAuditState{lifecycle: vpsassets.LifecycleCancelled, usage: vpsassets.UsageStandby, renewal: vpsassets.RenewalCancel})
	if _, ok := after["error"].(string); !ok {
		t.Fatalf("failed archive step after_state = %#v, want failure information", after)
	}
}

func TestVPSStateRepairArchiveAuditInsertionFailureIsReturned(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Archive audit insertion failure", vpsassets.LifecycleActive, vpsassets.UsageIdle, vpsassets.RenewalKeep)
	prepareVPSStateRepairCancelledVPS(t, ctx, pool, vps.VPSID, vpsassets.UsageIdle, vpsassets.RenewalCancel)
	if _, err := pool.Exec(ctx, `
		create function reject_all_vps_archive_audits() returns trigger
		language plpgsql as $$
		begin
			if new.action_type = 'archive_vps' then
				raise exception 'injected archive audit insertion failure';
			end if;
			return new;
		end $$`); err != nil {
		t.Fatalf("create archive audit failure function: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		create trigger reject_all_vps_archive_audits
		before insert on asset_lifecycle_actions
		for each row execute function reject_all_vps_archive_audits()`); err != nil {
		t.Fatalf("create archive audit failure trigger: %v", err)
	}

	_, err := NewPostgresAssetLifecycleRepository(pool).ApplyVPSArchive(ctx, vps.VPSID, assetlifecycle.ApplyArchiveInput{ConfirmationName: vps.DisplayName, Reason: "must expose audit failure"})
	if err == nil {
		t.Fatal("ApplyVPSArchive returned success although both completed and failed audit inserts were rejected")
	}
	if !strings.Contains(err.Error(), "injected archive audit insertion failure") {
		t.Fatalf("ApplyVPSArchive error = %v, want audit-insertion failure context", err)
	}
	assertVPSStateRepairStoredVPSState(t, ctx, pool, vps.VPSID, vpsassets.LifecycleCancelled, vpsassets.UsageIdle, vpsassets.RenewalCancel, false)
	var actionCount int
	if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, vps.VPSID, assetlifecycle.ActionTypeArchiveVPS).Scan(&actionCount); err != nil {
		t.Fatalf("count archive actions after rejected audit insertion: %v", err)
	}
	if actionCount != 0 {
		t.Fatalf("archive action count after audit insert failure = %d, want 0", actionCount)
	}
}

func TestVPSStateRepairLifecycleReasonsAreRequired(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	lifecycleRepository := NewPostgresAssetLifecycleRepository(pool)

	archiveVPS := createVPSStateRepairTransitionVPS(t, ctx, pool, "Required archive reason", vpsassets.LifecycleActive, vpsassets.UsageIdle, vpsassets.RenewalKeep)
	prepareVPSStateRepairCancelledVPS(t, ctx, pool, archiveVPS.VPSID, vpsassets.UsageIdle, vpsassets.RenewalCancel)
	if _, err := lifecycleRepository.ApplyVPSArchive(ctx, archiveVPS.VPSID, assetlifecycle.ApplyArchiveInput{ConfirmationName: archiveVPS.DisplayName, Reason: "  "}); !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("archive with blank reason = %v, want invalid input", err)
	}
	assertVPSStateRepairStoredVPSState(t, ctx, pool, archiveVPS.VPSID, vpsassets.LifecycleCancelled, vpsassets.UsageIdle, vpsassets.RenewalCancel, false)

	restoreVPS := createVPSStateRepairTransitionVPS(t, ctx, pool, "Required restore reason", vpsassets.LifecycleActive, vpsassets.UsageStandby, vpsassets.RenewalKeep)
	prepareVPSStateRepairCancelledVPS(t, ctx, pool, restoreVPS.VPSID, vpsassets.UsageStandby, vpsassets.RenewalAutoRenewCancelled)
	if _, err := lifecycleRepository.ApplyVPSArchive(ctx, restoreVPS.VPSID, assetlifecycle.ApplyArchiveInput{ConfirmationName: restoreVPS.DisplayName, Reason: "archive before testing restore reason"}); err != nil {
		t.Fatalf("archive before restore-reason check: %v", err)
	}
	if _, err := lifecycleRepository.RestoreVPSFromArchive(ctx, restoreVPS.VPSID, assetlifecycle.RestoreArchiveInput{Reason: " \t "}); !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("restore with blank reason = %v, want invalid input", err)
	}
	assertVPSStateRepairStoredVPSState(t, ctx, pool, restoreVPS.VPSID, vpsassets.LifecycleArchived, vpsassets.UsageUnknown, vpsassets.RenewalAutoRenewCancelled, true)

	migrationVPS := createVPSStateRepairTransitionVPS(t, ctx, pool, "Required migration reason", vpsassets.LifecycleActive, vpsassets.UsageInUse, vpsassets.RenewalKeep)
	if _, err := lifecycleRepository.StartVPSMigration(ctx, migrationVPS.VPSID, assetlifecycle.StartMigrationInput{Reason: "  "}); !errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput) {
		t.Fatalf("start migration with blank reason = %v, want invalid input", err)
	}
	assertVPSStateRepairStoredVPSState(t, ctx, pool, migrationVPS.VPSID, vpsassets.LifecycleActive, vpsassets.UsageInUse, vpsassets.RenewalKeep, false)

	for _, check := range []struct {
		vpsID      string
		actionType assetlifecycle.ActionType
	}{
		{vpsID: archiveVPS.VPSID, actionType: assetlifecycle.ActionTypeArchiveVPS},
		{vpsID: restoreVPS.VPSID, actionType: assetlifecycle.ActionTypeRestoreVPS},
		{vpsID: migrationVPS.VPSID, actionType: assetlifecycle.ActionTypeStartMigration},
	} {
		var actionCount int
		if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, check.vpsID, check.actionType).Scan(&actionCount); err != nil {
			t.Fatalf("count lifecycle actions for %q: %v", check.actionType, err)
		}
		want := 0
		if actionCount != want {
			t.Errorf("%s action count after blank-reason attempt = %d, want %d", check.actionType, actionCount, want)
		}
	}
}

func TestVPSStateRepairStartMigrationRecordsStateOnceAndPreservesUsage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepository := NewPostgresVPSAssetRepository(pool)
	lifecycleRepository := NewPostgresAssetLifecycleRepository(pool)

	for _, state := range []struct {
		name    string
		initial vpsassets.LifecycleStatus
		usage   vpsassets.UsageStatus
	}{
		{name: "active in use", initial: vpsassets.LifecycleActive, usage: vpsassets.UsageInUse},
		{name: "idle standby", initial: vpsassets.LifecycleIdle, usage: vpsassets.UsageStandby},
		{name: "testing usage", initial: vpsassets.LifecycleTesting, usage: vpsassets.UsageTesting},
	} {
		t.Run(state.name, func(t *testing.T) {
			vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Start migration "+state.name, state.initial, state.usage, vpsassets.RenewalKeep)
			const reason = "approved migration tracking"
			first, err := lifecycleRepository.StartVPSMigration(ctx, vps.VPSID, assetlifecycle.StartMigrationInput{Reason: reason})
			if err != nil {
				t.Fatalf("StartVPSMigration from %s: %v", state.initial, err)
			}
			if first.Action.ActionType != assetlifecycle.ActionTypeStartMigration || first.Action.Status != assetlifecycle.ActionStatusCompleted || first.Action.Reason != reason || len(first.Steps) != 1 {
				t.Fatalf("first migration action result = %#v, want completed start_migration action and one step", first)
			}
			assertVPSStateRepairLifecycleAudit(t, ctx, pool, vps.VPSID, assetlifecycle.ActionTypeStartMigration, reason,
				vpsStateRepairExpectedAuditState{lifecycle: state.initial, usage: state.usage, renewal: vpsassets.RenewalKeep},
				vpsStateRepairExpectedAuditState{lifecycle: vpsassets.LifecycleToMigrate, usage: state.usage, renewal: vpsassets.RenewalMigrate},
			)
			if got := first.Steps[0]; got.StepType != assetlifecycle.StepTypeVPSLifecycle ||
				got.Status != assetlifecycle.StepStatusCompleted ||
				got.BeforeState["lifecycle_status"] != state.initial ||
				got.BeforeState["usage_status"] != state.usage ||
				got.AfterState["lifecycle_status"] != vpsassets.LifecycleToMigrate ||
				got.AfterState["usage_status"] != state.usage ||
				got.AfterState["renewal_decision"] != vpsassets.RenewalMigrate {
				t.Fatalf("migration step = %#v, want usage preserved and migration state recorded", got)
			}
			stored, err := vpsRepository.GetVPSAsset(ctx, vps.VPSID)
			if err != nil {
				t.Fatalf("read migrated VPS: %v", err)
			}
			if stored.LifecycleStatus != vpsassets.LifecycleToMigrate || stored.UsageStatus != state.usage || stored.RenewalDecision != vpsassets.RenewalMigrate {
				t.Fatalf("migrated VPS = lifecycle:%q usage:%q renewal:%q, want to_migrate/%q/migrate", stored.LifecycleStatus, stored.UsageStatus, stored.RenewalDecision, state.usage)
			}

			second, err := lifecycleRepository.StartVPSMigration(ctx, vps.VPSID, assetlifecycle.StartMigrationInput{Reason: "idempotent repeat"})
			if err != nil {
				t.Fatalf("same-state StartVPSMigration: %v", err)
			}
			if second.Action.ActionID != "" || len(second.Steps) != 0 {
				t.Fatalf("same-state migration result = %#v, want no action or steps", second)
			}
			var historyCount int
			if err := pool.QueryRow(ctx, `select count(*) from asset_lifecycle_actions where vps_id = $1 and action_type = $2`, vps.VPSID, assetlifecycle.ActionTypeStartMigration).Scan(&historyCount); err != nil {
				t.Fatalf("count migration action history: %v", err)
			}
			if historyCount != 1 {
				t.Fatalf("same-state migration action history count = %d, want exactly one", historyCount)
			}
		})
	}
}

func TestVPSStateRepairStartMigrationRejectsTerminalStates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	lifecycleRepository := NewPostgresAssetLifecycleRepository(pool)

	for _, state := range []struct {
		lifecycle vpsassets.LifecycleStatus
		usage     vpsassets.UsageStatus
		renewal   vpsassets.RenewalDecision
	}{
		{lifecycle: vpsassets.LifecycleToCancel, usage: vpsassets.UsageIdle, renewal: vpsassets.RenewalCancel},
		{lifecycle: vpsassets.LifecycleCancelled, usage: vpsassets.UsageStandby, renewal: vpsassets.RenewalCancel},
		{lifecycle: vpsassets.LifecycleArchived, usage: vpsassets.UsageUnknown, renewal: vpsassets.RenewalCancel},
	} {
		t.Run(string(state.lifecycle), func(t *testing.T) {
			vps := createVPSStateRepairTransitionVPS(t, ctx, pool, "Reject migration from "+string(state.lifecycle), vpsassets.LifecycleActive, vpsassets.UsageIdle, vpsassets.RenewalKeep)
			if state.lifecycle == vpsassets.LifecycleArchived {
				prepareVPSStateRepairCancelledVPS(t, ctx, pool, vps.VPSID, state.usage, state.renewal)
				if _, err := lifecycleRepository.ApplyVPSArchive(ctx, vps.VPSID, assetlifecycle.ApplyArchiveInput{ConfirmationName: vps.DisplayName, Reason: "archive before migration rejection"}); err != nil {
					t.Fatalf("prepare archived VPS: %v", err)
				}
			} else if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status = $2, usage_status = $3, renewal_decision = $4 where vps_id = $1`, vps.VPSID, state.lifecycle, state.usage, state.renewal); err != nil {
				t.Fatalf("prepare terminal VPS: %v", err)
			}
			_, err := lifecycleRepository.StartVPSMigration(ctx, vps.VPSID, assetlifecycle.StartMigrationInput{Reason: "terminal state must not enter migration"})
			if !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
				t.Fatalf("StartVPSMigration from %s = %v, want lifecycle conflict", state.lifecycle, err)
			}
			assertVPSStateRepairStoredVPSState(t, ctx, pool, vps.VPSID, state.lifecycle, state.usage, state.renewal, state.lifecycle == vpsassets.LifecycleArchived)
		})
	}
}

func createVPSStateRepairTransitionVPS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string, lifecycle vpsassets.LifecycleStatus, usage vpsassets.UsageStatus, renewal vpsassets.RenewalDecision) vpsassets.Record {
	t.Helper()
	vps, err := NewPostgresVPSAssetRepository(pool).CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     name,
		LifecycleStatus: lifecycle,
		UsageStatus:     usage,
		RenewalDecision: renewal,
	})
	if err != nil {
		t.Fatalf("create VPS %q in %s/%s/%s: %v", name, lifecycle, usage, renewal, err)
	}
	return vps
}

func prepareVPSStateRepairCancelledVPS(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, usage vpsassets.UsageStatus, renewal vpsassets.RenewalDecision) {
	t.Helper()
	if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status = $2, usage_status = $3, renewal_decision = $4 where vps_id = $1`, vpsID, vpsassets.LifecycleCancelled, usage, renewal); err != nil {
		t.Fatalf("prepare cancelled VPS %q: %v", vpsID, err)
	}
}

func assertVPSStateRepairStoredVPSState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, wantLifecycle vpsassets.LifecycleStatus, wantUsage vpsassets.UsageStatus, wantRenewal vpsassets.RenewalDecision, wantArchived bool) {
	t.Helper()
	var lifecycle vpsassets.LifecycleStatus
	var usage vpsassets.UsageStatus
	var renewal vpsassets.RenewalDecision
	var archivedAt *time.Time
	var snapshotJSON []byte
	if err := pool.QueryRow(ctx, `select lifecycle_status, usage_status, renewal_decision, archived_at, archived_state_snapshot from vps_assets where vps_id = $1`, vpsID).Scan(&lifecycle, &usage, &renewal, &archivedAt, &snapshotJSON); err != nil {
		t.Fatalf("read VPS state for %q: %v", vpsID, err)
	}
	if lifecycle != wantLifecycle || usage != wantUsage || renewal != wantRenewal || (archivedAt != nil) != wantArchived {
		t.Fatalf("stored VPS state = lifecycle:%q usage:%q renewal:%q archived_at:%v, want lifecycle:%q usage:%q renewal:%q archived:%t", lifecycle, usage, renewal, archivedAt, wantLifecycle, wantUsage, wantRenewal, wantArchived)
	}
	if wantArchived && (len(snapshotJSON) == 0 || string(snapshotJSON) == "null") {
		t.Fatalf("archived VPS %q has no retained state snapshot", vpsID)
	}
	if !wantArchived && len(snapshotJSON) != 0 && string(snapshotJSON) != "null" {
		t.Fatalf("unarchived VPS %q unexpectedly has archived snapshot %s", vpsID, snapshotJSON)
	}
}

type vpsStateRepairExpectedAuditState struct {
	lifecycle     vpsassets.LifecycleStatus
	usage         vpsassets.UsageStatus
	renewal       vpsassets.RenewalDecision
	hasArchivedAt bool
	snapshot      *vpsStateRepairSnapshotExpectation
}

type vpsStateRepairSnapshotExpectation struct {
	lifecycle vpsassets.LifecycleStatus
	usage     vpsassets.UsageStatus
	renewal   vpsassets.RenewalDecision
	source    string
}

func assertVPSStateRepairLifecycleAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, actionType assetlifecycle.ActionType, reason string, beforeWant, afterWant vpsStateRepairExpectedAuditState) {
	t.Helper()
	var actionStatus, actionReason, objectType, objectID, stepType, stepStatus, message string
	var beforeJSON, afterJSON []byte
	if err := pool.QueryRow(ctx, `
		select a.status, a.reason, s.object_type, s.object_id, s.step_type, s.status, s.message, s.before_state, s.after_state
		from asset_lifecycle_actions a
		join asset_lifecycle_action_steps s using (action_id)
		where a.vps_id = $1 and a.action_type = $2
		order by a.created_at desc limit 1`, vpsID, actionType).Scan(
		&actionStatus, &actionReason, &objectType, &objectID, &stepType, &stepStatus, &message, &beforeJSON, &afterJSON,
	); err != nil {
		t.Fatalf("read %s lifecycle audit: %v", actionType, err)
	}
	if actionStatus != assetlifecycle.ActionStatusCompleted || actionReason != reason || objectType != assetlifecycle.ObjectTypeVPS || objectID != vpsID || stepType != assetlifecycle.StepTypeVPSLifecycle || stepStatus != assetlifecycle.StepStatusCompleted || message != reason {
		t.Fatalf("%s audit = action(%q,%q) step(%q,%q,%q,%q), want completed VPS audit with full reason", actionType, actionStatus, actionReason, objectType, stepType, stepStatus, message)
	}
	assertVPSStateRepairAuditState(t, decodeVPSStateRepairAuditJSON(t, beforeJSON), beforeWant)
	assertVPSStateRepairAuditState(t, decodeVPSStateRepairAuditJSON(t, afterJSON), afterWant)
}

func decodeVPSStateRepairAuditJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode lifecycle audit state %q: %v", raw, err)
	}
	return state
}

func assertVPSStateRepairAuditState(t *testing.T, state map[string]any, want vpsStateRepairExpectedAuditState) {
	t.Helper()
	if state["lifecycle_status"] != string(want.lifecycle) || state["usage_status"] != string(want.usage) || state["renewal_decision"] != string(want.renewal) {
		t.Fatalf("lifecycle audit state = %#v, want %s/%s/%s", state, want.lifecycle, want.usage, want.renewal)
	}
	archivedAt := state["archived_at"]
	if (archivedAt != nil) != want.hasArchivedAt {
		t.Fatalf("lifecycle audit archived_at = %#v, want present:%t", archivedAt, want.hasArchivedAt)
	}
	if archivedAt != nil {
		value, ok := archivedAt.(string)
		if !ok {
			t.Fatalf("lifecycle audit archived_at type = %T, want string", archivedAt)
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			t.Fatalf("parse lifecycle audit archived_at %q: %v", value, err)
		}
	}
	snapshotValue := state["archived_state_snapshot"]
	if want.snapshot == nil {
		if snapshotValue != nil {
			t.Fatalf("lifecycle audit snapshot = %#v, want null", snapshotValue)
		}
		return
	}
	snapshot, ok := snapshotValue.(map[string]any)
	if !ok {
		t.Fatalf("lifecycle audit snapshot = %#v, want archived snapshot object", snapshotValue)
	}
	if snapshot["lifecycle_status"] != string(want.snapshot.lifecycle) || snapshot["usage_status"] != string(want.snapshot.usage) || snapshot["renewal_decision"] != string(want.snapshot.renewal) || snapshot["source"] != want.snapshot.source {
		t.Fatalf("lifecycle audit snapshot = %#v, want %s/%s/%s source:%s", snapshot, want.snapshot.lifecycle, want.snapshot.usage, want.snapshot.renewal, want.snapshot.source)
	}
	capturedAt, ok := snapshot["captured_at"].(string)
	if !ok || capturedAt == "" {
		t.Fatalf("lifecycle audit snapshot captured_at = %#v, want timestamp", snapshot["captured_at"])
	}
	if _, err := time.Parse(time.RFC3339Nano, capturedAt); err != nil {
		t.Fatalf("parse lifecycle audit snapshot captured_at %q: %v", capturedAt, err)
	}
}

func assertVPSStateRepairSnapshot(t *testing.T, snapshot *vpsassets.ArchivedStateSnapshot, lifecycle vpsassets.LifecycleStatus, usage vpsassets.UsageStatus, renewal vpsassets.RenewalDecision, source string) {
	t.Helper()
	if snapshot == nil {
		t.Fatal("archived state snapshot is nil")
	}
	if snapshot.LifecycleStatus != lifecycle || snapshot.UsageStatus != usage || snapshot.RenewalDecision != renewal || snapshot.Source != source || snapshot.CapturedAt.IsZero() {
		t.Fatalf("archived state snapshot = %#v, want %s/%s/%s source:%s with capture time", snapshot, lifecycle, usage, renewal, source)
	}
}

func containsVPSStateRepairString(values []string, wanted string) bool {
	for _, value := range values {
		if strings.Contains(value, wanted) {
			return true
		}
	}
	return false
}
