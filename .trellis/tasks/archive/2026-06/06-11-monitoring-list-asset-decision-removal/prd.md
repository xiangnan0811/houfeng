# Monitoring list asset decision removal

## Goal

移除监控列表页中过度设计的资产判断层，让 `/monitoring` 回到监控实例运行观测、筛选、批量控制和列表扫描的主路径。资产决策继续集中在资产决策页、VPS 详情/取消退役工作台，以及必要的详情联动位置。

## Confirmed Facts

- 当前真实测试环境中 3 台 VPS 都已接入 agent 并正常运行，监控列表页的「资产判断支撑」区域仍占据列表上方大面积空间，压缩了监控列表主体。
- 项目已有完整资产决策页面，监控列表页不应重复承担资产判断中枢职责。
- 当前 Monitoring 列表页使用 `GET /api/asset-context/monitoring-instances` 只为列表「资产上下文」列提供取消/过期摘要；监控详情页已有独立 `GET /api/monitoring-instances/{id}/vps` 展示关联 VPS。
- Target 资产上下文、VPS 详情和取消/退役联动仍有价值，不在本任务中删除。

## Requirements

- 移除 Monitoring 列表页的「资产判断支撑」区域。
- 移除 Monitoring 列表页的「资产上下文」列和相关批量资产上下文请求。
- 删除仅服务 Monitoring 列表页的 `GET /api/asset-context/monitoring-instances` 前后端合同。
- 保留 Monitoring 详情页「关联 VPS」、VPS 详情/取消退役工作台、Target 资产上下文和资产决策页面。
- Hero 保持紧凑概览；Hero 后应直接进入视图切换、筛选、批量操作和表格。
- 更新权威设计/spec 文档，避免未来重新引入同类过度设计。

## Acceptance Criteria

- [ ] `/monitoring` 不再渲染「资产判断支撑」「资产组合决策」「资产上下文」监控列表列。
- [ ] `/monitoring` 首屏结构为页面标题/CTA、紧凑 Hero、toolbar/filter/batch/table，列表主体不再被资产判断支撑区下压。
- [ ] Monitoring 列表页不再请求 `/api/asset-context/monitoring-instances`。
- [ ] 前端不再导出或使用 `listMonitoringInstanceAssetContexts()` / `AssetContextForMonitoringInstance`。
- [ ] 后端不再注册或暴露 `GET /api/asset-context/monitoring-instances`。
- [ ] `/api/asset-context/targets`、Target 页面资产上下文、监控详情页关联 VPS 仍正常保留。
- [ ] 相关前后端测试和 mock 数据同步更新。
- [ ] `go test ./...`、`npm --prefix web test -- MonitoringPage api`、`sh scripts/verify.sh` 通过。

## Notes

- 不做数据库迁移；本任务删除的是读接口、UI 消费和文档合同，不改表结构和历史数据。
