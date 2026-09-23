# Records Web 合同

通用数据层见 [Web 规范](../../web/README.md)。

### Scenario: AppShell 动态到达 records transport（global search 分组）

#### 1. Scope / Trigger

- Trigger: AppShell 常驻组件（当前只有 `GlobalSearch`）需要 server-backed records 数据，或改动 records 分组的配额/失败语义时。
- 目标：让 shell 能搜到记录，同时不让 records transport 回到 entry chunk。

#### 2. Signatures

- `web/src/pages/records/globalRecordSearch.ts`：`searchRecordsForGlobalSearch(query, limit): Promise<GlobalRecordSearchHit[]>`、`GlobalRecordSearchHit`、`RECORD_SEARCH_ALL_HIT_ID`。
- `GlobalSearch.tsx` 侧：`RECORD_RESULTS = 4`（records 配额）、`MAX_RESULTS = 10`（asset 配额）。

#### 3. Contracts

- shell **只能**用 `import('../../pages/records/globalRecordSearch')` 动态到达；不得在 shell 里静态 import `recordsApi.ts`、`searchFilterModel.ts` 或 records DTO 的 value import。type-only import 不构成 runtime edge，但没有必要时也不要加。
- 领域模块负责映射与降级，shell 不认识 records 类型。canonical `/records?...` 链接由该模块用 `recordSearchParamsFromFilters` 生成——URL 编解码只有一个 owner。
- **失败必须隔离**：records 分组任何异常（索引未就绪、平台关闭、401、网络失败）都 resolve 成空数组，不得阻断 asset 分组；asset 分组失败也不得清空已经返回的 records。`Promise.all` 的每个分支必须自带 catch。
- 401 的 session 结束副作用属于 transport（`requestJSON` 先通知 unauthorized handler 再 reject），领域模块吞掉 rejection **不等于**吞掉该副作用；不要为了"提前失败"在请求前自行判断登录态。
- records 有独立配额，不与 asset 上限共享：asset 是客户端未排序匹配，records 由服务端排序，共享上限会让高频关键词把记录全部挤掉。
- 服务端返回非空时才追加通向 `/records` 的末位入口；索引不可用时指向搜索页是误导。
- 迟到的旧查询不得覆盖新查询结果：`GlobalSearch` 用单调 generation ref 丢弃过期响应。

#### 4. Tests Required

- `globalRecordSearch.test.ts`：bounded limit、raw query 透传（服务端匹配，不做客户端小写化）、hit 映射与 id 回退、末位 canonical 链接、空结果不追加链接、三类失败均降级为空。
- `GlobalSearch.test.tsx`：records 分组渲染与链接、无 asset 命中时仍出结果、asset 失败仍保留 records 并展示错误、迟到响应被丢弃。
- `recordsTransportArchitectureContract.test.ts` 的 `EXPECTED_EXPORTS` 必须包含 `searchRecords`；fresh build 后 `bundle:check` 证明 entry 不含 records transport。

## 草稿与比较 transport

- `CreateRecordDraftInput` 是关闭联合：新记录草稿只发送 `payload`，已有记录草稿必须同时发送 `record_id` 与 `base_revision_id`；不得在 TypeScript contract 中强迫新草稿伪造空 ID，也不得允许两个 routing fields 只出现一个。
- Records 草稿 PATCH 原样发送响应中的 `If-Match: <draft-etag>`，不得套用 legacy metadata helper 的额外引号；formal mutation 使用独立 `Idempotency-Key`。permanent-delete execute 的 `DeletionRequestTokenV1` 是唯一 `Idempotency-Key`，JSON body 只能含 `reservation_id`。
- `/records/compare` 使用 `comparison-url/v1` query `state`（canonical key order、UTC、整数秒）。state 不含 `token` / `comparison_intent` / `payload` / `title` / `body_markdown`。candidate 确认前 `POST /api/evidence/comparisons` 次数为 0。另存必须走 `createRecordDraft` + `saveComparisonRecord`，不得调用 `createRecord` / `useRecordDraft.publish()`。同一 digest 重试必须复用 `record_id` 与 `Idempotency-Key`。证据类型切换是 SegmentedControl 值选择，不是无 panel 的 Tabs。`HOUFENG_COMPARISON_ENABLED` 默认关。

| 条件 | 预期 |
| --- | --- |
| 新记录草稿携带一个或伪造两个空 routing fields | TypeScript union/source review 阻断；body 只含 `payload` |
| Records draft PATCH 给 ETag 增加引号 | 后端 exact `If-Match` 拒绝；原样发送 draft response 的 `etag` |
| deletion token 同时进入 header 和 JSON body | body unknown-field decode 失败；只保留 header token 与 body `reservation_id` |
| Records error body 含 malformed `code/field_errors` 或未知 debug 字段 | 显式 decoder 保留 status/message，忽略 malformed/未知元数据；`recovery` 仍按 unknown 处理 |

## Subject workspace

Activity, records, and evidence for VPS, monitoring instances, and entrypoints remain views of the shared subject workspace. Preserve the existing URL filter codec and location.state return context through record publication, restoration, and evidence links. Interactive filters match each view's server predicate; retained incompatible URL filters stay visible and removable. A scoped new record consumes the same canonical subject reference emitted by its entry link and isolates its unsynced buffer from other subjects and unscoped creation. Reopening the same entry restores its buffer without overwriting newer edits; unscoped draft recovery remains available from /records/new. A valid canonical return_to provides an explicit subject-return link, without changing the post-publication record destination. Record search prioritizes results; import/export remain secondary tools. Evidence reading follows the [evidence Web contract](../evidence-web.md).

Source presence 在册 is a neutral identity badge, not a green health state. VPS, monitoring, and 入口探测 activity/records/evidence share `SubjectActivityWorkspace` and `SubjectLocalNavigation`; only VPS exposes an overview hop because only that source has a detail workspace. Do not duplicate those surfaces or invent missing source fields.
