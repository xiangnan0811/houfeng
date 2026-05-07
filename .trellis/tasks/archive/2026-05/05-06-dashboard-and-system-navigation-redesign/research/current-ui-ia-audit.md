# Research: current UI IA audit

- Query: Research Houfeng's current frontend information architecture for the dashboard redesign.
- Scope: internal
- Date: 2026-05-06

## Findings

### Files Found

- `web/src/pages/DashboardPage.tsx` - dashboard page with overview fetch, abnormal node/target sections, group summary, recent event feed, and nested summary/table helpers.
- `web/src/pages/NodesPage.tsx` - node fleet list, URL-driven filters, create flow, runtime actions, batch actions, inline metadata editing, and sparkline strip.
- `web/src/pages/TargetsPage.tsx` - target list, URL-driven filters, create flow, runtime/archive actions, batch actions, inline metadata editing, and target latency sparklines.
- `web/src/pages/EventsPage.tsx` - event timeline/search page with local filter form, time-range tabs, grouped events, and load-more refetch behavior.
- `web/src/pages/SettingsPage.tsx` - large global settings form for theme, Telegram/Feishu, frequencies, incident defaults, override JSON, and retention.
- `web/src/app/layout/*` - app shell, static primary nav, breadcrumb, global search, sync status, user chip, and layout CSS.
- `web/src/app/metadata.ts` - product naming and primary IA nav source.
- `web/src/lib/types.ts` - hand-written frontend API contracts for nodes, targets, events, dashboard, settings, and sparkline responses.
- `web/src/lib/api.ts` - business API client and endpoint helpers consumed by the pages.
- `docs/design/v1-baseline/*` - frozen product/IA/data/interaction constraints.
- `docs/design/v2-houfeng/*` - active visual/component contracts for current redesign work.
- `.trellis/spec/web/*` - implementation guardrails for component boundaries, state/data, styling, and quality.

### Current Data Available

- Top-level routes are fixed to `/`, `/nodes`, `/nodes/:nodeId`, `/nodes/:nodeId/onboarding`, `/targets`, `/targets/:targetId`, `/events`, and `/settings`; unknown child routes redirect to `/` (`web/src/app/router.tsx:15`).
- Primary nav is a flat five-item IA: 首页, 节点, 目标, 事件, 设置 (`web/src/app/metadata.ts:12`; `docs/design/v1-baseline/rules-and-interaction.md:157`; `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:117`).
- `DashboardOverview` exposes fleet counts, abnormal/severe/maintenance counts, 24h incident/recovery counts, abnormal node/target summaries, recent events, and optional 24h incident/recovery trends (`web/src/lib/types.ts:270`).
- Dashboard node summaries expose identity/location/provider, lifecycle/monitoring/health, heartbeat freshness, active incident count, and primary issue summary (`web/src/lib/types.ts:298`); target summaries expose type, host/port, run status, health, success/failure times, active incident count, and primary issue (`web/src/lib/types.ts:313`).
- Dashboard page also fetches node CPU/memory/disk sparklines and target latency sparklines through `listNodeSparklines()` / `listTargetSparklines()` in nested abnormal-list components (`web/src/pages/DashboardPage.tsx:87`; `web/src/pages/DashboardPage.tsx:254`; `web/src/lib/api.ts:169`; `web/src/lib/api.ts:226`).
- Nodes page has full `NodeRecord` data including binding status, monitoring status, lifecycle status, labels/note, heartbeat/sync times, health, incidents, last action, and timestamps (`web/src/lib/types.ts:1`), plus local `NodeSparklinesResponse` (`web/src/lib/types.ts:482`) and node runtime action APIs (`web/src/lib/api.ts:178`).
- Targets page has full `TargetRecord` data including host/port, execution node labels, run status, labels/note, health, success/failure times, incidents, and timestamps (`web/src/lib/types.ts:123`), plus target latency sparkline data (`web/src/lib/types.ts:486`) and target runtime/archive APIs (`web/src/lib/api.ts:289`).
- Events page has `StateChangeEventRecord` and `EventListFilter` for object type/id, severity, event type, time range, label, notification/recovery/maintenance flags, and limit (`web/src/lib/types.ts:258`; `web/src/lib/types.ts:339`; `web/src/lib/api.ts:332`).
- Settings page has Telegram, Feishu, host/probe frequencies, incident thresholds/notification toggles, override rules, and retention policy (`web/src/lib/types.ts:360`; `web/src/lib/types.ts:472`; `web/src/pages/SettingsPage.tsx:29`).
- AppShell currently injects placeholder sync status and zero anomaly counts into the sidebar, not real dashboard data (`web/src/app/layout/AppShell.tsx:21`).
- Global search fetches full nodes and targets in parallel on submit, filters client-side by display name/id/location/provider/labels/host, shows six results, and navigates to detail pages (`web/src/app/layout/GlobalSearch.tsx:17`; `web/src/app/layout/GlobalSearch.tsx:48`; `web/src/app/layout/GlobalSearch.tsx:146`).

### Current UX Failures

- Dashboard heading says "当前风险总览", but the first strip collapses node and target risks into aggregate totals; severe/maintenance are also totals, so the user must scroll into lower sections to distinguish node-vs-target urgency (`web/src/pages/DashboardPage.tsx:510`; `docs/design/v1-baseline/rules-and-interaction.md:175` expects separate node/target counts).
- Dashboard `groupSummaries` are derived only from abnormal nodes/targets, so groups with healthy objects disappear and counts are not true group totals (`web/src/pages/DashboardPage.tsx:427`). If retained, label it as abnormal distribution or fetch true group totals.
- Sidebar anomaly badges are structurally supported, but AppShell hard-codes `{ nodes: 0, targets: 0 }`, so navigation never reflects current risk (`web/src/app/layout/AppShell.tsx:28`; `web/src/app/layout/Sidebar.tsx:34`).
- SyncStatus is also static (`state: 'ok'`, version `v1.0`, `lastSync: new Date()`), so the shell suggests live center health even without backend evidence (`web/src/app/layout/AppShell.tsx:23`; `web/src/app/layout/SyncStatus.tsx:16`).
- Dashboard sparkline fetches are duplicated with list pages and can issue separate requests for dashboard/node/target tables with silent failure and no loading distinction (`web/src/pages/DashboardPage.tsx:94`; `web/src/pages/DashboardPage.tsx:261`; `web/src/pages/NodesPage.tsx:270`; `web/src/pages/TargetsPage.tsx:346`).
- Dashboard and list pages use ad hoc severity mapping/sorting in each file instead of a shared health/status mapping, increasing drift risk (`web/src/pages/DashboardPage.tsx:68`; `web/src/pages/NodesPage.tsx:122`; `web/src/pages/TargetsPage.tsx:137`).
- Events page still uses `summary-card` as form-control containers and several inline layout styles, diverging from the existing `components/filters` pattern used by Nodes/Targets (`web/src/pages/EventsPage.tsx:247`; `web/src/pages/EventsPage.tsx:254`; `web/src/pages/EventsPage.tsx:446`).
- Events page's "include backfilled" control is disabled because the backend query has no backfill dimension, which is truthful but adds a visible dead control in a high-density filter area (`web/src/pages/EventsPage.tsx:27`; `web/src/pages/EventsPage.tsx:413`).
- Settings page has several inline spacing styles and is 1,145 lines, already called out by web specs as a known gap (`web/src/pages/SettingsPage.tsx:418`; `web/src/pages/SettingsPage.tsx:1125`; `.trellis/spec/web/component-conventions.md:127`; `.trellis/spec/web/styling-guidelines.md:118`).
- Nodes/Targets pages are very large and combine data fetching, filtering, forms, runtime actions, batch actions, metadata editing, table rendering, and confirmations in one file (`web/src/pages/NodesPage.tsx:215`; `web/src/pages/TargetsPage.tsx:292`; `.trellis/spec/web/component-conventions.md:138`).
- GlobalSearch has no cache, no ranking, and fetches full lists every submitted query; this is okay for current scale but becomes noticeable if dashboard/nav redesign encourages frequent search use (`web/src/app/layout/GlobalSearch.tsx:25`; `web/src/app/layout/GlobalSearch.tsx:59`).
- Breadcrumb intentionally hides on root and level-1 routes, so dashboard redesign should not rely on breadcrumb for primary orientation at those depths (`web/src/app/layout/Breadcrumb.tsx:20`; `web/src/app/layout/TopBar.tsx:4`).
- Current empty states are mostly text-only and do not yet match v2 empty-state expectations for an icon plus optional CTA (`web/src/pages/DashboardPage.tsx:100`; `docs/design/v2-houfeng/design-language.md:247`).

### Reusable Components / Patterns

- Keep page/page-shell layering: `app/layout` only composes shell, `pages` fetch and orchestrate, `components` stay controlled/presentational, `components/atoms` stay business-free (`.trellis/spec/web/component-conventions.md:25`; `.trellis/spec/web/component-conventions.md:38`).
- `DataTable` is the current high-density table primitive with compact rows, row hover/click, caption, per-cell alignment/class hooks, and tests (`docs/design/v2-houfeng/component-spec.md:87`; `web/src/styles/atoms.css:432`; `web/src/pages/NodesPage.tsx:1316`; `web/src/pages/TargetsPage.tsx:1309`).
- `DetailSection` is the standard section container with eyebrow/title/aside and optional top ribbon tone (`web/src/components/DetailSection.tsx:13`; `web/src/styles/pages.css:129`; `docs/design/v2-houfeng/component-spec.md:126`).
- `EventList` is reusable for dashboard and events timeline views; it already owns empty-state fallback and event type/object/severity labeling (`web/src/components/EventList.tsx:58`; `web/src/pages/DashboardPage.tsx:560`; `web/src/pages/EventsPage.tsx:440`).
- `StatusGlyph`, `StatusBadge`, `MonoDigits`, `Hostname`, `Timestamp`, and `Sparkline` are already applied across dashboard/list/event surfaces for status, technical identifiers, numeric metrics, and time (`docs/design/v2-houfeng/design-language.md:138`; `docs/design/v2-houfeng/design-language.md:207`; `web/src/pages/DashboardPage.tsx:4`).
- Nodes and Targets share a strong table pattern: URL-backed filters, `FilterBar` / `FilterChip` / `FilterSelect` / `FilterMultiSelect` / `FilterToggle`, row-click navigation, hover/focus-revealed actions, inline label/group editor, and below-table confirmation/error overlays (`web/src/pages/NodesPage.tsx:454`; `web/src/pages/NodesPage.tsx:1078`; `web/src/pages/TargetsPage.tsx:537`; `web/src/pages/TargetsPage.tsx:1133`).
- Runtime control semantics are already codified in page helpers and v1 docs: maintenance/resume are lighter operations; pause/archive/retire/reset need confirmation and clear state effects (`web/src/pages/NodesPage.tsx:161`; `web/src/pages/TargetsPage.tsx:203`; `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:95`; `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:463`).
- `lib/api.ts` is the only business fetch surface; keep new dashboard/nav data calls there and mirror response fields in `lib/types.ts` (`web/src/lib/api.ts:42`; `.trellis/spec/web/state-and-data.md:20`; `.trellis/spec/web/state-and-data.md:31`).
- Existing loading/error/data page pattern uses local state, `useEffect`, a `cancelled` flag, and `ApiError` handling; keep it unless a separate decision introduces a data cache layer (`web/src/pages/DashboardPage.tsx:401`; `.trellis/spec/web/state-and-data.md:46`).
- Current visual authority is v2-houfeng for visual language/component contracts, while business IA/data/behavior remain in v1-baseline frozen structural docs (`docs/design/v1-baseline/README.md:11`; `docs/design/v1-baseline/README.md:23`; `.trellis/spec/web/index.md:1`).

### Implementation Risks

- Do not change v1 product IA by adding new top-level modules such as rule center/probe center/notification center; v1 intentionally keeps those under objects or settings (`docs/design/v1-baseline/interactive-prototype-and-operation-flow.md:127`).
- Dashboard/nav redesign that needs real sidebar anomaly counts or real sync health may require backend/API work; current shell has only placeholders (`web/src/app/layout/AppShell.tsx:23`).
- `DashboardOverview` lacks true all-group totals, per-group healthy counts, and probe-item abnormal counts; deriving these from abnormal summaries will produce misleading IA if the UI labels them as fleet distribution (`web/src/lib/types.ts:270`; `web/src/pages/DashboardPage.tsx:427`).
- Avoid introducing a global store, React Query, Zustand, Redux, or another cache layer inside this redesign unless explicitly decided; current spec says no third-party state/cache layer is present or needed (`.trellis/spec/web/state-and-data.md:9`; `.trellis/spec/web/state-and-data.md:16`).
- Avoid adding direct `fetch()` in pages/components; all new business requests belong in `web/src/lib/api.ts` and new types in `web/src/lib/types.ts` (`.trellis/spec/web/state-and-data.md:24`; `web/src/lib/api.ts:313`).
- If dashboard shares sparkline data with Nodes/Targets or AppShell, be careful not to create duplicate requests or stale shell data without a deliberate invalidation story; current fetches are mount/interaction only and no polling exists (`.trellis/spec/web/state-and-data.md:116`; `web/src/pages/NodesPage.tsx:270`).
- High-density dashboard tables should keep `DataTable` semantics rather than reverting to card stacks; v2 explicitly made DataTable a core primitive (`docs/design/v2-houfeng/design-language.md:273`; `docs/design/v2-houfeng/component-spec.md:196`).
- Do not copy current inline layout styles from Events/Settings into new dashboard work; move new layout needs to `styles/pages.css` or `app/layout/layout.css` with BEM classes (`.trellis/spec/web/styling-guidelines.md:113`; `web/src/pages/EventsPage.tsx:446`).
- `toFixed()` currently appears inside pages for target latency display, but spec wants display formatting centralized in `lib/format.ts`; dashboard redesign should use/add format helpers for new numbers (`web/src/pages/DashboardPage.tsx:357`; `web/src/pages/TargetsPage.tsx:880`; `.trellis/spec/web/state-and-data.md:38`).
- Page tests exist for these pages and layout components; user-visible IA changes will need updated page/layout tests with mocked fetch and visible-behavior assertions (`.trellis/spec/web/quality-guidelines.md:78`; `web/src/pages/DashboardPage.test.tsx`; `web/src/app/layout/Sidebar.test.tsx`).

## Caveats / Not Found

- I did not inspect backend handlers or database queries; "current data available" is based on frontend types/API client and requested docs only.
- I did not run tests or render the UI; this is a repo-read IA audit, not visual verification.
- The requested path was explicit even though `task.py current --source` reported no active task; output was written only under the specified task `research/` directory.
