# Dashboard 决策优先简化

## Goal

继续修正 Dashboard。上一轮 `dashboard-ia-convergence` 在工程上完成了，但没有达到用户最初目标：页面仍然混乱，信息仍然摊开，用户仍需要同时理解 hero、facts、5 个 KPI、处理表格、右侧快捷入口、Group、最近事件。此轮目标是把 Dashboard 改成真正的“决策优先首页”：异常态先回答“我现在该处理什么”，正常态先回答“系统是否稳定、我要去哪里管理”，首次接入只回答“下一步接入什么”。

本任务的核心是删减和渐进披露，不是继续把信息换一个容器摆放。

## What I Already Know

* 用户在 2026-05-07 再次反馈当前 Dashboard 仍然“很乱”，并要求及时反思。
* 截图显示当前页面仍然有三层强视觉区：
  * 大型 Fleet State hero，右侧有 4 个 boxed facts。
  * 5 个等权 KPI 卡片。
  * 一个大工作台，左侧处理队列，右侧同时展示系统快捷入口、Group 摘要、最近事件摘要。
* 上一轮的失败原因是把“减少独立 section 数量”误当成“降低认知负担”。结果只是从纵向堆叠变成横向堆叠。
* 当前测试仍在断言 `系统快捷入口`、`Group 摘要`、`最近事件摘要` 在异常态可见，这会把混乱锁死。
* 当前 `/api/dashboard` contract 足够支撑首页，但 Dashboard 不应该把 contract 的所有字段都显示出来。
* PR4 深链仍然是必要能力，必须保留核心跳转语义。

## Reflection

上一轮设计存在三个根本问题：

1. **没有真正做取舍**：系统入口、Group、最近事件从完整 section 变成右侧 rail，但仍然全部展示。用户看到的信息数量没有实质减少。
2. **首屏层级错误**：hero 的右侧 facts 和 KPI strip 过度强调数字与元数据，抢走了“当前需要处理”的注意力。
3. **验收标准错误**：测试证明了旧标题不存在，但没有证明用户不再被次要信息干扰；甚至继续要求右侧摘要全部存在。

新的约束：Dashboard 不是全部系统事实的展台，而是进入正确工作流的起点。

## Requirements

### 1. 异常态首屏只保留一个主决策路径

当 `abnormal_node_count + abnormal_target_count > 0` 时：

* 首屏必须以异常处理为中心，直接呈现最高优先级对象。
* 允许保留的可见内容：
  * 紧凑状态结论：标题、简短说明、主 CTA。
  * 最多 3-4 个紧凑状态 chip 或 summary item，用于节点/目标/严重/24h 变化。
  * 当前需要处理队列或 top incident list。
  * 队列相关深链：查看全部异常节点、查看全部异常目标、查看事件流。
* 不允许在异常态首屏始终展示：
  * 4 个 boxed API/facts 卡片。
  * 5 个大型 KPI 卡片 strip。
  * `系统快捷入口` heading + 四项详细描述。
  * `Group 摘要` heading 和列表。
  * `最近事件摘要` heading 和事件列表。
* 系统入口仍必须可达，但应作为紧凑动作区或下拉/secondary action，不要展示四段说明和状态文字。
* Group 和最近事件不应在异常态首屏展示；用户通过异常节点、异常目标、事件流进入详情页。

### 2. Hero 从大面板降为紧凑状态栏

当前 Fleet State hero 过大，且右侧 facts 形成噪音。新设计要求：

* 移除右侧 boxed facts definition list。
* `snapshot_generated_at` 可以作为状态说明下方的 muted inline metadata，例如 `Dashboard 摘要 16:20`。
* 库存、队列、API loaded 不再各自成卡片。
* CTA 保留主次关系：主 CTA 强，次 CTA 和设置为轻量链接或 secondary action。
* 状态栏高度应显著低于当前 hero，不再成为页面大屏式头图。

### 3. KPI 从 5 张大卡变成摘要 chips

KPI 的作用是辅助决策，而不是形成第二个首页主体。

* 移除大型 `dashboard-kpi-strip` card layout。
* 改为紧凑 summary rail/chips，最多显示 4 个高价值维度。
* 异常态优先显示：异常对象、严重、24h 变化、维护。
* 正常态优先显示：节点、目标、24h 变化、通知配置。
* 每个 summary item 仍可链接到 PR4 支持的路由。
* 不需要 Sparkline，除非它不会增加卡片高度和视觉噪音。

### 4. 工作台去掉右侧信息摊开

当前右侧 rail 是新的混乱来源。新设计要求：

* 异常态工作台只展示处理队列，不展示右侧 context rail。
* 正常态和维护态可以展示一个紧凑“管理入口”行，但不要同时展示 Group + recent + shortcuts 三组内容。
* Group 和 recent events 作为 Dashboard 次级信息，默认不在首屏展开。可以通过 `查看事件流`、`查看节点`、`查看目标` 跳转解决。
* 如果保留 secondary context，只允许一个轻量提示区，不允许多个 heading/list 连续出现。

### 5. 正常态是管理入口，不是空态或信息仓库

当没有活跃异常：

* 显示 `系统运行正常` 或 `维护观察` 的紧凑状态栏。
* 主体展示“管理入口”而非空表格：
  * 节点
  * 目标
  * 事件
  * 设置
* 每个入口可以只有标题、一个数字/状态和链接，不要写长描述。
* 最近事件只保留一个 `查看事件流` 链接，不默认列出事件列表。

### 6. 首次接入态继续极简

首次接入只展示：

* 紧凑状态栏。
* onboarding steps。
* 必要入口。

不展示 KPI strip、Group、Recent、API facts。

### 7. Tests must enforce simplification

更新 `DashboardPage.test.tsx`，明确断言：

* 异常态不出现 `系统快捷入口`、`Group 摘要`、`最近事件摘要` heading。
* 异常态不出现 `系统全局指标` 的 5-card strip。
* 异常态仍能通过 summary/action links 到：
  * `/nodes?abnormal=1`
  * `/targets?abnormal=1`
  * `/events?time_range=24h`
  * `/events?severity=严重`
* 正常态不出现 `当前需要处理` 和右侧 context rail。
* 首次接入态不出现 KPI strip、Group、Recent。
* 行点击、action link click/keyboard propagation 继续通过。

## Acceptance Criteria

* [ ] 截图中的右侧大 facts 卡组被移除或降为一行 muted metadata。
* [ ] Dashboard 不再渲染 5 个大型 KPI 卡片；改为紧凑 summary/chip。
* [ ] 异常态首屏只展示状态结论、摘要、处理队列和相关跳转，不展示 Group/recent/shortcut 详情 rail。
* [ ] 正常态和维护态展示管理入口，但不把 Group 和 recent events 展开成列表。
* [ ] 首次接入态保持 onboarding-only，不展示库存/事件噪音。
* [ ] PR4 深链和处理队列行交互不回退。
* [ ] `DashboardPage.test.tsx` 的断言防止再次把全部信息摊开。
* [ ] `docs/design/v2-houfeng/component-spec.md` 与 `.trellis/spec/web/state-and-data.md` 同步更新，明确 Dashboard 默认不展示所有 contract 字段。
* [ ] `cd web && TMPDIR=/tmp npm run test -- --run src/pages/DashboardPage.test.tsx`、`cd web && npm run lint`、`cd web && npm run build`、`git diff --check` 通过。

## Out of Scope

* 不改 Go 后端和 `/api/dashboard` contract。
* 不改 Nodes/Targets/Events 页 URL-state。
* 不做自定义 dashboard、保存视图、拖拽布局。
* 不引入新依赖。
* 不做完整视觉系统重写；本轮只修 Dashboard 首屏决策路径。

## Technical Notes

* 主要代码：`web/src/pages/DashboardPage.tsx`。
* 主要样式：`web/src/styles/pages.css` 中 `dashboard-*`。
* 测试：`web/src/pages/DashboardPage.test.tsx`。
* 文档：`docs/design/v2-houfeng/component-spec.md`、`.trellis/spec/web/state-and-data.md`。
* 参考上一轮失败任务：`.trellis/tasks/archive/2026-05/05-07-dashboard-ia-convergence/prd.md`。
* UI review notes：`.trellis/tasks/05-07-dashboard-decision-simplification/research/web-interface-guidelines-dashboard-review.md`。
