package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
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
	withAudit := snapshot.Clone()
	withAudit.NotificationAudits = append(withAudit.NotificationAudits, recordcollaboration.PortableNotificationAudit{
		NotificationID: "rnt_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Kind:           recordcollaboration.NotificationEventActionCompleted, SubjectKind: recordcollaboration.NotificationSubjectAction,
		SourceVersion: 1, EventAt: snapshot.Actions[0].UpdatedAt,
	})
	if err := portability.Restore(ctx, tx, binding, withAudit); !errors.Is(err, recordcollaboration.ErrInvalidPortabilitySnapshot) {
		t.Fatalf("Restore(non-restorable disclosure audit) error = %v, want ErrInvalidPortabilitySnapshot", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit provider backup transaction: %v", err)
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
		if _, err := fixture.db.Exec(ctx, statement, parent.RecordID); err != nil {
			t.Fatalf("clear restorable collaboration state: %v", err)
		}
	}
	tx, err = runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(restore) error = %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
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
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback stale portability row: %v", err)
	}
	tx, err = runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(fence checks) error = %v", err)
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

	if _, err := tx.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint,
			actor_scope_digest, preview_binding_digest,
			preview_current_revision_id, preview_lock_version,
			preview_authorization_epoch, preview_content_delivery_epoch,
			preview_dependency_graph_digest, preview_backup_inventory_digest,
			preview_processor_inventory_digest, adapter_readiness_digest,
			adapter_preview_digest, preview_witness_sequence,
			preview_witness_entry_hash, state, expires_at, completed_at
		) values (
			'drs_providerfenced', 'default', 'record', $1,
			decode(repeat('41', 32), 'hex'), decode(repeat('42', 32), 'hex'),
			decode(repeat('43', 32), 'hex'), decode(repeat('44', 32), 'hex'),
			$2, $3, $4, 0,
			decode(repeat('45', 32), 'hex'), decode(repeat('46', 32), 'hex'),
			decode(repeat('47', 32), 'hex'), decode(repeat('48', 32), 'hex'),
			decode(repeat('49', 32), 'hex'), 1,
			decode(repeat('4a', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		)`, parent.RecordID, parent.RevisionID, parent.LockVersion, parent.AuthorizationEpoch); err != nil {
		t.Fatalf("seed committed provider deletion reservation: %v", err)
	}
	assertCollaborationProvidersDeletionReserved(t, ctx, activityProvider, portability, tx, binding, snapshot)
	if _, err := tx.Exec(ctx, `delete from public.deletion_reservations where reservation_id = 'drs_providerfenced'`); err != nil {
		t.Fatalf("remove committed provider deletion reservation: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.deletion_fence_leases (
			project_id, object_kind, object_id, owner_id, owner_generation, expires_at, created_at
		) values ('default', 'record', $1, 'provider-fence-owner', 1,
			transaction_timestamp() + interval '5 minutes', transaction_timestamp())`, parent.RecordID); err != nil {
		t.Fatalf("seed live provider deletion fence: %v", err)
	}
	assertCollaborationProvidersDeletionReserved(t, ctx, activityProvider, portability, tx, binding, snapshot)
	if _, err := tx.Exec(ctx, `delete from public.deletion_fence_leases where project_id = 'default' and object_kind = 'record' and object_id = $1`, parent.RecordID); err != nil {
		t.Fatalf("remove live provider deletion fence: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.record_domain_activities (
			activity_id, project_id, record_id, revision_id, event_kind, source_event_id,
			source_version, actor_id, authorization_epoch, record_lock_version, event_at
		) values ('rac_forgedprovider', 'default', $1, $2, 'action_created', 'raev_missingprovider',
			1, 'usr_records1', 1, $3, transaction_timestamp())`, parent.RecordID, parent.RevisionID, int64(parent.LockVersion)); err != nil {
		t.Fatalf("seed forged collaboration activity: %v", err)
	}
	if _, err := activityProvider.ListFacts(ctx, tx, binding); !errors.Is(err, recordcollaboration.ErrInvalidActivityFact) {
		t.Fatalf("ListFacts(forged source) error = %v, want ErrInvalidActivityFact", err)
	}
}

func assertCollaborationProvidersDeletionReserved(
	t *testing.T,
	ctx context.Context,
	activityProvider *recordcollaboration.ActivityProvider,
	portability *recordcollaboration.PortabilityAdapter,
	tx pgx.Tx,
	binding recordcollaboration.RecordFenceBinding,
	snapshot recordcollaboration.PortabilitySnapshot,
) {
	t.Helper()
	if _, err := activityProvider.ListFacts(ctx, tx, binding); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ListFacts(deletion reserved) error = %v, want ErrRecordDeletionReserved", err)
	}
	if _, err := portability.Backup(ctx, tx, binding); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("Backup(deletion reserved) error = %v, want ErrRecordDeletionReserved", err)
	}
	if err := portability.Restore(ctx, tx, binding, snapshot); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("Restore(deletion reserved) error = %v, want ErrRecordDeletionReserved", err)
	}
}
