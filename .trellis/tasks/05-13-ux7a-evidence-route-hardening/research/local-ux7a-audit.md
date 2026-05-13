# Local UX-7A Audit

## Evidence UI Duplication

The observability support pages now share a stable pattern:

* `NodesSupportSurface` renders a lead block, four support lanes, and a focus row.
* `TargetsSupportSurface` renders the same lead/focus skeleton with Target-specific copy and links.
* `EventsSupportSurface` renders the same lead/focus skeleton with Event-specific copy and links.

The CSS already proves the pattern is shared: `pages.css` groups selectors such as `.nodes-evidence-lead`, `.targets-evidence-lead`, and `.events-evidence-lead` for the same declarations. This makes the next safe abstraction a shared business component in `web/src/components/`, not a low-level atom.

Recommended component boundary:

* `ObservabilityEvidenceLead`
  * Props: tone, eyebrow, title, description, filters/defaultFilterLabel, filterAriaLabel, action, secondaryAction.
  * Pure presentation. Parent passes the action as ReactNode so page-specific Link/Button behavior stays outside.
* `ObservabilityEvidenceFocus`
  * Props: stable flag, glyph, eyebrow, title, description, meta, action.
  * Pure presentation. Parent supplies domain-specific `StatusGlyph`, `Hostname`, and `Link`.

Why this boundary is safe:

* The repeated JSX appears three times and has stabilized through UX-6A/6B/6C.
* Page-specific domain decisions remain in page-private helpers and support surfaces.
* The shared components are testable without fetch, router state, or URL-state.

## Route Loading / Bundle Warning

`web/src/app/router.tsx` eagerly imports every page:

* Dashboard, asset decisions, VPS, details, observability pages, Settings, and Login are all in the same route module.
* The most recent build artifact before UX-7A had a single main JS bundle (`index-UGrxPhOF.js`) and Vite repeatedly warned about chunks larger than 500 kB.

Recommended route hardening:

* Use `lazy()` from React for route page modules.
* Wrap route elements in a small `Suspense` helper local to `router.tsx`.
* Use a v2-compatible loading fallback: no spinner, Chinese loading copy, `page-panel` surface, and mono metadata.
* Keep `appRoutes` exported so `matchRoutes(appRoutes, ...)` tests continue to verify route registration.

Why this boundary is safe:

* The project is a pure client SPA and React 19 supports `lazy`/`Suspense`.
* No route loaders/actions are in use, so element-level lazy splitting is enough.
* Existing page tests render pages directly and do not depend on router eager imports.

## Visual Evidence Need

This task changes user-visible DOM/CSS class structure on `/nodes`, `/targets`, and `/events`, even if the intended visual output is unchanged. Per `docs/operations/v2-visual-evidence.md`, browser sanity is required for:

* `/nodes` at 1440x1000 and 390x900
* `/targets` at 1440x1000 and 390x900
* `/events` at 1440x1000 and 390x900

Screenshots are optional if the browser sanity proves nonblank viewports, no horizontal overflow, and no evidence lead/focus overlap. If screenshots are committed, store them under `docs/operations/v2-visual-evidence/` and add manifest rows marked `Needs review`.

## Implementation Result

* Shared evidence UI landed as:
  * `web/src/components/ObservabilityEvidenceLead.tsx`
  * `web/src/components/ObservabilityEvidenceFocus.tsx`
* Page support surfaces now pass domain-specific actions, glyphs, Hostname nodes, and links into the shared components. Page-specific evidence ranking and route decisions remain in page/private helpers.
* Shared CSS class names now use `.observability-evidence-*` instead of page-specific grouped selectors.
* Route pages are lazy-loaded from `web/src/app/router.tsx`; `RouteModuleFallback` provides the route-level loading surface.
* Build output after route splitting no longer emits the Vite large chunk warning. The entry chunk is `291.72 kB` / `93.20 kB gzip`; route chunks range from small helper chunks to `57.26 kB` for `VPSDetailPage`.
* Browser sanity passed for `/nodes`, `/targets`, and `/events` at `1440x1000` and `390x900`; document/body scroll width matched viewport width and evidence lead/focus blocks were present and in viewport.
