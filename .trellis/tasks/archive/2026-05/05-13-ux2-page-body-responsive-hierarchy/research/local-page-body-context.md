# Local Page Body Context

## Reviewed

- `docs/release/ui-evolution-roadmap.md`
- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`
- `docs/operations/v2-visual-evidence.md`
- `.trellis/spec/web/{index,styling-guidelines,component-conventions,state-and-data,quality-guidelines}.md`
- `web/src/styles/pages.css`
- `web/src/pages/{VPSPage,AssetDecisionsPage,NodesPage,TargetsPage,EventsPage,SettingsPage}.tsx`
- UX-1 visual evidence notes from archived task `05-13-ux1-app-shell-visual-baseline`

## Findings

- UX-1 fixed shell-level mobile issues, but route screenshots showed page-body issues can still dominate narrow viewports.
- `pages.css` already contains many page primitives and responsive rules. The likely high-value fix is shared CSS refinement, not per-page rewrites.
- Existing asset pages already use a decent structure: page panel header, summary/workbench surface, tabs/filter bar, drawer filters, DataTable. The risk is responsive behavior and section density, not missing functionality.
- Observability pages have support surfaces and table wrappers that should keep their own horizontal scrolling without widening the entire document.
- `docs/release/ui-evolution-roadmap.md` is stale relative to the user's accepted next step: it says UX-2 is Dashboard polish, but the chosen next step is page-body responsive hierarchy.

## Implications

- Prefer one shared responsive pass in `web/src/styles/pages.css`, plus a small roadmap doc update.
- Avoid changing API helpers, page state machines, or URL-state logic unless a layout bug requires markup hooks.
- Treat large table horizontal scroll as acceptable only when contained inside the table panel.
- Run screenshot sanity against the full core route matrix because shared CSS changes can affect many pages.

## Verification Notes

- Local quality gates passed after implementation:
  - `cd web && npm run lint`
  - `cd web && TMPDIR=$PWD/.tmp npm run test -- --run` (`60` files / `460` tests)
  - `cd web && npm run build`
- Browser sanity used a local mocked API at `http://127.0.0.1:18080` and Vite preview at `http://127.0.0.1:5178/`.
- Screenshot evidence was captured for `/`, `/vps`, `/asset-decisions`, `/nodes`, `/targets`, `/events`, `/settings` at `1440x1000` and `390x900`.
- Mobile layout probe result: all checked routes reported page-level overflow `0`; `/vps`, `/asset-decisions`, `/nodes`, and `/targets` keep wide tables inside local `.page-panel--scroll-x` surfaces.
- During verification, Nodes and Targets initially exposed clipped table content outside a local scroll surface. The implementation now wraps those DataTables in page-panel scroll containers.
