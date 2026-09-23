# 监控与命令审计合同

## MonitoringInstance command action durability, global audit, and output TTL

### 1. Scope / Trigger

- Trigger: 修改 MonitoringInstance action / remote command / global audit 链路时必须加载本节，包括 `POST /api/monitoring-instances/{monitoring_instance_id}/actions`、`GET /api/command-audits`、`agentapi.PendingAction`、`agentapi.CommandResult`、`syncing.CommandResult`、`monitoringinstances.last_action`、`monitoring_instance_command_action_audit`、`store/command_actions.go`、`store/command_audits.go` 或 `store/sync_batches.go`。
- 目标：单 pending action 模型下保持 command identity 可追踪，加入 backend-owned sensitivity / confirmation / permanent metadata audit / 24h output TTL，并在用户或实例永久删除后仍可稳定分页追溯；任何永久审计路径都不得保存或返回 stdout/stderr。

### 2. Signatures

- HTTP request: `POST /api/monitoring-instances/{monitoring_instance_id}/actions` with body `{"command_id":"uptime"}`；sensitive command 必须是 `{"command_id":"systemctl_status","confirmed_sensitive":true}`。
- HTTP response: `{"action_id":"act_xxx","command_id":"uptime","status":"pending"}`。
- Agent plan: `agentapi.PendingAction{ActionID, CommandID}` serializes as `action_id` + `command_id`。
- Agent result: `agentapi.CommandResult{ActionID, CommandID, Stdout, Stderr, ExitCode}` serializes as `action_id` + `command_id` + output fields。
- DB state: `monitoring_instances.pending_action_id`, `monitoring_instances.pending_action_command_id`, and `monitoring_instances.last_action jsonb`。
- DB audit: `monitoring_instance_command_action_audit(audit_id, action_id?, monitoring_instance_id, monitoring_instance_name_snapshot, command_id, sensitivity, event_type, actor_user_id?, actor_username_snapshot, actor_display_name_snapshot, source, exit_code?, occurred_at, details)`；`event_type in ('queued','dispatched','completed','rejected')`，`rejected` 是唯一允许 `action_id is null` 的事件。
- Read API: `GET /api/command-audits`；首次请求支持 `window=24h|7d|30d|all|custom`、custom bounds、实例/命令/敏感级别/outcome/actor/action ID 与 `limit=1..100`，续页只接受 opaque `cursor`。
- Read model: `commandaudits.Query -> commandaudits.Page`；普通 action 按 `action_id` 分组，拒绝按 `audit_id` 分组，固定执行一条 action query 和一条 page events query。
- Backend metadata source: `internal/contracts/agentapi.KnownCommandDefinitions()` owns command IDs and `standard|sensitive` sensitivity tiers.

### 3. Contracts

- Current sensitivity tiers:
  - `standard`: `df_h`, `free_m`, `uptime`
  - `sensitive`: `top_head`, `journalctl_u`, `systemctl_status`, `dmesg_err`, `docker_ps`
- Backend is the enforcement authority for sensitivity. Frontend command metadata is presentation only.
- Queueing an action writes both pending columns and `last_action={"status":"pending","action_id":...,"command_id":...,"sensitivity":...,"queued_at":...}` so API/UI readers see pending immediately.
- Sensitive commands require `confirmed_sensitive:true` before queueing. 对认证会话、已知敏感命令、真实且可执行实例的缺确认请求，handler 必须在生成 action ID 前写且只写一个 `rejected`，`details` 精确为 `{"reason":"sensitive_confirmation_required"}`，随后仍返回 400；无效 JSON、未知命令以及不存在/归档/未绑定/暂停实例不写拒绝审计。
- 所有 `queued` / `dispatched` / `completed` / `rejected` 都必须调用 `insertCommandActionAudit`；helper 使用 `INSERT … SELECT` 从当前实例/用户生成快照，并要求 `RowsAffected()==1`。`rejected` 的同一条 INSERT 还必须重新检查 `archived_at is null`、`binding_status='已绑定'`、`monitoring_status<>'暂停'`，避免 handler 读取后状态变化仍留下不可信拒绝；0 行按审计完整性失败返回 500。调用方不得自行拼接审计 INSERT。
- Queueing inserts a `queued` audit event in the same transaction as pending state, with `source='web'` and `actor_user_id` when a browser session user is available.
- Sync dispatch clears `pending_action_*` columns to prevent duplicate dispatch, but rewrites the same pending `last_action` to keep the in-flight identity durable until a matching result arrives. Dispatch must preserve queued `sensitivity` and `queued_at` from the existing pending `last_action`; only missing legacy values may fall back to backend command metadata and dispatch time.
- Dispatch inserts a `dispatched` audit event only after the clear update affects one row, with `source='agent_sync'`.
- Command result storage must include the real `command_id` and update `last_action` to `status="done"` only when current `last_action` is still `pending` with the same `action_id` and `command_id`.
- Completion inserts a `completed` audit event only after the guarded result update affects one row, with `source='agent_sync'` and `exit_code`; stale result `UPDATE 0` must not create audit rows.
- Audit rows are metadata only。具名数据库约束递归禁止 `details` 任意层出现 stdout/stderr；Go read model 和 HTTP response 也只映射 allowlist fields，不定义/透传 `details`、stdout 或 stderr。Handler 的 action/event/instance/actor response DTO 必须由 handler 自有并逐字段复制，不得把领域 JSON 类型直接嵌入 response；否则领域类型以后增加内部字段会静默扩大 API。
- 实例/用户外键在永久审计升级后解除；迁移只能删除约束列包含 `monitoring_instance_id` 或 `actor_user_id` 的目标 FK，必须保留审计表以后可能拥有的其他外键。稳定 ID 是权威身份，名称/用户名/显示名是事件时快照。永久清理实例或删除用户不删除审计，管理审查的 `command_action_audit_count` 属于 evidence，但不计入 cleanup 的 `deleted_reference_count`。
- 旧二进制兼容依靠三个快照列 `not null default ''`；旧式 queued、dispatched、completed 三种 INSERT 都必须实测仍可写，读取空快照时回退稳定 ID。不要在回滚时自动恢复 cascade 外键。
- Read API 首次请求固定上下界；cursor 使用 versioned base64url JSON 封装规范化筛选、limit、固定 bounds 与 `(before_started_at,before_id)`。排序固定为 action `started_at desc,id desc`、event `occurred_at asc,audit_id asc`；outcome 优先级为 rejected → completed(exit 0 succeeded, otherwise failed) → dispatched → queued。
- `monitoring_instance` 和 `actor` 只做转义后的字面量 `ILIKE` 子串匹配；`%`、`_`、`\` 不得成为通配符。默认 30 天 + limit 20，全局时间/action 索引与固定两次查询是当前容量边界。
- Completed `last_action` includes `completed_at`, `output_expires_at = completed_at + 24h`, and `output_expired:false` while output is visible.
- Read-side scan must hide stdout/stderr and return `output_expired:true` once `output_expires_at <= now`, even before retention physically rewrites persisted JSON.
- Retention cleanup must remove expired `last_action.stdout` / `last_action.stderr` and set `output_expired:true`; action ID, command ID, status, exit code, completion time, expiry time, and sensitivity metadata remain.
- `last_action.status` currently uses only `pending` and `done`; command success/failure is represented by `exit_code`, not by `success` / `failed` status strings.
- Go `monitoringinstances.LastAction.ExitCode` must stay nullable (`*int`) with `omitempty`: pending actions omit it, while completed success still serializes `exit_code: 0`.
- `last_action` is the current visible action state, not a full audit log. Do not infer historical command execution from it after another action is queued.
- 系统目前只有 admin 角色；读取沿用 session/same-origin，不在本链路伪造角色隔离。第二种真实角色出现时按 GitHub #381 同时设计命令授权、审计读取范围与 `authorization_denied` 审计。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| Missing `command_id` in MonitoringInstance action request | 400 `command_id required` |
| Unknown `command_id` | 400 `unknown command_id`; no repository write |
| Sensitive command missing confirmation, executable instance | Insert exactly one `rejected`; no action/queue/`last_action` change; return 400 |
| Sensitive command missing confirmation, missing/archived/unbound/paused instance | Keep confirmation 400 priority; do not write rejected audit |
| Standard `command_id` without confirmation | Queue action |
| Unknown monitoring instance | 404 `monitoring instance not found` |
| MonitoringInstance is not bound | 409 `monitoring instance agent not bound` |
| monitoring instance monitoring is paused | 409 `monitoring instance monitoring is paused` |
| Agent result lacks `action_id` or `command_id` | Ignore the result row; do not overwrite `last_action` |
| Agent result identity does not match current pending `last_action` | Ignore the result row; do not overwrite `last_action` |
| Completed output `output_expires_at <= now` | API omits stdout/stderr and returns `output_expired:true` |
| DB write failure while queueing/dispatching/storing | Return wrapped repository error; handler maps to 500 where applicable |
| Trusted rejection lookup/audit write fails | Return 500; do not silently return the ordinary 400 |
| Instance becomes archived/unbound/paused before rejected INSERT snapshot | INSERT 0 rows; return 500 and do not create an untrusted audit |
| Invalid command-audit enum/time/limit/cursor or cursor mixed with another query | 400 `invalid input`; repository is not called |
| User or monitoring instance permanently deleted | Audit remains queryable with stable ID/snapshot and `monitoring_instance.deleted=true` |

### 5. Good/Base/Bad Cases

- Good: user queues `uptime`, API immediately returns `command_id`, `last_action` shows pending `uptime`, agent returns matching `action_id` + `command_id`, and `last_action` becomes done with stdout/stderr/exit code.
- Good: user clicks `systemctl_status`, frontend opens a second confirmation, POST includes `confirmed_sensitive:true`, backend queues it and writes a sensitive `queued` audit event.
- Good: `systemctl_status` pending state is dispatched later; dispatch preserves `sensitivity:"sensitive"` and the original `queued_at` while writing a `dispatched` audit row.
- Good: completed output expires after 24h; API still returns command identity and exit code, but stdout/stderr are empty and retention later clears persisted output fields.
- Good: executable `systemctl_status` without confirmation writes one rejected metadata event, creates no action, and appears in `/api/command-audits?outcome=rejected` without output fields.
- Good: an archived MonitoringInstance is permanently cleaned; global audit pages still show its stable ID, name snapshot, actor snapshot and “deleted” state through the same cursor.
- Base: no pending action and no command results in a sync batch leaves `last_action` unchanged.
- Bad: writing `last_action.command_id=""` from command results makes the UI lose the command label.
- Bad: storing command results with `WHERE monitoring_instance_id = $2` only can let a stale result overwrite a newer pending action.
- Bad: audit `details` includes command stdout/stderr "for convenience"; this turns audit into long-lived sensitive output storage.
- Bad: dispatch rewrites sensitive pending state as `sensitivity:"standard"` or resets `queued_at` to dispatch time; this breaks UI/audit continuity.
- Bad:解除外键后改用 `VALUES` 直接写 monitoring instance / actor ID；这会为从未存在的实体制造永久伪审计。
- Bad:0050 遍历并删除审计表全部外键；以后新增的无关引用约束会被静默破坏。
- Bad:分页续页重新计算 `now()-30d`，或按 `started_at` 单键翻页；同时间 action 和新事件会产生重复/跳页。

### 6. Tests Required

- Agent runtime test: pending action execution returns `CommandResult.ActionID` and `CommandResult.CommandID`.
- Agent handler test: sync request conversion preserves `command_results[].command_id`.
- Store tests: queueing writes pending `last_action` with `sensitivity` / `queued_at`; queue/dispatch/completion audit insert order and metadata; dispatch clears pending columns while preserving pending JSON metadata; result update SQL guards on pending status, action ID, and command ID; `UPDATE 0` mismatch is non-fatal and has no completion audit; result storage runs before dispatching a newly queued action in the same sync transaction; TTL scan and retention clear expired stdout/stderr.
- Handler tests: trusted rejection writes once/no action，non-trusted inputs do not write，lookup/audit failures return 500；command-audit filters/cursor/cursor-only continuation、handler-owned nested response DTO 和 response allowlist are covered.
- Migration tests: 0046→0050 + repeated apply、snapshot backfill、三种旧 INSERT compatibility、只移除实例/actor FK 且保留无关 FK、named constraints and three indexes；fresh install also records 0050 once。
- Real PostgreSQL tests: deletion/cleanup retention、five outcomes、literal filters、same-time composite keyset、real handler cursor and `EXPLAIN (ANALYZE, BUFFERS)`；代表性数据必须使用 global time/action indexes、limit+1，repository query count exactly 2。
- Frontend API/page/browser tests: `postMonitoringInstanceAction` confirmation，`listCommandAudits` cursor-only，URL canonicalization/draft/load-more/deleted identity/output allowlist，以及 `/command-audit` 的 10×3 route、axe、390px named local scroll 与 Modal focus。

### 7. Wrong vs Correct

```go
// 错误：丢失 command identity，且 stale result 可覆盖当前 action。
payload := map[string]any{"action_id": result.ActionID, "command_id": "", "status": "done"}
_, err := tx.Exec(ctx, `UPDATE monitoring_instances SET last_action = $1 WHERE monitoring_instance_id = $2`, payload, monitoringInstanceID)
```

```go
// 正确：结果只落到仍匹配的 pending action。
_, err := tx.Exec(ctx, `
	UPDATE monitoring_instances
	SET last_action = $1, updated_at = now()
	WHERE monitoring_instance_id = $2
		AND last_action->>'status' = $3
		AND last_action->>'action_id' = $4
		AND last_action->>'command_id' = $5`,
raw, monitoringInstanceID, "pending", result.ActionID, result.CommandID)
```

```go
// 错误：dispatch 时丢掉 queued metadata，把 sensitive 命令改成 standard。
raw, _ := marshalPendingLastAction(actionID, commandID, "standard", time.Now().UTC())
```

```go
// 正确：dispatch 从现有 pending last_action 继承 sensitivity / queued_at，缺失时才兜底。
raw, _ := marshalDispatchedPendingLastAction(actionID, commandID, existingLastActionRaw, dispatchedAt)
```

```go
// 错误：解除 FK 后由调用点直接 VALUES 写入，无法证明实例/actor 曾存在。
_, _ = tx.Exec(ctx, `insert into monitoring_instance_command_action_audit (...) values (...)`)

// 正确：所有事件走同一个 INSERT … SELECT helper，并要求恰好一行。
if err := insertCommandActionAudit(ctx, tx, event); err != nil {
	return fmt.Errorf("insert command action audit: %w", err)
}
```

```sql
-- 错误：rejected 只信任 handler 较早读取的状态。
where mi.monitoring_instance_id = $3

-- 正确：写入快照再次执行可信状态安全门；状态已变化时 INSERT 0 行并 fail closed。
where mi.monitoring_instance_id = $3
  and mi.archived_at is null
  and mi.binding_status = '已绑定'
  and mi.monitoring_status <> '暂停'
```

```go
// 错误：领域类型未来增加字段时会自动进入 handler JSON。
type commandAuditActionResponse struct {
	Actor *commandaudits.ActorIdentity `json:"actor"`
}

// 正确：handler 自有 DTO，逐字段复制公开 allowlist。
type commandAuditActorResponse struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}
```

## MonitoringInstance lifecycle management and archive gates

### 1. Scope / Trigger

- Trigger: 修改 `monitoring_instances` lifecycle / monitoring / archive 字段、监控实例列表 scope、管理审查、退役 / 恢复 / 归档 / 永久清理 API、agent sync ingest、onboarding / runtime control / action / metadata 写路径。
- 目标：MonitoringInstance 是可管理对象，不只是“新增接入 agent”的副产品；错误创建的空实例要能安全清理，真实历史实例要能暂停、退役、归档和恢复，且停止状态不得继续沉淀观测数据。

### 2. Signatures

- DB columns: `monitoring_instances.archived_at timestamptz null`、`monitoring_instances.archived_reason text not null default ''`。
- List API: `GET /api/monitoring-instances?scope=active|archived|all`，省略 scope 等同 `active`。
- Review API: `GET /api/monitoring-instances/{monitoring_instance_id}/management-review`。
- Management APIs:
  - `POST /api/monitoring-instances/{id}/lifecycle/retire` with `{"reason": "..."}`
  - `POST /api/monitoring-instances/{id}/lifecycle/restore` with `{"reason": "..."}`
  - `POST /api/monitoring-instances/{id}/archive` with `{"reason":"...","confirmation_name":"<display_name>"}`
  - `POST /api/monitoring-instances/{id}/restore-from-archive`
  - `POST /api/monitoring-instances/{id}/permanent-cleanup` with `{"reason":"...","confirmation_name":"<display_name>"}`
- Domain types: `monitoringinstances.ListScope`、`ManagementReview`、`ManagementCounts`、`ManagementActions`、`LifecycleActionInput`、`ArchiveInput`、`PermanentCleanupInput`、`PermanentCleanupResult`。

### 3. Contracts

- `lifecycle_status` 不包含 `已归档`；归档只由 `archived_at is not null` 表达。允许的 lifecycle 仍是 `待接入`、`在用`、`观察中`、`不续费`、`已退役`。
- 默认列表只返回未归档实例；`scope=archived` 只返回归档实例；`scope=all` 返回全部实例，但仍沿用已有 VPS 关联工作集裁剪规则。
- `management-review` 必须一次返回实例、活跃 VPS link、数据 / 审计计数、warnings、blockers、actions 和 `empty_mistake_candidate`；前端不得自行拼多个接口后决定危险操作是否允许。
- 退役必须设置 `已退役 + 暂停`，清空 enrollment token、sync token、pending binding、pending action，并写生命周期事件。
- 从退役恢复必须设置 `观察中 + 暂停`，不自动恢复 token、action 或采集。
- 归档必须在事务内 `select ... for update` 锁定实例，重新计算 review，校验 `confirmation_name`，要求实例已退役且没有仍在当前工作集的 VPS link；成功后设置归档字段、暂停监控、撤销 token / pending binding / pending action。
- 从归档恢复必须清空归档字段并设置 `观察中 + 暂停`；恢复后仍需要用户显式接入或恢复监控。
- 永久清理必须在事务内锁定实例、重新计算 review、校验名称确认。空误创建实例可直接清理；有观测 / 事件 / 通知 / lifecycle step 等证据的实例必须先归档。删除实例前先显式删除没有 FK cascade 保护的直接引用，再删除 `monitoring_instances`，其余心跳、样本、观测、IP 质量和 VPS link 依赖 FK cascade。
- 暂停、退役或归档实例的 agent sync 必须在任何心跳、host sample、probe observation、IP 质量报告或 action result 写入前短路，返回空 plan；不要推进 `last_sync_at`。
- 已归档实例必须阻断 install command / enrollment token、binding confirm/reject/reset、metadata update、runtime resume、action queue/dispatch 等会继续接入或控制 agent 的写路径。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| invalid list scope | HTTP 400 `invalid input` |
| missing management reason | HTTP 400 `invalid input` |
| archive / cleanup confirmation name mismatch | HTTP 400 `invalid input` |
| unknown monitoring instance | HTTP 404 `monitoring instance not found` |
| archive while not retired | HTTP 409 management blocked |
| archive with active non-cancelled/non-archived VPS link | HTTP 409 management blocked |
| restore lifecycle when not retired | HTTP 409 management blocked |
| restore archive when not archived | HTTP 409 management blocked |
| non-empty instance cleanup before archive | HTTP 409 management blocked |
| archived instance metadata/onboarding/runtime/action write | HTTP 409 |
| paused/retired sync with observations/IP quality/action result | accepted sync response with empty plan, no persisted writes |

### 5. Good/Base/Bad Cases

- Good: 重复创建且没有观测证据的 MonitoringInstance 通过 management review 显示为空误创建候选，用户输入名称和原因后永久清理，VPS link 随实例 cascade 删除。
- Good: 真实运行过的实例先退役再归档；默认列表消失，但详情和归档范围仍可查看历史并可恢复。
- Base: 暂停或退役实例的旧 agent 继续同步；center 验证 token 后返回空 plan，不写入新心跳或 IP 质量报告。
- Bad: 把 `已归档` 塞进 `lifecycle_status`，破坏 VPS lifecycle action 对 `不续费` / `已退役` 的含义。
- Bad: 只在前端隐藏按钮，后端 action / onboarding / sync 写路径仍允许归档实例产生新状态。
- Bad: `ApplyBatch` 先写心跳和 IP 质量报告，再依赖 `BuildSyncPlan` 返回空计划；这会让暂停 / 退役实例继续沉淀新数据。

### 6. Tests Required

- Migration / scan tests: 新增归档字段默认值、select/scan/JSON 合同。
- Store tests: list scope、review counts/blockers/actions、retire/restore/archive/restore archive、cleanup 空实例、cleanup 非空未归档阻塞、cleanup 删除非 FK 引用。
- Sync tests: paused / retired / archived sync 不写心跳、样本、观测、IP 质量或 action result，并返回空 plan。
- Handler/router/bootstrap tests: 新 endpoint 方法、输入校验、scope 校验、错误码、router subtree 不落到 item handler / SPA fallback、bootstrap nil 断言。
- Gating tests: archived metadata、onboarding/binding、runtime resume、action queue/batch 返回冲突。

### 7. Wrong vs Correct

```go
// 错误：在 buildSyncPlan 返回空计划前已经写入观测事实。
recordHeartbeatBatch(ctx, tx, id, fingerprint, receivedAt, batch.Heartbeats)
recordIPQualityReports(ctx, tx, newID, batch.IPQualityReports, receivedAt)
plan, _ := buildSyncPlan(ctx, tx, id)
```

```go
// 正确：先读取并锁定实例状态，暂停 / 退役 / 归档时直接返回空 plan。
syncState, err := validateAcceptedSyncBatch(ctx, tx, batch)
if err != nil {
	return syncing.Result{}, err
}
if syncState.SuppressWritesAndPlan() {
	return syncing.Result{AcceptedAt: receivedAt, Plan: agentplan.SyncPlan{ProbeAssignments: []agentplan.ProbeAssignment{}}}, nil
}
```

## 模型层关键不变量

> 来源：当前代码与 `docs/design/product-and-architecture.md`。本节承接原根规范中的模型不变量；历史架构仅作背景。**任何 SQL / 仓库 / 服务改动都必须先验证这些不变量没被破坏**。

- **VPS 是主服务器控制面对象**，拥有 provider identity、业务生命周期、用途、续费 / 迁移 / 取消决策、账单证据及服务 / 域名上下文；`vps_assets.lifecycle_status`、`usage_status`、`renewal_decision` 是人工业务状态的唯一来源。
- **Subscription 是 VPS 范围内的账单事实**，价格、币种、计费周期、续费日期、自动续费、付款方式和备注服务于 VPS 决策；legacy/internal `status` 不得成为第二套面向用户的业务状态。
- **MonitoringInstance 是 VPS 范围内或显式关联的运行观测证据**。正常接入从 VPS 详情创建 Subscription 或 create-and-link MonitoringInstance；后者从 VPS 派生显示名、provider、位置、labels 与 note。已有实例的 link/unlink 是高级关联 / 历史操作，不要求重复输入 VPS 身份。
- **取消 / 退役从 VPS 生命周期工作台发起**，必须经过显式 preview、用户确认和 audit，才可联动运行时或账单事实；不得由普通 Subscription / MonitoringInstance CRUD 反向改写 VPS 业务状态。

1. **MonitoringInstance = agent 接入后的运行观测对象**。同一台机重装系统后可保持同一个 MonitoringInstance（保留 `monitoring_instance_id` 与历史时间序列）；换了硬件或明确的新 agent identity 应新建 MonitoringInstance，不要在旧 `monitoring_instance_id` 上重新绑定异种主机。指纹变化通过 `binding_status = '指纹变更待确认'` 进入 `pending_binding_*` 字段（见 `monitoring_instances` 表与 `internal/center/enrollment/`）。
2. **Target = 一个可观测入口**，地址 (`host` / `base_port`) 属于 Target；`ProbeItem` 仅描述**如何观测**它（探针种类、频率档、超时、配置），不再额外存地址。Target 与 ProbeItem 是 1:N，删除 Target 级联清理 ProbeItem (`on delete cascade`)。
3. **探针种类只有 `tcp` / `http` / `tls`**（`internal/contracts/agentapi/types.go` 中的 `ProbeKind*` 常量）。`https` 不是独立种类，而是带 TLS 配置的 HTTP 观测。新增种类必须先获得基线批准，并同步更新设计文档与契约包。
4. **健康状态 (`current_health_status`) 是派生量**（`正常 / 关注 / 告警 / 严重`），由 incident service 在写后计算并回写；**不要直接接受外部 API 的健康字段写入**。
5. **MonitoringInstance 生命周期状态 (`lifecycle_status`) 是 VPS 附属接入/收尾事实，不是独立业务状态入口**（`待接入 / 在用 / 观察中 / 不续费 / 已退役`）。普通监控 handler 只能处理运行控制、接入、绑定和 metadata；退役/不续费类变更只能从 VPS 生命周期工作台的 `asset_lifecycle` 联动路径写入，并记录审计步骤。其他写路径不应触碰该列。
6. **维护模式 (`monitoring_status = '维护中'` / `'暂停'`) 是 runtime control，不是健康状态**。维护期间观测照常落库（`maintenance_context = true`），但 incident / notification 处理需识别该上下文（参考 `store/monitoring_instances.go:74-77`、`incidents/service.go`）。暂停、维护、退役或归档 MonitoringInstance 不应保留当前 active incident 投影；incident service 必须把已有 active incidents 行政恢复为 recovered events，且不得发送恢复通知。
7. **先提交原始观测，再评估投影**：handler 经 `internal/center/syncing/` 原子提交 heartbeat / host sample / probe observation 后，调用 post-sync hook；`incidentSvc` 同时提供周期 worker 收敛。不得在 raw-ingest transaction 内发送通知，也不得用 post-sync 评估失败反转已提交的 sync 成功结果。具体 provenance、重复批次与 CAS 规则见后续场景。
8. **回填观测 (`is_backfilled = true`) 必须落库但不得触发实时告警**。请求路径仍旧 `insert`（参见 `store/sync_batches.go:188`），但 incident service 在 select 阶段对历史数据的处理需带条件分支。**不要在 incident 判定里忽略 `is_backfilled` 字段，也不要在写路径里干脆丢弃这条数据**。
9. **notification_records.channel 是真实发送通道，不是 evaluator 默认值**。`incidents.NotificationChannel` 当前只允许 `telegram` / `feishu` 作为生产通道语义；Feishu-only 发送只写 `channel='feishu'`，Telegram+Feishu 混合发送必须按 channel 写多条 record，单个 channel 失败只能把该 channel 标为 `failed`。通知策略关闭、维护/回填抑制或无可用 channel 时写 `suppressed`，但不能把 Feishu-only 或 mixed delivery 误记成 Telegram-only。

## Scenario: Replay-safe latest reads and incident projection row-version CAS

### 1. Scope / Trigger

- Trigger：修改 sync batch disposition、host/probe/heartbeat/IP-quality 的 latest/current 查询、incident snapshot/evaluator/writer、对象 summary 更新，或 current APP ACL/migration convergence。
- 目标：live/backfill 任意到达顺序都得到同一 current 事实；旧 incident evaluation 不能晚写回退新投影；生产 exact-current 数据库不因本修复被要求重建。

### 2. Signatures

- `syncing.ResultDisposition`：闭合值 `recorded | exact_duplicate | suppressed`；`Result` 保持既有 `AcceptedAt`、plan 与 pending action wire 语义。
- latest SQL ordering：`observed_at DESC, is_backfilled ASC, received_at DESC, stable_row_key DESC`；host/probe/heartbeat 稳定键为各自 `id`，IP-quality 为 `report_id`。
- snapshot：`GetObjectRowVersion(ctx, ObjectType, objectID) (string, error)`，固定读取 monitoring instance 或 target 的 `xmin::text`。
- mutation：`IncidentMutation.ExpectedObjectRowVersion string`；typed conflict 为 `incidents.ErrIncidentProjectionConflict`。
- writer guard 必须是事务的第一条数据库操作：对静态 allowlisted 对象表执行 `SELECT xmin::text ... FOR UPDATE` 后做 opaque equality compare。

### 3. Contracts

- `agent_sync_batches` exact marker 继续阻止重复 raw append；只有 `exact_duplicate` 跳过 post-sync incident 处理。`recorded` 正常评估，`suppressed` 仍允许暂停/维护/退役/归档对象的行政恢复。
- 所有 current/latest consumer 必须优先事件时间，再 live-over-backfill、接收时间与稳定主键；Go normalizer 使用相同前三项的 stable sort，完全同值保留 SQL 稳定键顺序。历史 analytics 不得被误改为只保留 latest。
- 每次 incident attempt 先读取 opaque object row version，再完整读取对象、active incidents、raw facts 与 settings；token 不解析、不排序、不持久化，也不进入日志、API 或 event payload。
- writer guard token 不匹配时，在 active delete/upsert、event insert、summary update、notification append/dispatch 和 commit 之前返回 typed conflict；成功 mutation 才替换 active set、写 events、更新 object summary 并自然推进 `xmin`。
- service 对首次 typed conflict 只做一次完整重读/重评；retry 时间使用 `max(originalTriggerTime, serviceNow)`。第二次 conflict 脱敏、60 秒按对象类型限频记录并安全 yield，已提交 sync response/plan 仍返回成功，由后续 sweep 收敛。
- inactive object 且没有 previous active incident 时不写空 mutation，避免每轮无意义推进 `xmin`。对象在枚举后、fresh Get/row-version 读取前，或完整评估后、writer guard 前被删除，都必须通过稳定 missing-object classification 安全 yield；普通 repository error 与未分类 `pgx.ErrNoRows` 保持 `errors.Is` 并 fail closed。
- notification dispatch/append 只在 mutation commit 成功后发生；CAS 不宣称外部 exactly-once，但 stale/duplicate mutation 不得主动增加 event 或 notification record。
- 本合同不新增 schema revision。current convergence 对 previous exact-current + future migration 会在 DDL 前返回 rebuild-required；因此该类修复不得偷偷新增 successor migration、ACL fragment 或现场 GRANT。base monitoring instance/target 的既有 runtime table SELECT/UPDATE 足以完成 token guard 与 summary update，column ACL 必须仍为零。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| live/backfill 同 `observed_at` | live 胜出，不受 arrival order 影响 |
| provenance 相同、`received_at` 不同 | 较新 received row 胜出；稳定键不能越级覆盖 |
| exact same provenance/time | stable row key 决胜，结果确定性 |
| exact duplicate sync | raw count、object row version、events、notifications 不变；post-sync skip |
| old mutation token 与 current `xmin` 不同 | typed conflict；零 incident/event/summary/notification 副作用 |
| unrelated object UPDATE 推进 `xmin` | 安全 false conflict；完整重读后再评估，不盲重放旧 mutation |
| 两个 writer 同持旧 token 并发 | object `FOR UPDATE` 串行化；先提交者成功，等待者比较新 token 后 conflict |
| pre-evaluation read 或 writer guard 返回稳定 object-not-found classification | safe yield；周期 sweep 继续下一对象，零 DML/event/notification |
| empty row-version / unsupported object type / ordinary DB error | fail closed；普通 error cause 保留，不得误归类为对象删除 |
| previous exact-current DB 加 future migration | rebuild-required 且 durable state 不变；本任务不建立 successor |

### 5. Good / Base / Bad Cases

- Good：live T2 后收到 backfill T1；raw facts 都保留，但 latest、active incident、health summary 与通知保持 T2 结论。
- Good：A/B 同读 token N；B 先提交并推进对象 row version，A 从 guard 等待恢复后看到新 token 并在任何 DML 前冲突。
- Base：没有竞争时，token 匹配，incident replacement、events 与 summary 在一个事务提交；通知随后独立追加。
- Bad：latest 只按 `observed_at, id` 排序；相同事件时间下晚到 backfill 或高键旧 receipt 会覆盖 live。
- Bad：先 delete active incidents 再比较 token；即使最终返回 conflict，也已破坏 current projection。
- Bad：把 token 放进 `state_change_events.payload`、日志或 API，或把 `xmin` 当可排序业务时间。
- Bad：为了获得 CAS 加 `updated_at` migration 或 current ACL successor；生产 exact-current 会在 DDL 前要求 rebuild。

### 6. Tests Required

- strict PostgreSQL 16 actual-row test 必须覆盖 backfill-first/live-first 两种顺序，并对 host/probe/heartbeat agent-version/IP summary/IP latest report/asset decision consumer 证明 backfill、received-at、stable-key 三层独立区分力；不能只做 SQL substring/fake rows。
- store/service tests 覆盖 `recorded/exact_duplicate/suppressed`，证明只有 exact duplicate skip，且 committed response/plan 不被 projection conflict 反转。
- incident writer unit tests 必须 trace guard 是事务第一条 DB op，并覆盖 match、mismatch、missing/empty/unsupported、零 DML/commit/token leakage。
- strict direct-runtime PostgreSQL 16 CAS test 必须覆盖 monitoring instance 与 target：重叠 writer 在 object guard 上确定性等待、先提交/后 conflict；无关 runtime object UPDATE 的安全 conflict；active/summary/events/notification payload/records/token 在 stale attempt 前后守恒。owner 只 seed/read/assert，生产操作使用 direct runtime role。
- service/store tests 覆盖一次 retry、第二次安全 yield/限频、fresh monotonic retry time、inactive empty no-op、枚举删除、writer-guard 删除、普通 DB/未分类 `pgx.ErrNoRows` 负控、notification-after-commit；direct-runtime PostgreSQL 删除窗口必须覆盖 MI/target 并证明对象/incident/event/notification 零副作用。
- migration regression 必须证明 previous exact-current + future migration 返回 rebuild-required/no mutation；migration/schema/current ACL diff guard 必须为空。

### 7. Wrong vs Correct

```sql
-- Wrong: arrival/key order can let an older backfill win.
order by observed_at desc, id desc

-- Correct: event time, live provenance, receipt time, then stable key.
order by observed_at desc, is_backfilled asc, received_at desc, id desc
```

```go
// Wrong: destructive projection replacement happens before stale detection.
replaceActive(ctx, tx, mutation.Active)
if currentVersion != mutation.ExpectedObjectRowVersion { return ErrIncidentProjectionConflict }

// Correct: lock and compare first; no side effect exists on conflict.
currentVersion := selectObjectRowVersionForUpdate(ctx, tx, mutation.ObjectType, mutation.ObjectID)
if currentVersion != mutation.ExpectedObjectRowVersion { return ErrIncidentProjectionConflict }
replaceActive(ctx, tx, mutation.Active)
```

## Scenario: Heartbeat incident policy and stable live recovery

### 1. Scope / Trigger

- Trigger：修改 heartbeat incident evaluator/service、settings-backed policy、heartbeat receipt snapshot 查询、心跳通知正文、`0063_tune_heartbeat_incident_policy.sql` 或对应 current APP ACL fragment。
- 目标：periodic 与 post-sync 使用同一持久化策略精确判定失联，只用服务端确认的稳定实时心跳恢复，并保持 settings/CAS/通知/迁移边界 fail closed。

### 2. Signatures

- `HeartbeatIncidentPolicy` 是必填显式值，包含 heartbeat interval、missing threshold、recovery successes（当前固定 3）与 max recovery gap（当前固定 `2 * interval`）；evaluator 不得保留 variadic threshold 或硬编码 3 的兼容 fallback。
- `SnapshotReader.ListRecentLiveHeartbeatReceipts(ctx, monitoringInstanceID, startedAt)` 只返回有界的 `SyncBatchID` 与服务端 `ReceivedAt`。
- `syncing.MaxBatchItems = 256` 是 Agent sync handler 与恢复查询共用的单批集合上限；有效 HTTP heartbeat carrier 固定为 `1..MaxBatchItems`，同一请求内每个 heartbeat 必须使用相同 `sync_batch_id`，不得在两处复制 256。

### 3. Contracts

- `N` 是首次事件边界：`N <= missed < 2N` 为关注、`2N <= missed < 4N` 为告警、`missed >= 4N` 为严重。默认 `N=12`；自定义 `N=20` 的精确边界是 20/40/80。
- periodic sweep 与 `AfterSuccessfulSync` 必须通过同一个权威 resolver 读取 persisted policy。settings read/decode/validation error 返回内部评估边界且不产生新的 heartbeat mutation/notification；periodic 返回该错误，公开 `AfterSuccessfulSync` 因 sync 事实已提交而记录稳定日志并返回 `nil`，交给 periodic 后续收敛，避免 Agent 重试命中 `exact_duplicate` 后永久跳过 post-sync。CAS retry 必须重读 record、active incidents、settings、notification toggles 与 recovery receipts。
- 非空 heartbeat carrier 全为 `is_backfilled=true` 时，公开 post-sync full attempt 必须跳过 heartbeat start/escalate/recover 并保留 active heartbeat incident，但同批其他维度仍按既有 provenance 规则评估；空 carrier 为兼容 fixture 正常评估，mixed/live carrier 也正常评估。CAS retry 保留首次触发 provenance，但仍重读上述事实与策略。
- settings 校验必须限制 `seconds * time.Second`、`2 * heartbeat interval` 与 `4 * N` 的平台安全上界，domain policy validation 也拒绝越界直接构造；missed-interval 转换必须饱和，不能因 `int` 宽度回绕。
- active heartbeat incident 只在 `started_at` 之后收到 3 个不同 `sync_batch_id` 的 non-backfilled live receipts 后恢复；排序和 gap 均使用服务端 `received_at`，相邻 gap 必须 `<= 2 * heartbeat interval`。1/2 个 receipt、duplicate batch、backfill、pre-incident、超 gap、query error 或单纯提高阈值都保留 active incident。
- PostgreSQL reader 必须先建立 `recent_live AS MATERIALIZED`：限定 monitoring instance、`received_at > started_at`、`is_backfilled = false`，按 `received_at desc, id desc` 排序，并在任何 WindowAgg/去重前 `limit $3`；arg3 固定为 `3 * syncing.MaxBatchItems = 768`。随后才按 `sync_batch_id` 去重并最终 `limit 3`；只 scan batch ID/received time，所有 query/scan/iteration error 用 `%w` 保留 cause。每个 accepted batch 最多 256 条 heartbeat，因此 768 行足以包含最近三个批次的证据；同时间戳以 `id` 稳定决胜，不假设时间唯一。绕过 ingress 上限的 legacy/direct write 至多令查询少取 distinct batch、延迟恢复，不能制造恢复，保持 fail closed。严格 PostgreSQL 证据必须先对大历史 `ANALYZE`，再以 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` 递归证明 heartbeat relation 使用 `idx_monitoring_instance_heartbeats_live_received` 的有序 `Index Scan` / `Index Only Scan`；禁止该 relation 的 Seq/Bitmap 路径，并固定约束 scan rows×loops、filter removals 与 shared hit/read blocks。既有 latest/current ordering 不得改变。
- 心跳通知只在 post-commit delivery 边界格式化为 `VPS/监控实例：<安全名称或未命名监控实例>（<稳定 ID>）`、事件标签和原 evaluator summary。名称处理固定为：CR/LF 与控制字符替换为空白、bidi controls 移除、Unicode 空白折叠并 trim；最多 80 个 rune，超限以单个 `…` 结尾且总长仍不超过 80，净化后空值 fallback。领域 event summary 保持简洁，Telegram/飞书 dispatch 文本必须与对应 `notification_records.summary` 完全一致。行政恢复继续零通知。
- `0063` 把 `center_settings.incident_defaults` 列默认值改为 12，只将现有全局顶层值 3 更新为 12 并推进 `updated_at`；全局 20/其他值与 `override_rules` 中显式 3 保持不变。partial covering index 固定为 non-backfilled `(monitoring_instance_id, received_at desc, id desc) INCLUDE(sync_batch_id)`，迁移可重复执行。
- `0063` 在 current APP ACL registry 注册 explicit empty fragment；`Privileges` callback 必须非 nil 且返回 nil，不能借恢复查询扩张 runtime privilege。不得修改历史 migration。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| missed 为 `N-1 / N / 2N / 4N` | 正常 / 关注 / 告警 / 严重 |
| 第三个 distinct live receipt 到达且相邻 gap 合规 | heartbeat incident 恢复一次 |
| receipt 为 backfill、重复 batch、事件前或 gap 超限 | active incident 保持不变 |
| settings 或 receipt query 失败 | fail closed；零新 mutation/notification，error cause 可追踪 |
| 已提交的 post-sync 内部评估失败 | 稳定日志 + 公开 hook 返回 `nil`；periodic 后续收敛 |
| 非空全回填 / mixed-live heartbeat carrier | heartbeat transition 全抑制且 active 保留 / 正常 heartbeat 评估；其他维度各按自身 provenance |
| interval、seconds 或 N 的派生计算越界 | settings/domain validation 拒绝；零回绕计算或副作用 |
| CAS 首次冲突 | 第二次 attempt 完整重读策略、对象、active 与 receipts 后重评 |
| global 3 / global 20 / override 3 应用 `0063` | 12 / 20 / 3；重复 apply 不再改变数据 |
| post-incident live history 远超 768 行 | exact covering index 有序扫描最多候选上界，WindowAgg/Sort 只消费 inner candidate；扩大旧历史量级不得带来线性 block reads；HTTP 拒绝同请求混用 heartbeat batch ID，若写入绕过单批 256 上限或单 batch ID 约束，证据不足时保持 active |
| DisplayName 含 CRLF/control/bidi、全空或超过 80 rune | 净化/折叠/Unicode 安全截断；净化后空则 fallback，稳定 ID 始终保留 |

### 5. Good / Base / Bad Cases

- Good：自定义 `N=20` 时 periodic/post-sync 都在 19 周期保持正常并在 20 周期首次关注；三个连续 live batch 后恢复。
- Good：长期 active 有数千条相同 batch 历史；查询沿 0063 exact covering index 截取最多 768 行，再 WindowAgg 去重；扩大旧历史量级时 scan rows 和 blocks 保持固定上界，三个最新 batch 的 `received_at` 相同时仍由 `id desc` 稳定返回。
- Base：阈值提高但没有 incident 后的新 live receipts，旧 heartbeat incident 继续 active。
- Bad：post-sync 走旧阈值 3、settings 错误时回退默认值，或用最新一条 heartbeat/Agent observed time立即恢复。
- Bad：通知只写显示名而无稳定 ID，或 dispatch 和 notification record 分别格式化导致正文不一致。
- Bad：把最终 `limit 3` 写在 WindowAgg 之后却没有 inner candidate limit；结果虽然只有三行，数据库仍会扫描/排序长期完整历史。

### 6. Tests Required

- evaluator/service focused tests 覆盖 N/2N/4N、自定义 20、跳级、完整 recovery 负例、真实公开 `AfterSuccessfulSync`、全回填与 mixed/live carrier、settings/receipt error 的内部返回及公开 log+ack、CAS 全量重读并保留 trigger provenance、overflow 拒绝/饱和，以及通知开关/多通道/partial failure/nil dispatcher/行政恢复；通知用例还必须覆盖 CRLF/control/bidi、超长多字节、净化后 fallback，并断言 dispatcher/所有 channel records 逐字一致。
- SQL mock tests 固定 receipt filter/dedupe/order、`recent_live MATERIALIZED`、窗口前 `limit $3`、arg3=768、最终 limit 与 `%w`；strict PostgreSQL 16 必须实际 RUN/PASS，以远超候选上限的重复历史和相同时间戳三批次证明结果。对大历史 `ANALYZE` 后，必须对捕获的生产 SQL 执行 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`：递归拒绝 heartbeat relation 的 Seq/Bitmap path，断言 exact 0063 Index/Index Only Scan、scan rows×loops/filter removals/shared blocks 固定有界，并用第二量级旧历史证明读取不线性增长；同时保留 WindowAgg/Sort/input actual rows 不超过 768 的断言。迁移测试还要证明 `0063` 数据保留/default/covering index、runtime existing SELECT 可查询且 APP ACL tuple 不扩张，SKIP 不算证据。

### 7. Wrong vs Correct

```go
// Wrong：事实已经提交后把内部评估错误返回给 Agent；exact_duplicate 重试会跳过 post-sync。
if err := evaluateAfterSync(ctx, batch); err != nil { return err }

// Correct：trigger provenance 进入可重试 full attempt；内部错误稳定记录，公开 hook ack。
trigger := heartbeatEvaluationTriggerForSync(batch.Heartbeats)
if err := evaluateMonitoringInstanceWithTrigger(ctx, batch.MonitoringInstanceID, trigger); err != nil {
    logger.Error("evaluate monitoring instance incidents after sync failed", "error", err)
}
return nil
```

全回填 trigger 只令 heartbeat class 使用 `skip(previousHeartbeat)`；不得跳过 host/target 等其他 class，也不得把 mixed/live carrier 当成全回填。

```sql
-- Wrong：最终 limit 不会限制其前面的 WindowAgg/Sort 输入。
select ... from (
  select ..., row_number() over (partition by sync_batch_id ...)
  from monitoring_instance_heartbeats
) ranked
where batch_rank = 1 limit 3;

-- Correct：先按 covering index 建立 materialized、稳定排序且有界的候选集，
-- 再做 batch 去重；$3 来自 3 * syncing.MaxBatchItems。
with recent_live as materialized (
  select sync_batch_id, received_at, id
  from monitoring_instance_heartbeats
  where monitoring_instance_id = $1
    and received_at > $2 and is_backfilled = false
  order by received_at desc, id desc
  limit $3
)
select ... from (... row_number() over (...) from recent_live) ranked
where batch_rank = 1 limit 3;
```

## Scenario: Administrative incident recovery for inactive objects

### 1. Scope / Trigger

- Trigger: 修改 `internal/center/incidents/service.go`、MonitoringInstance `monitoring_status/lifecycle_status/archived_at` 语义、Target `run_status` 语义、或 active incident mutation / notification 写入。
- 目标：用户主动暂停、维护、退役或归档的对象不再在页面上表现为“当前 active 风险”，但仍保留一条 recovered event 解释历史收敛。

### 2. Signatures

- Service paths: `EvaluateStaleMonitoringInstances(ctx, now)`、`AfterSuccessfulSync(ctx, batch, result)`、`EvaluatePeriodicState(ctx, now)`。
- Repositories:
  - MonitoringInstance repo must provide current record for MI evaluation.
  - Target repo must provide `GetTarget(ctx, targetID)` so every touched-target attempt reloads current lifecycle state before evaluating observations.
- Mutation: `IncidentMutation{ObjectType, ObjectID, Active: []IncidentRecord{}, Events: []StateChangeEventRecord{EventType: recovered}}`。

### 3. Contracts

- MonitoringInstance inactive states for incident recovery: `monitoring_status in ('暂停','维护中')`、`lifecycle_status='已退役'`、or `archived_at is not null`。
- Target inactive states for incident recovery: `run_status in ('暂停','已归档')`。
- Periodic stale sweep must close existing active incidents for inactive MonitoringInstances instead of silently skipping them.
- `AfterSuccessfulSync` must recover inactive MonitoringInstance incidents before host metric evaluation, so old samples cannot keep disk/resource incidents active after an administrative stop.
- Periodic Target sweep must recover inactive Target incidents and skip probe/TLS/trend evaluation.
- If a touched Target is inactive, `AfterSuccessfulSync` must recover prior target incidents and skip new evaluation for that target. If the Target disappears before the fresh load or writer guard, the attempt must safely yield with no projection/event/notification side effect; observation-only fallback is forbidden because it can recreate incidents for a deleted object. Ordinary repository errors still fail closed.
- Administrative recovery writes recovered events but intentionally does not call notification append/dispatch. User-initiated stop should not generate a recovery notification storm.

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| paused / maintenance / retired / archived MI has prior active incident | mutation active is empty; recovered event written; no notification records |
| inactive MI has no prior active incident | no mutation required |
| active MI stale heartbeat | normal heartbeat evaluation still applies |
| paused / archived Target has prior active incident | target mutation active is empty; recovered event written; no notification records |
| touched paused Target has fresh failing observations | administrative recovery wins; no new active probe incident |
| Target getter or writer guard returns stable object-not-found classification | safe yield with zero projection/event/summary/notification side effect |
| Target getter returns an ordinary repository error | fail closed; preserve the error cause and do not retry |

### 5. Good/Base/Bad Cases

- Good: 用户暂停监控实例后，旧 heartbeat/disk active incident 被恢复为“按暂停状态收敛”，当前异常列表清空。
- Good: 用户归档 Target 后，旧 TLS/probe active incident 被恢复，事件流保留收敛说明。
- Base: 正常运行对象继续按 stale threshold、probe failure 和 TLS expiry 生成/恢复 incidents。
- Bad: stale sweep 对暂停对象直接 `continue`，旧 active incident 永远挂在 Dashboard 上。
- Bad: 行政恢复调用通知派发，用户暂停一批对象后收到大量“恢复”消息。

### 6. Tests Required

- Service tests: MI periodic inactive recovery, MI `AfterSuccessfulSync` inactive recovery before metric evaluation, Target periodic inactive recovery, touched Target inactive recovery, all assert no notification records/sends.
- Regression tests: active MI/Target still evaluate normally; MI/Target not-found at fresh read or writer guard safely yields and the sweep continues; ordinary repository errors remain fail-closed negative controls.

### 7. Wrong vs Correct

```go
// 错误：非运行态直接跳过，旧 active_incidents 投影仍留在当前风险列表。
if !shouldEvaluate(record) {
	continue
}
```

```go
// 正确：非运行态先做行政恢复，再跳过实时评估。
if !shouldEvaluate(record) {
	return s.recoverActiveIncidentsForInactiveObject(ctx, objectType, id, now, summary)
}
```

---

## Incident / 通知 vs 业务错误

**重要区分**：

- **业务错误**：HTTP 请求处理 / 仓库读写出错，使用 `error` 返回链路，`writeError` 翻译成 4xx/5xx。
- **incident（异常事件）**：监控实例 / 目标的健康观测结论，是**派生数据**，不是 `error`。它由 `internal/center/incidents/` 的 `Service.Run(ctx)` 异步产出 `IncidentRecord` / `StateChangeEventRecord`，写入 DB 与 Telegram。

请求路径**只**收原始观测、入库；**不要在 handler / service 内把 incident 失败当成 HTTP error 抛回 agent**。incident 评估失败由 `s.logger.Error("evaluate monitoring instance incidents after sync failed", ...)` 记录后继续，不阻塞 sync 应答。

`active_incidents` 是当前状态投影，不是 append-only 审计事实。写入必须对重复评估、worker 重放和遗留确定性 `incident_id` 行保持幂等：替换对象当前 active 集合时，插入 active incident 必须使用 `on conflict (incident_id) do update` 刷新当前事实，避免 `active_incidents_pkey` 把 center 进程拖崩。状态变化历史仍由 `state_change_events` 承担，不要通过保留重复 active rows 模拟历史。

---

## Scenario: incident threshold settings order

### 1. Scope / Trigger

- Trigger: 修改 `centersettings.IncidentDefaults`、`IncidentDefaultsOverride`、`internal/center/incidents.MetricThresholds`、`/api/settings` 的 incident defaults 请求/响应，或前端监控阈值展示/设置提交。
- 目标：异常等级阈值必须严格递进，避免用户保存倒序配置后 evaluator、图表阈值线和中文等级文案互相矛盾。

### 2. Signatures

- Backend settings type: `internal/center/settings.IncidentDefaults`。
- Backend override type: `internal/center/settings.IncidentDefaultsOverride`。
- Frontend settings type: `web/src/lib/types.ts` `IncidentDefaults` / `IncidentDefaultsOverride`。
- Frontend runtime resolver: `web/src/config/thresholds.ts` `resolveThresholds(...)`。

### 3. Contracts

- CPU / memory / disk / inode 三段阈值必须满足 `warning < alert < critical`。
- IOWait / Load5 两段设置必须满足 `warning < critical`；alert 只能由中点派生。
- `settings.Validate` 是持久化与 API 输入的权威校验入口，必须拒绝倒序或相等阈值并返回可 `errors.Is(err, ErrInvalidSettings)` 判定的错误。
- override 只给出部分 incident threshold 字段时，必须先与当前已规范化的全局 `IncidentDefaults` 合成，再校验有效阈值顺序；不能与代码默认值合成。
- 前端 Settings 页提交前必须做同样校验，错误文案使用中文等级名，例如 `CPU 阈值必须满足 关注 < 告警 < 严重。`。
- 前端展示用 `resolveThresholds` 收到旧脏数据时，按 metric 回退到 `DEFAULT_THRESHOLDS`，不要渲染倒序阈值线。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| CPU `warning >= alert` 或 `alert >= critical` | `settings.Validate` 返回 `ErrInvalidSettings` |
| Load5 / IOWait `warning >= critical` | `settings.Validate` 返回 `ErrInvalidSettings` |
| override 局部字段与当前全局 defaults 合成后倒序 | `settings.Validate` 返回 `ErrInvalidSettings` |
| Settings 页用户输入倒序阈值 | 页面显示中文校验错误且不发 `PUT /api/settings` |
| `resolveThresholds` 收到倒序 runtime settings | 该 metric 使用默认阈值 |

### 5. Good/Base/Bad Cases

- Good: `CPU 80/90/95`、`IOWait 20/50`、`Load5 4/8` 保存成功，evaluator 和图表均按递进等级工作。
- Base: 全局 CPU 改成 `50/60/70`，某个 override 只把 critical 改成 `75`，按 `50/60/75` 校验后允许。
- Bad: `CPU 95/80/90` 被保存，导致关注/告警/严重语义在 evaluator 中反转。
- Bad: override 与代码默认值合成而不是当前全局配置合成，误放行或误拒绝局部阈值。

### 6. Tests Required

- `internal/center/settings/types_test.go`: 覆盖默认阈值三段/两段倒序与相等拒绝、override 局部字段按当前全局 defaults 合成校验。
- `web/src/pages/SettingsPage.test.tsx`: 覆盖倒序设置显示中文错误，且 `fetch` 只发生初始 GET、不发生 PUT。
- `web/src/config/thresholds.test.ts`: 覆盖 runtime settings 倒序时按 metric 回退默认阈值。

### 7. Wrong vs Correct

```go
// 错误：只校验范围，不校验等级顺序。
if warning < 1 || warning > 100 { return err }
```

```go
// 正确：范围校验后必须校验有效等级顺序。
if !(warning < alert && alert < critical) {
	return invalidSettings("cpu thresholds must satisfy warning < alert < critical")
}
```

---
