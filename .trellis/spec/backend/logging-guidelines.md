# 日志规范

> **权威来源**：`CLAUDE.md` + 业务/结构以 `docs/design/v1-baseline/` frozen 子集（architecture-data-model / rules-and-interaction / tech-selection / interactive-prototype-and-operation-flow）为准，视觉以 `docs/design/v2-houfeng/`（design-language / component-spec）为准。冲突时以前述顺序为准。本项目处于初始开发阶段，规则会随代码演进调整。

---

## Overview

候风后端**统一使用 Go 标准库 `log/slog`**。`grep -r "zap\|logrus\|zerolog" ./` 结果为空，**禁止引入第三方日志库**。

格式：

- **center**：`bootstrap.go` / 各 worker 通过 `slog.Default()` 取默认 logger（即 stdlib 默认 text handler，写到 stderr）。
- **agent**：`cmd/houfeng-agent/main.go:26` 显式构造 `slog.New(slog.NewTextHandler(os.Stdout, nil))` 写入 stdout，便于 systemd journal 直接收集。

> 入口文件特殊用法：`cmd/houfeng-center/main.go` 仍使用 stdlib 的 `"log".Fatalf(...)`（启动期致命错误退出，例如 `log.Fatalf("load center config: %v", err)`，见 `main.go:17/25/30`）。这是**仅限二进制 main 函数的启动期错误**的例外，其他业务代码不要再引 `"log"`。

---

## Logger 初始化

### Center

`cmd/houfeng-center/bootstrap.go` 不构造自己的 `*slog.Logger`，所有 worker 都接收 `slog.Default()` 作为 logger 入参：

- `retention.NewWorker(retentionRepo, settingsRepo, slog.Default(), retention.DefaultWorkerInterval)`（`bootstrap.go:78`）
- `incidentservice.NewSettingsBackedService(..., slog.Default(), ...)`（`bootstrap.go:96`）
- `auth.NewSessionCleanupWorker(sessionRepo, slog.Default(), auth.DefaultSessionCleanupInterval)`（`bootstrap.go:112`）

每个 worker 构造器都允许 `logger == nil` 时回退到 `slog.Default()`（参考 `retention/worker.go:20-22`、`auth/cleanup.go:18-20`、`incidents/service.go:143-145`），所以单测可以传 `slog.Default()` 或 `slog.New(slog.NewTextHandler(io.Discard, nil))`（参考 `retention/worker_test.go:204`，把日志重定向到 `io.Discard` 抑制噪音）。

**约定**：业务包**不要**自己读 env 决定 log level / format。所有进程级配置由二进制入口（cmd/）一次设定。

### Agent

`cmd/houfeng-agent/main.go:26-30`：

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
runtime := agentruntime.New(cfg, logger, ...)
```

`agent/runtime/runtime.go:104-106` 的构造器也允许 `nil` 回退到 `slog.Default()`，但生产路径恒走显式 logger。

---

## Log level 约定

当前代码库实际使用 3 个 level：`Info`、`Error`，**没有任何 `Warn` 或 `Debug` 调用**（`grep` 结果为空）。新代码保持同一约定：

| Level | 何时使用 | 例子 |
|-------|----------|------|
| `Info` | 进程 / worker 生命周期事件、首次绑定成功、retention pass 完成、session sweep 删除条数 > 0 | `agent/runtime/runtime.go:139` `"agent runtime started"`；`agent/runtime/runtime.go:160` `"agent enrolled"`；`retention/worker.go:90-101` `"retention pass completed"`；`auth/cleanup.go:48` `"session sweep"` (deleted > 0) |
| `Error` | 操作失败但 worker 选择继续；HTTP / DB 异常需要排查 | `incidents/service.go:172` `"evaluate node incidents after sync failed"`；`retention/worker.go:71/77/86`；`auth/cleanup.go:44` `"session sweep"` (with error)；`agent/runtime/runtime.go:194` `"sync queue flush failed"` |

**新代码不要随便引入 `Debug`**：当前没有运行时 level 切换机制（slog 默认 handler 不读 `LOG_LEVEL` env），加 `Debug` 等于代码里写死永远不输出。如果确实需要详细诊断，先讨论是否扩 `cmd/houfeng-center/main.go` 与 `cmd/houfeng-agent/main.go` 的 handler 配置。

`Warn` 在当前代码库未被任何业务路径使用——**保持空缺**，把"该 retry / 后续会被 sweep 兜底的失败"也归到 `Error`，让 systemd / 日志聚合更容易抓异常。

---

## 结构化字段约定

slog 调用一律走 `key, value, key, value` 形式，**不要拼字符串**。当前代码使用的标准 key 名（保持一致以方便聚合查询）：

| Key | 出现位置 | 含义 |
|-----|----------|------|
| `error` | 所有 `Error` 调用 | 必填，挂底层 `err`，例如 `"error", err` |
| `node_id` | `incidents/service.go:172`、`agent/runtime/runtime.go:160` | Node 主键 `nd_xxx` |
| `target_id` | `incidents/service.go:176`、`incidents/service.go:491` | Target 主键 `tg_xxx` |
| `object_type` / `object_id` | `incidents/service.go:491` | incident 通知失败时定位主体 |
| `server_url` | `agent/runtime/runtime.go:139,140` | agent 当前指向的 center 地址 |
| `status` / `binding_status` | `agent/runtime/runtime.go:160` | enroll 应答里的状态字段，原样透传，便于排查绑定问题 |
| `deleted` | `auth/cleanup.go:48` | session sweep 删除条数 |
| 各类 `*_rows` / `deleted_*` | `retention/worker.go:91-101` | retention pass 的逐表统计 |

约定：

- key 用 `snake_case` 英文，与 DB 列名 / JSON 字段名对齐。
- value 不要包 `fmt.Sprintf`：直接传原值（`int` / `string` / `error` / `time.Time`），让 slog handler 自己格式化。
- **不要使用 `slog.With(...)` 派生 logger** —— 当前代码库没有这种用法；保持每条 log 自带完整上下文，便于全文搜索。

---

## 应该 log 的位置

参考现有调用，新代码遵循以下覆盖：

1. **进程启动 / 关闭**：`Run` 入口 `Info "<svc> started"` + `defer Info "<svc> stopped"`。例：`agent/runtime/runtime.go:139-140`。
2. **重要状态变更**：agent 首次 enroll 成功（`runtime.go:160`）、binding 状态变化、retention 完成扫描。
3. **worker tick 内的失败**：worker 必须吞错继续，**每次失败必须 `Error` 一行**，把失败位置 + 关键 ID + `error` 一并记录。例：`incidents/service.go:172/176/491`、`retention/worker.go:71/77/86`、`auth/cleanup.go:44`、`agent/runtime/runtime.go:194/221/298`。
4. **HTTP 请求路径不主动 log**：handler 通过 `writeError` 返回 4xx/5xx 即足够；当前代码 handler 内**没有** `slog.Info/Error` 调用。如需排查请求级问题，看 systemd journal 里 stdlib net/http 的访问日志或扩中间件，**不要在每个 handler 里散加 log**。
5. **agent enroll / sync 边界**：enroll 成功或绑定失败必须 log 一行，便于 fleet 排查；sync 失败由 `runtime.go:194/197` 处理。

---

## 不该 log 的位置 / 内容

- ❌ **请求 hot path（每条观测、每个 sync batch 详情）**：center 每分钟一次 sync × N agent，逐条 log 会爆 systemd。当前代码 `internal/center/syncing/service.go` 与 `store/sync_batches.go` **完全没有 log**——保持现状，让仅在错误 / 周期性事件时打印。
- ❌ **敏感字段**：以下字段**禁止出现在 log value 里**：
  - `sync_token` / 任何来自 `enrollment.SyncToken`（`agent/runtime/runtime.go:160` 故意只 log `binding_status` 不 log `sync_token`）
  - `password` / `password_hash` / bcrypt 字符串
  - 完整的 enrollment token 内容（仅在请求路径校验，不 log；一键安装命令也只能在认证 UI 中按用户操作 reveal/copy，不写入 center/agent 日志）
  - cookie / Authorization 头
  - Telegram bot token / chat id（`internal/center/notify/telegram.go` 全程不 log payload 内容，仅 `fmt.Errorf` 包装失败）
  - Feishu webhook URL（`internal/center/notify/feishu.go` 全程不 log webhook URL，仅 `fmt.Errorf` 包装失败）
- ❌ **完整的 SQL / 完整的 JSON body**：体积大且可能含敏感字段。失败时记录 `error` 包装后的链路即可。
- ❌ **`fmt.Println` / `println` / `fmt.Printf`**：`grep` 结果为空，新代码不要引入。

---

## agent vs center 的差异

| 维度 | center | agent |
|------|--------|-------|
| Logger 来源 | `slog.Default()`（继承 stdlib 默认 handler，写 stderr） | 显式 `slog.New(slog.NewTextHandler(os.Stdout, nil))` 写 stdout |
| 部署 | 由 systemd 收 stderr | 由 systemd 收 stdout，目录约定见 `docs/deploy/systemd/houfeng-agent.service` |
| 启动期错误 | `cmd/houfeng-center/main.go` 用 stdlib `log.Fatalf` | `cmd/houfeng-agent/main.go:19-21` 用 `slog.Error("load agent config", "error", err)` + `os.Exit(1)` |
| 是否可 log Telegram 内容 | 否（含 chat 内容） | agent 不接触通知，无此问题 |

差异是历史遗留，**新代码不要试图统一**——先保持一致。如果以后要切 JSON handler、加 trace id，应该一次性同时改 cmd/houfeng-center 与 cmd/houfeng-agent，不要单边升级。

---

## 反模式 / Common Mistakes

- ❌ **`fmt.Println("debug:", ...)` 调试遗漏到 main**：会污染 systemd journal，且无 level 标记。用 `slog.Info` / `slog.Debug` 配合临时 handler。
- ❌ **同时 log + 返回 error，导致重复输出**：调用栈每一层都 log 同一个 err。约定**只在最外层 worker / handler / main 处 log**；内层只 `fmt.Errorf("...: %w", err)` 包装。当前代码遵循这一点（参考 `incidents/service.go` 内部 `evaluateNode` 返回 wrapped error，最外层 `AfterSuccessfulSync` 才 `s.logger.Error`）。
- ❌ **在 worker 内 log info 后吞掉 error**：`logger.Info("...", "error", err)` 会让监控系统漏掉异常。所有错误必须用 `Error` level。
- ❌ **拼字符串 log**：`logger.Info(fmt.Sprintf("node %s started", id))`。slog 提供 key-value，**直接 `logger.Info("node started", "node_id", id)`**。
- ❌ **log 含 token / 密码 / Telegram chat 内容 / Feishu webhook URL**：见上节"不该 log"。
- ❌ **新增第三方 logger 依赖（zap、logrus、zerolog 等）**：项目硬性约束，stdlib `log/slog` 已足够。

---

## 已知 gap

- 当前 slog handler 是 stdlib 默认 text 输出，**未配置最小 level、未输出 source 行号、不带 trace id**。如果 fleet 规模扩大需要更结构化日志（例如切到 `slog.NewJSONHandler` + LokiQuery），应统一在 `cmd/houfeng-center/main.go` 与 `cmd/houfeng-agent/main.go` 同步切换，并把可复用结论写进本 spec 或当前 active docs。
- `cmd/houfeng-center/main.go` 仍混用 stdlib `"log"`，与全仓 `log/slog` 风格不一致。短期内为启动失败兜底保留；新代码任何业务路径**禁止**再引 `"log"` 包。
