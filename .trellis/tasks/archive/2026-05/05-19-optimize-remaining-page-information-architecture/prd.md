# 优化其余页面信息架构

## Goal

以最近完成的 Node Detail 与 VPS Detail 信息架构优化为参照，把候风前端其余页面从“字段/区块平铺、操作分散、信息很多但判断很少”的状态，收敛为更符合页面任务的高密度工程工具界面：每个页面默认优先回答当前最重要的问题，低价值事实和次级操作后置或收口，减少滚屏与假丰富感。

## What I already know

- 用户希望参考最近两次修改：Node Detail 偏运行态观测/控制，VPS Detail 偏资产运营判断。
- 本次范围可以包括其他页面，也可以在评审中发现 Node Detail / VPS Detail 仍有问题时纳入修改。
- 项目设计规范强调 dark-first、高密度、克制、工程师长期使用友好，不做普通 SaaS 后台或大屏监控感。
- `docs/design/v2-houfeng/component-spec.md` 已明确若干页面模板，尤其 DashboardPage 的 command-surface/workbench 模型。
- 当前页面文件覆盖 Dashboard、Nodes、Node Detail、Targets、Target Detail、VPS、VPS Detail、Asset Decisions、Subscriptions、Providers、Events、Settings、Onboarding、Compare、Login 等。

## Assumptions (temporary)

- 本任务优先是前端信息架构和 UI 结构重排，不主动改后端 API 或数据模型。
- 不追求“一次改完整个前端”，而应先按用户价值和风险优先级分批。
- 已改过的 Node Detail / VPS Detail 默认作为设计参考，只在审计发现明显问题时小范围修正。
- 保留现有业务能力、路由、表单、测试契约；避免引入 CSS 框架、图表库或新设计系统。

## Open Questions

- 已解决：用户选择 Approach A，MVP 首批范围为 `SubscriptionsPage` + `ProvidersPage`；Target Detail / Node Detail / VPS Detail 只纳入实现中发现的安全、明显、低风险修正。

## Requirements (evolving)

- 每个纳入范围的页面必须先定义“页面要帮用户做的判断/操作”，再决定默认展示内容。
- 低价值资料、重复状态、危险操作和次级编辑入口不应平铺为同权页面区块。
- 优先复用现有 atoms/shared components、BEM、design tokens 与 `pages.css`，保持 dark-first 高密度风格。
- 保留关键能力与可测试路径，不用纯视觉改造破坏现有工作流。
- 如果纳入 Node Detail / VPS Detail 回归修正，必须保持近期改造的核心原则不倒退。
- `SubscriptionsPage` 是审计发现的最高优先级：create/edit 不应以内联大面板打断订阅列表扫描路径；续费/成本证据应先以紧凑摘要呈现。
- `ProvidersPage` 是低风险 quick win：create/edit 应与其他资产列表一样进入 Drawer，默认页保留主数据表和轻量上下文。
- `TargetDetailPage` 是下一层级风险较高但价值明显的候选：ProbeItem 证据、延迟趋势、当前异常/事件的层级需要重新梳理；MVP 可只做低风险标题/顺序/入口调整。

## Acceptance Criteria (evolving)

- [x] 形成页面审计与优先级，说明哪些页面进入 MVP、哪些延期。
- [x] MVP 页面默认视图减少同权平铺区块，突出页面主任务和下一步动作。
- [x] 次级详情/操作被合理收口到菜单、Drawer、筛选面板或详情入口，而不是散落在页面下方。
- [x] 更新对应页面测试，覆盖新信息架构和保留的关键能力。
- [x] Web lint/test/build 通过；必要时做浏览器 sanity。

## Definition of Done

- PRD、research、implement/check JSONL 完成并归档。
- 前端通过 `npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- 最终完整验证优先跑 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- UI/浏览器 sanity 覆盖被改造的主页面 golden path；如果无法连真实后端，明确 fixture/mock caveat。
- 按分支/PR/release 约定完成后续流程。

## Out of Scope (explicit)

- 不重做视觉语言或引入新设计系统。
- 不改后端模型/API，除非审计证明前端无法完成核心信息架构目标。
- 不一次性重写所有页面。
- 不把详情页的 Drawer 后置模式机械套到所有列表页；每个页面按任务判断。

## Technical Approach

- 先进行页面级 IA/UI 审计，按“页面主任务、首屏信息、区块层级、操作入口、低价值信息处理、测试风险”打分。
- 用 Node Detail / VPS Detail 的原则作为局部模式库：操作收口、事实按价值分层、判断优先、详情后置、危险操作隔离确认。
- 从审计结果选一个可交付 MVP 批次，优先改最能复用模式且风险可控的页面。
- 通过 Trellis research 文件保存审计证据，PRD 只记录结论和决策。

## Research References

- [`research/overview-list-pages-audit.md`](research/overview-list-pages-audit.md) — Overview/list audit recommends `SubscriptionsPage` first, `ProvidersPage` second, with Nodes/Targets deferred due interaction risk.
- [`research/detail-special-pages-audit.md`](research/detail-special-pages-audit.md) — Detail/specialized audit flags `TargetDetailPage` as the clearest remaining detail IA gap, with small Node/VPS follow-up candidates.
- [`research/design-patterns-for-page-ia.md`](research/design-patterns-for-page-ia.md) — Extracts v2 design guidance and recent Node/VPS patterns into reusable surface-selection rules.
- [`research/browser-sanity.md`](research/browser-sanity.md) — Confirms changed Subscriptions/Providers default IA and Drawer flows with mocked browser sanity.

## Research Notes

### Priority candidates from audit

1. **SubscriptionsPage** — highest priority; inline create/edit panels interrupt renewal/cost evidence scanning and should move to Drawer while preserving URL-driven create.
2. **ProvidersPage** — quick low-risk modernization; inline master-data forms should move to Drawer, with only lightweight context on the default page.
3. **TargetDetailPage** — highest detail-page gap; valuable but riskier because runtime/probe/history/metadata/lifecycle state is intertwined.
4. **NodeDetailPage / VPSDetailPage** — already mostly aligned; only surgical follow-ups should be considered, such as Node binding-conflict order or VPS evidence dedupe.

### Feasible MVP approaches

**Approach A: Asset Ledger form/workbench cleanup** (Recommended)

- Scope: `SubscriptionsPage` + `ProvidersPage`, plus only tiny Node/VPS/Target corrections if implementation finds safe obvious fixes.
- Pros: Addresses clearest remaining old-CRUD IA, directly supports VPS renewal/cost workflows, lower risk than observability control pages.
- Cons: Does not yet solve Target Detail’s deeper probe-evidence hierarchy.

**Approach B: Observability detail cleanup**

- Scope: `TargetDetailPage` first, with surgical Node Detail ordering/follow-up fixes.
- Pros: Best continuation of the Node Detail watchtower IA, improves a core observability route.
- Cons: Higher risk because Target detail mixes runtime controls, ProbeItem CRUD, metadata, lifecycle, history drawers, and tests.

**Approach C: Thin cross-page polish pass**

- Scope: shallow alignment across many pages: labels, headings, action placement, obvious duplicate panels.
- Pros: More pages look somewhat more consistent quickly.
- Cons: Risks becoming superficial and failing the user’s main critique: pages need design logic, not cosmetic consistency.

## Decision (ADR-lite)

**Context**: 页面审计显示剩余最大 IA 缺口集中在旧式资产台账 CRUD 页面；`TargetDetailPage` 价值明显但交互风险更高，已完成的 Node Detail / VPS Detail 只适合做外科式修正。

**Decision**: 用户选择 Approach A。本 MVP 聚焦 Asset Ledger form/workbench cleanup：先改造 `SubscriptionsPage` 与 `ProvidersPage`，把 create/edit 从默认列表扫描路径中移入 Drawer，并补充紧凑的续费/成本或主数据上下文摘要。`TargetDetailPage`、`NodeDetailPage`、`VPSDetailPage` 仅在实现中发现安全、明显、低风险问题时小范围修正。

**Consequences**: 本批次优先解决影响资产运营链路的旧 CRUD 平铺问题，风险低于直接重排观测详情页；Target Detail 的 ProbeItem/事件/趋势层级问题延期到单独任务处理。

## Post-merge Notes

- PR #124 was squash-merged with the GitHub-generated title `Optimize asset ledger page information architecture (#124)`, so Release Please could not parse the merged commit as the original `feat:` commit. A follow-up conventional commit records this release-trigger correction without changing product behavior.

## Technical Notes

- Likely page families: Dashboard / Nodes / Targets / Target Detail / Asset Decisions / Subscriptions / Providers / Events / Settings / onboarding / compare / login, plus Node Detail and VPS Detail review.
- Relevant specs: `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`, `.trellis/spec/web/component-conventions.md`, `.trellis/spec/web/styling-guidelines.md`, `.trellis/spec/web/state-and-data.md`, `.trellis/spec/web/quality-guidelines.md`.
- Current task: `.trellis/tasks/05-19-optimize-remaining-page-information-architecture`.
