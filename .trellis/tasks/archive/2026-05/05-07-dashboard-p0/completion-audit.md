# Completion Audit

Date: 2026-05-08
Outcome: close as stale partial and archive.

## Scope Checked

- PRD: `.trellis/tasks/05-07-dashboard-p0/prd.md`
- Likely implementation commit: `d5280a8 feat: polish dashboard, nodes list, and node detail pages (P0)`

## Landed Work

- `DataTable` has sortable column support and sort indicators.
- NodesPage sorts several columns and renders heartbeat/sync freshness in the identity cell.
- Watchtower metric cards calculate priority, render notice/critical ribbons, and sort abnormal metrics first.

## Remaining PRD Items

- The batch bar is still gated by active filters rather than all filtered rows; existing tests intentionally assert no-filter batch actions are hidden.
- Heartbeat time is displayed in the identity cell but is not a separate sortable key.
- The PRD asked for a normal-state group health overview table from `overview.group_summaries`.

## Superseding Evidence

- `.trellis/spec/web/state-and-data.md` now states that Dashboard must not default-expand all `/api/dashboard` contract fields and must not render a `Group 摘要` list on the first screen.
- `docs/design/v2-houfeng/component-spec.md` repeats that `group_summaries` and `recent_events` are secondary detail-entry data, not first-screen lists.

## Decision

Do not continue this stale broad task. The remaining Dashboard group-table acceptance contradicts the current Dashboard information-architecture spec. The batch-bar and heartbeat-sort differences are not blockers for the current product baseline and should only be revived through a new narrow task after an explicit UX/product decision.

Archive to stop this stale task from affecting future development.
