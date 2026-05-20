# Research: Target Detail browser sanity

- **Query**: Perform browser sanity for the changed Target Detail page IA: default ProbeItem evidence/list visibility, ProbeItem create/edit Drawer behavior, and unchanged metadata/lifecycle/runtime/history/danger surfaces.
- **Scope**: internal browser sanity with fixture/mock API
- **Date**: 2026-05-20

## Setup

- Preview server: `http://127.0.0.1:5178/`
- Route checked: `/targets/target_api_core`
- Viewports checked: `1440x1000`, `390x900`
- Browser tooling: local Python Playwright via `/opt/homebrew/opt/python@3.11/bin/python3.11`
- Data source: in-browser fixture routes for Target Detail only; no local center/auth backend was used.
- Screenshots: none created.

## Commands

```bash
npm --prefix /Users/weibo/Code/houfeng/web run dev -- --host 127.0.0.1 --port 5178
```

```bash
TMPDIR="/Users/weibo/Code/houfeng/.tmp/playwright" /opt/homebrew/opt/python@3.11/bin/python3.11 \
  /Users/weibo/Code/houfeng/scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5178/ \
  --mock-api observability-support \
  --route /targets/target_api_core \
  --viewport 1440x1000 \
  --viewport 390x900
```

Result:

```text
PASS /targets/target_api_core 1440x1000 text=249 doc=1440 body=1440 panels=2 mock=observability-support url=http://127.0.0.1:5178/targets/target_api_core
PASS /targets/target_api_core 390x900 text=237 doc=390 body=390 panels=2 mock=observability-support url=http://127.0.0.1:5178/targets/target_api_core
```

Because the repo helper's `observability-support` profile does not fixture Target Detail child endpoints (`GET /api/targets/:id`, `/probe-items`, `/runtime-facts`, etc.), I also ran a one-off Playwright script with route-specific fixture responses for read-only interaction checks. The script intercepted `GET /api/auth/me`, `GET /api/dashboard`, `GET /api/targets/target_api_core`, `GET /api/targets/target_api_core/probe-items`, `GET /api/targets/target_api_core/runtime-facts`, `GET /api/incidents`, and `GET /api/events`. It did not click submit/delete/runtime mutation controls.

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/TargetDetailPage.tsx` | Target Detail route container and ProbeItem create/edit/open/close state. |
| `web/src/pages/target-detail/TargetDetailPageBody.tsx` | Target Detail IA composition; renders default ProbeItem list before the secondary property list and renders the ProbeItem Drawer portal. |
| `web/src/pages/target-detail/TargetProbeListSection.tsx` | Default scan-path ProbeItem evidence section wrapper. |
| `web/src/components/target-detail/TargetProbeList.tsx` | ProbeItem cards, observations table, edit/toggle/delete row actions. |
| `web/src/pages/target-detail/TargetProbeFormDrawer.tsx` | ProbeItem create/edit Drawer wrapper around the reused form. |
| `web/src/components/target-detail/TargetProbeForm.tsx` | Reused create/edit form; heading and submit label switch by mode. |
| `web/src/pages/target-detail/TargetProbeManagementSection.tsx` | Secondary property row for opening the ProbeItem Drawer. |
| `web/src/components/atoms/Drawer.tsx` | Shared Drawer implementation used by ProbeItem form and history. |
| `docs/operations/v2-visual-evidence.md` | Active local browser-sanity workflow and screenshot policy. |

### Browser Observations

#### Default scan path

- Desktop `1440x1000`: `ProbeItem 列表` rendered in the page body with `probeInDetails: false`, `probeCardCount: 2`, headings `HTTP` and `TLS`, and no horizontal overflow (`docScrollWidth=1440`, `bodyScrollWidth=1440`).
- Mobile `390x900`: `ProbeItem 列表` rendered in the page body with `probeInDetails: false`, `probeCardCount: 2`, headings `HTTP` and `TLS`, and no horizontal overflow (`docScrollWidth=390`, `bodyScrollWidth=390`).
- The default ProbeItem evidence included latest observation text: `83 ms`, `200`, `HTTP probe timed out after 5s`, and `31 天`.
- The page still had a collapsed `标签与备注` details section, but the ProbeItem list itself was not inside a collapsed details element.

#### ProbeItem create Drawer

- Clicking `添加 ProbeItem` opened `role="dialog"` with `aria-label="ProbeItem 表单抽屉"`.
- Drawer title was `api-core.example.test · 创建 ProbeItem`.
- Drawer content included heading `创建 ProbeItem`, a `form`, fields `probeKind`, `frequencyTier`, `timeoutSeconds`, `port`, and submit button text `创建 ProbeItem`.
- Closing the Drawer with its `关闭` button returned to `/targets/target_api_core`; `ProbeItem 列表` remained visible and the ProbeItem Drawer count returned to `0`.

#### ProbeItem edit Drawer

- Clicking the row action with `aria-label^="编辑 ProbeItem pb_http_core"` opened the same `ProbeItem 表单抽屉`.
- Drawer title was `api-core.example.test · 编辑 ProbeItem`.
- Drawer content included heading `编辑 ProbeItem`, the same form surface, HTTP values populated from the fixture (`probeKindValue=http`, `pathValue=/healthz`, `methodValue=GET`, `timeoutValue=5`), and submit button text `保存 ProbeItem`.
- Closing the Drawer returned to `/targets/target_api_core`; `ProbeItem 列表`, `标签与备注`, and `生命周期` remained present and the ProbeItem Drawer count returned to `0`.

#### Other Target Detail surfaces

- Danger card was present (`当前主问题`) for the abnormal fixture.
- Runtime menu remained present; opening the `运行控制操作` menu showed `进入维护` and `暂停`.
- History remained present; clicking `查看历史` opened `role="dialog"` with `aria-label="目标历史抽屉"`, title `api-core.example.test · 历史`, `事件时间线` tab text, and a fixture event containing `HTTP 探测失败率升高`.
- Metadata summary (`标签与备注`) and lifecycle section (`生命周期`) remained present after both create Drawer close and edit Drawer close.
- No mutation requests were recorded by the fixture script (`MUTATIONS []`).

### Code Patterns

- `TargetDetailPageBody.tsx` places `<TargetProbeListSection ... />` directly after latency trends and before `<div className="watchtower-property-list">`, so ProbeItem evidence is on the default page scan path rather than nested in the secondary property list.
- `TargetProbeFormDrawer.tsx` wraps `<TargetProbeForm ... />` in `<Drawer ariaLabel="ProbeItem 表单抽屉">` and switches the title between create/edit modes.
- `TargetProbeForm.tsx` switches eyebrow, heading, and submit text based on `mode.kind`, while reusing the same form fields for create and edit.
- `TargetDetailPage.tsx` uses `openProbeCreateForm`, `openProbeEditForm`, and `closeProbeForm` to share one Drawer/form state for create and edit. `openProbeEditForm` refuses unsupported config fields with a mutation error rather than opening an unsafe edit surface.

### Related Specs

- `docs/operations/v2-visual-evidence.md` — active preview/browser-sanity workflow and no-tracked-screenshots policy.
- `.trellis/tasks/05-20-continue-page-information-architecture-optimization/prd.md` — active task PRD for page IA optimization.
- `.trellis/spec/web/` — package-level web specs directory noted by Trellis context.

## Caveats / Not Found

- No local authenticated center/backend was used. The interaction check used Playwright route interception with read-only Target Detail fixtures.
- The repo browser-sanity helper's `observability-support` profile currently proves the protected route can render without blank/overflow, but it does not provide full Target Detail child endpoint data; the route-specific one-off fixture supplied those details for the IA interaction check.
- No screenshots were created or tracked.
- The dev server was started as a background process by this research run and may still be running under the current session.
