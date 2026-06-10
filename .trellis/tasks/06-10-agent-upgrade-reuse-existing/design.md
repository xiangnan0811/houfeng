# 复用已接入监控实例升级 agent - 技术设计

## Architecture

修复跨越 VPS 详情前端、VPS↔MonitoringInstance link HTTP 合同、PostgreSQL store 写路径。MonitoringInstance 仍是观测对象，VPS link 仍是历史关联表；本任务只在普通接入路径阻止误创建/误关联第二个 active link，不把该规则固化为数据库唯一索引。

## Backend Design

- 在 `assetlinks` 增加 `ErrVPSActiveMonitoringInstanceExists`，语义为该 VPS 已有 active MonitoringInstance link，普通 create/link 入口必须先复用或解除现有关联。
- `POST /api/vps/{vps_id}/monitoring-instances` 将该错误映射为 409，并返回明确提示。
- `POST /api/vps/{vps_id}/link-monitoring-instance` 将该错误映射为 409。
- Store 层在 `CreateLinkedMonitoringInstance` 事务内先锁定 `vps_assets` 行并检查 active link。已有 active link 时直接返回 domain error，不能执行 `insert into monitoring_instances`。
- 普通 link store 写路径也在事务内锁定 VPS 并检查 active link，确保并发请求不能绕过应用层检查。

## Frontend Design

- 在 VPS 详情页集中计算 active monitoring link 数量。
- 0 个 active link：沿用创建表单和创建 API。
- 1 个 active link：按钮/深链进入 `/monitoring/{monitoring_instance_id}?onboarding=1&return_vps={vps_id}`。
- 多个 active links：隐藏创建按钮，展示人工核对提示；每行提供“升级/重新接入 agent”和现有“解除关联”。
- `workbench=monitoring` / `workbench=monitoring-instance-create` 初始深链也走相同分流，不能无条件打开创建 drawer。
- Monitoring detail onboarding drawer title、主按钮和说明按绑定/心跳状态使用“接入 agent”或“升级/重新接入 agent”。

## Compatibility

- 无新增 endpoint，无响应结构变更。
- 保留历史重复 active links，不做迁移、不自动 unlink。
- 不修改 agent enroll/sync 协议；升级仍由重新生成安装命令并在目标 VPS 执行完成。
