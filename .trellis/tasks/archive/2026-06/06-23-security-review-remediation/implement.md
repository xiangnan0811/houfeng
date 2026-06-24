# Security review remediation implementation plan

## Evidence matrix

- [x] Create `evidence.md` with one row per `HF-SEC-001` through `HF-SEC-023`.
- [x] For each row, cite current file paths / tests proving confirmed issue, already-fixed behavior, false positive, or deferred hardening.
- [x] Update the matrix after each implementation batch.

## Batch 1: HTTP and auth baseline

- [x] Add failing tests for secure `__Host-` cookie set / clear behavior.
- [x] Add failing tests for limited JSON decoding: oversized body, trailing JSON value, unknown field preservation.
- [x] Add failing tests for trusted proxy parsing and login handler not trusting forged XFF from direct clients.
- [x] Add failing tests for login rate limiting by username/IP/global failure bucket.
- [x] Add failing tests for admin same-origin middleware: cross-site unsafe method rejected, same-origin accepted, agent routes excluded.
- [x] Add failing tests for security headers and app server timeout defaults.
- [x] Add failing tests proving protected router routes fail closed without explicit no-auth test helper.
- [x] Implement the minimum code to pass these tests.
- [x] Run targeted Go tests for `internal/center/auth`, `internal/center/http`, `internal/center/http/handlers`, and `internal/center/app`.

## Batch 2: Session and settings hardening

- [x] Add failing tests that password change invalidates another active session and old sessions fail based on `password_changed_at`.
- [x] Add failing store / service tests for session deletion by user with current-session exception.
- [x] Add migration and repository tests for session ID hash-at-rest behavior.
- [x] Add failing tests that `GET /api/settings` and update responses do not expose full Feishu webhook URL and return a masked/present summary.
- [x] Add config / seed tests for `HOUFENG_INITIAL_PASSWORD_FILE` precedence and non-logging behavior.
- [x] Implement the minimum code to pass these tests.
- [x] Run targeted Go tests for `internal/center/auth`, `internal/center/store`, `internal/center/config`, `cmd/houfeng-center`, and settings handlers.

## Batch 3: Agent API hardening

- [x] Add failing tests for 32-byte secret token generator and update enrollment / sync token call sites.
- [x] Add failing tests for constant-time sync token hash comparison.
- [x] Add failing tests for enrollment one-time / TTL semantics if missing from current code.
- [x] Add failing tests for agent enroll / sync rate limiting.
- [x] Add failing tests for agent sync body size and per-batch count / string length limits.
- [x] Add failing tests for sync replay / duplicate `sync_batch_id` behavior.
- [x] Implement the minimum code to pass these tests.
- [x] Run targeted Go tests for `internal/center/ids`, `internal/center/enrollment`, `internal/center/store`, `internal/center/http/handlers`, and `agent` affected packages.

## Batch 4: Command, installer, and P2 hardening

- [x] Add failing tests for center-side command ID whitelist validation.
- [x] Add failing installer tests for default HTTPS enforcement plus `--insecure-allow-http`, `--enrollment-token-file`, and stdin token support.
- [x] Add failing SPA path hardening tests for encoded traversal and symlink/root checks.
- [x] Add failing tests for agent exec context parent cancellation.
- [x] Evaluate RawJSON depth hardening; sync queue disk byte limit is fixed with tests.
- [x] Implement the minimum code to pass these tests.
- [x] Run targeted tests for handlers, installer, SPA handler, agent exec/runtime/syncqueue as applicable.

## Batch 5: Reopened P2 security hardening

- [x] Add failing tests for configurable bcrypt cost parsing, invalid bounds, password-change hashes, initial-user seed hashes, and bootstrap wiring.
- [x] Implement `HOUFENG_PASSWORD_BCRYPT_COST` and route it through `config.CenterConfig`, `auth.Options`, `SeedInitialUserOptions`, and bootstrap defaults.
- [x] Add failing tests for production PostgreSQL TLS enforcement when `HOUFENG_DATABASE_REQUIRE_TLS=true`.
- [x] Implement config validation that accepts `sslmode=require`, `verify-ca`, and `verify-full`, while local/Compose can keep `sslmode=disable` with the guard off.
- [x] Add failing Docker static tests for non-root runtime image, no `gosu` entrypoint privilege drop, and named log volume.
- [x] Update `Dockerfile`, `scripts/docker-entrypoint.sh`, `compose.yaml`, deployment docs, and backend code-specs for rootless/default non-root runtime.
- [x] Run focused tests for auth/config/bootstrap/Docker deployment static checks and `docker compose --env-file docs/deploy/compose.env.example -f compose.yaml config --quiet`.

## Full verification and finish

- [x] Run `make verify-go`.
- [x] Run web verification or targeted web tests if any response contract visible to web changed.
- [x] Re-read the pasted report and audit every requirement against code/test evidence.
- [x] Update `.trellis/spec/` only for new durable contracts learned during remediation.
- [x] Record final state in the task artifacts and journal.
- [x] Re-run final verification after reopened P2 hardening.
