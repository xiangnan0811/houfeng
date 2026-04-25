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
		IncidentClass: incidents.IncidentNodeDiskPressure,
		ObjectType:    incidents.ObjectTypeNode,
		ObjectID:      "nd_001",
		EventType:     incidents.EventIncidentStarted,
		Severity:      incidents.SeverityAlert,
		Summary:       "磁盘使用率 92.0%",
		CreatedAt:     now,
	}}}

	handler := handlers.Events(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/events?object_type=node&object_id=nd_001&severity=告警&event_type=incident_started&limit=25", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.filter.ObjectType != incidents.ObjectTypeNode || repo.filter.ObjectID != "nd_001" || repo.filter.EventType != incidents.EventIncidentStarted || repo.filter.Limit != 25 {
		t.Fatalf("filter = %#v, want parsed filters", repo.filter)
	}
	var body []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("body = %#v, want one event", body)
	}
	for _, key := range []string{"event_id", "incident_id", "incident_class", "object_type", "object_id", "event_type", "severity", "summary", "created_at"} {
		if _, ok := body[0][key]; !ok {
			t.Fatalf("event = %#v, want key %q", body[0], key)
		}
	}
	if _, ok := body[0]["EventID"]; ok {
		t.Fatalf("event = %#v, want snake_case keys only", body[0])
	}
	if body[0]["event_id"] != "evt_001" {
		t.Fatalf("event = %#v, want event_id=evt_001", body[0])
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
