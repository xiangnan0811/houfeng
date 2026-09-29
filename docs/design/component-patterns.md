# 组件与页面设计原则

## Component defaults

The current frontend is a React/Vite SPA with plain CSS, design tokens, and shared atoms under `web/src/components/`. Prefer existing atoms and page primitives before adding a new abstraction.

Current reusable patterns include:

- `Button`, `Badge`, `Card`, `Input`, `Toggle`, and `Tabs` for ordinary controls;
- `Sparkline`, `MetricChart`, `TrendArrow`, and `StatusGlyph` for compact runtime evidence;
- `MonoDigits`, `Hostname`, and `Timestamp` for technical facts;
- `DataTable` for dense list scanning;
- `Drawer` for advanced filters and scoped edit flows;
- `DetailSection` for titled surfaces;
- `PageState` for route/list loading, error, and empty states;
- `ActionConfirmationCard` for explicit state transitions.

Shared control conventions:

- Button sizes follow placement, not page: `md` for the page-header action group (`.page__actions`) so it aligns with the 36px search and filter controls; `sm` for actions inside cards, panel headers, tables, and rows; `lg` only for touch-oriented or editor commit actions (for example the record editor's save/publish group).
- `Tabs variant="pill"` and `SegmentedControl` share one segmented look: a recessed track (`--panel-bg-muted`) with a raised selected segment (`--surface-elevated` plus `--shadow-sm`). Do not add page-local padding/radius overrides; underline tabs remain the page-section navigation.
- Empty states use a solid panel (`.empty-state`) or, inside an existing card, a recessed fill (`--panel-bg-muted`) without a second dashed frame. `PageState kind="empty"` shows no default eyebrow; pass `eyebrow` only when it adds information.
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
