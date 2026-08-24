# 目录结构

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风 / Houfeng Fleet Control Plane 当前后端代码组织围绕 **1 个 Go center + 1 个隔离 content processor + 1 个 required scanner + 1 个 Postgres + N 个 systemd Go agent** 这一拓扑。仓库严格区分：

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
│   ├── houfeng-content-processor/ # attachment processor 入口与显式 wiring
│   │   ├── main.go
│   │   ├── bootstrap.go
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

`cmd/houfeng-backup` / `cmd/houfeng-restore` 同样只做 flag、信号和 `recordbackup.NewService` / `recordrestore.NewService` 装配。编排、manifest、local/S3 store 与恢复状态机分别在 `internal/center/recordbackup/` 与 `internal/center/recordrestore/`。`cmd/houfeng-record-platform-admin` 只做 APP ACL migrate / bootstrap / finalize，**禁止**改成备份 CLI。能力矩阵在 `internal/center/recordreadiness/`，由 `newProductionRecordReadinessRegistry` 接线。

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

#### Scenario: release-asset production Compose and image contract

1. **Scope / Trigger**
   - 触发：修改 `Dockerfile`、`compose.yaml`、`docs/deploy/compose.env.example`、README / deployment docs、Center/processor/ClamAV/PostgreSQL/Records authority 边界，或 `.github/workflows/publish-images.yml`。
   - 目标：下载同一 GitHub Release 的 `compose.yaml` 与 `compose.env.example`（后者保存为本地 `.env`）后，operator 只需编辑 `.env`、验证并执行普通 Compose；不 checkout source、不 build、不运行 SQL/helper launcher。Agent 仍是 monitored host 上的 Linux/systemd workload。

2. **Signatures**
   - Root `Dockerfile` produces one published image containing `houfeng-center`, `houfeng-content-processor`, `houfeng-record-platform-admin`, baked `web/dist`, curl, and Poppler. Normal runtime stays `USER houfeng:houfeng` (UID/GID 10001); only explicit one-shot/authority services override identity.
   - `compose.yaml` defines exactly eight services: `houfeng-storage-init`, `houfeng-secrets-init`, `db`, `houfeng-db-init`, `houfeng-record-authority`, `clamav`, `houfeng`, and `houfeng-content-processor`. It has no `build:`, Caddy, or agent service.
   - Center listens on container port `16001`, joins the pre-existing external network named by `HOUFENG_PROXY_NETWORK` as alias `houfeng`, and has no public host port by default. Only Center joins that NPM network.
   - The tracked env template has exactly `Must change`, `Recommended`, and `Optional` operator sections. The release job writes the matching immutable `docker.io/linnea7171/houfeng:vX.Y.Z` into `HOUFENG_IMAGE`; installation does not ask the user to edit it.
   - GitHub release asset URLs use `https://github.com/xiangnan0811/houfeng/releases/...`; the Docker registry identity remains the separately configured published image path.

3. **Contracts**
   - `docker compose up -d` owns ordinary initialization. Storage init creates UID/GID-safe paths; secret init stages only bootstrap/runtime/migrator/platform-admin files for read-only consumers; db-init provisions roles, calls `ConvergeAppACLCurrent`, verifies/activates the signed authority bundle through the existing projector, and runs current runtime admission. Center waits for healthy authority; the processor waits for db-init and ClamAV.
   - Environment-backed Compose secrets remain scoped: DB gets bootstrap only; Center gets runtime + initial-admin + session; processor gets runtime only; db-init gets staged provisioning secrets; authority reads only its generated private state/database secret. The secret stager bind-mounts only `./data/secrets`, never the whole data tree or authority private bundle. Optional comparison keys are mounted only into Center; only the S3 credential directory is shared with the processor. Host optional-secret directories/files are owned by the image's UID/GID `10001` with directory mode `0700` and file mode `0400`, so non-root services can read their scoped bind mounts without broadening host access. Center/processor never receive bootstrap, migrator, platform-admin, or authority credentials.
   - Production pins `HOUFENG_RECORDS_ENABLED=true` and permanent delete false. Center receives a file-based deployment ID plus fixed `compose-center` / `api` / `records.runtime`; no allow gate or operator-provided authority identity is permitted.
   - Durable local state is a visible portable tree: `./data/postgres`, `./data/attachments`, `./data/logs`, `./data/clamav`, `./data/records-authority`, `./data/center-config`, and `./data/secrets`. PostgreSQL, local attachments, and Records authority state are one coordinated restore unit; active DB plus absent/corrupt/mismatched authority state fails closed.
   - Processor stays non-root, read-only, `cap_drop: ALL`, `no-new-privileges`, core=0, with bounded `noexec,nosuid,nodev` tmpfs. Center and processor share only the runtime DB role, Blob contract, attachment bind, and scanner settings.
   - `.env` passwords use independently generated, pairwise-distinct hex values (`openssl rand -hex 32`) to avoid dotenv interpolation traps and cross-role credential reuse; db-init rejects duplicate role secrets before mutation. Password-only edits do not guarantee service recreation: controlled rotation stops consumers, reruns `houfeng-secrets-init`, reruns `houfeng-db-init`, then force-recreates Center/processor.
   - `.github/workflows/publish-images.yml` may publish only on `release.published` or explicit maintainer dispatch. It checks out the resolved release source, builds the image, stages and validates the public `compose.env.example` filename (never hidden `dist/.env.example`), and uploads `compose.yaml` plus `compose.env.example` to the matching GitHub Release. A post-upload public readback queries all asset names, requires exactly one of each deployment name, rejects `.env.example` / `default.env.example` without rejecting unrelated agent assets, downloads both exact names into a fresh trap-cleaned directory, and requires byte identity with the staged files before the success summary.
   - Direct/systemd documentation may retain explicit pre-R1 provisioning as an advanced path, but the Docker quick-start section must contain no manual SQL, local toolchain, source build, or helper launcher.

4. **Validation & Error Matrix**

   | Condition | Expected behavior |
   | --- | --- |
   | required env blank, non-HTTPS public URL, or NPM network absent | Compose/preflight fails visibly; never add a public fallback port |
   | storage/secret/db init fails | dependent Center/processor stay unstarted; safe stage name is visible in init logs |
   | authority state missing/corrupt/mismatched for active DB | db-init/authority fails closed; never regenerate over active state |
   | authority heartbeat stale/unhealthy | Center dependency/readiness or Records writes fail closed |
   | Center/processor receives privileged/authority secret | reject static review and service config |
   | processor becomes root/writable/capable or loses bounds | reject static review and runtime inspection |
   | release env image differs from the release tag | deployment-assets job fails before upload |
   | required public deployment name absent/duplicated, legacy normalized name present, download absent, or public bytes differ | deployment-assets job fails after upload and before success summary |
   | operator changes only a secret then runs plain `up -d` | not a supported rotation proof; require explicit staging/init/recreation sequence |
   | operator restores only DB, attachments, or authority state | incomplete recovery point; restore the coordinated directory copy |

5. **Good / Base / Bad Cases**
   - Good: operator downloads release assets from `xiangnan0811/houfeng`, fills Must change secrets/network/HTTPS origin, runs `docker compose config`, `pull`, then `up -d`; PostgreSQL/ClamAV/authority/Center become healthy and processor runs without a public port.
   - Good: real Records smoke authenticates, uploads content, observes quarantine → ClamAV/processor → available, then publishes through the production admission gate and downloads the same content.
   - Base: exact repeat validates the same signed authority state/contract and leaves stable identity/epoch/fence unchanged except bounded membership expiry renewal and approved password rotation.
   - Bad: `build: .`, a wrapper script, manual SQL, a named business-data volume, `latest` in the release env asset, or an agent/Caddy service changes the approved operator contract.
   - Bad: copying PostgreSQL without `./data/records-authority`, editing the signed ledger, or exposing the authority database secret to Center bypasses the coordinated trust boundary.

6. **Tests Required**
   - `go test ./internal/center/deploy ./cmd/houfeng-record-platform-admin` covers topology, env grouping, secret scope/staging, HTTPS preflight, release URLs/assets, safe stage diagnostics, and authority wiring.
   - `docker compose config` must fail independently for every required blank and pass with a task-owned valid env. Validate current installed Compose syntax, including environment-backed secrets and external networks.
   - Strict PostgreSQL 16 tests must cover fresh init, exact repeat, role drift, password rotation, current convergence/admission, activation, heartbeat expiry/renewal, and privilege denials with zero skips.
   - Real isolated Compose evidence must build/inspect the release image, start unique task-owned resources, prove admitted Records + attachment/ClamAV flow, restart/exact repeat, corrupt-state fail-closed, portable-copy behavior, and clean teardown without touching unrelated Docker state.
   - Inspect the image for all three binaries, baked Web, Poppler/curl, default UID/GID 10001, and entrypoint behavior. Workflow static checks freeze post-upload name query/cardinality, forbidden-name rejection, fresh exact downloads, trap cleanup, byte comparison, and upload/readback/summary ordering while preserving unrelated Release assets. Run `make verify-go`, applicable Web/workflow checks, `actionlint` when installed, shell syntax, and `git diff --check`.
   - Static docs tests must scope Docker quick-start prohibitions to the Compose section so advanced direct/systemd provisioning remains correct.

7. **Wrong vs Correct**

```yaml
# Wrong: source build and public port silently replace the release/NPM contract.
services:
  houfeng:
    build: .
    ports: ["16001:16001"]
```

```yaml
# Correct: immutable release image, external NPM network, no public host port.
services:
  houfeng:
    image: "${HOUFENG_IMAGE:?set HOUFENG_IMAGE in .env}"
    networks:
      houfeng-proxy:
        aliases: [houfeng]
networks:
  houfeng-proxy:
    external: true
    name: "${HOUFENG_PROXY_NETWORK:?set HOUFENG_PROXY_NETWORK in .env}"
```

```text
Wrong: restore data/postgres alone or edit the signed authority ledger.
Correct: stop writes and copy compose.yaml + .env + optional-secrets + the complete data tree; restore PostgreSQL and authority state together.
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
