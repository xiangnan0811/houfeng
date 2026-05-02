# T3: Sync spec visual authority and gap-checklist

> Child of `.trellis/tasks/05-02-docs-roadmap/`. Parent PRD has Stage 1/2/3 framing
> and locked Option A (V1 收口) + Option B (拆 3 child). T1 已完成（82 文件 archive）。

## Goal

把 `.trellis/spec/*` 11 份文件的"权威来源"段修正为 v2-houfeng（视觉部分）+ v1-baseline 保留部分（结构/规则），并把 bootstrap-guidelines 任务期间累积的 12 条 gap 素材合并进 `docs/release/v1-gap-checklist.md`，同时对其 80% Closed 状态行做"重新评估"标记。

## What I already know

### 11 份 spec 文件清单（来自 .trellis/spec/）

- backend (5+1)：`backend/index.md` + `backend/{directory-structure, database-guidelines, error-handling, logging-guidelines, quality-guidelines}.md`
- web (5+1)：`web/index.md` + `web/{directory-structure, component-conventions, state-and-data, styling-guidelines, quality-guidelines}.md`
- guides 2 份（thinking guides，trellis 自带，本任务不动）

每份顶部都有一句类似：
> **权威来源**：本文件规则来自 `CLAUDE.md` + `docs/design/v1-baseline/`，如有冲突以前者为准。

需要按"该 spec 是结构/规则类还是视觉类"分别修：
- **结构/规则类**（多数 backend）→ 仍指向 v1-baseline frozen 三份（architecture / rules / tech-selection / interactive-prototype）
- **视觉类**（web 的 styling-guidelines 等）→ 指向 v2-houfeng

### 12 条 gap 素材（来自 bootstrap-guidelines task 的 sub-agent 累积报告）

**Backend 侧（7 条）**：
1. CLAUDE.md handler 清单缺 `auth.go`、`metadata.go`，但代码里都有（`router.go:35-69` 注册 `/api/auth/*` 4 路由）
2. CLAUDE.md 子包清单未提 `internal/center/auth/`（用户/会话/cookie/cleanup worker，配 0010 migration）
3. `db/migrations/` 0004 序号撞车（`0004_add_node_onboarding_binding_state.sql` + `0004_add_observation_provenance.sql`）；migrate.Apply 字典序处理仍 OK，但下一序号应从 0011 起
4. `0010_add_users_and_sessions.sql` 索引命名（`sessions_user_idx`）不遵循其他迁移普遍 `idx_<table>_<purpose>` 写法
5. bootstrap 实际 wire 了 **3 个 worker**（含 `sessionCleanup`），CLAUDE.md 只列 2 个；`bootstrap_test.go:152` 已断言 `len(workers)==3`
6. `agentapi.ProbeKind*` 只有 `tcp/http/tls` 三常量（`https` 走 http+配置区分），但 CLAUDE.md 列了 4 种
7. `cmd/houfeng-center/main.go` 仍用 stdlib `"log"`，与全仓 `slog` 不一致（历史遗留）

**Web 侧（5 条）**：
8. `web/src/components/atoms/` 子目录 CLAUDE.md 未提（事实上的设计系统原子落点）
9. `web/src/lib/` 并存 `fetcher.ts`（auth）+ `api.ts`（业务）双 fetch 包装 + 双 401 钩子（历史遗留）
10. `pages/NodesPage.tsx:60` `createNode` 直接 `fetch('/api/nodes')` 绕 `lib/api.ts`（已识别反模式）
11. 多 page > 1000 行：`TargetDetailPage` 1731 / `NodeDetailPage` 1138 / `SettingsPage` 873 / `TargetsPage` 740 / `NodesPage` 671（技术债）
12. **`make verify-web` 不跑 `npm run lint`** — lint 失败 CI 抓不到，潜在风险

### gap-checklist 当前现状

- 126 行，结构：Product/architecture / Core object model / Runtime behavior / UI / Notifications / Delivery & ops / Authentication (V1.x) / V1.x visual baseline / Final V1 release gate / V2 取代记录
- 大量行写 "Closed" 状态——但用户判定"实现连 V0.1 都不到"，意味着许多 Closed 行**实际状态待重审**

## Assumptions

1. T3 不重写代码，只改 markdown 文件
2. 重审 Closed 行**不要求**对每个 Closed 行做现场验证（那是巨大工作，需独立 task）；T3 只在文档顶部加"⚠️ 状态评估说明"，并把 Closed 标记加上"need-reassess"
3. T3 与 T2 并行——T2 改 CLAUDE.md/README/v1-baseline-README 时也会引用 v2-houfeng，文风/术语需保持一致

## Decision (ADR-lite)

### Q5: gap-checklist Closed 行重审策略

**Context**: gap-checklist ~30+ 行写 "Closed"，与用户 2026-05-02 判定"实现连 V0.1 都不到"严重 mismatch。

**Decision**: 选 Option A — **每个 Closed 行后加 `(⚠️ need-reassess)` 标记 + 顶部加状态说明段**。
- 不删任何行；Closed 值保留（便于 git diff 对比）
- 顶部说明：V1 收口未完成；下方 Closed 状态截至 2026-04-30，与 2026-05-02 用户判定冲突；全部 Closed 行加 need-reassess 标记；逐行现场验证推到独立 follow-up

**Consequences**:
- 文档稍啰嗦（每个 Closed 行多一个标记），但风险一目了然
- 留逐行现场验证给 T2 roadmap（作为下阶段工作项）/ 或独立 follow-up
- 不武断推翻具体行——sub-agent 不做"该行是不是真完成"的判断

### "权威来源"段格式（自决）

11 份 spec 用**统一一句**（不按文件类型分别），格式：

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

理由：sub-agent 不需根据文件类型推断；CLAUDE.md > v1-baseline frozen 子集 > v2-houfeng 三层冲突解决顺序明确。

## Requirements

1. 把 11 份 spec 文件（5 backend + 5 web + 1 web/index.md）的"权威来源"段全部替换为上面统一格式
2. 把 12 条新 gap 素材分类合并进 `docs/release/v1-gap-checklist.md` 现有章节（Backend 7 条到 Product/architecture 或 Runtime；Web 5 条到 UI 或 Delivery）
3. gap-checklist 顶部加"V1 收口未完成 + Closed 状态待重审"说明段
4. 现有所有 Closed 状态行后加 `(⚠️ need-reassess)` 标记
5. T3 不改 CLAUDE.md / README / 任何 keep 类 docs / 业务代码（守 Out of Scope）

## Acceptance Criteria

- [ ] 11 份 spec 的"权威来源"段全部统一为新格式（grep 验证）
- [ ] 12 条 gap 素材全部并入 gap-checklist，每条命中现有合理章节
- [ ] gap-checklist 顶部"状态说明段"出现
- [ ] 所有现存 Closed 行加 `(⚠️ need-reassess)`，新合并的 12 条 gap **不要**加该标记（它们是新发现，状态不是"Closed"）
- [ ] git diff 范围只在 `.trellis/spec/*.md` 和 `docs/release/v1-gap-checklist.md`
- [ ] 不修改 CLAUDE.md、README、其他 docs、业务代码

## Final Confirmation

**Goal**: 修 11 份 spec 视觉权威指向（v1-baseline → v2-houfeng）+ 合并 12 条新 gap + 标记现有 Closed 行 need-reassess。

**Approach**: 一个 trellis-implement sub-agent 一次完成。grep 验证修改范围。

**Implementation Plan**:
1. PR1: sub-agent 替换 11 份 spec 顶部"权威来源"段为新统一格式
2. PR2: sub-agent 把 12 条 gap 分类合并进 gap-checklist，加顶部状态说明段，给所有现存 Closed 行加 `(⚠️ need-reassess)`
3. main agent 在 Phase 3.4 commit（一个或两个 commit，看实际产出）

**Sub-agent 不能做**：
- 修改 spec 内容主体（只改顶部"权威来源"那一段）
- 改 gap-checklist 的具体技术内容（只加标记 + 合并新 gap + 加顶部说明）
- 对 Closed 行做"是不是真完成"的判断（统一加 need-reassess，不挑）
- 改 CLAUDE.md / README / 其他 docs / 业务代码
- git commit
- 跑 task.py

## Definition of Done

- 11 spec 修订后内容自洽（视觉类不再引用已 archive 的 v1-baseline 视觉文件）
- gap-checklist 经审视后"V1 收口需求"清晰浮现，作为 T2 起草 roadmap 时的 input
- commit 清晰可 review

## Out of Scope

- 起草 next-phase plan（T2 范围）
- 改 CLAUDE.md / README / v1-baseline/README.md（T2 范围）
- 对 Closed 行做现场代码验证（巨大独立 task）
- 改 spec 内容本身（仅改"权威来源"那一行）
