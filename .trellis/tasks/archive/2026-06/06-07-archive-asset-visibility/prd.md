# 归档资产可见性改造

## Goal

将已取消 / 已归档 VPS 从普通运营页面、成本统计和资产决策中移出，同时提供只读归档入口保留历史数据查看能力。

## Requirements

- 归档范围以 VPS 业务生命周期为准：`vps_assets.lifecycle_status in ('cancelled', 'archived')`。
- `to_cancel` 仍属于当前资产，继续出现在普通页面、取消联动和成本/订阅整理流程中。
- 订阅自身 `cancelled` / `expired` 不会让 VPS 自动归档；在 VPS 最终变为 `cancelled` / `archived` 前仍是当前资产的取消关注信号。
- VPS、订阅、监控、Target、Dashboard、全局搜索等普通运营入口默认不展示或统计只属于归档 VPS 的资产。
- 资产组合决策中枢完全不考虑 `cancelled` / `archived` VPS，包括取消分组、资料缺口、成本、续费、地域和服务商组合。
- 归档数据必须保留可查：提供独立只读归档页面 `/archive`，展示归档 VPS 基本信息、订阅历史、历史成本、生命周期/续费时间线、关联服务/域名、关联监控/Target 和相关决策记录。
- 订阅页默认不展示归档 VPS 的订阅；归档页提供这些订阅的历史次数、月成本和具体记录。
- 监控历史只承诺展示现有 retention 窗口内仍可读取的数据，不承诺永久保存。
- 归档页只读，不提供创建、编辑、恢复、取消、监控关联、订阅写入或 Target 操作。

## Acceptance Criteria

- [ ] `/api/vps`、`/api/subscriptions` 等普通列表默认排除 `cancelled` / `archived` VPS，显式 `asset_scope=archived|all` 可读取归档或全量数据。
- [ ] Dashboard 资产摘要、取消压力、运行对象压力和成本汇总不再把 `cancelled` / `archived` VPS 当作当前运营压力。
- [ ] Asset Decisions 的 overview/groups/records 不再包含 `cancelled` / `archived` VPS；相关测试更新为新的归档边界。
- [ ] Monitoring / Targets 普通列表隐藏“只关联归档 VPS”的对象，但保留独立对象或仍关联 current VPS 的对象。
- [ ] VPS 页提供归档入口且不再在主列表展示归档资产。
- [ ] 订阅页不展示归档 VPS 的订阅；归档页可以看到归档 VPS 的订阅历史和月成本。
- [ ] `/archive` 页面存在 happy-path 测试，且页面不暴露写操作入口。
- [ ] 后端、前端测试和 lint/build 验证通过，或明确记录无法运行的命令和原因。

## Notes

- 本任务不修改数据库 schema，不删除历史数据。
- 本任务不改变 lifecycle action 的写入语义，也不实现归档恢复流程。
