# Task 7 窄视口基线证据

## Source Baseline

- Base: `origin/main@dfe11a8d5305e0318382acdc3c408dec8a8f7ae9`
- Release-equivalent Web tree: `v0.58.1` / `f8fdb30d6339b00ae49f181527af7afac6ee4a70`；其后的 Task 6 archive merge 仅修改 Trellis 文档。
- Runtime: Node `22.23.1`；Chromium `150.0.7871.114`。
- Test floor inherited from Task 6: 90 files / 669 tests。
- Dependencies: `frontend-dashboard-trust` 与 `frontend-accessibility-contracts` 已归档；parent progress 6/10。

## Browser Fixture

- Web files直接来自已发布 Docker image `linnea7171/houfeng@sha256:ff15def93f7f42d9a9aaf3757e0b450723e1513ce64720a7d38a487583f3cbe6` 的 `/app/web/dist`。
- API 使用仓库 `scripts/visual_evidence.py` 的 `asset-workflows`，对未覆盖 endpoint 再使用 `observability-support` fallback；fixture/harness 只位于 `/tmp`，未修改 package/lockfile。
- Viewport: `390x900`。Routes: `/`、`/settings`、`/vps`、`/asset-decisions`、`/providers`、`/subscriptions`、`/monitoring`、`/targets`、`/events`。
- 九路由 page-level horizontal overflow 均为 false；console error、runtime exception、CSP violation、HTTP >=400 与 network loading failure 均为 0。

## RED Measurements

| Surface | Computed evidence | Failure |
| --- | --- | --- |
| Settings `监控策略` | `78×49px`; `white-space: normal`; client/scroll width `78/78` | 同组 tab 被多行高度拉伸，不是单行局部滚动 |
| Asset `场景与组合` title | client/scroll width `26/58`; `white-space: nowrap`; `overflow:hidden`; `text-overflow:ellipsis` | 可见标题主动裁切 |
| Provider `组合决策` link | client/scroll width `46/52`; `overflow:hidden`; `text-overflow:ellipsis` | 可见命令主动裁切 |
| Provider panel | client/scroll width `298/986`; `overflow-x:auto`; role/label absent; `tabIndex=-1` | 整个 section 滚动，heading/toolbar 不固定，区域无键盘语义 |
| Dashboard primary action | x `81..355`; y `272..372`; no overflow | 已在 390x900 首屏，本任务只防回归 |

## Source Causes

- `atoms.css`: `.tabs--pill .tab` 使用 `min-width:0`，没有 `flex:0 0 auto` / `white-space:nowrap`；pill 自身不是通用 scroll container。
- `legacy-assets.css`: `.asset-decision-support-strip__title` 明确使用 nowrap + hidden + ellipsis。
- `legacy-misc.css`: 同一 support strip 在 920px 与两组 640px rule 中重复设置 grid/min-height；部分 `grid-template-columns` 落在仍为 inline-flex 的 item 上，不生效。
- `legacy-provider.css`: entry container `overflow:hidden`，所有 links 统一 `max-width:48px` + ellipsis。
- `ProvidersPage.tsx`: `page-panel--scroll-x` 位于整个 section，DataTable 没有独立 region wrapper。

## Evidence Boundary

- 本基线证明 fixture 下的真实 CSSOM/geometry 和 browser diagnostics，不证明真实认证、真实 Provider 数据或 staging。
- Task 7 必须用同一 measurement 由 RED 转 GREEN；Task 10 才把 Playwright/axe/coverage/staging 持久化进 CI。
