# Agent replay 可观测性研究

## 结论

对 `field-audit.md` 问题 3 的回答是：**既有日志、MonitoringInstance API 和页面字段不足以可靠地区分“同步传输正常但仍在追赶 durable backlog”与断连/失败；需要一项最小可观测性变化，但 MVP 首选 Agent 结构化日志，不需要先改 Agent↔Center 协议、Center API 或 Web UI。**

原因有三点：

1. Agent 对 retryable sync failure 已有经过清洗的 Error 日志，但成功 flush 完全静默；“没有错误”不能作为“正在健康追赶”的正证据。
2. Center 的 `last_sync_at` 使用接收时间推进，而 `last_heartbeat_at` 使用事实的 `observed_at` 推进。两者呈现“前者新、后者旧”时，可以旁证 Center 仍在接收旧事实，但不能证明 Agent 本地尚余多少 backlog、是否持续前进，也不能在 Center 侧区分断连与反复失败。
3. heartbeat 原始事实已经保存 `received_at` 与 `is_backfilled`，但 MonitoringInstance 的公开 read model/API/UI 没有暴露这份 provenance；依赖数据库人工查询不是稳定的日常运维界面。

因此推荐：在计划中的 bounded/fair flush 结果上增加 Agent 本地的、限频的 replay 状态日志。只有当产品要求“无需登录 Agent 主机、必须从 Center 页面判断”时，才增加 Center 只读派生状态；不要在本次 MVP 直接引入 Agent 队列遥测协议。

## 现有信号盘点

| 信号 | 当前存在 | 可以证明 | 不能证明 |
| --- | --- | --- | --- |
| Agent `agent runtime started/stopped` | 是 | 进程曾启动/停止；仅记录清洗后的 Center origin | 当前一次 sync 是否成功、是否有 backlog、是否在前进 |
| Agent `sync queue flush failed` | 是 | 最近一次尝试发生 retryable transport/remote failure，且 `kind/action/status/code` 是稳定、清洗后的诊断 | 没有该日志时 sync 一定成功；失败前是否已成功删除部分 backlog |
| Agent 成功 flush/progress 日志 | 否 | — | 无法从 journal 获得“正在健康追赶”的正证据 |
| 本地 queue `Entry` | 是，仅 Agent 本地 | 可由 `CreatedAt/Attempts/Request` 判断旧 entry 与 durable remaining count | Center 无法读取；原始 Request 含凭据与完整 payload，不能直接输出 |
| Center `last_sync_at` | 是，API 有；当前页面不展示 | Center 最近接受某个 heartbeat batch 的接收时间 | batch 是否 backfilled、Agent 本地还剩多少、无新接收是断连还是请求失败 |
| Center `last_heartbeat_at` | 是，API/UI 有 | 最大 heartbeat `observed_at` | 传输新鲜度；旧值不等于 Agent 已断连 |
| heartbeat `received_at/is_backfilled` 原始事实 | 是，数据库层 | 最近收到的事实是否明确为补传；与 `observed_at` 一起可证明 Center 正在接收旧事实 | Agent queue 的剩余量；未经 read model 暴露时不能作为普通 UI 证据 |
| latest host sample `received_at/is_backfilled` | API 类型已有，但样本可为空 | 样本存在时可判断该样本是否补传 | 无 HostSample 的现场完全无效；页面 header 只使用 `observed_at`，没有显示 provenance |

### 源码证据

- Runtime 默认 5 秒 tick；每次 tick 先构造当前 request，再同步执行 enqueue/flush。retryable 错误仅写 `sync queue flush failed`，没有成功日志：[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:24)、[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:319)、[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:341)。
- `SyncQueue` 只有 enqueue/list/delete/mark/prune，没有统计快照；Runtime 仍可在已有 `List` 结果内安全计算 aggregate count，无需扩大 queue 持久化格式：[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:175)。
- 当前 flush 遍历全部 entries，成功后 durable delete，失败则 mark attempt 并分类返回；这正是“成功但长时间占用 tick”且“成功路径无可观测性”的边界：[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:470)。
- 旧 batch 或已有 attempt 的 request 在发送副本上标为 backfilled，因此 Agent 内部已经有无需新协议即可复用的 replay 判定：[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:558)。
- retry error 的字符串仅包含 `kind/action/status/code`，日志 helper 也只输出这些 allowlisted fields；这套隐私边界应原样复用：[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:109)、[agent/runtime/runtime.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime.go:570)。
- Agent sync contract 的 heartbeat 已含 `is_backfilled`，但 request/response 没有 queue depth/replay state：[internal/contracts/agentapi/types.go](/home/murray/.codex/worktrees/2313/houfeng/internal/contracts/agentapi/types.go:74)、[internal/contracts/agentapi/types.go](/home/murray/.codex/worktrees/2313/houfeng/internal/contracts/agentapi/types.go:218)、[internal/contracts/agentapi/types.go](/home/murray/.codex/worktrees/2313/houfeng/internal/contracts/agentapi/types.go:278)。
- Center 保存 heartbeat 的 observed/received/backfilled provenance，并分别以 observed time 和 receive time 推进 `last_heartbeat_at/last_sync_at`：[internal/center/store/sync_batches.go](/home/murray/.codex/worktrees/2313/houfeng/internal/center/store/sync_batches.go:462)、[internal/center/store/sync_batches.go](/home/murray/.codex/worktrees/2313/houfeng/internal/center/store/sync_batches.go:505)。
- MonitoringInstance record 只公开两个时间，没有 replay/failure/backlog 字段；HTTP handler 直接序列化该 record：[internal/center/monitoringinstances/types.go](/home/murray/.codex/worktrees/2313/houfeng/internal/center/monitoringinstances/types.go:86)、[internal/center/http/handlers/monitoring_instances.go](/home/murray/.codex/worktrees/2313/houfeng/internal/center/http/handlers/monitoring_instances.go:21)、[internal/center/http/handlers/monitoring_instances.go](/home/murray/.codex/worktrees/2313/houfeng/internal/center/http/handlers/monitoring_instances.go:61)。
- Web record 同样只有 `last_heartbeat_at/last_sync_at`；列表只展示 heartbeat，详情 header 也只显示 heartbeat/uptime。没有 HostSample 时页面只提示等待下一次同步：[web/src/lib/types.ts](/home/murray/.codex/worktrees/2313/houfeng/web/src/lib/types.ts:3)、[web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx](/home/murray/.codex/worktrees/2313/houfeng/web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx:26)、[web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx](/home/murray/.codex/worktrees/2313/houfeng/web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx:113)、[web/src/components/monitoring-detail/MonitoringInstanceWatchtowerHeader.tsx](/home/murray/.codex/worktrees/2313/houfeng/web/src/components/monitoring-detail/MonitoringInstanceWatchtowerHeader.tsx:109)、[web/src/components/monitoring-detail/MonitoringInstanceWatchtowerMetrics.tsx](/home/murray/.codex/worktrees/2313/houfeng/web/src/components/monitoring-detail/MonitoringInstanceWatchtowerMetrics.tsx:122)。
- `HostSample` transport 类型已有 `received_at/is_backfilled`，但它不是 heartbeat transport 的替代物，而且可能不存在：[web/src/lib/types.ts](/home/murray/.codex/worktrees/2313/houfeng/web/src/lib/types.ts:218)。

## 如何判定三类状态

应把“事实发生时间”和“传输状态”分开，不要继续让 `last_heartbeat_at` 一列同时承担两种语义。

### 1. 实时同步（live）

可验证条件应是：本次 current heartbeat 已被接受，且不存在更旧的 durable replay entry。Center 侧表现通常是 `last_sync_at` 与 `last_heartbeat_at` 都新，但 Agent 时钟偏差意味着不能只靠时间差下绝对结论。

### 2. 传输正常、正在追赶（catching_up）

Agent 侧的强条件是：flush 开始时存在旧 durable entries，本轮至少一个旧 entry 已经得到 Center ack 并完成本地 durable delete，同时本轮结束后仍有旧 entry。该定义既证明“传输成功”，也证明“backlog 尚未清空”；不能仅以 queue 非空或没有错误代替。

Center 的 `last_sync_at` 新、`last_heartbeat_at` 旧，再加上最新 heartbeat `is_backfilled=true`，只能安全表述为“最近正在接收补传事实”；它不能表述为“Agent 仍有 N 条 backlog”。

### 3. 失败或断连

- **明确失败**：Agent 最近产生 `action=retry` 或 terminal process error，且该轮没有满足上面的 ack+durable-delete 成功条件。
- **断连/进程未运行/主机不可达**：Center 的 `last_sync_at` 变旧，同时 Agent 端没有当前运行或尝试证据。
- Center 单独看 stale `last_sync_at` 无法区分网络失败、凭据终止错误、Agent 进程停止或主机离线；要想在 Center 页面细分，必须新增 Agent 上报的 attempt/error state，而不只是派生 heartbeat provenance。

“catching_up”与“本轮中途又失败”也可能同时发生：如果本轮先 ack 一部分、随后遇到 retryable error，应以 `retrying` 作为当前状态，同时保留已完成的 aggregate progress；不要把一次部分成功粉饰为纯健康状态。

## 方案与权衡

### A. Agent 本地结构化 replay 日志（MVP 推荐）

在 bounded flush 的本地结果上产生日志，不更改 durable entry 格式、Agent API contract、Center schema/read model 或 Web。

建议的低基数字段：

```text
msg="sync queue replay progress" state=catching_up acked_entries=<n> remaining_entries=<n>
msg="sync queue replay progress" state=caught_up acked_entries=<n> remaining_entries=0
msg="sync queue flush failed" state=retrying kind=<remote|transport> action=retry status=<optional> code=<optional> remaining_entries=<n>
```

约束：

- `catching_up/caught_up` 只能在相应 ack 后的 durable delete 成功后记录。
- 进入 replay、退出 replay 必须记录；持续 replay 最多每 60 秒一次，并且每轮最多一条 aggregate progress，避免 5 秒 tick 或每-entry 热路径日志。
- 正常无 backlog 的每个 tick 不记录成功日志。
- `remaining_entries` 只统计当前 authority 的旧 durable entries；新建的 current tick entry 不应把正常同步伪装成 backlog。
- 如果本轮遇到 retry/terminal，当前 state 是失败态；可以同时报告本轮已 durable 删除的 aggregate `acked_entries`，但不得只写健康 progress。

优点：能够以最小代码/测试面给出“成功追赶”的正证据，并与既有清洗后的 failure 日志形成对照；不产生协议兼容、迁移、ACL 或页面文案风险。缺点：需要主机 journal 权限，Center 用户无法在单一页面看到状态。

### B. Center 基于最新 heartbeat provenance 的只读派生状态（仅在要求页面可见时）

不改 Agent 协议或数据库表，使用已有 heartbeat facts 派生一个 read-model 字段，例如：

```text
sync_receive_state = live | receiving_backfill | stale | unknown
```

其中 `receiving_backfill` 只能表示“Center 最近收到 `is_backfilled=true` 的 heartbeat”，不能宣称本地 backlog 仍存在；`stale` 也不能细分断连和失败。API/UI 只需显示枚举与既有 `last_sync_at`，不要公开 fingerprint、batch ID 或原始请求。

优点：中心化、无需登录 Agent；复用已有事实，无 migration。缺点：MonitoringInstance list/get 查询需要稳定地取 latest heartbeat，需考虑索引/分页查询成本；它仍不能报告剩余量或失败原因，因此不能替代 Agent 日志。

### C. Agent 上报 queue telemetry（本次不推荐）

上报 `remaining_entries/replay_state/last_attempt` 可让 Center 精确显示 Agent 自报状态，但会触及 Agent contract、持久队列发送语义、Center ingest/schema/read model、ACL、Web types/UI 和兼容测试。尤其不能把 telemetry 原样固化进旧 queued request，否则回放旧 request 时会再次发送过期的“当时队列深度”，语义反而错误。该方案只有在中心化 fleet 运维成为明确产品需求时才合理。

## 隐私与安全边界

允许记录/公开的最小集合：固定枚举 `state`、aggregate `acked_entries/remaining_entries`、allowlisted `kind/action/status/code`、已有 freshness timestamp。

严禁进入日志、API 或 UI：

- sync token、enrollment token、Authorization/header/cookie；
- fingerprint、monitoring instance ID、durable entry ID、sync batch ID；
- 完整或局部 `SyncRequest`、heartbeat/host/probe/IP-quality/command payload；
- remote response body/message、raw local cause、SQL/DSN；
- server URL 的 userinfo/path/query/fragment（既有日志只允许 origin）。

即使 aggregate queue count 不含直接标识，也可能透露停机时长/采样量；MVP 应只保留本机 journal，不进入公开 API。若未来上 Center，沿用 MonitoringInstance read ACL，且只暴露枚举与时间，不暴露精确 queue count。

现有隐私测试已经提供可复用模式：retry 日志断言稳定字段存在且 token/fingerprint/remote message 不存在；stale/poison 日志还断言 entry ID、监控实例 ID 与注入换行不泄漏：[agent/runtime/runtime_test.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime_test.go:1594)、[agent/runtime/runtime_test.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime_test.go:1757)、[agent/runtime/runtime_test.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime_test.go:1950)。

## 可执行 TDD 最小推荐

将 observability 测试与 scheduler/fairness RED 放在同一个 `agent/runtime` 行为边界，不先造新公共 contract：

1. `TestRuntimeLogsBoundedReplayProgressAfterDurableAck`
   - seed 多个当前 authority 的旧 entries；一个 bounded pass 成功 ack/delete 一部分并保留一部分；
   - 断言恰好一条 `state=catching_up`，`acked_entries>0` 且 `remaining_entries>0`；
   - 断言该日志发生在 delete 成功之后。
2. `TestRuntimeLogsReplayCaughtUpOnceAndKeepsLiveTicksQuiet`
   - 最后一批旧 entries durable delete 且 current heartbeat 被接受后，断言一次 `state=caught_up remaining_entries=0`；
   - 后续正常 ticks 不重复成功日志。
3. `TestRuntimeThrottlesReplayProgressLogs`
   - 使用可控 clock，连续多个 5 秒 pass 仍在前进；
   - 断言 entry/exit 必记，持续 progress 在 60 秒窗口最多一条，且从不 per-entry。
4. `TestRuntimeReplayRetryIsFailureStateNotHealthyProgress`
   - 同一轮可先 ack 一部分再返回 retryable error；
   - 断言 current state 为 `retrying`，保留 `kind/action/status/code`，不把该轮只标成 `catching_up`。
5. `TestRuntimeReplayLogsDoNotExposeQueueOrCredentialMaterial`
   - 用 token、fingerprint、monitoring ID、entry ID、batch ID、remote message、含换行注入的 sentinel；
   - 对完整 log buffer 逐项断言不存在，只允许固定枚举与 aggregate counts。
6. 保留并改写现有 durable/backfilled/oldest-first 测试作为回归基线：失败 request 重试时标 backfilled，成功 ack 后队列清空，旧 facts 保持 oldest-first；不要让可观测性改变 durability/idempotency 语义：[agent/runtime/runtime_test.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime_test.go:1099)、[agent/runtime/runtime_test.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime_test.go:1902)、[agent/runtime/runtime_test.go](/home/murray/.codex/worktrees/2313/houfeng/agent/runtime/runtime_test.go:1986)。

若选择方案 B，再追加：repository 派生状态测试、list/get handler JSON 合同测试、Web type/列表或详情 badge 测试；必须用文案“正在接收补传”，不能写“仍有 backlog”。

## 唯一可能需要的用户选择

只有一个会实质改变工作量与层级边界的问题：**本次验收是否要求在 Center 页面直接可见，还是 Agent systemd journal 加现有 Center freshness API 足够？**

- 若 journal + API 足够：采用方案 A，保持本次修复聚焦 Agent scheduler/runtime，最小且可测试。
- 若必须页面可见：采用 A + B；仍不采用 C，也不展示精确 queue count。

在没有更强 UI 要求时，本研究建议默认第一项。
