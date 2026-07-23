# 负责人、行动项、评论、关注与通知设计

## 1. 边界、顺序与现状

当前 `experience_logs` 没有负责人、评论或个人通知；`notification_records` 是 incident Telegram/飞书投递审计，TopBar bell 只是 `/events?notification_only=1` 的链接。`asset_decision_record_members.followup_*` 属于资产决策成员执行记忆，不能提升为通用记录协作模型。现有 `internal/center/notify/{telegram,feishu}.go` 可作为网络 transport，但其同步调用、全局 channel 语义和错误正文不满足记录授权与重试合同。

本任务在 platform/core 合入后执行，拥有 `0056_create_record_collaboration.sql`。父任务刻意让它先于 Markdown child，以便后者直接消费稳定的 owner/action/comment/follow contract。`0055`是后续search migration的保留号，当前分支不会拥有该真实文件；0056不引用search表。本任务以真实0056验证0055缺席路径，并以real-PostgreSQL/custom migration-filesystem fixture验证ledger已有较大文件名后仍补应用缺失较小文件名；实际0055→0056和已记录0056→实际0055的schema/data/repeat测试由search child拥有。

## 2. 模块与依赖方向

```text
recordauth + recordplatform
          ↑
records revision participant ──> records/actions/comments/followers
          │                                 │
          └─────────────────────────────────> recordnotify
                                                    │
                                      settings + notify transports
```

- `internal/center/records/` 继续拥有 owner/participants/follow-up revision validation，以及 action/comment/follower 领域对象；它不调用外部网络。
- `internal/center/recordnotify/` 只消费不可变 domain/outbox event 和 `recordauth.Policy`；它不成为记录、评论或行动项的权威存储。
- `internal/center/store/record_collaboration.go` 与 `record_notifications.go` 提供显式 pgx transaction seam；业务事务与 outbox/inbox decision 原子写入。
- handler只做严格JSON、条件头、幂等键、错误映射；按backend一文件一资源拆为collaborators、actions、comments、follow、notifications和notification bindings，recipient、聚合、授权和模板选择位于service。
- Web canonical DTO追加到`web/src/lib/types.ts`；仅lazy records/inbox routes消费的协作transport扩展`web/src/lib/recordsApi.ts`，受控组件不直接`fetch`。AppShell/TopBar不得导入lazy façade；`NotificationBell`唯一使用的最小authorized unread-count helper留在eager `web/src/lib/api.ts`。`NotificationInboxPage`是本任务唯一新增完整route，Markdown child再把协作面板装入record detail/editor。

## 3. `0056` 数据合同

| 表 | 权威内容与约束 |
|---|---|
| `record_actions` | current projection：record/project、正文、status、assignee、due/completed、可空 revision subject identity、version、created/updated actor/time；record 内部显式 purge，不依赖来源 cascade |
| `record_action_events` | append-only 完整 action snapshot、event kind/reason、event no、actor/time、idempotency fingerprint；唯一 `(action_id,event_no)` 与 `(action_id,idempotency_fingerprint)` |
| `record_comments` | record、author、reply target、current revision、created/edited/deleted time、version、tombstone；回复目标必须属于同 record |
| `record_comment_revisions` | revision identity/no、editor/time、change kind为append-only metadata；未删除时保存safe Markdown source/canonical hash。DB one-way-redaction invariant只允许tombstone transaction将current及此前revision的source/render/cache/hash从non-null→null；metadata更新、null→non-null和tombstone后正文revision由trigger/constraint拒绝 |
| `record_comment_commands` | POST/PATCH/DELETE持久幂等结果：actor/route、HMAC key hash、keyed normalized-command fingerprint、comment/revision/tombstone/version、created time；唯一actor+route+key hash，不保存原key或正文，整record永久删除时清除 |
| `record_comment_revision_mentions` | comment revision 到稳定 user ID；显示 label 不是 identity，唯一且按项目/记录授权校验 |
| `record_followers` | record/user current preference、`manual_state=inherit|following|muted`、当前 author/owner/participant source flags、version；强制通知不读取 mute |
| `record_follower_events` | source/preference 的 append-only变化，用于审计、导出和重建 current projection |
| `user_notifications` | user-owned inbox projection：event family、generic summary code/安全参数、record deep-link ref、aggregate count/window、read/hidden/expiry；不保存正文/评论/行动项文本 |
| `record_notification_events` | 不可变 recipient decision：domain event、recipient、mandatory/optional、reason、aggregation key、policy revision；同 event/recipient/family 唯一 |
| `record_notification_channel_bindings` | 管理员声明的 project/group/user audience、Telegram/飞书 integration ref/revision、enabled/version/verified time；secret 仍在 `center_settings` |
| `record_notification_deliveries` | event/binding 当前投递状态、attempt、next attempt、safe error code、sent/cancelled time；不保存渲染正文或第三方 message body |
| `record_notification_delivery_attempts` | append-only attempt metadata、integration revision、policy/fence revision、HTTP status class、safe outcome；不保存 recipient secret/message ID/content |
| `record_collaboration_purge_receipts` | deletion operation、adapter version、各表/外部取消计数、无身份 external-copy aggregate/digest；不保存被删文本 |

0056同时给`center_settings`增加`record_notification_settings` JSONB（默认external disabled、due-soon 24h、overdue repeat 24h）；它只保存策略，不保存Telegram/飞书secret。迁移为 core 的 current owner/follow-up/participant projection增加必要索引，但不复制这些字段为第二权威。已有 records 的 author/owner/participant 自动来源由幂等 `FollowerReconciler` 从 current revision补齐，避免 migration 依赖 core 实际列布局的隐式猜测。

普通 action 不提供 DELETE；取消是可审计状态。普通comment删除保留comment row、reply relation、actor/time、revision metadata和tombstone，但在同一事务清除current与全部历史revision的Markdown、rendered HTML/cache及content-derived hash；API/activity/notification/export只能读取无正文metadata/tombstone。0056的trigger/受控store statement把这次内容redaction定义为唯一可变例外：只接受deleted flag/version推进时non-null→null，revision identity/editor/time/change kind永不UPDATE，null→non-null、删除后INSERT带正文revision和旧version writer全部拒绝。整条记录永久删除再由deletion adapter清除所有action/comment/follower/notification逻辑身份。

## 4. 领域与事务合同

### 4.1 Revision collaboration participant

`CollaborationRevisionParticipant` 注册到 core `RevisionParticipant`。在同一正式保存事务内比较 base/current input：

1. 只按稳定user ID校验owner/participants是current project members，并在同一事务用本次revision的post-save visibility与全部source authorization floor交集执行目标用户record read policy；跨项目、restricted group外、visibility收窄后不可读或source floor拒绝均rollback，不按username/display name推断。
2. 校验跟进开关和 `next_followup_at`；无状态类型未显式启用时必须为空。
3. 写 current owner/participant/follow-up projection。
4. 仅在第1步post-save授权通过后追加assignment/follow-up domain event。
5. 仅为仍有post-save read权限的目标更新自动follower source flags，但保留用户`manual_state=muted`。
6. 写 notification decision/outbox；actor 本人从 recipient 集合排除。

任一步失败使 revision、projection、follower、activity 和 outbox 全部回滚。恢复旧 revision 走相同 participant，因此 owner/participant/follow-up 与关注来源也恢复为该 revision 的值，并产生新的当前变化事件，而不是改写旧历史。

### 4.2 行动项

```go
type ActionCommand struct {
	RecordID          string
	ActionID          string
	ExpectedVersion   int64
	IdempotencyKey    string
	Content           string
	Status            ActionStatus
	AssigneeUserID    string
	DueAt             *time.Time
	SubjectRelationID string
	ReasonCode        string
}
```

- `AssigneeUserID` 允许为空；非空时create/update都按稳定user ID重新验证current project membership和该用户的record read policy，display name/username不参与identity，撤权后旧assignee只作为历史event metadata显示且不能被重新选择。只有实际为空且该行动项需要处理时才产生无负责人提示，不渲染正常态占位。`SubjectRelationID` 必须指向 current revision 已有 primary/related relation；行动项不能暗中扩大记录的 source authorization scope。
- `done` 必须写 `completed_at`，离开 done 清 current completion time但旧 event保留；`cancelled` 要求 reason code。blocked reason只作为有界枚举 + 可选安全正文存在 action event，不进入普通 telemetry。
- create/update 都要求 `Idempotency-Key`；PATCH 还要求 `If-Match` action version。相同 key/fingerprint 返回原 event，不同 fingerprint 409；版本冲突返回 current action 和字段级 merge input。
- 全部 done 只写 `record_status_change_suggested` 通知/活动，不调用 core revision service。

### 4.3 评论与提及

comment dialect `houfeng-comment-markdown/v1` 是 CommonMark 的段落、强调、列表、引用、inline/fenced code 和安全链接子集；raw HTML、image、`houfeng-evidence:`、`houfeng-attachment:`、`data:`、`javascript:` 和 iframe/style 全部拒绝。服务端使用固定 Goldmark/Bluemonday版本生成安全结构，Web 使用 react-markdown/rehype-sanitize 且不启用 raw HTML；双方共享 `testdata/markdown/houfeng-comment-v1.json` hostile/golden corpus。

提及语法为 `@[显示名](houfeng-user:<user_id>)`。parser 只信稳定 ID；保存时查询 project collaborator并对目标执行 record read policy。comment edit 计算 old/new mention ID set，仅 `new-old` 生成强制通知。reply target只提供一层上下文；回复另一条 reply 仍保存目标 ID，但 API 展示平铺且只附目标摘要/tombstone。

作者可在仍有`record.comment.write`时编辑/删除自己的评论；项目管理员的`record.comment.moderate`只能删除他人评论并记录reason code，不能编辑为管理员文字。POST/PATCH/DELETE都要求`Idempotency-Key`，PATCH/DELETE还要求`If-Match` comment version；key scope包含actor/route/comment、fingerprint包含normalized command/base version，同key同fingerprint返回原comment revision/tombstone，同key异请求或stale version返回409且mentions/domain event/outbox不变。delete transaction先追加无正文tombstone revision，再通过受控one-way statement清current与全部历史revision的source/render/cache/hash内容列，并让activity/export只见metadata；任一清除失败使tombstone、domain event和outbox全部rollback。删除后任何service/SQL stale writer都不能恢复正文或添加带正文revision。

## 5. 关注、recipient 与通知矩阵

effective optional follow/watch 规则：产品只显示“关注”；`manual_state=following` 总是订阅，`manual_state=muted` 抑制 follower/revision 一般通知，`inherit` 在作者/负责人/参与者任一 source flag存在时订阅。source 移除不清手动 following/muted历史。强制通知不读取 optional follow状态。

| 事件 | 强制 recipient | 可选 recipient | 去噪 |
|---|---|---|---|
| mention | 新增 mention user | 无 | actor/self 排除；每 comment revision set diff |
| record owner/participant assignment | 新增 assignee/participant | optional followers | 同一正式 revision 聚合 |
| action assignment | 新 assignee | optional followers | action event 幂等 |
| comment reply | 被回复评论作者 | optional followers | actor/self 排除 |
| action blocked | assignee | optional followers | 仅进入 blocked 或 reason 实际变化 |
| follow-up/action due soon | owner/assignee | optional followers | 首次跨越 T-24h 一次 |
| overdue | owner/assignee | optional followers | 首次跨越 due + 未完成期间每 24h最多一次 |
| business status / formal revision | 无 | optional followers | 同 record/event family 5 分钟窗口聚合 |
| permission/security | 受影响 user | 无 | 不允许 mute；只用 generic safe code |

站内notification聚合保留第一个/最后一个event time、数量和安全event code，不拼接正文。读取在一个policy snapshot/transaction中先选candidate，再逐row重新授权并把失权row原子转hidden、清summary params；随后只从authorized projection计算page rows、`unread_count`和cursor，绝不先count后filter。policy revision在hide/count/response间变化则整次读取重试或返回安全stale结果，不返回旧count/cursor。mark-read只操作当前user且幂等。

## 6. 外部投递安全合同

binding 是管理员对实际 Telegram chat/飞书 webhook audience 的显式声明；迁移不自动把现有 incident integration变成 record channel。初始 `enabled=false`。当前单 admin部署可创建 project audience binding；未来 group/user binding只有在 policy能验证 audience stable IDs 时才启用。

投递条件是 `record effective scope ⊇ declared binding audience` 且该 event至少一个当前授权 recipient落入 audience。restricted record不能投递到 project-wide binding。多个 user notification映射到同 event/binding只发送一次。

外部模板固定只包含产品名、事件类别短句、站内 HTTPS deep link和发生时间桶，例如“候风：你有一条新的记录分配，登录后查看”。禁止标题、主体名、actor display、comment/action text和材料摘要。outbox只存 template version与引用；worker每次 attempt重新渲染。`notify` transport错误必须映射为 `timeout|rate_limited|remote_4xx|remote_5xx|integration_disabled` 等安全 code，不把第三方 response body写入 DB/log。

退避为 1m、5m、30m、2h、12h，最多 5 次；429尊重有界 `Retry-After` 但不超过 12h。永久 4xx、binding/integration失效、撤权和 fence进入 cancelled，不重试。网络发送前最后一次 fence/policy检查与 attempt reservation属于同一 worker claim generation；结果未知按可能已交付记录 external-copy disclosure，不盲目重复。

## 7. API 与授权

| Method / path | Capability 与语义 |
|---|---|
| `GET /api/record-collaborators` | `record.collaborator.list`；项目内 allowlisted ID/display name游标检索，不是通用用户管理 |
| `GET/POST /api/records/:id/actions` | record read / `record.action.write`；list + create |
| `PATCH /api/record-actions/:action_id` | `record.action.write` + If-Match + Idempotency-Key；无 DELETE |
| `GET/POST /api/records/:id/comments` | record read / `record.comment.write`；flat cursor list；POST要求Idempotency-Key |
| `GET/PATCH/DELETE /api/record-comments/:comment_id` | read；author write或admin moderate删除；GET含revision metadata，PATCH/DELETE要求Idempotency-Key+If-Match |
| `GET /api/record-comments/:comment_id/revisions` | record read；未删除时按当前授权返回正文，删除后只返回revision metadata/tombstone且任何历史正文bytes为0 |
| `PUT/DELETE /api/records/:id/follow` | `record.follow.manage`；PUT following，DELETE muted，均幂等 |
| `GET/PATCH /api/notifications` | 仅当前 user；list/unread与mark-read/mark-all-read |
| `GET/POST/DELETE /api/notification-channel-bindings` | project admin；显式 audience/integration revision配置与验证 |

record owner/participants/follow-up不增加旁路 PATCH，继续通过 core `POST /api/records/:id/revisions`。所有 nested object先通过其 record ID执行 `recordauth.Policy`；无权与不存在统一 `resource_not_found` 404。关键错误码：`record_action_conflict`、`record_comment_conflict`、`record_comment_deleted`、`mention_target_unavailable`、`notification_binding_scope_mismatch`、`notification_delivery_unavailable`；版本/幂等冲突409，字段422，暂时依赖503。

## 8. Worker、保留与失败

- `RecordNotificationScheduler`：按 UTC bucket扫描 next follow-up/action due，写幂等 domain event；fake clock验证 T-24h/overdue/DST。
- `RecordNotificationWorker`：claim inbox/outbound event，fresh auth/render/send，安全退避。
- `RecordCollaborationJanitor`：清 180 天通知/delivery；永久删除走立即 adapter，不等待 TTL。
- `FollowerReconciler`：从 current revision重建自动 source flags并对账，不覆盖 manual preference。

business transaction失败不产生 action/comment/follow/outbox半状态。inbox projection失败由 immutable event重建；外投失败只改变 delivery。worker crash在 claim前/渲染后/send前/send后分别产生 retry、cancelled或 possible-delivery，幂等 identity阻止确定成功后的重复。

## 9. Activity、导出与永久删除

action event和comment revision/tombstone实现 `CollaborationActivityProvider`，提供stable source kind/ID/version、event/recorded time、actor与安全摘要；comment activity从创建起只用generic event code和无正文metadata，不复制comment Markdown/摘录。删除后provider只返回tombstone metadata并核验任何旧projection/cache的comment正文bytes为0；task 7将其投影到canonical activity，不复制权威正文。

`CollaborationExportProvider` 输出版本化 canonical DTO：current owner/participants/follow-up、全部action events、未删除comments/revisions、已删除comment的revision metadata/tombstone、reply/mention provenance和当前follower preference。deleted comment source/render/hash不可从历史row或cache恢复，human/machine export都只输出tombstone metadata。provider不导出inbox unread、外部重试任务、channel secret或已投递message ID；machine import将外部follower/notification只视为provenance，不自动订阅或重发。

recorddeletion adapter在 reservation后拒绝全部 mutation/notification claim，取消 scheduled/outbox/processor工作，清 action/comment/follow/inbox/delivery关联，并返回外部已交付类别/数量。只有 adapter receipt验证内容和关联均为0才能 `online_purged`；`not_committed` 不调用 adapter。导出/删除并发服从同一 source version + reservation epoch。

## 10. Web 体验

- `RecordCollaborationFields` 只在适用或显式启用跟进时展示 owner/participants/follow-up；错误字段展开，正常态不展示“无负责人/无逾期”。
- `RecordActionList` 无数据时显示用途明确的空态和创建入口；blocked/overdue只在实际 row上显示文字+图标+颜色，不创建页面级空异常槽。
- `RecordComments` 使用flat timeline、reply context和“评论已删除”tombstone；未删除评论的编辑历史按需展开，删除后只展开revision metadata而无旧正文，不把reply视觉缩进成无限树。
- `RecordFollowButton` 明确 following/muted状态；强制通知说明不伪装成可关闭。
- `/notifications`区分首次空、筛选无结果、局部错误和revoked；route controller调用lazy `recordsApi.ts`。TopBar Bell只调用eager `api.ts`的最小authorized unread helper，count只在`>0`时出现，不能把`recordsApi.ts`拉入AppShell entry chunk。
- 390px使用单列和受控 modal/drawer，44px触摸目标、focus restore、Axe与无横向溢出；协作组件由随后 Markdown route集成，不在 `VPSDetailPage` 增加新状态。

## 11. 兼容与回滚

0056只增加表/索引；records feature关闭时不启动新 worker、不改变旧 experience/incident notification行为。现有 Telegram/飞书配置仍服务 incidents/subscription reminders；record channel必须单独 binding启用。回滚关闭 collaboration capability和worker，保留 action/comment/inbox数据只读，不执行 down migration。已产生 comment/action历史不可被旧 binary改写；Markdown child必须从包含0056的主线开始。
