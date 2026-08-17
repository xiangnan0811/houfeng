package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

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
	actionRepository := newPostgresActionRepositoryForTest(t, runtimePool, allowRecordPlatformAdmissionGate)
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
	completeAction := postgresActionCommand(
		t, parent, recordcollaboration.ActionMutationComplete, action.ActionID, 1,
		recordcollaboration.ActionFields{}, "provider-action-complete",
	)
	if _, err := actionRepository.CommitAction(ctx, completeAction); err != nil {
		t.Fatalf("CommitAction(complete) error = %v", err)
	}
	commentRepository := newPostgresCommentRepositoryForTest(t, runtimePool, allowRecordPlatformAdmissionGate)
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
	if _, err := tx.Exec(ctx, `
		insert into public.record_notifications (
			notification_id, record_id, event_kind, subject_kind, subject_id,
			source_version, actor_id, authorization_epoch, record_fence_epoch,
			event_at, details_delete_after
		) values ('rnt_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', $1,
			'action_completed', 'action', 'ract_providerroundtrip', 1, 'usr_records1',
			$2, 0, transaction_timestamp(), transaction_timestamp() + interval '1 day')`,
		parent.RecordID, int64(parent.AuthorizationEpoch)); err != nil {
		t.Fatalf("seed portable notification audit: %v", err)
	}
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
	countedActivityTx := &collaborationProviderCountingTx{Tx: tx}
	facts, err := activityProvider.ListFacts(ctx, countedActivityTx, binding)
	if err != nil {
		t.Fatalf("ListFacts() error = %v", err)
	}
	if len(facts) < 3 {
		t.Fatalf("ListFacts() = %#v, want revision/action/comment facts", facts)
	}
	if countedActivityTx.queryCalls != 1 || countedActivityTx.queryRowCalls != 5 {
		t.Fatalf("ListFacts() SQL calls query/query-row = %d/%d, want fixed 1/5", countedActivityTx.queryCalls, countedActivityTx.queryRowCalls)
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
	if len(snapshot.Actions) != 1 || snapshot.Actions[0].Version != 2 ||
		snapshot.Actions[0].Status != recordcollaboration.ActionStatusCompleted ||
		snapshot.Actions[0].Details != "private portable action details" || len(snapshot.ActionEvents) != 2 ||
		len(snapshot.Comments) != 1 || snapshot.Comments[0].State != recordcollaboration.CommentStateRedacted ||
		snapshot.Comments[0].BodyMarkdown != "" || snapshot.Comments[0].BodyDigest != ([32]byte{}) ||
		len(snapshot.CommentRevisions) != 1 || snapshot.CommentRevisions[0].BodyMarkdown != "" ||
		len(snapshot.Tombstones) != 1 || len(snapshot.NotificationAudits) != 1 ||
		snapshot.NotificationAudits[0].NotificationID != "rnt_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("Backup() = %#v", snapshot)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit provider backup transaction: %v", err)
	}

	for _, statement := range []string{
		`delete from public.record_notification_delivery_attempts where record_id = $1`,
		`delete from public.record_notification_deliveries where record_id = $1`,
		`delete from public.record_notification_recipients where record_id = $1`,
		`delete from public.record_notifications where record_id = $1`,
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
	for name, mutate := range map[string]func(*recordcollaboration.PortabilitySnapshot){
		"sparse action history":  func(candidate *recordcollaboration.PortabilitySnapshot) { candidate.Actions[0].Version += 4 },
		"sparse comment history": func(candidate *recordcollaboration.PortabilitySnapshot) { candidate.Comments[0].Version += 4 },
	} {
		t.Run(name, func(t *testing.T) {
			invalidTx, err := runtimePool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			candidate := snapshot.Clone()
			mutate(&candidate)
			if err := portability.Restore(ctx, invalidTx, binding, candidate); !errors.Is(err, recordcollaboration.ErrInvalidPortabilitySnapshot) {
				t.Fatalf("Restore(malformed aggregate) error = %v, want ErrInvalidPortabilitySnapshot", err)
			}
			var actionCount, eventCount, commentCount, revisionCount int
			if err := invalidTx.QueryRow(ctx, `
				select
				  (select count(*)::int from public.record_actions where record_id = $1),
				  (select count(*)::int from public.record_action_events where record_id = $1),
				  (select count(*)::int from public.record_comments where record_id = $1),
				  (select count(*)::int from public.record_comment_revisions where record_id = $1)`,
				parent.RecordID,
			).Scan(&actionCount, &eventCount, &commentCount, &revisionCount); err != nil {
				t.Fatal(err)
			}
			if actionCount != 0 || eventCount != 0 || commentCount != 0 || revisionCount != 0 {
				t.Fatalf("malformed restore left action/event/comment/revision rows = %d/%d/%d/%d", actionCount, eventCount, commentCount, revisionCount)
			}
			if err := invalidTx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		})
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

func TestPostgresIntegrationCollaborationActivityRejectsOverCapWithFixedSQLCalls(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_provideractivitycap", "provider-activity-cap-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-collaboration-activity-cap", 2)
	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		insert into public.record_domain_activities (
		  activity_id, project_id, record_id, revision_id, event_kind, source_event_id,
		  source_version, actor_id, authorization_epoch, record_lock_version, event_at
		)
		select 'rac_overcap' || lpad(value::text, 5, '0'), 'default', $1, $2,
		       'action_created', 'raev_overcap' || lpad(value::text, 5, '0'),
		       1, 'usr_records1', $3, $4, transaction_timestamp() + value * interval '1 microsecond'
		from generate_series(1, $5::int) as value`,
		parent.RecordID, parent.RevisionID, int64(parent.AuthorizationEpoch), int64(parent.LockVersion),
		recordcollaboration.MaxCollaborationActivityFacts+1,
	); err != nil {
		t.Fatalf("seed over-cap activity rows: %v", err)
	}
	binding, err := recordcollaboration.NewRecordFenceBinding(recordplatform.ProjectIDDefault, parent.RecordID, 0)
	if err != nil {
		t.Fatal(err)
	}
	activityProvider, err := recordcollaboration.NewActivityProvider(NewPostgresRecordCollaborationProvider())
	if err != nil {
		t.Fatal(err)
	}
	countedTx := &collaborationProviderCountingTx{Tx: tx}
	if _, err := activityProvider.ListFacts(ctx, countedTx, binding); !errors.Is(err, recordcollaboration.ErrInvalidActivityFact) {
		t.Fatalf("ListFacts(over cap) error = %v, want ErrInvalidActivityFact", err)
	}
	if countedTx.queryCalls != 1 || countedTx.queryRowCalls != 5 {
		t.Fatalf("ListFacts(over cap) SQL calls query/query-row = %d/%d, want fixed 1/5", countedTx.queryCalls, countedTx.queryRowCalls)
	}
}

type collaborationProviderCountingTx struct {
	pgx.Tx
	queryCalls    int
	queryRowCalls int
}

func (tx *collaborationProviderCountingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.queryCalls++
	return tx.Tx.Query(ctx, sql, args...)
}

func (tx *collaborationProviderCountingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.queryRowCalls++
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestPostgresIntegrationCollaborationAuditRestoreRejectsConflictAndStaleEpochWithoutMutation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_providerauditconflict", "provider-audit-conflict-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-collaboration-audit-conflict", 2)
	binding, err := recordcollaboration.NewRecordFenceBinding(recordplatform.ProjectIDDefault, parent.RecordID, 0)
	if err != nil {
		t.Fatal(err)
	}
	audit := recordcollaboration.PortableNotificationAudit{
		NotificationID: "rnt_dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Kind:           recordcollaboration.NotificationEventActionCompleted, SubjectKind: recordcollaboration.NotificationSubjectAction,
		SourceVersion: 2, EventAt: time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
		RecipientCount: 2, DeliveryCount: 2, SentCount: 1, UnknownCount: 1,
	}
	snapshot := emptyCollaborationPortabilitySnapshot()
	snapshot.NotificationAudits = append(snapshot.NotificationAudits, audit)
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("audit snapshot invalid: %v", err)
	}
	portability, err := recordcollaboration.NewPortabilityAdapter(NewPostgresRecordCollaborationProvider())
	if err != nil {
		t.Fatal(err)
	}
	seed := func(fence, recipientCount int64) {
		t.Helper()
		if _, err := fixture.db.Exec(ctx, `
			insert into public.record_notification_audit_summaries (
				notification_id, record_id, event_kind, subject_kind, source_version, event_at,
				recipient_count, delivery_count, sent_count, unknown_count, permanent_failed_count,
				record_fence_epoch
			) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			audit.NotificationID, parent.RecordID, audit.Kind, audit.SubjectKind, int64(audit.SourceVersion), audit.EventAt,
			recipientCount, int64(audit.DeliveryCount), int64(audit.SentCount), int64(audit.UnknownCount), int64(audit.PermanentFailed), fence,
		); err != nil {
			t.Fatalf("seed notification audit summary: %v", err)
		}
	}
	seed(0, 1)
	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := portability.Restore(ctx, tx, binding, snapshot); !errors.Is(err, recordcollaboration.ErrInvalidPortabilitySnapshot) {
		t.Fatalf("Restore(conflicting audit) error = %v, want ErrInvalidPortabilitySnapshot", err)
	}
	_ = tx.Rollback(ctx)
	var recipientCount, rows int64
	if err := fixture.db.QueryRow(ctx, `
		select recipient_count, count(*) over ()
		from public.record_notification_audit_summaries where notification_id = $1`, audit.NotificationID,
	).Scan(&recipientCount, &rows); err != nil || recipientCount != 1 || rows != 1 {
		t.Fatalf("conflicting audit changed = recipient/rows %d/%d error %v", recipientCount, rows, err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.record_notification_audit_summaries where notification_id = $1`, audit.NotificationID); err != nil {
		t.Fatal(err)
	}
	seed(1, 2)
	tx, err = runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := portability.Restore(ctx, tx, binding, snapshot); !errors.Is(err, recordcollaboration.ErrInvalidRecordFenceBinding) {
		t.Fatalf("Restore(stale audit epoch) error = %v, want ErrInvalidRecordFenceBinding", err)
	}
	_ = tx.Rollback(ctx)
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
