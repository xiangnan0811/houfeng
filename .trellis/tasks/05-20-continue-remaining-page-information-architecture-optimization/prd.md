# 继续推进剩余页面信息架构优化

## Goal

在已完成 Node Detail、VPS Detail、Subscriptions、Providers、Target Detail、TargetsPage + NodesPage 列表控制带联合梳理后，继续推进候风前端剩余页面的信息架构优化。下一批需要在“继续推进速度”和“避免安全/高测试风险页面失控”之间取得平衡，优先选择仍有清晰 IA 缺口、实现和测试风险可控、能复用既有模式的页面。

## What I already know

- 用户在 v0.8.0 发布后要求继续推进页面 IA 优化。
- 已完成参考：Node Detail、VPS Detail、Subscriptions、Providers、Target Detail、TargetsPage + NodesPage list controls。
- 新审计显示剩余页面中 `NodeComparePage` 是最清晰且可控的 IA 缺口；`SettingsPage` 有已知 inline-style / 结构债务但涉及通知密钥和全局保存语义；`EventsPage`、`DashboardPage`、`AssetDecisionsPage`、`VPSPage` 已有较强模式或更适合具体问题驱动；`NodeOnboardingPage` 安全敏感，仅作 safety-only 候选。
- 项目约束仍是 dark-first、高密度、中文主界面，不引入新设计系统/图表库/CSS 框架，不主动改后端 API 或数据模型。

## Assumptions (temporary)

- 本任务继续聚焦前端 IA/UI 结构调整。
- 不反复重改刚完成的 Targets/Nodes、Target Detail、资产台账和详情页，除非发现阻塞一致性的问题。
- NodeOnboardingPage 不进入本批，除非用户明确要求安全保持型专项。

## Open Questions

- 已解决：用户选择 A，本批做 `NodeComparePage` 专项；不纳入 Settings、Events、Dashboard、AssetDecisions、VPSPage 或 NodeOnboardingPage。

## Requirements (evolving)

- 每个纳入范围的页面必须先定义页面主任务，再重排默认信息层级和操作入口。
- 默认视图应优先回答页面最重要的问题，减少同权平铺、重复状态和滚屏成本。
- 不引入新依赖，不改后端模型/API，优先复用现有 atoms/shared components、BEM、design tokens 与 `pages.css`。
- 保留现有路由、URL 状态、表单、确认流程、密钥遮蔽/保存语义和测试契约。
- `NodeComparePage` 本批只增强默认比较判断路径：compare command/header panel、24h runtime facts 窗口说明、A/B 摘要带、空/错态辅助说明。
- 保留 `/nodes/compare?id=...&id=...` query contract、`getNode` + `getNodeRuntimeFacts` 请求形状、`window=24h` 行为、直接链接和 `PageState` 空/错态语义。
- 保留 `NodeWatchtowerMetrics` 作为详细指标区，不拆分或重写共享指标图表内部。

## Candidate Approaches

### A. `NodeComparePage` 专项（推荐默认）

- 主任务：快速判断两个 Node 的差异是否值得进一步排查。
- 做法：增强 compare command/header panel，明确 24h runtime facts 对比窗口；增加紧凑 A/B 摘要带，突出 health、lifecycle、binding/monitoring、region/city/provider、sample availability；保留 `NodeWatchtowerMetrics` 作为详细指标区。
- 优点：剩余页面里 IA 缺口最清晰，代码和测试面小，不触碰后端/API 或共享指标内部。
- 代价：用户价值较窄，只解决比较页。

### B. `NodeComparePage` + `SettingsPage` limited cleanup

- 主任务：补齐比较页 IA，同时清理设置页已知样式/结构债务。
- 做法：NodeCompare 同 A；Settings 只替换 business inline styles 到 BEM/pages.css，并强化现有 tab/section 概览文案，不重写保存模型或通知 channel modal。
- 优点：推进更快，同时处理一个真实 operator 页面债务。
- 代价：Settings 涉及 Telegram/Feishu token masking、全局保存 payload、runtime delivery toggles，风险高于 A。

### C. Events/Dashboard/AssetDecisions/VPSPage 稳定化小批量

- 主任务：对已经较强的页面做一致性小修。
- 做法：只做具体可测试的 copy/层级微调，不做大重构。
- 优点：覆盖面广。
- 代价：缺少明确高价值缺口，Events/Dashboard 测试密集，容易产生低收益 churn。

## Acceptance Criteria (evolving)

- [x] 形成下一批页面审计与优先级，说明纳入范围与延期范围。
- [x] `NodeComparePage` 默认视图减少弱层级区域，突出“两个 Node 是否值得进一步排查”的主判断。
- [x] 增加 A/B 摘要带和比较上下文说明，不改详细指标图表内部。
- [x] 保留现有 query contract、API 请求形状、`window=24h`、错误/空态和关键测试契约。
- [x] 对应页面测试更新，覆盖新 IA 和保留关键工作流。
- [x] Web lint/test/build 通过，必要时跑完整 verify 和浏览器 sanity。

## Verification Evidence

- `trellis-implement` reported focused `NodeComparePage` test, web lint, full web tests, web build, and full repository verify passed.
- `trellis-check` independently reviewed the NodeCompare-only scope and reran focused `NodeComparePage` test, web lint, full web tests, and web build with no fixes required.
- Main session reran `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`; Go tests, web lint, 63 web test files / 488 tests, and web build passed.
- Browser sanity passed for `/nodes/compare?id=nd_a&id=nd_b`; evidence is recorded in [`research/browser-sanity.md`](research/browser-sanity.md). Caveat: sanity used a local mock API rather than a real authenticated center/PostgreSQL session.
- Local verify caveat: npm emitted existing `EBADENGINE` warning because this machine uses Node `v24.14.1` while `web/package.json` requires Node `22.x`, plus one moderate npm audit notice.

## Definition of Done

- PRD、research、implement/check JSONL 完成并归档。
- 前端通过 `npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- 最终完整验证优先跑 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- UI/浏览器 sanity 覆盖被改造的主页面 golden path；如果无法连真实后端，明确 fixture/mock caveat。
- 按分支/PR/release 约定完成后续流程。

## Out of Scope (explicit)

- 不一次性重写全部剩余页面。
- 不重做视觉语言或引入新设计系统。
- 不改后端模型/API。
- 不重改已完成 Node/VPS/Subscriptions/Providers/Target Detail/Targets/Nodes list controls。
- 不触碰 NodeOnboardingPage 的 token reveal/copy、center-generated install command、安全确认等敏感契约，除非用户另开专项。
- 不纳入 SettingsPage；不改 Telegram/Feishu secret 持久化语义、masked token summary、unchanged bot_token omit payload、runtime delivery toggle 语义。
- 不纳入 EventsPage、DashboardPage、AssetDecisionsPage、VPSPage。
- 不改 `NodeWatchtowerMetrics` 的共享指标渲染逻辑，除非发现明确回归阻塞 NodeCompare 新 IA。

## Technical Approach

- 复用上一批审计和已落地模式，使用 `research/remaining-pages-ia-audit.md` 作为本批范围依据。
- 用户选择 A：`NodeComparePage` 专项，因为它是剩余页面里最清晰、可控的 IA 缺口。
- 实施边界：增强 compare command/header panel，明确 24h runtime facts 对比窗口；增加紧凑 A/B 摘要带，突出 health、lifecycle、binding/monitoring、region/city/provider、sample availability；保留 `NodeWatchtowerMetrics` 作为详细指标区。
- 冻结契约：不改后端/API，不改 `/nodes/compare?id=...&id=...` query contract，不改 `getNode` / `getNodeRuntimeFacts` 请求形状，不改 `window=24h`，不改现有 PageState 空/错态语义。

## Research References

- [`research/remaining-pages-ia-audit.md`](research/remaining-pages-ia-audit.md) — 审计剩余前端页面，推荐 NodeComparePage 作为最安全的下一批默认范围。
- [`../archive/2026-05/05-20-continue-page-information-architecture-optimization/research/next-candidate-pages-audit.md`](../archive/2026-05/05-20-continue-page-information-architecture-optimization/research/next-candidate-pages-audit.md) — 上一批候选页审计，曾将 NodeComparePage 标记为低风险候选。
- [`../archive/2026-05/05-20-continue-next-page-information-architecture-optimization/research/targets-nodes-list-control-audit.md`](../archive/2026-05/05-20-continue-next-page-information-architecture-optimization/research/targets-nodes-list-control-audit.md) — 已完成 Targets/Nodes 联合梳理的专项审计。

## Decision (ADR-lite)

**Context**: 剩余页面中，`NodeComparePage` 是仍有明显 IA 缺口且实现/测试风险较低的页面；Settings 有样式/结构债务但涉及密钥保存语义；Events/Dashboard/AssetDecisions/VPSPage 已较强，低收益 churn 风险更高。

**Decision**: 用户选择 A。本批只做 `NodeComparePage` 专项：增强比较上下文和 A/B 摘要，让默认视图先回答两个 Node 的关键差异，再保留现有 `NodeWatchtowerMetrics` 作为详细指标区。

**Consequences**: 该选择范围最可控，能补齐剩余明显缺口；Settings 与其他已较强页面延期，NodeOnboarding 继续保持 safety-only。

## Technical Notes

- Current task: `.trellis/tasks/05-20-continue-remaining-page-information-architecture-optimization`.
- Likely candidate files for A: `web/src/pages/NodeComparePage.tsx`, `web/src/pages/NodeComparePage.test.tsx`, `web/src/styles/pages.css`.
- Likely candidate files for B: A files plus `web/src/pages/SettingsPage.tsx`, `web/src/pages/SettingsPage.test.tsx`, `web/src/styles/pages.css`.
