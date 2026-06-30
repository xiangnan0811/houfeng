package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

type fakeSubscriptionRepository struct {
	listSubscriptionsResult  []subscriptions.Record
	listSubscriptionsErr     error
	listSubscriptionsFilter  subscriptions.ListFilters
	getSubscriptionResult    subscriptions.Record
	getSubscriptionErr       error
	getSubscriptionID        string
	createSubscriptionResult subscriptions.Record
	createSubscriptionErr    error
	createSubscriptionInput  subscriptions.CreateInput
	patchSubscriptionResult  subscriptions.Record
	patchSubscriptionErr     error
	patchSubscriptionID      string
	patchSubscriptionInput   subscriptions.PatchInput
}

func (f *fakeSubscriptionRepository) ListSubscriptions(_ context.Context, filters subscriptions.ListFilters) ([]subscriptions.Record, error) {
	f.listSubscriptionsFilter = filters
	return f.listSubscriptionsResult, f.listSubscriptionsErr
}

func (f *fakeSubscriptionRepository) GetSubscription(_ context.Context, subscriptionID string) (subscriptions.Record, error) {
	f.getSubscriptionID = subscriptionID
	if f.getSubscriptionErr != nil {
		return subscriptions.Record{}, f.getSubscriptionErr
	}
	return f.getSubscriptionResult, nil
}

func (f *fakeSubscriptionRepository) CreateSubscription(_ context.Context, input subscriptions.CreateInput) (subscriptions.Record, error) {
	f.createSubscriptionInput = input
	if f.createSubscriptionErr != nil {
		return subscriptions.Record{}, f.createSubscriptionErr
	}
	return f.createSubscriptionResult, nil
}

func (f *fakeSubscriptionRepository) PatchSubscription(_ context.Context, subscriptionID string, input subscriptions.PatchInput) (subscriptions.Record, error) {
	f.patchSubscriptionID = subscriptionID
	f.patchSubscriptionInput = input
	if f.patchSubscriptionErr != nil {
		return subscriptions.Record{}, f.patchSubscriptionErr
	}
	return f.patchSubscriptionResult, nil
}

func TestSubscriptionsCollectionListsSubscriptionsWithFilters(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	repo := &fakeSubscriptionRepository{listSubscriptionsResult: []subscriptions.Record{{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          120,
		Currency:       "USD",
		BillingMonths:  12,
		MonthlyPrice:   10,
		RenewAt:        &renewAt,
		Status:         subscriptions.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}}}

	handler := handlers.SubscriptionsCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions?vps_id=+vps_001+&status=+active+&renew_before=2026-07-01&renew_after=2026-05-01&renew_within_days=30&sort=renew_at&order=desc&asset_scope=archived", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listSubscriptionsFilter.VPSID != "vps_001" ||
		repo.listSubscriptionsFilter.Status != subscriptions.StatusActive ||
		repo.listSubscriptionsFilter.RenewBefore == nil ||
		repo.listSubscriptionsFilter.RenewAfter == nil ||
		repo.listSubscriptionsFilter.RenewWithinDays == nil ||
		*repo.listSubscriptionsFilter.RenewWithinDays != 30 ||
		repo.listSubscriptionsFilter.Order != subscriptions.OrderDesc ||
		repo.listSubscriptionsFilter.AssetScope != vpsassets.AssetScopeArchived {
		t.Fatalf("filters = %#v, want normalized query filters", repo.listSubscriptionsFilter)
	}
	var body []subscriptions.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].SubscriptionID != "sub_001" || body[0].RenewAt == nil {
		t.Fatalf("body = %#v, want subscription list", body)
	}
}

func TestSubscriptionsCollectionAcceptsHistoricalAssetScope(t *testing.T) {
	repo := &fakeSubscriptionRepository{}
	handler := handlers.SubscriptionsCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions?asset_scope=historical", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listSubscriptionsFilter.AssetScope != vpsassets.AssetScope("historical") {
		t.Fatalf("asset scope = %q, want historical", repo.listSubscriptionsFilter.AssetScope)
	}
}

func TestSubscriptionsCollectionDefaultsToCurrentAssetScope(t *testing.T) {
	repo := &fakeSubscriptionRepository{}
	handler := handlers.SubscriptionsCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listSubscriptionsFilter.AssetScope != vpsassets.AssetScopeCurrent {
		t.Fatalf("asset scope = %q, want current", repo.listSubscriptionsFilter.AssetScope)
	}
}

func TestSubscriptionsCollectionCreatesSubscription(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	startedAt := subscriptions.NewDate(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	repo := &fakeSubscriptionRepository{createSubscriptionResult: subscriptions.Record{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          120,
		Currency:       "USD",
		BillingCycle:   "annual",
		BillingMonths:  12,
		MonthlyPrice:   10,
		StartedAt:      &startedAt,
		RenewAt:        &renewAt,
		AutoRenew:      true,
		Status:         subscriptions.StatusActive,
		PaymentMethod:  "card",
		Note:           "production",
		CreatedAt:      now,
		UpdatedAt:      now,
	}}

	handler := handlers.SubscriptionsCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{
		"vps_id":" vps_001 ",
		"price":120,
		"currency":" usd ",
		"billing_cycle":" annual ",
		"billing_months":12,
		"started_at":"2026-01-01",
		"renew_at":"2026-06-01",
		"auto_renew":true,
		"payment_method":" card ",
		"note":" production "
	}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createSubscriptionInput.VPSID != "vps_001" {
		t.Fatalf("create vps id = %q, want trimmed vps_001", repo.createSubscriptionInput.VPSID)
	}
	if repo.createSubscriptionInput.Currency != "USD" {
		t.Fatalf("create currency = %q, want USD", repo.createSubscriptionInput.Currency)
	}
	if repo.createSubscriptionInput.Status != subscriptions.StatusActive {
		t.Fatalf("create status = %q, want default active", repo.createSubscriptionInput.Status)
	}
	if repo.createSubscriptionInput.StartedAt == nil || repo.createSubscriptionInput.RenewAt == nil {
		t.Fatalf("create dates = %#v/%#v, want parsed dates", repo.createSubscriptionInput.StartedAt, repo.createSubscriptionInput.RenewAt)
	}
	var body subscriptions.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.SubscriptionID != "sub_001" || body.MonthlyPrice != 10 {
		t.Fatalf("body = %#v, want created subscription", body)
	}
}

func TestVPSSubscriptionsCreatesBillingFactWithoutUserStatus(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	repo := &fakeSubscriptionRepository{createSubscriptionResult: subscriptions.Record{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          12,
		Currency:       "USD",
		BillingCycle:   "monthly",
		BillingMonths:  1,
		MonthlyPrice:   12,
		RenewAt:        &renewAt,
		AutoRenew:      true,
		Status:         subscriptions.StatusActive,
		PaymentMethod:  "card",
		Note:           "billing fact",
		CreatedAt:      now,
		UpdatedAt:      now,
	}}

	handler := handlers.VPSSubscriptions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(`{
		"price":12,
		"currency":" usd ",
		"billing_cycle":" monthly ",
		"billing_months":1,
		"renew_at":"2026-06-01",
		"auto_renew":true,
		"payment_method":" card ",
		"note":" billing fact "
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createSubscriptionInput.VPSID != "vps_001" {
		t.Fatalf("create vps id = %q, want scoped vps_001", repo.createSubscriptionInput.VPSID)
	}
	if repo.createSubscriptionInput.Status != subscriptions.StatusActive {
		t.Fatalf("create status = %q, want internal default active", repo.createSubscriptionInput.Status)
	}
	if repo.createSubscriptionInput.Currency != "USD" || repo.createSubscriptionInput.PaymentMethod != "card" || repo.createSubscriptionInput.Note != "billing fact" {
		t.Fatalf("create input = %#v, want normalized billing fact", repo.createSubscriptionInput)
	}
	var body subscriptions.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.SubscriptionID != "sub_001" || body.VPSID != "vps_001" {
		t.Fatalf("body = %#v, want created scoped subscription", body)
	}
}

func TestVPSSubscriptionsRejectsStatusField(t *testing.T) {
	handler := handlers.VPSSubscriptions(&fakeSubscriptionRepository{})
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(`{"price":12,"currency":"USD","billing_months":1,"status":"paused"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestVPSSubscriptionsListsOnlyScopedVPSSubscriptions(t *testing.T) {
	repo := &fakeSubscriptionRepository{}
	handler := handlers.VPSSubscriptions(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/subscriptions?status=active&order=desc", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listSubscriptionsFilter.VPSID != "vps_001" {
		t.Fatalf("filter vps id = %q, want vps_001", repo.listSubscriptionsFilter.VPSID)
	}
	if repo.listSubscriptionsFilter.Status != subscriptions.StatusActive || repo.listSubscriptionsFilter.Order != subscriptions.OrderDesc {
		t.Fatalf("filters = %#v, want normalized query filters plus scoped vps", repo.listSubscriptionsFilter)
	}
}

func TestSubscriptionsCollectionRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "blank vps", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":" ","price":12,"currency":"USD","billing_months":1}`},
		{name: "negative price", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":-1,"currency":"USD","billing_months":1}`},
		{name: "too many price decimals", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":12.345,"currency":"USD","billing_months":1}`},
		{name: "zero billing months", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":12,"currency":"USD","billing_months":0}`},
		{name: "invalid currency", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":12,"currency":"US1","billing_months":1}`},
		{name: "invalid status", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":12,"currency":"USD","billing_months":1,"status":"online"}`},
		{name: "unknown field monthly price", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":12,"currency":"USD","billing_months":1,"monthly_price":12}`},
		{name: "invalid date", method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":12,"currency":"USD","billing_months":1,"renew_at":"tomorrow"}`},
		{name: "invalid filter status", method: http.MethodGet, path: "/api/subscriptions?status=online"},
		{name: "invalid filter date", method: http.MethodGet, path: "/api/subscriptions?renew_before=tomorrow"},
		{name: "invalid filter window", method: http.MethodGet, path: "/api/subscriptions?renew_within_days=soon"},
		{name: "negative filter window", method: http.MethodGet, path: "/api/subscriptions?renew_within_days=-1"},
		{name: "invalid sort", method: http.MethodGet, path: "/api/subscriptions?sort=price"},
		{name: "invalid order", method: http.MethodGet, path: "/api/subscriptions?order=later"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.SubscriptionsCollection(&fakeSubscriptionRepository{})
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestSubscriptionItemGetsSubscription(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeSubscriptionRepository{getSubscriptionResult: subscriptions.Record{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          120,
		Currency:       "USD",
		BillingMonths:  12,
		MonthlyPrice:   10,
		Status:         subscriptions.StatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}}

	handler := handlers.SubscriptionItem(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/sub_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.getSubscriptionID != "sub_001" {
		t.Fatalf("get subscription id = %q, want sub_001", repo.getSubscriptionID)
	}
	var body subscriptions.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.SubscriptionID != "sub_001" {
		t.Fatalf("subscription_id = %q, want sub_001", body.SubscriptionID)
	}
}

func TestSubscriptionItemPatchesSubscription(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	renewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC))
	repo := &fakeSubscriptionRepository{patchSubscriptionResult: subscriptions.Record{
		SubscriptionID:     "sub_001",
		VPSID:              "vps_002",
		Price:              240,
		Currency:           "EUR",
		BillingCycle:       "biennial",
		BillingMonths:      24,
		MonthlyPrice:       10,
		StartedAt:          nil,
		RenewAt:            &renewAt,
		AutoRenew:          false,
		AutoRenewCancelled: true,
		Status:             subscriptions.StatusPaused,
		PaymentMethod:      "paypal",
		Note:               "review",
		CreatedAt:          now.Add(-time.Hour),
		UpdatedAt:          now,
	}}

	handler := handlers.SubscriptionItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/subscriptions/sub_001", strings.NewReader(`{
		"vps_id":" vps_002 ",
		"price":240,
		"currency":" eur ",
		"billing_cycle":" biennial ",
		"billing_months":24,
		"started_at":null,
		"renew_at":"2026-12-01",
		"auto_renew":false,
		"auto_renew_cancelled":true,
		"status":"paused",
		"payment_method":" paypal ",
		"note":" review "
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.patchSubscriptionID != "sub_001" {
		t.Fatalf("patch subscription id = %q, want sub_001", repo.patchSubscriptionID)
	}
	if !repo.patchSubscriptionInput.VPSID.Set || repo.patchSubscriptionInput.VPSID.Value != "vps_002" {
		t.Fatalf("patch vps id = %#v, want trimmed vps", repo.patchSubscriptionInput.VPSID)
	}
	if !repo.patchSubscriptionInput.Currency.Set || repo.patchSubscriptionInput.Currency.Value != "EUR" {
		t.Fatalf("patch currency = %#v, want EUR", repo.patchSubscriptionInput.Currency)
	}
	if !repo.patchSubscriptionInput.StartedAt.Set || repo.patchSubscriptionInput.StartedAt.Value != nil {
		t.Fatalf("patch started_at = %#v, want explicit null", repo.patchSubscriptionInput.StartedAt)
	}
	if !repo.patchSubscriptionInput.RenewAt.Set || repo.patchSubscriptionInput.RenewAt.Value == nil {
		t.Fatalf("patch renew_at = %#v, want parsed date", repo.patchSubscriptionInput.RenewAt)
	}
	var body subscriptions.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.SubscriptionID != "sub_001" || body.MonthlyPrice != 10 || body.StartedAt != nil {
		t.Fatalf("body = %#v, want patched subscription", body)
	}
}

func TestSubscriptionItemReturnsNotFound(t *testing.T) {
	tests := []struct {
		name   string
		method string
		repo   *fakeSubscriptionRepository
	}{
		{name: "get", method: http.MethodGet, repo: &fakeSubscriptionRepository{getSubscriptionErr: subscriptions.ErrSubscriptionNotFound}},
		{name: "patch", method: http.MethodPatch, repo: &fakeSubscriptionRepository{patchSubscriptionErr: subscriptions.ErrSubscriptionNotFound}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.SubscriptionItem(tt.repo)
			req := httptest.NewRequest(tt.method, "/api/subscriptions/sub_missing", strings.NewReader(`{"note":"review"}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
			var response map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if response["error"] != "subscription not found" {
				t.Fatalf("error = %q, want subscription not found", response["error"])
			}
		})
	}
}

func TestSubscriptionItemRejectsInvalidPatchAndDeeperPaths(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "blank vps", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"vps_id":" "}`, want: http.StatusBadRequest},
		{name: "negative price", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"price":-1}`, want: http.StatusBadRequest},
		{name: "too many price decimals", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"price":12.345}`, want: http.StatusBadRequest},
		{name: "zero billing months", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"billing_months":0}`, want: http.StatusBadRequest},
		{name: "invalid currency", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"currency":"US1"}`, want: http.StatusBadRequest},
		{name: "invalid status", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"status":"online"}`, want: http.StatusBadRequest},
		{name: "invalid date", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"renew_at":"soon"}`, want: http.StatusBadRequest},
		{name: "unknown field monthly price", method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"monthly_price":10}`, want: http.StatusBadRequest},
		{name: "deeper path", method: http.MethodGet, path: "/api/subscriptions/sub_001/links", body: ``, want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.SubscriptionItem(&fakeSubscriptionRepository{})
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestSubscriptionsMapInvalidVPSReferenceToBadRequest(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{name: "create", handler: handlers.SubscriptionsCollection(&fakeSubscriptionRepository{createSubscriptionErr: subscriptions.ErrInvalidSubscriptionInput}), method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_missing","price":12,"currency":"USD","billing_months":1}`},
		{name: "patch", handler: handlers.SubscriptionItem(&fakeSubscriptionRepository{patchSubscriptionErr: subscriptions.ErrInvalidSubscriptionInput}), method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"vps_id":"vps_missing"}`},
		{name: "list", handler: handlers.SubscriptionsCollection(&fakeSubscriptionRepository{listSubscriptionsErr: subscriptions.ErrInvalidSubscriptionInput}), method: http.MethodGet, path: "/api/subscriptions", body: ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestSubscriptionsUnsupportedMethodsReturnMethodNotAllowed(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
	}{
		{name: "collection", handler: handlers.SubscriptionsCollection(&fakeSubscriptionRepository{}), method: http.MethodDelete, path: "/api/subscriptions"},
		{name: "vps scoped", handler: handlers.VPSSubscriptions(&fakeSubscriptionRepository{}), method: http.MethodDelete, path: "/api/vps/vps_001/subscriptions"},
		{name: "item", handler: handlers.SubscriptionItem(&fakeSubscriptionRepository{}), method: http.MethodPost, path: "/api/subscriptions/sub_001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestSubscriptionsMapRepositoryFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{name: "list", handler: handlers.SubscriptionsCollection(&fakeSubscriptionRepository{listSubscriptionsErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/subscriptions"},
		{name: "create", handler: handlers.SubscriptionsCollection(&fakeSubscriptionRepository{createSubscriptionErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/subscriptions", body: `{"vps_id":"vps_001","price":12,"currency":"USD","billing_months":1}`},
		{name: "vps scoped list", handler: handlers.VPSSubscriptions(&fakeSubscriptionRepository{listSubscriptionsErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/vps/vps_001/subscriptions"},
		{name: "vps scoped create", handler: handlers.VPSSubscriptions(&fakeSubscriptionRepository{createSubscriptionErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/vps/vps_001/subscriptions", body: `{"price":12,"currency":"USD","billing_months":1}`},
		{name: "get", handler: handlers.SubscriptionItem(&fakeSubscriptionRepository{getSubscriptionErr: errors.New("get failed")}), method: http.MethodGet, path: "/api/subscriptions/sub_001"},
		{name: "patch", handler: handlers.SubscriptionItem(&fakeSubscriptionRepository{patchSubscriptionErr: errors.New("patch failed")}), method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"note":"review"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
		})
	}
}
