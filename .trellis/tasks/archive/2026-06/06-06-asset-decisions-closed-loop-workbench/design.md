# Design

## Evidence-Based Scope

当前代码已经具备资产决策中枢的基础层：

- 自动组 read model：`getAssetDecisionOverview`、`listAssetDecisionGroups`、`getAssetDecisionGroup`。
- 场景模板：`listAssetDecisionScenarioTemplates`、`getAssetDecisionScenarioTemplate`、`createManualGroupFromScenarioTemplate`、`createAssetDecisionScenarioTemplate`、`patchAssetDecisionScenarioTemplate`。
- 自定义组合：`listAssetDecisionManualGroups`、`getAssetDecisionManualGroup`、`createAssetDecisionManualGroup`、成员增删改。
- 决策记录：`listAssetDecisionRecords`、`getAssetDecisionRecord`、`createAssetDecisionRecord`、`patchAssetDecisionRecord`，并包含 `execution_readback`。
- 单台辅助处理：`AssetDecisionWorkPanel` 仍调用 `updateVPSAsset`。

因此本阶段的设计不是新增一套后端聚合模型，而是在前端把现有 surfaces 收敛成闭环导览，并修复实际状态一致性问题。

## Architecture

### Existing Data Sources

`AssetDecisionsPage` 当前已经并行加载：

- `portfolioState.overview` 与 `portfolioState.groups`
- `templatesState.templates`
- `manualGroupsState.groups`
- `recordsState.records`
- `queueState.renewals`
- `queueState.unreviewed/migrate/cancel/subscriptions`
- `vpsCatalogState.rows`

本阶段新增的闭环导览使用这些已经加载的数据派生，不新增 API helper，除非实现中发现无法从现有类型获得必要字段。

### New Frontend-Derived View Model

新增本地派生类型，例如：

```ts
type AssetDecisionNextWorkKind =
  | 'record_drift'
  | 'record_blocked'
  | 'record_needs_evidence'
  | 'auto_group'
  | 'manual_group'
  | 'scenario_template'

type AssetDecisionNextWorkItem = {
  id: string
  kind: AssetDecisionNextWorkKind
  tone: BadgeTone
  title: string
  summary: string
  meta: string
  actionLabel: string
  open: () => void
  priority: number
}
```

排序原则：

1. drift record：最高，因为表示“跟进完成或判断之后当前事实仍不闭环”。
2. blocked record：第二，因为需要人工解除阻塞。
3. needs_evidence record：第三，因为证据不足会阻塞后续决策。
4. 当前上下文下的自动组：继续作为发现入口。
5. active manual group：用户已经创建但尚未形成记录或仍在比较。
6. active scenario template：只有在当前没有足够组/记录时作为启动入口。

该排序只影响 UI 显示，不写入任何后端状态。

### Closed-Loop Metrics

在前端从现有数据派生：

- `autoGroupCount`: `portfolioState.groups.length`。
- `manualActiveCount`: `manualGroupsState.groups.filter(status === 'active').length`。
- `recordOpenCount`: 记录状态非 `completed/abandoned` 的数量。
- `readbackDriftCount`: `recordsState.records` 中 `execution_readback.drift_count` 或 status 为 `drift` 的聚合。
- `readbackBlockedCount`: readback blocked 或 followup blocked 的聚合。
- `needsEvidenceCount`: readback needs evidence + overview evidence groups。

这些数字用于“闭环状态”扫描，不替代后端 `Overview`。

## UI Layout

首屏建议结构：

1. Page header。
2. 当前 focus summary，保留现有四个紧凑指标但调整文案，让它反映闭环状态而不只是组数量。
3. 新增 `CLOSED LOOP` surface：
   - 左侧：下一步工作项列表，最多 5 项。
   - 右侧或下方：闭环状态短摘要，展示发现、场景、记录、回读问题数量。
   - 若存在 context chips，导览文案明确“当前上下文筛选生效”。
4. 决策组列表仍是第一主 surface。
5. 场景模板、自定义组合、已保存记录、renewal evidence、单台队列保持现有顺序，但文案和状态可以微调以减少割裂。

移动端：

- 导览使用单列列表。
- 工作项 action 使用按钮，不要求横向并排。
- 所有长摘要使用 `overflow-wrap` / 已有表格滚动容器，不造成页面级横向溢出。

## URL State

继续使用现有参数：

- Context filters: `provider_id`、`vps_id`、`country`、`region`、`city`、`scenario`。
- Open state: `group_id`、`manual_group_id`、`record_id`、`template_id`。

导览项点击只调用现有 `openGroup/openManualGroup/openRecord/openTemplate`，从而复用当前 open-state URL 逻辑。

必须修复：

- `submitRecordSave` 成功后重复设置 `record_id` 的问题。

应检查但不强行改动：

- 清除 context filter 是否会意外关闭 modal。
- 关闭 modal 是否会意外移除 context filters。
- 深链打开对象失败时是否仍保留上下文。

## Data And Error Boundaries

闭环导览必须尊重各 state 的局部错误：

- `recordsState.error` 存在时，不渲染假 readback 工作项。
- `portfolioState.groupsError` 存在时，不渲染假自动组工作项。
- `manualGroupsState.error` 存在时，不渲染假自定义组合工作项。
- `templatesState.error` 存在时，不渲染假模板工作项。

错误提示可以汇总为“部分 surface 不可用”，但不得把加载失败解释成没有问题。

## Compatibility

- 不改变现有 API contract。
- 不改变已有 URL 参数。
- 不改变 `AssetDecisionWorkPanel` 的单台 PATCH 行为。
- 不改变 records/manual/template 的后端写入语义。
- 新增导览只消费现有数据，旧数据为空时显示空态。

## Non-Goals

- 不新增 `GET /api/asset-decisions/next-work`。
- 不新增 `asset_decision_tasks` 或类似持久化待办表。
- 不使用 HostSample / ProbeObservation / IP / 路由 / 性能数据。
- 不在前端重新实现 evidence scoring。
- 不把 `decision_recommendation` 或 `execution_readback` 当作自动执行承诺。

## Testing Design

主要测试落点是 `AssetDecisionsPage.test.tsx`：

- fixture 中构造 drift / blocked / needs_evidence records。
- 断言闭环导览按优先级展示。
- 点击导览项后请求正确 detail endpoint，并更新 URL open-state。
- 断言模板/记录/组错误不会产生假工作项。
- 断言 readback drift 不触发 `PATCH /api/vps`、`PATCH /api/subscriptions`、`PATCH /api/monitoring-instances`、`PATCH /api/targets`。
- 保留现有保存记录、成员跟进、模板创建组合、单台队列 PATCH payload 测试。

视觉 sanity 继续使用 `scripts/visual_evidence.py browser-sanity --mock-api asset-workflows`，必要时更新 mock fixture 以覆盖导览。
