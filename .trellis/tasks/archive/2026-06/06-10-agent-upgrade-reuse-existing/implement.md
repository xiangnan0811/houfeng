# 复用已接入监控实例升级 agent - 实施计划

## Checklist

1. 后端 TDD：为 create/link 已有 active link 返回 409 写 handler 失败测试。
2. 后端 TDD：为 store create/link 在事务内检查 active link 且 create 不插入孤立实例写失败测试。
3. 实现 `assetlinks.ErrVPSActiveMonitoringInstanceExists`、handler 映射和 store 事务保护。
4. 前端 TDD：为 VPS 详情 0/1/多 active link 行为、`workbench=monitoring` 深链分流写失败测试。
5. 前端 TDD：为 Monitoring detail onboarding 文案按状态区分写失败测试。
6. 实现 VPS 详情分流、重复提示、行级升级入口和 onboarding 文案。
7. 验证：`go test ./internal/center/... ./cmd/...` 与 `npm test -- --run`（在 `web/`）。

## Risk Controls

- 不添加数据库唯一索引，避免历史重复数据导致 migration 失败。
- 不自动解除历史重复 active links。
- 不修改 MonitoringInstance lifecycle、monitoring、health、Agent plan 或 sync 行为。
