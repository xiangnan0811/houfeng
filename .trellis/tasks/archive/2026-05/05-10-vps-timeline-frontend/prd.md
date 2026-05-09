# VPS timeline frontend

## Goal

Expose the already implemented Asset Ledger timeline endpoint in the VPS detail UI so operators can inspect renewal decision, price, IP, and spec history from the same place where they inspect current VPS facts and Node monitoring links.

## What I Already Know

- `houfeng_codex_下一步开发计划.md` recommended Task 8 to make history and decision capabilities usable after the core Asset Ledger slices are stable.
- The repository has completed the original Asset Ledger backbone: providers, VPS assets, subscriptions, JSON dry-run/import, VPS Node links, asset frontend pages, Dashboard asset summary, renewal decision history, and asset history snapshots.
- Backend endpoint `GET /api/vps/{vps_id}/timeline` already exists and returns `renewal_decisions`, `price_histories`, `ip_histories`, and `spec_snapshots`.
- `web/src/pages/VPSDetailPage.tsx` currently loads only `getVPSAsset(vpsId)` and renders current facts, active Node links, and connection summary.
- `web/src/lib/types.ts` and `web/src/lib/api.ts` do not yet expose a typed VPS timeline client.
- This session must not use subagents. Implementation and checks will be done directly in the main session while still following Trellis spec/context requirements.

## Requirements

- Add TypeScript types for the VPS timeline response, preserving backend snake_case field names and nullability.
- Add `getVPSTimeline(vpsId)` in `web/src/lib/api.ts`.
- Update `VPSDetailPage` to load VPS detail and timeline for the current `vpsId`.
- Render a VPS history/timeline section on the detail page with four inspectable groups:
  - renewal decision history
  - price history
  - IP history
  - spec snapshots
- Keep current VPS detail behavior intact: missing `vpsId`, loading, error, basic facts, Node links, and connection summary still work.
- Show empty states when each history group has no entries.
- Treat timeline load failure as a page-level error for that VPS detail view, consistent with current page loading/error handling.
- Reuse existing atoms, badges, timestamp/mono helpers, formatters, and global CSS patterns. Do not add a new CSS file or direct `fetch`.

## Acceptance Criteria

- [x] `VPSDetailPage` calls `/api/vps/{vps_id}` and `/api/vps/{vps_id}/timeline` through `web/src/lib/api.ts`.
- [x] Timeline UI shows all four backend arrays with user-readable labels and values.
- [x] Renewal decision labels use the existing Chinese status mapping while API values remain stable English machine values.
- [x] Price changes show original and updated amount, currency, monthly price, billing months/cycle, renewal date, auto-renew flags, and subscription status.
- [x] IP changes show IPv4/IPv6 before/after values.
- [x] Spec snapshots show captured product/access/OS/virtualization values.
- [x] Empty timeline groups render a compact empty state instead of disappearing silently.
- [x] Existing `VPSDetailPage` test is extended to cover timeline fetch/rendering.
- [x] `git diff --check`, focused Vitest, `npm run lint`, `npm run build`, and `make verify-web` pass before commit.

## Out Of Scope

- No backend schema, handler, router, migration, or store changes.
- No new history write path.
- No Dashboard changes.
- No JSON import changes.
- No standalone decision page or editing workflow.
- No Node/Target/Agent semantics changes.

## Technical Notes

- Backend contract source: `internal/center/renewals/types.go`.
- Frontend data entrypoints: `web/src/lib/types.ts`, `web/src/lib/api.ts`.
- UI entrypoint: `web/src/pages/VPSDetailPage.tsx` and colocated test.
- Style entrypoint: `web/src/styles/pages.css` under existing `asset-page` blocks.
- Relevant specs are curated into `implement.jsonl` and `check.jsonl`.
