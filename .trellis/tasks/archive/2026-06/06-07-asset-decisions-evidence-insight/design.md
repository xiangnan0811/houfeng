# 资产组合决策证据洞察与对比增强设计

## Architecture Boundary

本任务保持资产组合决策三层边界不变：

- 自动组：只读派生 read model，回答“系统发现了哪些组合问题”。
- 自定义组合：用户维护的 scenario layer，回答“我正在比较哪组资产以及成员意图是什么”。
- 决策记录：memory/readback layer，回答“某一次保存的判断、证据快照和执行回读是什么”。

新增能力是只读 `comparison_insight`，它属于解释层，不是执行层，不拥有状态迁移，也不写业务对象。

## Backend Contract

在 `internal/center/assetdecisions` 增加纯函数 comparison 派生器，建议类型如下，最终命名以本地代码风格为准：

- `ComparisonInsight`
  - `summary`
  - `primary_axis`: `renewal | cost | service_context | monitoring | evidence | lifecycle | review`
  - `lane_counts[]`: `{lane,count}`
  - `priority_vps_ids[]`
  - `tradeoffs[]`: `{kind,label,tone,details?}`
- `MemberComparisonInsight`
  - `rank`
  - `lane`: `primary | standby | observe | retire | evidence | review`
  - `summary`
  - `strengths[]`
  - `risks[]`
  - `gaps[]`
  - `tradeoffs[]`

字段通过 JSON snake_case 出现在：

- `GroupSummary.comparison_insight`
- `GroupMember.comparison_insight`
- `ManualGroupSummary.comparison_insight`
- `ManualGroupMember.comparison_insight`

`RecordSnapshotFromGroup` 与 `RecordSnapshotFromMember` 保存 `comparison_insight`。旧记录 snapshot 缺失该字段时，前端展示“保存时未记录对比洞察”，继续显示已有 evidence assessment、chips、readback 和 plan。

## Derivation Rules

成员 lane 只消费当前已有字段：

- `primary`：承载服务/域名/Target、active subscription、监控关联、证据质量较强，且 suggested role/action 偏保留。
- `standby`：usage 为 standby 或 observe，承载较弱但资料可用，适合作备用或观察。
- `retire`：cancel/open cancellation、idle paid、取消联动、生命周期/续费决策进入取消相关状态。
- `evidence`：missing subscription、missing monitoring、missing provider/location/access、no service context、source unavailable 或 current fact missing。
- `review`：动作为空、信号冲突、不能安全归类，或仅有普通复核建议。

组级 `primary_axis` 用组类型和最强证据决定：

- `cancellation_attention` 优先 `lifecycle`
- `cost_pressure` 优先 `cost`
- `evidence_gap` 优先 `evidence`
- `region_portfolio` / `provider_portfolio` 根据成员差异优先 `service_context`、`monitoring` 或 `cost`
- `renewal_attention` 优先 `renewal`

排序复用或扩展已有 `memberPriority`，但不能让新逻辑与现有组排序产生不可解释的分叉。若需要扩展，抽出共享 helper，供 recommendation priority 与 comparison insight 一起使用。

## Data Flow

```text
loadFacts()
  -> buildMember()
  -> assessMember() / RecommendMember()
  -> CompareMember()
  -> buildGroup()
  -> assessGroup() / RecommendGroup()
  -> CompareGroup()
  -> handler JSON
  -> web types
  -> group cards / group detail matrix / manual detail matrix / record snapshot view
```

Records snapshot flow:

```text
GroupDetail or ManualGroupDetail
  -> RecordSnapshotFromGroup / RecordSnapshotFromMember
  -> asset_decision_records.evidence_snapshot / asset_decision_record_members.evidence_snapshot
  -> record detail modal snapshot readback
```

## Frontend UX

### Group Cards

组卡保留当前结构，但新增轻量“比较结论”：

- 展示 `comparison_insight.summary`
- 展示 1-3 个 priority VPS / lane count chips
- 保留当前成本、承载、监控、evidence assessment，不把卡片变成宽表

### Auto Group Detail

在 summary 和 `GROUP TO SCENARIO` 后、成员 DataTable 前增加 `EVIDENCE MATRIX / 证据矩阵`：

- 按 lane 或 rank 展示成员卡片/矩阵行
- 每个成员展示 identity、lane、rank、角色/动作、成本、承载、续费、监控/Target、资料缺口、summary
- strengths / risks / gaps 用 chips 展示，限制数量并提供 `+N`
- CTA 仅复用现有“处理”、VPS 详情、取消/退役 workbench、创建自定义组合、保存记录

### Manual Group Detail

复用同一个矩阵组件，但增加：

- intended role/action 与 current comparison lane 的对照
- intent mismatch / evidence gap 的提示
- 成员意图表单和新增成员表单保持现有写接口，不改业务对象

### Record Detail

增加低权重 `SAVED EVIDENCE / 保存时依据`：

- 从 record/group/member `evidence_snapshot.comparison_insight` 解析展示
- 若缺失，展示降级文案
- 不修改 `decided_role` / `decided_action`
- 不影响 `SOURCE CONTINUITY`、`EXECUTION PLAN`、成员跟进表单

## Compatibility

- 新字段为 additive JSON 字段；旧前端忽略，新前端对缺失字段降级。
- 不新增 migration；records snapshot 结构仍为 JSONB map。
- 当前 handler/router endpoint 不变。
- 当前 frontend API helper 名称不变。
- 当前 `single_queue` legacy URL、context chips、object deep link 行为不变。

## Error Handling

- facts 查询失败继续由 repository fail closed，不伪造 comparison。
- source unavailable 只能降低可信度并生成 unavailable/gap 解释，不误报真实缺订阅或健康。
- manual member current fact missing 保留成员，并生成 `current_fact_missing` comparison gap。
- 旧 snapshot 缺 `comparison_insight` 是正常兼容路径，前端不得抛异常。

## Risks And Mitigations

- 风险：新增 comparison insight 变成第二套 scoring engine。
  - 规避：只解释已有 evidence assessment / chips / recommendation / facts，不新增独立业务结论。
- 风险：后端和前端重复归类逻辑。
  - 规避：lane、rank、tradeoff 均由后端派生；前端只展示和做旧 snapshot 解析降级。
- 风险：页面信息密度继续过高。
  - 规避：矩阵是首屏 scan surface，宽表保留为 detail fallback；chips 限量展示。
- 风险：记录历史和当前事实混淆。
  - 规避：record detail 明确区分 saved evidence、source continuity、execution readback、execution plan。
- 风险：误承诺性能/IP/路由能力。
  - 规避：文案只使用成本、用途、服务/域名、Target、监控、订阅、生命周期和资料缺口。

## Spec Updates

实施后需要更新：

- `.trellis/spec/backend/database-guidelines.md`：记录 `comparison_insight` 合同、派生边界、禁止数据源。
- `.trellis/spec/web/state-and-data.md`：记录前端展示层级、旧 snapshot 降级、禁止业务写入。
- `docs/design/v2-houfeng/component-spec.md`：记录 AssetDecisionsPage 的证据矩阵与保存时依据层级。
