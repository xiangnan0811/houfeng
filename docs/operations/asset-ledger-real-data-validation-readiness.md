# Asset Ledger real-data validation readiness

> Date: 2026-05-14
>
> Scope: `/asset-decisions`, `/vps`, `/providers`, `/subscriptions`
>
> Status: current readiness workflow; local sample evidence exists, while real 40+ VPS execution still requires a user-provided or explicitly authorized data source.
>
> Public navigation: this is the current place for Asset Ledger sample/real-data validation boundaries. Do not convert it into a claim that real provider billing, provider account truth, exchange rates, linked monitoring instance health, or the user's real inventory have been verified.

## Purpose

UX-7E made protected Asset Ledger routes renderable with `mock-api asset-workflows`. That is useful visual evidence, but it is not proof of real authentication, database import fidelity, or the user's actual inventory shape.

This workflow is the bridge between mocked layout evidence and real inventory validation:

1. validate a non-sensitive sample JSON through the existing import dry-run;
2. optionally import that sample into a local center database;
3. authenticate through the real center login flow;
4. run browser sanity against the protected asset routes without mock API;
5. only then move to the user's real 40+ VPS data after privacy review and authorization.

## Evidence Levels

| Data source | What it proves | What it does not prove |
| --- | --- | --- |
| `mock-api asset-workflows` | Protected route layout renders with representative frontend fixture states | Real auth cookies, database rows, import fidelity, provider account truth, real row counts |
| `local center sample` | Real login, real center API, imported or manually entered non-sensitive sample data, route geometry | User inventory completeness, actual billing truth, production provider account state |
| `real data` | Product fit against the user's real VPS inventory after redaction and authorization | Anything not present in the provided dataset, external provider truth unless separately checked |

## Safe Local Sample

Committed sample file:

```text
docs/operations/asset-ledger-local-sample.json
```

The sample uses fake provider names, fake order refs, reserved documentation IP ranges, `.example.invalid` URLs, and no secrets. It includes:

- 4 provider names across 5 VPS rows;
- 5 VPS candidates;
- 4 subscriptions;
- renewal-window candidates using dates valid for the 2026-05 readiness pass;
- one missing-subscription VPS;
- one missing-facts / cancellation-oriented VPS;
- one idle paid VPS;
- monitoring instance/target association hints that require manual confirmation.

If this sample is reused long after May 2026 and the renewal-window UI is the evidence target, update the `renew_at` dates in a temporary copy rather than changing the committed historical sample without a separate reason.

## Dry-Run Sample

Run the dry-run without writing to a database:

```bash
go run ./cmd/houfeng-import-vps-json \
  -file docs/operations/asset-ledger-local-sample.json \
  -dry-run \
  -format json
```

Expected high-level result:

- `can_import: true`;
- provider, VPS, and subscription candidates are present;
- no validation errors;
- no duplicate candidates in an empty/file-only dry-run;
- monitoring instance association candidates require manual confirmation;
- the missing-subscription VPS appears as a VPS candidate but not a subscription candidate.

This command does not prove database duplicate checks unless `HOUFENG_DATABASE_URL` is set and reachable.

## Local Center Sample Path

Use a local database that can be discarded. Do not point these commands at production.

```bash
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
export HOUFENG_INITIAL_USERNAME=admin
export HOUFENG_INITIAL_PASSWORD='replace-me-with-a-real-local-password'
```

Build and start the center as documented in `docs/operations/v1-smoke-run.md`:

```bash
npm --prefix web run build
go run ./cmd/houfeng-center
```

In another shell, run a database-aware dry-run:

```bash
go run ./cmd/houfeng-import-vps-json \
  -file docs/operations/asset-ledger-local-sample.json \
  -dry-run \
  -format json
```

If the dry-run is clean and the database is disposable, import the sample:

```bash
go run ./cmd/houfeng-import-vps-json \
  -file docs/operations/asset-ledger-local-sample.json \
  -import \
  -format json
```

Import is intentionally explicit. Do not use `-import` against a database that contains production or important local data unless you have already reviewed duplicates and rollback strategy.

## Authenticated Browser Sanity

Start the Vite dev server, pointing it at the local center:

```bash
cd web
VITE_API_TARGET=http://127.0.0.1:8080 npm run dev -- --host 127.0.0.1 --port 5178
```

From the repository root, run browser sanity with real center login. Prefer env-backed credentials:

```bash
export HOUFENG_INITIAL_USERNAME=admin
export HOUFENG_INITIAL_PASSWORD='replace-me-with-a-real-local-password'

mkdir -p .tmp/playwright
TMPDIR="$PWD/.tmp/playwright" python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --login-username-env HOUFENG_INITIAL_USERNAME \
  --login-password-env HOUFENG_INITIAL_PASSWORD \
  --route /asset-decisions \
  --route /vps \
  --route /providers \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900
```

Use the interpreter that owns the local Python Playwright package when needed:

```bash
TMPDIR="$PWD/.tmp/playwright" /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --login-username-env HOUFENG_INITIAL_USERNAME \
  --login-password-env HOUFENG_INITIAL_PASSWORD \
  --route /asset-decisions \
  --route /vps \
  --route /providers \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900
```

The helper prints `auth=session-login` when it authenticated through `/api/auth/login`. It fails the route if the final browser path is unexpectedly redirected away from the requested protected route.

Report this as:

```text
Data source: local center sample
Evidence: authenticated browser sanity
Limitations: local-only browser runtime; sample data only; no real inventory imported
```

## Real Inventory Checklist

Before using the user's real 40+ VPS data:

- Confirm explicit authorization for the specific source file or export.
- Keep the real source file out of git unless the user explicitly wants a redacted fixture committed.
- Remove secrets: SSH private keys, API tokens, provider passwords, one-time recovery codes, session cookies, billing portal credentials, agent tokens, webhook URLs, and private notes unrelated to asset operation.
- Replace or redact account identifiers that are not useful for product validation.
- Keep only operational fields needed by the current importer: provider identity, VPS display name, product/location/access facts, lifecycle/usage/renewal decision, labels, notes, subscription price/currency/billing/renewal/status, and optional monitoring instance/target hints.
- Use `null` or omit unknown optional facts; do not invent fake dates or prices for real-data validation.
- Run `-dry-run -format json` first and review `validation_errors`, `duplicate_candidates`, `missing_provider_rows`, `missing_renew_date_rows`, `monitoring_instance_association_candidates`, `renewal_candidates`, and `idle_paid_candidates`.
- Decide whether to import into a disposable local database or do manual entry for the first pass.
- Record evidence date, data source, redaction status, row counts, route list, viewport list, and any blocked checks.
- Do not claim provider account truth, exchange rates, real linked-monitoring instance health, or billing accuracy unless those facts were independently verified.

## UI Review Focus

When real or local-sample data is visible:

- `/asset-decisions`: queue priority for renewal due, unreviewed, migrate/cancel, missing subscriptions, and unlinked rows.
- `/vps`: scanning density, quick views, visible URL chips, provider filters, missing-fact badges, table scroll behavior, and mobile readability.
- `/providers`: duplicate provider naming, account hints, ratings, labels, and update timestamps.
- `/subscriptions`: price/monthly conversion, renewal sorting, status filters, and auto-renew labels.

If the real-data shape materially changes visual judgment, capture local screenshots for private review or external attachment, but do not commit screenshot directories or manifests by default. Browser sanity plus explicit row counts and limitations is enough for the readiness pass unless the user explicitly approves public README/docs image assets.

## Local Center Sample Evidence

> Date: 2026-05-14
>
> Evidence level: authenticated browser sanity, no committed screenshots
>
> Data source: `local center sample`

### Runtime

- PostgreSQL: disposable `postgres:16-alpine` container named `houfeng-local-sample-postgres`, published on `127.0.0.1:15432`.
- Center: `./bin/houfeng-center`, `HOUFENG_HTTP_ADDR=:18080`, `HOUFENG_WEB_DIST_DIR=web/dist`.
- Browser base URL: `http://127.0.0.1:18080/`.
- Browser runtime: local Python Playwright through `/opt/homebrew/opt/python@3.11/bin/python3.11`.
- Temp directory: `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright`.
- Credentials: local throwaway initial user credentials via `HOUFENG_INITIAL_USERNAME` and `HOUFENG_INITIAL_PASSWORD`; no real account credentials.

### Build And Startup

```bash
npm --prefix web run build
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go build -o ./bin/houfeng-center ./cmd/houfeng-center
```

Center health passed:

```text
{"name":"houfeng-center","version":"dev","status":"ok"}
```

The center applied 25 schema migrations in the disposable database.

### Sample Dry-Run

After center startup and migrations, the sample dry-run was database-aware:

```bash
HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@127.0.0.1:15432/houfeng?sslmode=disable' \
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp \
GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build \
go run ./cmd/houfeng-import-vps-json \
  -file docs/operations/asset-ledger-local-sample.json \
  -dry-run \
  -format json
```

Result summary:

- `database_checked: true`
- `can_import: true`
- `warnings: []`
- `input_rows: 5`
- `provider_create_candidates: 4`
- `vps_create_candidates: 5`
- `subscription_candidates: 4`
- `validation_errors: 0`
- `duplicate_candidates: 0`
- `monitoring_instance_association_candidates: 3`
- `renewal_candidates: 2`
- `idle_paid_candidates: 1`

### Sample Import

```bash
HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@127.0.0.1:15432/houfeng?sslmode=disable' \
TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp \
GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build \
go run ./cmd/houfeng-import-vps-json \
  -file docs/operations/asset-ledger-local-sample.json \
  -import \
  -format json
```

Result summary:

- `imported_providers: 4`
- `imported_vps_assets: 5`
- `imported_subscriptions: 4`

Post-import database row counts:

```text
providers: 4
vps_assets: 5
subscriptions: 4
```

### Authenticated Browser Sanity

Command:

```bash
HOUFENG_INITIAL_USERNAME=admin \
HOUFENG_INITIAL_PASSWORD='<redacted local throwaway password>' \
TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright \
/opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:18080/ \
  --login-username-env HOUFENG_INITIAL_USERNAME \
  --login-password-env HOUFENG_INITIAL_PASSWORD \
  --route /asset-decisions \
  --route /vps \
  --route /providers \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900
```

Result:

```text
PASS /asset-decisions 1440x1000 text=1436 doc=1440 body=1440 panels=4 auth=session-login url=http://127.0.0.1:18080/asset-decisions
PASS /asset-decisions 390x900 text=1424 doc=390 body=390 panels=4 auth=session-login url=http://127.0.0.1:18080/asset-decisions
PASS /vps 1440x1000 text=1568 doc=1440 body=1440 panels=4 auth=session-login url=http://127.0.0.1:18080/vps
PASS /vps 390x900 text=1556 doc=390 body=390 panels=4 auth=session-login url=http://127.0.0.1:18080/vps
PASS /providers 1440x1000 text=528 doc=1440 body=1440 panels=3 auth=session-login url=http://127.0.0.1:18080/providers
PASS /providers 390x900 text=516 doc=390 body=390 panels=3 auth=session-login url=http://127.0.0.1:18080/providers
PASS /subscriptions 1440x1000 text=938 doc=1440 body=1440 panels=5 auth=session-login url=http://127.0.0.1:18080/subscriptions
PASS /subscriptions 390x900 text=926 doc=390 body=390 panels=5 auth=session-login url=http://127.0.0.1:18080/subscriptions
```

The run found no blank page, no unexpected login redirect, no page/body horizontal overflow, and no reported leaf-text overflow warnings on the standard desktop and mobile viewports.

### Cleanup

The local center process was stopped, and the disposable PostgreSQL container was removed:

```text
docker rm -f houfeng-local-sample-postgres
```

### Limitations

- No screenshots were committed; screenshot directories and manifests are intentionally not tracked by default.
- This proves the local center sample path, not the user's real 40+ VPS inventory.
- MonitoringInstance association hints correctly remained manual evidence; the import did not create `vps_monitoring_instance_links`.
- Provider account truth, external billing truth, linked monitoring instance health, exchange rates, and production deployment behavior were not validated.
