# 订阅成本中枢二次 UX 修订 Design

## Architecture

本任务同时触及 DB、store、service、HTTP handler、前端 API/types、设置页和订阅页，按现有 subscription cost center 边界实现，不新增图表库或全局状态库。

数据主线：

```
subscription_monthly_budgets
  -> store.ListBudgetMonthBuckets()
  -> service.GetStatistics(window=year)
  -> GET /api/subscriptions/statistics
  -> SubscriptionInsights annual chart
```

成本构成主线：

```
subscriptions + vps_assets + providers + exchange rates
  -> CostRow(country, region, payment_method, monthly_price_base)
  -> service breakdown(...)
  -> statistics/overview breakdown fields
  -> frontend tabs/ranking bars
```

订阅编辑主线：

```
SubscriptionsPage row name click
  -> existing edit modal
  -> updateSubscription()
  -> reload list/overview/statistics
```

## Budget Contract

预算语义改为全局月预算，不再把预算作为 VPS/订阅行状态表达。

新增表建议：

```sql
subscription_monthly_budgets (
  budget_month date primary key,
  base_currency text not null,
  monthly_limit numeric(12,2) not null,
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
)
```

约束：

- `budget_month` 必须是月份第一天。
- `base_currency` 为 3 位大写货币代码。
- `monthly_limit >= 0`。
- 同一个月份最多一条预算。

沿用规则：

- 统计最近 N 个月时，对每个月取 `budget_month <= bucket_start` 的最近一条预算。
- 如果没有任何历史预算，该月预算为 `null`。
- 当前月预算风险只基于当前月有效预算和当前月成本，不落到 VPS 行。

保留兼容：

- 旧 `subscription_budgets` 表和 API 不在本次删除，避免破坏已存在数据和外部调用。
- 设置页订阅 tab 不再暴露旧预算 scope 管理入口。
- 订阅列表和新图表不再消费 row-level `budget_status`。

## Backend Changes

- `SeriesPoint` 扩展 `BudgetLimit *float64 json:"budget_limit,omitempty"`。
- `Statistics` 增加 `PaymentMethodBreakdown`、`RegionBreakdown`。
- `CostRow` 增加 `Country`、`Region`，已有 `PaymentMethod` 可用于聚合。
- Repository 增加：
  - `ListBudgetMonthBuckets(ctx, settings, months, now) ([]SeriesPoint, error)` 或独立 `BudgetMonthBucket`。
  - `ListMonthlyBudgets(ctx)`、`UpsertMonthlyBudget(ctx, input)`、可选 `DeleteMonthlyBudget(ctx, month)`。
- Service：
  - `GetStatistics` 同时返回成本 bucket 和预算 bucket，或把预算值填入 `CostMonthBuckets`。
  - 新增月预算 CRUD 方法给设置页使用。
  - `BudgetRiskCount` 从全局当前月有效预算计算；没有预算时风险为 0/unknown，不归咎任何 VPS。
  - 保留旧 budget API 但不再作为订阅页核心 UI 依据。
- Handler/API：
  - `GET /api/subscription-monthly-budgets`
  - `PUT /api/subscription-monthly-budgets/{YYYY-MM}`
  - 可选 `DELETE /api/subscription-monthly-budgets/{YYYY-MM}`

## Frontend Changes

### Subscriptions Page

- 顶部四卡使用专用 `.subscription-metric-grid`，桌面 `repeat(4,minmax(0,1fr))`，避免复用会被旧媒体查询影响的资产决策样式。
- 中部改为 `.subscription-workbench-grid` 四栏：
  1. 月成本：图表/排行切换。
  2. 成本构成：服务商、分类、币种、支付方式、国家/地区。
  3. 年度趋势与风险：双线 + 差值填充。
  4. 续费队列：紧凑列表，栏内滚动。
- `SubscriptionInsights` 拆成小的本文件 helper 或同目录组件，避免单组件继续膨胀；样式仍写入 `web/src/index.css`。
- 表格列调整：
  - 订阅/VPS（点击编辑弹窗）
  - 分类
  - 标签
  - 周期
  - 原币种/原价格
  - 基准货币成本
  - 续费
  - 续费方式
- 编辑 modal header/body 增加 VPS 详情链接，不在列表行放跳转按钮。

### Settings Subscription Tab

- 成本基准与汇率、提醒设置保留。
- 预算规则替换为“月预算时间线”：
  - 展示最近 12 个月和未来可编辑月份。
  - 支持新增/编辑某月预算、备注。
  - 未配置月份显示“沿用 YYYY-MM 预算”或“暂无预算”。
  - 不展示 scope_type/provider/category/vps 管理。

### Chart Design

- 月成本饼图使用 SVG，不引入第三方库。
- Tooltip 使用 HTML overlay 或 SVG title + 可见 focus panel；必须支持键盘 focus。
- 年度双线图可新增专用 `BudgetCostTrendChart`，因为现有 `MetricChart` 只有单序列 polyline，强行塞双线/区域会污染原子组件。
- 差值填充按相邻月份生成 polygon/area，成本高区域使用 `--color-state-alert/critical` 派生透明色，预算高区域使用 `--color-state-normal` 派生透明色。

## Compatibility

- 保留旧 budget API，不删除旧 `subscription_budgets` 表。
- 前端类型可保留 `SubscriptionBudgetRecord` 以免 Dashboard/历史测试断裂，但新订阅页不再显示 row-level budget status。
- 如果 statistics 的预算 bucket 失败，图表 panel 独立报错，列表仍可用。
- 月预算以基准货币保存；当用户切换基准货币后，后续新预算使用新基准货币，旧预算以自身 base_currency 展示。年度趋势中若预算币种与当前基准货币不一致，先标记数据不足而不是错误换算。

## Risks

- 年度双线图需要真实预算历史；必须补后端测试，否则容易画出误导曲线。
- 四栏布局在 1080px 可能拥挤，需要 CSS grid auto-fit 断点和浏览器截图验收。
- 旧 `subscription_budgets` 与新月预算并存，命名必须清楚，避免设置页出现两套预算概念。
