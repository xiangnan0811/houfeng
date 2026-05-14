# UX-7E asset workflow visual evidence

> Date: 2026-05-14
>
> Scope: `/asset-decisions`, `/vps`, `/providers`, `/subscriptions`
>
> Evidence level: local browser sanity, no committed screenshots

## Preview

- Preview URL: `http://127.0.0.1:5178/`
- Server command: `npm --prefix web run dev -- --host 127.0.0.1 --port 5178`
- Browser helper: `scripts/visual_evidence.py browser-sanity`
- Browser runtime: local Python Playwright through `/opt/homebrew/opt/python@3.11/bin/python3.11`
- Temp directory: `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright`
- Data source: `mock-api asset-workflows`

The mock API profile intercepts protected-route API calls inside the browser session. It returns a fixture user, dashboard shell summary, providers, VPS assets, and subscriptions so the Asset Ledger pages render their actual page surfaces instead of stopping at the login gate.

## Command

```bash
TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api asset-workflows \
  --route /asset-decisions \
  --route /vps \
  --route /providers \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900
```

## Result

```text
PASS /asset-decisions 1440x1000 text=1552 doc=1440 body=1440 panels=4 mock=asset-workflows url=http://127.0.0.1:5178/asset-decisions
PASS /asset-decisions 390x900 text=1540 doc=390 body=390 panels=4 mock=asset-workflows url=http://127.0.0.1:5178/asset-decisions
PASS /vps 1440x1000 text=1302 doc=1440 body=1440 panels=4 mock=asset-workflows url=http://127.0.0.1:5178/vps
PASS /vps 390x900 text=1290 doc=390 body=390 panels=4 mock=asset-workflows url=http://127.0.0.1:5178/vps
PASS /providers 1440x1000 text=487 doc=1440 body=1440 panels=3 mock=asset-workflows url=http://127.0.0.1:5178/providers
PASS /providers 390x900 text=475 doc=390 body=390 panels=3 mock=asset-workflows url=http://127.0.0.1:5178/providers
PASS /subscriptions 1440x1000 text=857 doc=1440 body=1440 panels=5 mock=asset-workflows url=http://127.0.0.1:5178/subscriptions
PASS /subscriptions 390x900 text=845 doc=390 body=390 panels=5 mock=asset-workflows url=http://127.0.0.1:5178/subscriptions
```

The run found no blank page, no page-level horizontal overflow, and no reported leaf-text overflow warnings on the standard desktop and mobile viewports.

## Fixture coverage

The `asset-workflows` profile covers:

- authenticated user state through `/api/auth/me`;
- AppShell dashboard summary through `/api/dashboard`;
- provider list with ratings, countries, account hints, and labels;
- VPS rows with unreviewed, migrate, cancel, keep, archived, missing facts, unlinked, and complete-data states;
- subscription rows with renewal-window, auto-renew, auto-renew-cancelled, active, and cancelled states;
- query filtering for VPS renewal/provider/lifecycle/usage filters and subscription VPS/status/renewal-window filters.

This is representative UI evidence, not production data evidence.

## Limitations

- No screenshots were committed, so no manifest rows were added.
- The run did not use a real center process or real user inventory.
- The run validates frontend layout geometry and protected-route rendering only; it does not validate backend handlers, database rows, auth cookies, import fidelity, provider account truth, or real renewal costs.
- The run checks route first viewports. It does not capture drawer-open screenshots or perform a full keyboard walkthrough.
- Fixture renewal dates are generated relative to the run date so the renewal-window UI remains meaningful.

## Route/data checklist for real 40+ VPS validation

Use this checklist before treating the UI as validated against real data.

### Mock API

- [x] `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions` render through the auth gate.
- [x] Standard `1440x1000` and `390x900` viewports have no page-level horizontal overflow.
- [x] Representative states are visible: renewal due, unreviewed, migrate, cancel, missing subscription, unlinked VPS, missing facts, provider labels, and subscription filters.
- [ ] Drawer-open surfaces are captured if the task materially changes drawer layout.

### Local center sample

- [ ] Run `houfeng-center` locally and authenticate through the real login flow.
- [ ] Seed or manually enter a small non-sensitive sample: at least 3 providers, 5 VPS rows, and 4 subscriptions covering the same state matrix as the mock fixture.
- [ ] Re-run browser sanity without `--mock-api` and record `Data source: local center sample`.
- [ ] Verify `/vps` quick views and URL chips show the same state reflected by the local center data.
- [ ] Verify `/subscriptions` filters use real API query parameters and visible chips.
- [ ] Verify create/edit panels or drawers submit to the local center only when intentionally tested.

### Real inventory

- [ ] Confirm the real 40+ VPS data source is privacy-reviewed and excludes secrets, tokens, SSH private material, and unrelated account metadata.
- [ ] Decide whether the first pass is manual entry, import dry-run, or a seeded local center database.
- [ ] Record real-data evidence as `Data source: real data` and include the date, row counts, and any redactions.
- [ ] Check `/asset-decisions` queue priority for renewal due, unreviewed, migrate/cancel, missing subscriptions, and unlinked rows.
- [ ] Check `/vps` scanning density, quick views, provider filters, table scroll surface, and missing-fact badges with the full row count.
- [ ] Check `/providers` for duplicate provider naming, account hints, ratings, labels, and update timestamps.
- [ ] Check `/subscriptions` for price/monthly conversion, renewal sorting, status filters, and auto-renew labels.
- [ ] Capture screenshots and manifest rows if visual review depends on the real-data shape.
- [ ] Keep observability facts bounded: list pages must not infer linked node health unless the page contract explicitly returns it.

