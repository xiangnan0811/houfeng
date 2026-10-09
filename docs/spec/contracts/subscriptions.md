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
- 订阅列表通过所属 VPS 生命周期裁剪：默认 `asset_scope=current` 只返回管理中 VPS，`archived` 返回已归档 VPS 的账单，`all` 返回全部。订阅自身 `status='cancelled'|'expired'` 不能让 VPS 自动归档。
- `renewal_mode` 只允许 `auto|manual|auto_cancelled`。获取来源在 VPS `acquisition_source` 独立记录。
- 订阅 CRUD 不得创建 `vps_monitoring_instance_links`、不得改写 `monitoring_instances.provider`、monitoring instance lifecycle、Target 或 Agent。成功提交后仅向既有汇率 worker 发非阻塞补取信号，不在请求中调用外部汇率网络，也不改变账单成功结果。
- 订阅 CRUD 仍不得反向改写 VPS、MonitoringInstance 或 Target；订阅取消 / 过期后如资产状态不一致，前端必须暴露 lifecycle action 入口，而不是在订阅 PATCH 中隐式停机或退役。
- 续费意向与服务商自动续费独立：保存 VPS `renewal_decision=cancel` 只记录意向、原因、复核时间和历史，不更改任何订阅自动续费事实。界面提示核对 VPS `auto_renew_check`。
- 记录一次续费不得将 cancel 自动改为 keep；用户明确确认新的 VPS `validity_mode/expires_at`，账单 `renew_at` 不自动覆盖资源有效期，二者不一致仅提示。
- 已归档 VPS 仍可补录和修订订阅账单事实，保留价格历史，不改变 VPS 生命周期、有效期、监控或命令。账单仍可能显示 active / 自动续费开启，以如实记录潜在扣费。
- 已归档订阅的所有实际变更（包括备注、退款说明、证据引用）同事务追加账单修订体验记录，保存 before/after；金额退款使用说明与已有证据能力记录，不以负订阅价格构造完整会计系统。
- 成本概览 `vps_costs` 和当前预计月/年成本只统计管理中 VPS；`current_unknown_amount_count` 表示已知成本合计不完整。已归档对象单独返回 `archived_potential_costs`、`archived_missing_subscription_assets`、`archived_unknown_amount_count` 与 nullable `archived_potential_monthly_cost`，未知金额不按零处理。
- 归档潜在扣费仅纳入服务商自动续费核对为 unchecked/enabled 的对象（空值防御同未知）；disabled/never_enabled/unsupported 已完成核对，不计潜在续费。缺账单或缺汇率时归档总额为 null；预算覆盖的当前金额未知且已知合计未超限时状态为 unknown，不能宣称预算正常。成本行查询保留归档对象以供核对。
- 来源在 VPS `acquisition_source` 记录；新流程的续费方式只表达付款安排。历史账单字段不构成 VPS 生命周期或有效期权威。

### Scenario: 续费方式与获取来源分离

- `subscriptions.renewal_mode` 只允许 `auto|manual|auto_cancelled`，价格历史使用相同约束。
- `gift|lottery|bonus|other` 等来源只写 VPS `acquisition_source`，不能作为 renewal_mode 创建、更新或导入；没有自动转换和兼容别名。
- `NormalizeRenewalMode` 只去空白与规范大小写，不把未知或来源值猜成手动续费；校验返回 invalid subscription input。
- 账单日期、资源有效期、自动续费核对与续费意向互不覆盖；补录账单和续费历史不能隐式恢复监控或 VPS。
- 回归覆盖来源模式拒绝、合法模式读写、取消意向不写订阅，以及无订阅的有效期修改。

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

- HTTP: `POST /api/subscriptions` 与 `POST /api/vps/{vps_id}/subscriptions` 均要求 header `Idempotency-Key: <8..128 characters>`。collection body 是完整 `CreateInput`；scoped body 是账单事实 DTO `vpsSubscriptionCreateRequest`（含可选的订阅名称 `display_name`），拒绝未知字段。
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
- 显式 PATCH `renewal_mode` 只切换续费方式并按该 mode 生成两个 flags。仅 PATCH flags 时，`auto` / `manual` / `auto_cancelled` 必须将未提供的另一 flag 与当前记录合成后再映射 mode。
- 来源只属于 VPS `acquisition_source`，订阅 PATCH 不接受 gift/lottery/bonus/other 模式，也不改写来源。
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
- Good: `auto` 订阅只 PATCH `auto_renew_cancelled=true` 时，读取锁定记录中的 `auto_renew=true` 并得到 `auto_cancelled`；VPS 来源不变。
- Bad: PATCH 单独一个 bool 时把另一个缺失 bool 当作 false，再误写 `manual` 或更改 VPS 来源。
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
- DB tables: `subscription_exchange_rates(provider, base_currency, quote_currency, rate, rate_date, fetched_at)` 只保存成功的正汇率；`subscription_budgets(budget_id, scope_type, scope_id, base_currency, monthly_limit, yearly_limit, warning_pct, enabled)`；`subscription_reminder_deliveries(subscription_id, renew_at, offset_days, reminder_kind, channel, delivery_status)`。
- Backend APIs:
  - `GET/PUT /api/subscriptions/settings`
  - `GET /api/subscriptions/exchange-rates/status` 与 `POST /api/subscriptions/exchange-rates/refresh`，后者返回 202，仅证明任务受理。
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
- Frankfurter is the default provider; Fixer is configurable. Provider failures must not block subscription CRUD. 成功缓存的可用性与补取任务状态分离，不将失败或零汇率写入缓存。
- 订阅成本设置与全局中心设置都必须通过同一 `MutateSettings` 原子入口修改：事务按 `READ COMMITTED` 初始化缺失的 `center` 单例、锁定并读取最新完整行、只调用一次 mutation callback、校验后写回完整设置并提交；提交前的 callback、校验或 SQL 失败整笔回滚。提交返回错误时结果可能未知（例如数据库已提交但响应丢失），必须向调用方返回失败，不得宣称一定回滚，也不得自动重放旧的完整设置。读取缺失行仍只返回默认值，不创建单例。
- `PUT /api/settings` 与 `PUT /api/subscriptions/settings` 在 callback 内基于锁定后的最新值合并服务端省略字段，禁止先 `GET` 再使用旧快照写回。订阅成本的 `fixer_api_key` 省略时保留已存密钥，显式空字符串才清除；响应只公开 `fixer_configured` 与 masked summary，绝不回显明文。
- `subscriptions` may hold billing facts such as display name, labels, category, trial/end dates, price, currency, cycle, renewal date, auto-renew, payment, and note. Monthly/yearly base costs, exchange rate metadata, budget status, and next reminder are read-model fields, not writable subscription facts.
- 订阅创建的 header、错误、事务和 receipt 生命周期统一见 [幂等合同](#scenario-idempotent-vps-scoped-subscription-creation)。成本功能不另建创建或重放路径。
- `CostRow`、订阅记录及续费队列统一返回 `exchange_rate_status: identity|fresh|stale|missing`，不再返回 `exchange_rate_stale`。同基准币种为 identity（原价 0 仍为已知 0）；缺汇率金额为 null；已有过期汇率继续返回数值并标 stale。当前概览分别返回 `current_missing_rate_count`、`current_stale_rate_count`，保留 `current_unknown_amount_count`；归档行不驱动补取。
- 成本趋势 `cost_month_buckets` 按订阅开始日期入桶：`started_at` 存在时自该日所在月起计入，未知时才退回 `created_at`，补录的存量订阅不会让此前月份显示为 0、录入当月陡升。非基准币种月份优先用当月末之前最近取得的汇率；月末之前没有该币对的汇率记录时（常见于补录的历史月份；汇率缓存同一 `rate_date` 刷新会覆盖 `fetched_at`，故不代表当时确实从未取得），按此后最早的汇率记录估算并在该月标 `rate_estimated=true`（前端在趋势标题注明“部分月份缺少当时的汇率记录，按此后最早的汇率估算”，照常绘制）；该币对没有任何汇率记录时才标 `data_insufficient`，不以 0 冒充。当前月概览总额仍按当前有效订阅统计，与趋势的开始日期口径分开。
- 当前概览的 `upcoming_renewals` 只含 UTC 当天起 `[0, 90]` 天的续费；续费日早于当天且 VPS `renewal_decision` 不是 `cancel` 的当前行进入 `overdue_renewals`（按续费日升序、最多 12 条），`overdue_renewal_count` 返回截断前总数。两者口径与 VPS 概览 `renewal.overdue.v1` 一致，不改变 90 天窗口。
- 有效币对由 active subscriptions 与 active VPS 驱动，按当前 provider/base/quote 连接 `fetched_at DESC, rate_date DESC` 最新成功缓存；沿用既有过期阈值严格比较，不混用旧基准或旧 provider 的值。
- `ExchangeRateWorker.Run(ctx)` 是唯一外部取汇率执行者；启动扫描、每 30 秒 reconcile、12 小时周期和容量 1 唤醒 channel 共用一条顺序执行 lane。成功写入口调用 `RequestRefresh(false)`；人工刷新调用 `RequestRefresh(true)`；信号非阻塞并合并，运行中同币对信号不导致相同调用再次排队。每次网络请求从应用 ctx 派生 10 秒 timeout。
- 普通轮次只补 missing/stale；已开始的人工强制刷新即使成功缓存仍为 fresh，其已排程重试也必须按既有 deadline 与 attempt budget 执行，不因每次重试而重置预算。失败后按 5、30、120 秒进行三次自动重试，耗尽后保留 failed；普通 CRUD 和 30 秒扫描不能重置失败预算，12 小时周期或人工重试可开启新轮。凭据/base/provider 变化开启当前设置新轮，过期阈值变化只重算 eligible；请求中的旧币对成功结果仍按旧币对保存为历史，下轮读取最新设置。
- “开启新轮/重置失败预算”不等于强制取 fresh：启动、12 小时周期及仅凭据变化均复用 fresh 缓存。人工刷新按币对合并；正在运行的币对由当前调用满足请求，其他已完成或未运行币对各保留一次待执行刷新。状态查询不能消费 worker 尚未处理的新币对刷新请求；在 worker 尚未发现该币对期间，状态可将 pending force discovery 的非 identity 币对投影为 `queued`，但必须保留 discovery marker 供 worker 消费。A 已完成而 B 在途时多次人工刷新，B 不重复排队，A 下一轮恰好再取一次，两者最终退出 queued/running。
- 全局/scoped 订阅创建（共用写入口）、PATCH 换币或恢复 active、VPS 归档恢复、两个 settings 更新入口仅在成功提交后通知 worker。幂等 replay 可合并通知但不能二次创建；worker 关闭或外部失败不能把已提交业务改报失败。独立 importer 最迟由 30 秒 reconcile 发现。
- 状态 GET 只读，不触发外部请求。两条汇率 API 均返回 `{items: ExchangeRatePairStatus[]}`，项包含 `provider, base_currency, quote_currency, rate_status, refresh_status, last_attempt_at?, next_retry_at?, attempt_count, error_summary?`；rate_status 为上述四态，refresh_status 为 `idle|queued|running|failed`。错误只提供脱敏摘要，禁止密钥及完整 URL。任务状态仅进程内保存，成功后清除错误/attempt，非活跃币对移出视图但不删缓存；重启从缓存重建并重新尝试。应用退出取消 timer 和在途网络，Run 返回，不遗留请求级 goroutine。
- 所有成本摘要与图表按完整性限定：未知数大于零时为“已知金额小计（另有 N 项待核对）”，占比仅指已知金额；全部未知时显示金额待核对，不以零冒充总额。无订阅且无缺口可显示完整零；预算风险零不证明金额完整。
- Budget scopes are `global`、`provider`、`label`、`category`、`vps`。Disabled budgets must not affect budget status. PATCH must distinguish omitted limits from explicit JSON `null`.

#### Budget derived amount and status

- `current_monthly_spend` and `current_yearly_spend` are required JSON keys on every `SubscriptionBudgetRecord`. Each value is either a number or `null`; the response must not use `omitempty`. A number means the complete comparable amount is known, while `null` means the amount cannot be used as a complete total. No separate completeness field and no zero-valued substitute are introduced.
- Budget comparison uses the query's `settings.base_currency` and the unit of `MonthlyPriceBase` (`CostRow.BaseCurrency`). It never uses the source billing currency (`CostRow.Currency`) and does not add a second exchange conversion. An enabled budget whose `base_currency` differs from the query base currency is `unknown` with both spend fields `null`, even when it has no matching rows, has zero spend, or happens to match a source billing currency.
- For an enabled budget with the same currency as the query, only matching current rows are considered. Any matching row whose `BaseCurrency` differs from the query base currency makes the entire budget non-comparable; do not select a subset based on `Currency` or use a partial sum to prove a risk. Missing exchange rate keeps the row amount `null`; an expired but present rate remains numeric and carries its stale marker.
- A comparable budget is complete when every matching current row has a non-null `MonthlyPriceBase`; no matching rows are a complete zero. Sum non-null monthly base amounts and derive yearly spend as monthly spend multiplied by 12. If the rows are incomplete, both public spend fields are `null`; only an `over` result proven by the known sum reaching the existing threshold is retained, otherwise the budget is `unknown`. Complete amounts remain numeric even when the status is `unknown` because an effective limit is zero; existing `EvaluateBudgetStatus` rules remain authoritative, including `>=` thresholds, monthly-limit precedence, and yearly-limit conversion.
- Disabled budgets retain `status=disabled` and numeric zero for both spend fields regardless of rows or currency. They are not candidates for row-level status merging. Budget derivation in create/edit responses, listing, statistics, and global base-currency changes never rewrites stored budget currency/limits or billing facts. Explicit budget edits still persist the requested facts; derived amounts and statuses always use the current query currency.

- 月度概览的 `budget_risks` 使用当前月 `total_monthly_cost` 的已知小计，并以 `current_unknown_amount_count == 0` 作为金额完整条件。当前缺有效账单资产的 VPS，或当前订阅缺换算金额（例如汇率缺失）的行，都会使当前月金额不完整；归档对象的缺口只计入 `archived_unknown_amount_count`，不改变当前完整性。
- 月度概览金额不完整时，不输出 `warning`；只有已知小计已达到现有限额并证明 `over` 时仍输出风险记录，并将 `current_monthly_spend` 与 `current_yearly_spend` 序列化为 `null`。不完整金额的 `unknown` 不进入 `budget_risks`；`budget_risk_count` 始终等于该列表长度，因此为 0 只表示没有确定的 `warning`/`over`，不表示金额完整。`current_unknown_amount_count` 仍表达当前缺口。

The individual budget status table is:

| Status | Condition |
| --- | --- |
| `disabled` | The budget is disabled; it has zero derived spend and is excluded from row-level matching. |
| `ok` | The enabled budget is comparable and complete, and the complete sum is below the warning threshold. Same-currency zero spend with a positive limit is `ok`. |
| `warning` | The enabled budget is comparable and complete, and the sum reaches the warning threshold without reaching the over threshold. |
| `over` | The enabled budget is comparable and the sum reaches the over threshold. An incomplete budget may retain `over` only when its known sum already proves the threshold; its public spend fields remain `null`. |
| `unknown` | The enabled budget has a currency/unit mismatch, incomplete amounts without a proven over threshold, a zero effective limit, or another effective-limit condition for which the existing evaluator returns unknown. Mismatched or incomplete amounts are `null`; a complete known amount may remain numeric. |

Row-level `budget_status` is merged after all matching work: archived rows and rows with `MonthlyPriceBase == null` remain `unknown`; otherwise only matching enabled budgets participate. With no matching enabled budget the row is `unknown`; with one or more matches, combine statuses by fixed severity `over > warning > unknown > ok`, independent of budget order. The status of each budget record remains independent, so a row-level `warning` or `over` only proves that at least one matching budget has that risk and does not claim that every matching budget is complete.
- 月度预算继承取 `budget_month <= bucket_start` 的最近历史月配置，不取未来月份，也不将“无当月行”误判为没有预算；统计与 evidence adapter 必须沿用同一继承规则。
- `next_reminder_at` is the next future pending reminder window calculated from settings and existing delivery rows. It must not report an already-delivered or past reminder as pending.
- Reminder dedupe is keyed by `subscription_id + renew_at + offset_days` independent of notification channel. The worker must reserve the dedupe row before dispatching notifications, then update delivery status after dispatch. This prevents duplicate sends on repeated scans.
- Ordinary renewal reminders skip cancelled/expired subscriptions and archived VPS. A cancel renewal intention with near-term billing risk remains actionable; archived potential charges use follow-up review rather than ordinary renewal reminders.
- Dashboard may show only high-signal subscription summary: total base cost, future renewal count, budget risk, and exchange anomaly. Full filtering, budget CRUD, settings, and refresh actions stay in `/subscriptions`.

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Unknown or invalid base currency | settings write returns invalid input; stored value remains unchanged |
| Fixer key omitted in PUT | existing key is preserved |
| Fixer key set to empty string in PUT | existing key is cleared |
| Provider error contains `access_key` / `api_key` / `token` | response/status stores redacted error only |
| Exchange rate missing for non-base currency | derived base costs are `null`, `exchange_rate_status=missing` |
| Exchange cache older than stale threshold | numeric cached amount remains, `exchange_rate_status=stale` |
| Budget has both monthly/yearly limits omitted or null | budget validation rejects missing effective limit |
| Budget PATCH omits a limit field | existing limit remains unchanged |
| Budget PATCH sends a limit as `null` | that limit is cleared, subject to at least one remaining limit |
| Reminder scan runs twice for same subscription/renewal/offset | dispatcher is called once; later scans return without sending |
| Dispatcher returns no channels | delivery row is updated to suppressed and audit can record suppressed status |
| Dashboard overview cannot read subscription overview | Dashboard UI may degrade; subscriptions workbench remains source of truth |

### 5. Good/Base/Bad Cases

- Good: active USD subscription has a fresh USD->CNY rate, global budget applies, overview reports CNY monthly/yearly cost and budget status.
- Good: Fixer 返回包含完整 URL 或 key 的错误时，状态只公开脱敏失败摘要，不公开请求 URL。
- Good: reminder worker inserts a `dispatch` delivery row, sends Telegram/Feishu through the shared dispatcher, then updates sent/failed/suppressed status and notification audit.
- Base: CNY subscription uses exchange rate `1` and `exchange_rate_status=identity`.
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
- Optional/non-null fields: `billing_period_unit: string`、`billing_period_length: number`、`renewal_mode: string`、`display_name: string`（订阅名称，normalize 时裁剪空白，空串表示不命名；与其他业务字段一样进入幂等 digest，同一 key 换名称返回 409 `idempotency_key_reused`）。
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
| body 包含 collection-only 或未知字段 | `400 invalid json`，保持 strict decode；`display_name` 属于 scoped DTO，不再是 collection-only |
| 同一 `Idempotency-Key` 仅 `display_name` 不同 | `409 idempotency_key_reused`，不新增行 |
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
