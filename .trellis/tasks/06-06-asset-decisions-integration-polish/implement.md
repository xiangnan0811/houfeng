# 资产决策中枢集成体验收敛 Implement

## Checklist

1. Confirm task context and specs.
   - Read `prd.md`, `design.md`, `.trellis/spec/web/index.md`, `state-and-data.md`, `component-conventions.md`, `styling-guidelines.md`, and v2 component spec AssetDecisionsPage section.
2. Normalize deep-link helpers and text.
   - Update `dashboardLinks.ts` constants.
   - Update Dashboard asset lane labels from queue wording to combination wording.
   - Update Monitoring / Targets support surfaces to use explicit evidence cleanup links.
   - Keep linked VPS detail routes with `vps_id` and scenario.
3. Tighten AssetDecisionsPage context / hierarchy where needed.
   - Ensure context chips remain visible and legacy single queue notice remains accurate.
   - Avoid promoting records, renewal evidence, or single VPS queue above automatic groups.
4. Update tests.
   - Dashboard command surface / Dashboard page assertions for deep links.
   - Monitoring / Targets support surface tests or add focused tests if missing.
   - AssetDecisionsPage context chips and legacy compatibility.
5. Update docs / specs.
   - `docs/operations/v2-visual-evidence.md`.
   - `.trellis/spec/web/state-and-data.md` or `docs/design/v2-houfeng/component-spec.md` only if implementation introduces a durable rule not already captured.
6. Validate.
   - `npm --prefix web test -- AssetDecisionsPage DashboardPage DashboardCommandSurface MonitoringSupportSurface TargetsSupportSurface`
   - `npm --prefix web test -- api`
   - `npm --prefix web run lint`
   - `npm --prefix web run build`
   - `git diff --check`
   - Browser sanity for `/asset-decisions?view=needs_decision&renew_within_days=30` desktop and mobile using current local options.
7. Finish.
   - Trellis validate / finish.
   - Commit, push, PR, CI monitor, merge when green, monitor post-merge release/publish if triggered.

## Risk Points

- Do not delete legacy `single_queue` parsing in `AssetDecisionsPage`.
- Do not turn support surface links into business PATCH actions.
- Do not update backend contracts or migrations.
- Do not introduce new global CSS tokens for local polish.
- Keep generated URLs readable and stable; prefer helper constants where already present.

## Validation Notes

If local browser sanity with mock API is unavailable due local Playwright tooling, report the limitation and use the in-app browser plus dev server/mock server fallback already proven in previous tasks.
