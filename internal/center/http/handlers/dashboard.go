package handlers

import (
	"context"
	"net/http"
	"strconv"

	"houfeng/internal/center/incidents"
)

type DashboardRepository interface {
	GetDashboardOverview(context.Context, int) (incidents.DashboardOverview, error)
}

func Dashboard(repo DashboardRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		limit, err := parseLimit(r, 10)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		overview, err := repo.GetDashboardOverview(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, overview)
	})
}

func parseLimit(r *http.Request, fallback int) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, http.ErrNoLocation
	}
	return value, nil
}
