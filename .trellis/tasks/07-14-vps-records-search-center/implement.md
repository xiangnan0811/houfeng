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

- [ ] Required dependencies are accepted on protected main.
- [ ] Run `trellis-before-dev` for backend database/http and Web component/state/
  quality guidance.
- [ ] Confirm `0056` is free and current APP ACL fragment APIs exist.
- [ ] Baseline Go/Web/global-search tests with Node 22.

## Task 1: Query, normalization, and cursor domain

- [ ] Write RED tables for Unicode/Markdown normalization, every filter,
  same-field OR/cross-field AND, time zones/ranges, sort, page bounds, and
  cursor tamper/mismatch/expiry.
- [ ] Implement immutable query values and opaque signed cursor codec.
- [ ] Run focused deterministic/race tests.

## Task 2: 0056 schema and current ACL fragment

- [ ] Write migration source/real PostgreSQL RED tests for tables, constraints,
  indexes, extensions, generation state, and no content duplication.
- [ ] Implement `0056` and its exact managed objects/privileges.
- [ ] Run fresh/repeat migration and current convergence/admission tests.

## Task 3: Transaction projection and rebuild

- [ ] Test revision participant order/rollback, archive/restore/delete, comment
  redaction, visibility/source-floor change, and import.
- [ ] Implement current document projector using shared Markdown plaintext.
- [ ] Test shadow rebuild, concurrent commits, crash/resume, hash/count coverage,
  publish CAS, and stale generation rejection.
- [ ] Implement worker/health/bootstrap and recovery rebuild adapter.

## Task 4: Store, handler, and performance

- [ ] Write query/authorization/response/cursor RED matrix and representative
  EXPLAIN fixtures.
- [ ] Implement scoped SQL query, counts/facets, snippets, and bounded global
  result endpoint.
- [ ] Prove no unauthorized count/snippet/facet/log leakage and reviewed query
  plans/latency.

## Task 5: Records and Drafts Web routes

- [ ] Add canonical query codec and lazy API facade methods.
- [ ] Implement Records/Drafts pages, filters, pagination, loading/first-empty/
  no-results/local-error/revoked states.
- [ ] Test deep links, back/forward, keyboard, focus, desktop/390px, Axe, and
  bundle boundaries.

## Task 6: Global search integration

- [ ] Replace browser full-dataset behavior only for the Records group with the
  bounded server endpoint.
- [ ] Test request cancellation, auth revoke, error isolation, result limit, and
  link to canonical `/records` query.
- [ ] Keep unrelated global search groups unchanged.

## Task 7: Quality and handoff

- [ ] Run focused race/real PostgreSQL/EXPLAIN and Web tests.
- [ ] Run full Go/Web/browser gates, `git diff --check`, and `trellis-check`.
- [ ] Update implemented search/cursor/projection specs.
- [ ] Merge through protected main and archive before dependent Activity and
  Portability final integration.

## Rollback

Disable routes/projector and rebuild state; authoritative Records remain intact.
Do not down-migrate `0056` or restore legacy search behavior as a compatibility
contract.
