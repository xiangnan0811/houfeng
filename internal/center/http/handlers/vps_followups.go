package handlers

import (
	"errors"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/vpsfollowups"
	"net/http"
	"strings"
)

func VPSFollowups(repo vpsfollowups.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if (len(p) != 4 && len(p) != 5) || p[0] != "api" || p[1] != "vps" || p[2] == "" || p[3] != "followups" || (len(p) == 5 && p[4] == "") {
			writeError(w, http.StatusNotFound, "follow-up not found")
			return
		}
		if len(p) == 4 && r.Method == http.MethodGet {
			records, err := repo.List(r.Context(), p[2])
			if writeVPSFollowupError(w, err) {
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
		if len(p) == 4 && r.Method == http.MethodPost {
			var input vpsfollowups.CreateInput
			if decodeJSON(r, &input) != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if vpsfollowups.ValidateCreate(input) != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			record, err := repo.Create(r.Context(), p[2], input, actor)
			if writeVPSFollowupError(w, err) {
				return
			}
			writeJSON(w, http.StatusCreated, record)
			return
		}
		if len(p) == 5 && r.Method == http.MethodPatch {
			var input vpsfollowups.ResolveInput
			if decodeJSON(r, &input) != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if vpsfollowups.ValidateResolve(input) != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			record, err := repo.Resolve(r.Context(), p[2], p[4], input, actor)
			if writeVPSFollowupError(w, err) {
				return
			}
			writeJSON(w, http.StatusOK, record)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
}
func writeVPSFollowupError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, vpsfollowups.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid follow-up")
	case errors.Is(err, vpsfollowups.ErrNotFound):
		writeError(w, http.StatusNotFound, "follow-up not found")
	case errors.Is(err, vpsfollowups.ErrConflict):
		writeError(w, http.StatusConflict, "follow-up already closed")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
	return true
}
