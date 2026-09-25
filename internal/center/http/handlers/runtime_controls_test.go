package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/store"
	"houfeng/internal/center/targets"
)

type fakeMonitoringInstanceRuntimeControlRepository struct {
	setMaintenanceMonitoringInstanceID string
	setMaintenanceResult               monitoringinstances.Record
	setMaintenanceErr                  error

	pauseMonitoringInstanceID string
	pauseResult               monitoringinstances.Record
	pauseErr                  error

	resumeMonitoringInstanceID string
	resumeResult               monitoringinstances.Record
	resumeErr                  error
}

func (f *fakeMonitoringInstanceRuntimeControlRepository) SetMonitoringInstanceMonitoringMaintenance(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.setMaintenanceMonitoringInstanceID = monitoringInstanceID
	if f.setMaintenanceErr != nil {
		return monitoringinstances.Record{}, f.setMaintenanceErr
	}
	return f.setMaintenanceResult, nil
}

func (f *fakeMonitoringInstanceRuntimeControlRepository) PauseMonitoringInstanceMonitoring(_ context.Context, monitoringInstanceID string, _ ...monitoringinstances.RuntimeControlInput) (monitoringinstances.Record, error) {
	f.pauseMonitoringInstanceID = monitoringInstanceID
	if f.pauseErr != nil {
		return monitoringinstances.Record{}, f.pauseErr
	}
	return f.pauseResult, nil
}

func (f *fakeMonitoringInstanceRuntimeControlRepository) ResumeMonitoringInstanceMonitoring(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.resumeMonitoringInstanceID = monitoringInstanceID
	if f.resumeErr != nil {
		return monitoringinstances.Record{}, f.resumeErr
	}
	return f.resumeResult, nil
}

type fakeTargetRuntimeControlRepository struct {
	err error
}

func (f *fakeTargetRuntimeControlRepository) SetTargetMaintenance(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *fakeTargetRuntimeControlRepository) PauseTargetRun(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *fakeTargetRuntimeControlRepository) ResumeTargetRun(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *fakeTargetRuntimeControlRepository) ArchiveTarget(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *fakeTargetRuntimeControlRepository) RestoreArchivedTargetToPaused(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func TestMonitoringInstanceRuntimeControlHandlerReturnsUpdatedMonitoringInstance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 26, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name                     string
		path                     string
		wantMonitoringInstanceID string
		wantStatus               string
		buildRepo                func() *fakeMonitoringInstanceRuntimeControlRepository
		assertCalledID           func(*testing.T, *fakeMonitoringInstanceRuntimeControlRepository, string)
	}{
		{
			name:                     "enter maintenance",
			path:                     "/api/monitoring-instances/mi_001/runtime/enter-maintenance",
			wantMonitoringInstanceID: "mi_001",
			wantStatus:               "维护中",
			buildRepo: func() *fakeMonitoringInstanceRuntimeControlRepository {
				return &fakeMonitoringInstanceRuntimeControlRepository{setMaintenanceResult: monitoringinstances.Record{MonitoringInstanceID: "mi_001", MonitoringStatus: "维护中", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeMonitoringInstanceRuntimeControlRepository, want string) {
				t.Helper()
				if repo.setMaintenanceMonitoringInstanceID != want {
					t.Fatalf("SetMonitoringInstanceMonitoringMaintenance monitoringInstanceID = %q, want %q", repo.setMaintenanceMonitoringInstanceID, want)
				}
			},
		},
		{
			name:                     "exit maintenance",
			path:                     "/api/monitoring-instances/mi_002/runtime/exit-maintenance",
			wantMonitoringInstanceID: "mi_002",
			wantStatus:               "启用",
			buildRepo: func() *fakeMonitoringInstanceRuntimeControlRepository {
				return &fakeMonitoringInstanceRuntimeControlRepository{resumeResult: monitoringinstances.Record{MonitoringInstanceID: "mi_002", MonitoringStatus: "启用", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeMonitoringInstanceRuntimeControlRepository, want string) {
				t.Helper()
				if repo.resumeMonitoringInstanceID != want {
					t.Fatalf("ResumeMonitoringInstanceMonitoring monitoringInstanceID = %q, want %q", repo.resumeMonitoringInstanceID, want)
				}
			},
		},
		{
			name:                     "pause",
			path:                     "/api/monitoring-instances/mi_003/runtime/pause",
			wantMonitoringInstanceID: "mi_003",
			wantStatus:               "暂停",
			buildRepo: func() *fakeMonitoringInstanceRuntimeControlRepository {
				return &fakeMonitoringInstanceRuntimeControlRepository{pauseResult: monitoringinstances.Record{MonitoringInstanceID: "mi_003", MonitoringStatus: "暂停", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeMonitoringInstanceRuntimeControlRepository, want string) {
				t.Helper()
				if repo.pauseMonitoringInstanceID != want {
					t.Fatalf("PauseMonitoringInstanceMonitoring monitoringInstanceID = %q, want %q", repo.pauseMonitoringInstanceID, want)
				}
			},
		},
		{
			name:                     "resume",
			path:                     "/api/monitoring-instances/mi_004/runtime/resume",
			wantMonitoringInstanceID: "mi_004",
			wantStatus:               "启用",
			buildRepo: func() *fakeMonitoringInstanceRuntimeControlRepository {
				return &fakeMonitoringInstanceRuntimeControlRepository{resumeResult: monitoringinstances.Record{MonitoringInstanceID: "mi_004", MonitoringStatus: "启用", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeMonitoringInstanceRuntimeControlRepository, want string) {
				t.Helper()
				if repo.resumeMonitoringInstanceID != want {
					t.Fatalf("ResumeMonitoringInstanceMonitoring monitoringInstanceID = %q, want %q", repo.resumeMonitoringInstanceID, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.buildRepo()
			handler := handlers.MonitoringInstanceRuntimeControls(repo)
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			tt.assertCalledID(t, repo, tt.wantMonitoringInstanceID)

			var body monitoringinstances.Record
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if body.MonitoringInstanceID != tt.wantMonitoringInstanceID {
				t.Fatalf("MonitoringInstanceID = %q, want %q", body.MonitoringInstanceID, tt.wantMonitoringInstanceID)
			}
			if body.MonitoringStatus != tt.wantStatus {
				t.Fatalf("MonitoringStatus = %q, want %q", body.MonitoringStatus, tt.wantStatus)
			}
		})
	}
}

func TestMonitoringInstanceRuntimeControlHandlerMapsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		repo        *fakeMonitoringInstanceRuntimeControlRepository
		path        string
		wantStatus  int
		wantMessage string
		wantCode    string
	}{
		{
			name:        "invalid transition",
			repo:        &fakeMonitoringInstanceRuntimeControlRepository{setMaintenanceErr: errors.Join(store.ErrInvalidMonitoringInstanceRuntimeTransition, errors.New("cannot enter maintenance"))},
			path:        "/api/monitoring-instances/mi_001/runtime/enter-maintenance",
			wantStatus:  http.StatusConflict,
			wantMessage: "invalid runtime transition",
		},
		{
			name:        "not found",
			repo:        &fakeMonitoringInstanceRuntimeControlRepository{pauseErr: monitoringinstances.ErrMonitoringInstanceNotFound},
			path:        "/api/monitoring-instances/mi_missing/runtime/pause",
			wantStatus:  http.StatusNotFound,
			wantMessage: "monitoring instance not found",
		},
		{
			name:        "archived monitoring instance",
			repo:        &fakeMonitoringInstanceRuntimeControlRepository{resumeErr: monitoringinstances.ErrArchivedMonitoringInstance},
			path:        "/api/monitoring-instances/mi_archived/runtime/resume",
			wantStatus:  http.StatusConflict,
			wantMessage: "archived monitoring instance",
		},
		{
			name:        "retired monitoring instance",
			repo:        &fakeMonitoringInstanceRuntimeControlRepository{resumeErr: monitoringinstances.ErrRetiredMonitoringInstance},
			path:        "/api/monitoring-instances/mi_retired/runtime/resume",
			wantStatus:  http.StatusConflict,
			wantMessage: "retired monitoring instance",
		},
		{
			name:        "shared impact confirmation required",
			repo:        &fakeMonitoringInstanceRuntimeControlRepository{pauseErr: assetlifecycle.ErrSharedImpactConfirmationRequired},
			path:        "/api/monitoring-instances/mi_shared/runtime/pause",
			wantStatus:  http.StatusConflict,
			wantMessage: "shared impact confirmation required",
			wantCode:    "shared_impact_confirmation_required",
		},
		{
			name:        "stale management review",
			repo:        &fakeMonitoringInstanceRuntimeControlRepository{pauseErr: assetlifecycle.ErrStaleCancellationPreview},
			path:        "/api/monitoring-instances/mi_shared/runtime/pause",
			wantStatus:  http.StatusConflict,
			wantMessage: "monitoring instance management review stale",
			wantCode:    "management_review_stale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := handlers.MonitoringInstanceRuntimeControls(tt.repo)
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			assertAdminError(t, recorder, tt.wantMessage)
			if tt.wantCode != "" {
				assertAdminErrorCode(t, recorder, tt.wantCode)
			}
		})
	}
}

func TestTargetRuntimeControlHandlerMapsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		repo        *fakeTargetRuntimeControlRepository
		path        string
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "invalid transition",
			repo:        &fakeTargetRuntimeControlRepository{err: errors.Join(store.ErrInvalidTargetRuntimeTransition, errors.New("cannot archive"))},
			path:        "/api/targets/tg_001/runtime/archive",
			wantStatus:  http.StatusConflict,
			wantMessage: "invalid runtime transition",
		},
		{
			name:        "not found",
			repo:        &fakeTargetRuntimeControlRepository{err: targets.ErrTargetNotFound},
			path:        "/api/targets/tg_missing/runtime/restore-to-paused",
			wantStatus:  http.StatusNotFound,
			wantMessage: "target not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := handlers.TargetRuntimeControls(tt.repo)
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			assertAdminError(t, recorder, tt.wantMessage)
		})
	}
}

func TestRuntimeControlHandlersRejectWrongMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.Handler
		path    string
		method  string
	}{
		{name: "monitoringInstance runtime", handler: handlers.MonitoringInstanceRuntimeControls(&fakeMonitoringInstanceRuntimeControlRepository{}), path: "/api/monitoring-instances/mi_001/runtime/pause", method: http.MethodGet},
		{name: "target runtime", handler: handlers.TargetRuntimeControls(&fakeTargetRuntimeControlRepository{}), path: "/api/targets/tg_001/runtime/archive", method: http.MethodGet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
			}
			assertAdminError(t, recorder, "method not allowed")
		})
	}
}
