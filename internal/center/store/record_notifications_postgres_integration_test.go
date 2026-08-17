package store

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
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
				_, err := NewPostgresRecordActionRepository(
					fixture.openDirectRuntimePool(t, ctx, "record-source-action-rollback", 1), gate, NewPostgresCollaborationMembershipReader(),
				).CommitAction(ctx, command)
				return command.Idempotency.Key.Key, err
			},
		},
		{
			name: "comment", subjectKind: recordplatform.OutboxSubjectKindComment,
			commit: func(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, parent records.RevisionCommitResult, gate AdmissionGate) (string, error) {
				command := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate, "rcm_pgsourcerollback", 0,
					"Rollback source.", "", []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}, "notification-source-comment-rollback")
				_, err := NewPostgresRecordCommentRepository(
					fixture.openDirectRuntimePool(t, ctx, "record-source-comment-rollback", 1), gate, NewPostgresCollaborationMembershipReader(),
				).CommitComment(ctx, command)
				return command.Idempotency.Key.Key, err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newRecordsPostgresFixture(t, ctx)
			seedCollaborationRevisionUsers(t, ctx, fixture)
			parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgsource"+test.name, "notification-source-"+test.name+"-parent")
			cutPoint := errors.New("automatic follower completion admission cut point")
			var admissions atomic.Int32
			gate := AdmissionGateFunc(func(context.Context, pgx.Tx) error {
				if admissions.Add(1) == 3 {
					return cutPoint
				}
				return nil
			})
			key, err := test.commit(t, ctx, fixture, parent, gate)
			if !errors.Is(err, cutPoint) || admissions.Load() != 3 {
				t.Fatalf("commit error/admissions = %v/%d, want completion cut point/3", err, admissions.Load())
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

func TestPostgresIntegrationRecordNotificationProjectionFencesTakeoverAndReplaysAtomically(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-projection", 4)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", participantIDs: []string{"usr_cccccccccccccccccccccccc"},
	})
	recordRepository := newRecordsPostgresRepository(
		t, runtimePool, NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
	)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_pgnotification", "", 0, 0, input, "notification-parent-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	storedSubject := input.Subjects()[0]
	identity, err := records.NewSubjectIdentitySnapshot(storedSubject.Kind, storedSubject.IdentitySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeCurrentRecordSubjectResolver{resolved: records.ResolvedSubject{
		ProjectID: recordauth.ProjectIDDefault, StableID: storedSubject.SourceID,
		IdentitySnapshot: identity, LiveRoute: "/vps/" + storedSubject.SourceID,
		CaptureAuthorization: storedSubject.CaptureAuthorization,
	}}
	authorization := newPostgresCurrentRecordAuthorizationSource(runtimePool, resolver, allowRecordPlatformAdmissionGate)
	projection := NewPostgresRecordNotificationRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(), authorization, 30*24*time.Hour,
	)
	queue := NewPostgresRecordPlatformRepository(runtimePool, allowRecordPlatformAdmissionGate)

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

	liveAuthorization := storedSubject.CaptureAuthorization
	revokedFloor := collaborationVisibility(t, recordauth.VisibilityKindRestricted, []string{"rag_revoked"})
	resolver.resolved.CaptureAuthorization = collaborationSourceAuthorization(t, liveAuthorization.CaptureScope, revokedFloor, recordauth.SourceStateTombstoned)
	if got, err := projection.ListInbox(ctx, listRequest); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListInbox(after source revoke) = (%#v, %v), want nonnil empty", got, err)
	}
	if _, err := projection.GetInboxDeepLink(ctx, itemRequest); !errors.Is(err, recordcollaboration.ErrInboxNotFound) {
		t.Fatalf("GetInboxDeepLink(after source revoke) error = %v, want opaque not found", err)
	}
	resolver.resolved.CaptureAuthorization = liveAuthorization
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

func TestPostgresIntegrationRecordNotificationActionMappingMandatoryMuteAndReasonPrecedence(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-actions", 4)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
	parent, err := newRecordsPostgresRepository(t, runtimePool).CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_pgnotifyaction", "", 0, 0, input, "notification-action-parent",
	))
	if err != nil {
		t.Fatalf("CommitRevision(action parent) error = %v", err)
	}
	seedNotificationPreferences(t, ctx, fixture, parent.RecordID, map[string]string{
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb": "muted",
		"usr_cccccccccccccccccccccccc": "watching",
		"usr_dddddddddddddddddddddddd": "watching",
	})
	actionRepository := NewPostgresRecordActionRepository(runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader())
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
	projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, input)
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

func TestPostgresIntegrationRecordNotificationCommentReplyMentionMapping(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-notification-comments", 4)
	input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
	parent, err := newRecordsPostgresRepository(t, runtimePool).CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_pgnotifycomment", "", 0, 0, input, "notification-comment-parent",
	))
	if err != nil {
		t.Fatalf("CommitRevision(comment parent) error = %v", err)
	}
	seedNotificationPreferences(t, ctx, fixture, parent.RecordID, map[string]string{
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb": "muted",
		"usr_cccccccccccccccccccccccc": "muted",
		"usr_dddddddddddddddddddddddd": "watching",
	})
	commentRepository := NewPostgresRecordCommentRepository(runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader())
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
	projection, queue := newPostgresNotificationProjectionHarness(t, runtimePool, input)
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

func newPostgresNotificationProjectionHarness(t *testing.T, pool *pgxpool.Pool, input records.CompleteRevisionInput) (*PostgresRecordNotificationRepository, *PostgresRecordPlatformRepository) {
	t.Helper()
	storedSubject := input.Subjects()[0]
	identity, err := records.NewSubjectIdentitySnapshot(storedSubject.Kind, storedSubject.IdentitySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeCurrentRecordSubjectResolver{resolved: records.ResolvedSubject{
		ProjectID: recordauth.ProjectIDDefault, StableID: storedSubject.SourceID,
		IdentitySnapshot: identity, LiveRoute: "/vps/" + storedSubject.SourceID,
		CaptureAuthorization: storedSubject.CaptureAuthorization,
	}}
	authorization := newPostgresCurrentRecordAuthorizationSource(pool, resolver, allowRecordPlatformAdmissionGate)
	return NewPostgresRecordNotificationRepository(pool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(), authorization, 30*24*time.Hour),
		NewPostgresRecordPlatformRepository(pool, allowRecordPlatformAdmissionGate)
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
