# Research: Design patterns for page information architecture

- **Query**: Research applicable project design guidance and recent refactor patterns for optimizing remaining page information architecture; include reusable principles, anti-patterns, page templates, and decision rules for summary cards, action menus, drawers, tables, and command surfaces.
- **Scope**: internal
- **Date**: 2026-05-19

## Findings

### Files Found

| File Path | Description |
|---|---|
| `docs/design/v2-houfeng/design-language.md` | Current visual language: dark-first, high-density, restrained engineering-tool feel, page hierarchy, loading/error/empty rules, and no-new-dependency constraints. |
| `docs/design/v2-houfeng/component-spec.md` | Current component and page-template contract for Dashboard, Asset Decisions, VPS, VPS Detail, Nodes, Node Detail, Events, Settings, Targets, Target Detail, Onboarding, and Login. |
| `.trellis/spec/web/component-conventions.md` | Web component boundaries, slots, PageState, DataTable row-click contract, Drawer focus/reset contract, selector guidance, and anti-patterns. |
| `.trellis/spec/web/styling-guidelines.md` | Token/BEM/dark-first styling authority, density rules, allowed style locations, and visual anti-patterns. |
| `.trellis/spec/web/state-and-data.md` | Page data contracts for Dashboard, Asset Ledger, Events filters, VPS detail, Drawer data flow, and loading/error/empty state flow. |
| `.trellis/spec/web/directory-structure.md` | Route/page/component/API placement rules and frontend layering. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification and browser-sanity expectations for user-visible UI changes. |
| `.trellis/spec/guides/code-reuse-thinking-guide.md` | Search-before-new-code guidance for avoiding duplicate patterns/components. |
| `.trellis/spec/guides/cross-layer-thinking-guide.md` | Data-flow and boundary mapping guidance for UI changes that depend on backend facts. |
| `.trellis/tasks/05-19-optimize-remaining-page-information-architecture/prd.md` | Active task scope: optimize remaining page IA by defining page jobs, reducing flat sections, and collecting secondary operations into menus/drawers/detail entries. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md` | Recent Node Detail IA refactor: keep 8-chart watchtower, add capacity context, remove low-value lower sections, and collect actions in top-right menu. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/prd.md` | Recent VPS Detail IA refactor: asset-operations judgment, summary cards plus detail drawers, primary CTA plus actions menu, lifecycle confirmation retained. |
| `.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/research/browser-sanity.md` | Browser-sanity evidence for compact VPS Detail default IA, actions menu, and Drawer-backed detail/form entries. |
| `.trellis/tasks/archive/2026-05/05-15-ux11-ui-design-expert-refinement/research/expert-ui-audit.md` | Prior expert audit candidate list for remaining UX refinement areas. |
| `.trellis/tasks/archive/2026-05/05-17-ui/research/drawer-form-surfaces.md` | Inventory of existing Drawer/form surfaces, including child content, state-reset patterns, and theme/layout caveats. |
| `web/src/components/PageState.tsx` | Shared loading/error/empty primitive with v2 mark, technical error summary truncation, and action slot. |
| `web/src/components/atoms/DataTable.tsx` | Shared high-density table atom; row click behavior is governed by spec and tests. |
| `web/src/components/atoms/Drawer.tsx` | Shared portal Drawer atom used for filters, create/edit forms, history, command, and VPS detail entries. |
| `web/src/pages/DashboardPage.tsx` and `web/src/pages/dashboard/DashboardCommandSurface.tsx` | Dashboard page as command-surface plus workbench model. |
| `web/src/pages/NodesPage.tsx`, `web/src/pages/nodes/*` | Observability list pattern: hero/support surface, create Drawer, toolbar/filter/list sections, DataTable columns/actions. |
| `web/src/pages/TargetsPage.tsx`, `web/src/pages/targets/*` | Target list pattern: hero, create Drawer, support surface, filters, batch controls, DataTable. |
| `web/src/pages/VPSPage.tsx` | Asset inventory pattern: quick views, focus strip, FilterBar/chips, advanced filter Drawer, DataTable, create Drawer. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset decision queue pattern: unified queue, focus metrics, Drawer work panel, secondary renewal evidence. |
| `web/src/pages/EventsPage.tsx`, `web/src/pages/events/*` | Events pattern: diagnostic support surface, filter overview, applied/draft filter Drawer, event stream. |
| `web/src/pages/NodeComparePage.tsx` | Compare page uses PageState for missing/loading/error identity slots and watchtower metrics side-by-side. |
| `web/src/components/node-detail/NodeWatchtowerHeader.tsx` | Recent Node Detail action consolidation in a top-right `watchtower-actions-menu`. |
| `web/src/pages/node-detail/NodeDetailPageBody.tsx` | Recent Node Detail default order: header, confirmations, active problem, binding conflict, time window, metrics, linked VPS, retire confirmation, containers, snapshot meta, history/command drawers. |
| `web/src/pages/vps-detail/VPSDetailHero.tsx` | Recent VPS Detail primary CTA plus actions menu. |
| `web/src/pages/vps-detail/VPSDecisionWorkbench.tsx` | Recent VPS Detail decision workbench: next action, evidence state, decision/cost/Node/context cards, compact metrics. |
| `web/src/pages/vps-detail/VPSOperationsSummary.tsx` | Recent VPS Detail summary-card layer with detail-entry buttons for Node, services, domains, timeline, and facts. |
| `web/src/pages/VPSDetailPage.tsx` | Recent VPS Detail orchestration: shortened default render plus multiplexed Drawer detail/form modes. |
| `web/src/components/target-detail/TargetWatchtowerHeader.tsx`, `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Comparable target detail watchtower pattern, with history and runtime actions in the header and lower probe/metadata/lifecycle sections. |
| `web/src/pages/ProvidersPage.tsx`, `web/src/pages/SubscriptionsPage.tsx` | Remaining Asset Ledger pages with DataTable and inline create/edit panels, useful as contrast against Drawer-first form guidance. |

### Code Patterns

#### 1. Page IA starts from the page job, not from API fields

- Active task requirement: each page in scope must first define “页面要帮用户做的判断/操作” before deciding default content (`.trellis/tasks/05-19-optimize-remaining-page-information-architecture/prd.md:26-31`).
- `component-spec.md` encodes this per page:
  - Dashboard answers “今天先处理什么” via a command surface, not a KPI hero (`docs/design/v2-houfeng/component-spec.md:203-219`).
  - Asset Decisions is the main Asset Ledger work queue, not three VPS status tables (`docs/design/v2-houfeng/component-spec.md:221-227`).
  - VPS Detail is a single-VPS asset judgment workbench, not a collection of forms (`docs/design/v2-houfeng/component-spec.md:235-241`).
  - Nodes and Targets are positioned as observation/evidence support surfaces, not independent generic resource centers (`docs/design/v2-houfeng/component-spec.md:243-256`, `docs/design/v2-houfeng/component-spec.md:301-309`).
  - Events is an audit/diagnostic timeline with filter context and EventList as the stable fact stream (`docs/design/v2-houfeng/component-spec.md:282-290`).
- `state-and-data.md` explicitly says Dashboard contract fields are a fact pool, not a display checklist: “不得默认展示所有 contract 字段” and must not render API facts, KPI strips, group lists, recent event summaries, or asset detail dumps just because the backend returns them (`.trellis/spec/web/state-and-data.md:102-145`).

#### 2. Five-level page hierarchy and high-density rhythm

- Visual north star is “冷静、克制、高密度、工程师长期使用友好” (`docs/design/v2-houfeng/design-language.md:22-39`).
- Default page hierarchy is: page identity, current problem, trend/context, history/events, danger zone (`docs/design/v2-houfeng/design-language.md:162-170`).
- Density rules: section gap is `--space-5`, card padding is `--space-3` to `--space-4`, compact table rows are 36px, and KPI should be a composite stat strip instead of “一个数字一张大卡片” (`docs/design/v2-houfeng/design-language.md:147-160`).
- Styling spec reinforces Chinese-first, compact engineering-tool text density, mono wrappers for numbers/IDs/timestamps, and design-token spacing instead of ad hoc pixels (`.trellis/spec/web/styling-guidelines.md:129-135`).

#### 3. Command surface pattern for a page-level decision cockpit

- Dashboard specification fixes the command surface lanes: `资产决策队列`, `观测异常队列`, and `下一步动作`; downstream workbench changes title/content by fleet state (`docs/design/v2-houfeng/component-spec.md:203-219`).
- Implementation follows this: `DashboardPage` renders only `<DashboardCommandSurface />` then `<DashboardWorkbench />` after loading/error handling (`web/src/pages/DashboardPage.tsx:96-120`).
- `DashboardCommandSurface` computes a command title from asset pressure, severe/abnormal counts, maintenance, and fresh-install state (`web/src/pages/dashboard/DashboardCommandSurface.tsx:79-123`) and builds focus items for asset pressure, observability focus, and next action (`web/src/pages/dashboard/DashboardCommandSurface.tsx:181-209`).
- Existing data contract guardrails: `snapshot_generated_at` can only be `生成时间` / `摘要生成`, not sync/health freshness (`.trellis/spec/web/state-and-data.md:106-107`), and notification status can only expose booleans, not secrets (`.trellis/spec/web/state-and-data.md:111`).

#### 4. Summary-card pattern for “evidence, not full detail”

- VPS Detail PRD chose summary cards plus detail/drawer entries: main page keeps Node, services, domains, recent history, and facts summaries; full tables and low-value facts move behind explicit entries (`.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/prd.md:31-39`, `.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/prd.md:65-80`).
- `VPSDecisionWorkbench` puts the first decision cue in `下一步动作`, then evidence status items, then four high-value cards: current decision, subscription/cost, Node evidence, and context/quality (`web/src/pages/vps-detail/VPSDecisionWorkbench.tsx:86-132`, `web/src/pages/vps-detail/VPSDecisionWorkbench.tsx:134-243`).
- `VPSOperationsSummary` states the contract in its description: “主页面只保留能支撑续费/保留判断的证据摘要；全量 Node、服务、域名、历史和低价值资料进入详情入口” (`web/src/pages/vps-detail/VPSOperationsSummary.tsx:102-112`). It then renders summary cards for renewal window, Node evidence, services/domains, recent history, and a facts summary with detail-entry buttons (`web/src/pages/vps-detail/VPSOperationsSummary.tsx:131-228`).
- Browser sanity for that refactor confirmed the old flat sections were not expanded by default and the default view presented `资产判断`, `下一步动作`, and `判断证据摘要` (`.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/research/browser-sanity.md:27-54`).

#### 5. Action menu pattern for secondary/routine operations

- Node Detail refactor decision: node operations moved to top-right `watchtower-actions-menu`; lower property/action sections were removed except necessary data sections (`.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md:14-17`, `.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md:39-52`).
- `NodeWatchtowerHeader` renders `查看历史` as a visible high-frequency action and moves runtime actions, `打开接入工作台`, `执行命令…`, and retire/restore into `<details className="watchtower-actions-menu">` (`web/src/components/node-detail/NodeWatchtowerHeader.tsx:51-72`, `web/src/components/node-detail/NodeWatchtowerHeader.tsx:73-115`).
- `VPSDetailHero` keeps primary `处理决策` visible, moves facts edit, experience, Node link, service/domain create, and archive/restore into the same menu pattern, and keeps back/list navigation separate (`web/src/pages/vps-detail/VPSDetailHero.tsx:52-80`).
- Target detail already uses the watchtower header pattern for `查看历史` plus runtime actions (`web/src/components/target-detail/TargetWatchtowerHeader.tsx:58-96`), while archive/restore and metadata/probe management still appear lower in the current target detail body (`web/src/pages/target-detail/TargetDetailPageBody.tsx:214-274`).

#### 6. Drawer pattern for forms, advanced filters, and deferred details

- Component spec defines Drawer as fixed-position portal panel with ESC/overlay close, focus containment, header/body classes, and desktop width contract (`docs/design/v2-houfeng/component-spec.md:105-109`).
- Component conventions require modal/drawer focus behavior to reuse `web/src/lib/useModalFocus.ts`, portal to `document.body`, use dialog semantics, trap Tab, Escape-close, and restore focus (`.trellis/spec/web/component-conventions.md:48`).
- Drawer close/cancel/Escape/overlay must discard draft, form errors, and save feedback, and reopen from saved/current data or an empty form (`.trellis/spec/web/component-conventions.md:49`).
- “列表主扫描路径上的创建/编辑表单优先放 Drawer” so the primary list/table/queue remains visible (`.trellis/spec/web/component-conventions.md:57`).
- Existing usage inventory shows Drawer is already the owner surface for Node create, Target create, VPS create/filter, Events advanced filter, Asset decision work panel, VPS Detail operations/details, Node/Target history, and Node command execution (`.trellis/tasks/archive/2026-05/05-17-ui/research/drawer-form-surfaces.md:57-82`).
- `VPSDetailPage` uses one `activeDrawer` union and maps drawer modes to operation/form/detail titles (`web/src/pages/VPSDetailPage.tsx:714-727`), then renders forms and full detail sections only when a drawer mode is active (`web/src/pages/VPSDetailPage.tsx:729-877`). The default render is hero → decision workbench → feedback → operations summary → conditional lifecycle confirmation → Drawer (`web/src/pages/VPSDetailPage.tsx:879-975`).
- Events filters use applied/draft separation: URL/applied filters are the request truth, Drawer draft initializes from applied filters, and close/Escape/overlay discard draft (`.trellis/spec/web/state-and-data.md:467-570`; implementation entry at `web/src/pages/EventsPage.tsx:402-410`).

#### 7. DataTable pattern for high-density inventories and scan paths

- DataTable is specified as semantic `<table>` with compact 36px rows, head styling, row hover, optional row click, and no built-in sort/pagination/virtualization (`docs/design/v2-houfeng/component-spec.md:87-93`).
- Component convention: DataTable `onRowClick` must ignore clicks/Enter/Space from interactive descendants (`a[href]`, `button`, inputs, roles) so action cells do not trigger row navigation (`.trellis/spec/web/component-conventions.md:45`).
- Nodes spec uses compact DataTable with identity/freshness merged into one column and hover action column (`docs/design/v2-houfeng/component-spec.md:243-256`). Implementation builds columns and rows before `NodesListSection` (`web/src/pages/NodesPage.tsx:625-650`, `web/src/pages/NodesPage.tsx:722-740`).
- Targets spec uses compact DataTable for service-entry scanning and row hover actions (`docs/design/v2-houfeng/component-spec.md:301-309`); implementation routes row navigation only when not editing or confirming (`web/src/pages/TargetsPage.tsx:563-567`, `web/src/pages/TargetsPage.tsx:642-679`).
- VPS inventory uses quick views, chips, advanced filter Drawer, and `DataTable` as the asset verification main surface (`web/src/pages/VPSPage.tsx:700-747`, `web/src/pages/VPSPage.tsx:749-785`).
- Target Detail uses DataTable inside each ProbeItem card for recent observations because Probe observations are tabular evidence under a Probe card, not the page’s top-level inventory (`web/src/components/target-detail/TargetProbeList.tsx:230-239`).

#### 8. Custom queue/list pattern for ranked decisions, not generic inventory

- Dashboard attention queue is explicitly not a list-page table copy: each item includes priority rank, glyph, display name, context, freshness, status, current problem, and a handling action; row click goes to detail, visible action stops propagation (`docs/design/v2-houfeng/component-spec.md:214-216`).
- Asset Decisions uses a unified ordered queue as the main surface and a secondary renewal evidence table (`docs/design/v2-houfeng/component-spec.md:221-227`). Implementation renders focus metrics and an ordered `<ol className="asset-decision-queue">` before the secondary renewal evidence table (`web/src/pages/AssetDecisionsPage.tsx:552-632`, `web/src/pages/AssetDecisionsPage.tsx:634-660`).
- Component convention warns custom clickable queues should not create nested interactive semantics: if visible Links/Buttons already exist inside a row/card, pointer row-click may exist, but keyboard entry should be on visible actions, not an outer `role="link"` wrapping inner controls (`.trellis/spec/web/component-conventions.md:46`).

#### 9. PageState pattern for loading/error/empty states

- Design language requires no spinner/skeleton; loading is a surface plus mono copy/timestamp, error is in-place with Chinese description + mono summary + retry if possible, and empty state uses `.empty-state` plus explanation/CTA (`docs/design/v2-houfeng/design-language.md:232-262`).
- Component convention says route/detail/list loading/error/empty should prefer `PageState`; error details go to `technicalSummary`, and empty surfaces can use `surface="empty"` (`.trellis/spec/web/component-conventions.md:44`).
- `PageState` implements kind-specific roles/live regions, v2 SVG mark, technical summary truncation, timestamp for loading, and action slot (`web/src/components/PageState.tsx:43-90`).
- Dashboard uses `PageState` for loading/error (`web/src/pages/DashboardPage.tsx:59-73`), Targets uses it for no targets and no filter matches (`web/src/pages/TargetsPage.tsx:629-641`, `web/src/pages/TargetsPage.tsx:666-678`), NodeCompare uses it for missing IDs and per-side loading/error (`web/src/pages/NodeComparePage.tsx:74-83`, `web/src/pages/NodeComparePage.tsx:129-154`), and TargetProbeList uses it for empty ProbeItem state (`web/src/components/target-detail/TargetProbeList.tsx:124-139`).

#### 10. Cross-layer fact discipline for IA content

- `cross-layer-thinking-guide.md` says to map source → transform → store → retrieve → transform → display, identify boundaries, and define exact formats/errors before implementation (`.trellis/spec/guides/cross-layer-thinking-guide.md:18-49`).
- Node Detail capacity context refactor followed that rule: `mem_total_bytes` and `disk_total_bytes` must come from real agent sampling through agent payload, center ingest, PostgreSQL, runtime facts, frontend type, and metrics UI; the PRD explicitly says not to infer from percentages (`.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md:9-13`, `.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md:39-44`).
- Asset Ledger list/detail contracts forbid inventing linked-node health on VPS lists: `VPSAssetRecord.active_node_link_count` is count only; health/heartbeat evidence is allowed on VPS Detail only because `VPSAssetDetail.node_links` returns those fields (`.trellis/spec/web/state-and-data.md:164-171`, `.trellis/spec/web/state-and-data.md:323-345`).
- Dashboard must not infer group/provider distribution from abnormal queues, treat backend failure as a real missing subscription, or expose notification secrets (`.trellis/spec/web/state-and-data.md:108-113`, `.trellis/spec/web/state-and-data.md:338-345`).

### Reusable Principles

1. **One page, one primary job.** The default view should answer the page’s current decision/operation question, not enumerate all available fields. This is stated in the active task PRD and encoded in page templates (`.trellis/tasks/05-19-optimize-remaining-page-information-architecture/prd.md:26-31`; `docs/design/v2-houfeng/component-spec.md:200-337`).
2. **Show facts by decision value.** High-value facts belong in the default scan path; low-value facts move into drawers/detail entries, collapsed sections, or supporting pages. VPS Detail is the current concrete example (`web/src/pages/vps-detail/VPSOperationsSummary.tsx:102-112`).
3. **Keep one high-frequency action visible; collect the rest.** Recent detail refactors keep a primary CTA or `查看历史` visible and collect secondary operations into `watchtower-actions-menu` (`web/src/components/node-detail/NodeWatchtowerHeader.tsx:67-115`; `web/src/pages/vps-detail/VPSDetailHero.tsx:52-80`).
4. **Use progressive disclosure without hiding capability.** Full Node/service/domain/timeline/facts content still exists behind explicit Drawer/detail modes in VPS Detail (`web/src/pages/VPSDetailPage.tsx:714-877`).
5. **Use tables for scanning many peers; use queues for ranked work.** DataTable serves inventories/lists; custom ordered queues serve prioritized work where rank/current problem/action are part of the row semantics (`docs/design/v2-houfeng/component-spec.md:221-227`, `docs/design/v2-houfeng/component-spec.md:243-256`).
6. **Use Drawer for workflow interruption, not for every piece of information.** Create/edit/filter forms and deferred full-detail surfaces are Drawer-friendly because they preserve the main list/workbench behind them (`.trellis/spec/web/component-conventions.md:57`; `.trellis/tasks/archive/2026-05/05-17-ui/research/drawer-form-surfaces.md:57-82`).
7. **Use PageState for all major missing/loading/error states.** It centralizes v2 loading/error/empty semantics and prevents ad hoc blank panels (`.trellis/spec/web/component-conventions.md:44`; `web/src/components/PageState.tsx:43-90`).
8. **Do not promote data that is not contract-backed.** If a page needs a new signal, the fact must exist in the contract or be explicitly absent/unknown; Node capacity and VPS linked-node health are the relevant recent examples (`.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md:9-13`; `.trellis/spec/web/state-and-data.md:164-171`).
9. **Visual density comes from hierarchy, not from piling up panels.** The v2 visual spec discourages ordinary SaaS card sprawl and “一个数字一张大卡片” KPI walls (`docs/design/v2-houfeng/design-language.md:33-39`, `docs/design/v2-houfeng/design-language.md:158-160`).
10. **Reuse before inventing.** The code-reuse guide requires searching existing patterns/components and abstracting only when repetition is real, not speculative (`.trellis/spec/guides/code-reuse-thinking-guide.md:18-38`, `.trellis/spec/guides/code-reuse-thinking-guide.md:62-74`).

### Decision Rules: Which Surface to Use

| Surface | Existing guidance / use when | Current examples | Avoid when |
|---|---|---|---|
| **Command surface** | Page must answer a top-level operational question and synthesize multiple lanes into “what first?” | Dashboard command surface (`docs/design/v2-houfeng/component-spec.md:203-219`; `web/src/pages/dashboard/DashboardCommandSurface.tsx:79-123`) | Page is a simple inventory/filter table or a detailed object page with one object identity. |
| **Summary cards / focus metrics** | A handful of decision-relevant evidence categories should be visible without exposing full tables. Cards should carry state, evidence, and detail entry, not random metrics. | VPS Detail decision/workbench cards (`web/src/pages/vps-detail/VPSDecisionWorkbench.tsx:134-243`); VPS operations cards (`web/src/pages/vps-detail/VPSOperationsSummary.tsx:131-228`); Asset Decisions focus strip (`web/src/pages/AssetDecisionsPage.tsx:552-598`) | The content is a row collection users must compare/sort/scan; use DataTable or queue instead. |
| **Action menu** | Secondary operations are numerous, routine, or conditionally available; one primary CTA remains visible. Dangerous actions still require confirmation. | Node header menu (`web/src/components/node-detail/NodeWatchtowerHeader.tsx:71-115`); VPS detail menu (`web/src/pages/vps-detail/VPSDetailHero.tsx:52-80`) | Action is the page’s main job or a row-level action that must be visible for repeated scanning. |
| **Drawer: create/edit form** | A form would displace the main scan path; user should edit/create while retaining list/workbench context. Close must discard draft/error/feedback. | Node/Target/VPS create drawers, AssetDecision drawer, VPS Detail operation forms (`.trellis/tasks/archive/2026-05/05-17-ui/research/drawer-form-surfaces.md:57-82`) | The form is the entire page’s purpose or requires multi-page navigation/history that Drawer cannot represent. |
| **Drawer: advanced filter** | Filters are too detailed for the main scan path; URL/applied state must remain distinct from draft. | Events filter drawer (`web/src/pages/EventsPage.tsx:402-410`); VPS advanced filter drawer (`web/src/pages/VPSPage.tsx:883-926`) | Lightweight single-toggle/chip filters are enough and do not crowd the page. |
| **Drawer: deferred full detail** | Default page should show summary evidence, but full section content remains available one click deeper. | VPS Detail Node/services/domains/timeline/facts detail modes (`web/src/pages/VPSDetailPage.tsx:831-874`) | The content is part of the primary default job and must be visible without opening a dialog. |
| **DataTable** | Users need dense peer comparison, scanning, compact columns, row navigation, and action cells. | Nodes, Targets, VPS inventory, Providers, Subscriptions (`docs/design/v2-houfeng/component-spec.md:87-93`; `web/src/pages/VPSPage.tsx:749-785`) | Rows need explicit ranking/story/current-problem work-queue semantics; use ordered custom queue. |
| **Custom queue/list** | Items are prioritized tasks, not equal records; row content includes rank, current issue, and a treatment action. | Asset Decisions queue (`web/src/pages/AssetDecisionsPage.tsx:552-632`); Dashboard attention queue spec (`docs/design/v2-houfeng/component-spec.md:214-216`) | Users need generic filtering/sorting across many homogeneous rows; use DataTable. |
| **DetailSection** | A default visible section has a stable role in the page hierarchy and can be scanned independently. | Settings ordered sections (`docs/design/v2-houfeng/component-spec.md:291-299`); Target Detail probe/current/event sections (`docs/design/v2-houfeng/component-spec.md:310-325`) | Section only repeats hero facts, exposes low-value fields, or exists mainly to hold actions that can live in header/menu/drawer. |
| **PageState** | Route/detail/list loading, error, or empty state needs v2-consistent explanation, technical summary, and action slot. | `PageState` component (`web/src/components/PageState.tsx:43-90`); Dashboard/Targets/NodeCompare/TargetProbeList usages | Raw `page-panel` plus bare text for major page states. |
| **ActionConfirmationCard** | User triggers a state transition/destructive/lifecycle action needing current/result/impact/unchanged explanation. | Node retire confirmation (`web/src/pages/node-detail/NodeDetailPageBody.tsx:220-237`); target probe delete confirmation (`web/src/components/target-detail/TargetProbeList.tsx:195-208`); VPS lifecycle confirmation (`web/src/pages/VPSDetailPage.tsx:953-963`) | Routine form submit or non-destructive navigation. |

### Page Templates Currently Documented or Implemented

| Template | Default structure | Evidence |
|---|---|---|
| **Dashboard / 工作台** | `PageState` loading/error → `DashboardCommandSurface` → one state-aware workbench; no global KPI wall, no full group/recent-event lists by default. | `docs/design/v2-houfeng/component-spec.md:203-219`; `.trellis/spec/web/state-and-data.md:102-145`; `web/src/pages/DashboardPage.tsx:59-120` |
| **Observability list (Nodes/Targets)** | Hero → support/evidence surface → create Drawer (not inline) → toolbar/filter/batch controls → compact DataTable → runtime overlays. | Nodes spec (`docs/design/v2-houfeng/component-spec.md:243-256`), Targets spec (`docs/design/v2-houfeng/component-spec.md:301-309`), `web/src/pages/NodesPage.tsx:661-740`, `web/src/pages/TargetsPage.tsx:570-679` |
| **Asset inventory (VPS list)** | Page header/create action → quick views/Tabs → focus signal strip → FilterBar with chips + advanced filter Drawer → `VPS 库存表` DataTable → create/filter Drawers. | `docs/design/v2-houfeng/component-spec.md:228-234`; `web/src/pages/VPSPage.tsx:700-785`, `web/src/pages/VPSPage.tsx:788-926` |
| **Asset decision queue** | Hero → unified decision queue board with summary/focus/Tabs → ordered queue → secondary renewal evidence table → Drawer work panel. | `docs/design/v2-houfeng/component-spec.md:221-227`; `web/src/pages/AssetDecisionsPage.tsx:536-680` |
| **Detail watchtower (Node)** | Watchtower header/action menu → runtime confirmation/error → current danger only if active incidents → binding conflict if needed → time-window tabs → metrics grid → necessary data sections (linked VPS, containers) → snapshot meta → history/command drawers. | Node PRD (`.trellis/tasks/archive/2026-05/05-19-optimize-node-detail-information-architecture/prd.md:3-17`); `web/src/pages/node-detail/NodeDetailPageBody.tsx:156-273` |
| **Detail judgment workbench (VPS)** | Hero with primary decision CTA + actions menu → asset decision workbench → feedback → compact operations summary cards → lifecycle confirmation only after menu action → multipurpose Drawer for forms/details. | VPS PRD (`.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/prd.md:31-39`); `web/src/pages/VPSDetailPage.tsx:879-975` |
| **Target detail / target watchtower** | Header/history/runtime menu → runtime confirmation/error → active danger if any → time-window tabs → latency trends → ProbeItem list/evidence → probe management/metadata/lifecycle lower sections → history drawer. | `docs/design/v2-houfeng/component-spec.md:310-325`; `web/src/pages/target-detail/TargetDetailPageBody.tsx:172-290` |
| **Events timeline** | Hero → diagnostic support surface → filter overview/chips → advanced filter Drawer with applied/draft separation → Event stream. | `docs/design/v2-houfeng/component-spec.md:282-290`; `web/src/pages/EventsPage.tsx:373-420`; `web/src/pages/events/EventsFilterOverview.tsx:99-118` |
| **Settings** | Hero → ordered `DetailSection`s for Theme, Telegram, frequency defaults, incident defaults, override rules, retention → bottom save/error/success. | `docs/design/v2-houfeng/component-spec.md:291-299`; settings subcomponents listed in `web/src/pages/settings/` |
| **Node onboarding** | Hero identity → Stepper phase → binding conflict if needed → one-command install section → steps/troubleshooting; command only from center, token not incidental. | `docs/design/v2-houfeng/component-spec.md:326-337`; `.trellis/spec/web/state-and-data.md:38-93` |
| **Compare** | Missing IDs use PageState; otherwise A/B identity cards and side-by-side watchtower metrics, with per-side PageState loading/error. | `web/src/pages/NodeComparePage.tsx:74-126`, `web/src/pages/NodeComparePage.tsx:129-180` |
| **Providers/Subscriptions current pattern** | Hero → inline create/edit panels → DataTable; expert audit identified them as Drawer/sidecar candidates, but current code still uses inline panels. | `web/src/pages/ProvidersPage.tsx:267-329`; `web/src/pages/SubscriptionsPage.tsx:417-479`; `.trellis/tasks/archive/2026-05/05-15-ux11-ui-design-expert-refinement/research/expert-ui-audit.md:5-38` |

### Anti-Patterns Identified in Active Guidance

| Anti-pattern | Why it matters / existing citation |
|---|---|
| Rendering every backend field because it exists | Dashboard spec explicitly treats contract fields as a fact pool, not a display checklist (`.trellis/spec/web/state-and-data.md:102-145`). |
| Single-value KPI wall / “one number per big card” | v2 density rules prefer composite stat strips and decision surfaces (`docs/design/v2-houfeng/design-language.md:158-160`). |
| Flat equal-weight sections for low-value facts, secondary actions, and danger controls | Active task and VPS/Node PRDs identify this as “字段/区块平铺、操作分散、信息很多但判断很少” (`.trellis/tasks/05-19-optimize-remaining-page-information-architecture/prd.md:3-14`; `.trellis/tasks/archive/2026-05/05-19-optimize-vps-detail-information-architecture/prd.md:5-16`). |
| Browser-synthesized production install commands or plaintext token transfer | Node onboarding contract says browser must show only center-generated command and avoid incidental token exposure (`.trellis/spec/web/state-and-data.md:38-93`). |
| Inventing health/risk facts from weak fields | Asset Ledger contract forbids linked-node health on lists from `active_node_link_count`; detail health is allowed only where detail contract returns it (`.trellis/spec/web/state-and-data.md:164-171`, `.trellis/spec/web/state-and-data.md:323-345`). |
| Treating request failure as true absence | VPS Detail subscription failure must show unknown/error, not render real `缺订阅` (`.trellis/spec/web/state-and-data.md:329-345`). |
| Inline forms crowding list scan paths | Component convention says list main-scan create/edit forms should prefer Drawer (`.trellis/spec/web/component-conventions.md:57`); Providers/Subscriptions currently show inline panels (`web/src/pages/ProvidersPage.tsx:284-329`, `web/src/pages/SubscriptionsPage.tsx:465-479`). |
| Nested interactive row semantics | Custom clickable queues must not wrap true links/buttons in an outer keyboard link role (`.trellis/spec/web/component-conventions.md:46`). |
| Raw loading/error/empty panels | `PageState` should be used for route/detail/list states (`.trellis/spec/web/component-conventions.md:44`; `docs/design/v2-houfeng/design-language.md:232-262`). |
| Direct `fetch()` in pages/components | API calls must go through `lib/api.ts` / `auth-client.ts` (`.trellis/spec/web/component-conventions.md:135-136`; `.trellis/spec/web/state-and-data.md:24-30`). |
| Requiring users to copy internal IDs for normal associations | Provider/Node/Target/Service associations should use selectors with recognizable labels and empty/error candidate states (`.trellis/spec/web/component-conventions.md:50`; `.trellis/spec/web/state-and-data.md:170-218`). |
| New one-off CSS files, CSS-in-JS, hardcoded colors/pixels, Tailwind/chart deps | Styling spec and design language restrict styling to tokens/BEM/pure CSS and no new CSS/chart frameworks (`.trellis/spec/web/styling-guidelines.md:21-35`, `.trellis/spec/web/styling-guidelines.md:138-151`; `docs/design/v2-houfeng/design-language.md:312-331`). |
| Copy-pasting similar components/logic before searching | Code reuse guide requires search-first and only abstracting when repetition is real (`.trellis/spec/guides/code-reuse-thinking-guide.md:18-38`, `.trellis/spec/guides/code-reuse-thinking-guide.md:62-74`). |

### Related Specs

- `.trellis/spec/web/component-conventions.md` — most directly relevant for PageState, DataTable, Drawer, action/menu semantics, selectors, and anti-patterns.
- `.trellis/spec/web/styling-guidelines.md` — visual authority, token/BEM constraints, density, Chinese-first UI, and style anti-patterns.
- `.trellis/spec/web/state-and-data.md` — data contract discipline, Dashboard display limits, Asset Ledger/VPS Detail flow, Events filter Drawer state machine, loading/error/empty data flow.
- `.trellis/spec/web/directory-structure.md` — page/component/API placement and route registration rules.
- `.trellis/spec/web/quality-guidelines.md` — page tests, lint/test/build, and local browser sanity expectations for UI changes.
- `.trellis/spec/guides/code-reuse-thinking-guide.md` — search/reuse criteria before adding page-specific components.
- `.trellis/spec/guides/cross-layer-thinking-guide.md` — boundary mapping for IA content that needs new backend facts.
- `docs/design/v2-houfeng/design-language.md` — visual north star, density, state, loading/error/empty, and no-dependency guardrails.
- `docs/design/v2-houfeng/component-spec.md` — concrete page templates and component contracts.

### External References

- None. The requested research was satisfied by repository docs/specs/archive tasks/source files; no external web references were used.

## Caveats / Not Found

- No single universal `PageIATemplate` component exists; page IA patterns are currently embodied in specs, page components, and recent PRDs/code.
- Node Detail and VPS Detail patterns are recent task outcomes, not yet fully generalized into `.trellis/spec/web/*` beyond the existing component/state/style guidance.
- Providers and Subscriptions still use inline create/edit panels in current code; the Drawer-first guidance exists in specs and archived audit, but those pages have not necessarily been refactored yet.
- This research did not run browser sanity for the current task. The cited browser sanity is from the archived VPS Detail refactor and used injected fixtures because no local center was running.
