# Reassess gap-checklist Closed status — batch 4 (Delivery + Auth + V1.x visual)

> Final batch. Sister to archived batch 1/2/3. Same 4-tier verdict scheme.

## Goal

reassess 剩余 17 个 Closed (need-reassess) 行，覆盖 v1-gap-checklist.md 最后 3 段：
- Delivery and operations (5 行)
- Authentication V1.x (4 行)
- V1.x visual baseline (8 行 Closed + 2 Deferred 不动)

## Verdict scheme (same as batch 1/2/3)

4 档：`Closed (verified 2026-05-02)` / `Partial (was Closed)` / `Not Closed (was Closed)` / `Reassess inconclusive`。
Status 列改值；Evidence 列追加 `**Reassessed 2026-05-02**: <finding>`；drop `(⚠️ need-reassess)`。

## V1.x visual section 特殊处理

`docs/design/v1.x-frontend-redesign/` 已被 v2-houfeng 取代且 archive 到 `docs/_archive/`。但 V1.x visual 段记录的具体实现（tokens.css 4 主题 / atoms 6 个 / Sidebar shell / Login / Route guard / 8 page chrome / Theme tab）**多数仍在代码中**——v2 重用了 v1.x 的 tokens 与 components。

**判 verdict 时**：
- 检查 v1.x 实现是否仍存在于当前代码（如 `web/src/styles/tokens.css` / `web/src/components/atoms/*` / `web/src/app/layout/Sidebar.tsx` 等）
- Verdict 仍按 4 档判（多数应是 Closed verified，因为代码还在）
- **Evidence 追加**注明 supersession context："实现仍在；视觉权威已从 v1.x 转向 v2-houfeng（v1.x 文档 archived to docs/_archive/）"

## Acceptance Criteria

- [ ] 17 行 Closed 全部 reassessed
- [ ] 0 个 `(⚠️ need-reassess)` 在 batch 4 范围内残留
- [ ] 2 个 Deferred 行（Page-level redesign + WCAG AA contrast）不动
- [ ] git diff 范围只在 `docs/release/v1-gap-checklist.md`

## Out of Scope

- 任何业务代码 / .trellis/spec/ / 已 archive 文档 / CLAUDE.md / README

## Final Confirmation

**Goal**: reassess 17 行 → 完成全部 42 行 reassess 工作流。
**Approach**: trellis-implement sub-agent。
**Plan**: PR1 = sub-agent reassess + edit；main commit (1 work + 1 trellis bookkeeping)。
