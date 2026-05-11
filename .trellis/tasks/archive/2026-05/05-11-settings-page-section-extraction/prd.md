# Settings page section extraction

## Goal

Reduce `web/src/pages/SettingsPage.tsx` long-file complexity by extracting settings form sections into page-private components while preserving the page API, state shape, labels, visual styling, and runtime behavior.

## Context

- `houfeng_codex_下一步开发计划.md` and release completion docs show the Asset Ledger plan is functionally closed; real 40+ VPS data execution remains user-data-dependent and deferred.
- The remaining non-external work in the prior plan family is Stage 2 technical debt around long page files.
- `SettingsPage.tsx` is over 1,200 lines and mixes state orchestration, API calls, validation, and large repeated section markup.
- The user explicitly requested no subagents; this task is implemented directly in the main session.

## Requirements

- Create page-private settings modules under `web/src/pages/settings/` or an equivalent local directory.
- Extract presentational settings sections from `SettingsPage.tsx`, including:
  - Telegram notification settings.
  - Feishu notification settings.
  - probe frequency defaults.
  - incident defaults.
  - override rules.
  - retention policy.
  - theme settings, if it improves ownership.
- Keep `SettingsPage.tsx` responsible for:
  - loading settings through `getSettings()`;
  - saving through `updateSettings()`;
  - state transitions for loading, errors, and save status;
  - `buildFormState(...)`, `buildUpdateInput(...)`, and validation/parsing logic.
- Extracted components must be controlled by props and callbacks; they must not call the API directly.
- Preserve current labels, helper copy, `aria-*` attributes, CSS class names, field names, tab semantics, and visible behavior.
- Preserve the settings API contract and type shapes.

## Out of Scope

- Changing settings backend endpoints or persistence.
- Changing form validation semantics or accepted payload shapes.
- Redesigning the Settings page visual layout.
- Changing theme token behavior or local theme persistence.
- Running or wiring release/publish workflow; user said this is deferred.
- Real VPS data import or smoke execution.

## Acceptance Criteria

- [x] `SettingsPage.tsx` is materially smaller and focused on orchestration rather than section markup.
- [x] Extracted settings sections live in page-private files and reuse existing atoms/components.
- [x] Existing Settings page tests pass without weakening assertions.
- [x] Lint, targeted Settings page tests, and web build/verification pass locally.
- [ ] Trellis task context is valid and archived after the implementation is committed.
- [ ] Work follows branch/PR flow: feature branch, PR, green CI, merge, local `main` sync.

## Verification

- `cd web && TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run build` — pass, with existing Vite chunk size warning.
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run test -- --run src/pages/SettingsPage.test.tsx` — pass, 1 file / 8 tests.
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run lint` — pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web` — pass, 60 files / 456 tests; local Node v24 prints the expected Node 22.x `EBADENGINE` warning, while CI uses Node 22.
- `git diff --check` — pass.
