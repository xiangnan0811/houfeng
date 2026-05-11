# Core pages product UX replan

## Goal

在继续真实 VPS 数据测试或前端页面实现之前，重新规划候风核心页面的产品定位、信息架构、页面优先级与 UX 落地顺序。

本任务只做产品/UX 规划与文档收口，不修改前端实现。规划产物需要能直接指导后续实现任务，让页面从当前“信息混乱、视觉不吸引、难以进入真实数据测试”的状态，转向一个以 VPS 资产、续费决策和观测信号为核心的工程工作台。

## What I Already Know

- 用户明确认为当前页面太丑、混乱，UI/UX 和用户体验不足以支撑真实数据测试。
- 2026-05-11 当前状态审计已经确认：真实数据问题暂不处理，前端大文件机械拆分暂停，下一阶段应优先做“产品/UX 重新规划”。
- `docs/design/v2-houfeng/design-language.md` 已定义 dark-first、克制、高密度、工程师长期使用友好的方向，但当前截图和页面体验没有真正体现该方向。
- `docs/design/v2-houfeng/component-spec.md` 对 Dashboard / Nodes / NodeDetail / Events 等页面有 v2 契约，但 Asset Ledger 加入后，核心入口已经从纯 Fleet Observability 转向 “Asset Ledger + Observability”，旧页面契约需要重新排序。
- 当前主导航是扁平资源列表：`首页 / VPS / 服务商 / 订阅 / 资产决策 / 节点 / 目标 / 事件 / 设置`。资产页面被加入后尚未形成清晰分组和主工作流。
- `docs/operations/` 现有截图只覆盖 Dashboard、节点、目标等观测页面，缺少 VPS / 资产决策页面的视觉证据。
- 本仓库后续仍必须遵守 Trellis 任务记录、非 main 分支、PR、CI 绿后合并、合并后 main CI 监控和本地 main 同步流程。本轮不使用 subagent。

## Requirements

### 1. 记录当前 UI/UX 问题

必须产出 research 记录，至少覆盖：

- 当前截图和代码状态暴露出的页面体验问题。
- 当前体验为什么会阻碍真实 VPS 数据测试。
- v2 设计语言与实际页面之间的偏差。
- Asset Ledger 加入后，旧首页/观测优先的信息架构为何不再充分。

### 2. 明确核心页面集

规划必须选定下一阶段优先重做的核心页面集，并说明排序原因。默认核心页面集为：

- 工作台 / Dashboard。
- 资产决策页。
- VPS 列表页。
- VPS 详情页。
- 支撑性观测页面：节点、目标、事件。

服务商、订阅、设置可作为资产上下文页面纳入导航规划，但不作为第一批视觉重做主战场。

### 3. 重新定义产品工作流

规划必须围绕真实用户的日常路径，而不是围绕后端资源表：

1. 进入应用后先知道现在有什么需要处理。
2. 查看 VPS 资产与续费压力。
3. 处理资产决策。
4. 查看单台 VPS 的成本、续费、关联节点、服务、域名和事件。
5. 通过节点、目标、事件追踪技术状态，但不让观测页抢走资产管理主线。

### 4. 确定视觉与 UX 方向

规划必须明确：

- 默认方向是 dark-first、高密度、克制、长期使用友好的工程工作台。
- 不做营销首页，不做卡片堆叠型 SaaS 后台，不做浅色大留白页面。
- 列表页应以扫描、比较、批量判断为主，筛选器不能压过数据本身。
- 详情页应以“当前判断 + 下一步动作 + 关联上下文”为主，不做技术字段长卷轴。
- Dashboard 应是 command surface，不是 KPI 墙或 API 字段展示页。

### 5. 拆出后续实现批次

规划必须把后续实现拆成可执行批次，且每批都有边界、目标页面和非目标。建议顺序：

1. App shell / 导航 / 视觉基线重置。
2. Dashboard 工作台重塑。
3. 资产决策 + VPS 列表重塑。
4. VPS 详情页重塑。
5. 节点 / 目标 / 事件支撑页收敛。

### 6. 更新现有下一阶段规划入口

需要更新 `docs/release/current-state-and-next-stage-plan.md`，把本轮 UX 规划作为用户已确认的下一阶段入口，避免后续回到真实数据或机械拆分。

## Acceptance Criteria

- [x] `research/current-ui-ux-audit.md` 记录当前页面体验问题和重新规划依据。
- [x] 新增 release/roadmap 文档，明确核心页面产品/UX 重新规划，包括问题诊断、目标体验、核心页面集、信息架构方向、视觉原则和后续实现批次。
- [x] `docs/release/current-state-and-next-stage-plan.md` 指向新规划文档，并记录该入口已经被用户确认。
- [x] 规划明确本任务不改前端实现，不处理真实数据导入，不继续机械拆分大文件。
- [x] Trellis context、任务状态、归档和 journal 记录完整。
- [x] 至少运行 `python3 ./.trellis/scripts/task.py validate .trellis/tasks/05-11-core-pages-ux-replan` 与 `git diff --check`；PR CI 通过后合并。

## Definition of Done

- 文档变更已提交到非 main 分支。
- Trellis task 已归档并记录 journal。
- PR 已创建，CI 全绿后合并。
- 合并后监控 main CI，并同步本地 `main`。

## Out of Scope

- 不修改前端页面代码、CSS、测试或截图。
- 不运行真实 40+ VPS JSON dry-run/import。
- 不实现新后端模型、API 或数据迁移。
- 不继续按文件行数拆分前端页面。
- 不处理 release/publish workflow。
- 不启动 Web SSH、Provider/DNS 同步、插件系统、完整域名管理、RBAC、汇率换算或评分算法。

## Technical Notes

- 当前状态入口：`docs/release/current-state-and-next-stage-plan.md`。
- Asset Ledger 完成度证据：`docs/release/asset-ledger-roadmap-completion.md`。
- 视觉权威：`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`。
- 前端规范：`.trellis/spec/web/index.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/component-conventions.md`。
- Git 流程规范：`.trellis/spec/guides/branch-workflow-governance.md`。
