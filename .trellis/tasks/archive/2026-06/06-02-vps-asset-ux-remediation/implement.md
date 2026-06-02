# VPS asset UX remediation implementation

## Checklist

1. Create branch/worktree and enable hooks.
2. Load backend/web/Trellis guidelines.
3. Add subscription migration and Go subscription fields/validation/store scan/write tests.
4. Add validity extension lifecycle action repository/handler/API tests.
5. Update web types/API and shared option helpers.
6. Refactor VPS create/edit network and country controls.
7. Refactor subscription create/edit into shared form with new billing and renewal controls.
8. Add VPS validity extension UI and refresh behavior.
9. Add VPS-origin monitoring instance creation and onboarding completion routing.
10. Widen Modal sizing and fold onboarding secondary sections.
11. Update specs/docs touched by new form contracts.
12. Run focused and full verification.

## Validation Commands

- `go test ./internal/center/subscriptions ./internal/center/store ./internal/center/http/handlers`
- `go test ./...`
- `cd web && npm run lint`
- `cd web && TMPDIR=$PWD/.tmp npm run test -- --run VPSPage VPSDetailPage SubscriptionsPage MonitoringPage MonitoringDetailPage`
- `cd web && TMPDIR=$PWD/.tmp npm run test -- --run`
- `cd web && npm run build`
- `git diff --check`

## Risk Points

- Subscription field migration touches many tests and list/detail display paths.
- Validity extension updates an existing subscription and must not silently choose among multiple active subscriptions.
- Query-param onboarding completion must not reopen drawers on refresh/back.
- Clipboard auto-copy must gracefully degrade in jsdom and browsers without clipboard permission.
