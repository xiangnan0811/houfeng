# Design

## Architecture

本任务延续现有两层资产决策模型：

- 自动组 read model：继续发现组合决策机会，不落库。
- 决策记录 memory layer：继续保存用户判断、证据快照和成员跟进。

新增 `execution_readback` 是第三层只读派生视图。它读取当前事实并解释“当前资产状态是否与已保存判断一致”，但不拥有任何业务状态机，也不执行取消、迁移、续费或监控操作。

## Backend Contract

沿用现有 records endpoints。新增字段为向后兼容的 JSON 字段：

- `RecordSummary.execution_readback`
- `RecordDetail.execution_readback`
- `RecordMember.execution_readback`

记录级 readback：

- `status`: `open | aligned | drift | blocked | needs_evidence | inactive`
- `summary`
- `open_count`
- `aligned_count`
- `drift_count`
- `blocked_count`
- `needs_evidence_count`

成员级 readback：

- `status`
- `summary`
- `issues:[{kind,label,tone,details?}]`
- `current_facts`：当前 lifecycle、usage、renewal decision、active subscription、service/domain/target/monitoring 计数和 running target/monitoring 计数。

状态聚合：

- `record.status=abandoned` -> inactive。
- 任一成员 drift -> record drift。
- 无 drift 且任一成员 blocked -> record blocked。
- 无 drift/blocked 且任一成员 needs_evidence -> record needs_evidence。
- 全部成员 aligned/skipped -> record aligned。
- 其他 -> record open。

## Readback Rules

成员回读使用 `decided_action`，为空时回退 `suggested_action`。

- `cancel` / `open_cancellation_workbench`：VPS 为 `to_cancel|cancelled|archived` 且无 active subscription、running monitoring、running target 时 aligned；若 follow-up done 但仍有这些冲突则 drift。
- `migrate`：`renewal_decision=migrate|replaced` 或 `lifecycle_status=to_migrate` 视为进入迁移链路；follow-up done 后旧 VPS 仍承载 service/domain/running target 时 drift。
- `keep`：VPS 未取消/归档且 `renewal_decision=keep` 时 aligned；done 后不匹配为 drift。
- `observe`：VPS 未取消/归档且 `renewal_decision=observe` 时 aligned；未完成时可保持 open；done 后不匹配为 drift。
- `complete_evidence`：当前没有已有 evidence gap 时 aligned；否则 needs_evidence，done 后仍未补齐为 drift。
- `review`：未关闭时 open；done/skipped 且无关键割裂时 aligned。

成员优先级：

- 当前 facts 缺失 -> drift + `current_fact_missing`。
- follow-up blocked -> blocked，除非存在关键 drift。
- follow-up skipped -> 抑制普通 open，但不抑制关键 drift。
- follow-up done 且事实不一致 -> drift。

## Store Data Flow

- `GetRecord`：读取 summary、members、当前 facts，返回带 readback 的 detail。
- `PatchRecord`：事务提交后调用 `GetRecord`，因此返回最新 readback。
- `CreateRecord`：创建记录后用已加载的 group facts 直接计算 readback，必要时复用当前 facts map。
- `ListRecords`：一次读取 summaries，一次 `loadFacts`，一次批量读取所有 record members，按 `record_id` 聚合 readback，避免 N+1。

facts 查询失败时 fail closed，返回 repository error。不得把未知事实伪造成 aligned、missing evidence 或 drift。

## Frontend Contract

`web/src/lib/types.ts` 增加 readback 类型，现有 API helper 名称不变。

`AssetDecisionsPage`：

- 已保存记录列表展示 readback badge、summary 和 drift / blocked / needs_evidence 计数。
- 记录详情顶部 summary 增加执行回读。
- 成员表增加“当前回读”列，展示状态、当前事实摘要和 issue chips。
- 既有“跟进”列继续负责人工 follow-up 状态、备注和保存动作。
- readback 不触发任何业务对象写请求。

## Boundaries

- 不新增 migration。
- 不新增 endpoint。
- 不读取 runtime facts detail，不接 HostSample / ProbeObservation。
- 不判断 IP 质量、路由质量、性能衰退、CPU/IO 趋势、超售。
- 不自动完成 record，不自动修改成员跟进，不批量执行取消或迁移。
