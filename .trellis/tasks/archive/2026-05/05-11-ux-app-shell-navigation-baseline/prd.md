# UX-1 app shell navigation baseline

## Goal

落实 `docs/release/core-pages-product-ux-replan.md` 的第一批实现：重置 App shell / 导航 / 视觉基线，让用户进入应用时先看到“总览 + 资产 + 观测 + 系统”的产品结构，而不是后端资源表平铺。

本任务只改壳层和导航基线，不重做 Dashboard、VPS 列表、资产决策页或 VPS 详情页的内部布局。

## What I Already Know

- 用户已确认继续推进核心页面产品/UX 重新规划。
- 上一任务已合并 `docs/release/core-pages-product-ux-replan.md`，明确下一步从 UX-1 开始。
- 当前 `web/src/app/metadata.ts` 只有扁平 `PRIMARY_NAV_ITEMS`：`首页 / VPS / 服务商 / 订阅 / 资产决策 / 节点 / 目标 / 事件 / 设置`。
- 当前 `Sidebar.tsx` 直接 map 扁平导航，因此 Asset Ledger 与 Fleet Observability 的主从关系无法被表达。
- `docs/design/v2-houfeng/component-spec.md` 对 Sidebar 的现有视觉契约仍有效：220px、`bg-sidebar`、brand、hairline divider、active accent 左条、节点/目标 count badge 固定 neutral。
- `.trellis/spec/web/styling-guidelines.md` 要求 dark-first、纯 CSS、BEM、tokens，AppShell 子树样式落 `web/src/app/layout/layout.css`。
- 本轮不使用 subagent。

## Requirements

### 1. 导航模型从扁平改为分组

需要在 `metadata.ts` 中表达稳定的导航分组模型，并让 Sidebar 渲染分组：

- 总览：`工作台` -> `/`
- 资产：`资产决策` -> `/asset-decisions`、`VPS` -> `/vps`、`服务商` -> `/providers`、`订阅` -> `/subscriptions`
- 观测：`节点` -> `/nodes`、`目标` -> `/targets`、`事件` -> `/events`
- 系统：`设置` -> `/settings`

保留兼容旧测试/调用方的 `PRIMARY_NAV_ITEMS` 导出，但它应从分组模型派生，而不是继续手写扁平权威。

### 2. Sidebar 视觉要强化产品结构

Sidebar 应：

- 渲染分组标题，降低“资源列表平铺”感。
- 把 `首页` 文案收敛为 `工作台`。
- 保持节点/目标异常 count badge，只在节点/目标上出现，且仍为 neutral tone。
- 保持 active route 的左侧 accent 条和可访问的 link 语义。
- 保持 brand、SyncStatus、UserChip 不退化。
- 在移动宽度下仍可读，不发生文本挤压或 layout 重叠。

### 3. 壳层密度与 chrome 微调

允许在 `layout.css` 中做小范围 shell 视觉基线重置：

- sidebar 宽度可小幅调整以容纳分组标题和中文文案。
- nav item 间距、group title、active/hover 背景可以重调。
- main padding/top-bar 可以收紧或增强沉稳感。

不允许：

- 引入新依赖、图标库或 CSS 框架。
- 改动 `styles/pages.css` 里的具体页面布局。
- 改动页面内部业务结构。
- 改动后端/API/路由路径。

### 4. 测试覆盖

需要更新或新增测试，至少覆盖：

- Sidebar 渲染分组标题与新的 `工作台` 标签。
- `PRIMARY_NAV_ITEMS` 仍包含可用扁平导航项，顺序与 Sidebar 分组一致。
- 节点/目标 count 仍只出现在节点/目标 nav 上，且不是 alert/critical tone。
- AppShell 仍渲染导航 chrome、用户信息、同步状态，并设置 document title。

## Acceptance Criteria

- [x] `metadata.ts` 有分组导航模型，`PRIMARY_NAV_ITEMS` 从分组模型派生。
- [x] Sidebar 渲染 `总览 / 资产 / 观测 / 系统` 分组。
- [x] 导航中 `首页` 改为 `工作台`，路由仍是 `/`。
- [x] 节点/目标异常 count 行为不回退。
- [x] `layout.css` 的壳层样式仍符合 tokens/BEM/dark-first 约束。
- [x] 相关 layout tests 通过。
- [x] `cd web && npm run lint`、`cd web && npm run test -- --run src/app/layout/Sidebar.test.tsx src/app/layout/AppShell.test.tsx`、`cd web && npm run build` 通过。
- [x] 启动 dev server，并提供本地预览 URL。
- [ ] PR CI 全绿后合并，合并后 main CI 通过，本地 main 同步干净。

## Definition of Done

- 代码和测试已提交到非 main 分支。
- Trellis task 已归档并记录 journal。
- PR 已创建，CI 全绿后合并。
- 合并后监控 main CI，并同步本地 `main`。

## Out of Scope

- 不重做 Dashboard 内部信息架构。
- 不重做资产决策页、VPS 列表页或 VPS 详情页。
- 不处理真实 VPS 数据导入。
- 不继续机械拆分大页面。
- 不处理 release/publish workflow。

## Technical Notes

- 父级规划：`docs/release/core-pages-product-ux-replan.md`。
- 代码入口：`web/src/app/metadata.ts`、`web/src/app/layout/Sidebar.tsx`、`web/src/app/layout/layout.css`。
- 测试：`web/src/app/layout/Sidebar.test.tsx`、`web/src/app/layout/AppShell.test.tsx`。
- 视觉权威：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`。
- Web specs：`.trellis/spec/web/index.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/component-conventions.md`、`.trellis/spec/web/quality-guidelines.md`。
