# Asset Decision Modal Visual Simplification Implementation Plan

## Preconditions

- Branch: `fix/asset-decision-modal-visual-simplification`.
- Hooks enabled with `sh scripts/setup-git-hooks.sh`.
- Start task before production code edits:

```bash
python3 ./.trellis/scripts/task.py start 07-02-asset-decision-modal-visual-simplification
```

## Step 1: Current-State Audit

- Inspect current rendering code in `web/src/pages/AssetDecisionsPage.tsx`:
  - `renderDetailCommand`
  - `renderDetailDirectory`
  - `renderMemberDecisionRows`
  - `renderRecordDraftMemberRows`
  - `renderRecordExecutionBoard`
  - automatic/manual/template/record modal blocks
- Inspect current focused tests in `web/src/pages/AssetDecisionsPage.test.tsx`.
- Run current focused test once for baseline:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

## Step 2: Runtime Audit

- Start local Vite dev server.
- Use CDP browser scripts against mock asset workflow data.
- Capture screenshots and metrics for:
  - automatic cost/budget group
  - at least two non-cost automatic groups
  - manual group
  - scenario template
  - saved record
  - source review reopening source group
- Record current failures in task notes or implementation comments before editing.

## Step 3: RED Tests

Modify `web/src/pages/AssetDecisionsPage.test.tsx` first.

Add or tighten helper assertions:

- visible text length budgets for cover/directory/task panels
- interactive control count budgets
- table/form absence in ordinary layers
- preview row limits
- forbidden cross-task content and internal ID checks

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Expected: at least one new test fails against current UI or demonstrates a coverage gap.

## Step 4: UI Fix

Modify `web/src/pages/AssetDecisionsPage.tsx`:

- Enforce short cover/default layers for every modal type.
- Make directory entries purely navigational and short.
- Tighten member rows and save/execution/source/template/manual panels.
- Move complete facts and wide tables to raw/bottom-level panels only.
- Keep payload generation and existing API calls unchanged.

Modify `web/src/index.css` only if runtime evidence shows layout instability, excessive spacing, or mobile overflow.

## Step 5: GREEN Verification

Run and fix failures:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
git diff --check
```

## Step 6: Browser Sanity

Repeat runtime audit after implementation:

- desktop `1440x1000`
- mobile `390x900`
- same modal paths as Step 2

Save metrics/screenshots under ignored temporary paths and summarize evidence in final response.

## Step 7: Spec, Review, Commit, PR, Release

- Update `.trellis/spec/web/*` if the implementation clarifies a durable rule.
- Run Trellis check expectations.
- Commit task artifacts, tests, implementation, CSS/spec updates.
- Push branch, create PR, monitor CI.
- Merge after checks pass.
- Monitor release PR, publish workflow, and Docker Hub tags if release is triggered.
