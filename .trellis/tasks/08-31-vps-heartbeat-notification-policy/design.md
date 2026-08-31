# VPS 心跳异常通知策略设计

## 1. 结论

采用已确认的平衡策略：默认首次失联阈值 `N = 12`，严重度边界为 `N / 2N / 4N`，恢复要求事件开始后的 3 个连续非回填实时同步批次。所有心跳评估入口使用同一个显式策略对象；任何调用方都不能省略阈值并静默退回 `3`。

通知正文只在投递边界补充主体信息，领域 incident/event 的简洁摘要保持不变。消息与 `notification_records.summary` 共用同一份丰富正文：

```text
VPS/监控实例：香港-01（mi_01H...）
事件：心跳失联
详情：最近 20 个心跳周期未收到心跳
```

## 2. 已确认根因

当前数据流有两条心跳判定路径：

```text
周期 worker
  -> EvaluateStaleMonitoringInstances
  -> incidentTimingFor
  -> evaluator(heartbeat interval, persisted stale threshold)

成功同步后的异步收敛
  -> AfterSuccessfulSync
  -> evaluateMonitoringInstance
  -> evaluator(heartbeat interval)
  -> variadic 参数缺失
  -> evaluator 静默使用 3
```

同时，`heartbeatSeverity` 把设置值解释成告警边界并在 `N - 1` 先创建关注；一次新心跳就恢复；通知 dispatcher 只收到不带对象身份的 `decision.Summary`。完整代码证据见 `research/root-cause.md`。

## 3. 领域策略合同

新增显式心跳策略值对象，名称可在实施时按包内命名统一，但字段语义固定：

```go
type HeartbeatIncidentPolicy struct {
    HeartbeatInterval       time.Duration
    MissingThreshold        int
    RecoverySuccesses       int
    RecoveryMaxIntervalGap  time.Duration
}
```

默认构造结果：

- `HeartbeatInterval = 5s`；
- `MissingThreshold = 12`；
- `RecoverySuccesses = 3`；
- `RecoveryMaxIntervalGap = 2 * HeartbeatInterval`。

evaluator 接口必须显式接收该策略及恢复证据，不再使用 variadic/default threshold。无效策略或证据读取错误在内部 service 评估边界 fail closed；已有 incident 被保留，不创建新的恢复或通知副作用。设置校验同时限制 `seconds * time.Second`、`2 * heartbeatInterval` 和 `4 * N` 的安全上界，领域 policy 也拒绝越界的直接构造；missed-interval 计算采用饱和转换，避免平台 `int` 宽度造成回绕。

### 3.1 失联与升级

令 `missed = floor((now - lastHeartbeatAt) / heartbeatInterval)`：

| 条件 | 当前等级 | 转换 |
| --- | --- | --- |
| `missed < N` 且无 active incident | 正常 | noop |
| `N <= missed < 2N` | 关注 | start 或保持 |
| `2N <= missed < 4N` | 告警 | start/upgrade 或保持 |
| `missed >= 4N` | 严重 | start/upgrade 或保持 |

扫描从正常直接跳到 `>= 4N` 时，只创建一次“严重” start，不补发关注/告警消息。配置 `N = 20` 的精确边界为 `20 / 40 / 80`。

### 3.2 稳定恢复

仅当已有 `monitoring_instance_heartbeat_missing` active incident 且当前已低于失联边界时，才判断恢复证据：

1. 只读取 `received_at > incident.started_at` 的心跳；
2. 只接受 `is_backfilled = false`；
3. 按 `sync_batch_id` 去重，至少 3 个不同批次；
4. 使用服务端 `received_at` 排序，不信任 Agent 时钟；
5. 相邻两个批次的接收间隔均不得超过 `2 * heartbeatInterval`；
6. 第 1、2 个合格批次保持原 incident，第 3 个才生成一次 recovered event/notification；
7. 数据库读取失败、回填、重复 batch、事件前数据或稀疏数据都保持 active。

因此，单个偶发心跳、修改阈值或重放历史数据都不会制造“心跳已恢复”。

## 4. 策略加载与两条入口统一

service 增加一个权威的 policy resolver：

- settings 行不存在或测试未注入 repository：使用代码默认 `12`；
- settings 行存在：使用已验证的持久化 `heartbeat_interval_seconds` 和 `stale_threshold_intervals`；
- settings 读取/解码/校验失败：内部评估返回错误并停止本次心跳评估，不回退到 `3`；periodic 调用方收到该错误；
- 周期扫描和 `AfterSuccessfulSync` 都先解析同一策略，再调用相同 evaluator；
- `AfterSuccessfulSync` 运行时原始事实已经提交；公开 post-sync hook 对内部评估错误记录稳定日志并返回 `nil`，由周期扫描继续收敛。不能把错误返回给 Agent，因为重试会命中 `exact_duplicate` 并跳过 post-sync，造成永久漏评估；
- 非空 heartbeat carrier 若全部 `is_backfilled=true`，本次 full attempt 只抑制 heartbeat start/escalate/recover，并保留已有 heartbeat incident；同批 host/target 等其他维度仍按各自 provenance 规则评估。空 carrier 为兼容现有调用仍正常评估，mixed/live carrier 也正常评估；
- CAS 冲突后的完整重试重新读取对象、active incidents、心跳证据和 settings，不能重放旧策略结果；post-sync 触发 provenance 在重试中保持不变。

同一次成功 mutation 对应的通知开关使用该次 settings snapshot，避免判定与投递之间第二次读取产生阈值/开关竞态。sweep 调度自身在设置暂时不可读时可沿用安全的调度 fallback，但不得据此创建心跳事件。

## 5. 恢复证据读取

在 `SnapshotReader` 增加有界的最近实时心跳读取能力，返回最少字段：`SyncBatchID`、`ReceivedAt`。Agent sync HTTP 入口固定接受每批 `1..syncing.MaxBatchItems` 条 heartbeat，当前共享上限为 256，且同一请求内所有 heartbeat 必须共享同一 `sync_batch_id`；handler 校验与恢复查询必须引用同一常量，禁止复制魔法数字。

PostgreSQL 查询先在任何 window/dedup 之前建立 `recent_live AS MATERIALIZED` 候选集：限定实例、`received_at > started_at`、`is_backfilled=false`，按 `(received_at DESC, id DESC)` 排序并 `LIMIT $3`，其中 `$3 = 3 * syncing.MaxBatchItems = 768`。随后才按 `sync_batch_id` 做 WindowAgg 去重并最终 `LIMIT 3`。每个已接纳批次最多 256 条 heartbeat，因此最近三个批次最多占 768 行；`id` 只作为相同服务端时间戳的稳定 tie-break，不依赖时间唯一。若 legacy/direct writer 绕过这一 ingress 不变量，候选截断至多导致恢复证据不足、继续保持 active，不会凭空恢复，因此仍是 fail closed。

新增 partial index：

```sql
create index if not exists idx_monitoring_instance_heartbeats_live_received
  on monitoring_instance_heartbeats (monitoring_instance_id, received_at desc, id desc)
  include (sync_batch_id)
  where is_backfilled = false;
```

该索引只服务恢复证据，不替换 current/latest 事实按 `observed_at DESC, is_backfilled ASC, received_at DESC, id DESC` 的既有排序合同。

## 6. 默认值与数据迁移

追加 `0063_tune_heartbeat_incident_policy.sql`，不修改 `0006`、`0026` 等历史 migration。迁移完成三件事：

1. 将 `center_settings.incident_defaults` 的列默认 JSON 中 `stale_threshold_intervals` 改为 `12`；
2. 对现有 singleton 行，仅当全局 `incident_defaults->>'stale_threshold_intervals' = '3'` 时用 JSONB merge 改为 `12` 并推进 `updated_at`；
3. 创建恢复查询 partial index。

以下数据保持不变：

- 全局 `20` 或任何其他非 `3` 值；
- `override_rules` 的完整 JSON，包括显式覆盖值 `3`；
- 通知开关、心跳间隔、扫描间隔和其他 incident thresholds。

代码侧 `centersettings.Default()` 同步改为 `12`。post-`0051` exact-current 合同要求为 `0063` 注册 explicit empty `AppACLCurrentMigrationFragment`，因为它没有新增 APP-managed table/view/sequence/function 或 privilege；同时更新 migration inventory/count 和 current-source tests。

## 7. 通知主体

heartbeat evaluator 继续只生成领域摘要，例如“最近 20 个心跳周期未收到心跳”和“心跳已恢复”。service 在 mutation 已成功提交、准备写通知记录/派发时，根据本次 fresh `monitoringinstances.Record` 构造消息：

- subject：在 delivery boundary 对 `DisplayName` 做安全净化：CR/LF 与控制字符替换为空白、移除 bidi controls、折叠 Unicode 空白并 trim，再按 Unicode rune 安全限制为最多 80 个字符（超限使用省略号）；净化后为空则回退“未命名监控实例”；
- stable identity：本次 object ID / `MonitoringInstanceID`；
- event label：started=`心跳失联`，escalated=`心跳失联升级`，recovered=`心跳恢复`；
- details：保留 evaluator summary。

只对 `IncidentMonitoringInstanceHeartbeatMissing` 的通知做该格式化。目标探针和资源类通知不在本任务中改写。dispatcher 收到的字符串与每个 channel 的 notification record summary 必须完全一致。

## 8. Web 设置语义

不增加字段或 API shape。`IncidentDefaultsSection` 在现有输入附近说明：

- 该值是“首次失联”边界，不是告警等级边界；
- 默认 12；按 5 秒心跳约 60 秒；
- 告警/严重分别为 2N/4N；
- 恢复固定要求 3 次连续实时心跳，回填不计。

默认 fixture、Settings page 测试、browser profile 与 `scripts/visual_evidence.py` active `/api/settings` mock 中代表默认配置的 `3` 更新为 `12`；明确用于自定义/迁移反例的 `3` 保留并写明意图。

## 9. TDD 与验证矩阵

### Evaluator

- `N-1 / N / 2N-1 / 2N / 4N-1 / 4N` 边界；
- `N=20` 精确 `20/40/80`；
- 扫描跨级只发一次实际等级；
- 1/2/3 个恢复批次；duplicate/backfill/pre-incident/gap 负例；
- 无效策略保留 previous，不产生恢复。
- recovery successes 不是 3 或 max gap 不是 `2 * interval` 的直接构造策略同样无效并 fail closed。

### Service

- 直接调用 `AfterSuccessfulSync` 与 periodic，证明两者使用相同 persisted `20`；
- settings/receipt error 在内部边界返回，公开 `AfterSuccessfulSync` 记录日志、返回 `nil` 且不产生心跳 mutation/notification；
- 非空全回填 heartbeat carrier 不 start/escalate/recover 或移除 active heartbeat incident，mixed/live 正常，同批其他维度不受该 heartbeat 抑制影响；
- CAS retry 重新读取 policy/evidence，且保留本次 post-sync provenance；
- 提高阈值不构造恢复；
- 开始/升级/恢复正文含名称和 ID；名称的换行/控制/bidi 被净化，超长多字节名称安全截断，净化后空名回退；
- 开关、多通道、行政恢复静默和 post-commit 顺序不回归。

### PostgreSQL / migration

- 实际执行 `0063`：全局 `3 -> 12`、全局 `20` 不变、override `3` 不变；
- fresh schema default 为 `12`；
- live receipt 查询在 WindowAgg 前只读取至多 768 个候选，再返回事件后的非回填 distinct batches；相同时间戳仍由 `id` 稳定排序；
- partial covering index 存在，且 `INCLUDE(sync_batch_id)`；
- 真实长重复历史先 `ANALYZE`，再对捕获的生产 SQL 执行 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`：递归拒绝 heartbeat relation 的 Seq/Bitmap path，证明使用 exact 0063 Index/Index Only Scan，scan rows×loops/filter removals/shared blocks 固定有界；第二量级旧历史证明读取不线性增长，同时 WindowAgg/Sort 及其输入实际行数不超过候选上界；
- current APP ACL fragment 显式存在且 object/privilege 为空；strict runner 必须实际 RUN/PASS。

### Agent sync ingress

- 超过 `syncing.MaxBatchItems` 的 heartbeat carrier 在 service 前拒绝；
- 同一请求内 heartbeat 使用不同 `sync_batch_id` 时在 service 前拒绝；
- 正常同批请求继续通过，handler 与恢复查询不复制 256。

### Web

- Settings 页加载默认 `12`、保存自定义 `20` 原样发送；
- 新解释文案可见；
- Node 22 focused Vitest、lint/build/budgets 与 `make verify-web` 通过。

## 10. 发布与回滚

- 发布顺序仍走 feature branch、PR、required CI、merge、main CI、Release Please 和镜像发布。
- migration 是前向兼容的：代码回滚不会撤销已经写入的 `12` 或 partial index，也不应 down-migrate。
- 若运营上需要更安静或更敏感，可在设置页显式保存其他正整数；`20` 将严格按 `20/40/80` 生效。
- 生产验收必须从通知正文、数据库 settings readback、active incident/event/notification records 和实际 Agent 心跳共同取证；仅看页面已保存或进程存活不足以证明修复。

## 11. 风险与控制

- **旧全局 3 的歧义：** 无法判断它是旧默认还是用户显式选择；按产品决策统一迁移为 12，并在发布说明中披露。
- **恢复误判：** distinct batch + non-backfilled + incident-start bound + server receive time + max gap 五层约束，查询错误 fail closed。
- **查询退化：** `recent_live MATERIALIZED` 在 WindowAgg 前以 ingress 共享上限裁成 `3 * 256 = 768`，再去重/`LIMIT 3`，配套 covering partial index；HTTP 同时强制一次请求内 heartbeat 共用 batch ID，保证每个新批次最多占 256 行。绕过 ingress 上限/分组约束只会延迟恢复，保持 fail closed。
- **两条入口再次漂移：** 删除 optional threshold，编译期强制所有调用点传显式 policy，并用两条 service path 回归冻结。
- **通知身份串线：** 使用同次 fresh record 构造 subject，CAS retry 后重建；不使用缓存或额外跨对象查询。
