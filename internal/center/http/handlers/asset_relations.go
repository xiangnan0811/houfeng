package handlers

import (
	"errors"
	"houfeng/internal/center/assetrelations"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/vpsassets"
	"net/http"
	"strings"
)

func VPSAssetAssociations(repo assetrelations.Repository, kind string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 || parts[0] != "api" || parts[1] != "vps" || parts[2] == "" || parts[3] != kind+"-associations" || (len(parts) != 4 && (len(parts) != 6 || parts[5] != "end" || parts[4] == "")) {
			writeError(w, http.StatusNotFound, "association not found")
			return
		}
		vpsID := parts[2]
		if len(parts) == 4 && r.Method == http.MethodGet {
			records, err := repo.List(r.Context(), vpsID, kind, r.URL.Query().Get("current") == "true")
			if writeAssetRelationError(w, err) {
				return
			}
			writeJSON(w, http.StatusOK, records)
			return
		}
		actor, ok := sessionctx.UserIDFromContext(r.Context())
		if !ok || actor == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if len(parts) == 4 && r.Method == http.MethodPost {
			var input assetrelations.LinkInput
			if decodeJSON(r, &input) != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			input = assetrelations.Normalize(input)
			if assetrelations.Validate(kind, input) != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			record, err := repo.Link(r.Context(), vpsID, kind, input, actor)
			if writeAssetRelationError(w, err) {
				return
			}
			writeJSON(w, http.StatusCreated, record)
			return
		}
		if len(parts) == 6 && r.Method == http.MethodPatch {
			var input struct {
				Reason string `json:"reason"`
			}
			if decodeJSON(r, &input) != nil || strings.TrimSpace(input.Reason) == "" {
				writeError(w, http.StatusBadRequest, "reason required")
				return
			}
			record, err := repo.End(r.Context(), vpsID, kind, parts[4], input.Reason, actor)
			if writeAssetRelationError(w, err) {
				return
			}
			writeJSON(w, http.StatusOK, record)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
}
func writeAssetRelationError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, assetrelations.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid association")
	case errors.Is(err, assetrelations.ErrNotFound):
		writeError(w, http.StatusNotFound, "association or object not found")
	case errors.Is(err, assetrelations.ErrConflict), errors.Is(err, vpsassets.ErrVPSAssetReadonly):
		writeError(w, http.StatusConflict, "association conflict")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
	return true
}
