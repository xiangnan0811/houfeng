package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/nodes"
)

func NodesCollection(repo nodes.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListNodes(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input nodes.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = normalizeCreateInput(input)
			if !isValidCreateInput(input) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateNode(r.Context(), input)
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

func NodeItem(repo nodes.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeID := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
		nodeID = strings.Trim(nodeID, "/")
		if nodeID == "" || strings.Contains(nodeID, "/") {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetNode(r.Context(), nodeID)
			if errors.Is(err, nodes.ErrNodeNotFound) {
				writeError(w, http.StatusNotFound, "node not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}

			writeJSON(w, http.StatusOK, record)
		case http.MethodPatch:
			var input nodes.UpdateMetadataInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = normalizeUpdateMetadataInput(input)
			if !isValidUpdateMetadataInput(input) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.UpdateNodeMetadata(r.Context(), nodeID, input)
			if errors.Is(err, nodes.ErrNodeNotFound) {
				writeError(w, http.StatusNotFound, "node not found")
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

func normalizeCreateInput(input nodes.CreateInput) nodes.CreateInput {
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Region = strings.TrimSpace(input.Region)
	input.City = strings.TrimSpace(input.City)
	input.Provider = strings.TrimSpace(input.Provider)
	input.Note = strings.TrimSpace(input.Note)
	input.LifecycleStatus = nodes.LifecyclePendingEnrollment
	return input
}

func isValidCreateInput(input nodes.CreateInput) bool {
	if input.DisplayName == "" || input.Region == "" || input.City == "" || input.Provider == "" || input.LifecycleStatus == "" {
		return false
	}
	return nodes.IsValidLifecycleStatus(input.LifecycleStatus)
}

func normalizeUpdateMetadataInput(input nodes.UpdateMetadataInput) nodes.UpdateMetadataInput {
	normalizedLabels := make([]string, 0, len(input.Labels))
	seen := make(map[string]struct{}, len(input.Labels))
	for _, label := range input.Labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		normalizedLabels = append(normalizedLabels, label)
	}
	input.Labels = normalizedLabels
	input.Note = strings.TrimSpace(input.Note)
	return input
}

func isValidUpdateMetadataInput(input nodes.UpdateMetadataInput) bool {
	if len(input.Labels) > 20 {
		return false
	}
	for _, label := range input.Labels {
		if len(label) > 64 {
			return false
		}
	}
	return len(input.Note) <= 2000
}
