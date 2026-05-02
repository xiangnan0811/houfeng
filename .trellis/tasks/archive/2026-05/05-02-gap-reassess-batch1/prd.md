# Reassess gap-checklist Closed status — batch 1 (Product/arch + Core model)

> 第一批 reassess，聚焦 `docs/release/v1-gap-checklist.md` 前两段。
> 后续 batch 2/3/4 涉及 Runtime / UI / Notifications / Delivery / Auth / V1.x visual 各章节。

## Goal

对 batch 1 的 9 个 `Closed (⚠️ need-reassess)` 行做现场代码验证，给每行打新 verdict，更新 gap-checklist。

## What I already know

### Batch 1 完整清单（实读自 v1-gap-checklist.md:21-39）

**Product and architecture baseline**（5 行，4 Closed + 1 Deferred 不动）：
1. `Product naming is 候风 / Houfeng Fleet Control Plane` — Closed
2. `Go center + Go agent + React/Vite + PostgreSQL` — Closed
3. `Single center process owns API/UI/background workers/notifications` — Closed
4. `systemd agent direction documented` — Closed
5. `Docker-first deployment` — **Deferred outside V1**（不动）

**Core object model**（5 行，5 Closed）：
6. `Node persistence and UI` — Closed
7. `Target persistence and UI` — Closed
8. `ProbeItem persistence and UI` — Closed
9. `HostSample and ProbeObservation ingestion` — Closed
10. `Incident and Event model` — Closed

### Verdict 格式（自决）

4 档 verdict，写在 Status 列：

| 新 Status | 含义 |
|---|---|
| `Closed (verified 2026-05-02)` | 真实现，与设计一致，证据扎实 |
| `Partial (was Closed)` | 实现存在但部分缺失/与设计偏离 |
| `Not Closed (was Closed)` | 实际未实现 / 严重偏离设计 / regressed |
| `Reassess inconclusive` | sub-agent 不能判断，需真环境或更多上下文（移交后续 task） |

Evidence 列保留原内容 + 追加：`**Reassessed 2026-05-02**: <验证 finding>` 一段。

drop `(⚠️ need-reassess)` 标记（无论 verdict 如何，都已 reassessed）。

## Decision (ADR-lite)

**Context**: 9 行 Closed 待重审，需要标准化 verdict 表达。

**Decision**: 4 档 verdict + 直接改 Status 列 + Evidence 列追加验证细节。**不新增列**（避免破坏下游引用）。

**Consequences**:
- gap-checklist 可读性保持
- 单一 Status 列权威，Evidence 提供证据
- "Reassess inconclusive" 行作为 follow-up task 输入

## Requirements

1. 对 9 行 Closed 逐行现场验证（grep 引用的代码路径 + 看 ` ./trellis/spec/`、CLAUDE.md 实际现状）
2. 每行更新 gap-checklist：
   - Status 列 `Closed (⚠️ need-reassess)` → 新 verdict
   - Evidence 列追加 `**Reassessed 2026-05-02**: <finding>`
3. 修改范围**仅** `docs/release/v1-gap-checklist.md`
4. 不动业务代码 / .trellis/spec/ / 其他 docs / CLAUDE.md / README

## Acceptance Criteria

- [ ] 9 行 Closed 全部 reassessed，各有新 verdict + 验证 finding
- [ ] 0 个行仍带 `(⚠️ need-reassess)` 标记（在前两段）
- [ ] Deferred outside V1 行（Docker）保持不动
- [ ] 后续段（Runtime/UI/...）的 need-reassess 标记**不动**（留给 batch 2/3/4）
- [ ] git diff 范围只在 `docs/release/v1-gap-checklist.md`
- [ ] 不修改 prd.md / .trellis/spec/ / CLAUDE.md / README / 业务代码

## Definition of Done

- gap-checklist 前两段 9 行 verdict 落地
- 任何"Not Closed" / "Reassess inconclusive" verdict 行有清晰理由（便于 follow-up 立 task）
- commit 清晰可 review

## Final Confirmation

**Goal**: reassess 9 行 Closed → 新 verdict（4 档）+ Evidence 追加验证 finding。

**Approach**: 一个 trellis-implement sub-agent（具备 grep / read / edit），先按 Evidence 列引用的代码路径现场验证，再 edit gap-checklist。

**Implementation Plan**:
- PR1: sub-agent 跑 reassess + edit
- PR2 (optional): 如发现 "Not Closed" / "Partial" 行需要单独 follow-up task，main agent 在 commit 阶段决定是否同时 propose

**Sub-agent 不能做**：
- 改业务代码 / .trellis/spec/ / 其他 docs
- 改 batch 1 之外的 gap-checklist 行（保留 need-reassess 给后续 batch）
- git commit / 跑 task.py / 改 prd.md

## Out of Scope

- batch 2/3/4 的 33 行 reassess（独立 task）
- 对 reassess 发现的 "Not Closed" / "Partial" 行做现场修复（仅记录 verdict + 理由，由后续 task 决定怎么修）
- 改 V1 业务范围 / V1 release gate 定义
