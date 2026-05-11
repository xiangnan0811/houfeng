# Current asset pages audit

## Scope

Reviewed current UX-3 targets and supporting contracts:

- `docs/release/core-pages-product-ux-replan.md`
- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/pages/VPSPage.tsx`
- `web/src/components/AssetDecisionVPSQueueTable.tsx`
- `web/src/components/AssetDecisionRenewalTable.tsx`
- `web/src/components/AssetDecisionWorkPanel.tsx`
- `web/src/lib/types.ts`
- `web/src/lib/api.ts`
- `.trellis/spec/web/*`

## Findings

### AssetDecisionsPage

- The page fetches subscriptions for a renewal window and three separate VPS lists for `unreviewed`, `migrate`, and `cancel`.
- The visible structure is still a metrics strip, a renewal table, then three queue tables plus a sticky side panel.
- This is functionally correct but visually reads as several backend slices rather than one work queue.
- Decision editing is a persistent side panel. It consumes attention even before a user selects a VPS, which conflicts with the UX-3 goal that the queue should be the product center.

### VPSPage

- The page fetches VPS assets and providers, then renders a normal DataTable with identity, provider/location, statuses, node count and labels.
- Filters are always visible through `FilterBar` selects. This is clear but too heavy for a 40+ VPS review workflow where rows should dominate.
- The page does not fetch subscriptions, so it cannot show renewal date, monthly cost, missing subscription, or auto-renew facts in the list.
- It only shows Node link count; no linked node health exists in `VPSAssetRecord`, so UX should avoid promising health here.

## Implementation Recommendation

1. Keep backend contracts unchanged and enrich page rows client-side with existing `listSubscriptions()`.
2. Replace the three decision VPS tables with one ranked work queue. Use tabs/quick filters for `全部`, `待评估`, `迁移`, `取消`, `缺关联`, and keep renewal candidates as supporting evidence.
3. Move decision editing into `Drawer` so the queue remains scannable.
4. Convert VPS filters to quick views + active chips + drawer. Keep URL query state as the source of truth, but use client-side filtering because the quick views include derived data such as `missing_subscription` and `unlinked`.
5. Extend tests around visible product behavior rather than implementation details: queue rank, drawer editing, quick views, active chips, row enrichment and create flow.

## Constraints

- No subagents are used for this task per user instruction.
- No real-data import or provider/DNS sync.
- No invented linked node health. Only show linked count / missing link unless a backend contract exists.
- CSS stays in `web/src/styles/pages.css` with token/BEM style.
