# Docker image auto publishing

## Goal

Add GitHub Actions automation so Houfeng release publication builds and publishes the project Docker image to Docker Hub as `linnea7171/houfeng`, making the Docker Compose deployment path usable for future operators without local builds while keeping image publication tied to deliberate releases.

## What I already know

* The current Compose deployment uses `linnea7171/houfeng:latest` and intentionally does not build locally.
* The user proposed referencing https://github.com/xiangnan0811/xirang and publishing to Docker Hub repository https://hub.docker.com/r/linnea7171/houfeng.
* The user corrected the publishing strategy: use Xirang's release-only approach; do not publish `latest` on every `main` push.
* `main` must not be used for local development and must not be directly pushed; all work happens on feature branches and reaches `main` through PR/merge flow.
* The user's deployment concern is future release readiness, not a demand that every `main` merge immediately updates a deployable `latest` image.
* Current `.github/workflows/ci.yml` only runs Go and web verification on pushes to `main` and pull requests.
* The root `Dockerfile` builds a single image containing `houfeng-center`, runtime entrypoint, and baked `web/dist`.
* Current Docker/Compose contract requires service image `linnea7171/houfeng:latest`, no local Compose `build:`, service name `houfeng`, port `16001`, and no agent container.

## Research References

* [`research/xirang-docker-publishing.md`](research/xirang-docker-publishing.md) — Xirang uses Docker Hub token login, semver/latest tags, multi-arch digest-first builds, and release/manual triggers; Houfeng should adapt the Docker action patterns but keep its root Dockerfile and center+web-only boundary.

## Requirements

* Add a GitHub Actions workflow that builds the root `Dockerfile` and pushes `linnea7171/houfeng` to Docker Hub.
* Publish images from deliberate release/manual events, not from every `main` push.
* Support GitHub Release publication as the primary trigger, matching Xirang's release-only publishing model.
* Support manual dispatch for maintainer-controlled rebuilds with explicit version/source ref inputs.
* Push release tags for the Docker image, including `vX.Y.Z` and `X.Y.Z`; update `latest` only for a normal published release, not for PRs or arbitrary main-branch pushes.
* Keep PRs safe: CI should validate Docker image buildability without pushing images or exposing Docker Hub credentials.
* Use GitHub Actions secrets for Docker Hub authentication, expected as `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` unless existing repo conventions require different names.
* Preserve the existing Docker/Compose architecture: center+web image only, PostgreSQL separate, no agent container.
* Document the required GitHub repository secrets and release publishing behavior.
* Preserve branch discipline: local development occurs on `feature/docker-image-auto-publishing` or another feature branch, never on `main`.

## Acceptance Criteria

* [ ] A GitHub Actions workflow exists for Docker image build/publish.
* [ ] Published GitHub releases build and push `linnea7171/houfeng:vX.Y.Z`, `linnea7171/houfeng:X.Y.Z`, and `linnea7171/houfeng:latest` for normal releases.
* [ ] Manual dispatch can rebuild/publish a specified version from a specified source ref without unintentionally updating `latest` unless the workflow explicitly matches the release rule.
* [ ] Pushes to `main` do not publish Docker images.
* [ ] Pull requests do not push Docker images.
* [ ] CI validates the root Dockerfile with a no-push Docker build job.
* [ ] Workflow uses GitHub Actions secrets for Docker Hub authentication and does not commit credentials.
* [ ] Workflow builds the root `Dockerfile`, not a copied Xirang Dockerfile path.
* [ ] Workflow/static checks preserve the Compose deployment boundary: no agent image/service, no local Compose `build:` quick-start.
* [ ] Docs mention the release-only image publishing flow and required secrets.

## Definition of Done

* Tests/checks updated or added where appropriate.
* Existing Go/web verification remains green.
* Docker workflow can be validated by YAML/static checks and, if Docker is available, a build check.
* Docs/spec updated for the durable release publishing contract.
* Rollout and failure behavior considered for missing/invalid Docker Hub secrets.

## Out of Scope

* Publishing Docker images on every `main` push.
* Direct pushes to `main` or local development on `main`.
* Containerizing the agent.
* Kubernetes/Helm deployment.
* Changing `compose.yaml` to build locally.
* Implementing file-based application logging.
* Publishing package-manager artifacts.
* Adding full Release Please automation unless required by the publishing workflow.
* Docker Hub README/description sync unless it falls out as a small, clearly separate follow-up.

## Technical Approach

Use a Xirang-style release-only GitHub Actions workflow adapted to Houfeng:

* Trigger on `release.published` and `workflow_dispatch` with explicit `version` and `source_ref` inputs.
* Validate release/manual versions as strict semver after stripping an optional leading `v`.
* Log in to Docker Hub with `docker/login-action` using `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`.
* Build the root `Dockerfile` as `docker.io/linnea7171/houfeng`.
* Prefer multi-arch publishing for `linux/amd64` and `linux/arm64` if the workflow can follow Xirang's native-runner/digest-first pattern without overcomplicating Houfeng.
* Apply OCI labels for source repository, revision, and version.
* Publish `vX.Y.Z`, `X.Y.Z`, and release-controlled `latest` for normal release publication.
* Avoid any `main` push image publication.

## Decision (ADR-lite)

**Context**: Houfeng now has Compose deployment artifacts that pull `linnea7171/houfeng:latest`, but no image publishing automation. The image should represent deliberate release output, not every merged main commit. Local development and direct pushes on `main` are prohibited.

**Decision**: Use release-only Docker image publishing modeled after Xirang, adapted to Houfeng's root Dockerfile and Docker/Compose boundary.

**Consequences**: Future operators can deploy published releases via Docker Compose once releases exist. `latest` is a release channel, not a continuous-main channel. Main-branch merges can update code/docs without automatically changing the published Docker image.

## Technical Notes

* Files inspected: `Dockerfile`, `compose.yaml`, `.github/workflows/ci.yml`, `.trellis/spec/backend/directory-structure.md`.
* Existing CI is named `ci` and has separate `go` and `web` jobs.
* Docker publishing is an infra integration and requires code-spec depth.
