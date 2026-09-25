package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/http/handlers"
)

func TestVPSStateRepairArchiveConflictReturnsObjectReview(t *testing.T) {
	review := assetlifecycle.ArchiveReview{
		Blockers: []string{"service svc_unknown status requires confirmation", "effective target target_active is still running"},
		BlockerDetails: []assetlifecycle.BlockerDetail{
			{Code: "dependency_needs_confirmation", ObjectType: "service", ObjectID: "svc_unknown", DisplayName: "Unknown service", CurrentState: "unknown", BlockedAction: "archive", ResolutionAction: "correct_dependency_status"},
			{Code: "target_running", ObjectType: "target", ObjectID: "target_active", DisplayName: "Active target", CurrentState: "启用", BlockedAction: "archive", ResolutionAction: "pause_or_archive"},
		},
	}
	repository := &fakeAssetLifecycleRepository{archiveErr: &assetlifecycle.ArchiveBlockedError{Review: review}}
	request := httptest.NewRequest(http.MethodPost, "/api/vps/vps_review/archive", strings.NewReader(`{"confirmation_name":"VPS review","reason":"archive review"}`))
	response := httptest.NewRecorder()

	handlers.VPSArchive(repository).ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("archive status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
	var payload struct {
		Code   string                       `json:"code"`
		Review assetlifecycle.ArchiveReview `json:"review"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode archive conflict response: %v", err)
	}
	if payload.Code != "lifecycle_action_blocked" {
		t.Fatalf("archive conflict code = %q, want lifecycle_action_blocked", payload.Code)
	}
	if len(payload.Review.BlockerDetails) != 2 || payload.Review.BlockerDetails[0].ObjectID != "svc_unknown" || payload.Review.BlockerDetails[1].ObjectID != "target_active" {
		t.Fatalf("archive conflict review details = %#v, want the unknown dependency and effective target blockers", payload.Review.BlockerDetails)
	}
}

func TestVPSStateRepairArchiveAuditFailureReturnsInternalServerError(t *testing.T) {
	repository := &fakeAssetLifecycleRepository{archiveErr: errors.New("archive completed audit and failed audit persistence both failed")}
	request := httptest.NewRequest(http.MethodPost, "/api/vps/vps_audit_failure/archive", strings.NewReader(`{"confirmation_name":"VPS audit failure","reason":"preserve audit failure"}`))
	response := httptest.NewRecorder()

	handlers.VPSArchive(repository).ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("archive audit-failure status = %d, want %d; body=%s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
}
