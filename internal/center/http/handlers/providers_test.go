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
	"houfeng/internal/center/providers"
)

type fakeProviderRepository struct {
	listProvidersResult  []providers.Record
	listProvidersErr     error
	getProviderResult    providers.Record
	getProviderErr       error
	getProviderID        string
	createProviderResult providers.Record
	createProviderErr    error
	createProviderInput  providers.CreateInput
	patchProviderResult  providers.Record
	patchProviderErr     error
	patchProviderID      string
	patchProviderInput   providers.PatchInput
}

func (f *fakeProviderRepository) ListProviders(context.Context) ([]providers.Record, error) {
	return f.listProvidersResult, f.listProvidersErr
}

func (f *fakeProviderRepository) GetProvider(_ context.Context, providerID string) (providers.Record, error) {
	f.getProviderID = providerID
	if f.getProviderErr != nil {
		return providers.Record{}, f.getProviderErr
	}
	return f.getProviderResult, nil
}

func (f *fakeProviderRepository) CreateProvider(_ context.Context, input providers.CreateInput) (providers.Record, error) {
	f.createProviderInput = input
	if f.createProviderErr != nil {
		return providers.Record{}, f.createProviderErr
	}
	return f.createProviderResult, nil
}

func (f *fakeProviderRepository) PatchProvider(_ context.Context, providerID string, input providers.PatchInput) (providers.Record, error) {
	f.patchProviderID = providerID
	f.patchProviderInput = input
	if f.patchProviderErr != nil {
		return providers.Record{}, f.patchProviderErr
	}
	return f.patchProviderResult, nil
}

func TestProvidersCollectionListsProviders(t *testing.T) {
	now := time.Date(2026, time.May, 8, 13, 0, 0, 0, time.UTC)
	repo := &fakeProviderRepository{listProvidersResult: []providers.Record{{
		ProviderID: "pv_001",
		Name:       "Hetzner",
		CreatedAt:  now,
		UpdatedAt:  now,
	}}}

	handler := handlers.ProvidersCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body []providers.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if len(body) != 1 || body[0].ProviderID != "pv_001" {
		t.Fatalf("body = %#v, want provider list", body)
	}
}

func TestProvidersCollectionCreatesProvider(t *testing.T) {
	now := time.Date(2026, time.May, 8, 13, 0, 0, 0, time.UTC)
	repo := &fakeProviderRepository{createProviderResult: providers.Record{
		ProviderID: "pv_001",
		Name:       "Hetzner",
		Website:    "https://hetzner.com",
		Rating:     intPtr(5),
		Labels:     []string{"core"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}}

	handler := handlers.ProvidersCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(`{"name":" Hetzner ","website":" https://hetzner.com ","rating":5,"labels":[" core ","","core"]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if repo.createProviderInput.Name != "Hetzner" {
		t.Fatalf("create name = %q, want trimmed name", repo.createProviderInput.Name)
	}
	if repo.createProviderInput.Website != "https://hetzner.com" {
		t.Fatalf("create website = %q, want trimmed website", repo.createProviderInput.Website)
	}
	if len(repo.createProviderInput.Labels) != 1 || repo.createProviderInput.Labels[0] != "core" {
		t.Fatalf("create labels = %#v, want normalized labels", repo.createProviderInput.Labels)
	}
	var body providers.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.ProviderID != "pv_001" {
		t.Fatalf("provider_id = %q, want pv_001", body.ProviderID)
	}
}

func TestProvidersCollectionRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "blank name", body: `{"name":" "}`},
		{name: "invalid rating", body: `{"name":"Hetzner","rating":6}`},
		{name: "unknown field", body: `{"name":"Hetzner","unexpected":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.ProvidersCollection(&fakeProviderRepository{})
			req := httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestProvidersItemGetsProvider(t *testing.T) {
	now := time.Date(2026, time.May, 8, 13, 0, 0, 0, time.UTC)
	repo := &fakeProviderRepository{getProviderResult: providers.Record{
		ProviderID: "pv_001",
		Name:       "Hetzner",
		CreatedAt:  now,
		UpdatedAt:  now,
	}}

	handler := handlers.ProviderItem(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/providers/pv_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.getProviderID != "pv_001" {
		t.Fatalf("get provider id = %q, want pv_001", repo.getProviderID)
	}
	var body providers.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.ProviderID != "pv_001" {
		t.Fatalf("provider_id = %q, want pv_001", body.ProviderID)
	}
}

func TestProvidersItemPatchesProvider(t *testing.T) {
	now := time.Date(2026, time.May, 8, 13, 0, 0, 0, time.UTC)
	repo := &fakeProviderRepository{patchProviderResult: providers.Record{
		ProviderID: "pv_001",
		Name:       "Hetzner Cloud",
		Rating:     nil,
		Labels:     []string{"core", "backup"},
		CreatedAt:  now.Add(-time.Hour),
		UpdatedAt:  now,
	}}

	handler := handlers.ProviderItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/providers/pv_001", strings.NewReader(`{"name":" Hetzner Cloud ","rating":null,"labels":[" core ","backup","core"]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.patchProviderID != "pv_001" {
		t.Fatalf("patch provider id = %q, want pv_001", repo.patchProviderID)
	}
	if !repo.patchProviderInput.Name.Set || repo.patchProviderInput.Name.Value != "Hetzner Cloud" {
		t.Fatalf("patch name = %#v, want trimmed set name", repo.patchProviderInput.Name)
	}
	if !repo.patchProviderInput.Rating.Set || repo.patchProviderInput.Rating.Value != nil {
		t.Fatalf("patch rating = %#v, want explicit null rating", repo.patchProviderInput.Rating)
	}
	if !repo.patchProviderInput.Labels.Set || len(repo.patchProviderInput.Labels.Values) != 2 {
		t.Fatalf("patch labels = %#v, want normalized labels", repo.patchProviderInput.Labels)
	}
	var body providers.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Name != "Hetzner Cloud" {
		t.Fatalf("body.Name = %q, want Hetzner Cloud", body.Name)
	}
}

func TestProvidersItemReturnsNotFound(t *testing.T) {
	tests := []struct {
		name   string
		method string
		repo   *fakeProviderRepository
	}{
		{name: "get", method: http.MethodGet, repo: &fakeProviderRepository{getProviderErr: providers.ErrProviderNotFound}},
		{name: "patch", method: http.MethodPatch, repo: &fakeProviderRepository{patchProviderErr: providers.ErrProviderNotFound}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.ProviderItem(tt.repo)
			body := strings.NewReader(`{"name":"New Name"}`)
			req := httptest.NewRequest(tt.method, "/api/providers/pv_missing", body)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
			var response map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if response["error"] != "provider not found" {
				t.Fatalf("error = %q, want provider not found", response["error"])
			}
		})
	}
}

func TestProvidersItemRejectsInvalidPatchAndDeeperPaths(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "blank name", method: http.MethodPatch, path: "/api/providers/pv_001", body: `{"name":" "}`, want: http.StatusBadRequest},
		{name: "invalid rating", method: http.MethodPatch, path: "/api/providers/pv_001", body: `{"rating":0}`, want: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPatch, path: "/api/providers/pv_001", body: `{"website":"https://example.com","extra":true}`, want: http.StatusBadRequest},
		{name: "deeper path", method: http.MethodGet, path: "/api/providers/pv_001/links", body: ``, want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.ProviderItem(&fakeProviderRepository{})
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestProvidersUnsupportedMethodsReturnMethodNotAllowed(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
	}{
		{name: "collection", handler: handlers.ProvidersCollection(&fakeProviderRepository{}), method: http.MethodDelete, path: "/api/providers"},
		{name: "item", handler: handlers.ProviderItem(&fakeProviderRepository{}), method: http.MethodPost, path: "/api/providers/pv_001"},
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

func TestProvidersMapRepositoryFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{name: "list", handler: handlers.ProvidersCollection(&fakeProviderRepository{listProvidersErr: errors.New("list failed")}), method: http.MethodGet, path: "/api/providers"},
		{name: "create", handler: handlers.ProvidersCollection(&fakeProviderRepository{createProviderErr: errors.New("create failed")}), method: http.MethodPost, path: "/api/providers", body: `{"name":"Hetzner"}`},
		{name: "get", handler: handlers.ProviderItem(&fakeProviderRepository{getProviderErr: errors.New("get failed")}), method: http.MethodGet, path: "/api/providers/pv_001"},
		{name: "patch", handler: handlers.ProviderItem(&fakeProviderRepository{patchProviderErr: errors.New("patch failed")}), method: http.MethodPatch, path: "/api/providers/pv_001", body: `{"name":"Hetzner"}`},
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

func intPtr(value int) *int {
	return &value
}
