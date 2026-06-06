# 资产组合决策工作台 UX 收敛与详情体验重构 - Implementation Plan

## Checklist

1. Load web/frontend specs before editing; confirm current `AssetDecisionsPage` state and tests.
2. Update page state helpers so main workbench tabs exclude `single_queue` while legacy `view=single_queue` remains supported.
3. Restructure top page layout:
   - compact focus summary,
   - compact next-work guide,
   - primary decision group workbench.
4. Add “场景与记录” workspace:
   - scenario template launcher,
   - custom manual groups panel,
   - saved records panel with readback/plan summary.
5. Demote renewal evidence and single VPS queue into support sections while preserving existing data loading and write behavior.
6. Improve responsive styles so main path uses cards/compact layouts and auxiliary wide tables are lower weight.
7. Update tests for new layout, old `single_queue` compatibility, scenario/record workspace, execution plan boundaries, and single queue update behavior.
8. Run targeted web tests, broader quality checks as needed, and Browser visual sanity for desktop/mobile.

## Validation Commands

- `npm --prefix web test -- AssetDecisionsPage`
- `npm --prefix web test -- api`
- `npm --prefix web run typecheck`
- `npm --prefix web run build`
- Browser sanity: `/asset-decisions?view=needs_decision&renew_within_days=30` desktop and mobile.

## Risk / Rollback Points

- Risk: removing `single_queue` from tabs could break deep links. Keep URL compatibility tests.
- Risk: combining scenario surfaces could hide record followup. Keep record list/detail tests.
- Risk: UI CTA accidentally writes business objects. Keep negative fetch assertions.
- Rollback: changes are front-end scoped; revert page/style/test edits without touching backend or migrations.
