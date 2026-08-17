package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/records"
)

func TestRecordCommentsHandlerUsesTrustedActorAndClosedMutationResponse(t *testing.T) {
	actor := testRecordActionActor(t)
	changedAt := time.Date(2026, 8, 17, 15, 0, 0, 0, time.UTC)
	application := &recordCommentHandlerStub{result: recordcollaboration.CommentMutationResult{
		CommentID: "rcm_comment1", RecordID: "rec_commentparent1", Version: 1,
		State: recordcollaboration.CommentStateActive, EventKind: recordcollaboration.CommentMutationCreate, ChangedAt: changedAt,
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/records/rec_commentparent1/comments",
		strings.NewReader(`{"body_markdown":"Safe **comment**.","reply_to_comment_id":"","mention_user_ids":["usr_bbbbbbbbbbbbbbbbbbbbbbbb"]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "comment-create-1")
	request.Header.Set("X-Role", "forged")
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	RecordComments(application).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl || recorder.Header().Get("ETag") != `"1"` {
		t.Fatalf("response status/headers = %d %#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if application.createCalls != 1 || application.create.Actor.UserID != actor.UserID ||
		application.create.RecordID != "rec_commentparent1" || application.create.IdempotencyKey != "comment-create-1" ||
		application.create.BodyMarkdown != "Safe **comment**." ||
		!reflect.DeepEqual(application.create.MentionUserIDs, []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}) {
		t.Fatalf("application request = %#v calls=%d", application.create, application.createCalls)
	}
	assertRecordCommentJSONKeys(t, recorder.Body.Bytes(), []string{
		"changed_at", "comment_id", "event_kind", "record_id", "replayed", "state", "version",
	})
	for _, forbidden := range []string{"body_markdown", "render_model", "authorization_epoch", "request_fingerprint", "result_fingerprint"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("mutation response leaks forbidden field %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestRecordCommentsHandlerReadAllowlistReturnsOnlyControlledRenderModel(t *testing.T) {
	actor := testRecordActionActor(t)
	model, err := recordcollaboration.ParseCommentMarkdownV1("Safe **comment**.")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 17, 15, 1, 0, 0, time.UTC)
	application := &recordCommentHandlerStub{comments: []recordcollaboration.CommentRecord{{
		CommentID: "rcm_comment1", RecordID: "rec_commentparent1", AuthorID: actor.UserID,
		Version: 1, State: recordcollaboration.CommentStateActive, BodyMarkdown: "Safe **comment**.", RenderModel: model,
		MentionUserIDs: []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}, CreatedAt: now, UpdatedAt: now,
	}}}
	request := httptest.NewRequest(http.MethodGet, "/api/records/rec_commentparent1/comments?limit=50", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	RecordComments(application).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || application.listCalls != 1 || application.list.Limit != 50 {
		t.Fatalf("status=%d calls=%d request=%#v body=%s", recorder.Code, application.listCalls, application.list, recorder.Body.String())
	}
	var body struct {
		Comments []json.RawMessage `json:"comments"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || len(body.Comments) != 1 {
		t.Fatalf("decode read response: %v body=%s", err, recorder.Body.String())
	}
	assertRecordCommentJSONKeys(t, body.Comments[0], []string{
		"author_id", "body_markdown", "comment_id", "created_at", "mention_user_ids", "record_id",
		"redacted_at", "render_model", "reply_to_comment_id", "state", "updated_at", "version",
	})
	for _, forbidden := range []string{"body_digest", "record_fence_epoch", "authorization_epoch", "tombstone_id", "edited_by"} {
		if bytes.Contains(recorder.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("read response leaks forbidden field %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestRecordCommentsHandlerSerializesRedactedContentAsNullAndMentionsAsArray(t *testing.T) {
	actor := testRecordActionActor(t)
	now := time.Date(2026, 8, 17, 15, 2, 0, 0, time.UTC)
	application := &recordCommentHandlerStub{comments: []recordcollaboration.CommentRecord{{
		CommentID: "rcm_comment1", RecordID: "rec_commentparent1", AuthorID: actor.UserID,
		Version: 2, State: recordcollaboration.CommentStateRedacted, CreatedAt: now, UpdatedAt: now, RedactedAt: &now,
	}}}
	request := httptest.NewRequest(http.MethodGet, "/api/records/rec_commentparent1/comments", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	RecordComments(application).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || len(response.Comments) != 1 {
		t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
	}
	comment := response.Comments[0]
	if comment == nil || comment["body_markdown"] != nil || comment["render_model"] != nil ||
		!reflect.DeepEqual(comment["mention_user_ids"], []any{}) {
		t.Fatalf("redacted response = %#v", comment)
	}
}

func TestRecordCommentsHandlerRoutesEditRedactAndMapsMarkdownSentinel(t *testing.T) {
	actor := testRecordActionActor(t)
	for _, test := range []struct {
		name, method, path, body, key string
		kind                          recordcollaboration.CommentMutationKind
	}{
		{name: "edit", method: http.MethodPatch, path: "/api/records/rec_commentparent1/comments/rcm_comment1", body: `{"body_markdown":"Edited.","mention_user_ids":[]}`, key: "comment-edit", kind: recordcollaboration.CommentMutationEdit},
		{name: "redact", method: http.MethodPost, path: "/api/records/rec_commentparent1/comments/rcm_comment1/redact", body: `{}`, key: "comment-redact", kind: recordcollaboration.CommentMutationRedact},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := recordcollaboration.CommentStateActive
			if test.kind == recordcollaboration.CommentMutationRedact {
				state = recordcollaboration.CommentStateRedacted
			}
			application := &recordCommentHandlerStub{result: recordcollaboration.CommentMutationResult{
				CommentID: "rcm_comment1", RecordID: "rec_commentparent1", Version: 2,
				State: state, EventKind: test.kind, ChangedAt: time.Now().UTC(),
			}}
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", test.key)
			request.Header.Set("If-Match", `"1"`)
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordComments(application).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || recorder.Header().Get("ETag") != `"2"` {
				t.Fatalf("status=%d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			if test.kind == recordcollaboration.CommentMutationEdit && (application.editCalls != 1 || application.edit.ExpectedVersion != 1) {
				t.Fatalf("edit request/calls = %#v/%d", application.edit, application.editCalls)
			}
			if test.kind == recordcollaboration.CommentMutationRedact && (application.redactCalls != 1 || application.redact.ExpectedVersion != 1) {
				t.Fatalf("redact request/calls = %#v/%d", application.redact, application.redactCalls)
			}
		})
	}

	application := &recordCommentHandlerStub{err: recordcollaboration.ErrInvalidCommentMarkdown}
	request := httptest.NewRequest(http.MethodPost, "/api/records/rec_commentparent1/comments", strings.NewReader(`{"body_markdown":"<script>x</script>"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "comment-invalid")
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	RecordComments(application).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), `"code":"invalid_comment_markdown"`) {
		t.Fatalf("markdown error status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRecordCommentsHandlerMapsMalformedReplyAfterOpaqueAuthorization(t *testing.T) {
	actor := testRecordActionActor(t)
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS,
		SourceID: "vps_0123456789abcdef", State: recordauth.SourceStateLive,
		CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatal(err)
	}
	currentSource := &recordCommentHandlerCurrentSourceStub{result: records.CurrentRecordAuthorization{
		RecordID: "rec_commentparent1", CurrentRevisionID: "rrv_current1", LockVersion: 7,
		AuthorizationEpoch: 9, Lifecycle: records.LifecycleActive,
		Evidence: records.RecordAuthorizationEvidence{
			ProjectID: recordauth.ProjectIDDefault, Visibility: visibility, Sources: []recordauth.SourceAuthorization{source},
		},
	}}
	store := &recordCommentHandlerStoreStub{}
	service, err := recordcollaboration.NewCommentService(currentSource, store)
	if err != nil {
		t.Fatal(err)
	}
	application, err := recordcollaboration.NewCommentApplication(service, recordcollaboration.CommentApplicationOptions{
		IdempotencyOwnerID: "record_comments_api", OwnerLeaseDuration: time.Minute,
		IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/records/rec_commentparent1/comments",
		strings.NewReader(`{"body_markdown":"Safe reply.","reply_to_comment_id":"not-a-comment-id","mention_user_ids":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "comment-malformed-reply")
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	RecordComments(application).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnprocessableEntity || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl ||
		!strings.Contains(recorder.Body.String(), `"code":"comment_invalid"`) || currentSource.calls != 1 || store.calls != 0 {
		t.Fatalf("status=%d headers=%#v current/store=%d/%d body=%s", recorder.Code, recorder.Header(), currentSource.calls, store.calls, recorder.Body.String())
	}
}

func TestRecordCommentsHandlerRejectsInvalidCommentUnicodeBeforeApplication(t *testing.T) {
	actor := testRecordActionActor(t)
	invalidUTF8 := append([]byte(`{"body_markdown":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	for _, test := range []struct {
		name, code string
		body       []byte
		status     int
	}{
		{name: "raw invalid UTF-8", body: invalidUTF8, status: http.StatusUnprocessableEntity, code: "invalid_comment_markdown"},
		{name: "lone escaped surrogate", body: []byte(`{"body_markdown":"\ud800"}`), status: http.StatusUnprocessableEntity, code: "invalid_comment_markdown"},
		{name: "isolated low surrogate", body: []byte(`{"body_markdown":"\udc00"}`), status: http.StatusUnprocessableEntity, code: "invalid_comment_markdown"},
		{name: "malformed unicode escape", body: []byte(`{"body_markdown":"\uZZZZ"}`), status: http.StatusBadRequest, code: "invalid_json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := &recordCommentHandlerStub{}
			request := httptest.NewRequest(http.MethodPost, "/api/records/rec_commentparent1/comments", bytes.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "comment-invalid-unicode")
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordComments(application).ServeHTTP(recorder, request)

			if recorder.Code != test.status || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl ||
				!strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status=%d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			if application.createCalls != 0 || application.editCalls != 0 || application.redactCalls != 0 || application.listCalls != 0 {
				t.Fatalf("application calls = create:%d edit:%d redact:%d list:%d", application.createCalls, application.editCalls, application.redactCalls, application.listCalls)
			}
		})
	}
}

func TestRecordCommentsHandlerAcceptsPairedEscapedSurrogate(t *testing.T) {
	actor := testRecordActionActor(t)
	application := &recordCommentHandlerStub{result: recordcollaboration.CommentMutationResult{
		CommentID: "rcm_comment1", RecordID: "rec_commentparent1", Version: 1,
		State: recordcollaboration.CommentStateActive, EventKind: recordcollaboration.CommentMutationCreate, ChangedAt: time.Now().UTC(),
	}}
	request := httptest.NewRequest(http.MethodPost, "/api/records/rec_commentparent1/comments",
		strings.NewReader(`{"body_markdown":"Safe \ud83d\ude00","reply_to_comment_id":"","mention_user_ids":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "comment-valid-unicode")
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	RecordComments(application).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || application.createCalls != 1 || application.create.BodyMarkdown != "Safe 😀" {
		t.Fatalf("status=%d calls=%d markdown=%q body=%s", recorder.Code, application.createCalls, application.create.BodyMarkdown, recorder.Body.String())
	}
}

func TestRecordCommentsHandlerPreservesJSONBoundAndStrictness(t *testing.T) {
	actor := testRecordActionActor(t)
	for _, test := range []struct {
		name, body, code string
		status           int
	}{
		{name: "body bound", body: `{"body_markdown":"` + strings.Repeat("a", DefaultJSONBodyLimit) + `"}`, status: http.StatusRequestEntityTooLarge, code: "request_too_large"},
		{name: "unknown field", body: `{"body_markdown":"Safe.","html":"<b>unsafe</b>"}`, status: http.StatusBadRequest, code: "invalid_json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := &recordCommentHandlerStub{}
			request := httptest.NewRequest(http.MethodPost, "/api/records/rec_commentparent1/comments", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "comment-json-bound")
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordComments(application).ServeHTTP(recorder, request)

			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), `"code":"`+test.code+`"`) || application.createCalls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", recorder.Code, application.createCalls, recorder.Body.String())
			}
		})
	}
}

func TestRecordCommentsHandlerKeepsPolicyDenialOpaqueFromMissingComment(t *testing.T) {
	actor := testRecordActionActor(t)
	responses := make([][]byte, 0, 2)
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "policy denied", err: recordcollaboration.ErrCommentPolicyDenied},
		{name: "comment missing", err: recordcollaboration.ErrCommentNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			application := &recordCommentHandlerStub{err: test.err}
			request := httptest.NewRequest(http.MethodGet, "/api/records/rec_commentparent1/comments", nil)
			request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
			recorder := httptest.NewRecorder()
			RecordComments(application).ServeHTTP(recorder, request)
			responses = append(responses, append([]byte(nil), recorder.Body.Bytes()...))

			if recorder.Code != http.StatusNotFound || recorder.Header().Get("Cache-Control") != recordPrivateCacheControl ||
				!strings.Contains(recorder.Body.String(), `"code":"resource_not_found"`) {
				t.Fatalf("status=%d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
		})
	}
	if !bytes.Equal(responses[0], responses[1]) {
		t.Fatalf("policy and missing responses differ: %s != %s", responses[0], responses[1])
	}
}

type recordCommentHandlerStub struct {
	create                                         recordcollaboration.CommentCreateApplicationRequest
	edit                                           recordcollaboration.CommentEditApplicationRequest
	redact                                         recordcollaboration.CommentRedactApplicationRequest
	list                                           recordcollaboration.CommentListApplicationRequest
	result                                         recordcollaboration.CommentMutationResult
	comments                                       []recordcollaboration.CommentRecord
	err                                            error
	createCalls, editCalls, redactCalls, listCalls int
}

type recordCommentHandlerCurrentSourceStub struct {
	result records.CurrentRecordAuthorization
	err    error
	calls  int
}

func (stub *recordCommentHandlerCurrentSourceStub) ResolveCurrentRecordAuthorization(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error) {
	stub.calls++
	return stub.result, stub.err
}

type recordCommentHandlerStoreStub struct {
	calls int
}

func (stub *recordCommentHandlerStoreStub) CommitComment(context.Context, recordcollaboration.CommentCommand) (recordcollaboration.CommentMutationResult, error) {
	stub.calls++
	return recordcollaboration.CommentMutationResult{}, nil
}

func (*recordCommentHandlerStoreStub) ListComments(context.Context, recordcollaboration.CommentReadCommand) ([]recordcollaboration.CommentRecord, error) {
	return nil, nil
}

func (stub *recordCommentHandlerStub) CreateComment(_ context.Context, request recordcollaboration.CommentCreateApplicationRequest) (recordcollaboration.CommentMutationResult, error) {
	stub.createCalls++
	stub.create = request
	return stub.result, stub.err
}

func (stub *recordCommentHandlerStub) EditComment(_ context.Context, request recordcollaboration.CommentEditApplicationRequest) (recordcollaboration.CommentMutationResult, error) {
	stub.editCalls++
	stub.edit = request
	return stub.result, stub.err
}

func (stub *recordCommentHandlerStub) RedactComment(_ context.Context, request recordcollaboration.CommentRedactApplicationRequest) (recordcollaboration.CommentMutationResult, error) {
	stub.redactCalls++
	stub.redact = request
	return stub.result, stub.err
}

func (stub *recordCommentHandlerStub) ListComments(_ context.Context, request recordcollaboration.CommentListApplicationRequest) ([]recordcollaboration.CommentRecord, error) {
	stub.listCalls++
	stub.list = request
	return append([]recordcollaboration.CommentRecord(nil), stub.comments...), stub.err
}

func assertRecordCommentJSONKeys(t *testing.T, raw []byte, want []string) {
	t.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, raw)
	}
	got := make([]string, 0, len(decoded))
	for key := range decoded {
		got = append(got, key)
	}
	if !sameSortedStrings(got, want) {
		t.Fatalf("JSON keys=%#v want=%#v body=%s", got, want, raw)
	}
}
