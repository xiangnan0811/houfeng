# VPS detail attention state integration implementation plan

## Branch

- Branch: `fix/vps-detail-attention-states`
- Base: `main`

## Files

- Modify `web/src/pages/vps-detail/vpsDetailOverviewModel.ts`
  - Remove `contextAction` from `VPSDetailOverviewModel`.
  - Remove `buildContextAction`.
  - Keep `VPSContextAction` as the attention item type unless renaming is low-risk.
- Modify `web/src/pages/vps-detail/vpsDetailOverviewModel.test.ts`
  - Replace all `contextAction` expectations with `judgement.attentionItems` expectations.
  - Add a combined cancellation + monitoring + renewal-risk test.
- Modify `web/src/pages/VPSDetailPage.tsx`
  - Remove `state.subscriptionsError` from `pageFeedbackCandidates`.
  - Keep operation feedback notices/errors for user-triggered actions.
- Modify `web/src/pages/VPSDetailPage.test.tsx`
  - Strengthen top-scoped assertions for monitoring attention.
  - Add subscription-load-failure test proving it appears in current judgement and not in `VPS 操作反馈`.
  - Add multi-attention page test if existing fixtures can cover it cheaply.
- Modify `web/src/index.css` only if attention list needs minor compact-layout adjustment after browser sanity.

## Ordered Steps

1. Update model tests first.
   - Expect `model` not to have a `contextAction` property.
   - Assert all attention states through `model.judgement.attentionItems`.
2. Run focused model tests and confirm they fail before implementation.
   - `cd web && npm run test -- --run src/pages/vps-detail/vpsDetailOverviewModel.test.ts`
3. Remove model `contextAction` implementation.
4. Run focused model tests and confirm they pass.
5. Update page tests for feedback-stack and multi-attention behavior.
6. Run focused page tests and confirm failures before page implementation if possible.
7. Remove persistent subscription-load error from middle `pageFeedbackCandidates`.
8. Run focused VPS detail tests.
   - `cd web && npm run test -- --run src/pages/vps-detail/vpsDetailOverviewModel.test.ts src/pages/VPSDetailPage.test.tsx`
9. Run `make verify-web`.
10. Run browser sanity against local preview/mock route:
    - desktop `1440x1000`;
    - mobile `390x900`;
    - confirm current judgement top placement and no middle attention strip.
11. Review diff for:
    - no backend/API change;
    - no new verbose page text;
    - attention actions still route/open modals;
    - no lingering `contextAction` references.

## Risk Points

- Page tests may have duplicate text from related overview. Scope assertions to `aria-label="当前判断"` and `aria-label="VPS 操作反馈"` rather than using global text matches.
- Removing `state.subscriptionsError` from the middle feedback stack means the only visible error path for subscription load failure is top current judgement and related overview. That matches this task, but tests must prove the top path remains visible.
- If TypeScript consumers still reference `contextAction`, `tsc` will expose them during `make verify-web`.

## Review Gate Before Implementation

This plan intentionally does not ask the user to choose between design alternatives because the current task is a bug fix against an already approved direction. The self-review checklist before `task.py start`:

- The root cause is source-backed: `contextAction` still exists and tests still assert it.
- The fix removes the old data source rather than only hiding UI.
- Multiple simultaneous attention states remain supported.
- Middle feedback still exists for short-lived operation results, not persistent VPS state.
