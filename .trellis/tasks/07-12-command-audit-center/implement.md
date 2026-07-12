# Global Command Audit Center Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. This repository uses Codex inline execution; do not dispatch sub-agents. Every production behavior follows RED → verify RED → minimal GREEN → verify GREEN → refactor.

**Goal:** Deliver a permanent, metadata-only global command audit query/API/UI with trusted sensitive-command rejection auditing and deletion-safe identity snapshots.

**Architecture:** Upgrade the append-only audit table, preserve the three transactional write paths behind one snapshot-aware helper, expose a separate read model with a fixed-bound keyset cursor, and add a lazy private React page whose controller/filter/table/timeline responsibilities are split. Use two bounded SQL queries per page, never one query per action.

**Tech Stack:** PostgreSQL migrations and pgx/v5, Go `net/http`, React 19 + TypeScript + React Router, Vitest/Testing Library, Playwright.

---

## Preflight gate

- [x] Fetch `origin/main`; compare drift from `a375c0b0`; confirm latest migration is 0049.
- [x] Enable versioned hooks with `sh scripts/setup-git-hooks.sh`.
- [x] Create `codex/command-audit-center` from `origin/main` in the clean primary checkout.
- [x] Create P2 task `.trellis/tasks/07-12-command-audit-center` and GitHub enhancement #381 for future RBAC.
- [x] Review `prd.md`, `design.md`, and this file; run `task.py start` only after no gaps remain.
- [x] Run `trellis-before-dev` and read every backend/web checklist document referenced by the applicable spec indexes.

## Task 1: Migration contract and PostgreSQL upgrade

**Files:**

- Create: `db/migrations/0050_extend_command_action_audit.sql`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

- [x] Add a focused migration text test asserting the three snapshot columns/defaults, action nullable transition, dynamic FK removal, named constraints, global index, and absence of destructive audit deletes. Run `go test ./internal/center/store/migrate -run CommandActionAudit -count=1`; verify RED because 0050 is missing.
- [x] Add 0050 with idempotent `ADD COLUMN IF NOT EXISTS`, empty-only backfill, FK removal, `DROP NOT NULL`, guarded named constraints, and `CREATE INDEX IF NOT EXISTS`; rerun focused test GREEN.
- [x] Add env-gated PostgreSQL test starting from migrations through 0046, seeding old audit rows, applying 0047–0050 twice, and asserting backfill/schema constraints plus an old-style INSERT. Run with `HOUFENG_POSTGRES_INTEGRATION=1 go test ./internal/center/store/migrate -run CommandActionAuditUpgrade -count=1`; verify RED before fixture/helper support, then GREEN.
- [x] Extend the real test to delete actor and monitoring instance, verify audit remains, and prove rejected/non-rejected action identity plus output-detail constraints fail closed.
- [x] Run `gofmt` on changed Go tests and rerun the package.

## Task 2: Snapshot-aware write helper and trusted rejection

**Files:**

- Modify: `internal/center/store/command_actions.go`
- Create: `internal/center/store/command_actions_test.go`
- Modify: `internal/center/store/monitoring_instances.go`
- Modify: `internal/center/store/monitoring_instances_test.go`
- Modify: `internal/center/store/sync_batches_test.go`
- Modify: `internal/center/monitoringinstances/types.go`
- Modify: `internal/center/http/handlers/monitoring_instance_actions.go`
- Modify: `internal/center/http/handlers/monitoring_instances_test.go`

- [x] Write helper tests that require `INSERT … SELECT` snapshots, optional real actor validation, exact one-row enforcement, rejected reason, and unsupported event rejection. Verify RED against the current VALUES helper.
- [x] Extend `commandActionAuditEvent` with details/rejection semantics; implement the single snapshot-aware helper and make RowsAffected != 1 an integrity error. Verify helper tests GREEN.
- [x] Update queued/dispatched/completed SQL expectations to prove all three still call the helper after their corresponding state update and roll back on helper error; run focused store tests GREEN.
- [x] Add handler tests for: trusted rejection writes once and returns 400; no action ID/queue; missing instance/archived/unbound/paused return confirmation 400 without audit; invalid JSON/unknown/standard behavior; lookup/audit failures return 500. Verify RED.
- [x] Add `RecordRejectedCommandAction` to the repository contract and implement the pre-action trusted-rejection state machine without changing confirmed/standard behavior. Verify handler/store tests GREEN.
- [x] Add `command_action_audit_count` tests to management review/evidence and permanent cleanup, including audit preservation and exclusion from deleted reference count. Verify RED, implement the type/query/evidence changes, then GREEN.

## Task 3: Read model, filters, outcomes, and stable keyset

**Files:**

- Create: `internal/center/commandaudits/types.go`
- Create: `internal/center/store/command_audits.go`
- Create: `internal/center/store/command_audits_test.go`

- [x] Write store tests defining the wished-for `ListCommandAudits(ctx, commandaudits.Query)` contract and asserting literal LIKE escaping, fixed time bounds, exact filters, limit+1, composite keyset, and only two queries. Verify compile/test RED because the package/repository is absent.
- [x] Implement minimal domain types with no `Details`, `Stdout`, or `Stderr` fields and a PostgreSQL repository constructor.
- [x] Implement query 1 for queued/rejected starters, five-outcome precedence, left-joined existence, action sorting/filtering/keyset; map snapshot fallbacks in Go. Run focused tests GREEN.
- [x] Add RED tests for nested event ordering and rejected audit-ID grouping; implement one collection query for all returned actions and attach allowlisted events. Verify GREEN and assert query count remains constant.
- [x] Add tests for query/scan/rows errors at both stages, empty results, same-time IDs, completed exit 0/nonzero, dispatched-only and queued-only outcomes; implement minimal error handling and rerun GREEN.

## Task 4: Session API, opaque cursor, router, and bootstrap

**Files:**

- Create: `internal/center/http/handlers/command_audits.go`
- Create: `internal/center/http/handlers/command_audits_cursor.go`
- Create: `internal/center/http/handlers/command_audits_test.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [x] Write handler RED tests for default 30d/20, every initial filter, all/custom bounds, exact fixed `Now`, limit 1..100, malformed inputs, wrong method, repository errors, and response allowlisting.
- [x] Implement initial query normalization with deterministic `Now` injection and JSON response; verify handler tests GREEN.
- [x] Write cursor RED tests for RawURLEncoding round-trip, version, normalized filters/bounds/limit/last key, cursor-only continuation, corrupt/unknown/invalid payloads, stable same-time pagination, and new events outside the fixed upper bound.
- [x] Implement private cursor codec/validation and next-cursor generation from `HasMore`; verify GREEN.
- [x] Add router RED tests proving `/api/command-audits` is fail-closed without auth middleware and protected when configured; wire `CommandAuditsHandler` and verify GREEN.
- [x] Add bootstrap source-contract RED assertions for a separate command-audit repository and handler; construct/wire them and verify bootstrap tests GREEN.

## Task 5: Shared Web contract and API client

**Files:**

- Create: `web/src/config/commands.ts`
- Create: `web/src/config/commands.test.ts`
- Modify: `web/src/pages/monitoring-detail/monitoringDetailConstants.ts`
- Modify: `web/src/pages/monitoring-detail/types.ts`
- Modify: `web/src/pages/monitoring-detail/MonitoringDetailPageBody.tsx`
- Modify: `web/src/lib/types.ts`
- Modify: `web/src/lib/api.ts`
- Create: `web/src/lib/observabilityApi.ts`（full bundle gate 时由 eager façade 迁出 route-lazy helpers）
- Modify: `web/src/lib/api.test.ts`

- [x] Write RED tests for shared command labels/options/sensitivity and migrate monitoring detail assertions to import them.
- [x] Move the existing command type/list/labels into `config/commands.ts`, keep monitoring detail behavior unchanged, and verify focused tests GREEN.
- [x] Add Web response types that enumerate only audit metadata/events, then write API-client RED tests for default no-query request, normalized initial query, and cursor-only continuation.
- [x] Implement `listCommandAudits` using the existing `withQuery`/`requestJSON` path and verify GREEN; full bundle gate 后将其与仅由 lazy route 消费的 event/incident helpers 收敛到 `observabilityApi.ts`，wire shape 不变；include a type/source assertion that no stdout/stderr audit fields exist.

## Task 6: Command audit controller, filters, table, and timeline

**Files:**

- Create: `web/src/pages/CommandAuditPage.tsx`
- Create: `web/src/pages/CommandAuditPage.test.tsx`
- Create: `web/src/pages/command-audit/types.ts`
- Create: `web/src/pages/command-audit/filterModel.ts`
- Create: `web/src/pages/command-audit/filterModel.test.ts`
- Create: `web/src/pages/command-audit/CommandAuditFilterPanel.tsx`
- Create: `web/src/pages/command-audit/CommandAuditFilterDrawer.tsx`
- Create: `web/src/pages/command-audit/CommandAuditTable.tsx`
- Create: `web/src/pages/command-audit/CommandAuditEventTimeline.tsx`
- Modify: `web/src/styles/partials/legacy-observability.css`
- Modify: `web/src/styles/partials/legacy-events.css`

- [x] Write filter-model RED tests for default URL omission, non-default/custom serialization, trimming, invalid canonicalization, and filter-key stability; implement pure parsing/normalization/serialization and verify GREEN.
- [x] Write page RED tests for default request, URL canonicalization, loading/error/empty, primary filter apply, advanced draft apply/cancel/reset, and stale response protection.
- [x] Implement the controller and split filter components using existing atoms/filter patterns; verify GREEN.
- [x] Write RED tests for load-more cursor append, filter-change reset of cursor/results/expanded IDs, same ID dedupe, and no request when cursor absent; implement and verify GREEN.
- [x] Write RED rendering tests for five outcomes, command labels, actor fallback, current/deleted instances, semantic expand/collapse, event ordering, malicious stdout/stderr fields, keyboard activation, and local horizontal scroll wrapper.
- [x] Implement DataTable summary and allowlisted timeline; add token-based styles only to existing CSS owners; verify component/page tests GREEN.

## Task 7: Private route, sidebar, detail entry, and cleanup copy

**Files:**

- Modify: `web/src/app/router.tsx`
- Modify: `web/src/app/router.test.tsx`
- Modify: `web/src/app/layout/Sidebar.tsx`
- Modify: `web/src/app/layout/Sidebar.test.tsx`
- Modify: `web/src/components/monitoring-detail/MonitoringInstanceWatchtowerHeader.tsx`
- Modify: `web/src/pages/MonitoringDetailPage.test.tsx`
- Modify: `web/src/pages/monitoring-detail/MonitoringInstanceManagementSection.tsx`
- Modify: `web/src/lib/types.ts`

- [x] Add RED route/sidebar tests for lazy private `/command-audit`, “观测” placement, active state, and unauthenticated redirect; wire the route/item and verify GREEN.
- [x] Add RED detail-header test for `/command-audit?monitoring_instance=<id>` navigation; add a menu/link action without changing command execution and verify GREEN.
- [x] Extend management-review UI tests for audit count and permanent-retention confirmation copy; update types/presentation and verify GREEN.

## Task 8: Browser fixtures, route matrices, docs, and specs

**Files:**

- Modify: `web/e2e/fixtures/contracts.ts`
- Modify: `web/e2e/fixtures/profiles.ts`
- Modify: `web/e2e/fixtures/router.ts`
- Modify: `web/e2e/fixture-router.spec.ts`
- Modify: `web/e2e/core-routes.spec.ts`
- Modify: `web/e2e/accessibility.spec.ts`
- Modify: `web/e2e/staging/staging-smoke.spec.ts`
- Modify: `docs/operations/ui-preview-and-browser-sanity.md`
- Modify: applicable files under `.trellis/spec/backend/`, `.trellis/spec/web/`, and `.trellis/spec/guides/`

- [x] Add fail-closed command-audit fixture tests first; verify RED, implement fixture contract/profile/router response, then GREEN.
- [x] Add `/command-audit` to the core route table and update exact 9×3 expectations to 10×3; add audit-specific accessibility and narrow viewport interaction assertions, then run the focused Playwright specs.
- [x] Add staging smoke presence/metadata-only assertions that do not require audit data; update browser-sanity route matrix and evidence wording.
- [x] Use `trellis-update-spec` to capture database audit contracts, Web state/data rules, page ownership, and browser verification requirements; run doc/spec checks.

## Task 9: Real PostgreSQL behavior and performance gate

**Files:**

- Modify: `internal/center/store/migrate/postgres_integration_test.go`
- Create or modify: focused PostgreSQL command-audit integration test under `internal/center/store/`
- Record evidence: `.trellis/tasks/07-12-command-audit-center/check.md` (generated/maintained during quality gate)

- [x] Seed multiple actions with equal timestamps, all outcomes, deleted users/instances, escaped filter characters, and events outside page boundaries; run real store/API queries and verify all filters/cursors/order.
- [x] Permanently clean an archived instance, call the global read path, and verify snapshot/deleted rendering data survives.
- [x] Run fresh-install and 0046-upgrade smoke twice against real PostgreSQL.
- [x] Run `EXPLAIN (ANALYZE, BUFFERS)` for representative default-window and outcome-filtered queries; require index-bounded candidates, limit enforcement, and exactly two application queries. If unacceptable, stop and revise `design.md` before continuing.

## Task 10: Full quality gate and PR delivery

- [x] Run `trellis-check` and resolve every spec, consistency, lint, type, test, cross-layer, reuse, and context-drift finding with a reproducing test.
- [x] Run `HOUFENG_POSTGRES_INTEGRATION=1` PostgreSQL suites and fresh/upgrade smoke; preserve exact results separately from unit results.
- [x] Run `make verify-go`.
- [x] Run `make verify-web`.
- [x] Run `npm --prefix web run test:e2e`.
- [x] Run `git diff --check` and inspect `git diff --stat`, `git status`, and the final diff for accidental output fields or unrelated changes.
- [x] Update task check evidence; keep the task active because staging/PR/required CI have not run.
- [ ] Commit the post-initial-commit review fixes and developer journal locally on `codex/command-audit-center`.
- [ ] Push/create PR/monitor required CI only after the user gives new authorization. The current explicit instruction forbids push, so these original delivery steps are suspended rather than silently treated as passed.
- [ ] After future required CI passes, stop without merge, release, deleting the old planning branch, or cleaning up the feature branch unless separately authorized.

Full-gate findings already resolved:

- [x] CSS budget RED：组合现有 observability/events/shared primitives，消除新增 rule/declaration/repeated-selector debt，并将 source/raw/gzip budgets 向下收紧到 fresh build 实测值。
- [x] Entry JS budget RED：把仅由 lazy route 消费的 observability endpoint façade 移出 eager `api.ts`，简化 audit lazy registration/icon，并将 entry budget 从 110742 向下收紧到 fresh build 实测 110738；未提高 max-async budget。
- [x] 提交后迁移审查：0050 只删除包含实例/actor 列的目标 FK，真实 PostgreSQL 证明无关 FK 保留；三种旧式 INSERT、三个索引和重复迁移均通过。
- [x] 提交后拒绝竞态审查：rejected 的单条 `INSERT … SELECT` 增加未归档、已绑定、未暂停状态门；状态变化时 0 行并返回 500，不静默降级。
- [x] 提交后 API 审查：handler 自有嵌套 response DTO，逐字段复制实例与 actor，避免领域类型未来扩展时扩大 JSON。
- [x] 提交后 Web 审查：严格日历日期、加载更多失败保留原结果/cursor、named-only lazy module、宽表可见标题/提示/键盘 region 和 hostile details/output 不渲染均有回归证据。
- [x] 提交后证据审查：真实 cursor 测试在第一页之后插入上界外 action；临时 PostgreSQL database 清理失败会使测试失败；本地视觉截图只写 `/tmp` 并已删除。

## User-directed delivery override

- 2026-07-12 用户要求完成本地提交后不得 push，并要求先做全方位审查。
- 因此本轮允许的终点是：本地 feature branch 提交 + 本地单元/真实 PostgreSQL/fixture Chromium/人工视觉证据 + 提交后复核。
- staging smoke、push、PR、required CI、merge、release 和远端分支清理均未获本轮授权；不得用本地证据替代这些层，也不得为满足旧 checkbox 违反最新用户指令。

## Rollback points

- After Task 1: code is still not wired; migration can be revised safely before PR. Never restore cascading FKs automatically once orphan audit rows may exist.
- After Task 4: API is session protected but Web is not linked; revert router/bootstrap wiring if read-model performance fails.
- After Task 7: the page is reachable; fixture/browser gates become mandatory before delivery.
- Any real PostgreSQL correctness or EXPLAIN failure rolls the task back to design, not forward to release.

## Plan self-review

- Requirement coverage: every PRD section maps to Tasks 1–9 and final delivery maps to Task 10.
- Placeholder scan: no TBD/TODO/“similar to” steps remain; each behavior has a test target and verification command or owning quality gate.
- Type consistency: backend uses `commandaudits.Query/Page/Action/Event`; Web uses explicit `CommandAudit*` metadata-only types; API path is consistently `/api/command-audits`; page path is consistently `/command-audit`.
- Scope control: RBAC/export/retention/summary table/automatic merge remain excluded.
