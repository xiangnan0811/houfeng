package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationRecordRevisionConcurrentSameBaseHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	seedRepository := newRevisionMutationAdmittedRepository(t, fixture)
	seedCommand := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Initial record"), "records-race-seed")
	seed, err := seedRepository.CommitRevision(ctx, seedCommand)
	if err != nil {
		t.Fatalf("CommitRevision() seed error = %v", err)
	}
	if !seed.Created || seed.RevisionNo != 1 || seed.LockVersion != 1 || seed.AuthorizationEpoch != 1 {
		t.Fatalf("seed result = %#v", seed)
	}

	commands := []records.RevisionCommitCommand{
		recordsPostgresRevisionCommand(
			t,
			recordplatform.OperationKindRecordUpdate,
			seed.RecordID,
			seed.RevisionID,
			seed.LockVersion,
			seed.AuthorizationEpoch,
			recordsPostgresCompleteRevisionInput(t, "Concurrent update A"),
			"records-race-a",
		),
		recordsPostgresRevisionCommand(
			t,
			recordplatform.OperationKindRecordUpdate,
			seed.RecordID,
			seed.RevisionID,
			seed.LockVersion,
			seed.AuthorizationEpoch,
			recordsPostgresCompleteRevisionInput(t, "Concurrent update B"),
			"records-race-b",
		),
	}

	type revisionOutcome struct {
		result records.RevisionCommitResult
		err    error
	}
	outcomes := make(chan revisionOutcome, len(commands))
	start := make(chan struct{})
	for _, command := range commands {
		workerFixture := fixture
		workerFixture.Runtime = revisionBirthBatchRuntimePool(t, ctx, fixture.Runtime)
		repository := newRevisionMutationAdmittedRepository(t, workerFixture)
		command := command
		go func() {
			<-start
			result, err := repository.CommitRevision(context.Background(), command)
			outcomes <- revisionOutcome{result: result, err: err}
		}()
	}
	close(start)

	var winner records.RevisionCommitResult
	var winnerCount int
	var conflictCount int
	for range commands {
		outcome := waitForRecordPlatformResult(t, outcomes, "concurrent record revision")
		switch {
		case outcome.err == nil:
			winner = outcome.result
			winnerCount++
		case errors.Is(outcome.err, records.ErrRecordRevisionConflict):
			conflictCount++
		default:
			t.Fatalf("concurrent CommitRevision() = (%#v, %v), want winner or ErrRecordRevisionConflict", outcome.result, outcome.err)
		}
	}
	if winnerCount != 1 || conflictCount != 1 || !winner.Created || winner.RevisionNo != 2 ||
		winner.LockVersion != 2 || winner.AuthorizationEpoch != 2 {
		t.Fatalf("concurrent outcomes = winner %#v, winners/conflicts %d/%d", winner, winnerCount, conflictCount)
	}

	var (
		currentRevisionID  string
		currentRevisionNo  int64
		lockVersion        int64
		authorizationEpoch int64
		currentTitle       string
		revisionTitle      string
		revisionCount      int
		activityCount      int
		outboxCount        int
		idempotencyCount   int
	)
	if err := fixture.Runtime.QueryRow(ctx, `
		select roots.current_revision_id,
		       revisions.revision_no,
		       roots.lock_version,
		       roots.authorization_epoch,
		       roots.current_title,
		       revisions.title
		from public.records roots
		join public.record_revisions revisions
		  on revisions.record_id = roots.record_id
		 and revisions.revision_id = roots.current_revision_id
		where roots.record_id = $1`, seed.RecordID).Scan(
		&currentRevisionID,
		&currentRevisionNo,
		&lockVersion,
		&authorizationEpoch,
		&currentTitle,
		&revisionTitle,
	); err != nil {
		t.Fatalf("read reconciled current record: %v", err)
	}
	if err := fixture.Runtime.QueryRow(ctx, `
		select (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $1),
		       (select count(*)::int from public.record_outbox where subject_kind = 'record' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys
		         where operation_kind in ('record_create', 'record_update')
		           and idempotency_key in ('records-race-seed', 'records-race-a', 'records-race-b'))`, seed.RecordID).Scan(
		&revisionCount,
		&activityCount,
		&outboxCount,
		&idempotencyCount,
	); err != nil {
		t.Fatalf("count concurrent record rows: %v", err)
	}
	if currentRevisionID != winner.RevisionID || currentRevisionNo != 2 || lockVersion != 2 ||
		authorizationEpoch != 2 || currentTitle != revisionTitle {
		t.Fatalf("current reconciliation = id %q no %d lock/auth %d/%d titles %q/%q, winner %#v",
			currentRevisionID, currentRevisionNo, lockVersion, authorizationEpoch, currentTitle, revisionTitle, winner)
	}
	if revisionCount != 2 || activityCount != 2 || outboxCount != 2 || idempotencyCount != 2 {
		t.Fatalf("concurrent row counts = revisions %d activities %d outbox %d idempotency %d, want 2/2/2/2",
			revisionCount, activityCount, outboxCount, idempotencyCount)
	}
	assertRecordsPostgresCurrentProjectionReconciled(t, ctx, fixture.Runtime, winner.RecordID, winner.RevisionID)
}

func TestPostgresIntegrationRecordRevisionRetryReplaysWithoutDuplicateRows(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	repository := newRevisionMutationAdmittedRepository(t, fixture)
	command := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Retry-safe record"), "records-retry-key")

	first, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("CommitRevision() first error = %v", err)
	}
	retryCommand := command
	retryCommand.Idempotency.OwnerID = "records_pg_retry"
	retryFixture := fixture
	retryFixture.Runtime = revisionBirthBatchRuntimePool(t, ctx, fixture.Runtime)
	replayed, err := newRevisionMutationAdmittedRepository(t, retryFixture).CommitRevision(ctx, retryCommand)
	if err != nil {
		t.Fatalf("CommitRevision() replay error = %v", err)
	}
	if !first.Created || first.Replayed || !replayed.Created || !replayed.Replayed ||
		first.RecordID != replayed.RecordID || first.RevisionID != replayed.RevisionID ||
		first.RevisionNo != replayed.RevisionNo || first.LockVersion != replayed.LockVersion ||
		first.AuthorizationEpoch != replayed.AuthorizationEpoch || !first.CommittedAt.Equal(replayed.CommittedAt) {
		t.Fatalf("retry results = first %#v replay %#v", first, replayed)
	}
	conflict := recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordCreate, command.RecordID, "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "A different request"), command.Idempotency.Key.Key)
	if result, err := repository.CommitRevision(ctx, conflict); !errors.Is(err, recordplatform.ErrIdempotencyKeyReused) || result != (records.RevisionCommitResult{}) {
		t.Fatalf("same revision key with different request = (%#v, %v)", result, err)
	}

	var (
		rootCount        int
		revisionCount    int
		subjectCount     int
		tagCount         int
		participantCount int
		activityCount    int
		outboxCount      int
		idempotencyCount int
	)
	if err := fixture.Runtime.QueryRow(ctx, `
		select (select count(*)::int from public.records where record_id = $1),
		       (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.record_revision_subjects where revision_id = $2),
		       (select count(*)::int from public.record_revision_tags where revision_id = $2),
		       (select count(*)::int from public.record_revision_participants where revision_id = $2),
		       (select count(*)::int from public.record_domain_activities where record_id = $1),
		       (select count(*)::int from public.record_outbox where subject_kind = 'record' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys
		         where operation_kind = 'record_create' and idempotency_key = 'records-retry-key')`, first.RecordID, first.RevisionID).Scan(
		&rootCount,
		&revisionCount,
		&subjectCount,
		&tagCount,
		&participantCount,
		&activityCount,
		&outboxCount,
		&idempotencyCount,
	); err != nil {
		t.Fatalf("count replay rows: %v", err)
	}
	if rootCount != 1 || revisionCount != 1 || subjectCount != 1 || tagCount != 1 ||
		participantCount != 1 || activityCount != 1 || outboxCount != 1 || idempotencyCount != 1 {
		t.Fatalf("retry row counts = root %d revision %d subject %d tag %d participant %d activity %d outbox %d idempotency %d, want all 1",
			rootCount, revisionCount, subjectCount, tagCount, participantCount, activityCount, outboxCount, idempotencyCount)
	}
}

func TestPostgresIntegrationRecordImportFinishRejectsDifferentActor(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	repository := newRevisionMutationAdmittedRepository(t, fixture)
	input := recordsPostgresCompleteRevisionInput(t, "Import actor")
	actor := recordauth.ActorScope{UserID: input.AuthorID(), Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault}
	allocation := reserveRevisionBirth(t, ctx, fixture, actor, "records-import-actor", recordbirth.PurposeImport)
	command := recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordCreate, allocation.RecordID, "", 0, 0, input, "records-import-actor")
	command = prepareRevisionBirthCommand(t, ctx, fixture, allocation, command)
	if err := command.Validate(); err != nil {
		t.Fatalf("invalid import revision fixture: %v", err)
	}
	for _, mismatch := range []string{"user", "role", "groups"} {
		t.Run(mismatch, func(t *testing.T) {
			actor := command.Actor.Clone()
			switch mismatch {
			case "user":
				actor.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
			case "role":
				actor.Role = recordauth.RoleViewer
			case "groups":
				actor.GroupIDs = []string{"rag_aaaaaaaaaaaaaaaaaaaaaaaa"}
			}
			finish := records.RevisionCommitFinish{Actor: actor, OriginKind: "import", OriginDigest: [32]byte{1},
				ImportJobID: "rij_importactor", JobLockVersion: 1}
			if err := finish.Validate(); err != nil {
				t.Fatalf("invalid import finish fixture: %v", err)
			}
			if results, err := repository.CommitRevisionsFinishing(ctx, []records.RevisionCommitCommand{command}, finish); !errors.Is(err, records.ErrInvalidRevisionCommand) || results != nil {
				t.Fatalf("cross-actor import = (%#v, %v), want rejected batch", results, err)
			}
			var roots, revisions, claims, epochs int
			if err := fixture.Runtime.QueryRow(ctx, `select
				(select count(*) from public.records where record_id=$1),
				(select count(*) from public.record_revisions where record_id=$1),
				(select count(*) from public.record_idempotency_keys where idempotency_key=$2),
				(select count(*) from public.content_delivery_epochs where object_kind='record' and object_id=$1)`,
				command.RecordID, command.Idempotency.Key.Key).Scan(&roots, &revisions, &claims, &epochs); err != nil {
				t.Fatalf("read rejected import state: %v", err)
			}
			if roots != 0 || revisions != 0 || claims != 0 || epochs != 0 {
				t.Fatalf("cross-actor import roots/revisions/claims/epochs = %d/%d/%d/%d", roots, revisions, claims, epochs)
			}
		})
	}
}

func TestPostgresIntegrationRecordRevisionParticipantFailureLeavesNoHalfCommit(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	participantErr := errors.New("participant failed")
	participant := &storeRevisionParticipantStub{
		name: "failing_projection",
		resources: func(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
			return records.RevisionMutationResources{}, nil
		},
		apply: func(context.Context, pgx.Tx, records.RevisionCommitted) error {
			return participantErr
		},
	}
	repository := newRevisionMutationAdmittedRepository(t, fixture, participant)
	command := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Rollback record"), "records-participant-failure")

	if result, err := repository.CommitRevision(ctx, command); !errors.Is(err, participantErr) {
		t.Fatalf("CommitRevision() = (%#v, %v), want participant failure", result, err)
	}

	var (
		rootCount        int
		revisionCount    int
		subjectCount     int
		tagCount         int
		participantCount int
		activityCount    int
		outboxCount      int
		idempotencyCount int
	)
	if err := fixture.Runtime.QueryRow(ctx, `
		select (select count(*)::int from public.records where record_id = $1),
		       (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.record_revision_subjects subjects
		         join public.record_revisions revisions on revisions.revision_id = subjects.revision_id
		         where revisions.record_id = $1),
		       (select count(*)::int from public.record_revision_tags tags
		         join public.record_revisions revisions on revisions.revision_id = tags.revision_id
		         where revisions.record_id = $1),
		       (select count(*)::int from public.record_revision_participants participants
		         join public.record_revisions revisions on revisions.revision_id = participants.revision_id
		         where revisions.record_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $1),
		       (select count(*)::int from public.record_outbox where subject_kind = 'record' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys
		         where operation_kind = 'record_create' and idempotency_key = 'records-participant-failure')`, command.RecordID).Scan(
		&rootCount,
		&revisionCount,
		&subjectCount,
		&tagCount,
		&participantCount,
		&activityCount,
		&outboxCount,
		&idempotencyCount,
	); err != nil {
		t.Fatalf("count participant rollback rows: %v", err)
	}
	if rootCount != 0 || revisionCount != 0 || subjectCount != 0 || tagCount != 0 ||
		participantCount != 0 || activityCount != 0 || outboxCount != 0 || idempotencyCount != 0 {
		t.Fatalf("participant rollback counts = root %d revision %d subject %d tag %d participant %d activity %d outbox %d idempotency %d, want all 0",
			rootCount, revisionCount, subjectCount, tagCount, participantCount, activityCount, outboxCount, idempotencyCount)
	}
	allocation, found, err := NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil).FindAllocation(ctx, command.BirthRequest)
	if err != nil || !found || allocation.State != recordbirth.StatePrepared {
		t.Fatalf("rolled back birth = %+v, found=%v, error=%v", allocation, found, err)
	}
}

func TestPostgresIntegrationRecordAttachmentRevisionParticipantTransfersAndRollsBackAtomically(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	fixture := signed.platform
	runtimePool := signed.Runtime
	draftRepository := NewPostgresRecordDraftRepository(runtimePool, signed.Gate)
	participant := NewRecordAttachmentRevisionParticipant()
	repository := newRevisionMutationAdmittedRepository(t, signed, participant)

	draft := createRecordsPostgresDraft(
		t, ctx, draftRepository, "rdf_pgattachmentpublish", recordsPostgresDraftPayload(t, "Attachment publish"),
	)
	seedRecordsPostgresRevisionAttachment(t, ctx, fixture, "att_pgattachmentfirst", draft.DraftID, attachments.UploadStateAvailable, "41")
	seedRecordsPostgresRevisionAttachment(t, ctx, fixture, "att_pgattachmentsecond", draft.DraftID, attachments.UploadStateAvailable, "42")
	input := recordsPostgresCompleteRevisionInput(
		t, "Attachment publish", "att_pgattachmentsecond", "att_pgattachmentfirst",
	)
	command := revisionBirthBatchCommand(t, ctx, signed, input, "records-attachment-publish")
	command.DraftID = draft.DraftID
	command.DraftETag = draft.ETag
	committed, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("CommitRevision(attachment publish) error = %v", err)
	}
	var ordered []string
	if err := fixture.db.QueryRow(ctx, `
		select array_agg(attachment_id order by ordinal)
		from public.record_revision_attachments
		where record_id = $1 and revision_id = $2`, committed.RecordID, committed.RevisionID).Scan(&ordered); err != nil {
		t.Fatalf("read ordered revision attachments: %v", err)
	}
	if len(ordered) != 2 || ordered[0] != "att_pgattachmentsecond" || ordered[1] != "att_pgattachmentfirst" {
		t.Fatalf("ordered revision attachments = %#v", ordered)
	}
	var transferred int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from public.record_attachments
		where record_id = $1 and draft_id is null
		  and attachment_id = any($2::text[])`, committed.RecordID, ordered).Scan(&transferred); err != nil {
		t.Fatalf("count transferred attachments: %v", err)
	}
	if transferred != 2 {
		t.Fatalf("transferred attachments = %d, want 2", transferred)
	}
	stored, err := repository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID: committed.RecordID, RevisionID: committed.RevisionID,
		CurrentRevisionID: committed.RevisionID, LockVersion: committed.LockVersion,
		AuthorizationEpoch: committed.AuthorizationEpoch,
	})
	if err != nil {
		t.Fatalf("ReadRecordRevision() error = %v", err)
	}
	if got := stored.Input.AttachmentIDs(); len(got) != 2 || got[0] != ordered[0] || got[1] != ordered[1] {
		t.Fatalf("ReadRecordRevision().AttachmentIDs = %#v", got)
	}

	reducedInput := recordsPostgresCompleteRevisionInput(t, "Attachment publish reduced", "att_pgattachmentfirst")
	reduced, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, committed.RecordID, committed.RevisionID,
		committed.LockVersion, committed.AuthorizationEpoch, reducedInput, "records-attachment-reduced",
	))
	if err != nil {
		t.Fatalf("CommitRevision(same-record reduced attachments) error = %v", err)
	}
	restoredInput := recordsPostgresCompleteRevisionInput(
		t, "Attachment publish restored", "att_pgattachmentsecond", "att_pgattachmentfirst",
	)
	restored, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, reduced.RecordID, reduced.RevisionID,
		reduced.LockVersion, reduced.AuthorizationEpoch, restoredInput, "records-attachment-restored",
	))
	if err != nil {
		t.Fatalf("CommitRevision(same-record restored attachments) error = %v", err)
	}
	var historicalFirstRefs, reducedRefs, restoredRefs []string
	if err := fixture.db.QueryRow(ctx, `
		select
		  array(select attachment_id from public.record_revision_attachments where revision_id = $1 order by ordinal),
		  array(select attachment_id from public.record_revision_attachments where revision_id = $2 order by ordinal),
		  array(select attachment_id from public.record_revision_attachments where revision_id = $3 order by ordinal)`,
		committed.RevisionID, reduced.RevisionID, restored.RevisionID,
	).Scan(&historicalFirstRefs, &reducedRefs, &restoredRefs); err != nil {
		t.Fatalf("read historical/reduced/restored attachment refs: %v", err)
	}
	if len(historicalFirstRefs) != 2 || historicalFirstRefs[0] != "att_pgattachmentsecond" || historicalFirstRefs[1] != "att_pgattachmentfirst" ||
		len(reducedRefs) != 1 || reducedRefs[0] != "att_pgattachmentfirst" ||
		len(restoredRefs) != 2 || restoredRefs[0] != "att_pgattachmentsecond" || restoredRefs[1] != "att_pgattachmentfirst" {
		t.Fatalf("historical/reduced/restored refs = %#v / %#v / %#v", historicalFirstRefs, reducedRefs, restoredRefs)
	}

	rollbackDraft := createRecordsPostgresDraft(
		t, ctx, draftRepository, "rdf_pgattachmentrollback", recordsPostgresDraftPayload(t, "Attachment rollback"),
	)
	seedRecordsPostgresRevisionAttachment(t, ctx, fixture, "att_pgrollbackavailable", rollbackDraft.DraftID, attachments.UploadStateAvailable, "43")
	seedRecordsPostgresRevisionAttachment(t, ctx, fixture, "att_pgrollbackpending", rollbackDraft.DraftID, attachments.UploadStateQuarantined, "44")
	rollbackCommand := revisionBirthBatchCommand(t, ctx, signed,
		recordsPostgresCompleteRevisionInput(t, "Attachment rollback", "att_pgrollbackavailable", "att_pgrollbackpending"),
		"records-attachment-rollback")
	rollbackCommand.DraftID = rollbackDraft.DraftID
	rollbackCommand.DraftETag = rollbackDraft.ETag
	if result, err := repository.CommitRevision(ctx, rollbackCommand); !errors.Is(err, attachments.ErrAttachmentConflict) {
		t.Fatalf("CommitRevision(attachment rollback) = (%#v, %v), want ErrAttachmentConflict", result, err)
	}
	var rootCount, revisionCount, refCount, retainedDraftOwnerCount int
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*)::int from public.records where record_id = $1),
		  (select count(*)::int from public.record_revisions where record_id = $1),
		  (select count(*)::int from public.record_revision_attachments where record_id = $1),
		  (select count(*)::int from public.record_attachments
		    where draft_id = $2 and record_id is null
		      and attachment_id in ('att_pgrollbackavailable', 'att_pgrollbackpending'))`,
		rollbackCommand.RecordID, rollbackDraft.DraftID,
	).Scan(&rootCount, &revisionCount, &refCount, &retainedDraftOwnerCount); err != nil {
		t.Fatalf("read attachment rollback state: %v", err)
	}
	if rootCount != 0 || revisionCount != 0 || refCount != 0 || retainedDraftOwnerCount != 2 {
		t.Fatalf("rollback state = root/revision/ref/draft-owner %d/%d/%d/%d", rootCount, revisionCount, refCount, retainedDraftOwnerCount)
	}
	for index, name := range []string{"foreign_draft", "foreign_record", "expired"} {
		t.Run(name, func(t *testing.T) {
			suffix := strings.ReplaceAll(name, "_", "")
			target := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_revisionreject"+suffix, recordsPostgresDraftPayload(t, name))
			attachmentID := "att_revisionreject" + suffix
			if name == "foreign_record" {
				attachmentID = "att_pgattachmentfirst"
			} else {
				ownerDraft := target.DraftID
				if name == "foreign_draft" {
					ownerDraft = rollbackDraft.DraftID
				}
				state := attachments.UploadStateAvailable
				if name == "expired" {
					state = attachments.UploadStateExpired
				}
				seedRecordsPostgresRevisionAttachment(t, ctx, fixture, attachmentID, ownerDraft, state, fmt.Sprintf("reject-%d", index))
			}
			var oldRecord, oldDraft string
			if err := fixture.db.QueryRow(ctx, `select coalesce(record_id,''),coalesce(draft_id,'') from public.record_attachments where attachment_id=$1`, attachmentID).Scan(&oldRecord, &oldDraft); err != nil {
				t.Fatal(err)
			}
			rejected := revisionBirthBatchCommand(t, ctx, signed, recordsPostgresCompleteRevisionInput(t, name, attachmentID), "revision-reject-"+name)
			rejected.DraftID, rejected.DraftETag = target.DraftID, target.ETag
			if _, err := repository.CommitRevision(ctx, rejected); !errors.Is(err, attachments.ErrAttachmentConflict) {
				t.Fatalf("foreign/unavailable publication: %v", err)
			}
			var roots, revisions, refs int
			var currentRecord, currentDraft string
			if err := fixture.db.QueryRow(ctx, `select (select count(*) from public.records where record_id=$1),(select count(*) from public.record_revisions where record_id=$1),(select count(*) from public.record_revision_attachments where record_id=$1),coalesce(record_id,''),coalesce(draft_id,'') from public.record_attachments where attachment_id=$2`, rejected.RecordID, attachmentID).Scan(&roots, &revisions, &refs, &currentRecord, &currentDraft); err != nil {
				t.Fatal(err)
			}
			if roots != 0 || revisions != 0 || refs != 0 || currentRecord != oldRecord || currentDraft != oldDraft {
				t.Fatalf("rejected publication mutated durable state: %d/%d/%d owners %s/%s -> %s/%s", roots, revisions, refs, oldRecord, oldDraft, currentRecord, currentDraft)
			}
		})
	}
}

func TestPostgresIntegrationRecordRevisionNoChangeAndLifecycleReplayPreserveRevisionAuthority(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	fixture := signed.platform
	repository := newRevisionMutationAdmittedRepository(t, signed)
	input := recordsPostgresCompleteRevisionInput(t, "Lifecycle record")
	createCommand := revisionBirthBatchCommand(t, ctx, signed, input, "records-lifecycle-create")
	created, err := repository.CommitRevision(ctx, createCommand)
	if err != nil {
		t.Fatalf("CommitRevision() create error = %v", err)
	}

	noChangeCommand := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		created.RecordID,
		created.RevisionID,
		created.LockVersion,
		created.AuthorizationEpoch,
		input,
		"records-lifecycle-no-change",
	)
	noChange, err := repository.CommitRevision(ctx, noChangeCommand)
	if err != nil {
		t.Fatalf("CommitRevision() no-change error = %v", err)
	}
	noChangeReplayCommand := noChangeCommand
	noChangeReplayCommand.Idempotency.OwnerID = "records_pg_no_change_retry"
	noChangeReplay, err := repository.CommitRevision(ctx, noChangeReplayCommand)
	if err != nil {
		t.Fatalf("CommitRevision() no-change replay error = %v", err)
	}
	if noChange.Created || noChange.Replayed || noChangeReplay.Created || !noChangeReplay.Replayed ||
		noChange.RevisionID != created.RevisionID || noChangeReplay.RevisionID != created.RevisionID ||
		noChange.LockVersion != created.LockVersion || noChangeReplay.LockVersion != created.LockVersion ||
		noChange.AuthorizationEpoch != created.AuthorizationEpoch ||
		noChangeReplay.AuthorizationEpoch != created.AuthorizationEpoch {
		t.Fatalf("no-change results = first %#v replay %#v created %#v", noChange, noChangeReplay, created)
	}
	beforeConflicts := snapshotPostgresCollaborationRecordState(t, ctx, fixture, created.RecordID)
	for _, test := range []struct {
		name   string
		mutate func(*records.RecordLifecycleCommand)
	}{
		{name: "revision", mutate: func(command *records.RecordLifecycleCommand) { command.CurrentRevisionID = "rrv_stalelifecycle" }},
		{name: "lock version", mutate: func(command *records.RecordLifecycleCommand) { command.LockVersion++ }},
		{name: "authorization epoch", mutate: func(command *records.RecordLifecycleCommand) { command.AuthorizationEpoch++ }},
	} {
		command := recordsPostgresLifecycleCommand(t, noChange, records.LifecycleArchived, "records-lifecycle-stale-"+test.name)
		// The idempotency key grammar excludes spaces.
		command.Idempotency.Key.Key = strings.ReplaceAll(command.Idempotency.Key.Key, " ", "-")
		test.mutate(&command)
		if result, err := repository.CommitRecordLifecycle(ctx, command); !errors.Is(err, records.ErrRecordRevisionConflict) || result != (records.RecordLifecycleResult{}) {
			t.Fatalf("stale %s lifecycle = (%#v, %v)", test.name, result, err)
		}
		assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, command.Idempotency.Key.Key)
		if after := snapshotPostgresCollaborationRecordState(t, ctx, fixture, created.RecordID); !reflect.DeepEqual(after, beforeConflicts) {
			t.Fatalf("stale %s lifecycle changed state: before=%#v after=%#v", test.name, beforeConflicts, after)
		}
	}
	missingActor := recordsPostgresLifecycleCommand(t, noChange, records.LifecycleArchived, "records-lifecycle-missing-actor")
	missingActor.Actor = collaborationActor(t, "usr_eeeeeeeeeeeeeeeeeeeeeeee", nil)
	missingActor.Idempotency.RequestFingerprint, err = recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: recordplatform.OperationKindRecordUpdate,
		ProjectID: recordplatform.ProjectIDDefault, ActorScopeDigest: missingActor.Actor.CanonicalHash(),
		RequestScopeDigest: testRecordRevisionDigest(0x73), PayloadDigest: testRecordRevisionDigest(0x81),
	})
	if err != nil {
		t.Fatalf("fingerprint missing actor command: %v", err)
	}
	if result, err := repository.CommitRecordLifecycle(ctx, missingActor); !errors.Is(err, recordauth.ErrDenied) || result != (records.RecordLifecycleResult{}) {
		t.Fatalf("lifecycle by missing actor = (%#v, %v), want denied", result, err)
	}
	assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, missingActor.Idempotency.Key.Key)
	if after := snapshotPostgresCollaborationRecordState(t, ctx, fixture, created.RecordID); !reflect.DeepEqual(after, beforeConflicts) {
		t.Fatalf("missing lifecycle actor changed state: before=%#v after=%#v", beforeConflicts, after)
	}

	archiveCommand := recordsPostgresLifecycleCommand(t, noChange, records.LifecycleArchived, "records-lifecycle-archive")
	archived, err := repository.CommitRecordLifecycle(ctx, archiveCommand)
	if err != nil {
		t.Fatalf("CommitRecordLifecycle() archive error = %v", err)
	}
	archiveReplayCommand := archiveCommand
	archiveReplayCommand.Idempotency.OwnerID = "records_pg_archive_retry"
	archiveReplay, err := repository.CommitRecordLifecycle(ctx, archiveReplayCommand)
	if err != nil {
		t.Fatalf("CommitRecordLifecycle() archive replay error = %v", err)
	}
	if archived.Replayed || !archiveReplay.Replayed || archived.RecordID != archiveReplay.RecordID ||
		archived.CurrentRevisionID != archiveReplay.CurrentRevisionID || archived.LockVersion != archiveReplay.LockVersion ||
		archived.AuthorizationEpoch != archiveReplay.AuthorizationEpoch || archived.Lifecycle != records.LifecycleArchived ||
		archiveReplay.Lifecycle != records.LifecycleArchived || !archived.ChangedAt.Equal(archiveReplay.ChangedAt) {
		t.Fatalf("archive results = first %#v replay %#v", archived, archiveReplay)
	}
	beforeDeniedReplay := snapshotPostgresCollaborationRecordState(t, ctx, fixture, created.RecordID)
	if _, err := fixture.db.Exec(ctx, `update public.users set role='viewer' where user_id=$1`, archiveCommand.Actor.UserID); err != nil {
		t.Fatalf("demote lifecycle actor: %v", err)
	}
	if result, err := repository.CommitRecordLifecycle(ctx, archiveCommand); !errors.Is(err, recordauth.ErrDenied) || result != (records.RecordLifecycleResult{}) {
		t.Fatalf("lifecycle replay by demoted actor = (%#v, %v)", result, err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.users set role='admin' where user_id=$1`, archiveCommand.Actor.UserID); err != nil {
		t.Fatalf("restore lifecycle actor: %v", err)
	}
	if after := snapshotPostgresCollaborationRecordState(t, ctx, fixture, created.RecordID); !reflect.DeepEqual(after, beforeDeniedReplay) {
		t.Fatalf("denied lifecycle replay changed durable state: before=%#v after=%#v", beforeDeniedReplay, after)
	}
	fingerprintConflict := archiveCommand
	fingerprintConflict.Idempotency.RequestFingerprint = mustStoreActionFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x7d)
	if result, err := repository.CommitRecordLifecycle(ctx, fingerprintConflict); !errors.Is(err, recordplatform.ErrIdempotencyKeyReused) || result != (records.RecordLifecycleResult{}) {
		t.Fatalf("same lifecycle key with different request = (%#v, %v)", result, err)
	}
	repeatedArchive := recordsPostgresLifecycleCommandFromLifecycleResult(t, archived, records.LifecycleArchived, "records-lifecycle-repeat-archive")
	if result, err := repository.CommitRecordLifecycle(ctx, repeatedArchive); !errors.Is(err, records.ErrRecordRevisionConflict) || result != (records.RecordLifecycleResult{}) {
		t.Fatalf("repeated archive with fresh key = (%#v, %v)", result, err)
	}
	assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, repeatedArchive.Idempotency.Key.Key)
	var archivedAt *time.Time
	if err := fixture.db.QueryRow(ctx, `select archived_at from public.records where record_id = $1`, created.RecordID).Scan(&archivedAt); err != nil {
		t.Fatalf("read archived_at: %v", err)
	}
	if archivedAt == nil || archivedAt.IsZero() {
		t.Fatalf("archived_at = %v, want set", archivedAt)
	}

	unarchiveCommand := recordsPostgresLifecycleCommandFromLifecycleResult(
		t,
		archived,
		records.LifecycleActive,
		"records-lifecycle-unarchive",
	)
	unarchived, err := repository.CommitRecordLifecycle(ctx, unarchiveCommand)
	if err != nil {
		t.Fatalf("CommitRecordLifecycle() unarchive error = %v", err)
	}
	if unarchived.Lifecycle != records.LifecycleActive || unarchived.CurrentRevisionID != created.RevisionID ||
		unarchived.LockVersion != created.LockVersion+2 || unarchived.AuthorizationEpoch != created.AuthorizationEpoch+2 {
		t.Fatalf("unarchive result = %#v, created %#v", unarchived, created)
	}
	unarchiveReplay, err := repository.CommitRecordLifecycle(ctx, unarchiveCommand)
	if err != nil || !unarchiveReplay.Replayed || unarchiveReplay.RecordID != unarchived.RecordID ||
		unarchiveReplay.CurrentRevisionID != unarchived.CurrentRevisionID || unarchiveReplay.LockVersion != unarchived.LockVersion ||
		unarchiveReplay.AuthorizationEpoch != unarchived.AuthorizationEpoch || unarchiveReplay.Lifecycle != unarchived.Lifecycle ||
		!unarchiveReplay.ChangedAt.Equal(unarchived.ChangedAt) {
		t.Fatalf("unarchive replay = (%#v, %v), first %#v", unarchiveReplay, err, unarchived)
	}

	var (
		lifecycle          string
		currentRevisionID  string
		lockVersion        int64
		authorizationEpoch int64
		finalArchivedAt    *time.Time
		revisionCount      int
		createdCount       int
		archivedCount      int
		unarchivedCount    int
		outboxCount        int
		idempotencyCount   int
	)
	if err := fixture.db.QueryRow(ctx, `
		select lifecycle, current_revision_id, lock_version, authorization_epoch, archived_at
		from public.records
		where record_id = $1`, created.RecordID).Scan(
		&lifecycle,
		&currentRevisionID,
		&lockVersion,
		&authorizationEpoch,
		&finalArchivedAt,
	); err != nil {
		t.Fatalf("read final lifecycle root: %v", err)
	}
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $1 and event_kind = 'record_created'),
		       (select count(*)::int from public.record_domain_activities where record_id = $1 and event_kind = 'record_archived'),
		       (select count(*)::int from public.record_domain_activities where record_id = $1 and event_kind = 'record_unarchived'),
		       (select count(*)::int from public.record_outbox where subject_kind = 'record' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys
		         where idempotency_key in (
		           'records-lifecycle-create',
		           'records-lifecycle-no-change',
		           'records-lifecycle-archive',
		           'records-lifecycle-unarchive'
		         ))`, created.RecordID).Scan(
		&revisionCount,
		&createdCount,
		&archivedCount,
		&unarchivedCount,
		&outboxCount,
		&idempotencyCount,
	); err != nil {
		t.Fatalf("count lifecycle rows: %v", err)
	}
	if lifecycle != string(records.LifecycleActive) || currentRevisionID != created.RevisionID ||
		lockVersion != int64(created.LockVersion+2) || authorizationEpoch != int64(created.AuthorizationEpoch+2) ||
		finalArchivedAt != nil {
		t.Fatalf("final lifecycle root = lifecycle %q revision %q lock/auth %d/%d archived_at %v",
			lifecycle, currentRevisionID, lockVersion, authorizationEpoch, finalArchivedAt)
	}
	if revisionCount != 1 || createdCount != 1 || archivedCount != 1 || unarchivedCount != 1 ||
		outboxCount != 3 || idempotencyCount != 4 {
		t.Fatalf("lifecycle row counts = revision %d created/archive/unarchive %d/%d/%d outbox %d idempotency %d, want 1 1/1/1 3 4",
			revisionCount, createdCount, archivedCount, unarchivedCount, outboxCount, idempotencyCount)
	}
	assertRecordsPostgresCurrentProjectionReconciled(t, ctx, fixture.db, unarchived.RecordID, unarchived.CurrentRevisionID)
}

func TestPostgresIntegrationRecordLifecycleActivityFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	fixture := signed.platform
	repository := newRevisionMutationAdmittedRepository(t, signed)
	birthCommand := revisionBirthBatchCommand(t, ctx, signed, recordsPostgresCompleteRevisionInput(t, "Lifecycle rollback"), "lifecycle-rollback-create")
	created, err := repository.CommitRevision(ctx, birthCommand)
	if err != nil {
		t.Fatalf("create lifecycle record: %v", err)
	}
	before := snapshotPostgresCollaborationRecordState(t, ctx, fixture, created.RecordID)
	for _, statement := range []string{
		`create function public.record_lifecycle_test_fail() returns trigger language plpgsql as $$
		begin
			if new.event_kind = 'record_archived' then
				raise exception 'record lifecycle trigger failure';
			end if;
			return new;
		end $$`,
		`create trigger record_lifecycle_test_fail before insert on public.record_domain_activities
		 for each row execute function public.record_lifecycle_test_fail()`,
		`grant execute on function public.record_lifecycle_test_fail() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	} {
		if _, err := fixture.db.Exec(ctx, statement); err != nil {
			t.Fatalf("install lifecycle failure trigger: %v", err)
		}
	}
	command := recordsPostgresLifecycleCommand(t, created, records.LifecycleArchived, "lifecycle-rollback-archive")
	if result, err := repository.CommitRecordLifecycle(ctx, command); err == nil || !strings.Contains(err.Error(), "record lifecycle trigger failure") || result != (records.RecordLifecycleResult{}) {
		t.Fatalf("archive with activity failure = (%#v, %v)", result, err)
	}
	if after := snapshotPostgresCollaborationRecordState(t, ctx, fixture, created.RecordID); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed archive changed durable state: before=%#v after=%#v", before, after)
	}
	assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, command.Idempotency.Key.Key)
	if _, err := fixture.db.Exec(ctx, `drop trigger record_lifecycle_test_fail on public.record_domain_activities`); err != nil {
		t.Fatalf("remove lifecycle failure trigger: %v", err)
	}
	if result, err := repository.CommitRecordLifecycle(ctx, command); err != nil || result.Lifecycle != records.LifecycleArchived || result.Replayed {
		t.Fatalf("retry after lifecycle rollback = (%#v, %v)", result, err)
	}
}

func TestPostgresIntegrationRecordRevisionRestoreReconcilesFullCurrentProjectionAndAuthorization(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	runtimePool := fixture.Runtime
	repository := newRevisionMutationAdmittedRepository(t, fixture)
	firstInput := recordsPostgresCompleteRevisionInput(t, "Original revision")
	createCommand := revisionBirthBatchCommand(t, ctx, fixture, firstInput, "records-restore-create")
	created, err := repository.CommitRevision(ctx, createCommand)
	if err != nil {
		t.Fatalf("CommitRevision() create error = %v", err)
	}
	secondInput := recordsPostgresCompleteRevisionInput(t, "Changed revision")
	revised, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		created.RecordID,
		created.RevisionID,
		created.LockVersion,
		created.AuthorizationEpoch,
		secondInput,
		"records-restore-revise",
	))
	if err != nil {
		t.Fatalf("CommitRevision() revise error = %v", err)
	}
	restoreCommand := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		revised.RecordID,
		revised.RevisionID,
		revised.LockVersion,
		revised.AuthorizationEpoch,
		firstInput,
		"records-restore-old",
	)
	restoreCommand.ActivityKind = records.DomainActivityRecordRestored
	restored, err := repository.CommitRevision(ctx, restoreCommand)
	if err != nil {
		t.Fatalf("CommitRevision() restore error = %v", err)
	}
	if !restored.Created || restored.RevisionNo != 3 || restored.RevisionID == created.RevisionID ||
		restored.RevisionID == revised.RevisionID || restored.LockVersion != 3 || restored.AuthorizationEpoch != 3 {
		t.Fatalf("restore result = %#v, created/revised = %#v/%#v", restored, created, revised)
	}

	var (
		firstTitle       string
		secondTitle      string
		restoredTitle    string
		restoredBase     string
		firstHash        []byte
		restoredHash     []byte
		revisionCount    int
		activityCount    int
		outboxCount      int
		participantCount int
	)
	if err := fixture.Runtime.QueryRow(ctx, `
		select first.title,
		       second.title,
		       restored.title,
		       restored.base_revision_id,
		       first.canonical_hash,
		       restored.canonical_hash
		from public.record_revisions first
		join public.record_revisions second on second.revision_id = $2
		join public.record_revisions restored on restored.revision_id = $3
		where first.revision_id = $1`, created.RevisionID, revised.RevisionID, restored.RevisionID).Scan(
		&firstTitle,
		&secondTitle,
		&restoredTitle,
		&restoredBase,
		&firstHash,
		&restoredHash,
	); err != nil {
		t.Fatalf("read restored revision history: %v", err)
	}
	if err := fixture.Runtime.QueryRow(ctx, `
		select (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $1),
		       (select count(*)::int from public.record_outbox where subject_kind = 'record' and subject_id = $1),
		       (select count(*)::int from public.record_revision_participants participants
		         join public.record_revisions revisions on revisions.revision_id = participants.revision_id
		         where revisions.record_id = $1)`, restored.RecordID).Scan(
		&revisionCount,
		&activityCount,
		&outboxCount,
		&participantCount,
	); err != nil {
		t.Fatalf("count restored record rows: %v", err)
	}
	if firstTitle != "Original revision" || secondTitle != "Changed revision" ||
		restoredTitle != firstTitle || restoredBase != revised.RevisionID || !bytes.Equal(firstHash, restoredHash) {
		t.Fatalf("restored history = titles %q/%q/%q base %q hashes %x/%x",
			firstTitle, secondTitle, restoredTitle, restoredBase, firstHash, restoredHash)
	}
	if revisionCount != 3 || activityCount != 3 || outboxCount != 3 || participantCount != 3 {
		t.Fatalf("restored row counts = revisions %d activities %d outbox %d participants %d, want 3/3/3/3",
			revisionCount, activityCount, outboxCount, participantCount)
	}
	assertRecordsPostgresCurrentProjectionReconciled(t, ctx, fixture.Runtime, restored.RecordID, restored.RevisionID)

	storedSubject := firstInput.Subjects()[0]
	identity, err := records.NewSubjectIdentitySnapshot(storedSubject.Kind, storedSubject.IdentitySnapshot)
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	resolver := &fakeCurrentRecordSubjectResolver{resolved: records.ResolvedSubject{
		ProjectID:            recordauth.ProjectIDDefault,
		StableID:             storedSubject.SourceID,
		IdentitySnapshot:     identity,
		LiveRoute:            "/vps/" + storedSubject.SourceID,
		CaptureAuthorization: storedSubject.CaptureAuthorization,
	}}
	authorizationSource := newPostgresCurrentRecordAuthorizationSource(
		runtimePool,
		resolver,
		fixture.Gate,
	)
	current, err := authorizationSource.ResolveCurrentRecordAuthorization(
		ctx,
		mustStoreRecordActor(t),
		restored.RecordID,
	)
	if err != nil {
		t.Fatalf("ResolveCurrentRecordAuthorization() PostgreSQL error = %v", err)
	}
	if current.RecordID != restored.RecordID || current.CurrentRevisionID != restored.RevisionID ||
		current.LockVersion != restored.LockVersion || current.AuthorizationEpoch != restored.AuthorizationEpoch ||
		current.Lifecycle != records.LifecycleActive || len(current.Evidence.Sources) != 1 || resolver.calls != 1 ||
		current.Evidence.Visibility.CanonicalHash != firstInput.VisibilityScope().CanonicalHash ||
		current.Evidence.Sources[0].Digest != storedSubject.CaptureAuthorization.Digest {
		t.Fatalf("PostgreSQL current authorization = %#v, resolver calls %d", current, resolver.calls)
	}
}

func newRecordsPostgresFixture(t *testing.T, ctx context.Context) recordPlatformPostgresFixture {
	t.Helper()
	fixture := newRecordPlatformPostgresBaseFixture(t, ctx)
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "records-current-migrator", 1)
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migratorPool, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatalf("ConvergeAppACLCurrent() for records integration: %v", err)
	}
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "records-current-admission", 1)
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, runtimePool); err != nil {
		t.Fatalf("AdmitAppACLCurrentRuntime() for records integration: %v", err)
	}
	seedRecordsPostgresUsers(t, ctx, fixture)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.vps_assets(vps_id, display_name, lifecycle_status, usage_status)
		values ($1, 'PostgreSQL VPS', 'active', 'in_use')`, testStoreRecordVPSID); err != nil {
		t.Fatalf("seed records VPS source: %v", err)
	}
	return fixture
}

func seedRecordsPostgresUsers(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	for index, userID := range []string{
		"usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		"usr_cccccccccccccccccccccccc",
		"usr_dddddddddddddddddddddddd",
	} {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.users (user_id, username, password_hash, display_name, role)
			values ($1, $2, 'test-hash', $2, 'admin')
			on conflict (user_id) do nothing`, userID, fmt.Sprintf("collaboration-user-%d", index)); err != nil {
			t.Fatalf("seed records user %q: %v", userID, err)
		}
	}
}

func assertRecordsPostgresCurrentProjectionReconciled(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	recordID string,
	revisionID string,
) {
	t.Helper()
	var reconciled bool
	if err := db.QueryRow(ctx, `
		select roots.current_revision_id = revisions.revision_id
		   and roots.project_id = revisions.project_id
		   and roots.current_title is not distinct from revisions.title
		   and roots.current_record_type is not distinct from revisions.record_type
		   and roots.current_business_status is not distinct from revisions.business_status
		   and roots.current_status_group is not distinct from revisions.status_group
		   and roots.current_impact_level is not distinct from revisions.impact_level
		   and roots.current_occurred_at is not distinct from revisions.occurred_at
		   and roots.current_completed_at is not distinct from revisions.completed_at
		   and roots.current_visibility_scope is not distinct from revisions.visibility_scope
		   and roots.current_visibility_digest is not distinct from revisions.visibility_digest
		   and roots.current_owner_id is not distinct from revisions.owner_id
		   and roots.current_follow_up_at is not distinct from revisions.follow_up_at
		from public.records roots
		join public.record_revisions revisions
		  on revisions.record_id = roots.record_id
		 and revisions.revision_id = roots.current_revision_id
		where roots.record_id = $1
		  and revisions.revision_id = $2`, recordID, revisionID).Scan(&reconciled); err != nil {
		t.Fatalf("read current projection reconciliation: %v", err)
	}
	if !reconciled {
		t.Fatalf("current projection for record %q revision %q is not reconciled", recordID, revisionID)
	}
}

func newRecordsPostgresRepository(t *testing.T, pool *pgxpool.Pool, participants ...records.RevisionParticipant) *PostgresRecordRepository {
	t.Helper()
	repository, err := NewPostgresRecordRepository(pool, allowRecordPlatformAdmissionGate, participants, newRecordsPostgresAuthorization(t, pool, allowRecordPlatformAdmissionGate), nil)
	if err != nil {
		t.Fatalf("NewPostgresRecordRepository() error = %v", err)
	}
	return repository
}

func newRecordsPostgresAuthorization(t *testing.T, pool *pgxpool.Pool, gate AdmissionGate) *PostgresCurrentRecordAuthorizationSource {
	t.Helper()
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		NewVPSRecordSubjectAdapter(NewPostgresVPSAssetRepository(pool)),
		NewMonitoringInstanceRecordSubjectAdapter(NewPostgresMonitoringInstanceRepository(pool)),
		NewTargetRecordSubjectAdapter(NewPostgresTargetRepository(pool)),
	})
	if err != nil {
		t.Fatal(err)
	}
	witness, err := NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, pool)
	if err != nil {
		t.Fatal(err)
	}
	return NewPostgresCurrentRecordAuthorizationSource(pool, NewRecordSubjectReadResolver(subjects, witness), gate)
}

func recordsPostgresCompleteRevisionInput(t *testing.T, title string, attachmentIDs ...string) records.CompleteRevisionInput {
	return recordsPostgresCompleteRevisionInputValues(t, title, attachmentIDs, nil)
}

func recordsPostgresCompleteRevisionInputWithEvidence(
	t *testing.T,
	title string,
	evidenceSnapshotIDs []string,
) records.CompleteRevisionInput {
	return recordsPostgresCompleteRevisionInputValues(t, title, nil, evidenceSnapshotIDs)
}

func recordsPostgresCompleteRevisionInputValues(
	t *testing.T,
	title string,
	attachmentIDs []string,
	evidenceSnapshotIDs []string,
) records.CompleteRevisionInput {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      recordauth.ProjectIDDefault,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindVPS,
		SourceID:     testStoreRecordVPSID,
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title:                  title,
		BodyMarkdown:           "# " + title + "\n",
		MarkdownDialectVersion: records.MarkdownDialectVersionV1,
		RecordType:             records.RecordTypeNote,
		ImpactLevel:            "informational",
		VisibilityScope:        visibility,
		Subjects: []records.RevisionSubject{{
			RegistryVersion:      records.SubjectRegistryVersionV1,
			Kind:                 records.SubjectKindVPS,
			Role:                 records.RelationRoleAffected,
			SourceID:             testStoreRecordVPSID,
			Primary:              true,
			IdentitySnapshot:     map[string]string{"display_name": "PostgreSQL VPS"},
			CaptureAuthorization: authorization,
		}},
		Tags:    []string{"postgres"},
		OwnerID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Participants: []records.RevisionParticipantSnapshot{{
			ParticipantID:    "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
			IdentitySnapshot: map[string]string{"display_name": "PostgreSQL Operator"},
		}},
		AttachmentIDs:       attachmentIDs,
		EvidenceSnapshotIDs: evidenceSnapshotIDs,
		AuthorID:            "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	return input
}

func seedRecordsPostgresRevisionAttachment(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	attachmentID string,
	draftID string,
	state attachments.UploadState,
	digestByte string,
) {
	t.Helper()
	if state == attachments.UploadStateAvailable {
		pool := fixture.openDirectRuntimePool(t, ctx, "revision-attachment-seed", 2)
		repository := NewPostgresAttachmentRepository(pool)
		blob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
		if err != nil {
			t.Fatal(err)
		}
		actor := recordauth.ActorScope{UserID: recordsPostgresDraftAuthorID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault}
		service := newAttachmentUploadWorkflowService(t, fixture, repository, blob, attachments.TransportKindLocal, actor, draftID, time.Now)
		content := []byte(digestByte)
		upload, err := service.CreateUpload(ctx, attachments.CreateUploadRequest{
			Actor: actor, UploadID: "aup_" + strings.TrimPrefix(attachmentID, "att_"), AttachmentID: attachmentID,
			DraftID: draftID, DisplayName: "revision.txt", MediaType: "text/plain", DeclaredSizeBytes: int64(len(content)), ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutContent(ctx, attachments.PutUploadContentRequest{Actor: actor, DraftID: draftID, UploadID: upload.UploadID, ExpectedSHA256: sha256.Sum256(content), Content: bytes.NewReader(content)}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.CompleteUpload(ctx, attachments.CompleteUploadRequest{Actor: actor, DraftID: draftID, UploadID: upload.UploadID}); err != nil {
			t.Fatalf("complete revision seed upload: %v", err)
		}
		claim, err := repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "revision-seed", OwnerLeaseDuration: time.Minute})
		if err != nil || claim == nil || claim.AttachmentID != attachmentID {
			t.Fatalf("claim revision seed: %#v %v", claim, err)
		}
		writer, err := attachments.NewManagedBlobWriter(blob, attachments.BackendKindLocal, NewPostgresLocalBlobRepository(pool, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		preview, intent := prepareManagedAttachmentPreview(t, ctx, repository, writer, *claim, content)
		if _, err := repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{Claim: *claim, Result: attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean, HasPreview: true, Preview: attachments.ManagedPreview{Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8}}, PreviewPublicationIntent: intent, Limits: attachments.DefaultLimits()}); err != nil {
			t.Fatalf("complete revision seed: %v", err)
		}
		return
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, project_id, draft_id, origin_draft_id, attachment_state,
			display_name, media_type, logical_size_bytes, created_by
		) values ($1, 'default', $2, $2, $3, $1 || '.txt', 'text/plain', 1,
			'usr_aaaaaaaaaaaaaaaaaaaaaaaa')`, attachmentID, draftID, state); err != nil {
		t.Fatalf("seed pending revision attachment %q: %v", attachmentID, err)
	}
}

func recordsPostgresRevisionCommand(
	t *testing.T,
	operation recordplatform.OperationKind,
	recordID string,
	baseRevisionID string,
	lockVersion uint64,
	authorizationEpoch uint64,
	input records.CompleteRevisionInput,
	idempotencyKey string,
) records.RevisionCommitCommand {
	t.Helper()
	actor := recordauth.ActorScope{UserID: input.AuthorID(), Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault}
	return recordsPostgresRevisionCommandForActor(t, operation, recordID, baseRevisionID, lockVersion, authorizationEpoch, input, idempotencyKey, actor)
}

func recordsPostgresRevisionCommandForActor(
	t *testing.T,
	operation recordplatform.OperationKind,
	recordID string,
	baseRevisionID string,
	lockVersion uint64,
	authorizationEpoch uint64,
	input records.CompleteRevisionInput,
	idempotencyKey string,
	actor recordauth.ActorScope,
) records.RevisionCommitCommand {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      operation,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   actor.CanonicalHash(),
		RequestScopeDigest: sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%d", recordID, baseRevisionID, lockVersion, authorizationEpoch))),
		PayloadDigest:      input.CanonicalHash(),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	activityKind := records.DomainActivityRecordCreated
	if operation == recordplatform.OperationKindRecordUpdate {
		activityKind = records.DomainActivityRecordRevised
	}
	return records.RevisionCommitCommand{
		Actor:              actor,
		RecordID:           recordID,
		BaseRevisionID:     baseRevisionID,
		LockVersion:        lockVersion,
		AuthorizationEpoch: authorizationEpoch,
		Input:              input,
		ActivityKind:       activityKind,
		OutboxTTL:          24 * time.Hour,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: operation,
				Key:           idempotencyKey,
			},
			RequestFingerprint: fingerprint,
			OwnerID:            "records_pg_owner",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          24 * time.Hour,
		},
	}
}

func recordsPostgresLifecycleCommand(
	t *testing.T,
	current records.RevisionCommitResult,
	target records.Lifecycle,
	idempotencyKey string,
) records.RecordLifecycleCommand {
	t.Helper()
	return recordsPostgresLifecycleCommandValues(
		t,
		current.RecordID,
		current.RevisionID,
		current.LockVersion,
		current.AuthorizationEpoch,
		target,
		idempotencyKey,
	)
}

func recordsPostgresLifecycleCommandFromLifecycleResult(
	t *testing.T,
	current records.RecordLifecycleResult,
	target records.Lifecycle,
	idempotencyKey string,
) records.RecordLifecycleCommand {
	t.Helper()
	return recordsPostgresLifecycleCommandValues(
		t,
		current.RecordID,
		current.CurrentRevisionID,
		current.LockVersion,
		current.AuthorizationEpoch,
		target,
		idempotencyKey,
	)
}

func recordsPostgresLifecycleCommandValues(
	t *testing.T,
	recordID string,
	currentRevisionID string,
	lockVersion uint64,
	authorizationEpoch uint64,
	target records.Lifecycle,
	idempotencyKey string,
) records.RecordLifecycleCommand {
	t.Helper()
	actor := recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault}
	payloadDigest := testRecordRevisionDigest(0x81)
	if target == records.LifecycleActive {
		payloadDigest = testRecordRevisionDigest(0x82)
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordUpdate,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   actor.CanonicalHash(),
		RequestScopeDigest: testRecordRevisionDigest(0x73),
		PayloadDigest:      payloadDigest,
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() lifecycle error = %v", err)
	}
	expectedLifecycle := records.LifecycleActive
	if target == records.LifecycleActive {
		expectedLifecycle = records.LifecycleArchived
	}
	return records.RecordLifecycleCommand{
		RecordID:           recordID,
		CurrentRevisionID:  currentRevisionID,
		LockVersion:        lockVersion,
		AuthorizationEpoch: authorizationEpoch,
		ExpectedLifecycle:  expectedLifecycle,
		TargetLifecycle:    target,
		Actor:              actor,
		OutboxTTL:          24 * time.Hour,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: recordplatform.OperationKindRecordUpdate,
				Key:           idempotencyKey,
			},
			RequestFingerprint: fingerprint,
			OwnerID:            "records_pg_owner",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          24 * time.Hour,
		},
	}
}
