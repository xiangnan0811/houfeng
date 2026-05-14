# v2 visual evidence and preview workflow

> Status: active workflow for v2-houfeng UI acceptance.
>
> Visual authority: `docs/design/v2-houfeng/design-language.md` and `docs/design/v2-houfeng/component-spec.md`.
>
> Scope: local preview, browser sanity, and screenshot evidence for user-visible frontend work. This does not replace automated lint/test/build checks.

## Why this exists

The archived V1 visual verification flow was tied to v1-baseline Stitch screenshots. The active product direction has moved to v2-houfeng and the core page UX replan. Future UI work needs a repeatable way to answer:

- Which local URL can the reviewer open?
- Which routes and viewports were checked?
- Was the check automated browser sanity, screenshot capture, or manual inspection?
- What evidence exists, and what remains a human judgment?

## Evidence levels

Use the lowest level that honestly proves the change.

| Level | Required when | Evidence |
| --- | --- | --- |
| Preview URL | Any user-visible UI change | Running dev server URL and route list in the final report / PR body |
| Browser sanity | Core page UX, layout, responsive, or route-level interaction changes | Notes from desktop and mobile checks: routes, viewport sizes, key selectors, overflow / overlap result |
| Screenshot evidence | First-viewport structure, page hierarchy, theme, or cross-page UX materially changes | Saved screenshots plus a manifest row that records route, viewport, data source, theme, and reviewer notes |
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

Every UI task final report must include:

- preview URL;
- routes checked;
- viewports checked;
- whether the server is still running or has been stopped;
- any blocked evidence, with reason.

## Repo-local evidence helpers

These helpers make evidence easier to repeat, but they still do not replace reviewer judgment.

Validate committed screenshot evidence before submitting a UI PR that adds or edits manifest rows:

```bash
make validate-visual-evidence
```

Equivalent direct command:

```bash
python3 scripts/visual_evidence.py validate-manifest
```

The validator checks the manifest table shape, route/date/viewport formatting, supported verdict values, duplicate `File` entries, and whether every referenced screenshot file exists under `docs/operations/v2-visual-evidence/`.

For browser sanity against a running preview, use the local-only helper:

```bash
python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --route /nodes \
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

`--mock-api asset-workflows` intercepts `/api/auth/me`, `/api/dashboard`, `/api/providers`, `/api/vps`, and `/api/subscriptions` in the browser session. The fixture rows intentionally cover renewal due, unreviewed/migrate/cancel decisions, missing subscription, unlinked VPS, missing facts, provider labels/ratings, subscription filters, and shell summary state.

Report this as `Data source: mock-api asset-workflows`. It proves the protected route layout can render with representative asset workflow states, but it does **not** prove backend correctness, real account completeness, import fidelity, or the real 40+ VPS inventory result.

If local Playwright cannot create browser temp files, prefer a repo-local temp directory (`TMPDIR="$PWD/.tmp/playwright"`) and record that in the evidence notes. Do not add browser automation dependencies to `web/package.json` to work around local tooling.

## Core route matrix

This is the current v2 core-page acceptance set. A task only needs to check routes it changes, but broad UX tasks should cover the full relevant subset.

| Surface | Route | Why it matters | Current evidence |
| --- | --- | --- | --- |
| Dashboard / 工作台 | `/` | Command desk and first daily entry | Historical one-time screenshot: `docs/operations/Dashboard.jpg`; UX-2 changed it afterward, so new broad evidence should recapture |
| Asset decisions | `/asset-decisions` | Main asset work queue | No committed screenshot yet |
| VPS inventory | `/vps` | Primary real-data testing entry | No committed screenshot yet |
| VPS detail | `/vps/:vpsId` | Single-asset decision workbench | No committed screenshot yet |
| Nodes | `/nodes` | Observability evidence for assets | Historical one-time screenshot: `docs/operations/节点列表页面.jpg`; UX-5 changed it afterward |
| Node detail | `/nodes/:nodeId` | Runtime evidence and watchtower details | Historical one-time screenshot: `docs/operations/节点详情页面.jpg` |
| Targets | `/targets` | Service / entry observability evidence | Historical one-time screenshot: `docs/operations/目标列表页面.jpg`; UX-5 changed it afterward |
| Target detail | `/targets/:targetId` | ProbeItem and entry detail workflow | Historical one-time screenshot: `docs/operations/目标详情页面.jpg` |
| Events | `/events` | Diagnostic and audit timeline | No current v2 screenshot after the advanced-filter and UX-5 changes |
| Settings | `/settings` | Runtime configuration and theme controls | No current v2 screenshot |

## Recommended viewports

Use at least these when browser sanity is required:

| Viewport | Purpose |
| --- | --- |
| `1440x1000` | Primary desktop workbench |
| `390x900` | Narrow mobile sanity for text overflow and layout collapse |

For large tables, also check horizontal scroll behavior rather than forcing all columns into mobile width.

## Screenshot storage

Do not reuse the archived `docs/operations/visual-evidence/` path. If screenshots are committed for v2, use:

```text
docs/operations/v2-visual-evidence/
```

Suggested layout:

```text
docs/operations/v2-visual-evidence/
  manifest.md
  2026-05-12-dashboard-1440-dark.png
  2026-05-12-vps-1440-dark.png
```

Use lowercase ASCII filenames for new screenshots. Existing historical JPEG names under `docs/operations/*.jpg` are kept as-is for traceability.

Manifest row format:

```markdown
| Date | Route | Viewport | Theme | Data source | File | Verdict | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-05-12 | `/vps` | 1440x1000 | houfeng-dark | mocked API / local center / real data | `2026-05-12-vps-1440-dark.png` | Needs review / Accepted | ... |
```

Do not mark screenshots as accepted unless a human reviewer has accepted the visual result or the user explicitly says it is acceptable.

When screenshot rows are added or changed, run `make validate-visual-evidence` and include the result in the PR or final report. Local browser sanity without committed screenshots does not require manifest rows.

## Browser sanity checklist

For each checked route:

- First viewport shows the page's primary workflow, not only navigation chrome.
- Text does not overlap or escape buttons, badges, cards, table cells, or sidebars.
- Primary actions and links are visible and named clearly.
- Drawer / modal entry points remain reachable by keyboard and mouse.
- Table-heavy pages still allow scanning; narrow screens may scroll horizontally where appropriate.
- Empty / loading / error states do not create large confusing blank areas.
- Theme tokens are used; no return to archived v1/stitch visual direction.

## Temporary automation helpers

External browser tools such as a locally installed Playwright CLI may be used for one-off sanity checks or screenshots, but they are not part of the repository contract unless a separate task intentionally introduces browser automation.

The repo-local `scripts/visual_evidence.py browser-sanity` command is also a temporary/local evidence helper. It standardizes the geometry checks and output shape, but it remains outside `make verify-web` and CI.

Current repository constraints:

- Do not add Playwright, Cypress, WebDriverIO, or screenshot diffing dependencies in ordinary UI tasks.
- Do not add CI visual regression without a dedicated architecture decision.
- If a local browser automation run depends on machine-specific tools, record it as local evidence and include the limitation.

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
  - Browser sanity
  - Screenshot evidence: none / `docs/operations/v2-visual-evidence/...`
- Data source:
  - mocked API / mock-api asset-workflows / local center / real data
- Result:
  - no blank viewport, no text overlap, no support-surface overflow
- Limitations:
  - no screenshot committed / real data not tested / browser automation was local only
```

## Relationship to release workflow

This document covers UI preview and visual evidence only. It does not define release/publish workflow, Docker publishing, Release Please, or production deployment automation.
