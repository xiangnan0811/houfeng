# Subscription insights UX polish

## Goal

Improve the subscription cost workbench so the cost insight area feels usable in realistic operator workflows, not merely feature-complete. The work should reduce cramped panels, remove awkward always-visible empty hover detail UI, make breakdown switching compact, and let first-time users seed monthly budgets across useful historical ranges without configuring one month at a time.

## Requirements

- `/subscriptions` cost insights must use a 2x2 desktop layout instead of 1x4:
  - First row: `月成本` and `年度趋势与风险`.
  - Second row: `成本构成` and `续费队列`.
  - Responsive behavior must remain readable on 1080px and 390px without page-level horizontal overflow.
- The `月成本` donut hover detail must behave like contextual tooltip/popover UI:
  - No permanent empty detail card below the chart.
  - No placeholder copy when nothing is hovered/focused.
  - On hover/focus, show the relevant VPS name, original price, base-currency monthly cost, and monthly share near the chart interaction area.
  - Keyboard focus must still expose the same information for accessibility.
- The `成本构成` dimension switcher must become a compact select control in the panel header/right side.
  - Supported dimensions remain service provider, category, currency, payment method, and country/region.
  - Labels must not wrap awkwardly in desktop layout.
- `/settings?tab=subscriptions` monthly budget creation must support optional bulk historical coverage:
  - Options: all available historical months, recent year, current year.
  - Default: no bulk coverage selected.
  - Bulk coverage should create/update multiple month budget records using the chosen monthly limit, warning percentage, note, and current base currency.
  - The feature is primarily for first-time setup and must not require one month-by-month manual operation.
- Maintain existing subscription creation/editing flows and existing settings separation.
- Do not introduce a charting library or state-management library.
- Keep user-visible copy concise and grounded in the actual data contract.

## Acceptance Criteria

- [x] At 1440px, `.subscription-insights__grid` renders two columns and two rows with the required visual order.
- [x] At 390px, `/subscriptions` has no page-level horizontal overflow; table scrolling remains scoped to its panel.
- [x] Month-cost donut renders without a permanent empty hover-detail panel; hover/focus on a segment displays detail and blur/mouseleave hides it.
- [x] Cost composition switching uses a select in the panel header and supports all five dimensions without label wrapping.
- [x] Monthly budget form defaults to single-month mode and can bulk-save all history, recent year, or current year ranges.
- [x] Bulk monthly budget requests are backed by a clear API/service contract, not repeated ad hoc client-only behavior that silently diverges from backend validation.
- [x] Existing settings tabs continue to save system settings without touching subscription settings.
- [x] Existing `/subscriptions?vps_id=<id>&create=1` behavior remains intact.
- [x] Relevant Go and web tests cover layout semantics, tooltip behavior, select switching, and monthly budget bulk range payloads.
- [x] `make verify-go`, `cd web && npm run lint`, relevant Vitest tests, `cd web && npm run build`, and browser sanity for `/subscriptions` and `/settings?tab=subscriptions` pass or any blocker is explicitly documented.

## Verification

- `go test ./internal/center/subscriptioncosts ./internal/center/store ./internal/center/http/...`
- `cd web && npm run lint`
- `cd web && npm run test -- --run api SubscriptionsPage SettingsPage`
- `cd web && npm run build`
- `./scripts/verify.sh`
- Node Playwright visual smoke at 1440x1000, 1080x900, 390x900 for `/subscriptions` and `/settings?tab=subscriptions`; screenshots saved under `research/screenshots/`.

## Notes

- User feedback pattern: design decisions must be tested from actual operator use. A visible control being technically functional is not enough if it is cramped, awkward, or increases first-time setup burden.
