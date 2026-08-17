package store

import (
	"context"
	"crypto/sha256"
	"reflect"
	"testing"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationRecordCollaborationDeletionPurgesExactOwnedSurfacesAndReplays(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_collabdelete", "collaboration-deletion-parent")
	digest := sha256.Sum256([]byte("collaboration deletion private body"))

	statements := []struct {
		sql  string
		args []any
	}{
		{`insert into public.record_actions (
			action_id, record_id, action_version, title, details, status,
			created_by, updated_by, record_fence_epoch
		) values ('ract_collabdelete', $1, 1, 'Delete action', 'private details', 'open',
		  'usr_records1', 'usr_records1', 0)`, []any{parent.RecordID}},
		{`insert into public.record_action_events (
			action_event_id, record_id, action_id, action_version, event_kind,
			current_status, actor_id, record_fence_epoch, occurred_at
		) values ('raev_collabdelete', $1, 'ract_collabdelete', 1, 'created',
		  'open', 'usr_records1', 0, transaction_timestamp())`, []any{parent.RecordID}},
		{`insert into public.record_comments (
			comment_id, record_id, author_id, comment_version, body_markdown,
			render_contract_version, render_model, body_digest, record_fence_epoch
		) values ('rcm_collabparent', $1, 'usr_records1', 1, 'private parent',
		  'comment_markdown/v1', '{}'::jsonb, $2, 0)`, []any{parent.RecordID, digest[:]}},
		{`insert into public.record_comment_revisions (
			comment_revision_id, record_id, comment_id, comment_version, edited_by,
			body_markdown, render_contract_version, render_model, body_digest, record_fence_epoch
		) values ('rcr_collabparent', $1, 'rcm_collabparent', 1, 'usr_records1',
		  'private parent', 'comment_markdown/v1', '{}'::jsonb, $2, 0)`, []any{parent.RecordID, digest[:]}},
		{`insert into public.record_comments (
			comment_id, record_id, author_id, comment_version, body_markdown,
			render_contract_version, render_model, body_digest, record_fence_epoch
		) values ('rcm_collabchild', $1, 'usr_records1', 1, 'private child',
		  'comment_markdown/v1', '{}'::jsonb, $2, 0)`, []any{parent.RecordID, digest[:]}},
		{`insert into public.record_comment_revisions (
			comment_revision_id, record_id, comment_id, comment_version, edited_by,
			body_markdown, render_contract_version, render_model, body_digest, record_fence_epoch
		) values ('rcr_collabchild', $1, 'rcm_collabchild', 1, 'usr_records1',
		  'private child', 'comment_markdown/v1', '{}'::jsonb, $2, 0)`, []any{parent.RecordID, digest[:]}},
		{`insert into public.record_comment_replies (
			record_id, child_comment_id, parent_comment_id, record_fence_epoch
		) values ($1, 'rcm_collabchild', 'rcm_collabparent', 0)`, []any{parent.RecordID}},
		{`insert into public.record_comment_mentions (
			record_id, comment_id, comment_version, mentioned_user_id, record_fence_epoch
		) values ($1, 'rcm_collabchild', 1, 'usr_bbbbbbbbbbbbbbbbbbbbbbbb', 0)`, []any{parent.RecordID}},
		{`insert into public.record_comments (
			comment_id, record_id, author_id, comment_version, body_markdown,
			render_contract_version, render_model, body_digest, record_fence_epoch
		) values ('rcm_collabredacted', $1, 'usr_records1', 1, 'must disappear',
		  'comment_markdown/v1', '{}'::jsonb, $2, 0)`, []any{parent.RecordID, digest[:]}},
		{`insert into public.record_comment_revisions (
			comment_revision_id, record_id, comment_id, comment_version, edited_by,
			body_markdown, render_contract_version, render_model, body_digest, record_fence_epoch
		) values ('rcr_collabredacted', $1, 'rcm_collabredacted', 1, 'usr_records1',
		  'must disappear', 'comment_markdown/v1', '{}'::jsonb, $2, 0)`, []any{parent.RecordID, digest[:]}},
		{`insert into public.record_comment_tombstones (
			tombstone_id, record_id, comment_id, tombstone_version, deleted_by,
			reason_code, deleted_at, record_fence_epoch
		) values ('rct_collabredacted', $1, 'rcm_collabredacted', 2, 'usr_records1',
		  'author_deleted', transaction_timestamp(), 0)`, []any{parent.RecordID}},
		{`update public.record_comment_revisions
		set body_markdown = null, render_contract_version = null, render_model = null,
		    body_digest = null, tombstone_id = 'rct_collabredacted',
		    redacted_at = (select deleted_at from public.record_comment_tombstones where tombstone_id = 'rct_collabredacted')
		where comment_revision_id = 'rcr_collabredacted'`, nil},
		{`update public.record_comments
		set comment_state = 'redacted', comment_version = 2, body_markdown = null,
		    render_contract_version = null, render_model = null, body_digest = null,
		    tombstone_id = 'rct_collabredacted',
		    redacted_at = (select deleted_at from public.record_comment_tombstones where tombstone_id = 'rct_collabredacted'),
		    updated_at = (select deleted_at from public.record_comment_tombstones where tombstone_id = 'rct_collabredacted')
		where comment_id = 'rcm_collabredacted'`, nil},
		{`insert into public.record_followers (
			record_id, user_id, follower_version, manual_preference, record_fence_epoch
		) values ($1, 'usr_records1', 1, 'watching', 0)`, []any{parent.RecordID}},
		{`insert into public.record_notifications (
			notification_id, record_id, event_kind, subject_kind, subject_id,
			source_version, actor_id, authorization_epoch, record_fence_epoch,
			event_at, details_delete_after
		) values ($1, $2, 'comment_mentioned', 'comment', 'rcm_collabchild',
		  1, 'usr_records1', 1, 0, transaction_timestamp(), transaction_timestamp() + interval '1 day')`,
			[]any{"rnt_" + string(make([]byte, 64)), parent.RecordID}},
	}
	// The notification id must be lowercase hex rather than NUL bytes.
	statements[len(statements)-1].args[0] = "rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, statement := range statements {
		if _, err := fixture.db.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed collaboration deletion surface: %v\nSQL: %s", err, statement.sql)
		}
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_notification_recipients (
			notification_id, record_id, recipient_user_id, reason_kind, mandatory,
			authorization_epoch, record_fence_epoch
		) values ($1, $2, 'usr_bbbbbbbbbbbbbbbbbbbbbbbb', 'mention', true, 1, 0)`,
		"rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", parent.RecordID,
	); err != nil {
		t.Fatalf("seed collaboration recipient: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_notification_deliveries (
			delivery_id, record_id, notification_id, recipient_user_id, channel,
			binding_id, delivery_state, attempt_count, reason_code,
			authorization_epoch, record_fence_epoch, sent_at
		) values ('rnd_collabdelete', $1, $2, 'usr_bbbbbbbbbbbbbbbbbbbbbbbb',
		  'telegram', 'binding-collabdelete', 'sent', 1, '', 1, 0, transaction_timestamp())`,
		parent.RecordID, "rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	); err != nil {
		t.Fatalf("seed collaboration delivery: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_notification_delivery_attempts (
			attempt_id, record_id, delivery_id, notification_id, recipient_user_id,
			attempt_no, outcome, authorization_epoch, record_fence_epoch,
			started_at, completed_at
		) values ('rna_collabdelete', $1, 'rnd_collabdelete', $2,
		  'usr_bbbbbbbbbbbbbbbbbbbbbbbb', 1, 'sent', 1, 0,
		  transaction_timestamp(), transaction_timestamp())`,
		parent.RecordID, "rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	); err != nil {
		t.Fatalf("seed collaboration delivery attempt: %v", err)
	}

	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_collabdelete", ReservationID: "drs_collabdelete",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: parent.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7, LedgerSequence: 11, LedgerEntryHash: sha256.Sum256([]byte("collaboration deletion ledger")),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, parent.RevisionID)
	repository := NewPostgresRecordDeletionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-collaboration-deletion", 2),
		allowRecordPlatformAdmissionGate,
	)
	adapter, err := recordcollaboration.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	health, err := adapter.HealthSnapshot(ctx)
	if err != nil || !health.Healthy() {
		t.Fatalf("HealthSnapshot() = %#v, %v", health, err)
	}
	preview, err := adapter.PreviewDeletion(ctx, recorddeletion.PreviewTarget{
		Object: operation.Object, CurrentRevisionID: parent.RevisionID,
		LockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
		ContentDeliveryEpoch: 0, DependencyGraphDigest: sha256.Sum256([]byte("collaboration graph")),
	})
	if err != nil {
		t.Fatalf("PreviewDeletion() error = %v", err)
	}
	wantCopies := []recorddeletion.AdapterSurvivingCopy{{
		Kind: recorddeletion.SurvivingCopyKindDeliveredNotification, CopyCount: 1,
	}}
	if !reflect.DeepEqual(preview.SurvivingCopies, wantCopies) {
		t.Fatalf("PreviewDeletion().SurvivingCopies = %#v, want %#v", preview.SurvivingCopies, wantCopies)
	}
	target := recorddeletion.PurgeTarget{Operation: operation}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if receipt.RemovedRowCount != 16 {
		t.Fatalf("PurgeDeletion().RemovedRowCount = %d, want 16", receipt.RemovedRowCount)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	replay, err := adapter.PurgeDeletion(ctx, target)
	if err != nil || replay != receipt {
		t.Fatalf("PurgeDeletion(replay) = %#v, %v, want %#v", replay, err, receipt)
	}
	var remaining, receipts int64
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*) from public.record_actions where record_id = $1) +
		  (select count(*) from public.record_comments where record_id = $1) +
		  (select count(*) from public.record_notifications where record_id = $1),
		  (select count(*) from public.record_collaboration_purge_receipts where operation_id = $2)`,
		parent.RecordID, operation.OperationID,
	).Scan(&remaining, &receipts); err != nil {
		t.Fatalf("read collaboration deletion result: %v", err)
	}
	if remaining != 0 || receipts != 1 {
		t.Fatalf("remaining/receipts = %d/%d, want 0/1", remaining, receipts)
	}
}
