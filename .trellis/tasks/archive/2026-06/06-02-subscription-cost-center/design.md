# 订阅模块 VPS 成本中枢设计

## Architecture

- 新增领域包优先保持职责窄：
  - `internal/center/subscriptioncosts`：设置、汇率、成本折算、overview/statistics DTO。
  - `internal/center/subscriptionbudgets`：预算类型、校验、仓库接口。
  - `internal/center/subscriptionreminders`：提醒规则解析、扫描、去重、通知发送。
- `internal/center/subscriptions` 保留账单事实模型；只添加确属订阅事实的轻量字段，派生成本/预算/提醒状态由 read model 返回。
- HTTP handler 放在 `internal/center/http/handlers/subscription_costs.go` 或扩展 `subscriptions.go`，router/bootstrap 显式 wire。
- worker 使用现有 `centerapp.Worker` 形状，在 `cmd/houfeng-center/bootstrap.go` 中加入汇率刷新 worker 和续费提醒 worker。

## Data Model

- 追加迁移从当前最大编号之后开始。
- `center_settings` 扩展 `subscription_cost_settings jsonb`，包含：
  - `base_currency`
  - `exchange_rate_provider`
  - `fixer_api_key`
  - `default_reminder_offsets_days`
  - `max_reminder_lead_days`
  - `exchange_rate_stale_after_hours`
- `subscription_exchange_rates`：
  - provider, base_currency, quote_currency, rate, rate_date, fetched_at, error_summary
  - unique(provider, base_currency, quote_currency, rate_date)
- `subscription_budgets`：
  - budget_id, scope_type, scope_id, name, base_currency, monthly_limit, yearly_limit, warning_pct, enabled, note, created_at, updated_at
  - scope_type: global/provider/label/vps
- `subscription_reminder_deliveries`：
  - delivery_id, subscription_id, vps_id, renew_at, offset_days, reminder_kind, channel, delivery_status, summary, notification_id, sent_at, created_at
  - unique(subscription_id, renew_at, offset_days, reminder_kind, channel)
- `subscriptions` 只保守增加：display_name, cost_category, labels, trial_ends_at, ends_at。现有周期/续费模式字段继续使用。

## API Contracts

- `GET/PUT /api/subscriptions/settings`
  - 返回设置时 `fixer_api_key` 不返回明文，只返回 `fixer_configured` 和可选 masked summary。
  - PUT 不传 key 时保留现有 key；传空字符串表示清除 key。
- `POST /api/subscriptions/exchange-rates/refresh`
  - 拉取当前活跃订阅涉及币种到 base currency 的汇率。
  - 返回刷新结果、失败币种、stale 状态。
- `GET /api/subscriptions/overview`
  - 返回基准货币总月/年成本、预算风险、未来续费、汇率状态、cost_by_currency/provider/vps/category、missing_subscription_vps_count。
- `GET /api/subscriptions/statistics?window=month|quarter|year`
  - 返回趋势/拆分用 DTO，第一版可由当前订阅和快照实时计算。
- `GET/POST/PATCH /api/subscription-budgets`
  - 预算 CRUD，PATCH 使用 Optional 字段，禁用预算不参与风险判断。
- `GET /api/subscriptions`
  - 兼容旧数组响应，但每条记录追加 base cost、exchange、budget、reminder 派生字段。
  - 新过滤：currency, provider_id, budget_status, auto_renew, payment_method, label, renewal_decision。

## Notifications And Workers

- 提醒 worker 每日扫描，也支持测试中直接调用 service sweep。
- reminder summary 包含 VPS 名称、原币种金额、CNY 折算、续费日期、决策状态和跳转上下文。
- 普通续费提醒和决策关注提醒使用不同 `reminder_kind`。
- 复用通知发送客户端能力；若 incident service 内部 wrapper 不适合复用，则新增 settings-aware notifier facade，避免依赖 incident 语义。
- 发送结果写 `notification_records` 和 reminder delivery；无通道/被策略抑制写 `suppressed`。

## Frontend

- `/subscriptions` 使用 workbench 结构：总览、续费队列、明细、预算、汇率与提醒设置。
- 使用现有 atoms、DataTable、Tabs、MetricChart/Sparkline 或轻量 CSS/SVG，不引入 chart library。
- 页面数据通过 `web/src/lib/api.ts` 新增函数获取，类型统一写入 `web/src/lib/types.ts`。
- VPS 详情接入 cost card；Asset Decisions 接入 cost signal；Dashboard 只展示高信号摘要，不扩成订阅明细。

## Compatibility And Risks

- 旧订阅 API 字段保持不删；新增字段可选或有默认值。
- 汇率失败不阻塞订阅 CRUD。
- 提醒 worker 不应在启动时立刻对所有历史续费造成通知风暴；只对未来窗口内、未投递组合发送。
- Fixer key 不进入日志、前端响应、测试 fixture 或迁移。
- Dashboard 不恢复全量 KPI/字段仓库形态。
