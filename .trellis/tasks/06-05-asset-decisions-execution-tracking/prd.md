# 资产组合决策执行跟踪闭环

## Goal

把已保存的资产组合决策记录从“结论快照”推进到“执行跟踪入口”。用户保存一组组合判断后，应能在 `/asset-decisions` 继续逐台 VPS 跟踪下一步处理状态、记录阻塞原因，并从记录详情跳转到正确对象页面执行真实操作。

这一阶段仍不执行取消、迁移、订阅修改、监控修改或批量动作。资产决策页只记录“该 VPS 的组合决策后续跟进到哪一步”，真实业务状态仍由 VPS、订阅、监控、Target 等原页面负责。

## Requirements

- 为 `asset_decision_record_members` 增加成员级跟进信息：
  - `followup_status`，允许 `todo`、`in_progress`、`blocked`、`done`、`skipped`。
  - `followup_note`，记录执行备注、阻塞原因或外部处理说明。
  - `followup_updated_at`，记录最后一次成员跟进更新时间。
- 已有记录成员迁移后默认 `followup_status = todo`、`followup_note = ''`，不得破坏已有记录快照。
- 记录列表与记录详情需要返回成员跟进聚合计数，至少能看出总成员数、已完成/跳过数量、进行中数量、阻塞数量和待处理数量。
- `PATCH /api/asset-decisions/records/{record_id}` 保持现有记录级状态更新能力，并扩展支持成员级跟进更新。
- 成员级跟进更新必须只允许修改记录内已有 VPS 成员；未知 `vps_id`、重复成员、非法状态都返回 `400 invalid input`。
- 记录详情 UI 需要展示每台 VPS 的跟进状态、备注和最后更新时间，并允许直接修改单个成员的跟进状态和备注。
- 成员推进入口继续使用现有对象页面链接：
  - `cancel` 或 `open_cancellation_workbench` 进入 VPS lifecycle workbench。
  - 其他动作进入 VPS 详情。
- 记录级状态仍由用户显式修改；成员全部完成不自动修改整条记录状态，避免隐式状态机扩张。
- 本阶段不新增删除记录、不新增批量执行、不自动推断跟进完成、不引入性能/路由/IP 质量趋势证据。

## Acceptance Criteria

- [ ] 新迁移可在空库和已有 `asset_decision_record_members` 数据上安全执行。
- [ ] 后端 record list/detail JSON 包含成员跟进计数；record member JSON 包含 `followup_status`、`followup_note`、`followup_updated_at`。
- [ ] 创建新记录时所有成员默认进入 `todo` 跟进状态，并在列表聚合中体现。
- [ ] PATCH 记录级状态的现有行为保持不变。
- [ ] PATCH 单个成员跟进后，详情返回更新后的成员状态/备注，列表中的计数同步变化，记录 `updated_at` 被刷新。
- [ ] PATCH 非法成员跟进输入返回稳定错误，不落入 500。
- [ ] `/asset-decisions` 已保存决策记录列表能显示跟进进度和阻塞数量。
- [ ] 记录详情 modal 能查看并更新每个成员的跟进状态/备注，更新 payload 与 API 合同一致。
- [ ] 现有“保存组合决策记录”和“单台续费处理”路径不回退。
- [ ] 覆盖后端 store、handler/API、前端 API helper 和页面交互测试。

## Notes

- 这是资产组合决策中枢的下一步小闭环：它解决 Phase 2 planning 中指出的“组合判断之后用户仍需自己记住下一步去哪里处理”的缺口。
- 决策记录仍是 memory layer，不是 VPS 生命周期状态机，也不是订阅/监控的第二套编辑入口。
