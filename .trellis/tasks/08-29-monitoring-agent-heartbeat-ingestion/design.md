# Agent 心跳队列永久拒绝修复设计

## 1. Recommended approach

采用“当前 authority 识别 + typed remote error 分类”的最小 agent-runtime 修复。

- 当前 authority 由 runtime 已加载的 MonitoringInstance ID、sync token 和本机 fingerprint 组成；它只在本地内存与既有加密边界内比较，不进入日志。
- 每次 tick 仍先持久化当前请求，保留 crash-safe 与离线回填语义。
- command result 与已 drain 的 IP-quality report 只有在 queue Enqueue durable 成功（compatibility no-queue 路径为 Sync 成功）后才从 runtime buffer 清理；本地写盘失败不能先消费 payload。
- flush 在发送前用一次原子 batch delete 淘汰与当前 authority 不一致的历史项，并继续扫描队列；一个旧身份项或完整 72 小时 stale backlog 都不能再阻塞当前心跳。
- 对已发送项使用 `errors.As` 读取 `*enroll.RemoteError` 的 status/code，区分 discard、terminal 与 retry。
- center、DTO、数据库和 Web 状态合同保持不变；只有 center 接受的真实 heartbeat/HostSample 才能推进页面状态。

不采用基于次数的盲目丢弃、UI 强制置绿或要求用户删除/重建资产。

## 2. Queue authority model

Runtime 在 enrollment 或持久凭据加载完成后已经持有当前：

- `monitoringInstanceID`；
- `syncToken`；
- `fingerprint`。

`enqueueAndFlush` / `flushSyncQueue` 接收这组当前 authority，而不是只接收 current batch ID。队列项满足以下条件才属于当前 authority：

1. `Request.MonitoringInstanceID` 与当前 ID 精确相等；
2. `Request.SyncToken` 与当前 token 精确相等；
3. 至少有一个 heartbeat，且所有携带 fingerprint 的 heartbeat/HostSample/probe/IP-quality carrier 均与当前 fingerprint 精确相等；
4. heartbeat 的 batch ID 非空，满足 runtime 队列发送的最小身份载体要求。

不匹配项视为本机持久状态中的 stale authority，不发送到 center。runtime 先在一次 queue file 原子替换中批量删除本轮可独立删除的 stale ID，成功后才按 reason 聚合写 Error 级脱敏日志，然后继续 current 项。日志只包含 stable action/reason/count，不包含 persisted queue entry ID、MonitoringInstance ID、token、原始 fingerprint、batch payload、raw local cause 或自由文本 body。

生产 `FileStore` 在 enqueue 时给 heartbeat batch ID 冲突分配 suffix，并在读取 legacy/hostile 文件时确定性规范化 duplicate/empty entry ID。List、Delete、DeleteMany 与 MarkAttempt 必须复用同一映射，因此同 ID 的多个 retained current facts 可以 oldest-first 分别 ack；第一个 Delete 不能删除尚未发送的第二个 fact。runtime 仍保留 collision guard，保护未提供唯一 ID 合同的其他 `SyncQueue`：stale 与 retained current 项共享 ID 时禁止 stale ID-wide delete。

suffix 只属于本地 `Entry.ID`，不能改写 carrier `SyncBatchID`；重复 SyncBatchID 仍表示 center 的同一个幂等事实，不能声称 suffix 会创造两个独立的 center facts。duplicate entry ID 规范化使用 per-base 单调 suffix cursor，默认容量量级的恢复不得退化成每项从 `-1` 重扫的 O(n²) 算法。

该判断保留同一当前 authority 的合法离线历史；旧 observed time 或 attempts 本身不是删除理由。

## 3. Remote error decision matrix

| Result | Queue action | Runtime action | Rationale |
| --- | --- | --- | --- |
| success 2xx | delete acknowledged entry | apply plan and continue | existing contract |
| HTTP 400 + `invalid_json` / `invalid_request` | delete poison entry, then sanitized Error | continue scanning | spec requires record then discard, never infinite retry |
| current-authority 401 / `invalid_sync_token` | mark attempt, retain | return terminal sanitized error | credential intervention required |
| current-authority 404 / instance not found | mark attempt, retain | return terminal sanitized error | retry cannot recreate authority |
| current-authority 409 / binding not accepted | mark attempt, retain | return terminal sanitized error | binding intervention required |
| 405 or other non-429 4xx not explicitly discardable | mark attempt, retain | return terminal sanitized error | configuration/protocol intervention required |
| 429 | mark attempt, retain | return retryable error to next tick | rate limit is recoverable |
| 503 or other 5xx | mark attempt, retain | return retryable error to next tick | server failure is recoverable |
| transport/context-independent client failure | mark attempt, retain | return retryable error to next tick | preserve offline durability |
| typed remote error with status 0, 2xx or 3xx | mark attempt, retain | return retryable error to next tick | ambiguous client result must not cause speculative deletion |
| local queue persistence/delete/mark failure | preserve existing error behavior | terminate runtime | local durability cannot be claimed |

For terminal errors the queued current request remains available. After an operator corrects credentials/binding and runtime loads new authority, the old item becomes stale and is safely discarded; no manual queue edit is required.

Parent `context.Canceled` remains a normal shutdown at fingerprint、credential load、enrollment、non-queue client 与 queue boundaries；只在 `ctx.Err()` 已设置时吞掉 cancellation，客户端内部独立返回的 canceled error 仍按真实失败处理。A terminal classification exits `Run`; the existing systemd restart policy may restart the process, but journal/status now shows a repeated actionable failure instead of a long-lived false healthy loop.

## 4. Sanitized error and logging contract

Remote response `Message` and fallback raw body are untrusted and must not reach the operational runtime error string. The classification layer produces a sanitized error containing only:

- stable phase (`sync` / `queue`);
- HTTP status when known;
- allowlisted `agentapi.ErrorCode*` when known;
- queue action/reason;
- stable non-secret count/reason only; persisted entry/MonitoringInstance identifiers are never needed in this operational path.

The original typed remote/local error remains reachable through `Unwrap` when needed for classification/tests, but its free-form message is not formatted into outer logs. Fingerprint、credential load、enrollment token 与 credential persistence 也使用 stable operation wrapper。Token, Authorization, raw fingerprint, request JSON, response body, persisted queue identifiers and observation payloads are forbidden. Enrollment 与没有 durable queue 的兼容 sync 路径使用同一脱敏原则。

Discard events use Error level because they represent lost stale/invalid facts and require fleet visibility. Normal successful sync remains quiet to avoid hot-path log volume.

Terminal failures are returned without logging inside runtime; `cmd/houfeng-agent` is the single outer logging boundary. This preserves one actionable journal event per failed process run instead of duplicate inner/outer reports.

`agent enrolled` 只在 bound response 校验、sync token 非空与凭据 durable persistence 全部成功后记录，且只记录 allowlisted status/binding status，不记录 MonitoringInstance ID；start/stop 的 `server_url` 只保留 parsed origin，避免 URL userinfo/path/query/fragment 进入 journal。

## 5. Recovery and compatibility

- A v0.79.1 host with a stale queue head recovers after installing/restarting the fixed agent: stale-authority entries are removed and the current heartbeat is attempted in the same flush.
- A host with valid same-instance credentials and valid queued facts behaves exactly as before: oldest-first, retry and backfill are preserved.
- A legacy/hostile queue that reused an entry ID exposes independently addressable facts; a successful first ack cannot remove a later current-authority fact before it is sent.
- A local Enqueue failure leaves pending command/IP-quality payload buffered in the same Runtime for a retry. If MaxBytes cannot retain even the newest entry, FileStore returns a local durability error without rewriting the prior queue instead of silently reporting success.
- A host whose current saved credential itself is invalid stops with a sanitized terminal error; the agent must not silently generate a new enrollment or weaken center binding authority.
- A first accepted heartbeat obtains the existing center plan. HostSample is collected on a subsequent due tick, after which existing center lifecycle/read APIs expose `在用`, agent version and runtime facts.
- Existing installer behavior that preserves post-enrollment JSON remains unchanged. Cross-instance reuse or explicit reset/re-enroll semantics are deferred because the generated command currently does not carry a safe target identity usable for correlation.

## 6. Files and boundaries

Expected product edits:

- `agent/runtime/runtime.go`: current-authority validation, queue decision flow, terminal/retry/discard classification, pending payload durable handoff and sanitized errors/logs.
- `agent/runtime/runtime_test.go`: deterministic RED/GREEN matrix with real `syncqueue.FileStore`, typed remote errors and captured slog output.
- `agent/syncqueue/store.go`: atomic `DeleteMany` primitive，避免大 stale backlog 逐项 rewrite/fsync；统一规范化 duplicate/empty ID，并在 path lock 后和 durable mutation 前重检 cancellation。
- `agent/syncqueue/store_test.go`: batch persistence、duplicate/missing ID、独立 ack 与 cancellation 回归。

Only if tests expose a clean reusable need:

- `agent/enroll/client.go` / tests: a sanitized typed error helper without changing wire behavior.
- `.trellis/spec/backend/error-handling.md`: correct stale line anchors or add executable matrix detail after implementation; the semantic rule already exists and does not need invention.

Do not modify `internal/contracts/agentapi`, center handlers/store, migrations, installer or Web unless a separately observed RED proves an independent defect and the plan is re-approved.

## 7. TDD and verification model

1. Establish focused baseline and record current GREEN.
2. Add the persisted old-authority queue-head regression and observe the current implementation fail because the current heartbeat is never attempted.
3. Add table-driven classification tests before implementation: stale authority, poison 400, current permanent 4xx, ambiguous status 0/2xx/3xx, transient 429/5xx/transport, valid same-authority backlog.
4. Implement the smallest queue decision/classification helpers and make each RED GREEN.
5. Add captured-log assertions proving stable diagnostics and absence of all fixture secrets, raw fingerprints and response text.
6. Re-run runtime/syncqueue/enroll and center handler/store packages, then repeat/race tests and `make verify-go`.
7. Review PRD acceptance, spec compliance, privacy, durable queue invariants and diff scope independently before protected delivery.

No Web change is planned, so browser screenshots are observational evidence only. The protocol acceptance proof is that the current heartbeat reaches the fake/real handler contract and center's existing tests still prove persistence/read semantics.

## 8. Risks and rollback

- **False stale classification:** mitigated by exact current ID/token/fingerprint equality and tests preserving same-authority history.
- **Data loss on ambiguous failure:** only explicit stale authority or stable invalid request codes are discarded; transient and unknown client failures remain queued.
- **Restart storm on current invalid credentials:** intentional fail-visible behavior required by spec; errors are sanitized and the retained entry becomes discardable after authority correction.
- **Secret leakage:** captured-log negative tests cover every fixture token/fingerprint/remote message; diff review searches logging and formatting paths.
- **Large-backlog recovery:** stale 项必须一次 batch delete 并按 reason 聚合日志，避免默认五万级队列的 O(n²) rewrite/fsync 与日志洪泛。
- **Persisted ID collision:** FileStore 在 runtime 看到 facts 前稳定规范化重复 ID，enqueue 时也分配 suffix；runtime collision guard 继续保护其他 queue 实现，防止 current fact 被 ID-wide delete 误删。
- **Rollback:** revert the agent runtime change. No schema or center state migration exists. Rolling back reintroduces head-of-line blocking but does not require data rollback.

Implementation has passed the local Trellis review. Delivery still follows feature branch → protected PR → required CI → merge → post-merge/release artifact verification; no commit or publication is performed by the reviewer.
