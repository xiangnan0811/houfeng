# Asset frontend pages

## Goal

Build the first usable VPS Asset Ledger frontend slice so operators can create and inspect providers, VPS assets, VPS detail, and subscriptions from the SPA. This turns the backend delivered in Tasks 1-5 into visible operational workflows while keeping Fleet Observability routes as first-class navigation entries.

## What I already know

- The root plan defines Task 6 as `资产前端页面`.
- Tasks 1-5 delivered provider, VPS asset, subscription, JSON dry-run/import, and VPS-to-Node link backend APIs.
- The frontend is a React 19 + TypeScript + Vite SPA under `web/`.
- Business API requests must go through `web/src/lib/api.ts`.
- Shared contract types must live in `web/src/lib/types.ts` and keep backend JSON snake_case.
- New pages live under `web/src/pages/` with colocated tests.
- Routes are registered only through `web/src/app/router.tsx`.
- Sidebar navigation comes from `web/src/app/metadata.ts` and `web/src/app/layout/Sidebar.tsx`.
- Existing page patterns use local `useState` / `useEffect`, `ApiError`, loading/error/data states, URL search params for filters, and no third-party data-fetching library.
- The task must be implemented directly in the main session; the user explicitly requested no subagents.

## Scope

- Add asset ledger types to `web/src/lib/types.ts`:
  - `ProviderRecord`, `CreateProviderInput`
  - `VPSAssetRecord`, `CreateVPSAssetInput`, list filter type
  - `SubscriptionRecord`, `CreateSubscriptionInput`, list filter type
  - VPS/Node link summary types needed by VPS detail
  - centralized status label maps for VPS lifecycle, VPS usage, renewal decision, and subscription status
- Add asset API helpers to `web/src/lib/api.ts`:
  - providers list/create/get
  - VPS list/create/get
  - subscriptions list/create/get
  - VPS detail Node summary load via the existing VPS detail response or `/api/vps/{id}/nodes`
- Add route pages:
  - `web/src/pages/ProvidersPage.tsx`
  - `web/src/pages/VPSPage.tsx`
  - `web/src/pages/VPSDetailPage.tsx`
  - `web/src/pages/SubscriptionsPage.tsx`
- Register routes and navigation:
  - `/providers`
  - `/vps`
  - `/vps/:vpsId`
  - `/subscriptions`
  - Keep existing 首页 / 节点 / 目标 / 事件 / 设置 entries visible.
- Implement focused first-version workflows:
  - create and view providers
  - create, filter, and view VPS assets
  - open VPS detail and inspect infrastructure fields plus linked Node monitoring summaries
  - create and view subscriptions, including backend-computed monthly price
- Use Chinese UI labels while sending stable machine values to the API.
- Add focused tests for API helpers, routes/nav, and all four new pages.

## Requirements

- Pages must not call `fetch` directly.
- Page and component code must import API helpers from `web/src/lib/api.ts`.
- API and response field names must remain snake_case to match backend JSON.
- Status labels must be centralized, not copied independently inside each page.
- The VPS list should prioritize decision fields:
  - name
  - provider
  - region
  - lifecycle status
  - usage status
  - renewal decision
  - active Node link count / monitoring summary
  - labels
- VPS detail should show the lower-priority infrastructure fields:
  - IP / SSH / OS / virtualization / datacenter / order reference
  - provider context
  - lifecycle / usage / renewal decision
  - linked Node monitoring summaries
- Subscriptions should display:
  - VPS ID
  - price / currency
  - billing months / billing cycle
  - monthly price
  - renew date
  - auto-renew flags
  - status
- Create forms should be compact, inline with existing page style, and rely on backend validation for final authority.
- The frontend may apply light client-side checks for obvious required fields to avoid sending empty names or IDs.
- UI must remain a dense operational tool, not a landing page or marketing screen.
- New CSS must go into `web/src/styles/pages.css` using BEM classes and existing tokens.

## Acceptance Criteria

- [x] `/providers` loads providers through `listProviders()` and can create a provider through `createProvider()`.
- [x] `/vps` loads VPS assets through `listVPSAssets()`, supports provider/lifecycle/usage/renewal filters, and can create a VPS.
- [x] Clicking a VPS row opens `/vps/{vps_id}`.
- [x] `/vps/{vps_id}` loads the VPS detail and shows linked Node monitoring summaries when present.
- [x] `/subscriptions` loads subscriptions through `listSubscriptions()` and can create a subscription.
- [x] Status UI displays Chinese labels while API payloads keep machine values.
- [x] No new page or component uses direct `fetch`.
- [x] Navigation includes VPS, 服务商, and 订阅 without removing 节点, 目标, 事件, 设置.
- [x] Each new route page has a colocated test covering happy-path render and at least one key interaction.
- [x] `web/src/lib/api.test.ts` covers new API helper paths and request bodies.
- [x] `git diff --check`, frontend focused tests, and `make verify-web` pass.

## Out of Scope

- Dashboard asset summary cards (Task 7).
- Backend API changes.
- Editing or deleting existing providers, VPS assets, or subscriptions.
- Provider API auto-sync.
- JSON import creating links.
- Node lifecycle / monitoring / health actions from asset pages.
- Web SSH, remote commands, or Agent changes.
- New state-management or data-fetching libraries.
- Visual screenshot automation.

## Technical Notes

- Relevant plan sections:
  - `houfeng_codex_下一步开发计划.md` section 6
  - `houfeng_codex_下一步开发计划.md` Task 6
- Relevant specs:
  - `.trellis/spec/web/index.md`
  - `.trellis/spec/web/directory-structure.md`
  - `.trellis/spec/web/component-conventions.md`
  - `.trellis/spec/web/state-and-data.md`
  - `.trellis/spec/web/styling-guidelines.md`
  - `.trellis/spec/web/quality-guidelines.md`
  - `.trellis/spec/guides/branch-workflow-governance.md`
  - `.trellis/spec/guides/cross-layer-thinking-guide.md`
- Existing frontend patterns inspected:
  - `web/src/app/router.tsx`
  - `web/src/app/layout/Sidebar.tsx`
  - `web/src/app/metadata.ts`
  - `web/src/lib/api.ts`
  - `web/src/lib/types.ts`
  - `web/src/pages/EventsPage.tsx`
  - `web/src/pages/TargetsPage.tsx`
  - `web/src/components/atoms/DataTable.tsx`
  - `web/src/components/filters/*`
- Backend contracts inspected:
  - `internal/center/providers/types.go`
  - `internal/center/vpsassets/types.go`
  - `internal/center/subscriptions/types.go`
  - `internal/center/assetlinks/types.go`

## Verification Plan

```bash
git diff --check
cd web && npm run lint
cd web && npm run test -- --run ProvidersPage VPSPage VPSDetailPage SubscriptionsPage api AppShell Sidebar
cd web && npm run build
make verify-web
```

## Verification Results

- `git diff --check`: passed.
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run lint`: passed.
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run test -- --run ProvidersPage VPSPage VPSDetailPage SubscriptionsPage api Sidebar AppShell format`: 8 files passed, 50 tests passed.
- `cd web && TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm run build`: passed. Vite reported the existing chunk-size warning.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp make verify-web`: passed. 59 files passed, 424 tests passed, build passed. npm reported the local Node v24 vs required Node 22 engine warning; this did not fail the target and CI uses Node 22.
