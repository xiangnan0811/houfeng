package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/assetdecisions"
	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairAssetDecisionReadbackUsesEffectiveReferences(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	domainRepo := NewPostgresAssetDomainRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	decisionRepo := NewPostgresAssetDecisionRepository(pool)

	createVPS := func(name string) string {
		t.Helper()
		record, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
			DisplayName:     name,
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageStatus:     vpsassets.UsageIdle,
			RenewalDecision: vpsassets.RenewalMigrate,
		})
		if err != nil {
			t.Fatalf("create VPS %q: %v", name, err)
		}
		return record.VPSID
	}

	targetSequence := 0
	createTarget := func() string {
		t.Helper()
		targetSequence++
		record, err := targetRepo.CreateTarget(ctx, targets.CreateTargetInput{
			Name:                              fmt.Sprintf("Readback target %d", targetSequence),
			TargetType:                        targets.TargetTypeService,
			Host:                              fmt.Sprintf("readback-%d.example.test", targetSequence),
			ExecutionMonitoringInstanceLabels: []string{},
			Labels:                            []string{},
			RunStatus:                         targets.RunStatusEnabled,
		})
		if err != nil {
			t.Fatalf("create target %d: %v", targetSequence, err)
		}
		return record.TargetID
	}

	serviceSequence := 0
	addService := func(vpsID, targetID string, status assetservices.ServiceStatus) {
		t.Helper()
		serviceSequence++
		_, err := serviceRepo.CreateAssetService(ctx, assetservices.CreateInput{
			VPSID:       vpsID,
			TargetID:    &targetID,
			Name:        fmt.Sprintf("readback service %d", serviceSequence),
			ServiceType: assetservices.ServiceTypeWeb,
			Status:      status,
		})
		if err != nil {
			t.Fatalf("create service %d with status %q: %v", serviceSequence, status, err)
		}
	}

	domainSequence := 0
	addDomain := func(vpsID, targetID string, status assetdomains.DomainStatus) {
		t.Helper()
		domainSequence++
		_, err := domainRepo.CreateAssetDomain(ctx, assetdomains.CreateInput{
			VPSID:      vpsID,
			TargetID:   &targetID,
			DomainName: fmt.Sprintf("readback-%d.example.test", domainSequence),
			Status:     status,
		})
		if err != nil {
			t.Fatalf("create domain %d with status %q: %v", domainSequence, status, err)
		}
	}

	updateTargetStatus := func(targetID, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `update targets set run_status = $2 where target_id = $1`, targetID, status); err != nil {
			t.Fatalf("set target %q status to %q: %v", targetID, status, err)
		}
	}

	historyVPS := createVPS("Migration readback historical dependencies")
	historyTarget := createTarget()
	addService(historyVPS, historyTarget, assetservices.ServiceStatusRetired)
	addService(historyVPS, historyTarget, assetservices.ServiceStatusPaused)
	addDomain(historyVPS, historyTarget, assetdomains.DomainStatusRetired)
	addDomain(historyVPS, historyTarget, assetdomains.DomainStatusPaused)

	activeVPS := createVPS("Migration readback active dependencies")
	enabledTarget := createTarget()
	maintenanceTarget := createTarget()
	pausedTarget := createTarget()
	archivedTarget := createTarget()
	addService(activeVPS, enabledTarget, assetservices.ServiceStatusActive)
	addService(activeVPS, maintenanceTarget, assetservices.ServiceStatusActive)
	addService(activeVPS, pausedTarget, assetservices.ServiceStatusActive)
	addDomain(activeVPS, enabledTarget, assetdomains.DomainStatusActive)
	addDomain(activeVPS, archivedTarget, assetdomains.DomainStatusActive)
	updateTargetStatus(maintenanceTarget, targets.RunStatusMaintenance)
	updateTargetStatus(pausedTarget, targets.RunStatusPaused)
	updateTargetStatus(archivedTarget, targets.RunStatusArchived)

	unknownVPS := createVPS("Migration readback unknown dependencies")
	unknownTarget := createTarget()
	addService(unknownVPS, unknownTarget, assetservices.ServiceStatusUnknown)
	addDomain(unknownVPS, unknownTarget, assetdomains.DomainStatusUnknown)

	archivedVPS := createVPS("Migration readback archived parent")
	addService(archivedVPS, historyTarget, assetservices.ServiceStatusActive)
	addDomain(archivedVPS, historyTarget, assetdomains.DomainStatusUnknown)
	if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status = 'archived', usage_status = 'unknown', archived_at = now() where vps_id = $1`, archivedVPS); err != nil {
		t.Fatalf("archive parent VPS %q: %v", archivedVPS, err)
	}

	if _, err := pool.Exec(ctx, `
		update vps_assets
		set archived_state_snapshot = jsonb_build_object(
			'lifecycle_status', 'cancelled',
			'usage_status', 'idle',
			'renewal_decision', 'cancel',
			'captured_at', now(),
			'source', 'archive'
		)
		where vps_id = $1`, historyVPS); err != nil {
		t.Fatalf("set historical archive snapshot on active VPS: %v", err)
	}
	facts, err := decisionRepo.loadFacts(ctx)
	if err != nil {
		t.Fatalf("load migration readback facts: %v", err)
	}
	factsByVPS := assetdecisions.FactsByVPSID(facts)
	factFor := func(vpsID string) assetdecisions.Fact {
		t.Helper()
		fact, ok := factsByVPS[vpsID]
		if !ok {
			t.Fatalf("VPS %q missing from migration facts", vpsID)
		}
		return fact
	}
	migrationMember := func(vpsID string) assetdecisions.RecordMember {
		return assetdecisions.RecordMember{
			VPSID:          vpsID,
			DecidedAction:  assetdecisions.ActionMigrate,
			FollowupStatus: assetdecisions.FollowupDone,
		}
	}

	historyFact := factFor(historyVPS)
	if historyFact.ServiceCount != 2 || historyFact.DomainCount != 2 ||
		historyFact.EffectiveServiceCount != 0 || historyFact.EffectiveDomainCount != 0 ||
		historyFact.UnknownServiceCount != 0 || historyFact.UnknownDomainCount != 0 || historyFact.RunningTargetCount != 0 {
		t.Fatalf("historical-only facts = (service %d/%d, domain %d/%d, unknown %d/%d, running target %d), want historical counts retained without effective carriers",
			historyFact.ServiceCount, historyFact.EffectiveServiceCount,
			historyFact.DomainCount, historyFact.EffectiveDomainCount,
			historyFact.UnknownServiceCount, historyFact.UnknownDomainCount,
			historyFact.RunningTargetCount)
	}
	if snapshot := historyFact.VPS.ArchivedStateSnapshot; snapshot == nil ||
		snapshot.LifecycleStatus != vpsassets.LifecycleCancelled || snapshot.Source != "archive" {
		t.Fatalf("archived state snapshot = %#v, want scanned prior archive state", snapshot)
	}
	historyReadback := assetdecisions.EvaluateMemberExecutionReadback(migrationMember(historyVPS), factsByVPS)
	if historyReadback.Status != assetdecisions.ReadbackAligned {
		t.Fatalf("historical-only readback = %#v, want aligned", historyReadback)
	}
	if historyReadback.CurrentFacts.ServiceCount != 2 || historyReadback.CurrentFacts.DomainCount != 2 ||
		historyReadback.CurrentFacts.EffectiveServiceCount != 0 || historyReadback.CurrentFacts.EffectiveDomainCount != 0 {
		t.Fatalf("historical readback facts = %#v, want historical counts plus zero effective counts", historyReadback.CurrentFacts)
	}

	activeFact := factFor(activeVPS)
	if activeFact.ServiceCount != 3 || activeFact.DomainCount != 2 ||
		activeFact.EffectiveServiceCount != 3 || activeFact.EffectiveDomainCount != 2 ||
		activeFact.TargetCount != 4 || activeFact.RunningTargetCount != 2 {
		t.Fatalf("active facts = (service %d/%d, domain %d/%d, target %d/%d), want effective refs and enabled/maintenance targets only",
			activeFact.ServiceCount, activeFact.EffectiveServiceCount,
			activeFact.DomainCount, activeFact.EffectiveDomainCount,
			activeFact.TargetCount, activeFact.RunningTargetCount)
	}
	activeReadback := assetdecisions.EvaluateMemberExecutionReadback(migrationMember(activeVPS), factsByVPS)
	if activeReadback.Status != assetdecisions.ReadbackDrift || !hasStoredReadbackIssue(activeReadback, "old_carrier_remaining", "critical") {
		t.Fatalf("active readback = %#v, want critical old-carrier drift", activeReadback)
	}

	unknownFact := factFor(unknownVPS)
	if unknownFact.ServiceCount != 1 || unknownFact.DomainCount != 1 ||
		unknownFact.EffectiveServiceCount != 0 || unknownFact.EffectiveDomainCount != 0 ||
		unknownFact.UnknownServiceCount != 1 || unknownFact.UnknownDomainCount != 1 || unknownFact.RunningTargetCount != 0 {
		t.Fatalf("unknown facts = (service %d/%d/%d, domain %d/%d/%d, running target %d), want confirmation-only relations",
			unknownFact.ServiceCount, unknownFact.EffectiveServiceCount, unknownFact.UnknownServiceCount,
			unknownFact.DomainCount, unknownFact.EffectiveDomainCount, unknownFact.UnknownDomainCount,
			unknownFact.RunningTargetCount)
	}
	unknownReadback := assetdecisions.EvaluateMemberExecutionReadback(migrationMember(unknownVPS), factsByVPS)
	if unknownReadback.Status != assetdecisions.ReadbackDrift ||
		hasStoredReadbackIssue(unknownReadback, "old_carrier_remaining", "critical") ||
		!hasStoredReadbackIssue(unknownReadback, "carrier_needs_confirmation", "warning") {
		t.Fatalf("unknown readback = %#v, want warning that prevents completed status without known-carrier critical", unknownReadback)
	}

	if _, ok := factsByVPS[archivedVPS]; ok {
		t.Fatalf("archived parent VPS %q appeared in migration facts", archivedVPS)
	}
}

func hasStoredReadbackIssue(readback assetdecisions.MemberExecutionReadback, kind, tone string) bool {
	for _, issue := range readback.Issues {
		if issue.Kind == kind && issue.Tone == tone {
			return true
		}
	}
	return false
}
