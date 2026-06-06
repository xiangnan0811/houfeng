# 资产组合决策证据洞察与对比增强

## Goal

把 `/asset-decisions` 已有的自动组、自定义组合、决策记录证据从“字段齐全”推进到“用户一眼能理解为什么这么取舍”。本任务在现有只读 read model 基础上增加可解释的组合对比洞察，并重排页面详情体验，让用户能围绕主力、备用、观察、退役、补证据这些真实决策场景比较 VPS，而不是在多张宽表里自行拼结论。

本阶段仍不实现 IP 质量、路由质量、性能衰退、CPU/IO 趋势或超售判断；这些依赖 agent 与观测语义成熟后再进入资产决策模型。

## Confirmed Facts

- 当前 `main` 已包含资产组合决策中枢的基础闭环：自动组、场景模板、自定义组合、保存记录、执行回读、执行编排、决策路径和下一步导览。
- 后端已有只读 `evidence_assessment` 与 `decision_recommendation`，由 `internal/center/assetdecisions/assessment.go`、`recommendation.go`、`types.go` 派生，不依赖新增表。
- 前端 `web/src/pages/AssetDecisionsPage.tsx` 已展示主工作台、组卡、组详情表格、自定义组合详情、记录详情、执行 board、续费 evidence 和单台队列。
- 当前组详情和自定义组合详情仍主要依赖宽表列展示成员事实；用户可以看到成本、服务、订阅、监控、建议动作，但缺少一层明确的“同组成员差异解释 / 证据矩阵 / 候选分层”。
- 当前测试已覆盖页面主 surface、URL context、组详情、手工组合、记录回读/执行 plan、单台续费 PATCH 不变、订阅 evidence 失败不误报缺订阅；但没有覆盖“为什么 A 是主力候选、B 是备用/补证据/退役候选”的比较体验。

## Requirements

- 增加只读组合对比洞察，不新增 endpoint、不新增 migration、不新增批量执行。
- 后端在现有 `assetdecisions` domain 内派生 comparison insight，复用已有 facts、evidence chips、evidence assessment、decision recommendation、成员事实计数、角色/动作建议。
- comparison insight 只能解释现有证据，不引入 IP、路由、性能、CPU、IO、超售、runtime facts detail、HostSample 或 ProbeObservation。
- 自动组与自定义组合都应返回组级和成员级对比洞察；自定义组合成员当前 facts 缺失时必须保留成员并显示事实缺失解释。
- 决策记录保存时应把新增对比洞察写入 evidence snapshot，旧记录缺失该字段时前端降级显示，不报错。
- 前端组列表保持自动组为第一主 surface，但组卡应更清晰表达“比较结论”和高优先级成员，而不是只堆指标。
- 组详情 modal 增加“证据矩阵 / 取舍对比”主 surface，位置在 summary / 场景推进建议之后、宽表之前。
- 自定义组合详情增加同样的对比 surface，并额外展示成员 intended role/action 与当前证据是否一致，帮助用户把比较篮子推进到可保存记录。
- 决策记录详情只补强“保存时判断依据”回看：展示保存时对比洞察快照；不修改 record decided_role / decided_action，不自动完成状态，不触发业务写接口。
- 页面视觉层级不得倒置：自动组和组合对比仍高于记录执行编排，记录高于续费 evidence 与单台队列。
- 所有 CTA 仍只打开现有 group/manual group/record/VPS/subscription detail 或提交已有 record/manual followup；不得自动 PATCH VPS / Subscription / MonitoringInstance / Target。
- 保持当前 URL-state 行为：`view`、`renew_within_days`、context chips 和 `group_id/manual_group_id/record_id/template_id` 深链不破坏。

## Acceptance Criteria

- [ ] 后端 `GroupSummary` / `GroupMember` / `ManualGroupSummary` / `ManualGroupMember` 返回只读 comparison insight，字段 snake_case 且可由现有 facts 完整派生。
- [ ] comparison insight 有稳定、可测试的 lane / factor / tradeoff 语义，能区分主力候选、备用/观察、退役/取消、补证据、复核。
- [ ] comparison insight 不读取新增表，不调用 runtime facts detail，不使用 IP/路由/性能/CPU/IO/超售字段。
- [ ] `RecordSnapshotFromGroup` / `RecordSnapshotFromMember` 保存新增对比洞察；旧 snapshot 缺失时前端显示降级文案。
- [ ] 自动组详情展示“证据矩阵 / 取舍对比”，用户能在不横向滚动宽表的情况下比较每台 VPS 的角色、成本、承载、续费压力、监控/Target、资料缺口和下一步。
- [ ] 自定义组合详情展示当前证据对比、成员意图、意图/证据一致性和保存记录准备度，不写业务资产。
- [ ] 决策记录详情展示保存时对比依据，并保留当前执行回读 / 执行计划的只读边界。
- [ ] 组列表卡片展示比较结论和优先 VPS，但首屏仍以“决策组列表”为主，不把记录/单台队列提升为主视觉。
- [ ] 现有单台续费处理 `PATCH /api/vps/{id}` payload 不变；取消/退役仍只跳 VPS lifecycle workbench。
- [ ] 局部 API 失败仍 fail closed 或局部错误展示，不把未知事实解释成健康、缺口、漂移或已对齐。
- [ ] Go domain/store/handler tests 覆盖 comparison insight 派生、snapshot、records/manual/auto group 响应和 forbidden data sources。
- [ ] Frontend tests 覆盖组列表比较结论、自动组详情矩阵、自定义组合详情矩阵、记录详情快照回看、旧 snapshot 降级和不触发业务写请求。
- [ ] Visual sanity 覆盖 `/asset-decisions?view=needs_decision&renew_within_days=30` 桌面与移动端，重点检查证据矩阵、chips、modal、宽表不横向溢出页面。

## Out of Scope

- 不新增数据库表、migration 或资产决策持久状态机。
- 不新增批量保留、批量迁移、批量取消、自动完成记录或自动修改跟进状态。
- 不修改 VPS、Subscription、MonitoringInstance、Target、Service、Domain 的业务状态。
- 不引入 IP 质量、路由质量、性能衰退、CPU/IO 趋势、超售判断或 agent runtime facts detail。
- 不重做 Dashboard、VPS、Subscriptions、Providers 页；只在必要时保持已有深链兼容。
- 不把对比洞察做成黑盒评分或智能决策承诺；它只是现有证据的可解释组织层。

## Open Questions

- 当前没有阻塞实现的问题。后续如视觉检查发现页面密度仍过高，应优先调整组详情和自定义组合详情的信息层级，不扩大后端语义。

## Notes

- 本任务属于复杂跨层任务，必须配套 `design.md` 与 `implement.md`，并在 review gate 后才能 `task.py start`。
