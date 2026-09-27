package handlers

import (
	"errors"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/vpsmaintenance"
	"net/http"
	"strings"
)

func VPSMaintenance(repo vpsmaintenance.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(p) != 4 || p[0] != "api" || p[1] != "vps" || p[2] == "" || (p[3] != "maintenance" && p[3] != "maintenance-review") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if p[3] == "maintenance-review" && r.Method == http.MethodGet {
			result, err := repo.Review(r.Context(), p[2])
			if writeVPSMaintenanceError(w, result, err) {
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
		if p[3] != "maintenance" || (r.Method != http.MethodPost && r.Method != http.MethodDelete) {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		actor, ok := sessionctx.UserIDFromContext(r.Context())
		if !ok || actor == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		var result vpsmaintenance.Review
		var err error
		if r.Method == http.MethodPost {
			var input vpsmaintenance.StartInput
			if decodeJSON(r, &input) != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			result, err = repo.Start(r.Context(), p[2], input, actor)
		} else {
			var input vpsmaintenance.EndInput
			if decodeJSON(r, &input) != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			result, err = repo.End(r.Context(), p[2], input, actor)
		}
		if writeVPSMaintenanceError(w, result, err) {
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func writeVPSMaintenanceError(w http.ResponseWriter, review vpsmaintenance.Review, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, vpsmaintenance.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid maintenance input")
	case errors.Is(err, vpsmaintenance.ErrNotFound):
		writeError(w, http.StatusNotFound, "VPS not found")
	case errors.Is(err, vpsmaintenance.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "maintenance state changed", "review": review})
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
	return true
}
