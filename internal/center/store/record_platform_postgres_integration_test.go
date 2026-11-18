package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	storemigrate "houfeng/internal/center/store/migrate"
)

const (
	recordPlatformFenceEpochBlockLock  int64 = 817_230_001
	recordPlatformObjectClaimBlockLock int64 = 817_230_002
	recordPlatformIdempotencyClaimLock int64 = 817_230_003
)

func TestPostgresIntegrationRecordPlatformFenceSerializesObjectLeaseTakeover(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresFixture(t, ctx)
	fixture.installFenceObjectLeaseRaceTriggers(t, ctx)

	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_fence_race"}
	fixture.seedFenceObjectLeaseRace(t, ctx, object)

	blocker := fixture.openDirectRuntimeConnection(t, ctx)
	defer blocker.Close(ctx)
	for _, lockID := range []int64{recordPlatformFenceEpochBlockLock, recordPlatformObjectClaimBlockLock} {
		if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_lock($1)`, lockID); err != nil {
			t.Fatalf("hold record platform race advisory lock %d: %v", lockID, err)
		}
		defer func(lockID int64) {
			if _, err := blocker.Exec(context.Background(), `select pg_catalog.pg_advisory_unlock($1)`, lockID); err != nil {
				t.Errorf("release record platform race advisory lock %d: %v", lockID, err)
			}
		}(lockID)
	}

	fencePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-fence-race", 1)
	objectPool := fixture.openDirectRuntimePool(t, ctx, "record-platform-object-race", 1)
	fenceRepository := NewPostgresRecordPlatformRepository(fencePool, allowRecordPlatformAdmissionGate)
	objectRepository := NewPostgresRecordPlatformRepository(objectPool, allowRecordPlatformAdmissionGate)

	fenceResult := make(chan error, 1)
	go func() {
		_, err := fenceRepository.FenceDeletionReservation(context.Background(), recordplatform.ReservationFenceInputV1{
			ReservationID:      "drs_fencerace",
			Object:             object,
			OwnerID:            "fence_worker",
			OwnerLeaseDuration: time.Minute,
		})
		fenceResult <- err
	}()
	waitForRecordPlatformBackendLock(t, ctx, fixture.db, "record-platform-fence-race")

	type objectClaimResult struct {
		lease recordplatform.ObjectContentLeaseV1
		err   error
	}
	objectResult := make(chan objectClaimResult, 1)
	go func() {
		lease, err := objectRepository.AcquireObjectContentLease(context.Background(), object, recordplatform.LeaseClaimInputV1{
			OwnerID:       "content_worker",
			LeaseDuration: time.Minute,
		})
		objectResult <- objectClaimResult{lease: lease, err: err}
	}()
	waitForRecordPlatformBackendLock(t, ctx, fixture.db, "record-platform-object-race")

	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_unlock($1)`, recordPlatformFenceEpochBlockLock); err != nil {
		t.Fatalf("release fence epoch blocker: %v", err)
	}
	if err := waitForRecordPlatformResult(t, fenceResult, "fence deletion reservation"); err != nil {
		t.Fatalf("FenceDeletionReservation() error = %v", err)
	}
	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_unlock($1)`, recordPlatformObjectClaimBlockLock); err != nil {
		t.Fatalf("release object claim blocker: %v", err)
	}

	claimed := waitForRecordPlatformResult(t, objectResult, "claim object content lease")
	if !errors.Is(claimed.err, recordplatform.ErrDeletionFenceLeaseLive) {
		t.Fatalf("AcquireObjectContentLease() = (%#v, %v), want ErrDeletionFenceLeaseLive after the committed fence", claimed.lease, claimed.err)
	}

	var liveObjectLeaseCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from public.object_content_leases
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
		  and expires_at > transaction_timestamp()
	`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&liveObjectLeaseCount); err != nil {
		t.Fatalf("count live object content leases after fence: %v", err)
	}
	if liveObjectLeaseCount != 0 {
		t.Fatalf("live object content leases after committed fence = %d, want 0", liveObjectLeaseCount)
	}
}

func TestPostgresIntegrationRecordPlatformConcurrentIdempotencyClaimHasOneFencedWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresFixture(t, ctx)
	fixture.installConcurrentIdempotencyClaimBlocker(t, ctx)

	blocker := fixture.openDirectRuntimeConnection(t, ctx)
	defer blocker.Close(ctx)
	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_lock($1)`, recordPlatformIdempotencyClaimLock); err != nil {
		t.Fatalf("hold idempotency claim blocker: %v", err)
	}
	defer func() {
		if _, err := blocker.Exec(context.Background(), `select pg_catalog.pg_advisory_unlock($1)`, recordPlatformIdempotencyClaimLock); err != nil {
			t.Errorf("release idempotency claim blocker: %v", err)
		}
	}()

	input := recordplatform.IdempotencyClaimInputV1{
		Key: recordplatform.IdempotencyKey{
			ProjectID:     recordplatform.ProjectIDDefault,
			OperationKind: recordplatform.OperationKindRecordCreate,
			Key:           "concurrent-idempotency",
		},
		RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordCreate, 1),
		OwnerLeaseDuration: time.Minute,
		RecordTTL:          5 * time.Minute,
	}
	type claimResult struct {
		claim recordplatform.IdempotencyClaimResultV1
		err   error
	}
	results := make(chan claimResult, 2)
	for _, worker := range []struct {
		owner           string
		applicationName string
		repository      *PostgresRecordPlatformRepository
	}{
		{
			owner:           "idem_worker_a",
			applicationName: "record-platform-idempotency-a",
			repository:      NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-idempotency-a", 1), allowRecordPlatformAdmissionGate),
		},
		{
			owner:           "idem_worker_b",
			applicationName: "record-platform-idempotency-b",
			repository:      NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-idempotency-b", 1), allowRecordPlatformAdmissionGate),
		},
	} {
		worker := worker
		go func() {
			claimInput := input
			claimInput.OwnerID = worker.owner
			claim, err := worker.repository.ClaimIdempotency(context.Background(), claimInput)
			results <- claimResult{claim: claim, err: err}
		}()
	}
	waitForRecordPlatformBackendLock(t, ctx, fixture.db, "record-platform-idempotency-a")
	waitForRecordPlatformBackendLock(t, ctx, fixture.db, "record-platform-idempotency-b")
	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_unlock($1)`, recordPlatformIdempotencyClaimLock); err != nil {
		t.Fatalf("release idempotency claim blocker: %v", err)
	}

	var winnerCount int
	var inProgressCount int
	for range 2 {
		result := waitForRecordPlatformResult(t, results, "concurrent idempotency claim")
		switch {
		case result.err == nil && result.claim.Owner != nil && result.claim.Owner.Generation == 1:
			winnerCount++
		case errors.Is(result.err, recordplatform.ErrIdempotencyInProgress):
			inProgressCount++
		default:
			t.Fatalf("concurrent ClaimIdempotency() = (%#v, %v), want one generation-one owner or ErrIdempotencyInProgress", result.claim, result.err)
		}
	}
	if winnerCount != 1 || inProgressCount != 1 {
		t.Fatalf("concurrent idempotency outcomes = winners %d in-progress %d, want 1/1", winnerCount, inProgressCount)
	}
}

func TestPostgresIntegrationRecordPlatformConcurrentOutboxAndLeaseClaimsHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	t.Run("outbox", func(t *testing.T) {
		fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
		seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-race-parent")

		results := make(chan struct {
			claim *recordplatform.ClaimedOutboxEventV1
			err   error
		}, 2)
		start := make(chan struct{})
		for _, repository := range []*PostgresRecordPlatformRepository{
			NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-outbox-claim-a", 1), gate),
			NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-outbox-claim-b", 1), gate),
		} {
			repository := repository
			go func() {
				<-start
				claim, err := repository.ClaimOutbox(context.Background(), recordplatform.OutboxClaimInputV1{OwnerID: "outbox_worker", OwnerLeaseDuration: time.Minute})
				results <- struct {
					claim *recordplatform.ClaimedOutboxEventV1
					err   error
				}{claim: claim, err: err}
			}()
		}
		close(start)

		var claimedCount int
		for range 2 {
			result := waitForRecordPlatformResult(t, results, "concurrent outbox claim")
			if result.err != nil {
				t.Fatalf("ClaimOutbox() error = %v", result.err)
			}
			if result.claim != nil {
				claimedCount++
				if result.claim.Owner.Generation != 1 {
					t.Fatalf("outbox claim owner generation = %d, want 1", result.claim.Owner.Generation)
				}
			}
		}
		if claimedCount != 1 {
			t.Fatalf("concurrent outbox claim winners = %d, want 1", claimedCount)
		}
	})

	t.Run("object content lease", func(t *testing.T) {
		fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
		created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "lease-race-parent")
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}

		results := make(chan struct {
			lease recordplatform.ObjectContentLeaseV1
			err   error
		}, 2)
		start := make(chan struct{})
		for _, worker := range []struct {
			owner           string
			applicationName string
		}{
			{owner: "lease_worker_a", applicationName: "record-platform-lease-claim-a"},
			{owner: "lease_worker_b", applicationName: "record-platform-lease-claim-b"},
		} {
			worker := worker
			repository := NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, worker.applicationName, 1), gate)
			go func() {
				<-start
				lease, err := repository.AcquireObjectContentLease(context.Background(), object, recordplatform.LeaseClaimInputV1{OwnerID: worker.owner, LeaseDuration: time.Minute})
				results <- struct {
					lease recordplatform.ObjectContentLeaseV1
					err   error
				}{lease: lease, err: err}
			}()
		}
		close(start)

		var winnerCount int
		var heldCount int
		for range 2 {
			result := waitForRecordPlatformResult(t, results, "concurrent object-content lease claim")
			switch {
			case result.err == nil && result.lease.Owner.Generation == 1:
				winnerCount++
			case errors.Is(result.err, recordplatform.ErrLeaseAlreadyHeld):
				heldCount++
			default:
				t.Fatalf("AcquireObjectContentLease() = (%#v, %v), want one generation-one owner or ErrLeaseAlreadyHeld", result.lease, result.err)
			}
		}
		if winnerCount != 1 || heldCount != 1 {
			t.Fatalf("concurrent object-content lease outcomes = winners %d held %d, want 1/1", winnerCount, heldCount)
		}
	})
}

func TestPostgresIntegrationRecordPlatformRejectsRawDeletionTokenTransportBeforePersistence(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresFixture(t, ctx)
	repository := NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-token-rejection", 1), allowRecordPlatformAdmissionGate)
	transport := "drt1_" + strings.Repeat("c", 43)
	if _, err := recordplatform.ParseDeletionRequestTokenV1(transport); err != nil {
		t.Fatalf("canonical deletion token fixture must parse: %v", err)
	}

	if _, err := repository.ClaimIdempotency(ctx, recordplatform.IdempotencyClaimInputV1{
		Key: recordplatform.IdempotencyKey{
			ProjectID:     recordplatform.ProjectIDDefault,
			OperationKind: recordplatform.OperationKindRecordDelete,
			Key:           transport,
		},
		RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordDelete, 0x71),
		OwnerID:            "token_rejection_worker",
		OwnerLeaseDuration: time.Minute,
		RecordTTL:          5 * time.Minute,
	}); !errors.Is(err, recordplatform.ErrInvalidIdempotencyClaim) {
		t.Fatalf("ClaimIdempotency() raw deletion token error = %v, want ErrInvalidIdempotencyClaim", err)
	}

	err := repository.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		_, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
			Event: recordplatform.OutboxEvent{
				ProjectID:          string(recordplatform.ProjectIDDefault),
				EventKind:          recordplatform.OutboxEventKindRecordDeleted,
				SubjectKind:        recordplatform.OutboxSubjectKindRecord,
				SubjectID:          transport,
				AuthorizationEpoch: 0,
			},
			ExpiresAfter: time.Hour,
		})
		return err
	})
	if !errors.Is(err, recordplatform.ErrInvalidOutboxEnqueue) {
		t.Fatalf("RunRecordPlatformTransaction() raw deletion token error = %v, want ErrInvalidOutboxEnqueue", err)
	}

	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-token-rejection-inspect", 1)
	var idempotencyRows, outboxRows int
	if err := runtimePool.QueryRow(ctx, `
		select count(*)::int
		from public.record_idempotency_keys
		where idempotency_key = $1
	`, transport).Scan(&idempotencyRows); err != nil {
		t.Fatalf("count rejected idempotency rows as direct runtime: %v", err)
	}
	if err := runtimePool.QueryRow(ctx, `
		select count(*)::int
		from public.record_outbox
		where subject_id = $1
	`, transport).Scan(&outboxRows); err != nil {
		t.Fatalf("count rejected outbox rows as direct runtime: %v", err)
	}
	if idempotencyRows != 0 || outboxRows != 0 {
		t.Fatalf("raw deletion token persistence rows = idempotency %d outbox %d, want 0/0", idempotencyRows, outboxRows)
	}
}

func TestPostgresIntegrationRecordPlatformTransactionRollsBackBusinessIdempotencyAndOutbox(t *testing.T) {
	ctx := context.Background()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	fixture.installAtomicBusinessFactTable(t, ctx)
	repository := NewPostgresRecordPlatformRepository(runtime, gate)
	const (
		factID         = "fact_atomicrollback"
		idempotencyKey = "atomic-rollback"
	)
	parent := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "atomic-rollback-parent")
	subjectID := parent.RecordID
	var initialOutboxRows int
	if err := runtime.QueryRow(ctx, `select count(*)::int from public.record_outbox where subject_id=$1`, subjectID).Scan(&initialOutboxRows); err != nil {
		t.Fatal(err)
	}
	key := recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: idempotencyKey}
	rollback := errors.New("force atomic record platform rollback")

	err := repository.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		acquired, err := acquireEffectMutationResources(ctx, transaction.tx, []string{subjectID}, nil,
			records.RevisionMutationResources{Idempotency: []records.RevisionMutationIdempotency{{Key: key}}})
		if err != nil {
			return err
		}
		transaction.registerAcquiredRecordMutationResources(acquired)
		if _, err := recordPlatformTestExec(ctx, transaction, `insert into public.record_platform_test_business_facts_v1 (fact_id) values ($1)`, factID); err != nil {
			return err
		}
		if _, err := transaction.ClaimIdempotency(ctx, recordplatform.IdempotencyClaimInputV1{
			Key:                key,
			RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordCreate, 0x81),
			OwnerID:            "atomic_rollback_worker",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          5 * time.Minute,
		}); err != nil {
			return err
		}
		event, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{
			Event: recordplatform.OutboxEvent{
				ProjectID:          string(recordplatform.ProjectIDDefault),
				EventKind:          recordplatform.OutboxEventKindRecordCreated,
				SubjectKind:        recordplatform.OutboxSubjectKindRecord,
				SubjectID:          subjectID,
				AuthorizationEpoch: parent.AuthorizationEpoch,
			},
			ExpiresAfter: time.Hour,
		})
		if err != nil {
			return err
		}
		if event.Event.RowID <= 0 {
			return errors.New("outbox enqueue did not return a row identity")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("RunRecordPlatformTransaction() rollback error = %v, want forced callback error", err)
	}

	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-atomic-rollback-inspect", 1)
	var businessRows, idempotencyRows, outboxRows int
	if err := runtimePool.QueryRow(ctx, `select count(*)::int from public.record_platform_test_business_facts_v1 where fact_id = $1`, factID).Scan(&businessRows); err != nil {
		t.Fatalf("count rolled-back business rows as direct runtime: %v", err)
	}
	if err := runtimePool.QueryRow(ctx, `
		select count(*)::int
		from public.record_idempotency_keys
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
	`, recordplatform.ProjectIDDefault, recordplatform.OperationKindRecordCreate, idempotencyKey).Scan(&idempotencyRows); err != nil {
		t.Fatalf("count rolled-back idempotency rows as direct runtime: %v", err)
	}
	if err := runtimePool.QueryRow(ctx, `select count(*)::int from public.record_outbox where subject_id = $1`, subjectID).Scan(&outboxRows); err != nil {
		t.Fatalf("count rolled-back outbox rows as direct runtime: %v", err)
	}
	if businessRows != 0 || idempotencyRows != 0 || outboxRows != initialOutboxRows {
		t.Fatalf("rolled-back transaction rows = business %d idempotency %d outbox %d, want 0/0/%d", businessRows, idempotencyRows, outboxRows, initialOutboxRows)
	}
}

func TestPostgresIntegrationRecordPlatformExpiredTakeoverFencesStaleFinalizers(t *testing.T) {
	ctx := context.Background()
	t.Run("idempotency completion", func(t *testing.T) {
		fixture := newRecordPlatformPostgresFixture(t, ctx)
		repository := NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-idempotency-takeover", 1), allowRecordPlatformAdmissionGate)
		input := recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: recordplatform.OperationKindRecordUpdate,
				Key:           "expired-idempotency",
			},
			RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 2),
			OwnerID:            "idem_owner_a",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          5 * time.Minute,
		}
		first, err := repository.ClaimIdempotency(ctx, input)
		if err != nil || first.Owner == nil || first.Owner.Generation != 1 {
			t.Fatalf("first ClaimIdempotency() = (%#v, %v), want generation-one owner", first, err)
		}
		fixture.expireIdempotencyOwner(t, ctx, input.Key)
		input.OwnerID = "idem_owner_b"
		second, err := repository.ClaimIdempotency(ctx, input)
		if err != nil || second.Owner == nil || second.Owner.Generation != 2 {
			t.Fatalf("takeover ClaimIdempotency() = (%#v, %v), want generation-two owner", second, err)
		}
		if err := repository.CompleteIdempotency(ctx, input.Key, *first.Owner, recordPlatformIntegrationRequestFingerprint(t, input.Key.OperationKind, 3)); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("CompleteIdempotency() from stale owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.CompleteIdempotency(ctx, input.Key, *second.Owner, recordPlatformIntegrationRequestFingerprint(t, input.Key.OperationKind, 4)); err != nil {
			t.Fatalf("CompleteIdempotency() from current owner: %v", err)
		}
	})

	t.Run("outbox sent finalization", func(t *testing.T) {
		fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
		seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-takeover-parent")
		repository := NewPostgresRecordPlatformRepository(runtime, gate)
		first, err := repository.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "outbox_owner_a", OwnerLeaseDuration: time.Minute})
		if err != nil || first == nil || first.Owner.Generation != 1 {
			t.Fatalf("first ClaimOutbox() = (%#v, %v), want generation-one owner", first, err)
		}
		fixture.expireOutboxOwner(t, ctx, first.Event.RowID)
		second, err := repository.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "outbox_owner_b", OwnerLeaseDuration: time.Minute})
		if err != nil || second == nil || second.Owner.Generation != 2 {
			t.Fatalf("takeover ClaimOutbox() = (%#v, %v), want generation-two owner", second, err)
		}
		if err := repository.CancelOutbox(ctx, *first); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("CancelOutbox() from stale owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.RetryOutbox(ctx, *first, time.Second); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("RetryOutbox() from stale owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.MarkOutboxSent(ctx, *first); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("MarkOutboxSent() from stale owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.MarkOutboxSent(ctx, *second); err != nil {
			t.Fatalf("MarkOutboxSent() from current owner: %v", err)
		}
	})

	t.Run("object content release", func(t *testing.T) {
		fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
		created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "lease-takeover-parent")
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}
		repository := NewPostgresRecordPlatformRepository(runtime, gate)
		first, err := repository.AcquireObjectContentLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "lease_owner_a", LeaseDuration: time.Minute})
		if err != nil || first.Owner.Generation != 1 {
			t.Fatalf("first AcquireObjectContentLease() = (%#v, %v), want generation-one owner", first, err)
		}
		fixture.expireObjectContentLease(t, ctx, object)
		second, err := repository.AcquireObjectContentLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "lease_owner_b", LeaseDuration: time.Minute})
		if err != nil || second.Owner.Generation != 2 {
			t.Fatalf("takeover AcquireObjectContentLease() = (%#v, %v), want generation-two owner", second, err)
		}
		if err := repository.ReleaseObjectContentLease(ctx, object, first.Owner); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("ReleaseObjectContentLease() from stale owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.ReleaseObjectContentLease(ctx, object, second.Owner); err != nil {
			t.Fatalf("ReleaseObjectContentLease() from current owner: %v", err)
		}
	})
}

func TestPostgresIntegrationOutboxRequiresAcquiredRecordRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	parent := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-required-parent")
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "outbox_route_owner", OwnerLeaseDuration: time.Minute})
	if err != nil || claim == nil {
		t.Fatalf("claim outbox = %#v, %v", claim, err)
	}
	input := recordplatform.OutboxEnqueueInputV1{Event: recordplatform.OutboxEvent{
		ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordUpdated,
		SubjectKind: recordplatform.OutboxSubjectKindRecord, SubjectID: parent.RecordID, AuthorizationEpoch: parent.AuthorizationEpoch,
	}, ExpiresAfter: time.Hour}
	err = queue.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		_, err := transaction.EnqueueOutbox(ctx, input)
		return err
	})
	if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("enqueue without acquired route = %v", err)
	}
	err = queue.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		return transaction.AssertOutboxClaim(ctx, *claim)
	})
	if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("assert without acquired route = %v", err)
	}
	err = queue.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		acquired, err := acquireEffectMutationResources(ctx, transaction.tx, []string{parent.RecordID}, nil, records.RevisionMutationResources{})
		if err != nil {
			return err
		}
		transaction.registerAcquiredRecordMutationResources(acquired)
		if err := transaction.AssertOutboxClaim(ctx, *claim); err != nil {
			return err
		}
		_, err = transaction.EnqueueOutbox(ctx, input)
		return err
	})
	if err != nil {
		t.Fatalf("assert and enqueue with acquired route: %v", err)
	}
	var count int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_outbox where subject_id=$1`, parent.RecordID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("outbox row count=%d error=%v, want original plus admitted enqueue", count, err)
	}
}

func TestPostgresIntegrationOutboxFenceAndTerminalOutcomes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	ids := make([]string, 4)
	for index := range ids {
		ids[index] = seedAdmittedOutboxRecord(t, ctx, runtime, gate, fmt.Sprintf("outbox-fence-parent-%d", index)).RecordID
	}
	claims := make([]recordplatform.ClaimedOutboxEventV1, 2)
	for index := range claims {
		claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "outbox_fence_worker", OwnerLeaseDuration: time.Minute})
		if err != nil || claim == nil || claim.Event.SubjectID != ids[index] {
			t.Fatalf("claim %d = (%#v, %v)", index, claim, err)
		}
		claims[index] = *claim
	}
	for _, id := range ids[:3] {
		_, err := queue.AcquireDeletionFenceLease(ctx, recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: id},
			recordplatform.LeaseClaimInputV1{OwnerID: "outbox_fence_owner", LeaseDuration: time.Minute})
		if err != nil {
			t.Fatalf("acquire deletion fence for %s: %v", id, err)
		}
	}
	if err := queue.RetryOutbox(ctx, claims[0], time.Second); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("retry behind deletion fence = %v, want reserved", err)
	}
	if err := queue.CancelOutbox(ctx, claims[0]); err != nil {
		t.Fatalf("cancel terminal claim behind deletion fence: %v", err)
	}
	if err := queue.MarkOutboxSent(ctx, claims[1]); err != nil {
		t.Fatalf("record terminal sent outcome behind deletion fence: %v", err)
	}
	claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "outbox_unrelated_worker", OwnerLeaseDuration: time.Minute})
	if err != nil || claim == nil || claim.Event.SubjectID != ids[3] {
		t.Fatalf("claim after fenced candidate = (%#v, %v), want unrelated record", claim, err)
	}
	var cancelled, sent, pending string
	if err := fixture.db.QueryRow(ctx, `select
		(select status from public.record_outbox where outbox_row_id=$1),
		(select status from public.record_outbox where outbox_row_id=$2),
		(select status from public.record_outbox where subject_id=$3)`, claims[0].Event.RowID, claims[1].Event.RowID, ids[2]).Scan(&cancelled, &sent, &pending); err != nil {
		t.Fatal(err)
	}
	if cancelled != "cancelled" || sent != "sent" || pending != "pending" {
		t.Fatalf("outbox states = %q/%q/%q, want cancelled/sent/pending", cancelled, sent, pending)
	}
}

func TestPostgresIntegrationOutboxClaimPagesPastFencedRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	blocked := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-page-parent-a")
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	if err := queue.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		acquired, err := acquireEffectMutationResources(ctx, transaction.tx, []string{blocked.RecordID}, nil, records.RevisionMutationResources{})
		if err != nil {
			return err
		}
		transaction.registerAcquiredRecordMutationResources(acquired)
		for range 129 {
			_, err := transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{Event: recordplatform.OutboxEvent{
				ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordUpdated,
				SubjectKind: recordplatform.OutboxSubjectKindRecord, SubjectID: blocked.RecordID,
				AuthorizationEpoch: blocked.AuthorizationEpoch,
			}, ExpiresAfter: time.Hour})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("enqueue fenced page: %v", err)
	}
	if _, err := queue.AcquireDeletionFenceLease(ctx, recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: blocked.RecordID},
		recordplatform.LeaseClaimInputV1{OwnerID: "outbox_page_fence", LeaseDuration: 10 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	available := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-page-parent-b")
	claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "outbox_page_worker", OwnerLeaseDuration: time.Minute})
	if err != nil || claim == nil || claim.Event.SubjectID != available.RecordID {
		t.Fatalf("claim after fenced pages = %#v, %v", claim, err)
	}
	var pending int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_outbox where subject_id=$1 and status='pending'`, blocked.RecordID).Scan(&pending); err != nil || pending != 130 {
		t.Fatalf("fenced pending events=%d error=%v, want 130", pending, err)
	}
}

func TestPostgresIntegrationOutboxRetrySerializesBeforeDeletionFence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	parent := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-writer-first-parent")
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "writer_first", OwnerLeaseDuration: time.Minute})
	if err != nil || claim == nil {
		t.Fatalf("claim outbox: %#v %v", claim, err)
	}
	for _, statement := range []string{
		`create table public.test_outbox_retry_latch (id integer primary key)`,
		`insert into public.test_outbox_retry_latch values (1)`,
		`create function record_platform_internal.test_outbox_retry_latch() returns trigger
		 language plpgsql security definer set search_path=pg_catalog as $$
		 begin perform id from public.test_outbox_retry_latch where id=1 for update; return new; end $$`,
		`create trigger test_outbox_retry_latch after update on public.record_outbox for each row
		 when (old.status='processing' and new.status='pending') execute function record_platform_internal.test_outbox_retry_latch()`,
	} {
		if _, err := fixture.db.Exec(ctx, statement); err != nil {
			t.Fatalf("install retry barrier: %v", err)
		}
	}
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select id from public.test_outbox_retry_latch where id=1 for update`); err != nil {
		t.Fatal(err)
	}
	writerPool := fixture.openDirectRuntimePool(t, ctx, "outbox-writer-first", 1)
	fencePool := fixture.openDirectRuntimePool(t, ctx, "outbox-fence-second", 1)
	var writerPID, fencePID int
	if err := writerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	if err := fencePool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&fencePID); err != nil {
		t.Fatal(err)
	}
	writerResult := make(chan error, 1)
	go func() {
		writerResult <- NewPostgresRecordPlatformRepository(writerPool, gate).RetryOutbox(ctx, *claim, time.Second)
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, writerPID, holderPID)
	fenceResult := make(chan error, 1)
	go func() {
		_, err := NewPostgresRecordPlatformRepository(fencePool, gate).AcquireDeletionFenceLease(ctx,
			recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: parent.RecordID},
			recordplatform.LeaseClaimInputV1{OwnerID: "outbox_later_fence", LeaseDuration: time.Minute})
		fenceResult <- err
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, fencePID, writerPID)
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := waitForRecordPlatformResult(t, writerResult, "outbox retry"); err != nil {
		t.Fatalf("retry before deletion fence: %v", err)
	}
	if err := waitForRecordPlatformResult(t, fenceResult, "outbox deletion fence"); err != nil {
		t.Fatalf("deletion fence after retry: %v", err)
	}
	var status string
	if err := fixture.db.QueryRow(ctx, `select status from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("writer-first durable status=%q error=%v", status, err)
	}
}

func TestPostgresIntegrationOutboxFinalizationRestartsOnRouteDrift(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	first := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-route-parent-a")
	second := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-route-parent-b")
	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values ('ract_outboxroute',$1,1,'Route','open','usr_aaaaaaaaaaaaaaaaaaaaaaaa','usr_aaaaaaaaaaaaaaaaaaaaaaaa',0)`, first.RecordID); err != nil {
		t.Fatal(err)
	}
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	if err := queue.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		acquired, err := acquireEffectMutationResources(ctx, transaction.tx, []string{first.RecordID}, nil, records.RevisionMutationResources{})
		if err != nil {
			return err
		}
		transaction.registerAcquiredRecordMutationResources(acquired)
		_, err = transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{Event: recordplatform.OutboxEvent{
			ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordActionAssigned,
			SubjectKind: recordplatform.OutboxSubjectKindAction, SubjectID: "ract_outboxroute",
			SourceVersion: 1, AuthorizationEpoch: first.AuthorizationEpoch,
		}, ExpiresAfter: time.Hour})
		return err
	}); err != nil {
		t.Fatalf("enqueue real action route: %v", err)
	}
	claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, "route_drift_owner")
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, first.RecordID); err != nil {
		t.Fatal(err)
	}
	secondHolder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondHolder.Rollback(context.Background()) }()
	if _, err := secondHolder.Exec(ctx, `select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, second.RecordID); err != nil {
		t.Fatal(err)
	}
	workerPool := fixture.openDirectRuntimePool(t, ctx, "outbox-route-drift", 1)
	var workerPID int
	if err := workerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- NewPostgresRecordPlatformRepository(workerPool, gate).CancelOutbox(ctx, claim) }()
	waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
	if _, err := fixture.db.Exec(ctx, `update public.record_actions set record_id=$2 where action_id=$1`, "ract_outboxroute", second.RecordID); err != nil {
		t.Fatalf("change persisted action route: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if err := waitForRecordPlatformResult(t, result, "outbox route drift"); !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		t.Fatalf("cancel with changed subject route = %v, want serialization failure", err)
	}
	// B remains locked while the worker finishes: it never substitutes B's
	// scope for the immutable route captured when the event was enqueued.
	var status, route string
	if err := fixture.db.QueryRow(ctx, `select status,record_id from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&status, &route); err != nil {
		t.Fatal(err)
	}
	if status != "processing" || route != first.RecordID {
		t.Fatalf("drifted outbox row = %s/%s, want processing/%s", status, route, first.RecordID)
	}
	if err := secondHolder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresIntegrationOutboxFinalizersRejectExpiryAfterLockWait(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(context.Context, *PostgresRecordPlatformRepository, recordplatform.ClaimedOutboxEventV1) error
	}{
		{name: "cancel", run: func(ctx context.Context, queue *PostgresRecordPlatformRepository, claim recordplatform.ClaimedOutboxEventV1) error {
			return queue.CancelOutbox(ctx, claim)
		}},
		{name: "sent", run: func(ctx context.Context, queue *PostgresRecordPlatformRepository, claim recordplatform.ClaimedOutboxEventV1) error {
			return queue.MarkOutboxSent(ctx, claim)
		}},
		{name: "retry", run: func(ctx context.Context, queue *PostgresRecordPlatformRepository, claim recordplatform.ClaimedOutboxEventV1) error {
			return queue.RetryOutbox(ctx, claim, time.Second)
		}},
	} {
		for _, lockKind := range []string{"record", "outbox"} {
			t.Run(operation.name+"-"+lockKind, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
				parent := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-expiry-parent")
				queue := NewPostgresRecordPlatformRepository(runtime, gate)
				claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "expiry_worker", OwnerLeaseDuration: time.Minute})
				if err != nil || claim == nil {
					t.Fatalf("claim outbox: %#v %v", claim, err)
				}
				if err := fixture.db.QueryRow(ctx, `update public.record_outbox set owner_expires_at=clock_timestamp()+interval '2 seconds'
				where outbox_row_id=$1 returning owner_expires_at`, claim.Event.RowID).Scan(&claim.Owner.ExpiresAt); err != nil {
					t.Fatal(err)
				}
				holder, err := fixture.db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = holder.Rollback(context.Background()) }()
				var holderPID int
				if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
					t.Fatal(err)
				}
				if lockKind == "record" {
					if _, err := holder.Exec(ctx, `select 1 from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, parent.RecordID); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := holder.Exec(ctx, `select 1 from public.record_outbox where outbox_row_id=$1 for update`, claim.Event.RowID); err != nil {
						t.Fatal(err)
					}
				}
				workerPool := fixture.openDirectRuntimePool(t, ctx, "outbox-expiry-wait-"+operation.name, 1)
				var workerPID int
				if err := workerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
					t.Fatal(err)
				}
				result := make(chan error, 1)
				go func() { result <- operation.run(ctx, NewPostgresRecordPlatformRepository(workerPool, gate), *claim) }()
				if lockKind == "record" {
					waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
				} else {
					var pgErr *pgconn.PgError
					if err := waitForRecordPlatformResult(t, result, "contended outbox row"); !errors.As(err, &pgErr) || pgErr.Code != "40001" {
						t.Fatalf("contended outbox finalizer = %v, want serialization failure", err)
					}
				}
				if _, err := fixture.db.Exec(ctx, `select pg_catalog.pg_sleep_until($1::timestamptz + interval '1 millisecond')`, claim.Owner.ExpiresAt); err != nil {
					t.Fatal(err)
				}
				if err := holder.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				var finalErr error
				if lockKind == "record" {
					finalErr = waitForRecordPlatformResult(t, result, "outbox expiry after wait")
				} else {
					finalErr = operation.run(ctx, NewPostgresRecordPlatformRepository(workerPool, gate), *claim)
				}
				if !errors.Is(finalErr, recordplatform.ErrLostOwnerLease) {
					t.Fatalf("%s after expiry wait = %v, want lost owner", operation.name, finalErr)
				}
				var status string
				if err := fixture.db.QueryRow(ctx, `select status from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&status); err != nil || status != "processing" {
					t.Fatalf("expired finalization status=%q error=%v", status, err)
				}
			})
		}
	}
}

func TestPostgresIntegrationIdempotencyEntryPointsShareScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	seed := NewPostgresRecordPlatformRepository(runtime, gate)
	for _, transactional := range []bool{false, true} {
		for _, operation := range []string{"claim", "complete", "renew", "release"} {
			name := fmt.Sprintf("%s-%t", operation, transactional)
			t.Run(name, func(t *testing.T) {
				input := recordplatform.IdempotencyClaimInputV1{
					Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordUpdate, Key: "scope-" + name},
					RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x31),
					OwnerID:            "scope_owner", OwnerLeaseDuration: time.Minute, RecordTTL: 5 * time.Minute,
				}
				fingerprint := recordPlatformIntegrationRequestFingerprint(t, input.Key.OperationKind, 0x32)
				var owner recordplatform.OwnerLease
				if operation != "claim" {
					claimed, err := seed.ClaimIdempotency(ctx, input)
					if err != nil || claimed.Owner == nil {
						t.Fatalf("seed idempotency owner: %#v %v", claimed, err)
					}
					owner = *claimed.Owner
				}
				holder, err := fixture.db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = holder.Rollback(context.Background()) }()
				var holderPID int
				if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
					t.Fatal(err)
				}
				if err := lockRecordIdempotencyScope(ctx, holder, input.Key); err != nil {
					t.Fatal(err)
				}
				workerPool := fixture.openDirectRuntimePool(t, ctx, "idempotency-scope-"+name, 1)
				var workerPID int
				if err := workerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
					t.Fatal(err)
				}
				worker := NewPostgresRecordPlatformRepository(workerPool, gate)
				result := make(chan error, 1)
				go func() {
					perform := func(transaction *RecordPlatformTransaction) error {
						switch operation {
						case "claim":
							var got recordplatform.IdempotencyClaimResultV1
							var err error
							if transaction == nil {
								got, err = worker.ClaimIdempotency(ctx, input)
							} else {
								got, err = transaction.ClaimIdempotency(ctx, input)
							}
							if err == nil && got.Owner == nil {
								return errors.New("claim returned no owner")
							}
							return err
						case "complete":
							if transaction == nil {
								return worker.CompleteIdempotency(ctx, input.Key, owner, fingerprint)
							}
							return transaction.CompleteIdempotency(ctx, input.Key, owner, fingerprint)
						case "renew":
							renewal := recordplatform.IdempotencyRenewInputV1{Key: input.Key, Owner: owner, OwnerLeaseDuration: 2 * time.Minute, RecordTTL: 5 * time.Minute}
							if transaction == nil {
								_, err := worker.RenewIdempotency(ctx, renewal)
								return err
							}
							_, err := transaction.RenewIdempotency(ctx, renewal)
							return err
						default:
							if transaction == nil {
								return worker.ReleaseIdempotency(ctx, input.Key, owner)
							}
							return transaction.ReleaseIdempotency(ctx, input.Key, owner)
						}
					}
					if !transactional {
						result <- perform(nil)
						return
					}
					result <- worker.RunRecordPlatformTransaction(ctx, func(_ context.Context, transaction *RecordPlatformTransaction) error { return perform(transaction) })
				}()
				waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
				if err := holder.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				if err := waitForRecordPlatformResult(t, result, name); err != nil {
					t.Fatalf("%s after scope release: %v", name, err)
				}
			})
		}
	}
}

func TestPostgresIntegrationIdempotencyRejectsUndeclaredLateScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	base := recordplatform.IdempotencyClaimInputV1{
		Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordUpdate, Key: "late-existing"},
		RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x41),
		OwnerID:            "late_scope_owner", OwnerLeaseDuration: time.Minute, RecordTTL: 5 * time.Minute,
	}
	claimed, err := queue.ClaimIdempotency(ctx, base)
	if err != nil || claimed.Owner == nil {
		t.Fatalf("seed existing owner: %#v %v", claimed, err)
	}
	for _, operation := range []string{"claim", "complete", "renew", "release"} {
		t.Run(operation, func(t *testing.T) {
			first := base
			first.Key.Key = "late-first-" + operation
			err := queue.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
				if _, err := transaction.ClaimIdempotency(ctx, first); err != nil {
					return err
				}
				switch operation {
				case "claim":
					_, err := transaction.ClaimIdempotency(ctx, base)
					return err
				case "complete":
					return transaction.CompleteIdempotency(ctx, base.Key, *claimed.Owner, base.RequestFingerprint)
				case "renew":
					_, err := transaction.RenewIdempotency(ctx, recordplatform.IdempotencyRenewInputV1{Key: base.Key, Owner: *claimed.Owner, OwnerLeaseDuration: time.Minute, RecordTTL: 5 * time.Minute})
					return err
				default:
					return transaction.ReleaseIdempotency(ctx, base.Key, *claimed.Owner)
				}
			})
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != "40001" {
				t.Fatalf("%s undeclared scope error = %v, want 40001", operation, err)
			}
			var firstRows int
			if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_idempotency_keys where project_id=$1 and operation_kind=$2 and idempotency_key=$3`, first.Key.ProjectID, first.Key.OperationKind, first.Key.Key).Scan(&firstRows); err != nil || firstRows != 0 {
				t.Fatalf("late scope transaction rollback rows=%d error=%v", firstRows, err)
			}
		})
	}
	parent := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-late-scope-parent")
	err = queue.runSourceAuthorizedTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		acquired, err := acquireEffectMutationResources(ctx, transaction.tx, []string{parent.RecordID}, nil, records.RevisionMutationResources{})
		if err != nil {
			return err
		}
		transaction.registerAcquiredRecordMutationResources(acquired)
		return transaction.CompleteIdempotency(ctx, base.Key, *claimed.Owner, base.RequestFingerprint)
	})
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "40001" {
		t.Fatalf("undeclared scope after record resources = %v, want 40001", err)
	}
}

func TestPostgresIntegrationIdempotencyCollisionDoesNotDeclareAnotherKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	var firstKey, secondKey string
	err := fixture.db.QueryRow(ctx, `with candidates as (
		select 'collision-' || value::text as key from generate_series(1,500000) as value
	), hashed as (
		select key, pg_catalog.hashtext(length($1::text)::text || ':' || $1::text ||
			length($2::text)::text || ':' || $2::text || length(key)::text || ':' || key) as hash from candidates
	)
	select min(key), max(key) from hashed group by hash having count(*)>1 order by hash limit 1`,
		string(recordplatform.ProjectIDDefault), string(recordplatform.OperationKindRecordUpdate)).Scan(&firstKey, &secondKey)
	if err != nil || firstKey == secondKey {
		t.Fatalf("find distinct physical hash collision: %q/%q %v", firstKey, secondKey, err)
	}
	input := recordplatform.IdempotencyClaimInputV1{
		Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordUpdate, Key: firstKey},
		RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x49),
		OwnerID:            "collision_owner", OwnerLeaseDuration: time.Minute, RecordTTL: 5 * time.Minute,
	}
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	err = queue.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if _, err := transaction.ClaimIdempotency(ctx, input); err != nil {
			return err
		}
		input.Key.Key = secondKey
		_, err := transaction.ClaimIdempotency(ctx, input)
		return err
	})
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "40001" {
		t.Fatalf("undeclared colliding full key = %v, want 40001", err)
	}
	var count int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_idempotency_keys where idempotency_key=any($1::text[])`, []string{firstKey, secondKey}).Scan(&count); err != nil || count != 0 {
		t.Fatalf("colliding key rollback count=%d error=%v", count, err)
	}
}

func TestPostgresIntegrationIdempotencyRejectsExpiredOwnerAfterScopeWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	input := recordplatform.IdempotencyClaimInputV1{
		Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordUpdate, Key: "scope-expiry"},
		RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x51),
		OwnerID:            "expiry_scope_owner", OwnerLeaseDuration: time.Minute, RecordTTL: 5 * time.Minute,
	}
	claim, err := queue.ClaimIdempotency(ctx, input)
	if err != nil || claim.Owner == nil {
		t.Fatalf("seed claim = %#v, %v", claim, err)
	}
	if err := fixture.db.QueryRow(ctx, `update public.record_idempotency_keys set owner_expires_at=clock_timestamp()+interval '2 seconds'
		where project_id=$1 and operation_kind=$2 and idempotency_key=$3 returning owner_expires_at`,
		input.Key.ProjectID, input.Key.OperationKind, input.Key.Key).Scan(&claim.Owner.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	var holderPID int
	if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	if err := lockRecordIdempotencyScope(ctx, holder, input.Key); err != nil {
		t.Fatal(err)
	}
	workerPool := fixture.openDirectRuntimePool(t, ctx, "idempotency-expiry-wait", 1)
	var workerPID int
	if err := workerPool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- NewPostgresRecordPlatformRepository(workerPool, gate).CompleteIdempotency(ctx, input.Key, *claim.Owner, input.RequestFingerprint)
	}()
	waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
	if _, err := fixture.db.Exec(ctx, `select pg_catalog.pg_sleep_until($1::timestamptz + interval '1 millisecond')`, claim.Owner.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := waitForRecordPlatformResult(t, result, "idempotency completion after expiry"); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("completion after scope wait = %v, want lost owner", err)
	}
	input.OwnerID = "takeover_scope_owner"
	takeover, err := queue.ClaimIdempotency(ctx, input)
	if err != nil || takeover.Owner == nil || takeover.Owner.Generation != claim.Owner.Generation+1 {
		t.Fatalf("takeover after expiry = %#v, %v", takeover, err)
	}
}

func TestPostgresIntegrationStaleIdempotencyCompletionRollsBackBusiness(t *testing.T) {
	ctx := context.Background()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	fixture.installAtomicBusinessFactTable(t, ctx)
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	input := recordplatform.IdempotencyClaimInputV1{
		Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordUpdate, Key: "stale-atomic"},
		RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x61),
		OwnerID:            "stale_atomic_owner", OwnerLeaseDuration: time.Minute, RecordTTL: 5 * time.Minute,
	}
	first, err := queue.ClaimIdempotency(ctx, input)
	if err != nil || first.Owner == nil {
		t.Fatalf("first claim = %#v, %v", first, err)
	}
	fixture.expireIdempotencyOwner(t, ctx, input.Key)
	input.OwnerID = "current_atomic_owner"
	current, err := queue.ClaimIdempotency(ctx, input)
	if err != nil || current.Owner == nil || current.Owner.Generation != first.Owner.Generation+1 {
		t.Fatalf("takeover = %#v, %v", current, err)
	}
	err = queue.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if _, err := recordPlatformTestExec(ctx, transaction, `insert into public.record_platform_test_business_facts_v1(fact_id) values('stale_atomic')`); err != nil {
			return err
		}
		return transaction.CompleteIdempotency(ctx, input.Key, *first.Owner, input.RequestFingerprint)
	})
	if !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("stale completion = %v", err)
	}
	var facts int
	if err := runtime.QueryRow(ctx, `select count(*) from public.record_platform_test_business_facts_v1 where fact_id='stale_atomic'`).Scan(&facts); err != nil || facts != 0 {
		t.Fatalf("stale completion business facts=%d error=%v", facts, err)
	}
	if err := queue.CompleteIdempotency(ctx, input.Key, *current.Owner, input.RequestFingerprint); err != nil {
		t.Fatalf("complete current owner: %v", err)
	}
}

func TestPostgresIntegrationRecordPlatformRenewalRejectsPreRenewExpiryTokens(t *testing.T) {
	ctx := context.Background()

	t.Run("idempotency completion", func(t *testing.T) {
		fixture := newRecordPlatformPostgresFixture(t, ctx)
		repository := NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-idempotency-renewal", 1), allowRecordPlatformAdmissionGate)
		input := recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: recordplatform.OperationKindRecordUpdate,
				Key:           "same-owner-renewal",
			},
			RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x91),
			OwnerID:            "renewal_owner",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          5 * time.Minute,
		}
		claimed, err := repository.ClaimIdempotency(ctx, input)
		if err != nil || claimed.Owner == nil {
			t.Fatalf("ClaimIdempotency() = (%#v, %v), want live owner", claimed, err)
		}

		renewed, err := repository.RenewIdempotency(ctx, recordplatform.IdempotencyRenewInputV1{
			Key:                input.Key,
			Owner:              *claimed.Owner,
			OwnerLeaseDuration: 2 * time.Minute,
			RecordTTL:          5 * time.Minute,
		})
		if err != nil {
			t.Fatalf("RenewIdempotency() error = %v", err)
		}
		if renewed.OwnerID != claimed.Owner.OwnerID || renewed.Generation != claimed.Owner.Generation || !renewed.ExpiresAt.After(claimed.Owner.ExpiresAt) {
			t.Fatalf("RenewIdempotency() owner = %#v, want same owner/generation with later expiry than %#v", renewed, *claimed.Owner)
		}

		if err := repository.CompleteIdempotency(ctx, input.Key, *claimed.Owner, recordPlatformIntegrationRequestFingerprint(t, input.Key.OperationKind, 0x92)); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("CompleteIdempotency() with pre-renew expiry error = %v, want ErrLostOwnerLease", err)
		}

		runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-idempotency-renewal-inspect", 1)
		var persistedOwnerID string
		var persistedGeneration int64
		var persistedExpiry time.Time
		var status string
		if err := runtimePool.QueryRow(ctx, `
			select owner_id, owner_generation, owner_expires_at, status
			from public.record_idempotency_keys
			where project_id = $1
			  and operation_kind = $2
			  and idempotency_key = $3
		`, string(input.Key.ProjectID), string(input.Key.OperationKind), input.Key.Key).Scan(&persistedOwnerID, &persistedGeneration, &persistedExpiry, &status); err != nil {
			t.Fatalf("read idempotency row after stale completion: %v", err)
		}
		if status != string(recordplatform.IdempotencyStatusInProgress) || persistedOwnerID != renewed.OwnerID || uint64(persistedGeneration) != renewed.Generation || !persistedExpiry.Equal(renewed.ExpiresAt) {
			t.Fatalf("idempotency row after stale completion = owner %q generation %d expiry %s status %q, want renewed owner %#v and in-progress status", persistedOwnerID, persistedGeneration, persistedExpiry, status, renewed)
		}

		if err := repository.CompleteIdempotency(ctx, input.Key, renewed, recordPlatformIntegrationRequestFingerprint(t, input.Key.OperationKind, 0x93)); err != nil {
			t.Fatalf("CompleteIdempotency() with renewed expiry: %v", err)
		}

		var completedOwnerID string
		var completedGeneration int64
		var ownerExpiryIsNull, hasResultFingerprint bool
		if err := runtimePool.QueryRow(ctx, `
			select owner_id,
			       owner_generation,
			       owner_expires_at is null,
			       coalesce(octet_length(result_fingerprint), 0) = 32
			from public.record_idempotency_keys
			where project_id = $1
			  and operation_kind = $2
			  and idempotency_key = $3
			  and status = 'completed'
		`, string(input.Key.ProjectID), string(input.Key.OperationKind), input.Key.Key).Scan(&completedOwnerID, &completedGeneration, &ownerExpiryIsNull, &hasResultFingerprint); err != nil {
			t.Fatalf("read completed idempotency row: %v", err)
		}
		if completedOwnerID != "" || completedGeneration != 0 || !ownerExpiryIsNull || !hasResultFingerprint {
			t.Fatalf("completed idempotency row = owner %q generation %d owner-expiry-null %t result-fingerprint %t, want cleared owner and persisted result", completedOwnerID, completedGeneration, ownerExpiryIsNull, hasResultFingerprint)
		}
	})

	t.Run("object content release", func(t *testing.T) {
		fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
		created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "lease-renewal-parent")
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}
		repository := NewPostgresRecordPlatformRepository(runtime, gate)
		claimed, err := repository.AcquireObjectContentLease(ctx, object, recordplatform.LeaseClaimInputV1{
			OwnerID:       "renewal_owner",
			LeaseDuration: time.Minute,
		})
		if err != nil {
			t.Fatalf("AcquireObjectContentLease() error = %v", err)
		}

		renewed, err := repository.RenewObjectContentLease(ctx, object, claimed.Owner, 2*time.Minute)
		if err != nil {
			t.Fatalf("RenewObjectContentLease() error = %v", err)
		}
		if renewed.Owner.OwnerID != claimed.Owner.OwnerID || renewed.Owner.Generation != claimed.Owner.Generation || !renewed.Owner.ExpiresAt.After(claimed.Owner.ExpiresAt) {
			t.Fatalf("RenewObjectContentLease() owner = %#v, want same owner/generation with later expiry than %#v", renewed.Owner, claimed.Owner)
		}

		if err := repository.ReleaseObjectContentLease(ctx, object, claimed.Owner); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("ReleaseObjectContentLease() with pre-renew expiry error = %v, want ErrLostOwnerLease", err)
		}

		runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-object-renewal-inspect", 1)
		var persistedOwnerID string
		var persistedGeneration int64
		var persistedExpiry time.Time
		if err := runtimePool.QueryRow(ctx, `
			select owner_id, owner_generation, expires_at
			from public.object_content_leases
			where project_id = $1
			  and object_kind = $2
			  and object_id = $3
		`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&persistedOwnerID, &persistedGeneration, &persistedExpiry); err != nil {
			t.Fatalf("read object-content lease after stale release: %v", err)
		}
		if persistedOwnerID != renewed.Owner.OwnerID || uint64(persistedGeneration) != renewed.Owner.Generation || !persistedExpiry.Equal(renewed.Owner.ExpiresAt) {
			t.Fatalf("object-content lease after stale release = owner %q generation %d expiry %s, want renewed owner %#v", persistedOwnerID, persistedGeneration, persistedExpiry, renewed.Owner)
		}

		if err := repository.ReleaseObjectContentLease(ctx, object, renewed.Owner); err != nil {
			t.Fatalf("ReleaseObjectContentLease() with renewed expiry: %v", err)
		}

		var leaseIsLive bool
		if err := runtimePool.QueryRow(ctx, `
			select expires_at > transaction_timestamp()
			from public.object_content_leases
			where project_id = $1
			  and object_kind = $2
			  and object_id = $3
		`, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&leaseIsLive); err != nil {
			t.Fatalf("read object-content lease after current release: %v", err)
		}
		if leaseIsLive {
			t.Fatal("object-content lease remains live after release with renewed expiry")
		}
	})
}

func TestPostgresIntegrationRecordPlatformReleaseAndCleanupRetainGenerationFences(t *testing.T) {
	ctx := context.Background()

	t.Run("object-content release", func(t *testing.T) {
		_, gate, runtime := newAdmittedOutboxFixture(t, ctx)
		created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "lease-aba-parent")
		object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}
		repository := NewPostgresRecordPlatformRepository(runtime, gate)
		first, err := repository.AcquireObjectContentLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "aba_owner", LeaseDuration: time.Minute})
		if err != nil || first.Owner.Generation != 1 {
			t.Fatalf("first AcquireObjectContentLease() = (%#v, %v), want generation-one owner", first, err)
		}
		if err := repository.ReleaseObjectContentLease(ctx, object, first.Owner); err != nil {
			t.Fatalf("ReleaseObjectContentLease() first owner: %v", err)
		}
		second, err := repository.AcquireObjectContentLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "aba_owner", LeaseDuration: time.Minute})
		if err != nil || second.Owner.Generation != 2 {
			t.Fatalf("reacquire after release = (%#v, %v), want generation-two owner", second, err)
		}
		if _, err := repository.RenewObjectContentLease(ctx, object, first.Owner, time.Minute); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("RenewObjectContentLease() old owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.ReleaseObjectContentLease(ctx, object, first.Owner); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("ReleaseObjectContentLease() old owner error = %v, want ErrLostOwnerLease", err)
		}
	})

	t.Run("idempotency cleanup", func(t *testing.T) {
		fixture := newRecordPlatformPostgresFixture(t, ctx)
		repository := NewPostgresRecordPlatformRepository(fixture.openDirectRuntimePool(t, ctx, "record-platform-aba-idempotency", 1), allowRecordPlatformAdmissionGate)
		input := recordplatform.IdempotencyClaimInputV1{
			Key: recordplatform.IdempotencyKey{
				ProjectID:     recordplatform.ProjectIDDefault,
				OperationKind: recordplatform.OperationKindRecordUpdate,
				Key:           "aba-cleanup",
			},
			RequestFingerprint: recordPlatformIntegrationRequestFingerprint(t, recordplatform.OperationKindRecordUpdate, 0x61),
			OwnerID:            "aba_owner",
			OwnerLeaseDuration: time.Minute,
			RecordTTL:          5 * time.Minute,
		}
		first, err := repository.ClaimIdempotency(ctx, input)
		if err != nil || first.Owner == nil || first.Owner.Generation != 1 {
			t.Fatalf("first ClaimIdempotency() = (%#v, %v), want generation-one owner", first, err)
		}
		if err := repository.ReleaseIdempotency(ctx, input.Key, *first.Owner); err != nil {
			t.Fatalf("ReleaseIdempotency() first owner: %v", err)
		}
		fixture.expireIdempotencyRow(t, ctx, input.Key)
		if _, err := repository.CleanupExpiredRecordPlatformPrimitives(ctx); err != nil {
			t.Fatalf("CleanupExpiredRecordPlatformPrimitives() error = %v", err)
		}
		second, err := repository.ClaimIdempotency(ctx, input)
		if err != nil || second.Owner == nil || second.Owner.Generation != 2 {
			t.Fatalf("reclaim after cleanup = (%#v, %v), want generation-two owner", second, err)
		}
		renew := recordplatform.IdempotencyRenewInputV1{Key: input.Key, Owner: *first.Owner, OwnerLeaseDuration: time.Minute, RecordTTL: 5 * time.Minute}
		if _, err := repository.RenewIdempotency(ctx, renew); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("RenewIdempotency() old owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.ReleaseIdempotency(ctx, input.Key, *first.Owner); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("ReleaseIdempotency() old owner error = %v, want ErrLostOwnerLease", err)
		}
		if err := repository.CompleteIdempotency(ctx, input.Key, *first.Owner, recordPlatformIntegrationRequestFingerprint(t, input.Key.OperationKind, 0x62)); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
			t.Fatalf("CompleteIdempotency() old owner error = %v, want ErrLostOwnerLease", err)
		}
	})
}

func TestPostgresIntegrationRecordPlatformServingLeaseUsesDatabaseState(t *testing.T) {
	ctx := context.Background()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	repository := NewPostgresRecordPlatformRepository(runtime, gate)
	created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "serving-lease-parent")
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}

	serving, err := repository.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{OwnerID: "serving_owner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("AcquireServingLease() error = %v", err)
	}
	if serving.CapturedEpoch != 0 {
		t.Fatalf("AcquireServingLease() captured epoch = %d, want 0", serving.CapturedEpoch)
	}
	if err := repository.AssertServingLease(ctx, serving); err != nil {
		t.Fatalf("AssertServingLease() live database-backed permit: %v", err)
	}

	fabricated := serving
	fabricated.CapturedEpoch++
	if err := repository.AssertServingLease(ctx, fabricated); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("AssertServingLease() fabricated epoch error = %v, want ErrLostOwnerLease", err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch = delivery_epoch + 1 where project_id=$1 and object_kind=$2 and object_id=$3`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("advance content delivery epoch: %v", err)
	}
	if err := repository.AssertServingLease(ctx, serving); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("AssertServingLease() advanced epoch error = %v, want ErrLostOwnerLease", err)
	}

	secondCreated := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "serving-fence-parent")
	secondObject := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: secondCreated.RecordID}
	secondServing, err := repository.AcquireServingLease(ctx, secondObject, recordplatform.LeaseClaimInputV1{OwnerID: "serving_owner", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatalf("AcquireServingLease() second object: %v", err)
	}
	seedServingTestFence(t, ctx, fixture, secondObject, "clock_timestamp() + interval '5 minutes'")
	if err := repository.AssertServingLease(ctx, secondServing); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("AssertServingLease() live deletion fence error = %v, want ErrLostOwnerLease", err)
	}
}

func TestPostgresIntegrationRecordPlatformReservationFenceCancelsServingRenewal(t *testing.T) {
	ctx := context.Background()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "serving-drain-parent")
	object := recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: created.RecordID}
	repository := NewPostgresRecordPlatformRepository(runtime, gate)
	serving, err := repository.AcquireServingLease(ctx, object, recordplatform.LeaseClaimInputV1{
		OwnerID:       "serving_owner",
		LeaseDuration: time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireServingLease() error = %v", err)
	}
	seedServingTestFence(t, ctx, fixture, object, "clock_timestamp() + interval '5 minutes'")
	if _, err := repository.RenewServingLease(ctx, serving, time.Second); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("RenewServingLease() after reservation fence error = %v, want ErrLostOwnerLease", err)
	}
	if err := repository.AssertServingLease(ctx, serving); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("AssertServingLease() after reservation fence error = %v, want ErrLostOwnerLease", err)
	}
	if err := repository.ReleaseObjectContentLease(ctx, serving.Object, serving.Owner); err != nil {
		t.Fatalf("ReleaseObjectContentLease() drained serving owner error = %v", err)
	}
}

var allowRecordPlatformAdmissionGate = AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil })

type recordPlatformPostgresFixture struct {
	db             *pgxpool.Pool
	databaseName   string
	bootstrapOwner string
	runtime        string
	admin          string
	deletionWorker string
	migrator       string
	passwords      map[string]string
	admissionGate  AdmissionGate
}

// Use the production signed activation and heartbeat with a direct runtime
// role, so outbox tests exercise admission as well as the record route.
func newAdmittedOutboxFixture(t *testing.T, ctx context.Context) (recordPlatformPostgresFixture, AdmissionGate, *pgxpool.Pool) {
	t.Helper()
	importAttachmentAuthorityMu.Lock()
	t.Cleanup(importAttachmentAuthorityMu.Unlock)
	fixture := newRecordPlatformPostgresBaseFixture(t, ctx)
	stateRoot := filepath.Join(t.TempDir(), "outbox-authority")
	state, err := recordauthority.CreateComposeState(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	const authorityRole = "houfeng_records_authority"
	var exists bool
	if err := fixture.db.QueryRow(ctx, `select pg_catalog.to_regrole($1) is not null`, authorityRole).Scan(&exists); err != nil || exists {
		t.Fatalf("outbox fixture requires absent authority role: exists=%t error=%v", exists, err)
	}
	if err := platformmigrate.ProvisionComposeBootstrap(ctx, fixture.db, platformmigrate.ComposeBootstrapConfig{
		DatabaseName: fixture.databaseName, BootstrapRole: fixture.bootstrapOwner, AuthorityRole: authorityRole,
		Roles:     platformmigrate.AppRoleSetV2{CenterRuntime: fixture.runtime, PlatformAdmin: fixture.admin, DeletionWorker: fixture.deletionWorker, Migrator: fixture.migrator},
		Passwords: platformmigrate.ComposeRolePasswords{Runtime: fixture.passwords[fixture.runtime], PlatformAdmin: fixture.passwords[fixture.admin], DeletionWorker: fixture.passwords[fixture.deletionWorker], Migrator: fixture.passwords[fixture.migrator], Authority: state.DatabasePassword()},
	}); err != nil {
		t.Fatal(err)
	}
	fixture.passwords[authorityRole] = state.DatabasePassword()
	fixture.dropRole(t, authorityRole)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator, "outbox-admission-migrator", 1)
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	activation, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var receipt []byte
	if err := migrator.QueryRow(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, activation).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if err := recordauthority.PersistActivationReceipt(stateRoot, state, receipt); err != nil {
		t.Fatal(err)
	}
	authority := fixture.openDirectRolePool(t, ctx, authorityRole, "outbox-admission-heartbeat", 1)
	heartbeat, wantExpiry, err := recordauthority.MarshalMembershipHeartbeatCommandV1(state, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	if err := authority.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, heartbeat).Scan(&expiry); err != nil || !expiry.Equal(wantExpiry) {
		t.Fatalf("outbox heartbeat expiry=%v want=%v error=%v", expiry, wantExpiry, err)
	}
	runtime := fixture.openDirectRuntimePool(t, ctx, "outbox-admitted-runtime", 4)
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatal(err)
	}
	gate, err := NewDeploymentMembershipAdmissionGate(DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: state.DeploymentID, ProjectID: recordplatform.ProjectIDDefault,
		InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedPostgresCollaborationSource(t, ctx, fixture)
	return fixture, gate, runtime
}

// newAdmittedCoreRecordFixture retains the signed deployment gate for
// independent runtime pools opened by record integration tests.
func newAdmittedCoreRecordFixture(t *testing.T, ctx context.Context) recordPlatformPostgresFixture {
	t.Helper()
	fixture, gate, _ := newAdmittedOutboxFixture(t, ctx)
	fixture.admissionGate = gate
	return fixture
}

func seedAdmittedOutboxRecord(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, gate AdmissionGate, key string) records.RevisionCommitResult {
	t.Helper()
	repository, err := NewPostgresRecordRepository(runtime, gate, nil, newRecordsPostgresAuthorization(t, runtime, gate), nil)
	if err != nil {
		t.Fatal(err)
	}
	command := revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtime, Gate: gate},
		recordsPostgresCompleteRevisionInput(t, "Outbox owner"), key)
	result, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("create outbox record: %v", err)
	}
	return result
}

func newRecordPlatformPostgresFixture(t *testing.T, ctx context.Context) recordPlatformPostgresFixture {
	t.Helper()
	fixture := newRecordPlatformPostgresBaseFixture(t, ctx)
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "record-platform-migrator", 1)
	if _, err := storemigrate.ConvergeAppACLR1(ctx, migratorPool, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() for record platform integration: %v", err)
	}
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-runtime-admission", 1)
	if err := storemigrate.AdmitAppACLRuntime(ctx, runtimePool); err != nil {
		t.Fatalf("AdmitAppACLRuntime() for direct runtime integration role: %v", err)
	}
	return fixture
}

func newRecordPlatformPostgresBaseFixture(t *testing.T, ctx context.Context) recordPlatformPostgresFixture {
	t.Helper()
	db := openRecordPlatformTemporaryPostgresDatabase(t, ctx)
	fixture := recordPlatformPostgresFixture{
		db:        db,
		passwords: make(map[string]string, 4),
	}
	if err := db.QueryRow(ctx, `select pg_catalog.current_database(), current_user`).Scan(&fixture.databaseName, &fixture.bootstrapOwner); err != nil {
		t.Fatalf("read record platform integration database identity: %v", err)
	}
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
	fixture.runtime = "houfeng_delivery_runtime_" + suffix
	fixture.admin = "houfeng_delivery_admin_" + suffix
	fixture.deletionWorker = "houfeng_deletion_worker_" + suffix
	fixture.migrator = "houfeng_delivery_migrator_" + suffix
	for _, role := range []string{fixture.runtime, fixture.admin, fixture.deletionWorker, fixture.migrator} {
		password := recordPlatformIntegrationPassword(t)
		fixture.passwords[role] = password
		if _, err := db.Exec(ctx, `create role `+pgx.Identifier{role}.Sanitize()+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+password+`'`); err != nil {
			t.Fatalf("create direct record platform role %q: %v", role, err)
		}
		fixture.dropRole(t, role)
	}

	quotedDatabase := pgx.Identifier{fixture.databaseName}.Sanitize()
	quotedMigrator := pgx.Identifier{fixture.migrator}.Sanitize()
	quotedBootstrapOwner := pgx.Identifier{fixture.bootstrapOwner}.Sanitize()
	if _, err := db.Exec(ctx, `alter database `+quotedDatabase+` owner to `+quotedMigrator); err != nil {
		t.Fatalf("assign temporary database to direct migrator: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.Exec(cleanupCtx, `alter database `+quotedDatabase+` owner to `+quotedBootstrapOwner); err != nil {
			t.Errorf("restore temporary database owner %q: %v", fixture.bootstrapOwner, err)
		}
	})

	return fixture
}

func (fixture recordPlatformPostgresFixture) installFenceObjectLeaseRaceTriggers(t *testing.T, ctx context.Context) {
	t.Helper()
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "record-platform-race-trigger", 1)
	for _, definition := range []string{
		`create function public.record_platform_test_block_epoch_update_v1()
			returns trigger
			language plpgsql
			as $$
			begin
				perform pg_catalog.pg_advisory_xact_lock(` + fmt.Sprint(recordPlatformFenceEpochBlockLock) + `);
				return new;
			end;
			$$`,
		`create function public.record_platform_test_block_object_claim_v1()
			returns trigger
			language plpgsql
			as $$
			begin
				perform pg_catalog.pg_advisory_xact_lock(` + fmt.Sprint(recordPlatformObjectClaimBlockLock) + `);
				return new;
			end;
			$$`,
		`create trigger record_platform_test_block_epoch_update_v1
			before update on public.content_delivery_epochs
			for each row execute function public.record_platform_test_block_epoch_update_v1()`,
		`create trigger record_platform_test_block_object_claim_v1
			before insert or update on public.object_content_leases
			for each row execute function public.record_platform_test_block_object_claim_v1()`,
		`grant execute on function public.record_platform_test_block_epoch_update_v1() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
		`grant execute on function public.record_platform_test_block_object_claim_v1() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	} {
		if _, err := migratorPool.Exec(ctx, definition); err != nil {
			t.Fatalf("install record platform race trigger: %v", err)
		}
	}
}

func (fixture recordPlatformPostgresFixture) installConcurrentIdempotencyClaimBlocker(t *testing.T, ctx context.Context) {
	t.Helper()
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "record-platform-idempotency-trigger", 1)
	for _, definition := range []string{
		`create function public.record_platform_test_block_idempotency_claim_v1()
			returns trigger
			language plpgsql
			as $$
			begin
				perform pg_catalog.pg_advisory_xact_lock(` + fmt.Sprint(recordPlatformIdempotencyClaimLock) + `);
				return new;
			end;
			$$`,
		`create trigger record_platform_test_block_idempotency_claim_v1
			before insert on public.record_idempotency_keys
			for each row execute function public.record_platform_test_block_idempotency_claim_v1()`,
		`grant execute on function public.record_platform_test_block_idempotency_claim_v1() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	} {
		if _, err := migratorPool.Exec(ctx, definition); err != nil {
			t.Fatalf("install concurrent idempotency claim blocker: %v", err)
		}
	}
}

func (fixture recordPlatformPostgresFixture) installAtomicBusinessFactTable(t *testing.T, ctx context.Context) {
	t.Helper()
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "record-platform-atomic-fixture", 1)
	for _, definition := range []string{
		`create table public.record_platform_test_business_facts_v1 (fact_id text primary key)`,
		`grant select, insert on table public.record_platform_test_business_facts_v1 to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	} {
		if _, err := migratorPool.Exec(ctx, definition); err != nil {
			t.Fatalf("install record platform atomic business fixture: %v", err)
		}
	}
}

func (fixture recordPlatformPostgresFixture) seedFenceObjectLeaseRace(t *testing.T, ctx context.Context, object recordplatform.ObjectRef) {
	t.Helper()
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-race-seed", 1)
	if _, err := runtimePool.Exec(ctx, `
		insert into public.content_delivery_epochs (project_id, object_kind, object_id, delivery_epoch)
		values ($1, $2, $3, 0)
	`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("seed content delivery epoch: %v", err)
	}
	if _, err := runtimePool.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint, state, expires_at
		) values (
			'drs_fencerace', $1, $2, $3,
			decode(repeat('11', 32), 'hex'), decode(repeat('22', 32), 'hex'), 'previewed',
			transaction_timestamp() + interval '5 minutes'
		)
	`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("seed previewed deletion reservation: %v", err)
	}
	if _, err := runtimePool.Exec(ctx, `
		insert into public.object_content_leases (
			project_id, object_kind, object_id, owner_id, owner_generation, created_at, expires_at
		) values (
			$1, $2, $3, 'expired_content_owner', 1,
			transaction_timestamp() - interval '2 seconds', transaction_timestamp() - interval '1 second'
		)
	`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("seed expired object content lease: %v", err)
	}
}

func (fixture recordPlatformPostgresFixture) seedContentDeliveryEpoch(t *testing.T, ctx context.Context, object recordplatform.ObjectRef, epoch int64) {
	t.Helper()
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-epoch-seed", 1)
	if _, err := runtimePool.Exec(ctx, `
		insert into public.content_delivery_epochs (project_id, object_kind, object_id, delivery_epoch)
		values ($1, $2, $3, $4)
	`, object.ProjectID, object.ObjectKind, object.ObjectID, epoch); err != nil {
		t.Fatalf("seed content delivery epoch: %v", err)
	}
}

func (fixture recordPlatformPostgresFixture) advanceContentDeliveryEpoch(t *testing.T, ctx context.Context, object recordplatform.ObjectRef) {
	t.Helper()
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-epoch-advance", 1)
	if _, err := runtimePool.Exec(ctx, `
		update public.content_delivery_epochs
		set delivery_epoch = delivery_epoch + 1,
		    updated_at = transaction_timestamp()
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
	`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("advance content delivery epoch: %v", err)
	}
}

func (fixture recordPlatformPostgresFixture) expireIdempotencyOwner(t *testing.T, ctx context.Context, key recordplatform.IdempotencyKey) {
	t.Helper()
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-idempotency-expire", 1)
	if _, err := runtimePool.Exec(ctx, `
		update public.record_idempotency_keys
		set owner_expires_at = transaction_timestamp() - interval '1 second'
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
	`, string(key.ProjectID), string(key.OperationKind), key.Key); err != nil {
		t.Fatalf("expire idempotency owner: %v", err)
	}
}

func (fixture recordPlatformPostgresFixture) expireIdempotencyRow(t *testing.T, ctx context.Context, key recordplatform.IdempotencyKey) {
	t.Helper()
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-idempotency-row-expire", 1)
	if _, err := runtimePool.Exec(ctx, `
		update public.record_idempotency_keys
		set created_at = transaction_timestamp() - interval '2 seconds',
		    owner_expires_at = transaction_timestamp() - interval '1500 milliseconds',
		    expires_at = transaction_timestamp() - interval '1 second'
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
	`, string(key.ProjectID), string(key.OperationKind), key.Key); err != nil {
		t.Fatalf("expire idempotency row: %v", err)
	}
}

func (fixture recordPlatformPostgresFixture) expireOutboxOwner(t *testing.T, ctx context.Context, rowID int64) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		update public.record_outbox
		set owner_expires_at = transaction_timestamp() - interval '1 second'
		where outbox_row_id = $1
	`, rowID); err != nil {
		t.Fatalf("expire outbox owner: %v", err)
	}
}

func (fixture recordPlatformPostgresFixture) expireObjectContentLease(t *testing.T, ctx context.Context, object recordplatform.ObjectRef) {
	t.Helper()
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-platform-lease-expire", 1)
	if _, err := runtimePool.Exec(ctx, `
		update public.object_content_leases
		set created_at = transaction_timestamp() - interval '2 seconds',
		    expires_at = transaction_timestamp() - interval '1 second'
		where project_id = $1
		  and object_kind = $2
		  and object_id = $3
	`, object.ProjectID, object.ObjectKind, object.ObjectID); err != nil {
		t.Fatalf("expire object content lease: %v", err)
	}
}

func (fixture recordPlatformPostgresFixture) openDirectRuntimeConnection(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	config := fixture.directRoleConfig(t, fixture.runtime, "record-platform-race-blocker")
	connection, err := pgx.ConnectConfig(ctx, config.ConnConfig)
	if err != nil {
		t.Fatalf("open direct runtime blocker connection: %v", err)
	}
	return connection
}

func (fixture recordPlatformPostgresFixture) openDirectRuntimePool(t *testing.T, ctx context.Context, applicationName string, maxConns int32) *pgxpool.Pool {
	t.Helper()
	return fixture.openDirectRolePool(t, ctx, fixture.runtime, applicationName, maxConns)
}

func (fixture recordPlatformPostgresFixture) openDirectRolePool(t *testing.T, ctx context.Context, role, applicationName string, maxConns int32) *pgxpool.Pool {
	t.Helper()
	config := fixture.directRoleConfig(t, role, applicationName)
	config.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open direct role pool %q: %v", role, err)
	}
	t.Cleanup(pool.Close)
	var sessionUser, currentUser string
	if err := pool.QueryRow(ctx, `select session_user, current_user`).Scan(&sessionUser, &currentUser); err != nil {
		t.Fatalf("read direct role %q identities: %v", role, err)
	}
	if sessionUser != role || currentUser != role {
		t.Fatalf("direct role %q identities = (%q, %q), want (%q, %q)", role, sessionUser, currentUser, role, role)
	}
	return pool
}

func (fixture recordPlatformPostgresFixture) directRoleConfig(t *testing.T, role, applicationName string) *pgxpool.Config {
	t.Helper()
	password, ok := fixture.passwords[role]
	if !ok {
		t.Fatalf("no direct password for role %q", role)
	}
	config := fixture.db.Config().Copy()
	config.ConnConfig.User = role
	config.ConnConfig.Password = password
	config.AfterConnect = nil
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	} else {
		copied := make(map[string]string, len(config.ConnConfig.RuntimeParams)+1)
		for key, value := range config.ConnConfig.RuntimeParams {
			copied[key] = value
		}
		config.ConnConfig.RuntimeParams = copied
	}
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	return config
}

func (fixture recordPlatformPostgresFixture) dropRole(t *testing.T, role string) {
	t.Helper()
	quotedRole := pgx.Identifier{role}.Sanitize()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if role == fixture.migrator {
			if _, err := fixture.db.Exec(cleanupCtx, `reassign owned by `+quotedRole+` to `+pgx.Identifier{fixture.bootstrapOwner}.Sanitize()); err != nil {
				t.Errorf("reassign temporary migrator %q ownership: %v", role, err)
			}
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop owned by `+quotedRole); err != nil {
			t.Errorf("drop owned by temporary role %q: %v", role, err)
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop role if exists `+quotedRole); err != nil {
			t.Errorf("drop temporary role %q: %v", role, err)
		}
	})
}

func openRecordPlatformTemporaryPostgresDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for record platform PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for record platform PostgreSQL integration tests")
	}
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open PostgreSQL integration admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	databaseName := fmt.Sprintf("houfeng_delivery_%d_%d", time.Now().UnixNano(), os.Getpid())
	if _, err := adminPool.Exec(ctx, `create database `+pgx.Identifier{databaseName}.Sanitize()); err != nil {
		t.Fatalf("create temporary record platform database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, `drop database if exists `+pgx.Identifier{databaseName}.Sanitize()+` with (force)`); err != nil {
			t.Errorf("drop temporary record platform database %q: %v", databaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary record platform database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary record platform database %q: %v", databaseName, err)
	}
	return testPool
}

func recordPlatformIntegrationRequestFingerprint(t *testing.T, operationKind recordplatform.OperationKind, payloadDigest byte) recordplatform.RequestFingerprintV1 {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      operationKind,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   recordPlatformIntegrationDigest(0xa1),
		RequestScopeDigest: recordPlatformIntegrationDigest(0xb2),
		PayloadDigest:      recordPlatformIntegrationDigest(payloadDigest),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return fingerprint
}

func recordPlatformIntegrationDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

func recordPlatformIntegrationPassword(t *testing.T) string {
	t.Helper()
	var entropy [24]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		t.Fatalf("read random record platform test password: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(entropy[:])
}

func waitForRecordPlatformBackendLock(t *testing.T, ctx context.Context, db *pgxpool.Pool, applicationName string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waitEventType string
		err := db.QueryRow(ctx, `
			select coalesce(wait_event_type, '')
			from pg_catalog.pg_stat_activity
			where application_name = $1
			order by backend_start desc
			limit 1
		`, applicationName).Scan(&waitEventType)
		if err != nil {
			t.Fatalf("observe record platform backend %q: %v", applicationName, err)
		}
		if waitEventType == "Lock" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend %q did not reach a PostgreSQL lock wait; last wait event type %q", applicationName, waitEventType)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForRecordPlatformResult[T any](t *testing.T, result <-chan T, operation string) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
		var zero T
		return zero
	}
}
