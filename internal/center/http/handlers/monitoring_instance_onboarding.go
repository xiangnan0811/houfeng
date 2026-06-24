package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"houfeng/internal/center/monitoringinstances"
)

func MonitoringInstanceOnboarding(repo monitoringinstances.OnboardingRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceIDFromActionPath(r.URL.Path, "/onboarding")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		state, err := repo.GetMonitoringInstanceOnboarding(r.Context(), monitoringInstanceID)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, state)
	})
}

func MonitoringInstanceEnrollmentToken(repo monitoringinstances.OnboardingRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceIDFromActionPath(r.URL.Path, "/enrollment-token")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		issue, err := repo.IssueMonitoringInstanceEnrollmentToken(r.Context(), monitoringInstanceID)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
			writeError(w, http.StatusConflict, "archived monitoring instance")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, issue)
	})
}

type InstallCommandOptions struct {
	PublicBaseURL string
	AgentVersion  string
	ReleaseRepo   string
}

const defaultAgentReleaseRepo = "xiangnan0811/houfeng"

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func MonitoringInstanceInstallCommand(repo monitoringinstances.OnboardingRepository, opts InstallCommandOptions) http.Handler {
	publicBaseURL := strings.TrimRight(strings.TrimSpace(opts.PublicBaseURL), "/")
	agentVersion := strings.TrimSpace(opts.AgentVersion)
	releaseRepo := strings.TrimSpace(opts.ReleaseRepo)
	if releaseRepo == "" {
		releaseRepo = defaultAgentReleaseRepo
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if publicBaseURL == "" {
			writeError(w, http.StatusConflict, "public base URL is not configured")
			return
		}
		if agentVersion == "" || agentVersion == "dev" {
			writeError(w, http.StatusConflict, "agent release version is not configured")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceIDFromActionPath(r.URL.Path, "/install-command")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		issue, err := repo.IssueMonitoringInstanceEnrollmentToken(r.Context(), monitoringInstanceID)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
			writeError(w, http.StatusConflict, "archived monitoring instance")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		installerURL := publicBaseURL + "/api/agent/install.sh"
		insecureHTTPFlag := ""
		if strings.HasPrefix(publicBaseURL, "http://") {
			insecureHTTPFlag = " --insecure-allow-http"
		}
		command := fmt.Sprintf(
			"tmp_installer=\"$(mktemp)\" && curl -fsSL %s -o \"$tmp_installer\" && sudo sh \"$tmp_installer\" --server-url %s --enrollment-token-stdin --version %s --release-repo %s%s <<'HOUFENG_ENROLLMENT_TOKEN'\n%s\nHOUFENG_ENROLLMENT_TOKEN\nstatus=$?; rm -f \"$tmp_installer\"; test \"$status\" -eq 0",
			shellQuote(installerURL),
			shellQuote(publicBaseURL),
			shellQuote(agentVersion),
			shellQuote(releaseRepo),
			insecureHTTPFlag,
			issue.Token,
		)

		writeJSON(w, http.StatusOK, monitoringinstances.InstallCommandIssue{
			Command:       command,
			IssuedAt:      issue.IssuedAt,
			ExpiresAt:     issue.ExpiresAt,
			InstallerURL:  installerURL,
			PublicBaseURL: publicBaseURL,
			AgentVersion:  agentVersion,
			ReleaseRepo:   releaseRepo,
		})
	})
}

func MonitoringInstanceBindingConfirmRebind(repo monitoringinstances.OnboardingRepository) http.Handler {
	return monitoringInstanceBindingAction(repo, "/binding/confirm-rebind", repo.ConfirmMonitoringInstanceRebind)
}

func MonitoringInstanceBindingRejectPending(repo monitoringinstances.OnboardingRepository) http.Handler {
	return monitoringInstanceBindingAction(repo, "/binding/reject-pending", repo.RejectPendingFingerprint)
}

func MonitoringInstanceBindingReset(repo monitoringinstances.OnboardingRepository) http.Handler {
	return monitoringInstanceBindingAction(repo, "/binding/reset", repo.ResetMonitoringInstanceBinding)
}

func monitoringInstanceBindingAction(repo monitoringinstances.OnboardingRepository, suffix string, action func(context.Context, string) (monitoringinstances.Record, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		monitoringInstanceID, ok := monitoringInstanceIDFromActionPath(r.URL.Path, suffix)
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		_, err := action(r.Context(), monitoringInstanceID)
		switch {
		case errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound):
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		case errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance):
			writeError(w, http.StatusConflict, "archived monitoring instance")
			return
		case errors.Is(err, monitoringinstances.ErrInvalidBindingTransition):
			writeError(w, http.StatusConflict, "invalid binding transition")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		state, err := repo.GetMonitoringInstanceOnboarding(r.Context(), monitoringInstanceID)
		if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, state)
	})
}

func monitoringInstanceIDFromActionPath(path, suffix string) (string, bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/")
	if trimmed == "" || !strings.HasSuffix(trimmed, suffix) {
		return "", false
	}

	monitoringInstanceID := strings.Trim(strings.TrimSuffix(trimmed, suffix), "/")
	if monitoringInstanceID == "" || strings.Contains(monitoringInstanceID, "/") {
		return "", false
	}
	return monitoringInstanceID, true
}
