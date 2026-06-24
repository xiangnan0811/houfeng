# Security review remediation design

## Scope and approach

The pasted report is the requirement source, but each item is verified against the current repository before implementation. The work is grouped by shared enforcement boundary rather than by report order:

1. HTTP and auth baseline: cookie policy, request decoding, client IP parsing, rate limiting, CSRF / origin enforcement, security headers, server timeouts, router fail-closed.
2. Session and credential storage: password-change revocation, session expiry checks against `password_changed_at`, session ID hashing at rest, initial password file support.
3. Agent public API hardening: secret-token entropy, enrollment one-time semantics, public endpoint rate limits, sync token comparison, sync replay / duplicate handling, body and batch limits.
4. Sensitive settings and command safety: Feishu webhook write-only response, center-side command ID validation, installer HTTPS and token-file / stdin support.
5. P2 hardening: SPA path root validation, JSON raw/depth checks, agent exec context inheritance, sync queue disk bound where low-risk.

## HTTP boundary

`internal/center/http` becomes the enforcement layer for cross-cutting HTTP concerns. Router construction remains explicit, but protected routes no longer silently run without auth. Tests that intentionally bypass auth must call an explicit test-only helper such as `NoAuthForTestOnly`.

Request JSON decoding stays in `internal/center/http/handlers/json.go`, but moves from raw `json.Decoder` use to a limited decoder with:

- `http.MaxBytesReader`
- `DisallowUnknownFields`
- single JSON value enforcement
- named size limits for auth, settings / admin CRUD, agent enroll, and agent sync

CSRF protection is middleware for cookie-authenticated admin routes only. It rejects unsafe methods when `Origin` is cross-site or when both `Origin` and `Referer` are missing. `/api/auth/login`, `/api/agent/*`, `/api/healthz`, the installer script, and static SPA serving stay outside this admin-cookie CSRF policy.

## Auth and session boundary

Cookie issuance uses a `__Host-` session cookie name with `Secure`, `HttpOnly`, `Path=/`, and `SameSite=Strict`. The same flags are used for clearing. If a local HTTP development escape hatch is needed, it must be an explicit config and test case rather than silent production behavior.

Login rate limiting lives at the handler/middleware edge so it can combine username, trusted client IP, and global failure buckets before bcrypt can be abused. Client IP parsing must only trust forwarded headers when `RemoteAddr` belongs to configured trusted proxy CIDRs.

Session validation checks both the session row and the owning user. A session issued before `users.password_changed_at` is invalid. Password change deletes other sessions; keeping the current session requires the handler to pass the current session ID to the service. If deletion fails, the `password_changed_at` guard still invalidates older sessions.

## Persistence boundary

Schema changes are append-only migrations. Session storage changes introduce a session hash column and update repository methods so database lookup / refresh / delete operate on the hash. The cookie still contains the opaque random session ID. Hashing uses HMAC-SHA256 with a center secret loaded from config; a compatibility migration path must keep existing sessions either invalidated or queryable by a deliberate compatibility branch.

Agent replay protection uses persistent sync batch identity rather than assuming ordering. The center records processed `(monitoring_instance_id, sync_batch_id)` identities, rejects duplicates as replay or treats them idempotently without rewriting facts, and keeps the transaction boundary around validation plus writes.

## Agent and installer boundary

`internal/center/ids` continues to generate short non-secret IDs. New secret-bearing tokens use a separate 32-byte generator, and call sites for enrollment / sync / installer tokens must use it. Token hashes are compared with `crypto/subtle.ConstantTimeCompare`.

The center validates `command_id` against the same stable catalog as the agent whitelist, through a shared contract-level list rather than importing agent implementation internals. The agent still enforces its local whitelist before execution.

Installer behavior defaults to HTTPS server URLs. HTTP requires explicit `--insecure-allow-http`. Enrollment tokens can be supplied by `--enrollment-token-file` or stdin to avoid process-list and shell-history exposure.

## Verification

Every behavior change is test-first. The minimum final gate is `make verify-go`; web verification is required when response contracts or frontend-visible settings change. The final audit must re-read the pasted report and map every item to current evidence.
