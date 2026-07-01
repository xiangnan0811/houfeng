# Asset Decision Page Deep Declutter Implementation Plan

## 1. Red Tests

- Update `web/src/pages/AssetDecisionsPage.test.tsx` to fail on current dense defaults:
  - main group list default must not render `NEXT STEP` / `COMPARISON` report blocks;
  - group detail default must not render `GROUP DECISION`, `MEMBERS`, score bar labels, `支撑 N · 风险 N · 缺口 N`, panel nav summary text, or full member-card content;
  - record/manual detail defaults must not render score bars or checklist/card-heavy blocks;
  - secondary panels must still expose original tables/forms/actions.

## 2. Compact Helpers

- Add local helper(s) in `AssetDecisionsPage.tsx`:
  - compact risk chip selector with max 2 chips;
  - compact fact rail renderer;
  - compact member/object row renderer;
  - concise panel nav renderer with label + count only.
- Keep helper scope local to this page to avoid broad component extraction.

## 3. Main Page Declutter

- Simplify command summary:
  - remove low-value explanatory copy;
  - reduce facts to 3-4 high-signal numbers.
- Replace automatic group card report layout:
  - remove `NEXT STEP` and `COMPARISON` subcards from default row;
  - remove score bars;
  - keep title, one-line reason, max 2 chips, 3 facts, primary action.

## 4. Automatic Group Detail

- Replace default `renderDetailCommand` usage with compact modal lead.
- Replace default `renderMemberDecisionPreview` with max 2 concise rows.
- Move score/evidence detail into member/detail panel or an evidence panel if needed.
- Shorten panel nav to label + count only.
- Preserve:
  - create manual group;
  - save record;
  - members panel;
  - data panel;
  - VPS work panel.

## 5. Manual / Record / Template Details

- Manual detail:
  - default shows readiness as one line, not checklist cards;
  - max 2 members visible;
  - edit/member/add/save/raw panels unchanged.
- Record detail:
  - default shows status, goal, follow-up counts, saved judgement line;
  - saved evidence score bars move out of default;
  - execution/member/source/raw panels unchanged.
- Template detail:
  - keep current compact direction;
  - align panel nav and copy budget with other modals.

## 6. CSS

- Add compact classes for:
  - queue row;
  - compact modal lead;
  - fact rail;
  - concise member rows;
  - icon-like panel nav.
- Reduce nested card feel and repeated borders.
- Ensure mobile modal first viewport is not dominated by score bars or long paragraphs.

## 7. Browser Verification

- Run local Vite + asset-workflows mock.
- CDP/browser checks:
  - desktop `1440x1000`;
  - mobile `390x900`;
  - open automatic groups: cancel, renewal, cost, evidence;
  - open record, manual group, template;
  - inspect text/badge/button counts against current baseline;
  - verify no document/body horizontal overflow.

## 8. Quality Gates

- `git diff --check`
- `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
- `cd web && npm run lint`
- `cd web && npm run test -- --run`
- `cd web && npm run build`

## Rollback

If a mutation path regresses, restore that capability inside its secondary panel. Do not restore default report-style rendering.
