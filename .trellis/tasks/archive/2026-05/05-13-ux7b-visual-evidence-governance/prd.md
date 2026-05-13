# UX-7B visual evidence governance

## Goal

Turn the v2 visual evidence process from an informal checklist into a lightweight, repeatable repo-local workflow. Future UI PRs should be able to validate the screenshot manifest, run a browser sanity probe against a local preview, and report evidence in a consistent format without adding a full e2e or visual-regression framework.

## What I already know

* The active UI roadmap is in `docs/release/ui-evolution-roadmap.md`; UX-7 is the current "Design system / evidence / performance hardening" stage.
* UX-7A has already extracted shared observability evidence components and split route bundles through `React.lazy`.
* The active visual workflow is `docs/operations/v2-visual-evidence.md`.
* `docs/operations/v2-visual-evidence/manifest.md` already contains UX-2 through UX-6C rows and screenshot files exist under per-task subdirectories.
* Current repo constraints explicitly say ordinary UI tasks must not add Playwright, Cypress, WebDriverIO, screenshot diffing dependencies, or a new CSS framework.
* User-visible UI tasks already require preview URL, checked routes, checked viewports, and visual evidence limitations in PR/final reports.
* The user asked to continue the UI evolution flow, and the current work must stay in the main session without subagents.

## Problem

The visual evidence process is documented but still too manual:

* Manifest rows can drift from actual screenshot files without a cheap repo-local check.
* The browser sanity checklist is repeatable but not executable, so each UI PR re-implements its own local probe.
* PR authors can report "browser sanity" without a consistent route/viewport/result shape.
* The current workflow does not distinguish enough between committed screenshot evidence and temporary local browser sanity notes.

## Requirements

1. Add a repo-local manifest validation command or script that checks `docs/operations/v2-visual-evidence/manifest.md` against the screenshot files it references.
2. Add a lightweight local browser sanity helper that can check a running preview URL across routes and viewports for nonblank content, page-level horizontal overflow, and basic text overlap risk without adding repository npm dependencies.
3. Keep the helper opt-in and local-only. It must not become part of `make verify-web` or CI in this task.
4. Update `docs/operations/v2-visual-evidence.md` so future UI PRs know when to run the manifest validator, how to run the browser sanity helper, and how to report limitations.
5. Update the UI roadmap to mark UX-7A complete and define UX-7B as the current evidence-governance slice.
6. Update Trellis specs only if the new workflow creates a durable convention future agents need to follow.

## Acceptance Criteria

* [x] `docs/operations/v2-visual-evidence/manifest.md` validates successfully against all currently referenced files.
* [x] A command exists to validate manifest rows and fails with useful messages for malformed rows or missing screenshot files.
* [x] A local browser sanity command exists for a running preview and can be run against at least one route/viewport without repository e2e dependencies.
* [x] Documentation includes exact commands, required preview URL input, evidence output semantics, and local-tool limitations.
* [x] `docs/release/ui-evolution-roadmap.md` reflects UX-7A as completed and UX-7B as the next/current hardening slice.
* [x] The implementation does not add new npm dependencies, Playwright/Cypress/WebDriverIO packages, or CI visual-regression gates.
* [x] Relevant local verification passes.
* [ ] PR CI is green before merge.

## Out of Scope

* Do not add screenshot diffing, pixel baselines, or visual regression CI.
* Do not add Playwright, Cypress, WebDriverIO, or browser automation dependencies to `web/package.json`.
* Do not mark existing screenshot manifest rows as `Accepted`; they remain `Needs review` unless the human reviewer accepts them.
* Do not redesign pages or change user-visible UI in this task.
* Do not run real 40+ VPS import or real-data visual review in this task.

## Technical Notes

* Existing screenshot files live under `docs/operations/v2-visual-evidence/<task>/`.
* Current manifest row format is a Markdown table with columns: `Date`, `Route`, `Viewport`, `Theme`, `Data source`, `File`, `Verdict`, `Notes`.
* The browser sanity helper should prefer system/local tooling where available and clearly fail if the required local browser driver is not installed.
* The repository already uses `npm`; do not introduce `pnpm`.
* Local Vitest on this machine may need repo-local `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest`.

## Verification Plan

* Run the new manifest validator against the current manifest.
* Run the local browser sanity helper against a temporary preview URL for a representative route and both standard viewports when feasible.
* Run any script-specific unit checks if added.
* Run documentation/tooling-focused local checks appropriate to touched files; for web-impacting changes, run `npm --prefix web run lint`, `npm --prefix web run build`, and focused/full Vitest as needed.
* Open PR, monitor `go`, `web`, and GitGuardian checks, merge only after all required checks pass, then sync local `main`.

## Verification Evidence

* `python3 -m py_compile scripts/visual_evidence.py` — pass.
* `make validate-visual-evidence` — pass; current manifest has 34 rows and all referenced files exist.
* `python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /login --viewport 390x900` with default `python3` — exits with a clear local-tooling message because that interpreter does not have Python Playwright installed.
* `TMPDIR=/Users/weibo/Code/houfeng/.tmp/playwright /opt/homebrew/opt/python@3.11/bin/python3.11 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /login --viewport 1440x1000 --viewport 390x900` — pass against `npm --prefix web run preview -- --host 127.0.0.1 --port 5178`; both viewports reported no blank page and no page-level horizontal overflow. Preview server stopped after the check.
* `npm --prefix web run lint` — pass.
* `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- --run` — pass; 63 files / 477 tests.
* `npm --prefix web run build` — pass; no large chunk warning; entry chunk remains `291.72 kB` / `93.20 kB gzip`.
* `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest make verify-web` — pass; local Node v24 emits the expected `EBADENGINE` warning for `web@0.0.0` requiring Node 22.x, then lint/test/build all pass.
