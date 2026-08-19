package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
)

type recordSearchServiceStub struct {
	result   recordsearch.Result
	err      error
	requests []recordsearch.SearchRequest
}

func (stub *recordSearchServiceStub) Search(
	_ context.Context,
	request recordsearch.SearchRequest,
) (recordsearch.Result, error) {
	stub.requests = append(stub.requests, request)
	if stub.err != nil {
		return recordsearch.Result{}, stub.err
	}
	return stub.result, nil
}

func testRecordSearchActor(t *testing.T) recordauth.ActorScope {
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

func serveRecordSearch(
	t *testing.T,
	service recordSearchService,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), testRecordSearchActor(t)))
	recorder := httptest.NewRecorder()
	RecordSearch(service).ServeHTTP(recorder, request)
	return recorder
}

// The handler owns transport parsing only. Every filter has to reach the domain
// intact, because a filter silently dropped here becomes a wider answer than the
// operator asked for.
func TestRecordSearchHandlerTranslatesEveryFilterToTheDomain(t *testing.T) {
	service := &recordSearchServiceStub{}
	recorder := serveRecordSearch(t, service, "/api/records/search?"+
		"q=%E7%A3%81%E7%9B%98&type=troubleshooting&type=migration&status=investigating"+
		"&status_group=in_progress&lifecycle=active&owner=usr_aaaaaaaaaaaaaaaaaaaaaaaa"+
		"&participant=usr_bbbbbbbbbbbbbbbbbbbbbbbb&tag=disk&tag=nvme"+
		"&subject=vps:vps_0123456789abcdef:affected:primary&subject=target::context"+
		"&follow_up=overdue&action=open"+
		"&occurred_from=2026-07-01T00:00:00Z&occurred_to=2026-08-01T00:00:00Z"+
		"&updated_from=2026-07-15T00:00:00Z&updated_to=2026-08-15T00:00:00Z"+
		"&sort=updated_at_asc&limit=25&cursor=opaque-token")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if len(service.requests) != 1 {
		t.Fatalf("search calls = %d, want 1", len(service.requests))
	}
	request := service.requests[0]
	if request.Cursor != "opaque-token" {
		t.Fatalf("cursor = %q, want the token verbatim", request.Cursor)
	}
	if request.Actor.UserID != "usr_aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("actor = %#v, want the session actor", request.Actor)
	}
	query := request.Query
	if query.Text() != "磁盘" || query.PageSize() != 25 || query.Sort() != recordsearch.SortUpdatedAsc {
		t.Fatalf("text/page/sort = %q/%d/%q", query.Text(), query.PageSize(), query.Sort())
	}
	if len(query.Types()) != 2 || len(query.Statuses()) != 1 || len(query.StatusGroups()) != 1 ||
		len(query.Lifecycles()) != 1 || len(query.OwnerIDs()) != 1 || len(query.ParticipantIDs()) != 1 ||
		len(query.Tags()) != 2 {
		t.Fatalf("repeated filters = %#v", query)
	}
	if query.FollowUp() != recordsearch.FollowUpOverdue || query.Action() != recordsearch.ActionOpen {
		t.Fatalf("follow up/action = %q/%q", query.FollowUp(), query.Action())
	}
	occurred, updated := query.Occurred(), query.Updated()
	if occurred.From == nil || occurred.To == nil || updated.From == nil || updated.To == nil {
		t.Fatalf("time ranges = %#v / %#v", occurred, updated)
	}
	if !occurred.From.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("occurred from = %v", occurred.From)
	}
	subjects := query.Subjects()
	if len(subjects) != 2 {
		t.Fatalf("subjects = %#v, want two filters", subjects)
	}
	var full, partial recordsearch.SubjectFilter
	for _, subject := range subjects {
		if subject.Kind == records.SubjectKindVPS {
			full = subject
		} else {
			partial = subject
		}
	}
	if full.SourceID != "vps_0123456789abcdef" || full.Role != records.RelationRoleAffected ||
		full.Placement != recordsearch.SubjectPlacementPrimary {
		t.Fatalf("fully specified subject = %#v", full)
	}
	// An omitted segment has to mean "any" rather than an empty match, or the
	// positional form could only ever express the fully specified case.
	if partial.Kind != records.SubjectKindTarget || partial.SourceID != "" ||
		partial.Role != records.RelationRoleContext ||
		partial.Placement != recordsearch.SubjectPlacementAny {
		t.Fatalf("partially specified subject = %#v", partial)
	}
}

// A misspelled parameter must fail rather than be ignored: answering a wider
// question and presenting it as the narrow one is the worst possible outcome for
// a search surface.
func TestRecordSearchHandlerRefusesUnknownParameters(t *testing.T) {
	service := &recordSearchServiceStub{}

	tests := []string{
		"/api/records/search?tags=disk",
		"/api/records/search?record_type=note",
		"/api/records/search?page=2",
		"/api/records/search?q=disk&unknown=1",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			recorder := serveRecordSearch(t, service, target)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
	if len(service.requests) != 0 {
		t.Fatalf("search calls = %d, want the query refused before the service", len(service.requests))
	}
}

func TestRecordSearchHandlerRejectsMalformedQueryBeforeTheService(t *testing.T) {
	service := &recordSearchServiceStub{}

	tests := []struct {
		name   string
		target string
	}{
		{name: "unknown record type", target: "/api/records/search?type=rumor"},
		{name: "unknown lifecycle", target: "/api/records/search?lifecycle=deleted"},
		{name: "unknown follow up state", target: "/api/records/search?follow_up=someday"},
		{name: "unknown sort", target: "/api/records/search?sort=relevance"},
		{name: "zero limit", target: "/api/records/search?limit=0"},
		{name: "oversized limit", target: "/api/records/search?limit=1000"},
		{name: "non numeric limit", target: "/api/records/search?limit=ten"},
		{name: "malformed instant", target: "/api/records/search?occurred_from=2026-07-01"},
		{name: "inverted range", target: "/api/records/search?updated_from=2026-08-01T00:00:00Z&updated_to=2026-07-01T00:00:00Z"},
		{name: "malformed owner", target: "/api/records/search?owner=someone"},
		{name: "duplicate tag", target: "/api/records/search?tag=disk&tag=disk"},
		{name: "unknown subject kind", target: "/api/records/search?subject=rack"},
		{name: "oversized subject filter", target: "/api/records/search?subject=vps:a:b:c:d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := serveRecordSearch(t, service, tt.target)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			var response recordErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Code != "query_invalid" {
				t.Fatalf("error code = %q, want query_invalid", response.Code)
			}
		})
	}
	if len(service.requests) != 0 {
		t.Fatalf("search calls = %d, want every malformed query refused first", len(service.requests))
	}
}

func TestRecordSearchHandlerReturnsPageWithCursorAndGeneration(t *testing.T) {
	service := &recordSearchServiceStub{result: recordsearch.Result{
		Records:    []records.Record{{RecordID: "rec_searchresult00000001"}},
		NextCursor: "next-token",
		Generation: 12,
	}}

	recorder := serveRecordSearch(t, service, "/api/records/search?q=disk")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response recordSearchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Items) != 1 || response.Items[0].RecordID != "rec_searchresult00000001" {
		t.Fatalf("items = %#v", response.Items)
	}
	if response.NextCursor != "next-token" || response.Generation != 12 {
		t.Fatalf("cursor/generation = %q/%d", response.NextCursor, response.Generation)
	}
	if recorder.Header().Get("Cache-Control") != recordPrivateCacheControl {
		t.Fatalf("Cache-Control = %q, want %q",
			recorder.Header().Get("Cache-Control"), recordPrivateCacheControl)
	}
}

// An empty result set is a valid answer, and it has to serialize as an empty
// array rather than null so a client can render it without a special case.
func TestRecordSearchHandlerEncodesEmptyPageAsArray(t *testing.T) {
	recorder := serveRecordSearch(t, &recordSearchServiceStub{}, "/api/records/search?q=nothing")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if string(decoded["items"]) != "[]" {
		t.Fatalf("items = %s, want []", decoded["items"])
	}
	if _, present := decoded["next_cursor"]; present {
		t.Fatalf("next_cursor present on a final page: %s", recorder.Body.String())
	}
}

// Each failure carries a distinct remedy, so they must not collapse into one
// status: a republished index means "retry from page one", an unavailable index
// means "the deployment has a problem".
func TestRecordSearchHandlerMapsDomainFailuresToDistinctStatuses(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{name: "invalid query", err: recordsearch.ErrInvalidQuery, wantCode: http.StatusBadRequest, wantBody: "query_invalid"},
		{name: "invalid cursor", err: recordsearch.ErrInvalidCursor, wantCode: http.StatusBadRequest, wantBody: "cursor_invalid"},
		{
			name: "superseded generation", err: recordsearch.ErrGenerationSuperseded,
			wantCode: http.StatusConflict, wantBody: "search_generation_superseded",
		},
		{
			name: "unavailable index", err: recordsearch.ErrIndexUnavailable,
			wantCode: http.StatusServiceUnavailable, wantBody: "search_unavailable",
		},
		{
			name: "denied", err: recordauth.ErrDenied,
			wantCode: http.StatusNotFound, wantBody: "resource_not_found",
		},
		{
			name: "unexpected fault", err: errors.New("boom"),
			wantCode: http.StatusInternalServerError, wantBody: "internal_error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := serveRecordSearch(t, &recordSearchServiceStub{err: tt.err}, "/api/records/search?q=disk")
			if recorder.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, tt.wantCode, recorder.Body.String())
			}
			var response recordErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Code != tt.wantBody {
				t.Fatalf("error code = %q, want %q", response.Code, tt.wantBody)
			}
		})
	}
}

// A raw fault must never reach the client, because a search error string can
// carry the operator's own query text back out through a log or a UI toast.
func TestRecordSearchHandlerNeverEchoesFaultDetail(t *testing.T) {
	secret := "internal-index-detail-磁盘"
	recorder := serveRecordSearch(t,
		&recordSearchServiceStub{err: errors.New(secret)},
		"/api/records/search?q=disk")

	if body := recorder.Body.String(); body == "" {
		t.Fatal("empty error response")
	} else if strings.Contains(body, secret) {
		t.Fatalf("response echoed fault detail: %s", body)
	}
}

func TestRecordSearchHandlerRequiresSessionActorAndService(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/records/search?q=disk", nil)
	recorder := httptest.NewRecorder()
	RecordSearch(&recordSearchServiceStub{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status without a session actor = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}

	recorder = serveRecordSearch(t, nil, "/api/records/search?q=disk")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status without a service = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/records/search", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), testRecordSearchActor(t)))
	recorder = httptest.NewRecorder()
	RecordSearch(&recordSearchServiceStub{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status for POST = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}
