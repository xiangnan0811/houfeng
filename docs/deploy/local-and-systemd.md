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
make build-center
make build-agent
cd web && npm ci && npm run build
```

Expected outputs:

- `bin/houfeng-center`
- `bin/houfeng-agent`
- `web/dist/`

## Center environment

Minimum `/etc/houfeng/center.env`:

```dotenv
HOUFENG_HTTP_ADDR=:8080
HOUFENG_WEB_DIST_DIR=/opt/houfeng/web/dist
HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@127.0.0.1:5432/houfeng?sslmode=disable
HOUFENG_INCIDENT_SWEEP_INTERVAL=1m
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=
```

`HOUFENG_DATABASE_URL` is required. Telegram is disabled unless both Telegram values are set.

## Local center run

```bash
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
make build-center
./bin/houfeng-center
```

The center applies embedded migrations on startup and serves API plus the built web UI.

## Agent environment

Minimum `/etc/houfeng-agent/agent.env`:

```dotenv
HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=2048
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
```

The token file must contain an enrollment token issued from the Node onboarding workflow.

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
HOUFENG_INCIDENT_SWEEP_INTERVAL=1m
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=
EOF
sudo chown root:houfeng /etc/houfeng/center.env
sudo chmod 0640 /etc/houfeng/center.env
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-center.service /etc/systemd/system/houfeng-center.service
sudo systemctl daemon-reload
sudo systemctl enable --now houfeng-center
```

Then issue an enrollment token from the Node onboarding flow or smoke run, install the agent runtime files, and start the agent:

```bash
getent passwd houfeng-agent >/dev/null || sudo useradd --system --home-dir /var/lib/houfeng-agent --shell /usr/sbin/nologin houfeng-agent
sudo install -d -o houfeng-agent -g houfeng-agent -m 0750 /etc/houfeng-agent
sudo install -d -o houfeng-agent -g houfeng-agent -m 0750 /var/lib/houfeng-agent
sudo install -o root -g root -m 0755 bin/houfeng-agent /usr/local/bin/houfeng-agent
sudo tee /etc/houfeng-agent/agent.env >/dev/null <<'EOF'
HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=2048
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
EOF
sudo chown root:houfeng-agent /etc/houfeng-agent/agent.env
sudo chmod 0640 /etc/houfeng-agent/agent.env
printf '%s' '<enrollment_token>' | sudo tee /etc/houfeng-agent/token >/dev/null
sudo chown houfeng-agent:houfeng-agent /etc/houfeng-agent/token
sudo chmod 0600 /etc/houfeng-agent/token
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-agent.service /etc/systemd/system/houfeng-agent.service
sudo systemctl daemon-reload
sudo systemctl enable --now houfeng-agent
```

Adjust users, paths, PostgreSQL URL, TLS/reverse-proxy setup, and token file ownership for the target host. Do not enable the agent until `/etc/houfeng-agent/token` contains a valid enrollment token.

## Authentication

The center protects every `/api/*` route except `/api/healthz` and the agent
endpoints (`/api/agent/enroll`, `/api/agent/sync`) with a session-cookie auth
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
- Agent endpoints continue to authenticate via enrollment tokens (independent
  of the user session layer).

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
