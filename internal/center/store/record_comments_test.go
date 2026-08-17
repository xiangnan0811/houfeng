package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
)

func TestRecordCommentNotificationOutboxKindsUseExplicitReplyAndMentionFacts(t *testing.T) {
	tests := []struct {
		name     string
		kind     recordcollaboration.CommentMutationKind
		reply    bool
		mentions bool
		want     []string
	}{
		{name: "plain create", kind: recordcollaboration.CommentMutationCreate, want: []string{recordplatform.OutboxEventKindRecordCommentCreated}},
		{name: "reply and mention create", kind: recordcollaboration.CommentMutationCreate, reply: true, mentions: true, want: []string{recordplatform.OutboxEventKindRecordCommentCreated, recordplatform.OutboxEventKindRecordCommentReplied, recordplatform.OutboxEventKindRecordCommentMentioned}},
		{name: "mention edit", kind: recordcollaboration.CommentMutationEdit, mentions: true, want: []string{recordplatform.OutboxEventKindRecordCommentEdited, recordplatform.OutboxEventKindRecordCommentMentioned}},
		{name: "redact", kind: recordcollaboration.CommentMutationRedact, want: []string{recordplatform.OutboxEventKindRecordCommentRedacted}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recordCommentNotificationOutboxKinds(tt.kind, tt.reply, tt.mentions); !slices.Equal(got, tt.want) {
				t.Fatalf("recordCommentNotificationOutboxKinds() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPostgresRecordCommentRepositoryFailsClosedWithMissingAdmission(t *testing.T) {
	repository := NewPostgresRecordCommentRepository(nil, nil, NewPostgresCollaborationMembershipReader())
	if repository == nil {
		t.Fatal("NewPostgresRecordCommentRepository() = nil")
	}
	_, err := repository.CommitComment(context.Background(), recordcollaboration.CommentCommand{})
	if !errors.Is(err, recordcollaboration.ErrInvalidCommentCommand) {
		t.Fatalf("CommitComment() error = %v, want invalid command before SQL", err)
	}
	_, err = repository.ListComments(context.Background(), recordcollaboration.CommentReadCommand{})
	if !errors.Is(err, recordcollaboration.ErrInvalidCommentRequest) {
		t.Fatalf("ListComments() error = %v, want invalid request before SQL", err)
	}
}

func TestPostgresRecordCommentRepositoryPreservesReplyLookupDependencyFailure(t *testing.T) {
	dependencyErr := errors.New("reply lookup unavailable")
	repository := &PostgresRecordCommentRepository{members: &recordActionMembershipStub{}}
	command := recordcollaboration.CommentCommand{
		RecordID: "rec_commentparent1", CommentID: "rcm_comment1", ReplyToCommentID: "rcm_parent1",
	}
	binding, err := recordcollaboration.NewRecordFenceBinding(recordplatform.ProjectIDDefault, command.RecordID, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = repository.validateCommentRelationsInTransaction(
		context.Background(), &commentReplyLookupErrorTx{err: dependencyErr}, command, binding,
	)
	if !errors.Is(err, dependencyErr) {
		t.Fatalf("validateCommentRelationsInTransaction() error = %v, want dependency failure", err)
	}
}

type commentReplyLookupErrorTx struct {
	pgx.Tx
	err error
}

func (tx *commentReplyLookupErrorTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRecordRevisionRow{err: tx.err}
}
