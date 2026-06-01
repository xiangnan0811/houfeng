package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/monitoringinstances"
)

func MonitoringInstancesCollection(repo monitoringinstances.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListMonitoringInstances(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input monitoringinstances.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = normalizeCreateInput(input)
			if !isValidCreateInput(input) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateMonitoringInstance(r.Context(), input)
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

func MonitoringInstanceItem(repo monitoringinstances.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitoringInstanceID := strings.TrimPrefix(r.URL.Path, "/api/monitoring-instances/")
		monitoringInstanceID = strings.Trim(monitoringInstanceID, "/")
		if monitoringInstanceID == "" || strings.Contains(monitoringInstanceID, "/") {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetMonitoringInstance(r.Context(), monitoringInstanceID)
			if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
				writeError(w, http.StatusNotFound, "monitoring instance not found")
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

			input := monitoringinstances.UpdateMetadataInput{Group: group, Labels: labels, Note: note}
			expectedUpdatedAt, ok := parseMetadataPrecondition(r.Header.Get("If-Match"))
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			input.ExpectedUpdatedAt = expectedUpdatedAt
			input = normalizeUpdateMetadataInput(input)
			if !isValidUpdateMetadataInput(input) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.UpdateMonitoringInstanceMetadata(r.Context(), monitoringInstanceID, input)
			if errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
				writeError(w, http.StatusNotFound, "monitoring instance not found")
				return
			}
			if errors.Is(err, monitoringinstances.ErrMonitoringInstanceMetadataConflict) {
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

func normalizeCreateInput(input monitoringinstances.CreateInput) monitoringinstances.CreateInput {
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Group = strings.TrimSpace(input.Group)
	input.Region = strings.TrimSpace(input.Region)
	input.City = strings.TrimSpace(input.City)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Note = strings.TrimSpace(input.Note)
	input.LifecycleStatus = monitoringinstances.LifecyclePendingEnrollment
	return input
}

func isValidCreateInput(input monitoringinstances.CreateInput) bool {
	if input.DisplayName == "" || input.Region == "" || input.City == "" || input.Provider == "" || input.LifecycleStatus == "" {
		return false
	}
	return monitoringinstances.IsValidLifecycleStatus(input.LifecycleStatus)
}

func normalizeUpdateMetadataInput(input monitoringinstances.UpdateMetadataInput) monitoringinstances.UpdateMetadataInput {
	if input.Group != nil {
		v := strings.TrimSpace(*input.Group)
		input.Group = &v
	}
	input.Labels, input.Note = normalizeMetadata(input.Labels, input.Note)
	return input
}

func isValidUpdateMetadataInput(input monitoringinstances.UpdateMetadataInput) bool {
	return isValidMetadata(input.Labels, input.Note)
}
