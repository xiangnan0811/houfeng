# Research: Route Verification Matrix

- **Query**: Research the Houfeng frontend route/page coverage for task `.trellis/tasks/05-17-ui`; inspect router, page components, tests, and style files; produce a verification matrix of all logged-in routes and key browser surfaces to exercise, including tables, forms, drawers, empty/error states where derivable.
- **Scope**: internal
- **Date**: 2026-05-17

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Central `createBrowserRouter` route registration; logged-in routes are children of `<RequireAuth />` + `<AppShell />`. |
| `web/src/app/RequireAuth.tsx` | Auth gate; unauthenticated users redirect to `/login?next=...`. |
| `web/src/app/layout/AppShell.tsx` | Logged-in shell with skip link, sidebar, top bar, global anomaly alert, change-password modal, and route `<Outlet />`. |
| `web/src/app/metadata.ts` | Primary navigation groups and labels used by logged-in shell. |
| `web/src/app/layout/Sidebar.tsx` | Sidebar navigation, anomaly counts, sync status, user chip. |
| `web/src/app/layout/TopBar.tsx` | Top bar composition with breadcrumb and global search. |
| `web/src/app/layout/Breadcrumb.tsx` | Breadcrumb behavior for nested/detail routes. |
| `web/src/app/layout/GlobalSearch.tsx` | Global search/command surface linking to VPS, nodes, targets, providers, subscriptions. |
| `web/src/app/layout/UserChip.tsx` | User menu with settings link, password modal entry, logout. |
| `web/src/app/layout/ChangePasswordModal.tsx` | Change-password modal form and validation. |
| `web/src/components/atoms/Drawer.tsx` | Portal drawer used by page-level secondary forms/history/filter panels; closes via overlay and Escape. |
| `web/src/components/atoms/Modal.tsx` | Modal atom used by settings notification channel flow. |
| `web/src/components/PageState.tsx` | Shared loading/error/empty state surface used across routes. |
| `web/src/components/ActionConfirmationCard.tsx` | Inline confirmation surface used for pause/archive/rebind/delete operations. |
| `web/src/components/EventList.tsx` | Event list/empty state renderer used by Events and detail pages. |
| `web/src/components/IncidentList.tsx` | Incident list/empty state renderer used by detail pages. |
| `web/src/components/filters/FilterBar.tsx` | Filter-bar composition used by VPS, subscriptions, nodes, targets. |
| `web/src/components/filters/FilterSelect.tsx` | Select control used in list filters and drawer filters. |
| `web/src/components/filters/FilterMultiSelect.tsx` | Multi-select filter control for labels and similar fields. |
| `web/src/components/filters/FilterToggle.tsx` | Toggle filter control used by events and list filters. |
| `web/src/pages/DashboardPage.tsx` | `/` page; dashboard command surface, workbench, refresh/auto-refresh, loading/error states. |
| `web/src/pages/VPSPage.tsx` | `/vps` page; inventory table, quick views, advanced filter drawer, create VPS drawer, subscription evidence states. |
| `web/src/pages/VPSDetailPage.tsx` | `/vps/:vpsId` page; VPS detail dashboard, renewal/facts/node-link/experience/service/domain drawers, timeline, lifecycle. |
| `web/src/pages/ProvidersPage.tsx` | `/providers` page; provider create/edit forms and providers table. |
| `web/src/pages/SubscriptionsPage.tsx` | `/subscriptions` page; subscription create/edit forms, filters, table. |
| `web/src/pages/AssetDecisionsPage.tsx` | `/asset-decisions` page; VPS decision queue, renewal evidence, decision drawer. |
| `web/src/pages/NodesPage.tsx` | `/nodes` page; node list/table, create node drawer, filters, runtime actions, batch bar, trends, compare selection. |
| `web/src/pages/nodes/CreateNodeDrawer.tsx` | Node creation drawer form fields and submit action. |
| `web/src/pages/nodes/NodesListSection.tsx` | Node list filters, batch panel, table, empty states, runtime overlays. |
| `web/src/pages/nodes/NodesBatchPanel.tsx` | Batch action bar, batch command panel, batch pause confirmation. |
| `web/src/pages/NodeComparePage.tsx` | `/nodes/compare` page; A/B comparison from `id` query params. |
| `web/src/pages/NodeDetailPage.tsx` | `/nodes/:nodeId` page; node details, runtime controls, binding/lifecycle/metadata/history/command flows. |
| `web/src/pages/node-detail/NodeDetailPageBody.tsx` | Node detail body sections and drawers. |
| `web/src/pages/NodeOnboardingPage.tsx` | `/nodes/:nodeId/onboarding` page; one-command install, token reveal/copy, binding conflict actions, stepper. |
| `web/src/pages/TargetsPage.tsx` | `/targets` page; target list/table, create target drawer, filters, runtime actions, sparklines. |
| `web/src/pages/targets/CreateTargetPanel.tsx` | Structured create-target drawer form fields and actions. |
| `web/src/pages/TargetDetailPage.tsx` | `/targets/:targetId` page; target detail, ProbeItem CRUD, runtime controls, metadata, history. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Target detail body sections, ProbeItem panels, drawers/confirmations. |
| `web/src/pages/EventsPage.tsx` | `/events` page; event timeline, URL filters, advanced filter drawer, load-more. |
| `web/src/pages/events/EventsFilterDrawer.tsx` | Events advanced filter drawer fields, toggles, apply/reset/close actions. |
| `web/src/pages/events/EventsStreamSection.tsx` | Event grouping and load-more/empty state behavior. |
| `web/src/pages/SettingsPage.tsx` | `/settings` page; settings tabs, theme, notification, incident, override rules, retention. |
| `web/src/pages/LoginPage.tsx` | `/login` unauthenticated route; excluded from logged-in-route matrix but relevant for redirect sanity. |
| `web/src/styles/pages.css` | Page-level layout, panels, page states, dashboard, lists, detail, drawer-form classes. |
| `web/src/styles/atoms.css` | Atom styles including buttons, inputs, badges, cards, data tables, drawers, modals, stepper. |
| `web/src/app/layout/layout.css` | App shell, sidebar, top bar, breadcrumb, global alert/search, user menu, layout modal. |
| `web/src/components/filters/filters.css` | Filter bar/select/multiselect/toggle/chip styles. |
| `web/src/pages/LoginPage.css` | Login page styles; not part of logged-in matrix. |
| `web/src/pages/*.test.tsx` | Colocated page tests covering route rendering, data fetch, primary interactions, empty/error states. |
| `.trellis/spec/web/directory-structure.md` | Web organization conventions and route/page placement rules. |
| `.trellis/spec/web/quality-guidelines.md` | Verification commands, testing expectations, browser sanity guidance. |
| `.trellis/tasks/05-17-ui/prd.md` | Active task requirements and acceptance criteria for all logged-in UI theme/layout sweep. |

### Code Patterns

#### Router/auth/layout pattern

- `web/src/app/router.tsx:62-99` declares `appRoutes`. `/login` is outside auth; logged-in routes are nested under `element: <RequireAuth />` and then `path: '/'`, `element: <AppShell />`.
- `web/src/app/router.tsx:71-94` registers logged-in routes:
  - index `/`
  - `/vps`
  - `/vps/:vpsId`
  - `/providers`
  - `/subscriptions`
  - `/asset-decisions`
  - `/nodes`
  - `/nodes/compare`
  - `/nodes/:nodeId`
  - `/nodes/:nodeId/onboarding`
  - `/targets`
  - `/targets/:targetId`
  - `/events`
  - `/settings`
  - wildcard `*` redirects to `/`.
- `web/src/app/router.tsx:54-59` wraps page modules in `Suspense` with `RouteModuleFallback`, so browser verification should include route-level lazy loading if possible.
- `web/src/app/RequireAuth.tsx:4-12` gates all logged-in routes; while auth is loading it returns `null`, and unauthenticated access redirects to `/login?next=<current path+search>`.
- `web/src/app/layout/AppShell.tsx:84-102` wraps every logged-in page with `.app-shell`, skip link, `Sidebar`, `TopBar`, `GlobalCriticalAlert`, route `<Outlet />`, and `ChangePasswordModal` when open.
- `web/src/app/layout/AppShell.tsx:110-147` renders the global anomaly alert only when dashboard summary exists and abnormal/severe counts are positive. Its action links deep-link to `/events?severity=严重`, `/nodes?abnormal=1`, `/targets?abnormal=1`.
- `web/src/app/metadata.ts:17-43` defines the primary navigation groups: 总览, 资产, 观测, 系统.

#### Drawer/modal pattern

- `web/src/components/atoms/Drawer.tsx:20-65` renders a portal drawer to `document.body`; it mounts only when open, uses `role="dialog"`, `aria-modal="true"`, closes by overlay `onMouseDown`, and relies on `useModalFocus` for Escape/focus handling.
- `web/src/pages/events/EventsFilterDrawer.tsx:49-176` shows the typical advanced-filter drawer pattern: `<Drawer>` + form + grouped fields + apply/reset/close actions.
- `web/src/pages/nodes/CreateNodeDrawer.tsx:30-118` is a direct node-creation drawer use. It currently renders title `节点创建`, description, raw `<p><label><input>` groups, error text, and submit button `创建并进入接入工作台`.
- `web/src/pages/targets/CreateTargetPanel.tsx:30-172` is a structured drawer-body form reference using `target-create-drawer__*` classes, explicit description, cancel/submit actions, and alert role for errors.
- `web/src/pages/SettingsPage.tsx:1-7` imports `Modal` for notification channel modal; `web/src/app/layout/ChangePasswordModal.tsx` supplies a layout-level password modal.

#### Loading/error/empty state pattern

- `PageState` is consistently used for route-level loading/error/empty surfaces. Examples:
  - `web/src/pages/DashboardPage.tsx:59-72`: `正在加载工作台…` and `工作台不可用`.
  - `web/src/pages/NodeComparePage.tsx:74-84`: missing two IDs yields empty state `需要选择 2 个节点` with action back to `/nodes`.
  - `web/src/pages/nodes/NodesListSection.tsx:100-119`: first-run or binding-conflict empty state.
  - `web/src/pages/nodes/NodesListSection.tsx:161-172`: filtered-empty state `没有匹配当前筛选的节点` with clear-filter action.
  - `web/src/pages/events/EventsStreamSection.tsx:92-108`: timeline empty state uses `EventList` with different copy for filtered vs default mode.

#### URL/deep-link filter pattern

- `web/src/pages/VPSPage.tsx:187-209` parses and serializes filter state from URL query params for quick view/provider/lifecycle/usage/renewal filters.
- `web/src/pages/SubscriptionsPage.tsx:88-114` parses `vps_id`, `status`, and `renew_within_days` from URL params and converts them to API filters.
- `web/src/pages/EventsPage.tsx:60-132` parses and canonicalizes timeline filters including object type, severity, event type, limit, custom range, labels, notification/recovery/maintenance/backfilled toggles.
- `web/src/pages/NodeComparePage.tsx:65-72` reads two `id` query parameters for A/B comparison.

#### Runtime confirmation and stale-route protection pattern

- `web/src/pages/NodesPage.tsx:166-210` opens confirmation for node pause and keeps runtime errors local to row overlays.
- `web/src/pages/nodes/NodesBatchPanel.tsx:132-144` uses `ActionConfirmationCard` for batch pause.
- `web/src/pages/TargetsPage.tsx:197-245` confirms target pause/archive before mutation and restores focus afterward.
- `web/src/pages/NodeDetailPage.tsx:99-108` keeps route/action refs to avoid stale route/action responses mutating the wrong node detail.
- `web/src/pages/TargetDetailPage.tsx:99-118` keeps route/action/probe refs to avoid stale target/probe mutations after route changes.

#### Style anchors

- `web/src/styles/pages.css:5-223` defines page stack, panels, route fallback, and page-state surfaces.
- `web/src/styles/pages.css:317-616` defines dashboard command surface and dashboard command controls.
- `web/src/styles/atoms.css:6-82` defines `.btn` variants and disabled state.
- `web/src/styles/atoms.css:337-434` defines `DataTable` structure, sortable headers, clickable rows, and empty table surface.
- `web/src/styles/atoms.css:480-488` defines drawer overlay/drawer/header/title/close/body selectors.
- `web/src/styles/atoms.css:491-541` defines modal overlay/content/header/close selectors.
- `web/src/app/layout/layout.css:3-47` defines skip link and app-shell main layout.
- `web/src/app/layout/layout.css:58-180` defines sidebar navigation.
- `web/src/app/layout/layout.css:183-225` defines sync status.
- `web/src/app/layout/layout.css:232-323` defines user chip/menu.
- `web/src/app/layout/layout.css:356-405` defines top bar and breadcrumb.
- `web/src/app/layout/layout.css:407-487` defines global critical alert.
- `web/src/app/layout/layout.css:489-592` defines global search.
- `web/src/components/filters/filters.css:6-66` defines filter bar/control/chip layout.
- `web/src/components/filters/filters.css:67-181` defines filter select and multiselect controls.
- `web/src/components/filters/filters.css:182-240` defines filter toggle and chip controls.

### Logged-in Route Verification Matrix

| Route | Page entry | Primary browser surfaces to exercise | Tables / lists | Forms / drawers / modals / confirmations | Loading / empty / error states derivable | Test references |
|---|---|---|---|---|---|---|
| `/` | `web/src/pages/DashboardPage.tsx` | Dashboard command surface; Dashboard workbench; refresh button; auto-refresh select; severe/abnormal/maintenance/normal/first-install modes; links to asset decisions, VPS, nodes, targets, events. | Attention queue rows and compact management entries in dashboard command/workbench sections. | Refresh/auto-refresh controls only; no route-level drawer in this page. Layout-level user menu/change-password modal still applies. | `正在加载工作台…`; `工作台不可用`; first-run onboarding when node/target counts are zero. | `web/src/pages/DashboardPage.test.tsx:86`, `:321`, `:374`, `:419`, `:487`, `:546`, `:583`, `:594`. |
| `/asset-decisions` | `web/src/pages/AssetDecisionsPage.tsx` | Unified VPS decision queue; renewal window selector; queue tabs all/unreviewed/renewal/migrate/cancel/unlinked/missing_subscription; focus metrics; row navigation. | Decision queue rows; renewal evidence table via `AssetDecisionRenewalTable`. | Decision drawer via `Drawer`; `AssetDecisionWorkPanel` for renewal decision draft; drawer close/cancel/Escape/overlay. | `正在加载 VPS 决策队列…`; `资产决策队列不可用`; empty queue copy `当前视图暂无待处理 VPS`; subscription evidence failure distinct from missing subscription. | `web/src/pages/AssetDecisionsPage.test.tsx:90`, `:159`, `:208`, `:234`, `:268`, `:313`. |
| `/vps` | `web/src/pages/VPSPage.tsx` | VPS page intro; quick views all/renewal/unreviewed/unlinked/missing_subscription/missing_facts/archived; inventory focus metrics; active filter chips; row navigation to detail. | VPS inventory `DataTable`; subscription evidence state per row. | Advanced filter drawer; create VPS drawer; clear-filter/create actions from empty table; create submit navigates to detail. | `正在加载 VPS…`; `VPS 库存不可用`; empty table state; subscription evidence failure notice; missing subscription only after subscription evidence is ready. | `web/src/pages/VPSPage.test.tsx:105`, `:166`, `:197`, `:254`, `:275`. |
| `/vps/:vpsId` | `web/src/pages/VPSDetailPage.tsx` | VPS hero; decision workbench; renewal evidence; decision evidence; linked node summaries; facts; services/domains context; timeline; access summary; lifecycle card. | Timeline panel; services list; domains list; linked node summaries; subscriptions evidence. | Drawers/modes for renewal decision, facts edit, node link, experience log, service create, domain create; unlink action; archive/restore lifecycle confirmation. | `VPSDetailLoading`; `VPSDetailErrorPanel`; `VPSDetailMissingID`; empty timeline; subscription load failure vs true missing subscription; service/domain validation errors. | `web/src/pages/VPSDetailPage.test.tsx:84`, `:294`, `:371`, `:445`, `:573`, `:696`, `:874`, `:999`, `:1123`, `:1235`, `:1303`, `:1383`, `:1481`, `:1539`, `:1648`, `:1707`. |
| `/providers` | `web/src/pages/ProvidersPage.tsx` | Providers page with provider identities, labels, ratings, timestamps. | Providers `DataTable`. | Create provider panel; edit provider panel; validation/errors local to panel. | `正在加载服务商…`; `服务商列表不可用`; empty content `暂无服务商`; invalid provider input. | `web/src/pages/ProvidersPage.test.tsx:19`, `:77`, `:91`. |
| `/subscriptions` | `web/src/pages/SubscriptionsPage.tsx` | Subscription management; links back to VPS; filters by VPS/status/renewal window. | Subscriptions `DataTable`, backend monthly price display. | Create subscription panel; edit subscription panel; form validation for VPS, price, billing months, currency. | `正在加载订阅…`; `订阅列表不可用`; `暂无订阅`; filtered state via URL params. | `web/src/pages/SubscriptionsPage.test.tsx:69`, `:100`, `:151`. |
| `/nodes` | `web/src/pages/NodesPage.tsx` | Nodes hero; support surface; list view tabs including binding conflict; filter bar; evidence lead; sparklines/trend toggle; auto-refresh; row navigation; onboarding path. | Nodes `DataTable`; trends column with three mini sparklines; identity column timestamps; binding-conflict filtered rows. | Create node drawer (`节点创建`); row metadata editor; row runtime actions; pause confirmation; batch bar when filters active; batch command panel; batch pause confirmation; compare selection. | `正在加载节点列表…`; `节点列表不可用`; `候风尚未接入任何节点`; `没有绑定异常节点`; `没有匹配当前筛选的节点`; create errors local to drawer/page. | `web/src/pages/NodesPage.test.tsx:71`, `:161`, `:198`, `:233`, `:277`, `:330`, `:362`, `:489`, `:559`, `:592`, `:637`, `:918`, `:959`, `:1034`, `:1083`, `:1166`, `:1240`, `:1307`, `:1353`, `:1380`, `:1475`, `:1558`. |
| `/nodes/compare?id=<a>&id=<b>` | `web/src/pages/NodeComparePage.tsx` | A/B identity cards; link back to node list; links to each node detail; metrics comparison columns. | Two metric columns via `NodeWatchtowerMetrics`; no route-level table. | No drawer/modal; URL query is primary control. Layout-level user menu/change-password modal still applies. | Missing/insufficient IDs empty state `需要选择 2 个节点`; per-side loading cards `A 节点读取中`/`B 节点读取中`; per-side error `A 节点不可用`/`B 节点不可用`; metric placeholder `指标读取中`/`指标不可用`. | `web/src/pages/NodeComparePage.test.tsx:93`, `:101`, `:131`. |
| `/nodes/:nodeId` | `web/src/pages/NodeDetailPage.tsx` | Node watchtower header; host sample cards; metric grid; time-window tabs; linked VPS section; lifecycle; access credential state; metadata; container list; danger zone; snapshot meta. | Incidents list; events list; historical incidents/events in drawer; containers list; linked VPS summaries. | Runtime operations menu; pause confirmation; maintenance controls; retire/restore lifecycle confirmation; binding conflict actions; metadata edit form; history drawer; command drawer with presets and dispatch. | Loading via `NodeDetailLoading`; unavailable via `NodeDetailUnavailable`; `节点不存在`; empty recent samples; empty first-sync/incidents/events; related incidents/events can fail while core detail remains visible; stale-route protection. | `web/src/pages/NodeDetailPage.test.tsx:157`, `:231`, `:369`, `:504`, `:538`, `:588`, `:673`, `:782`, `:1125`, `:1206`, `:1421`, `:1463`, `:1530`, `:1631`, `:1956`, `:2591`, `:2630`, `:2674`, `:2802`, `:3028`, `:3178`, `:3275`, `:3330`. |
| `/nodes/:nodeId/onboarding` | `web/src/pages/NodeOnboardingPage.tsx` | Onboarding hero; lifecycle/monitoring/binding/phase badges; stepper; one-command install panel; manual fallback snippets; conflict metadata. | Checklist and fingerprint metadata rows; no data table. | Generate install command; hide/reveal command; copy command; regenerate command; binding conflict `ActionConfirmationCard` for confirm/reject/reset; reset-binding ghost action. | Onboarding unavailable/error panel; missing center install config warning; unbound/waiting/completed/conflict stepper states; binding action errors local to conflict section. | `web/src/pages/NodeOnboardingPage.test.tsx:62`, `:92`, `:120`, `:151`, `:195`, `:220`, `:272`, `:313`, `:357`, `:399`, `:434`, `:480`, `:536`, `:587`, `:619`, `:694`, `:772`. |
| `/targets` | `web/src/pages/TargetsPage.tsx` | Targets intro/support surface; filter bar; evidence lead; target runtime quick actions; row navigation; sparklines/trends. | Targets `DataTable`; latency sparkline trends column. | Create target drawer with structured `CreateTargetPanel`; row metadata editor; pause confirmation; archive confirmation; restore action; runtime overlays. | `目标列表不可用`; first empty `候风尚未配置任何观测目标`; filtered empty `没有匹配当前筛选的目标`; create errors local to drawer; stale create responses ignored. | `web/src/pages/TargetsPage.test.tsx:66`, `:147`, `:173`, `:201`, `:234`, `:268`, `:332`, `:404`, `:502`, `:532`, `:639`, `:690`, `:735`, `:1082`, `:1115`, `:1156`, `:1218`, `:1263`, `:1339`, `:1482`, `:1540`, `:1578`, `:1623`. |
| `/targets/:targetId` | `web/src/pages/TargetDetailPage.tsx` | Target watchtower header; latency trends; time-window tabs; probe list; probe management; metadata; lifecycle; snapshot meta; danger zone. | ProbeItem list; recent latency samples grouped by ProbeItem; incidents/events; historical drawer lists. | ProbeItem create/edit form; ProbeItem enable/disable/delete row actions; delete confirmation; runtime pause/archive confirmations; metadata edit; history drawer. | Loading via `TargetDetailLoading`; unavailable via `TargetDetailUnavailable`; probe/incident/event empty states; latency empty state; related incidents/events can fail while core detail remains visible; stale target/probe mutation protection. | `web/src/pages/TargetDetailPage.test.tsx:52`, `:201`, `:330`, `:386`, `:433`, `:546`, `:706`, `:757`, `:1015`, `:1220`, `:1322`, `:1412`, `:1484`, `:1547`, `:1611`, `:1773`, `:1865`, `:2076`, `:2153`, `:2302`, `:2453`, `:2633`, `:2824`, `:2875`, `:3357`, `:3405`, `:3559`. |
| `/events` | `web/src/pages/EventsPage.tsx` | Page intro `审计与诊断时间线`; support surface; evidence lead; active filter overview; grouped event stream; load-more behavior. | Event stream grouped into 今天/昨天/本周/更早 via `EventsStreamSection`; `EventList` renders events/empty states. | Advanced `事件筛选` drawer; time range tabs; object/severity/event type/limit selects; notification/recovery/maintenance/backfilled toggles; custom start/end; label; apply/reset/close; active-chip removal. | `正在加载事件…`; `事件不可用`; `最近没有状态变更事件`; `没有匹配的事件`; invalid URL params normalized/ignored; exhausted load-more state `无更多事件`. | `web/src/pages/EventsPage.test.tsx:51`, `:139`, `:170`, `:196`, `:230`, `:247`, `:275`, `:339`, `:379`, `:438`, `:482`, `:515`, `:544`, `:562`, `:573`, `:587`, `:625`, `:646`. |
| `/settings` | `web/src/pages/SettingsPage.tsx` | Settings tabs general/notifications/advanced; theme settings; frequency defaults; Telegram/Feishu settings; incident defaults; override rules; retention policy; save status. | No route-level data table; JSON preview panel for valid override arrays. | Notification channel modal; Telegram runtime-management controls; Feishu webhook fields; override JSON textareas; save button; save success/error panels. | `正在加载设置…`; `设置不可用`; local validation errors for malformed integers/JSON and Telegram token/chat combinations; dismissed channel draft not persisted. | `web/src/pages/SettingsPage.test.tsx:106`, `:169`, `:195`, `:282`, `:390`, `:470`, `:497`, `:527`, `:556`. |
| `*` under logged-in shell | `web/src/app/router.tsx` | Unknown logged-in route redirects to `/`. | N/A | N/A | Redirect sanity only. | Router-level behavior is declared at `web/src/app/router.tsx:94`. |

### Cross-route Browser Surfaces to Exercise

| Surface | Where to exercise | Notes |
|---|---|---|
| Logged-in shell | Any logged-in route; preferably `/`, `/nodes`, `/settings` | Verify `.app-shell`, skip link, sidebar active state, top bar, breadcrumb, global alert, global search, user chip. |
| Sidebar navigation | `/`, `/asset-decisions`, `/vps`, `/providers`, `/subscriptions`, `/nodes`, `/targets`, `/events`, `/settings` | Navigation groups are defined in `metadata.ts`; anomaly count badges appear for nodes/targets when dashboard summary contains counts. |
| Breadcrumb | Detail/nested routes: `/vps/:vpsId`, `/nodes/:nodeId`, `/nodes/:nodeId/onboarding`, `/targets/:targetId` | Breadcrumb is hidden on root and level-1 routes; verify nested route trail and current item. |
| Global search | App shell from any logged-in route | Trigger via input and keyboard shortcut (`⌘K`/`Ctrl+K` where supported); verify result groups and links to VPS/node/target/providers/subscriptions. |
| User menu | App shell from any logged-in route | Verify settings link, change password modal, logout action; change-password modal validates current/new/confirm fields. |
| Global anomaly alert | App shell with dashboard abnormal/severe summary | Verify alert vs status role/copy and links to `/events?severity=严重`, `/nodes?abnormal=1`, `/targets?abnormal=1`. |
| Route fallback | Any lazily loaded route | `routeElement` uses `RouteModuleFallback`; browser can observe only if chunk load is slow. |
| Drawer atom | `/nodes`, `/targets`, `/events`, `/vps`, `/vps/:vpsId`, `/asset-decisions`, `/nodes/:nodeId`, `/targets/:targetId` | Because `Drawer` portals to `document.body`, verify theme inheritance/background/text/border/focus for both houfeng dark and houfeng light. |
| Modal atom/layout modal | `/settings`; user menu from any route | Settings notification modal and change-password modal should be checked in both themes. |
| DataTable atom | `/vps`, `/providers`, `/subscriptions`, `/nodes`, `/targets`, decision/probe tables where rendered | Verify header, compact density, row hover/click, sortable headers where available, scroll-x panels, empty table states. |
| Filter components | `/vps`, `/subscriptions`, `/nodes`, `/targets`, `/events` | Verify select, multiselect, toggle, active chips, clear actions, URL sync/deep links. |
| ActionConfirmationCard | `/nodes`, `/nodes/:nodeId`, `/targets`, `/targets/:targetId`, `/nodes/:nodeId/onboarding`, `/vps/:vpsId` | Verify warning copy, confirm/cancel focus, disabled/submitting, local error persistence. |
| Empty/error/loading surfaces | Every route | Use `PageState` copy and route-specific empty states listed in the matrix. |
| Light/dark themes | Every logged-in route and each drawer/modal/form/table surface | Active task requires houfeng light and houfeng dark parity. Theme setting lives under `/settings`. |

### Browser Verification Checklist by Task Acceptance Criteria

- `CreateNodeDrawer`:
  - Open from `/nodes` section heading button.
  - Verify title `节点创建`, description, fields 显示名称 / Group / 地区 / 城市 / 供应商 / 标签 / 备注, error text, disabled/submitting submit button.
  - Exercise golden path: fill required fields, submit, navigate to `/nodes/:nodeId/onboarding` without pre-issuing token.
  - Exercise local create API failure and validation/readability in houfeng light/dark.
- All `<Drawer>` uses explicitly named by the task:
  - Events filter drawer: `/events` -> `事件筛选`.
  - VPS create/filter drawers: `/vps`.
  - VPS detail drawers: `/vps/:vpsId` decision/facts/node-link/experience/service/domain.
  - Asset Decisions drawer: `/asset-decisions` decision handling.
  - Target create drawer: `/targets` -> create target.
  - Node Detail history/command drawers: `/nodes/:nodeId`.
  - Target Detail history/probe drawer/form surfaces: `/targets/:targetId`.
- All logged-in route main pages:
  - `/`, `/vps`, `/providers`, `/subscriptions`, `/asset-decisions`, `/nodes`, `/nodes/compare`, `/nodes/:nodeId`, `/nodes/:nodeId/onboarding`, `/targets`, `/targets/:targetId`, `/events`, `/settings`.
  - Detail pages require data IDs; record uncovered pages if local fixture/production data cannot provide IDs.
- Main Modal/Form/Table states:
  - Modal: change-password user menu; settings notification channel modal.
  - Forms: provider/subscription/VPS/node/target/create-edit forms, settings forms, metadata forms, ProbeItem forms.
  - Tables/lists: VPS, providers, subscriptions, decision queues, nodes, targets, ProbeItems, events/incidents, timeline/services/domains.
  - States: loading, empty, error, filtered, submitting/disabled, dangerous confirmation/error.

### Related Specs

- `.trellis/spec/web/directory-structure.md` — documents route registration in `web/src/app/router.tsx`, page placement in `web/src/pages/`, component and style organization.
- `.trellis/spec/web/quality-guidelines.md` — documents `make verify-web`, page-test conventions, and browser sanity guidance.
- `.trellis/tasks/05-17-ui/prd.md` — active task requirements and acceptance criteria for all logged-in route UI/theme/layout sweep.
- `docs/design/v2-houfeng/design-language.md` — visual authority for dark-first but light-equivalent tokenized UI.
- `docs/design/v2-houfeng/component-spec.md` — component-level visual/behavior authority for drawer/page surfaces.

### External References

None. This research was internal-only and based on the repository files and active Trellis task/spec context.

## Caveats / Not Found

- `/login` is intentionally excluded from the logged-in route matrix because it is outside `<RequireAuth />` in `web/src/app/router.tsx:63`; it remains relevant for redirect/login sanity only.
- The logged-in wildcard route redirects to `/` and has no standalone browser surface beyond redirect sanity.
- Detail routes (`/vps/:vpsId`, `/nodes/:nodeId`, `/nodes/:nodeId/onboarding`, `/targets/:targetId`) require valid IDs and data states; if a browser environment lacks fixture/production data, record those states as not covered and use available unit tests as supporting evidence.
- Route-level fallback UI may be hard to observe in a fast local build because lazy chunks load quickly; it is still registered through `RouteModuleFallback`.
- This matrix describes what exists and what to exercise. It does not propose implementation changes beyond enumerating current surfaces required by the task.
