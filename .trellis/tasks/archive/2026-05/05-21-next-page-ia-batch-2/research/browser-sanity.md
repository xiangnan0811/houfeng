# Browser sanity: ProvidersPage + SubscriptionsPage IA polish

- **Date**: 2026-05-21
- **Scope**: `/providers`, `/subscriptions`
- **Evidence level**: browser sanity
- **Preview URL**: `http://127.0.0.1:5178/`
- **Data source**: local mock API fixture `asset-workflows`
- **Viewports**: `1440x1000`, `390x900`

## Result

PASS.

- `/providers` rendered the Asset Ledger hero, service-provider master-data summary, provider evidence table, create/edit Drawer framing, and empty/error boundaries without blank viewport or page-level horizontal overflow.
- `/subscriptions` rendered the renewal/cost evidence hero, VPS context, applied-filter context, subscription evidence table, create/edit Drawer framing, and prerequisite/empty/error boundaries without blank viewport or page-level horizontal overflow.
- Table-first scan path stayed visible on desktop; mobile viewport did not introduce page-level horizontal overflow beyond the intended table scroll behavior.

## Caveats

- This sanity check used local mock data, not a live center session with real Asset Ledger records.
- Default `python3` did not have local Playwright available during the implement/check runs; `/opt/homebrew/opt/python@3.11/bin/python3.11` was used successfully.
- The local preview server was stopped after the check.
