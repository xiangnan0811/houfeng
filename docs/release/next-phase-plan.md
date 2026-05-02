# 候风 / Houfeng Fleet Control Plane — 下一阶段开发计划

> **关联文档**：`docs/release/docs-audit.md`（docs 审计与 archive 决策）+ `docs/release/v1-gap-checklist.md`（gap 清单与 V1 release gate）+ `CLAUDE.md`（AI session 项目入口）+ `docs/design/v1-baseline/`（V1 业务结构 frozen 子集）+ `docs/design/v2-houfeng/`（视觉权威）+ `docs/operations/v1-smoke-run.md`（真实环境冒烟脚本）。
>
> **状态判定**（2026-05-02）：用户判定当前实现"连 V0.1 都不到"；V1 是当前阶段的收口期目标，且 **V1 ≠ MVP**——用户心目中的 MVP 范围比 v1-baseline 更大。本文档作为 V1 收口期间的统一指引，明确"现在做什么 / 何时触发下一阶段 / 哪些事确定不做"。

## 阶段框架

| Stage | 内容 | 当前状态 |
|---|---|---|
| **Stage 1** | V1 收口（current focus） | 进行中 |
| **Stage 2** | post-V1 → MVP | 占位（trigger 后 brainstorm） |
| **Stage 3+** | 远期 | 占位（多用户/OAuth/移动端等明确不在 roadmap） |

文档不把 V1 等同于 MVP；不在 V1 收口期讨论 Stage 2/3 的具体范围。

## Stage 1: V1 收口（current focus）

**目标**：把 v1-baseline frozen 子集真正做对，让 V1 业务功能在真实环境可冒烟交付，并把"V1 release gate"从字面通过升级为现场验证通过。

**业务范围**（不变）：v1-baseline frozen 子集 4 份——`architecture-data-model.md` + `rules-and-interaction.md` + `tech-selection.md` + `interactive-prototype-and-operation-flow.md`。不扩范围；不引入新探针类型 / 新对象模型 / 新通知通道 / 新存储后端。

**视觉**：v2-houfeng（`design-language.md` + `component-spec.md`），已落地，权威不变。

### Stage 1 工作项（按优先级 P0/P1/P2）

#### P0（阻塞 V1 收口）

- **Front-end list-page filter completion** (root cause of user judgment 实现连 V0.1 都不到)：补齐 3 个 list page 的筛选功能
  - NodesPage：补 §6.3 的 5 项缺失筛选（生命周期 / 供应商 / 地区 / 标签 / 健康，仅"仅看异常 / 运行状态" 2 toggle 已就位）
  - TargetsPage：从零补齐 §6.4 的 6 项筛选条（当前列表是只读表）
  - EventsPage：补 §10.9 的剩余筛选（含 backfill boolean / 时间 segmented / 时间分组 / 加载更早分页）
  - 拆 3 个 follow-up task 推进

- **重审 gap-checklist 42 个 Closed 行的真实状态**
  - 当前 Closed 标记带 `(⚠️ need-reassess)`，需逐行现场验证（跑相关代码 + 看是否真的满足设计意图，不是字面 import 通过即算 Closed）
  - 建议拆 1-2 个独立 follow-up task，按 area（产品/对象模型/运行时/UI/通知/交付/Auth/V1.x 视觉）分批
  - 关联：`docs/release/v1-gap-checklist.md` 全表 + 顶部 banner

- **解决 12 条新 gap 中的 P0 项**
  - **gap #12**：`make verify-web` 加 `npm run lint`（CI 当前抓不到 lint 失败的潜在风险，改造成本极低）
  - **gap #3**：约定下次 migration 序号从 0011 起（0004 撞车文件**不动**——`schema_migrations` 用文件名作主键，rename 会破坏已部署环境；约定已落入 `.trellis/spec/backend/database-guidelines.md`）
  - **gap #7**：`cmd/houfeng-center/main.go` stdlib `"log"` → `slog`（与全仓 `slog` 一致）

- **真实环境冒烟 V1 完整路径**
  - 路径：Node 接入 → Target 创建 → ProbeItem → 异常发生 → 通知 → 恢复
  - 脚本：`docs/operations/v1-smoke-run.md`（已记录 2026-04-29 一次跑通；本阶段需要在每次 P0 修完后追加新一次）
  - Telegram 真发：开 bot token 后跑一遍异常 → 恢复，证据进入 smoke-run 文档

#### P1（V1 收口质量门）

- **解决 12 条新 gap 中的 P1 项**
  - **gap #9**：双 fetch wrapper 合并（`web/src/lib/fetcher.ts` + `web/src/lib/api.ts` → 单一封装，统一 401 处理）
  - **gap #10**：`pages/NodesPage.tsx:60` `createNode` 重构进 `lib/api.ts`（消灭裸 `fetch('/api/nodes')` 反模式）
  - **gap #4**：修正 `0010_add_users_and_sessions.sql` 索引命名（`sessions_user_idx` / `sessions_expires_idx` → `idx_sessions_user` / `idx_sessions_expires`，与其他迁移一致）

- **CLAUDE.md 文档断层修补**
  - 已在 T2（本任务）落地；后续若发现新断层，按 gap-checklist 流程登记

- **长 page 文件初步拆分**（gap #11 的子集）
  - 选 1-2 个最大的拆（`TargetDetailPage` 1731 / `NodeDetailPage` 1138）
  - 其他延后到 P2 或下一阶段；拆分目标是可读性 + 可测性，不是为了上抽象层

- **Telegram 通知真实环境验证**
  - 当前 gap-checklist `Live Telegram delivery evidence` = Partial
  - 跑通后转 Closed 并附证据路径

#### P2（V1 收口可推迟）

- **gap #6**：`agentapi.ProbeKind` 与 CLAUDE.md 描述统一（决定 `https` 是独立常量还是 http + 配置位；当前实际是后者）
- **长 page 文件全部拆分**（`SettingsPage` 873 / `TargetsPage` 740 / `NodesPage` 671）
- **gap #8**：`web/src/components/atoms/` 子目录在 CLAUDE.md / spec 中显式登记
- **视觉证据 (visual-evidence) 在 v2 视觉下重抓**
  - 之前的 stitch 截图与 `docs/operations/visual-evidence/` 已 archive
  - v2 视觉证据收集流程未定，可与 V1 收口分离独立做

### Stage 1 完成判定（V1 release gate）

参考 `docs/release/v1-gap-checklist.md` 末尾 "Final V1 release gate" 段，**外加**本阶段补充：

- gap-checklist 42 Closed 行重审完成（按 area 分批；P0 优先）
- 12 条新 gap 中 P0 + P1 全部 closed
- 真实环境冒烟通过且记录在 `docs/operations/v1-smoke-run.md`（含 Telegram 真发证据）
- `go test ./...` / `./scripts/verify.sh` / `cd web && npm run build` 全绿
- v2 视觉证据：可选；若延后则在 release notes 中显式注明

完成上述判定后，可以打 V1 tag，并触发 Stage 2 brainstorm task。

## Stage 2: post-V1 → MVP

**占位**。具体范围在 Stage 1 完成后单独 brainstorm，不在 V1 收口期讨论。

**Trigger condition**：Stage 1 全部 P0 + P1 closed → 触发 Stage 2 brainstorm task（创建于 `.trellis/tasks/`）。

**已知方向起点**（仅作 trigger 后讨论的种子，不锁定）：
- 用户判定 MVP 比 V1 范围大；具体功能扩展待 brainstorm
- 候选方向（来自 v1-baseline 自身的"intentionally out of scope"段，与本阶段无关）：
  - 通用脚本执行 / 远程操作面（v1 明确划在外）
  - Docker / 容器编排（v1 明确划在外）
  - 复杂规则引擎（v1 强调"统一规则集中在中心，不下放 agent"）
  - 多目标分组与批量操作 / 模板化探针（v1 强调单用户、低密度配置面）
- 是否引入新存储后端（TSDB？）/ 新通知通道（邮件 / 飞书 / 企微？）属于 Stage 2 brainstorm 决议，**不在 V1 收口期预埋**

**Out of scope（即使 MVP 也不做）**：参考 `docs/design/v2-houfeng/design-language.md` 的"避免反例"段与"不做的事（约束）"段；本阶段不复述。

## Stage 3+: 远期

**占位**。当前**不在 roadmap**：

- 多用户 / 角色 / 权限 / 协作（数据库 schema 可留口，但 UI 不暴露）
- OAuth / SSO / 双因素认证
- 操作审计日志面板
- 主题包市场 / 第三方主题 / 字体上传 / 用户自定义字体
- 大屏 / 移动端独立布局（v2 视觉规范已声明响应式只到平板宽度）
- 国际化（运行时语言切换；v2 明确"不引入英文界面 — 中文为主"）
- 图表库（recharts / echarts / ECharts）—— v2 明确 sparkline 用纯 SVG
- 节点列表的地图视图 / 拓扑视图

（来源：`docs/design/v2-houfeng/design-language.md` §1.2 与 §12；与 v2 自身约束保持一致）

任何上述方向若日后被重新评估，必须先以独立 brainstorm task 提出，并记录到本 roadmap 的 Stage 3 段。

## 引用关系

- **本文档** ←→ `docs/release/v1-gap-checklist.md`：gap-checklist 是 V1 收口工作项的事实清单；本文档把它组织成 P0/P1/P2 + release gate
- **本文档** ← `docs/release/docs-audit.md`：审计报告决定哪些 docs 是权威；本文档基于审计后的 docs 清单组织 roadmap
- **本文档** ← `CLAUDE.md`：AI session 入口指向本文档作为"下阶段权威"
- **本文档** ← `README.md`：repo 根入口在 V1 verification artifacts 段引用本文档
- **本文档** ↔ `docs/design/v1-baseline/{architecture-data-model, rules-and-interaction, tech-selection, interactive-prototype-and-operation-flow}.md`：V1 业务 frozen 子集，本文档不重述、不修改
- **本文档** ↔ `docs/design/v2-houfeng/{design-language, component-spec}.md`：v2 视觉权威，本文档不修改、不复述其约束
- **本文档** ↔ `docs/operations/v1-smoke-run.md`：真实环境冒烟脚本，本文档把它指定为 V1 release gate 的核心证据

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

## 变更日志

| 日期 | 变更 |
|---|---|
| 2026-05-02 | 初版，由 T2 (`.trellis/tasks/05-02-roadmap-and-claude-md`) 起草。关联 T1 (docs-audit) + T3 (spec-sync) 已落地的成果。Stage 1 详细 + Stage 2/3 占位 + trigger condition。 |
