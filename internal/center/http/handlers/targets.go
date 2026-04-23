package handlers

import (
	"encoding/json"
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
		targetID, isCollection := targetProbePath(r.URL.Path)
		if targetID == "" {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		if !isCollection {
			writeError(w, http.StatusNotFound, "probe item not found")
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
	if !targets.IsValidProbeKind(input.ProbeKind) || !targets.IsValidFrequencyTier(input.FrequencyTier) || input.TimeoutSeconds <= 0 {
		return false
	}
	return hasValidProbeConfig(input.ProbeKind, input.Config)
}

func hasValidProbeConfig(probeKind string, raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return false
	}
	if config == nil {
		return false
	}

	switch probeKind {
	case targets.ProbeKindTCP, targets.ProbeKindTLS:
		return hasRequiredConfigFields(config, "port")
	case targets.ProbeKindHTTP:
		return hasRequiredConfigFields(config, "scheme", "path", "method")
	default:
		return false
	}
}

func hasRequiredConfigFields(config map[string]json.RawMessage, fields ...string) bool {
	for _, field := range fields {
		value, ok := config[field]
		if !ok || len(value) == 0 || string(value) == "null" {
			return false
		}
	}
	return true
}

func targetProbePath(path string) (targetID string, isCollection bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] != "probe-items" {
		return "", false
	}
	if len(segments) == 2 {
		return segments[0], true
	}
	return segments[0], false
}
