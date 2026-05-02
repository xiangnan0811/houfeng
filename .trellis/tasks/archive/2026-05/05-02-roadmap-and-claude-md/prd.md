# T2: Draft next-phase roadmap and revise CLAUDE.md

> Child of `.trellis/tasks/05-02-docs-roadmap/`. Parent PRD locked: V1 收口 / V1≠MVP / 拆 3 child.
> T1 ✅（82 archive + `docs/release/docs-audit.md`）。T3 ✅（spec authority 对齐 + 12 新 gap 入 gap-checklist + 42 Closed 加 need-reassess）。
> T2 是 docs-roadmap workstream 的最后一块。

## Goal

把 docs/ 整理 + spec sync 的成果落到三份"对外面"的文档：

1. `docs/release/next-phase-plan.md` —— 下一阶段开发计划（Stage 1/2/3 框架，**新建**）
2. `CLAUDE.md` —— 让 AI session 看到的项目入口（**修订过时点**）
3. `README.md` —— repo 根入口（**修订 6 处 stale refs**）
4. `docs/design/v1-baseline/README.md` —— v1-baseline 总入口（**改"V1 frozen 完成"措辞**）

## What I already know

### 累积的 ground truth（T1 + T3 已落地）

- **现存 docs 清单**：`docs/release/docs-audit.md`（13 keep + 82 archive + 3 rewrite）
- **gap 清单**：`docs/release/v1-gap-checklist.md`（42 Closed 行带 need-reassess + 末尾 12 条新 gap + 顶部状态 banner）
- **spec 权威**：11 份 `.trellis/spec/*` 已统一指向 `CLAUDE.md > v1-baseline frozen 子集 > v2-houfeng`
- **方向决策**：V1 收口（Stage 1）/ V1 ≠ MVP（Stage 2）/ 远期（Stage 3）

### CLAUDE.md 待修订清单（实读出 8-10 处）

| 行 | 当前内容 | 需改为 |
|---|---|---|
| 7 | "V1 product, interaction, and visual design are **frozen** in `docs/design/v1-baseline/`" | "V1 业务结构 frozen 在 v1-baseline 子集；视觉部分已 unfrozen，权威指向 v2-houfeng；V1 实现仍在收口期，未达 MVP" |
| 50 | "workers (`incidentSvc`, `retentionWorker`)" | "workers (`incidentSvc`, `retentionWorker`, `sessionCleanup`)"（实际 3 个，gap #5） |
| 69 | handler 清单缺 `auth.go`、`metadata.go` | 补上（gap #1） |
| 77 | 子包清单未提 `auth/` | 加 `auth/`（gap #2）+ 提 `http/middleware.go`（RequireSession） |
| 82 | "Probe kinds in V1 are exactly `tcp`, `http`/`https`, `tls`" | 改为"agentapi.ProbeKind 常量只有 `tcp`/`http`/`tls`；https 走 http + 配置区分"（gap #6） |
| 105 | "Visual authority: only the **Unified / Baseline Stitch** screens documented in `docs/design/v1-baseline/baseline-screens.md` and `ui-ux-spec.md`" | 改为指向 `docs/design/v2-houfeng/{design-language, component-spec}.md` |
| 111-114 | V1 verification artifacts 段引用 `v1-visual-verification.md` + `visual-evidence/` | 这两个已 archive，移除引用（或注明 archive 位置） |
| 116 | "copy/adjust evidence into `docs/operations/visual-evidence/`" | visual-evidence 已 archive，这条流程不再适用，需删/改 |

附加：可在某处提及 `next-phase-plan.md` 作为"下阶段权威"入口。

### README.md 待修订清单（6 处 stale refs）

| 行 | 引用 | 处理 |
|---|---|---|
| 17 | `docs/design/v1-baseline/ui-ux-spec.md` | 已 archive，移除/改指向 v2-houfeng |
| 18 | `docs/design/v1-baseline/visual-review-round2.md` | 已 archive，移除 |
| 20 | `docs/design/v1-baseline/handoff.md` | 已 archive，移除 |
| 21 | `docs/design/v1-baseline/baseline-screens.md` | 已 archive，移除 |
| 26 | "V1.x frontend redesign at `docs/design/v1.x-frontend-redesign/`" + "earlier Unified / Baseline Stitch screens under `docs/design/v1-baseline/`" | v1.x 已 archive，改指向 v2-houfeng |
| 49 | `docs/operations/v1-visual-verification.md` | 已 archive，移除 |

附加：README 整体对"V1 频繁提 frozen / 已完成"的措辞需要软化（与 CLAUDE.md 同步）。

### v1-baseline/README.md 待修订清单

- 顶部 banner（"VISUAL PORTION SUPERSEDED"）已是 OK，**保留**
- Line 35 段："**当前结论：V1 结构层与视觉层均已冻结，整体设计包已完成。**" → 改为："V1 结构层冻结；视觉层已被 v2-houfeng 取代；V1 实现仍在收口期，未达 MVP（详见 `docs/release/next-phase-plan.md`）"
- 文档导航段（line 73-83）引用 obsidian wiki 链接（如 `[[服务器舰队控制面-v1-视觉设计系统与页面规范]]`）—— 那些视觉文档已 archive，wiki link 失效；建议附 archive 路径或移除

### next-phase-plan.md 起草思路（待 Q4 答完锁定）

- **Stage 1 = V1 收口**
  - 重审 gap-checklist 42 个 Closed 行（独立 follow-up）
  - 解决末尾 12 条新 gap（按优先级排序）
  - 真实环境冒烟（`docs/operations/v1-smoke-run.md` 已记录 2026-04-29 一次，可继续追加）
  - V1 release gate（已在 gap-checklist 末尾列）
- **Stage 2 = post-V1 → MVP**
  - 用户判定 MVP 比 V1 范围大；**本任务不深入定义具体范围**，只做占位 + 触发条件（V1 收口完成后再开 brainstorm）
- **Stage 3+ = 远期**
  - 占位（多用户、OAuth、移动端等，明确"目前不在 roadmap"）

## Assumptions

1. T2 不重写 v2-houfeng 视觉规范、不动业务代码、不改业务规则
2. roadmap 起草不深入 Stage 2/3 具体范围（Stage 2 由独立 brainstorm 任务再展开）
3. CLAUDE.md / README 的修订策略由 Q4 决定（minimal vs targeted vs full）

## Decision (ADR-lite)

### Q4: CLAUDE.md / README 修订幅度

**Context**: 8-10 处 stale + "V1 frozen 完成" 措辞与现实割裂。

**Decision**: 选 Option B — **Targeted rewrite**。
- **重写 3 个核心段**（CLAUDE.md）：Project identity / Visual authority / V1 verification artifacts
- **重写 3 个核心段**（README.md）：Frozen V1 baseline package / Guardrails / Verification artifacts
- 其他段做 **minimal patch**（worker 数、handler/子包清单、ProbeKind 等点状修订）
- v1-baseline/README.md：仅改 Line 35 的"已冻结 / 已完成"措辞 + 文档导航段加注 archive 路径

**Consequences**:
- diff 中等大小，需仔细 review
- 措辞从此与"V1 收口未完成 / V1 ≠ MVP / v2-houfeng 视觉权威"对齐
- Project identity / Visual authority 段是 sub-agent 重写重点（注意保留语调风格）

### Q4.2: v1-baseline/README 文档导航段（自决）

obsidian wiki link 段保留，附加注："（部分文档已 archive 至 `docs/_archive/design/v1-baseline/`，wiki link 仅作历史索引）"。理由：少冒险、明示 archive 位置、保留历史可追溯。

## Open Questions

- ✅ Q4 修订幅度 → Option B (targeted rewrite)
- ✅ Q4.1 roadmap 粒度 → Option B (中等：stage + 工作项 + 优先级 P0/P1/P2，不带估时/依赖；Stage 2/3 占位 + trigger condition)
- ✅ Q4.2 wiki link 处理 → 保留 + 加注（自决）

## Requirements

1. 起草 `docs/release/next-phase-plan.md`（~200-280 行）：
   - 顶部说明文档定位（V1 收口 / V1≠MVP / 关联 gap-checklist + docs-audit）
   - **Stage 1: V1 收口** - 工作项 + P0/P1/P2 优先级，关联 gap-checklist 条目
   - **Stage 2: post-V1 → MVP** - 占位 + trigger condition（如 Stage 1 全部 P0 closed → 触发 brainstorm）
   - **Stage 3+: 远期** - 占位（多用户/OAuth/移动端等明确"目前不在 roadmap"）
   - 与 v1-gap-checklist.md / docs-audit.md 互相引用
2. 修订 `CLAUDE.md`：
   - **重写** 3 段：Project identity / Visual authority / V1 verification artifacts
   - **minimal patch** 其他 8 处具体行（worker 数、handler 清单 +auth.go/metadata.go、子包 +auth/、ProbeKind 描述、archived 文档引用移除）
3. 修订 `README.md`：
   - **重写** 3 段：Frozen V1 baseline package / Guardrails (Visual authority) / Verification artifacts
   - 移除 6 处 archived 引用，改指向 v2-houfeng / 其他 keep 类文档
4. 修订 `docs/design/v1-baseline/README.md`：
   - Line 35 段："V1 结构层冻结；视觉层已被 v2-houfeng 取代；V1 实现仍在收口期，未达 MVP（详见 next-phase-plan.md）"
   - 文档导航段加注："（部分文档已 archive 至 docs/_archive/design/v1-baseline/，wiki link 仅作历史索引）"
5. 不动业务代码 / .trellis/spec/ / 已 archive 的文档 / v2-houfeng/ / docs/operations/v1-smoke-run.md / docs/deploy/

## Acceptance Criteria

- [ ] `docs/release/next-phase-plan.md` 落地，Stage 1/2/3 都有内容（Stage 1 详细，Stage 2/3 占位）
- [ ] roadmap 与 v1-gap-checklist.md / docs-audit.md 互相引用
- [ ] CLAUDE.md 3 段重写 + 8 处 minimal patch 全部完成
- [ ] CLAUDE.md grep 不再命中 archived 路径（除 docs-audit.md 引用）
- [ ] README.md 3 段重写 + 6 处 stale refs 清除（grep 验证）
- [ ] v1-baseline/README.md Line 35 措辞改 + 文档导航段加注
- [ ] CLAUDE.md / README.md / next-phase-plan.md 三处 "V1 收口未完成 / V1 ≠ MVP / v2-houfeng 视觉权威" 表述一致
- [ ] git diff 范围只在：CLAUDE.md / README.md / docs/release/next-phase-plan.md / docs/design/v1-baseline/README.md

## Final Confirmation

**Goal**: 起草 next-phase-plan.md（中等粒度 Stage 1/2/3）+ targeted rewrite CLAUDE.md / README + minimal touch v1-baseline/README，让对外面文档与 V1 收口现状对齐。

**Approach**: 一个 trellis-implement sub-agent 一次完成 4 份文档；先起草 roadmap（最具创造性），再用 roadmap 框架写其他 3 份的引用，保持术语一致。

**Implementation Plan**:
1. PR1: sub-agent 起草 `docs/release/next-phase-plan.md`
2. PR2: sub-agent targeted rewrite CLAUDE.md（3 段重写 + 8 处 minimal patch）
3. PR3: sub-agent targeted rewrite README.md（3 段重写 + 6 处 stale refs 清除）
4. PR4: sub-agent minimal patch v1-baseline/README.md
5. main agent 在 Phase 3.4 commit（一个或两个 commit，取决于产出大小）

**Sub-agent 不能做**：
- 修改 .trellis/spec/ / 业务代码 / 已 archive 文档 / v2-houfeng/ / 其他 keep 类 docs（smoke-run, deploy, v1-baseline frozen 业务三份）
- git commit
- 跑 task.py
- 改 prd.md
- 重写 CLAUDE.md/README 中"非核心 3 段"的整体结构（minimal patch only on those）

## Requirements (evolving)

待 Q4 答完填充。

## Acceptance Criteria (evolving)

- [ ] `docs/release/next-phase-plan.md` 落地（含 Stage 1/2/3）
- [ ] `CLAUDE.md` 8-10 处 stale 修订
- [ ] `README.md` 6 处 stale refs 修订
- [ ] `docs/design/v1-baseline/README.md` "V1 frozen 完成" 措辞修订
- [ ] CLAUDE.md / README / next-phase-plan.md 三处对 "V1≠MVP" 表述一致
- [ ] 不动业务代码 / .trellis/spec/ / 任何已 archive 的文档

## Definition of Done

- 三份修订/新建的文档自洽（视觉权威 / V1 完成度 / Stage 分层 表述一致）
- next-phase-plan.md 与 v1-gap-checklist.md 互相引用
- commit 清晰可 review

## Out of Scope

- 起草 Stage 2 (MVP) 具体范围 / Stage 3 远期具体内容（占位即可）
- 修任何业务代码 / .trellis/spec/ / 已 archive 的文档
- 重写 v2-houfeng 视觉规范
- 对 gap-checklist 42 Closed 行做现场验证（独立 follow-up）
