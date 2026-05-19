# Research: overview and list page information architecture audit

- **Query**: Audit current IA issues for overview/list pages using the recent Node Detail and VPS Detail refactors plus v2 design guidance as references.
- **Scope**: internal/static code and spec research
- **Date**: 2026-05-19

## Findings

### Files Reviewed

- `web/src/pages/DashboardPage.tsx`
- `web/src/pages/dashboard/*`
- `web/src/pages/NodesPage.tsx`
- `web/src/pages/nodes/*`
- `web/src/pages/TargetsPage.tsx`
- `web/src/pages/targets/*`
- `web/src/pages/VPSPage.tsx`
- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/pages/SubscriptionsPage.tsx`
- `web/src/pages/ProvidersPage.tsx`
- `web/src/pages/EventsPage.tsx`
- `web/src/pages/events/*`
- `web/src/pages/NodeDetailPage.tsx`
- `web/src/pages/node-detail/NodeDetailPageBody.tsx`
- `web/src/pages/VPSDetailPage.tsx`
- `web/src/pages/vps-detail/VPSDetailHero.tsx`
- `web/src/pages/vps-detail/VPSDecisionWorkbench.tsx`
- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`
- `.trellis/spec/web/component-conventions.md`
- `.trellis/spec/web/styling-guidelines.md`
- `.trellis/spec/web/directory-structure.md`
- `.trellis/spec/web/quality-guidelines.md`

### Overall Priority Recommendation

1. `SubscriptionsPage` — highest MVP priority.
2. `ProvidersPage` — quick low-risk cleanup after subscriptions.
3. `TargetsPage` — medium priority; more operationally important but higher interaction risk.
4. `NodesPage` — medium/lower; already partly aligned, but dense.
5. `AssetDecisionsPage` — medium; important workflow, but already follows queue/workbench pattern.
6. `EventsPage` — low/medium; already aligned, URL-state risk.
7. `DashboardPage` — low; already command-surface-first.
8. `VPSPage` — low; already strong reference implementation.

## Page-by-page Audit

### DashboardPage

- **Purpose**: Main operational dashboard answering what to handle today.
- **Current IA**: Already command-surface-first, rendering `DashboardCommandSurface` then `DashboardWorkbench`.
- **Likely issues**: Lower workbench can still feel visually heavy, but major IA work risks undoing tested command-surface behavior.
- **Quick wins**: Treat as reference; avoid adding more sections; preserve deep links and command lanes.
- **Risk**: Medium.
- **MVP priority**: Low.

### NodesPage

- **Purpose**: Node list and observability evidence page; runtime evidence supporting VPS asset decisions.
- **Current IA**: Hero, support surface, create drawer, toolbar/filter/list sections, runtime overlays.
- **Likely issues**: High first-screen density; possible duplication between hero and support surface; many control bands before the table.
- **Quick wins**: Avoid large rewrite; preserve URL filters, onboarding, runtime confirmations, DataTable row-click isolation, create drawer.
- **Risk**: Medium/high.
- **MVP priority**: Medium/lower.

### TargetsPage

- **Purpose**: Service-entry observability list page for Targets and ProbeItems.
- **Current IA**: Hero, create target drawer, support surface, filter panel, batch controls, table, runtime overlays.
- **Likely issues**: Heavy stack of support surface, filters, batch controls, table actions, runtime overlays; primary scanning path can feel busy.
- **Quick wins**: Preserve create drawer and support surface; focus future work on reducing control-band weight around filters/batch controls.
- **Risk**: Medium/high.
- **MVP priority**: Medium.

### VPSPage

- **Purpose**: VPS inventory/reconciliation page.
- **Current IA**: Already strong inventory-command pattern: quick views, evidence strip, filter summary, advanced filter drawer, DataTable, create drawer.
- **Likely issues**: No major IA gap. Main risk is weakening subscription-evidence semantics.
- **Quick wins**: Use as a reference for Subscriptions/Providers.
- **Risk**: Low/medium.
- **MVP priority**: Low.

### AssetDecisionsPage

- **Purpose**: Asset Ledger renewal/migration/cancellation decision queue.
- **Current IA**: Unified queue board, focus cards, ordered decision queue, secondary renewal evidence table, drawer work panel.
- **Likely issues**: Secondary `RENEWAL EVIDENCE` may visually compete with the primary queue; local queue state is not URL-shareable.
- **Quick wins**: Avoid major restructuring; preserve unified queue, drawer decision update, subscription reload behavior, row click/action isolation.
- **Risk**: Medium.
- **MVP priority**: Medium.

### SubscriptionsPage

- **Purpose**: Subscription CRUD/list page for VPS renewal and cost evidence.
- **Current IA**: URL filters and URL-driven create (`create=1`, optional `vps_id`); create/edit forms are inline full-width page panels.
- **Likely issues**: Clearest remaining IA gap. Inline create/edit forms push filters/table down and interrupt list scanning; page lacks compact renewal/cost evidence strip comparable to VPSPage/AssetDecisions/VPSDetail.
- **Quick wins**: Move create/edit into Drawer; preserve URL-driven create behavior and URL cleanup; add compact evidence strip for upcoming renewals, active/manual/auto-renew signals, cancelled/expired records, current filtered count; keep URL filters and DataTable as main path.
- **Risk**: Medium/high due URL-driven create, create/update payloads, monthly price display, tests.
- **MVP priority**: High.

### ProvidersPage

- **Purpose**: Provider master-data CRUD/list page.
- **Current IA**: Plain CRUD list; create/edit are inline full-width panels.
- **Likely issues**: Older than drawer/table-first pages; inline forms interrupt scanning; lacks lightweight context summary.
- **Quick wins**: Move create/edit into Drawer; keep scope small; optionally add provider count/country/account/low-rated/label summary.
- **Risk**: Low.
- **MVP priority**: Medium-low / quick win after Subscriptions.

### EventsPage

- **Purpose**: Audit and diagnostic event timeline, with URL as filter source of truth.
- **Current IA**: Hero, diagnostic support surface, filter overview, advanced filter drawer, grouped event stream.
- **Likely issues**: Stacked surfaces before stream can feel heavy, but this is mostly intentional for a diagnostic page.
- **Quick wins**: Low-priority polish only; preserve URL filters, draft/applied drawer separation, grouped stream, deep links.
- **Risk**: Medium.
- **MVP priority**: Low/medium.

## Reference Patterns from Recent Detail Refactors

### Node Detail

Useful pattern: top watchtower header, urgent conditional surfaces, time-window tabs, runtime metrics, linked VPS evidence, drawers for history/commands. Principle for list pages: urgent evidence comes before secondary history/detail surfaces.

### VPS Detail

Useful pattern: hero with compact identity and action menu, visible primary decision CTA, secondary operations grouped into overflow menu, workbench leads with next action, evidence grouped by decision/cost/Node/context/data quality, edit/create workflows use drawers.

## Final Recommendation

For the MVP implementation sequence:

1. Refactor `SubscriptionsPage` IA first.
   - Biggest remaining mismatch with drawer/table-first and evidence-first patterns.
   - Direct impact on renewal/cost decision workflows.
2. Refactor `ProvidersPage` second as a quick win.
   - Low-risk modernization of older CRUD page.
   - Move inline forms to drawers and add only minimal summary/context.
3. Defer `TargetsPage` and `NodesPage` until after simpler asset-ledger pages.
   - They are important, but interaction risk is higher.
4. Leave `VPSPage`, `DashboardPage`, `AssetDecisionsPage`, and `EventsPage` mostly intact except small visual polish.
   - These pages already embody the intended direction or have significant URL/deep-link/test risk.

## Caveats

- Static code/spec audit only; no browser screenshots or runtime UI inspection.
- One research sub-agent returned these findings without writing the file despite being asked to persist them; this file preserves those findings for the Trellis task record.
