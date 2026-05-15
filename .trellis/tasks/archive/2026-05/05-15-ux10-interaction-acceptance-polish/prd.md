# UX-10 Interaction acceptance polish

## Goal

在 UX-8 的共享视觉减噪和 UX-9 的页面级首屏接受度之后，继续做一轮面向人类操作意愿的交互验收修正。目标不是继续扩大真实数据校验，也不是再做大面积页面重排，而是让核心页面在浏览器里更“愿意点、点得准、退得出、看得懂”：主动作更可发现，Drawer / 筛选 / 表格 / 队列的操作反馈更顺，窄屏下仍能完成关键路径。

## Requirements

- 明确排除真实 40+ VPS 数据校验、local center sample、import/dry-run、字段映射或隐私检查。
- 不重复 UX-8 的全局 CSS 降噪，也不重复 UX-9 的页面首屏大重排；本轮必须聚焦 page-specific interaction/usability friction。
- 优先检查并改进以下核心交互路径：
  - `/`：Dashboard 第一动作与支撑入口是否可点击、可理解、不会在窄屏互相挤压。
  - `/asset-decisions`：决策队列行、处理按钮、Decision Drawer、保存成功 notice、队列状态更新是否清楚。
  - `/vps`：quick views、filter chips、高级筛选 Drawer、表格行/行内操作、创建 Drawer 的操作路径是否顺畅。
  - `/vps/:id`：资产判断、facts/decision/service/domain/experience Drawers 的入口层级、关闭/保存反馈、危险区隔离是否清楚；若 mock detail 数据不完整，明确记录限制。
  - `/nodes`、`/targets`、`/events`：观测证据页的筛选/批量/队列/事件流入口是否支撑资产判断，而不是制造监控后台操作噪音。
- 改动聚焦“交互接受度”：
  - 主 CTA / 次 CTA / 危险操作是否有明显层级。
  - 表格行点击、行内按钮、链接、队列 item 不出现嵌套交互误触或键盘路径含混。
  - Drawer 打开、关闭、Esc/overlay cancel、保存成功、保存失败、draft/apply/cancel 行为清楚且符合既有合同。
  - URL-state 筛选必须有可见承接：tab/chip/drawer state 与 URL/query/API 请求一致；清空和 chip remove 不产生惰性筛选。
  - 390x900 下关键按钮、chips、drawer footer、表格入口不互相遮挡；需要横向比较的数据只在表格 surface 内横向滚动。
- 可以修改页面 JSX、page-private components、少量共享组件或 CSS，但必须保持克制：
  - 优先修正可见操作路径、文案、按钮层级、空/错/保存反馈和窄屏布局。
  - 只在现有跨页交互模式已经稳定时抽组件；不要为了减少行数做机械拆分。
  - 不新增 UI 框架、CSS-in-JS、图表库、e2e/visual regression 依赖。
- 保留现有 API、router、URL-state、Drawer focus/ESC/restore、DataTable row action、PageState 三态合同。
- UI 完成后必须给出浏览器预览证据：routes、viewports、data source、evidence level、限制；明确 mock/browser sanity 不等于真实用户接受。

## Acceptance Criteria

- [ ] Dashboard `/` 的第一动作和辅助入口在 1440x1000 与 390x900 下都可发现、可点击、文案明确，且不退化为按钮噪音。
- [ ] `/asset-decisions` 决策队列支持顺畅处理单台 VPS：行主路径、处理按钮、Drawer draft/cancel/save、保存后 notice 和队列更新都清楚。
- [ ] `/vps` quick views、active chips、高级筛选 Drawer、清空/移除筛选和表格主路径一致；URL-state 与可见状态不漂移。
- [ ] `/vps` 创建 Drawer 不抢占库存表主体；打开/关闭/保存/错误反馈在桌面和窄屏都可操作。
- [ ] `/vps/:id` 详情页的 routine actions 与危险操作分层明确；主要 Drawers 的入口、关闭、保存反馈不把用户困在表单里。
- [ ] `/nodes`、`/targets`、`/events` 的筛选/批量/事件流操作继续服务观测证据定位，不重新变成监控大屏或资源后台。
- [ ] 自绘队列和表格交互不制造嵌套 link/button 语义；DataTable 行点击合同保持，内部 action 不触发行导航。
- [ ] 390x900 下上述路由没有 page-level horizontal overflow；drawer footer、chips、按钮组、表格入口不互相遮挡。
- [ ] 没有后端/API/schema/import/real-data validation 改动。
- [ ] `npm run lint`、relevant/full Vitest、`npm run build` 通过。
- [ ] 若新增或调整稳定交互契约，更新 `.trellis/spec/`；否则在 Phase 3.3 明确说明无 spec update。

## Definition of Done

- PRD/context 完成后进入 Trellis implement/check 代理流程。
- 代码变更集中在 web frontend 和必要 spec/task/journal；`.tmp/houfeng_frontend_ux_review.md` 不提交。
- Browser/dev-server evidence 覆盖 desktop `1440x1000` 和 narrow `390x900`，至少覆盖 Dashboard、Asset Decisions、VPS inventory、VPS detail、Observability support routes；VPS detail 视 mock availability 记录限制。
- 最终按既定顺序：work commit → finish-work archive/journal → PR → CI green → merge → local main sync。

## Technical Approach

1. Use existing code and browser preview to identify the highest-friction interaction paths after UX-9, especially CTA hierarchy, drawer behavior, URL-state filters, table/queue click paths, and narrow viewport operability.
2. Make restrained page/component changes before more CSS: action copy and grouping, drawer footer/notice placement, filter-chip affordances, row/queue action targets, empty/error/save feedback, and mobile button/chip wrapping.
3. Keep contracts stable: no new routes, no API shape changes, no real data workflow, no dependency additions.
4. Validate with lint, tests, build, and browser sanity/manual preview.

## Decision (ADR-lite)

**Context:** UX-8 reduced shared visual noise and UX-9 improved page-specific first-screen hierarchy, but a product can still feel unusable if the user cannot confidently act: unclear CTA priority, brittle filter state, drawers that hide feedback, or table rows that are easy to misclick.

**Decision:** UX-10 is an interaction acceptance polish pass. It should inspect and improve real operation paths on the core pages, while remaining a human-facing UI task rather than real-data validation or backend expansion.

**Consequences:** This may touch page JSX/components and tests around existing interactions, but should avoid broad layout redesign. Any reusable interaction convention discovered must be captured in specs; otherwise the task stays focused on visible usability.

## Out of Scope

- Real-data/local-center validation, import/dry-run/privacy workflow.
- New backend endpoints, DB migrations, auth/session changes, agent changes.
- Provider/DNS sync, Web SSH, plugins, service discovery, full domain management, RBAC, currency conversion, scoring algorithms.
- New UI framework, Tailwind, CSS-in-JS, chart library, visual regression/e2e dependency.
- Mechanical file splitting or broad architecture cleanup unrelated to interaction acceptance.

## Technical Notes

- UX-8 archived PRD: `.trellis/tasks/archive/2026-05/05-15-ux8-human-ui-acceptance/prd.md`.
- UX-9 archived PRD: `.trellis/tasks/archive/2026-05/05-15-ux9-page-visual-acceptance/prd.md`.
- Active visual authority: `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`.
- Interaction contracts: `.trellis/spec/web/component-conventions.md` covers Drawer focus behavior, DataTable row action guard, self-drawn queue semantics, AppShell / Command Search contracts, and Drawer-first create/edit patterns.
- State contracts: `.trellis/spec/web/state-and-data.md` covers URL-state, applied/draft filter behavior, Asset Ledger list/decision data flow, and Dashboard facts boundaries.
- Visual evidence workflow: `docs/operations/v2-visual-evidence.md`.
- Current roadmap’s real-data recommendation is explicitly not the default for this task because UI acceptance remains the blocker.
- Security/process note: `.tmp/houfeng_frontend_ux_review.md` remains untracked and must not be committed.
