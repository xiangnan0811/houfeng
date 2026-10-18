# 数据库规范

> 项目权威入口为根 `AGENTS.md`；领域行为见 [合同索引](../contracts/README.md)。

---

## Overview

候风当前后端持久化栈是 **PostgreSQL + pgx/v5**，没有 ORM、没有 query builder。所有 SQL 都是手写原生语句，迁移文件 (`db/migrations/*.sql`) 是 schema 的**唯一权威源**——不允许通过 ORM auto-migrate、SQL 控制台或运维脚本绕过迁移修改 schema。

核心约定一句话总结：
- **driver**：`github.com/jackc/pgx/v5` 与 `github.com/jackc/pgx/v5/pgxpool`，连接池在 `cmd/houfeng-center/bootstrap.go` 内构造（参见 `bootstrap.go:60-69`，调用 `store.OpenPostgres`）。
- **仓库**：`internal/center/store/` 下一文件一 aggregate（`monitoring_instances.go`、`targets.go`、`incidents.go`、`sync_batches.go` 等）。
- **schema 演进**：冻结 R1 source prefix 仍为 `0001_*.sql` … `0051_create_record_platform_foundation.sql`（含两个按文件名字典序排列的 `0004_*`，共 52 个 SQL 文件）；current root 为 C80，止于 `0080_bound_agent_sync_replay.sql`，共 81 个 source / 29 个 post-R1 exact fragments。历史 C77/C78/C79 的 78/79/80 sources 与 26/27/28 fragments 原字节不变。`db/migrations/embed.go` 用 `embed.FS` 嵌入，状态记在 `schema_migrations`。两个 record flag 都关闭的 legacy 路径由 `migrate.Apply` 顺序应用；`records-on/delete-off` 必须先由显式 scoped migrator 收敛 exact current 或完整独立 golden 匹配的注册 predecessor，center/importer 启动只做 current read-only admission，不补跑迁移。0067 对旧业务数据仍明确拒绝自动转换/删除；0072 增加新鲜度投影，0073 增加并回填资料版本，0074 增加月末汇率标记，0075 增加设置所有权字段（empty ACL fragment）。历史 0076 仅增加 family-state view/SELECT，0077 增加 product 两表/七个 runtime grants，0078 只替换 family-state view，0079 只增加普通 cache expiry B-tree 索引。0080 的精确退休与有界重放合同见下文。
- **事务边界**：写多张表时使用 `pgx.Tx`，参考 `store/sync_batches.go:40-91` 的 `ApplyBatch`（一次同步批次串起 4-5 张表的写入与一次 plan 计算）。
- **不变量**：领域规则（MonitoringInstance/Target/Probe 语义、健康状态派生、回填观测不告警）必须落到 SQL + 仓库 + 服务层共同遵守，详见后文。

---

## Query Patterns

### 基本约定

- **统一通过 `internal/center/store/<aggregate>.go` 访问数据库**。HTTP handler、incident service、retention worker 等都不直接持有 `*pgxpool.Pool`，而是接受领域接口（`monitoringinstances.Repository`、`targets.Repository`、`syncing.Repository` 等）的具体实现。
- 仓库构造器固定签名 `NewPostgres<Aggregate>Repository(*pgxpool.Pool) *Postgres<Aggregate>Repository`（参考 `store/monitoring_instances.go:34-36`、`store/sync_batches.go:30-36`）。`settings` 与 `dashboard` 使用固定签名例外：`NewPostgresSettingsRepository(*pgxpool.Pool, centersettings.DeploymentDefaults)` 与 `NewPostgresDashboardRepository(*pgxpool.Pool, centersettings.DeploymentDefaults)` 接受不可变的部署默认值快照；不要加入可选参数或旧签名 alias。
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
- 当前 C80 registry 为 17 个固定 profile（P62/P64/P63/P66/P67/P68/P69/P70/P71/P72/P73/P74/P75/P76/P77/P78/P79）、98303 条注册 predecessor chains。原 profile ID、R1/R2 及历史 C79 49151-chain/C78 24575-chain/C77 12287-chain 等 oracle 不变；所有旧 successor suffix 追加 0080，P79（ID=17）仅执行 `[0080]`，P78 执行 `[0079,0080]`，P77 执行 `[0078,0079,0080]`。current admission 仅接受 C80，任何旧终点必须先经 scoped migrator。独立 C79 oracle 在修改 compiler/retirement registry 前冻结：migration body 5832 bytes，SHA-256 `3879119c952bd0a59dee938a0468ce848070985bd0b6bd55a7da26d40263c7db`；privilege body 35745 bytes，SHA-256 `3bda892781478202e876702943a66463e25345c205c9c125304b2b6ba1a1eb0c`。独立 C78 oracle 的 migration SHA-256 仍为 `af2497ecde095a8f33e847dd21fd2f863764c86ff1a141aa40fac6ae6cb34ad3`。不得用修改后 current compiler 动态截取的 oracle 代替独立前驱事实；详见 [APP 当前迁移合同](../contracts/platform/app-acl-current.md)。
- `ipQualityProductAppACLCurrentMigrationFragment()` 只注册 `ip_quality_intelligence` 与 `ip_quality_source_cache`；center_runtime 对前者仅 SELECT/INSERT/UPDATE，对后者仅 SELECT/INSERT/UPDATE/DELETE。0077 SQL 不包含角色名或 GRANT；scoped convergence 在同事务内应用 exact DCL、验证完整 catalog 并发布 successor。旧报告/来源/服务的新增 JSONB 列保持 NULL，不回填 product、不生成历史 job/cache，原表 ACL 不变。
- 历史 `ipQualityCollectionScopeAppACLCurrentMigrationFragment()`（0078）只替换 family-state view；`ipQualityCacheExpiryAppACLCurrentMigrationFragment()`（0079）只增加 `ip_quality_source_cache(expires_at)` 的普通、非 concurrent、非 partial B-tree 索引。二者自身无 managed object/function/ACL delta。升级到 C80 时 P77/P78 也必须执行 0080 的精确权限增删，不能把“历史迁移空 delta”误当“current successor 空 delta”。旧 manifest 历史字节及业务/settings/cache 数据保持不变。
- `AppACLCurrentMigrationFragment.RetiredObjects` 随 fragment/compiler clone 深拷贝；effective object/privilege/function inventory 从冻结 R1 按 migration 顺序物化。退休必须针对当时存在的对象，移除该对象及关联权限，拒绝不存在/重复退休、同 fragment 重新声明和退休后悬空权限。0080 只允许退休 `agent_sync_batches` 与 `agent_live_signals`，以无 `CASCADE` 的 `DROP TABLE` 移除，未知依赖必须失败回滚。新 catalog delta 恰为两个旧表移除、两个新表及两个 hardened invoker functions 增加，不能放宽其他角色绑定、权限移除/增加或 GRANT OPTION。迁移前验证前驱 catalog，同一个 scoped 事务执行 DDL、exact DCL、current catalog 验证及 manifest/head 发布，失败整体回滚。
- 0080 的 `agent_replay_states` 以 session 为主键、永久绑定 UUID producer；持久 `retired_through`、`allocated_through`、`live_sequence_high` 均为非负 signed bigint 且不可降低。runtime 仅 SELECT/INSERT/UPDATE，不能 DELETE。`agent_batch_receipts` 以 `(session_id,batch_sequence)` 为主键、存 32-byte SHA-256 digest，runtime 仅 SELECT/INSERT/DELETE，不能 UPDATE；不保存逐 live signal 行、接收时间索引或永久逐 ack 历史。两表开启 autovacuum，vacuum threshold=1000、scale factor=0.05；state fillfactor=80，不改全局 PostgreSQL 参数。
- `enforce_agent_replay_state()` 与 `enforce_agent_batch_receipt()` 均为 `search_path=pg_catalog` 的普通 invoker，使用全限定表名，PUBLIC 不获 EXECUTE。state BEFORE UPDATE 防止改 session/producer 或降低任何水位；AFTER UPDATE OF retired_through 在同事务清除本 session 已退休 receipts，清理只在触发器实现。receipt guard 对 INSERT/DELETE 以 `FOR SHARE` 读取 state，禁止越过持久 floor、allocated 或 131072 窗口，禁止删除未退休 receipt；不以 KEY SHARE 放过非键 floor 更新。违规 SQL 为 23514，直接 runtime SQL 不能擦除防重放证据。生产锁序固定 graph → instance/session → replay state → receipt。
- v2 batch 窗口锚定退休 floor，而非最高收到序号；同序号同 digest 为 exact duplicate，异 digest 或 producer 不符/退休重放 fail closed；超窗口请求只提交合法 floor/live，返回 deferred，不写 facts/receipt/命令。live 使用独立单调高水位，Center 的数据库接收时间仍是在线证据；较旧 live 不刷新在线，也不能挡住合法 backlog。每 session receipts 逻辑上至多 131072 行，通常随 durable ack/floor 推进仅保留少量在途记录；state 为一行/session。session 历史、业务历史仍增长，dead tuples/文件高水位依赖 autovacuum 重用，逻辑有界不等于数据库文件立即缩小。
- 0080 升级前必须逐台停止全部旧 Agent（含 evidence-only）、检查实际 buffer 为合法空数组；非空/离线/不可读均阻止切换，不能清队列。协调备份数据库、附件、Records authority、凭据及 queue，再升级 Center/全部 Agent。迁移不导入旧随机重放身份，也不自行宣称跨机排空完成。C80 后不支持旧协议 writer、单侧数据库/queue 快照回滚或自动重新编号恢复；恢复必须另行授权并在全部 writer 停止后恢复同一协调点。
- **迁移重复执行必须安全**：legacy 常用 `create table if not exists`、`create index if not exists`、`alter table ... add column if not exists`（见 `0001_initial_schema.sql`、`0009_add_observability_filter_indexes.sql`）。0079 使用上述 exact index DDL，不用 `IF NOT EXISTS` 掩盖同名定义漂移；其重复安全依赖 exact ledger/manifest/catalog 验证与整事务回滚，current exact repeat 不再执行 SQL/DCL/manifest 写入。不得以事务外 concurrent DDL、current-time partial index 或运维 SQL 替代注册迁移。
- **视图列结构变化必须先 drop 再 recreate**：PostgreSQL 的 `CREATE OR REPLACE VIEW` 不能删除列、重排列或在中间插入列；否则会出现类似 `cannot change name of view column "evidence_snapshot" to "followup_todo_count"` 的启动失败。迁移需要使用 `drop view if exists <view_name>;` 后再 `create or replace view ...`，并保证依赖对象可随迁移重建。

### 流程

1. 想清楚改动是否需要持久化（业务模型变化、查询需要新索引、retention 行为变化等）。
2. 在 `db/migrations/` 新建下一个未占用序号。冻结 R1 仍止于 0051，current 已到 `0080_bound_agent_sync_replay.sql`；若无并发新增，下一个候选是 `0081_<verb>_<scope>.sql`。post-R1 APP migration 必须同时注册 exact fragment、更新全部 exact successor suffix，并以独立冻结的 predecessor golden 明确支持升级；records-on 启动不补跑迁移。
3. 文件内只允许 `create / alter / drop / insert` 等 DDL/DML 语句，不要在里面写 Go。
4. 同时更新对应 `internal/center/store/<aggregate>.go` 的 `select` 列、`insert` / `update` 语句、读写函数签名。
5. 跑 `make verify-go`（含 `migrate` 包的单测，见 `migrate_test.go`）；接着按 `docs/operations/fresh-install-smoke-run.md` 在真 Postgres 上做 fresh-install smoke。
   C80 必须经 strict PostgreSQL wrapper 运行 current umbrella 与 `TestPostgresIntegrationAgentReplayMigration`：fresh C80、独立 C79 genesis/历史 chain、credentials/session/trusted-online/raw/aggregate/business/settings 完整行保持、exact ACL/真实 runtime 操作与拒绝、reject-before/admit-after、repeat、无关权限增删/假 retirement 拒绝，以及 DDL/DCL/manifest 失败回滚（旧表及其旧记录复原、新表不存在）。保留历史 product/collection-scope/cache-expiry 专项的真实前驱证据。`scripts/test-pg16-catalog.sh` 在 PG16 16.0/16.6/16.12 要求 R2、current umbrella 和 replay migration 三个顶级 anchor 全部 RUN/PASS，任意 skip/fail 均拒绝。`scripts/test-business-postgres.sh` 在保留既有 anchors 上增加 `TestPostgresIntegrationAgentReplayWindow`、`TestPostgresIntegrationRetentionConcurrentSync`、`TestPostgresIntegrationRetentionPreservesHistoryAndFinalizedBuckets`，不以源码字符串断言或被 skip 的测试替代真实事务/并发证据。

### 历史 / 审计字段同步

- 如果业务主表新增用户可见合同字段，且该字段会被历史、审计或决策表记录（如订阅价格历史、生命周期动作），同一个迁移必须同步补齐历史表列、backfill、约束与仓库 scan/insert 逻辑。不要只改源表导致后续审计丢失新字段。
- 兼容旧字段时，迁移需要给出可重复的推导规则和约束收口：例如订阅以 `billing_period_unit` + `billing_period_length` + `renewal_mode` 为新合同，同时从 `billing_months`、`billing_cycle`、`auto_renew`、`auto_renew_cancelled` 回填，并短期保留旧字段供下游兼容。
- 会同时更新业务事实和审计记录的动作必须在一个事务内完成。VPS 有效期更新应锁定目标 VPS，写生命周期 action / step，并更新 VPS 自身 `validity_mode/expires_at`；不要求存在订阅，不自动覆盖 subscription `renew_at`。账单续费和资源有效期分别显式确认。
- 历史迁移 fixture 必须在实际 predecessor schema 上用该版本的固定 SQL 列清单写入，不能调用已增加后续列的当前生产 writer，也不能为测试加入生产 backward shim。先独立执行被验证的迁移并逐列比较历史事实，再安装 current schema 验证现行 reader；新增列的 NULL/回填合同单独断言，不把合法 additive schema 差异当成旧事实变化。

### 不要做

- ❌ 修改已经合并/发布过的迁移文件内容（包括加空格）。要修就再写一个新迁移。
- 例外：如果某个已发布迁移在记录到 `schema_migrations` 前必然失败，导致后续修复迁移无法执行，可以修正该失败迁移本身；修复必须保持幂等，并增加回归测试说明原因。涉及约束收口时，测试必须断言 backfill 在约束前执行。
- ❌ 用任何运维脚本 / SQL 客户端直接改线上 schema，必须走迁移文件。
- ❌ 把测试数据 / seed 数据写进迁移文件——种子用户由 `internal/center/auth/seed.go` 在 bootstrap 阶段执行（`bootstrap.go:104-107`）。

> ⚠️ **已知 gap**：存在两个历史 `0004_*`（`0004_add_node_onboarding_binding_state.sql`、`0004_add_observation_provenance.sql`），仍按文件名字典序作为冻结 R1 的两个独立 checksum source。新增迁移必须先检查目录并使用最大已占编号之后的下一个空号；current root 为 0080，无并发迁移时下一个候选为 `0081_*`，不得覆盖他人的迁移。

## Naming Conventions

### VPS 与监控生命周期持久化

- `vps_assets.lifecycle_status` 仅允许 `active/archived`，`renewal_decision` 仅允许 `unreviewed/keep/cancel`。自定义用途写入 `usage_tags[]`，与普通 `labels[]` 分离；旧 `usage_status` 暂保留用于内部 SQL，不能成为公开状态权威。
- 有效期由 `validity_mode=fixed/unlimited/unknown` 与 `expires_at` 共同约束；仅 fixed 必须有日期，其余必须为空。服务商核对事实 `auto_renew_check/auto_renew_checked_at` 与续费意向、订阅账期独立。
- `monitoring_instances.vps_id` 为必填外键，数据库触发器拒绝转移所有权；旧关联表通过复合外键禁止写入与实例所有权不符的 VPS。当前实例通过 `lifecycle_status <> '已退役'` 的部分唯一索引保证每 VPS 最多一个；阶段 `monitoring_agent_sessions` 保存独立凭据、指纹摘要、权限与持续在线事实，不能随原始观测清理。
- C80 不再保存 `agent_live_signals(session_id,signal_id)` 或 `agent_sync_batches` 永久逐次身份；重放由 session/producer、持久退休 floor、有界 receipts 和独立 live sequence high 共同约束。可信在线时间由 Center 接收时间产生。`receiver_health` 只维护当前进程连续健康窗口，空表不代表健康。
- `asset_service_associations/asset_domain_associations` 保存关联起止、承载信息及结束快照；对象身份独立于关联，未结束关联由部分唯一索引限制同对象同 VPS 重复创建。
- `vps_followups` 保留解决或忽略原因、操作者和时间；未解决项按 `(vps_id,kind,dedupe_key)` 去重。`vps_archive_requests`、`monitoring_instance_lifecycle_receipts` 为不可更新的幂等结果凭据。
- 新表运行权限由 0067 current fragment 明确注册；仅接入阶段、健康窗口、跟进项与关联记录允许 UPDATE。冻结 R1 和已发布 SQL 不变。
- 日聚合 `finalized` 在原始数据清理时冻结 UTC 日桶，避免后续残缺原始数据覆盖完整聚合。默认原始保留 30 天，日聚合 365 天；事件、通知和低频 IP 报告不按天自动删除。

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
