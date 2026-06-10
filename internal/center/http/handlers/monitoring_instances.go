package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/monitoringinstances"
)

type MonitoringInstanceManagementRepository interface {
	GetMonitoringInstanceManagementReview(context.Context, string) (monitoringinstances.ManagementReview, error)
	RetireMonitoringInstance(context.Context, string, monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error)
	RestoreMonitoringInstanceLifecycle(context.Context, string, monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error)
	ArchiveMonitoringInstance(context.Context, string, monitoringinstances.ArchiveInput) (monitoringinstances.Record, error)
	RestoreMonitoringInstanceFromArchive(context.Context, string) (monitoringinstances.Record, error)
	PermanentCleanupMonitoringInstance(context.Context, string, monitoringinstances.PermanentCleanupInput) (monitoringinstances.PermanentCleanupResult, error)
}

func MonitoringInstancesCollection(repo monitoringinstances.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			scope, ok := monitoringinstances.NormalizeListScope(monitoringinstances.ListScope(r.URL.Query().Get("scope")))
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			records, err := repo.ListMonitoringInstances(r.Context(), scope)
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
			if errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
				writeError(w, http.StatusConflict, "archived monitoring instance")
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

func MonitoringInstanceManagementReview(repo MonitoringInstanceManagementRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitoringInstanceID, ok := parseMonitoringInstanceSubresourcePath(r.URL.Path, "management-review")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		review, err := repo.GetMonitoringInstanceManagementReview(r.Context(), monitoringInstanceID)
		if handled := writeMonitoringInstanceManagementError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, review)
	})
}

func MonitoringInstanceLifecycleRetire(repo MonitoringInstanceManagementRepository) http.Handler {
	return monitoringInstanceLifecycleActionHandler("lifecycle", "retire", func(ctx context.Context, monitoringInstanceID string, input monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error) {
		return repo.RetireMonitoringInstance(ctx, monitoringInstanceID, input)
	})
}

func MonitoringInstanceLifecycleRestore(repo MonitoringInstanceManagementRepository) http.Handler {
	return monitoringInstanceLifecycleActionHandler("lifecycle", "restore", func(ctx context.Context, monitoringInstanceID string, input monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error) {
		return repo.RestoreMonitoringInstanceLifecycle(ctx, monitoringInstanceID, input)
	})
}

func MonitoringInstanceArchive(repo MonitoringInstanceManagementRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitoringInstanceID, ok := parseMonitoringInstanceSubresourcePath(r.URL.Path, "archive")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input monitoringinstances.ArchiveInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConfirmationName) == "" {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		record, err := repo.ArchiveMonitoringInstance(r.Context(), monitoringInstanceID, input)
		if handled := writeMonitoringInstanceManagementError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func MonitoringInstanceRestoreFromArchive(repo MonitoringInstanceManagementRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitoringInstanceID, ok := parseMonitoringInstanceSubresourcePath(r.URL.Path, "restore-from-archive")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		record, err := repo.RestoreMonitoringInstanceFromArchive(r.Context(), monitoringInstanceID)
		if handled := writeMonitoringInstanceManagementError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func MonitoringInstancePermanentCleanup(repo MonitoringInstanceManagementRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitoringInstanceID, ok := parseMonitoringInstanceSubresourcePath(r.URL.Path, "permanent-cleanup")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input monitoringinstances.PermanentCleanupInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.ConfirmationName) == "" {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		result, err := repo.PermanentCleanupMonitoringInstance(r.Context(), monitoringInstanceID, input)
		if handled := writeMonitoringInstanceManagementError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func monitoringInstanceLifecycleActionHandler(parent, action string, apply func(context.Context, string, monitoringinstances.LifecycleActionInput) (monitoringinstances.Record, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitoringInstanceID, ok := parseMonitoringInstanceNestedSubresourcePath(r.URL.Path, parent, action)
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input monitoringinstances.LifecycleActionInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if strings.TrimSpace(input.Reason) == "" {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		record, err := apply(r.Context(), monitoringInstanceID, input)
		if handled := writeMonitoringInstanceManagementError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func parseMonitoringInstanceNestedSubresourcePath(path, parent, action string) (string, bool) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/")
	segments := strings.Split(relative, "/")
	if len(segments) != 3 || segments[0] == "" || segments[1] != parent || segments[2] != action {
		return "", false
	}
	return segments[0], true
}

func writeMonitoringInstanceManagementError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, monitoringinstances.ErrInvalidManagementInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, monitoringinstances.ErrManagementActionBlocked), errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance):
		writeError(w, http.StatusConflict, "management action blocked")
	case errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound):
		writeError(w, http.StatusNotFound, "monitoring instance not found")
	default:
		return false
	}
	return true
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
