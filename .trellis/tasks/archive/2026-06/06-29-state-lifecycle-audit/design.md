# 状态生命周期修复设计

## 背景

审查报告确认状态风险主要来自三类问题：

- 同名/近名字段跨领域复用，例如订阅 `renewal_mode` 与 VPS `renewal_decision`。
- 事实状态、意图状态、流程状态没有写入路径边界，例如 VPS 普通 PATCH 能进入流程/终态。
- 阈值与运行态策略跨后端 settings、evaluator、前端图表、周期 sweep 漂移。

本轮修复以“收口现有错误合同”为主，不半实现大型新流程。完整迁移工作台、完整 lifecycle action audit 和订阅来源拆分需要独立产品任务。

## 状态合同

### VPS 状态轴

- `lifecycle_status` 是事实/流程状态。
  - 普通资料 PATCH 只允许低风险事实状态：`active`、`idle`、`testing`。
  - `to_cancel`、`cancelled` 必须通过取消/退役 workbench 或现有 archive/cancellation endpoint。
  - `to_migrate` 暂不允许普通 PATCH 直接写入，因为当前没有迁移工作台。
  - `archived` 继续只允许 archive endpoint。
- `usage_status` 是使用事实，仍可普通 PATCH 更新。
- `renewal_decision` 是续费/资产处理意图，普通 PATCH 可保存 `keep`、`observe`、`migrate`、`cancel` 等判断，但这不等同于流程已执行。

### 订阅筛选

- `subscriptions.ListFilters.RenewalDecision` 筛的是关联 VPS 的 `vps_assets.renewal_decision`。
- normalization/validation 必须使用 `internal/center/vpsassets` 的 renewal decision 合同。
- 若未来需要筛选订阅续费方式，新增 `renewal_mode` query 参数，不复用 `renewal_decision`。

### 监控阈值

- CPU、Memory、Disk、Inode 使用三层阈值：`warning`、`alert`、`critical`。
- IOWait、Load5 当前 settings 只有两层：`warning`、`critical`。本轮在 evaluator 内派生 alert 为中点，避免新增 settings 字段和迁移：
  - `alert = warning + (critical - warning) / 2`
  - 默认保持 settings 的 `20/50` 与 `4.0/8.0`。
- Heartbeat `stale_threshold_intervals` 表示 alert 边界：
  - notice = `max(1, threshold - 1)`
  - alert = `threshold`
  - critical = `threshold + 2`
  - 默认 threshold=3 时保持原有 2/3/5 行为。

### 非运行态监控实例

- `monitoring_status = 维护中` 或 `暂停`，以及 lifecycle `已退役`，不产生新的 heartbeat stale active incident。
- 已有 active incident 的长期收敛策略需要更完整的 store mutation/audit 设计，本轮至少保证周期 sweep 不新增误报。
- Target pause/archive active incident 收敛保留为后续任务，因为 probe/resource incident 涉及 target sweep、state event 和现有 active incident 历史策略。

### IP 质量 stale

- 新增 `ip_quality_settings.stale_after_seconds`，默认 7 天，保持兼容。
- Settings validation 要求 stale window 至少不小于采集周期，避免“还没到下一次采集就判过期”。
- 现有 SQL view 不能从 JSON settings 动态参数化。为降低迁移风险：
  - 迁移保留 view 默认 stale 7 天。
  - store/service 读出 summary 后按 settings 重新计算 `stale`。
  - 资产决策和 VPS API 使用 store 层修正后的 summary。

## 数据流

### 订阅筛选

HTTP query -> `subscriptions.ListFilters` -> `NormalizeListFilters` -> `ValidateListFilters` -> store SQL `exists vps_assets.renewal_decision = $n`。

边界：handler 只解析 query，不做枚举判断；domain type 负责 validation。

### 阈值

Settings JSON -> `centersettings.IncidentDefaults` -> `incidents.MetricThresholdsFromDefaults` -> evaluator severity。

前端：`GET /api/settings` -> `resolveThresholds` -> monitoring detail metrics / monitoring list trend cell props。没有 settings 时用 `DEFAULT_THRESHOLDS` fallback。

### Heartbeat stale

Settings JSON -> `incidentTiming` 同时携带 heartbeat interval、sweep interval、stale threshold intervals -> `EvaluateMonitoringInstanceHeartbeatMissing` -> `heartbeatSeverity`。

### IP 质量

Settings JSON -> `IPQualitySettings.StaleAfterSeconds` -> store summary/read model correction -> API / asset decisions / frontend badge。

## 页面策略

- 监控详情指标卡显示 warning、alert、critical 三层阈值线；IOWait/Load5 显示 warning/alert(派生)/critical。
- 监控列表趋势色调使用同一阈值 props，避免默认配置与列表提示漂移。
- 迁移文案降级为“标记迁移意向/人工跟进”，不再写“推进迁移”或暗示 VPS 详情里有迁移工作台。

## 兼容与回滚

- 新增 settings 字段必须有 JSON 默认、validation fallback 和前端可选字段兼容。
- 不修改已发布 migration；如需 schema/default 变化，新增 `0047_*`。
- 普通 PATCH lifecycle 限制可能拒绝旧客户端直接写流程状态。这是预期收口；危险流程必须走已有 endpoint。
- 回滚方式：恢复 domain validation/evaluator/前端阈值调用，新增 settings 字段保留无害。

## 后续任务

- 完整迁移 workbench：新增 `migrate_vps` lifecycle action、source/target VPS 关系、cutover step、readback 和旧机 closure。
- 完整 lifecycle 状态矩阵与审计：归档/恢复、取消/退役、迁移、替换全部写统一 action/step。
- Target pause/archive active incident 收敛：定义 probe incident recovery/suspended 策略并实现。
- 订阅来源拆分：抽奖、赠送、Bonus/余额抵扣从 renewal mode 中拆出。
