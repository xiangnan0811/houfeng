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
		targetID := strings.TrimPrefix(r.URL.Path, "/api/targets/")
		targetID = strings.Trim(targetID, "/")
		if targetID == "" || strings.Contains(targetID, "/") {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
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
		case http.MethodPatch:
			group, labels, note, ok, err := decodeUpdateMetadataRequest(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			input := targets.UpdateMetadataInput{Group: group, Labels: labels, Note: note}
			expectedUpdatedAt, ok := parseMetadataPrecondition(r.Header.Get("If-Match"))
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			input.ExpectedUpdatedAt = expectedUpdatedAt
			input = normalizeTargetMetadataInput(input)
			if !isValidTargetMetadataInput(input) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.UpdateTargetMetadata(r.Context(), targetID, input)
			if errors.Is(err, targets.ErrTargetNotFound) {
				writeError(w, http.StatusNotFound, "target not found")
				return
			}
			if errors.Is(err, targets.ErrTargetMetadataConflict) {
				writeError(w, http.StatusConflict, "metadata conflict")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}

			writeJSON(w, http.StatusOK, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func TargetProbeItems(repo targets.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetID, probeItemID, isCollection := targetProbePath(r.URL.Path)
		if targetID == "" {
			writeError(w, http.StatusNotFound, "target not found")
			return
		}
		if !isCollection && probeItemID == "" {
			writeError(w, http.StatusNotFound, "probe item not found")
			return
		}
		if !isCollection {
			switch r.Method {
			case http.MethodPut:
				var input targets.UpdateProbeItemInput
				if err := decodeJSON(r, &input); err != nil {
					writeError(w, http.StatusBadRequest, "invalid json")
					return
				}

				var err error
				input, err = targets.ValidateUpdateProbeItemInput(input)
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid input")
					return
				}

				record, err := repo.UpdateProbeItem(r.Context(), targetID, probeItemID, input)
				if errors.Is(err, targets.ErrTargetNotFound) {
					writeError(w, http.StatusNotFound, "target not found")
					return
				}
				if errors.Is(err, targets.ErrProbeItemNotFound) {
					writeError(w, http.StatusNotFound, "probe item not found")
					return
				}
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				writeJSON(w, http.StatusOK, record)
			case http.MethodDelete:
				err := repo.DeleteProbeItem(r.Context(), targetID, probeItemID)
				if errors.Is(err, targets.ErrTargetNotFound) {
					writeError(w, http.StatusNotFound, "target not found")
					return
				}
				if errors.Is(err, targets.ErrProbeItemNotFound) {
					writeError(w, http.StatusNotFound, "probe item not found")
					return
				}
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				w.WriteHeader(http.StatusNoContent)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
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

			var err error
			input, err = targets.ValidateCreateProbeItemInput(input)
			if err != nil {
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
	input.Group = strings.TrimSpace(input.Group)
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func isValidCreateTargetInput(input targets.CreateTargetInput) bool {
	if input.Name == "" || input.TargetType == "" || input.Host == "" || input.RunStatus == "" {
		return false
	}
	if len(input.ExecutionMonitoringInstanceLabels) == 0 {
		return false
	}
	if !targets.IsValidTargetType(input.TargetType) || !targets.IsValidRunStatus(input.RunStatus) {
		return false
	}
	return true
}

func normalizeTargetMetadataInput(input targets.UpdateMetadataInput) targets.UpdateMetadataInput {
	if input.Group != nil {
		v := strings.TrimSpace(*input.Group)
		input.Group = &v
	}
	input.Labels, input.Note = normalizeMetadata(input.Labels, input.Note)
	return input
}

func isValidTargetMetadataInput(input targets.UpdateMetadataInput) bool {
	return isValidMetadata(input.Labels, input.Note)
}

func targetProbePath(path string) (targetID string, probeItemID string, isCollection bool) {
	segments := strings.Split(strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] != "probe-items" {
		return "", "", false
	}
	if len(segments) == 2 {
		return segments[0], "", true
	}
	if len(segments) == 3 && segments[2] != "" {
		return segments[0], segments[2], false
	}
	return segments[0], "", false
}
