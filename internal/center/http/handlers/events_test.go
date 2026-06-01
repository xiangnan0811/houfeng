package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/store"
)

type fakeEventsRepository struct {
	filter store.EventsFilter
	result []store.EventListItem
	err    error
}

func (f *fakeEventsRepository) ListEvents(_ context.Context, filter store.EventsFilter) ([]store.EventListItem, error) {
	f.filter = filter
	return f.result, f.err
}

func TestEventsHandlerReturnsListWithFilters(t *testing.T) {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	repo := &fakeEventsRepository{result: []store.EventListItem{{
		EventID:       "evt_001",
		IncidentID:    "inc_001",
		IncidentClass: incidents.IncidentMonitoringInstanceDiskPressure,
		ObjectType:    incidents.ObjectTypeMonitoringInstance,
		ObjectID:      "mi_001",
		EventType:     incidents.EventIncidentStarted,
		Severity:      incidents.SeverityAlert,
		Summary:       "磁盘使用率 92.0%",
		CreatedAt:     now,
	}}}

	handler := handlers.Events(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/events?object_type=monitoring_instance&object_id=mi_001&severity=告警&event_type=incident_started&limit=25", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.filter.ObjectType != incidents.ObjectTypeMonitoringInstance || repo.filter.ObjectID != "mi_001" || repo.filter.EventType != incidents.EventIncidentStarted || repo.filter.Limit != 25 {
		t.Fatalf("filter = %#v, want parsed filters", repo.filter)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("body = %#v, want one event", body)
	}
	for _, key := range []string{"event_id", "incident_id", "incident_class", "object_type", "object_id", "event_type", "severity", "summary", "created_at"} {
		if _, ok := body.Items[0][key]; !ok {
			t.Fatalf("event = %#v, want key %q", body.Items[0], key)
		}
	}
	if _, ok := body.Items[0]["EventID"]; ok {
		t.Fatalf("event = %#v, want snake_case keys only", body.Items[0])
	}
	if body.Items[0]["event_id"] != "evt_001" {
		t.Fatalf("event = %#v, want event_id=evt_001", body.Items[0])
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &topLevel); err != nil {
		t.Fatalf("unmarshal top-level response: %v", err)
	}
	if _, ok := topLevel["items"]; !ok {
		t.Fatalf("top-level response = %#v, want items envelope", topLevel)
	}
}

func TestEventsHandlerReturnsListWithAdvancedFilters(t *testing.T) {
	repo := &fakeEventsRepository{}

	handler := handlers.Events(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/events?created_from=2026-04-25T00:00:00Z&created_to=2026-04-26T00:00:00Z&label=edge&notification_only=true&recovery_only=true&maintenance_only=true&include_backfilled=true", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.filter.CreatedFrom == nil || !repo.filter.CreatedFrom.Equal(time.Date(2026, time.April, 25, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("CreatedFrom = %v, want parsed RFC3339 timestamp", repo.filter.CreatedFrom)
	}
	if repo.filter.CreatedTo == nil || !repo.filter.CreatedTo.Equal(time.Date(2026, time.April, 26, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("CreatedTo = %v, want parsed RFC3339 timestamp", repo.filter.CreatedTo)
	}
	if repo.filter.Label != "edge" || !repo.filter.NotificationOnly || !repo.filter.RecoveryOnly || !repo.filter.MaintenanceOnly || !repo.filter.IncludeBackfilled {
		t.Fatalf("filter = %#v, want advanced filters", repo.filter)
	}
}

func TestEventsHandlerReturnsEmptyItemsArray(t *testing.T) {
	handler := handlers.Events(&fakeEventsRepository{})
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Items == nil {
		t.Fatalf("items = nil, want empty array")
	}
	if len(body.Items) != 0 {
		t.Fatalf("items = %#v, want empty array", body.Items)
	}
}

func TestEventsHandlerRejectsInvalidLimit(t *testing.T) {
	handler := handlers.Events(&fakeEventsRepository{})
	req := httptest.NewRequest(http.MethodGet, "/api/events?limit=-1", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestEventsHandlerRejectsInvalidAdvancedFilters(t *testing.T) {
	tests := []string{
		"/api/events?created_from=not-a-time",
		"/api/events?created_to=not-a-time",
		"/api/events?notification_only=definitely",
		"/api/events?recovery_only=definitely",
		"/api/events?maintenance_only=definitely",
		"/api/events?include_backfilled=definitely",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			handler := handlers.Events(&fakeEventsRepository{})
			req := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}
