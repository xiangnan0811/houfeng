# Research: Xirang Release Automation

- **Query**: Research https://github.com/xiangnan0811/xirang for release automation that creates release PRs and publishes GitHub Releases. Include workflow files, release-please config/manifest/package files, token/permissions/secrets, trigger behavior after main merges, how release PRs are merged and releases published, and how Docker image publishing is connected. Map what Houfeng should adapt and what not to copy.
- **Scope**: external
- **Date**: 2026-05-17

## Findings

### Files Found

| File Path | Description |
|---|---|
| `xiangnan0811/xirang:.github/workflows/release-please.yml` | Release Please workflow that opens/updates release PRs and publishes GitHub Releases after release PR merge. |
| `xiangnan0811/xirang:release-please-config.json` | Release Please configuration for single root package, simple release type, v-prefixed tags, and changelog sections. |
| `xiangnan0811/xirang:.release-please-manifest.json` | Release Please manifest with current root package version. |
| `xiangnan0811/xirang:CHANGELOG.md` | Generated changelog maintained by release-please release PRs. |
| `xiangnan0811/xirang:.github/workflows/publish-images.yml` | Docker image publishing workflow triggered by GitHub Release publication or manual dispatch. |
| `xiangnan0811/xirang:.github/workflows/dockerhub-description.yml` | Docker Hub README/description sync workflow, separate from image publishing. |
| `xiangnan0811/xirang:.github/workflows/deploy.yml` | Manual private deployment workflow that waits for an image tag and deploys by SSH. |
| `xiangnan0811/xirang:.github/workflows/ci.yml` | CI workflow triggered on every push and PR; validates PR title only for pull_request events. |
| `xiangnan0811/xirang:deploy/allinone/Dockerfile` | Multi-stage all-in-one Docker image build target used by publish workflow. |
| `xiangnan0811/xirang:docker-compose.yml` | Runtime compose file consuming `linnea7171/xirang:${IMAGE_TAG:-latest}`. |
| `xiangnan0811/xirang:Makefile` | Local Docker build/push/buildx targets using the same Docker Hub image name. |
| `xiangnan0811/xirang:web/package.json` | Frontend package file; version remains `0.1.0` and is not used as release-please version source for root release. |

### Release Please workflow

`xiangnan0811/xirang:.github/workflows/release-please.yml:1-26`:

```yaml
name: Release Please

on:
  push:
    branches:
      - main

concurrency:
  group: release-please-${{ github.ref }}
  cancel-in-progress: true

permissions:
  contents: write
  pull-requests: write

jobs:
  release-please:
    name: Prepare Release PR
    runs-on: ubuntu-latest
    steps:
      - name: Run release-please
        uses: googleapis/release-please-action@45996ed1f6d02564a971a2fa1b5860e934307cf7 # v5.0.0
        with:
          token: ${{ secrets.RELEASE_PLEASE_TOKEN }}
          config-file: release-please-config.json
          manifest-file: .release-please-manifest.json
```

Observed behavior:

- Trigger is `push` to `main` only (`release-please.yml:3-6`).
- Every merge to `main` runs release-please; it either creates/updates a release PR when releasable conventional commits exist, or publishes the release after the release PR itself is merged.
- Concurrency key is per ref (`release-please-${{ github.ref }}`) and cancels previous in-progress release-please runs (`release-please.yml:8-10`).
- Workflow grants `contents: write` and `pull-requests: write` (`release-please.yml:12-14`), which are needed to write tags/releases/changelog commits and manage PRs.
- The action is pinned to a full commit SHA for `googleapis/release-please-action`, with comment `# v5.0.0` (`release-please.yml:21-22`).
- It uses `secrets.RELEASE_PLEASE_TOKEN`, not the default `GITHUB_TOKEN` (`release-please.yml:23-26`). In GitHub Actions, this usually means a PAT or GitHub App token is configured so the bot-created release PR branch/commits can trigger CI and satisfy branch protection.

### Release Please config, manifest, package/version files

`xiangnan0811/xirang:release-please-config.json:1-23`:

```json
{
  "$schema": "https://raw.githubusercontent.com/googleapis/release-please/main/schemas/config.json",
  "include-v-in-tag": true,
  "packages": {
    ".": {
      "release-type": "simple",
      "changelog-path": "CHANGELOG.md",
      "changelog-sections": [
        { "type": "feat", "section": "✨ Features", "hidden": false },
        { "type": "fix", "section": "🐛 Bug Fixes", "hidden": false },
        { "type": "perf", "section": "⚡ Performance", "hidden": false },
        { "type": "revert", "section": "⏪ Reverts", "hidden": false },
        { "type": "docs", "section": "📚 Documentation", "hidden": false },
        { "type": "refactor", "section": "♻️ Refactor", "hidden": true },
        { "type": "test", "section": "✅ Tests", "hidden": true },
        { "type": "build", "section": "🛠 Build System", "hidden": true },
        { "type": "ci", "section": "👷 CI", "hidden": true },
        { "type": "chore", "section": "🧹 Chores", "hidden": true },
        { "type": "style", "section": "💄 Style", "hidden": true }
      ]
    }
  }
}
```

Key details:

- Uses a single manifest package at path `.` (`release-please-config.json:4-21`).
- Uses `release-type: simple` (`release-please-config.json:6`), so it does not appear to update language-specific package manifests as authoritative version sources.
- `include-v-in-tag: true` makes tags such as `v0.33.1` (`release-please-config.json:3`).
- `CHANGELOG.md` is the changelog path (`release-please-config.json:7`).
- Visible changelog sections are `feat`, `fix`, `perf`, `revert`, `docs`; hidden sections include `refactor`, `test`, `build`, `ci`, `chore`, `style` (`release-please-config.json:8-20`).

`xiangnan0811/xirang:.release-please-manifest.json:1-3`:

```json
{
  ".": "0.33.1"
}
```

`xiangnan0811/xirang:web/package.json:1-15` shows the web package has its own private package version `0.1.0`; it is not aligned with release manifest version `0.33.1`. A fetch for `backend/package.json` returned 404, consistent with the backend being Go-only.

`xiangnan0811/xirang:CHANGELOG.md:1-9` starts with release `0.33.1`, links `v0.33.0...v0.33.1`, and records a documentation entry from PR/commit metadata. This is the file updated by release PRs.

### Trigger behavior after main merges

Sequence inferred from workflow triggers and observed PR/release history:

1. A normal feature/fix/docs PR is merged to `main`.
2. `Release Please` runs on the resulting `push` to `main` (`release-please.yml:3-6`).
3. If conventional commits since the last release warrant a release under the config, release-please opens or updates a PR titled like `chore(main): release 0.33.1`.
4. The release PR changes at least `CHANGELOG.md` and `.release-please-manifest.json`; with `release-type: simple`, no project package file is shown as an authoritative version target.
5. CI runs on the release PR because `.github/workflows/ci.yml` listens on `pull_request` (`ci.yml:3-5`).
6. A maintainer merges the release PR to `main`.
7. The merge to `main` triggers `Release Please` again. Because the release PR has been merged, release-please creates the Git tag and publishes the GitHub Release.
8. Publishing the GitHub Release triggers Docker image publishing through `.github/workflows/publish-images.yml` on `release.published`.

Recent observed release PRs from GitHub search:

| PR | Title | Created | Merged |
|---|---|---:|---:|
| `xiangnan0811/xirang#176` | `chore(main): release 0.33.1` | 2026-05-16T08:49:00Z | 2026-05-16T08:55:28Z |
| `xiangnan0811/xirang#174` | `chore(main): release 0.33.0` | 2026-05-15T04:18:00Z | 2026-05-15T04:26:32Z |
| `xiangnan0811/xirang#172` | `chore(main): release 0.32.0` | 2026-05-15T00:06:23Z | 2026-05-15T00:09:29Z |
| `xiangnan0811/xirang#169` | `chore(main): release 0.31.3` | 2026-05-14T08:05:40Z | 2026-05-14T08:46:26Z |
| `xiangnan0811/xirang#167` | `chore(main): release 0.31.2` | 2026-05-14T02:49:37Z | 2026-05-14T02:53:45Z |

Recent observed releases from GitHub Releases API:

| Tag | Name | Published | Target commit | Assets |
|---|---|---:|---|---|
| `v0.33.1` | `v0.33.1` | 2026-05-16T08:55:37Z | `af3841312bb726df65eb78a6f9db81aa42eb1a6b` | none |
| `v0.33.0` | `v0.33.0` | 2026-05-15T04:26:43Z | `6836882a7d9f207bc89c5b60d2bf2f4060ce0d1f` | none |
| `v0.32.0` | `v0.32.0` | 2026-05-15T00:09:39Z | `88ffc1496ea32ef468c2c571987e61f4fe950a1d` | none |
| `v0.31.3` | `v0.31.3` | 2026-05-14T08:46:37Z | `55bd2eb87b458676e25b23322a9f99c9cb23f0c0` | none |
| `v0.31.2` | `v0.31.2` | 2026-05-14T02:54:02Z | `cb06d17a9bb014506906690de90a5c52520f898b` | none |

The release publication timestamps are shortly after release PR merge timestamps, consistent with release-please publishing releases on the post-merge push to `main`.

### PR merge behavior and CI

`xiangnan0811/xirang:.github/workflows/ci.yml:3-13`:

```yaml
on:
  push:
  pull_request:

concurrency:
  group: ci-${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

permissions:
  contents: read
```

Important release-adjacent details:

- CI runs on all pushes and PRs, not only `main` (`ci.yml:3-5`).
- CI has `contents: read` only (`ci.yml:11-13`).
- PR title validation runs only on `pull_request` events (`ci.yml:15-26`), using `scripts/check-pr-title.sh`. Release PR titles such as `chore(main): release 0.33.1` match conventional title style.
- Historical PR `#2`, `fix(ci): make release-please branches trigger CI`, indicates the project intentionally adjusted CI so release-please branches/PRs get CI coverage.

No repository setting or branch protection details are visible from repository files/API output in this research. The observed release PRs are maintainer-merged PRs, not direct commits to `main`.

### Docker image publishing connection

`xiangnan0811/xirang:.github/workflows/publish-images.yml:1-341` publishes Docker images. It is separate from release-please, and the connection point is the GitHub Release event.

Trigger and permissions (`publish-images.yml:1-26`):

```yaml
name: Publish Docker Images

on:
  release:
    types:
      - published
  workflow_dispatch:
    inputs:
      version:
        description: "维护者手动重发时使用的镜像版本号（例如 0.2.0 或 v0.2.0）"
        required: true
        type: string
      source_ref:
        description: "维护者手动重发时使用的源码 ref（tag/branch/SHA）"
        required: true
        type: string

concurrency:
  group: publish-images-${{ github.ref }}
  cancel-in-progress: false

permissions:
  contents: read
  attestations: write
  id-token: write
```

Key details:

- Primary trigger is `release` event with type `published` (`publish-images.yml:3-6`). This fires after release-please publishes the GitHub Release.
- Manual rebuild path exists through `workflow_dispatch` with required `version` and `source_ref` inputs (`publish-images.yml:7-16`).
- Permissions are `contents: read`, `attestations: write`, and `id-token: write` (`publish-images.yml:22-25`), supporting checkout and provenance attestation.
- Concurrency does not cancel in-progress publishes (`publish-images.yml:18-20`).

Build job (`publish-images.yml:27-155`):

- Matrix builds native `linux/amd64` on `ubuntu-latest` and `linux/arm64` on `ubuntu-24.04-arm` (`publish-images.yml:32-41`).
- Uses `REGISTRY_IMAGE: docker.io/linnea7171/xirang` (`publish-images.yml:42-43`).
- Rejects prerelease GitHub Releases (`publish-images.yml:45-50`).
- Checks out the release tag for release events or the manual `source_ref` for dispatch (`publish-images.yml:52-55`).
- Resolves source commit with `git rev-parse HEAD` (`publish-images.yml:57-60`).
- Sets up Buildx using a SHA-pinned action (`publish-images.yml:62-63`).
- Resolves semver by stripping `refs/tags/` and leading `v`, then requiring `^[0-9]+\.[0-9]+\.[0-9]+$` (`publish-images.yml:65-90`).
- Logs in to Docker Hub with `secrets.DOCKERHUB_USERNAME` and `secrets.DOCKERHUB_TOKEN` (`publish-images.yml:91-96`).
- Generates image tags with `docker/metadata-action`: `vX.Y.Z`, `X.Y.Z`, and `latest` only for non-prerelease release events (`publish-images.yml:98-110`).
- Builds `deploy/allinone/Dockerfile` with `docker/build-push-action`, pushing by digest only and using per-platform GitHub Actions cache scopes (`publish-images.yml:112-124`).
- Scans each platform digest with Trivy and fails on `HIGH,CRITICAL`, `ignore-unfixed: true` (`publish-images.yml:126-139`).
- Uploads digest marker artifacts for the publish job (`publish-images.yml:141-155`).

Publish job (`publish-images.yml:157-341`):

- Depends on successful `build` matrix (`publish-images.yml:157-162`).
- Repeats checkout/version/login/metadata setup (`publish-images.yml:166-225`).
- Downloads digest artifacts (`publish-images.yml:227-232`).
- Creates and pushes a multi-platform manifest with all resolved tags using `docker buildx imagetools create` (`publish-images.yml:234-283`).
- Requires at least two platform digests before publishing (`publish-images.yml:259-273`).
- Records the manifest digest via `docker buildx imagetools inspect` (`publish-images.yml:275-277`).
- Attests build provenance with `actions/attest-build-provenance` and `push-to-registry: true` (`publish-images.yml:291-296`).
- Writes a GitHub Step Summary showing version, source ref/commit, digest, latest status, tags, and platform digests (`publish-images.yml:298-341`).

### Docker build target and runtime image use

`xiangnan0811/xirang:deploy/allinone/Dockerfile:1-89`:

- Builds web with Node (`node:20-alpine`) and `npm ci`/`npm run build` (`Dockerfile:1-8`).
- Builds Go backend with `golang:1.26.3-alpine`, `TARGETOS`, `TARGETARCH`, CGO enabled, and binary output `/out/xirang` (`Dockerfile:10-24`).
- Builds `supercronic` from source in a separate Go builder stage (`Dockerfile:26-38`).
- Uses `nginx:1.27-alpine` runtime image, installs runtime dependencies, copies web dist, backend binary, nginx template, entrypoint, backup script, and cron file (`Dockerfile:40-68`).
- Runtime defaults include SQLite paths and logs; exposes port `10761`; includes a healthcheck (`Dockerfile:71-86`).

`xiangnan0811/xirang:docker-compose.yml:1-20` consumes the published image:

```yaml
services:
  xirang:
    image: linnea7171/xirang:${IMAGE_TAG:-latest}
    container_name: xirang
    env_file:
      - .env
    environment:
      TZ: ${TZ:-Asia/Shanghai}
    volumes:
      - ./data:/data
      - ./backups:/backup
      - ./logs:/logs
    ports:
      - "10761:10761"
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://127.0.0.1:10761/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 15s
    restart: unless-stopped
```

`xiangnan0811/xirang:Makefile:87-112` mirrors the same image name for local Docker operations:

- `DOCKER_FULL_IMAGE = docker.io/linnea7171/xirang`.
- `docker-build` builds `deploy/allinone/Dockerfile` locally.
- `docker-push` pushes `$(DOCKER_TAG)` and only pushes `latest` when `TAG_LATEST=1`.
- `docker-buildx` builds `linux/amd64,linux/arm64` and pushes.

### Docker Hub description sync

`xiangnan0811/xirang:.github/workflows/dockerhub-description.yml:1-52` is independent from image publication:

- Triggered by `push` to `main` only when `README.md` or the workflow file changes, and by manual dispatch (`dockerhub-description.yml:3-10`).
- Uses `contents: read` (`dockerhub-description.yml:12-13`).
- Resolves Docker Hub metadata credentials from `DOCKERHUB_DESCRIPTION_USERNAME`, `DOCKERHUB_DESCRIPTION_PASSWORD`, and fallback `DOCKERHUB_USERNAME` (`dockerhub-description.yml:23-41`).
- Skips with a step summary if description credentials are missing (`dockerhub-description.yml:30-41`).
- Uses `peter-evans/dockerhub-description` to update Docker Hub repository `linnea7171/xirang` from `README.md` (`dockerhub-description.yml:44-52`).

### Deployment workflow

`xiangnan0811/xirang:.github/workflows/deploy.yml:1-89` is not part of release publication but consumes images:

- Manual only via `workflow_dispatch` with `environment` and `image_tag` inputs (`deploy.yml:3-17`).
- Uses GitHub environment named by input (`deploy.yml:22-27`).
- Logs in to Docker Hub with `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` (`deploy.yml:32-37`).
- Waits for `docker.io/linnea7171/xirang:${IMAGE_TAG}` manifest to exist before deploy (`deploy.yml:39-58`).
- SSH deploys with `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY`, optional port vars/secrets, and `DEPLOY_PATH` (`deploy.yml:60-89`).
- Remote script runs `docker compose pull`, `docker compose up -d`, checks container health, then `docker compose ps` (`deploy.yml:70-89`).

### Token, permissions, secrets, and variables observed

| Name | Kind | Where Used | Purpose |
|---|---|---|---|
| `RELEASE_PLEASE_TOKEN` | secret | `release-please.yml:24` | Auth token for release-please to create/update release PRs and publish tags/releases. Likely avoids limitations of default `GITHUB_TOKEN` around triggering CI from bot commits. |
| `DOCKERHUB_USERNAME` | secret | `publish-images.yml:94-95`, `dockerhub-description.yml:26-31`, `deploy.yml:35-36` | Docker Hub login username; also fallback username for Docker Hub description sync. |
| `DOCKERHUB_TOKEN` | secret | `publish-images.yml:95-96`, `deploy.yml:36-37` | Docker Hub token/password for image push/pull/login. |
| `DOCKERHUB_DESCRIPTION_USERNAME` | secret | `dockerhub-description.yml:27,31` | Optional Docker Hub username for metadata sync. |
| `DOCKERHUB_DESCRIPTION_PASSWORD` | secret | `dockerhub-description.yml:28,32,48-49` | Docker Hub password/token for repository description sync. |
| `CODECOV_TOKEN` | secret | `ci.yml:69-73`, `ci.yml:105-112` | Optional coverage upload token. |
| `DEPLOY_HOST` | secret | `deploy.yml:63` | Private deployment host. |
| `DEPLOY_USER` | secret | `deploy.yml:64` | SSH user for private deployment. |
| `DEPLOY_SSH_KEY` | secret | `deploy.yml:65` | SSH private key for private deployment. |
| `DEPLOY_SSH_PORT` | secret/var fallback | `deploy.yml:66` | SSH port fallback. |
| `DEPLOY_PATH` | secret | `deploy.yml:71` | Remote deployment directory. |
| `IMAGE_WAIT_MAX_ATTEMPTS` | variable | `deploy.yml:29` | Deploy image wait loop max attempts, default 90. |
| `IMAGE_WAIT_INTERVAL_SECONDS` | variable | `deploy.yml:30` | Deploy image wait interval, default 10 seconds. |
| `DEPLOY_SSH_PORT` | variable | `deploy.yml:66` | SSH port preferred over secret fallback. |

Workflow permissions observed:

| Workflow | Permissions |
|---|---|
| `release-please.yml` | `contents: write`, `pull-requests: write` |
| `publish-images.yml` | `contents: read`, `attestations: write`, `id-token: write` |
| `dockerhub-description.yml` | `contents: read` |
| `deploy.yml` | `contents: read` |
| `ci.yml` | `contents: read` |

### What Houfeng should adapt

These are mappings from Xirang’s existing automation shape to Houfeng’s likely release/Docker publishing needs; they are adaptation notes, not a review of Xirang.

| Xirang Pattern | Houfeng Adaptation |
|---|---|
| Separate `Release Please` workflow on `push` to `main`. | Use a separate release-please workflow triggered by `push` to `main`, so normal PR merges update/open a release PR and release PR merges publish GitHub Releases. |
| Use `secrets.RELEASE_PLEASE_TOKEN` instead of default `GITHUB_TOKEN`. | Use a repo secret token if Houfeng needs release-please PR branches/commits to trigger CI under branch protection. Document required token scopes/behavior in workflow or repo setup docs. |
| `contents: write`, `pull-requests: write` only for release-please. | Keep release workflow permissions minimal but sufficient: write contents/releases/tags and PRs there, read-only elsewhere. |
| Single root package with `release-type: simple` and manifest `.` version. | Houfeng is Go + web without a single npm package version authority; `simple` + root manifest can fit if the release version is repo-wide. If binary version stamping later depends on files, add those intentionally rather than copying Xirang’s lack of package updates. |
| `include-v-in-tag: true`. | Matches Houfeng’s existing release-asset naming examples (`VERSION=v1.2.3`) and installer release asset expectations; v-prefixed tags are likely appropriate. |
| Release PR updates `CHANGELOG.md` and `.release-please-manifest.json`. | Houfeng can use the same release PR artifact pattern, with changelog sections adjusted to public Houfeng commit categories. |
| Docker publish workflow triggered by `release.published`. | Connect Docker publishing to GitHub Release publication, not arbitrary pushes, so images correspond to immutable release tags. |
| Manual `workflow_dispatch` rebuild path with `version` and `source_ref`. | Include a manual rebuild path for recovering/publishing a known tag without creating a new release. Ensure it does not move `latest` unless intended. |
| Resolve version by stripping leading `v` and requiring strict semver. | Reuse strict semver validation for Docker tags and release artifacts. Houfeng may need to decide whether prerelease tags like `v1.2.3-rc.1` are supported; Xirang rejects them. |
| Build per-platform digests first, scan, then publish manifest/tags. | Adapt for Houfeng images if multi-arch images are needed. This avoids publishing official tags before scans pass. |
| Use native arm64 runner for arm64 build. | Useful if Houfeng Docker build involves CGO or expensive cross-arch build steps. If no hosted arm64 runner is available, this must be changed. |
| Docker metadata tags `vX.Y.Z`, `X.Y.Z`, and `latest` for stable releases. | Use equivalent tags for Houfeng center/web image if Docker publishing is in scope. Consider whether `latest` should only move on stable GitHub Releases. |
| Provenance attestation with `attestations: write` and `id-token: write`. | Adapt for published Houfeng images if registry/SLSA provenance is desired. |
| Docker Hub secrets isolated to publish/deploy/description workflows. | Keep Docker credentials out of release-please; only image workflows need Docker Hub credentials. |
| Docker Hub description sync separate from release publishing. | Optional for Houfeng; if used, keep it decoupled from image publication and safe to skip when credentials are missing. |
| Private deployment workflow is manual and separate. | If Houfeng has private deployment automation, keep it separate from public release/image publication. |

### What Houfeng should not copy directly

| Xirang Detail | Reason not to copy directly into Houfeng |
|---|---|
| Docker image namespace `docker.io/linnea7171/xirang`. | Houfeng needs its own registry namespace and image name(s). |
| `deploy/allinone/Dockerfile` path and image shape. | Houfeng’s documented boundary is center+web with PostgreSQL; agent remains host systemd. Xirang’s all-in-one nginx+Go+SQLite+supercronic image does not match Houfeng topology. |
| SQLite `/data`, backup volumes, `/backup`, `/logs`, `xirang` container name, port `10761`. | These are Xirang-specific runtime/deploy choices, not Houfeng’s center/PostgreSQL/agent topology. |
| Go/Node versions from Xirang (`golang:1.26.3-alpine`, Node 20 in Dockerfile/workflow). | Houfeng should use versions matching its own `go.mod`, Makefile, web Node 22 guidance, and CI. |
| Xirang changelog section labels with emojis. | Houfeng instructions require avoiding emojis in communication/files unless explicitly requested; changelog section labels should be adapted if that policy applies to repo files. |
| Xirang release manifest version `0.33.1` or web package version `0.1.0`. | Houfeng needs its own initial release version and version source policy. |
| Xirang deploy workflow secrets/SSH path. | Private environment-specific deploy automation should not be copied unless Houfeng has an explicit deploy target and secrets model. |
| Xirang Docker Hub description repository and Chinese short description. | Public metadata must reflect Houfeng’s name, scope, and deployment truth. |
| Xirang prerelease rejection if Houfeng wants RC/nightly publishing. | Xirang rejects prerelease GitHub Releases and validates only `X.Y.Z`; Houfeng should only copy this if prereleases are intentionally out of scope. |
| Xirang no-release-assets pattern. | Houfeng already has agent release asset expectations (`houfeng-agent_<version>_linux_amd64`, `houfeng-agent_<version>_linux_arm64`, `sha256sums.txt`); release automation likely must include binary assets, not just GitHub Release metadata + Docker images. |

### Related Specs

No Houfeng spec files were read for this external repository research. Relevant project guidance already present in `CLAUDE.md` includes:

- Houfeng topology: single Go center + PostgreSQL + outbound systemd agents.
- Docker/Compose boundary memory: containerize center+web with PostgreSQL only; agent remains host systemd install.
- Agent release asset names and checksum verification expectations.

### External References

- `https://github.com/xiangnan0811/xirang` — source repository researched through GitHub API.
- `https://github.com/xiangnan0811/xirang/blob/main/.github/workflows/release-please.yml` — release-please workflow.
- `https://github.com/xiangnan0811/xirang/blob/main/release-please-config.json` — release-please config.
- `https://github.com/xiangnan0811/xirang/blob/main/.release-please-manifest.json` — release manifest.
- `https://github.com/xiangnan0811/xirang/blob/main/.github/workflows/publish-images.yml` — Docker publish workflow.
- `https://github.com/googleapis/release-please-action` — action used by the release workflow.

## Caveats / Not Found

- Repository branch protection settings, required checks, and exact merge method are not visible from repository files/API output used here.
- The exact scopes/identity of `RELEASE_PLEASE_TOKEN` are not visible; only its use as a secret is visible.
- GitHub Release bodies were not expanded in this research; recent release metadata shows no uploaded assets.
- No Docker Hub API checks were performed; registry tag behavior is inferred from workflow definitions.
