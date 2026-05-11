# EventsPage Section Extraction

## Background

`docs/release/next-phase-plan.md` keeps long-page splitting as Stage 2 technical debt. After recent detail-page extraction batches, `web/src/pages/EventsPage.tsx` is now the largest remaining route page at about 800 lines.

`EventsPage` currently mixes several concerns in one file:

- URL search-param parsing and canonicalization;
- event API query construction and loading state;
- filter drawer form composition;
- active filter chips and overview;
- grouped event stream rendering and load-more controls.

The query parsing/loading logic should remain page-owned, but the filter and event-list presentation can move into page-private modules.

## Goal

Reduce `EventsPage.tsx` complexity by extracting presentational/controlled sections into `web/src/pages/events/`, while preserving all existing EventsPage URL-state, filter, time-range, grouping, loading, and load-more behavior.

## Scope

- Add page-private modules under `web/src/pages/events/` for:
  - active filter overview/chips;
  - filter drawer form;
  - grouped event stream with load-more button.
- Keep `EventsPage.tsx` responsible for:
  - `useSearchParams` and canonical URL state;
  - parsing and normalizing filters;
  - building `EventListFilter` for `listEvents`;
  - event data loading and load-more state;
  - deciding when loading/error states render.
- Export page-private types/constants only when needed by sibling `events/` components.
- Preserve existing text, aria labels, button labels, CSS class names, empty/error/loading states, and test-visible behavior.

## Non-goals

- No backend/API contract changes.
- No filter semantics changes.
- No EventList behavior changes.
- No visual redesign.
- No new global/shared abstraction.
- No real VPS data dry-run/import execution.
- No release/publish workflow changes.
- No subagent execution.

## Acceptance Criteria

- `EventsPage.tsx` is materially smaller and delegates filter/list presentation to page-private modules.
- Extracted components are presentational/controlled and do not call API clients directly.
- Existing event filters, URL query params, time-range tabs, filter chips, reset/apply/close controls, grouping, empty state, and load-more behavior remain intact.
- Existing `EventsPage` tests pass without weakening assertions.
- `make verify-web` passes locally.
- Work lands through feature branch PR, green PR CI, merge, main CI monitoring, and local `main` sync.

## Verification

- `npm --prefix web run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/EventsPage.test.tsx`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`

## Technical Notes

- Keep URL/query helpers in `EventsPage.tsx` unless a helper must be imported by a page-private component.
- Avoid introducing a shared events abstraction in `web/src/components/`; this is page-specific composition debt.
- Use the recent page-private body/section extraction pattern, but prefer smaller section components here because the filter drawer and grouped stream have clearer local boundaries.
