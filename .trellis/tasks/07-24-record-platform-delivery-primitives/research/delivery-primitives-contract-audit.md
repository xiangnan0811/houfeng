# Delivery primitives contract audit

Date: 2026-07-24

## Source facts

- `db/migrations/0051_create_record_platform_foundation.sql:55-203` supplies the tables and CHECKs but no claim/finalize SQL, lock order, FK between reservation/epoch/lease rows, sender payload or deletion outcome state machine.
- `record_outbox` persists only event identity and `authorization_epoch`. It has no recipient, content, renderer, capability or uniqueness key beyond its generated row ID. The worker therefore must use an injected fresh authorizer/renderer after the claim commits.
- `record_idempotency_keys` has a stable primary key but no CHECK requiring completed result fingerprint or `expires_at > owner_expires_at`; Child 1 enforces both in its store contract. A mismatched fingerprint must not overwrite a legitimate existing row.
- `identity_mutation_guards`, `deletion_fence_leases`, `object_content_leases` and `client_content_leases` all have owner/generation/expiry fields. `content_delivery_epochs` has no default or FK, so it is a required object pivot, never lazily reset by this task.
- `deployment_membership` exists later in the migration, but its durable writer/heartbeat/readiness and concrete admission query belong to parent Task 10. Child 1 accepts an injected same-transaction gate and rejects nil/error gates.

## Required invariants

1. Claims/takeovers increment generations; first owner is generation 1. Every renew/release/finalization compares exact owner ID, generation and the table's live DB-time expiry. Affected rows = 0 means stale/lost authority.
2. Relation lock order is `record_idempotency_keys → identity_mutation_guards → deletion_reservations → content_delivery_epochs → deletion_fence_leases → object_content_leases → client_content_leases → record_outbox`; keys within a relation sort canonically.
3. A reservation fence accepts only a live `previewed` reservation, locks reservation/epoch/fence/object lease in order, rejects a live object lease, advances epoch and records the same new owner/generation/fence epoch on reservation and deletion fence. It does not decide committed/not-committed/audited outcomes.
4. A serving permit is only a live object lease plus exact captured epoch with no live deletion fence. A client lease has no object key and therefore cannot independently prove an object drain or authorize serving/purge.
5. Outbox performs `admission+claim+commit → fresh authorize/render → send outside transaction → admission+fenced finalization`; deny/mismatch/missing handler cancels, temporary error retries, stale finalizer performs no compensating write.

## Verification implications

Use fake pgx transactions for exact SQL/transaction ordering and real PostgreSQL fixture tests for concurrent `SKIP LOCKED` claim, expired takeover, stale zero-row finalization and reservation/epoch/object-lease serialization. A local integration skip is not acceptance evidence.
