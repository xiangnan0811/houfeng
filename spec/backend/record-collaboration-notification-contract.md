# Records 协作与通知合同

> 本文记录 Child 9 已落地的可执行合同。它补充
> `database-guidelines.md`、`record-authorization.md`、`error-handling.md` 与
> `logging-guidelines.md`，不替代其中的通用 Records / `recordplatform`
> 规则。

## 1. Scope / Trigger

以下改动必须加载本文：

- 修改 `db/migrations/0055_create_record_collaboration.sql`；
- 修改 `internal/center/recordcollaboration/` 或对应 `store/record_*.go`；
- 修改 Records revision 的 owner / participants / follow-up participant；
- 修改 action、comment、watch、inbox、recipient projection 或外部通知；
- 修改协作 Activity、Portability、deletion、backup/restore provider/adapter；
- 修改 `/api/records/{record_id}/actions|comments|watch` 或
  `/api/record-notifications`。

边界固定为：协作复用 `records.RevisionParticipant`、`recordplatform`
admission/idempotency/outbox/owner lease、`recordauth` 与现有 Records deletion
reservation/fence；不得创建第二套通用 auth、idempotency、outbox、lease 或
deletion orchestrator。Child 9 只发布 typed facts/provider/adapter，不创建
Search、Activity、Portability 的 root table、projection、job、page 或 aggregate
orchestration。

## 2. Signatures

### 2.1 Domain / store

```go
func NewPostgresRecordActionRepository(
    *pgxpool.Pool, store.AdmissionGate, store.CollaborationMembershipReader,
    *store.PostgresCurrentRecordAuthorizationSource,
) *store.PostgresRecordActionRepository
func (*store.PostgresRecordActionRepository) CommitAction(
    context.Context, recordcollaboration.ActionCommand,
) (recordcollaboration.ActionMutationResult, error)
func (*store.PostgresRecordActionRepository) ListActions(
    context.Context, recordcollaboration.ActionReadCommand,
) ([]recordcollaboration.ActionRecord, error)

func NewPostgresRecordCommentRepository(
    *pgxpool.Pool, store.AdmissionGate, store.CollaborationMembershipReader,
    *store.PostgresCurrentRecordAuthorizationSource,
) *store.PostgresRecordCommentRepository
func (*store.PostgresRecordCommentRepository) CommitComment(
    context.Context, recordcollaboration.CommentCommand,
) (recordcollaboration.CommentMutationResult, error)
func (*store.PostgresRecordCommentRepository) ListComments(
    context.Context, recordcollaboration.CommentReadCommand,
) ([]recordcollaboration.CommentRecord, error)

func NewPostgresRecordWatchRepository(
    *pgxpool.Pool, store.AdmissionGate, store.CollaborationMembershipReader,
    *store.PostgresCurrentRecordAuthorizationSource,
) *store.PostgresRecordWatchRepository
func (*store.PostgresRecordWatchRepository) SetWatch(
    context.Context, recordcollaboration.WatchCommand,
) (recordcollaboration.WatchStatus, error)
func (*store.PostgresRecordWatchRepository) GetWatch(
    context.Context, recordcollaboration.WatchReadCommand,
) (recordcollaboration.WatchStatus, error)
```

Notification/inbox 复用同一 repository 与 foundation outbox：

```go
func NewPostgresRecordNotificationRepository(
    *pgxpool.Pool, store.AdmissionGate, store.CollaborationMembershipReader,
    *store.PostgresCurrentRecordAuthorizationSource, time.Duration,
) *store.PostgresRecordNotificationRepository
func NewPostgresRecordNotificationRepositoryWithExternalBindings(
    *pgxpool.Pool, store.AdmissionGate, store.CollaborationMembershipReader,
    *store.PostgresCurrentRecordAuthorizationSource, time.Duration,
    store.ScopedTransportBindingSource,
) *store.PostgresRecordNotificationRepository

func (*store.PostgresRecordNotificationRepository) ProjectNotification(
    context.Context, recordplatform.ClaimedOutboxEventV1,
) (recordcollaboration.NotificationProjectionResult, error)
func (*store.PostgresRecordNotificationRepository) ListInbox(
    context.Context, recordcollaboration.InboxListRequest,
) ([]recordcollaboration.InboxItem, error)
func (*store.PostgresRecordNotificationRepository) GetInboxItem(
    context.Context, recordcollaboration.InboxItemRequest,
) (recordcollaboration.InboxItem, error)
func (*store.PostgresRecordNotificationRepository) GetInboxDeepLink(
    context.Context, recordcollaboration.InboxItemRequest,
) (recordcollaboration.InboxDeepLinkTarget, error)
func (*store.PostgresRecordNotificationRepository) TransitionInbox(
    context.Context, recordcollaboration.InboxTransitionRequest,
) (recordcollaboration.InboxItem, error)
func (*store.PostgresRecordNotificationRepository) CountUnreadInbox(
    context.Context, recordcollaboration.InboxListRequest,
) (int, error)
```

### 2.2 Markdown / providers / deletion

```go
func recordcollaboration.ParseCommentMarkdownV1(string) (
    recordcollaboration.CommentRenderModel, error,
)
func recordcollaboration.DecodeCommentRenderModelV1([]byte) (
    recordcollaboration.CommentRenderModel, error,
)

func recordcollaboration.NewActivityProvider(
    recordcollaboration.ActivityFactSource,
) (*recordcollaboration.ActivityProvider, error)
func (*recordcollaboration.ActivityProvider) ListFacts(
    context.Context, pgx.Tx, recordcollaboration.RecordFenceBinding,
) ([]recordcollaboration.ActivityFact, error)

func recordcollaboration.NewPortabilityAdapter(
    recordcollaboration.PortabilityStore,
) (*recordcollaboration.PortabilityAdapter, error)
func (*recordcollaboration.PortabilityAdapter) Backup(
    context.Context, pgx.Tx, recordcollaboration.RecordFenceBinding,
) (recordcollaboration.PortabilitySnapshot, error)
func (*recordcollaboration.PortabilityAdapter) Restore(
    context.Context, pgx.Tx, recordcollaboration.RecordFenceBinding,
    recordcollaboration.PortabilitySnapshot,
) error

func recordcollaboration.NewDeletionAdapter(
    recordcollaboration.DeletionStore,
) (*recordcollaboration.DeletionAdapter, error)
```

Activity/Portability contract version 都是 `1`。Activity 上限为 4096 facts / 4
MiB；Portability 每 surface 4096 rows、aggregate JSON 32 MiB。provider 必须使用
caller-owned `pgx.Tx`，先复用现有 read fence，再验证 exact current content epoch；
nil/typed-nil tx 或 dependency 一律 fail closed。

### 2.3 HTTP

```text
GET  /api/records/{record_id}/actions?limit=1..100        default 50
POST /api/records/{record_id}/actions
PATCH /api/records/{record_id}/actions/{action_id}
POST /api/records/{record_id}/actions/{action_id}/{complete|cancel|reopen}

GET  /api/records/{record_id}/comments?limit=1..200       default 100
POST /api/records/{record_id}/comments
PATCH /api/records/{record_id}/comments/{comment_id}
POST /api/records/{record_id}/comments/{comment_id}/redact

GET   /api/records/{record_id}/watch
PATCH /api/records/{record_id}/watch

GET /api/record-notifications?limit=1..100                default 50
GET /api/record-notifications/unread-count                no query
GET /api/record-notifications/{notification_id}
GET /api/record-notifications/{notification_id}/target
PUT /api/record-notifications/{notification_id}/{read|unread|dismiss}
```

Mutation 必须使用单值 canonical `Idempotency-Key`。action/comment edit、transition、
redact 与 watch PATCH 还必须使用 strong quoted `If-Match`; watch 允许 `"0"` 表示
无 row，action/comment version 必须可在 PostgreSQL signed bigint 中继续递增。inbox
transition body 必须精确 0 byte。

## 3. Contracts

### 3.1 Database / transaction

`0055_create_record_collaboration.sql` 拥有 14 张表：

```text
record_actions, record_action_events,
record_comments, record_comment_revisions, record_comment_tombstones,
record_comment_replies, record_comment_mentions,
record_followers,
record_notifications, record_notification_recipients,
record_notification_deliveries, record_notification_delivery_attempts,
record_notification_audit_summaries,
record_collaboration_purge_receipts
```

它只扩展现有 `record_outbox` 的 `source_version` 与
`record_fence_epoch`，不新建 outbox/idempotency/lease 表。typed notification event
使用 raw stable `subject_id`，另以正数 `source_version` 与 captured auth/fence
epoch 绑定 exact source；generic foundation event 的新增字段保持 0。

action/comment/watch/revision participant 的 durable 顺序是 admitted transaction
内的 idempotency claim → deletion reservation/epoch/fence → locked current record +
current source authorization → same-tx membership → business/history/follower/activity/
identity-only outbox → idempotency completion → commit。业务回调任一点失败都必须整体
rollback；不得在事务中发送网络请求。

Watch 是可继续演进的 current resource，没有第二套历史表。foundation 的 content-free
result fingerprint 必须绑定 idempotency key 与完整返回状态；已存在的 follower row 还要
保留同一 opaque fingerprint marker。曾由 watch mutation 触达的 row 即使变成
`default` 且没有 automatic source，也必须保留并保持 `follower_version` 单调，controlled
source prune 不得删除该 replay anchor；仅从未承载 watch result marker 的 automatic-only
row 可被清理。原 key 只有在 current state 与 row marker 共同唯一证明原结果时可 exact
replay；任何 preference/source/version/timestamp 演进都必须 content-free fail closed 为
`409` 且零写，禁止把新状态伪装成旧命令响应，也禁止复制第二套 idempotency/history
primitive。

当前 v1 membership 只接受 `project_id=default` 且 `users.role` 精确为 `admin`
的现存 user。missing、malformed、其他 role、查询不可用都 fail closed。access-group
visibility 只参与 `recordauth`，不是 membership proof。
Revision collaboration 的 author/owner/participants 去重后最多 512 个 follower identity；
必须在第一次 membership 查询或 follower UPSERT 前完成 512/513 边界检查。

comment revision/action event/tombstone/reply/mention/notification/audit/receipt 是
append-only 或 trigger-guarded。comment redaction 必须清空 current 与全部 history 的
source/render/hash，写 tombstone 后不可恢复；reply 只允许单层且同 record/fence。

runtime 不拥有 raw DELETE。正常 follower/recipient pruning 与 permanent purge 只能
调用 `0055` 的 closed `SECURITY DEFINER` public `bytea` wrappers；permanent purge 在
同一 transaction 内证明 13 个 purgeable surfaces 全空后才写 immutable receipt。

### 3.2 Comment Markdown v1

`comment_markdown/v1` 的 source 必须是 1..16,384 UTF-8 bytes。render model 最多
512 nodes、depth 8，link 最多 2,048 serialized bytes。唯一 node registry 是：

```text
paragraph, text, line_break, emphasis, strong, strikethrough,
inline_code, fenced_code, ordered_list, unordered_list, list_item, link
```

link 只接受 byte-canonical `http` / `https`，禁止 userinfo、default/zero/padded/
越界 port、legacy numeric host、dot segment、非 canonical percent encoding。raw
HTML、image、heading、table、task list、footnote、blockquote、thematic break、
attachment/evidence ref、unsafe URL、invalid UTF-8 与任何 over-limit 输入返回
`ErrInvalidCommentMarkdown`；不得降级为 HTML/JSON/通用 Markdown renderer。Go 与
Web 必须共用 `internal/center/recordcollaboration/testdata/comment_markdown_v1.json`。

### 3.3 Recipient / inbox / delivery

recipient priority 是 `security > mention > assignee > reply > owner > participant >
follower`。actor self 在 priority 计算前消除。mention/assignee/security 是 mandatory，
不受 mute/unwatch 抑制；其余 optional 只有 watching 或 automatic source 时可收，
muted 抑制 optional。

每次 projection replay 与每次 inbox list/count/item/target/transition 都重新计算
same-tx exact membership、record/source auth、captured auth epoch、current fence、
subject existence 与 current watch facts。projection replay 通过 closed prune 函数精确
reconcile recipient，并保留 surviving recipient 的 read/dismiss state；读路径只返回当前
仍获授权的 recipient，失效项 fail closed/opaque missing，不把 skipped row 当有效结果。
list/count 使用
`event_at DESC, notification_id ASC` keyset、SQL `LIMIT 100` page 与 500-row hard scan
budget；count 还必须遵守 caller limit。hard budget 是 fail-closed work bound，不承诺单次
请求越过 500 个失效候选继续寻找更旧结果。

external delivery 默认未配置。只有 exact `(default,user,channel,binding_id)` scoped
binding 才可计划；global incident Telegram/Feishu settings 不是 binding source。发送
发生在 claim transaction 之外，但 prepare/finalize 各自复用 admitted transaction，
并重新验证完整 outbox claim tuple、membership/auth/fence/source/policy/binding。消息仅为
固定英文 summary 与 canonical HTTPS `/records/{record}?notification={rnt}` link；不得
包含 Record title、comment、action details、recipient、credential、provider response。

provider timeout 最多 30 秒且严格早于 owner lease expiry；temporary failure 使用
deterministic exponential retry，最多 8 attempts。crash/takeover 的不确定发送结果进入
`unknown_outcome`，不得猜测并重发。未配置 delivery processor 时 typed delivery event
被 closed cancel；业务/inbox transaction 仍可成功。

### 3.4 Provider / adapter

Activity facts 使用 closed 11-kind registry，按稳定顺序返回，并用一次 bounded joined
provenance query 校验 source version/kind/actor/time 与 deterministic activity ID；先做
cap+1，再做 provenance，禁止逐 fact N+1。

Portability snapshot 要求所有 9 个 slice 非 nil、稳定排序、defensive clone；action 与
comment history 都从 version 1 连续到 current version。action final event 必须匹配 current
status/assignee/actor/time，当前 title/details/due/subject 由 current action row 单独验证并
恢复；action event 不是完整字段快照。comment final revision 必须匹配 current
state/content/time。reply/mention 不得 orphan，reply 必须 flat。redacted comment 及所有
revision 的 body/render/hash 必须为空且绑定 tombstone。notification audit 只含 closed
kind/subject/source version/time 与 signed-bigint-safe counts；不得包含 recipient、binding、
credential、subject ID、provider response。live audits 与 content-free summary 合并时
duplicate fail closed；restore 幂等且 exact，任何 drift 整体 rollback。

Deletion descriptor 名为 `record_collaboration`，surface digest 来自上述 14 张表的
closed sorted registry。已经外送的 copy 只在 preview 以 identity-free count 披露；不能
宣称远端副本被召回。

### 3.5 Production activation

真实 deployment-membership `AdmissionGate` 缺失时，route 可以注册但 mutation/worker
稳定 503，notification/external worker 不启动。禁止 allow-all、`AdmissionGateFunc`、
typed-nil 绕过或使用测试 membership 打开 production。witnessed source deletion authority
与 aggregate readiness 属后续 Child 10/11。

## 4. Validation & Error Matrix

| 条件 | 结果 |
|---|---|
| malformed/unknown/duplicate query；非法 header/JSON | application 前 `400 invalid_request|invalid_json`；零调用 |
| body 超限 | `413 request_too_large`；零 application 调用 |
| invalid comment UTF-8/surrogate/grammar/model/link/bound | `422 invalid_comment_markdown` |
| invalid action/mention/reply/watch/inbox domain | 对应 closed `422 action_invalid|comment_invalid|watch_invalid|inbox_invalid` |
| CAS、state transition 或 idempotency conflict/in-progress | closed `409`，无 partial durable state |
| missing/denied/policy denied/deletion reserved/source gone | opaque `404 resource_not_found` |
| admission/membership/source/fence/inbox/provider dependency unavailable | content-free `503 record_service_unavailable` 或 worker retry；不返回 cause |
| outbox claim owner/generation/expiry/event tuple drift | `ErrLostOwnerLease`；零 projection/finalize |
| captured auth/fence 与 current 不同 | cancel typed notification；零 recipient/delivery |
| current recipient/watch policy 不再允许 | projection replay reconcile/prune；读路径跳过或 opaque missing |
| source loader true dependency error | `ErrInboxUnavailable` → no-store 503，不折叠为 404 |
| unknown provider outcome 或 crash after send | terminal `unknown_outcome`；不得 resend |
| portability oversize/sparse/drift/orphan/content restore | `ErrInvalidPortabilitySnapshot`；pre-mutation fail closed/rollback |

所有 HTTP 响应使用 `Cache-Control: private, no-store`。handler/store/worker error 和日志
不得输出 ID、Markdown、render model、payload、credential、webhook/token 或 provider body。

## 5. Good / Base / Bad Cases

- Good：comment create 在 admitted transaction 内保存 current+revision+mentions，写
  identity-only outbox；随后 projection 重新授权后生成 inbox，render 时只使用闭合 model。
- Good：notification projection 已 commit、finalize 前 crash；新 generation takeover 重新
  计算 current recipients，幂等 reconciliation 后 owner-fenced sent。
- Good：portable redacted comment backup/restore 只保留 tombstone 和 content-free audit；
  restore 后再次 backup exact equal。
- Base：真实 AdmissionGate 或 scoped external binding 未配置；route/业务保持 fail-closed，
  external delivery 不成为完成依赖。
- Bad：把 access-group visibility 当成 project membership，或在 caller tx 外查 assignee。
- Bad：把 comment source/render、action details、recipient 或 provider response 写入 outbox、
  notification audit、日志或 external message。
- Bad：给 collaboration 表 runtime raw DELETE，或另建 cleanup/purge orchestrator。
- Bad：portability 逐 row QueryRow 校验 provenance，或接受 sparse history 后回写 current。

## 6. Tests Required

- Domain/unit：`internal/center/recordcollaboration/*_test.go` 覆盖 state machine、typed-nil、
  closed enums、fingerprint、recipient matrix、Markdown shared corpus、provider bounds/history。
- Store/unit：`internal/center/store/record_{actions,comments,watches,notifications}*_test.go`
  覆盖 transaction order、rollback cut points、claim full tuple、bounded keyset/reconcile。
- Real PostgreSQL：使用
  `scripts/test-record-platform-integration.sh postgres -- go test ...`，不得以 `SKIP`
  作为证据；覆盖 lifecycle/replay/CAS、real blocking、takeover/stale finalizer、redaction、
  recipient revoke、external outcomes、deletion/restore rollback。
- Migration/ACL：fresh + exact repeat + current runtime/admin ACL；runtime raw DELETE 必须
  `42501`，closed wrappers 与 immutable triggers 必须真实执行。
- Race/vet：affected packages `go test -race`、`go vet`、`make verify-go`。
- HTTP：每条 route 的 allowlist、non-nil `[]`、opaque 404、closed 409/422/503、
  malformed query/header/body 的 application 零调用。
- Privacy：扫描新增行，确认无 body/markdown/html/json/payload/details/evidence/credential/
  provider response 进入 identity-only durable surfaces 或 ordinary worker logs。

## 7. Wrong vs Correct

### Wrong

```go
// visibility 不是 membership；read outside caller tx 还会产生 TOCTOU。
if actor.CanRead(record) {
    saveAssignee(actor.UserID)
}

// 网络发送进入业务 transaction，且错误可能泄露 provider body。
return repository.RunRecordPlatformTransaction(ctx, func(tx *Transaction) error {
    saveComment(tx, body)
    return telegram.Send(ctx, body)
})
```

### Correct

```go
return repository.RunRecordPlatformTransaction(ctx, func(tx *Transaction) error {
    member, err := members.ReadMemberActor(ctx, tx.PGX(), "default", assigneeID)
    if err != nil { return err }
    if err := authorizeCurrentRecordAndFence(ctx, tx, member); err != nil { return err }
    // business/history/activity/identity-only outbox/idempotency complete atomically
    return persistCollaborationFacts(tx)
})

// claim commit 后，prepare 重新授权；provider 在 tx 外；finalize 再绑定 owner tuple。
prepared, err := store.PrepareExternalDelivery(ctx, claim, publicBaseURL)
if err != nil { return err }
outcome := prepared.Binding.Provider.SendExternalDelivery(sendCtx, prepared.Message)
return store.FinalizeExternalDelivery(ctx, claim, prepared.Attempt, outcome, retryAfter)
```
