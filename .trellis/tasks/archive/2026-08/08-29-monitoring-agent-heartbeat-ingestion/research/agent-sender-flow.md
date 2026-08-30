# Research: v0.79.1 agent sender flow and persistent-state failure modes

- Query: Trace the exact center-to-agent bootstrap, identity, authentication, heartbeat, HostSample, retry, logging, and test paths for the reported v0.79.1 behavior; determine whether retained credentials or a retained sync queue can explain an active/bound agent with no heartbeat.
- Scope: mixed (repository code, git history, release metadata, tests, and operational reproduction design)
- Date: 2026-08-29

## Findings

### Conclusion and sufficiency verdict

There is no v0.79.1 agent-protocol change that explains the symptom by itself. The v0.79.1 bootstrap fix changed the Overview UI path, while the released agent, installer, HTTP client, runtime, and center ingestion paths are unchanged between the v0.79.1 tag and this task's `bbf4f043` baseline.

For the narrower symptom **“the target is already bound, the service is active, but no new heartbeat is recorded”**, an invalid persisted queue head is the strongest code-complete single-cause hypothesis:

1. Every tick enqueues a new request, then lists the persistent queue oldest-first (`agent/runtime/runtime.go:311-329`; `agent/syncqueue/store.go:118-169`, `agent/syncqueue/store.go:434-440`).
2. Each queue entry contains the identity and sync credential captured when it was created, not merely facts to be re-signed with the current credential (`agent/syncqueue/store.go:36-53`, `agent/syncqueue/store.go:443-449`).
3. The first failed entry is marked attempted and immediately stops the flush; later current-identity heartbeats are not attempted (`agent/runtime/runtime.go:329-340`).
4. A remote 401/404/409 is treated as retryable queue failure. The runtime logs it, remains alive, and tries the same oldest entry on later ticks (`agent/runtime/runtime.go:216-224`, `agent/runtime/runtime.go:345-354`).
5. Center authentication/binding validation occurs before heartbeat persistence (`internal/center/http/handlers/agent.go:248-308`, `internal/center/store/sync_batches.go:72-83`, `internal/center/store/sync_batches.go:345-415`), so an active process can make requests forever without creating a heartbeat row.

This queue explanation is sufficient only when the oldest retained entry's identity/token/fingerprint is no longer accepted—for example, after binding reset and re-enrollment, token-hash/key rotation, database restoration, or reuse of the state directory for another monitoring instance. A retained entry with still-valid credentials drains normally and is not sufficient by itself.

Retained post-enrollment credentials are also sufficient if they are stale or belong to another instance. The installer deliberately ignores the newly generated one-time token whenever the existing token file looks like post-enrollment JSON (`internal/center/installer/houfeng-agent-install.sh:271-280`), and the runtime deliberately skips enrollment whenever those persisted credentials load (`agent/runtime/runtime.go:182-193`; locked by `agent/runtime/runtime_test.go:326-357`). If the saved credentials are valid for another instance, heartbeats go there; if they are invalid, the service remains active and retries rejected syncs. Mere credential preservation during a same-instance upgrade is not sufficient when the preserved credential still matches center state.

The two mechanisms can compose: the installer preserves the token JSON and the same `/var/lib/houfeng-agent/sync-buffer.json`; even after credentials are corrected, an older rejected queue entry can continue to block all current heartbeats for up to the queue's 72-hour age bound.

### Version, commit, and release evidence

- `v0.79.1` resolves to `c2ba7b972fb9d90a5ac30d26a39ebd6e294a4139`; the task baseline is `bbf4f043bdfbb7aa792da408a562bc05d57d1cfb`.
- A path-limited diff from `v0.79.1` to `bbf4f043` over `agent/`, `cmd/houfeng-agent`, relevant center handlers/stores, installer, systemd docs, Makefile, and release workflow is empty.
- The bootstrap restoration commit is `30cbe86597469eb29e39d7989dcd52d3c03c3434` (`fix: restore monitoring agent bootstrap`). It changes the Overview anomaly construction and web routes/components/tests, not agent bootstrap artifacts, sender code, credentials, endpoints, or ingestion.
- The one-command flow and preservation behavior originated in `d3eb4e66ee2991bb6740f670cd8311156c0d648d` (`Add one-command agent install flow`).
- Restart-on-upgrade behavior originated in `c9a1d4fd9121a88bad99e90fcc2db33a7c09d2c4` (`Fix agent installer restart on upgrade (#113)`).
- Legacy `node_id` credential preservation originated in `8b78a939a3f6823648976bb21891ce8f574e4df2` (`fix(agent): support legacy sync state upgrade`).
- The public v0.79.1 release targets `c2ba7b9...`; its Linux amd64 agent asset is 7,032,994 bytes with SHA-256 `02f2ea6af7a78960506302347a65d8647679bf074b1eae1f3b43d9c4df8286d8`. The release-triggered publish workflow completed successfully at the same SHA.
- The release workflow builds the version-stamped agent assets and verifies the artifact set before signing/uploading (`.github/workflows/publish-images.yml:76-120`, `.github/workflows/publish-images.yml:138-176`; `Makefile:67-88`). This makes a missing or accidentally unstamped v0.79.1 agent asset a low-evidence hypothesis.

### Files found

| File | Relevance |
| --- | --- |
| `internal/center/http/handlers/monitoring_instance_onboarding.go` | Creates the one-time token and renders the exact install command. |
| `cmd/houfeng-center/bootstrap.go` | Supplies public base URL/version/repository and wires install/enroll/sync handlers. |
| `internal/contracts/agentapi/routes.go` | Single source of truth for the three agent HTTP paths. |
| `internal/center/installer/houfeng-agent-install.sh` | Writes agent env, token, buffer settings, systemd unit, and restart behavior. |
| `internal/center/installer/embed.go` | Embeds the installer served by the center. |
| `agent/config/config.go` | Maps the installer's environment names into runtime config. |
| `cmd/houfeng-agent/main.go` | Constructs stdout logging, token store, fingerprint provider, and runtime. |
| `agent/token/file.go` | Distinguishes a plaintext enrollment token from persisted JSON sync credentials. |
| `agent/fingerprint/provider.go` | Derives identity fingerprint from machine-id or hostname; there is no identity env field. |
| `agent/enroll/client.go` | Sends enroll/sync requests to exact routes and maps every non-2xx to a remote error. |
| `agent/runtime/runtime.go` | Chooses enroll versus saved credentials, builds heartbeat/HostSample requests, queues, flushes, retries, and logs. |
| `agent/syncqueue/store.go` | Persists complete requests, including identity/token, and lists oldest-first. |
| `internal/center/http/handlers/agent.go` | Authenticates enrollment and sync requests and maps rejected states to HTTP errors. |
| `internal/center/store/monitoring_instances.go` | Issues/consumes enrollment tokens, binds instances, and resets binding credentials/state. |
| `internal/center/store/sync_batches.go` | Validates a sync transaction before persisting heartbeat and observations. |
| `internal/center/store/agent_plan.go` | Returns the HostSample collection tier after an accepted sync. |
| `docs/deploy/local-and-systemd.md` | Documents exact env values and the existing-credential preservation contract. |

### Exact center-to-agent value and endpoint trace

| Center-produced value | Artifact/wire representation | Exact agent consumer | Effect |
| --- | --- | --- | --- |
| `cfg.PublicBaseURL`, trimmed | Install command `--server-url <publicBaseURL>` (`internal/center/http/handlers/monitoring_instance_onboarding.go:83-89`, `internal/center/http/handlers/monitoring_instance_onboarding.go:125-138`) | Installer writes `HOUFENG_AGENT_SERVER_URL` (`internal/center/installer/houfeng-agent-install.sh:260-267`); config reads it (`agent/config/config.go:29-38`, `agent/config/config.go:55-63`) | HTTP client trims a trailing slash and appends the agent route (`agent/enroll/client.go:38-44`, `agent/enroll/client.go:87-104`). |
| One-time token issued for requested monitoring instance | Token is passed on stdin to `--enrollment-token-stdin`; it is not placed in argv (`internal/center/http/handlers/monitoring_instance_onboarding.go:105-138`) | Installer normally writes plaintext `/etc/houfeng-agent/token` (`internal/center/installer/houfeng-agent-install.sh:275-280`); `FileSource.Token` reads it (`agent/token/file.go:21-29`, `agent/token/file.go:84-110`) | `POST /api/agent/enroll` body `{token,fingerprint}` (`agent/runtime/runtime.go:241-252`; `agent/enroll/client.go:47-53`). |
| Monitoring instance ID + new sync token returned by accepted enrollment | Enrollment response | Runtime validates bound/nonempty token and atomically replaces the plaintext file with JSON credentials (`agent/runtime/runtime.go:255-267`; `agent/token/file.go:46-75`) | All later sync requests identify this instance and authenticate with the sync token. |
| Current/legacy JSON in an existing token file | `{monitoring_instance_id|node_id, sync_token}` | Installer recognizes only the key shapes and preserves the file (`internal/center/installer/houfeng-agent-install.sh:271-280`); runtime loads it through `SyncCredentials` (`agent/token/file.go:32-44`) | Enrollment token from the newly generated command is ignored; runtime does not call enroll (`agent/runtime/runtime.go:182-193`). |
| Machine identity | No center-generated identity field | `/etc/machine-id`, falling back to hostname, SHA-256 hex (`agent/fingerprint/provider.go:12-39`) | Sent during enrollment and every heartbeat; center uses it for binding checks. |
| Heartbeat identity/auth | Request object contains monitoring instance ID and sync token (`agent/runtime/runtime.go:270-280`) | HTTP client serializes the ID/facts and sends `Authorization: Bearer <sync token>`; it excludes `sync_token` from JSON (`agent/enroll/client.go:55-85`) | `POST /api/agent/sync`; center authenticates before persistence. |
| HostSample plan | Plan returned by a successful sync | Runtime stores the plan and then collects on a later tick (`agent/runtime/runtime.go:357-360`, `agent/runtime/runtime.go:387-405`) | First post-enrollment sync is heartbeat-only; HostSample can first appear on the next sync. |

The shared route constants are `/api/agent/enroll`, `/api/agent/sync`, and `/api/agent/install.sh` (`internal/contracts/agentapi/routes.go:3-7`). Center exposes these routes without browser-session middleware (`internal/center/http/router.go:109-112`, `internal/center/http/router.go:554-562`).

### Generated installation and systemd state

- The onboarding handler requires configured public base URL, agent version, and release repository, then issues a token for the requested instance and returns the full command (`internal/center/http/handlers/monitoring_instance_onboarding.go:71-148`). Tests assert exact installer URL, server URL, version, stdin token transport, and optional HTTP flag (`internal/center/http/handlers/monitoring_instance_onboarding_test.go:189-289`).
- The installer downloads an exact version/platform artifact and verifies minisign/checksums before installation (`internal/center/installer/houfeng-agent-install.sh:165-250`).
- It always rewrites `agent.env` with server URL, token path, persistent buffer path, and queue limits: 65,536 entries, 72 hours, 64 MiB (`internal/center/installer/houfeng-agent-install.sh:260-267`).
- It conditionally preserves the existing JSON credential file but always reuses the same persistent buffer path (`internal/center/installer/houfeng-agent-install.sh:271-280`). There is no correlation check between the existing monitoring instance ID and the instance for which the new one-time token was issued.
- The systemd unit runs as `houfeng-agent`, loads `/etc/houfeng-agent/agent.env`, restarts on all exits after 10 seconds, and retains `/var/lib/houfeng-agent` plus the token file as writable state (`internal/center/installer/houfeng-agent-install.sh:282-306`). Installation restarts an active service or starts an inactive one (`internal/center/installer/houfeng-agent-install.sh:309-318`).
- The operator guide explicitly describes rerunning the generated command on a bound node as preserving sync credentials and restarting the new binary (`docs/deploy/local-and-systemd.md:564-589`). That is a valid same-binding upgrade contract, but it is not a safe cross-instance or post-reset re-enrollment contract.

### Startup, enrollment, heartbeat, and HostSample timing

1. `cmd/houfeng-agent/main.go:17-31` loads required config, creates a text logger on stdout for journald, wires the file token source and fingerprint provider, and exits nonzero on a fatal runtime error.
2. Runtime logs `agent runtime started` with server URL, derives fingerprint, and loads saved credentials (`agent/runtime/runtime.go:174-193`). Saved credentials win over any plaintext enrollment path.
3. Without saved credentials, enrollment is synchronous and fatal on any error. Successful bound enrollment is logged and its credentials are persisted (`agent/runtime/runtime.go:241-267`). Systemd therefore restarts failed enrollment attempts every 10 seconds.
4. Runtime waits for the first five-second ticker before building a sync (`agent/runtime/runtime.go:21-24`, `agent/runtime/runtime.go:195-205`). Every request always contains one heartbeat (`agent/runtime/runtime.go:270-280`).
5. The production runtime always enqueues, prunes, and flushes the persistent queue (`agent/runtime/runtime.go:311-321`). A remote flush failure is logged and the loop continues (`agent/runtime/runtime.go:216-224`).
6. A HostSample is added only when the already-applied plan enables a valid frequency tier; collection failure is logged without removing the heartbeat (`agent/runtime/runtime.go:282-284`, `agent/runtime/runtime.go:387-405`). Consequently, HostSample collector failure cannot explain zero heartbeat.
7. Center requires at least one heartbeat, validates auth/binding, and only then records heartbeat and observations in one transaction (`internal/center/store/sync_batches.go:63-83`, `internal/center/store/sync_batches.go:101-128`). A fresh accepted heartbeat receives a plan; HostSample normally follows on a later tick.

### Retry, error, and logging behavior

- Enrollment non-2xx is returned as a typed remote error and terminates the runtime; systemd restart behavior makes this a visible restart loop (`agent/enroll/client.go:110-145`; `agent/runtime/runtime.go:241-252`; `internal/center/installer/houfeng-agent-install.sh:295-296`).
- Sync non-2xx is wrapped as `errRemoteSync`, the queue entry's attempt count is incremented, and the runtime logs `sync queue flush failed` then continues (`agent/runtime/runtime.go:216-224`, `agent/runtime/runtime.go:329-354`). HTTP status class is not used to distinguish permanent 4xx from transient failure.
- The center maps invalid enrollment token to 401, missing/invalid sync token to 401, binding mismatch to 409, and missing instance to 404 (`internal/center/http/handlers/agent.go:199-308`). Any of these at queue head produces indefinite head-of-line blocking until age/count/size pruning removes it.
- Agent logs intentionally go to stdout/journald and must not expose tokens (`.trellis/spec/backend/logging-guidelines.md:13-16`, `.trellis/spec/backend/logging-guidelines.md:34-59`, `.trellis/spec/backend/logging-guidelines.md:97-107`). The log message includes the remote status/code/message but not request credentials.
- Paused/retired/archived instance state is a separate behavior: valid sync is accepted but writes and plans are suppressed (`internal/center/store/sync_batches.go:85-95`, `internal/center/store/sync_batches.go:339-343`). This can produce no heartbeat despite successful HTTP, but it contradicts the normal newly created/enabled path unless state was later changed.

### Ranked single-root-cause hypotheses

1. **Rejected persistent queue head blocks the current heartbeat.** Highest fit for “bound + active + zero new heartbeat,” provided identity/credential state changed while `/var/lib/houfeng-agent/sync-buffer.json` survived. The entire failure is proven by queue ordering, entry-local credentials, early return, pre-write auth, and remote-error continuation. A normal binary-only upgrade with no credential/state change does not create the required rejected entry.
2. **Preserved JSON credentials are stale or belong to a different instance.** Highest fit if the generated command was used as “重新接入,” after reset/recovery, or on a reused host. The new enrollment token is deterministically ignored. It explains active/no-data; it explains a still-bound target when the row retains historical bound state but its accepted token/hash no longer matches, or when traffic is actually going to another instance. It does not explain failure for a clean same-instance upgrade with valid saved credentials.
3. **Center URL/proxy/TLS reachability from the agent host.** The base URL is copied exactly from center config into agent env. An enroll failure causes a restart loop; a sync failure remains active and retries. This needs host journal/access-log evidence and is not suggested by a repository regression.
4. **Lifecycle write suppression.** Valid requests for a paused/retired/archived instance can be accepted while heartbeat writes are suppressed, but fresh instance creation normally starts enabled/unbound; check actual row state.
5. **Broken v0.79.1 release asset.** Low evidence: exact release assets exist, publishing succeeded, and no relevant code changed after the tag.

### Minimal reproductions

#### Working reference: pristine enrollment

1. Use a clean Linux host with neither `/etc/houfeng-agent/token` nor `/var/lib/houfeng-agent/sync-buffer.json`.
2. Create/link instance B and run its generated v0.79.1 command within the token's validity window.
3. Expect one enroll request for B, a first heartbeat after about five seconds, a returned plan, and a HostSample on a later tick.
4. Compare the amd64 binary hash to the public v0.79.1 hash above if release provenance is in question.

#### Persisted-credential reproduction

1. Enroll host A normally so `/etc/houfeng-agent/token` becomes post-enrollment JSON.
2. Generate an install command for instance B and run it on the same host without deleting state.
3. Observe the installer message `preserving existing post-enrollment token file`.
4. The runtime makes zero enrollment calls for B and sends sync using A's stored identity/token. If A credentials remain valid, data lands on A; if rejected, the service stays active and journals a repeated sync queue failure.
5. Do not print the token file. Classify its shape or compare only monitoring instance IDs with sync tokens redacted.

#### Persistent-queue head-of-line reproduction

1. Seed the persistent queue with an oldest request carrying invalid instance-A credentials.
2. Make the current token store contain valid instance-B credentials and start the runtime with the same queue path.
3. Each tick enqueues B, attempts A first, receives 401/404/409, increments A's attempts, and returns. No B heartbeat reaches center; the service remains active and the queue grows.
4. Repeat with a valid A entry as the control: A drains, then B is sent. This proves invalidity/mismatch—not age alone—is the causal condition.

### Minimum RED regression test recommendation

Add one focused runtime test beside the existing queue retry/restart tests in `agent/runtime/runtime_test.go:659-787`, provisionally named:

`TestRuntimeRejectedPersistedIdentityDoesNotBlockCurrentCredentialHeartbeat`

Minimum fixture and contract:

1. Use the real `syncqueue.FileStore` and seed one oldest request for instance A with an old token.
2. Use persisted current credentials for instance B, so enrollment calls must remain zero (same setup principle as `agent/runtime/runtime_test.go:326-357`).
3. Use a credential-aware fake client: return typed 401 `invalid_sync_token` for A and accept B.
4. Run enough ticks for one retry.
5. Assert that a heartbeat for B reaches `Sync` despite the rejected A entry; retain an assertion that the current B batch is not silently lost. Do not prescribe whether A is dropped, quarantined, or bypassed until the product retry policy is chosen.

This test is RED against current behavior because `flushSyncQueue` returns at A on every tick. It directly captures the reported ingestion invariant without coupling the test to a particular remediation. A separate installer test for cross-instance re-enrollment should wait until product semantics decide whether an existing JSON credential must be preserved, rejected, or explicitly replaced when a newly generated command targets another instance.

### Existing tests and gaps

- Onboarding command tests cover exact URL/version/token stdin transport (`internal/center/http/handlers/monitoring_instance_onboarding_test.go:189-289`).
- Installer embed tests explicitly require current and legacy JSON credential preservation (`internal/center/installer/embed_test.go:51-60`). They do not correlate preserved identity with the command's target identity.
- Token tests cover plaintext enrollment token, atomic replacement with current JSON credentials, legacy `node_id`, current-field precedence, and incomplete JSON (`agent/token/file_test.go:13-137`).
- Runtime has explicit coverage that saved sync credentials skip enrollment and are used verbatim (`agent/runtime/runtime_test.go:326-357`), successful enrollment persistence (`agent/runtime/runtime_test.go:359-386`), heartbeat-before-HostSample plan timing (`agent/runtime/runtime_test.go:466-551`), HostSample failure with heartbeat continuation (`agent/runtime/runtime_test.go:553-587`), transient retry/backfill (`agent/runtime/runtime_test.go:659-712`), and restart queue flushing (`agent/runtime/runtime_test.go:753-787`).
- Queue tests prove persistence across restart and preservation of legacy request identity/token (`agent/syncqueue/store_test.go:17-84`).
- Not covered: nonretryable 4xx at queue head, credential rotation with retained queue, binding reset followed by the generated reinstall command, cross-instance host reuse, a current credential behind an old rejected entry, or operational redaction of queue diagnostics.

### Privacy-safe evidence collection

- Capture `systemctl status houfeng-agent --no-pager` and `journalctl -u houfeng-agent --since <install-time> --no-pager`.
- Read only `HOUFENG_AGENT_SERVER_URL` from `/etc/houfeng-agent/agent.env`.
- Classify `/etc/houfeng-agent/token` as plaintext enrollment versus post-enrollment JSON; never print the enrollment or sync token.
- Inspect queue metadata with request `sync_token` removed. Required fields are entry ID, creation time, attempts, redacted monitoring instance ID, and heartbeat batch ID. The queue file contains live sync credentials and must not be attached raw.
- Correlate server access logs and database records by monitoring instance ID, endpoint, response status/code, binding state, last heartbeat, and counts. Do not select or log token hashes/secrets.
- Check whether `/api/agent/enroll` was ever called after the v0.79.1 command and whether `/api/agent/sync` attempts name the intended instance or another instance.

### External references

- v0.79.1 release: https://github.com/xiangnan0811/houfeng/releases/tag/v0.79.1
- Successful release-triggered image/asset workflow for the tag SHA: https://github.com/xiangnan0811/houfeng/actions/runs/33252579902
- No external protocol standard governs this private agent contract; repository route constants, handlers, and tests are authoritative.

### Related specs

- `.trellis/spec/backend/directory-structure.md:161-197` — current/legacy local credential compatibility and installer preservation.
- `.trellis/spec/backend/directory-structure.md:482-559` — one-command bootstrap fields, routes, artifacts, and public base URL authority.
- `.trellis/spec/backend/database-guidelines.md:1021-1065` — one-time enrollment token and sync credential contract.
- `.trellis/spec/backend/database-guidelines.md:1240-1304` — lifecycle-state write suppression.
- `.trellis/spec/backend/quality-guidelines.md:267-322` — persistent agent queue scenarios.
- `.trellis/spec/backend/quality-guidelines.md:475-482` — cross-layer agent protocol verification.
- `.trellis/spec/backend/logging-guidelines.md:85-107` — expected lifecycle/retry logs and secret non-disclosure.
- `.trellis/tasks/archive/2026-08/08-29-monitoring-agent-bootstrap/research/current-flow-audit.md:1-42` — v0.79.0 UI bootstrap-route root cause and explicit non-protocol scope.

## Caveats / Not Found

- No report-specific host journal, installer stdout, redacted token shape, queue metadata, center access log, or database state was available. The repository proves sufficiency and gives discriminators, but does not confirm which persistent-state condition occurred in production.
- “Bound” must be verified for the exact target instance and current binding epoch. A historical bound row does not prove that the host's current sync token is accepted, and traffic may instead be reaching a different preserved instance ID.
- It is not known whether the host was pristine, whether binding was reset, whether the center HMAC/session key or database was rotated/restored, or whether the state directory came from another installation.
- It is not known whether the oldest queue entry returns 401, 404, 409, or a transport error. The exact response determines whether credential mismatch, missing instance, binding mismatch, or network/proxy failure is primary.
- It is not known whether the target lifecycle is active versus paused/retired/archived; suppressed writes must be excluded from database state.
- The intended product semantics for a newly generated command on a host with post-enrollment JSON are unresolved: same-instance upgrade, explicit re-enrollment, and cross-instance reuse require distinguishable contracts before an installer regression expectation can be fixed.
- No code or configuration was changed during this research.
