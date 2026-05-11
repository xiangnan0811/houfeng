# Nodes page section extraction

## Goal

Reduce `web/src/pages/NodesPage.tsx` long-file complexity by extracting page-private Nodes list sections and table helpers while preserving API calls, URL filter semantics, labels, copy, CSS classes, row behavior, focus behavior, and tests-visible runtime behavior.

## Context

- `houfeng_codex_下一步开发计划.md` is functionally closed for the current Asset Ledger implementation track; real 40+ VPS data execution remains user-data-dependent and deferred.
- `docs/release/next-phase-plan.md` still tracks Stage 2 long-page file splitting as deferred technical debt.
- `SettingsPage` section extraction is already complete; `NodesPage.tsx` remains one of the largest page files.
- The user explicitly requested no subagents; this task is implemented directly in the main session.

## Requirements

- Create page-private modules under `web/src/pages/nodes/` or an equivalent local directory.
- Extract low-risk Nodes page presentation sections/helpers, targeting:
  - hero inventory summary section;
  - create-node drawer/form;
  - toolbar and compare/action controls;
  - filter bar/chips;
  - batch action panels;
  - table column construction or table cell helpers where practical.
- Keep `NodesPage.tsx` responsible for:
  - loading nodes and sparklines;
  - create-node submission and onboarding token flow;
  - runtime action, metadata edit, batch action, and command side effects;
  - URL search parameter ownership and filter state derivation;
  - sort, compare, and focus restore state.
- Extracted components must be controlled by props and callbacks; they must not call API helpers directly or mutate URL state directly.
- Preserve current behavior:
  - filter URL parameters and chips;
  - row navigation and stop-propagation behavior;
  - table columns and class names;
  - create drawer fields and post-create onboarding navigation;
  - runtime pause confirmation focus restore;
  - batch action confirmation and command panel behavior.
- Do not change backend, API payloads, status/value labels, CSS token system, or release/publish workflow.

## Out of Scope

- Redesigning the Nodes page layout or changing visual styling.
- Changing Nodes API helpers or backend handlers.
- Changing filter semantics, query parameter names, or dashboard deep-link contracts.
- Adding new Node actions, batch actions, or command identities.
- Changing `NodeDetailPage`, `NodeOnboardingPage`, or asset ledger pages.
- Running real VPS data import or release/publish workflow.

## Acceptance Criteria

- [x] `NodesPage.tsx` is materially smaller and more orchestration-focused.
- [x] Extracted Nodes page modules are page-private and controlled by props/callbacks.
- [x] Existing `NodesPage` tests pass without weakening assertions.
- [x] Lint, focused Nodes page tests, build, and `make verify-web` pass locally.
- [ ] Trellis task is archived and journal recorded after the work commit.
- [ ] PR flow is completed: feature branch, PR, green CI, merge, main CI, local `main` synced.

## Verification

- `cd web && npm run lint`
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run test -- --run src/pages/NodesPage.test.tsx`
- `cd web && npm run build`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`

Notes: local Node is v24.14.1 while `web/package.json` declares `22.x`, so `npm ci` prints an engine warning. The full web verification still passes. Vite also prints the existing large chunk warning.
