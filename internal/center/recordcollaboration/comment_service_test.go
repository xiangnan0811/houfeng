package recordcollaboration

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestCommentServiceCreateBuildsContentBoundCommandAfterCurrentAuthorization(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	current.RecordID = "rec_commentparent1"
	currentSource := &commentCurrentSourceStub{result: current}
	store := &commentCommandStoreStub{result: CommentMutationResult{
		CommentID: "rcm_result1", RecordID: current.RecordID, Version: 1,
		State: CommentStateActive, EventKind: CommentMutationCreate,
		ChangedAt: time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC),
	}}
	service, err := NewCommentService(currentSource, store)
	if err != nil {
		t.Fatalf("NewCommentService() error = %v", err)
	}

	result, err := service.CreateComment(context.Background(), CommentCreateRequest{
		Actor: actor, RecordID: current.RecordID, BodyMarkdown: "Reply **safely**.",
		ReplyToCommentID: "rcm_parent1", MentionUserIDs: []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb", "usr_aaaaaaaaaaaaaaaaaaaaaaaa", "usr_bbbbbbbbbbbbbbbbbbbbbbbb"},
		IdempotencyKey: "comment-create-1", IdempotencyOwnerID: "comment_api", OwnerLeaseDuration: time.Minute,
		IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}
	if result != store.result || currentSource.calls != 1 || store.calls != 1 {
		t.Fatalf("result/calls = %#v current=%d store=%d", result, currentSource.calls, store.calls)
	}
	command := store.command
	if command.Kind != CommentMutationCreate || ValidateCommentID(command.CommentID) != nil || command.ExpectedVersion != 0 ||
		command.RecordID != current.RecordID || command.CurrentRevisionID != current.CurrentRevisionID ||
		command.RecordLockVersion != current.LockVersion || command.AuthorizationEpoch != current.AuthorizationEpoch ||
		command.Content.Source() != "Reply **safely**." || command.Content.Model().Validate() != nil ||
		command.ReplyToCommentID != "rcm_parent1" ||
		!reflect.DeepEqual(command.MentionUserIDs, []string{"usr_aaaaaaaaaaaaaaaaaaaaaaaa", "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}) ||
		command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordCommentCreate ||
		command.Idempotency.RequestFingerprint.Validate() != nil || command.ResultFingerprint.Validate() != nil {
		t.Fatalf("command = %#v", command)
	}
}

func TestCommentServiceRejectsMarkdownOnlyAfterFailClosedCurrentResolution(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	current.RecordID = "rec_commentparent1"
	dependencyErr := errors.New("admission unavailable")
	currentSource := &commentCurrentSourceStub{err: dependencyErr}
	store := &commentCommandStoreStub{}
	service, err := NewCommentService(currentSource, store)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateComment(context.Background(), CommentCreateRequest{
		Actor: actor, RecordID: current.RecordID, BodyMarkdown: "<script>alert(1)</script>",
		IdempotencyKey: "comment-create-1", IdempotencyOwnerID: "comment_api", OwnerLeaseDuration: time.Minute,
		IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	})
	if !errors.Is(err, dependencyErr) || currentSource.calls != 1 || store.calls != 0 {
		t.Fatalf("CreateComment() error/calls = %v current=%d store=%d", err, currentSource.calls, store.calls)
	}
}

func TestCommentServiceNormalizesMalformedReplyOnlyAfterCurrentAuthorization(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	current.RecordID = "rec_commentparent1"
	request := CommentCreateRequest{
		Actor: actor, RecordID: current.RecordID, BodyMarkdown: "Safe reply.", ReplyToCommentID: "not-a-comment-id",
		IdempotencyKey: "comment-create-reply", IdempotencyOwnerID: "comment_api", OwnerLeaseDuration: time.Minute,
		IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	}

	t.Run("authorized malformed reply", func(t *testing.T) {
		currentSource := &commentCurrentSourceStub{result: current}
		store := &commentCommandStoreStub{}
		service, err := NewCommentService(currentSource, store)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.CreateComment(context.Background(), request)
		if !errors.Is(err, ErrInvalidCommentContent) || result != (CommentMutationResult{}) || currentSource.calls != 1 || store.calls != 0 {
			t.Fatalf("CreateComment() result/error/calls = %#v/%v current=%d store=%d", result, err, currentSource.calls, store.calls)
		}
	})

	t.Run("missing record stays opaque", func(t *testing.T) {
		currentSource := &commentCurrentSourceStub{err: records.ErrRecordNotFound}
		store := &commentCommandStoreStub{}
		service, err := NewCommentService(currentSource, store)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.CreateComment(context.Background(), request)
		if !errors.Is(err, records.ErrRecordNotFound) || result != (CommentMutationResult{}) || currentSource.calls != 1 || store.calls != 0 {
			t.Fatalf("CreateComment() result/error/calls = %#v/%v current=%d store=%d", result, err, currentSource.calls, store.calls)
		}
	})
}

func TestCommentServiceEditRedactAndListUseClosedOperations(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	current.RecordID = "rec_commentparent1"
	tests := []struct {
		name      string
		operation recordplatform.OperationKind
		kind      CommentMutationKind
		call      func(*CommentService) error
	}{
		{name: "edit", operation: recordplatform.OperationKindRecordCommentEdit, kind: CommentMutationEdit, call: func(service *CommentService) error {
			_, err := service.EditComment(context.Background(), CommentEditRequest{
				CommentCommandRequest: testCommentCommandRequest(actor, current.RecordID, "rcm_comment1", 4, "comment-edit-1"),
				BodyMarkdown:          "Edited with [safe link](https://example.com/path).",
				MentionUserIDs:        []string{"usr_aaaaaaaaaaaaaaaaaaaaaaaa"},
			})
			return err
		}},
		{name: "redact", operation: recordplatform.OperationKindRecordCommentRedact, kind: CommentMutationRedact, call: func(service *CommentService) error {
			_, err := service.RedactComment(context.Background(), testCommentCommandRequest(actor, current.RecordID, "rcm_comment1", 4, "comment-redact-1"))
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &commentCommandStoreStub{result: CommentMutationResult{
				CommentID: "rcm_comment1", RecordID: current.RecordID, Version: 5,
				State:     map[CommentMutationKind]CommentState{CommentMutationEdit: CommentStateActive, CommentMutationRedact: CommentStateRedacted}[test.kind],
				EventKind: test.kind, ChangedAt: time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC),
			}}
			service, err := NewCommentService(&commentCurrentSourceStub{result: current}, store)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.call(service); err != nil {
				t.Fatal(err)
			}
			if store.command.Kind != test.kind || store.command.Idempotency.Key.OperationKind != test.operation ||
				store.command.ExpectedVersion != 4 || store.command.ResultFingerprint.Validate() != nil {
				t.Fatalf("command = %#v", store.command)
			}
			if test.kind == CommentMutationRedact && !store.command.Content.Empty() {
				t.Fatal("redaction command retained content")
			}
		})
	}

	readStore := &commentCommandStoreStub{listResult: []CommentRecord{{
		CommentID: "rcm_comment1", RecordID: current.RecordID, AuthorID: actor.UserID,
		Version: 1, State: CommentStateActive, BodyMarkdown: "safe",
		RenderModel: mustCommentModel(t, "safe"), CreatedAt: time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 17, 13, 0, 0, 0, time.UTC),
	}}}
	service, err := NewCommentService(&commentCurrentSourceStub{result: current}, readStore)
	if err != nil {
		t.Fatal(err)
	}
	comments, err := service.ListComments(context.Background(), CommentListRequest{Actor: actor, RecordID: current.RecordID, Limit: 100})
	if err != nil || len(comments) != 1 || comments[0].BodyMarkdown != "safe" {
		t.Fatalf("ListComments() = %#v, %v", comments, err)
	}
}

func testCommentCommandRequest(actor recordauth.ActorScope, recordID, commentID string, version uint64, key string) CommentCommandRequest {
	return CommentCommandRequest{
		Actor: actor, RecordID: recordID, CommentID: commentID, ExpectedVersion: version,
		IdempotencyKey: key, IdempotencyOwnerID: "comment_api", OwnerLeaseDuration: time.Minute,
		IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	}
}

func mustCommentModel(t *testing.T, source string) CommentRenderModel {
	t.Helper()
	model, err := ParseCommentMarkdownV1(source)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

type commentCurrentSourceStub struct {
	result records.CurrentRecordAuthorization
	err    error
	calls  int
}

func (source *commentCurrentSourceStub) ResolveCurrentRecordAuthorization(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error) {
	source.calls++
	return source.result, source.err
}

type commentCommandStoreStub struct {
	command    CommentCommand
	result     CommentMutationResult
	err        error
	calls      int
	listResult []CommentRecord
	listErr    error
}

func (store *commentCommandStoreStub) CommitComment(_ context.Context, command CommentCommand) (CommentMutationResult, error) {
	store.calls++
	store.command = command
	return store.result, store.err
}

func (store *commentCommandStoreStub) ListComments(context.Context, CommentReadCommand) ([]CommentRecord, error) {
	return append([]CommentRecord(nil), store.listResult...), store.listErr
}

type commentReadStoreStub struct {
	result []CommentRecord
	err    error
	calls  int
}

func (store *commentReadStoreStub) ListComments(_ context.Context, _ CommentReadCommand) ([]CommentRecord, error) {
	store.calls++
	return append([]CommentRecord(nil), store.result...), store.err
}
