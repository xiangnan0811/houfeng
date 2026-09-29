# 组件与页面设计原则

## Component defaults

The current frontend is a React/Vite SPA with plain CSS, design tokens, and shared atoms under `web/src/components/`. Prefer existing atoms and page primitives before adding a new abstraction.

Current reusable patterns include:

- `Button`, `Badge`, `Card`, `Input`, `Toggle`, and `Tabs` for ordinary controls;
- `Sparkline`, `MetricChart`, `TrendArrow`, and `StatusGlyph` for compact runtime evidence;
- `MonoDigits`, `Hostname`, and `Timestamp` for technical facts (`Timestamp mode="both"` shows one date when the relative form has already fallen back to the absolute date);
- `ScrollRegion` for wide tables: a named, keyboard-focusable region whose scroll hint appears only while the content actually overflows;
- `DataTable` for dense list scanning;
- `Drawer` for advanced filters and scoped edit flows;
- `DetailSection` for titled surfaces;
- `PageState` for route/list loading, error, and empty states;
- `ActionConfirmationCard` for explicit state transitions.

Shared control conventions:

- Button sizes follow placement, not page: `md` for the page-header action group (`.page__actions`) so it aligns with the 36px search and filter controls; `sm` for actions inside cards, panel headers, tables, and rows; `lg` only for touch-oriented or editor commit actions (for example the record editor's save/publish group).
- `Tabs variant="pill"` and `SegmentedControl` share one segmented look: a recessed track (`--panel-bg-muted`) with a raised selected segment (`--surface-elevated` plus `--shadow-sm`). Do not add page-local padding/radius overrides; underline tabs remain the page-section navigation.
- Settings › 外观 presents the three theme presets as preview cards (`role="group"` 主题风格, each a pressed-state button named by the preset). Each preview is scoped with that preset's own `theme-<preset>-<scheme>` class so it shows the real canvas, panel, accent and state colours for the current light/dark choice; dark-only presets always preview dark and are labelled 仅深色.
- State colour carries meaning: accent bars, borders and badges reflect the current value (for example a count greater than zero), never a fixed per-card colour.
- Empty states use a solid panel (`.empty-state`) or, inside an existing card, a recessed fill (`--panel-bg-muted`) without a second dashed frame. `PageState kind="empty"` shows no default eyebrow; pass `eyebrow` only when it adds information.
- Direct children of a `.filter-bar__controls-row` (and one wrapper `div` level, such as the monitoring search) — `FilterSelect` / `FilterMultiSelect` / `FilterSearchSelect` and hand-written `.filter-select` text filters — render as compact chips: the dimension name is a visible prefix inside the same bordered control ("健康 全部 ▾") and is part of the control's accessible name, select chips are sized by their current value while text/date inputs keep a fixed width (`filter-select__control--text`), popovers stay at least 16rem wide, and an applied value (including non-empty text) adds `is-filtered` for an accent highlight alongside the changed visible value. Use the default "全部" placeholder there instead of repeating the dimension ("全部健康"); keep placeholders that name a real default (for example "最近 30 天"). Additive selects that append each pick to a removable list (record search 类型/状态分组/生命周期) reset after each pick and label their empty option truthfully — "全部" when nothing is selected, "已选 N" otherwise — with `is-filtered` while N > 0. Every filter stays visible; drawers, forms and popover contents keep the label-above layout, and rows without chips keep their original bottom alignment.
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
- Every destination has a distinct icon; do not reuse one glyph for two destinations.
- The top-bar global search shows a search icon and its keyboard shortcut (`⌘K` on Apple platforms, `Ctrl K` elsewhere, exposed via `aria-keyshortcuts`). On narrow screens the page title keeps priority over the search field and the shortcut hint is hidden.

## Current surface responsibilities

- Dashboard / workbench: daily entry point and highest-priority next actions, not a dump of every API field.
- Asset decisions: portfolio and scenario decisions, saved records, readback, and renewal/cost evidence.
- VPS inventory/detail: asset facts, subscriptions, lifecycle decisions, monitoring linkage, and local asset workbenches.
- Monitoring list/detail: runtime observation objects, health, sync/heartbeat evidence, trends, incidents, and controlled agent actions.
- Targets/detail: service entrypoint probing, ProbeItems, coverage, recent observations, and target events.
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
