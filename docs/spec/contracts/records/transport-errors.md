# Records HTTP 与错误合同

## Scenario: Records versioned transport errors and opaque resource denial

### 1. Scope / Trigger

- Trigger：新增或修改 `/api/records`、`/api/record-drafts`、revision/lifecycle/permanent-delete endpoint，或扩展 Records 领域 sentinel、conflict recovery、request body/header validation 时。
- Records family 是普通 center API 的版本化例外：它使用稳定 `code/message/field_errors/recovery` DTO；其他既有普通 handler 继续使用 `writeError` 的 `{"error":"..."}`，不得借此全仓改写错误格式。

### 2. Signatures

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

### 3. Contracts

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

### 4. Validation & Error Matrix

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

### 5. Good/Base/Bad Cases

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

### 6. Tests Required

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

### 7. Wrong vs Correct

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
