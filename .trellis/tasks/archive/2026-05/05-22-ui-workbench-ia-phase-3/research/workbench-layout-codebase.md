# Research: workbench layout codebase

- **Query**: Repo-local frontend patterns for `.trellis/tasks/05-22-ui-workbench-ia-phase-3`: existing page-panel/list/table/filter patterns in Dashboard/VPS/Nodes/Targets/AssetDecisions; shared route-agnostic layout primitives; CSS locations for workbench-layout / compact-header / table-workbench / filter variants; relevant Trellis web spec constraints; constraints, reusable patterns, critical files, and implementation risks.
- **Scope**: internal
- **Date**: 2026-05-22

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/DashboardPage.tsx` | Dashboard route orchestration; renders loading/error `PageState`, then `DashboardCommandSurface` + `DashboardWorkbench` inside `page-stack dashboard-page`. |
| `web/src/pages/dashboard/DashboardCommandSurface.tsx` | Asset-decision-first dashboard command surface with lanes, focus item, secondary controls, and dashboard-specific BEM classes. |
| `web/src/pages/dashboard/DashboardWorkbench.tsx` | Dashboard secondary workbench under `DetailSection`, switching between onboarding, abnormal queue, and running overview modes. |
| `web/src/pages/VPSPage.tsx` | VPS inventory workbench: URL-state quick views and filters, command panel, filter chips, advanced filter Drawer, and `DataTable`. |
| `web/src/pages/NodesPage.tsx` | Nodes list workbench orchestration: URL filters, sort state, advanced filter Drawer, toolbar, support surface, and list section. |
| `web/src/pages/nodes/NodesToolbar.tsx` | `list-command-band` implementation for Nodes quick view, count meta, filters, batch, trend, compare, and auto-refresh actions. |
| `web/src/pages/nodes/NodesSupportSurface.tsx` | Nodes evidence/support surface using route-agnostic observability components via action/meta/glyph slots. |
| `web/src/pages/nodes/NodesListSection.tsx` | Nodes table/empty-state/batch composition; wraps compact `DataTable` in `page-panel page-panel--scroll-x nodes-table-panel`. |
| `web/src/pages/nodes/NodesFilterPanel.tsx` | Nodes advanced filter body using shared filter primitives inside `FilterBar className="list-filter-panel"`. |
| `web/src/pages/TargetsPage.tsx` | Targets list workbench: header panel, create Drawer, support surface, inline filter panel, batch panel, compact `DataTable`. |
| `web/src/pages/targets/TargetsFilterPanel.tsx` | Targets filter body using shared filter primitives inside `FilterBar className="list-filter-panel"`. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset decision work queue surface: custom queue rows, focus strip, summary, renewal evidence table, and Drawer edit flow. |
| `web/src/components/atoms/DataTable.tsx` | Shared semantic table atom with compact/normal density, optional sortable headers, row click, and interactive-descendant guard. |
| `web/src/components/atoms/Drawer.tsx` | Shared portal Drawer with overlay close, dialog semantics, and `useModalFocus` focus handling. |
| `web/src/components/atoms/Tabs.tsx` | Shared quick-view/queue-view tabs with `underline` or `pill` variants and optional count badges. |
| `web/src/components/PageState.tsx` | Shared route/list/detail loading, error, and empty-state primitive. |
| `web/src/components/filters/FilterBar.tsx` | Shared filter layout wrapper with controls row, active chips, and clear-all action. |
| `web/src/components/filters/FilterSelect.tsx` | Controlled single-select filter primitive. |
| `web/src/components/filters/FilterMultiSelect.tsx` | Controlled multi-select filter primitive with local popup state. |
| `web/src/components/filters/FilterToggle.tsx` | Controlled boolean filter primitive wrapping atom `Toggle`. |
| `web/src/components/filters/FilterChip.tsx` | Removable active filter chip primitive. |
| `web/src/components/ObservabilityEvidenceLead.tsx` | Route-agnostic observability lead component using `ReactNode` action and secondary-action slots. |
| `web/src/components/ObservabilityEvidenceFocus.tsx` | Route-agnostic observability focus card using `ReactNode` glyph/meta/action slots. |
| `web/src/components/DetailSection.tsx` | Shared section surface with optional aside and top ribbon, used by Dashboard workbench. |
| `web/src/styles/pages.css` | Page-level CSS for `page-panel`, dashboard workbench, asset decisions, VPS inventory, observability list frame, command bands, nodes/targets tables. |
| `web/src/styles/atoms.css` | Atom CSS, including `DataTable` density, clickable rows, sortable header padding, and empty table styling. |
| `web/src/components/filters/filters.css` | Filter primitive CSS, including `filter-bar`, select, multi-select, toggle, chip, and mobile breakpoint rules. |
| `web/src/main.tsx` | Central CSS import order: reset, tokens, atoms, pages, app layout, filters. |
| `.trellis/spec/web/component-conventions.md` | Web component contracts: route-agnostic slots, `PageState`, `DataTable` row clicks, Drawer focus/cleanup, and custom queue semantics. |
| `.trellis/spec/web/styling-guidelines.md` | Styling constraints: pure CSS, token/BEM, no new page-local CSS, CSS import order, DataTable sortable padding caveat. |
| `.trellis/spec/web/state-and-data.md` | Dashboard, Nodes, Asset Ledger, URL-state, Drawer applied/draft, and derived-data constraints. |
| `.trellis/spec/web/directory-structure.md` | Directory and layering constraints: pages as orchestration, components pure/controlled, API via `lib/api.ts`, styles centralized. |
| `.trellis/spec/web/quality-guidelines.md` | Test/build expectations for pages, atoms, route chunking, lint/test/build. |
| `docs/design/v2-houfeng/design-language.md` | Visual language: dark-first, Chinese-primary, high-density engineering-tool feel, 8pt spacing, tokenized typography/colors. |
| `docs/design/v2-houfeng/component-spec.md` | Component/page IA contracts for Dashboard, Asset Decisions, VPS inventory, Nodes, Targets, Drawer, DataTable, Tabs, DetailSection. |

### Code Patterns

#### Route pages as orchestration points

- Dashboard keeps route state and loading/error handling in the page, then composes focused child surfaces: `web/src/pages/DashboardPage.tsx:60` uses `<PageState kind="loading" title="正在加载工作台…" />`; `web/src/pages/DashboardPage.tsx:97-113` renders `page-stack dashboard-page`, `DashboardCommandSurface`, and `DashboardWorkbench`.
- VPS inventory is page-owned data/filter orchestration: `web/src/pages/VPSPage.tsx:421-424` initializes `useSearchParams`, parsed filters, `draftFilters`, and `filterDrawerOpen`; `web/src/pages/VPSPage.tsx:631` builds `DataTableColumn<InventoryRow>[]`; `web/src/pages/VPSPage.tsx:711-839` lays out command panel, filter bar, table panel, and `DataTable`.
- Nodes follows the same route orchestration shape with more observability controls: `web/src/pages/NodesPage.tsx:62` uses `useSearchParams`; `web/src/pages/NodesPage.tsx:89` owns `DataTableSortState`; `web/src/pages/NodesPage.tsx:93-94` owns advanced-filter Drawer state and draft state; `web/src/pages/NodesPage.tsx:865-938` composes `observability-list-frame`, `NodesToolbar`, `NodesSupportSurface`, and `NodesListSection`.
- Targets parallels Nodes with inline filters: `web/src/pages/TargetsPage.tsx:56` uses URL-state; `web/src/pages/TargetsPage.tsx:571` uses `page-panel page-panel--inline`; `web/src/pages/TargetsPage.tsx:629-704` composes `observability-list-frame`, `list-command-band`, `TargetsFilterPanel`, `TargetsBatchPanel`, and compact `DataTable`.
- Asset Decisions is a queue workbench rather than a plain table page: `web/src/pages/AssetDecisionsPage.tsx:568-675` renders `page-panel asset-decision-board`, tabs, focus strip, `PageStateView` branches, and an ordered `asset-decision-queue`.

#### Page panels and command/list frames

- `page-panel` is the base page surface, defined at `web/src/styles/pages.css:73`; scrollable table variants use `page-panel--scroll-x` at `web/src/styles/pages.css:88`; compact inline page headers use `page-panel--inline` at `web/src/styles/pages.css:240`.
- `section-heading` is the reusable table/list panel heading pattern, defined at `web/src/styles/pages.css:1663`; VPS table heading uses it in `web/src/pages/VPSPage.tsx:795-804`.
- Dashboard command/workbench CSS begins at `web/src/styles/pages.css:326` for `dashboard-command-surface` and `web/src/styles/pages.css:928` for `dashboard-workbench`.
- Asset decision board and queue CSS are in `web/src/styles/pages.css:4375` (`asset-decision-board`), `web/src/styles/pages.css:4379` (`asset-decision-focus`), `web/src/styles/pages.css:4478` (`asset-decision-queue`), and `web/src/styles/pages.css:4487` (`asset-decision-row`).
- VPS inventory CSS is in `web/src/styles/pages.css:4642` (`vps-inventory-command`), `web/src/styles/pages.css:4742` (`vps-inventory-table-panel`), and `web/src/styles/pages.css:4771` (`vps-filter-bar`).
- Observability list shell CSS is in `web/src/styles/pages.css:5696` (`observability-list-frame`), `web/src/styles/pages.css:5733` (`list-command-band`), `web/src/styles/pages.css:5935` (`nodes-toolbar`), `web/src/styles/pages.css:6048-6049` (`nodes-table-panel`, `targets-table-panel`), `web/src/styles/pages.css:6041-6175` (`nodes-table`), and `web/src/styles/pages.css:6486-6625` (`targets-table`).

#### Quick views, URL-state filters, and Drawer applied/draft separation

- VPS quick views are represented as `Tabs variant="pill"` in the command panel: `web/src/pages/VPSPage.tsx:729-736`. URL-state filter parsing and draft filter state are initialized at `web/src/pages/VPSPage.tsx:421-424`; opening the filter Drawer copies applied filters into draft state at `web/src/pages/VPSPage.tsx:588-590`; applying writes query state and closes the Drawer at `web/src/pages/VPSPage.tsx:593-595`.
- Nodes advanced filters use the stronger applied/draft pattern: `web/src/pages/NodesPage.tsx:646-648` opens by copying applied `filterState` into `draftFilterState`; `web/src/pages/NodesPage.tsx:651-653` closes by clearing Drawer and draft state; `web/src/pages/NodesPage.tsx:657-660` applies draft state via `setSearchParams(..., { replace: true })` and then closes.
- Nodes filter Drawer renders `NodesFilterPanel` from draft state at `web/src/pages/NodesPage.tsx:817-863`.
- Targets keeps filters inline inside the list frame: `web/src/pages/TargetsPage.tsx:667-678` renders `TargetsFilterPanel`; `TargetsFilterPanel` itself uses shared filter primitives in `web/src/pages/targets/TargetsFilterPanel.tsx:30-40`.
- Shared filter wrapper contract: `FilterBarProps` is declared at `web/src/components/filters/FilterBar.tsx:3`; the component renders `filter-bar__controls`, clear-all, and `filter-bar__chips` at `web/src/components/filters/FilterBar.tsx:25-38`.
- Filter primitive CSS is centralized in `web/src/components/filters/filters.css`: `filter-bar` at line 6, `filter-select` at line 63, `filter-multiselect` at line 103, `filter-toggle` at line 178, `filter-chip` at line 194, mobile breakpoint at line 235.

#### DataTable and custom queue contracts

- `DataTable` column, sort, and prop interfaces are declared at `web/src/components/atoms/DataTable.tsx:17`, `web/src/components/atoms/DataTable.tsx:31`, and `web/src/components/atoms/DataTable.tsx:36`.
- `DataTable` has an interactive-descendant guard: selector declaration at `web/src/components/atoms/DataTable.tsx:3`; guard function at `web/src/components/atoms/DataTable.tsx:13`; row click/key handling checks the guard at `web/src/components/atoms/DataTable.tsx:144-151`; clickable rows get `tabIndex` at `web/src/components/atoms/DataTable.tsx:147`.
- `DataTable` CSS is in `web/src/styles/atoms.css`: base at line 334, compact/normal header and cell density at lines 364-417, clickable row states at lines 419-426, empty state at line 430. Sortable header padding is explicitly handled by `data-table__th--sortable` selectors at `web/src/styles/atoms.css:375-376` and `web/src/styles/atoms.css:897`.
- Nodes table pattern: `web/src/pages/nodes/NodesListSection.tsx:147-157` wraps compact `DataTable<NodeRecord>` in `page-panel page-panel--scroll-x nodes-table-panel`, with `sortState`, `onSortChange`, and `onRowClick`.
- Targets table pattern: `web/src/pages/TargetsPage.tsx:703-704` wraps compact `DataTable<TargetRecord>` in `page-panel page-panel--scroll-x targets-table-panel`; row navigation is passed via `onRowClick`.
- VPS table pattern: `web/src/pages/VPSPage.tsx:795-839` wraps `DataTable` in `page-panel page-panel--scroll-x vps-inventory-table-panel` and applies `className="asset-table vps-table vps-inventory-table"`.
- Asset Decisions intentionally uses a custom queue, not `DataTable`: `renderDecisionQueueItem` is defined at `web/src/pages/AssetDecisionsPage.tsx:241`; queue rows use `li.asset-decision-row asset-decision-row--clickable` at `web/src/pages/AssetDecisionsPage.tsx:250`; inner `Link` and `Button` stop propagation at `web/src/pages/AssetDecisionsPage.tsx:299-309`.

#### Shared route-agnostic layout/display primitives

- `PageState` supports `kind="loading|error|empty"`, `surface="panel|empty"`, `action`, and `technicalSummary`; types are at `web/src/components/PageState.tsx:5-16`; rendering picks `page-panel` vs `empty-state` at `web/src/components/PageState.tsx:60` and sets state role/aria at `web/src/components/PageState.tsx:70`.
- `Drawer` contract is at `web/src/components/atoms/Drawer.tsx:6-13`; it uses `useModalFocus` at line 28, returns `null` when closed at line 33, portals via `createPortal` at line 35, closes on overlay mouse down at line 39, and declares `role="dialog"` / `aria-modal="true"` at lines 44-45.
- `Tabs` contract is at `web/src/components/atoms/Tabs.tsx:3-13`; count badges only render when `count > 0` at `web/src/components/atoms/Tabs.tsx:37-40`.
- `DetailSection` accepts `aside` and optional `ribbon`, declared at `web/src/components/DetailSection.tsx:13-18`; it is used by Dashboard workbench at `web/src/pages/dashboard/DashboardWorkbench.tsx:55-96`.
- `ObservabilityEvidenceLead` accepts route-owned `action` and optional `secondaryAction` slots at `web/src/components/ObservabilityEvidenceLead.tsx:11-20`; Nodes passes a route-specific `Link` or `Button` action at `web/src/pages/nodes/NodesSupportSurface.tsx:71-95`.
- `ObservabilityEvidenceFocus` accepts `glyph`, `meta`, and `action` slots at `web/src/components/ObservabilityEvidenceFocus.tsx:3-9`; Nodes supplies `StatusGlyph`, `Hostname`, and `Link` at `web/src/pages/nodes/NodesSupportSurface.tsx:161-199`.

#### CSS organization and requested class names

- Global CSS import order is centralized in `web/src/main.tsx:5-10`: `reset.css`, `tokens.css`, `atoms.css`, `pages.css`, `app/layout/layout.css`, `components/filters/filters.css`.
- Exact class-name search found no existing matches for `workbench-layout`, `compact-header`, or `table-workbench` in `web/src` or `.trellis/spec/web`.
- Existing analogs for those requested concepts are:
  - workbench layout: `dashboard-workbench`, `asset-decision-board`, `vps-inventory-command`, `observability-list-frame`, `list-command-band`, `DetailSection`.
  - compact header: `page-panel page-panel--inline`, `section-heading`, `list-command-band__main` / `list-command-band__meta`.
  - table workbench: `page-panel page-panel--scroll-x ...-table-panel` + `DataTable density="compact"`, with page-specific classes such as `nodes-table`, `targets-table`, `vps-inventory-table`.
  - filter variants: `FilterBar` plus `list-filter-panel` or `vps-filter-bar`, with primitive styles in `filters.css`.

### External References

- None. This was repo-local/internal research only.

### Related Specs

- `.trellis/spec/web/component-conventions.md` — route-agnostic slot pattern (`ReactNode` action/meta/glyph), `PageState` for route/detail/list states, `DataTable` clickable-row contract, custom queue no nested interactive semantics, Drawer focus/cleanup, create/edit forms in Drawer (`lines 43-49`, `57`, `146`, `164`, `168`).
- `.trellis/spec/web/styling-guidelines.md` — no Tailwind/CSS-in-JS/preprocessors; fixed global CSS import order; token/BEM-only styling; no new page-local CSS except `LoginPage.css`; DataTable sortable header padding caveat (`lines 9-15`, `40-45`, `81-89`, `110`, `124-151`).
- `.trellis/spec/web/state-and-data.md` — Dashboard must stay asset-decision-first and not expand all contract fields; Nodes quick view and advanced filter applied/draft separation; Asset Ledger frontend joins, URL-state, decision queue, subscription-failure caveats, and VPS list `active_node_link_count` limitation (`lines 102-113`, `147-165`, `169-204`).
- `.trellis/spec/web/directory-structure.md` — pages are route orchestration, components are pure/controlled, API calls go through `lib/api.ts`, styles are centralized, new route/pages/tests and atom/barrel/CSS expectations (`lines 107-127`, `139-141`, `169-176`, `184-192`).
- `.trellis/spec/web/quality-guidelines.md` — colocated tests for pages/atoms, route pages remain lazy-loaded, run lint/test/build for changes (`lines 74-83`, `210-232`).
- `docs/design/v2-houfeng/design-language.md` — dark-first, Chinese-primary, high-density engineering-tool feel, 8pt spacing, tokenized typography, DataTable/Tabs/Badge/Button roles (`lines 10`, `133-150`, `273-287`).
- `docs/design/v2-houfeng/component-spec.md` — Dashboard command surface and single workbench hierarchy; Asset Decisions unified queue/Drawer; VPS quick views/filter Drawer; Nodes and Targets compact DataTable contracts; Node onboarding install command must come from center (`lines 202-215`, `223-231`, `246-249`, `306`, `328-330`).

## Caveats / Not Found

- No existing `workbench-layout`, `compact-header`, or `table-workbench` classes were found. Implementation should reference existing analogs above rather than assuming those class names already exist.
- `web/src/styles/pages.css` is large and already contains page-specific BEM blocks. New page/workbench layout styling should go into this existing file unless it is atom-level styling for `atoms.css`; do not add page-local CSS files.
- `web/src/app/layout/layout.css` is AppShell-only. Page workbench/list/table CSS should not be added there.
- `AssetDecisionRenewalTable` uses `DataTable` but is not fully route-agnostic because it imports Asset Ledger badges/helpers; treat it as a domain composite, not a generic table primitive.
- The current research did not inspect every test file in detail; spec constraints indicate changed pages/atoms require colocated tests and lint/test/build validation.

## Constraints, Reusable Patterns, Critical Files, and Implementation Risks

### Constraints

- Keep pages as route-level orchestration; do not make shared primitives import route pages or `react-router-dom` unless they are page/domain components that already do so.
- Use existing atoms and composites: `PageState`, `DataTable`, `Drawer`, `Tabs`, `Button`, `Badge`, `MonoDigits`, `Hostname`, `Timestamp`, `DetailSection`, `ObservabilityEvidenceLead`, `ObservabilityEvidenceFocus`, and filter primitives.
- Use pure CSS with tokens and BEM. Do not introduce Tailwind, CSS-in-JS, Sass/Less, styled-components, utility-class systems, hardcoded colors, or hardcoded layout spacing.
- Add page/workbench styles to `web/src/styles/pages.css`; add atom styles to `web/src/styles/atoms.css`; add filter primitive styles to `web/src/components/filters/filters.css`; preserve import order in `web/src/main.tsx`.
- For advanced filters, keep applied URL/list state separate from Drawer draft state; close/Esc/overlay/cancel must discard draft.
- For list scanning paths, keep create/edit/advanced controls in Drawers when they would crowd the main table/queue.
- For `DataTable` rows, rely on the atom's interactive-descendant guard; for custom queues, explicitly stop propagation on inner `Link`/`Button` and avoid adding nested interactive semantics to the container.
- Keep Dashboard as asset-decision-first command surface plus a single secondary workbench; do not restore KPI-grid-first or many same-weight sections.
- Keep Asset Decisions as a unified work queue; do not restore multiple same-weight VPS queue tables.
- For VPS inventory, subscription load failures must not be converted into true missing-subscription evidence; `active_node_link_count` only supports count/unlinked state, not linked-node health.

### Reusable Patterns

- Header/summary surface: `page-panel page-panel--inline` and `section-heading`.
- Main list shell: `observability-list-frame` + `list-command-band` + optional support surface + table panel.
- Table workbench: `page-panel page-panel--scroll-x <domain>-table-panel` + `DataTable density="compact"` + page-specific BEM class.
- Queue workbench: `page-panel <domain>-board` + tabs/focus strip + custom queue rows + `PageStateView surface="empty" compact` for loading/error/empty branches.
- Filter overview: `FilterBar` with active `FilterChip`s and `onClearAll`, specialized by page-level class such as `vps-filter-bar` or `list-filter-panel`.
- Advanced filters: page-owned applied state + page-owned draft state + `Drawer` + shared filter primitives.
- Route-agnostic support cards: shared component takes `ReactNode` slots for action/glyph/meta; page passes route-specific `Link`, `Button`, `StatusGlyph`, and formatted identifiers.

### Critical Files

- `web/src/pages/DashboardPage.tsx`
- `web/src/pages/dashboard/DashboardCommandSurface.tsx`
- `web/src/pages/dashboard/DashboardWorkbench.tsx`
- `web/src/pages/VPSPage.tsx`
- `web/src/pages/NodesPage.tsx`
- `web/src/pages/nodes/NodesToolbar.tsx`
- `web/src/pages/nodes/NodesListSection.tsx`
- `web/src/pages/TargetsPage.tsx`
- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/components/atoms/DataTable.tsx`
- `web/src/components/atoms/Drawer.tsx`
- `web/src/components/atoms/Tabs.tsx`
- `web/src/components/PageState.tsx`
- `web/src/components/filters/*`
- `web/src/styles/pages.css`
- `web/src/styles/atoms.css`
- `web/src/components/filters/filters.css`
- `web/src/main.tsx`
- `.trellis/spec/web/component-conventions.md`
- `.trellis/spec/web/styling-guidelines.md`
- `.trellis/spec/web/state-and-data.md`
- `docs/design/v2-houfeng/component-spec.md`

### Implementation Risks

- Introducing new class families (`workbench-layout`, `compact-header`, `table-workbench`) without mapping them to existing BEM patterns may duplicate layout concepts already present in `pages.css`.
- Adding page/component-local CSS files would violate current styling guidance; `LoginPage.css` is the historical exception only.
- Moving route-specific links/API semantics into shared primitives would break route-agnostic component boundaries.
- Treating custom queue rows like table rows can create nested interactive accessibility issues; use DataTable for semantic tables, and for custom queues keep keyboard actions on visible links/buttons.
- Letting Drawer draft filters mutate URL/list state on every control change would violate the applied/draft contract and existing tests/spec expectations.
- Treating failed subscription requests as true missing-subscription evidence would mislead Asset Ledger decisions.
- Showing linked-node health in VPS list rows would exceed the current list contract (`active_node_link_count` only).
- Expanding Dashboard into all available backend facts would violate the asset-decision-first IA constraints.
- Changing DataTable sortable header CSS without the matching padding reset can create wider sortable headers than non-sortable headers.
- Changed pages or new atoms need corresponding tests and build/lint/test validation per web quality guidelines.
