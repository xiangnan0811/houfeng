# Child 6 current-main rebaseline (2026-08-18)

## Baseline

- Protected `origin/main` at `ed843341`, released as `v0.68.0`
  (PR #413 Child 5, PR #414 release, PR #415 archive; main CI, signed release
  assets and multi-arch image all verified).
- Children 1–5 and 9 are archived on protected main. Parent progress is `6/11`.
- Root migrations end at `0055_create_record_collaboration.sql`. `0056` is free
  and this child owns `0056_create_record_search.sql`.
- Every dependency named in the execution gate (Platform Foundation, Records
  Core, Markdown Workspace, Collaboration) is accepted on protected main.

## Already on this baseline — extend, do not rebuild

- `GET /api/records` already exists with a signed cursor over
  `(updated_at, record_id)` and `q` / `lifecycle` / `record_type` / `sort`
  parameters (`handlers/records.go` `recordListRequestFromHTTP`). But the
  filters are applied **in Go after reading full revisions**
  (`records/read_service.go` `matchesRecordQuery`: `strings.Contains` over
  title, body and tags). There is no `tsvector`, GIN index or `ILIKE` anywhere
  in the records code. This child replaces that path; it must not leave a second
  search path beside the indexed one, and the existing list tests move with it.
- A current-revision projection already exists as denormalized columns on
  `public.records`, written by `updateRecordCurrentProjection` inside the
  revision transaction. `0056` search documents are a new derived artifact, not
  a second copy of that projection, and they join the same transaction through a
  registered participant rather than a second commit.
- `record_search` is already reserved as a deletion/recovery adapter name
  (`recorddeletion/types.go`). Register the real adapter under that existing
  name instead of introducing a new one.
- Subject links are constrained by `0052`: `subject_kind` is exactly
  `vps` / `monitoring_instance` / `target` and `relation_role` is exactly
  `affected` / `context` / `evidence_source`, with one primary per revision.
  Identity snapshots, live-route resolution and witnessed tombstones already
  exist (`store/record_subjects.go`). This child indexes that registry as it is;
  widening kinds or roles belongs to whoever adds those subjects, not here.
- Web already has four lazy record routes (`records/new`, `records/:recordId`,
  `records/:recordId/edit`, `records/:recordId/revisions/:revisionId`) plus
  `record-inbox`. There is **no** `/records` index route and **no** Records
  entry in `Sidebar.tsx`; both are new in this child.
- `GlobalSearch.tsx` exists in the eager shell (mounted by `TopBar`, ⌘K/Ctrl+K)
  and filters client-side over `api.ts` lists for VPS, monitoring instances,
  targets, providers and subscriptions. Records are absent from it. The
  implement-plan wording "replace browser full-dataset behavior only for the
  Records group" therefore describes work that does not exist yet: this child
  **adds** a bounded server-backed Records group and leaves the other groups'
  current client-side behavior untouched.
- `GET /api/record-drafts` is author-scoped with `limit` only — no cursor and no
  per-record filter, a ceiling Child 5 documented as a known limitation. The
  Drafts surface in Task 5 requires adding cursor pagination here.

## Contracts that shape the implementation

1. `web/src/security/recordsTransportArchitectureContract.test.ts`
   - `EXPECTED_EXPORTS` is an exact 24-name list. Every new transport function
     must be added there in the same change, or the contract fails.
   - `recordsApi.ts` runtime imports are exactly `./apiError` and
     `./apiRequest`; only `./types` may be imported type-only. The canonical
     query codec therefore cannot be a runtime dependency of the façade — the
     page owns encoding and passes already-serialized parameters.
   - The eager-graph walk follows only static `import` / `export ... from`
     declarations out of six roots (`main.tsx`, `router.tsx`, `AppShell.tsx`,
     `TopBar.tsx`, `Sidebar.tsx`, `api.ts`). A dynamic
     `await import('../../lib/recordsApi')` inside `GlobalSearch` satisfies both
     this contract and the entry-chunk assertion, and is the sanctioned way to
     give global search a Records group. A static import from the shell would
     break both.
2. Budgets measured on this baseline: entry JS `108984` of `110738`
   (about 1.7KB gzip of headroom), entry CSS `37125` of `37125`, largest async
   `48453` of `48453`, fonts unchanged. Consequences: a sidebar entry plus route
   registration fits the entry-JS headroom; the records center page must ship
   **zero new CSS** and compose from the existing `page.css` / `atoms.css`
   vocabulary, as Child 5's whole workspace did; a new lazy chunk must stay
   under the async ceiling or carry its own written, audited exception.
3. APP ACL fragments live in
   `internal/center/store/migrate/app_acl_current_contract.go` as one function
   per child (`recordsCoreAppACLCurrentMigrationFragment`,
   `recordCollaborationAppACLCurrentMigrationFragment`). This child adds the
   search fragment enumerating `0056` tables and privileges, with convergence
   and admission tests following `record_collaboration_app_acl_test.go`.
4. `HOUFENG_RECORDS_ENABLED` still gates every records route
   (`http/router.go`). New search, facet and global-group endpoints register
   inside that same gate.

## Reconciled decisions

- Global search gets its Records group through a dynamic import of the existing
  lazy transport, not a new eager module and not a static shell import.
- The indexed query replaces the in-memory `q` / `lifecycle` / `record_type`
  filter in this child. One search path, cut over in the same change.
- Draft cursor pagination lands here because the Drafts page is in this child's
  scope and the current limit-only endpoint cannot back it.
- Subject kind/role registry stays exactly as `0052` defines it.
- Text matching uses `pg_trgm` over one normalized lowercase text column, not
  PostgreSQL full-text search. Reason: the UI is Chinese-primary, and the
  built-in `simple` configuration does not segment Chinese, so a whole CJK run
  becomes one lexeme and an infix term like `磁盘` never matches
  `磁盘故障排查`. `pg_trgm` is a trusted core contrib extension and indexes both
  CJK and Latin infix matching through `gin_trgm_ops`, so it accelerates `ILIKE`
  with no operator qualification at the query site. A non-core segmenter such as
  `zhparser` is rejected because it would break the one-command install
  contract.
- Measured on PG 16.12 with the real migration applied and 20,000 documents, so
  the trade is stated in numbers rather than assumed. A term of three or more
  characters uses the index (`%故障排%` → bitmap index scan). A two-character
  term cannot, because `pg_trgm` extracts no full trigram from a pattern shorter
  than three characters, and two characters is the common Chinese term length
  (`%磁盘%` → sequential scan, 21 of 20,000 rows matched, 11 ms end to end
  including sort and limit). The index costs 6.7 MB against a 17 MB table at
  that size. Both halves of that are acceptable here: a single-operator fleet
  archive is orders of magnitude smaller than the measured corpus, so the
  unindexed case stays comfortably interactive while the indexed case covers
  hostnames, error strings, and longer phrases. This is the honest reason the
  index is kept, and the reason no minimum term length is imposed on the
  operator.
- That decision touches no frozen surface, which is why it is affordable.
  `pg_trgm` goes into `record_platform_internal` beside `pgcrypto`, and the
  existing catalog readers already exclude extension members generically
  (`pg_depend.deptype = 'e'`), so its functions never appear as unexpected
  functions, owners, or grants. The frozen r2 pgcrypto attestation filters on
  `extname = 'pgcrypto'` and is unaffected. The generic opaque-extension-member
  reachability verifier keeps failing closed if anyone ever installs a text
  extension into a schema an app role can reach.
- Relevance ordering is dropped, and this is the one place the frozen surfaces
  actually decide a product question. Ranking needs a per-row scalar, the
  trigram functions live in a schema no app role may `USAGE`, and the APP ACL
  privilege grammar only admits functions shaped `public.name(bytea)`
  (`validFunctionIdentity`), which a `(text, text)` ranking wrapper cannot
  satisfy. Widening either the grammar or the schema boundary to buy result
  ordering is a bad trade, so `Sort` is exactly `updated_at_desc` /
  `updated_at_asc`, the cursor carries no float component, and the trigram index
  still earns its place by making the text filter indexed. Recency ordering also
  matches how an operator reads an incident archive.

## Decisions taken during implementation

- The content digest on `record_search_documents` covers projected content only
  (revision identity, type/status/impact/owner, title, derived text, tags,
  participants, subjects, occurrence dates, visibility). Lifecycle, lock version,
  authorization epoch, fence epoch and the timestamps are deliberately outside
  it. Those are stored plainly and compared column by column, which is what lets
  the lifecycle path update the index in place. That mattered because
  `CommitRecordLifecycle` writes no revision and therefore never runs revision
  participants: without an in-place update an archived record would have kept
  answering an active-only search.
- The projector writes every generation in `published` or `building` state. A
  shadow rebuild must receive live commits while it runs, or publishing the
  rebuilt generation would drop records committed during the rebuild. The
  `record_lock_version` / `record_fence_epoch` fence on the upsert is what keeps
  a rebuild replaying an older snapshot from overwriting a newer live commit, and
  subject edges are only rewritten for generations whose document that
  transaction actually won.
- A body the Markdown dialect cannot parse yields title-only indexed text rather
  than an error. A derived index must never be the reason a record fails to save.
- The search read service has to live in `recordsearch`, not `records`.
  `recordsearch` already imports `records` for the registry vocabulary
  (`RecordType`, `Lifecycle`, `SubjectKind`, …), so `records.RecordCandidatePage`
  cannot carry a `recordsearch.Query` without an import cycle. Consequently
  `records.RecordListRequest` keeps only sort and paging, the filter vocabulary
  is not restated at the `records` layer, and the HTTP layer routes any filtered
  or text query to the `recordsearch` service. That is also what retires
  `matchesRecordQuery` without leaving two filter implementations.
- Authorization stays out of SQL. The index stores a visibility digest, not the
  grants, so the candidate query narrows by indexed facts only and every
  candidate is still authorized through `recordauth.Policy` before it is
  hydrated, exactly as `RecordReadService.ListRecords` does today. No total count
  or facet may be reported over unauthorized candidates.

## Dispatch

Cursor implements this child directly with Trellis native auto artifacts.
`implement.jsonl` / `check.jsonl` are curated before `task.py start`.
