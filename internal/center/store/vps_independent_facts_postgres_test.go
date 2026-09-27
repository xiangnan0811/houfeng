package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestVPSIndependentFactsPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresVPSAssetRepository(pool)
	expiry := "2027-09-26"
	checked := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	vps, err := repo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "independent facts", UsageTags: []string{" 自定义 ", "自定义", "服务"}, ValidityMode: "fixed", ExpiresAt: &expiry, AcquisitionSource: "gift", AutoRenewCheck: "enabled", AutoRenewCheckedAt: &checked})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(vps.UsageTags, []string{"自定义", "服务"}) || vps.ExpiresAt == nil || *vps.ExpiresAt != expiry || vps.AcquisitionSource != "gift" {
		t.Fatalf("facts did not round trip: %+v", vps)
	}
	subRepo := NewPostgresSubscriptionRepository(pool)
	sub, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{VPSID: vps.VPSID, Price: 5, Currency: "USD", BillingMonths: 1, RenewalMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	vps, err = repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalCancel), RenewalReason: vpsassets.PatchString("manual decision"), RenewalReviewAt: vpsassets.OptionalTime{Set: true, Value: &checked}})
	if err != nil {
		t.Fatal(err)
	}
	gotSub, err := subRepo.GetSubscription(ctx, sub.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if !gotSub.AutoRenew || gotSub.RenewalMode != "auto" || vps.AutoRenewCheck != "enabled" {
		t.Fatalf("intent overwrote billing/provider facts: %+v %+v", vps, gotSub)
	}
	if _, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{ValidityMode: vpsassets.PatchString("unlimited")}); err == nil {
		t.Fatal("partial validity contradiction accepted")
	}
	vps, err = repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{ValidityMode: vpsassets.PatchString("unlimited"), ExpiresAt: vpsassets.PatchNullableString(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if vps.ExpiresAt != nil || vps.ValidityMode != "unlimited" || vps.RenewalDecision != vpsassets.RenewalCancel {
		t.Fatalf("wrong independent patch: %+v", vps)
	}
	if _, err := pool.Exec(ctx, `update vps_assets set lifecycle_status='archived', archived_at=now() where vps_id=$1`, vps.VPSID); err != nil {
		t.Fatal(err)
	}
	archived, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{AutoRenewCheck: vpsassets.PatchString("disabled"), AutoRenewCheckedAt: vpsassets.OptionalTime{Set: true, Value: &checked}, Note: vpsassets.PatchString("provider reviewed")})
	if err != nil {
		t.Fatal(err)
	}
	if archived.LifecycleStatus != vpsassets.LifecycleArchived || archived.AutoRenewCheck != "disabled" {
		t.Fatalf("archive review changed lifecycle: %+v", archived)
	}
	if _, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{DisplayName: vpsassets.PatchString("forbidden rename")}); err == nil {
		t.Fatal("archive allowed unrelated metadata patch")
	}
	var revisions int
	if err := pool.QueryRow(ctx, `select count(*) from experience_logs where vps_id=$1 and details like '%provider reviewed%'`, vps.VPSID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("revision audit count=%d err=%v", revisions, err)
	}
	if _, err := subRepo.CreateSubscription(ctx, subscriptions.CreateInput{VPSID: vps.VPSID, Price: 7, Currency: "USD", BillingMonths: 1, RenewalMode: "manual", Note: "late bill"}); err != nil {
		t.Fatalf("archived supplemental bill: %v", err)
	}
	if _, err := subRepo.PatchSubscription(ctx, sub.SubscriptionID, subscriptions.PatchInput{Note: subscriptions.PatchString("refund USD 2; receipt verified")}); err != nil {
		t.Fatalf("archived supplemental note: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from experience_logs where vps_id=$1 and summary='归档账单补充修订' and details like '%refund USD 2%'`, vps.VPSID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("bill note revision count=%d err=%v", revisions, err)
	}
}
