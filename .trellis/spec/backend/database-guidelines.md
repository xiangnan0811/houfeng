# 数据库规范

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风 V1 后端持久化栈是 **PostgreSQL + pgx/v5**，没有 ORM、没有 query builder。所有 SQL 都是手写原生语句，迁移文件 (`db/migrations/*.sql`) 是 schema 的**唯一权威源**——不允许通过 ORM auto-migrate、SQL 控制台或运维脚本绕过迁移修改 schema。

核心约定一句话总结：
- **driver**：`github.com/jackc/pgx/v5` 与 `github.com/jackc/pgx/v5/pgxpool`，连接池在 `cmd/houfeng-center/bootstrap.go` 内构造（参见 `bootstrap.go:60-69`，调用 `store.OpenPostgres`）。
- **仓库**：`internal/center/store/` 下一文件一 aggregate（`monitoring_instances.go`、`targets.go`、`incidents.go`、`sync_batches.go` 等）。
- **schema 演进**：`db/migrations/0001_*.sql` … 当前最大 migration（现为 `0029_rename_nodes_to_monitoring_instances.sql`）+ `db/migrations/embed.go` 用 `embed.FS` 嵌入；启动时由 `internal/center/store/migrate/migrate.go` 中的 `Apply` 顺序应用，状态记在 `schema_migrations` 表。
- **事务边界**：写多张表时使用 `pgx.Tx`，参考 `store/sync_batches.go:40-91` 的 `ApplyBatch`（一次同步批次串起 4-5 张表的写入与一次 plan 计算）。
- **不变量**：领域规则（MonitoringInstance/Target/Probe 语义、健康状态派生、回填观测不告警）必须落到 SQL + 仓库 + 服务层共同遵守，详见后文。

---

## Query Patterns

### 基本约定

- **统一通过 `internal/center/store/<aggregate>.go` 访问数据库**。HTTP handler、incident service、retention worker 等都不直接持有 `*pgxpool.Pool`，而是接受领域接口（`monitoringinstances.Repository`、`targets.Repository`、`syncing.Repository` 等）的具体实现。
- 仓库构造器固定签名 `NewPostgres<Aggregate>Repository(*pgxpool.Pool) *Postgres<Aggregate>Repository`（参考 `store/monitoring_instances.go:34-36`、`store/sync_batches.go:30-36`）。
- 每个仓库通过 `var _ <domain>.Repository = (*Postgres<Aggregate>Repository)(nil)` 显式断言接口契约（参见 `store/monitoring_instances.go:70-72`）。

### SQL 写法

- 直接用 `pgx` 的 `Query` / `QueryRow` / `Exec`，参数化占位符使用 `$1, $2, ...`，**严禁字符串拼接 SQL**。
- 复用列清单时定义包级常量字符串（如 `monitoringInstanceSelectColumns`，参见 `store/monitoring_instances.go:38-64`），避免 select / insert 列漂移。
- 错误用 `fmt.Errorf("…: %w", err)` 包装，让上层能 `errors.Is` 判定（典型例子：`store/postgres.go:13-29`）。
- 单条 `select` 用 `QueryRow(...).Scan(...)`，并把 `pgx.ErrNoRows` 显式转换为领域错误或 `bool false`（参考 `cmd/houfeng-center/bootstrap.go:230-247` 的 `hasPersistedSettings`）。

### 事务

- 多表写或写后读必须开事务。参考模板：

  ```go
  tx, err := r.beginTx(ctx, pgx.TxOptions{})
  if err != nil {
      return result, fmt.Errorf("begin <op> transaction: %w", err)
  }
  defer func() { _ = tx.Rollback(ctx) }()
  // ... tx.QueryRow / tx.Exec ...
  if err := tx.Commit(ctx); err != nil {
      return result, fmt.Errorf("commit <op> transaction: %w", err)
  }
  ```

  实例：`store/sync_batches.go:40-91` 把 `validateAcceptedSyncBatch → recordHeartbeatBatch → recordObservationBatch → advanceMonitoringInstanceSyncState → buildSyncPlan` 串在同一个事务内，保证一次 agent sync 是原子的。
- 仓库可以把 `BeginTx` 通过函数字段注入（`PostgresSyncRepository.beginTx`，见 `store/sync_batches.go:26-36`），便于在测试里替换为内存实现，实际线上仍走 `*pgxpool.Pool`。

### 分页 / 时间窗口

- 时间序列表（`monitoring_instance_heartbeats`、`host_samples`、`probe_observations`、`state_change_events` 等）一律按 `(<entity>_id, observed_at desc)` 排序，并配套 `idx_<table>_<entity>_time` 索引（见 `db/migrations/0001_initial_schema.sql:145-150`）。
- 列表查询不要 `select *`：明确列清单，方便审 schema 变化。

---

## Migrations

### 机制

- 文件位置：`db/migrations/<NNNN>_<verb>_<scope>.sql`，纯 SQL，序号 4 位起步从 `0001` 递增。
- 嵌入：`db/migrations/embed.go` 仅有一行 `//go:embed *.sql`，把所有 `.sql` 打到二进制里。
- 应用：进程启动时 `cmd/houfeng-center/bootstrap.go:66-69` 调用 `migrate.Apply(ctx, db.Pool())`；后者实现见 `internal/center/store/migrate/migrate.go:48-104`：
  1. `EnsureLedger` —— 建 `schema_migrations(name primary key, applied_at)` 表
  2. 按文件名排序遍历，逐条 `HasMigration` 检查，未应用则 `ExecMigration` + `RecordMigration`
- **每个迁移必须幂等**：`create table if not exists`、`create index if not exists`、`alter table ... add column if not exists` 是基线写法（见 `0001_initial_schema.sql`、`0009_add_observability_filter_indexes.sql`）。

### 流程

1. 想清楚改动是否需要持久化（业务模型变化、查询需要新索引、retention 行为变化等）。
2. 在 `db/migrations/` 新建下一个未占用序号的文件，例如当前最大为 `0029_rename_nodes_to_monitoring_instances.sql` 时，下一个应为 `0030_<verb>_<scope>.sql`。
3. 文件内只允许 `create / alter / drop / insert` 等 DDL/DML 语句，不要在里面写 Go。
4. 同时更新对应 `internal/center/store/<aggregate>.go` 的 `select` 列、`insert` / `update` 语句、读写函数签名。
5. 跑 `make verify-go`（含 `migrate` 包的单测，见 `migrate_test.go`）；接着按 `docs/operations/v1-smoke-run.md` 在真 Postgres 上做 fresh-install smoke。

### 历史 / 审计字段同步

- 如果业务主表新增用户可见合同字段，且该字段会被历史、审计或决策表记录（如订阅价格历史、生命周期动作），同一个迁移必须同步补齐历史表列、backfill、约束与仓库 scan/insert 逻辑。不要只改源表导致后续审计丢失新字段。
- 兼容旧字段时，迁移需要给出可重复的推导规则和约束收口：例如订阅以 `billing_period_unit` + `billing_period_length` + `renewal_mode` 为新合同，同时从 `billing_months`、`billing_cycle`、`auto_renew`、`auto_renew_cancelled` 回填，并短期保留旧字段供下游兼容。
- 会同时更新业务事实和审计记录的动作必须在一个事务内完成。VPS 有效期延长这类操作应锁定目标 VPS，确认唯一 active subscription，写生命周期 action / step，更新 subscription `renew_at`，并在必要时写 price history。

### 不要做

- ❌ 修改已经合并/发布过的迁移文件内容（包括加空格）。要修就再写一个新迁移。
- ❌ 用任何运维脚本 / SQL 客户端直接改线上 schema，必须走迁移文件。
- ❌ 把测试数据 / seed 数据写进迁移文件——种子用户由 `internal/center/auth/seed.go` 在 bootstrap 阶段执行（`bootstrap.go:104-107`）。

> ⚠️ **已知 gap**：当前 `db/migrations/` 里存在两个 `0004_*` 文件 (`0004_add_node_onboarding_binding_state.sql`、`0004_add_observation_provenance.sql`)。前者是历史 Node 命名迁移，当前 schema 由 `0029_rename_nodes_to_monitoring_instances.sql` 迁到 MonitoringInstance 语义；`migrate.Apply` 按文件名字典序排序，二者顺序由后缀决定，并不冲突；但序号撞车违反了"序号唯一"的隐含约定，新增迁移时**必须先查看 `db/migrations/`，再使用当前最大编号之后的下一个未占用序号**（当前最大为 `0029_rename_nodes_to_monitoring_instances.sql`，下一个应为 `0030_*`，如果期间已有新迁移则继续顺延）。

---

## Naming Conventions

参考 `db/migrations/0001_initial_schema.sql`、`0010_add_users_and_sessions.sql` 的实际风格：

| 对象 | 规则 | 例子 |
|------|------|------|
| 表名 | `snake_case`，复数（如果是聚合事实表则单数+aggregates 后缀） | `monitoring_instances`、`targets`、`probe_items`、`probe_observations`、`monitoring_instance_host_sample_daily_aggregates` |
| 主键 | `<entity>_id text primary key`（业务主键，由 `internal/center/ids/ids.go` 生成）；纯事实表用 `id bigserial primary key` | `monitoring_instance_id text primary key`、`id bigserial primary key`（`host_samples`） |
| 外键 | `<other>_id text not null references <other_table>(<other_id>) on delete cascade` | `target_id text not null references targets(target_id) on delete cascade`（`probe_items`） |
| 时间戳 | 一律 `timestamptz`，业务列 `created_at` / `updated_at` 默认 `now()` | 见 `monitoring_instances` / `targets` |
| 布尔 | `<adj>` 或 `is_<adj>`；带默认值 | `is_backfilled boolean not null default false`、`maintenance_context boolean not null default false` |
| JSONB 列 | 默认值用 `'{}'::jsonb` 或具体默认对象 | `config jsonb not null default '{}'::jsonb`（`probe_items`）、`incident_defaults jsonb not null default '{...}'::jsonb`（`center_settings`） |
| 数组列 | `text[] not null default '{}'` | `labels text[] not null default '{}'` |
| 索引 | `idx_<table>_<purpose>`；GIN 索引带 `_gin` 后缀 | `idx_monitoring_instance_heartbeats_instance_time`、`idx_monitoring_instances_labels_gin` |
| 唯一约束 | 表内列 `unique`，或单独命名 | `username text not null unique`（`users`） |

> 例外：`db/migrations/0010_add_users_and_sessions.sql` 中的 `sessions_user_idx` / `sessions_expires_idx` 没有 `idx_` 前缀，是仓库现存差异；新增索引请遵循 `idx_<table>_<purpose>` 主流写法。

### MonitoringInstance enrollment token one-time consumption

#### 1. Scope / Trigger

- Trigger: 修改 MonitoringInstance enrollment token issuance/validation、`monitoring_instances.enrollment_token_*` 字段、`/api/agent/enroll`、或 MonitoringInstance onboarding install-command 生成路径。
- 目标：enrollment token 是一次性 bootstrap secret，不是长期 agent credential；成功绑定或进入待确认 fingerprint 路径后不得继续复用同一 token。

#### 2. Signatures

- DB columns: `monitoring_instances.enrollment_token_hash`, `monitoring_instances.enrollment_token_issued_at`, `monitoring_instances.enrollment_token_consumed_at`。
- Domain constant: `monitoringinstances.EnrollmentTokenTTL = 30 * time.Minute`。
- Issue method: `IssueMonitoringInstanceEnrollmentToken(ctx, monitoringInstanceID) -> monitoringinstances.EnrollmentTokenIssue{Token, IssuedAt, ExpiresAt}`。
- Validation path: `/api/agent/enroll` consumes a matching active token before or during binding evaluation.

#### 3. Contracts

- Token lookup must require non-empty hash, `enrollment_token_consumed_at is null`, and `enrollment_token_issued_at >= now() - monitoringinstances.EnrollmentTokenTTL`.
- Issuing a new token overwrites the previous hash/issued time and clears consumed state, so only the latest generated command is active.
- Successful token validation must mark `enrollment_token_consumed_at` in the same transaction as the enrollment/binding state change.
- A pending fingerprint conflict can consume the one-time token even before the operator confirms/rejects the conflict; UI copy must tell operators to regenerate when needed.
- Store only token hashes in Postgres; plaintext enrollment tokens appear only in the generated command and target host token file.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Token older than 30 minutes | enrollment returns invalid enrollment token semantics |
| Token already consumed | enrollment returns invalid enrollment token semantics |
| Token regenerated | old token becomes invalid; new token can be used until consumed or expired |
| MonitoringInstance missing during issue | `monitoringinstances.ErrMonitoringInstanceNotFound` |
| DB failure during consume/bind | transaction rolls back; caller returns wrapped repository error |

#### 5. Good/Base/Bad Cases

- Good: user generates command, runs it once within 30 minutes, agent binds and receives sync token; the bootstrap token is consumed.
- Base: user waits beyond 30 minutes; command fails at enroll and user regenerates from onboarding page.
- Bad: leaving multiple generated tokens valid lets shell history or chat leaks enroll the same MonitoringInstance later.
- Bad: consuming token after fingerprint confirmation instead of at enroll attempt makes conflict retries reuse a leaked bootstrap secret.

#### 6. Tests Required

- Migration test: `enrollment_token_consumed_at` column exists and active-token index excludes consumed tokens.
- Store tests: issue sets `expires_at = issued_at + 30m`, regeneration invalidates prior token, expired token fails, consumed token fails, successful enroll consumes token.
- Enrollment service/handler tests: invalid/expired/consumed token maps to the existing invalid enrollment response and does not leak token details.
- Frontend onboarding tests: conflict-resolution copy does not claim the one-time token remains unchanged.

#### 7. Wrong vs Correct

```sql
-- 错误：只按 hash 找 token，过期或已用 token 仍可登录。
where enrollment_token_hash = $1
```

```sql
-- 正确：只接受最新、未消费、TTL 内的 bootstrap token。
where enrollment_token_hash = $1
  and enrollment_token_consumed_at is null
  and enrollment_token_issued_at >= now() - interval '30 minutes'
```

### MonitoringInstance command action durability

#### 1. Scope / Trigger

- Trigger: 修改 MonitoringInstance action / remote command 链路时必须加载本节，包括 `POST /api/monitoring-instances/{monitoring_instance_id}/actions`、`agentapi.PendingAction`、`agentapi.CommandResult`、`syncing.CommandResult`、`monitoringinstances.last_action` 或 `store/sync_batches.go`。
- 目标：单 pending action 模型下保持 command identity 可追踪，避免 agent 结果晚到时覆盖另一个已排队或派发中的 action。

#### 2. Signatures

- HTTP request: `POST /api/monitoring-instances/{monitoring_instance_id}/actions` with body `{"command_id":"uptime"}`。
- HTTP response: `{"action_id":"act_xxx","command_id":"uptime","status":"pending"}`。
- Agent plan: `agentapi.PendingAction{ActionID, CommandID}` serializes as `action_id` + `command_id`。
- Agent result: `agentapi.CommandResult{ActionID, CommandID, Stdout, Stderr, ExitCode}` serializes as `action_id` + `command_id` + output fields。
- DB state: `monitoring_instances.pending_action_id`, `monitoring_instances.pending_action_command_id`, and `monitoring_instances.last_action jsonb`。

#### 3. Contracts

- Queueing an action writes both pending columns and `last_action={"status":"pending","action_id":...,"command_id":...}` so API/UI readers see pending immediately.
- Sync dispatch clears `pending_action_*` columns to prevent duplicate dispatch, but rewrites the same pending `last_action` to keep the in-flight identity durable until a matching result arrives.
- Command result storage must include the real `command_id` and update `last_action` to `status="done"` only when current `last_action` is still `pending` with the same `action_id` and `command_id`.
- `last_action.status` currently uses only `pending` and `done`; command success/failure is represented by `exit_code`, not by `success` / `failed` status strings.
- Go `monitoringinstances.LastAction.ExitCode` must stay nullable (`*int`) with `omitempty`: pending actions omit it, while completed success still serializes `exit_code: 0`.
- `last_action` is the current visible action state, not a full audit log. Do not infer historical command execution from it after another action is queued.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Missing `command_id` in MonitoringInstance action request | 400 `command_id required` |
| Unknown monitoring instance | 404 `monitoring instance not found` |
| MonitoringInstance is not bound | 409 `monitoring instance agent not bound` |
| monitoring instance monitoring is paused | 409 `monitoring instance monitoring is paused` |
| Agent result lacks `action_id` or `command_id` | Ignore the result row; do not overwrite `last_action` |
| Agent result identity does not match current pending `last_action` | Ignore the result row; do not overwrite `last_action` |
| DB write failure while queueing/dispatching/storing | Return wrapped repository error; handler maps to 500 where applicable |

#### 5. Good/Base/Bad Cases

- Good: user queues `uptime`, API immediately returns `command_id`, `last_action` shows pending `uptime`, agent returns matching `action_id` + `command_id`, and `last_action` becomes done with stdout/stderr/exit code.
- Base: no pending action and no command results in a sync batch leaves `last_action` unchanged.
- Bad: writing `last_action.command_id=""` from command results makes the UI lose the command label.
- Bad: storing command results with `WHERE monitoring_instance_id = $2` only can let a stale result overwrite a newer pending action.

#### 6. Tests Required

- Agent runtime test: pending action execution returns `CommandResult.ActionID` and `CommandResult.CommandID`.
- Agent handler test: sync request conversion preserves `command_results[].command_id`.
- Store tests: queueing writes pending `last_action`; dispatch clears pending columns while preserving pending JSON; result update SQL guards on pending status, action ID, and command ID; `UPDATE 0` mismatch is non-fatal; result storage runs before dispatching a newly queued action in the same sync transaction.
- Frontend API/page tests: `postMonitoringInstanceAction` preserves `command_id`; MonitoringInstance detail command drawer shows pending command label immediately after dispatch.

#### 7. Wrong vs Correct

```go
// 错误：丢失 command identity，且 stale result 可覆盖当前 action。
payload := map[string]any{"action_id": result.ActionID, "command_id": "", "status": "done"}
_, err := tx.Exec(ctx, `UPDATE monitoring_instances SET last_action = $1 WHERE monitoring_instance_id = $2`, payload, monitoringInstanceID)
```

```go
// 正确：结果只落到仍匹配的 pending action。
_, err := tx.Exec(ctx, `
	UPDATE monitoring_instances
	SET last_action = $1, updated_at = now()
	WHERE monitoring_instance_id = $2
		AND last_action->>'status' = $3
		AND last_action->>'action_id' = $4
		AND last_action->>'command_id' = $5`,
	raw, monitoringInstanceID, "pending", result.ActionID, result.CommandID)
```

### Asset Ledger providers

`db/migrations/0016_create_asset_ledger.sql` 是 post-V1 Asset Ledger 的 schema 入口，当前落 `providers` 服务商主数据表：

- `providers.provider_id` 使用 `ids.New("pv")` 生成，字段和 JSON contract 保持英文稳定值。
- `name` 必须通过数据库 `providers_name_not_blank` 约束保证 trim 后非空；领域层也必须在 create / patch 时校验。
- `rating` 是 nullable `integer`，只允许 `null` 或 `1..5`，数据库约束为 `providers_rating_range`。
- `labels` 使用 `text[] not null default '{}'`；领域层负责 trim 和过滤空标签。
- provider CRUD 不得自动改写、规范化或 backfill `monitoring_instances.provider`。`monitoring_instances.provider` 仍是 Fleet Observability 的监控实例元数据字符串，Asset Ledger provider 是独立资产层主数据。

### Asset Ledger VPS assets

`db/migrations/0017_add_vps_assets.sql` 添加 `vps_assets`，代表资产层 VPS 账本。它依赖 `providers.provider_id`，但仍与 Fleet Observability 的 `monitoring_instances.provider` 字符串保持分离。

- `vps_assets.vps_id` 使用 `ids.New("vps")` 生成。
- `provider_id` 可为 `null`；存在时必须引用 `providers(provider_id)`，并在 provider 删除时 `on delete set null`。
- `provider_name` 是导入 / 展示兼容字符串，不能创建、更新或回填 `providers`。
- `display_name` 必须由数据库 `vps_assets_display_name_not_blank` 约束保证 trim 后非空；领域层 create / patch 也必须校验。
- `lifecycle_status`、`usage_status`、`renewal_decision` 使用稳定英文机器值，并分别由数据库 check 约束和领域校验共同保护。
- VPS 是业务状态主体：人工生命周期、用途、续费 / 迁移 / 取消决策只写在 `vps_assets`。Subscription 和 MonitoringInstance 只能提供账单事实与运行观测事实，不得在普通创建 / 编辑流程里要求用户重复选择业务状态。
- `ssh_port` 默认为 `22`，数据库约束为 `1..65535`；领域 create 中 `0` 表示省略并默认，patch 中显式 `0` 必须拒绝。
- `archived_at` 是派生字段：生命周期切到 `archived` 时补时间，从 `archived` 切出时清空；API 输入不得任意写入 `archived_at`。
- VPS 资产 CRUD 不得改写 `monitoring_instances.provider`，也不得改变 MonitoringInstance / Target / Agent 的既有语义。
- 普通 VPS CRUD 只维护 VPS 自身账本；跨订阅、MonitoringInstance、Target 的取消 / 退役协调必须通过 `assetlifecycle` 显式 preview + confirm + audit action 完成。
- subscription summary 属于 subscriptions 查询；active monitoring instance link count / monitoring instance summary 由 `assetlinks.Repository` 在 HTTP 展示层补充，不得让 `store/vps_assets.go` 直接耦合 MonitoringInstance 表或 link 表细节。

### Asset Ledger subscriptions

`db/migrations/0018_add_subscriptions.sql` 添加 `subscriptions`，代表资产层 VPS 订阅账本。它依赖 `vps_assets.vps_id`，但不得反向改写 VPS 资产、Provider、MonitoringInstance、Target 或 Agent 状态。

- `subscriptions.subscription_id` 使用 `ids.New("sub")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时 `on delete cascade`。
- `currency` 使用大写 3 字母代码；领域层负责 trim + uppercase，数据库约束兜底。
- `price` 对齐数据库 `numeric(12, 2)`，领域层必须拒绝负数、超过精度或超过 2 位小数的输入，避免入库四舍五入后与派生字段漂移。
- `monthly_price` 是后端派生字段，按 `price / billing_months` 计算并四舍五入到 4 位小数；create / patch JSON 不接受 `monthly_price`，patch 修改 `price` 或 `billing_months` 时必须重新计算。
- `started_at` 与 `renew_at` 是 nullable `date`：未知日期用 `null`，不要写假日期。
- `status` 使用稳定英文机器值：`active`、`paused`、`cancelled`、`expired`、`unknown`。新用户流程不得把它暴露为必填业务状态；VPS-scoped create 默认只收 price / currency / billing cycle / dates / auto-renew / payment / note 等账单事实，内部可保留 legacy status 作为兼容和历史解释字段。
- 订阅 CRUD 不得创建 `vps_monitoring_instance_links`、不得改写 `monitoring_instances.provider`、不得增加 Dashboard / import / currency exchange 行为。
- 订阅 CRUD 仍不得反向改写 VPS、MonitoringInstance 或 Target；订阅取消 / 过期后如资产状态不一致，前端必须暴露 lifecycle action 入口，而不是在订阅 PATCH 中隐式停机或退役。
- 受控例外：用户显式在 `PATCH /api/vps/{vps_id}` 将 VPS `renewal_decision` 改成取消类决策（当前为 `cancel` 或 `auto_renew_cancelled`）时，VPS patch 事务可以同步处理该 VPS 的明确订阅事实。只有恰好一条 `status='active'` 的订阅候选时，才能在同一事务里把该订阅 `auto_renew=false`、`auto_renew_cancelled=true`，并按既有 `price_histories` 机制记录自动续费字段变化；无 active 订阅或多 active 订阅时只返回 linkage status/message，不批量写订阅。
- 上述例外仍属于 Asset Ledger 内部 VPS↔Subscription 用户决策流：不得创建或修改 `vps_monitoring_instance_links`、Provider、MonitoringInstance、Target、ProbeItem、Agent 计划或运行时控制；subscription 自己的 CRUD 仍不得反向改写 VPS renewal decision。

### Asset lifecycle actions

`assetlifecycle` 是唯一允许跨 Subscription、VPS、MonitoringInstance、Target/实例做取消或退役联动的领域服务。它不是普通 CRUD 的旁路，而是一个显式的 lifecycle action 工作流：先预览影响范围，再由用户确认要执行的步骤，最后以审计记录落库。

- 后端 API：
  - `GET /api/vps/{vps_id}/cancellation-preview` 从 VPS 出发返回 VPS 当前生命周期、所有关联订阅候选（包括 active、expired、cancelled、paused、unknown/latest）、活跃 `vps_monitoring_instance_links`、通过 asset service / domain 关联的 Target、推荐步骤、风险提示和阻塞项。
  - `POST /api/vps/{vps_id}/cancellation` 接受用户显式选择的 `subscription_ids`、`vps_lifecycle_status`、`monitoring_instance_actions`、`target_actions`、`reason`、`effective_date`，在一个事务内写入状态变化与审计步骤。
  - `GET /api/asset-context/monitoring-instances` 与 `GET /api/asset-context/targets` 是批量上下文接口，供列表页显示关联 VPS 的取消 / 过期 / 不一致状态，避免前端逐行请求。
- 审计表：`asset_lifecycle_actions` 保存一次操作的发起对象、确认时间、原因、执行摘要和最终状态；`asset_lifecycle_action_steps` 保存每个 subscription / VPS / MonitoringInstance / Target 步骤的前后状态、状态码、错误和摘要。
- 普通 CRUD 不得静默调用 lifecycle action；只有工作台或等价的显式确认入口可以调用 `POST /api/vps/{vps_id}/cancellation`。
- 如果 VPS 没有 active subscription，但存在 expired/cancelled/paused/unknown subscription，preview 和旧续费联动提示必须说明“订阅账单记录已无续费动作，仍需处理 VPS、MonitoringInstance 与入口探测状态”，不得误导为“没有关联订阅，需要创建订阅”。
- 默认语义：已过期且不续费的 VPS 写 `renewal_decision=cancel`、`lifecycle_status=cancelled`；未来到期但已决定不续费的 VPS 写 `renewal_decision=cancel`、`lifecycle_status=to_cancel`；未来取消但仍观察的 MonitoringInstance 用 `lifecycle_status='不续费'` 且监控保持启用；实际退役 MonitoringInstance 用 `lifecycle_status='已退役'` 并可按确认步骤暂停监控；随 VPS 下线的 Target/实例确认后用 `run_status='已归档'`，临时停用才用 `暂停`。
- `vps_monitoring_instance_links` 默认保留为历史证据；取消 / 退役 action 不自动 unlink，除非未来新增单独的“解除错误关联”显式动作。
- 执行事务必须先锁定 VPS，再写 action 与各步骤；任何一步失败时业务状态与步骤写入整体回滚，避免部分取消造成新割裂。失败审计是例外：必须先显式回滚业务事务，再用独立事务写入 `status='failed'` 的 action 和 failed step，避免失败记录随业务回滚消失，也避免复用同一 `action_id` 时被未回滚事务锁住。
- preview 的 blocker 必须在 POST 执行路径重新校验；例如 `lifecycle_status='archived'` 的 VPS 不允许通过 cancellation POST 改回 cancelled/to_cancel，handler 应返回冲突而不是清空 `archived_at`。
- Dashboard asset summary 只返回聚合计数；成本只统计 active subscriptions，取消待处理 / 已取消 VPS、状态割裂 VPS、仍运行的关联 MonitoringInstance/Target 进入告警计数。

#### Scenario: VPS renewal decision links subscription auto-renew

##### 1. Scope / Trigger

- Trigger: 修改 `PATCH /api/vps/{vps_id}`、`internal/center/store/vps_assets.go` 的 history transaction path、`subscriptions` 自动续费字段，或前端续费决策保存 flow。

##### 2. Signatures

- Backend API: `PATCH /api/vps/{vps_id}` with body containing `renewal_decision` and optional `renewal_reason`.
- Response: VPS record fields plus optional `renewal_subscription_linkage` object when a cancellation-class decision path was evaluated.
- Linkage response fields: `status`, `candidate_count`, optional `subscription_id`, `updated`, `message`.
- Store method: `PatchVPSAssetWithSubscriptionRenewalLinkage(ctx, vpsID, input) (vpsassets.Record, vpsassets.RenewalSubscriptionLinkage, error)`.
- DB writes: `vps_assets`, `renewal_decisions`, optional one `subscriptions` row, optional one `price_histories` row, all in one transaction.

##### 3. Contracts

- Cancellation-class decisions are currently `cancel` and `auto_renew_cancelled` only; `migrate`, `observe`, `keep`, `replaced`, and `unreviewed` must not modify subscriptions.
- The transaction must `select ... for update` the VPS row before patching and must lock active subscription candidates before deciding whether to write.
- Exactly one `subscriptions.status = 'active'` row for the VPS is the only unambiguous write case.
- In the write case, final subscription state must be `auto_renew=false` and `auto_renew_cancelled=true`.
- The linkage write must reuse the existing subscription patch/history semantics: when automatic-renewal fields change, insert `price_histories` in the same transaction.
- The response `message` is user-facing Chinese copy; frontend may display it directly but must not infer extra writes from it.
- This path must not create/update `vps_monitoring_instance_links`, Provider, MonitoringInstance, Target, ProbeItem, Agent plans, runtime controls, Dashboard summary rows, or import state.

##### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| VPS not found | Return existing `vps asset not found` behavior; no subscription write |
| invalid VPS patch input | Return invalid VPS input; no subscription write |
| renewal decision unchanged | Do not insert renewal history and do not evaluate subscription linkage |
| cancellation-class decision with 0 active subscriptions | Save VPS decision/history, return `status=no_active_subscription`, no subscription write |
| cancellation-class decision with >1 active subscriptions | Save VPS decision/history, return `status=multiple_active_subscriptions`, no subscription write |
| exactly 1 active subscription already cancelled | Save VPS decision/history, return `status=subscription_already_cancelled`, do not add no-op price history |
| exactly 1 active subscription needing cancellation | Save VPS decision/history, update subscription, insert price history, return `status=subscription_updated` |

##### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情把 `renewal_decision` 从 `keep` 改为 `cancel`，该 VPS 只有一条 active 订阅；响应包含 `subscription_updated`，VPS timeline 有 renewal decision，subscription timeline 有 auto-renew price history。
- Base: 用户把 `renewal_decision` 改为 `migrate`；只更新 VPS 决策和 history，不返回联动写入结果。
- Bad: 因为某 VPS 有两条 active 订阅而批量把两条都取消自动续费。
- Bad: 从 subscription PATCH 反向把 VPS renewal decision 改成 `auto_renew_cancelled`。

##### 6. Tests Required

- Store tests: exactly-one active subscription update, no active subscription, multiple active subscriptions, already-cancelled subscription, non-cancellation decision no write, unchanged decision no history/no write.
- Handler tests: cancellation-class PATCH returns `renewal_subscription_linkage`; ordinary PATCH still returns the plain VPS record contract used by existing clients.
- Frontend tests: decision save displays linkage message/action for `no_active_subscription` and keeps normal decision-save notice for non-linkage decisions.

##### 7. Wrong vs Correct

```go
// 错误：取消类决策后单独再 patch subscription，两个事务可能漂移。
record, _ := repo.PatchVPSAsset(ctx, vpsID, input)
_, _ = subscriptionRepo.PatchSubscription(ctx, subID, subscriptions.PatchInput{AutoRenewCancelled: subscriptions.PatchBool(true)})
```

```go
// 正确：VPS 当前状态、renewal history、subscription 当前状态和 price history 同事务完成。
record, linkage, err := repo.PatchVPSAssetWithSubscriptionRenewalLinkage(ctx, vpsID, input)
```

```go
// 错误：跨观测边界自动改 MonitoringInstance 运行态。
_, _ = tx.Exec(ctx, `update monitoring_instances set lifecycle_status = '不续费' where monitoring_instance_id = $1`, monitoringInstanceID)
```

```go
// 正确：只返回 linkage status，让 UI 引导用户显式处理 MonitoringInstance/Target/Agent 相关动作。
return vpsassets.RenewalSubscriptionLinkage{Status: vpsassets.RenewalSubscriptionLinkageMultipleActiveSubscription}
```

### Asset Ledger VPS MonitoringInstance links

`db/migrations/0019_create_vps_node_links.sql` 添加历史 link 表，`0029_rename_nodes_to_monitoring_instances.sql` 将其迁移为 `vps_monitoring_instance_links`，用于连接资产层 VPS 与 Fleet Observability 的 `monitoring_instances`。它是关联历史表，不是 MonitoringInstance 状态机的一部分。

- `vps_monitoring_instance_links.link_id` 使用 `ids.New("vnl")` 生成，避免用 `(vps_id, monitoring_instance_id, linked_at)` 做 API identity。
- `vps_id` 必须引用 `vps_assets(vps_id)`，`monitoring_instance_id` 必须引用 `monitoring_instances(monitoring_instance_id)`；删除 VPS 或 MonitoringInstance 时可以级联清理 link 历史。
- active link 定义为 `unlinked_at is null`。`idx_vps_monitoring_instance_links_pair_active` 必须保证同一 `(vps_id, monitoring_instance_id)` 同时最多一条 active link。
- unlink 必须写 `unlinked_at`，不得物理删除；如果提供 note，只更新 link note，不改 MonitoringInstance 或 VPS 业务字段。
- link / unlink 不得改写 `monitoring_instances.provider`、MonitoringInstance `lifecycle_status`、`monitoring_status`、`current_health_status`、Target、Agent 或 subscription。
- VPS item/list API 可以补 `active_monitoring_instance_link_count`，VPS detail 可以返回 active MonitoringInstance 摘要；这些摘要通过 `internal/center/assetlinks.Repository` 查询，不要把 MonitoringInstance 查询 SQL 塞进 `store/vps_assets.go`。
- MonitoringInstance 侧 VPS 摘要使用独立 `/api/monitoring-instances/{monitoring_instance_id}/vps` 查询，不把资产字段混入基础 `monitoringinstances.Record`。

### VPS-scoped MonitoringInstance creation

`POST /api/vps/{vps_id}/monitoring-instances` 是普通 agent 接入的主合同。它从 VPS 创建 MonitoringInstance 并在同一个事务内写入 active `vps_monitoring_instance_links`，避免先创建孤立监控实例再回 VPS 关联。

- 请求只允许少量覆盖字段：`display_name`、`group`、`region`、`city`、`provider`、`labels`、`note`、`link_note`。缺省值必须从 VPS 的 display name、provider、region/city/datacenter/country、labels、note 派生。
- 创建出的 MonitoringInstance 默认是运行观测附属事实：`lifecycle_status='待接入'`，binding / health / heartbeat 等仍由 onboarding 和 agent sync 推进。
- 如果 VPS 不存在、MonitoringInstance insert 失败或 link 失败，整个事务必须回滚，不留下孤立 MonitoringInstance。
- 该路径不得修改 VPS lifecycle / usage / renewal decision，也不得修改 Subscription、Target、ProbeItem 或 Agent plan；它只创建观测对象和 VPS 关联证据。

### VPS-scoped Subscription creation

`POST /api/vps/{vps_id}/subscriptions` 是普通补录订阅的主合同。path `vps_id` 是唯一 VPS 来源，请求体不得接受或覆盖 `vps_id`，也不得接受用户输入的 `status`。

- 请求字段只表达账单事实：`price`、`currency`、`billing_cycle`、`billing_months`、`started_at`、`renew_at`、`auto_renew`、`auto_renew_cancelled`、`payment_method`、`note`。
- 后端可以为 legacy `subscriptions.status` 写入内部默认值，但新 UI / API contract 不把它作为人工业务状态。
- 创建订阅不得反向修改 VPS lifecycle / usage / renewal decision，不得创建 MonitoringInstance link，不得修改 Provider、Target、ProbeItem 或 Agent。

### Asset Ledger timeline histories

`db/migrations/0020_create_renewal_decisions.sql` 添加 `renewal_decisions`，用于记录资产层 VPS 续费决策变化历史。它补充 `vps_assets.renewal_decision` 当前状态，不替代当前状态字段。

- `renewal_decisions.decision_id` 使用 `ids.New("rdec")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `from_decision` 允许 `null`，用于未来导入或补录；正常 VPS PATCH 自动记录时应写入变更前的决策值。
- `to_decision` 必须是 `vpsassets.RenewalDecision` 合法英文机器值，数据库 check 约束与领域校验共同保护。
- `reason` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `decided_at` 默认 `now()`，领域入口可传入 UTC 时间；timeline 按 `decided_at desc, created_at desc, decision_id desc` 排序。
- `PATCH /api/vps/{vps_id}` 只有在显式设置 `renewal_decision` 且最终值发生变化时才插入历史；只改其他字段或设置为原值不得插入历史。
- VPS 当前状态更新与 history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 VPS 行，避免当前状态和历史漂移。
- `GET /api/vps/{vps_id}/timeline` 返回真实表驱动的 `renewal_decisions[]`、`price_histories[]`、`ip_histories[]`、`spec_snapshots[]`、`experience_logs[]`，不得返回占位假数据。
- 续费决策历史本身不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target 或 Agent。唯一可同时改写 subscription 的路径是上一节定义的 `PATCH /api/vps/{vps_id}` 取消类续费决策受控联动例外；该路径必须同时保留 renewal decision history 与 subscription price history。

`db/migrations/0021_create_asset_histories.sql` 添加 `price_histories`、`ip_histories`、`vps_spec_snapshots`，用于补齐资产层价格、IP、规格变化历史。三张表补充当前状态字段，不替代 `subscriptions` 或 `vps_assets` 当前状态。

- `price_histories.price_history_id` 使用 `ids.New("ph")` 生成；`ip_histories.ip_history_id` 使用 `ids.New("iph")`；`vps_spec_snapshots.snapshot_id` 使用 `ids.New("vss")`。
- `price_histories` 必须同时引用 `subscriptions(subscription_id)` 和 `vps_assets(vps_id)`；subscription PATCH 只有在价格、币种、计费周期、计费月数、月付折算、续费日、自动续费标记或状态最终发生变化时才插入历史。
- subscription 当前状态更新与 price history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 subscription 行，避免当前订阅和历史漂移。
- `ip_histories` 必须记录 IPv4 / IPv6 前后值，且只有至少一个 IP 字段变化时才插入；数据库约束 `ip_histories_changed` 兜底拒绝无变化历史。
- `vps_spec_snapshots` 记录变化后的规格快照：`product_name`、`ssh_host`、`ssh_port`、`ssh_user`、`os_name`、`virtualization`。VPS PATCH 只有这些字段最终发生变化时才插入 snapshot。
- VPS 当前状态更新与 IP / spec history insert 必须复用 VPS history 事务路径，并先 `select ... for update` 锁定 VPS 行。
- 所有 history 仍属于 Asset Ledger，不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent 或 provider。

`db/migrations/0022_create_experience_logs.sql` 添加 `experience_logs`，用于记录单台 VPS 的人工体验、稳定性、网络、账单、服务支持、迁移或取消原因。它补充资产历史，不替代 `vps_assets.note` 或续费决策历史。

- `experience_logs.experience_log_id` 使用 `ids.New("elog")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `category` 使用稳定英文机器值：`note`、`stability`、`network`、`support`、`billing`、`migration`、`cancellation`；数据库 check 约束与领域校验共同保护。
- `severity` 使用稳定英文机器值：`info`、`warning`、`critical`；数据库 check 约束与领域校验共同保护。
- `summary` 必须 trim 后非空；`details` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `occurred_at` 默认 `now()`，领域入口可传入 UTC 时间；experience log 列表按 `occurred_at desc, created_at desc, experience_log_id desc` 排序。
- `GET /api/vps/{vps_id}/experience-logs` 只返回该 VPS 的经验记录；VPS 不存在时返回 asset timeline not found 语义。
- `POST /api/vps/{vps_id}/experience-logs` 的 path `vps_id` 是唯一 VPS 来源，请求 body 不接受覆盖 `vps_id`；写入只创建 experience log，不改写 VPS 当前字段。
- experience log 不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent、Provider、VPS 当前状态或 subscription。

#### Scenario: VPS experience logs contract

##### 1. Scope / Trigger

- Trigger: 修改 `experience_logs` schema、`/api/vps/{vps_id}/experience-logs`、`GET /api/vps/{vps_id}/timeline` 的 `experience_logs[]` 字段，或前端 VPS 详情页经验记录表单。

##### 2. Signatures

- DB table: `experience_logs(experience_log_id text primary key, vps_id text references vps_assets(vps_id) on delete cascade, category text, severity text, summary text, details text, occurred_at timestamptz, created_at timestamptz)`.
- Backend API: `GET /api/vps/{vps_id}/experience-logs` -> `[]renewals.ExperienceLogRecord`。
- Backend API: `POST /api/vps/{vps_id}/experience-logs` with JSON body `{category, severity, summary, details?, occurred_at?}` -> `renewals.ExperienceLogRecord`。
- Timeline API: `GET /api/vps/{vps_id}/timeline` includes `experience_logs: []`.
- Frontend API: `createVPSExperienceLog(vpsId, input)` and `listVPSExperienceLogs(vpsId)`.

##### 3. Contracts

- `category` and `severity` are machine values; UI maps them to Chinese labels in `web/src/lib/types.ts`.
- `occurred_at` is an RFC3339 timestamp when supplied; browser `datetime-local` values must be converted to ISO before posting.
- `summary` is the short user-facing title; `details` carries optional longer context and is never `null` in responses.
- `experience_logs[]` belongs to the VPS timeline contract; adding it requires updating Go DTOs, `web/src/lib/types.ts`, API tests, VPS detail tests, and timeline rendering together.

##### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| blank path `vps_id` | 400 from domain validation or 404 from route parsing |
| missing VPS foreign key | 404 `vps asset not found` at HTTP layer |
| invalid `category` / `severity` | 400 `invalid input` |
| blank `summary` | 400 `invalid input` |
| zero / invalid `occurred_at` | 400 `invalid input` / `invalid json` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

##### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页记录“晚高峰丢包”，POST 成功后刷新 timeline，并在 `experience_logs[]` 里按发生时间展示。
- Base: VPS 没有经验记录时，`experience_logs` 返回空数组，前端展示空态。
- Bad: 把 `vps_id` 放进 body 并允许覆盖 path VPS；这会制造跨资产误写风险。
- Bad: 经验记录写入后顺手修改 `vps_assets.note`、续费决策或 MonitoringInstance 状态。

##### 6. Tests Required

- Migration test: 断言表、外键、枚举 check、summary not blank、排序索引。
- Domain test: normalize / validate 覆盖 trim、UTC、invalid category/severity、blank summary。
- Store test: create/list/timeline 聚合、排序 SQL、missing VPS / foreign-key mapping。
- Handler/router/bootstrap tests: GET/POST、invalid JSON、invalid input、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API client URL/body，VPS 详情页创建经验记录后刷新 timeline，空态显示。

##### 7. Wrong vs Correct

```go
// 错误：body 里的 vps_id 覆盖 path，可能写入另一台 VPS。
input.VPSID = input.VPSID

// 正确：path 是唯一 VPS 来源。
input.VPSID = vpsID
```

```tsx
// 错误：直接提交 datetime-local 字符串，Go time.Time 不能稳定解析。
occurred_at: form.occurredAt

// 正确：提交 ISO/RFC3339 时间。
occurred_at: form.occurredAt ? new Date(form.occurredAt).toISOString() : null
```

### Asset Ledger service assets

`db/migrations/0023_create_asset_services.sql` 添加 `asset_services`，用于记录一台 VPS 上人工维护的服务资产。它是 VPS-scoped 资产备注和可选 Target 关联，不是完整服务注册中心、服务发现、域名管理或 Agent 自动采集入口。

- `asset_services.service_id` 使用 `ids.New("svc")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，命名外键为 `asset_services_vps_fk`，并在 VPS 删除时级联清理服务记录。
- `target_id` 可为 `null`，存在时必须引用 `targets(target_id)`，命名外键为 `asset_services_target_fk`，并在 Target 删除时置空；创建或列出服务不得修改 Target / ProbeItem。
- `name` 必须 trim 后非空；数据库约束为 `asset_services_name_not_blank`，领域 create 也必须校验。
- `service_type` 使用稳定英文机器值：`web`、`api`、`database`、`worker`、`proxy`、`other`；空输入默认 `other`。
- `status` 使用稳定英文机器值：`active`、`paused`、`retired`、`unknown`；空输入默认 `active`。
- `port` 可为 `null`；存在时必须在 `1..65535`。
- `labels` 使用 `text[] not null default '{}'`；领域层负责 trim、过滤空标签和去重。
- service asset 写入不得改变 VPS 当前状态、subscription、experience log、MonitoringInstance、Target、ProbeItem、Agent 或 `monitoring_instances.provider`。

#### Scenario: VPS service assets contract

##### 1. Scope / Trigger

- Trigger: 修改 `asset_services` schema、`internal/center/assetservices/`、`/api/services`、`/api/vps/{vps_id}/services`，或前端 VPS 详情页服务资产区块。

##### 2. Signatures

- DB table: `asset_services(service_id text primary key, vps_id text not null, target_id text null, name text, service_type text, status text, url text, port integer null, labels text[], note text, created_at timestamptz, updated_at timestamptz)`。
- Backend API: `GET /api/services?vps_id=&target_id=&service_type=&status=` -> `[]assetservices.Record`。
- Backend API: `POST /api/services` with JSON body `{vps_id, target_id?, name, service_type?, status?, url?, port?, labels?, note?}` -> `assetservices.Record`。
- Backend API: `GET /api/vps/{vps_id}/services` -> `[]assetservices.Record`。
- Backend API: `POST /api/vps/{vps_id}/services` with JSON body `{target_id?, name, service_type?, status?, url?, port?, labels?, note?}` -> `assetservices.Record`。
- Frontend API: `listAssetServices(filter)`, `createAssetService(input)`, `listVPSServices(vpsId)`, `createVPSService(vpsId, input)`。

##### 3. Contracts

- `POST /api/vps/{vps_id}/services` 的 path `vps_id` 是唯一 VPS 来源；body 中的 `vps_id` 必须被忽略，不能覆盖 path。
- `GET /api/vps/{vps_id}/services` 必须先确认 VPS 存在；VPS 不存在时返回 not-found 语义，不把缺失 VPS 静默表现为空列表。
- `target_id` 是可选关联，只做引用校验和展示跳转；不得创建 Target、改写 Target、改 ProbeItem 或改变观测语义。
- 全局 `GET /api/services` 可以按 VPS、Target、类型和状态过滤；非法枚举必须在进入 store 查询前返回 400。
- service asset 不进入 `/api/dashboard` 的资产摘要，也不进入 `GET /api/vps/{vps_id}/timeline`；它在 VPS 详情页作为独立服务区块加载。

##### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| missing / blank `vps_id` on collection POST | 400 `invalid input` |
| body `vps_id` conflicts with path VPS | path VPS wins; write to path VPS only |
| missing VPS on path GET/POST or collection POST FK | 404 `vps asset not found` |
| missing Target FK | 404 `target not found` |
| blank `name` | 400 `invalid input` |
| invalid `service_type` / `status` | 400 `invalid input` |
| `port < 1` or `port > 65535` | 400 `invalid input` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

##### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页为 `vps_001` 创建 `Blog` 服务，带 `target_id=tg_001`，写入后只刷新服务列表。
- Base: VPS 存在但没有服务时，path GET 返回空数组，前端展示 `尚未记录服务`。
- Bad: VPS 不存在时 path GET 返回空数组，用户会误以为资产存在但没有服务。
- Bad: 创建 service asset 时自动创建 Target 或修改 Target 探针，越过了本 MVP 的人工关联边界。

##### 6. Tests Required

- Migration test: 断言表、命名外键、枚举 check、name not blank、port range 和索引。
- Domain test: normalize / validate 覆盖空名称、枚举、labels、optional Target、port 边界和默认值。
- Store test: create、list all、list by VPS、排序 SQL、missing VPS exists check、missing Target FK、check violation 映射。
- Handler/router/bootstrap tests: collection/path GET/POST、invalid JSON、invalid input、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API helper URL/body，尤其 path-scoped create 不带 body `vps_id`；VPS 详情页服务加载、空态、创建成功和本地校验失败。

##### 7. Wrong vs Correct

```go
// 错误：让 body 覆盖 path，可能写到另一台 VPS。
input = assetservices.NormalizeCreateInput(input)

// 正确：先用 path 写入，再 normalize/validate。
input.VPSID = vpsID
input = assetservices.NormalizeCreateInput(input)
```

```tsx
// 错误：path scoped create 仍把 body.vps_id 传给后端。
postJSONBody(`/api/vps/${vpsId}/services`, input)

// 正确：path scoped create 去掉 vps_id，只传服务字段。
postJSONBody(`/api/vps/${vpsId}/services`, { name, service_type, status, target_id, url, port, labels, note })
```

`db/migrations/0024_create_asset_domains.sql` 添加 `asset_domains`，用于记录一台 VPS 关联或承载的手工维护域名资产。它是 VPS-scoped 资产记录，不是 DNS provider、注册商同步、解析记录管理或服务发现入口。

- `asset_domains.domain_id` 使用 `ids.New("dom")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，命名外键为 `asset_domains_vps_fk`，并在 VPS 删除时级联清理域名记录。
- `service_id` 可为 `null`，存在时必须引用 `asset_services(service_id)`，命名外键为 `asset_domains_service_fk`，并在 Service 删除时置空。写入前仓库还必须确认该 `service_id` 属于同一个 `vps_id`，避免跨 VPS 误关联。
- `target_id` 可为 `null`，存在时必须引用 `targets(target_id)`，命名外键为 `asset_domains_target_fk`，并在 Target 删除时置空；创建或列出域名不得修改 Target / ProbeItem。
- `domain_name` 必须是归一化的小写 ASCII 域名，不含协议、路径、空白或尾随点；数据库用 `asset_domains_name_unique` 保证全局唯一，领域层负责 trim/lower/remove trailing dot 和 label 校验。
- `status` 使用稳定英文机器值：`active`、`paused`、`retired`、`unknown`；空输入默认 `active`。
- `expires_at` 是 nullable `date`，未知日期用 `null`，API 复用 subscription `Date` 的 `YYYY-MM-DD` JSON 语义。
- `auto_renew` 与 `https_enabled` 只记录人工事实，不触发续费决策、证书检查或 Target probe 修改。
- domain asset 写入不得改变 VPS 当前状态、subscription、experience log、service asset、MonitoringInstance、Target、ProbeItem、Agent 或 `monitoring_instances.provider`。

#### Scenario: VPS domain assets contract

##### 1. Scope / Trigger

- Trigger: 修改 `asset_domains` schema、`internal/center/assetdomains/`、`/api/domains`、`/api/vps/{vps_id}/domains`，或前端 VPS 详情页域名资产区块。

##### 2. Signatures

- DB table: `asset_domains(domain_id text primary key, vps_id text not null, service_id text null, target_id text null, domain_name text, purpose text, status text, registrar text, expires_at date null, auto_renew boolean, https_enabled boolean, labels text[], note text, created_at timestamptz, updated_at timestamptz)`。
- Backend API: `GET /api/domains?vps_id=&service_id=&target_id=&status=` -> `[]assetdomains.Record`。
- Backend API: `POST /api/domains` with JSON body `{vps_id, service_id?, target_id?, domain_name, purpose?, status?, registrar?, expires_at?, auto_renew?, https_enabled?, labels?, note?}` -> `assetdomains.Record`。
- Backend API: `GET /api/vps/{vps_id}/domains` -> `[]assetdomains.Record`。
- Backend API: `POST /api/vps/{vps_id}/domains` with JSON body `{service_id?, target_id?, domain_name, purpose?, status?, registrar?, expires_at?, auto_renew?, https_enabled?, labels?, note?}` -> `assetdomains.Record`。
- Frontend API: `listAssetDomains(filter)`, `createAssetDomain(input)`, `listVPSDomains(vpsId)`, `createVPSDomain(vpsId, input)`。

##### 3. Contracts

- `POST /api/vps/{vps_id}/domains` 的 path `vps_id` 是唯一 VPS 来源；body 中的 `vps_id` 必须被忽略，不能覆盖 path。
- `GET /api/vps/{vps_id}/domains` 必须先确认 VPS 存在；VPS 不存在时返回 not-found 语义，不把缺失 VPS 静默表现为空列表。
- `service_id` 和 `target_id` 都是可选关联，只做引用校验和展示跳转；不得创建或修改 Service、Target、ProbeItem 或观测语义。
- `service_id` 若存在，必须属于同一 VPS；跨 VPS service 关联应返回 `asset service not found` 语义。
- 全局 `GET /api/domains` 可以按 VPS、Service、Target 和状态过滤；非法枚举必须在进入 store 查询前返回 400。
- domain asset 不进入 `/api/dashboard` 的资产摘要，也不进入 `GET /api/vps/{vps_id}/timeline`；它在 VPS 详情页作为独立域名区块加载。

##### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| missing / blank `vps_id` on collection POST | 400 `invalid input` |
| body `vps_id` conflicts with path VPS | path VPS wins; write to path VPS only |
| missing VPS on path GET/POST or collection POST FK | 404 `vps asset not found` |
| missing / cross-VPS Service FK | 404 `asset service not found` |
| missing Target FK | 404 `target not found` |
| duplicate `domain_name` | 409 `asset domain conflict` |
| invalid `domain_name` | 400 `invalid input` |
| invalid `status` | 400 `invalid input` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

##### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页为 `vps_001` 创建 `api.example.com`，可选带 `service_id=svc_001` 与 `target_id=tg_001`，写入后只刷新域名列表。
- Base: VPS 存在但没有域名时，path GET 返回空数组，前端展示 `尚未记录域名`。
- Bad: VPS 不存在时 path GET 返回空数组，用户会误以为资产存在但没有域名。
- Bad: 创建 domain asset 时自动创建 DNS 记录、创建 Target、修改 Target 探针或尝试注册商同步，越过了本 MVP 的人工维护边界。

##### 6. Tests Required

- Migration test: 断言表、命名外键、唯一约束、域名归一化 check、枚举 check、date 字段和索引。
- Domain test: normalize / validate 覆盖空域名、URL/path、裸主机名、非法 label、枚举、labels、默认值。
- Store test: create、list all、list by VPS、排序 SQL、missing VPS exists check、service 同 VPS 校验、missing Service/Target FK、unique/check violation 映射。
- Handler/router/bootstrap tests: collection/path GET/POST、invalid JSON、invalid input、conflict、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API helper URL/body，尤其 path-scoped create 不带 body `vps_id`；VPS 详情页域名加载、空态、创建成功和本地校验失败。

##### 7. Wrong vs Correct

```go
// 错误：只靠 FK，允许 dom(vps_a) 关联 svc(vps_b)。
insert into asset_domains (vps_id, service_id, domain_name) values (...)

// 正确：写入前确认 service 属于同一个 VPS。
select exists (select 1 from asset_services where service_id = $1 and vps_id = $2)
```

```tsx
// 错误：VPS scoped create 仍把 body.vps_id 传给后端。
postJSONBody(`/api/vps/${vpsId}/domains`, input)

// 正确：path scoped create 去掉 vps_id，只传域名字段。
postJSONBody(`/api/vps/${vpsId}/domains`, { domain_name, service_id, target_id, status, expires_at, labels, note })
```

### Asset Ledger JSON import

`internal/center/importing/` 是真实 VPS JSON dry-run/import 的领域入口。它复用 `providers`、`vpsassets`、`subscriptions` 的 normalize / validate 规则，不维护第二套枚举、金额、日期或 provider 校验。

- dry-run 是默认路径，只解析、归一化、校验和产出报告，不写数据库。
- dry-run 必须报告 provider 创建候选、VPS 创建候选、subscription 创建候选、缺失 provider、缺失续费日期、非法字段、重复候选、MonitoringInstance 关联候选、未来 30 天续费候选和闲置但付费候选。
- 数据库可用时，dry-run 可以读取现有 providers / vps_assets / subscriptions / monitoring_instances 做重复和 MonitoringInstance 候选诊断；数据库不可用时仍应能完成纯文件模型校验。
- `-import` 必须显式开启，且在一个事务中按 provider → VPS asset → subscription 顺序写入；校验错误或重复候选存在时拒绝写入。
- import 不接受也不写 `monthly_price`，仍由 subscription 后端计算。
- import 不创建 `vps_monitoring_instance_links`，不改写 `monitoring_instances.provider`，不改变 MonitoringInstance / Target / Agent 语义。MonitoringInstance 相关输入只能作为人工确认候选进入报告。

### Asset Ledger Dashboard summary

`internal/center/store/dashboard.go` 可以读取资产层表来生成 `/api/dashboard` 的少量决策摘要，但它仍是 Dashboard read model，不是资产 CRUD 仓库。

- `incidents.DashboardOverview.AssetSummary` 的 JSON contract 是 `asset_summary`，只允许返回聚合计数和按币种成本分组，不返回 VPS、subscription、MonitoringInstance 或 provider 明细数组。
- `asset_summary` 的 30 天续费口径：`subscriptions.status = 'active'`，`renew_at >= current_date` 且 `renew_at <= current_date + 30`，并只统计未取消/未归档的 VPS。
- active VPS 口径：`vps_assets.lifecycle_status not in ('cancelled', 'archived')`。
- active link 口径：`vps_monitoring_instance_links.unlinked_at is null`。
- 异常关联 VPS 口径：active link 关联到 `monitoring_instances.current_health_status <> '正常'` 的 MonitoringInstance；只读 MonitoringInstance 派生状态，不改写 MonitoringInstance。
- 成本口径：`sum(active subscriptions monthly_price)` 按 `currency` 分组，`yearly_total = monthly_total * 12`；第一阶段不做汇率换算。
- 取消联动口径：`cancelled_vps_count` 统计 `lifecycle_status='cancelled'`；`cancellation_attention_vps_count` 统计订阅非活跃但 VPS 未取消、VPS 取消/待取消但订阅仍 active、VPS 取消/待取消但 MonitoringInstance/Target 仍运行、或取消类续费决策与 lifecycle 未对齐的 VPS；`running_cancelled_asset_count` 统计取消/待取消 VPS 下仍运行的 active MonitoringInstance link 与未归档/未暂停 Target。
- 该查询不得改变 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent、VPS、subscription 或 link 记录。
- `limit` 只限制异常队列和 recent events；不得限制 `asset_summary`。

### Scenario: Asset decision portfolio read model and memory layer

#### 1. Scope / Trigger

- Trigger: 修改 `internal/center/assetdecisions/`、`internal/center/store/asset_decisions.go`、`/api/asset-decisions/*`、`db/migrations/*asset_decision*`，或任何依赖 VPS / Subscription / Service / Domain / MonitoringInstance / Target 聚合生成组合决策组和决策记录的逻辑。
- 目标：`/asset-decisions` 是资产组合决策中枢。自动组仍是只读派生 read model；手工组合是用户定义的 scenario layer，只保存场景、成员意图和备注；决策记录是独立 memory layer，只保存用户判断和证据快照。三层都不成为第二套 VPS / Subscription / MonitoringInstance / Target 状态机。

#### 2. Signatures

- Domain package: `internal/center/assetdecisions`，包含 `Repository`、`ListFilters`、`Overview`、`GroupSummary`、`GroupDetail`、`GroupMember`、`ManualGroupSummary`、`ManualGroupDetail`、`ManualGroupMember`、`RecordSummary`、`RecordDetail`、`RecordMember`、`CreateRecordInput`、`PatchRecordInput`、`ErrAssetDecisionGroupNotFound`、`ErrAssetDecisionManualGroupNotFound`、`ErrAssetDecisionRecordNotFound`、`ErrInvalidAssetDecisionInput`。
- Backend APIs:
  - `GET /api/asset-decisions/overview?view=&renew_within_days=`
  - `GET /api/asset-decisions/groups?view=&renew_within_days=`
  - `GET /api/asset-decisions/groups/{group_id}?renew_within_days=`
  - `GET /api/asset-decisions/records`
  - `POST /api/asset-decisions/records`
  - `GET /api/asset-decisions/records/{record_id}`
  - `PATCH /api/asset-decisions/records/{record_id}`
  - `GET /api/asset-decisions/manual-groups`
  - `POST /api/asset-decisions/manual-groups`
  - `GET /api/asset-decisions/manual-groups/{manual_group_id}`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}`
  - `POST /api/asset-decisions/manual-groups/{manual_group_id}/members`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
  - `DELETE /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
- Store source tables: `vps_assets`、`providers`、`subscriptions`、`asset_services`、`asset_domains`、`vps_monitoring_instance_links`、`monitoring_instances`、`targets`。
- Manual scenario tables: `asset_decision_manual_groups`、`asset_decision_manual_group_members`。manual group id 使用 `admg_*`；成员引用现有 `vps_assets.vps_id`，只保存 `intended_role`、`intended_action`、`reason`、`note`、`sort_order` 和创建时 evidence snapshot。
- Decision memory tables: `asset_decision_records`、`asset_decision_record_members`；view: `asset_decision_records_with_counts`。`source_type` 允许 `auto_group` 与 `manual_group`；未传 source type 时默认 `auto_group` 以兼容旧调用。
- Stable group id: `adg_auto_<12hex>`，由 group type、scope key 和续费窗口等只读 key 确定性派生；detail endpoint 每次重新计算组列表后按 ID 查找。
- Decision record id: `adr_*`，由 `ids.New("adr")` 生成；记录只引用来源自动组 ID 作为历史来源，不把自动组 ID 当长期外键。
- Evidence assessment: `GroupSummary` 和 `GroupMember` 必须返回 `evidence_assessment`，字段固定为 `confidence_score`、`pressure_score`、`readiness_score`、`quality_tier`（`strong|usable|weak|blocked`）、`decision_bias`（`keep|observe|complete_evidence|retire|migrate|review`）、`support_signal_count`、`risk_signal_count`、`gap_signal_count`、`summary`。
- Record member follow-up: `asset_decision_record_members.followup_status` 固定为 `todo|in_progress|blocked|done|skipped`，`followup_note` 为 trim 后的执行备注，`followup_updated_at` 为最后一次成员跟进更新时间；`asset_decision_records_with_counts` 必须返回各状态聚合计数。
- Execution readback: `RecordSummary` / `RecordDetail` 和 `RecordMember` 必须返回只读派生字段 `execution_readback`。记录级字段为 `status`（`open|aligned|drift|blocked|needs_evidence|inactive`）、中文 `summary`、`open_count`、`aligned_count`、`drift_count`、`blocked_count`、`needs_evidence_count`。成员级字段为同一 status、summary、`issues[]`（`kind,label,tone,details?`）和 `current_facts`（当前 VPS lifecycle、usage、renewal decision、active subscription / service / domain / Target / monitoring 计数与 source availability）。

#### 3. Contracts

- 自动组只读派生，不写数据库；手工组合只写 `asset_decision_manual_groups` / `asset_decision_manual_group_members`；用户保存一次判断才写入 `asset_decision_records` / `asset_decision_record_members`。
- 手工组合支持 `source_type=manual` 和 `source_type=auto_group`。从自动组创建手工组合时，store 必须重新读取当前 facts 并定位自动组，复制当前成员建议角色/动作与 evidence snapshot；自动组不存在或请求成员不属于组时返回 invalid/not found，不得信任前端传入的成员事实。
- 手工组合详情和列表必须复用当前 `loadFacts` 聚合实时回读成员事实；成员当前 VPS facts 缺失时仍返回 manual metadata，并展示 `current_fact_missing` evidence chip，不得静默丢成员。
- 手工组合成员增删改只能修改 manual member 行，不得修改 VPS、Subscription、MonitoringInstance、Target、Service、Domain 或决策记录跟进状态。手工组合没有 hard delete endpoint；归档使用 `status=archived`。
- 创建决策记录时必须重新读取当前事实。`source_type=auto_group` 通过 `FindGroup` 定位 `source_group_id`；`source_type=manual_group` 通过 manual group detail 生成 group/member snapshot 并使用成员 intended role/action/reason 作为决定默认值。来源不存在返回 404，不得按前端传入的成员列表凭空创建记录。
- 决策记录必须保存组级来源字段、标题、目标、状态、组级 `evidence_snapshot`，并为组内每台 VPS 保存系统建议角色/动作、用户决定角色/动作、成员理由和成员级 `evidence_snapshot`。
- 决策记录状态固定为 `draft`、`decided`、`in_progress`、`completed`、`abandoned`；PATCH 可更新记录级标题、目标、状态，以及记录内已有成员的 `followup_status` / `followup_note`，但不执行 VPS / Subscription / MonitoringInstance / Target 业务动作。
- 成员跟进 PATCH 的 payload 为 `members:[{vps_id, followup_status?, followup_note?}]`；`vps_id` 必须属于当前记录，同一 payload 不得重复，状态必须合法，状态或备注至少设置一项。成功更新成员跟进时必须刷新成员 `followup_updated_at`、成员 `updated_at` 与记录 `updated_at`，并返回 detail 风格的最新记录。
- 成员全部 `done` / `skipped` 不得自动推进整条决策记录状态；组合决策记录状态仍由用户显式修改，避免在 memory layer 内扩张隐式状态机。
- 执行回读只校验“保存的组合判断是否与当前事实一致”，不得变成第二套状态机：records API 不自动 PATCH record status，不自动完成成员跟进，不自动修改 VPS / Subscription / MonitoringInstance / Target。
- 成员回读以 `decided_action` 为主，历史值为空才回退 `suggested_action`。`cancel` / `open_cancellation_workbench` 只判断 VPS 是否进入 `to_cancel|cancelled|archived` 且无 active subscription、无 running monitoring、无 running target；`migrate` 只判断是否进入迁移链路（`renewal_decision=migrate|replaced` 或 `lifecycle_status=to_migrate`），不判断新 VPS 是否已替代旧 VPS；`keep` / `observe` 只检查 lifecycle 未取消/归档和 renewal decision 是否相符；`complete_evidence` 只检查当前已有证据缺口。
- 回读状态优先级：`record.status=abandoned` 为 `inactive`；成员 `followup_status=blocked` 优先 `blocked`，但 `done` 后关键事实不一致仍为 `drift`；`skipped` 抑制普通 open，但不隐藏关键 drift；存在证据缺口为 `needs_evidence`；事实与动作一致为 `aligned`。记录级聚合优先级为 drift > blocked > needs_evidence > aligned > open。
- 成员级 `decided_action=cancel` 或 `open_cancellation_workbench` 只能给前端提供跳转到 VPS lifecycle workbench 的入口；后端 records API 不做批量取消、批量退役或批量迁移。
- Group type 固定语义：`renewal_attention`、`cancellation_attention`、`region_portfolio`、`provider_portfolio`、`cost_pressure`、`evidence_gap`。
- `renew_within_days` 默认 30，仅允许产品认可的窗口（当前 `30/60/90`）；非法值在 handler 返回 400。
- `view` 只筛选返回的自动组，不改变底层事实读取；非法值返回 400。
- Store 读取现有表后在 Go 中派生组合摘要和成员建议，避免 Dashboard / VPS / Subscription / Provider 页面各自重复 join 后语义漂移。
- 组级摘要可以聚合成本、续费窗口、取消联动、服务 / 域名 / Target、监控关联、异常和 evidence chips；成员级建议角色 / 建议动作只能作为扫描提示，不执行写操作。
- `evidence_assessment` 是只读、可解释评分层，只消费当前 `GroupMember` / `GroupSummary` 已有事实、source availability 和 evidence chips；它不得新增数据库读取、逐台 runtime facts 调用或执行语义，也不得把评分当成自动 keep / migrate / cancel 写入。
- 证据源不可用时只能降低 `confidence_score` / `readiness_score` 并增加 gap 计数；不得把 `subscription_unavailable`、Monitoring/Target/Service/Domain 查询失败解释为真实 `missing_subscription` / `missing_monitoring` 业务事实。
- 决策记录回读必须 fail closed：当前事实查询失败时 records list/detail/create/patch 返回 repository error，不得把未知事实伪造成 `aligned`、`needs_evidence` 或 `drift`。成员存在但当前 facts 中找不到对应 VPS 时，成员 readback 为 `drift` 且 issue kind 为 `current_fact_missing`。
- `RecordSnapshotFromGroup` 与 `RecordSnapshotFromMember` 必须把当时的 `evidence_assessment` 写入 `evidence_snapshot`，用于记录详情回看保存时的判断基础；旧记录缺少该字段时前端必须可降级显示。
- archived VPS 不进入普通 region/provider/cost/evidence 组合；cancelled/to_cancel 只能作为取消联动相关证据出现，避免归档资产污染正常组合比较。
- 订阅、服务、域名、监控或 Target 查询失败必须返回 repository error；不得构造“健康”或“缺证据”假结果。只有查询成功且事实为空时，才生成 `missing_subscription`、`unlinked_monitoring` 等真实 evidence gap。
- `/api/asset-decisions/*` 不逐台调用 runtime facts detail endpoint，只读 MonitoringInstance / Target 当前摘要字段和关联计数；CPU / IO / 路由 / IP 质量 / 超售判断属于后续能力。
- 执行回读同样只能复用 `loadFacts` 聚合事实，不得逐台请求 runtime facts detail、HostSample、ProbeObservation、agent 性能趋势、IP 质量或路由质量。IP / 路由 / 性能衰退 / CPU / IO / 超售判断等待 agent 与观测语义成熟后再进入模型。
- 组合页仍只通过既有 `PATCH /api/vps/{id}` 改单台 VPS renewal decision；取消 / 退役执行必须回到 VPS lifecycle workbench。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| invalid `view` | handler 返回 400 `invalid input` |
| invalid `renew_within_days` | handler 返回 400 `invalid input` |
| missing `group_id` | handler 返回 404 `asset decision group not found` |
| missing `record_id` | handler 返回 404 `asset decision record not found` |
| missing `manual_group_id` | handler 返回 404 `asset decision manual group not found` |
| missing manual group member | handler 返回 404 `asset decision manual group member not found` |
| create record with missing auto group | handler 返回 404 `asset decision group not found` |
| create record with missing manual group | handler 返回 404 `asset decision manual group not found` |
| create record with member not in group | handler 返回 400 `invalid input`，不得开启写事务 |
| create manual group with duplicate members | handler 返回 400 `invalid input`，不得写入半成品 |
| manual group member current facts missing | detail/list 保留成员并显示 `current_fact_missing`，但从该手工组创建 record 应 fail closed 返回 invalid input |
| patch record with invalid status/title | handler 返回 400 `invalid input` |
| patch record member with unknown `vps_id` | handler 返回 400 `invalid input`，事务回滚 |
| patch record member with duplicate `vps_id` in payload | handler 返回 400 `invalid input`，不得开启写事务 |
| patch record member with invalid follow-up status | handler 返回 400 `invalid input`，不得写入成员 |
| repository query failure | handler 返回 500 `internal server error`，store error 用 `%w` 包装 |
| source table query failed | 不返回 evidence gap；整体 request fail，避免误报缺订阅 / 缺监控 |
| source query succeeded but rows empty | 返回可解释的 `evidence_gap` 或空组，不伪造健康证据 |
| source availability false but fact rows unknown | `evidence_assessment` 降低可信度并提示证据不可用；不得生成真实缺订阅 / 缺监控 chip |
| old decision record snapshot without `evidence_assessment` | API 正常返回历史 snapshot；前端显示缺失评估，不要求后端 backfill |
| record `status=abandoned` | `execution_readback.status=inactive`，不参与执行推进提示 |
| member followup `done` but current facts still not closed | 成员 `execution_readback.status=drift`，记录级聚合为 `drift` |
| member followup `blocked` | 成员优先显示 `blocked`，记录级无 drift 时聚合为 `blocked` |
| record member VPS missing from current facts | 成员 `drift`，issue kind 为 `current_fact_missing` |
| unsupported method | handler 返回 405 `method not allowed` |
| `/api/asset-decisions/*` route missing | router test 必须失败；该路径不得落 SPA fallback |

#### 5. Good/Base/Bad Cases

- Good: 同一国家 / region / city 下两台 active VPS 自动形成 `region_portfolio`，成员显示成本、用途、服务 / 域名 / 监控差异，帮助用户取舍。
- Good: Provider 下多台 active VPS 自动形成 `provider_portfolio`，用于服务商组合比较。
- Good: subscription query 成功且某台 VPS 没有 active subscription，`evidence_gap` 或成员 evidence chips 标记缺订阅。
- Good: 用户打开自动组，保存为 `adr_*` 决策记录；记录保留当时成本、服务/域名/Target、监控和成员建议快照，后续只推进记录状态。
- Good: 用户把自动组保存为 `admg_*` 手工组合，随后调整成员 intended role/action/reason，再从手工组合保存为 `source_type=manual_group` 的 `adr_*` 记录。
- Good: 用户给手工组合新增一台现有 VPS，只保存手工组合成员行；VPS 的 lifecycle、renewal decision、订阅、监控和 Target 均不被修改。
- Good: 用户把记录中某台 VPS 标记为 `blocked` 并记录“等待迁移窗口”，API 只更新该 record member 的跟进字段与记录 `updated_at`，不修改 VPS lifecycle 或 subscription。
- Good: 已保存记录中 `cancel` 成员跟进标记 `done` 后，如果当前仍有 active subscription 或 running target，readback 显示 `drift`，提示“跟进已完成但事实未闭环”。
- Good: 已保存记录的成员 facts 找不到对应 VPS 时，readback 显示 `current_fact_missing` 而不是伪造已对齐。
- Good: 完整证据的同区组合返回较高可信度/准备度与 `quality_tier=strong`，资料缺口或来源不可用返回较低可信度与 `decision_bias=complete_evidence`。
- Base: 没有任何 VPS 时 overview 仍返回 0 计数和空 `top_groups`。
- Bad: 在 store 里写入 `asset_decision_groups` 表，或把自动组 ID 当长期外键依赖。
- Bad: 手工组合成员保存当前成本、订阅、监控、服务等实时事实并长期展示，不再从 `loadFacts` 回读当前状态。
- Bad: `PATCH /api/asset-decisions/records/{id}` 同时修改 VPS renewal decision、Subscription 状态或执行取消/退役。
- Bad: records list 为了给每条记录计算 readback 逐条调用 `GetRecord`，造成 N+1。
- Bad: readback 使用 HostSample、ProbeObservation、IP 质量、路由质量或性能衰退数据，在 agent 语义未成熟前给出超售判断。
- Bad: group detail 为了展示性能趋势逐台请求 runtime facts detail endpoint，造成 N+1 和语义越界。
- Bad: subscriptions 查询失败后把所有 VPS 标记为 `missing_subscription`，误导用户取消资产。

#### 6. Tests Required

- Domain tests: stable group id、view/window validation、record input validation、snapshot builder、renewal/cancellation/region/provider/cost/evidence group derivation、archived/cancelled 边界、source unavailable 不误报。
- Domain assessment tests: 完整证据、资料缺口、证据源不可用、取消联动 / 预算压力、record snapshot 均断言 `evidence_assessment` 的 tier / bias / score 方向。
- Store tests: member facts 聚合、主订阅选择、服务 / 域名 / Target / 监控计数、成本和 evidence chips，manual groups list/create/get/patch/member add/patch/delete、records list/create/get/patch、成员跟进计数、成员跟进事务更新与未知成员回滚，且不依赖 runtime facts detail。
- Execution readback domain tests: cancel / cancellation workbench aligned/open/drift、migrate 链路与旧承载 drift、keep / observe 一致性、complete_evidence 只检查当前已有缺口、done drift、blocked 优先、skipped 抑制普通 open、abandoned inactive、current fact missing。
- Store tests: records list/detail/create/patch 均返回 `execution_readback`；ListRecords 批量读取成员并聚合，不逐条调用 `GetRecord`；facts 查询失败 fail closed；成员跟进 PATCH 后 readback 随响应刷新；不依赖 runtime facts detail / HostSample / ProbeObservation。
- Handler tests: overview、groups list、group detail、manual groups list/create/detail/patch/member add/patch/delete、records list/create/detail/patch success 且 records 响应包含 readback、成员跟进 patch；invalid query/input、missing group/manual group/member/record、未知或重复成员、repo failure、method not allowed。
- Router/bootstrap tests: `/api/asset-decisions/overview`、`/api/asset-decisions/groups`、`/api/asset-decisions/groups/{id}`、`/api/asset-decisions/manual-groups/*`、`/api/asset-decisions/records`、`/api/asset-decisions/records/{id}` 登录保护且不落 SPA fallback；`bootstrapCenter` wiring 非 nil。
- Frontend tests: API helper query/payload、页面主 surface、saved records surface、tabs、group detail、保存记录、记录详情状态推进、单台 PATCH payload、evidence 请求失败边界。

#### 7. Wrong vs Correct

```go
// 错误：证据源失败时构造“缺订阅”假事实。
if err != nil {
    member.EvidenceChips = append(member.EvidenceChips, EvidenceChip{Kind: "missing_subscription"})
    return groups, nil
}
```

```go
// 正确：查询失败是 source unavailable，交给 handler 返回错误或前端显示局部失败。
if err != nil {
    return nil, fmt.Errorf("list asset decision facts: %w", err)
}
```

```go
// 错误：详情依赖已经保存的自动组状态。
return repo.loadPersistedGroup(ctx, groupID)

// 正确：每次重新派生组，再按稳定 ID 查找。
groups := DeriveGroups(facts, filters)
return FindGroup(groups, groupID)
```

```go
// 错误：成员跟进完成后隐式修改 VPS 或整条记录状态。
if member.FollowupStatus == assetdecisions.FollowupDone {
    _, _ = tx.Exec(ctx, `update vps_assets set lifecycle_status = 'cancelled' where vps_id = $1`, member.VPSID)
}
```

```go
// 正确：跟进只是决策记录成员的执行记忆，真实动作回到对应业务页面。
_, err := tx.Exec(ctx, `
    update asset_decision_record_members
    set followup_status = $1, followup_note = $2, followup_updated_at = now(), updated_at = now()
    where record_id = $3 and vps_id = $4`,
    status, note, recordID, vpsID)
```

### Scenario: Events backfilled filter contract

#### 1. Scope / Trigger

- Trigger: 修改 `/api/events` 查询参数、`store.EventsFilter`、`PostgresDashboardRepository.ListEvents`，或改动 `state_change_events` 与 runtime facts 的 backfill 展示语义。

#### 2. Signatures

- Backend API: `GET /api/events?include_backfilled=<bool>` -> `{"items":[]}`。
- Handler field: `store.EventsFilter.IncludeBackfilled bool`。
- DB source rows: `state_change_events e`；backfill provenance lives in `monitoring_instance_heartbeats.is_backfilled`、`host_samples.is_backfilled`、`probe_observations.is_backfilled`。

#### 3. Contracts

- `/api/events` 成功响应返回 envelope：`{"items":[...]}`；错误响应保持通用 `{"error":"..."}`。
- `include_backfilled` 使用 Go `strconv.ParseBool` 解析；前端 URL 用 `include_backfilled=1`，API 请求用 `include_backfilled=true`。
- 默认 `IncludeBackfilled=false` 时，`ListEvents` 必须排除可关联到 backfilled runtime facts 的事件。
- `IncludeBackfilled=true` 时不得添加 backfill exclusion predicate。
- 由于 `state_change_events` 没有 `is_backfilled` 列，backfill 关联只在 read model 查询中通过对象和时间建立：
  - monitoring instance event: `e.object_type = 'monitoring_instance'` 且存在同 `monitoring_instance_id`、同 `observed_at = e.created_at` 的 backfilled `monitoring_instance_heartbeats` 或 `host_samples`。
  - target event: `e.object_type = 'target'` 且存在同 `target_id`、同 `observed_at = e.created_at` 的 backfilled `probe_observations`。
- 不要为了这个筛选改写 raw facts、incident mutation、notification 记录或 retention 语义。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| `include_backfilled` 缺失 | handler 传 `IncludeBackfilled=false`，store 默认排除 backfilled 关联事件 |
| `include_backfilled=true` / `1` | handler 传 `IncludeBackfilled=true`，store 不添加 backfill exclusion |
| `include_backfilled=bad` | handler 返回 400 `invalid include_backfilled` |
| runtime fact backfilled row exists but事件对象/时间不匹配 | 不视为该事件的 backfill provenance |
| store 查询失败 | handler 返回 500 `internal server error`，store error 用 `%w` 包装 |

#### 5. Good/Base/Bad Cases

- Good: backfilled probe recovery event 默认不出现在事件流；用户打开“包含补传事件”后 `/api/events?include_backfilled=true` 返回它。
- Base: 普通 live event 没有关联 backfilled facts，默认和 include 模式都能返回。
- Bad: 在 `state_change_events` 写路径临时塞 `is_backfilled`、或丢弃 backfilled raw facts 来实现 UI 筛选。
- Bad: handler 解析成功但前端仍禁用 toggle，造成 UI 与 API 契约漂移。

#### 6. Tests Required

- Handler test: 成功响应顶层必须是 object，并包含 `items` 数组，不能回退为 bare JSON array。
- Handler test: `include_backfilled=true` 进入 `store.EventsFilter.IncludeBackfilled`。
- Handler test: `include_backfilled=bad` 返回 400。
- Store test: 默认 SQL 包含 monitoring instance heartbeat / host sample / probe observation 的 `is_backfilled` exclusion。
- Store test: `IncludeBackfilled=true` 时 SQL 不包含 backfill exclusion。
- Cross-layer test: `web/src/lib/api.test.ts` 断言 `include_backfilled: true` 序列化为 `include_backfilled=true`。

#### 7. Wrong vs Correct

```go
// 错误：默认查询把 backfilled provenance 忽略掉，UI toggle 只是空操作。
records, err := repo.ListEvents(ctx, store.EventsFilter{Limit: 50})

// 正确：默认 store 查询排除可关联 backfilled facts 的事件。
records, err := repo.ListEvents(ctx, store.EventsFilter{Limit: 50, IncludeBackfilled: false})
```

```sql
-- 错误：假设 state_change_events 有 is_backfilled 列。
where e.is_backfilled = false

-- 正确：从 runtime facts 建立只读关联。
where not exists (
  select 1 from probe_observations po
  where po.target_id = e.object_id
    and po.is_backfilled
    and po.observed_at = e.created_at
)
```

---

## 模型层关键不变量

> 来源：`docs/design/v1-baseline/architecture-data-model.md` + `CLAUDE.md` "Key model invariants"。**任何 SQL / 仓库 / 服务改动都必须先验证这些不变量没被破坏**。

1. **MonitoringInstance = agent 接入后的运行观测对象**。同一台机重装系统后可保持同一个 MonitoringInstance（保留 `monitoring_instance_id` 与历史时间序列）；换了硬件或明确的新 agent identity 应新建 MonitoringInstance，不要在旧 `monitoring_instance_id` 上重新绑定异种主机。指纹变化通过 `binding_status = '指纹变更待确认'` 进入 `pending_binding_*` 字段（见 `monitoring_instances` 表与 `internal/center/enrollment/`）。
2. **Target = 一个可观测入口**，地址 (`host` / `base_port`) 属于 Target；`ProbeItem` 仅描述**如何观测**它（探针种类、频率档、超时、配置），不再额外存地址。Target 与 ProbeItem 是 1:N，删除 Target 级联清理 ProbeItem (`on delete cascade`)。
3. **探针种类只有 `tcp` / `http` / `tls`**（`internal/contracts/agentapi/types.go` 中的 `ProbeKind*` 常量）。`https` 不是独立种类，而是带 TLS 配置的 HTTP 观测。新增种类必须先获得基线批准，并同步更新设计文档与契约包。
4. **健康状态 (`current_health_status`) 是派生量**（`正常 / 关注 / 告警 / 严重`），由 incident service 在写后计算并回写；**不要直接接受外部 API 的健康字段写入**。
5. **MonitoringInstance 生命周期状态 (`lifecycle_status`) 是 VPS 附属接入/收尾事实，不是独立业务状态入口**（`待接入 / 在用 / 观察中 / 不续费 / 已退役`）。普通监控 handler 只能处理运行控制、接入、绑定和 metadata；退役/不续费类变更只能从 VPS 生命周期工作台的 `asset_lifecycle` 联动路径写入，并记录审计步骤。其他写路径不应触碰该列。
6. **维护模式 (`monitoring_status = '维护中'` / `'暂停'`) 是 runtime control，不是健康状态**。维护期间观测照常落库（`maintenance_context = true`），但 incident / notification 处理需识别该上下文（参考 `store/monitoring_instances.go:74-77`、`incidents/service.go`）。
7. **请求路径只写原始观测**：handler 接收 sync batch 后通过 `internal/center/syncing/` 落 `monitoring_instance_heartbeats` / `host_samples` / `probe_observations`，**不在请求路径里跑 incident 判定 / 通知**。incident 与通知由 `incidentSvc`（`incidents.NewSettingsBackedService`，启动时作为 `Worker.Run(ctx)` 跑）异步产出。
8. **回填观测 (`is_backfilled = true`) 必须落库但不得触发实时告警**。请求路径仍旧 `insert`（参见 `store/sync_batches.go:188`），但 incident service 在 select 阶段对历史数据的处理需带条件分支。**不要在 incident 判定里忽略 `is_backfilled` 字段，也不要在写路径里干脆丢弃这条数据**。
9. **notification_records.channel 是真实发送通道，不是 evaluator 默认值**。`incidents.NotificationChannel` 当前只允许 `telegram` / `feishu` 作为生产通道语义；Feishu-only 发送只写 `channel='feishu'`，Telegram+Feishu 混合发送必须按 channel 写多条 record，单个 channel 失败只能把该 channel 标为 `failed`。通知策略关闭、维护/回填抑制或无可用 channel 时写 `suppressed`，但不能把 Feishu-only 或 mixed delivery 误记成 Telegram-only。

---

## Common Mistakes / 反模式

> 这些是当前代码库已经避免的写法，**新代码也不要做**。

- ❌ **引入 ORM**（GORM、ent、sqlc 生成器等）。手写 SQL 是项目硬性约束。
- ❌ **在 handler / service 里直接拼 SQL**。所有 SQL 必须落到 `internal/center/store/<aggregate>.go`，handler 只调用仓库方法。
- ❌ **绕过迁移文件改 schema**。任何 DDL 都要走 `db/migrations/`，并保持迁移幂等。
- ❌ **修改已合入的迁移**。需要修复就追加新迁移做 `alter`，不要回写历史。
- ❌ **在请求路径内做 incident 判定 / 发通知**。判定 + 通知是 in-process worker 的职责（`incidentSvc`、`notify` 包）。
- ❌ **在多个包重复定义同一张表的列结构**。仓库内的 select/insert 列清单是单一来源；DTO 与领域类型放对应包（`monitoringinstances/`、`targets/`、`incidents/` 等）。
- ❌ **写路径里偷偷跳过回填数据**。`is_backfilled = true` 仍要落库，只是不能反向触发告警。
- ❌ **直接 `select *`**。请求只取需要的列，便于追踪 schema 演进。
- ❌ **对回填或维护期数据做删除/覆盖**。原始观测层是 append-only，retention 由 `internal/center/retention/` 与 `db/migrations/0008_add_retention_aggregates.sql` 配合实现。
