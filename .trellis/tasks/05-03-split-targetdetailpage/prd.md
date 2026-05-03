# Stage 1 P1 phase 2: split TargetDetailPage into section components

> 复用 NodeDetailPage phase 1 (archived) pattern。1731 行，最大 page。

## Goal

把 `web/src/pages/TargetDetailPage.tsx` (1731 行) 主要 section 抽到 `web/src/components/target-detail/` 子目录的独立组件，page 变薄成 orchestrator。所有现有业务行为保持。

## Decision (ADR-lite)

**Decision**: 镜像 NodeDetailPage phase 1 模式 — `web/src/components/target-detail/` 子目录 + presentational components。
- Page 保留 state/useEffect/API/handlers/refs（业务逻辑）
- Section component 接受 props 渲染 UI

**抽取目标**（per reassess + §10.8 设计稿，TargetDetailPage 含多 section）：
- hero（target 身份头）
- status summary（health / 异常 ProbeItem 数 / 关键异常摘要）
- ProbeItem 列表 + CRUD（toggle / edit / delete / + 新增）
- 执行节点视角（哪些节点承担 + 成功率）
- 当前活跃异常
- 最近事件
- 趋势 / 概览 / 标签备注 / 运行控制 等其他 section

**目标至少抽 5 个**（最独立的）；ProbeItem CRUD section state interlock 高，可抽 presentational shell + 留 state 在 page，或推 phase 2.2 follow-up。

## Requirements

1. 新建 `web/src/components/target-detail/` 子目录 + barrel `index.ts`
2. 至少抽 **5 个** section component
3. TargetDetailPage 改为 orchestrator
4. 抽出 component 命名：`TargetHero` / `TargetStatusSummary` / `TargetProbeItemList` / `TargetExecutionNodes` / `TargetActiveIncidents` / `TargetRecentEvents` / `TargetTrends` / `TargetLabelsAndNote` / `TargetRuntimeControls` 任选 5+
5. 加 component 单元测试（每抽出一个 ≥1 test）
6. 现有 `TargetDetailPage.test.tsx` 必须保持业务断言；DOM 改动需 minimal-fix
7. TargetDetailPage 行数显著下降（目标 < 1100；理想 < 900）—— 1731 行较大，500+ 行降幅合理
8. `make verify-web` 全绿

## Acceptance Criteria

- [ ] `web/src/components/target-detail/` 含 ≥ 5 section + barrel + 测试
- [ ] TargetDetailPage 行数 < 1100
- [ ] TargetDetailPage 业务行为完全保留（fetch / ProbeItem CRUD / runtime actions / metadata edit / detail navigate）
- [ ] `make verify-web` 全绿（lint + 296+N 测试 + build）
- [ ] git diff 范围只在 web/src/components/target-detail/ + web/src/pages/TargetDetailPage.tsx + web/src/pages/TargetDetailPage.test.tsx (+ 任务脚手架)

## Out of Scope

- ProbeItem CRUD 完整解耦（state interlock 高，可推 phase 2.2）
- 抽所有 section（5+ 起步即可）
- 改 page state 管理 / 引入新 lib

## Technical Notes

- 参考 archived `.trellis/tasks/archive/2026-05/05-03-split-nodedetailpage/prd.md` 的 D1-D5 决策
- 参考 `web/src/components/node-detail/` 5 个抽出 component 的 props 模式

## Final Confirmation

**Goal**: 抽 ≥5 个 TargetDetailPage section；page 显著变薄。
**Approach**: trellis-implement 一次完成；预估 4-6h（比 NodeDetailPage 大）。
