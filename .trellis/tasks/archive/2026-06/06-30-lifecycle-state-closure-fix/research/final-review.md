# Lifecycle State Closure Fix Final Review

## Scope

本轮复审覆盖上一轮报告中的 VPS 状态三元组、订阅 `renewal_mode` 导入、监控行政恢复、归档页面 scope、浏览器 mock fixture、DB 约束和相关 Trellis spec。复审时额外检查了受控取消/退役路径是否会绕过新不变量。

## Findings Status

### Closed: 普通 VPS PATCH 可写出矛盾三元组

- `PatchVPSAsset` 对非历史 patch 现在先读取当前行，应用 patch preview，再调用 `vpsassets.ValidateVPSStateCombination`。
- `patchVPSAssetWithHistoryAndOptionalSubscriptionLinkage` 在事务锁行后、update 前执行同样的合成状态校验。
- 回归测试覆盖：
  - `active/in_use/keep + renewal_decision=replaced` 被拒绝且不会 update / commit。
  - `cancelled/idle/cancel + usage_status=in_use` 被拒绝且不会 update。
- 新增 `0049_vps_asset_state_combination_constraint.sql` 作为 DB 级兜底，阻止绕过 store 的硬失败组合。

### Closed During Re-review: 受控取消路径会被新 DB 约束拦截

复审中发现 `ApplyVPSCancellation` 直接调用底层 `patchVPSAssetRow`，如果当前 VPS 是 `active/in_use/keep` 且目标是 `cancelled/cancel`，旧逻辑不会修改 `usage_status`，会形成 `cancelled/in_use/cancel` 并被新 DB 约束拦截。

修复：

- 受控取消到 `cancelled` 时，如果当前 `usage_status=in_use`，同步 patch 为 `usage_status=idle`。
- 受控生命周期 action 也复用合成状态校验。
- 生命周期 step 审计 now records `usage_status` before/after，避免用户回看时看不到实际变更。

### Closed: JSON 导入不能表达 `subscription.renewal_mode=gift`

- `importing.SubscriptionInput` 新增 `RenewalMode`。
- `prepareInputRecord` 传递到 `subscriptions.CreateInput.RenewalMode`。
- dry-run subscription candidate 新增 `renewal_mode`，让导入预览可见“赠送/抽奖/手动”等语义。
- 回归测试覆盖 decode `gift`、dry-run 保留 `gift`、非法 `renewal_mode` validation error、import create input 归一为 `gift false/false`。
- `docs/operations/asset-ledger-local-sample.json` 包含 `lottery` 和 `gift`，legacy flags 均为 `false,false`。

### Closed: Low-risk closure

- archived MonitoringInstance 行政恢复有直接测试，确认恢复事件文案为归档收敛且无通知记录/发送。
- `ArchivePage.test.tsx` 测试描述改为 historical scope，与实际 API query 一致。
- `scripts/visual_evidence.py` asset workflow fixture 覆盖 `renewal_mode=lottery` 和 `renewal_mode=gift` 的可见订阅行。

## Destructive Change

新增 DB check constraint `vps_assets_state_combination_valid` 是破坏性数据完整性收口：如果现有数据库里已有 `cancelled/in_use/*`、`to_cancel` 非取消决策、`to_migrate` 非迁移决策、`replaced + active/in_use` 等矛盾行，迁移会失败。当前项目没有用户，此 fail-fast 行为符合本轮目标；不做自动 backfill，避免悄悄改写语义不明的数据。

## Remaining Risk

- `asset_scope=archived` 仍保留为兼容别名，当前已由 spec 明确为 legacy alias，未发现运行时歧义。
- 旧迁移 `0031` 仍不包含 `gift`，由后续 `0048` 放开约束，符合 append-only migration 规则。
- Browser sanity 仍报告若干 `overflow-risk` warning，但命令整体 PASS，且未出现空页面、页面级水平溢出失败或状态标签缺失。本轮不把这些既有风险扩展为版式重构。

## Verification

- `go test ./internal/center/vpsassets ./internal/center/store ./internal/center/store/migrate ./internal/center/importing ./cmd/houfeng-import-vps-json ./internal/center/incidents ./internal/center/http/handlers ./internal/center/subscriptions` -> pass.
- `cd web && npm run test -- --run src/lib/api.test.ts src/lib/assetOptions.test.ts src/pages/ArchivePage.test.tsx` -> 3 files / 51 tests pass.
- `python3 -m json.tool docs/operations/asset-ledger-local-sample.json` -> pass.
- `python3 -m py_compile scripts/visual_evidence.py` -> pass.
- `make verify-go` -> pass.
- `make verify-web` -> pass, including `npm ci`, ESLint, 71 Vitest files / 544 tests, and `tsc -b && vite build`.
- `git diff --check` -> pass.
- Browser sanity asset workflow:
  - Command: `TMPDIR="$PWD/.tmp/playwright" uv run --with playwright python scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --mock-api asset-workflows --route /asset-decisions --route /vps --route /subscriptions --route /archive --viewport 1440x1000 --viewport 390x900 --timeout-ms 30000`
  - Result: pass for all routes/viewports.
- Browser sanity observability support:
  - Command: `TMPDIR="$PWD/.tmp/playwright" uv run --with playwright python scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --mock-api observability-support --route /monitoring --route /targets --route /events --viewport 1440x1000 --viewport 390x900 --timeout-ms 30000`
  - Result: pass for all routes/viewports.
