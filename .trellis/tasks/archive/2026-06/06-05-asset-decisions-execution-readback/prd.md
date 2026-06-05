# 资产组合决策执行证据回读

## Goal

把已保存的资产组合决策记录从“人工跟进记忆”推进为“可被当前资产事实校验的闭环工作台”。用户打开 `/asset-decisions` 时，应能看到一条组合决策是否仍待处理、是否阻塞、是否与当前 VPS / 订阅 / 服务 / 域名 / Target / 监控关联事实一致，或是否出现“跟进完成但真实资产状态未闭环”的漂移。

## Requirements

- 扩展现有资产决策记录响应，不新增 endpoint、不新增 migration。
- `RecordSummary` 与 `RecordDetail` 返回记录级 `execution_readback`，包含状态、摘要和 open / aligned / drift / blocked / needs_evidence 计数。
- `RecordMember` 返回成员级 `execution_readback`，包含状态、摘要、issues 和当前事实摘要。
- 回读只使用现有 asset decision read model 已聚合事实：VPS、订阅、服务、域名、Target、监控关联、生命周期、续费决策、资料缺口和 source availability。
- 回读不得调用 runtime facts detail，不依赖 HostSample / ProbeObservation，不判断 IP 质量、路由质量、性能衰退、CPU/IO 趋势或超售。
- 回读不得修改 VPS、Subscription、MonitoringInstance、Target、record status 或 member follow-up；它只解释当前事实与保存判断之间的一致性。
- `decided_action` 是成员回读主输入；为空时回退到 `suggested_action`。
- 取消 / 退役类动作必须检查 VPS 是否进入 `to_cancel | cancelled | archived`，且没有 active subscription、running monitoring、running target。
- 迁移类动作只检查旧 VPS 是否进入迁移链路；不判断新 VPS 是否完成替代。
- 保留 / 观察类动作只检查基础 lifecycle 与 renewal decision 一致性。
- 补证据类动作只检查当前已有 evidence gap，不扩展新智能证据。
- `abandoned` 记录为 inactive；blocked / skipped / done 的成员跟进状态必须参与 readback 优先级。
- 列表和详情 UI 展示 readback 状态与问题，但不得把 drift 表达成自动执行承诺。
- 取消 / 退役入口仍只跳转 `/vps/{id}?workbench=cancellation`。
- 更新 Trellis backend / web 规范，固化 readback 边界和禁止项。

## Acceptance Criteria

- [ ] 后端记录列表、创建、详情、PATCH 响应都包含 `execution_readback`。
- [ ] 后端能区分 open、aligned、drift、blocked、needs_evidence、inactive。
- [ ] `followup_status=done` 但当前事实不一致时返回 drift。
- [ ] `followup_status=blocked` 优先返回 blocked。
- [ ] `followup_status=skipped` 不计入普通 open，但不隐藏关键 drift。
- [ ] `record.status=abandoned` 返回 inactive。
- [ ] 当前 facts 缺失时返回 `current_fact_missing` issue。
- [ ] Store 的 `ListRecords` 批量读取成员与 facts，不逐条调用 `GetRecord`，不调用 runtime facts detail。
- [ ] 前端已保存记录列表展示 readback 状态与 drift / blocked / needs_evidence 计数。
- [ ] 前端记录详情展示成员当前事实摘要、issue chips、跟进表单和动作入口。
- [ ] 成员跟进保存 payload 与现有行为一致，且成功后 readback 随响应刷新。
- [ ] readback 不触发任何 VPS / Subscription / MonitoringInstance / Target 写请求。
- [ ] Go domain、store、handler 测试覆盖主要判定与错误边界。
- [ ] Web API / AssetDecisionsPage 测试覆盖新增展示与不写业务对象边界。
- [ ] `/asset-decisions?view=needs_decision&renew_within_days=30` 桌面与移动视觉 sanity 无横向页面溢出。

## Notes

- 本任务完成的是执行证据回读闭环，不是最终智能决策系统。
- IP 质量、路由质量、性能衰退、CPU/IO 趋势、超售判断全部延后，等待 agent 与观测语义成熟。
- 手工组合 / 自定义对比篮子留到 readback 稳定后的后续任务。
