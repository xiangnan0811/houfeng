# Research: Browser sanity for workbench IA changes

- **Query**: Perform browser sanity for current worktree frontend workbench IA changes across Dashboard (/), VPS (/vps), Nodes (/nodes), Targets (/targets), and Asset Decisions (/asset-decisions); verify CompactHeader/workbench-main/workbench-aside/table-workbench, primary tables/queues, no obvious runtime errors, and no continuous stack of large page-panel sections above the fold.
- **Scope**: internal browser/DOM sanity
- **Date**: 2026-05-22

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/components/CompactHeader.tsx` | Shared compact top-level page header (`.compact-header`). |
| `web/src/components/WorkbenchPage.tsx` | Shared layout wrapper rendering `.workbench-main` and optional `.workbench-aside`. |
| `web/src/components/ListWorkbench.tsx` | Shared list/table workbench wrapper rendering `.table-workbench`. |
| `web/src/pages/dashboard/DashboardCommandSurface.tsx` | Dashboard workbench composition with CompactHeader, ListWorkbench, and aside context. |
| `web/src/pages/VPSPage.tsx` | VPS page workbench composition with inventory DataTable primary. |
| `web/src/pages/NodesPage.tsx` | Nodes page workbench composition with NodesListSection/DataTable primary. |
| `web/src/pages/TargetsPage.tsx` | Targets page workbench composition with DataTable primary. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset Decisions page workbench composition with decision queue primary and renewal table evidence in aside details. |

### Code Patterns

- `WorkbenchPage` renders a section with `page-stack workbench-page`, then a layout containing `.workbench-main` and optional `.workbench-aside` (`web/src/components/WorkbenchPage.tsx:20-35`).
- `ListWorkbench` renders `.table-workbench`, heading/toolbar/tabs/chips/priority areas, and `.table-workbench__content` (`web/src/components/ListWorkbench.tsx:32-61`).
- `CompactHeader` renders the page `header.compact-header`, `h1`, metrics, and actions (`web/src/components/CompactHeader.tsx:31-60`).
- Dashboard uses `WorkbenchPage` + `CompactHeader` + `ListWorkbench`; primary work is attention queue, command rows, onboarding, or running overview depending on state (`web/src/pages/dashboard/DashboardCommandSurface.tsx:564-700`).
- VPS, Nodes, Targets, and Asset Decisions all compose `WorkbenchPage` with `CompactHeader` and put the main table/queue inside `ListWorkbench` (`web/src/pages/VPSPage.tsx:699-856`, `web/src/pages/NodesPage.tsx:806-1092`, `web/src/pages/TargetsPage.tsx:601-918`, `web/src/pages/AssetDecisionsPage.tsx:557-735`).

### Browser Evidence

Sanity was run against the current Vite dev server at `http://localhost:5173` using a headless Chrome session with intercepted authenticated API fixtures. Viewport: 1440 × 1100.

| Page | DOM/layout evidence | Primary table/queue evidence | Runtime evidence | Page-panel stack evidence |
|---|---|---|---|---|
| `/` Dashboard | `.compact-header`, `.workbench-main`, `.workbench-aside`, `.table-workbench` all present. | Queue-like primary present (`.dashboard-attention` / command surface); `queueCount=1`. | No captured console errors, page errors, or failed requests. | `.page-panel` above fold: `0`. |
| `/vps` | `.compact-header`, `.workbench-main`, `.workbench-aside`, `.table-workbench` all present. | Inventory table present; `tableCount=1`. | No captured console errors, page errors, or failed requests. | `.page-panel` above fold: `0`. |
| `/nodes` | `.compact-header`, `.workbench-main`, `.workbench-aside`, `.table-workbench` all present. | Nodes table present; `tableCount=1`. | No captured console errors, page errors, or failed requests. | `.page-panel` above fold: `0`. |
| `/targets` | `.compact-header`, `.workbench-main`, `.workbench-aside`, `.table-workbench` all present. | Targets table present; `tableCount=1`. | No captured console errors, page errors, or failed requests. | `.page-panel` above fold: `0`. |
| `/asset-decisions` | `.compact-header`, `.workbench-main`, `.workbench-aside`, `.table-workbench` all present. | Decision queue present (`.asset-decision-queue`); renewal evidence table also present inside aside details, so `tableCount=1`, `queueCount=1`. | No captured console errors, page errors, or failed requests. | `.page-panel` above fold: `0`. |

Observed above-fold primary positions were consistent with a compact header followed by workbench content: Dashboard primary queue top ≈ 540px, VPS table top ≈ 555px, Nodes table top ≈ 602px, Targets table top ≈ 717px, Asset Decisions queue top ≈ 467px. A global critical alert rendered above pages because the sanity fixture included one severe target; this is outside the page workbench content and did not create page-panel stacking.

### Related Specs

- Not inspected for this browser sanity pass; task scope was the current worktree UI layout behavior.

### External References

- None.

## Caveats / Not Found

- Authentication was satisfied with intercepted `/api/auth/me` fixture data in headless Chrome, not an existing real logged-in browser session, because the local center at `127.0.0.1:8080` returned unauthenticated and no credentials were requested or recorded.
- Data-dependent visual states were exercised with representative mock Node/Target/VPS/subscription/dashboard payloads, not the live local database.
- A Vite dev server was already listening on `localhost:5173`; no new long-running server process was started.

## Authenticated local sanity

- **Date**: 2026-05-22
- **Scope**: real authenticated local browser sanity against the existing Vite dev server at `http://localhost:5173` and local center at `http://127.0.0.1:8080`.
- **Browser/session**: Chrome via the running local remote-debugging endpoint, using an already-authenticated local session. The local-only admin credentials were not written, echoed, persisted, screenshotted, or reported; no credential values are recorded in this file.
- **Overall result**: PASS.

| Page | Result | Evidence |
|---|---:|---|
| `/` Dashboard | PASS | Rendered authenticated app shell with `.compact-header`, `.workbench-main`, `.workbench-aside`, `.table-workbench`; primary dashboard queue visible; no console warnings/errors, page errors, failed requests, or HTTP >=400 responses captured during this pass. |
| `/vps` | PASS | Rendered authenticated app shell with required workbench classes; VPS inventory table visible; no runtime console/page/request errors captured. |
| `/nodes` | PASS | Rendered authenticated app shell with required workbench classes; nodes table visible; no runtime console/page/request errors captured. |
| `/targets` | PASS | Rendered authenticated app shell with required workbench classes; targets table visible; no runtime console/page/request errors captured. |
| `/asset-decisions` | PASS | Rendered authenticated app shell with required workbench classes; primary workbench empty state visible (`当前窗口暂无续费候选`); no runtime console/page/request errors captured. |

### Caveats

- The browser session was already authenticated, so the login form was not exercised in this pass.
- Asset Decisions had no current renewal candidates in the live local data, so the visible primary surface was the empty-state workbench rather than populated queue rows.
- No screenshots were captured or persisted.
