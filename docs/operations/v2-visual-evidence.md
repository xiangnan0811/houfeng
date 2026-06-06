# v2 UI preview and browser-sanity workflow

> Status: active local workflow for v2-houfeng UI checks.
>
> Visual authority: `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`.
>
> Scope: local preview, route/viewport browser sanity, and human review notes for user-visible frontend work. This does not replace automated lint/test/build checks.

## Why this exists

The removed V1 visual verification flow was tied to historical Stitch screenshots. The active product direction has moved to v2-houfeng. Future UI work still needs a repeatable way to answer:

- Which local URL can the reviewer open?
- Which routes and viewports were checked?
- Was the check automated browser sanity or manual inspection?
- What evidence exists, and what remains a human judgment?

Bulk screenshot evidence is no longer tracked in this repository. Capture local screenshots only when useful for discussion, keep them out of git, and commit image assets only after explicit approval as public README/docs assets under the allowlisted docs asset path.

## Evidence levels

Use the lowest level that honestly proves the change.

| Level | Required when | Evidence |
| --- | --- | --- |
| Preview URL | Any user-visible UI change | Running dev server URL and route list in the final report / PR body |
| Browser sanity | Core page UX, layout, responsive, or route-level interaction changes | Notes from desktop and mobile checks: routes, viewport sizes, key selectors, overflow / overlap result |
| Local screenshot for review | First-viewport structure, page hierarchy, theme, or cross-page UX materially changes and a reviewer asks for visual context | Local, untracked screenshots or external attachments; do not commit bulk screenshots or manifests by default |
| Manual review | Visual quality, taste, density, copy, or product judgment cannot be automated | Explicit reviewer notes; do not present automated tests as visual acceptance |

Vitest, lint, build, and CI are still required quality gates, but they are not visual proof.

## Local preview

Start the web dev server from `web/`:

```bash
cd web
npm run dev -- --host 127.0.0.1 --port 5178
```

If the center API is running elsewhere, set `VITE_API_TARGET`:

```bash
cd web
VITE_API_TARGET=http://127.0.0.1:8080 npm run dev -- --host 127.0.0.1 --port 5178
```

If port `5178` is occupied, use another explicit port and report the actual URL.

Every UI task final report should include:

- preview URL;
- routes checked;
- viewports checked;
- whether the server is still running or has been stopped;
- any blocked evidence, with reason.

## Repo-local browser sanity helper

For browser sanity against a running preview, use the local-only helper:

```bash
python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --route /monitoring \
  --route /targets \
  --viewport 1440x1000 \
  --viewport 390x900
```

If no `--viewport` is provided, the helper uses the standard `1440x1000` and `390x900` viewports. It fails on nonblank-body and page-level horizontal overflow problems, and reports obvious text overflow risks as warnings for human review. It intentionally does not take screenshots or compare pixels.

The browser sanity helper uses locally installed Python Playwright when available. The repository intentionally does not depend on Playwright/Cypress/WebDriverIO, so a missing browser driver is a local tooling limitation to report, not a CI failure.

If your machine has multiple Python versions, run the helper with the interpreter that owns the local Playwright package, for example:

```bash
/opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --route /login
```

### Protected asset workflow routes

Asset Ledger routes are protected by the app auth gate and need center API data before their page surfaces render. Browser sanity against a plain Vite preview may only prove that `/login` works. To exercise the protected asset routes without a running center, use the explicit local mock API profile:

```bash
mkdir -p .tmp/playwright
TMPDIR="$PWD/.tmp/playwright" python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api asset-workflows \
  --route /asset-decisions \
  --route /vps \
  --route /providers \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900
```

Use the interpreter that has local Python Playwright installed. For example, on this machine:

```bash
TMPDIR="$PWD/.tmp/playwright" /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api asset-workflows \
  --route /asset-decisions \
  --route /vps \
  --route /providers \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900
```

`--mock-api asset-workflows` intercepts `/api/auth/me`, `/api/dashboard`, `/api/asset-decisions/*`, `/api/providers`, `/api/vps`, and `/api/subscriptions` in the browser session. The fixture rows intentionally cover the asset portfolio decision workbench: automatic decision groups, context filter chips, closed-loop next work, scenario templates, manual groups, saved decision records with readback/plan snippets, renewal evidence, the single-asset auxiliary queue, missing subscription, unlinked VPS, missing facts, provider labels/ratings, subscription filters, and shell summary state.

Report this as `Data source: mock-api asset-workflows`. It proves the protected route layout can render with representative asset workflow states, but it does not prove backend correctness, real account completeness, import fidelity, or the real inventory result.

If local Playwright cannot create browser temp files, prefer a repo-local temp directory (`TMPDIR="$PWD/.tmp/playwright"`) and record that in the evidence notes. Do not add browser automation dependencies to `web/package.json` to work around local tooling.

### Observability support routes

Monitoring, Targets, and Events are also protected by the app auth gate. To exercise the observability support pages without a running center, use the explicit local mock API profile:

```bash
mkdir -p .tmp/playwright
TMPDIR="$PWD/.tmp/playwright" python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api observability-support \
  --route /monitoring \
  --route /targets \
  --route /events \
  --viewport 1440x1000 \
  --viewport 390x900
```

Use the interpreter that has local Python Playwright installed when needed:

```bash
TMPDIR="$PWD/.tmp/playwright" /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api observability-support \
  --route /monitoring \
  --route /targets \
  --route /events \
  --viewport 1440x1000 \
  --viewport 390x900
```

`--mock-api observability-support` intercepts `/api/auth/me`, `/api/dashboard`, `/api/monitoring-instances`, `/api/monitoring-instances/sparklines`, `/api/targets`, `/api/targets/sparklines`, and `/api/events` in the browser session. The fixture rows intentionally cover abnormal monitoring instances, pending onboarding and binding conflict, maintenance / paused / retired monitoring instances, abnormal targets, paused / archived / maintenance targets, missing execution coverage, event severity, recovery / maintenance / notification filters, and explicit backfilled event opt-in.

Report this as `Data source: mock-api observability-support`. It proves the protected observability support route layout can render with representative states, but it does not prove backend correctness, real incident evaluation, real notification delivery, real backfill classification, or real asset-to-observability linkage.

`asset-workflows` and `observability-support` are separate mock profiles. A route outside the selected profile intentionally returns a profile-specific mock 404 so browser sanity failures make the selected data source clear. Real login cannot be combined with `--mock-api`; for authenticated local center checks, use the local center sample flow below instead.

If local Playwright cannot create browser temp files, prefer a repo-local temp directory (`TMPDIR="$PWD/.tmp/playwright"`) and record that in the evidence notes. Missing local Playwright or browser runtime is a local tooling limitation, not a reason to add Playwright/Cypress/WebDriverIO to `web/package.json`.

### Local center sample routes

After `houfeng-center` is running and a disposable local database contains the sample or manually entered Asset Ledger records, run browser sanity with the real login flow instead of `--mock-api`:

```bash
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

`--login-username-env` and `--login-password-env` read credentials from environment variables and authenticate through `/api/auth/login` before navigating protected routes. The helper prints `auth=session-login` and fails if a protected route is redirected away from the requested path. Real login cannot be combined with `--mock-api`; data source labels should be one of `mock-api asset-workflows`, `mock-api observability-support`, `local center sample`, or `real data`.

The local sample and real-data readiness workflow is documented in `docs/operations/asset-ledger-real-data-validation-readiness.md`. One-off UX mock asset workflow evidence has been folded into the mock/local/real data guidance above and removed from tracked docs; use the current sections in this document for new checks.

## Core route matrix

This is the current v2 core-page acceptance set. A task only needs to check routes it changes, but broad UX tasks should cover the full relevant subset.

| Surface | Route | Why it matters |
| --- | --- | --- |
| Dashboard / 工作台 | `/` | Command desk and first daily entry |
| Asset decisions | `/asset-decisions` | Portfolio decision workbench with automatic groups, scenario/manual groups, saved records/readback, and auxiliary single-asset queue |
| VPS inventory | `/vps` | Primary real-data testing entry |
| VPS detail | `/vps/:vpsId` | Single-asset decision workbench |
| Monitoring | `/monitoring` | Monitoring instances and runtime evidence for assets |
| Monitoring detail | `/monitoring/:monitoringInstanceId` | Runtime evidence and watchtower details |
| Targets | `/targets` | Service / entry observability evidence |
| Target detail | `/targets/:targetId` | ProbeItem and entry detail workflow |
| Events | `/events` | Diagnostic and audit timeline |
| Settings | `/settings` | Runtime configuration and theme controls |

## Recommended viewports

Use at least these when browser sanity is required:

| Viewport | Purpose |
| --- | --- |
| `1440x1000` | Primary desktop workbench |
| `390x900` | Narrow mobile sanity for text overflow and layout collapse |

For large tables, also check horizontal scroll behavior rather than forcing all columns into mobile width.

## Screenshot and image policy

Do not restore or reuse old V1 visual-evidence paths, the removed `docs/operations/v2-visual-evidence/` directory, or bulk screenshot manifests. Local screenshots may be useful while discussing UI changes, but they should remain untracked unless the user explicitly approves specific images for public README/docs presentation.

Repository ignore rules block common raster image formats in docs by default. Future approved public images should use the allowlisted `docs/public-assets/` path and be referenced from README or maintained docs. That directory is intentionally not created until there is an approved public asset. SVG is not blocked by this image policy because the project may use SVG as source assets or code-like vector assets.

## Browser sanity checklist

For each checked route:

- First viewport shows the page's primary workflow, not only navigation chrome.
- Text does not overlap or escape buttons, badges, cards, table cells, or sidebars.
- Primary actions and links are visible and named clearly.
- Drawer / modal entry points remain reachable by keyboard and mouse.
- Table-heavy pages still allow scanning; narrow screens may scroll horizontally where appropriate.
- Empty / loading / error states do not create large confusing blank areas.
- Theme tokens are used; no return to the removed v1/Stitch visual direction.

## Temporary automation helpers

External browser tools such as a locally installed Playwright CLI may be used for one-off sanity checks or local screenshots, but they are not part of the repository contract unless a separate task intentionally introduces browser automation.

The repo-local `scripts/visual_evidence.py browser-sanity` command is also a temporary/local evidence helper. It standardizes the geometry checks and output shape, but it remains outside `make verify-web` and CI.

Current repository constraints:

- Do not add Playwright, Cypress, WebDriverIO, or screenshot diffing dependencies in ordinary UI tasks.
- Do not add CI visual regression without a dedicated architecture decision.
- If a local browser automation run depends on machine-specific tools, record it as local evidence and include the limitation.
- Do not commit screenshot directories or manifests by default; attach local screenshots externally or use approved public assets only.

## PR / final-report template

Use this section in PR bodies and final reports for UI tasks:

```markdown
## Visual / UX Evidence

- Preview URL: http://127.0.0.1:5178/
- Routes checked:
  - `/`
  - `/vps`
- Viewports:
  - 1440x1000
  - 390x900
- Evidence level:
  - Browser sanity / manual review / blocked
  - Local screenshots: none / external attachment only, not committed
- Data source:
  - mocked API / mock-api asset-workflows / mock-api observability-support / local center / real data
- Result:
  - no blank viewport, no text overlap, no support-surface overflow
- Limitations:
  - no screenshot committed / real data not tested / browser automation was local only
```

## Relationship to release workflow

This document covers UI preview and browser sanity only. It does not define release/publish workflow, Docker publishing, Release Please, or production deployment automation.
