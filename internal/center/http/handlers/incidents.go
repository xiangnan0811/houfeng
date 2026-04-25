package handlers

import (
	"context"
	"net/http"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/store"
)

type IncidentsRepository interface {
	ListActiveIncidents(context.Context, store.IncidentsFilter) ([]store.ActiveIncidentListItem, error)
}

func Incidents(repo IncidentsRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		limit, err := parseLimit(r, 50)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		records, err := repo.ListActiveIncidents(r.Context(), store.IncidentsFilter{
			ObjectType: incidents.ObjectType(r.URL.Query().Get("object_type")),
			ObjectID:   r.URL.Query().Get("object_id"),
			Severity:   incidents.Severity(r.URL.Query().Get("severity")),
			Limit:      limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}
