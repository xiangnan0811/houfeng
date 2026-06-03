package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/subscriptions"
)

type fakeSubscriptionCostRepository struct {
	monthlyBudgets            []subscriptioncosts.MonthlyBudgetRecord
	upsertMonthlyBudget       subscriptioncosts.UpsertMonthlyBudgetInput
	upsertMonthlyBudgetResult subscriptioncosts.MonthlyBudgetRecord
}

func (r *fakeSubscriptionCostRepository) ListCostRows(context.Context, centersettings.SubscriptionCostSettings) ([]subscriptioncosts.CostRow, error) {
	return nil, nil
}

func (r *fakeSubscriptionCostRepository) ListCostMonthBuckets(context.Context, centersettings.SubscriptionCostSettings, int, time.Time) ([]subscriptioncosts.SeriesPoint, error) {
	return nil, nil
}

func (r *fakeSubscriptionCostRepository) ListBudgetMonthBuckets(context.Context, centersettings.SubscriptionCostSettings, int, time.Time) ([]subscriptioncosts.SeriesPoint, error) {
	return nil, nil
}

func (r *fakeSubscriptionCostRepository) ListMissingSubscriptionAssets(context.Context) ([]subscriptioncosts.MissingSubscriptionAsset, error) {
	return nil, nil
}

func (r *fakeSubscriptionCostRepository) ListBudgets(context.Context, subscriptioncosts.BudgetListFilters) ([]subscriptioncosts.BudgetRecord, error) {
	return nil, nil
}

func (r *fakeSubscriptionCostRepository) CreateBudget(context.Context, subscriptioncosts.CreateBudgetInput) (subscriptioncosts.BudgetRecord, error) {
	return subscriptioncosts.BudgetRecord{}, nil
}

func (r *fakeSubscriptionCostRepository) PatchBudget(context.Context, subscriptioncosts.PatchBudgetInput) (subscriptioncosts.BudgetRecord, error) {
	return subscriptioncosts.BudgetRecord{}, nil
}

func (r *fakeSubscriptionCostRepository) ListMonthlyBudgets(context.Context) ([]subscriptioncosts.MonthlyBudgetRecord, error) {
	return r.monthlyBudgets, nil
}

func (r *fakeSubscriptionCostRepository) UpsertMonthlyBudget(_ context.Context, input subscriptioncosts.UpsertMonthlyBudgetInput) (subscriptioncosts.MonthlyBudgetRecord, error) {
	r.upsertMonthlyBudget = input
	if r.upsertMonthlyBudgetResult.BudgetMonth.Time.IsZero() {
		return subscriptioncosts.MonthlyBudgetRecord{
			BudgetMonth:  input.BudgetMonth,
			BaseCurrency: input.BaseCurrency,
			MonthlyLimit: input.MonthlyLimit,
			WarningPct:   input.WarningPct,
			Note:         input.Note,
		}, nil
	}
	return r.upsertMonthlyBudgetResult, nil
}

func (r *fakeSubscriptionCostRepository) ListActiveCurrencies(context.Context) ([]string, error) {
	return nil, nil
}

func (r *fakeSubscriptionCostRepository) UpsertExchangeRate(context.Context, subscriptioncosts.ExchangeRateUpsert) (subscriptioncosts.ExchangeRateRecord, error) {
	return subscriptioncosts.ExchangeRateRecord{}, nil
}

func (r *fakeSubscriptionCostRepository) ListReminderCandidates(context.Context, centersettings.SubscriptionCostSettings, []int) ([]subscriptioncosts.ReminderCandidate, error) {
	return nil, nil
}

func (r *fakeSubscriptionCostRepository) TryCreateReminderDelivery(context.Context, subscriptioncosts.ReminderDeliveryInput) (string, bool, error) {
	return "", false, nil
}

func (r *fakeSubscriptionCostRepository) UpdateReminderDelivery(context.Context, string, subscriptioncosts.ReminderDeliveryUpdate) error {
	return nil
}

type fakeSubscriptionCostSettingsRepository struct {
	settings centersettings.CenterSettings
}

func (r *fakeSubscriptionCostSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	if r.settings.SubscriptionCost.BaseCurrency == "" {
		r.settings = centersettings.Default()
	}
	return r.settings, nil
}

func (r *fakeSubscriptionCostSettingsRepository) PutSettings(_ context.Context, settings centersettings.CenterSettings) (centersettings.CenterSettings, error) {
	r.settings = settings
	return settings, nil
}

func TestSubscriptionMonthlyBudgetsListsBudgets(t *testing.T) {
	month := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	repo := &fakeSubscriptionCostRepository{monthlyBudgets: []subscriptioncosts.MonthlyBudgetRecord{{
		BudgetMonth:  month,
		BaseCurrency: "CNY",
		MonthlyLimit: 120,
		WarningPct:   80,
		Note:         "baseline",
	}}}
	service := subscriptioncosts.NewService(repo, &fakeSubscriptionCostSettingsRepository{}, nil)
	handler := handlers.SubscriptionMonthlyBudgets(service)
	req := httptest.NewRequest(http.MethodGet, "/api/subscription-monthly-budgets", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body []subscriptioncosts.MonthlyBudgetRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body) != 1 || body[0].BudgetMonth.Time.Format("2006-01-02") != "2026-06-01" || body[0].MonthlyLimit != 120 {
		t.Fatalf("body = %#v, want monthly budget", body)
	}
}

func TestSubscriptionMonthlyBudgetsUpsertsPathMonth(t *testing.T) {
	repo := &fakeSubscriptionCostRepository{}
	service := subscriptioncosts.NewService(repo, &fakeSubscriptionCostSettingsRepository{}, nil)
	handler := handlers.SubscriptionMonthlyBudgets(service)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription-monthly-budgets/2026-07", strings.NewReader(`{
		"base_currency":"usd",
		"monthly_limit":140.5,
		"warning_pct":75,
		"note":" next budget "
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.upsertMonthlyBudget.BudgetMonth.Time.Format("2006-01-02") != "2026-07-01" {
		t.Fatalf("budget month = %s, want path month", repo.upsertMonthlyBudget.BudgetMonth.Time.Format("2006-01-02"))
	}
	if repo.upsertMonthlyBudget.BaseCurrency != "USD" || repo.upsertMonthlyBudget.MonthlyLimit != 140.5 || repo.upsertMonthlyBudget.WarningPct != 75 || repo.upsertMonthlyBudget.Note != "next budget" {
		t.Fatalf("upsert input = %#v, want normalized payload", repo.upsertMonthlyBudget)
	}
}

func TestSubscriptionMonthlyBudgetsRejectsInvalidInput(t *testing.T) {
	service := subscriptioncosts.NewService(&fakeSubscriptionCostRepository{}, &fakeSubscriptionCostSettingsRepository{}, nil)
	handler := handlers.SubscriptionMonthlyBudgets(service)
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "invalid path month", path: "/api/subscription-monthly-budgets/2026-13", body: `{"base_currency":"CNY","monthly_limit":100}`},
		{name: "non first day body month", path: "/api/subscription-monthly-budgets", body: `{"budget_month":"2026-06-02","base_currency":"CNY","monthly_limit":100}`},
		{name: "negative limit", path: "/api/subscription-monthly-budgets/2026-06", body: `{"base_currency":"CNY","monthly_limit":-1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d body %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestSubscriptionMonthlyBudgetsRejectsNestedPath(t *testing.T) {
	service := subscriptioncosts.NewService(&fakeSubscriptionCostRepository{}, &fakeSubscriptionCostSettingsRepository{}, nil)
	handler := handlers.SubscriptionMonthlyBudgets(service)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription-monthly-budgets/2026-06/extra", strings.NewReader(`{"base_currency":"CNY","monthly_limit":100}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d body %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}
