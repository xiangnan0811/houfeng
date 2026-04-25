package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/store"
)

type fakeIncidentsRepository struct {
	filter store.IncidentsFilter
	result []store.ActiveIncidentListItem
	err    error
}

func (f *fakeIncidentsRepository) ListActiveIncidents(_ context.Context, filter store.IncidentsFilter) ([]store.ActiveIncidentListItem, error) {
	f.filter = filter
	return f.result, f.err
}

func TestIncidentsHandlerReturnsListWithFilters(t *testing.T) {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	repo := &fakeIncidentsRepository{result: []store.ActiveIncidentListItem{{
		IncidentID:      "inc_001",
		IncidentClass:   incidents.IncidentNodeDiskPressure,
		ObjectType:      incidents.ObjectTypeNode,
		ObjectID:        "nd_001",
		Severity:        incidents.SeverityAlert,
		StartedAt:       now,
		LastEvaluatedAt: now.Add(time.Minute),
		SourceSummary:   "磁盘使用率 92.0%",
	}}}

	handler := handlers.Incidents(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/incidents?object_type=node&object_id=nd_001&severity=告警&limit=25", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.filter.ObjectType != incidents.ObjectTypeNode || repo.filter.ObjectID != "nd_001" || repo.filter.Severity != incidents.SeverityAlert || repo.filter.Limit != 25 {
		t.Fatalf("filter = %#v, want parsed filters", repo.filter)
	}
	var body []store.ActiveIncidentListItem
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body) != 1 || body[0].IncidentID != "inc_001" {
		t.Fatalf("body = %#v, want one incident", body)
	}
	responseBody := recorder.Body.String()
	for _, want := range []string{
		`"incident_id":`,
		`"incident_class":`,
		`"object_type":`,
		`"object_id":`,
		`"severity":`,
		`"started_at":`,
		`"last_evaluated_at":`,
		`"source_summary":`,
	} {
		if !strings.Contains(responseBody, want) {
			t.Fatalf("response body = %q, want snake_case field %s", responseBody, want)
		}
	}
	for _, unwanted := range []string{`"IncidentID":`, `"IncidentClass":`, `"ObjectType":`, `"ObjectID":`, `"Severity":`, `"StartedAt":`, `"LastEvaluatedAt":`, `"SourceSummary":`} {
		if strings.Contains(responseBody, unwanted) {
			t.Fatalf("response body = %q, unexpectedly contains Go field name %s", responseBody, unwanted)
		}
	}
}

func TestIncidentsHandlerRejectsInvalidLimit(t *testing.T) {
	handler := handlers.Incidents(&fakeIncidentsRepository{})
	req := httptest.NewRequest(http.MethodGet, "/api/incidents?limit=0", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
