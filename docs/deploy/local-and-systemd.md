# Houfeng V1 Local and systemd Deployment Guide

> Authentication note (added in V1.x): the center now requires a username +
> password login for every API call except `/api/healthz` and `/api/agent/*`.
> First-startup credentials come from `HOUFENG_INITIAL_USERNAME` and
> `HOUFENG_INITIAL_PASSWORD`; once the `users` table is populated those
> variables are ignored. Session cookies are HttpOnly + SameSite=Lax and
> the deployment **must** terminate HTTPS at a reverse proxy to prevent
> cookie leakage on plain HTTP. See the **Authentication** section below.

## Scope

This guide describes the V1 deployment path for `候风 / Houfeng Fleet Control Plane`: one Go center process, one PostgreSQL database, and one or more Go agents managed by systemd.

It intentionally does not introduce Docker, extra queues, TSDBs, or microservices.

## Build artifacts

From the repository root:

```bash
make build-center VERSION=v1.2.3
make build-agent
cd web && npm ci && npm run build
```

Expected local outputs:

- `bin/houfeng-center` stamped with the release version used by generated install commands
- `bin/houfeng-agent`
- `web/dist/`

For one-command agent installation, publish Linux release artifacts for the agent and checksum manifest:

```bash
make build-agent-release VERSION=v1.2.3
```

Expected release outputs under `dist/`:

- `houfeng-agent_v1.2.3_linux_amd64`
- `houfeng-agent_v1.2.3_linux_arm64`
- `sha256sums.txt`

`build-agent-release` stamps the agent heartbeat version with the same `VERSION` value used in the artifact names. Upload those files to the matching GitHub Release tag. The center-served installer script is fetched from the deployed center; GitHub Release is only used for these binary/checksum assets.

## Center environment

Minimum `/etc/houfeng/center.env`:

```dotenv
HOUFENG_HTTP_ADDR=:8080
HOUFENG_WEB_DIST_DIR=/opt/houfeng/web/dist
HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@127.0.0.1:5432/houfeng?sslmode=disable
HOUFENG_PUBLIC_BASE_URL=https://center.example.com
HOUFENG_INCIDENT_SWEEP_INTERVAL=1m
HOUFENG_INITIAL_USERNAME=admin
HOUFENG_INITIAL_PASSWORD=replace-me-with-a-real-password
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=
```

`HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, and `HOUFENG_INITIAL_PASSWORD` are required. `HOUFENG_PUBLIC_BASE_URL` is optional for center startup, but required to generate one-command install commands. Set it to the externally reachable absolute `http(s)` URL that target agents can access, for example `https://center.example.com` or `http://203.0.113.10:8080`; the center normalizes trailing slashes when generating commands. Telegram is disabled unless both Telegram values are set. Use `.env.example` as the full local variable inventory; the systemd snippets below are deployment-shaped examples and must include the required auth seed vars on first startup.

> Note: `HOUFENG_WEB_DIST_DIR` must be set; otherwise center `/` returns 404 and the SPA is unavailable. Production deployments should point it at `web/dist`.

## Local center run

```bash
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
export HOUFENG_PUBLIC_BASE_URL='http://localhost:8080'
export HOUFENG_INITIAL_USERNAME=admin
export HOUFENG_INITIAL_PASSWORD='replace-me-with-a-real-password'
make build-center VERSION=dev-local
./bin/houfeng-center
```

The center applies embedded migrations on startup and serves API plus the built web UI. For production one-command installs, build `houfeng-center` with a real release version (not the default `dev`) so generated install commands point at published agent assets.

## Agent environment

Minimum `/etc/houfeng-agent/agent.env`:

```dotenv
HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=2048
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
```

The token file initially contains an enrollment token issued from the Node onboarding workflow. After the first successful enrollment, `houfeng-agent` replaces it with post-enrollment sync credentials for that Node so service restarts do not reuse the consumed enrollment token. In normal deployments, use the one-command installer from the Node onboarding page instead of manually writing this file.

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

Create service users and install the center runtime files:

```bash
getent passwd houfeng >/dev/null || sudo useradd --system --home-dir /opt/houfeng --shell /usr/sbin/nologin houfeng
sudo install -d -o houfeng -g houfeng -m 0755 /opt/houfeng
sudo install -d -o houfeng -g houfeng -m 0755 /opt/houfeng/web
sudo install -d -o houfeng -g houfeng -m 0755 /opt/houfeng/docs
sudo cp -a web/dist /opt/houfeng/web/
sudo cp -a docs/deploy /opt/houfeng/docs/
sudo install -o root -g root -m 0755 bin/houfeng-center /usr/local/bin/houfeng-center
sudo install -d -o root -g houfeng -m 0750 /etc/houfeng
sudo tee /etc/houfeng/center.env >/dev/null <<'EOF'
HOUFENG_HTTP_ADDR=:8080
HOUFENG_WEB_DIST_DIR=/opt/houfeng/web/dist
HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@127.0.0.1:5432/houfeng?sslmode=disable
HOUFENG_PUBLIC_BASE_URL=https://center.example.com
HOUFENG_INCIDENT_SWEEP_INTERVAL=1m
HOUFENG_INITIAL_USERNAME=admin
HOUFENG_INITIAL_PASSWORD=replace-me-with-a-real-password
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=
EOF
sudo chown root:houfeng /etc/houfeng/center.env
sudo chmod 0640 /etc/houfeng/center.env
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-center.service /etc/systemd/system/houfeng-center.service
sudo systemctl daemon-reload
sudo systemctl enable --now houfeng-center
```

Then create a Node in the web UI and open its Node onboarding workspace. The primary path is the generated one-command installer:

1. Click **生成一键安装命令**.
2. Copy the command shown by the center.
3. Run it once on the target Linux systemd host with root privileges or a sudo-capable account.

The generated command downloads `/api/agent/install.sh` from this center's `HOUFENG_PUBLIC_BASE_URL`, passes a 30-minute one-time enrollment token, and instructs the installer which GitHub Release version/repository to use for the `houfeng-agent` binary. Regenerating a command invalidates the previous token for that Node. If the center was built with the placeholder `dev` version or without `HOUFENG_PUBLIC_BASE_URL`, command generation returns a configuration error instead of guessing. Treat the command as a secret: do not paste it into tickets, chat, screenshots, process logs, or shell transcripts you plan to share.

The installer supports Linux systemd hosts on `amd64` and `arm64`. It fails before writing runtime files when the OS, architecture, service manager, downloader, or checksum tools are unsupported. On success it downloads the release asset, verifies it against `sha256sums.txt`, installs `/usr/local/bin/houfeng-agent`, writes `/etc/houfeng-agent/agent.env`, writes `/etc/houfeng-agent/token` with mode `0600` when the file does not already contain post-enrollment sync credentials, creates `/var/lib/houfeng-agent`, installs the systemd unit, and runs `systemctl daemon-reload` plus `systemctl enable --now houfeng-agent`.

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
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=2048
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
EOF
sudo chown root:houfeng-agent /etc/houfeng-agent/agent.env
sudo chmod 0640 /etc/houfeng-agent/agent.env
printf '%s' '<30-minute-one-time-enrollment-token>' | sudo tee /etc/houfeng-agent/token >/dev/null
sudo chown houfeng-agent:houfeng-agent /etc/houfeng-agent/token
sudo chmod 0600 /etc/houfeng-agent/token
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-agent.service /etc/systemd/system/houfeng-agent.service
sudo systemctl daemon-reload
sudo systemctl enable --now houfeng-agent
```

Adjust users, paths, PostgreSQL URL, TLS/reverse-proxy setup, public center URL, and token file ownership for the target host. Do not enable the agent until `/etc/houfeng-agent/token` contains a valid unexpired enrollment token.

## Authentication

The center protects every `/api/*` route except `/api/healthz` and the agent
endpoints (`/api/agent/enroll`, `/api/agent/sync`, `/api/agent/install.sh`) with a session-cookie auth
layer added in V1.x. Required environment variables:

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `HOUFENG_INITIAL_USERNAME` | yes | — | Admin username seeded on first startup. |
| `HOUFENG_INITIAL_PASSWORD` | yes | — | Admin password seeded on first startup (bcrypt-hashed before persisting). |
| `HOUFENG_INITIAL_DISPLAY_NAME` | no | username | Display name for the seed user. |
| `HOUFENG_SESSION_TTL` | no | `168h` | Rolling session lifetime; refreshed on each authenticated request. |

Behavior:

- On first startup with an empty `users` table, the center creates a single
  admin user from the environment variables.
- On subsequent startups (any rows present in `users`) the variables are
  **ignored**; password rotation is done through the future change-password
  flow, not by re-setting the env var.
- Sessions are stored in the new `sessions` table (migration `0010`) and
  delivered as `HttpOnly` + `SameSite=Lax` cookies named `houfeng_session`.
  A background worker sweeps expired rows hourly.
- Agent enrollment/sync endpoints continue to authenticate via enrollment or sync tokens (independent of the user session layer); `/api/agent/install.sh` is a public read-only installer script and does not carry a token by itself.

### HTTPS requirement

The session cookie does **not** carry the `Secure` attribute in V1.x — the
deployment is expected to terminate HTTPS at a reverse proxy (Caddy, Nginx,
etc.) and forward to `HOUFENG_HTTP_ADDR`. Running the center directly on a
public network without HTTPS will leak the session cookie on plain HTTP and
**must not** be done.

## Reverse proxy and TLS

V1 can run behind a local reverse proxy that terminates TLS and forwards to `HOUFENG_HTTP_ADDR`. The center itself remains the single API/UI process.

## Operational verification

After startup:

```bash
curl -fsS http://127.0.0.1:8080/api/healthz
systemctl status houfeng-center
systemctl status houfeng-agent
journalctl -u houfeng-center -n 100 --no-pager
journalctl -u houfeng-agent -n 100 --no-pager
```

Continue with `docs/operations/v1-smoke-run.md` for the full fresh-install path.
