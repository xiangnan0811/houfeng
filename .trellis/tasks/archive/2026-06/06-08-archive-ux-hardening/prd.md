# 归档功能体验与规则重构

## Goal

把 VPS 归档从普通详情页的直接状态修改，重构为有资格检查、强确认、成功跳转和只读归档详情的受控生命周期流程；同时把归档查看体验从混合页面改为列表页 + 详情页，保留历史数据用于服务商质量、成本和使用体验回看。

## Requirements

- `/archive` 是归档 VPS 列表页，只展示已 `cancelled` / `archived` VPS 的清单和摘要，不在列表页直接展开单台详情。
- `/archive/:vpsId` 是只读归档详情页，支持 `cancelled` / `archived` VPS；详情页不提供创建、编辑、关联、取消或普通 lifecycle 工作台入口。
- `/archive/:vpsId` 不承接当前/活跃资产；如果 review 返回的 VPS 不是 `cancelled` / `archived`，前端必须跳回 `/vps/:vpsId`，避免把可操作资产展示成归档只读资产。
- 归档详情布局采用 v2：
  - 顶部展示只读身份头和归档说明 / 恢复影响说明。
  - 上方用三栏或四栏区域展示基础信息、访问入口、订阅历史、月成本、服务、域名、续费判断和资产历史摘要。
  - 中间重点展示用户记录 / `experience_logs`。
  - 其后展示续费、价格、规格和 IP 历史摘要。
  - 订阅、服务、域名明细属于次级事实明细，不能排在用户记录之前。
  - 最底部全宽展示监控关联历史与 Target 历史。
- VPS 普通详情页对 `cancelled` / `archived` 资产不再作为可操作详情展示；直接访问 `/vps/:id` 时跳转 `/archive/:id`。
- 从 VPS 普通详情页发起归档时，必须先加载归档资格 review；归档成功后 `replace` 跳转 `/archive/:vpsId`。
- 归档必须有服务端前置条件：
  - 任一关联订阅仍为 `active` 时阻止归档。
  - 任一关联 MonitoringInstance 不是 `不续费` / `已退役`，或监控状态不是 `暂停` 时阻止归档。
  - 任一关联 Target 不是 `暂停` / `已归档` 时阻止归档。
- 归档需要强二次确认：用户必须输入 VPS 展示名，服务端校验确认名称。
- 归档必须使用专用受控 API，不能再通过普通 `PATCH /api/vps/:id` 写入 `lifecycle_status=archived`。
- 恢复只允许 `lifecycle_status=archived` 的 VPS，恢复为 `idle`；`cancelled` 保持只读，不提供恢复。
- 恢复不删除或重建 VPS、订阅、监控实例关联、服务、域名、Target 引用或资产历史。
- 归档详情读取订阅历史时必须使用归档 / 全量 scope，不能把归档资产误判成缺订阅。
- Archive detail 的 Target 历史不能依赖普通 `/api/targets` 列表，因为普通列表会隐藏只关联归档 VPS 的对象。

## Acceptance Criteria

- [ ] `GET /api/vps/:id/archive-review` 返回 VPS、订阅影响、监控关联、服务、域名、Target 影响、blockers 和 eligibility。
- [ ] `POST /api/vps/:id/archive` 校验 confirmation name 和 blockers；成功写入 `archived` 并返回受控生命周期结果或归档 review；失败返回 400/409/404。
- [ ] `POST /api/vps/:id/restore-from-archive` 只恢复 `archived` VPS 为 `idle`，不允许恢复 `cancelled` 或当前资产。
- [ ] `PATCH /api/vps/:id` 禁止直接写入 `archived`，并禁止从 `archived` 通过普通 PATCH 恢复。
- [ ] 后端 router、bootstrap 和 router tests 覆盖新的 VPS archive 子路径。
- [ ] VPS 详情归档按钮展示 review/blockers 和输入名称确认；成功后跳转 `/archive/:vpsId`，且不会出现归档后“缺订阅”误判。
- [ ] `/archive` 列表页不直接加载详情上下文；点击行/操作进入 `/archive/:vpsId`。
- [ ] `/archive/:vpsId` 详情页按 v2 区域划分展示只读历史，并包含用户记录、订阅历史、服务、域名、监控和 Target 历史。
- [ ] `/archive/:vpsId` 对非 `cancelled` / `archived` VPS 跳回普通 VPS 详情，不展示归档只读详情。
- [ ] `/archive/:vpsId` 中用户记录排在订阅、服务、域名明细之前；监控和 Target 历史保持页面底部全宽展示。
- [ ] `/archive/:vpsId` 对 `archived` 展示受控恢复入口；对 `cancelled` 不展示恢复入口。
- [ ] 相关 Go 单测、前端单测和项目验证命令通过，或明确记录无法运行的命令与原因。

## Notes

- 本任务不迁移数据库 schema，不删除历史数据。
- 本任务不改变已取消 `cancelled` 的业务含义；`cancelled` 是归档可读资产，但不是可恢复资产。
- 归档资格检查优先复用现有 asset lifecycle 聚合数据源，避免前端多接口自行拼 blockers。
