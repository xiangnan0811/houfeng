# Node detail page section extraction

## Goal

Reduce `web/src/pages/NodeDetailPage.tsx` long-file complexity by extracting page-private presentation sections, drawers, table column helpers, constants, and pure helpers while preserving the current node detail behavior, API flow, user-visible copy, CSS classes, tests-visible request order, and interaction semantics.

## Context

- `houfeng_codex_下一步开发计划.md` has moved into deferred technical-debt cleanup after the user explicitly deferred real-data work and release/publish workflow.
- Recent long-page extraction tasks already completed `SettingsPage`, `NodesPage`, and `VPSDetailPage`; those tasks created page-private directories under `web/src/pages/settings/`, `web/src/pages/nodes/`, and `web/src/pages/vps-detail/`.
- `NodeDetailPage.tsx` is now the clearest remaining long-page frontend debt at roughly 1600 lines, and `.trellis/spec/web/component-conventions.md` explicitly lists it as an accepted gap to pay down.
- The user explicitly requested no subagents; this task is implemented and checked directly in the main session.

## Requirements

- Create page-private modules under `web/src/pages/node-detail/`.
- Extract low-risk Node detail presentation and support code, targeting:
  - page state/types, constants, command option labels, and pure helper functions;
  - loading/unavailable panels;
  - diagnosis summary;
  - linked VPS Asset Ledger section and VPS table columns;
  - binding conflict section;
  - current problem warning card;
  - runtime pause confirmation card;
  - lifecycle controls section;
  - access credential status section;
  - container table section;
  - snapshot metadata;
  - history drawer;
  - command drawer and command result.
- Keep `NodeDetailPage.tsx` responsible for:
  - route parameter handling;
  - loading node details, runtime facts, linked VPS records, binding conflict onboarding state, activity, and historical incidents;
  - polling pending command results;
  - all API calls and request ordering;
  - all submit handlers and optimistic/stale-route guards;
  - local UI state ownership, refs, focus restoration, and lazy-load triggers.
- Extracted components must be controlled by props and callbacks; they must not call API helpers directly or mutate route state directly.
- Preserve current behavior:
  - initial node/runtime request order and activity request order;
  - linked VPS lazy loading through the existing intersection observer section ref;
  - binding conflict labels, action disabling, loading/error states, and onboarding link;
  - runtime pause confirmation copy and focus restoration behavior;
  - metadata edit/save behavior;
  - lifecycle retire/restore copy and error handling;
  - container table status glyphs and empty states;
  - history drawer tabs, lazy historical incident loading, retry behavior, and empty/error copy;
  - command drawer disabled state, command list, pending/completed result rendering, stdout truncation behavior, and polling behavior;
  - CSS class names, Chinese UI copy, links, labels, badges, and test assertions.
- Do not change backend, API payloads, state labels, monitoring semantics, Asset Ledger model, real-data dry-run/import behavior, release/publish workflow, or top-level route structure.

## Out of Scope

- Redesigning the Node detail page layout or changing visual styling.
- Adding or changing node/backend/API functionality.
- Moving existing shared `components/node-detail/*` components.
- Extracting route/stateful custom hooks.
- Running real VPS data dry-run/import.
- Release/publish workflow.

## Acceptance Criteria

- [x] `NodeDetailPage.tsx` is materially smaller and more orchestration-focused.
- [x] Extracted Node detail modules are page-private and controlled by props/callbacks.
- [x] Existing `NodeDetailPage` tests pass without weakening assertions.
- [x] Lint, focused Node detail tests, build, and `make verify-web` pass locally.
- [ ] Trellis task is archived and journal recorded after the work commit.
- [ ] PR flow is completed: feature branch, PR, green CI, merge, main CI, local `main` synced.

## Verification

- `npm --prefix web run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/NodeDetailPage.test.tsx --reporter=verbose`
- `npm --prefix web run build`
- `make verify-web` — 60 test files, 456 tests passed; build passed. Local npm reported an engine warning because this machine uses Node v24.14.1 while `web/package.json` declares Node 22.x. Vite also emitted the existing chunk-size warning for the main bundle.

## Spec Update Review

No `.trellis/spec/` update was needed. This task applied the existing page-private component extraction pattern already documented in the web component/directory specs; it did not change API signatures, payloads, request ordering contracts, validation semantics, route structure, or project conventions.
