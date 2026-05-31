# 统一资产取消与生命周期协同

## Goal

实现可预览、可确认、可审计的订阅 / VPS / Node / Target 取消与退役联动工作流，让订阅页面、VPS 页面、节点页面和实例相关页面都能看出同一台资产的真实生命周期状态，避免各对象状态割裂。

## Problem

用户在订阅页面取消一台已过期且不续费的服务器后，订阅显示过期，但 VPS 列表、VPS 详情、Node 列表和 Node 详情仍看不出这台服务器已取消；再从 VPS 详情取消时又提示没有 active 订阅，形成“订阅已经取消但 VPS/节点仍像正常运行”的割裂体验。

根因是当前系统把订阅、VPS、VPS-Node 链接、Node、Target/服务资产作为独立账本对象管理；普通 CRUD 不会跨域传播状态，且现有 VPS 取消联动只查找 `status='active'` 的订阅，忽略已经 `expired/cancelled` 的关联订阅证据。

## Requirements

- 新增显式生命周期工作流：从 VPS 出发预览取消/退役影响范围，再由用户确认执行所选步骤。
- 预览必须覆盖关联订阅、VPS 当前状态、活跃 VPS-Node 链接、相关 service/domain 资产，以及通过 service/domain 关联到的 Target。
- 执行必须可审计：记录一次 lifecycle action 及每个步骤的对象、前后状态、执行状态和错误信息。
- 普通订阅 / VPS / Node / Target CRUD 不做隐式跨域写入；只有明确确认的 cancellation action 可以跨域更新。
- 当无 active 订阅但存在 expired/cancelled 订阅时，系统必须显示“已有非活跃订阅证据，仍需处理 VPS/Node/实例状态”，不能继续提示“没有关联订阅”。
- 订阅、VPS、Node、Target 页面必须能显示取消/过期资产上下文，用户能从这些页面进入统一处理路径。
- Dashboard/资产摘要必须把取消待处理、状态不一致、仍运行的关联 Node/Target 计入可见风险，而不是隐藏在单个页面里。
- 更新 `.trellis/spec` 与 `docs/design/v2-houfeng`，把受控 lifecycle action 作为跨域联动的唯一例外写入规范。
- “实例”按当前代码结构解释为 Node 运行实例，以及通过 asset service/domain 关联到的 Target；后续若新增独立 Instance 实体，再接入同一 workflow。
- 本任务覆盖本分支已经完成的后端/API/页面联动修复，以及后续发现的取消/退役工作台 UI/UX 修复；不要另建任务或另开 worktree。
- 取消/退役工作台必须符合候风 v2 工具型密度：顶部摘要不能出现 3+1 断行；VPS state、订阅、Node、Target、确认执行必须按桌面/小桌面/平板/手机都有用的响应式布局组织，避免一行一个低密度卡片造成大面积空白。
- 工作台中的 checkbox、状态 badge、说明文字、select/date/reason 控件必须形成清晰的扫描路径：默认展示当前事实、只突出用户需要确认的变更，不把“说明文字”和“状态”混成难读的长行。
- 工作台必须沿用项目现有 atoms、badge、form 和 v2 设计 token，不出现“部分像项目、部分像临时表单”的割裂视觉。

## Acceptance Criteria

- [ ] `GET /api/vps/{vps_id}/cancellation-preview` 返回完整影响图，包含 inactive subscription 证据、Node、service/domain 和 Target 候选。
- [ ] `POST /api/vps/{vps_id}/cancellation` 在一个事务中写入所选订阅、VPS、Node、Target 状态变更和 lifecycle action/step 审计记录。
- [ ] `GET /api/asset-context/nodes` 与 `GET /api/asset-context/targets` 提供列表页可批量消费的资产取消上下文。
- [ ] 订阅已 `expired/cancelled` 且 VPS 仍 active 时，preview 和页面提示状态不一致，而不是 `no_active_subscription`。
- [ ] Node/Target 运行状态只在用户确认的 action step 中改变；preview 本身不修改任何运行对象。
- [ ] VPS 列表有“取消待处理/状态不一致”视图；VPS 详情有取消/退役工作台；订阅页在取消/过期订阅后显示联动入口；Node/Target 列表与详情能显示关联 VPS 的取消/过期上下文。
- [ ] Dashboard 资产摘要的有效成本只统计 active subscription，并暴露取消待处理/状态割裂风险计数。
- [ ] 后端 store/handler/router/bootstrap 测试覆盖新增 API、审计表迁移、事务行为和错误映射。
- [ ] 前端 API/type/page 测试覆盖工作台、上下文显示、订阅取消入口、VPS/Node/Target 列表状态。
- [ ] 取消/退役工作台在桌面、小桌面、平板、手机视口下布局可用：摘要区稳定分栏，执行区可并排或紧凑堆叠，行内控件不撑满无意义空白，文本不溢出、不重叠。
- [ ] 工作台视觉与候风 v2 保持一致：不嵌套卡片墙，不出现 3+1 summary card，不把 `取消` / `已取消` 这类单选值做成整行宽条。
- [ ] `go test ./...`、`cd web && npm run lint`、`cd web && npm run test -- --run`、`cd web && npm run build` 通过；前端完成后用本地浏览器 sanity 检查核心页面。

## Out Of Scope

- 不自动删除或断开 `vps_node_links`；链接是资产历史证据。
- 不在订阅 PATCH 中静默停止 Node、归档 Target 或删除服务/域名资产。
- 不引入 React Query、状态管理库、ORM 或 SQL codegen。
- 不把 `archived` 作为普通取消动作的默认结果；`archived` 仍是人工整理后的最终归档状态。

## Notes

- 当前实现分支：`worktree/asset-lifecycle-coordination`。
- PR 基线：`main`。
- 当前 worktree：`/Users/weibo/Code/houfeng/.worktree/asset-lifecycle-coordination`。继续在此 worktree 和分支上修复，不要切换到主 checkout 或创建第二个 lifecycle/UI 分支。
