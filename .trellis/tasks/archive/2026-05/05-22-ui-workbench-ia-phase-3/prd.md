# Frontend workbench IA phase 3

## Goal

将候风前端第三阶段信息架构从“多个 `page-panel` 纵向平铺”重构为“一个主工作区 + 一个辅助上下文区”的 workbench 布局。目标不是继续调颜色、阴影、渐变或文案，而是改变一级页面的布局骨架和视觉优先级，让 Dashboard、VPS、Nodes、Targets、Asset Decisions 更像个人 VPS 舰队驾驶舱 / 决策系统。

## Requirements

### 全局约束

- 只修改 `web/src`。
- 不修改后端代码。
- 不修改 API 协议、请求路径、请求参数或响应 contract。
- 不改变路由结构。
- 不引入大型 UI 框架、CSS 框架、状态库或新构建工具。
- 保留现有主题 token、候风 v2 dark-first 气质、中文为主和高密度工程工具感。
- 可以新增小型布局组件和 CSS class。
- 可以拆分 / 合并现有 React 组件。
- 可以删除、折叠或降级低优先级展示块。
- 不要为了保守而保留所有现有框体。

### 新增 / 抽象布局模式

- 新增 `CompactHeader`：承载页面名、关键数字、主 CTA；不使用 `page-panel` 大框。
- 新增 `WorkbenchPage`：提供轻量 header strip、主工作区 `main`、右侧辅助栏 `aside`。
- 新增 `ListWorkbench`：提供列表工具条、quick view tabs、inline active filter chips、`DataTable`/队列主体、可选 priority strip、可选 aside context。
- 扩展 `FilterBar` compact/inline/drawer 模式，列表页使用 inline，Drawer 内使用 drawer/default。
- 新增 CSS class：`workbench-layout`、`workbench-main`、`workbench-aside`、`compact-header`、`table-workbench` 等 BEM 家族。

### 硬性布局约束

- 每个一级页面首屏最多只能有一个主要框体。
- 页面标题不再用 `page-panel` 大卡片承载。
- 筛选不作为独立大面板出现在主流程中。
- 批量操作默认隐藏；仅在选中对象、打开批量操作、出现错误、正在提交或有待确认动作时显示。
- 自动刷新、说明文字、证据边界、次级链接降级为辅助栏或 `<details>`。
- 并列卡片必须属于同一维度，不把不同语义内容并列成三张同权卡。
- 只有一句说明和一个按钮的区域不得单独占一个 `page-panel`。
- 表格页的 `DataTable` / 队列必须成为主视觉。
- 移动端可退化为纵向布局；桌面端必须形成主次分区。

### 页面要求

#### DashboardPage / DashboardCommandSurface

- 保留现有计算逻辑：`assetPressureCount`、`nextActions`、`assetRows`、`observabilityRows`。
- 新增 `primaryMode`：`onboarding | critical | asset | observation | maintenance | normal`。
- 根据 `primaryMode` 只在主工作区展示一个优先队列 / 工作区：
  - `critical`：严重异常对象优先。
  - `asset`：资产决策队列优先。
  - `observation`：观测异常优先。
  - `maintenance`：维护 / 暂停观察优先。
  - `normal`：运行概览优先。
  - `onboarding`：首次接入路径优先。
- 原资产决策队列、观测异常队列、次级动作不再三列同权并排。
- 刷新、自动刷新、生成时间和次级链接移入右侧辅助栏。
- `dashboard-command-focus` 三个卡片改为顶部 compact stats，不再作为三张同权卡片展示。

#### VPSPage

- 删除顶部 `page-panel` 大标题框，改为 `CompactHeader`。
- 删除或合并 `vps-inventory-command` 大面板。
- `DataTable` 所在区域提前成为主工作区。
- quick view tabs、订阅证据状态、高级筛选、active chips 合并为表格工具条。
- `vps-inventory-focus` 三个卡片改为 compact stats，不单独成卡。
- “当前 lens” summary 删除，tabs 已表达当前视图。
- “VPS 库存表”标题删除或降级为 visually quiet / compact label。
- 保留现有 fetch 顺序、URL-state、订阅证据失败边界、Drawer apply/cancel、创建 VPS payload 与跳转。

#### NodesPage

- 删除或拆分 `NodesSupportSurface`，不再作为主流程大 panel。
- 保留 `topEvidence`，改成表格上方 `NodePriorityStrip`。
- 资产判断支撑、VPS 关联、严重事件、资产决策入口移入右侧 aside。
- `NodesToolbar` 分两层：quick tabs + count；filters/actions 作为 secondary row。
- “选择 2 个节点可对比”不常驻，只在 `compareSet.size > 0` 时显示状态。
- 批量操作保持现有条件显示，但视觉贴近表格工具区，不成为独立大块。
- 保留 URL-state、draft/applied filter 分离、运行控制、焦点恢复、行导航 guard。

#### TargetsPage

- 按 NodesPage 的新结构同步改造。
- `TargetsSupportSurface` 不再作为主流程大 panel。
- 异常、暂停、归档、覆盖缺口变成 quick view tabs、priority strip 或侧栏指标。
- 筛选逻辑进入高级筛选 Drawer 或表格工具条，不独立平铺。
- 表格成为主工作区。
- 保留 Target 运行控制、元数据编辑、batch 行为、sparklines、行导航、创建 Drawer。

#### AssetDecisionsPage

- 队列是主角，`asset-decision-queue` 必须提前成为主工作区。
- 顶部只保留 `CompactHeader`：资产决策、当前队列数、优先处理数、续费窗口选择。
- summary `dl`、focus cards、证据边界、续费候选证据移到右侧 aside 或 details。
- 续费候选证据表默认折叠，或放到右侧摘要入口，不再默认在底部占一个大 `page-panel`。
- 保留队列排序/筛选、决策更新 Drawer、联动 notice、续费窗口重载、订阅证据失败边界。

## Acceptance Criteria

- [ ] Dashboard 首屏一眼能知道今天第一件事是什么。
- [ ] Dashboard 只展示一个主优先队列 / 工作区，不再把资产、观测、次级动作同权并排。
- [ ] VPS 页首屏主角是库存表 / 队列，不是标题、说明和多个证据面板。
- [ ] VPS quick view、订阅证据、高级筛选和 active chips 收束在表格工具条或 aside。
- [ ] Nodes 页优先对象和列表是主角，资产判断支撑进入 priority strip / aside。
- [ ] Targets 页与 Nodes 页保持相同 workbench 结构，表格成为主工作区。
- [ ] Asset Decisions 页首屏主角是统一决策队列，续费候选证据默认折叠或辅助化。
- [ ] 每个一级页面不再出现连续 3 个以上 `page-panel` 纵向堆叠。
- [ ] 筛选、提示、按钮收束在工具条、侧栏、Drawer 或 details 内，不到处横排或堆叠。
- [ ] 批量操作默认隐藏，并只在原有条件满足时显示。
- [ ] API 请求、URL-state、Drawer 取消/关闭、row click guard、decision save、batch actions 的现有行为保持不变。
- [ ] 新增布局组件有 colocated tests。
- [ ] 更新页面测试覆盖主工作区、默认隐藏状态、关键交互与请求不变。
- [ ] `make verify-web` 通过。
- [ ] 对 Dashboard、VPS、Nodes、Targets、Asset Decisions 做本地 browser sanity，并记录 local-only evidence。

## Definition of Done

- Trellis task 已启动并记录研究/实现/检查上下文。
- 代码只改 `web/src` 与当前任务必要的 `.trellis` 文件。
- 新增/修改组件符合命名导出、受控 slots、BEM + token 样式规范。
- 所有受影响页面测试和新增组件测试通过。
- `make verify-web` 通过；如本地 Node 版本或 npm audit 有 caveat，明确记录。
- 浏览器 sanity 记录在当前任务 `research/` 目录。
- 完成后按 Trellis finish 流程归档、提交、更新 journal。

## Technical Approach

- 先新增 route-agnostic layout primitives，再逐页迁移。
- 优先迁移 list/table/queue pages，因为这些页面最能暴露“表格不是主角”的问题。
- Dashboard 最后迁移，因为它需要在保留现有优先级决策函数的前提下新增 `primaryMode`。
- CSS 只落在 `web/src/styles/pages.css` 和 `web/src/components/filters/filters.css`，不新增 page-local CSS。
- 页面行为状态机不抽到共享组件；共享组件只承载布局 slots。
- 每迁移一个页面先跑对应 targeted Vitest，再继续下一个页面。

## Decision (ADR-lite)

**Context**: 前两阶段已做视觉去装饰和文案压缩，但页面仍像多个功能块平铺，主要问题已经不是视觉风格，而是一级页面的布局骨架和主次关系。

**Decision**: 采用共享 workbench 布局 primitives + 逐页迁移。顶部标题改 `CompactHeader`，列表/队列页改 `ListWorkbench`，页面改 `WorkbenchPage` 主/侧栏结构。低优先级说明、刷新、证据边界、次级链接移动到 aside/details/Drawer。

**Consequences**: 这会触及五个页面和共享 CSS，范围较大；但可以保持 API/state 不变，只移动展示结构。通过页面级 checkpoints 和现有测试请求断言控制回归。

## Out of Scope

- 不做后端/API/数据库/路由改动。
- 不新增第三方 UI 框架、CSS 框架、状态库、图表库或 e2e 框架。
- 不重做候风主题 token 或四套主题。
- 不把详情页、Settings、Events、Login、Onboarding 纳入本批重构。
- 不新增截图资产或 tracked raster evidence，除非用户另行批准。
- 不把 Dashboard 扩展成全量 KPI dashboard。
- 不改变 Asset Ledger 的数据语义，例如从 `active_node_link_count` 推导 Node health。

## Research References

- [`research/workbench-layout-codebase.md`](research/workbench-layout-codebase.md) — repo-local page/layout/filter/table patterns, CSS locations, relevant specs, and implementation risks.

## Technical Notes

- 当前分支必须是 feature branch；本地 `main` 禁止直接开发。
- 相关规范：`.trellis/spec/web/{component-conventions,styling-guidelines,state-and-data,directory-structure,quality-guidelines}.md`、`.trellis/spec/guides/branch-workflow-governance.md`。
- 视觉权威：`docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`。
- `workbench-layout`、`compact-header`、`table-workbench` 当前不存在，需要新增。
- `page-panel` 仍可用于详情页、Drawer 内复杂局部区域、empty/error states，但不作为一级列表页默认容器。
