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
	"houfeng/internal/center/targets"
)

type fakeTargetRepository struct {
	listTargetsResult     []targets.TargetRecord
	listTargetsErr        error
	getTargetResult       targets.TargetRecord
	getTargetErr          error
	createTargetResult    targets.TargetRecord
	createTargetErr       error
	createTargetInput     targets.CreateTargetInput
	listProbeItemsResult  []targets.ProbeItemRecord
	listProbeItemsErr     error
	createProbeItemResult targets.ProbeItemRecord
	createProbeItemErr    error
	createProbeItemInput  targets.CreateProbeItemInput
}

func (f *fakeTargetRepository) ListTargets(context.Context) ([]targets.TargetRecord, error) {
	return f.listTargetsResult, f.listTargetsErr
}

func (f *fakeTargetRepository) GetTarget(context.Context, string) (targets.TargetRecord, error) {
	if f.getTargetErr != nil {
		return targets.TargetRecord{}, f.getTargetErr
	}
	return f.getTargetResult, nil
}

func (f *fakeTargetRepository) CreateTarget(_ context.Context, input targets.CreateTargetInput) (targets.TargetRecord, error) {
	f.createTargetInput = input
	if f.createTargetErr != nil {
		return targets.TargetRecord{}, f.createTargetErr
	}
	return f.createTargetResult, nil
}

func (f *fakeTargetRepository) ListProbeItems(context.Context, string) ([]targets.ProbeItemRecord, error) {
	return f.listProbeItemsResult, f.listProbeItemsErr
}

func (f *fakeTargetRepository) CreateProbeItem(_ context.Context, targetID string, input targets.CreateProbeItemInput) (targets.ProbeItemRecord, error) {
	f.createProbeItemInput = input
	if f.createProbeItemErr != nil {
		return targets.ProbeItemRecord{}, f.createProbeItemErr
	}
	record := f.createProbeItemResult
	record.TargetID = targetID
	return record, nil
}

func TestListTargetsHandlerReturnsJSON(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeTargetRepository{
		listTargetsResult: []targets.TargetRecord{{
			TargetID:            "tg_001",
			Name:                "Blog",
			TargetType:          "service",
			Host:                "blog.example.com",
			ExecutionNodeLabels: []string{"edge"},
			RunStatus:           targets.RunStatusEnabled,
			CurrentHealthStatus: targets.HealthNormal,
			CreatedAt:           now,
			UpdatedAt:           now,
		}},
	}

	handler := handlers.TargetsCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/targets", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body []targets.TargetRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if len(body) != 1 {
		t.Fatalf("expected 1 target, got %d", len(body))
	}

	if body[0].TargetID != "tg_001" {
		t.Fatalf("expected target_id %q, got %q", "tg_001", body[0].TargetID)
	}
	if body[0].Name != "Blog" {
		t.Fatalf("expected name %q, got %q", "Blog", body[0].Name)
	}
}

func TestCreateProbeItemHandlerReturnsCreatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeTargetRepository{
		createProbeItemResult: targets.ProbeItemRecord{
			ProbeItemID:    "pb_001",
			ProbeKind:      "http",
			Enabled:        true,
			FrequencyTier:  "1m",
			TimeoutSeconds: 5,
			Config:         json.RawMessage(`{"scheme":"https","path":"/healthz","method":"GET","expected_status_range":[200,299]}`),
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", strings.NewReader(`{"probe_kind":"http","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"scheme":"https","path":"/healthz","method":"GET","expected_status_range":[200,299]}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}

	var body targets.ProbeItemRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if body.ProbeItemID != "pb_001" {
		t.Fatalf("expected probe_item_id %q, got %q", "pb_001", body.ProbeItemID)
	}
}

func TestCreateTargetHandlerRejectsEmptyExecutionNodeLabels(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetsCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/targets", strings.NewReader(`{"name":"Blog","target_type":"service","host":"blog.example.com","execution_node_labels":[],"run_status":"启用","labels":[],"note":""}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestCreateProbeItemHandlerRejectsInvalidProbeKind(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", strings.NewReader(`{"probe_kind":"icmp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestCreateProbeItemHandlerRejectsInvalidFrequencyTierOrMissingKindConfig(t *testing.T) {
	t.Run("invalid frequency tier", func(t *testing.T) {
		repo := &fakeTargetRepository{}
		handler := handlers.TargetProbeItems(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", strings.NewReader(`{"probe_kind":"tcp","enabled":true,"frequency_tier":"30s","timeout_seconds":5,"config":{"port":443}}`))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
		}
	})

	t.Run("missing kind specific config", func(t *testing.T) {
		repo := &fakeTargetRepository{}
		handler := handlers.TargetProbeItems(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", strings.NewReader(`{"probe_kind":"http","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"scheme":"https","path":"/healthz"}}`))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
		}
	})
}

func TestCreateProbeItemHandlerRejectsStrictConfigViolations(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "unknown field",
			body: `{"probe_kind":"tcp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443,"unexpected":true}}`,
		},
		{
			name: "port wrong type",
			body: `{"probe_kind":"tcp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":"443"}}`,
		},
		{
			name: "http missing expected status range",
			body: `{"probe_kind":"http","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"scheme":"https","path":"/healthz","method":"GET"}}`,
		},
		{
			name: "tls missing expiry warning days",
			body: `{"probe_kind":"tls","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeTargetRepository{}
			handler := handlers.TargetProbeItems(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}

			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if body["error"] != "invalid input" {
				t.Fatalf("expected error %q, got %q", "invalid input", body["error"])
			}
		})
	}
}

func TestTargetProbeItemsHandlerRejectsDeeperPaths(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/probe-items/pb_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}
