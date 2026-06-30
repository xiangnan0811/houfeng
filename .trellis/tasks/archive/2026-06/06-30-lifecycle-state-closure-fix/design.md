# Lifecycle state closure fix design

## Architecture and Boundaries

本轮修复跨后端 domain/store/import、DB migration、前端测试/fixture、spec 与浏览器 sanity。边界如下：

1. **VPS state invariant**：`internal/center/vpsassets/types.go` owns pure validation; `internal/center/store/vps_assets.go` owns merged persisted-state validation before writing.
2. **DB hard guard**：新增 append-only migration，给 `vps_assets` 增加跨列 check constraint，防止绕过 domain/store 的写入形成硬失败三元组。
3. **Import contract**：`internal/center/importing` JSON DTO accepts `subscription.renewal_mode` and passes it to `subscriptions.CreateInput`; strict unknown-field behavior stays.
4. **Incident coverage**：只补 archived MonitoringInstance direct regression test，不改变 incident service behavior unless the test exposes a defect.
5. **Frontend/test fixture**：修正 stale test name; update `scripts/visual_evidence.py` asset workflow subscriptions to include `lottery` and `gift`, so browser sanity can exercise user-visible labels on `/subscriptions`.
6. **Specs**：update backend DB/spec and web state-data spec for merged-state patch validation and import renewal_mode contract.

## Data Flow

### VPS PATCH merged state

HTTP / UI -> `handlers.VPSItem` -> `vpsassets.NormalizePatchInput` / `ValidateOrdinaryPatchInput` -> `store.PatchVPSAsset*` -> read current row -> apply patch preview -> `ValidateVPSStateCombination` -> DB update -> history/linkage.

Implementation principle:

- Domain delta validation remains useful for request-local contradictions.
- Store validation is the final current-state gate because it can see persisted values.
- `patchVPSAssetRow` should not be called with an input that produces an invalid merged state.

### DB constraint

Migration adds `vps_assets_state_combination_valid`:

- `cancelled` implies cancellation renewal and not `usage_status='in_use'`.
- `to_cancel` implies cancellation renewal.
- `to_migrate` implies `renewal_decision='migrate'`.
- `renewal_decision='replaced'` implies `lifecycle_status <> 'active'` and `usage_status <> 'in_use'`.

This is destructive if existing data violates it. The project currently has no users, so fail-fast migration is acceptable.

### Import renewal mode

JSON input `subscription.renewal_mode` -> `importing.SubscriptionInput.RenewalMode` -> `subscriptions.NormalizeCreateInput` -> canonical renewal mode and legacy flags -> dry-run candidates and import writes.

Strict decoder behavior remains: unknown fields still fail decode.

## Compatibility / Destructive Notes

- Adding `subscription.renewal_mode` to import is backward-compatible.
- DB cross-column check is destructive for invalid existing rows. No data backfill is added because silently rewriting ambiguous `replaced/active/in_use` rows would hide a data quality problem.
- `asset_scope=archived` alias is not removed in this task.

## Review Gates

- TDD red/green for VPS merged state and import renewal mode before production changes.
- Targeted backend and frontend tests after each cluster.
- Full verify and browser sanity before completion.
- Final report must explicitly state whether all复审 findings are closed.
