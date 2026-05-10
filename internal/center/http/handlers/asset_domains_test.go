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
)

type fakeAssetDomainRepository struct {
	listResult       []assetdomains.Record
	listErr          error
	listFilters      assetdomains.ListFilters
	listForVPSResult []assetdomains.Record
	listForVPSErr    error
	listForVPSID     string
	createResult     assetdomains.Record
	createErr        error
	createInput      assetdomains.CreateInput
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
	f.createInput = input
	if f.createErr != nil {
		return assetdomains.Record{}, f.createErr
	}
	return f.createResult, nil
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
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listFilters.VPSID != "vps_001" ||
		repo.listFilters.ServiceID != "svc_001" ||
		repo.listFilters.TargetID != "tg_001" ||
		repo.listFilters.Status != assetdomains.DomainStatusActive {
		t.Fatalf("filters = %#v, want normalized filters", repo.listFilters)
	}
	var body []assetdomains.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].DomainID != "dom_001" {
		t.Fatalf("body = %#v, want domain list", body)
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
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
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
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listForVPSID != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listForVPSID)
	}
	var body []assetdomains.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].DomainID != "dom_001" {
		t.Fatalf("body = %#v, want domain list", body)
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
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createInput.VPSID != "vps_path" {
		t.Fatalf("create VPSID = %q, want path vps id", repo.createInput.VPSID)
	}
	if repo.createInput.Status != assetdomains.DomainStatusActive {
		t.Fatalf("create status = %q, want default active", repo.createInput.Status)
	}
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
		{name: "create failure", handler: handlers.AssetDomainsCollection(&fakeAssetDomainRepository{createErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/domains", body: `{"vps_id":"vps_001","domain_name":"example.com"}`, want: http.StatusInternalServerError},
		{name: "vps list invalid", handler: handlers.VPSDomains(&fakeAssetDomainRepository{listForVPSErr: assetdomains.ErrInvalidDomainInput}), method: http.MethodGet, path: "/api/vps/vps_001/domains", want: http.StatusBadRequest},
		{name: "vps list failure", handler: handlers.VPSDomains(&fakeAssetDomainRepository{listForVPSErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/vps/vps_001/domains", want: http.StatusInternalServerError},
		{name: "vps create failure", handler: handlers.VPSDomains(&fakeAssetDomainRepository{createErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/vps/vps_001/domains", body: `{"domain_name":"example.com"}`, want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
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
