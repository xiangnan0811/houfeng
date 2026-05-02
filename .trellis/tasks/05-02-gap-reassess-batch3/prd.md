# Reassess gap-checklist Closed status — batch 3 (UI surfaces)

> Sister task to archived batch 1 + batch 2. Same 4-tier verdict scheme.

## Goal

reassess "UI and interaction surfaces" 段 8 行 Closed (need-reassess)。本段是用户"实现连 V0.1 都不到"判断**最可能源头**——sub-agent 需更严格：实际打开 page 文件、对照 `docs/design/v1-baseline/rules-and-interaction.md` 的交互期望、判断"功能完整 CRUD"还是"只剩壳子"。

## Scope

**UI and interaction surfaces** 段 8 行 Closed（第 9 行 "Visual screenshot comparison" 已 Partial，**跳过**）：
1. Frozen app shell and primary navigation
2. Dashboard abnormal summaries and event stream
3. Nodes list filters and onboarding entry
4. Node detail operational summary and trends
5. Target list/detail and ProbeItem management
6. Events advanced filters
7. Settings runtime truthfulness
8. Chinese-first UI copy and dense baseline hierarchy

## Verdict scheme (same as batch 1+2)

4 档：`Closed (verified 2026-05-02)` / `Partial (was Closed)` / `Not Closed (was Closed)` / `Reassess inconclusive`。
Status 列改值；Evidence 列追加 `**Reassessed 2026-05-02**: <finding>`；drop `(⚠️ need-reassess)`。

## 严格判断准则（本批特殊）

UI 段比 batch 1+2 更需要严格。判 verdict 时考虑：

- **功能完整性**：page 是否实现了 rules-and-interaction.md 描述的所有交互（创建 / 编辑 / 状态切换 / 删除 / 搜索 / 筛选 / 详情 / ...），还是只渲染表头 / 只读视图 / 半成品
- **设计 vs 实现 mismatch**：v1-baseline rules-and-interaction.md / interactive-prototype-and-operation-flow.md 描述的关键流程是否真的可走通
- **错误态 / 空态 / loading 态**：是否有合理处理还是缺失
- **已知偿还点**：gap-checklist 末尾 12 条新 gap 中涉及 UI 的（gap #10 NodesPage createNode bypass / gap #11 长 page 文件）应该被纳入 verdict 考量

宁严勿松：UI 行普遍倾向 Partial / Not Closed 不奇怪，符合 user judgment。

## Acceptance Criteria

- [ ] 8 行 reassessed，verdict + finding 落地
- [ ] 0 个行（UI 8 行）仍带 `(⚠️ need-reassess)`
- [ ] Visual screenshot comparison (Partial) 不动
- [ ] 后续段（Delivery / Auth / V1.x visual）保留 need-reassess
- [ ] git diff 范围只在 `docs/release/v1-gap-checklist.md`

## Out of Scope

- batch 4 (Delivery + Auth + V1.x visual) reassess
- 修任何业务代码 / 已 archive 文档
- 改 prd.md / .trellis/spec/ / CLAUDE.md / README

## Final Confirmation

**Goal**: reassess 8 UI 行 → 4 档 verdict（预期含较多 Partial）。
**Approach**: trellis-implement sub-agent，**实读** page 文件（不只看文件名）。
**Plan**: PR1 = sub-agent reassess + edit；main agent commit (1 work + 1 trellis bookkeeping)。
