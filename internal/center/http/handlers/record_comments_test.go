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
	"houfeng/internal/center/recordcollaboration"
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
