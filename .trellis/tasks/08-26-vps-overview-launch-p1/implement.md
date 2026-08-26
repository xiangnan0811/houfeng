# VPS overview launch P1 delivery plan

## Phase 1 — Reconcile and document

- [x] Reconcile the dirty branch against repeated Critical/Important/Minor
  reviews and current source rather than relying on handoff claims.
- [x] Record requirements and technical decisions in this new task without
  changing archived Trellis designs.

## Phase 2 — Backend lifecycle and overview

- [x] Harden preview digest, ordinary lifecycle writes and terminal read-only
  UPDATE predicates.
- [x] Implement serializable cancellation retries, typed 409 exhaustion and
  failed-audit separation.
- [x] Add transaction cut-point unit tests and deterministic real PostgreSQL
  overlap/no-deadlock coverage with safe cleanup.
- [x] Extend overview anomaly/relation presentation contracts and goldens.

## Phase 3 — Web ownership and destinations

- [x] Enforce terminal archive routing and lifecycle-specific write surfaces.
- [x] Make modern and legacy cancellation preview/apply state generation-safe.
- [x] Harden events, monitoring, relations and internal destination allowlists.
- [x] Add native Tab and exact runtime-stream Playwright contracts.

## Phase 4 — Verification and review

- [x] `GOTOOLCHAIN=go1.26.2 GOFLAGS='-p=1' make verify-go`.
- [x] Real PostgreSQL cancellation/subscription overlap regression.
- [x] Node 22 `make verify-web`: ESLint, 199 files / 1404 Vitest tests,
  TypeScript, production build, bundle and CSS budgets.
- [x] Node 22 Playwright: 129/129 Chromium tests.
- [x] Comprehensive read-only review: no remaining Critical or Important; both
  actionable P3 findings fixed and reverified.

## Phase 5 — Delivery and cleanup

- [x] Commit task metadata and implementation in reviewable batches.
- [ ] Push and open the protected-main pull request.
- [ ] Resolve review and required CI on the same branch; merge when green.
- [ ] Verify main CI, Release Please, GitHub release and container publication.
- [ ] Archive this task, record the session journal, sync the primary checkout
  and remove stale task branch/worktree state without touching protected main
  directly.
