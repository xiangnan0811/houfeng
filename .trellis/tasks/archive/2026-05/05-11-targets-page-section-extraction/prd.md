# TargetsPage Section Extraction

## Background

`web/src/pages/TargetsPage.tsx` is currently the largest route page in the frontend. The V1 and Stage 2 planning documents explicitly defer long-page splitting as technical debt, and recent work has already established page-private extraction patterns for `NodesPage`, `VPSDetailPage`, `NodeDetailPage`, and `TargetDetailPage`.

This task continues that cleanup for `TargetsPage` without changing user-visible behavior.

## Goal

Reduce `TargetsPage.tsx` complexity by extracting page-private types, constants, helpers, table columns, filters, create form, batch panel, and runtime overlays into `web/src/pages/targets/`.

## Scope

- Create a page-private `web/src/pages/targets/` directory.
- Move target list-only type aliases and constants out of `TargetsPage.tsx`.
- Move pure helper functions out of `TargetsPage.tsx`.
- Extract the create target panel/form into a controlled page-private component.
- Extract the target filter bar and active chips into a controlled page-private component.
- Extract the batch action bar and batch pause confirmation into a controlled page-private component.
- Extract target table column construction into a page-private module.
- Extract inline metadata editing/actions/trends cells into page-private components if it keeps column construction readable.
- Extract per-row runtime confirmation/error overlays into a controlled page-private component.
- Keep `TargetsPage.tsx` responsible for API calls, effects, search-param state, navigation, mutation handlers, and focus restoration refs.

## Non-goals

- No visual redesign.
- No API, route, query-param, or backend behavior changes.
- No changes to `TargetsPage.test.tsx` unless refactor-induced accessible names need equivalent updates.
- No new shared cross-page abstraction unless existing duplication is already stable and clearly shared.
- No real-data dry-run/import execution.
- No release/publish workflow changes.
- No subagent execution.

## Acceptance Criteria

- `TargetsPage.tsx` is materially smaller and delegates rendering sections to page-private modules.
- Existing filtering, URL-state, create-target, row click, runtime action, batch action, metadata edit, sparkline, and confirmation/focus behavior is preserved.
- Extracted components are controlled/presentational and do not call API clients.
- Existing `TargetsPage` tests pass.
- `make verify-web` passes locally.
- Work lands through feature branch PR, green PR CI, merge, main CI monitoring, and local `main` sync.

## Verification

- `cd web && npm run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/TargetsPage.test.tsx`
- `cd web && npm run build`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`
