# Asset Decision Modal Comprehensive Density Implementation Plan

## Preconditions

- Branch: `fix/asset-decision-modal-comprehensive-density`.
- Hooks enabled with `sh scripts/setup-git-hooks.sh`.
- Start task before production code edits:

```bash
python3 ./.trellis/scripts/task.py start 07-02-asset-decision-modal-comprehensive-density
```

## Step 1: Baseline Audit

- Read current implementation sections:
  - `renderDetailCommand`
  - `renderDetailDirectory`
  - `renderMemberDecisionRows`
  - `renderRecordDraftMemberRows`
  - `renderRecordExecutionBoard`
  - auto/manual/template/record modal render blocks
- Read current focused tests in `web/src/pages/AssetDecisionsPage.test.tsx`.
- Run current focused tests:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

## Step 2: RED Tests

Modify `web/src/pages/AssetDecisionsPage.test.tsx` first.

Add helper assertions:

- `dialogMetrics(dialog)` returns visible text length, buttons, inputs, textareas, selects, tables, articles.
- `expectCompactLayer(dialog, limits)` asserts density budgets.
- `expectNoCrossTaskContent(dialog, forbiddenTexts)` asserts ordinary panels do not mix tasks.
- `expectPreviewLimit(container, rowSelector, max)` asserts member preview caps.

Add failing tests for:

- auto cost group default/directory/members/save density.
- auto non-cost group default/directory/members density.
- manual group members/edit/add/save/raw isolation.
- template create/members/status normal vs confirmation states.
- saved record execution/source/members/raw isolation.
- source review reopening source group keeps the source group default/directory short.

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Expected: at least one new test fails for density/isolation, not syntax.

## Step 3: UI Fix

Modify `web/src/pages/AssetDecisionsPage.tsx`:

- Remove duplicate headings and explanatory copy from ordinary task panels.
- Tighten member rows:
  - one primary row action only.
  - no provider/product/cost/facts in member task panels.
  - compact summary fallback.
- Tighten save panels:
  - preview rows capped.
  - no all-member editor expansion.
- Tighten execution panel:
  - compact row copy.
  - no migration “推进” wording.
  - preview cap applies before lane rendering and hidden note remains.
- Tighten template/manual panels:
  - ordinary panel body only contains fields/actions needed for the task.
  - confirmation copy remains gated behind explicit pending confirmation state.

Modify CSS only if needed:

- Ensure compact row layouts stay stable on mobile.
- Keep raw tables internally scrollable.
- Avoid adding decorative cards or nested card structures.

## Step 4: GREEN Verification

Run and fix failures:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
git diff --check
```

## Step 5: Browser Sanity

Run local browser sanity with mock asset workflow data:

- `/asset-decisions` desktop `1440x1000`.
- `/asset-decisions` mobile `390x900`.
- Exercise:
  - auto cost group.
  - auto non-cost group.
  - manual group.
  - scenario template.
  - saved record.
  - source review.

Record:

- document/body no horizontal overflow.
- dialog density metrics within budget.
- forbidden marker/content absent from default, directory, and ordinary task panels.

## Step 6: Spec and Commit

- Update `.trellis/spec/web/component-conventions.md` only if rules need clarification.
- Update `.trellis/spec/web/state-and-data.md` to remove stale guidance that still instructs `GROUP TO SCENARIO` / `EVIDENCE MATRIX` default detail rendering.
- Run Trellis check expectations.
- Commit task artifacts, tests, implementation, CSS/spec updates.
