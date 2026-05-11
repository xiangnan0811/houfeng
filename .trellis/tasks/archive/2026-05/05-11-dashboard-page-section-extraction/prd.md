# DashboardPage Section Extraction

## Background

`web/src/pages/DashboardPage.tsx` remains the largest route page after the recent page-section extraction batches. The Stage 2 technical-debt plan explicitly kept long-page splitting as follow-up work, while the current Asset Ledger plan has already closed Tasks 1-8 except for real-data import execution, which is user-data-dependent and deferred.

This task continues the same page-private extraction pattern already used for `SettingsPage`, `NodesPage`, `VPSDetailPage`, `NodeDetailPage`, `TargetDetailPage`, and `TargetsPage`.

## Goal

Reduce `DashboardPage.tsx` complexity by moving dashboard-only presentation sections, derived helpers, and table definitions into `web/src/pages/dashboard/` without changing user-visible behavior or backend contracts.

## Scope

- Create a page-private `web/src/pages/dashboard/` directory if it does not exist.
- Move dashboard-only type aliases, constants, and derived helper functions out of `DashboardPage.tsx`.
- Extract asset summary, key metrics, abnormal nodes, abnormal targets, recent events, command queue, operational context, and management links into controlled page-private components where it improves readability.
- Extract table column builders and row rendering helpers into page-private modules when needed.
- Keep `DashboardPage.tsx` responsible for API loading, refresh/error state, `useNavigate`, and page composition.
- Preserve current links, accessible labels, empty states, loading state, error state, copy, CSS class names, and test-visible text.

## Non-goals

- No visual redesign.
- No dashboard data contract or backend API changes.
- No changes to Asset Ledger real-data dry-run/import execution.
- No release/publish workflow changes.
- No shared cross-page abstraction unless an existing stable shared abstraction already exists.
- No subagent execution.

## Acceptance Criteria

- `DashboardPage.tsx` is materially smaller and delegates dashboard sections to page-private modules.
- Extracted components are presentational/controlled and do not call API clients directly.
- Existing dashboard navigation links, row click behavior, summaries, empty states, and management links are preserved.
- Existing `DashboardPage` tests pass without weakening assertions.
- `make verify-web` passes locally.
- Work lands through feature branch PR, green PR CI, merge, main CI monitoring, and local `main` sync.

## Verification

- `npm --prefix web run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/DashboardPage.test.tsx`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`
