# UX-11 expert UI audit

Read-only design/product audit for UX-11 planning after UX-8/UX-9/UX-10.

## Candidate refinements

1. `/providers`, `/subscriptions`: create/edit still use inline page panels. Refine into compact Drawer/sidecar or clearer “editing X” surfaces with success row feedback.
   - Likely files: `web/src/pages/ProvidersPage.tsx`, `web/src/pages/SubscriptionsPage.tsx`.

2. `/nodes/compare`: missing-ID/loading/error states are plain panels/divs. Promote to `PageState` and strengthen the “A vs B” identity composition.
   - Likely file: `web/src/pages/NodeComparePage.tsx`.

3. `/events`: “默认事件流” is actually unbounded/custom + limit 50. Clarify visible time/limit context and “加载更早事件” result expectation.
   - Likely files: `web/src/pages/EventsPage.tsx`, `web/src/pages/events/EventsFilterOverview.tsx`, `web/src/pages/events/EventsStreamSection.tsx`.

4. `/vps`, `/asset-decisions`: empty table copy is generic one-line. Add route-specific next actions: clear filters, create VPS, add subscription, link Node.
   - Likely files: `web/src/pages/VPSPage.tsx`, `web/src/components/AssetDecisionRenewalTable.tsx`.

5. `/nodes`, `/targets`, `/events`: support-surface copy repeats internal positioning (“不是资产主体”). Tighten to user-facing outcome/action language.
   - Likely files: `web/src/pages/nodes/NodesSupportSurface.tsx`, `web/src/pages/targets/TargetsSupportSurface.tsx`, `web/src/pages/events/EventsSupportSurface.tsx`.

6. `/asset-decisions`: custom clickable queue rows still lack true keyboard row affordance/focus target. Make main body an explicit link or add role/tabIndex/Enter/Space.
   - Likely file: `web/src/pages/AssetDecisionsPage.tsx`.

7. Long create drawers: group essentials/optional/runtime fields and add compact/sticky form footer for 390px comfort.
   - Likely files: `web/src/pages/VPSPage.tsx`, `web/src/pages/targets/CreateTargetPanel.tsx`.

## PRD convergence recommendation

Use these as expert-review candidates, not a mandatory “touch every file” checklist. Prioritize visible product-quality wins that are not repeats of UX-8/9/10:

- PageState / empty/error/success copy and next actions.
- Expert copy cleanup on observability support surfaces.
- Events filter/stream context clarity.
- Keyboard/focus affordance for custom queues only where it does not violate existing nested-interaction guidance.
- Providers/subscriptions inline create/edit surfaces if the implementation can stay restrained.

Avoid expanding into real-data validation, backend contracts, new dependencies, or broad layout redesign.
