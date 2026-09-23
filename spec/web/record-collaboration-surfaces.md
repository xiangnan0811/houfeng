# Records 协作 Web Surface 合同

> 本文记录 Child 9 已实现的 API façade、受控组件、Inbox lazy route 与 unread eager
> seam。Child 5 才拥有把这些组件挂进 Records read/edit workspace 的职责。

## 1. Scope / Trigger

修改以下文件或合同前必须加载本文：

- `web/src/lib/recordCollaborationApi.ts`、`recordCollaborationDto.ts`、
  `recordInboxUnreadApi.ts`；
- `RecordRevisionCollaborationControls`、`RecordActionPanel`、
  `RecordCommentThread`、`RecordWatchControl`、`RecordCommentMarkdown`；
- `web/src/pages/RecordInboxPage.tsx`、`app/router.tsx` 或 TopBar unread badge；
- collaboration 的 loading/empty/error/revoked/deleted、modal/focus、390px、Axe、
  bundle/CSS budget 行为。

边界是“纯受控组件 + lazy Inbox route + narrow eager unread count”。组件不 fetch、
不读 route、不拥有 server cache；Child 9 不修改 Records workspace/page 来挂载它们。

## 2. Signatures

API façade 必须经过共享 `apiRequest` transport，并在返回前执行 exact runtime decode：

```ts
listRecordActions(recordId: string, limit?: number): Promise<RecordActionListResponse>
createRecordAction(recordId: string, input: RecordActionInput, idempotencyKey: string): Promise<RecordActionMutation>
updateRecordAction(recordId: string, actionId: string, input: RecordActionInput, version: number, idempotencyKey: string): Promise<RecordActionMutation>
transitionRecordAction(recordId: string, actionId: string, transition: RecordActionTransition, version: number, idempotencyKey: string): Promise<RecordActionMutation>
listRecordComments(recordId: string, limit?: number): Promise<RecordCommentListResponse>
createRecordComment(recordId: string, input: RecordCommentInput, idempotencyKey: string): Promise<RecordCommentMutation>
editRecordComment(recordId: string, commentId: string, input: Omit<RecordCommentInput, 'reply_to_comment_id'>, version: number, idempotencyKey: string): Promise<RecordCommentMutation>
redactRecordComment(recordId: string, commentId: string, version: number, idempotencyKey: string): Promise<RecordCommentMutation>
getRecordWatch(recordId: string): Promise<RecordWatch>
setRecordWatch(recordId: string, preference: RecordFollowerPreference, version: number, idempotencyKey: string): Promise<RecordWatch>
listRecordNotifications(limit?: number): Promise<RecordNotificationListResponse>
getRecordNotification(notificationId: string): Promise<RecordNotification>
getRecordNotificationTarget(notificationId: string): Promise<RecordNotificationTarget>
markRecordNotificationRead(notificationId: string): Promise<RecordNotification>
markRecordNotificationUnread(notificationId: string): Promise<RecordNotification>
dismissRecordNotification(notificationId: string): Promise<RecordNotification>
getRecordNotificationUnreadCount(): Promise<RecordNotificationUnreadResponse>
```

Mutations 必须由 caller 传入 fresh ETag version 与 idempotency key；facade 只发送
allowlisted JSON/header。action update payload 必须保留 server-authorized `details`，不能
因为 list/read UI 隐藏字段而用空串覆盖。

组件 contract：

```text
RecordRevisionCollaborationControls  完整 revision draft owner/participants/follow_up
RecordActionPanel                     current action + onCreate/onUpdate/onTransition
RecordCommentThread                   fresh comments + create/edit/reply/redact commands
RecordWatchControl                    current watch + onSetPreference
RecordCommentMarkdown                 untrusted render_model -> React-only DOM
RecordInboxPage                       route-owned list/target/transition orchestration
```

## 3. Contracts

### 3.1 DTO / transport

- decoder 必须 exact-key、closed enum、stable ID、signed-bigint-safe version、canonical
  UTC timestamp、relation/state consistent；unknown/missing/null-wrong 字段立即 throw。
- action list 最多 100；comment list 最多 200；inbox list 最多 100。合法 comment
  101..200 不能被客户端误判 malformed。
- Go 的空 slice 必须在线上编码为 `[]`；Web 不把 `null` 当空数组。
- redacted comment 的 `body_markdown` / `render_model` 必须是 `null`，mentions 是 `[]`；
  active comment 必须有 validated `comment_markdown/v1` model。
- watch version `0` 只允许 `preference=default`、所有 source false、`updated_at=null`。
- unread endpoint 不接受 query；失败保持 unknown/error，不能伪造成 0。
- list query 必须 canonical 单值 `limit`；不要用 `URLSearchParams` 把 caller 的非法
  value 静默规范化成另一请求。

### 3.2 Lazy / eager ownership

- `/record-inbox` 在 `web/src/app/router.tsx` 使用 `React.lazy` + 现有 route fallback。
- Inbox page、collaboration façade、comments/actions/Markdown renderer 不得进入 eager
  entry graph。
- TopBar 只可 import `recordInboxUnreadApi.ts` 的 narrow unread-count seam；不得 import
  `recordCollaborationApi.ts`、Records transport façade 或 route page。
- 四个 collaboration component 保持跨页纯受控；route-private controller/state 留在
  `RecordInboxPage`，最终 Records workspace composition 留给 Child 5。

### 3.3 State / races

- async settle 必须同时检查 mounted lifecycle、request generation 与同 item operation
  token。旧 list/target/transition 不得覆盖较新选择或 mutation。
- same-item read/dismiss 会 supersede pending target 并清理 cache；unrelated item B 的
  mutation不能 suppress item A target 的 404/503 authority loss。
- revoked/deleted/opaque 404 必须 fail closed：清 list、target、draft、edit/redaction 与
  optimistic state，不保留 server body 或完整 comment object 的本地副本。
- ready → loading/error/revoked/deleted → ready 必须 remount/reset editor，不能恢复旧
  action draft、comment body 或 modal intent。
- loading/empty/error/revoked/deleted 是显式 presentation state；empty 不能伪装 loading，
  request failure 不能伪装 empty/unread=0。

### 3.4 Safe rendering / accessibility

- `RecordCommentMarkdown` 自己 decode untrusted model，只能创建 React
  `p/span/br/em/strong/s/code/pre/ol/ul/li/a`；禁止 `dangerouslySetInnerHTML`、HTML/
  JSON/Markdown fallback。
- comment redaction 使用现有 portal `Modal`，role 为 `alertdialog`；复用 modal stack /
  focus hook，覆盖 initial focus、Tab、Escape、close 后 focus restore。不得复制 ad-hoc
  focus trap。
- 390px 保持 tap target、无 viewport overflow、明确 keyboard focus；桌面/移动都要 Axe。
- 不为本 slice 新增 CSS source 或抬 budget；复用现有 token/atom/page/card/badge classes。

## 4. Validation & Error Matrix

| 输入/状态 | Web 行为 |
|---|---|
| unknown DTO key、非法 ID/version/time/enum/relation | decoder throw，进入 closed error state |
| API 400/409/422 | 保留可操作上下文，只显示 allowlisted错误文案，不展示 raw body |
| API opaque 404 | revoked/deleted；清 protected state/cache/draft |
| API 503 / network failure | unavailable/error；不伪造 empty 或 unread=0 |
| stale list/target success | generation/token 不匹配，忽略 |
| stale same-item target failure after mutation | mutation token supersedes，忽略 |
| target A failure after unrelated B mutation | A 仍 fail closed，不被 B suppress |
| redacted comment | 只呈现 tombstone state，无 body/model fallback |
| malformed render model | renderer fail closed，不渲染任意 DOM |

## 5. Good / Base / Bad Cases

- Good：Inbox A target pending，用户 dismiss B；A 随后 404，页面仍移除/关闭 A，不能
  因 B 的 mutation generation 忽略 authority loss。
- Good：action edit 使用 decoder 返回的完整 authorized fields，提交时保留非空
  `details`，只替换用户实际编辑的字段。
- Good：comment redaction modal Escape 关闭并把焦点还给原 trigger。
- Base：Child 5 尚未挂载组件；独立 test-only browser harness 仍验证真实 production
  component，且不进入 production router/bundle。
- Bad：component 内直接 fetch，或把整个 comment object/body 放进异步 closure/cache。
- Bad：TopBar import collaboration façade，导致 comment/action renderer 进入 entry chunk。
- Bad：用 innerHTML 渲染 server Markdown，或用 JSON stringify 作为 fallback。
- Bad：为通过视觉测试新增生产 demo route、复制 DOM，或抬高 bundle/CSS budget。

## 6. Tests Required

- Vitest：API URL/header/body、exact DTO decoders、100/101/200 comment boundary、non-nil
  arrays、hostile render model、component commands/state reset、Inbox deferred interleavings。
- Shared corpus：Go 与 Web 都读取
  `internal/center/recordcollaboration/testdata/comment_markdown_v1.json`。
- Component browser：test-only Vite harness 渲染真实四组件，desktop/390px、keyboard/tap、
  alertdialog focus/Escape/restore、loading/empty/error/revoked/deleted、Axe。
- Primary Playwright：`/record-inbox` desktop/1024/390、keyboard/touch/Axe、TopBar unread。
- Node 必须是 22；运行 `make verify-web`、primary Playwright、component browser。
- Fresh build 后运行 bundle/CSS budgets；entry、max async、CSS source/production 都不得
  抬 ratchet。检查 production router/bundle 不含 test harness。

## 7. Wrong vs Correct

### Wrong

```tsx
function RecordComment({ comment }: { comment: unknown }) {
  return <div dangerouslySetInnerHTML={{ __html: String(comment) }} />
}

// unrelated B mutation increments one global generation and suppresses A failure.
const generation = ++targetGeneration
await dismiss(b)
if (generation !== targetGeneration) return
```

### Correct

```tsx
function RecordComment({ model }: { model: unknown }) {
  return <RecordCommentMarkdown model={model} />
}

// Success obeys latest selection; failure additionally binds A's own operation token.
const operationToken = itemOperations.get(a.notification_id) ?? 0
const target = await getRecordNotificationTarget(a.notification_id)
if (!mounted || operationToken !== (itemOperations.get(a.notification_id) ?? 0)) return
applyTargetResult(a.notification_id, target)
```
