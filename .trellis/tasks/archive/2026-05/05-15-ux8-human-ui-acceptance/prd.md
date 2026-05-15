# UX-8 Human UI acceptance hardening

## Goal

把 UX-1 到 UX-7E 已完成的功能性 UI 改造继续推进到“人愿意长期打开并操作”的水准。当前阻塞点不是缺真实数据校验能力，而是用户仍认为页面丑、不可用、没有操作欲望；因此本任务优先修正核心页面的视觉质量、首屏可扫性和操作意愿，再谈 real-data validation。

## Requirements

- 明确拒绝把本轮定义为真实 40+ VPS 数据校验、local center sample 或 import/dry-run 工作。
- 围绕核心路径做页面级视觉接受度硬化：`/dashboard`、`/asset-decisions`、`/vps`、`/vps/:id`、`/nodes`、`/targets`、`/events`。
- 优先处理“人看起来丑/乱/不想用”的体验问题，而不是新增业务能力：
  - 页面首屏必须有明确主任务，而不是多个同权模块堆叠。
  - 说明文案、筛选器、辅助控件不得抢占表格、队列、证据和下一步动作的视觉主体。
  - 深色主题应更接近 v2 设计语言的“冷静、克制、高密度、工程师长期使用友好”，避免浅色卡片感、过重边框和装饰噪音。
  - 核心数字、时间、ID、状态、风险信号必须更容易扫读。
  - 移动窄屏仍不追求完整移动产品，但不能出现页面级横向溢出、按钮互相遮挡、标题和表格入口不可读。
- 对核心页面做一致的视觉减噪：降低无意义 panel/card chrome，统一 section rhythm、header/action 密度、表格周边留白和状态面板权重。
- 保留已经完成的产品结构：资产决策工作台 + 观测证据系统。Node/Target/Event 继续作为资产判断证据，不重新变成监控大屏中心。
- 保留现有 API、路由语义、URL-state、Drawer draft/apply/cancel 合同、PageState 三态合同和 visual evidence tooling。
- UI 变更必须通过本地浏览器/预览做人工可见核对；mock/browser evidence 只能证明渲染和可扫性，不能替代真实人类接受度。

## Acceptance Criteria

- [ ] `/dashboard` 在 1440x1000 首屏能更像日常 command desk：最重要的处理线索、风险摘要和下一步动作有明显主次，不能像 KPI 墙或模块目录。
- [ ] `/asset-decisions` 和 `/vps` 在 1440x1000 下继续保持高密度扫描，但视觉上更愿意读：队列/表格是主体，筛选和说明退到工具层。
- [ ] `/vps/:id` 保持 evidence-first 决策页结构，并进一步减少长卷轴、卡片堆叠和视觉噪音，使“当前该做什么”更快被识别。
- [ ] `/nodes`、`/targets`、`/events` 保持观测证据定位，页面首屏不与资产主线争抢产品中心。
- [ ] 390x900 下核心路由没有 page-level horizontal overflow；需要横向比较的数据只在对应 table surface 内滚动。
- [ ] 深色主题下主背景、panel、表格、badge、focus、hover、状态色符合 v2 dark-first 气质；light 主题不出现明显不可读或破版。
- [ ] 没有引入 Tailwind、CSS-in-JS、UI 框架、图表库、repo 级 Playwright/Cypress/WebDriverIO 依赖。
- [ ] `npm run lint`、相关 Vitest、`npm run build` 通过。
- [ ] 启动 dev server 并用浏览器核对核心路由；最终报告包含 preview URL、routes checked、viewports checked、data source、证据级别和限制。

## Definition of Done

- Tests added or updated where visual hierarchy affects user-visible copy, interaction boundaries, route state, or component contracts.
- Lint, relevant Vitest, and web build pass locally.
- Browser sanity / manual preview evidence recorded in the final report with explicit limitation：这不是用户真实接受度，只是开发者侧可见核对。
- No backend API, database schema, import flow, or real-data validation work is included.
- If durable reusable conventions are created or changed, update `.trellis/spec/` or explicitly record why no spec update is needed during Phase 3.3.

## Technical Approach

1. Audit the current core pages from code and browser preview for page-level visual friction: chrome weight, section order, table density, mobile overflow, and action discoverability.
2. Make restrained CSS/component/page adjustments that improve perceived quality without changing data contracts:
   - shared page rhythm and panel chrome;
   - Dashboard command surface visual hierarchy;
   - asset queue/table emphasis;
   - VPS Detail evidence/workbench clarity;
   - observability support page visual secondary status.
3. Prefer editing existing shared CSS and existing page sections over introducing new abstractions. Only extract a reusable component if the same stable pattern appears across multiple pages and reduces future UI work.
4. Validate with tests/build and a running dev server across desktop and narrow viewports.

## Decision (ADR-lite)

**Context:** Roadmap documents currently mention local center sample and real-data validation as possible next steps, but the user explicitly rejected that direction while the UI is still perceived as ugly and unusable.

**Decision:** UX-8 is a human UI acceptance hardening batch. It treats visual quality, willingness-to-use, and page-level clarity as the blocker. It explicitly excludes real-data validation and uses mock/local preview only as rendering evidence.

**Consequences:** This may delay real 40+ VPS data validation, but it prevents “validation” from degrading into database checking. The success signal for this task is improved human-facing usability and visual acceptance, not expanded backend coverage or import readiness.

## Out of Scope

- Real 40+ VPS data import, dry-run, field mapping, or privacy review.
- Local center sample validation as the main work item.
- Backend API changes, database migrations, auth/session changes, agent protocol changes.
- Provider/DNS sync, Web SSH, plugins, service discovery, full domain management, RBAC, currency conversion, scoring algorithms.
- Replacing the design system with external UI kits or visual regression tooling.
- Mechanical file splitting or broad refactors whose only benefit is line-count reduction.

## Technical Notes

- Parent planning: `docs/release/core-pages-product-ux-replan.md` says the current blocker is page UX/visual quality, not backend functionality.
- Current route roadmap: `docs/release/ui-evolution-roadmap.md` lists UX-7A through UX-7E as completed, but its real-data next-step recommendation is superseded for this session by the user’s explicit UI acceptance correction.
- Visual authority: `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`.
- Visual evidence workflow: `docs/operations/v2-visual-evidence.md`; use preview/browser evidence, but do not treat mock evidence as human acceptance.
- Security/process note: `.tmp/houfeng_frontend_ux_review.md` must not be committed; it remains an external review reference only.
