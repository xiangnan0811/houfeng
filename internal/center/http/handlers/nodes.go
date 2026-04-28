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
			labels, note, ok, err := decodeUpdateMetadataRequest(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			input := nodes.UpdateMetadataInput{Labels: labels, Note: note}
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

			record, err := repo.UpdateNodeMetadata(r.Context(), nodeID, input)
			if errors.Is(err, nodes.ErrNodeNotFound) {
				writeError(w, http.StatusNotFound, "node not found")
				return
			}
			if errors.Is(err, nodes.ErrNodeMetadataConflict) {
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
	input.Labels, input.Note = normalizeMetadata(input.Labels, input.Note)
	return input
}

func isValidUpdateMetadataInput(input nodes.UpdateMetadataInput) bool {
	return isValidMetadata(input.Labels, input.Note)
}
