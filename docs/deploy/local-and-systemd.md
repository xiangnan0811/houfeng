# Houfeng local, Docker Compose, and systemd deployment guide

> Authentication note: the center requires a username + password login for
> every API call except `/api/healthz` and `/api/agent/*`.
> First-startup credentials come from `HOUFENG_INITIAL_USERNAME` and
> `HOUFENG_INITIAL_PASSWORD` or `HOUFENG_INITIAL_PASSWORD_FILE`; once the
> `users` table is populated those variables are ignored. Session cookies are
> `__Host-` scoped, Secure, HttpOnly, and SameSite=Strict; the deployment
> **must** terminate HTTPS at a reverse proxy for authenticated browser use.
> See the **Authentication** section below.

## Scope

This guide describes the current deployment paths for `候风 / Houfeng Fleet Control Plane`: one Go center process serving API + web UI, one isolated attachment content-processor process, one required ClamAV scanner, one PostgreSQL database, and one or more Go agents managed by systemd on the monitored hosts. The center, processor, scanner, and database can run directly on the host or in the provided Docker Compose stack; agents are not containerized.

This repository does not currently document package-manager repositories, Kubernetes deployment, automatic upgrades, or non-systemd agent installation.

## Build artifacts

From the repository root:

```bash
make build-center VERSION=v1.2.3
go build -trimpath -o bin/houfeng-content-processor ./cmd/houfeng-content-processor
make build-agent
cd web && npm ci && npm run build
```

Expected local outputs:

- `bin/houfeng-center` stamped with the release version used by generated install commands
- `bin/houfeng-content-processor`
- `bin/houfeng-agent`
- `web/dist/`

For one-command agent installation, each published GitHub Release must contain Linux agent artifacts, the checksum manifest, and a detached minisign signature for that manifest. The release workflow builds, signs, and uploads them automatically on `release.published`; keep the local target for sanity checks and emergency backfills:

```bash
make build-agent-release VERSION=v1.2.3
```

Expected release outputs under `dist/`:

- `houfeng-agent_v1.2.3_linux_amd64`
- `houfeng-agent_v1.2.3_linux_arm64`
- `sha256sums.txt`
- `sha256sums.txt.minisig`

`build-agent-release` stamps the agent heartbeat version with the same `VERSION` value used in the artifact names. The center-served installer script is fetched from the deployed center; GitHub Release is only used for these binary and signed-checksum assets. Maintainers must configure `HOUFENG_RELEASE_MINISIGN_PRIVATE_KEY` in GitHub Secrets with the secret key matching the installer-pinned public key before publishing installable agent assets. If the key is encrypted, also set `HOUFENG_RELEASE_MINISIGN_PASSWORD`. Target hosts need `minisign` to verify the signed checksum manifest. The generated command includes `--install-missing-deps`, so if `minisign` is absent the installer downloads the pinned upstream static verifier, checks its SHA256, installs it to `/usr/local/bin/minisign`, and only then verifies Houfeng release assets.

## PostgreSQL pre-R1 provisioning

Every target PostgreSQL database must be provisioned immediately after it is
created and before R1 migrations or any APP credential is used. Connect as the
bootstrap superuser or the owner of `pg_catalog.pg_control_system()` and run:

```bash
psql -X -v ON_ERROR_STOP=1 --dbname "$BOOTSTRAP_DATABASE_URL" \
  --file docs/deploy/app-acl-r2-pre-r1-provisioning.sql
```

`BOOTSTRAP_DATABASE_URL` must authenticate a principal distinct from the
application principal in `HOUFENG_DATABASE_URL`. The SQL verifies that its
current actor is a superuser or the function owner; naming an application role
in the command is not sufficient. Keep the bootstrap password in the database
client's secret mechanism or a mode-0600 file rather than a command argument or
tracked environment file.

The provisioning transaction resolves the zero-argument function by catalog
signature, requires owner OID 10, revokes PUBLIC `EXECUTE`, and verifies the
resulting explicit owner-only ACL. Stock PostgreSQL 16 grants PUBLIC
`EXECUTE`; an unprovisioned database is therefore intentionally rejected by
APP ACL R2 bootstrap. Neither application migrations nor the R2 bootstrap
repairs this privilege. Repeat this step for every newly created target
database before distributing direct-migrator, runtime, or admin credentials.

## Center environment

Minimum `/etc/houfeng/center.env`:

```dotenv
HOUFENG_HTTP_ADDR=:8080
HOUFENG_WEB_DIST_DIR=/opt/houfeng/web/dist
HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@127.0.0.1:5432/houfeng?sslmode=disable
HOUFENG_DATABASE_REQUIRE_TLS=false
HOUFENG_PUBLIC_BASE_URL=https://center.example.com
HOUFENG_INCIDENT_SWEEP_INTERVAL=5s
HOUFENG_LOG_FILE=/var/log/houfeng/center.log
HOUFENG_INITIAL_USERNAME=admin
HOUFENG_INITIAL_PASSWORD=replace-me-with-a-real-password
HOUFENG_SESSION_HMAC_KEY=replace-me-with-32-plus-random-bytes
HOUFENG_PASSWORD_BCRYPT_COST=10
HOUFENG_TRUSTED_PROXIES=
HOUFENG_ATTACHMENT_BLOB_BACKEND=local
HOUFENG_ATTACHMENT_BLOB_ROOT=/var/lib/houfeng/attachments
HOUFENG_CLAMAV_NETWORK=tcp
HOUFENG_CLAMAV_ADDRESS=127.0.0.1:3310
HOUFENG_CLAMAV_DIAL_TIMEOUT=5s
HOUFENG_CLAMAV_OPERATION_TIMEOUT=2m
HOUFENG_CLAMAV_CHUNK_SIZE=65536
HOUFENG_CLAMAV_RESPONSE_LIMIT=4096
HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS=3
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=
```

`HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, an initial password source, and `HOUFENG_SESSION_HMAC_KEY` are required for center startup. Use `HOUFENG_INITIAL_PASSWORD_FILE` instead of `HOUFENG_INITIAL_PASSWORD` when the password is provided by a file or secret mount; the file value takes precedence. Use `HOUFENG_SESSION_HMAC_KEY_FILE` instead of `HOUFENG_SESSION_HMAC_KEY` when the session HMAC secret is provided by a file or secret mount; the file value takes precedence. The session HMAC secret must be at least 32 bytes and must stay stable across restarts; rotating it invalidates existing browser sessions and any agent enrollment/sync token hashes already migrated to the HMAC format. `HOUFENG_PUBLIC_BASE_URL` is optional for center startup, but required to generate one-command install commands and to enable production Host allowlisting. Set it to the externally reachable absolute `http(s)` URL that target agents can access, for example `https://center.example.com` or `http://203.0.113.10:8080`; it must not include query or fragment, and the center normalizes trailing slashes when generating commands. `HOUFENG_TRUSTED_PROXIES` is optional and should only list the exact reverse-proxy CIDRs that are allowed to supply forwarded client IP headers; `0.0.0.0/0` and `::/0` are rejected. `HOUFENG_DATABASE_REQUIRE_TLS=true` makes startup reject database URLs without `sslmode=require`, `sslmode=verify-ca`, or `sslmode=verify-full`; enable it for external/production PostgreSQL. `HOUFENG_PASSWORD_BCRYPT_COST` defaults to Go bcrypt's default cost and can be raised after benchmarking login and password-change latency on the target host. `HOUFENG_LOG_FILE` is optional; when set, center writes application logs to both stdout and the configured file, and startup fails if the file cannot be opened. Telegram is disabled unless both Telegram values are set. Use `.env.example` as the full local variable inventory; the systemd snippets below are deployment-shaped examples and must include the required auth seed vars on first startup.

Attachment storage has no implicit backend when Records runtime admission is enabled. `local` requires an absolute private directory that is neither `/` nor another broad system path. S3 deployments set `HOUFENG_ATTACHMENT_BLOB_BACKEND=s3`, endpoint, bucket, and TLS mode, and load credentials through `HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE` and `HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE`; do not put credential values in the environment file. Center and processor must use the same immutable Blob backend and scanner settings.

> Note: `HOUFENG_WEB_DIST_DIR` defaults to `web/dist`, but the configured directory must exist and contain the built SPA; otherwise center `/` returns 404 and the UI is unavailable. Production deployments should point it at the installed `web/dist` path.

## Local center run

```bash
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
export HOUFENG_DATABASE_REQUIRE_TLS=false
export HOUFENG_PUBLIC_BASE_URL='http://localhost:8080'
export HOUFENG_INITIAL_USERNAME=admin
export HOUFENG_INITIAL_PASSWORD='replace-me-with-a-real-password'
export HOUFENG_SESSION_HMAC_KEY='replace-me-with-32-plus-random-bytes'
make build-center
./bin/houfeng-center
```

The center applies embedded migrations on startup and serves API plus the built web UI. The default center version is `dev`, so one-command install generation returns a configuration error instead of pointing at nonexistent release assets. For production one-command installs, build `houfeng-center` with a real release version and publish matching agent assets so generated install commands point at a real release.

## Docker Compose deployment

The Compose path runs the center and built web UI, the isolated content processor, a pinned ClamAV daemon, and PostgreSQL. It is a development/conformance topology for exercising the complete attachment path on one Docker host. It is not an independent production recovery domain: the named Blob volume and the PostgreSQL bind-mounted data directory share one host failure boundary. It does not run agents in containers; monitored hosts still use the center-generated Linux/systemd onboarding command reached after creating a VPS and its scoped MonitoringInstance from the VPS detail page.

Prerequisites: Docker with Compose support, and an operator-managed HTTPS reverse proxy for production/public access.

```bash
cp docs/deploy/compose.env.example docs/deploy/compose.env
install -d -m 700 docs/deploy/secrets
umask 077
openssl rand -hex 32 > docs/deploy/secrets/postgres-bootstrap-password
openssl rand -hex 32 > docs/deploy/secrets/houfeng-database-password
# edit docs/deploy/compose.env and replace the admin password and session HMAC key
# optionally set HOUFENG_PUBLIC_BASE_URL before agent onboarding
set -eu
scripts/compose-up.sh docs/deploy/compose.env
```

The tracked env example names two different catalog principals:
`POSTGRES_BOOTSTRAP_USER` initializes the PostgreSQL OID-10 superuser and
`HOUFENG_DATABASE_USER` is the application login. Their passwords live in two
untracked mode-0600 files. Compose mounts the bootstrap password only into the
database container; the center and processor containers receive only the
application password file. The processor does not receive center-only initial
admin or session HMAC secrets.

`scripts/compose-up.sh` is the fail-stop quick-start boundary. It validates the
Compose configuration, starts only `db`, polls `pg_isready`, requires a
successful `SELECT 1` against the configured target database as the bootstrap
principal, and rejects equal bootstrap/application names. Only after that
database-level readiness proof does it run
`docs/deploy/app-acl-r2-pre-r1-provisioning.sql`; it then creates or updates the
constrained application login and transfers the fresh database to that login.
Any readiness, identity, provisioning, or application-role error exits nonzero
before `docker compose ... up -d houfeng houfeng-content-processor` is invoked.

An existing application role is accepted only when it has no direct role
membership in either direction; that also excludes every recursive membership
path involving the role. `NOINHERIT` is not a substitute because membership can
still authorize `SET ROLE`. Membership drift is rejected without cleanup: the
application-role transaction rolls back, database ownership stays unchanged,
and Houfeng is not started.

The principal values are consumed by PostgreSQL only during initial database
creation. A data directory created by an older Compose layout with `houfeng` as
the OID-10 superuser cannot be made compliant by changing the env file. Back it
up and initialize a fresh database with this sequence, or use a separately
reviewed migration plan; do not rename or repurpose the existing OID-10 role in
place.

The default Compose file pulls and runs `linnea7171/houfeng:latest`. The project image contains `houfeng-center`, `houfeng-content-processor`, Poppler tools, a small runtime entrypoint, and baked `web/dist`; both project services run as the non-root `houfeng` user. The center uses `HOUFENG_HTTP_ADDR=:16001`, `HOUFENG_WEB_DIST_DIR=/app/web/dist`, and `HOUFENG_LOG_FILE=/var/log/houfeng/center.log`, so no host-mounted `web/dist` directory is required. The processor runs with a read-only root filesystem, all capabilities dropped, `no-new-privileges`, core dumps disabled, a bounded private tmpfs workspace, and the same persistent Blob volume as center. When `HOUFENG_DATABASE_URL` is unset, the entrypoint assembles it from the application user, password-file value, and database name loaded from `docs/deploy/compose.env`. It percent-encodes each component as UTF-8 bytes, so printable URI-reserved characters in strong passwords and catalog names are preserved; it rejects ASCII control bytes and never executes either binary on fallback-input failure. The password file takes precedence over `HOUFENG_DATABASE_PASSWORD`. An explicitly supplied `HOUFENG_DATABASE_URL` bypasses fallback assembly unchanged and remains subject to normal URL/TLS validation. Center-only initial-user and session secrets are validated only for the center command and are not mounted into or required by the processor. The entrypoint does not perform runtime privilege dropping. The root `Dockerfile` is published by the release-only Docker image workflow; the default quick-start still pulls the published image and does not build locally.

Sensitive Compose values remain untracked. The database bootstrap and application passwords live under the ignored `docs/deploy/secrets/` directory and are mounted as service-scoped Docker secrets; the initial admin password and session HMAC key live in the untracked `docs/deploy/compose.env` copied from `docs/deploy/compose.env.example`. The tracked `compose.yaml` contains only secret-file mount paths and intentionally avoids password values or database URLs so repository secret scanners do not flag placeholder deployment configuration.

Maintainers publish Docker images and installer-required agent assets through the release pipeline. Configure GitHub repository secrets `RELEASE_PLEASE_TOKEN`, `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`, and `HOUFENG_RELEASE_MINISIGN_PRIVATE_KEY`; if the minisign key is password-protected, also configure `HOUFENG_RELEASE_MINISIGN_PASSWORD`. After an eligible conventional feature/fix/docs PR merges to `main`, `.github/workflows/release-please.yml` opens or updates a release PR. When that release PR passes CI and is merged, Release Please publishes a GitHub Release such as `v1.2.3`; the `release.published` event then runs `.github/workflows/publish-images.yml`, uploads `houfeng-agent_v1.2.3_linux_amd64`, `houfeng-agent_v1.2.3_linux_arm64`, `sha256sums.txt`, and `sha256sums.txt.minisig` to the release, and pushes `linnea7171/houfeng:v1.2.3`, `linnea7171/houfeng:1.2.3`, and `linnea7171/houfeng:latest`. Manual workflow dispatch requires explicit `version` and `source_ref` inputs, can rebuild/upload those agent assets for emergency backfills, and does not update the Docker `latest` tag.

`compose.yaml` starts four required services:

- `houfeng` — the Houfeng project image, bound by default to `127.0.0.1:16001` on the host for a local reverse proxy upstream. Override only the host port with `HOUFENG_HOST_PORT=<port>` in `docs/deploy/compose.env` if needed.
- `houfeng-content-processor` — the same project image with the processor command, isolated workspace, explicit retry/reconciliation bounds, shared Blob volume, and no center-only admin/session secrets.
- `clamav` — the pinned ClamAV daemon and persistent signature database used by both admission readiness and processor scanning.
- `db` — PostgreSQL initialized under `POSTGRES_BOOTSTRAP_USER`, with a separate constrained `HOUFENG_DATABASE_USER`, and the user-migratable host directory `./data/postgres/` mounted at `/var/lib/postgresql/data`.

The Houfeng service writes center application logs to `/var/log/houfeng/center.log` inside the container, backed by the `houfeng_logs` named Docker volume. It still emits logs to stdout so `docker compose logs houfeng` remains the primary quick troubleshooting path. The processor emits content-free lifecycle/errors to stdout; inspect it with `docker compose logs houfeng-content-processor`. To inspect the center log file directly, use a temporary container with the named volume mounted instead of relying on a host bind path.

PostgreSQL has a `pg_isready` healthcheck using the bootstrap identity, ClamAV has a daemon readiness check, and both project services retain `service_healthy` dependencies. The quick-start additionally waits for database readiness before provisioning and does not request either project service until provisioning and application-role setup have both succeeded. The center then applies embedded migrations as the application principal on startup.

For local Blob storage, PostgreSQL metadata and `houfeng_blobs` are one logical recovery point. Quiesce center and processor writes, capture both stores within the same backup window, and restore both before serving traffic; a database-only or Blob-only snapshot is incomplete. Copying a named volume beside `./data/postgres/` does not create an independent production recovery domain. Production S3 deployments still require a coordinated database/object manifest and exact-version verification; the full cross-storage recovery controller belongs to the later recovery task.

`HOUFENG_PUBLIC_BASE_URL` in `docs/deploy/compose.env` may be empty for first login. Before generating one-command agent install commands, set it to the externally reachable absolute `http(s)` URL that browsers and target agents can access, then recreate the Houfeng container:

```bash
docker compose --env-file docs/deploy/compose.env up -d houfeng
```

For production/public deployments, terminate HTTPS outside this Compose stack with Caddy, Nginx Proxy Manager, Nginx, a cloud load balancer, or similar, and forward to the loopback-bound Houfeng port. Configure `HOUFENG_PUBLIC_BASE_URL` to the external origin so Host allowlisting rejects mismatched hosts, set `HOUFENG_TRUSTED_PROXIES` only to the reverse proxy CIDRs that should be trusted, and apply request body, rate, and connection limits at the proxy. Do not expose the center directly on public plain HTTP. If one-command agent onboarding is needed, publish a GitHub Release so the release workflow builds `linnea7171/houfeng:vX.Y.Z`, `linnea7171/houfeng:X.Y.Z`, release-controlled `linnea7171/houfeng:latest`, and the matching signed Linux agent release assets described above. For controlled production rollouts, pin a tested image tag or digest instead of relying only on mutable `latest`.

For troubleshooting, collect recent `docker compose --env-file docs/deploy/compose.env logs --tail=100 houfeng` output plus `/var/log/houfeng/center.log` from the `houfeng_logs` named volume when file logs are needed. Do not paste enrollment commands, tokens, cookies, passwords, or provider credentials into shared logs or issues.

Rollback/removal expectations:

- `docker compose down` stops and removes the center, processor, ClamAV, and database containers but keeps PostgreSQL files under `./data/postgres/` and keeps the named Blob/signature/log volumes unless volumes are explicitly requested for removal.
- Delete `./data/postgres/` or the `houfeng_blobs` volume only when intentionally discarding the complete deployment. Back up or move PostgreSQL and Blob data as the same recovery point when migrating to another host.

## Agent environment

Minimum `/etc/houfeng-agent/agent.env`:

```dotenv
HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=65536
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
HOUFENG_AGENT_BUFFER_MAX_BYTES=67108864
```

The token file initially contains an enrollment token issued from the MonitoringInstance onboarding workflow. After the first successful enrollment, `houfeng-agent` replaces it with post-enrollment sync credentials for that MonitoringInstance so service restarts do not reuse the consumed enrollment token. In normal deployments, create or open the VPS first, use **创建并接入 agent** from the VPS detail page, and run the generated one-command installer instead of manually writing this file.

Run the agent under the dedicated `houfeng-agent` system user created by the installer. Do not add that user to privileged groups such as `docker` unless the operator accepts the host-control risk. Diagnostic command output is redacted by the agent before upload and by the center before persistence, but redaction is best effort and the remaining text can still reveal operational details such as service names, disk layout, process state, and recent kernel or systemd messages.

## Local agent run

```bash
export HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080
export HOUFENG_AGENT_TOKEN_FILE=/tmp/houfeng-agent-token
install -d -m 0700 /tmp/houfeng-agent
export HOUFENG_AGENT_BUFFER_FILE=/tmp/houfeng-agent/sync-buffer.json
make build-agent
./bin/houfeng-agent
```

Use a private agent state directory for the sync buffer. A buffer file placed directly under `/tmp` can fail on hosts where the agent cannot tighten permissions on the shared `/tmp` directory.

## systemd installation example

Install `clamav-daemon` and `poppler-utils` from the target distribution first. Configure ClamAV to listen only on `127.0.0.1:3310` (or adapt both environment files to a private Unix socket that the `houfeng` user can access), update signatures, and prove the daemon is healthy before starting Houfeng. Center and processor intentionally run as the same unprivileged `houfeng` user so they can access one private Blob tree; the monitored-host agent remains the separate `houfeng-agent` user.

Create the service user and install the center/processor runtime files:

```bash
getent passwd houfeng >/dev/null || sudo useradd --system --home-dir /opt/houfeng --shell /usr/sbin/nologin houfeng
sudo install -d -o houfeng -g houfeng -m 0755 /opt/houfeng
sudo install -d -o houfeng -g houfeng -m 0755 /opt/houfeng/web
sudo install -d -o houfeng -g houfeng -m 0755 /opt/houfeng/docs
sudo install -d -o houfeng -g houfeng -m 0750 /var/log/houfeng
sudo install -d -o houfeng -g houfeng -m 0700 /var/lib/houfeng/attachments
sudo cp -a web/dist /opt/houfeng/web/
sudo cp -a docs/deploy /opt/houfeng/docs/
sudo install -o root -g root -m 0755 bin/houfeng-center /usr/local/bin/houfeng-center
sudo install -o root -g root -m 0755 bin/houfeng-content-processor /usr/local/bin/houfeng-content-processor
sudo install -d -o root -g houfeng -m 0750 /etc/houfeng
sudo tee /etc/houfeng/center.env >/dev/null <<'EOF'
HOUFENG_HTTP_ADDR=:8080
HOUFENG_WEB_DIST_DIR=/opt/houfeng/web/dist
HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@127.0.0.1:5432/houfeng?sslmode=disable
HOUFENG_DATABASE_REQUIRE_TLS=false
HOUFENG_PUBLIC_BASE_URL=https://center.example.com
HOUFENG_INCIDENT_SWEEP_INTERVAL=5s
HOUFENG_LOG_FILE=/var/log/houfeng/center.log
HOUFENG_INITIAL_USERNAME=admin
HOUFENG_INITIAL_PASSWORD=replace-me-with-a-real-password
HOUFENG_SESSION_HMAC_KEY=replace-me-with-32-plus-random-bytes
HOUFENG_PASSWORD_BCRYPT_COST=10
HOUFENG_ATTACHMENT_BLOB_BACKEND=local
HOUFENG_ATTACHMENT_BLOB_ROOT=/var/lib/houfeng/attachments
HOUFENG_CLAMAV_NETWORK=tcp
HOUFENG_CLAMAV_ADDRESS=127.0.0.1:3310
HOUFENG_CLAMAV_DIAL_TIMEOUT=5s
HOUFENG_CLAMAV_OPERATION_TIMEOUT=2m
HOUFENG_CLAMAV_CHUNK_SIZE=65536
HOUFENG_CLAMAV_RESPONSE_LIMIT=4096
HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS=3
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=
EOF
sudo tee /etc/houfeng/content-processor.env >/dev/null <<'EOF'
HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@127.0.0.1:5432/houfeng?sslmode=disable
HOUFENG_ATTACHMENT_BLOB_BACKEND=local
HOUFENG_ATTACHMENT_BLOB_ROOT=/var/lib/houfeng/attachments
HOUFENG_CLAMAV_NETWORK=tcp
HOUFENG_CLAMAV_ADDRESS=127.0.0.1:3310
HOUFENG_CLAMAV_DIAL_TIMEOUT=5s
HOUFENG_CLAMAV_OPERATION_TIMEOUT=2m
HOUFENG_CLAMAV_CHUNK_SIZE=65536
HOUFENG_CLAMAV_RESPONSE_LIMIT=4096
HOUFENG_CONTENT_PROCESSOR_WORKSPACE_ROOT=/run/houfeng-content-processor
HOUFENG_CONTENT_PROCESSOR_OWNER_ID=systemd-processor
HOUFENG_CONTENT_PROCESSOR_LEASE_DURATION=5m
HOUFENG_CONTENT_PROCESSOR_CLEANUP_TIMEOUT=30s
HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_ITEMS=100
HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_MAX_RUNTIME=30s
HOUFENG_CONTENT_PROCESSOR_RECONCILIATION_RETRY_DELAY=1s
HOUFENG_CONTENT_PROCESSOR_MAX_ATTEMPTS=3
HOUFENG_CONTENT_PROCESSOR_JOB_TTL=24h
HOUFENG_PDFINFO_BINARY=/usr/bin/pdfinfo
HOUFENG_PDFTOPPM_BINARY=/usr/bin/pdftoppm
EOF
sudo chown root:houfeng /etc/houfeng/center.env
sudo chown root:houfeng /etc/houfeng/content-processor.env
sudo chmod 0640 /etc/houfeng/center.env /etc/houfeng/content-processor.env
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-center.service /etc/systemd/system/houfeng-center.service
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-content-processor.service /etc/systemd/system/houfeng-content-processor.service
sudo systemctl daemon-reload
sudo systemctl enable --now houfeng-center houfeng-content-processor
```

For S3, replace the local backend lines in both environment files with the same endpoint/bucket/TLS settings and private `HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE` / `HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE` paths. Grant the `houfeng` user read access to those mode-restricted files; never put S3 credentials directly in a tracked unit or environment example. The processor unit creates its private `0700` runtime workspace, sets `LimitCORE=0`, and grants writes only to that workspace and the durable Blob path.

Then create or open the VPS in the web UI and use **创建并接入 agent** from the VPS detail page. That action creates the scoped MonitoringInstance and opens its onboarding workspace. The primary path is the generated one-command installer. During early development, the same generated command is also the accepted agent upgrade path for an already-bound systemd monitoring instance:

1. Click **生成一键安装命令**.
2. Copy the command shown by the center.
3. Run it on the target Linux systemd host with root privileges or a sudo-capable account.

The generated command has this shape:

```sh
tmp_installer="$(mktemp)" && curl -fsSL 'https://center.example.com/api/agent/install.sh' -o "$tmp_installer" && sudo sh "$tmp_installer" --server-url 'https://center.example.com' --enrollment-token-stdin --install-missing-deps --version 'v1.2.3' --release-repo 'xiangnan0811/houfeng' <<'HOUFENG_ENROLLMENT_TOKEN'
<token>
HOUFENG_ENROLLMENT_TOKEN
status=$?; rm -f "$tmp_installer"; test "$status" -eq 0
```

Important behavior:

- The command is generated by the center from `HOUFENG_PUBLIC_BASE_URL`; the browser must not guess the production URL from its current origin.
- `POST /api/monitoring-instances/{monitoring_instance_id}/install-command` issues a fresh 30-minute one-time enrollment token. Regenerating a command invalidates the previous active token for that MonitoringInstance.
- If the center was built with the placeholder `dev` version or without `HOUFENG_PUBLIC_BASE_URL`, command generation returns a configuration error instead of guessing.
- `/api/agent/install.sh` is a public, read-only script route served by the deployed center. It contains no deployment-specific token until the generated command feeds a token through `--enrollment-token-stdin` at execution time.
- GitHub Release hosts only `houfeng-agent_<version>_linux_amd64`, `houfeng-agent_<version>_linux_arm64`, `sha256sums.txt`, and `sha256sums.txt.minisig`; the install script is not taken from GitHub raw/release assets.
- The generated command grants explicit consent for missing dependency recovery with `--install-missing-deps`. Manual installer runs can omit that flag to get an interactive prompt, or pass `--no-install-missing-deps` to fail if `minisign` is missing.
- Treat the generated command as a secret: do not paste it into tickets, chat, screenshots, process logs, or shell transcripts you plan to share.

The installer supports Linux systemd hosts on `amd64` and `arm64`. It fails before writing runtime files when the OS, architecture, service manager, downloader, or checksum tools are unsupported. If `minisign` is missing, the generated command allows the installer to download upstream `minisign` 0.12, verify the pinned tarball SHA256, and install the matching static binary to `/usr/local/bin/minisign`; if the operator declines or disables dependency recovery, the installer stops before changing agent files. It downloads the selected release asset, `sha256sums.txt`, and `sha256sums.txt.minisig` from the configured release repository, verifies the checksum manifest signature with the installer-pinned public key, verifies the exact checksum entry, then replaces `/usr/local/bin/houfeng-agent` and changes systemd state. It writes `/etc/houfeng-agent/agent.env`, writes `/etc/houfeng-agent/token` with mode `0600` when the file does not already contain post-enrollment sync credentials, creates `/var/lib/houfeng-agent`, installs the systemd unit, runs `systemctl daemon-reload`, enables the service, then restarts an already-active `houfeng-agent` or starts it when inactive. Re-running the command on a bound node preserves the existing sync credentials and activates the newly installed agent binary without requiring a separate manual restart.

If an old command fails with `houfeng-agent install: minisign is required to verify release checksums`, first inspect the copied command. Commands generated by `v0.55.0` do not contain `--install-missing-deps`, and the `v0.55.0` installer stops immediately when `minisign` is missing. This is especially visible on Debian 11 bullseye and older Ubuntu hosts where `apt install minisign` may return `E: Unable to locate package minisign`. This is not a reason to disable signature verification or edit release checksum files.

Use the fixed-center recovery path:

1. Discard the old generated command and its one-time enrollment token.
2. Upgrade the center to `v0.55.1` or newer, preferably the latest published patch release, and restart it.
3. Confirm the center is serving a fixed installer before regenerating commands:

   ```bash
   curl -fsSL https://center.example.com/api/agent/install.sh | grep -- '--install-missing-deps'
   ```

4. Regenerate the MonitoringInstance install/upgrade command from the fixed center.
5. Confirm the regenerated command includes `--install-missing-deps`, and that `--version` points at the fixed release whose GitHub Release contains `houfeng-agent_<version>_linux_amd64`, `houfeng-agent_<version>_linux_arm64`, `sha256sums.txt`, and `sha256sums.txt.minisig`.
6. Run the regenerated command on the target host.

If the center cannot be upgraded immediately, manually install a trusted compatible `minisign` verifier as a temporary host-level workaround, then rerun the old command. Keep this as an emergency bridge only; the durable fix is a regenerated command from a fixed center.

Manual installation remains a troubleshooting fallback when investigating installer failures:

```bash
getent passwd houfeng-agent >/dev/null || sudo useradd --system --home-dir /var/lib/houfeng-agent --shell /usr/sbin/nologin houfeng-agent
sudo install -d -o houfeng-agent -g houfeng-agent -m 0750 /etc/houfeng-agent
sudo install -d -o houfeng-agent -g houfeng-agent -m 0750 /var/lib/houfeng-agent
sudo install -o root -g root -m 0755 bin/houfeng-agent /usr/local/bin/houfeng-agent
sudo tee /etc/houfeng-agent/agent.env >/dev/null <<'EOF'
HOUFENG_AGENT_SERVER_URL=https://center.example.com
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=65536
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
HOUFENG_AGENT_BUFFER_MAX_BYTES=67108864
EOF
sudo chown root:houfeng-agent /etc/houfeng-agent/agent.env
sudo chmod 0640 /etc/houfeng-agent/agent.env
printf '%s' '<30-minute-one-time-enrollment-token>' | sudo tee /etc/houfeng-agent/token >/dev/null
sudo chown houfeng-agent:houfeng-agent /etc/houfeng-agent/token
sudo chmod 0600 /etc/houfeng-agent/token
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-agent.service /etc/systemd/system/houfeng-agent.service
sudo systemctl daemon-reload
sudo systemctl enable houfeng-agent
if systemctl is-active --quiet houfeng-agent; then
  sudo systemctl restart houfeng-agent
else
  sudo systemctl start houfeng-agent
fi
```

Adjust users, paths, PostgreSQL URL, TLS/reverse-proxy setup, public center URL, and token file ownership for the target host. Do not start or restart the agent until `/etc/houfeng-agent/token` contains a valid unexpired enrollment token or post-enrollment sync credentials.

## Authentication

The center protects every `/api/*` route except `/api/healthz` and the agent
endpoints (`/api/agent/enroll`, `/api/agent/sync`, `/api/agent/install.sh`) with a session-cookie auth
layer. Required environment variables:

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `HOUFENG_INITIAL_USERNAME` | yes | — | Admin username seeded on first startup. |
| `HOUFENG_INITIAL_PASSWORD` | yes | — | Admin password seeded on first startup (bcrypt-hashed before persisting). |
| `HOUFENG_INITIAL_PASSWORD_FILE` | no | — | File path containing the initial admin password; takes precedence over `HOUFENG_INITIAL_PASSWORD`. |
| `HOUFENG_INITIAL_DISPLAY_NAME` | no | username | Display name for the seed user. |
| `HOUFENG_SESSION_TTL` | no | `168h` | Rolling session lifetime; refreshed on each authenticated request. |
| `HOUFENG_SESSION_HMAC_KEY` | yes | — | At least 32-byte HMAC secret for hashing session IDs at rest; keep stable across restarts. |
| `HOUFENG_SESSION_HMAC_KEY_FILE` | no | — | File path containing the session HMAC secret; takes precedence over `HOUFENG_SESSION_HMAC_KEY`. |
| `HOUFENG_PASSWORD_BCRYPT_COST` | no | Go bcrypt default cost | Cost used for newly seeded or changed passwords; validate with a latency benchmark before raising. |

Behavior:

- On first startup with an empty `users` table, the center creates a single
  admin user from the environment variables.
- On subsequent startups (any rows present in `users`) the variables are
  **ignored**; password rotation is done through the authenticated change-password
  flow, not by re-setting the env var.
- Sessions are stored in the `sessions` table, persisted by HMAC hash rather
  than plaintext session ID, and delivered as Secure + HttpOnly +
  SameSite=Strict cookies named `__Host-houfeng_session`.
  The HMAC secret is loaded from `HOUFENG_SESSION_HMAC_KEY` or
  `HOUFENG_SESSION_HMAC_KEY_FILE`; rotating it invalidates existing browser
  sessions. A background worker sweeps expired rows hourly.
- Agent enrollment/sync endpoints continue to authenticate via enrollment or sync tokens (independent of the user session layer); `/api/agent/install.sh` is a public read-only installer script and does not carry a token by itself.

### HTTPS requirement

The session cookie carries the `Secure` attribute. Browsers will not send it over plain HTTP, so authenticated deployments must terminate HTTPS at a reverse proxy (Caddy, Nginx, etc.) and forward to `HOUFENG_HTTP_ADDR`. Running the center directly on a public network without HTTPS will break browser login and **must not** be done.

## Reverse proxy and TLS

Houfeng can run behind a local reverse proxy that terminates TLS and forwards to `HOUFENG_HTTP_ADDR`. The center itself remains the single API/UI process.

## Operational verification

After local/systemd startup:

```bash
curl -fsS http://127.0.0.1:8080/api/healthz
systemctl status houfeng-center
systemctl status houfeng-content-processor
systemctl status clamav-daemon
systemctl status houfeng-agent
sudo test -d /var/lib/houfeng/attachments
sudo test "$(stat -c '%a' /var/lib/houfeng/attachments)" = 700
sudo test -s /var/log/houfeng/center.log
sudo tail -n 100 /var/log/houfeng/center.log
journalctl -u houfeng-center -n 100 --no-pager
journalctl -u houfeng-content-processor -n 100 --no-pager
journalctl -u houfeng-agent -n 100 --no-pager
```

After Docker Compose startup:

```bash
curl -fsS http://127.0.0.1:16001/api/healthz
docker compose --env-file docs/deploy/compose.env ps
docker compose --env-file docs/deploy/compose.env logs --tail=100 houfeng
docker compose --env-file docs/deploy/compose.env logs --tail=100 houfeng-content-processor clamav
```

Continue with `docs/operations/fresh-install-smoke-run.md` for the full fresh-install path.
