# VPS Detail and Records Program Implementation Plan

> **For agentic workers:** Execute only the currently approved child or bounded
> slice. Use `superpowers:subagent-driven-development` or
> `superpowers:executing-plans` according to the reviewed execution choice; do
> not treat this parent plan as one implementation job.

**Goal:** Deliver the approved VPS overview and project Records experience
through 12 independently verifiable children.

**Architecture:** The parent owns requirements, dependency order, migration
numbering, and final cross-child acceptance. Each child owns its domain schema,
backend, frontend, adapters, and tests. The selected branch is integrated and
reviewed before the next dependent child starts.

**Tech Stack:** Go, pgx/PostgreSQL, React/TypeScript, local/S3-compatible object
storage, Vitest, Playwright, and Trellis child-task workflow.

---

## 0. Authority and current state

- Read `research/development-rebaseline-2026-08-02.md` before any child work.
- Parent status remains `planning`; it is not an implementation target.
- Current delivered functional completion is `12/12` on protected main. The
  2026-08-23 closeout parent and its two children are coordination/audit nodes,
  not a thirteenth product capability. Current handoff:
  `research/handoff-2026-08-23.md`; the 2026-08-21 audit is historical.
- Overview management is closed by PR #438: selected commit
  `7e9080f208a5f1f5cce7e563f5030b9d068629de`, merge commit
  `af23844adc82ce97e6815a3dbd8706f7fdab10e8`, 7/7 PR checks and post-merge
  main CI `32637395760` successful. It shipped in `v0.75.0`. Overview task
  archival PR #440 merged as current `origin/main`
  `62f975c535f076ef7c322a07e25c4c158a9efe34`; post-merge main CI
  `32638216017` succeeded.
- The user abandoned single-record irreversible permanent deletion for the
  current scope on 2026-08-23. It remains unimplemented, disabled and
  fail-closed. Do not return missing adapters to historical owners or reopen
  Child 11. A future need requires a new Trellis task and new requirements,
  design, implementation and acceptance.
- The closing boundary is seven missing readiness rows:
  `deletion.record_markdown_client`, `deletion.record_comparison`,
  `recovery.record_search`, `recovery.record_collaboration`,
  `recovery.record_portability`, `backup.orchestration`, and
  `restore.replay`. The backup/restore rows are paired absent in production;
  the production HTTP surface is additionally closed by
  `handlers.RecordDeletions(nil)`.
- Keep three lifecycle meanings separate: reversible record archive/restore;
  deleting or rebuilding the whole disposable test environment; and the
  abandoned single-record irreversible permanent-delete capability.
- Three leftovers are accepted deferrals and remain unimplemented/unverified:
  activity group-granted viewer digest (revisit when viewer scope exceeds the
  project digest), comparison sticky row headers at 390px (revisit on an actual
  positioning/usability issue), and the 4 GiB / 512 MiB mixed-load harness
  (revisit for a formal capacity SLO, target hardware, or continuous benchmark).
- Child 8 reconciliation and scope A live in the archived child research
  note. Do not revive the 2026-08-02 Child 8 implement list.
- Child 10 current-main reconciliation is
  `../archive/2026-08/07-14-vps-records-portability-migration/research/current-main-reconciliation-2026-08-21.md`.
  Alan chose scope **B** (one Trellis child, two PRs; exit after both).
  Child 10 PR2 exit was narrowed 2026-08-21; do not re-expand it.
- Child 11 current-main inventory lived in its task research and is now
  on main via #433; do not start a second Child 11.
- The old 121-item matrix is historical risk coverage, not a current
  line-by-line release gate.
- Do not resume the stopped parent goal. A goal or sub-agent may be used only
  for a bounded slice with an explicit output and review checkpoint.

## 1. Branch and review policy

1. Start from current `origin/main` in a non-main branch/worktree with hooks
   enabled.
2. Prefer one active implementation branch/worktree/PR.
3. Allow parallel work only when dependencies, migrations, file ownership, and
   reviewer capacity are demonstrably independent.
4. Keep unrelated dirty checkouts and stale worktrees untouched.
5. Run the child-specific focused gates before the repository-wide gates.
6. Integrate through a protected-main PR and verify required CI.
7. Re-read the next child's plan against the new main before starting it.

An old branch or worktree is never selected merely because it already exists.
Compare it to main first; most foundation worktree tips are already ancestors
of main.

## 2. Migration ownership

| Child | Migration | Required ACL/admission ownership |
|---|---|---|
| 1. Foundation | existing `0051` | current embedded source-set compiler and admission |
| 2. Core | `0052_create_records_core.sql` | core tables/sequences and runtime grants |
| 3. Attachments | `0053_create_record_attachments.sql` | attachment/blob metadata objects and grants |
| 4. Evidence | `0054_create_record_evidence.sql` | evidence objects and grants |
| 9. Collaboration | `0055_create_record_collaboration.sql` | collaboration objects and grants |
| 6. Search | `0056_create_record_search.sql` | search projection objects and grants |
| 7. Activity | `0057_create_record_activity.sql` | activity projection objects and grants |
| 10. Portability | `0058_create_record_portability.sql` | import/export job and origin objects/grants |
| Portability follow-up | `0059_relax_portability_blob_key_regex.sql` | musl-safe `blob_key` CHECK only; no new tables |

Children 5, 8, and 11 add no root migration unless a later reviewed design
proves one necessary. If any migration number is unexpectedly occupied on main,
stop and rebaseline all later unimplemented numbers together.

Every migration-owning child must add a current APP ACL fragment and prove:

- the embedded source set has exactly one explicit fragment per post-`0051`
  migration, including an explicit empty fragment when no APP object is added;
- managed objects and runtime/admin privileges match the migration DDL;
- a fresh PostgreSQL database converges and admits the direct runtime role;
- exact repeat convergence/startup is read-only and succeeds;
- a missing or drifting fragment fails before catalog mutation.

## 3. Execution order and child exits

### Slice 1: Close Child 1

- [x] Execute the plan in
  `../archive/2026-08/07-14-vps-records-platform-foundation/research/current-app-migration-baseline-plan.md`.
- [x] Do not add Records Core schema or UI.
- [x] Run focused migration/admission/CLI/bootstrap tests, PostgreSQL integration,
  full Go verification, and `trellis-check`.
- [x] Audit Child 1 acceptance against behavior already on main plus this slice.
- [x] Update specs only for contracts that the resulting code actually establishes.
- [x] Merge and archive Child 1 before starting Child 2.

Completion evidence: PR #394 merged as `2cbeb1bb`; required PR CI run
`30750684376`, final independent review, and protected-main CI run `30751460764`
all passed. At that historical stage, progress was `1/11` under the former child
count; current authoritative functional progress is `12/12`.

### Child 2: Records core

- Deliver stable record roots, immutable complete revisions, private server
  drafts, CAS/idempotent save, lifecycle, APIs, and the core deletion adapter.
- Use `0052`; do not migrate or dual-write `experience_logs`.
- Exit only when fresh/repeat migration, unit, handler, real PostgreSQL, and
  relevant Web contract tests pass on the same commit.

### Child 3: Attachments

- Deliver `0053`, local/S3-compatible BlobStore, upload/scan/quota lifecycle,
  authorized streaming, revision participant, and deletion/recovery adapters.
- Exit with the same conformance suite passing for local and MinIO-backed
  storage plus processor failure/cleanup tests.

### Child 4: Evidence

- Deliver `0054`, a versioned evidence registry, initial trusted source
  adapters, bounded capture, classification, rendering, and adapters.
- Exit with conformance coverage for every registered kind and real source
  partial/deleted/archived behavior.

### Child 9: Collaboration

- Deliver `0055`, owners/participants, actions, comments, watches, inbox, and
  permission-safe notifications.
- Reuse Core revision participants, `recordplatform` admission/idempotency/
  identity-only outbox, `recordauth`, and the existing deletion registry; do not
  introduce parallel platform primitives.
- Own a minimal versioned comment-safe Markdown renderer and hostile corpus.
  Publish only typed filter/activity/portability/recovery contracts to later
  children; do not build their tables, projections, jobs, pages, or orchestration.
- Exit with transaction, recipient, revocation, deletion, and notification
  retry tests; external delivery is optional unless configured.

### Child 5: Markdown workspace

- Start only after Children 2, 3, 4, and 9 are integrated on protected main.
- Deliver the safe Markdown dialect, preview/read model, editor, drafts,
  revision diff/conflict, references, and material workspace.
- Reuse and extend Child 9's comment-safe Markdown contract/corpus; do not create
  a second incompatible comment renderer.
- Integrate attachments, evidence, and collaboration only through their public
  contracts.
- Exit with XSS corpus, editor state, desktop/390px, keyboard, and bundle tests.

### Child 6: Search center

- Deliver `0056`, authoritative transaction projection/rebuild, scoped query,
  Records/Drafts pages, stable cursor/URL, and global search integration.
- Exit with authorization, archived/deleted, Chinese/English query, EXPLAIN,
  pagination, and UI state tests.

### Child 7: Activity and VPS overview

- [x] Deliver `0057`, versioned source adapters, projector/checkpoints, subject
  timeline, overview aggregation, and approved VPS detail composition.
- [x] Exit with deterministic ordering, partial-source behavior, stable/anomaly
  layout, source deletion, desktop/390px, and accessibility tests.

Completion evidence: PR #422 merged, Release Please #420 published `v0.70.0`.
The overview management follow-up is also closed: PR #438 mounts the real five
action panels on the overview route while keeping `LegacyVPSDetail` as a tested
fallback; PR/main CI and `v0.75.0` evidence are listed in section 0. Activity
group-granted viewer digest remains unimplemented and is an accepted deferral.

### Child 8: Comparison workbench

- [x] Deliver candidate discovery, fixed comparison, compatibility/coverage
  reasons, workbench UI, shareable URL, and save-as-record.
- [x] Reuse pairwise `Kind.Compare`; series only for `monitoring.host/v1` and
  `monitoring.probe/v2`; HMAC save-as-record stays in-child.
- [x] Exit with bounded process-local admission, partial evidence, desktop/390
  named-scroll matrix, accessibility, and atomic save tests.

Archived 2026-08-21 at `../archive/2026-08/07-14-vps-records-comparison-workbench`
(PR #423).
Accepted deferrals: the 4 GiB / 512 MiB mixed-load harness was never delivered by
Child 11, and sticky row headers remain omitted at 390px. Neither is represented
as implemented; each requires a new task when its trigger in section 0 occurs.

### Child 10: Portability (narrowed 2026-08-21)

- Scope B: one Trellis child, two reviewable PRs; exit after both are on
  protected main **under the narrowed PR2 contract**. Consume
  `comparison.result/v1` Export; do not add a workbench download.
- PR1: named `AdmissionGate` + witnessed tombstone reader + `0058` + human
  Markdown + comparison Export consumption + default-off flag + adapter
  surfaces.
- PR2: ZIP64 archive of document + known evidence JSON; dry-run/apply of
  documents + origin + job in one transaction; origin tombstone and
  existing-origin 409; derived PDF (in-process); unknown schema
  fail-closed; search-page export follows the selected row.
- Do not create `0059` or convert `experience_logs`. Do not fold Asset
  JSON import into Records archives.

### Abandoned at parent (do not revive in Child 10/11/12)

- ZIP activity pages: import rebuilds search/activity from authoritative
  rows; checkpoints stay rejected.
- Persisted import `quarantine` rows: unknown schema stays
  `ErrImportSchemaBlocked`; `optional:true` is not trusted. Do not add a
  store-and-display opaque-envelope path unless a later product decision
  reopens it.

### Child 12: Archive restore fidelity

- After Child 10 archives. Before Child 11 claims official restore.
- Persist known evidence snapshots in the same apply transaction.
- Put authorized attachment bytes into the archive and restore them.
- Wire `houfeng-content-processor` so PDF isolation/no-network is real.
- Do not implement abandoned quarantine persist or activity-in-ZIP.

### Child 11: Integration verification

- Register and verify every child-owned backup/restore/replay adapter.
- Compose the real admission/tombstone authority, prove readiness and fail-closed
  behavior, and enable protected capabilities only after the aggregate gate is
  complete.
- Run Child 10's existing `HOUFENG_POSTGRES_INTEGRATION=1` and
  `HOUFENG_MINIO_INTEGRATION=1` portability suites (do not write new
  persist paths). Verify unknown-schema fail-closed; do not implement
  quarantine rows.
- Exercise PostgreSQL, local and S3-compatible storage, processor, worker,
  security, capacity, failure injection, browser, accessibility, and recovery.
- Validate permanent deletion only after all adapter and replay gates are green.
- Do not implement Child 12 domain work. A missing restore-fidelity
  contract returns to Child 12.
- Do not require staging deployment, release-image publication, Release Please,
  soak, or human-participant studies for child completion.

## 4. Per-child delivery loop

For each child:

- [ ] Refresh `origin/main`; confirm dependency commits and migration number.
- [ ] Run `trellis-before-dev` for every package/layer that will change.
- [ ] Reconcile the child PRD/design/implement with actual dependency APIs.
- [ ] Obtain explicit start approval and set only that child `in_progress`.
- [ ] Execute small RED -> verified RED -> minimal GREEN -> verified GREEN slices.
- [ ] Stop for review when a slice changes public contracts or scope.
- [ ] Run focused tests after each slice and full relevant gates before review.
- [ ] Run `trellis-check`, update executable specs, and inspect the complete diff.
- [ ] Commit and open one PR from the selected non-main branch.
- [ ] Monitor required CI, fix on the same branch, merge, and verify main CI.
- [ ] Archive the child only after its acceptance is present on main.

## 5. Parent final acceptance

After all 12 children are on protected main:

- [x] Assembly path exists for search/activity/compare/export/import; official
  backup/restore packages exist; production permanent-delete HTTP stays nil.
- [x] Child 11 added no root migration. Follow-up #436 added `0059`
  (musl-safe `blob_key` CHECK only).
- [x] Child 11 `verify-web`, browser 64/64, local recovery/capacity/security,
  and main CI `32497370438` passed. After #436, local + S3
  `./scripts/run-records-integration.sh` passed; main CI `32542193350`
  and release CI `32542453260` were green.
- [x] `HOUFENG_RECORDS_ENABLED` / `HOUFENG_PORTABILITY_ENABLED` remain default
  off; `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` is default off and true is
  rejected; Comparison also remains default off. No legacy content compatibility
  is claimed.
- [x] Permanent delete is still visibly disabled with the seven exact missing
  readiness rows and separate nil production HTTP handler recorded above.
- [x] Cross-child evidence:
  `research/final-cross-child-audit-2026-08-21.md`.
- [x] All 12 functional children are archived and included in protected main.
  Overview management is closed by PR #438 and released in `v0.75.0`.
- [x] The user accepted the permanent-delete fail-closed boundary and the three
  explicit deferrals on 2026-08-23. This is a scope decision, not an
  implementation claim; future triggers create new tasks.
- [ ] Deliver the final current-authority documentation through a protected-main
  PR and verify required/main CI, then archive in order: final-audit child,
  closeout parent, original parent. Do not claim delivery or archive completion
  from the local audit branch.

## 6. Rollback and replanning triggers

Stop the active child and return to planning when:

- a dependency contract is missing or materially differs from its child plan;
- migration numbering or APP ACL ownership collides;
- the implementation starts solving released-database upgrade, mixed-version,
  staging, release, or APP V3 governance without a new user decision;
- one slice cannot be reviewed independently or grows across multiple child
  ownership boundaries;
- unrelated dirty work must be moved, deleted, or overwritten;
- test evidence depends on unsupported Node 24 or quota-constrained `/tmp`.

Replanning updates the affected child and this parent map only when the global
dependency or migration contract changes. It does not restart the entire
program or invalidate already accepted behavior.
