package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairCancellationOnlyChangesSelectedSubscriptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	lifecycle := NewPostgresAssetLifecycleRepository(pool)
	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "Explicit subscription selection", LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse})
	if err != nil {
		t.Fatal(err)
	}
	records := make([]subscriptions.Record, 3)
	for i := range records {
		records[i], err = subRepo.CreateSubscription(ctx, subscriptions.CreateInput{VPSID: vps.VPSID, Price: 10, Currency: "USD", BillingMonths: 1, Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeAuto)})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, linkage, err := vpsRepo.PatchVPSAssetWithSubscriptionRenewalLinkage(ctx, vps.VPSID, vpsassets.PatchInput{RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalCancel)})
	if err != nil {
		t.Fatal(err)
	}
	if linkage.Status != vpsassets.RenewalSubscriptionLinkageMultipleActiveSubscription || linkage.Updated {
		t.Fatalf("ambiguous linkage = %#v", linkage)
	}
	apply := func(ids []string) {
		t.Helper()
		preview, err := lifecycle.GetVPSCancellationPreview(ctx, vps.VPSID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = lifecycle.ApplyVPSCancellation(ctx, vps.VPSID, assetlifecycle.ApplyCancellationInput{Reason: "explicit billing scope", VPSLifecycleStatus: vpsassets.LifecycleToCancel, SubscriptionIDs: ids, PreviewDigest: preview.PreviewDigest})
		if err != nil {
			t.Fatal(err)
		}
	}
	apply(nil)
	for _, record := range records {
		got, err := subRepo.GetSubscription(ctx, record.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != subscriptions.StatusActive || !got.AutoRenew {
			t.Fatalf("unselected subscription changed: %#v", got)
		}
	}
	apply([]string{records[0].SubscriptionID, records[1].SubscriptionID})
	for i, record := range records {
		got, err := subRepo.GetSubscription(ctx, record.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if got.Status != subscriptions.StatusCancelled || got.AutoRenew || !got.AutoRenewCancelled || got.RenewalMode != string(subscriptions.RenewalModeAutoCancelled) {
				t.Fatalf("selected subscription was not cancelled: %#v", got)
			}
		} else if got.Status != subscriptions.StatusActive || !got.AutoRenew || got.AutoRenewCancelled {
			t.Fatalf("unselected subscription changed: %#v", got)
		}
	}
	review, err := lifecycle.GetVPSArchiveReview(ctx, vps.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Eligible {
		t.Fatal("retained active subscription must still prevent archive")
	}
}
