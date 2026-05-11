# NodeDetailPage Remaining Section Extraction

## Background

`web/src/pages/NodeDetailPage.tsx` already has a page-private `web/src/pages/node-detail/` directory, but the route page itself remains above 1000 lines after prior extraction batches. `docs/release/next-phase-plan.md` explicitly lists `NodeDetailPage phase 1.2 剩余 sections` and long-page splitting as Stage 2 deferred technical debt.

The Asset Ledger roadmap tasks are closed except for real-data import execution, which remains user-data-dependent and deferred. This task therefore continues the technical-debt cleanup path without changing product scope.

## Goal

Reduce `NodeDetailPage.tsx` complexity by extracting remaining inline presentation sections, form panels, derived UI fragments, and local helper types into existing page-private modules under `web/src/pages/node-detail/`, while preserving behavior.

## Scope

- Continue using `web/src/pages/node-detail/` as the page-private directory.
- Move remaining inline section/panel JSX out of `NodeDetailPage.tsx` where the boundary is clear.
- Move page-only helper types or derived presentation helpers into `node-detail/types.ts`, `nodeDetailHelpers.ts`, or focused section files as appropriate.
- Keep `NodeDetailPage.tsx` responsible for route params, API loading, mutation handlers, command execution handlers, tab/window state, and refresh orchestration.
- Preserve current links, accessible names, text, empty/loading/error states, CSS class names, and test-visible behavior.

## Non-goals

- No visual redesign.
- No backend/API contract changes.
- No Node runtime, command, Agent, or Docker behavior changes.
- No real-data dry-run/import execution.
- No release/publish workflow changes.
- No shared cross-page abstraction unless an existing stable shared abstraction already exists.
- No subagent execution.

## Acceptance Criteria

- `NodeDetailPage.tsx` is materially smaller and delegates additional sections to page-private modules.
- Extracted modules are presentational/controlled and do not call API clients directly.
- Existing node detail metadata, lifecycle, binding conflict, linked VPS, containers, command, history, timeline window, and danger workflows remain intact.
- Existing `NodeDetailPage` tests pass without weakening assertions.
- `make verify-web` passes locally.
- Work lands through feature branch PR, green PR CI, merge, main CI monitoring, and local `main` sync.

## Verification

- `npm --prefix web run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/NodeDetailPage.test.tsx`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`
