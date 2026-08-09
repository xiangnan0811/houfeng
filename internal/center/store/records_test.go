package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresRecordRepositoryCreateRevisionUsesFixedAtomicOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	steps := make([]string, 0, 32)
	tx := &fakeRecordRevisionTx{now: now, steps: &steps}
	participant := &storeRevisionParticipantStub{name: "search", apply: func(context.Context, pgx.Tx, records.RevisionCommitted) error {
		steps = append(steps, "transaction_participant")
		return nil
	}}
	repository := newRecordRevisionTestRepository(t, tx, &steps, participant)
	command := testRecordRevisionCommitCommand(t, now)

	result, err := repository.CommitRevision(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	if !result.Created || result.Replayed || result.RecordID != command.RecordID || result.RevisionNo != 1 || result.LockVersion != 1 || result.AuthorizationEpoch != 1 {
		t.Fatalf("CommitRevision() = %#v, want created revision 1 at root versions 1/1", result)
	}
	if result.RevisionID == "" {
		t.Fatal("CommitRevision() returned an empty deterministic revision id")
	}

	want := []string{
		"begin",
		"admission",
		"admission",
		"idempotency_lock",
		"idempotency_claim",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"root_create",
		"root_lock",
		"revision_insert",
		"revision_subject",
		"revision_tag",
		"revision_participant",
		"current_projection",
		"domain_activity",
		"transaction_participant",
		"outbox",
		"admission",
		"idempotency_complete",
		"commit",
		"rollback_cleanup",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
	}
}

func TestPostgresRecordRepositoryReviseAndRestoreAppendRevision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		activityKind records.DomainActivityKind
	}{
		{name: "revise", activityKind: records.DomainActivityRecordRevised},
		{name: "restore", activityKind: records.DomainActivityRecordRestored},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.August, 3, 13, 0, 0, 0, time.UTC)
			steps := make([]string, 0, 32)
			currentRevisionID := "rrv_current00000001"
			currentRevisionNo := int64(1)
			currentRevisionCreatedAt := now.Add(-time.Hour)
			currentCanonicalHash := testRecordRevisionDigest(0x44)
			tx := &fakeRecordRevisionTx{
				now:                      now,
				steps:                    &steps,
				rootExists:               true,
				currentRevisionID:        &currentRevisionID,
				currentRevisionNo:        &currentRevisionNo,
				rootLockVersion:          7,
				rootAuthorizationEpoch:   5,
				currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
				currentRevisionCreatedAt: &currentRevisionCreatedAt,
			}
			participant := &storeRevisionParticipantStub{name: "search", apply: func(context.Context, pgx.Tx, records.RevisionCommitted) error {
				steps = append(steps, "transaction_participant")
				return nil
			}}
			repository := newRecordRevisionTestRepository(t, tx, &steps, participant)
			command := testRecordRevisionUpdateCommand(t, test.activityKind)
			command.BaseRevisionID = currentRevisionID

			result, err := repository.CommitRevision(context.Background(), command)
			if err != nil {
				t.Fatalf("CommitRevision() error = %v", err)
			}
			if !result.Created || result.Replayed || result.RecordID != command.RecordID || result.RevisionNo != 2 || result.LockVersion != 8 || result.AuthorizationEpoch != 6 {
				t.Fatalf("CommitRevision() = %#v, want appended revision 2 at root versions 8/6", result)
			}
			if result.RevisionID == currentRevisionID || !result.CommittedAt.Equal(now) {
				t.Fatalf("CommitRevision() = %#v, want a newly committed immutable revision", result)
			}

			want := []string{
				"begin",
				"admission",
				"admission",
				"idempotency_lock",
				"idempotency_claim",
				"fence_reservation_lock",
				"fence_epoch_init",
				"fence_epoch_lock",
				"fence_lease_lock",
				"fence_reservation_recheck",
				"root_lock",
				"revision_insert",
				"revision_subject",
				"revision_tag",
				"revision_participant",
				"current_projection",
				"domain_activity",
				"transaction_participant",
				"outbox",
				"admission",
				"idempotency_complete",
				"commit",
				"rollback_cleanup",
			}
			if !reflect.DeepEqual(steps, want) {
				t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
			}
			if tx.insertedRevisionNo != 2 || tx.projectedLockVersion != 8 || tx.projectedAuthorizationEpoch != 6 {
				t.Fatalf("persisted revision/root versions = (%d, %d, %d), want (2, 8, 6)", tx.insertedRevisionNo, tx.projectedLockVersion, tx.projectedAuthorizationEpoch)
			}
			if tx.domainActivityKind != string(test.activityKind) || tx.outboxEventKind != string(recordplatform.OutboxEventKindRecordUpdated) {
				t.Fatalf("activity/outbox kinds = (%q, %q), want (%q, %q)", tx.domainActivityKind, tx.outboxEventKind, test.activityKind, recordplatform.OutboxEventKindRecordUpdated)
			}
		})
	}
}

func TestPostgresRecordRepositoryNoChangeCompletesIdempotencyWithoutFormalSideEffects(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 14, 0, 0, 0, time.UTC)
	steps := make([]string, 0, 24)
	command := testRecordRevisionUpdateCommand(t, records.DomainActivityRecordRevised)
	currentRevisionID := command.BaseRevisionID
	currentRevisionNo := int64(4)
	currentRevisionCreatedAt := now.Add(-2 * time.Hour)
	currentCanonicalHash := command.Input.CanonicalHash()
	tx := &fakeRecordRevisionTx{
		now:                      now,
		steps:                    &steps,
		rootExists:               true,
		currentRevisionID:        &currentRevisionID,
		currentRevisionNo:        &currentRevisionNo,
		rootLockVersion:          7,
		rootAuthorizationEpoch:   5,
		currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
		currentRevisionCreatedAt: &currentRevisionCreatedAt,
	}
	participant := &storeRevisionParticipantStub{name: "search", apply: func(context.Context, pgx.Tx, records.RevisionCommitted) error {
		steps = append(steps, "transaction_participant")
		return nil
	}}
	repository := newRecordRevisionTestRepository(t, tx, &steps, participant)

	result, err := repository.CommitRevision(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	if result.Created || result.Replayed || result.RecordID != command.RecordID || result.RevisionID != currentRevisionID || result.RevisionNo != 4 || result.LockVersion != 7 || result.AuthorizationEpoch != 5 {
		t.Fatalf("CommitRevision() = %#v, want unchanged current revision and root versions", result)
	}
	if !result.CommittedAt.Equal(currentRevisionCreatedAt) {
		t.Fatalf("CommitRevision().CommittedAt = %v, want current revision time %v", result.CommittedAt, currentRevisionCreatedAt)
	}

	want := []string{
		"begin",
		"admission",
		"admission",
		"idempotency_lock",
		"idempotency_claim",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"root_lock",
		"admission",
		"idempotency_complete",
		"commit",
		"rollback_cleanup",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
	}
	for _, forbidden := range []string{
		"root_create",
		"revision_insert",
		"revision_subject",
		"revision_tag",
		"revision_participant",
		"current_projection",
		"domain_activity",
		"transaction_participant",
		"outbox",
	} {
		if countRecordRevisionStep(steps, forbidden) != 0 {
			t.Fatalf("no-change path executed forbidden step %q: %#v", forbidden, steps)
		}
	}
}

func TestPostgresRecordRepositoryPublishesDraftAfterFormalSideEffectsInSameTransaction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 14, 30, 0, 0, time.UTC)
	steps := make([]string, 0, 32)
	command := testRecordRevisionUpdateCommand(t, records.DomainActivityRecordRevised)
	draftPayload, err := records.NewDraftPayload([]byte(`{"title":"Publish revision"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	draftETag, err := records.NewDraftETag("rdf_0123456789abcdef", command.Input.AuthorID(), 4, draftPayload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	command.DraftID = "rdf_0123456789abcdef"
	command.DraftETag = draftETag
	currentRevisionID := command.BaseRevisionID
	currentRevisionNo := int64(1)
	currentRevisionCreatedAt := now.Add(-time.Hour)
	currentCanonicalHash := testRecordRevisionDigest(0x44)
	publishedDraft := records.Draft{
		DraftID:        command.DraftID,
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       command.RecordID,
		BaseRevisionID: command.BaseRevisionID,
		AuthorID:       command.Input.AuthorID(),
		Payload:        draftPayload,
		Version:        4,
		ETag:           draftETag,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		CreatedAt:      now.Add(-2 * time.Hour),
		UpdatedAt:      now,
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}
	tx := &fakeRecordRevisionTx{
		now:                      now,
		steps:                    &steps,
		rootExists:               true,
		currentRevisionID:        &currentRevisionID,
		currentRevisionNo:        &currentRevisionNo,
		rootLockVersion:          7,
		rootAuthorizationEpoch:   5,
		currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
		currentRevisionCreatedAt: &currentRevisionCreatedAt,
		publishedDraft:           &publishedDraft,
	}
	var participantDraftID string
	participant := &storeRevisionParticipantStub{name: "search", apply: func(_ context.Context, _ pgx.Tx, committed records.RevisionCommitted) error {
		steps = append(steps, "transaction_participant")
		participantDraftID = committed.DraftID
		return nil
	}}
	repository := newRecordRevisionTestRepository(t, tx, &steps, participant)

	result, err := repository.CommitRevision(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	if !result.Created || result.RevisionNo != 2 {
		t.Fatalf("CommitRevision() = %#v, want created revision 2", result)
	}
	if participantDraftID != command.DraftID {
		t.Fatalf("participant DraftID = %q, want %q", participantDraftID, command.DraftID)
	}
	want := []string{
		"begin",
		"admission",
		"admission",
		"idempotency_lock",
		"idempotency_claim",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"root_lock",
		"draft_lock",
		"revision_insert",
		"revision_subject",
		"revision_tag",
		"revision_participant",
		"current_projection",
		"domain_activity",
		"transaction_participant",
		"outbox",
		"draft_checkpoint_delete",
		"draft_delete",
		"admission",
		"idempotency_complete",
		"commit",
		"rollback_cleanup",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
	}
}

func TestPostgresRecordRepositoryPublishesNewRecordDraftIntoRevisionOne(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 14, 40, 0, 0, time.UTC)
	steps := make([]string, 0, 32)
	command := testRecordRevisionCommitCommand(t, now)
	publishedDraft := testPublishedRecordDraft(t, now, &command, "", "")
	tx := &fakeRecordRevisionTx{now: now, steps: &steps, publishedDraft: &publishedDraft}
	repository := newRecordRevisionTestRepository(t, tx, &steps)

	result, err := repository.CommitRevision(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	if !result.Created || result.RevisionNo != 1 || result.LockVersion != 1 || result.AuthorizationEpoch != 1 {
		t.Fatalf("CommitRevision() = %#v, want created revision 1", result)
	}
	want := []string{
		"begin",
		"admission",
		"admission",
		"idempotency_lock",
		"idempotency_claim",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"root_create",
		"root_lock",
		"draft_lock",
		"revision_insert",
		"revision_subject",
		"revision_tag",
		"revision_participant",
		"current_projection",
		"domain_activity",
		"outbox",
		"draft_checkpoint_delete",
		"draft_delete",
		"admission",
		"idempotency_complete",
		"commit",
		"rollback_cleanup",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
	}
}

func TestPostgresRecordRepositoryPublishesDraftOnNoChangeBeforeCompletingIdempotency(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 14, 45, 0, 0, time.UTC)
	steps := make([]string, 0, 24)
	command := testRecordRevisionUpdateCommand(t, records.DomainActivityRecordRevised)
	publishedDraft := testPublishedRecordDraft(t, now, &command, command.RecordID, command.BaseRevisionID)
	currentRevisionID := command.BaseRevisionID
	currentRevisionNo := int64(4)
	currentRevisionCreatedAt := now.Add(-2 * time.Hour)
	currentCanonicalHash := command.Input.CanonicalHash()
	tx := &fakeRecordRevisionTx{
		now:                      now,
		steps:                    &steps,
		rootExists:               true,
		currentRevisionID:        &currentRevisionID,
		currentRevisionNo:        &currentRevisionNo,
		rootLockVersion:          7,
		rootAuthorizationEpoch:   5,
		currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
		currentRevisionCreatedAt: &currentRevisionCreatedAt,
		publishedDraft:           &publishedDraft,
	}
	repository := newRecordRevisionTestRepository(t, tx, &steps)

	result, err := repository.CommitRevision(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	if result.Created || result.RevisionID != currentRevisionID || result.RevisionNo != 4 {
		t.Fatalf("CommitRevision() = %#v, want unchanged revision 4", result)
	}
	want := []string{
		"begin",
		"admission",
		"admission",
		"idempotency_lock",
		"idempotency_claim",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"root_lock",
		"draft_lock",
		"draft_checkpoint_delete",
		"draft_delete",
		"admission",
		"idempotency_complete",
		"commit",
		"rollback_cleanup",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
	}
	for _, forbidden := range []string{
		"revision_insert",
		"revision_subject",
		"revision_tag",
		"revision_participant",
		"current_projection",
		"domain_activity",
		"transaction_participant",
		"outbox",
	} {
		if countRecordRevisionStep(steps, forbidden) != 0 {
			t.Fatalf("no-change publish executed forbidden step %q: %#v", forbidden, steps)
		}
	}
}

func TestPostgresRecordRepositoryPublishConflictPreservesDraftBeforeFormalWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*testing.T, *records.RevisionCommitCommand, *records.Draft)
		wantErr error
	}{
		{
			name: "stale etag",
			mutate: func(t *testing.T, command *records.RevisionCommitCommand, draft *records.Draft) {
				staleETag, err := records.NewDraftETag(draft.DraftID, draft.AuthorID, draft.Version-1, draft.Payload)
				if err != nil {
					t.Fatalf("NewDraftETag(stale) error = %v", err)
				}
				command.DraftETag = staleETag
			},
			wantErr: records.ErrDraftConflict,
		},
		{
			name: "update with new-record draft",
			mutate: func(_ *testing.T, _ *records.RevisionCommitCommand, draft *records.Draft) {
				draft.RecordID = ""
				draft.BaseRevisionID = ""
			},
			wantErr: records.ErrDraftRevisionConflict,
		},
		{
			name: "update with different base",
			mutate: func(_ *testing.T, _ *records.RevisionCommitCommand, draft *records.Draft) {
				draft.BaseRevisionID = "rrv_other0000000001"
			},
			wantErr: records.ErrDraftRevisionConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.August, 3, 14, 50, 0, 0, time.UTC)
			steps := make([]string, 0, 20)
			command := testRecordRevisionUpdateCommand(t, records.DomainActivityRecordRevised)
			publishedDraft := testPublishedRecordDraft(t, now, &command, command.RecordID, command.BaseRevisionID)
			tt.mutate(t, &command, &publishedDraft)
			currentRevisionID := command.BaseRevisionID
			currentRevisionNo := int64(1)
			currentRevisionCreatedAt := now.Add(-time.Hour)
			currentCanonicalHash := testRecordRevisionDigest(0x44)
			tx := &fakeRecordRevisionTx{
				now:                      now,
				steps:                    &steps,
				rootExists:               true,
				currentRevisionID:        &currentRevisionID,
				currentRevisionNo:        &currentRevisionNo,
				rootLockVersion:          7,
				rootAuthorizationEpoch:   5,
				currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
				currentRevisionCreatedAt: &currentRevisionCreatedAt,
				publishedDraft:           &publishedDraft,
			}
			repository := newRecordRevisionTestRepository(t, tx, &steps)

			result, err := repository.CommitRevision(context.Background(), command)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CommitRevision() error = %v, want %v", err, tt.wantErr)
			}
			if result != (records.RevisionCommitResult{}) {
				t.Fatalf("CommitRevision() result = %#v, want zero", result)
			}
			want := []string{
				"begin",
				"admission",
				"admission",
				"idempotency_lock",
				"idempotency_claim",
				"fence_reservation_lock",
				"fence_epoch_init",
				"fence_epoch_lock",
				"fence_lease_lock",
				"fence_reservation_recheck",
				"root_lock",
				"draft_lock",
				"rollback_cleanup",
			}
			if !reflect.DeepEqual(steps, want) {
				t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
			}
			for _, forbidden := range []string{"revision_insert", "domain_activity", "outbox", "draft_checkpoint_delete", "draft_delete", "idempotency_complete", "commit"} {
				if countRecordRevisionStep(steps, forbidden) != 0 {
					t.Fatalf("publish conflict executed forbidden step %q: %#v", forbidden, steps)
				}
			}
		})
	}
}

func TestPostgresRecordRepositoryPublishedDraftReplayDoesNotReadCleanedDraft(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 14, 55, 0, 0, time.UTC)
	committedAt := now.Add(-time.Hour)
	steps := make([]string, 0, 20)
	command := testRecordRevisionCommitCommand(t, now)
	_ = testPublishedRecordDraft(t, now, &command, "", "")
	fingerprint, err := command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	revisionID, err := command.RevisionID()
	if err != nil {
		t.Fatalf("RevisionID() error = %v", err)
	}
	tx := &fakeRecordRevisionTx{
		now:                           now,
		steps:                         &steps,
		idempotencyRequestFingerprint: append([]byte(nil), fingerprint[:]...),
		idempotencyResultFingerprint:  append([]byte(nil), fingerprint[:]...),
		replayRevisionID:              revisionID,
		replayRevisionNo:              1,
		replayRevisionCreatedAt:       committedAt,
		replayRevisionCreated:         true,
		publishedDraft:                nil,
	}
	repository := newRecordRevisionTestRepository(t, tx, &steps)

	result, err := repository.CommitRevision(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	if !result.Replayed || !result.Created || result.RevisionID != revisionID || !result.CommittedAt.Equal(committedAt) {
		t.Fatalf("CommitRevision() = %#v, want original published result", result)
	}
	want := []string{
		"begin",
		"admission",
		"admission",
		"idempotency_lock",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"revision_replay",
		"commit",
		"rollback_cleanup",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
	}
}

func TestPostgresRecordRepositoryPublishedDraftCleanupFailureRollsBackFormalMutation(t *testing.T) {
	t.Parallel()

	for _, cutPoint := range []string{"draft_checkpoint_delete", "draft_delete"} {
		t.Run(cutPoint, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.August, 3, 14, 58, 0, 0, time.UTC)
			steps := make([]string, 0, 32)
			command := testRecordRevisionCommitCommand(t, now)
			publishedDraft := testPublishedRecordDraft(t, now, &command, "", "")
			tx := &fakeRecordRevisionTx{now: now, steps: &steps, failAt: cutPoint, publishedDraft: &publishedDraft}
			repository := newRecordRevisionTestRepository(t, tx, &steps)

			result, err := repository.CommitRevision(context.Background(), command)
			if !errors.Is(err, errRecordRevisionCutPoint) {
				t.Fatalf("CommitRevision() error = %v, want cut-point cause", err)
			}
			if result != (records.RevisionCommitResult{}) {
				t.Fatalf("CommitRevision() result = %#v, want zero", result)
			}
			failedIndex := recordRevisionFailureStepIndex(steps, cutPoint)
			rollbackIndex := recordRevisionStepIndex(steps, "rollback_cleanup")
			if failedIndex < 0 || rollbackIndex != failedIndex+1 {
				t.Fatalf("steps after failed %q before rollback: %#v", cutPoint, steps)
			}
			for _, forbidden := range []string{"idempotency_complete", "commit"} {
				if countRecordRevisionStep(steps, forbidden) != 0 {
					t.Fatalf("cleanup failure executed forbidden step %q: %#v", forbidden, steps)
				}
			}
		})
	}
}

func TestPostgresRecordRepositorySameKeyReplayReturnsOriginalCreatedOrNoChangeResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		command                func(*testing.T, time.Time) records.RevisionCommitCommand
		replayRevisionID       func(records.RevisionCommitCommand) string
		replayRevisionNo       int64
		wantCreated            bool
		wantLockVersion        uint64
		wantAuthorizationEpoch uint64
	}{
		{
			name: "created revision",
			command: func(t *testing.T, now time.Time) records.RevisionCommitCommand {
				return testRecordRevisionCommitCommand(t, now)
			},
			replayRevisionID: func(command records.RevisionCommitCommand) string {
				revisionID, err := command.RevisionID()
				if err != nil {
					return ""
				}
				return revisionID
			},
			replayRevisionNo:       1,
			wantCreated:            true,
			wantLockVersion:        1,
			wantAuthorizationEpoch: 1,
		},
		{
			name: "no change",
			command: func(t *testing.T, _ time.Time) records.RevisionCommitCommand {
				return testRecordRevisionUpdateCommand(t, records.DomainActivityRecordRevised)
			},
			replayRevisionID: func(command records.RevisionCommitCommand) string {
				return command.BaseRevisionID
			},
			replayRevisionNo:       4,
			wantCreated:            false,
			wantLockVersion:        7,
			wantAuthorizationEpoch: 5,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.August, 3, 15, 0, 0, 0, time.UTC)
			committedAt := now.Add(-time.Hour)
			steps := make([]string, 0, 20)
			command := test.command(t, now)
			fingerprint, err := command.Idempotency.RequestFingerprint.PersistedBytes()
			if err != nil {
				t.Fatalf("PersistedBytes() error = %v", err)
			}
			replayRevisionID := test.replayRevisionID(command)
			tx := &fakeRecordRevisionTx{
				now:                           now,
				steps:                         &steps,
				idempotencyRequestFingerprint: append([]byte(nil), fingerprint[:]...),
				idempotencyResultFingerprint:  append([]byte(nil), fingerprint[:]...),
				replayRevisionID:              replayRevisionID,
				replayRevisionNo:              test.replayRevisionNo,
				replayRevisionCreatedAt:       committedAt,
				replayRevisionCreated:         test.wantCreated,
			}
			repository := newRecordRevisionTestRepository(t, tx, &steps)

			result, err := repository.CommitRevision(context.Background(), command)
			if err != nil {
				t.Fatalf("CommitRevision() error = %v", err)
			}
			if result.RecordID != command.RecordID || result.RevisionID != replayRevisionID || result.RevisionNo != uint64(test.replayRevisionNo) ||
				result.LockVersion != test.wantLockVersion || result.AuthorizationEpoch != test.wantAuthorizationEpoch ||
				result.Created != test.wantCreated || !result.Replayed || result.Lifecycle != records.LifecycleActive ||
				!result.CommittedAt.Equal(committedAt) {
				t.Fatalf("CommitRevision() = %#v, want original replay result", result)
			}

			want := []string{
				"begin",
				"admission",
				"admission",
				"idempotency_lock",
				"fence_reservation_lock",
				"fence_epoch_init",
				"fence_epoch_lock",
				"fence_lease_lock",
				"fence_reservation_recheck",
				"revision_replay",
				"commit",
				"rollback_cleanup",
			}
			if !reflect.DeepEqual(steps, want) {
				t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
			}
		})
	}
}

func TestPostgresRecordRepositorySameKeyDifferentFingerprintFailsBeforeRecordRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 16, 0, 0, 0, time.UTC)
	steps := make([]string, 0, 8)
	command := testRecordRevisionCommitCommand(t, now)
	originalFingerprint, err := command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	differentFingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordCreate,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testRecordRevisionDigest(0x11),
		RequestScopeDigest: testRecordRevisionDigest(0x22),
		PayloadDigest:      testRecordRevisionDigest(0x99),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	command.Idempotency.RequestFingerprint = differentFingerprint
	tx := &fakeRecordRevisionTx{
		now:                           now,
		steps:                         &steps,
		idempotencyRequestFingerprint: append([]byte(nil), originalFingerprint[:]...),
		idempotencyResultFingerprint:  append([]byte(nil), originalFingerprint[:]...),
	}
	repository := newRecordRevisionTestRepository(t, tx, &steps)

	result, err := repository.CommitRevision(context.Background(), command)
	if !errors.Is(err, recordplatform.ErrIdempotencyKeyReused) {
		t.Fatalf("CommitRevision() error = %v, want ErrIdempotencyKeyReused", err)
	}
	if result != (records.RevisionCommitResult{}) {
		t.Fatalf("CommitRevision() result = %#v, want zero result", result)
	}
	want := []string{"begin", "admission", "admission", "idempotency_lock", "rollback_cleanup"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transaction steps = %#v, want fingerprint rejection before record read %#v", steps, want)
	}
}

func TestPostgresRecordRepositoryArchiveAndUnarchiveUseAtomicLifecycleOnlyTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		current      records.Lifecycle
		target       records.Lifecycle
		wantActivity records.DomainActivityKind
	}{
		{name: "archive", current: records.LifecycleActive, target: records.LifecycleArchived, wantActivity: records.DomainActivityRecordArchived},
		{name: "unarchive", current: records.LifecycleArchived, target: records.LifecycleActive, wantActivity: records.DomainActivityRecordUnarchived},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.August, 3, 17, 0, 0, 0, time.UTC)
			steps := make([]string, 0, 24)
			currentRevisionID := "rrv_current00000004"
			currentRevisionNo := int64(4)
			currentRevisionCreatedAt := now.Add(-time.Hour)
			currentCanonicalHash := testRecordRevisionDigest(0x44)
			tx := &fakeRecordRevisionTx{
				now:                      now,
				steps:                    &steps,
				rootExists:               true,
				rootLifecycle:            string(test.current),
				currentRevisionID:        &currentRevisionID,
				currentRevisionNo:        &currentRevisionNo,
				rootLockVersion:          7,
				rootAuthorizationEpoch:   5,
				currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
				currentRevisionCreatedAt: &currentRevisionCreatedAt,
			}
			repository := newRecordRevisionTestRepository(t, tx, &steps)
			command := testRecordLifecycleCommand(t, test.target)
			command.CurrentRevisionID = currentRevisionID

			result, err := repository.CommitRecordLifecycle(context.Background(), command)
			if err != nil {
				t.Fatalf("CommitRecordLifecycle() error = %v", err)
			}
			if result.RecordID != command.RecordID || result.CurrentRevisionID != currentRevisionID || result.Lifecycle != test.target ||
				result.LockVersion != 8 || result.AuthorizationEpoch != 6 || result.Replayed || !result.ChangedAt.Equal(now) {
				t.Fatalf("CommitRecordLifecycle() = %#v, want lifecycle transition at root versions 8/6", result)
			}
			want := []string{
				"begin", "admission", "admission", "idempotency_lock", "idempotency_claim",
				"fence_reservation_lock", "fence_epoch_init", "fence_epoch_lock", "fence_lease_lock", "fence_reservation_recheck",
				"root_lock", "lifecycle_update", "domain_activity", "outbox", "admission", "idempotency_complete",
				"commit", "rollback_cleanup",
			}
			if !reflect.DeepEqual(steps, want) {
				t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
			}
			if tx.lifecycleTarget != string(test.target) || tx.lifecycleLockVersion != 8 || tx.lifecycleAuthorizationEpoch != 6 ||
				tx.domainActivityKind != string(test.wantActivity) || tx.outboxEventKind != recordplatform.OutboxEventKindRecordUpdated {
				t.Fatalf("persisted lifecycle fact = (%q, %d, %d, %q, %q), want (%q, 8, 6, %q, %q)",
					tx.lifecycleTarget, tx.lifecycleLockVersion, tx.lifecycleAuthorizationEpoch, tx.domainActivityKind, tx.outboxEventKind,
					test.target, test.wantActivity, recordplatform.OutboxEventKindRecordUpdated)
			}
			for _, forbidden := range []string{"root_create", "revision_insert", "revision_subject", "revision_tag", "revision_participant", "current_projection", "transaction_participant"} {
				if countRecordRevisionStep(steps, forbidden) != 0 {
					t.Fatalf("lifecycle path executed forbidden step %q: %#v", forbidden, steps)
				}
			}
		})
	}
}

func TestPostgresRecordRepositoryLifecycleRejectsStaleCASOrRepeatedTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*fakeRecordRevisionTx, *records.RecordLifecycleCommand)
	}{
		{name: "stale current revision", mutate: func(_ *fakeRecordRevisionTx, command *records.RecordLifecycleCommand) {
			command.CurrentRevisionID = "rrv_stale0000000001"
		}},
		{name: "stale lock", mutate: func(_ *fakeRecordRevisionTx, command *records.RecordLifecycleCommand) { command.LockVersion = 6 }},
		{name: "stale authorization epoch", mutate: func(_ *fakeRecordRevisionTx, command *records.RecordLifecycleCommand) { command.AuthorizationEpoch = 4 }},
		{name: "already archived", mutate: func(tx *fakeRecordRevisionTx, _ *records.RecordLifecycleCommand) {
			tx.rootLifecycle = string(records.LifecycleArchived)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.August, 3, 18, 0, 0, 0, time.UTC)
			steps := make([]string, 0, 16)
			currentRevisionID := "rrv_current00000004"
			currentRevisionNo := int64(4)
			currentRevisionCreatedAt := now.Add(-time.Hour)
			currentCanonicalHash := testRecordRevisionDigest(0x44)
			tx := &fakeRecordRevisionTx{
				now:                      now,
				steps:                    &steps,
				rootExists:               true,
				rootLifecycle:            string(records.LifecycleActive),
				currentRevisionID:        &currentRevisionID,
				currentRevisionNo:        &currentRevisionNo,
				rootLockVersion:          7,
				rootAuthorizationEpoch:   5,
				currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
				currentRevisionCreatedAt: &currentRevisionCreatedAt,
			}
			command := testRecordLifecycleCommand(t, records.LifecycleArchived)
			command.CurrentRevisionID = currentRevisionID
			test.mutate(tx, &command)
			repository := newRecordRevisionTestRepository(t, tx, &steps)

			result, err := repository.CommitRecordLifecycle(context.Background(), command)
			if !errors.Is(err, records.ErrRecordRevisionConflict) {
				t.Fatalf("CommitRecordLifecycle() error = %v, want ErrRecordRevisionConflict", err)
			}
			if result != (records.RecordLifecycleResult{}) {
				t.Fatalf("CommitRecordLifecycle() result = %#v, want zero result", result)
			}
			for _, forbidden := range []string{"lifecycle_update", "domain_activity", "outbox", "idempotency_complete", "commit"} {
				if countRecordRevisionStep(steps, forbidden) != 0 {
					t.Fatalf("conflict path executed forbidden step %q: %#v", forbidden, steps)
				}
			}
		})
	}
}

func TestPostgresRecordRepositoryLifecycleSameKeyReplayReturnsOriginalResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		target       records.Lifecycle
		activityKind records.DomainActivityKind
	}{
		{name: "archive", target: records.LifecycleArchived, activityKind: records.DomainActivityRecordArchived},
		{name: "unarchive", target: records.LifecycleActive, activityKind: records.DomainActivityRecordUnarchived},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.August, 3, 19, 0, 0, 0, time.UTC)
			changedAt := now.Add(-time.Hour)
			steps := make([]string, 0, 20)
			command := testRecordLifecycleCommand(t, test.target)
			fingerprint, err := command.Idempotency.RequestFingerprint.PersistedBytes()
			if err != nil {
				t.Fatalf("PersistedBytes() error = %v", err)
			}
			tx := &fakeRecordRevisionTx{
				now:                           now,
				steps:                         &steps,
				idempotencyRequestFingerprint: append([]byte(nil), fingerprint[:]...),
				idempotencyResultFingerprint:  append([]byte(nil), fingerprint[:]...),
				replayLifecycleRevisionID:     command.CurrentRevisionID,
				replayLifecycleActivityKind:   string(test.activityKind),
				replayLifecycleLockVersion:    8,
				replayLifecycleAuthEpoch:      6,
				replayLifecycleChangedAt:      changedAt,
			}
			repository := newRecordRevisionTestRepository(t, tx, &steps)

			result, err := repository.CommitRecordLifecycle(context.Background(), command)
			if err != nil {
				t.Fatalf("CommitRecordLifecycle() error = %v", err)
			}
			if result.RecordID != command.RecordID || result.CurrentRevisionID != command.CurrentRevisionID ||
				result.LockVersion != 8 || result.AuthorizationEpoch != 6 || result.Lifecycle != test.target ||
				!result.Replayed || !result.ChangedAt.Equal(changedAt) {
				t.Fatalf("CommitRecordLifecycle() = %#v, want original replay result", result)
			}
			want := []string{
				"begin", "admission", "admission", "idempotency_lock",
				"fence_reservation_lock", "fence_epoch_init", "fence_epoch_lock", "fence_lease_lock", "fence_reservation_recheck",
				"lifecycle_replay", "commit", "rollback_cleanup",
			}
			if !reflect.DeepEqual(steps, want) {
				t.Fatalf("transaction steps =\n%#v\nwant\n%#v", steps, want)
			}
		})
	}
}

func TestPostgresRecordRepositoryLifecycleRollsBackAtEveryCutPoint(t *testing.T) {
	t.Parallel()

	cutPoints := []string{
		"admission_1",
		"admission_2",
		"idempotency_lock",
		"idempotency_claim",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"root_lock",
		"lifecycle_update",
		"domain_activity",
		"outbox",
		"admission_3",
		"idempotency_complete",
		"commit",
	}
	for _, cutPoint := range cutPoints {
		t.Run(cutPoint, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.August, 3, 20, 0, 0, 0, time.UTC)
			steps := make([]string, 0, 24)
			currentRevisionID := "rrv_current00000004"
			currentRevisionNo := int64(4)
			currentRevisionCreatedAt := now.Add(-time.Hour)
			currentCanonicalHash := testRecordRevisionDigest(0x44)
			tx := &fakeRecordRevisionTx{
				now:                      now,
				steps:                    &steps,
				failAt:                   cutPoint,
				rootExists:               true,
				rootLifecycle:            string(records.LifecycleActive),
				currentRevisionID:        &currentRevisionID,
				currentRevisionNo:        &currentRevisionNo,
				rootLockVersion:          7,
				rootAuthorizationEpoch:   5,
				currentCanonicalHash:     append([]byte(nil), currentCanonicalHash[:]...),
				currentRevisionCreatedAt: &currentRevisionCreatedAt,
			}
			repository := newRecordRevisionTestRepository(t, tx, &steps)
			command := testRecordLifecycleCommand(t, records.LifecycleArchived)
			command.CurrentRevisionID = currentRevisionID

			result, err := repository.CommitRecordLifecycle(context.Background(), command)
			if !errors.Is(err, errRecordRevisionCutPoint) {
				t.Fatalf("CommitRecordLifecycle() error = %v, want cut-point cause", err)
			}
			if result != (records.RecordLifecycleResult{}) {
				t.Fatalf("CommitRecordLifecycle() result = %#v, want zero result after rollback", result)
			}
			if got := countRecordRevisionStep(steps, "rollback_cleanup"); got != 1 {
				t.Fatalf("rollback count = %d; steps=%#v", got, steps)
			}
			if cutPoint != "commit" && countRecordRevisionStep(steps, "commit") != 0 {
				t.Fatalf("commit ran after %q failed; steps=%#v", cutPoint, steps)
			}
			failedIndex := recordRevisionFailureStepIndex(steps, cutPoint)
			rollbackIndex := recordRevisionStepIndex(steps, "rollback_cleanup")
			if failedIndex < 0 || rollbackIndex != failedIndex+1 {
				t.Fatalf("steps after failed %q before rollback: %#v", cutPoint, steps)
			}
		})
	}
}

func TestPostgresRecordRepositoryCreateRevisionRollsBackAtEveryCutPoint(t *testing.T) {
	t.Parallel()

	cutPoints := []string{
		"admission_1",
		"admission_2",
		"idempotency_lock",
		"idempotency_claim",
		"fence_reservation_lock",
		"fence_epoch_init",
		"fence_epoch_lock",
		"fence_lease_lock",
		"fence_reservation_recheck",
		"root_create",
		"root_lock",
		"revision_insert",
		"revision_subject",
		"revision_tag",
		"revision_participant",
		"current_projection",
		"domain_activity",
		"transaction_participant",
		"outbox",
		"admission_3",
		"idempotency_complete",
		"commit",
	}
	for _, cutPoint := range cutPoints {
		t.Run(cutPoint, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
			steps := make([]string, 0, 32)
			tx := &fakeRecordRevisionTx{now: now, steps: &steps, failAt: cutPoint}
			participant := &storeRevisionParticipantStub{name: "search", apply: func(context.Context, pgx.Tx, records.RevisionCommitted) error {
				steps = append(steps, "transaction_participant")
				if tx.shouldFail("transaction_participant") {
					return errRecordRevisionCutPoint
				}
				return nil
			}}
			repository := newRecordRevisionTestRepository(t, tx, &steps, participant)

			result, err := repository.CommitRevision(context.Background(), testRecordRevisionCommitCommand(t, now))
			if !errors.Is(err, errRecordRevisionCutPoint) {
				t.Fatalf("CommitRevision() error = %v, want cut-point cause", err)
			}
			if result != (records.RevisionCommitResult{}) {
				t.Fatalf("CommitRevision() result = %#v, want zero result after rollback", result)
			}
			if got := countRecordRevisionStep(steps, "rollback_cleanup"); got != 1 {
				t.Fatalf("rollback count = %d; steps=%#v", got, steps)
			}
			if cutPoint != "commit" && countRecordRevisionStep(steps, "commit") != 0 {
				t.Fatalf("commit ran after %q failed; steps=%#v", cutPoint, steps)
			}
			failedIndex := recordRevisionFailureStepIndex(steps, cutPoint)
			rollbackIndex := recordRevisionStepIndex(steps, "rollback_cleanup")
			if failedIndex < 0 || rollbackIndex != failedIndex+1 {
				t.Fatalf("steps after failed %q before rollback: %#v", cutPoint, steps)
			}
		})
	}
}

func newRecordRevisionTestRepository(
	t *testing.T,
	tx pgx.Tx,
	steps *[]string,
	participants ...records.RevisionParticipant,
) *PostgresRecordRepository {
	t.Helper()
	registry, err := records.NewRevisionParticipantRegistry(participants)
	if err != nil {
		t.Fatalf("NewRevisionParticipantRegistry() error = %v", err)
	}
	platform := &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			*steps = append(*steps, "begin")
			return tx, nil
		},
		gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error {
			*steps = append(*steps, "admission")
			if fake, ok := tx.(*fakeRecordRevisionTx); ok {
				fake.admissionCalls++
				if fake.shouldFail(fmt.Sprintf("admission_%d", fake.admissionCalls)) {
					return errRecordRevisionCutPoint
				}
			}
			return nil
		}),
	}
	return &PostgresRecordRepository{platform: platform, participants: registry}
}

func testRecordRevisionCommitCommand(t *testing.T, now time.Time) records.RevisionCommitCommand {
	t.Helper()
	input := testStoreCompleteRevisionInput(t)
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordCreate,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testRecordRevisionDigest(0x11),
		RequestScopeDigest: testRecordRevisionDigest(0x22),
		PayloadDigest:      input.CanonicalHash(),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return records.RevisionCommitCommand{
		RecordID:     "rec_order",
		LockVersion:  0,
		Input:        input,
		ActivityKind: records.DomainActivityRecordCreated,
		OutboxTTL:    24 * time.Hour,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: recordplatform.OperationKindRecordCreate,
				Key:           "create-order-1",
			},
			RequestFingerprint: fingerprint,
			OwnerID:            "records_api_01",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          24 * time.Hour,
		},
	}
}

func testRecordRevisionUpdateCommand(t *testing.T, activityKind records.DomainActivityKind) records.RevisionCommitCommand {
	t.Helper()
	input := testStoreCompleteRevisionInput(t)
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordUpdate,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testRecordRevisionDigest(0x11),
		RequestScopeDigest: testRecordRevisionDigest(0x33),
		PayloadDigest:      input.CanonicalHash(),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return records.RevisionCommitCommand{
		RecordID:           "rec_order",
		BaseRevisionID:     "rrv_current00000001",
		LockVersion:        7,
		AuthorizationEpoch: 5,
		Input:              input,
		ActivityKind:       activityKind,
		OutboxTTL:          24 * time.Hour,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: recordplatform.OperationKindRecordUpdate,
				Key:           "update-order-1",
			},
			RequestFingerprint: fingerprint,
			OwnerID:            "records_api_01",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          24 * time.Hour,
		},
	}
}

func testPublishedRecordDraft(
	t *testing.T,
	now time.Time,
	command *records.RevisionCommitCommand,
	recordID string,
	baseRevisionID string,
) records.Draft {
	t.Helper()
	payload, err := records.NewDraftPayload([]byte(`{"title":"Publish revision"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	command.DraftID = "rdf_0123456789abcdef"
	etag, err := records.NewDraftETag(command.DraftID, command.Input.AuthorID(), 4, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	command.DraftETag = etag
	return records.Draft{
		DraftID:        command.DraftID,
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       recordID,
		BaseRevisionID: baseRevisionID,
		AuthorID:       command.Input.AuthorID(),
		Payload:        payload,
		Version:        4,
		ETag:           etag,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		CreatedAt:      now.Add(-2 * time.Hour),
		UpdatedAt:      now,
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}
}

func testRecordLifecycleCommand(t *testing.T, target records.Lifecycle) records.RecordLifecycleCommand {
	t.Helper()
	payloadDigest := testRecordRevisionDigest(0x61)
	if target == records.LifecycleActive {
		payloadDigest = testRecordRevisionDigest(0x62)
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordUpdate,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testRecordRevisionDigest(0x11),
		RequestScopeDigest: testRecordRevisionDigest(0x55),
		PayloadDigest:      payloadDigest,
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return records.RecordLifecycleCommand{
		RecordID:           "rec_order",
		CurrentRevisionID:  "rrv_current00000004",
		LockVersion:        7,
		AuthorizationEpoch: 5,
		TargetLifecycle:    target,
		ActorID:            "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		OutboxTTL:          24 * time.Hour,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: recordplatform.OperationKindRecordUpdate,
				Key:           "lifecycle-order-1",
			},
			RequestFingerprint: fingerprint,
			OwnerID:            "records_api_01",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          24 * time.Hour,
		},
	}
}

func testStoreCompleteRevisionInput(t *testing.T) records.CompleteRevisionInput {
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
		SourceID:     "vps_0123456789abcdef",
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title:                  "Investigate packet loss",
		BodyMarkdown:           "# Packet loss\n",
		MarkdownDialectVersion: records.MarkdownDialectVersionV1,
		RecordType:             records.RecordTypeNote,
		ImpactLevel:            "informational",
		VisibilityScope:        visibility,
		Subjects: []records.RevisionSubject{{
			RegistryVersion:      records.SubjectRegistryVersionV1,
			Kind:                 records.SubjectKindVPS,
			Role:                 records.RelationRoleAffected,
			SourceID:             "vps_0123456789abcdef",
			Primary:              true,
			IdentitySnapshot:     map[string]string{"display_name": "Order VPS"},
			CaptureAuthorization: authorization,
		}},
		Tags:    []string{"network"},
		OwnerID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Participants: []records.RevisionParticipantSnapshot{{
			ParticipantID:    "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
			IdentitySnapshot: map[string]string{"display_name": "Operator"},
		}},
		AuthorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	return input
}

func testRecordRevisionDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

type storeRevisionParticipantStub struct {
	name  string
	apply func(context.Context, pgx.Tx, records.RevisionCommitted) error
}

func (participant *storeRevisionParticipantStub) Name() string {
	return participant.name
}

func (participant *storeRevisionParticipantStub) ApplyRevision(ctx context.Context, tx pgx.Tx, committed records.RevisionCommitted) error {
	return participant.apply(ctx, tx, committed)
}

type fakeRecordRevisionTx struct {
	pgx.Tx
	now                           time.Time
	steps                         *[]string
	failAt                        string
	failed                        bool
	admissionCalls                int
	rootExists                    bool
	rootLifecycle                 string
	currentRevisionID             *string
	currentRevisionNo             *int64
	rootLockVersion               int64
	rootAuthorizationEpoch        int64
	currentCanonicalHash          []byte
	currentRevisionCreatedAt      *time.Time
	insertedRevisionNo            int64
	projectedLockVersion          int64
	projectedAuthorizationEpoch   int64
	domainActivityKind            string
	outboxEventKind               string
	lifecycleTarget               string
	lifecycleLockVersion          int64
	lifecycleAuthorizationEpoch   int64
	idempotencyRequestFingerprint []byte
	idempotencyResultFingerprint  []byte
	replayRevisionID              string
	replayRevisionNo              int64
	replayRevisionCreatedAt       time.Time
	replayRevisionCreated         bool
	replayLifecycleRevisionID     string
	replayLifecycleActivityKind   string
	replayLifecycleLockVersion    int64
	replayLifecycleAuthEpoch      int64
	replayLifecycleChangedAt      time.Time
	publishedDraft                *records.Draft
}

func (tx *fakeRecordRevisionTx) Commit(context.Context) error {
	*tx.steps = append(*tx.steps, "commit")
	if tx.shouldFail("commit") {
		return errRecordRevisionCutPoint
	}
	return nil
}

func (tx *fakeRecordRevisionTx) Rollback(context.Context) error {
	*tx.steps = append(*tx.steps, "rollback_cleanup")
	return nil
}

func (tx *fakeRecordRevisionTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := recordRevisionSQLStep(sql)
	*tx.steps = append(*tx.steps, step)
	if tx.shouldFail(step) {
		return pgconn.CommandTag{}, errRecordRevisionCutPoint
	}
	if step == "current_projection" {
		tx.projectedLockVersion = args[13].(int64)
		tx.projectedAuthorizationEpoch = args[14].(int64)
	}
	switch step {
	case "root_create":
		if tx.rootExists {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case "fence_epoch_init", "revision_subject", "revision_tag", "revision_participant", "current_projection", "idempotency_complete":
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
}

func (tx *fakeRecordRevisionTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	step := recordRevisionSQLStep(sql)
	*tx.steps = append(*tx.steps, step)
	if tx.shouldFail(step) {
		return fakeRecordRevisionRow{err: errRecordRevisionCutPoint}
	}
	switch step {
	case "idempotency_lock":
		if len(tx.idempotencyRequestFingerprint) == 0 {
			return fakeRecordRevisionRow{err: pgx.ErrNoRows}
		}
		return fakeRecordRevisionRow{values: []any{
			tx.idempotencyRequestFingerprint,
			tx.idempotencyResultFingerprint,
			"completed",
			"",
			int64(0),
			nil,
			tx.now.Add(24 * time.Hour),
			tx.now,
		}}
	case "fence_reservation_lock", "fence_lease_lock", "fence_reservation_recheck":
		return fakeRecordRevisionRow{err: pgx.ErrNoRows}
	case "idempotency_claim":
		return fakeRecordRevisionRow{values: []any{"records_api_01", int64(1), tx.now.Add(time.Minute)}}
	case "fence_epoch_lock":
		return fakeRecordRevisionRow{values: []any{int64(0)}}
	case "root_lock":
		lifecycle := tx.rootLifecycle
		if lifecycle == "" {
			lifecycle = string(records.LifecycleActive)
		}
		values := []any{
			"rec_order", "default", lifecycle, tx.currentRevisionID,
			tx.rootLockVersion, tx.rootAuthorizationEpoch, tx.currentCanonicalHash,
		}
		compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
		if strings.Contains(compact, "current_revision.revision_no") {
			values = append(values, tx.currentRevisionNo, tx.currentRevisionCreatedAt)
		}
		return fakeRecordRevisionRow{values: values}
	case "draft_lock":
		if tx.publishedDraft == nil {
			return fakeRecordRevisionRow{err: pgx.ErrNoRows}
		}
		draft := *tx.publishedDraft
		var recordID *string
		var baseRevisionID *string
		if draft.RecordID != "" {
			recordIDValue := draft.RecordID
			baseRevisionIDValue := draft.BaseRevisionID
			recordID = &recordIDValue
			baseRevisionID = &baseRevisionIDValue
		}
		payloadHash := draft.Payload.Hash()
		etagDigest, err := draft.ETag.Digest()
		if err != nil {
			return fakeRecordRevisionRow{err: err}
		}
		return fakeRecordRevisionRow{values: []any{
			draft.DraftID,
			string(draft.ProjectID),
			recordID,
			baseRevisionID,
			draft.AuthorID,
			draft.Payload.JSON(),
			append([]byte(nil), payloadHash[:]...),
			int64(draft.Version),
			append([]byte(nil), etagDigest[:]...),
			draft.WarningAt,
			draft.CreatedAt,
			draft.UpdatedAt,
			draft.ExpiresAt,
		}}
	case "revision_insert":
		tx.insertedRevisionNo = args[4].(int64)
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "domain_activity":
		tx.domainActivityKind = args[4].(string)
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "lifecycle_update":
		tx.lifecycleTarget = args[1].(string)
		tx.lifecycleLockVersion = args[2].(int64)
		tx.lifecycleAuthorizationEpoch = args[3].(int64)
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "outbox":
		tx.outboxEventKind = fmt.Sprint(args[1])
		return fakeRecordRevisionRow{values: []any{int64(41)}}
	case "revision_replay":
		return fakeRecordRevisionRow{values: []any{
			tx.replayRevisionID,
			tx.replayRevisionNo,
			tx.replayRevisionCreatedAt,
			tx.replayRevisionCreated,
		}}
	case "lifecycle_replay":
		return fakeRecordRevisionRow{values: []any{
			tx.replayLifecycleRevisionID,
			tx.replayLifecycleActivityKind,
			tx.replayLifecycleLockVersion,
			tx.replayLifecycleAuthEpoch,
			tx.replayLifecycleChangedAt,
		}}
	default:
		return fakeRecordRevisionRow{err: errors.New("unexpected query row step: " + step)}
	}
}

func (tx *fakeRecordRevisionTx) shouldFail(step string) bool {
	if tx.failed || tx.failAt != step {
		return false
	}
	tx.failed = true
	return true
}

type fakeRecordRevisionRow struct {
	values []any
	err    error
}

func (row fakeRecordRevisionRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("unexpected scan destination count")
	}
	for index := range dest {
		if row.values[index] == nil {
			continue
		}
		target := reflect.ValueOf(dest[index])
		value := reflect.ValueOf(row.values[index])
		if target.Kind() != reflect.Pointer || !value.Type().AssignableTo(target.Elem().Type()) {
			return errors.New("unexpected scan destination type")
		}
		target.Elem().Set(value)
	}
	return nil
}

func recordRevisionSQLStep(sql string) string {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "select request_fingerprint"):
		return "idempotency_lock"
	case strings.Contains(compact, "insert into public.record_idempotency_keys"):
		return "idempotency_claim"
	case strings.Contains(compact, "from public.deletion_reservations") && strings.Contains(compact, "for update"):
		return "fence_reservation_lock"
	case strings.Contains(compact, "insert into public.content_delivery_epochs"):
		return "fence_epoch_init"
	case strings.Contains(compact, "from public.content_delivery_epochs"):
		return "fence_epoch_lock"
	case strings.Contains(compact, "from public.deletion_fence_leases"):
		return "fence_lease_lock"
	case strings.Contains(compact, "from public.deletion_reservations"):
		return "fence_reservation_recheck"
	case strings.Contains(compact, "insert into public.records"):
		return "root_create"
	case strings.Contains(compact, "from public.records") && strings.Contains(compact, "for update"):
		return "root_lock"
	case strings.Contains(compact, "from public.record_drafts") && strings.Contains(compact, "for update"):
		return "draft_lock"
	case strings.Contains(compact, "insert into public.record_revisions"):
		return "revision_insert"
	case strings.Contains(compact, "from public.record_revisions"):
		return "revision_replay"
	case strings.Contains(compact, "insert into public.record_revision_subjects"):
		return "revision_subject"
	case strings.Contains(compact, "insert into public.record_revision_tags"):
		return "revision_tag"
	case strings.Contains(compact, "insert into public.record_revision_participants"):
		return "revision_participant"
	case strings.Contains(compact, "update public.records") && strings.Contains(compact, "set lifecycle"):
		return "lifecycle_update"
	case strings.Contains(compact, "update public.records"):
		return "current_projection"
	case strings.Contains(compact, "insert into public.record_domain_activities"):
		return "domain_activity"
	case strings.Contains(compact, "from public.record_domain_activities"):
		return "lifecycle_replay"
	case strings.Contains(compact, "insert into public.record_outbox"):
		return "outbox"
	case strings.Contains(compact, "delete from public.record_draft_checkpoints"):
		return "draft_checkpoint_delete"
	case strings.Contains(compact, "delete from public.record_drafts"):
		return "draft_delete"
	case strings.Contains(compact, "update public.record_idempotency_keys"):
		return "idempotency_complete"
	default:
		return "unexpected_sql"
	}
}

var errRecordRevisionCutPoint = errors.New("record revision cut point failed")

func countRecordRevisionStep(steps []string, want string) int {
	count := 0
	for _, step := range steps {
		if step == want {
			count++
		}
	}
	return count
}

func recordRevisionFailureStepIndex(steps []string, cutPoint string) int {
	if strings.HasPrefix(cutPoint, "admission_") {
		ordinal := 0
		if _, err := fmt.Sscanf(cutPoint, "admission_%d", &ordinal); err != nil {
			return -1
		}
		seen := 0
		for index, step := range steps {
			if step == "admission" {
				seen++
				if seen == ordinal {
					return index
				}
			}
		}
		return -1
	}
	return recordRevisionStepIndex(steps, cutPoint)
}

func recordRevisionStepIndex(steps []string, want string) int {
	for index, step := range steps {
		if step == want {
			return index
		}
	}
	return -1
}
