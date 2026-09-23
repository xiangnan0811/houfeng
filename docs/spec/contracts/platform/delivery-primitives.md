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

---
