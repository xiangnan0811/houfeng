# Research: backend / agent audit

- Query: Research backend and agent implementation risks for the active Trellis task, focused on Go center, agent, contracts, store, migrations, tests, V1 baseline, CLAUDE, README, runtime/data-flow risks, and verification commands.
- Scope: internal
- Date: 2026-05-06

## Findings

### Files Found

| Path | Description |
| --- | --- |
| `README.md` | Public project positioning and verification commands; states the repository is a V1 implementation and warns not to redesign V1 behavior (`README.md:19-30`, `README.md:93-101`). |
| `CLAUDE.md` | Agent-facing baseline; freezes V1 identity/runtime expectations and says the agent has only enroll/sync center routes and does not run arbitrary scripts or Docker locally (`CLAUDE.md:7-13`, `CLAUDE.md:54-60`, `CLAUDE.md:96-107`). |
| `docs/design/v1-baseline/README.md` | V1 frozen baseline index; notification scope is minimal and Telegram-only (`docs/design/v1-baseline/README.md:174-181`). |
| `docs/design/v1-baseline/rules-and-interaction.md` | Notification rules; start/escalate/recover only, suppresses routine observations and maintenance/backfill noise (`docs/design/v1-baseline/rules-and-interaction.md:127-153`). |
| `docs/design/v1-baseline/tech-selection.md` | V1 technical baseline; one Go center, one Postgres, N systemd agents, no MQ/TSDB/microservices, agent local buffer, not dependent on Docker (`docs/design/v1-baseline/tech-selection.md:52-64`). |
| `.trellis/spec/backend/*.md` | Backend conventions for package placement, DTO lockstep, migrations, error handling, and verification. |
| `internal/contracts/agentapi/types.go` | Center-agent wire contract; now includes container samples, pending actions, and command results (`internal/contracts/agentapi/types.go:75-110`, `internal/contracts/agentapi/types.go:138-182`). |
| `agent/runtime/runtime.go` | Agent sync loop, queue/backfill behavior, host sample collection, and pending action execution (`agent/runtime/runtime.go:229-238`, `agent/runtime/runtime.go:289-305`, `agent/runtime/runtime.go:338-343`). |
| `agent/exec/whitelist.go` | Remote command whitelist; includes host, systemd, journal, dmesg, and Docker commands (`agent/exec/whitelist.go:17-26`). |
| `agent/exec/runner.go` | Command runner uses no shell, fixed timeout, and bounded stdout/stderr (`agent/exec/runner.go:21-79`). |
| `agent/containersample/sample.go` | Container telemetry collector using the local Docker CLI when present (`agent/containersample/sample.go:16-58`, `agent/containersample/sample.go:61-74`). |
| `internal/center/http/router.go` | Center HTTP router; contains the node action route classifier but does not dispatch it in the main node switch (`internal/center/http/router.go:101-162`, `internal/center/http/router.go:226-240`, `internal/center/http/router.go:271-273`). |
| `internal/center/http/handlers/node_actions.go` | Handler for `POST /api/nodes/{id}/actions`; queues a pending action for bound, unpaused nodes (`internal/center/http/handlers/node_actions.go:19-74`). |
| `internal/center/http/handlers/agent.go` | Agent sync handler; validates core heartbeat/sample fields and maps pending action/result DTOs (`internal/center/http/handlers/agent.go:114-139`, `internal/center/http/handlers/agent.go:228-273`). |
| `internal/center/syncing/service.go` | Sync service batch DTO includes command results and returns pending action in the sync plan (`internal/center/syncing/service.go:21-35`, `internal/center/syncing/service.go:63-73`). |
| `internal/center/store/sync_batches.go` | Atomic sync batch store; dispatches pending actions and stores command results (`internal/center/store/sync_batches.go:81-96`, `internal/center/store/sync_batches.go:272-330`). |
| `internal/center/store/nodes.go` | Node repository pending-action and last-action storage helpers (`internal/center/store/nodes.go:1336-1388`). |
| `internal/center/store/observations.go` | Host sample writes, including containers JSONB (`internal/center/store/observations.go:53-119`, `internal/center/store/observations.go:174-186`). |
| `internal/center/store/runtime_facts.go` | Runtime facts reads, including container JSONB scan path (`internal/center/store/runtime_facts.go:23-88`, `internal/center/store/runtime_facts.go:274-309`). |
| `internal/center/settings/types.go` | Settings DTO now includes Feishu alongside Telegram (`internal/center/settings/types.go:21-29`, `internal/center/settings/types.go:157-180`). |
| `internal/center/incidents/service.go` | Incident notification service sends Telegram and, when enabled, Feishu, but records one decision channel (`internal/center/incidents/service.go:84-153`, `internal/center/incidents/service.go:579-615`). |
| `db/migrations/0012_add_node_pending_action.sql` | Adds pending action and last action columns. |
| `db/migrations/0014_add_feishu_settings.sql` | Adds Feishu settings columns. |
| `db/migrations/0015_add_host_containers.sql` | Adds host sample container JSONB column. |
| `cmd/houfeng-center/bootstrap.go` | Wires center dependencies, including `NodeActionsHandler` (`cmd/houfeng-center/bootstrap.go:117-149`). |
| `cmd/houfeng-center/bootstrap_test.go` | Bootstrap nil-handler regression test; currently omits `NodeActionsHandler` from assertions (`cmd/houfeng-center/bootstrap_test.go:177-245`). |
| `internal/center/store/sync_batches_test.go` | Sync batch store tests; cover heartbeat/sample/backfill behaviors but not pending action dispatch or command result persistence. |
| `internal/center/http/handlers/nodes_test.go` | Node handler fake repository implements pending-action methods, but node action behavior is not exercised. |
| `web/src/lib/api.ts` | Frontend posts node actions to `/api/nodes/{nodeId}/actions` (`web/src/lib/api.ts:364-367`). |
| `web/src/lib/types.ts` | Frontend `LastAction` type requires `command_id` (`web/src/lib/types.ts:23-30`). |
| `web/src/pages/NodeDetailPage.tsx` | Frontend command palette and last-action display depend on action command IDs (`web/src/pages/NodeDetailPage.tsx:166-175`, `web/src/pages/NodeDetailPage.tsx:1237-1257`). |

### Code Patterns

- V1 baseline is still the governing product contract. README says this repository is an implementation of V1 rather than a place to redesign it (`README.md:19-30`), while CLAUDE says V1 business structure is frozen and future enhancements must be handled as V1-compatible extensions or explicitly outside V1 (`CLAUDE.md:7-13`).
- Center-agent wire types live in `internal/contracts/agentapi`, and the backend spec calls this a shared DTO package for endpoints with multiple internal consumers (`.trellis/spec/backend/directory-structure.md:176-183`). Any agent request/response change needs center, agent, store, and tests updated together.
- The backend quality spec requires endpoint additions to update handler/router/bootstrap wiring and tests together (`.trellis/spec/backend/quality-guidelines.md:173-178`). The node action feature violates that pattern: handler and bootstrap exist, but router dispatch and tests are missing.
- Migrations are authoritative for schema changes; the database spec says SQL belongs in repositories/migrations and not handlers (`.trellis/spec/backend/database-guidelines.md:129-137`). Pending action, Feishu settings, and host containers all follow the migration route, but their product/test coverage does not fully follow the quality gate.
- Runtime incident and observation paths should preserve raw facts and avoid converting request-path anomalies into user-visible operational truth without care (`.trellis/spec/backend/error-handling.md:126-133`). The current action/notification issues are mostly truth/data-flow risks, not SQL-layer placement problems.
- Agent runtime mostly preserves the intended buffered sync architecture: telemetry sync requests are enqueued, the center is contacted through enroll/sync, and samples can be marked with backfill state. The newer pending-action result path is less durable than the telemetry path.

### Risk 1: Node action endpoint is implemented but unreachable

Severity: Blocker for the node action feature.

Evidence:
- Frontend calls `POST /api/nodes/${nodeId}/actions` from `executeNodeAction` (`web/src/lib/api.ts:364-367`).
- Center bootstrap wires `NodeActionsHandler` (`cmd/houfeng-center/bootstrap.go:135`), and router options include that field (`internal/center/http/router.go:29`).
- The route classifier recognizes `/api/nodes/{id}/actions` as `nodeSubtreeActions` (`internal/center/http/router.go:226-240`, `internal/center/http/router.go:271-273`).
- The main `/api/nodes/` switch has cases for node details, pause/resume, timeline, samples, observations, runtime facts, dependency graph, settings, maintenance windows, incidents, acknowledgements, history, and notification history, but it has no `nodeSubtreeActions` case (`internal/center/http/router.go:101-162`).
- Result: the recognized route falls through to the default `http.NotFound`, so the UI action path returns 404 before reaching `handlers.NodeActions`.

Impact:
- The action drawer/button cannot queue an action even though the UI, handler, store, migration, and agent contract are present.
- Existing tests allow this to pass: `cmd/houfeng-center/bootstrap_test.go:177-245` checks many handler fields but omits `NodeActionsHandler`; router/handler tests do not exercise `POST /api/nodes/{id}/actions`.

Recommended repair:
- Add `nodeSubtreeActions` dispatch in `internal/center/http/router.go`.
- Add router coverage for authenticated `POST /api/nodes/{id}/actions`.
- Add handler tests for successful queue, invalid command body, unknown node, unbound node, paused monitoring, and repository failure.
- Add `NodeActionsHandler` to the bootstrap nil-handler assertion.

### Risk 2: Completed command results lose the command ID

Severity: High data-flow bug.

Evidence:
- Backend node state models last action with `CommandID` (`internal/center/nodes/types.go:43-53`).
- Frontend type requires `last_action.command_id` (`web/src/lib/types.ts:23-30`) and the Node detail page displays it as the completed command label (`web/src/pages/NodeDetailPage.tsx:1237-1257`).
- The agent result DTO contains `action_id`, `exit_code`, `stdout`, `stderr`, `started_at`, `completed_at`, and `error`, but not `command_id` (`internal/contracts/agentapi/types.go:177-182`).
- The center sync batch result DTO also lacks `command_id` (`internal/center/syncing/service.go:21-27`).
- `storeCommandResults` writes the last-action JSON with `"command_id": ""` for every result (`internal/center/store/sync_batches.go:310-317`).

Impact:
- The center permanently loses which whitelisted command was executed once the result is stored.
- UI history can show a completed result with a blank or unknown command label.
- Operator audit evidence is weaker than the UI implies; action intent, result output, and action ID are not tied together in persisted state.

Recommended repair:
- Preserve command ID through the result path. Options include adding `command_id` to `CommandResult`, looking up a dispatched action record by `action_id`, or storing a dispatched-action map until result acknowledgement.
- Add store tests proving last-action JSON includes the original command ID.
- Add frontend/API contract coverage or fixture coverage for completed action rendering.

### Risk 3: Pending action delivery is one-shot and result reporting can be lost

Severity: High runtime/data-flow risk.

Evidence:
- Center dispatches a pending action and clears `nodes.pending_action_id` / `pending_action_command_id` inside the same sync transaction before the agent executes the command or returns a result (`internal/center/store/sync_batches.go:272-299`).
- Agent executes the pending action after receiving a sync plan (`agent/runtime/runtime.go:289-305`).
- Agent appends any pending command results into the next sync request and then immediately clears `r.pendingResults = nil` while building the request (`agent/runtime/runtime.go:229-238`), before the request is proven accepted by the center.
- The default runtime path uses an on-disk queue for sync requests, so a successfully enqueued request retains the result. The result can still be lost if request construction is followed by enqueue failure, queue write failure, or a non-queued client path error after `pendingResults` has been cleared.
- Current runtime tests cover successful result send and unknown-command ignore behavior, but not result retention after sync/queue failure.

Impact:
- A remote action can be cleared from the center before execution evidence returns.
- If the command executes but result reporting fails before durable queue persistence, the center has neither a pending action to redispatch nor a last-action result to display.
- This is sharper than ordinary telemetry loss because remote operator actions are expected to have an auditable result.

Recommended repair:
- Keep center action state until a matching result is acknowledged, or add an explicit dispatched/in-flight state instead of clearing immediately.
- In the agent, clear `pendingResults` only after the result is durably queued or accepted by the center.
- Add tests for action dispatch followed by agent sync failure, queue enqueue failure, duplicate sync plans, and result retry after restart.

### Risk 4: Remote command and Docker/container features drift from the V1 thin-agent baseline

Severity: Product-scope / contract risk.

Evidence:
- CLAUDE states the runtime is exactly one Go center plus Postgres plus N systemd agents, with agents using only `/api/agent/enroll` and `/api/agent/sync` (`CLAUDE.md:54-60`).
- CLAUDE also says agents collect metrics, run protocol probes, and sync state, but "do not run arbitrary scripts, run Docker, or evaluate rules locally" (`CLAUDE.md:96-107`).
- V1 tech selection says the agent is a systemd service, keeps a local buffer, and is not dependent on Docker (`docs/design/v1-baseline/tech-selection.md:52-64`).
- Current code executes whitelisted host commands on the agent (`agent/exec/whitelist.go:17-26`, `agent/runtime/runtime.go:289-305`).
- Current code collects container facts through the local Docker CLI when present (`agent/containersample/sample.go:16-58`) and exposes them in the center-agent contract (`internal/contracts/agentapi/types.go:75-110`).

Impact:
- If these are intended Stage 2 features, README/CLAUDE/V1 docs currently do not make that boundary visible.
- If the active repair task is meant to stay inside V1, these surfaces exceed the frozen baseline and can mislead future agents into treating remote command/container behavior as V1 scope.
- Docker CLI use is opportunistic rather than a hard dependency, which lowers runtime risk, but it still contradicts the plain-language "do not run Docker" instruction.

Recommended repair:
- Decide whether pending actions and container samples are post-V1 extensions or should be disabled/hidden for the V1 audit baseline.
- If retained, update authoritative project guidance in a separate docs/spec task so CLAUDE/README/V1 baseline no longer misstate implementation reality.
- Add tests around Docker-unavailable behavior, command whitelist stability, and center-agent DTO compatibility.

### Risk 5: Feishu notification support conflicts with Telegram-only baseline and records misleading channels

Severity: High truth/data-flow risk when Feishu is enabled.

Evidence:
- V1 baseline says notifications are minimal and Telegram-only (`docs/design/v1-baseline/README.md:174-181`).
- V1 rules say notifications are limited to start/escalate/recover events and suppress routine/backfill/maintenance observations (`docs/design/v1-baseline/rules-and-interaction.md:127-153`).
- Settings now include Feishu enablement and webhook URL (`internal/center/settings/types.go:21-29`, `internal/center/settings/types.go:157-180`), backed by migration `db/migrations/0014_add_feishu_settings.sql`.
- `SettingsAwareNotifier.Send` sends Telegram and then Feishu when enabled (`internal/center/incidents/service.go:84-153`).
- Incident notification records are appended from `decision.Channel`, a single channel chosen by the evaluator, and the record status is based on the aggregate `s.notifier.Send` result (`internal/center/incidents/service.go:579-615`).
- Evaluator decisions currently use `"telegram"` as the notification channel label in notification decisions (`internal/center/incidents/evaluator.go:330` and related decision construction sites).

Impact:
- A Feishu delivery can be recorded as a Telegram notification because record creation is not per-channel.
- If Telegram is disabled and Feishu succeeds, notification history can still claim a Telegram-channel send.
- This creates public/runtime truth drift: user-facing notification history and audit records no longer match actual transport.

Recommended repair:
- Either keep Feishu hidden/disabled outside V1 until the channel model is expanded, or model notification delivery per channel.
- Add incident service tests for Telegram-only, Feishu-only, both enabled, partial failure, and record channel/status accuracy.
- Add settings handler/store tests that assert Feishu response and update round trips, not only SQL shape.

### Risk 6: Current tests miss backend-agent integration regressions

Severity: Medium to high, because the Go suite can pass with user-visible action failures.

Evidence:
- No test currently exercises `POST /api/nodes/{id}/actions`; repository fakes implement action methods, but the endpoint path is not covered.
- `cmd/houfeng-center/bootstrap_test.go:177-245` omits `NodeActionsHandler` from its nil-handler checks.
- `internal/center/store/sync_batches_test.go` covers heartbeat, host sample, probe sample, backfill flags, plan behavior, and rollback cases, but its pending-action query path simulates no pending action and never asserts dispatch/result persistence.
- Runtime action tests cover happy-path result collection and unknown command handling, but not queue write failure or retry semantics after a result is produced.
- The README recommends `go test ./...` (`README.md:97-101`), but in this working tree that command also discovers a Go package under `web/node_modules/flatted/golang/pkg/flatted`. The Makefile uses narrower Go package patterns, which avoids this noisy package discovery.

Impact:
- The repository can report passing Go tests while the node action API returns 404.
- Backend-agent command execution can appear implemented while the persisted result is incomplete.
- Broad `go test ./...` can be environment-sensitive because it includes vendored/transitive frontend artifacts under `web/node_modules`.

Recommended repair:
- Add focused HTTP router/handler tests for node actions.
- Add store tests for pending action dispatch, center clearing semantics, action result persistence, and rollback.
- Add agent runtime tests that prove command results survive transient sync/queue failures.
- Consider aligning README verification guidance with `make verify-go` or the same package patterns used by the Makefile.

### Risk 7: Container JSON read path silently ignores malformed data

Severity: Low runtime observability risk.

Evidence:
- Host samples store container JSONB through `marshalContainers`, returning SQL null for empty values and marshaled JSON for non-empty values (`internal/center/store/observations.go:174-186`).
- Runtime facts scan container JSON with `_ = json.Unmarshal(containersJSON, &containers)` and proceeds with the zero value on failure (`internal/center/store/runtime_facts.go:306-308`).

Impact:
- Corrupt or manually edited container JSONB can silently render as "no containers" rather than surfacing a store/data issue.
- Because most writes originate from code-controlled marshaling, this is lower risk than the action and notification findings.

Recommended repair:
- Return a wrapped scan error or log a structured warning when JSONB cannot be decoded.
- Add a store scan test for malformed container JSON if the reader is expected to reject data drift.

### Positive Notes

- The agent command runner uses `exec.CommandContext` without a shell, bounded output, and a default timeout, reducing injection and runaway-output risk (`agent/exec/runner.go:21-79`).
- Docker/container sampling is opportunistic: if the Docker CLI is unavailable, the collector returns no containers rather than failing telemetry sync (`agent/containersample/sample.go:26-37`).
- Recent migrations use additive columns and `IF NOT EXISTS` patterns for the audited feature additions.
- Host heartbeat, host sample, probe sample, dependency graph, and backfill storage have better coverage than the newer pending-action path.

## Related Specs

- `.trellis/spec/backend/index.md` points backend work to the relevant implementation, database, testing, and quality documents.
- `.trellis/spec/backend/directory-structure.md:176-183` says shared endpoint DTOs belong in `internal/contracts`.
- `.trellis/spec/backend/directory-structure.md:198-206` says new backend code should follow the existing package split and keep generated/templates in sync when applicable.
- `.trellis/spec/backend/database-guidelines.md:129-137` requires SQL to stay in repository/migration layers and forbids handler-level SQL.
- `.trellis/spec/backend/error-handling.md:126-133` says request-path issues should remain operational facts and should not turn into misleading user-visible incident truth.
- `.trellis/spec/backend/quality-guidelines.md:158-163` lists the backend verification checklist (`go test ./internal/center/...`, `make verify-go`, targeted store/API tests).
- `.trellis/spec/backend/quality-guidelines.md:173-178` requires cross-layer endpoint updates: handler, router, auth, bootstrap, integration tests, generated frontend API, fixtures, and store tests.

## External References

- None. This audit used repository sources only.

## Verification Commands

Commands run:

```sh
python3 ./.trellis/scripts/task.py current --source
```

Result: no active task was configured in the script output. The user then explicitly confirmed `.trellis/tasks/05-06-comprehensive-audit-repair` as the task directory.

```sh
go test ./...
```

Result: failed before package execution because Go could not create a build work directory under the default macOS temp path:

```text
go: creating work dir: mkdir /var/folders/zz/.../T/go-build...: permission denied
```

```sh
mkdir -p /tmp/houfeng-go-build /tmp/houfeng-gocache
TMPDIR=/tmp GOTMPDIR=/tmp/houfeng-go-build GOCACHE=/tmp/houfeng-gocache go test ./...
```

Result: passed. Note that broad `go test ./...` also discovered `houfeng/web/node_modules/flatted/golang/pkg/flatted`, which is likely not intended as part of the backend verification surface.

Recommended follow-up verification after repairs:

```sh
go test ./internal/center/http -run 'TestRouter.*NodeActions|TestAuthEndToEnd' -v
go test ./internal/center/http/handlers -run TestNodeActions -v
go test ./internal/center/store -run 'TestPostgresSyncRepository.*PendingAction|TestPostgresSyncRepository.*CommandResult' -v
go test ./agent/runtime -run 'TestRuntime.*PendingAction|TestRuntime.*CommandResult' -v
go test ./internal/center/incidents -run 'Test.*Feishu|TestService.*Notification' -v
make verify-go
./scripts/verify.sh
```

## Caveats / Not Found

- `python3 ./.trellis/scripts/task.py current --source` reported no active task. This file uses the user-confirmed task directory instead of guessing.
- I did not edit code, specs, README, CLAUDE, migrations, or existing docs. The only intended write for this audit is this research file.
- I did not run live Postgres migrations, live Telegram/Feishu delivery, a center+agent end-to-end process, or web build commands.
- I did not find tests that exercise the node action endpoint or command-result persistence end to end.
- Feishu and remote command/container behavior may be intentional Stage 2 work, but current V1/CLAUDE/README sources do not clearly mark them that way.
