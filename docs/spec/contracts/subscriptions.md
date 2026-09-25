# 订阅与成本合同

## Asset Ledger subscriptions

`db/migrations/0018_add_subscriptions.sql` 添加 `subscriptions`，代表资产层 VPS 订阅账本。它依赖 `vps_assets.vps_id`，但不得反向改写 VPS 资产、Provider、MonitoringInstance、Target 或 Agent 状态。

- `subscriptions.subscription_id` 使用 `ids.New("sub")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时 `on delete cascade`。
- `currency` 使用大写 3 字母代码；领域层负责 trim + uppercase，数据库约束兜底。
- `price` 对齐数据库 `numeric(12, 2)`，领域层必须拒绝负数、超过精度或超过 2 位小数的输入，避免入库四舍五入后与派生字段漂移。
- `monthly_price` 是后端派生字段，由 `CalculateMonthlyPriceForPeriod` 按 day/week/month/year 周期与长度折算并四舍五入到 4 位小数；legacy `billing_months` 经归一化进入同一周期合同。create / patch JSON 不接受 `monthly_price`，修改价格或周期时必须重新计算。
- `started_at` 与 `renew_at` 是 nullable `date`：未知日期用 `null`，不要写假日期。
- `status` 使用稳定英文机器值：`active`、`paused`、`cancelled`、`expired`、`unknown`。新用户流程不得把它暴露为必填业务状态；VPS-scoped create 默认只收 price / currency / billing cycle / dates / auto-renew / payment / note 等账单事实，内部可保留 legacy status 作为兼容和历史解释字段。
- 订阅列表查询同样支持 `AssetScope`，通过关联 `vps_assets.lifecycle_status` 裁剪；默认 `current` 排除归档/已取消 VPS 的订阅，`asset_scope=historical` 供只读归档页查看已取消/已归档 VPS 的历史订阅；`asset_scope=archived` 是兼容别名。订阅自身 `status='cancelled'|'expired'` 不能让 VPS 自动进入归档范围，归档边界只能来自 VPS lifecycle。
- `renewal_mode` 允许 `auto`、`manual`、`auto_cancelled`、`lottery`、`gift`、`bonus`、`other`。`lottery` 只表达抽奖，`gift` 只表达赠送；两者都不是 legacy 自动续费标记。`LegacyRenewalFlags(gift)` 与 `LegacyRenewalFlags(lottery)` 必须返回 `false,false`。
- 订阅 CRUD 不得创建 `vps_monitoring_instance_links`、不得改写 `monitoring_instances.provider`、不得增加 Dashboard / import / currency exchange 行为。
- 订阅 CRUD 仍不得反向改写 VPS、MonitoringInstance 或 Target；订阅取消 / 过期后如资产状态不一致，前端必须暴露 lifecycle action 入口，而不是在订阅 PATCH 中隐式停机或退役。
- 受控例外：用户显式在 `PATCH /api/vps/{vps_id}` 将 VPS `renewal_decision` 改成取消类决策（当前为 `cancel` 或 `auto_renew_cancelled`）时，VPS patch 事务可以同步处理该 VPS 的明确订阅事实。只有恰好一条 `status='active'` 的订阅候选时，才能在同一事务里把该订阅 `auto_renew=false`、`auto_renew_cancelled=true`，并按既有 `price_histories` 机制记录自动续费字段变化；无 active 订阅或多 active 订阅时只返回 linkage status/message，不批量写订阅。
- 上述例外仍属于 Asset Ledger 内部 VPS↔Subscription 用户决策流：不得创建或修改 `vps_monitoring_instance_links`、Provider、MonitoringInstance、Target、ProbeItem、Agent 计划或运行时控制；subscription 自己的 CRUD 仍不得反向改写 VPS renewal decision。

### Scenario: Subscription renewal mode gift and historical scope

#### 1. Scope / Trigger

- Trigger: 修改 `subscriptions.renewal_mode`、`price_histories.from_renewal_mode/to_renewal_mode`、订阅列表 scope、订阅表单选项或续费方式展示标签。
- 目标：让账单行为、抽奖来源、赠送来源和历史资产范围在 DB、Go 和 UI 中保持同义。

#### 2. Signatures

- DB constraints: `subscriptions_renewal_mode_allowed` and `price_histories_renewal_mode_allowed` must include `gift` alongside `auto|manual|auto_cancelled|lottery|bonus|other`。
- Domain constant: `subscriptions.RenewalModeGift = "gift"`。
- List API: `GET /api/subscriptions?asset_scope=current|historical|archived|all`。
- Store behavior: `historical` and legacy `archived` both query related VPS `lifecycle_status in ('cancelled','archived')`。

#### 3. Contracts

- New migrations must not edit old applied migrations. To add a renewal mode, append a migration that drops/re-adds the two allowed constraints.
- `NormalizeRenewalMode` must trim and lowercase `gift` like all other machine values.
- `IsValidRenewalMode("gift")` and renewal history validation must both accept `gift`。
- `RenewalModeFromLegacyFlags` remains only `auto` / `auto_cancelled` / default `manual`; `gift` is not inferable from legacy booleans.
- Existing `lottery` rows stay `lottery` and display as 抽奖; there is no automatic backfill to `gift` without historical evidence.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| subscription create `renewal_mode=gift` | accepted; legacy booleans normalized to false/false |
| price history `from_renewal_mode=gift` or `to_renewal_mode=gift` | accepted |
| renewal mode `抽奖/赠送` or unknown string | invalid subscription input |
| `asset_scope=historical` | same SQL predicate as compatibility `archived` |
| `asset_scope=archived` | still accepted for old clients |

#### 5. Good/Base/Bad Cases

- Good: 用户录入赠送订阅，API 保存 `renewal_mode=gift`，前端显示“赠送”，不勾自动续费。
- Base: 旧抽奖订阅仍是 `lottery`，前端显示“抽奖”。
- Bad: 把 `lottery` 标签写成“抽奖/赠送”，导致用户无法区分权益来源。
- Bad: 只改 `subscriptions` constraint，忘记 `price_histories`，导致修改订阅时历史写入失败。

#### 6. Tests Required

- Migration tests: new migration contains both subscription and price history constraints with `gift`。
- Domain tests: `gift` normalize / validate / create / price history validation / legacy flags。
- Store/handler tests: subscriptions historical scope query parsing and SQL predicate。
- Frontend tests: `RenewalMode` union、option label、normalizer、legacy flags、Archive page historical query。

#### 7. Wrong vs Correct

```sql
-- 错误：只放松当前订阅表，历史表仍不能记录 gift。
alter table subscriptions add constraint subscriptions_renewal_mode_allowed check (renewal_mode in (..., 'gift'));
```

```sql
-- 正确：当前事实与价格历史的续费方式约束一起放松。
alter table subscriptions add constraint subscriptions_renewal_mode_allowed check (...);
alter table price_histories add constraint price_histories_renewal_mode_allowed check (...);
```

## VPS-scoped Subscription creation

`POST /api/vps/{vps_id}/subscriptions` 是普通补录订阅的主合同。path `vps_id` 是唯一 VPS 来源，请求体不得接受或覆盖 `vps_id`，也不得接受用户输入的 `status`。`decodeJSON` 必须 `DisallowUnknownFields`。

- 创建重放遵循下方 [幂等合同](#scenario-idempotent-vps-scoped-subscription-creation)。
- 请求字段只表达账单事实：`price`、`currency`、`billing_cycle`、`billing_months`、`billing_period_unit`、`billing_period_length`、`started_at`、`renew_at`、`auto_renew`、`auto_renew_cancelled`、`renewal_mode`、`payment_method`、`note`。
- 后端可以为 legacy `subscriptions.status` 写入内部默认值，但新 UI / API contract 不把它作为人工业务状态。
- 创建订阅不得反向修改 VPS lifecycle / usage / renewal decision，不得创建 MonitoringInstance link，不得修改 Provider、Target、ProbeItem 或 Agent。

### Scenario: Idempotent VPS-scoped subscription creation

#### 1. Scope / Trigger

- Trigger: 修改 `POST /api/subscriptions`、`POST /api/vps/{vps_id}/subscriptions`、`subscriptions.CreateInput`、`PostgresSubscriptionRepository.CreateSubscriptionIdempotent`、`subscription_create_idempotency` schema 或 APP ACL。

#### 2. Signatures

- HTTP: `POST /api/subscriptions` 与 `POST /api/vps/{vps_id}/subscriptions` 均要求 header `Idempotency-Key: <8..128 characters>`。collection body 是完整 `CreateInput`；scoped body 是账单事实 DTO `vpsSubscriptionCreateRequest`，拒绝未知字段。
- Store: `CreateSubscriptionIdempotent(ctx, input, idempotencyKey) (record, replayed, error)`。
- DB: `subscription_create_idempotency(idempotency_key text primary key, request_digest text, subscription_id text references subscriptions on delete cascade, created_at timestamptz)`。

#### 3. Contracts

- key trim 后长度必须为 8..128，只允许 `[A-Za-z0-9._:-]`；HTTP 与数据库约束必须一致。
- digest 是 normalize 后完整 `CreateInput`（包含 path `vps_id`、周期字段、续费模式和内部默认 status）的 canonical JSON SHA-256；`nil` labels 必须按空数组计算。
- repository 必须在同一事务内按 key 获取 advisory transaction lock、检查 receipt、插入 subscription 与 receipt；不能先提交 subscription 再记录 key。
- 首次创建返回 201；相同 key + 相同 digest 返回原 subscription 和 200；相同 key + 不同 digest 不写入并返回 coded 409。
- APP runtime role 只需要 receipt 表 `select` / `insert`；不授予 `update` / `delete`。
- `subscription_create_idempotency` receipt 与 subscription row 同寿命；没有 TTL、janitor 或重放窗口。删除 subscription 会 cascade receipt，当前没有用户侧 subscription DELETE。备份恢复同时恢复 receipt，旧 key 继续有效；每次成功创建占一行，可通过 SQL 查看容量，不为该表新增指标平台。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| header 缺失、过短、过长或含非法字符 | 400 `invalid_idempotency_key`，不创建 subscription |
| body 有未知字段 | 400 `invalid json`，不创建 subscription |
| body 账单事实非法 | 400 `invalid input`，不创建 receipt |
| key 首次出现 | 同事务创建 subscription + receipt，201 |
| key 与 digest 均相同 | 返回 receipt 指向的原记录，200，不新增行 |
| key 相同但 digest 不同 | 409 `idempotency_key_reused`，不新增行 |
| subscription insert、receipt insert 或 commit 失败 | 事务回滚，不留下孤立 subscription 或 receipt |

#### 5. Good/Base/Bad Cases

- Good: 客户端收到 201 前断网，使用同一 key 和同一草稿重试；服务端返回 200 原记录，数据库仍只有一条 subscription。
- Base: 新草稿使用新 key，正常创建另一条订阅事实。
- Bad: 网络失败后生成新 key，可能把已经提交但丢失响应的请求再创建一次。
- Bad: 只用 key 不比较 digest，使同一 key 能静默返回另一份业务输入的结果。

#### 6. Tests Required

- Domain unit: key 长度/字符边界；normalize 后 digest 稳定；任一业务字段变化都会改变 digest。
- Handler: 真实周期字段可解码；缺 key 400；replay 200；reused key 409 且 code 稳定。
- PostgreSQL integration: 模拟丢失 201 后 replay，断言 subscription 与 receipt 各一行；不同 digest 冲突不写入。
- Migration/ACL: table、FK/check/index 存在；APP role 只有 `select` / `insert`。

#### 7. Wrong vs Correct

```go
// 错误：先创建 subscription，再在另一个事务记录 key。
record, err := repo.CreateSubscription(ctx, input)
_, _ = db.Exec(ctx, `insert into subscription_create_idempotency (...) values (...)`)
```

```go
// 正确：key lock、digest 检查、subscription 和 receipt 共用一个事务。
record, replayed, err := repo.CreateSubscriptionIdempotent(ctx, input, idempotencyKey)
```

> 适用范围：VPS-first 订阅成本、汇率、预算、续费提醒、订阅工作台 API 与 Dashboard / VPS / Asset Decisions 成本信号。

---

## Scenario: Subscription PATCH ownership and renewal normalization

### 1. Scope / Trigger

- Trigger: 修改普通 `PATCH /api/subscriptions/{subscription_id}`、订阅创建幂等回放、续费模式或 legacy `auto_renew` / `auto_renew_cancelled` 归一化。
- 目标：订阅的 VPS 归属只能由创建事实确定；legacy flags 的部分更新不能因 Go 零值而丢失当前账单来源或续费意向。

### 2. Contracts

- 普通 PATCH 不允许将订阅改挂到另一 VPS。省略 `vps_id` 不改归属；显式提交当前 `vps_id` 作为 no-op 接受，并且不写归属/价格历史；提交不同 VPS 返回 HTTP 409 `subscription_ownership_change_forbidden`。
- `CreateSubscriptionIdempotent` 在匹配 receipt digest 后，必须在提交重放事务前读回 subscription 并验证其当前 `vps_id` 等于本次规范化输入的 `vps_id`。不一致时返回 HTTP 409 `subscription_replay_ownership_conflict`，不得返回他属记录或创建替代对象。
- 同 key 的不同规范化请求仍返回 HTTP 409 `idempotency_key_reused`。Receipt 指向不存在的 subscription 是内部一致性错误，不能当作 ownership conflict 或自动重建。
- `NormalizePatchInput` 只做与当前记录无关的存在字段规范化；缺失的 legacy bool 不得当作 `false` 推导 `renewal_mode`。生产 PATCH 在锁定并读取当前订阅后调用 `NormalizePatchAgainstRecord(current, input)`。
- 显式 PATCH `renewal_mode` 可切换来源并按该 mode 生成两个 legacy flags。仅 PATCH legacy flags 时，`auto` / `manual` / `auto_cancelled` 必须将未提供的另一 flag 与当前记录合成后再映射 mode。
- `gift`、`lottery`、`bonus`、`other` 是独立来源，不从 legacy flags 推断或覆盖；取消/归一这类账单时保留当前来源并持久化 `auto_renew=false`、`auto_renew_cancelled=false`。
- 订阅 `status` 与续费字段没有新增互斥约束。正常 PATCH 允许把 `cancelled` 修正回 `active`，也允许独立纠正自动续费事实。

### 3. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| PATCH 未提供 `vps_id` | 原归属不变 |
| PATCH 显式提交相同 `vps_id` | 200；作为归属 no-op，不写归属/价格历史 |
| PATCH 提交不同 `vps_id` | 409 `subscription_ownership_change_forbidden`；其它 PATCH 字段也不写入 |
| idempotency receipt digest 相同且当前归属与请求一致 | 200，返回原记录 |
| idempotency receipt digest 相同但当前归属与请求不一致 | 409 `subscription_replay_ownership_conflict`；不提交或替代创建 |
| 同 key、不同规范化请求 | 409 `idempotency_key_reused` |
| 订阅状态 `cancelled` PATCH 为 `active` | 接受；续费事实只按显式输入或当前记录归一 |
| 部分 legacy flag PATCH | 未提供 flag 取锁定当前记录值后再映射；非 legacy 来源保留且 flags 为 false/false |

### 4. Good/Base/Bad Cases

- Good: 对同一订阅提交当前 VPS ID，不改变归属或追加历史；对不同 VPS ID 的 PATCH 整体拒绝。
- Good: 历史上经受控数据修复改过归属的 receipt 被原始创建请求重放时，返回专用冲突，不把已改挂记录交给旧 VPS 请求方。
- Good: `auto` 订阅只 PATCH `auto_renew_cancelled=true` 时，读取锁定记录中的 `auto_renew=true` 并得到 `auto_cancelled`；`gift` 等来源在取消路径仍保留原 mode 与 false/false flags。
- Bad: PATCH 单独一个 bool 时把另一个缺失 bool 当作 false，再误写 `manual` 或覆盖 `gift` / `lottery` 来源。
- Bad: receipt digest 匹配便直接返回记录，不检查该记录当前是否仍归属于请求中的 VPS。

### 5. Tests Required

- Domain: context-free partial bool 不派生 mode；四种非 legacy 来源保留，legacy 三种 mode 按当前未提供 flag 合成。
- PostgreSQL: 改挂 PATCH 返回冲突且不写其它字段；相同 VPS ID 接受为 no-op；用受控 SQL 构造历史改挂后验证 receipt 重放冲突；legacy 来源/部分 flag/status 纠错使用生产读写。
- Handler: 两种 ownership conflict 分别返回稳定 409 code。

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
  - `GET/PUT /api/subscription-monthly-budgets`、`PUT /api/subscription-monthly-budgets/{month}`、`POST /api/subscription-monthly-budgets/bulk`；path month 存在时覆盖 body 月份，月度预算与普通 scope budget 是独立入口。
  - `GET /api/subscriptions` appends cost, exchange, budget, and reminder derived fields.
- Workers: exchange-rate refresh worker and subscription reminder worker are wired in `cmd/houfeng-center/bootstrap.go`.
- Notification audit: subscription reminders reuse notifier dispatch capability and write `notification_records.object_type='subscription'` plus `subscription_reminder_deliveries` dedupe rows.

### 3. Contracts

- Default base currency is `CNY`; user may change it through settings.
- Fixer API key is secret material. It may be accepted in settings input or environment-backed config, but must never appear in migrations, source defaults, frontend responses, test snapshots, logs, or provider error summaries. Settings responses expose only `fixer_configured` and masked summary.
- Frankfurter is the default provider; Fixer is configurable. Provider failures must not block subscription CRUD; failed refresh responses may mark exchange data stale or missing.
- `subscriptions` may hold billing facts such as display name, labels, category, trial/end dates, price, currency, cycle, renewal date, auto-renew, payment, and note. Monthly/yearly base costs, exchange rate metadata, budget status, and next reminder are read-model fields, not writable subscription facts.
- 订阅创建的 header、错误、事务和 receipt 生命周期统一见 [幂等合同](#scenario-idempotent-vps-scoped-subscription-creation)。成本功能不另建创建或重放路径。
- Budget scopes are `global`、`provider`、`label`、`category`、`vps`。Disabled budgets must not affect budget status. PATCH must distinguish omitted limits from explicit JSON `null`.
- 月度预算继承取 `budget_month <= bucket_start` 的最近历史月配置，不取未来月份，也不将“无当月行”误判为没有预算；统计与 evidence adapter 必须沿用同一继承规则。
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

## Scenario: VPS scoped subscription create 字段 presence 合同

### 1. Scope / Trigger

- Trigger: 修改 `POST /api/vps/{vps_id}/subscriptions`、`vpsSubscriptionCreateRequest`、`vps_subscription_create_fields.json`、`CreateVPSSubscriptionInput`，或复用 `subscriptions.Optional*` 解码字段时。
- 目标：shared manifest 的 `required` / `nullable` 不只是静态文档；VPS scoped HTTP 门必须区分字段缺失、JSON `null` 和显式零值，避免 Go 零值把不完整请求伪装成合法输入。

### 2. Signatures

- Scoped API: `POST /api/vps/{vps_id}/subscriptions` + caller-owned `Idempotency-Key`。
- Runtime boundary: `vpsSubscriptionCreateRequest.toCreateInput(vpsID) (subscriptions.CreateInput, bool)`；`bool=false` 表示 required presence 不完整，handler 返回 `400 invalid input`。
- Required/non-null fields:
  - `price: number`
  - `currency: string`
  - `billing_cycle: string`
  - `billing_months: number`
  - `auto_renew: boolean`
  - `auto_renew_cancelled: boolean`
  - `payment_method: string`
  - `note: string`
- Optional/non-null fields: `billing_period_unit: string`、`billing_period_length: number`、`renewal_mode: string`。
- Optional/nullable date fields: `started_at: date | null`、`renew_at: date | null`。

### 3. Contracts

- Scoped request 使用已有 `subscriptions.OptionalString` / `OptionalFloat` / `OptionalInt` / `OptionalBool` / `OptionalDate`。禁止新增另一套 presence wrapper。
- Required 字段同时有 `required:"true"` struct tag 和 `.Set` runtime 检查；检查 presence，不检查 truthiness。`0`、`false`、`""` 均表示字段已显式提供。
- 非空 scalar wrapper 遇到 JSON `null` 必须在 decode 阶段失败；`OptionalDate` 接受 `null` 并映射为 nil date。
- decode 与 required-presence 检查必须发生在 normalize、domain validate、idempotency 与 repository 调用之前。非法输入不得写 receipt，也不得调用 create repository。
- `vps_subscription_create_fields.json`、Go json tag/type/required/nullable 和 TypeScript DTO 必须按顺序完全一致；contract test 必须同时比较四个维度。
- Go/TS mirror 对每个 exported Go DTO 字段都要求显式、非空 JSON 名称；只有精确 `json:"-"` 可以忽略。缺 tag、`json:""` 或 `json:",omitempty"` 必须 fail closed，因为 `encoding/json` 会按 exported 字段名暴露这些字段。
- Go/TS mirror 遇到 anonymous embedded field 时同样 fail closed：除非该字段精确标记 `json:"-"`，不得静默跳过或展平。该规则同时覆盖未导出的 anonymous struct type，因为 `encoding/json` 仍可能提升其 exported members；bounded parser 不负责递归展开 promoted wire surface。
- Go type classifier 只接受明确列出的 built-in wire scalar 与 `subscriptions.Optional*` / `Date` 类型；未知 named DTO type 必须 fail closed，不得按底层 `reflect.Kind` 猜测成合法 manifest type。defined/named pointer 必须在 `Elem()` 前拒绝；普通 pointer 的 element 仍需通过同一精确白名单。
- `BillingPeriodUnit` / `RenewalMode` 不是按名字永久信任的 string。TS 与 Go mirrors 必须从同一 TypeScript source 读取 alias 定义，并要求每个 member 都是非空 string literal；加入 `number`、`undefined`、未知 alias 或空 member 时立即 fail closed。
- Bounded TypeScript object parser 必须校验目标 closing brace 同行剩余文本，只允许空白或一个分号；`& { ... }` 等 intersection/suffix 不得被首个 `\n}` 截断后忽略。TS Go-source mirror 在 anonymous-field token 分类前必须处理或拒绝 trailing inline comment，避免合法 embedding 因多 token 被当作普通未导出字段跳过。
- DTO 与 approved alias marker 都必须恰有一个位于注释/字符串之外、行首仅有水平空白的 live declaration；block-comment shadow-only 或 shadow+live 均 fail closed。alias union tokenization 必须识别 quotes/escapes，使 literal 内的 `|` 不成为 union separator。
- TS Go-source mirror 解析 raw struct tag 时必须精确匹配 `json` / `required` key，不能用 substring 让 `notjson` / `notrequired` 冒充；Go type helper 对未知 named primitive 直接 panic/reject，不能只返回一个等待最终 equality 发现的 mismatch string。
- Object mirror 在 closing brace 同行无分号时，必须跳过后续空白与注释 trivia 并拒绝下一有效 token 为 `&` / `|`；alias mirror 不得只验证定义首行，任何多行 `&` / `|` continuation 都必须完整验证或 fail closed。TS Go-source mirror 必须剥离 raw tag 外的闭合 line/block comment、保留 tag 内 comment marker，并对未闭合/跨行 block comment fail closed。
- Collection `POST /api/subscriptions` 继续接受完整 `subscriptions.CreateInput`，不得因为 scoped DTO 收紧而换成 wrapper request。
- Scoped handler 继续拒绝未知字段，并保留 VPS path 绑定、默认 status、normalize/validate、幂等 replay/reuse 与成功响应语义。
- `status` 等 collection-only 字段的 scoped unknown-field 回归必须携带有效 `Idempotency-Key`，精确断言响应 `error: "invalid json"`，并证明 repository/idempotency create 调用数为零；不能让缺 key 的 400 代替 strict-decode 证据。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| 任一 required 字段缺失 | `400 invalid input`；repository / idempotency 调用数为 0 |
| required 或 optional-non-null scalar 显式 `null` | `400 invalid json`；repository / idempotency 调用数为 0 |
| `started_at` / `renew_at` 缺失或显式 `null` | 接受；domain input 对应 date 为 nil |
| `price: 0`、两个 boolean 为 `false` | 算已提供，原值进入 domain input |
| `payment_method: ""`、`note: ""` | 算已提供；现有 normalize 可 trim，但 presence 校验不得拒绝 |
| required 字段已提供但业务值非法 | 由现有 `NormalizeCreateInput` / `ValidateCreateInput` 返回 `400 invalid input` |
| body 包含 collection-only 或未知字段 | `400 invalid json`，保持 strict decode |
| exported DTO field 缺少/使用空 JSON 名称 | contract parser 失败；不得静默从 manifest 比较中省略该 wire field |
| unknown named pointer 的底层元素恰为受支持 date/scalar | Go mirror 在解引用前失败；不得按 element 猜测合法 nullable 字段 |
| anonymous embedded field 未精确标记 `json:"-"` | Go 与 TS mirror 都失败；不得静默省略 encoding/json 可见的 promoted wire surface |
| scoped payload 带 `status` 且有有效 idempotency key | `400` 且响应 `error: "invalid json"`；repository/idempotency create 调用数为 0 |
| approved string alias 加入 `number` / `undefined` | TS 与 Go mirrors 都失败；不得继续按 alias 名称分类为 string |
| object type closing brace 后存在 intersection/non-semicolon suffix | TS 与 Go mirrors 都失败；额外字段不得从 exact-field 比较中消失 |
| anonymous Go embedding 带 trailing inline comment | TS Go-source mirror 仍按 anonymous field fail closed；不得按 lowercase ordinary field 跳过 |
| DTO/alias declaration 只存在于 block comment，或 comment shadow 与 live declaration 并存 | 两侧 mirror 都失败；不得选择第一个 raw substring |
| raw tag 只有 `notjson` / `notrequired` | 不得当成 `json` / `required`；exported field 缺真实 json key 时失败 |
| object closing brace 或 approved alias 后在下一有效行继续 `&` / `|` | 两侧 mirror fail closed；不得只信任 closing/alias 首行 |
| anonymous Go embedding 带 inline block comment | TS mirror 仍按 anonymous field 拒绝；raw tag 内 `/* */` 保持原样，未闭合 comment 直接失败 |

### 5. Good/Base/Bad Cases

- Good: 完整 payload 明确发送 `price: 0`、`auto_renew: false`、`auto_renew_cancelled: false`、空 payment/note；repository 收到精确零值，不被当作 missing。
- Base: optional billing/renewal 字段缺失，由既有 normalize/default 逻辑补齐；nullable dates 为 nil。
- Bad: plain `float64` / `bool` / `string` request 字段把 missing/null 折叠成 Go 零值，随后创建 subscription。
- Bad: 用 `if request.Price == 0` 或 `if request.Note == ""` 判断 required，错误拒绝合法显式零值/空字符串。
- Bad: 只让 manifest/contract test 声明 required，却不在真实 handler 执行 presence 检查。
- Bad: 把空 `json` tag 与 `json:"-"` 一并跳过；前者仍由 `encoding/json` 暴露。
- Bad: 对任意 pointer 先取 `Elem()`，使未知具名 pointer 绕过 exact type allowlist。
- Bad: 因 anonymous field 本身未导出就跳过；其 exported members 仍可能被 `encoding/json` 提升到外层 wire object。
- Bad: unknown-field 测试不带 idempotency key，只看到相同的 400，实际可能从未到达 JSON strict decode。
- Bad: `switch` 只按 `BillingPeriodUnit` / `RenewalMode` 名称返回 string，却不读取 alias 定义。
- Bad: 在第一个 `\n}` 截断 object type，或在 counting tokens 前保留 trailing comment，使 intersection/anonymous embedding 从镜像中消失。
- Bad: 用第一个 marker substring 选择 DTO/alias，或用 tag-key substring 匹配；注释 shadow 与 near-miss key 会制造 source-only 假绿。
- Bad: 只检查 closing brace / alias 定义所在行，忽略下一行 `&` / `|`；或只剥离 `//` 导致 block-comment anonymous embedding 被按 lowercase 普通字段跳过。

### 6. Tests Required

- Real-handler table tests：逐个删除八个 required key，再逐个发送 null；断言 HTTP 400 和 repository 调用数为 0。
- Real-handler table tests：三个 optional-non-null 字段逐个 null 均 400；两种 nullable date 的 missing/null 均成功且映射 nil。
- Mapping test：完整 wrapper request 的每个字段都精确进入 `CreateInput`，防止新增/重排字段漏映射。
- Required-tag enforcement test：从 struct tag 枚举 required 字段，证明每个 tagged field 的 `.Set=false` 都被 runtime boundary 拒绝；只设置 required fields 时 optional fields 不会被误判 required。
- Contract tests：manifest、Go request、TypeScript DTO 的 name/type/required/nullable 完全一致，并有 unknown named Go DTO type、TypeScript union、requiredness 与 date nullability drift negative cases。
- Contract parser negatives：合成 exported 字段分别使用 missing tag、空 tag、空名称 options 与精确 dash；前三者必须失败，只有 dash 忽略。另直接拒绝底层为受支持 date 的 unknown named pointer。
- Contract parser negatives：合成 exported 与 unexported-type anonymous embedding；所有非 dash anonymous fields 都必须在 Go/TS mirrors 失败，精确 `json:"-"` 可忽略。
- Scoped `status` rejection：发送 contract-complete payload 和有效 `Idempotency-Key`，精确断言 `400` + `error: "invalid json"`，并断言 `createSubscriptionCalls == 0` 且 idempotency key capture 仍为空。
- Alias-definition negatives：在 synthetic source 中把 `BillingPeriodUnit` / `RenewalMode` 扩宽为 `number`、`undefined`；TS 与 Go mirrors 都拒绝，真实纯 string-literal aliases 通过。
- Object/source-shape negatives：两个 TS-object mirrors 拒绝 `} & { debug?: string }`；TS Go-source mirror 对带 trailing `//` comment 的 anonymous embedding 仍显式失败。
- Marker/tag/token negatives：两侧拒绝 DTO/alias block-comment shadow-only 与 shadow+live；TS Go-source mirror 拒绝 `notjson`/忽略 `notrequired`；Go unknown named primitive 直接失败；escaped/embedded-pipe string literals 作为合法 alias members 通过。
- Multiline/comment negatives：两侧拒绝 later-line object `&` / `|` continuation 与 `BillingPeriodUnit` / `RenewalMode` continuation-line widening；TS Go-source mirror 拒绝 inline-block-comment anonymous embedding 和未闭合 block comment，并证明 raw tag 内 comment marker 不被剥离。
- Regression：scoped success、unknown field、missing key、replay、same-key-different-digest、repository error 与整个 handler package 继续通过；collection fixtures保持原合同。

### 7. Wrong vs Correct

```go
// 错误：missing/null 被 json decoder 折叠成零值，无法证明字段出现过。
type vpsSubscriptionCreateRequest struct {
	Price     float64 `json:"price"`
	AutoRenew bool    `json:"auto_renew"`
}
```

```go
// 正确：wrapper 记录 presence；required tag 同时受 semantic contract test 保护。
type vpsSubscriptionCreateRequest struct {
	Price     subscriptions.OptionalFloat `json:"price" required:"true"`
	AutoRenew subscriptions.OptionalBool  `json:"auto_renew" required:"true"`
}

if !request.Price.Set || !request.AutoRenew.Set {
	writeError(w, http.StatusBadRequest, "invalid input")
	return
}
```

```go
// 错误：空 tag 被当作忽略，任意 pointer 先 Elem 后再猜类型。
tag := field.Tag.Get("json")
if tag == "" { continue }
if field.Type.Kind() == reflect.Pointer { fieldType = field.Type.Elem() }

// 正确：只有精确 dash 可忽略；exported 字段必须有可用名称，具名 pointer 先拒绝。
tag, present := field.Tag.Lookup("json")
if tag == "-" { continue }
name, _, _ := strings.Cut(tag, ",")
if !present || name == "" { panic("exported field requires explicit json name") }
if field.Type.Kind() == reflect.Pointer && field.Type.Name() != "" {
	panic("unknown named pointer type")
}
```
