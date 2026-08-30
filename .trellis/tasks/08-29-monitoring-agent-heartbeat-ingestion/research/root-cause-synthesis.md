# Root-cause synthesis: agent active but no accepted heartbeat

- Date: 2026-08-29
- Baseline: `bbf4f043` on `fix/monitoring-agent-heartbeat-ingestion`
- Evidence scope: repository source, tests, specs, git/release metadata; no report-specific host journal, proxy log, queue file or database row

## Outcome

The strongest deterministic defect is in the agent's durable queue retry policy, not in the Monitoring UI or the center happy path. A queue entry stores the identity, token and fingerprint that existed when it was created. Runtime sends entries oldest-first, stops on the first error, treats every remote rejection as retryable and continues its outer loop. A permanently rejected old entry can therefore keep the process `active (running)` while preventing every current heartbeat from reaching center acceptance.

This is also an implementation drift from `.trellis/spec/backend/error-handling.md`: permanent auth/binding failures must not retry forever; invalid JSON/request entries must be recorded then discarded; only recoverable server failures should remain queued.

## Evidence chain

1. `agent/runtime/runtime.go` enqueues every fresh request before flush and lists the durable queue oldest-first.
2. `agent/syncqueue/store.go` persists the complete `SyncRequest`, including MonitoringInstance ID and sync token, and retains it for up to the configured 72-hour default.
3. On the first send error, runtime increments attempts and immediately returns. Newer/current heartbeats are never attempted.
4. `*enroll.RemoteError` already exposes HTTP status and stable agent API code, but runtime wraps every error as the same `errRemoteSync` and ignores that classification.
5. The outer tick loop logs the failure and continues, so systemd legitimately sees a live process even when every request is permanently rejected.
6. Center validates identity/token/binding/fingerprint before writes. An accepted heartbeat updates authoritative timestamps; a later tick adds HostSample after receiving a plan. The inspected handler/store/read path contains no normal silent discard of an otherwise valid active-instance request.
7. Center's only `200`/no-write paths are paused/retired/archived suppression and duplicate batch dedupe. They remain operational discriminators but do not match a fresh enabled linked instance as closely as queue head-of-line blocking.

## Failure sequence matching the report

1. An old request remains in `/var/lib/houfeng-agent/sync-buffer.json` after credentials, binding epoch, fingerprint, database state or HMAC authority changes.
2. The current v0.79.1 agent loads credentials, starts normally and creates a fresh heartbeat every five seconds.
3. Each fresh heartbeat is appended behind the old entry.
4. Center rejects the old entry with 401, 404, 409 or a nonretryable 400.
5. Runtime marks the old entry attempted, returns from flush, logs one error and waits for the next tick.
6. No current heartbeat is accepted, so no plan is returned and HostSample collection never starts. UI truthfully remains `待接入` / `未收到心跳` / agent `—`.

## Root-cause confidence boundary

The repository proves that this failure mode is sufficient and currently untested. It does not prove which production event made the oldest entry invalid. Candidate triggers include binding reset/re-enrollment, reused state from another instance, token/HMAC rotation, database restore or fingerprint change. A normal same-instance binary upgrade with still-valid credentials is not sufficient by itself.

The following privacy-safe evidence would distinguish the live trigger without blocking the code fix:

- journal status/code sequence for `/api/agent/sync`, excluding response text and credentials;
- queue entry count, oldest age, attempts and equality-only comparison of oldest versus current instance/token/fingerprint;
- proxy endpoint/status counts and whether Authorization/Host were preserved;
- authoritative instance lifecycle/monitoring/binding/archive fields plus heartbeat/HostSample/sync-batch counts;
- deployed center/agent image digests and stability of the center HMAC secret.

Never print or attach the raw token file or sync queue: both contain live credentials.

## Options considered

### A. Identity-aware queue draining plus typed remote-error policy — recommended

- Drop queued entries whose MonitoringInstance ID, sync token or carrier fingerprint does not match the runtime's current authority, log a sanitized reason, and continue to the current heartbeat in the same flush.
- Drop center-classified `invalid_json` / `invalid_request` poison entries after recording a sanitized error, then continue.
- Treat current-authority 401/404/409/405 and other nonrecoverable 4xx as terminal runtime errors so systemd/journal exposes intervention rather than a false healthy loop.
- Retain and retry network failures, 429, 503 and other 5xx; preserve oldest-first/backfill for valid current-authority history.

This directly fixes the queue liveness invariant while preserving center authority and offline durability.

### B. Drop the oldest entry after an attempt threshold — rejected

An arbitrary retry count would destroy facts during a long but recoverable outage and cannot distinguish stale credentials from a temporary 503.

### C. Mark Monitoring as connected from binding or systemd state — rejected

This would make the screenshots look better while leaving the data path broken and would violate the authoritative heartbeat/sample contract.

### D. Document manual queue deletion as the only remediation — rejected

Manual deletion is a useful emergency discriminator, but the raw queue contains credentials and the same code defect would recur. Existing release history already records a host recovering after this file was cleared.

## Minimum RED

`TestRuntimeRejectedPersistedIdentityDoesNotBlockCurrentCredentialHeartbeat` must use a real `syncqueue.FileStore`, seed an older instance-A request, run with saved current instance-B credentials and have a credential-aware fake client reject A with typed 401/409 while accepting B. Current code never attempts B; the fixed runtime must discard A and deliver B without losing the newly enqueued batch.

Companion tests must cover poison 400 deletion, current-identity permanent rejection, transient retention/retry, valid current-identity backlog ordering and log redaction.

## Supporting research

- `research/agent-sender-flow.md` contains the exact installer/runtime/queue/release trace.
- `research/center-receiver-flow.md` contains the enroll/sync/persist/read trace, explicit reject/suppress matrix and center integration gap.
