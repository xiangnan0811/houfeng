# 目录结构

> **权威来源**：本文件规则来自 `CLAUDE.md` + `docs/design/v1-baseline/`，如有冲突以前者为准。

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
│   │   ├── targets/           # Target / ProbeItem 领域类型与频率档枚举
│   │   ├── nodes/             # Node 领域类型 + Repository 接口
│   │   ├── agentplan/         # 下发给 agent 的 plan 类型
│   │   ├── runtimefacts/      # 运行时事实领域类型
│   │   ├── observations/      # 原始观测的 service / 校验
│   │   └── ids/               # ID 生成（node_id / target_id 等）
│   └── contracts/
│       └── agentapi/          # ★ center 与 agent 共享的契约：路径、类型、错误码
├── db/
│   └── migrations/            # 0001_*.sql … 0010_*.sql + embed.go（embed.FS）
├── docs/                      # 设计基线 / 部署 / 验证
├── scripts/                   # verify.sh 等
├── bin/                       # build 产物（go build 输出）
└── web/                       # React 19 + Vite SPA（不展开）
```

> **`internal/center/` 子包以 `ls` 实际结果为准**。当前实际存在但 `CLAUDE.md` 未提及的子包（必须在文档中考虑到）：`auth/`（用户/会话/密码/cookie/cleanup）。详见后文 *与 CLAUDE.md 的差异* 一节。

---

## Module Organization

### `cmd/`

每个二进制一个目录。`main.go` 仅做：解析配置 → 调用同包内 `bootstrap*` → 处理信号。`bootstrap.go` 把所有依赖显式注入（参见 `cmd/houfeng-center/bootstrap.go:58-147`，`bootstrapCenter` 函数），并通过 `bootstrapDeps` 暴露可替换的工厂以便测试（见 `bootstrap_test.go`）。**禁止把业务逻辑写进 `cmd/`**。

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
agent_plan.go      dashboard.go     incidents.go     nodes.go
observations.go    postgres.go      probe_metadata.go retention.go
runtime_facts.go   sessions.go      settings.go      sync_batches.go
targets.go         users.go         migrate/
```

每个文件提供一个 `NewPostgres<Aggregate>Repository(*pgxpool.Pool)` 构造器（参见 `store/nodes.go:34-36`）。`postgres.go` 提供共享的 `OpenPostgres` 入口（`store/postgres.go:11-31`）。

### `agent/<subpkg>/`

agent 子包扁平化拆分，每个职责一个包：

- `config/`、`token/`、`fingerprint/`、`enroll/`、`hostsample/`、`probe/`、`syncqueue/`、`runtime/`
- `runtime/` 是装配中心，把其余子包按 `collect → buffer → sync → apply plan` 串起来
- agent 必须保持"thin"：不执行任意脚本、不跑 Docker、不本地评估规则

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
- 迁移文件：`<NNNN>_<verb>_<scope>.sql`，序号 4 位起步、动词放第一个（`add`、`normalize`），见 `db/migrations/0001_initial_schema.sql` … `0010_add_users_and_sessions.sql`。

---

## 哪里放新代码

| 变更类型 | 落点 |
|----------|------|
| 新增 HTTP endpoint | 1) `internal/center/http/handlers/<resource>.go` 内增加工厂；2) 在 `internal/center/http/router.go` 的 `RouterOptions` 加字段并 mux 注册；3) 在 `cmd/houfeng-center/bootstrap.go` 的 `bootstrapCenter` 显式构造并塞进 `RouterOptions`；4) 同目录 `<resource>_test.go` 增 table-driven 测试 |
| 新持久化字段 / 表 | 1) `db/migrations/<next-NNNN>_<verb>_<scope>.sql` 写原生 SQL；2) 更新 `internal/center/store/<aggregate>.go` 仓库的 select / insert / update；3) 更新对应 `internal/center/<domain>/types.go` |
| 新 agent ↔ center 字段 | 1) `internal/contracts/agentapi/types.go` 改 DTO；2) center 端在 `internal/center/syncing/` 或对应 handler 处理；3) agent 端在 `agent/runtime/` 或采集子包消费；**严禁两侧各自定义同名结构** |
| 新领域行为 | 优先放进既有 `internal/center/<domain>/`；只有当确实属于新领域时才新增子包 |
| agent 新增采集项 | 在 `agent/hostsample/` 或 `agent/probe/` 内扩展，并通过 `agent/runtime/` 串接；不要往 agent 里塞规则判定 |

---

## Examples

以下是当前代码库内"组织到位"的真实参考点：

- **HTTP 资源完整一条线**：`internal/center/http/handlers/nodes.go`（handler）+ `internal/center/http/handlers/nodes_test.go`（table-driven 测试）+ `internal/center/store/nodes.go`（仓库）+ `internal/center/nodes/`（领域类型）+ `cmd/houfeng-center/bootstrap.go:122-131`（wiring）。
- **Settings-aware notifier**：`internal/center/notify/`（基础 Telegram 客户端）被 `internal/center/incidents/` 用 `NewSettingsAwareNotifier` 包装，最终在 `bootstrap.go:88-99` 装配，体现"领域子包负责行为，bootstrap 负责拼装"。
- **agent ↔ center 契约**：`internal/contracts/agentapi/routes.go` + `types.go` 同时被 `internal/center/http/handlers/agent.go` 与 `agent/runtime/` 引用。
- **迁移闭环**：`db/migrations/0010_add_users_and_sessions.sql`（schema） + `internal/center/store/users.go` + `internal/center/store/sessions.go`（仓库） + `internal/center/auth/`（领域）+ `bootstrap.go:102-113`（wiring）。
