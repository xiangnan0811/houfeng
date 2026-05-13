# UX-7C page state consistency

## Goal

把候风前端高影响页面的 loading / error / empty 状态从分散手写收敛为共享、可测试、符合 v2 设计语言的状态 primitive。目标不是重做页面信息架构，而是在真实数据验证前消除“页面可用但状态面粗糙”的体验断点。

## What I Already Know

- UX-7A 已完成 evidence 组件抽取和路由 lazy loading；UX-7B 已完成视觉证据治理。
- `docs/design/v2-houfeng/design-language.md` 第 7 节已经定义 loading / error / empty 三态：
  - Loading 使用 surface 底色、一行 mono 文案和时间戳，不使用 spinner / skeleton。
  - Error 使用 warning / critical 语义 surface，提供中文说明、截断的技术错误摘要和 retry action（可重试时）。
  - Empty 复用 `.empty-state`，升级为小型单色装饰、解释文案和必要 CTA。
- 当前 `DashboardPage`、`NodesPage`、`TargetsPage`、`EventsPage`、`SettingsPage`、Node/Target/VPS 详情页 loading/error 均各自手写 `page-panel` 或裸文本。
- Nodes / Targets / Events 的列表空态已有业务文案，但与 v2 空态锚点不完全一致：
  - 无 Node 应表达“候风尚未接入任何节点”并提供“新建第一个节点”。
  - 无 Target 应表达“候风尚未配置任何观测目标”并提供“新建第一个目标”。
  - Events 空查询应表达“没有匹配的事件”并提供“重置筛选”。
  - Probe 列表空应表达“目标尚未配置 ProbeItem”并提供“添加 Probe”。
- `RouteModuleFallback` 已经是一个稳定的路由模块加载 surface，可以复用新状态 primitive 的视觉语言，但本轮不需要改变路由注册方式。

## Requirements

1. 新增共享页面状态组件，落点为 `web/src/components/`，遵守命名导出、纯展示、route-agnostic 和 ReactNode 插槽约定。
2. 新组件至少覆盖三类状态：
   - `loading`
   - `error`
   - `empty`
3. 新组件必须支持：
   - eyebrow/title/description；
   - 可选 action slot；
   - 可选技术摘要；
   - 可选 compact 模式，用于嵌入 page panel 或列表 surface；
   - 可访问的 `aria-live` / `role` 行为，不让错误只靠颜色表达。
4. 新样式必须落在 `web/src/styles/pages.css`，使用 BEM 和现有 CSS tokens，不新增 CSS 框架、不新建单组件 CSS 文件、不写业务 inline style。
5. 替换高影响 route/page 状态：
   - Dashboard loading/error；
   - Nodes loading/error；
   - Targets loading/error；
   - Events loading/error；
   - Settings loading/error；
   - Node detail loading/error；
   - Target detail loading/error；
   - VPS detail loading/error。
6. 替换高影响 empty 状态：
   - Nodes 无节点和筛选无结果；
   - Targets 无目标和筛选无结果；
   - Events 空时间线与筛选空结果；
   - Target Probe 列表无 ProbeItem。
7. 保持现有业务行为不变：
   - 不改变 API client、URL 参数、filter state、drawer 行为或路由结构；
   - 不把 create / reset 动作写死进共享组件，由 page 传入 action slot；
   - 不改变 Dashboard / Nodes / Targets / Events 的证据 lead/focus 业务逻辑。
8. 更新 UI 演进路线，把 UX-7B 从“当前切片”更新为已完成，并记录 UX-7C 的目的和下一步建议。

## Acceptance Criteria

- [ ] 新增 `PageState` 组件有同名测试，覆盖 loading/error/empty、action slot、technical summary 和 compact class。
- [ ] Dashboard / Nodes / Targets / Events / Settings 的 loading/error 状态使用共享状态组件，现有 page tests 继续覆盖用户可见文案。
- [ ] Node / Target / VPS 详情页 loading/error 状态使用共享状态组件，返回动作仍然可用。
- [ ] Nodes / Targets / Events / Probe list 空态文案对齐 v2 设计语言，并保留原有 create/reset/add 行为。
- [ ] `npm --prefix web run lint` 通过。
- [ ] `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run` 通过。
- [ ] `npm --prefix web run build` 通过。
- [ ] `make validate-visual-evidence` 通过。
- [ ] 本地浏览器 sanity 或等效可见状态验证结果写入 PR / final report；若无法覆盖状态页截图，需要明确说明测试证据和限制。

## Definition of Done

- 工作提交在非 main 分支，走 PR。
- PR CI 全绿后合并。
- 合并后同步本地 `main`。
- Trellis 任务归档并记录 journal。

## Out of Scope

- 不引入 Playwright/Cypress/视觉回归依赖。
- 不改 API 数据结构、后端接口或真实数据 import 流程。
- 不重排 Dashboard command surface 或资产/观测页面信息架构。
- 不在本轮偿还所有历史 inline style、所有小组件空态或所有 drawer 内部状态。
- 不拆分 `NodeDetailPage.tsx` / `TargetDetailPage.tsx` 这类大文件。

## Technical Notes

- 相关规范：
  - `.trellis/spec/web/component-conventions.md`
  - `.trellis/spec/web/styling-guidelines.md`
  - `.trellis/spec/web/quality-guidelines.md`
  - `.trellis/spec/guides/code-reuse-thinking-guide.md`
  - `.trellis/spec/guides/branch-workflow-governance.md`
- 视觉权威：
  - `docs/design/v2-houfeng/design-language.md`
  - `docs/design/v2-houfeng/component-spec.md`
- 本地审查记录：
  - `research/page-state-audit.md`
