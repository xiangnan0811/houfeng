package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		createdFrom, err := parseOptionalRFC3339(r, "created_from")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid created_from")
			return
		}
		createdTo, err := parseOptionalRFC3339(r, "created_to")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid created_to")
			return
		}
		notificationOnly, err := parseOptionalBool(r, "notification_only")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid notification_only")
			return
		}
		recoveryOnly, err := parseOptionalBool(r, "recovery_only")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid recovery_only")
			return
		}
		maintenanceOnly, err := parseOptionalBool(r, "maintenance_only")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid maintenance_only")
			return
		}
		records, err := repo.ListEvents(r.Context(), store.EventsFilter{
			ObjectType:       incidents.ObjectType(r.URL.Query().Get("object_type")),
			ObjectID:         r.URL.Query().Get("object_id"),
			Severity:         incidents.Severity(r.URL.Query().Get("severity")),
			EventType:        incidents.EventType(r.URL.Query().Get("event_type")),
			CreatedFrom:      createdFrom,
			CreatedTo:        createdTo,
			Label:            strings.TrimSpace(r.URL.Query().Get("label")),
			NotificationOnly: notificationOnly,
			RecoveryOnly:     recoveryOnly,
			MaintenanceOnly:  maintenanceOnly,
			Limit:            limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}

func parseOptionalRFC3339(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseOptionalBool(r *http.Request, key string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}
