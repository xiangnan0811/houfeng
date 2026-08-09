# 数据库规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风当前后端持久化栈是 **PostgreSQL + pgx/v5**，没有 ORM、没有 query builder。所有 SQL 都是手写原生语句，迁移文件 (`db/migrations/*.sql`) 是 schema 的**唯一权威源**——不允许通过 ORM auto-migrate、SQL 控制台或运维脚本绕过迁移修改 schema。

核心约定一句话总结：
- **driver**：`github.com/jackc/pgx/v5` 与 `github.com/jackc/pgx/v5/pgxpool`，连接池在 `cmd/houfeng-center/bootstrap.go` 内构造（参见 `bootstrap.go:60-69`，调用 `store.OpenPostgres`）。
- **仓库**：`internal/center/store/` 下一文件一 aggregate（`monitoring_instances.go`、`targets.go`、`incidents.go`、`sync_batches.go` 等）。
- **schema 演进**：冻结 R1 source prefix 仍为 `0001_*.sql` … `0051_create_record_platform_foundation.sql`（含两个按文件名字典序排列的 `0004_*`，共 52 个 SQL 文件）；current development root 再包含 `0052_create_records_core.sql`，共 53 个 source。`db/migrations/embed.go` 用 `embed.FS` 嵌入，状态记在 `schema_migrations` 表。两个 record flag 都关闭时，旧 center/importer 启动路径仍由 `internal/center/store/migrate/migrate.go` 的 `Apply` 顺序应用；`records-on/delete-off` 必须先由显式 scoped migrator 收敛当前 build 的 exact embedded set，center/importer 只做 current runtime admission，绝不在启动时调用 `Apply`。
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
2. 在 `db/migrations/` 新建下一个未占用序号的文件。冻结 r1 固定清单末尾是 `0051_create_record_platform_foundation.sql`，current development root 已到 `0052_create_records_core.sql`，因此若没有并发新增文件，下一个候选是 `0053_<verb>_<scope>.sql`；任何 r1 之后的 APP migration 还必须在同一 PR 注册 exact current fragment，不能由 records-on 启动路径补跑。
3. 文件内只允许 `create / alter / drop / insert` 等 DDL/DML 语句，不要在里面写 Go。
4. 同时更新对应 `internal/center/store/<aggregate>.go` 的 `select` 列、`insert` / `update` 语句、读写函数签名。
5. 跑 `make verify-go`（含 `migrate` 包的单测，见 `migrate_test.go`）；接着按 `docs/operations/fresh-install-smoke-run.md` 在真 Postgres 上做 fresh-install smoke。

### 历史 / 审计字段同步

- 如果业务主表新增用户可见合同字段，且该字段会被历史、审计或决策表记录（如订阅价格历史、生命周期动作），同一个迁移必须同步补齐历史表列、backfill、约束与仓库 scan/insert 逻辑。不要只改源表导致后续审计丢失新字段。
- 兼容旧字段时，迁移需要给出可重复的推导规则和约束收口：例如订阅以 `billing_period_unit` + `billing_period_length` + `renewal_mode` 为新合同，同时从 `billing_months`、`billing_cycle`、`auto_renew`、`auto_renew_cancelled` 回填，并短期保留旧字段供下游兼容。
- 会同时更新业务事实和审计记录的动作必须在一个事务内完成。VPS 有效期延长这类操作应锁定目标 VPS，确认唯一 active subscription，写生命周期 action / step，更新 subscription `renew_at`，并在必要时写 price history。

### VPS 资产状态组合不变量

- `vps_assets.lifecycle_status`、`usage_status`、`renewal_decision` 是一个组合状态，不是三个互不相关的枚举。所有写路径必须验证最终组合：`cancelled` 必须使用取消类续费决策且不能 `in_use`；`to_cancel` 必须使用取消类续费决策；`to_migrate` 必须使用 `migrate`；`replaced` 不能仍是 `active` 或 `in_use`。
- PATCH 入口不能只校验请求体内出现的字段。仓库写入边界必须读取当前行，应用 patch preview 后调用 `vpsassets.ValidateVPSStateCombination`，再执行 `update vps_assets`；受控生命周期 action 若直接调用底层 update helper，也必须先做同样的合成状态校验。
- DB 必须有跨列 check constraint 作为最后兜底。新增或调整这类约束是破坏性数据完整性收口：迁移必须先用幂等 backfill 处理可确定归一化的历史组合，再添加 validated constraint；无法安全推导的脏数据才应 fail fast。不得用 `not valid` 静默放过。
- 如果某个已发布迁移在记录到 `schema_migrations` 前已经会因历史数据违反新约束而失败，可以按例外修正该失败迁移本身；修正必须把 backfill 放在 `add constraint` 前，并增加 `migrate_test.go` 断言 backfill 语句存在且顺序早于约束。
- JSON 导入的 `subscription` 对象必须同步订阅创建合同。`subscription.renewal_mode` 是合法字段，支持 `auto|manual|auto_cancelled|lottery|gift|bonus|other`；`gift` 和 `lottery` 归一后 legacy `auto_renew` / `auto_renew_cancelled` 必须为 `false,false`。`DecodeRecords` 继续 `DisallowUnknownFields`，新增可导入字段时必须同时改 DTO、dry-run report、create input 传递和测试。

### 不要做

- ❌ 修改已经合并/发布过的迁移文件内容（包括加空格）。要修就再写一个新迁移。
- 例外：如果某个已发布迁移在记录到 `schema_migrations` 前必然失败，导致后续修复迁移无法执行，可以修正该失败迁移本身；修复必须保持幂等，并增加回归测试说明原因。涉及约束收口时，测试必须断言 backfill 在约束前执行。
- ❌ 用任何运维脚本 / SQL 客户端直接改线上 schema，必须走迁移文件。
- ❌ 把测试数据 / seed 数据写进迁移文件——种子用户由 `internal/center/auth/seed.go` 在 bootstrap 阶段执行（`bootstrap.go:104-107`）。

> ⚠️ **已知 gap**：当前 `db/migrations/` 里存在两个 `0004_*` 文件 (`0004_add_node_onboarding_binding_state.sql`、`0004_add_observation_provenance.sql`)。前者是历史 Node 命名迁移，当前 schema 由 `0029_rename_nodes_to_monitoring_instances.sql` 迁到 MonitoringInstance 语义。legacy `migrate.Apply` 按文件名字典序排序，scoped r1 migrator 也把它们作为固定 52-source 清单中的两个独立 checksum source；二者顺序均由后缀决定，并不冲突。序号撞车仍违反“序号唯一”的隐含约定，新增迁移时**必须先查看 `db/migrations/`，再使用当前最大编号之后的下一个未占用序号**（current development root 已到 `0052_create_records_core.sql`，若没有并发新增文件，下一个候选为 `0053_*`）。

### Scenario: Records core `0052` schema and exact APP ACL fragment

#### 1. Scope / Trigger

- 触发：修改 `0052_create_records_core.sql`、`internal/center/records/`、Records store transaction、current APP ACL fragment，或任何 Records core purge/readiness 路径时。
- 项目当前只支持 fresh/current development database 与 exact repeat；不为 `experience_logs`、旧 `0052`、混合版本或部分升级建设 backfill/upgrader。`0052` 合并后按普通迁移不可修改，后续 schema 变化使用 `0053+`。

#### 2. Signatures

- Root migration：`db/migrations/0052_create_records_core.sql`。
- Current fragment：`recordsCoreAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment`，migration name 必须精确为 `0052_create_records_core.sql`。
- Deferred validator：`record_platform_internal.validate_record_revision_primary_subject() returns trigger`，`SECURITY INVOKER`、`search_path=pg_catalog`、显式 revoke `PUBLIC`。
- 九张 owned table：`records`、`record_revisions`、`record_revision_subjects`、`record_revision_tags`、`record_revision_participants`、`record_drafts`、`record_draft_checkpoints`、`record_domain_activities`、`record_core_purge_receipts`。

#### 3. Contracts

- `records` 是稳定 root/current projection；`record_revisions` 与 subjects/tags/participants 是只插入的完整历史。来源删除不能 cascade Records；所有 record-owned 清理由 core purge adapter 在一个事务中显式执行。
- `(record_id,current_revision_id)` 使用 initially-deferred same-record FK。revision subject 使用 partial unique index 保证至多一个 primary，并由两个 initially-deferred constraint trigger 覆盖 revision insert 与 subject insert/delete，在 commit 时保证每个仍存在的 revision 恰有一个 primary。受控 purge 同事务删除 subject 与 revision 时 validator 看到 revision 已消失并允许提交。
- immutable history tables没有 APP `UPDATE` grant，并复用 `reject_immutable_mutation()` 拒绝 owner/migrator update；delete 仅用于受控显式 purge。
- current fragment 精确登记九张 table 加 primary-subject validator function。`center_runtime` 只取得 Records 在线读写/显式 purge 所需 table privilege；`platform_admin` 只能读取无内容的 `record_core_purge_receipts`，不能读取 Records content table；validator 没有额外 direct APP EXECUTE tuple。
- `record_core_purge_receipts` 的 schema、insert 参数和 receipt digest 都只能保存 operation-scoped proof：不得包含或散列 `project_id`、`record_id`、revision ID 或业务内容。需要在 preview 中关联对象时，经 `record_purge_operations -> deletion_reservations` 读取当前 operation binding，不能把对象身份反规范化回 receipt。
- `record_draft_checkpoints` 是唯一恢复点名称；revision participant 只存在于独立 `record_revision_participants`，不得出现 `participant_ids` 或 `record_draft_recovery_points`。
- production current/historical authorization snapshot loader 必须在 admitted pgx transaction 中先执行 record read fence，再读取 root、visibility、identity snapshot、capture authorization 或 subject rows；直接 DB loader 只允许作为注入式单元测试 seam，不能由 production constructor 绑定。
- record candidate list 必须在同一条 SQL 中以 correlated `not exists` 排除 `fenced|committed` deletion reservation，并在过滤之后应用 `order by/limit`；后续 authorization snapshot 与 revision content read 仍各自 recheck fence，以关闭查询之间的 reservation race。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| revision transaction 在 commit 时没有 primary subject | deferred validator 返回 SQLSTATE `23514`；revision/relations 整体回滚。 |
| 同 revision 插入第二个 primary | partial unique index 返回 SQLSTATE `23505`。 |
| root 指向另一 record 的 revision | same-record FK 在约束检查时返回 SQLSTATE `23503`。 |
| 单独删除唯一 primary 而保留 revision | commit 返回 SQLSTATE `23514`；不得留下无 primary 历史。 |
| 同事务显式删除 subject、revision 与 root | validator 跳过已删除 revision，事务可以提交。 |
| runtime 尝试 UPDATE immutable revision | ACL 先返回 SQLSTATE `42501`；owner/migrator 直接 update 由 immutable trigger 返回 `55000`。 |
| `0052` 缺 fragment、function hardening 或任一 managed object/privilege | current source/catalog compile 在 transaction 前 fail closed。 |
| authorization admission 不可用或 record 已 reserved | admission 前 0 DB read；reserved 时只运行 fence read，0 root/subject/live resolver read。 |
| candidate 对应 `fenced|committed` reservation | SQL 返回 0 candidate row；record ID 不进入 application scan，也不能成为外部 cursor。 |

#### 5. Good / Base / Bad Cases

- Good：同一 admitted transaction 先插 revision，再插恰好一个 primary 和任意 related subject，commit 时统一验证。
- Base：fresh apply 后 exact repeat 不改变 migration ledger、manifest、owner、ACL、function 或 trigger state。
- Bad：只建 `where is_primary` partial unique index并声称“恰好一个”；它只能拒绝第二个 primary，完全没有 subject 的 revision 仍可提交。
- Bad：为了绕开 deferred check 把 subject/revision/root 分成多个 purge transaction；第一笔 subject delete 必须失败，而不是制造暂时不合法状态。

#### 6. Tests Required

```bash
go test ./internal/center/store/migrate -run 'RecordsCore|AppACLCurrent' -count=1
go test -race ./internal/center/records -run 'Revision|Lifecycle|Status|Template|Canonical|Subject|Authorization|Tombstone' -count=10
scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store/migrate \
  -run '^(TestPostgresIntegrationRecordsCoreSchema|TestPostgresIntegrationAppACLCurrent)$' -count=1
```

- PostgreSQL test 必须覆盖 fresh/exact repeat、无 primary 的 commit-time `23514`、第二 primary `23505`、same-record FK、immutable update、单事务显式 purge、receipt 的 exact content-free column set，以及 runtime/admin exact privilege；不得以 `SKIP` 作为证据。

#### 7. Wrong vs Correct

```sql
-- 错误：只能保证至多一个 primary。
create unique index uq_record_revision_subjects_primary
  on record_revision_subjects(revision_id) where is_primary;

-- 正确：保留 partial unique，并对 revision insert 和 subject insert/delete
-- 注册 initially-deferred constraint trigger，在 transaction commit 检查恰好一个。
create constraint trigger record_revisions_require_primary_subject
after insert on public.record_revisions
deferrable initially deferred
for each row execute function
  record_platform_internal.validate_record_revision_primary_subject();
```

### Scenario: Records private drafts, bounded checkpoints, and atomic publish cleanup

#### 1. Scope / Trigger

- Trigger: 修改 `internal/center/records/drafts.go`、`internal/center/store/record_drafts.go`、revision command 的 draft publication 字段、`record_drafts` / `record_draft_checkpoints` SQL，或 draft expiry cleanup 时。
- 该场景覆盖作者私有 server draft、精确 ETag PATCH、bounded checkpoint、显式 discard/revoke，以及与正式 revision transaction 同生共死的 publish cleanup；浏览器 buffer、Records HTTP DTO 与永久删除 core purge 由各自 owner 继续闭合。

#### 2. Signatures

```go
type DraftRepository interface {
	GetDraft(context.Context, string, string) (Draft, error)
	CreateDraft(context.Context, DraftCreateCommand) (Draft, error)
	PatchDraft(context.Context, DraftPatchCommand) (Draft, error)
	DeleteDraft(context.Context, DraftDeleteCommand) error
}

func (r *PostgresRecordDraftRepository) ClaimExpiredDrafts(
	context.Context,
	uint64,
) ([]string, error)

type RevisionCommitCommand struct {
	// Existing revision fields omitted.
	DraftID   string
	DraftETag DraftETag
}
```

- `DraftID` / `DraftETag` 是 optional pair：两者同时为空表示非 draft 正式保存；两者同时有效表示 publish。
- `ClaimExpiredDrafts` 的 `limit` 闭合为 `1..100`，返回同一事务实际删除的 draft IDs。

#### 3. Contracts

- draft payload 是 immutable canonical JSON object；payload hash 与 ETag 必须从 persisted payload、draft ID、author、version 重新计算验证，不能信任数据库中的摘要列或客户端项目/作者字段。
- `GetDraft`、list、PATCH 与作者操作的 cleanup 使用 author-scoped routing SQL；该 SQL 必须在返回 metadata row 前以 correlated `not exists` 排除 existing-record draft 的 `fenced|committed` reservation，并在过滤之后应用 list `limit`。错误作者与已 reserved 的 existing-record draft 在 payload read 前得到 `ErrDraftNotFound`；`record_id is null` 的 new-record draft 保持可见。
- routing SQL 的原子 reservation filter 不能替代 race recheck。PATCH 在一个 admitted pgx transaction 中按 `atomic routing -> optional mutation-fence recheck -> author row FOR UPDATE -> exact ETag -> update -> checkpoint -> expiry prune -> newest-20 prune` 执行；Get/list 使用 read-fence recheck。相同 canonical payload 只续 `updated_at/warning_at/expires_at`，不增加 version、发行 checkpoint ID 或写 checkpoint。
- 内容变化时每个 `date_bin(..., 5 minutes, fixed origin)` bucket 最多一个 immutable checkpoint；保留最新 20 个并删除 `checkpoint_expires_at <= transaction_timestamp()` 的行。draft inactivity TTL 为 90 天，warning boundary 为 expiry 前 7 天；所有时间以 database transaction time 为准。
- discard/revoke 与 publish cleanup 都先删除 checkpoints 再删除 draft。publish 必须在现有 revision transaction 内锁定作者 draft，校验 exact ETag 及 create/new-draft 或 update/same-record-and-base shape，在 formal revision/no-change 成功后、idempotency complete 前 cleanup。任一 conflict 或 cleanup error 回滚正式事实并保留 draft。
- completed idempotency replay 在 draft validation/cleanup 之前返回 persisted revision result；首次 publish 已删除 draft 后，同 key/same fingerprint replay 仍必须成功。request fingerprint 绑定 `DraftID` 与强 ETag，换 draft 或换 version 不能复用同 key。
- 普通 draft create/read/PATCH/discard/revoke/expiry cleanup 不写 `record_domain_activities`、`record_outbox`、search 或 notification；只有 publish 成功产生正式 revision 既有的 activity/outbox。
- `ClaimExpiredDrafts` 必须在 claim SQL 中、`order by/limit` 之前以 correlated `not exists` 排除 existing-record draft 的 `fenced|committed` reservation，同时返回 nullable `record_id`。claim 完成后、任何 checkpoint/draft delete 之前，对去重后的每个非空 record ID 再执行 mutation-fence recheck；并发 reservation 命中时整批 rollback。`record_id is null` 的 new-record draft 不受对象 reservation 过滤影响。
- duration 以 Go `time.Duration.Microseconds()` 作为 `bigint` 传入。一个 bind 参数乘 interval 可沿用现有 SQL；两个参数先做减法时必须显式 cast 两侧为 `bigint`，否则 PostgreSQL parse 会对 `unknown - unknown` 返回 `42725`。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| draft ID、author、payload、policy 或 optional publish pair 非法 | 对应 `ErrInvalidDraftCommand` / `ErrInvalidRevisionCommand`；SQL 前拒绝。 |
| lookup author 不匹配 | `ErrDraftNotFound`；只执行 author-scoped routing lookup，0 payload read/0 draft write。 |
| existing-record draft 在 routing query 前已有 `fenced|committed` reservation | correlated filter 返回 0 row / `ErrDraftNotFound`；0 routing metadata row、0 payload read、0 draft write。 |
| reservation 在 routing row 返回后并发建立 | transaction 内 read/mutation fence recheck 返回 `ErrRecordDeletionReserved`；0 payload read、0 draft write。 |
| PATCH / publish ETag 已推进 | `ErrDraftConflict`；PATCH typed error携带 current server draft 与 local payload；0 draft/checkpoint write。 |
| existing draft base/current lifecycle 已推进 | create/prepare/publish 返回 `ErrDraftRevisionConflict`；draft 保留。 |
| PATCH payload 未变化 | version/ETag/payload 不变，只刷新 90-day TTL 与 7-day warning；0 checkpoint。 |
| checkpoint ID、insert、retention prune 或 publish cleanup 任一步失败 | 整个 transaction rollback；不得留下半份 draft 或半份 formal revision。 |
| expiry cleanup limit 为 0 或大于 100 | `ErrInvalidDraftCommand`；不开始 transaction。 |
| existing-record draft 在 expiry claim 前已有 `fenced|committed` reservation | claim SQL 返回 0 row；expired draft/checkpoint 保留，且不占 batch limit。 |
| reservation 在 expiry row claim 后并发建立 | mutation-fence recheck 返回 `ErrRecordDeletionReserved`；整批 0 checkpoint/draft delete。 |
| 两个 cleanup worker 同时运行 | `FOR UPDATE SKIP LOCKED` 使 claimed ID 集合不相交；每个 batch 原子删除 checkpoints/drafts。 |

#### 5. Good / Base / Bad Cases

- Good：两个客户端持有相同 ETag；先取得 row lock 的请求推进到 v2 并写一个 bucket checkpoint，后取得锁的请求读到 v2 typed conflict 且不覆盖。
- Good：publish 创建 revision/activity/outbox 后在同一 transaction 删除 checkpoint/draft并完成 idempotency；同 key retry 不再要求 draft 存在。
- Good：expiry worker 的首条 SQL 跳过已经 reserved 的 existing-record draft；claim 后出现 reservation 时，二次 mutation fence 在 delete 前中止并回滚整批。
- Base：autosave 内容与 server canonical payload 相同，仅刷新 inactivity TTL，避免 version 与 recovery history 噪音。
- Bad：formal revision 先 commit，再调用独立 `DeleteDraft`；cleanup failure 会留下“已发布但仍可编辑”的 server draft，retry 也无法证明单一结果。
- Bad：用 `limit` 但没有 `SKIP LOCKED` 或跨 transaction claim/delete；并发 worker 会阻塞、重复 claim 或留下部分 cleanup。
- Bad：expiry cleanup 只按 `expires_at` claim 后直接 delete；它会绕过 permanent-delete reservation，或者在 claim 与 delete 之间吞掉刚被 fenced 的 draft。

#### 6. Tests Required

```bash
go test -race ./internal/center/records ./internal/center/store \
  -run 'Draft|Checkpoint' -count=10

scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store \
  -run '^TestPostgresIntegrationRecordDraft' -count=1
```

- Unit/race 必须覆盖 immutable payload/ETag、作者隔离、two-client conflict、no-change TTL、checkpoint SQL、discard/revoke、expired batch grammar、publish create/update/no-change/conflict/rollback/replay。
- 真实 PostgreSQL 必须覆盖并发 PATCH 单赢家、五分钟 bucket/newest 20/seven-day retention、并发 cleanup claim 不相交、`fenced|committed` 过期 existing-record draft/checkpoint 均保留、publish/discard/revoke cleanup，以及普通 draft 操作的 activity/outbox 零行；runner 不接受 `SKIP`。

#### 7. Wrong vs Correct

```sql
-- 错误：两个 bind 参数都是 unknown，PostgreSQL parse 返回 42725。
warning_at = transaction_timestamp()
  + (($9 - $10) * interval '1 microsecond')

-- 正确：明确声明 duration microseconds 的 bigint 运算域。
warning_at = transaction_timestamp()
  + (($9::bigint - $10::bigint) * interval '1 microsecond')
```

```go
// 错误：formal commit 与 draft cleanup 分属两个 transaction。
result, err := revisions.CommitRevision(ctx, command)
if err == nil {
	err = drafts.DeleteDraft(ctx, deleteCommand)
}

// 正确：revision store 在 caller-owned admitted pgx.Tx 内完成 formal writes、
// draft checkpoint/draft cleanup 和 idempotency complete，再统一 commit/rollback。
result, err := revisions.CommitRevision(ctx, commandWithDraftIDAndExactETag)
```

```sql
-- 错误：过期即 claim，随后没有对象 mutation-fence recheck。
select draft_id from public.record_drafts
where expires_at <= transaction_timestamp()
for update skip locked limit $1;

-- 正确：claim SQL 先排除 fenced|committed reservation；scan 完 record_id 后，
-- application 在任何 delete 前对去重的非空 record ID 调用 assertRecordMutationFence。
```

### Scenario: APP current-development scoped migrator and one-snapshot runtime admission

#### 1. Scope / Trigger

- 触发：修改 `HOUFENG_RECORDS_ENABLED` / `HOUFENG_RECORD_PERMANENT_DELETE_ENABLED` 模式选择、`houfeng-record-platform-admin migrate --scope app`、root migration、current APP fragment/compiler、manifest/catalog verifier、`ConvergeAppACLCurrent`、`AdmitAppACLCurrentRuntime`、center bootstrap、VPS importer，或其 PostgreSQL regression 时。
- current contract 的前 52 个 source 必须 byte-for-byte 等于冻结 `0001…0051` r1 inventory（包含两个按文件名字典序排列的 `0004_*`）。每个后来 embedded migration 必须在同一个 PR 注册一个 exact `AppACLCurrentMigrationFragment`；无 APP object 也必须注册 explicit empty fragment。当前 root set 有 53 个 source并止于 `0052_create_records_core.sql`，production registry 恰有一个 `0052` fragment。
- 两个 record flag 都关闭时保留 legacy owner `migrate.Apply`。`records-on/delete-off` 必须先运行 current scoped migrator，随后 center/importer 只能以 runtime 身份执行 current admission。`false/true` 和 `true/true` 在读取 URL、`_FILE` secret、DNS、数据库、输入文件或外部域配置前失败。
- `ConvergeAppACLR1`、`AdmitAppACLRuntime` 与 isolated APP R2 bootstrap/finalize/runtime API 是冻结历史合同；保留其导出签名和 regression，但 product migration/startup 不再默认调用它们。
- frozen `AdmitAppACLRuntime` 必须在开启 transaction 前通过 `snapshotAppACLR1MigrationSources(migrations.FS)` 取得并验证 exact R1 prefix，再把已 canonicalize 的 frozen set 交给 manifest verifier。它不得把 R1 manifest/ledger 与会随 `0052+` 增长的完整 `CanonicalMigrationSetFromFS(migrations.FS)` 比较；后者会让新增 current migration 反向破坏冻结 R1 admission。

#### 2. Signatures

- 模式入口：`config.LoadRecordPlatformMode() (config.RecordPlatformMode, error)`，只返回 `RecordPlatformModeLegacy` 或 `RecordPlatformModeRuntimeAdmission`。
- Writer：`ConvergeAppACLCurrent(ctx context.Context, db *pgxpool.Pool, runtimeRole, adminRole string) (AppACLManifestPersistedV1, error)`；`houfeng-record-platform-admin migrate --scope app` 是 records-on 的唯一 APP schema/ACL writer。
- Runtime gate：`AdmitAppACLCurrentRuntime(ctx context.Context, db *pgxpool.Pool) error`；center/importer 在构造任何 repository 前调用。
- Extension contract：`AppACLCurrentMigrationFragment{Migration, Objects, Privileges, Functions}`；fragment registry 与 `migrations.FS` 在 transaction 前 closed-world compile，later migration 与 fragment 必须一一对应。
- Typed cause：`migrate.ErrDevelopmentDatabaseRebuildRequired`。admin CLI 只允许该 safe sentinel 穿过 redaction boundary；任意数据库 error、raw SQL、role password 或 DSN 仍统一屏蔽。
- 持久化合同：`AppACLManifestPersistedV1.MigratorCatalogRole` 不可变且 digest-bound；runtime 从 persisted binding 构造 current catalog verifier，不读取 migrator credential/configuration。

#### 3. Contracts

- `center_runtime`、`platform_admin` 与 migrator 是三个预创建、两两不同、直接认证的 `LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS` role。三者的直接与递归 membership 均为空；migration 与 runtime admission 都证明 `session_user == current_user`。`SET ROLE`、复用 owner、role membership、default ACL 或共享 login 都不能满足该合同。
- source/fragment compiler 必须在 `BeginTx` 前拒绝 missing/extra/duplicate fragment、duplicate object/privilege、unknown subject、unmanaged privilege/function hardening，以及新 function 缺少 exact hardening。每个 fragment 的 `Privileges(databaseName)` callback 只在 source compile 时用固定验证数据库占位符求值一次；结果、fragment input 和 nested function config 都必须 defensive-copy。后续 catalog compile 只能复制已物化 privilege template，并替换 `database` tuple 的占位符，不能再次调用 callback。
- current convergence 只支持 fresh 与 exact-current。fresh 在一个 `SERIALIZABLE` transaction 中取得 advisory lock、固定 search path、apply exact source、revoke-first DCL、catalog verify 并插入一个 genesis manifest；exact repeat 只验证，不改变 ledger、manifest/head、owner、ACL 或 function state。null-head adoption、old source upgrade、repair 和 successor append 全部禁止；冻结 R1 wrapper 单独保留其历史 null-head adoption。
- current catalog 以冻结 r1 base（当前为 **204** ACL tuple）加 ordered fragment object/privilege/function hardening 编译。`public.record_platform_cas_contract_activation_projection(bytea)` 与 `public.record_platform_cas_domain_rotation_projection(bytea)` 仍是 migrator-owned、`SECURITY DEFINER`、唯一 `bytea` overload、`search_path=pg_catalog` 且显式 revoke `PUBLIC`。
- production `0052` fragment 增加九张 Records core table、一个 `record_platform_internal.validate_record_revision_primary_subject()` hardened function 与 29 个精确 APP privilege tuple；current expected-function catalog 是冻结两个 projector 加该 validator。不得给 platform admin Records content table读取权，也不得给 immutable history table `UPDATE`。
- admission 只验证 compiled migration-owned surface：database、managed schema、relation/view/sequence/function、ledger/manifest、role attributes/membership、owner、direct/effective/column/default ACL 和 function hardening。current convergence 的 placement、fresh-state 与 legacy-ledger companion-object preflight 均以完整 `(schema, object identity)` tuple 检查 relation/function；不同 managed schema 可声明同名对象，无关 schema 中的同名 relation、同名 function 或其他 overload 也不属于 managed tuple。冻结 R1 的历史裸名称 shadow rejection 保持不变。managed private schema 内 unknown object 仍是 drift；无关 schema/object 与 unrelated-owner default ACL 必须接受。
- PostgreSQL 16 `pgcrypto` 必须安装在 `record_platform_internal`；若 extension 已在其他 schema 则 fail closed。extension-member procedure 按 OID 识别，并对普通 managed owner/direct/effective/function reader 保持 opaque，因为受限 migrator 不能可靠改写 bootstrap-owned member ACL。opacity 绝不产生 reachability：`PUBLIC`、runtime、admin 对 `record_platform_internal` 都没有 `USAGE` 或 `CREATE`；同一 admission snapshot 还会拒绝同时具有 schema `USAGE` 与 function `EXECUTE` 的 reachable opaque member。migrator-owned helper/projector 仍必须显式 revoke `PUBLIC`。
- `AdmitAppACLCurrentRuntime` 精确开启一个 `REPEATABLE READ READ ONLY` transaction。在同一 snapshot 中交叉校验 direct identity、manifest/head、exact applied source、current privileges 与 compiled catalog。它不执行 DDL/DCL、不调用 writer；失败时 center/importer 关闭 pool，不得回退到 owner migration 或 warning-only dry-run。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| 两个 flag 都为 false | 选择 legacy path；现有 owner `migrate.Apply` 行为继续允许。 |
| `false/true` 或 `true/true` flag | 在读取 URL/secret/file/network/external-domain 前 fail；不连接 database，也不执行 migration。 |
| records-on/delete-off | admin 默认调用 `ConvergeAppACLCurrent`；center/importer 默认调用 `AdmitAppACLCurrentRuntime`；禁止 product fallback 到 frozen R1、R2 或 `migrate.Apply`。 |
| embedded root set 追加 `0052+`，数据库仍为 exact R1 manifest/ledger | frozen `AdmitAppACLRuntime` 只验证已固定的 `0001...0051` prefix 并成功；新增 current source 不得改变 R1 admission 结果。 |
| frozen R1 prefix 缺失、顺序变化或 SQL bytes 漂移 | `AdmitAppACLRuntime` 在 `BeginTx` 前 fail closed；不得退化为完整 embedded set 或跳过 checksum contract。 |
| embedded post-`0051` migration 缺 fragment，或 fragment extra/duplicate/invalid | transaction 前拒绝；`BeginTx` 调用次数为 0。 |
| fragment privilege callback 有状态，或 callback 返回的 captured slice 在 source compile 后被修改 | callback 调用次数固定为 1；catalog/manifest 使用 source compile 时深拷贝的 template，后续状态不能改变合同。 |
| fresh：无 ledger/manifest/managed object | apply exact current set、DCL/catalog verify、一个 genesis，全 transaction atomic。 |
| exact current：source/manifest/catalog 全匹配 | migrate 与 runtime 都成功；repeat 前后 durable snapshot 深相等。 |
| applied/manifest source 数量、filename 或 raw-byte checksum 不同 | `errors.Is(err, ErrDevelopmentDatabaseRebuildRequired)`；catalog read 与所有 durable write 为 0。 |
| nullable historical head 或有效 successor revision | rebuild-required；不得 adopt、append、repair 或读取 catalog。 |
| malformed manifest chain、exact-source catalog/owner/ACL/function drift | 返回具体 fail-closed corruption/catalog error；不得误标为 rebuild-required。 |
| 任一 role 不是不同的直接 constrained `LOGIN NOINHERIT` role，具有 direct/recursive membership，或 `session_user != current_user` | 在 scoped migration/admission 前 fail closed。`SET ROLE` runtime snapshot 精确拒绝为 `session user %q does not match current user %q`。 |
| current compiler output 与 persisted privileges 不同，或 runtime/admin 取得未编译 privilege | catalog/manifest drift，拒绝。runtime/admin 对 base projector 的 direct call 返回 SQLSTATE `42501`。 |
| managed object/grant/column ACL/default ACL/owner 或 projector definition drift | 拒绝；projector 必须 owner-only、唯一 `bytea`、`SECURITY DEFINER`、显式 revoke `PUBLIC`，并使用 `search_path=pg_catalog`。 |
| 不同 compiled managed schema 声明同名 relation/function tuple | 独立编译并按 exact tuple 检查；fresh/legacy preflight 不得因裸名称冲突拒绝。 |
| unrelated schema 中存在 managed relation/function 的同名对象或其他 overload，或存在 unrelated-owner default ACL | 只要完整 tuple 不能影响 compiled managed surface 就接受；冻结 R1 regression 继续保留历史 shadow rejection。 |
| `pgcrypto` 位于 `record_platform_internal` 之外 | fail closed（migration/convergence precondition 是 SQLSTATE `55000`）。 |
| runtime/admin/PUBLIC 取得 `record_platform_internal` 的 `USAGE` 或 `CREATE`，或 opaque extension member 变为 reachable | 拒绝 catalog admission。runtime 对 `record_platform_internal.digest` 的 direct call 返回 SQLSTATE `42501`。 |
| scoped migrator 收到 serialization failure | rollback 后重试整个 `SERIALIZABLE` closure；任一不可重试错误都不留下 partial ledger、ACL、revision 或 head state。 |
| `BeginTx` 异常返回 `(nil, nil)` | 返回 defensive error；禁止注册 nil transaction rollback 导致 panic。 |

#### 5. Good / Base / Bad Cases

- Good：两个 flag 都关闭时保留 legacy migration；records-on/delete-off 时 direct migrator fresh converge exact current，direct runtime 在 repository 打开前通过 current one-snapshot admission。
- Base：当前 embedded set 是冻结 52-source r1 prefix 加 `0052_create_records_core.sql`，fragment registry 恰有一个 exact `0052` fragment；fresh convergence 写入一个 current genesis，exact repeat 和 direct runtime admission均不改 durable state。
- Good：未来 child 同 PR 添加 `0053+` SQL 与 exact fragment；compiler 在 transaction 前证明一一覆盖，fresh database 自动消费新 source 与 catalog contract。
- Good：binary 已嵌入 `0052+`，strict R2 PostgreSQL anchor 中的 R1 fixture 仍可调用 frozen `AdmitAppACLRuntime`；admission 只消费 validated R1 prefix，而 current admission 独立消费完整 current set。
- Good：fragment callback 在 source compile 返回 privilege slice 后，调用方修改 captured slice 或 callback 自身状态；current catalog 仍使用首次物化的深拷贝结果，callback 不会再次执行。
- Good：第三方 schema 及其第三方 owner 的 default ACL 可以保留而不扩张 APP role，所以 scoped admission 接受它们。
- Good：第三方 schema 可以拥有 `monitoring_instances` 或 `record_platform_cas_contract_activation_projection(bytea)` 同名对象；current path 只检查 compiled schema/identity tuple，fresh convergence 与 runtime admission 仍成功。
- Bad：把 old checksum、null head 或 successor revision 当作 generic error，CLI 会丢失唯一安全可操作的 rebuild cause；只测 migration 数量变化不能覆盖该状态矩阵。
- Bad：对任一 projector 给 runtime/admin grant、把通用 `REVOKE EXECUTE ON ALL FUNCTIONS` 当作 PG16 `pgcrypto` hardening evidence，或按 extension-member name 过滤，都会创建 callable privilege 或隐藏 non-extension drift。
- Bad：分开开启 manifest/catalog transaction、以 member login 后 `SET ROLE`、product route 调用 frozen R1/R2 或 `migrate.Apply`、admission failure warning-only，都会破坏 exact-current boundary。
- Bad：frozen `AdmitAppACLRuntime` 的 verifier closure 直接捕获 `migrations.FS` 并调用 full-set verifier；第一次追加 current migration 后，exact R1 manifest 会被误报为 `latest app ACL manifest migration set does not match embedded migrations`。
- Bad：source preflight 调用一次 `Privileges(validationDatabase)`，catalog compile 又调用一次 `Privileges(actualDatabase)`；stateful callback 或 captured slice 可以让事务前验证与实际 privilege contract 不一致。

#### 6. Tests Required

- Current compiler/convergence/runtime 与 caller selector：

  ```bash
  go test ./internal/center/store/migrate ./cmd/houfeng-record-platform-admin \
    ./cmd/houfeng-center ./cmd/houfeng-import-vps-json -count=1
  ```

  必须覆盖 missing/registered fragment、invalid/duplicate fragment、privilege callback 单次求值与 materialized slice defensive copy、跨 managed schema 同名 tuple、unrelated ledger companion、future managed private schema、fresh/exact transaction cutpoint、count/name/checksum mismatch、null head、successor revision、SET ROLE、catalog drift、nil transaction、admin safe sentinel、legacy `Apply` 保留和三个 current 默认 binding。每个 mismatch test 同时断言 catalog/write seam 未调用。
- Real PostgreSQL current suite 必须经 strict wrapper 运行；locally skipped test 不构成 evidence：

  ```bash
  scripts/test-record-platform-integration.sh postgres -- \
    go test ./internal/center/store/migrate \
    -run '^TestPostgresIntegrationAppACLCurrent$' -count=1
  ```

  断言 fresh + direct runtime、exact repeat 的 ledger `name/checksum/applied_at` 与其余 durable snapshot 深相等、unrelated schema 中同名 relation/function 被接受、injected future source 对 prior baseline 返回 rebuild sentinel 且前后 snapshot 深相等；wrapper 输出不得含 `SKIP`。
- Frozen regression：完整 migrate package run 必须保留 `ConvergeAppACLR1` null-head adoption、`AdmitAppACLRuntime` one-snapshot，以及 isolated R2 bootstrap/finalize/runtime suites；current product caller 不得路由到它们。strict `TestPostgresIntegrationAppACLR2` 的 R1 reader/runtime subtest 必须在 binary 已嵌入 `0052+` 时实际调用 `AdmitAppACLRuntime` 并通过，不能只测 injected verifier 或 zero-test compile。
- Full gate 与 static writer audit：

  ```bash
  make verify-go
  rg -n 'ConvergeAppACLR1|ConvergeAppACLCurrent|AdmitAppACLRuntime|AdmitAppACLCurrentRuntime|migrate\.Apply' \
    cmd/houfeng-record-platform-admin cmd/houfeng-center cmd/houfeng-import-vps-json
  ```

  审查 `migrate.Apply` 仅位于 legacy defaults，current writer/runtime 是 records-enabled product defaults，R2 commands 仍独立。

#### 7. Wrong vs Correct

```go
// 错误：records-on startup 复用 owner migration writer。
if err := migrate.Apply(ctx, db.Pool()); err != nil {
    return err
}

// 正确：mode 选择互斥的 legacy migration 或 current runtime admission。
switch cfg.RecordPlatformMode {
case config.RecordPlatformModeLegacy:
    err = applyMigrations(ctx, db)
case config.RecordPlatformModeRuntimeAdmission:
    err = migrate.AdmitAppACLCurrentRuntime(ctx, db.Pool())
}
```

```sql
-- 错误：把每个 persistent schema/default-ACL owner 都视作 APP-managed。
where namespace.nspname !~ '^pg_'
  and namespace.nspname <> 'information_schema'

-- 正确：object reader 接收 compiled current inventory；default-ACL reader 是单独查询，
-- 并只按 persisted migrator role 限定范围。
where namespace.nspname = any($1::name[]) -- compiled managed schema inventory

-- 单独的 default-ACL query：
where default_acl.defaclrole = $2
  and (
    default_acl.defaclnamespace = 0
    or namespace.nspname = any($3::name[])
  )
```

```go
// 错误：每个 reader 各自开启 snapshot，随后 startup 修复 drift。
manifest := NewPostgresAppACLManifestRuntimeReader(db).ReadAppACLManifestRuntimeSnapshotV1(ctx)
catalog := VerifyPostgresAppACLEffectiveCatalogR1(ctx, db, input)
_ = migrate.Apply(ctx, db)

// 正确：一个 direct-runtime REPEATABLE READ READ ONLY transaction 检查
// identity + manifest + ledger + scoped catalog，然后只会 admit 或 stop。
if err := migrate.AdmitAppACLCurrentRuntime(ctx, db); err != nil {
	return fmt.Errorf("admit app runtime: %w", err)
}
```

```go
// 错误：冻结 R1 admission 读取会增长的完整 embedded set。
return verifyAppACLManifestRuntimeSnapshotV1(snapshot, migrations.FS)

// 正确：先验证并截取 exact frozen prefix，再复用已 canonicalize 的 set。
frozenSources, err := snapshotAppACLR1MigrationSources(migrations.FS)
if err != nil {
	return err
}
return verifyAppACLManifestRuntimeSnapshotWithMigrationSetV1(snapshot, frozenSources.canonicalSet)
```

```go
// 错误：不同 source 只给 generic error，caller 无法安全提示重建。
return fmt.Errorf("checksum mismatch for %q", filename)

// 正确：count/name/checksum、null head、valid successor 都保留 typed cause。
return fmt.Errorf("%w: checksum mismatch for %q", ErrDevelopmentDatabaseRebuildRequired, filename)
```

```go
// 错误：transaction-specific catalog compile 再次调用 public callback。
privileges := fragment.Privileges(databaseName)

// 正确：source compile 已物化并深拷贝 template；这里只替换 database tuple 占位符。
privileges, err := appACLCurrentPrivilegesForDatabase(fragment.Privileges, databaseName)
```

---

### Scenario: APP ACL R2 Slice 5 direct finalizer, M2 evidence, and ACL provenance

#### 1. Scope / Trigger

- Trigger：修改隔离的 `db/appaclr2/migrations/0052_app_acl_r2_privileged_transition.sql` 的 finalize section、`internal/center/store/migrate/app_acl_r2_finalize.go`、M2 catalog verifier、或 `houfeng-record-platform-admin finalize --scope app-acl-r2` 时。
- 此场景只覆盖 Slice 5 从 exact `PREPARED` 到 exact `FINALIZED` 的 direct-migrator writer。它新增的持久化面仅为 `public.app_acl_r2_manifest_revisions`、`public.app_acl_r2_manifest_head` 及其 immutable trigger helper；冻结 M1 relation、R1 writer 和 runtime route 都不是本场景的写入目标。
- 不把此场景的源码/单元验证当作 PostgreSQL 16 integration evidence，也不把它延伸为 Slice 6/7、R2 总验收、physical clone/restore detection 或 Child 1/PF-AC 的证据。

#### 2. Signatures

- Admin route：`houfeng-record-platform-admin finalize --scope app-acl-r2`；唯一允许的 credential input 是 `HOUFENG_RECORD_PLATFORM_MIGRATOR_APP_DATABASE_URL`。
- Go entry：`FinalizeAppACLR2(ctx context.Context, db *pgxpool.Pool) error`。它在保留的 direct-migrator connection 上开启 `SERIALIZABLE READ WRITE` transaction，并复用 `ClassifyAppACLR2State(ctx, tx)` 和 `ReadAppACLR2CatalogPredicatesInTx(ctx, tx)`。
- M2 SQL identity：`public.app_acl_r2_manifest_revisions`、`public.app_acl_r2_manifest_head`、`record_platform_internal.app_acl_r2_reject_manifest_mutation()`；唯一 revision/head 是 `(protocol_version, manifest_revision) = (2, 2)` 与 singleton head。

#### 3. Contracts

- Finalizer 先固定 `search_path`、验证 direct constrained migrator 的 `session_user == current_user` 与冻结 M1 migrator binding、按固定顺序加锁，再只经共享 constrained catalog path 接受 exact `PREPARED`。它不创建 private classifier/predicate/snapshot，也不调用 `pg_control_system()`。
- **Persisted evidence is not fresh physical identity**：bootstrap 在初始 PREPARED commit 前把 live `postgres_system_identifier` 绑定进 immutable receipt/domain。finalizer 只比较该 persisted receipt/domain（以及 M2-domain）并新鲜读取 database OID/name 与 catalog continuity；它既不重读也不声称证明 fresh physical system identifier。
- Finalizer 执行 marker-bounded M2 DDL，写入 immutable M1 links、53-source/206-tuple/domain/receipt/control-ACL digest bodies，以 M1 revision/digest 和 empty-head CAS 建立一 revision/one true head，revoke-first 收敛 ACL，随后以同一共享 path 回读 exact `FINALIZED` 才 commit。`40001`/`40P01` 只能重试整个 closure；不确定 commit acknowledgement 只能在同一总 attempt budget 内重新分类 exact `FINALIZED` 或 `PREPARED`。
- **Owner identity is distinct from explicit ACL and effective privilege**：direct migrator 仍拥有两张 M2 table 和 helper，但 owner OID 不能替代 ACL/effective evidence。revoke-first DCL 必须写入 owner 的 non-grantable self `SELECT`/`EXECUTE` row；effective table vector 对 owner/runtime 都只能是 `SELECT=true`，其余六项为 false，admin 七项全 false；helper 仅 owner `EXECUTE=true`。
- **Raw ACL provenance and effective reachability must both be exact**：`aclexplode` 精确要求每张 M2 table 有 direct-migrator 与 center-runtime 的 no-grant-option `SELECT`，helper 只有 direct-migrator no-grant-option `EXECUTE`；`has_table_privilege`/`has_function_privilege` 独立探测上述完整向量。删除 owner self row 后 raw 与对应 effective 结果都为 false，Exact M2 必须拒绝。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| route/scope/DSN 不精确，或存在 ambient `PG*`、service/pass/TLS-file source | 在 pool 创建前拒绝；不回退通用 opener。 |
| 非 direct constrained migrator、R1/CORRUPT/nonexact PREPARED、receipt/M1/source/domain continuity drift | mutation 前 fail closed；不得创建 M2。 |
| M2 section body/marker/cardinality/digest 不精确，或 M1/head CAS 不是一行 | 拒绝；整个 transaction rollback 到 PREPARED。 |
| DDL/DCL/readback 的 `40001` 或 `40P01` | 仅在总 attempt budget 内重跑整个 serializable closure；其他错误或耗尽立即停止。 |
| 缺少/撤销 owner self `SELECT` 或 helper self `EXECUTE` row | raw `aclexplode` 与对应 effective probe 都为 false；不得作为 FINALIZED 或 ACK-recovery success。 |
| owner/runtime 获得非 `SELECT` table privilege、admin 获得任何 queried M2 privilege，或 owner 缺少 `SELECT`/helper `EXECUTE` | effective privilege drift，拒绝。 |
| 需要新的 physical-system 读或 PG16 live integration conclusion | 不属于 Slice 5 source contract；不得以 persisted evidence 或 unit test 代替。 |

#### 5. Good / Base / Bad Cases

- Good：精确 PREPARED、direct migrator direct login、完整 shared continuity 和 canonical source section 在一个 transaction 中产生两个 direct-owned M2 relations、one revision/head、精确 ACL，readback 为 FINALIZED。
- Base：已 exact FINALIZED 的 direct-migrator repeat 仅做受约束的分类/验证，不重放 M2 DDL/DML；uncertain ACK 只接受 exact FINALIZED，或在剩余 budget 内从 exact PREPARED 整体重试。
- Bad：把 receipt 中保存的 system identifier 当成 finalizer 可重新读取的 fresh physical identity；这越过 bootstrap-only boundary。
- Bad：只检查 owner OID、raw ACL 或 effective privilege 中任一项，因此把缺行、扩权或 grant-option 漂移当作健康；三类证据必须独立核验。

#### 6. Tests Required

- Slice 5 focused unit/source gate：

  ```bash
  go test ./db/appaclr2/migrations ./cmd/houfeng-record-platform-admin ./internal/center/store/migrate \
    -run 'AppACLR2(Source|Finalize|Manifest|M2)|VerifyAppACLR2M2' -count=1
  ```

  断言 strict finalizer DSN/opener、reserved connection + lock/transaction lifecycle、whole-closure retry/ACK bounds、M2 DDL shape/CAS/rollback、owner self-ACL inversion，以及 raw ACL/effective privilege split。
- 同一三个 package 的 full `go test -count=1`、`go vet`、受影响 Go 文件的 `gofmt -d` 与 `git diff --check HEAD` 是本地 source gate。
- 真 PostgreSQL 16 authority matrix、OID-10/bootstrap/live-system trace 和 image lanes 仍由 Slice 7 integration tests 负责；未运行该 lane 时不得写成 PG16 evidence。

#### 7. Wrong vs Correct

```go
// 错误：post-bootstrap writer 重新读取 physical system identity。
systemID := readPGControlSystem(ctx, tx)

// 正确：只比较 persisted receipt/domain 与 fresh database OID/name/catalog facts。
state, err := ClassifyAppACLR2State(ctx, tx)
```

```sql
-- 错误：用 owner 或 has_table_privilege 取代 raw grant proof。
select has_table_privilege(:owner_oid, relation_oid, 'SELECT');

-- 正确：先精确检查 aclexplode 的 owner/runtime rows，再独立检查完整 effective vector。
select grantor, grantee, privilege_type, is_grantable
from pg_catalog.aclexplode(relacl);
```

---

### Scenario: APP ACL R2 runtime admission/startup

#### 1. Scope / Trigger

- Trigger：新增或修改 `AdmitAppACLR1OnlyRuntime`、`AdmitAppACLR2Runtime`、`StartAppACLR2Runtime`，或改变其跨 PostgreSQL connection、advisory lock、transaction、classifier、frozen verifier、direct-runtime predicate、startup admission contract 时。本场景命中新公开 API 与跨 DB runtime admission/startup 的 code-spec mandatory trigger。
- 此场景只定义 transition-aware R2 runtime route：R1-only route 只 admit exact R1；R2/startup route 在 transition 前 admit exact R1、在 shared proof 已完成后 admit exact `FINALIZED`。不得修改冻结的 R1 parser、reader、converger、`AdmitAppACLRuntime`、既有 R1 startup route 或 generic `migrate --scope app`，也不得加入 R2 dispatch dependency。
- 当前冻结摘要必须按 closed source contract 与独立 vector 使用：R1 `0051_create_record_platform_foundation.sql` SHA-256 为 `503d58670dc790c4b852bfb58cf93d2b816c1ce956958567dc605cb28d5cd23f`，共 52 sources、204 tuples；R2 `0052_app_acl_r2_privileged_transition.sql` SHA-256 为 `23f79c60dcede45a42aae82da5a9de0d3d650d7eef64dbfd7ce96c6dd5d95fff`，53-source digest 为 `1d9dc20e71e9f319f8b1cef4b22f9dc92051a88dc9cb8a892b69494658c44dd3`，共 53 sources、206 tuples。

#### 2. Signatures

```go
func AdmitAppACLR1OnlyRuntime(ctx context.Context, db *pgxpool.Pool) error
func AdmitAppACLR2Runtime(ctx context.Context, db *pgxpool.Pool) (AppACLR2State, error)
func StartAppACLR2Runtime(ctx context.Context, db *pgxpool.Pool) (AppACLR2State, error)
```

- `StartAppACLR2Runtime` 是 opt-in startup wrapper，必须只委派给 `AdmitAppACLR2Runtime`；它不得创建第二套 classifier、predicate、connection 或 cleanup lifecycle。

#### 3. Contracts

- 必须先 `Acquire` 一个 reserved connection；在该连接的任何 snapshot query 前，以 exact key `houfeng.app-acl-r2-privileged-transition.v1` 和 `hashtextextended($1, 0)` 取得 **session-scoped shared** advisory lock：`pg_advisory_lock_shared(...)`。seed 固定为 `0`，key、shared mode、session scope 与 bootstrap 的 exclusive transition lock 必须是同一物理 lock identity。
- 同一已锁 connection 只允许开启一个 `pgx.Tx`：`REPEATABLE READ, READ ONLY`。classify、R1 verifier/predicate 与 commit/rollback 都必须在这个 transaction/connection lifecycle 内；不得在 lock 后另取 connection 或另开 snapshot。
- `R1`：只能按 `ClassifyAppACLR2State(ctx, tx)` -> `VerifyFrozenAppACLR1StateInTx(ctx, tx)` -> `RequireDirectFrozenAppACLR1RuntimeInTx(ctx, tx, frozen)` 的共享顺序在同一 tx 中执行。direct-runtime identity mismatch fail closed。
- `FINALIZED`：只消费 shared classifier 的 exact finalized result；不得再调用 frozen R1 verifier、direct-runtime predicate、frozen `AdmitAppACLRuntime`、R2 parser 或另一条 R2 admission path。
- `PREPARED`、`CORRUPT` 和 unknown state 只能由 classifier 识别后 fail closed；它们不得调用 frozen verifier/predicate、frozen `AdmitAppACLRuntime`，也不得进行 R2 payload、receipt 或 manifest parsing/admission。
- 成功 `Commit` 或 `Rollback` 后必须用相同 key/seed/mode/scope 执行 `pg_advisory_unlock_shared(...)`；unlock 成功才 `Release` connection。任何 lock/begin/finish/unlock 异常都必须 discard reserved connection，不能让可能持有 session lock 的连接回到 pool；discard 使用 fresh、bounded background cleanup context。

#### 4. Validation & Error Matrix

| 条件 | 外显结果与 cleanup 不变量 |
| --- | --- |
| public API 的 `db == nil` | R1-only 返回 error；R2/startup 返回 `AppACLR2StateCorrupt` + error。不得 acquire connection。 |
| nil begin dependency，或 classifier/verifier/predicate dependency 不完整 | fail closed（内部 state 为 `AppACLR2StateCorrupt`；R1-only 对外仅返回 error）；不得 reserve、lock 或 begin tx。 |
| acquire error 或 acquire 返回 nil connection | 返回 wrapped reserve error；没有可 cleanup 的 connection，不得 begin tx。 |
| session shared lock error | 返回 lock error；不得 begin/unlock/release，已 reserve 的 connection 必须 discard。 |
| begin error 或 begin 返回 nil tx | 返回 wrapped begin/nil-tx error；在已取得 lock 的连接上尝试 matching unlock，unlock 成功才 release，否则 discard。 |
| classifier、frozen verifier 或 direct-runtime predicate error | fail closed；primary body error 对外返回。deferred rollback 必须尝试完成 locked tx；rollback 成功时 matching unlock + release，rollback failure 使连接状态不确定并 discard，不使用 `errors.Join` 返回 rollback error。 |
| commit error | explicit `Commit` finish API error 返回给其 caller；admission 返回 `AppACLR2StateCorrupt` + wrapped commit error；不执行 unlock/release，直接 discard connection。 |
| rollback finish error | explicit `Rollback` finish API error 返回给其 caller；admission 的 deferred rollback 保留 primary body error，对 rollback failure 只 discard connection，不使用 `errors.Join`。 |
| unlock query error 或 unlock 返回 `false` | 返回 unlock error（`false` 也不是成功）；不 release，必须 discard connection。 |
| request context 在 finish 前取消 | 返回 cancellation error；不得把 canceled request context 用于回收。discard 必须使用 fresh、bounded `context.Background()` timeout。 |
| 同一个 wrapped tx double finish | 第二次返回 `pgx.ErrTxClosed`；不得重复底层 commit/rollback、unlock、release 或 discard。 |

#### 5. Good / Base / Bad Cases

- Good：shared classifier 在 locked read-only snapshot 中返回 exact `FINALIZED`；R2/startup route 不调用 R1 verifier/predicate，commit、matching session-shared unlock、release 各一次。
- Base：shared classifier 返回 exact `R1`；同一 locked `REPEATABLE READ, READ ONLY` tx 依次完成 frozen verifier 与 direct-runtime predicate，再 commit/unlock/release。
- Bad：`PREPARED` 或 `CORRUPT` 被 classifier 识别后仍调用 frozen verifier、direct-runtime predicate、R2 parser 或 admission；必须拒绝且这些额外调用次数为零。
- Bad：R1 direct-runtime identity mismatch，或 lock key、seed、mode、scope、owner lifecycle 任一漂移；必须 fail closed，不得让 connection 以可复用状态返回 pool。

#### 6. Tests Required

```bash
go test ./internal/center/store/migrate \
  -run 'AppACLR2(State|RuntimeAdmission|Startup)|AppACLRuntimeAdmission|Canonical.*V1' -count=1
```

- Focused unit tests 必须断言三个 public API 的 exact-state routing、single reserved connection、lock-before-snapshot、一个 `REPEATABLE READ, READ ONLY` tx、R1 同-tx shared classifier/verifier/predicate，以及 FINALIZED 的 classifier-only trace。
- 必须保留 adversarial `TestAdmitAppACLR2RuntimeRejectsR1ToPreparedRace`：R1 classifier 暂停于 shared lock 持有期间时，bootstrap exclusive transition 不能 acquire/commit；admission 结束后 bootstrap 才能 commit `PREPARED`；随后 fresh admission 只能 classify 后拒绝，且 paired PREPARED assertion 证明没有额外 frozen/R2 call。
- exact-R1 frozen verifier error propagation 是当前 Slice 6 gate：injected sentinel 必须返回 `AppACLR2StateCorrupt`，`errors.Is` 保留 primary error，direct-runtime predicate 调用次数为零；deferred rollback 成功时 unlock/release，rollback failure 只 discard，不能以 `errors.Join` 对外返回 deferred rollback error。
- `StartAppACLR2Runtime` 必须有 `go/parser` AST static binding proof：其 sole return expression 直接调用 `AdmitAppACLR2Runtime(ctx, db)`，不得只做 source substring assertion，也不得依赖 production seam。
- public nil-pool defensive check 可保留。nil dependency、acquire/nil connection、session-lock、begin/nil tx、classifier/predicate injection、commit/rollback/unlock/cancel/double-finish 等 internal seam fault cases 是 optional hardening，不是当前 Slice 6 acceptance gate；保留现有 race、lock-identity 与 fault tests 时，不得把它们扩写为新公开合同。
- Lock mutation tests 必须对 key、mode、seed、scope 和 owner lifecycle 敏感；race fixture 只用于证明这些要求和并发阻塞，不能作为生产 PostgreSQL semantics 的来源。
- 真 PostgreSQL 16 authority/physical-lock evidence 明确属于 Slice 7；在该 lane 未运行时，不得写成已验证的 PG16 结论。

#### 7. Wrong vs Correct

##### Wrong

- 创建 runtime-private classifier，或让 `FINALIZED` 调用 R1 verifier/direct-runtime predicate。
- 先开始 snapshot 再加锁，或用 xact advisory lock 代替 session-shared lock。
- 只比较锁名称、未同时绑定 exact key/seed/mode/scope，或让 unlock 使用不同 lock identity。
- 在 caller cancellation 后用其 canceled context release/discard connection，随后把污染连接交回 pool。

##### Correct

- 只复用 shared classifier；exact R1 在一个 locked snapshot 中串联 frozen verifier 与 direct-runtime predicate，`FINALIZED` 只消费 classifier。
- 在第一个 snapshot query 前，用 exact key + seed `0` 的 session-shared lock 固定单一 connection/`REPEATABLE READ, READ ONLY` snapshot。
- 对 key/mode/seed/scope/owner lifecycle 做精确匹配；每次成功 finish 后 matching unlock 再 release，任何异常走 fresh bounded discard cleanup。

---

### Scenario: Record-platform delivery primitives 的 opaque identity 与 owner fencing

#### 1. Scope / Trigger

- Trigger：修改 `internal/center/recordplatform/` 的 deletion-request token、request fingerprint、outbox worker、identity mutation guard、content lease，或 `internal/center/store/record_platform*.go` 的 idempotency/outbox/lease/reservation-fence transaction 时。
- 此场景只覆盖 r1 已有 APP 表上的可复用 delivery primitive。它不新增 migration、不实现 `deployment_membership` 的 writer/heartbeat/readiness、不注册 bootstrap worker，也不决定 deletion 的 committed/not-committed、ledger/witness/recovery 或 records 业务事实。

#### 2. Signatures

```go
func NewIssuedDeletionRequestTokenV1() (IssuedDeletionRequestTokenV1, error)
func ParseDeletionRequestTokenTransportV1(string) (DeletionRequestTokenTransportV1, error)
func FingerprintRequestV1(RequestFingerprintInputV1) (RequestFingerprintV1, error)
func ParseTrustedPersistedRequestFingerprintV1([]byte) (PersistedRequestFingerprintV1, error)

func NewPostgresRecordPlatformRepository(*pgxpool.Pool, AdmissionGate) *PostgresRecordPlatformRepository
func (r *PostgresRecordPlatformRepository) RunRecordPlatformTransaction(context.Context, RecordPlatformTransactionCallback) error
```

- `RecordPlatformTransaction` 只暴露 transaction-scoped primitive 方法；不暴露任意 SQL、network sender、HTTP client、renderer callback 或正文/recipient 持久化入口。
- `OutboxWorker` 只接收 `FreshOutboxAuthorizer` 与 `OutboxSender`；其 `RenderedDelivery` 是 transient opaque value，不能跨入 store API。

#### 3. Contracts

- `drt1_` transport 只接受无 padding base64url 的 32-byte canonical spelling。issued token 由 `crypto/rand` 生成，只有 issued capability 能计算 deployment/project-bound commitment；parsed transport 不能产生可持久化 commitment。普通 formatting 必须 redacted。
- durable primitive identifier（idempotency key、object/client/mutation、outbox subject 等）不得包含 canonical token 或可解码的 noncanonical `drt1_` alias 子串。拒绝发生在 SQL 前；不得把 raw token、正文或 stable object ID 写入普通日志。
- `RequestFingerprintV1` 只能由固定字段顺序、length-prefix v1 codec 产生。`PersistedRequestFingerprintV1` 是独立的 readback-only sealed type：trusted DB readback 可用于 replay 比较，但不能转换为 claim/complete 所需的 canonical write fingerprint。
- 每个 idempotency/outbox/guard/lease/reservation owner transition 必须同时比较 owner ID、generation、调用方持有的精确 DB-observed expiry 和对应的 `> transaction_timestamp()` live predicate。任何 0-row result 都映射为 lost/stale owner，不能补写；claim/takeover 递增 generation。
- idempotency mismatch 永远是只读 conflict；completed row 必须有结果 fingerprint，idempotency expiry 必须严格晚于 active owner expiry。cleanup 只能删除无 live owner 的过期 primitive，不能删除 reservation/ledger/evidence。
- 所有 claim/finalize 在同一 `pgx.Tx` 内先调用 injected `AdmissionGate`。nil/error gate 必须拒绝且产生 0 primitive write/0 send；Child 1 不实现 concrete membership SQL。
- outbox 顺序固定为 `gate + claim + commit -> fresh authorize/render -> network send -> gate + fenced terminal/retry`。只有 allow 且 current epoch 等于 captured authorization epoch 时可发送；deny/mismatch/missing handler cancel，temporary error retry；ordinary worker logs 只允许固定安全分类，不得记录 dependency error 本文。`Run`/`RunOnce` 的 nil context 直接返回 invalid-worker error，不得 panic。
- `RunOnce` 在依赖校验后必须先返回已取消的 context，`Run` 在启动 pass 前必须安静退出已取消的 context；pre-cancelled worker 不得 claim、authorize 或 send。`LeaseWorkGuardV1` 必须拒绝 typed-nil `Clock`，并同步 `CanContinue` 与 `Renew` 对 owner/stopped state 的访问，避免并发复活本地 authority 或 data race。nil renew callback 也属于 renewal failure：非 nil guard 必须先在 mutex 下永久置为 stopped 再返回 `ErrLeaseRenewalStopped`，后续有效 callback 不得恢复本地 work。
- `DeletionReservationFenceV1` 的成功 fence epoch 至少为 1；reservation/epoch/deletion-fence/object-content lock 顺序不变。client-content lease 没有 object identity，不能单独授权 serving。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| token grammar/长度/canonical spelling 不合法，或 durable identifier 含 token-shaped substring | 返回 input sentinel；不执行 SQL、不持久化或日志化 secret。 |
| caller 试图以 persisted readback 作为 canonical request fingerprint | 类型边界阻断；claim/complete 只接受 sealed `RequestFingerprintV1`。 |
| owner 续租后，旧 expiry token 尝试 renew/release/complete/sent/retry/cancel/assert | SQL 0 row，返回 `ErrLostOwnerLease`；不得补偿写入。 |
| nil/error admission gate | `ErrRecordPlatformAdmissionUnavailable`；0 primitive write，worker 不 authorize/send。 |
| outbox authorizer deny/epoch mismatch/missing handler | fenced cancel，0 send。 |
| outbox temporary authorizer/render/sender failure | fenced retry；下一 claim 必须重新授权。 |
| nil worker context 或 zero `FenceEpoch` | `ErrInvalidOutboxWorker` 或 `ErrInvalidReservationFence`；不得 panic/写库。 |
| typed-nil clock、nil renew callback、pre-cancelled worker 或 concurrent renewal | 拒绝/永久停止本地 work；不得 panic、claim、authorize、send、恢复 authority 或产生 data race。 |

#### 5. Good / Base / Bad Cases

- Good：同 fingerprint 的 completed idempotency request 返回原 result digest；相同 owner/generation 但已续租前的旧 expiry 不能完成或释放新 lease。
- Good：业务 callback、idempotency 和 identity-only outbox enqueue 在一个 admitted transaction 中一起 commit 或 rollback；网络发送只在 claim commit 后运行。
- Base：object epoch 已存在为 0 时，fence 操作把它递增为 1，并以同一 owner/generation 写入 reservation 与 deletion-fence lease。
- Bad：只比较 owner ID/generation 而忽略 observed expiry，会让同一 owner 的旧 token 在 renew 后继续写入。
- Bad：记录 `%w` dependency error、把 transport parser 的 raw token 当 commitment capability，或让 client lease 代替 object serving check，都会越过 secret/authorization boundary。

#### 6. Tests Required

```bash
GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 \
go test ./internal/center/recordplatform ./internal/center/store \
  -run 'Idempotency|Outbox|Guard|Lease|Serving|DeletionFence|Token' -count=1

GOTMPDIR=/home/murray/.codex GOFLAGS=-p=1 \
go test -race ./internal/center/recordplatform ./internal/center/store \
  -run 'RecordPlatform|Idempotency|Outbox|Guard|Lease' -count=10

scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store \
  -run '^TestPostgresIntegrationRecordPlatform' -count=1
```

- Unit/store tests 必须覆盖 canonical/noncanonical token alias、persisted fingerprint 不可写、old-expiry fencing、nil gate/context、typed-nil clock、nil renew callback 的永久 stop、pre-cancelled worker、fixed-safe worker log、guard `Renew`/`CanContinue` race、零 fence epoch、0-row lost owner。
- PostgreSQL selector 不能以 `SKIP` 作为 acceptance evidence；必须覆盖并发 claim、expired takeover、stale finalizer、atomic rollback 和 reservation/epoch/object lease serialization。

#### 7. Wrong vs Correct

```sql
-- 错误：同一 owner renew 后，旧 token 仍可使用。
where owner_id = $1
  and owner_generation = $2
  and expires_at > transaction_timestamp()

-- 正确：同时绑定 DB readback 的 exact expiry 与当前 live predicate。
where owner_id = $1
  and owner_generation = $2
  and expires_at = $3
  and expires_at > transaction_timestamp()
```

```go
// 错误：dependency error 不受 worker 的日志安全合同约束。
logger.Error("record outbox pass failed", "error", err)

// 正确：日志不携带任意 dependency error 文本。
logger.Error("record outbox pass failed")
```

---

### Scenario: Records permanent deletion、core purge 与连续 recovery

#### 1. Scope / Trigger

- Trigger：修改 `internal/center/recorddeletion/`、`internal/center/store/record_deletions*.go`、`record_deletion_recovery*.go`、`0052_create_records_core.sql` 的删除投影/恢复字段，或后续 Records child 注册新的 permanent-delete adapter 时。
- 本场景建立在上一节的 reservation、fence、opaque token、owner lease 与 admission primitive 之上；它拥有 Records 删除编排、core 在线清除和 ledger replay recovery，但不拥有独立 deletion ledger/witness 服务或后续 attachment/evidence/search/activity/collaboration/portability 的内容表。

#### 2. Signatures

```go
func NewRegistry([]Adapter) (Registry, error)
func (Registry) RequireReady(context.Context) (ReadinessSnapshot, error)
func NewService(recordplatform.DeploymentID, Registry,
    DeletionRecordSnapshotSource, DeletionWitnessSource,
    DeletionPreviewRepository, ServiceOptions) (*Service, error)
func NewDeletionWorker(DeletionWorkerRepository, DeletionLedger,
    DeletionEntryWitness, DeletionOnlinePurger,
    DeletionWorkerOptions) *DeletionWorker
func NewCoreAdapter(RecordCoreStore) (*CoreAdapter, error)
func NewRecoveryAdapter(RecoveryStore) (*RecoveryAdapter, error)

func NewPostgresRecordDeletionRepository(
    *pgxpool.Pool, AdmissionGate,
) *PostgresRecordDeletionRepository
```

- Production readiness 的 adapter 名称闭合集固定为 `record_core|record_attachments|record_evidence|record_markdown_client|record_search|record_activity_projection|record_comparison|record_collaboration|record_portability`，顺序同时参与 readiness/preview digest。
- `record_purge_operations` 只是可重建应用投影；primary ledger + full witness 才能证明 delete commit、`attempt_not_committed` 或 operation 不存在。

#### 3. Contracts

- Preview 在任何 reservation/operation 写入前要求九个 adapter 全部注册且 health proof 有效，并重新授权 current record。它绑定 actor scope、record/current revision、lock/auth/content-delivery epoch、dependency/backup/processor inventory、adapter readiness/preview 和 witness head；任一未知、缺失或漂移都 fail closed。
- Execute 只接受同一 preview 的 opaque token commitment/request fingerprint，重新授权并重新计算全部 binding 后才建立 provisional fence。相同 token/fingerprint replay 返回同一 operation；token 复用到不同 binding 是只读 conflict。
- delete worker 的 durable 顺序固定为 provisional fence -> append/resolve delete commit -> witness -> permanent fence -> propagate/read fence -> online purge -> content-free receipt。ledger append/outcome 不确定时持续保留 fence，不能猜测成功或补偿释放。
- 只有 sealed ledger absence proof 才能追加 `attempt_not_committed`；该 outcome 经 witness durable 后，才能以单调 `release_epoch` 释放 provisional reservation/fence。旧 owner/generation/observed-expiry 不能完成、释放或复活新 owner 的 operation。
- `record_core` 只拥有 `records`、revisions/subjects/tags/participants、drafts/checkpoints、`record_domain_activities`、`record_core_purge_receipts` 与该对象的 `content_delivery_epochs`。它在一个 transaction 内清除 current projection 与 exact surfaces、写无正文 receipt，并在 commit 前后验证 absence；不得宣称后续 child 的内容表已清除。
- Registry readiness、preview 与 aggregate receipt 编码保留闭合的 root-first adapter 顺序；online purge 必须按该顺序的逆序逐个 purge + verify，让 attachment 等依赖 surface 先释放 restrictive FK，`record_core` 最后删除 revision、draft 与 record root。
- Recovery 按 ledger sequence/hash 连续 replay，绑定 previous hash、witness proof、request fingerprint bytes、entry-type 对应的固定 surface allowlist 与 surface digest。delete commit replay 原子重建 terminal projection并清除 core；`attempt_not_committed` replay只建立/修正 terminal projection并释放 fence，不得 purge content。
- recovery 必须覆盖 existing operation、preview-only reservation 和无本地 projection 三种 cut point；重复同一 entry 幂等，identity/cursor/receipt/audit/fence 任一分歧都保持 fail closed，不推进 cursor。
- existing operation 从 `fenced` 进入 recovery terminal state 时，只能失效该 projection 读取到的 exact deletion-fence owner tuple（owner ID、generation、observed expiry）。已终态 operation、preview-only reservation、synthetic projection 和幂等 replay 不得按对象释放 fence；verification 也不得要求对象全局不存在 active fence。另一个较新 operation 的 fence 必须原样保留并继续阻止内容读取。
- 所有 PostgreSQL mutation 先执行 injected `AdmissionGate`，nil/error gate 产生 0 write。operation status 本地 row 缺失且 ledger+witness fallback 尚未证明不存在时返回 unavailable，不能伪装成 not-found 或完成。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| adapter 缺失/重复/额外、surface 重复、health error/unknown/unhealthy | `ErrDeletionSafetyUnavailable`；0 preview reservation、0 ledger mutation。 |
| preview 后授权、record CAS、dependency/inventory、adapter digest 或 witness head 漂移 | stale/conflict；不建立 provisional fence。 |
| delete append/resolve 或 outcome append 结果未知 | operation 保持 `ledger_commit_unknown` / `release_pending` 且 fence 保留；等待 authoritative resolution。 |
| delete commit 已 witness，purge/fence propagation/receipt 失败 | `retry_required` 记录 exact retry stage；不得回退为未删除或释放 fence。 |
| sealed absence 未证明或 `attempt_not_committed` 未 witness | 不释放 reservation/fence、不恢复普通读写。 |
| core receipt 含正文、identity 漂移、surface/receipt digest 不符或仍有 owned row | rollback/verification error；operation 不得进入 `online_purged`。 |
| recovery cursor 不连续、previous hash/witness/fingerprint/surface digest 不符 | `ErrRecoveryContractUnavailable`；0 content/projection/cursor change。 |
| recovery 要释放的 exact owner tuple 已被续租、替换或不存在 | `ErrRecoveryContractUnavailable`；transaction rollback，不得触碰当前 fence。 |
| terminal/idempotent replay 时对象存在另一个 active fence | replay 按自身 receipt/audit/cursor 合同成功；另一个 fence 的 owner/generation/expiry 完全不变。 |
| status projection missing 且无 authoritative fallback | `ErrDeletionStatusUnavailable`，由 HTTP 映射为 opaque 503。 |

#### 5. Good / Base / Bad Cases

- Good：delete commit 已 witness，worker 先永久 fence，再逐 adapter purge/verify；core receipt 不含 record/revision ID 或任何业务内容，最终 operation 为 `online_purged`。
- Good：ledger 明确证明 delete 未提交，worker 追加并见证 `attempt_not_committed` 后释放 provisional fence；record 内容完全保留，同 token replay 仍返回同一 `not_committed` operation。
- Base：当前 production 只注册 core adapter；即使所有 core 表为空，readiness 仍为 false，preview 不签发 token。
- Base：应用投影丢失后 recovery 从连续 ledger cursor 重建 terminal projection；重复 replay 不重复 receipt/audit，也不跳过未知 entry。
- Good：operation A 已终态后 operation B 建立新 fence；A 的幂等或延迟 replay 不更新 B 的 fence，B 继续让 Records read 返回 reserved。
- Bad：以“后续表当前为空”省略 adapter，收到 ledger timeout 就释放 fence，或仅凭 `record_purge_operations` 无 row 返回 404；这些都会把未知状态误当作安全结论。
- Bad：recovery 以 `(project, kind, object)` 无条件 expire fence，或在幂等验证中要求 active fence count 为零；前者会破坏较新 operation，后者会让合法旧 entry replay 永久卡住。

#### 6. Tests Required

```bash
go test -race ./internal/center/recorddeletion ./internal/center/store \
  -run 'Deletion|Purge|Recovery|Reservation' -count=10

scripts/test-record-platform-integration.sh postgres -- \
  go test ./internal/center/store \
  -run '^TestPostgresIntegrationRecord(Platform|Deletion)' -count=1
```

- Registry/service tests必须覆盖 exact nine-adapter readiness、preview reauthorization/CAS/digest drift、same-key replay/reuse、unknown ledger outcome、witness pending 和 sealed-absence-only release。
- Worker/store tests必须覆盖每个 durable cut point、stale owner、permanent fence、retry stage、content-free receipt、core exact ownership 和 purge rollback。
- Recovery PostgreSQL tests必须覆盖 existing operation、preview-only reservation、synthetic terminal projection、delete/not-committed replay、连续 cursor、幂等重放、较新/无关 active fence 的 exact tuple 保留，以及 receipt failure 全事务回滚；runner 中任何 `SKIP` 都不能作为验收证据。

#### 7. Wrong vs Correct

```go
// 错误：本地没有 operation row 就把不可证明状态当成权威不存在。
if errors.Is(err, pgx.ErrNoRows) {
	return ErrDeletionOperationNotFound
}

// 正确：ledger + full witness 未证明不存在时保持 unavailable。
if errors.Is(err, pgx.ErrNoRows) {
	return ErrDeletionStatusUnavailable
}
```

```go
// 错误：只注册当前已有数据的 core adapter，就开放不可逆删除。
registry, _ := NewRegistry([]Adapter{core})
_ = startPermanentDelete(registry)

// 正确：完整闭合集和每个 health proof 都通过后才允许 preview/execute。
snapshot, err := registry.RequireReady(ctx)
if err != nil || !snapshot.Ready() {
	return ErrDeletionSafetyUnavailable
}
```

```sql
-- 错误：按对象释放，可能命中另一个 operation 的新 fence。
update public.deletion_fence_leases
set expires_at = transaction_timestamp()
where project_id = $1 and object_kind = $2 and object_id = $3;

-- 正确：仅当原 recovery projection 仍是 fenced 时，绑定它读取到的
-- owner_id、owner_generation 与 exact expires_at；terminal replay 不执行该 SQL。
```

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

### MonitoringInstance command action durability, global audit, and output TTL

#### 1. Scope / Trigger

- Trigger: 修改 MonitoringInstance action / remote command / global audit 链路时必须加载本节，包括 `POST /api/monitoring-instances/{monitoring_instance_id}/actions`、`GET /api/command-audits`、`agentapi.PendingAction`、`agentapi.CommandResult`、`syncing.CommandResult`、`monitoringinstances.last_action`、`monitoring_instance_command_action_audit`、`store/command_actions.go`、`store/command_audits.go` 或 `store/sync_batches.go`。
- 目标：单 pending action 模型下保持 command identity 可追踪，加入 backend-owned sensitivity / confirmation / permanent metadata audit / 24h output TTL，并在用户或实例永久删除后仍可稳定分页追溯；任何永久审计路径都不得保存或返回 stdout/stderr。

#### 2. Signatures

- HTTP request: `POST /api/monitoring-instances/{monitoring_instance_id}/actions` with body `{"command_id":"uptime"}`；sensitive command 必须是 `{"command_id":"systemctl_status","confirmed_sensitive":true}`。
- HTTP response: `{"action_id":"act_xxx","command_id":"uptime","status":"pending"}`。
- Agent plan: `agentapi.PendingAction{ActionID, CommandID}` serializes as `action_id` + `command_id`。
- Agent result: `agentapi.CommandResult{ActionID, CommandID, Stdout, Stderr, ExitCode}` serializes as `action_id` + `command_id` + output fields。
- DB state: `monitoring_instances.pending_action_id`, `monitoring_instances.pending_action_command_id`, and `monitoring_instances.last_action jsonb`。
- DB audit: `monitoring_instance_command_action_audit(audit_id, action_id?, monitoring_instance_id, monitoring_instance_name_snapshot, command_id, sensitivity, event_type, actor_user_id?, actor_username_snapshot, actor_display_name_snapshot, source, exit_code?, occurred_at, details)`；`event_type in ('queued','dispatched','completed','rejected')`，`rejected` 是唯一允许 `action_id is null` 的事件。
- Read API: `GET /api/command-audits`；首次请求支持 `window=24h|7d|30d|all|custom`、custom bounds、实例/命令/敏感级别/outcome/actor/action ID 与 `limit=1..100`，续页只接受 opaque `cursor`。
- Read model: `commandaudits.Query -> commandaudits.Page`；普通 action 按 `action_id` 分组，拒绝按 `audit_id` 分组，固定执行一条 action query 和一条 page events query。
- Backend metadata source: `internal/contracts/agentapi.KnownCommandDefinitions()` owns command IDs and `standard|sensitive` sensitivity tiers.

#### 3. Contracts

- Current sensitivity tiers:
  - `standard`: `df_h`, `free_m`, `uptime`
  - `sensitive`: `top_head`, `journalctl_u`, `systemctl_status`, `dmesg_err`, `docker_ps`
- Backend is the enforcement authority for sensitivity. Frontend command metadata is presentation only.
- Queueing an action writes both pending columns and `last_action={"status":"pending","action_id":...,"command_id":...,"sensitivity":...,"queued_at":...}` so API/UI readers see pending immediately.
- Sensitive commands require `confirmed_sensitive:true` before queueing. 对认证会话、已知敏感命令、真实且可执行实例的缺确认请求，handler 必须在生成 action ID 前写且只写一个 `rejected`，`details` 精确为 `{"reason":"sensitive_confirmation_required"}`，随后仍返回 400；无效 JSON、未知命令以及不存在/归档/未绑定/暂停实例不写拒绝审计。
- 所有 `queued` / `dispatched` / `completed` / `rejected` 都必须调用 `insertCommandActionAudit`；helper 使用 `INSERT … SELECT` 从当前实例/用户生成快照，并要求 `RowsAffected()==1`。`rejected` 的同一条 INSERT 还必须重新检查 `archived_at is null`、`binding_status='已绑定'`、`monitoring_status<>'暂停'`，避免 handler 读取后状态变化仍留下不可信拒绝；0 行按审计完整性失败返回 500。调用方不得自行拼接审计 INSERT。
- Queueing inserts a `queued` audit event in the same transaction as pending state, with `source='web'` and `actor_user_id` when a browser session user is available.
- Sync dispatch clears `pending_action_*` columns to prevent duplicate dispatch, but rewrites the same pending `last_action` to keep the in-flight identity durable until a matching result arrives. Dispatch must preserve queued `sensitivity` and `queued_at` from the existing pending `last_action`; only missing legacy values may fall back to backend command metadata and dispatch time.
- Dispatch inserts a `dispatched` audit event only after the clear update affects one row, with `source='agent_sync'`.
- Command result storage must include the real `command_id` and update `last_action` to `status="done"` only when current `last_action` is still `pending` with the same `action_id` and `command_id`.
- Completion inserts a `completed` audit event only after the guarded result update affects one row, with `source='agent_sync'` and `exit_code`; stale result `UPDATE 0` must not create audit rows.
- Audit rows are metadata only。具名数据库约束递归禁止 `details` 任意层出现 stdout/stderr；Go read model 和 HTTP response 也只映射 allowlist fields，不定义/透传 `details`、stdout 或 stderr。Handler 的 action/event/instance/actor response DTO 必须由 handler 自有并逐字段复制，不得把领域 JSON 类型直接嵌入 response；否则领域类型以后增加内部字段会静默扩大 API。
- 实例/用户外键在永久审计升级后解除；迁移只能删除约束列包含 `monitoring_instance_id` 或 `actor_user_id` 的目标 FK，必须保留审计表以后可能拥有的其他外键。稳定 ID 是权威身份，名称/用户名/显示名是事件时快照。永久清理实例或删除用户不删除审计，管理审查的 `command_action_audit_count` 属于 evidence，但不计入 cleanup 的 `deleted_reference_count`。
- 旧二进制兼容依靠三个快照列 `not null default ''`；旧式 queued、dispatched、completed 三种 INSERT 都必须实测仍可写，读取空快照时回退稳定 ID。不要在回滚时自动恢复 cascade 外键。
- Read API 首次请求固定上下界；cursor 使用 versioned base64url JSON 封装规范化筛选、limit、固定 bounds 与 `(before_started_at,before_id)`。排序固定为 action `started_at desc,id desc`、event `occurred_at asc,audit_id asc`；outcome 优先级为 rejected → completed(exit 0 succeeded, otherwise failed) → dispatched → queued。
- `monitoring_instance` 和 `actor` 只做转义后的字面量 `ILIKE` 子串匹配；`%`、`_`、`\` 不得成为通配符。默认 30 天 + limit 20，全局时间/action 索引与固定两次查询是当前容量边界。
- Completed `last_action` includes `completed_at`, `output_expires_at = completed_at + 24h`, and `output_expired:false` while output is visible.
- Read-side scan must hide stdout/stderr and return `output_expired:true` once `output_expires_at <= now`, even before retention physically rewrites persisted JSON.
- Retention cleanup must remove expired `last_action.stdout` / `last_action.stderr` and set `output_expired:true`; action ID, command ID, status, exit code, completion time, expiry time, and sensitivity metadata remain.
- `last_action.status` currently uses only `pending` and `done`; command success/failure is represented by `exit_code`, not by `success` / `failed` status strings.
- Go `monitoringinstances.LastAction.ExitCode` must stay nullable (`*int`) with `omitempty`: pending actions omit it, while completed success still serializes `exit_code: 0`.
- `last_action` is the current visible action state, not a full audit log. Do not infer historical command execution from it after another action is queued.
- 系统目前只有 admin 角色；读取沿用 session/same-origin，不在本链路伪造角色隔离。第二种真实角色出现时按 GitHub #381 同时设计命令授权、审计读取范围与 `authorization_denied` 审计。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Missing `command_id` in MonitoringInstance action request | 400 `command_id required` |
| Unknown `command_id` | 400 `unknown command_id`; no repository write |
| Sensitive command missing confirmation, executable instance | Insert exactly one `rejected`; no action/queue/`last_action` change; return 400 |
| Sensitive command missing confirmation, missing/archived/unbound/paused instance | Keep confirmation 400 priority; do not write rejected audit |
| Standard `command_id` without confirmation | Queue action |
| Unknown monitoring instance | 404 `monitoring instance not found` |
| MonitoringInstance is not bound | 409 `monitoring instance agent not bound` |
| monitoring instance monitoring is paused | 409 `monitoring instance monitoring is paused` |
| Agent result lacks `action_id` or `command_id` | Ignore the result row; do not overwrite `last_action` |
| Agent result identity does not match current pending `last_action` | Ignore the result row; do not overwrite `last_action` |
| Completed output `output_expires_at <= now` | API omits stdout/stderr and returns `output_expired:true` |
| DB write failure while queueing/dispatching/storing | Return wrapped repository error; handler maps to 500 where applicable |
| Trusted rejection lookup/audit write fails | Return 500; do not silently return the ordinary 400 |
| Instance becomes archived/unbound/paused before rejected INSERT snapshot | INSERT 0 rows; return 500 and do not create an untrusted audit |
| Invalid command-audit enum/time/limit/cursor or cursor mixed with another query | 400 `invalid input`; repository is not called |
| User or monitoring instance permanently deleted | Audit remains queryable with stable ID/snapshot and `monitoring_instance.deleted=true` |

#### 5. Good/Base/Bad Cases

- Good: user queues `uptime`, API immediately returns `command_id`, `last_action` shows pending `uptime`, agent returns matching `action_id` + `command_id`, and `last_action` becomes done with stdout/stderr/exit code.
- Good: user clicks `systemctl_status`, frontend opens a second confirmation, POST includes `confirmed_sensitive:true`, backend queues it and writes a sensitive `queued` audit event.
- Good: `systemctl_status` pending state is dispatched later; dispatch preserves `sensitivity:"sensitive"` and the original `queued_at` while writing a `dispatched` audit row.
- Good: completed output expires after 24h; API still returns command identity and exit code, but stdout/stderr are empty and retention later clears persisted output fields.
- Good: executable `systemctl_status` without confirmation writes one rejected metadata event, creates no action, and appears in `/api/command-audits?outcome=rejected` without output fields.
- Good: an archived MonitoringInstance is permanently cleaned; global audit pages still show its stable ID, name snapshot, actor snapshot and “deleted” state through the same cursor.
- Base: no pending action and no command results in a sync batch leaves `last_action` unchanged.
- Bad: writing `last_action.command_id=""` from command results makes the UI lose the command label.
- Bad: storing command results with `WHERE monitoring_instance_id = $2` only can let a stale result overwrite a newer pending action.
- Bad: audit `details` includes command stdout/stderr "for convenience"; this turns audit into long-lived sensitive output storage.
- Bad: dispatch rewrites sensitive pending state as `sensitivity:"standard"` or resets `queued_at` to dispatch time; this breaks UI/audit continuity.
- Bad:解除外键后改用 `VALUES` 直接写 monitoring instance / actor ID；这会为从未存在的实体制造永久伪审计。
- Bad:0050 遍历并删除审计表全部外键；以后新增的无关引用约束会被静默破坏。
- Bad:分页续页重新计算 `now()-30d`，或按 `started_at` 单键翻页；同时间 action 和新事件会产生重复/跳页。

#### 6. Tests Required

- Agent runtime test: pending action execution returns `CommandResult.ActionID` and `CommandResult.CommandID`.
- Agent handler test: sync request conversion preserves `command_results[].command_id`.
- Store tests: queueing writes pending `last_action` with `sensitivity` / `queued_at`; queue/dispatch/completion audit insert order and metadata; dispatch clears pending columns while preserving pending JSON metadata; result update SQL guards on pending status, action ID, and command ID; `UPDATE 0` mismatch is non-fatal and has no completion audit; result storage runs before dispatching a newly queued action in the same sync transaction; TTL scan and retention clear expired stdout/stderr.
- Handler tests: trusted rejection writes once/no action，non-trusted inputs do not write，lookup/audit failures return 500；command-audit filters/cursor/cursor-only continuation、handler-owned nested response DTO 和 response allowlist are covered.
- Migration tests: 0046→0050 + repeated apply、snapshot backfill、三种旧 INSERT compatibility、只移除实例/actor FK 且保留无关 FK、named constraints and three indexes；fresh install also records 0050 once。
- Real PostgreSQL tests: deletion/cleanup retention、five outcomes、literal filters、same-time composite keyset、real handler cursor and `EXPLAIN (ANALYZE, BUFFERS)`；代表性数据必须使用 global time/action indexes、limit+1，repository query count exactly 2。
- Frontend API/page/browser tests: `postMonitoringInstanceAction` confirmation，`listCommandAudits` cursor-only，URL canonicalization/draft/load-more/deleted identity/output allowlist，以及 `/command-audit` 的 10×3 route、axe、390px named local scroll 与 Modal focus。

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

```go
// 错误：dispatch 时丢掉 queued metadata，把 sensitive 命令改成 standard。
raw, _ := marshalPendingLastAction(actionID, commandID, "standard", time.Now().UTC())
```

```go
// 正确：dispatch 从现有 pending last_action 继承 sensitivity / queued_at，缺失时才兜底。
raw, _ := marshalDispatchedPendingLastAction(actionID, commandID, existingLastActionRaw, dispatchedAt)
```

```go
// 错误：解除 FK 后由调用点直接 VALUES 写入，无法证明实例/actor 曾存在。
_, _ = tx.Exec(ctx, `insert into monitoring_instance_command_action_audit (...) values (...)`)

// 正确：所有事件走同一个 INSERT … SELECT helper，并要求恰好一行。
if err := insertCommandActionAudit(ctx, tx, event); err != nil {
	return fmt.Errorf("insert command action audit: %w", err)
}
```

```sql
-- 错误：rejected 只信任 handler 较早读取的状态。
where mi.monitoring_instance_id = $3

-- 正确：写入快照再次执行可信状态安全门；状态已变化时 INSERT 0 行并 fail closed。
where mi.monitoring_instance_id = $3
  and mi.archived_at is null
  and mi.binding_status = '已绑定'
  and mi.monitoring_status <> '暂停'
```

```go
// 错误：领域类型未来增加字段时会自动进入 handler JSON。
type commandAuditActionResponse struct {
	Actor *commandaudits.ActorIdentity `json:"actor"`
}

// 正确：handler 自有 DTO，逐字段复制公开 allowlist。
type commandAuditActorResponse struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}
```

### MonitoringInstance lifecycle management and archive gates

#### 1. Scope / Trigger

- Trigger: 修改 `monitoring_instances` lifecycle / monitoring / archive 字段、监控实例列表 scope、管理审查、退役 / 恢复 / 归档 / 永久清理 API、agent sync ingest、onboarding / runtime control / action / metadata 写路径。
- 目标：MonitoringInstance 是可管理对象，不只是“新增接入 agent”的副产品；错误创建的空实例要能安全清理，真实历史实例要能暂停、退役、归档和恢复，且停止状态不得继续沉淀观测数据。

#### 2. Signatures

- DB columns: `monitoring_instances.archived_at timestamptz null`、`monitoring_instances.archived_reason text not null default ''`。
- List API: `GET /api/monitoring-instances?scope=active|archived|all`，省略 scope 等同 `active`。
- Review API: `GET /api/monitoring-instances/{monitoring_instance_id}/management-review`。
- Management APIs:
  - `POST /api/monitoring-instances/{id}/lifecycle/retire` with `{"reason": "..."}`
  - `POST /api/monitoring-instances/{id}/lifecycle/restore` with `{"reason": "..."}`
  - `POST /api/monitoring-instances/{id}/archive` with `{"reason":"...","confirmation_name":"<display_name>"}`
  - `POST /api/monitoring-instances/{id}/restore-from-archive`
  - `POST /api/monitoring-instances/{id}/permanent-cleanup` with `{"reason":"...","confirmation_name":"<display_name>"}`
- Domain types: `monitoringinstances.ListScope`、`ManagementReview`、`ManagementCounts`、`ManagementActions`、`LifecycleActionInput`、`ArchiveInput`、`PermanentCleanupInput`、`PermanentCleanupResult`。

#### 3. Contracts

- `lifecycle_status` 不包含 `已归档`；归档只由 `archived_at is not null` 表达。允许的 lifecycle 仍是 `待接入`、`在用`、`观察中`、`不续费`、`已退役`。
- 默认列表只返回未归档实例；`scope=archived` 只返回归档实例；`scope=all` 返回全部实例，但仍沿用已有 VPS 关联工作集裁剪规则。
- `management-review` 必须一次返回实例、活跃 VPS link、数据 / 审计计数、warnings、blockers、actions 和 `empty_mistake_candidate`；前端不得自行拼多个接口后决定危险操作是否允许。
- 退役必须设置 `已退役 + 暂停`，清空 enrollment token、sync token、pending binding、pending action，并写生命周期事件。
- 从退役恢复必须设置 `观察中 + 暂停`，不自动恢复 token、action 或采集。
- 归档必须在事务内 `select ... for update` 锁定实例，重新计算 review，校验 `confirmation_name`，要求实例已退役且没有仍在当前工作集的 VPS link；成功后设置归档字段、暂停监控、撤销 token / pending binding / pending action。
- 从归档恢复必须清空归档字段并设置 `观察中 + 暂停`；恢复后仍需要用户显式接入或恢复监控。
- 永久清理必须在事务内锁定实例、重新计算 review、校验名称确认。空误创建实例可直接清理；有观测 / 事件 / 通知 / lifecycle step 等证据的实例必须先归档。删除实例前先显式删除没有 FK cascade 保护的直接引用，再删除 `monitoring_instances`，其余心跳、样本、观测、IP 质量和 VPS link 依赖 FK cascade。
- 暂停、退役或归档实例的 agent sync 必须在任何心跳、host sample、probe observation、IP 质量报告或 action result 写入前短路，返回空 plan；不要推进 `last_sync_at`。
- 已归档实例必须阻断 install command / enrollment token、binding confirm/reject/reset、metadata update、runtime resume、action queue/dispatch 等会继续接入或控制 agent 的写路径。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| invalid list scope | HTTP 400 `invalid input` |
| missing management reason | HTTP 400 `invalid input` |
| archive / cleanup confirmation name mismatch | HTTP 400 `invalid input` |
| unknown monitoring instance | HTTP 404 `monitoring instance not found` |
| archive while not retired | HTTP 409 management blocked |
| archive with active non-cancelled/non-archived VPS link | HTTP 409 management blocked |
| restore lifecycle when not retired | HTTP 409 management blocked |
| restore archive when not archived | HTTP 409 management blocked |
| non-empty instance cleanup before archive | HTTP 409 management blocked |
| archived instance metadata/onboarding/runtime/action write | HTTP 409 |
| paused/retired sync with observations/IP quality/action result | accepted sync response with empty plan, no persisted writes |

#### 5. Good/Base/Bad Cases

- Good: 重复创建且没有观测证据的 MonitoringInstance 通过 management review 显示为空误创建候选，用户输入名称和原因后永久清理，VPS link 随实例 cascade 删除。
- Good: 真实运行过的实例先退役再归档；默认列表消失，但详情和归档范围仍可查看历史并可恢复。
- Base: 暂停或退役实例的旧 agent 继续同步；center 验证 token 后返回空 plan，不写入新心跳或 IP 质量报告。
- Bad: 把 `已归档` 塞进 `lifecycle_status`，破坏 VPS lifecycle action 对 `不续费` / `已退役` 的含义。
- Bad: 只在前端隐藏按钮，后端 action / onboarding / sync 写路径仍允许归档实例产生新状态。
- Bad: `ApplyBatch` 先写心跳和 IP 质量报告，再依赖 `BuildSyncPlan` 返回空计划；这会让暂停 / 退役实例继续沉淀新数据。

#### 6. Tests Required

- Migration / scan tests: 新增归档字段默认值、select/scan/JSON 合同。
- Store tests: list scope、review counts/blockers/actions、retire/restore/archive/restore archive、cleanup 空实例、cleanup 非空未归档阻塞、cleanup 删除非 FK 引用。
- Sync tests: paused / retired / archived sync 不写心跳、样本、观测、IP 质量或 action result，并返回空 plan。
- Handler/router/bootstrap tests: 新 endpoint 方法、输入校验、scope 校验、错误码、router subtree 不落到 item handler / SPA fallback、bootstrap nil 断言。
- Gating tests: archived metadata、onboarding/binding、runtime resume、action queue/batch 返回冲突。

#### 7. Wrong vs Correct

```go
// 错误：在 buildSyncPlan 返回空计划前已经写入观测事实。
recordHeartbeatBatch(ctx, tx, id, fingerprint, receivedAt, batch.Heartbeats)
recordIPQualityReports(ctx, tx, newID, batch.IPQualityReports, receivedAt)
plan, _ := buildSyncPlan(ctx, tx, id)
```

```go
// 正确：先读取并锁定实例状态，暂停 / 退役 / 归档时直接返回空 plan。
syncState, err := validateAcceptedSyncBatch(ctx, tx, batch)
if err != nil {
	return syncing.Result{}, err
}
if syncState.SuppressWritesAndPlan() {
	return syncing.Result{AcceptedAt: receivedAt, Plan: agentplan.SyncPlan{ProbeAssignments: []agentplan.ProbeAssignment{}}}, nil
}
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
- VPS 列表查询支持 `AssetScope`：未显式传入时 handler 默认 `current`，排除 `lifecycle_status in ('cancelled','archived')`；`historical` 返回这两个历史不可访问状态；`archived` 是保留给旧客户端的兼容别名，语义与 `historical` 完全相同；`all` 不按生命周期裁剪。显式 `lifecycle_status` 精确筛选优先于 scope，避免旧状态筛选与归档入口互相冲突。
- VPS 是业务状态主体：人工生命周期、用途、续费 / 迁移 / 取消决策只写在 `vps_assets`。Subscription 和 MonitoringInstance 只能提供账单事实与运行观测事实，不得在普通创建 / 编辑流程里要求用户重复选择业务状态。
- VPS create/import 只能创建当前事实：`lifecycle_status` 允许 `active`、`idle`、`testing`；`to_migrate`、`to_cancel`、`cancelled`、`archived` 必须来自 lifecycle action、archive API 或底层 store fixture。不得创建缺少 lifecycle action 审计的历史/流程态资产。
- `ssh_port` 默认为 `22`，数据库约束为 `1..65535`；领域 create 中 `0` 表示省略并默认，patch 中显式 `0` 必须拒绝。
- `archived_at` 是派生字段：生命周期切到 `archived` 时补时间，从 `archived` 切出时清空；API 输入不得任意写入 `archived_at`。
- VPS 资产 CRUD 不得改写 `monitoring_instances.provider`，也不得改变 MonitoringInstance / Target / Agent 的既有语义。
- 普通 VPS CRUD 只维护 VPS 自身账本；跨订阅、MonitoringInstance、Target 的取消 / 退役协调必须通过 `assetlifecycle` 显式 preview + confirm + audit action 完成。
- subscription summary 属于 subscriptions 查询；active monitoring instance link count / monitoring instance summary 由 `assetlinks.Repository` 在 HTTP 展示层补充，不得让 `store/vps_assets.go` 直接耦合 MonitoringInstance 表或 link 表细节。

#### Scenario: VPS lifecycle / usage / renewal matrix

##### 1. Scope / Trigger

- Trigger: 修改 `internal/center/vpsassets/types.go`、`PATCH /api/vps/{vps_id}`、VPS create/import、archive/lifecycle action、或任何会写 `vps_assets.lifecycle_status`、`usage_status`、`renewal_decision` 的路径。
- 目标：防止页面和决策模型读到互相矛盾的 VPS 当前事实，例如“已取消但仍在用”或“迁移流程态但续费决策是取消”。

##### 2. Signatures

- Domain helpers: `ValidateCreateInput(input CreateInput) error`、`ValidateOrdinaryPatchInput(input PatchInput) error`、`ValidateVPSStateCombination(lifecycle, usage, renewal) error`、`ValidateVPSPatchStateCombination(input PatchInput) error`。
- Machine values:
  - `lifecycle_status`: `active|idle|testing|to_migrate|to_cancel|cancelled|archived`
  - `usage_status`: `in_use|idle|standby|testing|unknown`
  - `renewal_decision`: `unreviewed|keep|observe|migrate|cancel|auto_renew_cancelled|replaced`

##### 3. Contracts

- `ValidateCreateInput` must reject `to_migrate`、`to_cancel`、`cancelled`、`archived`; create/import is not an audit-less lifecycle action path.
- Ordinary PATCH remains current-fact only for lifecycle: `active|idle|testing`。流程态/终态只能由 lifecycle action/archive API 或 store-level historical fixtures 写入。
- Full combination hard failures:
  - `cancelled` requires `renewal_decision in (cancel, auto_renew_cancelled)`。
  - `cancelled` cannot pair with `usage_status=in_use`。
  - `to_cancel` requires `renewal_decision in (cancel, auto_renew_cancelled)`。
  - `to_migrate` requires `renewal_decision=migrate`。
  - `renewal_decision=replaced` cannot pair with `lifecycle_status=active` or `usage_status=in_use`。
- Patch delta validation only rejects contradictions among fields present in the same request. It must not infer omitted current values, because archive/lifecycle action paths may call lower-level store helpers after doing their own review.
- Warning-only or transitional readback combinations can remain visible for historical explanation, but new create/import and ordinary PATCH must fail closed for the hard failures above.

##### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| create `lifecycle_status=cancelled` / `archived` / `to_cancel` / `to_migrate` | 400 invalid VPS asset input |
| create `lifecycle_status=active, usage_status=in_use, renewal_decision=keep` | allowed |
| full combination `cancelled + keep` | invalid VPS asset input |
| full combination `cancelled + in_use` | invalid VPS asset input |
| full combination `to_migrate + cancel` | invalid VPS asset input |
| full combination `replaced + active/in_use` | invalid VPS asset input |
| ordinary PATCH `lifecycle_status=to_cancel` | invalid VPS asset input |
| PATCH delta `usage_status=in_use, renewal_decision=replaced` | invalid VPS asset input |

##### 5. Good/Base/Bad Cases

- Good: 新导入 VPS 默认 `active/unknown/unreviewed` 或用户明确填 `idle/idle/observe`，后续再通过决策或 lifecycle action 改状态。
- Base: 旧历史资产在 archive 视图读到 `cancelled/idle/cancel`，作为历史 readback 展示。
- Bad: 导入 JSON 直接写 `cancelled/in_use/keep`，用户在列表看到“已取消但仍在用且保留”的矛盾资产。
- Bad: 普通 PATCH 把 VPS 改成 `to_migrate`，但没有 lifecycle action step 或迁移 workbench 审计。

##### 6. Tests Required

- Domain tests: create lifecycle boundary、full combination hard failures、allowed coherent states、PATCH delta hard failures。
- Handler/store tests: ordinary API create/patch maps invalid matrix to invalid input; archive/lifecycle paths keep their dedicated tests.
- Import tests: dry-run/import reuse `vpsassets.NormalizeCreateInput` + `ValidateCreateInput` and reject workflow/terminal lifecycle creation.

##### 7. Wrong vs Correct

```go
// 错误：create 只检查枚举合法，让流程态直接落库。
if !IsValidLifecycleStatus(input.LifecycleStatus) {
	return ErrInvalidVPSAssetInput
}
```

```go
// 正确：create 先限制当前事实边界，再检查跨字段组合。
if !IsValidCreateLifecycleStatus(input.LifecycleStatus) {
	return ErrInvalidVPSAssetInput
}
if err := ValidateVPSStateCombination(input.LifecycleStatus, input.UsageStatus, input.RenewalDecision); err != nil {
	return err
}
```

### Asset Ledger subscriptions

`db/migrations/0018_add_subscriptions.sql` 添加 `subscriptions`，代表资产层 VPS 订阅账本。它依赖 `vps_assets.vps_id`，但不得反向改写 VPS 资产、Provider、MonitoringInstance、Target 或 Agent 状态。

- `subscriptions.subscription_id` 使用 `ids.New("sub")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时 `on delete cascade`。
- `currency` 使用大写 3 字母代码；领域层负责 trim + uppercase，数据库约束兜底。
- `price` 对齐数据库 `numeric(12, 2)`，领域层必须拒绝负数、超过精度或超过 2 位小数的输入，避免入库四舍五入后与派生字段漂移。
- `monthly_price` 是后端派生字段，按 `price / billing_months` 计算并四舍五入到 4 位小数；create / patch JSON 不接受 `monthly_price`，patch 修改 `price` 或 `billing_months` 时必须重新计算。
- `started_at` 与 `renew_at` 是 nullable `date`：未知日期用 `null`，不要写假日期。
- `status` 使用稳定英文机器值：`active`、`paused`、`cancelled`、`expired`、`unknown`。新用户流程不得把它暴露为必填业务状态；VPS-scoped create 默认只收 price / currency / billing cycle / dates / auto-renew / payment / note 等账单事实，内部可保留 legacy status 作为兼容和历史解释字段。
- 订阅列表查询同样支持 `AssetScope`，通过关联 `vps_assets.lifecycle_status` 裁剪；默认 `current` 排除归档/已取消 VPS 的订阅，`asset_scope=historical` 供只读归档页查看已取消/已归档 VPS 的历史订阅；`asset_scope=archived` 是兼容别名。订阅自身 `status='cancelled'|'expired'` 不能让 VPS 自动进入归档范围，归档边界只能来自 VPS lifecycle。
- `renewal_mode` 允许 `auto`、`manual`、`auto_cancelled`、`lottery`、`gift`、`bonus`、`other`。`lottery` 只表达抽奖，`gift` 只表达赠送；两者都不是 legacy 自动续费标记。`LegacyRenewalFlags(gift)` 与 `LegacyRenewalFlags(lottery)` 必须返回 `false,false`。
- 订阅 CRUD 不得创建 `vps_monitoring_instance_links`、不得改写 `monitoring_instances.provider`、不得增加 Dashboard / import / currency exchange 行为。
- 订阅 CRUD 仍不得反向改写 VPS、MonitoringInstance 或 Target；订阅取消 / 过期后如资产状态不一致，前端必须暴露 lifecycle action 入口，而不是在订阅 PATCH 中隐式停机或退役。
- 受控例外：用户显式在 `PATCH /api/vps/{vps_id}` 将 VPS `renewal_decision` 改成取消类决策（当前为 `cancel` 或 `auto_renew_cancelled`）时，VPS patch 事务可以同步处理该 VPS 的明确订阅事实。只有恰好一条 `status='active'` 的订阅候选时，才能在同一事务里把该订阅 `auto_renew=false`、`auto_renew_cancelled=true`，并按既有 `price_histories` 机制记录自动续费字段变化；无 active 订阅或多 active 订阅时只返回 linkage status/message，不批量写订阅。
- 上述例外仍属于 Asset Ledger 内部 VPS↔Subscription 用户决策流：不得创建或修改 `vps_monitoring_instance_links`、Provider、MonitoringInstance、Target、ProbeItem、Agent 计划或运行时控制；subscription 自己的 CRUD 仍不得反向改写 VPS renewal decision。

#### Scenario: Subscription renewal mode gift and historical scope

##### 1. Scope / Trigger

- Trigger: 修改 `subscriptions.renewal_mode`、`price_histories.from_renewal_mode/to_renewal_mode`、订阅列表 scope、订阅表单选项或续费方式展示标签。
- 目标：让账单行为、抽奖来源、赠送来源和历史资产范围在 DB、Go 和 UI 中保持同义。

##### 2. Signatures

- DB constraints: `subscriptions_renewal_mode_allowed` and `price_histories_renewal_mode_allowed` must include `gift` alongside `auto|manual|auto_cancelled|lottery|bonus|other`。
- Domain constant: `subscriptions.RenewalModeGift = "gift"`。
- List API: `GET /api/subscriptions?asset_scope=current|historical|archived|all`。
- Store behavior: `historical` and legacy `archived` both query related VPS `lifecycle_status in ('cancelled','archived')`。

##### 3. Contracts

- New migrations must not edit old applied migrations. To add a renewal mode, append a migration that drops/re-adds the two allowed constraints.
- `NormalizeRenewalMode` must trim and lowercase `gift` like all other machine values.
- `IsValidRenewalMode("gift")` and renewal history validation must both accept `gift`。
- `RenewalModeFromLegacyFlags` remains only `auto` / `auto_cancelled` / default `manual`; `gift` is not inferable from legacy booleans.
- Existing `lottery` rows stay `lottery` and display as 抽奖; there is no automatic backfill to `gift` without historical evidence.

##### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| subscription create `renewal_mode=gift` | accepted; legacy booleans normalized to false/false |
| price history `from_renewal_mode=gift` or `to_renewal_mode=gift` | accepted |
| renewal mode `抽奖/赠送` or unknown string | invalid subscription input |
| `asset_scope=historical` | same SQL predicate as compatibility `archived` |
| `asset_scope=archived` | still accepted for old clients |

##### 5. Good/Base/Bad Cases

- Good: 用户录入赠送订阅，API 保存 `renewal_mode=gift`，前端显示“赠送”，不勾自动续费。
- Base: 旧抽奖订阅仍是 `lottery`，前端显示“抽奖”。
- Bad: 把 `lottery` 标签写成“抽奖/赠送”，导致用户无法区分权益来源。
- Bad: 只改 `subscriptions` constraint，忘记 `price_histories`，导致修改订阅时历史写入失败。

##### 6. Tests Required

- Migration tests: new migration contains both subscription and price history constraints with `gift`。
- Domain tests: `gift` normalize / validate / create / price history validation / legacy flags。
- Store/handler tests: subscriptions historical scope query parsing and SQL predicate。
- Frontend tests: `RenewalMode` union、option label、normalizer、legacy flags、Archive page historical query。

##### 7. Wrong vs Correct

```sql
-- 错误：只放松当前订阅表，历史表仍不能记录 gift。
alter table subscriptions add constraint subscriptions_renewal_mode_allowed check (renewal_mode in (..., 'gift'));
```

```sql
-- 正确：当前事实与价格历史的续费方式约束一起放松。
alter table subscriptions add constraint subscriptions_renewal_mode_allowed check (...);
alter table price_histories add constraint price_histories_renewal_mode_allowed check (...);
```

### Asset lifecycle actions

`assetlifecycle` 是唯一允许跨 Subscription、VPS、MonitoringInstance、Target/实例做取消或退役联动的领域服务。它不是普通 CRUD 的旁路，而是一个显式的 lifecycle action 工作流：先预览影响范围，再由用户确认要执行的步骤，最后以审计记录落库。

- 后端 API：
  - `GET /api/vps/{vps_id}/cancellation-preview` 从 VPS 出发返回 VPS 当前生命周期、所有关联订阅候选（包括 active、expired、cancelled、paused、unknown/latest）、活跃 `vps_monitoring_instance_links`、通过 asset service / domain 关联的 Target、推荐步骤、风险提示和阻塞项。
  - `POST /api/vps/{vps_id}/cancellation` 接受用户显式选择的 `subscription_ids`、`vps_lifecycle_status`、`monitoring_instance_actions`、`target_actions`、`reason`、`effective_date`，在一个事务内写入状态变化与审计步骤。
  - `GET /api/asset-context/targets` 是 Target 批量上下文接口，供 Target 列表 / 详情显示关联 VPS 的取消 / 过期 / 不一致状态，避免前端逐行请求。Monitoring 列表不再暴露批量 asset-context 接口；Monitoring 详情使用 `/api/monitoring-instances/{id}/vps` 返回所属 VPS。
- 审计表：`asset_lifecycle_actions` 保存一次操作的发起对象、确认时间、原因、执行摘要和最终状态；`asset_lifecycle_action_steps` 保存每个 subscription / VPS / MonitoringInstance / Target 步骤的前后状态、状态码、错误和摘要。
- 普通 CRUD 不得静默调用 lifecycle action；只有工作台或等价的显式确认入口可以调用 `POST /api/vps/{vps_id}/cancellation`。
- 如果 VPS 没有 active subscription，但存在 expired/cancelled/paused/unknown subscription，preview 和旧续费联动提示必须说明“订阅账单记录已无续费动作，仍需处理 VPS、MonitoringInstance 与入口探测状态”，不得误导为“没有关联订阅，需要创建订阅”。
- 默认语义：已过期且不续费的 VPS 写 `renewal_decision=cancel`、`lifecycle_status=cancelled`；未来到期但已决定不续费的 VPS 写 `renewal_decision=cancel`、`lifecycle_status=to_cancel`；未来取消但仍观察的 MonitoringInstance 用 `lifecycle_status='不续费'` 且监控保持启用；实际退役 MonitoringInstance 用 `lifecycle_status='已退役'` 并可按确认步骤暂停监控；随 VPS 下线的 Target/实例确认后用 `run_status='已归档'`，临时停用才用 `暂停`。
- `vps_monitoring_instance_links` 默认保留为历史证据；取消 / 退役 action 不自动 unlink，除非未来新增单独的“解除错误关联”显式动作。
- 执行事务必须先锁定 VPS，再写 action 与各步骤；任何一步失败时业务状态与步骤写入整体回滚，避免部分取消造成新割裂。失败审计是例外：必须先显式回滚业务事务，再用独立事务写入 `status='failed'` 的 action 和 failed step，避免失败记录随业务回滚消失，也避免复用同一 `action_id` 时被未回滚事务锁住。
- preview 的 blocker 必须在 POST 执行路径重新校验；例如 `lifecycle_status='archived'` 的 VPS 不允许通过 cancellation POST 改回 cancelled/to_cancel，handler 应返回冲突而不是清空 `archived_at`。
- VPS 归档 / 恢复必须走受控 archive API：`GET /api/vps/{vps_id}/archive-review` 返回 VPS、订阅、MonitoringInstance、服务、域名、Target、warnings/blockers/eligible；`POST /api/vps/{vps_id}/archive` 在事务中锁定 VPS、重新计算 review、校验 `confirmation_name` 与 blockers 后才写 `lifecycle_status='archived'`；`POST /api/vps/{vps_id}/restore-from-archive` 只允许 `archived -> idle`。普通 `PATCH /api/vps/{vps_id}` 不得写入 `archived`，也不得从 `archived` 恢复。
- archive blockers 至少包括：任一关联订阅仍为 `active`；任一关联 MonitoringInstance lifecycle 非 `不续费` / `已退役` 或 monitoring status 非 `暂停`；任一关联 Target 非 `暂停` / `已归档`。这些 blockers 必须在 archive POST 内重新计算，前端 review 只能作为提示，不能作为权限来源。
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
- VPS 普通资料 PATCH 只能维护低风险事实 lifecycle：`active`、`idle`、`testing`。`to_cancel`、`cancelled`、`to_migrate`、`archived` 是受控流程/终态，不得通过普通 PATCH 写入；取消/退役走 lifecycle workbench 或专用 endpoint，归档走 archive endpoint，迁移在完整 workbench 存在前只能作为 `renewal_decision=migrate` 的人工意向。
- active link 口径：`vps_monitoring_instance_links.unlinked_at is null`。
- 异常关联 VPS 口径：active link 关联到 `monitoring_instances.current_health_status <> '正常'` 的 MonitoringInstance；只读 MonitoringInstance 派生状态，不改写 MonitoringInstance。
- 成本口径：`sum(active subscriptions monthly_price)` 按 `currency` 分组，`yearly_total = monthly_total * 12`；第一阶段不做汇率换算。
- 取消联动口径：Dashboard 只处理 current VPS，最终 `cancelled` / `archived` 不进入 `asset_summary`；`to_cancel_vps_count` 统计仍需处理的 `lifecycle_status='to_cancel'`，`cancelled_vps_count` 当前为 0 兼容字段；`cancellation_attention_vps_count` 统计 current VPS 中订阅非活跃但 lifecycle 未进入 `to_cancel`、`to_cancel` 但订阅仍 active、`to_cancel` 但 MonitoringInstance/Target 仍运行，或取消类续费决策与 lifecycle 未对齐的 VPS；`running_cancelled_asset_count` 只统计 `to_cancel` VPS 下仍运行的 active MonitoringInstance link 与未归档/未暂停 Target。
- 该查询不得改变 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent、VPS、subscription 或 link 记录。
- `limit` 只限制异常队列和 recent events；不得限制 `asset_summary`。

### Scenario: Asset decision portfolio read model and memory layer

#### 1. Scope / Trigger

- Trigger: 修改 `internal/center/assetdecisions/`、`internal/center/store/asset_decisions.go`、`/api/asset-decisions/*`、`db/migrations/*asset_decision*`，或任何依赖 VPS / Subscription / Service / Domain / MonitoringInstance / Target 聚合生成组合决策组和决策记录的逻辑。
- 目标：`/asset-decisions` 是资产组合决策中枢。自动组仍是只读派生 read model；手工组合是用户定义的 scenario layer，只保存场景、成员意图和备注；决策记录是独立 memory layer，只保存用户判断和证据快照。三层都不成为第二套 VPS / Subscription / MonitoringInstance / Target 状态机。

#### 2. Signatures

- Domain package: `internal/center/assetdecisions`，包含 `Repository`、`ListFilters`、`Overview`、`GroupSummary`、`GroupDetail`、`GroupMember`、`ManualGroupSummary`、`ManualGroupDetail`、`ManualGroupMember`、`ScenarioTemplateSummary`、`ScenarioTemplateDetail`、`ScenarioTemplateMember`、`RecordSummary`、`RecordDetail`、`RecordMember`、`CreateRecordInput`、`PatchRecordInput`、`ErrAssetDecisionGroupNotFound`、`ErrAssetDecisionManualGroupNotFound`、`ErrAssetDecisionScenarioTemplateNotFound`、`ErrAssetDecisionRecordNotFound`、`ErrInvalidAssetDecisionInput`。
- Backend APIs:
  - `GET /api/asset-decisions/overview?view=&renew_within_days=&provider_id=&vps_id=&country=&region=&city=&scenario=`
  - `GET /api/asset-decisions/groups?view=&renew_within_days=&provider_id=&vps_id=&country=&region=&city=&scenario=`
  - `GET /api/asset-decisions/groups/{group_id}?renew_within_days=`
  - `GET /api/asset-decisions/records`
  - `POST /api/asset-decisions/records`
  - `GET /api/asset-decisions/records/{record_id}`
  - `PATCH /api/asset-decisions/records/{record_id}`
  - `GET /api/asset-decisions/manual-groups?view=&renew_within_days=&provider_id=&vps_id=&country=&region=&city=&scenario=`
  - `POST /api/asset-decisions/manual-groups`
  - `GET /api/asset-decisions/manual-groups/{manual_group_id}`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}`
  - `POST /api/asset-decisions/manual-groups/{manual_group_id}/members`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
  - `DELETE /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
  - `GET /api/asset-decisions/scenario-templates`
  - `POST /api/asset-decisions/scenario-templates`
  - `GET /api/asset-decisions/scenario-templates/{template_id}`
  - `PATCH /api/asset-decisions/scenario-templates/{template_id}`
  - `POST /api/asset-decisions/scenario-templates/{template_id}/manual-groups`
- Store source tables: `vps_assets`、`providers`、`subscriptions`、`asset_services`、`asset_domains`、`vps_monitoring_instance_links`、`monitoring_instances`、`targets`。
- Manual scenario tables: `asset_decision_manual_groups`、`asset_decision_manual_group_members`。manual group id 使用 `admg_*`；成员引用现有 `vps_assets.vps_id`，只保存 `intended_role`、`intended_action`、`reason`、`note`、`sort_order` 和创建时 evidence snapshot。
- Scenario template tables: `asset_decision_scenario_templates`、`asset_decision_scenario_template_members`。内置模板使用确定性 ID `adt_builtin_<scenario>` 且由代码返回，不允许 PATCH；自定义模板使用 `adt_*`，只保存场景 blueprint（status、scenario、title、goal、note、source_manual_group_id 和可选成员 intended role/action/reason/note/sort_order），不得保存当前成本、订阅、监控、服务、域名或 Target 实时事实。
- Decision memory tables: `asset_decision_records`、`asset_decision_record_members`；view: `asset_decision_records_with_counts`。`source_type` 允许 `auto_group` 与 `manual_group`；未传 source type 时默认 `auto_group` 以兼容旧调用。
- Stable group id: `adg_auto_<12hex>`，由 group type、scope key 和续费窗口等只读 key 确定性派生；detail endpoint 每次重新计算组列表后按 ID 查找。
- Decision record id: `adr_*`，由 `ids.New("adr")` 生成；记录只引用来源自动组 ID 作为历史来源，不把自动组 ID 当长期外键。
- Evidence assessment: `GroupSummary` 和 `GroupMember` 必须返回 `evidence_assessment`，字段固定为 `confidence_score`、`pressure_score`、`readiness_score`、`quality_tier`（`strong|usable|weak|blocked`）、`decision_bias`（`keep|observe|complete_evidence|retire|migrate|review`）、`support_signal_count`、`risk_signal_count`、`gap_signal_count`、`summary`。
- Decision recommendation: `GroupSummary`、`GroupMember`、`ManualGroupSummary`、`ManualGroupDetail` 和 manual members 必须返回只读 `decision_recommendation`，字段固定为 `summary`、`next_step`、`reasons[]`、`blockers[]`、`priority_vps_ids[]`、`confidence_label`。它只能解释 `evidence_assessment`、evidence chips、group type、scenario 和已有成员事实计数，不得新增评分引擎、runtime facts detail、HostSample、ProbeObservation、IP/路由/性能/超售判断。
- Comparison insight: `GroupSummary`、`GroupMember`、`ManualGroupSummary`、`ManualGroupDetail` 和 manual members 必须返回只读 `comparison_insight`，用于解释同组成员差异，不是新的执行层。组级字段固定为 `summary`、`primary_axis`（`renewal|cost|service_context|monitoring|evidence|lifecycle|review`）、`lane_counts[]`、`priority_vps_ids[]`、`tradeoffs[]`；成员级字段固定为 `rank`、`lane`（`primary|standby|observe|retire|evidence|review`）、`summary`、`strengths[]`、`risks[]`、`gaps[]`、`tradeoffs[]`。`tradeoffs/strengths/risks/gaps` item 使用 `kind,label,tone,details?`。
- Record member follow-up: `asset_decision_record_members.followup_status` 固定为 `todo|in_progress|blocked|done|skipped`，`followup_note` 为 trim 后的执行备注，`followup_updated_at` 为最后一次成员跟进更新时间；`asset_decision_records_with_counts` 必须返回各状态聚合计数。
- Execution readback: `RecordSummary` / `RecordDetail` 和 `RecordMember` 必须返回只读派生字段 `execution_readback`。记录级字段为 `status`（`open|aligned|drift|blocked|needs_evidence|inactive`）、中文 `summary`、`open_count`、`aligned_count`、`drift_count`、`blocked_count`、`needs_evidence_count`。成员级字段为同一 status、summary、`issues[]`（`kind,label,tone,details?`）和 `current_facts`（当前 VPS lifecycle、usage、renewal decision、active subscription / service / domain / Target / monitoring 计数与 source availability）。
- Execution plan: records API 响应必须在 readback 之后同步返回只读派生字段 `execution_plan`，但不新增 endpoint / migration。记录级字段为中文 `summary`、`lane_counts[]`、`actionable_count`、`blocked_count`；成员级字段为 `lane`（`cancel_retire|migration|keep_observe|evidence|review`）、`step_kind`（`open_cancellation_workbench|open_vps_detail|open_subscription_context|review_record`）、`tone`（`critical|alert|notice|normal|neutral`）、中文 `summary`、`step_label`、`issue_count`、`blocked`、`actionable`。
- Execution plan 只能消费当前 `execution_readback` 与 `loadFacts` 已有事实，不能引入第二套执行状态机。后端只返回 step kind 等语义，不得返回 SPA 路由字符串；URL 深链由前端根据 step kind 本地映射。

#### 3. Contracts

- 自动组只读派生，不写数据库；手工组合只写 `asset_decision_manual_groups` / `asset_decision_manual_group_members`；用户保存一次判断才写入 `asset_decision_records` / `asset_decision_record_members`。
- 场景模板只能创建或预填自定义组合，不能直接创建决策记录，不能修改 VPS / Subscription / MonitoringInstance / Target / Service / Domain。`POST /scenario-templates/{id}/manual-groups` 必须重新读取当前 facts 后复用 manual group 创建路径；模板成员缺失、重复或非法输入必须 fail closed。
- 手工组合支持 `source_type=manual` 和 `source_type=auto_group`。从自动组创建手工组合时，store 必须重新读取当前 facts 并定位自动组，复制当前成员建议角色/动作与 evidence snapshot；自动组不存在或请求成员不属于组时返回 invalid/not found，不得信任前端传入的成员事实。
- 手工组合详情和列表必须复用当前 `loadFacts` 聚合实时回读成员事实；成员当前 VPS facts 缺失时仍返回 manual metadata，并展示 `current_fact_missing` evidence chip，不得静默丢成员。
- 手工组合成员增删改只能修改 manual member 行，不得修改 VPS、Subscription、MonitoringInstance、Target、Service、Domain 或决策记录跟进状态。手工组合没有 hard delete endpoint；归档使用 `status=archived`。
- 创建决策记录时必须重新读取当前事实。`source_type=auto_group` 通过 `FindGroup` 定位 `source_group_id`；`source_type=manual_group` 通过 manual group detail 生成 group/member snapshot 并使用成员 intended role/action/reason 作为决定默认值。来源不存在返回 404，不得按前端传入的成员列表凭空创建记录。
- 决策记录必须保存组级来源字段、标题、目标、状态、组级 `evidence_snapshot`，并为组内每台 VPS 保存系统建议角色/动作、用户决定角色/动作、成员理由和成员级 `evidence_snapshot`。
- 决策记录状态固定为 `draft`、`decided`、`in_progress`、`completed`、`abandoned`；PATCH 可更新记录级标题、目标、状态，以及记录内已有成员的 `followup_status` / `followup_note`，但不执行 VPS / Subscription / MonitoringInstance / Target 业务动作。
- 成员跟进 PATCH 的 payload 为 `members:[{vps_id, followup_status?, followup_note?}]`；`vps_id` 必须属于当前记录，同一 payload 不得重复，状态必须合法，状态或备注至少设置一项。成功更新成员跟进时必须刷新成员 `followup_updated_at`、成员 `updated_at` 与记录 `updated_at`，并返回 detail 风格的最新记录。
- 成员全部 `done` / `skipped` 不得自动推进整条决策记录状态；组合决策记录状态仍由用户显式修改，避免在 memory layer 内扩张隐式状态机。
- 执行回读只校验“保存的组合判断是否与当前事实一致”，不得变成第二套状态机：records API 不自动 PATCH record status，不自动完成成员跟进，不自动修改 VPS / Subscription / MonitoringInstance / Target。
- 执行编排只把已保存判断组织为下一步导览，不执行真实动作：records API 不自动 PATCH VPS / Subscription / MonitoringInstance / Target，不自动 PATCH record status，不自动改写成员 `decided_action` / `decided_role`。若用户判断需要改写，路径是 abandon 旧记录后从自定义组合或自动组保存新记录。
- 成员回读以 `decided_action` 为主，历史值为空才回退 `suggested_action`。`cancel` / `open_cancellation_workbench` 只判断 VPS 是否进入 `to_cancel|cancelled|archived` 且无 active subscription、无 running monitoring、无 running target；`migrate` 只判断是否进入迁移链路（`renewal_decision=migrate|replaced` 或 `lifecycle_status=to_migrate`），不判断新 VPS 是否已替代旧 VPS；`keep` / `observe` 只检查 lifecycle 未取消/归档和 renewal decision 是否相符；`complete_evidence` 只检查当前已有证据缺口。
- 回读状态优先级：`record.status=abandoned` 为 `inactive`；成员 `followup_status=blocked` 优先 `blocked`，但 `done` 后关键事实不一致仍为 `drift`；`skipped` 抑制普通 open，但不隐藏关键 drift；存在证据缺口为 `needs_evidence`；事实与动作一致为 `aligned`。记录级聚合优先级为 drift > blocked > needs_evidence > aligned > open。
- 成员级 `decided_action=cancel` 或 `open_cancellation_workbench` 只能给前端提供跳转到 VPS lifecycle workbench 的入口；后端 records API 不做批量取消、批量退役或批量迁移。
- 成员级 execution plan 的 cancel / retire lane 只能编排到 `open_cancellation_workbench`；migration lane 只能编排到 VPS detail 复核迁移意向并人工跟进，`step_label` 不得写成“推进迁移”或暗示已有迁移工作台；evidence lane 对缺订阅优先 `open_subscription_context`，其余证据缺口走 VPS detail；`current_fact_missing`、空动作或不能安全归类的成员必须走 `review_record`。
- Group type 固定语义：`renewal_attention`、`cancellation_attention`、`region_portfolio`、`provider_portfolio`、`cost_pressure`、`evidence_gap`。
- `renew_within_days` 默认 30，仅允许产品认可的窗口（当前 `30/60/90`）；非法值在 handler 返回 400。
- `view` 只筛选返回的自动组，不改变底层事实读取；`provider_id`、`vps_id`、`country`、`region`、`city`、`scenario` 是列表上下文筛选，只筛出相关组/手工组合/记录，不裁剪 group detail 成员；非法值返回 400。
- Store 读取现有表后在 Go 中派生组合摘要和成员建议，避免 Dashboard / VPS / Subscription / Provider 页面各自重复 join 后语义漂移。
- 组级摘要可以聚合成本、续费窗口、取消联动、服务 / 域名 / Target、监控关联、异常和 evidence chips；成员级建议角色 / 建议动作只能作为扫描提示，不执行写操作。
- `evidence_assessment` 是只读、可解释评分层，只消费当前 `GroupMember` / `GroupSummary` 已有事实、source availability 和 evidence chips；它不得新增数据库读取、逐台 runtime facts 调用或执行语义，也不得把评分当成自动 keep / migrate / cancel 写入。
- `comparison_insight` 是只读、可解释的组合对比层，只消费当前 `evidence_assessment`、`decision_recommendation`、evidence chips、组类型、成员建议角色/动作和已有成员事实计数；它不得新增独立 scoring engine、不得读取新增表、不得逐台调用 runtime facts detail，也不得使用 IP 质量、路由质量、性能衰退、CPU/IO、超售或 HostSample / ProbeObservation 语义。成员 lane / rank 必须稳定可测，用于 UI 的证据矩阵和优先核对顺序，不可触发写操作。
- 自定义组合详情必须对所有 manual members 返回 comparison insight。成员当前 facts 缺失时不得静默丢成员，必须保留 manual metadata，并生成 `current_fact_missing` evidence / comparison gap，使 UI 能解释“组合成员仍在，但当前事实不可回读”。
- 证据源不可用时只能降低 `confidence_score` / `readiness_score` 并增加 gap 计数；不得把 `subscription_unavailable`、Monitoring/Target/Service/Domain 查询失败解释为真实 `missing_subscription` / `missing_monitoring` 业务事实。
- 决策记录回读必须 fail closed：当前事实查询失败时 records list/detail/create/patch 返回 repository error，不得把未知事实伪造成 `aligned`、`needs_evidence` 或 `drift`。成员存在但当前 facts 中找不到对应 VPS 时，成员 readback 为 `drift` 且 issue kind 为 `current_fact_missing`。
- `RecordSnapshotFromGroup` 与 `RecordSnapshotFromMember` 必须把当时的 `evidence_assessment` 与 `comparison_insight` 写入 `evidence_snapshot`，用于记录详情回看保存时的判断基础；旧记录缺少这些字段时前端必须可降级显示，后端不要求 backfill。
- archived VPS 不进入普通 region/provider/cost/evidence 组合；cancelled/to_cancel 只能作为取消联动相关证据出现，避免归档资产污染正常组合比较。
- 订阅、服务、域名、监控或 Target 查询失败必须返回 repository error；不得构造“健康”或“缺证据”假结果。只有查询成功且事实为空时，才生成 `missing_subscription`、`unlinked_monitoring` 等真实 evidence gap。
- `/api/asset-decisions/*` 不逐台调用 runtime facts detail endpoint，只读 MonitoringInstance / Target 当前摘要字段和关联计数；CPU / IO / 路由 / IP 质量 / 超售判断属于后续能力。
- 执行回读同样只能复用 `loadFacts` 聚合事实，不得逐台请求 runtime facts detail、HostSample、ProbeObservation、agent 性能趋势、IP 质量或路由质量。IP / 路由 / 性能衰退 / CPU / IO / 超售判断等待 agent 与观测语义成熟后再进入模型。
- 执行编排同样只能复用 readback / `loadFacts` 聚合事实，不得为了生成下一步导览逐台请求 runtime facts detail、HostSample、ProbeObservation、agent 性能趋势、IP 质量、路由质量或性能衰退信号。
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
| missing scenario template | handler 返回 404 `asset decision scenario template not found` |
| patch builtin scenario template | handler 返回 400 `invalid input`，内置模板由代码版本化 |
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
| record status `completed` but facts drift | `execution_readback.status=drift` 且 `execution_plan.actionable_count>0`，不得因 completed 掩盖漂移 |
| record status `abandoned` | `execution_readback.status=inactive`，`execution_plan` 不产生可执行项 |
| member has `current_fact_missing` | 成员 plan 使用 `lane=review` + `step_kind=review_record`，不得跳到业务执行页 |
| record member VPS missing from current facts | 成员 `drift`，issue kind 为 `current_fact_missing` |
| unsupported method | handler 返回 405 `method not allowed` |
| `/api/asset-decisions/*` route missing | router test 必须失败；该路径不得落 SPA fallback |

#### 5. Good/Base/Bad Cases

- Good: 同一国家 / region / city 下两台 active VPS 自动形成 `region_portfolio`，成员显示成本、用途、服务 / 域名 / 监控差异，帮助用户取舍。
- Good: Provider 下多台 active VPS 自动形成 `provider_portfolio`，用于服务商组合比较。
- Good: subscription query 成功且某台 VPS 没有 active subscription，`evidence_gap` 或成员 evidence chips 标记缺订阅。
- Good: 用户打开自动组，保存为 `adr_*` 决策记录；记录保留当时成本、服务/域名/Target、监控和成员建议快照，后续只推进记录状态。
- Good: 用户把自动组保存为 `admg_*` 手工组合，随后调整成员 intended role/action/reason，再从手工组合保存为 `source_type=manual_group` 的 `adr_*` 记录。
- Good: 用户从内置场景模板创建自定义组合，后端重新读取当前 facts；用户把手工组合另存为自定义模板时只保存成员 blueprint，不保存当前成本、监控或订阅事实。
- Good: `/api/asset-decisions/groups?view=provider&provider_id=pv_001` 只返回与该服务商相关的组合；打开其中某个 `group_id` 的详情仍保留完整组成员，不按 provider/vps 筛选裁剪。
- Good: 用户给手工组合新增一台现有 VPS，只保存手工组合成员行；VPS 的 lifecycle、renewal decision、订阅、监控和 Target 均不被修改。
- Good: 用户把记录中某台 VPS 标记为 `blocked` 并记录“等待迁移窗口”，API 只更新该 record member 的跟进字段与记录 `updated_at`，不修改 VPS lifecycle 或 subscription。
- Good: 已保存记录中 `cancel` 成员跟进标记 `done` 后，如果当前仍有 active subscription 或 running target，readback 显示 `drift`，提示“跟进已完成但事实未闭环”。
- Good: 已保存记录的成员 facts 找不到对应 VPS 时，readback 显示 `current_fact_missing` 而不是伪造已对齐。
- Good: 已保存记录的 drift / blocked / needs_evidence 成员返回 execution plan，前端据此打开记录详情、VPS 详情、订阅上下文或取消工作台；后端响应仍只包含语义 step kind。
- Good: 完整证据的同区组合返回较高可信度/准备度与 `quality_tier=strong`，资料缺口或来源不可用返回较低可信度与 `decision_bias=complete_evidence`。
- Base: 没有任何 VPS 时 overview 仍返回 0 计数和空 `top_groups`。
- Bad: 在 store 里写入 `asset_decision_groups` 表，或把自动组 ID 当长期外键依赖。
- Bad: 手工组合成员保存当前成本、订阅、监控、服务等实时事实并长期展示，不再从 `loadFacts` 回读当前状态。
- Bad: 模板 API 直接生成 `adr_*` 决策记录，或把模板当作第二套业务状态机保存执行状态。
- Bad: `decision_recommendation` 读取 agent CPU/IO、HostSample、ProbeObservation、IP 质量、路由质量或性能衰退数据。
- Bad: `PATCH /api/asset-decisions/records/{id}` 同时修改 VPS renewal decision、Subscription 状态或执行取消/退役。
- Bad: records list 为了给每条记录计算 readback 逐条调用 `GetRecord`，造成 N+1。
- Bad: records list 为了给每条记录计算 execution plan 逐条调用 `GetRecord`，造成 N+1。
- Bad: 后端 `execution_plan` 返回 `/vps/{id}`、`/subscriptions?...` 等 SPA URL 字符串，把 API contract 与前端路由耦合。
- Bad: readback 使用 HostSample、ProbeObservation、IP 质量、路由质量或性能衰退数据，在 agent 语义未成熟前给出超售判断。
- Bad: group detail 为了展示性能趋势逐台请求 runtime facts detail endpoint，造成 N+1 和语义越界。
- Bad: subscriptions 查询失败后把所有 VPS 标记为 `missing_subscription`，误导用户取消资产。

#### 6. Tests Required

- Domain tests: stable group id、view/window validation、record input validation、snapshot builder、renewal/cancellation/region/provider/cost/evidence group derivation、archived/cancelled 边界、source unavailable 不误报。
- Domain assessment tests: 完整证据、资料缺口、证据源不可用、取消联动 / 预算压力、record snapshot 均断言 `evidence_assessment` 的 tier / bias / score 方向。
- Store tests: member facts 聚合、主订阅选择、服务 / 域名 / Target / 监控计数、成本和 evidence chips，manual groups list/create/get/patch/member add/patch/delete、records list/create/get/patch、成员跟进计数、成员跟进事务更新与未知成员回滚，且不依赖 runtime facts detail。
- Execution readback domain tests: cancel / cancellation workbench aligned/open/drift、migrate 链路与旧承载 drift、keep / observe 一致性、complete_evidence 只检查当前已有缺口、done drift、blocked 优先、skipped 抑制普通 open、abandoned inactive、current fact missing。
- Store tests: records list/detail/create/patch 均返回 `execution_readback`；ListRecords 批量读取成员并聚合，不逐条调用 `GetRecord`；facts 查询失败 fail closed；成员跟进 PATCH 后 readback 随响应刷新；不依赖 runtime facts detail / HostSample / ProbeObservation。
- Store tests: records list/detail/create/patch 均返回 `execution_plan`；plan 派生沿用 records/facts/members 的批量读取路径，不逐条调用 `GetRecord`；成员跟进 PATCH 后 readback 与 execution plan 同步刷新。
- Handler tests: overview、groups list、group detail、manual groups list/create/detail/patch/member add/patch/delete、records list/create/detail/patch success 且 records 响应包含 readback、成员跟进 patch；invalid query/input、missing group/manual group/member/record、未知或重复成员、repo failure、method not allowed。
- Handler tests: scenario templates list/create/get/patch/create-manual-group success；builtin PATCH、missing template、invalid template input、repo failure、method not allowed。
- Router/bootstrap tests: `/api/asset-decisions/overview`、`/api/asset-decisions/groups`、`/api/asset-decisions/groups/{id}`、`/api/asset-decisions/manual-groups/*`、`/api/asset-decisions/scenario-templates/*`、`/api/asset-decisions/records`、`/api/asset-decisions/records/{id}` 登录保护且不落 SPA fallback；`bootstrapCenter` wiring 非 nil。
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

> 来源：当前代码、`docs/design/current/product-and-architecture.md`、历史架构背景与 `CLAUDE.md` "Key model invariants"。**任何 SQL / 仓库 / 服务改动都必须先验证这些不变量没被破坏**。

1. **MonitoringInstance = agent 接入后的运行观测对象**。同一台机重装系统后可保持同一个 MonitoringInstance（保留 `monitoring_instance_id` 与历史时间序列）；换了硬件或明确的新 agent identity 应新建 MonitoringInstance，不要在旧 `monitoring_instance_id` 上重新绑定异种主机。指纹变化通过 `binding_status = '指纹变更待确认'` 进入 `pending_binding_*` 字段（见 `monitoring_instances` 表与 `internal/center/enrollment/`）。
2. **Target = 一个可观测入口**，地址 (`host` / `base_port`) 属于 Target；`ProbeItem` 仅描述**如何观测**它（探针种类、频率档、超时、配置），不再额外存地址。Target 与 ProbeItem 是 1:N，删除 Target 级联清理 ProbeItem (`on delete cascade`)。
3. **探针种类只有 `tcp` / `http` / `tls`**（`internal/contracts/agentapi/types.go` 中的 `ProbeKind*` 常量）。`https` 不是独立种类，而是带 TLS 配置的 HTTP 观测。新增种类必须先获得基线批准，并同步更新设计文档与契约包。
4. **健康状态 (`current_health_status`) 是派生量**（`正常 / 关注 / 告警 / 严重`），由 incident service 在写后计算并回写；**不要直接接受外部 API 的健康字段写入**。
5. **MonitoringInstance 生命周期状态 (`lifecycle_status`) 是 VPS 附属接入/收尾事实，不是独立业务状态入口**（`待接入 / 在用 / 观察中 / 不续费 / 已退役`）。普通监控 handler 只能处理运行控制、接入、绑定和 metadata；退役/不续费类变更只能从 VPS 生命周期工作台的 `asset_lifecycle` 联动路径写入，并记录审计步骤。其他写路径不应触碰该列。
6. **维护模式 (`monitoring_status = '维护中'` / `'暂停'`) 是 runtime control，不是健康状态**。维护期间观测照常落库（`maintenance_context = true`），但 incident / notification 处理需识别该上下文（参考 `store/monitoring_instances.go:74-77`、`incidents/service.go`）。暂停、维护、退役或归档 MonitoringInstance 不应保留当前 active incident 投影；incident service 必须把已有 active incidents 行政恢复为 recovered events，且不得发送恢复通知。
7. **请求路径只写原始观测**：handler 接收 sync batch 后通过 `internal/center/syncing/` 落 `monitoring_instance_heartbeats` / `host_samples` / `probe_observations`，**不在请求路径里跑 incident 判定 / 通知**。incident 与通知由 `incidentSvc`（`incidents.NewSettingsBackedService`，启动时作为 `Worker.Run(ctx)` 跑）异步产出。
8. **回填观测 (`is_backfilled = true`) 必须落库但不得触发实时告警**。请求路径仍旧 `insert`（参见 `store/sync_batches.go:188`），但 incident service 在 select 阶段对历史数据的处理需带条件分支。**不要在 incident 判定里忽略 `is_backfilled` 字段，也不要在写路径里干脆丢弃这条数据**。
9. **notification_records.channel 是真实发送通道，不是 evaluator 默认值**。`incidents.NotificationChannel` 当前只允许 `telegram` / `feishu` 作为生产通道语义；Feishu-only 发送只写 `channel='feishu'`，Telegram+Feishu 混合发送必须按 channel 写多条 record，单个 channel 失败只能把该 channel 标为 `failed`。通知策略关闭、维护/回填抑制或无可用 channel 时写 `suppressed`，但不能把 Feishu-only 或 mixed delivery 误记成 Telegram-only。

### Scenario: Administrative incident recovery for inactive objects

#### 1. Scope / Trigger

- Trigger: 修改 `internal/center/incidents/service.go`、MonitoringInstance `monitoring_status/lifecycle_status/archived_at` 语义、Target `run_status` 语义、或 active incident mutation / notification 写入。
- 目标：用户主动暂停、维护、退役或归档的对象不再在页面上表现为“当前 active 风险”，但仍保留一条 recovered event 解释历史收敛。

#### 2. Signatures

- Service paths: `EvaluateStaleMonitoringInstances(ctx, now)`、`AfterSuccessfulSync(ctx, batch, result)`、`EvaluatePeriodicState(ctx, now)`。
- Repositories:
  - MonitoringInstance repo must provide current record for MI evaluation.
  - Target repo may implement optional `GetTarget(ctx, targetID)` so touched target sync can re-check current run status before evaluating observations.
- Mutation: `IncidentMutation{ObjectType, ObjectID, Active: []IncidentRecord{}, Events: []StateChangeEventRecord{EventType: recovered}}`。

#### 3. Contracts

- MonitoringInstance inactive states for incident recovery: `monitoring_status in ('暂停','维护中')`、`lifecycle_status='已退役'`、or `archived_at is not null`。
- Target inactive states for incident recovery: `run_status in ('暂停','已归档')`。
- Periodic stale sweep must close existing active incidents for inactive MonitoringInstances instead of silently skipping them.
- `AfterSuccessfulSync` must recover inactive MonitoringInstance incidents before host metric evaluation, so old samples cannot keep disk/resource incidents active after an administrative stop.
- Periodic Target sweep must recover inactive Target incidents and skip probe/TLS/trend evaluation.
- If a touched Target can be loaded and is inactive, `AfterSuccessfulSync` must recover prior target incidents and skip new evaluation for that target. If the repository cannot load the Target or returns not found, legacy observation-only evaluation may continue for compatibility.
- Administrative recovery writes recovered events but intentionally does not call notification append/dispatch. User-initiated stop should not generate a recovery notification storm.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| paused / maintenance / retired / archived MI has prior active incident | mutation active is empty; recovered event written; no notification records |
| inactive MI has no prior active incident | no mutation required |
| active MI stale heartbeat | normal heartbeat evaluation still applies |
| paused / archived Target has prior active incident | target mutation active is empty; recovered event written; no notification records |
| touched paused Target has fresh failing observations | administrative recovery wins; no new active probe incident |
| Target getter returns not found | fallback to existing observation evaluation |

#### 5. Good/Base/Bad Cases

- Good: 用户暂停监控实例后，旧 heartbeat/disk active incident 被恢复为“按暂停状态收敛”，当前异常列表清空。
- Good: 用户归档 Target 后，旧 TLS/probe active incident 被恢复，事件流保留收敛说明。
- Base: 正常运行对象继续按 stale threshold、probe failure 和 TLS expiry 生成/恢复 incidents。
- Bad: stale sweep 对暂停对象直接 `continue`，旧 active incident 永远挂在 Dashboard 上。
- Bad: 行政恢复调用通知派发，用户暂停一批对象后收到大量“恢复”消息。

#### 6. Tests Required

- Service tests: MI periodic inactive recovery, MI `AfterSuccessfulSync` inactive recovery before metric evaluation, Target periodic inactive recovery, touched Target inactive recovery, all assert no notification records/sends.
- Regression tests: active MI/Target still evaluate normally; `ErrTargetNotFound` fallback remains compatible.

#### 7. Wrong vs Correct

```go
// 错误：非运行态直接跳过，旧 active_incidents 投影仍留在当前风险列表。
if !shouldEvaluate(record) {
	continue
}
```

```go
// 正确：非运行态先做行政恢复，再跳过实时评估。
if !shouldEvaluate(record) {
	return s.recoverActiveIncidentsForInactiveObject(ctx, objectType, id, now, summary)
}
```

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
