# Current implementation audit — 2026-08-23

## Visible gap

- `web/src/pages/VPSDetailPage.tsx` selects the overview route when
  `records_v2_read` is present, but passes an empty `onManagePanel` callback.
- `web/src/pages/vps-detail/VPSManagementMenu.tsx` exposes `facts`, `decision`,
  `subscription`, `cancellation`, and `archive` and changes the visibility
  controller.
- `web/src/pages/vps-detail/VPSOverviewPageView.tsx` renders only a placeholder
  `PageState` for the selected panel.
- `web/src/pages/vps-detail/hooks/useVPSManagementController.ts` owns only
  menu/panel visibility, not mutation data or API calls.

## Reusable existing owners

- Facts: `VPSFactsEditForm`, `detailToFactEditForm`, `buildFactEditInput`,
  `updateVPSAsset`, provider selector loading.
- Decision: `VPSRenewalDecisionForm`, `updateVPSAsset`, renewal-subscription
  linkage copy/action and detail/timeline refresh.
- Subscription: `VPSSubscriptionForm`, `INITIAL_SUBSCRIPTION_DRAFT`,
  `buildSubscriptionInput`, `createVPSSubscription`.
- Cancellation: `getVPSCancellationPreview`, `VPSCancellationWorkbench`,
  `applyVPSCancellation`, result and preview refresh.
- Archive: `getVPSArchiveReview`, `ActionConfirmationModal`, eligible/blockers,
  exact display-name confirmation, `archiveVPS`, `/archive/:id` navigation.

These owners currently live in or are orchestrated by
`web/src/pages/vps-detail/LegacyVPSDetail.tsx`. The new overview must reuse the
forms and small pure helpers without importing/mounting the entire legacy page or
duplicating lifecycle safety rules.

## Existing test seams

- `web/src/pages/VPSDetailPage.test.tsx` covers overview/legacy gate behavior and
  the no-duplicate overview seed fetch.
- `web/src/pages/vps-detail/VPSOverviewPageView.test.tsx` covers the overview
  composition but not management actions.
- `web/src/pages/vps-detail/hooks/useVPSManagementController.test.tsx` covers
  visibility only.
- `web/src/pages/vps-detail/LegacyVPSDetail.test.tsx` has the current mutation,
  cancellation and archive regression corpus.
- `web/src/components/VPSCancellationWorkbench.test.tsx` protects workbench
  validation and blocker behavior.

## Constraints confirmed by current code

- Overview identity is a reduced read model; forms requiring `VPSAssetDetail`
  must load it from the existing asset API.
- Overview refresh is owned by `useVPSOverview().commands.refresh`.
- Legacy is a lazy route chunk and must remain absent from the overview first
  paint graph.
- Existing modal primitives should own focus trap/return; the new shell must add
  tests for menu Escape/focus and 390px behavior.
