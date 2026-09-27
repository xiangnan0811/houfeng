# 数据保留合同

## 范围与接口

`internal/center/settings`、`internal/center/retention`、`store/retention.go` 和 Settings 数据保留面板共同实现此合同。
设置 `retention_policy` 仅包含 `raw_layer_days` 与 `aggregate_layer_days`；不接受事件、通知或 IP 质量的按天删除配置。

| 数据 | 默认保留 | 规则 |
| --- | --- | --- |
| 原始心跳、主机性能、探测观测 | 30 天 | `raw_layer_days` 为 30–365，按 `observed_at`，严格早于截止时间才删除 |
| 主机与探测 UTC 日聚合 | 365 天 | `aggregate_layer_days` 为正整数，按 UTC `bucket_date`，不删除截止日桶 |
| 事件、通知记录、生命周期审计、业务历史、低频 IP 质量报告及脱敏原始结果 | 长期 | 不参与定时按天清理 |
| Agent 会话与实时信号去重身份 | 长期 | 原始心跳清理不得删除最后可信在线时间、曾接入事实或重放去重依据 |
| 远程命令 stdout/stderr | 24 小时 | 保留现有 `output_expires_at` 安全清理及读取时隐藏；永久命令元数据审计不含输出 |

归档、退役、恢复和维护都不改变数据的发生时间，也不重置保留起点。归档后的原始数据按相同策略清理；业务历史与 IP 质量继续可查。

## 聚合与事务

每次启动及每小时，worker 重新读取并校验设置。一个 Repeatable Read 事务先计算当天之前的 UTC 日桶，再删除过期原始数据、过期聚合与到期命令输出。任一步失败回滚全部变更。

原始截止时间触及的 UTC 日桶及更早桶在同一聚合语句中标记 `finalized=true`。后续 upsert 仅更新未 finalized 的桶；这样部分原始数据被删除后，剩余数据或晚到回填不会覆盖原有完整聚合。当前 UTC 日不提前聚合；仍保留完整原始数据的历史桶可以接纳回填并重新计算。超过原始保留窗口的晚到数据不能重新改写已封存桶。

设置缩短保留时间时也必须先聚合并封存涉及桶，再执行删除。不能先清理后补算，不能把较小样本集覆盖到已封存桶。扩大保留期限不会恢复已删除的原始数据或聚合。

## 验证

- `internal/center/settings`：默认 30/365、30 与 365 边界、29 与 366 拒绝、已移除低频 TTL 不再序列化。
- `internal/center/retention`：启动执行、每轮加载最新设置、取消与失败后的继续处理。
- `store/retention_test.go`：事务隔离、截止时间和永久历史不被清理。
- `TestPostgresIntegrationRetentionPreservesHistoryAndFinalizedBuckets`：实际数据库及独立运行角色下验证精确时间边界、UTC 日桶、先聚合后删除、重复运行和晚到数据不覆盖封存桶、归档不重置时钟、低频长期保留、24 小时输出到期与故障原子回滚。

历史手动删除入口不属于本次范围；不得把新的保留设置解释成手动清理授权。
