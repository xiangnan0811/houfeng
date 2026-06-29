# Installer minisign dependency recovery

## Goal

Make the center-generated agent install/upgrade command usable on common Linux systemd hosts even when `minisign` is missing, while preserving signed-release verification and making the operator's choice explicit before installing a missing verifier dependency.

## Background

- A real Debian 11 bullseye x86_64 host failed agent upgrade to `v0.55.0` with `houfeng-agent install: minisign is required to verify release checksums`.
- `apt install minisign` and `apt-get install minisign` on that host both failed with `E: Unable to locate package minisign`.
- Current installer code fails immediately when `command -v minisign` is false.
- The installer must verify `sha256sums.txt.minisig` before trusting `sha256sums.txt`; checksum-only fallback is not acceptable.
- The generated command feeds the enrollment token through installer stdin, so interactive prompts must not consume stdin.

## Confirmed Compatibility Facts

- Debian package availability differs by release:
  - Debian 11 bullseye: no `minisign` package in the checked main amd64 package index.
  - Debian 12 bookworm: `minisign=0.11-1`.
  - Debian 13 trixie: `minisign=0.12-1`.
- Ubuntu package availability differs by release:
  - Ubuntu 20.04 focal and 22.04 jammy: no `minisign` package found in checked amd64 archive components.
  - Ubuntu 24.04 noble: `minisign=0.11-1` in universe.
- Alpine community package pages exist for recent 3.19-3.22 releases, but the Houfeng installer currently requires systemd, so Alpine is not a supported runtime target unless a systemd environment exists.
- EPEL has `minisign` packages for EL 8 and EL 9, but EPEL may not be enabled on real hosts.
- Upstream `minisign-0.12-linux.tar.gz` contains statically linked x86_64 and aarch64 binaries and has SHA256 `9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73`.

## Requirements

- The installer must keep failing closed: agent release assets are installed only after a valid minisign verification of the checksum manifest and a matching SHA256 check for the selected agent binary.
- The installer must detect missing `minisign` before release verification and present a clear explanation:
  - `minisign` is needed to verify the signed checksum manifest.
  - Continuing without it is not supported.
  - If the operator refuses installation, the agent install/upgrade will stop before modifying the agent binary, config, token, or systemd unit.
- On an interactive TTY, the installer must ask the operator for consent before installing the missing `minisign` verifier.
- The prompt must use `/dev/tty` or equivalent so it does not consume the enrollment token passed through `--enrollment-token-stdin`.
- The generated center command must support unattended copy/paste by passing an explicit consent flag for dependency installation.
- Operators who run the installer manually must be able to opt out with a flag and must get a deterministic failure if `minisign` is missing.
- Non-interactive environments without explicit consent must fail with a message that tells the operator which flag to pass if they want automatic dependency installation.
- The dependency recovery path must work on Linux `amd64` and `arm64`, matching the agent release architectures.
- The dependency recovery path must not silently trust an unverified binary download. If downloading upstream `minisign`, the installer must verify the tarball SHA256 before installing the binary.
- The installer must use existing downloader and checksum tool detection; it must not require a package manager to be present.
- The installer must handle common failure cases with actionable errors: unsupported architecture, missing downloader, missing checksum tool, missing `tar`, failed download, SHA256 mismatch, missing expected binary in tarball, install failure, and failed verification after install.
- Documentation and UI/operator text must stop saying the installer simply fails when `minisign` is missing. They must describe the prompted/flagged recovery behavior.

## Acceptance Criteria

- [ ] On a host with `minisign` already present, the installer continues through the existing signed manifest and checksum verification path without prompting or installing anything.
- [ ] On an interactive Linux `amd64` or `arm64` host without `minisign`, the installer explains the missing dependency and asks for consent using `/dev/tty`.
- [ ] If the operator answers yes, the installer downloads upstream `minisign` 0.12, checks the pinned tarball SHA256, installs the correct architecture binary to `/usr/local/bin/minisign`, verifies it is executable, and continues with release verification.
- [ ] If the operator answers no, the installer exits before downloading release assets or writing `/usr/local/bin/houfeng-agent`, `/etc/houfeng-agent/*`, or `/etc/systemd/system/houfeng-agent.service`.
- [ ] If no TTY is available and no explicit install-dependency flag is passed, the installer exits before release download or local writes with an actionable message.
- [ ] The center-generated install/upgrade command includes an explicit dependency consent flag so normal UI-generated copy/paste commands work on Debian 11-style hosts.
- [ ] The installer supports an explicit opt-out flag that disables dependency installation and fails if `minisign` is missing.
- [ ] Tests assert the presence and ordering of the dependency recovery flow, signed manifest verification, no checksum-only fallback, no ignored `minisign` failure, and center command flag generation.
- [ ] Deployment docs and onboarding UI text mention the missing-verifier recovery behavior and the security trade-off.

## Out of Scope

- Supporting non-systemd hosts.
- Supporting non-Linux operating systems.
- Adding a full package-manager abstraction for apt, dnf, yum, zypper, pacman, or apk.
- Automatically enabling third-party package repositories such as EPEL or Ubuntu universe.
- Providing a checksum-only or unsigned install fallback.
- Designing a general-purpose dependency manager for future agent dependencies.

## Product Decisions

- The default generated command should be optimized for the common operator path: copy from center, paste into a root/sudo shell, and succeed without requiring the operator to separately diagnose `minisign` packaging differences.
- Manual direct installer usage remains conservative: missing dependency installation requires either an interactive yes answer or an explicit flag.
- The auto-installed verifier is placed in `/usr/local/bin/minisign`, matching local-admin installed tooling and avoiding mutation of distro package databases.
