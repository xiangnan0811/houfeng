# 升级 staging 并验收 VPS 心跳通知策略

## Goal

将 https://staging.yading.de 从当前 v0.59.0 升级到 v0.79.5 或更高兼容版本，并在可控测试实例上完成持久化阈值 readback、N=20 的 19/20 边界、三次非回填实时心跳恢复以及通知主体的真实环境验收；部署授权、凭据和回滚必须另行确认。

## Requirements

- R1. 在任何外部变更前确认 staging 的部署 owner、访问授权、当前 Compose/镜像来源、数据库备份与可执行回滚方案；本任务不得触碰生产环境。
- R2. 将 staging 升级到 `v0.79.5` 或包含同一修复的更高兼容版本，固定公开镜像 tag/digest，并证明 `/api/healthz.version`、容器 revision 与数据库 migration `0063` 属于同一发布。
- R3. 升级后从 Settings API/UI read back 全局 `stale_threshold_intervals`；旧全局默认 `3` 应迁移为 `12`，现存非 `3` 自定义值与 override 中显式值不得被覆盖。
- R4. 从受保护 `main` dispatch `frontend-staging-smoke.yml`，`expected_version` 必须精确匹配部署版本；真实登录、路由、可回滚 Settings mutation 与脱敏 artifact 必须通过。
- R5. 使用独立、可清理的测试监控实例验证 `N=20`：遗漏 19 个周期时无事件/通知，达到 20 时创建关注；如继续验证升级，应分别在 40/80 边界产生告警/严重且不补发虚构中间等级。
- R6. 验证 active 事件在前两个合格实时心跳后保持 active，第 3 个不同 `sync_batch_id` 的非回填实时心跳才恢复；duplicate、backfill 与过大间隔不得恢复。
- R7. 开始、升级、恢复的 notification record 和实际测试通道消息必须包含净化后的监控实例名与稳定 ID；使用隔离通道/收件人，禁止向真实用户制造测试噪声。
- R8. 记录 health/settings、migration、事件、通知记录和外发消息的联合证据；完成后恢复全部临时 Settings、测试实例、通知目标和数据，任何失败都停止后续变更并按批准的回滚方案处理。

## Acceptance Criteria

- [ ] AC1. 有明确部署授权、备份/回滚 receipt 和 staging-only 目标清单，日志及任务资料不包含凭据或 token。
- [ ] AC2. staging health、镜像 OCI label/revision 和数据库 migration readback 一致指向 `v0.79.5` 或已复核的更高兼容发布。
- [ ] AC3. 全局旧默认迁移/自定义值保留与 Settings readback 符合 migration `0063` 合同。
- [ ] AC4. main-only authenticated staging smoke 对精确部署版本通过，并保存可公开复核的脱敏 artifact URL/digest。
- [ ] AC5. 可控测试实例对 `N=20` 的 19/20 边界、三次稳定恢复及全部负例均有真实事件/数据库证据。
- [ ] AC6. 测试通知正文含安全名称与稳定 ID，且没有向非测试用户或生产通道投递。
- [ ] AC7. staging 设置和测试数据恢复完成，部署/数据库/服务健康检查通过，并记录残余风险与未覆盖范围。

## Current evidence and boundary

- Release [v0.79.5](https://github.com/xiangnan0811/houfeng/releases/tag/v0.79.5) 已发布并通过 `publish-images`；本任务不重新实现已发布策略。
- `frontend staging smoke` run `33358504336` 从受保护 `main@e427f41b` 以 `expected_version=v0.79.5` 执行，在登录或任何 Settings mutation 前 fail closed：health 实际返回 `v0.59.0`。脱敏 artifact `frontend-staging-audit-33358504336` digest 为 `sha256:f5f7fcdbbdeb6a506f2f8da5691ad4f52ae36e2f5ededad177cf85a9f40a5118`。
- 仓库只有 read-only staging audit workflow，没有部署 workflow 或主机授权；不得把发布成功、镜像可拉取或本地测试通过冒充真实 staging 验收。
- 本任务保持 planning/not-started，部署权限和回滚方案确认后再用 Trellis 正式启动。
