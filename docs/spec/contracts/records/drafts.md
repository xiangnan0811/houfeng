# Records 私有草稿与发布

## Scenario: Records private drafts, bounded checkpoints, and atomic publish cleanup

### 1. Scope / Trigger

- Trigger: 修改 `internal/center/records/drafts.go`、`internal/center/store/record_drafts.go`、revision command 的 draft publication 字段、`record_drafts` / `record_draft_checkpoints` SQL，或 draft expiry cleanup 时。
- 该场景覆盖作者私有 server draft、精确 ETag PATCH、bounded checkpoint、显式 discard/revoke，以及与正式 revision transaction 同生共死的 publish cleanup；浏览器 buffer、Records HTTP DTO 与永久删除 core purge 由各自 owner 继续闭合。

### 2. Signatures

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

### 3. Contracts

- draft payload 是 immutable canonical JSON object；payload hash 与 ETag 必须从 persisted payload、draft ID、author、version 重新计算验证，不能信任数据库中的摘要列或客户端项目/作者字段。
- `GetDraft`、list、PATCH 与作者操作的 cleanup 使用 author-scoped routing SQL；该 SQL 必须在返回 metadata row 前以 correlated `not exists` 排除 existing-record draft 的 `fenced|committed` reservation，并在过滤之后应用 list `limit`。错误作者与已 reserved 的 existing-record draft 在 payload read 前得到 `ErrDraftNotFound`；`record_id is null` 的 new-record draft 保持可见。
- routing SQL 的原子 reservation filter 不能替代 race recheck。PATCH 在一个 admitted pgx transaction 中按 `atomic routing -> optional mutation-fence recheck -> author row FOR UPDATE -> exact ETag -> update -> checkpoint -> expiry prune -> newest-20 prune` 执行；Get/list 使用 read-fence recheck。相同 canonical payload 只续 `updated_at/warning_at/expires_at`，不增加 version、发行 checkpoint ID 或写 checkpoint。
- 内容变化时每个 `date_bin(..., 5 minutes, fixed origin)` bucket 最多一个 immutable checkpoint；保留最新 20 个并删除 `checkpoint_expires_at <= transaction_timestamp()` 的行。draft inactivity TTL 为 90 天，warning boundary 为 expiry 前 7 天；所有时间以 database transaction time 为准。
- discard/revoke 与 publish cleanup 都先删除 checkpoints 再删除 draft。publish 必须在现有 revision transaction 内锁定作者 draft，校验 exact ETag 及 create/new-draft 或 update/same-record-and-base shape，在 formal revision/no-change 成功后、idempotency complete 前 cleanup。任一 conflict 或 cleanup error 回滚正式事实并保留 draft。
- completed idempotency replay 在 draft validation/cleanup 之前返回 persisted revision result；首次 publish 已删除 draft 后，同 key/same fingerprint replay 仍必须成功。request fingerprint 绑定 `DraftID` 与强 ETag，换 draft 或换 version 不能复用同 key。
- 普通 draft create/read/PATCH/discard/revoke/expiry cleanup 不写 `record_domain_activities`、`record_outbox`、search 或 notification；只有 publish 成功产生正式 revision 既有的 activity/outbox。
- `ClaimExpiredDrafts` 必须在 claim SQL 中、`order by/limit` 之前以 correlated `not exists` 排除 existing-record draft 的 `fenced|committed` reservation，同时返回 nullable `record_id`。claim 完成后、任何 checkpoint/draft delete 之前，对去重后的每个非空 record ID 再执行 mutation-fence recheck；并发 reservation 命中时整批 rollback。`record_id is null` 的 new-record draft 不受对象 reservation 过滤影响。
- duration 以 Go `time.Duration.Microseconds()` 作为 `bigint` 传入。一个 bind 参数乘 interval 可沿用现有 SQL；两个参数先做减法时必须显式 cast 两侧为 `bigint`，否则 PostgreSQL parse 会对 `unknown - unknown` 返回 `42725`。

### 4. Validation & Error Matrix

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

### 5. Good / Base / Bad Cases

- Good：两个客户端持有相同 ETag；先取得 row lock 的请求推进到 v2 并写一个 bucket checkpoint，后取得锁的请求读到 v2 typed conflict 且不覆盖。
- Good：publish 创建 revision/activity/outbox 后在同一 transaction 删除 checkpoint/draft并完成 idempotency；同 key retry 不再要求 draft 存在。
- Good：expiry worker 的首条 SQL 跳过已经 reserved 的 existing-record draft；claim 后出现 reservation 时，二次 mutation fence 在 delete 前中止并回滚整批。
- Base：autosave 内容与 server canonical payload 相同，仅刷新 inactivity TTL，避免 version 与 recovery history 噪音。
- Bad：formal revision 先 commit，再调用独立 `DeleteDraft`；cleanup failure 会留下“已发布但仍可编辑”的 server draft，retry 也无法证明单一结果。
- Bad：用 `limit` 但没有 `SKIP LOCKED` 或跨 transaction claim/delete；并发 worker 会阻塞、重复 claim 或留下部分 cleanup。
- Bad：expiry cleanup 只按 `expires_at` claim 后直接 delete；它会绕过 permanent-delete reservation，或者在 claim 与 delete 之间吞掉刚被 fenced 的 draft。

### 6. Tests Required

```bash
go test -race ./internal/center/records ./internal/center/store \
  -run 'Draft|Checkpoint' -count=10

scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store \
  -run '^TestPostgresIntegrationRecordDraft' -count=1
```

- Unit/race 必须覆盖 immutable payload/ETag、作者隔离、two-client conflict、no-change TTL、checkpoint SQL、discard/revoke、expired batch grammar、publish create/update/no-change/conflict/rollback/replay。
- 真实 PostgreSQL 必须覆盖并发 PATCH 单赢家、五分钟 bucket/newest 20/seven-day retention、并发 cleanup claim 不相交、`fenced|committed` 过期 existing-record draft/checkpoint 均保留、publish/discard/revoke cleanup，以及普通 draft 操作的 activity/outbox 零行；runner 不接受 `SKIP`。

### 7. Wrong vs Correct

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
