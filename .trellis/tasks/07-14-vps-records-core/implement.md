# 记录、修订、草稿与状态核心 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline only. Use RED → verify RED → minimal GREEN → verify GREEN.

**Goal:** 实现可完整重建、不可静默覆盖并受删除 fence 保护的记录/修订/草稿核心。

**Architecture:** records domain 编排强一致 revision transaction；store 提供 pgx tx seam；recorddeletion 注册 core purge adapter；HTTP 和 Web contract 暂不开放正式页面。

**Tech Stack:** Go/pgx/PostgreSQL、React TypeScript contract tests。

---

## 2026-08-02 execution override

- 只从 Child 1 已关闭的 protected main 开始。
- `0052` 与 current APP ACL fragment/admission tests 是同一交付。
- 不做 upgrade fixture、legacy mapping、`experience_logs` 回填/双写或 staging cutover。
- 计划中 “feature off” 只表示开发回滚，不是旧数据兼容承诺。

## Preconditions

- [ ] `07-14-vps-records-platform-foundation` 已合入 main，ledger/auth/outbox/guard contracts 可用。
- [ ] 重新检查 migration 最大号，确认 0052 可用；运行 hooks、`make verify-go` baseline 和 `trellis-before-dev`。

## Task 1: 领域类型、注册表与完整 revision contract

**Files:** Create `internal/center/records/{types,validate,templates}.go` and colocated tests.

- [ ] 写 compile/table RED tests，覆盖全部 revision 字段、kind/role registry、类型状态映射、无状态类型、template provenance/切换 diff。
- [ ] 实现 immutable input/value types；所有 slice/map defensive copy，时间 UTC，Markdown 不在此解析。
- [ ] 运行 `go test ./internal/center/records -run 'Revision|Subject|Template|Status' -count=1` GREEN。

## Task 2: 0052 schema 与真实迁移

**Files:** Create `db/migrations/0052_create_records_core.sql`; modify migrate tests.

- [ ] source test RED：表、FK/unique/check/current pointer、无 cascade source、draft author/recovery indexes。
- [ ] 实现幂等 migration并登记 exact current APP ACL objects/privileges；添加 pgx real fresh/repeat convergence/admission，运行 migration unit + PostgreSQL 16 integration GREEN。

## Task 3: record/revision store transaction

**Files:** Create `internal/center/store/records.go`, `records_test.go`; create `internal/center/records/service.go`, `revisions.go`, tests.

- [ ] fake pgx tx RED tests固定 begin/lock/CAS/insert relations/current/activity/participant/commit 顺序和任一步 rollback。
- [ ] 实现 `RevisionParticipant` registry 与 Create/Revise/Restore；idempotency 复用子任务1。
- [ ] 添加真实 PG tests：并发 base revision 只有一方成功、同 key重试单 revision、无变化返回 `created=false`。
- [ ] `go test -race ./internal/center/records ./internal/center/store -run 'Record|Revision' -count=10` GREEN。

## Task 4: 私有 draft 与 conflict DTO

**Files:** Create `internal/center/store/record_drafts.go`; `internal/center/records/drafts.go`; tests.

- [ ] RED tests覆盖 author isolation、ETag、2 clients conflict、基准推进、5分钟/20个/7天恢复点、90天 TTL 和正式保存清理。
- [ ] 实现 GET/list/create/PATCH/delete service；草稿不写 domain activity/outbox/search。
- [ ] fake clock + real PG GREEN；权限撤销调用平台 cleanup hook。

## Task 5: handlers/router/bootstrap 与 response allowlist

**Files:** Create handlers `records.go`,`record_drafts.go`,`record_deletions.go` + tests; modify router/bootstrap and tests.

- [ ] handler RED matrix覆盖 methods/paths/400/404/409/413/503、If-Match、Idempotency-Key、capabilities 与嵌套 DTO allowlist。
- [ ] 实现 design §19.1 endpoints；router 静态路径优先于 `:id`，session/auth mandatory。
- [ ] bootstrap构造 repositories/services/participants；feature off 时返回 capability unavailable且旧 API不变。
- [ ] 运行 handler/router/bootstrap focused tests GREEN。

## Task 6: core permanent deletion adapter

**Files:** Create `internal/center/recorddeletion/{types,service,worker,recovery_adapter}.go`, store `record_deletions.go`, tests including `recovery_adapter_test.go`.

- [ ] RED state/cutpoint tests覆盖 preview CAS、撤权、reservation、lease drain、ledger unknown、attempt_not_committed、core purge receipt、adapter missing。
- [ ] 实现 adapter registry 和 core purge；未注册 material/search/portability adapter 时 execute fail closed。
- [ ] 实现 `recorddeletion.NewRecoveryAdapter`，只拥有root/revision/draft/recovery-point/relations/reservation/outcome/minimal-audit重放；unknown contract拒绝，projection/material留给各owner。
- [ ] 真实 PG并发测试证明 reservation 后 core 新读/写为0，同 key not_committed不删除。

## Task 7: Web contract façade

**Files:** Modify `web/src/lib/types.ts`; create `web/src/lib/recordsApi.ts`, `recordsApi.test.ts`; modify `apiRequest.ts` only for stable error code/field/recovery shape; extend bundle/import architecture tests.

- [ ] RED tests固定 URL、headers、cursor/query、404/409/503 error code 和无多余字段。
- [ ] 实现 lazy-consumer façade，复用 `requestJSON/withQuery`，不直接 fetch。
- [ ] 写source/bundle RED test禁止AppShell/TopBar/Sidebar/eager `api.ts`导入`recordsApi`，并证明records transport只出现在lazy chunks；`NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts` 和 build/bundle GREEN。

## Task 8: 质量门与现有功能回归

- [ ] 现有非 Records VPS/renewal/monitoring tests 通过，确认本任务没有写入或读取 `experience_logs`。
- [ ] 运行 PG integration、`make verify-go`、Node22 `make verify-web`、`git diff --check`。
- [ ] `trellis-check` + spec update；提交/PR/required CI，保持 records feature off。

## Rollback

- 关闭 feature 停止新 Records 入口，不执行 down migration。
- 返回不含 `0052` 的代码版本时重建开发数据库；不构造旧 binary 兼容路径。
