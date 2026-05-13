# Local Shell Context

## Reviewed

- `docs/release/ui-evolution-roadmap.md`
- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`
- `docs/operations/v2-visual-evidence.md`
- `.trellis/spec/web/{index,styling-guidelines,component-conventions,state-and-data,quality-guidelines}.md`
- `web/src/app/layout/{AppShell,Sidebar,TopBar,GlobalSearch,Breadcrumb,SyncStatus,UserChip}.tsx`
- `web/src/app/layout/layout.css`
- `web/src/app/metadata.ts`
- `web/src/app/router.tsx`

## Findings

- Current shell functionality is already aligned with the product direction: grouped nav, `工作台`, dashboard summary source labels, and neutral anomaly counts exist.
- The shell visual baseline is still too broad/glassy for the v2 contract: 260px sidebar, sans brand, stronger transform hover, hardcoded RGBA surfaces, 40px nav rows, and larger user avatar create a heavier chrome than the 232px compact workbench target.
- Breadcrumb currently omits asset routes. This weakens the UX-1 goal because VPS detail is part of the real-data path and needs visible navigation context.
- GlobalSearch behavior is acceptable and already parallelizes node/target fetches. The likely fix is sizing/responsive styling, not data or state changes.
- The safest first implementation is a scoped shell/chrome polish plus Breadcrumb route coverage; page bodies should stay untouched.

## Implications

- Prefer CSS/Breadcrumb/test changes over route/page rewrites.
- Keep dashboard summary semantics unchanged.
- Do not introduce new dependencies or a visual regression framework.

## Visual Evidence Notes

- The running center on `:8080` was unauthenticated during this session, so visual sanity used a temporary local mock API on `:18080` and Vite proxy override (`VITE_API_TARGET=http://127.0.0.1:18080`). No repository files were created for the mock.
- Captured routes: `/`, `/vps`, `/asset-decisions`, `/nodes`, `/targets`, `/events`, `/settings`.
- Captured viewports: `1440x1000` and `390x900`.
- Screenshots were generated under `/tmp/houfeng-ux1-shots` for local inspection only.
- The first mobile pass exposed a shell issue: stacked mobile navigation consumed the whole first viewport. The shell now uses a compact mobile nav grid.
- The second mobile pass exposed a page-body issue on `/nodes`: `NodesHero` inherited an inline desktop layout and clipped the stats grid. A narrow, low-risk responsive rule was added for `.nodes-hero` at small widths.
