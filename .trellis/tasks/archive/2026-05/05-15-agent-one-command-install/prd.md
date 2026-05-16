# Agent One-Command Install

## Goal

Make VPS agent onboarding operationally simple: after creating a VPS/Node in the center UI, the operator should be able to copy one command, run it on the target VPS, and get `houfeng-agent` installed, configured, enrolled, and started without cloning the repository or manually writing multiple environment files.

## What I already know

- The user has deprioritized current visual redesign work and wants to focus on functionality, especially agent deployment experience.
- The repository is now public on GitHub, so GitHub Release assets can be used as the default binary distribution channel.
- Accepted defaults:
  - Distribute `houfeng-agent` as prebuilt GitHub Release binaries, not by copying source code or compiling on each VPS.
  - Use short-lived, one-time enrollment tokens in generated install commands.
  - The installer manages install/start/status through systemd for MVP; upgrade and uninstall commands are deferred.
  - The install script must be served by each deployed center instance, not GitHub raw/release, so public open-source users' self-hosted centers remain isolated by their own center URL and token authority.
- Install commands must use an explicit `HOUFENG_PUBLIC_BASE_URL` configured on the center; both domain URLs and `http(s)://IP:port` URLs are acceptable to reduce DNS dependency when desired.
- Current deployment docs require manual steps for agent install: create user/directories, install binary, write `/etc/houfeng-agent/agent.env`, write `/etc/houfeng-agent/token`, install systemd unit, daemon-reload, enable/start service.
- Current agent config requires only two non-default env vars: `HOUFENG_AGENT_SERVER_URL` and `HOUFENG_AGENT_TOKEN_FILE`; buffer settings already have code defaults but are still documented in `.env.example` and deployment docs.
- Current Node onboarding UI already issues an enrollment token and shows manual snippets derived from `window.location.origin`, token, and fixed paths.
- Current center-side enrollment stores only hashed enrollment/sync tokens and binds by host fingerprint, but it does not enforce token TTL or one-time consumption yet.
- Current `houfeng-agent` reads an enrollment token file once at startup, enrolls, receives a sync token, and keeps that sync token only in memory for the running process.

## Research References

- [`research/installer-conventions.md`](research/installer-conventions.md) — One-line installers commonly normalize OS/arch, verify package/release integrity, write deterministic systemd/config paths, and keep upgrade/uninstall as separate lifecycle actions.
- [`research/secure-enrollment.md`](research/secure-enrollment.md) — Comparable agents use bootstrap secrets for initial join, but safest copied-command pattern is short-lived/one-time token plus separate post-enrollment identity, with careful handling of shell history/process-list leakage.

## Requirements

### Center / API / onboarding

- Center must support generating a copyable one-command installer command for a specific Node from the onboarding flow.
- Center must serve the installer script from the deployed center instance through a stable public read-only endpoint; the copied command must not fetch the script from GitHub raw/release.
- The command must include:
  - center public server URL from `HOUFENG_PUBLIC_BASE_URL`,
  - short-lived one-time enrollment token,
  - release/version source for `houfeng-agent`,
  - enough install options for the installer to write config and start systemd.
- `HOUFENG_PUBLIC_BASE_URL` should accept domain URLs or `http(s)://IP:port` URLs; it should be validated as an absolute HTTP(S) URL without a trailing slash in generated commands.
- If `HOUFENG_PUBLIC_BASE_URL` is missing, the onboarding page/API must not silently generate a guessed production command; it should surface a clear configuration error or limited preview.
- Token generation for installer commands must preserve existing hashed-token storage and must add fixed 30-minute TTL + one-time-use semantics.
- Regenerating an enrollment/install token should invalidate the previously generated enrollment token for that node.
- The onboarding page should replace or strongly de-emphasize the current multi-step manual snippets with a primary one-command copy surface.
- The UI must warn that the command contains a secret and should be treated as sensitive.
- The UI should continue to support regenerating the token/command when expired, hidden, or lost.

### Installer

- Provide a Linux installer script suitable for a copied command such as `curl -fsSL <center install script URL> | sudo sh -s -- ...` or an equivalent safer variant.
- MVP installer supports systemd-based Linux hosts only.
- MVP installer supports `linux/amd64` and `linux/arm64` release binaries.
- Installer must detect unsupported OS/arch/service-manager combinations and fail with a clear message before making partial destructive changes where practical.
- Installer must download the selected `houfeng-agent` release asset from GitHub Release, install it to `/usr/local/bin/houfeng-agent`, and make it executable.
- Installer must create or converge:
  - service user/group `houfeng-agent`,
  - `/etc/houfeng-agent/agent.env`,
  - `/etc/houfeng-agent/token`,
  - `/var/lib/houfeng-agent/`,
  - `/etc/systemd/system/houfeng-agent.service`.
- Installer must write secret-bearing files with restrictive permissions:
  - token file owned by `houfeng-agent:houfeng-agent`, mode `0600`,
  - env file owned by `root:houfeng-agent`, mode `0640`,
  - config/state directories no more permissive than current deployment docs.
- Installer must run `systemctl daemon-reload` and `systemctl enable --now houfeng-agent`.
- Installer output must not print the full enrollment token.
- Re-running the same command should be safe enough for common failed-install retry cases, while respecting one-time token behavior after successful enrollment.

### Agent/runtime compatibility

- Existing agent environment contract should remain compatible: `HOUFENG_AGENT_SERVER_URL`, `HOUFENG_AGENT_TOKEN_FILE`, and existing buffer defaults.
- The agent should not accept arbitrary scripts, user-supplied command args, remote shell text, or center-pushed installation logic.
- Agent enrollment/sync logs must not leak enrollment tokens or sync tokens.

### Release/build

- Add release build support for at least `houfeng-agent` Linux amd64/arm64 artifacts.
- Prefer deterministic asset naming that the installer can derive from `uname`, e.g. `houfeng-agent_<version>_linux_amd64` and `houfeng-agent_<version>_linux_arm64`, or a compressed equivalent.
- Installer must verify the downloaded `houfeng-agent` release artifact against a GitHub Release `sha256sums.txt` manifest before installing it.

## Acceptance Criteria

- [ ] From the Node onboarding page, an operator can generate/copy a one-command install command for that node.
- [ ] The command includes a 30-minute one-time enrollment token and a center server URL from `HOUFENG_PUBLIC_BASE_URL`.
- [ ] Running the command on a supported fresh Linux amd64/arm64 system downloads `houfeng-agent`, verifies it with `sha256sums.txt`, installs it, writes config/token, installs systemd service, enables and starts the service.
- [ ] The installed service uses the existing `houfeng-agent.service` security posture or a deliberately equivalent template.
- [ ] After successful install, the agent enrolls, binds the node, and syncs at least one heartbeat/host sample visible in center.
- [ ] A reused/expired enrollment token is rejected by center with a stable error and does not bind another node/host.
- [ ] Regenerating a command invalidates the prior enrollment token.
- [ ] Installer and center/agent logs do not print full enrollment or sync tokens.
- [ ] Unsupported OS/arch/non-systemd hosts fail clearly.
- [ ] Checksum mismatch fails before replacing or starting the local agent binary.
- [ ] Go tests cover token TTL/one-time behavior and affected onboarding API behavior.
- [ ] Web tests cover command generation/copy-state behavior without asserting visual redesign details.
- [ ] Docs are updated to make one-command install the primary agent deployment path while keeping manual deployment as fallback/troubleshooting.

## Definition of Done

- Tests added/updated where behavior changes.
- `make verify-go` passes.
- `cd web && npm run lint && npm run test -- --run && npm run build` passes for frontend changes.
- Installer shell script passes at least shell syntax/static sanity checks available in repo environment.
- Browser sanity check confirms the onboarding page exposes the intended copy command and regenerate flow.
- Rollback path is clear: manual agent deployment docs still work if the installer path fails.

## Technical Approach

Recommended MVP direction:

1. Add `HOUFENG_PUBLIC_BASE_URL` to center configuration and use it as the authoritative externally reachable center URL for generated install commands.
2. Add an install command abstraction to the authenticated Node onboarding API rather than building the full command purely in the browser. The backend can centralize TTL, server URL input, release version defaults, and command formatting.
3. Serve a repo-owned shell installer from the center through a stable public read-only endpoint, backed by an embedded script file so every self-hosted deployment generates commands against its own center.
3. Extend enrollment token persistence/service logic to enforce short TTL and one-time consumption. The exact schema can reuse `enrollment_token_issued_at` plus new consumed/expiry fields if needed.
4. Keep the installer script source in the repo, have it download release artifacts, verify the selected artifact against `sha256sums.txt`, write the current documented systemd/env/token layout, and start the service.
5. Add release artifact build automation for Linux amd64/arm64 agent binaries and checksum manifest.
6. Update `NodeOnboardingPage` and API types/client to present the one-command install as the primary path.

## Decision (ADR-lite)

**Context**: Manual agent deployment is currently too operationally heavy for many VPS nodes and requires cloning/copying code plus several env/systemd/token steps.

**Decision**: Use center-served installer scripts, checksum-verified GitHub Release binaries, fixed 30-minute one-time enrollment tokens, and a systemd-focused installer as the first-class onboarding path. Defer auto-upgrade/uninstall and non-systemd package-manager integration.

**Consequences**:

- Operators do not need Go, git clone, or source code on each VPS.
- Copied commands contain bootstrap secrets, so the UI and installer must treat them as sensitive and short-lived.
- Each self-hosted center remains the authority for its own installer command and enrollment token, while GitHub Release remains only the binary artifact source.
- Release automation becomes part of the agent deployment contract.
- Non-systemd hosts and automated upgrades remain future work.

## Out of Scope

- Replacing the visual design system or introducing Shadcn as part of this task.
- Agent auto-update, rollback, or uninstall command UX.
- Package repository publishing (`apt`/`yum`) or distro-native packages.
- Docker/Kubernetes agent install paths.
- Non-Linux or non-systemd agent hosts.
- Arbitrary remote execution or user-defined scripts on agents.
- TLS pinning/custom CA UX unless needed for MVP HTTPS deployment correctness.
- Center self-hosted binary mirror, unless GitHub Release access is proven insufficient.

## Open Questions

- None; MVP scope is ready for final confirmation.

## Expansion Sweep

### Future evolution

- Add explicit upgrade/uninstall commands once install is stable.
- Add package-manager repositories if the number of managed VPS nodes grows enough to justify native lifecycle management.

### Related scenarios

- Existing manual snippets should remain available as fallback/troubleshooting until the installer is proven reliable.
- VPS Asset Ledger linking may later want to generate install commands directly from a VPS detail page, but this MVP can reuse Node onboarding.

### Failure / edge cases

- Token may expire before operator runs the command.
- Command may be copied into shell history; TTL and one-time semantics reduce but do not eliminate leakage risk.
- VPS may lack systemd, use unsupported CPU arch, lack curl/wget, or be blocked from GitHub Release downloads.
- Agent may enroll successfully but fail to produce host samples due to service/user/path issues.

## Technical Notes

- Existing deployment docs: `docs/deploy/local-and-systemd.md`.
- Existing systemd service: `docs/deploy/systemd/houfeng-agent.service`.
- Existing onboarding handlers: `internal/center/http/handlers/node_onboarding.go`.
- Existing enrollment service: `internal/center/enrollment/service.go`.
- Existing token schema/repository behavior: `internal/center/store/nodes.go` and migrations `0001`, `0003`, `0004`.
- Existing agent config: `agent/config/config.go`.
- Existing agent runtime enrollment flow: `agent/runtime/runtime.go`.
- Existing frontend onboarding page: `web/src/pages/NodeOnboardingPage.tsx`.
- Existing web API/types: `web/src/lib/api.ts`, `web/src/lib/types.ts`.
- Existing build targets: `Makefile` has `build-agent` but no cross-platform release target yet.
