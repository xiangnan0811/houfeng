# Implementation Plan

## Checklist

- [x] Confirm branch and working tree state.
- [x] Read relevant web styling and quality specs before further edits.
- [x] Review staged diff by risk area:
  - [x] package/build/style entry changes;
  - [x] global CSS and login CSS;
  - [x] Settings page and settings section behavior;
  - [x] page-level import/class changes;
  - [x] tests and generated/unwanted files.
- [x] Run focused tests for CSS contract and Settings page behavior.
- [x] Run local browser sanity for `/login` at `375x667` and `1440x900`.
- [x] Fix any newly discovered issue with a failing test or browser reproduction first.
- [x] Run `make verify-web`.
- [x] Stage any additional fixes.
- [x] Record final evidence in the user-facing response.

## Review Notes

- Additional issue found during retroactive review: discarding an unsaved newly added notification channel reset `form`, but left derived `activeChannels` / `expandedChannels` visible. Fixed by deriving channel state from saved settings on discard and covering the Telegram draft discard path in `SettingsPage.test.tsx`.
- No further deterministic code issues were found in the second staged-diff pass. `ANALYZE` build analysis remains opt-in and normal dev/build paths are not loaded with the visualizer plugin.
- Trellis placeholder manifests were replaced with real spec/doc references so the task records which project rules governed implementation and verification.

## Verification Evidence

- `cd web && npm run test -- --run src/styles/indexCssContract.test.ts`: 1 file / 3 tests passed.
- `cd web && npm run test -- --run src/pages/SettingsPage.test.tsx`: 1 file / 19 tests passed.
- `make verify-web`: passed `npm ci`, ESLint, 74 Vitest files / 578 tests, and `tsc -b && vite build`.
- Environment warning: local Node is `v24.18.0` / npm `11.16.0`; `web/package.json` requires Node `22.x`, so `npm ci` emitted EBADENGINE warning while still exiting 0.
- `/login` local browser sanity via Chromium remote debugging + CDP against `http://127.0.0.1:5185/login`: `375x667` and `1440x900` both rendered nonblank, `flex-direction: column`, footer below card, card/footer within viewport, and no page-level horizontal overflow.
- Repo helper note: `scripts/visual_evidence.py browser-sanity` could not run because local Python Playwright is not installed; Chromium/CDP was used as the documented fallback. Vite logged expected `/api/auth/me` proxy 502 because local center was not running; `/login` geometry evidence remained valid.

## Validation Commands

```bash
cd web && npm run test -- --run src/styles/indexCssContract.test.ts
cd web && npm run test -- --run src/pages/SettingsPage.test.tsx
make verify-web
```

Browser sanity:

```bash
cd web && npm run dev -- --host 127.0.0.1 --port 5185
chromium --remote-debugging-port=9222 --headless=new --no-sandbox --disable-gpu ...
```

Then inspect `/login` geometry via CDP for both target viewports.

## Risk Points

- `web/src/index.css` is large and legacy-like; small selector changes can have wide impact.
- `LoginPage.css` is the only page-local CSS exception and must stay aligned with global/bundled CSS behavior.
- `SettingsPage.test.tsx` has asynchronous save flows; tests must wait for user-visible state rather than rely on timing.
- `make verify-web` runs `npm ci`, so local `node_modules` may be rewritten.

## Out Of Scope

- Commit, push, PR creation, release automation, or main-branch changes.
- Adding a formal browser automation framework.
- Redesigning the broader UI beyond fixing review findings.
