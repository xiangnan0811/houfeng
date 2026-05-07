# Research: Peer Dashboard Patterns

- Query: Research dashboard/home patterns in comparable server management or observability tools using official sources where possible; focus on small server management/control-plane needs: global health, inventory, incidents/events, quick navigation/actions, onboarding/empty states, and what to avoid.
- Scope: mixed
- Date: 2026-05-06

## Findings

### Files Found

- `.trellis/workflow.md` — Trellis workflow requires research artifacts under `{task_dir}/research/` and no product-code edits during research-only work.
- `.trellis/spec/web/index.md` — web spec entry point; confirms the relevant layer is the React/Vite SPA front end.
- `.trellis/spec/web/component-conventions.md` — component ownership rules: pages fetch/compose, `components/` stay presentational, `app/layout/*` is AppShell-only.
- `.trellis/spec/web/state-and-data.md` — current data pattern: business API calls through `web/src/lib/api.ts`; page-level loading/error/data states; no polling/SSE/global data cache.
- `.trellis/spec/web/styling-guidelines.md` — dark-first, dense engineering UI, pure CSS/BEM/tokens; no new per-page CSS files for normal pages.
- `docs/design/v2-houfeng/component-spec.md` — current visual contract for Sidebar, SyncStatus, AppShell, DashboardPage, data tables, event lists, and dashboard stat strip.
- `web/src/pages/DashboardPage.tsx` — current home/dashboard implementation: risk stats, abnormal node/target tables, group distribution derived from abnormal objects, recent events, and first-install onboarding.
- `web/src/app/layout/AppShell.tsx` — current shell composition and placeholder global sync/anomaly counts.
- `web/src/app/layout/Sidebar.tsx` — navigation structure, neutral count badges for node/target anomaly counts, SyncStatus and UserChip placement.
- `web/src/app/layout/GlobalSearch.tsx` — minimal global search that fetches node/target lists on explicit submit and navigates to matching detail pages.

### Code Patterns

- Current DashboardPage is already risk-first rather than metric-wall-first: top stats are computed from current abnormal/severe/maintenance/recent incident counts, then the page drills into abnormal nodes, abnormal targets, group distribution, and recent events (`web/src/pages/DashboardPage.tsx:461`, `web/src/pages/DashboardPage.tsx:510`, `web/src/pages/DashboardPage.tsx:528`, `web/src/pages/DashboardPage.tsx:537`, `web/src/pages/DashboardPage.tsx:560`).
- Current first-install state is explicit and says "no nodes/targets" is not an error, then gives an ordered path to create a node, connect an agent, create a target, and add a ProbeItem (`web/src/pages/DashboardPage.tsx:462`, `web/src/pages/DashboardPage.tsx:468`, `web/src/pages/DashboardPage.tsx:479`).
- Current abnormal node/target rows already support severity ordering, freshness, current primary issue, compact trend strips, and row/detail navigation (`web/src/pages/DashboardPage.tsx:87`, `web/src/pages/DashboardPage.tsx:109`, `web/src/pages/DashboardPage.tsx:153`, `web/src/pages/DashboardPage.tsx:166`, `web/src/pages/DashboardPage.tsx:223`, `web/src/pages/DashboardPage.tsx:254`, `web/src/pages/DashboardPage.tsx:274`, `web/src/pages/DashboardPage.tsx:321`, `web/src/pages/DashboardPage.tsx:336`, `web/src/pages/DashboardPage.tsx:370`).
- Current group distribution only counts objects present in `abnormal_nodes` / `abnormal_targets`, so it is not a real inventory distribution yet; it will omit healthy groups entirely (`web/src/pages/DashboardPage.tsx:427`, `web/src/pages/DashboardPage.tsx:434`, `web/src/pages/DashboardPage.tsx:439`, `web/src/pages/DashboardPage.tsx:546`).
- AppShell currently passes hard-coded global status into Sidebar: `sync.state='ok'`, `version='v1.0'`, `lastSync=new Date().toISOString()`, and zero anomaly counts. A redesign that makes shell status credible needs a real data source or should demote/remove these placeholders (`web/src/app/layout/AppShell.tsx:21`, `web/src/app/layout/AppShell.tsx:23`, `web/src/app/layout/AppShell.tsx:28`).
- Sidebar already encodes the design decision that navigation counts should be informational and neutral rather than alarm-colored. The visual spec explicitly says nav items must not carry alarm semantics (`web/src/app/layout/Sidebar.tsx:33`, `web/src/app/layout/Sidebar.tsx:51`, `docs/design/v2-houfeng/component-spec.md:162`, `docs/design/v2-houfeng/component-spec.md:169`).
- GlobalSearch is intentionally minimal: no shortcut, no caching, no backend ranking; it fetches full node and target lists only when the user submits a query (`web/src/app/layout/GlobalSearch.tsx:17`, `web/src/app/layout/GlobalSearch.tsx:25`, `web/src/app/layout/GlobalSearch.tsx:48`, `web/src/app/layout/GlobalSearch.tsx:59`, `web/src/app/layout/GlobalSearch.tsx:74`).
- The v2 visual contract already defines the intended dashboard skeleton: hero, five-column stat strip, abnormal node summary/table, abnormal target summary/table, recent events (`docs/design/v2-houfeng/component-spec.md:196`). Peer research below supports this direction, but suggests adding credible fleet inventory and control-plane freshness so home is not only an abnormal-object list.

### External References

- Cockpit official README: <https://github.com/cockpit-project/cockpit/blob/main/README.md>
  - Relevant pattern: Cockpit frames itself as an interactive server admin interface, not just charts. It makes server administration discoverable and routes sysadmins to concrete tasks: containers, storage administration, network configuration, logs, and multi-host switching over SSH.
  - Implication for Houfeng: home should route to operational surfaces and detail pages, not become a replacement for them. A small control plane should answer "what needs attention?" and "where do I go next?"
- Netdata official docs:
  - Home tab: <https://github.com/netdata/netdata/blob/master/docs/dashboards-and-charts/home-tab.md>
  - Nodes tab: <https://github.com/netdata/netdata/blob/master/docs/dashboards-and-charts/nodes-tab.md>
  - Alerts tab: <https://github.com/netdata/netdata/blob/master/docs/dashboards-and-charts/alerts-tab.md>
  - Events tab: <https://github.com/netdata/netdata/blob/master/docs/dashboards-and-charts/events-feed.md>
  - Logs tab: <https://github.com/netdata/netdata/blob/master/docs/dashboards-and-charts/logs-tab.md>
  - Node states: <https://github.com/netdata/netdata/blob/master/docs/netdata-cloud/node-states-and-transitions.md>
  - Relevant pattern: Netdata home combines live/stale/offline inventory, active warning/critical alert counts, topology/role breakdowns, "nodes with most alerts", "top alerts", metrics coverage, retention coverage, and a node status map. Nodes and Alerts tabs then provide filterable operational drill-downs and actions.
  - Implication for Houfeng: home should include a small, credible inventory health model: total nodes/targets, state breakdown, stale/offline/freshness semantics, active incident severity counts, and "worst objects / top recurring issues" before detailed trends.
- Portainer official docs:
  - Home: <https://docs.portainer.io/user/home.md>
  - Docker dashboard: <https://docs.portainer.io/user/docker/dashboard.md>
  - Relevant pattern: Portainer splits global Home from per-environment Dashboard. Home is an environment inventory with vital stats, search/filter, connected status, "Live connect" / "Browse snapshot", and edit/settings actions. The environment Dashboard then summarizes environment info, cluster info, and resource-count tiles for stacks/services/containers/images/volumes/networks.
  - Implication for Houfeng: separate fleet-level Home concerns from resource-specific pages. Home should make the selected/connected control-plane scope and data freshness obvious, then provide counts and entry points for nodes, targets, incidents/events, and settings.
- Grafana official docs:
  - Dashboards: <https://grafana.com/docs/grafana/latest/visualizations/dashboards.md>
  - Dashboard best practices: <https://grafana.com/docs/grafana/latest/visualizations/dashboards/build-dashboards/best-practices.md>
  - Alerting: <https://grafana.com/docs/grafana/latest/alerting.md>
  - Relevant pattern: Grafana defines a dashboard as panels arranged to provide an at-a-glance view of related information. Its best-practice docs emphasize story/question-driven dashboards, reduced cognitive load, meaningful color, hierarchical drill-downs, alert-directed browsing, and avoiding dashboard sprawl. Alerting is a consolidated view for detecting, managing, and acting on issues.
  - Implication for Houfeng: one home dashboard must answer a small set of operational questions rather than expose every metric. The top question is "which servers or targets are in trouble?", so showing all healthy detail on home is lower value than a well-ordered attention queue and links.
- Uptime Kuma official README: <https://github.com/louislam/uptime-kuma/blob/master/README.md>
  - Relevant pattern: Uptime Kuma focuses on uptime monitors, reactive fast UI, notification integrations, multiple status pages, ping charts, certificate info, and clear monitor/status-page screenshots.
  - Implication for Houfeng: for a small server control plane, clear monitor status and notification/event outcome are first-class. Home should expose monitor/target reachability, last success/failure, certificate/TCP/HTTP failure summaries where applicable, and links to public/status-style reporting only if the product supports it.
- Proxmox VE official GUI docs: <https://pve.proxmox.com/pve-docs/chapter-pve-gui.html>
  - Relevant pattern: Proxmox uses a resource tree for datacenter, nodes, guests, storage, and pools; Datacenter Summary gives cluster health and resource usage; node Summary gives node resource usage; recent cluster-wide tasks and cluster log give operational history.
  - Implication for Houfeng: the navigation model should preserve a clear resource hierarchy and leave detailed resource usage to node/target detail pages. Home should surface recent task/event/log signals without becoming the task log itself.

### Synthesis: What Home Must Provide

#### 1. Global health

Home must answer, above the fold:

- Is the control plane itself healthy and fresh?
- How old is the data snapshot?
- How many fleet objects exist, and how many are live/fresh, stale/offline, abnormal, severe, or in maintenance?
- What is the highest current severity?
- What changed recently?

Recommended Houfeng shape:

- A top "fleet state" strip with: center/sync status, snapshot time, total nodes, total targets, abnormal objects, severe objects, maintenance objects, recent new incidents, recent recoveries.
- Use stable state tokens and consistent severity ordering. Keep shell/nav badges neutral if they are route counts, and reserve alert/critical color for actual state rows or incident cards.
- Distinguish "offline", "stale/no recent heartbeat", "maintenance", and "never onboarded" if the backend can support it. Netdata's Live/Stale/Offline/Unseen distinction is a useful model to copy conceptually, even if Houfeng names differ.

#### 2. Inventory

Home needs a small inventory map, not only abnormal lists:

- Total nodes and targets, grouped by health/status.
- Optional grouping by Group/region/provider only when healthy groups are represented too.
- Direct links to `NodesPage` and `TargetsPage`, ideally prefiltered to abnormal/offline/maintenance objects.
- For objects shown inline, include identity, display name, group/location, freshness, current issue, and one or two trend/failure signals.

Current gap:

- `groupSummaries` is derived only from abnormal objects, so it cannot serve as a real fleet inventory distribution. Either rename it as abnormal distribution or back it with total group inventory from the dashboard API.

#### 3. Incidents / Events

Home should carry an attention queue and recent-history feed:

- Current active incidents sorted by severity and freshness.
- Top recurring issues over a short window, not just raw event chronology.
- Recent transitions: new abnormal, recovered, maintenance entered/exited, node connected/disconnected, target probe changed state.
- Each incident/event row should route to object detail or filtered Events page.
- Historical and filter-heavy investigation belongs to EventsPage, not home.

Recommended hierarchy:

1. Current severe/critical issues and failed/freshness-sensitive objects.
2. Recently changed state.
3. Recurring/top issue summaries.
4. Full event feed link.

#### 4. Quick navigation and actions

Home and shell should reduce time-to-action:

- Global search should remain an omnipresent route-to-object tool; a future shortcut/cached index/backend ranking can come later.
- Home should expose obvious first actions: create node, open onboarding, create target, view all abnormal nodes, view all abnormal targets, open events, open settings/notifications.
- Table rows should navigate to detail pages; inline actions should stop event propagation, as current rows already do.
- If shell status/counts are displayed, they must be data-backed or clearly static/build metadata. Hard-coded "ok" and `0` counts are dangerous because they visually certify a state that may be false.

#### 5. Onboarding and empty states

Home must distinguish:

- Fresh install: no nodes and no targets. This is not an incident. Show a short setup path and primary CTA.
- Partial setup: nodes exist but no targets; show next target/probe CTA.
- Waiting for agent: node created but no heartbeat yet; show onboarding/status guidance.
- Healthy empty attention queue: "no active abnormalities" is good, but still show inventory and freshness.
- Data unavailable: API/load error or stale snapshot; this is operationally different from "healthy".

Current implementation already has the fresh-install pattern; extend it to partial/healthy-empty/data-unavailable cases rather than collapsing all emptiness into the same panel.

#### 6. What to Avoid

- Do not turn Home into a full Grafana-like dashboard builder or metric wall. For this product, home is triage and navigation.
- Do not duplicate node/target detail pages on Home. Use compact rows and links.
- Do not show only abnormal inventory if the section label implies all inventory.
- Do not use alarm color in navigation badges if the badge is only a count or route affordance; the current neutral-badge rule is good.
- Do not hide data freshness. A green dashboard with stale data is misleading.
- Do not make every chart auto-refresh aggressively. Current project has no polling model; if refresh is added later, tie cadence to data semantics and backend cost.
- Do not create dashboard sprawl: one canonical home, then structured detail pages and filtered Events/Nodes/Targets routes.
- Do not overfit to large observability suites. Netdata/Grafana patterns must be scaled down to Houfeng's server-control-plane scope.
- Do not rely on screenshots alone. Use documented semantics: state definitions, alert/event lifecycle, inventory grouping, and action paths.

### Recommended Houfeng Home Contract

The home/dashboard redesign should keep the current risk-first foundation and tighten the contract around five questions:

1. **Can I trust this view?** Show center/sync status, snapshot timestamp, and data freshness.
2. **What is the current fleet state?** Show total inventory and health/severity breakdown for nodes and targets.
3. **What needs attention now?** Show severe/alert objects and active incidents sorted by severity/freshness.
4. **What changed recently?** Show recent state transitions and recovery/new-incident trend.
5. **Where do I go next?** Provide direct links/actions to create/connect nodes, add targets, inspect abnormal objects, open Events, and open Settings/notifications.

Suggested section order:

- Header/hero: product context and one sentence explaining the triage model.
- Fleet state strip: global health, snapshot time, totals, severe/abnormal/maintenance/recent changes.
- Attention queue: severe/alert nodes and targets, unified or split, with links.
- Inventory health: all nodes/targets by health/status and group; avoid abnormal-only group summaries unless explicitly labeled.
- Recent events: concise timeline with link to full EventsPage.
- Onboarding/next action panel: conditional for fresh/partial setup; otherwise compact quick actions.

## Caveats / Not Found

- No product code was edited. This file is the only intended write for this research task.
- `python3 ./.trellis/scripts/task.py current --source` returned no active task pointer; the user explicitly provided `.trellis/tasks/05-06-dashboard-and-system-navigation-redesign/research/peer-dashboard-patterns.md`, so research was written there.
- Official docs were preferred. Some vendor sites render heavily through JS/GitBook/Docusaurus; where raw markdown existed, the raw official markdown URL was used for precision.
- Cockpit official documentation is more product/README-oriented than dashboard-page-specific in the accessible sources; conclusions for Cockpit are about discoverable admin task routing rather than a literal home dashboard layout.
- Grafana is a broad dashboard/observability platform, not a small server control plane; its most useful lessons here are dashboard strategy and anti-sprawl guidance, not its full product IA.
- Uptime Kuma official README is screenshot/feature-list heavy; it supports monitor/status-page/notification patterns but does not provide a deep IA spec.
- Peer products have different domains and data models. The safe transfer is the pattern set: credible health summary, inventory, alert/event triage, fast drill-down, and honest empty/freshness states.
