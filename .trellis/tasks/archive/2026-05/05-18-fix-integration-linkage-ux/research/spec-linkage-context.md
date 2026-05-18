# Research: integration/linkage UX spec context

- **Query**: Research project/Trellis specs and active design docs relevant to integration/linkage UX for task `.trellis/tasks/05-18-fix-integration-linkage-ux`. Focus on `.trellis/spec`, `docs/design/v1-baseline`, `docs/design/v2-houfeng`, and active docs for Asset Ledger, Nodes, Providers, Subscriptions, operation flows, and frontend design rules. Extract constraints that should govern a unified fix.
- **Scope**: internal
- **Date**: 2026-05-18

## Findings

### Files Found

| File Path | Description |
|---|---|
| `.trellis/spec/web/state-and-data.md` | Frontend API/type/state contracts; contains the densest current contracts for Node onboarding, Dashboard deep links, Asset Ledger list/decision flows, VPS detail, service/domain assets, and Events URL state. |
| `.trellis/spec/web/component-conventions.md` | Component layering, route-agnostic shared components, `PageState`, `Drawer`, DataTable row-click semantics, AppShell/GlobalSearch route contract, and list/create/edit surface placement. |
| `.trellis/spec/web/directory-structure.md` | Frontend route/page/API/type locations; one page + test per route; API calls in `web/src/lib/api.ts`; types in `web/src/lib/types.ts`. |
| `.trellis/spec/web/styling-guidelines.md` | Current visual authority, pure CSS/BEM/tokens, dark-first Chinese engineering-tool constraints, allowed style locations, loading/error/empty style guidance. |
| `.trellis/spec/web/quality-guidelines.md` | Web quality gates, visual/UX evidence requirements, and cross-layer consistency checklist for API shape changes. |
| `.trellis/spec/backend/directory-structure.md` | Backend organization and Asset Ledger examples; documents providers, VPS assets, subscriptions, VPS↔Node links, histories/timeline, import CLI, and one-command install contract. |
| `.trellis/spec/backend/database-guidelines.md` | Persistence and domain invariants for Providers, VPS assets, subscriptions, VPS↔Node links, services, domains, JSON import, Dashboard asset summary, and raw observability model boundaries. |
| `.trellis/spec/backend/quality-guidelines.md` | Cross-layer checklist for endpoint/schema/linkage changes, including required backend/frontend/test touchpoints. |
| `.trellis/spec/guides/cross-layer-thinking-guide.md` | General boundary-thinking checklist for mapping data flow and defining contracts across DB/API/frontend/component layers. |
| `.trellis/spec/guides/code-reuse-thinking-guide.md` | General reuse checklist to avoid duplicated logic and drift between parallel mechanisms. |
| `docs/design/v1-baseline/README.md` | Frozen V1 product/architecture boundary and active-vs-superseded authority statement. |
| `docs/design/v1-baseline/architecture-data-model.md` | Frozen Node/Target/ProbeItem/observation/incident model and separation between Node, Target, ProbeItem, facts, and interpretations. |
| `docs/design/v1-baseline/rules-and-interaction.md` | Frozen page structure, Node/Target/Events surfaces, health/notification semantics, and “prefer state over deletion” rule. |
| `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md` | Frozen operation flows for Node onboarding, Target creation, ProbeItem management, state transitions, empty states, recovery paths, and workbench/list philosophy. |
| `docs/design/v1-baseline/tech-selection.md` | Frozen topology/technology constraints: Go center/agent, PostgreSQL, React/Vite SPA, dark-first Chinese dense UI. |
| `docs/design/v2-houfeng/design-language.md` | Active visual language: dark-first, high-density, token/font/status/three-state/error/empty constraints, no Tailwind/chart-library/visual-regression dependencies. |
| `docs/design/v2-houfeng/component-spec.md` | Active page/component contracts for Dashboard, Asset Decisions, VPS, VPS Detail, Nodes, Node Detail/Onboarding, Targets, Target Detail, Events, Sidebar nav grouping, and route/deep-link behavior. |
| `docs/operations/asset-ledger-real-data-validation-readiness.md` | Active Asset Ledger validation boundaries for mock/local/real data, sample importer workflow, real-data privacy checklist, and route review focus for `/asset-decisions`, `/vps`, `/providers`, `/subscriptions`. |
| `docs/operations/v1-smoke-run.md` | Active fresh-install operation flow, one-command Node onboarding path, Target/ProbeItem smoke path, UI checkpoints, and secret-handling constraints. |
| `docs/operations/v2-visual-evidence.md` | Active UI preview/browser-sanity workflow, route matrix, mock API profiles for asset and observability routes, viewport requirements, and screenshot policy. |
| `docs/operations/asset-ledger-local-sample.json` | Non-sensitive sample data fixture referenced by the Asset Ledger readiness workflow; sample includes provider, VPS, subscription, and manual Node/Target association hints. |

### Code Patterns

No production code was inspected for this research topic. The constraints below are extracted from Trellis specs and active design/operations documents only.

#### 1. Authority order and product boundary

- Current spec authority is explicit: `CLAUDE.md` plus frozen V1 business/structure docs; active visual authority is `docs/design/v2-houfeng/`. The web specs state this at `.trellis/spec/web/index.md:3`, `.trellis/spec/web/styling-guidelines.md:3`, and `.trellis/spec/web/component-conventions.md:3`.
- V1 visual material is superseded, but V1 structure remains authoritative. The V1 README states the visual portion is superseded by v2 while `architecture-data-model`, `rules-and-interaction`, `tech-selection`, and `interactive-prototype-and-operation-flow` remain frozen and authoritative (`docs/design/v1-baseline/README.md:11-26`, `docs/design/v1-baseline/README.md:33-36`).
- Product identity remains monitoring/probe-first for a single operator, not a generic ops platform, Docker orchestration platform, complete CMDB, script execution system, or multi-tenant monitoring platform (`docs/design/v1-baseline/README.md:87-99`; `docs/design/v1-baseline/architecture-data-model.md:15-30`).
- The technical topology stays `1 Go center + 1 PostgreSQL + N systemd agents`; no extra MQ, TSDB, microservices, or Docker/Kubernetes agent deployment is part of the active boundary (`docs/design/v1-baseline/README.md:101-127`; `docs/design/v1-baseline/tech-selection.md:48-75`).

#### 2. Core model boundaries for linkage UX

- `Node = 一台具体服务器`; same-machine reinstall remains the same Node, new hardware requires a new Node (`docs/design/v1-baseline/architecture-data-model.md:107-116`). Node has lifecycle, monitoring, health, heartbeat/sync, binding, and incident summary fields (`docs/design/v1-baseline/architecture-data-model.md:117-166`).
- `Target = 一个明确的可观测入口`; address belongs to Target (`host`/`base_port`), while ProbeItem describes how to observe it (`docs/design/v1-baseline/architecture-data-model.md:167-218`).
- ProbeItem only supports TCP, HTTP/HTTPS, and TLS in V1 and remains attached to a Target (`docs/design/v1-baseline/architecture-data-model.md:219-268`).
- Model boundaries are explicit: Node manages machines; Target manages entrypoints; ProbeItem manages observation method; raw facts and interpretation layers are separate; health is derived; lifecycle is managed; maintenance is a runtime control, not a health state (`docs/design/v1-baseline/architecture-data-model.md:385-405`).
- Request path stores raw observations first; incident/event/notification interpretation is asynchronous and center-owned (`docs/design/v1-baseline/architecture-data-model.md:67-95`; `.trellis/spec/backend/database-guidelines.md:657-669`).

#### 3. Asset Ledger linkage is an extension layer, not a replacement for observability models

- Providers are Asset Ledger master data and must not automatically rewrite or backfill `nodes.provider` (`.trellis/spec/backend/database-guidelines.md:238-247`; `.trellis/spec/backend/directory-structure.md:505-506`).
- VPS assets are asset-ledger records, separate from Fleet Observability Node/Target/Agent semantics. VPS CRUD must not rewrite `nodes.provider` or change Node/Target/Agent semantics (`.trellis/spec/backend/database-guidelines.md:250-260`; `.trellis/spec/backend/directory-structure.md:506-507`).
- Subscriptions are asset-layer VPS billing records and must not create node links or rewrite VPS/Provider/Node/Target/Agent state (`.trellis/spec/backend/database-guidelines.md:262-273`; `.trellis/spec/backend/directory-structure.md:507-508`).
- VPS↔Node links live in `vps_node_links` as association history. Active link means `unlinked_at is null`; unlink writes `unlinked_at`, not physical deletion; link/unlink must not change `nodes.provider`, Node lifecycle/monitoring/health, Target, Agent, or subscription (`.trellis/spec/backend/database-guidelines.md:275-286`; `.trellis/spec/backend/directory-structure.md:508-509`).
- Node-side VPS summary is intentionally queried through `/api/nodes/{node_id}/vps`; asset fields are not mixed into the base `nodes.Record` (`.trellis/spec/backend/database-guidelines.md:284-286`).
- Asset histories/timeline, price/IP/spec history, renewal decisions, experience logs, services, and domains are all asset-layer records and must not mutate Node/Target/Agent state (`.trellis/spec/backend/database-guidelines.md:287-323`, `.trellis/spec/backend/database-guidelines.md:390-557`).
- JSON import reports Node association candidates only for manual confirmation; import does not create `vps_node_links`, rewrite `nodes.provider`, or change Node/Target/Agent semantics (`.trellis/spec/backend/database-guidelines.md:559-569`; `docs/operations/asset-ledger-real-data-validation-readiness.md:39-49`, `docs/operations/asset-ledger-real-data-validation-readiness.md:322-327`).

#### 4. Dashboard and cross-page deep links must stay contract-backed

- Dashboard is global workbench, but it can only display facts returned by `/api/dashboard`; it must not treat every contract field as a first-screen display list (`.trellis/spec/web/state-and-data.md:102-114`).
- Dashboard command surface is asset-decision-first and contains three lanes: `资产决策队列`, `观测异常队列`, and `下一步动作`; asset summary is only high-weight in the asset lane and only as aggregated decision entrances, not detail rows (`docs/design/v2-houfeng/component-spec.md:202-220`; `.trellis/spec/web/state-and-data.md:104-113`).
- Dashboard supported deep links include `/nodes?onboarding=pending`, `/nodes?abnormal=1`, `/targets?abnormal=1`, `/targets?run_status=暂停`, `/targets?run_status=已归档`, `/events?severity=严重`, `/events?time_range=24h`, and `/events?maintenance_only=1`. New deep links must first be supported by URL-state and visible chips/toggles on the target page (`.trellis/spec/web/state-and-data.md:113`; `docs/design/v2-houfeng/component-spec.md:208`, `docs/design/v2-houfeng/component-spec.md:217`, `docs/design/v2-houfeng/component-spec.md:285`).
- `asset_summary` is limited to aggregated counts and cost groups; it must not include VPS/subscription/provider/node detail arrays (`.trellis/spec/backend/database-guidelines.md:570-581`; `.trellis/spec/web/state-and-data.md:351-425`).
- `snapshot_generated_at` is only dashboard response generation time and must not be phrased as center health, sync freshness, or agent heartbeat proof (`.trellis/spec/web/state-and-data.md:107`, `.trellis/spec/web/state-and-data.md:369-370`; `docs/design/v2-houfeng/component-spec.md:203-205`).

#### 5. Route/link destinations and search constraints

- Current AppShell sidebar groups navigation by product mind: `总览` → 工作台, `资产` → 资产决策/VPS/服务商/订阅, `观测` → 节点/目标/事件, `系统` → 设置 (`docs/design/v2-houfeng/component-spec.md:162-177`).
- Core route matrix includes `/`, `/asset-decisions`, `/vps`, `/vps/:vpsId`, `/nodes`, `/nodes/:nodeId`, `/targets`, `/targets/:targetId`, `/events`, and `/settings`; the visual evidence workflow also checks `/providers` and `/subscriptions` as protected Asset Ledger routes (`docs/operations/v2-visual-evidence.md:180-195`, `docs/operations/v2-visual-evidence.md:84-119`).
- GlobalSearch result links must point only to registered/landable routes. Objects with detail pages link to details (`/vps/:id`, `/nodes/:id`, `/targets/:id`). Objects without detail pages link to list or filtered-list pages; providers use `/providers`, subscriptions use `/subscriptions?vps_id=<vps_id>`. It explicitly forbids nonexistent `/providers/:id` or `/subscriptions/:id` routes (`.trellis/spec/web/component-conventions.md:51-56`).
- Shared business components must stay route-agnostic; if a shared component needs links/actions, the page passes `ReactNode` slots rather than importing router/domain-specific helpers inside the component (`.trellis/spec/web/component-conventions.md:38-44`, `.trellis/spec/web/component-conventions.md:130-145`).

#### 6. URL state must be visible and reversible on receiving pages

- VPS inventory URL-state supports `view=all|renewal|unreviewed|unlinked|missing_subscription|missing_facts|archived` and also `provider_id`, `lifecycle_status`, `usage_status`, and `renewal_decision` (`.trellis/spec/web/state-and-data.md:155-170`; `docs/design/v2-houfeng/component-spec.md:228-234`).
- Dashboard links into VPS pages must be carried by first-screen visible tabs/chips/drawer state; they must not be silently discarded (`.trellis/spec/web/state-and-data.md:169-170`).
- Node list URL-state must surface Dashboard/onboarding deep links: `onboarding=pending` displays a `待接入/绑定待处理` chip/toggle and matches lifecycle pending, unbound, or fingerprint-change-pending nodes (`docs/design/v2-houfeng/component-spec.md:243-256`).
- Target list URL-state must surface Dashboard deep links for `abnormal=1`, `run_status=暂停`, and `run_status=已归档`, with chip/toggle removal and clear-all writing URL back (`docs/design/v2-houfeng/component-spec.md:301-309`).
- Events page URL-state supports object, severity, event type, limit, time range, label, notification/recovery/maintenance filters, and `include_backfilled=1`; URL is the applied filter truth, and Drawer draft changes must not update URL or fetch until applied (`.trellis/spec/web/state-and-data.md:430-497`; `docs/design/v2-houfeng/component-spec.md:282-289`).

#### 7. Asset workflow page contracts

- Asset Decisions is the Asset Ledger main work queue, not three equal VPS status tables. The first screen must show a unified `资产决策工作队列` ordered by human processing priority: unreviewed, renewal window, migrate/cancel, unlinked Node, missing subscription, etc. (`docs/design/v2-houfeng/component-spec.md:221-227`; `.trellis/spec/web/state-and-data.md:147-193`).
- Decision editing is done in a `Drawer`/secondary surface. A successful save notice remains visible in the queue surface after the drawer closes (`docs/design/v2-houfeng/component-spec.md:224-226`; `.trellis/spec/web/state-and-data.md:162-165`).
- VPS page is a high-density inventory table for 40+ VPS validation, with quick views/tabs/chips/advanced filter drawer and table columns for identity/access, provider/region/product, subscription/cost/renewal/auto-renew, lifecycle/usage/decision, Node link count, data-quality badges, and labels (`docs/design/v2-houfeng/component-spec.md:228-234`; `.trellis/spec/web/state-and-data.md:147-193`).
- `VPSAssetRecord.active_node_link_count` may show Node link count or unlinked state only; it cannot imply linked Node health, heartbeat, or incident state on list pages unless backend contract adds those facts (`.trellis/spec/web/state-and-data.md:166`, `.trellis/spec/web/state-and-data.md:186-187`; `docs/design/v2-houfeng/component-spec.md:224`, `docs/design/v2-houfeng/component-spec.md:233`).
- VPS detail may show linked Node health/heartbeat/incident summary because `VPSAssetDetail.node_links` detail contract explicitly returns that evidence; this permission does not extend back to VPS list rows (`.trellis/spec/web/state-and-data.md:284-305`; `docs/design/v2-houfeng/component-spec.md:235-242`).
- Providers and Subscriptions have active protected routes. The current operations doc review focus for `/providers` is duplicate provider naming, account hints, ratings, labels, and update timestamps; for `/subscriptions`, price/monthly conversion, renewal sorting, status filters, and auto-renew labels (`docs/operations/asset-ledger-real-data-validation-readiness.md:181-188`).

#### 8. Service/domain linkage to Target is optional navigation, not automation

- Asset services are manual VPS-scoped records. `target_id` is optional and only for reference/jump; creating or listing services must not modify Target, ProbeItem, or observability semantics (`.trellis/spec/backend/database-guidelines.md:390-425`; `.trellis/spec/web/state-and-data.md:194-259`).
- VPS service UI displays name, type, status, URL/port, optional Target link, labels, and note; Target link only navigates and does not trigger Target creation or modification (`.trellis/spec/web/state-and-data.md:209-217`).
- Asset domains are manual VPS-scoped records. `service_id` and `target_id` are optional references; `target_id` is navigation-only and must not create/modify Service, Target, ProbeItem, or observation semantics (`.trellis/spec/backend/database-guidelines.md:475-557`; `.trellis/spec/web/state-and-data.md:260-349`).
- Domains/services are not timeline items and must not be inserted into Dashboard asset summary; creation refreshes only the corresponding service/domain list (`.trellis/spec/web/state-and-data.md:215-217`, `.trellis/spec/web/state-and-data.md:281-283`, `.trellis/spec/web/state-and-data.md:321-324`).

#### 9. Nodes/Targets remain observability support surfaces for assets

- Nodes page copy must position Node as VPS asset-decision runtime evidence, not an independent resource center. Its `资产判断支撑` surface can derive lanes from the loaded Node list only: abnormal evidence, onboarding/binding, maintenance/paused, VPS association; it must not do per-row VPS queries or display linked VPS health unavailable in the list contract (`docs/design/v2-houfeng/component-spec.md:243-256`).
- Targets page copy must position Target as service entry/probe coverage evidence, not a full service registry. Its `服务入口支撑` surface derives from the loaded Target list and may link to VPS ledger/asset decisions, but must not expand into a cross-page service registry (`docs/design/v2-houfeng/component-spec.md:301-309`).
- Target creation still follows V1: create short Target first, then add ProbeItem from Target detail/empty state; ProbeItem is only created in Target context (`docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:319-459`; `docs/design/v1-baseline/rules-and-interaction.md:369-421`).

#### 10. Node onboarding and install-command integration constraints

- Primary onboarding path is the center-generated one-command install command from Node onboarding page or `POST /api/nodes/{node_id}/install-command`; manual token issuance is only a troubleshooting/API fallback (`docs/operations/v1-smoke-run.md:3-18`, `docs/operations/v1-smoke-run.md:120-151`).
- Browser must not construct production install commands from `window.location.origin`; UI only displays backend `issue.command` (`.trellis/spec/web/state-and-data.md:38-93`; `.trellis/spec/backend/directory-structure.md:400-462`).
- Generated command contains a short-lived one-time enrollment token. UI may reveal/copy it only in deliberate surfaces and must not print the full token in incidental notices/conflict copy/logs; regeneration invalidates the prior token (`.trellis/spec/web/state-and-data.md:50-80`; `.trellis/spec/backend/database-guidelines.md:108-167`; `docs/operations/v1-smoke-run.md:146-150`).
- Binding conflict copy must state a pending fingerprint attempt may have consumed the one-time token, so regeneration may be required after confirm/reject (`.trellis/spec/web/state-and-data.md:54-55`; `docs/design/v2-houfeng/component-spec.md:326-337`).

#### 11. Interaction and state-change semantics that affect linkage fixes

- Observability is primary and configuration is secondary; structural editing and runtime controls are separate flows. Structure edit uses edit/drawer/form; runtime control uses state buttons/quick actions/confirmation surfaces (`docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:32-86`).
- High-frequency actions may be done in list/detail headers, but dangerous actions require stronger confirmation. Reset binding, archive, retire, and delete are high risk (`docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:87-115`, `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:463-505`).
- Prefer state over deletion: Node → retired, Target → archived/paused, ProbeItem → disabled; deletion is for mistaken objects, not normal management (`docs/design/v1-baseline/rules-and-interaction.md:450-462`; `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:538-558`).
- Recovery paths must be visible and history-preserving: restore state rather than rolling back facts; observation/events/notification records should not be erased to simulate undo (`docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:1211-1867`).
- Empty states follow a common pattern: explain what is absent, why it is not an error, and the recommended next action (`docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:1147-1174`; `docs/design/v2-houfeng/design-language.md:232-262`).

#### 12. Frontend data, component, and style contracts

- All business `/api/*` calls go through `web/src/lib/api.ts`; auth requests use `auth-client.ts` but share request helpers. Page/component direct `fetch()` is forbidden (`.trellis/spec/web/state-and-data.md:20-37`, `.trellis/spec/web/state-and-data.md:573-585`; `.trellis/spec/web/directory-structure.md:129-138`).
- Frontend types live in `web/src/lib/types.ts` and mirror center JSON snake_case; fields are not camel-cased in frontend (`.trellis/spec/web/state-and-data.md:31-37`).
- Current app has no React Query/SWR/Redux/Zustand; page-local `useState`/`useEffect` and URL state are the normal model (`.trellis/spec/web/state-and-data.md:7-17`, `.trellis/spec/web/state-and-data.md:544-571`).
- Page/components layering: pages fetch/assemble data; `components/` are controlled/pure; atoms are business-agnostic; pages must not import layout directly; components must not call API client (`.trellis/spec/web/component-conventions.md:21-58`, `.trellis/spec/web/directory-structure.md:101-145`).
- Loading/error/empty route/list states should use `PageState`; errors show local warning surfaces with technical summary rather than toasts (`.trellis/spec/web/component-conventions.md:44`; `docs/design/v2-houfeng/design-language.md:232-249`).
- DataTable row click has an interactive-descendant guard. For custom clickable queues/lists, visible `<Link>`/`<Button>` controls must not be nested under a keyboard-link outer container; row-background mouse click can navigate, but keyboard entry should remain on visible actions (`.trellis/spec/web/component-conventions.md:45-47`, `.trellis/spec/web/component-conventions.md:145`).
- Drawer/modal focus behavior reuses `web/src/lib/useModalFocus.ts`: portal, `role="dialog"`/`aria-modal`, focus trap, Escape close, restore trigger focus. Drawer cancellation must discard draft state and errors (`.trellis/spec/web/component-conventions.md:48-50`).
- Visual implementation uses pure CSS, BEM, design tokens, centralized style files, dark-first Chinese high-density UI, and no Tailwind/CSS-in-JS/chart library (`.trellis/spec/web/styling-guidelines.md:7-18`, `.trellis/spec/web/styling-guidelines.md:21-35`, `.trellis/spec/web/styling-guidelines.md:38-76`, `.trellis/spec/web/styling-guidelines.md:79-150`; `docs/design/v2-houfeng/design-language.md:312-325`).

#### 13. Visual/UX evidence and route sanity for unified linkage work

- User-visible UI work must be checked against v2 design docs and the active visual evidence workflow. `make verify-web` covers lint/test/build, but not visual proof (`.trellis/spec/web/quality-guidelines.md:205-217`; `docs/operations/v2-visual-evidence.md:20-31`).
- Asset protected route browser sanity can use `--mock-api asset-workflows`, covering `/asset-decisions`, `/vps`, `/providers`, `/subscriptions` with renewal/unreviewed/migrate/cancel/missing-subscription/unlinked/missing-facts/provider/subscription states (`docs/operations/v2-visual-evidence.md:84-119`).
- Observability protected route browser sanity can use `--mock-api observability-support`, covering `/nodes`, `/targets`, `/events` with abnormal, onboarding, binding conflict, maintenance/paused/retired, target coverage, event filters, and backfilled opt-in states (`docs/operations/v2-visual-evidence.md:121-156`).
- Broad UX tasks should cover the relevant route subset at `1440x1000` and `390x900`; table-heavy mobile screens may scroll horizontally rather than forcing all columns into narrow width (`docs/operations/v2-visual-evidence.md:180-207`).
- Screenshot directories/manifests and bulk raster evidence are not tracked by default; local screenshots are private/external unless explicitly approved as public docs assets (`docs/operations/v2-visual-evidence.md:208-237`; `docs/operations/asset-ledger-real-data-validation-readiness.md:181-190`).

### External References

None. This task requested internal project/Trellis specs and active design/operations docs only.

### Related Specs

- `.trellis/spec/web/state-and-data.md` — current frontend data/API contracts for Dashboard, Asset Ledger, Nodes/Targets/Events, Node onboarding, service/domain assets, and URL-state filters.
- `.trellis/spec/web/component-conventions.md` — current component layering, navigation/search link contract, Drawer/PageState/DataTable interaction rules.
- `.trellis/spec/web/styling-guidelines.md` — current CSS/tokens/BEM/dark-first visual implementation rules.
- `.trellis/spec/web/quality-guidelines.md` — frontend verification and cross-layer checklist.
- `.trellis/spec/backend/database-guidelines.md` — Asset Ledger and observability persistence/linkage invariants.
- `.trellis/spec/backend/directory-structure.md` — backend package/handler/store/wiring locations and one-command install contract.
- `.trellis/spec/backend/quality-guidelines.md` — required backend/frontend/test touchpoints when changing endpoints/schema/linkage.
- `.trellis/spec/guides/cross-layer-thinking-guide.md` — general cross-layer data-flow contract guide.
- `.trellis/spec/guides/code-reuse-thinking-guide.md` — general reuse/search-before-copy guide.

## Caveats / Not Found

- No source code was read for this research; line citations point to specs and active docs only.
- No external docs were searched because the requested scope was internal project/Trellis specs and active design/operations docs.
- Active v2 docs define detailed page contracts for Asset Decisions, VPS, VPS Detail, Nodes, Targets, Events, Node onboarding, and Dashboard. They do not contain full page-level visual sections for ProvidersPage or SubscriptionsPage; the active constraints found for those routes are in the sidebar route grouping, protected route/browser-sanity workflow, Asset Ledger readiness review focus, and backend provider/subscription contracts.
- `docs/design/v1-baseline/tech-selection.md` includes historical frontend recommendations such as Tailwind/TanStack Query/ECharts, but current `.trellis/spec/web/*` and `docs/design/v2-houfeng/*` explicitly state the implemented active constraints are pure CSS, no state/cache library, and no chart library. Use the current specs/v2 visual docs as the active implementation authority for frontend decisions.
