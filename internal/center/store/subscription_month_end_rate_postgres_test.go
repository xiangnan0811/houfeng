package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/subscriptions"
)

// 回补的月末汇率让补录月份按当月真实汇率计入趋势（不再标估算），回补后不再列为缺口；
// 它的 fetched_at 是回补时间（比实时汇率更新），但当前成本、币对状态与续费提醒仍只用实时汇率。
func TestPostgresIntegrationSubscriptionMonthEndRatesFeedOnlyTheTrend(t *testing.T) {
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
		values ('vps_month_end', 'month end', 'active', 'in_use', 'keep')`)
	exec(`insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, status, started_at, renew_at, created_at
		) values
			('sub_month_end_usd', 'vps_month_end', 10, 'USD', 'monthly', 1, 10, 'active', '2026-07-01', current_date + 3, '2026-10-09T03:00:00Z')`)
	exec(`insert into subscription_exchange_rates (rate_id, provider, base_currency, quote_currency, rate, rate_date, fetched_at)
		values ('rate_month_end_live', 'frankfurter', 'CNY', 'USD', 7, '2026-10-05', '2026-10-05T00:00:00Z')`)

	repo := NewPostgresSubscriptionCostRepository(pool)
	settings := centersettings.SubscriptionCostSettings{BaseCurrency: "CNY", ExchangeRateProvider: "frankfurter", ExchangeRateStaleAfterHours: 24}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	missing := func() []string {
		t.Helper()
		requests, err := repo.ListMissingMonthEndExchangeRates(ctx, settings, 12, now)
		if err != nil {
			t.Fatalf("ListMissingMonthEndExchangeRates() error = %v", err)
		}
		got := make([]string, 0, len(requests))
		for _, request := range requests {
			got = append(got, request.QuoteCurrency+"@"+request.MonthEnd.Time.Format(subscriptions.DateLayout))
		}
		return got
	}
	// 当月已有实时汇率，不列入；补录月份按从近到远排列。
	if got, want := missing(), []string{"USD@2026-09-30", "USD@2026-08-31", "USD@2026-07-31"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing month-end rates = %v, want %v", got, want)
	}

	backfilledAt := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	upsert := func(monthEnd, rateDate string, rate float64) {
		t.Helper()
		monthEndDate, _ := subscriptions.ParseDate(monthEnd)
		rateDateValue, _ := subscriptions.ParseDate(rateDate)
		if err := repo.UpsertMonthEndExchangeRate(ctx, subscriptioncosts.MonthEndExchangeRateUpsert{
			Provider: "frankfurter", BaseCurrency: "CNY", QuoteCurrency: "USD",
			MonthEnd: monthEndDate, Rate: rate, RateDate: rateDateValue, FetchedAt: backfilledAt,
		}); err != nil {
			t.Fatalf("UpsertMonthEndExchangeRate(%s) error = %v", monthEnd, err)
		}
	}
	upsert("2026-09-30", "2026-09-30", 6.5)
	upsert("2026-09-30", "2026-09-30", 6.9) // 同月重复回补覆盖为最新结果
	upsert("2026-08-31", "2026-08-28", 6.8)
	if got, want := missing(), []string{"USD@2026-07-31"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing month-end rates after backfill = %v, want %v", got, want)
	}

	buckets, err := repo.ListCostMonthBuckets(ctx, settings, 4, now)
	if err != nil {
		t.Fatalf("ListCostMonthBuckets() error = %v", err)
	}
	type point struct {
		Bucket       string
		Cost         float64
		Estimated    bool
		Insufficient bool
	}
	got := make([]point, 0, len(buckets))
	for _, bucket := range buckets {
		got = append(got, point{bucket.Bucket, bucket.MonthlyCost, bucket.RateEstimated, bucket.DataInsufficient})
	}
	want := []point{
		// 7 月还没回补：按此后最早的实时汇率（7）估算，并且不会误用回补的 8、9 月月末汇率。
		{"2026-07", 70, true, false},
		{"2026-08", 68, false, false},
		{"2026-09", 69, false, false},
		{"2026-10", 70, false, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buckets = %+v, want %+v", got, want)
	}

	rows, err := repo.ListCostRows(ctx, settings)
	if err != nil {
		t.Fatalf("ListCostRows() error = %v", err)
	}
	if len(rows) != 1 || rows[0].ExchangeRate == nil || *rows[0].ExchangeRate != 7 {
		t.Fatalf("current cost rows = %+v, want the live USD rate 7 rather than a newer-fetched month-end rate", rows)
	}
	pairs, err := repo.ListActiveExchangeRatePairs(ctx, settings)
	if err != nil {
		t.Fatalf("ListActiveExchangeRatePairs() error = %v", err)
	}
	if len(pairs) != 1 || pairs[0].LatestRate == nil || pairs[0].LatestRate.Rate != 7 || !pairs[0].LatestRate.FetchedAt.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("active pairs = %+v, want the live USD rate as the latest cache entry", pairs)
	}
	candidates, err := repo.ListReminderCandidates(ctx, settings, []int{3})
	if err != nil {
		t.Fatalf("ListReminderCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].MonthlyPriceBase == nil || *candidates[0].MonthlyPriceBase != 70 {
		t.Fatalf("reminder candidates = %+v, want the base price converted with the live rate", candidates)
	}

	// 窗口边界：与最长趋势窗口一致（含当月共 12 个月），只补当月之前的 11 个历史月份，不补更早的月份。
	exec(`insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, status, started_at, created_at
		) values ('sub_month_end_eur', 'vps_month_end', 5, 'EUR', 'monthly', 1, 5, 'active', '2024-01-01', '2026-10-09T03:00:00Z')`)
	var eur []string
	for _, item := range missing() {
		if strings.HasPrefix(item, "EUR@") {
			eur = append(eur, item)
		}
	}
	if len(eur) != 11 || eur[0] != "EUR@2026-09-30" || eur[len(eur)-1] != "EUR@2025-11-30" {
		t.Fatalf("EUR missing month-end rates = %v, want the 11 historical months 2026-09 back to 2025-11", eur)
	}
}

// 任何读取汇率缓存的查询都必须区分实时汇率与回补的月末汇率：不带 month_end_for 条件的读取
// 会把回补时间最新的历史汇率当成“最新/当时已知”的汇率。新增读取方时必须在这里显式说明。
func TestSubscriptionExchangeRateReadersDistinguishMonthEndRates(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob store sources: %v", err)
	}
	reader := regexp.MustCompile(`(?s)from subscription_exchange_rates\b.*?(?:limit 1|\)\s*,|\)\s*$|order by[^\n]*\n)`)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, match := range reader.FindAllString(string(raw), -1) {
			checked++
			if !strings.Contains(match, "month_end_for") {
				t.Errorf("%s reads subscription_exchange_rates without a month_end_for condition:\n%s", file, match)
			}
		}
	}
	// subscription_costs.go：当前成本、趋势 lr/hr/er_first、币对状态、提醒；evidence：两处证据快照。
	if checked != 8 {
		t.Fatalf("checked %d exchange-rate readers, want 8; update this guard when readers change", checked)
	}
}
