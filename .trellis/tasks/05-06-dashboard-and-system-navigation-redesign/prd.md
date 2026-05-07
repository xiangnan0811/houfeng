# Dashboard 与系统入口体验重构

## Goal

把候风首页从“分散的异常数据堆叠”改成服务器管理系统应有的全局工作台：用户进入首页后能快速判断系统是否正常、哪些对象需要先处理、下一步应该去哪个页面，并能从首页跳转到节点、目标、事件、设置等核心工作流。此次规划同时沉淀一个全站原则：候风可以有自己的气质，但不能缺少服务器管理系统的基础导航、库存、状态、操作与事件闭环能力。

## What I Already Know

* 用户明确反馈当前首页“混乱、没有重点、布局分散、无从下手、删掉也不影响系统”，这说明问题是信息架构和工作流入口，不只是视觉样式。
* 当前 `DashboardPage` 已消费 `/api/dashboard`，包含总节点数、总目标数、异常/严重/维护计数、24h 新异常/恢复计数、异常节点、异常目标、最近事件。
* 当前首页按顺序渲染 hero、5 个 KPI、异常节点区、异常目标区、Group 分布区、最近事件区，所有区块视觉权重接近，缺少“优先处理什么”的主轴。
* 当前 Group 分布只从 `abnormal_nodes` / `abnormal_targets` 派生，不代表全量分组分布；文案“按 Group 分布”容易误导。
* 当前 AppShell 给 Sidebar 的 `anomalyCounts` 是 `{ nodes: 0, targets: 0 }`，侧边栏异常计数设计存在但未接真实数据。
* 当前顶层导航只有首页、节点、目标、事件、设置；这符合 v1 基线，但首页应承担进入这些页面的导航解释和快捷入口。
* 后端 `/api/dashboard` 当前接口只支持 `limit`，主要返回异常摘要和最近事件，没有全量 group 分布、待接入节点数、暂停/退役/归档数、通知通道状态等更完整的系统态势字段。
* 设计文档 v1 明确首页职责是：看当前问题分布、看高优先级对象、看最近变化。
* 设计文档 v1 后续段落已经指出“首页看完之后没有真正的工作台入口”是多对象日常使用的问题，并建议工作台视图应是保存好的列表入口，而不是第二套首页。
* 设计文档 v2 当前 DashboardPage 视觉契约仍偏“stat strip + detail sections”，与用户本次反馈相比，现有契约需要修订。

## Current UX Problems

* 没有明确状态结论：用户首先看到的是标题和若干数字，不知道“系统现在是否需要处理”。
* 没有主操作入口：异常节点/异常目标虽然能点行进入详情，但首页没有显式告诉用户去节点、目标、事件、设置分别解决什么问题。
* 同类信息重复：顶部“风险对象/严重/维护”后，节点和目标各自又重复 3 个 KPI，形成数字噪音。
* 区块之间缺少优先级：异常列表、Group、事件流都垂直堆叠，没有主栏/侧栏或任务队列结构。
* 首页对正常态价值不足：如果没有异常，首页主要显示空态和事件空态，不提供库存、配置完整度、最近观测状态、快捷导航等系统理解能力。
* 首页对首次接入路径过轻：fresh install 只有文本步骤和一个“创建第一个节点”链接，没有把“节点创建 → agent 接入 → 目标创建 → ProbeItem”设计成真正的引导工作流。
* 全站导航没有借助首页承接：侧边栏是固定导航，但首页本身没有提供“我该去哪里”的分流区。

## Product Direction

首页应从“Dashboard 大屏”转为“服务器舰队控制面工作台”。它不做自由布局、不做自定义仪表盘、不堆叠大量图表；它应回答 5 个问题：

1. 系统现在是否正常？
2. 最需要处理的对象是什么？
3. 节点、目标、事件、设置各自有没有值得关注的状态？
4. 第一次使用或系统不完整时，下一步是什么？
5. 我能不能从这里直接进入正确页面，而不是重新理解导航？

同类系统校准：

* Cockpit / Portainer 这类服务器管理界面把首页或环境页作为“进入具体管理任务”的入口，而不是把所有详情复制到首页。
* Netdata / Uptime Kuma 这类监控系统会在首页强调 live/stale/offline、warning/critical、当前最坏对象、最近事件与通知结果；候风应吸收“可信状态 + 分诊队列”，但不要复制大型监控套件的大屏模式。
* Grafana 的经验适合反向约束：dashboard 要围绕少量问题组织，降低认知负担，避免 dashboard sprawl；候风现阶段不应做拖拽/自定义/多 dashboard。
* Proxmox 的资源层级经验适合候风：全局页给出集群/资源总览和近期任务/日志，详细指标留给节点/目标详情页。

## Proposed MVP Structure

### 1. 顶部状态结论区 / Fleet State

用一个紧凑 hero / status band 取代当前泛泛的“当前风险总览”：

* 标题根据状态动态变化：
  * 有严重对象：`需要处理严重异常`
  * 有告警/关注：`存在活跃异常`
  * 无异常但有维护：`系统处于维护观察中`
  * 全部正常：`系统运行正常`
  * 首次使用：`开始接入第一台服务器`
* 副文案解释当前态势，例如 `3 个对象异常，1 个严重；最近 24h 新增 4 次异常，恢复 1 次。`
* 右侧提供主要 CTA：
  * 有异常：`查看当前异常`
  * 无异常：`查看节点`
  * 首次使用：`创建第一个节点`
* 次要 CTA：`查看事件流` / `进入设置`。
* 展示数据可信度：当前 snapshot 时间、center/API 是否加载成功；如果使用 AppShell SyncStatus，必须接真实数据或降级成静态版本信息，不能继续假装 `ok`。

### 2. 一行系统全局 KPI

保留少量真正有全局意义的指标，不再重复拆成多层数字：

* `节点`：总数 + 异常数。
* `目标`：总数 + 异常数。
* `严重`：节点 + 目标严重总数。
* `维护`：节点 + 目标维护总数。
* `24h 变化`：新增异常 / 恢复事件 + sparkline。

这些 KPI 必须可点击或包含明确跳转：

* 节点 → `/nodes`
* 目标 → `/targets`
* 严重 → 对应列表筛选（若当前列表筛选 URL 暂不支持，先跳列表并在 PRD 标记后续 URL-state）
* 24h 变化 → `/events`

### 3. 主工作区：当前需要处理

把异常节点和异常目标合并为一个“当前需要处理”队列，按 severity + 活跃异常数排序，而不是拆成两个同权区块：

* 每行显示对象类型、对象名/ID、健康状态、当前主问题、最近心跳/成功/失败、入口。
* 节点与目标共享列表样式，减少两张表造成的割裂感。
* 默认只展示最高优先级若干条；提供 `查看全部异常节点` / `查看全部异常目标` / `查看事件流`。
* 无异常时该区块转为正常态摘要：`当前没有活跃异常`，并显示最近一次事件或建议检查节点/目标库存。
* 首页只展示分诊信息，不复制节点/目标详情页里的完整 watchtower 图表。

### 4. 右侧/下方导航工作台

新增“系统入口”或“管理入口”区，不做营销卡片，而是 4 个高密度入口：

* `节点`：管理服务器、agent 接入、维护/暂停/退役。
* `目标`：管理观测目标、ProbeItem、运行状态。
* `事件`：查看异常开始、升级、恢复、维护操作历史。
* `设置`：通知、阈值、频率、保留策略。

每个入口显示 1-2 个当前状态数字，例如节点总数/异常数、目标总数/异常数、24h 事件数、通知配置状态。当前接口没有通知配置状态时，不先假装实现。

### 5. 库存健康 / 分组信息

首页应能告诉用户“系统里有什么”，但只在数据真实时展示：

* 第一轮可展示节点/目标总数、异常数、严重数、维护数。
* 当前 `groupSummaries` 只来自异常对象，必须改名为 `异常对象按 Group` 或移除。
* 如果要展示真正的 Group / region / provider 分布，需要扩展 `/api/dashboard`，不能前端伪造。

### 6. 最近变化

最近事件保留，但降为“上下文”而不是首页主体：

* 展示最近 5-10 条。
* 提供 `查看全部事件`。
* 空态说明清晰：`最近没有状态变更事件`。

### 7. 首次接入 / 系统不完整状态

如果没有节点与目标，首页应变成 onboarding 工作台：

* 4 步明确状态：创建节点、接入 agent、创建目标、添加 ProbeItem。
* 当前只知道“0 节点 + 0 目标”；后续可以增加 “有节点未绑定 agent / 有目标无 ProbeItem” 的中间状态。
* 每步都有目标页面入口，不只是一段 `<ol>`。

## Backend / API Options

### Option A: Frontend-only MVP

只使用现有 `/api/dashboard` 字段重构布局。

Pros:
* 改动小，风险低。
* 可以快速验证首页信息架构是否改善。

Cons:
* 无法真实展示全量 Group 分布、待接入/暂停/退役/归档、通知配置状态。
* 侧边栏异常计数仍需在 AppShell 拉 dashboard 或另加轻量 endpoint。

### Option B: Extend `/api/dashboard`

在现有接口增加系统入口所需字段，例如：

* `pending_onboarding_node_count`
* `paused_node_count`
* `retired_node_count`
* `paused_target_count`
* `archived_target_count`
* `group_summaries`（全量节点/目标/异常计数）
* `notification_status`（至少 telegram / feishu enabled + configured）

Pros:
* 首页可真正承担服务器管理系统总览。
* AppShell / Sidebar 可复用同一轻量概览。

Cons:
* 需要后端 SQL、类型、测试同步。
* 通知配置状态可能牵涉 settings repository，范围需控制。

Temporary recommendation:
* 第一轮采用 Option A + 清除误导性 Group 全量文案。
* 如果用户希望首页成为真正全局系统入口，第二轮采用 Option B。

## Recommended Execution Plan

### PR1: Dashboard IA Frontend Redesign

Scope:
* 重写 DashboardPage 的呈现结构：Fleet State、global KPI、attention queue、entry points、recent events、onboarding state。
* 用现有 `/api/dashboard` 字段，不扩后端。
* 移除或重命名误导性的 Group 分布。
* 更新 `DashboardPage.test.tsx`。

Why first:
* 立刻解决用户当前截图里的“混乱、无重点、无入口”问题。
* 风险低，不等待后端 contract 扩展。

### PR2: Credible Shell Counts / Freshness

Scope:
* AppShell 不再硬编码 Sidebar anomaly counts 为 0。
* 如果沿用 SyncStatus，必须给它真实来源；否则改成非健康断言的静态版本/登录信息。
* 评估是否复用 `/api/dashboard`，或新增更轻量的 shell summary endpoint。

Why separate:
* Shell 是全站体验，不应夹在 Dashboard 单页重构里偷偷改。

### PR3: Dashboard Contract Extension

Scope:
* 扩展 Go `DashboardOverview` 和 SQL，增加真实 group summaries、pending onboarding、paused/retired/archived、notification configured 等字段。
* 前端把库存健康、系统完整度和设置状态接真实数据。

Why separate:
* 这是“真正全局系统总览”的基础，但涉及后端、类型、测试与设置域数据。

### PR4: 全站管理系统基础能力补齐

Scope:
* 节点/目标列表 URL-state 筛选可从首页深链进入，例如严重、异常、维护、暂停、归档、未接入。
* 事件页高级筛选 Drawer / chip flow 继续偿还。
* 保存筛选视图作为后续工作台能力，而不是做自定义 dashboard。

## Acceptance Criteria

* [x] 首页首屏能明确表达系统状态结论，并提供符合当前状态的主 CTA。
* [x] 首页不再由多个同权重 DetailSection 线性堆叠组成；至少形成“状态结论 / 全局指标 / 当前处理队列 / 系统入口 / 最近变化”的清晰层级。
* [x] 异常节点与异常目标能被统一排序或统一呈现，用户能先处理最高优先级对象。
* [x] 首页提供到节点、目标、事件、设置的明确入口，并能说明每个入口解决什么问题。
* [x] 首次接入状态不只是文本列表，而是可操作 onboarding 工作台。
* [x] 首页不展示接口无法支撑的全量事实；旧 `按 Group 分布` 已移除，PR1 不从异常对象伪装全量 group 分布。
* [x] DashboardPage tests 覆盖严重/异常/正常/首次接入/错误态/关键跳转。
* [x] 不引入新的状态库、图表库或 CSS 体系；继续使用 `lib/api.ts`、`lib/types.ts`、atoms、BEM + tokens。

## PR1 Completion Evidence

Implemented in:
* `web/src/pages/DashboardPage.tsx`
* `web/src/pages/DashboardPage.test.tsx`
* `web/src/styles/pages.css`
* `docs/design/v2-houfeng/component-spec.md`
* `.trellis/spec/web/state-and-data.md`

Verification:
* `cd web && TMPDIR=/tmp npm run test -- --run src/pages/DashboardPage.test.tsx` passed: 7 tests.
* `cd web && TMPDIR=/tmp npm run lint` passed.
* `cd web && TMPDIR=/tmp npm run build` passed.
* `git diff --check` passed before this PRD/spec update; rerun required before commit.

## Definition of Done

* PRD 与 research 文件完整。
* `implement.jsonl` / `check.jsonl` 注入相关 web spec、设计文档和 research 文件。
* 代码实现后运行 `npm run lint`、`npm run test -- --run DashboardPage.test.tsx` 或全量 web test、`npm run build`。
* 如修改 Go dashboard contract，必须补 Go handler/store tests，并运行 Go 相关测试。
* 如发现设计规范已不符合本次方向，更新 `docs/design/v2-houfeng/component-spec.md` 或 `.trellis/spec/web/*`。

## Out of Scope

* 自定义 dashboard、拖拽布局、多 dashboard 模板。
* 新增地图视图、拓扑视图、大屏监控模式。
* 引入 React Query / Zustand / 图表库。
* 一次性重做所有页面；本任务可以记录全站原则，但实现优先聚焦首页与必要导航数据。
* 批量操作能力本身，除非只作为入口跳转，不在首页直接执行高风险操作。

## Technical Notes

* `web/src/pages/DashboardPage.tsx` 当前 500+ 行，重构时建议拆出 dashboard-local 子组件或 `components/dashboard/`，避免继续膨胀。
* `web/src/lib/api.ts` 已有 `getDashboard()`；新增 dashboard 字段需同步 `web/src/lib/types.ts`。
* `internal/center/incidents/types.go` 定义 dashboard JSON contract。
* `internal/center/store/dashboard.go` 当前 SQL 已支持 counts、异常节点/目标、24h trends、recent events。
* `web/src/app/layout/AppShell.tsx` 当前 Sidebar `anomalyCounts` 写死为 0，可作为导航体验后续改进点。
* `docs/design/v1-baseline/rules-and-interaction.md` §6.2 是当前首页职责依据。
* `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md` §13 已指出工作台入口和保存列表视图的重要性。
* `docs/design/v2-houfeng/component-spec.md` 当前 DashboardPage 契约需要随本次方案更新。
