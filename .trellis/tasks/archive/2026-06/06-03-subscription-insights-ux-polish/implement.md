# Implement

## Steps

1. Update Trellis metadata and start the task on `worktree/subscription-insights-ux-polish`.
2. Add backend bulk monthly budget contract:
   - Types, validation, service method.
   - Repository methods and PostgreSQL implementation.
   - Handler route and tests.
3. Update frontend API/types for bulk monthly budget creation.
4. Refine `SubscriptionInsights`:
   - 2x2 ordering.
   - Donut hover/focus overlay only when active.
   - Composition select in header.
5. Refine subscription settings monthly budget form:
   - Optional bulk coverage checkbox/select.
   - Single upsert remains default.
   - Success/failure copy and tests.
6. Update CSS only in `web/src/index.css`.
7. Add/update tests:
   - Go service/handler/store tests for bulk budget ranges.
   - `web/src/lib/api.test.ts`.
   - `SubscriptionsPage.test.tsx` and `SettingsPage.test.tsx`.
8. Validate:
   - `make verify-go`
   - `cd web && npm run lint`
   - `cd web && npm run test -- --run SubscriptionsPage SettingsPage api`
   - `cd web && npm run build`
   - Browser sanity for `/subscriptions` and `/settings?tab=subscriptions` at 1440, 1080, 390.

## Review Gates

- Do not accept a permanent blank hover detail region under the donut.
- Do not use tabs/pills for the five composition dimensions.
- Do not make bulk budget coverage default-on.
- Do not fake historical budget months on the frontend without backend validation.
- Do not add third-party dependencies.
