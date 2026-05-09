# Dashboard asset summary

## Goal

在现有 Dashboard 系统概览工作台中增加少量资产决策摘要，让首页能快速暴露 VPS Asset Ledger 的续费、决策、关联和成本风险，同时保持异常处理队列仍是 Dashboard 主任务面。

## What I already know

* 用户要求暂不处理真实 VPS JSON 数据问题，继续按 `houfeng_codex_下一步开发计划.md` 推进下一个代码任务。
* 计划文档的下一个代码任务是 Task 7：Dashboard 资产摘要。
* Task 1-6 的主体能力已在 `main` 完成并合入：providers、VPS assets、subscriptions、JSON dry-run/import 工具、VPS-Node link backend、资产前端页面。
* 当前 `/api/dashboard` 已返回监控控制面摘要：节点/目标数量、异常/严重/维护计数、group summaries、通知状态、异常队列、24h trend、recent events。
* 现有 Dashboard 设计规范要求它是系统概览工作台，不是 `/api/dashboard` 字段仓库；异常处理队列仍应是主任务面。
* 资产摘要应作为少量决策入口，详细数据进入 VPS、订阅、服务商页面。
* 用户明确要求不使用 subagent；本任务在主会话手动加载 Trellis spec、实现和检查。

## Requirements

* 后端在 `incidents.DashboardOverview` 增加一个 `asset_summary` 字段，字段名保持 snake_case JSON contract。
* `asset_summary` 第一版只包含摘要，不包含 VPS、订阅或 Node 明细数组。
* 后端聚合以下指标：
  * 未来 30 天需要续费的 active subscription / VPS 数量。
  * 待决策 VPS 数量，按 `renewal_decision = 'unreviewed'` 统计。
  * 待取消 / 待迁移 VPS 数量，按 `lifecycle_status in ('to_cancel', 'to_migrate')` 统计。
  * 未关联 active Node 的 VPS 数量。
  * 关联了异常 Node 的 VPS 数量。
  * active subscriptions 按币种聚合的月付折算与年付折算成本。
* 成本聚合规则为 `monthly_total = sum(active subscriptions monthly_price)`，`yearly_total = monthly_total * 12`，不做汇率换算。
* Dashboard 前端在工作台中低权重展示资产摘要入口，不恢复被禁止的 `Dashboard 摘要指标` / `系统全局指标` / `系统快捷入口` / Group dump / Recent dump。
* 资产摘要入口应跳转到现有页面筛选：
  * 未来 30 天续费 -> `/subscriptions?renew_within_days=30`
  * 待决策 -> `/vps?renewal_decision=unreviewed`
  * 待取消/迁移 -> `/vps?lifecycle_status=to_cancel` 或更合理的现有单筛选入口
  * 未关联 Node -> `/vps`，如果现有 VPS 页面尚无对应筛选则只作为入口，不新增筛选 contract
  * 异常关联 VPS -> `/nodes?abnormal=1`
* AppShell 使用同一个 dashboard contract 时必须兼容新增字段，不把资产摘要误当作 center 健康或同步状态。

## Acceptance Criteria

* [ ] `GET /api/dashboard` 返回 `asset_summary`，嵌套字段为 snake_case。
* [ ] `asset_summary` 不包含资产明细 dump，也不暴露敏感配置。
* [ ] 后端 store 测试覆盖资产摘要 SQL、扫描和成本聚合。
* [ ] handler 测试覆盖 `asset_summary` JSON contract。
* [ ] `web/src/lib/types.ts` 定义 `DashboardAssetSummary` 与成本分组类型。
* [ ] DashboardPage 展示 4-6 个资产决策入口，并保持现有异常队列、运行上下文、管理入口不退化。
* [ ] DashboardPage 测试覆盖资产摘要展示和禁止恢复旧的 Dashboard warehouse 文案。
* [ ] AppShell 与 API client 测试 fixture 同步新增 contract。
* [ ] 本地验证通过：`git diff --check`、相关 Go 测试、相关 Web 测试、`cd web && npm run build`、`make verify-go`。

## Out of Scope

* 不处理真实 40+ VPS JSON 数据 dry-run/import 执行。
* 不新增 `renewal_decisions`、`price_histories`、`ip_histories`、`vps_spec_snapshots` 等历史表。
* 不做汇率换算。
* 不做 VPS 评分算法。
* 不新增 provider API 自动同步、DNS 同步、Web SSH、插件、服务发现、服务注册、域名管理或复杂 agent 命令。
* 不把 Node/Target/Agent 语义改造成资产层语义。
* 不新增 Dashboard 资产明细表格或资产字段总览。

## Technical Notes

* 后端路径：`internal/center/incidents/types.go`、`internal/center/store/dashboard.go`、`internal/center/store/dashboard_test.go`、`internal/center/http/handlers/dashboard_test.go`。
* 前端路径：`web/src/lib/types.ts`、`web/src/lib/api.test.ts`、`web/src/pages/DashboardPage.tsx`、`web/src/pages/DashboardPage.test.tsx`、`web/src/app/layout/AppShell.test.tsx`、`web/src/styles/pages.css`。
* Dashboard store 可在现有 `GetDashboardOverview` 中新增 `loadDashboardAssetSummary`，独立查询资产表，避免污染 Node / Target 读模型。
* `limit` 只影响异常队列与 recent events，不应影响资产摘要。
* active link 定义沿用 `vps_node_links.unlinked_at is null`。
* 异常 Node 定义沿用 `nodes.current_health_status <> '正常'`。
* active subscription 定义沿用 `subscriptions.status = 'active'`。
* VPS 资产是否 active 用 `vps_assets.lifecycle_status not in ('cancelled', 'archived')` 排除取消/归档资产。

## Definition of Done

* 代码实现与测试在非 main 分支完成。
* Trellis task 按流程启动、检查、归档并记录 journal。
* 提交 feature branch，推送，创建 PR。
* 监控 PR CI，全部 green 后合并。
* 合并后同步本地 main，并确认主分支状态干净。
