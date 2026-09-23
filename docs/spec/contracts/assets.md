# 资产合同

## VPS 资产状态组合不变量

- `vps_assets.lifecycle_status`、`usage_status`、`renewal_decision` 是一个组合状态，不是三个互不相关的枚举。所有写路径必须验证最终组合：`cancelled` 必须使用取消类续费决策且不能 `in_use`；`to_cancel` 必须使用取消类续费决策；`to_migrate` 必须使用 `migrate`；`replaced` 不能仍是 `active` 或 `in_use`。
- PATCH 入口不能只校验请求体内出现的字段。仓库写入边界必须读取当前行，应用 patch preview 后调用 `vpsassets.ValidateVPSStateCombination`，再执行 `update vps_assets`；受控生命周期 action 若直接调用底层 update helper，也必须先做同样的合成状态校验。
- DB 必须有跨列 check constraint 作为最后兜底。新增或调整这类约束是破坏性数据完整性收口：迁移必须先用幂等 backfill 处理可确定归一化的历史组合，再添加 validated constraint；无法安全推导的脏数据才应 fail fast。不得用 `not valid` 静默放过。
- 如果某个已发布迁移在记录到 `schema_migrations` 前已经会因历史数据违反新约束而失败，可以按例外修正该失败迁移本身；修正必须把 backfill 放在 `add constraint` 前，并增加 `migrate_test.go` 断言 backfill 语句存在且顺序早于约束。
- JSON 导入的 `subscription` 对象必须同步订阅创建合同。`subscription.renewal_mode` 是合法字段，支持 `auto|manual|auto_cancelled|lottery|gift|bonus|other`；`gift` 和 `lottery` 归一后 legacy `auto_renew` / `auto_renew_cancelled` 必须为 `false,false`。`DecodeRecords` 继续 `DisallowUnknownFields`，新增可导入字段时必须同时改 DTO、dry-run report、create input 传递和测试。

## Asset Ledger providers

`db/migrations/0016_create_asset_ledger.sql` 是 post-V1 Asset Ledger 的 schema 入口，当前落 `providers` 服务商主数据表：

- `providers.provider_id` 使用 `ids.New("pv")` 生成，字段和 JSON contract 保持英文稳定值。
- `name` 必须通过数据库 `providers_name_not_blank` 约束保证 trim 后非空；领域层也必须在 create / patch 时校验。
- `rating` 是 nullable `integer`，只允许 `null` 或 `1..5`，数据库约束为 `providers_rating_range`。
- `labels` 使用 `text[] not null default '{}'`；领域层负责 trim 和过滤空标签。
- provider CRUD 不得自动改写、规范化或 backfill `monitoring_instances.provider`。`monitoring_instances.provider` 仍是 Fleet Observability 的监控实例元数据字符串，Asset Ledger provider 是独立资产层主数据。

## Asset Ledger VPS assets

`db/migrations/0017_add_vps_assets.sql` 添加 `vps_assets`，代表资产层 VPS 账本。它依赖 `providers.provider_id`，但仍与 Fleet Observability 的 `monitoring_instances.provider` 字符串保持分离。

- `vps_assets.vps_id` 使用 `ids.New("vps")` 生成。
- `provider_id` 可为 `null`；存在时必须引用 `providers(provider_id)`，并在 provider 删除时 `on delete set null`。
- `provider_name` 是导入 / 展示兼容字符串，不能创建、更新或回填 `providers`。
- `display_name` 必须由数据库 `vps_assets_display_name_not_blank` 约束保证 trim 后非空；领域层 create / patch 也必须校验。
- `lifecycle_status`、`usage_status`、`renewal_decision` 使用稳定英文机器值，并分别由数据库 check 约束和领域校验共同保护。
- VPS 列表查询支持 `AssetScope`：未显式传入时 handler 默认 `current`，排除 `lifecycle_status in ('cancelled','archived')`；`historical` 返回这两个历史不可访问状态；`archived` 是保留给旧客户端的兼容别名，语义与 `historical` 完全相同；`all` 不按生命周期裁剪。显式 `lifecycle_status` 精确筛选优先于 scope，避免旧状态筛选与归档入口互相冲突。
- VPS 是业务状态主体：人工生命周期、用途、续费 / 迁移 / 取消决策只写在 `vps_assets`。Subscription 和 MonitoringInstance 只能提供账单事实与运行观测事实，不得在普通创建 / 编辑流程里要求用户重复选择业务状态。
- VPS create/import 只能创建当前事实：`lifecycle_status` 允许 `active`、`idle`、`testing`；`to_migrate`、`to_cancel`、`cancelled`、`archived` 必须来自 lifecycle action、archive API 或底层 store fixture。不得创建缺少 lifecycle action 审计的历史/流程态资产。
- `ssh_port` 默认为 `22`，数据库约束为 `1..65535`；领域 create 中 `0` 表示省略并默认，patch 中显式 `0` 必须拒绝。
- `archived_at` 是派生字段：生命周期切到 `archived` 时补时间，从 `archived` 切出时清空；API 输入不得任意写入 `archived_at`。
- VPS 资产 CRUD 不得改写 `monitoring_instances.provider`，也不得改变 MonitoringInstance / Target / Agent 的既有语义。
- 普通 VPS CRUD 只维护 VPS 自身账本；跨订阅、MonitoringInstance、Target 的取消 / 退役协调必须通过 `assetlifecycle` 显式 preview + confirm + audit action 完成。
- subscription summary 属于 subscriptions 查询；active monitoring instance link count / monitoring instance summary 由 `assetlinks.Repository` 在 HTTP 展示层补充，不得让 `store/vps_assets.go` 直接耦合 MonitoringInstance 表或 link 表细节。

### Scenario: VPS lifecycle / usage / renewal matrix

#### 1. Scope / Trigger

- Trigger: 修改 `internal/center/vpsassets/types.go`、`PATCH /api/vps/{vps_id}`、VPS create/import、archive/lifecycle action、或任何会写 `vps_assets.lifecycle_status`、`usage_status`、`renewal_decision` 的路径。
- 目标：防止页面和决策模型读到互相矛盾的 VPS 当前事实，例如“已取消但仍在用”或“迁移流程态但续费决策是取消”。

#### 2. Signatures

- Domain helpers: `ValidateCreateInput(input CreateInput) error`、`ValidateOrdinaryPatchInput(input PatchInput) error`、`ValidateVPSStateCombination(lifecycle, usage, renewal) error`、`ValidateVPSPatchStateCombination(input PatchInput) error`。
- Machine values:
  - `lifecycle_status`: `active|idle|testing|to_migrate|to_cancel|cancelled|archived`
  - `usage_status`: `in_use|idle|standby|testing|unknown`
  - `renewal_decision`: `unreviewed|keep|observe|migrate|cancel|auto_renew_cancelled|replaced`

#### 3. Contracts

- `ValidateCreateInput` must reject `to_migrate`、`to_cancel`、`cancelled`、`archived`; create/import is not an audit-less lifecycle action path.
- Ordinary PATCH remains current-fact only for lifecycle: `active|idle|testing`。流程态/终态只能由 lifecycle action/archive API 或 store-level historical fixtures 写入。
- Full combination hard failures:
  - `cancelled` requires `renewal_decision in (cancel, auto_renew_cancelled)`。
  - `cancelled` cannot pair with `usage_status=in_use`。
  - `to_cancel` requires `renewal_decision in (cancel, auto_renew_cancelled)`。
  - `to_migrate` requires `renewal_decision=migrate`。
  - `renewal_decision=replaced` cannot pair with `lifecycle_status=active` or `usage_status=in_use`。
- Patch delta validation only rejects contradictions among fields present in the same request. It must not infer omitted current values, because archive/lifecycle action paths may call lower-level store helpers after doing their own review.
- Warning-only or transitional readback combinations can remain visible for historical explanation, but new create/import and ordinary PATCH must fail closed for the hard failures above.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| create `lifecycle_status=cancelled` / `archived` / `to_cancel` / `to_migrate` | 400 invalid VPS asset input |
| create `lifecycle_status=active, usage_status=in_use, renewal_decision=keep` | allowed |
| full combination `cancelled + keep` | invalid VPS asset input |
| full combination `cancelled + in_use` | invalid VPS asset input |
| full combination `to_migrate + cancel` | invalid VPS asset input |
| full combination `replaced + active/in_use` | invalid VPS asset input |
| ordinary PATCH `lifecycle_status=to_cancel` | invalid VPS asset input |
| PATCH delta `usage_status=in_use, renewal_decision=replaced` | invalid VPS asset input |

#### 5. Good/Base/Bad Cases

- Good: 新导入 VPS 默认 `active/unknown/unreviewed` 或用户明确填 `idle/idle/observe`，后续再通过决策或 lifecycle action 改状态。
- Base: 旧历史资产在 archive 视图读到 `cancelled/idle/cancel`，作为历史 readback 展示。
- Bad: 导入 JSON 直接写 `cancelled/in_use/keep`，用户在列表看到“已取消但仍在用且保留”的矛盾资产。
- Bad: 普通 PATCH 把 VPS 改成 `to_migrate`，但没有 lifecycle action step 或迁移 workbench 审计。

#### 6. Tests Required

- Domain tests: create lifecycle boundary、full combination hard failures、allowed coherent states、PATCH delta hard failures。
- Handler/store tests: ordinary API create/patch maps invalid matrix to invalid input; archive/lifecycle paths keep their dedicated tests.
- Import tests: dry-run/import reuse `vpsassets.NormalizeCreateInput` + `ValidateCreateInput` and reject workflow/terminal lifecycle creation.

#### 7. Wrong vs Correct

```go
// 错误：create 只检查枚举合法，让流程态直接落库。
if !IsValidLifecycleStatus(input.LifecycleStatus) {
	return ErrInvalidVPSAssetInput
}
```

```go
// 正确：create 先限制当前事实边界，再检查跨字段组合。
if !IsValidCreateLifecycleStatus(input.LifecycleStatus) {
	return ErrInvalidVPSAssetInput
}
if err := ValidateVPSStateCombination(input.LifecycleStatus, input.UsageStatus, input.RenewalDecision); err != nil {
	return err
}
```

## Asset lifecycle actions

`assetlifecycle` 是唯一允许跨 Subscription、VPS、MonitoringInstance、Target/实例做取消或退役联动的领域服务。它不是普通 CRUD 的旁路，而是一个显式的 lifecycle action 工作流：先预览影响范围，再由用户确认要执行的步骤，最后以审计记录落库。

- 后端 API：
  - `GET /api/vps/{vps_id}/cancellation-preview` 从 VPS 出发返回 VPS 当前生命周期、所有关联订阅候选（包括 active、expired、cancelled、paused、unknown/latest）、活跃 `vps_monitoring_instance_links`、通过 asset service / domain 关联的 Target、推荐步骤、风险提示和阻塞项。
  - `POST /api/vps/{vps_id}/cancellation` 接受用户显式选择的 `subscription_ids`、`vps_lifecycle_status`、`monitoring_instance_actions`、`target_actions`、`reason`、`effective_date`，在一个事务内写入状态变化与审计步骤。
  - `GET /api/asset-context/targets` 是 Target 批量上下文接口，供 Target 列表 / 详情显示关联 VPS 的取消 / 过期 / 不一致状态，避免前端逐行请求。Monitoring 列表不再暴露批量 asset-context 接口；Monitoring 详情使用 `/api/monitoring-instances/{id}/vps` 返回所属 VPS。
- 审计表：`asset_lifecycle_actions` 保存一次操作的发起对象、确认时间、原因、执行摘要和最终状态；`asset_lifecycle_action_steps` 保存每个 subscription / VPS / MonitoringInstance / Target 步骤的前后状态、状态码、错误和摘要。
- 普通 CRUD 不得静默调用 lifecycle action；只有工作台或等价的显式确认入口可以调用 `POST /api/vps/{vps_id}/cancellation`。
- 如果 VPS 没有 active subscription，但存在 expired/cancelled/paused/unknown subscription，preview 和旧续费联动提示必须说明“订阅账单记录已无续费动作，仍需处理 VPS、MonitoringInstance 与入口探测状态”，不得误导为“没有关联订阅，需要创建订阅”。
- 默认语义：已过期且不续费的 VPS 写 `renewal_decision=cancel`、`lifecycle_status=cancelled`；未来到期但已决定不续费的 VPS 写 `renewal_decision=cancel`、`lifecycle_status=to_cancel`；未来取消但仍观察的 MonitoringInstance 用 `lifecycle_status='不续费'` 且监控保持启用；实际退役 MonitoringInstance 用 `lifecycle_status='已退役'` 并可按确认步骤暂停监控；随 VPS 下线的 Target/实例确认后用 `run_status='已归档'`，临时停用才用 `暂停`。
- `vps_monitoring_instance_links` 默认保留为历史证据；取消 / 退役 action 不自动 unlink，除非未来新增单独的“解除错误关联”显式动作。
- 执行事务必须先锁定 VPS，再写 action 与各步骤；任何一步失败时业务状态与步骤写入整体回滚，避免部分取消造成新割裂。失败审计是例外：必须先显式回滚业务事务，再用独立事务写入 `status='failed'` 的 action 和 failed step，避免失败记录随业务回滚消失，也避免复用同一 `action_id` 时被未回滚事务锁住。
- preview 的 blocker 必须在 POST 执行路径重新校验；例如 `lifecycle_status='archived'` 的 VPS 不允许通过 cancellation POST 改回 cancelled/to_cancel，handler 应返回冲突而不是清空 `archived_at`。
- VPS 归档 / 恢复必须走受控 archive API：`GET /api/vps/{vps_id}/archive-review` 返回 VPS、订阅、MonitoringInstance、服务、域名、Target、warnings/blockers/eligible；`POST /api/vps/{vps_id}/archive` 在事务中锁定 VPS、重新计算 review、校验 `confirmation_name` 与 blockers 后才写 `lifecycle_status='archived'`；`POST /api/vps/{vps_id}/restore-from-archive` 只允许 `archived -> idle`。普通 `PATCH /api/vps/{vps_id}` 不得写入 `archived`，也不得从 `archived` 恢复。
- archive blockers 至少包括：任一关联订阅仍为 `active`；任一关联 MonitoringInstance lifecycle 非 `不续费` / `已退役` 或 monitoring status 非 `暂停`；任一关联 Target 非 `暂停` / `已归档`。这些 blockers 必须在 archive POST 内重新计算，前端 review 只能作为提示，不能作为权限来源。
- Dashboard asset summary 只返回聚合计数；成本只统计 active subscriptions，取消待处理 / 已取消 VPS、状态割裂 VPS、仍运行的关联 MonitoringInstance/Target 进入告警计数。

### Scenario: VPS renewal decision links subscription auto-renew

#### 1. Scope / Trigger

- Trigger: 修改 `PATCH /api/vps/{vps_id}`、`internal/center/store/vps_assets.go` 的 history transaction path、`subscriptions` 自动续费字段，或前端续费决策保存 flow。

#### 2. Signatures

- Backend API: `PATCH /api/vps/{vps_id}` with body containing `renewal_decision` and optional `renewal_reason`.
- Response: VPS record fields plus optional `renewal_subscription_linkage` object when a cancellation-class decision path was evaluated.
- Linkage response fields: `status`, `candidate_count`, optional `subscription_id`, `updated`, `message`.
- Store method: `PatchVPSAssetWithSubscriptionRenewalLinkage(ctx, vpsID, input) (vpsassets.Record, vpsassets.RenewalSubscriptionLinkage, error)`.
- DB writes: `vps_assets`, `renewal_decisions`, optional one `subscriptions` row, optional one `price_histories` row, all in one transaction.

#### 3. Contracts

- Cancellation-class decisions are currently `cancel` and `auto_renew_cancelled` only; `migrate`, `observe`, `keep`, `replaced`, and `unreviewed` must not modify subscriptions.
- The transaction must `select ... for update` the VPS row before patching and must lock active subscription candidates before deciding whether to write.
- Exactly one `subscriptions.status = 'active'` row for the VPS is the only unambiguous write case.
- In the write case, final subscription state must be `auto_renew=false` and `auto_renew_cancelled=true`.
- The linkage write must reuse the existing subscription patch/history semantics: when automatic-renewal fields change, insert `price_histories` in the same transaction.
- The response `message` is user-facing Chinese copy; frontend may display it directly but must not infer extra writes from it.
- This path must not create/update `vps_monitoring_instance_links`, Provider, MonitoringInstance, Target, ProbeItem, Agent plans, runtime controls, Dashboard summary rows, or import state.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| VPS not found | Return existing `vps asset not found` behavior; no subscription write |
| invalid VPS patch input | Return invalid VPS input; no subscription write |
| renewal decision unchanged | Do not insert renewal history and do not evaluate subscription linkage |
| cancellation-class decision with 0 active subscriptions | Save VPS decision/history, return `status=no_active_subscription`, no subscription write |
| cancellation-class decision with >1 active subscriptions | Save VPS decision/history, return `status=multiple_active_subscriptions`, no subscription write |
| exactly 1 active subscription already cancelled | Save VPS decision/history, return `status=subscription_already_cancelled`, do not add no-op price history |
| exactly 1 active subscription needing cancellation | Save VPS decision/history, update subscription, insert price history, return `status=subscription_updated` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情把 `renewal_decision` 从 `keep` 改为 `cancel`，该 VPS 只有一条 active 订阅；响应包含 `subscription_updated`，VPS timeline 有 renewal decision，subscription timeline 有 auto-renew price history。
- Base: 用户把 `renewal_decision` 改为 `migrate`；只更新 VPS 决策和 history，不返回联动写入结果。
- Bad: 因为某 VPS 有两条 active 订阅而批量把两条都取消自动续费。
- Bad: 从 subscription PATCH 反向把 VPS renewal decision 改成 `auto_renew_cancelled`。

#### 6. Tests Required

- Store tests: exactly-one active subscription update, no active subscription, multiple active subscriptions, already-cancelled subscription, non-cancellation decision no write, unchanged decision no history/no write.
- Handler tests: cancellation-class PATCH returns `renewal_subscription_linkage`; ordinary PATCH still returns the plain VPS record contract used by existing clients.
- Frontend tests: decision save displays linkage message/action for `no_active_subscription` and keeps normal decision-save notice for non-linkage decisions.

#### 7. Wrong vs Correct

```go
// 错误：取消类决策后单独再 patch subscription，两个事务可能漂移。
record, _ := repo.PatchVPSAsset(ctx, vpsID, input)
_, _ = subscriptionRepo.PatchSubscription(ctx, subID, subscriptions.PatchInput{AutoRenewCancelled: subscriptions.PatchBool(true)})
```

```go
// 正确：VPS 当前状态、renewal history、subscription 当前状态和 price history 同事务完成。
record, linkage, err := repo.PatchVPSAssetWithSubscriptionRenewalLinkage(ctx, vpsID, input)
```

```go
// 错误：跨观测边界自动改 MonitoringInstance 运行态。
_, _ = tx.Exec(ctx, `update monitoring_instances set lifecycle_status = '不续费' where monitoring_instance_id = $1`, monitoringInstanceID)
```

```go
// 正确：只返回 linkage status，让 UI 引导用户显式处理 MonitoringInstance/Target/Agent 相关动作。
return vpsassets.RenewalSubscriptionLinkage{Status: vpsassets.RenewalSubscriptionLinkageMultipleActiveSubscription}
```

## Asset Ledger VPS MonitoringInstance links

`db/migrations/0019_create_vps_node_links.sql` 添加历史 link 表，`0029_rename_nodes_to_monitoring_instances.sql` 将其迁移为 `vps_monitoring_instance_links`，用于连接资产层 VPS 与 Fleet Observability 的 `monitoring_instances`。它是关联历史表，不是 MonitoringInstance 状态机的一部分。

- `vps_monitoring_instance_links.link_id` 使用 `ids.New("vnl")` 生成，避免用 `(vps_id, monitoring_instance_id, linked_at)` 做 API identity。
- `vps_id` 必须引用 `vps_assets(vps_id)`，`monitoring_instance_id` 必须引用 `monitoring_instances(monitoring_instance_id)`；删除 VPS 或 MonitoringInstance 时可以级联清理 link 历史。
- active link 定义为 `unlinked_at is null`。`idx_vps_monitoring_instance_links_pair_active` 必须保证同一 `(vps_id, monitoring_instance_id)` 同时最多一条 active link。
- unlink 必须写 `unlinked_at`，不得物理删除；如果提供 note，只更新 link note，不改 MonitoringInstance 或 VPS 业务字段。
- link / unlink 不得改写 `monitoring_instances.provider`、MonitoringInstance `lifecycle_status`、`monitoring_status`、`current_health_status`、Target、Agent 或 subscription。
- VPS item/list API 可以补 `active_monitoring_instance_link_count`，VPS detail 可以返回 active MonitoringInstance 摘要；这些摘要通过 `internal/center/assetlinks.Repository` 查询，不要把 MonitoringInstance 查询 SQL 塞进 `store/vps_assets.go`。
- MonitoringInstance 侧 VPS 摘要使用独立 `/api/monitoring-instances/{monitoring_instance_id}/vps` 查询，不把资产字段混入基础 `monitoringinstances.Record`。

## VPS-scoped MonitoringInstance creation

`POST /api/vps/{vps_id}/monitoring-instances` 是普通 agent 接入的主合同。它从 VPS 创建 MonitoringInstance 并在同一个事务内写入 active `vps_monitoring_instance_links`，避免先创建孤立监控实例再回 VPS 关联。

- 请求只允许少量覆盖字段：`display_name`、`group`、`region`、`city`、`provider`、`labels`、`note`、`link_note`。缺省值必须从 VPS 的 display name、provider、region/city/datacenter/country、labels、note 派生。
- 创建出的 MonitoringInstance 默认是运行观测附属事实：`lifecycle_status='待接入'`，binding / health / heartbeat 等仍由 onboarding 和 agent sync 推进。
- 如果 VPS 不存在、MonitoringInstance insert 失败或 link 失败，整个事务必须回滚，不留下孤立 MonitoringInstance。
- 该路径不得修改 VPS lifecycle / usage / renewal decision，也不得修改 Subscription、Target、ProbeItem 或 Agent plan；它只创建观测对象和 VPS 关联证据。

### Scenario: Idempotent VPS-scoped detail creates

#### 1. Scope / Trigger

- Trigger: 修改 VPS scoped experience/service/domain/monitoring create handler、对应 repository、`internal/center/createidempotency`、`0062_create_vps_create_idempotency.sql` 或 APP ACL current fragment。Collection `POST /api/services|domains|monitoring-instances` 不在此合同内，必须保持原签名和状态语义。

#### 2. Signatures

- HTTP: `POST /api/vps/{vps_id}/experience-logs|services|domains|monitoring-instances`，必须恰有一个合法 `Idempotency-Key`。
- Store signatures:

  ```go
  CreateExperienceLogIdempotent(context.Context, renewals.CreateExperienceLogInput, string) (renewals.ExperienceLogRecord, bool, error)
  CreateAssetServiceIdempotent(context.Context, assetservices.CreateInput, string) (assetservices.Record, bool, error)
  CreateAssetDomainIdempotent(context.Context, assetdomains.CreateInput, string) (assetdomains.Record, bool, error)
  CreateLinkedMonitoringInstanceIdempotent(context.Context, pathVPSID string, wire monitoringinstances.LinkedCreateWireIdentity, key string) (monitoringinstances.Record, assetlinks.Record, bool, error)
  ```

  `bool` 为 `replayed`；monitoring 同时返回原 instance 与 link，且 handler 不在调用前读取 VPS defaults。
- DB: `experience_log_create_idempotency`、`asset_service_create_idempotency`、`asset_domain_create_idempotency`、`vps_monitoring_instance_create_idempotency`。每表以 key 为 PK，保存 digest、结果 FK、`created_at`；结果删除级联 receipt，无 TTL/janitor/update 路径。

#### 3. Contracts

- shared key trim/format 为 8..128 且只允许 `[A-Za-z0-9._:-]`；缺失、重复或非法 header 为 400 `invalid_idempotency_key`，且不得调用 create repository。
- digest 必须来自 normalize 后的 path VPS scope + 实际 wire identity。Monitoring digest 不包含从可变 VPS 状态派生的 persistence defaults；相同 wire retry 即使 VPS 默认值变化也必须 replay 原 instance/link。
- 顺序统一为 normalize/validate → begin transaction → operation-namespaced advisory xact lock → receipt lookup → mismatch/replay 或 result insert → receipt insert → commit。任一 cut point 失败都 rollback/fail closed。
- first create 为 201；same key + same digest 为 200 原 ID 且无额外写；same key + different digest 为 409 `idempotency_key_reused`。HTTP 错误/日志不得包含 key、digest、body、note/details、SQL 或 wrapped internal error。
- `0062` 是 `0061` 后的 additive migration；runtime APP 对四张 receipt 表只有 `select`/`insert`，无 update/delete/sequence 权限。不得修改已发布 migration。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| body/领域输入非法 | 400，事务前拒绝；不得创建 result/receipt |
| receipt replay 指向的 result/link 缺失 | fail closed internal error；不得静默重建 |
| monitoring 的 VPS 不存在或已有 active link | 404/409 既有稳定语义；instance/link/receipt 均不落单 |
| committed response unknown 后同 key/body retry | 200 原 ID；每类 result/receipt 各一行，monitoring link 也只有一行 |

#### 5. Good / Base / Bad Cases

- Good: monitoring 首次提交只给实际 wire 字段，repository 在 receipt miss 后于同一事务锁定 VPS、派生 defaults 并原子创建 instance + link + receipt；随后 VPS defaults 改变，同 key/wire 仍返回原双 ID。
- Base: 新 key + 合法新 body 创建新记录并返回 201；collection create 继续走旧签名，不要求 `Idempotency-Key`。
- Bad: handler 先读取 VPS defaults 再查 receipt；后续 VPS lookup 失败会让已提交请求的 replay 错误返回 404/500。
- Bad: 把派生 VPS defaults 放进 digest，或把 monitoring instance/link/receipt 拆到不同事务，都会破坏 lost-response replay。

#### 6. Tests Required

- Handler tests 覆盖四类 missing/duplicate/invalid、201/200/409 与 zero/one repository create count。
- Store unit 覆盖 begin/lock/lookup/replay scan/result insert/receipt insert/commit rollback cut points；PostgreSQL lost-response integration 覆盖四类真实表和 monitoring 双 ID。
- Migration/ACL tests 校验 successor 顺序、四表 FK/check/index、migration registry 与 current APP exact privileges；严格 PostgreSQL runner 缺 fixture/DSN 时是 blocker，不得 skip-as-pass。
- Monitoring regression 必须让 receipt replay 路径上的 VPS/default lookup 成为 unexpected，并在真实 PostgreSQL 中修改首次派生所依赖的 defaults 后证明原 ID、原持久化值及单行 materialization 不变。
- 测试失败输出不得打印 body、key、digest、note/details、SQL、DSN、record 或 raw error；AST/privacy contract 应覆盖本场景新增 handler/store/PG tests。

#### 7. Wrong vs Correct

```go
// 错误：handler 先读可变 VPS，再把派生 persistence 交给幂等仓储。
vps, err := vpsRepo.GetVPSAsset(ctx, pathVPSID)
record, link, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, pathVPSID, derive(vps, wire), key)
```

```go
// 正确：handler 只传 path + normalized wire；repository 先查 receipt，miss 才在事务内锁 VPS 并派生。
record, link, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(ctx, pathVPSID, wire, key)
```

## Asset Ledger timeline histories

`db/migrations/0020_create_renewal_decisions.sql` 添加 `renewal_decisions`，用于记录资产层 VPS 续费决策变化历史。它补充 `vps_assets.renewal_decision` 当前状态，不替代当前状态字段。

- `renewal_decisions.decision_id` 使用 `ids.New("rdec")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `from_decision` 允许 `null`，用于未来导入或补录；正常 VPS PATCH 自动记录时应写入变更前的决策值。
- `to_decision` 必须是 `vpsassets.RenewalDecision` 合法英文机器值，数据库 check 约束与领域校验共同保护。
- `reason` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `decided_at` 默认 `now()`，领域入口可传入 UTC 时间；timeline 按 `decided_at desc, created_at desc, decision_id desc` 排序。
- `PATCH /api/vps/{vps_id}` 只有在显式设置 `renewal_decision` 且最终值发生变化时才插入历史；只改其他字段或设置为原值不得插入历史。
- VPS 当前状态更新与 history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 VPS 行，避免当前状态和历史漂移。
- `GET /api/vps/{vps_id}/timeline` 返回真实表驱动的 `renewal_decisions[]`、`price_histories[]`、`ip_histories[]`、`spec_snapshots[]`、`experience_logs[]`，不得返回占位假数据。
- 续费决策历史本身不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target 或 Agent。唯一可同时改写 subscription 的路径是上一节定义的 `PATCH /api/vps/{vps_id}` 取消类续费决策受控联动例外；该路径必须同时保留 renewal decision history 与 subscription price history。

`db/migrations/0021_create_asset_histories.sql` 添加 `price_histories`、`ip_histories`、`vps_spec_snapshots`，用于补齐资产层价格、IP、规格变化历史。三张表补充当前状态字段，不替代 `subscriptions` 或 `vps_assets` 当前状态。

- `price_histories.price_history_id` 使用 `ids.New("ph")` 生成；`ip_histories.ip_history_id` 使用 `ids.New("iph")`；`vps_spec_snapshots.snapshot_id` 使用 `ids.New("vss")`。
- `price_histories` 必须同时引用 `subscriptions(subscription_id)` 和 `vps_assets(vps_id)`；subscription PATCH 只有在价格、币种、计费周期、计费月数、月付折算、续费日、自动续费标记或状态最终发生变化时才插入历史。
- subscription 当前状态更新与 price history insert 必须在同一个事务中完成，并先 `select ... for update` 锁定 subscription 行，避免当前订阅和历史漂移。
- `ip_histories` 必须记录 IPv4 / IPv6 前后值，且只有至少一个 IP 字段变化时才插入；数据库约束 `ip_histories_changed` 兜底拒绝无变化历史。
- `vps_spec_snapshots` 记录变化后的规格快照：`product_name`、`ssh_host`、`ssh_port`、`ssh_user`、`os_name`、`virtualization`。VPS PATCH 只有这些字段最终发生变化时才插入 snapshot。
- VPS 当前状态更新与 IP / spec history insert 必须复用 VPS history 事务路径，并先 `select ... for update` 锁定 VPS 行。
- 所有 history 仍属于 Asset Ledger，不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent 或 provider。

`db/migrations/0022_create_experience_logs.sql` 添加 `experience_logs`，用于记录单台 VPS 的人工体验、稳定性、网络、账单、服务支持、迁移或取消原因。它补充资产历史，不替代 `vps_assets.note` 或续费决策历史。

- `experience_logs.experience_log_id` 使用 `ids.New("elog")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，并在 VPS 删除时级联清理历史。
- `category` 使用稳定英文机器值：`note`、`stability`、`network`、`support`、`billing`、`migration`、`cancellation`；数据库 check 约束与领域校验共同保护。
- `severity` 使用稳定英文机器值：`info`、`warning`、`critical`；数据库 check 约束与领域校验共同保护。
- `summary` 必须 trim 后非空；`details` 是 trim 后的可空字符串语义，但数据库列必须 `not null default ''`，避免 timeline JSON 出现 null 文案。
- `occurred_at` 默认 `now()`，领域入口可传入 UTC 时间；experience log 列表按 `occurred_at desc, created_at desc, experience_log_id desc` 排序。
- `GET /api/vps/{vps_id}/experience-logs` 只返回该 VPS 的经验记录；VPS 不存在时返回 asset timeline not found 语义。
- `POST /api/vps/{vps_id}/experience-logs` 的 path `vps_id` 是唯一 VPS 来源，请求 body 不接受覆盖 `vps_id`；写入只创建 experience log，不改写 VPS 当前字段。
- experience log 不得创建 `vps_monitoring_instance_links`，不得改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target、Agent、Provider、VPS 当前状态或 subscription。

### Scenario: VPS experience logs contract

#### 1. Scope / Trigger

- Trigger: 修改 `experience_logs` schema、`/api/vps/{vps_id}/experience-logs`、`GET /api/vps/{vps_id}/timeline` 的 `experience_logs[]` 字段，或前端 VPS 详情页经验记录表单。

#### 2. Signatures

- DB table: `experience_logs(experience_log_id text primary key, vps_id text references vps_assets(vps_id) on delete cascade, category text, severity text, summary text, details text, occurred_at timestamptz, created_at timestamptz)`.
- Backend API: `GET /api/vps/{vps_id}/experience-logs` -> `[]renewals.ExperienceLogRecord`。
- Backend API: `POST /api/vps/{vps_id}/experience-logs` with JSON body `{category, severity, summary, details?, occurred_at?}` -> `renewals.ExperienceLogRecord`。
- Timeline API: `GET /api/vps/{vps_id}/timeline` includes `experience_logs: []`.
- Frontend API: `createVPSExperienceLog(vpsId, input)` and `listVPSExperienceLogs(vpsId)`.

#### 3. Contracts

- `category` and `severity` are machine values; UI maps them to Chinese labels in `web/src/lib/types.ts`.
- `occurred_at` is an RFC3339 timestamp when supplied; browser `datetime-local` values must be converted to ISO before posting.
- `summary` is the short user-facing title; `details` carries optional longer context and is never `null` in responses.
- `experience_logs[]` belongs to the VPS timeline contract; adding it requires updating Go DTOs, `web/src/lib/types.ts`, API tests, VPS detail tests, and timeline rendering together.

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| blank path `vps_id` | 400 from domain validation or 404 from route parsing |
| missing VPS foreign key | 404 `vps asset not found` at HTTP layer |
| invalid `category` / `severity` | 400 `invalid input` |
| blank `summary` | 400 `invalid input` |
| zero / invalid `occurred_at` | 400 `invalid input` / `invalid json` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页记录“晚高峰丢包”，POST 成功后刷新 timeline，并在 `experience_logs[]` 里按发生时间展示。
- Base: VPS 没有经验记录时，`experience_logs` 返回空数组，前端展示空态。
- Bad: 把 `vps_id` 放进 body 并允许覆盖 path VPS；这会制造跨资产误写风险。
- Bad: 经验记录写入后顺手修改 `vps_assets.note`、续费决策或 MonitoringInstance 状态。

#### 6. Tests Required

- Migration test: 断言表、外键、枚举 check、summary not blank、排序索引。
- Domain test: normalize / validate 覆盖 trim、UTC、invalid category/severity、blank summary。
- Store test: create/list/timeline 聚合、排序 SQL、missing VPS / foreign-key mapping。
- Handler/router/bootstrap tests: GET/POST、invalid JSON、invalid input、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API client URL/body，VPS 详情页创建经验记录后刷新 timeline，空态显示。

#### 7. Wrong vs Correct

```go
// 错误：body 里的 vps_id 覆盖 path，可能写入另一台 VPS。
input.VPSID = input.VPSID

// 正确：path 是唯一 VPS 来源。
input.VPSID = vpsID
```

```tsx
// 错误：直接提交 datetime-local 字符串，Go time.Time 不能稳定解析。
occurred_at: form.occurredAt

// 正确：提交 ISO/RFC3339 时间。
occurred_at: form.occurredAt ? new Date(form.occurredAt).toISOString() : null
```

## Asset Ledger service assets

`db/migrations/0023_create_asset_services.sql` 添加 `asset_services`，用于记录一台 VPS 上人工维护的服务资产。它是 VPS-scoped 资产备注和可选 Target 关联，不是完整服务注册中心、服务发现、域名管理或 Agent 自动采集入口。

- `asset_services.service_id` 使用 `ids.New("svc")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，命名外键为 `asset_services_vps_fk`，并在 VPS 删除时级联清理服务记录。
- `target_id` 可为 `null`，存在时必须引用 `targets(target_id)`，命名外键为 `asset_services_target_fk`，并在 Target 删除时置空；创建或列出服务不得修改 Target / ProbeItem。
- `name` 必须 trim 后非空；数据库约束为 `asset_services_name_not_blank`，领域 create 也必须校验。
- `service_type` 使用稳定英文机器值：`web`、`api`、`database`、`worker`、`proxy`、`other`；空输入默认 `other`。
- `status` 使用稳定英文机器值：`active`、`paused`、`retired`、`unknown`；空输入默认 `active`。
- `port` 可为 `null`；存在时必须在 `1..65535`。
- `labels` 使用 `text[] not null default '{}'`；领域层负责 trim、过滤空标签和去重。
- service asset 写入不得改变 VPS 当前状态、subscription、experience log、MonitoringInstance、Target、ProbeItem、Agent 或 `monitoring_instances.provider`。

### Scenario: VPS service assets contract

#### 1. Scope / Trigger

- Trigger: 修改 `asset_services` schema、`internal/center/assetservices/`、`/api/services`、`/api/vps/{vps_id}/services`，或前端 VPS 详情页服务资产区块。

#### 2. Signatures

- DB table: `asset_services(service_id text primary key, vps_id text not null, target_id text null, name text, service_type text, status text, url text, port integer null, labels text[], note text, created_at timestamptz, updated_at timestamptz)`。
- Backend API: `GET /api/services?vps_id=&target_id=&service_type=&status=` -> `[]assetservices.Record`。
- Backend API: `POST /api/services` with JSON body `{vps_id, target_id?, name, service_type?, status?, url?, port?, labels?, note?}` -> `assetservices.Record`。
- Backend API: `GET /api/vps/{vps_id}/services` -> `[]assetservices.Record`。
- Backend API: `POST /api/vps/{vps_id}/services` with JSON body `{target_id?, name, service_type?, status?, url?, port?, labels?, note?}` -> `assetservices.Record`。
- Frontend API: `listAssetServices(filter)`, `createAssetService(input)`, `listVPSServices(vpsId)`, `createVPSService(vpsId, input)`。

#### 3. Contracts

- `POST /api/vps/{vps_id}/services` 的 path `vps_id` 是唯一 VPS 来源；body 中的 `vps_id` 必须被忽略，不能覆盖 path。
- `GET /api/vps/{vps_id}/services` 必须先确认 VPS 存在；VPS 不存在时返回 not-found 语义，不把缺失 VPS 静默表现为空列表。
- `target_id` 是可选关联，只做引用校验和展示跳转；不得创建 Target、改写 Target、改 ProbeItem 或改变观测语义。
- 全局 `GET /api/services` 可以按 VPS、Target、类型和状态过滤；非法枚举必须在进入 store 查询前返回 400。
- service asset 不进入 `/api/dashboard` 的资产摘要，也不进入 `GET /api/vps/{vps_id}/timeline`；它在 VPS 详情页作为独立服务区块加载。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| missing / blank `vps_id` on collection POST | 400 `invalid input` |
| body `vps_id` conflicts with path VPS | path VPS wins; write to path VPS only |
| missing VPS on path GET/POST or collection POST FK | 404 `vps asset not found` |
| missing Target FK | 404 `target not found` |
| blank `name` | 400 `invalid input` |
| invalid `service_type` / `status` | 400 `invalid input` |
| `port < 1` or `port > 65535` | 400 `invalid input` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页为 `vps_001` 创建 `Blog` 服务，带 `target_id=tg_001`，写入后只刷新服务列表。
- Base: VPS 存在但没有服务时，path GET 返回空数组，前端展示 `尚未记录服务`。
- Bad: VPS 不存在时 path GET 返回空数组，用户会误以为资产存在但没有服务。
- Bad: 创建 service asset 时自动创建 Target 或修改 Target 探针，越过了本 MVP 的人工关联边界。

#### 6. Tests Required

- Migration test: 断言表、命名外键、枚举 check、name not blank、port range 和索引。
- Domain test: normalize / validate 覆盖空名称、枚举、labels、optional Target、port 边界和默认值。
- Store test: create、list all、list by VPS、排序 SQL、missing VPS exists check、missing Target FK、check violation 映射。
- Handler/router/bootstrap tests: collection/path GET/POST、invalid JSON、invalid input、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API helper URL/body，尤其 path-scoped create 不带 body `vps_id`；VPS 详情页服务加载、空态、创建成功和本地校验失败。

#### 7. Wrong vs Correct

```go
// 错误：让 body 覆盖 path，可能写到另一台 VPS。
input = assetservices.NormalizeCreateInput(input)

// 正确：先用 path 写入，再 normalize/validate。
input.VPSID = vpsID
input = assetservices.NormalizeCreateInput(input)
```

```tsx
// 错误：path scoped create 仍把 body.vps_id 传给后端。
postJSONBody(`/api/vps/${vpsId}/services`, input)

// 正确：path scoped create 去掉 vps_id，只传服务字段。
postJSONBody(`/api/vps/${vpsId}/services`, { name, service_type, status, target_id, url, port, labels, note })
```

`db/migrations/0024_create_asset_domains.sql` 添加 `asset_domains`，用于记录一台 VPS 关联或承载的手工维护域名资产。它是 VPS-scoped 资产记录，不是 DNS provider、注册商同步、解析记录管理或服务发现入口。

- `asset_domains.domain_id` 使用 `ids.New("dom")` 生成。
- `vps_id` 必须引用已存在的 `vps_assets(vps_id)`，命名外键为 `asset_domains_vps_fk`，并在 VPS 删除时级联清理域名记录。
- `service_id` 可为 `null`，存在时必须引用 `asset_services(service_id)`，命名外键为 `asset_domains_service_fk`，并在 Service 删除时置空。写入前仓库还必须确认该 `service_id` 属于同一个 `vps_id`，避免跨 VPS 误关联。
- `target_id` 可为 `null`，存在时必须引用 `targets(target_id)`，命名外键为 `asset_domains_target_fk`，并在 Target 删除时置空；创建或列出域名不得修改 Target / ProbeItem。
- `domain_name` 必须是归一化的小写 ASCII 域名，不含协议、路径、空白或尾随点；数据库用 `asset_domains_name_unique` 保证全局唯一，领域层负责 trim/lower/remove trailing dot 和 label 校验。
- `status` 使用稳定英文机器值：`active`、`paused`、`retired`、`unknown`；空输入默认 `active`。
- `expires_at` 是 nullable `date`，未知日期用 `null`，API 复用 subscription `Date` 的 `YYYY-MM-DD` JSON 语义。
- `auto_renew` 与 `https_enabled` 只记录人工事实，不触发续费决策、证书检查或 Target probe 修改。
- domain asset 写入不得改变 VPS 当前状态、subscription、experience log、service asset、MonitoringInstance、Target、ProbeItem、Agent 或 `monitoring_instances.provider`。

### Scenario: VPS domain assets contract

#### 1. Scope / Trigger

- Trigger: 修改 `asset_domains` schema、`internal/center/assetdomains/`、`/api/domains`、`/api/vps/{vps_id}/domains`，或前端 VPS 详情页域名资产区块。

#### 2. Signatures

- DB table: `asset_domains(domain_id text primary key, vps_id text not null, service_id text null, target_id text null, domain_name text, purpose text, status text, registrar text, expires_at date null, auto_renew boolean, https_enabled boolean, labels text[], note text, created_at timestamptz, updated_at timestamptz)`。
- Backend API: `GET /api/domains?vps_id=&service_id=&target_id=&status=` -> `[]assetdomains.Record`。
- Backend API: `POST /api/domains` with JSON body `{vps_id, service_id?, target_id?, domain_name, purpose?, status?, registrar?, expires_at?, auto_renew?, https_enabled?, labels?, note?}` -> `assetdomains.Record`。
- Backend API: `GET /api/vps/{vps_id}/domains` -> `[]assetdomains.Record`。
- Backend API: `POST /api/vps/{vps_id}/domains` with JSON body `{service_id?, target_id?, domain_name, purpose?, status?, registrar?, expires_at?, auto_renew?, https_enabled?, labels?, note?}` -> `assetdomains.Record`。
- Frontend API: `listAssetDomains(filter)`, `createAssetDomain(input)`, `listVPSDomains(vpsId)`, `createVPSDomain(vpsId, input)`。

#### 3. Contracts

- `POST /api/vps/{vps_id}/domains` 的 path `vps_id` 是唯一 VPS 来源；body 中的 `vps_id` 必须被忽略，不能覆盖 path。
- `GET /api/vps/{vps_id}/domains` 必须先确认 VPS 存在；VPS 不存在时返回 not-found 语义，不把缺失 VPS 静默表现为空列表。
- `service_id` 和 `target_id` 都是可选关联，只做引用校验和展示跳转；不得创建或修改 Service、Target、ProbeItem 或观测语义。
- `service_id` 若存在，必须属于同一 VPS；跨 VPS service 关联应返回 `asset service not found` 语义。
- 全局 `GET /api/domains` 可以按 VPS、Service、Target 和状态过滤；非法枚举必须在进入 store 查询前返回 400。
- domain asset 不进入 `/api/dashboard` 的资产摘要，也不进入 `GET /api/vps/{vps_id}/timeline`；它在 VPS 详情页作为独立域名区块加载。

#### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| missing / blank `vps_id` on collection POST | 400 `invalid input` |
| body `vps_id` conflicts with path VPS | path VPS wins; write to path VPS only |
| missing VPS on path GET/POST or collection POST FK | 404 `vps asset not found` |
| missing / cross-VPS Service FK | 404 `asset service not found` |
| missing Target FK | 404 `target not found` |
| duplicate `domain_name` | 409 `asset domain conflict` |
| invalid `domain_name` | 400 `invalid input` |
| invalid `status` | 400 `invalid input` |
| repository query failure | 500 `internal server error` |
| unsupported method | 405 `method not allowed` |

#### 5. Good/Base/Bad Cases

- Good: 用户在 VPS 详情页为 `vps_001` 创建 `api.example.com`，可选带 `service_id=svc_001` 与 `target_id=tg_001`，写入后只刷新域名列表。
- Base: VPS 存在但没有域名时，path GET 返回空数组，前端展示 `尚未记录域名`。
- Bad: VPS 不存在时 path GET 返回空数组，用户会误以为资产存在但没有域名。
- Bad: 创建 domain asset 时自动创建 DNS 记录、创建 Target、修改 Target 探针或尝试注册商同步，越过了本 MVP 的人工维护边界。

#### 6. Tests Required

- Migration test: 断言表、命名外键、唯一约束、域名归一化 check、枚举 check、date 字段和索引。
- Domain test: normalize / validate 覆盖空域名、URL/path、裸主机名、非法 label、枚举、labels、默认值。
- Store test: create、list all、list by VPS、排序 SQL、missing VPS exists check、service 同 VPS 校验、missing Service/Target FK、unique/check violation 映射。
- Handler/router/bootstrap tests: collection/path GET/POST、invalid JSON、invalid input、conflict、not found、method not allowed、subtree routing、bootstrap non-nil wiring。
- Frontend tests: API helper URL/body，尤其 path-scoped create 不带 body `vps_id`；VPS 详情页域名加载、空态、创建成功和本地校验失败。

#### 7. Wrong vs Correct

```go
// 错误：只靠 FK，允许 dom(vps_a) 关联 svc(vps_b)。
insert into asset_domains (vps_id, service_id, domain_name) values (...)

// 正确：写入前确认 service 属于同一个 VPS。
select exists (select 1 from asset_services where service_id = $1 and vps_id = $2)
```

```tsx
// 错误：VPS scoped create 仍把 body.vps_id 传给后端。
postJSONBody(`/api/vps/${vpsId}/domains`, input)

// 正确：path scoped create 去掉 vps_id，只传域名字段。
postJSONBody(`/api/vps/${vpsId}/domains`, { domain_name, service_id, target_id, status, expires_at, labels, note })
```

## Asset Ledger JSON import

`internal/center/importing/` 是真实 VPS JSON dry-run/import 的领域入口。它复用 `providers`、`vpsassets`、`subscriptions` 的 normalize / validate 规则，不维护第二套枚举、金额、日期或 provider 校验。

- dry-run 是默认路径，只解析、归一化、校验和产出报告，不写数据库。
- dry-run 必须报告 provider 创建候选、VPS 创建候选、subscription 创建候选、缺失 provider、缺失续费日期、非法字段、重复候选、MonitoringInstance 关联候选、未来 30 天续费候选和闲置但付费候选。
- 数据库可用时，dry-run 可以读取现有 providers / vps_assets / subscriptions / monitoring_instances 做重复和 MonitoringInstance 候选诊断；数据库不可用时仍应能完成纯文件模型校验。
- `-import` 必须显式开启，且在一个事务中按 provider → VPS asset → subscription 顺序写入；校验错误或重复候选存在时拒绝写入。
- import 不接受也不写 `monthly_price`，仍由 subscription 后端计算。
- import 不创建 `vps_monitoring_instance_links`，不改写 `monitoring_instances.provider`，不改变 MonitoringInstance / Target / Agent 语义。MonitoringInstance 相关输入只能作为人工确认候选进入报告。
