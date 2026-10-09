package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	centersettings "houfeng/internal/center/settings"
)

// 补录的存量订阅按 started_at 入月桶：录入时间在本月，不应让此前月份显示为 0、本月陡升。
// 逐月断言完整 12 个桶，覆盖开始月边界、未来开始日期、开始日期未知的回退和价格历史。
func TestPostgresIntegrationSubscriptionCostBackfilledStartMonth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture SQL: %v", err)
		}
	}
	exec(`insert into vps_assets (vps_id, display_name, lifecycle_status, usage_status, renewal_decision)
		values ('vps_backfill', 'backfill', 'active', 'in_use', 'keep')`)
	exec(`insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, status, started_at, created_at
		) values
			('sub_backfill_old', 'vps_backfill', 30, 'CNY', 'monthly', 1, 30, 'active', '2025-03-15', '2026-10-09T03:00:00Z'),
			('sub_backfill_mid', 'vps_backfill', 20, 'CNY', 'monthly', 1, 20, 'active', '2026-08-01', '2026-10-09T03:00:00Z'),
			('sub_future_start', 'vps_backfill', 9, 'CNY', 'monthly', 1, 9, 'active', '2026-11-01', '2026-10-09T03:00:00Z'),
			('sub_unknown_start', 'vps_backfill', 7, 'CNY', 'monthly', 1, 7, 'active', null, '2026-10-09T03:00:00Z')`)
	// 补录订阅在 2026-06-15 从 25 涨到 30：此前月份取变更前价格，变更当月起取新价格。
	exec(`insert into price_histories (
			price_history_id, subscription_id, vps_id, from_price, to_price, from_currency,
			to_currency, from_billing_cycle, to_billing_cycle, from_billing_months,
			to_billing_months, from_monthly_price, to_monthly_price, from_auto_renew,
			to_auto_renew, from_auto_renew_cancelled, to_auto_renew_cancelled, from_status,
			to_status, from_billing_period_unit, to_billing_period_unit,
			from_billing_period_length, to_billing_period_length, changed_at, created_at
		) values ('ph_backfill_old', 'sub_backfill_old', 'vps_backfill', 25, 30, 'CNY',
			'CNY', 'monthly', 'monthly', 1, 1, 25, 30, false, false, false, false, 'active',
			'active', 'month', 'month', 1, 1, '2026-06-15T00:00:00Z', '2026-10-09T03:00:00Z')`)

	repo := NewPostgresSubscriptionCostRepository(pool)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	buckets, err := repo.ListCostMonthBuckets(ctx, centersettings.SubscriptionCostSettings{BaseCurrency: "CNY", ExchangeRateProvider: "frankfurter"}, 12, now)
	if err != nil {
		t.Fatalf("ListCostMonthBuckets() error = %v", err)
	}
	type point struct {
		Bucket string
		Cost   float64
	}
	want := []point{
		{"2025-11", 25}, {"2025-12", 25}, {"2026-01", 25}, {"2026-02", 25}, {"2026-03", 25}, {"2026-04", 25}, {"2026-05", 25},
		{"2026-06", 30}, {"2026-07", 30}, {"2026-08", 50}, {"2026-09", 50}, {"2026-10", 57},
	}
	got := make([]point, 0, len(buckets))
	for _, bucket := range buckets {
		if bucket.DataInsufficient {
			t.Fatalf("base-currency bucket %s marked insufficient", bucket.Bucket)
		}
		got = append(got, point{bucket.Bucket, bucket.MonthlyCost})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buckets = %v, want %v", got, want)
	}
}
