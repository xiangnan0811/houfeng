# Research: design-spec candidate audit

- **Query**: Research v2 design/component-spec gaps for remaining Houfeng frontend pages after SettingsPage shipped as v0.16.0. Compare `docs/design/v2-houfeng/design-language.md`, `docs/design/v2-houfeng/component-spec.md`, relevant `.trellis/spec/web/*.md`, and current `web/src/pages/**` implementation/tests. Identify the highest-value remaining frontend-only IA/page hierarchy mismatch suitable for one PR.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `docs/design/v2-houfeng/design-language.md` | v2 design language: dark-first, density, default page hierarchy, loading/error/empty, no new frameworks/libraries. |
| `docs/design/v2-houfeng/component-spec.md` | v2 visual/page contracts for Dashboard, Asset Decisions, VPS, VPS Detail, Nodes, Node Detail, Events, Settings, Targets, Target Detail, Node Onboarding, Login. |
| `.trellis/spec/web/component-conventions.md` | Current component/page split, drawer/focus contracts, PageState primitive, DataTable interaction contracts. |
| `.trellis/spec/web/directory-structure.md` | Current route/page organization and co-located page test expectations. |
| `.trellis/spec/web/state-and-data.md` | Frontend data-flow contracts, including Dashboard, Asset Ledger, Subscriptions URL context, and VPS detail data flow. |
| `.trellis/spec/web/styling-guidelines.md` | CSS/token/BEM style constraints and v2 visual authority pointers. |
| `.trellis/spec/web/quality-guidelines.md` | Current verification and page-test expectations. |
| `web/src/app/router.tsx` | Registered frontend routes; includes `providers`, `subscriptions`, and `nodes/compare`, which are not covered by v2 page templates. |
| `web/src/app/metadata.ts` | Sidebar IA groups; `providers` and `subscriptions` are primary Asset nav items. |
| `web/src/pages/SubscriptionsPage.tsx` | Subscription evidence page implementation with hero, optional context/prerequisite panels, summary panel, inline filter panel, table, create/edit drawers. |
| `web/src/pages/SubscriptionsPage.test.tsx` | Tests for subscriptions rendering, renew-window filter, create/edit drawer behavior, URL `vps_id` + `create=1`, and error handling. |
| `web/src/pages/ProvidersPage.tsx` | Provider master-data page implementation with hero, summary panel, table, create/edit drawers. |
| `web/src/pages/ProvidersPage.test.tsx` | Tests for provider list/create/edit drawers, empty/error states, and draft reset. |
| `web/src/pages/NodeComparePage.tsx` | Utility route for two-node A/B comparison; command panel, identity cards, summary strip, metric comparison. |
| `web/src/pages/NodeComparePage.test.tsx` | Tests for empty selection, A/B summary, command context, metric placeholders, and error states. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset Decisions implementation; aligns with v2 page template as the primary Asset Ledger work queue. |
| `web/src/pages/VPSPage.tsx` | VPS inventory implementation; aligns with v2 page template using inventory lens, FilterBar chips, filter drawer, and DataTable. |
| `web/src/pages/DashboardPage.tsx` + `web/src/pages/dashboard/*` | Dashboard implementation; split into command surface and workbench matching v2 template. |
| `web/src/pages/NodesPage.tsx` + `web/src/pages/nodes/*` | Nodes implementation; split into hero, support surface, toolbar/list sections matching v2 template. |
| `web/src/pages/TargetsPage.tsx` + `web/src/pages/targets/*` | Targets implementation; support surface and DataTable patterns matching v2 template. |
| `web/src/pages/EventsPage.tsx` + `web/src/pages/events/*` | Events implementation; support surface, filter overview/drawer, and stream section matching v2 template. |
| `web/src/pages/SettingsPage.tsx` + `web/src/pages/settings/*` | Settings implementation shipped as v0.16.0; no longer the best next IA candidate. |

### Code Patterns

#### Design/spec baseline used for comparison

- `docs/design/v2-houfeng/design-language.md:147-170` defines the page hierarchy rhythm: page identity, current problem, trend/context, history/events, danger. It says not every page uses all five levels, but order is fixed.
- `docs/design/v2-houfeng/design-language.md:232-262` defines shared Loading / Error / Empty expectations; `.trellis/spec/web/component-conventions.md:44` confirms pages should prefer the shared `PageState` primitive for route/detail/list loading/error/empty states.
- `docs/design/v2-houfeng/component-spec.md:200-345` lists page templates. It includes Dashboard, AssetDecisions, VPS, VPSDetail, Nodes, NodeDetail, Events, Settings, Targets, TargetDetail, NodeOnboarding, and Login. It does not define page templates for ProvidersPage, SubscriptionsPage, or NodeComparePage.
- `web/src/app/router.tsx:71-93` registers all route pages. The routes without page-template coverage are:
  - `/providers` at `web/src/app/router.tsx:74`
  - `/subscriptions` at `web/src/app/router.tsx:75`
  - `/nodes/compare` at `web/src/app/router.tsx:81`
- `web/src/app/metadata.ts:23-29` makes `续费决策`, `VPS`, `服务商`, and `订阅` primary `资产` sidebar items. This raises the value of Providers/Subscriptions IA coverage relative to the utility-only compare route.

#### Pages already aligned with explicit v2 page templates

- Dashboard: `docs/design/v2-houfeng/component-spec.md:202-220` requires an asset-decision-first command surface plus a single workbench. Current implementation composes `DashboardCommandSurface` and `DashboardWorkbench` in `web/src/pages/DashboardPage.tsx:96-120`. `DashboardCommandSurface` renders the h1 command surface and three lanes in `web/src/pages/dashboard/DashboardCommandSurface.tsx:468-688`; `DashboardWorkbench` renders the stateful workbench in `web/src/pages/dashboard/DashboardWorkbench.tsx:54-97`.
- Asset Decisions: `docs/design/v2-houfeng/component-spec.md:221-227` requires a unified `资产决策工作队列` and secondary `RENEWAL EVIDENCE`. Current implementation has the queue surface at `web/src/pages/AssetDecisionsPage.tsx:570-680`, Tabs at `web/src/pages/AssetDecisionsPage.tsx:622-625`, secondary renewal evidence at `web/src/pages/AssetDecisionsPage.tsx:682-712`, and Drawer decision handling at `web/src/pages/AssetDecisionsPage.tsx:714-731`.
- VPS: `docs/design/v2-houfeng/component-spec.md:228-234` requires quick views, chips/drawer filters, and `VPS 库存表`. Current implementation has inventory lens/Tabs at `web/src/pages/VPSPage.tsx:713-737`, filter chips plus advanced filter Drawer entry at `web/src/pages/VPSPage.tsx:761-787`, table at `web/src/pages/VPSPage.tsx:797-838`, create Drawer at `web/src/pages/VPSPage.tsx:841-934`, and filter Drawer at `web/src/pages/VPSPage.tsx:936-979`.
- Nodes: `docs/design/v2-houfeng/component-spec.md:243-256` requires `节点观测`, an `资产判断支撑` support surface, filtering, and compact DataTable. Current implementation renders `NodesHero` at `web/src/pages/NodesPage.tsx:662-672`, `NodesSupportSurface` at `web/src/pages/NodesPage.tsx:674-691`, CreateNodeDrawer at `web/src/pages/NodesPage.tsx:693-706`, and the toolbar/list frame at `web/src/pages/NodesPage.tsx:708-770`.
- Events: `docs/design/v2-houfeng/component-spec.md:282-290` requires a diagnostic support surface, filter overview with Drawer, and event stream. Current implementation renders hero at `web/src/pages/EventsPage.tsx:373-381`, support surface at `web/src/pages/EventsPage.tsx:383-392`, filter overview at `web/src/pages/EventsPage.tsx:394-400`, filter drawer at `web/src/pages/EventsPage.tsx:402-410`, and stream section at `web/src/pages/EventsPage.tsx:412-419`.
- Targets: `docs/design/v2-houfeng/component-spec.md:301-308` requires `入口观测`, support surface, filters, compact DataTable, and create Drawer. Current implementation renders hero at `web/src/pages/TargetsPage.tsx:570-588`, create Drawer at `web/src/pages/TargetsPage.tsx:590-604`, support surface at `web/src/pages/TargetsPage.tsx:606-627`, list control band at `web/src/pages/TargetsPage.tsx:629-651`, filters/batch/table at `web/src/pages/TargetsPage.tsx:667-715`.
- Settings: `docs/design/v2-houfeng/component-spec.md:291-299` now has an explicit SettingsPage template. Current `web/src/pages/SettingsPage.tsx:500-628` implements a hero, tabs, notification group, section components, and save footer. Since SettingsPage has just shipped as v0.16.0, it is not the highest-value next candidate.

#### Remaining uncovered page-template gaps

1. **SubscriptionsPage**
   - Primary nav placement: `web/src/app/metadata.ts:23-29` puts `订阅` in the top-level Asset group.
   - Current hierarchy is: hero (`web/src/pages/SubscriptionsPage.tsx:446-459`), optional VPS context panel (`web/src/pages/SubscriptionsPage.tsx:461-477`), optional prerequisite panel (`web/src/pages/SubscriptionsPage.tsx:479-490`), summary/command panel (`web/src/pages/SubscriptionsPage.tsx:493-521`), separate filter panel (`web/src/pages/SubscriptionsPage.tsx:523-551`), table panel (`web/src/pages/SubscriptionsPage.tsx:553-620`), create/edit drawers (`web/src/pages/SubscriptionsPage.tsx:622-761`).
   - Tests assert this hierarchy and current copy: `web/src/pages/SubscriptionsPage.test.tsx:84-91` checks `续费与成本证据`, `当前筛选上下文`, `订阅续费证据表`, `当前筛选`, `下一笔续费证据`, and URL-truth copy. `web/src/pages/SubscriptionsPage.test.tsx:164-191` covers URL-requested create drawer and `vps_id` context. `web/src/pages/SubscriptionsPage.test.tsx:295-317` covers subscription error state without treating it as missing data.
   - `.trellis/spec/web/state-and-data.md:147-203` contains detailed data contracts for Asset Ledger list and decision queues, including SubscriptionsPage tests (`SubscriptionsPage.test.tsx`: URL context, `create=1`, prefill, close behavior). However, `docs/design/v2-houfeng/component-spec.md:200-345` has no visual/page-template section for SubscriptionsPage.
   - Mismatch shape: SubscriptionsPage is a primary Asset route with detailed state/data contracts but no v2 component/page template. Its current page hierarchy has multiple same-weight `page-panel` surfaces before the work table (`当前 VPS 上下文`, `续费与成本证据`, `当前筛选上下文`), while adjacent v2 Asset pages define a clearer command/lens surface + DataTable path.

2. **ProvidersPage**
   - Primary nav placement: `web/src/app/metadata.ts:23-29` puts `服务商` in the top-level Asset group.
   - Current hierarchy is compact: hero (`web/src/pages/ProvidersPage.tsx:274-287`), master-data context panel (`web/src/pages/ProvidersPage.tsx:289-317`), evidence table (`web/src/pages/ProvidersPage.tsx:319-384`), create/edit drawers (`web/src/pages/ProvidersPage.tsx:386-477`).
   - Tests assert current copy and drawer behavior: `web/src/pages/ProvidersPage.test.tsx:42-49` checks `服务商主数据概览`, `服务商账号证据表`, and boundary copy; `web/src/pages/ProvidersPage.test.tsx:87-107` checks empty state and drawer draft reset; `web/src/pages/ProvidersPage.test.tsx:193-209` checks error state with retry action.
   - There is no explicit ProvidersPage visual template in `docs/design/v2-houfeng/component-spec.md:200-345`. It is a spec gap, but current implementation is already small and follows the same hero → context → DataTable → Drawer shape.

3. **NodeComparePage**
   - Registered route: `web/src/app/router.tsx:81`.
   - Current hierarchy is command panel (`web/src/pages/NodeComparePage.tsx:150-168`), two identity cards (`web/src/pages/NodeComparePage.tsx:92-95`, implementation `web/src/pages/NodeComparePage.tsx:319-378`), summary strip (`web/src/pages/NodeComparePage.tsx:211-227`), and metric DetailSection (`web/src/pages/NodeComparePage.tsx:99-127`).
   - Tests assert current hierarchy: `web/src/pages/NodeComparePage.test.tsx:120-143` checks h1 `判断两个 Node 是否需要深入排查`, `A/B 摘要判断`, object labels, health/lifecycle/runtime/binding/context/sample rows, and metric placeholders.
   - There is no explicit NodeComparePage visual template in `docs/design/v2-houfeng/component-spec.md:200-345`. It is a route-template gap, but it is not in the sidebar primary nav and depends on two selected Node IDs, making it a lower-value utility route for a broad IA batch.

### Highest-value one-PR candidate

**Recommended candidate: SubscriptionsPage IA/page hierarchy.**

Reasoning:

- It is a primary Asset sidebar route (`web/src/app/metadata.ts:23-29`), unlike NodeComparePage.
- It carries important state/data contracts and tests around `vps_id`, `create=1`, renewal-window filtering, error boundaries, selector use, and drawer state (`.trellis/spec/web/state-and-data.md:147-203`; `web/src/pages/SubscriptionsPage.test.tsx:69-350`).
- It is not covered by the v2 page-template section (`docs/design/v2-houfeng/component-spec.md:200-345`), unlike Dashboard, Asset Decisions, VPS, Nodes, Events, Targets, Settings, and detail pages.
- Its current hierarchy has the clearest page-level mismatch among remaining gaps: multiple same-weight pre-table panels (`当前 VPS 上下文`, `续费与成本证据`, `当前筛选上下文`) at `web/src/pages/SubscriptionsPage.tsx:461-551`, followed by the actual table at `web/src/pages/SubscriptionsPage.tsx:553-620`. Adjacent Asset pages use a stronger command/lens + table path (`AssetDecisionsPage.tsx:570-712`, `VPSPage.tsx:713-838`).
- The scope is frontend-only: implementation can stay within `web/src/pages/SubscriptionsPage.tsx`, `web/src/pages/SubscriptionsPage.test.tsx`, and shared page CSS/classes if needed; no backend/API/data-shape change is indicated by the current contracts.

### Related Specs

- `.trellis/spec/web/component-conventions.md:44-50` — PageState, Drawer focus/reset contracts, and selector-over-internal-ID rule.
- `.trellis/spec/web/directory-structure.md:114-121` — pages are route assembly points and have co-located tests.
- `.trellis/spec/web/state-and-data.md:147-203` — Asset Ledger list/decision queue contracts and required SubscriptionsPage tests for `vps_id` URL context and `create=1`.
- `.trellis/spec/web/styling-guidelines.md:21-34` — v2 design docs are the active visual authority.
- `.trellis/spec/web/quality-guidelines.md:78-85` — every route page has co-located tests, and changed page behavior should be tested.

### External References

No external references used; this was an internal code/spec audit.

## Caveats / Not Found

- No `docs/design/v2-houfeng/component-spec.md` page template was found for `ProvidersPage`, `SubscriptionsPage`, or `NodeComparePage`.
- No backend/API change appears necessary for the recommended SubscriptionsPage IA candidate; the current tests and `.trellis/spec/web/state-and-data.md` already define the relevant data contracts.
- This audit did not run visual/browser sanity or the test suite; it only compared current source/tests/spec documents.
