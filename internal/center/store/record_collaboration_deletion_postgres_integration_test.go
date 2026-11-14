package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/notify"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationRecordCollaborationDeletionPurgesExactOwnedSurfacesAndReplays(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "collaboration-deletion-parent")
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
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_notification_audit_summaries (
			notification_id, record_id, event_kind, subject_kind, source_version, event_at,
			recipient_count, delivery_count, sent_count, unknown_count, permanent_failed_count,
			record_fence_epoch
		) values ('rnt_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc', $1,
			'action_completed', 'action', 1, transaction_timestamp(), 2, 3, 2, 1, 0, 0)`, parent.RecordID); err != nil {
		t.Fatalf("seed collaboration restored notification audit: %v", err)
	}

	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_collabdelete", ReservationID: "drs_collabdelete",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: parent.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7}
	repository := NewPostgresRecordDeletionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-collaboration-deletion", 2),
		fixture.admissionGate,
	)
	adapter, err := recordcollaboration.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	health, err := adapter.HealthSnapshot(ctx)
	if err != nil || !health.Healthy() {
		t.Fatalf("HealthSnapshot() = %#v, %v", health, err)
	}
	func() {
		tx, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `alter function public.record_outbox_drain_v2(bytea) rename to record_outbox_drain_v2_test_hidden`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `create function public.record_outbox_drain_v2(bytea)
			returns table(drained_count bigint) language sql security definer set search_path=pg_catalog
			as $$ select 0::bigint $$`); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.collaborationDeletionHealthInTransaction(ctx, tx); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
			t.Fatalf("health with incomplete drain result error = %v, want unavailable", err)
		}
	}()
	preview, err := adapter.PreviewDeletion(ctx, recorddeletion.PreviewTarget{
		Object: operation.Object, CurrentRevisionID: parent.RevisionID,
		LockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
		ContentDeliveryEpoch: 0, DependencyGraphDigest: sha256.Sum256([]byte("collaboration graph")),
	})
	if err != nil {
		t.Fatalf("PreviewDeletion() error = %v", err)
	}
	wantCopies := []recorddeletion.AdapterSurvivingCopy{
		recorddeletion.KnownAdapterSurvivingCopy(recorddeletion.SurvivingCopyKindDeliveredNotification, 3),
		recorddeletion.KnownAdapterSurvivingCopy(recorddeletion.SurvivingCopyKindPossibleExternalDelivery, 1),
	}
	if !reflect.DeepEqual(preview.SurvivingCopies, wantCopies) {
		t.Fatalf("PreviewDeletion().SurvivingCopies = %#v, want %#v", preview.SurvivingCopies, wantCopies)
	}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, parent.RevisionID)
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2 where project_id='default' and object_kind='record' and object_id=$1`, parent.RecordID, int64(operation.FenceEpoch)); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(
		fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "record-collaboration-worker", 2),
		allowRecordPlatformAdmissionGate,
	)
	adapter, err = recordcollaboration.NewDeletionAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if receipt.RemovedRowCount != 17 {
		t.Fatalf("PurgeDeletion().RemovedRowCount = %d, want 17", receipt.RemovedRowCount)
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
		  (select count(*) from public.record_notifications where record_id = $1) +
		  (select count(*) from public.record_notification_audit_summaries where record_id = $1),
		  (select count(*) from public.record_collaboration_purge_receipts where operation_id = $2)`,
		parent.RecordID, operation.OperationID,
	).Scan(&remaining, &receipts); err != nil {
		t.Fatalf("read collaboration deletion result: %v", err)
	}
	if remaining != 0 || receipts != 1 {
		t.Fatalf("remaining/receipts = %d/%d, want 0/1", remaining, receipts)
	}
}

func TestPostgresIntegrationRecordCollaborationDeletionCountsInFlightDeliveryOnce(t *testing.T) {
	for _, outcome := range []recordcollaboration.ExternalDeliveryProviderOutcome{
		recordcollaboration.ExternalDeliveryProviderSent,
		recordcollaboration.ExternalDeliveryProviderUnknownOutcome,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			harness := newPostgresExternalDeliveryHarness(t)
			ctx := harness.ctx
			pool := harness.fixture.openDirectRuntimePool(t, ctx, "delivery-disclosure", 2)
			deletion := NewPostgresRecordDeletionRepository(pool, harness.fixture.admissionGate)
			target := recorddeletion.PreviewTarget{
				Object:            recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
				CurrentRevisionID: harness.created.RevisionID,
				LockVersion:       harness.created.LockVersion, AuthorizationEpoch: harness.created.AuthorizationEpoch,
				ContentDeliveryEpoch: 0, DependencyGraphDigest: sha256.Sum256([]byte("delivery-disclosure")),
			}
			binding, err := recordcollaboration.NewRecordFenceBinding("default", harness.created.RecordID, 0)
			if err != nil {
				t.Fatal(err)
			}
			assertCounts := func(stage string, wantSent, wantPossible uint64) {
				t.Helper()
				preview, err := deletion.PreviewCollaborationDeletion(ctx, target)
				if err != nil {
					t.Fatalf("%s preview: %v", stage, err)
				}
				var sent, possible uint64
				for _, copy := range preview.SurvivingCopies {
					if copy.CopyCount == nil {
						t.Fatalf("%s notification count is unknown: %#v", stage, copy)
					}
					switch copy.Kind {
					case recorddeletion.SurvivingCopyKindDeliveredNotification:
						sent += *copy.CopyCount
					case recorddeletion.SurvivingCopyKindPossibleExternalDelivery:
						possible += *copy.CopyCount
					}
				}
				if sent != wantSent || possible != wantPossible {
					t.Errorf("%s preview sent/possible = %d/%d, want %d/%d", stage, sent, possible, wantSent, wantPossible)
				}
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(ctx) }()
				snapshot, err := NewPostgresRecordCollaborationProvider().BackupCollaboration(ctx, tx, binding)
				if err != nil {
					t.Fatalf("%s backup: %v", stage, err)
				}
				var deliveries uint64
				sent, possible = 0, 0
				for _, audit := range snapshot.NotificationAudits {
					deliveries += audit.DeliveryCount
					sent += audit.SentCount
					possible += audit.UnknownCount
				}
				if deliveries != 1 || sent != wantSent || possible != wantPossible {
					t.Errorf("%s backup deliveries/sent/possible = %d/%d/%d, want 1/%d/%d",
						stage, deliveries, sent, possible, wantSent, wantPossible)
				}
			}
			assertCounts("pending", 0, 0)
			claim := harness.claim
			prepared, err := harness.repository.PrepareExternalDelivery(ctx, claim, "https://houfeng.example", time.Microsecond)
			if err != nil || prepared.Prepared == nil {
				t.Fatalf("prepare first attempt: %#v, %v", prepared, err)
			}
			assertCounts("processing first attempt", 0, 1)
			fact, err := harness.repository.ReportExternalDeliveryOutcome(
				ctx, claim, prepared.Prepared.Attempt, prepared.Prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderTemporaryFailure,
			)
			if err != nil {
				t.Fatalf("report temporary failure: %v", err)
			}
			assertCounts("temporary failure reported before finalization", 0, 0)
			result, err := harness.repository.FinalizeExternalDelivery(ctx, claim, prepared.Prepared.Attempt, fact, time.Microsecond)
			if err != nil || !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxRetry {
				t.Fatalf("finalize temporary failure: %#v, %v", result, err)
			}
			assertCounts("retry wait", 0, 0)
			claim = claimNotificationOutboxKind(t, ctx, harness.queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "disclosure_second_attempt")
			prepared, err = harness.repository.PrepareExternalDelivery(ctx, claim, "https://houfeng.example", time.Microsecond)
			if err != nil || prepared.Prepared == nil {
				t.Fatalf("prepare second attempt: %#v, %v", prepared, err)
			}
			assertCounts("processing second attempt", 0, 1)
			fact, err = harness.repository.ReportExternalDeliveryOutcome(ctx, claim, prepared.Prepared.Attempt, prepared.Prepared.ReportingCapability, outcome)
			if err != nil {
				t.Fatalf("report terminal outcome: %v", err)
			}
			if outcome == recordcollaboration.ExternalDeliveryProviderSent {
				assertCounts("sent reported before finalization", 1, 0)
			} else {
				assertCounts("unknown reported before finalization", 0, 1)
			}
			result, err = harness.repository.FinalizeExternalDelivery(ctx, claim, prepared.Prepared.Attempt, fact, 0)
			if err != nil || !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete {
				t.Fatalf("finalize terminal outcome: %#v, %v", result, err)
			}
			if outcome == recordcollaboration.ExternalDeliveryProviderSent {
				assertCounts("sent after two attempts", 1, 0)
			} else {
				assertCounts("unknown after two attempts", 0, 1)
			}
			conflict := recordcollaboration.ExternalDeliveryProviderSent
			if outcome == recordcollaboration.ExternalDeliveryProviderSent {
				conflict = recordcollaboration.ExternalDeliveryProviderUnknownOutcome
			}
			if _, err := harness.repository.ReportExternalDeliveryOutcome(ctx, claim, prepared.Prepared.Attempt, prepared.Prepared.ReportingCapability, conflict); err != nil {
				t.Fatalf("report conflicting outcome: %v", err)
			}
			assertCounts("conflicting reports", 0, 1)

			// An aggregate restored from elsewhere has no per-delivery identity.
			// Its overlap with retained obligations cannot be counted safely.
			tag, err := harness.fixture.db.Exec(ctx, `insert into public.record_notification_audit_summaries
				(notification_id,record_id,event_kind,subject_kind,source_version,event_at,
				 recipient_count,delivery_count,sent_count,unknown_count,permanent_failed_count,record_fence_epoch)
				select notification_id,record_id,event_kind,subject_kind,source_version,event_at,1,1,0,1,0,record_fence_epoch
				from public.record_notifications where notification_id=$1`, prepared.Prepared.Attempt.NotificationID)
			if err != nil || tag.RowsAffected() != 1 {
				t.Fatalf("seed overlapping audit: rows=%d error=%v", tag.RowsAffected(), err)
			}
			if _, err := deletion.PreviewCollaborationDeletion(ctx, target); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
				t.Fatalf("preview overlapping aggregate error = %v, want unavailable", err)
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := NewPostgresRecordCollaborationProvider().BackupCollaboration(ctx, tx, binding); !errors.Is(err, recordcollaboration.ErrInvalidPortabilitySnapshot) {
				t.Fatalf("backup overlapping aggregate error = %v, want unavailable", err)
			}
		})
	}
}

func TestPostgresIntegrationCollaborationPurgeRetainsStartedDeliveryUntilOutcome(t *testing.T) {
	harness := newPostgresExternalDeliveryHarness(t)
	ctx := harness.ctx
	runtime := harness.fixture.openDirectRuntimePool(t, ctx, "collaboration-start-inventory", 2)
	var before, after []byte
	var count int64
	var mismatched bool
	if err := runtime.QueryRow(ctx, `select start_digest,start_count,mismatched from public.record_collaboration_start_inventory($1,0)`, harness.created.RecordID).Scan(&before, &count, &mismatched); err != nil || count != 0 || mismatched {
		t.Fatalf("empty start inventory: count=%d mismatch=%t error=%v", count, mismatched, err)
	}

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		entered <- struct{}{}
		<-release
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	provider, err := recordcollaboration.NewScopedNotifierProvider(notify.NewTelegramNotifierWithBaseURL("fixture-token", "fixture-chat", server.URL, server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	harness.bindings.provider = provider
	processor, err := recordcollaboration.NewScopedExternalDeliveryProcessor(harness.repository, recordcollaboration.ScopedExternalDeliveryProcessorOptions{
		PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second,
		SendTimeout: 30 * time.Second, Clock: postgresExternalDeliveryClock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	type deliveryResult struct {
		result recordcollaboration.ExternalDeliveryOutboxResult
		err    error
	}
	done := make(chan deliveryResult, 1)
	go func() {
		result, err := processor.ProcessExternalDelivery(ctx, harness.claim)
		done <- deliveryResult{result: result, err: err}
	}()
	select {
	case <-entered:
	case result := <-done:
		t.Fatalf("sender returned before provider barrier: %#v, %v", result.result, result.err)
	case <-time.After(20 * time.Second):
		t.Fatal("sender did not reach provider barrier")
	}
	if err := runtime.QueryRow(ctx, `select start_digest,start_count,mismatched from public.record_collaboration_start_inventory($1,0)`, harness.created.RecordID).Scan(&after, &count, &mismatched); err != nil || count != 1 || mismatched || reflect.DeepEqual(before, after) {
		t.Fatalf("started inventory: count=%d mismatch=%t error=%v", count, mismatched, err)
	}

	neighborInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
	})
	birthFixture := ImportAttachmentGCFixture{Runtime: runtime, Gate: harness.fixture.admissionGate}
	neighbor, err := newRevisionMutationAdmittedRepository(t, birthFixture, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx,
		revisionBirthBatchCommand(t, ctx, birthFixture, neighborInput, "collab-neighbor-create"))
	if err != nil {
		t.Fatal(err)
	}
	parentClaim := claimNotificationOutboxKind(t, ctx, harness.queue, recordplatform.OutboxEventKindRecordOwnerChanged, "collab_neighbor_parent")
	if _, err := harness.repository.ProjectNotification(ctx, parentClaim); err != nil {
		t.Fatal(err)
	}
	if err := harness.queue.MarkOutboxSent(ctx, parentClaim); err != nil {
		t.Fatal(err)
	}
	neighborClaim := claimNotificationOutboxKind(t, ctx, harness.queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "collab_neighbor_delivery")
	neighborPrepared, err := harness.repository.PrepareExternalDelivery(ctx, neighborClaim, "https://houfeng.example", time.Second)
	if err != nil || neighborPrepared.Prepared == nil {
		t.Fatalf("prepare neighboring delivery: %#v, %v", neighborPrepared, err)
	}

	// The seeded gate exercises restricted commands only; it is not issuer E2E.
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
		OperationID: "rpo_collabstarted", ReservationID: "drs_collabstarted",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, harness.fixture, operation, harness.created.RevisionID)
	if _, err := harness.fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2 where project_id='default' and object_kind='record' and object_id=$1`, harness.created.RecordID, int64(operation.FenceEpoch)); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(harness.fixture.openDirectRolePool(t, ctx, harness.fixture.deletionWorker, "collaboration-start-worker", 2), allowRecordPlatformAdmissionGate)
	adapter, err := recordcollaboration.NewDeletionAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	for _, mutate := range []func(*recorddeletion.PurgeTarget){
		func(target *recorddeletion.PurgeTarget) { target.OwnerClaim.Owner.OwnerID = "wrong_owner" },
		func(target *recorddeletion.PurgeTarget) { target.OwnerClaim.Owner.Generation++ },
		func(target *recorddeletion.PurgeTarget) {
			target.OwnerClaim.Owner.ExpiresAt = target.OwnerClaim.Owner.ExpiresAt.Add(time.Microsecond)
		},
	} {
		wrong := target
		mutate(&wrong)
		if _, err := adapter.PurgeDeletion(ctx, wrong); err == nil {
			t.Fatal("incorrect deletion owner drained the delivery")
		}
	}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil || receipt.RemovedRowCount == 0 {
		t.Fatalf("purge collaboration with started delivery: %#v, %v", receipt, err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("verify handed-off delivery: %v", err)
	}
	var starts, handoffs, pending, live int64
	if err := harness.fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_notification_delivery_starts where record_id=$1),
		(select count(*) from public.record_notification_delivery_handoffs where record_id=$1 and operation_id=$2 and attempt_id is not null),
		(select count(*) from public.record_outbox where record_id=$1 and status in ('pending','processing')),
		(select count(*) from public.record_notification_deliveries where record_id=$1)`, harness.created.RecordID, operation.OperationID).Scan(&starts, &handoffs, &pending, &live); err != nil || starts != 1 || handoffs != 1 || pending != 0 || live != 0 {
		t.Fatalf("handoff state starts=%d handoffs=%d pending=%d live=%d error=%v", starts, handoffs, pending, live, err)
	}
	var classification string
	var beforeFactDigest []byte
	if err := runtime.QueryRow(ctx, `select classification,evidence_digest,mismatched
		from public.record_collaboration_delivery_inventory($1,0) where delivery_id=$2`,
		harness.created.RecordID, harness.claim.Event.SubjectID).Scan(&classification, &beforeFactDigest, &mismatched); err != nil || classification != "possible_external" || len(beforeFactDigest) != sha256.Size || mismatched {
		t.Fatalf("handed-off disclosure classification=%q mismatched=%t error=%v", classification, mismatched, err)
	}

	core, err := recorddeletion.NewCoreAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.PurgeDeletion(ctx, target); err != nil {
		t.Fatalf("purge isolated core after collaboration handoff: %v", err)
	}
	var roots, epochs int64
	if err := harness.fixture.db.QueryRow(ctx, `select
		(select count(*) from public.records where record_id=$1),
		(select count(*) from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1)`, harness.created.RecordID).Scan(&roots, &epochs); err != nil || roots != 0 || epochs != 0 {
		t.Fatalf("core root/epoch after purge: %d/%d error=%v", roots, epochs, err)
	}

	close(release)
	select {
	case result := <-done:
		if result.err == nil {
			t.Fatalf("old sender finalized after drain: %#v", result.result)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("sender did not finish after provider release")
	}
	var reports, observations int64
	if err := harness.fixture.db.QueryRow(ctx, `select
		count(*) filter (where fact_kind='provider_report' and outcome='sent'),
		count(*) filter (where fact_kind='drain_observation' and outcome='unknown_outcome')
		from public.record_notification_delivery_facts where record_id=$1`, harness.created.RecordID).Scan(&reports, &observations); err != nil || reports != 1 || observations != 1 {
		t.Fatalf("late facts reports=%d observations=%d error=%v", reports, observations, err)
	}
	var afterFactDigest []byte
	if err := runtime.QueryRow(ctx, `select classification,evidence_digest,mismatched
		from public.record_collaboration_delivery_inventory($1,0) where delivery_id=$2`,
		harness.created.RecordID, harness.claim.Event.SubjectID).Scan(&classification, &afterFactDigest, &mismatched); err != nil || classification != "sent" || len(afterFactDigest) != sha256.Size || mismatched || reflect.DeepEqual(beforeFactDigest, afterFactDigest) {
		t.Fatalf("late disclosure classification=%q mismatched=%t error=%v", classification, mismatched, err)
	}
	if err := harness.fixture.db.QueryRow(ctx, `select
		(select count(*) from public.records where record_id=$1),
		(select count(*) from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1),
		(select count(*) from public.record_outbox where record_id=$1 and status in ('pending','processing')),
		(select count(*) from public.record_notification_deliveries where record_id=$1)`, harness.created.RecordID).Scan(&roots, &epochs, &pending, &live); err != nil || roots != 0 || epochs != 0 || pending != 0 || live != 0 {
		t.Fatalf("late report recreated root/epoch/queue/delivery: %d/%d/%d/%d error=%v", roots, epochs, pending, live, err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("verify stable receipt after late report: %v", err)
	}
	if replay, err := adapter.PurgeDeletion(ctx, target); err != nil || replay != receipt {
		t.Fatalf("replay collaboration receipt after late report: %#v, %v", replay, err)
	}
	select {
	case <-entered:
		t.Fatal("delivery was sent more than once")
	default:
	}
	var neighborState string
	if err := harness.fixture.db.QueryRow(ctx, `select d.delivery_state,
		(select count(*) from public.record_notification_delivery_starts s where s.delivery_id=d.delivery_id)
		from public.record_notification_deliveries d where d.record_id=$1 and d.delivery_id=$2`,
		neighbor.RecordID, neighborPrepared.Prepared.Attempt.DeliveryID).Scan(&neighborState, &count); err != nil || neighborState != "processing" || count != 1 {
		t.Fatalf("neighboring delivery after purge: state=%q starts=%d error=%v", neighborState, count, err)
	}
}

func TestPostgresIntegrationCollaborationStartAndDrainLockOrder(t *testing.T) {
	t.Run("start commits before gate", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		ctx, cancel := context.WithTimeout(harness.ctx, 30*time.Second)
		defer cancel()
		fixture := harness.fixture
		runtime := fixture.openDirectRuntimePool(t, ctx, "collaboration-race-start", 2)
		startTx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = startTx.Rollback(ctx) }()
		var startPID int32
		if err := startTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&startPID); err != nil {
			t.Fatal(err)
		}
		row, err := loadExternalDelivery(ctx, startTx, harness.claim.Event.SubjectID)
		if err != nil {
			t.Fatal(err)
		}
		attempt, capability, queueFinalized, err := startExternalDeliveryAttempt(ctx, startTx, harness.claim, row)
		if err != nil || queueFinalized {
			t.Fatalf("start delivery in held transaction: finalized=%t error=%v", queueFinalized, err)
		}

		gateTx, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = gateTx.Rollback(ctx) }()
		var gatePID int32
		if err := gateTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&gatePID); err != nil {
			t.Fatal(err)
		}
		if _, err := gateTx.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899,1)`); err != nil {
			t.Fatal(err)
		}
		gateLock := make(chan error, 1)
		go func() {
			_, err := gateTx.Exec(ctx, `select pg_advisory_xact_lock(1213154899,81)`)
			gateLock <- err
		}()
		waitForPostgresCollaborationBlocker(t, ctx, fixture, gatePID, startPID)
		if err := startTx.Commit(ctx); err != nil {
			t.Fatalf("commit started obligation: %v", err)
		}
		if err := <-gateLock; err != nil {
			t.Fatalf("obtain gate comparison fence: %v", err)
		}
		if _, err := gateTx.Exec(ctx, `select delivery_epoch from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, harness.created.RecordID); err != nil {
			t.Fatalf("obtain gate epoch lock: %v", err)
		}
		operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
			OperationID: "rpo_collabstartfirst", ReservationID: "drs_collabstartfirst",
			Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
		claim := seedCollaborationGateInTransaction(t, ctx, gateTx, operation, harness.created.RevisionID, time.Hour)
		if err := gateTx.Commit(ctx); err != nil {
			t.Fatalf("commit seeded gate: %v", err)
		}
		worker := NewPostgresRecordDeletionRepository(fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "collaboration-race-start-worker", 2), allowRecordPlatformAdmissionGate)
		adapter, err := recordcollaboration.NewDeletionAdapter(worker)
		if err != nil {
			t.Fatal(err)
		}
		target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
		receipt, err := adapter.PurgeDeletion(ctx, target)
		if err != nil {
			t.Fatalf("drain committed start: %v", err)
		}
		if _, err := harness.repository.ReportExternalDeliveryOutcome(ctx, harness.claim, attempt, capability, recordcollaboration.ExternalDeliveryProviderSent); err != nil {
			t.Fatalf("report original attempt after handoff: %v", err)
		}
		if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
			t.Fatalf("verify handed-off start: %v", err)
		}
		var starts, handoffs int
		if err := fixture.db.QueryRow(ctx, `select
			(select count(*) from public.record_notification_delivery_starts where record_id=$1),
			(select count(*) from public.record_notification_delivery_handoffs where operation_id=$2 and attempt_id=$3)`,
			harness.created.RecordID, operation.OperationID, recordcollaboration.NotificationDeliveryAttemptID(attempt.DeliveryID, attempt.Attempt)).Scan(&starts, &handoffs); err != nil || starts != 1 || handoffs != 1 {
			t.Fatalf("start-first responsibility starts=%d handoffs=%d error=%v", starts, handoffs, err)
		}
	})

	t.Run("drain commits before start", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		ctx, cancel := context.WithTimeout(harness.ctx, 30*time.Second)
		defer cancel()
		fixture := harness.fixture
		runtime := fixture.openDirectRuntimePool(t, ctx, "collaboration-race-late-start", 2)
		startTx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = startTx.Rollback(ctx) }()
		row, err := loadExternalDelivery(ctx, startTx, harness.claim.Event.SubjectID)
		if err != nil {
			t.Fatal(err)
		}
		operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
			OperationID: "rpo_collabdrainfirst", ReservationID: "drs_collabdrainfirst",
			Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
		claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, harness.created.RevisionID)
		if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2 where project_id='default' and object_kind='record' and object_id=$1`, harness.created.RecordID, int64(operation.FenceEpoch)); err != nil {
			t.Fatal(err)
		}
		workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "collaboration-race-drain-worker", 2)
		drainTx, err := workerPool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = drainTx.Rollback(ctx) }()
		digest := recorddeletion.RecordCollaborationSurfaceDigest()
		payload, err := encodeRecordPurgeCommand(clientComparisonPurgeEnvelope{
			OperationID: operation.OperationID, RecordID: operation.Object.ObjectID, ReservationID: operation.ReservationID,
			FenceEpoch: uint64(operation.FenceEpoch), OwnerID: claim.Owner.OwnerID, OwnerGeneration: claim.Owner.Generation,
			OwnerExpiresAt: claim.Owner.ExpiresAt.UTC().Format(time.RFC3339Nano), SurfaceDigest: hex.EncodeToString(digest[:]),
		})
		if err != nil {
			t.Fatal(err)
		}
		var drained int64
		var remaining bool
		if err := drainTx.QueryRow(ctx, `select drained_count,remaining from public.record_outbox_drain_v2($1)`, payload).Scan(&drained, &remaining); err != nil || drained == 0 {
			t.Fatalf("hold drain transaction: drained=%d remaining=%t error=%v", drained, remaining, err)
		}
		_, _, _, err = startExternalDeliveryAttempt(ctx, startTx, harness.claim, row)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
			t.Fatalf("start against held drain error=%v, want lock conflict", err)
		}
		if err := startTx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err := drainTx.Commit(ctx); err != nil {
			t.Fatalf("commit drain before start: %v", err)
		}
		startTx, err = runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = startTx.Rollback(ctx) }()
		if _, _, _, err := startExternalDeliveryAttempt(ctx, startTx, harness.claim, row); err == nil {
			t.Fatal("start succeeded after the committed deletion drain")
		}
		_ = startTx.Rollback(ctx)
		worker := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
		adapter, err := recordcollaboration.NewDeletionAdapter(worker)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}); err != nil {
			t.Fatalf("complete drain-first collaboration purge: %v", err)
		}
		var starts, handoffs, facts int
		if err := fixture.db.QueryRow(ctx, `select
			(select count(*) from public.record_notification_delivery_starts where record_id=$1),
			(select count(*) from public.record_notification_delivery_handoffs where operation_id=$2 and delivery_id=$3
			  and attempt_no=1 and attempt_id is null and legacy_observation_kind='not_started'
			  and observed_status='not_sent' and legacy_outbox_row_id=$4),
			(select count(*) from public.record_notification_delivery_facts where record_id=$1)`,
			harness.created.RecordID, operation.OperationID, harness.claim.Event.SubjectID, harness.claim.Event.RowID).Scan(&starts, &handoffs, &facts); err != nil || starts != 0 || handoffs != 1 || facts != 0 {
			t.Fatalf("drain-first starts=%d handoffs=%d facts=%d error=%v", starts, handoffs, facts, err)
		}
		var classification string
		if err := runtime.QueryRow(ctx, `select classification from public.record_collaboration_delivery_inventory($1,0) where delivery_id=$2`,
			harness.created.RecordID, harness.claim.Event.SubjectID).Scan(&classification); err != nil || classification != "none" {
			t.Fatalf("drain-first disclosure=%q error=%v", classification, err)
		}
	})
}

func TestPostgresIntegrationCollaborationDrainRollsBackWhenOwnerExpiresAfterLockWait(t *testing.T) {
	harness := newPostgresExternalDeliveryHarness(t)
	ctx, cancel := context.WithTimeout(harness.ctx, 30*time.Second)
	defer cancel()
	fixture := harness.fixture
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "collaboration-expiring-drain", 1)
	workerPID := postgresCollaborationBackendPID(t, ctx, workerPool)
	worker := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	adapter, err := recordcollaboration.NewDeletionAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	var holderPID int32
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select outbox_row_id from public.record_outbox where outbox_row_id=$1 for update`, harness.claim.Event.RowID); err != nil {
		t.Fatal(err)
	}
	gateTx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateTx.Rollback(ctx) }()
	if _, err := gateTx.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899,1), pg_advisory_xact_lock(1213154899,81)`); err != nil {
		t.Fatal(err)
	}
	if _, err := gateTx.Exec(ctx, `select delivery_epoch from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, harness.created.RecordID); err != nil {
		t.Fatal(err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
		OperationID: "rpo_collabexpire", ReservationID: "drs_collabexpire",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	claim := seedCollaborationGateInTransaction(t, ctx, gateTx, operation, harness.created.RevisionID, 6*time.Second)
	if err := gateTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim})
		done <- err
	}()
	waitForPostgresCollaborationBlocker(t, ctx, fixture, workerPID, holderPID)
	// The blocker, not a timer, establishes that the batch reached the locked
	// queue row. The database clock establishes when that exact claim expires.
	waitForCollaborationOwnerExpiry(t, ctx, fixture, claim.Owner.ExpiresAt)
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	err = <-done
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("drain after owner expired = %v, want owner rejection", err)
	}
	var status, deliveryState string
	var handoffs, receipts int64
	if err := fixture.db.QueryRow(ctx, `select
		(select status from public.record_outbox where outbox_row_id=$1),
		(select delivery_state from public.record_notification_deliveries where delivery_id=$2),
		(select count(*) from public.record_notification_delivery_handoffs where operation_id=$3),
		(select count(*) from public.record_collaboration_purge_receipts where operation_id=$3)`,
		harness.claim.Event.RowID, harness.claim.Event.SubjectID, operation.OperationID).Scan(&status, &deliveryState, &handoffs, &receipts); err != nil || status != "processing" || deliveryState != "pending" || handoffs != 0 || receipts != 0 {
		t.Fatalf("expired owner left status=%q delivery=%q handoffs=%d receipts=%d error=%v", status, deliveryState, handoffs, receipts, err)
	}
}

func TestPostgresIntegrationCollaborationDrainRejectsOwnerExpiryAtCommit(t *testing.T) {
	harness := newPostgresExternalDeliveryHarness(t)
	ctx, cancel := context.WithTimeout(harness.ctx, 30*time.Second)
	defer cancel()
	fixture := harness.fixture
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "collaboration-commit-expiry", 1)
	gateTx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateTx.Rollback(ctx) }()
	if _, err := gateTx.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899,1), pg_advisory_xact_lock(1213154899,81)`); err != nil {
		t.Fatal(err)
	}
	if _, err := gateTx.Exec(ctx, `select delivery_epoch from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, harness.created.RecordID); err != nil {
		t.Fatal(err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
		OperationID: "rpo_collabcommitexpire", ReservationID: "drs_collabcommitexpire",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	claim := seedCollaborationGateInTransaction(t, ctx, gateTx, operation, harness.created.RevisionID, 6*time.Second)
	if err := gateTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	digest := recorddeletion.RecordCollaborationSurfaceDigest()
	payload, err := encodeRecordPurgeCommand(clientComparisonPurgeEnvelope{
		OperationID: operation.OperationID, RecordID: operation.Object.ObjectID, ReservationID: operation.ReservationID,
		FenceEpoch: uint64(operation.FenceEpoch), OwnerID: claim.Owner.OwnerID, OwnerGeneration: claim.Owner.Generation,
		OwnerExpiresAt: claim.Owner.ExpiresAt.UTC().Format(time.RFC3339Nano), SurfaceDigest: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := workerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var drained int64
	var remaining bool
	if err := tx.QueryRow(ctx, `select drained_count,remaining from public.record_outbox_drain_v2($1)`, payload).Scan(&drained, &remaining); err != nil || drained == 0 {
		t.Fatalf("drain before delayed commit: drained=%d remaining=%t error=%v", drained, remaining, err)
	}
	// Match the normal Go transaction discipline: leave the constraint deferred
	// and attempt COMMIT only after the database clock passes the exact expiry.
	waitForCollaborationOwnerExpiry(t, ctx, fixture, claim.Owner.ExpiresAt)
	err = tx.Commit(ctx)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("commit after owner expired = %v, want owner rejection", err)
	}
	var status, deliveryState string
	var handoffs, receipts int64
	if err := fixture.db.QueryRow(ctx, `select
		(select status from public.record_outbox where outbox_row_id=$1),
		(select delivery_state from public.record_notification_deliveries where delivery_id=$2),
		(select count(*) from public.record_notification_delivery_handoffs where operation_id=$3),
		(select count(*) from public.record_collaboration_purge_receipts where operation_id=$3)`,
		harness.claim.Event.RowID, harness.claim.Event.SubjectID, operation.OperationID).Scan(&status, &deliveryState, &handoffs, &receipts); err != nil || status != "processing" || deliveryState != "pending" || handoffs != 0 || receipts != 0 {
		t.Fatalf("expired commit left status=%q delivery=%q handoffs=%d receipts=%d error=%v", status, deliveryState, handoffs, receipts, err)
	}
}

func waitForCollaborationOwnerExpiry(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, expires time.Time) {
	t.Helper()
	for {
		var expired bool
		if err := fixture.db.QueryRow(ctx, `select clock_timestamp() >= $1`, expires).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Seeded gate fixture for the lock-order tests only; the transaction already
// holds the epoch lock. It does not establish issuer acceptance.
func seedCollaborationGateInTransaction(t *testing.T, ctx context.Context, tx pgx.Tx, operation recorddeletion.DeletionOperation, revisionID string, ownerLease time.Duration) recorddeletion.OperationOwnerClaim {
	t.Helper()
	digest := sha256.Sum256([]byte(operation.OperationID))
	if _, err := tx.Exec(ctx, `insert into public.deletion_reservations
		(reservation_id,project_id,object_kind,object_id,deletion_token_commitment,request_fingerprint,state,fence_epoch,
		 expires_at,completed_at,actor_scope_digest,preview_binding_digest,preview_current_revision_id,preview_lock_version,
		 preview_authorization_epoch,preview_content_delivery_epoch,adapter_readiness_digest,adapter_preview_digest,deletion_contract_version)
		values ($1,'default','record',$2,$3,$3,'committed',$4,clock_timestamp()+interval '1 hour',clock_timestamp(),$3,$3,$5,1,1,0,$3,$3,2)`,
		operation.ReservationID, operation.Object.ObjectID, digest[:], int64(operation.FenceEpoch), revisionID); err != nil {
		t.Fatal(err)
	}
	claim := recorddeletion.OperationOwnerClaim{OperationID: operation.OperationID}
	if err := tx.QueryRow(ctx, `insert into public.record_purge_operations
		(operation_id,reservation_id,project_id,operation_state,deployment_id,actor_id,reason_code,deletion_contract_version,owner_id,owner_generation,owner_expires_at)
		values ($1,$2,'default','online_purging','dp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','usr_records1','user_confirmed',2,
		        'collaboration_race_worker',1,clock_timestamp()+($3::bigint * interval '1 microsecond')) returning owner_id,owner_generation,owner_expires_at`,
		operation.OperationID, operation.ReservationID, ownerLease.Microseconds()).Scan(&claim.Owner.OwnerID, &claim.Owner.Generation, &claim.Owner.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `insert into public.record_deletion_gates
		(operation_id,record_id,expected_lifecycle,expected_revision_id,expected_lock_version,expected_authorization_epoch,
		 fence_epoch,actor_id,actor_scope_digest,request_fingerprint,binding_digest,authorization_evidence)
		values ($1,$2,'active',$3,1,1,$4,'usr_records1',$5,$5,$5,$6)`, operation.OperationID, operation.Object.ObjectID,
		revisionID, int64(operation.FenceEpoch), digest[:], []byte("isolated fixture authorization evidence")); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2 where project_id='default' and object_kind='record' and object_id=$1`, operation.Object.ObjectID, int64(operation.FenceEpoch)); err != nil {
		t.Fatal(err)
	}
	return claim
}

// This fixture seeds the gate and predecessor rows to exercise the restricted
// drain command; it does not claim issuer or production end-to-end coverage.
func TestPostgresIntegrationCollaborationDrainRoutesAndLegacyUnknown(t *testing.T) {
	harness := newPostgresExternalDeliveryHarness(t)
	ctx := harness.ctx
	fixture := harness.fixture
	recordID := harness.created.RecordID
	neighbor := seedPostgresActionParent(t, ctx, fixture, "collaboration-drain-neighbor")
	digest := sha256.Sum256([]byte("drain comment"))
	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values ('ract_drainroute',$1,1,'Action','open','usr_records1','usr_records1',0)`, recordID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_comments
		(comment_id,record_id,author_id,comment_version,body_markdown,render_contract_version,render_model,body_digest,record_fence_epoch)
		values ('rcm_drainroute',$1,'usr_records1',1,'Comment','comment_markdown/v1','{}'::jsonb,$2,0)`, recordID, digest[:]); err != nil {
		t.Fatal(err)
	}
	// A predecessor may have persisted processing without a start or capability.
	if _, err := fixture.db.Exec(ctx, `update public.record_notification_deliveries
		set delivery_state='processing', attempt_count=1, attempt_started_at=clock_timestamp(), updated_at=clock_timestamp()
		where delivery_id=$1`, harness.claim.Event.SubjectID); err != nil {
		t.Fatal(err)
	}
	var routed []int64
	if err := fixture.db.QueryRow(ctx, `with inserted as (
		insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,authorization_epoch,record_id,created_at,expires_at,next_attempt_at)
		select 'default','record_created','record',$1,0,$1,clock_timestamp()-interval '2 hours',
		       clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 day'
		from generate_series(1,103) returning outbox_row_id
	) select array_agg(outbox_row_id) from inserted`, recordID).Scan(&routed); err != nil {
		t.Fatal(err)
	}
	var actionRow, commentRow, neighborRow int64
	if err := fixture.db.QueryRow(ctx, `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,authorization_epoch,status,owner_id,owner_generation,owner_expires_at,expires_at)
		values ('default','record_action_created','action','ract_drainroute',0,'processing','live_queue_owner',1,
		        clock_timestamp()+interval '1 hour',clock_timestamp()+interval '2 hours') returning outbox_row_id`).Scan(&actionRow); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(ctx, `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,authorization_epoch,status,owner_id,owner_generation,owner_expires_at,created_at,expires_at)
		values ('default','record_comment_created','comment','rcm_drainroute',0,'processing','expired_queue_owner',1,
		        clock_timestamp()-interval '1 hour',clock_timestamp()-interval '2 hours',clock_timestamp()-interval '1 hour') returning outbox_row_id`).Scan(&commentRow); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(ctx, `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,authorization_epoch,record_id,expires_at)
		values ('default','record_created','record',$1,0,$1,clock_timestamp()+interval '1 hour') returning outbox_row_id`, neighbor.RecordID).Scan(&neighborRow); err != nil {
		t.Fatal(err)
	}
	routed = append(routed, actionRow, commentRow, harness.claim.Event.RowID)
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
		OperationID: "rpo_collabroutes", ReservationID: "drs_collabroutes",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, harness.created.RevisionID)
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2 where project_id='default' and object_kind='record' and object_id=$1`, recordID, int64(operation.FenceEpoch)); err != nil {
		t.Fatal(err)
	}
	descriptor := recorddeletion.RecordCollaborationSurfaceDigest()
	payload, err := encodeRecordPurgeCommand(clientComparisonPurgeEnvelope{
		OperationID: operation.OperationID, RecordID: recordID, ReservationID: operation.ReservationID,
		FenceEpoch: uint64(operation.FenceEpoch), OwnerID: claim.Owner.OwnerID, OwnerGeneration: claim.Owner.Generation,
		OwnerExpiresAt: claim.Owner.ExpiresAt.UTC().Format(time.RFC3339Nano), SurfaceDigest: hex.EncodeToString(descriptor[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := fixture.openDirectRuntimePool(t, ctx, "collaboration-drain-role", 2)
	var drained int64
	var remaining bool
	err = runtime.QueryRow(ctx, `select drained_count,remaining from public.record_outbox_drain_v2($1)`, payload).Scan(&drained, &remaining)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("runtime drain error = %v, want insufficient privilege", err)
	}
	worker := NewPostgresRecordDeletionRepository(fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "collaboration-drain-worker", 2), allowRecordPlatformAdmissionGate)
	adapter, err := recordcollaboration.NewDeletionAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	wrong := target
	wrong.OwnerClaim.Owner.Generation++
	if _, err := adapter.PurgeDeletion(ctx, wrong); err == nil {
		t.Fatal("stale owner drained routed events")
	}
	var stillPending int64
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_outbox where outbox_row_id=any($1::bigint[]) and status in ('pending','processing')`, routed).Scan(&stillPending); err != nil || stillPending != int64(len(routed)) {
		t.Fatalf("stale owner changed queue: pending=%d want=%d error=%v", stillPending, len(routed), err)
	}
	// More than 100 units force the adapter through multiple committed batches.
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("drain and purge routed events: %v", err)
	}
	var legacy, starts int64
	var neighborStatus string
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_outbox where outbox_row_id=any($1::bigint[]) and status in ('pending','processing')),
		(select count(*) from public.record_notification_delivery_handoffs where operation_id=$2 and delivery_id=$3 and attempt_id is null
		  and legacy_observation_kind='predecessor' and observed_status='possible_external' and legacy_outbox_row_id=$6
		  and octet_length(legacy_observation_digest)=32 and octet_length(legacy_observation)>0),
		(select count(*) from public.record_notification_delivery_starts where record_id=$4),
		(select status from public.record_outbox where outbox_row_id=$5)`, routed, operation.OperationID, harness.claim.Event.SubjectID, recordID, neighborRow, harness.claim.Event.RowID).Scan(&stillPending, &legacy, &starts, &neighborStatus); err != nil || stillPending != 0 || legacy != 1 || starts != 0 || neighborStatus != "pending" {
		t.Fatalf("drain result pending=%d legacy=%d starts=%d neighbor=%q error=%v", stillPending, legacy, starts, neighborStatus, err)
	}
	preparation, err := harness.repository.PrepareExternalDelivery(ctx, harness.claim, "https://houfeng.example", time.Second)
	if err == nil && preparation.Prepared != nil {
		t.Fatal("drained predecessor obtained a new send capability")
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("verify routed drain receipt: %v", err)
	}
	if err := adapter.VerifyDeletion(ctx, wrong, receipt); err == nil {
		t.Fatal("stale owner verified a purge receipt")
	}
}

func TestPostgresIntegrationCollaborationDrainRejectsUnrouteableLegacyEvent(t *testing.T) {
	harness := newPostgresExternalDeliveryHarness(t)
	ctx := harness.ctx
	fixture := harness.fixture
	if _, err := fixture.db.Exec(ctx, `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,source_version,authorization_epoch,record_fence_epoch,expires_at)
		values ('default','record_action_assigned','action','ract_unknownlegacy',1,1,0,clock_timestamp()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2,
		OperationID: "rpo_collabunrouteable", ReservationID: "drs_collabunrouteable",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, harness.created.RevisionID)
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2 where project_id='default' and object_kind='record' and object_id=$1`, harness.created.RecordID, int64(operation.FenceEpoch)); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "collaboration-unrouteable-worker", 2), allowRecordPlatformAdmissionGate)
	adapter, err := recordcollaboration.NewDeletionAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}); err == nil {
		t.Fatal("unrouteable legacy event was treated as empty inventory")
	}
	var receipts int64
	var status string
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_collaboration_purge_receipts where operation_id=$1),
		(select status from public.record_outbox where outbox_row_id=$2)`, operation.OperationID, harness.claim.Event.RowID).Scan(&receipts, &status); err != nil || receipts != 0 || status != "processing" {
		t.Fatalf("unrouteable drain receipts=%d outbox=%q error=%v", receipts, status, err)
	}
}

func TestPostgresIntegrationRecordCollaborationDeletionDoesNotCommitReceiptBeforeExactAbsence(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	parent := seedPostgresActionParent(t, ctx, fixture, "collaboration-deletion-blocked-parent")
	runtime := fixture.openDirectRuntimePool(t, ctx, "collaboration-absence-notifications", 2)
	projection, queue := newPostgresNotificationProjectionHarness(t, runtime, fixture.admissionGate)
	projector, err := recordcollaboration.NewNotificationProjector(queue, projection, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := projector.ProjectNext(ctx, recordplatform.OutboxClaimInputV1{
		OwnerID: "collaboration_absence_setup", OwnerLeaseDuration: time.Minute,
	})
	if err != nil || !processed {
		t.Fatalf("finish parent notification effect: processed=%t error=%v", processed, err)
	}
	var parentStatus string
	var outstanding int
	if err := fixture.db.QueryRow(ctx, `select
		(select status from public.record_outbox where record_id=$1 and event_kind='record_created'),
		(select count(*)::int from public.record_outbox where record_id=$1 and status in ('pending','processing'))`,
		parent.RecordID).Scan(&parentStatus, &outstanding); err != nil || parentStatus != "cancelled" || outstanding != 0 {
		t.Fatalf("parent notification status=%q outstanding=%d error=%v", parentStatus, outstanding, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_followers (
			record_id, user_id, follower_version, manual_preference, record_fence_epoch
		) values ($1, 'usr_records1', 1, 'watching', 0)`, parent.RecordID); err != nil {
		t.Fatalf("seed blocked collaboration follower: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		create or replace function record_platform_internal.suppress_collaboration_follower_delete()
		returns trigger language plpgsql security invoker set search_path = pg_catalog
		as $$ begin return null; end $$;
		create trigger suppress_collaboration_follower_delete
		before delete on public.record_followers
		for each row execute function record_platform_internal.suppress_collaboration_follower_delete()`); err != nil {
		t.Fatalf("install blocked collaboration delete trigger: %v", err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_collabblocked", ReservationID: "drs_collabblocked",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: parent.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 7}
	claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, operation, parent.RevisionID)
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2 where project_id='default' and object_kind='record' and object_id=$1`, parent.RecordID, int64(operation.FenceEpoch)); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRecordDeletionRepository(
		fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "record-collaboration-blocked-deletion", 2),
		allowRecordPlatformAdmissionGate,
	)
	adapter, err := recordcollaboration.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	_, err = adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" || pgErr.Message != "collaboration purge absence unavailable" {
		t.Fatalf("PurgeDeletion(suppressed owned row) error = %v, want collaboration absence failure", err)
	}
	var blockedRows, blockedReceipts int
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*)::int from public.record_followers where record_id = $1),
		  (select count(*)::int from public.record_collaboration_purge_receipts where operation_id = $2)`,
		parent.RecordID, operation.OperationID,
	).Scan(&blockedRows, &blockedReceipts); err != nil {
		t.Fatalf("read blocked collaboration purge state: %v", err)
	}
	if blockedRows != 1 || blockedReceipts != 0 {
		t.Fatalf("blocked collaboration rows/receipts = %d/%d, want 1/0 rollback", blockedRows, blockedReceipts)
	}
}
