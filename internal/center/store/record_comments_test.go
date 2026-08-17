package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
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
	var typedNilAuthorization *PostgresCurrentRecordAuthorizationSource
	beginCalls := 0
	repository := NewPostgresRecordCommentRepository(nil, nil, NewPostgresCollaborationMembershipReader(), typedNilAuthorization)
	repository.platform = &PostgresRecordPlatformRepository{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		beginCalls++
		return nil, errors.New("transaction must not begin")
	}}
	if repository == nil {
		t.Fatal("NewPostgresRecordCommentRepository() = nil")
	}
	parent := records.RevisionCommitResult{RecordID: "rec_commentparent1", RevisionID: "rrv_current1", LockVersion: 7, AuthorizationEpoch: 9}
	_, err := repository.CommitComment(context.Background(), postgresCommentCommand(
		t, parent, recordcollaboration.CommentMutationCreate, "rcm_comment1", 0, "closed", "", nil, "comment-typed-nil",
	))
	if !errors.Is(err, recordcollaboration.ErrInvalidCommentCommand) {
		t.Fatalf("CommitComment() error = %v, want typed-nil authorization rejection", err)
	}
	_, err = repository.ListComments(context.Background(), postgresCommentReadCommand(t, parent, 25))
	if !errors.Is(err, recordcollaboration.ErrInvalidCommentRequest) {
		t.Fatalf("ListComments() error = %v, want typed-nil authorization rejection", err)
	}
	if beginCalls != 0 {
		t.Fatalf("begin transaction calls = %d, want 0", beginCalls)
	}
}

func TestPostgresRecordCommentRepositoryRejectsPersistedActorMismatch(t *testing.T) {
	input := mustPostgresCommentActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", recordauth.RoleProjectAdmin)
	tests := []struct {
		name      string
		persisted recordauth.ActorScope
	}{
		{name: "user", persisted: mustPostgresCommentActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", recordauth.RoleProjectAdmin)},
		{name: "project", persisted: recordauth.ActorScope{UserID: input.UserID, Role: input.Role, ProjectID: "other"}},
		{name: "role", persisted: mustPostgresCommentActor(t, input.UserID, recordauth.RoleViewer)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &PostgresRecordCommentRepository{members: &commentMembershipReaderStub{actor: test.persisted}}
			_, _, err := repository.loadCurrentCommentAuthorization(
				context.Background(), nil, input, "rec_commentparent1", recordauth.CapabilityRecordRead,
			)
			if !errors.Is(err, recordauth.ErrDenied) {
				t.Fatalf("loadCurrentCommentAuthorization() error = %v, want ErrDenied", err)
			}
		})
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
		context.Background(), &commentReplyLookupErrorTx{err: dependencyErr}, command, binding, command.AuthorizationEvidence,
	)
	if !errors.Is(err, dependencyErr) {
		t.Fatalf("validateCommentRelationsInTransaction() error = %v, want dependency failure", err)
	}
}

type commentReplyLookupErrorTx struct {
	pgx.Tx
	err error
}

type commentMembershipReaderStub struct {
	actor recordauth.ActorScope
	err   error
}

func (reader *commentMembershipReaderStub) ReadMemberActor(
	context.Context,
	pgx.Tx,
	recordauth.ProjectID,
	string,
) (recordauth.ActorScope, error) {
	return reader.actor, reader.err
}

func (tx *commentReplyLookupErrorTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRecordRevisionRow{err: tx.err}
}
