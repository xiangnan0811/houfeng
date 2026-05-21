# Research: Design/spec candidate audit for next page IA batch

- **Query**: Research design/spec fit for the next Houfeng frontend page IA batch after TargetDetailPage; recommend which remaining page/page group is under-aligned with Houfeng v2 visual/component language and safe to polish next, preserving API/data/security contracts.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `docs/design/v2-houfeng/design-language.md` | Active visual authority: dark-first, high-density engineering tool, state language, page hierarchy, PageState expectations, and no backend/API changes for visual work. |
| `docs/design/v2-houfeng/component-spec.md` | Active component/page visual contract; explicitly specifies Dashboard, AssetDecisions, Events, Settings, Targets, TargetDetail, NodeOnboarding, Login, VPS/VPSDetail, Nodes/NodeDetail. |
| `.trellis/spec/web/styling-guidelines.md` | Web styling contract: tokens/BEM/global CSS, no new per-page CSS except LoginPage, PageState reuse, known Settings inline-style gap. |
| `.trellis/spec/web/component-conventions.md` | Web component/page contract: route pages as assemblers, named exports, API via `lib/api.ts`, `PageState`, `Drawer`, `DataTable`, no page-to-page imports, known oversized-page debt. |
| `.trellis/spec/web/state-and-data.md` | Data-flow contracts for Dashboard, Asset Ledger pages, Events filters/backfilled events, onboarding/install command, and no invented facts. |
| `.trellis/tasks/05-21-next-page-ia-batch-2/prd.md` | Current task context: already completed NodeDetail, VPSDetail, Settings, Targets+Nodes lists, NodeCompare, VPSPage inventory, NodeOnboarding, TargetDetailPage; current batch must be frontend-only, low-risk, and contract-preserving. |
| `.trellis/tasks/archive/2026-05/05-20-continue-remaining-page-information-architecture-optimization/research/remaining-pages-ia-audit.md` | Prior IA audit; now partly stale because task PRD says Settings, NodeCompare, VPSPage inventory, and NodeOnboarding have since been polished. Still useful for past ranking and contract notes. |
| `web/src/pages/DashboardPage.tsx` | Current dashboard shell; loads `getDashboard`, derives state, renders command surface and workbench. |
| `web/src/pages/dashboard/DashboardCommandSurface.tsx` | Dashboard command surface with asset decision lane, observability lane, next actions, refresh, auto-refresh, StatusGlyph/Badge/Mono/Timestamp usage. |
| `web/src/pages/dashboard/DashboardWorkbench.tsx` | Dashboard workbench that switches between first-run, abnormal queue, maintenance, and normal overview inside `DetailSection`. |
| `web/src/pages/EventsPage.tsx` | Events page with URL-state parser/normalizer, advanced filter drawer, support surface, filter overview, event stream, and load-more behavior. |
| `web/src/pages/events/EventsSupportSurface.tsx` | Diagnostic timeline support surface with current slice, object context, severity/type, time/source lanes, and prioritized event evidence. |
| `web/src/pages/events/EventsFilterOverview.tsx` | Events filter condition `DetailSection` with `FilterBar`, chips, and advanced-filter action. |
| `web/src/pages/events/EventsStreamSection.tsx` | Events timeline section with date buckets, `EventList`, empty action, and load-more button. |
| `web/src/pages/AssetDecisionsPage.tsx` | Unified asset decision work queue with renewal window, queue tabs, priority rows, drawer work panel, and renewal evidence table. |
| `web/src/pages/AssetDecisionsPage.test.tsx` | Focused coverage for queue loading, renewal-window reloads, decision updates, empty states, row/action isolation, and drawer behavior. |
| `web/src/pages/ProvidersPage.tsx` | Service provider master-data page: summary panel, DataTable, create/edit drawers, local validation, API helper usage. |
| `web/src/pages/ProvidersPage.test.tsx` | Coverage for create/update payloads, draft reset, validation, and rendered provider overview. |
| `web/src/pages/SubscriptionsPage.tsx` | Subscription evidence page: URL filters, VPS context/create deep link, renewal/cost summary, DataTable, create/edit drawers. |
| `web/src/pages/SubscriptionsPage.test.tsx` | Coverage for URL filters, `create=1` context, create/update payloads, and drawer reset. |
| `web/src/pages/LoginPage.tsx` | Small login route with historical dedicated CSS exception and low IA surface. |
| `web/src/styles/pages.css` | Shared page/asset styles including `asset-workbench-summary`, `asset-table`, decision queue, support surfaces, and page panels. |

### Design / Spec Constraints

- `docs/design/v2-houfeng/design-language.md:147-170` defines page density and hierarchy: high information density, 8pt rhythm, page identity, current problem area, context/trend/history, danger-zone ordering.
- `docs/design/v2-houfeng/design-language.md:232-249` requires consistent loading/error/empty handling; implementation should reuse `PageState` rather than bespoke panels where applicable.
- `docs/design/v2-houfeng/design-language.md:312-325` freezes IA polish boundaries: no new CSS framework, chart library, backend/API/data-shape changes, i18n, or broad responsive work.
- `.trellis/spec/web/styling-guidelines.md:95-111` limits page styling to global style files (`pages.css`, `atoms.css`) and forbids new page-local CSS except LoginPage.
- `.trellis/spec/web/component-conventions.md:38-50` requires pages to assemble data/state, use `PageState`, use Drawer focus/state reset semantics, and keep association forms as selectors rather than raw internal ID inputs.
- `.trellis/spec/web/state-and-data.md:104-114` is strict for Dashboard: only show `getDashboard()` facts, do not turn all returned fields into first-screen KPI strips, and do not interpret `snapshot_generated_at` as center/agent health.
- `.trellis/spec/web/state-and-data.md:153-172` is strict for Asset Ledger list/decision pages: derived joins are allowed, but linked-node health may not be invented on list records; `/subscriptions?vps_id=<id>&create=1` must remain a landed context.
- `.trellis/spec/web/state-and-data.md:469-514` is strict for Events: applied URL filters are request truth; drawer draft changes must not update URL or fetch until apply/reset; `include_backfilled=1` maps to API `include_backfilled=true`.
- Current task PRD `prd.md:9-18` narrows the candidate set: NodeCompare, Settings, VPSPage inventory, NodeOnboarding, and TargetDetailPage are already recent; current batch should stay frontend-only and avoid API/data/security changes.

### Candidate Fit Analysis

#### 1. Providers + Subscriptions asset-support pages — strongest remaining candidate

Evidence of current implementation:

- `ProvidersPage.tsx:271-355` already uses the current asset-page skeleton: page hero, a `providers-command-panel`, `asset-workbench-summary`, then `DataTable` with `PageState` loading/error states.
- `ProvidersPage.tsx:356-440` uses separate create/edit `Drawer`s and clears draft/errors on close/cancel, matching drawer-reset expectations.
- `SubscriptionsPage.tsx:443-580` uses page hero, optional current-VPS context panel, prerequisite panel, renewal/cost evidence summary, visible filters, and `DataTable`.
- `SubscriptionsPage.tsx:582-715` uses create/edit `Drawer`s and selector-based VPS association rather than asking for raw internal IDs.
- `SubscriptionsPage.tsx:201-219` preserves URL-requested creation with `create=1` and `vps_id` prefill derived from URL state, rather than effect-driven state sync.
- Shared styling exists for this family: `pages.css:4147-4197` defines `asset-workbench-summary` and `asset-decision-board__summary` with tokenized grid, border, radius, background, and eyebrow styles.

Under-alignment / opportunity:

- `docs/design/v2-houfeng/component-spec.md` has explicit templates for `AssetDecisionsPage` (`:221-227`), `VPSPage` (`:228-234`), and `VPSDetailPage` (`:235-241`), but it does not define dedicated page templates for `ProvidersPage` or `SubscriptionsPage`. They are supporting Asset Ledger pages that currently borrow generic asset-page conventions.
- They are not listed in the task PRD's recently polished set except that older prior audit had considered them recently improved; the current PRD says Settings/NodeCompare/VPSPage inventory/NodeOnboarding/TargetDetail are done, while Providers/Subscriptions are not called out in the same recent-completion list.
- User value is practical: these pages support VPS inventory and asset decision workflows by providing provider master data and subscription evidence. IA polish can improve context/framing while leaving core forms and API helpers intact.
- Safe scope is narrow: keep endpoints, URL filters, `create=1`, selector semantics, create/update payloads, DataTable rows, and drawer reset behavior unchanged; polish only page composition/copy/CSS and tests.

Contract caveats to preserve:

- `ProvidersPage.test.tsx:19-78` asserts `POST /api/providers` payload shape; do not add/remove fields.
- `ProvidersPage.test.tsx:116-174` asserts `PATCH /api/providers/{id}` payload shape; do not alter update semantics.
- `SubscriptionsPage.test.tsx:69-102` asserts `renew_within_days` filter serializes to `/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc`.
- `SubscriptionsPage.test.tsx:157-183` asserts closing `create=1` removes create request but preserves `vps_id` filter/context and resets draft.
- `SubscriptionsPage.test.tsx:214-282` asserts update payload and backend-computed `monthly_price` display; front-end should not compute or send `monthly_price`.

Recommended polish scope:

- Treat as one small page group: `ProvidersPage` + `SubscriptionsPage`.
- Add/clarify asset-support command framing that explains how provider master data and subscription evidence feed VPS inventory / asset decisions.
- Keep main table as primary scan path; do not replace with cards or full-page forms.
- Use existing `page-panel`, `asset-workbench-summary`, `DataTable`, `Drawer`, `FilterBar`, `FilterChip`, `MonoDigits`, `Timestamp`, and existing badge helpers.
- Add tests only for new IA copy/structure and existing frozen contracts.

#### 2. EventsPage — aligned, high regression risk for broad rewrite

Evidence of alignment:

- `EventsPage.tsx:195-422` already follows component-spec `EventsPage` shape: hero, diagnostic support surface, filter overview, advanced drawer, event stream.
- `EventsSupportSurface.tsx:97-252` implements `DIAGNOSTIC TIMELINE`, lane-based context, deep links to abnormal nodes/targets, events filters, VPS, and asset decisions, using `StatusGlyph`, `Badge`, `MonoDigits`, `Hostname`, and shared observability components.
- `EventsFilterOverview.tsx:99-118` wraps filter conditions in `DetailSection`, `FilterBar`, and chips.
- `EventsStreamSection.tsx:80-135` renders a `DetailSection` event stream, grouped timeline buckets, `EventList`, empty action, and load-more control.
- `.trellis/spec/web/state-and-data.md:469-514` is already closely reflected in `EventsPage.tsx:60-184` parse/normalize/serialize/build-query logic and `EventsPage.tsx:232-279` fetch behavior.

Candidate ranking note:

- High user visibility, but broad IA changes risk churn across URL canonicalization, drawer draft/apply semantics, deep links, and test-dense behavior. Only suitable for tiny, specific polish.

#### 3. DashboardPage — aligned and high-value but already v2-specific

Evidence of alignment:

- `DashboardPage.tsx:96-120` renders only `DashboardCommandSurface` and `DashboardWorkbench`, matching command-surface-first design.
- `DashboardCommandSurface.tsx:468-688` implements command title/description, today's judgment summary, asset decision lane, observability lane, next actions, refresh and auto-refresh controls.
- `DashboardWorkbench.tsx:54-98` switches into first-run onboarding, abnormal attention queue, maintenance, or running overview in a single `DetailSection`.
- `.trellis/spec/web/state-and-data.md:104-114` warns not to restore old KPI/group/recent-event expansions; current implementation is already built to avoid that.

Candidate ranking note:

- Keep out of broad next batch unless there is a concrete known issue; changes here are high-value but contract-sensitive because `/api/dashboard` also feeds AppShell summary behavior.

#### 4. AssetDecisionsPage — aligned and already a unified work queue

Evidence of alignment:

- `AssetDecisionsPage.tsx:536-632` renders hero plus `资产决策工作队列` with summary, queue tabs, focus items, queue rows, empty state, and queue notice.
- `AssetDecisionsPage.tsx:634-661` places `续费候选证据` as secondary evidence, matching component spec that renewal evidence should be lower weight than the unified work queue.
- `AssetDecisionsPage.tsx:663-680` uses `Drawer` with `AssetDecisionWorkPanel`.
- `AssetDecisionsPage.test.tsx:90-157` covers unified queue and renewal window API reloads; `:159-206` covers decision update and queue movement; `:208-232` covers empty state actions.

Candidate ranking note:

- Important page but already matches the v2 AssetDecisions template. Avoid broad rewrite; only do issue-driven polish.

#### 5. LoginPage — low IA leverage

Evidence:

- `LoginPage.tsx:31-65` is a small, full-screen login form with seal, brand, motto, error alert, username/password inputs, and submit button.
- `docs/design/v2-houfeng/component-spec.md:339-345` explicitly defines LoginPage's full-screen centered seal/card/form/error contract.
- `.trellis/spec/web/styling-guidelines.md:15` and `:160` document `LoginPage.css` as the only page-local CSS exception and do not require removing it.

Candidate ranking note:

- Safe but low value; not a good next IA batch unless login-specific acceptance criteria appear.

### Candidate Ranking

| Rank | Candidate | Design/spec gap | User value | Implementation risk | Test risk | Recommendation |
|---:|---|---|---|---|---|---|
| 1 | `ProvidersPage` + `SubscriptionsPage` | Medium | Medium-High | Low-Medium | Medium | Best remaining low-risk group: Asset Ledger support pages can receive clearer v2 IA framing without changing contracts. |
| 2 | `EventsPage` tiny polish only | Low | Medium | Medium | High | Already aligned; only adjust if a specific usability issue is identified. |
| 3 | `DashboardPage` tiny polish only | Low | High | Medium | High | Already aligned with command-surface spec; broad changes risk reopening settled dashboard constraints. |
| 4 | `AssetDecisionsPage` tiny polish only | Low | High | Medium | Medium-High | Already matches unified queue spec; avoid unless addressing a concrete gap. |
| 5 | `LoginPage` | Low | Low | Low | Low | Too small/low-leverage for the next IA batch. |

### Recommended Scope

Recommended next batch: **Asset Ledger support pages — `ProvidersPage` + `SubscriptionsPage`**.

Scope boundaries:

- Allowed touch points: `web/src/pages/ProvidersPage.tsx`, `web/src/pages/ProvidersPage.test.tsx`, `web/src/pages/SubscriptionsPage.tsx`, `web/src/pages/SubscriptionsPage.test.tsx`, `web/src/styles/pages.css`.
- Optional if reused without new behavior: existing asset helper components/types already imported by these pages.
- Freeze API calls and payloads: `listProviders`, `createProvider`, `updateProvider`, `listSubscriptions`, `createSubscription`, `updateSubscription`, `listVPSAssets`.
- Freeze URL contract: `/subscriptions?vps_id=<id>`, `status`, `renew_within_days`, and `create=1` semantics.
- Freeze drawer contracts: cancel/Escape/overlay close must discard drafts/errors; create/edit drawers remain secondary surfaces.
- Freeze data semantics: monthly price remains backend-computed; provider edits do not mutate Node provider hints; subscriptions must bind through selectable VPS options.
- Do not add backend fields, page-local CSS, dependencies, charts, or new routes.

Expected outcome:

- Provider master data and subscription evidence read as first-class Asset Ledger support surfaces, not generic CRUD tables.
- Main scan path remains high-density table-first.
- Existing tests continue to assert API/data contracts, with added assertions for new IA framing/copy where needed.

### External References

No external search was needed; request scope was internal design/spec/code fit.

### Related Specs

- `docs/design/v2-houfeng/design-language.md` — visual north star, dark-first/high-density constraints, PageState and no-backend-change boundaries.
- `docs/design/v2-houfeng/component-spec.md` — current page templates; notably lacks dedicated Providers/Subscriptions page templates while adjacent Asset Ledger pages are specified.
- `.trellis/spec/web/styling-guidelines.md` — token/BEM/global CSS constraints.
- `.trellis/spec/web/component-conventions.md` — page assembly, Drawer, DataTable, PageState, and selector conventions.
- `.trellis/spec/web/state-and-data.md` — Dashboard, Asset Ledger, Events, and association-form data contracts.

## Caveats / Not Found

- No dedicated v2 `ProvidersPage` or `SubscriptionsPage` template was found in `docs/design/v2-houfeng/component-spec.md`; this does not prove a historical design never existed.
- The current task PRD says NodeCompare, Settings, VPSPage inventory, NodeOnboarding, and TargetDetailPage were recently polished, so this audit intentionally treats them as out of scope even though older archived research ranked NodeCompare/Settings highly before those later batches.
- This audit did not perform browser visual inspection; it compares source/spec structure only.
