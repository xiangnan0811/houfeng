package subscriptioncosts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptions"
)

// historicalExchangeRateProvider 记录按日期取汇率的请求；默认返回请求日期当天的汇率。
type historicalExchangeRateProvider struct {
	mu       sync.Mutex
	requests []string
	failOn   map[string]error
	dateFor  map[string]string
}

func (p *historicalExchangeRateProvider) FetchRate(context.Context, string, string) (FetchedExchangeRate, error) {
	return FetchedExchangeRate{}, errors.New("live fetch is not expected in month-end backfill tests")
}

func (p *historicalExchangeRateProvider) FetchRateOn(_ context.Context, quote, base string, date subscriptions.Date) (FetchedExchangeRate, error) {
	key := quote + "/" + base + "@" + date.Time.Format(subscriptions.DateLayout)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, key)
	if err := p.failOn[key]; err != nil {
		return FetchedExchangeRate{}, err
	}
	rateDate := date
	if raw, ok := p.dateFor[key]; ok {
		parsed, err := subscriptions.ParseDate(raw)
		if err != nil {
			return FetchedExchangeRate{}, err
		}
		rateDate = parsed
	}
	return FetchedExchangeRate{Rate: 7.1, RateDate: rateDate}, nil
}

func (p *historicalExchangeRateProvider) requested() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.requests...)
}

func monthEndRequest(t *testing.T, quote, monthEnd string) MonthEndExchangeRateRequest {
	t.Helper()
	date, err := subscriptions.ParseDate(monthEnd)
	if err != nil {
		t.Fatalf("parse month end %q: %v", monthEnd, err)
	}
	return MonthEndExchangeRateRequest{QuoteCurrency: quote, MonthEnd: date}
}

func newMonthEndBackfillWorker(t *testing.T) (*ExchangeRateWorker, *fakeSubscriptionCostRepo, *historicalExchangeRateProvider, *fakeSettingsRepo) {
	t.Helper()
	service, repo := newTestService()
	provider := &historicalExchangeRateProvider{failOn: map[string]error{}, dateFor: map[string]string{}}
	service.providers["frankfurter"] = provider
	service.now = func() time.Time { return time.Date(2026, time.October, 13, 8, 0, 0, 0, time.UTC) }
	return NewExchangeRateWorker(service, nil, time.Hour), repo, provider, service.settingsRepo.(*fakeSettingsRepo)
}

func TestMonthEndBackfillFillsRecentMonthsFirstInBoundedBatches(t *testing.T) {
	worker, repo, provider, _ := newMonthEndBackfillWorker(t)
	for _, monthEnd := range []string{"2026-09-30", "2026-08-31", "2026-07-31", "2026-06-30", "2026-05-31", "2026-04-30", "2026-03-31", "2026-02-28"} {
		repo.missingMonthEnd = append(repo.missingMonthEnd, monthEndRequest(t, "USD", monthEnd))
	}

	if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
		t.Fatalf("first round: %v", err)
	}
	if repo.missingMonthEndMonths != monthEndBackfillWindowMonths {
		t.Fatalf("backfill window = %d months, want %d", repo.missingMonthEndMonths, monthEndBackfillWindowMonths)
	}
	got := provider.requested()
	want := []string{"USD/CNY@2026-09-30", "USD/CNY@2026-08-31", "USD/CNY@2026-07-31", "USD/CNY@2026-06-30", "USD/CNY@2026-05-31", "USD/CNY@2026-04-30"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("first round requests = %v, want the %d most recent months %v", got, monthEndBackfillPerRound, want)
	}
	if len(repo.monthEndUpserts) != len(want) {
		t.Fatalf("month-end upserts = %d, want %d", len(repo.monthEndUpserts), len(want))
	}
	first := repo.monthEndUpserts[0]
	if first.Provider != "frankfurter" || first.BaseCurrency != "CNY" || first.QuoteCurrency != "USD" ||
		first.MonthEnd.Time.Format(subscriptions.DateLayout) != "2026-09-30" || first.Rate != 7.1 ||
		!first.FetchedAt.Equal(time.Date(2026, time.October, 13, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("first month-end upsert = %#v, want the September month-end rate with the real fetch time", first)
	}
	if len(repo.upserts) != 0 {
		t.Fatalf("live rate upserts = %d, want month-end backfill to never write live rates", len(repo.upserts))
	}

	// 30 秒对账继续补剩余月份，不需要等 12 小时周期。
	if err := worker.runRound(context.Background(), exchangeRateRoundReconcile); err != nil {
		t.Fatalf("second round: %v", err)
	}
	got = provider.requested()[len(want):]
	if strings.Join(got, ",") != "USD/CNY@2026-03-31,USD/CNY@2026-02-28" {
		t.Fatalf("second round requests = %v, want the remaining two months", got)
	}
	if err := worker.runRound(context.Background(), exchangeRateRoundReconcile); err != nil {
		t.Fatalf("third round: %v", err)
	}
	if calls := len(provider.requested()); calls != 8 {
		t.Fatalf("total requests after everything is filled = %d, want 8", calls)
	}
}

func TestMonthEndBackfillDoesNotRetryFailedMonthsUntilANewRound(t *testing.T) {
	worker, repo, provider, _ := newMonthEndBackfillWorker(t)
	repo.missingMonthEnd = []MonthEndExchangeRateRequest{
		monthEndRequest(t, "USD", "2026-09-30"),
		monthEndRequest(t, "EUR", "2026-09-30"),
	}
	provider.failOn["USD/CNY@2026-09-30"] = errors.New("frankfurter returned status 429")
	// 返回的交易日不在该月内：该月没有这个币对的数据，不能冒充当月汇率。
	provider.dateFor["EUR/CNY@2026-09-30"] = "2026-08-29"

	if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
		t.Fatalf("first round: %v", err)
	}
	if len(repo.monthEndUpserts) != 0 {
		t.Fatalf("month-end upserts = %#v, want none for a failed fetch or an out-of-month rate", repo.monthEndUpserts)
	}
	for _, trigger := range []exchangeRateRoundTrigger{exchangeRateRoundReconcile, exchangeRateRoundRetry} {
		if err := worker.runRound(context.Background(), trigger); err != nil {
			t.Fatalf("follow-up round: %v", err)
		}
	}
	if calls := len(provider.requested()); calls != 2 {
		t.Fatalf("requests before a new round = %d (%v), want failed months skipped", calls, provider.requested())
	}

	delete(provider.failOn, "USD/CNY@2026-09-30")
	if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
		t.Fatalf("new round: %v", err)
	}
	if calls := len(provider.requested()); calls != 4 {
		t.Fatalf("requests after a new round = %d, want both months retried once", calls)
	}
	if len(repo.monthEndUpserts) != 1 || repo.monthEndUpserts[0].QuoteCurrency != "USD" {
		t.Fatalf("month-end upserts = %#v, want only the recovered USD month", repo.monthEndUpserts)
	}
}

func TestMonthEndBackfillOnlyRunsForFrankfurter(t *testing.T) {
	worker, repo, provider, settingsRepo := newMonthEndBackfillWorker(t)
	repo.missingMonthEnd = []MonthEndExchangeRateRequest{monthEndRequest(t, "USD", "2026-09-30")}
	settingsRepo.settings.SubscriptionCost.ExchangeRateProvider = string(centersettings.SubscriptionExchangeRateProviderFixer)
	worker.service.providers["fixer"] = provider

	if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
		t.Fatalf("round: %v", err)
	}
	if calls := len(provider.requested()); calls != 0 {
		t.Fatalf("historical requests with Fixer = %d, want none", calls)
	}
}

func TestFrankfurterFetchRateOnRequestsTheDate(t *testing.T) {
	// 生产注入的 Frankfurter 提供方必须能按日期取汇率，否则回补会被类型断言静默跳过。
	if _, ok := NewFrankfurterProvider(nil).(HistoricalExchangeRateProvider); !ok {
		t.Fatal("NewFrankfurterProvider() does not implement HistoricalExchangeRateProvider")
	}
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"rate":7.0456,"date":"2026-02-27"}`))
	}))
	defer server.Close()
	provider := &HTTPExchangeRateProvider{baseURL: server.URL + "/v2", client: server.Client(), kind: "frankfurter"}

	got, err := provider.FetchRateOn(context.Background(), "usd", "cny", monthEndRequest(t, "USD", "2026-02-28").MonthEnd)
	if err != nil {
		t.Fatalf("FetchRateOn: %v", err)
	}
	if gotPath != "/v2/rate/USD/CNY" || gotQuery != "date=2026-02-28" {
		t.Fatalf("request = %s?%s, want /v2/rate/USD/CNY?date=2026-02-28", gotPath, gotQuery)
	}
	if got.Rate != 7.0456 || got.RateDate.Time.Format(subscriptions.DateLayout) != "2026-02-27" {
		t.Fatalf("FetchRateOn = %#v, want the last trading day rate", got)
	}

	if _, err := provider.FetchRate(context.Background(), "USD", "CNY"); err != nil || gotQuery != "" {
		t.Fatalf("FetchRate err=%v query=%q, want the live endpoint without a date", err, gotQuery)
	}

	fixer := &HTTPExchangeRateProvider{baseURL: server.URL, client: server.Client(), kind: "fixer", apiKey: "secret"}
	if _, err := fixer.FetchRateOn(context.Background(), "USD", "CNY", got.RateDate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Fixer FetchRateOn err = %v, want ErrInvalidInput", err)
	}
}
