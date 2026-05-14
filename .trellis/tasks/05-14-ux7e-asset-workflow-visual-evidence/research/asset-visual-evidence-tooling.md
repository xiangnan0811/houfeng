# Asset visual evidence tooling research

## Route and auth facts

- `web/src/app/router.tsx` registers `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions` under `<RequireAuth />`.
- `RequireAuth` returns `null` while auth is loading and redirects to `/login?next=...` when no user is available.
- `AuthProvider` resolves initial auth through `auth-client.me()`, which calls `/api/auth/me`.
- Therefore browser sanity against a plain Vite preview does not exercise protected asset page surfaces unless `/api/auth/me` returns a valid user.

## Shell and asset API facts

- `AppShell` calls `getDashboard()` / `/api/dashboard` to derive sidebar sync status and anomaly counts.
- `AssetDecisionsPage` calls:
  - `/api/subscriptions?renew_within_days=<30|60|90>&sort=renew_at&order=asc`
  - `/api/subscriptions?sort=renew_at&order=asc`
  - `/api/vps?renewal_decision=unreviewed`
  - `/api/vps?renewal_decision=migrate`
  - `/api/vps?renewal_decision=cancel`
- `VPSPage` calls `/api/vps`, `/api/providers`, and `/api/subscriptions?sort=renew_at&order=asc`, then applies URL-state quick views client-side.
- `ProvidersPage` calls `/api/providers`.
- `SubscriptionsPage` calls `/api/subscriptions` with optional `vps_id`, `status`, `renew_within_days`, `sort`, and `order`, plus `/api/vps` for select labels.

## Tooling facts

- Existing `scripts/visual_evidence.py browser-sanity` checks:
  - nonblank body text,
  - document/body horizontal overflow,
  - leaf text overflow risks,
  - page surface count.
- Existing browser-sanity behavior has no network mocking and remains useful for public pages or a fully running local center.
- Repository policy forbids adding Playwright/Cypress/WebDriverIO dependencies to `web/package.json` for this class of task.
- Local interpreter check:
  - `python3` has no `playwright` module.
  - `/opt/homebrew/opt/python@3.11/bin/python3.11` has the `playwright` module available.

## External guideline check

- The latest Vercel Web Interface Guidelines were fetched from `https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md`.
- Relevant checks for this slice:
  - route state should be observable in URL where applicable,
  - focus and interactive controls should stay reachable,
  - content must not overflow viewports,
  - drawer/modal boundaries should be clear,
  - labels and state text must stay visible without requiring instruction copy.

## Recommended implementation

Add an opt-in browser-sanity argument such as `--mock-api asset-workflows`.

The helper should:

- install Playwright request routes before each page navigation,
- fulfill `/api/auth/me` with a stable admin fixture user,
- fulfill `/api/dashboard` with a shape-complete dashboard overview fixture,
- fulfill `/api/providers`, `/api/vps`, and `/api/subscriptions` from asset workflow fixture rows,
- apply simple query filtering for `renewal_decision`, `provider_id`, `lifecycle_status`, `usage_status`, `vps_id`, `status`, and `renew_within_days`,
- print the mock profile in browser-sanity output so evidence provenance is obvious.

This keeps UX-7E evidence repeatable without claiming real-data correctness.

