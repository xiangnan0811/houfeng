# Docker Compose deployment

## Goal

Add a Docker/Compose deployment path that lets an operator quickly run Houfeng center + web + PostgreSQL using one project image, one database image, and a small set of environment variables, while preserving the existing architecture where agents run on real Linux/systemd hosts and enroll through center-generated install commands.

## What I already know

* The deployment should containerize only center + built web assets and PostgreSQL.
* The project image should contain `houfeng-center` and `web/dist` together.
* Agents must not be containerized; they continue to depend on real host Linux/systemd environment.
* External reverse proxy/TLS is user-managed via Caddy, Nginx Proxy Manager, Nginx, cloud load balancer, or similar.
* Deployment configuration should be minimal; non-essential settings should be changed after login when the system supports that.
* Current center startup requires `HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, and `HOUFENG_INITIAL_PASSWORD`.
* `HOUFENG_HTTP_ADDR` defaults to `:8080`; container deployment can set it explicitly.
* `HOUFENG_WEB_DIST_DIR` defaults to `web/dist`; container deployment should point it at the image-baked SPA path.
* `HOUFENG_PUBLIC_BASE_URL` is optional for startup but required for generated one-command agent install commands.
* Telegram variables are optional and should not be part of the minimal quick-start configuration.

## Requirements

* Provide a Docker image definition for the project service that builds the React SPA and Go center binary, then runs only `houfeng-center` in the runtime image.
* Serve the baked SPA from a fixed container path via `HOUFENG_WEB_DIST_DIR`.
* Provide a Docker Compose deployment with exactly two required services for MVP: project service and PostgreSQL.
* The published Compose file must not build the project image locally; it must reference the placeholder Docker Hub image `linnea7171/houfeng:latest` until automated image publishing is added.
* The Docker/Compose default Houfeng service port must be `16001` internally and externally; operators can override the host port if needed.
* The Compose project service must be named `houfeng`, not `center`.
* Deployment data should be exposed through user-migratable host directories rather than opaque volumes where practical.
* File-based service logging is required for future troubleshooting and user feedback; if not implemented in this task, it must be documented as a required follow-up, not optional.
* Persist PostgreSQL data in a user-migratable host directory by default.
* Keep agent deployment out of Docker/Compose and document that agents must be installed on target hosts through center-generated onboarding commands.
* Keep reverse proxy/TLS out of the Compose stack and document that the user must provide it for production/public deployments.
* Keep the default env surface small and avoid requiring Telegram, retention, session, incident, or agent-specific variables for first boot.
* Make `HOUFENG_PUBLIC_BASE_URL` visible in the deployment template and document it as optional for first login but required before one-command agent onboarding.

## Open Questions

* None.

## Acceptance Criteria

* [ ] A fresh operator can run center + web + PostgreSQL with Docker Compose and a small `.env` file.
* [ ] The Compose file references `linnea7171/houfeng:latest` and does not contain a `build:` block for the project service.
* [ ] The Compose project service is named `houfeng` and maps default host port `16001` to container port `16001`.
* [ ] PostgreSQL data is exposed through a user-migratable host directory by default.
* [ ] Docs explicitly state file-based Houfeng logs are a required follow-up if the current app still logs only to stdout/stderr.
* [ ] The project container serves the built Web UI without requiring a host-mounted `web/dist`.
* [ ] PostgreSQL data survives container recreation through the default `./data/postgres/` host directory.
* [ ] The Compose path does not introduce containerized agents or agent-side Docker deployment docs.
* [ ] Docs clearly state that production/public access requires user-managed HTTPS reverse proxy.
* [ ] Docs clearly state that `HOUFENG_PUBLIC_BASE_URL` must be an externally reachable URL for one-command agent install generation.
* [ ] Existing Go and web verification still pass, or any failures are explained and fixed.

## Definition of Done

* Docker/Compose artifacts are added or updated.
* Minimal environment template is added or existing examples are updated without bloating first-boot configuration.
* Active deployment docs are updated truthfully without reviving obsolete roadmap/process documents.
* Build/verification checks are run for changed areas.
* Rollout and rollback expectations are documented where relevant.

## Technical Approach

Build a multi-stage Docker image definition for the project image: use a Node 22 stage to build `web/dist`, a Go stage to build `houfeng-center` with a configurable `VERSION`, and a small runtime stage containing the center binary plus baked web assets. The published Compose file should not build that image locally; it should reference `linnea7171/houfeng:latest` as the project image placeholder, run it as service `houfeng` alongside PostgreSQL, pass the database URL through env, expose port `16001` internally and externally by default for local/reverse-proxy access, and persist database state in a user-migratable host data directory. File-based Houfeng logs are a required follow-up if application logging remains stdout/stderr-only in this task.

## Decision (ADR-lite)

**Context**: The project needs a faster deployment path, but the current architecture intentionally relies on real host agents and a single center + PostgreSQL topology.

**Decision**: Docker/Compose will package only center + web and PostgreSQL. Agent installation remains host-level Linux/systemd via center-generated commands. Reverse proxy/TLS remains user-managed infrastructure. `HOUFENG_PUBLIC_BASE_URL` appears in the quick-start env template, but is documented as optional for initial login and required before one-command agent onboarding.

**Consequences**: The quick-start path becomes simpler for the center, but operators still need to configure an externally reachable `HOUFENG_PUBLIC_BASE_URL`, release-matched agent assets, and HTTPS reverse proxy before production agent onboarding is complete.

## Out of Scope

* Containerized agent deployment.
* Docker/Kubernetes agent orchestration.
* Bundling Caddy, Nginx, Nginx Proxy Manager, or TLS certificate automation into the MVP Compose stack.
* Package-manager distribution.
* Automatic agent upgrade.
* Center-hosted agent binary mirror unless a later architecture decision changes the installer contract.
* Automated Docker image publishing/release workflow; this is a required follow-up after the placeholder image reference is introduced.
* Full application file logging implementation if not completed in this task; this remains required for user troubleshooting.

## Technical Notes

* `Makefile` builds center with `make build-center VERSION=...` and web with `cd web && npm ci && npm run build`.
* `web/package.json` requires Node 22.x.
* `internal/center/config/config.go` currently requires `HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, and `HOUFENG_INITIAL_PASSWORD` for center startup.
* `docs/deploy/local-and-systemd.md` is the active deployment guide and already documents HTTPS, auth seed vars, public base URL, and agent one-command install boundaries.
* No Dockerfile, Compose file, or `.dockerignore` existed before this task.
* Research reference: `research/docker-compose-conventions.md` maps Docker/Compose conventions to this repo's center+web image, PostgreSQL volume, external proxy, and non-containerized agent boundary.
