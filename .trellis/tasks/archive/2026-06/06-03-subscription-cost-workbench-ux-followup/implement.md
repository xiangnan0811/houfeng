# 订阅成本中枢二次 UX 修订 Implement

## Checklist

1. Backend data contract
   - Add migration for `subscription_monthly_budgets`.
   - Extend repository/service types with monthly budget buckets and new breakdown dimensions.
   - Add store methods for monthly budget list/upsert and budget bucket lookup with carry-forward semantics.
   - Add service tests for budget carry-forward, missing budget, currency mismatch/data-insufficient, and payment/region breakdown.
   - Add handler/router tests for monthly budget API and updated statistics payload.

2. Frontend API/types
   - Add `SubscriptionMonthlyBudgetRecord`, input types, API helpers.
   - Extend `SubscriptionStatistics`/`SubscriptionSeriesPoint`.
   - Remove new UI dependence on row-level budget status.

3. Settings subscription tab
   - Replace scoped budget rule editor with global monthly budget timeline.
   - Keep cost base/exchange/reminder settings intact.
   - Test settings tab load/save/upsert/cancel/error behavior.

4. Subscriptions page UX
   - Replace top metric grid with a dedicated four-column metric grid.
   - Rebuild `SubscriptionInsights` as four compact workbench columns.
   - Add month cost chart/list toggle and hover/focus tooltip.
   - Add payment method and country/region breakdown tabs.
   - Add annual budget/cost dual-line chart with differential fill.
   - Move renewal queue into fourth compact column.
   - Update subscriptions table columns and row edit behavior.
   - Keep create/edit deep-link behavior.

5. CSS and accessibility
   - Edit only `web/src/index.css` / existing global style files.
   - Ensure chart SVGs have `role="img"`/aria labels and keyboard focus paths.
   - Ensure four cards/columns do not overflow or overlap at 1440/1080/390.

6. Verification
   - `git diff --check`
   - `go test ./internal/center/store ./internal/center/subscriptioncosts ./internal/center/http`
   - `cd web && npm run lint`
   - `cd web && npm run test -- --run SubscriptionsPage SettingsPage MetricChart Sparkline`
   - `cd web && npm run build`
   - Browser sanity for `/subscriptions` and `/settings?tab=subscriptions` at 1440x1000, 1080x900, 390x900.
   - `make verify-go && make verify-web`

## Review Gates

- Do not start implementation until `task.py start` marks this task in progress.
- After backend contract changes, run targeted Go tests before deep frontend edits.
- After layout changes, inspect actual browser screenshots before finalizing.
- Before PR, run full verification and record any known non-blocking local warnings.

## Rollback Points

- If monthly budget migration proves too broad, keep migration and API minimal: list/upsert only, no delete.
- If four columns are unreadable at 1080px, retain four columns at 1440px and two columns at 1080px; acceptance only requires no overflow and professional readability.
- If old scoped budget API is used elsewhere, leave it untouched and hide it only from the new settings UI.

## Verification Results

- `git diff --check` passed.
- `go test ./internal/center/subscriptioncosts ./internal/center/http/handlers ./internal/center/http ./internal/center/store ./cmd/houfeng-center` passed.
- `make verify-go` passed.
- `cd web && npm run lint && npm run test -- --run && npm run build` passed.
- `make verify-web` passed; local Node v24.14.1 emitted an npm engine warning for the project's Node 22.x range, but install/test/build completed successfully.
- `python3 scripts/test_visual_evidence.py` passed.
- Browser sanity passed for `/subscriptions` and `/settings?tab=subscriptions` at `1440x1000`, `1080x900`, and `390x900`.
