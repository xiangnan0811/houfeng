# PRD: UX-6C Events Timeline Evidence Convergence

## Summary

EventsPage should become the diagnostic and audit timeline for the v2 observability support system. Nodes and Targets already expose evidence leads and priority evidence cards; Events needs the same evidence language while preserving its existing advanced filter drawer, URL-state contract, and timeline list.

## Goals

- Add a first-screen evidence lead that explains the current event slice from URL filters and loaded events.
- Surface the highest-priority event as the next diagnostic object, with clear object, event type, severity, timestamp, and cross-page route.
- Preserve Dashboard/VPS/Node/Target deep-link handoff:
  - `/events?severity=严重`
  - `/events?time_range=24h`
  - `/events?maintenance_only=1`
  - object-scoped Node/Target event filters.
- Keep filtering truth in URL-state; support clearing active filters from the evidence lead and existing filter overview.
- Keep the Events page as audit and diagnostic evidence, not a new incident management page.

## Non-Goals

- Do not change backend API contracts or add fields to `StateChangeEventRecord`.
- Do not introduce a state/cache library, visual regression framework, or new CSS framework.
- Do not move complete filtering controls out of the existing Drawer.
- Do not infer VPS health, service ownership, notification delivery, or backfill details that `/api/events` does not return.

## Product Behavior

- Default event stream:
  - Lead states that the timeline is stable if there is no critical/alert/notice event in the current loaded slice.
  - Priority focus falls back to a stable card linking to 24h events or the asset decision queue.
- Filtered event stream:
  - Lead shows applied context chips derived from `appliedFilters`.
  - If filtered results are empty, lead gives a clear action to reset filters.
- Priority event selection:
  - Severity order: `严重 > 告警 > 关注 > 正常/empty`.
  - Incident lifecycle events outrank routine runtime events within the same severity.
  - More recent events break ties.
  - Node events route to `/nodes/{object_id}`; Target events route to `/targets/{object_id}`.
- Support lanes remain compact:
  - Current slice, object context, severity/type, time/source.
  - Lanes provide links to Nodes, Targets, Dashboard, VPS, asset decisions, and relevant Events filters.

## Acceptance

- Events first viewport has a diagnostic evidence lead and a priority-event/stable context card.
- Active Dashboard deep-link filters are visible and clearable.
- Empty filtered state has an explicit reset action near the lead, not only in the lower filter section.
- EventList still shows object type, object ID, severity, event type, timestamp, and summary.
- Existing Drawer close/apply/reset behavior remains unchanged.
- Tests cover priority event, filtered empty clear path, stable state, and URL filter handoff.
- UI evidence is recorded under `docs/operations/v2-visual-evidence/` with desktop and mobile screenshots plus manifest rows.

## Verification

- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest npm --prefix web run test -- EventsPage.test.tsx`
- `npm --prefix web run lint`
- `npm --prefix web run build`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/vitest make verify-web`
- Browser sanity and screenshots for `/events` at `1440x1000` and `390x900` using mocked API data.
