# 继续推进下一批页面信息架构优化

## Goal

在已完成 Node Detail、VPS Detail、Subscriptions、Providers、Target Detail 的信息架构优化后，继续选择下一批候风前端页面做高价值、可控风险的 IA 优化。目标不是横扫所有页面，而是按页面主任务收敛默认信息层级、操作入口和次级工作面，让页面更像工程师长期使用的工具界面。

## What I already know

- 用户希望在上一批 Target Detail 完成后继续推进其他页面。
- 用户选择 `TargetsPage` + `NodesPage` 列表控制带联合梳理，并明确理由：候风仍处于早期开发阶段，不能每个任务都只做过小 MVP，否则推进慢且多个页面容易割裂；统一修改优势明显。
- 已完成参考：Node Detail 偏运行态观测/控制；VPS Detail 偏资产运营判断；Subscriptions/Providers 偏资产台账列表 Drawer 化；Target Detail 偏 ProbeItem 证据默认可见和 ProbeItem create/edit Drawer 化。
- 上一批候选页审计已经指出：`NodeComparePage` 是低风险薄修候选；`TargetsPage`/`NodesPage` 用户价值高但交互和测试风险高；`NodeOnboardingPage` 安全敏感；`EventsPage`、`DashboardPage`、`AssetDecisionsPage` 当前已有较强模式或优先级较低。
- 项目约束仍是 dark-first、高密度、中文主界面，不引入新设计系统/图表库/CSS 框架，不主动改后端 API 或数据模型。

## Assumptions (temporary)

- 本任务继续聚焦前端 IA/UI 结构调整。
- 下一批扩大为 `TargetsPage` + `NodesPage` 联合梳理，但仍限制在列表控制带、支持面、筛选入口、批量状态和创建入口的默认层级，不做全页重写。
- 已完成页面不在本任务里反复重改，除非发现明显回归或阻塞一致性的问题。

## Open Questions

- 已解决：用户选择 C，下一批做 `TargetsPage` + `NodesPage` 列表控制带联合梳理；接受更大范围以换取核心观测列表页一致性。

## Requirements (evolving)

- 每个纳入范围的页面必须先定义页面主任务，再重排默认信息层级和操作入口。
- 默认视图应优先回答页面最重要的问题，减少同权平铺、重复状态和滚屏成本。
- 创建/编辑/筛选/历史/次级详情应进入 Drawer、菜单、筛选面板或可解释的详情入口，而不是挤占主扫描路径。
- 不引入新依赖，不改后端模型/API，优先复用现有 atoms/shared components、BEM、design tokens 与 `pages.css`。
- 保留现有路由、URL 状态、表单、确认流程和测试契约。
- `TargetsPage` 与 `NodesPage` 应形成一致的列表控制带信息层级：页面主判断、支持面、筛选入口、选择/批量状态、创建/接入入口的优先级应可互相映射。
- 本批不改后端/API，不改 DataTable row navigation guards，不改 Node onboarding token/command 安全契约，不改 runtime/batch mutation 语义。

## Candidate Approaches

### A. `NodeComparePage` 薄修（推荐默认）

- 主任务：快速判断两个 Node 的差异是否值得进一步排查。
- 做法：在现有 A/B metrics 之前增加比较摘要带，突出身份差异、运行状态、最新样本可用性和关键 CPU/内存/磁盘差异；保留现有 `NodeWatchtowerMetrics` 细节。
- 优点：低风险、测试面小、能补齐上一批审计里最明确的低风险页。
- 代价：用户价值窄，不会解决列表页扫描效率问题。

### B. `TargetsPage` 窄切片

- 主任务：让目标列表页更快回答“哪些 Target 需要处理、筛选/批量/创建入口在哪里”。
- 做法：只梳理顶部支持面、筛选入口和选择/批量状态层级；不改表格列、不改 URL filter、不改 create Drawer、不改 runtime overlay。
- 优点：用户价值高，承接 Target Detail 后的观测资产工作流。
- 代价：页面交互密集，测试风险高于 A，必须严格控制边界。

### C. `TargetsPage` + `NodesPage` 列表控制带联合梳理

- 主任务：统一两个核心观测列表页的列表扫描、筛选、批量操作和创建入口层级。
- 做法：只做控制带/支持面层级，不动底层表格行为和安全敏感 onboarding 语义。
- 优点：一致性收益最大。
- 代价：测试面最大，容易变成列表页大重构，不适合作为默认 MVP。

## Acceptance Criteria (evolving)

- [ ] 形成 `TargetsPage` + `NodesPage` 当前控制带/支持面/筛选/批量/创建入口审计，说明纳入范围与延期范围。
- [ ] 两个页面默认视图减少弱层级或割裂的控制区域，突出页面主判断和下一步动作。
- [ ] 两个页面的筛选、创建、选择/批量状态、运行控制入口形成一致但不机械同构的层级。
- [ ] 保留现有 URL 状态、表格 row guard、create/onboarding、runtime/batch mutation 和危险确认契约。
- [ ] 对应页面测试更新，覆盖新 IA 和保留关键工作流。
- [ ] Web lint/test/build 通过，必要时跑完整 verify 和浏览器 sanity。

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
- 不重改已完成 Node/VPS/Subscriptions/Providers/Target Detail。
- 不触碰 NodeOnboardingPage 的 token reveal/copy、center-generated install command、安全确认等敏感契约，除非另开专项。
- 不改 `TargetsPage` / `NodesPage` 的后端 API、URL filter contract、DataTable row navigation guard、runtime/batch mutation payload 或确认语义。
- 不把本批扩大到 `NodeComparePage`、`EventsPage`、`DashboardPage`、`AssetDecisionsPage`。

## Technical Approach

- 复用上一批审计和已落地模式，已补充 `research/targets-nodes-list-control-audit.md` 作为本批代码级审计。
- 本批采用联合梳理而非极小 MVP：在早期开发阶段优先消除核心观测列表页之间的控制层级割裂。
- 共同层级定义为：页面身份 + 主创建入口 → observability support/evidence → list frame command band → filter controls → batch scope controls → table/runtime overlays。
- Targets 可以新增或强化 list-scope command band，让结果数量/当前范围靠近表格，而不是只在 support surface 出现；Nodes 可以保留 tabs/compare/trends/auto-refresh/sort/command 等 domain-specific 控件，但要放入同一套 list-frame command band 语义里。
- 批量操作只显式说明“当前筛选范围”/“当前 filtered set”，不改变 eligibility 和 payload：Targets 仍只在 group filter active 时显示批量，Nodes 仍在任意 active filter 时显示批量。
- 创建入口、空态位置/文案、support context 与 filter editing controls 可以做展示层统一，但必须保留 target create → detail、node create → onboarding 的行为。
- 实施边界固定为 presentation-level IA pass：统一梳理列表控制带、支持面、筛选、批量、创建/接入入口层级；不改 URL 状态、表格 row guards、create/onboarding 安全语义和 runtime/batch mutation 契约。

## Research References

- [`../archive/2026-05/05-20-continue-page-information-architecture-optimization/research/next-candidate-pages-audit.md`](../archive/2026-05/05-20-continue-page-information-architecture-optimization/research/next-candidate-pages-audit.md) — 上一批剩余页面候选审计，提示列表页价值高但交互和测试风险高。
- [`research/targets-nodes-list-control-audit.md`](research/targets-nodes-list-control-audit.md) — Targets/Nodes 列表控制带专项审计，建议做 presentation-level 联合梳理并冻结 URL/filter/row guard/onboarding/runtime/batch 契约。

## Decision (ADR-lite)

**Context**: 上一批审计将 `TargetsPage` / `NodesPage` 列为高价值但高风险页面。用户指出候风仍处于早期开发阶段，如果每个任务都只做过小 MVP，会导致推进太慢，且多个页面之后容易出现信息架构割裂。

**Decision**: 本批选择 `TargetsPage` + `NodesPage` 列表控制带联合梳理，接受较大的前端测试面，换取两个核心观测列表页在支持面、筛选、选择/批量、创建/接入入口上的一致层级。

**Consequences**: 实施前必须做更细的代码级审计，严格冻结 URL 状态、DataTable row guard、onboarding token/command 安全契约和 runtime/batch mutation 语义，避免联合梳理失控成大重构。

## Verification Notes

- Focused Targets/Nodes page tests passed: `TMPDIR="/Users/weibo/Code/houfeng/.tmp/vitest" npm --prefix "/Users/weibo/Code/houfeng/web" run test -- --run src/pages/TargetsPage.test.tsx src/pages/NodesPage.test.tsx`.
- Full web tests passed: `TMPDIR="/Users/weibo/Code/houfeng/.tmp/vitest" npm --prefix "/Users/weibo/Code/houfeng/web" run test -- --run`.
- Web lint/build passed: `npm --prefix "/Users/weibo/Code/houfeng/web" run lint`, `npm --prefix "/Users/weibo/Code/houfeng/web" run build`.
- Full repository verification passed: `TMPDIR="/Users/weibo/Code/houfeng/.tmp/verify-tmp" GOCACHE="/Users/weibo/Code/houfeng/.tmp/go-cache" "/Users/weibo/Code/houfeng/scripts/verify.sh"`.
- Browser sanity passed with fixture/mock API for `/targets` and `/nodes` at `1440x1000` and `390x900`: `TMPDIR="$PWD/.tmp/playwright" /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5179/ --mock-api observability-support --route /targets --route /nodes --viewport 1440x1000 --viewport 390x900`.
- Caveat: full verify emitted a local Node engine warning because the environment uses Node `v24.14.1` while the web package declares Node `22.x`; verification still completed successfully.

## Technical Notes

- Current task: `.trellis/tasks/05-20-continue-next-page-information-architecture-optimization`.
- Prior archived task: `.trellis/tasks/archive/2026-05/05-20-continue-page-information-architecture-optimization`.
- Candidate files: `web/src/pages/NodeComparePage.tsx`, `web/src/pages/TargetsPage.tsx`, `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx`, `web/src/pages/EventsPage.tsx`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/AssetDecisionsPage.tsx`.
