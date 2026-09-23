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
