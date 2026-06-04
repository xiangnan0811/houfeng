# Browser Sanity

## 2026-06-04

Local route check used `web` Vite dev server on `http://127.0.0.1:5178/` and a temporary mock API on `127.0.0.1:8080` for `/api/auth/me`, `/api/dashboard`, `/api/providers`, `/api/vps`, and `/api/subscriptions`.

- `python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /providers --viewport 1440x1000 --viewport 390x900 --mock-api asset-workflows` was attempted, but this local Python environment does not have Playwright installed.
- Headless Google Chrome via Chrome DevTools Protocol was used as the fallback for deterministic desktop/mobile viewport checks and screenshots.
- The pre-release table-density defect was reproduced from the implementation shape: an overly wide identity column plus under-sized status/action columns could make short status labels wrap and leave uneven whitespace. The page now uses fixed column widths, `table-layout: fixed`, no-wrap status/action chips, shorter visible external-reputation labels with full accessible names, and a narrower identity column.

Final measured results:

- Desktop `1440x1000`: `documentElement.scrollWidth = 1440`, `body.scrollWidth = 1440`; `.provider-directory-panel` `clientWidth = 1162`, `scrollWidth = 1162`; table `clientWidth = 1114`, `scrollWidth = 1112`; `tableLayout = fixed`.
- Desktop columns: `196px`, `140px`, `136px`, `88px`, `226px`, `116px`, `192px`.
- Desktop first-row cell overflow checks: all columns returned `overflow = false`, including service entry, external reputation, updated time, and actions.
- Service entry text checks: `面板已记录`, `缺面板入口`, and `网站已记录` all returned `whiteSpace = nowrap` and `overflow = false`.
- External reputation links: visible labels `LET`, `Trust`, `HostAdv`, `Bench` all returned `overflow = false`; accessible names still use full sources such as `Trustpilot`, `HostAdvice`, and `VPSBenchmarks`.
- Action buttons: `编辑`, `官网`, `面板`, `VPS`, `订阅` all returned `whiteSpace = nowrap` and the action container returned `overflow = false`.
- Mobile `390x900`: `documentElement.scrollWidth = 390`, `body.scrollWidth = 390`; summary rail `display = grid`, `clientWidth = 308`, `scrollWidth = 308`; the high-density provider table scrolls inside its own panel (`panelScrollWidth = 1152`), not at page level.
- The old provider metric-card strip remains absent (`.provider-directory-metrics .subscription-metric-card` count = 0 by implementation review and screenshot).
- Screenshots saved: `providers-desktop-final.png` and `providers-mobile-final.png`.

Caveat: This proves protected route layout and interactions against representative local mock state only. It does not prove backend correctness, real account completeness, real billing accuracy, real authentication/session behavior, or real inventory data.
