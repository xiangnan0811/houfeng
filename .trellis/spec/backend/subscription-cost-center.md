# 订阅成本中枢规范

> 适用范围：VPS-first 订阅成本、汇率、预算、续费提醒、订阅工作台 API 与 Dashboard / VPS / Asset Decisions 成本信号。

---

## Scenario: Subscription Cost Center Contracts

### 1. Scope / Trigger

- Trigger: 修改 `internal/center/subscriptioncosts/`、`internal/center/store/subscription_costs.go`、`internal/center/http/handlers/subscription_costs.go`、`db/migrations/*subscription_cost*`、订阅设置 JSON、订阅汇率 Provider、预算模型、续费提醒 worker、或前端消费的订阅成本字段。
- 目标：订阅模块是 VPS Asset Ledger 的成本中枢，不是第二套 VPS 业务状态机；VPS 的用户侧业务决策仍以 `vps_assets.renewal_decision`、`lifecycle_status` 和 lifecycle action 为准。

### 2. Signatures

- Settings JSON: `center_settings.subscription_cost_settings`，字段包含 `base_currency`、`exchange_rate_provider`、`fixer_api_key`、`default_reminder_offsets_days`、`max_reminder_lead_days`、`exchange_rate_stale_after_hours`。
- DB tables: `subscription_exchange_rates(provider, base_currency, quote_currency, rate, rate_date, fetched_at, stale, error_summary)`、`subscription_budgets(budget_id, scope_type, scope_id, base_currency, monthly_limit, yearly_limit, warning_pct, enabled)`、`subscription_reminder_deliveries(subscription_id, renew_at, offset_days, reminder_kind, channel, delivery_status)`。
- Backend APIs:
  - `GET/PUT /api/subscriptions/settings`
  - `POST /api/subscriptions/exchange-rates/refresh`
  - `GET /api/subscriptions/overview`
  - `GET /api/subscriptions/statistics?window=month|quarter|year`
  - `GET/POST/PATCH /api/subscription-budgets`
  - `GET /api/subscriptions` appends cost, exchange, budget, and reminder derived fields.
- Workers: exchange-rate refresh worker and subscription reminder worker are wired in `cmd/houfeng-center/bootstrap.go`.
- Notification audit: subscription reminders reuse notifier dispatch capability and write `notification_records.object_type='subscription'` plus `subscription_reminder_deliveries` dedupe rows.

### 3. Contracts

- Default base currency is `CNY`; user may change it through settings.
- Fixer API key is secret material. It may be accepted in settings input or environment-backed config, but must never appear in migrations, source defaults, frontend responses, test snapshots, logs, or provider error summaries. Settings responses expose only `fixer_configured` and masked summary.
- Frankfurter is the default provider; Fixer is configurable. Provider failures must not block subscription CRUD; failed refresh responses may mark exchange data stale or missing.
- `subscriptions` may hold billing facts such as display name, labels, category, trial/end dates, price, currency, cycle, renewal date, auto-renew, payment, and note. Monthly/yearly base costs, exchange rate metadata, budget status, and next reminder are read-model fields, not writable subscription facts.
- Budget scopes are `global`、`provider`、`label`、`category`、`vps`。Disabled budgets must not affect budget status. PATCH must distinguish omitted limits from explicit JSON `null`.
- `next_reminder_at` is the next future pending reminder window calculated from settings and existing delivery rows. It must not report an already-delivered or past reminder as pending.
- Reminder dedupe is keyed by `subscription_id + renew_at + offset_days` independent of notification channel. The worker must reserve the dedupe row before dispatching notifications, then update delivery status after dispatch. This prevents duplicate sends on repeated scans.
- Ordinary renewal reminders skip cancelled/expired subscriptions and archived/cancelled VPS. Decision-attention reminders are allowed for cancellation/migration/auto-renew-cancelled decisions when a near-term renewal risk still exists.
- Dashboard may show only high-signal subscription summary: total base cost, future renewal count, budget risk, and exchange anomaly. Full filtering, budget CRUD, settings, and refresh actions stay in `/subscriptions`.

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Unknown or invalid base currency | settings write returns invalid input; stored value remains unchanged |
| Fixer key omitted in PUT | existing key is preserved |
| Fixer key set to empty string in PUT | existing key is cleared |
| Provider error contains `access_key` / `api_key` / `token` | response/status stores redacted error only |
| Exchange rate missing for non-base currency | derived base costs are `null`, `exchange_rate_stale=true` |
| Exchange cache older than stale threshold | derived row marks `exchange_rate_stale=true` |
| Budget has both monthly/yearly limits omitted or null | budget validation rejects missing effective limit |
| Budget PATCH omits a limit field | existing limit remains unchanged |
| Budget PATCH sends a limit as `null` | that limit is cleared, subject to at least one remaining limit |
| Reminder scan runs twice for same subscription/renewal/offset | dispatcher is called once; later scans return without sending |
| Dispatcher returns no channels | delivery row is updated to suppressed and audit can record suppressed status |
| Dashboard overview cannot read subscription overview | Dashboard UI may degrade; subscriptions workbench remains source of truth |

### 5. Good/Base/Bad Cases

- Good: active USD subscription has a fresh USD->CNY rate, global budget applies, overview reports CNY monthly/yearly cost and budget status.
- Good: Fixer returns a URL-like error containing `access_key=...`; stored refresh result shows `access_key=[redacted]`.
- Good: reminder worker inserts a `dispatch` delivery row, sends Telegram/Feishu through the shared dispatcher, then updates sent/failed/suppressed status and notification audit.
- Base: CNY subscription uses exchange rate `1` and `exchange_rate_stale=false`.
- Base: provider outage makes non-CNY rows stale/missing but does not break `GET /api/subscriptions`.
- Bad: subscription PATCH changes `vps_assets.renewal_decision` or lifecycle status.
- Bad: reminder worker dispatches first and writes dedupe after sending; a crash can duplicate renewal messages.
- Bad: Dashboard expands subscription overview into a full budget/settings workbench.

### 6. Tests Required

- Migration tests: settings JSON default, new subscription facts, exchange-rate cache, budget table, reminder delivery constraints/indexes.
- Provider tests: Frankfurter success, Fixer success, provider failure, stale fallback, missing currency, secret redaction.
- Cost tests: monthly, yearly, multi-month, zero price, base currency, non-base currency, stale exchange.
- Budget tests: all scope types, OK/warning/over/unknown, disabled budgets, PATCH null/omitted limits.
- Reminder tests: default `14/7/1`, max lead filtering, repeated scan dedupe, cancelled/archived suppression, decision-attention reminders, fake clock/fake notifier.
- Handler tests: overview, statistics, settings read/write, manual refresh, budget CRUD, extended subscription filters.
- Frontend/API tests: workbench loading/error/empty states, multi-currency display, budget risk, exchange anomaly, reminder settings, VPS cost card, Asset Decisions cost signals, Dashboard high-signal summary.

### 7. Wrong vs Correct

```go
// 错误：先发送通知，随后才写去重记录。进程崩溃或重复扫描会重复投递。
deliveries := dispatcher.Dispatch(ctx, summary)
_ = repo.CreateReminderDelivery(ctx, input)
```

```go
// 正确：先用 subscription_id + renew_at + offset_days 抢占去重记录，再发送并更新状态。
deliveryID, inserted, err := repo.TryCreateReminderDelivery(ctx, input)
if err != nil || !inserted {
    return err
}
deliveries := dispatcher.Dispatch(ctx, summary)
_ = repo.UpdateReminderDelivery(ctx, deliveryID, update)
```

```go
// 错误：把 provider 原始错误直接返回，可能泄露 fixer access_key。
return err.Error()
```

```go
// 正确：统一脱敏 secret-like query 参数，再截断错误摘要。
message := sensitiveProviderErrorPattern.ReplaceAllString(err.Error(), "$1=[redacted]")
```

```tsx
// 错误：Dashboard 承担完整订阅分析和设置入口。
<DashboardSubscriptionBudgetEditor budgets={overview.subscription_cost.budgets} />
```

```tsx
// 正确：Dashboard 只显示高信号摘要，把操作交给订阅工作台。
<Link to="/subscriptions">预算风险 {overview.subscription_cost.budget_risk_count}</Link>
```
