# Search, Records Center, and Global Search

## Goal

Deliver permission-safe Records search, project Records/Drafts pages, stable
filters/cursors/URLs, and bounded integration with global search.

## 2026-08-02 Development Rebaseline

This child owns `0056_create_record_search.sql`. Collaboration owns `0055` and
must already be merged. Only fresh/current development databases and exact
repeat startup are supported; the former lower-after-higher migration scheme is
removed.

## Requirements

- Direct dependencies: Platform Foundation, Records Core, Markdown Workspace,
  and Collaboration are accepted on protected main.
- `0056` creates derived search documents, subject links, rebuild/checkpoint
  state, and required indexes/extensions.
- Register `0056` objects/privileges in the current APP ACL fragment.
- The current revision transaction updates the search document atomically
  through a registered participant. Drafts never enter the authoritative record
  search index.
- Rebuild is deterministic, resumable, generation-bound, and safe under
  concurrent revisions. It publishes a generation only after complete coverage.
- Normalize Unicode and Markdown through the shared Markdown plain-text
  contract. Do not index rendered HTML, attachment bytes, forbidden evidence, or
  external notification payload.
- Search authorization combines record visibility, project/group policy, and
  source authorization floor before result fields, counts, snippets, or facets
  are returned.
- Support type/status/status-group/lifecycle, subject, owner/participant,
  follow-up, action, tag, occurred/updated ranges, and query filters with stable
  same-field OR/cross-field AND semantics.
- Use a signed/versioned opaque cursor bound to normalized query, authorization
  namespace, generation, page size, and total sort tuple.
- Records and Drafts routes use canonical query encoding and cover loading,
  first-empty, query-no-results, local failure, revoked/deleted, and normal
  states on desktop/390px.
- Global search requests a bounded Records result group from the server; it does
  not fetch all records into the browser.
- Archive/restore, permanent delete, source deletion, visibility change, comment
  redaction, and import rebuild update or remove indexed content correctly.
- Search is derived and is rebuilt after restore; backup does not treat the
  mutable index/checkpoint as authoritative content.

## Acceptance Criteria

- [ ] `0056` fresh/repeat migration plus current APP ACL/admission tests pass.
- [ ] Revision create/update/archive/restore/delete changes the search document
  in the same transaction or rolls back the complete revision.
- [ ] Draft/comment-redacted/forbidden material never appears in index, snippet,
  count, facet, error, log, or global result.
- [ ] Authorization changes and deletion fences affect list/count/snippet/global
  search without leakage or stale cursor reuse.
- [ ] Filter normalization and URL round trips preserve documented OR/AND,
  inclusive/exclusive range, timezone, and default semantics.
- [ ] Cursor tamper, expiry, query/auth/generation/page-size mismatch, duplicate
  sort values, and concurrent inserts have deterministic behavior.
- [ ] Rebuild from the same authoritative state yields the same content hash and
  complete count, and a failed build never becomes current.
- [ ] Representative Chinese/English/code-token search and PostgreSQL EXPLAIN
  meet reviewed query/index budgets without unbounded scans.
- [ ] Records/Drafts/global search Web states, keyboard, 390px, accessibility,
  route deep links, and lazy-bundle ownership pass.
- [ ] Deletion/recovery adapters and Child 11 rebuild entry point are registered
  and tested.
- [ ] Full focused/Go/Web/browser gates, `git diff --check`, and
  `trellis-check` pass.

## Out of Scope

- External search service or browser-side full dataset indexing.
- Semantic/vector ranking, saved queries, analytics, or public search.
- Searching raw attachment bytes, forbidden evidence, command output, or drafts
  outside the author's Drafts page.
- Legacy `experience_logs` search compatibility.
- Old database upgrade or staging/release cutover.

## Execution Gate

Keep `planning` until Foundation, Core, Markdown, and Collaboration are accepted
on protected main and `0056` is free. Reconcile participant/filter APIs with the
merged dependencies before start.
