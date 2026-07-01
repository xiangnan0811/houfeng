# Asset Decision Modal Simplification Implementation Plan

## 1. Red Tests

- Update `web/src/pages/AssetDecisionsPage.test.tsx` to fail on current behavior:
  - Automatic group default modal must not contain raw member table, save form, VPS work panel, or explanatory fallback text.
  - Manual group default modal must not contain edit form, add member form, full member edit controls, raw table, or explanatory fallback text.
  - Record default modal must not contain execution board, source continuity block, member follow-up forms, or raw table.
  - Template default modal must not contain create form or full blueprint list.
- Add expansion assertions for every hidden capability.
- Run `cd web && npm run test -- --run AssetDecisionsPage.test.tsx` and confirm failures are caused by current default over-rendering.

## 2. Add Panel State

- Add detail panel union types and `useState` variables in `AssetDecisionsPage`.
- Reset panel state to `overview` in close/apply URL open helpers.
- Switch panels when existing handlers create drafts or select a VPS:
  - `startRecordSave` / `startManualRecordSave`;
  - `selectVPS`;
  - template status request.

## 3. Automatic Group Modal

- Keep summary and compact command.
- Replace default `renderMemberDecisionCards` with compact member preview.
- Render secondary buttons for members/save/raw/VPS panels.
- Move full member cards, record form, raw table, and work panel behind panel conditions.
- Remove default explanatory fallback strings from overview.

## 4. Manual Group Modal

- Keep summary and compact command/readiness.
- Render compact member preview by default.
- Move group edit form, add member form, full member cards/edit forms, save record form, and raw table behind panels.
- Keep remove confirmation only inside member maintenance context.

## 5. Record Modal

- Keep summary, goal/status snapshot, and compact saved evidence.
- Move status patch form and execution board behind `execution`.
- Move member follow-up controls behind `members`.
- Move source continuity behind `source`.
- Move raw table behind `raw`.

## 6. Template Modal

- Keep summary and template goal/status overview.
- Move create form behind `create`.
- Move member blueprint behind `members`.
- Move status confirmation behind `status`.

## 7. CSS

- Add/adjust classes for:
  - detail subnav / panel launcher;
  - compact member preview;
  - detail panel wrapper;
  - less dense modal overview spacing.
- Ensure `.asset-table-scroll` remains the only horizontal scroll container for tables.
- Verify mobile rules for single-column panel controls.

## 8. Verification

- `git diff --check`
- `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
- `cd web && npm run lint`
- `cd web && npm run test -- --run`
- `cd web && npm run build`
- Browser/Playwright validation with mock API:
  - route `/asset-decisions`;
  - viewports `1440x1000` and `390x900`;
  - open automatic group, manual group, record, template;
  - assert no document/body horizontal overflow;
  - assert default modal lacks tables/forms/execution blocks;
  - capture screenshots or DOM evidence.

## Rollback

If panel gating creates a regression in a mutation path, keep the panel model and restore the capability within its panel. Do not revert to default all-content rendering.
