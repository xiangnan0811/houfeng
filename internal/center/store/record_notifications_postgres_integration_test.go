package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordAutomaticFollowerSourcesRollbackWithProducerTransaction(t *testing.T) {
	for _, test := range []struct {
		name        string
		subjectKind string
		commit      func(*testing.T, context.Context, recordPlatformPostgresFixture, records.RevisionCommitResult, AdmissionGate) (string, error)
	}{
		{
			name: "action", subjectKind: recordplatform.OutboxSubjectKindAction,
			commit: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, parent records.RevisionCommitResult, gate AdmissionGate) (string, error) {
				command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgsourcerollback", 0,
					mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Rollback source", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}), "notification-source-action-rollback")
				pool := fixture.openDirectRuntimePool(t, ctx, "record-source-action-rollback", 1)
				_, err := NewPostgresRecordActionRepository(pool, gate, NewPostgresCollaborationMembershipReader(),
					newRecordsPostgresAuthorization(t, pool, fixture.admissionGate)).CommitAction(ctx, command)
				return command.Idempotency.Key.Key, err
			},
		},
		{
			name: "comment", subjectKind: recordplatform.OutboxSubjectKindComment,
			commit: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, parent records.RevisionCommitResult, gate AdmissionGate) (string, error) {
				command := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate, "rcm_pgsourcerollback", 0,
					"Rollback source.", "", []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}, "notification-source-comment-rollback")
				pool := fixture.openDirectRuntimePool(t, ctx, "record-source-comment-rollback", 1)
				_, err := NewPostgresRecordCommentRepository(pool, gate, NewPostgresCollaborationMembershipReader(),
					newRecordsPostgresAuthorization(t, pool, fixture.admissionGate)).CommitComment(ctx, command)
				return command.Idempotency.Key.Key, err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			seedCollaborationRevisionUsers(t, ctx, fixture)
			seedPostgresCollaborationSource(t, ctx, fixture)
			parent := seedPostgresActionParent(t, ctx, fixture, "notification-source-"+test.name+"-parent")
			cutPoint := errors.New("automatic follower completion admission cut point")
			var admissions atomic.Int32
			gate := AdmissionGateFunc(func(ctx context.Context, tx pgx.Tx) error {
				if err := fixture.admissionGate.Admit(ctx, tx); err != nil {
					return err
				}
				if admissions.Add(1) == 3 {
					return cutPoint
				}
				return nil
			})
			key, err := test.commit(t, ctx, fixture, parent, gate)
			if !errors.Is(err, cutPoint) {
				t.Fatalf("commit error = %v, want completion cut point", err)
			}
			var followers, subjects, outbox, keys int
			if err := fixture.db.QueryRow(ctx, `
				select (select count(*)::int from public.record_followers where record_id = $1),
				       (select count(*)::int from public.record_outbox where subject_kind = $2),
				       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $3),
				       case $2
				         when 'action' then (select count(*)::int from public.record_actions where record_id = $1)
				         else (select count(*)::int from public.record_comments where record_id = $1)
				       end`, parent.RecordID, test.subjectKind, key).Scan(&followers, &outbox, &keys, &subjects); err != nil {
				t.Fatalf("read rollback counts: %v", err)
			}
			if followers != 0 || subjects != 0 || outbox != 0 || keys != 0 {
				t.Fatalf("rollback followers/subjects/outbox/keys = %d/%d/%d/%d", followers, subjects, outbox, keys)
			}
		})
	}
}

func TestPostgresIntegrationRecordNotificationCancelsCapturedAuthorizationAndFenceEpochMismatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		slug   string
		mutate func(*testing.T, context.Context, recordPlatformPostgresFixture, records.RevisionCommitResult)
	}{
		{
			name: "authorization epoch", slug: "auth",
			mutate: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, created records.RevisionCommitResult) {
				if _, err := fixture.db.Exec(ctx, `update public.records set authorization_epoch = authorization_epoch + 1 where record_id = $1`, created.RecordID); err != nil {
					t.Fatalf("advance authorization epoch: %v", err)
				}
			},
		},
		{
			name: "record fence epoch", slug: "fence",
			mutate: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, created records.RevisionCommitResult) {
				fixture.advanceContentDeliveryEpoch(t, ctx, recordplatform.ObjectRef{ProjectID: string(recordplatform.ProjectIDDefault), ObjectKind: "record", ObjectID: created.RecordID})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			seedCollaborationRevisionUsers(t, ctx, fixture)
			seedPostgresCollaborationSource(t, ctx, fixture)
			runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-stale-"+test.slug, 3)
			input := collaborationRevisionInput(t, collaborationRevisionInputValues{ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"})
			created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "notification-stale-"+test.slug))
			if err != nil {
				t.Fatalf("CommitRevision() error = %v", err)
			}
			projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
			claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "notification_stale_claim")
			if claim.Event.AuthorizationEpoch != created.AuthorizationEpoch || claim.Event.RecordFenceEpoch != 0 {
				t.Fatalf("captured epochs = auth %d/fence %d, want %d/0", claim.Event.AuthorizationEpoch, claim.Event.RecordFenceEpoch, created.AuthorizationEpoch)
			}
			test.mutate(t, ctx, fixture, created)
			preclaimed := &notificationPreclaimedQueue{claim: &claim, queue: queue}
			projector, err := recordcollaboration.NewNotificationProjector(preclaimed, projection, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			processed, err := projector.ProjectNext(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "notification_stale_worker", OwnerLeaseDuration: time.Minute})
			if err != nil || !processed {
				t.Fatalf("ProjectNext(stale epochs) = (%v, %v)", processed, err)
			}
			assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 0, 0)
			var status string
			if err := fixture.db.QueryRow(ctx, `select status from public.record_outbox where outbox_row_id = $1`, claim.Event.RowID).Scan(&status); err != nil {
				t.Fatalf("read stale outbox status: %v", err)
			}
			if status != "cancelled" {
				t.Fatalf("stale outbox status = %q, want cancelled", status)
			}
		})
	}
}

func TestPostgresIntegrationRecordPlatformAssertOutboxClaimRejectsImmutableTupleDrift(t *testing.T) {
	ctx := context.Background()
	fixture, gate, runtimePool := newAdmittedOutboxFixture(t, ctx)
	parent := seedAdmittedOutboxRecord(t, ctx, runtimePool, gate, "outbox-tuple-parent")
	repository := NewPostgresRecordPlatformRepository(runtimePool, gate)
	actions := NewPostgresRecordActionRepository(runtimePool, gate, NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, runtimePool, gate))
	for index, test := range []struct {
		name   string
		mutate func(*testing.T, int64)
	}{
		{name: "event kind", mutate: func(t *testing.T, rowID int64) {
			_, err := fixture.db.Exec(ctx, `update public.record_outbox set event_kind = 'record_action_completed' where outbox_row_id = $1`, rowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "subject kind", mutate: func(t *testing.T, rowID int64) {
			_, err := fixture.db.Exec(ctx, `update public.record_outbox set subject_kind = 'comment' where outbox_row_id = $1`, rowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "subject id", mutate: func(t *testing.T, rowID int64) {
			_, err := fixture.db.Exec(ctx, `update public.record_outbox set subject_id = 'ract_tuple_changed' where outbox_row_id = $1`, rowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "source version", mutate: func(t *testing.T, rowID int64) {
			_, err := fixture.db.Exec(ctx, `update public.record_outbox set source_version = source_version + 1 where outbox_row_id = $1`, rowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "authorization epoch", mutate: func(t *testing.T, rowID int64) {
			_, err := fixture.db.Exec(ctx, `update public.record_outbox set authorization_epoch = authorization_epoch + 1 where outbox_row_id = $1`, rowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "record fence epoch", mutate: func(t *testing.T, rowID int64) {
			_, err := fixture.db.Exec(ctx, `update public.record_outbox set record_fence_epoch = record_fence_epoch + 1 where outbox_row_id = $1`, rowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "event expiry", mutate: func(t *testing.T, rowID int64) {
			_, err := fixture.db.Exec(ctx, `update public.record_outbox set expires_at = expires_at + interval '1 minute' where outbox_row_id = $1`, rowID)
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			subjectID := fmt.Sprintf("ract_tuple%d", index)
			command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, subjectID, 0,
				mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Tuple route", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}),
				fmt.Sprintf("outbox-tuple-%d", index))
			if _, err := actions.CommitAction(ctx, command); err != nil {
				t.Fatalf("create action tuple fixture: %v", err)
			}
			claim := claimNotificationOutboxKind(t, ctx, repository, recordplatform.OutboxEventKindRecordActionAssigned, fmt.Sprintf("tuple_drift_%d", index))
			assertClaim := func() error {
				return repository.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
					acquired, err := acquireEffectMutationResources(ctx, transaction.tx, []string{parent.RecordID}, nil, records.RevisionMutationResources{})
					if err != nil {
						return err
					}
					transaction.registerAcquiredRecordMutationResources(acquired)
					return transaction.AssertOutboxClaim(ctx, claim)
				})
			}
			if err := assertClaim(); err != nil {
				t.Fatalf("assert original outbox claim: %v", err)
			}
			test.mutate(t, claim.Event.RowID)
			err := assertClaim()
			if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("AssertOutboxClaim(tuple drift) error = %v, want ErrLostOwnerLease", err)
			}
			for _, finalize := range []struct {
				name string
				run  func() error
			}{
				{name: "cancel", run: func() error { return repository.CancelOutbox(ctx, claim) }},
				{name: "sent", run: func() error { return repository.MarkOutboxSent(ctx, claim) }},
				{name: "retry", run: func() error { return repository.RetryOutbox(ctx, claim, time.Second) }},
			} {
				if err := finalize.run(); err == nil {
					t.Fatalf("%s accepted drifted outbox tuple", finalize.name)
				}
			}
			var status string
			if err := fixture.db.QueryRow(ctx, `select status from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&status); err != nil || status != "processing" {
				t.Fatalf("drifted finalization status=%q error=%v", status, err)
			}
		})
	}
}

func TestPostgresIntegrationRecordNotificationTakeoverOverlapsAndRejectsStaleFinalizer(t *testing.T) {
	ctx := context.Background()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	parent := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-action-takeover-parent")
	stalePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-stale-finalizer", 1)
	takeoverPool := fixture.openDirectRuntimePool(t, ctx, "record-notification-live-takeover", 1)
	staleQueue := NewPostgresRecordPlatformRepository(stalePool, gate)
	command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_takeoveroverlap", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Takeover route", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}), "outbox-takeover-action")
	actions := NewPostgresRecordActionRepository(runtime, gate, NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, runtime, gate))
	if _, err := actions.CommitAction(ctx, command); err != nil {
		t.Fatalf("create takeover action: %v", err)
	}
	staleClaim := claimNotificationOutboxKind(t, ctx, staleQueue, recordplatform.OutboxEventKindRecordActionAssigned, "notification_stale_owner")
	expireNotificationClaim(t, ctx, fixture, staleClaim.Event.RowID)
	for _, statement := range []string{
		`create table public.test_record_notification_takeover_latch (latch_id integer primary key)`,
		`insert into public.test_record_notification_takeover_latch (latch_id) values (1)`,
		`create function record_platform_internal.test_record_notification_takeover_overlap() returns trigger
		 language plpgsql security definer set search_path = pg_catalog as $$
		 begin
		   perform latch_id from public.test_record_notification_takeover_latch where latch_id = 1 for update;
		   return new;
		 end
		 $$`,
		`create trigger test_record_notification_takeover_overlap after update on public.record_outbox
		 for each row when (new.owner_id = 'notification_takeover_owner' and new.owner_generation > old.owner_generation)
		 execute function record_platform_internal.test_record_notification_takeover_overlap()`,
	} {
		if _, err := fixture.db.Exec(ctx, statement); err != nil {
			t.Fatalf("install notification takeover overlap latch: %v", err)
		}
	}
	controlTx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = controlTx.Rollback(context.Background()) }()
	var controlPID, latchID, takeoverPID int
	if err := controlTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&controlPID); err != nil {
		t.Fatal(err)
	}
	if err := controlTx.QueryRow(ctx, `select latch_id from public.test_record_notification_takeover_latch where latch_id = 1 for update`).Scan(&latchID); err != nil || latchID != 1 {
		t.Fatalf("lock notification takeover latch = %d/%v", latchID, err)
	}
	if err := takeoverPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&takeoverPID); err != nil {
		t.Fatal(err)
	}
	type claimOutcome struct {
		claim *recordplatform.ClaimedOutboxEventV1
		err   error
	}
	raceCtx, cancelRace := context.WithTimeout(ctx, 10*time.Second)
	defer cancelRace()
	takeoverResults := make(chan claimOutcome, 1)
	go func() {
		claim, err := NewPostgresRecordPlatformRepository(takeoverPool, gate).ClaimOutbox(raceCtx, recordplatform.OutboxClaimInputV1{
			OwnerID: "notification_takeover_owner", OwnerLeaseDuration: time.Minute,
		})
		takeoverResults <- claimOutcome{claim: claim, err: err}
	}()
	waitForPostgresBlockingPID(t, raceCtx, fixture.db, takeoverPID, controlPID)
	staleResults := make(chan error, 1)
	go func() { staleResults <- staleQueue.MarkOutboxSent(raceCtx, staleClaim) }()
	select {
	case err := <-staleResults:
		if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("overlapped stale finalizer error = %v, want ErrLostOwnerLease", err)
		}
	case <-raceCtx.Done():
		t.Fatalf("stale finalizer did not reject while takeover was in flight: %v", raceCtx.Err())
	}
	if err := controlTx.Commit(ctx); err != nil {
		t.Fatalf("release notification takeover latch: %v", err)
	}
	takeover := <-takeoverResults
	if takeover.err != nil || takeover.claim == nil || takeover.claim.Owner.Generation != staleClaim.Owner.Generation+1 {
		t.Fatalf("takeover claim = (%#v, %v)", takeover.claim, takeover.err)
	}
	var status, ownerID string
	var generation int64
	if err := fixture.db.QueryRow(ctx, `select status, owner_id, owner_generation from public.record_outbox where outbox_row_id = $1`, staleClaim.Event.RowID).Scan(&status, &ownerID, &generation); err != nil {
		t.Fatal(err)
	}
	if status != "processing" || ownerID != "notification_takeover_owner" || generation != int64(takeover.claim.Owner.Generation) {
		t.Fatalf("overlap durable owner = %q/%q/%d", status, ownerID, generation)
	}
}

func TestPostgresIntegrationRecordNotificationProjectionFencesTakeoverAndReplaysAtomically(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-projection", 4)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
	})
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()))
	created, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "notification-parent-create"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	authorization := newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate)
	projection := NewPostgresRecordNotificationRepository(
		runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(), authorization, 30*24*time.Hour,
	)
	queue := NewPostgresRecordPlatformRepository(runtimePool, fixture.admissionGate)

	ownerClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "notification_owner_a")
	if ownerClaim.Event.SubjectID != created.RecordID || ownerClaim.Event.SourceVersion != created.RevisionNo {
		t.Fatalf("owner claim event = %#v, want raw record and revision version", ownerClaim.Event)
	}
	expireNotificationClaim(t, ctx, fixture, ownerClaim.Event.RowID)
	ownerTakeover := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "notification_owner_b")
	if ownerTakeover.Owner.Generation != ownerClaim.Owner.Generation+1 {
		t.Fatalf("owner takeover generation = %d, want %d", ownerTakeover.Owner.Generation, ownerClaim.Owner.Generation+1)
	}
	if got, err := projection.ProjectNotification(ctx, ownerClaim); !errors.Is(err, recordplatform.ErrLostOwnerLease) || got != (recordcollaboration.NotificationProjectionResult{}) {
		t.Fatalf("stale ProjectNotification() = (%#v, %v), want lost owner", got, err)
	}
	assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 0, 0)
	ownerResult, err := projection.ProjectNotification(ctx, ownerTakeover)
	if err != nil || ownerResult.RecipientCount != 2 || ownerResult.NotificationID == "" {
		t.Fatalf("owner ProjectNotification() = (%#v, %v)", ownerResult, err)
	}
	if err := queue.MarkOutboxSent(ctx, ownerTakeover); err != nil {
		t.Fatalf("MarkOutboxSent(owner) error = %v", err)
	}

	participantClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordParticipantChanged, "notification_participant_a")
	participantResult, err := projection.ProjectNotification(ctx, participantClaim)
	if err != nil || participantResult.RecipientCount != 2 || participantResult.NotificationID == "" {
		t.Fatalf("participant ProjectNotification() = (%#v, %v)", participantResult, err)
	}
	assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 2, 4)
	expireNotificationClaim(t, ctx, fixture, participantClaim.Event.RowID)
	participantTakeover := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordParticipantChanged, "notification_participant_b")
	replay, err := projection.ProjectNotification(ctx, participantTakeover)
	if err != nil || replay != participantResult {
		t.Fatalf("participant replay = (%#v, %v), want %#v", replay, err, participantResult)
	}
	if err := queue.MarkOutboxSent(ctx, participantTakeover); err != nil {
		t.Fatalf("MarkOutboxSent(participant) error = %v", err)
	}
	assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 2, 4)

	var deliveries int
	if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.record_notification_deliveries where record_id = $1`, created.RecordID).Scan(&deliveries); err != nil {
		t.Fatalf("count notification deliveries: %v", err)
	}
	if deliveries != 0 {
		t.Fatalf("projection delivery rows = %d, want zero", deliveries)
	}
	var ownerNotifications, participantNotifications int
	if err := fixture.db.QueryRow(ctx, `
		select count(*) filter (where event_kind = 'record_owner_changed')::int,
		       count(*) filter (where event_kind = 'record_participant_changed')::int
		from public.record_notifications where record_id = $1`, created.RecordID).Scan(&ownerNotifications, &participantNotifications); err != nil {
		t.Fatalf("count per-event projection identities: %v", err)
	}
	if ownerNotifications != 1 || participantNotifications != 1 {
		t.Fatalf("per-event notifications owner/participant = %d/%d, want exact 1/1 after replay", ownerNotifications, participantNotifications)
	}

	actor := collaborationActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", nil)
	listRequest := recordcollaboration.InboxListRequest{Actor: actor, Limit: 100}
	items, err := projection.ListInbox(ctx, listRequest)
	if err != nil || len(items) != 2 {
		t.Fatalf("ListInbox() = (%#v, %v), want two authorized items", items, err)
	}
	if items[0].EventAt.Before(items[1].EventAt) || (items[0].EventAt.Equal(items[1].EventAt) && items[0].NotificationID > items[1].NotificationID) {
		t.Fatalf("ListInbox() order = %#v", items)
	}
	count, err := projection.CountUnreadInbox(ctx, listRequest)
	if err != nil || count != 2 {
		t.Fatalf("CountUnreadInbox() = (%d, %v), want 2", count, err)
	}
	count, err = projection.CountUnreadInbox(ctx, recordcollaboration.InboxListRequest{Actor: actor, Limit: 1})
	if err != nil || count != 1 {
		t.Fatalf("CountUnreadInbox(limit 1) = (%d, %v), want capped 1", count, err)
	}
	itemRequest := recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: items[0].NotificationID}
	if got, err := projection.GetInboxItem(ctx, itemRequest); err != nil || got != items[0] {
		t.Fatalf("GetInboxItem() = (%#v, %v), want %#v", got, err, items[0])
	}
	target, err := projection.GetInboxDeepLink(ctx, itemRequest)
	if err != nil || target.RecordID != created.RecordID || target.SubjectKind != recordcollaboration.NotificationSubjectRecord || target.SubjectID != created.RecordID {
		t.Fatalf("GetInboxDeepLink() = (%#v, %v)", target, err)
	}
	read, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: items[0].NotificationID, Kind: recordcollaboration.InboxTransitionRead})
	if err != nil || read.ReadAt == nil || read.DismissedAt != nil {
		t.Fatalf("TransitionInbox(read) = (%#v, %v)", read, err)
	}
	readReplay, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: items[0].NotificationID, Kind: recordcollaboration.InboxTransitionRead})
	if err != nil || readReplay.ReadAt == nil || !readReplay.ReadAt.Equal(*read.ReadAt) {
		t.Fatalf("TransitionInbox(read replay) = (%#v, %v), want stable timestamp %#v", readReplay, err, read.ReadAt)
	}
	dismissed, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: items[0].NotificationID, Kind: recordcollaboration.InboxTransitionDismiss})
	if err != nil || dismissed.ReadAt == nil || dismissed.DismissedAt == nil {
		t.Fatalf("TransitionInbox(dismiss) = (%#v, %v)", dismissed, err)
	}
	itemsAfterDismiss, err := projection.ListInbox(ctx, listRequest)
	if err != nil || len(itemsAfterDismiss) != 1 {
		t.Fatalf("ListInbox(after dismiss) = (%#v, %v), want one", itemsAfterDismiss, err)
	}
	unread, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: items[0].NotificationID, Kind: recordcollaboration.InboxTransitionUnread})
	if err != nil || unread.ReadAt != nil || unread.DismissedAt != nil {
		t.Fatalf("TransitionInbox(unread) = (%#v, %v)", unread, err)
	}
	transitionResults := make(chan error, 2)
	startTransitions := make(chan struct{})
	for _, kind := range []recordcollaboration.InboxTransitionKind{recordcollaboration.InboxTransitionRead, recordcollaboration.InboxTransitionDismiss} {
		kind := kind
		go func() {
			<-startTransitions
			_, transitionErr := projection.TransitionInbox(context.Background(), recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: items[1].NotificationID, Kind: kind})
			transitionResults <- transitionErr
		}()
	}
	close(startTransitions)
	for range 2 {
		if err := <-transitionResults; err != nil {
			t.Fatalf("concurrent inbox transition error = %v", err)
		}
	}
	concurrentItem, err := projection.GetInboxItem(ctx, recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: items[1].NotificationID})
	if err != nil || concurrentItem.ReadAt == nil || concurrentItem.DismissedAt == nil {
		t.Fatalf("concurrent read+dismiss item = (%#v, %v)", concurrentItem, err)
	}

	if _, err := fixture.db.Exec(ctx, `update public.users set role = 'viewer' where user_id = $1`, actor.UserID); err != nil {
		t.Fatalf("revoke inbox membership: %v", err)
	}
	if got, err := projection.ListInbox(ctx, listRequest); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListInbox(after membership revoke) = (%#v, %v), want nonnil empty", got, err)
	}
	if _, err := projection.GetInboxItem(ctx, itemRequest); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
		t.Fatalf("GetInboxItem(after membership revoke) error = %v, want opaque not found", err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.users set role = 'admin' where user_id = $1`, actor.UserID); err != nil {
		t.Fatalf("restore inbox membership: %v", err)
	}

	if _, err := fixture.db.Exec(ctx, `delete from public.vps_assets where vps_id = $1`, input.Subjects()[0].SourceID); err != nil {
		t.Fatalf("remove current source: %v", err)
	}
	if _, err := projection.ListInbox(ctx, listRequest); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("ListInbox(without source) error = %v, want source unavailable", err)
	}
	if _, err := projection.GetInboxDeepLink(ctx, itemRequest); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("GetInboxDeepLink(without source) error = %v, want source unavailable", err)
	}
	seedPostgresCollaborationSource(t, ctx, fixture)
	if _, err := fixture.db.Exec(ctx, `
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
			'drs_inboxdelete', 'default', 'record', $1,
			decode(repeat('31', 32), 'hex'), decode(repeat('32', 32), 'hex'),
			decode(repeat('33', 32), 'hex'), decode(repeat('34', 32), 'hex'),
			$2, $3, $4, 0,
			decode(repeat('35', 32), 'hex'), decode(repeat('36', 32), 'hex'),
			decode(repeat('37', 32), 'hex'), decode(repeat('38', 32), 'hex'),
			decode(repeat('39', 32), 'hex'), 1,
			decode(repeat('3a', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		)`, created.RecordID, created.RevisionID, created.LockVersion, created.AuthorizationEpoch); err != nil {
		t.Fatalf("seed inbox deletion reservation: %v", err)
	}
	if got, err := projection.ListInbox(ctx, listRequest); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListInbox(after deletion reservation) = (%#v, %v), want nonnil empty", got, err)
	}
	if _, err := projection.GetInboxDeepLink(ctx, itemRequest); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
		t.Fatalf("GetInboxDeepLink(after deletion reservation) error = %v, want opaque not found", err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.deletion_reservations where reservation_id = 'drs_inboxdelete'`); err != nil {
		t.Fatalf("remove inbox deletion reservation fixture: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.records set authorization_epoch = authorization_epoch + 1
		where record_id = $1`, created.RecordID); err != nil {
		t.Fatalf("advance inbox authorization epoch: %v", err)
	}
	if got, err := projection.ListInbox(ctx, listRequest); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListInbox(after authorization epoch advance) = (%#v, %v), want nonnil empty", got, err)
	}
	if _, err := projection.GetInboxItem(ctx, itemRequest); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
		t.Fatalf("GetInboxItem(after authorization epoch advance) error = %v, want opaque not found", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.records set authorization_epoch = $2
		where record_id = $1`, created.RecordID, int64(created.AuthorizationEpoch)); err != nil {
		t.Fatalf("restore inbox authorization epoch fixture: %v", err)
	}

	fixture.advanceContentDeliveryEpoch(t, ctx, recordplatform.ObjectRef{ProjectID: string(recordplatform.ProjectIDDefault), ObjectKind: "record", ObjectID: created.RecordID})
	if got, err := projection.ListInbox(ctx, listRequest); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListInbox(after fence advance) = (%#v, %v), want nonnil empty", got, err)
	}
	if _, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: items[0].NotificationID, Kind: recordcollaboration.InboxTransitionRead}); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
		t.Fatalf("TransitionInbox(after fence advance) error = %v, want opaque not found", err)
	}
}

func TestPostgresIntegrationRecordNotificationProjectionEnqueuesScopedDeliveryWithoutInboxCoupling(t *testing.T) {
	for _, test := range []struct {
		name          string
		bindingReader *notificationBindingListerStub
		wantDelivery  int
		wantErr       error
	}{
		{
			name: "exact scoped binding",
			bindingReader: &notificationBindingListerStub{byUser: map[string][]recordcollaboration.ScopedTransportBindingRef{
				"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {{
					ProjectID: recordauth.ProjectIDDefault, RecipientUserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
					Channel: recordcollaboration.NotificationDeliveryTelegram, BindingID: "telegram_primary",
				}},
			}},
			wantDelivery: 1,
		},
		{
			name:          "binding source unavailable keeps inbox",
			bindingReader: &notificationBindingListerStub{err: errors.New("scoped binding unavailable")},
			wantDelivery:  0,
			wantErr:       recordcollaboration.ErrExternalDeliveryUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			seedCollaborationRevisionUsers(t, ctx, fixture)
			seedPostgresCollaborationSource(t, ctx, fixture)
			runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-delivery-projection", 4)
			input := collaborationRevisionInput(t, collaborationRevisionInputValues{
				ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
			})
			created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "delivery-projection-parent"))
			if err != nil {
				t.Fatalf("CommitRevision() error = %v", err)
			}
			_, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
			projection := NewPostgresRecordNotificationRepositoryWithExternalBindings(
				runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
				newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate), 30*24*time.Hour, test.bindingReader,
			)
			claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "delivery_projection_owner")
			result, err := projection.ProjectNotification(ctx, claim)
			if test.wantErr == nil && err != nil || test.wantErr != nil && !errors.Is(err, test.wantErr) || result.RecipientCount != 2 {
				t.Fatalf("ProjectNotification() = (%#v, %v), want committed inbox and planning error=%v", result, err, test.wantErr)
			}
			if test.wantErr == nil {
				if _, err := projection.ProjectNotification(ctx, claim); err != nil {
					t.Fatalf("ProjectNotification(replay) error = %v", err)
				}
			}
			assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 1, 2)

			var deliveries, deliveryOutbox int
			var deliveryID, bindingID, channel string
			if err := fixture.db.QueryRow(ctx, `
				select count(*)::int, coalesce(min(delivery_id), ''), coalesce(min(binding_id), ''), coalesce(min(channel), '')
				from public.record_notification_deliveries where record_id = $1`, created.RecordID).Scan(
				&deliveries, &deliveryID, &bindingID, &channel,
			); err != nil {
				t.Fatal(err)
			}
			if err := fixture.db.QueryRow(ctx, `
				select count(*)::int from public.record_outbox
				where event_kind = 'record_notification_delivery' and subject_kind = 'delivery'
				  and source_version = $1 and authorization_epoch = $2 and record_fence_epoch = $3`,
				int64(claim.Event.SourceVersion), int64(claim.Event.AuthorizationEpoch), int64(claim.Event.RecordFenceEpoch),
			).Scan(&deliveryOutbox); err != nil {
				t.Fatal(err)
			}
			if deliveries != test.wantDelivery || deliveryOutbox != test.wantDelivery {
				t.Fatalf("delivery rows/outbox = %d/%d, want %d/%d", deliveries, deliveryOutbox, test.wantDelivery, test.wantDelivery)
			}
			if test.wantDelivery == 1 {
				if recordcollaboration.ValidateNotificationDeliveryID(deliveryID) != nil || bindingID != "telegram_primary" || channel != "telegram" {
					t.Fatalf("delivery identity = %q/%q/%q", deliveryID, bindingID, channel)
				}
			}
		})
	}
}

func TestPostgresIntegrationRecordNotificationPlanningFailureRetriesParentWithoutRollingBackInbox(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-planning-retry", 4)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
	})
	created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "delivery-planning-retry-parent"))
	if err != nil {
		t.Fatal(err)
	}
	_, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
	planningErr := errors.New("credential=secret webhook=https://provider.invalid/hook response=provider-body")
	bindings := &notificationBindingListerStub{
		err: planningErr,
		byUser: map[string][]recordcollaboration.ScopedTransportBindingRef{
			"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {{
				ProjectID: recordauth.ProjectIDDefault, RecipientUserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
				Channel: recordcollaboration.NotificationDeliveryTelegram, BindingID: "telegram_primary",
			}},
		},
	}
	projection := NewPostgresRecordNotificationRepositoryWithExternalBindings(
		runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
		newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate), 30*24*time.Hour, bindings,
	)
	claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "delivery_planning_first")
	projector, err := recordcollaboration.NewNotificationProjector(
		&notificationPreclaimedQueue{claim: &claim, queue: queue}, projection, time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := projector.ProjectNext(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "unused_preclaimed", OwnerLeaseDuration: time.Minute})
	if !processed || !errors.Is(err, recordcollaboration.ErrExternalDeliveryUnavailable) || errors.Is(err, planningErr) {
		t.Fatalf("ProjectNext(planning unavailable) = (%t, %v), want closed ErrExternalDeliveryUnavailable", processed, err)
	}
	for _, forbidden := range []string{"credential", "secret", "webhook", "provider.invalid", "provider-body"} {
		if strings.Contains(strings.ToLower(err.Error()), forbidden) {
			t.Fatalf("closed planning error leaked %q in %q", forbidden, err.Error())
		}
	}
	assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 1, 2)
	var parentStatus string
	if err := fixture.db.QueryRow(ctx, `select status from public.record_outbox where outbox_row_id = $1`, claim.Event.RowID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "pending" {
		t.Fatalf("parent outbox status = %q, want pending retry", parentStatus)
	}
	bindings.err = nil
	if _, err := fixture.db.Exec(ctx, `
		update public.record_outbox set next_attempt_at = transaction_timestamp() - interval '1 second'
		where outbox_row_id = $1`, claim.Event.RowID); err != nil {
		t.Fatal(err)
	}
	recoveryClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "delivery_planning_recovery")
	recoveryProjector, err := recordcollaboration.NewNotificationProjector(
		&notificationPreclaimedQueue{claim: &recoveryClaim, queue: queue}, projection, time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := recoveryProjector.ProjectNext(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "unused_preclaimed", OwnerLeaseDuration: time.Minute}); err != nil || !processed {
		t.Fatalf("ProjectNext(recovered planner) = (%t, %v)", processed, err)
	}
	assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 1, 2)
	var deliveries, deliveryOutbox int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_notification_deliveries where record_id = $1),
		       (select count(*)::int from public.record_outbox where event_kind = 'record_notification_delivery')`,
		created.RecordID,
	).Scan(&deliveries, &deliveryOutbox); err != nil {
		t.Fatal(err)
	}
	if deliveries != 1 || deliveryOutbox != 1 {
		t.Fatalf("recovered deliveries/outbox = %d/%d, want exact 1/1", deliveries, deliveryOutbox)
	}
}

func TestPostgresIntegrationRecordNotificationPlanningRechecksPolicyAndOwnerBetweenTransactions(t *testing.T) {
	for _, test := range []struct {
		name   string
		drift  func(*testing.T, context.Context, recordPlatformPostgresFixture, records.RevisionCommitResult, *PostgresRecordPlatformRepository, recordplatform.ClaimedOutboxEventV1) *recordplatform.ClaimedOutboxEventV1
		assert func(*testing.T, error)
	}{
		{
			name: "policy muted",
			drift: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, created records.RevisionCommitResult, _ *PostgresRecordPlatformRepository, _ recordplatform.ClaimedOutboxEventV1) *recordplatform.ClaimedOutboxEventV1 {
				if _, err := fixture.db.Exec(ctx, `
					update public.record_followers
					set manual_preference = 'muted', follower_version = follower_version + 1
					where record_id = $1 and user_id = 'usr_bbbbbbbbbbbbbbbbbbbbbbbb'`, created.RecordID); err != nil {
					t.Fatal(err)
				}
				return nil
			},
			assert: func(t *testing.T, err error) {
				if err != nil {
					t.Fatalf("ProjectNotification(policy drift) error = %v", err)
				}
			},
		},
		{
			name: "membership revoked",
			drift: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, _ records.RevisionCommitResult, _ *PostgresRecordPlatformRepository, _ recordplatform.ClaimedOutboxEventV1) *recordplatform.ClaimedOutboxEventV1 {
				if _, err := fixture.db.Exec(ctx, `update public.users set role = 'viewer' where user_id = 'usr_bbbbbbbbbbbbbbbbbbbbbbbb'`); err != nil {
					t.Fatal(err)
				}
				return nil
			},
			assert: func(t *testing.T, err error) {
				if err != nil {
					t.Fatalf("ProjectNotification(membership drift) error = %v", err)
				}
			},
		},
		{
			name: "outbox takeover",
			drift: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, _ records.RevisionCommitResult, queue *PostgresRecordPlatformRepository, claim recordplatform.ClaimedOutboxEventV1) *recordplatform.ClaimedOutboxEventV1 {
				expireNotificationClaim(t, ctx, fixture, claim.Event.RowID)
				takeover := claimNotificationOutboxKind(t, ctx, queue, claim.Event.EventKind, "delivery_planning_takeover")
				return &takeover
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
					t.Fatalf("ProjectNotification(takeover drift) error = %v, want ErrLostOwnerLease", err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			seedCollaborationRevisionUsers(t, ctx, fixture)
			seedPostgresCollaborationSource(t, ctx, fixture)
			runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-planning-recheck", 6)
			input := collaborationRevisionInput(t, collaborationRevisionInputValues{
				ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
			})
			created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "delivery-planning-recheck-parent"))
			if err != nil {
				t.Fatal(err)
			}
			_, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
			bindings := &notificationBindingListerStub{byUser: map[string][]recordcollaboration.ScopedTransportBindingRef{
				"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {{
					ProjectID: recordauth.ProjectIDDefault, RecipientUserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
					Channel: recordcollaboration.NotificationDeliveryTelegram, BindingID: "telegram_primary",
				}},
			}}
			enteredPlanning := make(chan struct{})
			releasePlanning := make(chan struct{})
			var admissions atomic.Int32
			gate := AdmissionGateFunc(func(ctx context.Context, tx pgx.Tx) error {
				if err := fixture.admissionGate.Admit(ctx, tx); err != nil {
					return err
				}
				if admissions.Add(1) == 5 {
					close(enteredPlanning)
					select {
					case <-releasePlanning:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			})
			projection := NewPostgresRecordNotificationRepositoryWithExternalBindings(
				runtimePool, gate, NewPostgresCollaborationMembershipReader(),
				newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate), 30*24*time.Hour, bindings,
			)
			claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "delivery_planning_interphase")
			type projectionOutcome struct {
				result recordcollaboration.NotificationProjectionResult
				err    error
			}
			outcomes := make(chan projectionOutcome, 1)
			go func() {
				result, err := projection.ProjectNotification(ctx, claim)
				outcomes <- projectionOutcome{result: result, err: err}
			}()
			select {
			case <-enteredPlanning:
			case <-time.After(10 * time.Second):
				t.Fatal("second planning transaction did not reach admission latch")
			}
			released := false
			defer func() {
				if !released {
					close(releasePlanning)
				}
			}()
			takeover := test.drift(t, ctx, fixture, created, queue, claim)
			close(releasePlanning)
			released = true
			outcome := <-outcomes
			if outcome.result.RecipientCount != 2 {
				t.Fatalf("committed inbox result = %#v, want two initial recipients", outcome.result)
			}
			test.assert(t, outcome.err)
			assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 1, 2)
			var deliveries, deliveryOutbox int
			if err := fixture.db.QueryRow(ctx, `
				select (select count(*)::int from public.record_notification_deliveries where record_id = $1),
				       (select count(*)::int from public.record_outbox where event_kind = 'record_notification_delivery')`,
				created.RecordID,
			).Scan(&deliveries, &deliveryOutbox); err != nil {
				t.Fatal(err)
			}
			if deliveries != 0 || deliveryOutbox != 0 {
				t.Fatalf("drifted planning deliveries/outbox = %d/%d, want zero", deliveries, deliveryOutbox)
			}
			if takeover != nil {
				replay, err := projection.ProjectNotification(ctx, *takeover)
				if err != nil || replay.RecipientCount != 2 {
					t.Fatalf("ProjectNotification(takeover replay) = (%#v, %v)", replay, err)
				}
				if err := queue.MarkOutboxSent(ctx, *takeover); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPostgresIntegrationRecordNotificationTakeoverReconcilesRecipientWithoutDeletingDeliveryAudit(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-recipient-reconcile", 5)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
	})
	created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "delivery-recipient-reconcile-parent"))
	if err != nil {
		t.Fatal(err)
	}
	_, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
	provider := &notificationBindingProviderStub{}
	bindings := &notificationBindingListerStub{
		provider: provider,
		byUser: map[string][]recordcollaboration.ScopedTransportBindingRef{
			"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {{
				ProjectID: recordauth.ProjectIDDefault, RecipientUserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
				Channel: recordcollaboration.NotificationDeliveryTelegram, BindingID: "telegram_primary",
			}},
		},
	}
	projection := NewPostgresRecordNotificationRepositoryWithExternalBindings(
		runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
		newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate), 30*24*time.Hour, bindings,
	)
	claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "delivery_reconcile_first")
	first, err := projection.ProjectNotification(ctx, claim)
	if err != nil || first.RecipientCount != 2 {
		t.Fatalf("ProjectNotification(first) = (%#v, %v)", first, err)
	}
	var deliveryID string
	if err := fixture.db.QueryRow(ctx, `select delivery_id from public.record_notification_deliveries where record_id = $1`, created.RecordID).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.record_followers
		set manual_preference = 'muted', follower_version = follower_version + 1
		where record_id = $1 and user_id = 'usr_bbbbbbbbbbbbbbbbbbbbbbbb'`, created.RecordID); err != nil {
		t.Fatal(err)
	}
	expireNotificationClaim(t, ctx, fixture, claim.Event.RowID)
	takeover := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "delivery_reconcile_takeover")
	replay, err := projection.ProjectNotification(ctx, takeover)
	if err != nil || replay.RecipientCount != 1 || replay.NotificationID != first.NotificationID {
		t.Fatalf("ProjectNotification(takeover reconcile) = (%#v, %v), want one current recipient", replay, err)
	}
	assertPostgresNotificationCounts(t, ctx, fixture, created.RecordID, 1, 1)
	if err := queue.MarkOutboxSent(ctx, takeover); err != nil {
		t.Fatal(err)
	}
	deliveryClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "delivery_reconcile_worker")
	result, err := newPostgresExternalDeliveryProcessor(t, projection).ProcessExternalDelivery(ctx, deliveryClaim)
	if err != nil || result.Disposition != recordcollaboration.ExternalDeliveryOutboxCancel {
		t.Fatalf("ProcessExternalDelivery(reconciled recipient) = (%#v, %v), want cancel", result, err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls after recipient reconcile = %d, want zero", provider.calls)
	}
	if err := queue.CancelOutbox(ctx, deliveryClaim); err != nil {
		t.Fatal(err)
	}
	assertExternalDeliveryState(t, ctx, fixture, deliveryID, "cancelled", "cancelled", 1)
}

func TestPostgresIntegrationRecordExternalDeliveryUsesOutboxOwnerForAttemptAndTakeover(t *testing.T) {
	for _, test := range []struct {
		name     string
		takeover bool
	}{
		{name: "sent"},
		{name: "expired processing becomes unknown without resend", takeover: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			seedCollaborationRevisionUsers(t, ctx, fixture)
			seedPostgresCollaborationSource(t, ctx, fixture)
			runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-external-delivery", 4)
			input := collaborationRevisionInput(t, collaborationRevisionInputValues{
				ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
			})
			created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "external-delivery-parent"))
			if err != nil {
				t.Fatal(err)
			}
			_, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
			provider := &notificationBindingProviderStub{}
			bindings := &notificationBindingListerStub{
				provider: provider,
				byUser: map[string][]recordcollaboration.ScopedTransportBindingRef{
					"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {{
						ProjectID: recordauth.ProjectIDDefault, RecipientUserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
						Channel: recordcollaboration.NotificationDeliveryTelegram, BindingID: "telegram_primary",
					}},
				},
			}
			repository := NewPostgresRecordNotificationRepositoryWithExternalBindings(
				runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
				newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate), 30*24*time.Hour, bindings,
			)
			parentClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "external_parent")
			if _, err := repository.ProjectNotification(ctx, parentClaim); err != nil {
				t.Fatal(err)
			}
			if err := queue.MarkOutboxSent(ctx, parentClaim); err != nil {
				t.Fatal(err)
			}
			claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "external_delivery_a")
			preparation, err := repository.PrepareExternalDelivery(ctx, claim, "https://houfeng.example", time.Second)
			if err != nil || preparation.Prepared == nil || preparation.Validate() != nil {
				t.Fatalf("PrepareExternalDelivery() = (%#v, %v)", preparation, err)
			}
			if preparation.Prepared.Attempt.RecordID != created.RecordID || preparation.Prepared.Binding.Provider != provider {
				t.Fatalf("prepared exact scope = %#v", preparation.Prepared)
			}

			if !test.takeover {
				fact, err := repository.ReportExternalDeliveryOutcome(ctx, claim, preparation.Prepared.Attempt, preparation.Prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderSent)
				if err != nil {
					t.Fatalf("ReportExternalDeliveryOutcome(sent): %v", err)
				}
				result, err := repository.FinalizeExternalDelivery(ctx, claim, preparation.Prepared.Attempt, fact, 0)
				if err != nil || !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete {
					t.Fatalf("FinalizeExternalDelivery(sent) = (%#v, %v)", result, err)
				}
				replayed, err := repository.FinalizeExternalDelivery(ctx, claim, preparation.Prepared.Attempt, fact, 0)
				if err != nil || replayed != result {
					t.Fatalf("replay committed sent completion = (%#v, %v), want %#v", replayed, err, result)
				}
				var queueState string
				if err := fixture.db.QueryRow(ctx, `select status from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&queueState); err != nil || queueState != "sent" {
					t.Fatalf("sent completion queue=%q error=%v", queueState, err)
				}
				assertExternalDeliveryState(t, ctx, fixture, claim.Event.SubjectID, "sent", "sent", 1)
				return
			}

			expireNotificationClaim(t, ctx, fixture, claim.Event.RowID)
			takeover := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "external_delivery_b")
			replay, err := repository.PrepareExternalDelivery(ctx, takeover, "https://houfeng.example", time.Second)
			if err != nil || replay.Prepared != nil || !replay.Result.QueueFinalized || replay.Result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete {
				t.Fatalf("PrepareExternalDelivery(takeover) = (%#v, %v)", replay, err)
			}
			fact, err := repository.ReportExternalDeliveryOutcome(ctx, claim, preparation.Prepared.Attempt, preparation.Prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderSent)
			if err != nil {
				t.Fatalf("late original sender report: %v", err)
			}
			if _, err := repository.FinalizeExternalDelivery(ctx, claim, preparation.Prepared.Attempt, fact, 0); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
				t.Fatalf("stale FinalizeExternalDelivery() error = %v, want ErrLostOwnerLease", err)
			}
			assertExternalDeliveryState(t, ctx, fixture, claim.Event.SubjectID, "unknown_outcome", "unknown_outcome", 1)
		})
	}
}

func TestPostgresIntegrationRecordExternalDeliveryCrashTakeoverOverlapsStaleFinalizer(t *testing.T) {
	harness := newPostgresExternalDeliveryHarness(t)
	preparation, err := harness.repository.PrepareExternalDelivery(harness.ctx, harness.claim, "https://houfeng.example", time.Second)
	if err != nil || preparation.Prepared == nil {
		t.Fatalf("PrepareExternalDelivery() = (%#v, %v)", preparation, err)
	}
	attempt := preparation.Prepared.Attempt
	fact, err := harness.repository.ReportExternalDeliveryOutcome(harness.ctx, harness.claim, attempt, preparation.Prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderSent)
	if err != nil {
		t.Fatalf("commit provider fact before takeover: %v", err)
	}
	expireNotificationClaim(t, harness.ctx, harness.fixture, harness.claim.Event.RowID)

	takeoverPool := harness.fixture.openDirectRuntimePool(t, harness.ctx, "record-external-delivery-live-takeover", 1)
	for _, statement := range []string{
		`create table public.test_record_external_delivery_takeover_latch (latch_id integer primary key)`,
		`insert into public.test_record_external_delivery_takeover_latch (latch_id) values (1)`,
		`create function record_platform_internal.test_record_external_delivery_takeover_overlap() returns trigger
		 language plpgsql security definer set search_path = pg_catalog as $$
		 begin
		   perform latch_id from public.test_record_external_delivery_takeover_latch where latch_id = 1 for update;
		   return new;
		 end
		 $$`,
		`create trigger test_record_external_delivery_takeover_overlap after update on public.record_outbox
		 for each row when (new.owner_id = 'external_delivery_takeover_owner' and new.owner_generation > old.owner_generation)
		 execute function record_platform_internal.test_record_external_delivery_takeover_overlap()`,
	} {
		if _, err := harness.fixture.db.Exec(harness.ctx, statement); err != nil {
			t.Fatalf("install external delivery takeover overlap latch: %v", err)
		}
	}
	controlTx, err := harness.fixture.db.Begin(harness.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = controlTx.Rollback(context.Background()) }()
	var controlPID, latchID, takeoverPID int
	if err := controlTx.QueryRow(harness.ctx, `select pg_backend_pid()`).Scan(&controlPID); err != nil {
		t.Fatal(err)
	}
	if err := controlTx.QueryRow(harness.ctx, `
		select latch_id from public.test_record_external_delivery_takeover_latch
		where latch_id = 1 for update`).Scan(&latchID); err != nil || latchID != 1 {
		t.Fatalf("lock external delivery takeover latch = %d/%v", latchID, err)
	}
	if err := takeoverPool.QueryRow(harness.ctx, `select pg_backend_pid()`).Scan(&takeoverPID); err != nil {
		t.Fatal(err)
	}
	type claimOutcome struct {
		claim *recordplatform.ClaimedOutboxEventV1
		err   error
	}
	raceCtx, cancelRace := context.WithTimeout(harness.ctx, 10*time.Second)
	defer cancelRace()
	takeoverResults := make(chan claimOutcome, 1)
	go func() {
		claim, err := NewPostgresRecordPlatformRepository(takeoverPool, harness.fixture.admissionGate).ClaimOutbox(
			raceCtx,
			recordplatform.OutboxClaimInputV1{OwnerID: "external_delivery_takeover_owner", OwnerLeaseDuration: time.Minute},
		)
		takeoverResults <- claimOutcome{claim: claim, err: err}
	}()
	waitForPostgresBlockingPID(t, raceCtx, harness.fixture.db, takeoverPID, controlPID)

	staleResults := make(chan error, 1)
	staleStarted := make(chan struct{})
	go func() {
		close(staleStarted)
		_, err := harness.repository.FinalizeExternalDelivery(raceCtx, harness.claim, attempt, fact, 0)
		staleResults <- err
	}()
	<-staleStarted
	if err := controlTx.Commit(harness.ctx); err != nil {
		t.Fatalf("release external delivery takeover latch: %v", err)
	}
	takeover := <-takeoverResults
	if takeover.err != nil || takeover.claim == nil || takeover.claim.Event.SubjectID != harness.claim.Event.SubjectID ||
		takeover.claim.Owner.Generation != harness.claim.Owner.Generation+1 {
		t.Fatalf("external delivery takeover claim = (%#v, %v)", takeover.claim, takeover.err)
	}
	select {
	case err := <-staleResults:
		var pgErr *pgconn.PgError
		if !errors.Is(err, recordplatform.ErrLostOwnerLease) && !(errors.As(err, &pgErr) && pgErr.Code == "40001") {
			t.Fatalf("overlapped stale finalizer error = %v, want lost owner or a serialization conflict", err)
		}
	case <-raceCtx.Done():
		t.Fatalf("stale finalizer did not finish: %v", raceCtx.Err())
	}
	if _, err := harness.repository.FinalizeExternalDelivery(harness.ctx, harness.claim, attempt, fact, 0); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("stale finalizer after takeover error = %v, want ErrLostOwnerLease", err)
	}
	result, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(harness.ctx, *takeover.claim)
	if err != nil || !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete {
		t.Fatalf("ProcessExternalDelivery(takeover) = (%#v, %v)", result, err)
	}
	if harness.provider.calls != 0 {
		t.Fatalf("provider calls after uncertain takeover = %d, want zero", harness.provider.calls)
	}
	assertExternalDeliveryState(t, harness.ctx, harness.fixture, harness.claim.Event.SubjectID, "sent", "sent", 1)
}

func TestPostgresIntegrationRecordExternalDeliveryBoundsRetriesAndTerminalOutcomes(t *testing.T) {
	t.Run("temporary failures stop after eight sends", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		harness.provider.outcome = recordcollaboration.ExternalDeliveryProviderTemporaryFailure
		claim := harness.claim
		for attempt := 1; attempt <= int(recordcollaboration.MaxNotificationDeliveryAttempts); attempt++ {
			processor := newPostgresExternalDeliveryProcessor(t, harness.repository)
			result, err := processor.ProcessExternalDelivery(harness.ctx, claim)
			if err != nil {
				t.Fatalf("attempt %d ProcessExternalDelivery() error = %v", attempt, err)
			}
			if attempt < int(recordcollaboration.MaxNotificationDeliveryAttempts) {
				if !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxRetry || result.RetryAfter <= 0 || result.NextAttemptAt.IsZero() {
					t.Fatalf("attempt %d result = %#v, want retry", attempt, result)
				}
				forceExternalDeliveryRetryDue(t, harness, claim.Event.RowID)
				claim = claimNotificationOutboxKind(t, harness.ctx, harness.queue, recordplatform.OutboxEventKindRecordNotificationDelivery, fmt.Sprintf("external_retry_%d", attempt+1))
				continue
			}
			if !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete {
				t.Fatalf("attempt eight result = %#v, want complete terminal", result)
			}
		}
		if harness.provider.calls != int(recordcollaboration.MaxNotificationDeliveryAttempts) {
			t.Fatalf("provider calls = %d, want %d", harness.provider.calls, recordcollaboration.MaxNotificationDeliveryAttempts)
		}
		assertExternalDeliveryState(t, harness.ctx, harness.fixture, claim.Event.SubjectID, "permanent_failure", "temporary_failure", int(recordcollaboration.MaxNotificationDeliveryAttempts))
		var exhaustedReason string
		if err := harness.fixture.db.QueryRow(harness.ctx, `select reason_code from public.record_notification_deliveries where delivery_id = $1`, claim.Event.SubjectID).Scan(&exhaustedReason); err != nil {
			t.Fatal(err)
		}
		if exhaustedReason != "attempts_exhausted" {
			t.Fatalf("terminal reason = %q, want attempts_exhausted", exhaustedReason)
		}
	})

	for _, test := range []struct {
		name      string
		outcome   recordcollaboration.ExternalDeliveryProviderOutcome
		wantState string
	}{
		{name: "permanent", outcome: recordcollaboration.ExternalDeliveryProviderPermanentFailure, wantState: "permanent_failure"},
		{name: "provider unknown", outcome: recordcollaboration.ExternalDeliveryProviderUnknownOutcome, wantState: "unknown_outcome"},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newPostgresExternalDeliveryHarness(t)
			harness.provider.outcome = test.outcome
			result, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(harness.ctx, harness.claim)
			if err != nil || !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete {
				t.Fatalf("ProcessExternalDelivery() = (%#v, %v)", result, err)
			}
			assertExternalDeliveryState(t, harness.ctx, harness.fixture, harness.claim.Event.SubjectID, test.wantState, string(test.outcome), 1)
		})
	}
}

func TestPostgresIntegrationRecordExternalDeliveryProjectorUsesAtomicDisposition(t *testing.T) {
	for _, test := range []struct {
		name       string
		outcome    recordcollaboration.ExternalDeliveryProviderOutcome
		queueState string
		state      string
	}{
		{name: "sent", outcome: recordcollaboration.ExternalDeliveryProviderSent, queueState: "sent", state: "sent"},
		{name: "retry", outcome: recordcollaboration.ExternalDeliveryProviderTemporaryFailure, queueState: "pending", state: "retry_wait"},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newPostgresExternalDeliveryHarness(t)
			harness.provider.outcome = test.outcome
			queue := &notificationPreclaimedQueue{claim: &harness.claim, queue: harness.queue}
			projector, err := recordcollaboration.NewNotificationProjectorWithExternalDelivery(queue, harness.repository, newPostgresExternalDeliveryProcessor(t, harness.repository), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			processed, err := projector.ProjectNext(harness.ctx, recordplatform.OutboxClaimInputV1{OwnerID: "projector_delivery", OwnerLeaseDuration: time.Minute})
			if err != nil || !processed || harness.provider.calls != 1 {
				t.Fatalf("ProjectNext()=(%t,%v), provider calls=%d", processed, err, harness.provider.calls)
			}
			var queueState, deliveryState string
			var attempts, completions int
			if err := harness.fixture.db.QueryRow(harness.ctx, `select o.status,d.delivery_state,d.attempt_count,
				(select count(*) from public.record_notification_delivery_completions c where c.delivery_id=d.delivery_id)
				from public.record_outbox o join public.record_notification_deliveries d on d.delivery_id=o.subject_id
				where o.outbox_row_id=$1`, harness.claim.Event.RowID).Scan(&queueState, &deliveryState, &attempts, &completions); err != nil ||
				queueState != test.queueState || deliveryState != test.state || attempts != 1 || completions != 1 {
				t.Fatalf("atomic disposition queue=%q delivery=%q attempts=%d completions=%d error=%v", queueState, deliveryState, attempts, completions, err)
			}
		})
	}
}

func TestPostgresIntegrationRecordExternalDeliveryFactAndFinalizationRecovery(t *testing.T) {
	t.Run("temporary fact survives takeover and replay retains retry time", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		ctx := harness.ctx
		preparation, err := harness.repository.PrepareExternalDelivery(ctx, harness.claim, "https://houfeng.example", time.Second)
		if err != nil || preparation.Prepared == nil {
			t.Fatalf("prepare original sender: %#v, %v", preparation, err)
		}
		prepared := preparation.Prepared
		fact, err := harness.repository.ReportExternalDeliveryOutcome(ctx, harness.claim, prepared.Attempt, prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderTemporaryFailure)
		if err != nil {
			t.Fatalf("commit temporary fact: %v", err)
		}
		repeated, err := harness.repository.ReportExternalDeliveryOutcome(ctx, harness.claim, prepared.Attempt, prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderTemporaryFailure)
		if err != nil || repeated != fact {
			t.Fatalf("repeat provider fact = %#v, %v", repeated, err)
		}
		expireNotificationClaim(t, ctx, harness.fixture, harness.claim.Event.RowID)
		takeover := claimNotificationOutboxKind(t, ctx, harness.queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "fact_retry_takeover")
		recovered, err := harness.repository.PrepareExternalDelivery(ctx, takeover, "https://houfeng.example", time.Second)
		if err != nil || recovered.Prepared != nil || !recovered.Result.QueueFinalized ||
			recovered.Result.Disposition != recordcollaboration.ExternalDeliveryOutboxRetry ||
			recovered.Result.RetryAfter != time.Second || recovered.Result.NextAttemptAt.IsZero() {
			t.Fatalf("recover temporary fact: %#v, %v", recovered, err)
		}
		if harness.provider.calls != 0 {
			t.Fatalf("takeover resent provider attempt: %d calls", harness.provider.calls)
		}
		var state, queueState string
		var nextAt time.Time
		if err := harness.fixture.db.QueryRow(ctx, `select d.delivery_state,o.status,o.next_attempt_at
			from public.record_notification_deliveries d join public.record_outbox o on o.subject_id=d.delivery_id
			where o.outbox_row_id=$1`, takeover.Event.RowID).Scan(&state, &queueState, &nextAt); err != nil ||
			state != "retry_wait" || queueState != "pending" || !nextAt.Equal(recovered.Result.NextAttemptAt) {
			t.Fatalf("persisted retry state=%q queue=%q next=%v result=%v error=%v", state, queueState, nextAt, recovered.Result.NextAttemptAt, err)
		}
		replayed, err := harness.repository.FinalizeExternalDelivery(ctx, takeover, prepared.Attempt, fact, time.Second)
		if err != nil || !replayed.QueueFinalized || replayed.Disposition != recovered.Result.Disposition ||
			replayed.RetryAfter != recovered.Result.RetryAfter || !replayed.NextAttemptAt.Equal(recovered.Result.NextAttemptAt) {
			t.Fatalf("completion replay changed retry: %#v, %v", replayed, err)
		}
		if _, err := harness.repository.FinalizeExternalDelivery(ctx, harness.claim, prepared.Attempt, fact, time.Second); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("original owner finalized after takeover: %v", err)
		}
		forceExternalDeliveryRetryDue(t, harness, takeover.Event.RowID)
		next := claimNotificationOutboxKind(t, ctx, harness.queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "fact_retry_next")
		result, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(ctx, next)
		if err != nil || !result.QueueFinalized || result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete || harness.provider.calls != 1 {
			t.Fatalf("new authorized attempt = %#v, %v; sends=%d", result, err, harness.provider.calls)
		}
		var attempts int
		if err := harness.fixture.db.QueryRow(ctx, `select delivery_state,attempt_count from public.record_notification_deliveries where delivery_id=$1`, next.Event.SubjectID).Scan(&state, &attempts); err != nil || state != "sent" || attempts != 2 {
			t.Fatalf("retried delivery state=%q attempts=%d error=%v", state, attempts, err)
		}
	})

	t.Run("conflicts and forged reports never authorize a send", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		ctx := harness.ctx
		preparation, err := harness.repository.PrepareExternalDelivery(ctx, harness.claim, "https://houfeng.example", time.Second)
		if err != nil || preparation.Prepared == nil {
			t.Fatalf("prepare original sender: %#v, %v", preparation, err)
		}
		prepared := preparation.Prepared
		forged, err := recordcollaboration.ParseExternalDeliveryReportingCapability([]byte(strings.Repeat("x", 32)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := harness.repository.ReportExternalDeliveryOutcome(ctx, harness.claim, prepared.Attempt, forged, recordcollaboration.ExternalDeliveryProviderSent); err == nil {
			t.Fatal("forged reporting capability recorded a provider fact")
		}
		for _, mutate := range []func(*recordplatform.ClaimedOutboxEventV1){
			func(claim *recordplatform.ClaimedOutboxEventV1) { claim.Owner.OwnerID = "other_sender" },
			func(claim *recordplatform.ClaimedOutboxEventV1) { claim.Owner.Generation++ },
			func(claim *recordplatform.ClaimedOutboxEventV1) {
				claim.Owner.ExpiresAt = claim.Owner.ExpiresAt.Add(time.Microsecond)
			},
			func(claim *recordplatform.ClaimedOutboxEventV1) {
				claim.ExpiresAt = claim.ExpiresAt.Add(time.Microsecond)
			},
		} {
			drifted := harness.claim
			mutate(&drifted)
			if _, err := harness.repository.ReportExternalDeliveryOutcome(ctx, drifted, prepared.Attempt, prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderSent); err == nil {
				t.Fatal("changed sender tuple recorded a provider fact")
			}
		}
		for _, mutate := range []func(*recordcollaboration.ExternalDeliveryAttempt){
			func(attempt *recordcollaboration.ExternalDeliveryAttempt) {
				attempt.StartedAt = attempt.StartedAt.Add(time.Microsecond)
			},
			func(attempt *recordcollaboration.ExternalDeliveryAttempt) {
				attempt.RecipientUserID = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
			},
			func(attempt *recordcollaboration.ExternalDeliveryAttempt) {
				if attempt.RecordID == "rec_aaaaaaaaaaaaaaaaaaaaaaaa" {
					attempt.RecordID = "rec_bbbbbbbbbbbbbbbbbbbbbbbb"
				} else {
					attempt.RecordID = "rec_aaaaaaaaaaaaaaaaaaaaaaaa"
				}
			},
		} {
			drifted := prepared.Attempt
			mutate(&drifted)
			if _, err := harness.repository.ReportExternalDeliveryOutcome(ctx, harness.claim, drifted, prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderSent); err == nil {
				t.Fatal("changed attempt tuple recorded a provider fact")
			}
		}
		workerPool := harness.fixture.openDirectRolePool(t, ctx, harness.fixture.deletionWorker, "delivery-report-denied-worker", 2)
		var workerCanReport bool
		if err := workerPool.QueryRow(ctx, `select has_function_privilege(current_user,'public.record_external_delivery_outcome(bytea)','execute')`).Scan(&workerCanReport); err != nil || workerCanReport {
			t.Fatalf("worker report privilege=%t error=%v", workerCanReport, err)
		}
		workerRepository := &PostgresRecordNotificationRepository{platform: NewPostgresRecordPlatformRepository(workerPool, harness.fixture.admissionGate)}
		if _, err := workerRepository.ReportExternalDeliveryOutcome(ctx, harness.claim, prepared.Attempt, prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderSent); err == nil {
			t.Fatal("deletion worker reported a provider fact")
		}
		first, err := harness.repository.ReportExternalDeliveryOutcome(ctx, harness.claim, prepared.Attempt, prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderSent)
		if err != nil {
			t.Fatalf("first provider fact: %v", err)
		}
		second, err := harness.repository.ReportExternalDeliveryOutcome(ctx, harness.claim, prepared.Attempt, prepared.ReportingCapability, recordcollaboration.ExternalDeliveryProviderPermanentFailure)
		if err != nil || second == first {
			t.Fatalf("conflicting provider fact = %#v, %v", second, err)
		}
		expireNotificationClaim(t, ctx, harness.fixture, harness.claim.Event.RowID)
		takeover := claimNotificationOutboxKind(t, ctx, harness.queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "fact_conflict_takeover")
		recovered, err := harness.repository.PrepareExternalDelivery(ctx, takeover, "https://houfeng.example", time.Second)
		if err != nil || recovered.Prepared != nil || !recovered.Result.QueueFinalized || recovered.Result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete || harness.provider.calls != 0 {
			t.Fatalf("conflicting fact takeover = %#v, %v; sends=%d", recovered, err, harness.provider.calls)
		}
		assertExternalDeliveryState(t, ctx, harness.fixture, harness.claim.Event.SubjectID, "unknown_outcome", "unknown_outcome", 1)
		var providerFacts int
		if err := harness.fixture.db.QueryRow(ctx, `select count(*) from public.record_notification_delivery_facts where attempt_id=$1 and fact_kind='provider_report'`, first.AttemptID).Scan(&providerFacts); err != nil || providerFacts != 2 {
			t.Fatalf("retained provider facts=%d error=%v", providerFacts, err)
		}
	})

	t.Run("legacy processing without a start remains unknown", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		ctx := harness.ctx
		// This models predecessor inventory; it creates no start or sender capability.
		if _, err := harness.fixture.db.Exec(ctx, `update public.record_notification_deliveries
			set delivery_state='processing',attempt_count=1,attempt_started_at=clock_timestamp()
			where delivery_id=$1`, harness.claim.Event.SubjectID); err != nil {
			t.Fatalf("seed predecessor processing projection: %v", err)
		}
		expireNotificationClaim(t, ctx, harness.fixture, harness.claim.Event.RowID)
		takeover := claimNotificationOutboxKind(t, ctx, harness.queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "legacy_delivery_takeover")
		recovered, err := harness.repository.PrepareExternalDelivery(ctx, takeover, "https://houfeng.example", time.Second)
		if err != nil || recovered.Prepared != nil || !recovered.Result.QueueFinalized || recovered.Result.Disposition != recordcollaboration.ExternalDeliveryOutboxComplete || harness.provider.calls != 0 {
			t.Fatalf("legacy processing takeover = %#v, %v; sends=%d", recovered, err, harness.provider.calls)
		}
		assertExternalDeliveryState(t, ctx, harness.fixture, harness.claim.Event.SubjectID, "unknown_outcome", "unknown_outcome", 1)
		var starts, facts, completions int
		if err := harness.fixture.db.QueryRow(ctx, `select
			(select count(*) from public.record_notification_delivery_starts where delivery_id=$1),
			(select count(*) from public.record_notification_delivery_facts where record_id=$2),
			(select count(*) from public.record_notification_delivery_completions where delivery_id=$1 and attempt_id is null)`,
			harness.claim.Event.SubjectID, harness.created.RecordID).Scan(&starts, &facts, &completions); err != nil || starts != 0 || facts != 0 || completions != 1 {
			t.Fatalf("legacy retained starts=%d facts=%d completions=%d error=%v", starts, facts, completions, err)
		}
	})
}

func TestPostgresIntegrationRecordExternalDeliveryRechecksEveryPreSendAuthority(t *testing.T) {
	for _, test := range []struct {
		name       string
		mutate     func(*testing.T, *postgresExternalDeliveryHarness)
		wantError  bool
		wantState  string
		wantOutbox recordcollaboration.ExternalDeliveryOutboxDisposition
	}{
		{
			name: "membership revoked",
			mutate: func(t *testing.T, harness *postgresExternalDeliveryHarness) {
				if _, err := harness.fixture.db.Exec(harness.ctx, `update public.users set role = 'viewer' where user_id = 'usr_bbbbbbbbbbbbbbbbbbbbbbbb'`); err != nil {
					t.Fatal(err)
				}
			},
			wantState: "cancelled", wantOutbox: recordcollaboration.ExternalDeliveryOutboxCancel,
		},
		{
			name: "authorization epoch stale",
			mutate: func(t *testing.T, harness *postgresExternalDeliveryHarness) {
				if _, err := harness.fixture.db.Exec(harness.ctx, `update public.records set authorization_epoch = authorization_epoch + 1 where record_id = $1`, harness.created.RecordID); err != nil {
					t.Fatal(err)
				}
			},
			wantState: "cancelled", wantOutbox: recordcollaboration.ExternalDeliveryOutboxCancel,
		},
		{
			name: "record fence stale",
			mutate: func(t *testing.T, harness *postgresExternalDeliveryHarness) {
				harness.fixture.advanceContentDeliveryEpoch(t, harness.ctx, recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID})
			},
			wantState: "cancelled", wantOutbox: recordcollaboration.ExternalDeliveryOutboxCancel,
		},
		{
			name: "current source unavailable",
			mutate: func(t *testing.T, harness *postgresExternalDeliveryHarness) {
				if _, err := harness.fixture.db.Exec(harness.ctx, `delete from public.vps_assets where vps_id = $1`, harness.sourceID); err != nil {
					t.Fatal(err)
				}
			},
			wantError: true, wantState: "pending",
		},
		{
			name: "source retirement coverage unknown",
			mutate: func(t *testing.T, harness *postgresExternalDeliveryHarness) {
				installNotificationCoverageUnknown(t, harness.ctx, harness.fixture, harness.sourceID)
			},
			wantError: true, wantState: "pending",
		},
		{
			name: "record deletion reserved",
			mutate: func(t *testing.T, harness *postgresExternalDeliveryHarness) {
				seedExternalDeliveryDeletionReservation(t, harness)
			},
			wantState: "cancelled", wantOutbox: recordcollaboration.ExternalDeliveryOutboxCancel,
		},
		{
			name: "binding unbound",
			mutate: func(_ *testing.T, harness *postgresExternalDeliveryHarness) {
				harness.bindings.byUser = nil
			},
			wantState: "cancelled", wantOutbox: recordcollaboration.ExternalDeliveryOutboxCancel,
		},
		{
			name: "binding dependency unavailable",
			mutate: func(_ *testing.T, harness *postgresExternalDeliveryHarness) {
				harness.bindings.resolveErr = errors.New("binding backend unavailable: credential=secret")
			},
			wantError: true, wantState: "pending",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newPostgresExternalDeliveryHarness(t)
			test.mutate(t, harness)
			result, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(harness.ctx, harness.claim)
			if test.wantError {
				if !errors.Is(err, recordcollaboration.ErrExternalDeliveryUnavailable) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("ProcessExternalDelivery() error = %v, want sanitized dependency unavailable", err)
				}
			} else if err != nil || result.Disposition != test.wantOutbox {
				t.Fatalf("ProcessExternalDelivery() = (%#v, %v), want %q", result, err, test.wantOutbox)
			}
			if harness.provider.calls != 0 {
				t.Fatalf("provider calls = %d, want zero", harness.provider.calls)
			}
			var state string
			if err := harness.fixture.db.QueryRow(harness.ctx, `select delivery_state from public.record_notification_deliveries where delivery_id = $1`, harness.claim.Event.SubjectID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != test.wantState {
				t.Fatalf("delivery state = %q, want %q", state, test.wantState)
			}
		})
	}
}

func TestPostgresIntegrationRecordExternalDeliveryFencedTerminalAndMissingEpoch(t *testing.T) {
	installGate := func(t *testing.T, harness *postgresExternalDeliveryHarness) {
		t.Helper()
		// This isolated gate tests an existing owner's terminal path; it is not
		// evidence of production issuer or birth admission.
		operation := recorddeletion.DeletionOperation{
			ContractVersion: recorddeletion.DeletionContractV2,
			OperationID:     "rpo_deliveryterminal",
			ReservationID:   "drs_deliveryterminal",
			Object:          recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: harness.created.RecordID},
			FenceEpoch:      1,
		}
		seedLocalAttachmentDeletionOperation(t, harness.ctx, harness.fixture, operation, harness.created.RevisionID)
		if _, err := harness.fixture.db.Exec(harness.ctx, `update public.content_delivery_epochs set delivery_epoch=1
			where project_id='default' and object_kind='record' and object_id=$1`, harness.created.RecordID); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("live owner records cancellation after gate", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		installGate(t, harness)
		result, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(harness.ctx, harness.claim)
		if err != nil || result.Disposition != recordcollaboration.ExternalDeliveryOutboxCancel || harness.provider.calls != 0 {
			t.Fatalf("gated delivery = (%#v, %v), sends=%d", result, err, harness.provider.calls)
		}
		assertExternalDeliveryState(t, harness.ctx, harness.fixture, harness.claim.Event.SubjectID, "cancelled", "cancelled", 1)
		if err := harness.queue.CancelOutbox(harness.ctx, harness.claim); err != nil {
			t.Fatalf("finish gated outbox cancellation: %v", err)
		}
		var status, reason string
		var starts int
		if err := harness.fixture.db.QueryRow(harness.ctx, `select o.status, d.reason_code,
			(select count(*)::int from public.record_notification_delivery_starts s where s.delivery_id=d.delivery_id)
			from public.record_outbox o join public.record_notification_deliveries d on d.delivery_id=o.subject_id
			where o.outbox_row_id=$1`, harness.claim.Event.RowID).Scan(&status, &reason, &starts); err != nil {
			t.Fatal(err)
		}
		if status != "cancelled" || reason != "authorization_revoked" || starts != 0 {
			t.Fatalf("gated outbox/delivery/starts = %q/%q/%d", status, reason, starts)
		}
	})
	t.Run("stale owner cannot cancel after gate", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		installGate(t, harness)
		expireNotificationClaim(t, harness.ctx, harness.fixture, harness.claim.Event.RowID)
		_, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(harness.ctx, harness.claim)
		if !errors.Is(err, recordplatform.ErrLostOwnerLease) || harness.provider.calls != 0 {
			t.Fatalf("stale gated owner error=%v sends=%d", err, harness.provider.calls)
		}
		assertExternalDeliveryState(t, harness.ctx, harness.fixture, harness.claim.Event.SubjectID, "pending", "", 0)
	})
	t.Run("missing epoch stays missing", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		if _, err := harness.fixture.db.Exec(harness.ctx, `delete from public.content_delivery_epochs
			where project_id='default' and object_kind='record' and object_id=$1`, harness.created.RecordID); err != nil {
			t.Fatal(err)
		}
		_, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(harness.ctx, harness.claim)
		if !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) || harness.provider.calls != 0 {
			t.Fatalf("delivery with missing epoch error=%v sends=%d", err, harness.provider.calls)
		}
		assertExternalDeliveryState(t, harness.ctx, harness.fixture, harness.claim.Event.SubjectID, "pending", "", 0)
		var epochs int
		if err := harness.fixture.db.QueryRow(harness.ctx, `select count(*)::int from public.content_delivery_epochs
			where project_id='default' and object_kind='record' and object_id=$1`, harness.created.RecordID).Scan(&epochs); err != nil || epochs != 0 {
			t.Fatalf("missing epoch count=%d error=%v", epochs, err)
		}
	})
	t.Run("admission failure remains unavailable", func(t *testing.T) {
		harness := newPostgresExternalDeliveryHarness(t)
		installGate(t, harness)
		harness.repository.platform.gate = AdmissionGateFunc(func(context.Context, pgx.Tx) error {
			return errors.New("admission unavailable: credential=secret")
		})
		_, err := newPostgresExternalDeliveryProcessor(t, harness.repository).ProcessExternalDelivery(harness.ctx, harness.claim)
		if !errors.Is(err, recordcollaboration.ErrExternalDeliveryUnavailable) || strings.Contains(err.Error(), "secret") || harness.provider.calls != 0 {
			t.Fatalf("unavailable admission error=%v sends=%d", err, harness.provider.calls)
		}
		assertExternalDeliveryState(t, harness.ctx, harness.fixture, harness.claim.Event.SubjectID, "pending", "", 0)
	})
}

func TestPostgresIntegrationRecordInboxRecomputesCurrentRecipientAndProjectionReconciles(t *testing.T) {
	t.Run("mute hides optional but preserves mandatory", func(t *testing.T) {
		ctx := context.Background()
		fixture := newAdmittedCoreRecordFixture(t, ctx)
		seedCollaborationRevisionUsers(t, ctx, fixture)
		seedPostgresCollaborationSource(t, ctx, fixture)
		runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-inbox-current-mute", 4)
		input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
		parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "inbox-current-mute-parent"))
		if err != nil {
			t.Fatalf("CommitRevision(parent) error = %v", err)
		}
		seedNotificationPreferences(t, ctx, fixture, parent.RecordID, map[string]string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb": "watching"})
		actions := newPostgresActionRepositoryForTest(t, runtimePool, fixture.admissionGate)
		optionalCreate := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pginboxoptional", 0,
			mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Optional completion"}), "inbox-current-optional-create")
		optionalComplete := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, optionalCreate.ActionID, 1,
			recordcollaboration.ActionFields{}, "inbox-current-optional-complete")
		mandatoryCreate := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pginboxmandatory", 0,
			mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Mandatory assignment", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}), "inbox-current-mandatory-create")
		for _, command := range []recordcollaboration.ActionCommand{optionalCreate, optionalComplete, mandatoryCreate} {
			if _, err := actions.CommitAction(ctx, command); err != nil {
				t.Fatalf("CommitAction(%s/%s) error = %v", command.ActionID, command.Kind, err)
			}
		}
		projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
		optionalClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionCompleted, "inbox_current_optional_a")
		optionalResult, err := projection.ProjectNotification(ctx, optionalClaim)
		if err != nil || optionalResult.RecipientCount != 1 {
			t.Fatalf("ProjectNotification(optional) = (%#v, %v)", optionalResult, err)
		}
		mandatoryClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, "inbox_current_mandatory")
		mandatoryResult, err := projection.ProjectNotification(ctx, mandatoryClaim)
		if err != nil || mandatoryResult.RecipientCount != 1 {
			t.Fatalf("ProjectNotification(mandatory) = (%#v, %v)", mandatoryResult, err)
		}
		if err := queue.MarkOutboxSent(ctx, mandatoryClaim); err != nil {
			t.Fatal(err)
		}
		actor := collaborationActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", nil)
		optionalRequest := recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: optionalResult.NotificationID}
		dismissed, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{
			Actor: actor, NotificationID: optionalResult.NotificationID, Kind: recordcollaboration.InboxTransitionDismiss,
		})
		if err != nil || dismissed.ReadAt == nil || dismissed.DismissedAt == nil {
			t.Fatalf("TransitionInbox(optional dismiss) = (%#v, %v)", dismissed, err)
		}
		expireNotificationClaim(t, ctx, fixture, optionalClaim.Event.RowID)
		preserveClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionCompleted, "inbox_current_optional_b")
		if replay, err := projection.ProjectNotification(ctx, preserveClaim); err != nil || replay != optionalResult {
			t.Fatalf("ProjectNotification(valid replay) = (%#v, %v), want %#v", replay, err, optionalResult)
		}
		preserved, err := projection.GetInboxItem(ctx, optionalRequest)
		if err != nil || preserved.ReadAt == nil || preserved.DismissedAt == nil ||
			!preserved.ReadAt.Equal(*dismissed.ReadAt) || !preserved.DismissedAt.Equal(*dismissed.DismissedAt) {
			t.Fatalf("valid replay did not preserve inbox state: (%#v, %v)", preserved, err)
		}
		expireNotificationClaim(t, ctx, fixture, preserveClaim.Event.RowID)
		muteClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionCompleted, "inbox_current_optional_c")
		if _, err := fixture.db.Exec(ctx, `update public.record_followers set manual_preference = 'muted' where record_id = $1 and user_id = $2`, parent.RecordID, actor.UserID); err != nil {
			t.Fatalf("mute projected optional recipient: %v", err)
		}
		for _, statement := range []string{
			`create function record_platform_internal.test_notification_reconcile_rollback() returns trigger
			 language plpgsql as $$ begin raise exception using errcode = 'P0001', message = 'notification recipient reconcile cut point'; end $$`,
			`create trigger test_notification_reconcile_rollback before delete on public.record_notification_recipients
			 for each row execute function record_platform_internal.test_notification_reconcile_rollback()`,
		} {
			if _, err := fixture.db.Exec(ctx, statement); err != nil {
				t.Fatalf("install notification reconcile rollback cut point: %v", err)
			}
		}
		if replay, err := projection.ProjectNotification(ctx, muteClaim); err == nil || replay != (recordcollaboration.NotificationProjectionResult{}) || !strings.Contains(err.Error(), "notification recipient reconcile cut point") {
			t.Fatalf("ProjectNotification(reconcile cut point) = (%#v, %v)", replay, err)
		}
		var rollbackReadAt, rollbackDismissedAt *time.Time
		if err := fixture.db.QueryRow(ctx, `select read_at, dismissed_at from public.record_notification_recipients where notification_id = $1 and recipient_user_id = $2`, optionalResult.NotificationID, actor.UserID).Scan(&rollbackReadAt, &rollbackDismissedAt); err != nil {
			t.Fatalf("read reconcile rollback recipient: %v", err)
		}
		if rollbackReadAt == nil || rollbackDismissedAt == nil || !rollbackReadAt.Equal(*dismissed.ReadAt) || !rollbackDismissedAt.Equal(*dismissed.DismissedAt) {
			t.Fatalf("reconcile rollback lost read/dismiss state: %v/%v", rollbackReadAt, rollbackDismissedAt)
		}
		if _, err := fixture.db.Exec(ctx, `drop trigger test_notification_reconcile_rollback on public.record_notification_recipients`); err != nil {
			t.Fatal(err)
		}
		if replay, err := projection.ProjectNotification(ctx, muteClaim); err != nil || replay != (recordcollaboration.NotificationProjectionResult{}) {
			t.Fatalf("ProjectNotification(muted replay) = (%#v, %v), want empty", replay, err)
		}
		var optionalRecipients int
		if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.record_notification_recipients where notification_id = $1`, optionalResult.NotificationID).Scan(&optionalRecipients); err != nil {
			t.Fatal(err)
		}
		if optionalRecipients != 0 {
			t.Fatalf("muted replay recipients = %d, want exact zero reconciliation", optionalRecipients)
		}
		listRequest := recordcollaboration.InboxListRequest{Actor: actor, Limit: 100}
		items, err := projection.ListInbox(ctx, listRequest)
		if err != nil || len(items) != 1 || items[0].NotificationID != mandatoryResult.NotificationID || !items[0].Mandatory {
			t.Fatalf("ListInbox(after mute) = (%#v, %v), want only mandatory", items, err)
		}
		if count, err := projection.CountUnreadInbox(ctx, listRequest); err != nil || count != 1 {
			t.Fatalf("CountUnreadInbox(after mute) = (%d, %v), want 1", count, err)
		}
		for _, operation := range []struct {
			name string
			call func() error
		}{
			{name: "item", call: func() error { _, err := projection.GetInboxItem(ctx, optionalRequest); return err }},
			{name: "target", call: func() error { _, err := projection.GetInboxDeepLink(ctx, optionalRequest); return err }},
			{name: "transition", call: func() error {
				_, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: optionalResult.NotificationID, Kind: recordcollaboration.InboxTransitionRead})
				return err
			}},
		} {
			if err := operation.call(); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
				t.Fatalf("%s muted optional error = %v, want opaque not found", operation.name, err)
			}
		}
		if _, err := projection.GetInboxItem(ctx, recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: mandatoryResult.NotificationID}); err != nil {
			t.Fatalf("mandatory item hidden by mute: %v", err)
		}
	})

	t.Run("unwatch hides manual optional recipient", func(t *testing.T) {
		ctx := context.Background()
		fixture := newAdmittedCoreRecordFixture(t, ctx)
		seedCollaborationRevisionUsers(t, ctx, fixture)
		seedPostgresCollaborationSource(t, ctx, fixture)
		runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-inbox-current-unwatch", 3)
		input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
		parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "inbox-current-unwatch-parent"))
		if err != nil {
			t.Fatal(err)
		}
		watcherID := "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
		seedNotificationPreferences(t, ctx, fixture, parent.RecordID, map[string]string{watcherID: "watching"})
		actions := newPostgresActionRepositoryForTest(t, runtimePool, fixture.admissionGate)
		create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pginboxunwatch", 0,
			mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Manual watcher"}), "inbox-unwatch-create")
		complete := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 1,
			recordcollaboration.ActionFields{}, "inbox-unwatch-complete")
		for _, command := range []recordcollaboration.ActionCommand{create, complete} {
			if _, err := actions.CommitAction(ctx, command); err != nil {
				t.Fatal(err)
			}
		}
		projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
		claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionCompleted, "inbox_unwatch_optional")
		result, err := projection.ProjectNotification(ctx, claim)
		if err != nil || result.RecipientCount != 1 {
			t.Fatalf("ProjectNotification(optional) = (%#v, %v)", result, err)
		}
		if err := queue.MarkOutboxSent(ctx, claim); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(ctx, `delete from public.record_followers where record_id = $1 and user_id = $2`, parent.RecordID, watcherID); err != nil {
			t.Fatalf("unwatch manual recipient: %v", err)
		}
		actor := collaborationActor(t, watcherID, nil)
		listRequest := recordcollaboration.InboxListRequest{Actor: actor, Limit: 100}
		if items, err := projection.ListInbox(ctx, listRequest); err != nil || items == nil || len(items) != 0 {
			t.Fatalf("ListInbox(after unwatch) = (%#v, %v), want nonnil empty", items, err)
		}
		if count, err := projection.CountUnreadInbox(ctx, listRequest); err != nil || count != 0 {
			t.Fatalf("CountUnreadInbox(after unwatch) = (%d, %v), want 0", count, err)
		}
		itemRequest := recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: result.NotificationID}
		if _, err := projection.GetInboxItem(ctx, itemRequest); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
			t.Fatalf("GetInboxItem(after unwatch) error = %v", err)
		}
		if _, err := projection.GetInboxDeepLink(ctx, itemRequest); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
			t.Fatalf("GetInboxDeepLink(after unwatch) error = %v", err)
		}
		if _, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{Actor: actor, NotificationID: result.NotificationID, Kind: recordcollaboration.InboxTransitionRead}); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
			t.Fatalf("TransitionInbox(after unwatch) error = %v", err)
		}
	})
}

func TestPostgresIntegrationRecordInboxScanBudgetCachesAndStableMultiRecordOrder(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-inbox-bounded", 4)
	inputs := []records.CompleteRevisionInput{
		collaborationRevisionInput(t, collaborationRevisionInputValues{title: "Bounded inbox A"}),
		collaborationRevisionInput(t, collaborationRevisionInputValues{title: "Bounded inbox B"}),
	}
	recordRepository := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate})
	parents := make([]records.RevisionCommitResult, 0, 2)
	for index := range inputs {
		input := inputs[index]
		parent, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, fmt.Sprintf("inbox-bounded-parent-%d", index)))
		if err != nil {
			t.Fatalf("CommitRevision(%d) error = %v", index, err)
		}
		parents = append(parents, parent)
		command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, fmt.Sprintf("ract_pginboxbounded%d", index), 0,
			mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Bounded inbox", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}), fmt.Sprintf("inbox-bounded-action-%d", index))
		if _, err := newPostgresActionRepositoryForTest(t, runtimePool, fixture.admissionGate).CommitAction(ctx, command); err != nil {
			t.Fatalf("CommitAction(%d) error = %v", index, err)
		}
	}
	projection := NewPostgresRecordNotificationRepository(
		runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
		newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate), 30*24*time.Hour,
	)
	queue := NewPostgresRecordPlatformRepository(runtimePool, fixture.admissionGate)
	validIDs := make([]string, 0, 2)
	for index := range parents {
		claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, fmt.Sprintf("inbox_bounded_project_%d", index))
		result, err := projection.ProjectNotification(ctx, claim)
		if err != nil || result.RecipientCount != 1 {
			t.Fatalf("ProjectNotification(%d) = (%#v, %v)", index, result, err)
		}
		validIDs = append(validIDs, result.NotificationID)
		if err := queue.MarkOutboxSent(ctx, claim); err != nil {
			t.Fatal(err)
		}
	}
	actor := collaborationActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", nil)
	staleEpoch := int64(parents[0].AuthorizationEpoch + 1)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_notifications (
			notification_id, project_id, record_id, event_kind, subject_kind, subject_id,
			source_version, actor_id, authorization_epoch, record_fence_epoch, event_at, details_delete_after
		)
		select 'rnt_' || lpad(to_hex(1000 + series), 64, '0'), 'default', $1,
		       'record_owner_changed', 'record', $1, 1000 + series, 'usr_aaaaaaaaaaaaaaaaaaaaaaaa', $2, 0,
		       transaction_timestamp() + (series * interval '1 second'), transaction_timestamp() + interval '30 days'
		from generate_series(1, $3) series`, parents[0].RecordID, staleEpoch, inboxScanBudget); err != nil {
		t.Fatalf("seed hidden inbox budget window notifications: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_notification_recipients (
			notification_id, record_id, recipient_user_id, reason_kind, mandatory, authorization_epoch, record_fence_epoch
		)
		select notification_id, record_id, $3, 'follower', false, authorization_epoch, record_fence_epoch
		from public.record_notifications
		where record_id = $1 and authorization_epoch = $2`, parents[0].RecordID, staleEpoch, actor.UserID); err != nil {
		t.Fatalf("seed hidden inbox budget window recipients: %v", err)
	}
	items, err := projection.ListInbox(ctx, recordcollaboration.InboxListRequest{Actor: actor, Limit: 100})
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("ListInbox(over budget) = (%#v, %v), want bounded nonnil empty", items, err)
	}
	for _, notificationID := range validIDs {
		if _, err := projection.GetInboxItem(ctx, recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: notificationID}); err != nil {
			t.Fatalf("valid item beyond scan window %s unavailable directly: %v", notificationID, err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.record_notification_recipients where record_id = $1 and authorization_epoch = $2`, parents[0].RecordID, staleEpoch); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.record_notifications where record_id = $1 and authorization_epoch = $2`, parents[0].RecordID, staleEpoch); err != nil {
		t.Fatal(err)
	}
	items, err = projection.ListInbox(ctx, recordcollaboration.InboxListRequest{Actor: actor, Limit: 100})
	if err != nil || len(items) != 2 {
		t.Fatalf("ListInbox(multi record) = (%#v, %v), want 2", items, err)
	}
	if items[0].EventAt.Before(items[1].EventAt) || (items[0].EventAt.Equal(items[1].EventAt) && items[0].NotificationID > items[1].NotificationID) {
		t.Fatalf("multi-record stable order = %#v", items)
	}
}

func TestPostgresIntegrationRecordInboxSourceDependencyFailureIsUnavailableForEveryOperation(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-inbox-source-dependency", 3)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
	parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "inbox-dependency-parent"))
	if err != nil {
		t.Fatalf("CommitRevision(parent) error = %v", err)
	}
	actionRepository := newPostgresActionRepositoryForTest(t, runtimePool, fixture.admissionGate)
	create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pginboxdependency", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Dependency failure", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}),
		"inbox-dependency-action")
	if _, err := actionRepository.CommitAction(ctx, create); err != nil {
		t.Fatalf("CommitAction() error = %v", err)
	}
	projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
	claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, "inbox_dependency_worker")
	projected, err := projection.ProjectNotification(ctx, claim)
	if err != nil || projected.RecipientCount == 0 {
		t.Fatalf("ProjectNotification() = (%#v, %v), want recipients", projected, err)
	}
	second := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pginboxdependencysecond", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Unprojected", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}),
		"inbox-dependency-second-action")
	if _, err := actionRepository.CommitAction(ctx, second); err != nil {
		t.Fatalf("CommitAction(second) error = %v", err)
	}
	secondClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, "inbox_dependency_second_worker")
	actor := collaborationActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", nil)
	itemRequest := recordcollaboration.InboxItemRequest{Actor: actor, NotificationID: projected.NotificationID}
	if _, err := projection.GetInboxItem(ctx, itemRequest); err != nil {
		t.Fatalf("otherwise-authorized inbox item unavailable before dependency failure: %v", err)
	}
	installNotificationCoverageUnknown(t, ctx, fixture, input.Subjects()[0].SourceID)
	if _, err := projection.ProjectNotification(ctx, claim); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("ProjectNotification(replay with unavailable prefetch) error = %v", err)
	}
	if _, err := projection.ProjectNotification(ctx, secondClaim); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("ProjectNotification(new with unavailable prefetch) error = %v", err)
	}
	assertPostgresNotificationCounts(t, ctx, fixture, parent.RecordID, 1, 1)

	operations := []struct {
		name string
		call func() error
	}{
		{name: "item", call: func() error { _, err := projection.GetInboxItem(ctx, itemRequest); return err }},
		{name: "list", call: func() error {
			_, err := projection.ListInbox(ctx, recordcollaboration.InboxListRequest{Actor: actor, Limit: 10})
			return err
		}},
		{name: "count", call: func() error {
			_, err := projection.CountUnreadInbox(ctx, recordcollaboration.InboxListRequest{Actor: actor, Limit: 10})
			return err
		}},
		{name: "target", call: func() error { _, err := projection.GetInboxDeepLink(ctx, itemRequest); return err }},
		{name: "transition", call: func() error {
			_, err := projection.TransitionInbox(ctx, recordcollaboration.InboxTransitionRequest{
				Actor: actor, NotificationID: projected.NotificationID, Kind: recordcollaboration.InboxTransitionRead,
			})
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			err := operation.call()
			if !errors.Is(err, ErrRecordSubjectUnavailable) || errors.Is(err, recordcollaboration.ErrInboxNotFound) {
				t.Fatalf("source prefetch error = %v, want ErrRecordSubjectUnavailable only", err)
			}
		})
	}
	var readAt, dismissedAt *time.Time
	if err := fixture.db.QueryRow(ctx, `select read_at, dismissed_at from public.record_notification_recipients where notification_id = $1 and recipient_user_id = $2`, projected.NotificationID, actor.UserID).Scan(&readAt, &dismissedAt); err != nil {
		t.Fatalf("read recipient after failed prefetch: %v", err)
	}
	if readAt != nil || dismissedAt != nil {
		t.Fatalf("failed prefetch changed recipient state: read=%v dismissed=%v", readAt, dismissedAt)
	}
}

func TestPostgresIntegrationRecordNotificationActionMappingMandatoryMuteAndReasonPrecedence(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-actions", 4)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
	parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "notification-action-parent"))
	if err != nil {
		t.Fatalf("CommitRevision(action parent) error = %v", err)
	}
	seedNotificationPreferences(t, ctx, fixture, parent.RecordID, map[string]string{
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb": "muted",
		"usr_cccccccccccccccccccccccc": "watching",
		"usr_dddddddddddddddddddddddd": "watching",
	})
	actionRepository := newPostgresActionRepositoryForTest(t, runtimePool, fixture.admissionGate)
	create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgnotify", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Notify assignment", AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"}), "notification-action-create")
	update := postgresActionCommand(t, parent, recordcollaboration.ActionMutationUpdate, create.ActionID, 1,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Notify reassignment", AssigneeID: "usr_cccccccccccccccccccccccc"}), "notification-action-update")
	complete := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 2, recordcollaboration.ActionFields{}, "notification-action-complete")
	reopen := postgresActionCommand(t, parent, recordcollaboration.ActionMutationReopen, create.ActionID, 3, recordcollaboration.ActionFields{}, "notification-action-reopen")
	cancel := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCancel, create.ActionID, 4, recordcollaboration.ActionFields{}, "notification-action-cancel")
	for _, command := range []recordcollaboration.ActionCommand{create, update, complete, reopen, cancel} {
		if _, err := actionRepository.CommitAction(ctx, command); err != nil {
			t.Fatalf("CommitAction(%s) error = %v", command.Kind, err)
		}
	}
	for _, want := range []struct {
		userID, preference string
		version            int
	}{
		{userID: create.Actor.UserID, preference: "default", version: 1},
		{userID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", preference: "muted", version: 2},
		{userID: "usr_cccccccccccccccccccccccc", preference: "watching", version: 2},
	} {
		var preference string
		var version, fence int
		var followsAuthor, followsOwner, followsParticipant, followsComment, followsMention, followsAction bool
		if err := fixture.db.QueryRow(ctx, `
			select follower_version::int, manual_preference,
			       follows_author, follows_owner, follows_participant,
			       follows_comment, follows_mention, follows_action, record_fence_epoch::int
			from public.record_followers
			where record_id = $1 and user_id = $2`, parent.RecordID, want.userID).Scan(
			&version, &preference, &followsAuthor, &followsOwner, &followsParticipant,
			&followsComment, &followsMention, &followsAction, &fence,
		); err != nil {
			t.Fatalf("read automatic action source for %q: %v", want.userID, err)
		}
		if version != want.version || preference != want.preference || followsAuthor || followsOwner || followsParticipant ||
			followsComment || followsMention || !followsAction || fence != 0 {
			t.Fatalf("automatic action source for %q = v%d/%s/%v/%v/%v/%v/%v/%v/f%d", want.userID,
				version, preference, followsAuthor, followsOwner, followsParticipant, followsComment, followsMention, followsAction, fence)
		}
	}
	projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
	wants := []struct {
		kind       string
		version    uint64
		recipients int
		assignee   string
	}{
		{recordplatform.OutboxEventKindRecordActionAssigned, 1, 3, "usr_bbbbbbbbbbbbbbbbbbbbbbbb"},
		{recordplatform.OutboxEventKindRecordActionAssigned, 2, 2, "usr_cccccccccccccccccccccccc"},
		{recordplatform.OutboxEventKindRecordActionCompleted, 3, 2, "usr_cccccccccccccccccccccccc"},
		{recordplatform.OutboxEventKindRecordActionCancelled, 5, 2, "usr_cccccccccccccccccccccccc"},
	}
	for index, want := range wants {
		claim := claimNotificationOutboxKind(t, ctx, queue, want.kind, "notification_action_worker_"+string(rune('a'+index)))
		if claim.Event.SubjectID != create.ActionID || claim.Event.SourceVersion != want.version {
			t.Fatalf("claim %q identity = %#v, want version %d", want.kind, claim.Event, want.version)
		}
		result, err := projection.ProjectNotification(ctx, claim)
		if err != nil || result.RecipientCount != want.recipients {
			t.Fatalf("ProjectNotification(%q/v%d) = (%#v, %v), want %d recipients", want.kind, want.version, result, err, want.recipients)
		}
		if err := queue.MarkOutboxSent(ctx, claim); err != nil {
			t.Fatalf("MarkOutboxSent(%q/v%d) error = %v", want.kind, want.version, err)
		}
		var reason string
		var mandatory bool
		if err := fixture.db.QueryRow(ctx, `
			select reason_kind, mandatory
			from public.record_notification_recipients
			where notification_id = $1 and recipient_user_id = $2`, result.NotificationID, want.assignee).Scan(&reason, &mandatory); err != nil {
			t.Fatalf("read assignee recipient %q/v%d: %v", want.kind, want.version, err)
		}
		if reason != "assignee" || !mandatory {
			t.Fatalf("assignee reason/mandatory = %q/%v", reason, mandatory)
		}
	}
}

func TestPostgresIntegrationRecordNotificationUnassignedCompletionAndCancellationReachOptionalWatcher(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-unassigned-action", 3)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
	parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "notification-unassigned-parent"))
	if err != nil {
		t.Fatalf("CommitRevision(parent) error = %v", err)
	}
	seedNotificationPreferences(t, ctx, fixture, parent.RecordID, map[string]string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb": "watching"})
	repository := newPostgresActionRepositoryForTest(t, runtimePool, fixture.admissionGate)
	create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgnotifyunassigned", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Unassigned action"}), "notification-unassigned-create")
	complete := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 1, recordcollaboration.ActionFields{}, "notification-unassigned-complete")
	reopen := postgresActionCommand(t, parent, recordcollaboration.ActionMutationReopen, create.ActionID, 2, recordcollaboration.ActionFields{}, "notification-unassigned-reopen")
	cancel := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCancel, create.ActionID, 3, recordcollaboration.ActionFields{}, "notification-unassigned-cancel")
	for _, command := range []recordcollaboration.ActionCommand{create, complete, reopen, cancel} {
		if _, err := repository.CommitAction(ctx, command); err != nil {
			t.Fatalf("CommitAction(%s) error = %v", command.Kind, err)
		}
	}
	projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
	for _, want := range []struct {
		kind    string
		version uint64
	}{
		{kind: recordplatform.OutboxEventKindRecordActionCompleted, version: 2},
		{kind: recordplatform.OutboxEventKindRecordActionCancelled, version: 4},
	} {
		claim := claimNotificationOutboxKind(t, ctx, queue, want.kind, "notification_unassigned_"+want.kind)
		if claim.Event.SubjectID != create.ActionID || claim.Event.SourceVersion != want.version || claim.Event.RecordFenceEpoch != 0 {
			t.Fatalf("unassigned %q claim = %#v", want.kind, claim.Event)
		}
		result, err := projection.ProjectNotification(ctx, claim)
		if err != nil || result.RecipientCount != 1 {
			t.Fatalf("ProjectNotification(%q) = (%#v, %v), want one optional watcher", want.kind, result, err)
		}
		var recipient, reason string
		var mandatory bool
		if err := fixture.db.QueryRow(ctx, `
			select recipient_user_id, reason_kind, mandatory
			from public.record_notification_recipients where notification_id = $1`, result.NotificationID).Scan(&recipient, &reason, &mandatory); err != nil {
			t.Fatalf("read unassigned %q recipient: %v", want.kind, err)
		}
		if recipient != "usr_bbbbbbbbbbbbbbbbbbbbbbbb" || reason != "follower" || mandatory {
			t.Fatalf("unassigned %q recipient = %q/%q/%v", want.kind, recipient, reason, mandatory)
		}
		if err := queue.MarkOutboxSent(ctx, claim); err != nil {
			t.Fatalf("MarkOutboxSent(%q) error = %v", want.kind, err)
		}
	}
}

func TestPostgresIntegrationRecordNotificationSuppressesSelfAssignmentAndSelfMention(t *testing.T) {
	t.Run("self assignment", func(t *testing.T) {
		ctx := context.Background()
		fixture := newAdmittedCoreRecordFixture(t, ctx)
		seedCollaborationRevisionUsers(t, ctx, fixture)
		seedPostgresCollaborationSource(t, ctx, fixture)
		runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-self-assign", 3)
		input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
		parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "notification-self-assign-parent"))
		if err != nil {
			t.Fatalf("CommitRevision(parent) error = %v", err)
		}
		repository := newPostgresActionRepositoryForTest(t, runtimePool, fixture.admissionGate)
		command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgnotifyselfassign", 0,
			mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Self assignment", AssigneeID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa"}), "notification-self-assign-create")
		command.Actor = mustPostgresCommentActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", recordauth.RoleProjectAdmin)
		if _, err := repository.CommitAction(ctx, command); err != nil {
			t.Fatalf("CommitAction(self assignment) error = %v", err)
		}
		projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
		claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, "notification_self_assign")
		result, err := projection.ProjectNotification(ctx, claim)
		if err != nil || result != (recordcollaboration.NotificationProjectionResult{}) {
			t.Fatalf("ProjectNotification(self assignment) = (%#v, %v), want empty result", result, err)
		}
		assertPostgresNotificationCounts(t, ctx, fixture, parent.RecordID, 0, 0)
		if err := queue.MarkOutboxSent(ctx, claim); err != nil {
			t.Fatalf("MarkOutboxSent(self assignment) error = %v", err)
		}
	})

	t.Run("self mention", func(t *testing.T) {
		ctx := context.Background()
		fixture := newAdmittedCoreRecordFixture(t, ctx)
		seedCollaborationRevisionUsers(t, ctx, fixture)
		seedPostgresCollaborationSource(t, ctx, fixture)
		runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-self-mention", 3)
		input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
		parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "notification-self-mention-parent"))
		if err != nil {
			t.Fatalf("CommitRevision(parent) error = %v", err)
		}
		repository := newPostgresCommentRepositoryForTest(t, runtimePool, fixture.admissionGate)
		command := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate, "rcm_pgnotifyselfmention", 0,
			"Self mention.", "", []string{"usr_aaaaaaaaaaaaaaaaaaaaaaaa"}, "notification-self-mention-create")
		if _, err := repository.CommitComment(ctx, command); err != nil {
			t.Fatalf("CommitComment(self mention) error = %v", err)
		}
		projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
		claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordCommentMentioned, "notification_self_mention")
		result, err := projection.ProjectNotification(ctx, claim)
		if err != nil || result != (recordcollaboration.NotificationProjectionResult{}) {
			t.Fatalf("ProjectNotification(self mention) = (%#v, %v), want empty result", result, err)
		}
		assertPostgresNotificationCounts(t, ctx, fixture, parent.RecordID, 0, 0)
		if err := queue.MarkOutboxSent(ctx, claim); err != nil {
			t.Fatalf("MarkOutboxSent(self mention) error = %v", err)
		}
	})
}

func TestPostgresIntegrationRecordNotificationCommentReplyMentionMapping(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-comments", 4)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
	parent, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "notification-comment-parent"))
	if err != nil {
		t.Fatalf("CommitRevision(comment parent) error = %v", err)
	}
	seedNotificationPreferences(t, ctx, fixture, parent.RecordID, map[string]string{
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb": "muted",
		"usr_cccccccccccccccccccccccc": "muted",
		"usr_dddddddddddddddddddddddd": "watching",
	})
	commentRepository := newPostgresCommentRepositoryForTest(t, runtimePool, fixture.admissionGate)
	parentComment := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate, "rcm_pgnotifyparent", 0, "Parent", "", nil, "notification-comment-create-parent")
	parentComment.Actor = mustPostgresCommentActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", recordauth.RoleProjectAdmin)
	if _, err := commentRepository.CommitComment(ctx, parentComment); err != nil {
		t.Fatalf("CommitComment(parent) error = %v", err)
	}
	reply := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate, "rcm_pgnotifyreply", 0,
		"Reply to parent and mention.", parentComment.CommentID, []string{"usr_cccccccccccccccccccccccc"}, "notification-comment-create-reply")
	if _, err := commentRepository.CommitComment(ctx, reply); err != nil {
		t.Fatalf("CommitComment(reply) error = %v", err)
	}
	edit := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationEdit, reply.CommentID, 1,
		"Edited reply with same mention.", "", []string{"usr_cccccccccccccccccccccccc"}, "notification-comment-edit-reply")
	if _, err := commentRepository.CommitComment(ctx, edit); err != nil {
		t.Fatalf("CommitComment(edit reply) error = %v", err)
	}
	for _, want := range []struct {
		userID, preference string
		version            int
		comment, mention   bool
		author             bool
	}{
		{userID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", preference: "default", version: 1, comment: true},
		{userID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", preference: "muted", version: 2, comment: true},
		{userID: "usr_cccccccccccccccccccccccc", preference: "muted", version: 2, mention: true},
	} {
		var preference string
		var version, fence int
		var followsAuthor, followsOwner, followsParticipant, followsComment, followsMention, followsAction bool
		if err := fixture.db.QueryRow(ctx, `
			select follower_version::int, manual_preference,
			       follows_author, follows_owner, follows_participant,
			       follows_comment, follows_mention, follows_action, record_fence_epoch::int
			from public.record_followers
			where record_id = $1 and user_id = $2`, parent.RecordID, want.userID).Scan(
			&version, &preference, &followsAuthor, &followsOwner, &followsParticipant,
			&followsComment, &followsMention, &followsAction, &fence,
		); err != nil {
			t.Fatalf("read automatic comment sources for %q: %v", want.userID, err)
		}
		if version != want.version || preference != want.preference || followsAuthor != want.author || followsOwner || followsParticipant ||
			followsComment != want.comment || followsMention != want.mention || followsAction || fence != 0 {
			t.Fatalf("automatic comment sources for %q = v%d/%s/%v/%v/%v/%v/%v/%v/f%d", want.userID,
				version, preference, followsAuthor, followsOwner, followsParticipant, followsComment, followsMention, followsAction, fence)
		}
	}
	projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, fixture.admissionGate)
	repliedClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordCommentReplied, "notification_comment_reply")
	replied, err := projection.ProjectNotification(ctx, repliedClaim)
	if err != nil || replied.RecipientCount != 1 {
		t.Fatalf("ProjectNotification(reply) = (%#v, %v), want only watcher after muted parent", replied, err)
	}
	if err := queue.MarkOutboxSent(ctx, repliedClaim); err != nil {
		t.Fatal(err)
	}
	mentionedClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordCommentMentioned, "notification_comment_mention")
	mentioned, err := projection.ProjectNotification(ctx, mentionedClaim)
	if err != nil || mentioned.RecipientCount != 2 {
		t.Fatalf("ProjectNotification(mention) = (%#v, %v), want mandatory mention plus watcher", mentioned, err)
	}
	if err := queue.MarkOutboxSent(ctx, mentionedClaim); err != nil {
		t.Fatal(err)
	}
	var reason string
	var mandatory bool
	if err := fixture.db.QueryRow(ctx, `
		select reason_kind, mandatory from public.record_notification_recipients
		where notification_id = $1 and recipient_user_id = 'usr_cccccccccccccccccccccccc'`, mentioned.NotificationID).Scan(&reason, &mandatory); err != nil {
		t.Fatalf("read mention recipient: %v", err)
	}
	if reason != "mention" || !mandatory {
		t.Fatalf("mention reason/mandatory = %q/%v", reason, mandatory)
	}
	var mutedReplyRecipient int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_notification_recipients
		where notification_id = $1 and recipient_user_id = 'usr_bbbbbbbbbbbbbbbbbbbbbbbb'`, replied.NotificationID).Scan(&mutedReplyRecipient); err != nil {
		t.Fatalf("count muted reply recipient: %v", err)
	}
	if mutedReplyRecipient != 0 {
		t.Fatalf("muted optional reply recipients = %d, want 0", mutedReplyRecipient)
	}
}

func newPostgresNotificationProjectionHarness(t *testing.T, pool *pgxpool.Pool, gate AdmissionGate) (*PostgresRecordNotificationRepository, *PostgresRecordPlatformRepository) {
	t.Helper()
	authorization := newRecordsPostgresAuthorization(t, pool, gate)
	return NewPostgresRecordNotificationRepository(pool, gate, NewPostgresCollaborationMembershipReader(), authorization, 30*24*time.Hour),
		NewPostgresRecordPlatformRepository(pool, gate)
}

func installNotificationCoverageUnknown(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, sourceID string) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.source_retirement_coverage_unknown
			(project_id, source_kind, source_id, recovery_id, installed_generation, prepare_ids)
		values ('default', 'vps', $1, 'recovery_test', 1, array['retirement_unknown'])`, sourceID); err != nil {
		t.Fatalf("install unknown source coverage: %v", err)
	}
}

type notificationPreclaimedQueue struct {
	claim *recordplatform.ClaimedOutboxEventV1
	queue *PostgresRecordPlatformRepository
}

type notificationBindingListerStub struct {
	byUser     map[string][]recordcollaboration.ScopedTransportBindingRef
	err        error
	resolveErr error
	provider   recordcollaboration.ExternalDeliveryProvider
}

func (reader *notificationBindingListerStub) ListScopedTransportBindings(
	_ context.Context,
	_ pgx.Tx,
	projectID recordauth.ProjectID,
	recipientUserID string,
) ([]recordcollaboration.ScopedTransportBindingRef, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	if projectID != recordauth.ProjectIDDefault {
		return nil, errors.New("unexpected project")
	}
	return append([]recordcollaboration.ScopedTransportBindingRef(nil), reader.byUser[recipientUserID]...), nil
}

func (reader *notificationBindingListerStub) ResolveScopedTransportBinding(
	_ context.Context,
	_ pgx.Tx,
	want recordcollaboration.ScopedTransportBindingRef,
) (recordcollaboration.ScopedTransportBinding, error) {
	if reader.resolveErr != nil {
		return recordcollaboration.ScopedTransportBinding{}, reader.resolveErr
	}
	for _, candidate := range reader.byUser[want.RecipientUserID] {
		if candidate == want {
			return recordcollaboration.ScopedTransportBinding{
				ProjectID: candidate.ProjectID, RecipientUserID: candidate.RecipientUserID,
				Channel: candidate.Channel, BindingID: candidate.BindingID, Provider: reader.provider,
			}, nil
		}
	}
	return recordcollaboration.ScopedTransportBinding{}, recordcollaboration.ErrScopedTransportBindingNotFound
}

type notificationBindingProviderStub struct {
	outcome  recordcollaboration.ExternalDeliveryProviderOutcome
	calls    int
	messages []recordcollaboration.SafeExternalDelivery
}

func (provider *notificationBindingProviderStub) SendExternalDelivery(_ context.Context, message recordcollaboration.SafeExternalDelivery) recordcollaboration.ExternalDeliveryProviderOutcome {
	provider.calls++
	provider.messages = append(provider.messages, message)
	if provider.outcome == "" {
		return recordcollaboration.ExternalDeliveryProviderSent
	}
	return provider.outcome
}

type postgresExternalDeliveryHarness struct {
	ctx        context.Context
	fixture    recordPlatformPostgresFixture
	repository *PostgresRecordNotificationRepository
	queue      *PostgresRecordPlatformRepository
	claim      recordplatform.ClaimedOutboxEventV1
	provider   *notificationBindingProviderStub
	bindings   *notificationBindingListerStub
	sourceID   string
	created    records.RevisionCommitResult
}

func newPostgresExternalDeliveryHarness(t *testing.T) *postgresExternalDeliveryHarness {
	t.Helper()
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-external-delivery-matrix", 5)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
	})
	created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader())).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}, input, "external-delivery-matrix-parent"))
	if err != nil {
		t.Fatal(err)
	}
	authorization := newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate)
	provider := &notificationBindingProviderStub{}
	bindings := &notificationBindingListerStub{
		provider: provider,
		byUser: map[string][]recordcollaboration.ScopedTransportBindingRef{
			"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {{
				ProjectID: recordauth.ProjectIDDefault, RecipientUserID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
				Channel: recordcollaboration.NotificationDeliveryTelegram, BindingID: "telegram_primary",
			}},
		},
	}
	repository := NewPostgresRecordNotificationRepositoryWithExternalBindings(
		runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
		authorization, 30*24*time.Hour, bindings,
	)
	queue := NewPostgresRecordPlatformRepository(runtimePool, fixture.admissionGate)
	parentClaim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordOwnerChanged, "external_matrix_parent")
	if _, err := repository.ProjectNotification(ctx, parentClaim); err != nil {
		t.Fatal(err)
	}
	if err := queue.MarkOutboxSent(ctx, parentClaim); err != nil {
		t.Fatal(err)
	}
	claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordNotificationDelivery, "external_matrix_delivery")
	return &postgresExternalDeliveryHarness{
		ctx: ctx, fixture: fixture, repository: repository, queue: queue, claim: claim,
		provider: provider, bindings: bindings, sourceID: input.Subjects()[0].SourceID, created: created,
	}
}

type postgresExternalDeliveryClock struct{}

func (postgresExternalDeliveryClock) Now() time.Time { return time.Now().UTC() }

func newPostgresExternalDeliveryProcessor(t *testing.T, repository *PostgresRecordNotificationRepository) *recordcollaboration.ScopedExternalDeliveryProcessor {
	t.Helper()
	processor, err := recordcollaboration.NewScopedExternalDeliveryProcessor(
		repository,
		recordcollaboration.ScopedExternalDeliveryProcessorOptions{
			PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second, SendTimeout: 10 * time.Second,
			Clock: postgresExternalDeliveryClock{},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func forceExternalDeliveryRetryDue(t *testing.T, harness *postgresExternalDeliveryHarness, outboxRowID int64) {
	t.Helper()
	if _, err := harness.fixture.db.Exec(harness.ctx, `
		update public.record_outbox set next_attempt_at = transaction_timestamp() - interval '1 second'
		where outbox_row_id = $1`, outboxRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.fixture.db.Exec(harness.ctx, `
		update public.record_notification_deliveries set next_attempt_at = transaction_timestamp() - interval '1 second'
		where delivery_id = $1`, harness.claim.Event.SubjectID); err != nil {
		t.Fatal(err)
	}
}

func seedExternalDeliveryDeletionReservation(t *testing.T, harness *postgresExternalDeliveryHarness) {
	t.Helper()
	if _, err := harness.fixture.db.Exec(harness.ctx, `
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
			'drs_externaldelivery', 'default', 'record', $1,
			decode(repeat('41', 32), 'hex'), decode(repeat('42', 32), 'hex'),
			decode(repeat('43', 32), 'hex'), decode(repeat('44', 32), 'hex'),
			$2, $3, $4, 0,
			decode(repeat('45', 32), 'hex'), decode(repeat('46', 32), 'hex'),
			decode(repeat('47', 32), 'hex'), decode(repeat('48', 32), 'hex'),
			decode(repeat('49', 32), 'hex'), 1,
			decode(repeat('4a', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		)`, harness.created.RecordID, harness.created.RevisionID, harness.created.LockVersion, harness.created.AuthorizationEpoch); err != nil {
		t.Fatal(err)
	}
}

func (queue *notificationPreclaimedQueue) ClaimOutbox(context.Context, recordplatform.OutboxClaimInputV1) (*recordplatform.ClaimedOutboxEventV1, error) {
	claim := queue.claim
	queue.claim = nil
	return claim, nil
}

func (queue *notificationPreclaimedQueue) CancelOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) error {
	return queue.queue.CancelOutbox(ctx, claim)
}

func (queue *notificationPreclaimedQueue) RetryOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1, delay time.Duration) error {
	return queue.queue.RetryOutbox(ctx, claim, delay)
}

func (queue *notificationPreclaimedQueue) MarkOutboxSent(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) error {
	return queue.queue.MarkOutboxSent(ctx, claim)
}

func seedNotificationPreferences(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, recordID string, preferences map[string]string) {
	t.Helper()
	for userID, preference := range preferences {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.record_followers (
				project_id, record_id, user_id, follower_version, manual_preference, record_fence_epoch
			) values ('default', $1, $2, 1, $3, 0)`, recordID, userID, preference); err != nil {
			t.Fatalf("seed notification preference %s/%s: %v", userID, preference, err)
		}
	}
}

func claimNotificationOutboxKind(t *testing.T, ctx context.Context, queue *PostgresRecordPlatformRepository, kind, ownerID string) recordplatform.ClaimedOutboxEventV1 {
	t.Helper()
	for attempt := 0; attempt < 8; attempt++ {
		claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: ownerID, OwnerLeaseDuration: time.Minute})
		if err != nil {
			t.Fatalf("ClaimOutbox(%q) error = %v", kind, err)
		}
		if claim == nil {
			t.Fatalf("ClaimOutbox(%q) returned no event", kind)
		}
		if claim.Event.EventKind == kind {
			return *claim
		}
		if err := queue.CancelOutbox(ctx, *claim); err != nil {
			t.Fatalf("CancelOutbox(%q) error = %v", claim.Event.EventKind, err)
		}
	}
	t.Fatalf("outbox kind %q not found", kind)
	return recordplatform.ClaimedOutboxEventV1{}
}

func expireNotificationClaim(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, rowID int64) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		update public.record_outbox
		set owner_expires_at = transaction_timestamp() - interval '1 second'
		where outbox_row_id = $1 and status = 'processing'`, rowID); err != nil {
		t.Fatalf("expire notification claim: %v", err)
	}
}

func assertPostgresNotificationCounts(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, recordID string, wantNotifications, wantRecipients int) {
	t.Helper()
	var notifications, recipients int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_notifications where record_id = $1),
		       (select count(*)::int from public.record_notification_recipients where record_id = $1)`, recordID).Scan(&notifications, &recipients); err != nil {
		t.Fatalf("count projected notifications: %v", err)
	}
	if notifications != wantNotifications || recipients != wantRecipients {
		t.Fatalf("projected notifications/recipients = %d/%d, want %d/%d", notifications, recipients, wantNotifications, wantRecipients)
	}
}

func assertExternalDeliveryState(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	deliveryID, wantState, wantOutcome string,
	wantAttempts int,
) {
	t.Helper()
	var state string
	var attemptCount, attempts int
	var outcome string
	if err := fixture.db.QueryRow(ctx, `
		select delivery_state, attempt_count,
		       (select count(*)::int from public.record_notification_delivery_attempts where delivery_id = $1),
		       coalesce((select max(outcome) from public.record_notification_delivery_attempts where delivery_id = $1), '')
		from public.record_notification_deliveries where delivery_id = $1`, deliveryID).Scan(
		&state, &attemptCount, &attempts, &outcome,
	); err != nil {
		t.Fatal(err)
	}
	if state != wantState || attemptCount != wantAttempts || attempts != wantAttempts || outcome != wantOutcome {
		t.Fatalf("delivery state/count/attempts/outcome = %q/%d/%d/%q, want %q/%d/%d/%q", state, attemptCount, attempts, outcome, wantState, wantAttempts, wantAttempts, wantOutcome)
	}
}
