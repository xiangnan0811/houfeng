package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
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
	repository               *PostgresRecordPlatformRepository
	tx                       pgx.Tx
	mutationRecords          map[string]struct{}
	idempotencyScopes        map[recordplatform.IdempotencyKey]struct{}
	idempotencyScopesSealed  bool
	idempotencyScopeConflict bool
}

// The resource coordinator calls this only after the complete sorted scope
// set has actually been acquired. This tracks ordering within this Go
// transaction; it is not database authorization or evidence of a SQL lock.
func (transaction *RecordPlatformTransaction) registerAcquiredIdempotencyScopes(resources recordMutationResources) {
	items := resources.resources.Idempotency
	if transaction.idempotencyScopesSealed {
		if len(items) != len(transaction.idempotencyScopes) {
			transaction.idempotencyScopeConflict = true
			return
		}
		for _, item := range items {
			if _, held := transaction.idempotencyScopes[item.Key]; !held {
				transaction.idempotencyScopeConflict = true
				return
			}
		}
		return
	}
	if len(items) != 0 {
		transaction.idempotencyScopes = make(map[recordplatform.IdempotencyKey]struct{}, len(items))
		for _, item := range items {
			transaction.idempotencyScopes[item.Key] = struct{}{}
		}
	}
	transaction.idempotencyScopesSealed = true
}

func (transaction *RecordPlatformTransaction) requireIdempotencyScope(ctx context.Context, key recordplatform.IdempotencyKey) error {
	if transaction.idempotencyScopeConflict {
		return &pgconn.PgError{Code: "40001", Message: "idempotency scope set changed; restart transaction"}
	}
	if transaction.idempotencyScopesSealed {
		if _, held := transaction.idempotencyScopes[key]; !held {
			return &pgconn.PgError{Code: "40001", Message: "undeclared idempotency scope; restart transaction"}
		}
		return nil
	}
	// An otherwise uncoordinated primitive may acquire one scope. Once its row
	// has been touched, acquiring another physical scope could invert rank 71.
	if err := lockRecordIdempotencyScope(ctx, transaction.tx, key); err != nil {
		return err
	}
	transaction.idempotencyScopes = map[recordplatform.IdempotencyKey]struct{}{key: {}}
	transaction.idempotencyScopesSealed = true
	return nil
}

// Only the coordinator that has acquired and rechecked the complete resource
// set may register routes here; a declaration alone grants no authority.
func (transaction *RecordPlatformTransaction) registerAcquiredRecordMutationResources(resources recordMutationResources) {
	transaction.registerAcquiredIdempotencyScopes(resources)
	if transaction.mutationRecords == nil {
		transaction.mutationRecords = make(map[string]struct{}, len(resources.resources.Records))
	}
	for _, record := range resources.resources.Records {
		transaction.mutationRecords[record.ProjectID+"\x00"+record.RecordID] = struct{}{}
	}
}

// RunRecordPlatformTransaction executes one business callback atomically with
// record-platform primitives. The initial admission gate protects all callback
// writes; primitive claims re-check it before their own durable transition.
func (repository *PostgresRecordPlatformRepository) RunRecordPlatformTransaction(ctx context.Context, callback RecordPlatformTransactionCallback) error {
	return repository.runRecordPlatformTransaction(ctx, pgx.TxOptions{}, callback)
}

// Source authorization and its effects share the asset-graph fence. W2 proof
// preparation must finish before entering this callback; no external I/O is
// permitted while the generation and graph locks are held.
func (repository *PostgresRecordPlatformRepository) runSourceAuthorizedTransaction(ctx context.Context, callback RecordPlatformTransactionCallback) error {
	if callback == nil {
		return errors.New("source-authorized transaction callback is nil")
	}
	return repository.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := lockAssetGraphForSync(ctx, transaction.tx); err != nil {
			return err
		}
		return callback(ctx, transaction)
	})
}

func (repository *PostgresRecordPlatformRepository) runRecordPlatformTransaction(ctx context.Context, options pgx.TxOptions, callback RecordPlatformTransactionCallback) error {
	if callback == nil {
		return errors.New("record platform transaction callback is nil")
	}
	return repository.runRecordPlatformTransactionAttempt(ctx, options, callback)
}

// Mutation callbacks explicitly opt in to whole-transaction reselection. They
// reset captured results on entry and perform no external I/O. Read callbacks
// and unknown commit outcomes are never retried by the general wrapper.
func (repository *PostgresRecordPlatformRepository) runRecordMutationTransaction(ctx context.Context, callback RecordPlatformTransactionCallback) error {
	if ctx == nil || callback == nil {
		return errors.New("invalid record mutation transaction")
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := repository.runRecordPlatformTransactionAttempt(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, callback)
		var postgresError *pgconn.PgError
		if attempt == 2 || !errors.As(err, &postgresError) || postgresError.Code != "40001" {
			return err
		}
	}
}

// Each birth-dependent attempt reads continuity before opening its APP
// transaction. A serialization retry must not reuse the previous observation.
func (repository *PostgresRecordPlatformRepository) runRecordBirthMutationTransaction(ctx context.Context, source recordbirth.ContinuitySource, callback func(context.Context, *RecordPlatformTransaction, []byte) error) error {
	if ctx == nil || callback == nil {
		return errors.New("invalid record birth mutation transaction")
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		proof := []byte{}
		if !nilRecordSubjectDependency(source) {
			var err error
			proof, err = source.ReadContinuity(ctx)
			if err != nil {
				return err
			}
			if proof == nil {
				proof = []byte{}
			}
		}
		err := repository.runRecordPlatformTransactionAttempt(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			// The epoch guard authenticates this signed token before rank 55;
			// setting it alone does not grant authority or acquire the head lock.
			if _, err := transaction.tx.Exec(ctx, `select set_config('houfeng.birth_continuity_proof',$1,true)`, hex.EncodeToString(proof)); err != nil {
				return err
			}
			return callback(ctx, transaction, proof)
		})
		var postgresError *pgconn.PgError
		if attempt == 2 || !errors.As(err, &postgresError) || postgresError.Code != "40001" {
			return err
		}
	}
}

func (repository *PostgresRecordPlatformRepository) runSourceAuthorizedBirthMutationTransaction(ctx context.Context, source recordbirth.ContinuitySource, callback func(context.Context, *RecordPlatformTransaction, []byte) error) error {
	if callback == nil {
		return errors.New("source-authorized birth mutation callback is nil")
	}
	return repository.runRecordBirthMutationTransaction(ctx, source, func(ctx context.Context, transaction *RecordPlatformTransaction, proof []byte) error {
		if err := lockAssetGraphForSync(ctx, transaction.tx); err != nil {
			return err
		}
		return callback(ctx, transaction, proof)
	})
}

func (repository *PostgresRecordPlatformRepository) runSourceAuthorizedMutationTransaction(ctx context.Context, callback RecordPlatformTransactionCallback) error {
	if callback == nil {
		return errors.New("source-authorized mutation callback is nil")
	}
	return repository.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := lockAssetGraphForSync(ctx, transaction.tx); err != nil {
			return err
		}
		return callback(ctx, transaction)
	})
}

func (repository *PostgresRecordPlatformRepository) runRecordPlatformTransactionAttempt(ctx context.Context, options pgx.TxOptions, callback RecordPlatformTransactionCallback) error {
	tx, err := repository.startTransactionWithOptions(ctx, options)
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
	if err := transaction.requireIdempotencyScope(ctx, input.Key); err != nil {
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
	if err := transaction.requireIdempotencyScope(ctx, key); err != nil {
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
	if err := transaction.requireIdempotencyScope(ctx, input.Key); err != nil {
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
	if err := transaction.requireIdempotencyScope(ctx, key); err != nil {
		return err
	}
	return releaseIdempotencyInTransaction(ctx, transaction.tx, key, owner)
}

// EnqueueOutbox inserts an identity-only delivery event in the same
// transaction as the business fact. Its worker must reauthorize the subject
// before every later delivery attempt.
func (transaction *RecordPlatformTransaction) EnqueueOutbox(ctx context.Context, input recordplatform.OutboxEnqueueInputV1) (recordplatform.OutboxEventRecordV1, error) {
	if transaction == nil || transaction.tx == nil || transaction.repository == nil {
		return recordplatform.OutboxEventRecordV1{}, recordplatform.ErrInvalidOutboxEnqueue
	}
	if err := input.Validate(); err != nil {
		return recordplatform.OutboxEventRecordV1{}, err
	}
	recordID, err := transaction.requireAcquiredOutboxRoute(ctx, input.Event)
	if err != nil {
		return recordplatform.OutboxEventRecordV1{}, fmt.Errorf("enqueue outbox record route: %w", err)
	}

	command := outboxEventCommand(input.Event, recordID)
	command["expires_after_us"] = input.ExpiresAfter.Microseconds()
	body, err := json.Marshal(command)
	if err != nil {
		return recordplatform.OutboxEventRecordV1{}, fmt.Errorf("encode outbox enqueue: %w", err)
	}
	var rowID int64
	if err := transaction.tx.QueryRow(ctx, `select outbox_row_id from public.record_outbox_enqueue($1::bytea)`, body).Scan(&rowID); err != nil {
		return recordplatform.OutboxEventRecordV1{}, fmt.Errorf("enqueue outbox event: %w", err)
	}
	if rowID <= 0 {
		return recordplatform.OutboxEventRecordV1{}, fmt.Errorf("%w: outbox row id", recordplatform.ErrInvalidOutboxEvent)
	}
	currentRecordID, err := transaction.requireAcquiredOutboxRoute(ctx, input.Event)
	if err != nil && !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		return recordplatform.OutboxEventRecordV1{}, fmt.Errorf("recheck outbox enqueue route: %w", err)
	}
	if err != nil || currentRecordID != recordID {
		return recordplatform.OutboxEventRecordV1{}, &pgconn.PgError{Code: "40001", Message: "outbox enqueue route changed; restart transaction"}
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
	recordID, err := transaction.requireAcquiredOutboxRoute(ctx, claim.Event)
	if err != nil {
		return fmt.Errorf("assert outbox record route: %w", err)
	}
	if err := lockRecordMutationProtectedRow(ctx, transaction.tx, map[string]any{"family": "outbox", "outbox_row_id": claim.Event.RowID}); err != nil {
		return fmt.Errorf("lock outbox projection owner: %w", err)
	}
	var present int
	err = transaction.tx.QueryRow(ctx, `
		select 1
		from public.record_outbox
		where outbox_row_id = $1
		  and status = 'processing'
		  and owner_id = $2
		  and owner_generation = $3
		  and owner_expires_at = $4
		  and project_id = $5
		  and event_kind = $6
		  and subject_kind = $7
		  and subject_id = $8
		  and source_version = $9
		  and authorization_epoch = $10
		  and record_fence_epoch = $11
		  and expires_at = $12
		  and (record_id is null or record_id = $13)
		  and owner_expires_at > clock_timestamp()
		  and expires_at > clock_timestamp()`,
		claim.Event.RowID, claim.Owner.OwnerID, claim.Owner.Generation, claim.Owner.ExpiresAt,
		claim.Event.ProjectID, claim.Event.EventKind, claim.Event.SubjectKind, claim.Event.SubjectID,
		claim.Event.SourceVersion, claim.Event.AuthorizationEpoch, claim.Event.RecordFenceEpoch, claim.ExpiresAt, recordID,
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
	var live bool
	if err := transaction.tx.QueryRow(ctx, `select owner_expires_at > clock_timestamp() and expires_at > clock_timestamp()
		from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&live); err != nil {
		return fmt.Errorf("recheck outbox owner after lock: %w", err)
	}
	if !live {
		return recordplatform.ErrLostOwnerLease
	}
	currentRecordID, err := transaction.requireAcquiredOutboxRoute(ctx, claim.Event)
	if err != nil && !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		return fmt.Errorf("recheck outbox claim route: %w", err)
	}
	if err != nil || currentRecordID != recordID {
		return &pgconn.PgError{Code: "40001", Message: "outbox claim route changed; restart transaction"}
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
	var ceiling int64
	if err := repository.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		return transaction.tx.QueryRow(ctx, `select coalesce(max(outbox_row_id),0) from public.record_outbox`).Scan(&ceiling)
	}); err != nil {
		return nil, err
	}
	var after int64
	for after < ceiling {
		var candidates []int64
		err := repository.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			candidates = nil
			rows, err := transaction.tx.Query(ctx, `select outbox_row_id from public.record_outbox
				where outbox_row_id>$1 and outbox_row_id<=$2 and expires_at>clock_timestamp()+($3 * interval '1 microsecond')
				  and ((status='pending' and next_attempt_at<=clock_timestamp()) or
				       (status='processing' and owner_expires_at<=clock_timestamp()))
				order by outbox_row_id limit 128`, after, ceiling, input.OwnerLeaseDuration.Microseconds())
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err != nil {
					return err
				}
				candidates = append(candidates, id)
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return nil, nil
		}
		for _, id := range candidates {
			after = id
			var claim *recordplatform.ClaimedOutboxEventV1
			err := repository.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
				claim = nil
				var err error
				claim, err = claimOutboxInTransaction(ctx, transaction, input, id)
				return err
			})
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.Is(err, records.ErrRecordDeletionReserved) || errors.Is(err, recordplatform.ErrLostOwnerLease) ||
					errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) ||
					(errors.As(err, &pgErr) && (pgErr.Code == "55P03" || pgErr.Code == "40001" || pgErr.Code == "55000")) {
					continue
				}
				return nil, err
			}
			if claim != nil {
				return claim, nil
			}
		}
	}
	return nil, nil
}

// CancelOutbox fences an undeliverable event with the exact committed owner.
func (repository *PostgresRecordPlatformRepository) CancelOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	return repository.finalizeOutbox(ctx, claim, outboxCancel, 0)
}

// RetryOutbox releases a still-live claim back to pending with a database-time retry point.
func (repository *PostgresRecordPlatformRepository) RetryOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1, retryAfter time.Duration) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	if retryAfter.Microseconds() <= 0 {
		return fmt.Errorf("%w: retry delay", recordplatform.ErrInvalidOutboxClaim)
	}
	return repository.finalizeOutbox(ctx, claim, outboxRetry, retryAfter)
}

// MarkOutboxSent records a successful delivery only while the exact owner is live.
func (repository *PostgresRecordPlatformRepository) MarkOutboxSent(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) error {
	if err := claim.Validate(); err != nil {
		return err
	}
	return repository.finalizeOutbox(ctx, claim, outboxSent, 0)
}

type outboxFinalization uint8

const (
	outboxCancel outboxFinalization = iota
	outboxRetry
	outboxSent
)

func (repository *PostgresRecordPlatformRepository) finalizeOutbox(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1, operation outboxFinalization, delay time.Duration) error {
	return repository.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		candidate, recordID, err := acquireOutboxFinalizationRoute(ctx, transaction, claim, operation != outboxRetry)
		if err != nil {
			return err
		}
		body, err := outboxCandidateCommand(candidate, recordID, "", delay)
		if err != nil {
			return fmt.Errorf("encode outbox finalization: %w", err)
		}
		var row pgx.Row
		switch operation {
		case outboxCancel:
			row = transaction.tx.QueryRow(ctx, `select outbox_row_id from public.record_outbox_cancel($1::bytea)`, body)
		case outboxRetry:
			row = transaction.tx.QueryRow(ctx, `select outbox_row_id from public.record_outbox_retry($1::bytea)`, body)
		case outboxSent:
			row = transaction.tx.QueryRow(ctx, `select outbox_row_id from public.record_outbox_sent($1::bytea)`, body)
		default:
			return recordplatform.ErrInvalidOutboxClaim
		}
		var rowID int64
		if err := row.Scan(&rowID); errors.Is(err, pgx.ErrNoRows) {
			return recordplatform.ErrLostOwnerLease
		} else if err != nil {
			return fmt.Errorf("finalize outbox row: %w", err)
		}
		if rowID != claim.Event.RowID {
			return recordplatform.ErrLostOwnerLease
		}
		return nil
	})
}

func (repository *PostgresRecordPlatformRepository) PeekCompletedIdempotency(
	ctx context.Context,
	key recordplatform.IdempotencyKey,
) (bool, error) {
	if err := key.Validate(); err != nil {
		return false, err
	}
	tx, err := repository.startTransaction(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.admit(ctx, tx); err != nil {
		return false, err
	}
	var status string
	err = tx.QueryRow(ctx, `
		select status
		from public.record_idempotency_keys
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and status = 'completed'
		  and expires_at > transaction_timestamp()`,
		string(key.ProjectID),
		string(key.OperationKind),
		key.Key,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("peek completed idempotency: %w", err)
	}
	return status == string(recordplatform.IdempotencyStatusCompleted), nil
}

// ClaimIdempotency creates, replays, or takes over one idempotency row while
// holding its serialization scope and using fresh database time after waits.
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
	if err := lockRecordIdempotencyScope(ctx, tx, input.Key); err != nil {
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
	if err := lockRecordIdempotencyScope(ctx, tx, key); err != nil {
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
	if err := lockRecordIdempotencyScope(ctx, tx, input.Key); err != nil {
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
	if err := lockRecordIdempotencyScope(ctx, tx, key); err != nil {
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

func lockIdempotencyRow(ctx context.Context, tx pgx.Tx, key recordplatform.IdempotencyKey) error {
	_, err := tx.Exec(ctx, `select 1 from public.record_idempotency_keys
		where project_id=$1 and operation_kind=$2 and idempotency_key=$3 for update`,
		key.ProjectID, key.OperationKind, key.Key)
	if err != nil {
		return fmt.Errorf("lock idempotency row: %w", err)
	}
	return nil
}

func completeIdempotencyInTransaction(ctx context.Context, tx pgx.Tx, key recordplatform.IdempotencyKey, owner recordplatform.OwnerLease, result recordplatform.RequestFingerprintV1) error {
	persisted, err := result.PersistedBytes()
	if err != nil {
		return fmt.Errorf("encode completed idempotency fingerprint: %w", err)
	}
	if err := lockIdempotencyRow(ctx, tx, key); err != nil {
		return err
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
		  and owner_expires_at > clock_timestamp()`,
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
	if err := lockIdempotencyRow(ctx, tx, input.Key); err != nil {
		return recordplatform.OwnerLease{}, err
	}
	owner, err := scanIdempotencyOwner(tx.QueryRow(ctx, `
		with tick as materialized (select clock_timestamp() as now)
		update public.record_idempotency_keys
		set owner_expires_at = tick.now + ($5 * interval '1 microsecond'),
		    expires_at = tick.now + ($6 * interval '1 microsecond')
		from tick
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and status = 'in_progress'
		  and owner_id = $4
		  and owner_generation = $7
		  and owner_expires_at = $8
		  and owner_expires_at > tick.now
		  and expires_at > tick.now + ($5 * interval '1 microsecond')
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
	if err := lockIdempotencyRow(ctx, tx, key); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		with tick as materialized (select clock_timestamp() as now)
		update public.record_idempotency_keys
		set owner_expires_at = tick.now
		from tick
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and status = 'in_progress'
		  and owner_id = $4
		  and owner_generation = $5
		  and owner_expires_at = $6
		  and owner_expires_at > tick.now`,
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
	if err := lockIdempotencyRow(ctx, tx, input.Key); err != nil {
		return recordplatform.IdempotencyClaimResultV1{}, err
	}
	row := observedIdempotencyRow{}
	err := tx.QueryRow(ctx, `
		select request_fingerprint,
		       result_fingerprint,
		       status,
		       owner_id,
		       owner_generation,
		       owner_expires_at,
		       expires_at,
		       clock_timestamp()
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
		with tick as materialized (select clock_timestamp() as now)
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
		)
		select $1, $2, $3, $4, 'in_progress', $5, 1,
			tick.now + ($6 * interval '1 microsecond'),
			tick.now + ($7 * interval '1 microsecond')
		from tick where true
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
		with tick as materialized (select clock_timestamp() as now)
		update public.record_idempotency_keys
		set owner_id = $4,
		    owner_generation = owner_generation + 1,
		    owner_expires_at = tick.now + ($5 * interval '1 microsecond'),
		    expires_at = tick.now + ($6 * interval '1 microsecond')
		from tick
		where project_id = $1
		  and operation_kind = $2
		  and idempotency_key = $3
		  and request_fingerprint = $7
		  and status = 'in_progress'
		  and owner_expires_at <= tick.now
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
	return repository.startTransactionWithOptions(ctx, pgx.TxOptions{})
}

func (repository *PostgresRecordPlatformRepository) startTransactionWithOptions(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	if repository == nil || repository.beginTx == nil {
		return nil, fmt.Errorf("begin record platform transaction: %w", ErrRecordPlatformAdmissionUnavailable)
	}
	tx, err := repository.beginTx(ctx, options)
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
