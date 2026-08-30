# Agent Replay Freshness and Activity ACL Design (Draft)

> 状态：规划设计已于 2026-08-30 获用户接受，其中确认 `K=2`、backlog lane FIFO 与 fresh 有界越过；用户随后授权按项目规范完成独立审查、提交、PR/CI、合并、发布、测试环境部署、验收与最终清理。任务保持 `in_progress`，直至发布后现场条件和清理完成。

## 1. 结论

本任务保留为一个 P1 父任务和一个协调发布，但按三条可独立审查的实现切片推进：

1. Agent durable queue 的有界公平调度与本地聚合日志；
2. Center 新旧事实交错时的 ingest 单调性、latest 选择和 incident projection CAS；
3. Activity Projection 的锁语义修复与 direct-runtime PostgreSQL 16 回归。

推荐并默认采用的运维合同是 **Agent journal 聚合状态 + Center 现有 freshness 字段**，不新增同步协议、API 或 UI；它已经满足 PRD 允许的最小可观测性。若以后要求必须在 Center 页面直接看到“正在追赶”，再另开 UI 子切片，不阻断本次 correctness 修复。

## 2. 现场事实与非缺陷基线

- v0.79.3 新 Center 启动后，`agent_sync_batches` 的 SQLSTATE `42501` 为 0；本任务不恢复显式 conflict target，也不扩大该表的 SELECT 权限。
- 两台新版 Agent 正在成功回放 v0.79.1 durable queue。当前 `last_sync_at` 证明传输仍在发生，但旧 `observed_at` 使 `last_heartbeat_at` 陈旧；同步 flush 独占 runtime tick，所以没有新 host sample。
- Activity Projection 每分钟失败的直接原因是对 `record_activity_projection` 做 `SELECT ... FOR UPDATE`，而事实表的 current ACL 有意保持 `SELECT, INSERT, DELETE`、`UPDATE=false`。
- 三个 `Exited (0)` 服务是成功的一次性 init job；常驻容器 healthy、无 OOM、无异常重启。除非出现新证据，本任务不修改 Compose 生命周期。

## 3. 五个设计问题的回答

### 3.1 Agent backlog 如何兼顾实时性、FIFO 和最终排空？

采用固定 `K=2` 的 **两车道微轮转**：

1. 每个 runtime tick 先采集 fresh heartbeat/host/probe/辅助载荷，并先持久化到 durable queue；
2. 记录 `Enqueue` 返回的本地 entry ID，作为本轮 fresh carrier 的唯一身份；不能用 `sync_batch_id` 判断，因为 file store 为本地 ID 冲突加后缀时不会改写请求内 batch ID；
3. prune 并剔除 authority 已变化的 stale entries；
4. 从本轮 fresh entry 之前已存在的 backlog 中，按现有 `CreatedAt, ID` 顺序最多尝试两个；
5. 无论 backlog 是否已经排空，都把本轮 fresh entry 作为本轮最后一次网络尝试；
6. 每个成功响应只有在对应 entry 完成 durable delete 后才算 ack；backlog 响应可立即执行其中的 pending action，但 fresh 响应最后应用，保证下一 tick 使用最新完整 plan，而不是被 exact-duplicate backlog 的空 plan 清除。

公平性合同：

- backlog lane 内保持 FIFO；只允许 fresh lane 越过尚未轮到的旧 backlog，不宣称全局 FIFO。
- 一轮最多进行 `K+1=3` 次网络同步。以当前 10 秒 client timeout 计，在本地 queue I/O 正常且 context 未提前取消时，fresh 网络尝试应在至多约 20 秒后开始，并在约 30 秒内完成；测试合同以“至多两个旧尝试后必尝试 fresh”为准，不把文件系统停顿伪装成绝对墙钟承诺。
- backlog 连续成功时，轮后旧 backlog 满足 `B(n+1)=max(B(n)-2,0)`，因此在持续连通条件下最终排空。
- 429、5xx、transport timeout 等 retryable backlog head：保留并 `MarkAttempt`，停止本轮 backlog lane，但仍尝试 fresh；fresh 自己失败时保留并结束本轮。
- 可识别的 invalid JSON/request 400：先 durable delete 再记为 discard，本次尝试计入 K，可继续下一旧 entry。
- 其他 terminal 4xx、authority/local queue 错误：保持现有 fail-closed 行为并结束本轮；这类错误同样会使 fresh 失败或令 queue 状态不可信，不做绕过。
- 成功上送但 durable delete 失败不算 progress；entry 留存并依赖 Center exact-duplicate 幂等重试。

不选严格 FIFO，因为 65,536 条上限下会再次饿死实时 tick；不选无限 current-first，因为它需要另造“忽略 replay plan”语义且降低排空吞吐；不选并发 sender，因为共享 plan、pending results、IP quality payload acknowledgement 和 queue rewrite 会引入新的竞态面。

### 3.2 Center 哪些状态必须单调，现有 SQL 是否完整？

现有 ingest 保护是必要但不完整的：

- `agent_sync_batches` marker 阻止 exact duplicate 再次追加 raw facts；
- `monitoring_instances.last_heartbeat_at` 和 `last_sync_at` 使用 `GREATEST`；
- lifecycle 只允许 pending 在接受 host sample 后提升，其他 paused/retired/observing 状态不由旧 batch 回退；
- command result 使用 action/command identity CAS；
- latest host/probe 查询以 `observed_at` 为第一顺序。

仍需修复两个缺口：

1. **相同 `observed_at` 的 latest 选择依赖到达顺序。** host、probe、IP-quality 与 heartbeat agent-version 的 SQL 统一使用 `(observed_at DESC, is_backfilled ASC, received_at DESC, stable_row_key DESC)`；`stable_row_key` 是各表真实主键，host/probe/heartbeat 使用 `id`，IP-quality 使用 `report_id`。对应 Go 归一化用前三项做 stable sort，并在完全同值时保留 SQL 已按该稳定键降序给出的输入顺序。同一时刻优先 live，之后才按接收时间和稳定行键破同值。
2. **incident/current-health 投影存在 stale-writer race。** 两个 sync/sweep 可先后读出旧/新快照，再以相反顺序执行“删除全部 active incidents、重建集合、写事件、更新摘要”。现有 writer 没有 revision，旧评估可以晚写并回退 `current_health_status`、incident 集合和 `last_evaluated_at`，还可能制造重复 event/notification。

使用 monitoring instance / target 对象行的 PostgreSQL row version `xmin` 作为一次评估内的 **短生命周期 CAS token**，不新增 schema。实现合同：

- 每次评估在读取 active incidents、对象状态和 raw facts 之前，读取对象 `xmin::text`；该 token 只在进程内随本次 mutation 传递，不进入日志、API 或持久任务证据；
- `IncidentMutation` 携带 `ExpectedObjectRowVersion`；
- writer 事务先 `SELECT xmin::text ... FOR UPDATE` 锁定对应对象行并比较 token，不匹配则返回 typed conflict，且在任何 delete/insert/event/summary 前退出；
- 匹配时，在同一事务中替换 active set、追加 state events 并更新 object summary；对象 UPDATE 自然产生新的 row version，使其他持有旧 token 的 mutation 冲突；
- 只有 mutation 成功后才允许 dispatch/append notification；conflict 不产生外部通知或 notification record；
- incident processor 遇到 conflict 最多重新读取并完整评估一次。第二次仍冲突时只记录脱敏、限频告警并放弃该次 projection，依赖后续 sweep 收敛，绝不提交旧 mutation；它必须向已提交的 post-sync 边界返回成功，不能把已经接受且可能携带 pending action/plan 的 sync 反转为失败。

不新增 migration，也不改 current APP ACL fragment。原因不是省事：当前 exact manifest 会把任何 successor migration 识别为 rebuild-required；生产修复不能隐式要求重建数据库。也不把现有 `updated_at` 直接当 CAS：事务时间戳可能与旧值相同，且要让它成为可靠版本还需审计并改造所有 writer 以保证严格递增。`xmin` 直接标识 PostgreSQL 行版本，能在一个短评估窗口内检测任何并发 object UPDATE；事务 ID wraparound 或 vacuum 造成的变化至多产生安全的 false conflict，不会让 stale mutation 通过。base tables 已在既有 runtime managed surface 中具备生产所需 SELECT/UPDATE。

`syncing.Result` 增加闭合的事实 disposition enum：`recorded`、`exact_duplicate`、`suppressed`，不使用会混淆零值的 bool。post-sync processor 只跳过 exact duplicate；recorded 进入正常评估，paused/retired/archived 的 suppressed 仍进入既有行政恢复路径。raw append、marker 和 GREATEST 更新仍在原事务内。

需要单调/稳定验证的面包括：

- `last_heartbeat_at`、`last_sync_at`；
- lifecycle、binding/token/fingerprint 和 command state；
- latest host sample、latest probe observation、IP-quality observation、对应 agent version；
- active incident set、`last_evaluated_at`、object health/count/summary 和 object row version；
- state-change events 与 notification records 对 exact duplicate 或 stale mutation不重复。

本任务不改变“pending + 任一合法 host sample 可提升 in-use”的既有 onboarding 语义，只补回归锁定；也不扩展到超出默认 72 小时 queue / 30 天 raw retention 的历史聚合重算，或混合 carrier batch ID 的额外协议硬化。这两项作为后续风险记录，不混入本 P1 修复。

### 3.3 如何表达“正在追赶 backlog”？

默认采用 Agent 本地、限频、聚合日志，不上报 queue depth：

- `sync queue replay progress state=catching_up acked_entries=<n> remaining_entries=<n>`
- `sync queue replay progress state=caught_up acked_entries=<n> remaining_entries=0`
- retryable failure 使用 `state=retrying` 和 allowlist 后的 `failure_kind/status_code/code/action`，不得把失败轮写成健康 progress。

日志规则：

- 进入 replay 和 caught-up 各写一次；持续追赶最多每 60 秒一次、每轮最多一条；正常无 backlog tick 保持安静。
- 仅统计 sync 成功且 durable delete 已完成的 entry；同轮稍后失败时输出 retrying，而不是成功状态。
- 不记录 token、Authorization、DSN、fingerprint、monitoring/object ID、entry ID、batch ID、原始请求/响应、remote message、本地 error cause、私有 server URL。
- Center 现有 `last_sync_at` / `last_heartbeat_at` 继续作为接收与事实新鲜度证据。两者结合 Agent journal 可以区分 catching-up、live 与真正失败。

不推荐新增 Agent queue telemetry 协议。若未来必须在 Center 页面展示，另行设计只读派生状态 `live | receiving_backfill | stale | unknown`；它只能证明 Center 最近收到 backfill，不能精确知道 Agent 本地剩余条数，因此也不能替代 Agent journal。

### 3.4 Activity Projection 应改 SQL 还是扩大 ACL？

改 SQL，保持现有 ACL。

`record_activity_projection_heads` 的 active head 已在同一事务开始时 `FOR UPDATE`，是一个 project/generation 的 publisher/rebuild 串行化根。候选 hash 分类发生在持有该 head lock 之后；`record_activity_projection` 中不存在的候选行本来也无法被行锁锁住，最终 strict insert/unique/canonical-hash 检查仍是最后防线。因此从 `loadExistingActivityHashes` 的事实查询中移除 `FOR UPDATE` 不削弱并发或去重合同。

保持：

- `record_activity_projection` runtime 权限精确为 SELECT/INSERT/DELETE，UPDATE=false、无 column ACL；
- head 和 revision interval 等真正原地更新的表继续保留各自 UPDATE；
- 不新增 migration，不修改 APP ACL fragment，不做现场 GRANT。

真实 PostgreSQL 16 direct-runtime 测试必须覆盖 first insert、exact retry、same identity/different canonical hash mismatch、并发发布、连续 head watermark；RED 在旧 SQL 上自然以 `42501` 失败，GREEN 走同一生产 store 路径成功。catalog-only、fake tx 和 SKIP 都不算证据。

### 3.5 任务、提交和发布如何拆分？

保留当前父任务，未来实施时按以下独立 review/commit slice 组织，但不在本规划阶段执行 Git 操作：

1. Agent scheduler + replay logs + agent queue spec；
2. Center disposition/latest ordering + incident row-version CAS + schema/ACL no-change guard + PostgreSQL races；
3. Activity SQL + direct-runtime ACL/concurrency regression + activity spec；
4. 汇总规范、任务证据和 release notes。

Center 单调性切片必须先于或与 Agent scheduler 同一版本部署，不能先释放会主动交错 live/backfill 的 Agent、后补 Center 防线。Activity 修复技术上独立，但同属同一次 P1 现场缺陷闭环，建议同一发布。UI 若被要求，则作为额外子切片，不反向扩张核心协议。

## 4. Agent 状态机

| 阶段 | durable queue | 网络行为 | plan/载荷行为 |
| --- | --- | --- | --- |
| enqueue | fresh 已落盘 | 无 | pending results / IP reports 仅在 enqueue 成功后从内存确认 |
| sanitize | prune + stale authority delete | 无 | 失败即停止，fresh 留在队列 |
| backlog lane | oldest-first，最多 K 次 | backlog retryable 时停止本 lane | 成功且 durable delete 后可执行 response pending action |
| fresh lane | 以本地 entry ID 精确定位 | 每轮恰好尝试一次 fresh | fresh response 在本轮最后应用，成为下一 tick current plan |
| finish | 重新计算 remaining | 无 | 产生限频 aggregate state；无 backlog 时安静 |

如果 process 在任一阶段崩溃，已经 enqueue 但未 durable delete 的 entry 会在重启后再次成为 backlog；Center marker 保证重试不会重复 raw facts，Center `exact_duplicate` disposition 保证该 duplicate 不再触发 incident side effects。

## 5. Center 事务与竞态边界

```text
sync request
  -> ApplyBatch transaction
       marker insert / exact duplicate disposition
       raw append
       GREATEST instance timestamps + protected lifecycle/command state
  -> if recorded or suppressed (exact_duplicate skips)
       capture object xmin row version before snapshot
       evaluate raw facts + active incidents
       ApplyIncidentMutation transaction
         SELECT object xmin FOR UPDATE
         compare expected row version
         replace incidents + append events + project summary (new xmin)
       dispatch/append notifications only after successful mutation
  -> conflict: re-read and re-evaluate once; second conflict logs safely and yields to sweep
       never fail an already committed sync or apply stale mutation
```

跨顺序测试同时覆盖 `live(T2) -> backfill(T1)` 与 `backfill(T1) -> live(T2)`。两种顺序的 raw fact multiset 相同；latest/current state 以事件时间和 backfill tie-break 为准；exact duplicate 不改变 raw count、object row version、events 或 notifications。

CAS 只解决 stale projection writer 顺序，不声称通知 exactly-once。外部 dispatch 与 notification record append 之间的崩溃窗口是既有残余风险；本任务只保证 conflict 在 dispatch 前被截断，并且不会因 duplicate/stale mutation 主动追加副作用。

## 6. Activity 并发不变量

在一个 `(project_id, active_generation)` 内：

1. publisher/rebuild 先锁 active projection head；
2. 持锁状态下用普通 SELECT 读取已有 identity/canonical hash；
3. 将 candidate 分类为 insert、exact duplicate 或 mismatch；
4. mismatch 整批失败并回滚；
5. inserts 与 head watermark 在同一事务提交；
6. 并发 publisher 必须先后取得 head lock，所以不会同时把同一缺失 identity 判为可插入后各自提交。

事实表保持 append-only/correction-as-new-event 语义；为了行锁读取而授予 UPDATE 反而会扩大错误的历史改写能力。

## 7. 安全与隐私

- 所有新日志字段必须使用固定词汇或计数；error code 仅允许既有 allowlist。
- 测试夹具、失败输出和任务证据不得包含真实 token、Authorization、DSN、fingerprint、原始 payload 或私有定位信息。
- strict PostgreSQL runner 通过环境接收 DSN；缺失 DSN 必须 fail closed，不能 SKIP-as-pass。
- 交付授权只覆盖私有交接上下文中已明确的测试部署目标及其约定资源；仍不授权现场手工 ACL、queue 删除、Agent 重装、数据库事实修正或对其他服务器路径的操作。部署前必须重新验证精确目标和冷恢复点，不能从仓库材料猜测私有定位信息。

## 8. 规范更新要求

未来实现必须同步更新：

- `.trellis/spec/backend/error-handling.md`：把现有“全局 oldest-first 完整 flush”合同改为 backlog-lane FIFO + K=2 + fresh-last + retry behavior；
- `.trellis/spec/backend/database-guidelines.md`：记录 incident projection row-version CAS、current exact-baseline 禁止隐式 successor migration，以及 direct-runtime race evidence；
- `.trellis/spec/backend/record-activity-projection.md`：明确 active head 是串行化根，事实分类 SELECT 不需要事实表 UPDATE/row lock；
- `.trellis/spec/backend/logging-guidelines.md`：记录 replay aggregate log 的节流与禁止字段。

## 9. 已确认的产品选择

用户已于 2026-08-30 接受 `K=2` 两车道合同对现有全局严格 FIFO 的有界放宽：**backlog lane 内仍严格 FIFO，但每两个旧 entry 后允许本轮 fresh durable entry 越过剩余 backlog**。可观测性默认采用 Agent journal 聚合状态；Center API/UI 不纳入本次范围。
