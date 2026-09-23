# 错误处理规范

> 项目权威入口为根 `AGENTS.md`；领域行为见 [合同索引](../contracts/README.md)。

---

## Overview

候风后端的错误处理分三层处理，每一层职责清晰、互不串味：

| 层 | 职责 | 实现位置 |
|----|------|----------|
| **领域层** | 用 `errors.New` 定义可被上层 `errors.Is` 判定的 sentinel error；用 `fmt.Errorf("...: %w", err)` 包装下游错误并保留链路 | `internal/center/<domain>/types.go` 与 `service.go`、`agent/<subpkg>/*.go` |
| **HTTP 层** | 用 `errors.Is` 把领域 sentinel 翻译成 HTTP status + JSON 响应；agent 端点额外带 `agentapi.ErrorCode*` | `internal/center/http/handlers/*.go`，统一走 `handlers/json.go` 的 `writeError` / `writeAgentAPIError` |
| **agent ↔ center 契约层** | 用 `agentapi.ErrorResponse{Code, Message}` 携带稳定错误码字符串，agent 端解码为 `enroll.RemoteError` 用于决定是否重试 | `internal/contracts/agentapi/types.go:14-23`、`agent/enroll/client.go:97-116` |

> **合同**：生产失败路径必须返回 `error`，不得用 `panic` 处理运行错误。已知实现缺口见下文，不把现存偏差解释为允许模式。

---

## 领域错误：sentinel + `errors.Is`

每个领域子包在 `types.go` 顶部（或 `service.go` 内）定义一组 sentinel error，所有调用方通过 `errors.Is` 判定，**严禁字符串比较**：

| 子包 | sentinel 定义位置 | 例子 |
|------|-------------------|------|
| `internal/center/monitoringinstances/` | `types.go:30-32` | `ErrMonitoringInstanceNotFound`、`ErrInvalidBindingTransition`、`ErrMonitoringInstanceMetadataConflict` |
| `internal/center/targets/` | `types.go:31-33` | `ErrTargetNotFound`、`ErrProbeItemNotFound`、`ErrTargetMetadataConflict` |
| `internal/center/auth/` | `types.go:22-30` | `ErrUserNotFound`、`ErrInvalidCredentials`、`ErrSessionExpired`、`ErrPasswordTooShort` 等 |
| `internal/center/enrollment/` | `service.go:14-16` | `ErrBindingNotAccepted`、`ErrInvalidEnrollmentToken`、`ErrInvalidSyncToken` |
| `internal/center/observations/` | `service.go:18-19` | `ErrInvalidProbeObservation`、`ErrProbeMetadataNotFound` |
| `internal/center/settings/` | `types.go:14` | `ErrInvalidSettings`（再用 `fmt.Errorf("%w: %s", ErrInvalidSettings, message)` 携带详情，见 `types.go:368`） |
| `internal/center/syncing/` | `service.go:13-17` | `ErrBindingNotAccepted` / `ErrInvalidSyncToken`（**别名转发自 `enrollment` 包**，避免 handler 多包导入） |
| `internal/center/subscriptions/` | `types.go:14-15` | `ErrSubscriptionNotFound`、`ErrInvalidSubscriptionInput` |
| `internal/center/renewals/` | `types.go` | `ErrAssetTimelineNotFound`、`ErrInvalidAssetHistoryInput`；兼容别名 `ErrRenewalTimelineNotFound`、`ErrInvalidRenewalDecisionInput` |
| `internal/center/recordreadiness/` | `types.go` | `ErrInvalidCapabilityRegistry`、`ErrReadinessUnavailable`、`ErrContentLeak` |
| `internal/center/recordbackup/` | `types.go` | `ErrInvalidBackupRequest`、`ErrUnknownManifestVersion`、`ErrTamperedManifest`、`ErrBackupUnavailable`、`ErrBackupCleanupRequired` |
| `internal/center/recordrestore/` | `types.go` | `ErrTargetNotEmpty`、`ErrIncompatibleRestore`、`ErrMissingArtifact`、`ErrResurrectionBlocked`、`ErrRestoreUnavailable` |

命名约定：

- 公开 sentinel：`Err<Subject><Condition>` 全大写驼峰（`ErrMonitoringInstanceNotFound`）。
- 包内私有 sentinel：`err<Subject><Condition>`（参考 `internal/center/targets/probe_config.go:12` 的 `errInvalidProbeItemInput`）。
- sentinel 文案（`errors.New("...")` 内的字符串）使用全小写英文，**禁止句末标点**（与 Go stdlib 风格一致）。

---

## 错误包装：`%w` vs `%v`

- **必须用 `%w`**：当上层需要 `errors.Is` / `errors.As` 判定底层 sentinel 时。这是绝大多数包装场景。
  - 例：`store/sync_batches.go:51` `fmt.Errorf("begin sync batch transaction for monitoring instance %q: %w", batch.MonitoringInstanceID, err)`
  - 例：`auth/seed.go:28` `fmt.Errorf("count users: %w", err)`
  - 例：`observations/service.go:53` `fmt.Errorf("%w: probe_item_id %q not found", ErrInvalidProbeObservation, observation.ProbeItemID)` — sentinel 放在格式串前面，便于 `errors.Is` 判定 `ErrInvalidProbeObservation`。
- **可以用 `%v`**：仅当确认底层 error 是文案性、不参与上层判定时（极少数）。当前代码库几乎全部用 `%w`。
- **多层分类**：需要同时保留稳定策略和底层 typed cause 时，优先使用带 `Unwrap() error` 的 typed wrapper；调用方用 `errors.Is` / `errors.As`，面向日志的 `Error()` 不得格式化不受信的底层文案。

---

## HTTP 层错误转换

### 通用 handler

非 agent 端点统一通过 `handlers.writeError(w, status, message)`（`internal/center/http/handlers/json.go:28-30`）返回 `{"error": "<message>"}` JSON 体。典型转换片段（`internal/center/http/handlers/targets.go`）：

```go
if errors.Is(err, targets.ErrTargetNotFound) {
    writeError(w, http.StatusNotFound, "target not found")
    return
}
if errors.Is(err, targets.ErrTargetMetadataConflict) {
    writeError(w, http.StatusConflict, "metadata conflict")
    return
}
writeError(w, http.StatusInternalServerError, "internal server error")
```

约定：

- 错误判定一律 `errors.Is(err, <sentinel>)`，禁止 `err == sentinel` 直比。
- handler **不向客户端泄露原始 `err.Error()`**：必须把领域 sentinel 翻译为短英文 message，例如 `"target not found"` / `"invalid input"` / `"internal server error"`。
- 兜底分支：所有未识别 error 一律 `writeError(w, http.StatusInternalServerError, "internal server error")`（参考 `targets.go:36`）。
- 405 走 `writeError(w, http.StatusMethodNotAllowed, "method not allowed")`（参考 `targets.go:41`）。
- 同一 handler 内出现 2 个以上 sentinel 分支时使用 `switch { case errors.Is(...) }`（参考 `internal/center/http/handlers/auth.go:128-131`、`runtime_controls.go:63-66`）。
- 需要前端分支的 409 必须走 `writeCodedError` 并带稳定 `code`。当前 VPS 普通 PATCH 使用 `vps_asset_conflict` 与 `vps_asset_readonly`；订阅创建使用 `invalid_idempotency_key` 与 `idempotency_key_reused`。不要只返回英文 `error` 而让中文 UI 回退到 `error.message`。

## panic 政策

**实现缺口**：`internal/center/recordauthority/state.go` 的 `mustComposeCanonicalJSON` 在 JSON marshal 失败时仍调用 `panic`（审计时为 652–657 行）。这与本节零 panic 要求不一致；本次仅迁移文档，不修改业务实现。影响是该异常可终止进程，后续修复应传播错误并补回归。

**合同要求：生产代码（`internal/`、`agent/`、`cmd/` 下非 `_test.go`）不得使用 panic**。任何错误必须返回 `error`；当前已知偏差见上方实现缺口。

- 启动期不可恢复的失败：入口函数记录 `slog.Error(..., "error", err)` 后 `os.Exit(1)`（参考 `cmd/houfeng-center/main.go`、`cmd/houfeng-agent/main.go`）。center 配置加载或 logging 初始化失败发生在 `setupLogging` 完成前，因此只写 stderr；初始化成功后的启动失败会进入配置后的 stdout 或 stdout+file handler。
- 不要用 `panic` 做"不可能发生"的断言；用类型 / 接口契约让编译器保证。
- 测试代码内 `t.Fatalf` 是替代 `panic` 的标准做法，**不要在测试里用 `panic`**。

---

## 反模式 / Common Mistakes

> 这些是当前代码已经避免的写法，新代码也别做。

- ❌ **忽略 error**：`_ = repo.X(...)` / 直接丢弃 `err`。例外仅有 `defer tx.Rollback(ctx)` 这种 commit 之后无意义的清理（`store/sync_batches.go:53-55`），写注释说明原因。
- ❌ **字符串比较 error**：`if err.Error() == "..."`。永远用 `errors.Is` / `errors.As`。
- ❌ **handler 直接返回原始 error 给客户端**：`writeError(w, 500, err.Error())` 会泄露内部细节（包括 SQL 列名、表名、栈片段）。统一使用预定义短文案。
- ❌ **handler 内自定义 ad-hoc 错误码字符串**：agent 端点必须复用 `agentapi.ErrorCode*` 常量；普通端点保持英文短文案统一。
- ❌ **在 worker 内 `panic` 把整个进程带挂**：worker 错误一律 `s.logger.Error(...)` 后继续下一轮（参考 `retention/worker.go:71-87` 的 load/validate/apply 三阶段都吞错继续）。仅当 ctx 取消时返回 nil 终止。
- ❌ **包装时丢 `%w`**：`fmt.Errorf("...: %v", err)` 会切断 `errors.Is` 链路。除非显式想隐藏内部 error 类型，否则一律 `%w`。
- ❌ **在领域 sentinel 文案里加句号 / 感叹号**：违反 stdlib 风格。
- ❌ **绕过 `writeError` 写自己的 JSON 错误体**：所有 HTTP 错误响应都必须通过 `writeError` 或 `writeAgentAPIError`，否则前端 / agent 解析不到统一字段。

---

## 已知 gap

- `internal/center/observations/service.go:59,68` 的 `fmt.Errorf` 把 sentinel 包在 `%w` 后面携带上下文，文案有时较长（含 `probe_kind`、`error_code` 值）。前端 / agent 不会展示这段文案，仅 server 日志里看得到——保持现状。
- `agent/enroll/client.go:97-116` 的 `decodeRemoteError` 在 body 不是合法 JSON 时降级为 `RemoteError{Code: ""}`。后续 agent 应避免依赖 `Code != ""` 假设；如果发现脆弱点应回头补 sentinel。
