# NodesPage List Section Extraction

## Background

`houfeng_codex_下一步开发计划.md` marks the VPS Asset Ledger plan as functionally closed, except the user-data-dependent real VPS dry-run/import execution. The remaining immediate work should therefore avoid expanding asset scope and continue Stage 2 frontend technical debt reduction.

Recent work has already split several long route pages. `web/src/pages/NodesPage.tsx` remains one of the largest route pages at about 760 lines. It already has page-private components under `web/src/pages/nodes/`, but the page still directly composes the list filter panel, batch panel, filtered empty state, data table, and runtime overlays.

## Goal

Reduce `NodesPage.tsx` complexity by extracting the list/table presentation section into page-private modules under `web/src/pages/nodes/`, while preserving all current NodesPage URL filtering, sorting, batch action, compare, runtime control, creation, onboarding, loading, and error behavior.

## Scope

- Add a page-private `NodesListSection` component for the list area after `NodesToolbar`.
- Move JSX composition for:
  - base empty state;
  - `NodesFilterPanel`;
  - `NodesBatchPanel`;
  - filtered empty state;
  - `DataTable`;
  - `NodesRuntimeOverlays`;
  into the new list section.
- Add or extend page-private types only when needed for clean props.
- Keep `NodesPage.tsx` responsible for:
  - API loading and mutation calls;
  - `useSearchParams` ownership and URL filter updates;
  - create drawer and onboarding token flow;
  - runtime and batch action state;
  - sort state and table column construction;
  - row-click navigation decision.
- Preserve existing visible text, CSS class names, aria/test-visible behavior, table columns, sorting semantics, filter semantics, batch behavior, compare behavior, and runtime confirmation behavior.

## Non-goals

- No backend/API changes.
- No new Node list features.
- No filter semantic changes.
- No table column redesign.
- No shared/global component extraction.
- No test weakening.
- No real VPS data dry-run/import execution.
- No release/publish workflow changes.
- No subagent execution.

## Acceptance Criteria

- `NodesPage.tsx` is materially smaller and delegates the list/table JSX to page-private component(s).
- Extracted component(s) are controlled/presentational and do not call API clients directly.
- Existing NodesPage filters, URL search params, view tabs, sort behavior, batch actions, compare selection, create flow, row navigation, and runtime overlays keep working.
- Existing `NodesPage` tests pass without weakening assertions.
- `make verify-web` passes locally.
- Work lands through feature branch PR, green PR CI, merge, main CI monitoring, and local `main` sync.

## Verification

- `npm --prefix web run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/NodesPage.test.tsx`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`

## Technical Notes

- Follow the existing `web/src/pages/nodes/` page-private component pattern.
- Prefer a single `NodesListSection` over many smaller extractions unless implementation reveals a clear need.
- Keep data ownership in `NodesPage`; the extracted section receives already-computed nodes, options, columns, states, and callbacks.
