# Record platform delivery primitives: idempotency, outbox, guards, and leases

## Goal

交付 Child 1 可被后续 records、deletion 与 worker 切片复用的 fail-closed 交付原语：canonical deletion-request token、同事务 idempotency/outbox 边界、owner-generation identity guard、对象/客户端/删除 fence lease，以及不在数据库事务中进行网络调用的 outbox worker。

## Confirmed facts

- 本切片从干净的 `383763f1` 开始；它已经包含 APP r1 schema/ACL、runtime admission 与 `recordauth`。冻结旧 foundation worktree 不是来源、不能读取或合并。
- `0051_create_record_platform_foundation.sql` 已创建 `record_outbox`、`record_idempotency_keys`、`identity_mutation_guards`、`deletion_reservations`、三个 lease 表和 `content_delivery_epochs`；它没有定义这些 primitive 的 claim/finalize SQL、业务状态机、跨表 FK 或 lock order。
- 本切片不修改 `0051`、ACL/admission、bootstrap、projector、ledger/witness/recovery，也不放行 Child 2–11。Task 10 才拥有 deployment-membership admission SQL；Task 4 才拥有 deletion reservation/ledger transition 与 epoch 的业务语义。
- `record_outbox` 没有正文、recipient 或 renderer payload。它只能保存无内容的 event identity；每次 delivery 必须在已提交 claim 之后重新授权并由调用方加载/渲染，网络调用不得进入持久化 transaction。

## Requirements

1. 新建纯 Go `internal/center/recordplatform`，定义闭合的 v1 ID、token、fingerprint、owner lease、idempotency、outbox 与 guard contracts。生产代码不 panic、不持久化 raw deletion token、正文或 stable object ID 到普通日志。
2. `DeletionRequestTokenV1` 必须由 `crypto/rand` 产生 32 raw bytes，transport 仅接受 `drt1_` 加无 padding base64url 的 43 字符形式。commitment 固定为 `SHA-256("houfeng-deletion-request-token-v1" || 0x00 || deployment_id || 0x00 || project_id || 0x00 || raw32)`；跨 deployment/project 重放、非 canonical、低熵/错误长度和携带文本 token 作为 preimage 都在写入前拒绝。
3. request fingerprint 必须是版本化、固定字段顺序、length-prefix 的 SHA-256 值；只接收已验证的 operation/project/scope/payload digest，不接受 map、JSON 排序、自由文本或未规范化 caller bytes。未来业务 owner 可提供 payload digest，但不能绕过该 codec。
4. 同一 `(project, operation_kind, idempotency_key)` + 同 fingerprint：completed 返回原 result fingerprint；live in-progress 不能被其他 owner finalize；过期 in-progress 只能以递增 generation 接管。不同 fingerprint 永远返回稳定 conflict，且绝不覆写原有 row、结果或 expiry。每一次 claim、renew、release、complete、sent、retry、cancel 都同时比较 `owner_id`、`owner_generation` 和该表的 live-expiry column `> transaction_timestamp()`；影响 0 行即 lost/stale owner，不能补写。
5. idempotency row 的 persisted expiry 必须严格晚于本次 owner lease；completed result fingerprint 必须存在。janitor/cleanup 只能删除没有 live owner 的过期 primitive，不能删除 live idempotency/outbox/guard/reservation/lease。
6. 实现显式 transaction seam：同一 pgx transaction 可依次运行同事务 `AdmissionGate`、写业务 owner 提供的事实、idempotency row 和 outbox row；repository API 不接收 sender、HTTP client 或网络 callback。Child 1 只定义/注入 gate，绝不实现 `deployment_membership` writer、heartbeat 或 readiness；nil/error gate 必须拒绝 claim/finalize 且产生 0 send，Task 10 才拥有其 concrete SQL。
7. outbox claim/takeover 使用 `owner_generation` fencing；worker 顺序固定为 `gate + claim + commit → fresh authorize/render → network send → gate + fenced terminal/retry finalize`。fresh authorizer 必须返回当前 epoch，只有 allow 且 `CurrentEpoch == event.AuthorizationEpoch` 才可 send；deny、epoch mismatch、未知 handler 均 fenced-cancel，暂时性 authorize/render/send error fenced-retry。每次 retry 都重新授权，旧 owner 在接管后完成、取消或重试的成功数为 0。worker 只接收 identity/epoch，不保存正文、recipient、授权结果或未命名的 `recordauth` capability。
8. identity guard 与 deletion-fence/object-content/client-content lease 均采用相同的 non-empty owner、正 generation、DB-time expiry 和 owner triple fencing（outbox/idempotency/reservation 使用 `owner_expires_at`，guard/lease 使用 `expires_at`）。多资源操作按固定 relation order 获取：`record_idempotency_keys → identity_mutation_guards → deletion_reservations → content_delivery_epochs → deletion_fence_leases → object_content_leases → client_content_leases → record_outbox`；同类 key 按 canonical tuple 升序。续租失败发生在本地过期前时，调用方必须停止该 work，不能凭本地时钟继续写。
9. `content_delivery_epochs` 是每个 object 的 required pivot：首次 object owner 在其业务 transaction 中创建 epoch 0；本切片遇到缺失 epoch 必须 fail closed，不创建/重置。reservation fence 仅允许对 live、unexpired `previewed` reservation lock `reservation → epoch → deletion-fence lease → object-content lease`，拒绝 live object lease，原子递增 epoch，并把 reservation 和 deletion-fence lease 写成同一新的 object-global owner/generation，同时将 `fence_epoch` 设为新 epoch；它不实现 `committed|not_committed`、audit、purge operation 或 ledger proof。所谓 serving permit 是 live object lease + 无 live deletion fence + exact captured epoch 的组合值；client lease 不是 object-specific drain/purge 证明，不能单独授权 serving。
10. 所有新行为先有 observed RED test；unit/store/real PostgreSQL/fake-clock/concurrent takeover 和 race tests 都必须覆盖。实施完成前先完成 spec-compliance review，再完成 code-quality review；任一 P0/P1/P2 必须修复并复审。

## Acceptance Criteria

- [x] token/fingerprint golden 与 negative corpus 覆盖 exact token grammar、CSPRNG path、canonical decode/re-encode、domain separation、wrong deployment/project、trailing byte、非 canonical transport、错误长度和 raw-token 零持久化/零日志；同 logical request 的 canonical fingerprint 稳定，字段/长度/版本漂移拒绝。
- [x] same-key/same-fingerprint completed replay 返回原 result fingerprint；same-key/different-fingerprint 不改变原 row 并返回 conflict；过期 owner takeover generation 严格增加，所有旧 owner finalization/renew/release 的 affected rows 为 0。
- [x] 同一 store transaction 中业务 callback、idempotency claim 与 outbox enqueue 全部成功或全部 rollback；repository interface 没有 network sender 参数。
- [x] outbox live claim、expired takeover、fresh authorization deny cancel、sender error retry、sender success sent 与 old-owner fenced finalization 都通过；每次 send/retry 都重新授权，worker 不把网络调用包进 pgx transaction。
- [x] guard 和三个 lease table 的 acquire/renew/release 使用 DB-time owner triple predicate；相同 key 的并发 claimant 至多一个获胜，expired takeover 可获胜，续租丢失前停止工作；多键锁定以固定排序避免逆序。
- [x] reservation fence 在 existing epoch 上原子地增量、绑定 `fence_epoch`、拒绝 live object lease 并使旧 serving permit 失效；缺 epoch、live deletion fence、client lease 单独授权 object serving、final deletion state/ledger transition 均 fail closed 或不在本切片发生。
- [x] 所有 claim/finalize 在 caller transaction 内调用 injected `AdmissionGate`；没有 live gate 时为 0 claim / 0 send，但本切片没有 `deployment_membership` writer/heartbeat/readiness 实现。
- [x] focused unit/store/real PostgreSQL selectors、`go test -race ... -count=10`、`make verify-go`、`gofmt` 和 `git diff --check` 均有可复核 PASS 证据。

## Out of scope

- deletion reservation 的 committed/not-committed outcome、operation/audit、`FinalizeObjectContentDelivery`、ledger/witness/recovery commit、permanent delete API，以及 Task 10 的 membership writer/heartbeat/readiness/concrete gate SQL。
- records/evidence/attachment 业务事实、HTTP route、bootstrap worker registration、external sender/render implementation、S3、migration、ACL/manifest 或 runtime-admission 改动。
- 归档父任务或既有 `07-24` 子任务，推送/PR/合并，或允许 Child 2–11 开始实施。

## Rollback

本切片只在已授权的 r1 APP 表之上新增 Go contract/repository/worker。回滚 binary 后不会改变 schema；未能证明 owner/expiry/fingerprint/authorization 的操作保持拒绝或不写，而不是放宽 fencing。
