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
	createCalls      int
	idempotentCalls  int
	idempotentKey    string
	replayed         bool
}

type statefulAssetServiceRepository struct {
	creates testIdempotentCreateState[assetservices.Record]
}

func (r *statefulAssetServiceRepository) ListAssetServicesForVPS(context.Context, string) ([]assetservices.Record, error) {
	return nil, nil
}

func (r *statefulAssetServiceRepository) CreateAssetServiceIdempotent(_ context.Context, input assetservices.CreateInput, key string) (assetservices.Record, bool, error) {
	identity := struct {
		VPSID string
		Input assetservices.CreateInput
	}{VPSID: input.VPSID, Input: input}
	return r.creates.create(key, identity, func() assetservices.Record {
		return assetservices.Record{
			ServiceID:   "svc_sequence_001",
			VPSID:       input.VPSID,
			Name:        input.Name,
			ServiceType: input.ServiceType,
			Status:      input.Status,
		}
	})
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
	f.createCalls++
	f.createInput = input
	if f.createErr != nil {
		return assetservices.Record{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeAssetServiceRepository) CreateAssetServiceIdempotent(_ context.Context, input assetservices.CreateInput, key string) (assetservices.Record, bool, error) {
	f.idempotentCalls++
	f.createInput = input
	f.idempotentKey = key
	if f.createErr != nil {
		return assetservices.Record{}, false, f.createErr
	}
	return f.createResult, f.replayed, nil
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
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.listFilters.VPSID != "vps_001" ||
		repo.listFilters.TargetID != "tg_001" ||
		repo.listFilters.ServiceType != assetservices.ServiceTypeWeb ||
		repo.listFilters.Status != assetservices.ServiceStatusActive {
		t.Fatalf("filters = %#v, want normalized filters", repo.listFilters)
	}
	var body []assetservices.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if len(body) != 1 || body[0].ServiceID != "svc_001" {
		t.Fatalf("response count/ID matches = %d/%t", len(body), len(body) == 1 && body[0].ServiceID == "svc_001")
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
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
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
	if repo.createCalls != 1 || repo.idempotentCalls != 0 {
		t.Fatalf("create calls = legacy:%d idempotent:%d, want collection legacy create only", repo.createCalls, repo.idempotentCalls)
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
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.listForVPSID != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listForVPSID)
	}
	var body []assetservices.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if len(body) != 1 || body[0].ServiceID != "svc_001" {
		t.Fatalf("response count/ID matches = %d/%t", len(body), len(body) == 1 && body[0].ServiceID == "svc_001")
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
	req.Header.Set("Idempotency-Key", "service-create-001")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if repo.createInput.VPSID != "vps_path" {
		t.Fatalf("create VPSID = %q, want path vps id", repo.createInput.VPSID)
	}
	if repo.createInput.Status != assetservices.ServiceStatusActive {
		t.Fatalf("create status = %q, want default active", repo.createInput.Status)
	}
	if repo.createCalls != 0 || repo.idempotentCalls != 1 || repo.idempotentKey != "service-create-001" {
		t.Fatalf("create calls = legacy:%d idempotent:%d, want one idempotent call", repo.createCalls, repo.idempotentCalls)
	}
}

func TestVPSServicesRequiresSingleValidIdempotencyKeyBeforeCreate(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{name: "missing"},
		{name: "empty", keys: []string{""}},
		{name: "multiple", keys: []string{"service-key-001", "service-key-002"}},
		{name: "comma joined multiple", keys: []string{"service-key-001,service-key-002"}},
		{name: "too short", keys: []string{"short"}},
		{name: "too long", keys: []string{strings.Repeat("a", 129)}},
		{name: "invalid characters", keys: []string{"private/key"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAssetServiceRepository{}
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/services", strings.NewReader(`{"name":"Blog","service_type":"web"}`))
			for _, key := range tt.keys {
				req.Header.Add("Idempotency-Key", key)
			}
			recorder := httptest.NewRecorder()

			handlers.VPSServices(repo).ServeHTTP(recorder, req)

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

func TestVPSServicesValidatesBodyBeforeIdempotencyKey(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "invalid json", body: `{"name":`, wantError: "invalid json"},
		{name: "invalid input", body: `{"name":" "}`, wantError: "invalid input"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAssetServiceRepository{}
			recorder := httptest.NewRecorder()
			handlers.VPSServices(repo).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/services", strings.NewReader(tt.body)))

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

func TestVPSServicesMapsIdempotentCreateOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		replayed   bool
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "first create", wantStatus: http.StatusCreated},
		{name: "replay", replayed: true, wantStatus: http.StatusOK},
		{name: "reused key", err: assetservices.ErrIdempotencyKeyReused, wantStatus: http.StatusConflict, wantCode: "idempotency_key_reused"},
		{name: "repository rejects key", err: assetservices.ErrInvalidIdempotencyKey, wantStatus: http.StatusBadRequest, wantCode: "invalid_idempotency_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAssetServiceRepository{
				createResult: assetservices.Record{ServiceID: "svc_stable", VPSID: "vps_001", Name: "Blog"},
				createErr:    tt.err,
				replayed:     tt.replayed,
			}
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/services", strings.NewReader(`{"name":"Blog","service_type":"web"}`))
			req.Header.Set("Idempotency-Key", "service-outcome-001")
			recorder := httptest.NewRecorder()

			handlers.VPSServices(repo).ServeHTTP(recorder, req)

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
			var body assetservices.Record
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response error type = %T", err)
			}
			if body.ServiceID != "svc_stable" {
				t.Fatalf("service_id = %q, want stable replay DTO", body.ServiceID)
			}
		})
	}
}

func TestVPSServicesSequentialIdempotentCreateContract(t *testing.T) {
	repo := &statefulAssetServiceRepository{}
	handler := handlers.VPSServices(repo)
	const path = "/api/vps/vps_sequence/services"
	const key = "service-sequence-001"

	first := serveTestIdempotentCreate(handler, path, `{"name":" Blog ","service_type":" web "}`, key)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusCreated)
	}
	firstRecord := decodeTestResponse[assetservices.Record](t, first)
	if firstRecord.ServiceID != "svc_sequence_001" || firstRecord.VPSID != "vps_sequence" {
		t.Fatal("first response did not contain the materialized service identity")
	}
	assertTestIdempotentCreateCounts(t, &repo.creates, 1, 1)

	replay := serveTestIdempotentCreate(handler, path, `{"name":"Blog","service_type":"web"}`, key)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d", replay.Code, http.StatusOK)
	}
	replayRecord := decodeTestResponse[assetservices.Record](t, replay)
	if replayRecord.ServiceID != firstRecord.ServiceID {
		t.Fatal("replay did not return the original service identity")
	}
	assertTestIdempotentCreateCounts(t, &repo.creates, 2, 1)

	reused := serveTestIdempotentCreate(handler, path, `{"name":"API","service_type":"web"}`, key)
	if reused.Code != http.StatusConflict {
		t.Fatalf("reused-key status = %d, want %d", reused.Code, http.StatusConflict)
	}
	reusedError := decodeTestResponse[map[string]string](t, reused)
	if reusedError["code"] != "idempotency_key_reused" {
		t.Fatalf("reused-key code = %q, want idempotency_key_reused", reusedError["code"])
	}
	assertTestIdempotentCreateCounts(t, &repo.creates, 3, 1)
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
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "service-request-001")
			}
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
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "service-error-001")
			}
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
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "service-not-found-001")
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
