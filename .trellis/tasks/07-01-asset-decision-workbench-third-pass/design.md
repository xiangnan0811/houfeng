# Design

## Boundary

This is a frontend IA refactor inside the existing asset decisions page. It does not change backend contracts, API routes, storage, or route registration.

Primary files:

- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/pages/AssetDecisionsPage.test.tsx`
- `web/src/index.css`

## Information Architecture

The default detail layer becomes a true decision cover:

- one semantic cover section (`aria-label` identifies current judgment);
- one concise judgment sentence;
- compact status/risk metadata;
- one primary user action;
- one secondary affordance to enter details.

Detailed functions stay available after explicit expansion:

- automatic group: members, save record, raw data, selected VPS panel;
- manual group: members, edit, add member, save record, raw data;
- saved record: execution, member follow-up, source review, raw data.

Implementation uses existing local state (`groupDetailPanel`, `manualDetailPanel`, `recordDetailPanel`) but treats `overview` as the cover-only default. The detail navigation is rendered only after the user enters a non-overview panel or clicks the detail affordance. This prevents the default viewport from showing a tab row that visually competes with the main decision.

## Component Shape

Keep helpers in `AssetDecisionsPage.tsx` because the page is already a large integrated route and the change is narrowly scoped.

Add/adjust helpers:

- `renderDecisionCover(...)`: a stricter default cover helper for automatic groups, manual groups, and records.
- `renderDetailEntry(...)` or inline detail buttons: a quiet secondary action that sets the first useful detail panel.
- Simplify `renderDecisionGroupCards(...)`: remove nested metric cards and reduce each group card to rank, title, judgment, compact chips, and action.

Do not introduce a new design system or dependency.

## Styling

Use existing BEM classes in `web/src/index.css`. New classes stay under the `asset-decision-*` namespace and use existing CSS variables.

Expected visual changes:

- Default modal content has fewer bordered boxes and less grid fragmentation.
- Detail panels remain bordered and dense because they are intentionally secondary.
- Group cards are shorter, scannable list rows instead of mini reports.
- Mobile layout uses one-column cover/actions and keeps document/body overflow-x hidden.

## Compatibility

No API shape changes. Existing deep links still open the same modal. Existing detail actions should continue to work after the user opens details.

## Risks

- Some tests currently click panel nav buttons immediately after opening a modal; update tests to click the new detail affordance first.
- Hiding nav on the default layer must not hide form actions once a panel is opened.
- Browser visual checks must verify the real rendered page, because text-count-only checks failed to catch prior visual clutter.
