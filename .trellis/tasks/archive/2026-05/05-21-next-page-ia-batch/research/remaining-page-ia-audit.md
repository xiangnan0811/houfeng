# Research: remaining page IA audit

- **Query**: Inspect current routes/pages and archived Trellis IA tasks to identify which remaining Houfeng frontend page or page group has the highest value for a limited, low-risk information architecture polish; produce a candidate matrix and recommended next scope.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Active SPA route inventory: Dashboard, VPS, VPS detail, Providers, Subscriptions, AssetDecisions, Nodes, NodeCompare, NodeDetail, NodeOnboarding, Targets, TargetDetail, Events, Settings. |
| `web/src/app/metadata.ts` | Primary navigation IA groups: 总览, 资产, 观测, 系统. |
| `web/src/app/layout/AppShell.tsx` | Shell-level dashboard summary, sync status, critical alert, and cross-page deep links. |
| `web/src/pages/DashboardPage.tsx` / `web/src/pages/DashboardPage.test.tsx` | Dashboard command surface and workbench; tests already assert IA simplification and deep links. |
| `web/src/pages/EventsPage.tsx` / `web/src/pages/EventsPage.test.tsx` | Events support surface, filter overview/drawer, stream section, URL-state coverage. |
| `web/src/pages/AssetDecisionsPage.tsx` / `web/src/pages/AssetDecisionsPage.test.tsx` | Renewal/asset decision queue, focus cards, decision drawer, subscription/VPS evidence boundaries. |
| `web/src/pages/VPSPage.tsx` / `web/src/pages/VPSPage.test.tsx` | VPS inventory quick views, advanced filters, subscription evidence handling, create drawer. |
| `web/src/pages/ProvidersPage.tsx` / `web/src/pages/ProvidersPage.test.tsx` | Provider master-data table, summary panel, create/edit drawers and payload validation. |
| `web/src/pages/SubscriptionsPage.tsx` / `web/src/pages/SubscriptionsPage.test.tsx` | Subscription evidence list, filters, VPS-context create drawer, create/edit payload tests. |
| `web/src/pages/NodeComparePage.tsx` / `web/src/pages/NodeComparePage.test.tsx` | Recent A/B compare IA: command panel, identity cards, summary strip, runtime metrics columns. |
| `web/src/pages/TargetDetailPage.tsx` | Target detail orchestration: Target, ProbeItem, runtime facts, incidents/events, runtime actions, ProbeItem CRUD. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Current Target detail page composition and section order. |
| `web/src/pages/target-detail/TargetProbeManagementSection.tsx` | Compact ProbeItem management action row. |
| `web/src/pages/target-detail/TargetMetadataSection.tsx` | Collapsed labels/notes metadata section. |
| `web/src/pages/target-detail/TargetLifecycleSection.tsx` | Runtime lifecycle/archive/restore section and archive confirmation copy. |
| `web/src/pages/TargetDetailPage.test.tsx` | Existing Target detail behavior coverage for ProbeItems, runtime observations, trends, drawers, validation, payloads. |
| `.trellis/spec/web/component-conventions.md` | Frontend component and interaction conventions, including drawer reset and row-click contracts. |
| `.trellis/spec/web/styling-guidelines.md` | Pure CSS/token/BEM constraints and no new page CSS files. |
| `.trellis/spec/web/state-and-data.md` | API/data/security boundaries, install command/token trust boundaries, Asset Ledger evidence boundaries. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification expectations and colocated tests. |
| `docs/design/v2-houfeng/design-language.md` | Visual authority: dark-first, dense engineering tool, no new chart/CSS framework dependencies. |
| `docs/design/v2-houfeng/component-spec.md` | Page/component templates; TargetDetailPage expected visible sections include Hero, Summary grid, labels/notes, runtime control, latency trends, ProbeItem list, current incidents, events. |
| `.trellis/workspace/xiangnan-mac/index.md` and journals | Recent IA work evidence: Dashboard, AssetDecisions, VPS inventory/detail, Nodes/Targets lists, Events support, Settings, NodeDetail, NodeCompare, NodeOnboarding. |

### Code Patterns

- Current task constraints freeze scope to frontend-only IA polish: `prd.md:24-29` requires auditing routes/pages, selecting the highest-value next batch, and only changing IA composition/copy/CSS/tests without API/data/security behavior changes.
- VPS inventory is already a completed-style page: tests assert the scan path around `库存核对`, current lens, subscription evidence, field filters, work area, row navigation, and create drawer while preserving API calls and payloads (`web/src/pages/VPSPage.test.tsx:122-174`, `web/src/pages/VPSPage.test.tsx:301-380`).
- VPS subscription evidence is already guarded: failed subscription evidence keeps VPS rows visible, avoids factual `缺订阅`, and only marks missing subscriptions after evidence resolves (`web/src/pages/VPSPage.test.tsx:177-209`, `web/src/pages/VPSPage.test.tsx:268-299`).
- NodeCompare is already recent IA work: the page renders a command panel, identity pair, summary strip, and metric comparison section (`web/src/pages/NodeComparePage.tsx:89-127`), with tests covering empty, normal, no-sample, and error paths (`web/src/pages/NodeComparePage.test.tsx:95-183`).
- Target detail remains composition-heavy and high-value: `TargetDetailPage` loads target identity, ProbeItems, runtime facts, incidents, and events, then passes them to `TargetDetailPageBody` for page assembly. Existing behavior is rich enough that a polish should not alter data fetching or mutation contracts.
- Current Target detail page body places header, runtime confirmations/errors, optional danger card, time-window tabs, latency trends, ProbeItem list, a compact property list for Probe management/metadata/lifecycle, snapshot meta, and drawers. Compared with `docs/design/v2-houfeng/component-spec.md`, current incidents/events evidence appears mainly behind history surfaces rather than visible default sections.
- Target Probe management is currently a compact property item with the action `添加 ProbeItem` and drawer framing (`web/src/pages/target-detail/TargetProbeManagementSection.tsx:21-45`). This is a narrow, frontend-only IA seam if selected.
- Target lifecycle/archive handling includes explicit confirmation semantics and copy that must remain frozen: archive confirmation states current/result/impact/unchanged boundaries and preserves history/ProbeItem records (`web/src/pages/target-detail/TargetLifecycleSection.tsx`).

### Candidate Matrix

| Candidate | Current IA pain / opportunity | Risk | Likely touched files | Frozen contracts |
|---|---|---:|---|---|
| `TargetDetailPage` + `target-detail/*` | Highest remaining value. Detail page has rich Target/Probe/runtime/activity behavior, but default scan path can better separate command/health evidence, Probe work area, runtime controls, metadata, and activity boundaries. Component spec expects visible current incidents/events sections; current page appears more drawer/property-list oriented. | Medium | `web/src/pages/TargetDetailPage.tsx`, `web/src/pages/target-detail/*.tsx`, `web/src/pages/TargetDetailPage.test.tsx`, `web/src/styles/pages.css` | Preserve Target/ProbeItem GET/POST/PATCH/DELETE payloads, runtime pause/resume/maintenance/archive/restore behavior, destructive confirmations, optimistic-lock metadata save, time-window runtime facts, history drawer behavior, drawer draft/apply/discard, focus restoration. |
| `ProvidersPage` | Low-risk master-data page. Already has summary panel, table, create/edit drawers, reset and payload tests. IA gain likely limited to copy/section framing. | Low | `web/src/pages/ProvidersPage.tsx`, `web/src/pages/ProvidersPage.test.tsx`, `web/src/styles/pages.css` | Preserve create/update payload validation, provider labels/rating semantics, drawer reset behavior, row/action behavior. |
| `SubscriptionsPage` | Useful Asset Ledger evidence page, but already has VPS context, filters, prerequisite state, summary, create/edit drawers, URL-state tests. Some polish possible around renewal/cost evidence framing. | Low-Medium | `web/src/pages/SubscriptionsPage.tsx`, `web/src/pages/SubscriptionsPage.test.tsx`, `web/src/styles/pages.css` | Preserve list query params, `vps_id` + `create=1` behavior, create/update payloads, backend-computed `monthly_price` exclusion, drawer draft reset. |
| `ProvidersPage` + `SubscriptionsPage` group | Coherent Asset Ledger master-data/evidence group. Lower risk than TargetDetail, but less IA upside because both pages already follow current drawer/list conventions. | Medium | Above provider/subscription files and shared page CSS | Same as individual pages; do not invent cross-page data joins or new API fields. |
| `DashboardPage` | Already command-surface-first with workbench and attention paths; tests assert removal of extra context sections and deep links. | Low | Not recommended beyond incidental test updates. | Preserve dashboard summary/security trust boundaries; do not expose notification secrets. |
| `EventsPage` | Already has support surface, filter overview/drawer, event stream, URL canonicalization, time/maintenance/event-type filters. | Low | Not recommended. | Preserve URL filters, backfilled/maintenance semantics, event evidence copy. |
| `AssetDecisionsPage` | Already unified decision queue with renewal window, focus cards, drawer decision workflow, evidence table, and tests for evidence failures. | Medium | Not recommended. | Preserve subscription evidence failure handling, decision PATCH payload, queue semantics, row navigation isolation. |
| `VPSPage` | Recently polished; tests show lens/filter/evidence/create-row navigation contracts. | Medium | Not recommended. | Preserve subscription evidence unknown/failure behavior and create/list payloads. |
| `NodeComparePage` | Recently polished; command panel + identity + summary + metrics are already in place and tested. | Low | Not recommended. | Preserve two-id query contract and runtime facts API calls. |
| `AppShell` / nav metadata | Global shell already carries sync/critical status and grouped nav. Broad changes risk cross-page churn and are not needed for this batch. | Medium | Not recommended. | Preserve auth shell, dashboard summary fetch, critical alert links, nav routes. |
| `LoginPage` | Security-sensitive, not in current IA target set, and limited comparative value for this batch. | Medium | Not recommended. | Preserve auth/session behavior and login error handling. |

### Recommended Next Scope

Recommend selecting **TargetDetailPage** as the next IA batch.

Proposed limited scope:

1. Keep data and mutations unchanged, and only adjust Target detail composition/copy/styles/tests.
2. Clarify the default scan path: identity/health evidence first, runtime/probe work area next, then metadata/lifecycle/activity boundaries.
3. Make current incidents/events evidence more explicit if supported by existing `incidents`/`events` props, without adding new fetches or fields.
4. Retain ProbeItem CRUD drawers and all runtime/destructive confirmation flows exactly as behavior contracts.
5. Add/update tests only for new IA text/structure and frozen contracts that the composition touches.

### External References

- None. This was an internal code/spec/task audit only.

### Related Specs

- `.trellis/spec/web/component-conventions.md` — page assembly, `PageState`, drawer reset, DataTable/interactive row constraints.
- `.trellis/spec/web/styling-guidelines.md` — pure CSS, BEM, token usage, no new page-local CSS files.
- `.trellis/spec/web/state-and-data.md` — API client, snake_case model shape, security and evidence boundaries.
- `.trellis/spec/web/quality-guidelines.md` — lint/test/build expectations and colocated page tests.
- `docs/design/v2-houfeng/design-language.md` — visual authority and frontend-only design constraints.
- `docs/design/v2-houfeng/component-spec.md` — TargetDetailPage section template and page composition expectations.

## Caveats / Not Found

- The audit used current route/page/spec/journal evidence; no external documentation was needed.
- No implementation files were modified by this research.
- TargetDetail recommendation assumes the implementation phase keeps scope to IA composition/copy/CSS/tests and does not change Target, ProbeItem, runtime action, event, or incident API contracts.
