# Implementation Plan

## 1. Red Tests

- Update `web/src/pages/AssetDecisionsPage.test.tsx` before production code:
  - automatic group default must not render member preview headings, member names, or per-member `处理` buttons;
  - budget/cost group and renewal group must use the same default contract;
  - saved record default must not render saved evidence member list or per-member saved reasons;
  - secondary panels still reveal member detail, saved evidence, write actions and follow-up controls.

## 2. Default Cover Helpers

- Add local render helpers in `AssetDecisionsPage.tsx`:
  - compact modal fact strip;
  - decision cover section;
  - detail nav with lower visual weight.
- Prefer editing existing helpers over introducing broad component extraction.

## 3. Automatic Group Modal

- Remove `renderMemberDecisionPreview` from default automatic group overview.
- Keep member detail only behind `members` panel.
- Shorten default fact strip labels and remove confidence/pressure/readiness numeric sentence.
- Keep `创建自定义组合` as primary action and `保存记录` as secondary action.

## 4. Manual Group Modal

- Remove member preview from default manual group overview.
- Reduce readiness footer to at most one short status, or remove it from default if it duplicates badge state.
- Preserve edit/member/add/save/raw panels.

## 5. Saved Record Modal

- Move `renderRecordSavedEvidence` out of default overview into an explicit panel or reduce default to a single saved judgement line.
- Preserve execution/member/source/raw panels.

## 6. Main Page Queue

- Remove explanatory queue copy and shrink group-card facts where possible without losing risk signal.
- Keep group detail panels as the place for deeper facts.

## 7. CSS

- Tighten modal default spacing.
- Make nav visually compact and non-dominant.
- Ensure mobile 390px does not create document/body overflow.

## 8. Verification

- `git diff --check`
- `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
- `cd web && npm run lint`
- `cd web && npm run test -- --run`
- `cd web && npm run build`
- Chromium CDP audit on desktop/mobile with asset-workflows fixture.

## Rollback

If a default-layer removal hides an essential workflow, restore the workflow inside a secondary panel rather than reintroducing report content into the default overview.
