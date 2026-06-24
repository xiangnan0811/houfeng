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

type fakeMonitoringInstanceOnboardingRepository struct {
	getMonitoringInstanceOnboardingResult               monitoringinstances.OnboardingState
	getMonitoringInstanceOnboardingErr                  error
	getMonitoringInstanceOnboardingMonitoringInstanceID string
	issueEnrollmentTokenResult                          monitoringinstances.EnrollmentTokenIssue
	issueEnrollmentTokenErr                             error
	issueEnrollmentTokenMonitoringInstanceID            string
	confirmMonitoringInstanceRebindResult               monitoringinstances.Record
	confirmMonitoringInstanceRebindErr                  error
	confirmMonitoringInstanceRebindMonitoringInstanceID string
	rejectPendingFingerprintResult                      monitoringinstances.Record
	rejectPendingFingerprintErr                         error
	rejectPendingFingerprintMonitoringInstanceID        string
	resetMonitoringInstanceBindingResult                monitoringinstances.Record
	resetMonitoringInstanceBindingErr                   error
	resetMonitoringInstanceBindingMonitoringInstanceID  string
}

func (f *fakeMonitoringInstanceOnboardingRepository) IssueMonitoringInstanceEnrollmentToken(_ context.Context, monitoringInstanceID string) (monitoringinstances.EnrollmentTokenIssue, error) {
	f.issueEnrollmentTokenMonitoringInstanceID = monitoringInstanceID
	if f.issueEnrollmentTokenErr != nil {
		return monitoringinstances.EnrollmentTokenIssue{}, f.issueEnrollmentTokenErr
	}
	return f.issueEnrollmentTokenResult, nil
}

func (f *fakeMonitoringInstanceOnboardingRepository) GetMonitoringInstanceOnboarding(_ context.Context, monitoringInstanceID string) (monitoringinstances.OnboardingState, error) {
	f.getMonitoringInstanceOnboardingMonitoringInstanceID = monitoringInstanceID
	if f.getMonitoringInstanceOnboardingErr != nil {
		return monitoringinstances.OnboardingState{}, f.getMonitoringInstanceOnboardingErr
	}
	return f.getMonitoringInstanceOnboardingResult, nil
}

func (f *fakeMonitoringInstanceOnboardingRepository) ConfirmMonitoringInstanceRebind(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.confirmMonitoringInstanceRebindMonitoringInstanceID = monitoringInstanceID
	if f.confirmMonitoringInstanceRebindErr != nil {
		return monitoringinstances.Record{}, f.confirmMonitoringInstanceRebindErr
	}
	return f.confirmMonitoringInstanceRebindResult, nil
}

func (f *fakeMonitoringInstanceOnboardingRepository) RejectPendingFingerprint(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.rejectPendingFingerprintMonitoringInstanceID = monitoringInstanceID
	if f.rejectPendingFingerprintErr != nil {
		return monitoringinstances.Record{}, f.rejectPendingFingerprintErr
	}
	return f.rejectPendingFingerprintResult, nil
}

func (f *fakeMonitoringInstanceOnboardingRepository) ResetMonitoringInstanceBinding(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.resetMonitoringInstanceBindingMonitoringInstanceID = monitoringInstanceID
	if f.resetMonitoringInstanceBindingErr != nil {
		return monitoringinstances.Record{}, f.resetMonitoringInstanceBindingErr
	}
	return f.resetMonitoringInstanceBindingResult, nil
}

func TestMonitoringInstanceOnboardingHandlerReturnsState(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 9, 0, 0, 0, time.UTC)
	repo := &fakeMonitoringInstanceOnboardingRepository{
		getMonitoringInstanceOnboardingResult: monitoringinstances.OnboardingState{
			Record: monitoringinstances.Record{
				MonitoringInstanceID:    "mi_001",
				DisplayName:             "Tokyo Edge",
				BindingStatus:           monitoringinstances.BindingPendingConfirmation,
				CurrentHealthStatus:     monitoringinstances.HealthNormal,
				EnrollmentTokenIssuedAt: &issuedAt,
			},
			Phase:                            monitoringinstances.OnboardingPhaseBindingConflict,
			HasHostSample:                    true,
			HasAcceptedObservation:           false,
			EnrollmentTokenIssuedAt:          &issuedAt,
			CurrentBindingFingerprintSummary: "sha256:c…abcdef",
			PendingBinding: &monitoringinstances.PendingBindingMetadata{
				Fingerprint:  "fp-pending",
				AttemptCount: 3,
			},
		},
	}

	handler := handlers.MonitoringInstanceOnboarding(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/onboarding", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.getMonitoringInstanceOnboardingMonitoringInstanceID != "mi_001" {
		t.Fatalf("GetMonitoringInstanceOnboarding monitoringInstanceID = %q, want %q", repo.getMonitoringInstanceOnboardingMonitoringInstanceID, "mi_001")
	}

	var body monitoringinstances.OnboardingState
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.MonitoringInstanceID != "mi_001" {
		t.Fatalf("MonitoringInstanceID = %q, want %q", body.MonitoringInstanceID, "mi_001")
	}
	if body.Phase != monitoringinstances.OnboardingPhaseBindingConflict {
		t.Fatalf("Phase = %q, want %q", body.Phase, monitoringinstances.OnboardingPhaseBindingConflict)
	}
	if body.PendingBinding == nil || body.PendingBinding.Fingerprint != "fp-pending" {
		t.Fatalf("PendingBinding = %#v, want fingerprint %q", body.PendingBinding, "fp-pending")
	}
	if body.CurrentBindingFingerprintSummary != "sha256:c…abcdef" {
		t.Fatalf("CurrentBindingFingerprintSummary = %q, want %q", body.CurrentBindingFingerprintSummary, "sha256:c…abcdef")
	}
}

func TestMonitoringInstanceOnboardingHandlerReturnsNotFound(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{getMonitoringInstanceOnboardingErr: monitoringinstances.ErrMonitoringInstanceNotFound}

	handler := handlers.MonitoringInstanceOnboarding(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_missing/onboarding", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	assertAdminError(t, recorder, "monitoring instance not found")
}

func TestMonitoringInstanceEnrollmentTokenHandlerReturnsPlaintextToken(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 9, 10, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(monitoringinstances.EnrollmentTokenTTL)
	repo := &fakeMonitoringInstanceOnboardingRepository{
		issueEnrollmentTokenResult: monitoringinstances.EnrollmentTokenIssue{
			Token:     "enroll_001",
			IssuedAt:  issuedAt,
			ExpiresAt: expiresAt,
		},
	}

	handler := handlers.MonitoringInstanceEnrollmentToken(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/enrollment-token", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.issueEnrollmentTokenMonitoringInstanceID != "mi_001" {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken monitoringInstanceID = %q, want %q", repo.issueEnrollmentTokenMonitoringInstanceID, "mi_001")
	}

	var body monitoringinstances.EnrollmentTokenIssue
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Token != "enroll_001" {
		t.Fatalf("Token = %q, want %q", body.Token, "enroll_001")
	}
	if !body.IssuedAt.Equal(issuedAt) {
		t.Fatalf("IssuedAt = %s, want %s", body.IssuedAt.Format(time.RFC3339), issuedAt.Format(time.RFC3339))
	}
	if !body.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %s, want %s", body.ExpiresAt.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	}
}

func TestMonitoringInstanceInstallCommandHandlerReturnsCommand(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.May, 15, 8, 30, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(monitoringinstances.EnrollmentTokenTTL)
	repo := &fakeMonitoringInstanceOnboardingRepository{
		issueEnrollmentTokenResult: monitoringinstances.EnrollmentTokenIssue{
			Token:     "enroll_secret_001",
			IssuedAt:  issuedAt,
			ExpiresAt: expiresAt,
		},
	}

	handler := handlers.MonitoringInstanceInstallCommand(repo, handlers.InstallCommandOptions{
		PublicBaseURL: "https://center.example.com/",
		AgentVersion:  "v1.2.3",
		ReleaseRepo:   "owner/repo",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/install-command", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.issueEnrollmentTokenMonitoringInstanceID != "mi_001" {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken monitoringInstanceID = %q, want %q", repo.issueEnrollmentTokenMonitoringInstanceID, "mi_001")
	}

	var body monitoringinstances.InstallCommandIssue
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.InstallerURL != "https://center.example.com/api/agent/install.sh" {
		t.Fatalf("InstallerURL = %q, want center-served installer URL", body.InstallerURL)
	}
	if body.PublicBaseURL != "https://center.example.com" {
		t.Fatalf("PublicBaseURL = %q, want trimmed base URL", body.PublicBaseURL)
	}
	if body.AgentVersion != "v1.2.3" || body.ReleaseRepo != "owner/repo" {
		t.Fatalf("release metadata = version %q repo %q, want v1.2.3 owner/repo", body.AgentVersion, body.ReleaseRepo)
	}
	if !body.IssuedAt.Equal(issuedAt) || !body.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("issue timestamps = %s/%s, want %s/%s", body.IssuedAt.Format(time.RFC3339), body.ExpiresAt.Format(time.RFC3339), issuedAt.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	}
	for _, want := range []string{
		`curl -fsSL 'https://center.example.com/api/agent/install.sh'`,
		`--server-url 'https://center.example.com'`,
		`--enrollment-token-stdin`,
		"<<'HOUFENG_ENROLLMENT_TOKEN'",
		"\nenroll_secret_001\nHOUFENG_ENROLLMENT_TOKEN\n",
		`--version 'v1.2.3'`,
		`--release-repo 'owner/repo'`,
	} {
		if !strings.Contains(body.Command, want) {
			t.Fatalf("Command = %q, missing %q", body.Command, want)
		}
	}
	if strings.Contains(body.Command, "--enrollment-token 'enroll_secret_001'") {
		t.Fatalf("Command = %q, should not expose enrollment token as an installer argv value", body.Command)
	}
	if strings.Contains(body.Command, "printf %s 'enroll_secret_001'") {
		t.Fatalf("Command = %q, should not expose enrollment token as a printf argv value", body.Command)
	}
}

func TestMonitoringInstanceInstallCommandHandlerAddsExplicitInsecureFlagForHTTPBaseURL(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{
		issueEnrollmentTokenResult: monitoringinstances.EnrollmentTokenIssue{
			Token:     "enroll_secret_001",
			IssuedAt:  time.Date(2026, time.May, 15, 8, 30, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, time.May, 15, 9, 0, 0, 0, time.UTC),
		},
	}
	handler := handlers.MonitoringInstanceInstallCommand(repo, handlers.InstallCommandOptions{
		PublicBaseURL: "http://127.0.0.1:8080/",
		AgentVersion:  "v1.2.3",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/install-command", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body monitoringinstances.InstallCommandIssue
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.PublicBaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("PublicBaseURL = %q, want trimmed HTTP base URL", body.PublicBaseURL)
	}
	if !strings.Contains(body.Command, `--insecure-allow-http`) {
		t.Fatalf("Command = %q, want explicit insecure HTTP flag", body.Command)
	}
}

func TestMonitoringInstanceInstallCommandHandlerShellQuotesArguments(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{
		issueEnrollmentTokenResult: monitoringinstances.EnrollmentTokenIssue{
			Token:     "enroll_secret_001",
			IssuedAt:  time.Date(2026, time.May, 15, 8, 30, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, time.May, 15, 9, 0, 0, 0, time.UTC),
		},
	}
	handler := handlers.MonitoringInstanceInstallCommand(repo, handlers.InstallCommandOptions{
		PublicBaseURL: "https://center.example.com",
		AgentVersion:  "v1.2.3",
		ReleaseRepo:   "owner/o'repo",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/install-command", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body monitoringinstances.InstallCommandIssue
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if !strings.Contains(body.Command, `--release-repo 'owner/o'\''repo'`) {
		t.Fatalf("Command = %q, want shell-quoted release repo", body.Command)
	}
}

func TestMonitoringInstanceInstallCommandHandlerRequiresConfiguredPublicBaseURL(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{}
	handler := handlers.MonitoringInstanceInstallCommand(repo, handlers.InstallCommandOptions{AgentVersion: "v1.2.3"})
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/install-command", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	assertAdminError(t, recorder, "public base URL is not configured")
	if repo.issueEnrollmentTokenMonitoringInstanceID != "" {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken called for %q, want not called", repo.issueEnrollmentTokenMonitoringInstanceID)
	}
}

func TestMonitoringInstanceInstallCommandHandlerRequiresReleaseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
	}{
		{name: "missing", version: ""},
		{name: "dev placeholder", version: "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeMonitoringInstanceOnboardingRepository{}
			handler := handlers.MonitoringInstanceInstallCommand(repo, handlers.InstallCommandOptions{PublicBaseURL: "https://center.example.com", AgentVersion: tt.version})
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/install-command", nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
			}
			assertAdminError(t, recorder, "agent release version is not configured")
			if repo.issueEnrollmentTokenMonitoringInstanceID != "" {
				t.Fatalf("IssueMonitoringInstanceEnrollmentToken called for %q, want not called", repo.issueEnrollmentTokenMonitoringInstanceID)
			}
		})
	}
}

func TestMonitoringInstanceInstallCommandHandlerMapsRepositoryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		repoErr    error
		wantStatus int
		wantError  string
	}{
		{name: "missing monitoring instance", repoErr: monitoringinstances.ErrMonitoringInstanceNotFound, wantStatus: http.StatusNotFound, wantError: "monitoring instance not found"},
		{name: "archived monitoring instance", repoErr: monitoringinstances.ErrArchivedMonitoringInstance, wantStatus: http.StatusConflict, wantError: "archived monitoring instance"},
		{name: "repository failure", repoErr: errors.New("db boom"), wantStatus: http.StatusInternalServerError, wantError: "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakeMonitoringInstanceOnboardingRepository{issueEnrollmentTokenErr: tt.repoErr}
			handler := handlers.MonitoringInstanceInstallCommand(repo, handlers.InstallCommandOptions{
				PublicBaseURL: "https://center.example.com",
				AgentVersion:  "v1.2.3",
			})
			req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_001/install-command", nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			assertAdminError(t, recorder, tt.wantError)
		})
	}
}

func TestMonitoringInstanceBindingConfirmRebindHandlerReturnsUpdatedState(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{
		confirmMonitoringInstanceRebindResult: monitoringinstances.Record{MonitoringInstanceID: "mi_002"},
		getMonitoringInstanceOnboardingResult: monitoringinstances.OnboardingState{
			Record: monitoringinstances.Record{
				MonitoringInstanceID: "mi_002",
				BindingStatus:        monitoringinstances.BindingBound,
			},
			Phase: monitoringinstances.OnboardingPhaseBoundAwaitingObservation,
		},
	}

	handler := handlers.MonitoringInstanceBindingConfirmRebind(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_002/binding/confirm-rebind", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.confirmMonitoringInstanceRebindMonitoringInstanceID != "mi_002" {
		t.Fatalf("ConfirmMonitoringInstanceRebind monitoringInstanceID = %q, want %q", repo.confirmMonitoringInstanceRebindMonitoringInstanceID, "mi_002")
	}
	if repo.getMonitoringInstanceOnboardingMonitoringInstanceID != "mi_002" {
		t.Fatalf("GetMonitoringInstanceOnboarding monitoringInstanceID = %q, want %q", repo.getMonitoringInstanceOnboardingMonitoringInstanceID, "mi_002")
	}

	var body monitoringinstances.OnboardingState
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Phase != monitoringinstances.OnboardingPhaseBoundAwaitingObservation {
		t.Fatalf("Phase = %q, want %q", body.Phase, monitoringinstances.OnboardingPhaseBoundAwaitingObservation)
	}
}

func TestMonitoringInstanceBindingConfirmRebindHandlerReturnsConflictForInvalidTransition(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{
		confirmMonitoringInstanceRebindErr: errors.Join(monitoringinstances.ErrInvalidBindingTransition, errors.New("confirm rebind requires pending fingerprint")),
	}

	handler := handlers.MonitoringInstanceBindingConfirmRebind(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_002/binding/confirm-rebind", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	assertAdminError(t, recorder, "invalid binding transition")
}

func TestMonitoringInstanceBindingConfirmRebindHandlerReturnsConflictForArchivedInstance(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{
		confirmMonitoringInstanceRebindErr: monitoringinstances.ErrArchivedMonitoringInstance,
	}

	handler := handlers.MonitoringInstanceBindingConfirmRebind(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_archived/binding/confirm-rebind", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	assertAdminError(t, recorder, "archived monitoring instance")
}

func TestMonitoringInstanceOnboardingConfirmRebindReturnsNotFoundForMissingMonitoringInstance(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{confirmMonitoringInstanceRebindErr: monitoringinstances.ErrMonitoringInstanceNotFound}

	handler := handlers.MonitoringInstanceBindingConfirmRebind(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_missing/binding/confirm-rebind", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	assertAdminError(t, recorder, "monitoring instance not found")
}

func TestMonitoringInstanceBindingRejectPendingHandlerReturnsUpdatedState(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{
		rejectPendingFingerprintResult: monitoringinstances.Record{MonitoringInstanceID: "mi_003"},
		getMonitoringInstanceOnboardingResult: monitoringinstances.OnboardingState{
			Record: monitoringinstances.Record{
				MonitoringInstanceID: "mi_003",
				BindingStatus:        monitoringinstances.BindingBound,
			},
			Phase: monitoringinstances.OnboardingPhaseBoundAwaitingObservation,
		},
	}

	handler := handlers.MonitoringInstanceBindingRejectPending(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_003/binding/reject-pending", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.rejectPendingFingerprintMonitoringInstanceID != "mi_003" {
		t.Fatalf("RejectPendingFingerprint monitoringInstanceID = %q, want %q", repo.rejectPendingFingerprintMonitoringInstanceID, "mi_003")
	}
}

func TestMonitoringInstanceOnboardingRejectPendingReturnsNotFoundForMissingMonitoringInstance(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{rejectPendingFingerprintErr: monitoringinstances.ErrMonitoringInstanceNotFound}

	handler := handlers.MonitoringInstanceBindingRejectPending(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_missing/binding/reject-pending", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	assertAdminError(t, recorder, "monitoring instance not found")
}

func TestMonitoringInstanceBindingResetHandlerReturnsUpdatedState(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceOnboardingRepository{
		resetMonitoringInstanceBindingResult: monitoringinstances.Record{MonitoringInstanceID: "mi_004"},
		getMonitoringInstanceOnboardingResult: monitoringinstances.OnboardingState{
			Record: monitoringinstances.Record{
				MonitoringInstanceID: "mi_004",
				BindingStatus:        monitoringinstances.BindingUnbound,
			},
			Phase: monitoringinstances.OnboardingPhaseNotStarted,
		},
	}

	handler := handlers.MonitoringInstanceBindingReset(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/mi_004/binding/reset", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.resetMonitoringInstanceBindingMonitoringInstanceID != "mi_004" {
		t.Fatalf("ResetMonitoringInstanceBinding monitoringInstanceID = %q, want %q", repo.resetMonitoringInstanceBindingMonitoringInstanceID, "mi_004")
	}

	var body monitoringinstances.OnboardingState
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Phase != monitoringinstances.OnboardingPhaseNotStarted {
		t.Fatalf("Phase = %q, want %q", body.Phase, monitoringinstances.OnboardingPhaseNotStarted)
	}
}

func TestMonitoringInstanceOnboardingAdminHandlersRejectWrongMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.Handler
		path    string
		method  string
	}{
		{name: "onboarding read", handler: handlers.MonitoringInstanceOnboarding(&fakeMonitoringInstanceOnboardingRepository{}), path: "/api/monitoring-instances/mi_001/onboarding", method: http.MethodPost},
		{name: "issue token", handler: handlers.MonitoringInstanceEnrollmentToken(&fakeMonitoringInstanceOnboardingRepository{}), path: "/api/monitoring-instances/mi_001/enrollment-token", method: http.MethodGet},
		{name: "install command", handler: handlers.MonitoringInstanceInstallCommand(&fakeMonitoringInstanceOnboardingRepository{}, handlers.InstallCommandOptions{PublicBaseURL: "https://center.example.com", AgentVersion: "v1.2.3"}), path: "/api/monitoring-instances/mi_001/install-command", method: http.MethodGet},
		{name: "confirm rebind", handler: handlers.MonitoringInstanceBindingConfirmRebind(&fakeMonitoringInstanceOnboardingRepository{}), path: "/api/monitoring-instances/mi_001/binding/confirm-rebind", method: http.MethodGet},
		{name: "reject pending", handler: handlers.MonitoringInstanceBindingRejectPending(&fakeMonitoringInstanceOnboardingRepository{}), path: "/api/monitoring-instances/mi_001/binding/reject-pending", method: http.MethodGet},
		{name: "reset binding", handler: handlers.MonitoringInstanceBindingReset(&fakeMonitoringInstanceOnboardingRepository{}), path: "/api/monitoring-instances/mi_001/binding/reset", method: http.MethodGet},
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
