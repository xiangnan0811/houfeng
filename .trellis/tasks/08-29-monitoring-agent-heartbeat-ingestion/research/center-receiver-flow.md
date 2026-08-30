# Research: center receiver flow

- Query: Trace the v0.79.1 center-side agent enroll/sync/persist/read path, determine whether it has an independent break, and identify silent reject/ignore paths that can leave Monitoring at `待接入`.
- Scope: mixed (repository source/tests/specs plus GitHub commit/tag metadata; no production logs or database access)
- Date: 2026-08-29

## Findings

### Conclusion

The inspected v0.79.1-compatible center happy path is internally continuous: enrollment binds one MonitoringInstance and issues a sync token; the sync route requires that token as a Bearer credential; production bootstrap gives enrollment and sync repositories the same HMAC root; one accepted heartbeat and HostSample transaction writes all facts and promotes `待接入` to `在用`; list/detail/onboarding/runtime-facts readers expose those same rows. No independent, deterministic center break was found in this path.

That conclusion is source-level, not an end-to-end proof. Existing tests stop at handler fakes, transaction fakes, or bootstrap source-string assertions. There is no test that takes a token issued by the real enrollment repository through the real HTTP sync handler, PostgreSQL writes, and the read APIs. That missing boundary should be the first regression test.

The reported persistent combination—service process healthy, center has neither heartbeat nor HostSample evidence, UI remains `待接入`, and agent version is `—`—is more consistent with requests never reaching acceptance than with a read/UI-only fault. The strongest static hypothesis is an agent durable-queue head-of-line entry carrying stale instance/token/fingerprint authority: the runtime keeps running and logging remote sync failures, but retries the oldest rejected entry before every newer batch for up to the default 72-hour retention. Center-side identity/auth rejection is the next class to verify. Center also has two intentional `200 accepted` no-write paths, but fresh linked-instance defaults make them less likely.

### Files found

- `internal/contracts/agentapi/routes.go` — stable agent-facing enroll/sync paths.
- `internal/contracts/agentapi/types.go` — enrollment, heartbeat, HostSample, sync request/response wire DTOs.
- `internal/center/http/router.go` — agent routes and browser-auth separation; protected Monitoring read routes.
- `internal/center/http/handlers/agent.go` — rate/concurrency gates, Bearer extraction, JSON decoding, validation, service error mapping.
- `internal/center/enrollment/service.go` — enrollment service boundary; its heartbeat method is not used by `/api/agent/sync`.
- `internal/center/syncing/service.go` — repository transaction call and post-sync processors.
- `internal/center/store/monitoring_instances.go` — linked-instance creation, enrollment token consumption, binding/fingerprint epoch, sync-token storage, onboarding reads.
- `internal/center/store/agent_token_hash.go` — purpose-separated HMAC and legacy SHA-256 compatibility.
- `internal/center/store/sync_batches.go` — accepted-sync validation, suppression, deduplication, atomic writes, plan creation, lifecycle transition.
- `internal/center/store/observations.go` — HostSample insert.
- `internal/center/store/runtime_facts.go` — latest/window HostSample reads.
- `internal/center/http/handlers/monitoring_instances.go` — Monitoring list/detail API responses.
- `internal/center/http/handlers/monitoring_instance_onboarding.go` — onboarding-state and token/install-command APIs.
- `internal/center/http/handlers/runtime_facts.go` — runtime-facts and live stream APIs.
- `cmd/houfeng-center/bootstrap.go` — production construction and route wiring.
- `agent/enroll/client.go` — released agent Bearer request shape (used only to test the center/agent contract boundary).
- `agent/runtime/runtime.go`, `agent/syncqueue/store.go` — outside-center evidence for persistent queue head-of-line behavior.
- `web/src/pages/monitoring/monitoringHelpers.ts`, `MonitoringInstancesTableColumns.tsx`, `MonitoringPage.tsx`, `monitoring-detail/MonitoringInstanceWatchtowerHeader.tsx`, `MonitoringDetailPage.tsx`, `monitoring-detail/MonitoringDetailPageBody.tsx` — list/detail state and version/heartbeat rendering.
- `db/migrations/0001_initial_schema.sql`, `0029_rename_nodes_to_monitoring_instances.sql`, `0045_create_agent_sync_batches.sql` — heartbeat/HostSample storage and sync-batch idempotency key.

### Complete expected data flow

1. **Routes and authority.** `POST /api/agent/enroll` and `POST /api/agent/sync` are the stable paths (`internal/contracts/agentapi/routes.go:4-6`). They are deliberately registered outside browser session/CSRF middleware (`internal/center/http/router.go:109-113`, `internal/center/http/router.go:554-562`). The agent protocol carries no tenant identifier: center authority is the tuple of MonitoringInstance identity, enrollment/sync secret, binding state, and exact fingerprint. Browser Monitoring read APIs remain session/scope protected (`internal/center/http/router.go:417-423`). The outer allowed-host middleware still applies to agent routes and returns explicit HTTP 400 on a mismatched `Host` (`internal/center/http/middleware.go:106-120`).

2. **Enrollment and binding.** The enroll handler accepts only POST, bounded JSON `{token,fingerprint}`, and maps an invalid enrollment token to 401 (`internal/center/http/handlers/agent.go:199-245`; DTO at `internal/contracts/agentapi/types.go:57-67`). Enrollment selects an unconsumed token issued in the last 30 minutes (`internal/center/store/monitoring_instances.go:2093-2124`). An unbound instance becomes bound to the submitted fingerprint and a new binding epoch; the same bound fingerprint stays bound, while a different fingerprint becomes pending confirmation (`internal/center/store/monitoring_instances.go:2025-2070`). Only a bound result gets a new sync token, stored as a hash, and the enrollment token is consumed atomically (`internal/center/store/monitoring_instances.go:2137-2187`).

3. **Sync HTTP decoding.** The sync handler rate-limits before decoding and has a 32-request default inflight gate; overloads are explicit 429 or 503 responses (`internal/center/http/handlers/agent.go:67-75`, `internal/center/http/handlers/agent.go:168-196`). It requires the exact case-sensitive `Authorization: Bearer <nonempty-token>` shape and rejects whitespace/quotes/backslashes and tokens over 512 bytes (`internal/center/http/handlers/agent.go:264-280`, `internal/center/http/handlers/agent.go:311-330`). The handler overwrites `req.SyncToken` with the header token, so a JSON-only `sync_token` cannot authenticate. The released agent does send this header and deliberately omits the token from its JSON payload (`agent/enroll/client.go:55-84`).

4. **Wire validation.** A sync must have `monitoring_instance_id`, a valid Bearer-derived token, and at least one heartbeat. Every heartbeat and HostSample must have nonzero `observed_at` and nonempty `agent_version`, `fingerprint`, and `sync_batch_id`; each collection is capped at 256 (`internal/center/http/handlers/agent.go:341-473`). HostSamples without a heartbeat cannot reach storage. DTO field names and metric shapes are defined at `internal/contracts/agentapi/types.go:74-120` and `internal/contracts/agentapi/types.go:218-230`. Handler mapping is explicit: 401 invalid token, 409 binding/fingerprint not accepted, 404 missing instance, 400 invalid probe/request, otherwise 500 (`internal/center/http/handlers/agent.go:281-309`).

5. **Production key continuity.** Both the enrollment repository and sync repository receive the same `cfg.SessionHMACKey` (`cmd/houfeng-center/bootstrap.go:164-194`); the sync service and live-stream/incident post-processors are wired at `cmd/houfeng-center/bootstrap.go:193-225`, and the concrete handlers at `cmd/houfeng-center/bootstrap.go:390-421`. Agent token hashing uses versioned, purpose-separated HMAC, accepts legacy SHA-256 hashes, and migrates them after successful use (`internal/center/store/agent_token_hash.go:11-14`, `internal/center/store/agent_token_hash.go:25-33`, `internal/center/store/agent_token_hash.go:64-95`). Secret rotation invalidates already-HMAC-migrated agent tokens by documented contract (`.trellis/spec/backend/quality-guidelines.md:352-357`).

6. **Storage transaction.** `syncing.Service.SyncBatch` calls `Repository.ApplyBatch` then post-sync processors (`internal/center/syncing/service.go:72-99`). `ApplyBatch` starts one transaction, locks and validates the MonitoringInstance, requires bound state, verifies the sync-token hash, and requires every carrier fingerprint to equal the bound fingerprint (`internal/center/store/sync_batches.go:63-83`, `internal/center/store/sync_batches.go:332-416`). It inserts an idempotency row keyed by `(monitoring_instance_id, first heartbeat.sync_batch_id)`, then heartbeat, HostSample/probes/IP facts, state advancement, command results/plan, and commits (`internal/center/store/sync_batches.go:101-156`). The database primary key enforces that idempotency identity (`db/migrations/0045_create_agent_sync_batches.sql:1-8`). Heartbeats and HostSamples are separate fact tables (`db/migrations/0001_initial_schema.sql:54-90`) renamed to MonitoringInstance identity by `db/migrations/0029_rename_nodes_to_monitoring_instances.sql:3-30`. HostSample persistence is the insert at `internal/center/store/observations.go:53-123`. Any observation or plan-write error before commit rolls the entire batch back.

7. **Lifecycle and onboarding phase.** An accepted heartbeat always advances `last_heartbeat_at`/`last_sync_at`, but only a batch containing a HostSample promotes lifecycle `待接入` to `在用` (`internal/center/store/sync_batches.go:418-423`, `internal/center/store/sync_batches.go:504-525`). A freshly VPS-linked instance is created `待接入`, monitoring enabled, and unbound (`internal/center/store/monitoring_instances.go:1157-1180`, `internal/center/store/monitoring_instances.go:1204-1294`). Onboarding derives conflict for pending binding, complete only when bound plus heartbeat plus HostSample/accepted observation, bound-awaiting-observation otherwise, and not-started when unbound (`internal/center/monitoringinstances/types.go:409-420`). The agent itself does not include a HostSample in its first sync because it has no center plan yet; the first accepted heartbeat returns a plan, and a later tick can collect a HostSample (`agent/runtime/runtime.go:270-294`, `agent/runtime/runtime.go:387-415`). This can transiently leave lifecycle `待接入`, but it cannot explain persistent absence of `last_heartbeat_at`.

8. **Read and UI mapping.** List and detail handlers return repository records directly (`internal/center/http/handlers/monitoring_instances.go:21-35`, `internal/center/http/handlers/monitoring_instances.go:61-82`). Runtime facts reads the latest HostSample by observed time and returns no latest sample when none exists (`internal/center/store/runtime_facts.go:24-57`, `internal/center/store/runtime_facts.go:234-305`). The list considers lifecycle `待接入`, unbound, or binding conflict pending (`web/src/pages/monitoring/monitoringHelpers.ts:74-88`) and displays `未收到心跳` when `last_heartbeat_at` is absent (`web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx:26-31`, `web/src/pages/monitoring/MonitoringInstancesTableColumns.tsx:113-135`). Detail shows agent version only from the latest HostSample, hence `—` with no sample, and uses HostSample time with record heartbeat fallback (`web/src/pages/monitoring-detail/MonitoringInstanceWatchtowerHeader.tsx:111-113`, `web/src/pages/monitoring-detail/MonitoringInstanceWatchtowerHeader.tsx:224-226`). The list fetches once on scope change, without polling (`web/src/pages/MonitoringPage.tsx:73-93`), so it may remain stale until reload; detail performs fresh detail/runtime-facts reads and its WebSocket updates after a successfully persisted HostSample (`web/src/pages/MonitoringDetailPage.tsx:337-405`). Therefore a stale list is possible, but the full reported state is faithful to missing accepted facts, not a standalone state-mapping defect.

### Explicit rejection versus silent/ignored behavior

| Boundary | Result | Writes | Expected observable evidence |
| --- | --- | --- | --- |
| Missing/malformed Bearer, wrong/rotated token | 401 | none | Agent remote error; proxy/access status. Body token is ignored for authority. |
| Unknown MonitoringInstance | 404 | none | Agent remote error; access status. |
| Unbound/pending-confirmation or fingerprint mismatch | 409 | none | Agent remote error; binding fields show unbound/conflict/current fingerprint mismatch. |
| Invalid carrier/body or stale/invalid probe metadata | 400 | none; whole transaction rejected | Agent remote error; no heartbeat even if the heartbeat itself was valid. Probe validation precedes writes (`internal/center/store/sync_batches.go:332-416`). |
| Wrong public `Host` | 400 | none | Outer middleware response `{"error":"invalid host"}` (`internal/center/http/middleware.go:106-120`). |
| Rate/concurrency pressure | 429/503 | none | 503 carries `Retry-After: 5`; retryable from access log. |
| DB write/commit error | 500 | rolled back unless failure is after commit | Center error/access status. |
| Primary post-sync incident processor fails | 500 after facts committed | facts already present | Important diagnostic asymmetry: the agent sees failure, but reads/DB should show facts (`internal/center/syncing/service.go:72-99`). |
| Monitoring paused, lifecycle retired, or archived | **200 accepted** with empty plan | **none, including no heartbeat/last_sync** | Intentional silent suppression (`internal/center/store/sync_batches.go:86-95`, `internal/center/store/sync_batches.go:339-343`). Covered by `internal/center/store/sync_batches_test.go:414-498`. |
| Duplicate `(instance, first heartbeat batch ID)` | **200 accepted** with empty plan | **none after idempotency row already exists** | Intentional idempotent short circuit (`internal/center/store/sync_batches.go:101-114`, `internal/center/store/sync_batches.go:158-175`; test `internal/center/store/sync_batches_test.go:235-268`). |

The two `200/no-write` paths are the only center paths found that can look healthy to a syntactically correct agent while leaving the instance unchanged. They are distinguishable in the database: suppressed state has no new `agent_sync_batches` row, whereas duplicate state already has the matching row. A newly linked instance defaults to monitoring enabled, `待接入`, and unarchived, so suppression requires later state mutation. A fresh runtime generates the batch ID from each tick timestamp (`agent/runtime/runtime.go:203-205`), so duplicate suppression requires a persisted/replayed batch or a broken third-party agent.

### Ranked root-cause hypotheses

1. **Agent durable queue head-of-line poison after identity/token/fingerprint change — strongest static fit, not yet runtime-confirmed.** Every tick enqueues the new request, lists entries oldest-first, sends sequentially, and on the first remote failure marks one attempt and returns without reaching newer entries (`agent/runtime/runtime.go:311-352`; ordering at `agent/syncqueue/store.go:434-440`). The stored request retains its original MonitoringInstance ID, sync token, fingerprint, and facts. Remote errors are logged and the outer loop continues, so systemd can report a healthy running process indefinitely (`agent/runtime/runtime.go:216-224`). Default retention is 72 hours (`agent/config/config.go:12-16`; prune logic `agent/syncqueue/store.go:240-259`). Existing tests prove temporary failure retry but do not cover a terminal 401/404/409 at the queue head while a newer valid-identity entry waits (`agent/runtime/runtime_test.go:659-712`). This exactly predicts no accepted first heartbeat and no HostSample plan.

2. **Center rejects the current request at the auth/binding boundary.** Check 401/409 first: a stale token file, proxy stripping `Authorization`, case/format error, HMAC-secret rotation after enrollment, or changed fingerprint all reject before writes. Current released center and agent source agree on Bearer transport, and enrollment/sync repositories share the HMAC key, so no source-level protocol mismatch was found; deployment/config/runtime identity remains unverified.

3. **Atomic rejection caused by a persisted stale observation/probe assignment.** A batch containing an invalid probe identity/metadata rejects its otherwise-valid heartbeat before any writes. This is less likely for a clean first request because the runtime has no plan yet, but becomes plausible when the durable queue replays older planned batches.

4. **Intentional `200 accepted` no-write state suppression.** Paused/retired/archived state explains a healthy agent with no facts. It is lower-ranked because VPS-linked creation sets enabled/`待接入`/unarchived; verify authoritative DB state rather than UI labels.

5. **Duplicate sync-batch idempotency short circuit.** It explains `200/no-write` only if the same first-heartbeat batch ID already exists. It is lower-ranked for a fresh timestamp-generated batch, but can combine with a persisted queue/retry or third-party agent.

6. **UI list staleness only.** The list does not poll, so stale `待接入` is possible after acceptance, but it does not explain fresh detail/runtime-facts also showing no heartbeat/version. This is a contributing display behavior, not the primary ingestion hypothesis.

### Minimal reproducible regression boundary

Add one PostgreSQL-backed HTTP integration test (or the smallest existing test harness that uses a real PostgreSQL fixture) spanning the currently untested seam:

1. Create a VPS-linked MonitoringInstance and assert initial state: lifecycle `待接入`, monitoring enabled, binding unbound, unarchived.
2. Issue a real enrollment token and POST `/api/agent/enroll` with one fingerprint; capture the returned MonitoringInstance ID and plaintext sync token.
3. POST `/api/agent/sync` through the real router with `Authorization: Bearer <returned token>`, one valid heartbeat, and one valid HostSample carrying the same fingerprint and one fresh batch ID.
4. Assert HTTP 200 and, in PostgreSQL, one `agent_sync_batches` row, one heartbeat row, one HostSample row, non-null `last_heartbeat_at`/`last_sync_at`, and lifecycle `在用`.
5. Read through the real list, detail, onboarding, and runtime-facts handlers; assert list/detail `在用`, onboarding complete, and latest HostSample agent version present.

The same fixture should have focused negative subtests: JSON-only token → 401/no writes; fingerprint mismatch → 409/no writes; invalid probe metadata included with valid heartbeat/HostSample → 400/atomic no writes; paused state → 200/no writes; pre-seeded duplicate sync-batch key → 200/no fact rewrite. Separately, add an agent runtime test with an old queued request that always gets 401/409 followed by a new valid-identity request; assert whether the valid request is ever attempted. That test is outside the center product boundary but is the best reproducer for the leading hypothesis.

Existing coverage is narrower:

- Handler tests verify Bearer-over-body authority, request decoding, response mapping, limits, and carrier validation using a fake service (`internal/center/http/handlers/agent_test.go:268-502`, `internal/center/http/handlers/agent_test.go:693-795`, `internal/center/http/handlers/agent_test.go:831-1005`).
- Store tests verify rollback, invalid probes, heartbeat-required, token matching, lifecycle promotion, duplicate, and suppression using fake transactions (`internal/center/store/sync_batches_test.go:21-165`, `internal/center/store/sync_batches_test.go:235-326`, `internal/center/store/sync_batches_test.go:414-498`).
- Bootstrap tests assert handler presence and search source text for both HMAC constructors, but do not run enrollment-issued authority into sync (`cmd/houfeng-center/bootstrap_test.go:713-717`, `cmd/houfeng-center/bootstrap_test.go:821-835`).
- Runtime-facts tests prove a no-facts read returns no HostSample (`internal/center/store/runtime_facts_test.go:265-318`).

### Expected privacy-safe diagnostics

1. From reverse-proxy/HTTP access evidence, obtain counts and timestamps for `POST /api/agent/enroll` and `/api/agent/sync`, response statuses, latency, request ID, and client IP. Do not record Authorization, enrollment/sync tokens, raw payloads, IP-quality payloads, or raw fingerprint. Current logging guidance intentionally avoids per-batch application logs on the hot path (`.trellis/spec/backend/logging-guidelines.md:92-99`), so access logs and agent journal are necessary.
2. From the agent journal, capture the error class/status/code and queue attempt behavior with secrets redacted. Repeating 401/404/409 against one oldest entry while the service stays active supports the head-of-line hypothesis; repeating 400 supports invalid carrier/probe; 429/503 supports load gating.
3. Query the single MonitoringInstance for lifecycle, monitoring, binding, archived-at nullness, binding epoch, last heartbeat/sync, and only the token-hash **format/prefix**, never the token/hash value. Compare binding fingerprint using a one-way diagnostic digest or equality performed in-process, never raw fingerprint output.
4. Query privacy-safe counts/max received times for `agent_sync_batches`, `monitoring_instance_heartbeats`, and `host_samples`. For a 200/no-write result: no new sync-batch row plus paused/retired/archived state means suppression; an existing matching batch row means dedupe. Do not expose raw batch IDs; use exact in-DB equality or a short diagnostic digest.
5. If the agent saw 500, check facts before assuming rollback: a post-sync processor can fail after commit. If DB facts exist, the receiver transaction succeeded and the retry should dedupe.
6. Verify the deployed center and agent image digests, not only the `v0.79.1` label, plus proxy Authorization forwarding and stability of the session-HMAC secret across the enrollment-to-sync interval.

### Commit and release evidence

- Tag `v0.79.1` resolves to `c2ba7b972fb9d90a5ac30d26a39ebd6e294a4139` (`chore(main): release 0.79.1`, 2026-08-29). Its product merge parent is `54719379541f5168a1493ee9d7d44e8c03964276` (PR #474), whose implementation commit is `30cbe86597469eb29e39d7989dcd52d3c03c3434`, `fix: restore monitoring agent bootstrap`. The implementation changed VPS Overview/Monitoring navigation, copy, and onboarding UI plus one anomaly label; it did **not** change the agent client, agent HTTP handler, sync repository, agent DTO, database migration, or center bootstrap. Therefore v0.79.1 restored the path to create/open onboarding but did not independently modify or prove heartbeat ingestion. GitHub evidence: <https://github.com/xiangnan0811/houfeng/commit/c2ba7b972fb9d90a5ac30d26a39ebd6e294a4139>, <https://github.com/xiangnan0811/houfeng/commit/54719379541f5168a1493ee9d7d44e8c03964276>, <https://github.com/xiangnan0811/houfeng/commit/30cbe86597469eb29e39d7989dcd52d3c03c3434>.
- Baseline `bbf4f043bdfbb7aa792da408a562bc05d57d1cfb` is PR #476, which archives the task and updates the journal; it has no product-code changes. GitHub evidence: <https://github.com/xiangnan0811/houfeng/commit/bbf4f043bdfbb7aa792da408a562bc05d57d1cfb>.
- The relevant center/agent auth contract predates v0.79.1. Commit `48f3f363c402` (2026-06-26) added agent Bearer transport, HMAC-root wiring for enrollment and sync, and allowed-host enforcement. Commit `1d9df820d749b2a883a15cd9a9c1d160f0f9cd0c` made Bearer mandatory, replaced any JSON token with header authority, and removed `sync_token` from the outbound agent JSON. These paired changes are present together in v0.79.1; they do not evidence a current source mismatch. GitHub evidence: <https://github.com/xiangnan0811/houfeng/commit/48f3f363c402>, <https://github.com/xiangnan0811/houfeng/commit/1d9df820d749b2a883a15cd9a9c1d160f0f9cd0c>.

### Related specs

- `.trellis/spec/backend/database-guidelines.md:1240-1324` — accepted sync transaction and intentional paused/retired/archived no-write behavior.
- `.trellis/spec/backend/database-guidelines.md:2325-2332` — MonitoringInstance lifecycle/binding invariants.
- `.trellis/spec/backend/quality-guidelines.md:352-357` — shared HMAC root and rotation consequences.
- `.trellis/spec/backend/logging-guidelines.md:92-99` — no per-batch application logging on the agent sync hot path.
- `.trellis/spec/web/state-and-data.md:814-835` — Monitoring pending quick-view state.
- `.trellis/spec/guides/cross-layer-thinking-guide.md` — requirement to prove write-through/read-through contracts across layers.

### Unanswered technical questions

1. What exact HTTP status/code does the deployed agent journal show for enroll and each sync attempt, and does `/api/agent/sync` appear in proxy access logs?
2. What is in `/var/lib/houfeng-agent/sync-buffer.json`: entry count, oldest age, attempts, and whether oldest/current entries have equal MonitoringInstance/token/fingerprint authority? Inspect through a redacting tool; do not print secrets or raw fingerprints.
3. Does the deployed token file name the same MonitoringInstance as the current VPS link, and was it replaced/re-enrolled without clearing or rebinding the durable queue?
4. Did the reverse proxy preserve `Authorization` and the public `Host` value expected by center?
5. Was `HOUFENG_SESSION_HMAC_KEY` stable from enrollment through first sync, or did a redeploy/secret regeneration rotate it?
6. What are the authoritative DB states/counts described above, especially paused/retired/archived state and presence of an `agent_sync_batches` row without heartbeat/HostSample rows?
7. Does the first failing queued batch contain stale probe observations or a fingerprint from a prior binding epoch?
8. Are center and agent binaries actually the released v0.79.1 digest/architecture, rather than mixed or cached images with the same operator-visible version label?

## Caveats / Not Found

- No production agent journal, proxy access log, deployed config, queue file, image digest, or PostgreSQL row was supplied. The ranked hypotheses are not a root-cause confirmation.
- No independent deterministic break was found in the inspected center happy path, but the missing real PostgreSQL enroll→sync→read integration test prevents claiming it is end-to-end proven.
- The queue head-of-line hypothesis crosses outside the requested center boundary only to explain why a healthy process can generate no accepted center facts; no agent code was modified.
- Commit evidence was obtained through the GitHub API without performing any git operation.
