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
	createSubscriptionCalls  int
	createIdempotencyKey     string
	idempotencyByKey         map[string]fakeSubscriptionIdempotency
	patchSubscriptionResult  subscriptions.Record
	patchSubscriptionErr     error
	patchSubscriptionID      string
	patchSubscriptionInput   subscriptions.PatchInput
}

type fakeSubscriptionIdempotency struct {
	digest string
	record subscriptions.Record
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
	f.createSubscriptionCalls++
	f.createSubscriptionInput = input
	if f.createSubscriptionErr != nil {
		return subscriptions.Record{}, f.createSubscriptionErr
	}
	return f.createSubscriptionResult, nil
}

func (f *fakeSubscriptionRepository) CreateSubscriptionIdempotent(_ context.Context, input subscriptions.CreateInput, key string) (subscriptions.Record, bool, error) {
	f.createSubscriptionCalls++
	f.createSubscriptionInput = input
	f.createIdempotencyKey = key
	if f.createSubscriptionErr != nil {
		return subscriptions.Record{}, false, f.createSubscriptionErr
	}
	digest, err := subscriptions.CreateRequestDigest(input)
	if err != nil {
		return subscriptions.Record{}, false, err
	}
	if f.idempotencyByKey == nil {
		f.idempotencyByKey = make(map[string]fakeSubscriptionIdempotency)
	}
	if existing, ok := f.idempotencyByKey[key]; ok {
		if existing.digest != digest {
			return subscriptions.Record{}, false, subscriptions.ErrIdempotencyKeyReused
		}
		return existing.record, true, nil
	}
	f.idempotencyByKey[key] = fakeSubscriptionIdempotency{digest: digest, record: f.createSubscriptionResult}
	return f.createSubscriptionResult, false, nil
}

func (f *fakeSubscriptionRepository) PatchSubscription(_ context.Context, subscriptionID string, input subscriptions.PatchInput) (subscriptions.Record, error) {
	f.patchSubscriptionID = subscriptionID
	f.patchSubscriptionInput = input
	if f.patchSubscriptionErr != nil {
		return subscriptions.Record{}, f.patchSubscriptionErr
	}
	return f.patchSubscriptionResult, nil
}

func validVPSSubscriptionCreatePayload() map[string]any {
	return map[string]any{
		"price":                 12,
		"currency":              "USD",
		"billing_cycle":         "monthly",
		"billing_months":        1,
		"billing_period_unit":   "month",
		"billing_period_length": 1,
		"started_at":            "2026-05-01",
		"renew_at":              "2026-06-01",
		"auto_renew":            false,
		"auto_renew_cancelled":  false,
		"renewal_mode":          "manual",
		"payment_method":        "card",
		"note":                  "production",
	}
}

func marshalVPSSubscriptionCreatePayload(t *testing.T, payload map[string]any) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal VPS subscription create payload: %v", err)
	}
	return string(body)
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
	req.Header.Set("Idempotency-Key", "create-sub-collection-001")
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
	if repo.createIdempotencyKey != "create-sub-collection-001" {
		t.Fatalf("idempotency key = %q, want create-sub-collection-001", repo.createIdempotencyKey)
	}
}

func TestSubscriptionsCollectionCreateRequiresIdempotencyKey(t *testing.T) {
	handler := handlers.SubscriptionsCollection(&fakeSubscriptionRepository{})
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{
		"vps_id":"vps_001",
		"price":12,
		"currency":"USD",
		"billing_months":1
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["code"] != "invalid_idempotency_key" {
		t.Fatalf("code = %q, want invalid_idempotency_key; body=%#v", body["code"], body)
	}
}

func TestSubscriptionsCollectionCreateReplaysSameIdempotencyKey(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeSubscriptionRepository{createSubscriptionResult: subscriptions.Record{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          12,
		Currency:       "USD",
		CreatedAt:      now,
		UpdatedAt:      now,
	}}
	handler := handlers.SubscriptionsCollection(repo)
	body := `{"vps_id":"vps_001","price":12,"currency":"USD","billing_months":1}`

	first := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(body))
	first.Header.Set("Idempotency-Key", "replay-collection-001")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", firstRecorder.Code, http.StatusCreated, firstRecorder.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(body))
	second.Header.Set("Idempotency-Key", "replay-collection-001")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d; body=%s", secondRecorder.Code, http.StatusOK, secondRecorder.Body.String())
	}
	var replayed subscriptions.Record
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("unmarshal replay body: %v", err)
	}
	if replayed.SubscriptionID != "sub_001" {
		t.Fatalf("replayed id = %q, want sub_001", replayed.SubscriptionID)
	}
}

func TestSubscriptionsCollectionCreateRejectsReusedIdempotencyKey(t *testing.T) {
	repo := &fakeSubscriptionRepository{createSubscriptionResult: subscriptions.Record{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          12,
		Currency:       "USD",
	}}
	handler := handlers.SubscriptionsCollection(repo)

	first := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{"vps_id":"vps_001","price":12,"currency":"USD","billing_months":1}`))
	first.Header.Set("Idempotency-Key", "reuse-collection-001")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", firstRecorder.Code, http.StatusCreated, firstRecorder.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(`{"vps_id":"vps_001","price":24,"currency":"USD","billing_months":1}`))
	second.Header.Set("Idempotency-Key", "reuse-collection-001")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf("reuse status = %d, want %d; body=%s", secondRecorder.Code, http.StatusConflict, secondRecorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["code"] != "idempotency_key_reused" {
		t.Fatalf("code = %q, want idempotency_key_reused; body=%#v", body["code"], body)
	}
}

func TestVPSSubscriptionsCreateRejectsMissingRequiredFields(t *testing.T) {
	requiredFields := []string{
		"price",
		"currency",
		"billing_cycle",
		"billing_months",
		"auto_renew",
		"auto_renew_cancelled",
		"payment_method",
		"note",
	}

	for _, field := range requiredFields {
		t.Run(field, func(t *testing.T) {
			payload := validVPSSubscriptionCreatePayload()
			delete(payload, field)
			repo := &fakeSubscriptionRepository{}
			handler := handlers.VPSSubscriptions(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, payload)))
			req.Header.Set("Idempotency-Key", "missing-required-"+field)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if repo.createSubscriptionCalls != 0 {
				t.Fatalf("repository calls = %d, want 0", repo.createSubscriptionCalls)
			}
		})
	}
}

func TestVPSSubscriptionsCreateRejectsNullRequiredFields(t *testing.T) {
	requiredFields := []string{
		"price",
		"currency",
		"billing_cycle",
		"billing_months",
		"auto_renew",
		"auto_renew_cancelled",
		"payment_method",
		"note",
	}

	for _, field := range requiredFields {
		t.Run(field, func(t *testing.T) {
			payload := validVPSSubscriptionCreatePayload()
			payload[field] = nil
			repo := &fakeSubscriptionRepository{}
			handler := handlers.VPSSubscriptions(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, payload)))
			req.Header.Set("Idempotency-Key", "null-required-"+field)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if repo.createSubscriptionCalls != 0 {
				t.Fatalf("repository calls = %d, want 0", repo.createSubscriptionCalls)
			}
		})
	}
}

func TestVPSSubscriptionsCreateRejectsNullOptionalNonNullableFields(t *testing.T) {
	optionalNonNullableFields := []string{
		"billing_period_unit",
		"billing_period_length",
		"renewal_mode",
	}

	for _, field := range optionalNonNullableFields {
		t.Run(field, func(t *testing.T) {
			payload := validVPSSubscriptionCreatePayload()
			payload[field] = nil
			repo := &fakeSubscriptionRepository{}
			handler := handlers.VPSSubscriptions(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, payload)))
			req.Header.Set("Idempotency-Key", "null-optional-"+field)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if repo.createSubscriptionCalls != 0 {
				t.Fatalf("repository calls = %d, want 0", repo.createSubscriptionCalls)
			}
		})
	}
}

func TestVPSSubscriptionsCreateAcceptsAbsentOrNullNullableDates(t *testing.T) {
	tests := []struct {
		name         string
		field        string
		explicitNull bool
	}{
		{name: "started_at/null", field: "started_at", explicitNull: true},
		{name: "started_at/absent", field: "started_at"},
		{name: "renew_at/null", field: "renew_at", explicitNull: true},
		{name: "renew_at/absent", field: "renew_at"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := validVPSSubscriptionCreatePayload()
			if tt.explicitNull {
				payload[tt.field] = nil
			} else {
				delete(payload, tt.field)
			}
			repo := &fakeSubscriptionRepository{}
			handler := handlers.VPSSubscriptions(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, payload)))
			req.Header.Set("Idempotency-Key", "null-date-"+tt.field)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
			}
			if repo.createSubscriptionCalls != 1 {
				t.Fatalf("repository calls = %d, want 1", repo.createSubscriptionCalls)
			}
			if tt.field == "started_at" && repo.createSubscriptionInput.StartedAt != nil {
				t.Fatalf("started_at = %#v, want nil", repo.createSubscriptionInput.StartedAt)
			}
			if tt.field == "renew_at" && repo.createSubscriptionInput.RenewAt != nil {
				t.Fatalf("renew_at = %#v, want nil", repo.createSubscriptionInput.RenewAt)
			}
		})
	}
}

func TestVPSSubscriptionsCreatePreservesExplicitZeroFalseAndBlankValues(t *testing.T) {
	payload := validVPSSubscriptionCreatePayload()
	payload["price"] = 0
	payload["auto_renew"] = false
	payload["auto_renew_cancelled"] = false
	payload["payment_method"] = ""
	payload["note"] = ""
	repo := &fakeSubscriptionRepository{}
	handler := handlers.VPSSubscriptions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, payload)))
	req.Header.Set("Idempotency-Key", "explicit-zero-values")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createSubscriptionCalls != 1 {
		t.Fatalf("repository calls = %d, want 1", repo.createSubscriptionCalls)
	}
	input := repo.createSubscriptionInput
	if input.Price != 0 || input.AutoRenew || input.AutoRenewCancelled || input.PaymentMethod != "" || input.Note != "" {
		t.Fatalf("create input = %#v, want exact explicit zero, false, and blank values", input)
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
		"auto_renew_cancelled":false,
		"payment_method":" card ",
		"note":" billing fact "
	}`))
	req.Header.Set("Idempotency-Key", "create-sub-vps-001")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createIdempotencyKey != "create-sub-vps-001" {
		t.Fatalf("idempotency key = %q, want create-sub-vps-001", repo.createIdempotencyKey)
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

func TestVPSSubscriptionsCreateAcceptsRealWorkbenchFormPayload(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeSubscriptionRepository{createSubscriptionResult: subscriptions.Record{
		SubscriptionID:      "sub_form",
		VPSID:               "vps_001",
		Price:               12,
		Currency:            "USD",
		BillingCycle:        "monthly",
		BillingMonths:       1,
		BillingPeriodUnit:   string(subscriptions.BillingPeriodMonth),
		BillingPeriodLength: 1,
		RenewalMode:         string(subscriptions.RenewalModeManual),
		Status:              subscriptions.StatusActive,
		CreatedAt:           now,
		UpdatedAt:           now,
	}}
	handler := handlers.VPSSubscriptions(repo)
	// Real Overview / Legacy form body from buildSubscriptionInput:
	// billing_period_unit, billing_period_length, and renewal_mode are always sent.
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(`{
		"price":12,
		"currency":"USD",
		"billing_cycle":"monthly",
		"billing_months":1,
		"billing_period_unit":"month",
		"billing_period_length":1,
		"started_at":"2026-05-01",
		"renew_at":"2026-06-01",
		"auto_renew":false,
		"auto_renew_cancelled":false,
		"renewal_mode":"manual",
		"payment_method":"card",
		"note":"production"
	}`))
	req.Header.Set("Idempotency-Key", "form-sub-vps-001")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createSubscriptionInput.BillingPeriodUnit != string(subscriptions.BillingPeriodMonth) {
		t.Fatalf("billing_period_unit = %q, want month", repo.createSubscriptionInput.BillingPeriodUnit)
	}
	if repo.createSubscriptionInput.BillingPeriodLength != 1 {
		t.Fatalf("billing_period_length = %d, want 1", repo.createSubscriptionInput.BillingPeriodLength)
	}
	if repo.createSubscriptionInput.RenewalMode != string(subscriptions.RenewalModeManual) {
		t.Fatalf("renewal_mode = %q, want manual", repo.createSubscriptionInput.RenewalMode)
	}
	if repo.createSubscriptionInput.VPSID != "vps_001" {
		t.Fatalf("vps id = %q, want vps_001", repo.createSubscriptionInput.VPSID)
	}
}

func TestVPSSubscriptionsCreateRequiresIdempotencyKey(t *testing.T) {
	handler := handlers.VPSSubscriptions(&fakeSubscriptionRepository{})
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, validVPSSubscriptionCreatePayload())))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["code"] != "invalid_idempotency_key" {
		t.Fatalf("code = %q, want invalid_idempotency_key; body=%#v", body["code"], body)
	}
}

func TestVPSSubscriptionsCreateReplaysSameIdempotencyKey(t *testing.T) {
	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	repo := &fakeSubscriptionRepository{createSubscriptionResult: subscriptions.Record{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          12,
		Currency:       "USD",
		CreatedAt:      now,
		UpdatedAt:      now,
	}}
	handler := handlers.VPSSubscriptions(repo)
	body := marshalVPSSubscriptionCreatePayload(t, validVPSSubscriptionCreatePayload())

	first := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(body))
	first.Header.Set("Idempotency-Key", "replay-sub-001")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", firstRecorder.Code, http.StatusCreated, firstRecorder.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(body))
	second.Header.Set("Idempotency-Key", "replay-sub-001")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d; body=%s", secondRecorder.Code, http.StatusOK, secondRecorder.Body.String())
	}
	var replayed subscriptions.Record
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &replayed); err != nil {
		t.Fatalf("unmarshal replay body: %v", err)
	}
	if replayed.SubscriptionID != "sub_001" {
		t.Fatalf("replayed id = %q, want sub_001", replayed.SubscriptionID)
	}
}

func TestVPSSubscriptionsCreateRejectsReusedIdempotencyKey(t *testing.T) {
	repo := &fakeSubscriptionRepository{createSubscriptionResult: subscriptions.Record{
		SubscriptionID: "sub_001",
		VPSID:          "vps_001",
		Price:          12,
		Currency:       "USD",
	}}
	handler := handlers.VPSSubscriptions(repo)
	firstPayload := validVPSSubscriptionCreatePayload()

	first := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, firstPayload)))
	first.Header.Set("Idempotency-Key", "reuse-sub-001")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body=%s", firstRecorder.Code, http.StatusCreated, firstRecorder.Body.String())
	}

	secondPayload := validVPSSubscriptionCreatePayload()
	secondPayload["price"] = 24
	second := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, secondPayload)))
	second.Header.Set("Idempotency-Key", "reuse-sub-001")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf("reuse status = %d, want %d; body=%s", secondRecorder.Code, http.StatusConflict, secondRecorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["code"] != "idempotency_key_reused" {
		t.Fatalf("code = %q, want idempotency_key_reused; body=%#v", body["code"], body)
	}
}

func TestVPSSubscriptionsRejectsStatusField(t *testing.T) {
	repo := &fakeSubscriptionRepository{}
	handler := handlers.VPSSubscriptions(repo)
	payload := validVPSSubscriptionCreatePayload()
	payload["status"] = "paused"
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/subscriptions", strings.NewReader(marshalVPSSubscriptionCreatePayload(t, payload)))
	req.Header.Set("Idempotency-Key", "reject-status-field")
	if _, err := subscriptions.NormalizeIdempotencyKey(req.Header.Get("Idempotency-Key")); err != nil {
		t.Fatalf("status-field rejection regression must use a valid idempotency key: %v", err)
	}
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if len(body) != 1 || body["error"] != "invalid json" {
		t.Fatalf("body = %#v, want exact invalid json error", body)
	}
	if repo.createSubscriptionCalls != 0 || repo.createIdempotencyKey != "" {
		t.Fatalf("create calls/key = %d/%q, want zero calls and no idempotency create", repo.createSubscriptionCalls, repo.createIdempotencyKey)
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
		{name: "vps scoped create", handler: handlers.VPSSubscriptions(&fakeSubscriptionRepository{createSubscriptionErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/vps/vps_001/subscriptions", body: marshalVPSSubscriptionCreatePayload(t, validVPSSubscriptionCreatePayload())},
		{name: "get", handler: handlers.SubscriptionItem(&fakeSubscriptionRepository{getSubscriptionErr: errors.New("get failed")}), method: http.MethodGet, path: "/api/subscriptions/sub_001"},
		{name: "patch", handler: handlers.SubscriptionItem(&fakeSubscriptionRepository{patchSubscriptionErr: errors.New("patch failed")}), method: http.MethodPatch, path: "/api/subscriptions/sub_001", body: `{"note":"review"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.name == "create" || tt.name == "vps scoped create" {
				req.Header.Set("Idempotency-Key", "create-fail-sub-001")
			}
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
		})
	}
}
