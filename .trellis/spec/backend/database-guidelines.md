# 数据库规范

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风 V1 后端持久化栈是 **PostgreSQL + pgx/v5**，没有 ORM、没有 query builder。所有 SQL 都是手写原生语句，迁移文件 (`db/migrations/*.sql`) 是 schema 的**唯一权威源**——不允许通过 ORM auto-migrate、SQL 控制台或运维脚本绕过迁移修改 schema。

核心约定一句话总结：
- **driver**：`github.com/jackc/pgx/v5` 与 `github.com/jackc/pgx/v5/pgxpool`，连接池在 `cmd/houfeng-center/bootstrap.go` 内构造（参见 `bootstrap.go:60-69`，调用 `store.OpenPostgres`）。
- **仓库**：`internal/center/store/` 下一文件一 aggregate（`nodes.go`、`targets.go`、`incidents.go`、`sync_batches.go` 等）。
- **schema 演进**：`db/migrations/0001_*.sql` … 当前最大 migration（现为 `0020_create_renewal_decisions.sql`）+ `db/migrations/embed.go` 用 `embed.FS` 嵌入；启动时由 `internal/center/store/migrate/migrate.go` 中的 `Apply` 顺序应用，状态记在 `schema_migrations` 表。
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
2. 在 `db/migrations/` 新建下一个未占用序号的文件，例如当前最大为 `0020_create_renewal_decisions.sql` 时，下一个应为 `0021_<verb>_<scope>.sql`。
3. 文件内只允许 `create / alter / drop / insert` 等 DDL/DML 语句，不要在里面写 Go。
4. 同时更新对应 `internal/center/store/<aggregate>.go` 的 `select` 列、`insert` / `update` 语句、读写函数签名。
5. 跑 `make verify-go`（含 `migrate` 包的单测，见 `migrate_test.go`）；接着按 `docs/operations/v1-smoke-run.md` 在真 Postgres 上做 fresh-install smoke。

### 不要做

- ❌ 修改已经合并/发布过的迁移文件内容（包括加空格）。要修就再写一个新迁移。
- ❌ 用任何运维脚本 / SQL 客户端直接改线上 schema，必须走迁移文件。
- ❌ 把测试数据 / seed 数据写进迁移文件——种子用户由 `internal/center/auth/seed.go` 在 bootstrap 阶段执行（`bootstrap.go:104-107`）。

> ⚠️ **已知 gap**（值得记入 `docs/release/v1-gap-checklist.md`）：当前 `db/migrations/` 里存在两个 `0004_*` 文件 (`0004_add_node_onboarding_binding_state.sql`、`0004_add_observation_provenance.sql`)。`migrate.Apply` 按文件名字典序排序，二者顺序由后缀决定，并不冲突；但序号撞车违反了"序号唯一"的隐含约定，新增迁移时**必须先查看 `db/migrations/`，再使用当前最大编号之后的下一个未占用序号**（当前最大为 `0020_create_renewal_decisions.sql`，下一个应为 `0021_*`，如果期间已有新迁移则继续顺延）。

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

### Asset Ledger renewal decision history

`db/migrations/0020_create_renewal_decisions.sql` 添加 `renewal_decisions`，用于记录资产层 VPS 续费决策变化历史。它补充 `vps_assets.renewal_decision` 当前状态，不替代当前状态字段。

- `renewal_decisions.decision_id` 使用 `ids.New("rdec")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `from_decision` 允许 `null`，用于未来导入或补录；正常 VPS PATCH 自动记录时应写入变更前的决策值。
- `to_decision` 必须是 `vpsassets.RenewalDecision` 合法英文机器值，数据库 check 约束与领域校验共同保护。
- `reason` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `decided_at` 默认 `now()`，领域入口可传入 UTC 时间；timeline 按 `decided_at desc, created_at desc, decision_id desc` 排序。
- `PATCH /api/vps/{vps_id}` 只有在显式设置 `renewal_decision` 且最终值发生变化时才插入历史；只改其他字段或设置为原值不得插入历史。
- VPS 当前状态更新与 history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 VPS 行，避免当前状态和历史漂移。
- `GET /api/vps/{vps_id}/timeline` 第一版只返回 `renewal_decisions[]`，不得返回价格 / IP / 规格历史占位假数据。
- 续费决策历史不得创建 `vps_node_links`，不得改写 `nodes.provider`、Node lifecycle / monitoring / health、Target、Agent 或 subscription。

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

---

## 模型层关键不变量

> 来源：`docs/design/v1-baseline/architecture-data-model.md` + `CLAUDE.md` "Key model invariants"。**任何 SQL / 仓库 / 服务改动都必须先验证这些不变量没被破坏**。

1. **Node = 一台具体的服务器**。同一台机重装系统后**仍然是同一个 Node**（保持 `node_id` 与历史时间序列）；换了硬件则**必须新建 Node**，不要在旧 `node_id` 上重新绑定异种主机。指纹变化通过 `binding_status = '指纹变更待确认'` 进入 `pending_binding_*` 字段（见 `nodes` 表与 `internal/center/enrollment/`）。
2. **Target = 一个可观测入口**，地址 (`host` / `base_port`) 属于 Target；`ProbeItem` 仅描述**如何观测**它（探针种类、频率档、超时、配置），不再额外存地址。Target 与 ProbeItem 是 1:N，删除 Target 级联清理 ProbeItem (`on delete cascade`)。
3. **V1 探针种类只有 `tcp` / `http` / `https` / `tls`**（`internal/contracts/agentapi/types.go:30-34` 中的 `ProbeKind*` 常量）。新增种类必须先获得基线批准，并同步更新设计文档与契约包。
4. **健康状态 (`current_health_status`) 是派生量**（`正常 / 关注 / 告警 / 严重`），由 incident service 在写后计算并回写；**不要直接接受外部 API 的健康字段写入**。
5. **生命周期状态 (`lifecycle_status`) 是托管量**（`待接入 / 在用 / 观察中 / 不续费 / 已退役`），通过专用 handler (`runtime_controls.go` + `node_onboarding.go`) 改变；其他写路径不应触碰该列。
6. **维护模式 (`monitoring_status = '维护中'` / `'暂停'`) 是 runtime control，不是健康状态**。维护期间观测照常落库（`maintenance_context = true`），但 incident / Telegram 处理需识别该上下文（参考 `store/nodes.go:74-77`、`incidents/service.go`）。
7. **请求路径只写原始观测**：handler 接收 sync batch 后通过 `internal/center/syncing/` 落 `node_heartbeats` / `host_samples` / `probe_observations`，**不在请求路径里跑 incident 判定 / 通知**。incident 与通知由 `incidentSvc`（`incidents.NewSettingsBackedService`，启动时作为 `Worker.Run(ctx)` 跑）异步产出。
8. **回填观测 (`is_backfilled = true`) 必须落库但不得触发实时告警**。请求路径仍旧 `insert`（参见 `store/sync_batches.go:188`），但 incident service 在 select 阶段对历史数据的处理需带条件分支。**不要在 incident 判定里忽略 `is_backfilled` 字段，也不要在写路径里干脆丢弃这条数据**。

---

## Common Mistakes / 反模式

> 这些是当前代码库已经避免的写法，**新代码也不要做**。

- ❌ **引入 ORM**（GORM、ent、sqlc 生成器等）。手写 SQL 是项目硬性约束。
- ❌ **在 handler / service 里直接拼 SQL**。所有 SQL 必须落到 `internal/center/store/<aggregate>.go`，handler 只调用仓库方法。
- ❌ **绕过迁移文件改 schema**。任何 DDL 都要走 `db/migrations/`，并保持迁移幂等。
- ❌ **修改已合入的迁移**。需要修复就追加新迁移做 `alter`，不要回写历史。
- ❌ **在请求路径内做 incident 判定 / 发 Telegram**。判定 + 通知是 in-process worker 的职责（`incidentSvc`、`notify` 包）。
- ❌ **在多个包重复定义同一张表的列结构**。仓库内的 select/insert 列清单是单一来源；DTO 与领域类型放对应包（`nodes/`、`targets/`、`incidents/` 等）。
- ❌ **写路径里偷偷跳过回填数据**。`is_backfilled = true` 仍要落库，只是不能反向触发告警。
- ❌ **直接 `select *`**。请求只取需要的列，便于追踪 schema 演进。
- ❌ **对回填或维护期数据做删除/覆盖**。原始观测层是 append-only，retention 由 `internal/center/retention/` 与 `db/migrations/0008_add_retention_aggregates.sql` 配合实现。
