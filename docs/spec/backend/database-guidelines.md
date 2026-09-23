# 数据库规范

> 项目权威入口为根 `AGENTS.md`；领域行为见 [合同索引](../contracts/README.md)。

---

## Overview

候风当前后端持久化栈是 **PostgreSQL + pgx/v5**，没有 ORM、没有 query builder。所有 SQL 都是手写原生语句，迁移文件 (`db/migrations/*.sql`) 是 schema 的**唯一权威源**——不允许通过 ORM auto-migrate、SQL 控制台或运维脚本绕过迁移修改 schema。

核心约定一句话总结：
- **driver**：`github.com/jackc/pgx/v5` 与 `github.com/jackc/pgx/v5/pgxpool`，连接池在 `cmd/houfeng-center/bootstrap.go` 内构造（参见 `bootstrap.go:60-69`，调用 `store.OpenPostgres`）。
- **仓库**：`internal/center/store/` 下一文件一 aggregate（`monitoring_instances.go`、`targets.go`、`incidents.go`、`sync_batches.go` 等）。
- **schema 演进**：冻结 R1 source prefix 仍为 `0001_*.sql` … `0051_create_record_platform_foundation.sql`（含两个按文件名字典序排列的 `0004_*`，共 52 个 SQL 文件）；current root 已扩展到 `0064_add_network_rates_valid.sql`，共 65 个 source。`db/migrations/embed.go` 用 `embed.FS` 嵌入，状态记在 `schema_migrations` 表。两个 record flag 都关闭时，旧 center/importer 启动路径仍由 `internal/center/store/migrate/migrate.go` 的 `Apply` 顺序应用；`records-on/delete-off` 必须先由显式 scoped migrator收敛 current exact set 或一个明确注册、完整 golden 匹配的 predecessor，center/importer 只做 current runtime admission，绝不在启动时调用 `Apply`。
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
- 应用（仅两个 record flag 都关闭的 legacy 路径）：`cmd/houfeng-center/bootstrap.go` 调用 `migrate.Apply(ctx, db.Pool())`；后者实现见 `internal/center/store/migrate/migrate.go`：
  1. `EnsureLedger` —— 建 `schema_migrations(name primary key, applied_at)` 表
  2. 按文件名排序遍历，逐条 `HasMigration` 检查，未应用则 `ExecMigration` + `RecordMigration`
- `records-on/delete-off` 不复用这个逐迁移提交的 API：`houfeng-record-platform-admin migrate --scope app` 先编译 exact embedded set 与 post-`0051` fragment registry，再在一个 scoped `SERIALIZABLE` 事务内处理 migration、ACL 和 manifest；center/importer 的启动路径只能执行 current read-only admission。
- **每个迁移必须幂等**：`create table if not exists`、`create index if not exists`、`alter table ... add column if not exists` 是基线写法（见 `0001_initial_schema.sql`、`0009_add_observability_filter_indexes.sql`）。
- **视图列结构变化必须先 drop 再 recreate**：PostgreSQL 的 `CREATE OR REPLACE VIEW` 不能删除列、重排列或在中间插入列；否则会出现类似 `cannot change name of view column "evidence_snapshot" to "followup_todo_count"` 的启动失败。迁移需要使用 `drop view if exists <view_name>;` 后再 `create or replace view ...`，并保证依赖对象可随迁移重建。

### 流程

1. 想清楚改动是否需要持久化（业务模型变化、查询需要新索引、retention 行为变化等）。
2. 在 `db/migrations/` 新建下一个未占用序号的文件。冻结 r1 固定清单末尾是 `0051_create_record_platform_foundation.sql`，current root 已到 `0064_add_network_rates_valid.sql`，因此若没有并发新增文件，下一个候选是 `0065_<verb>_<scope>.sql`；任何 r1 之后的 APP migration 还必须在同一 PR 注册 exact current fragment，不能由 records-on 启动路径补跑。
3. 文件内只允许 `create / alter / drop / insert` 等 DDL/DML 语句，不要在里面写 Go。
4. 同时更新对应 `internal/center/store/<aggregate>.go` 的 `select` 列、`insert` / `update` 语句、读写函数签名。
5. 跑 `make verify-go`（含 `migrate` 包的单测，见 `migrate_test.go`）；接着按 `docs/operations/fresh-install-smoke-run.md` 在真 Postgres 上做 fresh-install smoke。

### 历史 / 审计字段同步

- 如果业务主表新增用户可见合同字段，且该字段会被历史、审计或决策表记录（如订阅价格历史、生命周期动作），同一个迁移必须同步补齐历史表列、backfill、约束与仓库 scan/insert 逻辑。不要只改源表导致后续审计丢失新字段。
- 兼容旧字段时，迁移需要给出可重复的推导规则和约束收口：例如订阅以 `billing_period_unit` + `billing_period_length` + `renewal_mode` 为新合同，同时从 `billing_months`、`billing_cycle`、`auto_renew`、`auto_renew_cancelled` 回填，并短期保留旧字段供下游兼容。
- 会同时更新业务事实和审计记录的动作必须在一个事务内完成。VPS 有效期延长这类操作应锁定目标 VPS，确认唯一 active subscription，写生命周期 action / step，更新 subscription `renew_at`，并在必要时写 price history。

### 不要做

- ❌ 修改已经合并/发布过的迁移文件内容（包括加空格）。要修就再写一个新迁移。
- 例外：如果某个已发布迁移在记录到 `schema_migrations` 前必然失败，导致后续修复迁移无法执行，可以修正该失败迁移本身；修复必须保持幂等，并增加回归测试说明原因。涉及约束收口时，测试必须断言 backfill 在约束前执行。
- ❌ 用任何运维脚本 / SQL 客户端直接改线上 schema，必须走迁移文件。
- ❌ 把测试数据 / seed 数据写进迁移文件——种子用户由 `internal/center/auth/seed.go` 在 bootstrap 阶段执行（`bootstrap.go:104-107`）。

> ⚠️ **已知 gap**：当前 `db/migrations/` 里存在两个 `0004_*` 文件 (`0004_add_node_onboarding_binding_state.sql`、`0004_add_observation_provenance.sql`)。前者是历史 Node 命名迁移，当前 schema 由 `0029_rename_nodes_to_monitoring_instances.sql` 迁到 MonitoringInstance 语义。legacy `migrate.Apply` 按文件名字典序排序，scoped r1 migrator 也把它们作为固定 52-source 清单中的两个独立 checksum source；二者顺序均由后缀决定，并不冲突。序号撞车仍违反“序号唯一”的隐含约定，新增迁移时**必须先查看 `db/migrations/`，再使用当前最大编号之后的下一个未占用序号**（current root 已到 `0064_add_network_rates_valid.sql`，若没有并发新增文件，下一个候选为 `0065_*`）。

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

## Common Mistakes / 反模式

> 这些是当前代码库已经避免的写法，**新代码也不要做**。

- ❌ **引入 ORM**（GORM、ent、sqlc 生成器等）。手写 SQL 是项目硬性约束。
- ❌ **在 handler / service 里直接拼 SQL**。所有 SQL 必须落到 `internal/center/store/<aggregate>.go`，handler 只调用仓库方法。
- ❌ **绕过迁移文件改 schema**。任何 DDL 都要走 `db/migrations/`，并保持迁移幂等。
- ❌ **修改已合入的迁移**。需要修复就追加新迁移做 `alter`，不要回写历史。
- ❌ **在原始观测事务内做通知或把投影失败反转成 sync 失败**。提交后的 hook 与周期 worker 边界见 [监控合同](../contracts/monitoring.md)。
- ❌ **在多个包重复定义同一张表的列结构**。仓库内的 select/insert 列清单是单一来源；DTO 与领域类型放对应包（`monitoringinstances/`、`targets/`、`incidents/` 等）。
- ❌ **写路径里偷偷跳过回填数据**。`is_backfilled = true` 仍要落库，只是不能反向触发告警。
- ❌ **直接 `select *`**。请求只取需要的列，便于追踪 schema 演进。
- ❌ **对回填或维护期数据做删除/覆盖**。原始观测层是 append-only，retention 由 `internal/center/retention/` 与 `db/migrations/0008_add_retention_aggregates.sql` 配合实现。
