# 数据保留合同

## 范围与接口

`internal/center/settings`、`internal/center/retention`、`store/retention.go` 和 Settings 数据保留面板共同实现此合同。
设置 `retention_policy` 仅包含 `raw_layer_days` 与 `aggregate_layer_days`；不接受事件、通知或 IP 质量的按天删除配置。

| 数据 | 默认保留 | 规则 |
| --- | --- | --- |
| 原始心跳、主机性能、探测观测 | 30 天 | `raw_layer_days` 为 30–365，按 `observed_at`，严格早于截止时间才删除 |
| 主机与探测 UTC 日聚合 | 365 天 | `aggregate_layer_days` 为正整数，按 UTC `bucket_date`，不删除截止日桶 |
| 事件、通知记录、生命周期审计、业务历史、低频 IP 质量报告及脱敏原始结果 | 长期 | 不参与定时按天清理 |
| Agent 会话、重放水位与有界批次收据 | 独立协议生命周期 | 原始心跳清理不得删除最后可信在线时间、曾接入事实或重放水位；收据仅由持久退休下界推进原子清理 |
| 远程命令 stdout/stderr | 24 小时 | 保留现有 `output_expires_at` 安全清理及读取时隐藏；永久命令元数据审计不含输出 |

归档、退役、恢复和维护都不改变数据的发生时间，也不重置保留起点。归档后的原始数据按相同策略清理；业务历史与 IP 质量继续可查。

## 聚合与事务

每次启动及每小时，worker 重新读取并校验设置。原始数据与日聚合在一个 Repeatable Read 事务中处理：先计算当天之前的完整 UTC 日桶，再删除过期原始数据与过期聚合。任一步失败回滚这一事务全部变更。事务通过 `pg_try_advisory_xact_lock(hashtextextended('houfeng:retention',0))` 排除重叠 worker；未取到锁返回 `Skipped=true` 并回滚，不记录 completed，也不清理命令输出。

仅 PostgreSQL `40001`（序列化失败）与 `40P01`（死锁）会触发整笔重试，最多三次 RR 尝试；每次失败先完成 rollback，再等待 50ms、100ms，等待可被 context 取消。三次使用同一 now/policy；只报告最终已提交事务的计数，不累加失败尝试。普通同步的身份校验仅对实例和 session 行使用 `NO KEY UPDATE`，保留同步与退役/降权之间的互斥，同时允许日桶外键的 `KEY SHARE`。

raw/aggregate 提交成功后，worker 先记录 `retention pass completed`，再调用独立 Read Committed 事务 `ClearExpiredCommandOutputs(ctx,now)` 清理到期命令 stdout/stderr。RC 在等待并发写入后重新检查当前行的 status/expiry，不能用旧快照清除新命令或新输出。成功单独记录 `command output cleanup completed` 及 count；失败单独记录 `clear expired command outputs failed`，不会将已提交 raw 清理描述为回滚。读路径继续隐藏到期输出。

原始截止时间触及的 UTC 日桶及更早桶在同一聚合语句中标记 `finalized=true`（日桶起点严格早于 raw cutoff；cutoff 恰为午夜时，截止日桶尚不封存）。输入按完整主体/UTC 日排除已有 finalized 桶：host 用实例+日，probe 用 target+probe item+日，不能误排除同日其他主体或探测项。后续 upsert 也仅更新未 finalized 的桶；这样部分原始数据被删除后，剩余数据或晚到回填不会覆盖原有完整聚合。当前 UTC 日不提前聚合；仍保留完整原始数据的历史桶可以接纳回填并按完整 raw 重新计算 avg/count/p95，不增量叠加。超过原始保留窗口的晚到数据不能重新改写已封存桶。

设置缩短保留时间时也必须先聚合并封存涉及桶，再执行删除。不能先清理后补算，不能把较小样本集覆盖到已封存桶。扩大保留期限不会恢复已删除的原始数据或聚合。

CPU usage/iowait/steal 的 avg/max 只纳入整组可用行：`cpu_rates_valid is not false` 且三个值均在 `[0,100]`。三个 nullable 有效计数为 `cpu_valid_sample_count`、`cpu_valid_backfilled_sample_count`、`cpu_valid_maintenance_sample_count`；新聚合必须写非 NULL（包括零），分别计可用、可用且回填、可用且维护。总 host sample_count 及非 CPU 聚合不变；整日无可用 CPU 时六个 CPU avg/max 为 NULL，非 CPU 日聚合仍保留。

迁移仅添加 nullable marker/count 并放开六个 CPU avg/max 的 NOT NULL，不默认填值、不历史回填或重算 finalized。计数必须非负，CPU 子计数不大于 CPU 有效数，有效数不大于 host 总数；legacy NULL 分别按原 host 总数/回填数/维护数解释。历史范围内 guest 偏差不可恢复，不反推或宣称已纠正；范围外、NaN/Infinity 的 legacy CPU 不成为新聚合或趋势基线。raw 清理后不能由日聚合复活伪零。

## 验证

- `internal/center/settings`：默认 30/365、30 与 365 边界、29 与 366 拒绝、已移除低频 TTL 不再序列化。
- `internal/center/retention`：启动执行、每轮加载最新设置、取消与失败后的继续处理；Skipped 或失败不记录 completed 也不清理输出；已提交 pass 先记录 completed，再独立记录输出清理成功/失败。
- `store/retention_test.go`：事务隔离、整笔有限重试、失败计数丢弃、回滚先于取消、重叠跳过与独立 RC 清理；不以 SQL 字符串断言实现。
- `TestPostgresIntegrationRetentionPreservesHistoryAndFinalizedBuckets`：真实数据库与独立 runtime 角色验证 UTC 午夜、缩短保留、缺失旧桶、完整未封存桶重算、CPU 有效性、probe p95、按主体/探测项隔离 finalized、晚到残留不覆盖封存、永久业务事实与清理失败原子回滚。
- `TestPostgresIntegrationRetentionConcurrentSync`：RR 快照后 committed/pending 非键父更新、旧 FOR UPDATE 的 40001 对照、同步与退役/降权互斥、重叠保留跳过、真实 PostgreSQL 40001/40P01 三次尝试/取消、RC 命令替换竞态、聚合之后到达的旧 raw 不会被同轮删除。测试以事务事件及 `pg_blocking_pids` 建立 barrier，不靠 sleep 猜测交错。

历史手动删除入口不属于本次范围；不得把新的保留设置解释成手动清理授权。
