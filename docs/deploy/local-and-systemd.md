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
HOUFENG_RECORDS_ENABLED=false
HOUFENG_RECORD_INSTANCE_ID=
HOUFENG_RECORD_DEPLOYMENT_ID=
HOUFENG_RECORD_INSTANCE_KIND=
HOUFENG_RECORD_INSTANCE_CAPABILITY=
HOUFENG_COMPARISON_ENABLED=false
HOUFENG_PORTABILITY_ENABLED=false
HOUFENG_COMPARISON_INTENT_KEYRING=/etc/houfeng/comparison-intent
HOUFENG_COMPARISON_INTENT_KEY_ID=cmp_current
HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES=67108864
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

The release bundle is the ordinary single-host production path. It runs only
published images and needs no source checkout, local build, helper launcher, or
manual database command. Prerequisites are Docker with Compose support, an
existing Docker network shared with Nginx Proxy Manager (NPM), and an HTTPS
hostname whose DNS already reaches NPM.

Create a private deployment directory and download the two assets from the same
GitHub Release:

```bash
install -d -m 0700 houfeng && cd houfeng
sudo install -d -o 10001 -g 10001 -m 0700 optional-secrets optional-secrets/comparison-keyring optional-secrets/s3
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.yaml -o compose.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.env.example -o .env
chmod 0600 .env
${EDITOR:-vi} .env
docker compose config
docker compose pull
docker compose up -d
```

Fill every value in **Must change** before validation. The downloaded
`HOUFENG_IMAGE` is already pinned to the matching `vX.Y.Z` image; do not replace
it with `latest`. **Recommended** contains the stable project name, the fixed
Records profile marker, proxy/session choices, and local-storage choice.
**Optional** contains integrations that may remain blank. Keep
`COMPOSE_PROJECT_NAME=houfeng` stable across upgrades and restores.
Generate every blank secret independently with `openssl rand -hex 32`, paste
the raw hex without quotes, and never reuse a value. Hex avoids dotenv traps
from `$`, `#`, and quote characters.
`HOUFENG_RECORDS_ENABLED=true` describes a production invariant: the Compose
service pins it true because database initialization converges and admits the
current Records ACL. Permanent deletion is pinned false because this profile
does not deliver the required external witness.

### Nginx Proxy Manager

Set `HOUFENG_PROXY_NETWORK` to the exact existing Docker network already joined
by NPM. Only `houfeng` joins it, with the stable alias `houfeng`; no Houfeng
service publishes a host port. In NPM create a Proxy Host with:

- domain name equal to the host in `HOUFENG_PUBLIC_BASE_URL`;
- scheme `http`, forward hostname `houfeng`, and forward port `16001`;
- **Websockets Support** and **Block Common Exploits** enabled;
- a valid certificate and **Force SSL** enabled.

Set `HOUFENG_TRUSTED_PROXIES` only when forwarded client IPs are required, and
then only to the exact NPM-network CIDR. Broad ranges such as `0.0.0.0/0` and
`::/0` are rejected. Apply appropriate request-body, rate, and connection limits
in NPM. Center Host validation is derived from `HOUFENG_PUBLIC_BASE_URL`.

### Topology and initialization

The stack contains eight services:

- `houfeng-storage-init` creates the UID/GID-owned attachment, log, authority,
  public Center identity, and staged-secret directories below `./data`;
- `houfeng-secrets-init` copies only the four operator database secrets needed
  by the read-only initializer/processor into service-specific private files;
- `db` runs pinned PostgreSQL 16 with its bootstrap secret;
- `houfeng-db-init` provisions distinct runtime, platform-admin, and migrator
  logins plus the internally generated constrained authority login, applies the
  pre-R1 catalog restriction, converges the current schema, verifies or creates
  the signed activation ledger, activates through the existing projector, and
  proves runtime admission;
- `houfeng-record-authority` verifies the signed state and active contract,
  renews only the fixed `compose-center` membership, and exposes a loopback-only
  health probe after the membership is fresh;
- `clamav` maintains the malware-signature cache and scans attachment content;
- `houfeng` serves the baked Web UI and API as UID/GID 10001;
- `houfeng-content-processor` runs the attachment processor as UID/GID 10001
  with a read-only root, no capabilities, `no-new-privileges`, core dumps
  disabled, and a bounded private tmpfs.

Storage, secret-staging, and database initialization are one-shot gates. Center
starts only after they succeed, PostgreSQL/ClamAV are healthy, and the Records
authority is healthy; the processor waits for initialization and scanner
readiness. Seeing all three initializer containers in `Exited (0)` state is
expected. Any catalog, identity, signed-state, activation, membership,
migration, or runtime-admission drift fails closed before application services
start. Exact-repeat initialization verifies the existing authority bundle and
active contract rather than creating a second identity or fabricating rows.

Compose secrets are sourced from the private root `.env` and exposed as files to
only the services that need them. PostgreSQL receives only its bootstrap
secret. `houfeng-secrets-init` receives the bootstrap and three operator-managed
database-role secrets, mounts only `data/secrets`, then writes separate private
directories for db-init and the processor because Compose cannot inject
environment-backed secret content into a read-only service root. It cannot read
the authority bundle, attachments, logs, or PostgreSQL tree. Center receives the
runtime, initial-admin, and session secrets. The processor receives only a staged
runtime secret. The
authority reads only its generated private state and generated constrained
database credential. Center and processor never receive bootstrap, migrator,
platform-admin, or authority credentials; the processor never receives initial
admin or session material.

All local durable state is visible in the deployment directory:

The authority's private bundle is under `data/records-authority`, Center receives
only `data/center-config`, and staged service files remain under `data/secrets`.

```text
data/
├── attachments/
├── center-config/
│   └── deployment-id
├── clamav/
├── logs/
│   └── center.log
├── postgres/
├── records-authority/
│   ├── private/
│   │   ├── activation-ledger.jsonl
│   │   ├── activation-receipt.json
│   │   ├── authority-key
│   │   └── database-secret
│   └── public/
│       └── deployment-id
└── secrets/
    ├── db-init/
    └── processor/
```

`optional-secrets/` is the only additional mounted operator directory and must
remain owned by UID/GID `10001` and mode `0700` so the non-root containers can
traverse their scoped bind mounts. Install an optional key file with
`sudo install -o 10001 -g 10001 -m 0400 SOURCE DEST`; do not loosen permissions
to make a host-side copy succeed. `optional-secrets/comparison-keyring/` is
mounted only into Center. `optional-secrets/s3/` is mounted into Center and the
processor because both access the same Blob backend; the processor cannot read
comparison keys.
Local attachments are the default. If S3 is selected, Center and processor must
use the same endpoint, bucket, TLS mode, and credential-file paths below the S3
directory; S3 data is no longer part of the local `./data` migration unit.

### Health and troubleshooting

`docker compose ps` must show PostgreSQL, ClamAV, Center, and the Records
authority healthy, the processor running, and all three initializer services
completed successfully. Verify the public route through NPM because no
loopback/host port exists:

```bash
docker compose ps
docker compose logs --tail=100 houfeng houfeng-record-authority houfeng-content-processor clamav
docker compose exec -T db pg_isready -U postgres -d houfeng
curl -fsS https://center.example.com/api/healthz
test -s data/logs/center.log
```

Replace the example HTTPS origin with `HOUFENG_PUBLIC_BASE_URL`. Keep shared
diagnostics free of passwords, cookies, provider credentials, enrollment
commands, and tokens. If an initializer failed, inspect it explicitly with
`docker compose logs houfeng-storage-init houfeng-secrets-init houfeng-db-init`;
do not bypass its dependency or start Center manually. Authority health failure
belongs in `docker compose logs houfeng-record-authority`; do not replace the
deployment ID or edit its private files to force readiness.

### Backup, restore, and host migration

PostgreSQL, local attachments, and Records authority state are one logical
recovery point. The matching public deployment ID, signed ledger, authority key,
generated database credential, and activation receipt are not reproducible
cache data.
For the simplest consistent backup, stop the whole stack with
`docker compose down`, copy the complete private deployment directory while
PostgreSQL is stopped, then restart with `docker compose up -d`. The copy must
include `compose.yaml`, `.env`, `optional-secrets/`, and the entire `data/`
tree. Protect it as secret material. A database-only, attachment-only,
authority-only, or live unordered filesystem copy is incomplete. An active
database with absent, corrupt, or mismatched authority state fails closed;
restore PostgreSQL and Records authority state together.

To restore or migrate hosts, stop the source, copy that same directory intact,
install Docker/Compose on the target, recreate or join the external NPM network
named in `.env`, and run `docker compose config` followed by
`docker compose up -d`. Do not start both copies against one identity or split
the PostgreSQL and attachment recovery points. Confirm the public health route
and a representative admitted Records write plus attachment after restore before
retiring the source. Starting two copied authorities for the same deployment is
unsupported.

For S3, a valid recovery point instead requires a coordinated PostgreSQL dump
and object-version manifest. The Compose bundle does not claim cross-storage
snapshot orchestration; use a separately reviewed recovery procedure.

### Upgrade, rollback, and secret rotation

Before upgrading, take a cold recovery point. Download `compose.yaml` and the
public `compose.env.example` asset from the exact target tag, review the new
template against the private local `.env`, preserve operator values, and update
`HOUFENG_IMAGE` to the matching immutable tag. Then run:

```bash
docker compose config
docker compose pull
docker compose up -d
docker compose ps
```

The database initializer applies the supported forward schema transition before
Center starts. Do not roll an older image back over a database already migrated
by a newer incompatible release. If release notes do not explicitly confirm
backward compatibility, rollback means restoring the complete pre-upgrade cold
recovery point together with its matching Compose asset and image tag.

Compose does not include the value behind an environment-sourced secret in its
service configuration hash. A plain `docker compose up -d` therefore does not
guarantee that a completed initializer or running application container will be
recreated after a password-only edit. For a runtime, migrator, or
platform-admin password rotation, use this controlled sequence:

```bash
docker compose stop houfeng houfeng-content-processor
${EDITOR:-vi} .env
docker compose run --rm houfeng-secrets-init
docker compose run --rm houfeng-db-init
docker compose up -d --force-recreate houfeng houfeng-content-processor
docker compose ps
```

The explicit secret-staging run is required: Compose does not include an
environment-backed secret's value in the completed one-shot service hash, and
db-init/processor intentionally read their staged read-only files. Do not restart
the application services if either staging or initialization fails. PostgreSQL's
bootstrap password must be rotated inside PostgreSQL and in `.env` as one
controlled operation before running the same initializer/recreation sequence;
changing only `.env` locks the initializer out. Change the Center admin password
in the authenticated UI after first startup because seed credentials are
ignored once users exist. Rotating `HOUFENG_SESSION_HMAC_KEY` requires explicit
Center recreation and invalidates browser sessions and HMAC-backed agent
credentials, so plan reauthentication/re-enrollment before doing it.

`docker compose down` removes containers and the private default network but
does not delete bind-mounted data. Never delete `./data` unless intentionally
discarding the entire deployment and its recovery point.

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
| `HOUFENG_RECORDS_ENABLED` | no | `false` | Enables the records/evidence platform. |
| `HOUFENG_RECORD_INSTANCE_ID` | no | — | Optional 0051 membership instance id. Must be set together with the other three record-identity variables. |
| `HOUFENG_RECORD_DEPLOYMENT_ID` | no | — | Optional `dp-` + 64 hex deployment id bound into the named AdmissionGate. |
| `HOUFENG_RECORD_INSTANCE_KIND` | no | — | Optional instance kind: `api`, `worker`, or `recovery`. |
| `HOUFENG_RECORD_INSTANCE_CAPABILITY` | no | — | Optional membership capability token. Empty or unactivated membership still fail-closes writes. |
| `HOUFENG_COMPARISON_ENABLED` | no | `false` | Enables the comparison workbench routes. Requires `HOUFENG_RECORDS_ENABLED=true` and a mounted comparison HMAC keyring. |
| `HOUFENG_COMPARISON_INTENT_KEYRING` | no | — | Directory of independent 0400 comparison HMAC keys. Required when comparison is enabled. Mount the path; do not COPY key bytes into the image. Do not reuse session, deletion, or backup keys. |
| `HOUFENG_COMPARISON_INTENT_KEY_ID` | no | — | Current key file name inside the comparison keyring. Required when comparison is enabled. |
| `HOUFENG_COMPARISON_ADMISSION_BUDGET_BYTES` | no | `67108864` | Process-local comparison admission budget. Must be at least 8 MiB. |
| `HOUFENG_PORTABILITY_ENABLED` | no | `false` | Enables record export preview/download routes. Requires `HOUFENG_RECORDS_ENABLED=true`. |
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
docker compose ps
docker compose logs --tail=100 houfeng houfeng-content-processor clamav
docker compose exec -T db pg_isready -U postgres -d houfeng
curl -fsS https://center.example.com/api/healthz
test -s data/logs/center.log
```

Continue with `docs/operations/fresh-install-smoke-run.md` for the full fresh-install path.
