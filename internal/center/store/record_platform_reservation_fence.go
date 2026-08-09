package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

const lockDeletionReservationForFenceSQL = `
	select state,
	       project_id,
	       object_kind,
	       object_id,
	       owner_generation,
	       owner_expires_at,
	       expires_at
	from public.deletion_reservations
	where reservation_id = $1
	  and state = 'previewed'
	  and expires_at > transaction_timestamp()
	for update`

const lockContentDeliveryEpochForFenceSQL = `
	select delivery_epoch
	from public.content_delivery_epochs
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	for update`

const lockDeletionFenceLeaseForFenceSQL = `
	select owner_id,
	       owner_generation,
	       expires_at,
	       expires_at > transaction_timestamp()
	from public.deletion_fence_leases
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	for update`

const lockObjectContentLeaseForFenceSQL = `
	select owner_id,
	       owner_generation,
	       expires_at,
	       expires_at > transaction_timestamp()
	from public.object_content_leases
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	for update`

const incrementContentDeliveryEpochForFenceSQL = `
	update public.content_delivery_epochs
	set delivery_epoch = delivery_epoch + 1,
	    updated_at = transaction_timestamp()
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	returning delivery_epoch`

const fenceDeletionReservationSQL = `
	update public.deletion_reservations
	set state = 'fenced',
	    fence_epoch = $2,
	    owner_id = $3,
	    owner_generation = $4,
	    owner_expires_at = transaction_timestamp() + ($5 * interval '1 microsecond')
	where reservation_id = $1
	  and state = 'previewed'
	  and expires_at > transaction_timestamp()
	returning owner_id, owner_generation, owner_expires_at`

const fenceDeletionFenceLeaseSQL = `
	insert into public.deletion_fence_leases (
		project_id, object_kind, object_id, owner_id, owner_generation, expires_at
	) values (
		$1, $2, $3, $4, $5,
		transaction_timestamp() + ($6 * interval '1 microsecond')
	)
	on conflict (project_id, object_kind, object_id) do update
	set owner_id = excluded.owner_id,
		owner_generation = excluded.owner_generation,
		expires_at = transaction_timestamp() + ($6 * interval '1 microsecond')
	where deletion_fence_leases.expires_at <= transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const renewDeletionReservationFenceSQL = `
	update public.deletion_reservations
	set owner_expires_at = transaction_timestamp() + ($5 * interval '1 microsecond')
	where reservation_id = $1
	  and state = 'fenced'
	  and owner_id = $2
	  and owner_generation = $3
	  and fence_epoch = $4
	  and owner_expires_at = $6
	  and owner_expires_at > transaction_timestamp()
	returning owner_id, owner_generation, owner_expires_at`

const renewDeletionFenceLeaseForReservationSQL = `
	update public.deletion_fence_leases
	set expires_at = transaction_timestamp() + ($6 * interval '1 microsecond')
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and expires_at = $7
	  and expires_at > transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const releaseDeletionReservationFenceSQL = `
	update public.deletion_reservations
	set state = 'released',
	    owner_id = '',
	    owner_generation = 0,
	    owner_expires_at = null
	where reservation_id = $1
	  and state = 'fenced'
	  and owner_id = $2
	  and owner_generation = $3
	  and fence_epoch = $4
	  and owner_expires_at = $5
	  and owner_expires_at > transaction_timestamp()`

const releaseDeletionFenceLeaseForReservationSQL = `
	update public.deletion_fence_leases
	set expires_at = transaction_timestamp()
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and expires_at = $6
	  and expires_at > transaction_timestamp()`

const assertDeletionReservationFenceSQL = `
	select 1
	from public.deletion_reservations as reservation
	join public.content_delivery_epochs as epoch
	  on epoch.project_id = reservation.project_id
	 and epoch.object_kind = reservation.object_kind
	 and epoch.object_id = reservation.object_id
	join public.deletion_fence_leases as lease
	  on lease.project_id = reservation.project_id
	 and lease.object_kind = reservation.object_kind
	 and lease.object_id = reservation.object_id
	where reservation.reservation_id = $1
	  and reservation.project_id = $2
	  and reservation.object_kind = $3
	  and reservation.object_id = $4
	  and reservation.state = 'fenced'
	  and reservation.owner_id = $5
	  and reservation.owner_generation = $6
	  and reservation.fence_epoch = $7
	  and reservation.owner_expires_at = $8
	  and reservation.owner_expires_at > transaction_timestamp()
	  and epoch.delivery_epoch = reservation.fence_epoch
	  and lease.owner_id = $5
	  and lease.owner_generation = $6
	  and lease.expires_at = $8
	  and lease.expires_at > transaction_timestamp()`

// FenceDeletionReservation atomically advances an existing delivery epoch and
// binds the preview reservation and deletion-fence lease to one new owner
// generation. It never creates a missing epoch or decides deletion outcomes.
func (repository *PostgresRecordPlatformRepository) FenceDeletionReservation(ctx context.Context, input recordplatform.ReservationFenceInputV1) (recordplatform.DeletionReservationFenceV1, error) {
	if err := input.Validate(); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	fence, err := repository.fenceDeletionReservationInTransaction(ctx, tx, input)
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("commit deletion reservation fence transaction: %w", err)
	}
	return fence, nil
}

// FenceDeletionReservation advances the compound fence inside an already-open
// admitted business transaction. Callers can atomically bind the fence to
// their own durable operation without exposing arbitrary SQL.
func (transaction *RecordPlatformTransaction) FenceDeletionReservation(
	ctx context.Context,
	input recordplatform.ReservationFenceInputV1,
) (recordplatform.DeletionReservationFenceV1, error) {
	if transaction == nil || transaction.repository == nil || transaction.tx == nil {
		return recordplatform.DeletionReservationFenceV1{}, recordplatform.ErrInvalidReservationFence
	}
	if err := input.Validate(); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	if err := transaction.repository.admit(ctx, transaction.tx); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	return transaction.repository.fenceDeletionReservationInTransaction(ctx, transaction.tx, input)
}

func (repository *PostgresRecordPlatformRepository) fenceDeletionReservationInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	input recordplatform.ReservationFenceInputV1,
) (recordplatform.DeletionReservationFenceV1, error) {
	reservation, err := lockPreviewedDeletionReservationForFence(ctx, tx, input.ReservationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.DeletionReservationFenceV1{}, recordplatform.ErrDeletionReservationUnavailable
	}
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("lock deletion reservation for fence: %w", err)
	}
	if reservation.object != input.Object {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("%w: reservation object", recordplatform.ErrInvalidReservationFence)
	}

	currentEpoch, err := lockContentDeliveryEpochForFence(ctx, tx, input.Object)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.DeletionReservationFenceV1{}, recordplatform.ErrContentDeliveryEpochMissing
	}
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("lock content delivery epoch for fence: %w", err)
	}

	deletionFence, err := lockDeletionFenceLeaseForFence(ctx, tx, input.Object)
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("lock deletion fence lease for fence: %w", err)
	}
	if deletionFence.live {
		return recordplatform.DeletionReservationFenceV1{}, recordplatform.ErrDeletionFenceLeaseLive
	}
	objectLease, err := lockObjectContentLeaseForFence(ctx, tx, input.Object)
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("lock object content lease for fence: %w", err)
	}
	_ = objectLease // The committed fence cancels renewal; the worker drains this exact lease before ledger append.

	generation, err := nextReservationFenceGeneration(reservation.ownerGeneration, deletionFence.generation)
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	newEpoch, err := incrementContentDeliveryEpochForFence(ctx, tx, input.Object)
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("increment content delivery epoch for fence: %w", err)
	}
	if newEpoch != currentEpoch+1 {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("%w: nonmonotonic delivery epoch", recordplatform.ErrInvalidReservationFence)
	}

	owner, err := scanLiveOwnerLease(tx.QueryRow(ctx, fenceDeletionReservationSQL,
		input.ReservationID,
		newEpoch,
		input.OwnerID,
		generation,
		input.OwnerLeaseDuration.Microseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.DeletionReservationFenceV1{}, recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("fence deletion reservation: %w", err)
	}
	fenceOwner, err := scanLiveOwnerLease(tx.QueryRow(ctx, fenceDeletionFenceLeaseSQL,
		input.Object.ProjectID,
		input.Object.ObjectKind,
		input.Object.ObjectID,
		input.OwnerID,
		generation,
		input.OwnerLeaseDuration.Microseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.DeletionReservationFenceV1{}, recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("fence deletion lease: %w", err)
	}
	if owner != fenceOwner || owner.OwnerID != input.OwnerID || owner.Generation != uint64(generation) {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("%w: divergent reservation and lease owners", recordplatform.ErrInvalidReservationFence)
	}

	fence := recordplatform.DeletionReservationFenceV1{
		ReservationID: input.ReservationID,
		Object:        input.Object,
		FenceEpoch:    recordplatform.ContentEpoch(newEpoch),
		Owner:         owner,
	}
	if err := fence.Validate(); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	return fence, nil
}

// RenewDeletionReservationFence extends the exact reservation and deletion
// fence owners together. A stale row in either relation rolls back both.
func (repository *PostgresRecordPlatformRepository) RenewDeletionReservationFence(ctx context.Context, fence recordplatform.DeletionReservationFenceV1, duration time.Duration) (recordplatform.DeletionReservationFenceV1, error) {
	if err := fence.Validate(); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	if err := validateLeaseRenewal(fence.Owner, duration); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	renewedOwner, err := scanRenewedOwnerLease(tx.QueryRow(ctx, renewDeletionReservationFenceSQL,
		fence.ReservationID,
		fence.Owner.OwnerID,
		fence.Owner.Generation,
		fence.FenceEpoch,
		duration.Microseconds(),
		fence.Owner.ExpiresAt,
	))
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("renew deletion reservation fence: %w", err)
	}
	renewedFenceOwner, err := scanRenewedOwnerLease(tx.QueryRow(ctx, renewDeletionFenceLeaseForReservationSQL,
		fence.Object.ProjectID,
		fence.Object.ObjectKind,
		fence.Object.ObjectID,
		fence.Owner.OwnerID,
		fence.Owner.Generation,
		duration.Microseconds(),
		fence.Owner.ExpiresAt,
	))
	if err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("renew deletion fence lease: %w", err)
	}
	if renewedOwner != renewedFenceOwner {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("%w: divergent renewed owners", recordplatform.ErrInvalidReservationFence)
	}
	renewed := fence
	renewed.Owner = renewedOwner
	if err := renewed.Validate(); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.DeletionReservationFenceV1{}, fmt.Errorf("commit deletion reservation fence renewal transaction: %w", err)
	}
	return renewed, nil
}

// ReleaseDeletionReservationFence releases only the exact live compound fence.
// The reservation transition and lease deletion occur in one transaction.
func (repository *PostgresRecordPlatformRepository) ReleaseDeletionReservationFence(ctx context.Context, fence recordplatform.DeletionReservationFenceV1) error {
	if err := fence.Validate(); err != nil {
		return err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return err
	}
	reservationCommand, err := tx.Exec(ctx, releaseDeletionReservationFenceSQL,
		fence.ReservationID,
		fence.Owner.OwnerID,
		fence.Owner.Generation,
		fence.FenceEpoch,
		fence.Owner.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("release deletion reservation fence: %w", err)
	}
	if reservationCommand.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	leaseCommand, err := tx.Exec(ctx, releaseDeletionFenceLeaseForReservationSQL,
		fence.Object.ProjectID,
		fence.Object.ObjectKind,
		fence.Object.ObjectID,
		fence.Owner.OwnerID,
		fence.Owner.Generation,
		fence.Owner.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("release deletion fence lease: %w", err)
	}
	if leaseCommand.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deletion reservation fence release transaction: %w", err)
	}
	return nil
}

// AssertDeletionReservationFence verifies the currently live compound fence
// and its exact delivery epoch without granting a local-clock authority.
func (repository *PostgresRecordPlatformRepository) AssertDeletionReservationFence(ctx context.Context, fence recordplatform.DeletionReservationFenceV1) error {
	if err := fence.Validate(); err != nil {
		return err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return err
	}
	var present int
	if err := tx.QueryRow(ctx, assertDeletionReservationFenceSQL,
		fence.ReservationID,
		fence.Object.ProjectID,
		fence.Object.ObjectKind,
		fence.Object.ObjectID,
		fence.Owner.OwnerID,
		fence.Owner.Generation,
		fence.FenceEpoch,
		fence.Owner.ExpiresAt,
	).Scan(&present); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return recordplatform.ErrLostOwnerLease
		}
		return fmt.Errorf("assert deletion reservation fence: %w", err)
	}
	if present != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deletion reservation fence assert transaction: %w", err)
	}
	return nil
}

type lockedDeletionReservationForFence struct {
	object          recordplatform.ObjectRef
	ownerGeneration int64
}

type lockedDeletionFenceLeaseForFence struct {
	generation int64
	live       bool
}

type lockedObjectContentLeaseForFence struct {
	owner recordplatform.OwnerLease
	live  bool
}

func lockPreviewedDeletionReservationForFence(ctx context.Context, tx pgx.Tx, reservationID string) (lockedDeletionReservationForFence, error) {
	var state string
	var projectID string
	var objectKind string
	var objectID string
	var ownerGeneration int64
	var ownerExpiresAt *time.Time
	var expiresAt time.Time
	err := tx.QueryRow(ctx, lockDeletionReservationForFenceSQL, reservationID).Scan(
		&state,
		&projectID,
		&objectKind,
		&objectID,
		&ownerGeneration,
		&ownerExpiresAt,
		&expiresAt,
	)
	if err != nil {
		return lockedDeletionReservationForFence{}, err
	}
	if state != "previewed" || ownerGeneration < 0 || ownerExpiresAt != nil || expiresAt.IsZero() {
		return lockedDeletionReservationForFence{}, fmt.Errorf("%w: observed preview reservation", recordplatform.ErrInvalidReservationFence)
	}
	object := recordplatform.ObjectRef{ProjectID: projectID, ObjectKind: objectKind, ObjectID: objectID}
	if err := object.Validate(); err != nil {
		return lockedDeletionReservationForFence{}, fmt.Errorf("%w: observed reservation object", recordplatform.ErrInvalidReservationFence)
	}
	return lockedDeletionReservationForFence{object: object, ownerGeneration: ownerGeneration}, nil
}

func lockContentDeliveryEpochForFence(ctx context.Context, tx pgx.Tx, object recordplatform.ObjectRef) (int64, error) {
	var epoch int64
	if err := tx.QueryRow(ctx, lockContentDeliveryEpochForFenceSQL, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&epoch); err != nil {
		return 0, err
	}
	if epoch < 0 || epoch == math.MaxInt64 {
		return 0, fmt.Errorf("%w: observed delivery epoch", recordplatform.ErrInvalidReservationFence)
	}
	return epoch, nil
}

func lockDeletionFenceLeaseForFence(ctx context.Context, tx pgx.Tx, object recordplatform.ObjectRef) (lockedDeletionFenceLeaseForFence, error) {
	var ownerID string
	var generation int64
	var expiresAt time.Time
	var live bool
	err := tx.QueryRow(ctx, lockDeletionFenceLeaseForFenceSQL, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&ownerID, &generation, &expiresAt, &live)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedDeletionFenceLeaseForFence{}, nil
	}
	if err != nil {
		return lockedDeletionFenceLeaseForFence{}, err
	}
	if generation < 1 || expiresAt.IsZero() || !recordPlatformOwnerIDValid(ownerID) {
		return lockedDeletionFenceLeaseForFence{}, fmt.Errorf("%w: observed deletion fence lease", recordplatform.ErrInvalidReservationFence)
	}
	return lockedDeletionFenceLeaseForFence{generation: generation, live: live}, nil
}

func lockObjectContentLeaseForFence(ctx context.Context, tx pgx.Tx, object recordplatform.ObjectRef) (lockedObjectContentLeaseForFence, error) {
	var ownerID string
	var generation int64
	var expiresAt time.Time
	var live bool
	err := tx.QueryRow(ctx, lockObjectContentLeaseForFenceSQL, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&ownerID, &generation, &expiresAt, &live)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedObjectContentLeaseForFence{}, nil
	}
	if err != nil {
		return lockedObjectContentLeaseForFence{}, err
	}
	if generation < 1 || expiresAt.IsZero() || !recordPlatformOwnerIDValid(ownerID) {
		return lockedObjectContentLeaseForFence{}, fmt.Errorf("%w: observed object content lease", recordplatform.ErrInvalidReservationFence)
	}
	owner := recordplatform.OwnerLease{OwnerID: ownerID, Generation: uint64(generation), ExpiresAt: expiresAt}
	if owner.Validate() != nil {
		return lockedObjectContentLeaseForFence{}, fmt.Errorf("%w: observed object content lease owner", recordplatform.ErrInvalidReservationFence)
	}
	return lockedObjectContentLeaseForFence{owner: owner, live: live}, nil
}

func incrementContentDeliveryEpochForFence(ctx context.Context, tx pgx.Tx, object recordplatform.ObjectRef) (int64, error) {
	var epoch int64
	err := tx.QueryRow(ctx, incrementContentDeliveryEpochForFenceSQL, object.ProjectID, object.ObjectKind, object.ObjectID).Scan(&epoch)
	if err != nil {
		return 0, err
	}
	if epoch < 0 {
		return 0, fmt.Errorf("%w: incremented delivery epoch", recordplatform.ErrInvalidReservationFence)
	}
	return epoch, nil
}

func nextReservationFenceGeneration(reservationGeneration, fenceGeneration int64) (int64, error) {
	if reservationGeneration < 0 || fenceGeneration < 0 || reservationGeneration == math.MaxInt64 || fenceGeneration == math.MaxInt64 {
		return 0, fmt.Errorf("%w: owner generation", recordplatform.ErrInvalidReservationFence)
	}
	if fenceGeneration > reservationGeneration {
		return fenceGeneration + 1, nil
	}
	return reservationGeneration + 1, nil
}

func recordPlatformOwnerIDValid(ownerID string) bool {
	return (&recordplatform.OwnerLease{OwnerID: ownerID, Generation: 1, ExpiresAt: time.Unix(1, 0)}).Validate() == nil
}
