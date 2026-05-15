# UX-11 UI design expert refinement

## Goal

在 UX-8 共享视觉降噪、UX-9 页面级首屏接受度、UX-10 操作交互打磨之后，继续做一轮更接近前端/UI 专家审查口径的“微观产品质感”修正。目标不是再做大布局重排，也不是进入真实数据校验，而是让核心页面在细节上更像一套长期使用的工程工作台：状态表达更有温度、空/错/成功反馈更像产品路径、hover/focus/行内 affordance 更精确、文案与密度更克制，整体更符合 v2 候风“东方观象台气质的工程工具”。

## Design Direction

**Refined industrial observatory / 精密观象台**：保留 dark-first、高密度、工程师长期使用友好，不做大屏监控霓虹、普通 SaaS 白盒、廉价中国风或花哨动画。视觉记忆点应来自精密仪器感：细 hairline、低噪声状态色、清楚的 action hierarchy、可靠的空/错/成功状态、可长时间凝视的数据表面。

## Requirements

- 明确排除真实 40+ VPS 数据校验、local center sample、import/dry-run、字段映射、隐私检查或 backend/API/schema 改动。
- 不重复 UX-8 的全局 CSS 降噪、不重复 UX-9 的首屏 page composition、不重复 UX-10 的 Drawer/交互路径修正；本轮聚焦 expert visual/product refinement。
- 优先审查并改进以下细节维度：
  - **状态表达**：loading/error/empty/success notice 是否像产品状态，而不是裸文本或后台提示；是否有清楚的下一步动作。
  - **微观层级**：主文案、secondary copy、metadata、badge、timestamp、technical id 是否有清楚阅读顺序；数字/ID/时间是否保持 mono/tabular 语义。
  - **Affordance**：hover/focus/active surface 是否让可点击目标可见但不过度发光；ghost/secondary/danger 是否分层稳定。
  - **密度与留白**：表格/队列/Drawer/section 的 padding、gap、分隔线是否在桌面和窄屏保持“紧凑但不挤”。
  - **文案质量**：中文 UI 文案是否回答用户“这是什么、我现在能做什么、这条状态是否可信”；避免空洞管理入口、技术字段堆叠和泛化后台词。
  - **窄屏细节**：390x900 下状态块、按钮组、filter chips、drawer footer、空/错态 CTA 是否换行可读，无 page-level overflow。
- 核心路由覆盖：
  - `/`：Command Surface 的状态说明、summary metadata、主动作 hover/focus 与空/正常/异常文案。
  - `/asset-decisions`：工作队列的状态/notice/empty/error/copy、队列行 affordance、续费 evidence 次级权重。
  - `/vps`：库存表周边状态、quick views/chips 文案、empty/error/create success feedback、资料质量提示的视觉噪声。
  - `/vps/:id`：detail workbench 的状态表达、routine action metadata、服务/域名/经验空态与保存反馈、危险区隔离文案。
  - `/nodes`、`/targets`、`/events`：观测证据页的 empty/error/filter summary、事件流/证据 surfaces 的可读性，不抢资产主路径。
  - `/providers`、`/subscriptions`、`/nodes/compare`：作为专家审查发现的边缘高价值 surface，优先修正 inline panel / plain state / identity composition 这类低成本质感问题，但不得让本轮扩成完整资产管理重构。
- 专家审查候选点（见 `research/expert-ui-audit.md`）应作为实现优先级输入，不是强制全量 checklist：
  - Provider/Subscription inline create/edit surface 的压缩或 Drawer/sidecar 化。
  - Node compare missing/loading/error state 与 A-vs-B identity composition。
  - Events filter/stream 的 time-range/limit context copy。
  - VPS/Asset Decisions 空态 next action。
  - Nodes/Targets/Events support-surface 用户结果导向文案。
  - Asset Decisions custom queue keyboard/focus affordance。
  - 长 create drawer 的 essentials/optional grouping 与 390px footer comfort。
- 可以修改页面 JSX、page-private components、少量共享组件或 `pages.css` / `atoms.css`，但必须保持克制：
  - 优先改可见微细节、状态 surface、copy、focus/hover、spacing/rhythm。
  - 不引入新 UI 框架、CSS-in-JS、图表库、动画库、e2e/visual regression 依赖。
  - 不做机械拆文件或大规模组件抽象；只有稳定跨页模式才提炼。
- 保留现有 API、router、URL-state、Drawer focus/ESC/restore、Drawer cancel state cleanup、DataTable row action、PageState 三态合同。
- UI 完成后必须给出浏览器预览证据：routes、viewports、data source、evidence level、限制；明确 mock/browser sanity 不等于真实用户接受。

## Acceptance Criteria

- [ ] 核心页面的 loading/error/empty/success 状态不再出现明显裸文本、巨大空白或无下一步动作的后台提示；相关 surface 使用现有 PageState/notice/empty-state 体系或等价 v2 样式。
- [ ] Dashboard `/` 的状态文案、summary metadata、主动作 hover/focus 与正常/异常/维护/首次接入状态保持克制、可信、可扫读。
- [ ] `/asset-decisions` 和 `/vps` 的队列/库存表周边状态、空态、保存反馈和资料质量提示更像人工工作台提示，不像字段堆叠或错误日志。
- [ ] `/vps/:id` 的 workbench、服务/域名/经验区块和危险区在 copy、空态、成功/错误反馈、focus/hover 上更清楚；危险操作不被日常动作视觉稀释。
- [ ] `/nodes`、`/targets`、`/events` 的观测证据 surfaces 在空/错/筛选 summary/事件流微观层级上更支撑资产判断，不变成监控大屏或 CRUD 后台。
- [ ] 专家审查候选点至少落地 3 类可见改进（例如 Provider/Subscription surface、Node compare state、Events context、空态 next action、support-surface copy、custom queue affordance、long drawer grouping），且每类都有 route/file 级说明。
- [ ] 390x900 下核心路由没有 page-level horizontal overflow；状态块、button groups、chips、empty/error CTA、drawer footer 不遮挡或不可读。
- [ ] 所有新增/修改样式继续使用 tokens + BEM + pure CSS；无硬编码颜色、无 page-local CSS 文件、无 CSS-in-JS。
- [ ] 没有后端/API/schema/import/real-data validation 改动。
- [ ] `npm run lint`、relevant/full Vitest、`npm run build` 通过。
- [ ] 若新增或调整稳定视觉/状态/交互契约，更新 `.trellis/spec/`；否则在 Phase 3.3 明确说明无 spec update。

## Definition of Done

- PRD/context 完成后进入 Trellis implement/check 代理流程。
- 代码变更集中在 web frontend 和必要 spec/task/journal；`.tmp/houfeng_frontend_ux_review.md` 不提交。
- Browser/dev-server evidence 覆盖 desktop `1440x1000` 和 narrow `390x900`，至少覆盖 Dashboard、Asset Decisions、VPS inventory、VPS detail、Observability support routes；若修改 Provider/Subscription/Node compare，也纳入对应路由 evidence；若本机 Playwright 缺失，明确记录 evidence blocker，不新增 browser automation 依赖。
- 最终按既定顺序：work commit → finish-work archive/journal → PR → CI green → merge → local main sync。

## Technical Approach

1. Use existing code, v2 design docs, expert/frontend-design guidance, and `research/expert-ui-audit.md` to audit page micro-quality after UX-10: state surfaces, copy, hover/focus, metadata hierarchy, empty/error/success affordances, narrow viewport wrapping.
2. Pick the highest-value subset rather than touching everything. Recommended default: land at least 3 expert-audit categories that are low-risk and visible, such as PageState/empty actions, Events context copy, support-surface copy, Node compare state, and focused Provider/Subscription surface polish.
3. Make restrained page/component/style changes, preferring existing PageState, Drawer, DataTable, Button, Badge, Mono/Timestamp, DetailSection and page-private structures.
4. Keep changes visible and product-facing: state/copy/affordance refinements should be testable by page tests or browser sanity, not hidden refactors.
5. Validate with lint, relevant tests, full tests/build, and browser sanity/manual evidence when local tooling allows.

## Decision (ADR-lite)

**Context:** UX-8/UX-9/UX-10 moved the UI from noisy and hard to operate toward clearer page hierarchy and interaction paths. The remaining quality gap is likely in small expert-review details: state language, copy, focus/hover affordance, empty/error/success surfaces, and micro-density.

**Decision:** UX-11 is a restrained expert UI refinement pass. It uses frontend/UI expert sensibility, but remains inside the established v2 Houfeng design language and does not introduce a new visual system or external dependencies.

**Consequences:** This round may touch many page-local details, but should avoid architectural churn. If a durable cross-page state/empty/error/success convention emerges, capture it in `.trellis/spec/`; otherwise keep the implementation concrete and page-specific.

## Out of Scope

- Real-data/local-center validation, import/dry-run/privacy workflow.
- New backend endpoints, DB migrations, auth/session changes, agent changes.
- New routes or route aliases, especially no `/dashboard` alias.
- Provider/DNS sync, Web SSH, plugins, service discovery, full domain management, RBAC, currency conversion, scoring algorithms.
- New UI framework, Tailwind, CSS-in-JS, chart library, animation library, visual regression/e2e dependency.
- Broad page layout redesign, full information architecture rewrite, or mechanical file splitting unrelated to expert UI refinement.

## Technical Notes

- UX-8 archived PRD: `.trellis/tasks/archive/2026-05/05-15-ux8-human-ui-acceptance/prd.md`.
- UX-9 archived PRD: `.trellis/tasks/archive/2026-05/05-15-ux9-page-visual-acceptance/prd.md`.
- UX-10 archived PRD: `.trellis/tasks/archive/2026-05/05-15-ux10-interaction-acceptance-polish/prd.md`.
- Active visual authority: `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`.
- Active component/state contracts: `.trellis/spec/web/component-conventions.md`, `.trellis/spec/web/styling-guidelines.md`, `.trellis/spec/web/state-and-data.md`, `.trellis/spec/web/quality-guidelines.md`.
- Design skill guidance applied as a constraint, not a new aesthetic system: choose a clear refined industrial/observatory direction, execute with precision, avoid generic SaaS/AI aesthetics, and match implementation complexity to restraint.
- Expert audit artifact: `research/expert-ui-audit.md`.
- Visual evidence workflow: `docs/operations/v2-visual-evidence.md`.
- Current roadmap’s real-data recommendation remains explicitly not the default for this task because UI acceptance is still the active blocker.
- Security/process note: `.tmp/houfeng_frontend_ux_review.md` remains untracked and must not be committed.
