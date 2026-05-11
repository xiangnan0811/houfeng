# Current VPS detail audit

## Files Reviewed

- `web/src/pages/VPSDetailPage.tsx`
- `web/src/pages/vps-detail/*`
- `web/src/components/VPSTimelinePanel.tsx`
- `web/src/pages/VPSDetailPage.test.tsx`
- `web/src/pages/assetPageUtils.ts`
- `web/src/lib/api.ts`
- `docs/release/core-pages-product-ux-replan.md`

## Current Behavior

`VPSDetailPage` loads the VPS detail, timeline, VPS-scoped service records, and VPS-scoped domain records. It renders:

1. hero,
2. always-visible operation panel,
3. basic facts with inline edit form,
4. linked Node table,
5. service table plus always-visible creation form,
6. domain table plus always-visible creation form,
7. timeline,
8. access summary.

The page is functionally complete, but the top scan path is form-heavy. The user sees operation forms before they see the full asset judgment context.

## Useful Existing Contracts

- `VPSAssetDetail.node_links` already carries enough evidence for Node status: health, monitoring status, binding status, active incident count, primary issue summary, heartbeat, and sync time.
- `VPSTimeline` already carries decision history, price history, IP history, spec snapshots, and experience logs.
- `listSubscriptions` supports `vps_id`, `sort`, and `order`, so the detail page can join current subscription/cost data without backend changes.
- `assetPageUtils` already has reusable helpers for renewal labels, subscription grouping/selection, renewal timing, and quality issues.
- `Drawer` already exists as an accessible secondary surface and is used by other pages.

## UX Problems

1. `VPSOperationsPanel` gives four forms equal priority above facts and evidence, making the page feel like an admin form collection instead of an asset judgment page.
2. Renewal/cost is represented only indirectly through timeline price changes; the page does not fetch current subscriptions.
3. Service and domain creation forms are always visible beside their tables, which adds clutter even when the user only wants to inspect the asset.
4. Fact editing expands a large form in place and interrupts the read path.
5. Lifecycle danger actions are visually close to routine edits because they live inside the same operation grid.

## Recommendation

- Replace the operation-first stack with a judgment workbench directly after the hero.
- Move routine creation/edit forms into Drawers launched from section actions.
- Keep lifecycle archive/restore in an isolated lifecycle section with confirmation.
- Fetch subscriptions for this VPS and show current cost/renewal state with explicit missing-data wording.
- Keep all existing APIs and domain behavior; this is a frontend information architecture change, not a backend expansion.
