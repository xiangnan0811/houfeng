# 资产组合决策手工组合与场景策略闭环

## Goal

把资产组合决策中枢从“系统自动发现组合 + 保存决策记录 + 跟进回读”，推进到“用户可围绕真实问题创建手工组合 / 对比篮子”的完整工作台。

本阶段的用户价值是：用户不必被动等待自动组覆盖自己的真实决策问题，而是可以主动创建诸如“德国主力与备用取舍”“美国高配 VPS 压缩预算”“某服务商组合风险评估”“闲置机器迁移退役计划”这样的场景组合，在组合内比较 VPS、补充目标、形成决策记录，并继续复用现有执行跟进和 execution readback。

## Requirements

- 新增持久化的手工资产决策组合，不再把手工场景伪装成自动组：
  - 组合 ID 使用 `admg_*`。
  - 组合只保存用户的决策场景、成员、意图、备注和排序，不执行任何资产动作。
  - 支持 active / archived 状态；archive 仅隐藏组合，不删除历史决策记录。
- 新增手工组合成员：
  - 成员引用现有 `vps_assets.vps_id`。
  - 成员可保存 intended role、intended action、reason、note、sort_order。
  - intended role/action 复用现有 `SuggestedRole` / `SuggestedAction` 枚举。
  - 成员当前事实、证据 chips、成本、服务/域名/Target/监控摘要必须从现有 facts 实时回读，不作为成员事实落库。
- 新增登录保护的 center API：
  - `GET /api/asset-decisions/manual-groups`
  - `POST /api/asset-decisions/manual-groups`
  - `GET /api/asset-decisions/manual-groups/{manual_group_id}`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}`
  - `POST /api/asset-decisions/manual-groups/{manual_group_id}/members`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
  - `DELETE /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
- 支持从自动组创建手工组合：
  - 前端从 group detail 触发，后端可接受 `source_group_id` + `renew_within_days`，重新加载当前自动组 facts 并生成手工组合和成员。
  - 自动组变化或缺失时返回 not found / invalid input，不保存不可信场景。
- 支持从手工组合保存资产决策记录：
  - `POST /api/asset-decisions/records` 支持 `source_type=manual_group`。
  - 手工组合生成记录时使用当前 facts 生成 group/member snapshot。
  - 现有 execution readback 继续适用于手工组合来源的记录。
- 自动组、手工组合、决策记录三者边界清晰：
  - 自动组仍是系统发现入口。
  - 手工组合是用户定义的决策场景。
  - 决策记录是一次已保存判断和后续跟进。
- `/asset-decisions` 前端新增“自定义组合”主 surface：
  - 自动组列表仍是主要发现入口。
  - 自定义组合列表展示 status、场景类型、成员数、成本、证据缺口、更新时间。
  - 组合详情展示成员对比、当前事实摘要、角色/动作意图、保存记录入口。
  - 从自动组详情可创建手工组合。
  - 从手工组合详情可新增/移除/编辑成员，并保存为决策记录。
- 页面层级保持资产组合决策中枢定位：
  - 单台队列仍是辅助 surface。
  - 续费 evidence 仍是证据区，不重新成为页面主体。
  - 不在本阶段新增批量执行或自动状态修改。
- 更新 Trellis 规范和项目设计说明：
  - 说明手工组合、自动组、决策记录、execution readback 的边界。
  - 明确本阶段不使用 IP 质量、路由、性能、CPU/IO、超售判断。

## Constraints

- VPS 仍是业务状态主体；Subscription、MonitoringInstance、Target 继续作为证据来源。
- 手工组合需要新增 migration，但不得修改现有 VPS / Subscription / MonitoringInstance / Target 行为。
- 不新增批量续费、批量取消、批量迁移，不自动调用 lifecycle workbench。
- 不自动 PATCH record status，不把 readback drift 当作执行承诺。
- 不读取 runtime facts detail，不依赖 HostSample / ProbeObservation。
- 不判断 IP 质量、路由质量、性能衰退、CPU/IO 趋势、超售。
- 当前 facts 查询失败时 fail closed，不能把未知事实伪造成健康、缺口或完成。

## Acceptance Criteria

- [ ] 新增 migration 创建手工组合和成员表，并放宽 `asset_decision_records.source_type` 允许 `manual_group`。
- [ ] 后端 domain 类型覆盖 manual group summary/detail/member/input，校验重复成员、非法枚举、空标题、非法 source。
- [ ] Store 支持 list/create/get/patch manual groups 和 add/patch/delete members。
- [ ] Store 复用现有 `loadFacts` 构造成员详情，不逐台调用 runtime facts detail。
- [ ] 从自动组创建手工组合时能复制成员建议角色/动作和 evidence snapshot 语义。
- [ ] 从手工组合创建 record 时返回包含 execution_readback 的 detail，records list/detail 兼容 manual source。
- [ ] Handler/router/bootstrap 显式 wiring 完成，`/api/asset-decisions/manual-groups/*` 不落 SPA fallback。
- [ ] API 对 invalid input、missing manual group、missing member、missing source group、method not allowed、repo failure 有测试覆盖。
- [ ] `/asset-decisions` 渲染自定义组合 surface，并能打开手工组合详情。
- [ ] 自动组详情可创建手工组合，成功后刷新列表并打开详情。
- [ ] 手工组合详情可新增、编辑、移除成员；不会调用 VPS / Subscription / MonitoringInstance / Target 写接口。
- [ ] 手工组合详情可保存决策记录，并沿用现有记录列表、详情、followup、readback 展示。
- [ ] 前端 API/types/tests 覆盖新增字段和交互。
- [ ] 桌面与移动端 visual sanity 覆盖 `/asset-decisions?view=needs_decision&renew_within_days=30`，确认列表、详情、表单无横向页面溢出。
- [ ] 更新 `.trellis/spec/backend/*`、`.trellis/spec/web/*` 或 `docs/design/v2-houfeng/component-spec.md` 中与资产决策边界相关的规范。
- [ ] 运行 Trellis check、Go/Web 相关测试，并完成 finish-work / commit / PR / CI / release 监控流程。

## Notes

- 本任务是复杂任务，必须有 `design.md` 和 `implement.md` 后才能 `task.py start`。
- 本任务在 `.worktree/asset-decisions-scenario-workbench` 隔离 worktree 内实施，分支为 `worktree/asset-decisions-scenario-workbench`，PR 目标为 `main`。
- 现有历史 active Trellis 任务不属于本阶段，不在本任务中清理。
