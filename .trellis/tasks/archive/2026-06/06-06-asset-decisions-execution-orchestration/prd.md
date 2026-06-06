# 资产组合决策执行编排工作台

## Goal

把 `/asset-decisions` 已保存组合决策记录从“回看 + 跟进表格”升级为执行编排工作台。用户打开一条记录后，可以按取消/退役、迁移、保留/观察、补证据、复核等 lane 查看成员当前事实、执行回读、下一步入口和人工跟进状态。

本阶段只做编排与回读展示，不做批量执行，不新增业务状态机，不自动修改 VPS / Subscription / MonitoringInstance / Target。

## Requirements

- 扩展现有 records API 响应，不新增 endpoint、不新增 migration：
  - `RecordSummary` / `RecordDetail` 返回只读 `execution_plan`。
  - `RecordMember` 返回只读 `execution_plan`。
- 后端 `execution_plan` 只返回语义，不返回 SPA 路由字符串：
  - lane: `cancel_retire | migration | keep_observe | evidence | review`
  - step kind: `open_cancellation_workbench | open_vps_detail | open_subscription_context | review_record`
  - tone: `critical | alert | notice | normal | neutral`
  - summary、step_label、issue_count、blocked、actionable 等扫描字段。
- 执行计划必须复用现有 `execution_readback` 与 `loadFacts` 当前事实，不调用 runtime facts detail、HostSample、ProbeObservation、IP/路由/性能/CPU/IO/超售数据。
- records list 必须继续避免 N+1：一次读 records、一次读 facts、一次批量读 members，然后批量派生 readback 与 plan。
- record status 仍由用户显式维护；成员全部 done/skipped 不自动 completed；completed 记录如果事实漂移仍显示 drift/actionable plan。
- 记录成员的 `decided_action` / `decided_role` 保持历史不可变；本阶段不得新增 patch 入口修改成员决定。
- 前端记录列表展示 execution plan 摘要、lane counts、actionable count，视觉层级仍低于自动组和自定义组合。
- 前端记录详情 modal 增加执行编排 board：
  - 顶部 summary 展示执行计划摘要和 lane counts。
  - lane board 按 lane 展示成员、readback badge、issue chips、current facts、下一步 CTA、快速跟进按钮。
  - 保留成员明细表或等价明细区，继续展示保存时证据快照、系统建议、用户判断和跟进备注。
- 前端根据 step kind 映射现有深链：
  - `open_cancellation_workbench` -> `/vps/{vps_id}?workbench=cancellation`
  - `open_subscription_context` -> `/subscriptions?vps_id={vps_id}`
  - `open_vps_detail` -> `/vps/{vps_id}`
  - `review_record` 留在记录详情复核，也可提供 VPS detail 入口。
- 快速跟进只能调用 `PATCH /api/asset-decisions/records/{record_id}` 的成员 followup payload；不得触发 VPS / Subscription / MonitoringInstance / Target 写请求。

## Acceptance Criteria

- [ ] records list/detail/create/patch success 响应包含 record/member `execution_plan`。
- [ ] Go domain tests 覆盖 action 到 lane/step kind 的映射、abandoned inactive、completed drift、blocked 优先、done drift、complete_evidence 只用现有 gap、current fact missing。
- [ ] Go store/handler tests 证明 records API 返回 `execution_plan`，ListRecords 继续批量读取 members/facts，facts 查询失败 fail closed，既有错误合同不变。
- [ ] 前端类型与 fixtures 覆盖新增 plan 字段。
- [ ] `/asset-decisions` 记录列表展示 plan summary、lane counts、actionable count。
- [ ] 记录详情展示 lane board、成员 current facts、issue chips、CTA、跟进表单和保存时证据明细。
- [ ] CTA URL 映射正确，且 readback drift / 快速跟进不会触发 VPS / Subscription / MonitoringInstance / Target 写请求。
- [ ] 自动组、自定义组合、记录、单台队列的视觉层级不倒置；桌面与移动端无横向页面溢出。
- [ ] Trellis backend/web spec 与 v2 component spec 记录 execution plan 边界。

## Notes

- 本任务是复杂跨层任务，需要 `design.md` 与 `implement.md`。
- 不处理 IP 质量、路由质量、性能衰退、CPU/IO 趋势、超售判断。
- 真正取消/退役执行仍只发生在 VPS lifecycle workbench。
