# Research: Remaining frontend pages IA audit

- **Query**: Research the remaining frontend pages for the active Trellis task after IA optimization was completed for Node Detail, VPS Detail, Subscriptions, Providers, Target Detail, and TargetsPage + NodesPage list controls; identify next batch scope options for continued frontend page information architecture optimization.
- **Scope**: internal
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Route registry for all page surfaces; confirms `/`, `/vps`, `/vps/:vpsId`, `/providers`, `/subscriptions`, `/asset-decisions`, `/nodes`, `/nodes/compare`, `/nodes/:nodeId`, `/nodes/:nodeId/onboarding`, `/targets`, `/targets/:targetId`, `/events`, `/settings`. |
| `web/src/pages/NodeComparePage.tsx` | A/B node comparison route; local `useNodeData` loads node identity + runtime facts and renders compare identity cards plus reused watchtower metrics. |
| `web/src/pages/NodeComparePage.test.tsx` | Covers missing second node empty state, A/B identity + runtime placeholder rendering, and one-side load failure. |
| `web/src/components/node-detail/NodeWatchtowerMetrics.tsx` | Reused by node detail and node comparison for host metric cards/charts. |
| `web/src/pages/EventsPage.tsx` | Event/audit timeline page with support surface, filter overview, advanced drawer, URL-state filters, event stream, load-more behavior. |
| `web/src/pages/EventsPage.test.tsx` | Extensive regression coverage for event timeline, filters, deep links, drawer draft behavior, canonicalization, backfilled events, errors, and load more. |
| `web/src/pages/events/EventsSupportSurface.tsx` | Events diagnostic command/support surface and prioritized links into abnormal nodes/targets, asset decisions, and filtered event timelines. |
| `web/src/pages/events/EventsFilterOverview.tsx` | Lightweight filter summary/chip surface for EventsPage. |
| `web/src/pages/events/EventsFilterDrawer.tsx` | Advanced filter drawer for EventsPage; draft/apply/reset behavior. |
| `web/src/pages/events/EventsStreamSection.tsx` | Timeline/event-stream section and load-more control. |
| `web/src/pages/DashboardPage.tsx` | Dashboard command surface and workbench shell using `getDashboard`, `useAutoRefresh`, derived fleet state, attention items, and first-run state. |
| `web/src/pages/DashboardPage.test.tsx` | Broad coverage for command-surface states, first-run onboarding, management entries, links, error state, and negative checks against old KPI-dashboard copy. |
| `web/src/pages/dashboard/DashboardCommandSurface.tsx` | Dashboard hero/command surface with asset lane, observation lane, next actions, refresh, and auto-refresh controls. |
| `web/src/pages/dashboard/DashboardWorkbench.tsx` | Switches between onboarding, attention queue, maintenance overview, and running overview based on derived state. |
| `web/src/pages/dashboard/AttentionQueue.tsx` | Priority queue for abnormal nodes/targets with row/detail action link behavior. |
| `web/src/pages/AssetDecisionsPage.tsx` | Unified Asset Ledger work queue for renewal/decision evidence with queue tabs, drawer work panel, and local queue update behavior. |
| `web/src/pages/AssetDecisionsPage.test.tsx` | Covers unified queue, renewal-window reloads, decision update/movement, empty state, row/action isolation, drawer cancel/Escape/overlay, and subscription evidence failure. |
| `web/src/components/AssetDecisionRenewalTable.tsx` | Supporting renewal evidence table for asset decisions/dashboard-style renewal pressure. |
| `web/src/components/AssetDecisionWorkPanel.tsx` | Drawer work panel for updating VPS renewal decision/evidence. |
| `web/src/pages/NodeOnboardingPage.tsx` | Security-sensitive one-command install and binding lifecycle page; only assessed for safety/risk. |
| `web/src/pages/NodeOnboardingPage.test.tsx` | Extensive coverage for backend-issued install command, token secrecy, hide/reveal/copy, missing center config, binding conflict actions, reset confirmation, and mono metadata. |
| `web/src/pages/VPSPage.tsx` | High-density VPS inventory with quick views, URL-state filters, advanced drawer, subscription/provider joins, and DataTable navigation. |
| `web/src/pages/VPSPage.test.tsx` | Covers inventory quick views, drawer filters, navigation, subscription evidence failure, and draft drawer discard behavior. |
| `web/src/pages/SettingsPage.tsx` | Substantial settings route with tabs, notification channel modal, global save, and security-sensitive Telegram/Feishu fields. |
| `web/src/pages/SettingsPage.test.tsx` | Covers persisted settings load, Telegram/retention copy, masked token summaries, runtime delivery toggles, and omitting unchanged Telegram token from payload. |
| `web/src/pages/LoginPage.tsx` | Small login route using the historical page-specific `LoginPage.css`; low IA surface. |
| `.trellis/spec/web/index.md` | Web spec index. |
| `.trellis/spec/web/directory-structure.md` | Defines route pages, colocated tests, API client, shared components, and styling layout expectations. |
| `.trellis/spec/web/component-conventions.md` | Defines page/component conventions: named exports, API client usage, `PageState`, `Drawer`, `DataTable`, and known debt. |
| `.trellis/spec/web/styling-guidelines.md` | Defines token/BEM styling rules and notes Settings inline-style debt. |
| `.trellis/spec/web/state-and-data.md` | Defines frontend data contracts including onboarding command generation, dashboard command surface, asset queue, and events backfilled filters. |
| `.trellis/spec/web/quality-guidelines.md` | Defines web verification and test expectations. |
| `docs/design/v2-houfeng/design-language.md` | Current visual authority: dark-first, high-density engineering tool, page hierarchy, atoms, and state language. |
| `docs/design/v2-houfeng/component-spec.md` | Current page/component visual contracts; includes Dashboard, AssetDecisions, VPS, details, Events, Settings, Targets, NodeOnboarding, Login, but no explicit NodeCompare page template found. |

### Already Recently Optimized / Lower-Priority Context

| Page / Area | Status |
|---|---|
| `NodeDetailPage` | Recently completed per task context; not part of this audit scope except via reused `NodeWatchtowerMetrics`. |
| `VPSDetailPage` | Recently completed per task context. |
| `SubscriptionsPage` | Recently completed per task context. |
| `ProvidersPage` | Recently completed per task context. |
| `TargetDetailPage` | Recently completed per task context. |
| `TargetsPage` + `NodesPage` list controls | Recently completed per task context. |
| `DashboardPage` | Appears already aligned with v2 command-surface spec and has broad regression tests. |
| `EventsPage` | Appears already aligned with v2 diagnostic timeline spec and has broad regression tests. |
| `AssetDecisionsPage` | Appears already aligned with unified work queue spec and has focused tests. |
| `VPSPage` | Appears already high-density IA optimized with quick views, filter drawer, and evidence-aware missing-data behavior. |
| `NodeOnboardingPage` | Security-sensitive; not recommended for casual IA scope. |
| `LoginPage` | Small/low-value IA surface. |

### Code Patterns

#### NodeComparePage: contained A/B comparison surface

- Query contract is repeated `id` params from `/nodes/compare?id=...&id=...`; empty state triggers when fewer than two IDs are present.
- Data loading is local and parallel per node: `Promise.all([getNode(nodeId), getNodeRuntimeFacts(nodeId)])`.
- Main IA is currently simple: header, two compare identity cards, then `DetailSection title="主机指标对比"` with two metric columns.
- The page reuses `NodeWatchtowerMetrics`, so metric card behavior and chart rendering should remain centralized there.
- Tests assert the request order/shape for `/api/nodes/:id` and `/api/nodes/:id/runtime-facts?window=24h`, plus link and PageState copy.

Observed behavior to preserve:

```tsx
// NodeComparePage.test.tsx asserts this URL/request shape for A-side data.
expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_a', {
  headers: { Accept: 'application/json' },
  cache: 'no-store',
  credentials: 'include',
})
expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_a/runtime-facts?window=24h', {
  headers: { Accept: 'application/json' },
  cache: 'no-store',
  credentials: 'include',
})
```

Relevant classes already exist in `web/src/styles/pages.css`: `.compare-identity`, `.compare-identity__card`, `.compare-identity__state`, `.compare-identity__header`, `.compare-identity__side`, `.compare-identity__title`, `.compare-identity__detail`, `.compare-identity__meta`, `.compare-metrics`, `.compare-metrics__col`.

#### EventsPage: applied URL filters + draft drawer filters

- `parseEventSearchParams`, `normalizeFilters`, and URL serialization make search params the source of truth for applied filters.
- `include_backfilled=1` maps to `include_backfilled: true` in the API query.
- `time_range=24h|7d|30d` remains in URL but is converted to dynamic `created_from` / `created_to` query values.
- Drawer state is draft-only until applied; close/Escape discards drafts.
- Event stream groups events by relative buckets (`今天`, `昨天`, `本周`, `更早`) and supports loading earlier events by increasing limit.

#### DashboardPage: command surface, not KPI warehouse

- `DashboardPage` loads `getDashboard`, supports manual refresh and `useAutoRefresh`, derives fleet state, and renders `DashboardCommandSurface` plus `DashboardWorkbench`.
- Tests explicitly reject old dashboard language such as `系统全局指标`, `Dashboard 摘要指标`, `系统快捷入口`, `Group 摘要`, and `最近事件摘要`.
- Current subcomponents already express the v2 page concept: asset lane, observation lane, next actions, attention queue, first-run workbench, maintenance/running overview.

#### AssetDecisionsPage: unified work queue

- The page loads renewal-window subscription evidence and three VPS decision slices (`unreviewed`, `migrate`, `cancel`) plus all subscriptions for evidence joins.
- Queue tabs are a single work-queue lens: `全部`, `待评估`, `30天续费`, `迁移`, `取消`, `未关联`, `缺订阅`.
- Drawer editing uses `AssetDecisionWorkPanel`; save updates local queue state and displays queue notice.
- Row navigation to `/vps/:id` is isolated from inner `详情`/`处理` controls with propagation guards.
- Subscription evidence failure must not falsely classify VPS rows as missing subscription.

#### NodeOnboardingPage: security-sensitive one-command install

Critical constraints observed in code/tests/specs:

- Browser must not synthesize production install commands from `window.location.origin`.
- The one-command install surface must use backend-issued `issueNodeInstallCommand(nodeId)` response.
- Full enrollment tokens must not appear in incidental notices/logs/conflict copy.
- Generated command hide/reveal/copy behavior is intentionally tested.
- Binding conflict confirm/reject/reset actions are explicitly tested and warn that a pending fingerprint attempt may have consumed the token.
- Manual fallback snippets use placeholders, not real generated secrets.

Important constants/patterns:

```tsx
const MANUAL_TOKEN_PLACEHOLDER = '<30-minute enrollment token>'
const MANUAL_SERVER_PLACEHOLDER = '<center public base URL>'
```

#### VPSPage: evidence-aware high-density inventory

- Uses `VPSQuickView` values including `renewal`, `unreviewed`, `unlinked`, `missing_subscription`, `missing_facts`, and `archived`.
- URL-state filters and advanced drawer follow the same applied/draft pattern used elsewhere.
- Missing subscription state is evidence-aware: only mark missing subscription when subscription evidence is loaded; if evidence fails, show unknown/unavailable rather than false deficiency.

#### SettingsPage: substantial but riskier

- Current structure uses page-level tabs: `通用与外观`, `通知与告警`, `高级与策略`.
- Uses a notification channel modal for Telegram/Feishu configuration and global save behavior.
- Specs describe a more linear settings template with `DetailSection` blocks: `主题`, `Telegram`, `频率档位`, `全局默认`, `覆盖规则`, `保留策略`.
- Known style debt: inline styles remain in SettingsPage for spacing/display/typography wrappers; spec allows inline styles only for calculations/dimensions, not business spacing/colors.
- Security-sensitive token behavior must be preserved: masked token summary and omit unchanged `bot_token` from save payload.

### Candidate Ranking for Next Batch

Scoring scale: High / Medium / Low. Implementation risk and test risk are inverse desirability: lower is safer.

| Rank | Candidate | User Value | IA Gap | Implementation Risk | Test Risk | Rationale |
|---:|---|---|---|---|---|---|
| 1 | `NodeComparePage` | Medium | High | Low-Medium | Low-Medium | It is a routed, user-visible comparison surface with a compact implementation and tests, but no explicit v2 page-template section was found. It can be improved without touching backend contracts or shared metric internals. |
| 2 | `SettingsPage` limited cleanup | Medium | Medium | Medium | Medium-High | Substantial operator page with known inline-style debt and some divergence from linear settings spec, but form state and notification secret handling increase risk. Keep scope narrow. |
| 3 | `VPSPage` minor evidence/IA polish only | Medium | Low-Medium | Medium | Medium | Important page, but already has high-density IA, quick views, URL filters, drawer, and evidence-aware behavior. Only worth a small follow-up if user wants inventory polish. |
| 4 | `EventsPage` small spec-aligned polish only | Medium | Low | Medium | High | Already strongly aligned and very test-dense. Useful to keep stable unless a specific usability issue is identified. |
| 5 | `DashboardPage` small spec-aligned polish only | High | Low | Medium | High | High-value page but already recently/strongly aligned with v2 command-surface behavior and broad regression tests. |
| 6 | `AssetDecisionsPage` small polish only | High | Low | Medium | Medium-High | Already matches unified work-queue model. Avoid unless addressing a concrete issue. |
| 7 | `NodeOnboardingPage` safety-only | High | Low-Medium | High | High | Security-sensitive one-command install/token/binding conflict workflow; only touch if explicitly scoped for safety-preserving changes. |
| 8 | `LoginPage` | Low | Low | Low | Low | Small route with low IA leverage; not a meaningful next batch. |

### Top Candidates: Concrete Low/Medium-Risk Changes

#### Candidate 1: NodeComparePage

Likely touched files:

- `web/src/pages/NodeComparePage.tsx`
- `web/src/pages/NodeComparePage.test.tsx`
- `web/src/styles/pages.css`
- Optional only if needed: `docs/design/v2-houfeng/component-spec.md` or task/spec notes through the proper spec-update workflow, because no explicit NodeCompare template was found.

Concrete low/medium-risk changes:

1. Add a stronger compare command/header panel above the A/B cards:
   - State what is being compared and the data window (`24h runtime facts`) without changing API queries.
   - Surface both selected node names/statuses and quick links back to node list/details.
2. Add a compact A/B summary strip before detailed metrics:
   - Compare health, lifecycle, binding/monitoring status, region/city/provider, and sample availability.
   - Use existing `StatusGlyph`, `Badge`, `MonoDigits`/`Timestamp` where appropriate.
3. Improve empty/error composition without changing copy contracts that tests already assert:
   - Preserve `需要选择 2 个节点`, `返回节点列表`, `B 节点不可用`, and existing technical summaries.
   - Add secondary guidance only if tests are updated accordingly.
4. Keep metric rendering delegated to `NodeWatchtowerMetrics`; do not split or rewrite chart internals in this scope.

Must-preserve behavior:

- `/nodes/compare?id=nd_a&id=nd_b` query contract.
- Direct links to `/nodes` and `/nodes/:nodeId`.
- `getNode` + `getNodeRuntimeFacts` request URLs and `window=24h` behavior.
- Current loading/empty/error `PageState` semantics.
- Existing A/B side labels (`对比对象 A`, `对比对象 B`) unless tests and copy are intentionally updated.

#### Candidate 2: SettingsPage limited cleanup

Likely touched files:

- `web/src/pages/SettingsPage.tsx`
- `web/src/pages/SettingsPage.test.tsx`
- `web/src/styles/pages.css`
- Possibly existing settings subcomponents under `web/src/pages/settings/` if the cleanup moves markup out of the main page without changing behavior.

Concrete low/medium-risk changes:

1. Replace business spacing/typography inline styles in SettingsPage with BEM classes in `pages.css`.
2. Add/clarify settings overview copy that explains what each settings group controls, while keeping the existing tab model if avoiding higher-risk IA restructuring.
3. If restructuring is desired, do only one tab at a time into clearer `DetailSection` blocks rather than replacing all settings navigation at once.
4. Preserve channel modal and global save behavior; do not change token field semantics in a cosmetic IA pass.

Must-preserve behavior:

- Loading/error `PageState` copy.
- Global save payload shape.
- Unchanged Telegram `bot_token` must be omitted from payload.
- Existing token masking; raw Telegram bot token / chat ID / Feishu webhook URL must not be exposed outside intended input surfaces.
- Runtime delivery toggle semantics.
- Validation for numeric and JSON-array fields.

#### Candidate 3: VPSPage minor polish only

Likely touched files:

- `web/src/pages/VPSPage.tsx`
- `web/src/pages/VPSPage.test.tsx`
- `web/src/styles/pages.css`

Concrete low/medium-risk changes:

1. Tighten top inventory command surface copy or grouping if the main agent wants a small follow-up batch.
2. Add/adjust evidence status hints for subscription/provider joins without changing classification logic.
3. Keep URL filter/drawer behavior unchanged; avoid changing table columns unless explicitly scoped.

Must-preserve behavior:

- URL-state filter parse/serialize behavior.
- Quick view values and semantics.
- Advanced drawer draft close/Escape/overlay discard behavior.
- Evidence-aware missing subscription classification; subscription evidence failure must not become false `缺订阅`.
- Row navigation to `/vps/:id` and action isolation.

### Recommended Scope Options for Main Agent

#### Option A — Safe next batch: NodeComparePage IA only

- Scope: `NodeComparePage.tsx`, its test, compare-related CSS.
- Why: Highest IA gap with contained implementation/test surface.
- Expected outcome: a clearer A/B comparison command surface and summary without touching metric internals or backend contracts.
- Risk: Low-Medium.

#### Option B — NodeComparePage + SettingsPage style/structure cleanup

- Scope: NodeCompare as above, plus limited Settings cleanup focused on replacing inline styles and clarifying existing sections.
- Why: Combines one clear page IA gap with one known spec/style-debt page.
- Risk: Medium because Settings has security-sensitive notification state and broader tests.
- Guardrail: No changes to Telegram/Feishu secret persistence semantics or install/onboarding flows.

#### Option C — Stabilization batch for already-optimized surfaces

- Scope: Tiny polish only across Events/Dashboard/AssetDecisions/VPSPage, driven by specific acceptance criteria.
- Why: These pages are already aligned but could receive small consistency improvements.
- Risk: Medium-High test churn because Dashboard and Events have extensive regression tests.
- Guardrail: Avoid broad IA rewrites; update tests only for intentional copy/structure changes.

Recommended default ask to user/main agent:

- Prefer Option A unless the user specifically wants Settings included.
- Ask whether to include Settings cleanup as a second page in the batch.
- Keep NodeOnboarding out of this batch unless explicitly requested for a security-preserving pass.

### External References

No web search was requested or used.

### Related Specs

| Spec / Doc | Relevant Notes |
|---|---|
| `.trellis/spec/web/directory-structure.md` | Route pages live in `web/src/pages`; tests are colocated; shared UI belongs under components; route registration is centralized in `web/src/app/router.tsx`. |
| `.trellis/spec/web/component-conventions.md` | Use API client helpers, `PageState`, `Drawer`, `DataTable` conventions; avoid direct `fetch()` and avoid new page CSS files. |
| `.trellis/spec/web/styling-guidelines.md` | Use CSS variables/tokens and BEM in existing global CSS; inline styles only for dimensions/calculations; Settings inline style debt is known. |
| `.trellis/spec/web/state-and-data.md` | Defines Dashboard command surface, Asset Ledger queue, Events `include_backfilled`, and Node onboarding one-command install constraints. |
| `.trellis/spec/web/quality-guidelines.md` | Web changes should pass lint/test/build; page tests mock fetch and assert request shape. |
| `docs/design/v2-houfeng/design-language.md` | Visual authority: dark-first, high-density engineering tool, state language, atom usage. |
| `docs/design/v2-houfeng/component-spec.md` | Provides templates for Dashboard, AssetDecisions, VPS, details, Events, Settings, Targets, NodeOnboarding, Login; no NodeCompare page template found in inspected material. |

## Caveats / Not Found

- `NodeComparePage` did not appear to have an explicit page-template section in `docs/design/v2-houfeng/component-spec.md`; this is a finding from inspected docs, not proof that no historical design existed elsewhere.
- The audit did not inspect every line of every supporting component; it focused on requested page surfaces and relevant subcomponents/tests.
- No web search was performed because the request explicitly said no web search was needed.
- The report recommends scope options because the task explicitly requested candidate ranking, concrete changes, and recommended options, even though the Research Agent role normally avoids unsolicited improvement proposals.
- NodeOnboarding should remain safety-only: do not casually alter one-command install generation, token visibility, or binding conflict behavior.
