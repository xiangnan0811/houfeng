# 记录、修订、草稿与状态核心 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline only. Every behavior change follows RED -> verify RED -> minimal GREEN -> verify GREEN. Track the checkboxes in this file.

**Goal:** 实现可完整重建、不可静默覆盖并受删除 fence 保护的记录/修订/草稿核心。

**Architecture:** `records` domain 编排强一致 revision transaction；PostgreSQL store 提供 admitted pgx transaction 和 fence-aware persistence；`recorddeletion` 注册 core purge adapter；HTTP/Web 只交付版本化 transport contract，不开放正式页面。

**Tech Stack:** Go 1.26, pgx v5, PostgreSQL 16, React/TypeScript/Vitest, Node 22.23.1.

---

## Delivery Rules

- [x] Child 1 已合入 protected `main`，当前 branch/worktree 基于 `51c24f752e96669cecf79fed1ffa55ee8a1e742f`，与 `origin/main` 为 `0/0`。
- [x] hooks 已配置为 `.githooks`；`make verify-go` 与 Node 22 `make verify-web` baseline 已通过。
- [x] 用户批准一个 Child 2 task、一个 branch/worktree、一个早期 Draft PR 和三个硬检查点。
- [x] 规划/激活先单独提交；之后每个检查点至少一个独立提交、完整 focused verification 和用户进度报告。
- [x] 检查点 1 完成后 push 并创建 Draft PR；检查点 2/3 继续在同一 PR。
- [x] 三检查点未整体闭合前不合并，不把 `0052` 当作不可再改的发布历史；2026-08-04 三个检查点及审查发现已整体闭合，Checkpoint 3 首轮 Draft PR required CI 7/7 GREEN，PR 仍为 Draft 且未合并。
- [x] 不读取、迁移、回填或双写 `experience_logs`；不触碰主检出目录的弃用 `0052_add_app_extension_hardening_receipt.sql`；2026-08-04 production forbidden scan 与该弃用路径的 branch diff/history 检查均为零结果。

## Checkpoint 1: `0052`, current APP ACL, domain contracts

### Task 1.1: Lock migration grammar with RED tests

**Files:**
- Create: `internal/center/store/migrate/records_core_migration_test.go`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

- [x] Add source tests that require exactly these owned tables: `records`, `record_revisions`, `record_revision_subjects`, `record_revision_tags`, `record_revision_participants`, `record_drafts`, `record_draft_checkpoints`, `record_domain_activities`, `record_core_purge_receipts`.
- [x] Assert no `participant_ids` revision column, no `record_draft_recovery_points`, partial-unique plus deferred exactly-one-primary enforcement, same-record current pointer, monotonic revision/index constraints, draft author/checkpoint retention indexes and no source-domain cascade.
- [x] Run `go test ./internal/center/store/migrate -run 'RecordsCore|MigrationFiles' -count=1`; verified RED because `0052_create_records_core.sql` was absent, then GREEN after the schema implementation.
- [x] Add a real PostgreSQL RED proving a revision without a primary previously committed; implement the hardened deferred validator on revision/subject mutations, then verify missing-primary `23514`, second-primary `23505` and single-transaction explicit purge GREEN.

### Task 1.2: Implement `0052` and current APP ACL as one unit

**Files:**
- Create: `db/migrations/0052_create_records_core.sql`
- Modify: `internal/center/store/migrate/app_acl_current_contract.go`
- Create: `internal/center/store/migrate/records_core_app_acl_test.go`
- Modify: `internal/center/store/migrate/app_acl_current_postgres_integration_test.go`
- Modify: `internal/center/store/migrate/app_acl_current_runtime_admission_test.go`

- [x] Implement the nine-table schema and explicit constraints/indexes from `design.md` §2 using idempotent current migration conventions.
- [x] Register one exact `AppACLCurrentMigrationFragment` for `0052_create_records_core.sql`; enumerate nine tables plus the primary-subject validator function, its exact hardening, and only required center-runtime/platform-admin privileges.
- [x] Add RED then GREEN tests for missing/extra objects, missing fragment, convergence, catalog privilege admission and exact repeat.
- [x] Run `go test ./internal/center/store/migrate -run 'RecordsCore|AppACLCurrent' -count=1`; GREEN.
- [x] Run `scripts/test-record-platform-integration.sh postgres -- go test ./internal/center/store/migrate -run '^TestPostgresIntegrationAppACLCurrent$' -count=1`; GREEN with no `SKIP`.

### Task 1.3: Implement immutable revision/type/template contracts

**Files:**
- Create: `internal/center/records/types.go`
- Create: `internal/center/records/types_test.go`
- Create: `internal/center/records/registry.go`
- Create: `internal/center/records/registry_test.go`
- Create: `internal/center/records/validation.go`
- Create: `internal/center/records/validation_test.go`

- [x] Write compile/table RED tests for all revision-authoritative fields, lifecycle, canonical status groups, seven builtin types, state transitions, no-state types, template provenance/diff and deterministic canonical hash.
- [x] Write mutation tests proving constructor inputs and returned slices/maps cannot mutate stored normalized values; assert UTC time normalization and duplicate relation/tag/participant rejection.
- [x] Implement minimal immutable value types, closed registries and validation to turn the tests GREEN; do not parse or render Markdown here.
- [x] Verify RED then GREEN that the server-owned template registry rejects non-UTF-8 Markdown instead of allowing later JSON/render corruption.
- [x] Run `go test -race ./internal/center/records -run 'Revision|Lifecycle|Status|Template|Canonical' -count=10`; GREEN.

### Task 1.4: Define subject adapter and authorization contracts

**Files:**
- Create: `internal/center/records/subjects.go`
- Create: `internal/center/records/subjects_test.go`
- Create: `internal/center/records/authorization.go`
- Create: `internal/center/records/authorization_test.go`

- [x] Write RED tests for registry version, `vps|monitoring_instance|target`, `affected|context|evidence_source`, exactly one primary, server-owned immutable snapshot/capture evidence, project match and adapter duplicate/unknown behavior.
- [x] Add live/tombstoned authorization cases covering capture/current intersection, current widening, missing floor, unknown floor kind/version, deleted live route, multi-source narrowing and external no-leak denial.
- [x] Implement `SubjectSourceAdapter`, closed registry, `ResolvedSubject`, immutable safe snapshot and `recordauth.Policy` integration without adding production repository adapters yet.
- [x] Verify RED then GREEN that complete revision normalization accepts canonical full-witness tombstoned authorization as well as live authorization, while retaining digest/kind/source fail-closed checks.
- [x] Run `go test -race ./internal/center/records -run 'Subject|Authorization|Tombstone' -count=10`; GREEN.

### Checkpoint 1 gate

- [x] Run focused migration/domain suites, real PostgreSQL fresh/repeat/current APP ACL admission, `make verify-go`, and `git diff --check`; 2026-08-03 evidence is GREEN, the PostgreSQL anchors executed with zero `SKIP`, and the domain race selector passed 10 iterations.
- [x] Run the production-only forbidden scans below; 2026-08-03 both returned zero matches. Frozen migration history and negative-test fixtures are intentionally outside the match surface:

  ```bash
  set -o pipefail
  if rg -n -g '!**/*_test.go' \
    'record_draft_recovery_points|participant_ids|experience_logs' \
    db/migrations/0052_create_records_core.sql internal/center/records; then exit 1; fi
  if git diff --unified=0 origin/main -- \
    'internal/center/store/migrate/*.go' \
    ':(exclude)internal/center/store/migrate/*_test.go' \
    | rg -n '^\+[^+].*(record_draft_recovery_points|participant_ids|experience_logs)'; then exit 1; fi
  ```
- [x] Commit checkpoint 1 separately, report behavior/tests/open risks, then push and open the approved Draft PR.

## Checkpoint 2: revision, draft, API behavior

### Task 2.1: Build production source adapters

**Files:**
- Create: `internal/center/store/record_subjects.go`
- Create: `internal/center/store/record_subjects_test.go`
- Modify: `internal/center/store/vps_assets.go`
- Modify: `internal/center/store/monitoring_instances.go`
- Modify: `internal/center/store/targets.go`

- [x] Write adapter RED tests for VPS/monitoring-instance/Target project identity, current ACL revision, safe snapshot, route and not-found/deleted behavior using existing repository seams.
- [x] Add only the narrow read methods needed to resolve subject authorization; never accept project/snapshot/scope from the client.
- [x] Implement adapters and tombstone input seam; each read derives live routing or witnessed tombstone floor without updating/cascading immutable revision rows.
- [x] Run focused store/records tests with `-race`; 2026-08-03 full-package race and 10-iteration focused race are GREEN.

### Task 2.2: Implement the atomic record/revision transaction

**Files:**
- Create: `internal/center/store/records.go`
- Create: `internal/center/store/records_test.go`
- Create: `internal/center/store/records_postgres_integration_test.go`
- Create: `internal/center/records/service.go`
- Create: `internal/center/records/service_test.go`
- Create: `internal/center/records/revisions.go`
- Create: `internal/center/records/revisions_test.go`

- [x] Write fake transaction RED tests fixing admission/idempotency/fence/lock/CAS/insert/current/activity/participant/outbox/complete/commit order and rollback at every cut point.
- [x] Implement deterministic `RevisionParticipant` registry and reuse Child 1 idempotency/outbox primitives inside the caller-owned transaction; external calls stay outside.
- [x] Add RED/GREEN tests for create revision 1, revise, restore old revision, archive/restore, no-change `created=false`, same-key replay and same-key/different-fingerprint rejection.
- [x] Add real PostgreSQL races proving one winner for the same base revision, no duplicate revision under retry, current projection/root reconciliation and no half-commit; 2026-08-03 five fresh/current PostgreSQL tests are GREEN with no `SKIP`.
- [x] Run `go test -race ./internal/center/records ./internal/center/store -run 'Record|Revision|Archive|Idempotency' -count=10`; 2026-08-03 GREEN, followed by full-package race, `make verify-go`, `git diff --check` and production forbidden scans GREEN.

### Task 2.3: Implement private drafts and bounded checkpoints

**Files:**
- Create: `internal/center/store/record_drafts.go`
- Create: `internal/center/store/record_drafts_test.go`
- Create: `internal/center/records/drafts.go`
- Create: `internal/center/records/drafts_test.go`

- [x] Write RED tests for author isolation, exact ETag, two-client conflict, base revision advancement, new versus existing-record drafts, five-minute bucket, newest 20/seven-day checkpoints, 90-day TTL/seven-day warning and publish/discard/revoke cleanup; 2026-08-03 unit RED/GREEN covers PATCH/no-change/conflict, cleanup, expiry claims and atomic publish create/update/no-change/conflict/replay/rollback.
- [x] Implement draft service/store with database-time retention seams; no draft path calls revision participants, activity, outbox, search or notifications, while publish cleanup runs only inside the formal revision transaction after successful mutation/no-change handling.
- [x] Add real PostgreSQL tests for concurrent PATCH, checkpoint pruning and cleanup claims; 2026-08-03 all five `TestPostgresIntegrationRecordDraft*` cases passed through the no-SKIP project runner, including reserved existing-draft routing/operation zero-hit behavior, publish/discard/revoke cleanup and zero ordinary-draft activity/outbox rows.
- [x] Run `go test -race ./internal/center/records ./internal/center/store -run 'Draft|Checkpoint' -count=10`; 2026-08-03 GREEN.

### Task 2.4: Add record/draft/revision HTTP behavior

**Files:**
- Create: `internal/center/http/handlers/records.go`
- Create: `internal/center/http/handlers/records_test.go`
- Create: `internal/center/http/handlers/record_drafts.go`
- Create: `internal/center/http/handlers/record_drafts_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [x] Write route/handler RED matrices for §19.1 non-deletion endpoints, session/actor middleware, static-path precedence, handler-owned nested DTO allowlists, exact `If-Match`, `Idempotency-Key`, 400/404/409/413/422/503, feature-off behavior and `Cache-Control: private, no-store`; persisted/typed-recovery unknown draft fields now have explicit 500/no-leak regressions.
- [x] Implement transport-only DTO mapping and bootstrap wiring; response construction copies allowlisted fields, revalidates every outbound draft payload and never serializes store/domain authority or source authorization evidence. Production bootstrap deliberately keeps the not-yet-owned admission gate nil, so registered routes fail closed rather than bypassing admission.
- [x] Prove every read/write reauthorizes all live/tombstoned sources and checks the reservation fence before content/cache access. Current/historical authorization snapshots now load only after admission + read fence; record candidates and author-scoped draft routing atomically filter `fenced|committed` reservations before row scan/limit and retain in-transaction race rechecks; historical reads also bind current visibility and exact current tuple before content.
- [x] Run focused handler/router/bootstrap tests and legacy VPS experience/timeline regression tests; 2026-08-03 full HTTP/records/store race suites, targeted timeline/experience regressions and `trellis-check` are GREEN.

### Checkpoint 2 gate

- [x] Run focused unit/race/real PostgreSQL suites, `make verify-go`, and `git diff --check`; 2026-08-03 handler/router/bootstrap plus records/store race suites passed, all 12 `TestPostgresIntegrationRecord(Read|Draft|Revision)` scenarios passed through the no-SKIP runner, and the final full Go gate/forbidden scans/diff check are GREEN.
- [x] Reconcile root/current revision differences to zero and prove drafts produce zero activity/outbox rows; the PostgreSQL concurrent revision/restore assertions and five draft scenarios verify exact projection reconciliation, ordinary-draft 0/0 side effects, publish 1/1 formal side effects and reserved draft preservation until purge.
- [x] Commit checkpoint 2 separately and report behavior/tests/open risks before starting deletion work; 2026-08-03 the isolated Checkpoint 2 commit contains Task 2.1-2.4 only, remains on the existing Draft PR and does not start deletion work.

## Checkpoint 3: permanent deletion, Web transport, full acceptance

### Task 3.1: Define closed deletion adapter/readiness contracts

**Files:**
- Create: `internal/center/recorddeletion/types.go`
- Create: `internal/center/recorddeletion/types_test.go`
- Create: `internal/center/recorddeletion/registry.go`
- Create: `internal/center/recorddeletion/registry_test.go`

- [x] Write RED tests for exact adapter names/surfaces, duplicate/missing/extra adapters, health snapshots and complete production readiness; 2026-08-03 RED failed on the absent contracts, then focused and 10-iteration race suites passed after the minimal implementation.
- [x] Implement the closed exact set `record_core|record_attachments|record_evidence|record_markdown_client|record_search|record_activity_projection|record_comparison|record_collaboration|record_portability`. The production set stays incomplete until later children register; test fixtures may explicitly supply all nine adapters. Descriptors are construction-time snapshots, surfaces have exact-one ownership, and readiness digests use the fixed adapter order.
- [x] Prove core-only or empty-table states keep permanent-delete capability false and return `deletion_safety_unavailable` without reservation/ledger mutation; `RequireReady` admits only an explicit complete, healthy nine-adapter fixture, while missing/unhealthy/error/cancelled/invalid health states fail closed before any caller mutation.

### Task 3.2: Implement deletion orchestration and core purge

**Files:**
- Create: `internal/center/recorddeletion/service.go`
- Create: `internal/center/recorddeletion/service_test.go`
- Create: `internal/center/recorddeletion/worker.go`
- Create: `internal/center/recorddeletion/worker_test.go`
- Create: `internal/center/recorddeletion/core_adapter.go`
- Create: `internal/center/recorddeletion/core_adapter_test.go`
- Create: `internal/center/recorddeletion/recovery_adapter.go`
- Create: `internal/center/recorddeletion/recovery_adapter_test.go`
- Create: `internal/center/store/record_deletions.go`
- Create: `internal/center/store/record_deletions_test.go`
- Create: `internal/center/store/record_deletions_postgres_integration_test.go`
- Create: `internal/center/store/record_deletion_recovery.go`
- Create: `internal/center/store/record_deletion_recovery_postgres_integration_test.go`

- [x] Write state/cut-point RED tests for preview CAS, authorization revocation, dependency drift, reservation/lease drain, ledger commit unknown, witness pending, permanent fence, retry, `attempt_not_committed`, same-key replay/reuse and content-free receipt; 2026-08-03 focused unit matrices cover every durable worker state plus post-commit `retry_required` transitions.
- [x] Implement preview/execute/worker over Child 1 reservation, ledger, witness, lease and fence interfaces; timeout/transport ambiguity remains fenced, sealed absence alone permits the witnessed `attempt_not_committed` branch, and stale owner CAS cannot finalize or resurrect work. The HTTP-owned status read seam originally listed here was completed with Task 3.3 so its authorization and 404/503 transport contract could be verified end to end.
- [x] Implement core purge exact ownership and verified absence. Recovery replay is continuous, idempotent and transaction-atomic; it handles existing operation, preview-only reservation and synthetic terminal projection backup cut points, while unknown identity/cursor/receipt/audit/fence contracts fail closed.
- [x] Add real PostgreSQL concurrency tests proving reservation-after core new reads/writes are zero, not-committed releases safely and stale workers cannot resurrect content; 2026-08-03 all nine deletion/recovery PostgreSQL cases passed through the no-SKIP runner, including purge rollback and preview-only recovery.
- [x] Run `go test -race ./internal/center/recorddeletion ./internal/center/store -run 'Deletion|Purge|Recovery|Reservation' -count=10`; 2026-08-03 GREEN, followed by full package tests, real PostgreSQL schema verification, `make verify-go`, forbidden scans and `git diff --check` GREEN.

### Task 3.3: Add deletion HTTP contracts

**Files:**
- Create: `internal/center/http/handlers/record_deletions.go`
- Create: `internal/center/http/handlers/record_deletions_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [x] Write RED matrices for preview/execute/status authorization, no-leak 404, stale 409, unavailable 503, pending 202, `not_committed` 200, same operation replay and response allowlists. The header-only token RED first returned 400 while a body token still reached application; the safety-summary RED then failed on absent domain/DTO fields. GREEN fixes make the canonical `DeletionRequestTokenV1` the sole `Idempotency-Key`, keep execute body reservation-only, and expose only ordered identity-free survivor/backup/ledger-health fields.
- [x] Wire production readiness so incomplete later adapters expose no permanent-delete capability and no token; do not create a test-only bypass in production bootstrap. Runtime-admission bootstrap registers the deletion transport with a nil application, so preview/execute return `503 deletion_safety_unavailable`, status returns `503 deletion_status_unavailable`, and legacy mode registers no deletion routes.
- [x] Run focused handler/router/bootstrap tests; 2026-08-03 affected-package tests, both 10-iteration race groups, the real PostgreSQL preview/reservation/replay anchor, `make verify-go`, production forbidden scans, tracked plus untracked whitespace checks, and Trellis cross-layer review are GREEN.

### Task 3.4: Add lazy Web DTO and transport contract

**Files:**
- Modify: `web/src/lib/types.ts`
- Create: `web/src/lib/recordsApi.ts`
- Create: `web/src/lib/recordsApi.test.ts`
- Modify: `web/src/lib/apiRequest.ts`
- Modify: `web/src/lib/apiRequest.test.ts`
- Create: `web/src/lib/apiError.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Modify: `web/src/lib/auth-client.ts`
- Create: `web/src/security/recordsTransportArchitectureContract.test.ts`
- Modify: `web/src/security/bundleBudgetContract.test.ts`
- Modify: `.trellis/spec/web/state-and-data.md`

- [x] Write RED tests fixing all Child 2 URLs, methods, cursor/query normalization, `If-Match`, `Idempotency-Key`, allowlisted responses and 404/409/503 recovery shapes; 2026-08-04 the seven façade cases cover all 17 exact exports, including payload-only new-record drafts and paired existing-record routing fields.
- [x] Move the existing `withQuery` implementation from `api.ts` into `apiRequest.ts`, preserve all legacy API tests, and extend `ApiError` with allowlisted `code`, field errors and recovery while retaining status/message compatibility; structured metadata is an explicit decoder seam owned by the lazy Records façade, while default eager transport remains legacy message-only.
- [x] Implement the façade by reusing `requestJSON`, `requestEmpty`, `jsonBodyInit` and `withQuery` from `apiRequest.ts`; production scans and AST tests prove there is no raw `fetch`, React, route, UI or eager `api.ts` dependency, and shared JSON request initialization has one owner.
- [x] Add an AST/source RED test forbidding imports from `web/src/app/layout/AppShell.tsx`, `TopBar.tsx`, `Sidebar.tsx`, `web/src/lib/api.ts` and the eager router dependency graph. The fresh production fixture contains neither `recordsApi.ts` nor `apiError.ts`; the synthetic lazy consumer places both in the same dynamic-only chunk.
- [x] Run `env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts` plus lint/build/bundle tests; 2026-08-04 the focused façade suite is 7/7 GREEN and fresh `make verify-web` is 126/126 files, 883/883 tests, coverage/build/CSS GREEN, with entry JS `110716 <= 110738` and max async JS `31903 <= 32052`.

### Task 3.5: Full acceptance and PR closure gate

- [x] Run all focused race tests and repository PostgreSQL migration/records/deletion integration suites; 2026-08-04 three 10-iteration race groups passed for Records/store, deletion/recovery and HTTP/router/bootstrap, followed by fresh/`0052` migration and 30 RecordPlatform/revision/read/draft/deletion/recovery PostgreSQL scenarios with zero `SKIP`.
- [x] Run `HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.12 scripts/test-record-platform-integration.sh pg16-catalog -- go test -json ./internal/center/store/migrate -run '^(TestPostgresIntegrationAppACLR2|TestPostgresIntegrationAppACLCurrent)$' -count=1`; 2026-08-04 both anchors passed with no `skip`/`fail` JSON events.
- [x] Run `make verify-go`; 2026-08-04 repository fmt, vet and complete Go test gate passed.
- [x] Run `env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web`; 2026-08-04 Node 22.23.1 passed 126/126 files and 883/883 tests plus coverage, ESLint, strict TypeScript/Vite build, bundle and CSS budgets (`entryJsGzipBytes=110716`, `maxAsyncJsGzipBytes=31903`).
- [x] Run `git diff --check` and scan production code for legacy `experience_logs` access or forbidden eager `recordsApi` imports; 2026-08-04 tracked/untracked whitespace, conflict-marker, legacy production access, eager import, raw Records fetch and forbidden schema-name scans all returned zero findings.
- [x] Load and run `trellis-check`; 2026-08-04 cross-layer Store -> Service/Worker -> HTTP -> lazy Web DTO flow, closed adapter/state/survivor sets, reuse/import direction, legacy API compatibility and bundle isolation are coherent. Phase 3.3 also captured the permanent-deletion/core-purge/recovery contract in the backend database spec; no code fix was required.
- [x] Commit checkpoint 3 separately, push, monitor Draft PR required CI and keep the PR unmerged until all three checkpoints and review findings are coherent; 2026-08-04 Checkpoint 3 commit `0a999477` was pushed separately, and Draft PR #397 completed 7/7 required checks GREEN with zero failure/skip while remaining unmerged.

## Rollback

- Before merge, revert only the failing checkpoint commit on the feature branch or correct `0052` in place; never modify local/remote `main` directly.
- Feature default-off prevents new Records capability exposure while diagnosing application failures.
- Returning a development environment to code without `0052` requires rebuilding that development database; no down migration or legacy compatibility path is added.
- Deletion reservations/outcomes are never manually cleared on uncertainty; recovery follows witnessed delete/outcome state and stays fail closed when proof is unavailable.
