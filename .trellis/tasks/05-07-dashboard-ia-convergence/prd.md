# Dashboard 信息架构收敛与首屏重构

## Goal

把 Dashboard 从“数据区块线性堆叠”收敛成候风作为服务器管理系统应有的全局工作台：进入首页后，用户应先得到一个可信的系统状态结论，再看到当前最需要处理的事项，并能直接跳转到节点、目标、事件、设置等核心页面。此次任务不是继续添加模块，而是纠正前几轮设计仍然分散的问题，减少同权区块，让首页真正有不可替代的系统入口价值。

## What I Already Know

* 用户明确指出当前 Dashboard 仍然“很乱”，并要求反思之前设计。问题不在字段缺失，而在信息架构仍然按 `hero + KPI + 处理队列 + 系统入口 + Group + 最近事件` 线性堆叠，所有区块视觉权重接近。
* PR1 已经把 Dashboard 从最早的多表格堆叠改成 Fleet State / KPI / 当前处理 / 系统入口 / Group / 最近事件，但这仍然没有形成清晰首屏重点。
* PR3 已扩展 `/api/dashboard`，Dashboard 已有 `snapshot_generated_at`、待接入/暂停/退役/归档计数、真实 `group_summaries`、`notification_status` 等事实。
* PR4 已建立 Dashboard 深链和列表 URL-state contract，Dashboard 的入口必须继续保留这些筛选 URL，不能退回到无语义跳转。
* 当前 `web/src/pages/DashboardPage.tsx` 中 `SystemEntryPoints`、`GroupDistribution`、`最近事件` 都是完整 `DetailSection`，它们在异常态、正常态、首次接入态中都容易和主任务抢视觉权重。
* 当前 `DashboardPage.test.tsx` 主要验证各区块存在、链接正确、行点击正确；需要新增/调整测试来验证“去堆叠”和“状态驱动”的体验约束。
* `docs/design/v2-houfeng/component-spec.md` 仍记录旧的 Dashboard 结构，必须同步修订，否则后续实现会继续回到线性堆叠。

## Product Judgment

候风可以有自己的风格，但它本质上仍是服务器管理系统。首页不应做成监控大屏、营销页面或数据仓库，也不应把节点页、目标页、事件页复制一份。它应承担三个核心职责：

1. 给出全局状态结论：现在正常、异常、严重、维护、还是首次接入。
2. 给出下一步行动：优先处理什么，或正常时该去哪里管理系统。
3. 提供可信入口：节点、目标、事件、设置的跳转要带当前语义，而不是让用户重新找筛选。

因此，本轮重构的关键不是增加更多信息，而是让信息有主次。Group 分布、最近事件、系统入口仍然有价值，但它们应该作为上下文和快捷入口被压缩到工作区内，而不是三个连续的完整首页 section。

## Requirements

### 1. Dashboard 采用状态驱动布局

Dashboard 顶层结构应收敛为：

* `FleetStatePanel`：保留动态状态结论和主要 CTA。
* `GlobalKpiStrip`：保留 5 个全局 KPI 和 PR4 深链，但视觉上保持紧凑。
* 一个主工作区：根据当前状态在同一个工作台内呈现异常分诊、正常概览、维护观察或首次接入。

除 `FleetStatePanel` 和 KPI strip 外，Dashboard 主流程不应继续出现 `当前需要处理`、`系统入口`、`按 Group 分布`、`最近事件` 四个完整 section 连续堆叠的结构。

### 2. 异常态以处理队列为唯一主内容

当存在异常节点或异常目标时：

* 主工作区的主要标题仍可为 `当前需要处理`。
* 异常节点和异常目标继续合并排序，行点击和操作链接行为保持不变。
* `查看全部异常节点`、`查看全部异常目标`、`查看事件流` 必须保留 PR4 筛选 URL。
* 系统入口、Group 摘要、最近事件只能作为紧凑上下文出现在主工作区的侧栏或下方摘要中，不能再作为与处理队列同权的独立 DetailSection。
* 如果数据很多，Dashboard 默认只展示最高优先级的少量对象；完整处理仍跳转到 Nodes / Targets / Events。

### 3. 正常态不显示大型空处理队列

当没有活跃异常且不是首次接入时：

* 不应渲染一个大表格或大型空态来告诉用户“当前没有活跃异常”。
* 主工作区应变为 `运行概览` 或同等语义，展示库存健康、维护观察、24h 变化、快捷入口和最近上下文。
* 用户应能快速进入节点、目标、事件、设置，而不是面对空表格后失去下一步。
* 最近事件可以出现为紧凑列表；若为空，只显示小型说明，不能占用大块页面。

### 4. 维护态表达为观察状态

当没有异常但存在维护对象时：

* Fleet State 应继续显示维护结论并跳转 `/events?maintenance_only=1`。
* 主工作区应强调“维护观察中”的库存/事件上下文，而不是把维护态伪装成紧急异常。
* 维护相关入口应方便跳到事件和对应列表。

### 5. 首次接入态成为唯一主任务

当节点和目标均为 0 时：

* `首次接入工作台` 应成为主工作区的核心内容。
* 二级信息应尽量减少；不要渲染空的 Group 分布、空的最近事件大区块或其它让用户误以为系统已有数据的区域。
* 四步入口继续保持：
  * 创建节点 -> `/nodes`
  * 接入 agent -> `/nodes?onboarding=pending`
  * 创建目标 -> `/targets`
  * 添加 ProbeItem -> `/targets`

### 6. 系统入口降级为紧凑快捷区

`系统入口` 不再作为独立完整 section。它应作为主工作区的一部分出现，例如侧栏、快捷入口 rail 或紧凑网格：

* 节点入口仍按 PR4 优先级跳转：待接入/绑定待处理 > 暂停 > 退役 > 默认 `/nodes`。
* 目标入口仍按 PR4 优先级跳转：异常 > 暂停 > 归档 > 默认 `/targets`。
* 事件入口跳转 `/events?time_range=24h`。
* 设置入口跳转 `/settings`，只展示通知配置摘要，不暴露 token、chat id 或 webhook URL。
* 文案应是管理系统语境，不写营销式介绍。

### 7. Group 和最近事件降级为上下文摘要

Group 和最近事件仍可保留，但必须改变权重：

* Group 使用 `/api/dashboard.group_summaries` 的真实数据，默认只显示少量摘要，例如 Top 3 group 或总览计数。
* Group 为空时显示一句小型说明，不制造大表格或虚假的 `未分组 0`。
* 最近事件默认只显示少量记录，例如 3-5 条，并保留 `查看全部事件` -> `/events?time_range=24h`。
* 不在 Dashboard 复制 EventsPage 的高级筛选 UI。

### 8. 视觉与交互约束

* 沿用现有 atoms、`DetailSection`、BEM class、CSS tokens 和页面密度，不引入 UI 框架、图表库、CSS-in-JS 或新的状态库。
* 不创建卡片套卡片的视觉结构。主工作区可以是一个 section，内部用 grid/aside 形成主次关系。
* 避免把页面继续切成很多大块。桌面端应清楚呈现“主任务 + 上下文”，移动端应保持阅读顺序清晰。
* 所有链接、行点击、`stopPropagation()` 行为不能回退。

## Acceptance Criteria

* [ ] 异常态 Dashboard 首屏以 `FleetStatePanel`、KPI strip、`当前需要处理` 工作区为主，不再连续渲染独立的 `系统入口`、`按 Group 分布`、`最近事件` 大 section。
* [ ] 正常态 Dashboard 不渲染大型空处理队列；用户看到的是运行概览、系统入口和轻量上下文。
* [ ] 首次接入态只突出 onboarding 工作台，不渲染空 Group 分布或空最近事件大区块。
* [ ] 系统入口保留节点、目标、事件、设置四个入口，并继续使用 PR4 深链 contract。
* [ ] Group 分布和最近事件以紧凑上下文呈现；空态不得占用大块页面。
* [ ] Dashboard 行点击、操作链接 `stopPropagation()`、Fleet State CTA、KPI 链接、异常列表 aside 链接继续通过测试。
* [ ] `DashboardPage.test.tsx` 覆盖异常态去堆叠、正常态、首次接入、深链、错误态和关键交互。
* [ ] `docs/design/v2-houfeng/component-spec.md` 同步更新 Dashboard 结构，避免后续再次按旧 section 堆叠实现。
* [ ] `cd web && npm run lint`、`cd web && TMPDIR=/tmp npm run test -- --run`、`cd web && npm run build`、`git diff --check` 通过。

## Out of Scope

* 不修改 Go 后端、数据库 schema 或 `/api/dashboard` response contract。
* 不改 NodesPage、TargetsPage、EventsPage 的筛选实现，除非发现 Dashboard 深链测试已因本次变更破坏。
* 不做自定义 dashboard、保存视图、拖拽布局、多 dashboard 或用户个性化配置。
* 不引入新的第三方依赖。
* 不做真实截图自动化；本轮以组件测试、lint、build 和代码审查约束体验。

## Technical Notes

* 主要代码：`web/src/pages/DashboardPage.tsx`。
* 主要测试：`web/src/pages/DashboardPage.test.tsx`。
* 样式：`web/src/styles/pages.css` 中现有 `dashboard-*` class。
* 文档：`docs/design/v2-houfeng/component-spec.md` 的 DashboardPage 小节。
* 旧 PRD 参考：
  * `.trellis/tasks/archive/2026-05/05-06-dashboard-and-system-navigation-redesign/prd.md`
  * `.trellis/tasks/archive/2026-05/05-07-dashboard-deep-links-url-state/prd.md`
* 实现建议：可以删除或内联重组当前 `SystemEntryPoints`、`GroupDistribution`、`最近事件 DetailSection`，改为 `DashboardWorkbench`、`DashboardShortcutRail`、`DashboardContextSummary` 等更收敛的组件。命名可按实际代码简化，不要求新增抽象过度分层。
