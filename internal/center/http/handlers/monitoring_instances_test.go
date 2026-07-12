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
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/monitoringinstances"
)

type fakeMonitoringInstanceRepository struct {
	listMonitoringInstancesResult           []monitoringinstances.Record
	listMonitoringInstancesErr              error
	getMonitoringInstanceResult             monitoringinstances.Record
	getMonitoringInstanceErr                error
	createMonitoringInstanceResult          monitoringinstances.Record
	createMonitoringInstanceErr             error
	createMonitoringInstanceInput           monitoringinstances.CreateInput
	setPendingActionErr                     error
	setPendingActionMonitoringInstanceID    string
	setPendingActionInput                   monitoringinstances.QueueCommandActionInput
	rejectedActionAuditErr                  error
	rejectedActionAuditCalls                int
	rejectedActionAuditMonitoringInstanceID string
	rejectedActionAuditInput                monitoringinstances.RejectedCommandActionInput
	getMonitoringInstanceCalls              int
	getMonitoringInstanceID                 string
	updateMonitoringInstanceMetadataResult  monitoringinstances.Record
	updateMonitoringInstanceMetadataErr     error
	updateMonitoringInstanceMetadataID      string
	updateMonitoringInstanceMetadataInput   monitoringinstances.UpdateMetadataInput
	listMonitoringInstancesScope            monitoringinstances.ListScope
	managementReviewResult                  monitoringinstances.ManagementReview
	managementReviewErr                     error
	managementReviewID                      string
	retireResult                            monitoringinstances.Record
	retireErr                               error
	retireID                                string
	retireInput                             monitoringinstances.LifecycleActionInput
	restoreLifecycleResult                  monitoringinstances.Record
	restoreLifecycleErr                     error
	restoreLifecycleID                      string
	restoreLifecycleInput                   monitoringinstances.LifecycleActionInput
	archiveResult                           monitoringinstances.Record
	archiveErr                              error
	archiveID                               string
	archiveInput                            monitoringinstances.ArchiveInput
	restoreArchiveResult                    monitoringinstances.Record
	restoreArchiveErr                       error
	restoreArchiveID                        string
	cleanupResult                           monitoringinstances.PermanentCleanupResult
	cleanupErr                              error
	cleanupID                               string
	cleanupInput                            monitoringinstances.PermanentCleanupInput
}

func (f *fakeMonitoringInstanceRepository) ListMonitoringInstances(_ context.Context, scopes ...monitoringinstances.ListScope) ([]monitoringinstances.Record, error) {
	if len(scopes) > 0 {
		f.listMonitoringInstancesScope = scopes[0]
	}
	return f.listMonitoringInstancesResult, f.listMonitoringInstancesErr
}

func (f *fakeMonitoringInstanceRepository) GetMonitoringInstance(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.getMonitoringInstanceCalls++
	f.getMonitoringInstanceID = monitoringInstanceID
	if f.getMonitoringInstanceErr != nil {
		return monitoringinstances.Record{}, f.getMonitoringInstanceErr
	}
	return f.getMonitoringInstanceResult, nil
}

func (f *fakeMonitoringInstanceRepository) RecordRejectedCommandAction(_ context.Context, monitoringInstanceID string, input monitoringinstances.RejectedCommandActionInput) error {
	f.rejectedActionAuditCalls++
	f.rejectedActionAuditMonitoringInstanceID = monitoringInstanceID
	f.rejectedActionAuditInput = input
	return f.rejectedActionAuditErr
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

func (f *fakeMonitoringInstanceRepository) QueueCommandAction(_ context.Context, monitoringInstanceID string, input monitoringinstances.QueueCommandActionInput) error {
	f.setPendingActionMonitoringInstanceID = monitoringInstanceID
	f.setPendingActionInput = input
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

func (f *fakeMonitoringInstanceRepository) GetMonitoringInstanceManagementReview(_ context.Context, monitoringInstanceID string) (monitoringinstances.ManagementReview, error) {
	f.managementReviewID = monitoringInstanceID
	if f.managementReviewErr != nil {
		return monitoringinstances.ManagementReview{}, f.managementReviewErr
	}
	return f.managementReviewResult, nil
}

func (f *fakeMonitoringInstanceRepository) RetireMonitoringInstance(_ context.Context, monitoringInstanceID string, input monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error) {
	f.retireID = monitoringInstanceID
	f.retireInput = input
	if f.retireErr != nil {
		return monitoringinstances.Record{}, f.retireErr
	}
	return f.retireResult, nil
}

func (f *fakeMonitoringInstanceRepository) RestoreMonitoringInstanceLifecycle(_ context.Context, monitoringInstanceID string, input monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error) {
	f.restoreLifecycleID = monitoringInstanceID
	f.restoreLifecycleInput = input
	if f.restoreLifecycleErr != nil {
		return monitoringinstances.Record{}, f.restoreLifecycleErr
	}
	return f.restoreLifecycleResult, nil
}

func (f *fakeMonitoringInstanceRepository) ArchiveMonitoringInstance(_ context.Context, monitoringInstanceID string, input monitoringinstances.ArchiveInput) (monitoringinstances.Record, error) {
	f.archiveID = monitoringInstanceID
	f.archiveInput = input
	if f.archiveErr != nil {
		return monitoringinstances.Record{}, f.archiveErr
	}
	return f.archiveResult, nil
}

func (f *fakeMonitoringInstanceRepository) RestoreMonitoringInstanceFromArchive(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.restoreArchiveID = monitoringInstanceID
	if f.restoreArchiveErr != nil {
		return monitoringinstances.Record{}, f.restoreArchiveErr
	}
	return f.restoreArchiveResult, nil
}

func (f *fakeMonitoringInstanceRepository) PermanentCleanupMonitoringInstance(_ context.Context, monitoringInstanceID string, input monitoringinstances.PermanentCleanupInput) (monitoringinstances.PermanentCleanupResult, error) {
	f.cleanupID = monitoringInstanceID
	f.cleanupInput = input
	if f.cleanupErr != nil {
		return monitoringinstances.PermanentCleanupResult{}, f.cleanupErr
	}
	return f.cleanupResult, nil
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

func TestListMonitoringInstancesHandlerPassesScope(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{}

	handler := handlers.MonitoringInstancesCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances?scope=archived", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.listMonitoringInstancesScope != monitoringinstances.ListScopeArchived {
		t.Fatalf("list scope = %q, want %q", repo.listMonitoringInstancesScope, monitoringinstances.ListScopeArchived)
	}
}

func TestListMonitoringInstancesHandlerRejectsInvalidScope(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{}

	handler := handlers.MonitoringInstancesCollection(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances?scope=deleted", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
	if repo.listMonitoringInstancesScope != "" {
		t.Fatalf("list scope = %q, want no repository call", repo.listMonitoringInstancesScope)
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

func TestMonitoringInstanceManagementReviewReturnsReview(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceRepository{
		managementReviewResult: monitoringinstances.ManagementReview{
			Record: monitoringinstances.Record{
				MonitoringInstanceID: "mi_001",
				DisplayName:          "Tokyo Edge",
				LifecycleStatus:      monitoringinstances.LifecycleRetired,
				MonitoringStatus:     monitoringinstances.MonitoringPaused,
				CurrentHealthStatus:  monitoringinstances.HealthNormal,
				CreatedAt:            now,
				UpdatedAt:            now,
			},
			Counts:                monitoringinstances.ManagementCounts{HeartbeatCount: 2},
			Warnings:              []string{"可清理"},
			EmptyMistakeCandidate: false,
			Actions:               monitoringinstances.ManagementActions{CanArchive: true},
		},
	}

	handler := handlers.MonitoringInstanceManagementReview(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/management-review", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.managementReviewID != "mi_001" {
		t.Fatalf("management review id = %q, want mi_001", repo.managementReviewID)
	}
	var body monitoringinstances.ManagementReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Record.MonitoringInstanceID != "mi_001" || body.Counts.HeartbeatCount != 2 || !body.Actions.CanArchive {
		t.Fatalf("review body = %#v, want populated review", body)
	}
}

func TestMonitoringInstanceManagementActionsCallRepository(t *testing.T) {
	now := time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC)
	archivedAt := now.Add(time.Minute)
	tests := []struct {
		name       string
		handler    func(*fakeMonitoringInstanceRepository) http.Handler
		method     string
		path       string
		body       string
		assertRepo func(*testing.T, *fakeMonitoringInstanceRepository)
	}{
		{
			name: "retire",
			handler: func(repo *fakeMonitoringInstanceRepository) http.Handler {
				return handlers.MonitoringInstanceLifecycleRetire(repo)
			},
			method: http.MethodPost,
			path:   "/api/monitoring-instances/mi_001/lifecycle/retire",
			body:   `{"reason":" duplicate "}`,
			assertRepo: func(t *testing.T, repo *fakeMonitoringInstanceRepository) {
				t.Helper()
				if repo.retireID != "mi_001" || repo.retireInput.Reason != " duplicate " {
					t.Fatalf("retire call = %q %#v, want id and raw input", repo.retireID, repo.retireInput)
				}
			},
		},
		{
			name: "restore lifecycle",
			handler: func(repo *fakeMonitoringInstanceRepository) http.Handler {
				return handlers.MonitoringInstanceLifecycleRestore(repo)
			},
			method: http.MethodPost,
			path:   "/api/monitoring-instances/mi_001/lifecycle/restore",
			body:   `{"reason":" restore "}`,
			assertRepo: func(t *testing.T, repo *fakeMonitoringInstanceRepository) {
				t.Helper()
				if repo.restoreLifecycleID != "mi_001" || repo.restoreLifecycleInput.Reason != " restore " {
					t.Fatalf("restore lifecycle call = %q %#v, want id and raw input", repo.restoreLifecycleID, repo.restoreLifecycleInput)
				}
			},
		},
		{
			name: "archive",
			handler: func(repo *fakeMonitoringInstanceRepository) http.Handler {
				return handlers.MonitoringInstanceArchive(repo)
			},
			method: http.MethodPost,
			path:   "/api/monitoring-instances/mi_001/archive",
			body:   `{"reason":" duplicate ","confirmation_name":"Tokyo Edge"}`,
			assertRepo: func(t *testing.T, repo *fakeMonitoringInstanceRepository) {
				t.Helper()
				if repo.archiveID != "mi_001" || repo.archiveInput.Reason != " duplicate " || repo.archiveInput.ConfirmationName != "Tokyo Edge" {
					t.Fatalf("archive call = %q %#v, want id and input", repo.archiveID, repo.archiveInput)
				}
			},
		},
		{
			name: "restore archive",
			handler: func(repo *fakeMonitoringInstanceRepository) http.Handler {
				return handlers.MonitoringInstanceRestoreFromArchive(repo)
			},
			method: http.MethodPost,
			path:   "/api/monitoring-instances/mi_001/restore-from-archive",
			body:   ``,
			assertRepo: func(t *testing.T, repo *fakeMonitoringInstanceRepository) {
				t.Helper()
				if repo.restoreArchiveID != "mi_001" {
					t.Fatalf("restore archive id = %q, want mi_001", repo.restoreArchiveID)
				}
			},
		},
		{
			name: "permanent cleanup",
			handler: func(repo *fakeMonitoringInstanceRepository) http.Handler {
				return handlers.MonitoringInstancePermanentCleanup(repo)
			},
			method: http.MethodPost,
			path:   "/api/monitoring-instances/mi_001/permanent-cleanup",
			body:   `{"reason":" cleanup ","confirmation_name":"Tokyo Edge"}`,
			assertRepo: func(t *testing.T, repo *fakeMonitoringInstanceRepository) {
				t.Helper()
				if repo.cleanupID != "mi_001" || repo.cleanupInput.Reason != " cleanup " || repo.cleanupInput.ConfirmationName != "Tokyo Edge" {
					t.Fatalf("cleanup call = %q %#v, want id and input", repo.cleanupID, repo.cleanupInput)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeMonitoringInstanceRepository{
				retireResult:           monitoringinstances.Record{MonitoringInstanceID: "mi_001", LifecycleStatus: monitoringinstances.LifecycleRetired, MonitoringStatus: monitoringinstances.MonitoringPaused, CurrentHealthStatus: monitoringinstances.HealthNormal, CreatedAt: now, UpdatedAt: now},
				restoreLifecycleResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", LifecycleStatus: monitoringinstances.LifecycleObserving, MonitoringStatus: monitoringinstances.MonitoringPaused, CurrentHealthStatus: monitoringinstances.HealthNormal, CreatedAt: now, UpdatedAt: now},
				archiveResult:          monitoringinstances.Record{MonitoringInstanceID: "mi_001", ArchivedAt: &archivedAt, ArchivedReason: "duplicate", CurrentHealthStatus: monitoringinstances.HealthNormal, CreatedAt: now, UpdatedAt: now},
				restoreArchiveResult:   monitoringinstances.Record{MonitoringInstanceID: "mi_001", LifecycleStatus: monitoringinstances.LifecycleObserving, MonitoringStatus: monitoringinstances.MonitoringPaused, CurrentHealthStatus: monitoringinstances.HealthNormal, CreatedAt: now, UpdatedAt: now},
				cleanupResult:          monitoringinstances.PermanentCleanupResult{MonitoringInstanceID: "mi_001", Deleted: true, DeletedReferenceCount: 3},
			}
			handler := tt.handler(repo)
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			tt.assertRepo(t, repo)
		})
	}
}

func TestMonitoringInstanceManagementHandlersValidateInputAndMapErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "review wrong method", handler: handlers.MonitoringInstanceManagementReview(&fakeMonitoringInstanceRepository{}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/management-review", want: http.StatusMethodNotAllowed},
		{name: "review malformed path", handler: handlers.MonitoringInstanceManagementReview(&fakeMonitoringInstanceRepository{}), method: http.MethodGet, path: "/api/monitoring-instances/mi_001/management-review/extra", want: http.StatusNotFound},
		{name: "review missing instance", handler: handlers.MonitoringInstanceManagementReview(&fakeMonitoringInstanceRepository{managementReviewErr: monitoringinstances.ErrMonitoringInstanceNotFound}), method: http.MethodGet, path: "/api/monitoring-instances/mi_missing/management-review", want: http.StatusNotFound},
		{name: "retire invalid json", handler: handlers.MonitoringInstanceLifecycleRetire(&fakeMonitoringInstanceRepository{}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/lifecycle/retire", body: `{`, want: http.StatusBadRequest},
		{name: "retire missing reason", handler: handlers.MonitoringInstanceLifecycleRetire(&fakeMonitoringInstanceRepository{}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/lifecycle/retire", body: `{}`, want: http.StatusBadRequest},
		{name: "retire archived blocked", handler: handlers.MonitoringInstanceLifecycleRetire(&fakeMonitoringInstanceRepository{retireErr: monitoringinstances.ErrArchivedMonitoringInstance}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/lifecycle/retire", body: `{"reason":"done"}`, want: http.StatusConflict},
		{name: "restore lifecycle blocked", handler: handlers.MonitoringInstanceLifecycleRestore(&fakeMonitoringInstanceRepository{restoreLifecycleErr: monitoringinstances.ErrManagementActionBlocked}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/lifecycle/restore", body: `{"reason":"done"}`, want: http.StatusConflict},
		{name: "archive missing confirmation", handler: handlers.MonitoringInstanceArchive(&fakeMonitoringInstanceRepository{}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/archive", body: `{"reason":"done"}`, want: http.StatusBadRequest},
		{name: "archive missing instance", handler: handlers.MonitoringInstanceArchive(&fakeMonitoringInstanceRepository{archiveErr: monitoringinstances.ErrMonitoringInstanceNotFound}), method: http.MethodPost, path: "/api/monitoring-instances/mi_missing/archive", body: `{"reason":"done","confirmation_name":"Tokyo Edge"}`, want: http.StatusNotFound},
		{name: "restore archive wrong method", handler: handlers.MonitoringInstanceRestoreFromArchive(&fakeMonitoringInstanceRepository{}), method: http.MethodGet, path: "/api/monitoring-instances/mi_001/restore-from-archive", want: http.StatusMethodNotAllowed},
		{name: "cleanup missing confirmation", handler: handlers.MonitoringInstancePermanentCleanup(&fakeMonitoringInstanceRepository{}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/permanent-cleanup", body: `{"reason":"done"}`, want: http.StatusBadRequest},
		{name: "cleanup blocked", handler: handlers.MonitoringInstancePermanentCleanup(&fakeMonitoringInstanceRepository{cleanupErr: monitoringinstances.ErrManagementActionBlocked}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/permanent-cleanup", body: `{"reason":"done","confirmation_name":"Tokyo Edge"}`, want: http.StatusConflict},
		{name: "cleanup repo failure", handler: handlers.MonitoringInstancePermanentCleanup(&fakeMonitoringInstanceRepository{cleanupErr: errors.New("boom")}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/permanent-cleanup", body: `{"reason":"done","confirmation_name":"Tokyo Edge"}`, want: http.StatusInternalServerError},
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

func TestMonitoringInstanceActionsQueuesStandardCommandWithoutConfirmation(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        monitoringinstances.BindingBound,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		},
	}

	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"uptime"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.setPendingActionMonitoringInstanceID != "mi_001" {
		t.Fatalf("queued monitoringInstance id = %q, want mi_001", repo.setPendingActionMonitoringInstanceID)
	}
	if repo.setPendingActionInput.ActionID == "" {
		t.Fatal("queued action id = empty, want generated id")
	}
	if repo.setPendingActionInput.CommandID != "uptime" {
		t.Fatalf("queued command = %q, want uptime", repo.setPendingActionInput.CommandID)
	}
	if repo.setPendingActionInput.Sensitivity != "standard" {
		t.Fatalf("queued sensitivity = %q, want standard", repo.setPendingActionInput.Sensitivity)
	}
	if repo.setPendingActionInput.Source != "web" {
		t.Fatalf("queued source = %q, want web", repo.setPendingActionInput.Source)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["status"] != "pending" || body["action_id"] == "" || body["command_id"] != "uptime" {
		t.Fatalf("body = %#v, want pending action response", body)
	}
}

func TestMonitoringInstanceActionsRejectsSensitiveCommandWithoutConfirmation(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        monitoringinstances.BindingBound,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		},
	}

	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemctl_status"}`))
	req = req.WithContext(sessionctx.WithUserID(req.Context(), "u_operator"))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if repo.setPendingActionInput.ActionID != "" {
		t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionInput.ActionID)
	}
	if repo.getMonitoringInstanceCalls != 1 || repo.getMonitoringInstanceID != "mi_001" {
		t.Fatalf("GetMonitoringInstance calls/id = %d/%q, want 1/mi_001", repo.getMonitoringInstanceCalls, repo.getMonitoringInstanceID)
	}
	if repo.rejectedActionAuditCalls != 1 || repo.rejectedActionAuditMonitoringInstanceID != "mi_001" {
		t.Fatalf("rejected audit calls/id = %d/%q, want 1/mi_001", repo.rejectedActionAuditCalls, repo.rejectedActionAuditMonitoringInstanceID)
	}
	if repo.rejectedActionAuditInput.CommandID != "systemctl_status" || repo.rejectedActionAuditInput.Sensitivity != "sensitive" || repo.rejectedActionAuditInput.ActorUserID != "u_operator" || repo.rejectedActionAuditInput.Source != "web" || repo.rejectedActionAuditInput.OccurredAt.IsZero() {
		t.Fatalf("rejected audit input = %#v", repo.rejectedActionAuditInput)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "sensitive command confirmation required" {
		t.Fatalf("error = %q, want sensitive command confirmation required", body["error"])
	}
}

func TestMonitoringInstanceActionsDoesNotAuditUntrustedSensitiveRejections(t *testing.T) {
	archivedAt := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		record monitoringinstances.Record
		err    error
	}{
		{name: "missing instance", err: monitoringinstances.ErrMonitoringInstanceNotFound},
		{
			name: "archived instance",
			record: monitoringinstances.Record{
				MonitoringInstanceID: "mi_001", BindingStatus: monitoringinstances.BindingBound, MonitoringStatus: monitoringinstances.MonitoringEnabled, ArchivedAt: &archivedAt,
			},
		},
		{
			name: "unbound instance",
			record: monitoringinstances.Record{
				MonitoringInstanceID: "mi_001", BindingStatus: monitoringinstances.BindingUnbound, MonitoringStatus: monitoringinstances.MonitoringEnabled,
			},
		},
		{
			name: "paused instance",
			record: monitoringinstances.Record{
				MonitoringInstanceID: "mi_001", BindingStatus: monitoringinstances.BindingBound, MonitoringStatus: monitoringinstances.MonitoringPaused,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeMonitoringInstanceRepository{getMonitoringInstanceResult: tt.record, getMonitoringInstanceErr: tt.err}
			handler := handlers.MonitoringInstanceActions(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemctl_status"}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if repo.getMonitoringInstanceCalls != 1 {
				t.Fatalf("GetMonitoringInstance calls = %d, want 1", repo.getMonitoringInstanceCalls)
			}
			if repo.rejectedActionAuditCalls != 0 || repo.setPendingActionInput.ActionID != "" {
				t.Fatalf("rejected audit calls = %d, queued input = %#v", repo.rejectedActionAuditCalls, repo.setPendingActionInput)
			}
		})
	}
}

func TestMonitoringInstanceActionsReturnsInternalErrorWhenTrustedRejectionCannotBeAudited(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeMonitoringInstanceRepository
	}{
		{
			name: "instance lookup",
			repo: &fakeMonitoringInstanceRepository{getMonitoringInstanceErr: errors.New("lookup failed")},
		},
		{
			name: "audit write",
			repo: &fakeMonitoringInstanceRepository{
				getMonitoringInstanceResult: monitoringinstances.Record{
					MonitoringInstanceID: "mi_001", BindingStatus: monitoringinstances.BindingBound, MonitoringStatus: monitoringinstances.MonitoringEnabled,
				},
				rejectedActionAuditErr: errors.New("audit failed"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := handlers.MonitoringInstanceActions(tt.repo)
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemctl_status"}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
			}
			if tt.repo.setPendingActionInput.ActionID != "" {
				t.Fatalf("queued action input = %#v, want none", tt.repo.setPendingActionInput)
			}
		})
	}
}

func TestMonitoringInstanceActionsQueuesSensitiveCommandWithConfirmationAndActor(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        monitoringinstances.BindingBound,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		},
	}

	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemctl_status","confirmed_sensitive":true}`))
	req = req.WithContext(sessionctx.WithUserID(req.Context(), "u_operator"))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.setPendingActionInput.CommandID != "systemctl_status" {
		t.Fatalf("queued command = %q, want systemctl_status", repo.setPendingActionInput.CommandID)
	}
	if repo.setPendingActionInput.Sensitivity != "sensitive" {
		t.Fatalf("queued sensitivity = %q, want sensitive", repo.setPendingActionInput.Sensitivity)
	}
	if repo.setPendingActionInput.ActorUserID != "u_operator" {
		t.Fatalf("queued actor = %q, want u_operator", repo.setPendingActionInput.ActorUserID)
	}
	if repo.setPendingActionInput.Source != "web" {
		t.Fatalf("queued source = %q, want web", repo.setPendingActionInput.Source)
	}
}

func TestMonitoringInstanceActionsRejectsUnknownCommandIDBeforeRepositoryWrite(t *testing.T) {
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

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if repo.setPendingActionInput.ActionID != "" {
		t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionInput.ActionID)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "invalid command_id" {
		t.Fatalf("error = %q, want invalid command_id", body["error"])
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

func TestMonitoringInstanceActionsRejectsTrailingJSONBeforeRepositoryWrite(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        monitoringinstances.BindingBound,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		},
	}
	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemctl_status"}{"command_id":"uptime"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if repo.setPendingActionInput.ActionID != "" {
		t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionInput.ActionID)
	}
}

func TestMonitoringInstanceActionsRejectsUnknownFieldsBeforeRepositoryWrite(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        monitoringinstances.BindingBound,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		},
	}
	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"systemctl_status","args":["-a"]}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if repo.setPendingActionInput.ActionID != "" {
		t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionInput.ActionID)
	}
}

func TestMonitoringInstanceActionsReturnsNotFoundForUnknownMonitoringInstance(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{getMonitoringInstanceErr: monitoringinstances.ErrMonitoringInstanceNotFound}
	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"uptime"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if repo.setPendingActionInput.ActionID != "" {
		t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionInput.ActionID)
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
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"uptime"}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestMonitoringInstanceActionsRejectsArchivedMonitoringInstance(t *testing.T) {
	t.Parallel()

	archivedAt := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceRepository{getMonitoringInstanceResult: monitoringinstances.Record{
		MonitoringInstanceID: "mi_archived",
		BindingStatus:        monitoringinstances.BindingBound,
		MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		ArchivedAt:           &archivedAt,
	}}
	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_archived/actions", strings.NewReader(`{"command_id":"uptime"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if repo.setPendingActionInput.ActionID != "" {
		t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionInput.ActionID)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "archived monitoring instance" {
		t.Fatalf("error = %q, want archived monitoring instance", body["error"])
	}
}

func TestMonitoringInstanceActionsMapsArchivedSetPendingRace(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceRepository{
		getMonitoringInstanceResult: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        monitoringinstances.BindingBound,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		},
		setPendingActionErr: monitoringinstances.ErrArchivedMonitoringInstance,
	}
	handler := handlers.MonitoringInstanceActions(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"uptime"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "archived monitoring instance" {
		t.Fatalf("error = %q, want archived monitoring instance", body["error"])
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
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/actions", strings.NewReader(`{"command_id":"uptime"}`))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
			}
			if repo.setPendingActionInput.ActionID != "" {
				t.Fatalf("queued action id = %q, want no queued action", repo.setPendingActionInput.ActionID)
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

func TestMonitoringInstanceItemMapsMetadataArchived(t *testing.T) {
	repo := &fakeMonitoringInstanceRepository{updateMonitoringInstanceMetadataErr: monitoringinstances.ErrArchivedMonitoringInstance}

	handler := handlers.MonitoringInstanceItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/monitoring-instances/mi_archived", strings.NewReader(`{"labels":["edge"],"note":"updated"}`))
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
	if body["error"] != "archived monitoring instance" {
		t.Fatalf("error = %q, want archived monitoring instance", body["error"])
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
