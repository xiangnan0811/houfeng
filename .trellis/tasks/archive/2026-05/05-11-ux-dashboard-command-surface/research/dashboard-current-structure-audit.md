# Dashboard current structure audit

## Sources inspected

- `docs/release/core-pages-product-ux-replan.md`
- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`
- `.trellis/spec/web/*`
- `web/src/pages/DashboardPage.tsx`
- `web/src/pages/dashboard/*`
- `web/src/pages/DashboardPage.test.tsx`
- `web/src/styles/pages.css`

## Current state

- `DashboardPage` fetches `getDashboard()` and derives `fleetState`, inline metrics and `attentionItems`.
- `FleetStatePanel` owns the first visual block. It focuses on fleet status, key metrics, 24h trend, refresh controls and primary CTA.
- `DashboardWorkbench` owns the lower block. Abnormal state renders `AttentionQueue` plus an aside with `DashboardContextStrip`, `AssetDecisionSummary` and `ManagementEntries`; normal / maintenance state renders `RunningOverview`, which includes `AssetDecisionSummary`, `ManagementEntries` and context.
- `AssetDecisionSummary` already has useful contract-safe entries, but its current placement is secondary and can duplicate across states.
- Tests already prevent restoring API facts, global KPI strip, Group summary and recent event summary.

## Problems for UX-2

- The first screen is still led by fleet health rather than the user's daily asset/renewal decisions.
- Asset pressure is visually subordinate in abnormal state and duplicated in normal/maintenance state.
- The page has several side-by-side small surfaces with similar visual weight, which works as an overview but not as a clear command surface.
- Copy still exposes `首页 / Dashboard` in loading/error paths.
- Attention queue rows lack an explicit `当前问题` label even though the current issue is the core decision signal.

## Constraints

- Do not introduce new API fields or derive asset details from unavailable data.
- `snapshot_generated_at` is only dashboard generation time, not shell health or sync freshness.
- `asset_summary` must remain aggregate-only: renewal due counts, unreviewed count, cancel/migrate counts, unlinked/abnormal-linked counts and currency cost summary.
- `group_summaries` and `recent_events` may feed compact context, but must not become Dashboard lists.
- Existing PR4 deep links must keep working.
- CSS must stay in `web/src/styles/pages.css` and use token variables / BEM naming.

## Recommended implementation

1. Add a first-screen `DashboardCommandSurface` component.
2. Make it the only prominent place for asset summary, observability status and next actions.
3. Keep the attention queue / running overview as the secondary workbench below the command surface.
4. Remove `AssetDecisionSummary` duplication from `DashboardWorkbench` and `RunningOverview`.
5. Update tests around user-visible Dashboard copy and command surface semantics.
6. Update visual/spec contracts to state that Dashboard is now asset-decision-first.

## Visual direction

- Utilitarian, dense, dark-first and operational.
- Use fewer framed cards; prefer grouped lanes, hairline separators, compact rows and strong typographic priority.
- Keep status colors semantic, not decorative.
- The memorable first impression should be: this page tells me what to do next.
