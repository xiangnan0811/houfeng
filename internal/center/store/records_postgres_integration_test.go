package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationRecordRevisionConcurrentSameBaseHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedRepository := newRecordsPostgresRepository(t, fixture.openDirectRuntimePool(t, ctx, "records-race-seed", 1))

	seedCommand := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrace",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Initial record"),
		"records-race-seed",
	)
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
	for index, command := range commands {
		repository := newRecordsPostgresRepository(
			t,
			fixture.openDirectRuntimePool(t, ctx, "records-race-worker-"+string(rune('a'+index)), 1),
		)
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
	if err := fixture.db.QueryRow(ctx, `
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
	if err := fixture.db.QueryRow(ctx, `
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
	assertRecordsPostgresCurrentProjectionReconciled(t, ctx, fixture.db, winner.RecordID, winner.RevisionID)
}

func TestPostgresIntegrationRecordRevisionRetryReplaysWithoutDuplicateRows(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := newRecordsPostgresRepository(t, fixture.openDirectRuntimePool(t, ctx, "records-retry-first", 1))
	command := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgretry",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Retry-safe record"),
		"records-retry-key",
	)

	first, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("CommitRevision() first error = %v", err)
	}
	retryCommand := command
	retryCommand.Idempotency.OwnerID = "records_pg_retry"
	replayed, err := newRecordsPostgresRepository(
		t,
		fixture.openDirectRuntimePool(t, ctx, "records-retry-second", 1),
	).CommitRevision(ctx, retryCommand)
	if err != nil {
		t.Fatalf("CommitRevision() replay error = %v", err)
	}
	if !first.Created || first.Replayed || !replayed.Created || !replayed.Replayed ||
		first.RecordID != replayed.RecordID || first.RevisionID != replayed.RevisionID ||
		first.RevisionNo != replayed.RevisionNo || first.LockVersion != replayed.LockVersion ||
		first.AuthorizationEpoch != replayed.AuthorizationEpoch || !first.CommittedAt.Equal(replayed.CommittedAt) {
		t.Fatalf("retry results = first %#v replay %#v", first, replayed)
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
	if err := fixture.db.QueryRow(ctx, `
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

func TestPostgresIntegrationRecordRevisionParticipantFailureLeavesNoHalfCommit(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	fixture.installAtomicBusinessFactTable(t, ctx)
	participantErr := errors.New("participant failed")
	participant := &storeRevisionParticipantStub{
		name: "failing_projection",
		apply: func(ctx context.Context, tx pgx.Tx, committed records.RevisionCommitted) error {
			if _, err := tx.Exec(ctx, `
				insert into public.record_platform_test_business_facts_v1 (fact_id)
				values ($1)`, committed.Result.RevisionID); err != nil {
				return err
			}
			return participantErr
		},
	}
	repository := newRecordsPostgresRepository(
		t,
		fixture.openDirectRuntimePool(t, ctx, "records-participant-failure", 1),
		participant,
	)
	command := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrollback",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Rollback record"),
		"records-participant-failure",
	)

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
		projectionCount  int
		outboxCount      int
		idempotencyCount int
	)
	if err := fixture.db.QueryRow(ctx, `
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
		       (select count(*)::int from public.record_platform_test_business_facts_v1),
		       (select count(*)::int from public.record_outbox where subject_kind = 'record' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys
		         where operation_kind = 'record_create' and idempotency_key = 'records-participant-failure')`, command.RecordID).Scan(
		&rootCount,
		&revisionCount,
		&subjectCount,
		&tagCount,
		&participantCount,
		&activityCount,
		&projectionCount,
		&outboxCount,
		&idempotencyCount,
	); err != nil {
		t.Fatalf("count participant rollback rows: %v", err)
	}
	if rootCount != 0 || revisionCount != 0 || subjectCount != 0 || tagCount != 0 ||
		participantCount != 0 || activityCount != 0 || projectionCount != 0 || outboxCount != 0 || idempotencyCount != 0 {
		t.Fatalf("participant rollback counts = root %d revision %d subject %d tag %d participant %d activity %d projection %d outbox %d idempotency %d, want all 0",
			rootCount, revisionCount, subjectCount, tagCount, participantCount, activityCount, projectionCount, outboxCount, idempotencyCount)
	}
}

func TestPostgresIntegrationRecordRevisionNoChangeAndLifecycleReplayPreserveRevisionAuthority(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := newRecordsPostgresRepository(t, fixture.openDirectRuntimePool(t, ctx, "records-lifecycle", 1))
	input := recordsPostgresCompleteRevisionInput(t, "Lifecycle record")
	createCommand := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pglifecycle",
		"",
		0,
		0,
		input,
		"records-lifecycle-create",
	)
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

func TestPostgresIntegrationRecordRevisionRestoreReconcilesFullCurrentProjectionAndAuthorization(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "records-restore", 1)
	repository := newRecordsPostgresRepository(t, runtimePool)
	firstInput := recordsPostgresCompleteRevisionInput(t, "Original revision")
	created, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrestore",
		"",
		0,
		0,
		firstInput,
		"records-restore-create",
	))
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
	if err := fixture.db.QueryRow(ctx, `
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
	if err := fixture.db.QueryRow(ctx, `
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
	assertRecordsPostgresCurrentProjectionReconciled(t, ctx, fixture.db, restored.RecordID, restored.RevisionID)

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
		allowRecordPlatformAdmissionGate,
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
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migratorPool, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLCurrent() for records integration: %v", err)
	}
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "records-current-admission", 1)
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, runtimePool); err != nil {
		t.Fatalf("AdmitAppACLCurrentRuntime() for records integration: %v", err)
	}
	return fixture
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
	repository, err := NewPostgresRecordRepository(pool, allowRecordPlatformAdmissionGate, participants)
	if err != nil {
		t.Fatalf("NewPostgresRecordRepository() error = %v", err)
	}
	return repository
}

func recordsPostgresCompleteRevisionInput(t *testing.T, title string) records.CompleteRevisionInput {
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
		AuthorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	return input
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
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      operation,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testRecordRevisionDigest(0x71),
		RequestScopeDigest: testRecordRevisionDigest(0x72),
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
	payloadDigest := testRecordRevisionDigest(0x81)
	if target == records.LifecycleActive {
		payloadDigest = testRecordRevisionDigest(0x82)
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordUpdate,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testRecordRevisionDigest(0x71),
		RequestScopeDigest: testRecordRevisionDigest(0x73),
		PayloadDigest:      payloadDigest,
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() lifecycle error = %v", err)
	}
	return records.RecordLifecycleCommand{
		RecordID:           recordID,
		CurrentRevisionID:  currentRevisionID,
		LockVersion:        lockVersion,
		AuthorizationEpoch: authorizationEpoch,
		TargetLifecycle:    target,
		ActorID:            "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
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
