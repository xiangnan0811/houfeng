# 服务商页轻量上下文升级设计

## Boundary

`ProvidersPage` 仍然是路由页装配点：拉取 providers / VPS / subscriptions，派生本页展示模型，组合页面状态、筛选、表格和表单。后端接口、类型 wire shape、数据库和路由不变。

## Data Model

新增页面内展示模型 `ProviderDirectoryRow`：

- `provider`: 原始 `ProviderRecord`
- `vpsCount`: 关联 VPS 数
- `subscriptionCount`: 归属该 provider 的订阅数
- `cost`: 页面安全摘要，包含展示文本、辅助文案和 `ready | unavailable` 完整度；只在全部关联订阅都有同一 `base_currency` 与可用 `monthly_price_base` 时汇总
- `metadataIssues`: `missing_panel | missing_account | missing_country | unrated`
- `hasAssets`: `vpsCount > 0 || subscriptionCount > 0`
- `accounts`: 从现有 `account_hint` 按逗号、中文逗号、分号、中文分号和换行拆分出的账号提示列表；仅用于展示和搜索
- `externalLinks`: 从 provider name 生成的外部研究入口；不代表实时评分，不写回 provider 数据

订阅归属规则：先建立 `vpsByID`，订阅按 `subscription.vps_id` 找 VPS，再按 `vps.provider_id` 归属；没有 provider_id 的 VPS 不归到任何 provider，避免按名字猜测。

## External Reputation

本次只做“外部口碑入口”，不做评分抓取或来源真相判断。原因：

- LowEndTalk 更接近社区讨论与用户经验帖，不能等价为数值评分。
- Trustpilot / HostAdvice 属于第三方评价站，页面和分数会漂移，前端直接抓取有 CORS、稳定性与语义风险。
- VPSBenchmarks 更接近性能基准，不是主观口碑。

因此 UI 把 `provider.rating` 命名为“我的评分”，把外部站点放入独立“外部口碑”列，并展示“入口，不代表我的评分”的说明。

## UX

- 顶部：`watchtower-header`，标题“服务商目录”，徽标显示总数、有资产引用、资料待补。
- 概览：紧凑 inline summary rail，不使用订阅页式 KPI 卡片；展示总数、有资产、多账号、资料待补、评分复盘和外部口碑源入口。
- 筛选：文本搜索 + quick view；筛选只作用于当前页面 state。quick view 包含全部、有资产、多账号、缺资料、未评分、低评分。
- 列表：使用语义表格或现有 table 样式，保持高密度；行内操作使用 `<Link>` / `<a>` / `<button>`，不制造不存在的 provider detail route。
- 跳转：内部使用 `/vps?provider_id=...` 与 `/subscriptions?provider_id=...`；外部使用已有官网和面板 URL；外部口碑使用 LowEndTalk / Trustpilot / HostAdvice / VPSBenchmarks 搜索入口。
- 表单：继续使用 `Modal` + `Input`，按三段分组；提交成功关闭，取消/关闭重置。

## Failure Handling

providers 是主数据源。若 `listProviders()` 失败，页面进入错误态。若 VPS 或 subscriptions 加载失败，仍展示服务商列表，并在 header meta 或列表区域提示“资产上下文不可用”，派生统计降级为 `—`，不误判缺资产。
