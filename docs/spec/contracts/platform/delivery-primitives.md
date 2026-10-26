# 交付原语与所有者 fencing

## Scenario: Record-platform delivery primitives 的 opaque identity 与 owner fencing

### 1. Scope / Trigger

- Trigger：修改 `internal/center/recordplatform/` 的 deletion-request token、request fingerprint、outbox worker、identity mutation guard、content lease，或 `internal/center/store/record_platform*.go` 的 idempotency/outbox/lease/reservation-fence transaction 时。
- 此场景只覆盖 r1 已有 APP 表上的可复用 delivery primitive。它不新增 migration、不实现 `deployment_membership` 的 writer/heartbeat/readiness、不注册 bootstrap worker，也不决定 deletion 的 committed/not-committed、ledger/witness/recovery 或 records 业务事实。

### 2. Signatures

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

### 3. Contracts

- `drt1_` transport 只接受无 padding base64url 的 32-byte canonical spelling。issued token 由 `crypto/rand` 生成，只有 issued capability 能计算 deployment/project-bound commitment；parsed transport 不能产生可持久化 commitment。普通 formatting 必须 redacted。
- durable primitive identifier（idempotency key、object/client/mutation、outbox subject 等）不得包含 canonical token 或可解码的 noncanonical `drt1_` alias 子串。拒绝发生在 SQL 前；不得把 raw token、正文或 stable object ID 写入普通日志。
- `RequestFingerprintV1` 只能由固定字段顺序、length-prefix v1 codec 产生。`PersistedRequestFingerprintV1` 是独立的 readback-only sealed type：trusted DB readback 可用于 replay 比较，但不能转换为 claim/complete 所需的 canonical write fingerprint。
- 每个 idempotency/outbox/guard/lease/reservation owner transition 必须同时比较 owner ID、generation、调用方持有的精确 DB-observed expiry 和对应的 `> transaction_timestamp()` live predicate。任何 0-row result 都映射为 lost/stale owner，不能补写；claim/takeover 递增 generation。
- idempotency mismatch 永远是只读 conflict；completed row 必须有结果 fingerprint，idempotency expiry 必须严格晚于 active owner expiry。cleanup 只能删除无 live owner 的过期 primitive，不能删除 reservation/ledger/evidence。
- 所有 claim/finalize 在同一 `pgx.Tx` 内先调用 injected `AdmissionGate`。nil/error gate 必须拒绝且产生 0 primitive write/0 send；相关集成 不实现 concrete membership SQL。
- outbox 顺序固定为 `gate + claim + commit -> fresh authorize/render -> network send -> gate + fenced terminal/retry`。只有 allow 且 current epoch 等于 captured authorization epoch 时可发送；deny/mismatch/missing handler cancel，temporary error retry；ordinary worker logs 只允许固定安全分类，不得记录 dependency error 本文。`Run`/`RunOnce` 的 nil context 直接返回 invalid-worker error，不得 panic。
- `RunOnce` 在依赖校验后必须先返回已取消的 context，`Run` 在启动 pass 前必须安静退出已取消的 context；pre-cancelled worker 不得 claim、authorize 或 send。`LeaseWorkGuardV1` 必须拒绝 typed-nil `Clock`，并同步 `CanContinue` 与 `Renew` 对 owner/stopped state 的访问，避免并发复活本地 authority 或 data race。nil renew callback 也属于 renewal failure：非 nil guard 必须先在 mutex 下永久置为 stopped 再返回 `ErrLeaseRenewalStopped`，后续有效 callback 不得恢复本地 work。
- `DeletionReservationFenceV1` 的成功 fence epoch 至少为 1；reservation/epoch/deletion-fence/object-content lock 顺序不变。client-content lease 没有 object identity，不能单独授权 serving。

### 4. Validation & Error Matrix

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

### 5. Good / Base / Bad Cases

- Good：同 fingerprint 的 completed idempotency request 返回原 result digest；相同 owner/generation 但已续租前的旧 expiry 不能完成或释放新 lease。
- Good：业务 callback、idempotency 和 identity-only outbox enqueue 在一个 admitted transaction 中一起 commit 或 rollback；网络发送只在 claim commit 后运行。
- Base：object epoch 已存在为 0 时，fence 操作把它递增为 1，并以同一 owner/generation 写入 reservation 与 deletion-fence lease。
- Bad：只比较 owner ID/generation 而忽略 observed expiry，会让同一 owner 的旧 token 在 renew 后继续写入。
- Bad：记录 `%w` dependency error、把 transport parser 的 raw token 当 commitment capability，或让 client lease 代替 object serving check，都会越过 secret/authorization boundary。

### 6. Tests Required

```bash
go test ./internal/center/recordplatform ./internal/center/store \
  -run 'Idempotency|Outbox|Guard|Lease|Serving|DeletionFence|Token' -count=1

go test -race ./internal/center/recordplatform ./internal/center/store \
  -run 'RecordPlatform|Idempotency|Outbox|Guard|Lease' -count=10

scripts/test-record-platform-integration.sh postgres -- \
  go test -v ./internal/center/store \
  -run '^TestPostgresIntegrationRecordPlatform' -count=1
```

- Unit/store tests 必须覆盖 canonical/noncanonical token alias、persisted fingerprint 不可写、old-expiry fencing、nil gate/context、typed-nil clock、nil renew callback 的永久 stop、pre-cancelled worker、fixed-safe worker log、guard `Renew`/`CanContinue` race、零 fence epoch、0-row lost owner。
- PostgreSQL selector 不能以 `SKIP` 作为 acceptance evidence；必须覆盖并发 claim、expired takeover、stale finalizer、atomic rollback 和 reservation/epoch/object lease serialization。

### 7. Wrong vs Correct

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

## Scenario: Attachment content delivery lifecycle and draft assertion windows

附件下载保持 `BlobStore`、`DownloadRepository`、`ContentLeaseRepository` 与
`ContentStream` 的导出接口不变。一次 delivery 在初始化阶段必须使用同一
`lifecycleCtx` 完成 record serving lease 的后台续租、初始内容断言、Blob
`Stat`、Blob `Open` 和最终内容断言；`Stat` 与 `Open` 都绑定完整的
`ObjectVersion`（版本、大小和摘要），迟到的 reader 必须在拒绝接管后关闭。

record delivery 的 lease renew 与 serving assertion 共享容量为一的操作门，
从读取当前 token 到数据库返回并发布成功结果都保持串行。关闭或撤销先发布
终态、取消 lifecycle context、关闭 reader，再等待在途 lease 操作；清理只能
释放精确的数据库返回 token。续租结果在终态之后到达时只能进入待释放槽，
不得恢复本地授权。网络写入一旦开始不可撤回，但终态后不得开始新的写块。

draft delivery 不获取 record serving lease，也不启动伪续租。每次新鲜
`AssertAttachmentContent` 查询以查询开始时间作为窗口起点；只有查询成功且
耗时未超过 `LeaseDuration`，才发布新的本地写入窗口。每个块写入前都重新
断言并在持锁边界确认窗口仍未过期；draft 发布、删除、归属或对象改变会使
固定 assertion 失败，旧流不得转为 record 流。

### 8. Attachment delivery evidence

- 初始化慢于授权窗口、取消、迟到 reader、续租提交后发布前的 Close/Assert
  交错、精确 token 清理和 reader 并发关闭必须有 barrier 回归测试。
- draft 必须覆盖跨多个新鲜窗口的成功传输，以及每个后续窗口前的删除、发布、
  归属改变和对象改变；在途 writer 已发送的字节不作为可撤回授权证据。
- reader 在声明长度恰好结束时的 `EOF` 成功、短读 `io.ErrUnexpectedEOF`、
  越界字节、short write 和最后非 EOF 错误必须保持可观察且 fail closed。

## Scenario: Attachment durable GC 的精确版本维护能力

```go
type BlobGCDeleter interface {
    DeleteGCVersion(context.Context, ObjectVersion) (DeletionReceipt, error)
}
```

- `BlobStore` 的 Put/Open/Stat/Delete 四方法及下载接口不变。S3 普通
  Open/Stat/Delete 仍要求 current 身份；维护接口仅供已经取得完整对象身份的
  durable GC claim 的消费者使用，不授权历史业务读取或任意版本清理。
- GC claim 绑定 backend/key/VersionID/SHA256/size。S3 维护入口不要求目标
  current，但仍检查请求/context、versioning Enabled、非 null VersionID、
  无 Object Lock、精确版本 HEAD/GET、大小及完整 SHA256；只按目标 VersionID
  REMOVE，随后确认精确 absence。禁止 key-only 删除、扫描猜版本或改删新版本。
  Local 维护入口沿用普通 Delete 的私有目录、mutex、完整身份检查及目录 sync。
- V1 已非 current 时只删除 V1；V2 或 current delete marker 保持不变。marker
  作为目标必须报错且不 REMOVE。错误 hash/size、missing bucket、Suspended、
  Object Lock、403 和传输错误不能作为成功 receipt。
- 精确 V1 已不存在时返回完整原身份、`Deleted:false`，即使 V2/marker 是 current。
  无法重新验证不存在版本的历史 hash；可信度来自已绑定的 durable claim 和
  completion CAS，而不是不存在对象的内容验证。REMOVE 后的确认错误可能已产生
  物理效果，但不能返回成功；接管者可通过精确 absence 收敛并只结算一次。
- `BlobPublicationReconciler` 仍为独立 current-only 协议；历史 bound residue
  可能继续阻塞 keywide GC 资格，本能力不自动扩权。删除 current 版本可能露出
  更老版本，不承诺整个 key 永久撤销，也不对外部管理员提供 current 锁。
- 必须通过真实 MinIO 的历史 V1/V2、marker、错误身份、配置漂移及删除后故障回归，
  并通过两条真实 PostgreSQL GC 消费路径；验收入口见
  [集成合同](../records/integration-recovery.md)。

### Import 的 pre-Put GC 生命周期观察

- 所有导入附件在第一次 Put 前，以短的 READ COMMITTED / READ ONLY 事务批量观察
  每个 canonical `blob_key` 的全部状态 GC history COUNT；存在任何非 completed 行即
  `ErrBlobGCProtected`，尚不执行附件 Put。重复 digest 保留同一 baseline，其他 key
  的 GC 不使本 key 冲突。数据库错误不得解释为零历史。
- 内部 `ImportBlobGCObservation` 绑定 digest 与非负 count，零 count 有效、零值 token
  无效；不进入 JSON/archive，也不是密码学授权。Put 返回的 exact identity 与原观察值
  一同登记，禁止在 Put 后刷新 baseline；不持数据库锁跨网络。
- 登记事务固定 quota account → Blob ROW EXCLUSIVE 锁 → **另一条语句**读取 history。
  每项 count 必须相等且无 active 行，再复用现行 exact metadata identity 校验并插入
  attachment/ref。三个含 imported attachments 的 revision commit 入口显式使用
  READ COMMITTED；其他调用保留原默认隔离。锁保持至 revision/origin/job 整体提交。
- 两个 fresh GC claim producer 同样显式 READ COMMITTED，已有 Blob SHARE ROW EXCLUSIVE
  锁后的资格查询看到最新已提交引用。import 先提交则共享引用受保护；claim 先赢，即便
  delete 与 completion 已完成，history count 增加仍使迟到登记回滚。旧生命周期完成后
  重新观察并 Put 可合法重建 local 同 digest 身份或 S3 新版本。
- 此协议依赖可信现有应用 API 保留全部 GC history：不 prune、不 rekey、不 reopen
  completed；reclaim 只更新 active 行。runtime 没有 DELETE 权限但有 UPDATE 权限，
  因而不是抵抗任意 SQL/DBA 的不可变 ledger。未来改变保留合同必须先替换此 fence，
  不能截断或抽样历史来换成功。当前使用每次观察一批、每 record 登记一批查询，无新增
  history index/migration；现有超时失败则回滚。
- 缺 observer 的附件 Blob 配置 fail closed。Apply 在观察或最终登记遇到 GC protection
  时映射既有 `ErrImportCASConflict` / HTTP 409；不自动重试，不补偿删除可能共享的
  Put 对象。用户重试 planned Apply 重新观察和 Put；applied replay 不再 Put。
- 该合同不扩展普通 publication/upload writer，不解决旧 owner 已发出的迟到外部 DELETE、
  远端 capability closure、root-independent obligation 或全局 G2 锁序。

---
