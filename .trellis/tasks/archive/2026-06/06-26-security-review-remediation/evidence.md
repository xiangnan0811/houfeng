# Security review remediation evidence matrix

Status values:

- `fixed`: implementation and tests in this task remediate the finding.
- `already-fixed`: existing code/tests still cover the report item.
- `compatibility-bound`: implemented compatible hardening, but the literal report requirement would break deployed agents.
- `scoped-deferred`: accepted recommendation intentionally left outside this task scope.

| ID | Report item | Current status | Evidence / notes |
| --- | --- | --- | --- |
| P1-01 | Public endpoint limiter maps can grow without bound | fixed | `internal/center/http/handlers/auth.go` and `agent.go` add `MaxTrackedKeys`, expired-key sweep, and global request budgets. Tests: `TestLoginLimiterCapsUsernameKeys`, `TestLoginLimiterSweepsExpiredKeys`, `TestAgentRequestLimiterCapsIPKeys`, `TestAgentRequestLimiterSweepsExpiredKeys`, and `TestAgentRequestLimiterAppliesGlobalLimit`. Trusted proxy all-address CIDRs are rejected in `internal/center/config/config.go` with config tests. |
| P1-02 | `/api/agent/sync` decodes large JSON before token rejection | compatibility-bound | New agent clients send `Authorization: Bearer <sync_token>` (`agent/enroll/client.go`), and the handler rejects malformed optional token headers plus saturated sync inflight gates before body read (`internal/center/http/handlers/agent.go`). Tests: `TestClientSyncReturnsDecodedPointer`, `TestAgentSyncHandlerRejectsMalformedHeaderTokenBeforeBodyRead`, and `TestAgentSyncHandlerLimitsInflightRequestsBeforeBodyRead`. Missing header is still allowed so legacy deployed agents that only send JSON `sync_token` continue to sync; strict missing-header pre-body rejection requires a future protocol migration. |
| P2-01 | Enrollment/sync token hashes use plain SHA-256 | fixed | `internal/center/store/agent_token_hash.go` adds versioned purpose-separated HMAC-SHA256 hashes derived from `cfg.SessionHMACKey`; production bootstrap wires that key into monitoring instance and sync repositories. Legacy SHA-256 hashes still verify and migrate after successful enrollment/sync/heartbeat. Tests: `agent_token_hash_test.go`, monitoring instance token tests, sync batch migration tests, and enrollment service delegation tests. |
| P2-02 | Diagnostic command output can leak secrets | fixed | `internal/security/redact` redacts common credential shapes. Agent `exec.Run` redacts stdout/stderr before returning command results, and center `marshalCompletedLastAction` redacts again before persistence. Tests cover redact patterns, agent stdout/stderr redaction, and persisted `last_action` redaction. Broader high-sensitive-command default-off, UI confirmation, audit, and TTL remain scoped out by this task PRD. |
| P2-03 | Installer verifies checksum but not checksum authority | fixed | Installer now pins a minisign public key, requires `minisign`, downloads `sha256sums.txt.minisig`, verifies the manifest signature, then checks binary hash. `.github/workflows/publish-images.yml` installs minisign, signs `dist/sha256sums.txt`, and uploads the detached signature. Tests: `TestScriptRequiresSignedChecksumManifest`. Docs now describe signing secrets and production version/digest pinning. |
| P2-04 / P2/P3-04 | Host / Origin / reverse proxy fail-open deployment boundary | fixed | Config rejects `0.0.0.0/0` and `::/0` trusted proxy ranges. `RequireAllowedHost` rejects Host mismatches when `HOUFENG_PUBLIC_BASE_URL` is configured and leaves local empty-base-url development unchanged. Bootstrap wraps the app handler. Tests: `TestLoadCenterConfigRejectsOverbroadTrustedProxyCIDR` and `TestRequireAllowedHost*`. Docs cover public base URL, trusted proxies, and proxy body/rate/connection limits. |
| P3-01 | `writeJSON` leaks encoder details to clients | fixed | `internal/center/http/handlers/json.go` now logs encoder details with `slog.Error("encode json", "error", err)` and returns only `internal server error`. Test: `TestWriteJSONEncodeFailureDoesNotExposeEncoderDetail`. |
| P3-02 | SPA symlink boundary hardening | already-fixed | Existing SPA handler and `TestSPAHandlerDoesNotServeSymlinkEscapingWebDist` continue to cover the symlink escape scenario. This task did not rework the handler. |

## Final audit notes

- Source report `/home/murray/.codex/attachments/0321052c-d788-43c2-9a35-e5734f908f6e/pasted-text-1.txt` was re-read during the finish pass.
- Literal report parity caveat: P1-02 says missing token should be rejected before body parsing. This branch implements that only for upgraded/header-capable agents and malformed optional headers; missing header remains JSON-compatible by design.
- Scoped diagnostics caveat: output redaction is implemented at both agent and center persistence boundaries. Full command sensitivity tiers, UI second confirmation, command-result TTL, and audit subsystem are larger follow-up work explicitly excluded by the PRD.
- Verification evidence belongs in the final session report; rerun `make verify-go` and `git diff --check` after any further changes before claiming completion.
