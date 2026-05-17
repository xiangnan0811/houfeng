# Auto release Docker publishing

## Goal

Add the missing auto-release chain so a normal feature PR merge to `main` opens or updates an auto release PR; after that release PR passes checks and is merged, a GitHub Release is published and the Docker image workflow builds and pushes `linnea7171/houfeng` to Docker Hub. Also resolve the GitHub Actions Node.js 20 deprecation annotation for Docker actions.

## What I already know

* PR #92 added release/manual Docker image publishing, but did not add auto release PR automation.
* The expected flow is: feature branch PR -> CI green -> merge to `main` -> auto release PR appears -> monitor release PR checks -> merge release PR -> GitHub Release publication -> Docker image build/push to Docker Hub.
* Current `publish-images.yml` triggers on `release.published` and manual dispatch; it does not itself create releases.
* Current `ci.yml` includes Docker actions that produced GitHub's Node.js 20 deprecation annotation.
* Xirang uses Release Please on pushes to `main` to create/update release PRs and publish GitHub Releases after the release PR is merged.
* Xirang's Docker publishing workflow is separate and is triggered by `release.published`.
* Docker official actions now have Node 24 major versions: `setup-buildx@v4`, `build-push@v7`, `login@v4`, `metadata@v6`.
* This repository currently has no git tags or GitHub releases, so the initial release-please manifest should establish a first baseline version.
* Work must happen on a feature branch, not on `main`; current branch is `feature/auto-release-docker-publish`.

## Research References

* [`research/xirang-release-automation.md`](research/xirang-release-automation.md) — Xirang uses Release Please with a root simple release, release PRs, and GitHub Release publication that triggers Docker image publishing.
* [`research/actions-node20-deprecation.md`](research/actions-node20-deprecation.md) — Docker actions have Node 24 major versions; upgrading action majors is preferred over `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.

## Requirements

* Add Release Please automation triggered by pushes to `main`.
* Release automation must open/update release PRs instead of directly pushing release commits to `main`.
* Merging the release PR must publish a GitHub Release automatically.
* Published GitHub Releases must continue to trigger Docker image publishing to Docker Hub through `publish-images.yml`.
* Use a root repo-wide release configuration suitable for Go + web (`release-type: simple`) rather than tying versioning to `web/package.json`.
* Use `include-v-in-tag: true` so release tags match existing Houfeng docs/examples (`vX.Y.Z`).
* Start the release-please manifest at `0.1.0` because the repo currently has no existing tags/releases.
* Use a `RELEASE_PLEASE_TOKEN` secret so release-please PRs/commits can trigger CI under branch protection, following Xirang.
* Upgrade Docker actions to Node 24 major versions instead of relying on `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`.
* Preserve release-only Docker publishing: no Docker image publish on ordinary `main` push or PR.
* CI must still validate Go, web, and no-push Docker image build.
* Docs/spec must explain the full release -> Docker publish chain and required secrets.

## Acceptance Criteria

* [ ] `.github/workflows/release-please.yml` exists and triggers on push to `main`.
* [ ] `release-please-config.json`, `.release-please-manifest.json`, and `CHANGELOG.md` establish root simple release automation.
* [ ] Release Please workflow uses `RELEASE_PLEASE_TOKEN` and minimal required write permissions.
* [ ] Release PRs run normal CI and are expected to be monitored/merged as part of post-PR follow-through.
* [ ] Merging a release PR publishes a GitHub Release with a `vX.Y.Z` tag.
* [ ] Published GitHub Releases trigger `publish-images.yml` and push Docker tags.
* [ ] `publish-images.yml` still has no `push` or `pull_request` trigger.
* [ ] Docker actions use Node 24 major versions (`setup-buildx@v4`, `build-push@v7`, `login@v4`, `metadata@v6`).
* [ ] CI still verifies Go, web, and no-push Docker image build.
* [ ] Docs/spec explain the full release -> Docker publish chain and required `RELEASE_PLEASE_TOKEN`, `DOCKERHUB_USERNAME`, and `DOCKERHUB_TOKEN` secrets.

## Definition of Done

* Work committed on feature branch and PR follow-through completed.
* Workflow YAML/action lint passes.
* Static checks confirm ordinary `main` pushes create/update release PRs but do not publish Docker images directly.
* Static checks confirm Docker image publishing remains release/manual only.
* Existing verification remains green.
* Docs/spec updated for durable release behavior.

## Out of Scope

* Publishing Docker images directly from feature PRs or ordinary main pushes.
* Containerizing agents.
* Changing Docker Compose to build locally.
* Manual Docker Hub publication outside GitHub Actions.
* Docker Hub description sync.
* Private deployment automation.
* Building/uploading Linux agent release assets in this task.

## Technical Approach

* Add `.github/workflows/release-please.yml`, modeled after Xirang, triggered on `push` to `main`.
* Grant only `contents: write` and `pull-requests: write` in the release workflow.
* Use `googleapis/release-please-action` with `config-file: release-please-config.json` and `manifest-file: .release-please-manifest.json`.
* Add `release-please-config.json` with root package `.` using `release-type: simple`, `include-v-in-tag: true`, and changelog sections without emojis.
* Add `.release-please-manifest.json` with `{ ".": "0.1.0" }` as the initial baseline.
* Add `CHANGELOG.md` as the release-please managed changelog seed.
* Upgrade Docker action references in `ci.yml` and `publish-images.yml` to Node 24 major versions.
* Keep `publish-images.yml` triggered only by `release.published` and `workflow_dispatch`.

## Decision (ADR-lite)

**Context**: Docker image publishing now exists but only fires after a GitHub Release or manual dispatch. The missing piece is automatic release PR/release creation after normal feature PRs merge to `main`. GitHub also warns that the Docker actions used by CI/publish workflows run on Node.js 20.

**Decision**: Add Release Please root/simple automation, using `RELEASE_PLEASE_TOKEN`, so main merges create release PRs and release PR merges publish GitHub Releases. Upgrade Docker official actions to their Node 24 major versions.

**Consequences**: Docker image publishing remains release-only but becomes reachable through the normal PR -> release PR -> release -> Docker image chain. The maintainer must configure `RELEASE_PLEASE_TOKEN`, `DOCKERHUB_USERNAME`, and `DOCKERHUB_TOKEN` secrets for the full chain to work.

## Technical Notes

* Files inspected: `.github/workflows/ci.yml`, `.github/workflows/publish-images.yml`, `.trellis/spec/backend/directory-structure.md`.
* Repository currently has no git tags or GitHub releases.
* Docker/Compose boundary remains center+web image plus PostgreSQL; no agent container.
