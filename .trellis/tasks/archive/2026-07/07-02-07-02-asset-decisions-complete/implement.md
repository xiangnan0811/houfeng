# 资产组合决策页面全面修复 Implementation Plan

## Setup

- Worktree: `.worktree/asset-decisions-complete`
- Branch: `ux/asset-decisions-complete`
- Hooks: `sh scripts/setup-git-hooks.sh`

## Steps

1. Write failing frontend regression tests in `web/src/pages/AssetDecisionsPage.test.tsx`.
   - Auto group -> create manual group -> close manual modal -> `场景工作区` remains visible.
   - Auto group detail -> members/save/raw panels -> `创建组合` remains visible.
2. Run the targeted Vitest file and confirm the new tests fail for the expected reasons.
   - `npm --prefix web run test -- AssetDecisionsPage.test.tsx --run`
3. Write failing backend test for archived scenario template create rejection.
   - Prefer existing store/domain/handler test pattern closest to `CreateManualGroupFromTemplate`.
4. Run targeted Go tests and confirm the archived-template test fails for the expected reason.
5. Implement frontend state fix.
   - Make successful auto-group manual creation explicitly select `scenarios`.
   - Ensure URL open-state clearing does not clear selected secondary workbench.
6. Implement automatic group modal shell fix.
   - Render `创建组合` as modal-level persistent action across all `groupDetailPanel` values.
   - Keep detail nav and panel content stable.
7. Implement visual/layout cleanup for the touched surfaces.
   - Reduce auxiliary entry/workspace copy density.
   - Move horizontal overflow responsibility to raw table regions.
   - Keep asset-decision modal content/header/body opaque enough that background page text cannot show through task panels.
8. Implement backend archived template guard.
9. Run targeted tests until green.
   - `npm --prefix web run test -- AssetDecisionsPage.test.tsx --run`
   - `go test ./internal/center/assetdecisions ./internal/center/http/handlers ./internal/center/store -run 'AssetDecision|ScenarioTemplate'`
10. Run broader gates.
    - `npm --prefix web run test -- api.test.ts --run`
    - `make verify-web`
    - `make verify-go` or `./scripts/verify.sh` if both layers require full validation.
11. Run browser visual checks.
    - Desktop 1440x1000 and mobile 390x900.
    - Cover default page, create-combo close persistence, and members/save/raw persistent action.
12. Final audit.
    - Re-read PRD acceptance criteria.
    - Inspect `git diff`.
    - If any issue remains, continue fixing; do not mark goal complete.

## Verification Notes

- Red phase was verified before production changes: new frontend regressions failed in `AssetDecisionsPage.test.tsx`; archived-template store test failed before the backend guard.
- Green target checks passed:
  - `npm --prefix web run test -- AssetDecisionsPage.test.tsx --run`
  - `go test ./internal/center/assetdecisions ./internal/center/http/handlers ./internal/center/store -run 'AssetDecision|ScenarioTemplate|CreateManualGroupFromTemplate|TestAssetDecisionHandlersMapErrors'`
  - `./scripts/verify.sh`
- Browser visual checks used CDP on Chrome `localhost:9222` because `scripts/visual_evidence.py browser-sanity` was blocked by missing Python Playwright in the local environment. No browser dependency was added to the repo.
- CDP covered 1440x1000 and 390x900 default page, automatic group modal, create-combo close persistence, members/save/raw panels, and single-VPS processing panel. The check caught translucent modal ghosting; CSS was tightened and the processing panel was rechecked with opaque computed backgrounds and no document/body overflow.
