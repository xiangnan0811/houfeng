# Dashboard 合同

## Asset Ledger Dashboard summary

`internal/center/store/dashboard.go` 可以读取资产层表来生成 `/api/dashboard` 的少量决策摘要，但它仍是 Dashboard read model，不是资产 CRUD 仓库。

- `incidents.DashboardOverview.AssetSummary` 的 JSON contract 是 `asset_summary`，只允许返回聚合计数和按币种成本分组，不返回 VPS、subscription、MonitoringInstance 或 provider 明细数组。
- `asset_summary` 的 30 天续费口径：`subscriptions.status = 'active'`，`renew_at >= current_date` 且 `renew_at <= current_date + 30`，并只统计未取消/未归档的 VPS。
- active VPS 口径：`vps_assets.lifecycle_status not in ('cancelled', 'archived')`。
- VPS 普通资料 PATCH 只能维护低风险事实 lifecycle：`active`、`idle`、`testing`。`to_cancel`、`cancelled`、`to_migrate`、`archived` 是受控流程/终态，不得通过普通 PATCH 写入；取消/退役走 lifecycle workbench 或专用 endpoint，归档走 archive endpoint，迁移在完整 workbench 存在前只能作为 `renewal_decision=migrate` 的人工意向。
- active link 口径：`vps_monitoring_instance_links.unlinked_at is null`。
- 异常关联 VPS 口径：active link 关联到 `monitoring_instances.current_health_status <> '正常'` 的 MonitoringInstance；只读 MonitoringInstance 派生状态，不改写 MonitoringInstance。
- 成本口径：`sum(active subscriptions monthly_price)` 按 `currency` 分组，`yearly_total = monthly_total * 12`；第一阶段不做汇率换算。
- 取消联动口径：Dashboard 只处理 current VPS，最终 `cancelled` / `archived` 不进入 `asset_summary`；`to_cancel_vps_count` 统计仍需处理的 `lifecycle_status='to_cancel'`，`cancelled_vps_count` 当前为 0 兼容字段；`cancellation_attention_vps_count` 统计 current VPS 中订阅非活跃但 lifecycle 未进入 `to_cancel`、`to_cancel` 但订阅仍 active、`to_cancel` 但 MonitoringInstance/Target 仍运行，或取消类续费决策与 lifecycle 未对齐的 VPS；`running_cancelled_asset_count` 只统计 `to_cancel` VPS 下仍运行的 active MonitoringInstance link 与未归档/未暂停 Target。
- 该查询不得改变 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent、VPS、subscription 或 link 记录。
- `limit` 只限制异常队列和 recent events；不得限制 `asset_summary`。


### Scenario: Dashboard Overview Contract

#### 1. Scope / Trigger

- Trigger: `/api/dashboard` 是 DashboardPage 与 AppShell 共享的全局摘要接口；任何新增字段都会跨越 PostgreSQL read model、Go JSON、`web/src/lib/types.ts` 和页面展示。
- 修改触发：新增/改名/改语义任一 `DashboardOverview` 字段，或让 Dashboard/AppShell 展示新的系统事实。

#### 2. Signatures

- Backend API: `GET /api/dashboard?limit=<positive-int>`。
- Backend method: `PostgresDashboardRepository.GetDashboardOverview(ctx, limit)`。
- Frontend API: `getDashboard(): Promise<DashboardOverview>`。
- Frontend type: `web/src/lib/types.ts` 的 `DashboardOverview`，字段保持 center JSON snake_case。
- Asset summary field: `DashboardOverview.asset_summary`，类型为 `DashboardAssetSummary`。

#### 3. Contracts

- `limit` 只限制 `abnormal_monitoring_instances`、`abnormal_targets` 和 `recent_events`；不得限制全局计数、`group_summaries` 或 `notification_status`。
- `snapshot_generated_at` 是 Center 生成 overview 的时间，只能被展示为 dashboard 生成时间。
- `abnormal_monitoring_instance_count` / `abnormal_target_count` 分别是对应 severe 集合的超集；`severe_*_count` 不能被前端再次加到 abnormal 总数。相同集合关系也适用于 `group_summaries` 中的 abnormal/severe 字段。
- `group_summaries` 必须由后端基于全量 `monitoring_instances` + `targets` 计算，空白 group 归一为 `未分组`，前端不得从异常队列 reduce。
- `notification_status` 只能包含配置布尔摘要，不包含 Telegram token/chat id 或 Feishu webhook URL。
- `asset_summary` 只能包含聚合摘要：`renewal_due_30d_subscription_count`、`renewal_due_30d_vps_count`、`unreviewed_vps_count`、`to_cancel_vps_count`、`to_migrate_vps_count`、`unlinked_vps_count`、`abnormal_linked_vps_count`、`cost_by_currency[]`。`cost_by_currency[]` 只包含 `currency`、`monthly_total`、`yearly_total`。
- 库存完整度计数必须来自后端 contract：待接入监控实例、暂停监控实例、退役监控实例、暂停目标、归档目标。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| `limit` 缺失 | handler 使用默认 limit |
| `limit <= 0` 或非数字 | handler 返回 400 |
| dashboard store 查询失败 | handler 返回 500，store error 用 `%w` 包装上下文 |
| abnormal=2、severe=1 | JSON 原样返回 2/1；前端异常总数保持 2 |
| `center_settings` singleton 缺失 | `notification_status` 全 false，不返回错误 |
| `group_summaries` 为空 | Dashboard 显示空态，不制造 `未分组 0` |
| Asset Ledger 表为空 | `asset_summary` 返回 0 计数与空 `cost_by_currency`，Dashboard 显示低权重空态 |

#### 5. Good/Base/Bad Cases

- Good: group 只存在于 targets 时仍出现在 `group_summaries`，监控实例计数为 0、目标计数为真实值。
- Good: severe monitoring instance 是 abnormal monitoring instances 的一项分层，后端返回 abnormal=2/severe=1，前端不做 2+1。
- Base: 无监控实例无目标时 dashboard 仍返回 200，计数为 0，首次接入工作台显示，Group 区为空态。
- Good: 资产摘要显示为工作台内低权重入口，链接到 VPS / 订阅 / 监控筛选页，不新增资产明细表。
- Bad: 从 `abnormal_monitoring_instances` 推导 `按 Group 分布`；把 `snapshot_generated_at` 写成同步完成；把通知 token 暴露给 Dashboard。
- Bad: 用 `abnormal_*_count + severe_*_count` 生成异常总数，导致严重对象被重复计数。
- Bad: 把 `asset_summary` 扩展成 `vps_assets` / `subscriptions` 明细数组，或在 Dashboard 首屏展示所有资产字段。

#### 6. Tests Required

- Go store test: abnormal=2/severe=1 的集合关系、新计数字段、全量 group SQL、settings 缺失时通知 false、`limit` 不影响 group summary。
- Go handler test: abnormal/severe snake_case 字段保持 2/1、severe 不大于 abnormal，且不泄露敏感通知字段。
- Frontend type/API fixture: `DashboardOverview` fixture 覆盖新增字段；新增 `asset_summary` 时必须同步 AppShell、DashboardPage、api test fixtures。
- DashboardPage test: 生成时间、五 mode 唯一主行动/deep link、abnormal/severe 不重复、VPS false-empty、局部 fallback，以及不展开独立 summary/KPI strip、Group、最近事件、API facts、资产明细 dump。
- AppShell test: 共享 dashboard fixture 与新增 contract 保持兼容。

#### 7. Wrong vs Correct

```tsx
// 错误：severe 已包含在 abnormal 中，却再次相加。
const abnormalTotal = overview.abnormal_monitoring_instance_count
  + overview.severe_monitoring_instance_count

// 正确：异常总数直接消费 abnormal，severe 只展示优先级分层。
const abnormalTotal = overview.abnormal_monitoring_instance_count
const severeTotal = overview.severe_monitoring_instance_count

// 错误：要求 settings secret 出现在 dashboard contract
overview.notification_status.feishu_webhook_url

// 错误：资产摘要变成 Dashboard 数据仓库
overview.asset_summary.subscriptions.map((item) => item.renew_at)

// 正确：只展示配置布尔摘要，并把编辑动作交给 SettingsPage
<Link to="/settings">{notificationSummary(overview)}</Link>

// 正确：资产摘要只做少量决策入口，明细交给资产页面
<Link to="/subscriptions?renew_within_days=30">
  30 天续费 <MonoDigits>{overview.asset_summary.renewal_due_30d_vps_count}</MonoDigits>
</Link>
```
