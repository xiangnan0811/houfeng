# Search, Records Center, and Global Search Implementation Plan

> **For agentic workers:** Use the reviewed execution mode and bounded
> RED -> verified RED -> minimal GREEN slices.

**Goal:** Deliver a transactionally current, permission-safe Records search and
usable Records/Drafts/global-search surfaces.

**Architecture:** A `0056` PostgreSQL projection is updated by a revision
participant and can be rebuilt by generation; server queries enforce auth and
opaque cursors; lazy Web routes own URL/state rendering.

**Tech Stack:** Go/pgx/PostgreSQL text search, React/TypeScript, Vitest,
Playwright/Axe.

---

## Preconditions

- [x] Required dependencies are accepted on protected main. See
  `research/current-main-rebaseline-2026-08-18.md` for the baseline, the
  surfaces that already exist, and the reconciled decisions. Read it first.
- [x] Run `trellis-before-dev` for backend database/http and Web component/state/
  quality guidance.
- [x] Confirm `0056` is free and current APP ACL fragment APIs exist. `0056` is
  free; fragments live in
  `internal/center/store/migrate/app_acl_current_contract.go`.
- [x] Baseline Go/Web/global-search tests with Node 22.

## Task 1: Query, normalization, and cursor domain

- [x] Write RED tables for Unicode/Markdown normalization, every filter,
  same-field OR/cross-field AND, time zones/ranges, sort, page bounds, and
  cursor tamper/mismatch/expiry.
- [x] Implement immutable query values and opaque signed cursor codec.
- [x] Run focused deterministic/race tests.

## Task 2: 0056 schema and current ACL fragment

- [x] Write migration source/real PostgreSQL RED tests for tables, constraints,
  indexes, extensions, generation state, and no content duplication.
- [x] Implement `0056` and its exact managed objects/privileges.
- [x] Run fresh/repeat migration and current convergence/admission tests.

## Task 3: Transaction projection and rebuild

- [x] Test revision participant order/rollback, archive/restore/delete, comment
  redaction, visibility/source-floor change, and import.
- [x] Implement current document projector using shared Markdown plaintext.
- [x] Test shadow rebuild, concurrent commits, crash/resume, hash/count coverage,
  publish CAS, and stale generation rejection.
- [x] Implement worker/health/bootstrap and recovery rebuild adapter.

## Task 4: Store, handler, and performance

- [x] Write query/authorization/response/cursor RED matrix and representative
  EXPLAIN fixtures.
- [x] Implement scoped SQL query, counts/facets, snippets, and bounded global
  result endpoint.
- [x] Retire the in-memory `q` / `lifecycle` / `record_type` filter in
  `records/read_service.go` in the same change, moving its tests onto the
  indexed path. Do not leave two search paths.
- [x] Prove no unauthorized count/snippet/facet/log leakage and reviewed query
  plans/latency.

## Task 5: Records and Drafts Web routes

- [x] Add canonical query codec and lazy API facade methods. The codec must not
  become a runtime dependency of `recordsApi.ts`, and every new façade export
  must be added to `EXPECTED_EXPORTS` in the transport contract.
- [x] Add cursor pagination to draft listing; the current endpoint takes only a
  limit and cannot back the Drafts page.
- [x] Implement Records/Drafts pages, filters, pagination, loading/first-empty/
  no-results/local-error/revoked states with zero new CSS.
- [x] Add the sidebar Records entry and the `/records` index route.
- [x] Test deep links, back/forward, keyboard, focus, desktop/390px, Axe, and
  bundle boundaries.

## Task 6: Global search integration

- [x] Add a bounded server-backed Records group to global search. No Records
  group exists there today, so this adds one rather than replacing browser-side
  behavior. Reach the transport by dynamic import so the eager shell contract
  and the entry-chunk budget both keep holding.
- [x] Test request cancellation, auth revoke, error isolation, result limit, and
  link to canonical `/records` query.
- [x] Keep unrelated global search groups unchanged.

## Task 7: Quality and handoff

- [x] Run focused race/real PostgreSQL/EXPLAIN and Web tests.
- [x] Run full Go/Web gates, `git diff --check`, and the bundle/CSS budgets.
- [x] Update implemented search/cursor/projection specs.
  `.trellis/spec/backend/record-search-index-contract.md` records the derived
  projection, generation publish CAS, rebuild lease/checkpoint, digest-bound
  cursor, single search path, and deletion pass-through.
  `.trellis/spec/web/state-and-data.md` records the AppShell dynamic-import
  boundary for the global search records group.
- [ ] Merge through protected main and archive before dependent Activity and
  Portability final integration.

## Rollback

Disable routes/projector and rebuild state; authoritative Records remain intact.
Do not down-migrate `0056` or restore legacy search behavior as a compatibility
contract.
