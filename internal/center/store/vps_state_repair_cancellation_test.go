package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairCancelledCannotReopenThroughCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	repo := NewPostgresAssetLifecycleRepository(pool)
	vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "Terminal cancellation", LifecycleStatus: vpsassets.LifecycleIdle, UsageStatus: vpsassets.UsageIdle})
	if err != nil {
		t.Fatal(err)
	}
	apply := func(target vpsassets.LifecycleStatus) error {
		preview, err := repo.GetVPSCancellationPreview(ctx, vps.VPSID)
		if err != nil {
			return err
		}
		_, err = repo.ApplyVPSCancellation(ctx, vps.VPSID, assetlifecycle.ApplyCancellationInput{Reason: "confirmed lifecycle decision", VPSLifecycleStatus: target, PreviewDigest: preview.PreviewDigest})
		return err
	}
	if err := apply(vpsassets.LifecycleCancelled); err != nil {
		t.Fatalf("initial cancellation: %v", err)
	}
	if err := apply(vpsassets.LifecycleCancelled); err != nil {
		t.Fatalf("same-state residual handling: %v", err)
	}
	if err := apply(vpsassets.LifecycleToCancel); !errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked) {
		t.Fatalf("cancelled -> to_cancel error = %v, want blocked", err)
	}
	preview, err := repo.GetVPSCancellationPreview(ctx, vps.VPSID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.VPS.LifecycleStatus != vpsassets.LifecycleCancelled {
		t.Fatalf("blocked cancellation changed lifecycle to %q", preview.VPS.LifecycleStatus)
	}
}

func TestVPSStateRepairCancellationRecommendationAfterWorkbenchCancelsSubscription(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	vpsRepo := NewPostgresVPSAssetRepository(pool)
	subscriptionRepo := NewPostgresSubscriptionRepository(pool)
	repo := NewPostgresAssetLifecycleRepository(pool)
	future := subscriptions.NewDate(time.Now().UTC().AddDate(0, 0, 30))
	yesterday := subscriptions.NewDate(time.Now().UTC().AddDate(0, 0, -1))
	for _, variant := range []struct {
		name  string
		dates func(date *subscriptions.Date) (endsAt, renewAt *subscriptions.Date)
		patch func(date *subscriptions.Date) subscriptions.PatchInput
	}{
		{
			name:  "explicit ends_at",
			dates: func(date *subscriptions.Date) (*subscriptions.Date, *subscriptions.Date) { return date, nil },
			patch: func(date *subscriptions.Date) subscriptions.PatchInput {
				return subscriptions.PatchInput{EndsAt: subscriptions.PatchDate(date)}
			},
		},
		{
			// VPS detail subscription forms and validity extension only write renew_at.
			name:  "renew_at only",
			dates: func(date *subscriptions.Date) (*subscriptions.Date, *subscriptions.Date) { return nil, date },
			patch: func(date *subscriptions.Date) subscriptions.PatchInput {
				return subscriptions.PatchInput{RenewAt: subscriptions.PatchDate(date)}
			},
		},
	} {
		t.Run(variant.name, func(t *testing.T) {
			vps, err := vpsRepo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "Workbench cancelled subscription " + variant.name, LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse})
			if err != nil {
				t.Fatal(err)
			}
			endsAt, renewAt := variant.dates(&future)
			sub, err := subscriptionRepo.CreateSubscription(ctx, subscriptions.CreateInput{VPSID: vps.VPSID, Price: 12, Currency: "USD", BillingMonths: 1, Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeManual), EndsAt: endsAt, RenewAt: renewAt})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := repo.GetVPSCancellationPreview(ctx, vps.VPSID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.ApplyVPSCancellation(ctx, vps.VPSID, assetlifecycle.ApplyCancellationInput{
				Reason:             "provider cancellation accepted",
				SubscriptionIDs:    []string{sub.SubscriptionID},
				VPSLifecycleStatus: vpsassets.LifecycleToCancel,
				PreviewDigest:      preview.PreviewDigest,
			}); err != nil {
				t.Fatalf("ApplyVPSCancellation: %v", err)
			}
			assertRecommendation := func(stage string, want vpsassets.LifecycleStatus) {
				t.Helper()
				preview, err := repo.GetVPSCancellationPreview(ctx, vps.VPSID)
				if err != nil {
					t.Fatalf("%s preview: %v", stage, err)
				}
				index := slices.IndexFunc(preview.RecommendedSteps, func(step assetlifecycle.RecommendedLifecycleStep) bool {
					return step.StepType == assetlifecycle.StepTypeVPSLifecycle
				})
				// The workbench preselects the VPS step target; without a step the
				// current lifecycle already matches the recommendation.
				got := preview.VPS.LifecycleStatus
				if index >= 0 {
					got = vpsassets.LifecycleStatus(strings.SplitN(preview.RecommendedSteps[index].ToState, "/", 2)[0])
				}
				if got != want {
					t.Fatalf("%s recommended VPS lifecycle = %q, want %q (steps %#v)", stage, got, want, preview.RecommendedSteps)
				}
				if slices.ContainsFunc(preview.Warnings, func(warning string) bool {
					return warning == cancellationRecommendationConfirmationWarning || strings.HasPrefix(warning, "仅有关联历史")
				}) {
					t.Fatalf("%s preview treats the cancelled entitlement as missing or historical evidence: %#v", stage, preview.Warnings)
				}
			}
			cancelled, err := subscriptionRepo.GetSubscription(ctx, sub.SubscriptionID)
			if err != nil {
				t.Fatal(err)
			}
			if cancelled.Status != subscriptions.StatusCancelled || cancelled.AutoRenew || cancelled.RenewalMode != string(subscriptions.RenewalModeAutoCancelled) {
				t.Fatalf("workbench-cancelled subscription = status %q auto_renew %t mode %q", cancelled.Status, cancelled.AutoRenew, cancelled.RenewalMode)
			}
			assertRecommendation("future entitlement", vpsassets.LifecycleToCancel)

			if _, err := subscriptionRepo.PatchSubscription(ctx, sub.SubscriptionID, variant.patch(&yesterday)); err != nil {
				t.Fatalf("move cancelled entitlement end into the past: %v", err)
			}
			assertRecommendation("ended entitlement", vpsassets.LifecycleCancelled)
			assertVPSStateRepairCancellationVPSLifecycle(t, ctx, pool, vps.VPSID, vpsassets.LifecycleToCancel)
		})
	}
}
