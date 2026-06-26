# Sync Auth And Command Governance Follow-Up

## Goal

Close the two security-review follow-ups that were intentionally left out of the `v0.54.5` remediation branch:

- make `/api/agent/sync` reject missing or invalid sync credentials before reading the request body;
- add command-action governance around sensitivity tiers, explicit sensitive-command confirmation, metadata audit, and command-result output TTL.

## Background And Confirmed Facts

- The previous remediation task archived at `.trellis/tasks/archive/2026-06/06-26-security-review-remediation` fixed output redaction and release signing, but recorded two remaining caveats:
  - P1-02: missing `Authorization` on `/api/agent/sync` remained accepted for legacy JSON `sync_token` compatibility.
  - P2-02: command sensitivity tiers, UI second confirmation, audit, and TTL were scoped out.
- The user confirmed there are no deployed legacy users or agents to preserve. The current real environment is self-owned test deployment only.
- Current agent client code already sends `Authorization: Bearer <sync_token>` from `agent/enroll/client.go`.
- Current center sync handler validates an optional `Authorization` or `X-Houfeng-Agent-Token` header before body read, but accepts missing headers and later requires JSON `sync_token`.
- Current command action request is `POST /api/monitoring-instances/{id}/actions` with body `{"command_id":"..."}`.
- Current command whitelist IDs are fixed: `df_h`, `free_m`, `uptime`, `top_head`, `journalctl_u`, `systemctl_status`, `dmesg_err`, `docker_ps`.
- Current command visible state is `monitoring_instances.last_action` JSONB. It is a current UI state, not an audit log.
- Command stdout/stderr are already redacted in agent upload and center persistence, but completed output currently remains visible indefinitely.
- Regular UI/API routes have authenticated session context available via `UserIDFromContext`; agent sync routes intentionally bypass browser-session middleware and must rely on sync-token authentication.

## Requirements

### Strict Agent Sync Authentication

- `/api/agent/sync` must require `Authorization: Bearer <sync_token>` before reading or limiting the request body.
- Missing, malformed, empty, or oversized bearer credentials must be rejected before body read and before calling the sync service.
- JSON `sync_token` must no longer be an accepted credential fallback.
- The canonical sync token used by center validation must come from the bearer header.
- The agent client should continue sending the bearer token and should stop placing plaintext sync tokens in new sync JSON bodies when doing so does not break local queue replay semantics.
- Any remaining local durable queue compatibility for already-written entries must be explicit and limited to local agent files, not center API compatibility.

### Command Sensitivity Tiers

- Command metadata must define a sensitivity tier for every known command ID.
- The backend must be the enforcement authority for sensitivity and confirmation; frontend metadata is only presentation.
- Unknown command IDs must still be rejected before repository writes.
- Proposed initial tiers:
  - `standard`: `df_h`, `free_m`, `uptime`
  - `sensitive`: `top_head`, `journalctl_u`, `systemctl_status`, `dmesg_err`, `docker_ps`
- Sensitive commands require explicit confirmation in the HTTP request before queueing.
- Standard commands continue to work without a second confirmation.

### UI Second Confirmation

- MonitoringInstance command drawer must visually distinguish sensitive commands.
- Clicking a sensitive command opens a second confirmation dialog before the POST request.
- The confirmation dialog must show the command label and explain that command output may include operational details.
- The frontend request for sensitive commands must include the explicit confirmation field required by the backend.
- Pending-action disabling behavior must remain unchanged: no new command may be queued while `last_action.status === "pending"`.

### Command Audit

- Queueing, dispatching, and completion of command actions must be recorded as audit metadata.
- Audit records must include action ID, monitoring instance ID, command ID, sensitivity tier, event type, timestamp, and available actor/source metadata.
- Queue events from the browser route should record the authenticated user ID when present.
- Dispatch and completion events from agent sync should record source as agent/system rather than browser user.
- Audit must not persist stdout or stderr; command output remains only in current `last_action` until TTL expiry.
- Audit writes must not weaken the existing command-result identity guard on `action_id` + `command_id`.

### Command Result TTL

- Completed command stdout/stderr must stop being returned by MonitoringInstance read APIs after a configured TTL.
- The default TTL for visible command output is 24 hours.
- Expiry should preserve enough UI metadata to explain what happened: action ID, command ID, status, exit code, completion time, and output-expired marker.
- Expired outputs should be cleared from persisted `last_action` by the existing retention worker path or equivalent periodic cleanup.
- Audit metadata should remain available after output TTL expiry, but it must not contain stdout/stderr.

## Acceptance Criteria

- [ ] Missing `Authorization` on `/api/agent/sync` returns an agent error before reading a body that would otherwise fail if read.
- [ ] Malformed, empty, whitespace-containing, or oversized bearer credentials are rejected before body read and before sync service calls.
- [ ] Valid bearer credentials with a sync body that omits JSON `sync_token` are accepted and pass the header token to the sync service.
- [ ] JSON-only `sync_token` requests are rejected before body read/service invocation.
- [ ] Agent sync client sends bearer auth and no longer serializes plaintext sync token in new sync requests when posting live sync batches.
- [ ] Every known command has exactly one sensitivity tier and tests fail if the known command list and sensitivity metadata drift.
- [ ] Sensitive command POST without explicit confirmation returns 400 and does not call repository write methods.
- [ ] Sensitive command POST with explicit confirmation queues the action and stores pending visible state.
- [ ] Standard command POST still queues without confirmation.
- [ ] Command audit records are inserted for queue, dispatch, and matching completion events; audit rows do not include stdout/stderr.
- [ ] Completed `last_action` includes completion and expiry timestamps; unexpired outputs are returned normally.
- [ ] Expired completed outputs are not returned by MonitoringInstance APIs and the UI shows an expired-output state instead of stale stdout/stderr.
- [ ] Retention cleanup clears expired stdout/stderr from persisted `last_action`.
- [ ] Frontend command drawer shows sensitive-command second confirmation and posts the backend confirmation field only after user confirmation.
- [ ] Existing command result durability behavior remains intact: stale or mismatched results cannot overwrite a newer pending action.
- [ ] `make verify-go`, targeted web tests, `make verify-web`, and `git diff --check` pass before commit.

## Out Of Scope

- Multi-user RBAC or per-role command authorization. The current system has authenticated sessions, but no role model in this task.
- Arbitrary command arguments, shell snippets, SSH, Docker orchestration, or any expansion beyond the compiled-in agent whitelist.
- A command audit list UI. This task records durable audit metadata and may leave audit browsing to a later task.
- Backward compatibility with deployed agents that only send JSON `sync_token`; the user explicitly confirmed this can be broken now.
- Changing release signing, Docker Hub publishing, or Minisign configuration. Those were completed in the previous task and are only background here.

## Decision To Confirm Before Implementation

Recommended default: use a 24-hour TTL for command stdout/stderr visibility, keep command audit metadata without output indefinitely for now, and require a boolean `confirmed_sensitive: true` field for sensitive command POST requests.

If you want a different TTL or a typed confirmation phrase instead of a boolean, decide before `task.py start` because it affects API, DB, and UI tests.

Confirmed by user on 2026-06-26: proceed with the recommended defaults.
