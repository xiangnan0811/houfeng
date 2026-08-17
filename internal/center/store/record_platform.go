package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordplatform"
)

var ErrRecordPlatformAdmissionUnavailable = errors.New("record platform admission unavailable")

// AdmissionGate is intentionally scoped to an already-open PostgreSQL
// transaction. Implementations bind their immutable membership identity when
// constructed; claims and finalizers never derive identity from an event or
// payload.
type AdmissionGate interface {
	Admit(context.Context, pgx.Tx) error
}

// AdmissionGateFunc is a test adapter for an injected transaction gate.
type AdmissionGateFunc func(context.Context, pgx.Tx) error

func (gate AdmissionGateFunc) Admit(ctx context.Context, tx pgx.Tx) error {
	if gate == nil {
		return ErrRecordPlatformAdmissionUnavailable
	}
	return gate(ctx, tx)
}

// PostgresRecordPlatformRepository owns the pgx transaction seam for record
// delivery primitives. It accepts no sender, HTTP client, or network callback.
type PostgresRecordPlatformRepository struct {
	beginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)
	gate    AdmissionGate
}

// NewPostgresRecordPlatformRepository constructs the storage primitive with a
// fail-closed transaction admission gate.
func NewPostgresRecordPlatformRepository(pool *pgxpool.Pool, gate AdmissionGate) *PostgresRecordPlatformRepository {
	repository := &PostgresRecordPlatformRepository{gate: gate}
	if pool != nil {
		repository.beginTx = pool.BeginTx
	}
	return repository
}

// RecordPlatformTransactionCallback writes one business change and its
// delivery primitives through the same PostgreSQL transaction.
type RecordPlatformTransactionCallback func(context.Context, *RecordPlatformTransaction) error

// RecordPlatformTransaction exposes only named transaction-bound primitive
// methods. It deliberately has no generic SQL, sender, HTTP client, renderer,
// or other network API.
type RecordPlatformTransaction struct {
	repository *PostgresRecordPlatformRepository
	tx         pgx.Tx
}

// RunRecordPlatformTransaction executes one business callback atomically with
// record-platform primitives. The initial admission gate protects all callback
// writes; primitive claims re-check it before their own durable transition.
func (repository *PostgresRecordPlatformRepository) RunRecordPlatformTransaction(ctx context.Context, callback RecordPlatformTransactionCallback) error {
	if callback == nil {
		return errors.New("record platform transaction callback is nil")
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return err
	}

	transaction := &RecordPlatformTransaction{repository: repository, tx: tx}
	if err := callback(ctx, transaction); err != nil {
		return fmt.Errorf("run record platform transaction callback: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit record platform transaction: %w", err)
	}
	return nil
}

// ClaimIdempotency claims, replays, or takes over a durable idempotency row in
// the already-open transaction. Admission is rechecked at the primitive fence.
func (transaction *RecordPlatformTransaction) ClaimIdempotency(ctx context.Context, input recordplatform.IdempotencyClaimInputV1) (recordplatform.IdempotencyClaimResultV1, error) {
	if err := input.Validate(); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	if err := transaction.repository.admit(ctx, transaction.tx); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	return transaction.repository.claimIdempotencyInTransaction(ctx, transaction.tx, input)
}

// CompleteIdempotency writes a terminal result through the caller-owned
// admitted transaction, so a business fact, idempotency completion, and
// outbox identity can either commit together or all roll back.
func (transaction *RecordPlatformTransaction) CompleteIdempotency(ctx context.Context, key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease, result recordplatform.RequestFingerprintV1) error {
	if err := validateIdempotencyCompletion(key, owner, result); err != nil {
		return err
	}
	if err := transaction.repository.admit(ctx, transaction.tx); err != nil {
		return err
	}
	return completeIdempotencyInTransaction(ctx, transaction.tx, key, owner, result)
}

// RenewIdempotency extends an exact active owner from the caller-owned
// transaction. It never opens or commits a nested transaction.
func (transaction *RecordPlatformTransaction) RenewIdempotency(ctx context.Context, input recordplatform.IdempotencyRenewInputV1) (recordplatform.OwnerLease, error) {
	if err := input.Validate(); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	if err := transaction.repository.admit(ctx, transaction.tx); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	return renewIdempotencyInTransaction(ctx, transaction.tx, input)
}

// ReleaseIdempotency expires the exact active owner from the caller-owned
// transaction without opening or committing a nested transaction.
func (transaction *RecordPlatformTransaction) ReleaseIdempotency(ctx context.Context, key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	if err := transaction.repository.admit(ctx, transaction.tx); err != nil {
		return err
	}
	return releaseIdempotencyInTransaction(ctx, transaction.tx, key, owner)
}

// EnqueueOutbox inserts an identity-only delivery event in the same
// transaction as the business fact. Its worker must reauthorize the subject
// before every later delivery attempt.
func (transaction *RecordPlatformTransaction) EnqueueOutbox(ctx context.Context, input recordplatform.OutboxEnqueueInputV1) (recordplatform.OutboxEventRecordV1, error) {
	if err := input.Validate(); err != nil {
		return recordplatform.OutboxEventRecordV1{}, err
	}

	var rowID int64
	if err := transaction.tx.QueryRow(ctx, `
		insert into public.record_outbox (
			project_id,
			event_kind,
			subject_kind,
			subject_id,
			source_version,
			authorization_epoch,
			record_fence_epoch,
			expires_at
		) values (
			$1, $2, $3, $4, $5, $6, $7,
			transaction_timestamp() + ($8 * interval '1 microsecond')
		)
		returning outbox_row_id`,
		input.Event.ProjectID,
		input.Event.EventKind,
		input.Event.SubjectKind,
		input.Event.SubjectID,
		input.Event.SourceVersion,
		input.Event.AuthorizationEpoch,
		input.Event.RecordFenceEpoch,
		input.ExpiresAfter.Microseconds(),
	).Scan(&rowID); err != nil {
		return recordplatform.OutboxEventRecordV1{}, fmt.Errorf("enqueue outbox event: %w", err)
	}
	if rowID <= 0 {
		return recordplatform.OutboxEventRecordV1{}, fmt.Errorf("%w: outbox row id", recordplatform.ErrInvalidOutboxEvent)
	}
	event := input.Event
	event.RowID = rowID
	return recordplatform.OutboxEventRecordV1{Event: event}, nil
}

// AssertOutboxClaim locks the exact currently-live outbox owner tuple inside
// the caller's transaction. Projection writes must call it before deriving or
// inserting any recipient state so a stale worker cannot project after a
// takeover, cancellation, or database-time lease expiry.
func (transaction *RecordPlatformTransaction) AssertOutboxClaim(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) error {
	if ctx == nil || transaction == nil || transaction.repository == nil || transaction.tx == nil || claim.Validate() != nil {
		return recordplatform.ErrInvalidOutboxClaim
	}
	if err := transaction.repository.admit(ctx, transaction.tx); err != nil {
		return err
	}
	var present int
	err := transaction.tx.QueryRow(ctx, `
		select 1
		from public.record_outbox
		where outbox_row_id = $1
		  and status = 'processing'
		  and owner_id = $2
		  and owner_generation = $3
		  and owner_expires_at = $4
		  and owner_expires_at > transaction_timestamp()
		  and expires_at > transaction_timestamp()
		for update`,
		claim.Event.RowID, claim.Owner.OwnerID, claim.Owner.Generation, claim.Owner.ExpiresAt,
	).Scan(&present)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return fmt.Errorf("assert outbox projection owner: %w", err)
	}
	if present != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	return nil
}

// ClaimOutbox atomically claims the next pending or expired-processing event.
// It commits before returning the claim, so all authorization, rendering, and
// sending performed by the worker necessarily happen outside this transaction.
func (repository *PostgresRecordPlatformRepository) ClaimOutbox(ctx context.Context, input recordplatform.OutboxClaimInputV1) (*recordplatform.ClaimedOutboxEventV1, error) {
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

	claim, err := claimOutboxInTransaction(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit outbox claim transaction: %w", err)
	}
	return claim, nil
}

// CancelOutbox fences an undeliverable event with the exact committed owner
// generation. It cannot cancel a newer owner's claim.
func (repository *PostgresRecordPlatformRepository) CancelOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	return repository.finalizeOutbox(ctx, claim, `
		update public.record_outbox
		set status = 'cancelled',
		    owner_id = '',
		    owner_expires_at = null
		where outbox_row_id = $1
		  and status = 'processing'
		  and owner_id = $2
		  and owner_generation = $3
		  and owner_expires_at = $4
		  and owner_expires_at > transaction_timestamp()`,
		claim.Event.RowID,
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

// RetryOutbox releases a still-live claim back to pending with a database-time
// retry point. It refuses to schedule an attempt that would outlive the event.
func (repository *PostgresRecordPlatformRepository) RetryOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1, retryAfter time.Duration) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	if retryAfter.Microseconds() <= 0 {
		return fmt.Errorf("%w: retry delay", recordplatform.ErrInvalidOutboxClaim)
	}
	return repository.finalizeOutbox(ctx, claim, `
		update public.record_outbox
		set status = 'pending',
		    owner_id = '',
		    owner_expires_at = null,
		    next_attempt_at = transaction_timestamp() + ($4 * interval '1 microsecond')
		where outbox_row_id = $1
		  and status = 'processing'
		  and owner_id = $2
		  and owner_generation = $3
		  and owner_expires_at = $5
		  and owner_expires_at > transaction_timestamp()
		  and expires_at > transaction_timestamp() + ($4 * interval '1 microsecond')`,
		claim.Event.RowID,
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		retryAfter.Microseconds(),
		claim.Owner.ExpiresAt,
	)
}

// MarkOutboxSent records a successful delivery only while the exact owner
// fence remains live. An expired claim cannot finalize after a newer takeover.
func (repository *PostgresRecordPlatformRepository) MarkOutboxSent(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	return repository.finalizeOutbox(ctx, claim, `
		update public.record_outbox
		set status = 'sent',
		    owner_id = '',
		    owner_expires_at = null,
		    sent_at = transaction_timestamp()
		where outbox_row_id = $1
		  and status = 'processing'
		  and owner_id = $2
		  and owner_generation = $3
		  and owner_expires_at = $4
		  and owner_expires_at > transaction_timestamp()
		  and expires_at > transaction_timestamp()`,
		claim.Event.RowID,
		claim.Owner.OwnerID,
		claim.Owner.Generation,
		claim.Owner.ExpiresAt,
	)
}

func claimOutboxInTransaction(ctx context.Context, tx pgx.Tx, input recordplatform.OutboxClaimInputV1) (*recordplatform.ClaimedOutboxEventV1, error) {
	row := observedOutboxClaimRow{}
	err := tx.QueryRow(ctx, `
		with candidate as (
			select outbox_row_id
			from public.record_outbox
			where (
				(status = 'pending' and next_attempt_at <= transaction_timestamp())
				or (status = 'processing' and owner_expires_at <= transaction_timestamp())
			)
			and expires_at > transaction_timestamp() + ($2 * interval '1 microsecond')
			order by outbox_row_id
			for update skip locked
			limit 1
		)
		update public.record_outbox as outbox
		set status = 'processing',
		    owner_id = $1,
		    owner_generation = owner_generation + 1,
		    owner_expires_at = transaction_timestamp() + ($2 * interval '1 microsecond'),
		    attempt_count = outbox.attempt_count + 1
		from candidate
		where outbox.outbox_row_id = candidate.outbox_row_id
		returning outbox.outbox_row_id,
		          outbox.project_id,
		          outbox.event_kind,
		          outbox.subject_kind,
		          outbox.subject_id,
		          outbox.source_version,
		          outbox.authorization_epoch,
		          outbox.record_fence_epoch,
		          outbox.owner_id,
		          outbox.owner_generation,
		          outbox.owner_expires_at,
		          outbox.expires_at`,
		input.OwnerID,
		input.OwnerLeaseDuration.Microseconds(),
	).Scan(
		&row.rowID,
		&row.projectID,
		&row.eventKind,
		&row.subjectKind,
		&row.subjectID,
		&row.sourceVersion,
		&row.authorizationEpoch,
		&row.recordFenceEpoch,
		&row.ownerID,
		&row.ownerGeneration,
		&row.ownerExpiresAt,
		&row.expiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim outbox row: %w", err)
	}
	claim, err := row.claim()
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (repository *PostgresRecordPlatformRepository) finalizeOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1, sql string, arguments ...any) error {
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, sql, arguments...)
	if err != nil {
		return fmt.Errorf("finalize outbox row: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox finalization transaction: %w", err)
	}
	return nil
}

// ClaimIdempotency creates, replays, or takes over one idempotency row while
// holding its serialization-key row lock. Every durable timestamp is computed
// by PostgreSQL transaction_timestamp().
func (repository *PostgresRecordPlatformRepository) ClaimIdempotency(ctx context.Context, input recordplatform.IdempotencyClaimInputV1) (recordplatform.IdempotencyClaimResultV1, error) {
	if err := input.Validate(); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}

	result, err := repository.claimIdempotencyInTransaction(ctx, tx, input)
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, fmt.Errorf("commit idempotency claim transaction: %w", err)
	}
	return result, nil
}

// CompleteIdempotency records only a nonzero result fingerprint and clears the
// active owner fields. A stale owner gets one typed result from one UPDATE; no
// compensating write is attempted.
func (repository *PostgresRecordPlatformRepository) CompleteIdempotency(ctx context.Context, key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease, result recordplatform.RequestFingerprintV1) error {
	if err := validateIdempotencyCompletion(key, owner, result); err != nil {
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
	if err := completeIdempotencyInTransaction(ctx, tx, key, owner, result); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit idempotency completion transaction: %w", err)
	}
	return nil
}

// RenewIdempotency extends an exact live owner fence and writes a row expiry
// that remains strictly later after PostgreSQL microsecond normalization.
func (repository *PostgresRecordPlatformRepository) RenewIdempotency(ctx context.Context, input recordplatform.IdempotencyRenewInputV1) (recordplatform.OwnerLease, error) {
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
	owner, err := renewIdempotencyInTransaction(ctx, tx, input)
	if err != nil {
		return recordplatform.OwnerLease{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("commit idempotency renewal transaction: %w", err)
	}
	return owner, nil
}

// ReleaseIdempotency expires only the exact live owner instead of deleting its
// row. A later claim increments the retained generation, fencing this token.
func (repository *PostgresRecordPlatformRepository) ReleaseIdempotency(ctx context.Context, key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease) error {
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
	if err := releaseIdempotencyInTransaction(ctx, tx, key, owner); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit idempotency release transaction: %w", err)
	}
	return nil
}

func validateIdempotencyCompletion(key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease, result recordplatform.RequestFingerprintV1) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	persisted, err := result.PersistedBytes()
	if err != nil || persisted == [32]byte{} {
		return fmt.Errorf("%w: completed result", recordplatform.ErrInvalidIdempotencyRecord)
	}
	return nil
}

func completeIdempotencyInTransaction(ctx context.Context, tx pgx.Tx, key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease, result recordplatform.RequestFingerprintV1) error {
	persisted, err := result.PersistedBytes()
	if err != nil {
		return fmt.Errorf("encode completed idempotency fingerprint: %w", err)
	}
	command, err := tx.Exec(ctx, `
		update public.record_idempotency_keys
		set result_fingerprint = $4,
		    status = 'completed',
		    owner_id = '',
		    owner_generation = 0,
		    owner_expires_at = null
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and status = 'in_progress'
		  and owner_id = $5
		  and owner_generation = $6
		  and owner_expires_at = $7
		  and owner_expires_at > transaction_timestamp()`,
		string(key.ProjectID),
		string(key.OperationKind),
		key.Key,
		persisted[:],
		owner.OwnerID,
		owner.Generation,
		owner.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("complete idempotency row: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	return nil
}

func renewIdempotencyInTransaction(ctx context.Context, tx pgx.Tx, input recordplatform.IdempotencyRenewInputV1) (recordplatform.OwnerLease, error) {
	owner, err := scanIdempotencyOwner(tx.QueryRow(ctx, `
		update public.record_idempotency_keys
		set owner_expires_at = transaction_timestamp() + ($5 * interval '1 microsecond'),
		    expires_at = transaction_timestamp() + ($6 * interval '1 microsecond')
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and status = 'in_progress'
		  and owner_id = $4
		  and owner_generation = $7
		  and owner_expires_at = $8
		  and owner_expires_at > transaction_timestamp()
		  and expires_at > transaction_timestamp() + ($5 * interval '1 microsecond')
		returning owner_id, owner_generation, owner_expires_at`,
		string(input.Key.ProjectID),
		string(input.Key.OperationKind),
		input.Key.Key,
		input.Owner.OwnerID,
		input.OwnerLeaseDuration.Microseconds(),
		input.RecordTTL.Microseconds(),
		input.Owner.Generation,
		input.Owner.ExpiresAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.OwnerLease{}, recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return recordplatform.OwnerLease{}, fmt.Errorf("renew idempotency row: %w", err)
	}
	return owner, nil
}

func releaseIdempotencyInTransaction(ctx context.Context, tx pgx.Tx, key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease) error {
	command, err := tx.Exec(ctx, `
		update public.record_idempotency_keys
		set owner_expires_at = transaction_timestamp()
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and status = 'in_progress'
		  and owner_id = $4
		  and owner_generation = $5
		  and owner_expires_at = $6
		  and owner_expires_at > transaction_timestamp()`,
		string(key.ProjectID),
		string(key.OperationKind),
		key.Key,
		owner.OwnerID,
		owner.Generation,
		owner.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("release idempotency row: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	return nil
}

func (repository *PostgresRecordPlatformRepository) claimIdempotencyInTransaction(ctx context.Context, tx pgx.Tx, input recordplatform.IdempotencyClaimInputV1) (recordplatform.IdempotencyClaimResultV1, error) {
	row := observedIdempotencyRow{}
	err := tx.QueryRow(ctx, `
		select request_fingerprint,
		       result_fingerprint,
		       status,
		       owner_id,
		       owner_generation,
		       owner_expires_at,
		       expires_at,
		       transaction_timestamp()
		from public.record_idempotency_keys
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		for update`,
		string(input.Key.ProjectID),
		string(input.Key.OperationKind),
		input.Key.Key,
	).Scan(
		&row.requestFingerprint,
		&row.resultFingerprint,
		&row.status,
		&row.ownerID,
		&row.ownerGeneration,
		&row.ownerExpiresAt,
		&row.expiresAt,
		&row.observedDBTime,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.insertIdempotencyClaim(ctx, tx, input)
	}
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, fmt.Errorf("lock idempotency row: %w", err)
	}

	record, err := row.record(input.Key)
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	resolution, err := recordplatform.ResolveIdempotencyV1(record, input.RequestFingerprint, row.observedDBTime)
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	switch resolution.Action {
	case recordplatform.IdempotencyActionReplay:
		result := resolution.ResultFingerprint
		return recordplatform.IdempotencyClaimResultV1{ReplayResult: &result}, nil
	case recordplatform.IdempotencyActionTakeover:
		return takeoverIdempotencyClaim(ctx, tx, input)
	default:
		return recordplatform.IdempotencyClaimResultV1{}, fmt.Errorf("%w: idempotency resolution", recordplatform.ErrInvalidIdempotencyRecord)
	}
}

func (repository *PostgresRecordPlatformRepository) insertIdempotencyClaim(ctx context.Context, tx pgx.Tx, input recordplatform.IdempotencyClaimInputV1) (recordplatform.IdempotencyClaimResultV1, error) {
	requestFingerprint, err := input.RequestFingerprint.PersistedBytes()
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, fmt.Errorf("%w: request fingerprint", recordplatform.ErrInvalidIdempotencyClaim)
	}
	owner, err := scanIdempotencyOwner(tx.QueryRow(ctx, `
		insert into public.record_idempotency_keys (
			project_id,
			operation_kind,
			idempotency_key,
			request_fingerprint,
			status,
			owner_id,
			owner_generation,
			owner_expires_at,
			expires_at
		) values (
			$1, $2, $3, $4, 'in_progress', $5, 1,
			transaction_timestamp() + ($6 * interval '1 microsecond'),
			transaction_timestamp() + ($7 * interval '1 microsecond')
		)
		on conflict (project_id, operation_kind, idempotency_key) do nothing
		returning owner_id, owner_generation, owner_expires_at`,
		string(input.Key.ProjectID),
		string(input.Key.OperationKind),
		input.Key.Key,
		requestFingerprint[:],
		input.OwnerID,
		input.OwnerLeaseDuration.Microseconds(),
		input.RecordTTL.Microseconds(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent first claimant committed after this transaction observed
		// no row. Re-read under the key lock so the same durable resolution path
		// returns replay, in-progress, or expired takeover rather than a raw
		// unique-constraint error.
		return repository.claimIdempotencyInTransaction(ctx, tx, input)
	}
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, fmt.Errorf("insert idempotency claim: %w", err)
	}
	return recordplatform.IdempotencyClaimResultV1{Owner: &owner}, nil
}

func takeoverIdempotencyClaim(ctx context.Context, tx pgx.Tx, input recordplatform.IdempotencyClaimInputV1) (recordplatform.IdempotencyClaimResultV1, error) {
	requestFingerprint, err := input.RequestFingerprint.PersistedBytes()
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, fmt.Errorf("%w: request fingerprint", recordplatform.ErrInvalidIdempotencyClaim)
	}
	owner, err := scanIdempotencyOwner(tx.QueryRow(ctx, `
		update public.record_idempotency_keys
		set owner_id = $4,
		    owner_generation = owner_generation + 1,
		    owner_expires_at = transaction_timestamp() + ($5 * interval '1 microsecond'),
		    expires_at = transaction_timestamp() + ($6 * interval '1 microsecond')
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and request_fingerprint = $7
		  and status = 'in_progress'
		  and owner_expires_at <= transaction_timestamp()
		returning owner_id, owner_generation, owner_expires_at`,
		string(input.Key.ProjectID),
		string(input.Key.OperationKind),
		input.Key.Key,
		input.OwnerID,
		input.OwnerLeaseDuration.Microseconds(),
		input.RecordTTL.Microseconds(),
		requestFingerprint[:],
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return recordplatform.IdempotencyClaimResultV1{}, recordplatform.ErrIdempotencyInProgress
	}
	if err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, fmt.Errorf("take over idempotency claim: %w", err)
	}
	return recordplatform.IdempotencyClaimResultV1{Owner: &owner}, nil
}

func (repository *PostgresRecordPlatformRepository) startTransaction(ctx context.Context) (pgx.Tx, error) {
	if repository == nil || repository.beginTx == nil {
		return nil, fmt.Errorf("begin record platform transaction: %w", ErrRecordPlatformAdmissionUnavailable)
	}
	tx, err := repository.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin record platform transaction: %w", err)
	}
	return tx, nil
}

func (repository *PostgresRecordPlatformRepository) admit(ctx context.Context, tx pgx.Tx) error {
	if repository == nil || nilAdmissionGate(repository.gate) {
		return ErrRecordPlatformAdmissionUnavailable
	}
	if err := repository.gate.Admit(ctx, tx); err != nil {
		return fmt.Errorf("%w: %w", ErrRecordPlatformAdmissionUnavailable, err)
	}
	return nil
}

func nilAdmissionGate(gate AdmissionGate) bool {
	if gate == nil {
		return true
	}
	value := reflect.ValueOf(gate)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

type observedOutboxClaimRow struct {
	rowID              int64
	projectID          string
	eventKind          string
	subjectKind        string
	subjectID          string
	sourceVersion      int64
	authorizationEpoch int64
	recordFenceEpoch   int64
	ownerID            string
	ownerGeneration    int64
	ownerExpiresAt     time.Time
	expiresAt          time.Time
}

func (row observedOutboxClaimRow) claim() (recordplatform.ClaimedOutboxEventV1, error) {
	if row.sourceVersion < 0 || row.authorizationEpoch < 0 || row.recordFenceEpoch < 0 || row.ownerGeneration < 1 {
		return recordplatform.ClaimedOutboxEventV1{}, fmt.Errorf("%w: observed claim generation or epoch", recordplatform.ErrInvalidOutboxClaim)
	}
	claim := recordplatform.ClaimedOutboxEventV1{
		Event: recordplatform.OutboxEvent{
			RowID:              row.rowID,
			ProjectID:          row.projectID,
			EventKind:          row.eventKind,
			SubjectKind:        row.subjectKind,
			SubjectID:          row.subjectID,
			SourceVersion:      uint64(row.sourceVersion),
			AuthorizationEpoch: uint64(row.authorizationEpoch),
			RecordFenceEpoch:   uint64(row.recordFenceEpoch),
		},
		Owner: recordplatform.OwnerLease{
			OwnerID:    row.ownerID,
			Generation: uint64(row.ownerGeneration),
			ExpiresAt:  row.ownerExpiresAt,
		},
		ExpiresAt: row.expiresAt,
	}
	if err := claim.Validate(); err != nil {
		return recordplatform.ClaimedOutboxEventV1{}, err
	}
	return claim, nil
}

type observedIdempotencyRow struct {
	requestFingerprint []byte
	resultFingerprint  []byte
	status             string
	ownerID            string
	ownerGeneration    int64
	ownerExpiresAt     *time.Time
	expiresAt          time.Time
	observedDBTime     time.Time
}

func (row observedIdempotencyRow) record(key recordplatform.IdempotencyKey) (recordplatform.IdempotencyRecordV1, error) {
	requestFingerprint, err := recordPlatformFingerprint(row.requestFingerprint)
	if err != nil {
		return recordplatform.IdempotencyRecordV1{}, err
	}
	record := recordplatform.IdempotencyRecordV1{
		Key:                key,
		RequestFingerprint: requestFingerprint,
		Status:             recordplatform.IdempotencyStatus(row.status),
		ExpiresAt:          row.expiresAt,
	}
	if row.resultFingerprint != nil {
		resultFingerprint, err := recordPlatformFingerprint(row.resultFingerprint)
		if err != nil {
			return recordplatform.IdempotencyRecordV1{}, err
		}
		record.ResultFingerprint = &resultFingerprint
	}
	if row.ownerID != "" || row.ownerGeneration != 0 || row.ownerExpiresAt != nil {
		if row.ownerExpiresAt == nil || row.ownerGeneration < 1 {
			return recordplatform.IdempotencyRecordV1{}, fmt.Errorf("%w: observed owner", recordplatform.ErrInvalidIdempotencyRecord)
		}
		owner := recordplatform.OwnerLease{OwnerID: row.ownerID, Generation: uint64(row.ownerGeneration), ExpiresAt: *row.ownerExpiresAt}
		record.Owner = &owner
	}
	return record, nil
}

func scanIdempotencyOwner(row pgx.Row) (recordplatform.OwnerLease, error) {
	var ownerID string
	var generation int64
	var expiresAt time.Time
	if err := row.Scan(&ownerID, &generation, &expiresAt); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	if generation < 1 {
		return recordplatform.OwnerLease{}, fmt.Errorf("%w: owner generation", recordplatform.ErrInvalidOwnerLease)
	}
	owner := recordplatform.OwnerLease{OwnerID: ownerID, Generation: uint64(generation), ExpiresAt: expiresAt}
	if err := owner.Validate(); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	return owner, nil
}

func recordPlatformFingerprint(raw []byte) (recordplatform.PersistedRequestFingerprintV1, error) {
	fingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(raw)
	if err != nil {
		return recordplatform.PersistedRequestFingerprintV1{}, fmt.Errorf("%w: persisted fingerprint", recordplatform.ErrInvalidIdempotencyRecord)
	}
	return fingerprint, nil
}
