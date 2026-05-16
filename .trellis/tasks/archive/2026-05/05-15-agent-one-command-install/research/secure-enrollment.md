# Research: Secure enrollment/token patterns for one-command fleet agent installs

- **Query**: Research secure enrollment/token patterns for fleet agents installed via copied one-line commands. Compare 2-4 patterns from OSS/commercial agents where possible. Focus on short-lived one-time tokens, claim codes, TLS/server URL handling, secret leakage risks in shell history/process lists, revocation/expiry, and local token storage permissions.
- **Scope**: mixed
- **Date**: 2026-05-15

## Findings

### Files Found

| File Path | Description |
|---|---|
| `internal/contracts/agentapi/types.go` | Agent enrollment/sync request-response contract; enrollment request carries `token` + `fingerprint`, enrollment response can return `sync_token`. |
| `internal/center/enrollment/service.go` | Center enrollment service; validates enrollment token, binds fingerprint, and issues a sync token only when binding status is bound. |
| `internal/center/store/nodes.go` | Postgres-backed node repository; stores hashes for enrollment and sync tokens; implements binding-state transitions and rebind-pending behavior. |
| `internal/center/http/handlers/agent.go` | Public unauthenticated agent endpoints `/api/agent/enroll` and `/api/agent/sync`; maps invalid enrollment/sync tokens to stable agent API error codes. |
| `internal/center/http/handlers/node_onboarding.go` | Authenticated node onboarding endpoints; includes issuance of enrollment tokens and binding reset/confirm/reject operations. |
| `agent/config/config.go` | Agent environment loading; requires `HOUFENG_AGENT_SERVER_URL` and `HOUFENG_AGENT_TOKEN_FILE`. |
| `agent/token/file.go` | Agent token source; reads and trims the token file content. |
| `agent/enroll/client.go` | Agent HTTP client; posts JSON to center using the configured base URL. |
| `agent/runtime/runtime.go` | Agent runtime; reads token once at startup, sends enrollment request, stores returned sync token in memory for the sync loop, and logs `server_url` but not tokens. |
| `db/migrations/0001_initial_schema.sql` | Initial `nodes` schema includes `enrollment_token_hash` and `binding_fingerprint`. |
| `db/migrations/0003_add_sync_token_hash.sql` | Adds `sync_token_hash` column. |
| `db/migrations/0004_add_node_onboarding_binding_state.sql` | Adds enrollment issue timestamp and pending-fingerprint columns. |
| `docs/deploy/local-and-systemd.md` | Canonical local/systemd deployment guide; documents agent env, token file, and `0600` token-file install command. |
| `.trellis/spec/backend/directory-structure.md` | Backend/agent topology and package boundaries; confirms thin systemd agent and `internal/contracts/agentapi` contract package. |
| `.trellis/spec/backend/error-handling.md` | Agent API error-code contract and handling expectations for invalid enrollment/sync/binding errors. |
| `.trellis/spec/backend/logging-guidelines.md` | Logging rules; explicitly forbids logging sync tokens, passwords, full enrollment tokens, cookies, auth headers, and notification secrets. |

### Code Patterns

#### Current Houfeng enrollment flow

- Contract: `agentapi.EnrollmentRequest` carries only token + host fingerprint; `EnrollmentResponse` may include `sync_token` (`internal/contracts/agentapi/types.go:50-60`).
- Token issuance: `IssueNodeEnrollmentToken` generates an opaque `enroll` token, stores only its SHA-256 hash in `nodes.enrollment_token_hash`, and records `enrollment_token_issued_at` (`internal/center/store/nodes.go:461-486`).
- Enrollment validation and binding:
  - `ApplyEnrollment` selects the node by `enrollment_token_hash` under `for update` and updates binding state (`internal/center/store/nodes.go:1135-1221`).
  - First bind moves `未绑定` to `已绑定` and stores the submitted fingerprint (`internal/center/store/nodes.go:1087-1095`).
  - Existing bound node with a different fingerprint enters `指纹变更待确认` and records pending fingerprint metadata (`internal/center/store/nodes.go:1096-1109`).
- Sync-token exchange: after successful bound enrollment, the service issues a distinct `sync` token (`internal/center/enrollment/service.go:44-66`); repository stores only `sync_token_hash` (`internal/center/store/nodes.go:1040-1061`).
- Agent startup: runtime reads the local token file, reads host fingerprint, posts enrollment, then uses the returned in-memory sync token for subsequent sync batches (`agent/runtime/runtime.go:145-181`).
- Server URL handling: agent requires `HOUFENG_AGENT_SERVER_URL` from env (`agent/config/config.go:25-33`), trims trailing slash in the HTTP client (`agent/enroll/client.go:38-44`), and logs `server_url` at start/stop (`agent/runtime/runtime.go:142-143`). No custom TLS pin/CA setting appears in current agent config.
- Local token storage: deployment guide writes the enrollment token to `/etc/houfeng-agent/token`, sets owner `houfeng-agent:houfeng-agent`, and mode `0600` (`docs/deploy/local-and-systemd.md:123-145`). The token reader currently reads and trims the file but does not inspect ownership/mode (`agent/token/file.go:14-25`).
- Expiry/revocation: schema records `enrollment_token_issued_at` (`db/migrations/0004_add_node_onboarding_binding_state.sql:1-6`), but current lookup uses only `enrollment_token_hash` and has no expiry predicate (`internal/center/store/nodes.go:1154-1167`). Issuing a new enrollment token overwrites the prior hash (`internal/center/store/nodes.go:468-477`); reset/confirm paths clear `sync_token_hash` in some binding transitions (`internal/center/store/nodes.go:583-638`, `internal/center/store/nodes.go:694-740`).

#### Pattern comparison from OSS/commercial agents

| Product / pattern | Initial secret shape | One-time / expiry | Server URL / TLS handling | Revocation behavior | Local secret handling notes | Shell/process leakage surface |
|---|---|---|---|---|---|---|
| **Teleport Secure Token** | Short-lived join/invite token passed to `teleport start`/`teleport configure`. | `tctl tokens add --ttl=5m --type=node`; docs say Teleport processes can use a token multiple times until TTL expires, except bot tokens; default TTL for ephemeral tokens is 30 minutes. | Join command includes `--auth-server` or `--proxy`; docs include `--ca-pin=sha256:...` and note CA pin becomes invalid on CA rotation. | `tctl tokens ls` lists outstanding tokens; `tctl tokens rm` deletes/revokes token. Static long-lived tokens are documented as insecure and discouraged. | Teleport exchange results in certificates; after initial join, valid certs are used for later connections. Static tokens may also be stored in config/file, but docs prefer short-lived tokens. | Docs examples put `--token=...` on command line, so the token can appear in shell history and process command line during execution. |
| **Tailscale auth keys** | Auth key supplied to `tailscale up --auth-key=...`. | Supports one-off keys for one-time use and reusable keys for multiple devices; key expiry configurable 1-90 days; one-off keys are automatically revoked after use. | Tailscale SaaS/control-plane URL is implicit in standard install/up flow; no copied center URL/pin in the auth-key example. | Admin console can revoke keys; revoking a key does not deauthorize nodes already using it, so machine deletion is separate. | Auth key is bootstrap credential; device/node keys are separate and have their own expiry behavior. | Example passes the auth key as a CLI flag, exposing it to shell history and possibly process command-line inspection while running. |
| **Netdata Cloud claim token** | Space-level claiming token plus room keys; used in generated install command, `claim.conf`, or env vars. | Claiming token is reusable for connecting multiple agents to a Space; docs describe regenerating the token to invalidate the previous one. No one-time semantics found in docs reviewed. | Claim config includes `url = https://app.netdata.cloud`; `insecure = yes/no` controls host verification; optional custom CA files may live under `cloud.d`. | Cloud UI can regenerate the claiming token; reconnect/unclaim workflows remove `/var/lib/netdata/cloud.d/` and reclaim with a new token. | Docs explicitly say `claim.conf` contains sensitive claiming tokens and require `0640` with owner `root:netdata`; ACLK stores `private.pem`, `public.pem`, `cloud.conf`, and `claimed_id` under `cloud.d`. | Quick reclaim/install commands pass `--claim-token` on the shell command line; env-var mode (`NETDATA_CLAIM_TOKEN`) is documented for containers/CI and can also leak through shell history or process environments depending runtime. |
| **Elastic Fleet enrollment token** | Fleet enrollment token / enrollment API key passed to `elastic-agent enroll` or `elastic-agent install` as `--enrollment-token`. | Docs say one token can enroll one or more Elastic Agents, can be used as many times as needed, and remains valid until revoked. No one-time/short-lived default in docs reviewed. | Agent command requires `--url`; supports `--ca-sha256`, `--certificate-authorities`, and `--insecure`; Fleet Server later passes minimal communication API keys and output TLS/auth data. | Revoking enrollment token invalidates the API key used to enroll agents; currently enrolled agents continue functioning. | Enrollment token is initial bootstrap credential; Fleet Server returns a separate communication API key with minimal Fleet permissions and output credentials as needed. | Docs expose `--enrollment-token <string>` in CLI synopsis, so copied one-line install commands containing the token have shell-history and process-list exposure risk. |

#### Secret leakage facts relevant to copied one-line commands

- Linux `/proc/<pid>/cmdline` exposes the complete command line for a running process, including command-line arguments (`proc_pid_cmdline(5)`). Tokens embedded as CLI flags can therefore be visible at least while the process runs.
- Bash history stores commands typed by the user in a history list and initializes/writes a history file named by `HISTFILE`, defaulting to `~/.bash_history`. Tokens in copied commands can therefore persist after install unless history is disabled/filtered/cleared by the operator.
- Houfeng logging spec already treats token disclosure as sensitive: logs must not contain `sync_token`, full enrollment token, password, cookies, Authorization headers, Telegram bot token/chat id, or Feishu webhook URL (`.trellis/spec/backend/logging-guidelines.md:97-107`).

### External References

- [Teleport: Join Services with a Secure Token](https://goteleport.com/docs/installation/agents/join-token/) — Short-lived join-token workflow; example `tctl tokens add --ttl=5m --type=node`; join command includes `--token`, `--ca-pin`, and `--auth-server`; tokens can be listed and revoked.
- [Teleport: Join Methods and Tokens](https://goteleport.com/docs/reference/deployment/join-methods/) — Classifies secret-based vs delegated join methods; discourages static tokens; describes ephemeral tokens, default TTL, static-token risk, and cloud/Kubernetes/TPM delegated alternatives.
- [Tailscale: Auth keys](https://tailscale.com/kb/1085/auth-keys) — Defines one-off vs reusable auth keys, 1-90 day key expiry, automatic revocation of one-off keys after use, and key revocation semantics.
- [Tailscale: Quickstart](https://tailscale.com/kb/1017/install) — Standard server/device add flow references auth keys as an unattended provisioning method.
- [Netdata: Connect Agent to Cloud](https://learn.netdata.cloud/docs/netdata-cloud/connect-agent) — Generated connect command and `claim.conf`/env-var claim-token modes; explicit `claim.conf` permission/ownership requirements; Cloud URL and `insecure` host-verification switch.
- [Netdata: Unclaiming and Reclaiming a Node](https://learn.netdata.cloud/docs/netdata-cloud/unclaim-and-reclaim-a-node) — Reclaim flow with `--claim-token`; unclaim requires removing `cloud.d/` and restarting; new token/room workflow.
- [Netdata: Secure Your Netdata Agent with Bearer Token Protection](https://learn.netdata.cloud/docs/netdata-agent/configuration/securing-agents/bearer-token-protection) — Time-limited bearer tokens for direct agent dashboard access; also notes combining with TLS/IP restrictions.
- [Elastic: Fleet enrollment tokens](https://www.elastic.co/docs/reference/fleet/fleet-enrollment-tokens) — Enrollment token is an API key; can enroll one or more agents; remains valid until revoked; after initial enrollment Fleet Server provides minimal communication/output credentials.
- [Elastic: Elastic Agent command reference](https://www.elastic.co/docs/reference/fleet/agent-command-reference) — `elastic-agent enroll --url <string> --enrollment-token <string>` and TLS-related flags including `--ca-sha256`, `--certificate-authorities`, and `--insecure`.
- [Linux man-pages: `/proc/pid/cmdline`](https://man7.org/linux/man-pages/man5/proc_pid_cmdline.5.html) — Documents that `/proc/<pid>/cmdline` exposes the complete command line of a running process.
- [GNU Bash manual: Bash History Facilities](https://www.gnu.org/software/bash/manual/html_node/Bash-History-Facilities.html) — Documents command history and the default `~/.bash_history` file behavior.

### Related Specs

- `.trellis/spec/backend/directory-structure.md` — Backend topology and package boundaries; identifies `agent/`, `internal/center/enrollment`, and `internal/contracts/agentapi` as the intended code locations for this feature area.
- `.trellis/spec/backend/error-handling.md` — Agent API error-code contract; relevant for invalid enrollment token, invalid sync token, and binding-not-accepted paths.
- `.trellis/spec/backend/logging-guidelines.md` — Sensitive logging contract; directly relevant because one-command install must avoid leaking enrollment/sync tokens in logs.
- `docs/deploy/local-and-systemd.md` — Current deployment recipe for agent env, token file, TLS/reverse proxy note, and local token-file mode/ownership.

## Caveats / Not Found

- No current Houfeng code path found for enrollment-token TTL enforcement, one-time consume semantics, or automatic expiry/revocation based on `enrollment_token_issued_at`; current token lookup matches only the stored hash.
- No current Houfeng agent config found for explicit CA pinning/custom CA bundle/insecure TLS mode; it relies on Go `net/http` defaults through the URL configured in `HOUFENG_AGENT_SERVER_URL`.
- The current agent token reader does not verify token-file mode or owner; file permissions are documented in deployment commands rather than enforced in `agent/token/file.go`.
- Teleport, Tailscale, Netdata, and Elastic examples often include bootstrap secrets directly in CLI flags. That is operationally convenient for copied commands but creates shell-history and process-command-line exposure surfaces.
- Datadog docs were searched as a commercial contrast, but the accessible install/getting-started pages did not provide as clear a one-time/claim-code enrollment model as the four patterns above; therefore Datadog is not included in the main comparison table.
