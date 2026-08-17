package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationCollaborationProvidersBindCallerTxEpochAndRoundTripRedactedSafeSnapshot(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_providerroundtrip", "provider-roundtrip-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-collaboration-providers", 3)
	actionRepository := NewPostgresRecordActionRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
	)
	action := postgresActionCommand(
		t, parent, recordcollaboration.ActionMutationCreate, "ract_providerroundtrip", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{
			Title: "Portable action", Details: "private portable action details",
		}),
		"provider-action-create",
	)
	if _, err := actionRepository.CommitAction(ctx, action); err != nil {
		t.Fatalf("CommitAction() error = %v", err)
	}
	commentRepository := NewPostgresRecordCommentRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
	)
	createComment := postgresCommentCommand(
		t, parent, recordcollaboration.CommentMutationCreate, "rcm_providerroundtrip", 0,
		"Private portability body.", "", nil, "provider-comment-create",
	)
	if _, err := commentRepository.CommitComment(ctx, createComment); err != nil {
		t.Fatalf("CommitComment(create) error = %v", err)
	}
	redactComment := postgresCommentCommand(
		t, parent, recordcollaboration.CommentMutationRedact, createComment.CommentID, 1,
		"", "", nil, "provider-comment-redact",
	)
	if _, err := commentRepository.CommitComment(ctx, redactComment); err != nil {
		t.Fatalf("CommitComment(redact) error = %v", err)
	}

	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	binding, err := recordcollaboration.NewRecordFenceBinding(
		recordplatform.ProjectIDDefault, parent.RecordID, 0,
	)
	if err != nil {
		t.Fatalf("NewRecordFenceBinding() error = %v", err)
	}
	store := NewPostgresRecordCollaborationProvider()
	activityProvider, err := recordcollaboration.NewActivityProvider(store)
	if err != nil {
		t.Fatalf("NewActivityProvider() error = %v", err)
	}
	facts, err := activityProvider.ListFacts(ctx, tx, binding)
	if err != nil {
		t.Fatalf("ListFacts() error = %v", err)
	}
	if len(facts) < 3 {
		t.Fatalf("ListFacts() = %#v, want revision/action/comment facts", facts)
	}
	for _, fact := range facts {
		if fact.RecordID != parent.RecordID || fact.Validate() != nil {
			t.Fatalf("invalid typed activity fact = %#v", fact)
		}
	}

	portability, err := recordcollaboration.NewPortabilityAdapter(store)
	if err != nil {
		t.Fatalf("NewPortabilityAdapter() error = %v", err)
	}
	snapshot, err := portability.Backup(ctx, tx, binding)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if len(snapshot.Actions) != 1 || snapshot.Actions[0].Details != "private portable action details" ||
		len(snapshot.Comments) != 1 || snapshot.Comments[0].State != recordcollaboration.CommentStateRedacted ||
		snapshot.Comments[0].BodyMarkdown != "" || snapshot.Comments[0].BodyDigest != ([32]byte{}) ||
		len(snapshot.CommentRevisions) != 1 || snapshot.CommentRevisions[0].BodyMarkdown != "" ||
		len(snapshot.Tombstones) != 1 {
		t.Fatalf("Backup() = %#v", snapshot)
	}

	for _, statement := range []string{
		`delete from public.record_comment_mentions where record_id = $1`,
		`delete from public.record_comment_replies where record_id = $1`,
		`delete from public.record_comment_revisions where record_id = $1`,
		`delete from public.record_comment_tombstones where record_id = $1`,
		`delete from public.record_comments where record_id = $1`,
		`delete from public.record_action_events where record_id = $1`,
		`delete from public.record_actions where record_id = $1`,
		`delete from public.record_followers where record_id = $1`,
	} {
		if _, err := tx.Exec(ctx, statement, parent.RecordID); err != nil {
			t.Fatalf("clear restorable collaboration state: %v", err)
		}
	}
	if err := portability.Restore(ctx, tx, binding, snapshot); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	restored, err := portability.Backup(ctx, tx, binding)
	if err != nil {
		t.Fatalf("Backup(restored) error = %v", err)
	}
	if !reflect.DeepEqual(restored, snapshot) {
		t.Fatalf("restored snapshot = %#v, want %#v", restored, snapshot)
	}
	if err := portability.Restore(ctx, tx, binding, snapshot); err != nil {
		t.Fatalf("Restore(idempotent replay) error = %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.record_followers (
			record_id, user_id, follower_version, manual_preference, record_fence_epoch
		) values ($1, 'usr_cccccccccccccccccccccccc', 1, 'watching', 1)`, parent.RecordID,
	); err != nil {
		t.Fatalf("seed stale portability row: %v", err)
	}
	if _, err := portability.Backup(ctx, tx, binding); !errors.Is(err, recordcollaboration.ErrInvalidRecordFenceBinding) {
		t.Fatalf("Backup(stale owned row) error = %v, want ErrInvalidRecordFenceBinding", err)
	}
	if _, err := tx.Exec(ctx, `
		delete from public.record_followers
		where record_id = $1 and user_id = 'usr_cccccccccccccccccccccccc'`, parent.RecordID,
	); err != nil {
		t.Fatalf("remove stale portability row: %v", err)
	}

	stale, err := recordcollaboration.NewRecordFenceBinding(
		recordplatform.ProjectIDDefault, parent.RecordID, 1,
	)
	if err != nil {
		t.Fatalf("NewRecordFenceBinding(stale) error = %v", err)
	}
	if _, err := portability.Backup(ctx, tx, stale); !errors.Is(err, recordcollaboration.ErrInvalidRecordFenceBinding) {
		t.Fatalf("Backup(stale epoch) error = %v, want ErrInvalidRecordFenceBinding", err)
	}
	if _, err := activityProvider.ListFacts(ctx, tx, stale); !errors.Is(err, recordcollaboration.ErrInvalidRecordFenceBinding) {
		t.Fatalf("ListFacts(stale epoch) error = %v, want ErrInvalidRecordFenceBinding", err)
	}
}
