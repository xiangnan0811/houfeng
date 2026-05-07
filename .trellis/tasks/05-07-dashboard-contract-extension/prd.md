# PR3: Dashboard Contract Extension

## Goal

把 `/api/dashboard` 从“异常摘要接口”扩展成首页和 Shell 可以共同信任的全局系统摘要 contract。PR3 要补齐 PR1/PR2 中刻意没有伪造的事实：真实生成时间、全量 Group 摘要、节点/目标生命周期完整度、暂停/退役/归档状态，以及通知通道配置状态。前端 Dashboard 应使用这些字段替换临时文案和前端推断，让首页更像服务器管理系统的总览入口，而不是只显示异常列表。

## What I Already Know

* PR1 已完成 Dashboard 信息架构重构：Fleet State、全局 KPI、当前处理队列、系统入口、首次接入工作台、最近事件。
* PR1 明确把真实 group summaries、pending onboarding、paused/retired/archived、notification configured 拆到 PR3。
* PR2 已完成 AppShell / Sidebar 可信计数：Shell 复用 `/api/dashboard`，但刻意不扩展后端 contract。
* 当前 Go `incidents.DashboardOverview` 只有总量、异常、严重、维护、24h 事件、异常节点/目标、最近事件和趋势，没有系统完整度字段。
* 当前 `store.GetDashboardOverview` 从 `nodes`、`targets`、`state_change_events` 查询基础统计，再查异常列表和最近事件。
* 当前 `DashboardPage` 仍有 `快照时间：接口暂未提供`，`设置` 入口只能显示静态 `配置入口`。
* 当前设计文档仍提醒 Dashboard API 不能提供真实 Group 分布，PR3 完成后需要同步文档/规范。
* Settings 数据已经存在于 `center_settings`，包含 Telegram token/chat id/runtime managed 与 Feishu enabled/webhook URL，可用于 Dashboard 暴露最小配置状态。

## Current Problems

* Dashboard 首屏明示 API 已加载，却不能给出任何可信生成时间。
* 首页系统入口里的设置状态无法表达通知是否配置，用户不知道异常发生后系统是否具备通知闭环。
* 首页只能展示异常对象，不知道库存里是否有未接入、暂停、退役或归档对象。
* 若继续从 `abnormal_nodes` / `abnormal_targets` 派生 Group，会把“异常分布”误读成“全量分布”。
* Dashboard 和 AppShell 已经成为全站共同依赖的数据源，contract 不完整会持续限制后续 PR4 的深链筛选和工作台能力。

## Proposed MVP Contract

在现有 `/api/dashboard` response 上做向后兼容的 additive extension，不新增 endpoint，不改变已有字段含义。

### 1. Snapshot Time

新增：

* `snapshot_generated_at`

语义：

* 后端生成 Dashboard overview 的时间，使用 Center 服务器时间。
* 它不是 agent 心跳时间、不是数据同步时间、不是健康检查结果。
* 前端文案应表达为 `生成时间` / `Dashboard 摘要`，不能暗示真实 Center health 或全链路 fresh。

### 2. Inventory Completeness Counts

新增顶层计数：

* `pending_onboarding_node_count`
* `paused_node_count`
* `retired_node_count`
* `paused_target_count`
* `archived_target_count`

字段定义：

* `pending_onboarding_node_count`: `nodes.lifecycle_status = '待接入'` OR `nodes.binding_status in ('未绑定', '指纹变更待确认')`
* `paused_node_count`: `nodes.monitoring_status = '暂停'`
* `retired_node_count`: `nodes.lifecycle_status = '已退役'`
* `paused_target_count`: `targets.run_status = '暂停'`
* `archived_target_count`: `targets.run_status = '已归档'`

使用边界：

* 这些字段用于首页库存健康和系统入口摘要。
* 不在 PR3 内新增或改变节点/目标筛选 URL；深链筛选留给 PR4。

### 3. Full Group Summaries

新增：

```go
type DashboardGroupSummary struct {
    Group                  string `json:"group"`
    NodeCount              int    `json:"node_count"`
    TargetCount            int    `json:"target_count"`
    AbnormalNodeCount      int    `json:"abnormal_node_count"`
    AbnormalTargetCount    int    `json:"abnormal_target_count"`
    SevereNodeCount        int    `json:"severe_node_count"`
    SevereTargetCount      int    `json:"severe_target_count"`
    MaintenanceNodeCount   int    `json:"maintenance_node_count"`
    MaintenanceTargetCount int    `json:"maintenance_target_count"`
}
```

并在 overview 上新增：

* `group_summaries`

字段定义：

* 覆盖全量 `nodes` 和 `targets`，不能只来自异常列表。
* `group` 取 `nodes."group"` / `targets."group"`，空白或空字符串规范化为 `未分组`。
* 同一 group 下分别统计节点与目标数量、异常数量、严重数量、维护数量。
* 排序建议：异常总数 desc、严重总数 desc、对象总数 desc、group asc。

使用边界：

* Dashboard 可以恢复/新增真实 `按 Group 分布` 区块。
* 如果没有任何节点/目标，前端显示空态，不制造 `未分组 0` 行。

### 4. Notification Status

新增：

```go
type DashboardNotificationStatus struct {
    TelegramConfigured          bool `json:"telegram_configured"`
    TelegramRuntimeManaged      bool `json:"telegram_runtime_managed"`
    TelegramRuntimeApplyActive  bool `json:"telegram_runtime_apply_active"`
    FeishuConfigured            bool `json:"feishu_configured"`
}
```

并在 overview 上新增：

* `notification_status`

字段定义：

* `telegram_configured`: `telegram_bot_token` 和 `telegram_chat_id` 均非空白。
* `telegram_runtime_managed`: 直接来自 `telegram_runtime_managed`。
* `telegram_runtime_apply_active`: runtime managed 为 true 且 Telegram 已配置。
* `feishu_configured`: `feishu_enabled` 为 true 且 `feishu_webhook_url` 非空白。
* 如果 `center_settings` 没有 singleton 行，所有字段返回 false。

使用边界：

* Dashboard 只展示配置摘要，不暴露 token、chat id、webhook URL。
* PR3 不新增通知发送逻辑，不改变 settings 更新语义。

## Backend Requirements

* 扩展 `internal/center/incidents/types.go` 的 `DashboardOverview` 与新增 summary/status types，JSON tag 使用 snake_case。
* 在 `internal/center/store/dashboard.go` 扩展统计 SQL，保持手写 SQL + pgx/v5 现有风格。
* `GetDashboardOverview` 应填充新增字段，并在出现查询错误时返回带上下文的 wrapped error。
* 可以在 dashboard store 内直接查询 `center_settings`，不要求改 handler DI；因为这是同一个 dashboard read model。
* 不新增数据库迁移，除非实现中发现 contract 依赖的列不存在。当前 schema 已有需要的列。
* 保持 `limit` 仅影响异常摘要与最近事件，不影响 group_summaries 或全局计数。
* Go tests 需要覆盖：
  * 新增顶层计数能从 store scan 填充。
  * group summary 使用全量 group SQL，而不是异常列表。
  * notification status 在 settings 缺失时为 false，在配置存在时按定义计算。
  * handler 输出 snake_case 新字段，且不泄露 Go struct 字段名。

## Frontend Requirements

* 扩展 `web/src/lib/types.ts` 的 `DashboardOverview`，与后端 JSON contract 对齐。
* `DashboardPage` 应使用 `snapshot_generated_at` 替换 `接口暂未提供`。
* 首页系统入口应把节点/目标完整度接入真实字段，例如待接入、暂停、退役、归档，而不是只显示总数/异常。
* 设置入口应展示通知配置摘要，例如 Telegram / Feishu 已配置数量或具体通道状态。
* 新增或恢复真实 `按 Group 分布` 展示，数据必须来自 `overview.group_summaries`。
* 首页文案需要保持运维工作台语气，避免把 generated snapshot 描述成真实健康检查。
* `DashboardPage.test.tsx` 需要覆盖：
  * snapshot generated time 显示。
  * group summaries 来自 contract 并展示全量节点/目标分布。
  * 库存健康显示待接入/暂停/退役/归档。
  * 设置入口显示 notification configured 状态。
* `api.test.ts` / AppShell 相关 fixtures 如因类型变更受影响，需要同步补齐。

## Docs / Spec Requirements

* 更新 `docs/design/v2-houfeng/component-spec.md`，把 Dashboard API 不支持 group/notification/snapshot 的旧限制替换为 PR3 后的真实 contract。
* 更新 `.trellis/spec/web/state-and-data.md` 或相关 spec，沉淀 Dashboard 事实展示规则：
  * Snapshot generated time 只能表达接口生成时间。
  * Group distribution 必须来自 `group_summaries`。
  * Notification status 只能展示配置布尔摘要，不展示敏感配置值。

## Out of Scope

* PR4 的 URL-state 深链筛选。
* 新增 Shell summary endpoint。
* 真实 Center heartbeat / uptime / health check。
* 通知发送、通知测试按钮、通知历史详情。
* 自定义 Dashboard、保存视图、拖拽布局。
* 新图表库、状态库或 CSS 体系。
* 对节点/目标列表页做大范围视觉重构。

## Acceptance Criteria

* [ ] `/api/dashboard` 返回 `snapshot_generated_at`、库存完整度计数、`group_summaries`、`notification_status`。
* [ ] 新字段全部由后端真实表数据计算，前端不再从异常摘要伪造全量 Group 或通知状态。
* [ ] Dashboard 首屏不再显示 `接口暂未提供`。
* [ ] Dashboard 能从首页理解待接入/暂停/退役/归档/通知配置这些服务器管理基础事实。
* [ ] Dashboard 的 Group 展示语义准确，来自全量 group summaries。
* [ ] 后端 handler/store tests 覆盖新增 contract 和 snake_case。
* [ ] 前端类型、页面测试和必要 fixtures 与新增 contract 同步。
* [ ] 设计文档与 Trellis web spec 不再描述 PR3 后已经具备的事实为缺失。

## Verification Plan

后端：

```bash
TMPDIR=/tmp GOTMPDIR=/tmp/houfeng-go-build GOCACHE=/tmp/houfeng-gocache go test ./internal/center/...
```

前端：

```bash
cd web && TMPDIR=/tmp npm run test -- --run src/pages/DashboardPage.test.tsx src/lib/api.test.ts src/app/layout/AppShell.test.tsx
cd web && TMPDIR=/tmp npm run lint
cd web && TMPDIR=/tmp npm run build
```

全局：

```bash
git diff --check
```

## Technical Notes

Likely touched files:

* `internal/center/incidents/types.go`
* `internal/center/store/dashboard.go`
* `internal/center/store/dashboard_test.go`
* `internal/center/http/handlers/dashboard_test.go`
* `web/src/lib/types.ts`
* `web/src/pages/DashboardPage.tsx`
* `web/src/pages/DashboardPage.test.tsx`
* `web/src/lib/api.test.ts`
* `web/src/app/layout/AppShell.test.tsx`
* `docs/design/v2-houfeng/component-spec.md`
* `.trellis/spec/web/state-and-data.md`
