package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/store"
	"houfeng/internal/center/targets"
)

type targetRuntimeControlErrorRepository struct {
	err error
}

func (f *targetRuntimeControlErrorRepository) SetTargetMaintenance(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *targetRuntimeControlErrorRepository) PauseTargetRun(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *targetRuntimeControlErrorRepository) ResumeTargetRun(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *targetRuntimeControlErrorRepository) ArchiveTarget(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func (f *targetRuntimeControlErrorRepository) RestoreArchivedTargetToPaused(_ context.Context, _ string, _ ...assetlinks.GlobalActionConfirmation) (targets.TargetRecord, error) {
	return targets.TargetRecord{}, f.err
}

func TestTargetRuntimeControlsAcceptsEmptyBody(t *testing.T) {
	handler := handlers.TargetRuntimeControls(&targetRuntimeControlErrorRepository{})
	req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_002/runtime/pause", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestTargetRuntimeControlsMapsActionErrors(t *testing.T) {
	for _, tt := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "missing target", err: targets.ErrTargetNotFound, wantStatus: http.StatusNotFound},
		{name: "state conflict", err: store.ErrInvalidTargetRuntimeTransition, wantStatus: http.StatusConflict},
		{name: "shared impact confirmation", err: assetlifecycle.ErrSharedImpactConfirmationRequired, wantStatus: http.StatusConflict, wantCode: "shared_impact_confirmation_required"},
		{name: "stale management review", err: assetlifecycle.ErrStaleCancellationPreview, wantStatus: http.StatusConflict, wantCode: "management_review_stale"},
		{name: "database failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &targetRuntimeControlErrorRepository{err: tt.err}
			handler := handlers.TargetRuntimeControls(repo)
			req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/runtime/pause", nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if tt.wantCode != "" {
				var body map[string]string
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if body["code"] != tt.wantCode {
					t.Fatalf("error code = %q, want %q", body["code"], tt.wantCode)
				}
			}
		})
	}
}

func TestTargetRuntimeControlsRejectsInvalidActionAndMalformedConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		body string
	}{
		{name: "unknown action", path: "/api/targets/tg_001/runtime/activate"},
		{name: "malformed confirmation", path: "/api/targets/tg_001/runtime/archive", body: `{"confirm_shared_impact":true`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &targetRuntimeControlErrorRepository{}
			handler := handlers.TargetRuntimeControls(repo)
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}
