# Records 永久删除与连续恢复

## Scenario: Records permanent deletion、core purge 与连续 recovery

### 1. Scope / Trigger

- Trigger：修改 `internal/center/recorddeletion/`、`internal/center/store/record_deletions*.go`、`record_deletion_recovery*.go`、`0052_create_records_core.sql` 的删除投影/恢复字段，或后续 Records child 注册新的 permanent-delete adapter 时。
- 本场景建立在 [交付原语](../platform/delivery-primitives.md) 的 reservation、fence、opaque token、owner lease 与 admission 之上；它拥有 Records 删除编排、core 在线清除和 ledger replay recovery，但不拥有独立 deletion ledger/witness 服务或 attachment/evidence/search/activity/collaboration/portability 的内容表。

### 2. Signatures

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

### 3. Contracts

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

### 4. Validation & Error Matrix

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

### 5. Good / Base / Bad Cases

- Good：delete commit 已 witness，worker 先永久 fence，再逐 adapter purge/verify；core receipt 不含 record/revision ID 或任何业务内容，最终 operation 为 `online_purged`。
- Good：ledger 明确证明 delete 未提交，worker 追加并见证 `attempt_not_committed` 后释放 provisional fence；record 内容完全保留，同 token replay 仍返回同一 `not_committed` operation。
- Base：生产装配已注册 core、attachment、evidence、search、activity、collaboration、portability 七个 adapter；`record_markdown_client` 与 `record_comparison` 未在该装配中注册，且永久删除仍为关闭模式。九个 adapter、ledger/witness 与全部 readiness 条件未闭合时，preview 不签发 token；不能把已有七个 adapter 解释为永久删除已上线。
- Base：应用投影丢失后 recovery 从连续 ledger cursor 重建 terminal projection；重复 replay 不重复 receipt/audit，也不跳过未知 entry。
- Good：operation A 已终态后 operation B 建立新 fence；A 的幂等或延迟 replay 不更新 B 的 fence，B 继续让 Records read 返回 reserved。
- Bad：以“后续表当前为空”省略 adapter，收到 ledger timeout 就释放 fence，或仅凭 `record_purge_operations` 无 row 返回 404；这些都会把未知状态误当作安全结论。
- Bad：recovery 以 `(project, kind, object)` 无条件 expire fence，或在幂等验证中要求 active fence count 为零；前者会破坏较新 operation，后者会让合法旧 entry replay 永久卡住。

### 6. Tests Required

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

### 7. Wrong vs Correct

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
