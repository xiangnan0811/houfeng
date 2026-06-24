# Security review remediation

## Goal

Verify the pasted integrated security review against the current Houfeng codebase and remediate every confirmed issue through tests, implementation, and final evidence.

## Requirements

- Source report: `/home/murray/.codex/attachments/54b2a069-032c-4bd2-8b21-7b0e26febf1b/pasted-text-1.txt`.
- Treat external review items as claims to verify, not assumptions. Record false positives, downgraded items, and already-fixed items with code evidence.
- Complete all confirmed P0 items before declaring the task ready for production exposure:
  - Secure, host-scoped session cookie policy with matching clear behavior.
  - Login rate limiting and trusted proxy client IP parsing; direct requests must not trust forged `X-Forwarded-For`.
  - Unified JSON body limits, single-value JSON enforcement, and route-specific / agent batch limits.
  - Feishu webhook read APIs must be write-only / masked and must not leak the full URL.
  - Password change must invalidate other sessions and old sessions must fail based on `password_changed_at`.
  - Cookie-authenticated unsafe admin APIs must enforce same-origin / CSRF protection without weakening CORS.
  - HTTP server must set production-safe timeouts and security response headers.
  - Router protected routes must fail closed unless tests explicitly opt into no-auth behavior.
  - Agent enrollment / sync secrets and public APIs must be hardened: secret-token entropy, public endpoint rate limits, one-time enrollment token behavior, and sync input limits.
- Complete confirmed P1 items unless code evidence shows they are false positives or intentionally deferred with a recorded rationale:
  - Session IDs hashed at rest.
  - Center-side `command_id` whitelist validation.
  - Constant-time sync token hash comparison.
  - Agent sync replay protection or idempotent duplicate-batch handling.
  - Installer HTTPS default and safer token input options.
  - `HOUFENG_INITIAL_PASSWORD_FILE` support.
  - Password policy improvement aligned with modern passphrase guidance.
- Address P2 items as hardening where low-risk and testable in this branch; otherwise record evidence and rationale:
  - SPA path root hardening.
  - Raw JSON depth / frontend rendering safety checks.
  - Agent exec context inheritance.
  - Agent sync queue disk bound.
- Preserve existing project contracts:
  - Agent remains a thin process with compiled command whitelist and no arbitrary shell execution.
  - Agent / center DTO changes must be made through `internal/contracts/agentapi`.
  - Existing migrations are append-only; new schema changes use the next migration number.
  - Tests must not require a live PostgreSQL instance unless explicitly documented as smoke coverage.

## Acceptance Criteria

- [x] Evidence matrix exists in task artifacts and maps every report item `HF-SEC-001` through `HF-SEC-023` to current status: confirmed-fixed, false-positive / downgraded, deferred hardening, or blocked with reason.
- [x] Every confirmed P0 has automated regression tests and passing implementation.
- [x] Every confirmed P1 has automated regression tests and passing implementation, or a documented evidence-backed defer decision.
- [x] Sensitive values are not returned by read APIs or logged: session IDs at rest, passwords, cookies, sync/enrollment tokens, Telegram secrets, Feishu webhook URLs, fixer API keys.
- [x] Public Agent endpoints reject oversized bodies, malformed/trailing JSON, invalid batch sizes, replay/duplicate batches, invalid tokens, and fingerprint mismatches without writing facts.
- [x] Cookie-authenticated admin unsafe methods reject cross-site requests and accept same-origin SPA requests.
- [x] `make verify-go` passes; if web/API contracts change frontend-visible settings or auth behavior, the relevant web tests or full web verification are run.
- [x] Final completion audit re-reads the source report and verifies every explicit requirement against code, tests, command output, or documented rationale.

## Notes

- Branch governance: all changes stay on non-main branch `security-review-remediation`.
- Local hooks were enabled with `sh scripts/setup-git-hooks.sh`.
- This is a complex, cross-layer security task; keep `design.md` and `implement.md` current as findings are verified.
