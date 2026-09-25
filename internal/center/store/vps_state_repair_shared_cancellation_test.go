package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairCancellationSharedImpactsRequireConfirmationAndRemainSelected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)

	vpsA := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "shared-cancel-a", vpsassets.LifecycleActive)
	vpsB := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "shared-cancel-b", vpsassets.LifecycleActive)
	vpsC := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "shared-cancel-c", vpsassets.LifecycleActive)

	sharedMI := createVPSStateRepairCancellationMI(t, ctx, miRepo, "shared-cancel-mi")
	for _, vpsID := range []string{vpsA.VPSID, vpsB.VPSID} {
		if _, err := linkRepo.LinkMonitoringInstance(ctx, vpsID, assetlinks.LinkInput{MonitoringInstanceID: sharedMI.MonitoringInstanceID}); err != nil {
			t.Fatalf("link shared MI to %s: %v", vpsID, err)
		}
	}
	historicalMI := createVPSStateRepairCancellationMI(t, ctx, miRepo, "archived-parent-mi")
	if _, err := linkRepo.LinkMonitoringInstance(ctx, vpsC.VPSID, assetlinks.LinkInput{MonitoringInstanceID: historicalMI.MonitoringInstanceID}); err != nil {
		t.Fatalf("link historical MI to %s: %v", vpsC.VPSID, err)
	}

	sharedTarget := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "shared-cancel-target")
	unselectedTarget := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "unselected-cancel-target")
	for _, relationship := range []struct {
		vpsID  string
		target targets.TargetRecord
		status assetservices.ServiceStatus
		name   string
	}{
		{vpsID: vpsA.VPSID, target: sharedTarget, status: assetservices.ServiceStatusActive, name: "shared-a"},
		{vpsID: vpsB.VPSID, target: sharedTarget, status: assetservices.ServiceStatusActive, name: "shared-b"},
		{vpsID: vpsC.VPSID, target: sharedTarget, status: assetservices.ServiceStatusRetired, name: "shared-archived-c"},
		{vpsID: vpsA.VPSID, target: unselectedTarget, status: assetservices.ServiceStatusActive, name: "unselected-a"},
	} {
		createVPSStateRepairCancellationService(t, ctx, serviceRepo, relationship.vpsID, relationship.target.TargetID, relationship.name, relationship.status)
	}
	for _, relationship := range []struct {
		vpsID  string
		status assetdomains.DomainStatus
		name   string
	}{
		{vpsID: vpsA.VPSID, status: assetdomains.DomainStatusActive, name: "shared-a.example.test"},
		{vpsID: vpsB.VPSID, status: assetdomains.DomainStatusActive, name: "shared-b.example.test"},
		{vpsID: vpsC.VPSID, status: assetdomains.DomainStatusRetired, name: "shared-archived-c.example.test"},
	} {
		if _, err := domainRepo.CreateAssetDomain(ctx, assetdomains.CreateInput{
			VPSID:      relationship.vpsID,
			TargetID:   new(sharedTarget.TargetID),
			DomainName: relationship.name,
			Status:     relationship.status,
		}); err != nil {
			t.Fatalf("create domain %s: %v", relationship.name, err)
		}
	}

	if _, err := targetRepo.PauseTargetRun(ctx, unselectedTarget.TargetID); err != nil {
		t.Fatalf("pause unselected Target: %v", err)
	}
	if _, err := targetRepo.ArchiveTarget(ctx, unselectedTarget.TargetID); err != nil {
		t.Fatalf("archive unselected Target: %v", err)
	}
	cancelVPSStateRepairParent(t, ctx, lifecycleRepo, vpsB.VPSID)
	cancelVPSStateRepairParent(t, ctx, lifecycleRepo, vpsC.VPSID)
	if _, err := miRepo.RetireMonitoringInstance(ctx, historicalMI.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "archive parent fixture"}); err != nil {
		t.Fatalf("retire MI for archived parent: %v", err)
	}
	archivedParent, err := lifecycleRepo.ApplyVPSArchive(ctx, vpsC.VPSID, assetlifecycle.ApplyArchiveInput{
		ConfirmationName: vpsC.DisplayName,
		Reason:           "retain dependency history",
	})
	if err != nil {
		t.Fatalf("archive parent with retired historical dependencies: %v", err)
	}
	if archivedParent.VPS.LifecycleStatus != vpsassets.LifecycleArchived {
		t.Fatalf("archived parent state = %q, want archived", archivedParent.VPS.LifecycleStatus)
	}

	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set lifecycle_status = $2,
			monitoring_status = $3,
			binding_status = $4,
			binding_fingerprint = 'fingerprint-current',
			binding_epoch_started_at = now(),
			enrollment_token_hash = $5,
			enrollment_token_issued_at = now(),
			enrollment_token_consumed_at = now(),
			sync_token_hash = $6,
			pending_binding_fingerprint = 'fingerprint-pending',
			pending_binding_first_seen_at = now(),
			pending_binding_last_seen_at = now(),
			pending_binding_attempt_count = 2,
			pending_action_id = 'act_shared_cancellation',
			pending_action_command_id = 'cmd_shared_cancellation',
			last_action = '{"action_id":"act_shared_cancellation","command_id":"cmd_shared_cancellation","status":"pending"}'::jsonb
		where monitoring_instance_id = $1`,
		sharedMI.MonitoringInstanceID,
		monitoringinstances.LifecycleRetired,
		monitoringinstances.MonitoringEnabled,
		monitoringinstances.BindingPendingConfirmation,
		hashEnrollmentToken("shared-cancellation-enrollment-token"),
		hashSyncToken("shared-cancellation-sync-token")); err != nil {
		t.Fatalf("seed dirty already-retired shared MI state: %v", err)
	}

	preview, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview: %v", err)
	}
	assertVPSStateRepairCancellationImpactClasses(t, preview.DependencyImpacts, assetlifecycle.ObjectTypeMonitoringInstance, sharedMI.MonitoringInstanceID, map[string]string{
		vpsA.VPSID: assetlinks.DependencyCurrent,
		vpsB.VPSID: assetlinks.DependencyResidual,
	})
	assertVPSStateRepairCancellationImpactClasses(t, preview.DependencyImpacts, assetlifecycle.ObjectTypeTarget, sharedTarget.TargetID, map[string]string{
		vpsA.VPSID: assetlinks.DependencyCurrent,
		vpsB.VPSID: assetlinks.DependencyResidual,
		vpsC.VPSID: assetlinks.DependencyHistorical,
	})
	assertVPSStateRepairCancellationImpactClasses(t, preview.DependencyImpacts, assetlifecycle.ObjectTypeTarget, unselectedTarget.TargetID, map[string]string{
		vpsA.VPSID: assetlinks.DependencyCurrent,
	})

	input := assetlifecycle.ApplyCancellationInput{
		Reason:             "retire and pause selected shared assets",
		VPSLifecycleStatus: vpsassets.LifecycleCancelled,
		PreviewDigest:      preview.PreviewDigest,
		MonitoringInstanceActions: []assetlifecycle.MonitoringInstanceActionInput{{
			MonitoringInstanceID: sharedMI.MonitoringInstanceID,
			LifecycleStatus:      monitoringinstances.LifecycleRetired,
			MonitoringStatus:     monitoringinstances.MonitoringPaused,
		}},
		TargetActions: []assetlifecycle.TargetActionInput{{TargetID: sharedTarget.TargetID, RunStatus: targets.RunStatusPaused}},
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, input); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("shared cancellation without confirmed_shared_objects = %v, want confirmation required", err)
	}
	assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vpsA.VPSID, vpsassets.LifecycleActive)
	assertVPSStateRepairMIRuntimeState(t, ctx, pool, sharedMI.MonitoringInstanceID, monitoringinstances.LifecycleRetired, monitoringinstances.MonitoringEnabled)
	assertVPSStateRepairMIEventCount(t, ctx, pool, sharedMI.MonitoringInstanceID, incidents.EventMonitoringInstanceRetirementReconciled, 0)
	unchangedMI, err := miRepo.GetMonitoringInstance(ctx, sharedMI.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("read dirty MI after rejected shared cancellation: %v", err)
	}
	if unchangedMI.SyncTokenHash != hashSyncToken("shared-cancellation-sync-token") ||
		unchangedMI.EnrollmentTokenHash != hashEnrollmentToken("shared-cancellation-enrollment-token") ||
		unchangedMI.PendingBindingFingerprint != "fingerprint-pending" ||
		unchangedMI.PendingActionID != "act_shared_cancellation" {
		t.Fatalf("rejected shared action changed dirty MI state: %#v", unchangedMI)
	}
	assertTargetRunStatus(t, ctx, pool, sharedTarget.TargetID, targets.RunStatusEnabled)

	input.ConfirmedSharedObjects = []assetlinks.SharedObjectReference{
		{ObjectType: assetlifecycle.ObjectTypeMonitoringInstance, ObjectID: sharedMI.MonitoringInstanceID},
		{ObjectType: assetlifecycle.ObjectTypeTarget, ObjectID: sharedTarget.TargetID},
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, input); err != nil {
		t.Fatalf("ApplyVPSCancellation with the same preview and explicit shared selections: %v", err)
	}
	assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vpsA.VPSID, vpsassets.LifecycleCancelled)
	assertVPSStateRepairMIRuntimeState(t, ctx, pool, sharedMI.MonitoringInstanceID, monitoringinstances.LifecycleRetired, monitoringinstances.MonitoringPaused)
	reconciledMI, err := miRepo.GetMonitoringInstance(ctx, sharedMI.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("read MI after cancellation retirement reconciliation: %v", err)
	}
	if reconciledMI.BindingStatus != monitoringinstances.BindingBound ||
		reconciledMI.EnrollmentTokenHash != "" || reconciledMI.EnrollmentTokenIssuedAt != nil || reconciledMI.EnrollmentTokenConsumedAt != nil ||
		reconciledMI.SyncTokenHash != "" || reconciledMI.PendingBindingFingerprint != "" || reconciledMI.PendingBindingFirstSeenAt != nil ||
		reconciledMI.PendingBindingLastSeenAt != nil || reconciledMI.PendingBindingAttemptCount != 0 ||
		reconciledMI.PendingActionID != "" || reconciledMI.PendingActionCommandID != "" || reconciledMI.LastAction != nil {
		t.Fatalf("selected MI retirement did not reconcile credentials and pending state: %#v", reconciledMI)
	}
	assertTargetRunStatus(t, ctx, pool, sharedTarget.TargetID, targets.RunStatusPaused)
	assertTargetRunStatus(t, ctx, pool, unselectedTarget.TargetID, targets.RunStatusArchived)
	assertStateChangeEventCount(t, ctx, pool, "target", unselectedTarget.TargetID, string(incidents.EventTargetRestoredToPaused), 0)
	assertVPSStateRepairMIRuntimeState(t, ctx, pool, historicalMI.MonitoringInstanceID, monitoringinstances.LifecycleRetired, monitoringinstances.MonitoringPaused)
	assertVPSStateRepairMIEventCount(t, ctx, pool, sharedMI.MonitoringInstanceID, incidents.EventMonitoringInstanceRetirementReconciled, 1)
	assertVPSStateRepairMIReconciliationPayload(t, ctx, pool, sharedMI.MonitoringInstanceID)
	assertTargetEventCount(t, ctx, pool, sharedTarget.TargetID, 1)
}

func TestVPSStateRepairCancellationRequiresConfirmationForAnotherVPSUnconfirmedTargetDependency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)

	vpsA := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "unconfirmed-shared-a", vpsassets.LifecycleActive)
	vpsB := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "unconfirmed-shared-b", vpsassets.LifecycleActive)
	target := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "unconfirmed-shared-target")
	createVPSStateRepairCancellationService(t, ctx, serviceRepo, vpsA.VPSID, target.TargetID, "unconfirmed-shared-a", assetservices.ServiceStatusActive)
	serviceB := createVPSStateRepairCancellationService(t, ctx, serviceRepo, vpsB.VPSID, target.TargetID, "unconfirmed-shared-b", assetservices.ServiceStatusUnknown)

	preview, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview: %v", err)
	}
	assertVPSStateRepairCancellationImpactClasses(t, preview.DependencyImpacts, assetlifecycle.ObjectTypeTarget, target.TargetID, map[string]string{
		vpsA.VPSID: assetlinks.DependencyCurrent,
		vpsB.VPSID: assetlinks.DependencyNeedsConfirmation,
	})
	input := assetlifecycle.ApplyCancellationInput{
		Reason:             "cancel A while B dependency is unconfirmed",
		VPSLifecycleStatus: vpsassets.LifecycleToCancel,
		TargetActions:      []assetlifecycle.TargetActionInput{{TargetID: target.TargetID, RunStatus: targets.RunStatusPaused}},
		PreviewDigest:      preview.PreviewDigest,
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, input); !errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired) {
		t.Fatalf("cancellation touching another VPS's unconfirmed dependency without confirmation = %v, want confirmation required", err)
	}
	assertTargetRunStatus(t, ctx, pool, target.TargetID, targets.RunStatusEnabled)
	assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vpsA.VPSID, vpsassets.LifecycleActive)

	preview, err = lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview after rejection: %v", err)
	}
	input.PreviewDigest = preview.PreviewDigest
	input.ConfirmedSharedObjects = []assetlinks.SharedObjectReference{{ObjectType: assetlifecycle.ObjectTypeTarget, ObjectID: target.TargetID}}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, input); err != nil {
		t.Fatalf("ApplyVPSCancellation with confirmed unconfirmed dependency: %v", err)
	}
	assertTargetRunStatus(t, ctx, pool, target.TargetID, targets.RunStatusPaused)
	assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vpsA.VPSID, vpsassets.LifecycleToCancel)
	var serviceStatus string
	if err := pool.QueryRow(ctx, `select status from asset_services where service_id = $1`, serviceB.ServiceID).Scan(&serviceStatus); err != nil {
		t.Fatalf("read VPS B service status: %v", err)
	}
	if serviceStatus != string(assetservices.ServiceStatusUnknown) {
		t.Fatalf("VPS B service status = %q, want unknown", serviceStatus)
	}
}

func TestVPSStateRepairCancellationPreviewDigestTracksGraphFactsButNotHeartbeat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	subscriptionRepo := NewPostgresSubscriptionRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)

	vpsA := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "digest-cancel-a", vpsassets.LifecycleActive)
	vpsB := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "digest-cancel-b", vpsassets.LifecycleActive)
	mi := createVPSStateRepairCancellationMI(t, ctx, miRepo, "digest-mi")
	if _, err := linkRepo.LinkMonitoringInstance(ctx, vpsA.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID}); err != nil {
		t.Fatalf("link MI to initial parent: %v", err)
	}
	subscription, err := subscriptionRepo.CreateSubscription(ctx, subscriptions.CreateInput{
		VPSID:         vpsA.VPSID,
		Price:         10,
		Currency:      "USD",
		BillingMonths: 1,
		Status:        subscriptions.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	target := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "digest-archived-target")
	createVPSStateRepairCancellationService(t, ctx, serviceRepo, vpsA.VPSID, target.TargetID, "digest-target-service", assetservices.ServiceStatusActive)
	if _, err := targetRepo.PauseTargetRun(ctx, target.TargetID); err != nil {
		t.Fatalf("pause fixture Target: %v", err)
	}
	if _, err := targetRepo.ArchiveTarget(ctx, target.TargetID); err != nil {
		t.Fatalf("archive fixture Target: %v", err)
	}
	const syncToken = "digest-heartbeat-sync-token"
	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set binding_status = $2,
			binding_fingerprint = 'digest-fingerprint',
			sync_token_hash = $3
		where monitoring_instance_id = $1`,
		mi.MonitoringInstanceID,
		monitoringinstances.BindingBound,
		hashSyncToken(syncToken)); err != nil {
		t.Fatalf("prepare supported heartbeat path: %v", err)
	}

	beforeLink, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview before new link: %v", err)
	}
	if _, err := linkRepo.LinkMonitoringInstance(ctx, vpsB.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID}); err != nil {
		t.Fatalf("add second parent link after preview: %v", err)
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, cancellationInputForStateRepairDigest(beforeLink.PreviewDigest)); !errors.Is(err, assetlifecycle.ErrStaleCancellationPreview) {
		t.Fatalf("ApplyVPSCancellation after new parent link = %v, want stale preview", err)
	}

	beforeDate, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview before subscription date update: %v", err)
	}
	newRenewAt := subscriptions.NewDate(time.Now().UTC().AddDate(0, 0, 45))
	if _, err := subscriptionRepo.PatchSubscription(ctx, subscription.SubscriptionID, subscriptions.PatchInput{RenewAt: subscriptions.PatchDate(&newRenewAt)}); err != nil {
		t.Fatalf("change active subscription date: %v", err)
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, cancellationInputForStateRepairDigest(beforeDate.PreviewDigest)); !errors.Is(err, assetlifecycle.ErrStaleCancellationPreview) {
		t.Fatalf("ApplyVPSCancellation after subscription date update = %v, want stale preview", err)
	}

	beforeHeartbeat, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview before heartbeat: %v", err)
	}
	observedAt := time.Now().UTC().Add(-time.Minute)
	if err := miRepo.RecordAcceptedHeartbeats(ctx, syncToken, []enrollment.HeartbeatWrite{{
		MonitoringInstanceID: mi.MonitoringInstanceID,
		ObservedAt:           observedAt,
		ReceivedAt:           observedAt.Add(time.Second),
		AgentVersion:         "digest-test-agent",
		Fingerprint:          "digest-fingerprint",
		SyncBatchID:          "digest-test-batch",
	}}); err != nil {
		t.Fatalf("RecordAcceptedHeartbeats: %v", err)
	}
	afterHeartbeat, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview after heartbeat: %v", err)
	}
	if afterHeartbeat.PreviewDigest != beforeHeartbeat.PreviewDigest {
		t.Fatalf("heartbeat changed cancellation preview digest from %q to %q", beforeHeartbeat.PreviewDigest, afterHeartbeat.PreviewDigest)
	}

	cancelVPSStateRepairParent(t, ctx, lifecycleRepo, vpsA.VPSID)
	cancelVPSStateRepairParent(t, ctx, lifecycleRepo, vpsB.VPSID)
	managementReview, err := miRepo.GetMonitoringInstanceManagementReview(ctx, mi.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("GetMonitoringInstanceManagementReview before retirement: %v", err)
	}
	sharedConfirmation := assetlinks.GlobalActionConfirmation{PreviewDigest: managementReview.PreviewDigest, ConfirmSharedImpact: true}
	if _, err := miRepo.RetireMonitoringInstance(ctx, mi.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{
		Reason:                   "prepare archived MI digest case",
		GlobalActionConfirmation: sharedConfirmation,
	}); err != nil {
		t.Fatalf("RetireMonitoringInstance with residual shared confirmation: %v", err)
	}
	preArchiveCancellation, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview before MI archive: %v", err)
	}
	managementReview, err = miRepo.GetMonitoringInstanceManagementReview(ctx, mi.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("GetMonitoringInstanceManagementReview before archive: %v", err)
	}
	if _, err := miRepo.ArchiveMonitoringInstance(ctx, mi.MonitoringInstanceID, monitoringinstances.ArchiveInput{
		Reason:                   "archive residual MI",
		ConfirmationName:         mi.DisplayName,
		GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{PreviewDigest: managementReview.PreviewDigest, ConfirmSharedImpact: true},
	}); err != nil {
		t.Fatalf("ArchiveMonitoringInstance with residual-link confirmation: %v", err)
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, cancellationInputForStateRepairDigest(preArchiveCancellation.PreviewDigest)); !errors.Is(err, assetlifecycle.ErrStaleCancellationPreview) {
		t.Fatalf("ApplyVPSCancellation after linked MI archive = %v, want stale preview", err)
	}
	archivedPreview, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vpsA.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview after MI archive: %v", err)
	}
	_, err = lifecycleRepo.ApplyVPSCancellation(ctx, vpsA.VPSID, assetlifecycle.ApplyCancellationInput{
		Reason:             "reject archived MI action",
		VPSLifecycleStatus: vpsassets.LifecycleCancelled,
		PreviewDigest:      archivedPreview.PreviewDigest,
		MonitoringInstanceActions: []assetlifecycle.MonitoringInstanceActionInput{{
			MonitoringInstanceID: mi.MonitoringInstanceID,
			LifecycleStatus:      monitoringinstances.LifecycleRetired,
		}},
		ConfirmedSharedObjects: []assetlinks.SharedObjectReference{{
			ObjectType: assetlifecycle.ObjectTypeMonitoringInstance,
			ObjectID:   mi.MonitoringInstanceID,
		}},
	})
	if !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
		t.Fatalf("cancellation action on archived MI = %v, want blocked", err)
	}
	assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vpsA.VPSID, vpsassets.LifecycleCancelled)
	gotTarget, err := targetRepo.GetTarget(ctx, target.TargetID)
	if err != nil {
		t.Fatalf("GetTarget after rejected archived MI action: %v", err)
	}
	if gotTarget.RunStatus != targets.RunStatusArchived {
		t.Fatalf("archived Target run status after rejected cancellation = %q, want archived", gotTarget.RunStatus)
	}
	assertStateChangeEventCount(t, ctx, pool, "target", target.TargetID, string(incidents.EventTargetRestoredToPaused), 0)
}

func TestVPSStateRepairCancellationPreviewDigestTracksActualPendingEnrollmentSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	syncRepo := NewPostgresSyncRepository(pool)

	vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "pending-sync-digest-vps", vpsassets.LifecycleActive)
	mi, err := miRepo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     "pending-sync-digest-mi",
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance pending enrollment: %v", err)
	}
	if _, err := linkRepo.LinkMonitoringInstance(ctx, vps.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID}); err != nil {
		t.Fatalf("LinkMonitoringInstance: %v", err)
	}

	const syncToken = "pending-sync-digest-token"
	const fingerprint = "pending-sync-digest-fingerprint"
	if _, err := pool.Exec(ctx, `
		update monitoring_instances
		set binding_status = $2,
			binding_fingerprint = $3,
			sync_token_hash = $4
		where monitoring_instance_id = $1`,
		mi.MonitoringInstanceID,
		monitoringinstances.BindingBound,
		fingerprint,
		syncRepo.tokenHasher.hashSyncToken(syncToken)); err != nil {
		t.Fatalf("prepare accepted pending-enrollment sync state: %v", err)
	}

	beforeSync, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vps.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview before accepted sync: %v", err)
	}
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	batchID := "pending_sync_digest_batch"
	batch := syncing.Batch{
		MonitoringInstanceID: mi.MonitoringInstanceID,
		SyncToken:            syncToken,
		Heartbeats: []syncing.HeartbeatPayload{{
			ObservedAt:   observedAt,
			AgentVersion: "agent/pending-sync-digest",
			Fingerprint:  fingerprint,
			SyncBatchID:  batchID,
		}},
		Observations: observations.BatchWrite{
			MonitoringInstanceID: mi.MonitoringInstanceID,
			HostSamples: []observations.HostSampleWrite{{
				MonitoringInstanceID: mi.MonitoringInstanceID,
				ObservedAt:           observedAt,
				AgentVersion:         "agent/pending-sync-digest",
				Fingerprint:          fingerprint,
				CPUUsagePct:          12,
				MemUsedPct:           40,
				MemAvailableBytes:    2 << 30,
				MemTotalBytes:        4 << 30,
				DiskUsedPct:          10,
				DiskTotalBytes:       100 << 30,
				InodeUsedPct:         5,
				UptimeSeconds:        3600,
				SyncBatchID:          batchID,
			}},
		},
	}
	holder, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatalf("begin graph lock before pending-enrollment sync: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	type syncOutcome struct {
		result syncing.Result
		err    error
	}
	syncCh := make(chan syncOutcome, 1)
	go func() {
		result, syncErr := syncRepo.ApplyBatch(ctx, batch)
		syncCh <- syncOutcome{result: result, err: syncErr}
	}()
	if err := waitForAssetGraphSyncLockWaiter(ctx, pool); err != nil {
		t.Fatalf("pending-enrollment sync did not wait for asset graph lock: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release graph lock before pending-enrollment sync: %v", err)
	}
	outcome := <-syncCh
	if outcome.err != nil {
		t.Fatalf("ApplyBatch pending-enrollment sync: %v", outcome.err)
	}
	if outcome.result.Disposition != syncing.ResultDispositionRecorded {
		t.Fatalf("ApplyBatch disposition = %q, want recorded", outcome.result.Disposition)
	}
	gotMI, err := miRepo.GetMonitoringInstance(ctx, mi.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("GetMonitoringInstance after accepted sync: %v", err)
	}
	if gotMI.LifecycleStatus != monitoringinstances.LifecycleInUse {
		t.Fatalf("MI lifecycle after accepted host sample = %q, want in-use", gotMI.LifecycleStatus)
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vps.VPSID, cancellationInputForStateRepairDigest(beforeSync.PreviewDigest)); !errors.Is(err, assetlifecycle.ErrStaleCancellationPreview) {
		t.Fatalf("ApplyVPSCancellation after accepted pending-enrollment sync = %v, want stale preview", err)
	}
}

func TestVPSStateRepairCancellationPreviewWaitsForCommittedGraphFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "preview-waits-for-facts", vpsassets.LifecycleActive)
	before, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vps.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview baseline: %v", err)
	}

	holder, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatalf("begin graph lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	previewDone := make(chan struct {
		preview assetlifecycle.CancellationPreview
		err     error
	}, 1)
	go func() {
		previewCtx, previewCancel := context.WithTimeout(ctx, 15*time.Second)
		defer previewCancel()
		preview, previewErr := lifecycleRepo.GetVPSCancellationPreview(previewCtx, vps.VPSID)
		previewDone <- struct {
			preview assetlifecycle.CancellationPreview
			err     error
		}{preview: preview, err: previewErr}
	}()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 1); err != nil {
		t.Fatalf("cancellation preview did not wait for graph lock: %v", err)
	}
	const serviceID = "svc_preview_committed_fact"
	if _, err := holder.Exec(ctx, `
		insert into asset_services (service_id, vps_id, name, service_type, status)
		values ($1, $2, 'committed while preview waited', 'web', 'active')`, serviceID, vps.VPSID); err != nil {
		t.Fatalf("insert committed service fact under graph lock: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit new graph fact: %v", err)
	}
	result := <-previewDone
	if result.err != nil {
		t.Fatalf("GetVPSCancellationPreview after graph lock release: %v", result.err)
	}
	if len(result.preview.Services) != 1 || result.preview.Services[0].ServiceID != serviceID {
		t.Fatalf("preview services after waiting = %#v, want committed service %q", result.preview.Services, serviceID)
	}
	if result.preview.PreviewDigest == before.PreviewDigest {
		t.Fatal("preview digest did not include the newly committed service fact")
	}
}

func TestVPSStateRepairArchiveRacesWithRelationshipWritersSerialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)
	subscriptionRepo := NewPostgresSubscriptionRepository(pool)

	t.Run("link commits before archive", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-link-writer-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		mi := createVPSStateRepairCancellationMI(t, ctx, miRepo, "archive-link-writer-first-mi")
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, true, func(writeCtx context.Context) error {
			_, err := linkRepo.LinkMonitoringInstance(writeCtx, vps.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID})
			return err
		})
		if writeErr != nil || !errors.Is(archiveErr, assetlifecycle.ErrLifecycleActionBlocked) {
			t.Fatalf("link-first serialization = archive error %v, link error %v, want archive blocked after link commit", archiveErr, writeErr)
		}
		assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vps.VPSID, vpsassets.LifecycleToCancel)
	})

	t.Run("service commits before archive", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-service-writer-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, true, func(writeCtx context.Context) error {
			_, err := serviceRepo.CreateAssetService(writeCtx, assetservices.CreateInput{
				VPSID:       vps.VPSID,
				Name:        "archive race service",
				ServiceType: assetservices.ServiceTypeWeb,
				Status:      assetservices.ServiceStatusActive,
			})
			return err
		})
		assertVPSStateRepairServiceDomainArchiveRaceResult(t, ctx, pool, vps.VPSID, "asset_services", archiveErr, writeErr)
	})

	t.Run("domain commits before archive", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-domain-writer-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, true, func(writeCtx context.Context) error {
			_, err := domainRepo.CreateAssetDomain(writeCtx, assetdomains.CreateInput{
				VPSID:      vps.VPSID,
				DomainName: strings.ReplaceAll(vps.VPSID, "_", "-") + ".example.test",
				Status:     assetdomains.DomainStatusActive,
			})
			return err
		})
		assertVPSStateRepairServiceDomainArchiveRaceResult(t, ctx, pool, vps.VPSID, "asset_domains", archiveErr, writeErr)
	})

	t.Run("active subscription commits before archive", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-subscription-writer-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, true, func(writeCtx context.Context) error {
			_, err := subscriptionRepo.CreateSubscription(writeCtx, subscriptions.CreateInput{
				VPSID:         vps.VPSID,
				Price:         10,
				Currency:      "USD",
				BillingMonths: 1,
				Status:        subscriptions.StatusActive,
			})
			return err
		})
		if writeErr != nil || !errors.Is(archiveErr, assetlifecycle.ErrLifecycleActionBlocked) {
			t.Fatalf("subscription-first serialization = archive error %v, subscription error %v, want archive blocked after active subscription commit", archiveErr, writeErr)
		}
		assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vps.VPSID, vpsassets.LifecycleToCancel)
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from subscriptions where vps_id = $1 and status = 'active'`, vps.VPSID).Scan(&count); err != nil {
			t.Fatalf("count active subscriptions after archive race: %v", err)
		}
		if count != 1 {
			t.Fatalf("active subscriptions after writer-first race = %d, want 1", count)
		}
	})
}

func TestVPSStateRepairArchiveWinsBeforeRelationshipWriters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)
	subscriptionRepo := NewPostgresSubscriptionRepository(pool)

	t.Run("archive wins link", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-link-action-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		mi := createVPSStateRepairCancellationMI(t, ctx, miRepo, "archive-link-action-first-mi")
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, false, func(writeCtx context.Context) error {
			_, err := linkRepo.LinkMonitoringInstance(writeCtx, vps.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID})
			return err
		})
		if archiveErr != nil || !errors.Is(writeErr, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
			t.Fatalf("archive-first link serialization = archive error %v, link error %v, want archive success and rejected link", archiveErr, writeErr)
		}
		assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vps.VPSID, vpsassets.LifecycleArchived)
	})

	t.Run("archive wins service", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-service-action-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, false, func(writeCtx context.Context) error {
			_, err := serviceRepo.CreateAssetService(writeCtx, assetservices.CreateInput{
				VPSID:       vps.VPSID,
				Name:        "service after archive",
				ServiceType: assetservices.ServiceTypeWeb,
				Status:      assetservices.ServiceStatusActive,
			})
			return err
		})
		assertVPSStateRepairServiceDomainArchiveRaceResult(t, ctx, pool, vps.VPSID, "asset_services", archiveErr, writeErr)
	})

	t.Run("archive wins domain", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-domain-action-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, false, func(writeCtx context.Context) error {
			_, err := domainRepo.CreateAssetDomain(writeCtx, assetdomains.CreateInput{
				VPSID:      vps.VPSID,
				DomainName: strings.ReplaceAll(vps.VPSID, "_", "-") + ".example.test",
				Status:     assetdomains.DomainStatusActive,
			})
			return err
		})
		assertVPSStateRepairServiceDomainArchiveRaceResult(t, ctx, pool, vps.VPSID, "asset_domains", archiveErr, writeErr)
	})

	t.Run("archive wins subscription insert", func(t *testing.T) {
		vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "archive-subscription-action-first", vpsassets.LifecycleActive)
		markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, vps.VPSID)
		archiveErr, writeErr := raceVPSStateRepairArchiveWithWriter(t, ctx, pool, lifecycleRepo, vps, false, func(writeCtx context.Context) error {
			_, err := subscriptionRepo.CreateSubscription(writeCtx, subscriptions.CreateInput{
				VPSID:         vps.VPSID,
				Price:         10,
				Currency:      "USD",
				BillingMonths: 1,
				Status:        subscriptions.StatusActive,
			})
			return err
		})
		if archiveErr != nil || !errors.Is(writeErr, vpsassets.ErrVPSAssetReadonly) {
			t.Fatalf("archive-first subscription serialization = archive error %v, insert error %v, want archive success and rejected insert", archiveErr, writeErr)
		}
		assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vps.VPSID, vpsassets.LifecycleArchived)
	})
}

func TestVPSStateRepairSharedTargetArchiveSerializesAgainstNewParentDependency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)

	for _, order := range []struct {
		name        string
		writerFirst bool
	}{
		{name: "new parent dependency first", writerFirst: true},
		{name: "global Target archive first", writerFirst: false},
	} {
		t.Run(order.name, func(t *testing.T) {
			vpsA := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "target-graph-a-"+order.name, vpsassets.LifecycleActive)
			vpsB := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "target-graph-b-"+order.name, vpsassets.LifecycleActive)
			vpsC := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "target-graph-c-"+order.name, vpsassets.LifecycleActive)
			target := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "target-graph-"+strings.ReplaceAll(order.name, " ", "-"))
			if _, err := targetRepo.PauseTargetRun(ctx, target.TargetID); err != nil {
				t.Fatalf("pause shared Target before linking dependencies: %v", err)
			}
			createVPSStateRepairCancellationService(t, ctx, serviceRepo, vpsA.VPSID, target.TargetID, "target-graph-a", assetservices.ServiceStatusActive)
			createVPSStateRepairCancellationService(t, ctx, serviceRepo, vpsB.VPSID, target.TargetID, "target-graph-b", assetservices.ServiceStatusActive)
			review, err := targetRepo.GetTargetLifecycleReview(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTargetLifecycleReview before C dependency: %v", err)
			}
			if len(review.DependencyImpacts) != 2 {
				t.Fatalf("shared Target initial dependencies = %#v, want A and B", review.DependencyImpacts)
			}
			archive := func(actionCtx context.Context) error {
				_, err := targetRepo.ArchiveTarget(actionCtx, target.TargetID, assetlinks.GlobalActionConfirmation{
					PreviewDigest:       review.PreviewDigest,
					ConfirmSharedImpact: true,
				})
				return err
			}
			addC := func(writeCtx context.Context) error {
				_, err := serviceRepo.CreateAssetService(writeCtx, assetservices.CreateInput{
					VPSID:       vpsC.VPSID,
					TargetID:    new(target.TargetID),
					Name:        "target graph C dependency",
					ServiceType: assetservices.ServiceTypeWeb,
					Status:      assetservices.ServiceStatusActive,
				})
				return err
			}
			var firstErr, secondErr error
			if order.writerFirst {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, addC, archive)
				if firstErr != nil || !errors.Is(secondErr, assetlifecycle.ErrStaleCancellationPreview) {
					t.Fatalf("C-dependency-first serialization = add error %v, archive error %v, want archive stale", firstErr, secondErr)
				}
			} else {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, addC)
				if firstErr != nil || !errors.Is(secondErr, targets.ErrTargetMetadataConflict) {
					t.Fatalf("Target-archive-first serialization = archive error %v, add error %v, want active relation rejected", firstErr, secondErr)
				}
			}
			gotTarget, err := targetRepo.GetTarget(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTarget after graph race: %v", err)
			}
			var dependencies int
			if err := pool.QueryRow(ctx, `select count(*) from asset_services where target_id = $1`, target.TargetID).Scan(&dependencies); err != nil {
				t.Fatalf("count shared Target dependencies after graph race: %v", err)
			}
			if order.writerFirst {
				if gotTarget.RunStatus != targets.RunStatusPaused || dependencies != 3 {
					t.Fatalf("dependency-first result = Target %q with %d dependencies, want paused with 3", gotTarget.RunStatus, dependencies)
				}
			} else if gotTarget.RunStatus != targets.RunStatusArchived || dependencies != 2 {
				t.Fatalf("archive-first result = Target %q with %d dependencies, want archived with 2", gotTarget.RunStatus, dependencies)
			}
		})
	}
}

func TestVPSStateRepairMonitoringInstanceArchiveSerializesAgainstLinkAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)

	for _, order := range []struct {
		name         string
		archiveFirst bool
	}{
		{name: "MI archive after link attempt", archiveFirst: false},
		{name: "MI archive before link attempt", archiveFirst: true},
	} {
		t.Run(order.name, func(t *testing.T) {
			parent := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "mi-link-race-"+strings.ReplaceAll(order.name, " ", "-"), vpsassets.LifecycleActive)
			newParent := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "mi-link-race-new-"+strings.ReplaceAll(order.name, " ", "-"), vpsassets.LifecycleActive)
			mi := createVPSStateRepairCancellationMI(t, ctx, miRepo, "mi-link-race-"+strings.ReplaceAll(order.name, " ", "-"))
			if _, err := linkRepo.LinkMonitoringInstance(ctx, parent.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID}); err != nil {
				t.Fatalf("link MI to residual parent: %v", err)
			}
			cancelVPSStateRepairParent(t, ctx, lifecycleRepo, parent.VPSID)
			if _, err := miRepo.RetireMonitoringInstance(ctx, mi.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "make residual MI archivable"}); err != nil {
				t.Fatalf("retire residual MI: %v", err)
			}
			review, err := miRepo.GetMonitoringInstanceManagementReview(ctx, mi.MonitoringInstanceID)
			if err != nil {
				t.Fatalf("GetMonitoringInstanceManagementReview before archive race: %v", err)
			}
			archive := func(actionCtx context.Context) error {
				_, err := miRepo.ArchiveMonitoringInstance(actionCtx, mi.MonitoringInstanceID, monitoringinstances.ArchiveInput{
					Reason:                   "archive residual MI",
					ConfirmationName:         mi.DisplayName,
					GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{PreviewDigest: review.PreviewDigest, ConfirmSharedImpact: true},
				})
				return err
			}
			link := func(writeCtx context.Context) error {
				_, err := linkRepo.LinkMonitoringInstance(writeCtx, newParent.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID})
				return err
			}
			var archiveErr, linkErr error
			if order.archiveFirst {
				archiveErr, linkErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, link)
			} else {
				linkErr, archiveErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, link, archive)
			}
			if archiveErr != nil || !errors.Is(linkErr, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
				t.Fatalf("MI archive/link serialization = archive error %v, link error %v, want archive success and no retired/archived link", archiveErr, linkErr)
			}
			var archivedAt *time.Time
			if err := pool.QueryRow(ctx, `select archived_at from monitoring_instances where monitoring_instance_id = $1`, mi.MonitoringInstanceID).Scan(&archivedAt); err != nil {
				t.Fatalf("read MI archive state: %v", err)
			}
			if archivedAt == nil {
				t.Fatal("MI archive did not commit")
			}
			var links int
			if err := pool.QueryRow(ctx, `select count(*) from vps_monitoring_instance_links where monitoring_instance_id = $1 and unlinked_at is null`, mi.MonitoringInstanceID).Scan(&links); err != nil {
				t.Fatalf("count active MI links after archive race: %v", err)
			}
			if links != 1 {
				t.Fatalf("active MI links after archive race = %d, want only the residual historical parent link", links)
			}
		})
	}

	for _, order := range []struct {
		name              string
		cancellationFirst bool
	}{
		{name: "cancellation commits before archive", cancellationFirst: true},
		{name: "MI archive attempts before cancellation", cancellationFirst: false},
	} {
		t.Run(order.name, func(t *testing.T) {
			parent := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "mi-cancel-race-"+strings.ReplaceAll(order.name, " ", "-"), vpsassets.LifecycleActive)
			markVPSStateRepairParentToCancel(t, ctx, lifecycleRepo, parent.VPSID)
			mi := createVPSStateRepairCancellationMI(t, ctx, miRepo, "mi-cancel-race-"+strings.ReplaceAll(order.name, " ", "-"))
			if _, err := linkRepo.LinkMonitoringInstance(ctx, parent.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID}); err != nil {
				t.Fatalf("link MI before cancellation: %v", err)
			}
			if _, err := miRepo.RetireMonitoringInstance(ctx, mi.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "prepare MI archive/cancellation race"}); err != nil {
				t.Fatalf("retire residual MI before race: %v", err)
			}
			preview, err := lifecycleRepo.GetVPSCancellationPreview(ctx, parent.VPSID)
			if err != nil {
				t.Fatalf("GetVPSCancellationPreview before cancellation race: %v", err)
			}
			managementReview, err := miRepo.GetMonitoringInstanceManagementReview(ctx, mi.MonitoringInstanceID)
			if err != nil {
				t.Fatalf("GetMonitoringInstanceManagementReview before cancellation race: %v", err)
			}
			archive := func(actionCtx context.Context) error {
				_, err := miRepo.ArchiveMonitoringInstance(actionCtx, mi.MonitoringInstanceID, monitoringinstances.ArchiveInput{
					Reason:                   "archive residual MI in race",
					ConfirmationName:         mi.DisplayName,
					GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{PreviewDigest: managementReview.PreviewDigest, ConfirmSharedImpact: true},
				})
				return err
			}
			cancelVPS := func(actionCtx context.Context) error {
				_, err := lifecycleRepo.ApplyVPSCancellation(actionCtx, parent.VPSID, assetlifecycle.ApplyCancellationInput{
					Reason:             "cancel parent while MI archive is waiting",
					VPSLifecycleStatus: vpsassets.LifecycleCancelled,
					PreviewDigest:      preview.PreviewDigest,
					MonitoringInstanceActions: []assetlifecycle.MonitoringInstanceActionInput{{
						MonitoringInstanceID: mi.MonitoringInstanceID,
						LifecycleStatus:      monitoringinstances.LifecycleRetired,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
					}},
				})
				return err
			}
			var firstErr, secondErr error
			if order.cancellationFirst {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, cancelVPS, archive)
				if firstErr != nil || !errors.Is(secondErr, assetlifecycle.ErrStaleCancellationPreview) {
					t.Fatalf("cancellation-first serialization = cancellation %v, archive %v, want archive review stale after parent transition", firstErr, secondErr)
				}
			} else {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, cancelVPS)
				if !errors.Is(firstErr, monitoringinstances.ErrManagementActionBlocked) || secondErr != nil {
					t.Fatalf("MI-archive-first serialization = archive %v, cancellation %v, want archive blocked then cancellation committed", firstErr, secondErr)
				}
			}
			assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, parent.VPSID, vpsassets.LifecycleCancelled)
			managementReview, err = miRepo.GetMonitoringInstanceManagementReview(ctx, mi.MonitoringInstanceID)
			if err != nil {
				t.Fatalf("refresh MI archive review after cancellation race: %v", err)
			}
			if _, err := miRepo.ArchiveMonitoringInstance(ctx, mi.MonitoringInstanceID, monitoringinstances.ArchiveInput{
				Reason:                   "archive residual MI after fresh review",
				ConfirmationName:         mi.DisplayName,
				GlobalActionConfirmation: assetlinks.GlobalActionConfirmation{PreviewDigest: managementReview.PreviewDigest, ConfirmSharedImpact: true},
			}); err != nil {
				t.Fatalf("ArchiveMonitoringInstance after fresh residual review: %v", err)
			}
			var archivedAt *time.Time
			if err := pool.QueryRow(ctx, `select archived_at from monitoring_instances where monitoring_instance_id = $1`, mi.MonitoringInstanceID).Scan(&archivedAt); err != nil {
				t.Fatalf("read MI state after cancellation race: %v", err)
			}
			if archivedAt == nil {
				t.Fatal("MI archive did not commit after refreshed residual review")
			}
		})
	}
}

func TestVPSStateRepairUnknownToActiveCorrectionSerializesAgainstTargetArchive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)

	for _, order := range []struct {
		name            string
		correctionFirst bool
	}{
		{name: "unknown dependency corrected first", correctionFirst: true},
		{name: "Target archived before correction", correctionFirst: false},
	} {
		t.Run(order.name, func(t *testing.T) {
			parent := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "unknown-target-race-"+strings.ReplaceAll(order.name, " ", "-"), vpsassets.LifecycleActive)
			target := createVPSStateRepairCancellationTarget(t, ctx, targetRepo, "unknown-target-"+strings.ReplaceAll(order.name, " ", "-"))
			if _, err := targetRepo.PauseTargetRun(ctx, target.TargetID); err != nil {
				t.Fatalf("pause Target before dependency creation: %v", err)
			}
			service := createVPSStateRepairCancellationService(t, ctx, serviceRepo, parent.VPSID, target.TargetID, "unknown relation", assetservices.ServiceStatusUnknown)
			review, err := targetRepo.GetTargetLifecycleReview(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTargetLifecycleReview with unknown dependency: %v", err)
			}
			if len(review.DependencyImpacts) != 1 || review.DependencyImpacts[0].Classification != assetlinks.DependencyNeedsConfirmation {
				t.Fatalf("unknown dependency review = %#v, want needs-confirmation", review.DependencyImpacts)
			}
			archive := func(actionCtx context.Context) error {
				_, err := targetRepo.ArchiveTarget(actionCtx, target.TargetID, assetlinks.GlobalActionConfirmation{
					PreviewDigest:       review.PreviewDigest,
					ConfirmSharedImpact: true,
				})
				return err
			}
			correct := func(writeCtx context.Context) error {
				_, err := serviceRepo.UpdateStatus(writeCtx, service.ServiceID, assetservices.ServiceStatusActive, "correct unknown dependency")
				return err
			}
			var firstErr, secondErr error
			if order.correctionFirst {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, correct, archive)
				if firstErr != nil || !errors.Is(secondErr, assetlifecycle.ErrStaleCancellationPreview) {
					t.Fatalf("correction-first serialization = correction %v, archive %v, want archive stale", firstErr, secondErr)
				}
			} else {
				firstErr, secondErr = raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, correct)
				if firstErr != nil || !errors.Is(secondErr, targets.ErrTargetMetadataConflict) {
					t.Fatalf("Target-archive-first serialization = archive %v, correction %v, want activation rejected", firstErr, secondErr)
				}
			}
			gotTarget, err := targetRepo.GetTarget(ctx, target.TargetID)
			if err != nil {
				t.Fatalf("GetTarget after unknown dependency race: %v", err)
			}
			gotServices, err := serviceRepo.ListAssetServicesForVPS(ctx, parent.VPSID)
			if err != nil {
				t.Fatalf("ListAssetServicesForVPS after unknown dependency race: %v", err)
			}
			if len(gotServices) != 1 {
				t.Fatalf("services after unknown dependency race = %#v, want one service", gotServices)
			}
			if order.correctionFirst {
				if gotTarget.RunStatus != targets.RunStatusPaused || gotServices[0].Status != assetservices.ServiceStatusActive {
					t.Fatalf("correction-first state = Target %q, dependency %q, want paused/active", gotTarget.RunStatus, gotServices[0].Status)
				}
			} else if gotTarget.RunStatus != targets.RunStatusArchived || gotServices[0].Status != assetservices.ServiceStatusUnknown {
				t.Fatalf("archive-first state = Target %q, dependency %q, want archived/unknown", gotTarget.RunStatus, gotServices[0].Status)
			}
		})
	}
}

func TestVPSStateRepairCancellationCannotDowngradeRetiredMIPastRecoveryBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)
	miRepo := NewPostgresMonitoringInstanceRepository(pool)
	linkRepo := NewPostgresVPSMonitoringInstanceLinkRepository(pool)

	vps := createVPSStateRepairCancellationVPS(t, ctx, vpsRepo, "retired-mi-workbench-boundary", vpsassets.LifecycleActive)
	mi := createVPSStateRepairCancellationMI(t, ctx, miRepo, "retired-mi-workbench-boundary")
	if _, err := linkRepo.LinkMonitoringInstance(ctx, vps.VPSID, assetlinks.LinkInput{MonitoringInstanceID: mi.MonitoringInstanceID}); err != nil {
		t.Fatalf("link MI to parent VPS: %v", err)
	}
	if _, err := miRepo.RetireMonitoringInstance(ctx, mi.MonitoringInstanceID, monitoringinstances.LifecycleActionInput{Reason: "establish dedicated retired state"}); err != nil {
		t.Fatalf("retire MI through dedicated lifecycle action: %v", err)
	}
	preview, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vps.VPSID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview: %v", err)
	}
	_, err = lifecycleRepo.ApplyVPSCancellation(ctx, vps.VPSID, assetlifecycle.ApplyCancellationInput{
		Reason:             "must not bypass retired MI recovery",
		VPSLifecycleStatus: vpsassets.LifecycleCancelled,
		PreviewDigest:      preview.PreviewDigest,
		MonitoringInstanceActions: []assetlifecycle.MonitoringInstanceActionInput{{
			MonitoringInstanceID: mi.MonitoringInstanceID,
			LifecycleStatus:      monitoringinstances.LifecycleNoRenewal,
		}},
	})
	if !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
		t.Fatalf("workbench retired-to-no-renewal action = %v, want blocked", err)
	}
	assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vps.VPSID, vpsassets.LifecycleActive)
	got, err := miRepo.GetMonitoringInstance(ctx, mi.MonitoringInstanceID)
	if err != nil {
		t.Fatalf("GetMonitoringInstance after rejected workbench action: %v", err)
	}
	if got.LifecycleStatus != monitoringinstances.LifecycleRetired ||
		got.MonitoringStatus != monitoringinstances.MonitoringPaused ||
		got.EnrollmentTokenHash != "" || got.EnrollmentTokenIssuedAt != nil || got.EnrollmentTokenConsumedAt != nil ||
		got.SyncTokenHash != "" || got.PendingBindingFingerprint != "" || got.PendingBindingFirstSeenAt != nil ||
		got.PendingBindingLastSeenAt != nil || got.PendingBindingAttemptCount != 0 ||
		got.PendingActionID != "" || got.PendingActionCommandID != "" || got.LastAction != nil {
		t.Fatalf("rejected workbench action crossed retired MI recovery boundary: %#v", got)
	}
	assertVPSStateRepairMIEventCount(t, ctx, pool, mi.MonitoringInstanceID, incidents.EventMonitoringInstanceRetired, 1)
	assertStateChangeEventCount(t, ctx, pool, "monitoring_instance", mi.MonitoringInstanceID, string(incidents.EventMonitoringInstanceLifecycleUpdated), 0)
	assertStateChangeEventCount(t, ctx, pool, "monitoring_instance", mi.MonitoringInstanceID, string(incidents.EventMonitoringInstanceRestoredToObserving), 0)
}

func createVPSStateRepairCancellationVPS(t *testing.T, ctx context.Context, repo *PostgresVPSAssetRepository, name string, lifecycle vpsassets.LifecycleStatus) vpsassets.Record {
	t.Helper()
	record, err := repo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     name,
		LifecycleStatus: lifecycle,
		UsageStatus:     vpsassets.UsageIdle,
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset %q: %v", name, err)
	}
	return record
}

func createVPSStateRepairCancellationMI(t *testing.T, ctx context.Context, repo *PostgresMonitoringInstanceRepository, name string) monitoringinstances.Record {
	t.Helper()
	record, err := repo.CreateMonitoringInstance(ctx, monitoringinstances.CreateInput{
		DisplayName:     name,
		Region:          "ap-northeast-1",
		City:            "Tokyo",
		Provider:        "repair-test",
		LifecycleStatus: monitoringinstances.LifecycleInUse,
		Labels:          []string{},
	})
	if err != nil {
		t.Fatalf("CreateMonitoringInstance %q: %v", name, err)
	}
	return record
}

func createVPSStateRepairCancellationTarget(t *testing.T, ctx context.Context, repo *PostgresTargetRepository, name string) targets.TargetRecord {
	t.Helper()
	record, err := repo.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              name,
		TargetType:                        targets.TargetTypeService,
		Host:                              strings.ReplaceAll(name, "_", "-") + ".example.test",
		ExecutionMonitoringInstanceLabels: []string{"edge"},
		RunStatus:                         targets.RunStatusEnabled,
		Labels:                            []string{},
	})
	if err != nil {
		t.Fatalf("CreateTarget %q: %v", name, err)
	}
	return record
}

func createVPSStateRepairCancellationService(t *testing.T, ctx context.Context, repo *PostgresAssetServiceRepository, vpsID, targetID, name string, status assetservices.ServiceStatus) assetservices.Record {
	t.Helper()
	record, err := repo.CreateAssetService(ctx, assetservices.CreateInput{
		VPSID:       vpsID,
		TargetID:    new(targetID),
		Name:        name,
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      status,
	})
	if err != nil {
		t.Fatalf("CreateAssetService %q: %v", name, err)
	}
	return record
}

func cancelVPSStateRepairParent(t *testing.T, ctx context.Context, repo *PostgresAssetLifecycleRepository, vpsID string) {
	t.Helper()
	preview, err := repo.GetVPSCancellationPreview(ctx, vpsID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview for %s: %v", vpsID, err)
	}
	if _, err := repo.ApplyVPSCancellation(ctx, vpsID, cancellationInputForStateRepairDigest(preview.PreviewDigest)); err != nil {
		t.Fatalf("ApplyVPSCancellation for %s: %v", vpsID, err)
	}
}
func markVPSStateRepairParentToCancel(t *testing.T, ctx context.Context, repo *PostgresAssetLifecycleRepository, vpsID string) {
	t.Helper()
	preview, err := repo.GetVPSCancellationPreview(ctx, vpsID)
	if err != nil {
		t.Fatalf("GetVPSCancellationPreview before to-cancel transition for %s: %v", vpsID, err)
	}
	_, err = repo.ApplyVPSCancellation(ctx, vpsID, assetlifecycle.ApplyCancellationInput{
		Reason:             "prepare archive concurrency case",
		VPSLifecycleStatus: vpsassets.LifecycleToCancel,
		PreviewDigest:      preview.PreviewDigest,
	})
	if err != nil {
		t.Fatalf("ApplyVPSCancellation to-cancel for %s: %v", vpsID, err)
	}
}

func cancellationInputForStateRepairDigest(digest string) assetlifecycle.ApplyCancellationInput {
	return assetlifecycle.ApplyCancellationInput{
		Reason:             "confirm cancellation fixture",
		VPSLifecycleStatus: vpsassets.LifecycleCancelled,
		PreviewDigest:      digest,
	}
}

func assertVPSStateRepairCancellationVPSLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, want vpsassets.LifecycleStatus) {
	t.Helper()
	var got vpsassets.LifecycleStatus
	if err := pool.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id = $1`, vpsID).Scan(&got); err != nil {
		t.Fatalf("read VPS %s lifecycle: %v", vpsID, err)
	}
	if got != want {
		t.Fatalf("VPS %s lifecycle = %q, want %q", vpsID, got, want)
	}
}

func assertVPSStateRepairCancellationImpactClasses(t *testing.T, impacts []assetlinks.DependencyImpact, objectType, objectID string, want map[string]string) {
	t.Helper()
	got := make(map[string]string)
	for _, impact := range impacts {
		if impact.ObjectType != objectType || impact.ObjectID != objectID {
			continue
		}
		if prior, ok := got[impact.VPSID]; ok && prior != impact.Classification {
			t.Fatalf("dependency %s for VPS %s has inconsistent classifications %q and %q", objectID, impact.VPSID, prior, impact.Classification)
		}
		got[impact.VPSID] = impact.Classification
	}
	if len(got) != len(want) {
		t.Fatalf("dependency classifications for %s/%s = %#v, want %#v", objectType, objectID, got, want)
	}
	for vpsID, classification := range want {
		if got[vpsID] != classification {
			t.Fatalf("dependency %s/%s for VPS %s classified %q, want %q", objectType, objectID, vpsID, got[vpsID], classification)
		}
	}
}

func assertVPSStateRepairMIReconciliationPayload(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoringInstanceID string) {
	t.Helper()
	var payload map[string]any
	if err := pool.QueryRow(ctx, `
		select payload from state_change_events
		where object_type = 'monitoring_instance' and object_id = $1 and event_type = $2`,
		monitoringInstanceID, string(incidents.EventMonitoringInstanceRetirementReconciled)).Scan(&payload); err != nil {
		t.Fatalf("read retirement reconciliation payload: %v", err)
	}
	for key, want := range map[string]any{
		"prior_state":     monitoringinstances.LifecycleRetired,
		"resulting_state": monitoringinstances.LifecycleRetired,
		"retirement_monitoring_status_reconciled":   true,
		"retirement_binding_status_reconciled":      true,
		"retirement_enrollment_credentials_revoked": true,
		"retirement_sync_credential_revoked":        true,
		"retirement_pending_binding_cleared":        true,
		"retirement_pending_action_cleared":         true,
	} {
		if payload[key] != want {
			t.Fatalf("retirement reconciliation payload %s = %#v, want %#v (payload %#v)", key, payload[key], want, payload)
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal retirement reconciliation payload: %v", err)
	}
	if strings.Contains(string(encoded), "shared-cancellation-enrollment-token") || strings.Contains(string(encoded), "fingerprint-pending") {
		t.Fatalf("retirement reconciliation event exposed secret state: %s", encoded)
	}
}

func assertStateChangeEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, objectType, objectID, eventType string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `
		select count(*) from state_change_events
		where object_type = $1 and object_id = $2 and event_type = $3`, objectType, objectID, eventType).Scan(&got); err != nil {
		t.Fatalf("count state change event %s/%s/%s: %v", objectType, objectID, eventType, err)
	}
	if got != want {
		t.Fatalf("state change event count for %s/%s/%s = %d, want %d", objectType, objectID, eventType, got, want)
	}
}

func raceVPSStateRepairArchiveWithWriter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, lifecycleRepo *PostgresAssetLifecycleRepository, vps vpsassets.Record, writerFirst bool, writer func(context.Context) error) (error, error) {
	t.Helper()
	archive := func(actionCtx context.Context) error {
		_, err := lifecycleRepo.ApplyVPSArchive(actionCtx, vps.VPSID, assetlifecycle.ApplyArchiveInput{
			ConfirmationName: vps.DisplayName,
			Reason:           "serialize archive with graph writer",
		})
		return err
	}
	if writerFirst {
		writeErr, archiveErr := raceVPSStateRepairOperationsInOrder(t, ctx, pool, writer, archive)
		return archiveErr, writeErr
	}
	archiveErr, writeErr := raceVPSStateRepairOperationsInOrder(t, ctx, pool, archive, writer)
	return archiveErr, writeErr
}

func raceVPSStateRepairOperationsInOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, first, second func(context.Context) error) (error, error) {
	t.Helper()
	holder, err := beginAssetGraphTx(ctx, pool.BeginTx)
	if err != nil {
		t.Fatalf("begin ordered lifecycle graph lock: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- first(ctx) }()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 1); err != nil {
		t.Fatalf("first lifecycle operation did not wait for graph lock: %v", err)
	}
	go func() { secondDone <- second(ctx) }()
	if err := waitForBlockedLifecycleSessions(ctx, pool, 2); err != nil {
		t.Fatalf("second lifecycle operation did not wait for graph lock: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release ordered lifecycle graph lock: %v", err)
	}
	return <-firstDone, <-secondDone
}

func assertVPSStateRepairServiceDomainArchiveRaceResult(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID, table string, archiveErr, writeErr error) {
	t.Helper()
	if archiveErr != nil {
		t.Fatalf("archive vs %s create failed: %v", table, archiveErr)
	}
	var lifecycle vpsassets.LifecycleStatus
	if err := pool.QueryRow(ctx, `select lifecycle_status from vps_assets where vps_id = $1`, vpsID).Scan(&lifecycle); err != nil {
		t.Fatalf("read raced %s parent lifecycle: %v", table, err)
	}
	var related int
	if err := pool.QueryRow(ctx, `select count(*) from `+table+` where vps_id = $1`, vpsID).Scan(&related); err != nil {
		t.Fatalf("count raced %s rows: %v", table, err)
	}
	switch {
	case writeErr == nil:
		if lifecycle != vpsassets.LifecycleArchived || related != 1 {
			t.Fatalf("writer-first %s result = lifecycle %q, relations %d, want archived parent with retained relation", table, lifecycle, related)
		}
	case errors.Is(writeErr, vpsassets.ErrVPSAssetReadonly):
		if lifecycle != vpsassets.LifecycleArchived || related != 0 {
			t.Fatalf("archive-first %s result = lifecycle %q, relations %d, want archived parent without relation", table, lifecycle, related)
		}
	default:
		t.Fatalf("%s race writer error = %v, want success or terminal-parent conflict", table, writeErr)
	}

}
