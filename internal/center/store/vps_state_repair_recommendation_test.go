package store

import (
	"slices"
	"testing"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSStateRepairCancellationRecommendation(t *testing.T) {
	date := func(value string) *subscriptions.Date {
		t.Helper()
		result, err := subscriptions.ParseDate(value)
		if err != nil {
			t.Fatal(err)
		}
		return &result
	}
	past, today, future := date("2026-09-22"), date("2026-09-23"), date("2026-09-24")
	cases := []struct {
		name              string
		lifecycle         vpsassets.LifecycleStatus
		records           []subscriptions.Record
		want              vpsassets.LifecycleStatus
		needsConfirmation bool
	}{
		{"future entitlement with historical expired", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: future}, {Status: subscriptions.StatusExpired, EndsAt: past}}, vpsassets.LifecycleToCancel, false},
		{"expired history alone is not current evidence", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusExpired, EndsAt: past}}, vpsassets.LifecycleToCancel, true},
		{"paused history is not current evidence", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusPaused, EndsAt: past}}, vpsassets.LifecycleToCancel, true},
		{"cancelled explicit ended entitlement recommends cancelled", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeAutoCancelled), AutoRenewCancelled: true, EndsAt: past}}, vpsassets.LifecycleCancelled, false},
		{"cancelled future entitlement stays to cancel without warning", vpsassets.LifecycleToCancel, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeAutoCancelled), AutoRenewCancelled: true, EndsAt: future}}, vpsassets.LifecycleToCancel, false},
		{"cancelled source entitlement ending today recommends cancelled", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeGift), EndsAt: today}}, vpsassets.LifecycleCancelled, false},
		{"cancelled renew_at fallback ended recommends cancelled", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeAutoCancelled), AutoRenewCancelled: true, RenewAt: past}}, vpsassets.LifecycleCancelled, false},
		{"cancelled renew_at fallback future stays to cancel without warning", vpsassets.LifecycleToCancel, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeAutoCancelled), AutoRenewCancelled: true, RenewAt: future}}, vpsassets.LifecycleToCancel, false},
		{"cancelled renew_at fallback contradictory flags need confirmation", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeManual), AutoRenewCancelled: true, RenewAt: past}}, vpsassets.LifecycleToCancel, true},
		{"cancelled source renewal date is history", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeGift), RenewAt: past}}, vpsassets.LifecycleToCancel, true},
		{"cancelled without any end date is history", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeAutoCancelled), AutoRenewCancelled: true}}, vpsassets.LifecycleToCancel, true},
		{"cancelled contradictory renewal flags need confirmation", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeManual), AutoRenewCancelled: true, EndsAt: past}}, vpsassets.LifecycleToCancel, true},
		{"cancelled and active entitlements must all expire", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusCancelled, RenewalMode: string(subscriptions.RenewalModeAutoCancelled), AutoRenewCancelled: true, EndsAt: past}, {Status: subscriptions.StatusActive, EndsAt: future}}, vpsassets.LifecycleToCancel, false},
		{"no current subscription needs confirmation", vpsassets.LifecycleActive, nil, vpsassets.LifecycleToCancel, true},
		{"to cancel recomputes expiry", vpsassets.LifecycleToCancel, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: past}}, vpsassets.LifecycleCancelled, false},
		{"source renewal date is not entitlement expiry", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeGift), RenewAt: past}}, vpsassets.LifecycleToCancel, true},
		{"future explicit entitlement with canonical auto renewal", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeAuto), AutoRenew: true, EndsAt: future}}, vpsassets.LifecycleToCancel, false},
		{"future fallback renewal date with canonical auto renewal", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeAuto), AutoRenew: true, RenewAt: future}}, vpsassets.LifecycleToCancel, false},
		{"expired auto renewal prevents inferred cancellation", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeAuto), AutoRenew: true, EndsAt: past}}, vpsassets.LifecycleToCancel, true},
		{"active expired manual contradictory flags need confirmation", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeManual), AutoRenewCancelled: true, EndsAt: past}}, vpsassets.LifecycleToCancel, true},
		{"active expired auto cancelled mode without flags needs confirmation", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeAutoCancelled), EndsAt: past}}, vpsassets.LifecycleToCancel, true},
		{"cancelled remains terminal", vpsassets.LifecycleCancelled, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: future}}, vpsassets.LifecycleCancelled, false},
		{"today is expired", vpsassets.LifecycleToCancel, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: today}}, vpsassets.LifecycleCancelled, false},
		{"trial cannot replace entitlement end", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, TrialEndsAt: past}}, vpsassets.LifecycleToCancel, true},
		{"unknown prevents terminal inference", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: past}, {Status: subscriptions.StatusUnknown}}, vpsassets.LifecycleToCancel, true},
		{"contradictory entitlement dates", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: past, RenewAt: future}}, vpsassets.LifecycleToCancel, true},
		{"explicit source entitlement can expire", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, RenewalMode: string(subscriptions.RenewalModeGift), EndsAt: past}}, vpsassets.LifecycleCancelled, false},
		{"historical automatic renewal remains a fact", vpsassets.LifecycleActive, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: past}, {Status: subscriptions.StatusCancelled, AutoRenew: true}}, vpsassets.LifecycleToCancel, true},
		{"all active entitlements must expire", vpsassets.LifecycleToCancel, []subscriptions.Record{{Status: subscriptions.StatusActive, EndsAt: past}, {Status: subscriptions.StatusActive, EndsAt: future}}, vpsassets.LifecycleToCancel, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preview := assetlifecycle.CancellationPreview{VPS: vpsassets.Record{LifecycleStatus: tc.lifecycle}, Subscriptions: buildSubscriptionImpacts(tc.records)}
			check := func() {
				t.Helper()
				got, reason := recommendedVPSCancellationLifecycle(preview, *today)
				if got != tc.want {
					t.Fatalf("recommendation = %q, want %q", got, tc.want)
				}
				if (reason != "") != tc.needsConfirmation {
					t.Fatalf("confirmation required = %t, want %t", reason != "", tc.needsConfirmation)
				}
			}
			check()
			if len(preview.Subscriptions) > 1 {
				slices.Reverse(preview.Subscriptions)
				check()
			}
		})
	}
}
