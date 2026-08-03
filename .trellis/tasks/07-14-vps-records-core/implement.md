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
- [ ] 检查点 1 完成后 push 并创建 Draft PR；检查点 2/3 继续在同一 PR。
- [ ] 三检查点未整体闭合前不合并，不把 `0052` 当作不可再改的发布历史。
- [ ] 不读取、迁移、回填或双写 `experience_logs`；不触碰主检出目录的弃用 `0052_add_app_extension_hardening_receipt.sql`。

## Checkpoint 1: `0052`, current APP ACL, domain contracts

### Task 1.1: Lock migration grammar with RED tests

**Files:**
- Create: `internal/center/store/migrate/records_core_migration_test.go`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

- [ ] Add source tests that require exactly these owned tables: `records`, `record_revisions`, `record_revision_subjects`, `record_revision_tags`, `record_revision_participants`, `record_drafts`, `record_draft_checkpoints`, `record_domain_activities`, `record_core_purge_receipts`.
- [ ] Assert no `participant_ids` revision column, no `record_draft_recovery_points`, exactly one-primary support, same-record current pointer, monotonic revision/index constraints, draft author/checkpoint retention indexes and no source-domain cascade.
- [ ] Run `go test ./internal/center/store/migrate -run 'RecordsCore|MigrationFiles' -count=1`; expect RED because `0052_create_records_core.sql` is absent.

### Task 1.2: Implement `0052` and current APP ACL as one unit

**Files:**
- Create: `db/migrations/0052_create_records_core.sql`
- Modify: `internal/center/store/migrate/app_acl_current_contract.go`
- Create: `internal/center/store/migrate/records_core_app_acl_test.go`
- Modify: `internal/center/store/migrate/app_acl_current_postgres_integration_test.go`
- Modify: `internal/center/store/migrate/app_acl_current_runtime_admission_test.go`

- [ ] Implement the nine-table schema and explicit constraints/indexes from `design.md` §2 using idempotent current migration conventions.
- [ ] Register one exact `AppACLCurrentMigrationFragment` for `0052_create_records_core.sql`; enumerate every new managed table/sequence and only required center-runtime/platform-admin privileges.
- [ ] Add RED then GREEN tests for missing/extra objects, missing fragment, convergence, catalog privilege admission and exact repeat.
- [ ] Run `go test ./internal/center/store/migrate -run 'RecordsCore|AppACLCurrent' -count=1`; expect GREEN.
- [ ] Run `scripts/test-record-platform-integration.sh postgres -- go test ./internal/center/store/migrate -run '^TestPostgresIntegrationAppACLCurrent$' -count=1`; require GREEN with no `SKIP`.

### Task 1.3: Implement immutable revision/type/template contracts

**Files:**
- Create: `internal/center/records/types.go`
- Create: `internal/center/records/types_test.go`
- Create: `internal/center/records/registry.go`
- Create: `internal/center/records/registry_test.go`
- Create: `internal/center/records/validation.go`
- Create: `internal/center/records/validation_test.go`

- [ ] Write compile/table RED tests for all revision-authoritative fields, lifecycle, canonical status groups, seven builtin types, state transitions, no-state types, template provenance/diff and deterministic canonical hash.
- [ ] Write mutation tests proving constructor inputs and returned slices/maps cannot mutate stored normalized values; assert UTC time normalization and duplicate relation/tag/participant rejection.
- [ ] Implement minimal immutable value types, closed registries and validation to turn the tests GREEN; do not parse or render Markdown here.
- [ ] Run `go test -race ./internal/center/records -run 'Revision|Lifecycle|Status|Template|Canonical' -count=10`; expect GREEN.

### Task 1.4: Define subject adapter and authorization contracts

**Files:**
- Create: `internal/center/records/subjects.go`
- Create: `internal/center/records/subjects_test.go`
- Create: `internal/center/records/authorization.go`
- Create: `internal/center/records/authorization_test.go`

- [ ] Write RED tests for registry version, `vps|monitoring_instance|target`, `affected|context|evidence_source`, exactly one primary, server-owned snapshot, project match and adapter duplicate/unknown behavior.
- [ ] Add live/tombstoned authorization cases covering capture/current intersection, current widening, missing floor, unknown floor kind/version, deleted live route, multi-source narrowing and external no-leak denial.
- [ ] Implement `SubjectSourceAdapter`, closed registry, `ResolvedSubject`, immutable safe snapshot and `recordauth.Policy` integration without adding production repository adapters yet.
- [ ] Run `go test -race ./internal/center/records -run 'Subject|Authorization|Tombstone' -count=10`; expect GREEN.

### Checkpoint 1 gate

- [ ] Run focused migration/domain suites, real PostgreSQL fresh/repeat/current APP ACL admission, `make verify-go`, and `git diff --check`.
- [ ] Confirm `rg -n 'record_draft_recovery_points|participant_ids|experience_logs' db/migrations/0052_create_records_core.sql internal/center/records internal/center/store/migrate` has no forbidden production match.
- [ ] Commit checkpoint 1 separately, report behavior/tests/open risks, then push and open the approved Draft PR.

## Checkpoint 2: revision, draft, API behavior

### Task 2.1: Build production source adapters

**Files:**
- Create: `internal/center/store/record_subjects.go`
- Create: `internal/center/store/record_subjects_test.go`
- Modify: `internal/center/store/vps_assets.go`
- Modify: `internal/center/store/monitoring_instances.go`
- Modify: `internal/center/store/targets.go`

- [ ] Write adapter RED tests for VPS/monitoring-instance/Target project identity, current ACL revision, safe snapshot, route and not-found/deleted behavior using existing repository seams.
- [ ] Add only the narrow read methods needed to resolve subject authorization; never accept project/snapshot/scope from the client.
- [ ] Implement adapters and tombstone input seam; source deletion clears live routing without cascading revision rows.
- [ ] Run focused store/records tests with `-race`; expect GREEN.

### Task 2.2: Implement the atomic record/revision transaction

**Files:**
- Create: `internal/center/store/records.go`
- Create: `internal/center/store/records_test.go`
- Create: `internal/center/store/records_postgres_integration_test.go`
- Create: `internal/center/records/service.go`
- Create: `internal/center/records/service_test.go`
- Create: `internal/center/records/revisions.go`
- Create: `internal/center/records/revisions_test.go`

- [ ] Write fake transaction RED tests fixing admission/idempotency/fence/lock/CAS/insert/current/activity/participant/outbox/complete/commit order and rollback at every cut point.
- [ ] Implement deterministic `RevisionParticipant` registry and reuse Child 1 idempotency/outbox primitives inside the caller-owned transaction; external calls stay outside.
- [ ] Add RED/GREEN tests for create revision 1, revise, restore old revision, archive/restore, no-change `created=false`, same-key replay and same-key/different-fingerprint rejection.
- [ ] Add real PostgreSQL races proving one winner for the same base revision, no duplicate revision under retry, current projection/root reconciliation and no half-commit.
- [ ] Run `go test -race ./internal/center/records ./internal/center/store -run 'Record|Revision|Archive|Idempotency' -count=10`; expect GREEN.

### Task 2.3: Implement private drafts and bounded checkpoints

**Files:**
- Create: `internal/center/store/record_drafts.go`
- Create: `internal/center/store/record_drafts_test.go`
- Create: `internal/center/records/drafts.go`
- Create: `internal/center/records/drafts_test.go`

- [ ] Write RED tests for author isolation, exact ETag, two-client conflict, base revision advancement, new versus existing-record drafts, five-minute bucket, newest 20/seven-day checkpoints, 90-day TTL/seven-day warning and publish/discard/revoke cleanup.
- [ ] Implement draft service/store with fake-clock seams; no draft path may call revision participants, activity, outbox, search or notifications.
- [ ] Add real PostgreSQL tests for concurrent PATCH, checkpoint pruning and cleanup claims.
- [ ] Run `go test -race ./internal/center/records ./internal/center/store -run 'Draft|Checkpoint' -count=10`; expect GREEN.

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

- [ ] Write route/handler RED matrices for §19.1 non-deletion endpoints, session/actor middleware, static-path precedence, allowlisted nested DTO, `If-Match`, `Idempotency-Key`, 400/404/409/413/422/503 and feature-off behavior.
- [ ] Implement transport-only DTO mapping and bootstrap wiring; do not serialize store/domain structs or expose source authorization evidence.
- [ ] Prove every read/write reauthorizes all live/tombstoned sources and checks the reservation fence before content/cache access.
- [ ] Run focused handler/router/bootstrap tests and legacy VPS experience/timeline regression tests; expect GREEN.

### Checkpoint 2 gate

- [ ] Run focused unit/race/real PostgreSQL suites, `make verify-go`, and `git diff --check`.
- [ ] Reconcile root/current revision differences to zero and prove drafts produce zero activity/outbox rows.
- [ ] Commit checkpoint 2 separately and report behavior/tests/open risks before starting deletion work.

## Checkpoint 3: permanent deletion, Web transport, full acceptance

### Task 3.1: Define closed deletion adapter/readiness contracts

**Files:**
- Create: `internal/center/recorddeletion/types.go`
- Create: `internal/center/recorddeletion/types_test.go`
- Create: `internal/center/recorddeletion/registry.go`
- Create: `internal/center/recorddeletion/registry_test.go`

- [ ] Write RED tests for exact adapter names/surfaces, duplicate/missing/extra adapters, health snapshots and complete production readiness.
- [ ] Implement the closed exact set `record_core|record_attachments|record_evidence|record_markdown_client|record_search|record_activity_projection|record_comparison|record_collaboration|record_portability`. The production set stays incomplete until later children register; test fixtures may explicitly supply all nine adapters.
- [ ] Prove core-only or empty-table states keep permanent-delete capability false and return `deletion_safety_unavailable` without reservation/ledger mutation.

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

- [ ] Write state/cut-point RED tests for preview CAS, authorization revocation, dependency drift, reservation/lease drain, ledger commit unknown, witness pending, permanent fence, retry, `attempt_not_committed`, same-key replay/reuse and content-free receipt.
- [ ] Implement preview/execute/status/worker over Child 1 reservation, ledger, witness, lease and fence interfaces; never infer deletion outcome from timeout or transport error.
- [ ] Implement core purge exact ownership and verified absence. Recovery adapter replays only root/revision/draft/checkpoint/relations/reservation/outcome/minimal audit; unknown contracts fail closed.
- [ ] Add real PostgreSQL concurrency tests proving reservation-after core new reads/writes are zero, not-committed releases safely and stale workers cannot resurrect content.
- [ ] Run `go test -race ./internal/center/recorddeletion ./internal/center/store -run 'Deletion|Purge|Recovery|Reservation' -count=10`; expect GREEN.

### Task 3.3: Add deletion HTTP contracts

**Files:**
- Create: `internal/center/http/handlers/record_deletions.go`
- Create: `internal/center/http/handlers/record_deletions_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] Write RED matrices for preview/execute/status authorization, no-leak 404, stale 409, unavailable 503, pending 202, `not_committed` 200, same operation replay and response allowlists.
- [ ] Wire production readiness so incomplete later adapters expose no permanent-delete capability and no token; do not create a test-only bypass in production bootstrap.
- [ ] Run focused handler/router/bootstrap tests; expect GREEN.

### Task 3.4: Add lazy Web DTO and transport contract

**Files:**
- Modify: `web/src/lib/types.ts`
- Create: `web/src/lib/recordsApi.ts`
- Create: `web/src/lib/recordsApi.test.ts`
- Modify: `web/src/lib/apiRequest.ts`
- Modify: `web/src/lib/apiRequest.test.ts`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/lib/api.test.ts`
- Create: `web/src/security/recordsTransportArchitectureContract.test.ts`
- Modify: `web/src/security/bundleBudgetContract.test.ts`

- [ ] Write RED tests fixing all Child 2 URLs, methods, cursor/query normalization, `If-Match`, `Idempotency-Key`, allowlisted responses and 404/409/503 recovery shapes.
- [ ] Move the existing `withQuery` implementation from `api.ts` into `apiRequest.ts`, preserve all legacy API tests, and extend `ApiError` with allowlisted `code`, field errors and recovery while retaining status/message compatibility.
- [ ] Implement the façade by reusing `requestJSON`, body helpers and `withQuery` from `apiRequest.ts`; no raw `fetch`, React, route or UI code.
- [ ] Add an AST/source RED test forbidding imports from `web/src/app/layout/AppShell.tsx`, `TopBar.tsx`, `Sidebar.tsx`, `web/src/lib/api.ts` and the eager router dependency graph. Because Child 2 creates no Records page, the production build must tree-shake the unconsumed transport from all current chunks; a test fixture with a synthetic lazy consumer proves it enters only that lazy chunk.
- [ ] Run `env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts` plus lint/build/bundle tests; expect GREEN.

### Task 3.5: Full acceptance and PR closure gate

- [ ] Run all focused race tests and repository PostgreSQL migration/records/deletion integration suites.
- [ ] Run `HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.12 scripts/test-record-platform-integration.sh pg16-catalog -- go test -json ./internal/center/store/migrate -run '^(TestPostgresIntegrationAppACLR2|TestPostgresIntegrationAppACLCurrent)$' -count=1`; require both anchors GREEN with no `SKIP`.
- [ ] Run `make verify-go`.
- [ ] Run `env PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web`.
- [ ] Run `git diff --check` and scan production code for legacy `experience_logs` access or forbidden eager `recordsApi` imports.
- [ ] Load and run `trellis-check`; fix spec drift, type/lint/test/data-flow/reuse/bundle findings on the same branch.
- [ ] Commit checkpoint 3 separately, push, monitor Draft PR required CI and keep the PR unmerged until all three checkpoints and review findings are coherent.

## Rollback

- Before merge, revert only the failing checkpoint commit on the feature branch or correct `0052` in place; never modify local/remote `main` directly.
- Feature default-off prevents new Records capability exposure while diagnosing application failures.
- Returning a development environment to code without `0052` requires rebuilding that development database; no down migration or legacy compatibility path is added.
- Deletion reservations/outcomes are never manually cleared on uncertainty; recovery follows witnessed delete/outcome state and stays fail closed when proof is unavailable.
