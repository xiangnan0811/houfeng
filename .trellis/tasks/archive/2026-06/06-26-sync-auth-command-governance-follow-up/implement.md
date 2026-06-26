# Sync Auth And Command Governance Implementation Plan

## Preconditions

- Current branch must remain a non-main branch: `fix/sync-auth-command-governance`.
- Hooks must stay enabled with `sh scripts/setup-git-hooks.sh`.
- Do not start implementation until the user approves the PRD/design/implementation artifacts and `python3 ./.trellis/scripts/task.py start ...` moves the task to `in_progress`.
- Codex runs inline for this repo; do not dispatch sub-agents.
- Before editing implementation files, reload project coding guidance with `trellis-before-dev`.

## File Map

- `internal/contracts/agentapi/commands.go`: backend/shared command metadata and sensitivity helpers.
- `internal/contracts/agentapi/commands_test.go`: command ID and sensitivity contract tests.
- `internal/center/http/handlers/agent.go`: strict pre-body bearer token extraction for `/api/agent/sync`.
- `internal/center/http/handlers/agent_test.go`: body-read sentinel tests for missing/invalid/header-only auth.
- `agent/enroll/client.go`: live sync POST header/body credential handling.
- `agent/enroll/client_test.go`: assert sync POST sends bearer header and does not serialize live JSON `sync_token`.
- `internal/center/http/handlers/monitoring_instance_actions.go`: sensitive-command confirmation and actor metadata.
- `internal/center/http/handlers/monitoring_instances_test.go`: handler validation for standard/sensitive command requests.
- `db/migrations/0046_create_command_action_audit.sql`: command audit table and indexes.
- `internal/center/store/migrate/migrate_test.go`: migration content/order test.
- `internal/center/monitoringinstances/types.go`: `LastAction` TTL/sensitivity fields.
- `internal/center/store/command_actions.go`: marshal pending/completed/expired `last_action` helpers.
- `internal/center/store/monitoring_instances.go`: queue action transaction, queued audit insert, expired-output scan filtering.
- `internal/center/store/monitoring_instances_test.go`: queue/audit and expired-output tests.
- `internal/center/store/sync_batches.go`: dispatch/completion audit and result TTL timestamps.
- `internal/center/store/sync_batches_test.go`: dispatch/completion audit tests and stale-result guard preservation.
- `internal/center/retention/types.go`: retention result counter for cleared command outputs.
- `internal/center/store/retention.go`: persisted expired command-output cleanup.
- `internal/center/store/retention_test.go`: cleanup SQL and result counter tests.
- `web/src/lib/types.ts`: frontend `LastAction` fields.
- `web/src/lib/api.ts`: confirmed-sensitive payload support.
- `web/src/lib/api.test.ts`: API payload tests.
- `web/src/pages/monitoring-detail/types.ts`: command sensitivity type.
- `web/src/pages/monitoring-detail/monitoringDetailConstants.ts`: UI command sensitivity metadata.
- `web/src/pages/monitoring-detail/MonitoringInstanceCommandDrawer.tsx`: sensitive command confirmation modal.
- `web/src/pages/monitoring-detail/MonitoringInstanceCommandResult.tsx`: expired-output state.
- `web/src/pages/MonitoringDetailPage.tsx`: execute handler signature and optimistic pending state fields.
- `web/src/pages/MonitoringDetailPage.test.tsx`: UI confirmation and expired-output tests.
- `.trellis/spec/backend/database-guidelines.md`: update command action durability/governance contracts after implementation.
- `.trellis/spec/backend/directory-structure.md`: update agent command boundary with sensitivity/TTL/audit rules after implementation.
- `.trellis/spec/web/state-and-data.md` or `.trellis/spec/web/component-conventions.md`: update frontend command drawer contract if needed.

## Ordered Tasks

### Task 1: Lock Command Metadata Contracts

- Add failing tests in `internal/contracts/agentapi/commands_test.go`:
  - known commands include sensitivity;
  - standard commands are exactly `df_h`, `free_m`, `uptime`;
  - sensitive commands are exactly `top_head`, `journalctl_u`, `systemctl_status`, `dmesg_err`, `docker_ps`;
  - unknown command IDs return unknown sensitivity and remain rejected.
- Implement command definitions and helpers in `commands.go`.
- Run:

```bash
go test ./internal/contracts/agentapi
```

### Task 2: Make Agent Sync Header-Only At Center Boundary

- Add failing tests in `internal/center/http/handlers/agent_test.go`:
  - missing `Authorization` with `errReader{}` returns `401 invalid_sync_token`;
  - JSON-only sync token with `errReader{}` is rejected before body read;
  - malformed/empty/oversized bearer tokens reject before body read;
  - valid bearer token with a body that omits `sync_token` reaches the service with that token.
- Replace optional sync-token header validation with required bearer extraction.
- Relax body validation so `SyncRequest.SyncToken` is not required from JSON.
- Assign header token into `req.SyncToken` before `syncBatchFromRequest`.
- Update existing sync handler tests that still expect body-only auth by adding the bearer header or changing expected failures.
- Run:

```bash
go test ./internal/center/http/handlers -run TestAgentSync
```

### Task 3: Remove Live Sync Token From New Agent JSON Payloads

- Add or adjust `agent/enroll/client_test.go` to inspect the live sync request body and assert it does not contain `sync_token` while the header remains `Bearer sync-token-001`.
- Update `Client.Sync` to prepare the header from `request.SyncToken`, then clear `SyncToken` in the payload copy passed to `postJSON`.
- Keep local sync queue structs unchanged unless tests prove replay needs a small adapter.
- Run:

```bash
go test ./agent/enroll ./agent/runtime ./agent/syncqueue
```

### Task 4: Add Command Audit Migration

- Create `db/migrations/0046_create_command_action_audit.sql` with the table, constraints, and indexes from `design.md`.
- Add migration test coverage in `internal/center/store/migrate/migrate_test.go` that reads `0046_create_command_action_audit.sql` and checks the core table/constraint/index fragments.
- Run:

```bash
go test ./internal/center/store/migrate
```

### Task 5: Enforce Sensitive Confirmation In The Action Handler

- Extend handler fake repository in `monitoring_instances_test.go` for new queue metadata.
- Add failing handler tests:
  - sensitive command without `confirmed_sensitive` returns `400` and does not queue;
  - sensitive command with `confirmed_sensitive:true` queues;
  - standard command without confirmation queues;
  - unknown fields/trailing JSON behavior remains unchanged.
- Update `monitoringInstanceActionRepository` interface and handler implementation.
- Thread `UserIDFromContext` into queue metadata when present.
- Run:

```bash
go test ./internal/center/http/handlers -run TestMonitoringInstanceActions
```

### Task 6: Queue Pending Action With Audit Metadata

- Introduce a queue input struct in the domain/store boundary rather than adding many positional string parameters.
- Update `SetPendingAction` or replace it with `QueueCommandAction`.
- Make queueing a transaction:
  - update `pending_action_id`, `pending_action_command_id`, and pending `last_action`;
  - insert a `queued` audit row with `source='web'`.
- Add store tests for:
  - pending JSON includes `sensitivity` and `queued_at`;
  - audit insert happens in the same queue operation;
  - archived/not-found behavior remains mapped to existing domain errors.
- Run:

```bash
go test ./internal/center/store -run 'Test.*PendingAction|Test.*CommandAction'
```

### Task 7: Add Result TTL Fields And Expired Scan Filtering

- Extend `monitoringinstances.LastAction` and frontend-independent JSON scan tests.
- Update marshal helpers to include `completed_at`, `output_expires_at`, and `output_expired:false`.
- Add helper tests for:
  - unexpired completed output keeps stdout/stderr;
  - expired completed output omits stdout/stderr and marks `output_expired=true`;
  - `exit_code:0` remains serialized and scanned.
- Keep legacy `last_action` without expiry readable.
- Run:

```bash
go test ./internal/center/store -run 'Test.*LastAction|TestScanMonitoringInstance'
```

### Task 8: Audit Dispatch And Completion In Sync Transaction

- Update fake sync batch transaction to report rows affected and capture audit inserts.
- Add failing tests:
  - dispatch writes a `dispatched` audit event after pending action is cleared;
  - matching completion writes a `completed` audit event with exit code and no stdout/stderr;
  - stale result with `UPDATE 0` does not insert completion audit;
  - result storage still runs before dispatch.
- Implement dispatch/completion audit inserts in `sync_batches.go`.
- Run:

```bash
go test ./internal/center/store -run 'TestSyncBatch.*Command|TestSyncBatchStoresCommandResultBeforeDispatchingQueuedAction'
```

### Task 9: Add Retention Cleanup For Expired Outputs

- Extend `retention.Result` with `ClearedCommandActionOutputs`.
- Add store retention tests that assert a SQL update clears `stdout`/`stderr` from expired completed `last_action` JSON and marks `output_expired`.
- Add worker log field for the new result counter.
- Run:

```bash
go test ./internal/center/retention ./internal/center/store -run 'TestWorker|Test.*Retention'
```

### Task 10: Implement Frontend API And UI Confirmation

- Update frontend types and API helper.
- Add API tests:
  - standard request body remains `{"command_id":"uptime"}`;
  - sensitive confirmed request body includes `confirmed_sensitive:true`.
- Update command constants with sensitivity metadata.
- Update drawer:
  - show a compact sensitive marker on sensitive command rows;
  - open an `alertdialog` confirmation modal on sensitive commands;
  - call execute only after confirmation.
- Update result component for `output_expired`.
- Update page execute handler signature and optimistic pending state.
- Add page tests:
  - sensitive command click opens confirmation and does not POST immediately;
  - confirm posts with `confirmed_sensitive:true`;
  - cancel does not POST;
  - standard command still posts immediately;
  - expired output state renders without stdout/stderr.
- Run:

```bash
cd web && npm run test -- --run src/lib/api.test.ts src/pages/MonitoringDetailPage.test.tsx
```

### Task 11: Full Verification And Spec Update

- Run format and targeted tests after the last code edit:

```bash
make verify-go
make verify-web
git diff --check
```

- Update relevant Trellis specs with the final command-governance contracts.
- Re-run the smallest tests touched by spec-adjacent changes if any code changed during spec update.
- Commit only after checks pass and `git status --short --branch` is understood.

## Rollback Points

- If strict sync auth creates unexpected agent-side queue failures, keep center strict and adapt queue flush to send bearer from queued request credentials; do not restore JSON-only center fallback.
- If audit transaction wiring becomes too broad, split repository methods by event type while keeping event insert in the same transaction as state transition.
- If frontend modal nesting causes focus issues, keep the existing drawer but render the confirmation as a sibling `Modal` portal controlled by the drawer state.
- If retention cleanup SQL becomes hard to express safely, keep scan-time expiry filtering as the must-have API guarantee and add persisted cleanup in a smaller follow-up only with explicit user approval.

## Review Gate Before Start

Before running `task.py start`, confirm:

- TTL remains 24 hours.
- Sensitive confirmation is a boolean `confirmed_sensitive`.
- Audit UI remains out of scope.
