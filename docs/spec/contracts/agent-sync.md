# Agent 安装、同步与兼容合同

## 接入会话、最小在线证据与采集权限

本协议要求匹配版本 Center/全部 Agent 协同升级，不支持旧 wire 协议混跑；既有会话凭据保留，升级前必须停写并排空旧队列，详见下方协调升级合同。

- Enrollment 成功返回 `session_id`、`capability: full` 及 `sync_token`。令牌使用
  `session_id.secret` 格式；Agent 持久化现有凭据文件并从令牌中恢复会话身份。
  数据库仅保存 HMAC 哈希。接入会话永久属于同一 MonitoringInstance，实例永久属于 VPS。
- 每次成功重新接入新建 `monitoring_agent_sessions` 阶段；保留原阶段指纹摘要、起止时间和
  最后可信在线时间。所有旧阶段变为 `evidence_only`，不得恢复其采集或命令权限。
- 安装器普通升级保留现有会话凭据；通过详情接入入口显式生成的所有安装命令附带 `--reenroll`，
  包括同一 VPS 退役旧实例后新建实例。不能从新实例没有历史会话推断目标主机没有旧凭据。
  安装器先验证签名发行物，再停止旧进程，以 0600 权限原子替换 bootstrap token，启动后签发新会话。
- Sync 必须携带 `session_id` 与持久 producer 身份。当前请求在实际发送前消费已持久预留的
  `live_signal.sequence`，并携带 `live_signal.fingerprint`；没有随机 `id` 字段或旧字段 alias。
  实时信号不含 Agent 时间，不写离线队列，不附加到历史回填。
  仅 sequence 高于该 session 的持久 `live_sequence_high` 才更新 Center 接收时间，使用事务内
  PostgreSQL `clock_timestamp()` 与归档检查保持同一时钟域，不能采用 Agent 或 Center 进程墙钟。
  旧/乱序信号、原始心跳回填及 Agent 自报时间均不能刷新归档安全计时；独立合法 batch 仍可接收。
- `monitoring_agent_sessions.last_trusted_online_at/ever_connected` 与实例同名字段保存长期事实，
  不依赖原始心跳保留任务。身份验证后首个有效实时心跳即进入“已接入”，无需性能样本。
- 启用正常采集；维护继续采集并带维护上下文。暂停、退役、VPS 已归档及旧会话仍接收
  最小在线证据，但返回 `stop_collection: true` 和空计划，性能及探测不进入正常观测链路。
  Agent 清空待发送采集、命令结果，取消运行中的 IP 质量任务，继续发送最小心跳。
- `evidence_only` 按自身会话指纹验证，不使用新阶段指纹拒绝仍在运行的旧 Agent。
  所属 VPS 已归档时保持归档，只更新去重的 `archived_online` 待核对事项。
- 实时信号事务先取资产关系共享锁，再锁实例与会话；归档取同一资产关系独占锁。
  持久化故障在回滚后标记接收链路故障，重启连续健康观察窗口。
- 原始事实批次通过有界 `agent_batch_receipts` 与单行 `agent_replay_states` 防重放；
  live 高水位独立于 batch 窗口，不产生逐 signal 永久行。
  最小在线/控制请求 `batch_sequence=0` 不构成批次 ack，不执行常规事件派生；
  `exact_duplicate`、`suppressed`、`deferred` 同样不执行常规 postSync 派生。

验收覆盖旧会话降权、暂停继续在线、接收时间权威、重复实时信号不刷新、回填不刷新、
无性能样本首次接入、归档后在线去重和采集队列清理。对应测试在
`store/agent_live_signals_test.go`、`agent/runtime/live_signal_test.go` 和
`agent/syncqueue/live_signal_test.go`。

## MonitoringInstance enrollment token one-time consumption

### 1. Scope / Trigger

- Trigger: 修改 MonitoringInstance enrollment token issuance/validation、`monitoring_instances.enrollment_token_*` 字段、`/api/agent/enroll`、或 MonitoringInstance onboarding install-command 生成路径。
- 目标：enrollment token 是一次性 bootstrap secret，不是长期 agent credential；成功绑定或进入待确认 fingerprint 路径后不得继续复用同一 token。

### 2. Signatures

- DB columns: `monitoring_instances.enrollment_token_hash`, `monitoring_instances.enrollment_token_issued_at`, `monitoring_instances.enrollment_token_consumed_at`。
- Domain constant: `monitoringinstances.EnrollmentTokenTTL = 30 * time.Minute`。
- Issue method: `IssueMonitoringInstanceEnrollmentToken(ctx, monitoringInstanceID) -> monitoringinstances.EnrollmentTokenIssue{Token, IssuedAt, ExpiresAt}`。
- Validation path: `/api/agent/enroll` consumes a matching active token before or during binding evaluation.

### 3. Contracts

- Token lookup must require non-empty hash, `enrollment_token_consumed_at is null`, and `enrollment_token_issued_at >= now() - monitoringinstances.EnrollmentTokenTTL`.
- Issuing a new token overwrites the previous hash/issued time and clears consumed state, so only the latest generated command is active.
- Successful token validation must mark `enrollment_token_consumed_at` in the same transaction as the enrollment/binding state change.
- A pending fingerprint conflict can consume the one-time token even before the operator confirms/rejects the conflict; UI copy must tell operators to regenerate when needed.
- Store only token hashes in Postgres; plaintext enrollment tokens appear only in the generated command and target host token file.

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Token older than 30 minutes | enrollment returns invalid enrollment token semantics |
| Token already consumed | enrollment returns invalid enrollment token semantics |
| Token regenerated | old token becomes invalid; new token can be used until consumed or expired |
| MonitoringInstance missing during issue | `monitoringinstances.ErrMonitoringInstanceNotFound` |
| DB failure during consume/bind | transaction rolls back; caller returns wrapped repository error |

### 5. Good/Base/Bad Cases

- Good: user generates command, runs it once within 30 minutes, agent binds and receives sync token; the bootstrap token is consumed.
- Base: user waits beyond 30 minutes; command fails at enroll and user regenerates from onboarding page.
- Bad: leaving multiple generated tokens valid lets shell history or chat leaks enroll the same MonitoringInstance later.
- Bad: consuming token after fingerprint confirmation instead of at enroll attempt makes conflict retries reuse a leaked bootstrap secret.

### 6. Tests Required

- Migration test: `enrollment_token_consumed_at` column exists and active-token index excludes consumed tokens.
- Store tests: issue sets `expires_at = issued_at + 30m`, regeneration invalidates prior token, expired token fails, consumed token fails, successful enroll consumes token.
- Enrollment service/handler tests: invalid/expired/consumed token maps to the existing invalid enrollment response and does not leak token details.
- Frontend onboarding tests: conflict-resolution copy does not claim the one-time token remains unchanged.

### 7. Wrong vs Correct

```sql
-- 错误：只按 hash 找 token，过期或已用 token 仍可登录。
where enrollment_token_hash = $1
```

```sql
-- 正确：只接受最新、未消费、TTL 内的 bootstrap token。
where enrollment_token_hash = $1
  and enrollment_token_consumed_at is null
  and enrollment_token_issued_at >= now() - interval '30 minutes'
```

## Agent sync v2：有界重放与 HTTP/client 边界

### 1. 能力屏障与请求格式

- 常量：`SyncProtocolRevision=2`、header `X-Houfeng-Sync-Protocol: 2`、
  `SyncReplayWindow=131072`、`LiveSequenceReservation=1024`。
- `GET /api/healthz` 保留 `name=houfeng-center`、`version`、`status=ok`，新增
  `agent_sync_protocol_revision: 2`、`agent_sync_replay_window: 131072`。
  `enroll.Client.CheckSyncProtocol(ctx) error` 读取真实 health endpoint，要求 HTTP 200、
  单个合法 JSON document、正确服务名/健康状态和两个能力值精确匹配。HTML 200、旧 Center、
  缺字段、错误类型/数值、尾随内容、超限 body、网络错误均失败。
  Runtime 必须先检查成功，再打开/改写队列或发送 sync；失败不得修改 buffer。
- `/api/agent/sync` 保留 POST、Bearer token 早检查、每进程 rate/inflight/body 资源上限。
  缺失、旧值、重复或非精确版本 header 在读取 body 前返回 HTTP 426、
  `code=sync_protocol_required`；JSON 中 `protocol_revision` 也必须为 2。
  client 每次发送 header 2，手工 wire DTO 显式传递全部 v2 控制字段；
  token 只走 Authorization，不进入 HTTP JSON。IP-quality capability 仍是独立 transient header。
- 请求新增 `protocol_revision`、`producer_id`（标准 UUID）、`batch_sequence`、
  `allocated_through`、`retired_through`；保留 session/instance 与事实载荷。
  序号为 signed int64，不允许 wrap；allocated/retired 非负且 `retired<=allocated`。
  有 batch 时 `0<batch_sequence<=allocated_through`，且高于有效退休 floor。
  `batch_sequence=0` 仅允许无 heartbeat/host/probe/IP-report/command-result 的控制或最小在线请求。
- 同批全部 heartbeat、host sample、probe observation、IP report 的 `sync_batch_id` 必须等于
  `producer_id + ":" + batch_sequence`（十进制），仅作事实关联身份，不再充当防重放权威。
  非空事实/命令/IP report 仍须 heartbeat carrier；缺少/不同 batch 身份或非法 bounds 拒绝。
  `live_signal.sequence` 必须为正 int64，fingerprint 非空；不接受旧 `id` alias。
  超出 replay window 不是 schema 错误，交由事务返回 `deferred`。

### 2. Center 自算 canonical digest

`agentapi.CanonicalSyncBatchDigest(req) ([32]byte,error)` 在解码后由 Center 计算，handler 将值
传给 `syncing.Batch.PayloadDigest`；不信任 client 提供的 digest，也不修改原 queue payload。

- 哈希内容包括 `monitoring_instance_id` authority、session、producer、batch sequence，
  以及全部事实、command results、IP reports。
- 排除 protocol revision、token、live signal、allocated/retired 控制字段、HTTP capability 和
  Center 接收时间；仅剔除事实顶层传输属性 `is_backfilled`，不剔除任意嵌套 raw JSON 同名字段。
- typed nil/empty collections 统一为空集合；optional `omitempty` 空值保持同义。
  Raw JSON 经 `UseNumber` 解码与 `encoding/json` 稳定 object key 排序规范化后哈希，
  保留大整数，不通过 float64 舍入；数组顺序有意义。
- live 请求改为 backfilled 重试时 digest 不变；修改事实或命令结果必须改变 digest。
  Agent 使用同一函数核验 ack；direct repository 测试也从同一原始 DTO 计算。

### 3. Ack 与错误合同

响应保留 `accepted_at`、`status`、`stop_collection` 与 plan，并新增 `session_id`、
`producer_id`、`batch_sequence`、`batch_digest`（SHA256 小写 hex）、`disposition`、
`retired_through`。既有 `status=accepted` 不是独立的队列删除凭证；必须解释 disposition。

| disposition | Center 行为 | Agent 行为 |
| --- | --- | --- |
| `recorded` | receipt 与全部事实/命令同事务首次提交 | 核验 session/producer/seq/digest 后 durable Delete，再应用 plan |
| `exact_duplicate` | 同 seq 同 digest，不重写事实/命令/通知，空 plan | 同样核验并 durable Delete |
| `suppressed` | 保留 stop_collection 抑制语义；最小在线 seq=0 不构成 batch ack | 仅匹配非零 batch 可按 ack 删除；执行 stop_collection 清理 |
| `deferred` | 窗口外仅提交合法 floor/live 证据，不写 facts/receipt、不执行命令；无 plan | 保留 entry，可重试，不应用命令计划或计入成功 ack |

错误必须保留 typed `RemoteError.StatusCode/Code`；426 和下列 409 均 terminal、保留队列，
不自动重新注册、不向服务端学习最大序号后重编号：

| HTTP | code | 含义 |
| --- | --- | --- |
| 426 | `sync_protocol_required` | header/body 或 direct service/repository revision 不支持 |
| 409 | `sync_producer_mismatch` | 同 session 已绑定其他 producer |
| 409 | `sync_batch_retired` | 非零 batch sequence 不高于有效持久退休 floor（包括请求自报 floor） |
| 409 | `sync_batch_identity_conflict` | 窗口内同 seq 的 canonical digest 不同 |
| 400 | `invalid_request` | UUID/序号 bounds/事实 carrier 等非法 |

### 4. 持久退休下界与有界存储

- 每 session 一个 `agent_replay_states` 行，永久绑定 producer，保存单调增长的
  retired/allocated/live 高水位；`agent_batch_receipts(session_id,batch_sequence,payload_digest)`
  只保留 `floor < seq <= allocated` 且 `seq-floor<=131072` 的收据。
  runtime 对 state 只有 SELECT/INSERT/UPDATE，对 receipt 只有 SELECT/INSERT/DELETE。
- 接收事务按 graph → instance/session → replay-state → receipt 锁顺序；producer、退休冲突
  拒绝时整笔回滚，不留下在线证据。有效 floor 为 max(server,request)，allocated 同样只增长。
  合法最小在线、暂停、归档、evidence_only 请求也可推进 floor。
- floor 推进由同事务 AFTER trigger 删除所有 `seq<=floor` 的 receipts；receipt guard
  用 `FOR SHARE` 与非键 floor UPDATE 互斥，禁止删除未退休 receipt、降低高水位或越窗 INSERT。
  相同 seq/digest 可 duplicate，不同 digest 是 409；退休后历史重放仍 409，不能靠删除 receipt 重放。
- live 仅推进独立持久 high；旧 signal 不刷新可信在线，也不阻断合法旧 batch。
  窗口锚定 retired floor，不锚定最高收到 seq，允许 FIFO backlog 与 new current 交错。
- 每 session receipt 逻辑最多 131072 行，正常 ack 推进 floor 后仅少量在途；state 一行/session。
  session 生命周期与业务历史仍增长；dead tuples/文件高水位依赖 autovacuum 重用，
  逻辑有界不等于立即缩小 relation 文件。

### 5. Agent 本地状态与协调升级（无旧队列自动转换）

- durable queue 使用 version 2 envelope：`version`、`session_id`、`producer_id`、
  `last_allocated_batch_seq`、`retired_batch_floor`、`live_reserved_through`、`entries`。
  producer 仅新 session 初始化时生成并持久化；同 session 重启复用全部计数。
  Enqueue 在一次原子写入中分配序号、统一所有事实 batch ID 并计算 floor。
  Delete/Prune/DeleteMany 同样持久推进 floor；实际发送读取最新 replay state 覆盖控制字段。
- 缺失文件或合法空旧数组 `[]` 可初始化。非空旧数组、空文件、截断/损坏 JSON 或新格式缺损
  明确失败且字节不变；非空旧队列必须恢复旧版本继续排空，绝不重编号/清空以绕过升级。
  既有 token 文件保留，不能把队列版本变化当作重新接入授权。
- `OpenSession` 持有独立 `<BufferFile>.lock` inode 的 process-lifetime 非阻塞排他 flock，
  `Close` 释放；不得锁会 rename 的 buffer inode。无 queue 的直发路径不受支持。
- live 每次先持久 reserve 1024 个值再发送，重启跳过旧 reserved 区间；不足完整 reservation
  时 overflow 失败，不回绕。batch 窗口与 live 计数互不影响。
- rename 前失败保持旧文件；rename 成功但目录 fsync 失败返回 `ErrAmbiguousDurability`，
  poison 当前 store 并立即终止 Runtime，不继续缓存 counter/发送/重复 enqueue。
  下一次仅通过新 OpenSession 读取验证可见 envelope 恢复，重新 reserve live 区间。
- 升级前必须枚举并停止全部旧 Agent（含旧/evidence_only 进程），读取各实际 buffer 确认合法空数组；
  任何离线/不可读/非空 Agent 都阻止切换。全部停写后协调备份 DB、附件、Records authority、
  Agent 凭据与 buffers；scoped migrator 升级 Center 后升级全部 Agent，保留身份及事实历史。
  C80 移除旧永久逐次表，不导入旧随机身份；迁移本身不能证明跨机已排空。
- C80 后禁止回退到不识别 v2 的 Center/Agent，禁止单边还原 DB 或 queue 快照。
  恢复必须另行授权、停止全部 writer、恢复同一协调点。检测到 producer/退休/identity 冲突
  保留队列失败，不自动“修复”。本地 flock 不防跨主机克隆，同 producer 窗口内原样重试合法，
  不宣称检测所有旧快照或数据库单边回滚。

### 6. Required acceptance

- HTTP/client：实际 Healthz、HTML/缺字段/网络失败；header pre-body 426；
  body revision、signed-int64/UUID/carrier bounds；自算 digest/domain 字段映射；
  四类 disposition、deferred 无 plan、三类 409 原样 typed error，以及旧 live ID 拒绝。
- `TestPostgresIntegrationAgentReplayWindow`：真实 runtime ACL + repository/HTTP，lost response、
  重启 duplicate、ack 持久失败、floor 回收后拒绝重放、live 乱序、窗口 131072/131073、
  producer/identity 冲突、suppressed 控制请求与 token/fingerprint/lifecycle 边界。
- `TestPostgresIntegrationAgentReplayMigration`：C79→C80 保留凭据/online/raw/business 历史，
  exact ACL、旧表移除、重复迁移与失败回滚；direct-runtime 双连接验证 guard/floor 并发安全。
- FileStore/Runtime：空旧数组升级、非空/非法文件字节不变、flock、live reserve 崩溃跳号、
  rename 前后故障/poison、int64 上界、严格 ack 核验与 durable deletion。


## Scenario: agent command / Docker 边界

1. **Scope / Trigger**
   - 触发：修改 `agent/exec/`、`agent/containersample/`、`agent/runtime` 的 pending action 执行或 Docker container facts 采样路径。
   - 目标：保持 Agent 是薄 observe / buffer / sync / apply-plan 进程，只允许白名单命令执行和 best-effort 本机 Docker facts，防止被误扩展成任意远程执行或容器编排面。

2. **Signatures**
   - 命令下发：center 只通过 `agentapi.PendingAction{ActionID, CommandID}` 下发稳定 `command_id`，不下发二进制路径、参数或 shell snippet。
   - 白名单解析：`agent/exec.Lookup(commandID)` 返回 `(bin, args, ok)`，`args` 必须是内部白名单参数的 defensive copy。
   - 命令执行：`agent/exec.Run(ctx, bin, args)` 使用 `exec.CommandContext`，带 30s timeout 与 stdout/stderr 独立 64KB 截断，并在返回结果前用 `internal/security/redact.Secrets` 脱敏 stdout/stderr。
   - 命令治理元数据：`internal/contracts/agentapi.KnownCommandDefinitions()` 是 center / web-facing command ID sensitivity 的后端权威源，当前 sensitivity 只有 `standard` 和 `sensitive`。
   - Docker facts：`agent/containersample.Collect(ctx)` 在 host sample 时 best-effort 调用 Docker CLI，返回 `[]agentapi.ContainerInfo` 或 `nil`。

3. **Contracts**
   - 白名单命令 ID 当前固定为：`df_h`、`free_m`、`uptime`、`top_head`、`journalctl_u`、`systemctl_status`、`dmesg_err`、`docker_ps`。
   - sensitivity tier 当前固定为：`standard` = `df_h`、`free_m`、`uptime`；`sensitive` = `top_head`、`journalctl_u`、`systemctl_status`、`dmesg_err`、`docker_ps`。
   - 新增、删除或重命名 command ID 时，必须同时更新 agent whitelist、`agentapi.KnownCommandDefinitions()`、center handler/store 测试、web command constants 和 API/page 测试；不得让 agent 可执行命令与 center 治理元数据漂移。
   - 命令参数全部编译进 agent，不接受中心、Web 或用户传入的动态参数。
   - sensitive command 的二次确认由 center handler 强制执行；前端标记和确认弹层只提供可用性，不是安全边界。
   - 命令 stdout/stderr 必须在 agent 上传前脱敏；center store 持久化 `last_action` 前必须再次脱敏，覆盖旧 agent 或第三方 agent。
   - center command audit 只保存 action/instance/command/sensitivity/event/source/actor/exit_code/occurred_at 等 metadata，不保存 stdout/stderr。
   - completed command output 是 24h 可见的当前状态字段；过期后 API 必须隐藏 stdout/stderr，retention 必须清理 persisted `last_action` 输出字段。
   - 脱敏至少覆盖 Authorization bearer、`token` / `access_token` / `refresh_token` / `api_key` / `secret` / `password` 的 key-value/JSON 形态，以及 PEM private key blocks。脱敏是 best-effort，不能替代 agent 最小权限和诊断命令分级。
   - 未知 `command_id` 由 agent runtime 静默忽略，不阻塞 sync loop，不生成 command result。
   - Docker CLI 不存在、daemon 不可用、`docker ps` 失败或 context 已取消时返回 `nil, nil`，不得让 host sample 失败。
   - `docker stats` 失败时仍返回 `docker ps` 的 container identity/status facts，CPU/mem 百分比留空。
   - Docker facts 只描述本机容器快照，不代表 Docker runtime 是必需部署依赖，不提供 start/stop/restart/logs/exec/compose/kubernetes 等控制能力。

4. **Validation & Error Matrix**
   - `Lookup` 未知 ID -> `ok=false`，bin/args 零值。
   - `agentapi.SensitivityForCommand` 未知 ID -> `("", false)`，且 `RequiresSensitiveConfirmation` 返回 false；handler 仍必须先拒绝未知 command ID。
   - sensitive command POST 缺少 `confirmed_sensitive:true` -> center 返回 400，不入队 action；真实且可执行实例的认证请求仍必须写一次 metadata-only `rejected` 审计，完整资格与事务规则见 [命令审计合同](monitoring.md)。
   - 调用方修改 `Lookup` 返回的 args -> 后续 `Lookup` 结果不变。
   - shell metacharacter 作为参数传给 `Run` -> 被当作普通参数，不执行额外 shell 语义。
   - stdout/stderr 含 `Authorization: Bearer abc` 或 `token=abc` -> agent result 和 center persisted `last_action` 都不含原始 secret。
   - command output 超过 `output_expires_at` -> MonitoringInstance read API 不再返回 stdout/stderr。
   - `docker ps` 输出空或无法解析 -> `nil, nil`。
   - `docker stats` 输出字段无法解析 -> 对应 CPU/mem 字段保持 nil。

5. **Good / Base / Bad Cases**
   - Good: center 下发 `command_id=uptime`，agent 通过 whitelist 解析为 `uptime` + nil args，执行后回传带 action/command identity 的 `CommandResult`。
   - Good: center 收到 `systemctl_status` queue request 时要求 `confirmed_sensitive:true`，queue / dispatch / completion audit 都记录 `sensitivity='sensitive'` 但不记录 stdout/stderr。
   - Good: Docker 可用时 host sample 附带 container name/image/status 和可选 CPU/mem 百分比；Docker 不可用时 host sample 仍正常上传且 `containers` 为空。
   - Good: 旧 agent 上传未脱敏 command result，center 持久化前再次 redacts stdout/stderr。
   - Base: 当前 whitelist 有 `docker_ps`，但这只是诊断命令，不等于 Docker 编排能力。
   - Bad: 让 center 或 Web 传入 `args:["-c","..."]`、`bin:"sh"`、`command:"docker rm ..."`，会把薄 Agent 扩成任意执行面。
   - Bad: 只改 agent whitelist 不改 `agentapi.KnownCommandDefinitions()`，导致 center 不能正确确认/审计新命令。
   - Bad: 在 `containersample` 中增加 `docker start/stop/restart/logs/exec` 或 Docker SDK 控制路径，违反 best-effort facts 边界。

6. **Tests Required**
   - `agent/exec/whitelist_test.go`：固定 whitelist command IDs、bin、args；未知 ID 拒绝；返回 args 是 defensive copy。
   - `internal/contracts/agentapi/commands_test.go`：固定 command ID set 与 sensitivity tier；未知 ID 无 sensitivity。
   - Handler/store tests：sensitive command confirmation、metadata-only audit、24h output TTL、expired output cleanup。
   - `agent/exec/runner_test.go`：正常/非零/timeout/not-found/output truncation；必须覆盖不隐式调用 shell 和 stdout/stderr secret redaction。
   - `internal/center/store/sync_batches_test.go` 或 command action tests：command result persistence redacts stdout/stderr before writing `last_action`。
   - `agent/containersample/sample_test.go`：Docker 不可用、`ps` 失败、`stats` 失败、状态归一化、固定 Docker CLI 参数形状。
   - `agent/runtime/runtime_test.go`：pending action 结果携带 action/command identity，未知 command ID 不产生结果，host sample 可附带 container facts。

7. **Wrong vs Correct**

```go
// 错误：把内部 whitelist args 直接暴露给调用方，调用方可篡改后续执行参数。
return cmd.Bin, cmd.Args, true
```

```go
// 正确：返回参数副本，白名单定义仍由编译期 map 独占。
args := append([]string(nil), cmd.Args...)
return cmd.Bin, args, true
```

```go
// 错误：把 command_id 当 shell 脚本执行，等价于开放任意远程命令。
cmd := exec.CommandContext(ctx, "sh", "-c", commandID)
```

```go
// 正确：只执行 whitelist 解析出的二进制和固定参数，不经过 shell。
bin, args, ok := agentexec.Lookup(action.CommandID)
if ok {
	result := agentexec.Run(ctx, bin, args)
}
```

## Scenario: `agent/hostsample` 平台采集边界

1. **Scope / Trigger**
   - 触发：修改主机采样实现，尤其是 Linux `/proc`、macOS local dev、文件系统统计、rate-based 指标。
   - 目标：`agent/runtime` 只依赖 `hostsample.Provider.Collect`，平台差异必须收敛在 `agent/hostsample/` 内。

2. **Signatures**
   - 生产入口：`hostsample.New() *Provider`，按 `runtime.GOOS` 选择采集路径。
   - 测试入口：`hostsample.NewWithDeps(readFile, statFS) *Provider`，固定走 Linux/procfs collector，便于注入 `/proc/*` fixture。
   - `Provider.Collect(observedAt time.Time) (agentapi.HostSamplePayload, error)` 是唯一对外采样方法。

3. **Contracts**
   - Linux: 读取 `/proc/loadavg`、`/proc/meminfo`、`/proc/uptime`、`/proc/stat`、`/proc/net/dev`、`/proc/diskstats`，并用 `statfs("/")` 计算磁盘/inode。
   - Darwin: 不读取 `/proc/*`；用 `sysctl -n vm.loadavg`、`sysctl -n hw.memsize`、`sysctl -n vm.swapusage`、`sysctl -n kern.boottime` 和 `vm_stat` 生成本地开发可用的 host sample。
   - 不新增 agent env key。CPU usage/iowait/steal 共用 `cpu_rates_valid`；新 Agent 每次明确发送 true/false，原百分比 JSON 键不变。

4. **Validation & Error Matrix**
   - 除 `/proc/stat` 外的必需来源读取失败 -> `Collect` 返回带上下文的 wrapped error，例如 `darwin sysctl vm.loadavg: %w` 或 `read /proc/loadavg: %w`。CPU 文件缺失或解析失败只令 CPU 组不可用，保留同条样本的其他观测。
   - Darwin `vm.swapusage` 读取失败 -> `swap_used_pct=0`，不得阻塞整条 host sample，因为部分 macOS 环境可能禁用 swap。
   - Darwin 没有真实 CPU 采集，`cpu_rates_valid=false`，CPU 零值仅为占位，不是观测；其他 rate-based 字段保持既有行为。

5. **Good / Base / Bad Cases**
   - Good: macOS 本地 agent 能完成 `host_samples` 上报，Linux systemd agent 仍走完整 procfs 指标。
   - Base: 首个 sample 的 CPU 为不可用；后续 Linux sample 根据 previous snapshot 推导 rate。CPU 缺失后的首个有效快照仅重建基线，再下一正常区间恢复。
   - Bad: 在 Darwin 分支 fallback 读取 `/proc/loadavg`，会让 macOS smoke 重新出现 `no such file or directory`。

6. **Tests Required**
   - Linux/procfs fixture tests: `agent/hostsample/provider_test.go` 覆盖 load/mem/uptime/rate/diskstats。
   - Darwin regression test: `agent/hostsample/provider_darwin_test.go` 必须断言不读取 `/proc/*`，并检查 load、mem、swap、disk、uptime 的核心字段。
   - Runtime safety: `go test ./agent/runtime` 必须继续通过，确保 host sample 仍被 `buildSyncRequest` 串入 sync batch。

7. **Wrong vs Correct**
   - Wrong: 在 `agent/runtime` 里按 OS 分支，或让 runtime 感知 `sysctl` / `/proc` 文件名。
   - Correct: 在 `hostsample.New()` / `Provider.Collect` 内选择平台 collector，runtime 只消费 `HostSamplePayload`。


### CPU 速率与升级边界

- Linux 仅累加 user、nice、system、idle、iowait、irq、softirq、steal 八个基础计数；guest/guest_nice 已包含在 user/nice，额外字段只验证整数格式，不重复累计或扣除。至少七项，缺 steal 按零。
- 首次、CPU 来源缺失/非法、基础字段数量变化、零/未递增时间、任一基础计数回退、累计加和溢出或总 delta 为零，整组 CPU 标记 false。有效区间 usage 是除 idle/iowait 外的 delta 比例，iowait/steal 使用同一八项 delta 分母；usage 包含 steal。
- Center 的共同判定为 `(marker == nil || marker) && usage/iowait/steal 全部有限且在 [0,100]`。nil/SQL NULL 仅是 legacy 未声明，不代表已验证；false 占位零不能当成观测。有限越界 CPU 只规范化 marker=false，不拒绝整批 sync；JSON 非有限数的原入口拒绝规则不变。
- v2 queue 持久化和重放必须保留 CPU marker=false，不能删除 marker 或重发伪零；
  历史 DB 中缺失 marker 的事实仍按上述 legacy 值语义读取。
- 本节不提供混合 wire 版本支持；所有 Agent/Center 必须遵循上方 v2 能力检查、旧队列排空及协调升级屏障。

## Scenario: Agent one-command install contract

1. **Scope / Trigger**
   - 触发：修改 MonitoringInstance onboarding、一键安装命令、center-served installer、`HOUFENG_PUBLIC_BASE_URL`、agent release artifact 命名、或 `/api/agent/install.sh` 路由。
   - 目标：自部署center负责安装命令/token；GitHub Release提供二进制、对应源码与signed checksum manifest，不作token/script authority，不能只靠同源checksum提供信任。

2. **Signatures**
   - Config: `config.CenterConfig.PublicBaseURL` 来自 `HOUFENG_PUBLIC_BASE_URL`，必须是无 query/fragment 的 absolute `http(s)` URL，可为 domain 或 `IP:port`。
   - Public route: `GET agentapi.InstallScriptPath` -> embedded shell script，未登录可读，只允许读取脚本。
   - Installer-pinned checksum public key: `HOUFENG_CHECKSUM_MINISIGN_PUBLIC_KEY` inside `internal/center/installer/houfeng-agent-install.sh`。
   - Release assets: `houfeng-agent_<version>_linux_amd64`、`houfeng-agent_<version>_linux_arm64`、`houfeng-agent_<version>_source.tar.gz`、`houfeng-agent_<version>_licenses.txt`、`sha256sums.txt`、`sha256sums.txt.minisig`。
   - Release workflow secrets: `HOUFENG_RELEASE_MINISIGN_PRIVATE_KEY` and optional `HOUFENG_RELEASE_MINISIGN_PASSWORD`。
   - Authenticated route: `POST /api/monitoring-instances/{monitoring_instance_id}/install-command` -> `monitoringinstances.InstallCommandIssue`。
   - Response JSON: `{command, issued_at, expires_at, installer_url, public_base_url, agent_version, release_repo}`。
   - Installer token inputs:
     - generated commands use `--enrollment-token-stdin`
     - manual fallback may use exactly one of `--enrollment-token TOKEN`、`--enrollment-token-file PATH`、`--enrollment-token-stdin`
   - Generated command:

     ```sh
     tmp_installer="$(mktemp)" && curl -fsSL '<public_base_url>/api/agent/install.sh' -o "$tmp_installer" && sudo sh "$tmp_installer" --server-url '<public_base_url>' --enrollment-token-stdin --install-missing-deps --version '<agent_version>' --release-repo '<owner/repo>' <<'HOUFENG_ENROLLMENT_TOKEN'
     <token>
     HOUFENG_ENROLLMENT_TOKEN
     status=$?; rm -f "$tmp_installer"; test "$status" -eq 0
     ```

3. **Contracts**
   - Production install commands must use `HOUFENG_PUBLIC_BASE_URL` as the authoritative externally reachable center URL; do not derive production commands from browser origin, request host, `Referer`, or SPA location.
   - `POST /api/monitoring-instances/{monitoring_instance_id}/install-command` issues a fresh short-lived one-time enrollment token for that MonitoringInstance; regeneration invalidates the previous active token.
   - `agent_version` must be a real release version, not empty and not `dev`; the installer downloads `houfeng-agent_<version>_linux_<amd64|arm64>` from the configured release repo.
   - Installer server URLs default to HTTPS-only. `http://` is accepted only when the operator passes `--insecure-allow-http`, which the center includes for explicitly configured HTTP `HOUFENG_PUBLIC_BASE_URL` values.
   - Generated commands must not pass the one-time enrollment token as installer argv or as another command's argv. Use a quoted heredoc into installer stdin so token exposure is limited to the copied command text rather than `ps` output for the installer process.
   - Manual installer invocations must provide exactly one enrollment token source; empty token, multiple sources, or unreadable `--enrollment-token-file` values fail before writes.
   - Detector preflight requires working Bash>=4 and jq in `/usr/bin` or `/bin` before downloads or existing Agent/systemd changes; only shell builtins and the private Agent fetch helper are otherwise needed. Missing/broken tools fail with manual repair instructions, even with `--install-missing-deps`; no detector dependencies are installed. Runtime loss is `detector_dependency_missing` for four script services/keyless geo, not monitoring exit or fallback to replaced algorithms.
   - Generated commands include `--install-missing-deps` so Debian 11 / older Ubuntu style hosts without a packaged `minisign` can recover without separate operator diagnosis. Manual installer invocations may omit the flag for an interactive `/dev/tty` prompt, or pass `--no-install-missing-deps` to fail closed when `minisign` is missing.
   - If `minisign` is missing and dependency recovery is allowed, the installer must download the pinned upstream static minisign tarball, verify its embedded SHA256 before extracting, install only the matching `amd64`/`arm64` verifier to `/usr/local/bin/minisign`, ensure the current script `PATH` can find it, then continue. Do not use apt/yum/dnf/apk or enable distro repositories in the installer path; package availability differs by distribution and version.
   - Any installer prompt must read from `/dev/tty`, never stdin, because generated commands reserve stdin for `--enrollment-token-stdin`.
   - The installer must require a working `minisign` before release verification, download `sha256sums.txt.minisig`, verify `sha256sums.txt` with the pinned public key, then verify the downloaded binary against the signed manifest before replacing `/usr/local/bin/houfeng-agent` or starting systemd. Missing signature, signature failure, missing checksum entry, checksum mismatch, denied dependency recovery, failed minisign bootstrap checksum, or failed bootstrap install must fail closed without checksum-only fallback.
   - Release workflow must sign `dist/sha256sums.txt` before uploading release assets. `HOUFENG_RELEASE_MINISIGN_PRIVATE_KEY` must match the installer-pinned public key; encrypted keys require `HOUFENG_RELEASE_MINISIGN_PASSWORD`.
   - Source archive contains complete tracked release HEAD (project AGPL-3.0-only license, third-party notices, detector source/assets/license/upstream original/modifications/SHA256SUMS, Go source/modules and build workflow/Makefile), enters the same signed checksum/upload mechanism and is verified before publication. Clean binary VCS metadata and archive material/hash checks remain required. Dirty local builds do not prove correspondence; Houfeng's original code is explicitly owner-licensed under AGPL-3.0-only, not exempted through subprocess execution. Third-party licenses remain applicable and corresponding source must include the actual distributed modifications.
   - Before release builds or output creation, reject any tracked/index difference from HEAD and any non-ignored untracked file. Ignored scratch is allowed and untouched. Dirty development candidates use build-agent, never a silently mismatched binary/source release pair.
   - Installed `agent.env` must include durable sync queue bounds: `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`、`HOUFENG_AGENT_BUFFER_MAX_AGE`、`HOUFENG_AGENT_BUFFER_MAX_BYTES`.
   - MVP support is Linux + systemd + `amd64`/`arm64` only. Auto-upgrade, uninstall UX, non-systemd hosts, package repos, Docker/Kubernetes installs, and center-hosted binary mirrors are out of scope.
   - Installer output, center logs, and UI conflict copy must not print the full enrollment token or imply a one-time token remains reusable after a failed/pending fingerprint attempt.

4. **Validation & Error Matrix**

   | Condition | Expected behavior |
   | --- | --- |
   | Missing `HOUFENG_PUBLIC_BASE_URL` | install-command returns 409 `public base URL is not configured` |
   | Invalid public URL scheme/query/fragment | center config load fails before serving traffic |
   | Missing or `dev` agent version | install-command returns 409 `agent release version is not configured` |
   | Unknown monitoring instance | install-command returns 404 `monitoring instance not found` |
   | HTTP `--server-url` without `--insecure-allow-http` | installer exits before writing runtime files |
   | Multiple token sources or empty token | installer exits before writing runtime files |
   | Unsupported install method | installer returns non-zero with a short error; no partial service start |
   | Unsupported OS / architecture / no running systemd | installer exits before writing binary/config/token |
   | Bash missing/older than4 or jq missing/broken | manual repair error before release downloads, stopping service or Agent file changes; --install-missing-deps does not authorize installing these tools |
   | `minisign` missing + generated command `--install-missing-deps` | installer verifies pinned minisign tarball SHA256, installs `/usr/local/bin/minisign`, then verifies release manifest |
   | `minisign` missing + manual interactive run without dependency flag | installer explains the consequence and asks via `/dev/tty`; no answer or `no` exits before release download and local writes |
   | `minisign` missing + `--no-install-missing-deps` or non-interactive run without consent | installer exits before release download, replacing binary, or starting service |
   | minisign bootstrap SHA256 mismatch or tarball missing expected arch binary | installer exits before installing verifier or touching agent files |
   | `sha256sums.txt.minisig` missing | installer exits before replacing binary or starting service |
   | checksum manifest signature invalid | installer exits before reading checksum entries or replacing binary |
   | Missing checksum entry or checksum mismatch | installer exits before replacing binary or starting service |
   | release workflow missing signing private key | publish workflow fails before asset upload |

5. **Good / Base / Bad Cases**
   - Good: logged-in operator opens MonitoringInstance onboarding, generates a command from center, copies it to a Linux systemd amd64/arm64 host, checksum signature and checksum verification pass, installer writes config/token with restrictive permissions, enables and starts `houfeng-agent`.
   - Base: the public script route is unauthenticated but contains no deployment-specific secret until command generation feeds a one-time token to installer stdin at execution time.
   - Bad: SPA constructs `curl ${window.location.origin}/api/agent/install.sh ...` and ships a command that works only behind the browser's current origin.
   - Bad: generated command uses `--enrollment-token '<token>'`, which exposes the token as installer argv.
   - Bad: putting the installer script only in GitHub Release/raw means all self-hosted deployments share script authority and cannot couple script behavior to their center token contract.
   - Bad: accepting `sha256sums.txt` without verifying `sha256sums.txt.minisig` lets an attacker who can replace release assets replace both binary and checksum.
   - Bad: installing or restarting the service before signature and checksum verification makes a corrupted or substituted binary executable.

6. **Tests Required**
   - Config tests for valid domain/IP public URLs, trim/trailing slash behavior, rejected scheme, relative URL, query, and fragment.
   - Handler tests for install-command success, 404 monitoring instance, 409 missing public URL, 409 dev/missing version, method not allowed, HTTP base URL `--insecure-allow-http`, no argv token exposure, and shell quoting of all command arguments.
   - Router/bootstrap tests proving `/api/agent/install.sh` is public while `/api/monitoring-instances/{id}/install-command` remains session-protected and wired non-nil.
   - Installer tests or embedded-script checks for Linux arch mapping, systemd requirement, missing-`minisign` dependency recovery flags, `/dev/tty` prompt usage, pinned bootstrap SHA256, recovery before release asset download, signed checksum manifest download, signature verification before checksum extraction, exact checksum-manifest matching, HTTPS-by-default behavior, token file/stdin sources, token file permissions, and no full-token logging.
   - Release target emits both Linux binaries, corresponding-source archive, project/third-party license bundle and sha256sums.txt with matching names; workflow verifies required source/license/build materials and detector hashes, includes all four checksums, then signs and uploads the manifest/signature. Images wait for successful source/license publication. Houfeng-owned code is AGPL-3.0-only by explicit owner choice; third-party notices and actual-version corresponding-source obligations remain applicable.

7. **Wrong vs Correct**

```tsx
// 错误：前端从浏览器 origin 拼生产安装命令。
const command = `curl -fsSL ${window.location.origin}/api/agent/install.sh | sudo sh -s -- ...`
```

```tsx
// 正确：前端只展示 center 生成的命令。
const issue = await issueMonitoringInstanceInstallCommand(node.monitoring_instance_id)
setInstallCommand(issue.command)
```

```go
// 错误：request host / browser origin 成为部署 URL authority。
installerURL := "https://" + r.Host + agentapi.InstallScriptPath
```

```go
// 正确：显式配置是唯一 production URL authority。
installerURL := publicBaseURL + agentapi.InstallScriptPath
```

## MonitoringInstance onboarding install-command endpoint

`POST /api/monitoring-instances/{monitoring_instance_id}/install-command` 是登录用户触发的普通 center API，不是 agent contract endpoint。它仍走 `writeError`，但有两个配置类 409 是产品契约，不能降级成 500：

| Condition | HTTP status | Message |
| --- | --- | --- |
| `HOUFENG_PUBLIC_BASE_URL` 未配置 | 409 | `public base URL is not configured` |
| agent release version 为空或 `dev` | 409 | `agent release version is not configured` |
| MonitoringInstance 不存在 | 404 | `monitoring instance not found` |
| 其他 repository error | 500 | `internal server error` |

前端必须展示这些短文案，引导 operator 回到部署配置或发布流程；不要在浏览器侧用 `window.location.origin` 兜底绕过 409。

## agent 端点（contract 层）

`internal/center/http/handlers/agent.go` 的 `writeAgentAPIError` 是 agent 专用错误响应，**额外带 `agentapi.ErrorCode*`**：

```go
func writeAgentAPIError(w http.ResponseWriter, status int, code, message string) {
    writeJSON(w, status, agentapi.ErrorResponse{Code: code, Message: message})
}
```

agent handler 的判定与映射全在 `agent.go`，必须使用 `agentapi.ErrorCode*` 常量，不要造新字符串：

| 领域 error | HTTP status | `agentapi.ErrorCode*` |
|-----------|-------------|-----------------------|
| `enrollment.ErrInvalidEnrollmentToken` | 401 | `ErrorCodeInvalidEnrollmentToken` |
| `syncing.ErrInvalidSyncToken` | 401 | `ErrorCodeInvalidSyncToken` |
| `syncing.ErrBindingNotAccepted` | 409 | `ErrorCodeBindingNotAccepted` |
| `syncing.ErrSyncProtocolRequired` / header/body 版本不匹配 | 426 | `ErrorCodeSyncProtocolRequired` |
| `syncing.ErrSyncProducerMismatch` | 409 | `ErrorCodeSyncProducerMismatch` |
| `syncing.ErrSyncBatchRetired` | 409 | `ErrorCodeSyncBatchRetired` |
| `syncing.ErrSyncBatchIdentityConflict` | 409 | `ErrorCodeSyncBatchIdentityConflict` |
| `syncing.ErrInvalidReplayState` / `syncing.ErrHeartbeatRequired` | 400 | `ErrorCodeInvalidRequest` |
| `monitoringinstances.ErrMonitoringInstanceNotFound` | 404 | `ErrorCodeMonitoringInstanceNotFound` |
| `observations.ErrInvalidProbeObservation` | 400 | `ErrorCodeInvalidRequest` |
| 任意其他 | 500 | `ErrorCodeInternalError` |
| `decodeJSON` 失败 | 400 | `ErrorCodeInvalidJSON` |
| 业务校验失败 (`isValidSyncRequest` 等) | 400 | `ErrorCodeInvalidRequest` |
| 方法不允许 | 405 | `ErrorCodeMethodNotAllowed` |

---

## agent 侧的错误码消费

agent 必须把 center 返回的非 2xx 解码为 `*enroll.RemoteError`（`agent/enroll/client.go:21-36`）：

```go
type RemoteError struct {
    StatusCode int
    Code       string  // 对应 agentapi.ErrorCode*
    Message    string
    RetryAfter time.Duration // 仅 429/503 消费，范围 1～60 秒
}
```

## Scenario: agent durable sync queue 的远端失败策略

### 1. Scope / Trigger

- Trigger：修改 `agent/runtime` 的 queue flush、`agent/syncqueue` authority/backfill、`agent/enroll.RemoteError` 消费或 agent sync 错误日志。
- 目标：永久拒绝或脏队列项不能阻塞后续当前心跳，同时不能把临时故障误删，也不能把持久队列内的凭据带入日志。

### 2. Signatures

- typed remote cause：`*enroll.RemoteError{StatusCode, Code, Message, RetryAfter}`。
- 当前 queue authority：runtime 已加载的 session/producer、`monitoring_instance_id`、`sync_token` 与本机 `fingerprint`；不能将显式旧 authority 的事实重编号为新 session。
- 本地 policy result：`kind=remote|transport`、`action=discard|terminal|retry`、可选 allowlisted `status` / `agentapi.ErrorCode*`。
- stale backlog durability primitive：`SyncQueue.DeleteMany(context.Context, []string) error`；生产 `FileStore` 必须在一次加锁、一次原子文件替换中删除整批 stale ID。
- `FileStore` 返回给 runtime 的本地 entry ID 必须唯一，保证单条 ack 不会删除尚未发送的同 ID fact。
  本地 `Entry.ID` 不是 replay 权威；不得把本地 suffix 当作新 batch 身份。
  Center 按 session/producer/seq/digest 判定重试，corrupt/legacy 文件按 v2 启动合同 fail closed。
- wrapped policy / remote operation / pre-sync local operation / local queue error 必须提供 `Unwrap() error`，使调用方仍可通过 `errors.As` / `errors.Is` 取得 typed cause；其 `Error()` 只格式化稳定、脱敏字段。
- 每轮调度上界：`maxBacklogSyncAttemptsPerRound = 2`；`Enqueue` 返回的本地 `Entry.ID` 是本轮 current carrier 的唯一身份。

### 3. Contracts

- 每个 tick 先 durable enqueue 当前请求，再以 exact 本地 `Entry.ID` 分离 backlog lane 与 current lane。backlog lane 仍按 `CreatedAt, ID` FIFO，但每轮最多尝试两个旧 entry；current entry 始终在本轮最后尝试，因此 fresh 不会被大积压无限饿死。该合同只放宽全局 FIFO，不放宽 backlog lane 内 FIFO。
- 只有旧 entry 发送时才把其中事实标记 `is_backfilled=true`；不得用可碰撞的 carrier `SyncBatchID` 判断 current/backfill。每轮最多 `K+1=3` 次同步尝试。
- 同步与注册的单进程预算分离：同步每 IP/全局均为 2400 次/一分钟滑动窗口；注册保持每 IP 120、全局 1000。30 台每 5 秒一次正常上报为 360 次/分，每轮两个补传加 current 为 1080 次/分。该预算不是硬件吞吐保证，也不是跨进程共享限流。`AgentRateLimitOptions` 每个零值/负值字段独立采用对应正默认值；只设置某个字段不能关闭其他限额。IP key 缓存仍上限 10000，同步并发仍上限 32，body 仍上限 4 MiB；token 语法在读取 body 之前检查，不新增认证数据库查询或限流环境变量。
- 未受信任的 socket peer 提供的 XFF 一律忽略。空 `HOUFENG_TRUSTED_PROXIES` 会把代理/NAT 后请求计入同一 peer IP 预算；按实际 socket peer 配置精确可信代理 CIDR，不自动信任整个 Docker 网段或 `0.0.0.0/0`、`::/0`。真实共享 NAT 即使正确解析也仍共享 IP 预算。
- 接收预算 429 与容量 503 都返回 `Retry-After: 5`。client 对这两个 status 解析正整数秒或合法 HTTP 日期，等待限制在 1～60 秒；缺失/非法值回退 5 秒。其他 status 不消费该 header，不改变既有 400 丢弃与其他 4xx 保留/终止策略。
- runtime 收到 429/503 后只推迟 backlog lane：`backlogRetryNotBefore` 前不尝试历史项，不增加这些项的 attempts；每个原有 5 秒 tick 的 current 仍 durable enqueue 并发送。本轮 backlog 被拒绝也仍发送 current；current 被拒绝仍保留为后续 backlog。无额外 sleep/重试 goroutine，恢复后 FIFO、每轮最多两个历史项和 current 最后发送的规则不变。
- 接收拒绝只调用 `store.NoteReceiverHealthFault(reason)`：立即递增内存 generation 并保存最新 allowlisted reason，不逐请求写 SQL 或日志。五秒 Observer 合并持久化；每个周期都出现故障时该周期保持 unhealthy，不能合并成健康。归档证据的 generation 差异即时失效；观察事务期间新到的 fault 不得在 commit 后被提前 acknowledge。其他持久化故障继续使用 `MarkReceiverHealthFault` 的现有立即落库路径；Observer 数据库异常日志最多每观察周期一次。
- 只有核验 session/producer/seq/digest、disposition 合格的非零 batch ack 才能 durable `Delete`；
  成功删除后再计入 ack 并应用 plan。`deferred` 保留队列且不应用 plan，seq=0 不构成 batch ack。
  backlog plan 可依次应用，但 current response 最后应用，避免旧空 plan 覆盖 fresh plan。
- pending command result 与已 drain 的 IP-quality report 在 durable Enqueue 成功前必须继续保留于 runtime；
  没有 compatibility no-queue 路径。`FileStore.Enqueue` 若容量裁剪连 newest entry 自身也无法保留，
  必须返回 local durability error 且不改写原队列，不能假成功后清空 payload。
  byte cap 包含完整 envelope 元数据，oldest-first byte pruning 必须线性计算序列化大小。
- 与当前 ID、token 或任一 carrier fingerprint 不一致，以及缺少 heartbeat/heartbeat batch ID 的持久项属于 stale authority：先用一次 `DeleteMany` durable 删除本轮可独立删除的全部 stale 项，成功后才按 stable reason 聚合写脱敏 discard 日志并继续。不得为默认 72 小时积压逐项重写/fsync 整个队列或逐项刷日志。
- v2 envelope 的 entry ID 必须等于其 canonical producer/sequence 身份，序号与顺序必须满足持久边界。duplicate/empty/noncanonical ID 或损坏 envelope 一律拒绝，保留原始字节；不规范化、不加 suffix、不重编号。非空旧数组必须恢复旧 Agent 排空后再升级；Runtime 对这一 allowlisted 原因提供静态排空提示，不暴露原始文件错误或内容。
- 每次实际发送失败先 `MarkAttempt`；随后按下表决定 delete/return/continue。delete 或 mark 失败是本地 durability error，必须终止 runtime，不能记录虚假的“已丢弃”。
- 所有 `FileStore` operation 在等待 path lock 后重新检查 `ctx.Err()`；mutation 在原子写之前再次检查。已取消的调用不得在锁等待后继续修改 durable queue。原子 write 一旦开始则允许完整结束，不能留下半写文件。
- terminal error 由 runtime 返回给 `cmd/houfeng-agent`，只允许最外层入口记录一次；内部不得同时 log + return。
- parent context 在 fingerprint、credential load、enrollment、health check 或 queue boundary 取消时属于 clean shutdown；runtime 检查 `ctx.Err()`，不能把客户端独立产生的 `context.Canceled` 无条件吞掉。
- sync 与 enrollment 的日志和返回错误禁止包含 `RemoteError.Message`、原始 response body、token、Authorization、原始 fingerprint、request payload、persisted entry/instance ID 或 raw local queue cause。未知远端 code 不进入日志；wrapped cause 仅用于 `errors.As` / `errors.Is`，不能被主进程直接记录。
- `agent enrolled` 只在 bound、sync token 非空且凭据 durable persistence 成功后记录，并且只含 allowlisted status/binding status，不记录 MonitoringInstance ID。runtime start/stop 的 `server_url` 只记录 parsed scheme + host origin，不能记录 userinfo、path、query 或 fragment。

### 4. Validation & Error Matrix

| 条件 | Queue action | Runtime action |
| --- | --- | --- |
| stale authority / malformed local carrier | 一次 batch delete 成功后按 reason 聚合记 discard | 继续 flush |
| duplicate/empty/noncanonical persisted ID 或损坏 envelope | 原始字节不变，不做修复 | OpenSession fail closed，不发送 |
| HTTP `400` + `ErrorCodeInvalidJSON` / `ErrorCodeInvalidRequest` | mark → delete 成功后记 discard；计入本轮 backlog 上限 | backlog lane 可继续；current discard 后结束本轮 |
| `deferred` 或 ack identity/digest 不匹配 | 保留 entry，不计成功 ack | 不应用 plan；deferred 可 retry，identity mismatch fail closed |
| `401` / `404` / `409` / `405` / `426` 或其他非 `429` 4xx | mark，保留 entry | 返回 sanitized terminal error，不自动 reenroll/重编号 |
| backlog `429` / `503` | mark，保留 entry | 停止本轮 backlog lane，仍尝试 exact current；Retry-After 到期后从同一 FIFO head retry |
| current `429` / `503` | mark，保留 entry | 结束本轮；延后后续 backlog，原 tick 的 fresh current 继续发送 |
| backlog 其他 5xx / transport failure | mark，保留 entry | 停止本轮 backlog lane，但仍尝试 exact current；下一 tick 从同一 backlog head retry |
| current 其他 5xx / transport failure | mark，保留 entry | 结束本轮；下一 tick 作为 backfilled backlog retry |
| typed remote status 0、2xx 或 3xx error | mark，保留 entry | 作为不明确 remote failure 下一 tick retry，不做推测性 delete |
| 非 400 status 携带 `invalid_json` / `invalid_request` code | 以 status 为准，不 delete | 4xx terminal 或 5xx retry |
| durable Mark/Delete 失败 | 保留可恢复状态 | 返回 local queue error |

### 5. Good / Base / Bad Cases

- Good：旧 instance/token/fingerprint 项位于队头；agent 删除它并在同一次 flush 送达当前心跳。
- Good：五万条同一 stale reason 的历史积压只触发一次 atomic `DeleteMany` 与一条聚合日志，不产生 O(n²) rewrite/fsync 或日志洪泛。
- Good：v2 queue 保留多个有独立 seq 的事实；backlog lane 仍 FIFO，每两个旧 entry 后 exact current 可有界越过积压，后续轮次继续排空。
- Good：含辅助 payload 的 Enqueue 在 rename 前失败，payload 仍在内存；rename 后目录 fsync 结果不确定则 poison/终止，下一进程只从可见 envelope 恢复。
- Base：backlog head 暂时 503；队头保留，停止本轮 backlog lane，但本轮 current 仍发送；Retry-After 到期前每轮只送 fresh current，到期后旧项继续作为 FIFO head retry。
- Bad：用 caller 提供的 carrier `SyncBatchID` 代替 Enqueue 返回的 durable entry 身份决定 current/backfill，或给冲突 ID 加 suffix 试图绕过 v2 序号校验。
- Bad：在 current 前遍历完整 backlog；65,536 条 durable queue 会让实时 heartbeat/host sample 长时间没有网络尝试。
- Bad：看到任意 `invalid_request` 字符串就 delete，导致携带该 code 的 404/500 被误当作脏请求。
- Bad：先 log “discarded” 再执行 delete；磁盘写失败时日志宣称的状态与 durable file 相反。
- Bad：terminal 分支既在 runtime 内 log 又 return 给 main，产生两条同一故障日志。
- Bad：把 persisted entry ID / stale instance ID 或 local queue cause 放进 Error/log；持久文件可损坏或被本机高权限操作改变，这些值不是安全的日志字段。
- Bad：收到 enrollment response 就先记 `agent enrolled`，随后才发现 binding 未确认、token 缺失或凭据写盘失败。

### 6. Tests Required

- `TestRuntimeRejectedPersistedIdentityDoesNotBlockCurrentCredentialHeartbeat`：真实 `syncqueue.FileStore` 证明旧 authority 不阻塞当前心跳。
- `TestRuntimeDiscardsPersistedQueueEntriesOutsideCurrentAuthority`：ID/token/各 carrier fingerprint 与最小 heartbeat identity 矩阵。
- `TestRuntimeBulkDiscardsLargeStaleAuthorityBacklog`：大积压只调用一次 batch delete、按 reason 聚合日志，并立即送达当前心跳。
- `TestRuntimeRejectsNonemptyLegacyQueueWithoutChangingFacts`、`TestFileStoreRejectsLegacyEmptyCorruptAndIncompleteBytesUnchanged`、`TestFileStoreRejectsInvalidEnvelopesWithoutRepair`：非空旧队列及无效 v2 身份拒绝且原始字节不变，不提供 duplicate-ID 恢复分支。
- `TestFileStoreAssignsCanonicalIdentityWithoutMutatingCaller`：持久化分配 canonical producer/sequence 身份，不修改调用方载荷。
- `TestRuntimeRetainsCommandResultUntilQueueEnqueueSucceeds`、`TestRuntimeRetainsIPQualityReportUntilQueueEnqueueSucceeds`：辅助 payload 只在 durable transfer 后从 runtime buffer ack。
- `TestFileStoreEnvelopeBytesCanRejectEntryThatBareArrayWouldFit`、`TestRuntimeCapacityFailureRetainsPendingAuxiliaryPayloads` 等容量回归：newest 无法满足含 envelope 的 MaxBytes 时 fail closed；byte pruning 线性，极小 cap 不改写原文件或丢失辅助 payload。
- `TestRuntimeDiscardsExplicitInvalidQueueEntryAndContinues`：仅 HTTP 400 + 两个稳定 invalid code 可 discard。
- `TestRuntimeCurrentAuthorityPermanentRemoteErrorsAreTerminalAndSanitized`：永久 4xx 保留 entry、只返回一次脱敏 terminal evidence，并覆盖 status/code precedence。
- `TestRuntimeLogsDiscardOnlyAfterQueueDeleteSucceeds`：stale/poison delete 失败时没有虚假 discard 日志。
- `TestRuntimeTransientQueueFailuresRemainRetryableAndBackfilled`：status 0/2xx/3xx、429/5xx/transport 保留、重试与 secret-bearing cause 不泄露。
- `TestRuntimeBoundsReplayBeforeCurrentDurableRequest`、`TestRuntimeReplaysBacklogFIFOAcrossFreshInterleaving`：fresh 前最多两个旧尝试，且 backlog lane 跨轮保持 FIFO 并最终排空。
- `TestRuntimeRetryableBacklogHeadDoesNotBlockCurrentDurableRequest`、`TestRuntimeCurrentRequestRetryRemainsDurableAndBackfilled`：旧 retry 只阻断 backlog lane；失败 fresh durable 留存并在下一轮变为 backfill。
- `TestRuntimeCongestionCooldownSkipsOnlyBacklog`、`TestRuntimeCooldownConsumesOnly429And503`：真实 FileStore 保留被拒绝 current/backlog，冷却期间 current 不间断，deadline 边界恢复最多两个 FIFO 补传；其他 status 不开启冷却。
- `TestRetryAfterBoundsAndFallback`、`TestClientConsumesRetryAfterOnlyForCongestion`：秒/HTTP 日期、1～60 秒边界、缺失/非法回退及 status 优先级。
- `TestAgentSyncSharedIPFleetBudget`、`TestAgentReceiverDefaultBudgetsAndWindow`、`TestAgentRateLimitOptionsNormalizeEachField`：30 session 同一 IP 的 360/1080 次每分、2400 同步及注册 120/1000 阈值、单字段配置不关闭其他预算、伪造 XFF 与滑动窗口恢复。
- `TestReceiverHealthFaultStormCoalescesWithoutHealthyTicks`、`TestReceiverHealthFaultReasonsAreAllowlisted`、`TestReceiverHealthFaultDuringCommitIsNotAcknowledged`、`TestReceiverHealthDatabaseFailureRecoveryResetsContinuity`：拒绝风暴仅随观察 tick 写库，即时禁止归档、持续故障不恢复健康及数据库故障后重新开始观察。
- `TestRuntimeCurrentResponsePlanWinsAfterBoundedReplay`：durable delete 后应用响应，且 current plan 最后生效。
- `TestRuntimeBoundedReplayPreservesDurableAuxiliaryPayloads`、`TestRuntimeLocalEntryIDControlsCurrentAndBackfilledOnBatchIDCollision`：辅助载荷不丢，本地 entry ID 独立决定 live/backfill。
- `TestFileStoreRechecksCancellationAfterLockAcrossOperations`：锁前检查后发生的取消也不能继续 durable mutation，包含 DeleteMany。
- parent cancellation 与脱敏错误回归必须覆盖 fingerprint/credential/enrollment/health/queue boundaries；没有无持久队列直发兼容路径。
- `TestRuntimeSanitizesLocalFailuresBeforeSyncLoop`：fingerprint、credential load、enrollment token 与 credential persistence 的原始 local cause 只可通过 unwrap 取得，不进入主进程可见 error string。
- `TestRuntimeLogsEnrollmentOnlyAfterCredentialsPersist`、`TestRuntimeEnrollmentSuccessLogDoesNotExposePersistedIdentity`、`TestRuntimeDoesNotLogServerURLCredentials`：覆盖 success-after-durability、enrollment identity privacy 与仅 origin 的启动日志。

### 7. Wrong vs Correct

```go
// Wrong: every remote error blocks the queue forever, and logging the raw
// cause may include RemoteError.Message/body.
return fmt.Errorf("remote sync failed: %w", err)
```

```go
// Correct: classify by typed status/code, preserve the cause for errors.As,
// but expose only sanitized policy fields and apply delete-before-log ordering.
policy := &syncPolicyError{
	kind: syncFailureKindTransport, action: syncQueueActionRetry, cause: err,
}
var remoteErr *enroll.RemoteError
if !errors.As(err, &remoteErr) {
	return policy
}
policy.kind = syncFailureKindRemote
policy.statusCode = remoteErr.StatusCode
policy.code = allowlistedAgentErrorCode(remoteErr.Code)
switch {
case remoteErr.StatusCode == http.StatusBadRequest &&
	(remoteErr.Code == agentapi.ErrorCodeInvalidJSON ||
		remoteErr.Code == agentapi.ErrorCodeInvalidRequest):
	policy.action = syncQueueActionDiscard
case remoteErr.StatusCode == http.StatusTooManyRequests || remoteErr.StatusCode >= 500:
	// keep retry
case remoteErr.StatusCode >= 400:
	policy.action = syncQueueActionTerminal
}
return policy
```

---

## Scenario: Agent durable replay 聚合状态日志

### 1. Scope / Trigger

- Trigger：修改 `agent/runtime` 的 durable replay scheduler、`syncRoundResult`、retry policy 或 `sync queue replay progress` 日志。
- 目标：运维能区分正在追赶、已经追平和 retrying，同时不把逐条 queue/request 内容或凭据写入 journal。

### 2. Signatures

- 固定 message：`sync queue replay progress`。
- `Info` state：`catching_up | caught_up`；字段只允许 `state`、`acked_entries`、`remaining_entries`。
- `Error` state：`retrying`；除 `error` 外只允许 `state`、allowlisted `kind/action/status/code` 与两个 aggregate count。
- 内存状态：`replayActive`、`lastReplayProgressAt`；持续进度间隔固定为一分钟，不新增 durable telemetry 文件或 wire 字段。

### 3. Contracts

- `acked_entries` 只统计 Sync 成功且对应 durable `Delete` 已完成的旧 backlog entry；invalid 400 的 discard 即使使 remaining 归零，也不能伪装成成功 ack。
- 首次有健康 replay progress 写一次 `catching_up`；持续追赶每轮最多一条且至多每 60 秒一条；归零时只写一次 `caught_up`；随后普通 live tick 保持安静。
- retryable round 使用 `Error state=retrying`，不能同时把该轮写成健康 catching-up。失败 entry 保留在 durable queue。
- 日志不得包含 sync/enrollment token、Authorization、DSN、fingerprint、server URL、MonitoringInstance/object/entry/batch ID、请求/响应 payload、remote message、raw local/remote cause、stdout/stderr。
- 所有测试使用注入 clock 推进节流窗口，不真实 sleep；失败文案也只能输出计数、位置、布尔或固定分类。

### 4. Validation & Error Matrix

| Condition | Expected |
| --- | --- |
| backlog ack 后仍有旧项 | `Info state=catching_up`，计数来自 durable delete 后状态 |
| backlog 全部 ack | 单次 `Info state=caught_up remaining_entries=0` |
| invalid 400 discard 到 0 | 可结束 replay episode，但 `acked_entries=0`；不得记虚假成功 |
| backlog retryable failure | `Error state=retrying`，entry retained；不写 catching_up/caught_up |
| 60 秒内持续健康进度 | 抑制重复 catching_up 日志 |
| delete 失败 | 不增加 ack，不写健康 progress；返回本地 durability error |

### 5. Good / Base / Bad Cases

- Good：每轮 durable ack 两条，首次打印 catching_up；一分钟内其余健康轮静默，最终排空只打印一次 caught_up。
- Base：没有 backlog 的普通 fresh tick 不写 replay 日志。
- Bad：每成功发送一条就打印 entry/batch ID，造成日志洪泛和标识泄露。
- Bad：网络成功、durable delete 失败后仍增加 ack 或打印 caught_up。
- Bad：把 `RemoteError.Message` 或 wrapped raw cause 作为 `error` 字段记录。

### 6. Tests Required

- `TestRuntimeLogsBoundedReplayProgressAfterDurableAck`、`TestRuntimeLogsInstantSuccessfulReplayDrainAsCaughtUpOnce`：progress 只来自 durable ack，直接排空只写一次 caught_up。
- `TestRuntimeLogsReplayCaughtUpOnceAndKeepsLiveTicksQuiet`、`TestRuntimeThrottlesReplayProgressLogs`：episode 转换、普通 live tick 静默与注入 clock 的 60 秒节流。
- `TestRuntimeReplayRetryIsFailureStateNotHealthyProgress`、`TestRuntimeReplayLogsDoNotExposeSensitiveQueueOrRequestFields`：retrying level/fields 和 sentinel privacy。
- delete failure、invalid discard tests 必须证明无虚假 ack/caught-up；focused tests 运行 `-count=10`，runtime package 运行 `-race`。

### 7. Wrong vs Correct

```go
// Wrong: per-entry log leaks durable identifiers and counts network success
// before the queue state is durable.
logger.Info("replayed entry", "entry_id", entry.ID)
acked++
_ = queue.Delete(ctx, entry.ID)
```

```go
// Correct: delete first; expose only aggregate episode state.
if err := queue.Delete(ctx, entry.ID); err != nil {
	return err
}
round.ackedEntries++
logger.Info("sync queue replay progress",
	"state", "catching_up",
	"acked_entries", round.ackedEntries,
	"remaining_entries", round.remainingEntries)
```

## Scenario: agent sync queue disk bounds

1. **Scope / Trigger**
   - Trigger: 修改 `agent/syncqueue/`、`agent/config/` durable buffer env、`agent/runtime` queue construction、installer `agent.env`，或离线队列保留策略。
   - 目标：agent 离线时保留可重试 sync request，但必须同时受 entry 数、年龄、磁盘字节上限约束，避免长期离线写满磁盘。

2. **Signatures**
   - Config env:
     - `HOUFENG_AGENT_BUFFER_FILE`
     - `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`
     - `HOUFENG_AGENT_BUFFER_MAX_AGE`
     - `HOUFENG_AGENT_BUFFER_MAX_BYTES`
   - Go config: `agent/config.AgentConfig{BufferFile, BufferMaxEntries, BufferMaxAge, BufferMaxBytes}`。
   - Queue options: `syncqueue.Options{MaxEntries, MaxAge, MaxBytes, SkipFsync}`。

3. **Contracts**
   - Defaults: `MaxEntries=65536`、`MaxAge=72h`、`MaxBytes=64MiB`。
   - `MaxBytes <= 0` uses the default; env override must be a positive integer.
   - Queue pruning order is oldest first after sorting by `CreatedAt` / ID, then max entries, then max bytes.
   - Byte pruning must calculate serialized entry sizes in a linear pass; a large v2 queue must not repeatedly marshal every remaining tail once per evicted entry.
   - The byte cap includes envelope metadata. If the newly enqueued entry cannot fit even by itself, `Enqueue` must return a local durability error and leave the previously persisted queue unchanged; it must never report success after pruning away the new entry or write a file larger than the cap.
   - Production runtime must pass `BufferMaxBytes` into `syncqueue.NewFileStore`; tests may set small caps and `SkipFsync: true`.

4. **Validation & Error Matrix**
   | Condition | Expected behavior |
   | --- | --- |
   | `HOUFENG_AGENT_BUFFER_MAX_BYTES` missing | default 64MiB |
   | non-integer / `<=0` max bytes | config load error |
   | two entries exceed max bytes but newest fits | oldest dropped, newest remains, file size <= cap |
   | newest entry exceeds max bytes | `Enqueue` returns a durability error; the previously persisted queue is unchanged and no oversized file is written |

5. **Good / Base / Bad Cases**
   - Good: center is offline for days; queue keeps recent facts within 64MiB and drops oldest entries predictably.
   - Base: operator lowers max bytes for a tiny VPS; fitting new entries may evict older entries, while an individually oversized new entry fails closed and preserves the prior queue.
   - Bad: only limiting entry count lets a few very large command/IP quality payloads fill disk.

6. **Tests Required**
   - `agent/config/config_test.go`: defaults, override, invalid max bytes.
   - `agent/syncqueue/store_test.go`: byte pruning keeps newest fitting entries and file size stays below cap; an individually oversized new entry returns an error without mutating the prior queue; a default-capacity-scale oversized persisted backlog is pruned without quadratic tail re-encoding.
   - `agent/runtime` or construction coverage proving `BufferMaxBytes` reaches `syncqueue.Options`.
   - Installer embedded-script check that generated `agent.env` includes `HOUFENG_AGENT_BUFFER_MAX_BYTES`.

7. **Wrong vs Correct**

```go
// 错误：只限制 entries/age，忽略单条 payload 大小。
syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: cfg.BufferMaxEntries, MaxAge: cfg.BufferMaxAge})
```

```go
// 正确：同时传入字节上限。
syncqueue.NewFileStore(path, syncqueue.Options{
	MaxEntries: cfg.BufferMaxEntries,
	MaxAge:     cfg.BufferMaxAge,
	MaxBytes:   cfg.BufferMaxBytes,
})
```

---

## 设置快照与覆盖优先级

Agent 计划读取同一已持久化设置快照中的主机频率、探测默认、事件判定默认和覆盖规则。规则校验必须使用该快照的全局阈值，不能改用代码默认值而拒绝已经通过设置保存校验的部分覆盖。持久化 JSON 或策略非法时仍失败，不以忽略规则降级；没有设置行时沿用既有缺省行为。使用校验后规范化的 selector 和字段。

主机频率由监控标签规则数组中首个精确匹配且有主机频率的规则覆盖，否则保留全局值。探测执行计划以 `probe_items.frequency_tier` 为基准，不以全局新建默认为基准；先应用匹配目标类型的当前探测 kind，再由目标标签数组中首个精确匹配且含该 kind 的规则覆盖。匹配但缺字段的标签规则继续向后查找，标签大小写不同不匹配。

本合同不新增事件覆盖运行消费者。事件判定覆盖、监控标签的探测覆盖及目标范围的主机覆盖继续保存和验证，但不影响当前运行计划；Web 必须如实区分存储值和实际应用值。
