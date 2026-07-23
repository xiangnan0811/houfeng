# 负责人、行动项、评论、关注与通知 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`. Codex inline execution only; do not dispatch implement/check sub-agents. Follow RED → verify RED → minimal GREEN → verify GREEN for every production behavior.

**Goal:** 交付与完整 revision 一致、具有独立历史并在权限变化/永久删除后安全收敛的记录协作与通知层。

**Architecture:** owner/participants/follow-up由 core revision participant原子处理；actions/comments/followers保存在 records domain并追加不可变事件；recordnotify从 platform outbox生成个人 inbox与权限安全外投。全部路径复用 recordauth、reservation/fence和 deletion adapter。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL、Goldmark 1.8.4、Bluemonday 1.0.27、React 19、TypeScript 6、react-markdown 10.1.0、remark-gfm 4.0.1、rehype-sanitize 6.0.0、Vitest/Testing Library、Playwright/axe。

---

## Preconditions and sequencing gate

- [ ] 确认 `07-14-vps-records-platform-foundation`、`07-14-vps-records-core` 已合入受保护主线且 post-merge required CI 通过；从该主线创建非 main分支并运行 `sh scripts/setup-git-hooks.sh`。
- [ ] 确认应用migration `0051`–`0054`已存在且冻结、`0055`尚可缺失、`0056_create_record_collaboration.sql`未被占用；如果主线新增migration占用未发布编号，只整体顺延仍未实施的0055–0060并同步相关sibling文档/测试，绝不改号或改写已合入的0051–0054。
- [ ] 运行 `python3 ./.trellis/scripts/get_context.py --mode phase --step 2.1 --platform codex` 与 `trellis-before-dev`，读取 backend database/error/logging/quality、web component/state/styling/quality和cross-layer/reuse规范。
- [ ] 记录 fresh baseline：`make verify-go`、Node 22下 `make verify-web`、`go test ./internal/center/incidents ./internal/center/subscriptioncosts ./internal/center/notify -count=1`；预期全部 PASS。

## Task 1: 0056 schema、领域类型与历史不变量

**Files:**

- Create: `db/migrations/0056_create_record_collaboration.sql`
- Modify (existing): `internal/center/store/migrate/migrate_test.go`
- Modify (existing): `internal/center/store/migrate/postgres_integration_test.go`
- Create: `internal/center/records/collaboration_types.go`
- Create: `internal/center/records/collaboration_validate.go`
- Create: `internal/center/records/collaboration_validate_test.go`
- Create: `internal/center/store/record_collaboration.go`
- Create: `internal/center/store/record_collaboration_test.go`

- [ ] 写migration source/domain RED tests，逐表断言design §3的PK/unique/FK/CHECK/index、notification安全默认、append-only event序号、同record reply约束、无来源cascade、正文与审计列分离、180天expiry和purge receipt；`record_comment_commands`唯一scope/HMAC key hash/keyed fingerprint/result且无原key/正文。专测comment redaction trigger只允许tombstone时content/render/cache/hash non-null→null，拒绝metadata UPDATE、null→non-null、旧version writer和deleted comment带正文INSERT。运行focused tests预期因0056/types缺失FAIL。
- [ ] 实现0056与action/comment/follower/notification value types及comment one-way-redaction DB invariant；时间统一UTC、slice/map defensive copy、正文长度/状态/transition/ID规范由服务端验证。复跑focused/source/真实PG tests，预期PASS。
- [ ] 新增两条可在当前分支运行的真实PostgreSQL路径：仅应用0051–0054后直接应用真实0056并repeat；使用custom migration filesystem先记录较大测试migration，再加入缺失的较小文件名并证明migrator补应用且repeat不漂移。不要在本任务伪造future search 0055；实际0055→0056和已记录0056→实际0055的schema/data tests由search child执行。
- [ ] 使用 `HOUFENG_POSTGRES_INTEGRATION=1 HOUFENG_DATABASE_URL="$HOUFENG_DATABASE_URL" go test ./internal/center/store/migrate ./internal/center/store -run 'PostgresIntegration.*RecordCollaboration|RecordCollaboration' -count=1`；预期 PASS，不允许以 SKIP作为交付证据。

## Task 2: Revision owner/participants/follow-up participant

**Files:**

- Create: `internal/center/records/collaboration_revision.go`
- Create: `internal/center/records/collaboration_revision_test.go`
- Modify (created by core dependency): `internal/center/records/service.go`
- Modify (created by core dependency): `internal/center/store/records.go`
- Modify (created by core dependency): `internal/center/records/revisions.go`

- [ ] 写transaction-order RED tests：owner/participant current-project membership、post-save record/source-floor read、跨project、restricted-group外、同revision visibility收窄、source floor拒绝、增删、follow-up显式开关、无状态类型隐藏、restore old revision、actor/self去噪，以及projection/follower/activity/outbox任一步失败整体rollback。
- [ ] 实现`CollaborationRevisionParticipant`并在core registry显式注册；owner/participants/follow-up不增加旁路更新接口。先按post-save visibility+source floor验证全部非空owner/participants，再在正式revision事务内写current projection、授权目标的自动follower source、domain event和outbox；任何目标失败整条revision rollback。
- [ ] 写真实PG并发测试：两个base revision只有一个成功；失败方的 follower/notification/action计数不变；恢复旧revision产生新revision并恢复完整协作字段。
- [ ] 运行 `go test -race ./internal/center/records ./internal/center/store -run 'CollaborationRevision|Owner|Participant|Followup' -count=10`；预期10轮PASS且race为0。

## Task 3: 行动项 command、CAS、活动与筛选 contract

**Files:**

- Create: `internal/center/records/actions.go`
- Create: `internal/center/records/actions_test.go`
- Create: `internal/center/records/collaboration_filters.go`
- Create: `internal/center/records/collaboration_filters_test.go`
- Modify (created in Task 1): `internal/center/store/record_collaboration.go`
- Modify (created in Task 1): `internal/center/store/record_collaboration_test.go`

- [ ] 写RED matrix覆盖create、全部状态、done/completed time、blocked reason、cancel reason、内容/assignee/due/subject变化、If-Match冲突、同key重试、同key异请求、无变化、全done建议与reservation竞态；assignee覆盖current project member+record read、跨project、撤权、display-name伪造与更新时重新校验。
- [ ] 实现action service；非空assignee只按稳定user ID查询并在每次create/update执行project membership+record read policy，关联主体只接受current revision已有relation ID；每次实际变化追加完整event，同事务写activity/outbox，不提供DELETE或业务对象writer。
- [ ] 为后续 search child定义并测试 `CollaborationFilter{OwnerIDs,ParticipantIDs,FollowupState/From/To,ActionStatuses,ActionAssigneeIDs,ActionDueState/From/To}`；同字段OR、字段组AND，action使用SQL `EXISTS`且同一record最多一行，不在本任务创建0055对象。
- [ ] 运行 `go test -race ./internal/center/records ./internal/center/store -run 'RecordAction|ActionFilter' -count=10`；预期 PASS，记录revision数在quick action更新后不变。

## Task 4: 安全评论、回复、提及、编辑与 tombstone

**Files:**

- Create: `testdata/markdown/houfeng-comment-v1.json`
- Create: `internal/center/records/comment_markdown.go`
- Create: `internal/center/records/comment_markdown_test.go`
- Create: `internal/center/records/comments.go`
- Create: `internal/center/records/comments_test.go`
- Modify (created in Task 1): `internal/center/store/record_collaboration.go`
- Modify (existing): `go.mod`
- Modify (existing): `go.sum`
- Modify (existing): `web/package.json`
- Modify (existing): `web/package-lock.json`

- [ ] 写共享hostile/golden corpus与Go RED tests，覆盖raw HTML、script/event attrs、image/data/javascript URL、evidence/attachment scheme、深层reply、伪造mention label/ID、跨project/无权mention、edit set-diff和delete tombstone；POST/PATCH/DELETE response-loss retry、同key异fingerprint、stale If-Match、并发delete/edit与SQL旧writer全部覆盖。删除后逐表/cache/API/activity/notification/export断言current与全部历史revision正文bytes/hash命中0且metadata/reply仍在。
- [ ] 固定父计划批准的 Goldmark/Bluemonday版本，实现comment parser、安全render model和mention extractor；原始HTML永不透传，display label永不用于identity。
- [ ] 实现comment create/edit/delete transaction；POST/PATCH/DELETE强制Idempotency-Key，PATCH/DELETE强制If-Match，持久化key scope+fingerprint+result revision/tombstone。作者只能改自己的正文，moderator只能带reason删除他人；普通删除在同一事务追加无正文tombstone revision、经one-way store statement清current与全部历史revision内容列并保留reply/revision metadata。任一CAS/redaction失败使mention/domain/outbox全部rollback。
- [ ] 在Web lockfile固定 react-markdown/remark-gfm/rehype-sanitize批准版本，先只供lazy collaboration组件消费；紧随其后的 Markdown child复用依赖/corpus，不再建立第二套宽松评论parser。
- [ ] 运行 `go test ./internal/center/records -run 'CommentMarkdown|RecordComment|Mention' -count=1`，预期 PASS；再运行 corpus fuzz `go test ./internal/center/records -run Fuzz -fuzz=FuzzCommentMarkdown -fuzztime=30s`，预期无panic/unsafe output。

## Task 5: 关注投影、recipient决策与调度扫描

**Files:**

- Create: `internal/center/records/followers.go`
- Create: `internal/center/records/followers_test.go`
- Create: `internal/center/recordnotify/types.go`
- Create: `internal/center/recordnotify/recipients.go`
- Create: `internal/center/recordnotify/recipients_test.go`
- Create: `internal/center/recordnotify/scheduler.go`
- Create: `internal/center/recordnotify/scheduler_test.go`
- Create: `internal/center/store/record_notifications.go`
- Create: `internal/center/store/record_notifications_test.go`

- [ ] 写 RED table固定 `inherit|following|muted`、author/owner/participant source变化、mandatory绕过mute、actor/self排除、同event recipient dedupe与权限交集。
- [ ] 实现 follower current projection + append-only events和 recipient decision；source移除不抹去manual preference，reconciler可从current revision重建。
- [ ] 用fake clock写 T-24h、due、每24h overdue、blocked transition、UTC/DST、取消/完成后的RED tests；实现scheduler bucket idempotency和可取消lease。
- [ ] `go test -race ./internal/center/records ./internal/center/recordnotify ./internal/center/store -run 'Follower|Recipient|NotificationScheduler' -count=10`；预期 PASS、重复inbox/outbox为0。

## Task 6: Inbox、scope-bound外投、重试与安全错误

**Files:**

- Create: `internal/center/recordnotify/inbox.go`
- Create: `internal/center/recordnotify/inbox_test.go`
- Create: `internal/center/recordnotify/bindings.go`
- Create: `internal/center/recordnotify/bindings_test.go`
- Create: `internal/center/recordnotify/templates.go`
- Create: `internal/center/recordnotify/templates_test.go`
- Create: `internal/center/recordnotify/worker.go`
- Create: `internal/center/recordnotify/worker_test.go`
- Create: `internal/center/recordnotify/channels.go`
- Modify (existing): `internal/center/notify/telegram.go`
- Modify (existing): `internal/center/notify/telegram_test.go`
- Modify (existing): `internal/center/notify/feishu.go`
- Modify (existing): `internal/center/notify/feishu_test.go`
- Modify (existing): `internal/center/settings/types.go`
- Modify (existing): `internal/center/settings/types_test.go`
- Modify (existing): `internal/center/store/settings.go`
- Modify (existing): `internal/center/store/settings_test.go`

- [ ] 写RED matrix覆盖unread隔离/聚合/mark-read、binding默认关闭、project/group/user scope、restricted→project拒绝、integration revision漂移、撤权、fence、429/4xx/5xx/timeout、五档退避、send结果未知和成功幂等；在candidate scan、row authorization/hide、count、cursor、response之间逐点撤权，断言row/title/summary/unread count/cursor均零泄露且无pre-filter count。
- [ ] 实现inbox与binding service；同一policy snapshot先隐藏/清summary params，再从authorized projection计算page/count/cursor，policy漂移则重试/安全stale。settings只增加record-notification显式开关/default thresholds，不自动继承incident notification enable。
- [ ] 实现generic external templates，hostile record/comment/action/evidence/secret字段均不能进入rendered bytes；每次claim/render/send前fresh authorize/fence/binding。transport返回typed safe error，不持久化/记录第三方response正文。
- [ ] 用httptest Telegram/飞书server运行 worker cutpoint tests；`go test -race ./internal/center/recordnotify ./internal/center/notify ./internal/center/settings ./internal/center/store -run 'Notification|Telegram|Feishu' -count=10`，预期 PASS。

## Task 7: HTTP、router、bootstrap与worker lifecycle

**Files:**

- Create: `internal/center/http/handlers/record_collaborators.go`
- Create: `internal/center/http/handlers/record_collaborators_test.go`
- Create: `internal/center/http/handlers/record_actions.go`
- Create: `internal/center/http/handlers/record_actions_test.go`
- Create: `internal/center/http/handlers/record_comments.go`
- Create: `internal/center/http/handlers/record_comments_test.go`
- Create: `internal/center/http/handlers/record_follow.go`
- Create: `internal/center/http/handlers/record_follow_test.go`
- Create: `internal/center/http/handlers/record_notifications.go`
- Create: `internal/center/http/handlers/record_notifications_test.go`
- Create: `internal/center/http/handlers/record_notification_bindings.go`
- Create: `internal/center/http/handlers/record_notification_bindings_test.go`
- Modify (existing): `internal/center/http/router.go`
- Modify (existing): `internal/center/http/router_test.go`
- Modify (existing): `internal/center/http/router_api_test.go`
- Modify (existing): `cmd/houfeng-center/bootstrap.go`
- Modify (existing): `cmd/houfeng-center/bootstrap_test.go`
- Modify (existing): `internal/center/app/app.go`
- Modify (existing): `internal/center/app/app_test.go`

- [ ] 先写 handler/router RED matrix固定 design §7全部method/path、静态/动态优先级、严格body key、If-Match/Idempotency-Key、400/404/409/422/503、cursor、capability与response allowlist。
- [ ] 按backend“一文件一资源”实现collaborators/actions/comments/follow/notifications/notification-bindings handlers和显式RouterOptions；nested action/comment从服务端解析record identity并执行同一policy，绝不信客户端project/actor字段。
- [ ] bootstrap显式构造store/services/participant/scheduler/worker/transport；records或external record notifications关闭时不claim任务，旧incident/subscription notifier仍工作。
- [ ] 运行 `go test ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center ./internal/center/app -run 'RecordCollaboration|RecordNotification|Bootstrap|Worker' -count=1`；预期 PASS且worker count断言更新为实际值。

## Task 8: Web contract、受控协作组件、收件箱和binding设置

**Files:**

- Modify (existing, extended by core dependency): `web/src/lib/types.ts`
- Modify (created by core dependency): `web/src/lib/recordsApi.ts`
- Modify (created by core dependency): `web/src/lib/recordsApi.test.ts`
- Modify (existing): `web/src/lib/api.ts`
- Modify (existing): `web/src/lib/api.test.ts`
- Create: `web/src/pages/records/collaboration/RecordCollaborationFields.tsx`
- Create: `web/src/pages/records/collaboration/RecordCollaborationFields.test.tsx`
- Create: `web/src/pages/records/collaboration/RecordActionList.tsx`
- Create: `web/src/pages/records/collaboration/RecordActionList.test.tsx`
- Create: `web/src/pages/records/collaboration/PromoteChecklistActionDialog.tsx`
- Create: `web/src/pages/records/collaboration/PromoteChecklistActionDialog.test.tsx`
- Create: `web/src/pages/records/collaboration/RecordComments.tsx`
- Create: `web/src/pages/records/collaboration/RecordComments.test.tsx`
- Create: `web/src/pages/records/collaboration/CommentMarkdown.tsx`
- Create: `web/src/pages/records/collaboration/CommentMarkdown.test.tsx`
- Create: `web/src/pages/records/collaboration/RecordFollowButton.tsx`
- Create: `web/src/pages/records/collaboration/RecordFollowButton.test.tsx`
- Create: `web/src/pages/NotificationInboxPage.tsx`
- Create: `web/src/pages/NotificationInboxPage.test.tsx`
- Create: `web/src/app/layout/NotificationBell.tsx`
- Create: `web/src/app/layout/NotificationBell.test.tsx`
- Modify (existing): `web/src/app/layout/TopBar.tsx`
- Modify (existing): `web/src/app/layout/TopBar.test.tsx`
- Modify (existing): `web/src/pages/SettingsPage.tsx`
- Modify (existing): `web/src/pages/SettingsPage.test.tsx`
- Modify (existing): `web/src/app/router.tsx`
- Modify (existing): `web/src/app/router.test.tsx`
- Modify (existing): `web/src/styles/partials/layout.css`
- Modify (existing): `web/src/styles/partials/legacy-subscriptions.css`

- [ ] 写`types.ts`/`recordsApi.test.ts`/`api.test.ts`与组件RED tests，覆盖全部状态/错误、owner applicability、action CAS、显式Markdown checklist提升预览/确认、comment POST/PATCH/DELETE idempotency/CAS与tombstone/reply/mention、follow/mute强制通知说明、authorization-safe unread row/count/cursor、binding scope和revoke；bundle/import test禁止AppShell导入`recordsApi.ts`。
- [ ] 实现受控组件与`/notifications` lazy route；route controller只调用lazy `recordsApi.ts`。仅把`getRecordNotificationUnreadCount`及最小response type所需eager调用放在`api.ts`供NotificationBell使用，TopBar不得导入records façade；Bell从旧event-filter链接切换为收件箱且仅authorized `unread_count>0`渲染badge。Bell新增规则只进入`layout.css`，Settings binding规则只进入`legacy-subscriptions.css`；协作组件复用现有atoms/forms/page primitives，不把业务规则塞进page/legacy-assets catch-all。
- [ ] normal-state fixtures断言异常标题/空异常卡/禁用异常动作/预留高度为0；空action/comment/inbox使用用途明确空态，不伪装异常。
- [ ] 在1440px与390px验证keyboard/focus/44px/no-overflow；运行 `NODE_ENV=test npm --prefix web run test -- --run src/lib/recordsApi.test.ts src/lib/api.test.ts src/pages/records/collaboration src/pages/NotificationInboxPage.test.tsx src/app/layout/NotificationBell.test.tsx src/pages/SettingsPage.test.tsx`，预期PASS。

## Task 9: Activity/export/deletion adapters与retention

**Files:**

- Create: `internal/center/records/collaboration_activity.go`
- Create: `internal/center/records/collaboration_activity_test.go`
- Create: `internal/center/records/collaboration_export.go`
- Create: `internal/center/records/collaboration_export_test.go`
- Create: `internal/center/records/collaboration_deletion.go`
- Create: `internal/center/records/collaboration_deletion_test.go`
- Create: `internal/center/records/collaboration_recovery.go`
- Create: `internal/center/records/collaboration_recovery_test.go`
- Create: `internal/center/recordnotify/janitor.go`
- Create: `internal/center/recordnotify/janitor_test.go`
- Modify (created by core dependency): `internal/center/recorddeletion/service.go`
- Modify (existing): `internal/center/retention/worker.go`
- Modify (existing): `internal/center/retention/worker_test.go`

- [ ] 写RED contract tests固定stable activity identity/event time、完整action与未删除comment export、deleted comment只含revision metadata/tombstone且历史正文bytes为0、provider missing fail-closed、180d retention、permanent purge与`not_committed` no-op。
- [ ] 实现并注册activity/export/deletion providers；activity/export不得从deleted comment历史row/cache恢复正文，export不含inbox unread/channel secret/retry/message ID，deletion receipt仅输出无身份external-copy aggregate。
- [ ] 实现 `records.NewCollaborationRecoveryAdapter`，拥有owner/participant/action/comment history+tombstone/follow/inbox/delivery重放与清理；不恢复foreign unread/outbox正文，delete commit后只保留无身份external-copy disclosure。
- [ ] 在comment/action transaction、outbox claim、send前后和purge阶段做并发故障测试；reservation后新增正文/inbox/outbound bytes为0，成功外投只进入披露计数。
- [ ] `go test -race ./internal/center/records ./internal/center/recordnotify ./internal/center/recorddeletion ./internal/center/retention -run 'CollaborationActivity|CollaborationExport|CollaborationDeletion|NotificationRetention' -count=10`；预期 PASS。

## Task 10: 浏览器合同、全量质量门与交接审查

**Files:**

- Modify (existing): `web/e2e/fixtures/contracts.ts`
- Modify (existing): `web/e2e/fixtures/profiles.ts`
- Modify (existing): `web/e2e/fixtures/router.ts`
- Modify (existing): `web/e2e/accessibility.spec.ts`
- Modify (existing): `web/e2e/page-states.spec.ts`
- Create: `web/e2e/record-collaboration.spec.ts`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/directory-structure.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/database-guidelines.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/error-handling.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/backend/logging-guidelines.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/web/directory-structure.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/web/component-conventions.md`
- Modify (existing, only after implementation establishes the contract): `.trellis/spec/web/state-and-data.md`

- [ ] 为 `/notifications` 与协作组件添加fail-closed fixtures，覆盖loading/first-empty/query-empty/error/submitting/revoked、scope mismatch、desktop/390、Axe、keyboard/focus/44px；未知API继续501。
- [ ] 运行 `npm --prefix web run test:e2e -- --grep 'record collaboration|notifications'`；预期PASS、console/network/overflow violation为0。
- [ ] 运行真实PostgreSQL migration/concurrency suites、`make verify-go`、Node 22 `make verify-web`、`npm --prefix web run test:e2e`、`git diff --check`；每个命令必须fresh PASS。
- [ ] 使用 `trellis-check` 做父PRD逐项覆盖、复用、跨层auth/notification/deletion flow、未决项和context drift审查；确认 Markdown child从含0056与本任务comment corpus/dependencies的主线开始。

## Review and rollback points

- Migration review：0056不得引用不存在的0055；custom migration-filesystem fixture必须证明lower-after-higher ledger能力，实际0055配对测试留给其search owner。不得给来源对象加cascade，不得把正文写入delivery/outbox/audit。
- Authorization review：action/comment/follow/inbox/external binding每条读取和worker重试必须到同一`recordauth.Policy`，前端条件显示不构成授权。
- Notification review：成功/失败/结果未知三态、generic template corpus与第三方error body redaction必须由独立reviewer逐项确认。
- Deletion review：adapter missing、receipt不完整、external outcome unknown时永久删除保持fail closed。
- 回滚只关闭collaboration/external-notification feature和worker；保留0056数据只读，不执行down migration、不删除评论/行动项metadata历史、绝不恢复已由comment tombstone清除的正文，也不把TopBar切回会泄露旧摘要的路径。
