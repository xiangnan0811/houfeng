# TargetDetailPage Remaining Section Extraction

## Background

`docs/release/next-phase-plan.md` leaves Stage 2 long-page splitting as deferred technical debt. Recent batches have already extracted `DashboardPage`, `NodesPage`, `TargetsPage`, `VPSDetailPage`, `NodeDetailPage`, and part of `TargetDetailPage`, but `web/src/pages/TargetDetailPage.tsx` remains the largest route page at about 1000 lines.

`web/src/pages/target-detail/` already contains page-private sections for danger state, runtime confirmations, probe management, metadata, lifecycle, history, and snapshot metadata. The route page still owns the final page JSX composition directly, which makes it harder to scan the data/mutation orchestration.

## Goal

Reduce `TargetDetailPage.tsx` complexity by extracting the remaining final route composition into a page-private controlled component under `web/src/pages/target-detail/`, while keeping the page owner responsible for route params, API loading, mutations, polling, probe form state, confirmations, and refresh orchestration.

## Scope

- Add a page-private `TargetDetailPageBody` component in `web/src/pages/target-detail/`.
- Move the final JSX composition from `TargetDetailPage.tsx` into that body component.
- Move only presentational derived values that belong to the body composition, such as danger-zone visibility, archived state, probe action disabled state, and callback wiring needed by child sections.
- Keep `TargetDetailPage.tsx` responsible for:
  - route params and request identity refs;
  - loading target/runtime facts/probe items/incidents/events;
  - runtime actions and confirmations;
  - probe create/edit/delete/toggle actions and confirmation focus restoration;
  - metadata updates;
  - history drawer loading/caching;
  - time-window state.
- Preserve current links, accessible names, text, empty/loading/error states, CSS class names, and test-visible behavior.

## Non-goals

- No visual redesign.
- No backend/API contract changes.
- No Target, ProbeItem, Agent, notification, or Dashboard behavior changes.
- No new shared cross-page abstraction.
- No real VPS data dry-run/import execution.
- No release/publish workflow changes.
- No subagent execution.

## Acceptance Criteria

- `TargetDetailPage.tsx` is materially smaller and delegates final route composition to `TargetDetailPageBody`.
- Extracted component is presentational/controlled and does not call API clients directly.
- Existing target detail runtime control, pause/archive confirmation, metadata edit, probe add/edit/delete/toggle, history drawer, time-window, danger card, and latency trend workflows remain intact.
- Existing `TargetDetailPage` tests pass without weakening assertions.
- `make verify-web` passes locally.
- Work lands through feature branch PR, green PR CI, merge, main CI monitoring, and local `main` sync.

## Verification

- `npm --prefix web run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/TargetDetailPage.test.tsx`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`

## Technical Notes

- Use the recent `NodeDetailPageBody` extraction as the local pattern.
- Do not extract API/effect-heavy logic into a hook in this task; that would blur the page-owner data boundary and increase risk.
- Keep all imports page-private unless a stable cross-page abstraction already exists.
