package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/store"
)

func NodesCollection(repo store.NodeRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			nodes, err := repo.ListNodes(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, nodes)
		case http.MethodPost:
			var input store.CreateNodeInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
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

func NodeItem(repo store.NodeRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		nodeID := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
		nodeID = strings.Trim(nodeID, "/")
		if nodeID == "" {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}

		record, err := repo.GetNode(r.Context(), nodeID)
		if errors.Is(err, store.ErrNodeNotFound) {
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
