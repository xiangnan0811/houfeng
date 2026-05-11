# UX-3 Asset decision and VPS list redesign

## Goal

把资产决策页和 VPS 列表页从“后台资源管理页”推进为真实 VPS 数据测试前可用的资产工作台。用户应能在 40+ VPS 数据进入后快速判断哪些资产需要先评估、哪些即将续费、哪些缺少 Node/订阅/基础信息，并从列表或队列直接进入处理。

## What I Already Know

- 父级规划 `docs/release/core-pages-product-ux-replan.md` 已确认 UX-3 范围：资产决策页变成工作队列，VPS 列表变成高密度资产库存表，筛选器收敛为 quick views + chips + drawer。
- UX-1 已完成 AppShell 导航分组；UX-2 已把 Dashboard 改成 asset-decision-first command surface，并深链到 `/asset-decisions` 与 `/vps`。
- 当前 `AssetDecisionsPage` 仍是续费候选表 + 三个队列表 + 固定右侧处理面板，队列之间割裂，编辑面板占用主视觉。
- 当前 `VPSPage` 是普通列表页：顶部创建表单、FilterBar 四个筛选控件和 5 列表格。它没有把订阅、续费窗口、缺失字段、未关联 Node 这些真实核对信号放进第一屏。
- 当前前端已有 `listVPSAssets()`、`listSubscriptions()`、`listProviders()`、`updateVPSAsset()`，本任务不需要新增后端接口。
- 用户已明确暂不处理真实数据 import，且本阶段不做 Provider/DNS 自动同步。

## Requirements

### Asset Decisions

- 页面主视觉改为工作队列，不再把三张队列表当作三个同权 section 堆叠。
- 队列应按“现在最需要处理什么”排序，优先考虑：未评估、即将续费、迁移/取消、未关联 Node、缺少订阅。
- 保留续费窗口切换，并把续费候选作为当前队列的证据来源，而不是单独喧宾夺主的大表。
- 决策编辑放入 drawer 或同等次级 surface，不占据队列扫描空间。
- 每条 VPS 队列行要展示足够决策依据：身份、服务商/区域、续费决策、关联 Node、订阅/续费信号、数据质量提示、详情与处理入口。
- 处理后 VPS 应在队列中正确移动或移除，notice/error 仍可见。

### VPS List

- VPS 列表变成高密度资产库存表，优先展示身份、provider/region、订阅/续费、决策、Node 关联、数据质量。
- 筛选器收敛为 quick views + active chips + 高级筛选 drawer。主屏不再常驻一排完整筛选控件。
- 支持从 Dashboard/URL 进入时恢复已有 query：`provider_id`、`lifecycle_status`、`usage_status`、`renewal_decision`，并允许本任务新增只在前端解释的 quick view query。
- 列表应能在无新增后端字段的情况下，通过现有 subscriptions/VPS 数据标出缺订阅、未关联 Node、缺 provider、缺 region/IP/SSH 等数据质量问题。
- 创建 VPS 能力保留，但不作为主视觉；创建表单仍可折叠展开。

## Acceptance Criteria

- [ ] 资产决策页首屏显示一个统一工作队列，且不再常驻三张同权 VPS 队列表。
- [ ] 资产决策页决策编辑通过 drawer 打开，关闭后不影响队列扫描。
- [ ] 资产决策页的续费窗口、工作队列、续费候选、处理结果和错误态有测试覆盖。
- [ ] VPS 页首屏显示 quick views、active chips、高密度库存表；高级筛选放入 drawer。
- [ ] VPS 表格展示订阅/续费、Node 关联和数据质量提示；缺订阅、未关联、缺基础信息能被直接发现。
- [ ] VPS 页保留创建 VPS 流程，并有测试覆盖。
- [ ] 不运行真实数据 import，不新增 Provider/DNS 自动同步，不新增后端 API 字段。
- [ ] `cd web && npm run lint`、`cd web && TMPDIR=$PWD/.tmp npm run test -- --run AssetDecisionsPage VPSPage`、`cd web && TMPDIR=$PWD/.tmp npm run test -- --run`、`cd web && npm run build` 通过。
- [ ] 做桌面与移动端视觉 sanity check，确认文本不重叠、首屏不混乱。

## Out Of Scope

- 真实 40+ VPS JSON dry-run/import。
- Provider API 或 DNS 自动同步。
- VPS 详情页重排；这属于 UX-4。
- 完整服务注册表、完整域名管理或 DNS record 管理。
- 后端 Asset Ledger 模型扩展、评分算法或汇率换算。

## Technical Notes

- Main files: `web/src/pages/AssetDecisionsPage.tsx`, `web/src/pages/VPSPage.tsx`, `web/src/components/AssetDecisionVPSQueueTable.tsx`, `web/src/components/AssetDecisionWorkPanel.tsx`, `web/src/styles/pages.css`.
- Tests: `web/src/pages/AssetDecisionsPage.test.tsx`, `web/src/pages/VPSPage.test.tsx`.
- Existing API constraints: `VPSAssetRecord` has `active_node_link_count` but not linked node health; this task can show linked/unlinked state, not invent health.
- Existing subscription constraints: `SubscriptionRecord` has `renew_at`, `monthly_price`, `price`, `currency`, auto-renew and status; page can enrich VPS rows by grouping subscriptions client-side.
- Use existing `Drawer`, `Tabs`, `Badge`, `DataTable`, `FilterChip`, `FilterSelect`, `MonoDigits` and asset badges; no new dependency.
