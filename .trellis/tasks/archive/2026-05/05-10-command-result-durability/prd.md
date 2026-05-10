# Command result durability

## Goal

关闭 `docs/release/v1-gap-checklist.md` 中 gap #21：让 Node action / remote command 结果链路具备完整 command identity，并避免 pending action 在 dispatch 后、agent 尚未回传结果前被中心过早清空导致状态不可恢复。

## What I already know

- 用户要求继续推进，但 release/publish workflow 后续再考虑；本任务不处理 release/publish。
- 用户此前明确要求不使用 subagent；本任务在主会话直接执行。
- 当前分支是 `fix/command-result-durability`，从干净 `main` 创建。
- `docs/release/asset-ledger-roadmap-completion.md` 判定 Asset Ledger 计划当前功能闭合；真实 40+ VPS 数据执行 deferred，不作为本任务。
- `docs/release/v1-gap-checklist.md` gap #21 仍 open：`CommandResult` 缺完整 `command_id`，`store/sync_batches.go` 当前写 `last_action.command_id = ""`，pending action 在同一 sync transaction 内 dispatch 后立即 clear。
- Agent 侧已有 `agentapi.PendingAction{action_id, command_id}` 与 `agentapi.CommandResult`；需要确认结果结构是否包含并回传 `command_id`。
- Web NodeDetailPage 使用 `last_action.command_id` 显示命令标签，空值会导致结果缺少命令身份。

## Assumptions

- 本任务只修现有 Node action 结果链路，不新增任意命令、不扩大 whitelist、不做产品边界调整。
- 兼容现有单 pending action 模型，不引入新的 actions 表，除非现场代码证明 JSONB/节点列无法安全表达状态。
- Pending action dispatch 后应保留足够状态，让 UI 能看到 `pending`，直到匹配的 agent result 写入最终 `last_action`。
- Agent result 必须带回 `action_id` 与 `command_id`，center 只接受能匹配当前 pending/in-flight action identity 的结果。
- 如果 agent 回传未知或不匹配结果，应忽略或保守记录错误，不得覆盖当前节点的真实 pending/in-flight 状态。

## Requirements

- Contract:
  - `internal/contracts/agentapi.CommandResult` 明确包含 `command_id`。
  - Center-side `syncing.CommandResult` 与 handler conversion 保留 `command_id`。
  - Agent runtime 执行 pending action 后把原始 `pending_action.command_id` 写入 result。
- Store durability:
  - Dispatch pending action 时不要立即丢失 action identity。
  - Node `last_action` 在 dispatch 后能表达 `status='pending'`，包含 `action_id` 与 `command_id`。
  - Agent result 回来后，center 用 result 中的 `action_id` / `command_id` 写最终 `done` 状态，并用 `exit_code` 表达成功或失败。
  - 不匹配当前 in-flight/pending identity 的 result 不应覆盖 `last_action`。
- UI:
  - NodeDetailPage 现有 `last_action.command_id` 展示应自然获得真实命令标签；如类型需要更新，同步 `web/src/lib/types.ts`。
  - 不新增 UI 功能，只保证现有 command drawer / result 显示不回归。
- Docs:
  - `docs/release/v1-gap-checklist.md` 将 gap #21 标为 Closed，并说明未扩展 Docker/exec 边界。
  - 如形成新的后端契约，更新 `.trellis/spec/backend/*`。

## Acceptance Criteria

- [x] Agent `CommandResult` 回传包含 `command_id`。
- [x] Center handler / syncing batch 保留 `command_id`。
- [x] Dispatch pending action 后，node `last_action` 可表达 `pending` 且包含 `action_id` / `command_id`。
- [x] Result 写入后，node `last_action` 包含真实 `command_id`，不再写空字符串。
- [x] 不匹配 action identity 的 result 不覆盖当前 action 状态。
- [x] Existing node action API / runtime tests 不回归。
- [x] 相关 gap 文档和 Trellis spec 同步。
- [x] `git diff --check` 通过。
- [x] `go test ./agent/... ./internal/... ./cmd/... ./db/...` 或 `make verify-go` 通过。
- [x] 若前端类型/测试有变更，`make verify-web` 通过。

## Verification

- `git diff --check`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build ./scripts/verify.sh`

Notes:
- Full verification passed on 2026-05-10.
- Local `npm ci` reported a non-blocking engine warning because this machine runs Node v24.14.1 while `web/package.json` requires Node 22.x. CI remains pinned to Node 22.
- Vite reported the existing chunk-size warning after build; build completed successfully.

## Definition of Done

- Work committed on a non-main branch after user confirms commit plan.
- Trellis task archived and journal recorded after work commits.
- PR opened, PR CI monitored until green, then merged.
- Local `main` synced to `origin/main`.
- Post-merge main CI monitored to success.
- Release/publish workflow is not part of this task.

## Out of Scope

- 不新增或更改 agent command whitelist。
- 不实现 full remote command audit log 或多 action 队列。
- 不扩展 Docker/exec 产品边界。
- 不改 Feishu / notification channel model。
- 不改 `/api/events` response envelope。
- 不处理真实 VPS 数据 dry-run/import。
- 不新增 release/publish automation。

## Technical Notes

- Likely files:
  - `internal/contracts/agentapi/types.go`
  - `agent/runtime/runtime.go`
  - `agent/runtime/runtime_test.go`
  - `internal/center/http/handlers/agent.go`
  - `internal/center/syncing/service.go`
  - `internal/center/store/sync_batches.go`
  - `internal/center/store/sync_batches_test.go`
  - `internal/center/store/nodes.go`
  - `internal/center/nodes/types.go`
  - possibly `web/src/lib/types.ts` / `web/src/pages/NodeDetailPage.test.tsx`
- Relevant specs:
  - `.trellis/spec/guides/branch-workflow-governance.md`
  - `.trellis/spec/guides/cross-layer-thinking-guide.md`
  - `.trellis/spec/guides/code-reuse-thinking-guide.md`
  - `.trellis/spec/backend/index.md`
  - `.trellis/spec/backend/directory-structure.md`
  - `.trellis/spec/backend/database-guidelines.md`
  - `.trellis/spec/backend/error-handling.md`
  - `.trellis/spec/backend/quality-guidelines.md`
  - `.trellis/spec/backend/logging-guidelines.md`
  - `.trellis/spec/web/state-and-data.md`
  - `.trellis/spec/web/quality-guidelines.md`
