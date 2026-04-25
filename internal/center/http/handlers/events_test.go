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
	var body []store.EventListItem
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body) != 1 || body[0].EventID != "evt_001" {
		t.Fatalf("body = %#v, want one event", body)
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
