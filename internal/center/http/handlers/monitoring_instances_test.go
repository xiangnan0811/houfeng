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
	"houfeng/internal/center/monitoringinstances"
)

type fakeMonitoringInstanceRepository struct {
	listMonitoringInstancesResult          []monitoringinstances.Record
	listMonitoringInstancesErr             error
	getMonitoringInstanceResult            monitoringinstances.Record
	getMonitoringInstanceErr               error
	createMonitoringInstanceResult         monitoringinstances.Record
	createMonitoringInstanceErr            error
	createMonitoringInstanceInput          monitoringinstances.CreateInput
	setPendingActionErr                    error
	setPendingActionMonitoringInstanceID   string
	setPendingActionID                     string
	setPendingActionCommand                string
	updateMonitoringInstanceMetadataResult monitoringinstances.Record
	updateMonitoringInstanceMetadataErr    error
	updateMonitoringInstanceMetadataID     string
	updateMonitoringInstanceMetadataInput  monitoringinstances.UpdateMetadataInput
}

func (f *fakeMonitoringInstanceRepository) ListMonitoringInstances(context.Context) ([]monitoringinstances.Record, error) {
	return f.listMonitoringInstancesResult, f.listMonitoringInstancesErr
}

func (f *fakeMonitoringInstanceRepository) GetMonitoringInstance(context.Context, string) (monitoringinstances.Record, error) {
	if f.getMonitoringInstanceErr != nil {
		return monitoringinstances.Record{}, f.getMonitoringInstanceErr
	}
	return f.getMonitoringInstanceResult, nil
}

func (f *fakeMonitoringInstanceRepository) CreateMonitoringInstance(_ context.Context, input monitoringinstances.CreateInput) (monitoringinstances.Record, error) {
	f.createMonitoringInstanceInput = input
	if f.createMonitoringInstanceErr != nil {
		return monitoringinstances.Record{}, f.createMonitoringInstanceErr
	}
	return f.createMonitoringInstanceResult, nil
}

func (f *fakeMonitoringInstanceRepository) UpdateMonitoringInstanceMetadata(_ context.Context, monitoringInstanceID string, input monitoringinstances.UpdateMetadataInput) (monitoringinstances.Record, error) {
	f.updateMonitoringInstanceMetadataID = monitoringInstanceID
	f.updateMonitoringInstanceMetadataInput = input
	if f.updateMonitoringInstanceMetadataErr != nil {
		return monitoringinstances.Record{}, f.updateMonitoringInstanceMetadataErr
	}
	return f.updateMonitoringInstanceMetadataResult, nil
}

func (f *fakeMonitoringInstanceRepository) SetPendingAction(_ context.Context, monitoringInstanceID, actionID, commandID string) error {
	f.setPendingActionMonitoringInstanceID = monitoringInstanceID
	f.setPendingActionID = actionID
	f.setPendingActionCommand = commandID
	return f.setPendingActionErr
}

func (f *fakeMonitoringInstanceRepository) GetPendingAction(context.Context, string) (string, string, error) {
	return "", "", nil
}

func (f *fakeMonitoringInstanceRepository) ClearPendingAction(context.Context, string) error {
	return nil
}

func (f *fakeMonitoringInstanceRepository) StoreActionResult(context.Context, string, []byte) error {
	return nil
}

func TestListMonitoringInstancesHandlerReturnsJSON(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceRepository{
		listMonitoringInstancesResult: []monitoringinstances.Record{{
			MonitoringInstanceID: "mi_001",
			DisplayName:          "Tokyo Edge",
			Region:               "ap-northeast-1",
			City:                 "Tokyo",
			Provider:             "Vultr",
			LifecycleStatus:      "待接入",
			MonitoringStatus:     "启用",
			BindingStatus:        "未绑定",
			CurrentHealthStatus:  "正常",
			CreatedAt:            now,
			UpdatedAt:            now,
		}},
	}

	handler := handlers.MonitoringInstancesCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body []monitoringinstances.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if len(body) != 1 {
		t.Fatalf("expected 1 monitoringInstance, got %d", len(body))
	}

	if body[0].MonitoringInstanceID != "mi_001" {
		t.Fatalf("expected monitoring_instance_id %q, got %q", "mi_001", body[0].MonitoringInstanceID)
	}
	if body[0].DisplayName != "Tokyo Edge" {
		t.Fatalf("expected display_name %q, got %q", "Tokyo Edge", body[0].DisplayName)
	}
}

func TestCreateMonitoringInstanceHandlerReturnsCreatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceRepository{
		createMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			DisplayName:          "Tokyo Edge",
			Region:               "ap-northeast-1",
			City:                 "Tokyo",
			Provider:             "Vultr",
			LifecycleStatus:      "待接入",
			MonitoringStatus:     "启用",
			BindingStatus:        "未绑定",
			CurrentHealthStatus:  "正常",
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}

	handler := handlers.MonitoringInstancesCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances", strings.NewReader(`{"display_name":"Tokyo Edge","region":"ap-northeast-1","city":"Tokyo","provider":"Vultr","lifecycle_status":"待接入"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}

	var body monitoringinstances.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if body.MonitoringInstanceID != "mi_001" {
		t.Fatalf("expected monitoring_instance_id %q, got %q", "mi_001", body.MonitoringInstanceID)
	}
	if repo.createMonitoringInstanceInput.DisplayName != "Tokyo Edge" {
		t.Fatalf("expected create input display_name %q, got %q", "Tokyo Edge", repo.createMonitoringInstanceInput.DisplayName)
	}
}

func TestCreateMonitoringInstanceHandlerForcesPendingLifecycleStatus(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceRepository{
		createMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			DisplayName:          "Tokyo Edge",
			Region:               "ap-northeast-1",
			City:                 "Tokyo",
			Provider:             "Vultr",
			LifecycleStatus:      "待接入",
			MonitoringStatus:     "启用",
			BindingStatus:        "未绑定",
			CurrentHealthStatus:  "正常",
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}

	handler := handlers.MonitoringInstancesCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances", strings.NewReader(`{"display_name":"Tokyo Edge","region":"ap-northeast-1","city":"Tokyo","provider":"Vultr","lifecycle_status":"在用"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}
	if repo.createMonitoringInstanceInput.LifecycleStatus != monitoringinstances.LifecyclePendingEnrollment {
		t.Fatalf("create lifecycle_status = %q, want %q", repo.createMonitoringInstanceInput.LifecycleStatus, monitoringinstances.LifecyclePendingEnrollment)
	}
}

func TestMonitoringInstanceItemReturnsNotFound(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{getMonitoringInstanceErr: monitoringinstances.ErrMonitoringInstanceNotFound}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_missing", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if !errors.Is(repo.getMonitoringInstanceErr, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("expected fake repo error to match ErrMonitoringInstanceNotFound")
	}
	if body["error"] != "monitoring instance not found" {
		t.Fatalf("expected error %q, got %q", "monitoring instance not found", body["error"])
	}
}

func TestMonitoringInstanceItemRejectsDeeperPaths(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestMonitoringInstanceActionsQueuesPendingAction(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        monitoringinstances.BindingBound,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		},
	}

	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemd_status"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.setPendingActionMonitoringInstanceID != "mi_001" {
		t.Fatalf("queued monitoringInstance id = %q, want mi_001", repo.setPendingActionMonitoringInstanceID)
	}
	if repo.setPendingActionID == "" {
		t.Fatal("queued action id = empty, want generated id")
	}
	if repo.setPendingActionCommand != "systemd_status" {
		t.Fatalf("queued command = %q, want systemd_status", repo.setPendingActionCommand)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["status"] != "pending" || body["action_id"] == "" || body["command_id"] != "systemd_status" {
		t.Fatalf("body = %#v, want pending action response", body)
	}
}

func TestMonitoringInstanceActionsRejectsInvalidBody(t *testing.T) {
	handler := handlers.MonitoringInstanceActions(&fakeMonitoringInstanceRepository{})
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestMonitoringInstanceActionsReturnsNotFoundForUnknownMonitoringInstance(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{getMonitoringInstanceErr: monitoringinstances.ErrMonitoringInstanceNotFound}
	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemd_status"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if repo.setPendingActionID != "" {
		t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionID)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "monitoring instance not found" {
		t.Fatalf("error = %q, want monitoring instance not found", body["error"])
	}
}

func TestMonitoringInstanceActionsReturnsInternalErrorForRepositoryFailures(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeMonitoringInstanceRepository
	}{
		{
			name: "get monitoringInstance",
			repo: &fakeMonitoringInstanceRepository{getMonitoringInstanceErr: errors.New("lookup failed")},
		},
		{
			name: "set pending action",
			repo: &fakeMonitoringInstanceRepository{
				getMonitoringInstanceResult: monitoringinstances.Record{
					MonitoringInstanceID: "mi_001",
					BindingStatus:        monitoringinstances.BindingBound,
					MonitoringStatus:     monitoringinstances.MonitoringEnabled,
				},
				setPendingActionErr: errors.New("queue failed"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.MonitoringInstanceActions(tt.repo)
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemd_status"}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestMonitoringInstanceActionsRejectsUnavailableMonitoringInstanceStates(t *testing.T) {
	tests := []struct {
		name               string
		monitoringInstance monitoringinstances.Record
	}{
		{
			name:               "unbound",
			monitoringInstance: monitoringinstances.Record{MonitoringInstanceID: "mi_001", BindingStatus: monitoringinstances.BindingUnbound, MonitoringStatus: monitoringinstances.MonitoringEnabled},
		},
		{
			name:               "paused",
			monitoringInstance: monitoringinstances.Record{MonitoringInstanceID: "mi_001", BindingStatus: monitoringinstances.BindingBound, MonitoringStatus: monitoringinstances.MonitoringPaused},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeMonitoringInstanceRepository{getMonitoringInstanceResult: tt.monitoringInstance}
			handler := handlers.MonitoringInstanceActions(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemd_status"}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
			}
			if repo.setPendingActionID != "" {
				t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionID)
			}
		})
	}
}

func TestMonitoringInstanceItemPatchMetadataReturnsUpdatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 27, 9, 0, 0, 0, time.UTC)
	expectedUpdatedAt := time.Date(2026, time.April, 27, 8, 55, 0, 123000000, time.UTC)
	repo := &fakeMonitoringInstanceRepository{
		updateMonitoringInstanceMetadataResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			DisplayName:          "Tokyo Edge",
			Region:               "ap-northeast-1",
			City:                 "Tokyo",
			Provider:             "Vultr",
			LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
			BindingStatus:        monitoringinstances.BindingUnbound,
			Labels:               []string{"edge", "core"},
			Note:                 "updated",
			CurrentHealthStatus:  monitoringinstances.HealthNormal,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/monitoring-instances/mi_001", strings.NewReader(`{"labels":[" edge ","core","edge"],"note":" updated "}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+expectedUpdatedAt.Format(time.RFC3339Nano)+`"`)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.updateMonitoringInstanceMetadataID != "mi_001" {
		t.Fatalf("update monitoring instance id = %q, want %q", repo.updateMonitoringInstanceMetadataID, "mi_001")
	}
	if len(repo.updateMonitoringInstanceMetadataInput.Labels) != 2 || repo.updateMonitoringInstanceMetadataInput.Labels[0] != "edge" || repo.updateMonitoringInstanceMetadataInput.Labels[1] != "core" {
		t.Fatalf("update labels = %#v, want %#v", repo.updateMonitoringInstanceMetadataInput.Labels, []string{"edge", "core"})
	}
	if repo.updateMonitoringInstanceMetadataInput.Note != "updated" {
		t.Fatalf("update note = %q, want %q", repo.updateMonitoringInstanceMetadataInput.Note, "updated")
	}
	if repo.updateMonitoringInstanceMetadataInput.ExpectedUpdatedAt == nil || !repo.updateMonitoringInstanceMetadataInput.ExpectedUpdatedAt.Equal(expectedUpdatedAt) {
		t.Fatalf("expected updated_at = %v, want %s", repo.updateMonitoringInstanceMetadataInput.ExpectedUpdatedAt, expectedUpdatedAt.Format(time.RFC3339Nano))
	}

	var body monitoringinstances.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.MonitoringInstanceID != "mi_001" {
		t.Fatalf("response monitoring_instance_id = %q, want %q", body.MonitoringInstanceID, "mi_001")
	}
	if body.Note != "updated" {
		t.Fatalf("response note = %q, want %q", body.Note, "updated")
	}
	if len(body.Labels) != 2 || body.Labels[0] != "edge" || body.Labels[1] != "core" {
		t.Fatalf("response labels = %#v, want %#v", body.Labels, []string{"edge", "core"})
	}
}

func TestMonitoringInstanceItemRejectsInvalidMetadata(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/monitoring-instances/mi_001", strings.NewReader(`{"labels":["01","02","03","04","05","06","07","08","09","10","11","12","13","14","15","16","17","18","19","20","21"],"note":""}`))
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

func TestMonitoringInstanceItemRejectsPartialMetadataPayloads(t *testing.T) {
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
			repo := &fakeMonitoringInstanceRepository{}

			handler := handlers.MonitoringInstanceItem(repo)
			req := httptest.NewRequest(http.MethodPatch, "/api/monitoring-instances/mi_001", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
			}
			if repo.updateMonitoringInstanceMetadataID != "" {
				t.Fatalf("UpdateMonitoringInstanceMetadata called for partial payload")
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

func TestMonitoringInstanceItemMetadataValidationCountsUnicodeCharacters(t *testing.T) {
	now := time.Date(2026, time.April, 27, 9, 0, 0, 0, time.UTC)
	label := strings.Repeat("候", 64)
	note := strings.Repeat("风", 2000)
	repo := &fakeMonitoringInstanceRepository{
		updateMonitoringInstanceMetadataResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			DisplayName:          "Tokyo Edge",
			Region:               "ap-northeast-1",
			City:                 "Tokyo",
			Provider:             "Vultr",
			LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
			BindingStatus:        monitoringinstances.BindingUnbound,
			Labels:               []string{label},
			Note:                 note,
			CurrentHealthStatus:  monitoringinstances.HealthNormal,
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/monitoring-instances/mi_001", strings.NewReader(`{"labels":["`+label+`"],"note":"`+note+`"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if got := repo.updateMonitoringInstanceMetadataInput.Labels; len(got) != 1 || got[0] != label {
		t.Fatalf("update labels = %#v, want %#v", got, []string{label})
	}
	if repo.updateMonitoringInstanceMetadataInput.Note != note {
		t.Fatalf("update note length = %d, want %d", len([]rune(repo.updateMonitoringInstanceMetadataInput.Note)), 2000)
	}
}

func TestMonitoringInstanceItemMapsMetadataNotFound(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{updateMonitoringInstanceMetadataErr: monitoringinstances.ErrMonitoringInstanceNotFound}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/monitoring-instances/mi_missing", strings.NewReader(`{"labels":["edge"],"note":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestMonitoringInstanceItemMapsMetadataConflict(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{updateMonitoringInstanceMetadataErr: monitoringinstances.ErrMonitoringInstanceMetadataConflict}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/monitoring-instances/mi_001", strings.NewReader(`{"labels":["edge"],"note":"updated"}`))
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
