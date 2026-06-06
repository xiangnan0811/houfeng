# 修复 active incident 重复插入崩溃

## Goal

修复 center 启动后 incident worker 因 `active_incidents` 确定性主键重复插入而退出的问题。生产日志表现为：

```text
run center app error="insert active incident \"...\": ERROR: duplicate key value violates unique constraint \"active_incidents_pkey\""
```

本任务只处理 active incident 派生数据写入幂等性，不改变监控判定阈值、不新增迁移、不调整通知/事件语义。

## Requirements

- `active_incidents` 是当前健康状态的派生视图，重复评估、并发/重放写入或遗留行存在时不得因为相同 `incident_id` 崩溃 center 进程。
- `ApplyIncidentMutation` 继续保持“替换当前对象 active incident 集合，然后写事件、通知、投影对象摘要”的事务边界。
- 当待写入的 `incident_id` 已存在时，写入必须更新当前事实字段，并保留合理的首次开始时间语义。
- 不新增数据库 migration，不修改已发布 migration。
- 不改变 `state_change_events` 和 `notification_records` 的生成策略。
- 测试必须覆盖 active incident upsert SQL，避免未来退回裸 insert。
- 本地验证至少运行相关 store 测试和 `make verify-go`。

## Acceptance Criteria

- [ ] `POSTGRES active_incidents_pkey` 冲突不会导致 `ApplyIncidentMutation` 返回 duplicate key 错误。
- [ ] 已存在 active incident 能被当前 mutation 刷新 `object_type`、`object_id`、`incident_class`、`severity`、`last_evaluated_at`、`status`、`source_summary`。
- [ ] `started_at` 不会因重复写入被推进到更晚时间。
- [ ] 对象摘要投影仍在同一事务内执行。
- [ ] Store 回归测试证明 active incident insert 使用 `on conflict (incident_id) do update`。
- [ ] Trellis/backend 规范记录 incident 派生数据写入的幂等要求。

## Notes

- 该任务是 P0 热修，PRD-only 足够；实现范围刻意收窄到 store 层 SQL 与测试。
