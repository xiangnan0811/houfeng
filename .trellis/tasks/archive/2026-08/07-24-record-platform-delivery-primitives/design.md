# Record platform delivery primitives — Design

## 1. Boundary

```text
future records/deletion service
  └─ one repository transaction
       ├─ owned business-fact callback
       ├─ idempotency claim/final result
       └─ outbox identity row

outbox worker
  claim + commit → authorize current resource → render/send outside tx
              → fenced sent/retry/cancel transaction

guard / leases
  acquire or expired takeover → LeaseToken(owner, generation, DB expiry)
  renew/release/finalize only with the same live token
```

`recordplatform` owns pure closed contracts and worker orchestration. `store` owns pgx/SQL and a transaction seam. A later business service owns actual facts and supplies a callback that writes them through the same transaction. Neither layer owns deletion ledger/witness state, deployment membership, HTTP routing, payload rendering or network transports.

## 2. Canonical non-secret request identity

`DeletionRequestTokenV1` is a value object, not a stored credential. Its parser accepts exactly `drt1_` plus 43 base64url-no-padding characters and decodes exactly 32 bytes; generation reads exactly 32 bytes from `crypto/rand`. Its raw bytes exist only in the request/process lifetime. The persisted identity is a 32-byte commitment:

```text
SHA-256("houfeng-deletion-request-token-v1" || NUL || deploymentID || NUL || projectID || NUL || rawToken)
```

The only request fingerprint input is a closed v1 body:

```text
version | operation-kind | project-id | actor-scope-digest | request-scope-digest | payload-digest
```

Each variable field is length-prefixed; digest fields are exactly 32 bytes. The codec rejects unknown version, invalid IDs, nil/short data and trailing bytes. It hashes the body with SHA-256. This lets later business owners protect arbitrary canonical payloads through a digest without storing content or using a map/JSON serializer here.

## 3. Fencing and idempotency

Every acquired durable owner is represented by:

```go
type OwnerLease struct {
    OwnerID     string
    Generation  uint64
    ExpiresAt   time.Time // observed DB value, never a client authority
}
```

Acquire/takeover increments the row generation; a first owner gets generation 1. The store makes all writes against database time (`transaction_timestamp()`), not a caller clock. Renewal, terminal finalization and release use all three fields plus the applicable live-expiry column (`owner_expires_at` for outbox/idempotency/reservation; `expires_at` for guard/lease) being later than `transaction_timestamp()`. A zero affected-row result becomes a typed stale/lost-lease error and must not lead to a compensating update.

Idempotency is deliberately immutable on a fingerprint mismatch. A matching completed row returns its recorded 32-byte result fingerprint. A matching active row is owned or may be taken over only after its DB expiry. A distinct fingerprint returns `ErrIdempotencyKeyReused` without changing the first caller's status, fingerprint, owner or expiry. An observed inherited `status='conflict'` row is a fail-closed read error; that status is not used as a destructive overwrite of a replayable row. On creation/takeover, the selected idempotency expiry is strictly after its owner expiry; completion requires a result fingerprint and clears the active owner fields.

## 4. Transaction and lock contract

The store exposes an owned transaction callback, not a generic network hook. A future service invokes it to perform its business-fact insert/update, idempotency state change and outbox enqueue atomically. The callback may only use the transaction-scoped store methods. If it returns an error or the commit fails, all three writes roll back.

When more than one primitive must be locked, the repository acquires relation locks in this exact order:

```text
record_idempotency_keys
identity_mutation_guards
deletion_reservations
content_delivery_epochs
deletion_fence_leases
object_content_leases
client_content_leases
record_outbox
```

Within one relation, its key is canonicalized then sorted lexicographically before any `FOR UPDATE`/upsert. Every claim/finalize accepts an injected `AdmissionGate` and calls it inside that same transaction before touching a primitive. Its store-side adapter receives the active transaction and binds an immutable membership identity through construction, never from an outbox event/payload. The gate is intentionally abstract in Child 1; nil/error means no claim/finalization/send. Task 10 will supply its membership query/writer/heartbeat lifecycle, rather than this slice inventing a second implementation.

## 5. Outbox and worker

An outbox row contains only project, event kind, subject kind/ID, authorization epoch, retry/expiry and owner fencing fields. `ClaimOutbox` atomically selects a due pending row or an expired processing row, increments generation, records a new DB-time expiry and returns the identity record. It does not return content.

`OutboxWorker` has two injected post-claim dependencies:

```go
type FreshOutboxAuthorizer interface {
    AuthorizeAndRender(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error)
}
type OutboxSender interface { SendOutbox(context.Context, RenderedDelivery) error }
```

After a claim transaction commits, the worker authorizes/renders the just-read identity; only an allowed decision whose current epoch exactly equals `OutboxEvent.AuthorizationEpoch` may reach the sender. Deny, epoch mismatch or missing handler invokes fenced cancel. It invokes the sender only after authorization succeeds and outside every store transaction. Sender/temporary authorization failure computes a next retry time then uses fenced retry; success uses fenced sent. The worker repeats authorization for every new claim/retry, so an authorization decision is never persisted or reused. A stale finalizer gets a lost-lease result and stops. No new generic recordauth capability is invented here: the future event owner supplies its named authorization implementation.

## 6. Guards, leases and content epoch boundary

Identity guards and the deletion-fence/object/client lease tables use a common repository algorithm: acquire empty/expired row with next generation, renew only while the same DB-time owner is live, release only with the same owner triple. The in-memory worker clock schedules renewal before expiry but cannot extend authority locally; a failed renew makes it stop work before its observed expiry.

There is no `serving_leases` table. `ServingLeaseV1` is an in-memory compound token consisting of a live object-content lease and captured content-delivery epoch. It is valid only when there is no live deletion-fence lease and the stored epoch is exactly the captured value. A client lease has no object key in the schema, so it cannot independently prove object drain or authorize serving/purge.

`content_delivery_epochs` is the per-object pivot and must exist at epoch 0 before primitives use it; a missing row is a refusal, never a lazy reset. A bounded reservation-fence operation accepts only a live, unexpired `previewed` reservation, locks `deletion_reservations → content_delivery_epochs → deletion_fence_leases → object_content_leases`, rejects a live object lease, computes one new object-global generation, atomically increments the epoch and writes that same owner/generation to the reservation and deletion-fence lease while entering `fenced` with `deletion_reservations.fence_epoch` equal to the new epoch. It exposes only fence/renew/release/assert operations. The later ledger owner alone maps the reservation to committed/not-committed/audited outcomes.

## 7. Failure and compatibility rules

All public invalid inputs produce sentinels that callers inspect with `errors.Is`; database errors are context-wrapped with `%w`. No production panic, raw token, request content or stable subject ID appears in ordinary logging. There is no bootstrap registration, so a worker type can be tested but is not started in production. Existing non-record behavior and the flags/admission boundary stay untouched.
