# Research: VPS Detail browser sanity

- **Query**: Browser sanity check for the VPS Detail page after the information architecture refactor.
- **Scope**: internal / browser sanity
- **Date**: 2026-05-19

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/VPSDetailPage.tsx` | VPS Detail page orchestration; loads VPS detail/timeline/services/domains/subscription data, renders hero, decision workbench, operations summary, lifecycle confirmation, and drawer content. |
| `web/src/pages/vps-detail/VPSDetailHero.tsx` | Top hero and VPS actions menu. |
| `web/src/pages/vps-detail/VPSDecisionWorkbench.tsx` | Judgment-focused decision workbench rendered immediately after hero. |
| `web/src/pages/vps-detail/VPSOperationsSummary.tsx` | Compact operations evidence summary cards and detail-entry buttons. |
| `web/src/pages/VPSDetailPage.test.tsx` | Existing unit/integration coverage for compact default view, actions menu, and drawers. |

### Browser Sanity Procedure

- Started Vite dev server with `npm --prefix /Users/weibo/Code/houfeng/web run dev -- --host 127.0.0.1`.
- No local center was listening on `127.0.0.1:8080` or `127.0.0.1:16001`, so real authenticated backend/data inspection was not available.
- Used the running Chrome DevTools endpoint on `127.0.0.1:9222` to open `http://127.0.0.1:5173/vps/vps_001`.
- Injected deterministic browser-side fetch fixtures before page load so auth (`/api/auth/me`) and VPS detail API calls could resolve in the real Vite/React UI.
- Viewport used for inspection: 1440 × 1100.

### Code Patterns

- `web/src/pages/VPSDetailPage.tsx:879-951` renders the shortened default IA in this order: `VPSDetailHero`, `VPSDecisionWorkbench`, optional feedback, and `VPSOperationsSummary`; the full legacy sections are only available through drawer modes.
- `web/src/pages/VPSDetailPage.tsx:714-727` maps drawer modes to titles: `续费决策`, `编辑基础信息`, `关联 Node`, `经验记录`, `新增服务`, `新增域名`, `Node 观测证据`, `服务资产详情`, `域名资产详情`, `资产历史详情`, `基础资料详情`.
- `web/src/pages/VPSDetailPage.tsx:831-874` renders full detail content only when a drawer mode is active: Node evidence, services, domains, timeline, and facts.
- `web/src/pages/vps-detail/VPSDetailHero.tsx:52-79` exposes the primary decision button (`处理决策`) and menu actions: `编辑基础信息`, `记录经验`, `关联 Node`, `新增服务`, `新增域名`, and `归档 VPS` or `恢复为闲置` depending on lifecycle status.
- `web/src/pages/VPSDetailPage.test.tsx:287-324` asserts the compact default IA and verifies that old section headings are not present in the default render.
- `web/src/pages/VPSDetailPage.test.tsx:326-363` asserts the Node evidence, services, domains, timeline, and facts drawers open from the compact summary.

### Visual / Runtime Observations

- Default view loaded with hero heading `Tokyo Edge` and hero metadata (`Hetzner · JP · Kanto · Tokyo`, lifecycle/usage/renewal badges, `1 个 Node`).
- The primary CTA `处理决策` was visible in the hero; an actions menu with `aria-label="VPS 详情操作"` was present top-right.
- The page showed judgment-focused content by default:
  - `资产判断`
  - `下一步动作`
  - `判断证据摘要`
  - operations summary cards for `续费窗口`, `Node 证据`, `服务与域名`, `最近历史`, and `资料摘要`
- Default page measurement in Chrome at 1440 × 1100: `.vps-detail-page` bounding height was about 1333 px; the rendered `OPERATIONS CONTEXT` summary began after hero/workbench at about y=1010 and contained compact evidence cards rather than full tables.
- The old flat, full-content sections were not expanded by default. The default DOM did not show full headings for:
  - `续费与成本证据`
  - `决策依据与经验记录`
  - `Node 观测证据`
  - `基础信息`
  - `服务资产`
  - `域名资产`
  - `访问摘要`
- The phrase `资产历史` appeared only as part of the compact button label `查看资产历史`; the full `资产历史` section was not visible until opening the timeline drawer.

### Actions Menu Check

Top-right VPS actions menu opened successfully and exposed:

| Expected action | Observed label |
|---|---|
| edit facts | `编辑基础信息` |
| record experience | `记录经验` |
| link Node | `关联 Node` |
| add service | `新增服务` |
| add domain | `新增域名` |
| archive/restore | `归档 VPS` for active fixture; code/test also cover `恢复为闲置` for archived fixture |

### Drawer / Detail Entry Check

With fixture data, these entries opened visually in Chrome as right-side dialogs:

| Trigger | Dialog observed | Visual content observed |
|---|---|---|
| `查看 Node 详情` | `Node 观测证据` | Node evidence heading, linked Node `Tokyo Node`, status/incident summary, and unlink action. |
| `服务详情` | `服务资产详情` | `服务资产` table/content, service `Blog`, URL `https://blog.example.com`, target `tg_001`. |
| `域名详情` | `域名资产详情` | `域名资产` table/content, domain `www.example.com`, registrar `NameSilo`, expiry `2026-07-01`. |
| `查看资产历史` | `资产历史详情` | Full timeline with renewal decision, price change, IP change, spec snapshot, and experience log. |
| `查看基础资料` | `基础资料详情` | Full facts/details content including VPS ID, provider ID, product, datacenter, IP, SSH, OS, virtualization, note. |
| `编辑基础信息` | `编辑基础信息` | Fact edit form opened from actions menu. |
| `记录经验` | `经验记录` | Experience-log form opened from actions menu. |
| `关联 Node` | `关联 Node` | Node-link form opened from actions menu. |
| `新增服务` | `新增服务` | Service-create form opened from actions menu. |
| `新增域名` | `新增域名` | Domain-create form opened from actions menu. |

### Related Specs

| File Path | Description |
|---|---|
| `.trellis/spec/web/component-conventions.md` | Component organization and colocated tests conventions for web pages/components. |
| `.trellis/spec/web/styling-guidelines.md` | Web styling/design-token guidance relevant to page visual structure. |
| `.trellis/spec/web/quality-guidelines.md` | Web verification expectations. |
| `.trellis/spec/guides/cross-layer-thinking-guide.md` | Cross-layer behavior/data-flow thinking guide. |

### External References

No external references were needed.

## Caveats / Not Found

- Real backend/auth/data could not be verified because neither `127.0.0.1:8080` nor `127.0.0.1:16001` had a running center during this sanity check.
- The browser run used mocked fetch responses injected into Chrome; it verified the real Vite/React UI rendering and interactions, but not live API data, session cookies, database state, or backend authorization.
- Chrome DevTools MCP was not exposed as a named tool in this agent environment, so inspection used the available local Chrome DevTools Protocol endpoint directly via `127.0.0.1:9222`.
- The running Vite server proxies `/api` to `127.0.0.1:8080`; without the injected fixtures, the VPS detail route would redirect/fail due to missing auth/backend.
