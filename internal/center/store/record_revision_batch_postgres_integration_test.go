package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRevisionBatchPreservesSequentialSemantics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	repository := newRevisionMutationAdmittedRepository(t, fixture)
	create := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "First revision"), "batch-sequence-create")
	wrongCreation := create
	wrongCreation.BirthPermit.Creation.CanonicalHash[0] ^= 1
	if err := wrongCreation.Validate(); !errors.Is(err, records.ErrInvalidRevisionCommand) {
		t.Fatalf("birth permit with a different first revision hash = %v", err)
	}
	wrongActor := create
	wrongActor.BirthRequest.ActorScopeDigest[0] ^= 1
	if err := wrongActor.Validate(); !errors.Is(err, records.ErrInvalidRevisionCommand) {
		t.Fatalf("birth permit with a different actor scope = %v", err)
	}
	birth := records.RevisionMutationBirth{AllocationID: create.BirthPermit.AllocationID, Request: create.BirthRequest, Creation: create.BirthPermit.Creation}
	changedBirth := birth
	changedBirth.Request.RequestFingerprint[0] ^= 1
	if _, err := normalizeRecordMutationDeclarations(
		records.RevisionMutationResources{Births: []records.RevisionMutationBirth{birth, changedBirth}},
	); !errors.Is(err, records.ErrInvalidRevisionMutationResources) {
		t.Fatalf("contradictory allocation routes = %v", err)
	}
	declared, err := normalizeRecordMutationDeclarations(records.RevisionMutationResources{Births: []records.RevisionMutationBirth{birth}})
	if err != nil {
		t.Fatal(err)
	}
	if len(declared.resources.Births) != 1 || len(declared.resources.Records) != 1 || declared.resources.Records[0].RecordID != create.RecordID {
		t.Fatalf("birth declaration omitted the record route: %+v", declared.resources)
	}
	birthRank := slices.IndexFunc(declared.order, func(item recordMutationDeclaration) bool {
		return item.family == recordMutationBirths && item.rank == 60
	})
	rootRank := slices.IndexFunc(declared.order, func(item recordMutationDeclaration) bool {
		return item.family == recordMutationRoots && item.rank == 60
	})
	if rootRank < 0 || birthRank <= rootRank {
		t.Fatalf("birth and root declarations have wrong rank order: %+v", declared.order)
	}
	finish := records.RevisionCommitFinish{Actor: create.Actor, OriginKind: "import", OriginDigest: [32]byte{1}, ImportJobID: "rij_wrong_purpose", JobLockVersion: 1}
	if _, err := repository.CommitRevisionsFinishing(ctx, []records.RevisionCommitCommand{create}, finish); !errors.Is(err, records.ErrInvalidRevisionCommand) {
		t.Fatalf("ordinary allocation accepted for import finish: %v", err)
	}
	revisionID, err := create.RevisionID()
	if err != nil {
		t.Fatal(err)
	}
	noop := recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordUpdate, create.RecordID, revisionID, 1, 1,
		create.Input, "batch-sequence-noop")
	update := recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordUpdate, create.RecordID, revisionID, 1, 1,
		recordsPostgresCompleteRevisionInput(t, "Second revision"), "batch-sequence-update")
	results, err := repository.CommitRevisions(ctx, []records.RevisionCommitCommand{create, create, noop, update, update})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 || !results[0].Created || results[0].Replayed || !results[1].Replayed ||
		results[2].Created || results[2].Replayed || results[2].RevisionID != revisionID ||
		!results[3].Created || results[3].RevisionNo != 2 || !results[4].Replayed || results[4].RevisionID != results[3].RevisionID {
		t.Fatalf("ordered create/replay/no-op/update/replay = %#v", results)
	}
	assertRecordsPostgresCurrentProjectionReconciled(t, ctx, fixture.Runtime, create.RecordID, results[3].RevisionID)

	t.Run("conflicting repeated key rolls back every speculative root and claim", func(t *testing.T) {
		first := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Rollback first"), "batch-rollback-first")
		second := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Rollback second"), "batch-rollback-second")
		conflictingAllocation := reserveRevisionBirth(t, ctx, fixture, first.Actor, "batch-rollback-conflict-allocation", recordbirth.PurposeCreate)
		conflict := recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordCreate, conflictingAllocation.RecordID, "", 0, 0,
			recordsPostgresCompleteRevisionInput(t, "Conflicting repeated key"), first.Idempotency.Key.Key)
		conflict = prepareRevisionBirthCommand(t, ctx, fixture, conflictingAllocation, conflict)
		if _, err := repository.CommitRevisions(ctx, []records.RevisionCommitCommand{first, second, conflict}); !errors.Is(err, recordplatform.ErrIdempotencyKeyReused) {
			t.Fatalf("conflicting batch error = %v", err)
		}
		var roots, revisions, keys, outbox, epochs int
		if err := fixture.Runtime.QueryRow(ctx, `select
			(select count(*) from public.records where record_id=any($1::text[])),
			(select count(*) from public.record_revisions where record_id=any($1::text[])),
			(select count(*) from public.record_idempotency_keys where idempotency_key=any($2::text[])),
			(select count(*) from public.record_outbox where subject_id=any($1::text[])),
			(select count(*) from public.content_delivery_epochs where object_id=any($1::text[]))`,
			[]string{first.RecordID, second.RecordID, conflict.RecordID}, []string{first.Idempotency.Key.Key, second.Idempotency.Key.Key}).Scan(&roots, &revisions, &keys, &outbox, &epochs); err != nil {
			t.Fatal(err)
		}
		if roots != 0 || revisions != 0 || keys != 0 || outbox != 0 || epochs != 0 {
			t.Fatalf("failed batch residue roots/revisions/keys/outbox/epochs = %d/%d/%d/%d/%d", roots, revisions, keys, outbox, epochs)
		}
	})
}

func TestPostgresIntegrationRevisionBatchAcquiresRootsBeforeBlobParticipants(t *testing.T) {
	for _, reverseBatch := range []bool{false, true} {
		name := "single"
		if reverseBatch {
			name = "reverse_batch"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
			seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
			seedRepository := newRevisionMutationAdmittedRepository(t, fixture)
			updates := make([]records.RevisionCommitCommand, 2)
			for index := range updates {
				key := fmt.Sprintf("batch-lock-%d", index)
				command := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Seed"), key+"-seed")
				seed, err := seedRepository.CommitRevision(ctx, command)
				if err != nil {
					t.Fatal(err)
				}
				updates[index] = recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordUpdate, seed.RecordID, seed.RevisionID,
					seed.LockVersion, seed.AuthorizationEpoch, recordsPostgresCompleteRevisionInput(t, "Batch update"), key+"-batch")
			}
			sort.Slice(updates, func(i, j int) bool { return updates[i].RecordID < updates[j].RecordID })
			firstPool := revisionBirthBatchRuntimePool(t, ctx, fixture.Runtime)
			secondPool := revisionBirthBatchRuntimePool(t, ctx, fixture.Runtime)
			var firstPID, secondPID int
			if err := firstPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&firstPID); err != nil {
				t.Fatal(err)
			}
			if err := secondPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&secondPID); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			blobResources := func(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
				return records.RevisionMutationResources{BlobTables: []records.RevisionMutationBlobTable{
					records.RevisionMutationBlobMetadataTable, records.RevisionMutationUploadPartsTable,
				}}, nil
			}
			firstParticipant := &storeRevisionParticipantStub{name: "blob_barrier", resources: blobResources, apply: func(ctx context.Context, _ pgx.Tx, committed records.RevisionCommitted) error {
				if committed.Result.RecordID == updates[0].RecordID {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			}}
			secondParticipant := &storeRevisionParticipantStub{name: "blob_writer", resources: blobResources, apply: func(context.Context, pgx.Tx, records.RevisionCommitted) error {
				return nil
			}}
			firstFixture, secondFixture := fixture, fixture
			firstFixture.Runtime, secondFixture.Runtime = firstPool, secondPool
			firstRepository := newRevisionMutationAdmittedRepository(t, firstFixture, firstParticipant)
			secondRepository := newRevisionMutationAdmittedRepository(t, secondFixture, secondParticipant)
			firstResult, secondResult := make(chan error, 1), make(chan error, 1)
			go func() { _, err := firstRepository.CommitRevisions(ctx, updates); firstResult <- err }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			competitors := []records.RevisionCommitCommand{updates[1]}
			if reverseBatch {
				competitors = append(competitors, updates[0])
			}
			for index := range competitors {
				prior := competitors[index]
				competitors[index] = recordsPostgresRevisionCommand(t, prior.Idempotency.Key.OperationKind, prior.RecordID,
					prior.BaseRevisionID, prior.LockVersion, prior.AuthorizationEpoch, recordsPostgresCompleteRevisionInput(t, "Competing update"), prior.RecordID+"-competitor")
			}
			go func() {
				var err error
				if reverseBatch {
					_, err = secondRepository.CommitRevisions(ctx, competitors)
				} else {
					_, err = secondRepository.CommitRevision(ctx, competitors[0])
				}
				secondResult <- err
			}()
			waitForPostgresBlockingPID(t, ctx, fixture.Runtime, secondPID, firstPID)
			close(release)
			if err := waitForRecordPlatformResult(t, firstResult, "batch holding Blob fence"); err != nil {
				t.Fatal(err)
			}
			if err := waitForRecordPlatformResult(t, secondResult, "competing revision"); !errors.Is(err, records.ErrRecordRevisionConflict) {
				t.Fatalf("competing revision error = %v, want stale base conflict", err)
			}
		})
	}
}

func TestPostgresIntegrationRevisionBirthBatchReplaysAfterConcurrentReverseOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	first := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Birth A"), "birth-batch-a")
	second := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Birth B"), "birth-batch-b")
	firstPool := revisionBirthBatchRuntimePool(t, ctx, fixture.Runtime)
	secondPool := revisionBirthBatchRuntimePool(t, ctx, fixture.Runtime)
	var firstPID, secondPID int
	if err := firstPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	if err := secondPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	barrier := &storeRevisionParticipantStub{name: "birth_barrier", resources: func(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
		return records.RevisionMutationResources{}, nil
	}, apply: func(ctx context.Context, _ pgx.Tx, committed records.RevisionCommitted) error {
		if committed.Result.RecordID != first.RecordID {
			return nil
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	firstFixture, secondFixture := fixture, fixture
	firstFixture.Runtime, secondFixture.Runtime = firstPool, secondPool
	firstRepository := newRevisionMutationAdmittedRepository(t, firstFixture, barrier)
	secondRepository := newRevisionMutationAdmittedRepository(t, secondFixture)
	type outcome struct {
		results []records.RevisionCommitResult
		err     error
	}
	firstResult, secondResult := make(chan outcome, 1), make(chan outcome, 1)
	go func() {
		results, err := firstRepository.CommitRevisions(ctx, []records.RevisionCommitCommand{first, second})
		firstResult <- outcome{results, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	go func() {
		results, err := secondRepository.CommitRevisions(ctx, []records.RevisionCommitCommand{second, first})
		secondResult <- outcome{results, err}
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.Runtime, secondPID, firstPID)
	close(release)
	written := waitForRecordPlatformResult(t, firstResult, "first birth batch")
	replayed := waitForRecordPlatformResult(t, secondResult, "reverse birth batch")
	if written.err != nil || replayed.err != nil || len(written.results) != 2 || len(replayed.results) != 2 {
		t.Fatalf("birth batches written=%+v replayed=%+v", written, replayed)
	}
	if written.results[0].Replayed || written.results[1].Replayed || !replayed.results[0].Replayed || !replayed.results[1].Replayed ||
		replayed.results[0].RecordID != written.results[1].RecordID || replayed.results[1].RecordID != written.results[0].RecordID ||
		replayed.results[0].RevisionID != written.results[1].RevisionID || replayed.results[1].RevisionID != written.results[0].RevisionID {
		t.Fatalf("reverse birth replay changed identities: written=%+v replayed=%+v", written.results, replayed.results)
	}
}

func revisionBirthBatchCommand(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture, input records.CompleteRevisionInput, key string) records.RevisionCommitCommand {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: input.AuthorID(), Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	return revisionBirthBatchCommandForActor(t, ctx, fixture, input, key, actor)
}

func revisionBirthBatchCommandForActor(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture, input records.CompleteRevisionInput, key string, actor recordauth.ActorScope) records.RevisionCommitCommand {
	t.Helper()
	allocation := reserveRevisionBirth(t, ctx, fixture, actor, key, recordbirth.PurposeCreate)
	command := recordsPostgresRevisionCommandForActor(t, recordplatform.OperationKindRecordCreate, allocation.RecordID, "", 0, 0, input, key, actor)
	return prepareRevisionBirthCommand(t, ctx, fixture, allocation, command)
}

func reserveRevisionBirth(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture, actor recordauth.ActorScope, key string, purpose recordbirth.Purpose) recordbirth.Allocation {
	t.Helper()
	coordinator, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := recordbirth.Request{
		ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
		Purpose: purpose, IdempotencyKey: key,
		RequestFingerprint: sha256.Sum256([]byte("revision allocation:" + key)),
	}
	allocation, err := coordinator.Reserve(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	return allocation
}

func prepareRevisionBirthCommand(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture, allocation recordbirth.Allocation, command records.RevisionCommitCommand) records.RevisionCommitCommand {
	t.Helper()
	coordinator, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisionID, err := command.RevisionID()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	creation := recordbirth.InitialCreation{
		ReservationID: allocation.ReservationID, RecordID: allocation.RecordID, RevisionID: revisionID,
		AuthorID: command.Actor.UserID, CanonicalHash: command.Input.CanonicalHash(), RequestFingerprint: fingerprint,
	}
	permit, err := coordinator.PrepareCreate(ctx, allocation.Request, creation)
	if err != nil {
		t.Fatal(err)
	}
	command.BirthRequest, command.BirthPermit = allocation.Request, permit
	return command
}

func revisionBirthBatchRuntimePool(t *testing.T, ctx context.Context, existing *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	config := existing.Config().Copy()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
