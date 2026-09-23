# 资产决策 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

### Asset Ledger 组合决策与列表数据流

Asset Ledger 的列表页可以把现有 VPS 与 Subscription contract 在前端做轻量 join，用于人工核对和资料质量提示；这不是后端字段扩展，也不能创造未存在的健康语义。`/asset-decisions` 例外地使用后端组合决策 read model 作为主语义来源：它把 VPS、订阅、服务、域名、Target 和监控关联聚成自动决策组；同时通过手工组合 scenario layer 保存用户正在比较的真实问题篮子，通过决策记录 memory layer 保存一次判断和证据快照。手工组合和记录层都不拥有 VPS / Subscription / MonitoringInstance / Target 的执行状态机。

#### 1. Scope / Trigger

- Trigger: 修改 `web/src/pages/AssetDecisionsPage.tsx`、`web/src/pages/VPSPage.tsx`、`web/src/pages/assetPageUtils.ts`、`AssetDecisionWorkPanel`、或改变 VPS/Subscription 列表页的筛选 URL-state。

#### 2. Signatures

- Frontend API: `getAssetDecisionOverview(filter?)`, `listAssetDecisionGroups(filter?)`, `getAssetDecisionGroup(groupId, filter?)`, `listAssetDecisionManualGroups(filter?)`, `createAssetDecisionManualGroup(input)`, `getAssetDecisionManualGroup(manualGroupId)`, `patchAssetDecisionManualGroup(manualGroupId, input)`, `addAssetDecisionManualGroupMember(manualGroupId, input)`, `patchAssetDecisionManualGroupMember(manualGroupId, vpsId, input)`, `deleteAssetDecisionManualGroupMember(manualGroupId, vpsId)`, `listAssetDecisionScenarioTemplates()`, `createAssetDecisionScenarioTemplate(input)`, `getAssetDecisionScenarioTemplate(templateId)`, `patchAssetDecisionScenarioTemplate(templateId, input)`, `createManualGroupFromScenarioTemplate(templateId, input)`, `listAssetDecisionRecords(filter?)`, `createAssetDecisionRecord(input)`, `getAssetDecisionRecord(recordId)`, `patchAssetDecisionRecord(recordId, input)`, `listVPSAssets(filter?)`, `listSubscriptions(filter?)`, `listProviders()`, `updateVPSAsset(vpsId, input)`。`listVPSAssets` / `listSubscriptions` 支持 `asset_scope='current'|'historical'|'archived'|'all'`；普通页面不传 scope，使用后端默认 current；归档列表页显式传 `historical`；`archived` 只作为旧客户端兼容别名；归档详情订阅历史使用 `all`。`updateVPSAsset` 仍返回 VPS record 字段，并可在取消类续费决策响应中附带 `renewal_subscription_linkage` 状态摘要。取消 / 退役协同使用 `getVPSCancellationPreview(vpsId)`、`applyVPSCancellation(vpsId, input)`、`listTargetAssetContexts()`；归档 / 恢复使用 `getVPSArchiveReview(vpsId)`、`archiveVPS(vpsId, input)`、`restoreVPSFromArchive(vpsId)`。页面不得直接 `fetch()`。
- Route-scoped controller signatures: `useAssetDecisionRouteState`、`useAssetDecisionPortfolio`、`useAssetDecisionGroups`、`useAssetDecisionManualGroups`、`useAssetDecisionTemplates`、`useAssetDecisionRecords`、`useAssetDecisionRenewalQueue` 均只返回 `{state, commands}`。`useAssetDecisionRouteState` 是唯一 `useSearchParams` / `useNavigate` owner；六个数据 controller 按各自 API 白名单拥有读取、mutation、错误与局部 UI state。
- Revalidation identity: `assetDecisionFilterKey(filter)` 是判断 filtered UI 是否仍代表同一语义查询的唯一 owner。route 的完整 `searchParams` identity 仍作为 effect dependency，以保留 open key 变化时 overview / groups / manual groups / records 四个 GET；UI currency 不得使用对象引用相等判断。
- Cross-domain invalidation: `renewal-decision-saved` 是唯一语义事件，`applyAssetDecisionInvalidation` 把它映射到 portfolio / groups / manualGroups / templates / records / renewalQueue 六个 revision；controller 不直接调用另一个 controller。
- Asset portfolio data: `AssetDecisionsPage` 首屏主 surface 从 `/api/asset-decisions/overview` 与 `/api/asset-decisions/groups?view=&renew_within_days=` 读取自动组；点击组时读取 `/api/asset-decisions/groups/{group_id}?renew_within_days=`。自动组是只读派生视图，不在前端保存或补写 group state；保存决策必须调用 `/api/asset-decisions/records`，由后端重新计算组详情并生成快照。
- Manual scenario data: 页面读取 `/api/asset-decisions/manual-groups` 展示“自定义组合” surface；点击组合读取 `/api/asset-decisions/manual-groups/{manual_group_id}`。从自动组创建手工组合 POST `source_type=auto_group`、`source_group_id`、`renew_within_days`、`scenario`、`title`、`goal`、`note`；成员增删改只调用 manual group member endpoints。新增成员必须用 VPS selector，从 `listVPSAssets()` 候选选择，不要求用户复制内部 ID。
- Scenario template data: 页面读取 `/api/asset-decisions/scenario-templates` 展示“场景模板” surface。内置模板只能用于创建自定义组合，不能 PATCH；自定义模板可从手工组合另存并可归档/启用。模板详情 `template_id` 深链只读取模板；从模板创建组合必须调用 `createManualGroupFromScenarioTemplate`，成功后打开新 manual group。模板不能直接创建 record，不能写 VPS / Subscription / MonitoringInstance / Target。
- Asset Decisions 的成功响应仍是运行时边界：Go `nil` slice 会编码成 JSON `null`，即使手写 TypeScript 把 `members`、`evidence_chips`、`monthly_cost_by_currency` 等字段声明为数组。对成功响应中的集合执行 `filter` / `map` / `some` 前，拥有该展示或 controller 边界的 helper 必须把 `null | undefined` 收口为空数组；这只处理字段级空集合，不得把请求失败或整个响应缺失伪装成成功空态。
- Asset decision records data: 页面读取 `/api/asset-decisions/records` 展示“已保存组合决策”辅助 surface；点击记录读取 `/api/asset-decisions/records/{record_id}`；推进记录状态 PATCH `/api/asset-decisions/records/{record_id}` 的 `title` / `goal` / `status` 字段，成员跟进 PATCH 同 endpoint 的 `members:[{vps_id, followup_status?, followup_note?}]`，但不得修改 VPS/订阅/监控/Target。
- Asset decision execution plan data: `RecordSummary`、`RecordDetail` 和 `RecordMember` 可以包含只读 `execution_plan`，字段与后端 snake_case 对齐。记录级展示 `summary`、`lane_counts`、`actionable_count`、`blocked_count`；成员级展示 `lane`、`step_kind`、`tone`、`summary`、`step_label`、`issue_count`、`blocked`、`actionable`。前端只根据 `step_kind` 生成本地深链，后端不得返回 URL。
- Evidence assessment data: `AssetDecisionGroupSummary` 与 `AssetDecisionGroupMember` 必须包含 `evidence_assessment`；字段为 `confidence_score`、`pressure_score`、`readiness_score`、`quality_tier`、`decision_bias`、`support_signal_count`、`risk_signal_count`、`gap_signal_count`、`summary`。记录详情从 `evidence_snapshot.evidence_assessment` 读取保存时快照；历史记录缺失该字段时只显示降级文案。
- Decision recommendation data: `AssetDecisionGroupSummary`、`AssetDecisionGroupMember`、`AssetDecisionManualGroupSummary` 和 manual members 可以包含 `decision_recommendation`；UI 只展示短摘要、下一步、理由/阻塞 chips 和优先 VPS，不在前端重新评分，不把 recommendation 当作自动执行承诺。
- Comparison insight data: `AssetDecisionGroupSummary`、`AssetDecisionGroupMember`、`AssetDecisionManualGroupSummary` 和 manual members 可以包含只读 `comparison_insight`。组级字段为 `summary`、`primary_axis`、`lane_counts[]`、`priority_vps_ids[]`、`tradeoffs[]`；成员级字段为 `rank`、`lane`、`summary`、`strengths[]`、`risks[]`、`gaps[]`、`tradeoffs[]`。前端只展示后端 lane / rank / signals，不在浏览器重新分类或评分。记录详情从 `evidence_snapshot.comparison_insight` 读取保存时快照；历史记录缺失时显示“保存时未记录对比洞察”降级，不影响 readback / execution plan / followup。
- Single queue data: `AssetDecisionsPage` 底部辅助队列继续拉取续费窗口 subscriptions、全量 subscriptions（按 `renew_at asc`）、以及 `renewal_decision=unreviewed|migrate|cancel` 三个 VPS 切片。
- Asset Decisions URL-state: `view=needs_decision|renewal|region|provider|cost|evidence|single_queue`，`renew_within_days=30|60|90`，上下文筛选 `provider_id`、`vps_id`、`country`、`region`、`city`、`scenario`，打开对象 `group_id`、`manual_group_id`、`record_id`、`template_id`。非法 view 在前端降级为 `needs_decision`，后端 API 对非法 view/window/scenario 返回 400。筛选 chips 必须首屏可见并可单个移除/全部清空；打开对象只触发读取和展示，不触发创建或 PATCH。

#### 3. Contracts

- `AssetDecisionsPage.tsx` 是薄 composition coordinator：只组合七个 controller、纯 model、两个 workbench 与五个 modal；不得 import `lib/api`、调用 router hook / `useEffect`、解析响应或构造 request body。展示组件不得 import controller/API，controller 之间不得互相 import。
- 同值 filter 的新对象 identity 表示 revalidation，不是新业务状态：四个 filtered GET 必须照常发出，但已有 settled overview/list 保持 `loading=false`、错误与 rows 可见，入口 DOM 不卸载。filter key、external revision 或 local retry 变化才进入 loading；返回结果仍以 cancelled flag 防止旧响应覆盖当前 state。
- Asset Decisions 首屏必须是组合优先的单主路径：`portfolio command summary` → 次级工作区入口 → `决策组扫描`。默认页面不得恢复 `决策路径`、`下一步导览`、记录表、场景模板、自定义组合、续费 evidence 或单台队列作为同权常驻 section；也不得把单台续费队列重新提升为主视觉主体。
- Asset Decisions 顶部必须先展示 portfolio command summary：从当前已加载的记录回读、自动组、自定义组合和模板中派生第一行动，并展示组合范围、续费窗口、执行闭环风险、evidence source 状态和 context filter 摘要。该 summary 是处理顺序导览，不是 KPI 卡片墙；不得把单台队列数量提升为主指标，也不得因为某来源失败而伪造无问题。
- Asset Decisions 的记录、场景/模板、续费 evidence、单台队列必须通过受控的次级工作区进入；默认 `secondaryWorkbench=null`，同一时间最多展开一个次级区。用户点击入口或 URL 深链可展开对应区：`record_id -> records`，`manual_group_id|template_id -> scenarios`，`view=renewal -> renewals`，legacy `view=single_queue -> single_queue`。`group_id` 只打开自动组详情，不要求展开次级区。
- 次级工作区切换和关闭只影响本地展开状态，不主动删除 `view`、`renew_within_days`、`provider_id`、`vps_id`、`country`、`region`、`city`、`scenario` 等筛选上下文。打开对象参数只触发读取和展示，不触发创建、PATCH 或业务对象写入。
- portfolio command summary、决策组扫描和次级入口的指标只能从当前已加载 rows 派生，例如自动组数量、进行中自定义组合、未关闭记录、readback drift/blocked/needs_evidence/open、预算压力和资料缺口。任一来源加载失败时只显示局部不可用提示并跳过该来源的工作项；风险标签必须降级为 `证据待确认` 或等价未知态，不得把失败解释成无问题、已对齐、已闭环、`证据稳定` 或真实资料缺口。
- 已保存组合决策必须作为次级工作区展示，承接“保存本次判断、回看当时证据、推进记录状态”的用户任务，但不得取代自动组发现入口。
- 自定义组合必须作为自动组发现和已保存记录之间的 scenario surface：自动组回答“系统发现哪些组合问题”，自定义组合回答“用户正在比较哪些真实场景”，记录回答“某一次判断和后续跟进是什么”。自定义组合可编辑 title/goal/note/scenario/status 与成员 intended role/action/reason/note/sort，不得修改 VPS / Subscription / MonitoringInstance / Target。
- 场景模板是 scenario surface 的入口层，位于自动组和自定义组合附近，视觉权重低于自动组列表。模板只启动场景或从手工组合保存 blueprint；内置模板不允许编辑，自定义模板只能改模板元数据/归档状态。模板失败只影响模板 surface 或模板 modal，不影响自动组、手工组合、记录、续费 evidence 和单台队列。
- 自动组至少覆盖 `renewal_attention`、`cancellation_attention`、`region_portfolio`、`provider_portfolio`、`cost_pressure`、`evidence_gap`。组卡展示顺序应优先服务扫描：组名/scope、主问题、当前压力、推荐下一步、证据评估、成员数量、用途分布、成本、服务 / 域名 / Target、监控关联、异常和 evidence chips；避免把五个以上指标做成同权小格，让用户先读表再理解问题。
- 组详情必须展示成员 VPS 基础事实、主订阅、服务 / 域名 / Target / 监控摘要、`suggested_role`、`suggested_action` 和 evidence chips。建议只能帮助扫描和排序，不得自动提交 keep / migrate / cancel。
- `evidence_assessment` 的视觉层级高于零散 evidence chips、低于组合事实本身：组列表展示判断尺度，组详情展示组级和成员级评估，记录详情展示保存时证据快照。UI 文案必须表达“证据质量 / 决策压力 / 准备度”，不得把 `decision_bias` 写成自动执行承诺。
- `decision_recommendation` 的视觉层级与 evidence assessment 相邻但更偏“下一步提示”：列表里保持短摘要，详情里展示下一步和理由/阻塞 chips。不得在前端根据 recommendation 自动调用 `PATCH /api/vps/*`、`PATCH /api/asset-decisions/records/*` 或其他业务对象写接口。
- `comparison_insight` 的视觉层级用于“为什么这组资产要这么取舍”，但资产决策弹窗不能再把它渲染成默认展开的报告矩阵：组卡只展示短比较结论、lane counts 和 priority VPS；自动组、自定义组合和记录详情统一走 `Cover -> Directory -> Task Panel -> Raw` 分层。普通 `members` / `save` / `execution` / `source` 面板只能展示当前任务所需的短摘要、role/action、少量信号和单一主动作；完整 rank/lane、成本/产品、承载/监控、状态/续费、证据源、strength/risk/gap chips 和宽表事实只能进入 `raw` / `底稿` 入口。不得恢复 `GROUP TO SCENARIO`、`EVIDENCE MATRIX`、`证据矩阵` 或类似 heading 作为普通详情面板。
- 组详情可以把当前自动组创建为自定义组合，也可以直接保存当前自动组为决策记录。默认层只展示短判断和主动作；保存表单只在 `save` 面板出现，并允许编辑标题、组合目标、状态，以及逐个展开成员的决定角色、决定动作和理由。保存成功后展示记录详情，而不是继续停留在只读组详情中。
- 组详情的场景推进分岔只能作为短入口表达：`直接保存记录` 适用于当前自动组已经就是本次判断范围，`先创建自定义组合` 适用于还需要补成员、目标或人工语境。分岔不得带长解释块，不新增执行路径，不写业务对象。
- 自定义组合详情必须隔离 `members` / `edit` / `add` / `save` / `raw` 面板：成员扫描只展示当前 facts 回读后的短判断和成员意图对照；组合属性表单、VPS 选择器新增成员、成员意图编辑和保存为决策记录入口各自在对应面板出现，不得混排。完整 comparison lane、facts、evidence gap 和 current fact missing 只能在 raw/底稿面板兜底；保存记录必须发送 `source_type=manual_group`，并使用当前成员 intended role/action/reason 作为默认决定值。
- 记录详情默认必须先展示保存记录短封面；`查看详情` 进入目录后，`execution` 只做记录状态推进和可执行成员预览，`members` 只做成员跟进状态/备注维护，`source` 只做来源复核入口，`raw` / `成员底稿` 才展示成员判断、证据快照、当前事实和完整宽表。复核来源只能打开已有来源 detail，不能自动恢复缺失来源、创建组合或执行业务写入。成员动作里的 `cancel` / `open_cancellation_workbench` 只能渲染到 `/vps/{id}?workbench=cancellation` 的跳转入口。
- 记录详情必须展示成员级跟进状态、备注与最后更新时间；单个成员保存跟进时只 PATCH 该成员 `vps_id`、`followup_status`、`followup_note`，成功后刷新当前记录详情与已保存记录列表的跟进计数。成员跟进状态只表达“组合判断后的执行记忆”，不能隐式修改记录级状态，也不能触发 VPS、Subscription、MonitoringInstance 或 Target 写操作。
- 已保存记录列表可以低权重展示 `execution_plan` 摘要、lane 计数、actionable / blocked 计数；点击仍只打开记录详情，不直接跳业务页。
- 记录详情的执行编排只能出现在 `execution` 面板，按 `cancel_retire / migration / keep_observe / evidence / review` lane 分组展示少量预览成员、lane summary、readback badge、issue chips、下一步 CTA 和快速跟进按钮。当前事实块和完整成员底稿只能在 `raw` / `成员底稿` 查看；board 不取代自动组主 surface，也不批量执行。
- 执行编排 CTA 的 URL 映射只能在前端本地完成：`open_cancellation_workbench -> /vps/{id}?workbench=cancellation`，`open_subscription_context -> /subscriptions?vps_id={id}`，`open_vps_detail -> /vps/{id}`，`review_record` 留在当前记录详情复核或提供普通 VPS 详情入口。
- 快速跟进按钮只能调用 `PATCH /api/asset-decisions/records/{record_id}` 更新成员 followup；不得自动 PATCH record status，也不得调用 VPS、Subscription、MonitoringInstance、Target 写接口。`completed` 记录若当前 facts drift，仍必须展示 drift/readback/plan，不能因为人工状态完成而隐藏问题。
- 单台决策编辑必须在 group detail drawer 或底部单台辅助队列中完成，仍使用 `AssetDecisionWorkPanel` 与 `PATCH /api/vps/{id}`。保存成功 notice 应留在页面可见 surface 内。取消类续费决策保存后，若 API 返回 `renewal_subscription_linkage`，页面必须展示联动结果；`no_active_subscription` 提供创建/跳转订阅入口，`multiple_active_subscriptions` 提供到订阅页筛选当前 VPS 的处理入口，不静默吞掉。
- 取消 / 退役不是 Phase 1 的组合页写动作；任何取消 / 退役执行入口必须跳到 `/vps/{id}?workbench=cancellation`，由 VPS 详情生命周期工作台加载 preview 并提交用户确认步骤。
- Asset Decisions、Dashboard 和 VPS 列表只能把 Subscription / MonitoringInstance / Target / Service / Domain 作为 VPS 的证据和缺口展示；主处理入口必须回到 `/vps/{id}`、VPS 筛选视图或组合决策组。不要把订阅或监控实例作为与 VPS 同级的“待处理主体”。
- 已取消 / 已归档 VPS 是归档资产：普通 VPS、订阅、Dashboard、Monitoring/Target 资产上下文和 Asset Decisions 都不应默认展示或统计这些资产；VPS 页和侧边栏可以提供 `/archive` 入口。归档页是 read-only readback，不复用 VPS 详情里的写操作菜单或 lifecycle workbench。
- Dashboard 资产 lane 应深链到 `/asset-decisions?view=...` 承接组合判断。VPS 页可以提供 `进入组合决策` 入口但不改变库存主路径，并应携带当前 `provider_id` 或证据缺口 scenario；VPS 详情入口应携带 `vps_id`；订阅页只展示 `需要资产判断` 链接并携带行内 `vps_id`，不在订阅页修改 VPS 决策；服务商页入口指向 `view=provider&provider_id=<id>`；Target 资产上下文入口应带 `vps_id` 和适当 scenario。Monitoring 列表不提供资产上下文入口，监控详情只保留所属 VPS 返回路径。
- 当订阅为 `expired` / `cancelled` / `paused` 而 VPS、MonitoringInstance 或 Target 仍表现为 active/running，页面必须把它归入 `cancellation_attention` 或等价的联动处理入口；入口应打开 `/vps/{id}?workbench=cancellation`，由统一工作台提交用户确认的步骤。
- 统一取消 / 退役工作台必须展示 preview 返回的 subscription、VPS、MonitoringInstance、Target 影响范围；MonitoringInstance/Target 默认只展示为待确认项，只有用户在工作台勾选并提交的 `monitoring_instance_actions` / `target_actions` 才能修改运行状态。
- Target 列表 / 详情必须消费批量 asset-context API 显示关联 VPS 的取消 / 过期 / 状态割裂上下文；Monitoring 列表不消费批量 asset-context API，Monitoring 详情通过 `/api/monitoring-instances/{id}/vps` 展示所属 VPS 并提供回到 VPS 详情 / 取消退役工作台的路径。
- 资料质量提示只能来自已有字段：缺订阅、`active_monitoring_instance_link_count <= 0`、缺 provider、缺 location、缺 SSH/IP access。不要从 provider 名称、region 文案或标签推断风险。
- `/subscriptions?vps_id=<id>&create=1` 只作为次级账单事实入口保留；普通补录从 VPS 详情页的 `createVPSSubscription(vpsId, input)` 发起，且不要求用户选择订阅状态。
- 订阅表单和 API contract 以 `billing_period_unit` + `billing_period_length` + `renewal_mode` 为用户可见主字段；`billing_cycle`、`billing_months`、`auto_renew`、`auto_renew_cancelled` 仅作为兼容旧数据和下游月化成本计算的辅助字段。币种和支付方式继续保存字符串，但 UI 必须通过共享常用选项 + 自定义入口标准化。`renewal_mode=gift` 与 `lottery` 都应让 legacy auto-renew flags 为 `false,false`；前端保存时不得把“赠送”误写成 `lottery` 或自动续费。
- `scripts/visual_evidence.py` 的 asset workflow mock 是 browser sanity 的状态展示夹具，必须至少覆盖 `renewal_mode=lottery` 和 `renewal_mode=gift` 的可见订阅行，确保 `/subscriptions` 与资产决策相关页面能实际展示“抽奖”和“赠送”标签；不要只在纯函数测试里覆盖这些标签。
- VPS 有效期延长必须走 `extendVPSValidity(vpsId, input)`，由后端更新当前 active subscription 的 `renew_at` 并写生命周期/价格历史；前端成功后刷新 detail、timeline、subscriptions 并关闭弹层。不要在浏览器里只改本地订阅日期，也不要在无 active subscription 时伪造延长成功。
- 从 VPS 补齐监控接入时，主路径是 VPS 详情内的“创建并接入监控实例”：表单按 VPS 资料预填并允许微调，成功后导航到 `/monitoring/{id}?onboarding=1&return_vps={vps_id}`。不要再增加“继承字段确认”前置弹窗；Monitoring detail 消费 onboarding 参数后必须清理 URL，生成命令后自动复制，复制失败时保留手动复制。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Asset Decisions evidence-boundary explanation grows long | 保持优先级队列为主 surface，证据边界用 `<details>` / 低权重说明承载，不抢占主视觉 |
| `/asset-decisions` default route renders | 首屏显示 portfolio command summary、次级入口和 `决策组扫描`；不渲染旧常驻 `决策路径`、`下一步导览`、记录、场景、续费 evidence 或单台队列 section |
| URL has `record_id` / `manual_group_id` / `template_id` / `view=renewal` / legacy `view=single_queue` | 自动展开对应次级工作区，并保留筛选上下文；`group_id` 只打开自动组详情 |
| `/api/asset-decisions/overview` 或 groups list failed | 组合工作台显示局部错误；底部单台队列和 renewal evidence 可按各自 API 独立加载 |
| group detail missing / 404 | Drawer 显示决策组不存在或已变化，允许返回列表；不得制造空 group |
| create manual group from auto group failed | 错误留在自动组详情内；不关闭当前组、不伪造手工组合 |
| manual group list/detail failed | 自定义组合 surface 或 modal 显示局部错误；自动组、记录、单台队列继续可用 |
| manual member selector candidate list failed | 成员新增表单显示局部错误并禁用 selector；不得让用户手输内部 ID 作为普通路径 |
| manual group member patch/delete failed | 错误留在自定义组合详情内；不修改本地成员意图，不触发业务对象写接口 |
| scenario template list/detail failed | 场景模板 surface/modal 显示局部错误；其他资产决策 surfaces 继续可用 |
| create manual group from scenario template failed | 错误留在模板详情；不关闭模板 modal，不伪造自定义组合 |
| empty builtin template create returns `members:[]` and `evidence_chips:null` | mutation 仍按成功处理并打开新组合详情；显示空成员任务面，不触发 route error，也不诱导用户重试并创建重复组合 |
| patch builtin scenario template | 前端不展示编辑按钮；后端返回错误时显示模板错误，不影响其他 surface |
| URL has `template_id` | 只读取并打开模板详情，不自动创建组合或记录 |
| save record from manual group | POST records 带 `source_type=manual_group`；成功后关闭自定义组合详情并打开记录详情 |
| create decision record 404 | 显示自动组已变化 / 不存在的保存错误，要求用户刷新组列表，不在前端补造记录 |
| saved decision records failed | 已保存组合决策 surface 显示局部错误；自动组、续费 evidence 和单台队列继续独立可用 |
| records / groups / manual groups / templates 部分来源失败 | portfolio command summary 显示 `证据待确认` 或等价未知态，不生成事实漂移 / 阻塞 / 缺证据工作项，也不显示 `闭环稳定` |
| record detail missing / 404 | 记录详情 modal 显示记录不存在，不制造空记录 |
| patch record status failed | 错误留在记录详情 modal 内，不改变本地状态，也不触发 VPS/订阅状态修改 |
| patch record member follow-up failed | 错误留在记录详情 modal 内，不改变成员本地状态，不触发 VPS/订阅/监控/Target 写操作 |
| record member follow-up note cleared | 前端发送空字符串，后端保存为空备注；不得因 falsy 值省略该字段 |
| subscriptions list failed in Asset Decisions renewal window | 续费候选 evidence 显示错误，单台辅助队列仍可显示已加载 VPS |
| all subscriptions failed while building single queue | 单台辅助队列显示加载错误，避免把全量缺订阅误报为真实数据质量 |
| backend group source availability says subscriptions unavailable | 组列表 / 组详情显示证据不可用，不渲染真实 `缺订阅` chip，也不把空 evidence chips 文案写成 `证据稳定` |
| decision record snapshot lacks `evidence_assessment` | 记录详情显示“未记录”或“无证据评估”，成员表继续展示其他 snapshot 字段 |
| decision record snapshot lacks `comparison_insight` | 记录详情 `SAVED EVIDENCE` 显示“保存时未记录对比洞察”降级；不得阻断 evidence assessment、execution readback、execution plan 或成员跟进 |
| decision record API returns `execution_readback` | 已保存记录列表显示 readback status badge、summary 和 drift / blocked / needs_evidence / open 计数；记录详情 summary 增加执行回读指标，成员表显示当前事实摘要与 issue chips |
| decision record API returns `execution_plan` | 已保存记录列表显示 plan summary / lane counts / actionable count；记录详情显示 execution board 和成员下一步列；CTA 按 step kind 本地映射 URL |
| readback status is `drift` | 只显示事实漂移证据和跳转入口，不自动 PATCH record status，也不调用 VPS / Subscription / MonitoringInstance / Target 写接口 |
| readback current facts missing for member | 成员表显示“当前事实缺失”与 issue chip，不把该成员渲染成已对齐 |
| user removes a chip or clears all filters | URL-state 与 visible rows 同步更新，不重新请求 `/api/vps?...` derived query |
| open/close `group_id` / `manual_group_id` / `record_id`，业务 filter 值不变 | overview / groups / manual groups / records 四个 GET 重发；settled 列表不切 loading，Modal 关闭后焦点回对应入口 |
| `view` / window / context filter key 改变 | 新语义查询进入 loading；旧响应不得覆盖新 filter 的 state |
| renewal decision 保存成功 | 六个 revision 各加一；默认 11 GET 与可选当前 detail GET 各重发一次，不扩大或缩小 inventory |
| subscription form/display sees `renewal_mode=lottery` | 显示“抽奖”，legacy auto-renew flags 为 false/false |
| subscription form/display sees `renewal_mode=gift` | 显示“赠送”，legacy auto-renew flags 为 false/false |
| UI copy contains `抽奖/赠送` | 错误；必须拆成 lottery=抽奖、gift=赠送 |
| `/subscriptions?vps_id=<id>&create=1` | 显示当前 VPS context panel，创建表单打开并预填该 VPS；关闭创建表单时移除 `create=1` 但保留 `vps_id` |

#### 5. Good/Base/Bad Cases

- Good: `/asset-decisions?view=provider&renew_within_days=60` 首屏请求 provider 组合组，列表展示同服务商 VPS 成本、服务/域名、监控和建议动作。
- Good: `/asset-decisions?view=provider&provider_id=pv_001&template_id=adt_builtin_provider_review` 首屏展示 provider 上下文 chip，并自动打开服务商评估模板；关闭模板后仍保留 provider 筛选。
- Good: 聚焦某行“查看”后打开 group/manual/record Modal；open key 让四个 filtered GET revalidate，但列表节点保持连接，Escape 关闭后焦点回同一行入口且 body 解锁。
- Good: `/asset-decisions` 默认首屏只展示当前组合判断、四个次级入口和自动组扫描；记录、场景、续费事实和单台辅助队列只有用户点击或深链时才展开。
- Good: `/asset-decisions?view=single_queue&renew_within_days=30` 承接旧链接并打开单台辅助队列，但 portfolio command summary 与决策组扫描仍是页面框架。
- Good: 决策记录接口失败时，页面保留已加载自动组，不生成 readback 工作项，portfolio 风险显示 `证据待确认`，不显示 `闭环稳定`。
- Good: 打开决策组后，同组 VPS 可以比较主订阅、服务 / 域名 / Target / 监控数量、建议角色和 evidence chips；点击单台 `处理` 仍提交原有 VPS renewal decision PATCH。
- Good: 打开自动组后创建自定义组合，页面刷新自定义组合列表并打开新组合详情；用户能继续编辑组合目标、场景和成员意图。
- Good: 自定义组合详情通过 VPS selector 新增成员，保存成员 intended action/reason 只调用 `/api/asset-decisions/manual-groups/{id}/members`，不写 VPS / Subscription / MonitoringInstance / Target。
- Good: 从内置模板创建自定义组合，成功后 URL 切换到 `manual_group_id=<id>` 并打开组合详情；从自定义组合另存模板，成功后刷新模板列表并打开 `template_id=<id>`。
- Base: “资料补齐”等内置模板可以没有蓝图成员；创建空组合后仍能打开详情，再由用户添加 VPS 或直接另存为自定义模板。
- Good: 从自定义组合保存记录时，payload 带 `source_type=manual_group` 和成员决定值，记录详情继续使用 execution readback。
- Good: 打开决策组后保存为组合决策记录，记录详情能回看成员决定角色/动作/理由和保存时证据快照，并能把状态从 `draft` 推进到 `in_progress`。
- Good: 打开已保存记录后，把单台 VPS 成员跟进从 `todo` 改为 `blocked` 并保存备注；记录详情显示更新时间，记录列表的阻塞 / 未关闭计数同步更新，业务动作入口仍只是跳到 VPS 详情或取消工作台。
- Good: 已保存记录列表低权重展示执行回读；`drift` / `blocked` / `needs_evidence` 只作为复核证据，不抢走组合工作台主 surface。
- Good: 记录详情成员表展示“当前回读”，包括 lifecycle / usage / renewal decision、active subscription、服务 / 域名 / Target / 监控计数和 issue chips；成员跟进 PATCH 成功后刷新记录详情和记录列表，readback 随 API 响应更新。
- Good: 记录详情 execution board 将取消退役成员导向 `/vps/{id}?workbench=cancellation`，将缺订阅证据导向 `/subscriptions?vps_id={id}`，同时快速跟进只 PATCH 当前 record member。
- Good: 组列表和记录详情展示 `evidence_assessment` 的 tier、bias、可信 / 压力 / 准备刻度；旧记录没有该字段时不崩溃。
- Good: 资产决策保存 `migrate` 后，VPS 从 `待评估` tab 消失并出现在 `迁移` tab，notice 留在队列 surface。
- Good: 资产决策保存 `cancel` 后，notice 继续展示 `VPS -> 取消`，并追加 API 返回的订阅联动消息 / 订阅页 action。
- Base: 订阅为空、Provider 为空时，页面仍能展示 VPS identity、状态、缺订阅、未关联/缺字段提示。
- Bad: 订阅 evidence 请求失败后，前端把所有 VPS 标成 `缺订阅`，导致用户做出错误取消判断。
- Bad: 默认 `/asset-decisions` 同时铺开记录表、场景模板、自定义组合、续费 evidence 和单台队列，让次级任务与自动组扫描同权。
- Bad: 用 `settled.filter === filter` 判断列表是否 current；只增加一个 `group_id` 就把已有 rows 替换成 loading，卸载 Modal restore target 并让焦点落到 `body`。
- Bad: 任一证据来源失败后，页面用空数组推断 `闭环稳定`、`证据稳定`、无风险或真实资料缺口。
- Bad: 在前端只保存自动组 ID 当作长期决策状态，或从记录详情批量取消 VPS / 直接修改 Subscription / MonitoringInstance / Target。
- Bad: 自定义组合新增成员让用户复制 `vps_...` 内部 ID，或者保存成员意图时顺手 PATCH `/api/vps/{id}`。
- Bad: 模板详情点击打开后自动创建手工组合，或从模板直接保存为决策记录。
- Bad: 用参数默认值 `chips = []` 代替运行时收口；JSON `null` 不会触发 JavaScript 默认参数，随后调用 `chips.filter(...)` 会在 mutation 已落库后让整条路由崩溃。
- Bad: 前端用 IP/路由/性能趋势填充 recommendation 文案；这些语义在 agent 未成熟前不属于资产决策推荐。
- Bad: 前端看到 readback `drift` 后自动把记录状态改成 `in_progress` / `completed`，或自动调用 `/api/vps/*`、`/api/subscriptions/*`、`/api/monitoring-instances/*`、`/api/targets/*` 写接口。
- Bad: 前端把 `execution_plan` 当作真实执行系统，点击 CTA 后直接 PATCH VPS / Subscription / MonitoringInstance / Target，或根据 `actionable_count=0` 自动把 record status 改成 completed。
- Bad: Page 直接 `fetch('/api/vps')` 或在组件层调 API；业务请求必须走 `lib/api.ts`。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: `getAssetDecisionOverview`、`listAssetDecisionGroups`、`getAssetDecisionGroup` 路径和 query string；manual group helper list/create/get/patch/member add/patch/delete；scenario template helpers list/create/get/patch/create-manual-group；记录 fixture 覆盖 `execution_readback` 和 `execution_plan`。
- `AssetDecisionsPage.test.tsx` 与 route/domain workflow tests: `资产组合决策` 主 surface、默认不渲染旧常驻 `决策路径` / `下一步导览` / 记录 / 场景 / 续费 / 单台队列 section、次级入口点击展开单一工作区、legacy `view=single_queue` 承接、tabs query、上下文筛选 chips、深链打开 group/manual group/record/template、同值 filter revalidation 仍发四个 GET 且 group/manual/record 关闭恢复同实体入口焦点、场景模板列表/详情/创建组合、自定义组合另存模板、组详情 drawer、创建自定义组合、自定义组合详情/成员表单/保存 manual record、组/成员/记录 `evidence_assessment` 与 `decision_recommendation` 展示、组卡 comparison summary、默认层/目录层/普通二级面板密度预算、自动组/自定义组/保存记录普通面板不出现 `GROUP TO SCENARIO` / `EVIDENCE MATRIX` / 宽表底稿 / provider-product-cost-facts 串、记录详情 saved evidence snapshot 只在 raw/底稿中可回看、旧 snapshot 缺 `comparison_insight` 降级、保存记录、记录详情状态推进、execution plan 列表片段 / lane board / CTA URL 映射 / 快速跟进、成员跟进 PATCH payload 与计数刷新、成员跟进面板预览限量且不展示完整当前事实、partial source failure 不显示 `闭环稳定` 或虚构 readback work、readback drift 不触发业务对象写请求、plan CTA 不触发业务对象写请求、模板打开不触发业务对象写请求、单台 `AssetDecisionWorkPanel` PATCH payload、renewal evidence 失败不误报缺订阅或 `证据稳定`、错误/空态。
- `templateWorkflows.test.tsx`: 必须用未经类型工厂收窄的真实 JSON fixture 覆盖空内置模板创建响应（至少 `members:[]`、`evidence_chips:null`），断言成功 notice、新组合 modal 和空成员面同时存在，且没有未捕获 render error。
- `assetDecisionArchitectureContract.test.ts`: TypeScript AST synthetic + repository fixtures 覆盖七个 controller entry、API owner 白名单、唯一 router owner、禁止依赖边、无 `*PageContent` 替身、page/controller/global/effect 预算；错误必须报告路径、行与 forbidden symbol/edge/budget。
- `web/e2e/accessibility.spec.ts` / `visual-contracts.spec.ts`：真实 Chromium 覆盖 Asset nested confirmation 逐层 Escape/inert/body lock/focus restore，以及 390px“场景与组合”命令完整可达；staging real lane 对已有自定义模板只打开确认并取消，禁止发送 template mutation。
- `SubscriptionsPage.test.tsx`: `vps_id` URL context、`create=1` 自动打开/预填、关闭创建表单保留 `vps_id` 并移除 `create=1`。

#### 7. Wrong vs Correct

```tsx
// 错误：默认参数只接住 undefined，接不住 Go nil slice 编码出的 JSON null。
function renderRiskChips(chips: EvidenceChip[] = []) {
  return chips.filter(isRisk)
}

// 正确：在成功响应的字段消费边界显式收口 nullable collection。
function renderRiskChips(chips: EvidenceChip[] | null | undefined) {
  return chips ? chips.filter(isRisk) : []
}
```

```tsx
// 错误：常规关联让用户复制内部 ID，且加载失败时只能猜。
<Input label="MonitoringInstance ID" value={draft.monitoringInstanceId} onChange={...} placeholder="mi_..." />
```

```tsx
// 正确：页面加载候选，选择器展示可辨识信息；无候选时给出落地入口。
<select aria-label="选择监控实例" value={draft.monitoringInstanceId} onChange={...}>
  <option value="">选择现有监控实例</option>
  {monitoringInstances.map((monitoringInstance) => (
    <option value={monitoringInstance.monitoring_instance_id}>
      {monitoringInstance.display_name} · {monitoringInstance.monitoring_instance_id}
    </option>
  ))}
</select>
<Link to="/monitoring">监控实例列表</Link>
```

```tsx
// 错误：接到 create=1 后用 effect 同步 setState 打开表单，触发 react-hooks/set-state-in-effect。
useEffect(() => {
  if (searchParams.get('create') === '1') setCreateOpen(true)
}, [searchParams])
```

```tsx
// 正确：把 URL 作为可见状态来源，必要的本地开关只处理用户交互。
const createRequested = searchParams.get('create') === '1'
const createPanelOpen = createOpen || createRequested
```

```tsx
// 错误：请求 identity 一变就把仍属同一业务 filter 的 settled UI 标成 loading。
const isCurrent = settled?.filter === filter

// 正确：effect 仍依赖 filter identity 发起 revalidation；UI currency 使用语义 key。
const currentFilterKey = assetDecisionFilterKey(filter)
const isCurrent = settled != null
  && assetDecisionFilterKey(settled.filter) === currentFilterKey
  && settled.revision === revision
  && settled.retryRevision === retryRevision
```


## Decision and workbench page IA

Decision-heavy pages (asset decisions, detail pages with decision boards) follow a three-tier information architecture instead of stacking every API field on one screen:

1. **Primary tier — one question**: "What should I act on now?" A single judgement plus a single primary action. When there is nothing to decide, show a quiet stable hint — no CTAs, warning colors, or stat cards.
2. **Scan tier — list**: one row per item: identity + status + single entry point, no explanatory sentences. Auxiliary entries (history, templates, renewal facts, single-VPS queue) collapse into a toolbar by default (one row on desktop, 2×2 on mobile) and expand a single panel on click.
3. **Bounded edit tier — modal**: at most 3 tabs, each tab one task. Default tab shows object name + one-line judgement + primary action. Long API text is trimmed to a short judgement, never shown verbatim. Raw data (full member lists, wide tables, execution details) lives behind an explicit "view all" entry; write payloads still use full data. This does not replace the independent VPS full-context workspace described above.
4. **Copy**: no explanatory paragraphs; eyebrows are Chinese or removed; field meaning is conveyed by labels and placeholders.

具体回归以完整用户任务与可达性为主，旧 marker 缺失不能单独证明任务完成；以下安全和完整集合规则继续适用。

- **决策类页面信息层级契约**（适用 `/asset-decisions` 及同类决策/工作台页面）：
  1. **默认层只回答一个问题**：“现在最该处理什么？”——一个主判断 + 一个主动作。无待办时显示一行稳定提示，不渲染 CTA、警示色、统计卡。
  2. **次级层是扫描列表**：每项一行，身份 + 状态 + 单一入口，无解释句。辅助入口（历史/模板/续费事实/单台队列）默认收起为工具条，桌面一行、移动端 2×2，点击展开对应单一面板。
  3. **详情层是弹窗**：弹窗内 ≤3 个 Tab，每个 Tab 单一任务。默认 Tab 只含对象名 + 一句判断 + 主动作。API 长文案（`comparison_insight.summary`、`execution_readback.summary` 等）裁成短判断，不原样展示。
  4. **底稿层是原始数据**：成员全量、宽表、执行细节默认折叠，显式进入。成员预览使用明确的“查看全部”入口，完整成员和写入 payload 不得丢失。
  5. **文案零解释**：无说明性段落；eyebrow 全中文或去除（不渲染 `PORTFOLIO`/`RENEWAL`/`WORKBENCH` 等英文噪声）；字段含义靠标签和占位符自解释。内部 ID（`adg_`/`admg_`/`adr_`/`adt_`）、后端 group type 机器值不进入用户可见层。
  6. **弹窗内容面不透明**：`var(--surface-elevated)`，overlay 半透明，底层页面文字不得透进弹窗。
  7. **测试以用户任务为正向断言**（能在 ≤3 步内完成 X），辅以行数守护；不以“旧 marker 不出现”为唯一标准。
  8. **可维护集合不得静默截断**：场景模板、自定义组合、决策记录等带详情/维护命令的集合，只有在同一 surface 提供可访问的分页、筛选或“查看全部”入口时才能限制首屏数量。不得对 controller 返回值直接固定 `slice(0, N)` 后丢弃其余项；场景模板 API 固定先返回 7 个内置模板再返回 custom templates，固定截断会让 custom 状态维护入口永久不可达。模板 workflow 回归必须覆盖“7 个内置 + 至少 1 个 custom”全部可见且 custom 可打开。


库存、归档和关联输入要求见 [资产 Web 合同](assets-web.md)。
