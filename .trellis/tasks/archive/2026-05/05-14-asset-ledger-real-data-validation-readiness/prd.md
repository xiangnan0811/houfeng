# Asset Ledger real-data validation readiness

## Goal

Bridge the gap between UX-7E's protected-route mock evidence and the next local-center / real-inventory validation step. The outcome should make it practical to validate Asset Ledger pages against a real `houfeng-center` login and a non-sensitive local sample, then move to the user's real 40+ VPS data only after privacy review and explicit authorization.

## Context

- UX-7E added `scripts/visual_evidence.py browser-sanity --mock-api asset-workflows`, which proves protected Asset Ledger route layout with representative fixture data.
- That evidence is intentionally not backend, auth-cookie, import-fidelity, or real-inventory proof.
- The active UI roadmap now recommends `Asset Ledger real-data validation readiness` instead of more visual-only polish.
- The existing VPS JSON import command already supports dry-run and explicit import, but the route from sample JSON to local center browser evidence is not documented as one repeatable workflow.
- The user asked to continue in the main session without subagents.

## Problem

The project is ready to start true data validation, but there is still a process gap:

- Browser sanity can render mocked protected routes, but cannot currently authenticate through the real center session flow from the helper.
- Operators need a safe, non-sensitive sample path before touching real account inventory.
- Real VPS data needs a privacy and field-mapping checklist so secrets, SSH keys, tokens, account metadata, and unrelated personal data do not enter the repo or evidence output.
- The existing docs mention local center and import separately; reviewers need one concrete path that ties import dry-run/import, center auth, routes, viewports, data-source labeling, and limits together.

## Requirements

1. Extend repo-local browser sanity so it can authenticate against a running center using the real `/api/auth/login` flow.
2. Keep the default browser-sanity behavior unchanged; real login must be opt-in and must not be mixed with `--mock-api asset-workflows`.
3. Avoid leaking passwords in recommended commands by supporting environment-backed login credentials.
4. Keep browser tooling local-only; do not add Playwright/Cypress/WebDriverIO or npm browser automation dependencies.
5. Provide a safe local Asset Ledger sample JSON that exercises provider creation, VPS creation, subscriptions, renewal-window candidates, missing subscription, missing facts, unlinked/node-candidate hints, and idle paid signals.
6. Document the repeatable local center sample workflow:
   - start local Postgres/center;
   - run import dry-run;
   - optionally import into the local database;
   - run authenticated browser sanity for `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions`;
   - label evidence as `local center sample`.
7. Document the real 40+ VPS readiness checklist:
   - privacy redaction;
   - field mapping;
   - dry-run review;
   - import/manual-entry decision;
   - evidence capture expectations and limits.
8. Update the roadmap so the next step after readiness is clear.

## Acceptance Criteria

- [x] `scripts/visual_evidence.py browser-sanity` still passes in its existing public-route mode.
- [x] `browser-sanity` supports opt-in center login through username/password arguments and/or environment-backed credentials.
- [x] `browser-sanity` refuses ambiguous credential states and refuses real login together with `--mock-api`.
- [x] Authenticated browser sanity records evidence provenance in command output without printing secrets.
- [x] Authenticated protected-route checks fail if the final browser path is unexpectedly redirected away from the requested route.
- [x] A committed non-sensitive sample JSON dry-runs successfully through `go run ./cmd/houfeng-import-vps-json -dry-run`.
- [x] Documentation clearly separates `mock-api asset-workflows`, `local center sample`, and `real data`.
- [x] Documentation explicitly states that real inventory execution still requires a user-provided/authorized data source.
- [x] No browser automation dependency is added to `web/package.json`.
- [x] Relevant local verification passes.
- [ ] PR CI is green before merge.

## Out of Scope

- Do not import or commit the user's real 40+ VPS inventory.
- Do not add new backend API fields, migrations, dashboard facts, Provider sync, DNS sync, Web SSH, exchange rates, or scoring algorithms.
- Do not redesign Asset Ledger pages in this task.
- Do not add screenshot diffing, visual-regression CI, or browser automation package dependencies.
- Do not treat sample or mock evidence as proof that production provider/account data is complete.
- Do not use subagents.

## Technical Notes

- Use the existing `cmd/houfeng-import-vps-json` and `internal/center/importing` path rather than creating a second import mechanism.
- Login should target `/api/auth/login` and rely on the browser context cookie jar before navigating protected routes.
- Passwords should be available via an environment variable option so recommended commands avoid putting the secret value directly in the command line.
- Browser sanity currently checks nonblank body, page/body horizontal overflow, and leaf-text overflow warnings. Real-login mode should preserve those checks and add final-route verification for protected route evidence.
- Static sample dates are acceptable for an operational sample file, but docs must warn reviewers to adjust renewal dates if they need a fresh renewal-window UI check in a later calendar period.

## Verification Plan

- `python3 -m py_compile scripts/visual_evidence.py`
- `python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /vps --mock-api asset-workflows --login-username admin --login-password-env HOUFENG_INITIAL_PASSWORD` should fail fast with a configuration error when the env var is present.
- `python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /vps --login-username admin` should fail fast with a credential-pair error.
- `go run ./cmd/houfeng-import-vps-json -file docs/operations/asset-ledger-local-sample.json -dry-run -format json`
- `make validate-visual-evidence`
- Start a local web preview/dev server and rerun browser sanity for `/login`; if a local center/Postgres is available, additionally run authenticated protected-route browser sanity and record it as local-only evidence.
- Run `npm --prefix web run lint`, `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run`, and `npm --prefix web run build` if helper/docs changes are treated as frontend-impacting for the PR.

## Verification Evidence

- `python3 -m py_compile scripts/visual_evidence.py` - pass.
- `python3 -m json.tool docs/operations/asset-ledger-local-sample.json` - pass.
- `HOUFENG_INITIAL_PASSWORD=sample python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /vps --mock-api asset-workflows --login-username admin --login-password-env HOUFENG_INITIAL_PASSWORD` - exits 2 with the expected `real login cannot be combined with --mock-api` configuration error.
- `python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /vps --login-username admin` - exits 2 with the expected credential-pair configuration error.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go run ./cmd/houfeng-import-vps-json -file docs/operations/asset-ledger-local-sample.json -dry-run -format json` - pass; `can_import=true`, 5 input rows, 4 providers, 5 VPS candidates, 4 subscription candidates, 2 renewal candidates, 1 idle paid candidate, 0 validation errors.
- `make validate-visual-evidence` - pass; manifest has 34 rows.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /login --viewport 1440x1000 --viewport 390x900` - pass against local Vite dev server.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --mock-api asset-workflows --route /asset-decisions --route /vps --route /providers --route /subscriptions --viewport 1440x1000 --viewport 390x900` - pass against local Vite dev server.
- `npm --prefix web run lint` - pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run` - pass; 64 files / 485 tests.
- `npm --prefix web run build` - pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest make verify-web` - pass; local Node v24 emits the known `EBADENGINE` warning because `web/package.json` requires Node 22.x, then lint/test/build pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build make verify-go` - pass.
