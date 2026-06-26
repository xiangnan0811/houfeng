# Sync Auth And Command Governance Design

## Design Summary

The branch will make sync authentication header-first and move command governance into backend-owned command metadata. The frontend will present the same metadata for usability, but backend validation remains authoritative. Command action output remains a short-lived current-state field; durable audit records store metadata only.

## Approach Options

### Option A: Minimal Compatibility Hardening

Keep JSON `sync_token` accepted when the header is missing, add command UI confirmation only, and document TTL as a manual cleanup rule.

- Pros: smallest code change.
- Cons: leaves the original pre-body sync-token issue partially open, and UI-only confirmation is bypassable.

### Option B: Strict Header Auth Plus Backend-Enforced Governance

Require bearer auth before body reads, define backend command sensitivity metadata, require explicit confirmation for sensitive commands, add metadata-only audit, and add output expiry to current `last_action`.

- Pros: directly closes the review caveats with server-side guarantees and clear tests.
- Cons: touches API, DB, sync transaction, retention, and web UI.

### Option C: Full Command Management Subsystem

Introduce command definitions as a database-managed admin surface with audit browsing, TTL settings UI, and per-command enablement.

- Pros: most flexible long-term.
- Cons: much larger than the follow-up scope and needs product design for permissions/settings.

Recommended: Option B. It matches the current single-operator product shape and closes the security gaps without building a command-admin product.

## Data Flow

### Agent Sync

```
agent runtime -> enroll.Client.Sync -> Authorization bearer header -> AgentSync handler pre-body auth check -> JSON decode -> req.SyncToken overwritten from header -> syncing.Service.SyncBatch
```

Contracts:

- `Authorization` must have exactly the bearer scheme and a non-empty token that passes existing agent-secret character and length rules.
- Header extraction happens after method/rate-limit checks but before inflight acquisition and body decode.
- Missing or invalid bearer credentials return `401 invalid_sync_token`.
- Malformed bearer format also uses the sync-token auth error instead of generic bad request so callers get one stable auth failure contract.
- `SyncRequest.SyncToken` can remain in Go structs for local agent queue compatibility, but center API treats the header as canonical.
- Before calling `syncBatchFromRequest`, the handler writes the header token into the decoded request. JSON `sync_token` is ignored for credentials and no longer required by `isValidSyncRequest`.

### Command Queue

```
MonitoringInstanceCommandDrawer -> postMonitoringInstanceAction(command_id, confirmed_sensitive?) -> handler validates command metadata and confirmation -> repository queues action + audit queued event -> last_action pending
```

Contracts:

- Add command metadata in `internal/contracts/agentapi/commands.go`:
  - `CommandSensitivityStandard = "standard"`
  - `CommandSensitivitySensitive = "sensitive"`
  - `CommandDefinition{ID, Sensitivity}`
  - helpers such as `CommandSensitivity(commandID)`, `RequiresSensitiveConfirmation(commandID)`, and a deterministic `KnownCommandDefinitions()`.
- Backend request body becomes:

```json
{
  "command_id": "journalctl_u",
  "confirmed_sensitive": true
}
```

- `confirmed_sensitive` is required only when the command sensitivity is `sensitive`.
- `MonitoringInstanceActions` reads user ID from request context when present, but does not fail if tests or no-auth middleware omit it.
- Repository queue method should accept metadata:
  - action ID
  - command ID
  - sensitivity
  - requested user ID or empty
  - requested timestamp

### Agent Dispatch And Completion

```
sync transaction -> storeCommandResults first -> dispatchPendingAction second -> audit completed/dispatch events -> plan.PendingAction
```

Contracts:

- Preserve the existing ordering: matching result storage runs before dispatching the next queued action in the same sync transaction.
- Dispatch event is written only when a pending action is actually cleared and returned in the sync plan.
- Completion event is written only when the guarded `UPDATE monitoring_instances ... last_action->>'status' = pending ... action_id ... command_id` affects one row.
- Audit event insertion happens in the same transaction as the corresponding state transition, so metadata cannot claim an event that did not happen.
- Stale command results remain non-fatal and must not create completion audit rows.

### Visible Result TTL

```
command result -> marshalCompletedLastAction -> last_action done with completed_at/expires_at/output_expired=false -> API scan filters expired output -> retention clears persisted stdout/stderr
```

Contracts:

- Default output TTL: `24h`.
- Completed `last_action` JSON adds:
  - `completed_at`
  - `output_expires_at`
  - `output_expired`
- While unexpired, API returns stdout/stderr as today.
- When expired, API returns the action metadata and exit code, sets `output_expired: true`, and omits stdout/stderr.
- Retention cleanup updates persisted `last_action` JSON for expired done actions by removing stdout/stderr and setting `output_expired=true`.
- TTL is a code constant for this task. A settings UI or env knob is out of scope.

## Database Design

Add migration `0046_create_command_action_audit.sql`.

### Audit Table

Proposed table: `monitoring_instance_command_action_audit`.

Columns:

- `audit_id text primary key`
- `action_id text not null`
- `monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade`
- `command_id text not null`
- `sensitivity text not null`
- `event_type text not null`
- `actor_user_id text null references users(user_id) on delete set null`
- `source text not null`
- `exit_code integer null`
- `occurred_at timestamptz not null default now()`
- `details jsonb not null default '{}'::jsonb`

Constraints:

- `sensitivity in ('standard', 'sensitive')`
- `event_type in ('queued', 'dispatched', 'completed')`
- `source in ('web', 'agent_sync')`

Indexes:

- `idx_monitoring_instance_command_action_audit_instance_time` on `(monitoring_instance_id, occurred_at desc, audit_id desc)`
- `idx_monitoring_instance_command_action_audit_action_time` on `(action_id, occurred_at asc, audit_id asc)`

The audit table stores metadata only. It intentionally does not store stdout or stderr.

### Last Action Shape

Pending:

```json
{
  "action_id": "act_001",
  "command_id": "uptime",
  "status": "pending",
  "sensitivity": "standard",
  "queued_at": "2026-06-26T10:00:00Z"
}
```

Completed:

```json
{
  "action_id": "act_001",
  "command_id": "uptime",
  "status": "done",
  "sensitivity": "standard",
  "stdout": "up 1 day",
  "stderr": "",
  "exit_code": 0,
  "completed_at": "2026-06-26T10:01:00Z",
  "output_expires_at": "2026-06-27T10:01:00Z",
  "output_expired": false
}
```

Expired:

```json
{
  "action_id": "act_001",
  "command_id": "uptime",
  "status": "done",
  "sensitivity": "standard",
  "exit_code": 0,
  "completed_at": "2026-06-26T10:01:00Z",
  "output_expires_at": "2026-06-27T10:01:00Z",
  "output_expired": true
}
```

## Backend Changes

### `internal/contracts/agentapi/commands.go`

- Replace the map-only command list with command definitions.
- Keep `IsKnownCommandID` behavior.
- Add sensitivity helpers.
- Add tests that lock the command IDs and sensitivity table.

### `internal/center/http/handlers/agent.go`

- Replace `isValidOptionalSyncTokenHeader` with required bearer extraction.
- Reject missing/invalid bearer before body read.
- Remove sync-token requirement from body validation.
- Overwrite decoded `req.SyncToken` with header token before service conversion.
- Add body-read sentinel tests for missing and JSON-only token cases.

### `agent/enroll/client.go`

- Keep setting `Authorization: Bearer <token>`.
- For live POST payloads, avoid serializing plaintext sync token by using an API payload copy with `SyncToken` cleared after the header has been prepared.
- Preserve local queue replay semantics by keeping `agentapi.SyncRequest.SyncToken` in memory/local queue types if needed.

### `internal/center/http/handlers/monitoring_instance_actions.go`

- Extend request body with `confirmed_sensitive`.
- Use `agentapi.CommandSensitivity`.
- Reject sensitive commands without confirmation before calling repository writes.
- Pass audit metadata and actor user ID into repository.

### `internal/center/store/monitoring_instances.go`

- Extend `SetPendingAction` or add a new method that accepts queue metadata.
- Write pending `last_action` with sensitivity/queued_at.
- Insert queued audit row in the same operation. If this stays a single-table update plus audit insert, use a transaction to keep both atomic.

### `internal/center/store/sync_batches.go`

- `dispatchPendingAction` should read enough metadata from `last_action` or pending columns to audit the dispatch event.
- `storeCommandResults` should marshal completed `last_action` with TTL timestamps and output-expired false.
- Insert completion audit row only after a matching result update affects one row.
- Preserve stale-result ignore behavior.

### `internal/center/store/retention.go`

- Add a cleanup step that clears expired `last_action.stdout` / `last_action.stderr` and marks `output_expired=true`.
- Add a result counter field, for example `ClearedCommandActionOutputs`.
- Existing retention worker can call it on its normal interval.

### `internal/center/monitoringinstances/types.go`

- Extend `LastAction` with `Sensitivity`, `QueuedAt`, `CompletedAt`, `OutputExpiresAt`, and `OutputExpired`.
- Keep `ExitCode *int` so `exit_code: 0` is preserved.
- `lastActionFromRaw` should mark output expired at scan time when `output_expires_at` is in the past and omit stdout/stderr from API output even before retention has physically cleaned persisted JSON.

Because `lastActionFromRaw` currently has no injected clock, introduce a small helper that accepts `now` for tests and have production call it with `time.Now().UTC()`.

## Frontend Changes

### Types And API

- `web/src/lib/types.ts`: extend `LastAction` with sensitivity and TTL fields.
- `web/src/lib/api.ts`: change `postMonitoringInstanceAction(monitoringInstanceId, commandId, options?)` to include `confirmed_sensitive` when requested.
- `web/src/lib/api.test.ts`: assert standard payload and sensitive confirmed payload.

### Command Metadata

- Extend `MonitoringInstanceCommand` with `sensitivity`.
- Mirror the backend initial tiers in `monitoringDetailConstants.ts`.
- UI labels remain Chinese and compact; no verbose feature explanation in the drawer.

### Drawer And Confirmation

- `MonitoringInstanceCommandDrawer` keeps the current drawer layout.
- Standard command click directly calls `onExecute(command.id, { confirmedSensitive: false })`.
- Sensitive command click opens a nested confirmation `Modal` with `dialogRole="alertdialog"` and a small footer:
  - cancel button closes only confirmation;
  - confirm button calls execute with `confirmedSensitive: true`.
- Disable command buttons while submitting or while there is a pending action as today.
- `MonitoringInstanceCommandResult` shows expired output state when `output_expired` is true.

## Error Handling

- Sync auth failures return `401` with `invalid_sync_token`; they must not leak whether a body token would have been valid.
- Sensitive command missing confirmation returns `400` with a concise error, e.g. `sensitive command confirmation required`.
- Audit insert failures fail the corresponding queue/dispatch/completion transaction because audit is part of the governance contract.
- Retention cleanup failures follow existing retention behavior: log and retry next pass.

## Compatibility And Migration Notes

- Breaking JSON-only sync-token compatibility is intentional for this task.
- Already written local agent queue files may still have `sync_token` from earlier builds; this is local credential storage, not center API compatibility. If live POST clears body tokens, queue flush must still send bearer from the queued request's token field.
- Existing `last_action` JSON without TTL fields remains readable. It should be treated as not expired unless a future completed action writes `output_expires_at`; the new retention step should only touch rows that have `output_expires_at`.
- No audit backfill is required for historical `last_action` records.

## Verification Plan

- Go targeted:
  - `go test ./internal/center/http/handlers -run 'TestAgentSync|TestMonitoringInstanceActions'`
  - `go test ./internal/center/store -run 'Test.*Command|Test.*Retention|TestAgentSyncBatchReplayMigration|TestCommandActionAuditMigration'`
  - `go test ./internal/contracts/agentapi ./agent/...`
- Web targeted:
  - `cd web && npm run test -- --run src/lib/api.test.ts src/pages/MonitoringDetailPage.test.tsx`
- Full gates:
  - `make verify-go`
  - `make verify-web`
  - `git diff --check`

## Follow-Up Items Not Forgotten

- Build a command audit browsing UI after metadata exists and real usage shows the desired filters.
- Consider settings/env support for command output TTL only if 24 hours proves wrong in practice.
- Consider role-based command authorization only when the product has more than one operator role.
