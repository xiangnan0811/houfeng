# Records 核心数据与 ACL

## Scenario: Records core `0052` schema and exact APP ACL fragment

### 1. Scope / Trigger

- 触发：修改 `0052_create_records_core.sql`、`internal/center/records/`、Records store transaction、current APP ACL fragment，或任何 Records core purge/readiness 路径时。
- 项目当前只支持 fresh/current development database 与 exact repeat；不为 `experience_logs`、旧 `0052`、混合版本或部分升级建设 backfill/upgrader。`0052` 合并后按普通迁移不可修改，后续 schema 变化使用 `0053+`。

### 2. Signatures

- Root migration：`db/migrations/0052_create_records_core.sql`。
- Current fragment：`recordsCoreAppACLCurrentMigrationFragment() AppACLCurrentMigrationFragment`，migration name 必须精确为 `0052_create_records_core.sql`。
- Deferred validator：`record_platform_internal.validate_record_revision_primary_subject() returns trigger`，`SECURITY INVOKER`、`search_path=pg_catalog`、显式 revoke `PUBLIC`。
- 九张 owned table：`records`、`record_revisions`、`record_revision_subjects`、`record_revision_tags`、`record_revision_participants`、`record_drafts`、`record_draft_checkpoints`、`record_domain_activities`、`record_core_purge_receipts`。

### 3. Contracts

- `records` 是稳定 root/current projection；`record_revisions` 与 subjects/tags/participants 是只插入的完整历史。来源删除不能 cascade Records；所有 record-owned 清理由 core purge adapter 在一个事务中显式执行。
- `(record_id,current_revision_id)` 使用 initially-deferred same-record FK。revision subject 使用 partial unique index 保证至多一个 primary，并由两个 initially-deferred constraint trigger 覆盖 revision insert 与 subject insert/delete，在 commit 时保证每个仍存在的 revision 恰有一个 primary。受控 purge 同事务删除 subject 与 revision 时 validator 看到 revision 已消失并允许提交。
- immutable history tables没有 APP `UPDATE` grant，并复用 `reject_immutable_mutation()` 拒绝 owner/migrator update；delete 仅用于受控显式 purge。
- current fragment 精确登记九张 table 加 primary-subject validator function。`center_runtime` 只取得 Records 在线读写/显式 purge 所需 table privilege；`platform_admin` 只能读取无内容的 `record_core_purge_receipts`，不能读取 Records content table；validator 没有额外 direct APP EXECUTE tuple。
- `record_core_purge_receipts` 的 schema、insert 参数和 receipt digest 都只能保存 operation-scoped proof：不得包含或散列 `project_id`、`record_id`、revision ID 或业务内容。需要在 preview 中关联对象时，经 `record_purge_operations -> deletion_reservations` 读取当前 operation binding，不能把对象身份反规范化回 receipt。
- `record_draft_checkpoints` 是唯一恢复点名称；revision participant 只存在于独立 `record_revision_participants`，不得出现 `participant_ids` 或 `record_draft_recovery_points`。
- production current/historical authorization snapshot loader 必须在 admitted pgx transaction 中先执行 record read fence，再读取 root、visibility、identity snapshot、capture authorization 或 subject rows；直接 DB loader 只允许作为注入式单元测试 seam，不能由 production constructor 绑定。
- record candidate list 必须在同一条 SQL 中以 correlated `not exists` 排除 `fenced|committed` deletion reservation，并在过滤之后应用 `order by/limit`；后续 authorization snapshot 与 revision content read 仍各自 recheck fence，以关闭查询之间的 reservation race。
- forward `0081` 增加 `trashed` lifecycle 与 `trashed_at`：状态为 trashed 当且仅当该时间非 NULL。内部 `Application.ChangeTrashState` 只允许 active→trashed、trashed→active；archive/unarchive 接口仍只管理原两种状态，不把旧 restore 路由变成回收站恢复入口。trash 使用 `record.delete`，恢复使用 `record.update`，两者仍经过当前资源授权。
- 回收站请求携带原 `CurrentRevisionID/LockVersion/AuthorizationEpoch`，与 actor、记录、目标状态一起形成独立域 fingerprint，不因重试时的 current 状态变化而重算原 CAS。相同 key/body 返回原成功；不同 body 拒绝，新 key 同状态或旧 CAS 冲突。恢复后重放旧 trash 返回原事实而不再次移入回收站；重放仍需当前可访问，不能借其恢复已永久删除的 root。
- trash/restore 不新增内容修订或改正文：同事务递增 root lock/auth epoch、更新 search lifecycle，并分别记录 `record_trashed` / `record_trash_restored` 活动。它们不打开新的 revision interval；历史内容恢复的 `record_restored` 语义不变。首次 trash 写 DB transaction time，成功重放不刷新；restore 清空 `trashed_at`。现有只读/search 页面显示回收站状态，不将其标为 active，也不开放普通编辑能力。本变更不增加回收站页面、自动清理或永久删除 HTTP。

#### 出生请求与外部连续性

- `recordbirth.Coordinator` 将独立 continuity reader 与可缺省的 birth producer 分开。已安装 lineage 的新 allocation 和仍会产生效果的 pending 重放，先在 APP 写事务之外读取 W2 anchor，再由实际 APP 命令验证当前 binding、MAC 和 canonical ancestry；已有 ordinary pending 行不能绕过。已完成结果仍需当前授权，但只读返回不触发补发或新 I/O。
- `BeginAllocation` 的 winning row 决定 request/domain/allocation；选择 certifiable 时同事务追加 immutable checkpoint entry/head。entry 绑定稳定 request、fingerprint、allocation ID、初始 binding、sequence/previous hash；普通请求不伪造 checkpoint，后续 progress/tombstone 不改该映射。lineage head 在 gate rank 50 后、request/root rank 60 前获取；批量请求按稳定顺序取得共同资源。
- APP 固定 `record_birth_checkpoint_proof(bytea)` 只从已提交持久行重建 `houfeng.record-birth.app-checkpoint.v1` proof；要求独立 READ ONLY 事务且尚未分配 transaction ID，不能从包含未提交 allocation 的事务取 proof。producer 不取得签名密钥。W2 reserve 在同一事务验证 proof、CAS monotonic anchor、保存 exact request coverage 后才分配认证 ID，禁止无 coverage 的 reserve。
- anchor CAS 冲突只重读当前 anchor 与已提交 proof，保留原 request/domain/allocation。较高 anchor 已覆盖当前请求时仍验证 inclusion。APP 领先 W2 可以继续；W2 anchor 不在当前 APP 链、hash 分叉、binding/key 缺失或 reader 不可用时暂停新效果，不能因 APP request 缺行就降为 ordinary。
- never-installed 由受保护的显式启动状态证明，不由一次缺行、缺配置或缺 producer 推断。已安装且 reader/source 有效时，producer 缺省或运行时认证/网络故障允许真正新请求选择 ordinary，既有 certified pending 仍保持原域。停用后仍保留 sticky lineage 与 reader 要求；整个 W2 不可读不放宽 source 准入。具体启动及凭据边界见 [Compose authority](../platform/compose-authority.md)。
- 该链只覆盖可能授权 W2 birth 效果的请求前缀，不声称发现没有外部义务的 ordinary-only tail 丢失。任何 APP 物理/逻辑恢复、替换或导入旧状态（包括同 generation/key 的 APP-only restore）均要求完整离线 fence、stop/join、零旧会话/无 prepared tx，并在重开前推进 W2 generation、退休旧 proof/producer。完整 production birth 启停与恢复 ceremony 仍未完成，不能用低层 PostgreSQL 或启动文件验收代替。

#### Revision 事务的只读资源声明

- `RevisionParticipant.MutationResources(ctx, tx, command)` 在调用者提供的同一
  transaction 内只做无锁 SELECT/纯发现，不写行、不取显式锁、不外调；registry
  合并闭合的 typed resource families，确定排序、去重并拒绝同 identity 的矛盾观察。
- attachment 声明包含整个待发布 draft 的剩余附件、upload、processor、publication
  和 part，不只包含本修订引用的附件；evidence/comparison 只从真实 snapshot owner
  与认证继承关系发现来源，不从导入 payload 中猜本地身份。collaboration/search
  包含持久 membership、旧 follower、published/building generation 和待替换的旧 subject。
- store 私有 `recordMutationResources` 记录固定 rank/subrank 声明；来源顺序固定
  VPS → monitoring instance → target。gate scope 只有精确 project/record，缺行不证明
  安全；idempotency 声明保留原 key。新 revision/activity 使用现有 deterministic
  identity，引用带精确 revision/ordinal；真正尚未生成的 outbox 行只声明语义范围。
- 声明本身不是 acquired set、source terminal proof 或授权。revision batch 先按固定顺序
  取得全批 namespace、idempotency、epoch/root 等低 rank 资源，再进入 quota/Blob fence；
  不能在首条 participant 持高 rank 锁后回头取下一 record 的低 rank 锁。
- 获锁后仍按原输入顺序逐条完整执行，不把全部 revision 写入提前到全部 participant 之前。
  重复 record/idempotency 共享事务内演进状态，保留 stale base、replay、no-op、draft 清理
  和结果顺序；新 record 的 obligation epoch binding 与 refs/import finish 同事务提交。
  新发现未锁资源或任一后续命令失败，整体 rollback，不留下 speculative root/claim/ref/quota。
  此本地写入接线不代表所有 G2 writer 或 source cold handoff 已通过准入。

### 4. Validation & Error Matrix

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

### 5. Good / Base / Bad Cases

- Good：同一 admitted transaction 先插 revision，再插恰好一个 primary 和任意 related subject，commit 时统一验证。
- Base：fresh apply 后 exact repeat 不改变 migration ledger、manifest、owner、ACL、function 或 trigger state。
- Bad：只建 `where is_primary` partial unique index并声称“恰好一个”；它只能拒绝第二个 primary，完全没有 subject 的 revision 仍可提交。
- Bad：为了绕开 deferred check 把 subject/revision/root 分成多个 purge transaction；第一笔 subject delete 必须失败，而不是制造暂时不合法状态。

### 6. Tests Required

```bash
go test ./internal/center/store/migrate -run 'RecordsCore|AppACLCurrent' -count=1
go test -race ./internal/center/records -run 'Revision|Lifecycle|Status|Template|Canonical|Subject|Authorization|Tombstone' -count=10
scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store/migrate \
  -run '^(TestPostgresIntegrationRecordsCoreSchema|TestPostgresIntegrationAppACLCurrent)$' -count=1
```

- PostgreSQL test 必须覆盖 fresh/exact repeat、无 primary 的 commit-time `23514`、第二 primary `23505`、same-record FK、immutable update、单事务显式 purge、receipt 的 exact content-free column set，以及 runtime/admin exact privilege；不得以 `SKIP` 作为证据。

### 7. Wrong vs Correct

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
