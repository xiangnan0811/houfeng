# UX-7E asset workflow visual evidence refresh

## Goal

Make the asset workflow visual evidence path repeatable enough to support the next real-data validation step. `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions` are protected routes that need authentication and center API data before the actual page surfaces render; UX-7E should let a local preview exercise those routes with explicit asset-workflow fixture data, while documenting exactly what that evidence proves and what it does not prove.

## What I already know

- The active UI roadmap recommends UX-7E as "Visual evidence refresh for asset workflows".
- The standard routes for this slice are `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions`.
- The standard viewports are `1440x1000` and `390x900`.
- `docs/operations/v2-visual-evidence.md` allows local browser sanity without committing screenshots when no visual review screenshot set is needed.
- `scripts/visual_evidence.py browser-sanity` already checks nonblank content, page/body horizontal overflow, and leaf text overflow risks.
- The default `python3` on this machine does not have Python Playwright, while `/opt/homebrew/opt/python@3.11/bin/python3.11` does.
- The app shell and asset routes are protected by `AuthProvider` and `RequireAuth`; without `/api/auth/me` and asset API responses, a Vite preview does not render the real Asset Ledger page surfaces.
- The user requires this work to proceed in the main session without subagents.

## Problem

Current browser sanity is useful for public routes such as `/login`, but it is not sufficient for the asset workflow pages:

- Protected routes redirect or withhold rendering until `/api/auth/me` resolves.
- The asset pages need `/api/dashboard`, `/api/vps`, `/api/providers`, and `/api/subscriptions` data to expose the first viewport, filters, chips, tables, drawers, and PageState surfaces that UX-7E is supposed to check.
- If evidence is collected against an unavailable local center, the result says more about environment setup than about frontend layout quality.
- If evidence uses mocked data, the PR must clearly distinguish mocked layout evidence from real 40+ VPS validation.

## Requirements

1. Add a local-only browser sanity mode that can render the four protected asset workflow routes with explicit mock API responses.
2. Keep the mode outside repo npm dependencies, outside CI, and outside `make verify-web`.
3. Preserve the existing browser-sanity behavior by default; mock data must be opt-in.
4. The mock data should cover the important visible states: renewal due, unreviewed/migrate/cancel decisions, missing subscription, unlinked VPS, missing facts, provider ratings/labels, subscription filters, and dashboard shell summary.
5. Document exact commands for the mocked asset workflow check and the evidence semantics.
6. Record UX-7E evidence and limitations in a persistent operations document.
7. Prepare a route/data checklist that separates `mock-api`, `local center`, and `real data` evidence for the future 40+ VPS validation step.
8. Update the UI roadmap when UX-7E is completed and name the next sensible step.

## Acceptance criteria

- [x] `scripts/visual_evidence.py browser-sanity` still works in its existing mode.
- [x] A documented opt-in command can run browser sanity for `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions` at `1440x1000` and `390x900` using asset workflow mock API data.
- [x] The command renders protected asset pages, not just `/login` or an auth blank state.
- [x] The browser sanity output passes page/body horizontal overflow checks for all standard route/viewport pairs.
- [x] Any text overflow warnings are either fixed or explicitly recorded with route/viewport evidence.
- [x] Documentation explains that mock API evidence validates frontend layout and interaction surfaces, not backend correctness or real inventory truth.
- [x] Documentation includes a future checklist for mocked/local-center/real-data validation, including what needs to be checked before importing or manually entering 40+ VPS records.
- [x] No Playwright/Cypress/WebDriverIO package is added to `web/package.json`.
- [x] Relevant local verification passes.
- [ ] PR CI is green before merge.

## Out of scope

- Do not run a real 40+ VPS import in this task.
- Do not introduce screenshot diffing, visual regression CI, or a browser automation framework dependency.
- Do not redesign Asset Ledger information architecture.
- Do not add backend contracts or change asset API response shapes.
- Do not treat mocked evidence as proof that real provider/account data is complete.
- Do not use subagents.

## Technical notes

- Auth success is enough for `RequireAuth`; `/api/auth/me` can return a fixture user.
- AppShell uses `/api/dashboard` for the sidebar summary; a minimal but shape-complete dashboard fixture avoids false "summary unavailable" states during visual checks.
- Asset pages call `listVPSAssets()`, `listProviders()`, and `listSubscriptions()` with query variants. The browser helper can intercept these route requests and filter fixture rows according to query parameters.
- The visual evidence helper should use typed plain Python data structures and standard-library URL parsing; it must remain dependency-light.
- If the local Python Playwright runtime is missing, the helper should keep returning the existing clear local-tooling message.

## Verification plan

- Run `python3 -m py_compile scripts/visual_evidence.py`.
- Run existing manifest validation if the visual evidence workflow docs or manifest are affected.
- Start a local Vite preview or dev server and run browser sanity with the asset workflow mock API mode across all standard routes and viewports.
- Run `npm --prefix web run lint`.
- Run `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run`.
- Run `npm --prefix web run build`.
- Run `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest make verify-web` if the final change set includes frontend-impacting files.
- Open PR, monitor `go`, `web`, and GitGuardian checks, merge only after all required checks pass, then sync local `main`.

## Verification evidence

- `python3 -m py_compile scripts/visual_evidence.py` — pass.
- `make validate-visual-evidence` — pass; current manifest has 34 rows.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /login --viewport 1440x1000 --viewport 390x900` — pass; default non-mock behavior still renders `/login`.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --mock-api asset-workflows --route /asset-decisions --route /vps --route /providers --route /subscriptions --viewport 1440x1000 --viewport 390x900` — pass; all 8 route/viewport pairs reported no blank page and no page-level horizontal overflow.
- `npm --prefix web run lint` — pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run` — pass; 64 files / 485 tests.
- `npm --prefix web run build` — pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest make verify-web` — pass; local Node v24 emits the known `EBADENGINE` warning because `web/package.json` requires Node 22.x, then lint/test/build pass.
