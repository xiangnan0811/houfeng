# Agent durable queue 回放调度研究

## 范围与结论

本文只回答 `research/field-audit.md` 的问题 1：Agent 如何在 durable queue 回放期间同时保证新鲜度、可恢复性、幂等和最终排空。本文不改变产品代码、部署或数据库。

建议采用 **双通道有限微轮次（bounded two-lane micro-round）**：每次 tick 先将完整的当前请求持久化到队列；清除过期 authority 的旧条目；按 FIFO 最多处理 `K=2` 个本轮之前已存在的有效 backlog；最后发送刚持久化的当前条目。回放通道内部继续严格 FIFO，但当前通道允许在每两个旧条目后越过剩余 backlog。

这项设计有一个必须显式确认的产品语义：**把“所有 durable entries 的全局严格 FIFO”放宽为“旧 backlog 内 FIFO，当前请求每轮可在固定 replay quantum 后越过剩余 backlog”**。若不接受这种有限越过，就无法同时给当前 heartbeat/host sample 一个与 backlog 深度无关的上界。

`K=2` 是建议默认值，而不是不可变协议常量：生产 tick 默认 5 秒，HTTP client timeout 为 10 秒，因此每轮当前请求之前至多发生 2 次回放 HTTP 调用；在不计本地磁盘/调度抖动时，开始当前请求的网络等待上界约为 20 秒，完成当前请求的网络等待上界约为 30 秒。若产品要求更严格的当前请求启动上界，应改为 `K=1`；若优先加速排空，可提高 `K`，但必须同步修改同一个可测试常量和相应 SLO。

## 现状控制流

1. `Runtime.Run` 建立默认 5 秒 ticker，并在同一个事件循环 goroutine 中消费 tick（`agent/runtime/runtime.go:24,269-357`）。
2. 每个 tick 用 ticker 自带时间构造完整 `SyncRequest`，包含 heartbeat、可能到期的 host/probe samples、IP reports 和 command results（`agent/runtime/runtime.go:324-326,403-440`）。
3. queue 模式调用 `enqueueAndFlush`：先 `Enqueue`，持久化成功后才从内存 pending 区确认 command/IP 辅助 payload；然后 `Prune`；最后同步调用 `flushSyncQueue`（`agent/runtime/runtime.go:341,443-455`）。
4. `flushSyncQueue` 一次列出完整快照，先用一次 `DeleteMany` 清除过期 authority 条目，再对剩余条目执行无数量上限的 `for` 循环：发送、按错误策略标记/保留或删除、应用响应计划（`agent/runtime/runtime.go:470-556`）。
5. `FileStore.List` 按 `CreatedAt`、再按本地 `Entry.ID` 排序，因而当前实现是全局 oldest-first（`agent/syncqueue/store.go:160-187,528-578`）。
6. 失败重试或跨 tick 回放时，runtime 只在发送副本上标记 heartbeat/host/probe/IP facts 为 backfilled；原始 observed timestamps 与 carrier `SyncBatchID` 不变（`agent/runtime/runtime.go:558-567`; `agent/syncqueue/store.go:291-305`）。

## 阻塞根因

根因不是 enqueue，也不是“Agent 没有 tick”，而是 **tick 消费、backlog 排空和当前同步共用一个串行 goroutine，且排空循环没有每轮工作量上限**。`enqueueAndFlush` 在完整 `flushSyncQueue` 返回前不会返回 `Run` 的 `select`；此时 ticker 即使继续产生时间，也不能驱动新的采样/同步（`agent/runtime/runtime.go:312-355,443-455,470-510`）。

因此，只要旧队列条目持续成功回放，单轮 flush 的耗时近似为 `backlog_size × (HTTP latency + durable delete cost)`。HTTP client 单次 timeout 是 10 秒（`agent/enroll/client.go:38-44`），而每个 ack 后的 `Delete` 都会在锁内读、重写并原子替换整个 queue 文件（`agent/syncqueue/store.go:189-210,385-435`）。大 backlog 同时放大网络串行耗时和本地文件重写耗时。即使所有请求都成功，这个循环也可持续数小时；在此期间 Center 会持续看到被回放批次更新的 `last_sync_at`，但新 tick 没有机会生成实时 heartbeat/host sample。

仅把 flush 改成“每 tick 前 K 个 oldest”可以限制单轮耗时，却不能满足新鲜度：当前条目仍追加在队尾，必须等待约 `ceil(backlog/K)` 个 tick 才能发送。反过来，当前条目若先发送、再回放旧条目，虽然新鲜，但旧批次的 duplicate 响应可能返回显式空计划并在本轮最后覆盖最新计划：Center 对重复 marker 提交空计划且不重写事实（`internal/center/store/sync_batches.go:101-114`），runtime 当前对每个成功响应都替换 `currentPlan`（`agent/runtime/runtime.go:504-507,689-710`）。所以建议让当前响应在有限微轮次中最后应用。

## 不可破坏的合同

| 合同 | 现状证据 | 调度约束 |
| --- | --- | --- |
| 持久化先于发送 | `Enqueue` 完成后才 ack 内存 pending payload，随后 flush（`agent/runtime/runtime.go:443-455`） | 当前通道也必须先落盘；禁止绕过队列直接发送。 |
| carrier 幂等键不变 | 本地 `Entry.ID` 可加碰撞后缀，但 request 的 `SyncBatchID` 保持原值（`agent/syncqueue/store.go:438-459,528-578`） | 用 `Enqueue` 返回的本地 `Entry.ID` 精确识别当前 durable carrier；不得为调度重写 `SyncBatchID`。 |
| Center 整批 INSERT-only 幂等 | marker 首次插入才写 facts；重复 marker 不重写（`internal/center/store/sync_batches.go:101-175`; `.trellis/spec/backend/database-guidelines.md:1326-1382`） | 不能把一个请求拆为多个使用相同 batch ID 的“最小 heartbeat”和“稍后 aux”请求；后到部分会被 marker 去重。 |
| backfill 保真 | 只在发送副本上标记事实为 backfilled，observed time 不改（`agent/runtime/runtime.go:558-567`; `agent/syncqueue/store.go:291-305`） | 旧条目/重试条目仍按现有条件标记；当前 tick 第一次发送不标记。 |
| 错误分类 | 仅 invalid JSON/request 的 400 可在可靠分类后丢弃；永久 4xx 终止；429/5xx/transport 重试；本地 mark/delete 错误停止（`.trellis/spec/backend/error-handling.md:307-384`） | 新调度只能改变选择顺序，不能改变 delete/retain/stop 策略。 |
| stale authority 批量清除 | 当前先分类、一次 `DeleteMany`、再逐条 flush（`agent/runtime/runtime.go:470-556`） | 保留批量删除，不能逐条重复重写 queue；日志必须在可靠删除后出现。 |
| queue 有界与耐久 | 默认 65,536 条、72h、64MiB，oldest-first prune，超大最新条目 fail closed（`agent/syncqueue/store.go:17-20,308-355`; `.trellis/spec/backend/quality-guidelines.md:272-327`） | 调度不得把 prune 变成优先删除未发送 backlog；超限仍走既有 fail-closed 规则。 |
| aux payload 原子 carrier | `SyncRequest` 同时承载 IP reports 与 command results（`internal/contracts/agentapi/types.go:218-230,268-276`） | 当前请求必须完整持久化、完整发送、失败时完整保留；不能为了 freshness 拆包或生成第二个幂等键。 |
| 安全日志 | queue 日志只能输出稳定安全元数据，不含 token/原始 payload（`.trellis/spec/backend/logging-guidelines.md:91,99-107`） | 新增 quantum/fairness 日志时也只记录计数、entry/batch ID 和分类后的错误。 |

## 方案比较

### 方案 A：严格 FIFO，每 tick 最多 K 个

- 优点：实现和现有语义最接近；全局 FIFO、重试 head-of-line 行为不变；单轮工作量有界。
- 缺点：新条目仍在队尾，新鲜度为 `O(backlog/K)`，不能给 heartbeat/host sample 一个与 backlog 深度无关的上界。对已经积压数万条的机器不解决核心事故。
- 结论：不推荐，只解决单轮阻塞，不解决 freshness。

### 方案 B：当前请求先发送，再回放 K 个旧条目

- 优点：当前请求最快，开始发送前没有 backlog HTTP 等待。
- 缺点：全局 FIFO 被放宽；更重要的是旧 duplicate batch 可能在本轮最后返回空 plan，覆盖当前请求刚获得的最新 plan。若要安全采用，需要额外修改 plan 协议/应用规则，例如区分“重复响应没有计划”与“权威空计划”，超出最小调度改动并扩大风险。
- 结论：不推荐作为首版。

### 方案 C：先有限 FIFO 回放，再发送当前 durable carrier（推荐）

- 优点：当前请求之前的工作量与 backlog 深度无关；backlog 仍按顺序排空；当前响应最后应用，避免历史/duplicate 响应在本轮末尾回退计划；所有 payload 仍走同一 durable carrier。
- 缺点：必须明确放宽全局 FIFO；当前请求的等待仍包含至多 K 次旧 HTTP 调用；需要重写几项依赖“整队一次排空”的 runtime 测试。
- 结论：以 `K=2` 起步，调度常量和测试共同锁定上界。

## 推荐状态机

每个 tick 执行以下一个微轮次：

1. 构建完整 current request，调用 `queue.Enqueue`；保存其返回的唯一 local `Entry.ID`。只有成功后才 ack 内存 pending command/IP payload。
2. 执行既有 `Prune`。若当前 entry 因 fail-closed/oversize 或本地错误未能耐久存在，停止本轮，不发送未持久化请求。
3. `List` 一次，按既有规则找出 stale-authority IDs，并一次 `DeleteMany`。从有效条目中分离 `entry.ID == currentEntryID` 的当前 carrier；其余是 replay lane。不得用 `SyncBatchID` 区分，因为本地兼容/碰撞规范允许相同 carrier batch ID 对应不同 local ID。
4. 从 replay lane 头部最多处理 `K=2` 个条目。成功 ack 后可靠删除并应用计划；可丢弃 poison 仅在可靠删除后继续；遇到 retryable head 时先可靠 `MarkAttempt`，保留它并停止本轮 replay lane，禁止更晚 backlog 越过它，但继续 current lane；遇到 permanent/local durability error 按既有终止合同停止。
5. 若本轮仍可继续，发送 exact current carrier。成功后可靠删除并最后应用响应计划；retryable 时 `MarkAttempt` 并保留；所有失败策略沿用现有分类。
6. 返回事件循环。下一 tick 中，前一轮失败的 current 已成为 replay lane 的旧条目，发送副本将按既有规则标记 backfilled；新 current 仍会在至多两个旧尝试后获得发送机会。

建议将选择/发送逻辑从当前无界 `flushSyncQueue` 抽成一个显式、可注入 quantum 的小函数，但生产只使用一个常量。不要新增并发发送：串行微轮次让本地文件修改、FIFO head retry、计划应用次序和取消语义保持可推理。

## 公平性、排空与上界

设每轮开始时旧 backlog 为 `B_n`，且 Center/网络/本地 durability 都成功。`K=2` 时：

```text
B_(n+1) = max(B_n - 2, 0)
```

因为本轮 current 在同轮成功删除，不增加下一轮 backlog，所以稳定成功窗口内不会饿死旧条目；任何固定旧条目最多等待 `ceil(position/2)` 个成功微轮次。默认每 5 秒一个 tick，理论 replay 吞吐为 0.4 entry/s：

- 72 小时 retention 在默认 tick 下最多约 `72h × 3600 / 5 = 51,840` 个按 tick 产生的条目，名义排空时间不超过 36 小时。
- 只按默认 65,536 entry 上限计算，名义排空时间约 45.5 小时。
- 若 replay 持续失败，无法保证排空；这是外部可用性条件，不应通过让后续 backlog 越过失败 head 来伪装成功。当前 lane 仍每轮尝试，因此 freshness 不会被该 head 无限阻塞。

当前请求的确定性工程上界应定义成“之前最多 `K` 次 replay send attempt”，而不是只写 wall-clock。现有 HTTP timeout 为 10 秒，所以 `K=2` 给出约 20 秒的网络等待到 current attempt 开始、约 30 秒到 current response 完成。文件队列操作还包括最多两次旧条目 ack-delete、当前 delete、一次 prune 和可能一次 stale bulk delete；文件大小受 64MiB 上限约束，但当前实现没有硬磁盘耗时 deadline，因此不能宣称绝对 wall-clock 上界。若验收要求绝对时间 SLO，还需另行给本地 queue 操作 deadline/性能门槛。

错误公平性：

- retryable backlog head：本轮 replay lane 停止，保留旧 FIFO；current lane 继续，避免新鲜度饥饿。
- poison 400：可靠删除后可继续消耗 quantum；删除失败立即停止。
- permanent 4xx 或本地 durability 失败：沿既有合同终止，不以 freshness 为由越过需要人工处置的错误。
- stale authority：先批量可靠删除，不消耗 HTTP replay quantum；删除失败停止，以免日志/队列状态失真。

## 精确 RED / GREEN 测试计划

### `agent/runtime/runtime_test.go`

1. 新增 `TestRuntimeBoundsReplayBeforeCurrentDurableRequest`（核心 RED）。预置 3 个有效 backlog（`K+1`），触发一个 tick，在第三个成功请求后取消。断言出站顺序为 `backlog-one`、`backlog-two`、`current`；前两项为 backfilled，第三项不是；队列只剩 `backlog-three`。现状会先发送第三个 backlog，因此稳定 RED，不依赖墙钟耗时。GREEN 后锁定“current 之前最多 2 个旧条目”。
2. 新增 `TestRuntimeReplaysBacklogFIFOAcrossFreshInterleaving`。运行多个 tick，筛选所有 backfilled carrier，断言旧 batch 顺序始终为 `old-1, old-2, old-3...`；每个微轮次都恰有一个 current，且它之前至多两个 old。锁定 replay lane FIFO 和无饥饿。
3. 新增 `TestRuntimeRetryableBacklogHeadDoesNotBlockCurrentDurableRequest`。让 oldest 对 503（再分别表驱动 429/transport）失败；断言它被 `MarkAttempt` 并保留、更晚 backlog 未发送，但 exact current 仍在同轮尝试并成功；下一轮该 head 先于新的 current 重试且带 backfilled。锁定 head retry 与 current fairness 同时成立。
4. 新增 `TestRuntimeCurrentRequestRetryRemainsDurableAndBackfilled`。本轮 current 返回 retryable；断言 entry 保留、carrier `SyncBatchID` 不变、attempt 增加；下一轮它作为 replay lane 的旧条目发送且 facts 标记 backfilled。锁定“越过不等于绕过 durability”。
5. 新增 `TestRuntimeCurrentResponsePlanWinsAfterBoundedReplay`。给 backlog 回复显式空 plan（模拟 duplicate batch），给 current 回复 host 5 秒计划；下一 tick 断言 host sample 按 current plan 到期。现状若采用“current-first”会失败；GREEN 锁定 current-last 的计划顺序理由。
6. 新增 `TestRuntimeBoundedReplayPreservesDurableAuxiliaryPayloads`。current 同时包含 command result 与 IP report；在每次 send 前检查 queue snapshot 已含该 exact carrier 和完整 aux payload；current transient 失败后断言 payload 仍在同一 `SyncBatchID` 下，下轮完整重试。不得通过拆分 payload 使测试通过。
7. 改写 `TestRuntimePreservesOldestFirstForValidCurrentAuthorityBacklog`（现位于 `agent/runtime/runtime_test.go:1986-2027`）：不再断言完整全局序列 `backlog-one, backlog-two,current` 在所有 backlog 长度上成立；改为断言 replay 子序列 FIFO，以及 current 在 quantum 后出现。
8. 复核并按新微轮次调整现有取消/次数假设：`TestRuntimeQueuesFailedSyncAndRetriesAsBackfilled`、`TestRuntimeTransientQueueFailuresRemainRetryableAndBackfilled`、`TestRuntimeFlushesPersistedQueueAfterRestart`，同时保留原有 SyncBatchID、observed time、backfilled 和 restart durability 断言。

### `agent/syncqueue/store_test.go`

生产 store 不需要新增“优先队列”API；调度层可用现有 `Enqueue` 返回的 local ID 与一次 `List` 完成分 lane。现有测试已经覆盖 persistence、oldest-first、DeleteMany、MarkAttempt、local-ID 碰撞后缀不改 carrier、legacy/duplicate ID normalization、并发 mutator、权限和三类上限。若实现抽取了选择 helper，只新增纯选择单测，避免改变 on-disk schema。

可增加一项窄回归 `TestFileStoreEnqueueIDSelectsExactDurableCarrierAfterCollision`：连续 enqueue 相同 `SyncBatchID`，断言返回的 local IDs 分别选择两个 exact entries，而 request carrier ID 均未变化。这不是新行为，只为 current-lane 选择依据提供直接证据。

### Center 合同回归（不属于 Agent 调度实现，但用于验收）

- 保留 `internal/center/store/sync_batches_test.go` 的 `TestPostgresSyncRepositoryDuplicateBatchCommitsWithoutRewritingFacts`，证明 current/backlog 交错不会绕过 batch marker。
- 保留 IP report、matching/mismatched command result 测试，证明完整 aux carrier 的事务语义。
- 保留 `internal/center/store/sync_batches_postgres_integration_test.go` 的 `TestPostgresIntegrationAgentSyncBatchRuntimeACL`；调度变更不能以 ACL 失败为“队列阻塞”替代解释。

### 计划中的 GREEN 命令（本研究阶段不执行）

```bash
go test ./agent/runtime -run 'BoundsReplay|BacklogFIFO|RetryableBacklog|CurrentRequestRetry|CurrentResponsePlan|AuxiliaryPayloads' -count=10
go test ./agent/syncqueue -count=1
go test -race ./agent/runtime ./agent/syncqueue -count=10
make verify-go
```

`-count=10` 用于捕捉微轮次/cancel 的非确定性，`-race` 用于确认没有因选择 helper 或测试注入引入共享状态竞争。测试应使用受控 fake client 和明确请求计数，不以 `time.Sleep` 判定 20/30 秒 SLO。

## 问题 1 的直接回答

采用“旧 backlog FIFO replay lane + current durable lane”的固定权重轮转：每个 tick 先完整落盘当前请求，再最多回放两个旧条目，最后发送当前条目。它以显式放宽全局 FIFO 换取 backlog 无关的新鲜度上界，同时保持旧条目之间 FIFO、整批 SyncBatchID 幂等、backfilled 标记、错误分类、restart durability 和 aux payload 原子性。成功条件下 backlog 每轮净减少 2，因而最终排空；失败 head 不阻塞 current，但仍阻止后续旧条目越过。最高价值的用户选择是确认这一 FIFO 语义变化；若必须保持全局严格 FIFO，则问题 1 的四个目标不能同时满足，应明确接受 freshness 随 backlog 线性退化。
