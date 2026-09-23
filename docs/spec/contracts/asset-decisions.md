# 资产组合决策合同

## Scenario: Asset decision portfolio read model and memory layer

### 1. Scope / Trigger

- Trigger: 修改 `internal/center/assetdecisions/`、`internal/center/store/asset_decisions.go`、`/api/asset-decisions/*`、`db/migrations/*asset_decision*`，或任何依赖 VPS / Subscription / Service / Domain / MonitoringInstance / Target 聚合生成组合决策组和决策记录的逻辑。
- 目标：`/asset-decisions` 是资产组合决策中枢。自动组仍是只读派生 read model；手工组合是用户定义的 scenario layer，只保存场景、成员意图和备注；决策记录是独立 memory layer，只保存用户判断和证据快照。三层都不成为第二套 VPS / Subscription / MonitoringInstance / Target 状态机。

### 2. Signatures

- Domain package: `internal/center/assetdecisions`，包含 `Repository`、`ListFilters`、`Overview`、`GroupSummary`、`GroupDetail`、`GroupMember`、`ManualGroupSummary`、`ManualGroupDetail`、`ManualGroupMember`、`ScenarioTemplateSummary`、`ScenarioTemplateDetail`、`ScenarioTemplateMember`、`RecordSummary`、`RecordDetail`、`RecordMember`、`CreateRecordInput`、`PatchRecordInput`、`ErrAssetDecisionGroupNotFound`、`ErrAssetDecisionManualGroupNotFound`、`ErrAssetDecisionScenarioTemplateNotFound`、`ErrAssetDecisionRecordNotFound`、`ErrInvalidAssetDecisionInput`。
- Backend APIs:
  - `GET /api/asset-decisions/overview?view=&renew_within_days=&provider_id=&vps_id=&country=&region=&city=&scenario=`
  - `GET /api/asset-decisions/groups?view=&renew_within_days=&provider_id=&vps_id=&country=&region=&city=&scenario=`
  - `GET /api/asset-decisions/groups/{group_id}?renew_within_days=`
  - `GET /api/asset-decisions/records`
  - `POST /api/asset-decisions/records`
  - `GET /api/asset-decisions/records/{record_id}`
  - `PATCH /api/asset-decisions/records/{record_id}`
  - `GET /api/asset-decisions/manual-groups?view=&renew_within_days=&provider_id=&vps_id=&country=&region=&city=&scenario=`
  - `POST /api/asset-decisions/manual-groups`
  - `GET /api/asset-decisions/manual-groups/{manual_group_id}`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}`
  - `POST /api/asset-decisions/manual-groups/{manual_group_id}/members`
  - `PATCH /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
  - `DELETE /api/asset-decisions/manual-groups/{manual_group_id}/members/{vps_id}`
  - `GET /api/asset-decisions/scenario-templates`
  - `POST /api/asset-decisions/scenario-templates`
  - `GET /api/asset-decisions/scenario-templates/{template_id}`
  - `PATCH /api/asset-decisions/scenario-templates/{template_id}`
  - `POST /api/asset-decisions/scenario-templates/{template_id}/manual-groups`
- Store source tables: `vps_assets`、`providers`、`subscriptions`、`asset_services`、`asset_domains`、`vps_monitoring_instance_links`、`monitoring_instances`、`targets`。
- Manual scenario tables: `asset_decision_manual_groups`、`asset_decision_manual_group_members`。manual group id 使用 `admg_*`；成员引用现有 `vps_assets.vps_id`，只保存 `intended_role`、`intended_action`、`reason`、`note`、`sort_order` 和创建时 evidence snapshot。
- Scenario template tables: `asset_decision_scenario_templates`、`asset_decision_scenario_template_members`。内置模板使用确定性 ID `adt_builtin_<scenario>` 且由代码返回，不允许 PATCH；自定义模板使用 `adt_*`，只保存场景 blueprint（status、scenario、title、goal、note、source_manual_group_id 和可选成员 intended role/action/reason/note/sort_order），不得保存当前成本、订阅、监控、服务、域名或 Target 实时事实。
- Decision memory tables: `asset_decision_records`、`asset_decision_record_members`；view: `asset_decision_records_with_counts`。`source_type` 允许 `auto_group` 与 `manual_group`；未传 source type 时默认 `auto_group` 以兼容旧调用。
- Stable group id: `adg_auto_<12hex>`，由 group type、scope key 和续费窗口等只读 key 确定性派生；detail endpoint 每次重新计算组列表后按 ID 查找。
- Decision record id: `adr_*`，由 `ids.New("adr")` 生成；记录只引用来源自动组 ID 作为历史来源，不把自动组 ID 当长期外键。
- Evidence assessment: `GroupSummary` 和 `GroupMember` 必须返回 `evidence_assessment`，字段固定为 `confidence_score`、`pressure_score`、`readiness_score`、`quality_tier`（`strong|usable|weak|blocked`）、`decision_bias`（`keep|observe|complete_evidence|retire|migrate|review`）、`support_signal_count`、`risk_signal_count`、`gap_signal_count`、`summary`。
- Decision recommendation: `GroupSummary`、`GroupMember`、`ManualGroupSummary`、`ManualGroupDetail` 和 manual members 必须返回只读 `decision_recommendation`，字段固定为 `summary`、`next_step`、`reasons[]`、`blockers[]`、`priority_vps_ids[]`、`confidence_label`。它只能解释 `evidence_assessment`、evidence chips、group type、scenario 和已有成员事实计数，不得新增评分引擎、runtime facts detail、HostSample、ProbeObservation、路由/性能/超售判断。
- Comparison insight: `GroupSummary`、`GroupMember`、`ManualGroupSummary`、`ManualGroupDetail` 和 manual members 必须返回只读 `comparison_insight`，用于解释同组成员差异，不是新的执行层。组级字段固定为 `summary`、`primary_axis`（`renewal|cost|service_context|monitoring|evidence|lifecycle|review`）、`lane_counts[]`、`priority_vps_ids[]`、`tradeoffs[]`；成员级字段固定为 `rank`、`lane`（`primary|standby|observe|retire|evidence|review`）、`summary`、`strengths[]`、`risks[]`、`gaps[]`、`tradeoffs[]`。`tradeoffs/strengths/risks/gaps` item 使用 `kind,label,tone,details?`。
- Record member follow-up: `asset_decision_record_members.followup_status` 固定为 `todo|in_progress|blocked|done|skipped`，`followup_note` 为 trim 后的执行备注，`followup_updated_at` 为最后一次成员跟进更新时间；`asset_decision_records_with_counts` 必须返回各状态聚合计数。
- Execution readback: `RecordSummary` / `RecordDetail` 和 `RecordMember` 必须返回只读派生字段 `execution_readback`。记录级字段为 `status`（`open|aligned|drift|blocked|needs_evidence|inactive`）、中文 `summary`、`open_count`、`aligned_count`、`drift_count`、`blocked_count`、`needs_evidence_count`。成员级字段为同一 status、summary、`issues[]`（`kind,label,tone,details?`）和 `current_facts`（当前 VPS lifecycle、usage、renewal decision、active subscription / service / domain / Target / monitoring 计数与 source availability）。
- Execution plan: records API 响应必须在 readback 之后同步返回只读派生字段 `execution_plan`，但不新增 endpoint / migration。记录级字段为中文 `summary`、`lane_counts[]`、`actionable_count`、`blocked_count`；成员级字段为 `lane`（`cancel_retire|migration|keep_observe|evidence|review`）、`step_kind`（`open_cancellation_workbench|open_vps_detail|open_subscription_context|review_record`）、`tone`（`critical|alert|notice|normal|neutral`）、中文 `summary`、`step_label`、`issue_count`、`blocked`、`actionable`。
- Execution plan 只能消费当前 `execution_readback` 与 `loadFacts` 已有事实，不能引入第二套执行状态机。后端只返回 step kind 等语义，不得返回 SPA 路由字符串；URL 深链由前端根据 step kind 本地映射。

### 3. Contracts

- 自动组只读派生，不写数据库；手工组合只写 `asset_decision_manual_groups` / `asset_decision_manual_group_members`；用户保存一次判断才写入 `asset_decision_records` / `asset_decision_record_members`。
- 场景模板只能创建或预填自定义组合，不能直接创建决策记录，不能修改 VPS / Subscription / MonitoringInstance / Target / Service / Domain。`POST /scenario-templates/{id}/manual-groups` 必须重新读取当前 facts 后复用 manual group 创建路径；模板成员缺失、重复或非法输入必须 fail closed。
- 手工组合支持 `source_type=manual` 和 `source_type=auto_group`。从自动组创建手工组合时，store 必须重新读取当前 facts 并定位自动组，复制当前成员建议角色/动作与 evidence snapshot；自动组不存在或请求成员不属于组时返回 invalid/not found，不得信任前端传入的成员事实。
- 手工组合详情和列表必须复用当前 `loadFacts` 聚合实时回读成员事实；成员当前 VPS facts 缺失时仍返回 manual metadata，并展示 `current_fact_missing` evidence chip，不得静默丢成员。
- 手工组合成员增删改只能修改 manual member 行，不得修改 VPS、Subscription、MonitoringInstance、Target、Service、Domain 或决策记录跟进状态。手工组合没有 hard delete endpoint；归档使用 `status=archived`。
- 创建决策记录时必须重新读取当前事实。`source_type=auto_group` 通过 `FindGroup` 定位 `source_group_id`；`source_type=manual_group` 通过 manual group detail 生成 group/member snapshot 并使用成员 intended role/action/reason 作为决定默认值。来源不存在返回 404，不得按前端传入的成员列表凭空创建记录。
- 决策记录必须保存组级来源字段、标题、目标、状态、组级 `evidence_snapshot`，并为组内每台 VPS 保存系统建议角色/动作、用户决定角色/动作、成员理由和成员级 `evidence_snapshot`。
- 决策记录状态固定为 `draft`、`decided`、`in_progress`、`completed`、`abandoned`；PATCH 可更新记录级标题、目标、状态，以及记录内已有成员的 `followup_status` / `followup_note`，但不执行 VPS / Subscription / MonitoringInstance / Target 业务动作。
- 成员跟进 PATCH 的 payload 为 `members:[{vps_id, followup_status?, followup_note?}]`；`vps_id` 必须属于当前记录，同一 payload 不得重复，状态必须合法，状态或备注至少设置一项。成功更新成员跟进时必须刷新成员 `followup_updated_at`、成员 `updated_at` 与记录 `updated_at`，并返回 detail 风格的最新记录。
- 成员全部 `done` / `skipped` 不得自动推进整条决策记录状态；组合决策记录状态仍由用户显式修改，避免在 memory layer 内扩张隐式状态机。
- 执行回读只校验“保存的组合判断是否与当前事实一致”，不得变成第二套状态机：records API 不自动 PATCH record status，不自动完成成员跟进，不自动修改 VPS / Subscription / MonitoringInstance / Target。
- 执行编排只把已保存判断组织为下一步导览，不执行真实动作：records API 不自动 PATCH VPS / Subscription / MonitoringInstance / Target，不自动 PATCH record status，不自动改写成员 `decided_action` / `decided_role`。若用户判断需要改写，路径是 abandon 旧记录后从自定义组合或自动组保存新记录。
- 成员回读以 `decided_action` 为主，历史值为空才回退 `suggested_action`。`cancel` / `open_cancellation_workbench` 只判断 VPS 是否进入 `to_cancel|cancelled|archived` 且无 active subscription、无 running monitoring、无 running target；`migrate` 只判断是否进入迁移链路（`renewal_decision=migrate|replaced` 或 `lifecycle_status=to_migrate`），不判断新 VPS 是否已替代旧 VPS；`keep` / `observe` 只检查 lifecycle 未取消/归档和 renewal decision 是否相符；`complete_evidence` 只检查当前已有证据缺口。
- 回读状态优先级：`record.status=abandoned` 为 `inactive`；成员 `followup_status=blocked` 优先 `blocked`，但 `done` 后关键事实不一致仍为 `drift`；`skipped` 抑制普通 open，但不隐藏关键 drift；存在证据缺口为 `needs_evidence`；事实与动作一致为 `aligned`。记录级聚合优先级为 drift > blocked > needs_evidence > aligned > open。
- 成员级 `decided_action=cancel` 或 `open_cancellation_workbench` 只能给前端提供跳转到 VPS lifecycle workbench 的入口；后端 records API 不做批量取消、批量退役或批量迁移。
- 成员级 execution plan 的 cancel / retire lane 只能编排到 `open_cancellation_workbench`；migration lane 只能编排到 VPS detail 复核迁移意向并人工跟进，`step_label` 不得写成“推进迁移”或暗示已有迁移工作台；evidence lane 对缺订阅优先 `open_subscription_context`，其余证据缺口走 VPS detail；`current_fact_missing`、空动作或不能安全归类的成员必须走 `review_record`。
- Group type 固定语义：`renewal_attention`、`cancellation_attention`、`region_portfolio`、`provider_portfolio`、`cost_pressure`、`evidence_gap`。
- `renew_within_days` 默认 30，仅允许产品认可的窗口（当前 `30/60/90`）；非法值在 handler 返回 400。
- `view` 只筛选返回的自动组，不改变底层事实读取；`provider_id`、`vps_id`、`country`、`region`、`city`、`scenario` 是列表上下文筛选，只筛出相关组/手工组合/记录，不裁剪 group detail 成员；非法值返回 400。
- Store 读取现有表后在 Go 中派生组合摘要和成员建议，避免 Dashboard / VPS / Subscription / Provider 页面各自重复 join 后语义漂移。
- 组级摘要可以聚合成本、续费窗口、取消联动、服务 / 域名 / Target、监控关联、异常和 evidence chips；成员级建议角色 / 建议动作只能作为扫描提示，不执行写操作。
- `evidence_assessment` 是只读、可解释评分层，只消费当前 `GroupMember` / `GroupSummary` 已有事实、source availability 和 evidence chips；它不得新增数据库读取、逐台 runtime facts 调用或执行语义，也不得把评分当成自动 keep / migrate / cancel 写入。
- `comparison_insight` 是只读、可解释的组合对比层，只消费当前 `evidence_assessment`、`decision_recommendation`、evidence chips、组类型、成员建议角色/动作和已有成员事实计数；它不得新增独立 scoring engine、不得读取新增表、不得逐台调用 runtime facts detail，也不得使用 路由质量、性能衰退、CPU/IO、超售或 HostSample / ProbeObservation 语义。成员 lane / rank 必须稳定可测，用于 UI 的证据矩阵和优先核对顺序，不可触发写操作。
- 自定义组合详情必须对所有 manual members 返回 comparison insight。成员当前 facts 缺失时不得静默丢成员，必须保留 manual metadata，并生成 `current_fact_missing` evidence / comparison gap，使 UI 能解释“组合成员仍在，但当前事实不可回读”。
- 证据源不可用时只能降低 `confidence_score` / `readiness_score` 并增加 gap 计数；不得把 `subscription_unavailable`、Monitoring/Target/Service/Domain 查询失败解释为真实 `missing_subscription` / `missing_monitoring` 业务事实。
- 决策记录回读必须 fail closed：当前事实查询失败时 records list/detail/create/patch 返回 repository error，不得把未知事实伪造成 `aligned`、`needs_evidence` 或 `drift`。成员存在但当前 facts 中找不到对应 VPS 时，成员 readback 为 `drift` 且 issue kind 为 `current_fact_missing`。
- `RecordSnapshotFromGroup` 与 `RecordSnapshotFromMember` 必须把当时的 `evidence_assessment` 与 `comparison_insight` 写入 `evidence_snapshot`，用于记录详情回看保存时的判断基础；旧记录缺少这些字段时前端必须可降级显示，后端不要求 backfill。
- archived VPS 不进入普通 region/provider/cost/evidence 组合；cancelled/to_cancel 只能作为取消联动相关证据出现，避免归档资产污染正常组合比较。
- 订阅、服务、域名、监控或 Target 查询失败必须返回 repository error；不得构造“健康”或“缺证据”假结果。只有查询成功且事实为空时，才生成 `missing_subscription`、`unlinked_monitoring` 等真实 evidence gap。
- `/api/asset-decisions/*` 不逐台调用 runtime facts detail endpoint，只读 MonitoringInstance / Target 当前摘要字段和关联计数；已有低频 IP 质量摘要可以进入 evidence chips、scoring 和 readback，按 [IP 质量合同](ip-quality.md) 保留来源与时效；CPU / IO / 路由 / 超售判断仍属于后续能力。
- 执行回读同样只能复用 `loadFacts` 聚合事实，不得逐台请求 runtime facts detail、HostSample、ProbeObservation、agent 性能趋势、路由质量。路由 / 性能衰退 / CPU / IO / 超售判断等待观测语义成熟后再进入模型。
- 执行编排同样只能复用 readback / `loadFacts` 聚合事实，不得为了生成下一步导览逐台请求 runtime facts detail、HostSample、ProbeObservation、agent 性能趋势、路由质量或性能衰退信号。
- 组合页仍只通过既有 `PATCH /api/vps/{id}` 改单台 VPS renewal decision；取消 / 退役执行必须回到 VPS lifecycle workbench。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| invalid `view` | handler 返回 400 `invalid input` |
| invalid `renew_within_days` | handler 返回 400 `invalid input` |
| missing `group_id` | handler 返回 404 `asset decision group not found` |
| missing `record_id` | handler 返回 404 `asset decision record not found` |
| missing `manual_group_id` | handler 返回 404 `asset decision manual group not found` |
| missing manual group member | handler 返回 404 `asset decision manual group member not found` |
| missing scenario template | handler 返回 404 `asset decision scenario template not found` |
| patch builtin scenario template | handler 返回 400 `invalid input`，内置模板由代码版本化 |
| create record with missing auto group | handler 返回 404 `asset decision group not found` |
| create record with missing manual group | handler 返回 404 `asset decision manual group not found` |
| create record with member not in group | handler 返回 400 `invalid input`，不得开启写事务 |
| create manual group with duplicate members | handler 返回 400 `invalid input`，不得写入半成品 |
| manual group member current facts missing | detail/list 保留成员并显示 `current_fact_missing`，但从该手工组创建 record 应 fail closed 返回 invalid input |
| patch record with invalid status/title | handler 返回 400 `invalid input` |
| patch record member with unknown `vps_id` | handler 返回 400 `invalid input`，事务回滚 |
| patch record member with duplicate `vps_id` in payload | handler 返回 400 `invalid input`，不得开启写事务 |
| patch record member with invalid follow-up status | handler 返回 400 `invalid input`，不得写入成员 |
| repository query failure | handler 返回 500 `internal server error`，store error 用 `%w` 包装 |
| source table query failed | 不返回 evidence gap；整体 request fail，避免误报缺订阅 / 缺监控 |
| source query succeeded but rows empty | 返回可解释的 `evidence_gap` 或空组，不伪造健康证据 |
| source availability false but fact rows unknown | `evidence_assessment` 降低可信度并提示证据不可用；不得生成真实缺订阅 / 缺监控 chip |
| old decision record snapshot without `evidence_assessment` | API 正常返回历史 snapshot；前端显示缺失评估，不要求后端 backfill |
| record `status=abandoned` | `execution_readback.status=inactive`，不参与执行推进提示 |
| member followup `done` but current facts still not closed | 成员 `execution_readback.status=drift`，记录级聚合为 `drift` |
| member followup `blocked` | 成员优先显示 `blocked`，记录级无 drift 时聚合为 `blocked` |
| record status `completed` but facts drift | `execution_readback.status=drift` 且 `execution_plan.actionable_count>0`，不得因 completed 掩盖漂移 |
| record status `abandoned` | `execution_readback.status=inactive`，`execution_plan` 不产生可执行项 |
| member has `current_fact_missing` | 成员 plan 使用 `lane=review` + `step_kind=review_record`，不得跳到业务执行页 |
| record member VPS missing from current facts | 成员 `drift`，issue kind 为 `current_fact_missing` |
| unsupported method | handler 返回 405 `method not allowed` |
| `/api/asset-decisions/*` route missing | router test 必须失败；该路径不得落 SPA fallback |

### 5. Good/Base/Bad Cases

- Good: 同一国家 / region / city 下两台 active VPS 自动形成 `region_portfolio`，成员显示成本、用途、服务 / 域名 / 监控差异，帮助用户取舍。
- Good: Provider 下多台 active VPS 自动形成 `provider_portfolio`，用于服务商组合比较。
- Good: subscription query 成功且某台 VPS 没有 active subscription，`evidence_gap` 或成员 evidence chips 标记缺订阅。
- Good: 用户打开自动组，保存为 `adr_*` 决策记录；记录保留当时成本、服务/域名/Target、监控和成员建议快照，后续只推进记录状态。
- Good: 用户把自动组保存为 `admg_*` 手工组合，随后调整成员 intended role/action/reason，再从手工组合保存为 `source_type=manual_group` 的 `adr_*` 记录。
- Good: 用户从内置场景模板创建自定义组合，后端重新读取当前 facts；用户把手工组合另存为自定义模板时只保存成员 blueprint，不保存当前成本、监控或订阅事实。
- Good: `/api/asset-decisions/groups?view=provider&provider_id=pv_001` 只返回与该服务商相关的组合；打开其中某个 `group_id` 的详情仍保留完整组成员，不按 provider/vps 筛选裁剪。
- Good: 用户给手工组合新增一台现有 VPS，只保存手工组合成员行；VPS 的 lifecycle、renewal decision、订阅、监控和 Target 均不被修改。
- Good: 用户把记录中某台 VPS 标记为 `blocked` 并记录“等待迁移窗口”，API 只更新该 record member 的跟进字段与记录 `updated_at`，不修改 VPS lifecycle 或 subscription。
- Good: 已保存记录中 `cancel` 成员跟进标记 `done` 后，如果当前仍有 active subscription 或 running target，readback 显示 `drift`，提示“跟进已完成但事实未闭环”。
- Good: 已保存记录的成员 facts 找不到对应 VPS 时，readback 显示 `current_fact_missing` 而不是伪造已对齐。
- Good: 已保存记录的 drift / blocked / needs_evidence 成员返回 execution plan，前端据此打开记录详情、VPS 详情、订阅上下文或取消工作台；后端响应仍只包含语义 step kind。
- Good: 完整证据的同区组合返回较高可信度/准备度与 `quality_tier=strong`，资料缺口或来源不可用返回较低可信度与 `decision_bias=complete_evidence`。
- Base: 没有任何 VPS 时 overview 仍返回 0 计数和空 `top_groups`。
- Bad: 在 store 里写入 `asset_decision_groups` 表，或把自动组 ID 当长期外键依赖。
- Bad: 手工组合成员保存当前成本、订阅、监控、服务等实时事实并长期展示，不再从 `loadFacts` 回读当前状态。
- Bad: 模板 API 直接生成 `adr_*` 决策记录，或把模板当作第二套业务状态机保存执行状态。
- Bad: `decision_recommendation` 读取 agent CPU/IO、HostSample、ProbeObservation、路由质量或性能衰退数据。
- Bad: `PATCH /api/asset-decisions/records/{id}` 同时修改 VPS renewal decision、Subscription 状态或执行取消/退役。
- Bad: records list 为了给每条记录计算 readback 逐条调用 `GetRecord`，造成 N+1。
- Bad: records list 为了给每条记录计算 execution plan 逐条调用 `GetRecord`，造成 N+1。
- Bad: 后端 `execution_plan` 返回 `/vps/{id}`、`/subscriptions?...` 等 SPA URL 字符串，把 API contract 与前端路由耦合。
- Bad: readback 使用 HostSample、ProbeObservation、路由质量或性能衰退数据，在 agent 语义未成熟前给出超售判断。
- Bad: group detail 为了展示性能趋势逐台请求 runtime facts detail endpoint，造成 N+1 和语义越界。
- Bad: subscriptions 查询失败后把所有 VPS 标记为 `missing_subscription`，误导用户取消资产。

### 6. Tests Required

- Domain tests: stable group id、view/window validation、record input validation、snapshot builder、renewal/cancellation/region/provider/cost/evidence group derivation、archived/cancelled 边界、source unavailable 不误报。
- Domain assessment tests: 完整证据、资料缺口、证据源不可用、取消联动 / 预算压力、record snapshot 均断言 `evidence_assessment` 的 tier / bias / score 方向。
- Store tests: member facts 聚合、主订阅选择、服务 / 域名 / Target / 监控计数、成本和 evidence chips，manual groups list/create/get/patch/member add/patch/delete、records list/create/get/patch、成员跟进计数、成员跟进事务更新与未知成员回滚，且不依赖 runtime facts detail。
- Execution readback domain tests: cancel / cancellation workbench aligned/open/drift、migrate 链路与旧承载 drift、keep / observe 一致性、complete_evidence 只检查当前已有缺口、done drift、blocked 优先、skipped 抑制普通 open、abandoned inactive、current fact missing。
- Store tests: records list/detail/create/patch 均返回 `execution_readback`；ListRecords 批量读取成员并聚合，不逐条调用 `GetRecord`；facts 查询失败 fail closed；成员跟进 PATCH 后 readback 随响应刷新；不依赖 runtime facts detail / HostSample / ProbeObservation。
- Store tests: records list/detail/create/patch 均返回 `execution_plan`；plan 派生沿用 records/facts/members 的批量读取路径，不逐条调用 `GetRecord`；成员跟进 PATCH 后 readback 与 execution plan 同步刷新。
- Handler tests: overview、groups list、group detail、manual groups list/create/detail/patch/member add/patch/delete、records list/create/detail/patch success 且 records 响应包含 readback、成员跟进 patch；invalid query/input、missing group/manual group/member/record、未知或重复成员、repo failure、method not allowed。
- Handler tests: scenario templates list/create/get/patch/create-manual-group success；builtin PATCH、missing template、invalid template input、repo failure、method not allowed。
- Router/bootstrap tests: `/api/asset-decisions/overview`、`/api/asset-decisions/groups`、`/api/asset-decisions/groups/{id}`、`/api/asset-decisions/manual-groups/*`、`/api/asset-decisions/scenario-templates/*`、`/api/asset-decisions/records`、`/api/asset-decisions/records/{id}` 登录保护且不落 SPA fallback；`bootstrapCenter` wiring 非 nil。
- Frontend tests: API helper query/payload、页面主 surface、saved records surface、tabs、group detail、保存记录、记录详情状态推进、单台 PATCH payload、evidence 请求失败边界。

### 7. Wrong vs Correct

```go
// 错误：证据源失败时构造“缺订阅”假事实。
if err != nil {
    member.EvidenceChips = append(member.EvidenceChips, EvidenceChip{Kind: "missing_subscription"})
    return groups, nil
}
```

```go
// 正确：查询失败是 source unavailable，交给 handler 返回错误或前端显示局部失败。
if err != nil {
    return nil, fmt.Errorf("list asset decision facts: %w", err)
}
```

```go
// 错误：详情依赖已经保存的自动组状态。
return repo.loadPersistedGroup(ctx, groupID)

// 正确：每次重新派生组，再按稳定 ID 查找。
groups := DeriveGroups(facts, filters)
return FindGroup(groups, groupID)
```

```go
// 错误：成员跟进完成后隐式修改 VPS 或整条记录状态。
if member.FollowupStatus == assetdecisions.FollowupDone {
    _, _ = tx.Exec(ctx, `update vps_assets set lifecycle_status = 'cancelled' where vps_id = $1`, member.VPSID)
}
```

```go
// 正确：跟进只是决策记录成员的执行记忆，真实动作回到对应业务页面。
_, err := tx.Exec(ctx, `
    update asset_decision_record_members
    set followup_status = $1, followup_note = $2, followup_updated_at = now(), updated_at = now()
    where record_id = $3 and vps_id = $4`,
    status, note, recordID, vpsID)
```
