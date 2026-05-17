# fix: agent service exits after installer success

## Goal

Fix the real deployment bug where the v0.3.3 installer reports success on Debian 11 but the Linux/systemd agent exits immediately because the release binary depends on newer glibc symbols than the host provides.

## Requirements

* Do not record or repeat the exposed enrollment token from the user report.
* Linux agent release assets must run on Debian 11-era glibc hosts; the installer must not install binaries that require GLIBC_2.32/2.34 on Debian 11.
* Build `houfeng-agent_<version>_linux_amd64` and `houfeng-agent_<version>_linux_arm64` with `CGO_ENABLED=0` unless a future product decision intentionally introduces native dependencies.
* Release workflow verification must reject agent assets that depend on glibc/dynamic Linux libc.
* Preserve the installer contract: center-generated command, public script without secrets, checksum verification, token file restrictive permissions, Linux/systemd amd64/arm64 MVP only.

## Acceptance Criteria

* [ ] Local release asset build produces linux/amd64 and linux/arm64 binaries that do not require glibc (`ldd` says not dynamic executable, or equivalent `file`/metadata verification).
* [ ] Generated `sha256sums.txt` verifies both Linux agent assets.
* [ ] Release workflow validates both clean Go VCS metadata and no glibc dependency before uploading assets.
* [ ] Existing Go verification passes.
* [ ] A follow-up release publishes corrected agent assets, and downloaded assets are verified as checksum-valid, clean metadata, and non-glibc-dependent.

## Definition of Done

* Root cause recorded: release asset was dynamically linked to build-runner glibc and failed on Debian 11 with missing GLIBC_2.32/2.34.
* Fix implemented in release build and workflow verification.
* PR/check/release chain completed.
* User is told to regenerate the exposed token and rerun the installer after the fixed release is available.

## Technical Approach

Set `CGO_ENABLED=0` for both Linux agent release builds in `make build-agent-release`, keep output names and checksum manifest unchanged, and extend `publish-images` agent asset verification to fail if `ldd` reports a dynamically linked executable. This keeps the thin Go agent portable across older glibc hosts without adding package manager distribution or OS-specific builds.

## Decision (ADR-lite)

**Context**: Debian 11 host failed to start v0.3.3 with `/lib/x86_64-linux-gnu/libc.so.6: version GLIBC_2.34 not found` and `GLIBC_2.32 not found`.
**Decision**: Linux agent release assets are pure-Go static binaries via `CGO_ENABLED=0`.
**Consequences**: The agent remains portable for the MVP Linux/systemd target and avoids requiring operators to upgrade glibc. Future features needing native libc/cgo must make a new release packaging decision.

## Out of Scope

* Auto-upgrade/uninstall UX.
* Package repositories or distro-specific `.deb`/`.rpm` builds.
* Non-systemd or non-Linux agent installation.
* Center-hosted binary mirrors.

## Technical Notes

* User observed installer success, then `systemctl status houfeng-agent` showed `Active: activating (auto-restart)` and `ExecStart=/usr/local/bin/houfeng-agent (code=exited, status=1/FAILURE)`.
* `journalctl -u houfeng-agent` showed repeated dynamic loader failures for GLIBC_2.34 and GLIBC_2.32 before the Go process could start.
* Prior release workflow already verifies checksum and `vcs.modified=false`; this task adds libc portability verification.
