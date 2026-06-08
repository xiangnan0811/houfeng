# 归档资产可见性改造设计

## 技术方案

- 在 `vpsassets` 增加共享 `AssetScope`：`current`、`archived`、`all`。默认 `current`，其中 current 排除 `cancelled` / `archived`，archived 仅包含 `cancelled` / `archived`。
- 将 `asset_scope` 接入 VPS、Subscriptions、MonitoringInstances、Targets 列表查询。按 ID 的详情接口不套默认 scope，确保归档页面能读取历史详情。
- 订阅列表通过 join VPS 生命周期实现 scope 过滤；成本中心和 Dashboard SQL 明确使用 current VPS 范围。
- Asset Decisions 在事实源加载或派生入口排除归档 VPS，避免任何决策视图继续引用最终不可访问资产。
- Monitoring / Targets 的 current scope 语义：隐藏“存在 VPS 关联且所有当前关联 VPS 都是归档”的对象；没有 VPS 关联或仍有关联 current VPS 的对象保留。
- 前端 API client 增加 `asset_scope` 参数。普通页面使用默认 current；`/archive` 显式请求 archived，并用现有 detail/timeline/subresource API 组成只读详情。

## 前端形态

- 新增 `/archive` 路由和侧边栏资产区入口；VPS 页保留一个到 `/archive` 的明显入口。
- 归档页是运营工具页，不做营销式页面。左侧或顶部显示归档 VPS 列表，详情区域展示：
  - VPS 基本字段和 lifecycle/archived_at；
  - 订阅历史数量、月成本合计和订阅表；
  - VPS timeline 中的续费决策、价格、IP、规格和体验日志；
  - services/domains/monitoring links/targets 的只读上下文；
  - 如现有 API 可读，展示相关 asset decision records；若无直接关联接口，仅显示 timeline 和资产上下文，不新增复杂决策反查。
- 归档页不复用 `VPSDetailPage` 的写操作 UI，只复用展示型组件或新增局部只读组件。

## 兼容与边界

- `asset_scope` 未传时行为从“全量”改为“current”，这是有意变更，用于修复普通页面污染。
- 显式 `lifecycle_status=cancelled|archived` 与默认 current 不能冲突：当用户传 lifecycle_status 时，scope 应退化为 `all` 或由 handler 允许精确状态过滤，避免老的状态筛选失效。
- 监控历史数据沿用现有 retention；归档页只展示接口还能返回的数据。
- 不新增写接口，不迁移或删除历史记录。

