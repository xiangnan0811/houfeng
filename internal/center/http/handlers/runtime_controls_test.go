package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func (f *fakeMonitoringInstanceRuntimeControlRepository) PauseMonitoringInstanceMonitoring(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
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
	setMaintenanceTargetID string
	setMaintenanceResult   targets.TargetRecord
	setMaintenanceErr      error

	pauseTargetID string
	pauseResult   targets.TargetRecord
	pauseErr      error

	resumeTargetID string
	resumeResult   targets.TargetRecord
	resumeErr      error

	archiveTargetID string
	archiveResult   targets.TargetRecord
	archiveErr      error

	restoreTargetID string
	restoreResult   targets.TargetRecord
	restoreErr      error
}

func (f *fakeTargetRuntimeControlRepository) SetTargetMaintenance(_ context.Context, targetID string) (targets.TargetRecord, error) {
	f.setMaintenanceTargetID = targetID
	if f.setMaintenanceErr != nil {
		return targets.TargetRecord{}, f.setMaintenanceErr
	}
	return f.setMaintenanceResult, nil
}

func (f *fakeTargetRuntimeControlRepository) PauseTargetRun(_ context.Context, targetID string) (targets.TargetRecord, error) {
	f.pauseTargetID = targetID
	if f.pauseErr != nil {
		return targets.TargetRecord{}, f.pauseErr
	}
	return f.pauseResult, nil
}

func (f *fakeTargetRuntimeControlRepository) ResumeTargetRun(_ context.Context, targetID string) (targets.TargetRecord, error) {
	f.resumeTargetID = targetID
	if f.resumeErr != nil {
		return targets.TargetRecord{}, f.resumeErr
	}
	return f.resumeResult, nil
}

func (f *fakeTargetRuntimeControlRepository) ArchiveTarget(_ context.Context, targetID string) (targets.TargetRecord, error) {
	f.archiveTargetID = targetID
	if f.archiveErr != nil {
		return targets.TargetRecord{}, f.archiveErr
	}
	return f.archiveResult, nil
}

func (f *fakeTargetRuntimeControlRepository) RestoreArchivedTargetToPaused(_ context.Context, targetID string) (targets.TargetRecord, error) {
	f.restoreTargetID = targetID
	if f.restoreErr != nil {
		return targets.TargetRecord{}, f.restoreErr
	}
	return f.restoreResult, nil
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
		})
	}
}

func TestTargetRuntimeControlHandlerReturnsUpdatedTarget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 26, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		path             string
		wantTargetID     string
		wantStatus       string
		buildRepo        func() *fakeTargetRuntimeControlRepository
		assertCalledWith func(*testing.T, *fakeTargetRuntimeControlRepository, string)
	}{
		{
			name:         "enter maintenance",
			path:         "/api/targets/tg_001/runtime/enter-maintenance",
			wantTargetID: "tg_001",
			wantStatus:   "维护中",
			buildRepo: func() *fakeTargetRuntimeControlRepository {
				return &fakeTargetRuntimeControlRepository{setMaintenanceResult: targets.TargetRecord{TargetID: "tg_001", RunStatus: "维护中", UpdatedAt: now}}
			},
			assertCalledWith: func(t *testing.T, repo *fakeTargetRuntimeControlRepository, want string) {
				t.Helper()
				if repo.setMaintenanceTargetID != want {
					t.Fatalf("SetTargetMaintenance targetID = %q, want %q", repo.setMaintenanceTargetID, want)
				}
			},
		},
		{
			name:         "exit maintenance",
			path:         "/api/targets/tg_002/runtime/exit-maintenance",
			wantTargetID: "tg_002",
			wantStatus:   "启用",
			buildRepo: func() *fakeTargetRuntimeControlRepository {
				return &fakeTargetRuntimeControlRepository{resumeResult: targets.TargetRecord{TargetID: "tg_002", RunStatus: "启用", UpdatedAt: now}}
			},
			assertCalledWith: func(t *testing.T, repo *fakeTargetRuntimeControlRepository, want string) {
				t.Helper()
				if repo.resumeTargetID != want {
					t.Fatalf("ResumeTargetRun targetID = %q, want %q", repo.resumeTargetID, want)
				}
			},
		},
		{
			name:         "pause",
			path:         "/api/targets/tg_003/runtime/pause",
			wantTargetID: "tg_003",
			wantStatus:   "暂停",
			buildRepo: func() *fakeTargetRuntimeControlRepository {
				return &fakeTargetRuntimeControlRepository{pauseResult: targets.TargetRecord{TargetID: "tg_003", RunStatus: "暂停", UpdatedAt: now}}
			},
			assertCalledWith: func(t *testing.T, repo *fakeTargetRuntimeControlRepository, want string) {
				t.Helper()
				if repo.pauseTargetID != want {
					t.Fatalf("PauseTargetRun targetID = %q, want %q", repo.pauseTargetID, want)
				}
			},
		},
		{
			name:         "resume",
			path:         "/api/targets/tg_004/runtime/resume",
			wantTargetID: "tg_004",
			wantStatus:   "启用",
			buildRepo: func() *fakeTargetRuntimeControlRepository {
				return &fakeTargetRuntimeControlRepository{resumeResult: targets.TargetRecord{TargetID: "tg_004", RunStatus: "启用", UpdatedAt: now}}
			},
			assertCalledWith: func(t *testing.T, repo *fakeTargetRuntimeControlRepository, want string) {
				t.Helper()
				if repo.resumeTargetID != want {
					t.Fatalf("ResumeTargetRun targetID = %q, want %q", repo.resumeTargetID, want)
				}
			},
		},
		{
			name:         "archive",
			path:         "/api/targets/tg_005/runtime/archive",
			wantTargetID: "tg_005",
			wantStatus:   "已归档",
			buildRepo: func() *fakeTargetRuntimeControlRepository {
				return &fakeTargetRuntimeControlRepository{archiveResult: targets.TargetRecord{TargetID: "tg_005", RunStatus: "已归档", UpdatedAt: now}}
			},
			assertCalledWith: func(t *testing.T, repo *fakeTargetRuntimeControlRepository, want string) {
				t.Helper()
				if repo.archiveTargetID != want {
					t.Fatalf("ArchiveTarget targetID = %q, want %q", repo.archiveTargetID, want)
				}
			},
		},
		{
			name:         "restore to paused",
			path:         "/api/targets/tg_006/runtime/restore-to-paused",
			wantTargetID: "tg_006",
			wantStatus:   "暂停",
			buildRepo: func() *fakeTargetRuntimeControlRepository {
				return &fakeTargetRuntimeControlRepository{restoreResult: targets.TargetRecord{TargetID: "tg_006", RunStatus: "暂停", UpdatedAt: now}}
			},
			assertCalledWith: func(t *testing.T, repo *fakeTargetRuntimeControlRepository, want string) {
				t.Helper()
				if repo.restoreTargetID != want {
					t.Fatalf("RestoreArchivedTargetToPaused targetID = %q, want %q", repo.restoreTargetID, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.buildRepo()
			handler := handlers.TargetRuntimeControls(repo)
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			tt.assertCalledWith(t, repo, tt.wantTargetID)

			var body targets.TargetRecord
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if body.TargetID != tt.wantTargetID {
				t.Fatalf("TargetID = %q, want %q", body.TargetID, tt.wantTargetID)
			}
			if body.RunStatus != tt.wantStatus {
				t.Fatalf("RunStatus = %q, want %q", body.RunStatus, tt.wantStatus)
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
			repo:        &fakeTargetRuntimeControlRepository{archiveErr: errors.Join(store.ErrInvalidTargetRuntimeTransition, errors.New("cannot archive"))},
			path:        "/api/targets/tg_001/runtime/archive",
			wantStatus:  http.StatusConflict,
			wantMessage: "invalid runtime transition",
		},
		{
			name:        "not found",
			repo:        &fakeTargetRuntimeControlRepository{restoreErr: targets.ErrTargetNotFound},
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
