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
	"houfeng/internal/center/nodes"
	"houfeng/internal/center/store"
	"houfeng/internal/center/targets"
)

type fakeNodeRuntimeControlRepository struct {
	setMaintenanceNodeID string
	setMaintenanceResult nodes.Record
	setMaintenanceErr    error

	pauseNodeID string
	pauseResult nodes.Record
	pauseErr    error

	resumeNodeID string
	resumeResult nodes.Record
	resumeErr    error

	retireNodeID  string
	retireResult  nodes.Record
	retireErr     error
	restoreNodeID string
	restoreResult nodes.Record
	restoreErr    error
}

func (f *fakeNodeRuntimeControlRepository) SetNodeMonitoringMaintenance(_ context.Context, nodeID string) (nodes.Record, error) {
	f.setMaintenanceNodeID = nodeID
	if f.setMaintenanceErr != nil {
		return nodes.Record{}, f.setMaintenanceErr
	}
	return f.setMaintenanceResult, nil
}

func (f *fakeNodeRuntimeControlRepository) PauseNodeMonitoring(_ context.Context, nodeID string) (nodes.Record, error) {
	f.pauseNodeID = nodeID
	if f.pauseErr != nil {
		return nodes.Record{}, f.pauseErr
	}
	return f.pauseResult, nil
}

func (f *fakeNodeRuntimeControlRepository) ResumeNodeMonitoring(_ context.Context, nodeID string) (nodes.Record, error) {
	f.resumeNodeID = nodeID
	if f.resumeErr != nil {
		return nodes.Record{}, f.resumeErr
	}
	return f.resumeResult, nil
}

func (f *fakeNodeRuntimeControlRepository) RetireNode(_ context.Context, nodeID string) (nodes.Record, error) {
	f.retireNodeID = nodeID
	if f.retireErr != nil {
		return nodes.Record{}, f.retireErr
	}
	return f.retireResult, nil
}

func (f *fakeNodeRuntimeControlRepository) RestoreRetiredNodeToObserving(_ context.Context, nodeID string) (nodes.Record, error) {
	f.restoreNodeID = nodeID
	if f.restoreErr != nil {
		return nodes.Record{}, f.restoreErr
	}
	return f.restoreResult, nil
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

func TestNodeRuntimeControlHandlerReturnsUpdatedNode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 26, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		path           string
		wantNodeID     string
		wantStatus     string
		buildRepo      func() *fakeNodeRuntimeControlRepository
		assertCalledID func(*testing.T, *fakeNodeRuntimeControlRepository, string)
	}{
		{
			name:       "enter maintenance",
			path:       "/api/nodes/nd_001/runtime/enter-maintenance",
			wantNodeID: "nd_001",
			wantStatus: "维护中",
			buildRepo: func() *fakeNodeRuntimeControlRepository {
				return &fakeNodeRuntimeControlRepository{setMaintenanceResult: nodes.Record{NodeID: "nd_001", MonitoringStatus: "维护中", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeNodeRuntimeControlRepository, want string) {
				t.Helper()
				if repo.setMaintenanceNodeID != want {
					t.Fatalf("SetNodeMonitoringMaintenance nodeID = %q, want %q", repo.setMaintenanceNodeID, want)
				}
			},
		},
		{
			name:       "exit maintenance",
			path:       "/api/nodes/nd_002/runtime/exit-maintenance",
			wantNodeID: "nd_002",
			wantStatus: "启用",
			buildRepo: func() *fakeNodeRuntimeControlRepository {
				return &fakeNodeRuntimeControlRepository{resumeResult: nodes.Record{NodeID: "nd_002", MonitoringStatus: "启用", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeNodeRuntimeControlRepository, want string) {
				t.Helper()
				if repo.resumeNodeID != want {
					t.Fatalf("ResumeNodeMonitoring nodeID = %q, want %q", repo.resumeNodeID, want)
				}
			},
		},
		{
			name:       "pause",
			path:       "/api/nodes/nd_003/runtime/pause",
			wantNodeID: "nd_003",
			wantStatus: "暂停",
			buildRepo: func() *fakeNodeRuntimeControlRepository {
				return &fakeNodeRuntimeControlRepository{pauseResult: nodes.Record{NodeID: "nd_003", MonitoringStatus: "暂停", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeNodeRuntimeControlRepository, want string) {
				t.Helper()
				if repo.pauseNodeID != want {
					t.Fatalf("PauseNodeMonitoring nodeID = %q, want %q", repo.pauseNodeID, want)
				}
			},
		},
		{
			name:       "resume",
			path:       "/api/nodes/nd_004/runtime/resume",
			wantNodeID: "nd_004",
			wantStatus: "启用",
			buildRepo: func() *fakeNodeRuntimeControlRepository {
				return &fakeNodeRuntimeControlRepository{resumeResult: nodes.Record{NodeID: "nd_004", MonitoringStatus: "启用", UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeNodeRuntimeControlRepository, want string) {
				t.Helper()
				if repo.resumeNodeID != want {
					t.Fatalf("ResumeNodeMonitoring nodeID = %q, want %q", repo.resumeNodeID, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.buildRepo()
			handler := handlers.NodeRuntimeControls(repo)
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			tt.assertCalledID(t, repo, tt.wantNodeID)

			var body nodes.Record
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if body.NodeID != tt.wantNodeID {
				t.Fatalf("NodeID = %q, want %q", body.NodeID, tt.wantNodeID)
			}
			if body.MonitoringStatus != tt.wantStatus {
				t.Fatalf("MonitoringStatus = %q, want %q", body.MonitoringStatus, tt.wantStatus)
			}
		})
	}
}

func TestNodeRuntimeControlHandlerMapsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		repo        *fakeNodeRuntimeControlRepository
		path        string
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "invalid transition",
			repo:        &fakeNodeRuntimeControlRepository{setMaintenanceErr: errors.Join(store.ErrInvalidNodeRuntimeTransition, errors.New("cannot enter maintenance"))},
			path:        "/api/nodes/nd_001/runtime/enter-maintenance",
			wantStatus:  http.StatusConflict,
			wantMessage: "invalid runtime transition",
		},
		{
			name:        "not found",
			repo:        &fakeNodeRuntimeControlRepository{pauseErr: nodes.ErrNodeNotFound},
			path:        "/api/nodes/nd_missing/runtime/pause",
			wantStatus:  http.StatusNotFound,
			wantMessage: "node not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := handlers.NodeRuntimeControls(tt.repo)
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

func TestNodeLifecycleControlHandlerReturnsUpdatedNode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 27, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		path           string
		wantNodeID     string
		wantLifecycle  string
		buildRepo      func() *fakeNodeRuntimeControlRepository
		assertCalledID func(*testing.T, *fakeNodeRuntimeControlRepository, string)
	}{
		{
			name:          "retire",
			path:          "/api/nodes/nd_001/lifecycle/retire",
			wantNodeID:    "nd_001",
			wantLifecycle: nodes.LifecycleRetired,
			buildRepo: func() *fakeNodeRuntimeControlRepository {
				return &fakeNodeRuntimeControlRepository{retireResult: nodes.Record{NodeID: "nd_001", LifecycleStatus: nodes.LifecycleRetired, UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeNodeRuntimeControlRepository, want string) {
				t.Helper()
				if repo.retireNodeID != want {
					t.Fatalf("RetireNode nodeID = %q, want %q", repo.retireNodeID, want)
				}
			},
		},
		{
			name:          "restore retired to observing",
			path:          "/api/nodes/nd_002/lifecycle/restore-to-observing",
			wantNodeID:    "nd_002",
			wantLifecycle: nodes.LifecycleObserving,
			buildRepo: func() *fakeNodeRuntimeControlRepository {
				return &fakeNodeRuntimeControlRepository{restoreResult: nodes.Record{NodeID: "nd_002", LifecycleStatus: nodes.LifecycleObserving, UpdatedAt: now}}
			},
			assertCalledID: func(t *testing.T, repo *fakeNodeRuntimeControlRepository, want string) {
				t.Helper()
				if repo.restoreNodeID != want {
					t.Fatalf("RestoreRetiredNodeToObserving nodeID = %q, want %q", repo.restoreNodeID, want)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.buildRepo()
			handler := handlers.NodeLifecycleControls(repo)
			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}
			tt.assertCalledID(t, repo, tt.wantNodeID)

			var body nodes.Record
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response body: %v", err)
			}
			if body.NodeID != tt.wantNodeID {
				t.Fatalf("NodeID = %q, want %q", body.NodeID, tt.wantNodeID)
			}
			if body.LifecycleStatus != tt.wantLifecycle {
				t.Fatalf("LifecycleStatus = %q, want %q", body.LifecycleStatus, tt.wantLifecycle)
			}
		})
	}
}

func TestNodeLifecycleControlHandlerMapsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		repo        *fakeNodeRuntimeControlRepository
		path        string
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "invalid transition",
			repo:        &fakeNodeRuntimeControlRepository{retireErr: errors.Join(store.ErrInvalidNodeLifecycleTransition, errors.New("cannot retire"))},
			path:        "/api/nodes/nd_001/lifecycle/retire",
			wantStatus:  http.StatusConflict,
			wantMessage: "invalid lifecycle transition",
		},
		{
			name:        "not found",
			repo:        &fakeNodeRuntimeControlRepository{restoreErr: nodes.ErrNodeNotFound},
			path:        "/api/nodes/nd_missing/lifecycle/restore-to-observing",
			wantStatus:  http.StatusNotFound,
			wantMessage: "node not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := handlers.NodeLifecycleControls(tt.repo)
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
		{name: "node runtime", handler: handlers.NodeRuntimeControls(&fakeNodeRuntimeControlRepository{}), path: "/api/nodes/nd_001/runtime/pause", method: http.MethodGet},
		{name: "node lifecycle", handler: handlers.NodeLifecycleControls(&fakeNodeRuntimeControlRepository{}), path: "/api/nodes/nd_001/lifecycle/retire", method: http.MethodGet},
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
