package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"houfeng/internal/center/nodes"
)

func NodeOnboarding(repo nodes.OnboardingRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		nodeID, ok := nodeActionNodeID(r.URL.Path, "/onboarding")
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		state, err := repo.GetNodeOnboarding(r.Context(), nodeID)
		if errors.Is(err, nodes.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, state)
	})
}

func NodeEnrollmentToken(repo nodes.OnboardingRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		nodeID, ok := nodeActionNodeID(r.URL.Path, "/enrollment-token")
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		issue, err := repo.IssueNodeEnrollmentToken(r.Context(), nodeID)
		if errors.Is(err, nodes.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
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

func NodeInstallCommand(repo nodes.OnboardingRepository, opts InstallCommandOptions) http.Handler {
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

		nodeID, ok := nodeActionNodeID(r.URL.Path, "/install-command")
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		issue, err := repo.IssueNodeEnrollmentToken(r.Context(), nodeID)
		if errors.Is(err, nodes.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		installerURL := publicBaseURL + "/api/agent/install.sh"
		command := fmt.Sprintf(
			"curl -fsSL %s | sudo sh -s -- --server-url %s --enrollment-token %s --version %s --release-repo %s",
			shellQuote(installerURL),
			shellQuote(publicBaseURL),
			shellQuote(issue.Token),
			shellQuote(agentVersion),
			shellQuote(releaseRepo),
		)

		writeJSON(w, http.StatusOK, nodes.InstallCommandIssue{
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

func NodeBindingConfirmRebind(repo nodes.OnboardingRepository) http.Handler {
	return nodeBindingAction(repo, "/binding/confirm-rebind", repo.ConfirmNodeRebind)
}

func NodeBindingRejectPending(repo nodes.OnboardingRepository) http.Handler {
	return nodeBindingAction(repo, "/binding/reject-pending", repo.RejectPendingFingerprint)
}

func NodeBindingReset(repo nodes.OnboardingRepository) http.Handler {
	return nodeBindingAction(repo, "/binding/reset", repo.ResetNodeBinding)
}

func nodeBindingAction(repo nodes.OnboardingRepository, suffix string, action func(context.Context, string) (nodes.Record, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		nodeID, ok := nodeActionNodeID(r.URL.Path, suffix)
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		_, err := action(r.Context(), nodeID)
		switch {
		case errors.Is(err, nodes.ErrNodeNotFound):
			writeError(w, http.StatusNotFound, "node not found")
			return
		case errors.Is(err, nodes.ErrInvalidBindingTransition):
			writeError(w, http.StatusConflict, "invalid binding transition")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		state, err := repo.GetNodeOnboarding(r.Context(), nodeID)
		if errors.Is(err, nodes.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, state)
	})
}

func nodeActionNodeID(path, suffix string) (string, bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/nodes/"), "/")
	if trimmed == "" || !strings.HasSuffix(trimmed, suffix) {
		return "", false
	}

	nodeID := strings.Trim(strings.TrimSuffix(trimmed, suffix), "/")
	if nodeID == "" || strings.Contains(nodeID, "/") {
		return "", false
	}
	return nodeID, true
}
