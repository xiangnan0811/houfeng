package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/targets"
)

func TargetsCollection(repo targets.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListTargets(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input targets.CreateTargetInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = normalizeCreateTargetInput(input)
			if !isValidCreateTargetInput(input) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateTarget(r.Context(), input)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func TargetItem(repo targets.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		targetID := strings.TrimPrefix(r.URL.Path, "/api/targets/")
		targetID = strings.Trim(targetID, "/")
		if targetID == "" || strings.Contains(targetID, "/") {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		record, err := repo.GetTarget(r.Context(), targetID)
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

func TargetProbeItems(repo targets.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetID := targetIDFromProbePath(r.URL.Path)
		if targetID == "" {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListProbeItems(r.Context(), targetID)
			if errors.Is(err, targets.ErrTargetNotFound) {
				writeError(w, http.StatusNotFound, "target not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input targets.CreateProbeItemInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = normalizeCreateProbeItemInput(input)
			if !isValidCreateProbeItemInput(input) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateProbeItem(r.Context(), targetID, input)
			if errors.Is(err, targets.ErrTargetNotFound) {
				writeError(w, http.StatusNotFound, "target not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func normalizeCreateTargetInput(input targets.CreateTargetInput) targets.CreateTargetInput {
	input.Name = strings.TrimSpace(input.Name)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.Host = strings.TrimSpace(input.Host)
	input.RunStatus = strings.TrimSpace(input.RunStatus)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func isValidCreateTargetInput(input targets.CreateTargetInput) bool {
	if input.Name == "" || input.TargetType == "" || input.Host == "" || input.RunStatus == "" {
		return false
	}
	if !targets.IsValidTargetType(input.TargetType) || !targets.IsValidRunStatus(input.RunStatus) {
		return false
	}
	return true
}

func normalizeCreateProbeItemInput(input targets.CreateProbeItemInput) targets.CreateProbeItemInput {
	input.ProbeKind = strings.TrimSpace(input.ProbeKind)
	input.FrequencyTier = strings.TrimSpace(input.FrequencyTier)
	return input
}

func isValidCreateProbeItemInput(input targets.CreateProbeItemInput) bool {
	if input.ProbeKind == "" || input.FrequencyTier == "" || input.TimeoutSeconds <= 0 {
		return false
	}
	return len(input.Config) > 0
}

func targetIDFromProbePath(path string) string {
	suffix := strings.TrimPrefix(path, "/api/targets/")
	suffix = strings.Trim(suffix, "/")
	if suffix == "" || !strings.HasSuffix(suffix, "/probe-items") {
		return ""
	}

	targetID := strings.TrimSuffix(suffix, "/probe-items")
	targetID = strings.Trim(targetID, "/")
	if targetID == "" || strings.Contains(targetID, "/") {
		return ""
	}
	return targetID
}
