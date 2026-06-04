# 服务商页轻量上下文升级

## Goal

把 `/providers` 从单纯 CRUD 表升级为“服务商目录 + 资产上下文入口”。页面保持低频维护定位，不新增后端 API，不做服务商详情页，不宣称真实 provider 账号、账单、外部评分或外部状态，只使用现有 providers / VPS / subscriptions 数据派生轻量上下文，并提供安全的外部口碑研究入口。

## Requirements

- 页面标题改为“服务商目录”，副文案表达“供 VPS 与订阅引用的低频资产事实”。
- 顶部使用候风 v2 风格的页面身份区，保留克制、高密度工程工具气质，不做大屏 KPI 卡片或服务商智能分析；摘要只用紧凑 inline rail 承接空白感。
- 前端并行加载 `listProviders()`、`listVPSAssets()`、`listSubscriptions()`；providers 仍是主数据源，VPS / subscriptions 仅用于本页派生。
- 为每个服务商派生：
  - `vpsCount`：`vps.provider_id === provider.provider_id` 的 VPS 数。
  - `subscriptionCount`：通过订阅 `vps_id` 找到 VPS，再归属到 provider。
  - `monthlyCostBase`：只汇总已有 `monthly_price_base`，不可用时不做跨币种估算。
  - `metadataIssues`：缺面板入口、缺账号提示、缺国家/地区、未评分。
  - `hasAssets`：有关联 VPS 或订阅。
- 主体保留高密度列表 / 表格，增强身份列、资产上下文列、入口状态、我的评分、外部口碑、更新时间和操作。
- 身份列支持从现有 `account_hint` 前端拆分多个账号提示；不新增账号模型或 provider account API。
- 我的评分与外部口碑必须分离展示：`rating` 是用户主观评分；LowEndTalk、Trustpilot、HostAdvice、VPSBenchmarks 只作为外部研究入口，不在前端抓取或宣称实时分数。
- 行操作包含编辑、打开官网、打开面板、查看 VPS、查看订阅；网站或面板地址为空时不生成空链接。
- `查看 VPS` 跳转 `/vps?provider_id=<provider_id>`；`查看订阅` 跳转 `/subscriptions?provider_id=<provider_id>`。
- 增加页面内筛选：全部、有资产、多账号、缺资料、未评分、低评分；增加文本搜索，匹配名称、国家、账号提示和标签。筛选不写入 URL。
- 创建/编辑表单按“身份 / 入口 / 复盘”分组，继续使用现有字段和 API payload；取消/关闭必须丢弃草稿与错误。

## Acceptance Criteria

- [ ] `/providers` 首屏呈现“服务商目录”身份区、轻量概览和增强列表。
- [ ] providers + VPS + subscriptions 能正确派生 VPS 数、订阅数和安全的月成本摘要。
- [ ] 多账号服务商能在身份列拆分显示，并可通过“多账号”筛选命中。
- [ ] 外部口碑列展示 LowEndTalk / Trustpilot / HostAdvice / VPSBenchmarks 入口，并明确不代表“我的评分”。
- [ ] 缺面板、缺账号、缺国家、未评分能够进入缺资料 / 未评分筛选。
- [ ] 搜索能匹配服务商名、国家、账号提示、标签。
- [ ] 查看 VPS / 查看订阅 / 官网 / 面板链接正确，空官网或空面板不渲染可点击空链接。
- [ ] 创建/编辑 API payload、校验、取消重置行为保持兼容。
- [ ] `cd web && npm run test -- --run ProvidersPage` 通过。
- [ ] `cd web && npm run lint`、`cd web && npm run build` 通过；完整 web 测试尽量运行并记录结果。

## Notes

- 本次仅改前端体验；不新增后端 provider summary API、不改数据库 schema。
- 月成本仅在 `monthly_price_base` 可用时汇总，部分缺失时要明确降级。
