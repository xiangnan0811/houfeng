# Asset Ledger real-data validation readiness

> Date: 2026-05-14
>
> Scope: `/asset-decisions`, `/vps`, `/providers`, `/subscriptions`
>
> Status: current readiness workflow; local sample evidence exists, while real 40+ VPS execution still requires a user-provided or explicitly authorized data source.
>
> Public navigation: this is the current place for Asset Ledger sample/real-data validation boundaries. Do not convert it into a claim that real provider billing, provider account truth, exchange rates, linked monitoring instance health, or the user's real inventory have been verified.

## Purpose

The local browser helper renders protected Asset Ledger routes with `mock-api asset-workflows`. That is useful visual evidence, but it is not proof of real authentication, database import fidelity, or the user's actual inventory shape.

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
export HOUFENG_SESSION_HMAC_KEY='replace-me-with-32-plus-random-bytes'
```

Build and start the center as documented in `docs/operations/fresh-install-smoke-run.md`:

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
- Keep only operational fields needed by the current importer: provider identity, VPS display name, product/location/access facts, VPS lifecycle/usage/renewal decision, labels, notes, subscription billing facts (price, currency, billing cadence, renewal date, auto-renew/payment/note), and optional monitoring instance/target hints. Treat legacy subscription status only as imported historical/internal evidence, not as a user-entered business state.
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
- `/subscriptions`: price/monthly conversion, renewal sorting, auto-renew/payment labels, and links back to the VPS detail workbench. Subscription status is not a primary user filter in the VPS-first flow.

If the real-data shape materially changes visual judgment, capture local screenshots for private review or external attachment, but do not commit screenshot directories or manifests by default. Browser sanity plus explicit row counts and limitations is enough for the readiness pass unless the user explicitly approves public README/docs image assets.

## 历史证据与本次验收记录

2026-05-14 的样例执行记录已通过 Git 保留；它只证明当时的本地认证、样例导入和浏览器几何检查，不是当前数据库迁移、真实库存或生产验收结论。固定 JSON 样例继续供 dry-run 使用。

每次重新验收记录日期、Center 版本、数据库范围、数据来源、dry-run 与导入计数、认证方式、路线和视口、通知配置及限制。只清理本次创建的可丢弃实例和临时文件，保护已有数据库与其他预览进程。

样例中的 MonitoringInstance 关联提示需要人工确认，导入不会创建 `vps_monitoring_instance_links`。供应商账户、外部账单、关联监控健康、汇率和生产部署均需独立证据。
