package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

func NodeRuntimeFacts(repo runtimefacts.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		nodeID, ok := nodeRuntimeFactsPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		record, err := repo.GetNodeRuntimeFacts(r.Context(), nodeID)
		if errors.Is(err, nodes.ErrNodeNotFound) {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func TargetRuntimeFacts(repo runtimefacts.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		targetID, ok := targetRuntimeFactsPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		record, err := repo.GetTargetRuntimeFacts(r.Context(), targetID)
		if errors.Is(err, targets.ErrTargetNotFound) {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		writeJSON(w, http.StatusOK, record)
	})
}

func nodeRuntimeFactsPath(path string) (nodeID string, ok bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/nodes/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "runtime-facts" {
		return "", false
	}
	return segments[0], true
}

func targetRuntimeFactsPath(path string) (targetID string, ok bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/"), "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != "runtime-facts" {
		return "", false
	}
	return segments[0], true
}
