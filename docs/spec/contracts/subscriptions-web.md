# 订阅 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

- `CreateVPSSubscriptionInput` 必须是 VPS-scoped billing-fact DTO 的显式类型，不能写成 `Omit<CreateSubscriptionInput, 'vps_id' | 'status'>`。字段集合以 `internal/center/http/handlers/vps_subscription_create_fields.json` 为共享语义清单（`name` / `type` / `required` / `nullable`）；Go 用 struct json tag + 类型/指针性，TS 用类型字段、`?` 与 `| null` 解析，两边都必须与清单一致。`createSubscription(input, idempotencyKey)` 与 `createVPSSubscription(..., idempotencyKey)` 都要发送 `Idempotency-Key`；网络错误后复用原 key，只有表单内容变化或 `idempotency_key_reused` 才轮换。

#### Scenario: Caller-owned subscription create idempotency

##### 1. Scope / Trigger

- Trigger: 修改 `createSubscription` / `createVPSSubscription`、collection/VPS 订阅草稿、订阅提交/重试状态、VPS-scoped DTO 或 `ApiError.code` 解码。

##### 2. Signatures

- Collection API client: `createSubscription(input, idempotencyKey): Promise<SubscriptionRecord>`。
- API client: `createVPSSubscription(vpsId, input, idempotencyKey): Promise<SubscriptionRecord>`。
- Header: `Idempotency-Key` 由 page/controller 生成并显式传给 API client。
- 请求、错误码和回执生命周期由 [订阅协议](subscriptions.md) 唯一维护；本节只约束调用方草稿、错误解码与 UI 重试。

##### 3. Contracts

- page/controller 持有 key；API helper 不得为每次调用隐式生成 key。
- 同一草稿的 transport failure、timeout 或丢失 201 重试必须复用原 key。
- 草稿发生业务语义变化时必须轮换 key；409 `idempotency_key_reused` 后也必须轮换 key，避免同一 key 永久冲突。
- 是否轮换只能依据草稿变化或 allowlisted `ApiError.code`，不得匹配英文 `message`。
- `SubscriptionsPage`、Overview 与 Legacy VPS detail 三个调用方必须使用相同 key 生命周期合同。
- `CreateVPSSubscriptionInput` 只能包含 VPS-scoped billing-fact 字段；共享清单、Go struct json tag、TypeScript 类型字段三方必须完全一致。

##### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| 首次提交成功 201 | 关闭表单并刷新对应订阅/Overview 数据 |
| 相同草稿 transport failure 后重试 | 复用完全相同的 `Idempotency-Key` |
| 已提交 201 但响应丢失，重试收到 200 | 当作成功原记录，不重复创建 |
| 409 `idempotency_key_reused` | 展示错误并轮换 key；下一次显式提交使用新 key |
| 其他 400/409/500 | 展示错误；不得仅因 status 或 message 轮换 key |
| 用户修改草稿 | 为新的业务输入生成新 key |

##### 5. Good/Base/Bad Cases

- Good: `fetch` 抛 transport error，用户不改表单直接重试，两次请求 header 完全相同。
- Base: 用户修改周期或续费模式后提交，新草稿使用新 key。
- Bad: `createVPSSubscription` 内部每次调用 `crypto.randomUUID()`，导致丢失响应重试重复写入。
- Bad: 通过 `error.message.includes('idempotency')` 决定轮换，文案变化或本地化后行为漂移。

##### 6. Tests Required

- API unit: helper 原样发送调用方 key，并提交完整 `buildSubscriptionInput` 周期字段。
- Collection page Vitest: transport failure 后复用 key；草稿变化或 allowlisted reused-key 409 后轮换 key。
- Overview Vitest: reused-key 409 后 key 改变；transport failure 后 key 保持不变。
- Legacy Vitest: 与 Overview 相同的 key 生命周期断言。
- Contract tests: `vps_subscription_create_fields.json`、Go `vpsSubscriptionCreateRequest` json tags、TS `CreateVPSSubscriptionInput` 字段集合完全一致；只改一端必须失败。
- PostgreSQL integration 由后端 spec 负责证明 replay 只有一行；前端测试不能替代该证据。

##### 7. Wrong vs Correct

```ts
// 错误：helper 每次调用都换 key，网络重试失去幂等性。
return requestJSON(path, jsonBodyInit('POST', input, {
  'Idempotency-Key': crypto.randomUUID(),
}))
```

```ts
// 正确：调用方按草稿生命周期持有 key，helper 只负责发送。
await createVPSSubscription(vpsId, input, subscriptionIdempotencyKeyRef.current)
```

### Subscription Cost Workbench 数据流

订阅成本工作台是 VPS-first 成本中枢的主操作入口。它可以聚合订阅、预算、汇率、续费提醒和 VPS 证据，但不能替代 VPS 业务状态机。

#### 1. Scope / Trigger

- Trigger: 修改 `SubscriptionsPage.tsx`、`VPSDetailPage.tsx` 成本卡、`AssetDecisionsPage.tsx` 成本信号、`DashboardPage.tsx` 订阅摘要、`web/src/lib/api.ts` 订阅成本 API，或 `web/src/lib/types.ts` 订阅成本类型。

#### 2. Signatures

- Frontend APIs: `getSubscriptionOverview()`、`getSubscriptionStatistics(window)`、`getSubscriptionSettings()`、`updateSubscriptionSettings(input)`、`refreshSubscriptionExchangeRates()`、`listSubscriptionBudgets(filter?)`、`createSubscriptionBudget(input)`、`patchSubscriptionBudget(input)`、`listSubscriptions(filter?)`。
- Frontend types mirror center JSON snake_case: `monthly_price_base`、`yearly_price_base`、`base_currency`、`exchange_rate`、`exchange_rate_date`、`exchange_rate_stale`、`budget_status`、`next_reminder_at`。
- Routes: `/subscriptions` 是完整工作台；`/vps/:id` 只展示单台成本卡；`/asset-decisions` 只展示成本信号；Dashboard 只展示高信号摘要。

#### 3. Contracts

- `/subscriptions` 初始加载 overview、statistics、budgets、settings、subscriptions、VPS 列表。局部请求失败必须显示局部错误，不得把失败当成真实空态。
- 订阅列表的 derived cost 字段只读展示。创建/编辑订阅仍只提交账单事实，不提交 `monthly_price_base`、`budget_status`、`exchange_rate_stale` 或 `next_reminder_at`。
- Settings 表单不显示 Fixer key 明文；空 key 输入在 UI 中默认表示不修改，显式清除需要单独确认或后续专用动作。
- 预算 UI 可先支持创建和总览；如果新增编辑/禁用交互，必须使用 PATCH，并保持 omitted 和 `null` limit 语义。
- VPS 成本卡只展示当前订阅证据：原币种价格、base 月/年成本、续费日、预算状态、提醒状态、汇率状态。订阅读取失败时显示未知/错误，不得标成真实缺订阅。
- Asset Decisions 成本信号只能作为 VPS 决策证据：临近续费、超预算、缺订阅、取消/迁移但仍可能续费。主操作仍回到 VPS 详情或 VPS 决策 drawer。
- Dashboard 只显示总成本、未来续费、预算风险、汇率异常等高信号入口；不得加入预算编辑、汇率设置、订阅明细表或完整图表。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| overview failed, subscriptions loaded | 工作台显示 overview 局部错误，明细仍可操作 |
| settings failed | 汇率与提醒设置区显示错误，订阅明细不被清空 |
| exchange refresh failed | 显示失败结果或错误，不泄露 provider secret |
| budget list empty | 显示可创建预算的空态，不标记所有订阅超预算 |
| subscription has stale exchange | 行内和摘要显示汇率异常，成本字段可为 stale/null |
| VPS scoped subscriptions failed | VPS 成本卡显示读取失败，不显示真实缺订阅 |
| Dashboard subscription summary failed | Dashboard 降级为摘要不可用，完整操作仍在 `/subscriptions` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 `/subscriptions` 查看 CNY 总月成本、续费队列、供应商拆分、预算风险，并刷新汇率。
- Good: VPS 详情成本卡显示 USD 原价、CNY 月/年成本、下一次续费和预算状态，并链接回订阅工作台。
- Good: Asset Decisions 在取消/迁移队列里显示“仍有临近续费风险”，但保存决策仍调用 VPS 决策 API。
- Base: CNY 订阅显示汇率 `1` 且不提示 stale。
- Bad: 前端把 `budget_status='over'` 自动改写 VPS `renewal_decision='cancel'`。
- Bad: Dashboard 放入完整预算 CRUD 或 Fixer key 配置表单。
- Bad: 页面直接 `fetch('/api/subscriptions/overview')`；必须走 `web/src/lib/api.ts`。

#### 6. Tests Required

- `web/src/lib/api.test.ts`: 新订阅成本 API 路径、方法、body 和 query。
- `SubscriptionsPage.test.tsx`: 工作台主加载、空态、多币种、预算风险、汇率异常、设置保存、刷新汇率、创建预算。
- `VPSDetailPage.test.tsx`: scoped 订阅成本卡、缺订阅、读取失败。
- `AssetDecisionsPage.test.tsx`: 临近续费、超预算、缺订阅、取消/迁移续费风险信号。
- `DashboardPage.test.tsx`: 高信号订阅摘要存在，且不展开完整订阅工作台。

#### 7. Wrong vs Correct

```tsx
// 错误：把成本风险直接升级成 VPS 业务决策。
if (subscription.budget_status === 'over') {
  await updateVPSAsset(subscription.vps_id, { renewal_decision: 'cancel' })
}
```

```tsx
// 正确：只展示成本信号，让用户在 VPS 决策入口确认。
<Link to={`/vps/${subscription.vps_id}`}>查看成本风险</Link>
```

```tsx
// 错误：组件内绕过统一 API client。
await fetch('/api/subscriptions/overview')
```

```tsx
// 正确：通过 `lib/api.ts` 保持 credentials、错误处理和类型统一。
const overview = await getSubscriptionOverview()
```

Subscription management keeps a compact cost summary above mutually exclusive 明细 and 成本洞察 views on `/subscriptions`. Portfolio-wide insights is the default (an explicit `view=insights` also works); `view=details` opens the filtered billing list. Scoped VPS/provider/search/decision links and the center-produced VPS subscription relation explicitly select details rather than depending on the default. View activation pushes history; filter changes replace it while preserving unrelated query fields and return context. Keep each view’s scroll position and loaded evidence when switching. Explicit details-only visits defer annual statistics until insights is requested; writes invalidate statistics without activating an unvisited source, including writes that finish after a view switch. Insights-to-VPS drill-down opens filtered details and moves focus there. List, VPS catalog, overview, and annual statistics retain independent loading/error states and source-only retries; one failed source must not erase unrelated evidence or turn missing data into zero cost. Filter changes never present an earlier filter’s records as current. Page-local create/edit dialogs retain native constraints, fixed actions, and pending-submission dismissal guards. These remain billing facts, not a second VPS business-state source.

Keep subscription summary facts attached to the page identity and the view switch reachable above a long list. Details alone contains the shared FilterBar controls and removable chips; the VPS catalog uses the shared searchable single-select with full accessible values, visible keyboard selection, and local name/identity/provider/location/IP search. The wide list remains a named keyboard-focusable local scroller. Insights uses the available desktop height for a full-width trend, an equal-height monthly cost/composition pair, and a full-width renewal queue. Pie/ranking and composition-dimension changes must not resize the middle row or move the renewal panel; overflow lists remain locally scrollable and keyboard reachable. Preserve readable minimum sizes and natural main scrolling on short or narrow screens rather than clipping chart contents. The monthly chart remains a stable height when switching mobile presentations. Cost and budget trends are piecewise-linear solid/dashed lines, with red/green difference fills split at true intersections. Fit the vertical scale to known costs and budgets with padding so positive clustered values do not flatten against an unnecessary zero baseline; include real zero values and keep flat series nondegenerate. Missing budgets leave gaps, not zero-budget comparisons. Keep accurate monetary axis ticks and exact in-flow pointer/touch/keyboard readouts; viewport growth must not shrink SVG labels or push the legend beyond its panel. Forms retain compact related-field grids, legible controls, complete fields, and natural mobile scrolling. Visual acceptance covers populated charts, all breakdown dimensions, overflow queues, pie/ranking stability, view/history/filter interactions, and complete desktop/mobile dialogs; request tests or empty charts alone are not design acceptance.


用户可见 `active` 使用“生效中”，保留存储枚举不变。
