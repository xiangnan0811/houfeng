# Lifecycle State Closure Final Review

## Findings

### High: 普通 VPS PATCH 可写出 `replaced + active/in_use` 的矛盾当前事实

- Evidence:
  - `web/src/pages/vps-detail/VPSRenewalDecisionForm.tsx` 使用 `RENEWAL_DECISION_OPTIONS`，该选项来自 `VPS_RENEWAL_DECISION_LABELS`，包含 `replaced`。
  - `web/src/pages/VPSDetailPage.tsx` 与 `web/src/pages/AssetDecisionsPage.tsx` 保存续费决策时只提交 `{renewal_decision, renewal_reason?}`。
  - `internal/center/vpsassets/types.go` 的 `ValidateVPSPatchStateCombination` 只校验同一请求内的字段组合；如果 PATCH 只提交 `renewal_decision=replaced`，不会把当前数据库中的 `lifecycle_status=active` 或 `usage_status=in_use` 合并检查。
  - `internal/center/store/vps_assets.go` 在 `patchVPSAssetWithHistoryAndOptionalSubscriptionLinkage` 中虽然 `select ... for update` 读取当前 VPS，但没有用 `current + input` 合成后调用 `ValidateVPSStateCombination`。
  - `db/migrations/0017_add_vps_assets.sql` 只有单列枚举 check constraint，没有跨列状态矩阵约束。
- Impact:
  - 用户可以在 VPS 详情页或资产决策页把仍为 `active/in_use` 的 VPS 标记为 `replaced`。
  - 这会破坏上一轮定义的硬约束：`renewal_decision=replaced` 不应与 `lifecycle_status=active` 或 `usage_status=in_use` 共存。
  - 下游 Asset Decisions readback 会把 `replaced` 当作迁移链路已体现，但 VPS 仍可能承载业务，造成“状态看起来完成，事实仍未收口”的体验歧义。
- Solution:
  1. 在 store/handler 写边界新增合成校验：读取 current 后应用 patch preview，调用 `ValidateVPSStateCombination(merged.LifecycleStatus, merged.UsageStatus, merged.RenewalDecision)`。
  2. 普通 PATCH 继续禁止写 `to_cancel/to_migrate/cancelled/archived` lifecycle；但只改续费决策时，也必须校验最终三元组。
  3. 给 `PatchVPSAsset` 和 `PatchVPSAssetWithSubscriptionRenewalLinkage` 增加回归测试：`active/in_use + renewal_decision=replaced` 返回 `ErrInvalidVPSAssetInput`；若同时提交 `usage_status=standby` 或 lifecycle 非 active 的合法组合，再按产品决策决定是否允许。
  4. 可选 DB 兜底：新增迁移加跨列 check constraint，防止 future bypass。当前项目无用户，可以接受对现存不合法 fixture/数据先清理再加约束。
- Destructive note:
  - 加 DB constraint 是破坏性收口：任何已有矛盾数据会阻塞迁移。当前无用户，建议优先做，因为它能保护脚本、未来仓库方法和人工 SQL 以外的写入口。

### Medium: JSON 导入链路不能表达 `subscription.renewal_mode=gift`

- Evidence:
  - `internal/center/importing/types.go` 的 `SubscriptionInput` 没有 `RenewalMode` 字段。
  - `internal/center/importing/importing.go` 构造 `subscriptions.CreateInput` 时没有传 `RenewalMode`。
  - `internal/center/importing/importing.go` 的 `DecodeRecords` 启用了 `decoder.DisallowUnknownFields()`，所以导入 JSON 中出现 `subscription.renewal_mode` 会被拒绝。
  - 核心订阅 create/patch、DB constraint、price history validation、前端类型和选项已支持 `gift`，但导入是独立用户入口。
- Impact:
  - 批量导入赠送订阅时，用户不能显式写 `gift`；只能通过 legacy `auto_renew=false/auto_renew_cancelled=false` 落成 `manual`。
  - 这会让“赠送”和“手动续费”在导入来源中再次混淆，影响成本/订阅历史的事实可信度。
- Solution:
  1. 在 `importing.SubscriptionInput` 增加 `RenewalMode string json:"renewal_mode"`。
  2. `prepareInputRecord` 传入 `subscriptions.CreateInput.RenewalMode`。
  3. 增加导入 dry-run / decode 测试：`renewal_mode=gift` 被接受并在 report candidate 中显示；非法 renewal mode 返回 validation error；未知字段仍被拒绝。
  4. 更新 `docs/operations/asset-ledger-local-sample.json` 至少包含一条 `gift` 或 `lottery` 示例，帮助用户理解“赠送”和“抽奖”不是 auto-renew flag。
- Destructive note:
  - 非破坏性新增字段。若同时把 sample 中旧 auto-renew-only 表达改成显式 `renewal_mode`，只是文档/示例变化。

### Low: 覆盖和命名仍有少量收尾缺口

- `internal/center/incidents/service.go` 处理 `MonitoringInstance.ArchivedAt != nil` 的行政恢复，但 `service_test.go` 只直接覆盖 paused / maintenance / retired MonitoringInstance，没有 archived MonitoringInstance case。
- `web/src/pages/ArchivePage.test.tsx` 测试名仍写 “through archived scope”，断言实际已经是 `asset_scope=historical`。
- `scripts/visual_evidence.py` 的 asset workflow fixture 尚未覆盖 `renewal_mode=gift` 或 `lottery`，浏览器 sanity 因此不能证明“赠送/抽奖”标签的实际页面效果，只能由 Vitest 覆盖共享 label helper。

## Confirmed Closed Items

- VPS create/import now rejects workflow/terminal lifecycle through `ValidateCreateInput`; import dry-run/import reuses that validation.
- `asset_scope=historical` is accepted by VPS and subscriptions domain, handlers, stores, frontend API/types, and Archive page. Store SQL maps both `historical` and compatibility `archived` to `cancelled + archived`.
- `renewal_mode=gift` is supported by core subscription create/patch validation, legacy flag normalization, DB constraints for `subscriptions` and `price_histories`, frontend type/options/labels, and API tests.
- Frontend no longer labels `lottery` as “抽奖/赠送”; shared options map `lottery -> 抽奖` and `gift -> 赠送`.
- Incident service now administratively recovers active incidents for inactive MonitoringInstance/Target states and suppresses notification sends in those administrative recovery paths.
- Migration wording in production implementation uses “标记迁移意向并人工跟进”; remaining “推进迁移” matches tests/user-provided free-form goal text, not system-generated execution plan copy.

## Compatibility And Destructive Cleanup Options

- `asset_scope=archived` compatibility alias:
  - Current state is semantically documented and harmless at runtime.
  - Because the project currently has no users, a destructive cleanup is possible: remove `archived` from `AssetScope`, handler tests, frontend type, and specs, leaving only `historical`.
  - Recommendation: keep for now unless API minimalism is prioritized. The alias is explicitly documented and does not create conflicting behavior.
- Old migration `0031` lacks `gift`; new migration `0048` relaxes constraints:
  - This follows the project rule not to edit merged migrations.
  - Because no users exist, squashing migrations before a public release is possible, but it is a broad destructive history rewrite. Recommendation: defer unless the maintainer intentionally performs a pre-release migration squash.
- Legacy `lottery` data is not auto-backfilled to `gift`:
  - Correct as-is. Without source evidence, changing `lottery` rows to `gift` would rewrite facts.
  - If the maintainer knows all existing lottery rows are actually gifts and no user data matters, a one-off destructive migration could be written, but it must be explicit.

## Verification

- `go test ./internal/center/vpsassets ./internal/center/subscriptions ./internal/center/store ./internal/center/incidents ./internal/center/http/handlers ./internal/center/importing ./cmd/houfeng-import-vps-json` -> pass.
- `cd web && npm run test -- --run src/lib/api.test.ts src/lib/assetOptions.test.ts src/pages/ArchivePage.test.tsx` -> 3 files / 51 tests pass.
- `make verify-go` -> pass.
- `make verify-web` -> `npm ci`, `eslint .`, 71 Vitest files / 544 tests, and `tsc -b && vite build` pass.
- `git diff --check` -> pass.
- Browser sanity with `uv run --with playwright` and mock `asset-workflows`:
  - Routes: `/asset-decisions`, `/vps`, `/subscriptions`, `/archive`.
  - Viewports: `1440x1000`, `390x900`.
  - Result: pass; warnings are overflow-risk diagnostics only, no blank page or page-level horizontal overflow failure.
- Browser sanity with mock `observability-support`:
  - Routes: `/monitoring`, `/targets`, `/events`.
  - Viewports: `1440x1000`, `390x900`.
  - Result: pass; warnings are overflow-risk diagnostics only.

## Conclusion

上一轮修复关闭了多数状态语义问题，但当前不能称为“完美修复”。仍有两个真实链路问题需要后续实现：

1. VPS PATCH 必须校验合成后的生命周期 / 用途 / 续费决策三元组，尤其是阻止 `replaced + active/in_use`。
2. JSON 导入链路必须支持 `subscription.renewal_mode`，否则 `gift` 不算全入口闭环。

本轮按用户要求只审查并给出方案，没有直接实施业务修复。
