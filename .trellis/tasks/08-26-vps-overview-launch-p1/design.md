# VPS overview launch P1 technical design

## Boundaries

This slice closes the launch-critical lifecycle, destination and browser
contracts of the existing VPS overview. It does not redesign the archived VPS
overview architecture, create new lifecycle states, or weaken feature-gate and
authorization boundaries.

## Backend decisions

- `PatchVPSAsset` is an ordinary-write API and validates through
  `ValidateOrdinaryPatchInput`; controlled lifecycle transitions remain owned by
  lifecycle commands.
- Ordinary UPDATE SQL includes the terminal-state predicate so authorization is
  atomic with the write. History writes reuse the already locked `current` row
  when an update affects no rows.
- Cancellation apply owns a complete `SERIALIZABLE` transaction with at most
  three attempts. A shared classifier recognizes PostgreSQL `40001` and
  `40P01` from every transactional operation. Retryable attempts roll back
  without an independent failed audit; exhausted retries become
  `ErrRetryableLifecycleConflict` and HTTP 409.
- `preview_digest` is a canonical SHA-256 over sorted lifecycle, renewal,
  subscription, monitoring, service/domain→target, target and recommended-step
  inputs. Apply re-reads and validates the digest inside the transaction.
- The PostgreSQL regression test uses a third holder transaction and polls the
  isolated database's `pg_stat_activity`. It queues cancellation Apply first,
  observes one active Lock waiter, then queues subscription Create and observes
  two waiters before release. Holder release is idempotent and
  cancellation-aware.
- Lifecycle apply locks shared monitoring instance rows in global
  `monitoring_instance_id` order before applying caller-selected steps, so two
  VPS transactions cannot invert shared-row acquisition order.

## Web decisions

- Terminal lifecycle routing precedes capability routing and uses replace
  navigation to `/archive/:id`; legacy cancellation success also replaces after
  its authoritative refresh reaches `cancelled` or `archived`.
- Cancellation preview/application state is owned by `vpsId:generation` and
  `preview_digest`. Every async continuation, including stale-409 recovery and
  successful detail refresh, verifies ownership before writing state.
- Cancellation/archive commands are lifecycle-specific. Terminal overview and
  legacy surfaces expose no ordinary write affordances.
- Destinations use exact, own-property allowlists. Event object IDs reject dot
  segments and fragments; runtime streams require the raw URL to equal the page
  protocol, host, port and exact owner path before normalized URL checks.
- Vitest proves component event handling; Playwright owns native focus movement,
  exact route and API-owner consumption, runtime WebSocket, same-protocol
  foreign-origin rejection and raw traversal rejection.

## Delivery decisions

- Verify Go with `GOTOOLCHAIN=go1.26.2`; the PNG golden is toolchain-bound.
- Run opt-in PostgreSQL integration with the repository's temporary-database
  runner, Node 22 Web gates and a fresh complete Playwright run.
- Split commits by Trellis/task metadata, backend lifecycle/overview, Web
  behavior and browser fixtures. Merge only through protected `main`, then
  observe Release Please and published release/image evidence before cleanup.
