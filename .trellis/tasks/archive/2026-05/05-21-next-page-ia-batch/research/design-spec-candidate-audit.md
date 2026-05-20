# Research: design/spec candidate audit

- **Query**: Research design/spec fit for the next Houfeng frontend page IA batch; inspect design docs/spec files and current web page patterns enough to recommend which remaining page/page group is under-aligned with Houfeng v2 visual/component language and safe to polish next.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `docs/design/v2-houfeng/design-language.md` | Visual authority for Houfeng v2: dark-first, restrained, high-density engineering-tool language; Chinese-primary UI; no Tailwind/CSS-in-JS/chart libraries; visual work must not alter backend/API/data shape. |
| `docs/design/v2-houfeng/component-spec.md` | Component/page visual contract, including explicit templates for Dashboard, AssetDecisions, VPS/VPSDetail, Nodes/NodeDetail, Events, Settings, Targets/TargetDetail, NodeOnboarding, and Login. |
| `.trellis/spec/web/styling-guidelines.md` | Styling rules: pure CSS, BEM, central global style files, design tokens, shared state patterns. |
| `.trellis/spec/web/component-conventions.md` | Frontend layering and component conventions: pages do not import pages, components do not call API directly, route states use `PageState`, forms/filters use Drawer patterns. |
| `.trellis/spec/web/state-and-data.md` | Data/API constraints: all business API calls through `web/src/lib/api.ts`, snake_case frontend types mirror backend JSON, dashboard/asset-ledger semantic boundaries. |
| `web/src/pages/TargetDetailPage.tsx` | Current Target detail route; already fetches target, runtime facts, ProbeItems, events, incidents, historical incidents, and observations through existing API client calls. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Main Target detail layout; currently organized as a watchtower-style page with header, danger/runtime controls, latency trends, ProbeItem list, property list sections, snapshot meta, and drawers. |
| `web/src/components/target-detail/TargetWatchtowerHeader.tsx` | Current Target identity/action header; uses badges, host/id display, runtime action affordances, and watchtower naming/layout. |
| `web/src/components/target-detail/TargetLatencyTrends.tsx` | Existing latency evidence component using `Sparkline`; can support the v2 Target detail “recent latency trends” section without API changes. |
| `web/src/pages/target-detail/TargetProbeListSection.tsx` | Existing `DetailSection` wrapper for ProbeItem evidence list. |
| `web/src/pages/target-detail/TargetProbeManagementSection.tsx` | Current ProbeItem management action block inside a watchtower property-list layout. |
| `web/src/pages/ProvidersPage.tsx` | Asset Ledger support page using page hero/command panel, `DataTable`, `Drawer`, and state patterns; plausible but lower-priority polish candidate. |
| `web/src/pages/SubscriptionsPage.tsx` | Asset Ledger support page with URL-state VPS context, filters, `DataTable`, and `Drawer`; plausible but lower-priority polish candidate. |
| `web/src/pages/DashboardPage.tsx` and `web/src/pages/dashboard/*` | Dashboard already follows a v2 command-surface/workbench pattern and respects dashboard data-shape constraints. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset decision queue already aligned around tabs, queue rows, Drawer detail panel, and renewal evidence. |
| `web/src/pages/VPSPage.tsx` | VPS inventory page already has a recent v2 inventory command panel, quick views, Drawer filters, and `DataTable`. |
| `web/src/pages/NodesPage.tsx` | Nodes list already uses recent v2 hero/support/toolbar/list sections. |
| `web/src/pages/TargetsPage.tsx` | Targets list already uses recent v2 support surface, list command band, Drawer creation, filters, and `DataTable`. |
| `web/src/pages/EventsPage.tsx` | Events page already uses URL-state filters, draft/applied Drawer filters, support surface, and stream section. |
| `web/src/pages/SettingsPage.tsx` | Settings page already uses recent v2 sections, tabs, `DetailSection`, state handling, and save footer. |
| `web/src/pages/NodeComparePage.tsx` | Node compare page already uses command panel, identity cards, summary strip, `DetailSection`, and existing watchtower metrics. |

### Code Patterns

- Houfeng v2 constraints come from design/spec rather than inferred screenshots. The relevant constraints for the next IA batch are:
  - Preserve API/data contracts and avoid backend changes for visual/IA work (`docs/design/v2-houfeng/design-language.md`, `.trellis/spec/web/state-and-data.md`).
  - Use existing atom/composite vocabulary and pure CSS/BEM tokens (`docs/design/v2-houfeng/component-spec.md`, `.trellis/spec/web/styling-guidelines.md`).
  - Prefer shared route state/section primitives (`PageState`, `DetailSection`, `DataTable`, `Drawer`) over page-local ad hoc UI (`.trellis/spec/web/component-conventions.md`).
- Current `TargetDetailPage.tsx` already has the necessary data surface for an API-preserving IA polish: target metadata, runtime facts, ProbeItems, recent observations, active incidents, historical incidents, and events are loaded via `web/src/lib/api.ts` functions such as `getTarget`, `getTargetRuntimeFacts`, `listTargetProbeItems`, `listEvents`, `listIncidents`, `listHistoricalIncidents`, and `listRecentObservations`.
- Current `TargetDetailPageBody.tsx` layout is functional but still reads as a Node/watchtower-style detail page. Its main order is: `TargetWatchtowerHeader`, pause confirmation/runtime error, optional danger card, time-window tabs, latency trends, ProbeItem list, watchtower property-list management/metadata/lifecycle blocks, snapshot meta, and form/history drawers.
- `docs/design/v2-houfeng/component-spec.md` gives `TargetDetailPage` a more explicit Target-centric IA contract: hero with target name and four hero meta cards, a summary grid for health / ProbeItem count / current main issue, sections for labels/note, runtime controls, recent latency trends, ProbeItem list, current incidents, and events.
- Existing target-detail building blocks make the recommended scope safe and bounded:
  - `TargetWatchtowerHeader.tsx` already supplies identity, host/id, badges, and runtime actions.
  - `TargetLatencyTrends.tsx` already supplies recent latency evidence with `Sparkline`.
  - `TargetProbeListSection.tsx` already wraps ProbeItem evidence in `DetailSection`.
  - `TargetProbeManagementSection.tsx`, `TargetMetadataSection.tsx`, and `TargetLifecycleSection.tsx` already isolate management/metadata/lifecycle affordances.
  - `TargetProbeFormDrawer` and `TargetHistoryDrawer` already support secondary workflows without page-level data contract changes.
- Recently improved or currently aligned pages should be deprioritized for this batch: Dashboard, AssetDecisions, VPSPage, NodesPage, TargetsPage, EventsPage, SettingsPage, NodeCompare, and the user-listed NodeDetail/VPSDetail/NodeOnboarding work all already show v2 command surfaces, Drawer workflows, `DetailSection`, `DataTable`, or route-state patterns.

### Candidate Ranking

1. **Recommended: `TargetDetailPage` / Target detail IA**
   - Highest-value remaining candidate.
   - It has an explicit v2 page contract in `component-spec.md`.
   - It was not listed among the recent IA batches already completed.
   - It already loads the needed evidence through existing APIs, so the work can be limited to IA/layout/component-language polish.
   - It is currently closest to a watchtower hybrid and appears under-aligned with the Target-specific v2 section order.

2. **Fallback group: `SubscriptionsPage` + `ProvidersPage` Asset Ledger support pages**
   - Plausible lower-priority polish group.
   - Both already use several v2 primitives (`DataTable`, `Drawer`, command/summary panels, state views), and `SubscriptionsPage` respects URL-state VPS context behavior.
   - Lower urgency because they are support/master-data pages and already broadly follow current component conventions.

3. **Low-priority visual-only candidate: `LoginPage`**
   - Has a design contract but offers lower operational IA value than Target detail.
   - Should not displace a Target detail polish unless the team wants a small isolated visual pass.

4. **Deprioritized for this batch**
   - `DashboardPage`, `AssetDecisionsPage`, `VPSPage`, `VPSDetailPage`, `NodesPage`, `TargetsPage`, `NodeDetailPage`, `NodeComparePage`, `NodeOnboardingPage`, `SettingsPage`, and `EventsPage` because recent work or current code already aligns them with v2 command surfaces, workbench/section structure, Drawer patterns, and shared state primitives.

### Recommended Scope

- Polish `TargetDetailPage` / target-detail components only.
- Preserve existing API/data contracts and keep all business API calls in `web/src/lib/api.ts`.
- Avoid backend changes, new endpoints, new state libraries, new chart libraries, Tailwind/CSS-in-JS, or broad route rewrites.
- Reorganize the existing target-detail evidence into the v2 Target detail IA contract:
  - Target-centric hero/meta area.
  - Summary grid for health, ProbeItem coverage/count, and current main issue.
  - `DetailSection` for labels/note/metadata.
  - `DetailSection` for runtime controls.
  - `DetailSection` for recent latency trends.
  - `DetailSection` for ProbeItem list and ProbeItem management entry.
  - First-class current incidents and recent events sections rather than relying primarily on the history Drawer.
- Reuse existing components where possible (`TargetLatencyTrends`, `TargetProbeListSection`, `TargetProbeFormDrawer`, `TargetHistoryDrawer`, runtime confirmation cards/controls, `StatusGlyph`, `StatusBadge`, `Hostname`, `Timestamp`, `MonoDigits`, `Sparkline`).
- Treat any CSS additions as small extensions to existing global page styles (`web/src/styles/pages.css`) using tokens/BEM; do not add page-local CSS files.

### External References

- None. This audit is internal to Houfeng design/spec/code alignment.

### Related Specs

- `docs/design/v2-houfeng/design-language.md` — visual language and hard visual/API boundaries.
- `docs/design/v2-houfeng/component-spec.md` — page/component templates, including TargetDetailPage.
- `.trellis/spec/web/styling-guidelines.md` — styling constraints for pure CSS, central files, BEM, and tokens.
- `.trellis/spec/web/component-conventions.md` — frontend layering, route state, Drawer/DataTable/PageState conventions.
- `.trellis/spec/web/state-and-data.md` — API/data flow and semantic constraints.

## Caveats / Not Found

- This was an internal audit only; no external design references were needed.
- Line numbers are not repeated here because the relevant files were large and several candidate pages were inspected as whole-page patterns; implementation planning should re-read exact target-detail sections before editing.
- No code changes, tests, or builds were performed as part of this research.
