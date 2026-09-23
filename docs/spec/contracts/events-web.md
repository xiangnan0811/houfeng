# 事件 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

### Scenario: Events include backfilled filter

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/pages/EventsPage.tsx` 的筛选状态、`web/src/lib/types.ts` 的 `EventListFilter`、或 `web/src/lib/observabilityApi.ts` 的 `listEvents` query 序列化。

#### 2. Signatures

- URL-state: `/events?include_backfilled=1`。
- Frontend filter type: `EventListFilter.include_backfilled?: boolean`。
- API client: `listEvents({ include_backfilled: true })` -> `/api/events?...&include_backfilled=true`，读取 `{"items":[]}` 并向 page 返回 `StateChangeEventRecord[]`。
- Backend contract: `/api/events?include_backfilled=true` 解除默认 backfilled event exclusion，成功响应为 `{"items":[...]}`。

#### 3. Contracts

- `include_backfilled` 是显式 opt-in；默认 `false` 时不写 URL，也不写 API query。
- URL 只接受 `include_backfilled=1` 为 active；`yes`、`true`、`0` 等 URL 值在 EventsPage canonicalize 时被移除。
- API query 用 `include_backfilled=true`，复用 `withQuery` 的 boolean 序列化和 false omission。
- `listEvents(...)` 是唯一解包点：page / component 继续消费数组，不在 UI 层重复判断 `{items}` envelope。
- EventsPage 必须把该维度纳入 parse -> normalize -> URL serialize -> `buildFilterQuery` -> chip -> reset/remove 全链路。
- “包含补传事件” toggle 不得禁用或显示“待后端支持”；如果后端 contract 未来撤销，必须同时更新 handler/store/spec/tests，而不是只改 UI 文案。
- EventsPage 的高级筛选采用 applied/draft 分离：URL 与 `appliedFilters` 是请求真相；Drawer 打开时 draft 必须从当前 applied filters 初始化；只有点击 `应用筛选` 或 `重置筛选` 才能改 URL 和触发 `/api/events` 请求。Esc、overlay、头部关闭与 Drawer 内 `关闭` 必须丢弃 draft，不能提交筛选或发请求。

#### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| 首次进入 `/events?include_backfilled=1` | 初始请求包含 `include_backfilled=true`，页面显示“包含补传事件” chip |
| 点击 toggle 后应用 | URL 写 `include_backfilled=1`，API 请求写 `include_backfilled=true` |
| 移除 chip | URL 和 API query 都清除 backfill 维度 |
| 重置筛选 | 回到 `/events`，请求 `/api/events?limit=50` |
| URL `include_backfilled=yes` | canonicalize 掉，不请求 backfill 维度 |
| Drawer 内修改草稿后关闭 / Esc | URL 不变，不发新请求；下次打开恢复当前 applied filters |

#### 5. Good/Base/Bad Cases

- Good: 用户从 Dashboard 或手写 URL 进入 `/events?include_backfilled=1`，页面首个请求已经包含该维度。
- Base: 默认 `/events` 不显示 backfill chip，仍请求 `/api/events?limit=50`。
- Bad: toggle 改变本地 UI 但 `listEvents` 未带 query，导致 inert filter。
- Bad: 只在 URL 写 `include_backfilled=1`，但 active chip/remove/reset 没有纳入同一状态机。
- Bad: Drawer 草稿通过 `onChange` 直接写 URL 或请求 API，导致关闭抽屉也会提交用户尚未确认的筛选。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: `listEvents({ include_backfilled: true })` 序列化为 `include_backfilled=true`，false 被省略，并从 `{"items":[...]}` envelope 解包为数组。
- `web/src/pages/EventsPage.test.tsx`: 初始 URL、Drawer apply、Drawer close / Esc discard、toggle apply、chip remove、reset、invalid URL canonicalization 全部覆盖。

#### 7. Wrong vs Correct

```tsx
// 错误：UI 状态有字段，但 normalize 永远清成 false。
include_backfilled: false

// 正确：parse / normalize / build query 都保留用户选择。
include_backfilled: filters.include_backfilled
```

```tsx
// 错误：后端已支持后仍禁用控件。
<FilterToggle label="包含补传事件" checked={false} disabled onChange={() => {}} />

// 正确：toggle 进入同一套筛选状态机。
<FilterToggle
  label="包含补传事件"
  checked={filters.include_backfilled}
  onChange={(checked) => updateDraftFilter('include_backfilled', checked)}
/>
```
