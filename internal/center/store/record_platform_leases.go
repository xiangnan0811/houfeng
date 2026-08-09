package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

const claimIdentityMutationGuardSQL = `
	insert into public.identity_mutation_guards (
		project_id,
		object_kind,
		object_id,
		mutation_kind,
		owner_id,
		owner_generation,
		expires_at
	) values (
		$1, $2, $3, $4, $5, 1,
		transaction_timestamp() + ($6 * interval '1 microsecond')
	)
	on conflict (project_id, object_kind, object_id, mutation_kind) do update
	set owner_id = excluded.owner_id,
		owner_generation = identity_mutation_guards.owner_generation + 1,
		expires_at = transaction_timestamp() + ($6 * interval '1 microsecond')
	where identity_mutation_guards.expires_at <= transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const renewIdentityMutationGuardSQL = `
	update public.identity_mutation_guards
	set expires_at = transaction_timestamp() + ($7 * interval '1 microsecond')
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and mutation_kind = $4
	  and owner_id = $5
	  and owner_generation = $6
	  and expires_at = $8
	  and expires_at > transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const releaseIdentityMutationGuardSQL = `
	update public.identity_mutation_guards
	set expires_at = transaction_timestamp()
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and mutation_kind = $4
	  and owner_id = $5
	  and owner_generation = $6
	  and expires_at = $7
	  and expires_at > transaction_timestamp()`

const claimDeletionFenceLeaseSQL = `
	insert into public.deletion_fence_leases (
		project_id, object_kind, object_id, owner_id, owner_generation, expires_at
	) values (
		$1, $2, $3, $4, 1,
		transaction_timestamp() + ($5 * interval '1 microsecond')
	)
	on conflict (project_id, object_kind, object_id) do update
	set owner_id = excluded.owner_id,
		owner_generation = deletion_fence_leases.owner_generation + 1,
		expires_at = transaction_timestamp() + ($5 * interval '1 microsecond')
	where deletion_fence_leases.expires_at <= transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const renewDeletionFenceLeaseSQL = `
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

const releaseDeletionFenceLeaseSQL = `
	update public.deletion_fence_leases
	set expires_at = transaction_timestamp()
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and expires_at = $6
	  and expires_at > transaction_timestamp()`

const claimObjectContentLeaseSQL = `
	insert into public.object_content_leases (
		project_id, object_kind, object_id, owner_id, owner_generation, expires_at
	) values (
		$1, $2, $3, $4, 1,
		transaction_timestamp() + ($5 * interval '1 microsecond')
	)
	on conflict (project_id, object_kind, object_id) do update
	set owner_id = excluded.owner_id,
		owner_generation = object_content_leases.owner_generation + 1,
		expires_at = transaction_timestamp() + ($5 * interval '1 microsecond')
	where object_content_leases.expires_at <= transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const renewObjectContentLeaseSQL = `
	update public.object_content_leases
	set expires_at = transaction_timestamp() + ($6 * interval '1 microsecond')
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and expires_at = $7
	  and expires_at > transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const releaseObjectContentLeaseSQL = `
	update public.object_content_leases
	set expires_at = transaction_timestamp()
	where project_id = $1
	  and object_kind = $2
	  and object_id = $3
	  and owner_id = $4
	  and owner_generation = $5
	  and expires_at = $6
	  and expires_at > transaction_timestamp()`

const claimClientContentLeaseSQL = `
	insert into public.client_content_leases (
		project_id, client_id, owner_id, owner_generation, expires_at
	) values (
		$1, $2, $3, 1,
		transaction_timestamp() + ($4 * interval '1 microsecond')
	)
	on conflict (project_id, client_id) do update
	set owner_id = excluded.owner_id,
		owner_generation = client_content_leases.owner_generation + 1,
		expires_at = transaction_timestamp() + ($4 * interval '1 microsecond')
	where client_content_leases.expires_at <= transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const renewClientContentLeaseSQL = `
	update public.client_content_leases
	set expires_at = transaction_timestamp() + ($5 * interval '1 microsecond')
	where project_id = $1
	  and client_id = $2
	  and owner_id = $3
	  and owner_generation = $4
	  and expires_at = $6
	  and expires_at > transaction_timestamp()
	returning owner_id, owner_generation, expires_at`

const releaseClientContentLeaseSQL = `
	update public.client_content_leases
	set expires_at = transaction_timestamp()
	where project_id = $1
	  and client_id = $2
	  and owner_id = $3
	  and owner_generation = $4
	  and expires_at = $5
	  and expires_at > transaction_timestamp()`

const assertServingLeaseSQL = `
	select 1
	from public.object_content_leases as object_lease
	join public.content_delivery_epochs as epoch
	  on epoch.project_id = object_lease.project_id
	 and epoch.object_kind = object_lease.object_kind
	 and epoch.object_id = object_lease.object_id
	where object_lease.project_id = $1
	  and object_lease.object_kind = $2
	  and object_lease.object_id = $3
	  and object_lease.owner_id = $4
	  and object_lease.owner_generation = $5
	  and object_lease.expires_at = $6
	  and object_lease.expires_at > transaction_timestamp()
	  and epoch.delivery_epoch = $7
	  and not exists (
		select 1
		from public.deletion_fence_leases as fence
		where fence.project_id = object_lease.project_id
		  and fence.object_kind = object_lease.object_kind
		  and fence.object_id = object_lease.object_id
		  and fence.expires_at > transaction_timestamp()
	)`

// AcquireIdentityMutationGuards claims the supplied guard rows in canonical
// tuple order inside one admitted transaction. A live row makes the full
// multi-key acquisition fail and roll back.
func (repository *PostgresRecordPlatformRepository) AcquireIdentityMutationGuards(ctx context.Context, keys []recordplatform.IdentityMutationGuardKeyV1, input recordplatform.LeaseClaimInputV1) ([]recordplatform.IdentityMutationGuardV1, error) {
	canonicalKeys, err := recordplatform.CanonicalIdentityMutationGuardKeysV1(keys)
	if err != nil {
		return nil, err
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return nil, err
	}

	guards := make([]recordplatform.IdentityMutationGuardV1, 0, len(canonicalKeys))
	for _, key := range canonicalKeys {
		owner, err := claimIdentityMutationGuardInTransaction(ctx, tx, key, input)
		if err != nil {
			return nil, err
		}
		guards = append(guards, recordplatform.IdentityMutationGuardV1{Key: key, Owner: owner})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit identity mutation guard claim transaction: %w", err)
	}
	return guards, nil
}

// RenewIdentityMutationGuard extends one guard only while its exact database
// owner fence is still live.
func (repository *PostgresRecordPlatformRepository) RenewIdentityMutationGuard(ctx context.Context, key recordplatform.IdentityMutationGuardKeyV1, owner recordplatform.OwnerLease, duration time.Duration) (recordplatform.IdentityMutationGuardV1, error) {
	if err := key.Validate(); err != nil {
		return recordplatform.IdentityMutationGuardV1{}, err
	}
	if err := validateLeaseRenewal(owner, duration); err != nil {
		return recordplatform.IdentityMutationGuardV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.IdentityMutationGuardV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.IdentityMutationGuardV1{}, err
	}
	renewed, err := scanRenewedOwnerLease(tx.QueryRow(ctx, renewIdentityMutationGuardSQL,
		key.Object.ProjectID,
		key.Object.ObjectKind,
		key.Object.ObjectID,
		key.MutationKind,
		owner.OwnerID,
		owner.Generation,
		duration.Microseconds(),
		owner.ExpiresAt,
	))
	if err != nil {
		return recordplatform.IdentityMutationGuardV1{}, fmt.Errorf("renew identity mutation guard: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.IdentityMutationGuardV1{}, fmt.Errorf("commit identity mutation guard renewal transaction: %w", err)
	}
	return recordplatform.IdentityMutationGuardV1{Key: key, Owner: renewed}, nil
}

// ReleaseIdentityMutationGuard removes one guard only while its exact owner
// fence remains live.
func (repository *PostgresRecordPlatformRepository) ReleaseIdentityMutationGuard(ctx context.Context, key recordplatform.IdentityMutationGuardKeyV1, owner recordplatform.OwnerLease) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	return repository.releaseIdentityMutationGuard(ctx, key, owner)
}

// AcquireDeletionFenceLease claims an object deletion-fence lease.
func (repository *PostgresRecordPlatformRepository) AcquireDeletionFenceLease(ctx context.Context, object recordplatform.ObjectRef, input recordplatform.LeaseClaimInputV1) (recordplatform.DeletionFenceLeaseV1, error) {
	if err := object.Validate(); err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, err
	}
	if err := input.Validate(); err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, err
	}
	if _, err := lockContentDeliveryEpochForFence(ctx, tx, object); errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.DeletionFenceLeaseV1{}, recordplatform.ErrContentDeliveryEpochMissing
	} else if err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, fmt.Errorf("lock content delivery epoch for deletion fence lease: %w", err)
	}
	owner, err := claimObjectLeaseInTransaction(ctx, tx, object, input, claimDeletionFenceLeaseSQL)
	if err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, fmt.Errorf("commit deletion fence lease claim transaction: %w", err)
	}
	return recordplatform.DeletionFenceLeaseV1{Object: object, Owner: owner}, nil
}

// RenewDeletionFenceLease extends the exact live deletion-fence owner.
func (repository *PostgresRecordPlatformRepository) RenewDeletionFenceLease(ctx context.Context, object recordplatform.ObjectRef, owner recordplatform.OwnerLease, duration time.Duration) (recordplatform.DeletionFenceLeaseV1, error) {
	renewed, err := repository.renewObjectLease(ctx, object, owner, duration, renewDeletionFenceLeaseSQL)
	if err != nil {
		return recordplatform.DeletionFenceLeaseV1{}, err
	}
	return recordplatform.DeletionFenceLeaseV1{Object: object, Owner: renewed}, nil
}

// ReleaseDeletionFenceLease removes the exact live deletion-fence owner.
func (repository *PostgresRecordPlatformRepository) ReleaseDeletionFenceLease(ctx context.Context, object recordplatform.ObjectRef, owner recordplatform.OwnerLease) error {
	return repository.releaseObjectLease(ctx, object, owner, releaseDeletionFenceLeaseSQL)
}

// AcquireObjectContentLease claims an object-specific content lease.
func (repository *PostgresRecordPlatformRepository) AcquireObjectContentLease(ctx context.Context, object recordplatform.ObjectRef, input recordplatform.LeaseClaimInputV1) (recordplatform.ObjectContentLeaseV1, error) {
	if err := object.Validate(); err != nil {
		return recordplatform.ObjectContentLeaseV1{}, err
	}
	if err := input.Validate(); err != nil {
		return recordplatform.ObjectContentLeaseV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.ObjectContentLeaseV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.ObjectContentLeaseV1{}, err
	}
	if _, err := lockContentDeliveryEpochForFence(ctx, tx, object); errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.ObjectContentLeaseV1{}, recordplatform.ErrContentDeliveryEpochMissing
	} else if err != nil {
		return recordplatform.ObjectContentLeaseV1{}, fmt.Errorf("lock content delivery epoch for object content lease: %w", err)
	}
	deletionFence, err := lockDeletionFenceLeaseForFence(ctx, tx, object)
	if err != nil {
		return recordplatform.ObjectContentLeaseV1{}, fmt.Errorf("lock deletion fence lease for object content lease: %w", err)
	}
	if deletionFence.live {
		return recordplatform.ObjectContentLeaseV1{}, recordplatform.ErrDeletionFenceLeaseLive
	}
	owner, err := claimObjectLeaseInTransaction(ctx, tx, object, input, claimObjectContentLeaseSQL)
	if err != nil {
		return recordplatform.ObjectContentLeaseV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.ObjectContentLeaseV1{}, fmt.Errorf("commit object content lease claim transaction: %w", err)
	}
	return recordplatform.ObjectContentLeaseV1{Object: object, Owner: owner}, nil
}

// RenewObjectContentLease extends the exact live object-content owner.
func (repository *PostgresRecordPlatformRepository) RenewObjectContentLease(ctx context.Context, object recordplatform.ObjectRef, owner recordplatform.OwnerLease, duration time.Duration) (recordplatform.ObjectContentLeaseV1, error) {
	renewed, err := repository.renewObjectLease(ctx, object, owner, duration, renewObjectContentLeaseSQL)
	if err != nil {
		return recordplatform.ObjectContentLeaseV1{}, err
	}
	return recordplatform.ObjectContentLeaseV1{Object: object, Owner: renewed}, nil
}

// RenewServingLease extends an exact serving owner only after serializing with
// reservation fencing under the epoch -> deletion fence -> content lease lock
// order. The captured epoch and absence of a live fence are checked in the
// same admitted transaction as the owner-expiry update.
func (repository *PostgresRecordPlatformRepository) RenewServingLease(ctx context.Context, serving recordplatform.ServingLeaseV1, duration time.Duration) (recordplatform.ServingLeaseV1, error) {
	if err := serving.Validate(); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	if err := validateLeaseRenewal(serving.Owner, duration); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	epoch, err := lockContentDeliveryEpochForFence(ctx, tx, serving.Object)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.ServingLeaseV1{}, recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("lock content delivery epoch for serving lease renewal: %w", err)
	}
	if recordplatform.ContentEpoch(epoch) != serving.CapturedEpoch {
		return recordplatform.ServingLeaseV1{}, recordplatform.ErrLostOwnerLease
	}
	deletionFence, err := lockDeletionFenceLeaseForFence(ctx, tx, serving.Object)
	if err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("lock deletion fence for serving lease renewal: %w", err)
	}
	if deletionFence.live {
		return recordplatform.ServingLeaseV1{}, recordplatform.ErrLostOwnerLease
	}
	contentLease, err := lockObjectContentLeaseForFence(ctx, tx, serving.Object)
	if err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("lock object content lease for serving lease renewal: %w", err)
	}
	if !contentLease.live || contentLease.owner != serving.Owner {
		return recordplatform.ServingLeaseV1{}, recordplatform.ErrLostOwnerLease
	}
	renewedOwner, err := scanRenewedOwnerLease(tx.QueryRow(ctx, renewObjectContentLeaseSQL,
		serving.Object.ProjectID,
		serving.Object.ObjectKind,
		serving.Object.ObjectID,
		serving.Owner.OwnerID,
		serving.Owner.Generation,
		duration.Microseconds(),
		serving.Owner.ExpiresAt,
	))
	if err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("renew serving object content lease: %w", err)
	}
	renewed, err := recordplatform.NewServingLeaseV1(
		recordplatform.ObjectContentLeaseV1{Object: serving.Object, Owner: renewedOwner},
		serving.CapturedEpoch,
	)
	if err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	if err := assertServingLeaseInTransaction(ctx, tx, renewed); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("commit serving lease renewal transaction: %w", err)
	}
	return renewed, nil
}

// ReleaseObjectContentLease removes the exact live object-content owner.
func (repository *PostgresRecordPlatformRepository) ReleaseObjectContentLease(ctx context.Context, object recordplatform.ObjectRef, owner recordplatform.OwnerLease) error {
	return repository.releaseObjectLease(ctx, object, owner, releaseObjectContentLeaseSQL)
}

// AcquireServingLease creates an object-content lease and returns a serving
// token only after one admitted transaction has captured the persisted epoch,
// checked that no deletion fence is live, and re-asserted the exact returned
// owner fence.
func (repository *PostgresRecordPlatformRepository) AcquireServingLease(ctx context.Context, object recordplatform.ObjectRef, input recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error) {
	if err := object.Validate(); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	if err := input.Validate(); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}

	epoch, err := lockContentDeliveryEpochForFence(ctx, tx, object)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.ServingLeaseV1{}, recordplatform.ErrContentDeliveryEpochMissing
	}
	if err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("lock content delivery epoch for serving lease: %w", err)
	}
	deletionFence, err := lockDeletionFenceLeaseForFence(ctx, tx, object)
	if err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("lock deletion fence lease for serving lease: %w", err)
	}
	if deletionFence.live {
		return recordplatform.ServingLeaseV1{}, recordplatform.ErrDeletionFenceLeaseLive
	}
	owner, err := claimObjectLeaseInTransaction(ctx, tx, object, input, claimObjectContentLeaseSQL)
	if err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	serving, err := recordplatform.NewServingLeaseV1(recordplatform.ObjectContentLeaseV1{Object: object, Owner: owner}, recordplatform.ContentEpoch(epoch))
	if err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	if err := assertServingLeaseInTransaction(ctx, tx, serving); err != nil {
		return recordplatform.ServingLeaseV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.ServingLeaseV1{}, fmt.Errorf("commit serving lease acquisition transaction: %w", err)
	}
	return serving, nil
}

// AssertServingLease checks the exact database owner fence, captured epoch,
// and absence of a live deletion fence in one admitted transaction. A caller's
// in-memory token is never accepted as proof of those facts.
func (repository *PostgresRecordPlatformRepository) AssertServingLease(ctx context.Context, serving recordplatform.ServingLeaseV1) error {
	if err := serving.Validate(); err != nil {
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
	if err := assertServingLeaseInTransaction(ctx, tx, serving); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit serving lease assertion transaction: %w", err)
	}
	return nil
}

// AcquireClientContentLease claims a client-content lease that has no object
// serving authority.
func (repository *PostgresRecordPlatformRepository) AcquireClientContentLease(ctx context.Context, key recordplatform.ClientContentLeaseKeyV1, input recordplatform.LeaseClaimInputV1) (recordplatform.ClientContentLeaseV1, error) {
	if err := key.Validate(); err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	if err := input.Validate(); err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	owner, err := claimClientContentLeaseInTransaction(ctx, tx, key, input)
	if err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.ClientContentLeaseV1{}, fmt.Errorf("commit client content lease claim transaction: %w", err)
	}
	return recordplatform.ClientContentLeaseV1{Key: key, Owner: owner}, nil
}

// RenewClientContentLease extends the exact live client-content owner.
func (repository *PostgresRecordPlatformRepository) RenewClientContentLease(ctx context.Context, key recordplatform.ClientContentLeaseKeyV1, owner recordplatform.OwnerLease, duration time.Duration) (recordplatform.ClientContentLeaseV1, error) {
	if err := key.Validate(); err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	if err := validateLeaseRenewal(owner, duration); err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.ClientContentLeaseV1{}, err
	}
	renewed, err := scanRenewedOwnerLease(tx.QueryRow(ctx, renewClientContentLeaseSQL,
		key.ProjectID,
		key.ClientID,
		owner.OwnerID,
		owner.Generation,
		duration.Microseconds(),
		owner.ExpiresAt,
	))
	if err != nil {
		return recordplatform.ClientContentLeaseV1{}, fmt.Errorf("renew client content lease: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.ClientContentLeaseV1{}, fmt.Errorf("commit client content lease renewal transaction: %w", err)
	}
	return recordplatform.ClientContentLeaseV1{Key: key, Owner: renewed}, nil
}

// ReleaseClientContentLease removes the exact live client-content owner.
func (repository *PostgresRecordPlatformRepository) ReleaseClientContentLease(ctx context.Context, key recordplatform.ClientContentLeaseKeyV1, owner recordplatform.OwnerLease) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
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
	command, err := tx.Exec(ctx, releaseClientContentLeaseSQL, key.ProjectID, key.ClientID, owner.OwnerID, owner.Generation, owner.ExpiresAt)
	if err != nil {
		return fmt.Errorf("release client content lease: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit client content lease release transaction: %w", err)
	}
	return nil
}

func claimIdentityMutationGuardInTransaction(ctx context.Context, tx pgx.Tx, key recordplatform.IdentityMutationGuardKeyV1, input recordplatform.LeaseClaimInputV1) (recordplatform.OwnerLease, error) {
	owner, err := scanLiveOwnerLease(tx.QueryRow(ctx, claimIdentityMutationGuardSQL,
		key.Object.ProjectID,
		key.Object.ObjectKind,
		key.Object.ObjectID,
		key.MutationKind,
		input.OwnerID,
		input.LeaseDuration.Microseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.OwnerLease{}, recordplatform.ErrLeaseAlreadyHeld
	}
	if err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("claim identity mutation guard: %w", err)
	}
	return owner, nil
}

func (repository *PostgresRecordPlatformRepository) releaseIdentityMutationGuard(ctx context.Context, key recordplatform.IdentityMutationGuardKeyV1, owner recordplatform.OwnerLease) error {
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, releaseIdentityMutationGuardSQL,
		key.Object.ProjectID,
		key.Object.ObjectKind,
		key.Object.ObjectID,
		key.MutationKind,
		owner.OwnerID,
		owner.Generation,
		owner.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("release identity mutation guard: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit identity mutation guard release transaction: %w", err)
	}
	return nil
}

func (repository *PostgresRecordPlatformRepository) acquireObjectLease(ctx context.Context, object recordplatform.ObjectRef, input recordplatform.LeaseClaimInputV1, claimSQL string) (recordplatform.OwnerLease, error) {
	if err := object.Validate(); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	if err := input.Validate(); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.OwnerLease{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	owner, err := claimObjectLeaseInTransaction(ctx, tx, object, input, claimSQL)
	if err != nil {
		return recordplatform.OwnerLease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("commit object lease claim transaction: %w", err)
	}
	return owner, nil
}

func claimObjectLeaseInTransaction(ctx context.Context, tx pgx.Tx, object recordplatform.ObjectRef, input recordplatform.LeaseClaimInputV1, claimSQL string) (recordplatform.OwnerLease, error) {
	owner, err := scanLiveOwnerLease(tx.QueryRow(ctx, claimSQL,
		object.ProjectID,
		object.ObjectKind,
		object.ObjectID,
		input.OwnerID,
		input.LeaseDuration.Microseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.OwnerLease{}, recordplatform.ErrLeaseAlreadyHeld
	}
	if err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("claim object lease: %w", err)
	}
	return owner, nil
}

func (repository *PostgresRecordPlatformRepository) renewObjectLease(ctx context.Context, object recordplatform.ObjectRef, owner recordplatform.OwnerLease, duration time.Duration, renewSQL string) (recordplatform.OwnerLease, error) {
	if err := object.Validate(); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	if err := validateLeaseRenewal(owner, duration); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.OwnerLease{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	renewed, err := scanRenewedOwnerLease(tx.QueryRow(ctx, renewSQL,
		object.ProjectID,
		object.ObjectKind,
		object.ObjectID,
		owner.OwnerID,
		owner.Generation,
		duration.Microseconds(),
		owner.ExpiresAt,
	))
	if err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("renew object lease: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("commit object lease renewal transaction: %w", err)
	}
	return renewed, nil
}

func (repository *PostgresRecordPlatformRepository) releaseObjectLease(ctx context.Context, object recordplatform.ObjectRef, owner recordplatform.OwnerLease, releaseSQL string) error {
	if err := object.Validate(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
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
	command, err := tx.Exec(ctx, releaseSQL,
		object.ProjectID,
		object.ObjectKind,
		object.ObjectID,
		owner.OwnerID,
		owner.Generation,
		owner.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("release object lease: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit object lease release transaction: %w", err)
	}
	return nil
}

func claimClientContentLeaseInTransaction(ctx context.Context, tx pgx.Tx, key recordplatform.ClientContentLeaseKeyV1, input recordplatform.LeaseClaimInputV1) (recordplatform.OwnerLease, error) {
	owner, err := scanLiveOwnerLease(tx.QueryRow(ctx, claimClientContentLeaseSQL,
		key.ProjectID,
		key.ClientID,
		input.OwnerID,
		input.LeaseDuration.Microseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.OwnerLease{}, recordplatform.ErrLeaseAlreadyHeld
	}
	if err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("claim client content lease: %w", err)
	}
	return owner, nil
}

func assertServingLeaseInTransaction(ctx context.Context, tx pgx.Tx, serving recordplatform.ServingLeaseV1) error {
	var present int
	err := tx.QueryRow(ctx, assertServingLeaseSQL,
		serving.Object.ProjectID,
		serving.Object.ObjectKind,
		serving.Object.ObjectID,
		serving.Owner.OwnerID,
		serving.Owner.Generation,
		serving.Owner.ExpiresAt,
		serving.CapturedEpoch,
	).Scan(&present)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return fmt.Errorf("assert serving lease: %w", err)
	}
	if present != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	return nil
}

func validateLeaseRenewal(owner recordplatform.OwnerLease, duration time.Duration) error {
	if err := owner.Validate(); err != nil {
		return err
	}
	if duration.Microseconds() <= 0 {
		return fmt.Errorf("%w: duration", recordplatform.ErrInvalidLease)
	}
	return nil
}

func scanLiveOwnerLease(row pgx.Row) (recordplatform.OwnerLease, error) {
	owner, err := scanIdempotencyOwner(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.OwnerLease{}, err
	}
	if err != nil {
		return recordplatform.OwnerLease{}, err
	}
	return owner, nil
}

func scanRenewedOwnerLease(row pgx.Row) (recordplatform.OwnerLease, error) {
	owner, err := scanLiveOwnerLease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.OwnerLease{}, recordplatform.ErrLostOwnerLease
	}
	return owner, err
}
