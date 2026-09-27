# Dashboard 合同

## Asset Ledger Dashboard summary

`internal/center/store/dashboard.go` 可以读取资产层表来生成 `/api/dashboard` 的少量决策摘要，但它仍是 Dashboard read model，不是资产 CRUD 仓库。

- `incidents.DashboardOverview.AssetSummary` 的 JSON contract 是 `asset_summary`，只允许返回聚合计数和按币种成本分组，不返回 VPS、subscription、MonitoringInstance 或 provider 明细数组。
- `asset_summary` 的 30 天续费口径：`subscriptions.status = 'active'`，`renew_at >= current_date` 且 `renew_at <= current_date + 30`，并只统计管理中的 VPS。
- 管理中 VPS 口径：`vps_assets.lifecycle_status = 'active'`；其余唯一生命周期是 `archived`。
- 当前监控以 `monitoring_instances.vps_id` 的永久所有权和 `lifecycle_status <> '已退役'` 派生；不再允许孤立或共享实例。
- 异常与严重运行统计只计管理中 VPS 的已接入、启用实例；维护、暂停、待接入、退役不作为运行异常。
- 运行关注队列、全局/分组计数及异常关联 VPS 计数先按绑定和可信在线证据投影健康：绑定非「已绑定」为「绑定待确认」，缺少可信在线时间或健康证据为「数据不可用」。即使 incident 存储摘要仍为「正常」，也必须进入关注集合；原始或回填心跳不能替代可信在线证据。
- Target 当前可见性要求其生命周期为 `active`，通过未结束的服务/域名关联判断 VPS 归属。共享探测只要仍有管理中的承载关联就保留。
- 成本口径：管理中 VPS 的 active subscriptions `monthly_price` 按币种求和；已归档潜在扣费在成本页单列，不进入当前资产预计成本。
- `no_renewal_vps_count` 统计决定不续费的管理中 VPS；`archived_vps_count` 统计归档资产；`auto_renew_check_vps_count` 统计决定不续费但服务商自动续费仍为 unchecked/enabled 的资产；`pending_followup_count` 统计待核对事项。续费意向与生命周期、服务商核对事实彼此独立。
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
- `asset_summary` 只能包含聚合摘要：`renewal_due_30d_subscription_count`、`renewal_due_30d_vps_count`、`unreviewed_vps_count`、`no_renewal_vps_count`、`archived_vps_count`、`auto_renew_check_vps_count`、`pending_followup_count`、`unlinked_vps_count`、`abnormal_linked_vps_count`、`cost_by_currency[]`。`cost_by_currency[]` 只包含 `currency`、`monthly_total`、`yearly_total`。
- 库存完整度计数必须来自后端 contract：待接入监控实例、暂停监控实例、退役监控实例、暂停目标、归档目标。
- 退役监控与归档 Target 是历史库存计数，分别直接按 `monitoring_instances.lifecycle_status='已退役'` 和 `targets.lifecycle_status='retired'` 统计；不从已排除历史对象的当前运行集合计数，也不读取旧 Target `run_status='已归档'`。

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
