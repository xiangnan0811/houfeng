# Current component and page patterns

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

## Historical references

The old component/page bundle was replaced by a short stub at `../v2-houfeng/component-spec.md`; use git history if you need to inspect it. Treat any old page notes as source material, not an instruction to preserve every surface exactly.
