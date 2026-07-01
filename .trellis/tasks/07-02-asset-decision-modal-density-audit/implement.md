# Asset Decision Modal Density Follow-up Implementation Plan

## Preconditions

- Branch: `fix/asset-decision-modal-density-audit`.
- Hooks enabled with `sh scripts/setup-git-hooks.sh`.
- Task must be started before production edits:

```bash
python3 ./.trellis/scripts/task.py start 07-02-asset-decision-modal-density-audit
```

## Step 1: Reproduce and Measure

- Run current focused test to establish baseline:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

- Start local preview and use mock asset workflow data.
- Use CDP/browser scripts or `scripts/visual_evidence.py browser-sanity` if local Playwright works.
- Capture DOM metrics for each modal/panel:
  - visible dialog text length.
  - number of buttons/links/form fields.
  - number of `section`, `article`, `table`, and paragraph nodes.
  - forbidden report markers and cross-task labels.
  - document/body horizontal overflow.

## Step 2: RED Tests

Modify `web/src/pages/AssetDecisionsPage.test.tsx` first.

Add helper assertions:

- `expectDialogTaskDensity(dialog, options)` for text length, button count, form field count, and absence of cross-task selectors.
- `expectNoReportStack(dialog)` for old report concepts and over-dense evidence/member/raw terms.
- `expectOnlySelectedTask(dialog, task)` for panel isolation.

Add failing coverage for:

- Budget/cost auto group members and save panels.
- Non-cost auto group members and raw separation.
- Manual group members/edit/add/save/raw isolation.
- Template create/members/status normal vs confirmation states.
- Saved record details opening directory first and execution/source/members/raw isolation.

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Confirm failures are about current density/isolation gaps, not typos.

## Step 3: Production Fix

Modify `web/src/pages/AssetDecisionsPage.tsx`:

- Tighten `renderMemberDecisionRows` output if density metrics prove it still over-renders.
- Keep member summary to a single short line; move provider/product/cost/facts to raw panel only.
- Ensure save record member editing is single-member expanded only.
- Ensure record execution panel defaults to compact rows and does not leak raw/member/source content.
- Ensure template and manual normal panels remove explanatory paragraphs unless in confirmation/error states.
- Avoid title/button duplicates that inflate visible hierarchy.

Modify `web/src/styles/pages.css` only as needed:

- Keep modal content width responsive.
- Make task panels and member rows stable on mobile.
- Keep raw tables internally scrollable.

## Step 4: GREEN and Broader Verification

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
cd web && npm run lint
cd web && npm run test -- --run
cd web && npm run build
git diff --check
```

Fix all failures.

## Step 5: Browser Sanity

Run local dev server and verify `/asset-decisions` with `mock-api asset-workflows`.

Desktop `1440x1000`:

- automatic cost/budget group.
- automatic non-cost group.
- manual group.
- scenario template.
- saved record -> source review -> reopened group.

Mobile `390x900`: same representative paths.

For each, record:

- no document/body horizontal overflow.
- modal text/block metrics within expected budget.
- no report markers or cross-task panels visible in default/directory/task panels.

## Step 6: Spec and Commit

- Update `.trellis/spec/web/component-conventions.md` if the new test approach becomes reusable guidance.
- Run Trellis check.
- Commit task artifacts, tests, code, spec updates.
- If requested or continuing full delivery, push branch, open PR, monitor CI, merge, Release Please, release and image publishing.
