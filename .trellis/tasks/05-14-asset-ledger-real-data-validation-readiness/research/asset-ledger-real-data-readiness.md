# Asset Ledger real-data readiness research

## Existing UX evidence state

- UX-7E added `scripts/visual_evidence.py browser-sanity --mock-api asset-workflows`.
- The mock profile intercepts `/api/auth/me`, `/api/dashboard`, `/api/providers`, `/api/vps`, and `/api/subscriptions`.
- UX-7E evidence passed for `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions` at `1440x1000` and `390x900`.
- UX-7E docs explicitly say mock evidence does not prove backend correctness, auth cookies, import fidelity, or real inventory truth.

## Existing import tool

- `cmd/houfeng-import-vps-json` accepts `-file`, `-dry-run`, `-import`, and `-format text|json`.
- With neither `-dry-run` nor `-import`, it defaults to dry-run.
- Dry-run can run without `HOUFENG_DATABASE_URL`; when a database URL is present but repository access fails, it records a warning and continues in file-only analysis mode.
- Import mode requires `HOUFENG_DATABASE_URL`, applies migrations, starts a transaction, uses Asset Ledger repositories, and commits only after import succeeds.
- `internal/center/importing.DecodeRecords` reads a strict JSON array, rejects unknown fields, and rejects trailing data.
- `internal/center/importing` validates provider, VPS, and subscription create inputs through the existing domain packages.

## Import input contract

Relevant `InputRecord` fields:

- Provider/VPS identity: `display_name`, `provider_id`, `provider_name`, `product_name`, `order_ref`.
- Location and access facts: `country`, `region`, `city`, `datacenter`, `ipv4`, `ipv6`, `ssh_host`, `ssh_port`, `ssh_user`, `os_name`, `virtualization`.
- Asset decisions: `lifecycle_status`, `usage_status`, `renewal_decision`, `importance`, `labels`, `note`.
- Subscription: `price`, `currency`, `billing_cycle`, `billing_months`, `started_at`, `renew_at`, `auto_renew`, `auto_renew_cancelled`, `status`, `payment_method`, `note`.
- Association hints: `node_id`, `node_name`, `agent_token_hint`, `target_url`.

The import report includes totals, provider candidates, VPS candidates, subscription candidates, missing provider rows, missing renewal dates, validation errors, duplicate candidates, node association candidates, renewal candidates, idle paid candidates, and import results.

## Local center facts

- `docs/operations/v1-smoke-run.md` documents local center startup with `HOUFENG_HTTP_ADDR`, `HOUFENG_WEB_DIST_DIR`, `HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, and `HOUFENG_INITIAL_PASSWORD`.
- `docs/deploy/local-and-systemd.md` states that all `/api/*` routes except health and agent endpoints require a session cookie.
- The browser helper currently has no real-login mode; without mock API or real session auth, protected routes may only prove `/login`.

## Relevant spec constraints

- Browser sanity remains local-only and outside CI; do not add Playwright/Cypress/WebDriverIO to `web/package.json`.
- Visual evidence must label data sources and limitations.
- Asset Ledger list pages must not infer linked node health from list contracts.
- Import tooling should use the existing dry-run/import command and must preserve transaction/dry-run boundaries.
- Real data execution is still user-data-dependent and must not be claimed complete without a provided/authorized dataset.

## Recommended implementation

1. Add environment-backed real-login options to `scripts/visual_evidence.py browser-sanity`.
2. Fail fast if only one credential is supplied, if credential environment variables are missing, or if real login is combined with `--mock-api`.
3. Use Playwright's context-bound request API to `POST /api/auth/login` before navigating each protected route.
4. In real-login or mock mode, check that the final browser path still matches the requested route so redirects to `/login` fail evidence.
5. Add a committed non-sensitive sample JSON under `docs/operations/` and verify it with the existing import dry-run command.
6. Add an operations doc that connects sample dry-run/import, center startup, authenticated browser sanity, and real-inventory privacy review.
