# Agent Replay Freshness and Activity ACL Implementation Plan (Draft)

> 状态：用户已于 2026-08-30 接受 `K=2` FIFO 语义变化，并授权按项目规范完成独立审查、提交、PR/CI、合并、发布、测试环境 Center-first/Agent-second 升级、验收与最终清理。任务保持 `in_progress`；可观测性默认采用 Agent journal 聚合日志，Trellis 的一次性提交计划确认不得省略。

**Goal:** 在不丢 durable facts、不扩大 Activity 事实表 ACL 的前提下，让 fresh Agent carrier 在每轮至多两个旧 replay 尝试后取得进展，并使 Center 的 latest/current incident 投影在 live/backfill 交错和并发写入下保持单调。

**Architecture:** Agent 使用单 goroutine、两车道 `K=2` 微轮转，保留 durable file queue 和现有错误策略；Center ingest 保留 marker/GREATEST，同时以对象行 `xmin` 实现短生命周期 incident projection CAS，并增加 exact-duplicate disposition；Activity 继续以 active head 行锁串行化 publisher，事实分类改为普通 SELECT。

**Tech stack:** Go 1.26.2、PostgreSQL 16、pgx v5、Go `testing`、Trellis strict PostgreSQL runner、现有 slog 与 file syncqueue。

## 0. 启动门禁（已于 2026-08-30 满足）

1. 已重新运行 `trellis-start` / `trellis-continue` 并读取 phase context。
2. `K=2` 下“backlog lane FIFO、fresh 有界越过”的行为变化与本地实施均已获用户接受；可观测性采用 Agent journal 聚合日志。
3. 已在独立非 main 分支工作，确认 checkout/worktree 与 `task.json.branch` 一致，并启用 `scripts/setup-git-hooks.sh`。
4. 已运行 `.trellis/scripts/task.py start ...`，任务状态为 `in_progress`。
5. 已重新读取 implement/check manifests 与所有 listed specs；若未来另行批准 Center API/UI observability，先更新 task scope/manifest，不直接混入 backend slice。
6. 实施开始前记录 baseline：`git status --short --branch`、Go toolchain、PostgreSQL strict fixture 可用性。DSN 缺失应阻断 strict lane，不得 SKIP。

## 1. Agent RED：有界 fresh 进展与跨轮 FIFO

**Files:**

- Modify: `agent/runtime/runtime_test.go`
- Reference: `agent/runtime/runtime.go`
- Reference: `agent/syncqueue/store.go`

**Step 1: 写稳定失败的 scheduler tests**

新增以下测试，使用 blocking/scripted client 而不是 sleep：

- `TestRuntimeBoundsReplayBeforeCurrentDurableRequest`：预置至少 4 条 backlog，断言旧实现会在 fresh 前尝试全部；新合同要求 fresh 前最多两个旧 entry。
- `TestRuntimeReplaysBacklogFIFOAcrossFreshInterleaving`：连续三轮，断言 backlog lane 顺序不变、fresh 可以越过剩余 backlog、最终旧队列归零。
- `TestRuntimeRetryableBacklogHeadDoesNotBlockCurrentDurableRequest`：第一个旧 entry 返回 transport/429/5xx，断言它仍在队列且本轮 fresh 仍被尝试。
- `TestRuntimeCurrentRequestRetryRemainsDurableAndBackfilled`：fresh 失败后留存，下一轮作为旧 entry 发送时事实被标记 backfilled，新一轮 fresh 仍为 live。
- `TestRuntimeCurrentResponsePlanWinsAfterBoundedReplay`：backlog exact duplicate 返回空 plan，fresh 返回完整 host/probe plan，断言下轮能采 host sample。
- `TestRuntimeBoundedReplayPreservesDurableAuxiliaryPayloads`：command results / IP quality reports 在 enqueue 后才从内存 ack，网络失败和重启不丢。
- `TestRuntimeLocalEntryIDControlsCurrentAndBackfilledOnBatchIDCollision`：请求 batch ID 相同但 file store 分配不同本地 ID 时，旧碰撞 entry 必须标记 backfilled，本轮 exact local entry 必须保持 live。

**Step 2: 运行 RED**

```bash
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run 'TestRuntime(BoundsReplayBeforeCurrentDurableRequest|ReplaysBacklogFIFOAcrossFreshInterleaving|RetryableBacklogHeadDoesNotBlockCurrentDurableRequest|CurrentRequestRetryRemainsDurableAndBackfilled|CurrentResponsePlanWinsAfterBoundedReplay|BoundedReplayPreservesDurableAuxiliaryPayloads|LocalEntryIDControlsCurrentAndBackfilledOnBatchIDCollision)' -count=1
```

Expected: 旧 `flushSyncQueue` 完整遍历 backlog，至少有 fresh 顺序/上界断言失败；不得通过降低 backlog 数量绕开 RED。

## 2. Agent GREEN：两车道 K=2 微轮转

**Files:**

- Modify: `agent/runtime/runtime.go`
- Modify: `agent/runtime/runtime_test.go`
- Modify only if interface evidence requires: `agent/syncqueue/store.go`

**Step 1: 用本地 entry ID 建立 round**

- `enqueueAndFlush` 保存 `Enqueue` 返回 ID；发送逻辑只用 `entry.ID == currentEntryID` 同时决定 lane 和 `is_backfilled`，停止以 `sync_batch_id` 推断 current/live。
- 引入固定 `maxBacklogAttemptsPerRound = 2` 和安全的 `syncRoundResult`，只包含 ack/discard/remaining 计数与枚举状态。
- `flushSyncQueue` 把 sanitized entries 分成 pre-existing backlog 和 exact current entry；current 缺失视为 local queue invariant error。

**Step 2: 实现错误与 plan 顺序**

- backlog 最多两次 attempt；成功后 durable delete 再计 ack 并 apply response；invalid 400 durable delete 后计 discard；retryable 先 MarkAttempt、结束 backlog lane但继续 current。
- terminal/local/authority 错误保持 fail closed。
- current 每轮最后尝试；成功后 durable delete，再 apply fresh response；失败 MarkAttempt 并保留。
- 不改变 400、429、5xx、transport、其他 4xx 的既有分类，也不记录 raw cause。

**Step 3: 调整既有测试合同**

- 将“单轮完整 oldest-first flush”测试改写为 backlog-lane FIFO / eventual drain。
- 保留 stale authority discard、restart retry、prune limits、payload durability、pending action identity、privacy tests。

**Step 4: 验证 focused GREEN 与调度稳定性**

```bash
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime ./agent/syncqueue -count=1
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run 'TestRuntime(Bounds|Replays|Retryable|Current|Bounded|LocalEntryID)' -count=10
GOTOOLCHAIN=go1.26.2 go test -race ./agent/runtime ./agent/syncqueue -count=1
```

Expected: focused、`-count=10` 和 race 全部 PASS。

## 3. Agent RED/GREEN：限频、聚合、无泄漏 replay 日志

**Files:**

- Modify: `agent/runtime/runtime.go`
- Modify: `agent/runtime/runtime_test.go`

**Step 1: 写日志 RED**

- `TestRuntimeLogsBoundedReplayProgressAfterDurableAck`
- `TestRuntimeLogsReplayCaughtUpOnceAndKeepsLiveTicksQuiet`
- `TestRuntimeThrottlesReplayProgressLogs`
- `TestRuntimeReplayRetryIsFailureStateNotHealthyProgress`
- `TestRuntimeReplayLogsDoNotExposeSensitiveQueueOrRequestFields`

注入可控 clock，避免测试真实等待 60 秒。隐私测试使用明显 sentinel，扫描 token、Authorization、DSN、fingerprint、IDs、payload、remote message 和 server URL 均不出现。

**Step 2: 运行 RED**

```bash
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run '^TestRuntime(LogsBoundedReplayProgressAfterDurableAck|LogsInstantSuccessfulReplayDrainAsCaughtUpOnce|LogsReplayCaughtUpOnceAndKeepsLiveTicksQuiet|ThrottlesReplayProgressLogs|ReplayRetryIsFailureStateNotHealthyProgress|ReplayLogsDoNotExposeSensitiveQueueOrRequestFields)$' -count=1
```

Expected: 尚无 catching_up/caught_up/限频状态，测试失败。

**Step 3: 最小实现并 GREEN**

- Runtime 仅保存 `replayActive`、`lastReplayProgressAt` 等内存状态；不落新状态文件、不改协议。
- 只用 fixed message、state、acked/remaining counts 和 allowlisted failure classification。
- ack 必须在 durable delete 后；失败轮只输出 retrying；live no-backlog tick 静默。

```bash
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime -run '^TestRuntime(LogsBoundedReplayProgressAfterDurableAck|LogsInstantSuccessfulReplayDrainAsCaughtUpOnce|LogsReplayCaughtUpOnceAndKeepsLiveTicksQuiet|ThrottlesReplayProgressLogs|ReplayRetryIsFailureStateNotHealthyProgress|ReplayLogsDoNotExposeSensitiveQueueOrRequestFields)$' -count=10
```

## 4. Center RED：new/old interleaving 和 exact duplicate disposition

**Files:**

- Modify: `internal/center/store/sync_batches_postgres_integration_test.go`
- Modify: `internal/center/syncing/service_test.go`
- Modify: `internal/center/incidents/service_test.go`
- Create: `internal/center/store/incidents_postgres_integration_test.go`

**Step 1: 写 direct-runtime PostgreSQL interleaving test**

构建真实 runtime role fixture 和生产 `syncing.Service` / store / incident processor，覆盖两种排列：

- `live(T2) -> backfill(T1) -> exact duplicate(T1)`；
- `backfill(T1) -> live(T2) -> exact duplicate(T2)`。

逐项断言：raw heartbeat/host/probe multiset；duplicate 不追加；`last_heartbeat_at/last_sync_at` 单调；非 pending lifecycle 不回退；pending promotion 既有语义保持；latest host/probe/IP-quality/agent version 按 observed/live tie-break；active incidents、health/count/summary、events、notifications 不因旧事实或 duplicate 回退/重复。

**Step 2: 写 deterministic stale-writer race RED**

- 两个 mutation 持有相同 expected object row version，通过 barrier 控制 B 先提交、A 后尝试；分别覆盖 monitoring instance 和 target。
- 断言 A 返回 typed conflict，A 不 delete/insert event、不更新 summary、不追加/发送 notification。
- 再覆盖 sweep 与 post-sync evaluation 竞争，最终状态由重新评估后的最新事实决定。

**Step 3: 写 service disposition RED**

- repository 返回 `exact_duplicate` disposition 时，`AfterSuccessfulSync` 不被调用；`recorded` 正常评估，`suppressed` 仍调用既有行政恢复路径。
- post-sync mutation conflict 只重评一次；第二次 conflict 记录脱敏告警并返回成功，不发送通知。
- `TestServiceCommittedSyncProjectionConflictsPreserveResponse` 端到端覆盖 ApplyBatch 已提交并返回 pending action/plan、incident 连续两次 conflict：Agent 仍得到成功响应和原 action/plan，不能因 projection 失败而保留 entry 后命中空 duplicate plan。

**Step 4: 运行 RED**

```bash
GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run 'TestPostgresIntegration(SyncBatchLiveBackfillInterleaving|IncidentProjectionCAS)' -count=1
GOTOOLCHAIN=go1.26.2 go test ./internal/center/syncing ./internal/center/incidents -run 'Test.*(Duplicate|Suppressed|ProjectionConflict|CommittedSync)' -count=1
```

Expected: 现有 result 无 disposition；incident writer 无 row-version CAS；至少 stale mutation / duplicate side-effect 断言失败。Strict runner 必须显示 RUN/PASS 或预期 RED failure，不能 SKIP。

## 5. Center GREEN：disposition、deterministic latest 与 object row-version CAS

**Files:**

- Modify: `internal/center/syncing/service.go`
- Modify: `internal/center/syncing/service_test.go`
- Modify: `internal/center/store/sync_batches.go`
- Modify: `internal/center/store/sync_batches_postgres_integration_test.go`
- Modify: `internal/center/store/runtime_facts.go`
- Modify: `internal/center/store/runtime_facts_test.go`
- Modify: `internal/center/store/monitoring_instances.go`
- Modify: `internal/center/store/monitoring_instances_test.go`
- Modify: `internal/center/store/ip_quality.go`
- Modify: `internal/center/store/ip_quality_test.go`
- Modify: `internal/center/incidents/types.go`
- Modify: `internal/center/incidents/service.go`
- Modify: `internal/center/incidents/service_test.go`
- Modify: `internal/center/incidents/evaluator.go`
- Modify: `internal/center/incidents/evaluator_test.go`
- Modify: `internal/center/store/incidents.go`
- Modify: `internal/center/store/incidents_test.go`
- Create: `internal/center/store/incidents_postgres_integration_test.go`

**Step 1: schema/ACL no-change guard**

- 复跑既有 previous-exact-current + future-migration PostgreSQL regression，锁定 successor 会 rebuild-required 且 durable state 不变；因此本任务不得引入 `0063` 或改 current manifest/APP ACL fragment。
- catalog/direct-runtime 证明 base `monitoring_instances` / `targets` 的既有 SELECT/UPDATE 足以读取 row version、锁对象行并更新 summary；不得增加 Activity projection UPDATE 或 column ACL。
- `xmin` token 只在本次 evaluation/mutation 内使用，不写入 schema、日志、API、event payload 或任务证据。

**Step 2: sync disposition**

- `syncing.Result` 增加闭合 enum `recorded / exact_duplicate / suppressed`；不得用 bool 零值混淆 duplicate 与 inactive-object suppression。
- `syncing.Service` 只对 exact duplicate 跳过 post-sync；recorded 正常评估，suppressed 保留 paused/retired/archived 行政恢复。plan/AcceptedAt/pending action 既有 wire 行为保持兼容。

**Step 3: latest tie-break**

- host/probe/IP-quality/heartbeat agent-version latest 及 incident series 查询统一为 `observed_at DESC, is_backfilled ASC, received_at DESC, stable_row_key DESC`；host/probe/heartbeat 的稳定键为 `id`，IP-quality 为 `report_id`。对应 Go normalization 以 observed/backfill/received 做 stable sort，完全同值时保留 SQL 的最终稳定键顺序。
- 增加 equal-observed-at live-vs-backfill 与同 provenance tie tests。

**Step 4: row-version reader + CAS writer**

- 在每个 evaluation entrypoint 开始时读取 object `xmin::text`；构建 mutation 时携带 `ExpectedObjectRowVersion`。
- writer transaction 首先 `SELECT xmin::text ... FOR UPDATE`；不匹配在任何 destructive DML 前返回 `ErrIncidentProjectionConflict`。
- 成功 mutation 在同一 transaction 内替换 active set、写 events并更新 summary；object UPDATE 自动产生新 `xmin`，不需要 schema revision。
- notification dispatch/append 只发生在 mutation 成功后；incident processor conflict 时从 row version/object/incidents/raw facts 完整重读一次。
- direct-runtime PG16 测试覆盖两事务反序提交：新评估先成功、旧 mutation 后到必须整体回滚，active incidents、summary、events、notification records 均不得回退或追加旧副作用；任意无关 object UPDATE 只可造成安全 conflict。
- `xmin` 仅做 opaque equality token，不解析或排序；测试同时证明它未进入日志、API、event payload。第二次 conflict 必须记录安全告警并向 post-sync 返回成功，不能循环盲重放，也不能反转已提交 sync；后续 sweep 负责收敛。

**Step 5: 验证 Center GREEN**

```bash
GOTOOLCHAIN=go1.26.2 go test ./internal/center/syncing ./internal/center/incidents ./internal/center/store ./internal/center/store/migrate -count=1
GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store ./internal/center/store/migrate -run 'TestPostgresIntegration(SyncBatchLiveBackfillInterleaving|IncidentProjectionCAS|IncidentProjectionRowVersion)' -count=1
GOTOOLCHAIN=go1.26.2 go test -race ./internal/center/syncing ./internal/center/incidents ./internal/center/store -count=1
```

Expected: unit、strict PG16 direct-runtime、race 全部 PASS；catalog 证明无意外 grant。

## 6. Activity RED：current ACL 下生产 source pass

**Files:**

- Create: `internal/center/store/record_activity_runtime_acl_postgres_integration_test.go`
- Reference: `internal/center/store/record_activity.go`
- Reference: `internal/center/store/migrate/app_acl_current_contract.go`

**Step 1: 建 direct-runtime fixture**

- 复用 `newRecordsPostgresFixture` 与 current ACL convergence；确认 PostgreSQL major version 16。
- 用 direct runtime role 调生产 `EnsureActiveActivityProjectionGeneration` / `PublishActivityBatch`，不用 owner 代跑生产 DML。
- catalog 断言 projection 精确 SELECT/INSERT/DELETE=true、UPDATE=false、无 column ACL。

**Step 2: 写 acceptance RED**

`TestPostgresIntegrationRecordActivityRuntimeACL` 期望首次 publish 成功并推进 head。旧 SQL 会自然返回 SQLSTATE `42501`，测试只输出安全分类，不打印 DSN/credential/raw payload；owner connection 断言 RED 后无 partial row/watermark。

```bash
GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationRecordActivityRuntimeACL$' -count=1
```

Expected RED: production runtime path 在事实表 locking SELECT 处失败；不得临时 GRANT UPDATE 让 RED 消失。

## 7. Activity GREEN：移除事实表行锁，保留 head 串行化

**Files:**

- Modify: `internal/center/store/record_activity.go`
- Modify: `internal/center/store/record_activity_runtime_acl_postgres_integration_test.go`
- Modify: `internal/center/store/record_activity_postgres_integration_test.go`

**Step 1: 最小 SQL 修复**

- 只从 `loadExistingActivityHashes` 的 `record_activity_projection` SELECT 移除 `FOR UPDATE`。
- 保留 active head `FOR UPDATE`、严格 canonical hash mismatch、insert-only fact rows、watermark transaction。
- 不改 migration、不改 ACL fragment、不加 column/table UPDATE。

**Step 2: 扩充真实并发覆盖**

同一 direct-runtime test 或相邻 test 覆盖：

- first insert + watermark；
- exact retry 不重复；
- same identity / different canonical hash 整批失败并回滚；
- 两个 publisher 并发同一 generation，head lock 串行化且只产生一份 canonical facts；
- 后续连续 sequence/head watermark，无 gap/回退。

**Step 3: 运行 GREEN 和 catalog guard**

```bash
GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationRecordActivity(RuntimeACL|.*Concurrent.*)$' -count=1
GOTOOLCHAIN=go1.26.2 go test ./internal/center/store/migrate -run 'Test.*RecordActivity.*ACL' -count=1
```

Expected: strict PG lane RUN/PASS，runtime UPDATE 仍 false。

## 8. 规范与任务证据

**Files:**

- Modify: `.trellis/spec/backend/error-handling.md`
- Modify: `.trellis/spec/backend/database-guidelines.md`
- Modify: `.trellis/spec/backend/record-activity-projection.md`
- Modify: `.trellis/spec/backend/logging-guidelines.md`
- Modify: `.trellis/tasks/08-30-agent-replay-freshness-activity-acl/task.json`
- Modify: `.trellis/tasks/08-30-agent-replay-freshness-activity-acl/implement.jsonl`
- Modify: `.trellis/tasks/08-30-agent-replay-freshness-activity-acl/check.jsonl`
- Append evidence in task-local files only; do not include secrets.

逐项写入 design contracts、实际 RED/GREEN 命令与结果、strict PG RUN/PASS、privacy scan 和 known follow-ups。若实现文件集扩展到 incidents/migration，先更新 manifests，避免审查漏读。

## 9. 完整本地质量门禁

实施全部 GREEN 后执行；任一失败都回到对应 slice，不先行提交/交付：

```bash
GOTOOLCHAIN=go1.26.2 gofmt -w <本任务实际修改的 Go 文件>
GOTOOLCHAIN=go1.26.2 go test ./agent/runtime ./agent/syncqueue ./internal/center/syncing ./internal/center/incidents ./internal/center/store ./internal/center/store/migrate -count=1
GOTOOLCHAIN=go1.26.2 go test -race ./agent/runtime ./agent/syncqueue ./internal/center/syncing ./internal/center/incidents ./internal/center/store -count=1
GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegration(SyncBatchLiveBackfillInterleaving|ReplaySafeLatestStoreConsumers|IncidentProjectionCAS.*|RecordActivityRuntimeACL|ActivityHeadLockKeepsPublishedRangeContiguousUnderContention|EvidenceSources)$' -count=2
GOTOOLCHAIN=go1.26.2 make verify-go
git diff --check
```

PostgreSQL strict lane 使用上述 task-scoped selector，要求每个列出的测试实际 RUN/PASS；不要用对 `store`/`migrate` 整包无 selector 的调用替代它，因为整包同时包含需额外 MinIO 环境的可选测试和百万行性能 fixture。整包 Go 单元/静态覆盖由 `make verify-go` 承担。另对 diff/test output/task evidence 做 sentinel/敏感词扫描，人工确认没有 token、Authorization、DSN、fingerprint、raw payload、私有 URL 或真实对象 ID。

## 10. 独立审查与交付流程

1. 规划级和代码级 spec/quality/security independent review 都必须在提交前完成；所有 Critical/Important/Minor findings 修复并复审归零后才可进入提交。
2. review 通过后，按 Agent、Center、Activity、spec/evidence 四个 slice 形成提交，并按 Trellis Phase 3.4 展示一次完整提交计划、取得一次性确认后再 stage/commit。
3. Center CAS 与 Agent scheduler 必须进入同一 release，且 Center 先部署或原子同步部署。
4. 用户已明确授权完成 push/PR/required CI/merge/main CI/Release Please/release publish，以及约定测试环境的部署、验收和最终清理；不得绕过任何项目门禁或把授权扩张到其他目标。
5. 部署前从私有交接上下文重新验证精确目标，按项目部署文档建立冷恢复点；私有主机、路径和环境定位信息不得写入仓库。若连通性或精确目标无法验证，部署 fail closed，不猜测、不改动。
6. 发布后验收检查：`agent_sync_batches` 42501 仍为 0；Agent fresh heartbeat/host sample；replay catching_up/caught_up；Activity projection 42501 消失且 watermark 推进；常驻服务 healthy、三个 init job 仍 exit 0。

## 11. 预定提交/审查切片

1. `fix(agent): bound backlog replay without starving fresh sync`
2. `fix(center): make replay projections monotonic and idempotent`
3. `fix(center): run activity projection under immutable-fact ACL`
4. `docs(trellis): record replay and projection contracts`

若未来另行批准 Center API/UI observability，再增加独立计划和 review slice；不修改上述 core correctness 顺序。
