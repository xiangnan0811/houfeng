# Local UI Context For Next Roadmap

## Sources Reviewed

- `docs/release/current-state-and-next-stage-plan.md`
- `docs/release/core-pages-product-ux-replan.md`
- `docs/release/next-phase-plan.md`
- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`
- `docs/operations/v2-visual-evidence.md`
- `web/src/app/layout/AppShell.tsx`
- `web/src/app/layout/AppShell.test.tsx`
- `web/src/app/layout/Sidebar.tsx`
- `web/src/app/layout/layout.css`
- `web/src/pages/DashboardPage.tsx`
- `web/src/pages/DashboardPage.test.tsx`
- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/pages/VPSPage.tsx`
- `web/src/pages/VPSDetailPage.tsx`

## Findings

- The product direction is already settled: Houfeng should read as an asset decision workbench plus observability evidence system.
- The old immediate work queue is closed or deferred. Real 40+ VPS import is data/user-authorization dependent, and mechanical frontend splitting is paused until UX direction stabilizes.
- AppShell is no longer a raw V1 shell. It already has grouped navigation, `工作台` copy, dashboard-backed summary status, and tests guarding against stale `首页` nav copy.
- Dashboard is already structurally aligned with the command-surface contract: asset lane, observability lane, next actions, state-aware workbench, and tests preventing old KPI/API dump regressions.
- The remaining risk is not “missing all v2 structure”; it is visual cohesion across shell/page chrome, first-viewport hierarchy, density, responsive behavior, and evidence discipline.
- Because current code is already partially redesigned, the next task should be a baseline-reset and polish pass, not a rewrite.

## Planning Consequence

UX-1 should be the first implementation task. It should stabilize app shell, navigation, chrome, density, and preview evidence so later Dashboard/VPS/detail work shares a credible visual baseline.
