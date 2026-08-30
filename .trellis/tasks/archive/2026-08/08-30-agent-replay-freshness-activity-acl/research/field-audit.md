# v0.79.3 测试环境现场审计

## 审计边界

- 日期：2026-08-30（Asia/Shanghai）。
- 方式：只读 SSH、Docker/Compose inspect/logs 和已有 API/数据库事实核对。
- 未重启、停止、删除或重建容器；未修改远端文件、配置、数据库、ACL 或 Agent。
- 本文不保存 IP、密码、token、DSN、fingerprint、Authorization 或请求载荷。

## Compose 结论

- Center 镜像为 `v0.79.3`，`houfeng`、PostgreSQL、ClamAV、record authority 均 healthy；content processor 正常 running。
- 所有常驻服务 `RestartCount=0`、`OOMKilled=false`，inspect 未报告 runtime error。
- `houfeng-storage-init`、`houfeng-secrets-init`、`houfeng-db-init` 是一次性初始化任务，三者均：
  - `status=exited`
  - `ExitCode=0`
  - `OOMKilled=false`
  - `RestartCount=0`
  - `RestartPolicy=no`
  - `.State.Error` 为空
- Compose 配置以 `service_completed_successfully` 作为后续依赖条件；因此 `docker compose ps -a` 显示这三项 `Exited (0)` 是成功终态，不是崩溃。只有非零退出、OOM、State.Error 或依赖未满足才属于异常。

## 升级后日志结论

- 新 Center 启动时间：`2026-08-30T07:13:48Z`。
- 自该时间起，Center 与 PostgreSQL 中 `agent_sync_batches` 权限错误为 0。数据库在 `07:13:35Z` 仍有一条旧容器切换前的历史错误，不能归入新进程。
- Center 启动约一分钟后开始每分钟一次：
  - `activity projection: source pass failed`
  - `permission denied for table record_activity_projection`
  - SQLSTATE `42501`
- 审计窗口内没有第三类 Center/数据库 ERROR，也没有 WARN、FATAL 或 PANIC；ClamAV self-check 报告 database status OK，content processor 正常启动。
- 当前容器的 24 小时 PostgreSQL 日志只归一化出两类 ERROR：历史 `agent_sync_batches` 42501 与当前 `record_activity_projection` 42501。

## Agent 心跳事实

- 两台已重新接入的新版 Agent 均在持续成功写入 sync batch 和 heartbeat；Center 的 `last_sync_at` 已推进到当前接收时间。
- 正在写入的 heartbeat 来自旧 durable queue，携带 `agent_version=v0.79.1`、`is_backfilled=true` 和前一天的 `observed_at`。因此 `last_heartbeat_at` 只按历史观测时间前进，当前还没有 host sample。
- 现场抽样中，每分钟成功 batch 数与 heartbeat 数一一对应，证明这不是网络、反向代理、绑定、token 或 Center handler 完全收不到请求。
- 当时按三分钟吞吐估算，两个队列分别约需 3.7 小时和 2.9 小时追平；队列缩短后速度可能变化，该估算不是设计合同。

## 已定位的代码边界

- `agent/runtime/runtime.go` 的 `flushSyncQueue` 会同步遍历可用队列，返回前 runtime 不处理下一 tick；旧条目在 `syncRequest` 中保持原 `observed_at` 并标记 backfill。
- `agent/syncqueue/store.go` 默认上限为 65,536 项、72 小时、64 MiB，说明完整 flush 可能是长任务。
- `internal/center/store/sync_batches.go` 分别以接收时间推进 `last_sync_at`、以 heartbeat 的观测时间推进 `last_heartbeat_at`，解释了“传输当前成功但心跳仍旧”的表象。
- `internal/center/store/record_activity.go` 对 `record_activity_projection` 执行 `SELECT ... FOR UPDATE`。
- `internal/center/store/migrate/app_acl_current_contract.go` 只授予 runtime 对该表 `SELECT, INSERT, DELETE`；远端 catalog 也确认 `UPDATE=false`。PostgreSQL 的行锁读取要求目标列/表具备相应 UPDATE 权限，导致生产路径 42501。

## 新会话必须先回答的设计问题

1. Agent backlog 应采用每 tick 有界批量、公平调度、优先发送新鲜事实还是其他机制，才能兼顾实时性、FIFO/幂等和最终排空？
2. 新旧事实交错或乱序到达时，Center 哪些时间戳/聚合必须单调，现有 SQL 是否已完整防止旧 backfill 回退新状态？
3. “正在追赶 backlog”应通过现有日志/状态字段表达，还是需要最小 API/UI 合同变化？
4. Activity projection 应调整锁/分类 SQL 以适配现有 ACL，还是 current ACL 本身缺少业务必需权限？必须用并发语义和 direct-runtime PostgreSQL 证据决定，不能先做现场 GRANT。
5. 两项修复是否应同一发布交付、分别提交，或拆为父子任务？在设计审查前不做实现决定。

## 前序任务边界

- 前序 `agent-heartbeat-onboarding-failure` 已随 v0.79.3 发布，修复了 `agent_sync_batches` 的显式 conflict target 与 INSERT-only ACL 冲突。
- 本任务不得恢复显式 conflict target、扩大该表 SELECT 权限或把新问题归因于前序修复失败。
