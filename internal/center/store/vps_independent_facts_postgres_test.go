package store

import (
	"context"
	"errors"
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

func TestVPSAddressValidationPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	repo := NewPostgresVPSAssetRepository(pool)
	if _, err := repo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "bad address", IPv4: "999.1.1"}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("create accepted malformed ipv4: %v", err)
	}
	vps, err := repo.CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: "legacy address", IPv4: "203.0.113.20"})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟升级前已存入的非法文本：整表单编辑原样回传时不应阻塞其他字段。
	if _, err := pool.Exec(ctx, `update vps_assets set ipv4='999.1.1' where vps_id=$1`, vps.VPSID); err != nil {
		t.Fatal(err)
	}
	patched, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{IPv4: vpsassets.PatchString("999.1.1"), Note: vpsassets.PatchString("keep legacy")})
	if err != nil {
		t.Fatalf("unchanged legacy ipv4 blocked patch: %v", err)
	}
	if patched.Note != "keep legacy" || patched.IPv4 != "999.1.1" {
		t.Fatalf("unexpected legacy patch result: %+v", patched)
	}
	// 只差首尾空白的存量非法文本仍视为未改动（与 ipidentity.Changed 同口径）。
	if _, err := pool.Exec(ctx, `update vps_assets set ipv4=' 999.1.1 ' where vps_id=$1`, vps.VPSID); err != nil {
		t.Fatal(err)
	}
	patched, err = repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{IPv4: vpsassets.PatchString("999.1.1"), Note: vpsassets.PatchString("keep padded legacy")})
	if err != nil || patched.Note != "keep padded legacy" {
		t.Fatalf("whitespace-only legacy ipv4 blocked patch: %+v %v", patched, err)
	}
	// 过期表单回传旧非法地址时必须先得到版本冲突，前端才能进入“加载最新版本”的合并流程。
	stale := patched.UpdatedAt.Add(-time.Minute)
	if _, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{IPv4: vpsassets.PatchString("888.1.1"), Note: vpsassets.PatchString("stale"), ExpectedUpdatedAt: &stale}); !errors.Is(err, vpsassets.ErrVPSAssetConflict) {
		t.Fatalf("stale patch with malformed ipv4 = %v, want conflict", err)
	}
	if _, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{IPv4: vpsassets.PatchString("999.1.2")}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("patch accepted changed malformed ipv4: %v", err)
	}
	if _, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{IPv6: vpsassets.PatchString("203.0.113.20")}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("patch accepted ipv4 text as ipv6: %v", err)
	}
	fixed, err := repo.PatchVPSAsset(ctx, vps.VPSID, vpsassets.PatchInput{IPv4: vpsassets.PatchString(" 203.0.113.21 ")})
	if err != nil || fixed.IPv4 != "203.0.113.21" {
		t.Fatalf("valid ipv4 patch failed: %+v %v", fixed, err)
	}
}
