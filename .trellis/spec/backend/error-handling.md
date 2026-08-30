# 错误处理规范

> **项目依据**：以当前代码、`.trellis/spec/`、任务文档和 `docs/design/current/` 为准；`docs/design/v1-baseline/` 与 `docs/design/v2-houfeng/` 只作为历史背景。硬性规则只用于保护安全、数据完整性、证据真实性和当前代码/API 合同。

---

## Overview

候风后端的错误处理分三层处理，每一层职责清晰、互不串味：

| 层 | 职责 | 实现位置 |
|----|------|----------|
| **领域层** | 用 `errors.New` 定义可被上层 `errors.Is` 判定的 sentinel error；用 `fmt.Errorf("...: %w", err)` 包装下游错误并保留链路 | `internal/center/<domain>/types.go` 与 `service.go`、`agent/<subpkg>/*.go` |
| **HTTP 层** | 用 `errors.Is` 把领域 sentinel 翻译成 HTTP status + JSON 响应；agent 端点额外带 `agentapi.ErrorCode*` | `internal/center/http/handlers/*.go`，统一走 `handlers/json.go` 的 `writeError` / `writeAgentAPIError` |
| **agent ↔ center 契约层** | 用 `agentapi.ErrorResponse{Code, Message}` 携带稳定错误码字符串，agent 端解码为 `enroll.RemoteError` 用于决定是否重试 | `internal/contracts/agentapi/types.go:14-23`、`agent/enroll/client.go:97-116` |

> **关键事实**：当前 `internal/`、`agent/`、`cmd/` 的生产代码（非 `_test.go`）**完全没有 `panic`**。这是项目硬性约束，所有失败路径必须返回 `error`。

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

### Scenario: Records versioned transport errors and opaque resource denial

#### 1. Scope / Trigger

- Trigger：新增或修改 `/api/records`、`/api/record-drafts`、revision/lifecycle/permanent-delete endpoint，或扩展 Records 领域 sentinel、conflict recovery、request body/header validation 时。
- Records family 是普通 center API 的版本化例外：它使用稳定 `code/message/field_errors/recovery` DTO；其他既有普通 handler 继续使用 `writeError` 的 `{"error":"..."}`，不得借此全仓改写错误格式。

#### 2. Signatures

```go
type recordErrorResponse struct {
	Code        string             `json:"code"`
	Message     string             `json:"message"`
	FieldErrors []recordFieldError `json:"field_errors"`
	Recovery    any                `json:"recovery,omitempty"`
}

func writeRecordError(http.ResponseWriter, int, string, string, any)
func writeRecordsApplicationError(http.ResponseWriter, error)

type recordDeletionPreviewResponse struct {
	ReservationID        string                                `json:"reservation_id"`
	DeletionRequestToken string                                `json:"deletion_request_token"`
	ExpiresAt            time.Time                             `json:"expires_at"`
	OnlinePurgeScopes    []string                              `json:"online_purge_scopes"`
	SurvivingCopies      []recordDeletionSurvivingCopyResponse `json:"surviving_copies"`
	ManagedBackup        recordDeletionManagedBackupResponse   `json:"managed_backup"`
	LedgerHealth         string                                `json:"ledger_health"`
}

type recordDeletionSurvivingCopyResponse struct {
	Scope     string `json:"scope"`
	Kind      string `json:"kind"`
	CopyCount uint64 `json:"copy_count"`
}

type recordDeletionManagedBackupResponse struct {
	RetainedCopyCount    uint64     `json:"retained_copy_count"`
	MaximumRetentionDays uint32     `json:"maximum_retention_days"`
	LatestExpiresAt      *time.Time `json:"latest_expires_at"`
}

type recordDeletionExecuteRequest struct {
	ReservationID string `json:"reservation_id"`
}

type recordDeletionOperationResponse struct {
	OperationID string                       `json:"operation_id"`
	State       recorddeletion.DeletionState `json:"state"`
}
```

- `field_errors` 即使为空也必须编码为 `[]`，不能是 `null`；`recovery` 只允许 handler-owned typed DTO，非 conflict 时省略。
- `decodeRecordsRequestJSON` 统一执行 body limit、unknown/trailing JSON 拒绝；`If-Match` 与 `Idempotency-Key` 由各 mutation endpoint 在调用 application 前校验。
- permanent-delete execute body 只允许 `reservation_id`。preview 返回的 canonical `DeletionRequestTokenV1` 本身就是 execute 的 `Idempotency-Key`；handler 只接受一个、且仅一个 canonical `Idempotency-Key` header value。token 进入 body、另造 generic key、缺失、格式错误或多个 header value 都在 application 调用前拒绝。

#### 3. Contracts

- handler 只通过 `sessionctx.ActorScopeFromContext` 取得可信 actor；不得从 header/query/body 构造 project、role、group 或 source scope。
- `recordauth.ErrDenied`、record/draft/source not found 与 deletion reservation fence 对外统一为 `404 resource_not_found`，message 固定且不包含 record/source ID；客户端不能区分不存在与无权访问。
- application/store error 一律用 `errors.Is` / `errors.As` 翻译。不得返回原始 `err.Error()`，不得序列化 domain/store error、authorization evidence、canonical hash、fence/ledger detail。
- draft/revision conflict 的 `recovery` 只能包含客户端恢复所需的 allowlisted draft/revision metadata 与 payload；不得整体序列化领域结构体。未知 error 固定为 `500 internal_error`。
- record/draft handler 在任何 actor、application、path 或 method 分支前设置精确 `Cache-Control: private, no-store`；成功、opaque 404、conflict、unavailable 与 204 都不得被共享或浏览器缓存复用。
- 所有出站 draft payload（普通 item、list、publish 读取、typed conflict 的 server/local payload）必须重新通过 transport allowlist decoder。持久化 payload 即使是合法 canonical JSON，只要包含未知字段、缺必填 array 或 array 为 `null`，就返回不带原 payload/ETag 的 `500 internal_error`，不能当作客户端 400/422，也不能原样回显。
- deletion preview 只允许 `reservation_id`、一次性 `deletion_request_token`、UTC `expires_at`、固定顺序的九项 `online_purge_scopes`、identity-free `surviving_copies[{scope,kind,copy_count}]`、`managed_backup{retained_copy_count,maximum_retention_days,latest_expires_at}` 与 `ledger_health`。`surviving_copies` 必须按 adapter/kind 闭合集顺序、去重且使用正数 count；空集合编码为 `[]`。无 retained backup 时 `latest_expires_at` 编码为 `null`。
- preview 只在完整九 adapter readiness、受管备份摘要和 ledger/witness 健康都可证明时返回 `ledger_health:"healthy"` 与 token；unknown/unhealthy 状态返回 `503 deletion_safety_unavailable`，不能在 JSON 中描述内部 ledger/witness 细节。operation 响应只允许 `operation_id` 与闭合 `state`。reservation commitment、request fingerprint、record/project identity、dependency/impact digest、ledger/witness tuple、fence/release epoch 和 receipt digest 均不得进入 HTTP JSON。
- deletion operation 的 pending state 返回 `202` 并设置 `Retry-After: 1`；`not_committed` 在 POST/GET 都返回 `200`，`online_purged` 只在 GET status 返回 `200`。delete commit 已持久化后的 POST replay 即使已 `online_purged` 仍返回同一 operation 的 `202`，不能改写成新的成功语义。
- `record_purge_operations` 是可重建应用投影，不是权威删除存在性来源。status query 的 `pgx.ErrNoRows` 在 primary ledger/full-witness fallback 尚未接线时必须映射 `ErrDeletionStatusUnavailable` -> `503 deletion_status_unavailable`；不得伪造 authoritative 404 或完成。未来 fallback 只有在 ledger namespace + full witness 明确证明不存在后才能返回 opaque 404。
- production bootstrap 在九个 deletion adapter、独立 ledger/witness client 或 admission 任一未就绪时可以注册 transport，但 preview/execute 固定 `503 deletion_safety_unavailable`、status 固定 `503 deletion_status_unavailable`，且 preview 响应绝不能包含 token。测试完整 registry 不能变成 production allow-all bypass。
- 缺失/格式错误的请求 header、query 或 JSON 是 transport validation；它们在 application 调用前返回 400/413。领域内容校验是 422；并发/idempotency 状态是 409；admission/source/reservation dependency unavailable 是 503。
- 所有 Records list、nested DTO 与 `field_errors` slice 在 JSON 边界显式初始化为空 slice，保证 `[]` 而不是 `null`。draft transport 的必填 array 字段同样拒绝 `null`/缺失。

#### 4. Validation & Error Matrix

| Condition | HTTP status / code |
| --- | --- |
| malformed/unknown/trailing JSON | `400 invalid_json` |
| missing/invalid `If-Match`、`Idempotency-Key`、cursor/query | `400 invalid_request` 或 cursor 专用 `400 cursor_invalid` |
| deletion execute body 携带 `deletion_request_token` 或其他未知字段 | `400 invalid_json`；application 零调用 |
| deletion execute 缺失、格式错误或包含多个 `Idempotency-Key` header value | `400 invalid_request`；application 零调用 |
| request body 超过 limit | `413 request_too_large` |
| denied、not found、deletion reserved、source gone | opaque `404 resource_not_found` |
| draft ETag conflict | `409 draft_conflict` + allowlisted recovery（如有 typed conflict） |
| record/base revision/CAS conflict | `409 record_revision_conflict` + allowlisted recovery（如有 typed conflict） |
| idempotency key reuse/in-progress 或 record already exists | 对应稳定 `409` code |
| revision/draft/lifecycle semantic validation | `422 record_invalid` |
| runtime admission、source resolution 或 reservation dependency unavailable | `503 record_service_unavailable` |
| handler context 缺 typed actor | `503 authorization_unavailable`；正常缺失/过期 session 仍由 middleware 在 handler 前返回 401 |
| persisted/typed recovery draft payload 不符合 transport allowlist | `500 internal_error`；response 不含未知字段、原 payload 或 ETag |
| deletion preview/execute dependency、adapter readiness 或 admission 未就绪 | `503 deletion_safety_unavailable`；preview 不签发 token |
| deletion status 应用投影缺失、损坏或无法读取，且无 authoritative ledger+witness fallback | `503 deletion_status_unavailable`；不得降级为 404/完成 |
| deletion status authoritative missing 或 initiator/project-admin 授权失败 | opaque `404 resource_not_found` |
| deletion preview/token 漂移或复用 | 对应稳定 `409 deletion_preview_stale` 或 `deletion_request_token_reused`；删除 handler 不返回 generic `idempotency_key_reused` |
| deletion operation pending / POST replay after committed delete | `202` + `Retry-After: 1`，只返回 `operation_id/state` |
| deletion operation `not_committed`；GET status `online_purged` | `200`，不返回 `Retry-After` |
| 未识别 error | `500 internal_error` |

#### 5. Good/Base/Bad Cases

- Good：viewer 无权读取某 record；policy 返回 `recordauth.ErrDenied`，handler 与真正不存在时都返回完全相同的 `404 resource_not_found`。
- Good：stale draft PATCH 返回 `409 draft_conflict`，`field_errors:[]`，recovery 只含 server draft、local payload 和允许的 metadata。
- Good：当前没有合法存续副本或 retained backup；preview 仍返回固定九项在线 purge scope，`surviving_copies:[]`，backup count 为 0 且 `latest_expires_at:null`。
- Good：status application projection 丢失时返回 `503 deletion_status_unavailable`；服务端没有把“本地没有 row”误报为账本中不存在 operation。
- Base：feature 已注册但 production transaction admission gate 尚未就绪；repository fail closed，handler 稳定返回 `503 record_service_unavailable`。
- Base：Records runtime admission 已启用但后续 deletion adapters/ledger/witness 尚未接线；删除 route 可稳定返回 503，但不能签发确认 token 或调用测试 bypass。
- Bad：把 `capture_authorization`、source floor、project ID 或 store row 塞入 recovery，或通过 `err.Error()` 泄露 SQL/资源身份。
- Bad：execute body 同时发送 deletion token，再为 `Idempotency-Key` 生成另一把通用 key；这会拆分同一不可逆请求的 durable identity，并允许 body/header 漂移。
- Bad：status projection `QueryRow` 返回 `pgx.ErrNoRows` 后直接映射 `ErrDeletionOperationNotFound`；应用数据库可能落后或已恢复，这会把不可证明状态伪装成权威 404。
- Bad：把 Records 专用 DTO 推广到所有旧 API，造成既有 Web/agent 错误解析合同漂移。

#### 6. Tests Required

- `internal/center/http/handlers/records_test.go`、`record_drafts_test.go`：覆盖 400/404/409/413/422/503、200/404/409/503/204 的 exact no-store header、typed recovery allowlist、persisted server/local unknown payload fail-closed、unknown/nested trusted field、数组 `[]`/`null`、raw error/evidence 禁泄露以及 application 零调用。
- `internal/center/http/router_test.go`：覆盖 session middleware、feature-off API 404/SPA 隔离、static/prefix route dispatch。
- `internal/center/http/handlers/record_deletions_test.go`：覆盖 preview/execute/status allowlist、固定九项 scope、空 `surviving_copies:[]`、无备份 `latest_expires_at:null`、非法/乱序 summary fail-closed、header-only canonical token、body token/缺失/格式错误/多个 header 的 application 零调用、opaque 404、409、503、pending `202`/`Retry-After`、`not_committed`/status `online_purged` 200 和同 operation replay。
- `internal/center/recorddeletion/preview_summary_test.go`：覆盖 adapter 与 aggregate survivor 的 nil、unknown、zero-count、duplicate、kind/scope 乱序拒绝。
- `internal/center/recorddeletion/service_test.go`、`internal/center/store/record_deletions_test.go`：覆盖 initiator/project-admin status 授权、非授权 opaque missing、content-free projection 和 projection missing fail-closed 503。
- `cmd/houfeng-center/bootstrap_test.go`：覆盖 legacy mode 不构造 Records handler、RuntimeAdmission mode 显式接线、transaction admission 未就绪时稳定 503，以及未接线 deletion dependencies 时 preview 无 token。
- 提交前至少运行：

  ```bash
  go test -race ./internal/center/http/handlers ./internal/center/http ./cmd/houfeng-center -count=1
  git diff --check
  ```

#### 7. Wrong vs Correct

```go
// 错误：泄露底层错误并让授权拒绝可与不存在区分。
if errors.Is(err, recordauth.ErrDenied) {
	writeRecordError(w, http.StatusForbidden, "record_denied", err.Error(), resource)
}

// 正确：统一 opaque 404；recovery 只由明确的 typed conflict 分支构造。
if errors.Is(err, recordauth.ErrDenied) || errors.Is(err, records.ErrRecordNotFound) {
	writeRecordNotFound(w)
}
```

```go
// 错误：删除 token 与另一把 generic key 分别出现在 body/header。
type executeRequest struct {
	ReservationID        string `json:"reservation_id"`
	DeletionRequestToken string `json:"deletion_request_token"`
}
genericKey := request.Header.Get("Idempotency-Key")

// 正确：body 只有 reservation；唯一 canonical header token 是 durable request identity。
type executeRequest struct {
	ReservationID string `json:"reservation_id"`
}
token, ok := recordDeletionIdempotencyToken(request)
if !ok {
	writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid deletion request", nil)
	return
}
```

```go
// 错误：应用投影缺失不等于权威 ledger namespace 中不存在。
if errors.Is(err, pgx.ErrNoRows) {
	return recorddeletion.ErrDeletionOperationNotFound
}

// 正确：没有 primary ledger + full-witness fallback 时保持状态不可证明。
if errors.Is(err, pgx.ErrNoRows) {
	return recorddeletion.ErrDeletionStatusUnavailable
}
```

### MonitoringInstance onboarding install-command endpoint

`POST /api/monitoring-instances/{monitoring_instance_id}/install-command` 是登录用户触发的普通 center API，不是 agent contract endpoint。它仍走 `writeError`，但有两个配置类 409 是产品契约，不能降级成 500：

| Condition | HTTP status | Message |
| --- | --- | --- |
| `HOUFENG_PUBLIC_BASE_URL` 未配置 | 409 | `public base URL is not configured` |
| agent release version 为空或 `dev` | 409 | `agent release version is not configured` |
| MonitoringInstance 不存在 | 404 | `monitoring instance not found` |
| 其他 repository error | 500 | `internal server error` |

前端必须展示这些短文案，引导 operator 回到部署配置或发布流程；不要在浏览器侧用 `window.location.origin` 兜底绕过 409。

### agent 端点（contract 层）

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

### Scenario: agent durable sync queue 的远端失败策略

#### 1. Scope / Trigger

- Trigger：修改 `agent/runtime` 的 queue flush、`agent/syncqueue` authority/backfill、`agent/enroll.RemoteError` 消费或 agent sync 错误日志。
- 目标：永久拒绝或脏队列项不能阻塞后续当前心跳，同时不能把临时故障误删，也不能把持久队列内的凭据带入日志。

#### 2. Signatures

- typed remote cause：`*enroll.RemoteError{StatusCode, Code, Message}`。
- 当前 queue authority：runtime 已加载的 `monitoring_instance_id`、`sync_token` 与本机 `fingerprint` 精确三元组。
- 本地 policy result：`kind=remote|transport`、`action=discard|terminal|retry`、可选 allowlisted `status` / `agentapi.ErrorCode*`。
- stale backlog durability primitive：`SyncQueue.DeleteMany(context.Context, []string) error`；生产 `FileStore` 必须在一次加锁、一次原子文件替换中删除整批 stale ID。
- `FileStore` 返回给 runtime 的 entry ID 必须唯一：enqueue 遇到 heartbeat batch ID 冲突时分配确定性 suffix，读取 legacy/hostile 持久文件时也在排序后确定性规范化 duplicate/empty ID，保证单条 ack 不会删除尚未发送的同 ID fact。
- 该规范化只作用于本地 `Entry.ID`，不得改写 carrier `SyncBatchID`。重复 SyncBatchID 仍是 center 的同一幂等事实，不能宣称 suffix 后会成为两个独立 center facts。大量同 ID 持久项的规范化、以及已有密集 `base-N` suffix 时的新 ID 分配都必须近似线性，不得为每个候选 suffix 重扫整个队列造成 O(n²) recovery。
- wrapped policy / remote operation / pre-sync local operation / local queue error 必须提供 `Unwrap() error`，使调用方仍可通过 `errors.As` / `errors.Is` 取得 typed cause；其 `Error()` 只格式化稳定、脱敏字段。
- 每轮调度上界：`maxBacklogSyncAttemptsPerRound = 2`；`Enqueue` 返回的本地 `Entry.ID` 是本轮 current carrier 的唯一身份。

#### 3. Contracts

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

#### 4. Validation & Error Matrix

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

#### 5. Good / Base / Bad Cases

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

#### 6. Tests Required

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

#### 7. Wrong vs Correct

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

## Incident / 通知 vs 业务错误

**重要区分**：

- **业务错误**：HTTP 请求处理 / 仓库读写出错，使用 `error` 返回链路，`writeError` 翻译成 4xx/5xx。
- **incident（异常事件）**：监控实例 / 目标的健康观测结论，是**派生数据**，不是 `error`。它由 `internal/center/incidents/` 的 `Service.Run(ctx)` 异步产出 `IncidentRecord` / `StateChangeEventRecord`，写入 DB 与 Telegram。

请求路径**只**收原始观测、入库；**不要在 handler / service 内把 incident 失败当成 HTTP error 抛回 agent**。incident 评估失败由 `s.logger.Error("evaluate monitoring instance incidents after sync failed", ...)` 记录后继续，不阻塞 sync 应答。

`active_incidents` 是当前状态投影，不是 append-only 审计事实。写入必须对重复评估、worker 重放和遗留确定性 `incident_id` 行保持幂等：替换对象当前 active 集合时，插入 active incident 必须使用 `on conflict (incident_id) do update` 刷新当前事实，避免 `active_incidents_pkey` 把 center 进程拖崩。状态变化历史仍由 `state_change_events` 承担，不要通过保留重复 active rows 模拟历史。

---

## panic 政策

**生产代码（`internal/`、`agent/`、`cmd/` 下非 `_test.go`）零 panic**。任何错误必须返回 `error`。

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
