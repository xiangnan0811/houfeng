# Implementation Plan

## Files

- Modify `web/src/pages/vps-detail/vpsDetailOverviewModel.ts`
  - Add `attentionItems` under `judgement`.
  - Reuse existing action types and context action construction.
  - Promote cancellation work into the same attention item list.
- Modify `web/src/pages/vps-detail/VPSDetailOverviewPanel.tsx`
  - Render multiple attention items inside top current judgement.
  - Route modal/link actions through existing callbacks.
- Modify `web/src/pages/VPSDetailPage.tsx`
  - Remove `VPSContextActionPanel` import and render block.
- Modify `web/src/index.css`
  - Add compact current-judgement attention list styles.
  - Remove or leave unused context-action styles only if they do not affect lint/build.
- Modify `web/src/pages/vps-detail/vpsDetailOverviewModel.test.ts`
  - Add model-level attention item coverage.
- Modify `web/src/pages/VPSDetailPage.test.tsx`
  - Add page-level regression for “运行观测需要核对” top placement and middle-section removal.

## Steps

1. Write failing model tests for top attention item generation and simultaneous cancellation + monitoring attention.
2. Write failing page test proving “运行观测需要核对” is inside current judgement and no middle action section exists.
3. Run focused tests and confirm they fail for the intended reason.
4. Update `vpsDetailOverviewModel.ts` to build `judgement.attentionItems`.
5. Update `VPSDetailOverviewPanel.tsx` to render attention items and dispatch modal/link actions.
6. Remove `VPSContextActionPanel` usage from `VPSDetailPage.tsx`.
7. Add compact CSS for top judgement attention list.
8. Run focused tests until green.
9. Run `cd web && npm run lint`, `cd web && npm run test -- --run`, `cd web && npm run build`.
10. Start local dev server and use browser sanity on `/vps/vps_001` if fixture/backend state is available; otherwise run frontend test evidence and note browser limitation.
11. Run Trellis check, update specs if a reusable convention was learned, commit on the feature branch.
12. Push branch, open PR, monitor CI, merge when green, then handle release/image workflow only if repository automation creates it for this frontend patch.

## Risk Points

- `primaryAction` currently assumes only cancellation; keeping it while adding `attentionItems` could create duplicate buttons. The implementation should render `attentionItems` as the source of truth and keep `primaryAction` only for backward compatibility/tests if needed.
- `VPSContextActionPanel` import removal must not leave dead imports.
- Link actions and modal actions share the same action type; panel dispatch must handle `monitoring-instance-create` via `onMonitoringAgent`, not blindly open create modal when an existing MonitoringInstance should be reused.
- Tests with `getAllByRole('button', { name: ... })` may need scoping to `当前判断` because the same action label appears in top menu or related overview.

## Validation Commands

```bash
cd web && npm run test -- --run src/pages/vps-detail/vpsDetailOverviewModel.test.ts src/pages/VPSDetailPage.test.tsx
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
```
