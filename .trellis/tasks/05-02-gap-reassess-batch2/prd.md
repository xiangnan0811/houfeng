# Reassess gap-checklist Closed status — batch 2 (Runtime + Notifications)

> Sister task to archived `.trellis/tasks/archive/2026-05/05-02-gap-reassess-batch1`.
> 复用 batch 1 同套 4-tier verdict scheme + Status-column-update + Evidence-append 格式。

## Goal

对 batch 2 范围内 8 个 `Closed (⚠️ need-reassess)` 行做现场代码验证，给每行打新 verdict，更新 `docs/release/v1-gap-checklist.md`。

## Scope

**Runtime behavior**（6 行 Closed）：
1. `Node enrollment and binding state` — `internal/center/enrollment`, `web/src/pages/NodeOnboardingPage.tsx`
2. `Agent durable sync buffer` — `agent/syncqueue`, `agent/runtime/runtime.go`
3. `Node pause/maintenance/retire sync semantics` — `internal/center/store/agent_plan.go`, runtime control tests
4. `Target pause/maintenance/archive semantics` — `internal/center/http/handlers/runtime_controls.go`, target page tests
5. `Retention and daily aggregation execution` — `internal/center/retention`, `internal/center/store/retention.go`
6. `Trend degradation incident families` — `internal/center/incidents/evaluator.go`

**Notifications**（2 行 Closed；第 3 行 Live Telegram 已是 Partial，**跳过**）：
7. `Telegram notifier implementation` — `internal/center/notify/telegram.go`
8. `Settings-aware notification policy` — `internal/center/incidents/service.go`, settings tests

## Verdict scheme (same as batch 1)

4 档：`Closed (verified 2026-05-02)` / `Partial (was Closed)` / `Not Closed (was Closed)` / `Reassess inconclusive`。
Status 列改值；Evidence 列追加 `**Reassessed 2026-05-02**: <finding>`；drop `(⚠️ need-reassess)`。

## Acceptance Criteria

- [ ] 8 行 Closed 全部 reassessed，verdict + finding 落地
- [ ] 0 个行（Runtime 6 行 + Notifications 前 2 行）仍带 `(⚠️ need-reassess)`
- [ ] Live Telegram delivery evidence (Partial) 不动
- [ ] 后续段（UI / Delivery / Auth / V1.x visual）保留 need-reassess（留给 batch 3/4）
- [ ] git diff 范围只在 `docs/release/v1-gap-checklist.md`

## Out of Scope

- batch 3/4 的剩余 reassess
- 对 Partial / Not Closed 行做现场修复（仅记录 verdict）
- 改业务代码 / .trellis/spec/ / 其他 docs / CLAUDE.md / README

## Final Confirmation

**Goal**: reassess 8 行 Closed → 4 档 verdict + Evidence 追加 finding。

**Approach**: 一个 trellis-implement sub-agent 完成，与 batch 1 工作流一致。

**Implementation Plan**:
- PR1: sub-agent 跑 reassess + edit
- main agent Phase 3.4 commit (1 work + 1 trellis bookkeeping)
