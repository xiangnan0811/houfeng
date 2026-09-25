package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairHistoricalAutoRenewRemainsActionable(t *testing.T) {
	impacts := buildSubscriptionImpacts([]subscriptions.Record{{SubscriptionID: "historical", Status: subscriptions.StatusCancelled, AutoRenew: true, RenewalMode: string(subscriptions.RenewalModeAuto)}})
	if impacts[0].Role != "attention" || impacts[0].RecommendedAction != "cancel_auto_renew_and_mark_cancelled" {
		t.Fatalf("contradictory automatic renewal must remain an explicit cancellation candidate: %#v", impacts[0])
	}
}

func TestVPSStateRepairSourcePreservedThroughBillingLinkages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	repo := NewPostgresAssetLifecycleRepository(pool)
	for _, mode := range []subscriptions.RenewalMode{subscriptions.RenewalModeAuto, subscriptions.RenewalModeManual, subscriptions.RenewalModeAutoCancelled, subscriptions.RenewalModeGift, subscriptions.RenewalModeLottery, subscriptions.RenewalModeBonus, subscriptions.RenewalModeOther} {
		for _, path := range []string{"workbench", "single-active"} {
			t.Run(string(mode)+"/"+path, func(t *testing.T) {
				vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: string(mode) + path, LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse})
				if err != nil {
					t.Fatal(err)
				}
				sub, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{VPSID: vps.VPSID, Price: 0, Currency: "USD", BillingMonths: 1, Status: subscriptions.StatusActive, RenewalMode: string(mode)})
				if err != nil {
					t.Fatal(err)
				}
				if path == "workbench" {
					preview, err := repo.GetVPSCancellationPreview(ctx, vps.VPSID)
					if err != nil {
						t.Fatal(err)
					}
					_, err = repo.ApplyVPSCancellation(ctx, vps.VPSID, assetlifecycle.ApplyCancellationInput{Reason: "end source entitlement", VPSLifecycleStatus: vpsassets.LifecycleToCancel, SubscriptionIDs: []string{sub.SubscriptionID}, PreviewDigest: preview.PreviewDigest})
					if err != nil {
						t.Fatal(err)
					}
				} else {
					_, _, err = vpsRepo.PatchVPSAssetWithSubscriptionRenewalLinkage(ctx, vps.VPSID, vpsassets.PatchInput{RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalCancel)})
					if err != nil {
						t.Fatal(err)
					}
				}
				got, err := subRepo.GetSubscription(ctx, sub.SubscriptionID)
				if err != nil {
					t.Fatal(err)
				}
				wantMode, wantCancelled := string(mode), false
				if mode == subscriptions.RenewalModeAuto || mode == subscriptions.RenewalModeManual || mode == subscriptions.RenewalModeAutoCancelled {
					wantMode, wantCancelled = string(subscriptions.RenewalModeAutoCancelled), true
				}
				if got.RenewalMode != wantMode || got.AutoRenew || got.AutoRenewCancelled != wantCancelled {
					t.Fatalf("renewal facts after %s: mode=%q auto=%t cancelled=%t; want mode=%q auto=false cancelled=%t", path, got.RenewalMode, got.AutoRenew, got.AutoRenewCancelled, wantMode, wantCancelled)
				}
				wantStatus := subscriptions.StatusActive
				if path == "workbench" {
					wantStatus = subscriptions.StatusCancelled
				}
				if got.Status != wantStatus {
					t.Fatalf("status = %q, want %q", got.Status, wantStatus)
				}
			})
		}
	}
}
