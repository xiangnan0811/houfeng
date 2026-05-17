# Fix agent release asset publishing

## Goal

Real first-node onboarding currently fails because the center-served installer command points to GitHub Release `v0.3.1`, but that release has no `houfeng-agent_<version>_linux_<arch>` binaries or `sha256sums.txt`. Fix the current release so the operator can install the first agent, and fix the release pipeline so future Release Please releases publish the installer-required agent assets automatically.

## What I already know

* The real installer command reached `/api/agent/install.sh`, detected `linux/amd64` with systemd, then failed downloading `houfeng-agent_v0.3.1_linux_amd64` with HTTP 404.
* `gh release view v0.3.1` shows `assets: []`.
* The installer expects assets at `https://github.com/<owner>/<repo>/releases/download/<version>/houfeng-agent_<version>_linux_<amd64|arm64>` plus `sha256sums.txt`.
* `make build-agent-release VERSION=vX.Y.Z` already builds the exact two Linux agent assets and checksum manifest under `dist/`.
* `.github/workflows/publish-images.yml` currently publishes Docker images on `release.published` but does not build/upload agent release assets.
* Documentation/spec currently says maintainers upload agent assets manually; current real deployment shows this manual step is too easy to miss.

## Requirements

* Restore current installability for `v0.3.1` by uploading:
  * `houfeng-agent_v0.3.1_linux_amd64`
  * `houfeng-agent_v0.3.1_linux_arm64`
  * `sha256sums.txt`
* Update release automation so future published GitHub Releases build and upload the same agent assets automatically.
* Keep the installer contract unchanged: the center serves the script; GitHub Release hosts only binaries and checksum manifest.
* Keep Docker image publishing behavior intact: release/manual only, no push/pull_request image publishing.
* Update active deployment/smoke docs and backend release/onboarding spec so they describe automated agent asset publishing instead of a manual-only upload step.
* Do not expose or reuse enrollment tokens in logs, commits, docs, or final output. Because the token appeared in user-provided text, recommend regenerating the install command after assets are available.

## Acceptance Criteria

* [ ] `v0.3.1` release contains both Linux agent binaries and `sha256sums.txt`.
* [ ] `sha256sums.txt` contains exact entries for both asset names expected by the installer.
* [ ] A URL sanity check for the current `v0.3.1` amd64 asset returns success instead of 404.
* [ ] `.github/workflows/publish-images.yml` has a release/manual agent-asset publishing path using the existing `make build-agent-release VERSION=vX.Y.Z` target.
* [ ] Workflow permissions are sufficient to upload release assets without broadening Docker credential exposure.
* [ ] `make build-agent-release VERSION=v0.3.1` passes locally.
* [ ] `git diff --check` passes.
* [ ] `actionlint` runs if available; otherwise static workflow review is recorded.
* [ ] Relevant docs/spec are updated to match the new automated behavior.

## Definition of Done

* Current real deployment can retry with a regenerated command and pass the release asset download/checksum stage.
* Future Release Please release publication automatically creates the assets the installer depends on.
* PR is created, checks are monitored, merged, release PR/release/image publishing are followed through per Houfeng workflow.

## Technical Approach

1. Build and upload the missing `v0.3.1` agent assets immediately from the current release source code to unblock installation.
2. Extend `.github/workflows/publish-images.yml` with an `agent-assets` job that checks out the resolved release source ref, sets up Go, runs `make build-agent-release VERSION=v<version>`, verifies the expected files, and uploads them to the matching GitHub Release with `gh release upload`.
3. Update active docs/spec from manual-only asset upload to automated release asset publishing, while preserving the manual build target for local sanity and emergency backfill.

## Decision (ADR-lite)

**Context**: The installer contract is correct, but release automation only published Docker images; the GitHub Release assets required by host agent installation were missing.

**Decision**: Keep GitHub Release as the binary/checksum authority for agent installation, and make the existing release workflow publish those assets automatically on release publication.

**Consequences**: Release workflow now needs `contents: write` to upload assets. This is bounded to GitHub Release assets and does not add Docker publication triggers or move tokens/scripts into GitHub Release.

## Out of Scope

* Changing the installer asset naming contract.
* Hosting agent binaries from the center container.
* Adding package repositories, auto-upgrade, uninstall UX, non-systemd support, Docker/Kubernetes agent installs, or additional architectures beyond Linux amd64/arm64.

## Technical Notes

* Task triggered by real deployment failure for first agent onboarding.
* Relevant files inspected:
  * `.github/workflows/publish-images.yml`
  * `.github/workflows/release-please.yml`
  * `Makefile`
  * `internal/center/installer/houfeng-agent-install.sh`
  * `docs/deploy/local-and-systemd.md`
  * `docs/operations/v1-smoke-run.md`
  * `.trellis/spec/backend/directory-structure.md`
  * `.trellis/spec/guides/branch-workflow-governance.md`
