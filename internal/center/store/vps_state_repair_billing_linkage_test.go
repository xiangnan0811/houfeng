package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairHistoricalAutoRenewRemainsActionable(t *testing.T) {
	impacts := buildSubscriptionImpacts([]subscriptions.Record{{SubscriptionID: "historical", Status: subscriptions.StatusCancelled, AutoRenew: true, RenewalMode: string(subscriptions.RenewalModeAuto)}})
	if impacts[0].Role != "attention" || impacts[0].RecommendedAction != "review_provider_auto_renew" {
		t.Fatalf("contradictory automatic renewal must remain an explicit cancellation candidate: %#v", impacts[0])
	}
}

func TestVPSRenewalDecisionPreservesBillingFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subRepo := NewPostgresSubscriptionRepository(pool)
	for _, mode := range []string{"auto", "manual", "auto_cancelled"} {
		t.Run(mode, func(t *testing.T) {
			vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: mode, AcquisitionSource: "gift"})
			if err != nil {
				t.Fatal(err)
			}
			sub, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{VPSID: vps.VPSID, Price: 0, Currency: "USD", BillingMonths: 1, RenewalMode: mode})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := vpsRepo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalCancel)}); err != nil {
				t.Fatal(err)
			}
			got, err := subRepo.GetSubscription(ctx, sub.SubscriptionID)
			if err != nil {
				t.Fatal(err)
			}
			if got.RenewalMode != sub.RenewalMode || got.AutoRenew != sub.AutoRenew || got.AutoRenewCancelled != sub.AutoRenewCancelled || got.Status != sub.Status {
				t.Fatalf("decision modified billing facts: before=%+v after=%+v", sub, got)
			}
		})
	}
}
