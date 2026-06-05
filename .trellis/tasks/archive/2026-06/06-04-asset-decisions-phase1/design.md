# 资产组合决策中枢 Phase 1 技术设计

## 设计原则

1. **VPS-first 不变**：组合页只聚合证据和提供入口，不成为第二套业务状态机。
2. **只读读模型**：自动组完全由现有表实时派生，不新增持久化状态。
3. **后端统一语义**：组合判断逻辑放在 `assetdecisions` 后端读模型，避免 Dashboard、VPS、订阅、服务商页各自前端 join 后漂移。
4. **建议不是执行**：`suggested_role` / `suggested_action` 只帮助排序和扫描；真正修改仍回到 VPS detail 或 `PATCH /api/vps/{id}`。
5. **证据可信优先**：请求失败与真实缺口分开表达，不能把不可用误报为缺订阅、健康或无风险。

## 后端架构

新增领域包：

- `internal/center/assetdecisions/types.go`

新增 store：

- `internal/center/store/asset_decisions.go`

新增 handler：

- `internal/center/http/handlers/asset_decisions.go`

新增路由与 wiring：

- `internal/center/http/router.go`
- `cmd/houfeng-center/bootstrap.go`
- `cmd/houfeng-center/bootstrap_test.go`
- `internal/center/http/router_api_test.go`

### Repository 接口

```go
type Repository interface {
  GetOverview(context.Context, ListFilters) (Overview, error)
  ListGroups(context.Context, ListFilters) ([]GroupSummary, error)
  GetGroup(context.Context, string, ListFilters) (GroupDetail, error)
}
```

`ListFilters` 包含：

- `View View`
- `RenewWithinDays int`

默认 `renew_within_days=30`，允许 `30/60/90`。

### API Contract

```text
GET /api/asset-decisions/overview?renew_within_days=30
GET /api/asset-decisions/groups?view=needs_decision&renew_within_days=30
GET /api/asset-decisions/groups/adg_auto_xxxxxx?renew_within_days=30
```

视图枚举：

- `needs_decision`
- `renewal`
- `region`
- `provider`
- `cost`
- `evidence`
- `single_queue`

### 数据源

只读以下现有表：

- `vps_assets`
- `providers`
- `subscriptions`
- `asset_services`
- `asset_domains`
- `vps_monitoring_instance_links`
- `monitoring_instances`
- `targets`

Phase 1 不读取 runtime facts detail endpoint，不逐台请求 `/runtime-facts`。如需异常证据，只读 `monitoring_instances.current_health_status`、`current_active_incident_count`、`current_primary_issue_summary`、Target 当前状态和已有关联计数。

### 聚合策略

Store 先通过一条或少量 SQL 查询构造 member fact rows，再在 Go 中派生自动组。这样比纯 SQL 拼 JSON 更易测试建议角色 / evidence chips，也避免前端重复逻辑。

Member fact row 包含：

- VPS 基础字段。
- provider rating / labels / note 的轻量摘要。
- primary subscription：优先 active，其次 renew_at 最近，其次 updated_at 最新。
- subscription rollup：active/inactive count、nearest renew_at、成本 base、预算状态、汇率 stale。
- service/domain/target counts。
- active monitoring link count、running monitoring count、abnormal monitoring count、active incident count、primary issue summary。
- cancellation attention reason。

### 自动组派生

`renewal_attention`：

- 非 archived、非 cancelled 的 VPS。
- 主订阅 active 且 renew_at 在窗口内。
- 或续费窗口内但 `renewal_decision=unreviewed|migrate|cancel|auto_renew_cancelled` 需要人工判断。

`cancellation_attention`：

- 复用 Dashboard/VPS 的 cancellation attention 语义：
  - inactive subscription + VPS 未进入 to_cancel/cancelled。
  - VPS to_cancel/cancelled + active subscription。
  - VPS cancel/auto_renew_cancelled + lifecycle 未同步。
  - VPS to_cancel/cancelled + running MonitoringInstance 或 Target。

`region_portfolio`：

- group key 使用 `country|region|city`，缺失字段使用 `未记录` 但不与真实值混淆。
- 只包含非 archived、非 cancelled 普通 VPS。
- 成员数 >= 2。

`provider_portfolio`：

- group key 使用 `provider_id`，缺 provider_id 时可用 `provider_name` 形成 `provider_name:<name>` 的只读组。
- 只包含非 archived、非 cancelled 普通 VPS。
- 成员数 >= 2。

`cost_pressure`：

- 包含预算风险、汇率 stale、高成本、续费临近且承载弱等证据。
- 首版不做复杂优化算法。高成本阈值按当前组内排序和 base cost 可解释地处理，例如 top cost rows 或预算风险 rows。

`evidence_gap`：

- 缺订阅、缺监控关联、缺 provider/location/access、缺服务和域名上下文。
- 区分“没有数据”和“数据源查询失败”。Store 查询失败返回错误，不构造 gap。

### 排序

每组计算 `priority`：

- cancellation attention 最高。
- renewal due + unreviewed 次高。
- cost pressure、budget risk、exchange stale。
- evidence gap。
- abnormal monitoring / active incidents。
- 成员数和成本作为 tie breaker。

### 稳定 ID

`group_id = "adg_auto_" + first12hex(sha256(group_type + "\x00" + scope_key + "\x00" + renew_window))`

详情接口调用 `ListGroups(all view)` 后按 ID 查找，再用同一 member facts 返回详情。

## 前端设计

新增类型：

- `AssetDecisionOverview`
- `AssetDecisionGroupSummary`
- `AssetDecisionGroupDetail`
- `AssetDecisionGroupMember`
- `AssetDecisionSuggestedRole`
- `AssetDecisionSuggestedAction`
- `AssetDecisionEvidenceChip`

新增 API helper：

- `getAssetDecisionOverview`
- `listAssetDecisionGroups`
- `getAssetDecisionGroup`

重构 `web/src/pages/AssetDecisionsPage.tsx`：

- 页面标题：`资产组合决策`。
- 顶部 low-density summary 展示组合组数、续费取舍、取消联动、资料缺口、预算风险。
- tabs 使用 URL-state `view`。
- 主 surface：决策组列表。
- group row 展示组名、类型、成员数、优先级、成本、续费 / 取消 / 缺口 chips、主要问题。
- group detail drawer 展示组内 VPS 对比表和操作入口。
- 保留单台辅助队列与 `AssetDecisionWorkPanel`。
- 续费表改成 `RENEWAL EVIDENCE` 次级区域。

入口融合：

- Dashboard links 改为 `/asset-decisions?view=...`。
- VPS 页增加进入组合决策 action。
- Subscriptions 表中对续费临近、预算风险或需要资产判断的行增加资产判断链接。
- Providers 行入口增加 `/asset-decisions?view=provider&provider_id=<id>`。如果后端 Phase 1 不支持 provider_id filter，前端仍可作为未来兼容 query，但页面至少显示 provider view。

## 兼容性

- 现有 `PATCH /api/vps/{id}` 续费决策 payload 不变。
- 现有 AssetDecisionsPage 测试中的 drawer 行为需要迁移到新页面结构。
- `/asset-decisions` 路由不变。
- 不新增第三方前端依赖。
- 不新增 DB migration。

## 错误和降级

- 组合 API 失败：主 surface 显示错误，单台辅助队列可尽量按现有 API 继续加载。
- 续费 evidence 失败：只影响 evidence 区，不误报缺订阅。
- group detail missing：drawer 显示 missing group 错误并允许返回列表。
- 非法 `view`：handler 返回 400；前端 URL 解析时降级到 `needs_decision`，用户切换后写回合法值。

## 非目标

- 手工保存组合组。
- 批量续费 / 批量取消 / 批量迁移。
- CPU / IO / 路由 / IP 质量智能评分。
- 修改 Subscription、MonitoringInstance、Target 或 Agent 状态。
- 外部 provider 口碑抓取。
