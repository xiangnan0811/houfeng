# Stage 1 completion docs sync

> Pure docs sync. Reflect本 session 实际完成度 to project-facing docs.

## Goal

把 Stage 1 P0/P1 marathon 完成度真实反映到 `docs/release/next-phase-plan.md` 和 `docs/release/v1-gap-checklist.md`，让两份权威文档与 git history 一致。

## What I already know

### 本 session 完成项汇总

**Stage 1 P0**：
- gap #3 0004 撞车 → docs 约定（commit `6a52ced`）
- gap #7 main.go logger to slog（commit `a613f8e`）
- gap #12 verify-web 加 lint（commit `1704c02`）
- lint baseline 4 errors（commit `75d4034`）
- 42 行 reassess (4 batch)（commits `dfa32fc` / `3f7cca9` / `f09bdf7` / `227537d`）
- next-phase-plan reframe（commit `4cbbed9`）
- list-filter completion 3 children（commits `7cbf8d6` / `43af18b` / `e8c6908` + 抽 FilterBar `05cb274`）
- V1 live smoke run（commit `6394b29`）

**Stage 1 P1**：
- gap #4 sessions 索引（commit `8cbae4d`）
- gap #10 NodesPage createNode（commit `d78ef0f`）
- smoke 4 caveats 入 gap-checklist（commit `92e5b6f`）
- Telegram mark deferred（同 92e5b6f）
- gap #9 双 fetch 合并（commit `b354f3f`）
- gap #11 phase 1 NodeDetailPage（commit `8b765c9`）
- gap #11 phase 2 TargetDetailPage（commit `9bcc779`）

**Partial 全部闭合**：4 个 Partial（NodesPage createNode + 3 list filter）→ 0 Partial。

## Requirements

### 1. next-phase-plan.md 标 Stage 1 完成度

Stage 1 P0 段下方加 "✅ Stage 1 P0 全部完成" banner + 每项工作项前缀 ✅。
Stage 1 P1 段同样加 "✅ Stage 1 P1 实质完成（剩余项推 Stage 2）" + 标完成项 ✅，未完成项标"🔲 deferred to Stage 2 / phase 2.2"。

注：Stage 2 / Stage 3 段保持原文（未触发）。

### 2. v1-gap-checklist.md "V1 收口期发现的 gap 项" 段加 closed 标注

末尾段（含 #1-#16）每个**已 closed** 的 gap 行 Status 列追加 `→ Closed (2026-05-03)`：
- Backend: #3 / #4 / #7 → Closed；#5 (worker count CLAUDE.md sync 由 T2 已闭) → Closed；#1 (handler list) / #2 (subpackage list) / #6 (ProbeKind) → Closed (CLAUDE.md 已同步)
- Web: #8 / #9 / #10 / #11 / #12 → Closed
- Operations: #15 (HOUFENG_WEB_DIST_DIR 警告已加) → Closed (docs); #13 / #14 / #16 → Open (未实施修复，仅记录)

注：closed 行加注 commit hash 引用。

### 3. v1-gap-checklist.md "Final V1 release gate" 段更新

在 line ~107 "Final V1 release gate" 段加完成度小结：
"2026-05-03 状态：Stage 1 P0/P1 marathon 完成；详见 next-phase-plan.md 末尾 Reassess findings 段。剩余 release-gate 项（Telegram 真实交付、严格视觉证据）仍 deferred ops follow-up。"

## Acceptance Criteria

- [ ] next-phase-plan.md Stage 1 P0/P1 段加 ✅ 标注 + 完成 banner
- [ ] v1-gap-checklist.md "V1 收口期发现的 gap 项" 段每行加 closed/open 状态
- [ ] v1-gap-checklist.md "Final V1 release gate" 段加 2026-05-03 状态小结
- [ ] git diff 范围只在 next-phase-plan.md + v1-gap-checklist.md（+ 任务脚手架）

## Out of Scope

- 改业务代码 / .trellis/spec/ / 其他 docs / CLAUDE.md / README
- archive 已完成的"V1 收口期发现的 gap 项"段（保留作历史；只标 closed）
- 修 Open gap 项 (#13 / #14 / #16) 实质代码 — 是 ops/code follow-up

## Final Confirmation

**Goal**: 2 docs sync 反映 Stage 1 完成度。
**Approach**: trellis-implement 一次完成。
