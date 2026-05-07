# Dashboard 视觉审查与持续打磨

## Goal

继续推进 Dashboard，不把上一轮 `finish-work` 当作产品完成。当前核心问题不是字段不足，也不是跳转不足，而是页面仍然没有形成明确的首屏判断路径：用户进入首页后仍需要在多个视觉区块之间判断“哪里重要、下一步做什么”。本任务要先做视觉审查，再做一轮更强的布局与交互收敛，使 Dashboard 更接近服务器管理系统的全局工作台。

目标不是差异化地展示更多信息，而是在同类服务器管理系统应有的基础能力上做清晰入口：状态判断、异常处理、资源管理、事件追踪、设置配置。

## What I Already Know

* 用户多次指出 Dashboard 仍然混乱，并明确要求完成后及时反思，不要停止迭代。
* 前两轮已经修掉了最明显的信息仓库问题：右侧 facts、`系统快捷入口`、`Group 摘要`、`最近事件摘要` 等不应在异常态继续展开。
* 最新代码仍保留三段式首屏：紧凑状态条、独立四个摘要 chip、`DetailSection` 工作台。
* 独立摘要 chip 虽然比 5 个 KPI 卡片轻，但它仍然形成第二个首页主体，继续与“当前需要处理”竞争注意力。
* `Fleet State`、`Dashboard 摘要`、`处理队列`、`当前需要处理` 等标签同时出现，技术标签偏多，管理首页的判断语言还不够直接。
* 异常队列当前仍使用完整 `DataTable`。它对多行运维处理有效，但在 1 个异常对象时会显得空、重、像列表页截断，而不是首页工作台。
* 当前质量门只证明测试、lint、build 通过，不能证明页面视觉层级已经达到用户目标。

## Reflection

这次问题暴露出之前设计与流程的两个缺陷：

1. **设计仍然偏“保留信息”而不是“组织行动”**：上一轮把右侧 rail 删掉后，摘要 strip 继续独立存在，页面仍像多个模块并排，而不是一个清晰工作流。
2. **完成定义偏工程正确，缺少视觉出口**：单测能防止旧模块回归，却不能判断首屏是否足够聚焦、是否让用户知道从哪里下手。后续 Dashboard 任务必须把实际页面审查作为验收条件。

新的完成标准：Dashboard 在异常态首屏应只有一个主判断区域和一个主处理区域；摘要、入口和元数据必须服务这两个区域，不能继续独立扩张。

## Requirements

### 1. 先审查再改动

实现前必须记录当前页面问题，至少覆盖：

* 首屏顶层区块数量与视觉权重。
* 异常态用户下一步是否明确。
* 摘要指标是否重复、抢注意力或只是“为了展示 contract”。
* 单条异常、多条异常、正常态、维护态、首次接入态的页面密度。
* 可跳转入口是否足够清晰且不喧宾夺主。

审查结果写入 `research/visual-audit.md`，并把最关键的 3 个问题转化为实现约束。

### 2. 异常态收敛为“判断 + 处理”

当存在异常对象时，Dashboard 顶层结构应收敛到：

* 一个紧凑的状态/指令区：说明当前系统状态、生成时间、最重要 CTA。
* 一个处理工作区：展示当前最高优先级对象和相关跳转。

要求：

* 不再渲染独立的 `Dashboard 摘要指标` 区块。
* 关键数字可以内联到状态区或处理区中，作为辅助信息，而不是形成第二个卡片 strip。
* 主 CTA 保持明确：严重异常优先到 `/events?severity=严重`，一般异常到 `/events?time_range=24h`。
* 异常处理相关深链必须保留：异常节点、异常目标、事件流。
* 不恢复 `系统快捷入口`、`Group 摘要`、`最近事件摘要` 或 API facts。

### 3. 处理区要适配首页，而不是复制列表页

异常对象较少时，首页不应出现“一个巨大表格只放一行”的空重感。

允许实现方向：

* 将异常态从完整表格改为紧凑处理列表，每项含对象、类型/状态、主问题、freshness 和进入链接。
* 或保留 `DataTable`，但必须让容器和行密度更像首页工作队列，不出现大面积空白或过重表头。

硬性要求：

* 行/项点击进入节点或目标详情。
* 操作链接点击与键盘事件不能触发行点击。
* 最高优先级排序仍按严重度和活跃异常数。
* 展示上限仍要防止首页变成长列表。

### 4. 正常、维护、首次接入状态继续保持清晰

正常态：

* 重点是“系统运行正常”和管理入口。
* 管理入口应紧凑，但不能变成另一组大卡片。
* 不展示最近事件列表或 group 列表。

维护态：

* 不把维护态伪装成紧急异常。
* 维护相关数字可以作为次级信息，主要入口是维护事件和管理页面。

首次接入态：

* 只展示 onboarding 路径，不展示摘要 strip、空库存、空事件或空 group。

### 5. 视觉与交互质量约束

* 顶层区块数量应明显少于前一轮；异常态不应再是 `状态条 + 摘要条 + 大工作台` 三段式。
* 文案以中文操作判断为主，英文 eyebrow 只在确有结构价值时保留；避免技术标签堆叠。
* 使用现有 tokens、BEM、atoms；不引入 UI 框架、CSS-in-JS 或新依赖。
* 交互元素必须可键盘访问，链接使用 React Router `Link`，按钮只用于动作。
* 长主机名、ID、摘要文案必须能截断或换行，不破坏布局。
* 改动后启动本地 dev server，提供可预览 URL；如果无法自动截图，必须明确说明可视化验证的实际方式和限制。

### 6. Tests must enforce the new shape

更新 `DashboardPage.test.tsx`，避免再次把“独立摘要 strip”锁死：

* 异常态不出现 `Dashboard 摘要指标` 独立区块。
* 异常态仍展示关键计数和异常处理深链。
* 异常态不出现 `系统快捷入口`、`Group 摘要`、`最近事件摘要`、`系统全局指标`、API facts。
* 正常态与维护态不恢复 Group/recent/context rail。
* 首次接入态不出现摘要 strip、库存噪音、事件噪音。
* 行/项点击、操作链接 click 和 keyboard propagation 继续通过。

## Acceptance Criteria

* [ ] `research/visual-audit.md` 记录了当前剩余视觉问题和本轮收敛约束。
* [ ] 异常态 Dashboard 不再渲染独立 `Dashboard 摘要指标` 区块。
* [ ] 异常态首屏只保留状态/指令区与当前处理区两个主视觉区域。
* [ ] 关键计数仍可见，但以辅助信息形式并入主流程。
* [ ] 异常节点、异常目标、严重事件、24h 事件深链不回退。
* [ ] 异常处理区不再像一张只有一行的大列表页；单条异常也能显得紧凑、明确。
* [ ] 正常态、维护态、首次接入态继续比异常态更轻，不恢复 Group/recent/context rail。
* [ ] `DashboardPage.test.tsx` 覆盖上述结构防回归。
* [ ] 需要时同步 `docs/design/v2-houfeng/component-spec.md` 与 `.trellis/spec/web/state-and-data.md`，把“独立摘要 strip 不是硬性结构”修正为最新契约。
* [ ] `cd web && TMPDIR=/tmp npm run test -- --run src/pages/DashboardPage.test.tsx`、`cd web && TMPDIR=/tmp npm run test -- --run`、`cd web && npm run lint`、`cd web && npm run build`、`git diff --check` 通过。
* [ ] 本地 dev server 已启动并给出 URL，供用户直接查看最新 Dashboard。

## Out of Scope

* 不修改 Go 后端和 `/api/dashboard` contract。
* 不修改 Nodes/Targets/Events 页的 URL-state contract。
* 不引入 Playwright/Cypress 或新视觉回归框架。
* 不做全站视觉系统重写。
* 不做自定义 Dashboard、拖拽布局、保存视图。
* 不改认证流程或权限模型。

## Technical Notes

* 主要代码：`web/src/pages/DashboardPage.tsx`。
* 主要样式：`web/src/styles/pages.css` 中 `dashboard-*`。
* 测试：`web/src/pages/DashboardPage.test.tsx`。
* 文档/spec：`docs/design/v2-houfeng/component-spec.md`、`.trellis/spec/web/state-and-data.md`。
* 参考上一轮任务：`.trellis/tasks/archive/2026-05/05-07-dashboard-decision-simplification/prd.md`。
* PR4 深链约束参考：`.trellis/tasks/archive/2026-05/05-07-dashboard-deep-links-url-state/prd.md`。
* Web Interface Guidelines 来源：<https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md>。
