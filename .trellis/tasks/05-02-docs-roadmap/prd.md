# Audit docs/ and plan next phase

## Goal

把 `docs/` 与 `CLAUDE.md` 拉回到匹配"项目仍在 pre-MVP 早期阶段"的真实状态，并起草下一阶段开发计划。

**驱动信号**：用户判定"当前项目甚至连 MVP 都不算"，与文档中大量"V1 frozen / 已完成"声明严重冲突。docs/ 累积了 60+ 份文件、~30K 行，混杂了：
- 真权威（V1 结构/数据模型/规则）
- 已 superseded 的视觉/重设计文档
- 已弃用工具（superpowers）的历史产物
- 部署 / 验证 / gap-checklist 等运维资产

需要一次审计把信号噪声分开，并基于真实代码现状起草下阶段路线。

## What I already know

### 仓库 docs 现状（实证）

| 路径 | 文件数 | 总行数 | 当前定位（实证） |
|---|---|---|---|
| `docs/superpowers/plans/` | 18 | ~10K | 已弃用工具产物（superpowers），命名 `2026-04-XX-houfeng-*` |
| `docs/superpowers/specs/` | 18 | ~3K | 同上 |
| `docs/design/v1-baseline/architecture-data-model.md` | 1 | 406 | **frozen authoritative**（v2 不动业务） |
| `docs/design/v1-baseline/rules-and-interaction.md` | 1 | 1139 | **frozen authoritative** |
| `docs/design/v1-baseline/tech-selection.md` | 1 | 546 | **frozen authoritative** |
| `docs/design/v1-baseline/ui-ux-spec.md` / `baseline-screens.md` / `visual-review-round2.md` / `stitch/*` | 4 | ~880 | **superseded by v2** |
| `docs/design/v1-baseline/handoff.md` | 1 | 515 | 待评估（旧交付指南） |
| `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md` | 1 | 2559 | 待评估（巨型，可能 superseded） |
| `docs/design/v1-baseline/README.md` | 1 | 215 | 已加 supersede 注 |
| `docs/design/v1.x-frontend-redesign/` | 5 | ~6K | **整目录 superseded**（含 README + status + 3 plan） |
| `docs/design/v2-houfeng/` | 2 | ~600 | **active 视觉权威**（design-language.md + component-spec.md） |
| `docs/operations/v1-smoke-run.md` | 1 | 242 | 保留（V1 真实环境冒烟脚本） |
| `docs/operations/v1-visual-verification.md` | 1 | 101 | 待评估（视觉对比，可能要换成 v2） |
| `docs/operations/visual-evidence/manifest.json` | 1 | (json) | 待评估 |
| `docs/deploy/local-and-systemd.md` + 2 service | 3 | 191 + service | 保留（canonical） |
| `docs/release/v1-gap-checklist.md` | 1 | 126 | 保留 + 待更新（accumulated 12 新 gap） |

### 已识别的强候选清理

- **整目录归档/删除**：
  - `docs/superpowers/`（plans + specs，已弃用工具）
  - `docs/design/v1.x-frontend-redesign/`（已被 v2-houfeng 取代）
  - `docs/design/v1-baseline/stitch/`（deprecated）
- **整目录改写**：
  - `docs/design/v1-baseline/` 中的视觉相关 4 份（superseded by v2）
- **整目录保留**：
  - `docs/deploy/`、`docs/operations/`、`docs/release/`
  - `docs/design/v1-baseline/` 中的 architecture / rules / tech-selection 三份
  - `docs/design/v2-houfeng/`

### CLAUDE.md 需要修正的点

- "Visual authority: only the Unified / Baseline Stitch screens documented in `baseline-screens.md` and `ui-ux-spec.md`" → 应改为 v2-houfeng
- "Project identity" 段说 "V1 product, interaction, and visual design are **frozen** in `docs/design/v1-baseline/`" → 视觉部分已 unfrozen → v2

### .trellis/spec/ 的连带影响

- 上一个任务（00-bootstrap-guidelines）写的 11 份 spec 顶部都引用了 `docs/design/v1-baseline/`，但其中视觉相关引用（如 `web/styling-guidelines.md` 的视觉权威段）实际应指向 v2-houfeng

## Decision (ADR-lite) — Q1: 方向定位

**Context**: docs 全段说 "V1 frozen / 已完成"，用户判定"实现连 V0.1 都不够"且"我心目中的 MVP 比 v1-baseline 范围更大"。两个判断必须收敛才能写 roadmap。

**Decision**: 选 Option A — **收口 V1**。
- v1-baseline 的业务范围（architecture-data-model / rules-and-interaction / tech-selection）维持不变
- v2-houfeng 作为视觉权威（已落地）
- 下阶段 = 修 gap + 补冒烟 + 补测试 + 把已有 8 页"做对"，让 V1 真正可交付

**Critical correction**：V1 ≠ MVP。
- 用户心目中的 MVP **大于** v1-baseline 范围
- "V1 收口完成" ≠ "MVP 达成"
- docs 里**禁止**把 V1 等同于 MVP；roadmap 必须明确分层：
  1. **Stage 1（current focus）**: V1 收口
  2. **Stage 2（post-V1）**: 评估 V1 → MVP 之间的范围扩张（具体内容本任务不深入）
  3. **Stage 3+**: 远期演进

**Consequences**:
- v1-baseline 的 architecture / rules / tech-selection 三份保留 frozen，但 README / status 类文件需修订"V1 = 完成"措辞
- v1-baseline 的视觉 4 份（ui-ux-spec / baseline-screens / visual-review-round2 / stitch）→ archive
- v1.x-frontend-redesign 整目录 → archive
- superpowers 整目录 → archive（与方向无关，独立判定）
- 不在本任务实施任何业务代码改动；本任务仅整理 docs / spec / CLAUDE.md + 起草 roadmap

## Assumptions (待用户确认)

1. v2-houfeng 是当前视觉方向、不再推翻
2. superpowers/ 目录内容**没有**任何代码运行时引用（应是纯历史 plan 文档），可整体归档
3. 用户接受"V1 收口"作为下阶段的明确名词（不替换为其他术语）

## Open Questions (Blocking / Preference)

按 brainstorm 规则，**一次问一个**。已答：

- ✅ Q1 方向定位 → Option A（V1 收口，且 V1 ≠ MVP）

待问：

- ✅ Q2 输出形态 → Option B（拆 3 子任务）

**剩余未答问题被下放到对应 child task 的 brainstorm 阶段处理**：

- Q3（superpowers/ + v1.x-frontend-redesign/ 处置策略）→ 在 **T1 child** 内 brainstorm
- Q4（CLAUDE.md 修订幅度）→ 在 **T2 child** 内 brainstorm

## Requirements (evolving)

基于 Q1 决定的 baseline requirements：

- 起草一份 **roadmap / next-phase 文档**，明确 3 层 stage（V1 收口 / post-V1 → MVP / 远期）
- **审计**整个 `docs/` 目录，每份文件标记 keep / rewrite / archive / delete
- **修订** `CLAUDE.md`，去掉"V1 frozen / 已完成"措辞，更新视觉权威指向 v2
- **修订** `.trellis/spec/` 11 份文件的"权威来源"段（视觉部分指向 v2-houfeng）
- **合并**累积的 12 条 gap-checklist 素材到 `docs/release/v1-gap-checklist.md`
- **archive** superpowers/ + v1.x-frontend-redesign/ + v1-baseline 视觉 4 份 + v1-baseline/stitch/
- 全程使用 `git mv` / `git rm` 留痕，不物理删除

## Acceptance Criteria (evolving)

- [ ] `docs/` 审计完成：每份现存文件被明确标记为 keep / rewrite / archive / delete
- [ ] `CLAUDE.md` 修订到匹配真实代码与 v2-houfeng 视觉权威
- [ ] 下阶段开发计划文档落地（路径待定，如 `docs/release/next-phase-plan.md`）
- [ ] 已积累的 12 条 gap-checklist 素材合并入 `docs/release/v1-gap-checklist.md`
- [ ] `.trellis/spec/` 中 11 份 spec 的"权威来源"段更新（视觉部分指向 v2-houfeng）

## Definition of Done

- 所有 docs 改动通过 `git mv` / `git rm` 留痕，不直接物理删除
- 新增的 next-phase-plan.md 与 CLAUDE.md 互相引用
- 本任务交付物 commit 后可独立 review（reviewer 能从 commit + diff 读懂方向）

## Out of Scope (explicit)

- **不实施**任何业务代码改动（只动 docs / spec / CLAUDE.md）
- 不重写 v2-houfeng 的视觉规范
- 不做 V1 业务的 scope 删减实施（只在 plan 里建议）
- 已 archive 的任务（00-bootstrap-guidelines）不动

## Technical Notes

- `docs/design/v1.x-frontend-redesign/README.md` 顶部 frontmatter 已自标 `status: superseded-by-v2-houfeng`
- `docs/design/v1-baseline/README.md` 已加 supersede banner
- `docs/design/v2-houfeng/design-language.md` 顶部 frontmatter 自标 `status: active`，`supersedes` 列出 v1-baseline/ui-ux-spec + v1.x-frontend-redesign
- `web/src/styles/tokens.css` 实际已经按 v2 落地

## Implementation Plan (3 child tasks)

本任务 = parent / coordination 任务，自身不输出代码或文档。所有实施落到 3 个 child task。

| Slug | Title | 依赖 | 输出物 |
|---|---|---|---|
| `T1: docs-audit-cleanup` | Audit and clean up docs/ | 无 | `docs/release/docs-audit.md` + `git mv` archive 操作 |
| `T2: roadmap-and-claude-md` | Draft roadmap & revise CLAUDE.md | **blocked by T1** | `docs/release/next-phase-plan.md` + 修订后的 `CLAUDE.md` |
| `T3: spec-sync` | Sync spec visual authority + gap-checklist | 可与 T1/T2 并行 | 11 份 `.trellis/spec/*.md` 的"权威来源"段修订 + `docs/release/v1-gap-checklist.md` 合并 12 条新素材 |

**依赖说明**：
- T2 必须等 T1 的 archive 完成，roadmap 才知道现存 docs 的最终结构
- T3 不依赖前两者：spec 修订 + gap-checklist 合并对 docs/ 整理结果不敏感
- 推荐执行顺序：**T1 → (T2 + T3 并行) → parent archive**

**Parent task 完成判定**：3 个 child 全部 archive 后，本 parent 也可 archive。Parent 自身不需要 `task.py start`，不需要 jsonl curation——它没有要派 sub-agent 的实施工作。

## Final Confirmation

**Goal**: 把 docs/ + CLAUDE.md + .trellis/spec/ 拉回到匹配"V1 仍在收口、V1 ≠ MVP"的真实状态，并起草分层 roadmap。

**Approach**: parent 协调 + 3 child 实施（B 方案），T2 blockedBy T1，T3 可并行。

**Stage 1 (current focus)** = V1 收口；Stage 2 = post-V1 → MVP；Stage 3+ = 远期。文档**禁止**把 V1 等同于 MVP。
