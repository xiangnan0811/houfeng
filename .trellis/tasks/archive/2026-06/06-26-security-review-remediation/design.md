# Security review remediation design

## Verified Findings

The report maps to real code paths in the current branch:

- `handlers/auth.go`: `loginLimiter.byUser` and `loginLimiter.byIP` grow by unbounded key count.
- `handlers/agent.go`: `agentRequestLimiter.byIP` grows by unbounded key count, and sync decodes a 4 MiB body before cheap token rejection.
- `store/monitoring_instances.go` and `enrollment/service.go`: enrollment/sync token hashes are plain SHA-256.
- `agent/exec/runner.go` and `store/command_actions.go` / `sync_batches.go`: command stdout/stderr are truncated but not redacted.
- `config/config.go`: trusted proxy CIDRs are parsed but overly broad CIDRs are accepted.
- `middleware.go`: same-origin checks exist, but there is no Host allowlist middleware bound to `HOUFENG_PUBLIC_BASE_URL`.
- `handlers/json.go`: encode errors are returned to the client with detail.
- `installer/houfeng-agent-install.sh` and `.github/workflows/publish-images.yml`: release assets have checksums but no signed checksum manifest.

The SPA symlink finding is already addressed in `handlers/spa.go` and covered by `TestSPAHandlerDoesNotServeSymlinkEscapingWebDist`; no production change is planned there.

## Architecture

Use small, local hardening primitives instead of broad subsystem rewrites:

- Add bounded in-memory limiter state to the existing handler package. The limiter keeps the current sliding-window semantics, adds max key caps, prunes expired keys during access, and periodically sweeps all tracked keys. When max key count is reached, unknown keys fall back to the global limiter instead of allocating more memory.
- Add a per-handler inflight gate for agent sync. This sits before body decode and returns a contract-level 503 agent error when saturated.
- Add header-compatible cheap sync token rejection without breaking existing JSON clients. The server accepts an optional `Authorization: Bearer <token>` / `X-Houfeng-Agent-Token` for early shape checks, but current JSON `sync_token` stays supported while the agent client starts sending the header for future cheap rejection.
- Move token hashing responsibility into store helpers that can produce and verify versioned token hashes. New hashes use HMAC-SHA256 derived from `HOUFENG_SESSION_HMAC_KEY` with purpose labels; legacy 64-hex SHA-256 hashes remain verifiable only for migration and are rewritten to HMAC after successful enrollment/sync validation.
- Redact command outputs both in the agent runner and center persistence. Use a shared internal package for string redaction so agent and center use the same rules without importing app-specific packages across boundaries.
- Add Host allowlist as outer HTTP middleware when `PublicBaseURL` is configured. This makes production deployments fail closed while leaving local development with empty `HOUFENG_PUBLIC_BASE_URL` unchanged.
- Sign `sha256sums.txt` with minisign. The installer pins a public key and requires `minisign -Vm sha256sums.txt -P <key> -x sha256sums.txt.minisig` before checksum comparison. The release workflow signs and uploads `sha256sums.txt.minisig`.

## Compatibility

- Existing agent JSON sync bodies remain accepted. Adding header token support is additive; removing `sync_token` from JSON is not part of this task.
- Existing DB rows with plain SHA-256 token hashes keep working. On successful use, repositories update the stored hash to the new HMAC form. There is no migration that can convert hashes offline because plaintext tokens are not available.
- Reusing `HOUFENG_SESSION_HMAC_KEY` as key material avoids a new startup-required secret in this task. Purpose-separated HMAC labels avoid using the exact same MAC input namespace for browser sessions and agent tokens.
- Host allowlist is only active when `HOUFENG_PUBLIC_BASE_URL` is non-empty. Existing local smoke instructions that use empty public base URL for first login stay usable.

## Security Boundaries

- Trusted proxy config must reject `0.0.0.0/0` and `::/0`; non-trusted clients' forwarded headers remain ignored by the existing resolver.
- Limiter caps protect process memory; they are not a substitute for reverse-proxy rate limits. Docs must continue recommending reverse proxy body/rate/connection limits.
- Command redaction is best effort. It must cover common names and formats: `Authorization: Bearer`, `token=`, `access_token=`, `refresh_token=`, `password=`, `secret=`, `api_key=`, JSON-style sensitive keys, and private-key PEM blocks.
- Installer signature verification protects against checksum-only replacement. It does not protect a compromised signing key; docs must describe release workflow protection and production version pinning.

## Rollback

- Limiter/inflight changes are in-memory only and can be reverted without data migration.
- HMAC token migration is backward compatible while legacy verifier remains. If rollback is needed, HMAC-prefixed rows would not be accepted by old binaries, so deployment rollback after successful agent syncs requires either retaining the new binary or reissuing enrollment tokens. This is the main operational risk and should be called out in final notes.
- Installer signing changes affect future installs. Existing installed agents are unaffected.

