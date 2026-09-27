package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairTargetContextsExposeHistoricalAutoRenewContradictions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	targetRepo := NewPostgresTargetRepository(pool)
	serviceRepo := NewPostgresAssetServiceRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)

	target, err := targetRepo.CreateTarget(ctx, targets.CreateTargetInput{
		Name:                              "Historical renewal context",
		TargetType:                        targets.TargetTypeService,
		Host:                              "renewal-context.example.test",
		ExecutionMonitoringInstanceLabels: []string{},
		RunStatus:                         targets.RunStatusEnabled,
		Labels:                            []string{},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	targetID := target.TargetID
	createLinkedVPS := func(name string) vpsassets.Record {
		t.Helper()
		record, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
			DisplayName:     name,
			LifecycleStatus: vpsassets.LifecycleActive,
			UsageTags:       []string{"闲置"},
		})
		if err != nil {
			t.Fatalf("create %s VPS: %v", name, err)
		}
		if _, err := serviceRepo.CreateAssetService(ctx, assetservices.CreateInput{
			VPSID:    record.VPSID,
			TargetID: &targetID,
			Name:     name + " service",
		}); err != nil {
			t.Fatalf("create %s service: %v", name, err)
		}
		return record
	}
	seedHistoricalSubscriptions := func(vpsID string) {
		t.Helper()
		for _, tc := range []struct {
			name   string
			status subscriptions.Status
			mode   subscriptions.RenewalMode
		}{
			{name: "highest-ranked expired record", status: subscriptions.StatusExpired, mode: subscriptions.RenewalModeManual},
			{name: "lower-ranked cancelled record with active auto renewal", status: subscriptions.StatusCancelled, mode: subscriptions.RenewalModeAuto},
		} {
			if _, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{
				VPSID:         vpsID,
				Price:         10,
				Currency:      "USD",
				BillingMonths: 1,
				Status:        tc.status,
				RenewalMode:   string(tc.mode),
				DisplayName:   tc.name,
			}); err != nil {
				t.Fatalf("create %s subscription for %s: %v", tc.name, vpsID, err)
			}
		}
	}

	active := createLinkedVPS("Active VPS")
	pending := createLinkedVPS("Keep renewal intention VPS")
	noRenewalDecision := createLinkedVPS("No-renewal decision VPS")
	cancelled := createLinkedVPS("Archived keep intention VPS")
	archived := createLinkedVPS("Archived VPS")
	for _, vps := range []vpsassets.Record{active, pending, noRenewalDecision} {
		seedHistoricalSubscriptions(vps.VPSID)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set renewal_decision = 'keep'
		where vps_id = $1`, pending.VPSID); err != nil {
		t.Fatalf("seed independent keep renewal intention: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set renewal_decision = 'cancel'
		where vps_id = $1`, noRenewalDecision.VPSID); err != nil {
		t.Fatalf("seed no-renewal decision: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set lifecycle_status = 'archived', renewal_decision = 'keep', archived_at = now()
		where vps_id = $1`, cancelled.VPSID); err != nil {
		t.Fatalf("seed archived lifecycle with keep intention: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set lifecycle_status = 'archived', usage_status = 'unknown', renewal_decision = 'cancel', archived_at = now()
		where vps_id = $1`, archived.VPSID); err != nil {
		t.Fatalf("seed archived lifecycle: %v", err)
	}

	contexts, err := lifecycleRepo.ListTargetAssetContexts(ctx)
	if err != nil {
		t.Fatalf("list target asset contexts: %v", err)
	}
	if len(contexts) != 1 || contexts[0].TargetID != target.TargetID {
		t.Fatalf("target contexts = %#v, want the one linked target", contexts)
	}
	contextForTarget := contexts[0]
	if !contextForTarget.CancellationAttention {
		t.Fatal("historical auto-renew contradictions must surface target attention")
	}
	if contextForTarget.LinkedVPSCount != 3 || len(contextForTarget.Summaries) != 3 {
		t.Fatalf("linked VPS count = %d and summaries = %d; want only three non-terminal VPS contexts", contextForTarget.LinkedVPSCount, len(contextForTarget.Summaries))
	}
	summaries := make(map[string]assetlifecycle.LinkedVPSContext, len(contextForTarget.Summaries))
	for _, summary := range contextForTarget.Summaries {
		summaries[summary.VPSID] = summary
	}
	for _, vps := range []vpsassets.Record{cancelled, archived} {
		if _, exists := summaries[vps.VPSID]; exists {
			t.Errorf("terminal VPS %s leaked into current target contexts", vps.VPSID)
		}
	}
	for _, vps := range []vpsassets.Record{active, pending, noRenewalDecision} {
		summary, exists := summaries[vps.VPSID]
		if !exists {
			t.Errorf("current VPS %s is missing from target contexts", vps.VPSID)
			continue
		}
		if vps.VPSID == active.VPSID && summary.SubscriptionState != string(subscriptions.StatusExpired) {
			t.Errorf("selected subscription state = %q, want expired so the lower-ranked cancelled record is the renewal contradiction", summary.SubscriptionState)
		}
		assertAutoRenewContradictionIsVisible(t, summary)
	}
}

func assertAutoRenewContradictionIsVisible(t *testing.T, summary assetlifecycle.LinkedVPSContext) {
	t.Helper()
	message := strings.ToLower(summary.Message)
	exposesAutoRenew := strings.Contains(message, "auto_renew=true") ||
		strings.Contains(message, "auto renew=true") ||
		(strings.Contains(summary.Message, "自动续费") &&
			(strings.Contains(message, "true") || strings.Contains(summary.Message, "开启") || strings.Contains(summary.Message, "启用") || strings.Contains(summary.Message, "仍")))
	if !exposesAutoRenew {
		t.Errorf("summary for VPS %s does not expose an enabled automatic-renewal fact: %q", summary.VPSID, summary.Message)
	}
	for _, stoppedClaim := range []string{
		"已无续费动作",
		"没有续费动作",
		"续费已停止",
		"自动续费已停止",
		"auto_renew=false",
		"auto renew=false",
	} {
		if strings.Contains(message, strings.ToLower(stoppedClaim)) {
			t.Errorf("summary for VPS %s falsely claims renewal stopped despite historical auto_renew=true: %q", summary.VPSID, summary.Message)
			break
		}
	}
}
