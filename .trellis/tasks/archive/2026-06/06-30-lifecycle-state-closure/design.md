# Complete lifecycle state closure design

## Architecture and Boundaries

本轮按四个边界收口，不引入大平台重构：

1. **VPS 状态矩阵**：落在 `internal/center/vpsassets` 领域层，供 create/import/patch 复用。普通 PATCH 继续只允许 `active/idle/testing` lifecycle；create/import 也不得创建流程态/归档态事实，避免没有 action audit 的“已取消/待迁移”资产直接出现。
2. **Incident 非运行态收敛**：落在 `internal/center/incidents.Service`。service 已能读取 previous active incidents 并写 `IncidentMutation`；新增非运行态收敛函数，构造 recovered events，active 置空，并且不调用 notification dispatcher。
3. **订阅来源语义**：扩展现有 `renewal_mode` 枚举，新增 `gift`。这是最小兼容修复；长期“续费方式 vs 权益来源”拆表不在本轮做。
4. **Asset scope 命名兼容**：在 `vpsassets.AssetScope` 中新增 `historical`，store 中与旧 `archived` 等价。旧参数不删，前端类型同步，Archive 页面可改用 `historical` 以表达真实语义。

## Data Flow and Contracts

### VPS state matrix

- New helpers in `vpsassets`:
  - `ValidateCreateLifecycleBoundary(input CreateInput)` or folded into `ValidateCreateInput`.
  - `ValidateVPSStateCombination(lifecycle, usage, renewal)`.
- Create/import boundary:
  - allowed create lifecycle: `active`, `idle`, `testing` only.
  - flow/terminal statuses must come from lifecycle actions or archive APIs.
- Combination hard failures:
  - `cancelled` requires cancellation renewal decision (`cancel` or `auto_renew_cancelled`) and cannot be `usage=in_use`.
  - `to_cancel` requires cancellation renewal decision.
  - `to_migrate` requires `renewal_decision=migrate`.
  - `renewal_decision=replaced` cannot be paired with active/in-use current lifecycle.
- Warning-only combinations stay in readback/UI and are not blocked unless they create direct contradiction.

### Incident convergence

- Add service helper:
  - `recoverActiveIncidentsForInactiveObject(ctx, objectType, objectID, now, summaryPrefix)`.
- For MonitoringInstance periodic stale sweep:
  - if paused/maintenance/retired/archived-like record is returned, close previous active incidents for that MI.
  - no notification records are appended for this administrative recovery.
- For `AfterSuccessfulSync` monitoring evaluation:
  - if fetched monitoring instance is paused/maintenance/retired/archive, close previous active incidents and skip metric evaluation.
- For periodic target sweep:
  - `run_status in ('暂停','已归档')` closes previous target active incidents and skips probe/TLS/trend evaluation.
  - `维护中` continues to rely on maintenance-context samples for suppress/recovery when observations exist, but periodic sweep can close active probe incidents if no active runtime should be interpreted as current risk.

### Subscription gift mode

- Migration adds `gift` to `subscriptions_renewal_mode_allowed` and `price_histories_renewal_mode_allowed` constraints.
- Go constants, validation, normalization, legacy flags and tests include `gift`.
- Frontend `RenewalMode` includes `gift`; option labels:
  - `lottery`: 抽奖
  - `gift`: 赠送
  - `bonus`: Bonus/余额抵扣

### Asset scope historical alias

- `AssetScopeHistorical = "historical"`.
- `archived` and `historical` both query `lifecycle_status in ('cancelled','archived')`.
- API docs/spec/tests call out `archived` as backward-compatible alias.
- Archive page uses `historical` in new requests so browser/network evidence matches user-facing wording.

## Compatibility

- All existing API clients using `asset_scope=archived` continue to work.
- Existing subscriptions with `lottery` remain valid and now display as “抽奖”; no data migration to `gift` is attempted.
- Incident active rows are not deleted directly by status transition store methods in this design; they converge on the next service evaluation/sweep, avoiding cross-package repository coupling.
- Full migration workbench remains explicitly out of scope; existing “迁移意向” wording remains the contract.

## Risk and Rollback

- VPS state matrix may reject imports/tests that previously relied on terminal lifecycle creation. Tests and fixtures must be updated to use lifecycle action/archive paths or explicit store-level historical fixtures.
- Incident convergence changes health summaries by clearing stale active incidents for paused/archived objects. This is intended but must be covered by service tests.
- DB migration only relaxes constraints by adding `gift`; rollback is low risk because old code would reject new `gift` rows if downgraded.

## Test Strategy

- Go domain tests for VPS create/combination validation and `asset_scope=historical`.
- Go store/handler tests for historical scope SQL/query parsing and gift constraint migration text.
- Incident service tests for MonitoringInstance and Target convergence with prior active incidents.
- Frontend API/type/label/page tests for gift and historical scope.
- Browser sanity on asset and observability routes after implementation.
