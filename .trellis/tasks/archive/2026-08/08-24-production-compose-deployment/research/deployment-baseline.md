# Production deployment baseline research

## Current repository facts

- The root image already bundles `houfeng-center`, `houfeng-content-processor`, Poppler, and `web/dist`, runs as UID/GID 10001, and is published as `linnea7171/houfeng` for amd64/arm64.
- Current Compose uses the published image but requires `scripts/compose-up.sh`, two manually created password files, two manually streamed SQL files, and project-named attachment/log volumes. It binds Center to host loopback and does not join a Nginx Proxy Manager network.
- Current release automation uploads agent artifacts and publishes Docker tags, but does not upload deployment assets.
- Records-on startup is not the legacy embedded-migration path. `houfeng-record-platform-admin migrate --scope app` is the only authoritative current APP schema/ACL writer and calls `migrate.ConvergeAppACLCurrent`; Center/processor must authenticate as the direct runtime role and pass current runtime admission.
- Current APP ACL requires three distinct constrained direct-login roles: center runtime, platform admin, and migrator. A fresh production initializer therefore cannot merely create the old single `houfeng` role.
- The existing project image does not include `houfeng-record-platform-admin`, so the release image must add that binary before Compose can run automatic Records-on initialization.
- Current local business state is split between `./data/postgres`, `houfeng_blobs`, and `houfeng_logs`. Named volumes make the documented migration unit incomplete.

## External Compose facts

- Docker Compose supports `depends_on.condition: service_completed_successfully` for one-shot prerequisites.
- Compose supports external networks whose concrete name is provided by configuration; services on the shared external network are reachable by service name/alias.
- Compose secrets are service-scoped. The top-level `secrets.environment` source lets the downloaded `.env` remain the single operator-edited file while exposing each value only to explicitly authorized services. The public deployment contract will declare the minimum supported Docker Compose version and test this syntax.

## Chosen architecture

1. `houfeng-storage-init` uses the published project image with an explicit root one-shot command to create/chown only `./data/attachments` and `./data/logs`; normal project services stay UID/GID 10001.
2. `db` uses pinned PostgreSQL 16 and `./data/postgres`.
3. `houfeng-db-init` uses the same pinned Houfeng image and a dedicated deploy-init command. It receives bootstrap/runtime/admin/migrator secrets, performs exact pre-provisioning and constrained role setup, then invokes current APP convergence through the direct migrator.
4. Center and processor wait for both one-shot services to complete successfully. They receive only the runtime database secret; Center alone receives initial-admin/session secrets.
5. ClamAV remains required and its signature cache is bound below `./data/clamav`.
6. Center joins both the private application network and the configured existing NPM network. Only Center joins the proxy network; no host port is published by default.
7. Release automation generates an env asset pinned to the release tag and uploads it alongside the version-matched Compose asset.

## Rejected approaches

- Manual SQL or a host-side launcher: violates the ordinary-user startup requirement and couples deployment to repository files.
- `build: .`: violates prebuilt-image distribution and host migration requirements.
- Giving Center bootstrap authority: violates steady-state least privilege and current Records admission contracts.
- Passing one `.env` wholesale to every service: leaks Center-only and migration secrets to the processor.
- Keeping business attachments/logs in named volumes: leaves the migration unit split across opaque Docker state.
- Adding Caddy: conflicts with the selected Nginx Proxy Manager edge.

