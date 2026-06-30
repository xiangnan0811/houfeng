# 状态生命周期一致性审查

日期：2026-06-29  
范围：VPS、订阅/续费、监控/Target、告警阈值、提醒、资产决策、主要前端页面。  
结论类型：审查与方案，不实施业务改动。

## 审查方法

- 从用户视角先列出会影响判断和操作的页面：Dashboard、VPS 列表、VPS 详情、资产决策、订阅、归档、监控详情、Target、设置。
- 再向下追踪数据链路：前端类型/API client -> HTTP handler -> domain validation -> store SQL -> DB migration 约束 -> 决策/提醒/告警服务。
- 对状态做三类判断：
  - 单一枚举是否一致：同一字段在 DB、Go、前端是否接受同一组值。
  - 多轴状态是否有合同：例如 VPS lifecycle、usage、renewal decision 能否组合，哪些组合需要阻止或提醒。
  - 状态变化是否有审计和联动：例如取消、迁移、归档、暂停是否影响订阅、监控、Target、告警、提醒。
- 页面实际效果验证：
  - Vite 预览可访问：`http://127.0.0.1:5181/` 返回 200。
  - 未登录访问 `/vps?renewal_decision=migrate` 会按预期跳到 `/login?next=%2Fvps%3Frenewal_decision%3Dmigrate`。
  - 仓库标准 browser sanity helper 依赖本机 Python Playwright；当前环境 `import playwright` 失败，因此无法完成标准截图/几何自动化。此限制已作为证据缺口记录。

## 状态清单

### VPS

字段与来源：

- `vps_assets.lifecycle_status` / `VPSLifecycleStatus`
  - `active`
  - `idle`
  - `testing`
  - `to_migrate`
  - `to_cancel`
  - `cancelled`
  - `archived`
- `vps_assets.usage_status` / `VPSUsageStatus`
  - `in_use`
  - `idle`
  - `standby`
  - `testing`
  - `unknown`
- `vps_assets.renewal_decision` / `VPSRenewalDecision`
  - `unreviewed`
  - `keep`
  - `observe`
  - `migrate`
  - `cancel`
  - `auto_renew_cancelled`
  - `replaced`
- `asset_scope`
  - `current`
  - `archived`
  - `all`

主要影响：

- VPS 列表筛选、归档页、Dashboard 数字、资产决策队列、订阅上下文、取消/退役 workbench、成本/续费提醒、监控/Target 运行数量。

### 订阅/续费

字段与来源：

- `subscriptions.status`
  - `active`
  - `paused`
  - `cancelled`
  - `expired`
  - `unknown`
- `subscriptions.renewal_mode`
  - `auto`
  - `manual`
  - `auto_cancelled`
  - `lottery`
  - `bonus`
  - `other`
- legacy flags
  - `auto_renew`
  - `auto_renew_cancelled`
- 续费提醒
  - `subscription_reminder_deliveries.reminder_kind`: `renewal`, `decision_attention`
  - `delivery_status`: `sent`, `suppressed`, `failed`

主要影响：

- 订阅列表和详情、续费窗口、成本换算、预算风险、通知发送、VPS 决策上下文。

### 监控实例

字段与来源：

- lifecycle
  - `待接入`
  - `在用`
  - `观察中`
  - `不续费`
  - `已退役`
- runtime monitoring status
  - `启用`
  - `维护中`
  - `暂停`
- binding
  - `未绑定`
  - `已绑定`
  - `指纹变更待确认`
- health
  - `正常`
  - `关注`
  - `告警`
  - `严重`

主要影响：

- 监控列表、监控详情、VPS 详情关联监控、取消/退役 workbench、heartbeat stale sweep、active incident 展示、通知。

### Target

字段与来源：

- `run_status`
  - `启用`
  - `维护中`
  - `暂停`
  - `已归档`
- target type
  - `service`
  - `china_reference`
- probe kind
  - `tcp`
  - `http`
  - `tls`
- frequency tier
  - `5s`
  - `1m`
  - `5m`
  - `15m`
  - `6h`

主要影响：

- Target 列表/详情、探测执行、告警、资产上下文、取消/迁移联动。

### 告警与阈值

字段与来源：

- incident severity / health
  - `normal`
  - `notice`
  - `alert`
  - `critical`
- evaluation transition
  - `noop`
  - `started`
  - `escalated`
  - `recovered`
  - `skipped`
- 默认阈值
  - CPU: warning 80, alert 90, critical 95
  - Memory: warning 85, alert 92, critical 95
  - Disk: warning 85, alert 92, critical 97
  - Inode: warning 80, alert 90, critical 95
  - IOWait: warning 20, critical 50
  - Load5: warning 4.0, critical 8.0
  - Heartbeat stale: interval 5s, stale threshold 3 intervals, sweep 5s

主要影响：

- 监控详情图表、监控列表趋势、告警创建/升级/恢复、通知、Dashboard 风险、资产决策证据。

### IP 质量

字段与来源：

- report status: `success`, `partial`, `failure`
- provider/probe status: `success`, `failure`, `skipped`, `not_configured`
- stale: read model 中固定 `observed_at < now() - interval '7 days'`

主要影响：

- VPS badge、IP 质量页、资产决策 evidence chip、迁移/保留判断。

## 关键转换链路

### VPS 取消/退役

当前强控制链路：

1. VPS 详情或资产决策页引导打开取消/退役 workbench。
2. `ApplyVPSCancellation` 写 `asset_lifecycle_actions`。
3. `applyVPSCancellationState` 更新 VPS lifecycle 与 renewal decision。
4. 同一动作下写 lifecycle action step。
5. 可联动订阅、监控实例、Target。

风险点：

- 普通 VPS PATCH 也能改变多数 lifecycle 状态，但不走 lifecycle action/step。
- 归档/恢复是专门 endpoint，但也没有写 `asset_lifecycle_actions` / `asset_lifecycle_action_steps`。

### VPS 迁移

当前链路：

1. VPS lifecycle 有 `to_migrate`，renewal decision 有 `migrate` / `replaced`。
2. 资产决策会把迁移归到 migration lane。
3. 页面上有“迁移”队列与“打开 VPS 详情推进迁移”文案。
4. lifecycle action type 只有 `cancel_vps` 和 `extend_validity`，没有迁移 action type。

风险点：

- 迁移不像取消那样有 controlled workflow，无法记录旧/新 VPS 关系、服务/domain/Target cutover、替换完成、旧机归档。

### 订阅续费与提醒

当前链路：

1. 订阅 `status` 与 `renewal_mode` 管理账单状态。
2. VPS `renewal_decision` / `lifecycle_status` 影响续费提醒类型。
3. reminder candidate 仅取 active subscription 且排除 archived/cancelled VPS。
4. `cancel` / `auto_renew_cancelled` / `migrate` / `to_cancel` / `to_migrate` 被归为 `decision_attention`。

总体判断：

- 提醒候选的主逻辑合理。
- 但订阅列表的 `renewal_decision` 筛选验证错误，会影响用户在订阅上下文里查看这些提醒/队列相关数据。

### 监控/Target 暂停、维护、退役、归档

当前链路：

- 监控实例 pause/retire/archive 会把 `monitoring_status` 置为 `暂停`。
- Target pause/archive 会把 `run_status` 改为 `暂停` / `已归档`。
- evaluator 对 sample/probe 里的 `MaintenanceContext` 有 suppress/recover 逻辑。
- heartbeat stale sweep 调用 active-scope `ListMonitoringInstances`，该列表按归档和关联 VPS currentness 过滤，但不显式过滤 `monitoring_status='暂停'` / `维护中` / lifecycle `已退役`。
- periodic target sweep 遍历 `ListTargets`，需要确认该列表是否按 `run_status` 过滤，以及 paused/archived active incidents 是否需要主动关闭。

风险点：

- 当前代码有部分维护抑制测试，但暂停/退役/归档的 heartbeat 和 active incident 收敛策略没有形成清晰合同。

## 问题清单

### SLC-01 P1：订阅 `renewal_decision` 筛选用错枚举

证据：

- HTTP handler 接收 `renewal_decision` query：`internal/center/http/handlers/subscriptions.go:286`
- Store 用它筛 `vps_assets.renewal_decision`：`internal/center/store/subscriptions.go:168`
- normalization/validation 却调用订阅 `NormalizeRenewalMode` / `IsValidRenewalMode`：`internal/center/subscriptions/types.go:531`、`internal/center/subscriptions/types.go:569`
- `IsValidRenewalMode` 只接受 `auto/manual/auto_cancelled/lottery/bonus/other`：`internal/center/subscriptions/types.go:651`
- 前端 `listSubscriptions` 也会传 `renewal_decision`：`web/src/lib/api.ts:867`

影响：

- 用户在订阅上下文里按 VPS 决策筛选 `cancel`、`migrate`、`keep`、`observe` 会被 400 拒绝。
- 这会破坏订阅页、资产决策页或未来深链中“按取消/迁移决策查看订阅”的路径。
- 最容易影响续费前判断：用户以为没有相关订阅，实际只是筛选失败或无法筛选。

根因：

- `renewal_mode` 是订阅账单续费方式；`renewal_decision` 是 VPS 续费决策。二者字段名接近但状态空间不同。

解决方案：

- 保持 query 参数 `renewal_decision` 兼容，但内部类型改成 VPS renewal decision 语义。
- validation 改用 `vpsassets.IsValidRenewalDecision`。
- 如果需要同时支持订阅 `renewal_mode` 筛选，应新增独立 query 参数 `renewal_mode`，不要复用 `renewal_decision`。
- 增加测试：
  - `ListFilters{RenewalDecision: "migrate"}` 应通过。
  - `ListFilters{RenewalDecision: "manual"}` 是否允许取决于产品定义；若是 VPS 决策，应拒绝。
  - handler/store/api client 覆盖 `/api/subscriptions?renewal_decision=migrate`。

### SLC-02 P1：VPS lifecycle 可被普通 PATCH 改变，绕过受控生命周期审计

证据：

- `ValidatePatchInput` 只校验 lifecycle 枚举是否合法：`internal/center/vpsassets/types.go:436`
- handler 只禁止直接 PATCH 到 `archived`，并禁止修改当前已归档资产：`internal/center/http/handlers/vps.go:196`
- Store 直接写 `lifecycle_status` 并根据 archived 切换 `archived_at`：`internal/center/store/vps_assets.go:638`
- `recordVPSAssetHistoryChanges` 记录续费决策、IP、规格，但不记录 lifecycle 转换：`internal/center/store/vps_assets.go:387`
- 取消 workbench 会写 action 与 step：`internal/center/store/asset_lifecycle.go:982`、`internal/center/store/asset_lifecycle.go:1220`
- 归档/恢复 endpoint 使用专门 store 方法，但没有写 lifecycle action/step：`internal/center/store/asset_lifecycle.go:137`、`internal/center/store/asset_lifecycle.go:184`

影响：

- 用户或前端路径可以让 VPS 进入 `to_cancel` / `cancelled` / `to_migrate` 等流程状态，但没有 workbench 的影响范围、阻塞项、订阅/监控/Target 联动，也没有统一审计。
- 后续资产决策和成本提醒会把这些状态当作真实流程状态，导致“状态看起来已进入流程，但实际联动未完成”。

根因：

- lifecycle 同时被当成普通资产字段和流程状态，缺少状态机边界。

解决方案：

- 定义 VPS lifecycle 状态机：
  - 普通 PATCH 只允许编辑非流程状态，或只允许 `active/idle/testing` 这类低风险状态。
  - `to_cancel/cancelled/archived` 必须通过 lifecycle endpoint。
  - `to_migrate/replaced` 如果保留为流程状态，也必须通过迁移 endpoint。
- 所有 lifecycle 状态变化写统一审计：
  - `asset_lifecycle_actions`
  - `asset_lifecycle_action_steps`
  - 或明确新增轻量 lifecycle transition history 表。
- 归档/恢复也应记录 action/step，至少记录 actor、from/to、reason、blockers snapshot。
- 增加测试：
  - 普通 PATCH 不允许进入流程/终态。
  - lifecycle endpoint 写 action/step。
  - archive/restore 审计可查。

### SLC-03 P1：迁移有状态和决策，但没有受控迁移工作流

证据：

- VPS lifecycle 有 `to_migrate`，renewal decision 有 `migrate` / `replaced`：`internal/center/vpsassets/types.go:21`、`internal/center/vpsassets/types.go:51`
- lifecycle action type 只有 `cancel_vps` / `extend_validity`：`internal/center/assetlifecycle/types.go:24`
- DB constraint 也只允许这两个 action type：`db/migrations/0031_subscription_periods_and_validity_extension.sql:157`
- 资产决策会识别迁移并进入 migration lane：`internal/center/assetdecisions/types.go:1130`、`internal/center/assetdecisions/execution_plan.go:139`
- readback 会检查 migration 状态和旧承载残留：`internal/center/assetdecisions/readback.go:234`
- VPS 详情的 lifecycle coordination 主要是取消 workbench，文案却提到“取消或迁移流程”：`web/src/pages/vps-detail/VPSDecisionBoard.tsx:141`

影响：

- 用户看到“迁移”像一个可推进流程，但系统没有迁移 workbench 来记录：
  - 目标新 VPS。
  - 服务、域名、Target、监控实例切换。
  - 旧 VPS 何时变成 `replaced` / `cancelled` / `archived`。
  - 迁移过程中的验证和回滚。
- 迁移决策可能长期停留在标签状态，无法形成可靠闭环。

根因：

- 决策模型先支持了迁移，但生命周期动作模型只实现了取消/延期。

解决方案：

- 两个可选产品方向，必须择一：
  - 方向 A：实现迁移 workbench。新增 action type，例如 `migrate_vps`，记录 source VPS、target VPS、服务/domain/Target/monitoring cutover step、readback、旧机 closure。
  - 方向 B：明确迁移只是人工决策标签，不承诺流程。移除或弱化“推进迁移”的流程文案，`to_migrate` 不作为 lifecycle 流程状态，readback 只做提醒。
- 如果选择 A，迁移流程至少应包含：
  - 新旧 VPS 关联。
  - 承载对象迁移清单。
  - 切换验证清单。
  - 旧 VPS 标记为 `replaced` 或进入取消 workbench。
  - 迁移失败/回滚记录。

### SLC-04 P2：VPS 多轴状态缺少组合排他与冲突合同

证据：

- lifecycle、usage、renewal decision 独立校验枚举，未校验组合：`internal/center/vpsassets/types.go:436`
- Store 独立写三个字段：`internal/center/store/vps_assets.go:657`
- 资产决策有冲突 readback，例如 keep 但 terminal lifecycle 会报 conflict：`internal/center/assetdecisions/readback.go:241`
- 归档 fixture/历史路径中可能出现 `archived` + `keep` 一类组合，说明当前模型允许终态与保留判断并存。

影响：

- 用户可能看到互相矛盾的状态，例如：
  - lifecycle `cancelled` 但 renewal decision `keep`
  - lifecycle `to_migrate` 但 renewal decision `cancel`
  - lifecycle `active` 但 renewal decision `replaced`
  - usage `in_use` 但 lifecycle `cancelled`
- 决策系统会尝试 readback，但用户在列表/详情/订阅/提醒中仍会先看到冲突状态，容易误操作。

根因：

- 缺少状态轴定义：哪些是事实状态，哪些是意图状态，哪些是流程状态，哪些允许历史保留。

解决方案：

- 建立 VPS 状态矩阵：
  - lifecycle 是事实/流程状态。
  - usage 是使用事实。
  - renewal decision 是续费前处理意图。
- 定义禁止组合、需要 warning 的组合、允许历史组合。
- 在写路径加入校验或 normalization：
  - 进入 `cancelled` 时 renewal decision 应为 `cancel` 或 `auto_renew_cancelled`。
  - 进入 `to_migrate` 时 renewal decision 应为 `migrate`。
  - `replaced` 只能配合迁移 closure 或 archived/cancelled 后历史。
- 前端列表和详情对冲突组合显示“状态冲突”而不是让用户自行推断。
- 增加 table-driven tests 覆盖状态矩阵。

### SLC-05 P2：前端监控阈值默认值和后端不一致，且丢失 alert 层级

证据：

- 后端默认 Disk warning/alert/critical 是 85/92/97：`internal/center/settings/types.go:201`
- 后端 `MetricThresholds` 有 warning/alert/critical 三层：`internal/center/incidents/types.go:291`
- 前端 `DEFAULT_THRESHOLDS` 注释说匹配后端，但只有 notice/critical，Disk 是 80/95：`web/src/config/thresholds.ts:1`
- Watchtower 图表直接用静态 `DEFAULT_THRESHOLDS`：`web/src/components/monitoring-detail/MonitoringInstanceWatchtowerMetrics.tsx:128`
- 磁盘 tooltip 和图表线显示 80/95：`web/src/components/monitoring-detail/MonitoringInstanceWatchtowerMetrics.tsx:247`
- 监控列表 trend cell 也使用 `DEFAULT_THRESHOLDS`：`web/src/pages/monitoring/MonitoringInstancesTrendCell.tsx:23`

影响：

- 用户在图表里看到“严重 ≥95%”，但后端告警可能在 92% alert、97% critical 触发。
- 设置页修改阈值后，图表仍可能按静态默认显示，用户无法理解为什么告警和图表不同步。
- `notice/alert/critical` 在后端是三层，前端压缩成两层，会让“告警”状态缺少视觉解释。

根因：

- 前端阈值模型和后端 evaluator 阈值合同分叉。

解决方案：

- 建立统一阈值 API contract：
  - CPU/mem/disk/inode 均包含 warning/alert/critical。
  - 前端图表、列表 trend、设置页使用同一解析函数。
- `resolveThresholds` 应真正接入监控详情/列表，而不是只存在配置文件中。
- 前端默认值从后端默认值同步，或后端提供 `/api/settings` 后以前端只做 fallback。
- 增加测试：
  - 前端 `DEFAULT_THRESHOLDS` 与后端 defaults 的 parity test，或快照式测试。
  - 设置为非默认值后，图表阈值线跟随变化。

### SLC-06 P2：IOWait/Load5 设置暴露给用户，但后端 evaluator 未使用同一设置

证据：

- settings 暴露 `IOWaitWarningPct` / `IOWaitCriticalPct` / `Load5Warning` / `Load5Critical`：`internal/center/settings/types.go:72`
- 设置页可编辑这些字段：`web/src/pages/settings/IncidentDefaultsSection.tsx:91`
- `MetricThresholds` 不包含 iowait/load5：`internal/center/incidents/types.go:291`
- `MetricThresholdsFromDefaults` 只读取 CPU/mem/disk/inode：`internal/center/incidents/service.go:527`
- evaluator 对 load/iowait 使用硬编码 2.5/1.8/1.2 与 20/10：`internal/center/incidents/evaluator.go:411`

影响：

- 用户在设置页修改 IOWait/Load5 阈值，以为会影响告警；实际 evaluator 仍按硬编码判断。
- 图表用前端默认的 20/50、4/8，后端判断又是 10/20、1.2/1.8/2.5，三套语义并存。

根因：

- 设置模型先暴露了字段，但 evaluator contract 没完成。

解决方案：

- 将 IOWait/Load5 纳入 `MetricThresholds`，明确是否需要 warning/alert/critical 三层。
- 统一 normalized load5 与原始 load5 的命名和单位；当前 evaluator 使用 `NormalizedLoad5`，设置页显示 `Load5`。
- 如果暂不支持自定义，应从设置页移除或标注为展示/预留，不要保存成“看似生效”的配置。
- 增加 evaluator tests：自定义 iowait/load 阈值会改变 severity。

### SLC-07 P2：heartbeat stale threshold 设置没有进入 heartbeat severity 计算

证据：

- settings 有 `StaleThresholdIntervals`：`internal/center/settings/types.go:51`
- 默认值是 3：`internal/center/settings/types.go:188`
- `applyIncidentDefaults` 只应用 heartbeat interval 和 sweep interval：`internal/center/incidents/service.go:621`
- heartbeat severity 硬编码 missed >=2/3/5：`internal/center/incidents/evaluator.go:362`
- 已有测试覆盖 heartbeat interval，但未覆盖 stale threshold 生效。

影响：

- 用户在设置页修改“失联判定阈值”，可能不会改变 notice/alert/critical 的真实分界。
- heartbeat 是最容易影响用户焦虑和通知噪音的告警之一，这类设置失效会明显损害信任。

根因：

- stale threshold 作为配置进入 settings，但没有成为 evaluator 输入。

解决方案：

- 明确 stale threshold 的语义：
  - 方案 A：`stale_threshold_intervals` 表示 notice 起点，alert/critical 按倍数推导。
  - 方案 B：拆成 heartbeat notice/alert/critical intervals 三个字段。
- 将阈值传入 `EvaluateMonitoringInstanceHeartbeatMissing`。
- 设置页文案要说明单位和各级别含义。
- 增加测试覆盖：
  - stale threshold 改成 4 时 missed=3 不触发。
  - alert/critical 边界符合产品合同。

### SLC-08 P2：暂停/维护/退役/归档对 active incident 与 heartbeat sweep 的合同不完整

证据：

- active-scope `ListMonitoringInstances` 只按 `archived_at` 和关联 VPS currentness 过滤，不按 `monitoring_status` 或 lifecycle retired 过滤：`internal/center/store/monitoring_instances.go:347`
- heartbeat stale sweep 对列表中的每个 monitoring instance 评估 heartbeat：`internal/center/incidents/service.go:398`
- retire/archive/pause 会把 monitoring status 设为 `暂停`：`internal/center/store/monitoring_instances.go:441`、`internal/center/store/monitoring_instances.go:551`、`internal/center/store/monitoring_instances.go:1536`
- permanent cleanup 会删除 active incidents，但普通 pause/retire/archive 片段没有同等删除：`internal/center/store/monitoring_instances.go:653`
- Target pause/archive 写 run status 和事件，但没有看到 active incident 收敛：`internal/center/store/targets.go:430`、`internal/center/store/targets.go:523`
- periodic target sweep 遍历 `ListTargets`：`internal/center/incidents/service.go:379`
- evaluator 对 `MaintenanceContext` 有 suppressed/recovery 测试，说明维护采样上下文已有部分机制，但这不是暂停/退役/归档的完整合同。

影响：

- 暂停或退役对象是否应继续显示 active incident，目前不够清晰。
- 如果保留 active incident 是历史提醒，页面需要标注“暂停前遗留”；如果应关闭，则需要进入 pause/retire/archive 时主动 recover/clear。
- heartbeat stale 如果继续扫描暂停/退役实例，可能产生不符合用户预期的失联提醒。

根因：

- runtime status、lifecycle status、incident active 状态之间没有统一状态机。

解决方案：

- 定义明确策略：
  - `维护中`：新 incident suppress；已有 incident 可静默恢复；是否保留 active 由合同决定。
  - `暂停`：不产生 heartbeat/probe/resource 新 incident；已有 active incident 应转 recovered 或标为 suspended。
  - `已退役` / `已归档`：不参与周期 sweep；active incident 应关闭或只保留历史事件。
- 将策略落实在 store/service 两端：
  - 列表过滤。
  - evaluator skip。
  - 状态切换时 active incident cleanup/recovery。
- 增加测试：
  - pause 后 heartbeat sweep 不新建 incident。
  - retire/archive 后 active incident 不再出现在当前健康状态。
  - maintenance 的现有 suppress/recovery 行为不回退。

### SLC-09 P2：IP 质量 stale 固定 7 天，与采集周期设置脱节

证据：

- IP 质量设置有 `FrequencySeconds`，默认 24h：`internal/center/settings/types.go:231`
- read model 固定 `observed_at < now() - interval '7 days' as stale`：`db/migrations/0041_filter_ip_quality_read_models.sql:76`、`db/migrations/0042_extend_ip_quality_source_details.sql:114`
- stale 会成为资产决策证据 `EvidenceIPQualityStale`：`internal/center/assetdecisions/types.go:112`

影响：

- 如果用户把采集周期调成更长或更短，stale 判断仍固定 7 天。
- 资产决策可能错误地提示“IP 质量过期”或漏提示，影响迁移/保留判断。

根因：

- stale 是 read model 固定表达式，而不是 settings 派生或显式 stale-after 配置。

解决方案：

- 新增显式 `IPQuality.StaleAfterSeconds`，默认可为 `max(7 days, frequency * N)`，或直接由 frequency 派生。
- read model 不应硬编码全局 7 天；可以在查询层计算，或把 stale-after 写入可配置 SQL 参数。
- 资产决策 evidence chip 文案显示过期窗口，例如“超过 7 天未更新”。
- 增加测试：
  - 不同 frequency/stale-after 下 summary.stale 变化符合预期。

### SLC-10 P3：订阅 renewal mode 的“抽奖/赠送”语义被合并，可能无法满足用户区分需求

证据：

- 后端 renewal mode 有 `lottery` 和 `bonus`，无 `gift`：`internal/center/subscriptions/types.go:42`
- 前端类型一致：`web/src/lib/types.ts:757`
- 前端 label 把 `lottery` 显示为“抽奖/赠送”，`bonus` 显示为“Bonus/余额抵扣”：`web/src/lib/assetOptions.ts:61`

影响：

- 用户明确提到“赠送、抽奖”等状态；当前如果需要统计或筛选“中奖获取”和“商家赠送”，系统无法区分。
- `bonus` 又同时承载余额抵扣语义，不适合表达免费赠送时长。

根因：

- 账单来源、续费方式、优惠/赠送来源被压缩到一个 `renewal_mode`。

解决方案：

- 如果产品确实需要区分，新增 `gift` / `promo` / `compensation` 等来源字段，或把“续费方式”和“费用来源”拆开。
- 最小修复是调整 label：
  - `lottery`: 抽奖
  - `bonus`: Bonus/余额抵扣
  - 新增 `gift`: 赠送
- 注意 DB constraint、Go enum、前端类型、表单选项、统计和测试同步。

### SLC-11 P3：`asset_scope=archived` 实际包含 cancelled 与 archived，命名容易误解

证据：

- VPS archived scope 查询 `cancelled OR archived`：`internal/center/store/vps_assets.go:132`
- 订阅 archived scope 也用 `lifecycle_status in ('cancelled', 'archived')`：`internal/center/store/subscriptions.go:174`
- 前端归档页文案写“已取消、已归档 VPS”：`web/src/pages/ArchivePage.tsx:80`

影响：

- 当前页面文案是准确的，因此不是功能错误。
- 但 API 参数名 `archived` 对开发者来说容易理解成只含 `archived`，后续可能写出错误筛选或统计。

根因：

- UI 概念“归档资产”覆盖了 cancelled 与 archived 两类历史资产，而 API scope 名称较窄。

解决方案：

- 保持兼容的情况下新增更准确 scope，例如 `historical` 或 `inactive`。
- 文档注明 `archived` scope 包含 `cancelled` 与 `archived`。
- 如果未来需要严格区分，在列表 API 中同时支持 `lifecycle_status=cancelled` 和 `asset_scope=archived` 的组合。

### SLC-12 P3：页面上的迁移入口和取消入口不对等，容易强化“迁移已流程化”的误解

证据：

- 资产决策页有迁移队列：`web/src/pages/AssetDecisionsPage.tsx:2894`
- 单台队列文案只说明取消/退役从 VPS lifecycle workbench 进入：`web/src/pages/AssetDecisionsPage.tsx:4937`
- 取消项有“取消/退役”链接，迁移项只有普通“处理”保存决策：`web/src/pages/AssetDecisionsPage.tsx:5078`
- VPS 决策表单只保存续费决策和理由：`web/src/pages/vps-detail/VPSRenewalDecisionForm.tsx:43`

影响：

- 用户看到迁移是一级队列，但进入后只能保存 `migrate` 决策，没有迁移 checklist。
- 对复杂迁移场景，用户体验会从“系统在协助闭环”降级为“系统只打标签”。

根因：

- 页面 IA 已经把迁移作为重要动作，但后端流程能力未跟上。

解决方案：

- 跟 SLC-03 同步决策：
  - 如果实现迁移 workbench，页面增加明确“打开迁移工作台”。
  - 如果不实现，页面文案改为“标记迁移意向/人工跟进”，不要使用“推进迁移”。

## 用户体验影响

### 续费前判断

高风险路径：

- 用户在资产决策或订阅页想查看“迁移/取消相关订阅”。
- 前端/API 传 `renewal_decision=migrate` 或 `cancel`。
- 订阅后端用错误枚举拒绝。
- 用户无法得到完整续费上下文，可能错过取消自动续费、迁移前延长、或手动续费窗口。

### 生命周期操作

高风险路径：

- 用户或未来页面直接 PATCH lifecycle 到 `to_cancel` 或 `to_migrate`。
- 系统页面和提醒都相信该状态。
- 订阅、监控、Target 没有经过 workbench 联动。
- Dashboard/决策页出现“已进入流程但仍有运行对象”的冲突。

### 监控可信度

高风险路径：

- 用户调整设置页阈值。
- 图表和后端告警仍使用不同阈值。
- 用户看到图表未达“严重线”，却收到严重告警；或反之。
- 暂停/退役对象如果继续显示 active incident，用户无法判断是历史遗留还是当前风险。

### 迁移闭环

高风险路径：

- 用户把 VPS 标记为迁移。
- 系统将其放入迁移队列，但没有迁移过程状态。
- 旧 VPS 的服务/domain/Target/监控仍可能保留。
- 资产决策 readback 会提示旧承载残留，但没有受控动作去解决。

## 推荐实施拆分

### Follow-up A：修复订阅筛选枚举合同

优先级：P1  
范围：后端 subscription filters、handler/store tests、前端 API/page tests。  
验收：

- `/api/subscriptions?renewal_decision=migrate` 返回合法结果或空数组，不返回 400。
- `renewal_mode` 与 `renewal_decision` 参数语义分离。

### Follow-up B：定义 VPS lifecycle 状态机与审计合同

优先级：P1  
范围：VPS patch validation、lifecycle endpoints、asset_lifecycle audit、归档/恢复审计。  
验收：

- 普通 PATCH 不能绕过流程状态。
- 每次流程/终态转换都有审计记录。
- 状态组合矩阵有 table-driven tests。

### Follow-up C：迁移能力产品决策

优先级：P1/P2  
范围：产品合同、后端 action type、前端工作台或文案降级。  
验收：

- 要么有迁移 workbench 和闭环 step。
- 要么迁移只作为人工标签，页面不再暗示系统流程化推进。

### Follow-up D：统一监控阈值合同

优先级：P2  
范围：settings、incidents evaluator、前端 threshold config、监控详情/列表图表、测试。  
验收：

- CPU/mem/disk/inode/heartbeat/iowait/load5 的设置、后端判断、前端图表一致。
- alert 层级在前端可见。
- Settings 修改能影响 evaluator 和图表。

### Follow-up E：暂停/维护/退役/归档的 incident 收敛策略

优先级：P2  
范围：incident service、monitoring/target store transitions、页面说明、测试。  
验收：

- 暂停/退役/归档对象不会生成新的 heartbeat/probe/resource active incident，除非产品明确允许。
- 进入非运行态时 active incident 的关闭/保留策略可测试、页面可解释。

### Follow-up F：IP 质量 stale 配置化

优先级：P2/P3  
范围：settings、IP quality read model/query、资产决策 evidence、页面文案。  
验收：

- stale 窗口与采集周期或显式配置一致。
- evidence chip 显示可解释的过期窗口。

### Follow-up G：订阅来源/赠送/抽奖语义整理

优先级：P3  
范围：枚举、DB constraint、前端 label、统计筛选。  
验收：

- 抽奖、赠送、Bonus/余额抵扣能按产品需要区分。

## 建议验证命令

只读审查可运行：

```bash
go test ./internal/center/subscriptions ./internal/center/incidents ./internal/center/assetdecisions ./internal/center/store
go test ./internal/center/incidents -run 'Threshold|Heartbeat|Resource|Maintenance|Stale'
cd web && npm test -- --run MonitoringPage.test.tsx DashboardPage.test.tsx SettingsPage.test.tsx AssetDecisionsPage.test.tsx VPSDetailPage.test.tsx SubscriptionsPage.test.tsx
cd web && npm run build
```

页面 sanity：

```bash
python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5181/ \
  --mock-api asset-workflows \
  --route /asset-decisions \
  --route /vps \
  --route /subscriptions \
  --viewport 1440x1000 \
  --viewport 390x900

python3 scripts/visual_evidence.py browser-sanity \
  --base-url http://127.0.0.1:5181/ \
  --mock-api observability-support \
  --route /monitoring \
  --route /targets \
  --route /events \
  --viewport 1440x1000 \
  --viewport 390x900
```

当前环境限制：

- Python Playwright 未安装，标准 browser sanity helper 不能运行。
- 不应为了审查任务把 Playwright/Cypress/WebDriverIO 加进 `web/package.json`。

## 本任务改动边界

- 本文档是审查产物。
- 未修改业务 Go 代码。
- 未修改 DB migration。
- 未修改前端实现。
- 未执行迁移、未改线上数据、未提交 PR。
