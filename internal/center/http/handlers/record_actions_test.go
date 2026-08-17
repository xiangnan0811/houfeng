package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

func TestRecordActionsHandlerUsesTrustedActorAndResponseAllowlist(t *testing.T) {
	actor := testRecordActionActor(t)
	changedAt := time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC)
	application := &recordActionHandlerStub{result: recordcollaboration.ActionMutationResult{
		ActionID: "ract_action1", RecordID: "rec_actionparent1", Version: 1,
		Status: recordcollaboration.ActionStatusOpen, EventKind: recordcollaboration.ActionMutationCreate,
		ChangedAt: changedAt,
	}}
	handler := RecordActions(application)
	request := httptest.NewRequest(http.MethodPost, "/api/records/rec_actionparent1/actions", strings.NewReader(`{"title":"Investigate","details":"safe","assignee_id":"","due_at":null,"subject_revision_id":""}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-action-1")
	request.Header.Set("X-Role", "forged")
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl || recorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("response status/headers = %d %#v", recorder.Code, recorder.Header())
	}
	if application.createCalls != 1 || application.create.Actor.UserID != actor.UserID ||
		application.create.RecordID != "rec_actionparent1" || application.create.IdempotencyKey != "create-action-1" ||
		application.create.Fields.Title != "Investigate" || application.create.Fields.Details != "safe" {
		t.Fatalf("application request = %#v calls=%d", application.create, application.createCalls)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"action_id", "changed_at", "event_kind", "record_id", "replayed", "status", "version"}
	gotKeys := make([]string, 0, len(body))
	for key := range body {
		gotKeys = append(gotKeys, key)
	}
	if !sameSortedStrings(gotKeys, wantKeys) {
		t.Fatalf("response keys = %#v, want allowlist %#v; body=%s", gotKeys, wantKeys, recorder.Body.String())
	}
	for _, forbidden := range []string{"details", "title", "record_fence_epoch", "authorization_epoch", "request_fingerprint", "result_fingerprint"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("response leaks forbidden field %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestRecordActionsHandlerListsCurrentActions(t *testing.T) {
	actor := testRecordActionActor(t)
	dueAt := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	application := &recordActionHandlerStub{listResult: []recordcollaboration.ActionRecord{{
		ActionID: "ract_action1", RecordID: "rec_actionparent1", Version: 2,
		Status: recordcollaboration.ActionStatusOpen, Title: "复核证据窗口",
		AssigneeID: "usr_0123456789abcdef01234567", DueAt: &dueAt,
		SubjectRevisionID: "rrv_actionrevision1", CreatedAt: createdAt, UpdatedAt: updatedAt,
	}}}
	request := httptest.NewRequest(http.MethodGet, "/api/records/rec_actionparent1/actions?limit=25", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()

	RecordActions(application).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET action list status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if application.listCalls != 1 || application.list.Actor.UserID != actor.UserID ||
		application.list.RecordID != "rec_actionparent1" || application.list.Limit != 25 {
		t.Fatalf("list application request = %#v calls=%d", application.list, application.listCalls)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("items = %#v, want one", body.Items)
	}
	wantKeys := []string{"action_id", "assignee_id", "completed_at", "created_at", "due_at", "record_id", "status", "subject_revision_id", "title", "updated_at", "version"}
	gotKeys := make([]string, 0, len(body.Items[0]))
	for key := range body.Items[0] {
		gotKeys = append(gotKeys, key)
	}
	if !sameSortedStrings(gotKeys, wantKeys) || bytes.Contains(recorder.Body.Bytes(), []byte("details")) {
		t.Fatalf("action list allowlist keys = %#v, want %#v; body=%s", gotKeys, wantKeys, recorder.Body.String())
	}
}

func TestRecordActionsHandlerRejectsMalformedListQueryBeforeApplication(t *testing.T) {
	actor := testRecordActionActor(t)
	application := &recordActionHandlerStub{}
	request := httptest.NewRequest(http.MethodGet, "/api/records/rec_actionparent1/actions?limit=%zz", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()

	RecordActions(application).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || application.listCalls != 0 {
		t.Fatalf("malformed query status/list calls = %d/%d, want 400/0; body=%s", recorder.Code, application.listCalls, recorder.Body.String())
	}
}

func TestRecordActionsHandlerRequiresCanonicalHeadersBeforeApplication(t *testing.T) {
	actor := testRecordActionActor(t)
	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		headers http.Header
		status  int
	}{
		{name: "missing idempotency", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions", body: `{"title":"ok"}`, status: 400},
		{name: "multiple idempotency", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions", body: `{"title":"ok"}`, headers: http.Header{"Idempotency-Key": {"one", "two"}}, status: 400},
		{name: "whitespace idempotency", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions", body: `{"title":"ok"}`, headers: http.Header{"Idempotency-Key": {" key"}}, status: 400},
		{name: "missing if match", method: http.MethodPatch, path: "/api/records/rec_actionparent1/actions/ract_action1", body: `{"title":"ok"}`, headers: http.Header{"Idempotency-Key": {"update"}}, status: 400},
		{name: "weak if match", method: http.MethodPatch, path: "/api/records/rec_actionparent1/actions/ract_action1", body: `{"title":"ok"}`, headers: http.Header{"Idempotency-Key": {"update"}, "If-Match": {`W/"1"`}}, status: 400},
		{name: "multiple if match", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions/ract_action1/complete", body: `{}`, headers: http.Header{"Idempotency-Key": {"complete"}, "If-Match": {`"1"`, `"2"`}}, status: 400},
		{name: "unknown json", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions", body: `{"title":"ok","secret":"raw"}`, headers: http.Header{"Idempotency-Key": {"create"}}, status: 400},
		{name: "oversized", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions", body: `{"title":"` + strings.Repeat("x", DefaultJSONBodyLimit) + `"}`, headers: http.Header{"Idempotency-Key": {"create"}}, status: 413},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := &recordActionHandlerStub{}
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header = make(http.Header)
			for key, values := range test.headers {
				request.Header[key] = append([]string(nil), values...)
			}
			request.Header.Set("Content-Type", "application/json")
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordActions(application).ServeHTTP(recorder, request)
			if recorder.Code != test.status || application.calls() != 0 || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl {
				t.Fatalf("status=%d calls=%d headers=%#v body=%s", recorder.Code, application.calls(), recorder.Header(), recorder.Body.String())
			}
		})
	}
}

func TestRecordActionsHandlerRoutesUpdateAndTransitionsWithExactCAS(t *testing.T) {
	actor := testRecordActionActor(t)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		kind   recordcollaboration.ActionMutationKind
	}{
		{name: "update", method: http.MethodPatch, path: "/api/records/rec_actionparent1/actions/ract_action1", body: `{"title":"Updated","details":"safe"}`, kind: recordcollaboration.ActionMutationUpdate},
		{name: "complete", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions/ract_action1/complete", body: `{}`, kind: recordcollaboration.ActionMutationComplete},
		{name: "cancel", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions/ract_action1/cancel", body: `{}`, kind: recordcollaboration.ActionMutationCancel},
		{name: "reopen", method: http.MethodPost, path: "/api/records/rec_actionparent1/actions/ract_action1/reopen", body: `{}`, kind: recordcollaboration.ActionMutationReopen},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := &recordActionHandlerStub{result: recordcollaboration.ActionMutationResult{ActionID: "ract_action1", RecordID: "rec_actionparent1", Version: 8, Status: actionStatusForHandlerTest(test.kind), EventKind: test.kind, ChangedAt: time.Now().UTC()}}
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "command-1")
			request.Header.Set("If-Match", `"7"`)
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordActions(application).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || recorder.Header().Get("ETag") != `"8"` {
				t.Fatalf("status=%d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			if application.lastKind != test.kind || application.lastExpectedVersion != 7 {
				t.Fatalf("kind/version = %q/%d", application.lastKind, application.lastExpectedVersion)
			}
		})
	}
}

func TestRecordActionsHandlerBoundsIfMatchToPostgresActionVersion(t *testing.T) {
	actor := testRecordActionActor(t)
	tests := []struct {
		name    string
		version uint64
		status  int
		calls   int
	}{
		{name: "maximum incrementable", version: recordcollaboration.MaxActionVersion - 1, status: http.StatusOK, calls: 1},
		{name: "maximum bigint cannot increment", version: recordcollaboration.MaxActionVersion, status: http.StatusBadRequest},
		{name: "above maximum bigint", version: recordcollaboration.MaxActionVersion + 1, status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := &recordActionHandlerStub{result: recordcollaboration.ActionMutationResult{
				ActionID: "ract_action1", RecordID: "rec_actionparent1", Version: recordcollaboration.MaxActionVersion,
				Status: recordcollaboration.ActionStatusCompleted, EventKind: recordcollaboration.ActionMutationComplete,
				ChangedAt: time.Now().UTC(),
			}}
			request := httptest.NewRequest(http.MethodPost, "/api/records/rec_actionparent1/actions/ract_action1/complete", strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "bounded-version")
			request.Header.Set("If-Match", `"`+strconv.FormatUint(test.version, 10)+`"`)
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordActions(application).ServeHTTP(recorder, request)
			if recorder.Code != test.status || application.calls() != test.calls {
				t.Fatalf("version=%d status/calls=%d/%d, want %d/%d; body=%s", test.version, recorder.Code, application.calls(), test.status, test.calls, recorder.Body.String())
			}
		})
	}
}

func TestRecordActionsHandlerMapsOpaqueAndContentFreeErrors(t *testing.T) {
	actor := testRecordActionActor(t)
	secret := "raw-action-secret"
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "denied", err: fmtWrap(recordauth.ErrDenied, secret), status: 404, code: "resource_not_found"},
		{name: "missing", err: fmtWrap(recordcollaboration.ErrActionNotFound, secret), status: 404, code: "resource_not_found"},
		{name: "reserved", err: fmtWrap(records.ErrRecordDeletionReserved, secret), status: 404, code: "resource_not_found"},
		{name: "cas", err: fmtWrap(recordcollaboration.ErrActionConflict, secret), status: 409, code: "action_conflict"},
		{name: "idempotency reuse", err: fmtWrap(recordplatform.ErrIdempotencyKeyReused, secret), status: 409, code: "idempotency_key_reused"},
		{name: "in progress", err: fmtWrap(recordplatform.ErrIdempotencyInProgress, secret), status: 409, code: "action_operation_in_progress"},
		{name: "semantic", err: fmtWrap(recordcollaboration.ErrInvalidActionFields, secret), status: 422, code: "action_invalid"},
		{name: "membership", err: fmtWrap(recordcollaboration.ErrMembershipDenied, secret), status: 422, code: "action_invalid"},
		{name: "unavailable", err: fmtWrap(store.ErrRecordPlatformAdmissionUnavailable, secret), status: 503, code: "record_service_unavailable"},
		{name: "unknown", err: errors.New(secret), status: 500, code: "internal_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := &recordActionHandlerStub{err: test.err}
			request := httptest.NewRequest(http.MethodPost, "/api/records/rec_actionparent1/actions", strings.NewReader(`{"title":"safe"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "create-1")
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordActions(application).ServeHTTP(recorder, request)
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) || strings.Contains(recorder.Body.String(), secret) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

type recordActionHandlerStub struct {
	create                                    recordcollaboration.ActionCreateApplicationRequest
	update                                    recordcollaboration.ActionUpdateApplicationRequest
	transition                                recordcollaboration.ActionTransitionApplicationRequest
	list                                      recordcollaboration.ActionListApplicationRequest
	result                                    recordcollaboration.ActionMutationResult
	listResult                                []recordcollaboration.ActionRecord
	err                                       error
	createCalls, updateCalls, transitionCalls int
	listCalls                                 int
	lastKind                                  recordcollaboration.ActionMutationKind
	lastExpectedVersion                       uint64
}

func (stub *recordActionHandlerStub) ListActions(_ context.Context, request recordcollaboration.ActionListApplicationRequest) ([]recordcollaboration.ActionRecord, error) {
	stub.listCalls++
	stub.list = request
	return stub.listResult, stub.err
}

func (stub *recordActionHandlerStub) CreateAction(_ context.Context, request recordcollaboration.ActionCreateApplicationRequest) (recordcollaboration.ActionMutationResult, error) {
	stub.createCalls++
	stub.create = request
	stub.lastKind = recordcollaboration.ActionMutationCreate
	return stub.result, stub.err
}
func (stub *recordActionHandlerStub) UpdateAction(_ context.Context, request recordcollaboration.ActionUpdateApplicationRequest) (recordcollaboration.ActionMutationResult, error) {
	stub.updateCalls++
	stub.update = request
	stub.lastKind = recordcollaboration.ActionMutationUpdate
	stub.lastExpectedVersion = request.ExpectedVersion
	return stub.result, stub.err
}
func (stub *recordActionHandlerStub) CompleteAction(_ context.Context, request recordcollaboration.ActionTransitionApplicationRequest) (recordcollaboration.ActionMutationResult, error) {
	return stub.transitionCall(recordcollaboration.ActionMutationComplete, request)
}
func (stub *recordActionHandlerStub) CancelAction(_ context.Context, request recordcollaboration.ActionTransitionApplicationRequest) (recordcollaboration.ActionMutationResult, error) {
	return stub.transitionCall(recordcollaboration.ActionMutationCancel, request)
}
func (stub *recordActionHandlerStub) ReopenAction(_ context.Context, request recordcollaboration.ActionTransitionApplicationRequest) (recordcollaboration.ActionMutationResult, error) {
	return stub.transitionCall(recordcollaboration.ActionMutationReopen, request)
}
func (stub *recordActionHandlerStub) transitionCall(kind recordcollaboration.ActionMutationKind, request recordcollaboration.ActionTransitionApplicationRequest) (recordcollaboration.ActionMutationResult, error) {
	stub.transitionCalls++
	stub.transition = request
	stub.lastKind = kind
	stub.lastExpectedVersion = request.ExpectedVersion
	return stub.result, stub.err
}
func (stub *recordActionHandlerStub) calls() int {
	return stub.createCalls + stub.updateCalls + stub.transitionCalls + stub.listCalls
}

func testRecordActionActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatal(err)
	}
	return actor
}

func actionStatusForHandlerTest(kind recordcollaboration.ActionMutationKind) recordcollaboration.ActionStatus {
	switch kind {
	case recordcollaboration.ActionMutationComplete:
		return recordcollaboration.ActionStatusCompleted
	case recordcollaboration.ActionMutationCancel:
		return recordcollaboration.ActionStatusCancelled
	default:
		return recordcollaboration.ActionStatusOpen
	}
}

func sameSortedStrings(left, right []string) bool {
	slicesSort(left)
	slicesSort(right)
	return reflect.DeepEqual(left, right)
}

func slicesSort(values []string) {
	for i := range values {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func fmtWrap(sentinel error, secret string) error { return errors.Join(sentinel, errors.New(secret)) }
