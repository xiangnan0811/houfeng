# UX-3 Dashboard command surface polish

## Goal

把 Dashboard 从“可用的三列队列集合”继续打磨成日常打开的工程 command desk。首屏需要在 5 秒内回答“今天先处理什么”，并在资产压力、严重异常、维护观察、正常运行和首次接入之间保持清晰的视觉节奏。

## What I already know

* 用户已确认进入 UX-3，并要求继续走 Trellis 流程完成实现、PR、CI、合并与本地同步。
* 本任务必须在非 `main` 分支开发；当前分支是 `ux3-dashboard-command-surface-polish`。
* 不使用 subagent；实现、检查和收尾都在主会话完成。
* UX-2 已完成页面主体响应式层级基线，Dashboard 只需在该基线上做首屏 polish，不需要重做所有页面。
* 当前 Dashboard 已有 `DashboardCommandSurface`、`DashboardWorkbench`、`AttentionQueue`、`DashboardContextStrip`、`ManagementEntries`、`RunningOverview` 等拆分。
* 当前 Dashboard contract 是 asset-decision-first command surface，不允许恢复 KPI wall、API facts warehouse、Group 摘要或最近事件摘要列表。
* 视觉权威是 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`。
* 项目没有 lucide/react-icons 等 icon 库；Dashboard 现有视觉语言使用 `StatusGlyph`、`Badge`、`MonoDigits`、`Hostname`、`Timestamp`。

## Requirements

* 保留 Dashboard 三条 lane 的信息架构：资产决策队列、观测异常队列、下一步动作。
* 强化首屏视觉判断，让资产压力、严重异常、维护态、正常态、首次接入态有更明确的节奏。
* 当资产压力和严重异常同时存在时，资产决策入口仍然是第一动作，同时严重异常必须在首屏获得足够视觉权重。
* 主按钮等于第一条动作；刷新、自动刷新是工具控件，不应抢业务动作权重。
* 资产 summary 仍只展示聚合入口，不展开 VPS / subscription 明细。
* `snapshot_generated_at` 只能表达 dashboard response 生成时间，不得伪装成 Center health、agent sync 或实时性证明。
* 正常态不展示大型空队列；维护态不伪装成紧急异常；首次接入态不展示 API facts warehouse。
* Dashboard 深链继续承接到 Nodes / Targets / Events / VPS / Asset Decisions。
* 不新增依赖、不新增 CSS 框架、不引入可视化回归依赖。
* 对用户可见 UI 改动必须记录 v2 visual evidence：preview URL、routes、viewports、数据源、截图或 browser sanity 说明。

## Acceptance Criteria

* [ ] `/` 在 1440x1000 下首屏能明确区分主结论、业务动作、资产 lane、观测 lane 和工具控件。
* [ ] `/` 在 390x900 下没有 page-level horizontal overflow，文本不重叠、不逃出按钮、badge、lane 或 action row。
* [ ] 有资产压力时，资产决策入口比普通管理入口更突出。
* [ ] 有严重异常时，严重异常在 command surface 中可见且不会被资产压力淹没。
* [ ] 资产压力 + 严重异常同时存在时，第一 CTA 进入资产决策，严重事件动作和异常对象仍可见。
* [ ] 正常态不显示大型空队列表格；维护态展示为观察窗口；首次接入态仍突出 onboarding。
* [ ] 旧反模式继续不出现：`已加载 /api/dashboard`、`首页数据可信度`、`系统全局指标`、`Dashboard 摘要指标`、`系统快捷入口`、`Group 摘要`、`最近事件摘要`。
* [ ] `DashboardPage.test.tsx` 覆盖新增或调整后的关键文案、链接和状态优先级。
* [ ] `npm run lint`、`TMPDIR=$PWD/.tmp npm run test -- --run`、`npm run build` 通过。
* [ ] PR 说明包含 Visual / UX Evidence。

## Out of Scope

* 不改 `/api/dashboard` 或任何后端 contract。
* 不改真实数据导入流程。
* 不重排 VPS inventory、Asset Decisions、VPS detail 或观测页。
* 不引入图表库、状态管理库、icon 库、Tailwind、CSS Modules、Playwright/Cypress。
* 不做 repo 级截图 diff 或 CI visual regression。

## Technical Notes

* 主要代码范围：
  * `web/src/pages/dashboard/DashboardCommandSurface.tsx`
  * `web/src/pages/dashboard/DashboardWorkbench.tsx`
  * `web/src/pages/dashboard/RunningOverview.tsx`
  * `web/src/styles/pages.css`
  * `web/src/pages/DashboardPage.test.tsx`
  * `docs/release/ui-evolution-roadmap.md`
  * `docs/operations/v2-visual-evidence/manifest.md`
* Dashboard contract 约束见 `.trellis/spec/web/state-and-data.md` 的 Dashboard 数据可信度章节。
* 组件与样式约束见 `.trellis/spec/web/component-conventions.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/quality-guidelines.md`。
* Browser evidence 使用 mocked API，不引入仓库依赖。

## Definition of Done

* UX-3 代码和文档在非 main 分支提交。
* Trellis task 已归档并记录 journal。
* PR 创建后等待 CI 全绿。
* PR 合并后同步本地 `main`，删除/清理特性分支。
