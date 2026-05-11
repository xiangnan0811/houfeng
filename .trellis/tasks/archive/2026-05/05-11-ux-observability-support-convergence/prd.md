# UX-5 Observability support convergence

## Goal

把 Nodes / Targets / Events 从并列的“观测资源管理页”收敛为资产工作流的观测证据支撑面。用户从 Dashboard、VPS Detail 或资产核对路径进入这些页面时，应能快速理解：这些运行事实如何帮助判断 VPS、服务入口和事件处置优先级。

## What I Already Know

- 父级规划 `docs/release/core-pages-product-ux-replan.md` 已确认 UX-5 范围：收敛 Nodes / Targets / Events 的列表密度、筛选权重和资产上下文，保留 URL-state 深链。
- UX-1 已完成导航分组；UX-2 已把 Dashboard 改成资产决策和观测异常 command surface；UX-3/UX-4 已强化 VPS 列表与详情中的 Node / 事件证据。
- 当前 NodesPage 已有 compact DataTable、sparklines、URL 筛选、批量操作、绑定异常视图和 Node Detail 关联 VPS 能力。
- 当前 TargetsPage 已有 compact DataTable、URL 筛选、运行控制和目标详情能力。
- 当前 EventsPage 已有 URL-state 高级筛选、事件流分组和深链承接能力。
- 列表 API 没有给 Nodes / Targets 返回 VPS 关联上下文字段。UX-5 不应通过逐行 N+1 请求伪造资产上下文，也不新增后端字段。
- 本任务不使用 subagent，由主会话直接执行 Trellis 等价流程。

## Scope

### NodesPage

- 将页面文案和首屏结构从“节点列表”升级为“Node 观测证据”，明确它服务 VPS 资产判断。
- 在列表前增加低权重支撑面，展示异常、待接入/绑定、维护/暂停、资产关联入口等可行动证据。
- 保留现有视图切换、URL-state 筛选、批量操作、sparklines、创建节点和行点击行为。
- 不在列表行展示未存在的 linked VPS health 或逐行拉取 VPS 关联。

### TargetsPage

- 将页面定位为服务/入口观测证据列表，而不是完整服务注册表。
- 在列表前增加支撑面，展示异常目标、暂停/归档、执行节点标签覆盖和资产服务上下文入口。
- 保留现有 URL-state 筛选、创建目标、运行控制、表格密度和行点击行为。
- 不把 VPS-scoped services 扩展成跨页服务注册表。

### EventsPage

- 将页面定位为审计与诊断时间线。
- 在筛选概览前增加支撑面，明确当前事件流的对象、严重度、时间窗口和来源深链语义。
- 保留现有 URL-state truth、Drawer 草稿行为、active chips、事件分组和加载更早事件。
- 默认视图继续清楚显示对象、严重度、发生时间；有 URL 筛选时应有可读上下文提示。

## Acceptance Criteria

- [ ] NodesPage 首屏出现观测证据支撑面，包含异常、接入/绑定、维护/暂停和资产关联入口。
- [ ] TargetsPage 首屏出现入口观测支撑面，包含异常、暂停/归档、执行节点覆盖和资产服务上下文入口。
- [ ] EventsPage 首屏出现诊断时间线支撑面，包含事件数量、筛选上下文、对象/严重度/时间窗口入口。
- [ ] 三页保留现有 URL-state 深链：Nodes `onboarding=pending` / `abnormal=1`，Targets `abnormal=1` / `run_status=暂停|已归档`，Events 全部 supported query。
- [ ] 三页不新增后端 API、不新增跨页 N+1 拉取、不展示未存在的 linked VPS health。
- [ ] 更新 `docs/design/v2-houfeng/component-spec.md` 中 Nodes / Targets / Events 的页面契约。
- [ ] 更新页面测试覆盖新支撑面和既有 URL-state 行为不回退。
- [ ] `cd web && npm run lint`、`cd web && TMPDIR=$PWD/.tmp npm run test -- --run NodesPage TargetsPage EventsPage`、`cd web && TMPDIR=$PWD/.tmp npm run test -- --run`、`cd web && npm run build` 通过。
- [ ] 启动本地 dev server，做桌面和移动视口 sanity check，确认首屏不混乱、文本不重叠。

## Out Of Scope

- 真实 VPS 数据 dry-run/import。
- Provider/DNS 自动同步。
- 前端大文件机械拆分。
- 后端观测数据模型重写。
- 大屏监控中心。
- 完整服务注册表、完整域名管理或 DNS record 管理。
- release/publish workflow。

## Technical Notes

- Main files:
  - `web/src/pages/NodesPage.tsx`
  - `web/src/pages/nodes/NodesHero.tsx`
  - `web/src/pages/TargetsPage.tsx`
  - `web/src/pages/EventsPage.tsx`
  - `web/src/styles/pages.css`
  - `web/src/pages/NodesPage.test.tsx`
  - `web/src/pages/TargetsPage.test.tsx`
  - `web/src/pages/EventsPage.test.tsx`
  - `docs/design/v2-houfeng/component-spec.md`
- Prefer small page-private components if structure grows; keep shared abstractions only if they reduce real duplication.
- Use existing atoms (`Button`, `Badge`, `MonoDigits`, `DataTable`, `FilterBar`, `Drawer`) and token/BEM CSS.
- Task audit: `research/current-observability-support-audit.md`.

## Definition of Done

- Code and tests committed on a non-main branch.
- Trellis task archived and journal recorded.
- PR opened, CI green, PR merged.
- Post-merge main CI monitored and local `main` synced.
- Release/publish workflow intentionally deferred per user instruction.
