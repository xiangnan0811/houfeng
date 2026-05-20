# Browser Sanity Evidence: VPS inventory IA polish

- **Date**: 2026-05-20
- **Task**: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish`
- **Route**: `/vps`
- **Evidence level**: DevTools-based browser sanity with visual screenshots
- **Result**: PASS

## Environment

- **Repo**: `/Users/weibo/Code/houfeng`
- **Preview server**: Vite dev server, `npm --prefix /Users/weibo/Code/houfeng/web run dev -- --host 127.0.0.1 --port 5178`
- **Preview URL**: `http://127.0.0.1:5178/`
- **Browser**: Google Chrome `148.0.7778.97`
- **Automation path**: Chrome DevTools Protocol (`--headless=new --remote-debugging-port=9222`) with request interception through CDP `Fetch.fulfillRequest`
- **Reason for alternate workflow**: local Python packages `playwright` and `selenium` were unavailable, so the repo-local Playwright helper could not run directly.
- **Cleanup**: the temporary Vite server and headless Chrome process were stopped after evidence capture.

## Data Source

- **Source**: mock-backed `asset-workflows` equivalent, not a real authenticated center/PostgreSQL run.
- **Mocked endpoints**: `/api/auth/me`, `/api/dashboard`, `/api/providers`, `/api/vps`, `/api/subscriptions`.
- **Fixture coverage**: representative Asset Ledger rows covering renewal-due VPS, unreviewed decisions, active subscription evidence, missing subscription, unlinked VPS, missing facts, cancel/migrate/archive states, and provider/subscription metadata.
- **Caveat**: this proves protected `/vps` route rendering and interactions against representative mock state only; it does not prove backend correctness, real account completeness, import fidelity, real authentication/session behavior, or real PostgreSQL inventory results.

## Viewports

| Viewport | URL checked | Result |
|---|---|---|
| `1440x1000` | `http://127.0.0.1:5178/vps` and `http://127.0.0.1:5178/vps?view=unlinked` | PASS |
| `390x900` | `http://127.0.0.1:5178/vps` and `http://127.0.0.1:5178/vps?view=unlinked` | PASS |

## Checks Performed

### Initial `/vps` render

Verified visually and through DOM inspection in both viewports:

- Page renders the VPS inventory command surface with `库存核对`.
- Current inventory lens is present via `aria-label="当前库存 lens"` and visible `当前 lens` copy.
- Quick view tabs render: `全部`, `30天续费`, `未评估`, `未关联`, `缺订阅`, `缺信息`, `已归档`.
- Subscription evidence card renders with `订阅证据` / `订阅证据已读取`.
- Filter context renders with `字段筛选`, `未应用字段筛选`, and `高级筛选`.
- Table work-area copy renders as `VPS ASSETS · WORK AREA`.
- Initial table shows `5` fixture rows.
- Page-level geometry had no horizontal overflow: desktop `doc/body = 1440/1440`, mobile `doc/body = 390/390`.
- Text overflow scan reported `0` leaf-text overflow warnings in both viewports.

### Quick view switch: `未关联`

Verified in both viewports:

- Clicking `未关联` changes URL to `/vps?view=unlinked`.
- Selected quick view becomes `未关联 3`.
- Lens/table count updates from `5 / 5` to `3 / 5`.
- Table includes the unlinked fixture row `tokyo-lab-unlinked` and does not show `ams-core-01` after the filter.
- Active chip shows `视图: 未关联`.
- Layout remains intact with no page-level horizontal overflow and `0` text overflow warnings.

### Advanced filter drawer draft behavior

Verified in both viewports:

- `高级筛选` opens a dialog labeled `VPS 高级筛选`.
- A draft lifecycle filter was changed to `testing` / `测试中` inside the drawer.
- Closing the drawer with the close control, without clicking `应用筛选`, removes the dialog and leaves URL unchanged at `/vps?view=unlinked`.
- No `生命周期: 测试中` filter chip appears after closing.
- Existing `未关联` quick view remains selected and the table remains at `3` rows.

### Create VPS drawer

Verified in both viewports:

- The mock fixture is non-empty, so the empty-state `录入第一台 VPS` path is not exposed in this mock-backed browser run.
- The primary create path (`新建 VPS`) opens a dialog labeled `VPS 创建表单` with `VPS 创建` title and the copy `创建只记录资产库存基础事实；订阅、Node 关联和详细编辑继续在对应页面补齐。`.
- The drawer contains the expected grouped create fields (`基础识别`, `访问入口`, `运行与决策`) and closes back to the unchanged filtered page.
- Empty-state create drawer behavior is covered by jsdom tests in `/Users/weibo/Code/houfeng/web/src/pages/VPSPage.test.tsx`.

## Output / Screenshots

- Raw DevTools sanity output: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish/research/devtools-browser-sanity-output.json`
- Desktop initial: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish/research/screenshots/vps-1440x1000-initial.png`
- Mobile initial: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish/research/screenshots/vps-390x900-initial.png`
- Desktop `未关联`: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish/research/screenshots/vps-1440x1000-unlinked.png`
- Mobile `未关联`: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish/research/screenshots/vps-390x900-unlinked.png`
- Desktop create drawer: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish/research/screenshots/vps-1440x1000-create-drawer.png`
- Mobile create drawer: `/Users/weibo/Code/houfeng/.trellis/tasks/05-20-vps-page-inventory-ia-polish/research/screenshots/vps-390x900-create-drawer.png`

## Caveats / Not Covered

- Mock-backed evidence only; no real authenticated center/PostgreSQL was used.
- Python Playwright remained unavailable locally, so this was not a direct `scripts/visual_evidence.py browser-sanity` execution.
- The empty-state create path was not reachable with the non-empty `asset-workflows` mock fixture; jsdom coverage exists in `VPSPage.test.tsx` for the empty-state create path.
- Screenshots and JSON output are task-local evidence files, not public docs assets.
