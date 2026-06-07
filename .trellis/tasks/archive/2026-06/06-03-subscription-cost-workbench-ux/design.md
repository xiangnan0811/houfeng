# Design

## Information Architecture

- `/subscriptions` 是日常工作台：顶部指标、续费待处理摘要、成本洞察、列表筛选和订阅明细。
- `/settings?tab=subscriptions` 是低频配置页：成本基准与汇率、提醒窗口、预算规则。
- 订阅工作台只依赖订阅列表、VPS 列表、overview 和 statistics；预算编辑和完整 settings 不再作为主页面必需数据。

## Backend Contract

- 扩展 `subscriptioncosts.SubscriptionStatistics`：
  - `cost_month_buckets`: 最近窗口内基准货币月成本序列。
  - `renewal_month_buckets`: 仍表示未来续费月份分布。
- `cost_month_buckets` 使用可证明数据重建：
  - 按订阅 `created_at` 纳入月份。
  - 通过 `price_histories` 在 bucket 结束时点选择当时价格、币种、状态。
  - 当前状态仅作为没有价格历史时的状态来源。
  - 非基准货币使用 bucket 当时可用的汇率；缺失汇率时不硬填历史成本。

## Frontend Data Flow

- `/subscriptions` 分层加载：
  - 核心数据：`listSubscriptions`、`listVPSAssets`、`getSubscriptionOverview`。
  - 图表数据：`getSubscriptionStatistics('year')` 独立加载和失败降级。
- `/settings?tab=subscriptions` 使用订阅专用 API：
  - `getSubscriptionCostSettings`
  - `updateSubscriptionCostSettings`
  - `listSubscriptionBudgets`
  - `createSubscriptionBudget`
  - `updateSubscriptionBudget`
  - `refreshSubscriptionExchangeRates`
- 非订阅 settings tab 继续使用 `getSettings`/`updateSettings`。

## UI Components

- 使用现有 `MetricChart`、`StatusGlyph`、`Tabs`、`Input`、`Select`、`Modal`。
- 新增 `SubscriptionInsights` 聚合图表：
  - VPS 成本占用 donut + legend。
  - 年度趋势 `MetricChart`。
  - 预算风险和续费月份摘要。
  - provider/category/currency breakdown 使用一个切换控件和排行条形图。
- 新增 `SubscriptionSettingsSection` 放在 `web/src/pages/settings/`，作为设置页订阅 tab 的独立表单区域。

## Layout Rules

- 顶部指标使用稳定四列 grid，窄屏 media query 才收缩。
- 列表工作区作为一个 `page-panel`，内部顺序为标题/数量、筛选、chips、表格或空态。
- 表格允许在自身 panel 内横向滚动；页面本身不应横向溢出。
- CSS 只追加到 `web/src/index.css`，复用 token、BEM 和状态色。

## Risk Controls

- 前端不根据当前订阅成本生成历史曲线；历史不足时展示空态。
- statistics 失败只影响洞察区域，不遮断订阅明细。
- SettingsPage 改表单边界时保留各系统设置 tab 的保存 payload，不把订阅 settings 混进 `/api/settings`。
- 预算编辑在订阅 tab 内独立提交，失败不污染系统设置草稿。
