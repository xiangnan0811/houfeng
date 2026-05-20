# Research: Next candidate pages IA audit

- **Query**: Research the next candidate batch for page information architecture optimization; rank `TargetDetailPage`, `TargetsPage`, `NodesPage`, `AssetDecisionsPage`, `EventsPage`, `DashboardPage`, `NodeOnboardingPage`, and `NodeComparePage` by user value, IA gap, implementation risk, and test risk; identify top low/medium-risk changes and files/tests touched; map recommendations to existing design patterns.
- **Scope**: internal
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/TargetDetailPage.tsx` | Target detail route container; coordinates target/runtime/probe/history/metadata/lifecycle state and mutations. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Main Target detail IA composition: watchtower header, runtime confirmation, danger card, time window tabs, latency trends, probe list, metadata/probe/lifecycle sections, snapshot meta, history drawer. |
| `web/src/pages/target-detail/TargetProbeListSection.tsx` | Wraps the ProbeItem evidence list in a collapsed `.watchtower-secondary` details section. |
| `web/src/pages/target-detail/TargetProbeManagementSection.tsx` | Inline ProbeItem create/edit management section; expands `TargetProbeForm` inside a property-list item. |
| `web/src/pages/target-detail/TargetMetadataSection.tsx` | Collapsed labels/note details section. |
| `web/src/pages/target-detail/TargetLifecycleSection.tsx` | Target lifecycle/archive section using dangerous confirmation patterns. |
| `web/src/pages/target-detail/TargetHistoryDrawer.tsx` | Existing Drawer + Tabs history surface for target events/incidents. |
| `web/src/pages/target-detail/TargetDangerCard.tsx` | Current-problem card shown when active target incidents exist. |
| `web/src/components/target-detail/TargetWatchtowerHeader.tsx` | Target detail identity/freshness/runtime actions header. |
| `web/src/components/target-detail/TargetLatencyTrends.tsx` | Watchtower-mode trend cards for recent latency by ProbeItem. |
| `web/src/components/target-detail/TargetProbeList.tsx` | ProbeItem cards, latest observations DataTables, empty state, row actions, delete confirmation. |
| `web/src/pages/TargetDetailPage.test.tsx` | Large, detailed Target detail test suite covering probe CRUD, runtime confirmations, stale route safety, metadata saves, danger zone, secondary collapsed state, and time-window tabs. |
| `web/src/pages/TargetsPage.tsx` | Important target list/workbench page with support surface, filter panel, batch panel, DataTable, create Drawer, runtime overlays, URL-state filters. |
| `web/src/pages/TargetsPage.test.tsx` | High-coverage tests for create drawer, filters/deep links, support surface, row navigation/action guards, runtime overlays, metadata quick edit, batch behavior, sparklines. |
| `web/src/pages/NodesPage.tsx` | Important node list/workbench page with hero/support surface, toolbar, DataTable list section, create drawer, compare, auto-refresh, batch and command surfaces. |
| `web/src/pages/NodesPage.test.tsx` | High-coverage tests for create/onboarding, binding conflict, runtime actions, metadata concurrency, filters, support lanes, DataTable guards, segmented controls, batch and freshness rows. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset Ledger decision queue page; already matches unified queue + Drawer + summary/evidence pattern. |
| `web/src/pages/EventsPage.tsx` | Events page with URL-backed applied filters, draft filter Drawer, overview, and stream section. |
| `web/src/pages/DashboardPage.tsx` | Dashboard route using command surface + workbench pattern. |
| `web/src/pages/dashboard/DashboardCommandSurface.tsx` | Dashboard command-surface implementation with asset/observability/next-action lanes. |
| `web/src/pages/dashboard/DashboardWorkbench.tsx` | Dashboard workbench states for onboarding, abnormal, maintenance, and normal contexts. |
| `web/src/pages/NodeOnboardingPage.tsx` | Security-sensitive onboarding workbench with center-generated install command, Stepper, summary cards, binding conflict actions. |
| `web/src/pages/NodeOnboardingPage.test.tsx` | Security-sensitive tests proving no browser-origin command synthesis, deliberate reveal/copy, regeneration, conflict copy, and Stepper states. |
| `web/src/pages/NodeComparePage.tsx` | Small A/B comparison page; low interaction surface. |
| `web/src/pages/NodeComparePage.test.tsx` | Small test suite for empty state, A/B identity/metrics, and one-side load failure. |
| `web/src/pages/NodeDetailPage.tsx` | Completed Node detail route container used as a pattern reference. |
| `web/src/pages/node-detail/NodeDetailPageBody.tsx` | Completed Node detail IA benchmark: watchtower header, confirmations, danger/binding cards, time tabs, metrics, linked VPS, containers, snapshot meta, history/command drawers. |
| `web/src/components/node-detail/NodeWatchtowerHeader.tsx` | Completed Node detail header benchmark. |
| `.trellis/spec/web/component-conventions.md` | Component layering, PageState/DataTable/Drawer/focus/interactive semantics constraints. |
| `.trellis/spec/web/directory-structure.md` | Page/container/component/API-client structure constraints. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification and page-test expectations. |
| `.trellis/spec/web/state-and-data.md` | State/data constraints including no React Query/SWR, API client usage, one-command install security contract. |
| `.trellis/spec/web/styling-guidelines.md` | Pure CSS/BEM/tokens/no page-local CSS/no business inline-style constraints. |
| `docs/design/v2-houfeng/design-language.md` | Dark-first high-density visual authority and page hierarchy guidance. |
| `docs/design/v2-houfeng/component-spec.md` | Reference patterns for Button, Card, DataTable, Drawer, Stepper, ActionConfirmationCard, Dashboard, AssetDecisions, etc. |

### Candidate Ranking

Scoring uses qualitative levels for the next IA batch, not absolute product importance.

| Rank | Candidate | User Value | IA Gap | Implementation Risk | Test Risk | Recommendation |
|---:|---|---|---|---|---|---|
| 1 | `TargetDetailPage` | High | High | Medium | Medium/High | Primary MVP candidate. It is the clearest remaining detail-page gap and can reuse completed Node Detail/watchtower patterns without backend/API changes. |
| 2 | `NodeComparePage` | Medium | Medium | Low | Low | Good optional thin-slice candidate if the batch needs a low-risk second page. Keep scope small: improve comparison framing/summary, not new data or charts. |
| 3 | `TargetsPage` | High | Medium | High | High | Defer or take only a very narrow polish slice. It is operationally important but interaction-heavy and already has strong support/filter/DataTable structure. |
| 4 | `NodesPage` | High | Low/Medium | High | Very High | Defer. High value, but current IA is already advanced and test surface is large. |
| 5 | `NodeOnboardingPage` | High | Low/Medium | Medium/High | High/Security | Defer unless real testing exposes a concrete issue. It is security-sensitive and already has deliberate command reveal/copy semantics. |
| 6 | `EventsPage` | Medium | Low | Medium | Medium | Defer. URL-backed filter/draft Drawer architecture is already strong. |
| 7 | `DashboardPage` | High | Low | Medium | Medium | Defer. Already command-surface-first and spec-aligned. |
| 8 | `AssetDecisionsPage` | Medium/High | Low | Medium | Unknown/Medium | Defer. Already matches unified decision queue + Drawer + summary/evidence pattern. |

### Top Candidate 1: Target Detail

#### Why this is the best next MVP

`TargetDetailPage` has the same domain weight as completed Node Detail: it is where an operator asks “is this target healthy, what is failing, which probes are responsible, and what should I do next?” The current page already has the main watchtower skeleton but still leaves core Target-specific evidence and configuration in secondary/inline areas.

Evidence:

- `TargetDetailPageBody.tsx` orders the page as watchtower header → pause confirmation/error → danger card → time tabs → latency trends → probe list → property-list sections → snapshot meta → history drawer.
- `TargetProbeListSection.tsx` wraps the ProbeItem list in collapsed `<details className="watchtower-secondary">`, even though ProbeItems are the target’s core observability evidence.
- `TargetMetadataSection.tsx` also uses collapsed secondary details; this is more acceptable because labels/note are true metadata.
- `TargetProbeManagementSection.tsx` expands create/edit forms inline inside `.watchtower-property-item`, including inline layout styles based on `probeCreateOpen`; this is weaker than the established Drawer/secondary-operation pattern and slightly conflicts with styling guidance that inline styles should not encode business layout.
- `TargetProbeList.tsx` already has strong local building blocks: `PageState` empty state, ProbeItem cards, latest-observation `DataTable`, row actions, and `ActionConfirmationCard` for deletion.
- `TargetDetailPage.test.tsx` explicitly asserts secondary details are collapsed by default (`document.querySelectorAll('.watchtower-secondary')`), so changing ProbeItem visibility is a known test update area.

#### Low/medium-risk IA changes

1. Promote ProbeItem evidence from collapsed secondary details into the default scan path.
   - Change `TargetProbeListSection` from a closed `<details>` wrapper into a visible `DetailSection`/watchtower evidence section, or make only deeper per-probe details collapsible while the list heading/summary remains visible.
   - Keep `TargetProbeList` and its DataTable internals rather than rewriting row/cards.
   - Expected benefit: the page default view answers “what probes define this target and what are their latest observations?” without requiring the user to open a secondary section.
   - Pattern mapping: watchtower/detail + DataTable/filter (for latest observations) + summary cards if adding compact counts.

2. Move ProbeItem create/edit into a Drawer or a clearly separated command surface, reusing the existing `TargetProbeForm`.
   - Use existing Drawer behavior from `TargetHistoryDrawer`, `TargetsPage` create drawer, or Asset Decisions work drawer.
   - Preserve local validation, pending state, stale-route protections, and disabled row actions from `TargetDetailPage.tsx`.
   - Avoid changing API payloads or form validation rules.
   - Expected benefit: removes a large mutable form from the main scan path and aligns create/edit with the project’s Drawer convention.
   - Pattern mapping: Drawer + command surface + PageState empty action.

3. Keep metadata and lifecycle as secondary/danger surfaces, but make their grouping clearer.
   - Metadata can remain collapsed/secondary because labels/note are not the main operational question.
   - Lifecycle/archive confirmation already matches `ActionConfirmationCard`; avoid redesigning dangerous action semantics.
   - Pattern mapping: dangerous action confirmation + watchtower/detail.

4. Optional small summary strip for Target detail, if implementation remains bounded.
   - A compact summary near trends/probes could show active ProbeItem count, enabled count, failing/latest-observed count, and covered node count using existing runtime/probe facts only.
   - Do not add backend data or new charts.
   - Pattern mapping: summary cards.

#### Files likely touched

- `web/src/pages/TargetDetailPage.tsx`
  - Likely state wiring changes if ProbeItem create/edit moves from inline section to Drawer open/close semantics.
  - Keep existing request ID / mounted / route-target guards intact.
- `web/src/pages/target-detail/TargetDetailPageBody.tsx`
  - Reorder/replace `TargetProbeListSection` / `TargetProbeManagementSection` placement.
  - Potentially pass Drawer state/handlers.
- `web/src/pages/target-detail/TargetProbeListSection.tsx`
  - Convert from collapsed details to visible evidence section or add an always-visible summary with nested details.
- `web/src/pages/target-detail/TargetProbeManagementSection.tsx`
  - Either shrink to command entry point or replace inline form expansion with Drawer launch behavior.
- `web/src/components/target-detail/TargetProbeForm.tsx`
  - Ideally reused unchanged; only adjust if Drawer title/layout requires small props/copy changes.
- `web/src/pages/target-detail/TargetMetadataSection.tsx`
  - Optional only if property grouping changes.
- `web/src/pages/target-detail/TargetHistoryDrawer.tsx`
  - Reference pattern; likely not edited unless shared Drawer naming/classes are extracted.
- `web/src/styles/pages.css`
  - Add BEM classes for promoted probe evidence / target operations; avoid page-local CSS and business inline styles.
- `web/src/pages/TargetDetailPage.test.tsx`
  - Update assertions for ProbeItem visibility and collapsed secondary sections.
  - Add/adjust Drawer open/close/focus/reset tests if create/edit moves to Drawer.
  - Preserve existing tests for validation localness, row action disabled states, stale route results, delete/runtime confirmation mutual exclusion, metadata concurrency, danger zone, and time-window tabs.

#### Test-risk notes from `TargetDetailPage.test.tsx`

The test file is extensive and creates meaningful guardrails:

- Initial render expects `标签与备注` and `ProbeItem 列表` text, runtime observations, fetch order, and no obsolete placeholder copy.
- Probe create tests cover empty-state creation, TLS defaults (`6h`), HTTP/TCP default preservation, validation errors staying inside the probe panel, edit/save payloads, unsupported config blocking, pending-save disabling, row mutation serialization, enable/disable payload preservation, and delete confirmation focus restore.
- Runtime tests cover restore from archived, pause/archive confirmations, local errors, stale route safety, and mutual exclusion between runtime confirmations and ProbeItem delete confirmation.
- Metadata tests cover empty note copy, PATCH payload and `If-Match`, local errors, stale save safety, and preserving saved metadata across unrelated runtime refreshes.
- IA-specific tests cover danger-zone presence/absence and “secondary details sections collapsed by default.” The latter is the key assertion to revisit if ProbeItem evidence becomes default-visible.

### Top Candidate 2: Node Compare

#### Why it is a reasonable optional second page

`NodeComparePage` has lower operational centrality than Target Detail, but it is small and has a low-risk test surface. It currently renders A/B identity composition and `NodeWatchtowerMetrics` sections, and handles missing selection / unavailable side with `PageState`. This makes it suitable for a bounded “thin polish” if the batch wants a second page without taking on another heavy list page.

#### Low-risk IA changes

1. Add a comparison summary band before metric details.
   - Use existing facts only: node names/statuses, region/city/provider/group, latest sample availability, key CPU/memory/disk deltas where both sides have samples.
   - Keep `NodeWatchtowerMetrics` as the detailed section.
   - Pattern mapping: summary cards + watchtower/detail.

2. Clarify “missing sample” and “one side unavailable” state hierarchy.
   - Current tests already expect PageState empty/error behavior; keep `PageState` semantics.
   - Do not add new data sources or routes.
   - Pattern mapping: PageState + summary cards.

#### Files likely touched

- `web/src/pages/NodeComparePage.tsx`
- `web/src/pages/NodeComparePage.test.tsx`
- `web/src/styles/pages.css` only if new shared BEM classes are needed.

#### Test-risk notes

`NodeComparePage.test.tsx` is small and covers:

- PageState empty when two IDs are not selected.
- A/B identity rendering, detail links, CPU metric heading, missing sample heading, and fetch URL/order for `/runtime-facts?window=24h`.
- PageState error when one side cannot be loaded.

This is the lowest-risk candidate, but it should not displace `TargetDetailPage` because its user value is narrower.

### Candidate 3: Targets Page

#### Why defer or keep very narrow

`TargetsPage` is high value but already has several good IA elements:

- Create target Drawer.
- `TargetsSupportSurface`.
- `TargetsFilterPanel` and URL-backed filters (`group`, `type`, `run_status`, `health`, `labels`, `execution_labels`, `abnormal`).
- `TargetsBatchPanel` and runtime overlays.
- `DataTable` row navigation with interactive descendant guards.
- Batch actions visible only when group filter is active.

The page is interaction-heavy, and tests are extensive: create flow, validation/reset, late create response safety, runtime confirmations, row-local errors, metadata quick edit and stale response protection, deep-link filters, support surface quick filters, coverage gap lane, stable evidence lead, row navigation/action guard, create drawer focus restore, and latency sparkline column.

#### Possible narrow slice if chosen later

- Tighten the top support/filter/selection hierarchy without changing row behavior or URL state.
- Avoid changing table columns, create drawer workflow, batch semantics, or runtime overlays in the same batch.
- Pattern mapping: DataTable/filter + command surface + Drawer.

### Candidate 4: Nodes Page

#### Why defer

`NodesPage` is high value but already advanced and very interaction-heavy:

- Uses `NodesHero`, `NodesSupportSurface`, `CreateNodeDrawer`, `NodesToolbar`, `NodesListSection`, compare set, auto-refresh, command drawer/batch command, batch operations, URL-backed filters, and DataTable columns.
- Tests cover create-to-onboarding, errors, binding conflict, runtime pause confirmation, metadata concurrency, filters/deep links including onboarding pending, support quick filters, stable evidence lead, DataTable action guards, segmented controls, drawer/panel toggle, sparklines, batch bar, and heartbeat/sync freshness.

This page should not be in the next MVP unless the task explicitly prioritizes list-page control-band simplification and accepts high test churn.

### Lower-priority / already aligned pages

#### Asset Decisions

`AssetDecisionsPage` already matches the intended Asset Ledger pattern: unified decision queue, tabs, summary metrics, `AssetDecisionWorkPanel` in a Drawer, and `AssetDecisionRenewalTable` as secondary evidence. Keep out of this batch unless a specific real-use issue appears.

Pattern mapping already present: command/decision surface + Drawer + summary cards + secondary evidence.

#### Events

`EventsPage` already has a strong data-state architecture:

- URL parse/normalize/canonicalize.
- Applied filters derived from URL.
- Draft filters inside `EventsFilterDrawer`.
- `commitFilters()` writes URL and triggers fetch.
- Separate support/overview/stream sections.

Pattern mapping already present: DataTable/filter-like applied/draft filter model + Drawer + PageState/stream.

#### Dashboard

`DashboardPage` is already command-surface-first:

- `DashboardCommandSurface` handles asset decision, observability, next-action lanes, primary action, focus items, and refresh/auto-refresh controls.
- `DashboardWorkbench` handles onboarding, abnormal, maintenance, and normal states via detail sections, queues, context strip, management entries, onboarding/running overview.

Pattern mapping already present: command surface + summary/workbench.

#### Node Onboarding

`NodeOnboardingPage` is high operational value but should be treated as security-sensitive and mostly out of scope:

- Install command is generated by center via `issueNodeInstallCommand(nodeId)`.
- UI hides/reveals/copies deliberately.
- Tests prove no production command synthesis from `window.location.origin`.
- Binding conflict copy includes one-time token consumption caveat.

Do not move/copy/rephrase token-bearing UI casually. Any IA work here must preserve the security contract: the center-generated `issue.command` is the only production command shown for copy, tokens are hidden except deliberate authenticated reveal/copy surfaces, and notices/conflict copy must not expose full tokens.

### Code Patterns

#### Watchtower/detail pattern

`NodeDetailPageBody.tsx` is the completed benchmark: header → runtime confirmation/error → danger/binding cards → time window → metrics → related section → dangerous lifecycle confirmation → containers/detail evidence → snapshot meta → drawers. `TargetDetailPageBody.tsx` already mirrors much of this, but Target-specific probe evidence/management is still partly secondary/inline.

`TargetDetailPageBody.tsx` current structure:

```tsx
<TargetWatchtowerHeader ... />
<TargetRuntimePauseConfirmation ... />
<TargetDangerCard ... />
<TargetTimeWindowTabs ... />
<TargetLatencyTrends ... watchtower />
<TargetProbeListSection ... />
<div className="watchtower-property-list">
  <TargetProbeManagementSection ... />
  <TargetMetadataSection ... />
  <TargetLifecycleSection ... />
</div>
<TargetSnapshotMeta />
<TargetHistoryDrawer ... />
```

#### Collapsed secondary ProbeItem evidence

`TargetProbeListSection.tsx` currently makes the ProbeItem list secondary:

```tsx
<details className="watchtower-secondary">
  <summary>ProbeItem 列表</summary>
  <div className="watchtower-secondary__body">
    <TargetProbeList ... />
  </div>
</details>
```

This is the clearest IA gap because ProbeItem evidence is primary to a Target detail page.

#### Inline ProbeItem management

`TargetProbeManagementSection.tsx` expands create/edit inline:

```tsx
<div
  className="watchtower-property-item"
  style={{
    flexDirection: probeCreateOpen ? 'column' : 'row',
    alignItems: probeCreateOpen ? 'stretch' : 'center',
  }}
>
  ...
  {probeCreateOpen ? (
    <div style={{ marginTop: 'var(--space-4)', width: '100%' }}>
      <TargetProbeForm ... />
    </div>
  ) : null}
</div>
```

This is a concrete opportunity to replace main-path inline expansion with Drawer-based create/edit while keeping existing form and mutation logic.

#### Dangerous action confirmation

Target detail already uses `ActionConfirmationCard` patterns for:

- Target pause confirmation.
- Target archive confirmation.
- ProbeItem delete confirmation.

Tests assert copy, focus restoration, local error behavior, and no `window.confirm` fallback. Preserve this unchanged in the next IA batch.

#### Drawer pattern

Existing Drawer references are available in:

- `TargetHistoryDrawer` for target history.
- `TargetsPage` create target flow.
- `AssetDecisionsPage` decision work panel.

If ProbeItem create/edit moves to Drawer, it should follow component conventions: portal to `document.body`, focus containment, Escape/overlay close, restore focus, and reset draft/errors on close.

#### DataTable/filter pattern

`TargetsPage` and `NodesPage` are already DataTable/filter-heavy pages with URL state and row-click guards. They should be used as references rather than next targets unless list-page IA is explicitly prioritized.

#### Summary cards / command surface pattern

- `DashboardCommandSurface` is the command-surface benchmark.
- `AssetDecisionsPage` is the unified decision queue benchmark.
- For Target Detail, a small summary strip should be optional and based only on existing target/probe/runtime facts.
- For Node Compare, summary cards are the safest improvement because they can summarize existing A/B facts without data/model changes.

### Related Specs

- `.trellis/spec/web/component-conventions.md` — PageState, DataTable row guards, Drawer focus/close/reset behavior, component layering, relation form guidance.
- `.trellis/spec/web/directory-structure.md` — pages assemble route state; shared API calls belong in `lib/api.ts`; colocated tests.
- `.trellis/spec/web/quality-guidelines.md` — lint/test/build expectations and page-test style with mocked fetch.
- `.trellis/spec/web/state-and-data.md` — no React Query/SWR/state library; business API through `lib/api.ts`; one-command install security contract.
- `.trellis/spec/web/styling-guidelines.md` — pure CSS, BEM, design tokens, no page-local CSS except LoginPage, inline style only for dimension/calculation rather than business layout.
- `docs/design/v2-houfeng/design-language.md` — dark-first, cold/calibrated/high-density engineering tool, page hierarchy identity → current problem → trends/context → history/events → danger.
- `docs/design/v2-houfeng/component-spec.md` — Button/Card/DataTable/Drawer/Stepper/ActionConfirmationCard and Dashboard/Asset Decisions page templates.

## Recommendation

Choose `TargetDetailPage` as the next-batch MVP.

Suggested implementation boundary:

1. Make ProbeItem evidence default-visible on Target detail.
2. Move ProbeItem create/edit out of inline property-list expansion and into a Drawer or compact command entry that opens a Drawer.
3. Preserve existing dangerous confirmations, runtime actions, stale-route safety, metadata concurrency, and API payloads.
4. Keep `TargetsPage`, `NodesPage`, `NodeOnboardingPage`, `DashboardPage`, `EventsPage`, and `AssetDecisionsPage` out of the MVP, except as pattern references.
5. Optionally add a very small `NodeComparePage` summary-card polish if the batch needs a low-risk secondary page.

## Caveats / Not Found

- No code changes or tests were run for this audit.
- `TargetDetailPage.test.tsx` was ultimately inspected in chunks after an initial oversized-read failure; the file is large and should be treated as a major test-risk source for any Target detail IA implementation.
- The audit did not deeply read `AssetDecisionsPage.test.tsx`, `EventsPage.test.tsx`, or `DashboardPage.test.tsx`; lower priority for those pages is based on page implementation, specs, design patterns, and prior context rather than full test-suite inspection.
- No backend/API changes appear necessary for the recommended MVP.
- `NodeOnboardingPage` contains security-sensitive one-command install behavior; avoid incidental token exposure or command synthesis changes.
