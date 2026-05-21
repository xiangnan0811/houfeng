# Research: remaining page IA audit

- **Query**: Research the next best Houfeng frontend page or page group for a limited low-risk information-architecture polish after completed scopes: NodeDetail, VPSDetail, Settings, Targets+Nodes list controls, NodeCompare, VPSPage inventory, NodeOnboarding, TargetDetailPage, ProvidersPage + SubscriptionsPage. Inspect active routes/pages, current page implementations/tests, and archived Trellis IA tasks; produce files found, code patterns/contracts, candidate matrix, recommended next scope, frozen contracts, and caveats.
- **Scope**: internal
- **Date**: 2026-05-21

## Findings

### Files Found

| File Path | Description |
|---|---|
| `.trellis/tasks/05-21-next-page-ia-batch-3/prd.md` | Active task context; states this batch should continue after Providers/Subscriptions, remain frontend-only, and preserve URL state, row navigation, Drawer/modal draft/apply/discard, payload, and destructive-confirmation contracts. |
| `web/src/app/router.tsx` | Active SPA route inventory: login, dashboard, VPS/VPS detail, Providers, Subscriptions, AssetDecisions, Nodes/NodeCompare/NodeDetail/NodeOnboarding, Targets/TargetDetail, Events, Settings. |
| `web/src/app/metadata.ts` | Primary navigation IA groups: 总览, 资产, 观测, 系统. |
| `web/src/app/layout/AppShell.tsx` | Authenticated shell; reads dashboard summary for sidebar counts, SyncStatus, and global critical alert. |
| `web/src/app/layout/Sidebar.tsx` | Renders grouped nav and neutral anomaly count badges. |
| `web/src/app/layout/Breadcrumb.tsx` | Detail-route breadcrumb behavior; root and first-level pages intentionally stay uncrumbed. |
| `web/src/pages/DashboardPage.tsx` / `web/src/pages/dashboard/*` | Dashboard command surface + workbench; loads `getDashboard`, derives fleet state, and renders asset/observability/next-action lanes. |
| `web/src/pages/DashboardPage.test.tsx` | Regression coverage for severe, abnormal, maintenance, normal, first-run, and negative anti-pattern assertions. |
| `web/src/pages/EventsPage.tsx` / `web/src/pages/events/*` | Diagnostic/audit timeline with URL-state filters, support surface, filter overview, Drawer draft/apply/reset, event stream, and load-more behavior. |
| `web/src/pages/EventsPage.test.tsx` | Dense coverage for URL filter parsing/canonicalization, backfilled events, time ranges, drawer discard/apply, chip removal, empty/error, and load-more. |
| `web/src/pages/AssetDecisionsPage.tsx` | Unified Asset Ledger decision queue with renewal-window evidence, queue tabs, focus cards, row navigation, and Drawer work panel. |
| `web/src/pages/AssetDecisionsPage.test.tsx` | Coverage for request shapes, renewal-window reloads, decision updates/queue movement, empty state, row/action isolation, drawer discard, and subscription-evidence failure truthfulness. |
| `web/src/pages/LoginPage.tsx` / `web/src/pages/LoginPage.test.tsx` | Small auth route with username/password form, generic error, `next` redirect, and single-user phrasing guard. |
| `web/src/pages/NodeComparePage.tsx` | Recently polished compare page; already has command panel, A/B identity, summary strip, and 24h runtime facts columns. Excluded by active task context. |
| `.trellis/spec/web/component-conventions.md` | Page/component contracts: pages assemble data/state, `PageState`, `Drawer` reset, DataTable/self-drawn row interaction guards, selector-based associations. |
| `.trellis/spec/web/styling-guidelines.md` | Styling constraints: tokens, BEM, global CSS, no new page-local CSS except LoginPage, PageState reuse. |
| `.trellis/spec/web/state-and-data.md` | Data contracts for Dashboard, Asset Ledger, Events, and onboarding/token boundaries. |
| `docs/design/v2-houfeng/design-language.md` | Visual authority: dark-first, high-density engineering tool, page hierarchy, unified loading/error/empty, no backend/API/data-shape changes. |
| `docs/design/v2-houfeng/component-spec.md` | Page templates for Dashboard, AssetDecisions, VPS/VPSDetail, Nodes/NodeDetail, Events, Settings, Targets/TargetDetail, NodeOnboarding, Login. |
| `.trellis/tasks/archive/2026-05/05-13-ux3-dashboard-command-surface-polish/prd.md` | Prior Dashboard command-surface task; freezes against KPI wall/API facts/Group/recent-events regressions. |
| `.trellis/tasks/archive/2026-05/05-13-ux6c-events-timeline-evidence/prd.md` | Prior Events diagnostic/evidence convergence task; keeps URL-state and Drawer filter contracts. |
| `.trellis/tasks/archive/2026-05/05-20-next-page-ia-batch/prd.md` | Prior NodeOnboarding safety-frozen IA task; now completed/recent per current task context. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch/prd.md` | Prior TargetDetail IA task; now completed/recent per current task context. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/prd.md` | Prior ProvidersPage + SubscriptionsPage IA task; immediate predecessor of current batch. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/research/remaining-page-ia-audit.md` | Previous candidate matrix recommending Providers/Subscriptions; now stale because that group has been completed. |
| `.trellis/tasks/archive/2026-05/05-21-next-page-ia-batch-2/research/design-spec-candidate-audit.md` | Previous design/spec audit; useful for why Dashboard/Events/AssetDecisions were deferred as already aligned or higher-risk. |

### Code Patterns

#### Active route inventory and excluded recent work

- `web/src/app/router.tsx:62-99` confirms all routed surfaces after auth: `/`, `/vps`, `/vps/:vpsId`, `/providers`, `/subscriptions`, `/asset-decisions`, `/nodes`, `/nodes/compare`, `/nodes/:nodeId`, `/nodes/:nodeId/onboarding`, `/targets`, `/targets/:targetId`, `/events`, `/settings`.
- Active task context says recently completed IA polish includes NodeDetail, VPSDetail, Settings, Targets+Nodes lists, NodeCompare, VPSPage inventory, NodeOnboarding, TargetDetailPage, ProvidersPage + SubscriptionsPage (`.trellis/tasks/05-21-next-page-ia-batch-3/prd.md:9-12`). This removes most previously ranked candidates from the next-batch pool.
- The remaining meaningful routed candidates are therefore `DashboardPage`, `EventsPage`, `AssetDecisionsPage`, and `LoginPage`; `AppShell`/nav is a global shell surface rather than a single page batch.

#### DashboardPage: already command-surface aligned

- `web/src/pages/DashboardPage.tsx:31-44` loads only `getDashboard()` for page data and handles errors through `PageState`; `:96-120` renders only `DashboardCommandSurface` and `DashboardWorkbench`.
- `docs/design/v2-houfeng/component-spec.md:202-220` explicitly defines Dashboard as asset-decision-first command surface + one workbench, not a KPI warehouse.
- `.trellis/spec/web/state-and-data.md:102-114` freezes Dashboard trust boundaries: `snapshot_generated_at` is dashboard response generation time only, `recent_events`/`group_summaries` should not become first-screen warehouses, and notification secrets must not be exposed.
- `web/src/pages/DashboardPage.test.tsx:310-318` asserts old anti-patterns do not render: `已加载 /api/dashboard`, `首页数据可信度`, `系统全局指标`, `Dashboard 摘要指标`, `系统快捷入口`, `Group 摘要`, `最近事件摘要`.
- The archived UX-3 Dashboard PRD has the same boundary: preserve the three lanes, do not restore KPI/API facts/Group/recent-event warehouses, and do not change `/api/dashboard` (`.trellis/tasks/archive/2026-05/05-13-ux3-dashboard-command-surface-polish/prd.md:18-49`).

Current contracts to preserve if touched:

- `getDashboard()` remains the only Dashboard data source for page facts.
- `snapshot_generated_at` remains generation metadata, not center/agent health or sync freshness.
- Dashboard deep links remain `/asset-decisions`, `/events?severity=严重`, `/events?time_range=24h`, `/events?maintenance_only=1`, `/nodes?abnormal=1`, `/targets?abnormal=1`, `/vps`.
- No notification token/chat/webhook facts in Dashboard/AppShell.
- No full KPI wall, `Group 摘要`, or `最近事件摘要` regression.

#### EventsPage: already diagnostic timeline aligned, high URL/filter regression surface

- `web/src/pages/EventsPage.tsx:60-132` parses, normalizes, and serializes URL filters; `include_backfilled=1` in URL becomes `include_backfilled: true` in API query; invalid URL params are canonicalized away.
- `web/src/pages/EventsPage.tsx:195-355` keeps `appliedFilters` and Drawer `draftState` separate; Drawer close/Escape discards draft while apply/reset commits URL and fetch state.
- `web/src/pages/EventsPage.tsx:373-420` page composition already matches the v2 diagnostic timeline: hero, `EventsSupportSurface`, `EventsFilterOverview`, `EventsFilterDrawer`, and `EventsStreamSection`.
- `docs/design/v2-houfeng/component-spec.md:282-290` defines this exact Events shape and URL-state contract.
- `.trellis/spec/web/state-and-data.md:469-514` freezes `include_backfilled`, URL truth, API boolean serialization, and Drawer draft/apply/discard behavior.
- `web/src/pages/EventsPage.test.tsx:139-168`, `:379-436`, `:438-480`, `:482-513`, `:544-560`, and `:625-644` cover initial URL filters, advanced context filters, backfilled toggle, drawer close/Escape discard, invalid URL canonicalization, and relative time ranges.
- The archived UX-6C Events PRD had already made Events the diagnostic/audit timeline while preserving filter Drawer and URL contracts (`.trellis/tasks/archive/2026-05/05-13-ux6c-events-timeline-evidence/prd.md:5-18`, `:43-50`).

Current contracts to preserve if touched:

- URL query support: `object_type`, `severity`, `event_type`, `limit`, `created_from`, `created_to`, `label`, `notification_only=1`, `recovery_only=1`, `maintenance_only=1`, `include_backfilled=1`, `time_range=24h|7d|30d|custom`.
- API query serialization: backfilled uses `include_backfilled=true`; relative `time_range` becomes dynamic `created_from`/`created_to` and does not send `time_range` to API.
- Drawer draft changes must not update URL or fetch until apply/reset.
- Event stream remains the default evidence list; support surface does not replace EventList facts.

#### AssetDecisionsPage: best remaining major route if a next batch is mandatory, but already aligned

- `web/src/pages/AssetDecisionsPage.tsx:327-354` loads renewal-window subscription evidence with `/api/subscriptions?renew_within_days=<window>&sort=renew_at&order=asc`.
- `web/src/pages/AssetDecisionsPage.tsx:356-392` loads all subscriptions plus three VPS decision slices: `renewal_decision=unreviewed`, `migrate`, and `cancel`.
- `web/src/pages/AssetDecisionsPage.tsx:112-140` builds one deduplicated priority queue from VPS rows + subscription evidence; `:157-166` applies queue tabs (`all`, `unreviewed`, `renewal`, `migrate`, `cancel`, `unlinked`, `missing_subscription`).
- `web/src/pages/AssetDecisionsPage.tsx:204-232` renders subscription signal; missing subscription is explicitly evidence-dependent, and the page avoids classifying rows if all subscription evidence fails.
- `web/src/pages/AssetDecisionsPage.tsx:235-311` renders self-drawn clickable queue rows and explicitly stops propagation on inner `Link`/`Button`; this matches `.trellis/spec/web/component-conventions.md:45-47` for self-drawn queues.
- `web/src/pages/AssetDecisionsPage.tsx:477-518` submits `updateVPSAsset(selectedVPS.vps_id, { renewal_decision, renewal_reason? })`, updates local queues, applies optional `renewal_subscription_linkage`, closes Drawer, and leaves notice on the queue surface.
- `web/src/pages/AssetDecisionsPage.tsx:536-681` already follows component spec: hero, unified `资产决策工作队列`, summary, renewal-window selector, tabs, focus cards, queue/empty/error, secondary `RENEWAL EVIDENCE`, and Drawer work panel.
- `docs/design/v2-houfeng/component-spec.md:221-227` says AssetDecisions must be a unified work queue, keep only decision-order summary numbers, must not show linked-node health beyond count, uses Drawer for `AssetDecisionWorkPanel`, and keeps renewal evidence secondary.
- `.trellis/spec/web/state-and-data.md:147-202` freezes Asset Ledger list/decision data flow: API helpers, queue slices, evidence-failure behavior, `active_node_link_count` semantics, and tests.
- `web/src/pages/AssetDecisionsPage.test.tsx:90-157` asserts request shapes and renewal-window reloads; `:159-206` asserts PATCH payload and queue movement; `:208-232` asserts empty actions; `:234-268` asserts row navigation/action isolation; `:270-313` asserts Drawer cancel/Escape/overlay discard; `:315-334` asserts all-subscription-evidence failure shows queue error instead of false missing-subscription rows.

Current contracts to preserve if touched:

- API helpers and request shapes: `listSubscriptions({ renew_within_days, sort: 'renew_at', order: 'asc' })`, `listSubscriptions({ sort: 'renew_at', order: 'asc' })`, `listVPSAssets({ renewal_decision })`, `updateVPSAsset(vpsId, input)`.
- Renewal windows remain `30 | 60 | 90`; invalid select values fall back through `parseRenewalWindow`.
- Queue tabs and meanings remain: `all`, `unreviewed`, `renewal`, `migrate`, `cancel`, `unlinked`, `missing_subscription`.
- PATCH payload remains only `renewal_decision` plus optional trimmed `renewal_reason`.
- `monthly_price` remains backend-computed subscription evidence; do not compute or send it.
- `VPSAssetRecord.active_node_link_count` remains count-only; do not display or imply linked-node health/heartbeat/incident facts.
- Subscription evidence failure must show local error and not misclassify all VPS rows as `缺订阅`.
- Drawer close/cancel/Escape/overlay discard draft/error and do not submit.
- Row background can navigate to `/vps/:vpsId`; inner `详情` and `处理` actions must remain isolated.

#### LoginPage: low IA leverage

- `web/src/pages/LoginPage.tsx:31-64` is a compact full-screen login form with seal, brand, motto, username/password inputs, generic error, submit button, and footer.
- `docs/design/v2-houfeng/component-spec.md:339-345` defines LoginPage as full-screen centered seal/card/form/error.
- `web/src/pages/LoginPage.test.tsx:9-62` covers no single-user phrasing, credential submission, and generic auth error.
- `web/src/pages/LoginPage.tsx:63` renders `v1.0`; this may be a truthfulness/copy cleanup seam, but it is too small to justify a full IA batch by itself.

Current contracts to preserve if touched:

- `useAuth().login(username, password)` flow.
- `next` redirect behavior after successful login.
- Generic auth failure copy; no exposure of auth internals.
- No single-user / full-permission / personal-system phrasing.
- `LoginPage.css` remains the only page-local CSS exception.

#### AppShell / navigation: aligned global shell, not a page batch

- `web/src/app/metadata.ts:17-43` groups nav exactly as v2 component spec expects: 总览, 资产, 观测, 系统.
- `docs/design/v2-houfeng/component-spec.md:160-177` defines Sidebar groups, neutral nav count badges, and no alarm-colored nav semantics; `web/src/app/layout/Sidebar.tsx:63-79` implements neutral count badges with an explicit comment.
- `web/src/app/layout/AppShell.tsx:52-76` reads dashboard summary once; `:149-188` derives SyncStatus labels from dashboard summary while avoiding fake center/agent sync semantics.
- `.trellis/spec/web/state-and-data.md:114` allows AppShell to reuse dashboard summary only as dashboard-derived summary, and forbids fake `center ok` / sync freshness language.
- `web/src/app/layout/Breadcrumb.tsx:24-38` intentionally omits breadcrumbs for root and first-level routes to avoid duplicate nav anchors; detail routes show section/id/tail without extra data fetches.

Current contracts to preserve if touched:

- AppShell keeps `skip-link` to `#main-content` and `main id="main-content" tabIndex={-1}` (`.trellis/spec/web/component-conventions.md:52-55`).
- Sidebar anomaly count badges remain neutral informational counts, not alert-state indicators.
- Global critical alert links remain existing filters (`/events?severity=严重`, `/nodes?abnormal=1`, `/targets?abnormal=1`).
- Dashboard summary must not imply real sync freshness beyond dashboard-derived status.

### Candidate Matrix

| Candidate | Current IA opportunity | Risk | Likely touched files if selected | Frozen contracts |
|---|---|---:|---|---|
| `AssetDecisionsPage` targeted micro-polish | Best remaining major routed work surface if the batch must select a page. It already matches the unified queue spec, so any value is narrow: clarify queue/evidence/action framing without altering queue semantics. | Medium | `web/src/pages/AssetDecisionsPage.tsx`, `web/src/pages/AssetDecisionsPage.test.tsx`, `web/src/styles/pages.css`; avoid shared component changes unless only copy/class reuse is necessary. | Preserve `listSubscriptions`, `listVPSAssets`, `updateVPSAsset`, renewal-window query, queue tabs, PATCH payload, subscription-evidence failure truthfulness, row/action isolation, Drawer discard, no linked-node health. |
| `EventsPage` issue-driven copy polish only | Already aligned diagnostic timeline with strong support surface and filter overview. Any broad IA change risks URL/filter/Draft/apply/load-more regressions. | High | Not recommended unless a concrete defect is identified. | Preserve URL canonicalization, filter params, dynamic time ranges, `include_backfilled`, Drawer draft/apply/discard, chips, load-more, empty/error semantics. |
| `DashboardPage` issue-driven copy polish only | High-value page, but it already has command-surface/workbench architecture and tests against old KPI/warehouse regressions. | Medium-High | Not recommended for broad next batch. | Preserve `getDashboard` truth boundaries, dashboard generation time copy, deep links, first-run/normal/maintenance/abnormal states, no KPI wall/Group/recent-events regression. |
| `LoginPage` tiny truthfulness/copy cleanup | Very small IA surface; possible footer/copy cleanup is not enough for a meaningful page IA batch. | Low | `web/src/pages/LoginPage.tsx`, `web/src/pages/LoginPage.test.tsx`, existing `LoginPage.css` only if needed. | Preserve login flow, `next` redirect, generic error, no single-user phrasing, no auth internals exposure. |
| `AppShell` / nav metadata | Current shell/nav already aligns with v2 grouping and dashboard-summary trust boundaries. Global changes would affect every page and are not a low-risk page batch. | Medium | Not recommended for this page IA task. | Preserve nav groups/labels, neutral count badge semantics, dashboard-derived SyncStatus wording, global critical links, skip link/main focus target. |
| `NodeComparePage` | Already completed/recent per active task context; current implementation has command panel + summary strip. | Low | Out of scope. | Preserve repeated `id` query contract, `getNode` + `getNodeRuntimeFacts?window=24h`, A/B labels, `NodeWatchtowerMetrics` reuse. |
| `VPSPage`, `VPSDetailPage`, `NodeDetailPage`, `SettingsPage`, `NodeOnboardingPage`, `TargetDetailPage`, `ProvidersPage`, `SubscriptionsPage`, `TargetsPage`, `NodesPage` | Recently completed/recent per current PRD and archived PRDs. | Varies | Out of scope for batch 3 selection except as precedent. | Preserve each completed task's frozen API/security/URL/drawer/payload/destructive contracts. |

### Recommended Next Scope

Recommend **`AssetDecisionsPage` targeted IA micro-polish** only if this task must select another page batch now.

Rationale:

1. It is the only remaining major routed work surface with meaningful operator value after the current PRD's completed list removes NodeDetail, VPSDetail, Settings, Targets/Nodes lists, NodeCompare, VPSPage inventory, NodeOnboarding, TargetDetailPage, and Providers/Subscriptions.
2. Dashboard and Events are already explicitly aligned with v2 page specs and have dense regression tests. They should stay issue-driven rather than receive speculative broad polish.
3. Login is too small to justify an IA batch, and AppShell/nav is a global shell surface rather than a page/page-group.
4. AssetDecisions is already aligned, so the scope should be framed as a **small contract-preserving micro-polish**, not a rewrite. The main value is maintaining consistency after Providers/Subscriptions and giving the Asset Ledger decision queue one final clarity pass without changing backend/data/security behavior.

Recommended limited scope boundaries:

- Allowed touch points by default:
  - `web/src/pages/AssetDecisionsPage.tsx`
  - `web/src/pages/AssetDecisionsPage.test.tsx`
  - `web/src/styles/pages.css`
- Optional only if necessary and behavior-neutral:
  - Existing `AssetDecisionWorkPanel` / `AssetDecisionRenewalTable` copy or class hooks, but avoid changing their API or shared semantics.
- Keep the unified queue as the primary scan path and `RENEWAL EVIDENCE` as secondary evidence.
- Do not add new data requests, new joins, new fields, new routes, new dependencies, charts, CSS systems, or page-local CSS.
- Tests should assert any new user-visible IA copy/structure while keeping current frozen-contract assertions for request shapes, PATCH payload, queue movement, evidence failure, row isolation, and Drawer discard.

Frozen contracts for the recommended scope:

- Preserve `listSubscriptions`, `listVPSAssets`, `updateVPSAsset` API helper usage.
- Preserve initial request shapes and order expectations currently covered by `AssetDecisionsPage.test.tsx`.
- Preserve renewal-window query mapping: `renew_within_days=<30|60|90>&sort=renew_at&order=asc`.
- Preserve queue tabs and derived meanings: `全部`, `待评估`, `<window>天续费`, `迁移`, `取消`, `未关联`, `缺订阅`.
- Preserve `updateVPSAsset` PATCH body shape: `renewal_decision` plus optional non-empty `renewal_reason` only.
- Preserve subscription evidence failure boundary: if all subscription evidence fails, show the queue error instead of rendering all VPS as missing subscriptions.
- Preserve backend-computed subscription values (`monthly_price`) as display-only evidence.
- Preserve `active_node_link_count` as count-only; do not invent linked-node health, heartbeat, incident, or freshness facts on list records.
- Preserve row navigation/action isolation and Drawer cancel/Escape/overlay discard behavior.
- Preserve success notice on the queue surface after Drawer close.

### External References

No external search was needed; this was an internal code/spec/task audit.

### Related Specs

- `.trellis/spec/web/component-conventions.md` — page assembly, `PageState`, `Drawer` reset/focus, DataTable/self-drawn row interaction, route/layout boundaries.
- `.trellis/spec/web/styling-guidelines.md` — token/BEM/global CSS constraints, PageState reuse, no new page-local CSS except LoginPage.
- `.trellis/spec/web/state-and-data.md` — Dashboard trust boundaries, Asset Ledger queue/evidence contracts, Events URL/backfilled filter contracts, onboarding token boundaries.
- `.trellis/spec/web/quality-guidelines.md` — page-test and web verification expectations.
- `docs/design/v2-houfeng/design-language.md` — dark-first, high-density engineering-tool hierarchy, loading/error/empty, and no backend/API/data-shape changes for visual work.
- `docs/design/v2-houfeng/component-spec.md` — Dashboard, AssetDecisions, Events, AppShell/Sidebar, Login, and adjacent Asset Ledger page templates.

## Caveats / Not Found

- No broad, high-upside remaining page IA gap was found after the completed scopes listed in the active PRD. `AssetDecisionsPage` is recommended as the least-bad/default remaining routed page only because the task asks for another batch.
- `AssetDecisionsPage` is already v2-spec aligned. The recommended scope should remain intentionally small; a broad rewrite would create churn without clear evidence.
- Older archived research that ranked `NodeComparePage`, `SettingsPage`, `VPSPage`, `NodeOnboardingPage`, `TargetDetailPage`, or `ProvidersPage`/`SubscriptionsPage` is stale for this task because those pages/groups are now completed or recent per `.trellis/tasks/05-21-next-page-ia-batch-3/prd.md`.
- AppShell/nav was included in the audit for completeness, but it is a global shell surface and should not be treated as the next low-risk page batch.
- This audit did not perform browser visual inspection and did not run tests; it is a static code/spec/archive review.
- No implementation files were modified by this research.
