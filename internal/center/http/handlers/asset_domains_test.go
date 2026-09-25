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

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

type fakeAssetDomainRepository struct {
	listResult         []assetdomains.Record
	listErr            error
	listFilters        assetdomains.ListFilters
	listForVPSResult   []assetdomains.Record
	listForVPSErr      error
	listForVPSID       string
	createResult       assetdomains.Record
	createErr          error
	createInput        assetdomains.CreateInput
	createCalls        int
	updateStatusResult assetdomains.Record
	updateStatusErr    error
	updateStatusID     string
	updateStatus       assetdomains.DomainStatus
	updateStatusReason string
	updateStatusCalls  int
	idempotentCalls    int
	idempotentKey      string
	replayed           bool
}

type statefulAssetDomainRepository struct {
	creates testIdempotentCreateState[assetdomains.Record]
}

func (r *statefulAssetDomainRepository) ListAssetDomainsForVPS(context.Context, string) ([]assetdomains.Record, error) {
	return nil, nil
}

func (r *statefulAssetDomainRepository) CreateAssetDomainIdempotent(_ context.Context, input assetdomains.CreateInput, key string) (assetdomains.Record, bool, error) {
	identity := struct {
		VPSID string
		Input assetdomains.CreateInput
	}{VPSID: input.VPSID, Input: input}
	return r.creates.create(key, identity, func() assetdomains.Record {
		return assetdomains.Record{
			DomainID:   "dom_sequence_001",
			VPSID:      input.VPSID,
			DomainName: input.DomainName,
			Status:     input.Status,
		}
	})
}

func (f *fakeAssetDomainRepository) ListAssetDomains(_ context.Context, filters assetdomains.ListFilters) ([]assetdomains.Record, error) {
	f.listFilters = filters
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeAssetDomainRepository) ListAssetDomainsForVPS(_ context.Context, vpsID string) ([]assetdomains.Record, error) {
	f.listForVPSID = vpsID
	if f.listForVPSErr != nil {
		return nil, f.listForVPSErr
	}
	return f.listForVPSResult, nil
}

func (f *fakeAssetDomainRepository) CreateAssetDomain(_ context.Context, input assetdomains.CreateInput) (assetdomains.Record, error) {
	f.createCalls++
	f.createInput = input
	if f.createErr != nil {
		return assetdomains.Record{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeAssetDomainRepository) UpdateStatus(_ context.Context, domainID string, status assetdomains.DomainStatus, reason string) (assetdomains.Record, error) {
	f.updateStatusCalls++
	f.updateStatusID = domainID
	f.updateStatus = status
	f.updateStatusReason = reason
	if f.updateStatusErr != nil {
		return assetdomains.Record{}, f.updateStatusErr
	}
	return f.updateStatusResult, nil
}

func (f *fakeAssetDomainRepository) CreateAssetDomainIdempotent(_ context.Context, input assetdomains.CreateInput, key string) (assetdomains.Record, bool, error) {
	f.idempotentCalls++
	f.createInput = input
	f.idempotentKey = key
	if f.createErr != nil {
		return assetdomains.Record{}, false, f.createErr
	}
	return f.createResult, f.replayed, nil
}

func TestAssetDomainsCollectionListsDomains(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	repo := &fakeAssetDomainRepository{listResult: []assetdomains.Record{{
		DomainID:   "dom_001",
		VPSID:      "vps_001",
		DomainName: "www.example.com",
		Status:     assetdomains.DomainStatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}}}

	handler := handlers.AssetDomainsCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/domains?vps_id=+vps_001+&service_id=svc_001&target_id=tg_001&status=active", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.listFilters.VPSID != "vps_001" ||
		repo.listFilters.ServiceID != "svc_001" ||
		repo.listFilters.TargetID != "tg_001" ||
		repo.listFilters.Status != assetdomains.DomainStatusActive {
		t.Fatalf("filters = %#v, want normalized filters", repo.listFilters)
	}
	var body []assetdomains.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if len(body) != 1 || body[0].DomainID != "dom_001" {
		t.Fatalf("response count/ID matches = %d/%t", len(body), len(body) == 1 && body[0].DomainID == "dom_001")
	}
}

func TestAssetDomainsCollectionCreatesDomain(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	serviceID := "svc_001"
	targetID := "tg_001"
	expiresAt := subscriptions.NewDate(time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC))
	repo := &fakeAssetDomainRepository{createResult: assetdomains.Record{
		DomainID:     "dom_001",
		VPSID:        "vps_001",
		ServiceID:    &serviceID,
		TargetID:     &targetID,
		DomainName:   "www.example.com",
		Purpose:      "site",
		Status:       assetdomains.DomainStatusActive,
		Registrar:    "NameSilo",
		ExpiresAt:    &expiresAt,
		AutoRenew:    true,
		HTTPSEnabled: true,
		Labels:       []string{"prod"},
		CreatedAt:    now,
		UpdatedAt:    now,
	}}

	handler := handlers.AssetDomainsCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/domains", strings.NewReader(`{
		"vps_id":" vps_001 ",
		"service_id":" svc_001 ",
		"target_id":" tg_001 ",
		"domain_name":" WWW.Example.COM. ",
		"purpose":" site ",
		"status":"active",
		"registrar":" NameSilo ",
		"expires_at":"2026-07-01",
		"auto_renew":true,
		"https_enabled":true,
		"labels":[" prod ","","prod"]
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if repo.createInput.VPSID != "vps_001" || repo.createInput.ServiceID == nil || *repo.createInput.ServiceID != "svc_001" || repo.createInput.TargetID == nil || *repo.createInput.TargetID != "tg_001" {
		t.Fatalf("create identity = %#v, want trimmed ids", repo.createInput)
	}
	if repo.createInput.DomainName != "www.example.com" || repo.createInput.Purpose != "site" || repo.createInput.Registrar != "NameSilo" {
		t.Fatalf("create input = %#v, want normalized text", repo.createInput)
	}
	if repo.createInput.ExpiresAt == nil || !repo.createInput.ExpiresAt.Time.Equal(expiresAt.Time) {
		t.Fatalf("expires_at = %#v, want 2026-07-01", repo.createInput.ExpiresAt)
	}
	if len(repo.createInput.Labels) != 1 || repo.createInput.Labels[0] != "prod" {
		t.Fatalf("create labels = %#v, want normalized labels", repo.createInput.Labels)
	}
	if repo.createCalls != 1 || repo.idempotentCalls != 0 {
		t.Fatalf("create calls = legacy:%d idempotent:%d, want collection legacy create only", repo.createCalls, repo.idempotentCalls)
	}
}

func TestVPSStateRepairAssetDomainStatusHandler(t *testing.T) {
	repo := &fakeAssetDomainRepository{updateStatusResult: assetdomains.Record{
		DomainID: "dom_001",
		VPSID:    "vps_001",
		Status:   assetdomains.DomainStatusRetired,
	}}
	handler := handlers.AssetDomainStatus(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/domains/dom_001/status", strings.NewReader(`{"status":" retired ","reason":" stopped after review "}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.updateStatusCalls != 1 || repo.updateStatusID != "dom_001" || repo.updateStatus != assetdomains.DomainStatusRetired || repo.updateStatusReason != "stopped after review" {
		t.Fatalf("update status request = calls:%d id:%q status:%q reason:%q", repo.updateStatusCalls, repo.updateStatusID, repo.updateStatus, repo.updateStatusReason)
	}
	record := decodeTestResponse[assetdomains.Record](t, recorder)
	if record.DomainID != "dom_001" || record.Status != assetdomains.DomainStatusRetired {
		t.Fatalf("response domain = %#v, want the updated domain", record)
	}
}

func TestVPSStateRepairAssetDomainStatusValidation(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "missing reason", method: http.MethodPatch, path: "/api/domains/dom_001/status", body: `{"status":"paused"}`, want: http.StatusBadRequest},
		{name: "unknown requested status", method: http.MethodPatch, path: "/api/domains/dom_001/status", body: `{"status":"unknown","reason":"reviewed"}`, want: http.StatusBadRequest},
		{name: "invalid json", method: http.MethodPatch, path: "/api/domains/dom_001/status", body: `{"status":`, want: http.StatusBadRequest},
		{name: "invalid path", method: http.MethodPatch, path: "/api/domains/dom_001/status/extra", body: `{"status":"paused","reason":"reviewed"}`, want: http.StatusNotFound},
		{name: "method", method: http.MethodPost, path: "/api/domains/dom_001/status", body: `{"status":"paused","reason":"reviewed"}`, want: http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAssetDomainRepository{}
			recorder := httptest.NewRecorder()
			handlers.AssetDomainStatus(repo).ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
			if repo.updateStatusCalls != 0 {
				t.Fatalf("UpdateStatus calls = %d, want zero for rejected request", repo.updateStatusCalls)
			}
		})
	}
}

func TestVPSDomainsListsDomains(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	repo := &fakeAssetDomainRepository{listForVPSResult: []assetdomains.Record{{
		DomainID:   "dom_001",
		VPSID:      "vps_001",
		DomainName: "www.example.com",
		Status:     assetdomains.DomainStatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}}}

	handler := handlers.VPSDomains(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/domains", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.listForVPSID != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listForVPSID)
	}
	var body []assetdomains.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if len(body) != 1 || body[0].DomainID != "dom_001" {
		t.Fatalf("response count/ID matches = %d/%t", len(body), len(body) == 1 && body[0].DomainID == "dom_001")
	}
}

func TestVPSDomainsCreatesDomainWithPathVPSID(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	repo := &fakeAssetDomainRepository{createResult: assetdomains.Record{
		DomainID:   "dom_001",
		VPSID:      "vps_path",
		DomainName: "www.example.com",
		Status:     assetdomains.DomainStatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}}

	handler := handlers.VPSDomains(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_path/domains", strings.NewReader(`{
		"vps_id":"vps_body",
		"domain_name":" WWW.Example.COM. "
	}`))
	req.Header.Set("Idempotency-Key", "domain-create-001")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if repo.createInput.VPSID != "vps_path" {
		t.Fatalf("create VPSID = %q, want path vps id", repo.createInput.VPSID)
	}
	if repo.createInput.Status != assetdomains.DomainStatusActive {
		t.Fatalf("create status = %q, want default active", repo.createInput.Status)
	}
	if repo.createCalls != 0 || repo.idempotentCalls != 1 || repo.idempotentKey != "domain-create-001" {
		t.Fatalf("create calls = legacy:%d idempotent:%d, want one idempotent call", repo.createCalls, repo.idempotentCalls)
	}
}

func TestVPSDomainsRequiresSingleValidIdempotencyKeyBeforeCreate(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{name: "missing"},
		{name: "empty", keys: []string{""}},
		{name: "multiple", keys: []string{"domain-key-001", "domain-key-002"}},
		{name: "comma joined multiple", keys: []string{"domain-key-001,domain-key-002"}},
		{name: "too short", keys: []string{"short"}},
		{name: "too long", keys: []string{strings.Repeat("a", 129)}},
		{name: "invalid characters", keys: []string{"private/key"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAssetDomainRepository{}
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/domains", strings.NewReader(`{"domain_name":"example.com"}`))
			for _, key := range tt.keys {
				req.Header.Add("Idempotency-Key", key)
			}
			recorder := httptest.NewRecorder()

			handlers.VPSDomains(repo).ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error body error type = %T", err)
			}
			if body["code"] != "invalid_idempotency_key" {
				t.Fatalf("code = %q, want invalid_idempotency_key", body["code"])
			}
			if strings.Contains(recorder.Body.String(), "private/key") {
				t.Fatal("response disclosed rejected idempotency key")
			}
			if repo.createCalls != 0 || repo.idempotentCalls != 0 {
				t.Fatalf("create calls = legacy:%d idempotent:%d, want zero", repo.createCalls, repo.idempotentCalls)
			}
		})
	}
}

func TestVPSDomainsValidatesBodyBeforeIdempotencyKey(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "invalid json", body: `{"domain_name":`, wantError: "invalid json"},
		{name: "invalid input", body: `{"domain_name":"localhost"}`, wantError: "invalid input"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAssetDomainRepository{}
			recorder := httptest.NewRecorder()
			handlers.VPSDomains(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/domains", strings.NewReader(tt.body)))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error body error type = %T", err)
			}
			if body["error"] != tt.wantError || body["code"] != "" {
				t.Fatalf("error/code matches = %t/%t", body["error"] == tt.wantError, body["code"] == "")
			}
			if repo.createCalls != 0 || repo.idempotentCalls != 0 {
				t.Fatalf("create calls = legacy:%d idempotent:%d, want zero", repo.createCalls, repo.idempotentCalls)
			}
		})
	}
}

func TestVPSDomainsMapsIdempotentCreateOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		replayed   bool
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "first create", wantStatus: http.StatusCreated},
		{name: "replay", replayed: true, wantStatus: http.StatusOK},
		{name: "reused key", err: assetdomains.ErrIdempotencyKeyReused, wantStatus: http.StatusConflict, wantCode: "idempotency_key_reused"},
		{name: "repository rejects key", err: assetdomains.ErrInvalidIdempotencyKey, wantStatus: http.StatusBadRequest, wantCode: "invalid_idempotency_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAssetDomainRepository{
				createResult: assetdomains.Record{DomainID: "dom_stable", VPSID: "vps_001", DomainName: "example.com"},
				createErr:    tt.err,
				replayed:     tt.replayed,
			}
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/domains", strings.NewReader(`{"domain_name":"example.com"}`))
			req.Header.Set("Idempotency-Key", "domain-outcome-001")
			recorder := httptest.NewRecorder()

			handlers.VPSDomains(repo).ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if repo.createCalls != 0 || repo.idempotentCalls != 1 {
				t.Fatalf("create calls = legacy:%d idempotent:%d, want one idempotent call", repo.createCalls, repo.idempotentCalls)
			}
			if tt.wantCode != "" {
				var body map[string]string
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("unmarshal error body error type = %T", err)
				}
				if body["code"] != tt.wantCode {
					t.Fatalf("code = %q, want %q", body["code"], tt.wantCode)
				}
				return
			}
			var body assetdomains.Record
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response error type = %T", err)
			}
			if body.DomainID != "dom_stable" {
				t.Fatalf("domain_id = %q, want stable replay DTO", body.DomainID)
			}
		})
	}
}

func TestVPSDomainsSequentialIdempotentCreateContract(t *testing.T) {
	repo := &statefulAssetDomainRepository{}
	handler := handlers.VPSDomains(repo)
	const path = "/api/vps/vps_sequence/domains"
	const key = "domain-sequence-001"

	first := serveTestIdempotentCreate(handler, path, `{"domain_name":" WWW.Example.COM. "}`, key)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusCreated)
	}
	firstRecord := decodeTestResponse[assetdomains.Record](t, first)
	if firstRecord.DomainID != "dom_sequence_001" || firstRecord.VPSID != "vps_sequence" {
		t.Fatal("first response did not contain the materialized domain identity")
	}
	assertTestIdempotentCreateCounts(t, &repo.creates, 1, 1)

	replay := serveTestIdempotentCreate(handler, path, `{"domain_name":"www.example.com"}`, key)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d", replay.Code, http.StatusOK)
	}
	replayRecord := decodeTestResponse[assetdomains.Record](t, replay)
	if replayRecord.DomainID != firstRecord.DomainID {
		t.Fatal("replay did not return the original domain identity")
	}
	assertTestIdempotentCreateCounts(t, &repo.creates, 2, 1)

	reused := serveTestIdempotentCreate(handler, path, `{"domain_name":"api.example.com"}`, key)
	if reused.Code != http.StatusConflict {
		t.Fatalf("reused-key status = %d, want %d", reused.Code, http.StatusConflict)
	}
	reusedError := decodeTestResponse[map[string]string](t, reused)
	if reusedError["code"] != "idempotency_key_reused" {
		t.Fatalf("reused-key code = %q, want idempotency_key_reused", reusedError["code"])
	}
	assertTestIdempotentCreateCounts(t, &repo.creates, 3, 1)
}

func TestAssetDomainsRejectInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "collection invalid filter", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{}), method: http.MethodGet, path: "/api/domains?status=deleted", want: http.StatusBadRequest},
		{name: "collection invalid json", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{}), method: http.MethodPost, path: "/api/domains", body: `{"domain_name":`, want: http.StatusBadRequest},
		{name: "collection invalid input", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{}), method: http.MethodPost, path: "/api/domains", body: `{"vps_id":"vps_001","domain_name":"localhost"}`, want: http.StatusBadRequest},
		{name: "vps domains invalid json", handler: handlers.VPSDomains(&fakeAssetDomainRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/domains", body: `{"domain_name":`, want: http.StatusBadRequest},
		{name: "vps domains invalid input", handler: handlers.VPSDomains(&fakeAssetDomainRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/domains", body: `{"domain_name":"https://example.com"}`, want: http.StatusBadRequest},
		{name: "vps domains deeper path", handler: handlers.VPSDomains(&fakeAssetDomainRepository{}), method: http.MethodGet, path: "/api/vps/vps_001/domains/extra", want: http.StatusNotFound},
		{name: "collection method", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{}), method: http.MethodDelete, path: "/api/domains", want: http.StatusMethodNotAllowed},
		{name: "vps domains method", handler: handlers.VPSDomains(&fakeAssetDomainRepository{}), method: http.MethodDelete, path: "/api/vps/vps_001/domains", want: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "domain-request-001")
			}
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestAssetDomainsMapRepositoryErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "list invalid", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{listErr: assetdomains.ErrInvalidDomainInput}), method: http.MethodGet, path: "/api/domains", want: http.StatusBadRequest},
		{name: "list failure", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{listErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/domains", want: http.StatusInternalServerError},
		{name: "create invalid", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: assetdomains.ErrInvalidDomainInput}), method: http.MethodPost, path: "/api/domains", body: `{"vps_id":"vps_001","domain_name":"example.com"}`, want: http.StatusBadRequest},
		{name: "create conflict", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: assetdomains.ErrDomainConflict}), method: http.MethodPost, path: "/api/domains", body: `{"vps_id":"vps_001","domain_name":"example.com"}`, want: http.StatusConflict},
		{name: "domain status not found", handler: handlers.AssetDomainStatus(&fakeAssetDomainRepository{updateStatusErr: assetdomains.ErrDomainNotFound}), method: http.MethodPatch, path: "/api/domains/missing/status", body: `{"status":"active","reason":"reviewed"}`, want: http.StatusNotFound},
		{name: "domain status failure", handler: handlers.AssetDomainStatus(&fakeAssetDomainRepository{updateStatusErr: errors.New("status update failed")}), method: http.MethodPatch, path: "/api/domains/dom_001/status", body: `{"status":"active","reason":"reviewed"}`, want: http.StatusInternalServerError},
		{name: "domain status conflict", handler: handlers.AssetDomainStatus(&fakeAssetDomainRepository{updateStatusErr: assetdomains.ErrDomainStatusConflict}), method: http.MethodPatch, path: "/api/domains/dom_001/status", body: `{"status":"active","reason":"reviewed"}`, want: http.StatusConflict},
		{name: "status terminal vps conflict", handler: handlers.AssetDomainStatus(&fakeAssetDomainRepository{updateStatusErr: vpsassets.ErrVPSAssetReadonly}), method: http.MethodPatch, path: "/api/domains/dom_001/status", body: `{"status":"active","reason":"reviewed"}`, want: http.StatusConflict},
		{name: "status archived target conflict", handler: handlers.AssetDomainStatus(&fakeAssetDomainRepository{updateStatusErr: targets.ErrTargetMetadataConflict}), method: http.MethodPatch, path: "/api/domains/dom_001/status", body: `{"status":"active","reason":"reviewed"}`, want: http.StatusConflict},
		{name: "create terminal vps conflict", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: vpsassets.ErrVPSAssetReadonly}), method: http.MethodPost, path: "/api/domains", body: `{"vps_id":"vps_001","domain_name":"example.com"}`, want: http.StatusConflict},
		{name: "create archived target conflict", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: targets.ErrTargetMetadataConflict}), method: http.MethodPost, path: "/api/domains", body: `{"vps_id":"vps_001","target_id":"tg_archived","domain_name":"example.com"}`, want: http.StatusConflict},
		{name: "create failure", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/domains", body: `{"vps_id":"vps_001","domain_name":"example.com"}`, want: http.StatusInternalServerError},
		{name: "vps list invalid", handler: handlers.VPSDomains(&fakeAssetDomainRepository{listForVPSErr: assetdomains.ErrInvalidDomainInput}), method: http.MethodGet, path: "/api/vps/vps_001/domains", want: http.StatusBadRequest},
		{name: "vps list failure", handler: handlers.VPSDomains(&fakeAssetDomainRepository{listForVPSErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/vps/vps_001/domains", want: http.StatusInternalServerError},
		{name: "vps create failure", handler: handlers.VPSDomains(&fakeAssetDomainRepository{createErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/vps/vps_001/domains", body: `{"domain_name":"example.com"}`, want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "domain-error-001")
			}
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestAssetDomainsMapRepositoryNotFoundErrors(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.Handler
		method    string
		path      string
		body      string
		wantError string
	}{
		{
			name:      "collection create missing vps",
			handler:   handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: assetdomains.ErrDomainOwnerNotFound}),
			method:    http.MethodPost,
			path:      "/api/domains",
			body:      `{"vps_id":"vps_missing","domain_name":"example.com"}`,
			wantError: "vps asset not found",
		},
		{
			name:      "collection create missing service",
			handler:   handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: assetdomains.ErrDomainServiceNotFound}),
			method:    http.MethodPost,
			path:      "/api/domains",
			body:      `{"vps_id":"vps_001","service_id":"svc_missing","domain_name":"example.com"}`,
			wantError: "asset service not found",
		},
		{
			name:      "collection create missing target",
			handler:   handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: assetdomains.ErrDomainTargetNotFound}),
			method:    http.MethodPost,
			path:      "/api/domains",
			body:      `{"vps_id":"vps_001","target_id":"tg_missing","domain_name":"example.com"}`,
			wantError: "target not found",
		},
		{
			name:      "vps list missing owner",
			handler:   handlers.VPSDomains(&fakeAssetDomainRepository{listForVPSErr: assetdomains.ErrDomainOwnerNotFound}),
			method:    http.MethodGet,
			path:      "/api/vps/vps_missing/domains",
			wantError: "vps asset not found",
		},
		{
			name:      "vps create missing owner",
			handler:   handlers.VPSDomains(&fakeAssetDomainRepository{createErr: assetdomains.ErrDomainOwnerNotFound}),
			method:    http.MethodPost,
			path:      "/api/vps/vps_missing/domains",
			body:      `{"domain_name":"example.com"}`,
			wantError: "vps asset not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "domain-not-found-001")
			}
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
			}
			var response map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if response["error"] != tt.wantError {
				t.Fatalf("error = %q, want %q", response["error"], tt.wantError)
			}
		})
	}
}
