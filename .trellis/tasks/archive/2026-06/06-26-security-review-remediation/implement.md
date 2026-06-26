# Security review remediation implementation plan

## Preconditions

- Branch: `fix/security-review-remediation`.
- Hooks enabled with `sh scripts/setup-git-hooks.sh`.
- Required specs read: `.trellis/spec/backend/error-handling.md`, `.trellis/spec/backend/quality-guidelines.md`, `.trellis/spec/guides/branch-workflow-governance.md`, plus shared thinking guide index.

## Execution Order

### 1. Bounded Rate Limiters

- Add tests in `internal/center/http/handlers/auth_test.go`:
  - many failed usernames with `MaxKeys` set low do not grow `byUser` beyond cap;
  - expired keys are swept after the window.
- Add tests in `internal/center/http/handlers/agent_test.go`:
  - many trusted forwarded IPs with `MaxKeys` set low do not grow `byIP` beyond cap;
  - global request limit rejects when aggregate budget is exhausted.
- Implement `MaxKeys`, `MaxRequestsGlobal`, `SweepInterval` defaults in `LoginRateLimitOptions` and `AgentRateLimitOptions`.
- Keep existing rate-limit behavior for normal same-key requests.

### 2. Agent Sync Pre-Body Guard And Inflight Limit

- Add tests in `agent_test.go`:
  - current agent JSON sync remains accepted;
  - agent client sends `Authorization: Bearer <sync_token>`;
  - requests with malformed header token are rejected before body decode;
  - saturated sync inflight limit returns 503 with an agent error response and does not call the service.
- Implement an `InflightLimit` option for sync endpoint and a default.
- Add optional header token support in the agent client and handler, but keep JSON `sync_token` required for compatibility until a future protocol migration.
- Use `writeAgentAPIError` and stable `agentapi` codes; add a new code only if needed for rate limiting / unavailable.

### 3. Agent Token HMAC Storage With Legacy Migration

- Add token hash helper tests in `internal/center/store/monitoring_instances_test.go` or a focused new test file:
  - new enrollment/sync token hashes are prefixed HMAC values and differ from plain SHA-256;
  - legacy SHA-256 hashes verify;
  - after a legacy enrollment/sync succeeds, the SQL updates stored hash to the new HMAC hash.
- Add derived key material to repository constructors without adding a new env requirement. Use `cfg.SessionHMACKey` and purpose labels.
- Update `NewPostgresMonitoringInstanceRepository` and `NewPostgresSyncRepository` plus `bootstrap.go` construction.
- Update tests that instantiate repositories directly to use helper constructors or default test key material.
- Remove or refactor duplicated plain SHA-256 hashing in `internal/center/enrollment/service.go` so there is one token verification policy.

### 4. Command Output Redaction

- Add a shared redaction helper under `internal/security/redact` or another neutral internal package.
- Add unit tests for:
  - key/value tokens;
  - JSON-like sensitive fields;
  - Authorization bearer headers;
  - private-key blocks;
  - non-sensitive text preservation.
- Add agent runner tests proving `Run` redacts stdout and stderr after truncation.
- Add store tests proving `marshalCompletedLastAction` / `storeCommandResults` redacts outputs before persisting.

### 5. Host And Trusted Proxy Hardening

- Add config tests rejecting `0.0.0.0/0` and `::/0`.
- Add middleware tests:
  - configured public base URL allows matching Host, including port;
  - mismatched Host returns 400 before inner handler;
  - empty public base URL leaves requests unchanged.
- Implement `RequireAllowedHost(publicBaseURL string)` and wrap the router outside security headers in `bootstrap.go`.

### 6. JSON Encode Error Contraction

- Add a `json_test.go` test using an unencodable value, asserting response body does not contain the encode detail.
- Change `writeJSON` to log encode details with `slog` and return generic JSON error text / `http.Error` without internal detail.

### 7. Installer Signature Verification

- Add installer embedded-script tests requiring:
  - `minisign` command check;
  - `sha256sums.txt.minisig` download;
  - signature verification before checksum extraction;
  - no checksum-only fallback.
- Update `.github/workflows/publish-images.yml`:
  - install minisign or use a deterministic signing action/container available on ubuntu;
  - sign `dist/sha256sums.txt`;
  - upload `dist/sha256sums.txt.minisig`.
- Add docs updates in `README.md` and `docs/deploy/local-and-systemd.md` for signed manifest verification and production version/digest pinning.

### 8. Verification And Re-Review

- Run targeted tests after each task:
  - `go test ./internal/center/http/handlers`
  - `go test ./internal/center/http`
  - `go test ./internal/center/config`
  - `go test ./internal/center/store`
  - `go test ./agent/exec`
  - `go test ./internal/center/installer`
- Run final `make verify-go`.
- Re-read the external report acceptance points and map each to evidence from code/tests.
- Run `git diff --check`.
- Check `git status --short --branch`.

## Review Gates

- Gate 1: before implementation, confirm this plan.
- Gate 2: after P1 fixes, run targeted handler tests before continuing to token/storage changes.
- Gate 3: after HMAC migration, run store and bootstrap tests before installer/workflow work.
- Gate 4: final report must explicitly classify every finding as fixed, already fixed, intentionally deferred with reason, or out of scope.
