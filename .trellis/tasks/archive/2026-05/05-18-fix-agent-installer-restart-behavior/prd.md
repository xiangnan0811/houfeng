# Fix agent installer restart behavior

## Goal

When an operator re-runs the center-generated one-command installer on an already-bound Linux systemd node, the installer must behave as the current acceptable upgrade path: preserve the existing post-enrollment sync token and ensure the running `houfeng-agent` process uses the newly installed binary and config without requiring a manual `systemctl restart`.

## Requirements

* Preserve an existing post-enrollment token file that contains both `node_id` and `sync_token`; do not force re-enrollment for already-bound nodes.
* Continue writing the generated enrollment token only when the token file is absent or not a post-enrollment sync credential.
* After installing/replacing the binary, env file, and unit, run `systemctl daemon-reload` and ensure the service is enabled.
* If `houfeng-agent.service` is already active, restart it so the new binary/config takes effect immediately.
* If the service is not active, start it so fresh installs still work.
* Keep the implementation POSIX shell/systemd-compatible with the current Linux amd64/arm64 installer boundary.
* Update docs to describe the one-command installer as install-or-upgrade behavior for the current early-development phase.

## Acceptance Criteria

* [ ] Re-running the generated one-command installer on a bound node preserves `/etc/houfeng-agent/token` when it contains post-enrollment sync credentials.
* [ ] Re-running the installer on a running service restarts `houfeng-agent.service` after replacing the binary/config/unit.
* [ ] Running the installer on a fresh or stopped service enables and starts `houfeng-agent.service`.
* [ ] Installer tests or script-level assertions cover the new systemctl sequence.
* [ ] Deployment docs no longer imply that re-running the one-command installer only starts services that are not already running.
* [ ] `make verify-go` or a narrower installer-focused test plus full repo verification passes before PR.

## Definition of Done

* Tests added or updated for installer behavior.
* Go formatting/tests and web verification are green through the normal project checks if code/docs change.
* No enrollment tokens or secrets are logged or committed.
* PR merged through normal branch flow, then release flow followed through.

## Technical Approach

Update `internal/center/installer/houfeng-agent-install.sh` so the final systemd phase separates enablement from process activation:

1. `systemctl daemon-reload`
2. `systemctl enable houfeng-agent`
3. `if systemctl is-active --quiet houfeng-agent; then systemctl restart houfeng-agent; else systemctl start houfeng-agent; fi`

This is intentionally not a full installer-upgrade framework. It only ensures the accepted early-stage upgrade path actually activates the new agent process.

## Decision (ADR-lite)

**Context:** The project is still early and the user accepts re-running the center-generated one-command installer as the current upgrade path. Real testing showed that `systemctl enable --now` does not restart an already-running service, leaving the old agent process alive and causing heartbeat-loss confusion until manual restart.

**Decision:** Keep the one-command installer as the upgrade path for now, but make it idempotently preserve sync credentials and restart active services after installing the new binary/config.

**Consequences:** Existing bound nodes can be upgraded without manual restart or re-binding. This does not add auto-upgrade, uninstall, package-manager integration, binary rollback, or a full version-aware upgrade manager.

## Out of Scope

* Agent self-update or center-pushed auto-upgrade.
* Package repository, Homebrew/apt/yum packaging, or non-systemd installer support.
* Re-enrollment UX changes or token rotation beyond preserving existing sync credentials.
* Database migration or agent protocol changes.

## Technical Notes

* Current installer: `internal/center/installer/houfeng-agent-install.sh` ends with `systemctl daemon-reload` and `systemctl enable --now houfeng-agent`; this starts fresh services but does not restart an already-running service.
* Current docs: `docs/deploy/local-and-systemd.md` describes the one-command installer behavior and should mention install-or-upgrade semantics.
* User evidence: after re-running a generated install command on a bound node, center UI showed heartbeat loss until `houfeng-agent` was manually restarted.
