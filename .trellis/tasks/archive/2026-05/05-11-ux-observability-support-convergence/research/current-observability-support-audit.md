# Current observability support audit

## Scope

Reviewed current UX-5 targets and supporting contracts:

- `docs/release/core-pages-product-ux-replan.md`
- `docs/design/v2-houfeng/component-spec.md`
- `web/src/pages/NodesPage.tsx`
- `web/src/pages/nodes/*`
- `web/src/pages/TargetsPage.tsx`
- `web/src/pages/targets/*`
- `web/src/pages/EventsPage.tsx`
- `web/src/pages/events/*`
- `web/src/components/EventList.tsx`
- `web/src/lib/api.ts`
- `web/src/lib/types.ts`
- `.trellis/spec/web/*`

## Findings

### NodesPage

- Functionality is already strong: compact DataTable, node view tabs, URL-state filters, sparklines, create drawer, batch actions, runtime controls and row click to detail.
- The current hero says "节点列表" and emphasizes health, onboarding and runtime facts. It does not yet make the asset workflow role explicit.
- Node Detail can lazy-load linked VPS through `listVPSForNode()`, but the list API does not provide per-row linked VPS details. The list should link users toward VPS asset reconciliation instead of faking row-level asset facts.
- The filter bar is functionally correct and must keep Dashboard deep links such as `onboarding=pending` and `abnormal=1`.

### TargetsPage

- Functionality is already strong: create target, URL-state filters, compact DataTable, execution labels, trends, runtime controls and target detail navigation.
- The page still reads as a target resource table. It does not explain that targets are service/entry observability evidence and not the service registry itself.
- Existing target records include execution node labels and observation recency, which are enough to create a support summary without backend changes.
- There is no direct VPS service relationship in the target list contract. UX should provide navigation to the VPS/service context without implying an automatic registry.

### EventsPage

- Functionality is already strong: URL is the applied filter source of truth, Drawer uses draft filters, chips are removable, relative time windows are preserved in URL and translated to API dates, events are grouped by date.
- The page currently opens with a generic "事件" panel. It does not surface whether the user came from Dashboard/VPS/Node/Target-style deep links or which diagnostic context is active.
- `EventList` clearly shows event type, summary, severity, object type, object ID and time. That satisfies the default fact display requirement; the page wrapper needs stronger diagnostic framing.

## Implementation Recommendation

1. Add a compact evidence/support strip after each page hero and before heavy controls.
2. Keep the support strip navigational and derived from already-loaded page state. Do not add backend calls.
3. For Nodes, make the strip answer: which nodes need attention, which are blocked in onboarding/binding, which are in maintenance/pause, where to reconcile VPS links.
4. For Targets, make the strip answer: which service entries are unhealthy, which are paused/archived, whether execution node coverage exists, where to inspect VPS-scoped services.
5. For Events, make the strip answer: what event slice is being viewed, how many events are loaded, what object/severity/time filters are active and where the stream commonly comes from.
6. Update docs and tests around product behavior rather than CSS details.

## Constraints

- No subagents are used for this task per user instruction.
- No real-data import.
- No backend API additions.
- No N+1 linked VPS fetches from list rows.
- No invented linked node or linked VPS health outside existing contracts.
- CSS stays in `web/src/styles/pages.css` with token/BEM style.
