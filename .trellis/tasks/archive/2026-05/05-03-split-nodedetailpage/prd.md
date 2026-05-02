# Stage 1 P1 phase 1: split NodeDetailPage into section components

> gap #11 phase 1: 拆 NodeDetailPage 1138 行。TargetDetailPage 1731 行留 phase 2 (Stage 2 候选)。

## Goal

把 `web/src/pages/NodeDetailPage.tsx` (1138 行) 主要 DetailSection 抽到 `web/src/components/node-detail/` 子目录的独立组件，page 变薄成 orchestrator。所有现有业务行为保持。

## Decision (ADR-lite)

**Decision**: section components in `web/src/components/node-detail/` 子目录（与 atoms/ filters/ 并列的 design-system 一员，但更"业务特定"）。
- Page level 保留 state、useEffect、API calls、event handlers
- Section component 接受 props（已 fetched 的 data + handler callbacks），渲染 UI
- 这是"presentational component"模式：业务逻辑留 page，展示拆 component

**抽取目标**（per reassess batch 3 finding，page 含 8 DetailSections）：
- hero（节点身份头）
- 状态摘要（status summary 4 微状态）
- 绑定冲突卡片（binding conflict）
- 标签备注（labels + note edit）
- 运行控制（pause/resume/maintenance）
- 生命周期 + 退役危险区
- 主机指标 4 卡（CPU / mem / disk / net）
- 趋势 4 卡（含 3 Sparkline）

至少抽 4 个最容易剥离的（无复杂 state interlock 的 section）；其他可推 phase 1.2 / 后续 task。

## Requirements

1. **新建 `web/src/components/node-detail/`** 子目录 + barrel `index.ts`
2. 至少抽 **4 个** section component（推荐：状态摘要 / 标签备注 / 主机指标 4 卡 / 趋势 4 卡——这 4 个相对独立）
3. NodeDetailPage 改为 orchestrator：state/handler/API call 不动；section 段渲染换成 `<NodeStatusSummary {...props} />` 等
4. 抽出的 component 命名：`NodeHero` / `NodeStatusSummary` / `NodeBindingConflict` / `NodeLabelsAndNote` / `NodeRuntimeControls` / `NodeLifecycleSection` / `NodeHostMetrics` / `NodeTrendCards` 任选 4+
5. 加 component 单元测试（每抽出一个 ≥1 test）
6. 现有 `NodeDetailPage.test.tsx` 必须不动业务断言；如断言失败因 DOM 结构变化必须 minimal-fix（不删功能）
7. NodeDetailPage 行数应**显著下降**（目标 < 700 行；如 4 个 section 抽出有限可能达不到，最低 < 900 行）
8. `make verify-web` 全绿

## Acceptance Criteria

- [ ] `web/src/components/node-detail/` 含 ≥ 4 个 section component + barrel + 测试
- [ ] NodeDetailPage 行数 < 900（理想 < 700）
- [ ] NodeDetailPage 业务行为完全保留（fetch / runtime actions / metadata edit / binding conflict 流程）
- [ ] `make verify-web` 全绿（lint + 至少 284+N 测试 + build）
- [ ] git diff 范围只在 web/src/components/node-detail/ + web/src/pages/NodeDetailPage.tsx + web/src/pages/NodeDetailPage.test.tsx (+ 任务脚手架)

## Out of Scope

- TargetDetailPage 拆分 (1731 行，phase 2)
- 其他 long-page (SettingsPage / TargetsPage / NodesPage) 拆分
- 抽取所有 8 sections（4 起步，其余 follow-up）
- 改 page state 管理（仍 useState/useEffect）
- 引入新 lib 依赖

## Technical Notes

- 参考 `web/src/components/atoms/` 的 props/导出风格
- 参考 `web/src/components/filters/` 的子目录组织（同模式）
- DetailSection 通用容器 (在 `web/src/components/DetailSection.tsx`) 已存在——抽出的 section 仍包在 DetailSection 内（保 baseline 视觉一致）

## Final Confirmation

**Goal**: 抽 ≥4 个 NodeDetailPage section 到独立 component；page 显著变薄。
**Approach**: trellis-implement 一次完成；预估 3-5h。
