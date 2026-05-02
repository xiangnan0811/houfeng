# Reframe next-phase-plan with reassess findings

> 短任务，纯文档：把 4 batch reassess 的 root cause 同步进 next-phase-plan + gap-checklist 顶部 banner。

## Goal

更新 2 份文档：
1. `docs/release/next-phase-plan.md` —— 新增 Stage 1 P0 工作项 "Front-end list-page filter completion"；新增 "Reassess findings (2026-05-02)" 段总结
2. `docs/release/v1-gap-checklist.md` —— 顶部 banner 更新：reassess 已完工（不再说 "deferred"），明示 4 partial = root cause

## Requirements

1. **next-phase-plan.md**：
   - 在 "#### P0（阻塞 V1 收口）" 列表 **最前**插入新工作项：
     ```
     - **Front-end list-page filter completion** (root cause of user judgment 实现连 V0.1 都不到)：补齐 3 个 list page 的筛选功能
       - NodesPage：补 §6.3 的 5 项缺失筛选（生命周期 / 供应商 / 地区 / 标签 / 健康，仅"仅看异常 / 运行状态" 2 toggle 已就位）
       - TargetsPage：从零补齐 §6.4 的 6 项筛选条（当前列表是只读表）
       - EventsPage：补 §10.9 的剩余筛选（含 backfill boolean / 时间 segmented / 时间分组 / 加载更早分页）
       - 拆 3 个 follow-up task 推进
     ```
   - 在文档末尾"变更日志"前**新增**段：
     ```
     ## Reassess findings (2026-05-02)
     
     gap-checklist 42 个 Closed (⚠️ need-reassess) 行已全部现场验证完成（拆 4 batch task）：
     - 38 行 → Closed (verified 2026-05-02)：foundational + runtime + notification + delivery + auth + visual 系统全部对齐 v1-baseline 设计
     - 4 行 → Partial (was Closed)：全部聚焦在前端，已具体定位
       - NodesPage createNode bypass lib/api.ts (gap #10)
       - NodesPage 列表筛选缺 5/7 项
       - TargetsPage 完全无筛选条
       - EventsPage 高级筛选缺 4 项
     - 0 行 Not Closed / 0 Inconclusive
     
     **关键洞察**：用户判定"实现连 V0.1 都不到"的实证根因 = 前端 list-page 筛选完成度，**不是**后端 / 运行时 / 通知 / 部署 / 认证 / 视觉系统。Stage 1 收口因此应优先解决 list-page filter 工作项。
     ```
2. **v1-gap-checklist.md** 顶部 banner（行 3-9）改写：
   - 原："T3 批量为所有 Closed 行追加 (⚠️ need-reassess) 标记，**不做逐行现场验证**——逐行验证由 T2 起草的 next-phase plan 列为独立 Stage 1 工作项。"
   - 改为："（**已于 2026-05-02 完成**）4 batch task 拆 reassess 工作流：42 个 Closed 行经现场验证后，38 verified + 4 Partial（全部聚焦前端 list-page 筛选完成度）。详见 `docs/release/next-phase-plan.md` 末尾 Reassess findings 段。本表此刻是"V1 已完成度"权威，不再有 mismatch。"

## Acceptance Criteria

- [ ] next-phase-plan.md 新增 P0 工作项 + Reassess findings 段
- [ ] v1-gap-checklist.md 顶部 banner 更新（去掉"不做逐行验证 / 推到 follow-up"措辞）
- [ ] git diff 范围只在这 2 份文档
- [ ] 不动业务代码 / .trellis/spec/ / 其他 docs / CLAUDE.md / README

## Out of Scope

- 实施 list-page filter（独立 follow-up task）
- 起草新 follow-up task PRD（独立任务）
- 重写 next-phase-plan 其他段

## Final Confirmation

**Goal**: 2 份文档微调，反映 reassess 完工 + 锁定 root cause。
**Approach**: trellis-implement sub-agent 一次完成。
**Plan**: 1 work commit + 1 trellis bookkeeping。
