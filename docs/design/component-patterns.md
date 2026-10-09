# 组件与页面设计原则

## Component defaults

The current frontend is a React/Vite SPA with plain CSS, design tokens, and shared atoms under `web/src/components/`. Prefer existing atoms and page primitives before adding a new abstraction.

Current reusable patterns include:

- `Button`, `Badge`, `Card`, `Input`, `Toggle`, and `Tabs` for ordinary controls;
- `Sparkline`, `MetricChart`, `TrendArrow`, and `StatusGlyph` for compact runtime evidence;
- `MonoDigits`, `Hostname`, and `Timestamp` for technical facts (`Timestamp mode="both"` shows one date when the relative form has already fallen back to the absolute date; future instants such as credential expiry read “N 分钟后”, and only |Δ| < 5s reads 刚刚);
- `ScrollRegion` for wide tables: a named, keyboard-focusable region whose scroll hint appears only while the content actually overflows;
- `DataTable` for dense list scanning;
- `Drawer` for advanced filters and scoped edit flows;
- `DetailSection` for titled surfaces;
- `PageState` for route/list loading, error, and empty states;
- `ActionConfirmationCard` for explicit state transitions.

Shared control conventions:

- Button sizes follow placement, not page: `md` for the page-header action group (`.page__actions`, including the record editor's save/publish commands) so it aligns with the 36px search and filter controls; `sm` for actions inside cards, panel headers, tables, and rows; `lg` only for touch-oriented commit actions at the end of a form (for example the comparison workbench's 另存为记录).
- `Tabs variant="pill"` and `SegmentedControl` share one segmented look: a recessed track (`--panel-bg-muted`) with a raised selected segment (`--surface-elevated` plus `--shadow-sm`). Do not add page-local padding/radius overrides; underline tabs remain the page-section navigation.
- Settings › 外观 presents the three theme presets as preview cards (`role="group"` 主题风格, each a pressed-state button named by the preset). Each preview is scoped with that preset's own `theme-<preset>-<scheme>` class so it shows the real canvas, panel, accent and state colours for the current light/dark choice; dark-only presets always preview dark and are labelled 仅深色.
- State colour carries meaning: accent bars, borders and badges reflect the current value (for example a count greater than zero), never a fixed per-card colour.
- Empty states use a solid panel (`.empty-state`) or, inside an existing card, a recessed fill (`--panel-bg-muted`) without a second dashed frame. `PageState kind="empty"` shows no default eyebrow; pass `eyebrow` only when it adds information.
- Direct children of a `.filter-bar__controls-row` (and one wrapper `div` level, such as the monitoring search) — `FilterSelect` / `FilterMultiSelect` / `FilterSearchSelect` and hand-written `.filter-select` text filters — render as compact chips: the dimension name is a visible prefix inside the same bordered control ("健康 全部 ▾") and is part of the control's accessible name, select chips are sized by their current value while text/date inputs keep a fixed width (`filter-select__control--text`), popovers stay at least 16rem wide, and an applied value (including non-empty text) adds `is-filtered` for an accent highlight alongside the changed visible value. Use the default "全部" placeholder there instead of repeating the dimension ("全部健康"); keep placeholders that name a real default (for example "最近 30 天"). Additive selects that append each pick to a removable list (record search 类型/状态分组/生命周期) reset after each pick and label their empty option truthfully — "全部" when nothing is selected, "已选 N" otherwise — with `is-filtered` while N > 0. Every filter stays visible, except that a page may opt in to `FilterBar`'s `narrowCollapse`: at ≤760px it keeps controls marked `filter-bar__keep` (such as search), the applied-filter chips with their remove buttons, 清空所有 and a 44px `筛选 (N)` disclosure button (`aria-expanded` / `aria-controls`, N counts applied filters other than search), and the other filters expand in place when opened; desktop layout is unchanged. The monitoring list uses it; drawers, forms and popover contents keep the label-above layout, and rows without chips keep their original bottom alignment.
- A failed submission never strands focus on `body`: validation errors move focus to the offending field, and a failed login returns focus to the selected password field with the error linked via `aria-describedby`.
- `.text-link` keeps a visible underline by default so links mixed into text (including `dd`/`span` fact rows) are not distinguished by color alone. Only links that stand alone as a section-header action ("查看…" next to a heading) add `text-link--action`, which hides the underline at rest and shows it on hover/focus.

Do not add a new atom because an old design document named one. Add one only when current code has repeated behavior, clear ownership, and tests or usage that justify the abstraction.

## Page composition

The current product prefers workbench-first pages:

- show the primary workflow in the first viewport;
- keep hero/header areas compact;
- put filters and advanced controls in drawers when they would crowd the scanning path;
- keep tables and queues dense enough for real inventory sizes;
- show current evidence and next actions before historical details;
- keep dangerous lifecycle actions isolated with explicit review and confirmation.

This is guidance, not a page freeze. A page may change structure when the current task has a clearer workflow and updates tests/specs accordingly.

## Shell navigation

- The sidebar lists every destination in always-open named groups instead of hiding frequently used pages behind a "更多" disclosure: 工作台, then 资产 (VPS, 订阅, 服务商, 资产决策, 归档), 观测 (监控, 入口探测, 事件), 记录 (运维记录, 命令审计), with 设置 pinned to the bottom. Each group is a `role="group"` labelled by its visible heading; collapsed and narrow rails hide the headings and separate groups with a divider.
- Every destination has a distinct icon; do not reuse one glyph for two destinations. Exactly one destination is current at a time; path matching follows the router (case-insensitive, trailing slash allowed). The observation group's items are fixed: 尚无观测 / 观测过期 are not destinations but category badge links beside 入口探测 (linking `/targets?view=unobserved|stale`, never `aria-current`, at least 24px targets; every sidebar badge shows at most `99+` while accessible names keep the exact count), so the list never grows or shrinks with data, and 入口探测 is current for every `/targets` path including those views. The 入口探测 link's accessible name lists all non-zero counts (abnormal, unobserved, stale) because the collapsed and narrow rails hide the category badges and show only a dot (abnormal red takes precedence, then stale, then unobserved).
- The top-bar global search shows a search icon and its keyboard shortcut (`⌘K` on Apple platforms, `Ctrl K` elsewhere, exposed via `aria-keyshortcuts`). On narrow screens (≤760px) the page title keeps priority: the search rests as a 44px magnifier, expands across the top bar while its input or results hold focus (click, Tab or the shortcut), and collapses back when focus leaves while keeping any typed query (signalled by an accent border); the shortcut hint is hidden.
- Viewports up to 1100px (tablets) start with the collapsed icon rail so content keeps its width; crossing that breakpoint resets to the width's default, and the toggle still expands the sidebar. The toggle is named by its action (折叠侧边栏 / 展开侧边栏) with `aria-expanded`. At 760px and below the fixed narrow rail applies.

## Narrow layouts

- At 760px and below, page headers stack: the title keeps its own line and the action group wraps beneath it, left-aligned. Settings rows likewise put the label above the control.
- Compact tiles and inspectors adapt to their own width with container queries rather than viewport breakpoints: a dashboard judgement tile narrower than 16rem moves its trend below the detail text; a directory inspector narrower than 28rem tightens its padding and splits renewal rows into name, then date and days; the directory column narrows to 280px when the VPS canvas is at most 50rem.

## Current surface responsibilities

- Dashboard / workbench: daily entry point and highest-priority next actions, not a dump of every API field.
- Asset decisions: portfolio and scenario decisions, saved records, readback, and renewal/cost evidence.
- VPS inventory/detail: asset facts, subscriptions, lifecycle decisions, monitoring linkage, and local asset workbenches.
- Monitoring list/detail: runtime observation objects, health, sync/heartbeat evidence, trends, incidents, and controlled agent actions.
- Targets/detail: service entrypoint probing, ProbeItems, coverage, recent observations, and target events.
- Records workspace / comparison: record reading and editing with a document column plus an attribute/material aside, collaboration below the document; the comparison workbench keeps selection and conditions beside one comparability → result → save column.
- Events: diagnostic and audit timeline with explicit filters.
- Settings: runtime configuration, notification settings, frequency defaults, overrides, retention, and theme controls.

Do not use these responsibilities to block future product exploration. Use them to avoid accidental duplication and to keep each page's current job legible.

## Contracts and tests

Component and page changes should update the closest useful tests:

- API client/type changes need tests at the API boundary.
- Page workflow changes need page tests that assert visible behavior, URL state, and request shape where applicable.
- Styling-only changes should still use browser sanity for user-visible work when layout, density, route structure, or responsive behavior can regress.
- Historical design files are not tests. Current behavior is proven by code, tests, docs, and explicit evidence.


## 具体页面规则

页面状态、布局、导航和验收合同统一在 [领域合同](../spec/contracts/README.md) 维护；共享组件实现约定见 [组件规范](../spec/web/component-conventions.md)。
