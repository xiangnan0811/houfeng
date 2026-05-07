# PR4: Dashboard deep links and list URL state

## Goal

把 Dashboard 从“看完还要自己找入口”的总览页继续推进为可操作工作台：首页的节点、目标、事件入口必须带着明确筛选语义跳到对应页面；Nodes / Targets / Events 页面必须能从 URL query string 还原筛选、展示可见筛选状态，并支持用户继续调整或清除。

本任务不扩展 `/api/dashboard` 后端 contract。PR3 已补齐 Dashboard 所需事实；PR4 只偿还前端深链与列表页 URL-state 能力。

## What I already know

* PR1 已完成首页信息架构调整，但当时 URL-state 深链明确留给 PR4。
* PR3 已让 `/api/dashboard` 返回 `snapshot_generated_at`、库存完整度计数、`group_summaries` 和 `notification_status`，因此 Dashboard 的跳转语义可以基于真实 contract。
* `NodesPage` 与 `TargetsPage` 已经使用 `useSearchParams` 承载大部分筛选。
* `EventsPage` 仍是本地 draft/apply state，尚未把高级筛选同步到 URL，也缺少和 Nodes/Targets 一致的 chip flow。
* 候风本质上仍是服务器管理系统，首页入口必须能把用户带到“可处理”的列表状态，而不是只做装饰性导航。

## Requirements

### 1. URL-state contract

NodesPage 保留既有 URL 参数：

* `group`
* `region`
* `city`
* `provider`
* `lifecycle`
* `run_status`
* `health`
* `labels`，逗号分隔多值
* `abnormal=1`

NodesPage 新增 Dashboard 可用的待接入语义：

* `onboarding=pending`
* 过滤条件必须与 PR3 的 `pending_onboarding_node_count` 语义一致：`lifecycle_status === '待接入'` 或 `binding_status` 为 `未绑定` / `指纹变更待确认`。
* 页面需要显示可见筛选控件或 chip，文案建议为 `待接入/绑定待处理`。
* 清除该筛选时要删除 `onboarding` query 参数。
* 既有 `绑定异常` segmented view 可以保持本地状态；PR4 不要求把该 tab 本身 URL-state 化。

TargetsPage 继续把既有 URL 参数作为权威 contract：

* `group`
* `type`
* `run_status`
* `health`
* `labels`，逗号分隔多值
* `execution_labels`，逗号分隔多值
* `abnormal=1`

EventsPage 新增 URL-state contract：

* `object_type=node|target`
* `severity=关注|告警|严重`
* `event_type=<StateChangeEventType>`
* `limit=10|25|50|100`
* `created_from=<datetime-local string>`
* `created_to=<datetime-local string>`
* `label=<text>`
* `notification_only=1`
* `recovery_only=1`
* `maintenance_only=1`
* `time_range=24h|7d|30d|custom`

EventsPage 行为要求：

* 首次渲染时从 URL 还原筛选，并用该筛选发出第一笔 `listEvents` 请求。
* 提交筛选时更新 URL query string，并重新拉取事件。
* 重置筛选时清空 URL query string，并回到默认请求。
* 删除 chip 时立即更新 URL 并重新拉取。
* `time_range=24h|7d|30d` 代表动态相对时间窗口：页面可以在构造 API filter 时计算 `created_from` / `created_to`，URL 中不需要固化计算后的绝对时间。
* `time_range=custom` 使用 URL 中的 `created_from` / `created_to`。
* 不把 `include_backfilled` 写入 URL；当前后端不支持该筛选，继续以不可用/禁用能力表达。

### 2. Dashboard deep links

DashboardPage 的所有关键入口应优先跳到带筛选语义的路由。具体约定：

* 严重事件：`/events?severity=严重`
* 24h 事件：`/events?time_range=24h`
* 维护事件：`/events?maintenance_only=1`
* 异常节点：`/nodes?abnormal=1`
* 异常目标：`/targets?abnormal=1`
* 待接入/绑定待处理节点：`/nodes?onboarding=pending`
* 暂停节点：`/nodes?run_status=暂停`
* 退役节点：`/nodes?lifecycle=已退役`
* 暂停目标：`/targets?run_status=暂停`
* 归档目标：`/targets?run_status=已归档`

Dashboard 区块入口：

* Fleet State hero：
  * 有严重项时主 CTA 指向 `/events?severity=严重`。
  * 有异常但无严重时主 CTA 指向 `/events?time_range=24h`。
  * 维护态主 CTA 指向 `/events?maintenance_only=1`。
  * 正常态和首次接入态保留到 `/nodes` 的主入口。
  * 次级 `查看事件流` 使用 `/events?time_range=24h`。
* Global KPI strip：
  * 节点：有异常节点时 `/nodes?abnormal=1`，否则 `/nodes`。
  * 目标：有异常目标时 `/targets?abnormal=1`，否则 `/targets`。
  * 严重：`/events?severity=严重`。
  * 维护：`/events?maintenance_only=1`。
  * `24h 变化`：`/events?time_range=24h`。
* `当前需要处理` aside：
  * `查看全部异常节点` 指向 `/nodes?abnormal=1`。
  * `查看全部异常目标` 指向 `/targets?abnormal=1`。
  * `查看事件流` 指向 `/events?time_range=24h`。
* `系统入口`：
  * 节点卡片按优先级选择链接：待接入/绑定待处理 > 暂停 > 退役 > 默认 `/nodes`。
  * 目标卡片按优先级选择链接：异常 > 暂停 > 归档 > 默认 `/targets`。
  * 事件卡片指向 `/events?time_range=24h`。
  * 设置卡片继续指向 `/settings`。
* `首次接入工作台`：
  * `创建节点` 指向 `/nodes`。
  * `接入 agent` 指向 `/nodes?onboarding=pending`。
  * `创建目标` 与 `添加 ProbeItem` 指向 `/targets`。
* `最近事件` aside 的 `查看全部事件` 指向 `/events?time_range=24h`。

### 3. UI and interaction

* Nodes / Targets / Events 的筛选状态必须在页面上可见；URL 不能成为隐藏状态。
* EventsPage 应复用 `components/filters` 的 FilterBar / FilterChip / FilterSelect / FilterToggle 等原语，向 Nodes/Targets 的交互一致性靠拢。
* EventsPage 可以保留普通输入框处理 label / date，但不要新增 inline 颜色、边框、阴影或硬编码 spacing。
* Dashboard 只负责把用户送到合适的筛选页，不复制 EventsPage 的高级筛选 UI。
* 文案保持中文、工程工具密度和服务器管理语境；不要把筛选入口写成营销式说明。

## Acceptance Criteria

* [ ] Dashboard 所有关键 CTA / aside / 系统入口 / 最近事件链接都指向符合 PR4 contract 的筛选路由。
* [ ] `/nodes?onboarding=pending` 能筛出待接入、未绑定、指纹变更待确认节点，并显示可清除的筛选状态。
* [ ] TargetsPage 的既有 URL 筛选从 Dashboard 深链进入时可见且有效；新增测试覆盖异常、暂停或归档入口。
* [ ] `/events?...` 可在首次加载时按 URL 筛选请求事件，提交/重置/删除 chip 会同步 URL 与请求。
* [ ] URL 参数无效时页面退回安全默认值，不崩溃、不把无效值传给 API。
* [ ] `include_backfilled` 仍不进入 URL 或 API 请求。
* [ ] 更新前端 spec / v2 component spec，明确 PR4 后 Dashboard 深链是受支持 contract，而不是临时拼接。
* [ ] `cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build` 通过。

## Out of Scope

* 不改 Go 后端、数据库 schema 或 `/api/dashboard` response。
* 不做保存筛选视图、自定义 dashboard、拖拽布局或多 dashboard。
* 不引入 React Query、Zustand、UI 框架、CSS-in-JS、图表库或 e2e 框架。
* 不把 Dashboard 变成事件页高级筛选的复制版。
* 不要求把 NodesPage 的 `绑定异常` segmented view URL-state 化，除非实现中发现这是最小必要路径。
* 不做真实浏览器截图流程；当前以 Vitest + lint + build 覆盖。

## Technical Notes

* 相关代码：`web/src/pages/DashboardPage.tsx`、`web/src/pages/NodesPage.tsx`、`web/src/pages/TargetsPage.tsx`、`web/src/pages/EventsPage.tsx`。
* 相关测试：`web/src/pages/DashboardPage.test.tsx`、`web/src/pages/NodesPage.test.tsx`、`web/src/pages/TargetsPage.test.tsx`、`web/src/pages/EventsPage.test.tsx`。
* 相关设计/spec：`.trellis/spec/web/state-and-data.md`、`docs/design/v2-houfeng/component-spec.md`。
* `EventsPage` 一旦使用 `useSearchParams`，测试需要包在 router 中渲染。
* `listEvents` 已支持 object/severity/type/date/label/notification/recovery/maintenance/limit 等筛选；PR4 应优先复用 `web/src/lib/api.ts` 的现有 query 构造，不在 page 里手写业务 fetch。
