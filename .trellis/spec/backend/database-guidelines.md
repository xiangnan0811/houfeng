# 数据库规范

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风 V1 后端持久化栈是 **PostgreSQL + pgx/v5**，没有 ORM、没有 query builder。所有 SQL 都是手写原生语句，迁移文件 (`db/migrations/*.sql`) 是 schema 的**唯一权威源**——不允许通过 ORM auto-migrate、SQL 控制台或运维脚本绕过迁移修改 schema。

核心约定一句话总结：
- **driver**：`github.com/jackc/pgx/v5` 与 `github.com/jackc/pgx/v5/pgxpool`，连接池在 `cmd/houfeng-center/bootstrap.go` 内构造（参见 `bootstrap.go:60-69`，调用 `store.OpenPostgres`）。
- **仓库**：`internal/center/store/` 下一文件一 aggregate（`nodes.go`、`targets.go`、`incidents.go`、`sync_batches.go` 等）。
- **schema 演进**：`db/migrations/0001_*.sql` … 当前最大 migration（现为 `0024_create_asset_domains.sql`）+ `db/migrations/embed.go` 用 `embed.FS` 嵌入；启动时由 `internal/center/store/migrate/migrate.go` 中的 `Apply` 顺序应用，状态记在 `schema_migrations` 表。
- **事务边界**：写多张表时使用 `pgx.Tx`，参考 `store/sync_batches.go:40-91` 的 `ApplyBatch`（一次同步批次串起 4-5 张表的写入与一次 plan 计算）。
- **不变量**：领域规则（Node/Target/Probe 语义、健康状态派生、回填观测不告警）必须落到 SQL + 仓库 + 服务层共同遵守，详见后文。

---

## Query Patterns

### 基本约定

- **统一通过 `internal/center/store/<aggregate>.go` 访问数据库**。HTTP handler、incident service、retention worker 等都不直接持有 `*pgxpool.Pool`，而是接受领域接口（`nodes.Repository`、`targets.Repository`、`syncing.Repository` 等）的具体实现。
- 仓库构造器固定签名 `NewPostgres<Aggregate>Repository(*pgxpool.Pool) *Postgres<Aggregate>Repository`（参考 `store/nodes.go:34-36`、`store/sync_batches.go:30-36`）。
- 每个仓库通过 `var _ <domain>.Repository = (*Postgres<Aggregate>Repository)(nil)` 显式断言接口契约（参见 `store/nodes.go:70-72`）。

### SQL 写法

- 直接用 `pgx` 的 `Query` / `QueryRow` / `Exec`，参数化占位符使用 `$1, $2, ...`，**严禁字符串拼接 SQL**。
- 复用列清单时定义包级常量字符串（如 `nodeSelectColumns`，参见 `store/nodes.go:38-64`），避免 select / insert 列漂移。
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

  实例：`store/sync_batches.go:40-91` 把 `validateAcceptedSyncBatch → recordHeartbeatBatch → recordObservationBatch → advanceNodeSyncState → buildSyncPlan` 串在同一个事务内，保证一次 agent sync 是原子的。
- 仓库可以把 `BeginTx` 通过函数字段注入（`PostgresSyncRepository.beginTx`，见 `store/sync_batches.go:26-36`），便于在测试里替换为内存实现，实际线上仍走 `*pgxpool.Pool`。

### 分页 / 时间窗口

- 时间序列表（`node_heartbeats`、`host_samples`、`probe_observations`、`state_change_events` 等）一律按 `(<entity>_id, observed_at desc)` 排序，并配套 `idx_<table>_<entity>_time` 索引（见 `db/migrations/0001_initial_schema.sql:145-150`）。
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
2. 在 `db/migrations/` 新建下一个未占用序号的文件，例如当前最大为 `0024_create_asset_domains.sql` 时，下一个应为 `0025_<verb>_<scope>.sql`。
3. 文件内只允许 `create / alter / drop / insert` 等 DDL/DML 语句，不要在里面写 Go。
4. 同时更新对应 `internal/center/store/<aggregate>.go` 的 `select` 列、`insert` / `update` 语句、读写函数签名。
5. 跑 `make verify-go`（含 `migrate` 包的单测，见 `migrate_test.go`）；接着按 `docs/operations/v1-smoke-run.md` 在真 Postgres 上做 fresh-install smoke。

### 不要做

- ❌ 修改已经合并/发布过的迁移文件内容（包括加空格）。要修就再写一个新迁移。
- ❌ 用任何运维脚本 / SQL 客户端直接改线上 schema，必须走迁移文件。
- ❌ 把测试数据 / seed 数据写进迁移文件——种子用户由 `internal/center/auth/seed.go` 在 bootstrap 阶段执行（`bootstrap.go:104-107`）。

> ⚠️ **已知 gap**：当前 `db/migrations/` 里存在两个 `0004_*` 文件 (`0004_add_node_onboarding_binding_state.sql`、`0004_add_observation_provenance.sql`)。`migrate.Apply` 按文件名字典序排序，二者顺序由后缀决定，并不冲突；但序号撞车违反了"序号唯一"的隐含约定，新增迁移时**必须先查看 `db/migrations/`，再使用当前最大编号之后的下一个未占用序号**（当前最大为 `0024_create_asset_domains.sql`，下一个应为 `0025_*`，如果期间已有新迁移则继续顺延）。

---

## Naming Conventions

参考 `db/migrations/0001_initial_schema.sql`、`0010_add_users_and_sessions.sql` 的实际风格：

| 对象 | 规则 | 例子 |
|------|------|------|
| 表名 | `snake_case`，复数（如果是聚合事实表则单数+aggregates 后缀） | `nodes`、`targets`、`probe_items`、`probe_observations`、`node_host_sample_daily_aggregates` |
| 主键 | `<entity>_id text primary key`（业务主键，由 `internal/center/ids/ids.go` 生成）；纯事实表用 `id bigserial primary key` | `node_id text primary key`、`id bigserial primary key`（`host_samples`） |
| 外键 | `<other>_id text not null references <other_table>(<other_id>) on delete cascade` | `target_id text not null references targets(target_id) on delete cascade`（`probe_items`） |
| 时间戳 | 一律 `timestamptz`，业务列 `created_at` / `updated_at` 默认 `now()` | 见 `nodes` / `targets` |
| 布尔 | `<adj>` 或 `is_<adj>`；带默认值 | `is_backfilled boolean not null default false`、`maintenance_context boolean not null default false` |
| JSONB 列 | 默认值用 `'{}'::jsonb` 或具体默认对象 | `config jsonb not null default '{}'::jsonb`（`probe_items`）、`incident_defaults jsonb not null default '{...}'::jsonb`（`center_settings`） |
| 数组列 | `text[] not null default '{}'` | `labels text[] not null default '{}'` |
| 索引 | `idx_<table>_<purpose>`；GIN 索引带 `_gin` 后缀 | `idx_node_heartbeats_node_time`、`idx_nodes_labels_gin` |
| 唯一约束 | 表内列 `unique`，或单独命名 | `username text not null unique`（`users`） |

> 例外：`db/migrations/0010_add_users_and_sessions.sql` 中的 `sessions_user_idx` / `sessions_expires_idx` 没有 `idx_` 前缀，是仓库现存差异；新增索引请遵循 `idx_<table>_<purpose>` 主流写法。

### Node enrollment token one-time consumption

#### 1. Scope / Trigger

- Trigger: 修改 Node enrollment token issuance/validation、`nodes.enrollment_token_*` 字段、`/api/agent/enroll`、或 Node onboarding install-command 生成路径。
- 目标：enrollment token 是一次性 bootstrap secret，不是长期 agent credential；成功绑定或进入待确认 fingerprint 路径后不得继续复用同一 token。

#### 2. Signatures

- DB columns: `nodes.enrollment_token_hash`, `nodes.enrollment_token_issued_at`, `nodes.enrollment_token_consumed_at`。
- Domain constant: `nodes.EnrollmentTokenTTL = 30 * time.Minute`。
- Issue method: `IssueNodeEnrollmentToken(ctx, nodeID) -> nodes.EnrollmentTokenIssue{Token, IssuedAt, ExpiresAt}`。
- Validation path: `/api/agent/enroll` consumes a matching active token before or during binding evaluation.

#### 3. Contracts

- Token lookup must require non-empty hash, `enrollment_token_consumed_at is null`, and `enrollment_token_issued_at >= now() - nodes.EnrollmentTokenTTL`.
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
| Node missing during issue | `nodes.ErrNodeNotFound` |
| DB failure during consume/bind | transaction rolls back; caller returns wrapped repository error |

#### 5. Good/Base/Bad Cases

- Good: user generates command, runs it once within 30 minutes, agent binds and receives sync token; the bootstrap token is consumed.
- Base: user waits beyond 30 minutes; command fails at enroll and user regenerates from onboarding page.
- Bad: leaving multiple generated tokens valid lets shell history or chat leaks enroll the same Node later.
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

### Node command action durability

#### 1. Scope / Trigger

- Trigger: 修改 Node action / remote command 链路时必须加载本节，包括 `POST /api/nodes/{node_id}/actions`、`agentapi.PendingAction`、`agentapi.CommandResult`、`syncing.CommandResult`、`nodes.last_action` 或 `store/sync_batches.go`。
- 目标：单 pending action 模型下保持 command identity 可追踪，避免 agent 结果晚到时覆盖另一个已排队或派发中的 action。

#### 2. Signatures

- HTTP request: `POST /api/nodes/{node_id}/actions` with body `{"command_id":"uptime"}`。
- HTTP response: `{"action_id":"act_xxx","command_id":"uptime","status":"pending"}`。
- Agent plan: `agentapi.PendingAction{ActionID, CommandID}` serializes as `action_id` + `command_id`。
- Agent result: `agentapi.CommandResult{ActionID, CommandID, Stdout, Stderr, ExitCode}` serializes as `action_id` + `command_id` + output fields。
- DB state: `nodes.pending_action_id`, `nodes.pending_action_command_id`, and `nodes.last_action jsonb`。

#### 3. Contracts

- Queueing an action writes both pending columns and `last_action={"status":"pending","action_id":...,"command_id":...}` so API/UI readers see pending immediately.
- Sync dispatch clears `pending_action_*` columns to prevent duplicate dispatch, but rewrites the same pending `last_action` to keep the in-flight identity durable until a matching result arrives.
- Command result storage must include the real `command_id` and update `last_action` to `status="done"` only when current `last_action` is still `pending` with the same `action_id` and `command_id`.
- `last_action.status` currently uses only `pending` and `done`; command success/failure is represented by `exit_code`, not by `success` / `failed` status strings.
- Go `nodes.LastAction.ExitCode` must stay nullable (`*int`) with `omitempty`: pending actions omit it, while completed success still serializes `exit_code: 0`.
- `last_action` is the current visible action state, not a full audit log. Do not infer historical command execution from it after another action is queued.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Missing `command_id` in Node action request | 400 `command_id required` |
| Unknown node | 404 `node not found` |
| Node is not bound | 409 `node agent not bound` |
| Node monitoring is paused | 409 `node monitoring is paused` |
| Agent result lacks `action_id` or `command_id` | Ignore the result row; do not overwrite `last_action` |
| Agent result identity does not match current pending `last_action` | Ignore the result row; do not overwrite `last_action` |
| DB write failure while queueing/dispatching/storing | Return wrapped repository error; handler maps to 500 where applicable |

#### 5. Good/Base/Bad Cases

- Good: user queues `uptime`, API immediately returns `command_id`, `last_action` shows pending `uptime`, agent returns matching `action_id` + `command_id`, and `last_action` becomes done with stdout/stderr/exit code.
- Base: no pending action and no command results in a sync batch leaves `last_action` unchanged.
- Bad: writing `last_action.command_id=""` from command results makes the UI lose the command label.
- Bad: storing command results with `WHERE node_id = $2` only can let a stale result overwrite a newer pending action.

#### 6. Tests Required

- Agent runtime test: pending action execution returns `CommandResult.ActionID` and `CommandResult.CommandID`.
- Agent handler test: sync request conversion preserves `command_results[].command_id`.
- Store tests: queueing writes pending `last_action`; dispatch clears pending columns while preserving pending JSON; result update SQL guards on pending status, action ID, and command ID; `UPDATE 0` mismatch is non-fatal; result storage runs before dispatching a newly queued action in the same sync transaction.
- Frontend API/page tests: `postNodeAction` preserves `command_id`; Node detail command drawer shows pending command label immediately after dispatch.

#### 7. Wrong vs Correct

```go
// 错误：丢失 command identity，且 stale result 可覆盖当前 action。
payload := map[string]any{"action_id": result.ActionID, "command_id": "", "status": "done"}
_, err := tx.Exec(ctx, `UPDATE nodes SET last_action = $1 WHERE node_id = $2`, payload, nodeID)
```

```go
// 正确：结果只落到仍匹配的 pending action。
_, err := tx.Exec(ctx, `
	UPDATE nodes
	SET last_action = $1, updated_at = now()
	WHERE node_id = $2
		AND last_action->>'status' = $3
		AND last_action->>'action_id' = $4
		AND last_action->>'command_id' = $5`,
	raw, nodeID, "pending", result.ActionID, result.CommandID)
```

### Asset Ledger providers

`db/migrations/0016_create_asset_ledger.sql` 是 post-V1 Asset Ledger 的 schema 入口，当前落 `providers` 服务商主数据表：

- `providers.provider_id` 使用 `ids.New("pv")` 生成，字段和 JSON contract 保持英文稳定值。
- `name` 必须通过数据库 `providers_name_not_blank` 约束保证 trim 后非空；领域层也必须在 create / patch 时校验。
- `rating` 是 nullable `integer`，只允许 `null` 或 `1..5`，数据库约束为 `providers_rating_range`。
- `labels` 使用 `text[] not null default '{}'`；领域层负责 trim 和过滤空标签。
- provider CRUD 不得自动改写、规范化或 backfill `nodes.provider`。`nodes.provider` 仍是 Fleet Observability 的节点元数据字符串，Asset Ledger provider 是独立资产层主数据。

### Asset Ledger VPS assets

`db/migrations/0017_add_vps_assets.sql` 添加 `vps_assets`，代表资产层 VPS 账本。它依赖 `providers.provider_id`，但仍与 Fleet Observability 的 `nodes.provider` 字符串保持分离。

- `vps_assets.vps_id` 使用 `ids.New("vps")` 生成。
- `provider_id` 可为 `null`；存在时必须引用 `providers(provider_id)`，并在 provider 删除时 `on delete set null`。
- `provider_name` 是导入 / 展示兼容字符串，不能创建、更新或回填 `providers`。
- `display_name` 必须由数据库 `vps_assets_display_name_not_blank` 约束保证 trim 后非空；领域层 create / patch 也必须校验。
- `lifecycle_status`、`usage_status`、`renewal_decision` 使用稳定英文机器值，并分别由数据库 check 约束和领域校验共同保护。
- `ssh_port` 默认为 `22`，数据库约束为 `1..65535`；领域 create 中 `0` 表示省略并默认，patch 中显式 `0` 必须拒绝。
- `archived_at` 是派生字段：生命周期切到 `archived` 时补时间，从 `archived` 切出时清空；API 输入不得任意写入 `archived_at`。
- VPS 资产 CRUD 不得改写 `nodes.provider`，也不得改变 Node / Target / Agent 的既有语义。
- subscription summary 属于 subscriptions 查询；active node link count / node summary 由 `assetlinks.Repository` 在 HTTP 展示层补充，不得让 `store/vps_assets.go` 直接耦合 Node 表或 link 表细节。

### Asset Ledger subscriptions

`db/migrations/0018_add_subscriptions.sql` 添加 `subscriptions`，代表资产层 VPS 订阅账本。它依赖 `vps_assets.vps_id`，但不得反向改写 VPS 资产、Provider、Node、Target 或 Agent 状态。

- `subscriptions.subscription_id` 使用 `ids.New("sub")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时 `on delete cascade`。
- `currency` 使用大写 3 字母代码；领域层负责 trim + uppercase，数据库约束兜底。
- `price` 对齐数据库 `numeric(12, 2)`，领域层必须拒绝负数、超过精度或超过 2 位小数的输入，避免入库四舍五入后与派生字段漂移。
- `monthly_price` 是后端派生字段，按 `price / billing_months` 计算并四舍五入到 4 位小数；create / patch JSON 不接受 `monthly_price`，patch 修改 `price` 或 `billing_months` 时必须重新计算。
- `started_at` 与 `renew_at` 是 nullable `date`：未知日期用 `null`，不要写假日期。
- `status` 使用稳定英文机器值：`active`、`paused`、`cancelled`、`expired`、`unknown`。
- 订阅 CRUD 不得创建 `vps_node_links`、不得改写 `nodes.provider`、不得增加 Dashboard / import / currency exchange 行为。

### Asset Ledger VPS node links

`db/migrations/0019_create_vps_node_links.sql` 添加 `vps_node_links`，用于连接资产层 VPS 与 Fleet Observability 的 `nodes`。它是关联历史表，不是 Node 状态机的一部分。

- `vps_node_links.link_id` 使用 `ids.New("vnl")` 生成，避免用 `(vps_id, node_id, linked_at)` 做 API identity。
- `vps_id` 必须引用 `vps_assets(vps_id)`，`node_id` 必须引用 `nodes(node_id)`；删除 VPS 或 Node 时可以级联清理 link 历史。
- active link 定义为 `unlinked_at is null`。`idx_vps_node_links_pair_active` 必须保证同一 `(vps_id, node_id)` 同时最多一条 active link。
- unlink 必须写 `unlinked_at`，不得物理删除；如果提供 note，只更新 link note，不改 Node 或 VPS 业务字段。
- link / unlink 不得改写 `nodes.provider`、Node `lifecycle_status`、`monitoring_status`、`current_health_status`、Target、Agent 或 subscription。
- VPS item/list API 可以补 `active_node_link_count`，VPS detail 可以返回 active Node 摘要；这些摘要通过 `internal/center/assetlinks.Repository` 查询，不要把 Node 查询 SQL 塞进 `store/vps_assets.go`。
- Node 侧 VPS 摘要使用独立 `/api/nodes/{node_id}/vps` 查询，不把资产字段混入基础 `nodes.Record`。

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
- 续费决策历史不得创建 `vps_node_links`，不得改写 `nodes.provider`、Node lifecycle / monitoring / health、Target、Agent 或 subscription。

`db/migrations/0021_create_asset_histories.sql` 添加 `price_histories`、`ip_histories`、`vps_spec_snapshots`，用于补齐资产层价格、IP、规格变化历史。三张表补充当前状态字段，不替代 `subscriptions` 或 `vps_assets` 当前状态。

- `price_histories.price_history_id` 使用 `ids.New("ph")` 生成；`ip_histories.ip_history_id` 使用 `ids.New("iph")`；`vps_spec_snapshots.snapshot_id` 使用 `ids.New("vss")`。
- `price_histories` 必须同时引用 `subscriptions(subscription_id)` 和 `vps_assets(vps_id)`；subscription PATCH 只有在价格、币种、计费周期、计费月数、月付折算、续费日、自动续费标记或状态最终发生变化时才插入历史。
- subscription 当前状态更新与 price history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 subscription 行，避免当前订阅和历史漂移。
- `ip_histories` 必须记录 IPv4 / IPv6 前后值，且只有至少一个 IP 字段变化时才插入；数据库约束 `ip_histories_changed` 兜底拒绝无变化历史。
- `vps_spec_snapshots` 记录变化后的规格快照：`product_name`、`ssh_host`、`ssh_port`、`ssh_user`、`os_name`、`virtualization`。VPS PATCH 只有这些字段最终发生变化时才插入 snapshot。
- VPS 当前状态更新与 IP / spec history insert 必须复用 VPS history 事务路径，并先 `select ... for update` 锁定 VPS 行。
- 所有 history 仍属于 Asset Ledger，不得创建 `vps_node_links`，不得改写 `nodes.provider`、Node lifecycle / monitoring / health、Target、Agent 或 provider。

`db/migrations/0022_create_experience_logs.sql` 添加 `experience_logs`，用于记录单台 VPS 的人工体验、稳定性、网络、账单、服务支持、迁移或取消原因。它补充资产历史，不替代 `vps_assets.note` 或续费决策历史。

- `experience_logs.experience_log_id` 使用 `ids.New("elog")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `category` 使用稳定英文机器值：`note`、`stability`、`network`、`support`、`billing`、`migration`、`cancellation`；数据库 check 约束与领域校验共同保护。
- `severity` 使用稳定英文机器值：`info`、`warning`、`critical`；数据库 check 约束与领域校验共同保护。
- `summary` 必须 trim 后非空；`details` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `occurred_at` 默认 `now()`，领域入口可传入 UTC 时间；experience log 列表按 `occurred_at desc, created_at desc, experience_log_id desc` 排序。
- `GET /api/vps/{vps_id}/experience-logs` 只返回该 VPS 的经验记录；VPS 不存在时返回 asset timeline not found 语义。
- `POST /api/vps/{vps_id}/experience-logs` 的 path `vps_id` 是唯一 VPS 来源，请求 body 不接受覆盖 `vps_id`；写入只创建 experience log，不改写 VPS 当前字段。
- experience log 不得创建 `vps_node_links`，不得改写 `nodes.provider`、Node lifecycle / monitoring / health、Target、Agent、Provider、VPS 当前状态或 subscription。

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
- Bad: 经验记录写入后顺手修改 `vps_assets.note`、续费决策或 Node 状态。

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
- service asset 写入不得改变 VPS 当前状态、subscription、experience log、Node、Target、ProbeItem、Agent 或 `nodes.provider`。

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
- domain asset 写入不得改变 VPS 当前状态、subscription、experience log、service asset、Node、Target、ProbeItem、Agent 或 `nodes.provider`。

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
- dry-run 必须报告 provider 创建候选、VPS 创建候选、subscription 创建候选、缺失 provider、缺失续费日期、非法字段、重复候选、Node 关联候选、未来 30 天续费候选和闲置但付费候选。
- 数据库可用时，dry-run 可以读取现有 providers / vps_assets / subscriptions / nodes 做重复和 Node 候选诊断；数据库不可用时仍应能完成纯文件模型校验。
- `-import` 必须显式开启，且在一个事务中按 provider → VPS asset → subscription 顺序写入；校验错误或重复候选存在时拒绝写入。
- import 不接受也不写 `monthly_price`，仍由 subscription 后端计算。
- import 不创建 `vps_node_links`，不改写 `nodes.provider`，不改变 Node / Target / Agent 语义。Node 相关输入只能作为人工确认候选进入报告。

### Asset Ledger Dashboard summary

`internal/center/store/dashboard.go` 可以读取资产层表来生成 `/api/dashboard` 的少量决策摘要，但它仍是 Dashboard read model，不是资产 CRUD 仓库。

- `incidents.DashboardOverview.AssetSummary` 的 JSON contract 是 `asset_summary`，只允许返回聚合计数和按币种成本分组，不返回 VPS、subscription、Node 或 provider 明细数组。
- `asset_summary` 的 30 天续费口径：`subscriptions.status = 'active'`，`renew_at >= current_date` 且 `renew_at <= current_date + 30`，并只统计未取消/未归档的 VPS。
- active VPS 口径：`vps_assets.lifecycle_status not in ('cancelled', 'archived')`。
- active link 口径：`vps_node_links.unlinked_at is null`。
- 异常关联 VPS 口径：active link 关联到 `nodes.current_health_status <> '正常'` 的 Node；只读 Node 派生状态，不改写 Node。
- 成本口径：`sum(active subscriptions monthly_price)` 按 `currency` 分组，`yearly_total = monthly_total * 12`；第一阶段不做汇率换算。
- 该查询不得改变 `nodes.provider`、Node lifecycle / monitoring / health、Target、Agent、VPS、subscription 或 link 记录。
- `limit` 只限制异常队列和 recent events；不得限制 `asset_summary`。

### Scenario: Events backfilled filter contract

#### 1. Scope / Trigger

- Trigger: 修改 `/api/events` 查询参数、`store.EventsFilter`、`PostgresDashboardRepository.ListEvents`，或改动 `state_change_events` 与 runtime facts 的 backfill 展示语义。

#### 2. Signatures

- Backend API: `GET /api/events?include_backfilled=<bool>` -> `{"items":[]}`。
- Handler field: `store.EventsFilter.IncludeBackfilled bool`。
- DB source rows: `state_change_events e`；backfill provenance lives in `node_heartbeats.is_backfilled`、`host_samples.is_backfilled`、`probe_observations.is_backfilled`。

#### 3. Contracts

- `/api/events` 成功响应返回 envelope：`{"items":[...]}`；错误响应保持通用 `{"error":"..."}`。
- `include_backfilled` 使用 Go `strconv.ParseBool` 解析；前端 URL 用 `include_backfilled=1`，API 请求用 `include_backfilled=true`。
- 默认 `IncludeBackfilled=false` 时，`ListEvents` 必须排除可关联到 backfilled runtime facts 的事件。
- `IncludeBackfilled=true` 时不得添加 backfill exclusion predicate。
- 由于 `state_change_events` 没有 `is_backfilled` 列，backfill 关联只在 read model 查询中通过对象和时间建立：
  - node event: `e.object_type = 'node'` 且存在同 `node_id`、同 `observed_at = e.created_at` 的 backfilled `node_heartbeats` 或 `host_samples`。
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
- Store test: 默认 SQL 包含 node heartbeat / host sample / probe observation 的 `is_backfilled` exclusion。
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

1. **Node = 一台具体的服务器**。同一台机重装系统后**仍然是同一个 Node**（保持 `node_id` 与历史时间序列）；换了硬件则**必须新建 Node**，不要在旧 `node_id` 上重新绑定异种主机。指纹变化通过 `binding_status = '指纹变更待确认'` 进入 `pending_binding_*` 字段（见 `nodes` 表与 `internal/center/enrollment/`）。
2. **Target = 一个可观测入口**，地址 (`host` / `base_port`) 属于 Target；`ProbeItem` 仅描述**如何观测**它（探针种类、频率档、超时、配置），不再额外存地址。Target 与 ProbeItem 是 1:N，删除 Target 级联清理 ProbeItem (`on delete cascade`)。
3. **V1 探针种类只有 `tcp` / `http` / `https` / `tls`**（`internal/contracts/agentapi/types.go:30-34` 中的 `ProbeKind*` 常量）。新增种类必须先获得基线批准，并同步更新设计文档与契约包。
4. **健康状态 (`current_health_status`) 是派生量**（`正常 / 关注 / 告警 / 严重`），由 incident service 在写后计算并回写；**不要直接接受外部 API 的健康字段写入**。
5. **生命周期状态 (`lifecycle_status`) 是托管量**（`待接入 / 在用 / 观察中 / 不续费 / 已退役`），通过专用 handler (`runtime_controls.go` + `node_onboarding.go`) 改变；其他写路径不应触碰该列。
6. **维护模式 (`monitoring_status = '维护中'` / `'暂停'`) 是 runtime control，不是健康状态**。维护期间观测照常落库（`maintenance_context = true`），但 incident / notification 处理需识别该上下文（参考 `store/nodes.go:74-77`、`incidents/service.go`）。
7. **请求路径只写原始观测**：handler 接收 sync batch 后通过 `internal/center/syncing/` 落 `node_heartbeats` / `host_samples` / `probe_observations`，**不在请求路径里跑 incident 判定 / 通知**。incident 与通知由 `incidentSvc`（`incidents.NewSettingsBackedService`，启动时作为 `Worker.Run(ctx)` 跑）异步产出。
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
- ❌ **在多个包重复定义同一张表的列结构**。仓库内的 select/insert 列清单是单一来源；DTO 与领域类型放对应包（`nodes/`、`targets/`、`incidents/` 等）。
- ❌ **写路径里偷偷跳过回填数据**。`is_backfilled = true` 仍要落库，只是不能反向触发告警。
- ❌ **直接 `select *`**。请求只取需要的列，便于追踪 schema 演进。
- ❌ **对回填或维护期数据做删除/覆盖**。原始观测层是 append-only，retention 由 `internal/center/retention/` 与 `db/migrations/0008_add_retention_aggregates.sql` 配合实现。
