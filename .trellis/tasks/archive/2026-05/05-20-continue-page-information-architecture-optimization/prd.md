# 继续推进页面信息架构优化

## Goal

在已完成 Node Detail、VPS Detail、Subscriptions、Providers 的信息架构优化后，继续分批处理候风前端剩余页面，把高价值页面从“区块/操作堆叠”收敛为更贴合页面任务的工程工具界面。下一批需要优先选择用户价值高、风险可控、能复用既有 IA 模式的页面，而不是一次性横扫所有页面。

## What I already know

- 用户认为上一批只完成了少数页面，希望继续推进其他页面。
- 已完成参考：Node Detail 偏运行态观测/控制；VPS Detail 偏资产运营判断；Subscriptions/Providers 已完成资产台账列表页 Drawer 化与紧凑摘要。
- 上一批研究已指出：`TargetDetailPage` 是剩余详情页里最明显的 IA 缺口，但风险较高；`TargetsPage`/`NodesPage` 是重要列表页但交互密集；`DashboardPage`、`VPSPage`、`AssetDecisionsPage`、`EventsPage` 当前优先级较低或已有较强模式。
- 继续推进仍应遵守 dark-first、高密度、工程师长期使用友好，不引入新设计系统/图表库/CSS 框架。

## Assumptions (temporary)

- 本任务仍优先是前端 IA/UI 结构调整，不主动改后端 API 或数据模型。
- 下一批应该收敛为一个清晰 MVP，不一次性重写所有剩余页面。
- 已完成页面作为模式参考，不应在本任务里反复重改，除非发现明显回归或阻塞一致性的问题。

## Open Questions

- 已解决：用户选择 A，下一批 MVP 做 `TargetDetailPage` 专项；不纳入 `NodeComparePage`、`TargetsPage`、`NodesPage` 或其他页面。

## Requirements (evolving)

- 每个纳入范围的页面必须先定义页面主任务，再重排默认信息层级和操作入口。
- 默认视图应优先回答页面最重要的问题，减少同权平铺、重复状态和滚屏成本。
- 创建/编辑/筛选/历史/次级详情应进入 Drawer、菜单、筛选面板或可解释的详情入口，而不是挤占主扫描路径。
- 不引入新依赖，不改后端模型/API，优先复用现有 atoms/shared components、BEM、design tokens 与 `pages.css`。
- 保留现有路由、URL 状态、表单、确认流程和测试契约。

## Acceptance Criteria (evolving)

- [x] 形成下一批页面审计与优先级，说明纳入 MVP 与延期范围。
- [x] 选中页面默认视图减少低价值平铺区块，突出页面主判断和下一步动作。
- [x] 次级操作/详情被合理收口，危险操作保留确认和隔离。
- [x] 对应页面测试更新，覆盖新 IA 和保留关键工作流。
- [x] Web lint/test/build 通过，必要时跑完整 verify 和浏览器 sanity。

## Definition of Done

- PRD、research、implement/check JSONL 完成并归档。
- 前端通过 `npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- 最终完整验证优先跑 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- UI/浏览器 sanity 覆盖被改造的主页面 golden path；如果无法连真实后端，明确 fixture/mock caveat。
- 按分支/PR/release 约定完成后续流程。

## Out of Scope (explicit)

- 不一次性重写全部剩余页面。
- 不重做视觉语言或引入新设计系统。
- 不改后端模型/API，除非审计证明前端无法完成核心 IA 目标。
- 不机械套用上一批 Drawer 模式；每个页面按任务判断。
- 不把已完成 Node/VPS/Subscriptions/Providers 当作本批主要重做对象。

## Technical Approach

- 复用上一批审计和已落地模式，补充对当前候选页面的代码级审计。
- 将候选分为：观测详情页、观测列表页、特殊/低风险页，按用户价值和交互风险选择 MVP。
- 当前研究推荐下一批以 `TargetDetailPage` 为 MVP：让 ProbeItem 证据默认可见，把 ProbeItem create/edit 从 inline property-list 移到 Drawer，保留危险操作确认、runtime 控制、stale-route safety、metadata 并发保护和 API payload。
- `NodeComparePage` 可作为低风险第二页，只做比较摘要薄修；`TargetsPage` / `NodesPage` / `NodeOnboardingPage` 因交互或安全风险延期。
- 先用 research 文件记录证据，再更新 PRD 的决策和实施边界。

## Research References

- [`research/next-candidate-pages-audit.md`](research/next-candidate-pages-audit.md) — Ranks next candidates and recommends `TargetDetailPage` as the next MVP, with `NodeComparePage` as an optional low-risk secondary slice.
- [`research/browser-sanity.md`](research/browser-sanity.md) — Confirms Target Detail ProbeItem evidence default visibility and create/edit Drawer behavior with fixture-backed browser sanity.

## Decision (ADR-lite)

**Context**: 继续推进剩余页面 IA 时，候选页存在明显价值/风险差异：`TargetDetailPage` 是最清晰的观测详情页缺口，`TargetsPage`/`NodesPage` 交互和测试风险更高，`NodeComparePage` 虽低风险但用户价值较窄。

**Decision**: 用户选择 A。本 MVP 只做 `TargetDetailPage` 专项：提升 ProbeItem 证据的默认可见性，把 ProbeItem create/edit 从 inline property-list 扩展移入 Drawer 或等价的次级工作面，保留 runtime/danger/metadata/lifecycle/history 的既有行为和安全确认。不纳入 `NodeComparePage` 或列表页。

**Consequences**: 该选择集中解决 Target detail 的核心判断路径，复用 Node Detail watchtower 模式，避免与列表页 URL/filter/batch 行为和安全敏感 onboarding 行为产生大范围测试 churn。

## Verification Notes

- Focused Target Detail tests passed: `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run src/pages/TargetDetailPage.test.tsx`.
- Full web tests passed: `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`.
- Web lint/build passed: `npm --prefix web run lint`, `npm --prefix web run build`.
- Full repository verification passed: `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`.
- Browser sanity passed with fixture/mock API for Target Detail; no local authenticated center/backend was used.

## Technical Notes

- Current task: `.trellis/tasks/05-20-continue-page-information-architecture-optimization`.
- Prior archived task: `.trellis/tasks/archive/2026-05/05-19-optimize-remaining-page-information-architecture`.
- Likely candidate files: `web/src/pages/TargetDetailPage.tsx`, `web/src/pages/TargetsPage.tsx`, `web/src/pages/NodesPage.tsx`, `web/src/pages/AssetDecisionsPage.tsx`, `web/src/pages/EventsPage.tsx`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx`, `web/src/pages/NodeComparePage.tsx`.
