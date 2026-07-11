# UI preview and browser-sanity workflow

> Status: current repository Chromium, local helper, and authenticated staging evidence workflow.
>
> Design guidance: `docs/design/current/interface-language.md` and `docs/design/current/component-patterns.md`.
>
> Scope: repository Playwright contracts, local preview/helper checks, authenticated staging audit, and human review notes for user-visible frontend work. These layers complement `make verify-web`; none replaces product/visual judgment.

## Why this exists

Earlier visual verification material was tied to historical Stitch screenshots and versioned design bundles. Future UI work still needs a repeatable way to answer:

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
| Repository Chromium gate | Every PR/main CI; especially core page UX, state, keyboard, CSP or responsive changes | `npm run test:e2e`: fail-closed fixture routes, 27 route/viewport matrix, state/a11y/security/geometry contracts |
| Local helper / center sample | Incremental detail route or real local data investigation | `visual_evidence.py`/CDP notes with explicit data source and local-only limitation |
| Authenticated staging real lane | Release/Gate C | health version, UI login, nine real-data routes, cancel-only nested confirmation, reversible Settings save/restore, theme reload, sanitized artifact |
| Staging injection lane | Release/Gate C frontend resilience | Deployed assets/origin/auth with explicitly intercepted five-state/503/slow/long-list responses; never described as backend or production-data proof |
| Local screenshot for review | First-viewport structure, page hierarchy, theme, or cross-page UX materially changes and a reviewer asks for visual context | Local, untracked screenshots or external attachments; do not commit bulk screenshots or manifests by default |
| Manual review | Visual quality, taste, density, copy, or product judgment cannot be automated | Explicit reviewer notes; do not present automated tests as visual acceptance |

Vitest, coverage, lint, build, static budgets and Chromium are required quality gates, but they still do not prove visual taste or real inventory truth.

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

## Repository Chromium gate

From `web/`, run:

```bash
env -u NODE_ENV npm ci --include=dev
npm run test:e2e
```

`pretest:e2e` creates a fresh production build. `playwright.config.ts` then starts Vite preview at `127.0.0.1:4175` with `strictPort`; do not reuse or terminate an unrelated process on another port. The lockfile owns `@playwright/test`, `@axe-core/playwright` and the compatible Chromium revision.

The fixture router is fail closed: method + canonical path/query must match, mutation fixtures declare exact body keys, and unknown API calls fail teardown. Every test shares collection for console/page/request/HTTP/CSP/unhandled-rejection diagnostics. The main document CSP must equal `internal/center/http/csp-policy.txt`; no spec keeps a second policy literal.

Current suites:

- `core-routes.spec.ts`: nine routes × `1440x1000` / `1024x768` / `390x900` = 27 contracts;
- `page-states.spec.ts`: Dashboard five modes, controlled loading, explicit empty/error/success, 503 false-empty prevention and scoped retry;
- `accessibility.spec.ts`: axe serious/critical=0 plus skip link, Tabs, Menu and nested Modal real-keyboard behavior;
- `security.spec.ts` / `fixture-router.spec.ts`: collector self-tests, CSP, unknown API, canonical query and mutation-body fail-closed behavior;
- `visual-contracts.spec.ts`: 390px critical commands and named keyboard-scroll table region.

CI runs these in the independent `web-browser` job. CI screenshots/traces are failure-only, short-retention diagnostics; they are not tracked golden images.

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

The helper uses locally installed Python Playwright when available. It is independent from the repository's Node Playwright dependency/browser revision, so a missing Python driver only blocks this local helper, not `npm run test:e2e` or CI.

Missing Python Playwright only blocks this helper path. Prefer the repository `npm run test:e2e` for standard contracts; for a running local center/detail route, CDP or another already-installed tool may provide equivalent incremental evidence. Report browser/runtime, data source, routes, viewports, selectors/counters and limitations; do not add a second e2e framework for one local check.

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

`--mock-api asset-workflows` intercepts `/api/auth/me`, `/api/dashboard`, `/api/asset-decisions/*`, `/api/providers`, `/api/vps`, and `/api/subscriptions` in the browser session. The fixture rows intentionally cover the asset portfolio decision workbench: portfolio command summary, automatic decision groups, context filter chips, closed-loop next work, scenario templates, manual groups, saved decision records with readback/plan snippets, record execution lane board, renewal evidence, the single-asset auxiliary queue, missing subscription, unlinked VPS, missing facts, provider labels/ratings, subscription filters, and shell summary state.

Report this as `Data source: mock-api asset-workflows`. It proves the protected route layout can render with representative asset workflow states, but it does not prove backend correctness, real account completeness, import fidelity, or the real inventory result.

If local Python Playwright cannot create browser temp files, prefer a repo-local temp directory (`TMPDIR="$PWD/.tmp/playwright"`) and record that in the evidence notes. Do not add a second automation stack to work around local tooling.

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

`--mock-api observability-support` intercepts `/api/auth/me`, `/api/dashboard`, `/api/monitoring-instances`, `/api/monitoring-instances/sparklines`, `/api/targets`, `/api/targets/sparklines`, `/api/asset-context/targets`, and `/api/events` in the browser session. The fixture rows intentionally cover abnormal monitoring instances, pending onboarding and binding conflict, maintenance / paused / retired monitoring instances, abnormal targets, paused / archived / maintenance targets, Target asset context, missing execution coverage, event severity, recovery / maintenance / notification filters, and explicit backfilled event opt-in.

Report this as `Data source: mock-api observability-support`. It proves the protected observability support route layout can render with representative states, but it does not prove backend correctness, real incident evaluation, real notification delivery, real backfill classification, or real asset-to-observability linkage.

`asset-workflows` and `observability-support` are separate mock profiles. A route outside the selected profile intentionally returns a profile-specific mock 404 so browser sanity failures make the selected data source clear. Real login cannot be combined with `--mock-api`; for authenticated local center checks, use the local center sample flow below instead.

If local Python Playwright cannot create browser temp files, prefer a repo-local temp directory (`TMPDIR="$PWD/.tmp/playwright"`) and record that in the evidence notes. This local limitation does not waive the repository Chromium gate.

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

The repository broad gate always covers these nine top-level routes. A focused task can run one spec during development, but the full browser job remains required before merge.

| Surface | Route | Why it matters |
| --- | --- | --- |
| Dashboard / 工作台 | `/` | Command desk and first daily entry |
| Asset decisions | `/asset-decisions` | Portfolio decision workbench with automatic groups, scenario/manual groups, saved records/readback, and auxiliary single-asset queue |
| VPS inventory | `/vps` | Primary real-data testing entry |
| Monitoring | `/monitoring` | Monitoring instances and runtime evidence for assets |
| Targets | `/targets` | Service / entry observability evidence |
| Events | `/events` | Diagnostic and audit timeline |
| Providers | `/providers` | Provider directory, decision links and wide table scroll ownership |
| Subscriptions | `/subscriptions` | Cost/renewal workbench |
| Settings | `/settings` | Runtime configuration and theme controls |

Detail routes such as `/vps/:vpsId`, `/monitoring/:monitoringInstanceId`, `/targets/:targetId` and archive/IP-quality routes remain task-specific expansion points. Add focused repository/local/staging evidence when their contract changes; do not multiply every detail route into the broad 27-case matrix without a stable reason.

## Recommended viewports

Use at least these when browser sanity is required:

| Viewport | Purpose |
| --- | --- |
| `1440x1000` | Primary desktop workbench |
| `1024x768` | Tablet/narrow desktop transition and tab/toolbar geometry |
| `390x900` | Narrow mobile sanity for text overflow and layout collapse |

For large tables, also check horizontal scroll behavior rather than forcing all columns into mobile width.

## Authenticated staging audit

The release-bound gate is `.github/workflows/frontend-staging-smoke.yml`. It is manual-only and must be dispatched from `main` with exact input `expected_version`. The GitHub `staging` environment must separately restrict deployment branches to `main` and provide:

- variable `HOUFENG_STAGING_BASE_URL` (a non-production staging origin);
- secrets `HOUFENG_STAGING_USERNAME` and `HOUFENG_STAGING_PASSWORD` for a dedicated account;
- non-sensitive, reversible staging data including at least one custom Asset scenario template.

The workflow's secret-free `ref-guard` rejects any ref other than `refs/heads/main` before the environment job is eligible. Runs share concurrency group `frontend-staging-smoke` with `cancel-in-progress: false`; do not change this, because a newer run must never cancel an older run between Settings save and restore.

The smoke checks `/api/healthz.version` before login, authenticates through the visible login form, and separates two evidence lanes:

1. Real environment: nine actual routes, main-document security headers/CSP, cancel-only nested template confirmation, raw retention `snapshot → +1 → save/readback → finally restore/readback`, and theme persistence after reload.
2. Deployed-frontend injection: Dashboard five modes, explicit VPS 503, controlled slow response, and long provider list across all three viewports. These steps prove deployed frontend resilience only.

Staging Playwright disables trace, video and automatic screenshots, places internal output outside the audit path, and uses `preserveOutput: 'never'` so `error-context` snapshots cannot enter the artifact. Explicit screenshots mask login inputs and the user chip. `StagingAudit` stores only allowlisted document headers, sanitized origin-relative paths with query values redacted, method/status/timing, counters and step outcomes; it never stores cookies, Authorization, credentials, request bodies or response bodies.

The always-uploaded artifact is `frontend-staging-audit-<run-id>` from `web/test-results/staging-audit`, retained 30 days. A successful artifact is still not enough to close Gate C: record the workflow run URL/id, artifact name, expected/observed version, commit/tag and conclusion in the Trellis Task 10 and parent task. If the environment, credentials or main-only policy do not exist, keep staging acceptance and Task 10 open; mock results are not a substitute.

## Screenshot and image policy

Do not restore removed historical visual-evidence paths or bulk screenshot manifests. Local screenshots may be useful while discussing UI changes, but they should remain untracked unless the user explicitly approves specific images for public README/docs presentation.

Repository ignore rules block common raster image formats in docs by default. Future approved public images should use the allowlisted `docs/public-assets/` path and be referenced from README or maintained docs. That directory is intentionally not created until there is an approved public asset. SVG is not blocked by this image policy because the project may use SVG as source assets or code-like vector assets.

## Browser sanity checklist

For each checked route:

- First viewport shows the page's primary workflow, not only navigation chrome.
- Text does not overlap or escape buttons, badges, cards, table cells, or sidebars.
- Primary actions and links are visible and named clearly.
- Drawer / modal entry points remain reachable by keyboard and mouse.
- Table-heavy pages still allow scanning; narrow screens may scroll horizontally where appropriate.
- Empty / loading / error states do not create large confusing blank areas.
- Theme tokens are used; no return to removed historical concept-screen or Stitch visual material.

## Additional local automation helpers

External browser tools may be used for one-off detail-route sanity checks or local screenshots, but their output is additive local evidence; the repository's Node Playwright suite remains the formal fixture browser contract.

The repo-local `scripts/visual_evidence.py browser-sanity` command is also a temporary/local evidence helper. It standardizes the geometry checks and output shape, but it remains outside `make verify-web` and CI.

Current repository constraints:

- Do not add a second e2e framework, cross-browser matrix or screenshot-diff stack in ordinary UI tasks; extend the existing Playwright fixtures/support when a stable contract belongs in CI.
- Do not add CI visual regression without a dedicated architecture decision.
- If a local browser automation run depends on machine-specific tools, record it as local evidence and include the limitation.
- If `scripts/visual_evidence.py browser-sanity` is unavailable because Python Playwright is missing, do not automatically mark browser sanity as blocked; first check whether a CDP/Chromium path or another already-installed local browser tool can verify the same route/viewport and selector-level expectations.
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
  - 1024x768
  - 390x900
- Evidence level:
  - Repository Chromium: passed / failed / not relevant
  - Local helper: command + local-only result / not run
  - Authenticated staging: run URL + artifact / blocked by missing environment / not release-bound
  - Local screenshots: none / external attachment only, not committed
- Data source:
  - fail-closed fixture / mock-api helper / local center / staging real lane / staging deployed-frontend injection
- Result:
  - no blank viewport, no text overlap, no support-surface overflow
- Limitations:
  - no tracked screenshot baseline / real data not tested / staging environment unavailable
```

## Relationship to release workflow

This document defines browser evidence through the release-bound staging audit, but it does not define Docker publishing, Release Please or production deployment automation. Follow the repository branch/release governance for those steps.
