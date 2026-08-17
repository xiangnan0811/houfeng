package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordcollaboration"
)

const testInboxNotificationID = "rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRecordInboxHandlerReturnsNoStoreClosedShapesAndNonNilCollections(t *testing.T) {
	actor := testRecordActionActor(t)
	application := &recordInboxHandlerStub{items: []recordcollaboration.InboxItem{testInboxItem()}, item: testInboxItem(), count: 1,
		target: recordcollaboration.InboxDeepLinkTarget{RecordID: "rec_inbox", SubjectKind: recordcollaboration.NotificationSubjectAction, SubjectID: "ract_inbox"}}
	handler := RecordInbox(application)
	for _, requestCase := range []struct {
		method, path string
		wantFragment string
	}{
		{method: http.MethodGet, path: "/api/record-notifications", wantFragment: `"items":[{`},
		{method: http.MethodGet, path: "/api/record-notifications/unread-count", wantFragment: `"unread_count":1`},
		{method: http.MethodGet, path: "/api/record-notifications/" + testInboxNotificationID, wantFragment: `"notification_id":"` + testInboxNotificationID + `"`},
		{method: http.MethodGet, path: "/api/record-notifications/" + testInboxNotificationID + "/target", wantFragment: `"subject_id":"ract_inbox"`},
		{method: http.MethodPut, path: "/api/record-notifications/" + testInboxNotificationID + "/read", wantFragment: `"notification_id":"` + testInboxNotificationID + `"`},
		{method: http.MethodPut, path: "/api/record-notifications/" + testInboxNotificationID + "/unread", wantFragment: `"notification_id":"` + testInboxNotificationID + `"`},
		{method: http.MethodPut, path: "/api/record-notifications/" + testInboxNotificationID + "/dismiss", wantFragment: `"notification_id":"` + testInboxNotificationID + `"`},
	} {
		request := httptest.NewRequest(requestCase.method, requestCase.path, nil)
		request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl || !strings.Contains(recorder.Body.String(), requestCase.wantFragment) {
			t.Fatalf("%s %s status/headers/body = %d %#v %s", requestCase.method, requestCase.path, recorder.Code, recorder.Header(), recorder.Body.String())
		}
		for _, forbidden := range []string{"body_markdown", "render_model", "evidence", "authorization_epoch", "record_fence_epoch", "outbox", "payload", "title"} {
			if strings.Contains(recorder.Body.String(), forbidden) {
				t.Fatalf("%s leaked %q: %s", requestCase.path, forbidden, recorder.Body.String())
			}
		}
	}
	if len(application.transitions) != 3 || application.transitions[0].Kind != recordcollaboration.InboxTransitionRead ||
		application.transitions[1].Kind != recordcollaboration.InboxTransitionUnread || application.transitions[2].Kind != recordcollaboration.InboxTransitionDismiss {
		t.Fatalf("transitions = %#v", application.transitions)
	}

	application.items = []recordcollaboration.InboxItem{}
	request := httptest.NewRequest(http.MethodGet, "/api/record-notifications", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"items":[]`) || strings.Contains(recorder.Body.String(), `"items":null`) {
		t.Fatalf("empty list response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRecordInboxHandlerRejectsNonCanonicalListQueryBeforeApplication(t *testing.T) {
	actor := testRecordActionActor(t)
	for _, rawQuery := range []string{"limit=%zz", "private=1", "limit=50&limit=51", "limit=050"} {
		t.Run(rawQuery, func(t *testing.T) {
			application := &recordInboxHandlerStub{}
			request := httptest.NewRequest(http.MethodGet, "/api/record-notifications?"+rawQuery, nil)
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordInbox(application).ServeHTTP(recorder, request)

			if recorder.Code != http.StatusBadRequest || application.listCalls != 0 {
				t.Fatalf("query %q status/list calls = %d/%d, want 400/0; body=%s", rawQuery, recorder.Code, application.listCalls, recorder.Body.String())
			}
		})
	}
}

func TestRecordInboxHandlerUsesOpaqueNotFoundForItemAndTarget(t *testing.T) {
	actor := testRecordActionActor(t)
	for _, suffix := range []string{"", "/target", "/read"} {
		application := &recordInboxHandlerStub{err: recordcollaboration.ErrInboxNotFound}
		method := http.MethodGet
		if suffix == "/read" {
			method = http.MethodPut
		}
		request := httptest.NewRequest(method, "/api/record-notifications/"+testInboxNotificationID+suffix, nil)
		request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
		recorder := httptest.NewRecorder()
		RecordInbox(application).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl || strings.Contains(recorder.Body.String(), testInboxNotificationID) {
			t.Fatalf("opaque %s response = %d %s", suffix, recorder.Code, recorder.Body.String())
		}
	}
	application := &recordInboxHandlerStub{err: errors.New("database details secret")}
	request := httptest.NewRequest(http.MethodGet, "/api/record-notifications", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	RecordInbox(application).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "database details secret") {
		t.Fatalf("internal error leaked: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRecordInboxHandlerMapsSourceDependencyUnavailableWithoutLeaking(t *testing.T) {
	actor := testRecordActionActor(t)
	for _, test := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/record-notifications"},
		{method: http.MethodGet, path: "/api/record-notifications/unread-count"},
		{method: http.MethodGet, path: "/api/record-notifications/" + testInboxNotificationID},
		{method: http.MethodGet, path: "/api/record-notifications/" + testInboxNotificationID + "/target"},
		{method: http.MethodPut, path: "/api/record-notifications/" + testInboxNotificationID + "/read"},
	} {
		application := &recordInboxHandlerStub{err: errors.Join(
			recordcollaboration.ErrInboxUnavailable,
			errors.New("source database details secret"),
		)}
		request := httptest.NewRequest(test.method, test.path, nil)
		request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
		recorder := httptest.NewRecorder()
		RecordInbox(application).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl ||
			!strings.Contains(recorder.Body.String(), `"code":"record_service_unavailable"`) ||
			strings.Contains(recorder.Body.String(), "source database details secret") {
			t.Fatalf("source dependency response %s %s = %d %#v %s", test.method, test.path, recorder.Code, recorder.Header(), recorder.Body.String())
		}
	}
}

func TestRecordInboxHandlerRejectsEveryNonEmptyTransitionBodyWithoutCallingApplication(t *testing.T) {
	actor := testRecordActionActor(t)
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "json value", body: `{}`},
		{name: "trailing bytes", body: "{} trailing"},
		{name: "whitespace", body: "\n"},
		{name: "oversized", body: strings.Repeat("x", DefaultJSONBodyLimit+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := &recordInboxHandlerStub{item: testInboxItem()}
			request := httptest.NewRequest(http.MethodPut, "/api/record-notifications/"+testInboxNotificationID+"/read", strings.NewReader(test.body))
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordInbox(application).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || len(application.transitions) != 0 {
				t.Fatalf("nonempty transition body status/calls = %d/%d, want 400/0", recorder.Code, len(application.transitions))
			}
		})
	}
}

type recordInboxHandlerStub struct {
	items       []recordcollaboration.InboxItem
	item        recordcollaboration.InboxItem
	target      recordcollaboration.InboxDeepLinkTarget
	count       int
	err         error
	listCalls   int
	transitions []recordcollaboration.InboxTransitionRequest
}

func (stub *recordInboxHandlerStub) ListInbox(context.Context, recordcollaboration.InboxListRequest) ([]recordcollaboration.InboxItem, error) {
	stub.listCalls++
	return stub.items, stub.err
}
func (stub *recordInboxHandlerStub) GetInboxItem(context.Context, recordcollaboration.InboxItemRequest) (recordcollaboration.InboxItem, error) {
	return stub.item, stub.err
}
func (stub *recordInboxHandlerStub) GetInboxDeepLink(context.Context, recordcollaboration.InboxItemRequest) (recordcollaboration.InboxDeepLinkTarget, error) {
	return stub.target, stub.err
}
func (stub *recordInboxHandlerStub) TransitionInbox(_ context.Context, request recordcollaboration.InboxTransitionRequest) (recordcollaboration.InboxItem, error) {
	stub.transitions = append(stub.transitions, request)
	return stub.item, stub.err
}
func (stub *recordInboxHandlerStub) CountUnreadInbox(context.Context, recordcollaboration.InboxListRequest) (int, error) {
	return stub.count, stub.err
}

func testInboxItem() recordcollaboration.InboxItem {
	return recordcollaboration.InboxItem{
		NotificationID: testInboxNotificationID, RecordID: "rec_inbox",
		EventKind:   recordcollaboration.NotificationEventActionAssigned,
		SubjectKind: recordcollaboration.NotificationSubjectAction, SubjectID: "ract_inbox", SourceVersion: 3,
		Reason: recordcollaboration.NotificationReasonAssignee, Mandatory: true,
		EventAt: time.Date(2026, 8, 17, 6, 0, 0, 0, time.UTC),
	}
}
