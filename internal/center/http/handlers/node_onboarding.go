package handlers

import (
	"context"
	"errors"
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
