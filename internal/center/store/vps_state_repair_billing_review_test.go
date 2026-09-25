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

func TestVPSStateRepairWorkbenchNormalizesSelectedHistoricalBillingFlags(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	lifecycleRepo := NewPostgresAssetLifecycleRepository(pool)

	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{
		DisplayName:     "Selected historical billing facts",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	})
	if err != nil {
		t.Fatalf("create VPS: %v", err)
	}

	type subscriptionCase struct {
		name     string
		mode     subscriptions.RenewalMode
		status   subscriptions.Status
		selected bool
	}
	sourceCases := []subscriptionCase{
		{name: "gift selected cancelled", mode: subscriptions.RenewalModeGift, status: subscriptions.StatusCancelled, selected: true},
		{name: "gift unselected expired", mode: subscriptions.RenewalModeGift, status: subscriptions.StatusExpired},
		{name: "lottery selected expired", mode: subscriptions.RenewalModeLottery, status: subscriptions.StatusExpired, selected: true},
		{name: "lottery unselected cancelled", mode: subscriptions.RenewalModeLottery, status: subscriptions.StatusCancelled},
		{name: "bonus selected cancelled", mode: subscriptions.RenewalModeBonus, status: subscriptions.StatusCancelled, selected: true},
		{name: "bonus unselected expired", mode: subscriptions.RenewalModeBonus, status: subscriptions.StatusExpired},
		{name: "other selected expired", mode: subscriptions.RenewalModeOther, status: subscriptions.StatusExpired, selected: true},
		{name: "other unselected cancelled", mode: subscriptions.RenewalModeOther, status: subscriptions.StatusCancelled},
		{name: "automatic renewal selected cancelled", mode: subscriptions.RenewalModeAuto, status: subscriptions.StatusCancelled, selected: true},
		{name: "automatic renewal unselected expired", mode: subscriptions.RenewalModeAuto, status: subscriptions.StatusExpired},
	}
	selectedIDs := make([]string, 0, len(sourceCases)/2+1)
	created := make(map[string]subscriptionCase, len(sourceCases))
	for _, tc := range sourceCases {
		record, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{
			VPSID:         vps.VPSID,
			Price:         10,
			Currency:      "USD",
			BillingMonths: 1,
			Status:        tc.status,
			RenewalMode:   string(tc.mode),
			DisplayName:   tc.name,
		})
		if err != nil {
			t.Fatalf("create %s subscription: %v", tc.name, err)
		}
		created[record.SubscriptionID] = tc
		if tc.mode != subscriptions.RenewalModeAuto {
			// Preserve the explicit source mode while seeding the legacy contradictory
			// flags found in persisted records from before source normalization.
			if _, err := pool.Exec(ctx, `update subscriptions set auto_renew = false, auto_renew_cancelled = true where subscription_id = $1`, record.SubscriptionID); err != nil {
				t.Fatalf("seed contradictory flags for %s: %v", tc.name, err)
			}
		}
		if tc.selected {
			selectedIDs = append(selectedIDs, record.SubscriptionID)
		}
	}

	preview, err := lifecycleRepo.GetVPSCancellationPreview(ctx, vps.VPSID)
	if err != nil {
		t.Fatalf("get cancellation preview: %v", err)
	}
	if _, err := lifecycleRepo.ApplyVPSCancellation(ctx, vps.VPSID, assetlifecycle.ApplyCancellationInput{
		Reason:             "normalize selected historical billing facts",
		VPSLifecycleStatus: vpsassets.LifecycleToCancel,
		SubscriptionIDs:    selectedIDs,
		PreviewDigest:      preview.PreviewDigest,
	}); err != nil {
		t.Fatalf("apply selected cancellation: %v", err)
	}

	for id, tc := range created {
		got, err := subRepo.GetSubscription(ctx, id)
		if err != nil {
			t.Fatalf("read %s subscription: %v", tc.name, err)
		}
		if got.Status != tc.status {
			t.Errorf("%s status = %q, want historical status %q", tc.name, got.Status, tc.status)
		}
		if tc.mode == subscriptions.RenewalModeAuto {
			if tc.selected {
				if got.RenewalMode != string(subscriptions.RenewalModeAutoCancelled) || got.AutoRenew || !got.AutoRenewCancelled {
					t.Errorf("selected historical automatic renewal facts = mode %q, auto_renew=%t, auto_renew_cancelled=%t; want auto_cancelled,false,true", got.RenewalMode, got.AutoRenew, got.AutoRenewCancelled)
				}
			} else if got.RenewalMode != string(tc.mode) || !got.AutoRenew || got.AutoRenewCancelled {
				t.Errorf("unselected historical automatic renewal changed: mode %q, auto_renew=%t, auto_renew_cancelled=%t", got.RenewalMode, got.AutoRenew, got.AutoRenewCancelled)
			}
			continue
		}
		if got.RenewalMode != string(tc.mode) {
			t.Errorf("%s source mode = %q, want retained mode %q", tc.name, got.RenewalMode, tc.mode)
		}
		if tc.selected {
			if got.AutoRenew || got.AutoRenewCancelled {
				t.Errorf("selected %s source flags = auto_renew=%t, auto_renew_cancelled=%t; want false,false", tc.name, got.AutoRenew, got.AutoRenewCancelled)
			}
		} else if got.AutoRenew || !got.AutoRenewCancelled {
			t.Errorf("unselected %s source flags changed: auto_renew=%t, auto_renew_cancelled=%t; want original false,true", tc.name, got.AutoRenew, got.AutoRenewCancelled)
		}
	}
}

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
			UsageStatus:     vpsassets.UsageIdle,
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
	pending := createLinkedVPS("Pending cancellation VPS")
	noRenewalDecision := createLinkedVPS("No-renewal decision VPS")
	cancelled := createLinkedVPS("Cancelled VPS")
	archived := createLinkedVPS("Archived VPS")
	for _, vps := range []vpsassets.Record{active, pending, noRenewalDecision} {
		seedHistoricalSubscriptions(vps.VPSID)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set lifecycle_status = 'to_cancel', renewal_decision = 'cancel'
		where vps_id = $1`, pending.VPSID); err != nil {
		t.Fatalf("seed pending cancellation lifecycle: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set renewal_decision = 'cancel'
		where vps_id = $1`, noRenewalDecision.VPSID); err != nil {
		t.Fatalf("seed no-renewal decision: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		update vps_assets
		set lifecycle_status = 'cancelled', renewal_decision = 'cancel'
		where vps_id = $1`, cancelled.VPSID); err != nil {
		t.Fatalf("seed cancelled lifecycle: %v", err)
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
