# Design

## Architecture and Boundaries

Monitoring 列表页应只承担运行观测列表职责。资产判断集中在资产决策页和 VPS scoped 工作台；监控详情页保留关联 VPS 表，用于排障后回到资产对象。

本任务删除 Monitoring 列表页专用的批量资产上下文链路：

- Frontend: `MonitoringPage` 不再拉取 monitoring-instance asset context，不再向表格传递 `assetContexts`。
- Table: `MonitoringInstancesTableColumns` 删除 `asset_context` 列。
- API client/types: 删除 `listMonitoringInstanceAssetContexts()` 和 `AssetContextForMonitoringInstance`。
- Backend: 删除 `GET /api/asset-context/monitoring-instances` handler、router option、bootstrap wiring、repository method 和 store query。

保留链路：

- `GET /api/asset-context/targets`
- `listTargetAssetContexts()`
- `AssetContextForTarget`
- `GET /api/monitoring-instances/{id}/vps`
- VPS cancellation/archive preview/apply/readback 相关能力

## Data Flow

删除后 `/monitoring` 的首屏数据流为：

1. `listMonitoringInstances(scope?)` 加载表格基础记录。
2. `listMonitoringInstanceSparklines()` 延迟加载趋势。
3. 本地 URL-state 筛选、排序、对比、批量操作继续基于 monitoring records 派生。

不再存在 Monitoring 列表页到 asset lifecycle repository 的批量上下文请求。

## Compatibility

删除的 monitoring-instance asset-context API 没有外部版本化合同；本仓库前端是唯一已知消费者。Target asset-context API 保持兼容。

已归档 Trellis 历史任务不改，只更新当前权威 spec，避免篡改历史记录。

## Rollback

如需回滚，恢复 `MonitoringSupportSurface`、表格 `asset_context` 列、API client/type、router/bootstrap wiring 和 store query 即可。没有数据库迁移，回滚不涉及数据恢复。
