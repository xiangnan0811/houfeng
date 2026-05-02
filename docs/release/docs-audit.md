# 候风 V1 收口期 docs/ 审计报告

## 审计时间 / 任务

- 时间：2026-05-02
- 关联任务：`.trellis/tasks/05-02-docs-audit-cleanup` (T1)
- 决策来源：parent PRD `.trellis/tasks/05-02-docs-roadmap/prd.md`
- 上位约束：V1 收口（Stage 1）/ V1 ≠ MVP / 不重写文档内容（仅整理 + git mv）
- 物理策略（PRD §Decision Q3）：archive 类一律 `git mv` 到 `docs/_archive/<original-relative-path>`，镜像原目录树

## 分类汇总

| 类别 | 文件数 | 处置 |
|---|---|---|
| Keep (frozen / 业务权威) | 4 | architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow——v2 不动业务 |
| Keep (active / 视觉权威) | 2 | v2-houfeng 当前视觉权威（design-language + component-spec）|
| Keep (operations / deploy / release) | 5 | local-and-systemd + 2 service + v1-smoke-run + v1-gap-checklist |
| Rewrite (T2 实施) | 1 | v1-baseline/README.md（措辞过时，由 T2 与 CLAUDE.md 一并改写）|
| Archive | 82 | `git mv` → `docs/_archive/<mirror>`（superpowers 39 + v1.x-frontend-redesign 5 + v1-baseline 视觉/handoff/stitch 29 + operations 9）|
| **合计（原 docs/）** | **94** | |
| New (本任务产物) | 1 | `docs/release/docs-audit.md` |

> 注 1：v1-baseline/README.md 物理位置仍在原处（不 archive、不 rewrite），仅在本审计中登记为"等待 T2 改写"。
>
> 注 2：原 docs/ 文件数实测 94（不是 parent PRD inventory 估算的 ~64）。差异主要来自 superpowers/specs 19 份（PRD 估 18）/ superpowers/plans 20 份（PRD 估 18）/ stitch 25 份（含 1 份 obsidian_core/DESIGN.md 与 12 组 code.html+screen.png 对，对数实际为 12，并非 PRD 隐含的 9）。

## 详表（按原路径排序）

### docs/deploy/

| 原路径 | 新路径 | 分类 | 一行理由 |
|---|---|---|---|
| `docs/deploy/local-and-systemd.md` | （不变） | Keep (deploy) | canonical deployment recipe，CLAUDE.md / README.md 引用 |
| `docs/deploy/systemd/houfeng-agent.service` | （不变） | Keep (deploy) | 部署模版，runbook 必须 |
| `docs/deploy/systemd/houfeng-center.service` | （不变） | Keep (deploy) | 部署模版，runbook 必须 |

### docs/design/v1-baseline/

| 原路径 | 新路径 | 分类 | 一行理由 |
|---|---|---|---|
| `docs/design/v1-baseline/README.md` | （不变） | Rewrite (T2) | 措辞需改"V1 frozen 完成 → V1 仍在收口；视觉部分指向 v2"，由 T2 与 CLAUDE.md 一并实施 |
| `docs/design/v1-baseline/architecture-data-model.md` | （不变） | Keep (frozen) | 业务结构权威，v2 不动 |
| `docs/design/v1-baseline/rules-and-interaction.md` | （不变） | Keep (frozen) | 业务规则权威，v2 不动 |
| `docs/design/v1-baseline/tech-selection.md` | （不变） | Keep (frozen) | 技术选型权威，v2 不动 |
| `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md` | （不变） | Keep (frozen) | 业务交互原型 / 操作流，与 architecture / rules / tech-selection 同性质 |
| `docs/design/v1-baseline/ui-ux-spec.md` | `docs/_archive/design/v1-baseline/ui-ux-spec.md` | Archive | superseded by v2-houfeng（README 已加 banner） |
| `docs/design/v1-baseline/baseline-screens.md` | `docs/_archive/design/v1-baseline/baseline-screens.md` | Archive | superseded by v2-houfeng |
| `docs/design/v1-baseline/visual-review-round2.md` | `docs/_archive/design/v1-baseline/visual-review-round2.md` | Archive | superseded by v2-houfeng |
| `docs/design/v1-baseline/handoff.md` | `docs/_archive/design/v1-baseline/handoff.md` | Archive | 视觉部分已被 v2 替代；命名段已被 CLAUDE.md 覆盖；保留作历史记录 |
| `docs/design/v1-baseline/stitch/` (整目录 18 文件) | `docs/_archive/design/v1-baseline/stitch/` | Archive | 标记为 deprecated；与 v2-houfeng 视觉权威不再对齐 |

stitch/ 子目录展开（共 18 文件）：

| 原路径 | 新路径 |
|---|---|
| `docs/design/v1-baseline/stitch/fleet_control_plane_dashboard/code.html` | `docs/_archive/design/v1-baseline/stitch/fleet_control_plane_dashboard/code.html` |
| `docs/design/v1-baseline/stitch/fleet_control_plane_dashboard/screen.png` | `docs/_archive/design/v1-baseline/stitch/fleet_control_plane_dashboard/screen.png` |
| `docs/design/v1-baseline/stitch/fleet_nodes_list/code.html` | `docs/_archive/design/v1-baseline/stitch/fleet_nodes_list/code.html` |
| `docs/design/v1-baseline/stitch/fleet_nodes_list/screen.png` | `docs/_archive/design/v1-baseline/stitch/fleet_nodes_list/screen.png` |
| `docs/design/v1-baseline/stitch/global_app_shell_baseline_obsidian_core/code.html` | `docs/_archive/design/v1-baseline/stitch/global_app_shell_baseline_obsidian_core/code.html` |
| `docs/design/v1-baseline/stitch/global_app_shell_baseline_obsidian_core/screen.png` | `docs/_archive/design/v1-baseline/stitch/global_app_shell_baseline_obsidian_core/screen.png` |
| `docs/design/v1-baseline/stitch/global_control_center_unified/code.html` | `docs/_archive/design/v1-baseline/stitch/global_control_center_unified/code.html` |
| `docs/design/v1-baseline/stitch/global_control_center_unified/screen.png` | `docs/_archive/design/v1-baseline/stitch/global_control_center_unified/screen.png` |
| `docs/design/v1-baseline/stitch/global_logs_explorer/code.html` | `docs/_archive/design/v1-baseline/stitch/global_logs_explorer/code.html` |
| `docs/design/v1-baseline/stitch/global_logs_explorer/screen.png` | `docs/_archive/design/v1-baseline/stitch/global_logs_explorer/screen.png` |
| `docs/design/v1-baseline/stitch/node_detail_center_unified/code.html` | `docs/_archive/design/v1-baseline/stitch/node_detail_center_unified/code.html` |
| `docs/design/v1-baseline/stitch/node_detail_center_unified/screen.png` | `docs/_archive/design/v1-baseline/stitch/node_detail_center_unified/screen.png` |
| `docs/design/v1-baseline/stitch/node_details_nd_us_east_04a/code.html` | `docs/_archive/design/v1-baseline/stitch/node_details_nd_us_east_04a/code.html` |
| `docs/design/v1-baseline/stitch/node_details_nd_us_east_04a/screen.png` | `docs/_archive/design/v1-baseline/stitch/node_details_nd_us_east_04a/screen.png` |
| `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict/code.html` | `docs/_archive/design/v1-baseline/stitch/node_onboarding_binding_conflict/code.html` |
| `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict/screen.png` | `docs/_archive/design/v1-baseline/stitch/node_onboarding_binding_conflict/screen.png` |
| `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/code.html` | `docs/_archive/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/code.html` |
| `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/screen.png` | `docs/_archive/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/screen.png` |
| `docs/design/v1-baseline/stitch/obsidian_core/DESIGN.md` | `docs/_archive/design/v1-baseline/stitch/obsidian_core/DESIGN.md` |
| `docs/design/v1-baseline/stitch/security_audit_events/code.html` | `docs/_archive/design/v1-baseline/stitch/security_audit_events/code.html` |
| `docs/design/v1-baseline/stitch/security_audit_events/screen.png` | `docs/_archive/design/v1-baseline/stitch/security_audit_events/screen.png` |
| `docs/design/v1-baseline/stitch/system_configuration/code.html` | `docs/_archive/design/v1-baseline/stitch/system_configuration/code.html` |
| `docs/design/v1-baseline/stitch/system_configuration/screen.png` | `docs/_archive/design/v1-baseline/stitch/system_configuration/screen.png` |
| `docs/design/v1-baseline/stitch/target_details_blog.example.com/code.html` | `docs/_archive/design/v1-baseline/stitch/target_details_blog.example.com/code.html` |
| `docs/design/v1-baseline/stitch/target_details_blog.example.com/screen.png` | `docs/_archive/design/v1-baseline/stitch/target_details_blog.example.com/screen.png` |

> 实际 stitch 子目录有 25 个文件（12 组 code.html + screen.png 对 = 24 + 1 个 obsidian_core/DESIGN.md = 25）。本表以子目录代表的 12 个屏 + DESIGN.md = 13 行展示，每行同时移动 code.html 与 screen.png。

### docs/design/v1.x-frontend-redesign/

| 原路径 | 新路径 | 分类 | 一行理由 |
|---|---|---|---|
| `docs/design/v1.x-frontend-redesign/README.md` | `docs/_archive/design/v1.x-frontend-redesign/README.md` | Archive | README frontmatter 自标 `superseded-by-v2-houfeng` |
| `docs/design/v1.x-frontend-redesign/status.md` | `docs/_archive/design/v1.x-frontend-redesign/status.md` | Archive | 同上目录方向 |
| `docs/design/v1.x-frontend-redesign/plans/2026-04-29-plan-1-backend-auth.md` | `docs/_archive/design/v1.x-frontend-redesign/plans/2026-04-29-plan-1-backend-auth.md` | Archive | 同上目录方向 |
| `docs/design/v1.x-frontend-redesign/plans/2026-04-29-plan-2-frontend-foundation.md` | `docs/_archive/design/v1.x-frontend-redesign/plans/2026-04-29-plan-2-frontend-foundation.md` | Archive | 同上目录方向 |
| `docs/design/v1.x-frontend-redesign/plans/2026-04-29-plan-3-page-rewrites.md` | `docs/_archive/design/v1.x-frontend-redesign/plans/2026-04-29-plan-3-page-rewrites.md` | Archive | 同上目录方向 |

### docs/design/v2-houfeng/

| 原路径 | 新路径 | 分类 | 一行理由 |
|---|---|---|---|
| `docs/design/v2-houfeng/design-language.md` | （不变） | Keep (active) | 当前视觉权威，`web/src/styles/tokens.css` 引用 |
| `docs/design/v2-houfeng/component-spec.md` | （不变） | Keep (active) | 当前视觉权威 |

### docs/operations/

| 原路径 | 新路径 | 分类 | 一行理由 |
|---|---|---|---|
| `docs/operations/v1-smoke-run.md` | （不变） | Keep (operations) | 真实环境冒烟脚本（CLAUDE.md / README.md 引用） |
| `docs/operations/v1-visual-verification.md` | `docs/_archive/operations/v1-visual-verification.md` | Archive | 整文 80% 引用 v1-baseline/stitch/*.png；stitch archive 后此文严重脱节；v2 视觉证据流程不属于 V1 收口范围 |
| `docs/operations/visual-evidence/manifest.json` | `docs/_archive/operations/visual-evidence/manifest.json` | Archive | 与 stitch 强绑定；archive stitch 后该目录失效 |
| `docs/operations/visual-evidence/dashboard.png` | `docs/_archive/operations/visual-evidence/dashboard.png` | Archive | 同上 |
| `docs/operations/visual-evidence/events.png` | `docs/_archive/operations/visual-evidence/events.png` | Archive | 同上 |
| `docs/operations/visual-evidence/node-detail.png` | `docs/_archive/operations/visual-evidence/node-detail.png` | Archive | 同上 |
| `docs/operations/visual-evidence/node-onboarding.png` | `docs/_archive/operations/visual-evidence/node-onboarding.png` | Archive | 同上 |
| `docs/operations/visual-evidence/nodes.png` | `docs/_archive/operations/visual-evidence/nodes.png` | Archive | 同上 |
| `docs/operations/visual-evidence/settings.png` | `docs/_archive/operations/visual-evidence/settings.png` | Archive | 同上 |
| `docs/operations/visual-evidence/target-detail.png` | `docs/_archive/operations/visual-evidence/target-detail.png` | Archive | 同上 |

### docs/release/

| 原路径 | 新路径 | 分类 | 一行理由 |
|---|---|---|---|
| `docs/release/v1-gap-checklist.md` | （不变） | Keep (release, T3 大幅修订) | 80% 行写"Closed"但用户判定"实现连 V0.1 都不到"——T3 合并 12 条新素材时一并重审 |
| `docs/release/docs-audit.md` | （新增，本任务产物） | New | 本审计报告 |

### docs/superpowers/

整目录 → archive。共 39 个文件（plans 20 + specs 19）。

| 原目录 | 新目录 | 分类 | 一行理由 |
|---|---|---|---|
| `docs/superpowers/plans/` (20 files) | `docs/_archive/superpowers/plans/` | Archive | 已弃用工具（superpowers）的纯历史 plan，无运行时引用 |
| `docs/superpowers/specs/` (19 files) | `docs/_archive/superpowers/specs/` | Archive | 同上 |

详细 plan 文件列表（20 份，按文件名排序）：

- `2026-04-25-houfeng-v1-observability-surfaces.md`
- `2026-04-26-houfeng-node-onboarding-binding-state.md`
- `2026-04-26-houfeng-runtime-control-surfaces.md`
- `2026-04-26-houfeng-settings-global-control.md`
- `2026-04-27-houfeng-dashboard-empty-onboarding.md`
- `2026-04-27-houfeng-node-detail-binding-conflict.md`
- `2026-04-27-houfeng-node-lifecycle-retire-restore.md`
- `2026-04-27-houfeng-nodes-binding-conflict-filter.md`
- `2026-04-27-houfeng-object-metadata-editing.md`
- `2026-04-27-houfeng-probe-item-management.md`
- `2026-04-27-houfeng-settings-execution-integration.md`
- `2026-04-27-houfeng-stateful-confirmations.md`
- `2026-04-27-houfeng-target-probe-creation.md`
- `2026-04-28-houfeng-agent-reliability-closure.md`
- `2026-04-28-houfeng-dashboard-events-acceptance-closure.md`
- `2026-04-28-houfeng-retention-aggregation-execution.md`
- `2026-04-28-houfeng-runtime-semantics-correction.md`
- `2026-04-28-houfeng-trend-degradation-surfaces.md`
- `2026-04-29-houfeng-v1-delivery-verification.md`
- `2026-04-29-houfeng-v1-visual-language-alignment.md`

详细 spec 文件列表（19 份，按文件名排序）：

- `2026-04-25-houfeng-v1-observability-surfaces-design.md`
- `2026-04-26-houfeng-node-onboarding-binding-state-design.md`
- `2026-04-26-houfeng-runtime-control-surfaces-design.md`
- `2026-04-26-houfeng-settings-global-control-design.md`
- `2026-04-27-houfeng-dashboard-empty-onboarding-design.md`
- `2026-04-27-houfeng-node-detail-binding-conflict-design.md`
- `2026-04-27-houfeng-node-lifecycle-retire-restore-design.md`
- `2026-04-27-houfeng-nodes-binding-conflict-filter-design.md`
- `2026-04-27-houfeng-object-metadata-editing-design.md`
- `2026-04-27-houfeng-probe-item-management-design.md`
- `2026-04-27-houfeng-settings-execution-integration-design.md`
- `2026-04-27-houfeng-stateful-confirmations-design.md`
- `2026-04-27-houfeng-target-probe-creation-design.md`
- `2026-04-28-houfeng-dashboard-events-acceptance-closure-design.md`
- `2026-04-28-houfeng-retention-aggregation-execution-design.md`
- `2026-04-28-houfeng-trend-degradation-surfaces-design.md`
- `2026-04-28-houfeng-v1-completion-sequencing-design.md`
- `2026-04-29-houfeng-v1-delivery-verification-design.md`
- `2026-04-29-houfeng-v1-visual-language-alignment-design.md`

实测 `git status --porcelain | grep '^R' | wc -l = 82`，与本审计的 archive 总数一致。

## 活引用清查

清查范围：仓内 `*.md / *.go / *.ts / *.tsx / *.json / *.yaml / *.yml / Makefile / *.css / *.html`，命中字符串：
`docs/superpowers` / `docs/design/v1.x-frontend-redesign` / `docs/design/v1-baseline/{ui-ux-spec, baseline-screens, visual-review-round2, handoff, stitch}` / `docs/operations/{v1-visual-verification, visual-evidence}`。

清查命令（参考）：

```bash
grep -rn "docs/superpowers\|docs/design/v1.x-frontend-redesign\|docs/design/v1-baseline/ui-ux-spec\|docs/design/v1-baseline/baseline-screens\|docs/design/v1-baseline/visual-review-round2\|docs/design/v1-baseline/handoff\|docs/design/v1-baseline/stitch\|docs/operations/v1-visual-verification\|docs/operations/visual-evidence" \
  --include='*.md' --include='*.go' --include='*.ts' --include='*.tsx' \
  --include='*.json' --include='*.yaml' --include='*.yml' \
  --include='*.css' --include='*.html' --include='Makefile' .
```

### A 类（已知断链，可接受 / T3 处理 / 已 archive 内部互引）

| 文件 | 行 | 引用 |
|---|---|---|
| `docs/operations/v1-smoke-run.md` | 238 | `docs/operations/v1-visual-verification.md` |
| `docs/release/v1-gap-checklist.md` | 55 | `docs/operations/v1-visual-verification.md` |
| `docs/release/v1-gap-checklist.md` | 56 | `docs/operations/visual-evidence/`, `docs/operations/visual-evidence/manifest.json` |
| `docs/release/v1-gap-checklist.md` | 87 | `docs/design/v1-baseline/ui-ux-spec.md` |
| `docs/release/v1-gap-checklist.md` | 89 | `docs/design/v1.x-frontend-redesign/` |
| `docs/release/v1-gap-checklist.md` | 104 | `docs/operations/visual-evidence/` |
| `docs/release/v1-gap-checklist.md` | 122 | `docs/design/v1-baseline/ui-ux-spec.md`, `docs/design/v1.x-frontend-redesign/` |
| `docs/design/v2-houfeng/design-language.md` | 5–6 | frontmatter `supersedes:` 列出 archived 路径——**保留是正确的**（语义上记录"我取代了什么"）|

archive 内部互引（path 子树整体迁移，相对引用结构保留）：

| 文件 | 备注 |
|---|---|
| `docs/operations/visual-evidence/manifest.json` 内 7 处 `reference` 字段 | 指向 `docs/design/v1-baseline/stitch/...` ——两者均 archive 到 `docs/_archive/...` 下，绝对路径仍可在新位置定位 |
| `docs/operations/v1-visual-verification.md` 内多处引用 stitch 截图 | 同上 |
| `docs/design/v1-baseline/handoff.md` 内多处引用 stitch / ui-ux-spec / visual-review-round2 / baseline-screens | 同上 |
| `docs/design/v1-baseline/baseline-screens.md` 第 3 行 | 自指 `docs/design/v1-baseline/stitch/baseline-screens.md` ——同 archive |
| `docs/design/v1.x-frontend-redesign/{README.md, status.md, plans/*}` 多处互引 | 整目录 archive，相对引用保留 |

### B 类（已知断链，T2 修订）

| 文件 | 行 | 引用 |
|---|---|---|
| `CLAUDE.md` | 105 | `docs/design/v1-baseline/baseline-screens.md`, `docs/design/v1-baseline/ui-ux-spec.md` |
| `CLAUDE.md` | 112 | `docs/operations/v1-visual-verification.md`, `docs/operations/visual-evidence/` |
| `CLAUDE.md` | 116 | `docs/operations/visual-evidence/` |
| `README.md` (repo root) | 17 | `docs/design/v1-baseline/ui-ux-spec.md` |
| `README.md` (repo root) | 18 | `docs/design/v1-baseline/visual-review-round2.md` |
| `README.md` (repo root) | 20 | `docs/design/v1-baseline/handoff.md` |
| `README.md` (repo root) | 21 | `docs/design/v1-baseline/baseline-screens.md` |
| `README.md` (repo root) | 26 | "Visual authority: V1.x frontend redesign at `docs/design/v1.x-frontend-redesign/`" |
| `README.md` (repo root) | 49 | `docs/operations/v1-visual-verification.md` |

> **新增 T2 工作建议**：`README.md` 在原 parent PRD 中只隐式提及"修订 CLAUDE.md"。本审计发现 repo 根 `README.md` 同样含大量已 archive 路径引用 + 已过时的"V1.x 视觉权威"措辞——T2 应同时修订 `README.md`，保持与 CLAUDE.md / v2-houfeng 视觉方向一致。

### C 类（已知断链，T3 修订）

| 文件 | 行 | 引用 |
|---|---|---|
| `.trellis/spec/web/component-conventions.md` | 9 | `docs/design/v1-baseline/baseline-screens.md`, `docs/design/v1-baseline/ui-ux-spec.md` |
| `.trellis/spec/web/styling-guidelines.md` | 25 | `docs/design/v1-baseline/baseline-screens.md` |
| `.trellis/spec/web/styling-guidelines.md` | 26 | `docs/design/v1-baseline/ui-ux-spec.md` |
| `.trellis/spec/web/styling-guidelines.md` | 33 | `docs/design/v1-baseline/`, `docs/operations/visual-evidence/` |
| `.trellis/spec/web/quality-guidelines.md` | 163 | `docs/operations/v1-visual-verification.md`, `docs/operations/visual-evidence/` |
| `.trellis/spec/web/quality-guidelines.md` | 211 | `docs/operations/v1-visual-verification.md`, `docs/operations/visual-evidence/`, `docs/design/v1-baseline/` |
| `.trellis/spec/backend/quality-guidelines.md` | 162 | `docs/operations/v1-visual-verification.md`, `docs/operations/visual-evidence/`, `docs/design/v1-baseline/` |

### D 类（意外活引用，需停下）

**0 条**。无意外活引用。

`.trellis/tasks/05-02-docs-roadmap/prd.md` 与 `.trellis/tasks/05-02-docs-audit-cleanup/prd.md` 内对 archive 路径的引用属于本 task family 的元数据，预期、保留、不计入断链。

## 后续建议

- **T2** 工作扩充：除 CLAUDE.md 外，repo 根 `README.md` 也需同步修订（去掉"V1.x frontend redesign"过时视觉权威；从"Frozen V1 baseline package"列表中删除已 archive 的 5 份视觉/handoff 文件，保留 architecture / rules / tech-selection / interactive-prototype / README；移除/改写"Visual verification record" 引用）
- **T3** 工作：合并 12 条新 gap 素材到 `docs/release/v1-gap-checklist.md`，重审 80% Closed 状态，并修 `.trellis/spec/{web,backend}/*.md` 7 处视觉/截图引用（指向 v2-houfeng 或新流程）
- **deferred follow-up**：在某个时点为 `docs/_archive/` 加 `README.md` 索引，说明各子树的来源 task / 归档原因 / 替代位置——本任务不做（PRD 已标 deferred）

## 执行记录

### git mv 实际操作清单

执行命令逐条记录（按顺序执行）：

```bash
# 创建 archive 顶级目录
mkdir -p docs/_archive

# 1. superpowers 整目录
git mv docs/superpowers docs/_archive/superpowers

# 2. v1.x-frontend-redesign 整目录
mkdir -p docs/_archive/design
git mv docs/design/v1.x-frontend-redesign docs/_archive/design/v1.x-frontend-redesign

# 3. v1-baseline 视觉 4 份 + stitch 整目录
mkdir -p docs/_archive/design/v1-baseline
git mv docs/design/v1-baseline/ui-ux-spec.md          docs/_archive/design/v1-baseline/ui-ux-spec.md
git mv docs/design/v1-baseline/baseline-screens.md    docs/_archive/design/v1-baseline/baseline-screens.md
git mv docs/design/v1-baseline/visual-review-round2.md docs/_archive/design/v1-baseline/visual-review-round2.md
git mv docs/design/v1-baseline/handoff.md             docs/_archive/design/v1-baseline/handoff.md
git mv docs/design/v1-baseline/stitch                 docs/_archive/design/v1-baseline/stitch

# 4. operations 视觉验证 + visual-evidence
mkdir -p docs/_archive/operations
git mv docs/operations/v1-visual-verification.md docs/_archive/operations/v1-visual-verification.md
git mv docs/operations/visual-evidence           docs/_archive/operations/visual-evidence
```

### 执行后 `git status` 验证（实测）

```
$ git status --porcelain | wc -l
87

$ git status --porcelain | awk '{print $1}' | sort | uniq -c
   5 ??
  82 R
```

| 状态 | 数量 | 内容 |
|---|---|---|
| `R ` (rename) | 82 | archive git mv 全部命中 rename detection |
| `?? ` (untracked) | 5 | `docs/release/docs-audit.md`（本审计） + 4 个 `.trellis/tasks/05-02-*` 任务目录（pre-existing，非本任务产物） |
| `M ` (modified) | 0 | **未触碰任何 keep / rewrite 类文件**（CLAUDE.md / README.md / .trellis/spec/ / docs/release/v1-gap-checklist.md 全部未改动）|

archive 文件总数实测：

```
$ find docs/_archive -type f | wc -l
82
```

与 `R` 行数一致，验证通过。

## Acceptance Criteria 自检

- [x] `docs/release/docs-audit.md` 列出原 docs/ 下每一份文件，每行包含原路径 / 分类 / 新路径（如适用）/ 一行理由
- [x] 所有 archive 操作通过 `git mv` 执行（git log --follow 可追溯）
- [x] 5 份 parent PRD "待评估"文件全部分类完成（handoff.md / interactive-prototype-and-operation-flow.md / v1-visual-verification.md / visual-evidence/manifest.json / v1.x-frontend-redesign/{README,status,plans/*}）
- [x] D 类活引用为 0
- [x] B/C 类断链全部分类登记
- [ ] commit 由 main agent 在 Phase 3.4 主导（sub-agent 不 commit）
