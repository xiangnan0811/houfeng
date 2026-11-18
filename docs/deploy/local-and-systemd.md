# Houfeng local, Docker Compose, and systemd deployment guide

> Authentication note: the center requires a username + password login for
> protected application API routes; health, public installer, token-authenticated agent routes and authentication endpoints have their own semantics.
> First-startup credentials come from `HOUFENG_INITIAL_USERNAME` and
> `HOUFENG_INITIAL_PASSWORD` or `HOUFENG_INITIAL_PASSWORD_FILE`; once the
> `users` table is populated those variables are ignored. Session cookies are
> `__Host-` scoped, Secure, HttpOnly, and SameSite=Strict; the deployment
> **must** terminate HTTPS at a reverse proxy for authenticated browser use.
> See the **Authentication** section below.

## Scope

规范正文见 [APP ACL 与迁移](../spec/contracts/platform/app-acl-current.md)、[Compose authority](../spec/contracts/platform/compose-authority.md)及[生产 Compose 合同](../spec/contracts/platform/production-compose.md)。本页给出操作顺序与恢复步骤；更改权限、迁移或拓扑时同步所属合同。

This guide describes the current deployment paths for `候风 / Houfeng Fleet Control Plane`: one Go center process serving API + web UI, one isolated attachment content-processor process, one required ClamAV scanner, one PostgreSQL database, and one or more Go agents managed by systemd on the monitored hosts. The center, processor, scanner, and database can run directly on the host or in the provided Docker Compose stack; agents are not containerized.

This repository does not currently document package-manager repositories, Kubernetes deployment, automatic upgrades, or non-systemd agent installation.

### VPS lifecycle fresh-install boundary

`0067_refactor_vps_monitoring_lifecycle.sql` introduces the VPS/session lifecycle model for fresh installations. Existing VPS, monitoring, service/domain or related business data that needs conversion causes initialization to fail explicitly. There is no old-state conversion, compatibility alias, or automatic data deletion. Do not rebuild an existing environment as an implicit upgrade step; rebuilding requires a separate operator decision and appropriate backup.

Deploy a matching Center, SPA and Agent candidate together. The new Agent session credentials and unbuffered online signals are required; mixed old/new Agent operation is unsupported. A Center restart resets the continuous receiver-health observation window. Archive eligibility therefore requires another uninterrupted 180-minute observation after restart for previously enrolled assets.

### IP quality product protocol upgrade and rollback

For the product revision 1 report upgrade, deploy the compatible **Center first,
then Agents**. The Center advertises `product_revision=1`; new Agents talking to
an older or unsupported plan omit all product/details/checks fields and do not
probe mail. Old Agent reports and queued 18-provider carriers remain accepted by
the new Center. C77 is additive; no historical report is re-collected or backfilled.

Once any Agent has persisted revision 1 reports in its sync queue, rolling the
Center back to a version that does not recognize that protocol is unsupported.
Before such a rollback, stop new IP-quality collection and verify those queues
have drained into a compatible Center. If a queue still contains revision 1
reports, restore a compatible Center instead. **Never delete or rewrite the queue
to downgrade it**, and never weaken strict request decoding or the Agent's
HTTP-400 discard policy to conceal incompatibility.

Source credentials are optional, self-owned keys stored only by Center settings.
They are not sent to Agents. Clearing a key prevents new use/cache reuse and fences
in-flight authenticated results at finalization; previously completed historical
business facts remain immutable. Changing a key or refreshing the page does not
reopen old enrichment snapshots: request a new report for new data.

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

For one-command agent installation, a published GitHub Release contains the Linux agent binaries, corresponding-source archive, checksum manifest and detached minisign manifest signature. The release workflow builds, signs and uploads these assets on `release.published`; local build/signing capability alone is not public-distribution authorization:

```bash
make build-agent-release VERSION=v1.2.3
```

Local `make build-agent-release` outputs under `dist/`:

- `houfeng-agent_v1.2.3_linux_amd64`
- `houfeng-agent_v1.2.3_linux_arm64`
- `houfeng-agent_v1.2.3_source.tar.gz`
- `houfeng-agent_v1.2.3_licenses.txt`
- `sha256sums.txt`

`build-agent-release` fails before building or creating release outputs if the tracked working tree or index differs from `HEAD`, or if any non-ignored untracked file exists. Ignored scratch is permitted and untouched. This prevents current-tree binaries from being paired silently with a different HEAD source archive; use `make build-agent` for a dirty development candidate instead.

The local target does not sign the manifest. The release workflow additionally creates `sha256sums.txt.minisig`; all six assets must exist before testing the complete distribution. The source archive is the complete tracked release `HEAD`, including project and third-party licenses, detector source/assets, upstream original, modification notes, SHA256SUMS, Go module files, Makefile and workflow. Binary, archive and license-bundle hashes share the signed manifest. Use a clean release checkout for corresponding-source evidence; a dirty local build is not such evidence.

`build-agent-release` stamps the heartbeat version with the `VERSION` used in artifact names. Center serves the installer; GitHub Release supplies binaries, corresponding source and signed checksums, not token authority. Configure `HOUFENG_RELEASE_MINISIGN_PRIVATE_KEY` to match the installer-pinned public key, and `HOUFENG_RELEASE_MINISIGN_PASSWORD` if encrypted. The target host needs minisign: `--install-missing-deps` retains the existing pinned static-verifier download/SHA256/install flow before release verification.

IP quality detector runtime dependencies are **Bash >=4 and jq**, installed in `/usr/bin` or `/bin`; it uses shell builtins and the Agent's private fetch helper, not curl/dig/bc. The installer preflights these before downloads, stopping the service or changing existing Agent files; missing/broken dependencies require manual operator repair, even with `--install-missing-deps`. No apt/yum/dnf/apk or new detector dependency installation is performed. The existing separately consented, checksum-pinned minisign bootstrap remains unchanged. If dependencies disappear after installation, the four script-backed service probes and keyless geo fallback report `detector_dependency_missing`; monitoring, five public risk sources, the retained three probes and PTR/mail do not exit or silently use old algorithms.

Houfeng's original code is licensed under [AGPL-3.0-only](../../LICENSE) by explicit owner choice. The embedded IPQuality fork retains its upstream license/source/provenance; other third-party components retain their own licenses and notices. Subprocess execution is not an AGPL exemption. Distributors must retain corresponding source and build materials; operators of modified network-facing versions must offer users the corresponding source of their actual version under AGPL section 13, including local changes rather than only linking upstream. See the [project license and source policy](../../README.md#license-and-corresponding-source) and [IP quality contract](../spec/contracts/ip-quality.md). Local verification alone does not authorize publication or production deployment.

The login and sidebar footers offer source and license links. Official image builds set Docker `SOURCE_URL` to the exact checked-out GitHub revision; custom builds must set it to their own published corresponding source (`VITE_SOURCE_URL` for a direct web build). `/licenses.txt` contains the project license and complete third-party notice wording, with line endings and trailing whitespace normalized; the image also retains them under `/usr/share/licenses/houfeng/` and keeps OS-package notices under `/usr/share/doc`. After dependency or toolchain updates, regenerate the tracked bundle with `node scripts/collect-licenses.mjs` (installed npm dependencies and the go.mod toolchain required), then check it with `node scripts/collect-licenses.mjs --check`. Image publication waits for the signed source/license assets to upload successfully.

The monitoring list’s upload/download rates use `network_rates_valid`. A missing valid sample shows an em dash rather than a guessed zero. The first Agent sample establishes a counter baseline; rates become available after a subsequent valid sample. System uptime remains sampled host uptime, not Agent process age.

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

`HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, an initial password source, and `HOUFENG_SESSION_HMAC_KEY` are required for center startup. Use `HOUFENG_INITIAL_PASSWORD_FILE` instead of `HOUFENG_INITIAL_PASSWORD` when the password is provided by a file or secret mount; the file value takes precedence. Use `HOUFENG_SESSION_HMAC_KEY_FILE` instead of `HOUFENG_SESSION_HMAC_KEY` when the session HMAC secret is provided by a file or secret mount; the file value takes precedence. The session HMAC secret must be at least 32 bytes and must stay stable across restarts; rotating it invalidates existing browser sessions and any agent enrollment/sync token hashes already migrated to the HMAC format. `HOUFENG_PUBLIC_BASE_URL` is optional for center startup, but required to generate one-command install commands and to enable production Host allowlisting. Set it to the externally reachable absolute `http(s)` URL that target agents can access, for example `https://center.example.com` or `http://203.0.113.10:8080`; it must not include query or fragment, and the center normalizes trailing slashes when generating commands. `HOUFENG_TRUSTED_PROXIES` is optional and should only list the exact reverse-proxy CIDRs that are allowed to supply forwarded client IP headers; `0.0.0.0/0` and `::/0` are rejected. `HOUFENG_DATABASE_REQUIRE_TLS=true` makes startup reject database URLs without `sslmode=require`, `sslmode=verify-ca`, or `sslmode=verify-full`; enable it for external/production PostgreSQL. `HOUFENG_PASSWORD_BCRYPT_COST` defaults to Go bcrypt's default cost and can be raised after benchmarking login and password-change latency on the target host. `HOUFENG_LOG_FILE` is optional; when set, center writes application logs to both stdout and the configured file, and startup fails if the file cannot be opened. The deployment Telegram default is disabled unless both Telegram values are set; the effective channel may still be page-overridden, inherited, or explicitly disabled in the settings singleton. Use `.env.example` as the full local variable inventory; the systemd snippets below are deployment-shaped examples and must include the required auth seed vars on first startup.

Direct and systemd deployments should leave `HOUFENG_LOG_FILE` unset and use
stdout/journald. The optional file API remains available, but its host must
rotate that file separately; Docker logging options do not bound custom files.
This setup does not change shared journald configuration.

An empty `HOUFENG_TRUSTED_PROXIES` trusts no forwarded headers: requests behind
a reverse proxy share the actual socket peer's IP budget. Sync allows 2400
requests per IP and 2400 globally per sliding minute; enrollment remains 120
per IP and 1000 globally. These are per-Center-process resource budgets, not a
hardware throughput guarantee. Thirty Agents at the default five-second tick
need 360 requests/minute, or up to 1080 with two historical replays per tick.
The existing 32-inflight and 4 MiB sync body limits remain. A genuine shared NAT
still shares an IP budget after forwarded headers are correctly configured.
429 and capacity 503 responses include `Retry-After: 5`; Agents delay only
historical replay (bounded to 1–60 seconds), not the next fresh five-second tick.

`HOUFENG_TELEGRAM_BOT_TOKEN` and `HOUFENG_TELEGRAM_CHAT_ID` are deployment
defaults, not a one-time import into the database. `HOUFENG_INCIDENT_SWEEP_INTERVAL`
is likewise the deployment sweep default (normalized to positive whole
seconds; an omitted value uses the program default of 5 seconds). Center
captures these inputs in its successful startup snapshot; settings requests do
not reread `.env`, secret mounts, or the process environment.

When the settings singleton is absent, or its ownership is `inherit`, the
effective Telegram and sweep values come from that startup snapshot. A page
`override` persists its own Telegram pair or sweep seconds and wins after
restart, process/repository recreation, and upgrade. `disabled` persists an
explicit empty Telegram configuration and prevents deployment fallback.
Restoring `inherit` clears the page-owned Telegram credentials, or resets the
raw sweep value to the program default, so the next read uses the current
deployment snapshot. Saving unrelated settings must not import a deployment
token or turn an effective inherited sweep into a database override.

`GET /api/settings` is read-only with respect to storage when no singleton
exists: it returns resolved defaults without seeding a row. `PUT /api/settings`
uses the locked latest row; an omitted Telegram block or sweep mode preserves
the raw ownership/value, while explicit modes perform the documented
inherit/override/disabled action. Both methods are private, `no-store`, and
never return raw secrets. The deployment-settings overview
(`GET /api/settings/deployment`) is supervisor-only and an immutable allowlist
snapshot; it is not a writable environment editor.

For an upgrade, stop old settings writers and follow the applicable migration
contract before starting the new Center. The persisted ownership and values
are the authority across the upgrade; changing deployment values takes effect
only after the new process starts and only for inherited settings. Do not
reconstruct a repository or import environment values as an upgrade
substitute.


Attachment storage has no implicit backend when Records runtime admission is enabled. `local` requires an absolute private directory that is neither `/` nor another broad system path. S3 deployments set `HOUFENG_ATTACHMENT_BLOB_BACKEND=s3`, endpoint, bucket, and TLS mode, and load credentials through `HOUFENG_ATTACHMENT_S3_ACCESS_KEY_FILE` and `HOUFENG_ATTACHMENT_S3_SECRET_KEY_FILE`; do not put credential values in the environment file. Center and processor must use the same immutable Blob backend and scanner settings.

### Local Blob namespace admission

The C81 local-storage contract requires an admitted namespace before Center or
content-processor starts. Ordinary fresh Compose performs this one-shot admission
after db-init and before either writer. Its UID/GID10001 initializer checks the
direct platform-admin connection, records three random UUIDs in the separate
UID10001-owned `data/local-namespace-init` journal, and reuses them on exact
retry. The administrator credential is a read-only staged file available only to
that initializer, not to the runtime processes. Creating an empty directory or
applying schema alone is insufficient. S3 selection does not initialize a local
namespace. Actual fresh-install acceptance remains a separate evidence gate.
The namespace marker is published atomically. On a source-enrolled stack, the
one-shot verifies an existing matching binding only; it never provisions one.

For a direct or systemd deployment using a fresh isolated tree with all business
writers stopped, run the built admin executable as the actual BlobRoot owner,
with a direct platform-admin SQL login supplied only through a private one-shot
request outside BlobRoot:

```bash
houfeng-record-platform-admin local-namespace provision --request-file /run/houfeng-local-provision/request.json
```

The strict JSON fields are `database_url`, absolute canonical `root`, `namespace_id`,
`incarnation_id`, `request_id`, numeric `owner_uid`, and numeric `owner_gid`. Preserve the three
random UUID identities for exact retry. The invoking user owns the private request directory
and mode-0600 request. The command consumes the credential input before DB access. After an
ambiguous failure keep writers stopped and resolve the same identities. Never leave privileged
credentials in the business volume/runtime configuration, invoke the participant as root, or
relax ownership to make it succeed.

The tree uses 0700 directories, 0600 marker/lock/temp files and a dedicated fixed provision
lock. Runtime opens the admitted physical binding without mkdir, repair or rebind; changed
marker/directory/anchor identity fails closed. Both processes share one actual Linux
filesystem/flock domain; btrfs is supported. The local writer also requires `O_TMPFILE`,
user xattrs, and access to its own `/proc/self/fd` on procfs so an initialized, durably
registered inode can be linked without extra capabilities. Missing support fails closed;
there is no named-file fallback. Keep namespace rows, historical incarnations, anchors,
obligations and registered temporary targets in the coordinated recovery point.

Source-enabled adoption and V2 source initialization are unsupported; retained
source V1 has a separate contract. The local UID10001 participant cannot acquire
the source outer guard or activate source recovery. Ordinary cold upgrade accepts
an unbound released BlobRoot only when the UID/GID10001 `0700` root contains
exactly an empty UID/GID10001 `0700` `sha256` directory and the database has no
existing local blob references. A missing or empty BlobRoot is refused for cold
upgrade; fresh installation retains its separate initialization behavior. The
controller checks references before ACTIVE and again after fencing; the
initializer checks the physical tree. Populated legacy roots are refused before
any change. New-boot or
copied physical namespace adoption is not supported; do not delete markers or
invent a new UUID to force admission. A database inventory with
`physical_evidence=not_observed` proves neither physical inventory nor stopped
writers. See the [local I/O contract](../spec/contracts/platform/delivery-primitives.md#scenario-本地持久写入跨进程锁与清理).


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
manual database command for a fresh installation. Use local rootful Docker with
Compose support, an existing Nginx Proxy Manager (NPM) deployment in either
supported proxy mode, and an HTTPS hostname whose DNS already reaches NPM.

For a new installation choose a protected, canonical path whose ancestors are
root-owned, non-writable by other users, free of symlinks, and traversable by the
UID10002/GID10001 native upgrade participant. A user-owned `0700` home directory
is not sufficient for that host operation. Verify `/srv` meets these conditions;
the following creates a new `/srv/houfeng` and fails if it already exists. Run
these commands and subsequent Compose operations in a root shell. Control files,
including Compose assets, `.env`, and upgrade requests, must stay root-owned and
not writable by UID10002; `.env` and private requests must remain private. Do not
chmod a user's home directory or relocate an existing identity to force an upgrade.
An existing installation with unsafe or inaccessible ancestors must be refused.
Download the four assets from the same GitHub Release:

```bash
sudo -i
mkdir /srv/houfeng || exit 1
chown root:10001 /srv/houfeng
chmod 0750 /srv/houfeng
cd /srv/houfeng
sudo install -d -o 10001 -g 10001 -m 0700 optional-secrets optional-secrets/comparison-keyring optional-secrets/s3
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.yaml -o compose.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.proxy-network.yaml -o compose.proxy-network.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.proxy-host.yaml -o compose.proxy-host.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.env.example -o .env
chmod 0600 .env
${EDITOR:-vi} .env
```

Fill every value required by the selected mode in **Must change** before
validation. The downloaded `HOUFENG_IMAGE` is already pinned to the matching
`vX.Y.Z` image; do not replace it with `latest`. **Recommended** contains the
stable project name, the fixed Records profile marker, proxy/session choices,
and local-storage choice. **Optional** contains integrations that may remain
blank. Keep `COMPOSE_PROJECT_NAME=houfeng` stable across upgrades and restores.
Generate every blank secret independently with `openssl rand -hex 32`, paste
the raw hex without quotes, and never reuse a value. Hex avoids dotenv traps
from `$`, `#`, and quote characters.
`HOUFENG_RECORDS_ENABLED=true` describes a production invariant: the Compose
service pins it true because database initialization converges and admits the
current Records ACL. Permanent deletion is pinned false because this profile
does not deliver the required external witness.

### Nginx Proxy Manager

Select exactly one of the reviewed modes through `COMPOSE_FILE`. Do not load both
mode files at once.

#### Shared-network mode (default)

Keep the template default:

```dotenv
COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml
```

From the existing NPM Compose directory, inspect the networks already joined by
its `app` service:

```bash
docker inspect "$(docker compose ps -q app)" \
  --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{"\n"}}{{end}}'
```

If that deployment uses another service name, substitute it for `app`. Set
`HOUFENG_PROXY_NETWORK` to the exact existing user-defined network reported by
the command. Do not invent a network name: a missing external network makes
startup fail rather than creating a substitute. Houfeng joins the network
already used by NPM; this does not require editing NPM's Compose
configuration. Only `houfeng` gains that external-network membership, with the
stable alias `houfeng`, and no Houfeng service publishes a host port. Use the NPM
upstream `http://houfeng:16001`.

#### Host-proxy mode

An NPM deployment already using `network_mode: host` cannot resolve the
shared-network alias. Keep NPM in its existing host-network configuration and
check the Docker server version before changing `.env`:

```bash
docker version --format '{{.Server.Version}}'
```

Docker Engine 28.0.0 or newer is required. Earlier Engines may allow another
host on the same L2 segment to reach a localhost-published container port, so
they are unsupported; this deployment contract does not substitute a guessed
host-firewall workaround. Select the host overlay and leave the network value
blank:

```dotenv
COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml
HOUFENG_PROXY_NETWORK=
```

Use the NPM upstream `http://127.0.0.1:16001`. This mode publishes Center TCP
16001 only to the fixed host IPv4 loopback address; it does not expose a public
or LAN bind and does not change Center's private PostgreSQL/ClamAV network.

#### Common NPM and startup settings

In NPM create a Proxy Host with:

- domain name equal to the host in `HOUFENG_PUBLIC_BASE_URL`;
- scheme `http` and the forward hostname/port for the selected mode above;
- **Websockets Support** and **Block Common Exploits** enabled;
- a valid certificate and **Force SSL** enabled.

Set `HOUFENG_TRUSTED_PROXIES` only when forwarded client IPs are required, and
then only to an exact **observed socket-peer source CIDR** for the selected mode.
With an empty value, NPM's `X-Forwarded-For` is ignored and all proxied traffic
shares NPM's actual peer-IP budget. Inspect the source seen by Center rather
than assuming that NPM's configured upstream address is also its source.
Do not automatically trust an entire Docker subnet or guess `127.0.0.0/8`;
broad ranges such as `0.0.0.0/0` and `::/0` are rejected. Reconfirm the exact
peer after proxy/network changes; no environment-specific address is a default.
Apply appropriate request-body, rate, and connection limits in NPM. Center Host
validation is derived from `HOUFENG_PUBLIC_BASE_URL`. Rejection storms immediately
invalidate archive safety in memory and are coalesced by the five-second health
observer, not written to PostgreSQL or logged once per rejected request.

After selecting one mode and completing `.env`, render the merged model before
pulling or starting anything:

```bash
docker compose config
docker compose pull
docker compose up -d
```

### Topology and initialization

The stack contains these services:

- `houfeng-storage-init` prepares UID/GID-owned data directories and the
  ordinary guard and root marker;
- `houfeng-secrets-init` stages the scoped operator database secrets;
- `db` runs pinned PostgreSQL 16 with its bootstrap secret;
- `houfeng-db-init` runs as UID/GID 10002 with supplementary GID10001, provisions
  the scoped database logins, converges the current schema, verifies or creates
  the signed activation history, projects ordinary V2 membership and publishes
  the protected launch bindings;
- `houfeng-local-namespace-init` runs as UID/GID10001 after db-init, admitting a
  fresh local namespace with the separate staged platform-admin file; for S3 it
  does not initialize local storage;
- `houfeng-record-authority` verifies the signed state and active contract,
  atomically renews the distinct `compose-center` API and `compose-processor`
  worker memberships, and exposes a loopback-only health probe;
- `clamav` maintains the malware-signature cache and scans attachment content;
- `houfeng` serves the baked Web UI and API as UID/GID 10001;
- `houfeng-content-processor` runs as UID/GID 10001 with a read-only root, no
  capabilities, `no-new-privileges`, core dumps disabled, and a bounded private
  tmpfs. It reads its launch and deployment ID through the read-only public
  `center-config` mount. Worker claim, renewal and completion require its own
  live membership at effect time; heartbeat renewal alone does not prove liveness.

Storage, secret-staging, database and namespace initialization are one-shot
gates. Center and processor wait for the namespace initializer, and Center also
waits for healthy authority. Seeing all four initializer containers in
`Exited (0)` state is expected. Catalog, identity, signed-state, membership,
migration, namespace or runtime-admission drift fails closed. Genuine fresh V2
and exact-repeat initialization preserve the signed state and receipts while
bounded heartbeat expiry may renew. An active ordinary V1 installation requires
the explicit cold upgrade below; normal init cannot silently replace its launch.

Compose secrets come from the private root `.env` and are scoped to the services
that need them. PostgreSQL gets bootstrap only. The stager receives bootstrap and
the four operator database-role secrets, has a read-only data parent with staged
secrets masked, and writes service-specific directories. The namespace-only
platform-admin file is UID10002:GID10001 mode `0440` under a `0750` directory;
only the namespace initializer mounts that directory read-only. Center receives
runtime, initial-admin and session secrets; the processor receives only staged
runtime credentials. Neither receives platform-admin, deletion-worker, bootstrap,
migrator or authority credentials. The authority uses only its generated private
state and database credential.

Storage preparation creates the ordinary root marker and permanent guard under
`maintenance/`. The namespace initializer mounts the maintenance parent read-only
to read the public marker and guard; private operation state remains unreadable
to UID10001. Management checks the durable disposition while holding the guard;
ACTIVE survives controller death and prevents ordinary protected work. Do not
delete, replace, or reset the guard, marker, or operation journal to recover.

C81 requires an independent `HOUFENG_RECORD_DELETION_WORKER_PASSWORD`, staged as
`houfeng_record_deletion_worker_password` for db-init only. Generate it separately
with `openssl rand -hex 32`; do not reuse any other database password. Initialization
binds the constrained `houfeng_record_deletion_worker` login into the current
manifest. This does not start a deletion worker or enable permanent-deletion HTTP.

All local durable state is visible in the deployment directory:

The authority's private bundle is under `data/records-authority`, Center receives
only `data/center-config`, and staged service files remain under `data/secrets`.

```text
data/
├── attachments/
├── center-config/
│   ├── deployment-id
│   ├── birth-launch.json
│   └── birth-launch-processor.json
├── clamav/
├── local-namespace-init/
│   ├── bootstrap.lock
│   └── identity.json
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
    ├── local-namespace-init/
    └── processor/
```

All services share Docker's `local` logging driver with `max-size: "10m"`,
`max-file: "3"`, and compression enabled. Center writes only stdout; no log
directory is mounted. Existing `data/logs` is not deleted and remains part of
existing backups, but no longer receives new Center logs. Operators retain or
remove those historical files according to their retention requirements.

`optional-secrets/` contains optional runtime credentials and must
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
The namespace initializer exits without creating a local namespace when the selected
backend is S3; it does not switch the deployment to local storage.

### Health and troubleshooting

`docker compose ps` must show PostgreSQL, ClamAV, Center, and the Records
authority healthy, the processor running, and all four initializer services
completed successfully. Verify the public route through NPM in either mode;
shared-network mode has no host port, while host-proxy mode has only the reviewed
IPv4-loopback mapping:

```bash
docker compose ps
docker compose logs --tail=100 houfeng houfeng-record-authority houfeng-content-processor clamav
docker compose exec -T db pg_isready -U postgres -d houfeng
curl -fsS https://center.example.com/api/healthz
```

Inspect the running configuration with
`docker inspect --format '{{json .HostConfig.LogConfig}}' "$(docker compose ps -q houfeng)"`.
For rotation acceptance, use an isolated disposable container with these
rendered options, emit more than 30 MiB of sanitized logs, and check rotation
and recent-log readability. Do not fill production disks for this test. These
limits cover container logs, not PostgreSQL WAL, backups, attachments, or the
whole host.

Replace the example HTTPS origin with `HOUFENG_PUBLIC_BASE_URL`. Keep shared
diagnostics free of passwords, cookies, provider credentials, enrollment
commands, and tokens. If an initializer failed, inspect it explicitly with
`docker compose logs houfeng-storage-init houfeng-secrets-init houfeng-db-init houfeng-local-namespace-init`;
do not bypass its dependency or start Center manually. Authority health failure
belongs in `docker compose logs houfeng-record-authority`; do not replace the
deployment ID or edit its private files to force readiness.

In host-proxy mode, an optional host-local check can isolate the Center mapping
from NPM while retaining the configured public Host authority:

```bash
curl -fsS -H 'Host: center.example.com' http://127.0.0.1:16001/api/healthz
```

### Backup, restore, and host migration

PostgreSQL, local attachments, and Records authority state must be captured with
the namespace journal as one logical recovery point. The matching public deployment ID,
signed ledger, authority key, generated database credential, activation receipt,
and namespace UUID journal are not reproducible cache data. Stop the whole stack
with `docker compose down` and copy the private deployment directory while
PostgreSQL is stopped. Include `compose.yaml`, `compose.proxy-network.yaml`,
`compose.proxy-host.yaml`, `.env`, `optional-secrets/`, and the entire `data/` tree.
Protect the copy as secret material. A database-only, attachment-only,
authority-only, or live unordered copy is incomplete; restore PostgreSQL and Records authority state together.

The permanent guard and durable operation state under `maintenance/` are outside
the rollback unit: retain them and never replace an active disposition with an
older copy. A copied data tree does not authorize a new-boot, new-host or new
physical namespace rebind. Do not start a second copied authority, silently
adopt populated unbound storage, or claim host-migration acceptance from a copy.
The supported ordinary cold operation uses the same already-admitted physical
namespace; physical recovery and source handoff require separate authorization
and an independently supported procedure.

For S3, a valid recovery point instead requires a coordinated PostgreSQL dump
and object-version manifest. The Compose bundle does not claim cross-storage
snapshot orchestration; use a separately reviewed recovery procedure.

### Upgrade, rollback, and secret rotation

Take a cold coordinated recovery point and preserve the permanent maintenance
guard and any operation state before changing an existing installation. Ordinary
active V1 requires `deploy-ordinary-upgrade`, an explicit root controller on
local rootful Docker. The target image must already be present and pinned by
immutable image ID; the controller extracts and checks its actual static admin,
Center and processor executables. Stop unsupervised host writers, external restart
controllers and manual starts before the operation and keep them stopped until
the controller has completed the target restart and application proof. The
controller fences and joins the project containers and isolates the target DB.
The child receives neither Docker credentials/socket nor permission to rewrite
operator Compose files, `.env`, or the restart recipe. The controller validates
the protected path and non-writable control files before controlled recreation.

The `.env` `COMPOSE_FILE` must select exactly `compose.yaml` and one published
proxy overlay: `compose.proxy-network.yaml` or `compose.proxy-host.yaml`.
The controller pins the selected files and refuses any other selection before
making changes. A wrong-checksum or unsupported C81 predecessor is also refused
before any change.

Install the host `houfeng-record-platform-admin` binary as root-owned and not
group- or world-writable, under root-controlled directories that UID10002 can
traverse; the binary must also be executable by UID10002. For example, use a
root-controlled `/usr/local/bin` with an executable mode such as `0755`. The
trusted preparation stage re-executes this host controller binary as UID10002
before launching the pinned target. A copy under `/root` or another inaccessible
`0700` directory is refused with a preflight error.

The host controller must run as root with effective `CAP_KILL` and
`CAP_SYS_PTRACE` retained for the entire operation. Native spawn and probe check
both before creating any child and refuse otherwise. `CAP_KILL` permits pidfd
cancellation and kernel delivery of the parent-death SIGKILL to the UID10002
participant; `CAP_SYS_PTRACE` permits independent `/proc` inspection of that
nondumpable child. Plain host root normally has both. Capability-dropping sudo,
systemd units and containers must retain them; the UID10002 participant itself
runs with zero capabilities.

Native preparation and the participant share the host mount namespace. They can
access files according to the permissions of trusted UID10002; there is no
Landlock, chroot, separate mount namespace, or other kernel filesystem sandbox.
The trust boundary includes root, UID10002 and the root-selected target code for
the whole operation. The participant instead uses the exact pinned target,
typed selected paths, a minimal environment, private pipes for the five database
credentials, no capabilities, no-new-privileges and race-free descriptor closure.
It has no Docker access; the per-deployment guard and SQL admission enforce the
operation's state boundaries. No LSM, sysctl or persistent host-configuration
change is required.

The controller first acquires the permanent `.ordinary-controller.lock` in the
working directory (for the example, `/srv/houfeng/.ordinary-controller.lock`),
then the ordinary exclusive guard. A
missing `maintenance/ordinary` guard is bootstrapped with a journal; root-marker
continuity prevents a silent rebind. Preserve both locks, the marker and the
operation records; established missing or conflicting state fails closed.

The controller requires a clean environment: ambient `PG*` and `SSL_CERT*`
variables are rejected rather than ignored. Its native participant uses a minimal
environment and trusts UID10002 processes for the duration of the operation;
this is not isolation from a hostile peer running as UID10002. Containment of
the privileged helper on control EOF or SIGKILL does not change normal runtime
container Docker SIGTERM behavior.

The ordinary cold request is canonical compact JSON with one trailing newline.
Run the implemented `houfeng-record-platform-admin deploy-ordinary-upgrade
--request-file <private-file>` path with the prerequisites above. The acceptance
run used an owned nested Docker host, not a real-host installation; arm64 was
cross-built only, and lost-commit and pre-lock backend cases have component
coverage only. G2 remains NOT_READY, permanent-deletion HTTP remains CLOSED,
and this work has not been deployed.

In a root shell on the Docker host, first choose a unique stable operation ID
containing only lowercase letters, digits, `_` or `-`, at most 128 characters.
Prepare seven distinct root-owned, single-link `0400` or `0600` files in a
protected, non-symlinked directory: `/root/private/bootstrap`,
`/root/private/runtime`, `/root/private/admin`, `/root/private/worker`,
`/root/private/migrator`, `/root/private/initial-password`, and
`/root/private/session-hmac-key`. The first five contain the corresponding
database passwords, not URLs; the last two provide Center's initial-password
and session-HMAC secret for faithful target Compose recreation, not credentials
for HTTP acceptance proof. The session-HMAC file must contain the old Center's
actual key, witnessed before fencing; this operation must not rotate it
implicitly. Keep all values out of shell arguments, environment variables,
Compose assets, and the business data tree. Use the same protected request and
files for an exact retry; do not recreate or edit them after the operation starts.
The controller recreates the selected target using file-backed secrets from a
root-only `0700` private store containing `0444` files. Per-secret commitments
are stored in a root-private record; shared intent contains only its nonce-bound
digest. Raw values do not go into Docker client arguments, environment, or the
durable journal.

The following uses `jq`, an existing project named `houfeng` in `/srv/houfeng`,
and the immutable ID of the locally present target image. The target Compose
assets and `.env` must be root-controlled and match that image. The root-owned
request directory must be new; do not overwrite an earlier operation:

```bash
cd /srv/houfeng
read -r -p 'Unique operation ID: ' operation_id
read -r -p 'Locally present target image ID (sha256:...): ' target_image_id
database_container_id=$(docker inspect --format '{{.Id}}' "$(docker compose ps -q db)")
docker image inspect "$target_image_id" >/dev/null
umask 077
mkdir /root/houfeng-upgrade-input &&
jq -cn --arg operation_id "$operation_id" \
  --arg database_container_id "$database_container_id" --arg target_image_id "$target_image_id" \
  '{version:1,operation_id:$operation_id,project:"houfeng",working_directory:"/srv/houfeng",database_container_id:$database_container_id,target_image_id:$target_image_id,secrets:{bootstrap:"/root/private/bootstrap",runtime:"/root/private/runtime",platform_admin:"/root/private/admin",deletion_worker:"/root/private/worker",migrator:"/root/private/migrator",initial_password:"/root/private/initial-password",session_hmac_key:"/root/private/session-hmac-key"}}' \
  > /root/houfeng-upgrade-input/request.json &&
env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root \
  houfeng-record-platform-admin deploy-ordinary-upgrade --request-file /root/houfeng-upgrade-input/request.json
```

The database container ID must be its full 64 lowercase hexadecimal characters;
the image ID must be `sha256:` followed by 64 lowercase hexadecimal characters.
The command validates the canonical paths, ownership and request shape. If the
operation fails or its outcome is uncertain, keep the same request, target and
private files and invoke the same command again only after correcting the
reported external condition; do not rerun the request-creation commands.

Keep the durable operation, its private request and the guard state. An
interruption may leave ACTIVE even after both processes exit; protected ordinary
work stays closed. Retry the exact same operation and target, preserving all
recorded state. Never delete or reset the guard, disposition, journal, database,
or launch files to force startup. Unknown state, changed physical namespace,
source-enrolled/installed-birth state or an unsupported prior private 0081
checksum fails closed. The supported path preserves authentic signed V1 history
and uses the same already-admitted physical namespace. V2 exact repeat verifies
existing receipts and state; heartbeat expiry can legitimately renew.

Do not roll an older image over a database migrated by an incompatible release.
A cold recovery point is not proof that copying or restoring to a different
physical namespace is supported. Keep the old writers stopped after a failed
transition and resolve the durable operation before any controlled restart.
G2 remains NOT_READY and permanent-deletion HTTP remains CLOSED.

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

The token file initially contains an enrollment token issued from the MonitoringInstance onboarding workflow. After enrollment, `houfeng-agent` replaces it with session-scoped credentials so restarts do not reuse the consumed token. Ordinary binary upgrades preserve that session. Explicit reset/reenrollment downgrades old sessions to evidence-only; its center-generated command includes `--reenroll`, stops the running service and atomically replaces the credential file with the new bootstrap token before restarting. The first enrollment creates a new session on the same instance. Create or open the VPS first and use the generated command rather than manually changing the credential file.

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

For record-backed processor work, configure the processor's own complete
`HOUFENG_RECORD_INSTANCE_ID`, `HOUFENG_RECORD_DEPLOYMENT_ID` (or its `_FILE`),
`HOUFENG_RECORD_INSTANCE_KIND`, and `HOUFENG_RECORD_INSTANCE_CAPABILITY` against
an actually admitted deployment member. Reusing the API instance name does not
establish processor membership. Ordinary Compose provisions its distinct worker
identity; direct/systemd deployments must supply and admit their own processor
identity and launch configuration.

An enrolled source also requires the independent reader URL in
`HOUFENG_DELETION_WITNESS_DATABASE_URL` (or its `_FILE`) and the exact physical
identity file in `HOUFENG_SOURCE_WITNESS_IDENTITY_FILE`. The processor verifies
the APP enrollment binding with the same validator as Center and closes its
witness pool when the runtime exits. Without witness configuration, source
authorization checks APP enrollment and rejects enrolled record effects;
standalone processing and existing cleanup retain their own authorization paths.
Partial or invalid supplied witness configuration is a startup error. This
configuration does not enable birth certification or permanent deletion.

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
- Explicit reenrollment is distinct from a binary upgrade: when token issuance observes a pending instance with prior sessions, the generated command includes `--reenroll`. This path replaces old session credentials with the new bootstrap token after stopping the service; an ordinary installer rerun retains its existing session credentials. Old sessions can still report minimal online evidence and never regain collection or command permissions.
- If the center was built with the placeholder `dev` version or without `HOUFENG_PUBLIC_BASE_URL`, command generation returns a configuration error instead of guessing.
- `/api/agent/install.sh` is a public, read-only script route served by the deployed center. It contains no deployment-specific token until the generated command feeds a token through `--enrollment-token-stdin` at execution time.
- GitHub Release also hosts `houfeng-agent_<version>_source.tar.gz` and `houfeng-agent_<version>_licenses.txt` alongside the two Linux binaries, `sha256sums.txt` and `sha256sums.txt.minisig`; the install script is served by Center, not GitHub raw/release assets. The installer downloads only the selected binary and signed checksums; source and license assets remain independently verifiable in that manifest. The workflow resolves its source ref to one commit before either asset or image builds, including manual dispatches.
- `--install-missing-deps` grants consent only for the existing minisign recovery. Manual runs can omit it for a minisign prompt or pass `--no-install-missing-deps`; it never installs Bash, jq or other detector dependencies.
- Treat the generated command as a secret: do not paste it into tickets, chat, screenshots, process logs, or shell transcripts you plan to share.

The installer supports Linux systemd hosts on `amd64` and `arm64`. It fails before writing runtime files when the OS, architecture, service manager, downloader, or checksum tools are unsupported. If `minisign` is missing, the generated command allows downloading upstream `minisign` 0.12, verifies its pinned tarball SHA256 and installs the matching static binary to `/usr/local/bin/minisign`; declining dependency recovery stops before agent changes. The installer downloads the selected release asset and signed checksums, verifies the manifest signature and exact checksum entry, then replaces the agent binary and changes systemd state. It writes `agent.env`, creates the state directory, installs the unit, reloads systemd, enables and starts or restarts the service. Token files use mode `0600`: ordinary binary upgrades preserve existing session credentials; an explicit `--reenroll` command first stops the service and atomically replaces them with the new bootstrap token. Neither path requires a separate manual restart.

If an old command fails with `houfeng-agent install: minisign is required to verify release checksums`, first inspect the copied command. Commands generated by `v0.55.0` do not contain `--install-missing-deps`, and the `v0.55.0` installer stops immediately when `minisign` is missing. This is especially visible on Debian 11 bullseye and older Ubuntu hosts where `apt install minisign` may return `E: Unable to locate package minisign`. This is not a reason to disable signature verification or edit release checksum files.

Use the fixed-center recovery path:

1. Discard the old generated command and its one-time enrollment token.
2. Use the matching Center/Agent candidate for the current fresh installation. The historical `v0.55.1` installer fix does not permit mixed old/new protocols or conversion of an existing business database.
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

The center protects application API routes with a session-cookie auth layer. Health, authentication endpoints (including public login), public installer download, and token-authenticated agent enroll/sync routes have their own semantics. Required environment variables:

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
```

Continue with `docs/operations/fresh-install-smoke-run.md` for the full fresh-install path.

## VPS lifecycle installation boundary

Migration `0067` supports a fresh business installation only. Existing VPS,
MonitoringInstance, service, domain, Target or subscription data that needs
conversion causes an explicit rejection. The older state preflight cannot make
that database eligible; a clean preflight is not permission to map old states
or delete data. Released migrations `0065`/`0066` and their historical snapshots
remain unchanged, but their old upgrade procedure is not the current path.

Use a separately provisioned empty database for local acceptance and deploy
matching Center, SPA and Agent binaries. Run migrations through the configured
owner or current scoped-migrator path before starting writers. Do not mix old
and new Center writers or Agent protocols. Preserve the previous environment;
rebuilding it requires its own explicit authorization and verified recovery
material.

Application rollback never reverses migrations. Do not drop lifecycle history,
session evidence or constraints to make an old binary start. Recover forward
with a matching candidate, or restore a coordinated pre-change recovery point
through a separately authorized procedure with all writers stopped.

## Coordinated v2 Agent sync upgrade (C79 → C80)

This is an operator procedure, not permission to upgrade a running deployment.
C80 removes the old permanent replay tables and requires protocol revision 2;
Center and every Agent must cross this boundary together. Retain the configured
30-day raw and 365-day daily-aggregate policy; the migration does not rebuild
assets, monitoring identities, credentials, raw observations, or business history.

1. While still on the old version, correct trusted-proxy CIDRs from the actual
   socket peer and verify queues can drain. If necessary, use a separately
   reviewed receiver-budget-only fix; never run the v2 binary against C79.
2. Inventory **every** old Agent writer, including paused, retired, and
   evidence-only sessions. Stop each old Agent, then read its actual buffer file
   and require a valid empty JSON array. Nonempty means restart that old Agent,
   let it drain, stop it, and check again. Never delete or truncate the queue.
   Missing, unreadable, invalid, offline, or unaccounted-for Agents block the
   cutover; do not assume their queues are empty.
3. With all Agent and Center writers stopped, capture one coordinated recovery
   point containing PostgreSQL, attachments, Records authority, and every Agent's
   credentials and buffer. Preserve existing historical `data/logs` in backups.
4. Use the configured scoped migrator to converge C80, then start the matching
   Center and verify `/api/healthz` advertises revision `2` and replay window
   `131072`. Upgrade every Agent while preserving credentials and empty buffers.
   The new Agent checks capabilities before rewriting the queue, creates its
   durable producer/sequence envelope, and fails closed on nonempty legacy data.
5. Start the upgraded Agents and verify current heartbeats, bounded receipts,
   queue drainage, and readable rotated logs. Old-protocol sync must return
   HTTP 426 without refreshing trusted online evidence or writing facts.

After C80, do not roll back to a Center or Agent that cannot understand v2.
Never restore only a database or only an Agent buffer snapshot, and never
renumber a rejected queue or automatically re-enroll to conceal a conflict.
Recovery requires separate authorization, all writers stopped, restoration of
the same coordinated recovery point, and renewed admission/protocol checks.
Local isolated upgrade drills do not establish that a production fleet has
completed this inventory, drain, backup, or upgrade.
