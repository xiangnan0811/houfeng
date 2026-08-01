# 目录结构

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风 / Houfeng Fleet Control Plane 当前后端代码组织围绕 **1 个 Go center + 1 个 Postgres + N 个 systemd Go agent** 这一拓扑。仓库严格区分：

- **入口（`cmd/`）**：单个二进制的 `main.go` + 装配代码，不放业务逻辑。
- **center 业务实现（`internal/center/`）**：按领域拆子包；HTTP 路由、Postgres 仓库、incident 判定、Telegram 通知、retention 等都各占一个子包。
- **agent 业务实现（`agent/`）**：thin agent，只做采集 / 缓冲 / 同步 / 应用计划。
- **跨进程契约（`internal/contracts/`）**：center 与 agent **同时依赖**的请求/响应类型、错误码、路径常量。
- **持久化 schema（`db/migrations/`）**：手写 SQL 迁移，启动时通过 `embed.FS` 嵌入并应用。
- **前端 SPA（`web/`）**：本文件不展开，详见 `.trellis/spec/web/`。

代码搜索友好性是核心约束：所有 wiring 在 `cmd/houfeng-center/bootstrap.go` 内显式拼装，禁止隐式依赖注入。

---

## Directory Layout

```
.
├── Makefile                   # fmt-go / vet-go / test-go / verify-go / verify-web 等
├── cmd/
│   ├── houfeng-center/        # center 二进制入口
│   │   ├── main.go
│   │   ├── bootstrap.go       # 装配 pgxpool、仓库、notifier、router、worker
│   │   └── bootstrap_test.go
│   └── houfeng-agent/         # agent 二进制入口
│       └── main.go
├── agent/                     # agent 业务子包（外部可被 cmd/houfeng-agent 引用）
│   ├── config/                # env 装载
│   ├── token/                 # 文件 token 源
│   ├── fingerprint/           # 主机指纹
│   ├── enroll/                # 首次 enroll
│   ├── hostsample/            # 主机采样
│   ├── probe/                 # tcp / http / tls 探针执行
│   ├── syncqueue/             # 单文件 JSON 缓冲队列（纯 Go，无嵌入式 DB）
│   └── runtime/               # 主循环：collect → buffer → sync → apply plan
├── internal/
│   ├── center/                # center 业务，仓库内独占
│   │   ├── app/               # HTTP server + Worker.Run(ctx) 进程生命周期
│   │   ├── config/            # CenterConfig / env 装载
│   │   ├── http/              # router.go + middleware.go
│   │   │   ├── router.go      # 用 RouterOptions 显式 wire 每个 handler
│   │   │   └── handlers/      # 一文件一资源，详见下表
│   │   ├── store/             # Postgres 仓库（一文件一 aggregate）
│   │   │   └── migrate/       # store/migrate.Apply：启动时应用 embed 的迁移
│   │   ├── auth/              # 用户、会话、密码、cookie、cleanup worker
│   │   ├── enrollment/        # token 颁发、指纹绑定、binding 状态机
│   │   ├── incidents/         # incident 判定、debounce、SettingsBackedService
│   │   ├── notify/            # Telegram 通知；被 incidents 包装为 SettingsAware
│   │   ├── syncing/           # /api/agent/sync 的批量 ingest 管线
│   │   ├── retention/         # 按表 retention worker
│   │   ├── settings/          # CenterSettings 模型 + Repository
│   │   ├── providers/         # Asset Ledger 服务商主数据 + Repository 接口
│   │   ├── vpsassets/         # Asset Ledger VPS 资产 + Repository 接口
│   │   ├── subscriptions/     # Asset Ledger VPS 订阅 + Repository 接口
│   │   ├── assetlinks/        # Asset Ledger VPS ↔ MonitoringInstance 关联 + 摘要查询接口
│   │   ├── renewals/          # Asset Ledger 续费 / 价格 / IP / 规格历史 + VPS timeline 接口
│   │   ├── importing/         # Asset Ledger JSON dry-run/import 解析、校验、报告与编排
│   │   ├── targets/           # Target / ProbeItem 领域类型与频率档枚举
│   │   ├── monitoringinstances/             # MonitoringInstance 领域类型 + Repository 接口
│   │   ├── agentplan/         # 下发给 agent 的 plan 类型
│   │   ├── runtimefacts/      # 运行时事实领域类型
│   │   ├── observations/      # 原始观测的 service / 校验
│   │   └── ids/               # ID 生成（monitoring_instance_id / target_id 等）
│   └── contracts/
│       └── agentapi/          # ★ center 与 agent 共享的契约：路径、类型、错误码
├── db/
│   └── migrations/            # 迁移文件 + embed.go（embed.FS），当前最大 0021_create_asset_histories.sql
├── docs/                      # 设计基线 / 部署 / 验证
├── scripts/                   # verify.sh 等
├── bin/                       # build 产物（go build 输出）
└── web/                       # React 19 + Vite SPA（不展开）
```

> **`internal/center/` 子包以 `ls` 实际结果为准**。当前实际存在但 `CLAUDE.md` 未提及的子包（必须在文档中考虑到）：`auth/`（用户/会话/密码/cookie/cleanup）。详见后文 *与 CLAUDE.md 的差异* 一节。

---

## Module Organization

### `cmd/`

每个二进制一个目录。`main.go` 仅做：解析配置 / flag → 调用同包内 `bootstrap*` 或内部领域包 → 处理信号。`bootstrap.go` 把所有依赖显式注入（参见 `cmd/houfeng-center/bootstrap.go:58-147`，`bootstrapCenter` 函数），并通过 `bootstrapDeps` 暴露可替换的工厂以便测试（见 `bootstrap_test.go`）。**禁止把业务逻辑写进 `cmd/`**。

`cmd/houfeng-import-vps-json` 是当前第一个运维型 CLI：它只负责 flag、文件读取、数据库连接 / migration、事务与报告输出；JSON 结构、dry-run 校验、导入编排和报告模型都放在 `internal/center/importing/`。后续新增 CLI 时沿用这个边界，不要在 `cmd/<binary>/main.go` 里直接堆业务规则。

### `internal/center/<domain>/`

按领域拆包。每个子包遵循以下惯例：

- `types.go`：领域类型与接口（`Repository`、`Service`、领域错误）
- `service.go`：业务行为
- `<file>_test.go`：与被测文件并列
- 包对外 API 通过包级函数 / 构造器暴露，例如 `incidents.NewSettingsBackedService(...)`、`auth.New(...)`、`enrollment.NewService(...)`

新增一个领域子包前先确认 `internal/center/` 下没有合适归属。**不要为每个新需求都建子包**；如果只是给 MonitoringInstance 加一个查询函数，就放进 `internal/center/monitoringinstances/` 与 `internal/center/store/monitoring_instances.go`。

### `internal/center/http/handlers/`

**一文件一资源**。当前实际文件（`ls internal/center/http/handlers/` 结果）：

| 文件 | 资源 |
|------|------|
| `agent.go` | `/api/agent/enroll`、`/api/agent/sync` |
| `auth.go` | `/api/auth/login`、`/logout`、`/me`、`/password` |
| `dashboard.go` | `/api/dashboard` |
| `events.go` | `/api/events` |
| `health.go` | `/api/healthz` |
| `incidents.go` | `/api/incidents` |
| `metadata.go` | 元数据查询辅助 |
| `monitoring_instance_onboarding.go` | 监控实例接入与 binding 操作 |
| `monitoring_instances.go` | `/api/monitoring-instances`、`/api/monitoring-instances/{id}` |
| `providers.go` | `/api/providers`、`/api/providers/{provider_id}` |
| `subscriptions.go` | `/api/subscriptions`、`/api/subscriptions/{subscription_id}` |
| `vps.go` | `/api/vps`、`/api/vps/{vps_id}`、`/api/vps/{vps_id}/timeline` |
| `asset_links.go` | `/api/vps/{vps_id}/monitoring-instances`、`link-monitoring-instance`、`unlink-monitoring-instance`、`/api/monitoring-instances/{monitoring_instance_id}/vps` |
| `runtime_controls.go` | 监控实例 / 目标 runtime 控制（含维护开关） |
| `runtime_facts.go` | 监控实例 / 目标运行时事实 |
| `settings.go` | `/api/settings` |
| `spa.go` | 静态 SPA fallback（`HOUFENG_WEB_DIST_DIR`） |
| `targets.go` | `/api/targets` |
| `json.go` | `writeJSON` / `decodeJSON` / `writeError` 共用辅助（详见 `handlers/json.go:10-31`） |

> 注：`CLAUDE.md` 列出的 handler 清单未包含 `auth.go` 与 `metadata.go`，但代码确实存在（`router.go:35-69` 注册了 `/api/auth/*`）。这是已知文档差距。

### `internal/center/store/`

**一文件一 aggregate** 的 Postgres 仓库，全部使用 `pgxpool.Pool`。当前真实文件：

```
agent_plan.go          dashboard.go       incidents.go      monitoring_instances.go
observations.go        postgres.go        probe_metadata.go providers.go
renewal_decisions.go   retention.go       runtime_facts.go  sessions.go
settings.go            subscriptions.go   sync_batches.go   targets.go
users.go               vps_assets.go      vps_monitoring_instance_links.go
migrate/
```

每个文件提供一个 `NewPostgres<Aggregate>Repository(*pgxpool.Pool)` 构造器（参见 `store/monitoring.go:34-36`）。`postgres.go` 提供共享的 `OpenPostgres` 入口（`store/postgres.go:11-31`）。

### `agent/<subpkg>/`

agent 子包扁平化拆分，每个职责一个包：

- `config/`、`token/`、`fingerprint/`、`enroll/`、`hostsample/`、`probe/`、`containersample/`、`exec/`、`syncqueue/`、`runtime/`
- `runtime/` 是装配中心，把其余子包按 `collect → buffer → sync → apply plan` 串起来
- agent 必须保持"thin"：不接受任意脚本 / 用户自定义参数、不本地评估规则；当前仅允许 `exec/` 中编译期白名单命令，以及 `containersample/` 对本机 Docker CLI 的 best-effort 事实采样（Docker 不存在或 daemon 不可用时静默跳过）。`exec.Lookup` 必须返回参数副本，避免调用方篡改编译期白名单；`exec.Run` 必须使用 `exec.CommandContext` 而不是 shell；Docker 采样只能调用固定参数形状的 `docker ps --all --no-trunc --format ...` 与 `docker stats --no-stream --format ...`，不得扩展为 Docker 控制、编排或容器生命周期操作。

#### Scenario: agent local state upgrade compatibility

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

#### Scenario: agent command / Docker 边界

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
   - sensitive command POST 缺少 `confirmed_sensitive:true` -> center 返回 400，不进入 repository write。
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

#### Scenario: `agent/hostsample` 平台采集边界

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

#### Scenario: Docker Compose center deployment and image release contract

1. **Scope / Trigger**
   - 触发：修改 `Dockerfile`、`compose.yaml`、`docs/deploy/compose.env.example`、Docker 部署文档、Release Please / Docker GitHub Actions、Docker 镜像命名 / 发布流程、或 center/web/PostgreSQL 部署边界。
   - 目标：Compose 只提供 center + built web + PostgreSQL 的快速部署路径，不改变 1 center + 1 PostgreSQL + N systemd agents 的产品拓扑，也不把 agent 变成容器工作负载；镜像发布必须走 feature PR → `main` → Release Please release PR → GitHub Release → Docker Hub 的 release-only 链路。

2. **Signatures**
   - Image definition: repository root `Dockerfile` builds a single project image containing `houfeng-center`, a small runtime entrypoint, and baked `web/dist`.
   - Runtime user: repository root `Dockerfile` creates the `houfeng` system user, owns `/app/web/dist` and `/var/log/houfeng`, and sets `USER houfeng:houfeng` in the runtime stage.
   - Runtime entrypoint: `scripts/docker-entrypoint.sh` assembles `HOUFENG_DATABASE_URL` and validates required startup secrets; it does not perform runtime privilege dropping.
   - Published Compose file: `compose.yaml` service set is exactly `houfeng` + `db` for MVP.
   - Project image reference: `houfeng.image = linnea7171/houfeng:latest`; release publishing produces `linnea7171/houfeng:vX.Y.Z`, `linnea7171/houfeng:X.Y.Z`, and release-controlled `linnea7171/houfeng:latest`.
   - Release automation: `.github/workflows/release-please.yml` runs on `push` to `main`, uses `googleapis/release-please-action`, and reads `release-please-config.json` plus `.release-please-manifest.json`.
   - Release config: root package `.` uses `release-type: simple`, `include-v-in-tag: true`, and `CHANGELOG.md` maintained by Release Please.
   - Docker Actions majors: Docker workflows use Node 24-compatible majors (`docker/setup-buildx-action@v4`, `docker/build-push-action@v7`, `docker/login-action@v4`, `docker/metadata-action@v6`) rather than relying on `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.
   - Runtime web path: `HOUFENG_WEB_DIST_DIR=/app/web/dist` inside the project image.
   - Runtime HTTP default: project image and Compose set `HOUFENG_HTTP_ADDR=:16001`; default port mapping is `127.0.0.1:16001:16001`, with host port override allowed.
   - Local Compose database identities: `POSTGRES_BOOTSTRAP_USER` initializes the PostgreSQL OID-10 bootstrap superuser, while distinct `HOUFENG_DATABASE_USER` identifies the application login. Unless an explicit `HOUFENG_DATABASE_URL` is already set, the project image entrypoint assembles `postgres://<application-user>:<application-password>@db:5432/?dbname=<database>&sslmode=disable` from the application identity, database name, and mounted password file. It percent-encodes each fallback component byte-for-byte, rejects ASCII control bytes before child execution, and gives the password file precedence over the password environment value. An explicit URL bypasses fallback assembly unchanged and remains subject to center config/TLS validation.
   - Production database TLS guard: `HOUFENG_DATABASE_REQUIRE_TLS=true` makes center startup reject missing `sslmode` and `sslmode=disable|allow|prefer`; accepted modes are `require`、`verify-ca`、`verify-full`.
   - Password hash cost tuning: `HOUFENG_PASSWORD_BCRYPT_COST` configures bcrypt cost for newly seeded/changed passwords and must stay within Go bcrypt `MinCost..MaxCost`.
   - Center log file config: deployed center uses `HOUFENG_LOG_FILE=/var/log/houfeng/center.log`; unset keeps stdout-only local behavior.
   - PostgreSQL data path: default Compose bind mount is `./data/postgres:/var/lib/postgresql/data` so operators can migrate the directory directly.
   - Center log path: default Compose log mount is the named volume `houfeng_logs:/var/log/houfeng`, initialized from the image-owned directory so the non-root container can open `center.log`.
   - Minimal env template: `docs/deploy/compose.env.example` copied to untracked `docs/deploy/compose.env`; it declares the two distinct database principal names plus host paths for separate untracked bootstrap/application password files.
   - Database passwords are service-scoped Docker secrets backed by ignored mode-0600 files. The bootstrap password is mounted only into `db`; the application password is mounted into `db` for post-provisioning role creation and into `houfeng` for its application connection. The tracked `compose.yaml` avoids password values and database URL assignments.

3. **Contracts**
   - Published `compose.yaml` must not contain a local project `build:` block or password values/database URL assignments, and quick-start docs must not instruct `docker compose up --build`; operators should be able to run the published image directly.
   - The documented Compose quick-start must run `scripts/compose-up.sh`. That script uses shell fail-stop, starts only `db`, awaits both `pg_isready` and a successful `SELECT 1` in the configured target database under the bootstrap identity, rejects a bootstrap/application identity collision, runs the pre-R1 provisioning SQL, creates/updates the constrained application role only afterward, and invokes `docker compose ... up -d houfeng` last. An existing application role must have no `pg_auth_members` edge in either direction, which excludes every direct or recursive membership relation; `NOINHERIT` alone is insufficient because `SET ROLE` remains possible. Membership drift is rejected without cleanup before committed database-owner transfer or Houfeng startup. Readiness, provisioning, or role-setup failure must make the script nonzero without requesting Houfeng startup.
   - The root `Dockerfile` is the image build definition for release-only GitHub Actions publishing, not the default Compose quick-start execution path.
   - Docker image and agent asset publishing must be deliberate release output: `release.published` and maintainer `workflow_dispatch` may publish; `main` push and pull request events must not publish images, upload release assets, or access Docker Hub credentials.
   - Feature PR merges to `main` should not publish Docker images directly; they trigger Release Please to open/update a release PR.
   - The release PR must pass normal CI before merge. Merging it publishes the GitHub Release, and only that `release.published` event updates Docker Hub release tags and `latest`.
   - Release Please requires `RELEASE_PLEASE_TOKEN`; Docker publishing requires `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`. These are repository secrets only and must not appear in docs as concrete values, compose examples, or committed env files.
   - Do not add a separate `main`-push Docker publishing workflow, `pull_request` Docker publishing, or a workaround env such as `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24` when an official Node 24 action major exists.
   - `houfeng` runs only `houfeng-center`; it does not start Vite, Nginx, Caddy, Postgres, or an agent inside the project container.
   - The project container must run as `houfeng:houfeng` by default through the Dockerfile `USER` instruction. Do not rely on `gosu`, `su-exec`, `id -u`, or root entrypoint chown logic for normal startup.
   - `db` uses the official PostgreSQL image with a user-migratable host directory mounted at `/var/lib/postgresql/data`; it is initialized under the bootstrap identity, not the application login. Center applies embedded migrations at startup through the separately provisioned application identity.
   - Compose may bind Houfeng to host loopback for an operator-managed reverse proxy upstream; TLS termination stays outside the app container/Compose MVP.
   - `HOUFENG_PUBLIC_BASE_URL` may be empty for first login, but must be set to an externally reachable absolute `http(s)` URL before one-command agent onboarding.
   - `HOUFENG_LOG_FILE` is center-only. When set, the center must tee structured `slog` output to stdout and the configured file; startup fails if the file cannot be opened.
   - Quick-start env stays minimal: database identity names/password-file paths, initial admin username/password, session HMAC key, and visible `HOUFENG_PUBLIC_BASE_URL`; do not add Telegram, agent env, retention/session/incident tuning, or release automation secrets to this template.
   - Do not add a log bind mount unless the center app actually writes files there; stdout/stderr-only logging must not be documented as sufficient long-term behavior for deployed center troubleshooting.
   - Agents remain Linux/systemd host installs through center-generated onboarding commands. Do not add an `agent` Compose service, Docker agent deployment docs, Docker socket mounts, host PID/network namespace requirements, Kubernetes manifests, or agent file logging under this contract.

4. **Validation & Error Matrix**

   | Condition | Expected behavior |
   | --- | --- |
   | `compose.yaml` contains `houfeng.build` or quick-start says `up --build` | Reject in review; published Compose must pull/use `linnea7171/houfeng:latest` from release publishing |
   | Docker image workflow publishes on `push` to `main` or `pull_request` | Reject in review; image publication must be release/manual only |
   | Release Please workflow is missing or not triggered by `push` to `main` | Reject in review; feature PR merge must create/update a release PR |
   | Release Please workflow can publish Docker images or use Docker Hub credentials | Reject in review; it only manages release PRs/GitHub Releases |
   | Docker workflows still use Node 20 Docker action majors (`setup-buildx@v3`, `build-push@v6`, `login@v3`, `metadata@v5`) | Reject in review; upgrade to the official Node 24 majors |
   | Docker image workflow updates `latest` during manual rebuilds | Reject in review unless deliberately changed; `latest` is controlled by normal release publication |
   | `compose.yaml` uses project service name `center` | Reject in review; service name must be `houfeng` |
   | `compose.yaml` keeps project container port `8080` as the Docker default | Reject in review; Docker/Compose default must be `16001` inside and outside |
   | `compose.yaml` adds an `agent` service | Reject in review; agents are host systemd services |
   | `compose.yaml` maps `/var/log/houfeng` but the center image does not set `HOUFENG_LOG_FILE` | Reject in review; the volume must back a real file-writing path |
   | `compose.yaml` bind-mounts `./data/logs:/var/log/houfeng` | Reject in review; first startup can create a root-owned host directory that non-root center cannot write |
   | Dockerfile runtime stage lacks `USER houfeng:houfeng` or installs `gosu` | Reject in review; runtime must be non-root by default without privilege-drop helper |
   | `HOUFENG_DATABASE_REQUIRE_TLS=true` with missing/weak `sslmode` | Center config load fails before serving traffic |
   | `docs/deploy/compose.env` is committed | Reject in review; only `docs/deploy/compose.env.example` is tracked |
   | `compose.yaml` contains `HOUFENG_DATABASE_URL:`, `POSTGRES_PASSWORD:`, or `HOUFENG_INITIAL_PASSWORD:` assignment lines | Reject in review; secrets must come from the env file and the tracked Compose file must avoid password-like assignments |
   | Bootstrap/application principals are equal | Fail before provisioning or application startup; the application login must never be OID-10 bootstrap authority |
   | Existing application role has direct/recursive membership in either direction | Role transaction rolls back without membership cleanup or committed database-owner transfer; Houfeng is not started |
   | Database readiness or pre-R1 provisioning fails | `scripts/compose-up.sh` exits nonzero and never invokes `up -d houfeng` |
   | Bootstrap/application password file is missing or empty | Compose validation, PostgreSQL initialization, role setup, or project entrypoint fails before serving traffic |
   | Fallback DSN component contains URI-reserved printable characters | Percent-encode the exact bytes and execute the child with a parseable URL; do not require URL-safe passwords |
   | Fallback DSN component contains an ASCII control byte | Entrypoint exits nonzero before child execution |
   | Missing `HOUFENG_INITIAL_PASSWORD` in env file | The project image entrypoint fails before serving traffic |
   | Empty `HOUFENG_PUBLIC_BASE_URL` | Center can start and login works; install-command generation remains unavailable until configured |
   | Internal Compose URL used as public base URL | Reject in docs/review unless target agents can actually reach it; production commands need the external browser/agent URL |
   | Public deployment exposes plain HTTP directly | Reject in docs/review; require operator-managed HTTPS reverse proxy |

5. **Good / Base / Bad Cases**
   - Good: operator copies `docs/deploy/compose.env.example`, creates the two ignored mode-0600 password files, runs `scripts/compose-up.sh docs/deploy/compose.env`, and accesses Houfeng on `127.0.0.1:16001` through a local reverse proxy upstream only after readiness and provisioning succeed.
   - Good: first Compose startup initializes the `houfeng_logs` named volume from image-owned `/var/log/houfeng`, and the center writes `/var/log/houfeng/center.log` while running as the non-root `houfeng` user.
   - Good: operator collects recent `docker compose logs houfeng` output and, when file logs are needed, reads `/var/log/houfeng/center.log` from a temporary container mounting the `houfeng_logs` volume.
   - Good: operator backs up or migrates `./data/postgres/` as an ordinary host directory before moving the deployment.
   - Good: feature work lands through a branch PR; the merge to `main` runs Release Please, opens/updates a release PR, the release PR passes CI and is merged, the resulting GitHub Release fires release publishing, Docker Hub receives `vX.Y.Z`, `X.Y.Z`, and release-controlled `latest`, and the GitHub Release receives `houfeng-agent_vX.Y.Z_linux_amd64`, `houfeng-agent_vX.Y.Z_linux_arm64`, and `sha256sums.txt`.
   - Good: release-only automation builds the root `Dockerfile` on published GitHub releases, tags/pushes `linnea7171/houfeng:vX.Y.Z`, `linnea7171/houfeng:X.Y.Z`, and release-controlled `latest`, uploads the installer-required agent assets built by `make build-agent-release VERSION=vX.Y.Z`, and leaves Compose without local `build:`.
   - Base: local login works with empty `HOUFENG_PUBLIC_BASE_URL`; before onboarding real agents, operator sets the external HTTPS URL and recreates the `houfeng` container.
   - Bad: using `build: .` in `compose.yaml` makes deployment depend on local Go/Node source builds and breaks the intended project-image distribution path.
   - Bad: adding a `/var/log/houfeng` bind mount while the app does not write files there creates misleading troubleshooting expectations.
   - Bad: adding an agent container with `/var/run/docker.sock` or broad host mounts changes the thin-agent security boundary and is not this deployment model.

6. **Tests Required**
   - `docker compose --env-file docs/deploy/compose.env.example -f compose.yaml config --quiet` must pass.
   - Static check must confirm `compose.yaml` has no `HOUFENG_DATABASE_URL:`, `POSTGRES_PASSWORD:`, or `HOUFENG_INITIAL_PASSWORD:` assignment lines, has no `build:` for `houfeng`, has no `agent` service, references `linnea7171/houfeng:latest`, maps `127.0.0.1:${HOUFENG_HOST_PORT:-16001}:16001`, bind-mounts `./data/postgres`, mounts `houfeng_logs:/var/log/houfeng`, declares the `houfeng_logs` named volume, and wires `depends_on.condition: service_healthy` for PostgreSQL.
   - Deployment behavior tests must run the actual `scripts/compose-up.sh` against a fake Docker command and prove exact ordering `db start -> readiness -> provisioning -> application role -> Houfeng start`; readiness and provisioning failures must both produce nonzero status with no Houfeng-start call. Focused PostgreSQL 16.12 evidence must also construct an existing-role membership edge and prove role provisioning fails, database ownership is unchanged, and the launcher never requests Houfeng startup.
   - Entrypoint behavior tests must execute the actual script as a subprocess with a controlled fake child and table-drive missing user/name/password, nonexistent/unreadable/empty secret files, file precedence, explicit-URL bypass, reserved-character encoding, malformed control-byte input, and zero child execution on every failure. Static checks must prove `POSTGRES_BOOTSTRAP_USER` and `HOUFENG_DATABASE_USER` are nonempty/distinct in the example and Compose maps only the bootstrap identity to `POSTGRES_USER`.
   - Static check must confirm the runtime image sets `USER houfeng:houfeng`, does not install `gosu`, and `scripts/docker-entrypoint.sh` contains no runtime privilege-drop branch.
   - `git diff --check` must pass after Docker/docs edits.
   - Search touched docs/configs for stale `center` service naming, `127.0.0.1:8080`, Docker `:8080` defaults, `postgres-data` named-volume wording, misleading log mount wording, and stale `--build` / local-build quick-start wording before review.
   - For Dockerfile changes, run a lightweight Dockerfile validation or image build when Docker is available; if unavailable, state that explicitly and rely on review plus existing `make verify-go` / `make verify-web` gates.
   - For Docker / agent asset publishing workflow changes, statically verify `.github/workflows/publish-images.yml` has no `push`/`pull_request` trigger, has `release.published` and `workflow_dispatch`, targets `docker.io/linnea7171/houfeng`, uses the root `Dockerfile`, builds agent assets with `make build-agent-release VERSION=vX.Y.Z`, uploads only `houfeng-agent_vX.Y.Z_linux_amd64`, `houfeng-agent_vX.Y.Z_linux_arm64`, and `sha256sums.txt` to the matching GitHub Release, grants `contents: write` only where needed for release asset upload, and references only GitHub Secrets for Docker Hub credentials.
   - For Release Please changes, statically verify `.github/workflows/release-please.yml` runs on `push` to `main`, has `contents: write` and `pull-requests: write`, uses `secrets.RELEASE_PLEASE_TOKEN`, points to `release-please-config.json` and `.release-please-manifest.json`, and does not access Docker Hub credentials.
   - For GitHub Actions maintenance, run `actionlint` when available and search Docker workflows for stale `docker/setup-buildx-action@v3`, `docker/build-push-action@v6`, `docker/login-action@v3`, `docker/metadata-action@v5`, and `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.

7. **Wrong vs Correct**

```yaml
# 错误：发布给 operator 的 Compose 默认从本地源码 build，且服务名不是 houfeng。
services:
  center:
    build: .
```

```yaml
# 正确：发布 Compose 默认使用项目镜像，服务名为 houfeng。
services:
  houfeng:
    image: linnea7171/houfeng:latest
```

```yaml
# 错误：Docker 默认仍监听 / 暴露 8080。
services:
  houfeng:
    environment:
      HOUFENG_HTTP_ADDR: ":8080"
    ports:
      - "127.0.0.1:8080:8080"
```

```yaml
# 正确：Docker/Compose 默认内外都是 16001，host 端口可覆盖。
services:
  houfeng:
    environment:
      HOUFENG_HTTP_ADDR: ":16001"
    ports:
      - "127.0.0.1:${HOUFENG_HOST_PORT:-16001}:16001"
```

```yaml
# 错误：把 agent 做成容器服务并要求宿主机高权限挂载。
services:
  agent:
    image: linnea7171/houfeng-agent:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
```

```text
正确：agent 继续通过 MonitoringInstance onboarding 生成的一键安装命令安装到真实 Linux/systemd 主机。
```

### `internal/contracts/agentapi/`

center 与 agent 同时引用的唯一契约包。内容：

- `routes.go`：`EnrollPath = "/api/agent/enroll"`、`SyncPath = "/api/agent/sync"`、`InstallScriptPath = "/api/agent/install.sh"`
- `types.go`：请求 / 响应 DTO、`BindingStatus*` / `ErrorCode*` / `ProbeKind*` / `ProbeError*` 常量

> 这是**唯一**允许同时被 `cmd/houfeng-center` / `internal/center/http/handlers` 与 `cmd/houfeng-agent` / `agent/runtime` 引用的包。新增 agent ↔ center 字段时，先改这里，两侧再各自适配。**不要把 DTO 定义在 handler 包或 runtime 包内自己重复一份**。

#### Scenario: Agent one-command install contract

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

---

## Naming Conventions

- 包名：全小写、单词内部不用下划线。一个领域一个子包（`incidents`、`enrollment`、`runtimefacts`）。
- 文件名：`snake_case.go`，与其内最重要的类型 / 资源对齐（`runtime_facts.go`、`monitoring_instance_onboarding.go`）。
- 测试文件：`<file>_test.go`，与被测文件**同目录同包**；端到端测试加 `_e2e_test.go` 后缀（参考 `internal/center/http/auth_e2e_test.go`）。
- 仓库类型：`Postgres<Aggregate>Repository`，构造器 `NewPostgres<Aggregate>Repository`。
- HTTP handler 工厂：`handlers.<Resource>(repoOrSvc)` 或 `handlers.<Resource><Action>(...)`，统一返回 `http.Handler`，由 `bootstrap.go` 注入到 `RouterOptions`。
- 迁移文件：`<NNNN>_<verb>_<scope>.sql`，序号 4 位起步、动词放第一个（`add`、`normalize`、`create`），见 `db/migrations/0001_initial_schema.sql` … `0021_create_asset_histories.sql`。

---

## 哪里放新代码

| 变更类型 | 落点 |
|----------|------|
| 新增 HTTP endpoint | 1) `internal/center/http/handlers/<resource>.go` 内增加工厂；2) 在 `internal/center/http/router.go` 的 `RouterOptions` 加字段并 mux 注册；3) 在 `cmd/houfeng-center/bootstrap.go` 的 `bootstrapCenter` 显式构造并塞进 `RouterOptions`；4) 同目录 `<resource>_test.go` 增 table-driven 测试 |
| 新持久化字段 / 表 | 1) `db/migrations/<next-NNNN>_<verb>_<scope>.sql` 写原生 SQL；2) 更新 `internal/center/store/<aggregate>.go` 仓库的 select / insert / update；3) 更新对应 `internal/center/<domain>/types.go` |
| 新 agent ↔ center 字段 | 1) `internal/contracts/agentapi/types.go` 改 DTO；2) center 端在 `internal/center/syncing/` 或对应 handler 处理；3) agent 端在 `agent/runtime/` 或采集子包消费；**严禁两侧各自定义同名结构** |
| 新领域行为 | 优先放进既有 `internal/center/<domain>/`；只有当确实属于新领域时才新增子包 |
| 新运维型 CLI / import 命令 | `cmd/<binary>/main.go` 只放 flag、I/O、数据库连接和调用；解析、校验、dry-run 报告、写入编排放到 `internal/center/<domain>/` 或专用领域包（当前 import 落在 `internal/center/importing/`） |
| agent 新增采集项 | 在 `agent/hostsample/` 或 `agent/probe/` 内扩展，并通过 `agent/runtime/` 串接；不要往 agent 里塞规则判定 |

---

## Examples

以下是当前代码库内"组织到位"的真实参考点：

- **MonitoringInstance 资源完整一条线**：`internal/center/http/handlers/monitoring_instances.go`（handler）+ `internal/center/http/handlers/monitoring_instances_test.go`（table-driven 测试）+ `internal/center/store/monitoring_instances.go`（仓库）+ `internal/center/monitoringinstances/`（领域类型）+ `cmd/houfeng-center/bootstrap.go`（wiring）。
- **Asset Ledger providers 完整一条线**：`internal/center/http/handlers/providers.go`（handler）+ `internal/center/store/providers.go`（仓库）+ `internal/center/providers/`（领域类型 / 校验 / PATCH presence helper）+ `db/migrations/0016_create_asset_ledger.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源是资产层服务商主数据，不回写 `monitoring_instances.provider`。
- **Asset Ledger VPS assets 完整一条线**：`internal/center/http/handlers/vps.go`（handler）+ `internal/center/store/vps_assets.go`（仓库）+ `internal/center/vpsassets/`（领域类型 / 校验 / PATCH presence helper）+ `db/migrations/0017_add_vps_assets.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只维护资产层 VPS 账本，不改写 MonitoringInstance / Target / Agent 语义。
- **Asset Ledger subscriptions 完整一条线**：`internal/center/http/handlers/subscriptions.go`（handler）+ `internal/center/store/subscriptions.go`（仓库）+ `internal/center/subscriptions/`（领域类型 / 校验 / PATCH presence helper / nullable date）+ `db/migrations/0018_add_subscriptions.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只维护资产层 VPS 订阅账本，不创建 monitoring-instance-link、不改写 MonitoringInstance / Target / Agent 语义。
- **Asset Ledger VPS ↔ MonitoringInstance link 完整一条线**：`internal/center/http/handlers/asset_links.go`（link / unlink / query handler）+ `internal/center/store/vps_monitoring_instance_links.go`（仓库）+ `internal/center/assetlinks/`（领域类型 / 摘要 DTO / sentinel errors）+ `db/migrations/0019_create_vps_node_links.sql` + `0029_rename_nodes_to_monitoring_instances.sql`（schema 历史与重命名迁移）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只维护关联历史；link / unlink 不改写 `monitoring_instances.provider`、monitoring instance lifecycle / monitoring / health、Target 或 Agent。
- **Asset Ledger history / timeline 完整一条线**：`internal/center/http/handlers/vps.go`（`VPSTimeline` handler 与 VPS PATCH 入口）+ `internal/center/store/renewal_decisions.go`（续费、价格、IP、规格历史仓库与 timeline 聚合）+ `internal/center/store/vps_assets.go`（续费 / IP / 规格 PATCH 事务内记录历史）+ `internal/center/store/subscriptions.go`（价格 / 续费日期 PATCH 事务内记录历史）+ `internal/center/renewals/`（历史 DTO / timeline DTO / sentinel errors）+ `db/migrations/0020_create_renewal_decisions.sql` / `0021_create_asset_histories.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只记录资产层历史；不得创建 MonitoringInstance link、不得改写 MonitoringInstance / Target / Agent。
- **Asset Ledger JSON import CLI**：`cmd/houfeng-import-vps-json/main.go`（flag / 文件 / DB / migration / 事务 / 输出）+ `internal/center/importing/`（严格 JSON、复用 provider/VPS/subscription 领域校验、dry-run 报告、导入编排）。dry-run 不写库；`-import` 才能写 provider、VPS asset、subscription，且不得创建 `vps_monitoring_instance_links` 或改写 MonitoringInstance / Target / Agent。
- **Settings-aware notifier**：`internal/center/notify/`（基础 Telegram / Feishu 客户端）被 `internal/center/incidents/` 用 `NewSettingsAwareNotifier` 包装，最终在 `bootstrap.go:88-99` 装配。`notify/` 只负责单 channel HTTP 调用；settings 读取、fallback、channel 展开与 notification record 状态判定都属于 `incidents/` 领域层。
- **agent ↔ center 契约**：`internal/contracts/agentapi/routes.go` + `types.go` 同时被 `internal/center/http/handlers/agent.go` 与 `agent/runtime/` 引用。
- **迁移闭环**：`db/migrations/0010_add_users_and_sessions.sql`（schema） + `internal/center/store/users.go` + `internal/center/store/sessions.go`（仓库） + `internal/center/auth/`（领域）+ `bootstrap.go:102-113`（wiring）。
