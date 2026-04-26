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
)

type fakeNodeOnboardingRepository struct {
	getNodeOnboardingResult        nodes.OnboardingState
	getNodeOnboardingErr           error
	getNodeOnboardingNodeID        string
	issueEnrollmentTokenResult     nodes.EnrollmentTokenIssue
	issueEnrollmentTokenErr        error
	issueEnrollmentTokenNodeID     string
	confirmNodeRebindResult        nodes.Record
	confirmNodeRebindErr           error
	confirmNodeRebindNodeID        string
	rejectPendingFingerprintResult nodes.Record
	rejectPendingFingerprintErr    error
	rejectPendingFingerprintNodeID string
	resetNodeBindingResult         nodes.Record
	resetNodeBindingErr            error
	resetNodeBindingNodeID         string
}

func (f *fakeNodeOnboardingRepository) IssueNodeEnrollmentToken(_ context.Context, nodeID string) (nodes.EnrollmentTokenIssue, error) {
	f.issueEnrollmentTokenNodeID = nodeID
	if f.issueEnrollmentTokenErr != nil {
		return nodes.EnrollmentTokenIssue{}, f.issueEnrollmentTokenErr
	}
	return f.issueEnrollmentTokenResult, nil
}

func (f *fakeNodeOnboardingRepository) GetNodeOnboarding(_ context.Context, nodeID string) (nodes.OnboardingState, error) {
	f.getNodeOnboardingNodeID = nodeID
	if f.getNodeOnboardingErr != nil {
		return nodes.OnboardingState{}, f.getNodeOnboardingErr
	}
	return f.getNodeOnboardingResult, nil
}

func (f *fakeNodeOnboardingRepository) ConfirmNodeRebind(_ context.Context, nodeID string) (nodes.Record, error) {
	f.confirmNodeRebindNodeID = nodeID
	if f.confirmNodeRebindErr != nil {
		return nodes.Record{}, f.confirmNodeRebindErr
	}
	return f.confirmNodeRebindResult, nil
}

func (f *fakeNodeOnboardingRepository) RejectPendingFingerprint(_ context.Context, nodeID string) (nodes.Record, error) {
	f.rejectPendingFingerprintNodeID = nodeID
	if f.rejectPendingFingerprintErr != nil {
		return nodes.Record{}, f.rejectPendingFingerprintErr
	}
	return f.rejectPendingFingerprintResult, nil
}

func (f *fakeNodeOnboardingRepository) ResetNodeBinding(_ context.Context, nodeID string) (nodes.Record, error) {
	f.resetNodeBindingNodeID = nodeID
	if f.resetNodeBindingErr != nil {
		return nodes.Record{}, f.resetNodeBindingErr
	}
	return f.resetNodeBindingResult, nil
}

func TestNodeOnboardingHandlerReturnsState(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeOnboardingRepository{
		getNodeOnboardingResult: nodes.OnboardingState{
			Record: nodes.Record{
				NodeID:                  "nd_001",
				DisplayName:             "Tokyo Edge",
				BindingStatus:           nodes.BindingPendingConfirmation,
				CurrentHealthStatus:     nodes.HealthNormal,
				EnrollmentTokenIssuedAt: &issuedAt,
			},
			Phase:                   nodes.OnboardingPhaseBindingConflict,
			HasHostSample:           true,
			HasAcceptedObservation:  false,
			EnrollmentTokenIssuedAt: &issuedAt,
			PendingBinding: &nodes.PendingBindingMetadata{
				Fingerprint:  "fp-pending",
				AttemptCount: 3,
			},
		},
	}

	handler := handlers.NodeOnboarding(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_001/onboarding", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.getNodeOnboardingNodeID != "nd_001" {
		t.Fatalf("GetNodeOnboarding nodeID = %q, want %q", repo.getNodeOnboardingNodeID, "nd_001")
	}

	var body nodes.OnboardingState
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.NodeID != "nd_001" {
		t.Fatalf("NodeID = %q, want %q", body.NodeID, "nd_001")
	}
	if body.Phase != nodes.OnboardingPhaseBindingConflict {
		t.Fatalf("Phase = %q, want %q", body.Phase, nodes.OnboardingPhaseBindingConflict)
	}
	if body.PendingBinding == nil || body.PendingBinding.Fingerprint != "fp-pending" {
		t.Fatalf("PendingBinding = %#v, want fingerprint %q", body.PendingBinding, "fp-pending")
	}
}

func TestNodeOnboardingHandlerReturnsNotFound(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeOnboardingRepository{getNodeOnboardingErr: nodes.ErrNodeNotFound}

	handler := handlers.NodeOnboarding(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_missing/onboarding", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	assertAdminError(t, recorder, "node not found")
}

func TestNodeEnrollmentTokenHandlerReturnsPlaintextToken(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 9, 10, 0, 0, time.UTC)
	repo := &fakeNodeOnboardingRepository{
		issueEnrollmentTokenResult: nodes.EnrollmentTokenIssue{
			Token:    "enroll_001",
			IssuedAt: issuedAt,
		},
	}

	handler := handlers.NodeEnrollmentToken(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_001/enrollment-token", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.issueEnrollmentTokenNodeID != "nd_001" {
		t.Fatalf("IssueNodeEnrollmentToken nodeID = %q, want %q", repo.issueEnrollmentTokenNodeID, "nd_001")
	}

	var body nodes.EnrollmentTokenIssue
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Token != "enroll_001" {
		t.Fatalf("Token = %q, want %q", body.Token, "enroll_001")
	}
	if !body.IssuedAt.Equal(issuedAt) {
		t.Fatalf("IssuedAt = %s, want %s", body.IssuedAt.Format(time.RFC3339), issuedAt.Format(time.RFC3339))
	}
}

func TestNodeBindingConfirmRebindHandlerReturnsUpdatedState(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeOnboardingRepository{
		confirmNodeRebindResult: nodes.Record{NodeID: "nd_002"},
		getNodeOnboardingResult: nodes.OnboardingState{
			Record: nodes.Record{
				NodeID:        "nd_002",
				BindingStatus: nodes.BindingBound,
			},
			Phase: nodes.OnboardingPhaseBoundAwaitingObservation,
		},
	}

	handler := handlers.NodeBindingConfirmRebind(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_002/binding/confirm-rebind", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.confirmNodeRebindNodeID != "nd_002" {
		t.Fatalf("ConfirmNodeRebind nodeID = %q, want %q", repo.confirmNodeRebindNodeID, "nd_002")
	}
	if repo.getNodeOnboardingNodeID != "nd_002" {
		t.Fatalf("GetNodeOnboarding nodeID = %q, want %q", repo.getNodeOnboardingNodeID, "nd_002")
	}

	var body nodes.OnboardingState
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Phase != nodes.OnboardingPhaseBoundAwaitingObservation {
		t.Fatalf("Phase = %q, want %q", body.Phase, nodes.OnboardingPhaseBoundAwaitingObservation)
	}
}

func TestNodeBindingConfirmRebindHandlerReturnsConflictForInvalidTransition(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeOnboardingRepository{
		confirmNodeRebindErr: errors.Join(nodes.ErrInvalidBindingTransition, errors.New("confirm rebind requires pending fingerprint")),
	}

	handler := handlers.NodeBindingConfirmRebind(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_002/binding/confirm-rebind", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	assertAdminError(t, recorder, "invalid binding transition")
}

func TestNodeOnboardingConfirmRebindReturnsNotFoundForMissingNode(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeOnboardingRepository{confirmNodeRebindErr: nodes.ErrNodeNotFound}

	handler := handlers.NodeBindingConfirmRebind(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_missing/binding/confirm-rebind", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	assertAdminError(t, recorder, "node not found")
}

func TestNodeBindingRejectPendingHandlerReturnsUpdatedState(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeOnboardingRepository{
		rejectPendingFingerprintResult: nodes.Record{NodeID: "nd_003"},
		getNodeOnboardingResult: nodes.OnboardingState{
			Record: nodes.Record{
				NodeID:        "nd_003",
				BindingStatus: nodes.BindingBound,
			},
			Phase: nodes.OnboardingPhaseBoundAwaitingObservation,
		},
	}

	handler := handlers.NodeBindingRejectPending(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_003/binding/reject-pending", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.rejectPendingFingerprintNodeID != "nd_003" {
		t.Fatalf("RejectPendingFingerprint nodeID = %q, want %q", repo.rejectPendingFingerprintNodeID, "nd_003")
	}
}

func TestNodeOnboardingRejectPendingReturnsNotFoundForMissingNode(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeOnboardingRepository{rejectPendingFingerprintErr: nodes.ErrNodeNotFound}

	handler := handlers.NodeBindingRejectPending(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_missing/binding/reject-pending", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	assertAdminError(t, recorder, "node not found")
}

func TestNodeBindingResetHandlerReturnsUpdatedState(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeOnboardingRepository{
		resetNodeBindingResult: nodes.Record{NodeID: "nd_004"},
		getNodeOnboardingResult: nodes.OnboardingState{
			Record: nodes.Record{
				NodeID:        "nd_004",
				BindingStatus: nodes.BindingUnbound,
			},
			Phase: nodes.OnboardingPhaseNotStarted,
		},
	}

	handler := handlers.NodeBindingReset(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/nd_004/binding/reset", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.resetNodeBindingNodeID != "nd_004" {
		t.Fatalf("ResetNodeBinding nodeID = %q, want %q", repo.resetNodeBindingNodeID, "nd_004")
	}

	var body nodes.OnboardingState
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Phase != nodes.OnboardingPhaseNotStarted {
		t.Fatalf("Phase = %q, want %q", body.Phase, nodes.OnboardingPhaseNotStarted)
	}
}

func TestNodeOnboardingAdminHandlersRejectWrongMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.Handler
		path    string
		method  string
	}{
		{name: "onboarding read", handler: handlers.NodeOnboarding(&fakeNodeOnboardingRepository{}), path: "/api/nodes/nd_001/onboarding", method: http.MethodPost},
		{name: "issue token", handler: handlers.NodeEnrollmentToken(&fakeNodeOnboardingRepository{}), path: "/api/nodes/nd_001/enrollment-token", method: http.MethodGet},
		{name: "confirm rebind", handler: handlers.NodeBindingConfirmRebind(&fakeNodeOnboardingRepository{}), path: "/api/nodes/nd_001/binding/confirm-rebind", method: http.MethodGet},
		{name: "reject pending", handler: handlers.NodeBindingRejectPending(&fakeNodeOnboardingRepository{}), path: "/api/nodes/nd_001/binding/reject-pending", method: http.MethodGet},
		{name: "reset binding", handler: handlers.NodeBindingReset(&fakeNodeOnboardingRepository{}), path: "/api/nodes/nd_001/binding/reset", method: http.MethodGet},
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

func assertAdminError(t *testing.T, recorder *httptest.ResponseRecorder, wantMessage string) {
	t.Helper()

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}

	if body["error"] != wantMessage {
		t.Fatalf("error = %q, want %q", body["error"], wantMessage)
	}
}
