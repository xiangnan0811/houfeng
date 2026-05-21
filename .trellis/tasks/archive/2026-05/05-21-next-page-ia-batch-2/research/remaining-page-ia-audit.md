# Research: remaining page IA audit

- **Query**: Inspect current routes/pages and archived Trellis IA tasks to identify which remaining Houfeng frontend page or page group has the highest value for a limited, low-risk information architecture polish after TargetDetailPage; produce a candidate matrix and recommended next scope.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Active SPA route inventory: Dashboard, VPS, VPS detail, Providers, Subscriptions, AssetDecisions, Nodes, NodeCompare, NodeDetail, NodeOnboarding, Targets, TargetDetail, Events, Settings, Login. |
| `web/src/app/metadata.ts` | Primary navigation IA groups: 总览, 资产, 观测, 系统. |
| `web/src/app/layout/Sidebar.tsx` | Navigation group rendering and anomaly count badge boundaries. |
| `web/src/app/layout/Breadcrumb.tsx` | Breadcrumb behavior for detail/nested routes; root and first-level routes intentionally stay uncrumbed. |
| `web/src/pages/ProvidersPage.tsx` / `web/src/pages/ProvidersPage.test.tsx` | Provider master-data page with summary panel, DataTable, create/edit drawers, validation, and payload/reset tests. |
| `web/src/pages/SubscriptionsPage.tsx` / `web/src/pages/SubscriptionsPage.test.tsx` | Subscription evidence page with URL filters, VPS-context create drawer, summary/prerequisite panels, DataTable, create/edit drawers, and payload/reset tests. |
| `web/src/pages/LoginPage.tsx` / `web/src/pages/LoginPage.test.tsx` | Small auth gate with username/password form, `next` redirect, generic error, and single-user phrasing guard. |
| `web/src/pages/DashboardPage.tsx` / `web/src/pages/DashboardPage.test.tsx` | Dashboard command surface and workbench; tests assert recent IA simplification and deep links. |
| `web/src/pages/EventsPage.tsx` / `web/src/pages/EventsPage.test.tsx` | Events diagnostic surface with URL-backed filters, draft filter drawer, event stream, load-more, and backfill/maintenance semantics. |
| `web/src/pages/AssetDecisionsPage.tsx` / `web/src/pages/AssetDecisionsPage.test.tsx` | Asset decision queue with renewal window, queue tabs, Drawer work panel, evidence table, row navigation isolation, and evidence failure tests. |
| `web/src/pages/VPSPage.tsx` / `web/src/pages/VPSPage.test.tsx` | Recently polished VPS inventory page with quick views, evidence-aware filters, subscription evidence handling, DataTable, and create drawer contracts. |
| `web/src/pages/NodeOnboardingPage.tsx` / `web/src/pages/NodeOnboardingPage.test.tsx` | Recently polished safety-sensitive onboarding page with center-issued command, token secrecy, conflict handling, and manual fallback contracts. |
| `web/src/pages/TargetDetailPage.tsx` / `web/src/pages/target-detail/*` / `web/src/pages/TargetDetailPage.test.tsx` | Recently completed Target detail IA pass; now out of scope for this next batch except as precedent. |
| `.trellis/spec/web/directory-structure.md` | Frontend file placement and route/page/test/component/API boundaries. |
| `.trellis/spec/web/component-conventions.md` | PageState, Drawer reset/focus, DataTable row-click, and interactive composition conventions. |
| `.trellis/spec/web/styling-guidelines.md` | Pure CSS/token/BEM constraints; no new page CSS files except existing Login exception. |
| `.trellis/spec/web/state-and-data.md` | API/data/security boundaries, Node onboarding command/token trust boundary, Dashboard/Event/Asset evidence boundaries. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification expectations and colocated page tests. |
| `docs/design/v2-houfeng/design-language.md` | Visual authority: dark-first, dense engineering tool, page hierarchy, no big-screen/KPI-wall drift. |
| `docs/design/v2-houfeng/component-spec.md` | Current page/component templates for command surfaces, queues, inventory lists, details, drawers, tables, and state sections. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-remaining-page-information-architecture/prd.md` | Archived IA task that selected Providers/Subscriptions at that time and identified TargetDetail as a later major detail gap. |
| `.trellis/tasks/archive/2026-05/05-20-next-page-ia-batch/prd.md` | Archived NodeOnboardingPage safety-frozen IA batch. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch/prd.md` | Archived TargetDetailPage IA batch, now completed/recent and therefore excluded from the next scope. |

### Code Patterns

- Active routes are centralized in `web/src/app/router.tsx`; routed candidates after the authenticated shell are Dashboard, VPS, VPSDetail, Providers, Subscriptions, AssetDecisions, Nodes, NodeCompare, NodeDetail, NodeOnboarding, Targets, TargetDetail, Events, and Settings. The active task asks for another frontend-only IA batch, so route inventory is the selection baseline.
- The active PRD freezes this batch to IA composition/copy/CSS/tests and requires preserving URL state, row navigation, Drawer/modal draft/apply/discard, create/update payloads, and destructive confirmations (`.trellis/tasks/05-21-next-page-ia-batch-2/prd.md:24-36`).
- Provider master-data behavior is already contract-tested: create and edit drawers build stable provider payloads, validate name/rating locally, and reset drafts on cancel (`web/src/pages/ProvidersPage.test.tsx`). The page already has summary evidence, a DataTable, and create/edit drawers (`web/src/pages/ProvidersPage.tsx`).
- Subscription behavior is already contract-tested around URL filters, `vps_id` context, `create=1`, Drawer reset, and create/update payloads that exclude backend-computed `monthly_price` (`web/src/pages/SubscriptionsPage.test.tsx`). The page already frames renewal and cost evidence with filters, prerequisite state, table, and drawers (`web/src/pages/SubscriptionsPage.tsx`).
- Dashboard has a narrow command-surface/workbench composition, and tests assert removal of prior extra IA sections such as `系统全局指标`, `Dashboard 摘要指标`, `系统快捷入口`, `Group 摘要`, and `最近事件摘要` (`web/src/pages/DashboardPage.test.tsx`). This makes another broad Dashboard polish low-upside and higher churn.
- EventsPage has dense URL/filter contracts: parsed URL filters, canonicalization, dynamic time windows, backfill inclusion, draft Drawer apply/reset/close/Escape discard, grouping, load-more, empty, and error states (`web/src/pages/EventsPage.tsx`, `web/src/pages/EventsPage.test.tsx`). This page is already an evidence-oriented diagnostic timeline.
- AssetDecisionsPage already follows the v2 unified work queue pattern: renewal-window controls, queue tabs, Drawer work panel, row navigation/action isolation, and subscription evidence failure handling are tested (`web/src/pages/AssetDecisionsPage.tsx`, `web/src/pages/AssetDecisionsPage.test.tsx`).
- LoginPage is intentionally small. It preserves username/password auth, `next` redirect, generic error copy, and test coverage that avoids single-user phrasing (`web/src/pages/LoginPage.tsx`, `web/src/pages/LoginPage.test.tsx`). The hard-coded footer `v1.0` is a truthfulness cleanup opportunity but not enough for a meaningful IA batch.
- Archived IA tasks show the major high-yield surfaces were already handled: Providers/Subscriptions, TargetDetail, Targets/Nodes lists, NodeCompare, NodeOnboarding, VPS inventory, Dashboard/Events/AssetDecisions evidence patterns, NodeDetail/VPSDetail/Settings. Remaining safe work should therefore prefer a small coherent support group rather than revisiting recently polished/high-risk pages.

### Candidate Matrix

| Candidate | Current IA pain / opportunity | Risk | Likely touched files | Frozen contracts |
|---|---|---:|---|---|
| `ProvidersPage` | Low but tangible. Master-data page already has summary + table + drawers; could better frame it as provider/account evidence and reduce generic CRUD feel. | Low | `web/src/pages/ProvidersPage.tsx`, `web/src/pages/ProvidersPage.test.tsx`, `web/src/styles/pages.css` | Preserve `listProviders`, create/update request shapes, local name/rating validation, labels/rating semantics, create/edit Drawer draft reset/cancel, table action behavior. |
| `SubscriptionsPage` | Low-medium. Already has renewal/cost evidence, filters, VPS-context create drawer, prerequisite panel, and tests; polish could clarify renewal evidence vs cost/account metadata and improve default scan path. | Low-Medium | `web/src/pages/SubscriptionsPage.tsx`, `web/src/pages/SubscriptionsPage.test.tsx`, `web/src/styles/pages.css` | Preserve URL filters (`vps_id`, `status`, `renew_within_days`, `create=1`), list query params, create/update payloads, backend-computed `monthly_price` exclusion, Drawer reset/cancel, VPS prerequisite behavior. |
| `ProvidersPage` + `SubscriptionsPage` group | Best remaining coherent low-risk scope. Both are Asset Ledger support/evidence pages, share master-data/evidence semantics, and can be polished together without touching backend/API. Upside is limited but clearer than a single tiny page. | Medium | Provider/subscription page files, tests, and shared `pages.css` only | Same frozen contracts as individual pages; do not add cross-page joins, new API fields, new asset health facts, or new dependencies. |
| `LoginPage` | Very small auth gate. Possible stale `v1.0` footer/truthfulness cleanup, but not enough IA surface for a batch. | Low-Medium | `web/src/pages/LoginPage.tsx`, `web/src/pages/LoginPage.test.tsx`, existing `web/src/pages/LoginPage.css` if needed | Preserve auth/session flow, `next` redirect, generic error handling, no single-user phrasing, no exposure of auth internals. |
| `DashboardPage` | High-value page but already command-surface/workbench aligned and explicitly tested against old extra sections. Further IA work likely becomes copy churn. | Medium-High | Not recommended | Preserve dashboard summary trust boundaries, deep links, refresh behavior, first-run state, and do not expose notification secrets or overstate `snapshot_generated_at`. |
| `EventsPage` | Already strong diagnostic/event-evidence page with URL-backed filters and draft Drawer. IA pain is low; regression surface is high. | High | Not recommended | Preserve URL canonicalization, time-range query derivation, `include_backfilled` semantics, filter Drawer draft/apply/discard, load-more/grouping, maintenance/backfilled evidence copy. |
| `AssetDecisionsPage` | Already unified work queue with renewal window, evidence failure guards, Drawer decision workflow, and row/action isolation. | Medium-High | Not recommended | Preserve renewal-window loading, queue semantics, decision PATCH payload, subscription evidence failure boundary, row navigation/action isolation, Drawer discard behavior. |
| `VPSPage` | Recently polished inventory; tests already guard lens/filter/evidence/create-row navigation contracts. | Medium | Not recommended | Preserve subscription evidence unknown/failure semantics, filter/create Drawer contracts, list/create payloads, row navigation. |
| `NodeOnboardingPage` | Recently polished and security-sensitive. Any further work risks command/token/binding contract churn. | High | Not recommended | Preserve center-issued install command only, no browser-origin command synthesis, token secrecy, placeholders, command reveal/copy/regenerate, binding conflict endpoints/confirmations/masked fingerprints. |
| `TargetDetailPage` | Just completed/recent IA batch; out of scope for this immediate next page selection. | Medium | Not recommended | Preserve Target/Probe/runtime/incident/event/history contracts from completed batch. |
| `NodesPage` + `TargetsPage` | Recently completed list-control pass; current task context treats them as already polished. | Medium | Not recommended | Preserve URL/list filters, row navigation, create/update payloads, Drawer state. |
| `NodeComparePage` | Recently polished A/B compare page with command panel, identity pair, summary, and metric comparison. | Low | Not recommended | Preserve two-id query contract and runtime facts calls. |
| `SettingsPage` | Recent IA scope per current task context; security/config-sensitive because notification secrets and center settings are involved. | Medium-High | Not recommended | Preserve settings payloads, secret handling, notification behavior, retention and threshold semantics. |
| `AppShell` / nav metadata | Global shell/nav already groups primary work surfaces. Broad changes would create cross-page churn for little IA gain. | Medium | Not recommended | Preserve auth shell, dashboard summary fetch, critical alert links, nav route labels/groups, anomaly count semantics. |

### Recommended Next Scope

Recommend selecting **ProvidersPage + SubscriptionsPage** as a small, coherent Asset Ledger support/evidence IA polish group.

Proposed limited scope:

1. Treat `ProvidersPage` as provider/account master-data evidence rather than generic CRUD: clarify summary strip, table framing, empty states, and drawer copy without changing fields or payloads.
2. Treat `SubscriptionsPage` as renewal/cost evidence: clarify current filter context, renewal horizon, VPS-context create path, prerequisite/empty/error states, and drawer framing without changing URL semantics or payloads.
3. Keep the group intentionally light because both pages already have recent baseline structure; do not expand into VPS inventory, AssetDecisions, or cross-page joins.
4. Touch only `ProvidersPage.tsx`, `ProvidersPage.test.tsx`, `SubscriptionsPage.tsx`, `SubscriptionsPage.test.tsx`, and `web/src/styles/pages.css` unless an existing shared atom needs no-behavior class reuse.
5. Add/update tests for user-visible IA text/structure plus frozen contract assertions that are easy to regress: provider payload/reset, subscription URL `create=1`, `monthly_price` exclusion, and Drawer discard behavior.

### External References

- None. This was an internal code/spec/task audit only.

### Related Specs

- `.trellis/spec/web/directory-structure.md` — route/page/test/component/API file ownership and no page-to-page imports.
- `.trellis/spec/web/component-conventions.md` — `PageState`, Drawer reset/focus, DataTable row-click, and interactive composition contracts.
- `.trellis/spec/web/styling-guidelines.md` — pure CSS, tokens, BEM-ish classes, and no new page-local CSS files.
- `.trellis/spec/web/state-and-data.md` — frontend API/data/security boundaries, including Asset Ledger evidence truthfulness and URL/filter semantics.
- `.trellis/spec/web/quality-guidelines.md` — lint/test/build expectations and colocated page tests.
- `docs/design/v2-houfeng/design-language.md` — dark-first, high-density engineering-tool visual and hierarchy guidance.
- `docs/design/v2-houfeng/component-spec.md` — inventory/list/workbench/drawer component patterns to reuse without adding new systems.

## Caveats / Not Found

- The recommended group is intentionally low-risk and moderate/limited upside; most high-yield pages have already been polished or are too security/URL/evidence-sensitive for speculative churn.
- No external documentation was needed.
- No implementation files were modified by this research.
- If the main agent wants a higher-impact but higher-risk scope despite this audit, the least-bad alternatives are Dashboard/Events/AssetDecisions targeted copy-only fixes, but current code/tests suggest they are already aligned and should be deferred absent a concrete defect.
