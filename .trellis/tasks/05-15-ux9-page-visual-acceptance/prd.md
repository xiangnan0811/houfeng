# UX-9 Page-specific visual acceptance pass

## Goal

在 UX-8 的共享视觉减噪之后，继续做一轮页面级、可见、可判断的视觉接受度修正。目标不是再做全局 CSS 微调，也不是推进真实数据校验，而是让核心页面在实际浏览器里看起来更像一个愿意长期使用的工程工作台：页面主任务清楚、首屏更有吸引力、关键操作更像产品路径而不是后台字段堆叠。

## Requirements

- 明确排除真实 40+ VPS 数据校验、local center sample、import/dry-run、字段映射或隐私检查。
- 不重复 UX-8 的“全局 CSS 降噪”作为主要工作；本轮必须基于具体页面 surface 做 page-specific 修正。
- 优先检查并改进以下核心路由的首屏体验：
  - `/`（Dashboard/工作台，router canonical route，不新增 `/dashboard` alias）
  - `/asset-decisions`
  - `/vps`
  - `/vps/:id`（使用现有 mock/fixture 可访问对象；若 mock detail 数据不完整，明确记录限制）
  - `/nodes`
  - `/targets`
  - `/events`
- 修改应聚焦“人类视觉接受度”：
  - 页面标题、eyebrow、lead copy 和主行动线是否清楚。
  - 关键队列/表格/证据是否是视觉主体，而不是说明、筛选或辅助入口。
  - 单页是否仍有明显长卷轴、卡片套卡片、多个同权区块、空洞管理入口或按钮噪音。
  - 资产页与观测页是否仍遵守“资产决策工作台 + 观测证据系统”的主次关系。
- 可以修改页面 JSX、已有 page-private components、共享 CSS 或少量共享组件，但必须保持克制：
  - 优先重排/删减/弱化已有 surface。
  - 只在有稳定跨页模式时抽组件；不要为了行数或假设复用做抽象。
  - 不新增 UI 框架、CSS-in-JS、图表库、e2e/visual regression 依赖。
- 保留现有 API、router、URL-state、Drawer draft/apply/cancel、DataTable row action、PageState 三态合同。
- UI 完成后必须给出浏览器预览证据：routes、viewports、data source、evidence level、限制；明确 mock/browser sanity 不等于真实用户接受。

## Acceptance Criteria

- [ ] Dashboard `/` 的首屏在 1440x1000 下有更明确的“今天先处理什么”视觉路径，避免重新退化为 KPI 墙、模块目录或多区块平铺。
- [ ] `/asset-decisions` 的工作队列更像决策队列：优先级、决策、续费/异常/缺口与行动入口更容易扫读。
- [ ] `/vps` 的库存表更像资产核对/比较工具：表格主体、quick views、质量/订阅/Node 线索主次清楚，筛选与说明不过度抢占。
- [ ] `/vps/:id` 详情页继续 evidence-first，并进一步提升“当前该做什么”的识别速度；如果只能验证 protected/fallback surface，要明确记录不是完整 detail acceptance。
- [ ] `/nodes`、`/targets`、`/events` 继续作为观测证据页存在，不抢占资产主路径；页面首屏应更有支撑关系而不是孤立监控对象列表。
- [ ] 390x900 下上述路由没有 page-level horizontal overflow；按钮、标题、badge、表格入口不互相遮挡。
- [ ] 没有后端/API/schema/import/real-data validation 改动。
- [ ] `npm run lint`、relevant/full Vitest、`npm run build` 通过。
- [ ] 若新增或调整稳定视觉/交互契约，更新 `.trellis/spec/`；否则在 Phase 3.3 明确说明无 spec update。

## Definition of Done

- PRD/context 完成后进入 Trellis implement/check 代理流程。
- 代码变更集中在 web frontend 和必要 spec/task/journal；`.tmp/houfeng_frontend_ux_review.md` 不提交。
- Browser/dev-server evidence 覆盖 desktop `1440x1000` 和 narrow `390x900`，至少覆盖 Dashboard、Asset Decisions、VPS inventory、Observability support routes；VPS detail 视 mock availability 记录限制。
- 最终按既定顺序：work commit → finish-work archive/journal → PR → CI green → merge → local main sync。

## Technical Approach

1. Use existing code and browser preview to identify the top visible page-level frictions after UX-8.
2. Make targeted page/component changes before more global CSS: adjust headers, action surfaces, section composition, row/queue emphasis, empty/normal/alert visual hierarchy.
3. Keep contracts stable: no new routes, no API shape changes, no real data workflow, no dependency additions.
4. Validate with lint, tests, build, and browser sanity/manual preview.

## Decision (ADR-lite)

**Context:** UX-8 improved shared visual rhythm, but a system can still feel ugly if individual pages retain weak product composition. The user asked to continue UI work and has explicitly rejected returning to real-data validation before UI acceptance.

**Decision:** UX-9 is a page-specific visual acceptance pass. It should inspect and improve actual core page surfaces rather than only tuning shared CSS. It remains a human-facing UI task, not a validation/import task.

**Consequences:** This may touch page JSX/components instead of only CSS, but it should produce more visible product-quality improvement. Any new reusable convention must be captured in specs; otherwise the task should stay focused and not become a refactor.

## Out of Scope

- Real-data/local-center validation, import/dry-run/privacy workflow.
- New backend endpoints, DB migrations, auth/session changes, agent changes.
- Provider/DNS sync, Web SSH, plugins, service discovery, full domain management, RBAC, currency conversion, scoring algorithms.
- New UI framework, Tailwind, CSS-in-JS, chart library, visual regression/e2e dependency.
- Mechanical file splitting or broad architecture cleanup unrelated to visual acceptance.

## Technical Notes

- UX-8 archived PRD: `.trellis/tasks/archive/2026-05/05-15-ux8-human-ui-acceptance/prd.md`.
- Active visual authority: `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`.
- Current roadmap’s real-data recommendation is explicitly not the default for this task because UI acceptance remains the blocker.
- Visual evidence workflow: `docs/operations/v2-visual-evidence.md`.
- Security/process note: `.tmp/houfeng_frontend_ux_review.md` remains untracked and must not be committed.
