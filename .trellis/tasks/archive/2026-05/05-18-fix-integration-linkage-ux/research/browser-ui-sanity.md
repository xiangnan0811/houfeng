# Research: Browser UI sanity for integration/linkage UX

- **Query**: Perform a browser sanity check for the Houfeng integration/linkage UX changes using the running dev server at `http://127.0.0.1:5173/`.
- **Scope**: internal / browser runtime
- **Date**: 2026-05-18

## Findings

### Browser / data setup

- Vite dev server responded at `http://127.0.0.1:5173/`.
- The center API was not running on the Vite proxy target: `GET /api/auth/me` through Vite returned `502 Bad Gateway` before browser stubbing.
- Browser checks were performed in Google Chrome headless via Chrome DevTools Protocol with `Fetch` API stubs for `/api/*`, including an authenticated `/api/auth/me` response and representative VPS, Provider, Subscription, Node, Target, Service, Domain, and timeline records.
- No runtime exceptions, browser console warnings/errors, or network loading failures were captured during the completed browser checks.

### Files Found

| File Path | Description |
|---|---|
| `web/src/app/router.tsx` | Defines protected routes for `/vps`, `/vps/:vpsId`, `/subscriptions`, `/asset-decisions`, and `/nodes/:nodeId`. |
| `web/src/pages/VPSPage.tsx` | VPS inventory page and VPS create drawer, including provider selector and empty state copy. |
| `web/src/pages/VPSDetailPage.tsx` | VPS detail route and drawers for renewal decision, facts, Node link, service creation, and domain creation. |
| `web/src/pages/SubscriptionsPage.tsx` | Subscription list/create page, URL context handling for `vps_id` and `create=1`. |
| `web/src/pages/AssetDecisionsPage.tsx` | Asset decision queue, renewal evidence table, and decision notice behavior. |
| `web/src/pages/node-detail/NodeLinkedVPSSection.tsx` | Node detail linked-VPS section and empty-state action to `/vps?view=unlinked`. |

### Route Checks

#### `/vps`

Verified with empty VPS/provider data:

- Page rendered without an obvious runtime crash.
- Empty inventory copy was visible: `还没有录入 VPS 资产` and an action to `录入第一台 VPS`.
- `VPS 创建表单` drawer opened from `创建第一台 VPS`.
- Create drawer provider selector was accessible as `资产服务商`.
- With no providers, the selector contained the option `未关联服务商` and the hint copy said providers can be created first or a snapshot name can be kept.
- The provider hint included a visible `服务商列表` link to `/providers`.

#### `/vps/vps_001`

Verified with mock detail data:

- Detail route rendered `Tokyo Edge` without an obvious runtime crash.
- Renewal decision drawer opened from `调整决策`; selector offered `未评估`, `保留`, `观察`, `迁移`, `取消`, `已取消自动续费`, `已替换`.
- Facts drawer opened from `编辑基础信息`; `资产服务商` selector showed provider records (`Hetzner · DE · pv_001`, `Vultr · US · pv_002`) and `用途状态` selector was present.
- Node link drawer opened from `关联 Node`; `选择 Node` selector showed available unlinked Node options (`Seoul Node · nd_002 ...`, `Unlinked Runtime Node · nd_unlinked ...`).
- Service drawer opened from `新增服务`; `服务类型`, `服务状态`, and `关联 Target` selectors were visible, with `Blog Target · tg_001 · blog.example.com · 启用` as an option.
- Domain drawer opened from `新增域名`; `域名状态`, `关联服务`, and `关联 Target` selectors were visible. The mocked service option rendered as `Blog · svc_001 · active` in the browser text extraction.

#### `/subscriptions?vps_id=vps_001&create=1`

Verified with mock VPS/subscription data:

- Context panel `当前 VPS 上下文` was visible and referenced `Tokyo Edge`.
- Create form opened automatically from the URL query.
- `订阅 VPS` select was prefilled to `vps_001` and showed `Tokyo Edge` among options.
- Closing via `收起创建` removed `create=1` while retaining `vps_id=vps_001`: URL changed from `/subscriptions?vps_id=vps_001&create=1` to `/subscriptions?vps_id=vps_001`.

#### `/asset-decisions`

Verified with mock queue and renewal data:

- Asset decision queue rendered without an obvious runtime crash.
- Renewal evidence avoided raw-ID-only display where mock VPS data existed: the renewal evidence link displayed `Tokyo Review` for `vps_review`.
- Decision drawer opened for `处理 vps_review`.
- Changing the decision to `取消` and saving produced a visible status notice: `续费决策已保存：Tokyo Review -> 取消`.
- The same notice area included the mocked linkage message: `已同步取消主订阅的自动续费状态。`

#### `/nodes/nd_unlinked`

Partially verified with mock Node data:

- Node detail route rendered `Unlinked Runtime Node` without an obvious runtime crash.
- `关联 VPS` section was visible.
- Browser issued `GET /api/nodes/nd_unlinked/vps`, and the stub returned an empty array.
- The section remained visually in `正在加载关联 VPS…` / `加载中` state during this browser run, so the empty-state copy `尚未关联 VPS` and the action link `去 VPS 库存选择并关联` to `/vps?view=unlinked` were not visually verified in the browser.
- The code path for that action exists in `web/src/pages/node-detail/NodeLinkedVPSSection.tsx:83-89`, where the link is shown when `!loading && loaded && records.length === 0`.

### API Requests Observed During Browser Run

- `GET /api/auth/me`
- `GET /api/dashboard`
- `GET /api/vps`
- `GET /api/providers`
- `GET /api/subscriptions?sort=renew_at&order=asc`
- `GET /api/vps/vps_001`
- `GET /api/vps/vps_001/timeline`
- `GET /api/vps/vps_001/services`
- `GET /api/vps/vps_001/domains`
- `GET /api/subscriptions?vps_id=vps_001&sort=renew_at&order=asc`
- `GET /api/nodes`
- `GET /api/targets`
- `GET /api/subscriptions?vps_id=vps_001`
- `GET /api/subscriptions?renew_within_days=30&sort=renew_at&order=asc`
- `GET /api/vps?renewal_decision=unreviewed`
- `GET /api/vps?renewal_decision=migrate`
- `GET /api/vps?renewal_decision=cancel`
- `PATCH /api/vps/vps_review`
- `GET /api/nodes/nd_unlinked`
- `GET /api/nodes/nd_unlinked/runtime-facts?window=24h`
- `GET /api/incidents?object_type=node&object_id=nd_unlinked`
- `GET /api/events?object_type=node&object_id=nd_unlinked`
- `GET /api/nodes/nd_unlinked/vps`

### Related Specs

- No `.trellis/spec` files were required for this browser sanity pass.

### External References

- None.

## Caveats / Not Found

- The real center backend was not available through the Vite proxy; results above use browser-level API stubs rather than real persisted/mock backend data.
- Chrome DevTools MCP was not exposed as a tool in this session; browser automation used Chrome DevTools Protocol directly against local Chrome remote debugging.
- `/vps/<id>` used `vps_001` from the browser stub data, not a backend-provided mock ID.
- `/nodes/<id>` used `nd_unlinked` from the browser stub data. The linked-VPS empty action did not visually settle from loading to empty state during the browser run despite the stubbed empty response; only the source code path for the action was confirmed.
