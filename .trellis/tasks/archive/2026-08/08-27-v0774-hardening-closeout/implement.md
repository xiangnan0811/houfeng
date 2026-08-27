# Implement: v0.77.4 remaining hardening seams

## Order

1. **P2-01 subscription collection idempotency**
   - Extract shared create-with-idempotency handling in `subscriptions.go`.
   - Collection POST requires key + `CreateSubscriptionIdempotent`.
   - Handler tests: missing key, replay 201 then 200, reused key 409; keep existing create test by adding a key.
   - `createSubscription(input, idempotencyKey)` sends the header.
   - `SubscriptionsPage` stores a UUID, reuses it on retry, rotates on form payload change / `idempotency_key_reused`.
   - Page test: POST fails (network), retry sends the same `Idempotency-Key` and only one create is accepted.

2. **P2-02 Legacy CAS recovery**
   - Extract `VPSVersionConflictBanner` and decision compare/label helpers.
   - Legacy facts/decision: conflict state, load latest, three-way merge, new ETag, block save until loaded.
   - Tests: facts 409 → load latest → preserve draft → success; decision 409 → load latest → preserve reason → success.
   - Add generation ownership for route/unmount/Drawer invalidation, a synchronous load-latest lock, and keyed `vps_id + request token` PATCH locks.
   - Race tests: late A responses cannot affect B; close/reopen while PATCH is pending cannot duplicate writes; A/B double-deferred PATCHes unlock only their own VPS.

3. **P2-03 disabled IP Quality best-effort history**
   - `LoadIPQuality`: if disabled, return ready/not_configured even when summary query errors.
   - Repo test: `disabled + summary error`.
   - Service test: overall healthy, no `source.unavailable.v1`, IP section not unavailable.

4. **P3-01 readonly code + archive routing**
   - `writeCodedError` on both handler branches.
   - Handler tests assert `code=vps_asset_readonly`.
   - `isVPSAssetReadonly` + Chinese copy; Overview and Legacy GET identity and route to `/archive/:id` when terminal.

5. **P3-02 CAS compare labels + already satisfied**
   - Format compare rows with `VPS_RENEWAL_DECISION_LABELS`.
   - `loadLatestVersion`: if local decision equals latest, show “该决策已由其他操作完成”, refresh, close panel.
   - Overview tests for labels and already-satisfied.

6. **P3-03 VPS-scoped TS DTO**
   - Replace `Omit<...>` with an explicit type matching Go json tags.
   - Go + TS contract tests on the frozen field list.

## Validation

```bash
go test ./internal/center/http/handlers -run 'TestSubscriptionsCollection|TestVPSSubscriptionsCreate|TestVPSItemReject'
go test ./internal/center/store -run 'TestLoadIPQuality|TestOverviewServiceWires|TestCreateSubscriptionIdempotent'
go test ./internal/center/vpsoverview -run 'Test.*IPQuality|Test.*Unavailable'
cd web && npx vitest run src/lib/api.test.ts src/lib/types.ts src/pages/SubscriptionsPage.test.tsx src/pages/vps-detail/vpsManagementHelpers.test.ts src/pages/vps-detail/vpsDetailHelpers.test.ts src/pages/vps-detail/VPSOverviewManagementActions.test.tsx src/pages/vps-detail/LegacyVPSDetail.test.tsx
make fmt-go
# before finish:
./scripts/verify.sh
```

PostgreSQL integration `TestCreateSubscriptionIdempotentReplayAfterLostResponseKeepsOneRow` already covers the receipt table. Handler + page tests cover the previously open door.

## Review gates

- Collection and VPS-scoped create share one idempotency helper.
- Legacy must not only change error text; it must load latest and retry.
- Legacy async mutations must prove generation invalidation and per-VPS request-token ownership under deferred responses.
- Disabled IP Quality summary errors must not enter `JudgementSourcesUnavailable`.
- `CreateVPSSubscriptionInput` must not compile extra fields.

## Rollback

Feature-branch revert. No schema change.
