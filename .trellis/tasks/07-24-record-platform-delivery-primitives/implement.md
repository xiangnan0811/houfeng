# Record platform delivery primitives — Implementation Plan

> Active task: `.trellis/tasks/07-24-record-platform-delivery-primitives`
>
> **For agentic workers:** One implementation worker owns this entire isolated slice. It must use `superpowers:test-driven-development`: every production behavior starts with a focused test that is observed failing, then minimal implementation, then a focused green run. Do not edit the frozen legacy worktree, `0051`, ACL/admission, bootstrap, ledger/witness/recovery, or Child 2–11 paths.

## Scope and ownership

Create:

- `internal/center/recordplatform/types.go`
- `internal/center/recordplatform/idempotency.go`
- `internal/center/recordplatform/outbox.go`
- `internal/center/recordplatform/guards.go`
- `internal/center/recordplatform/leases.go`
- `internal/center/recordplatform/worker.go`
- matching `internal/center/recordplatform/*_test.go`
- `internal/center/store/record_platform.go`
- `internal/center/store/record_platform_test.go`
- `internal/center/store/record_platform_postgres_integration_test.go`

Modify only task artifacts and applicable backend specs if implementation reveals a durable new convention. Do not introduce a migration, sender implementation, API route, concrete membership gate SQL/lifecycle or deletion/ledger outcome transition.

## Required contract names

Keep these names and responsibilities consistent across the files; add fields only when a RED test requires them:

```go
// recordplatform/types.go
type OwnerLease struct { OwnerID string; Generation uint64; ExpiresAt time.Time }
type IdempotencyKey struct { ProjectID, OperationKind, Key string }
type OutboxEvent struct { RowID int64; ProjectID, EventKind, SubjectKind, SubjectID string; AuthorizationEpoch uint64 }
type ObjectRef struct { ProjectID, ObjectKind, ObjectID string }
type ContentEpoch uint64

// recordplatform/worker.go — neither value is persisted by the worker.
type FreshOutboxAuthorizer interface {
    AuthorizeAndRender(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error)
}
type OutboxSender interface { SendOutbox(context.Context, RenderedDelivery) error }
```

`store/record_platform.go` owns the pgx-specific transaction interface and an injected `AdmissionGate` adapter. It binds membership identity in the repository constructor, invokes the gate with the same transaction before every claim/finalize, and treats a nil/error gate as a fail-closed error. It does not copy Task 10 membership SQL/types into `recordplatform`.

## Ordered TDD plan

### Task 1: Lock pure identifiers, token and fingerprint contracts

1. Add RED tests for exact `drt1_` transport: 32 random bytes round-trip, 43-character no-padding payload, wrong prefix/alphabet/length, trailing bytes, non-canonical encoding and cross deployment/project commitment mismatch. Capture a golden commitment and assert raw token never appears in returned persistence inputs or error text.
2. Add RED table tests for a length-prefixed v1 request fingerprint. The body must contain exactly version, operation kind, project ID, actor-scope digest, request-scope digest and payload digest; reordered/changed fields create a different digest, while identical fields reproduce the same one. Unknown version, invalid IDs, non-32-byte digests and trailing input fail.
3. Run:

   ```sh
   go test ./internal/center/recordplatform -run 'DeletionRequestToken|RequestFingerprint' -count=1
   ```

   Confirm RED is due to absent symbols, not an unrelated fixture failure.
4. Implement the smallest closed types/codecs using `crypto/rand`, `base64.RawURLEncoding`, SHA-256 and explicit length-prefix helpers. Define sentinels for invalid input/token/fingerprint and use no map/JSON serialization.
5. Re-run the selector until green and run `gofmt` on only the created package files.

### Task 2: Define and prove owner-generation idempotency semantics

1. Add pure contract RED tests for `OwnerLease`, same-key same-fingerprint replay, distinct-fingerprint immutable conflict, inherited/unknown `status='conflict'` fail-closed read, live-owner refusal, expired takeover from generation 1 to 2, result fingerprint requirement, owner expiry ordering and stale owner rejection.
2. Add store RED tests using the existing injected `beginTx`/fake-pgx style. Assert that the injected `AdmissionGate` is called inside the same transaction before create/takeover/complete; a nil/error gate makes zero primitive writes. Assert that create/takeover, renew and complete SQL contain the owner ID, generation and `owner_expires_at > transaction_timestamp()` fence; assert completion clears owner fields and does not accept a missing result fingerprint. Assert a zero affected-row command maps to the public lost-lease sentinel.
3. Run:

   ```sh
   go test ./internal/center/recordplatform ./internal/center/store \
     -run 'Idempotency|OwnerLease' -count=1
   ```

4. Implement closed idempotency input/result types and `PostgresRecordPlatformRepository` transaction methods. Creation/takeover must assign a strictly later idempotency expiry than the selected owner expiry. A mismatched fingerprint returns `ErrIdempotencyKeyReused` without issuing an UPDATE against the established row; an observed schema `conflict` state is read-only fail-closed.
5. Re-run the selector until green. Do not use table status `conflict` to destroy a replayable row.

### Task 3: Add atomic business/idempotency/outbox persistence seam

1. Add RED store tests showing one repository transaction first invokes an injected `AdmissionGate`, then invokes an owned business-fact callback, records/updates the idempotency state and enqueues the outbox identity row, then commits once. Gate/callback/SQL/commit failures separately roll back and create no logical result; nil/error gate means zero claim/finalization.
2. Add RED validation tests for closed outbox event kind/subject kind/subject ID/authorization epoch/expiry inputs. The repository API must accept an event identity, not body, recipient, renderer or network sender.
3. Run:

   ```sh
   go test ./internal/center/store ./internal/center/recordplatform \
     -run 'RecordPlatform.*Transaction|Outbox.*Enqueue' -count=1
   ```

4. Implement the transaction callback seam and append-only outbox enqueue on the existing `record_outbox` table. Retain the relation lock order from the design and document it beside the transaction-scoped methods. Roll back on every callback/write/commit error.
5. Re-run the selector until green; inspect the diff to ensure no network interface crossed into `store`.

### Task 4: Implement fenced outbox claim and post-commit worker flow

1. Add RED repository tests for a same-transaction `AdmissionGate` before due pending claim, expired-processing takeover, non-expired/sent/cancelled/expired skip, increasing generation, and owner-triple SQL on sent/retry/cancel finalizers. Add an old-owner test in which a new claim wins and every old finalizer affects zero rows.
2. Add RED `OutboxWorker` fake-clock tests for the exact flow: claim commits before fresh authorize/render; only allowed + matching current epoch calls sender; deny, mismatch and missing handler cancel without send; transient auth/render/sender error schedules retry; retry invokes authorizer again; sender is never called while a store transaction is active; stale finalization stops without a second write.
3. Run:

   ```sh
   go test ./internal/center/recordplatform ./internal/center/store \
     -run 'Outbox(Claim|Worker|Fence|Retry|Authorization)' -count=1
   ```

4. Implement `FreshOutboxAuthorizer`, `OutboxSender`, `OutboxWorker` and repository claim/finalization methods. Keep rendered content out of persisted structures and make every terminal/retry update verify DB-time owner triple fencing.
5. Re-run the selector until green. Do not register the worker in bootstrap in this task.

### Task 5: Implement identity guard and three lease primitives

1. Add RED table tests for guard, deletion-fence, object-content and client-content lease acquisition: the injected gate runs first in the same transaction; one active claimant wins; expired takeover increments generation; wrong owner/generation/expiry cannot renew or release; a local renewal failure makes the worker-side helper stop before its observed expiry.
2. Add RED ordering tests that submit multiple keys in reverse order and assert the repository canonicalizes/sorts keys before acquiring rows. Cover the fixed relation order from the design rather than relying on call order.
3. Add RED content epoch/reservation-fence tests: an absent epoch fails closed; serving permit requires live object lease + matching captured epoch + no live deletion fence; lock `reservation → epoch → deletion-fence lease → object lease`, reject a live object lease, increment epoch and bind reservation `fence_epoch`; client lease alone cannot authorize an object-specific permit. Do not implement committed/not-committed, audit, purge operation or ledger proof behavior.
4. Run:

   ```sh
   go test ./internal/center/recordplatform ./internal/center/store \
     -run 'Guard|Lease|ContentDeliveryEpoch|LockOrder' -count=1
   ```

5. Add RED cleanup tests: each expired-row delete predicate excludes every row with a live owner/lease and returns no raw token/content. The cleanup API may only handle expired primitive rows; it must not delete reservation/operation/ledger evidence that has a later owner.
6. Implement common validation/token helpers, separate SQL methods for the existing three tables, bounded reservation fence and conservative primitive cleanup. Represent serving as an in-memory `ServingLeaseV1`, never as a fabricated persistent table. All renew/release paths require `owner_id`, generation and live DB-time expiry.
7. Re-run the selector until green.

### Task 6: Prove real PostgreSQL fencing and atomicity

1. Add real PostgreSQL tests through the existing record-platform integration wrapper. First set up r1 only through the scoped migrator/runtime fixture; never bypass runtime ACL with owner credentials.
2. Prove concurrent same-key idempotency/outbox/lease claim has one winner, expired takeover increments generation, old owner finalization returns zero rows, business/idempotency/outbox writes are atomic on injected failure, reservation/epoch/object-lease fencing serializes, and no raw token appears in stored rows. Use fixture membership rows only as input to the injected gate; do not implement Task 10 production membership lifecycle.
3. Run:

   ```sh
   scripts/test-record-platform-integration.sh postgres -- \
     go test -v ./internal/center/store \
     -run 'TestPostgresIntegrationRecordPlatform(.*Idempotency|.*Outbox|.*Lease|.*Atomic)' -count=1
   ```

   Treat any `--- SKIP:` as non-evidence and report it instead of weakening the test.
4. Keep integration fixture setup/release cleanup correct; no temporary role/database/container may survive the selector.

### Task 7: Review, quality gates and commit

1. Run focused tests, then concurrency repetition:

   ```sh
   go test -race ./internal/center/recordplatform ./internal/center/store \
     -run 'RecordPlatform|Idempotency|Outbox|Guard|Lease' -count=10
   gofmt -w internal/center/recordplatform/*.go internal/center/store/record_platform.go internal/center/store/record_platform_test.go internal/center/store/record_platform_postgres_integration_test.go
   git diff --check
   GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 make verify-go
   ```

2. Update a backend spec only if a production contract proved by code needs future preservation; do not write aspirational details as current code facts.
3. Request a spec-compliance/security review first. Fix every P0/P1/P2, re-run affected RED/GREEN evidence, then request a code-quality review. Do not start the second review until the first approves.
4. Stage only this task's owned files/artifacts and commit on `codex/vps-records-platform-delivery-primitives`. Do not push, open a PR, merge, archive parent/child tasks or admit Child 2–11.

## Completion boundary

This slice provides reusable ownership/fencing and post-commit delivery primitives. It does not complete PF-AC-003 until a later real records/deletion service uses the transaction seam and the Task 10 membership gate is applied to every claim/finalize path.
