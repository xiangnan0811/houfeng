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
	listTargetsResult          []targets.TargetRecord
	listTargetsErr             error
	getTargetResult            targets.TargetRecord
	getTargetErr               error
	createTargetResult         targets.TargetRecord
	createTargetErr            error
	createTargetInput          targets.CreateTargetInput
	updateTargetMetadataResult targets.TargetRecord
	updateTargetMetadataErr    error
	updateTargetMetadataInput  targets.UpdateMetadataInput
	updateTargetMetadataID     string
	listProbeItemsResult       []targets.ProbeItemRecord
	listProbeItemsErr          error
	createProbeItemResult      targets.ProbeItemRecord
	createProbeItemErr         error
	createProbeItemInput       targets.CreateProbeItemInput
	updateProbeItemResult      targets.ProbeItemRecord
	updateProbeItemErr         error
	updateProbeItemInput       targets.UpdateProbeItemInput
	updateProbeItemID          string
	deleteProbeItemErr         error
	deleteProbeItemID          string
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

func (f *fakeTargetRepository) UpdateTargetMetadata(_ context.Context, targetID string, input targets.UpdateMetadataInput) (targets.TargetRecord, error) {
	f.updateTargetMetadataID = targetID
	f.updateTargetMetadataInput = input
	if f.updateTargetMetadataErr != nil {
		return targets.TargetRecord{}, f.updateTargetMetadataErr
	}
	record := f.updateTargetMetadataResult
	record.TargetID = targetID
	return record, nil
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

func (f *fakeTargetRepository) UpdateProbeItem(_ context.Context, targetID string, probeItemID string, input targets.UpdateProbeItemInput) (targets.ProbeItemRecord, error) {
	f.updateProbeItemID = probeItemID
	f.updateProbeItemInput = input
	if f.updateProbeItemErr != nil {
		return targets.ProbeItemRecord{}, f.updateProbeItemErr
	}
	record := f.updateProbeItemResult
	record.TargetID = targetID
	record.ProbeItemID = probeItemID
	return record, nil
}

func (f *fakeTargetRepository) DeleteProbeItem(_ context.Context, _ string, probeItemID string) error {
	f.deleteProbeItemID = probeItemID
	return f.deleteProbeItemErr
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
			FrequencyTier:  targets.FrequencyTier5s,
			TimeoutSeconds: 5,
			Config:         json.RawMessage(`{"scheme":"https","path":"/healthz","method":"GET","expected_status_range":[200,299]}`),
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", strings.NewReader(`{"probe_kind":"http","enabled":true,"frequency_tier":"5s","timeout_seconds":5,"config":{"scheme":"https","path":"/healthz","method":"GET","expected_status_range":[200,299]}}`))
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
	if repo.createProbeItemInput.FrequencyTier != targets.FrequencyTier5s {
		t.Fatalf("create input frequency tier = %q, want %q", repo.createProbeItemInput.FrequencyTier, targets.FrequencyTier5s)
	}
}

func TestUpdateProbeItemHandlerReturnsUpdatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 27, 10, 0, 0, 0, time.UTC)
	repo := &fakeTargetRepository{updateProbeItemResult: targets.ProbeItemRecord{
		ProbeKind:      targets.ProbeKindHTTP,
		Enabled:        false,
		FrequencyTier:  targets.FrequencyTier5m,
		TimeoutSeconds: 8,
		Config:         json.RawMessage(`{"scheme":"https","path":"/ready","method":"HEAD","expected_status_range":[200,204]}`),
		CreatedAt:      now,
		UpdatedAt:      now,
	}}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/tg_001/probe-items/pb_001", strings.NewReader(`{"probe_kind":"http","enabled":false,"frequency_tier":"5m","timeout_seconds":8,"config":{"scheme":"https","path":"/ready","method":"HEAD","expected_status_range":[200,204]}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.updateProbeItemID != "pb_001" {
		t.Fatalf("probe item id = %q, want pb_001", repo.updateProbeItemID)
	}
	if repo.updateProbeItemInput.Enabled {
		t.Fatal("expected update input enabled=false")
	}

	var body targets.ProbeItemRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.ProbeItemID != "pb_001" || body.TargetID != "tg_001" || body.Enabled {
		t.Fatalf("body = %#v, want updated scoped probe item", body)
	}
}

func TestDeleteProbeItemHandlerReturnsNoContent(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodDelete, "/api/targets/tg_001/probe-items/pb_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
	if repo.deleteProbeItemID != "pb_001" {
		t.Fatalf("probe item id = %q, want pb_001", repo.deleteProbeItemID)
	}
}

func TestTargetItemPatchMetadataReturnsUpdatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 27, 9, 0, 0, 0, time.UTC)
	expectedUpdatedAt := time.Date(2026, time.April, 27, 8, 55, 0, 123000000, time.UTC)
	repo := &fakeTargetRepository{
		updateTargetMetadataResult: targets.TargetRecord{
			Name:                "Blog",
			TargetType:          targets.TargetTypeService,
			Host:                "blog.example.com",
			ExecutionNodeLabels: []string{"edge"},
			RunStatus:           targets.RunStatusEnabled,
			Labels:              []string{"edge", "core"},
			Note:                "updated",
			CurrentHealthStatus: targets.HealthNormal,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
	}

	handler := handlers.TargetItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/targets/tg_001", strings.NewReader(`{"labels":[" edge ","core","edge"],"note":" updated "}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+expectedUpdatedAt.Format(time.RFC3339Nano)+`"`)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.updateTargetMetadataID != "tg_001" {
		t.Fatalf("update target id = %q, want %q", repo.updateTargetMetadataID, "tg_001")
	}
	if len(repo.updateTargetMetadataInput.Labels) != 2 || repo.updateTargetMetadataInput.Labels[0] != "edge" || repo.updateTargetMetadataInput.Labels[1] != "core" {
		t.Fatalf("update labels = %#v, want %#v", repo.updateTargetMetadataInput.Labels, []string{"edge", "core"})
	}
	if repo.updateTargetMetadataInput.Note != "updated" {
		t.Fatalf("update note = %q, want %q", repo.updateTargetMetadataInput.Note, "updated")
	}
	if repo.updateTargetMetadataInput.ExpectedUpdatedAt == nil || !repo.updateTargetMetadataInput.ExpectedUpdatedAt.Equal(expectedUpdatedAt) {
		t.Fatalf("expected updated_at = %v, want %s", repo.updateTargetMetadataInput.ExpectedUpdatedAt, expectedUpdatedAt.Format(time.RFC3339Nano))
	}

	var body targets.TargetRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.TargetID != "tg_001" {
		t.Fatalf("response target_id = %q, want %q", body.TargetID, "tg_001")
	}
	if body.Note != "updated" {
		t.Fatalf("response note = %q, want %q", body.Note, "updated")
	}
	if len(body.Labels) != 2 || body.Labels[0] != "edge" || body.Labels[1] != "core" {
		t.Fatalf("response labels = %#v, want %#v", body.Labels, []string{"edge", "core"})
	}
}

func TestTargetItemRejectsInvalidMetadata(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/targets/tg_001", strings.NewReader(`{"labels":["01","02","03","04","05","06","07","08","09","10","11","12","13","14","15","16","17","18","19","20","21"],"note":""}`))
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
}

func TestTargetItemRejectsPartialMetadataPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty object", body: `{}`},
		{name: "labels only", body: `{"labels":["edge"]}`},
		{name: "note only", body: `{"note":"updated"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeTargetRepository{}

			handler := handlers.TargetItem(repo)
			req := httptest.NewRequest(http.MethodPatch, "/api/targets/tg_001", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}
			if repo.updateTargetMetadataID != "" {
				t.Fatalf("UpdateTargetMetadata called for partial payload")
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

func TestTargetItemMetadataValidationCountsUnicodeCharacters(t *testing.T) {
	now := time.Date(2026, time.April, 27, 9, 0, 0, 0, time.UTC)
	label := strings.Repeat("候", 64)
	note := strings.Repeat("风", 2000)
	repo := &fakeTargetRepository{
		updateTargetMetadataResult: targets.TargetRecord{
			TargetID:            "tg_001",
			Name:                "Blog",
			TargetType:          targets.TargetTypeService,
			Host:                "blog.example.com",
			ExecutionNodeLabels: []string{"edge"},
			RunStatus:           targets.RunStatusEnabled,
			Labels:              []string{label},
			Note:                note,
			CurrentHealthStatus: targets.HealthNormal,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
	}

	handler := handlers.TargetItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/targets/tg_001", strings.NewReader(`{"labels":["`+label+`"],"note":"`+note+`"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if got := repo.updateTargetMetadataInput.Labels; len(got) != 1 || got[0] != label {
		t.Fatalf("update labels = %#v, want %#v", got, []string{label})
	}
	if repo.updateTargetMetadataInput.Note != note {
		t.Fatalf("update note length = %d, want %d", len([]rune(repo.updateTargetMetadataInput.Note)), 2000)
	}
}

func TestTargetItemMapsMetadataNotFound(t *testing.T) {
	repo := &fakeTargetRepository{updateTargetMetadataErr: targets.ErrTargetNotFound}

	handler := handlers.TargetItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/targets/tg_missing", strings.NewReader(`{"labels":["edge"],"note":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestTargetItemMapsMetadataConflict(t *testing.T) {
	repo := &fakeTargetRepository{updateTargetMetadataErr: targets.ErrTargetMetadataConflict}

	handler := handlers.TargetItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/targets/tg_001", strings.NewReader(`{"labels":["edge"],"note":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "metadata conflict" {
		t.Fatalf("expected error %q, got %q", "metadata conflict", body["error"])
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

func TestUpdateProbeItemHandlerRejectsInvalidConfig(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/tg_001/probe-items/pb_001", strings.NewReader(`{"probe_kind":"tls","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestProbeItemItemHandlerMapsNotFound(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   string
		err    error
	}{
		{name: "missing target on update", method: http.MethodPut, body: `{"probe_kind":"tcp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`, err: targets.ErrTargetNotFound},
		{name: "missing probe on update", method: http.MethodPut, body: `{"probe_kind":"tcp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`, err: targets.ErrProbeItemNotFound},
		{name: "missing target on delete", method: http.MethodDelete, err: targets.ErrTargetNotFound},
		{name: "missing probe on delete", method: http.MethodDelete, err: targets.ErrProbeItemNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeTargetRepository{updateProbeItemErr: tt.err, deleteProbeItemErr: tt.err}

			handler := handlers.TargetProbeItems(repo)
			req := httptest.NewRequest(tt.method, "/api/targets/tg_missing/probe-items/pb_missing", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
			}
		})
	}
}

func TestTargetProbeItemsHandlerRejectsUnsupportedItemMethods(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/probe-items/pb_001", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, recorder.Code)
	}
}

func TestTargetProbeItemsHandlerRejectsMalformedItemPaths(t *testing.T) {
	repo := &fakeTargetRepository{}

	handler := handlers.TargetProbeItems(repo)
	req := httptest.NewRequest(http.MethodPut, "/api/targets/tg_001/probe-items/pb_001/extra", strings.NewReader(`{"probe_kind":"tcp","enabled":true,"frequency_tier":"1m","timeout_seconds":5,"config":{"port":443}}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	if repo.updateProbeItemID != "" || repo.deleteProbeItemID != "" {
		t.Fatalf("unexpected repository mutation for malformed path: update=%q delete=%q", repo.updateProbeItemID, repo.deleteProbeItemID)
	}
}
