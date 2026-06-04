# Browser Sanity

## Local Tooling

- Standard command attempted:
  - `python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178 --route /providers --viewport 1280x900 --viewport 390x900 --mock-api asset-workflows`
- Result: blocked because the local Python Playwright package is not installed. The repo intentionally does not depend on it.
- Follow-up: used local Chrome via Puppeteer from `.tmp/browser-sanity` with a temporary mock API for `/api/auth/me`, `/api/dashboard`, `/api/providers`, `/api/vps`, and `/api/subscriptions`.
- Screenshots stayed under `.tmp/browser-sanity/` and are not tracked.

## Geometry Result

Desktop `1280x900`:

- `sectionTitle`: `服务商与入口`
- table headers: `服务商`, `资产上下文`, `服务入口`, `我的评分`, `外部口碑`, `标签 / 备注`, `更新时间`
- `documentScrollWidth`: `1280`
- `bodyClientWidth`: `1280`
- `hasPageOverflow`: `false`
- `hasPanelScroll`: `false`
- service entry links: `官网`, `面板`, `VPS`, `订阅`
- service entry link height: `22px`
- service entry `white-space`: `nowrap`
- forbidden legacy text found: none

Mobile `390x900`:

- `documentScrollWidth`: `390`
- `bodyClientWidth`: `390`
- `hasPageOverflow`: `false`
- `hasPanelScroll`: `true`
- service entry link height: `22px`
- service entry `white-space`: `nowrap`
- forbidden legacy text found: none

## Notes

- Desktop no longer needs table-panel horizontal scrolling at `1280x900`.
- Mobile preserves table-panel horizontal scrolling only inside the panel; the page itself does not overflow horizontally.
- Legacy explanatory placeholders checked absent: `缺面板入口`, `网站已记录`, `网站未记录`, `账号未记录`, `未标记`, `入口，不代表我的评分`, `成本未折算`, `目录与入口`.
