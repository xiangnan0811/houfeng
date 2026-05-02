# T1: Audit and clean up docs/

> Child of `.trellis/tasks/05-02-docs-roadmap/`. Parent PRD has Stage 1/2/3 framing
> and locked Option A (V1 收口) + Option B (拆 3 child). 不在本 PRD 重复，按需引用。

## Goal

完成 `docs/` 目录的全量审计：每份现存文件被明确分类为 keep / rewrite / archive / delete，
并对 archive 类文件执行 `git mv`（保留 git 历史，不物理删除）。
输出审计报告 `docs/release/docs-audit.md`，作为 T2 起草 roadmap 时的"现存 docs 清单"权威。

## What I already know

### docs/ 完整 inventory（来自 parent PRD 的 Step-1 探查）

详见 parent PRD `## What I already know → 仓库 docs 现状`。本 PRD 不复制，只列**T1 待决/已决分类**。

### 分类初判（来自 parent PRD + 本任务 Step-1 探查）

| 路径 | 分类 | 理由 |
|---|---|---|
| `docs/superpowers/plans/` (18 files) | **archive** | 已弃用工具产物（superpowers），纯历史 plan，无运行时引用 |
| `docs/superpowers/specs/` (18 files) | **archive** | 同上 |
| `docs/design/v1.x-frontend-redesign/` (5 files) | **archive** | README frontmatter 自标 `superseded-by-v2-houfeng` |
| `docs/design/v1-baseline/ui-ux-spec.md` | **archive** | superseded by v2-houfeng（README 已加 banner） |
| `docs/design/v1-baseline/baseline-screens.md` | **archive** | 同上 |
| `docs/design/v1-baseline/visual-review-round2.md` | **archive** | 同上 |
| `docs/design/v1-baseline/stitch/` | **archive** | 标记为 deprecated |
| `docs/design/v1-baseline/architecture-data-model.md` | **keep** (frozen) | 业务结构权威，v2 不动 |
| `docs/design/v1-baseline/rules-and-interaction.md` | **keep** (frozen) | 业务规则权威，v2 不动 |
| `docs/design/v1-baseline/tech-selection.md` | **keep** (frozen) | 技术选型权威，v2 不动 |
| `docs/design/v1-baseline/README.md` | **rewrite** | 措辞需从"V1 frozen 完成"改成"V1 仍在收口；视觉部分指向 v2"——但**rewrite 实施落到 T2**（与 CLAUDE.md 一起改）|
| `docs/design/v1-baseline/handoff.md` | **archive** | 内容是 v1 设计交付/Stitch 分层方法论，**视觉部分已被 v2-houfeng 替代**；命名段已被 CLAUDE.md 覆盖；保留作历史记录 |
| `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md` | **keep** (frozen) | 业务交互原型 / 操作流，与 architecture / rules / tech-selection 同性质（v2 不动业务），仍权威 |
| `docs/design/v2-houfeng/design-language.md` | **keep** (active) | 当前视觉权威 |
| `docs/design/v2-houfeng/component-spec.md` | **keep** (active) | 当前视觉权威 |
| `docs/operations/v1-smoke-run.md` | **keep** | 真实环境冒烟脚本（CLAUDE.md 引用） |
| `docs/operations/v1-visual-verification.md` | **archive** | 整文 80% 引用 v1-baseline/stitch/*.png 做"pixel 验收"；stitch 整目录 archive 后此文严重脱节；v2 视觉证据流程不属于 V1 收口范围（post-V1 工作） |
| `docs/operations/visual-evidence/` (含 manifest.json) | **archive** | 同上，所有 png 对照的是已 archive 的 stitch 参照 |
| `docs/deploy/local-and-systemd.md` + 2 service | **keep** | canonical deployment recipe |
| `docs/release/v1-gap-checklist.md` | **keep + 大幅修订（T3）** | 80% 行写"Closed"但用户判定"实现连 V0.1 都不到"——状态严重 mismatch；T3 合并 12 条新素材时一并重审过时 Closed 行 |

### Step-1 探查结论

5 份待评估文件全部分类完成（已并入上表）。关键发现：

- **`docs/release/v1-gap-checklist.md` 的 Closed 状态严重过时**：80% 行写 Closed，但用户判定"实现连 V0.1 都不到"。T3 合并新素材时必须重审。本任务（T1）不动其内容。
- **`docs/operations/visual-evidence/` 整目录与 stitch 强绑定**：archive stitch 后该目录失效，应一并 archive。
- **`interactive-prototype-and-operation-flow.md` 是业务规则文档**（与 architecture / rules / tech-selection 同级），不是视觉文档，不能 archive，需 keep frozen。

### 已发现的活引用风险（archive 前需 grep 验证）

- `CLAUDE.md` 引用 `docs/design/v1-baseline/baseline-screens.md` / `ui-ux-spec.md`（视觉权威表述，已过时，T2 修订）
- `docs/operations/v1-smoke-run.md` 可能引用其他 v1-baseline 文件（本任务前需 grep 检查）
- `docs/release/v1-gap-checklist.md` 大量行引用 stitch 截图、ui-ux-spec、handoff（archive 后链接断；接受现状，T3 修订时清理）
- `web/src/styles/tokens.css` 已知引用 `docs/design/v2-houfeng/design-language.md`（活，正确）
- `.trellis/spec/*.md` 11 份引用 `docs/design/v1-baseline/`（视觉部分需修订，T3 范围）

## Assumptions

1. `docs/superpowers/` 与 `docs/design/v1.x-frontend-redesign/` 的内容**没有**任何代码或别处文档活引用（archive 后不会断链）——上线前需 grep 验证
2. v1-baseline 三份 frozen 文档（architecture / rules / tech-selection）的内容仍与代码现状对齐——若不对齐由 T2 在 roadmap 里标注、不在 T1 内修改
3. T1 不重写任何文档内容；rewrite 类（如 v1-baseline/README.md）只在审计报告里登记，由 T2 实施

## Decision (ADR-lite) — Q3: archive 物理策略

**Context**: 48 个文件需 archive。物理删除 vs 仓内 archive vs 迁出仓库。

**Decision**: 选 Option B — **`git mv` 到 `docs/_archive/<original-relative-path>`**。
- 单一归档入口，**镜像原目录树**保结构对称
- 下划线前缀让 ls 排序靠末，明确"非主流内容"
- 例：`docs/superpowers/plans/X.md` → `docs/_archive/superpowers/plans/X.md`
- 例：`docs/design/v1-baseline/ui-ux-spec.md` → `docs/_archive/design/v1-baseline/ui-ux-spec.md`
- 例：`docs/operations/visual-evidence/dashboard.png` → `docs/_archive/operations/visual-evidence/dashboard.png`

**Consequences**:
- 仓体积不变（git history 已含字节）
- 早期项目阶段回头查设计决策快（vscode/ripgrep 仍可搜）
- 与 trellis 自家 `.trellis/tasks/archive/<year-month>/` 风格类似
- 镜像原目录结构 → 不引入"按时间分桶"约定（避免后续 archive 决策疲劳）

## Expansion Sweep 结论

| 类别 | 结论 |
|---|---|
| **Future evolution** | v2 某天可能被 v3 取代——archive 路径用"镜像原树"而非"按时间分桶"，长期稳定 |
| **Related scenarios** | trellis task archive 走 `<year-month>/` 是因任务短期；docs archive 走"镜像原树"是因事件性，**两种风格合理共存** |
| **Failure / edge cases** | (1) grep 活引用若发现意外，**停止 archive，记进报告，由用户决定**；(2) markdown 相对链接 archive 后断——T1 不修复，audit 报告标注，T2 起草 roadmap 时统一处理；(3) `visual-evidence/manifest.json` 内含相对路径，archive 后路径子结构保持，内部引用仍指向同 archive 内邻居文件，不需要改 |

## Requirements (evolving)

- 完成全量 docs/ 审计分类（含 5 份待评估文件）
- 对 archive 类文件执行 `git mv`（按 Q3 决定的物理策略）
- 输出 `docs/release/docs-audit.md`：每份文件的最终分类 + 处置位置 + 一行理由
- 审计报告本身入 commit
- archive 操作前 `grep -r` 验证无活引用（如有，记进审计报告"已知断链风险"段）

## Acceptance Criteria

- [ ] `docs/release/docs-audit.md` 列出原 docs/ 下每一份文件，每行包含：原路径 / 分类 / 新路径（如适用）/ 一行理由
- [ ] 所有 archive 操作通过 `git mv` 执行（git log --follow 可追溯）
- [ ] 5 份待评估文件全部分类完成
- [ ] archive 后 `grep -r "docs/superpowers\|docs/design/v1.x-frontend-redesign\|docs/design/v1-baseline/ui-ux-spec\|docs/design/v1-baseline/baseline-screens\|docs/design/v1-baseline/visual-review-round2\|docs/design/v1-baseline/stitch" --include='*.md' --include='*.go' --include='*.ts' --include='*.tsx' --include='*.json' --include='*.yaml' --include='*.yml' --include='Makefile'` 在仓库其他文件中不返回意外结果（`docs/release/docs-audit.md` 自身的引用是预期）
- [ ] commit 信息清晰：1 个 archive commit + 1 个审计报告 commit（或合并）

## Definition of Done

- 所有 archive 类文件按 Q3 决定的位置安放
- 审计报告通过同伴 review（自审 + 用户最终 ok）
- 不修改任何业务代码
- 不修改 `.trellis/spec/` 内容（T3 范围）
- 不修改 `CLAUDE.md`（T2 范围）
- 不起草 roadmap（T2 范围）

## Out of Scope

- 重写任何 keep / rewrite 类文档的内容（T2 处理）
- 修改 `.trellis/spec/` 的"权威来源"段（T3 处理）
- 合并 gap-checklist 12 条新素材（T3 处理）
- archive 后的"历史索引"建立（deferred，可作为单独 follow-up）

## Final Confirmation

**Goal**: 全量 docs/ 审计 + 对 48 个 archive 类文件执行 `git mv` 到 `docs/_archive/<mirror>/` + 输出 `docs/release/docs-audit.md`。

**Approach**: 一个 child task 完成。先 grep 活引用、再 git mv、再写 audit 报告。

**Implementation Plan**:

1. **PR1 (audit 报告 draft)**：sub-agent 写 `docs/release/docs-audit.md` 草稿（含完整 48 archive + 10 keep + 3 rewrite 表 + grep 验证结果），main agent review 后批准
2. **PR2 (执行 git mv)**：批量 `git mv` 操作（按"镜像原树"路径），中间如发现意外活引用就停下记报告
3. **PR3 (audit 报告定稿 + commit)**：填实 archive 实际路径、断链情况、grep 结果，与 git mv 操作合一个或两个 commit

**Sub-agent 不能做**：
- 修改 archive 目录之外的任何文件（不动 CLAUDE.md、不改 .trellis/spec/、不改 docs/release/v1-gap-checklist.md）
- 物理删除任何文件
- git commit（main agent 在 Phase 3.4 主导）

## Technical Notes

- `git mv` 保留 rename detection；review 时 `git log --follow` 可追溯到原路径
- `docs/superpowers/` 整目录约 21 MB（按 13K 行 + 4 png 估），git mv 后 working tree 移动，不真删字节
- archive 路径：`docs/_archive/<original-relative-path>`，镜像原目录树
- grep 验证范围：仓内全部 `*.md / *.go / *.ts / *.tsx / *.json / *.yaml / *.yml / Makefile`，命中 `docs/superpowers` 或 `docs/design/v1.x-frontend-redesign` 或 `docs/design/v1-baseline/{ui-ux-spec,baseline-screens,visual-review-round2,handoff,stitch}` 或 `docs/operations/{v1-visual-verification,visual-evidence}`
