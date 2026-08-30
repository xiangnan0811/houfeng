# Agent 心跳幂等写入权限修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:test-driven-development` for every behavior change and `superpowers:verification-before-completion` before any success claim. Do not run `task.py start`, edit product code, commit, push, open a PR, merge, release, or deploy until the user explicitly approves the corresponding scope.

**Goal:** 让 current APP ACL 的 INSERT-only runtime 角色可以持久化 agent 同步批次，使 v0.79.2 agent 的现有 durable retry 自动恢复心跳。

**Architecture:** 保留现有 `ApplyBatch` 事务和幂等表；只把显式复合 conflict target 改为 targetless `ON CONFLICT DO NOTHING`，并以真实 PostgreSQL 16 direct-runtime test 证明首次/重复批次和最小权限同时成立。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL 16、Trellis、现有 record-platform integration runner。

---

## Risk boundaries

- 不修改 migration、APP ACL manifest、agent/proxy/Compose、HTTP handler/DTO、Web 或生命周期语义。
- `agent_sync_batches` 继续只有 INSERT；不得增加 table/column SELECT。
- targetless form 只在当前唯一约束集合不变时等价；任何新 unique constraint 必须触发重新设计。
- 真实 PostgreSQL RED/GREEN 是必需证据；fake tx 与 catalog admission 不能单独证明生产 DML 可执行。
- 所有日志/测试输出保持脱敏，不打印 token、Authorization、DSN、raw fingerprint 或请求体。

## TDD execution checklist

- [x] **1. 启动已批准任务并建立基线**
  - 用户批准实施后运行：

    ```bash
    python3 ./.trellis/scripts/task.py start .trellis/tasks/08-30-agent-heartbeat-onboarding-failure
    git status --short --branch
    GOTOOLCHAIN=go1.26.2 go test ./internal/center/store ./internal/center/store/migrate ./internal/center/http/handlers -count=1
    ```

  - 确认当前分支是 `codex/agent-heartbeat-onboarding-failure`、hooks 已启用，dirty state 只包含本任务文件。

- [x] **2. RED — 单元测试冻结无冲突目标 SQL**
  - 修改 `internal/center/store/sync_batches_test.go` 的 `TestPostgresSyncRepositoryRecordsBatchIDBeforeWritingFacts`，在现有顺序与参数断言后增加：

    ```go
    batchSQL := strings.ToLower(tx.execSQL[recordIndex])
    if !strings.Contains(batchSQL, "on conflict do nothing") {
        t.Fatalf("agent sync batch SQL must use targetless ON CONFLICT DO NOTHING")
    }
    if strings.Contains(batchSQL, "on conflict (") {
        t.Fatalf("agent sync batch SQL must not name conflict columns under INSERT-only ACL")
    }
    ```

  - 运行：

    ```bash
    GOTOOLCHAIN=go1.26.2 go test ./internal/center/store -run '^TestPostgresSyncRepositoryRecordsBatchIDBeforeWritingFacts$' -count=1
    ```

  - 预期旧代码 RED：缺少 targetless form，且仍包含显式 conflict target。

- [x] **3. RED — 真实 runtime ACL 执行 production repository**
  - 新建 `internal/center/store/sync_batches_postgres_integration_test.go`，package 保持 `store`，测试名固定为 `TestPostgresIntegrationAgentSyncBatchRuntimeACL`。
  - 使用 `newRecordsPostgresFixture(t, ctx)` 应用完整迁移、收敛 current ACL 并完成 direct-runtime admission。
  - owner fixture seed：

    ```go
    const (
        monitoringInstanceID = "mi_sync_batch_acl"
        syncToken = "sync-token-acl-fixture"
        fingerprint = "fingerprint-acl-fixture"
        syncBatchID = "sync_batch_acl"
    )
    receivedAt := time.Date(2026, time.August, 30, 3, 30, 0, 0, time.UTC)
    if _, err := fixture.db.Exec(ctx, `
        insert into public.monitoring_instances (
            monitoring_instance_id, display_name, region, city, provider,
            lifecycle_status, monitoring_status, binding_status,
            binding_fingerprint, sync_token_hash
        ) values ($1, 'Sync batch ACL fixture', '', '', '', $2, $3, $4, $5, $6)`,
        monitoringInstanceID,
        monitoringinstances.LifecyclePendingEnrollment,
        monitoringinstances.MonitoringEnabled,
        monitoringinstances.BindingBound,
        fingerprint,
        hashSyncToken(syncToken),
    ); err != nil {
        t.Fatal("seed bound monitoring instance")
    }
    ```

  - 先用 owner 查询 `has_table_privilege`，断言 runtime 的 INSERT 为 true，SELECT/UPDATE/DELETE 全为 false；不得以临时 grant 让测试通过。
  - 通过 `fixture.openDirectRuntimePool` 构造 `NewPostgresSyncRepository`，固定 `repo.now`，提交只含一个 heartbeat 的 `syncing.Batch`。失败时声明 `var pgErr *pgconn.PgError` 并用 `errors.As(err, &pgErr)` 识别 SQLSTATE，但 failure 文本只输出 code，不输出 token/DSN/raw error。
  - 原样再调用一次 `ApplyBatch`，断言 duplicate 返回完整空 plan；最后由 owner 查询并断言：batch 行数 1、heartbeat 行数 1、`last_heartbeat_at` 等于 carrier time、`last_sync_at` 等于固定 receive time。
  - 运行旧代码：

    ```bash
    GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- \
      go test -v ./internal/center/store \
      -run '^TestPostgresIntegrationAgentSyncBatchRuntimeACL$' -count=1
    ```

  - 预期 RED：第一次 `ApplyBatch` 的 wrapped PostgreSQL cause 为 SQLSTATE `42501`；runner 不得出现 SKIP。

- [x] **4. GREEN — 最小生产 SQL 修复**
  - 修改 `internal/center/store/sync_batches.go` 的 `recordAgentSyncBatch`，不改函数签名、参数、错误包装或调用顺序：

    ```go
    // Keep the conflict target implicit so the INSERT-only runtime role does
    // not need SELECT on the idempotency key columns.
    tag, err := tx.Exec(ctx, `
        insert into agent_sync_batches (
            monitoring_instance_id,
            sync_batch_id
        ) values (
            $1,
            $2
        )
        on conflict do nothing`,
        batch.MonitoringInstanceID,
        batch.Heartbeats[0].SyncBatchID,
    )
    ```

  - 不修改 `acl_manifest_allowlist.go`、`0045_create_agent_sync_batches.sql` 或任何其他 SQL。
  - 重跑步骤 2 的 focused unit，确认 GREEN。

- [x] **5. GREEN — 首次、重复、状态推进与 ACL 不扩张**
  - 重跑步骤 3 的 strict PostgreSQL command，确认首次与重复 `ApplyBatch` 均成功、事实各一行且实例时间推进。
  - 在同一 integration test 最后再次断言 runtime privilege vector，防止 fixture 或 production convergence 意外授予 SELECT。
  - 运行 existing duplicate/suppression/token/binding regressions：

    ```bash
    GOTOOLCHAIN=go1.26.2 go test ./internal/center/store \
      -run 'Test(PostgresSyncRepository|SyncBatch).*(Batch|Heartbeat|Token|Binding|Suppress|Lifecycle)' \
      -count=1
    ```

- [x] **6. 同步 executable database spec**
  - 在 `.trellis/spec/backend/database-guidelines.md` 的 MonitoringInstance/sync ingestion 相邻区域新增 scenario，记录：
    - `agent_sync_batches` 是 INSERT-only idempotency surface；
    - 显式 conflict target 在 PostgreSQL 16 下需要读冲突键，与 ACL 不兼容；
    - production SQL 固定为 targetless `ON CONFLICT DO NOTHING`；
    - 当前只有复合主键唯一约束；新增 unique constraint 必须重审 ignore-all-conflicts 语义；
    - 真实 direct-runtime PostgreSQL test 是强制 regression，fake tx/catalog-only test 不足。
  - 不修改 error mapping；`error-handling.md` 已正确规定未知 store error -> agent `500 internal_error` 与 agent 5xx retry。
  - 在 `internal/center/http/handlers/agent_test.go` 以 secret-bearing store error 覆盖未知错误边界，断言 HTTP 只返回固定 `500`、`internal_error` 和 `internal server error`，且 response 不包含原始 store detail。

- [x] **7. 全量验证与范围审查**
  - 依次运行：

    ```bash
    GOTOOLCHAIN=go1.26.2 go test ./internal/center/store ./internal/center/store/migrate ./internal/center/http/handlers ./internal/center/syncing -count=1
    GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- \
      go test -v ./internal/center/store \
      -run '^TestPostgresIntegrationAgentSyncBatchRuntimeACL$' -count=1
    GOTOOLCHAIN=go1.26.2 make verify-go
    git diff --exit-code -- db/migrations internal/center/store/migrate/acl_manifest_allowlist.go
    git diff --check
    git status --short --branch
    ```

  - 用 `trellis-check` 对照 PRD、design、database/error/quality specs，重点审查权限未扩大、幂等未漂移、真实 PG 非 skip、未来 unique 风险与 diff scope。
  - 在声称完成前使用 `superpowers:verification-before-completion` 核对所有命令的当次输出。

- [ ] **8. 受保护交付与测试环境恢复证明（需相应明确授权）**
  - 在用户批准交付后才 commit、push feature branch、创建 PR；required CI 全绿后合并，继续监控 main CI、Release Please、release 与多架构 Center image 发布。
  - 测试环境升级固定 release image 并只重建/重启 Center；不得执行手工 GRANT、agent 重装或数据库数据修补。
  - 观察两个既有 v0.79.2 agent 的后续 retry：Center 不再返回 500，PostgreSQL 不再出现 `agent_sync_batches` 42501，批次/heartbeat/last-sync 事实实际推进。
  - 报告恢复证据与仍存在的无关告警；PR 创建或 systemd active 本身不算问题已修复。

## Execution evidence (2026-08-30)

- Baseline: hooks configured on `codex/agent-heartbeat-onboarding-failure`; `go test ./internal/center/store ./internal/center/store/migrate ./internal/center/http/handlers -count=1` passed.
- Unit RED:
  - Command: `GOTOOLCHAIN=go1.26.2 go test ./internal/center/store -run '^TestPostgresSyncRepositoryRecordsBatchIDBeforeWritingFacts$' -count=1`
  - Expected failure: `agent sync batch SQL must use targetless ON CONFLICT DO NOTHING`.
- PostgreSQL 16 RED:
  - Command: `GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationAgentSyncBatchRuntimeACL$' -count=1`
  - Expected failure: `first ApplyBatch PostgreSQL SQLSTATE = 42501, want success`; runner exited non-zero with no `SKIP` and no secret, DSN, fingerprint, or raw database error in the test failure.
- Unit GREEN: the focused SQL-shape command passed after changing only the conflict clause.
- PostgreSQL 16 GREEN: the strict direct-runtime command passed twice after the fix; the test proves INSERT-only privileges before/after, first and duplicate `ApplyBatch` success, one batch row, one heartbeat row, and exact timestamp advancement.
- Related regressions passed:
  - `GOTOOLCHAIN=go1.26.2 go test ./internal/center/store -run 'Test(PostgresSyncRepository|SyncBatch).*(Batch|Heartbeat|Token|Binding|Suppress|Lifecycle)' -count=1`
  - `GOTOOLCHAIN=go1.26.2 go test ./internal/center/store ./internal/center/store/migrate ./internal/center/http/handlers ./internal/center/syncing -count=1`
  - `GOTOOLCHAIN=go1.26.2 make verify-go`
- Scope commands passed: protected migration/ACL-manifest diff was empty and `git diff --check` reported no errors.
- Reviewer finding/fix: `quality-guidelines.md` no longer claims every store test is fake or reserves real PostgreSQL testing for one filename/Slice 7 exception. It now distinguishes fake-by-default unit tests from isolated `*_postgres_integration_test.go` coverage and requires strict RUN/PASS with SKIP-as-failure whenever a database/package spec names an acceptance gate.
- Independent review: specification review and code-quality review both passed after their findings were fixed and re-reviewed.
- Main-session fresh verification: focused SQL-shape unit, strict PostgreSQL direct-runtime integration (actual RUN/PASS, no SKIP), related four packages, `make verify-go`, protected migration/ACL-manifest diff, and `git diff --check` all exited 0.
- Second security/scope review fixed four evidence gaps: added an opaque agent `500 internal_error` regression for a secret-bearing store failure; tightened unit and real-PostgreSQL duplicate regressions to require an exact empty plan; aligned the executed fixture snippet with source while removing its raw PostgreSQL `%v` failure output; and reconciled completed implementation AC checkboxes while leaving release recovery unchecked.
- Second-review fresh verification passed: focused store SQL/typed-cause, handler error mapping and agent 429/5xx durable-retry tests; strict PostgreSQL 16 direct-runtime RUN/PASS with no SKIP; related four packages; `make verify-go`; focused `-race -count=10`; credential-pattern scan; protected migration/ACL/agent/DTO/handler-product/Web/lifecycle/Compose/runner diff; `git diff --check`; and runner container/workspace cleanup checks.
- Final full-scope review fixed the remaining handler privacy-test gap by scanning the exact response envelope, body and headers for store, token and fingerprint details; its full planned/focused, strict PostgreSQL `-count=2`, race, `make verify-go`, format, scope, task syntax and cleanup gates passed with zero remaining finding.
- Break-loop analysis is recorded in `break-loop.md`; database/quality specs and the cross-layer thinking guide now preserve the static-ACL-versus-production-DML lesson. The repository has no `src/templates/markdown/spec/` tree, so no template mirror exists to sync.
- Full delivery, release, test-environment recovery and final cleanup are now explicitly authorized and proceed under the protected branch workflow; step 8 remains unchecked until field facts and cleanup are complete.

## Planned validation commands

```bash
GOTOOLCHAIN=go1.26.2 go test ./internal/center/store -run '^TestPostgresSyncRepositoryRecordsBatchIDBeforeWritingFacts$' -count=1
GOTOOLCHAIN=go1.26.2 go test ./internal/center/http/handlers -run '^TestAgentSyncHandlerReturnsStableInternalErrorWithoutStoreDetails$' -count=1
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run '^TestRuntimeTransientQueueFailuresRemainRetryableAndBackfilled$' -count=1
GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationAgentSyncBatchRuntimeACL$' -count=1
GOTOOLCHAIN=go1.26.2 go test ./internal/center/store ./internal/center/store/migrate ./internal/center/http/handlers ./internal/center/syncing -count=1
GOTOOLCHAIN=go1.26.2 make verify-go
git diff --exit-code -- db/migrations internal/center/store/migrate/acl_manifest_allowlist.go
git diff --check
```

`postgres` runner 的 skip 会使命令失败；因此该 command 是本任务真实 PostgreSQL 16 acceptance evidence，而不是可选 smoke。
