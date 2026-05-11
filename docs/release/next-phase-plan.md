# 候风 / Houfeng Fleet Control Plane — 下一阶段开发计划

> **关联文档**：`docs/release/docs-audit.md`（docs 审计与 archive 决策）+ `docs/release/v1-gap-checklist.md`（gap 清单与 V1 release gate）+ `docs/release/current-state-and-next-stage-plan.md`（当前剩余工作审计与下一阶段规划）+ `CLAUDE.md`（AI session 项目入口）+ `docs/design/v1-baseline/`（V1 业务结构 frozen 子集）+ `docs/design/v2-houfeng/`（视觉权威）+ `docs/operations/v1-smoke-run.md`（真实环境冒烟脚本）。
>
> **状态判定**（2026-05-02）：用户判定当前实现"连 V0.1 都不到"；V1 是当前阶段的收口期目标，且 **V1 ≠ MVP**——用户心目中的 MVP 范围比 v1-baseline 更大。本文档作为 V1 收口期间的统一指引，明确"现在做什么 / 何时触发下一阶段 / 哪些事确定不做"。

## 阶段框架

| Stage | 内容 | 当前状态 |
|---|---|---|
| **Stage 1** | V1 收口 | 已完成 release gate 判定 |
| **Stage 2** | post-V1 → MVP | Asset Ledger 计划已闭合到当前边界；下一步需重新选入口 |
| **Stage 3+** | 远期 | 占位（多用户/OAuth/移动端等明确不在 roadmap） |

文档不把 V1 等同于 MVP；不在 V1 收口期讨论 Stage 2/3 的具体范围。

## Stage 1: V1 收口（current focus）

**目标**：把 v1-baseline frozen 子集真正做对，让 V1 业务功能在真实环境可冒烟交付，并把"V1 release gate"从字面通过升级为现场验证通过。

**业务范围**（不变）：v1-baseline frozen 子集 4 份——`architecture-data-model.md` + `rules-and-interaction.md` + `tech-selection.md` + `interactive-prototype-and-operation-flow.md`。不扩范围；不引入新探针类型 / 新对象模型 / 新通知通道 / 新存储后端。

**视觉**：v2-houfeng（`design-language.md` + `component-spec.md`），已落地，权威不变。

### Stage 1 工作项（按优先级 P0/P1/P2）

#### P0（阻塞 V1 收口）

**✅ Stage 1 P0 全部完成 (2026-05-03)**

- ✅ **Front-end list-page filter completion** (root cause of user judgment 实现连 V0.1 都不到)：补齐 3 个 list page 的筛选功能
  - ✅ NodesPage：补 §6.3 的 5 项缺失筛选（生命周期 / 供应商 / 地区 / 标签 / 健康，仅"仅看异常 / 运行状态" 2 toggle 已就位）—— commit `7cbf8d6`
  - ✅ TargetsPage：从零补齐 §6.4 的 6 项筛选条（当前列表是只读表）—— commit `43af18b`
  - ✅ EventsPage：补 §10.9 的剩余筛选（含 backfill boolean / 时间 segmented / 时间分组 / 加载更早分页）—— commit `e8c6908`
  - ✅ FilterBar 抽离公共组件 —— commit `05cb274`
  - ✅ 拆 3 个 follow-up task 推进

- ✅ **重审 gap-checklist 42 个 Closed 行的真实状态**
  - ✅ 已拆 4 batch 完成全部 42 行重审 —— commits `dfa32fc` / `3f7cca9` / `f09bdf7` / `227537d`
  - 结果：38 行 → Closed (verified)；4 行 → Partial（前端 list-page 筛选完成度），均已在 P0 完成项中闭合
  - 关联：`docs/release/v1-gap-checklist.md` 顶部 banner + 末尾 Reassess findings 段

- ✅ **解决 12 条新 gap 中的 P0 项**
  - ✅ **gap #12**：`make verify-web` 加 `npm run lint` —— commit `1704c02`；先建 lint baseline（4 errors）`75d4034`
  - ✅ **gap #3**：约定下次 migration 序号从 0011 起，落入 `.trellis/spec/backend/database-guidelines.md` —— commit `6a52ced`
  - ✅ **gap #7**：`cmd/houfeng-center/main.go` stdlib `"log"` → `slog` —— commit `a613f8e`

- ✅ **真实环境冒烟 V1 完整路径**
  - ✅ 2026-05-02 live smoke 真实跑通 —— commit `6394b29`
  - 路径：Node 接入 → Target 创建 → ProbeItem → 异常发生 → 通知 → 恢复
  - 4 caveats（gap #13-#16）已入 gap-checklist —— commit `92e5b6f`
  - 🔲 Telegram 真发：deferred to ops follow-up（user-env-required，无 bot token 时不阻塞 Stage 1 收口）

#### P1（V1 收口质量门）

**✅ Stage 1 P1 实质完成 (2026-05-03)；剩余项推 Stage 2 / phase 2.2**

- ✅ **解决 12 条新 gap 中的 P1 项**
  - ✅ **gap #9**：双 fetch wrapper 合并 —— commit `b354f3f`
  - ✅ **gap #10**：`pages/NodesPage.tsx:60` `createNode` 重构进 `lib/api.ts` —— commit `d78ef0f`
  - ✅ **gap #4**：修正 `0010_add_users_and_sessions.sql` sessions 索引命名 —— commit `8cbae4d`

- ✅ **CLAUDE.md 文档断层修补**
  - 已在 T2 落地；本 session 通过 next-phase-plan reframe 进一步收敛（commit `4cbbed9`）

- ✅ **长 page 文件初步拆分**（gap #11 的子集）
  - ✅ Phase 1 NodeDetailPage 拆分 —— commit `8b765c9`
  - ✅ Phase 2 TargetDetailPage 拆分 —— commit `9bcc779`
  - 🔲 phase 2.2 long-page helpers extract（剩余 `SettingsPage` 873 / `TargetsPage` 740 / `NodesPage` 671 拆分）—— deferred to Stage 2
  - 🔲 NodeDetailPage phase 1.2 剩余 sections —— deferred to Stage 2

- 🔲 **Telegram 通知真实环境验证** —— deferred to ops follow-up
  - user-env-required；2026-05-02 smoke 因无 Telegram env vars 未触发
  - 已在 gap-checklist `Live Telegram delivery evidence` 行标 Partial / acknowledged
  - **不阻塞 Stage 1 收口判定**

**Stage 1 P0/P1/P2 总体状态（2026-05-06 更新）**

P0 全完成 / P1 全完成 / P2 接近完成：

- ✅ **gap #6**：已闭（CLAUDE.md:89 明确 `https` = http + TLS config，与代码一致）
- ✅ **gap #8**：已闭（CLAUDE.md + component-spec.md 均列出全部 14 个 atoms，含 MetricChart / Drawer / Stepper 等 v2 新增）
- ✅ **视觉证据**：v2 5 page screenshots 已捕捉到 `docs/operations/*.jpg`（2026-05-06）
- 🔲 **长 page 文件全部拆分**：SettingsPage 899 / NodesPage 1136 / TargetsPage 1192 / TargetDetailPage 1321 / NodeDetailPage 1009 → deferred to Stage 2（watchtower 改造显著增加了页面长度，需要单独拆分 task）

### Stage 1 完成判定（V1 release gate）

参考 `docs/release/v1-gap-checklist.md` 末尾 "Final V1 release gate" 段，**外加**本阶段补充：

- gap-checklist 42 Closed 行重审完成（按 area 分批；P0 优先）
- 12 条新 gap 中 P0 + P1 全部 closed ✅
- 真实环境冒烟通过且记录在 `docs/operations/v1-smoke-run.md`（含 Telegram 真发证据，或明确记录本次部署 Telegram 已禁用 / 未配置且未尝试 outbound delivery）
- P2 残余项已清（gaps #6/#8 文档同步 + visual evidence v2 screenshots + atoms 登记），仅 long-page 拆分延后至 Stage 2
- `go test ./...` / `./scripts/verify.sh` / `cd web && npm run build` 全绿
- 385 vitest / 前端 watchtower 全栈完成（节点+目标双线）

**🔓 Stage 1 完成判定通过。Trigger condition 满足：可启动 Stage 2 brainstorm。**

完成上述判定后，可以打 V1 tag，并触发 Stage 2 brainstorm task。

## Stage 2: post-V1 → MVP

**占位**。具体范围在 Stage 1 完成后单独 brainstorm，不在 V1 收口期讨论。

**Trigger condition**：Stage 1 全部 P0 + P1 closed → 触发 Stage 2 brainstorm task（创建于 `.trellis/tasks/`）。

**2026-05-10 状态更新**：Stage 2 第一条具体扩展计划已经收敛为根目录 `houfeng_codex_下一步开发计划.md`（VPS Asset Ledger + Fleet Observability）。该计划的 Task 1-3、Task 5-8 与 VPS-scoped service/domain 轻量扩展已经完成；Task 4 的 dry-run/import 工具链完成，但真实 40+ VPS 数据执行仍为 user-data-dependent deferred。完成度审计见 `docs/release/asset-ledger-roadmap-completion.md`。

**2026-05-11 状态更新**：当前项目剩余工作审计见 `docs/release/current-state-and-next-stage-plan.md`。结论是：旧 Asset Ledger 计划没有新的立即开发任务；真实 VPS 数据 dry-run/import 保持条件性延期；Provider/DNS 同步、Web SSH、插件、服务发现/完整注册表、完整域名管理、RBAC、汇率、评分算法等方向必须另起产品计划；前端长页面/大文件机械拆分暂停，等待页面产品/UX 方向重新确定后再决定是否拆分。

**已知方向起点**（仅作 trigger 后讨论的种子，不锁定）：
- 用户判定 MVP 比 V1 范围大；具体功能扩展待 brainstorm
- 候选方向（来自 v1-baseline 自身的"intentionally out of scope"段，与本阶段无关）：
  - 通用脚本执行 / 远程操作面（v1 明确划在外；当前仅有编译期白名单 node actions；command identity / in-flight durability 已在 `05-10-command-result-durability` 收口，产品边界已在 `05-11-agent-command-boundary-hardening` 收口）
  - Docker / 容器编排（v1 明确划在外；当前仅有 best-effort Docker CLI container facts / `docker ps` 白名单，且 Docker CLI 参数形状已测试锁定；这不等于容器编排能力）
  - 复杂规则引擎（v1 强调"统一规则集中在中心，不下放 agent"）
  - 多目标分组与批量操作 / 模板化探针（v1 强调单用户、低密度配置面）
- 是否引入新存储后端（TSDB？）或继续扩展邮件 / 企微等通知通道，仍属于后续 Stage 2 brainstorm 决议。**2026-05-11 更新**：正式 Telegram / Feishu notification channel model 已由 `05-11-notification-channel-model` 收口，notification record 现在按真实 channel 写入；本计划不再把 Feishu-only / mixed delivery 语义列为未完成项。同日，Agent command / Docker boundary 已由 `05-11-agent-command-boundary-hardening` 收口；本计划不再把当前白名单命令或 best-effort Docker facts 误描述为 unresolved scope。

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
  - NodesPage createNode bypass lib/api.ts (gap #10) —— 2026-05-03 已闭
  - NodesPage 列表筛选缺 5/7 项 —— 2026-05-06 复核已闭
  - TargetsPage 完全无筛选条 —— 2026-05-06 复核已闭
  - EventsPage 高级筛选缺 4 项 —— 2026-05-10 已补齐 backfill API 维度 + v2 Drawer/chip flow
- 0 行 Not Closed / 0 Inconclusive

**关键洞察**：用户判定"实现连 V0.1 都不到"的实证根因 = 前端 list-page 筛选完成度，**不是**后端 / 运行时 / 通知 / 部署 / 认证 / 视觉系统。Stage 1 收口因此应优先解决 list-page filter 工作项。

## 变更日志

| 日期 | 变更 |
|---|---|
| 2026-05-11 | 增加 `docs/release/current-state-and-next-stage-plan.md` 作为当前剩余工作审计入口，明确旧计划无立即任务、真实数据条件性延期、前端机械拆分暂停。 |
| 2026-05-10 | 记录 Stage 2 第一条具体扩展计划已落地到 `houfeng_codex_下一步开发计划.md`，完成度审计见 `docs/release/asset-ledger-roadmap-completion.md`。 |
| 2026-05-02 | 初版，由 T2 (`.trellis/tasks/05-02-roadmap-and-claude-md`) 起草。关联 T1 (docs-audit) + T3 (spec-sync) 已落地的成果。Stage 1 详细 + Stage 2/3 占位 + trigger condition。 |
