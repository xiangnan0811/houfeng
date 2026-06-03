# Design

## Frontend Layout

`SubscriptionInsights` remains the single composition component for the cost-insight workbench. The panel DOM order will become:

1. 月成本
2. 年度趋势与风险
3. 成本构成
4. 续费队列

CSS changes in `web/src/index.css` set `.subscription-insights__grid` to `repeat(2, minmax(0, 1fr))` on desktop, dropping to one column on narrow screens. This avoids four narrow panels while keeping the insights grouped above the subscription list.

## Month Cost Hover Detail

The donut keeps SVG segments as keyboard-focusable controls. Hover/focus state stays local to `SubscriptionInsights`, but the visual detail becomes an overlay (`subscription-donut-popover`) positioned within the donut layout. Nothing renders when there is no active segment. The overlay is `role="status"` with concise text so keyboard users receive the same information without a permanent blank box.

Clicking a concrete VPS segment still applies the VPS filter. The `其他` segment remains non-filtering and explains that it is aggregate-only when shown.

## Composition Selector

The five-dimension segmented tabs are replaced with a native select in the composition panel header. This uses the project select styling/caret contract and prevents short Chinese labels from wrapping in pill tabs. The underlying `breakdownKind` state and `breakdownItems(...)` data flow stay unchanged.

## Monthly Budget Bulk Coverage

Add a subscription-cost service operation for bulk monthly budget upsert:

- Input: `scope`, `base_currency`, `monthly_limit`, `warning_pct`, `note`.
- Scope enum:
  - `all_history`: earliest relevant subscription month through current month.
  - `recent_year`: last 12 months including current month.
  - `current_year`: January through current month of the current year.
- Output: range metadata and created/updated records.

Repository boundary:

- Add `EarliestSubscriptionMonth(ctx) (*subscriptions.Date, error)` to discover available history from subscriptions/price histories.
- Add `UpsertMonthlyBudgets(ctx, []UpsertMonthlyBudgetInput) ([]MonthlyBudgetRecord, error)` for a transaction-backed batch write.

HTTP boundary:

- New endpoint: `POST /api/subscription-monthly-budgets/bulk`.
- It returns normalized records and range metadata.
- Existing single-month `PUT /api/subscription-monthly-budgets/{YYYY-MM}` remains unchanged.

Frontend boundary:

- `SubscriptionSettingsSection` adds an optional checkbox plus select for bulk coverage in the budget creation form. Default unchecked.
- When unchecked, use existing single upsert.
- When checked, call the bulk API once.
- Successful save reloads the monthly budget timeline and shows how many months were written.

## Compatibility

Existing monthly budget list and single-month upsert contracts remain compatible. The new bulk endpoint only adds capability and does not reinterpret existing records. Old scoped `subscription_budgets` remains untouched for compatibility, but the settings UI continues to use monthly budgets only.
