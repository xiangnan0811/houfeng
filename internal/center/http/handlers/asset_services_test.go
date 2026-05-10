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

	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/http/handlers"
)

type fakeAssetServiceRepository struct {
	listResult       []assetservices.Record
	listErr          error
	listFilters      assetservices.ListFilters
	listForVPSResult []assetservices.Record
	listForVPSErr    error
	listForVPSID     string
	createResult     assetservices.Record
	createErr        error
	createInput      assetservices.CreateInput
}

func (f *fakeAssetServiceRepository) ListAssetServices(_ context.Context, filters assetservices.ListFilters) ([]assetservices.Record, error) {
	f.listFilters = filters
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeAssetServiceRepository) ListAssetServicesForVPS(_ context.Context, vpsID string) ([]assetservices.Record, error) {
	f.listForVPSID = vpsID
	if f.listForVPSErr != nil {
		return nil, f.listForVPSErr
	}
	return f.listForVPSResult, nil
}

func (f *fakeAssetServiceRepository) CreateAssetService(_ context.Context, input assetservices.CreateInput) (assetservices.Record, error) {
	f.createInput = input
	if f.createErr != nil {
		return assetservices.Record{}, f.createErr
	}
	return f.createResult, nil
}

func TestAssetServicesCollectionListsServices(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	repo := &fakeAssetServiceRepository{listResult: []assetservices.Record{{
		ServiceID:   "svc_001",
		VPSID:       "vps_001",
		Name:        "Blog",
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      assetservices.ServiceStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}}}

	handler := handlers.AssetServicesCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/services?vps_id=+vps_001+&target_id=tg_001&service_type=web&status=active", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listFilters.VPSID != "vps_001" ||
		repo.listFilters.TargetID != "tg_001" ||
		repo.listFilters.ServiceType != assetservices.ServiceTypeWeb ||
		repo.listFilters.Status != assetservices.ServiceStatusActive {
		t.Fatalf("filters = %#v, want normalized filters", repo.listFilters)
	}
	var body []assetservices.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].ServiceID != "svc_001" {
		t.Fatalf("body = %#v, want service list", body)
	}
}

func TestAssetServicesCollectionCreatesService(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	targetID := "tg_001"
	port := 443
	repo := &fakeAssetServiceRepository{createResult: assetservices.Record{
		ServiceID:   "svc_001",
		VPSID:       "vps_001",
		TargetID:    &targetID,
		Name:        "Blog",
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      assetservices.ServiceStatusActive,
		URL:         "https://example.com",
		Port:        &port,
		Labels:      []string{"prod"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}}

	handler := handlers.AssetServicesCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/services", strings.NewReader(`{
		"vps_id":" vps_001 ",
		"target_id":" tg_001 ",
		"name":" Blog ",
		"service_type":"web",
		"status":"active",
		"url":" https://example.com ",
		"port":443,
		"labels":[" prod ","","prod"]
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createInput.VPSID != "vps_001" || repo.createInput.TargetID == nil || *repo.createInput.TargetID != "tg_001" {
		t.Fatalf("create identity = %#v, want trimmed vps and target", repo.createInput)
	}
	if repo.createInput.Name != "Blog" || repo.createInput.URL != "https://example.com" {
		t.Fatalf("create input = %#v, want trimmed name/url", repo.createInput)
	}
	if len(repo.createInput.Labels) != 1 || repo.createInput.Labels[0] != "prod" {
		t.Fatalf("create labels = %#v, want normalized labels", repo.createInput.Labels)
	}
}

func TestVPSServicesListsServices(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	repo := &fakeAssetServiceRepository{listForVPSResult: []assetservices.Record{{
		ServiceID:   "svc_001",
		VPSID:       "vps_001",
		Name:        "Blog",
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      assetservices.ServiceStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}}}

	handler := handlers.VPSServices(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/services", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listForVPSID != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listForVPSID)
	}
	var body []assetservices.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].ServiceID != "svc_001" {
		t.Fatalf("body = %#v, want service list", body)
	}
}

func TestVPSServicesCreatesServiceWithPathVPSID(t *testing.T) {
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	repo := &fakeAssetServiceRepository{createResult: assetservices.Record{
		ServiceID:   "svc_001",
		VPSID:       "vps_path",
		Name:        "Worker",
		ServiceType: assetservices.ServiceTypeWorker,
		Status:      assetservices.ServiceStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}}

	handler := handlers.VPSServices(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_path/services", strings.NewReader(`{
		"vps_id":"vps_body",
		"name":" Worker ",
		"service_type":"worker"
	}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createInput.VPSID != "vps_path" {
		t.Fatalf("create VPSID = %q, want path vps id", repo.createInput.VPSID)
	}
	if repo.createInput.Status != assetservices.ServiceStatusActive {
		t.Fatalf("create status = %q, want default active", repo.createInput.Status)
	}
}

func TestAssetServicesRejectInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "collection invalid filter", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{}), method: http.MethodGet, path: "/api/services?service_type=ssh", want: http.StatusBadRequest},
		{name: "collection invalid json", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{}), method: http.MethodPost, path: "/api/services", body: `{"name":`, want: http.StatusBadRequest},
		{name: "collection invalid input", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{}), method: http.MethodPost, path: "/api/services", body: `{"vps_id":"vps_001","name":" "}`, want: http.StatusBadRequest},
		{name: "vps services invalid json", handler: handlers.VPSServices(&fakeAssetServiceRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/services", body: `{"name":`, want: http.StatusBadRequest},
		{name: "vps services invalid input", handler: handlers.VPSServices(&fakeAssetServiceRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/services", body: `{"name":" ","service_type":"web"}`, want: http.StatusBadRequest},
		{name: "vps services deeper path", handler: handlers.VPSServices(&fakeAssetServiceRepository{}), method: http.MethodGet, path: "/api/vps/vps_001/services/extra", want: http.StatusNotFound},
		{name: "collection method", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{}), method: http.MethodDelete, path: "/api/services", want: http.StatusMethodNotAllowed},
		{name: "vps services method", handler: handlers.VPSServices(&fakeAssetServiceRepository{}), method: http.MethodDelete, path: "/api/vps/vps_001/services", want: http.StatusMethodNotAllowed},
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

func TestAssetServicesMapRepositoryErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "list invalid", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{listErr: assetservices.ErrInvalidServiceInput}), method: http.MethodGet, path: "/api/services", want: http.StatusBadRequest},
		{name: "list failure", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{listErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/services", want: http.StatusInternalServerError},
		{name: "create invalid", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{createErr: assetservices.ErrInvalidServiceInput}), method: http.MethodPost, path: "/api/services", body: `{"vps_id":"vps_001","name":"Blog","service_type":"web"}`, want: http.StatusBadRequest},
		{name: "create failure", handler: handlers.AssetServicesCollection(&fakeAssetServiceRepository{createErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/services", body: `{"vps_id":"vps_001","name":"Blog","service_type":"web"}`, want: http.StatusInternalServerError},
		{name: "vps list invalid", handler: handlers.VPSServices(&fakeAssetServiceRepository{listForVPSErr: assetservices.ErrInvalidServiceInput}), method: http.MethodGet, path: "/api/vps/vps_001/services", want: http.StatusBadRequest},
		{name: "vps list failure", handler: handlers.VPSServices(&fakeAssetServiceRepository{listForVPSErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/vps/vps_001/services", want: http.StatusInternalServerError},
		{name: "vps create failure", handler: handlers.VPSServices(&fakeAssetServiceRepository{createErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/vps/vps_001/services", body: `{"name":"Blog","service_type":"web"}`, want: http.StatusInternalServerError},
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

func TestAssetServicesMapRepositoryNotFoundErrors(t *testing.T) {
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
			handler:   handlers.AssetServicesCollection(&fakeAssetServiceRepository{createErr: assetservices.ErrServiceOwnerNotFound}),
			method:    http.MethodPost,
			path:      "/api/services",
			body:      `{"vps_id":"vps_missing","name":"Blog","service_type":"web"}`,
			wantError: "vps asset not found",
		},
		{
			name:      "collection create missing target",
			handler:   handlers.AssetServicesCollection(&fakeAssetServiceRepository{createErr: assetservices.ErrServiceTargetNotFound}),
			method:    http.MethodPost,
			path:      "/api/services",
			body:      `{"vps_id":"vps_001","target_id":"tg_missing","name":"Blog","service_type":"web"}`,
			wantError: "target not found",
		},
		{
			name:      "vps list missing owner",
			handler:   handlers.VPSServices(&fakeAssetServiceRepository{listForVPSErr: assetservices.ErrServiceOwnerNotFound}),
			method:    http.MethodGet,
			path:      "/api/vps/vps_missing/services",
			wantError: "vps asset not found",
		},
		{
			name:      "vps create missing owner",
			handler:   handlers.VPSServices(&fakeAssetServiceRepository{createErr: assetservices.ErrServiceOwnerNotFound}),
			method:    http.MethodPost,
			path:      "/api/vps/vps_missing/services",
			body:      `{"name":"Blog","service_type":"web"}`,
			wantError: "vps asset not found",
		},
		{
			name:      "vps create missing target",
			handler:   handlers.VPSServices(&fakeAssetServiceRepository{createErr: assetservices.ErrServiceTargetNotFound}),
			method:    http.MethodPost,
			path:      "/api/vps/vps_001/services",
			body:      `{"target_id":"tg_missing","name":"Blog","service_type":"web"}`,
			wantError: "target not found",
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
