package handlers

import (
	"context"
	"net/http"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/store"
)

type EventsRepository interface {
	ListEvents(context.Context, store.EventsFilter) ([]store.EventListItem, error)
}

func Events(repo EventsRepository) http.Handler {
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
		records, err := repo.ListEvents(r.Context(), store.EventsFilter{
			ObjectType: incidents.ObjectType(r.URL.Query().Get("object_type")),
			ObjectID:   r.URL.Query().Get("object_id"),
			Severity:   incidents.Severity(r.URL.Query().Get("severity")),
			EventType:  incidents.EventType(r.URL.Query().Get("event_type")),
			Limit:      limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}
