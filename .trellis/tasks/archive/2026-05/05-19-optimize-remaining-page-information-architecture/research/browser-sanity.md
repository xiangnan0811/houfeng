# Research: Browser sanity for Subscriptions and Providers IA

- **Query**: Perform browser sanity for the changed frontend pages: Subscriptions and Providers. Verify default information architecture and Drawer-based create/edit behavior; persist findings to this file.
- **Scope**: mixed internal + browser behavior, with in-browser mocked API because no local center/auth backend was available.
- **Date**: 2026-05-20

## Findings

### Files Found

| File Path | Description |
|---|---|
| `web/src/pages/SubscriptionsPage.tsx` | Subscriptions route page; default evidence/filter/table layout and create/edit Drawer state. |
| `web/src/pages/ProvidersPage.tsx` | Providers route page; default master-data context/table layout and create/edit Drawer state. |
| `web/src/components/atoms/Drawer.tsx` | Shared Drawer atom used by both pages. |
| `scripts/visual_evidence.py` | Repo-local browser sanity helper used for protected routes with `--mock-api asset-workflows`. |
| `docs/operations/v2-visual-evidence.md` | Active browser sanity workflow and mock API guidance. |
| `.trellis/spec/web/component-conventions.md` | Frontend component guidance that records Drawer/focus and list-scan IA contracts. |

### Setup

- Started Vite dev server at `http://127.0.0.1:5174/` with `npm --prefix /Users/weibo/Code/houfeng/web run dev -- --host 127.0.0.1 --port 5174`.
- Used local Python Playwright via `/opt/homebrew/opt/python@3.11/bin/python3.11`.
- First Playwright launch with default temp path failed on `EACCES: permission denied, mkdtemp ...`; reran with `TMPDIR=/private/tmp`, matching the workflow caveat in `docs/operations/v2-visual-evidence.md`.
- Data source for detailed interaction check: in-browser route mocks for `/api/auth/me`, `/api/dashboard`, `/api/providers`, `/api/vps`, and `/api/subscriptions` with representative provider/VPS/subscription rows. No real center or session login was used.
- Also ran repo helper:

```bash
TMPDIR=/private/tmp /opt/homebrew/opt/python@3.11/bin/python3.11 /Users/weibo/Code/houfeng/scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5174/ \
  --mock-api asset-workflows \
  --route /providers \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900
```

Helper output was all PASS:

```text
PASS /providers 1440x1000 text=656 doc=1440 body=1440 panels=4 mock=asset-workflows url=http://127.0.0.1:5174/providers
PASS /providers 390x900 text=644 doc=390 body=390 panels=4 mock=asset-workflows url=http://127.0.0.1:5174/providers
PASS /subscriptions 1440x1000 text=1001 doc=1440 body=1440 panels=5 mock=asset-workflows url=http://127.0.0.1:5174/subscriptions
PASS /subscriptions 390x900 text=989 doc=390 body=390 panels=5 mock=asset-workflows url=http://127.0.0.1:5174/subscriptions
```

### Pages Checked

| Page | Route / state | Viewport | Result |
|---|---|---:|---|
| Subscriptions | `/subscriptions` default | 1440x1000 | PASS |
| Subscriptions | `/subscriptions?vps_id=vps_001&create=1` URL-requested create | 1440x1000 | PASS |
| Subscriptions | edit from row action `编辑 sub_001` | 1440x1000 | PASS |
| Providers | `/providers` default | 1440x1000 | PASS |
| Providers | create from `新建服务商` | 1440x1000 | PASS |
| Providers | edit from row action `编辑 Akari Cloud` | 1440x1000 | PASS |
| Providers + Subscriptions | repo helper overflow/body sanity | 1440x1000 and 390x900 | PASS |

### Observations

- Subscriptions default page showed the primary page header, then `续费与成本证据` before `订阅列表`; measured heading order in the browser was `y=290.4 < y=651.3` at 1440x1000. The summary cards showed `当前筛选`, `最近续费`, `生效 / 续费方式`, and `取消 / 过期`.
- Subscriptions default page showed the filter controls `VPS`, `状态`, and `续费窗口`, plus table rows including `tokyo-edge-1 / sub_001`. No `role="dialog"` was mounted, and neither `订阅创建` nor `订阅编辑` appeared inline by default.
- Subscriptions create opened through `新建订阅` as `role="dialog"` with accessible name `订阅创建表单`; the table heading remained present behind the Drawer. Clicking `取消` closed the dialog and left the browser at `/subscriptions` without `create=1`.
- Subscriptions URL-requested create at `/subscriptions?vps_id=vps_001&create=1` opened the create Drawer, displayed `当前 VPS 上下文`, preselected `#subscription-create-vps` as `vps_001`, and showed the hint `已从 URL 上下文预填当前 VPS，可切换为其他 VPS。`. Clicking `取消` closed the Drawer, removed `create=1`, and preserved `vps_id=vps_001`.
- Subscriptions edit opened from row action `编辑 sub_001` as `role="dialog"` with accessible name `订阅编辑表单`; `#subscription-edit-vps` was `vps_001` and `价格` was `12.5`. Clicking `取消编辑` closed the Drawer without navigation.
- Providers default page showed the primary page header, then `服务商主数据概览` before `服务商列表`; measured heading order in the browser was `y=290.4 < y=508.3` at 1440x1000. The context summary showed `服务商`, `国家 / 地区`, `资料覆盖`, and `低评分`.
- Providers default page showed table rows including `Akari Cloud / pv_001`. No `role="dialog"` was mounted, and neither `服务商创建` nor `服务商编辑` appeared inline by default.
- Providers create opened through `新建服务商` as `role="dialog"` with accessible name `服务商创建表单`; the table heading remained present behind the Drawer. Clicking `取消` closed the dialog with the `/providers` route still active.
- Providers edit opened from row action `编辑 Akari Cloud` as `role="dialog"` with accessible name `服务商编辑表单`; `服务商名称` was `Akari Cloud` and `评分` was `4`. Clicking `取消编辑` closed the Drawer without navigation.
- No screenshots were created or tracked.

### Code Patterns

- `web/src/pages/SubscriptionsPage.tsx:214-218` derives Drawer open state from either local create state or `create=1`, and pre-fills `vpsID` from the URL filter when present.
- `web/src/pages/SubscriptionsPage.tsx:257-277` clears `create=1` on close while preserving current filter params; browser sanity confirmed the post-cancel URL keeps `vps_id`.
- `web/src/pages/SubscriptionsPage.tsx:443-580` renders the default Subscriptions path as page header, optional VPS context, prerequisite notice, renewal/cost evidence, filters, and table.
- `web/src/pages/SubscriptionsPage.tsx:582-715` renders create and edit forms inside `Drawer` rather than inline panels.
- `web/src/pages/ProvidersPage.tsx:271-354` renders the default Providers path as page header, master-data context summary, and table.
- `web/src/pages/ProvidersPage.tsx:356-440` renders provider create and edit forms inside `Drawer` rather than inline panels.
- `web/src/components/atoms/Drawer.tsx:30-64` only mounts when open, portals to `document.body`, and exposes `role="dialog"` / `aria-modal="true"` with overlay, close button, and focus hook support.

### External References

- None. This check used repository code, repository workflow docs, and local browser execution only.

### Related Specs / Workflow Docs

- `.trellis/spec/web/component-conventions.md` — lines 48-57 record modal/Drawer focus behavior, Drawer cleanup, selector-first association forms, and list-scan paths using Drawer create/edit.
- `docs/operations/v2-visual-evidence.md` — lines 84-119 document protected asset route browser sanity with `--mock-api asset-workflows`, and the temp-dir caveat for local Playwright.

## Caveats / Not Found

- No local center/auth backend was used, so this does not verify backend correctness, auth/session behavior, persistence, real inventory completeness, or POST/PATCH submit behavior.
- The detailed create/edit checks opened and canceled Drawers only; they did not submit create or edit forms.
- The repo helper covered 390x900 overflow/body sanity for default `/providers` and `/subscriptions`, but the detailed open/cancel Drawer interaction check was run at 1440x1000.
- Screenshots were not captured because the active workflow does not require tracked screenshots for this check.
