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
)

type fakeDashboardRepository struct {
	limit  int
	result incidents.DashboardOverview
	err    error
}

func (f *fakeDashboardRepository) GetDashboardOverview(_ context.Context, limit int) (incidents.DashboardOverview, error) {
	f.limit = limit
	return f.result, f.err
}

func TestDashboardHandlerReturnsOverview(t *testing.T) {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	repo := &fakeDashboardRepository{result: incidents.DashboardOverview{
		AbnormalNodeCount:      2,
		RecentNewIncidentCount: 3,
		RecentEvents: []incidents.StateChangeEventRecord{{
			IncidentID:    "inc_001",
			IncidentClass: incidents.IncidentNodeDiskPressure,
			ObjectType:    incidents.ObjectTypeNode,
			ObjectID:      "nd_001",
			EventType:     incidents.EventIncidentStarted,
			Severity:      incidents.SeverityAlert,
			Summary:       "磁盘使用率 92.0%",
			CreatedAt:     now,
		}},
	}}

	handler := handlers.Dashboard(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?limit=5", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.limit != 5 {
		t.Fatalf("limit = %d, want 5", repo.limit)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body["abnormal_node_count"] != float64(2) {
		t.Fatalf("body = %#v, want abnormal_node_count=2", body)
	}
	if _, ok := body["AbnormalNodeCount"]; ok {
		t.Fatalf("body = %#v, want snake_case keys only", body)
	}
	recentEvents, ok := body["recent_events"].([]any)
	if !ok || len(recentEvents) != 1 {
		t.Fatalf("body = %#v, want one recent event", body)
	}
	event, ok := recentEvents[0].(map[string]any)
	if !ok {
		t.Fatalf("recentEvents[0] = %#v, want object", recentEvents[0])
	}
	for _, key := range []string{"incident_id", "incident_class", "object_type", "object_id", "event_type", "severity", "summary", "created_at"} {
		if _, ok := event[key]; !ok {
			t.Fatalf("event = %#v, want key %q", event, key)
		}
	}
	if _, ok := event["IncidentID"]; ok {
		t.Fatalf("event = %#v, want snake_case nested keys only", event)
	}
}

func TestDashboardHandlerRejectsInvalidLimit(t *testing.T) {
	handler := handlers.Dashboard(&fakeDashboardRepository{})
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?limit=abc", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
