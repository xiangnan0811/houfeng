# 详情页 IA 统一：VPS / 节点 / 入口详情页 + 节点接入流程改造

## Goal

三个详情页（VPS 详情 `/vps/:id`、节点详情 `/nodes/:id`、入口/Target 详情 `/targets/:id`）在早期列表页统一时被遗漏，仍是旧候风设计：未统一到 v2 设计哲学、存在割裂感；主次不分（重要内容在角落、不重要信息用大卡片）；按钮被外层框体遮挡无法点击；节点接入工作台是独立路由、令人困惑。本任务把三个详情页统一到 v2 设计语言（五级信息层级、8pt 节奏、复用 `.page-stack`/`.watchtower-header`/`.card` 体系），把接入流程从独立页面收敛进节点详情页，并修复审计发现的功能性 bug。

## Decisions (grill-me 已确认)

1. **接入流程**：不强制使用弹窗，允许设计更合理的实现方式（内联区块 / 抽屉 / 弹窗按 IA 合理性选择）。
2. **MetricChart**：保守——保留节点详情 8 张大图，只修渲染 bug，不动信息层级 / 不改 160px 高度 / 不做大小分级。
3. **重构激进度**：激进——允许重写三个详情页的页面结构（非仅微调 CSS）。

## Audit Findings (6 维度审计 + 定向核实)

### Critical（功能性破损 / 用户点名）

- **C1 — VPS hero 下拉被裁剪（= 用户说的「按钮被遮挡无法点击」）**
  `web/src/pages/vps-detail/VPSDetailHero.tsx` 把操作下拉菜单放在 `overflow: hidden` 的 `.page-panel` 内（hero 容器 line 38 + 菜单 54-77），菜单展开后被外层框体裁剪、不可点。
  对照正确模式：节点/入口详情用 `.watchtower-header`（sticky，**非** overflow:hidden）置于 `.page-stack`（无 overflow）内。**修复方向**：VPS hero 对齐 watchtower-header 模式，移除裁剪容器。
- **C2 — VPS 重复网格 / 主次不分**
  `VPSDecisionWorkbench.tsx` 与 `VPSOperationsSummary.tsx` 各自维护一套自定义网格 + 硬编码 px，信息重复、层级混乱（用户说的「重要内容在角落、不重要信息用大卡片」）。统一到 `.metric-grid` + `.card` 体系并去重。

### High

- **H1 — `--row-bg` / `--row-bg-hover` 未定义**
  `index.css` 多处引用这两个 token 但三套主题都未定义 → 表格行底色 / hover 静默失效。补定义或改用既有 surface token。
- **H2 — 三页大量硬编码 px + 自定义网格**，未走 `--space-*` 8pt 节奏与复用类，是「割裂感」根因。

### 趋势图核实结论（定向查证，非 workflow）

- `MetricChart.tsx`（489 行）**组件健壮**：空数据→占位「暂无观测数据」；单点→「样本不足」；退化/平坦 Y 域→对称 padding；阈值线 `projectY` clamp；ResizeObserver 响应式宽度；tooltip 经 xPercent 定位。
- `NodeWatchtowerMetrics.tsx` **调用方干净**：8 卡按阈值优先级排序（critical 在前），`height={160}` 是有意统一大图。
- **唯一可疑**：tooltip（z-index 5）可能被 sticky `.watchtower-header`（z-index 10）遮挡——实现时浏览器核实。
- **结论**：无严重渲染 bug。按决策保留 8 大图，仅修 tooltip 层级（如确证）。

## Requirements

### R1 — VPS 详情页（激进重写）
- 移除裁剪 hero 下拉的 `overflow:hidden` 容器，操作菜单对齐节点/入口的 `.watchtower-header` 模式（sticky 头、菜单不被裁剪）。
- 合并 `VPSDecisionWorkbench` / `VPSOperationsSummary` 的重复网格，统一到 `.metric-grid` + `.card`，按五级层级排布：标识 → 当前决策/状态 → 上下文（服务商/续费/网络）→ 历史/事件 → 危险区。
- 去硬编码 px，改用 `--space-*` 8pt 节奏与复用类。

### R2 — 节点详情页（保守 + 结构对齐）
- 保留 8 张 `MetricChart` 大图与排序逻辑不变；仅在确证 tooltip 被 sticky 头遮挡时修 z-index。
- 页面结构对齐 v2：binding/danger 顺序、retire 确认放入危险区隔离。
- 接入入口收敛进本页（见 R4）。

### R3 — 入口/Target 详情页（激进重写）
- 概览 / 介绍 prose / 生命周期混排重组为 v2 五级层级，去自定义网格 + 硬编码 px。

### R4 — 接入流程收敛（删除独立路由）
- 删除 `/nodes/:nodeId/onboarding` 独立路由与 `NodeOnboardingPage.tsx`。
- 节点详情页已调用 `getNodeOnboarding` / `confirmNodeRebind` 等，仅缺 `issueNodeInstallCommand`——补上后在详情页内联完成接入（按 grill-me 决策，形态选最合理的：内联区块/抽屉，不强制弹窗）。
- 清理所有指向 onboarding 路由的导航与死链：`router.tsx`、`NodeBindingConflictSection.tsx`、`NodeWatchtowerHeader.tsx`、`NodesPage.tsx`、`nodeHelpers.ts`、`NodesActionsCell.tsx`、`Breadcrumb.tsx`、`CreateNodeDrawer.tsx`。

### R5 — 跨页 CSS 修复
- 补定义或替换 `--row-bg` / `--row-bg-hover`（三套主题），修复表格行底色/hover 静默失效。

## Acceptance Criteria
- [ ] VPS hero 操作菜单完全可见可点击（浏览器核实，非仅编译过）。
- [ ] 三个详情页无硬编码 px 布局，统一走 `.page-stack`/`.watchtower-header`/`.metric-grid`/`.card` + `--space-*`。
- [ ] 三个详情页符合 v2 五级信息层级，主次分明、危险区隔离。
- [ ] `/nodes/:nodeId/onboarding` 路由删除，全仓无指向该路由的死链（grep 验证）。
- [ ] 节点详情页可完成完整接入（含 `issueNodeInstallCommand`），三套主题正常。
- [ ] 8 张 MetricChart 保留；tooltip 不被遮挡（浏览器核实）。
- [ ] `--row-bg`/`--row-bg-hover` 在三套主题正确生效。
- [ ] `npm run build` + `npm run lint` + `npm run test` 全绿。
- [ ] 三套主题（houfeng-dark/light、classic-dark）下三个详情页视觉一致、无割裂。

## Definition of Done
- 单页一 PR，串行单分支（三页共改 `index.css`，并行写会冲突，worktree 隔离被禁）。
- build/lint/test 全绿；UI 改动浏览器核实（golden path + 边缘态：空数据/加载/错误/主题切换）。
- 死链 grep 清零；docs 如涉及用户可见行为则同步。

## Out of Scope
- 后端任何改动（接入/绑定 API 已存在）。
- 列表页（已在前序任务统一）。
- MetricChart 信息层级 / 大小分级 / 图表库引入。
- 旧 ChangePasswordModal 的 modal CSS 统一。

## Technical Approach & Implementation Plan
四阶段串行（单分支，按页/关注点切 PR）：
1. **C1 VPS hero 裁剪修复**（最紧急，真实功能破损）——独立小 PR。
2. **接入流程收敛 R4**——补 `issueNodeInstallCommand`、详情页内联接入、删路由 + 清死链。
3. **三详情页 IA 统一 R1/R2/R3 + R5**——结构重写 + CSS 复用类 + row-bg 修复。
4. **MetricChart tooltip 核实修复**（如确证遮挡）。

## Technical Notes
- v2 视觉权威：`docs/design/v2-houfeng/design-language.md` + `component-spec.md`。五级层级、8pt、section 间距 `--space-5`。
- 正确头部模式：`.watchtower-header`（sticky，非 overflow:hidden）置于 `.page-stack`（无 overflow）。
- 关键文件清单：见 Audit Findings 各条引用的文件路径与行号。
- 所有样式集中在单一 `web/src/index.css`（~2095 行）——这是串行实现、不可并行写的根因。
