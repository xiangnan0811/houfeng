# 目录结构

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风 / Houfeng Fleet Control Plane V1 实现仓的后端代码组织围绕 **1 个 Go center + 1 个 Postgres + N 个 systemd Go agent** 这一拓扑。仓库严格区分：

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
│   │   ├── assetlinks/        # Asset Ledger VPS ↔ Node 关联 + 摘要查询接口
│   │   ├── renewals/          # Asset Ledger 续费 / 价格 / IP / 规格历史 + VPS timeline 接口
│   │   ├── importing/         # Asset Ledger JSON dry-run/import 解析、校验、报告与编排
│   │   ├── targets/           # Target / ProbeItem 领域类型与频率档枚举
│   │   ├── nodes/             # Node 领域类型 + Repository 接口
│   │   ├── agentplan/         # 下发给 agent 的 plan 类型
│   │   ├── runtimefacts/      # 运行时事实领域类型
│   │   ├── observations/      # 原始观测的 service / 校验
│   │   └── ids/               # ID 生成（node_id / target_id 等）
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

新增一个领域子包前先确认 `internal/center/` 下没有合适归属。**不要为每个新需求都建子包**；如果只是给 nodes 加一个查询函数，就放进 `internal/center/nodes/` 与 `internal/center/store/nodes.go`。

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
| `node_onboarding.go` | 节点接入与 binding 操作 |
| `nodes.go` | `/api/nodes`、`/api/nodes/{id}` |
| `providers.go` | `/api/providers`、`/api/providers/{provider_id}` |
| `subscriptions.go` | `/api/subscriptions`、`/api/subscriptions/{subscription_id}` |
| `vps.go` | `/api/vps`、`/api/vps/{vps_id}`、`/api/vps/{vps_id}/timeline` |
| `asset_links.go` | `/api/vps/{vps_id}/nodes`、`link-node`、`unlink-node`、`/api/nodes/{node_id}/vps` |
| `runtime_controls.go` | 节点 / 目标 runtime 控制（含维护开关） |
| `runtime_facts.go` | 节点 / 目标运行时事实 |
| `settings.go` | `/api/settings` |
| `spa.go` | 静态 SPA fallback（`HOUFENG_WEB_DIST_DIR`） |
| `targets.go` | `/api/targets` |
| `json.go` | `writeJSON` / `decodeJSON` / `writeError` 共用辅助（详见 `handlers/json.go:10-31`） |

> 注：`CLAUDE.md` 列出的 handler 清单未包含 `auth.go` 与 `metadata.go`，但代码确实存在（`router.go:35-69` 注册了 `/api/auth/*`）。这是已知文档差距。

### `internal/center/store/`

**一文件一 aggregate** 的 Postgres 仓库，全部使用 `pgxpool.Pool`。当前真实文件：

```
agent_plan.go          dashboard.go       incidents.go      nodes.go
observations.go        postgres.go        probe_metadata.go providers.go
renewal_decisions.go   retention.go       runtime_facts.go  sessions.go
settings.go            subscriptions.go   sync_batches.go   targets.go
users.go               vps_assets.go      vps_node_links.go
migrate/
```

每个文件提供一个 `NewPostgres<Aggregate>Repository(*pgxpool.Pool)` 构造器（参见 `store/nodes.go:34-36`）。`postgres.go` 提供共享的 `OpenPostgres` 入口（`store/postgres.go:11-31`）。

### `agent/<subpkg>/`

agent 子包扁平化拆分，每个职责一个包：

- `config/`、`token/`、`fingerprint/`、`enroll/`、`hostsample/`、`probe/`、`containersample/`、`exec/`、`syncqueue/`、`runtime/`
- `runtime/` 是装配中心，把其余子包按 `collect → buffer → sync → apply plan` 串起来
- agent 必须保持"thin"：不接受任意脚本 / 用户自定义参数、不本地评估规则；当前仅允许 `exec/` 中编译期白名单命令，以及 `containersample/` 对本机 Docker CLI 的 best-effort 事实采样（Docker 不存在或 daemon 不可用时静默跳过）。`exec.Lookup` 必须返回参数副本，避免调用方篡改编译期白名单；`exec.Run` 必须使用 `exec.CommandContext` 而不是 shell；Docker 采样只能调用固定参数形状的 `docker ps --all --no-trunc --format ...` 与 `docker stats --no-stream --format ...`，不得扩展为 Docker 控制、编排或容器生命周期操作。

#### Scenario: agent command / Docker 边界

1. **Scope / Trigger**
   - 触发：修改 `agent/exec/`、`agent/containersample/`、`agent/runtime` 的 pending action 执行或 Docker container facts 采样路径。
   - 目标：保持 Agent 是薄 observe / buffer / sync / apply-plan 进程，只允许白名单命令执行和 best-effort 本机 Docker facts，防止被误扩展成任意远程执行或容器编排面。

2. **Signatures**
   - 命令下发：center 只通过 `agentapi.PendingAction{ActionID, CommandID}` 下发稳定 `command_id`，不下发二进制路径、参数或 shell snippet。
   - 白名单解析：`agent/exec.Lookup(commandID)` 返回 `(bin, args, ok)`，`args` 必须是内部白名单参数的 defensive copy。
   - 命令执行：`agent/exec.Run(ctx, bin, args)` 使用 `exec.CommandContext`，带 30s timeout 与 stdout/stderr 独立 64KB 截断。
   - Docker facts：`agent/containersample.Collect(ctx)` 在 host sample 时 best-effort 调用 Docker CLI，返回 `[]agentapi.ContainerInfo` 或 `nil`。

3. **Contracts**
   - 白名单命令 ID 当前固定为：`df_h`、`free_m`、`uptime`、`top_head`、`journalctl_u`、`systemctl_status`、`dmesg_err`、`docker_ps`。
   - 命令参数全部编译进 agent，不接受中心、Web 或用户传入的动态参数。
   - 未知 `command_id` 由 agent runtime 静默忽略，不阻塞 sync loop，不生成 command result。
   - Docker CLI 不存在、daemon 不可用、`docker ps` 失败或 context 已取消时返回 `nil, nil`，不得让 host sample 失败。
   - `docker stats` 失败时仍返回 `docker ps` 的 container identity/status facts，CPU/mem 百分比留空。
   - Docker facts 只描述本机容器快照，不代表 Docker runtime 是必需部署依赖，不提供 start/stop/restart/logs/exec/compose/kubernetes 等控制能力。

4. **Validation & Error Matrix**
   - `Lookup` 未知 ID -> `ok=false`，bin/args 零值。
   - 调用方修改 `Lookup` 返回的 args -> 后续 `Lookup` 结果不变。
   - shell metacharacter 作为参数传给 `Run` -> 被当作普通参数，不执行额外 shell 语义。
   - `docker ps` 输出空或无法解析 -> `nil, nil`。
   - `docker stats` 输出字段无法解析 -> 对应 CPU/mem 字段保持 nil。

5. **Good / Base / Bad Cases**
   - Good: center 下发 `command_id=uptime`，agent 通过 whitelist 解析为 `uptime` + nil args，执行后回传带 action/command identity 的 `CommandResult`。
   - Good: Docker 可用时 host sample 附带 container name/image/status 和可选 CPU/mem 百分比；Docker 不可用时 host sample 仍正常上传且 `containers` 为空。
   - Base: 当前 whitelist 有 `docker_ps`，但这只是诊断命令，不等于 Docker 编排能力。
   - Bad: 让 center 或 Web 传入 `args:["-c","..."]`、`bin:"sh"`、`command:"docker rm ..."`，会把薄 Agent 扩成任意执行面。
   - Bad: 在 `containersample` 中增加 `docker start/stop/restart/logs/exec` 或 Docker SDK 控制路径，违反 best-effort facts 边界。

6. **Tests Required**
   - `agent/exec/whitelist_test.go`：固定 whitelist command IDs、bin、args；未知 ID 拒绝；返回 args 是 defensive copy。
   - `agent/exec/runner_test.go`：正常/非零/timeout/not-found/output truncation；必须覆盖不隐式调用 shell。
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

### `internal/contracts/agentapi/`

center 与 agent 同时引用的唯一契约包。内容：

- `routes.go`：`EnrollPath = "/api/agent/enroll"`、`SyncPath = "/api/agent/sync"`
- `types.go`：请求 / 响应 DTO、`BindingStatus*` / `ErrorCode*` / `ProbeKind*` / `ProbeError*` 常量

> 这是**唯一**允许同时被 `cmd/houfeng-center` / `internal/center/http/handlers` 与 `cmd/houfeng-agent` / `agent/runtime` 引用的包。新增 agent ↔ center 字段时，先改这里，两侧再各自适配。**不要把 DTO 定义在 handler 包或 runtime 包内自己重复一份**。

---

## Naming Conventions

- 包名：全小写、单词内部不用下划线。一个领域一个子包（`incidents`、`enrollment`、`runtimefacts`）。
- 文件名：`snake_case.go`，与其内最重要的类型 / 资源对齐（`runtime_facts.go`、`node_onboarding.go`）。
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

- **HTTP 资源完整一条线**：`internal/center/http/handlers/nodes.go`（handler）+ `internal/center/http/handlers/nodes_test.go`（table-driven 测试）+ `internal/center/store/nodes.go`（仓库）+ `internal/center/nodes/`（领域类型）+ `cmd/houfeng-center/bootstrap.go:122-131`（wiring）。
- **Asset Ledger providers 完整一条线**：`internal/center/http/handlers/providers.go`（handler）+ `internal/center/store/providers.go`（仓库）+ `internal/center/providers/`（领域类型 / 校验 / PATCH presence helper）+ `db/migrations/0016_create_asset_ledger.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源是资产层服务商主数据，不回写 `nodes.provider`。
- **Asset Ledger VPS assets 完整一条线**：`internal/center/http/handlers/vps.go`（handler）+ `internal/center/store/vps_assets.go`（仓库）+ `internal/center/vpsassets/`（领域类型 / 校验 / PATCH presence helper）+ `db/migrations/0017_add_vps_assets.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只维护资产层 VPS 账本，不改写 Node / Target / Agent 语义。
- **Asset Ledger subscriptions 完整一条线**：`internal/center/http/handlers/subscriptions.go`（handler）+ `internal/center/store/subscriptions.go`（仓库）+ `internal/center/subscriptions/`（领域类型 / 校验 / PATCH presence helper / nullable date）+ `db/migrations/0018_add_subscriptions.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只维护资产层 VPS 订阅账本，不创建 node-link、不改写 Node / Target / Agent 语义。
- **Asset Ledger VPS ↔ Node link 完整一条线**：`internal/center/http/handlers/asset_links.go`（link / unlink / query handler）+ `internal/center/store/vps_node_links.go`（仓库）+ `internal/center/assetlinks/`（领域类型 / 摘要 DTO / sentinel errors）+ `db/migrations/0019_create_vps_node_links.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只维护关联历史；link / unlink 不改写 `nodes.provider`、Node lifecycle / monitoring / health、Target 或 Agent。
- **Asset Ledger history / timeline 完整一条线**：`internal/center/http/handlers/vps.go`（`VPSTimeline` handler 与 VPS PATCH 入口）+ `internal/center/store/renewal_decisions.go`（续费、价格、IP、规格历史仓库与 timeline 聚合）+ `internal/center/store/vps_assets.go`（续费 / IP / 规格 PATCH 事务内记录历史）+ `internal/center/store/subscriptions.go`（价格 / 续费日期 PATCH 事务内记录历史）+ `internal/center/renewals/`（历史 DTO / timeline DTO / sentinel errors）+ `db/migrations/0020_create_renewal_decisions.sql` / `0021_create_asset_histories.sql`（schema）+ `bootstrap.go` / `router.go` 显式 wiring。该资源只记录资产层历史；不得创建 Node link、不得改写 Node / Target / Agent。
- **Asset Ledger JSON import CLI**：`cmd/houfeng-import-vps-json/main.go`（flag / 文件 / DB / migration / 事务 / 输出）+ `internal/center/importing/`（严格 JSON、复用 provider/VPS/subscription 领域校验、dry-run 报告、导入编排）。dry-run 不写库；`-import` 才能写 provider、VPS asset、subscription，且不得创建 `vps_node_links` 或改写 Node / Target / Agent。
- **Settings-aware notifier**：`internal/center/notify/`（基础 Telegram / Feishu 客户端）被 `internal/center/incidents/` 用 `NewSettingsAwareNotifier` 包装，最终在 `bootstrap.go:88-99` 装配。`notify/` 只负责单 channel HTTP 调用；settings 读取、fallback、channel 展开与 notification record 状态判定都属于 `incidents/` 领域层。
- **agent ↔ center 契约**：`internal/contracts/agentapi/routes.go` + `types.go` 同时被 `internal/center/http/handlers/agent.go` 与 `agent/runtime/` 引用。
- **迁移闭环**：`db/migrations/0010_add_users_and_sessions.sql`（schema） + `internal/center/store/users.go` + `internal/center/store/sessions.go`（仓库） + `internal/center/auth/`（领域）+ `bootstrap.go:102-113`（wiring）。
