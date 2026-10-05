package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/store"
	"houfeng/internal/center/subscriptioncosts"
)

const (
	subscriptionCostHF17VPSA               = "vps_hf17_a"
	subscriptionCostHF17VPSB               = "vps_hf17_b"
	subscriptionCostHF17SubA               = "sub_hf17_a"
	subscriptionCostHF17SubB               = "sub_hf17_b"
	subscriptionCostHF17Provider           = "provider_hf17"
	subscriptionCostHF19AmountVPS          = "vps_hf19_amount"
	subscriptionCostHF19AmountSub          = "sub_hf19_amount"
	subscriptionCostHF19CurrentGapVPS      = "vps_hf19_current_gap"
	subscriptionCostHF19CurrentGapSub      = "sub_hf19_current_gap"
	subscriptionCostHF19CurrentMissingVPS  = "vps_hf19_current_missing"
	subscriptionCostHF19ArchivedGapVPS     = "vps_hf19_archived_gap"
	subscriptionCostHF19ArchivedGapSub     = "sub_hf19_archived_gap"
	subscriptionCostHF19ArchivedMissingVPS = "vps_hf19_archived_missing"
)

func TestPostgresIntegrationSubscriptionCostHF17HF18(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	fixture := newRuntimeStreamAuthFixture(t)
	seedSubscriptionCostHF17Facts(t, ctx, fixture.pool)

	costRepo := store.NewPostgresSubscriptionCostRepository(fixture.pool)
	settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
	subscriptionRepo := store.NewPostgresSubscriptionRepository(fixture.pool)
	costService := subscriptioncosts.NewService(costRepo, settingsRepo, nil)
	authMiddleware := centerhttp.RequireSession(fixture.service, fixture.scope)
	router := centerhttp.New(centerhttp.RouterOptions{
		Version:                        "test",
		AuthLoginHandler:               handlers.Login(fixture.service),
		AuthLogoutHandler:              handlers.Logout(fixture.service),
		AuthMeHandler:                  handlers.Me(fixture.service),
		AuthChangePasswordHandler:      handlers.ChangePassword(fixture.service),
		AuthMiddleware:                 authMiddleware,
		SubscriptionsCollectionHandler: handlers.SubscriptionsCollection(subscriptionRepo, costService),
		SubscriptionSettingsHandler:    handlers.SubscriptionSettings(costService),
		SubscriptionBudgetsHandler:     handlers.SubscriptionBudgets(costService),
	})
	server := httptest.NewTLSServer(router)
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("new cookie jar: %v", err)
	}
	client := runtimeStreamAuthHTTPClientWithJar(server, jar)
	runtimeStreamAuthLogin(t, client, server.URL, runtimeStreamAuthPassword)

	var (
		rows         map[string]subscriptionCostHF17Row
		globalCNY100 subscriptionCostHF17Budget
		vpsCNY100    subscriptionCostHF17Budget
		vpsUSD20     subscriptionCostHF17Budget
		vpsCNY80     subscriptionCostHF17Budget
		globalCNY60  subscriptionCostHF17Budget
		dual         subscriptionCostHF17Budget
		settings     struct {
			BaseCurrency string `json:"base_currency"`
		}
	)

	if !t.Run("1 default CNY cost rows", func(t *testing.T) {
		rows = getSubscriptionCostHF17Rows(t, ctx, client, server.URL, "")
		if len(rows) != 2 {
			t.Fatalf("cost row count = %d, want 2", len(rows))
		}
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubA], "USD", "CNY", new(float64(70)), new(float64(840)), "unknown")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubB], "EUR", "CNY", nil, nil, "unknown")
	}) {
		return
	}

	if !t.Run("2 create CNY budgets and merge statuses", func(t *testing.T) {
		globalCNY100 = postSubscriptionCostHF17Budget(t, ctx, client, server.URL,
			"hf17-global-cny-100", "global", "", "CNY", new(float64(100)), nil,
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "unknown"})
		vpsCNY100 = postSubscriptionCostHF17Budget(t, ctx, client, server.URL,
			"hf17-vps-cny-100", "vps", subscriptionCostHF17VPSA, "CNY", new(float64(100)), nil,
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "ok", monthlySpend: new(float64(70)), yearlySpend: new(float64(840))})
		rows = getSubscriptionCostHF17Rows(t, ctx, client, server.URL, "")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubA], "USD", "CNY", new(float64(70)), new(float64(840)), "unknown")
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "unknown", []string{subscriptionCostHF17SubA, subscriptionCostHF17SubB})
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "ok", nil)
	}) {
		return
	}

	if !t.Run("3 reject original-currency budget matching", func(t *testing.T) {
		vpsUSD20 = postSubscriptionCostHF17Budget(t, ctx, client, server.URL,
			"hf17-vps-usd-20", "vps", subscriptionCostHF17VPSA, "USD", new(float64(20)), nil,
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(20)), status: "unknown"})
	}) {
		return
	}

	if !t.Run("4 warning outranks unknown", func(t *testing.T) {
		vpsCNY80 = postSubscriptionCostHF17Budget(t, ctx, client, server.URL,
			"hf17-vps-cny-80", "vps", subscriptionCostHF17VPSA, "CNY", new(float64(80)), nil,
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(80)), status: "warning", monthlySpend: new(float64(70)), yearlySpend: new(float64(840))})
		rows = getSubscriptionCostHF17Rows(t, ctx, client, server.URL, "")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubA], "USD", "CNY", new(float64(70)), new(float64(840)), "warning")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubB], "EUR", "CNY", nil, nil, "unknown")
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "warning", []string{subscriptionCostHF17SubA})
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "unknown", []string{subscriptionCostHF17SubB})
	}) {
		return
	}

	if !t.Run("5 over lower bound remains null", func(t *testing.T) {
		globalCNY60 = postSubscriptionCostHF17Budget(t, ctx, client, server.URL,
			"hf17-global-cny-60", "global", "", "CNY", new(float64(60)), nil,
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(60)), status: "over"})
		rows = getSubscriptionCostHF17Rows(t, ctx, client, server.URL, "")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubA], "USD", "CNY", new(float64(70)), new(float64(840)), "over")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubB], "EUR", "CNY", nil, nil, "unknown")
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "over", []string{subscriptionCostHF17SubA})
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "unknown", []string{subscriptionCostHF17SubB})
	}) {
		return
	}

	if !t.Run("6 switch base currency without rewriting budgets", func(t *testing.T) {
		settingsBody := subscriptionCostHF17Request(t, ctx, client, http.MethodPut, server.URL+"/api/subscriptions/settings", `{"base_currency":"USD"}`, http.StatusOK)
		if err := json.Unmarshal(settingsBody, &settings); err != nil {
			t.Fatalf("decode USD settings response: %v", err)
		}
		if settings.BaseCurrency != "USD" {
			t.Fatalf("settings base_currency = %q, want USD", settings.BaseCurrency)
		}

		rows = getSubscriptionCostHF17Rows(t, ctx, client, server.URL, "")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubA], "USD", "USD", new(float64(10)), new(float64(120)), "unknown")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubB], "EUR", "USD", nil, nil, "unknown")
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "unknown", []string{subscriptionCostHF17SubA, subscriptionCostHF17SubB})
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "ok", nil)

		assertSubscriptionCostHF17BudgetListStates(t, ctx, client, server.URL, []subscriptionCostHF17BudgetExpectation{
			{budgetID: globalCNY100.BudgetID, name: globalCNY100.Name, scopeType: "global", scopeID: "", currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "unknown"}},
			{budgetID: vpsCNY100.BudgetID, name: vpsCNY100.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "unknown"}},
			{budgetID: vpsUSD20.BudgetID, name: vpsUSD20.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(20)), status: "ok", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))}},
			{budgetID: vpsCNY80.BudgetID, name: vpsCNY80.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(80)), status: "unknown"}},
			{budgetID: globalCNY60.BudgetID, name: globalCNY60.Name, scopeType: "global", scopeID: "", currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(60)), status: "unknown"}},
		})
		assertSubscriptionCostHF17BudgetFacts(t, ctx, fixture.pool, map[string]subscriptionCostHF17BudgetFact{
			globalCNY100.Name: {scopeType: "global", scopeID: "", currency: "CNY", monthlyLimit: new(float64(100)), warningPct: 80, enabled: true},
			vpsCNY100.Name:    {scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", monthlyLimit: new(float64(100)), warningPct: 80, enabled: true},
			vpsUSD20.Name:     {scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", monthlyLimit: new(float64(20)), warningPct: 80, enabled: true},
			vpsCNY80.Name:     {scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", monthlyLimit: new(float64(80)), warningPct: 80, enabled: true},
			globalCNY60.Name:  {scopeType: "global", scopeID: "", currency: "CNY", monthlyLimit: new(float64(60)), warningPct: 80, enabled: true},
		})
	}) {
		return
	}

	if !t.Run("7 patch currency and nullable dual limits", func(t *testing.T) {
		patchSubscriptionCostHF17Budget(t, ctx, client, server.URL, map[string]any{
			"budget_id":     vpsCNY100.BudgetID,
			"base_currency": "USD",
		}, vpsCNY100.BudgetID, "hf17-vps-cny-100", "vps", subscriptionCostHF17VPSA, "USD",
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "ok", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))})
		vpsCNY100 = patchSubscriptionCostHF17Budget(t, ctx, client, server.URL, map[string]any{
			"budget_id":     vpsCNY100.BudgetID,
			"base_currency": "CNY",
		}, vpsCNY100.BudgetID, "hf17-vps-cny-100", "vps", subscriptionCostHF17VPSA, "CNY",
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "unknown"})

		dual = postSubscriptionCostHF17Budget(t, ctx, client, server.URL,
			"hf17-vps-usd-dual", "vps", subscriptionCostHF17VPSA, "USD", new(float64(10)), new(float64(120)),
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(10)), yearlyLimit: new(float64(120)), status: "over", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))})
		dual = patchSubscriptionCostHF17Budget(t, ctx, client, server.URL, map[string]any{
			"budget_id":    dual.BudgetID,
			"yearly_limit": 240,
		}, dual.BudgetID, dual.Name, "vps", subscriptionCostHF17VPSA, "USD",
			subscriptionCostHF17BudgetState{monthlyLimit: new(float64(10)), yearlyLimit: new(float64(240)), status: "over", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))})
		dual = patchSubscriptionCostHF17Budget(t, ctx, client, server.URL, map[string]any{
			"budget_id":     dual.BudgetID,
			"monthly_limit": nil,
		}, dual.BudgetID, dual.Name, "vps", subscriptionCostHF17VPSA, "USD",
			subscriptionCostHF17BudgetState{monthlyLimit: nil, yearlyLimit: new(float64(240)), status: "ok", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))})

		invalidPatchBody := subscriptionCostHF17Marshal(t, map[string]any{
			"budget_id":    dual.BudgetID,
			"yearly_limit": nil,
		})
		subscriptionCostHF17Request(t, ctx, client, http.MethodPatch, server.URL+"/api/subscription-budgets", invalidPatchBody, http.StatusBadRequest)
		assertSubscriptionCostHF17BudgetListContains(t, ctx, client, server.URL, dual.BudgetID, dual.Name, "vps", subscriptionCostHF17VPSA, "USD",
			subscriptionCostHF17BudgetState{monthlyLimit: nil, yearlyLimit: new(float64(240)), status: "ok", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))})
		assertSubscriptionCostHF17BudgetFacts(t, ctx, fixture.pool, map[string]subscriptionCostHF17BudgetFact{
			globalCNY100.Name: {scopeType: "global", scopeID: "", currency: "CNY", monthlyLimit: new(float64(100)), warningPct: 80, enabled: true},
			vpsCNY100.Name:    {scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", monthlyLimit: new(float64(100)), warningPct: 80, enabled: true},
			vpsUSD20.Name:     {scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", monthlyLimit: new(float64(20)), warningPct: 80, enabled: true},
			vpsCNY80.Name:     {scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", monthlyLimit: new(float64(80)), warningPct: 80, enabled: true},
			globalCNY60.Name:  {scopeType: "global", scopeID: "", currency: "CNY", monthlyLimit: new(float64(60)), warningPct: 80, enabled: true},
			dual.Name:         {scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", yearlyLimit: new(float64(240)), warningPct: 80, enabled: true},
		})
	}) {
		return
	}

	if !t.Run("8 list consistency and restore CNY", func(t *testing.T) {
		assertSubscriptionCostHF17BudgetListStates(t, ctx, client, server.URL, []subscriptionCostHF17BudgetExpectation{
			{budgetID: globalCNY100.BudgetID, name: globalCNY100.Name, scopeType: "global", scopeID: "", currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "unknown"}},
			{budgetID: vpsCNY100.BudgetID, name: vpsCNY100.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "unknown"}},
			{budgetID: vpsUSD20.BudgetID, name: vpsUSD20.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(20)), status: "ok", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))}},
			{budgetID: vpsCNY80.BudgetID, name: vpsCNY80.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(80)), status: "unknown"}},
			{budgetID: globalCNY60.BudgetID, name: globalCNY60.Name, scopeType: "global", scopeID: "", currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(60)), status: "unknown"}},
			{budgetID: dual.BudgetID, name: dual.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", state: subscriptionCostHF17BudgetState{monthlyLimit: nil, yearlyLimit: new(float64(240)), status: "ok", monthlySpend: new(float64(10)), yearlySpend: new(float64(120))}},
		})

		settingsBody := subscriptionCostHF17Request(t, ctx, client, http.MethodPut, server.URL+"/api/subscriptions/settings", `{"base_currency":"CNY"}`, http.StatusOK)
		if err := json.Unmarshal(settingsBody, &settings); err != nil {
			t.Fatalf("decode CNY settings response: %v", err)
		}
		if settings.BaseCurrency != "CNY" {
			t.Fatalf("settings base_currency = %q, want CNY", settings.BaseCurrency)
		}

		assertSubscriptionCostHF17BudgetListStates(t, ctx, client, server.URL, []subscriptionCostHF17BudgetExpectation{
			{budgetID: globalCNY100.BudgetID, name: globalCNY100.Name, scopeType: "global", scopeID: "", currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "unknown"}},
			{budgetID: vpsCNY100.BudgetID, name: vpsCNY100.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(100)), status: "ok", monthlySpend: new(float64(70)), yearlySpend: new(float64(840))}},
			{budgetID: vpsUSD20.BudgetID, name: vpsUSD20.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(20)), status: "unknown"}},
			{budgetID: vpsCNY80.BudgetID, name: vpsCNY80.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(80)), status: "warning", monthlySpend: new(float64(70)), yearlySpend: new(float64(840))}},
			{budgetID: globalCNY60.BudgetID, name: globalCNY60.Name, scopeType: "global", scopeID: "", currency: "CNY", state: subscriptionCostHF17BudgetState{monthlyLimit: new(float64(60)), status: "over"}},
			{budgetID: dual.BudgetID, name: dual.Name, scopeType: "vps", scopeID: subscriptionCostHF17VPSA, currency: "USD", state: subscriptionCostHF17BudgetState{monthlyLimit: nil, yearlyLimit: new(float64(240)), status: "unknown"}},
		})
		rows = getSubscriptionCostHF17Rows(t, ctx, client, server.URL, "")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubA], "USD", "CNY", new(float64(70)), new(float64(840)), "over")
		assertSubscriptionCostHF17Row(t, rows[subscriptionCostHF17SubB], "EUR", "CNY", nil, nil, "unknown")
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "over", []string{subscriptionCostHF17SubA})
		assertSubscriptionCostHF17Filter(t, ctx, client, server.URL, "unknown", []string{subscriptionCostHF17SubB})
	}) {
		return
	}
}

func TestPostgresIntegrationSubscriptionCostHF19(t *testing.T) {
	scenarios := []subscriptionCostHF19Scenario{
		{
			name:               "90 with current missing exchange rate",
			amount:             90,
			currentMissingRate: true,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 90, totalYearly: 1080, currentUnknown: 1,
			},
		},
		{
			name:               "120 with current missing exchange rate retains over",
			amount:             120,
			currentMissingRate: true,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440, currentUnknown: 1,
				riskStatus: "over",
			},
		},
		{
			name:                       "90 with current missing subscription",
			amount:                     90,
			currentMissingSubscription: true,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 90, totalYearly: 1080, currentUnknown: 1,
			},
		},
		{
			name:                       "120 with current missing subscription retains over",
			amount:                     120,
			currentMissingSubscription: true,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440, currentUnknown: 1,
				riskStatus: "over",
			},
		},
		{
			name:   "complete 79",
			amount: 79,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 79, totalYearly: 948,
			},
		},
		{
			name:   "complete 90 warning",
			amount: 90,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 90, totalYearly: 1080,
				riskStatus: "warning", monthlySpend: new(float64(90)),
				yearlySpend: new(float64(1080)),
			},
		},
		{
			name:   "complete 120 over",
			amount: 120,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440,
				riskStatus: "over", monthlySpend: new(float64(120)),
				yearlySpend: new(float64(1440)),
			},
		},
		{
			name:                        "current complete with archived gaps",
			amount:                      90,
			archivedMissingRate:         true,
			archivedMissingSubscription: true,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 90, totalYearly: 1080, archivedUnknown: 2,
				riskStatus: "warning", monthlySpend: new(float64(90)),
				yearlySpend: new(float64(1080)),
			},
		},
		{
			name:   "complete 120 without budget",
			amount: 120,
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440,
			},
		},
		{
			name:   "complete 120 with future-only budget",
			amount: 120,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 12, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440,
			},
		},
		{
			name:   "complete 120 with mismatched currency budget",
			amount: 120,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: 0, currency: "USD", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440,
			},
		},
		{
			name:   "history inherits nearest comparable budget",
			amount: 120,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: -2, currency: "CNY", limit: 200},
				{offset: -1, currency: "CNY", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440,
				riskStatus: "over", monthlySpend: new(float64(120)),
				yearlySpend: new(float64(1440)),
			},
		},
		{
			name:   "history does not fall back past mismatched currency",
			amount: 120,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: -2, currency: "CNY", limit: 100},
				{offset: -1, currency: "USD", limit: 100},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440,
			},
		},
		{
			name:   "future budget does not mask historical budget",
			amount: 120,
			budgets: []subscriptionCostHF19BudgetSeed{
				{offset: -1, currency: "CNY", limit: 100},
				{offset: 12, currency: "CNY", limit: 200},
			},
			want: subscriptionCostHF19Expectation{
				totalMonthly: 120, totalYearly: 1440,
				riskStatus: "over", monthlySpend: new(float64(120)),
				yearlySpend: new(float64(1440)),
			},
		},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
			defer cancel()

			fixture := newRuntimeStreamAuthFixture(t)
			seedSubscriptionCostHF19Facts(t, ctx, fixture.pool, scenario)

			costRepo := store.NewPostgresSubscriptionCostRepository(fixture.pool)
			settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
			costService := subscriptioncosts.NewService(costRepo, settingsRepo, nil)
			authMiddleware := centerhttp.RequireSession(fixture.service, fixture.scope)
			router := centerhttp.New(centerhttp.RouterOptions{
				Version:                           "test",
				AuthLoginHandler:                  handlers.Login(fixture.service),
				AuthLogoutHandler:                 handlers.Logout(fixture.service),
				AuthMeHandler:                     handlers.Me(fixture.service),
				AuthChangePasswordHandler:         handlers.ChangePassword(fixture.service),
				AuthMiddleware:                    authMiddleware,
				SubscriptionOverviewHandler:       handlers.SubscriptionOverview(costService),
				SubscriptionMonthlyBudgetsHandler: handlers.SubscriptionMonthlyBudgets(costService),
			})
			server := httptest.NewTLSServer(router)
			defer server.Close()

			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatalf("new cookie jar: %v", err)
			}
			client := runtimeStreamAuthHTTPClientWithJar(server, jar)
			runtimeStreamAuthLogin(t, client, server.URL, runtimeStreamAuthPassword)

			initial := getSubscriptionCostHF19Overview(t, ctx, client, server.URL)
			for _, budget := range scenario.budgets {
				month := subscriptionCostHF19MonthStart(initial.SnapshotGeneratedAt, budget.offset)
				putSubscriptionCostHF19MonthlyBudget(t, ctx, client, server.URL, month, budget.currency, budget.limit)
			}

			final := getSubscriptionCostHF19Overview(t, ctx, client, server.URL)
			assertSubscriptionCostHF19Overview(t, final, scenario.want)
		})
	}
}

type subscriptionCostHF17Budget struct {
	Raw                 map[string]json.RawMessage `json:"-"`
	BudgetID            string                     `json:"budget_id"`
	ScopeType           string                     `json:"scope_type"`
	ScopeID             string                     `json:"scope_id"`
	Name                string                     `json:"name"`
	BaseCurrency        string                     `json:"base_currency"`
	MonthlyLimit        *float64                   `json:"monthly_limit"`
	YearlyLimit         *float64                   `json:"yearly_limit"`
	WarningPct          int                        `json:"warning_pct"`
	Enabled             bool                       `json:"enabled"`
	CurrentMonthlySpend *float64                   `json:"current_monthly_spend"`
	CurrentYearlySpend  *float64                   `json:"current_yearly_spend"`
	Status              string                     `json:"status"`
}

type subscriptionCostHF17BudgetState struct {
	monthlyLimit *float64
	yearlyLimit  *float64
	monthlySpend *float64
	yearlySpend  *float64
	status       string
}

type subscriptionCostHF17BudgetExpectation struct {
	budgetID  string
	name      string
	scopeType string
	scopeID   string
	currency  string
	state     subscriptionCostHF17BudgetState
}

type subscriptionCostHF17BudgetFact struct {
	scopeType    string
	scopeID      string
	currency     string
	monthlyLimit *float64
	yearlyLimit  *float64
	warningPct   int
	enabled      bool
}

type subscriptionCostHF17Row struct {
	Raw              map[string]json.RawMessage `json:"-"`
	SubscriptionID   string                     `json:"subscription_id"`
	VPSID            string                     `json:"vps_id"`
	Currency         string                     `json:"currency"`
	MonthlyPriceBase *float64                   `json:"monthly_price_base"`
	YearlyPriceBase  *float64                   `json:"yearly_price_base"`
	BaseCurrency     string                     `json:"base_currency"`
	BudgetStatus     string                     `json:"budget_status"`
}

func postSubscriptionCostHF17Budget(t *testing.T, ctx context.Context, client *http.Client, serverURL, name, scopeType, scopeID, currency string, monthlyLimit, yearlyLimit *float64, state subscriptionCostHF17BudgetState) subscriptionCostHF17Budget {
	t.Helper()
	payload := map[string]any{
		"scope_type":    scopeType,
		"scope_id":      scopeID,
		"name":          name,
		"base_currency": currency,
		"monthly_limit": monthlyLimit,
		"yearly_limit":  yearlyLimit,
		"warning_pct":   80,
		"enabled":       true,
	}
	body := subscriptionCostHF17Request(t, ctx, client, http.MethodPost, serverURL+"/api/subscription-budgets", subscriptionCostHF17Marshal(t, payload), http.StatusCreated)
	budget := decodeSubscriptionCostHF17Budget(t, body)
	assertSubscriptionCostHF17Budget(t, budget, name, scopeType, scopeID, currency, state)
	assertSubscriptionCostHF17BudgetListContains(t, ctx, client, serverURL, budget.BudgetID, name, scopeType, scopeID, currency, state)
	return budget
}

func patchSubscriptionCostHF17Budget(t *testing.T, ctx context.Context, client *http.Client, serverURL string, payload map[string]any, budgetID, name, scopeType, scopeID, currency string, state subscriptionCostHF17BudgetState) subscriptionCostHF17Budget {
	t.Helper()
	body := subscriptionCostHF17Request(t, ctx, client, http.MethodPatch, serverURL+"/api/subscription-budgets", subscriptionCostHF17Marshal(t, payload), http.StatusOK)
	budget := decodeSubscriptionCostHF17Budget(t, body)
	assertSubscriptionCostHF17Budget(t, budget, name, scopeType, scopeID, currency, state)
	if budget.BudgetID != budgetID {
		t.Fatalf("patched budget_id = %q, want %q", budget.BudgetID, budgetID)
	}
	assertSubscriptionCostHF17BudgetListContains(t, ctx, client, serverURL, budgetID, name, scopeType, scopeID, currency, state)
	return budget
}

func getSubscriptionCostHF17Budgets(t *testing.T, ctx context.Context, client *http.Client, serverURL string) []subscriptionCostHF17Budget {
	t.Helper()
	body := subscriptionCostHF17Request(t, ctx, client, http.MethodGet, serverURL+"/api/subscription-budgets", "", http.StatusOK)
	var rawRecords []json.RawMessage
	if err := json.Unmarshal(body, &rawRecords); err != nil {
		t.Fatalf("decode subscription budgets: %v; body=%s", err, body)
	}
	records := make([]subscriptionCostHF17Budget, 0, len(rawRecords))
	for _, raw := range rawRecords {
		records = append(records, decodeSubscriptionCostHF17Budget(t, raw))
	}
	return records
}

func assertSubscriptionCostHF17BudgetListContains(t *testing.T, ctx context.Context, client *http.Client, serverURL, budgetID, name, scopeType, scopeID, currency string, state subscriptionCostHF17BudgetState) {
	t.Helper()
	for _, budget := range getSubscriptionCostHF17Budgets(t, ctx, client, serverURL) {
		if budget.BudgetID != budgetID {
			continue
		}
		assertSubscriptionCostHF17Budget(t, budget, name, scopeType, scopeID, currency, state)
		return
	}
	t.Fatalf("budget list did not contain budget_id %q", budgetID)
}

func assertSubscriptionCostHF17BudgetListStates(t *testing.T, ctx context.Context, client *http.Client, serverURL string, expected []subscriptionCostHF17BudgetExpectation) {
	t.Helper()
	got := getSubscriptionCostHF17Budgets(t, ctx, client, serverURL)
	if len(got) != len(expected) {
		t.Fatalf("budget list count = %d, want %d", len(got), len(expected))
	}
	byID := make(map[string]subscriptionCostHF17Budget, len(got))
	for _, budget := range got {
		byID[budget.BudgetID] = budget
	}
	for _, want := range expected {
		budget, ok := byID[want.budgetID]
		if !ok {
			t.Fatalf("budget list missing budget_id %q (%s)", want.budgetID, want.name)
		}
		assertSubscriptionCostHF17Budget(t, budget, want.name, want.scopeType, want.scopeID, want.currency, want.state)
	}
}

func assertSubscriptionCostHF17Budget(t *testing.T, budget subscriptionCostHF17Budget, name, scopeType, scopeID, currency string, state subscriptionCostHF17BudgetState) {
	t.Helper()
	if budget.BudgetID == "" {
		t.Fatalf("budget %q has empty budget_id", name)
	}
	if budget.Name != name || budget.ScopeType != scopeType || budget.ScopeID != scopeID || budget.BaseCurrency != currency {
		t.Fatalf("budget identity = %#v, want name=%q scope=%s/%q currency=%s", budget, name, scopeType, scopeID, currency)
	}
	if budget.WarningPct != 80 || !budget.Enabled {
		t.Fatalf("budget %q warning_pct/enabled = %d/%v, want 80/true", name, budget.WarningPct, budget.Enabled)
	}
	if budget.Status != state.status {
		t.Fatalf("budget %q status = %q, want %q", name, budget.Status, state.status)
	}
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "monthly_limit", state.monthlyLimit)
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "yearly_limit", state.yearlyLimit)
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "current_monthly_spend", state.monthlySpend)
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "current_yearly_spend", state.yearlySpend)
}

func decodeSubscriptionCostHF17Budget(t *testing.T, body []byte) subscriptionCostHF17Budget {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode subscription budget object: %v; body=%s", err, body)
	}
	var budget subscriptionCostHF17Budget
	if err := json.Unmarshal(body, &budget); err != nil {
		t.Fatalf("decode subscription budget fields: %v; body=%s", err, body)
	}
	budget.Raw = raw
	return budget
}

func getSubscriptionCostHF17Rows(t *testing.T, ctx context.Context, client *http.Client, serverURL, budgetStatus string) map[string]subscriptionCostHF17Row {
	t.Helper()
	path := serverURL + "/api/subscriptions"
	if budgetStatus != "" {
		path += "?budget_status=" + budgetStatus
	}
	body := subscriptionCostHF17Request(t, ctx, client, http.MethodGet, path, "", http.StatusOK)
	var rawRecords []json.RawMessage
	if err := json.Unmarshal(body, &rawRecords); err != nil {
		t.Fatalf("decode subscription rows: %v; body=%s", err, body)
	}
	rows := make(map[string]subscriptionCostHF17Row, len(rawRecords))
	for _, raw := range rawRecords {
		var row subscriptionCostHF17Row
		if err := json.Unmarshal(raw, &row); err != nil {
			t.Fatalf("decode subscription row: %v; body=%s", err, raw)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatalf("decode subscription row fields: %v; body=%s", err, raw)
		}
		row.Raw = fields
		if row.SubscriptionID == "" {
			t.Fatalf("subscription row has empty subscription_id: %s", raw)
		}
		rows[row.SubscriptionID] = row
	}
	return rows
}

func assertSubscriptionCostHF17Filter(t *testing.T, ctx context.Context, client *http.Client, serverURL, status string, expected []string) {
	t.Helper()
	rows := getSubscriptionCostHF17Rows(t, ctx, client, serverURL, status)
	if len(rows) != len(expected) {
		t.Fatalf("budget_status=%s row count = %d, want %d (%v)", status, len(rows), len(expected), expected)
	}
	for _, id := range expected {
		if _, ok := rows[id]; !ok {
			t.Fatalf("budget_status=%s missing subscription %q; got %v", status, id, subscriptionCostHF17RowIDs(rows))
		}
	}
}

func assertSubscriptionCostHF17Row(t *testing.T, row subscriptionCostHF17Row, currency, baseCurrency string, monthly, yearly *float64, budgetStatus string) {
	t.Helper()
	if row.SubscriptionID == "" {
		t.Fatalf("missing expected subscription row for currency=%s", currency)
	}
	if row.Currency != currency || row.BaseCurrency != baseCurrency || row.BudgetStatus != budgetStatus {
		t.Fatalf("subscription %q = currency=%s base=%s status=%s, want currency=%s base=%s status=%s", row.SubscriptionID, row.Currency, row.BaseCurrency, row.BudgetStatus, currency, baseCurrency, budgetStatus)
	}
	subscriptionCostHF17AssertOptionalNullableNumber(t, row.Raw, "monthly_price_base", monthly)
	subscriptionCostHF17AssertOptionalNullableNumber(t, row.Raw, "yearly_price_base", yearly)
}

func subscriptionCostHF17RowIDs(rows map[string]subscriptionCostHF17Row) []string {
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func subscriptionCostHF17AssertRequiredNullableNumber(t *testing.T, fields map[string]json.RawMessage, name string, expected *float64) {
	t.Helper()
	raw, ok := fields[name]
	if !ok {
		t.Fatalf("JSON field %q is missing; want number or null", name)
	}
	subscriptionCostHF17AssertNullableNumber(t, raw, name, expected)
}

func subscriptionCostHF17AssertOptionalNullableNumber(t *testing.T, fields map[string]json.RawMessage, name string, expected *float64) {
	t.Helper()
	raw, ok := fields[name]
	if !ok {
		if expected != nil {
			t.Fatalf("JSON field %q is missing; want %.6f", name, *expected)
		}
		return
	}
	subscriptionCostHF17AssertNullableNumber(t, raw, name, expected)
}

func subscriptionCostHF17AssertNullableNumber(t *testing.T, raw json.RawMessage, name string, expected *float64) {
	t.Helper()
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if expected != nil {
			t.Fatalf("JSON field %q = null, want %.6f", name, *expected)
		}
		return
	}
	var actual float64
	if err := json.Unmarshal(raw, &actual); err != nil {
		t.Fatalf("JSON field %q is not a number or null: %v; raw=%s", name, err, raw)
	}
	if expected == nil {
		t.Fatalf("JSON field %q = %.6f, want null", name, actual)
	}
	if math.Abs(actual-*expected) > 0.000001 {
		t.Fatalf("JSON field %q = %.6f, want %.6f", name, actual, *expected)
	}
}

func subscriptionCostHF17Request(t *testing.T, ctx context.Context, client *http.Client, method, url, body string, wantStatus int) []byte {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		t.Fatalf("new %s %s request: %v", method, url, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s request: %v", method, url, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, url, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, url, resp.StatusCode, wantStatus, payload)
	}
	return payload
}

func subscriptionCostHF17Marshal(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON request: %v", err)
	}
	return string(body)
}

func seedSubscriptionCostHF17Facts(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	subscriptionCostHF17ExecSQL(t, ctx, db, `
		insert into providers (provider_id, name)
		values ($1, $2)`, subscriptionCostHF17Provider, "HF17 provider")
	subscriptionCostHF17ExecSQL(t, ctx, db, `
		insert into vps_assets (vps_id, display_name, provider_id, lifecycle_status, usage_status, renewal_decision)
		values
			($1, 'HF17 A', $3, 'active', 'in_use', 'keep'),
			($2, 'HF17 B', $3, 'active', 'in_use', 'keep')`, subscriptionCostHF17VPSA, subscriptionCostHF17VPSB, subscriptionCostHF17Provider)
	subscriptionCostHF17ExecSQL(t, ctx, db, `
		insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, status, created_at
		) values
			($1, $3, 10, 'USD', 'monthly', 1, 10, 'active', now()),
			($2, $4, 5, 'EUR', 'monthly', 1, 5, 'active', now())`, subscriptionCostHF17SubA, subscriptionCostHF17SubB, subscriptionCostHF17VPSA, subscriptionCostHF17VPSB)
	subscriptionCostHF17ExecSQL(t, ctx, db, `
		insert into subscription_exchange_rates (
			rate_id, provider, base_currency, quote_currency, rate, rate_date, fetched_at
		) values ($1, 'frankfurter', 'CNY', 'USD', 7, current_date, now())`, "rate_hf17_usd")
}

func subscriptionCostHF17ExecSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("subscription cost HF17 fixture SQL: %v", err)
	}
}

func assertSubscriptionCostHF17BudgetFacts(t *testing.T, ctx context.Context, db *pgxpool.Pool, expected map[string]subscriptionCostHF17BudgetFact) {
	t.Helper()
	rows, err := db.Query(ctx, `
		select name, scope_type, scope_id, base_currency,
			monthly_limit::float8, yearly_limit::float8, warning_pct, enabled
		from subscription_budgets
		where name like 'hf17-%'
		order by name`)
	if err != nil {
		t.Fatalf("query subscription budget facts: %v", err)
	}
	defer rows.Close()

	got := make(map[string]subscriptionCostHF17BudgetFact, len(expected))
	for rows.Next() {
		var (
			name         string
			scopeType    string
			scopeID      string
			currency     string
			monthlyLimit pgtype.Float8
			yearlyLimit  pgtype.Float8
			warningPct   int
			enabled      bool
		)
		if err := rows.Scan(&name, &scopeType, &scopeID, &currency, &monthlyLimit, &yearlyLimit, &warningPct, &enabled); err != nil {
			t.Fatalf("scan subscription budget facts: %v", err)
		}
		got[name] = subscriptionCostHF17BudgetFact{
			scopeType:    scopeType,
			scopeID:      scopeID,
			currency:     currency,
			monthlyLimit: subscriptionCostHF17PgFloat(monthlyLimit),
			yearlyLimit:  subscriptionCostHF17PgFloat(yearlyLimit),
			warningPct:   warningPct,
			enabled:      enabled,
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate subscription budget facts: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("subscription budget fact count = %d, want %d (%v)", len(got), len(expected), got)
	}
	for name, want := range expected {
		actual, ok := got[name]
		if !ok {
			t.Fatalf("subscription budget facts missing %q", name)
		}
		if actual.scopeType != want.scopeType || actual.scopeID != want.scopeID || actual.currency != want.currency || actual.warningPct != want.warningPct || actual.enabled != want.enabled {
			t.Fatalf("budget facts %q = %#v, want %#v", name, actual, want)
		}
		subscriptionCostHF17AssertFloatPtr(t, name+" monthly_limit", actual.monthlyLimit, want.monthlyLimit)
		subscriptionCostHF17AssertFloatPtr(t, name+" yearly_limit", actual.yearlyLimit, want.yearlyLimit)
	}
}

func subscriptionCostHF17PgFloat(value pgtype.Float8) *float64 {
	if !value.Valid {
		return nil
	}
	actual := value.Float64
	return &actual
}

func subscriptionCostHF17AssertFloatPtr(t *testing.T, name string, actual, expected *float64) {
	t.Helper()
	if actual == nil || expected == nil {
		if actual != nil || expected != nil {
			t.Fatalf("%s = %v, want %v", name, actual, expected)
		}
		return
	}
	if math.Abs(*actual-*expected) > 0.000001 {
		t.Fatalf("%s = %.6f, want %.6f", name, *actual, *expected)
	}
}

type subscriptionCostHF19Scenario struct {
	name                        string
	amount                      float64
	currentMissingRate          bool
	currentMissingSubscription  bool
	archivedMissingRate         bool
	archivedMissingSubscription bool
	budgets                     []subscriptionCostHF19BudgetSeed
	want                        subscriptionCostHF19Expectation
}

type subscriptionCostHF19BudgetSeed struct {
	offset   int
	currency string
	limit    float64
}

type subscriptionCostHF19Expectation struct {
	totalMonthly    float64
	totalYearly     float64
	currentUnknown  int
	archivedUnknown int
	riskStatus      string
	monthlySpend    *float64
	yearlySpend     *float64
}

type subscriptionCostHF19Overview struct {
	Raw                        map[string]json.RawMessage `json:"-"`
	TotalMonthlyCost           float64                    `json:"total_monthly_cost"`
	TotalYearlyCost            float64                    `json:"total_yearly_cost"`
	CurrentUnknownAmountCount  int                        `json:"current_unknown_amount_count"`
	ArchivedUnknownAmountCount int                        `json:"archived_unknown_amount_count"`
	BudgetRiskCount            int                        `json:"budget_risk_count"`
	SnapshotGeneratedAt        time.Time                  `json:"snapshot_generated_at"`
	BudgetRisks                []json.RawMessage          `json:"budget_risks"`
}

func getSubscriptionCostHF19Overview(t *testing.T, ctx context.Context, client *http.Client, serverURL string) subscriptionCostHF19Overview {
	t.Helper()
	body := subscriptionCostHF17Request(t, ctx, client, http.MethodGet, serverURL+"/api/subscriptions/overview", "", http.StatusOK)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode subscription overview fields: %v; body=%s", err, body)
	}
	var overview subscriptionCostHF19Overview
	if err := json.Unmarshal(body, &overview); err != nil {
		t.Fatalf("decode subscription overview: %v; body=%s", err, body)
	}
	if overview.SnapshotGeneratedAt.IsZero() {
		t.Fatalf("subscription overview snapshot_generated_at is zero; body=%s", body)
	}
	overview.Raw = raw
	return overview
}

func putSubscriptionCostHF19MonthlyBudget(t *testing.T, ctx context.Context, client *http.Client, serverURL string, month time.Time, currency string, limit float64) {
	t.Helper()
	path := serverURL + "/api/subscription-monthly-budgets/" + month.UTC().Format("2006-01")
	body := subscriptionCostHF17Marshal(t, map[string]any{
		"base_currency": currency,
		"monthly_limit": limit,
		"warning_pct":   80,
	})
	subscriptionCostHF17Request(t, ctx, client, http.MethodPut, path, body, http.StatusOK)
}

func assertSubscriptionCostHF19Overview(t *testing.T, overview subscriptionCostHF19Overview, want subscriptionCostHF19Expectation) {
	t.Helper()
	if math.Abs(overview.TotalMonthlyCost-want.totalMonthly) > 0.000001 {
		t.Fatalf("total_monthly_cost = %.6f, want %.6f", overview.TotalMonthlyCost, want.totalMonthly)
	}
	if math.Abs(overview.TotalYearlyCost-want.totalYearly) > 0.000001 {
		t.Fatalf("total_yearly_cost = %.6f, want %.6f", overview.TotalYearlyCost, want.totalYearly)
	}
	if overview.CurrentUnknownAmountCount != want.currentUnknown {
		t.Fatalf("current_unknown_amount_count = %d, want %d", overview.CurrentUnknownAmountCount, want.currentUnknown)
	}
	if overview.ArchivedUnknownAmountCount != want.archivedUnknown {
		t.Fatalf("archived_unknown_amount_count = %d, want %d", overview.ArchivedUnknownAmountCount, want.archivedUnknown)
	}
	wantRiskCount := 0
	if want.riskStatus != "" {
		wantRiskCount = 1
	}
	if overview.BudgetRiskCount != wantRiskCount {
		t.Fatalf("budget_risk_count = %d, want %d", overview.BudgetRiskCount, wantRiskCount)
	}
	if len(overview.BudgetRisks) != overview.BudgetRiskCount {
		t.Fatalf("budget_risk_count = %d but budget_risks length = %d", overview.BudgetRiskCount, len(overview.BudgetRisks))
	}
	rawRisks, ok := overview.Raw["budget_risks"]
	if !ok {
		t.Fatal(`overview JSON field "budget_risks" is missing`)
	}
	if wantRiskCount == 0 {
		if !bytes.Equal(bytes.TrimSpace(rawRisks), []byte("null")) {
			t.Fatalf(`overview JSON field "budget_risks" = %s, want null`, rawRisks)
		}
		return
	}

	budget := decodeSubscriptionCostHF17Budget(t, overview.BudgetRisks[0])
	wantBudgetID := "monthly-" + overview.SnapshotGeneratedAt.UTC().Format("2006-01")
	if budget.BudgetID != wantBudgetID {
		t.Fatalf("budget risk id = %q, want %q", budget.BudgetID, wantBudgetID)
	}
	if budget.ScopeType != "global" || budget.Name != "月预算 "+overview.SnapshotGeneratedAt.UTC().Format("2006-01") {
		t.Fatalf("budget risk identity = scope=%q name=%q, want global/current month", budget.ScopeType, budget.Name)
	}
	if budget.BaseCurrency != "CNY" || budget.WarningPct != 80 || !budget.Enabled || string(budget.Status) != want.riskStatus {
		t.Fatalf("budget risk metadata = currency=%q warning=%d enabled=%v status=%q, want CNY/80/true/%q", budget.BaseCurrency, budget.WarningPct, budget.Enabled, budget.Status, want.riskStatus)
	}
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "monthly_limit", new(float64(100)))
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "yearly_limit", nil)
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "current_monthly_spend", want.monthlySpend)
	subscriptionCostHF17AssertRequiredNullableNumber(t, budget.Raw, "current_yearly_spend", want.yearlySpend)
}

func seedSubscriptionCostHF19Facts(t *testing.T, ctx context.Context, db *pgxpool.Pool, scenario subscriptionCostHF19Scenario) {
	t.Helper()
	subscriptionCostHF19InsertSubscription(t, ctx, db, "sub_hf19_fixture_baseline", "vps_"+runtimeStreamAuthMonitoringID, 0, "CNY")
	subscriptionCostHF19InsertVPS(t, ctx, db, subscriptionCostHF19AmountVPS, "HF19 amount VPS", "active")
	subscriptionCostHF19InsertSubscription(t, ctx, db, subscriptionCostHF19AmountSub, subscriptionCostHF19AmountVPS, scenario.amount, "CNY")

	if scenario.currentMissingRate {
		subscriptionCostHF19InsertVPS(t, ctx, db, subscriptionCostHF19CurrentGapVPS, "HF19 current missing rate VPS", "active")
		subscriptionCostHF19InsertSubscription(t, ctx, db, subscriptionCostHF19CurrentGapSub, subscriptionCostHF19CurrentGapVPS, 1, "EUR")
	}
	if scenario.currentMissingSubscription {
		subscriptionCostHF19InsertVPS(t, ctx, db, subscriptionCostHF19CurrentMissingVPS, "HF19 current missing subscription VPS", "active")
	}
	if scenario.archivedMissingRate {
		subscriptionCostHF19InsertVPS(t, ctx, db, subscriptionCostHF19ArchivedGapVPS, "HF19 archived missing rate VPS", "archived")
		subscriptionCostHF19InsertSubscription(t, ctx, db, subscriptionCostHF19ArchivedGapSub, subscriptionCostHF19ArchivedGapVPS, 1, "EUR")
	}
	if scenario.archivedMissingSubscription {
		subscriptionCostHF19InsertVPS(t, ctx, db, subscriptionCostHF19ArchivedMissingVPS, "HF19 archived missing subscription VPS", "archived")
	}
}

func subscriptionCostHF19InsertVPS(t *testing.T, ctx context.Context, db *pgxpool.Pool, vpsID, displayName, lifecycle string) {
	t.Helper()
	subscriptionCostHF19ExecSQL(t, ctx, db, `
		insert into vps_assets (
			vps_id, display_name, lifecycle_status, usage_status, renewal_decision,
			auto_renew_check, archived_at
		) values ($1, $2, $3, 'in_use', 'keep', 'unchecked',
			case when $3 = 'archived' then now() else null end)`,
		vpsID, displayName, lifecycle)
}

func subscriptionCostHF19InsertSubscription(t *testing.T, ctx context.Context, db *pgxpool.Pool, subscriptionID, vpsID string, amount float64, currency string) {
	t.Helper()
	subscriptionCostHF19ExecSQL(t, ctx, db, `
		insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, billing_period_unit, billing_period_length, status, created_at, updated_at
		) values ($1, $2, $3, $4, 'monthly', 1, $3, 'month', 1, 'active', now(), now())`,
		subscriptionID, vpsID, amount, currency)
}

func subscriptionCostHF19ExecSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("subscription cost HF19 fixture SQL: %v", err)
	}
}

func subscriptionCostHF19MonthStart(snapshot time.Time, offset int) time.Time {
	snapshot = snapshot.UTC()
	return time.Date(snapshot.Year(), snapshot.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, offset, 0)
}
