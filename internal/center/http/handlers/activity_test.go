package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

type subjectActivityServiceStub struct {
	result   activity.ListResult
	err      error
	requests []activity.ListRequest
}

func (stub *subjectActivityServiceStub) List(
	_ context.Context,
	request activity.ListRequest,
) (activity.ListResult, error) {
	stub.requests = append(stub.requests, request)
	if stub.err != nil {
		return activity.ListResult{}, stub.err
	}
	return stub.result, nil
}

func testSubjectActivityActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func serveSubjectActivity(
	t *testing.T,
	service subjectActivityService,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), testSubjectActivityActor(t)))
	recorder := httptest.NewRecorder()
	SubjectActivity(service).ServeHTTP(recorder, request)
	return recorder
}

func TestSubjectActivityHandlerTranslatesFilters(t *testing.T) {
	service := &subjectActivityServiceStub{
		result: activity.ListResult{
			Subject: activity.SubjectHeader{
				Kind:     records.SubjectKindVPS,
				SourceID: "vps_7c2a4e18b09d5f31",
				Identity: map[string]string{"display_name": "Alpha"},
				Status:   activity.SubjectStatusLive,
			},
			View:           activity.ViewRecords,
			SnapshotCursor: "snap",
			Items:          []activity.Event{},
			SourceStatuses: []activity.SourceStatus{},
			Freshness: activity.Freshness{
				State: "ready",
			},
		},
	}
	recorder := serveSubjectActivity(t, service,
		"/api/subjects/vps/vps_7c2a4e18b09d5f31/activity?"+
			"view=records&source=record_domain&source=evidence_snapshot"+
			"&event_kind=record_created&from=2026-08-01T00:00:00Z&to=2026-08-20T00:00:00Z"+
			"&versions=current&limit=25&cursor=opaque-token")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(service.requests) != 1 {
		t.Fatalf("list calls = %d", len(service.requests))
	}
	request := service.requests[0]
	if request.Cursor != "opaque-token" {
		t.Fatalf("cursor = %q", request.Cursor)
	}
	if request.Query.Subject.Kind != records.SubjectKindVPS ||
		request.Query.Subject.SourceID != "vps_7c2a4e18b09d5f31" {
		t.Fatalf("subject = %#v", request.Query.Subject)
	}
	if request.Query.View != activity.ViewRecords || request.Query.Versions != activity.VersionsCurrent ||
		request.Query.Limit != 25 {
		t.Fatalf("query = %#v", request.Query)
	}
	if len(request.Query.Sources) != 2 || len(request.Query.EventKinds) != 1 {
		t.Fatalf("filters = %#v", request.Query)
	}
}

func TestSubjectActivityHandlerRefusesUnknownParametersAndKinds(t *testing.T) {
	service := &subjectActivityServiceStub{result: activity.ListResult{Items: []activity.Event{}}}
	cases := []struct {
		name   string
		target string
		status int
	}{
		{name: "unknown param", target: "/api/subjects/vps/vps_7c2a4e18b09d5f31/activity?page=1", status: http.StatusBadRequest},
		{name: "unknown kind", target: "/api/subjects/rack/rack_1/activity", status: http.StatusNotFound},
		{name: "bad path", target: "/api/subjects/vps/activity", status: http.StatusNotFound},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			recorder := serveSubjectActivity(t, service, tt.target)
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d body=%s", recorder.Code, tt.status, recorder.Body.String())
			}
		})
	}
}

func TestSubjectActivityHandlerMapsDomainErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "not found", err: activity.ErrSubjectNotFound, status: http.StatusNotFound, code: "resource_not_found"},
		{name: "unavailable", err: activity.ErrProjectionUnavailable, status: http.StatusServiceUnavailable, code: "activity_projection_unavailable"},
		{name: "cursor invalid", err: activity.ErrCursorInvalid, status: http.StatusBadRequest, code: "cursor_invalid"},
		{name: "cursor expired", err: activity.ErrCursorExpired, status: http.StatusConflict, code: "cursor_expired"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			service := &subjectActivityServiceStub{err: tt.err}
			recorder := serveSubjectActivity(t, service, "/api/subjects/vps/vps_7c2a4e18b09d5f31/activity")
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
			if !strings.Contains(recorder.Body.String(), tt.code) {
				t.Fatalf("body = %s, want code %s", recorder.Body.String(), tt.code)
			}
		})
	}
}

func TestSubjectActivityHandlerDenylistsGlobalHeadFields(t *testing.T) {
	observed := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	service := &subjectActivityServiceStub{
		result: activity.ListResult{
			Subject: activity.SubjectHeader{
				Kind: records.SubjectKindVPS, SourceID: "vps_7c2a4e18b09d5f31",
				Identity: map[string]string{}, Status: activity.SubjectStatusLive,
			},
			View:           activity.ViewActivity,
			SnapshotCursor: "snap",
			Items: []activity.Event{{
				ActivityID:     "act_gv2xq4hb7zsm3nkfr6wcyd5ple",
				EventKind:      activity.EventKindRecordCreated,
				EventAt:        observed,
				RecordedAt:     observed,
				SourceKind:     activity.SourceKindRecordDomain,
				Subjects:       []activity.SubjectSnapshot{},
				Presentation:   activity.Presentation{Version: 1, Title: "Created"},
				IngestSequence: 99,
			}},
			SourceStatuses: []activity.SourceStatus{},
			Freshness: activity.Freshness{
				State:             "ready",
				VisibleObservedAt: &observed,
			},
		},
	}
	recorder := serveSubjectActivity(t, service, "/api/subjects/vps/vps_7c2a4e18b09d5f31/activity")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{
		"projection_generation", "as_of_ingest_sequence", "current_ingest_sequence", "ingest_sequence",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["items"]; !ok {
		t.Fatal("items missing")
	}
}
