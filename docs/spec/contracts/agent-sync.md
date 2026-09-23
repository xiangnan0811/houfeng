# Agent 安装、同步与兼容合同

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

## Agent sync batch INSERT-only idempotency

### 1. Scope / Trigger

- Trigger: 修改 `internal/center/store/sync_batches.go`、`agent_sync_batches` 的 ACL / schema / unique constraint，或 agent heartbeat / sync ingestion 的幂等事务。
- 目标：runtime role 对 `agent_sync_batches` 保持最小 INSERT-only 权限，同时首次 batch 写入、原样重试和事务内事实去重继续工作。

### 2. Signatures

- Production SQL contract: `INSERT INTO agent_sync_batches (...) VALUES (...) ON CONFLICT DO NOTHING`，不得给 `ON CONFLICT` 添加 column list 或 constraint name。
- Required PostgreSQL integration test: `TestPostgresIntegrationAgentSyncBatchRuntimeACL`，必须通过生产 `NewPostgresSyncRepository` 和 direct runtime role 执行。

### 3. Contracts

- Runtime privilege vector 必须是 `INSERT=true`、`SELECT=false`、`UPDATE=false`、`DELETE=false`；不得用表级或列级 SELECT 来绕过 ingest SQL 的权限问题。
- PostgreSQL 16 对显式 conflict target（例如 `(monitoring_instance_id, sync_batch_id)`）要求读取 target columns，因此会让 INSERT-only runtime 以 SQLSTATE `42501` 失败；生产查询必须使用 targetless `ON CONFLICT DO NOTHING`。
- 当前 schema 的唯一性前提只有 `PRIMARY KEY (monitoring_instance_id, sync_batch_id)`，所以 targetless 形式保持首次写入和原样重复的既有语义；首次 insert 的 `RowsAffected()` 必须为 1 并继续写事实，重复 insert 必须为 0、提交空 plan，且不得重写 heartbeat、observation 或 `last_heartbeat_at` / `last_sync_at`。
- 给 `agent_sync_batches` 新增任何 primary key 或 unique constraint 前，必须重审 targetless “忽略任意冲突”的语义、ACL 和 direct-runtime 回归；schema 变更不得默认沿用当前前提。
- 验证 binding / token / fingerprint 和写入抑制状态后，batch marker 仍必须在 heartbeat / observation facts 之前写入；不得调整事务顺序、参数或错误包装来修复 ACL。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| current direct runtime ACL | INSERT allowed; SELECT/UPDATE/DELETE denied |
| first heartbeat-only batch | batch marker and heartbeat each persist once; sync timestamps advance |
| exact duplicate batch | accepted with an empty plan; batch/heartbeat row counts remain one and first-write heartbeat/sync timestamps remain unchanged |
| explicit conflict target under INSERT-only runtime | wrapped PostgreSQL SQLSTATE `42501`; regression RED |
| new unique constraint proposed | block until targetless conflict semantics and direct-runtime test are reviewed |

### 5. Good/Base/Bad Cases

- Good: direct runtime 只拥有 INSERT，首次 heartbeat-only `ApplyBatch` 成功，原样重试也成功且不重复写事实。
- Base: owner/migrator 负责 seed 和事后计数；被测生产调用始终由 direct runtime session 执行。
- Bad: 为了让显式 conflict target 工作而给 runtime 增加 SELECT，扩大了 ingest role 的读取能力。
- Bad: 只用 fake transaction 或 ACL catalog 断言证明修复；它们不能证明 PostgreSQL 16 实际的 privilege check。

### 6. Tests Required

- SQL shape 单元测试必须同时断言包含 targetless `on conflict do nothing`，且不包含 `on conflict (`。
- 必须运行真实 PostgreSQL 16 direct-runtime 回归；测试不得因 DSN、fixture 或容器缺失而 skip-as-pass，严格命令为：

```bash
scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store -run '^TestPostgresIntegrationAgentSyncBatchRuntimeACL$' -count=1
```

- 真实回归必须在调用前后核验 ACL 未扩张，并证明首次/重复成功、batch/heartbeat 各一行；首次调用推进 `last_heartbeat_at` / `last_sync_at`，重复调用使用不同 receive time 并证明两个状态时间不被重写。

### 7. Wrong vs Correct

```sql
-- 错误：PG16 读取显式 target columns，与 INSERT-only ACL 冲突。
on conflict (monitoring_instance_id, sync_batch_id) do nothing

-- 正确：在当前唯一复合主键前提下保留 INSERT-only 幂等合同。
on conflict do nothing
```

## Scenario: agent local state upgrade compatibility

1. **Scope / Trigger**
   - 触发：重命名 agent ↔ center 契约字段、修改 `agent/token/`、`agent/syncqueue/`、installer token-preserve 分支，或发布需要从旧 agent 本地状态平滑升级的新版本。
   - 目标：新版本 agent 能读取旧版本已经落盘的本地状态，避免 upgrade 后卡在不可发送的旧队列 entry 或覆盖已绑定 token。

2. **Signatures**
   - token 文件当前写入格式：`{"monitoring_instance_id":"<id>","sync_token":"<token>"}`。
   - token 文件 legacy 读取格式：`{"node_id":"<id>","sync_token":"<token>"}`。
   - sync queue entry 当前写入格式：`Entry{Request: agentapi.SyncRequest{MonitoringInstanceID, SyncToken, Heartbeats...}}`，JSON 字段为 `request.monitoring_instance_id`。
   - sync queue entry legacy 读取格式：`request.node_id`。
   - installer preserve 条件：已有 `/etc/houfeng-agent/token` 同时包含 `sync_token`，且包含 `monitoring_instance_id` 或 legacy `node_id`。

3. **Contracts**
   - 新写入一律使用 current `monitoring_instance_id`，不得重新对外发送或写入 `node_id`。
   - 读取 legacy `node_id` 时，仅把它映射为内存中的 MonitoringInstance ID；如果 current 与 legacy 字段同时存在，current 字段优先。
   - `sync_token` 仍然必需；只有 ID 字段但没有 `sync_token` 的 token 文件必须继续被判定为 incomplete credentials。
   - installer 只做存在性检查和权限收敛，不解析、不打印 token 内容。

4. **Validation & Error Matrix**
   - token JSON 含 `monitoring_instance_id` + `sync_token` -> sync credentials ok。
   - token JSON 含 `node_id` + `sync_token` -> sync credentials ok，返回 MonitoringInstance ID = legacy node id。
   - token JSON 含 `node_id` 但缺 `sync_token` -> incomplete sync credentials error。
   - sync queue JSON 含 `request.node_id` -> `List` 返回的 request 必须填充 `MonitoringInstanceID`。
   - legacy queue entry 缺 heartbeat / agent_version / fingerprint / sync_batch_id -> 不在 queue 层吞掉，仍由 center contract 校验拒绝。

5. **Good / Base / Bad Cases**
   - Good: v0.24.x agent 已有 `sync-buffer.json`，v0.25.x agent 启动后读出 legacy `node_id`，flush 时发送 current `monitoring_instance_id` carrier。
   - Good: 旧 token 文件存在 `node_id` + `sync_token`，installer upgrade 保留该文件并只修正 owner/mode。
   - Base: 新 enrollment 后 token 文件只包含 `monitoring_instance_id` + `sync_token`。
   - Bad: installer 只识别 current token 字段，导致 upgrade 时覆盖旧 post-enrollment sync credentials。
   - Bad: `agentapi.SyncRequest` 新写 JSON 同时携带 `node_id`，把内部兼容泄漏成新 public contract。

6. **Tests Required**
   - `agent/token/file_test.go`：覆盖 legacy `node_id` token、current 字段优先、缺 `sync_token` 仍失败、保存后只写 current 字段。
   - `agent/syncqueue/store_test.go`：覆盖 legacy `request.node_id` 被映射为 `MonitoringInstanceID`。
   - `internal/center/installer/embed_test.go`：覆盖 installer preserve 条件同时接受 current 与 legacy ID 字段，并要求 `sync_token`。

7. **Wrong vs Correct**

```go
// 错误：只认 current 字段会把 legacy queue entry 反序列化成空 ID。
type SyncRequest struct {
	MonitoringInstanceID string `json:"monitoring_instance_id"`
}
```

```go
// 正确：在本地持久化 reader 里兼容 legacy 字段，内存和新写入仍使用 current 字段。
if request.MonitoringInstanceID == "" {
	request.MonitoringInstanceID = legacyNodeID
}
```

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
   - 不新增 agent env key，不改变 `internal/contracts/agentapi.HostSamplePayload` JSON contract。

4. **Validation & Error Matrix**
   - 必需来源读取失败 -> `Collect` 返回带上下文的 wrapped error，例如 `darwin sysctl vm.loadavg: %w` 或 `read /proc/loadavg: %w`。
   - Darwin `vm.swapusage` 读取失败 -> `swap_used_pct=0`，不得阻塞整条 host sample，因为部分 macOS 环境可能禁用 swap。
   - Darwin rate-based 字段没有稳定来源时保持零值，不得让 center 拒收 host sample。

5. **Good / Base / Bad Cases**
   - Good: macOS 本地 agent 能完成 `host_samples` 上报，Linux systemd agent 仍走完整 procfs 指标。
   - Base: 首个 sample 的 CPU/net/disk rate 字段为零，后续 Linux sample 根据 previous snapshot 推导 rate。
   - Bad: 在 Darwin 分支 fallback 读取 `/proc/loadavg`，会让 macOS smoke 重新出现 `no such file or directory`。

6. **Tests Required**
   - Linux/procfs fixture tests: `agent/hostsample/provider_test.go` 覆盖 load/mem/uptime/rate/diskstats。
   - Darwin regression test: `agent/hostsample/provider_darwin_test.go` 必须断言不读取 `/proc/*`，并检查 load、mem、swap、disk、uptime 的核心字段。
   - Runtime safety: `go test ./agent/runtime` 必须继续通过，确保 host sample 仍被 `buildSyncRequest` 串入 sync batch。

7. **Wrong vs Correct**
   - Wrong: 在 `agent/runtime` 里按 OS 分支，或让 runtime 感知 `sysctl` / `/proc` 文件名。
   - Correct: 在 `hostsample.New()` / `Provider.Collect` 内选择平台 collector，runtime 只消费 `HostSamplePayload`。

## Scenario: Agent one-command install contract

1. **Scope / Trigger**
   - 触发：修改 MonitoringInstance onboarding、一键安装命令、center-served installer、`HOUFENG_PUBLIC_BASE_URL`、agent release artifact 命名、或 `/api/agent/install.sh` 路由。
   - 目标：让每个自部署 center 负责生成自己的安装命令和 enrollment token；GitHub Release 只提供二进制与 signed checksum manifest，不能成为 token/script authority，也不能只靠同源 checksum 提供供应链信任。

2. **Signatures**
   - Config: `config.CenterConfig.PublicBaseURL` 来自 `HOUFENG_PUBLIC_BASE_URL`，必须是无 query/fragment 的 absolute `http(s)` URL，可为 domain 或 `IP:port`。
   - Public route: `GET agentapi.InstallScriptPath` -> embedded shell script，未登录可读，只允许读取脚本。
   - Installer-pinned checksum public key: `HOUFENG_CHECKSUM_MINISIGN_PUBLIC_KEY` inside `internal/center/installer/houfeng-agent-install.sh`。
   - Release assets: `houfeng-agent_<version>_linux_amd64`、`houfeng-agent_<version>_linux_arm64`、`sha256sums.txt`、`sha256sums.txt.minisig`。
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
   - Generated commands include `--install-missing-deps` so Debian 11 / older Ubuntu style hosts without a packaged `minisign` can recover without separate operator diagnosis. Manual installer invocations may omit the flag for an interactive `/dev/tty` prompt, or pass `--no-install-missing-deps` to fail closed when `minisign` is missing.
   - If `minisign` is missing and dependency recovery is allowed, the installer must download the pinned upstream static minisign tarball, verify its embedded SHA256 before extracting, install only the matching `amd64`/`arm64` verifier to `/usr/local/bin/minisign`, ensure the current script `PATH` can find it, then continue. Do not use apt/yum/dnf/apk or enable distro repositories in the installer path; package availability differs by distribution and version.
   - Any installer prompt must read from `/dev/tty`, never stdin, because generated commands reserve stdin for `--enrollment-token-stdin`.
   - The installer must require a working `minisign` before release verification, download `sha256sums.txt.minisig`, verify `sha256sums.txt` with the pinned public key, then verify the downloaded binary against the signed manifest before replacing `/usr/local/bin/houfeng-agent` or starting systemd. Missing signature, signature failure, missing checksum entry, checksum mismatch, denied dependency recovery, failed minisign bootstrap checksum, or failed bootstrap install must fail closed without checksum-only fallback.
   - Release workflow must sign `dist/sha256sums.txt` before uploading release assets. `HOUFENG_RELEASE_MINISIGN_PRIVATE_KEY` must match the installer-pinned public key; encrypted keys require `HOUFENG_RELEASE_MINISIGN_PASSWORD`.
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
   - Release target test/sanity that `make build-agent-release VERSION=<tag>` emits both Linux binaries and `sha256sums.txt` with names matching installer expectations; publish workflow review checks signing and upload of `sha256sums.txt.minisig`.

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

`internal/center/http/handlers/agent.go:106-108` 的 `writeAgentAPIError` 是 agent 专用错误响应，**额外带 `agentapi.ErrorCode*`**：

```go
func writeAgentAPIError(w http.ResponseWriter, status int, code, message string) {
    writeJSON(w, status, agentapi.ErrorResponse{Code: code, Message: message})
}
```

agent handler 的判定与映射全在 `agent.go:46-94`，必须使用 `agentapi.ErrorCode*` 常量，不要造新字符串：

| 领域 error | HTTP status | `agentapi.ErrorCode*` |
|-----------|-------------|-----------------------|
| `enrollment.ErrInvalidEnrollmentToken` | 401 | `ErrorCodeInvalidEnrollmentToken` |
| `syncing.ErrInvalidSyncToken` | 401 | `ErrorCodeInvalidSyncToken` |
| `syncing.ErrBindingNotAccepted` | 409 | `ErrorCodeBindingNotAccepted` |
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
}
```

## Scenario: agent durable sync queue 的远端失败策略

### 1. Scope / Trigger

- Trigger：修改 `agent/runtime` 的 queue flush、`agent/syncqueue` authority/backfill、`agent/enroll.RemoteError` 消费或 agent sync 错误日志。
- 目标：永久拒绝或脏队列项不能阻塞后续当前心跳，同时不能把临时故障误删，也不能把持久队列内的凭据带入日志。

### 2. Signatures

- typed remote cause：`*enroll.RemoteError{StatusCode, Code, Message}`。
- 当前 queue authority：runtime 已加载的 `monitoring_instance_id`、`sync_token` 与本机 `fingerprint` 精确三元组。
- 本地 policy result：`kind=remote|transport`、`action=discard|terminal|retry`、可选 allowlisted `status` / `agentapi.ErrorCode*`。
- stale backlog durability primitive：`SyncQueue.DeleteMany(context.Context, []string) error`；生产 `FileStore` 必须在一次加锁、一次原子文件替换中删除整批 stale ID。
- `FileStore` 返回给 runtime 的 entry ID 必须唯一：enqueue 遇到 heartbeat batch ID 冲突时分配确定性 suffix，读取 legacy/hostile 持久文件时也在排序后确定性规范化 duplicate/empty ID，保证单条 ack 不会删除尚未发送的同 ID fact。
- 该规范化只作用于本地 `Entry.ID`，不得改写 carrier `SyncBatchID`。重复 SyncBatchID 仍是 center 的同一幂等事实，不能宣称 suffix 后会成为两个独立 center facts。大量同 ID 持久项的规范化、以及已有密集 `base-N` suffix 时的新 ID 分配都必须近似线性，不得为每个候选 suffix 重扫整个队列造成 O(n²) recovery。
- wrapped policy / remote operation / pre-sync local operation / local queue error 必须提供 `Unwrap() error`，使调用方仍可通过 `errors.As` / `errors.Is` 取得 typed cause；其 `Error()` 只格式化稳定、脱敏字段。
- 每轮调度上界：`maxBacklogSyncAttemptsPerRound = 2`；`Enqueue` 返回的本地 `Entry.ID` 是本轮 current carrier 的唯一身份。

### 3. Contracts

- 每个 tick 先 durable enqueue 当前请求，再以 exact 本地 `Entry.ID` 分离 backlog lane 与 current lane。backlog lane 仍按 `CreatedAt, ID` FIFO，但每轮最多尝试两个旧 entry；current entry 始终在本轮最后尝试，因此 fresh 不会被大积压无限饿死。该合同只放宽全局 FIFO，不放宽 backlog lane 内 FIFO。
- 只有旧 entry 发送时才把其中事实标记 `is_backfilled=true`；不得用可碰撞的 carrier `SyncBatchID` 判断 current/backfill。每轮最多 `K+1=3` 次同步尝试。
- 每个成功响应必须先完成对应 entry 的 durable `Delete`，再计入 ack 并应用 response plan。backlog plan 可依次应用，但 current response 最后应用，避免 exact-duplicate backlog 的空 plan 覆盖 fresh plan。
- pending command result 与已 drain 的 IP-quality report 在 durable queue Enqueue 成功前必须继续保留于 runtime；compatibility no-queue 路径则只在 Sync 成功后清理。`FileStore.Enqueue` 若容量裁剪连 newest entry 自身也无法保留，必须返回 local durability error 且不改写原队列，不能假成功后让 runtime 清空这些 payload。对超过新 `MaxBytes` 的 legacy/hostile 大文件做 oldest-first byte pruning 时必须线性计算序列化大小，不能为每个待删 entry 反复 marshal 整个剩余队列。
- 与当前 ID、token 或任一 carrier fingerprint 不一致，以及缺少 heartbeat/heartbeat batch ID 的持久项属于 stale authority：先用一次 `DeleteMany` durable 删除本轮可独立删除的全部 stale 项，成功后才按 stable reason 聚合写脱敏 discard 日志并继续。不得为默认 72 小时积压逐项重写/fsync 整个队列或逐项刷日志。
- 生产 `FileStore.List` 在 runtime 分类前确定性规范化 duplicate/empty persisted ID，后续 Delete/DeleteMany/MarkAttempt 必须以相同映射重新读取，下一次 mutation 把规范化 ID 持久化。这样两个 retained current facts 即使原文件复用同一 ID，也能 oldest-first 分别发送/ack；第一次 Delete 不能删除第二个未发送 fact。对未实现此能力的其他 `SyncQueue`，runtime 仍以 collision guard 禁止 stale ID-wide delete 与 retained current ID 相撞。
- 每次实际发送失败先 `MarkAttempt`；随后按下表决定 delete/return/continue。delete 或 mark 失败是本地 durability error，必须终止 runtime，不能记录虚假的“已丢弃”。
- 所有 `FileStore` operation 在等待 path lock 后重新检查 `ctx.Err()`；mutation 在原子写之前再次检查。已取消的调用不得在锁等待后继续修改 durable queue。原子 write 一旦开始则允许完整结束，不能留下半写文件。
- terminal error 由 runtime 返回给 `cmd/houfeng-agent`，只允许最外层入口记录一次；内部不得同时 log + return。
- parent context 在 fingerprint、credential load、enrollment、non-queue client 或 queue boundary 取消时属于 clean shutdown；runtime 检查的是 `ctx.Err()`，不能把客户端内部独立产生的 `context.Canceled` 无条件吞掉。
- queue/non-queue sync 与 enrollment 的日志和返回错误禁止包含 `RemoteError.Message`、原始 response body、token、Authorization、原始 fingerprint、request payload、persisted entry/instance ID 或 raw local queue cause。未知远端 code 不进入日志；wrapped cause 仅用于 `errors.As` / `errors.Is`，不能被主进程直接记录。
- `agent enrolled` 只在 bound、sync token 非空且凭据 durable persistence 成功后记录，并且只含 allowlisted status/binding status，不记录 MonitoringInstance ID。runtime start/stop 的 `server_url` 只记录 parsed scheme + host origin，不能记录 userinfo、path、query 或 fragment。

### 4. Validation & Error Matrix

| 条件 | Queue action | Runtime action |
| --- | --- | --- |
| stale authority / malformed local carrier | 一次 batch delete 成功后按 reason 聚合记 discard | 继续 flush |
| stale ID 与 retained current ID 冲突 | 不做 stale ID-wide delete、不记 discard | 跳过 stale carrier，继续 current entry |
| HTTP `400` + `ErrorCodeInvalidJSON` / `ErrorCodeInvalidRequest` | mark → delete 成功后记 discard；计入本轮 backlog 上限 | backlog lane 可继续；current discard 后结束本轮 |
| `401` / `404` / `409` / `405` 或其他非 `429` 4xx | mark，保留 entry | 返回 sanitized terminal error |
| backlog `429` / 其他 5xx / transport failure | mark，保留 entry | 停止本轮 backlog lane，但仍尝试 exact current；下一 tick 从同一 backlog head retry |
| current `429` / 其他 5xx / transport failure | mark，保留 entry | 结束本轮；下一 tick 作为 backfilled backlog retry |
| typed remote status 0、2xx 或 3xx error | mark，保留 entry | 作为不明确 remote failure 下一 tick retry，不做推测性 delete |
| 非 400 status 携带 `invalid_json` / `invalid_request` code | 以 status 为准，不 delete | 4xx terminal 或 5xx retry |
| durable Mark/Delete 失败 | 保留可恢复状态 | 返回 local queue error |

### 5. Good / Base / Bad Cases

- Good：旧 instance/token/fingerprint 项位于队头；agent 删除它并在同一次 flush 送达当前心跳。
- Good：五万条同一 stale reason 的历史积压只触发一次 atomic `DeleteMany` 与一条聚合日志，不产生 O(n²) rewrite/fsync 或日志洪泛。
- Good：legacy queue 含多个 retained current facts；FileStore 暴露稳定唯一 ID，backlog lane 仍 FIFO；每两个旧 entry 后本轮 exact current 可以有界越过剩余 backlog，后续轮次继续排空旧项。
- Good：含 command result/IP-quality report 的当前 request 首次 Enqueue 写盘失败；payload 留在内存，修复本地故障后同 Runtime 再次 Run 能将其 durable enqueue 并发送。
- Base：backlog head 暂时 503；队头保留，停止本轮 backlog lane，但本轮 current 仍发送；下一 tick 该旧项继续作为 FIFO head retry。
- Bad：按 carrier `SyncBatchID` 识别 current；本地 ID collision suffix 不会改 carrier ID，会把旧项误标 live 或把 fresh 误标 backfill。
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
- `TestRuntimeDoesNotBulkDeleteCurrentAuthorityEntrySharingStaleIdentifier`：persisted ID 冲突不能误删 retained current authority。
- `TestRuntimePreservesRetainedCurrentFactsWithPersistedDuplicateIDs`、`TestFileStoreEntryIDAddsSuffixOnHeartbeatBatchCollision`、`TestFileStoreNormalizesPersistedDuplicateIDsBeforeDelete`：生产 FileStore 中 duplicate entry ID retained facts 可独立 ack，首个 Delete 不丢后续 fact；不把 entry suffix 当成新的 center batch identity。
- `TestFileStoreNormalizesLargePersistedDuplicateIDBacklog`、`TestFileStoreEntryIDScalesAcrossDenseHeartbeatBatchSuffixes`：默认容量量级的 duplicate-ID 恢复与密集 suffix 分配不退化成 O(n²) 扫描。
- `TestRuntimeRetainsCommandResultUntilQueueEnqueueSucceeds`、`TestRuntimeRetainsIPQualityReportUntilQueueEnqueueSucceeds`：辅助 payload 只在 durable transfer 后从 runtime buffer ack。
- `TestFileStoreEnqueueFailsWithoutMutatingQueueWhenNewestEntryExceedsMaxBytes`、`TestFileStorePrunesLargeOversizedPersistedBacklogInLinearPass`、`TestFileStorePruneDoesNotWriteAnEmptyQueueBeyondMaxBytes`：newest entry 无法满足 MaxBytes 时 fail closed；legacy/hostile 超限积压线性裁剪；即使空数组编码也超过极小 cap 时不改写原文件。
- `TestRuntimeDiscardsExplicitInvalidQueueEntryAndContinues`：仅 HTTP 400 + 两个稳定 invalid code 可 discard。
- `TestRuntimeCurrentAuthorityPermanentRemoteErrorsAreTerminalAndSanitized`：永久 4xx 保留 entry、只返回一次脱敏 terminal evidence，并覆盖 status/code precedence。
- `TestRuntimeLogsDiscardOnlyAfterQueueDeleteSucceeds`：stale/poison delete 失败时没有虚假 discard 日志。
- `TestRuntimeTransientQueueFailuresRemainRetryableAndBackfilled`：status 0/2xx/3xx、429/5xx/transport 保留、重试与 secret-bearing cause 不泄露。
- `TestRuntimeBoundsReplayBeforeCurrentDurableRequest`、`TestRuntimeReplaysBacklogFIFOAcrossFreshInterleaving`：fresh 前最多两个旧尝试，且 backlog lane 跨轮保持 FIFO 并最终排空。
- `TestRuntimeRetryableBacklogHeadDoesNotBlockCurrentDurableRequest`、`TestRuntimeCurrentRequestRetryRemainsDurableAndBackfilled`：旧 retry 只阻断 backlog lane；失败 fresh durable 留存并在下一轮变为 backfill。
- `TestRuntimeCurrentResponsePlanWinsAfterBoundedReplay`：durable delete 后应用响应，且 current plan 最后生效。
- `TestRuntimeBoundedReplayPreservesDurableAuxiliaryPayloads`、`TestRuntimeLocalEntryIDControlsCurrentAndBackfilledOnBatchIDCollision`：辅助载荷不丢，本地 entry ID 独立决定 live/backfill。
- `TestFileStoreDeleteManyRechecksCancellationBeforeMutation`、`TestFileStoreRechecksCancellationAfterLockAcrossOperations`：锁前检查后发生的取消也不能继续 durable mutation。
- `TestRuntimeTreatsParentCancellationAcrossBoundariesAsCleanShutdown`：fingerprint/credential/enrollment/non-queue client 边界的 parent cancellation 都干净退出。
- `TestRuntimeSanitizesRemoteFailuresOutsideDurableQueue`、`TestRuntimeDoesNotExposePersistedQueueIdentifiersOrLocalCauses`：覆盖 enrollment/non-queue sync、queue 标识和 local cause 的主进程可见 Error/log 边界。
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
   - Byte pruning must calculate serialized entry sizes in a linear pass; an oversized legacy/hostile queue must not repeatedly marshal every remaining tail once per evicted entry.
   - If the newly enqueued entry cannot fit the configured byte cap even by itself, `Enqueue` must return a local durability error and leave the previously persisted queue unchanged; it must never report success after pruning away the new entry or write a file larger than the cap.
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
