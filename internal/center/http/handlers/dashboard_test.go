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
	var body incidents.DashboardOverview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.AbnormalNodeCount != 2 || len(body.RecentEvents) != 1 {
		t.Fatalf("body = %#v, want populated overview", body)
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
