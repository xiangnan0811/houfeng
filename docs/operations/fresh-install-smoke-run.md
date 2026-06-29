# Houfeng fresh-install smoke run

## Purpose

This smoke run verifies the current first operating path for `候风 / Houfeng Fleet Control Plane`:

1. build and start the center against PostgreSQL;
2. log in with the initial admin user;
3. create a VPS as the business subject;
4. create the VPS-scoped MonitoringInstance from the VPS detail page;
5. generate a center-owned one-command agent install command;
6. enroll/sync an agent;
7. create a Target and ProbeItem;
8. receive observations;
9. trigger and recover an incident;
10. verify events and notification records.

The primary onboarding path starts at the VPS detail page: create or open the VPS, create the VPS-scoped MonitoringInstance there, then use the generated install command from the MonitoringInstance onboarding page or `POST /api/monitoring-instances/{monitoring_instance_id}/install-command`. Manual enrollment-token issuance is kept only as an API/troubleshooting fallback.

## Evidence levels

- **Automated:** repository commands that can run in a complete development environment.
- **Local PostgreSQL required:** live center process and reachable PostgreSQL.
- **Linux systemd agent required:** full one-command installer path on a Linux `amd64` or `arm64` host running systemd.
- **Manual / notification-provider required:** optional Telegram/Feishu delivery evidence or operator-captured UI screenshots.

## Prerequisites

```bash
go version
node --version
npm --version
psql --version
```

Required center environment:

```bash
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
export HOUFENG_DATABASE_REQUIRE_TLS=false
export HOUFENG_PUBLIC_BASE_URL='http://127.0.0.1:8080'
export HOUFENG_INCIDENT_SWEEP_INTERVAL=5s
export HOUFENG_INITIAL_USERNAME=admin
export HOUFENG_INITIAL_PASSWORD='replace-me-with-a-real-password'
export HOUFENG_SESSION_HMAC_KEY='replace-me-with-32-plus-random-bytes'
```

`HOUFENG_PUBLIC_BASE_URL` is required for generated install commands. It must be an externally reachable absolute `http(s)` URL without query or fragment. For production-like one-command testing, build `houfeng-center` with a real release version and ensure the matching GitHub Release contains the Linux agent assets published by the release workflow before generating the command; `VERSION=dev` intentionally makes install-command generation return a configuration error.

Build:

```bash
cd web && npm ci && npm run build
cd ..
make build-center VERSION=v1.2.3
make build-agent-release VERSION=v1.2.3
```

Release assets expected under `dist/`:

- `houfeng-agent_v1.2.3_linux_amd64`
- `houfeng-agent_v1.2.3_linux_arm64`
- `sha256sums.txt`
- `sha256sums.txt.minisig`

Published releases should already contain those files because `.github/workflows/publish-images.yml` uploads them on `release.published`. Use `make build-agent-release VERSION=<tag>` locally as a sanity check or emergency backfill source if a historical release is missing assets, but remember that installable releases also need the signed manifest produced by the release workflow. GitHub Release hosts only the binary and signed-checksum assets; the installer script is served by the running center at `/api/agent/install.sh`.

For API-only local smoke on the same machine, `make build-agent` plus the manual fallback appendix can still verify enroll/sync behavior, but it does not verify the release-asset installer path.

## Start center

```bash
./bin/houfeng-center > /tmp/houfeng-center.log 2>&1 &
CENTER_PID=$!
```

Health check:

```bash
curl -fsS http://127.0.0.1:8080/api/healthz
```

Expected: HTTP 200 JSON response containing center health metadata.

Stop the center at the end with `kill "$CENTER_PID"`. If you also start a local foreground/background agent through the troubleshooting appendix, stop it too.

## Step 0: Log in and keep a session cookie

All `/api/*` routes except `/api/healthz` and `/api/agent/*` require the session cookie. Log in once and reuse the cookie jar for every protected center API call below.

```bash
COOKIE_JAR=/tmp/houfeng-smoke-cookie.txt
curl -fsS -c "$COOKIE_JAR" -b "$COOKIE_JAR" \
  -X POST http://127.0.0.1:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{
    "username": "'"$HOUFENG_INITIAL_USERNAME"'",
    "password": "'"$HOUFENG_INITIAL_PASSWORD"'"
  }'
```

Expected: HTTP 200 JSON response with the current user and a `__Host-houfeng_session` cookie in `$COOKIE_JAR`.

## Step 1: Create a VPS and its scoped MonitoringInstance

```bash
curl -fsS -b "$COOKIE_JAR" -X POST http://127.0.0.1:8080/api/vps \
  -H 'Content-Type: application/json' \
  -d '{
    "display_name": "smoke-vps-01",
    "provider_name": "local",
    "region": "local",
    "city": "local",
    "ipv4": "127.0.0.1",
    "ssh_host": "127.0.0.1",
    "labels": ["smoke", "fresh-install"],
    "note": "fresh-install smoke VPS"
  }'
```

Record the returned `vps_id`, then create the monitoring instance through the VPS-scoped contract:

```bash
curl -fsS -b "$COOKIE_JAR" -X POST http://127.0.0.1:8080/api/vps/<vps_id>/monitoring-instances \
  -H 'Content-Type: application/json' \
  -d '{}'
```

Record the returned `monitoring_instance_id`. The old independent `/api/monitoring-instances` creation path remains an advanced/no-VPS observability path; it is not the normal server onboarding smoke path.

## Step 2: Generate the one-command install command

Preferred UI path:

1. Open `http://127.0.0.1:8080/`.
2. Log in.
3. Open `/vps`, create or open the smoke VPS, then click **创建并接入 agent** from the VPS detail page.
4. After the scoped MonitoringInstance is created, click **生成一键安装命令** and copy the command shown by the center.

API equivalent:

```bash
curl -fsS -b "$COOKIE_JAR" \
  -X POST http://127.0.0.1:8080/api/monitoring-instances/<monitoring_instance_id>/install-command
```

Expected response fields:

- `command`
- `issued_at`
- `expires_at`
- `installer_url`
- `public_base_url`
- `agent_version`
- `release_repo`

The generated command downloads the center-served `/api/agent/install.sh`, passes a 30-minute one-time enrollment token, and tells the installer which GitHub Release repository/version to use for the Linux agent binary. Regenerating the command invalidates the previous active token for that MonitoringInstance.

Treat the full command as secret material. Do not paste it into public issues, screenshots, shared shell transcripts, or long-lived logs.

If this step returns `public base URL is not configured`, set `HOUFENG_PUBLIC_BASE_URL` and restart the center. If it returns `agent release version is not configured`, rebuild the center with a real version such as `make build-center VERSION=v1.2.3` and ensure matching release assets exist.

## Step 3: Run the generated installer on a target host

Run the exact generated command once on a Linux systemd `amd64` or `arm64` host with root privileges or a sudo-capable account.

Expected installer behavior:

- detects Linux/systemd and supported architecture before writing runtime files;
- if `minisign` is missing, the generated command's `--install-missing-deps` flag allows the installer to download upstream `minisign` 0.12, verify the pinned tarball SHA256, and install `/usr/local/bin/minisign`;
- downloads `houfeng-agent_<version>_linux_<amd64|arm64>`, `sha256sums.txt`, and `sha256sums.txt.minisig` from the configured GitHub Release;
- verifies the checksum manifest signature with the installer-pinned public key, then verifies the exact checksum entry before replacing `/usr/local/bin/houfeng-agent` or starting the service;
- writes `/etc/houfeng-agent/agent.env` and `/etc/houfeng-agent/token` with restrictive permissions;
- enables and starts `houfeng-agent` with systemd.

Expected runtime behavior:

- agent enrolls through `/api/agent/enroll`;
- subsequent syncs call `/api/agent/sync`;
- monitoring instance onboarding state changes from `未绑定` toward `已绑定` / `接入完成` after accepted observations.

Check from the center machine:

```bash
curl -fsS -b "$COOKIE_JAR" http://127.0.0.1:8080/api/monitoring-instances/<monitoring_instance_id>/onboarding
curl -fsS -b "$COOKIE_JAR" http://127.0.0.1:8080/api/monitoring-instances/<monitoring_instance_id>/runtime-facts
```

On the agent host:

```bash
systemctl status houfeng-agent
journalctl -u houfeng-agent -n 100 --no-pager
```

## Step 4: Create a Target

```bash
curl -fsS -b "$COOKIE_JAR" -X POST http://127.0.0.1:8080/api/targets \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "smoke-target-localhost",
    "target_type": "service",
    "host": "127.0.0.1",
    "base_port": 8080,
    "run_status": "启用",
    "group": "smoke",
    "labels": ["smoke", "fresh-install"],
    "execution_monitoring_instance_labels": ["smoke"],
    "note": "fresh-install smoke target"
  }'
```

Record the returned `target_id`.

## Step 5: Add a ProbeItem

```bash
curl -fsS -b "$COOKIE_JAR" -X POST http://127.0.0.1:8080/api/targets/<target_id>/probe-items \
  -H 'Content-Type: application/json' \
  -d '{
    "probe_kind": "http",
    "enabled": true,
    "frequency_tier": "5s",
    "timeout_seconds": 3,
    "config": {
      "scheme": "http",
      "path": "/api/healthz",
      "method": "GET",
      "expected_status_range": [200, 299]
    }
  }'
```

Expected: the target detail page and runtime facts show probe observations on the next due sync. Supported frequency tiers are `5s`, `1m`, `5m`, `15m`, and `6h`.

Check:

```bash
curl -fsS -b "$COOKIE_JAR" http://127.0.0.1:8080/api/targets/<target_id>/runtime-facts
```

## Step 6: Trigger an incident

One safe local method is to change the ProbeItem to a failing path or stop the service it checks, then wait for the incident sweep interval.

Check active incidents and events:

```bash
curl -fsS -b "$COOKIE_JAR" 'http://127.0.0.1:8080/api/incidents?object_type=target&object_id=<target_id>&limit=10'
curl -fsS -b "$COOKIE_JAR" 'http://127.0.0.1:8080/api/events?object_type=target&object_id=<target_id>&event_type=incident_started&limit=10'
```

Expected:

- an active target incident appears after repeated failing observations;
- the event response is an object with an `items` array, and `items[]` contains an `incident_started` event;
- notification delivery is attempted only if notification settings are configured and policy allows it.

## Step 7: Recover the incident

Restore the ProbeItem or checked service, wait for successful observations and the incident sweep interval, then check:

```bash
curl -fsS -b "$COOKIE_JAR" 'http://127.0.0.1:8080/api/incidents?object_type=target&object_id=<target_id>&limit=10'
curl -fsS -b "$COOKIE_JAR" 'http://127.0.0.1:8080/api/events?object_type=target&object_id=<target_id>&event_type=incident_recovered&limit=10'
```

Expected:

- active incident clears or moves out of the active list;
- the event response is an object with an `items` array, and `items[]` contains an `incident_recovered` event;
- recovery notification follows current settings.

## Step 8: Verify notification-backed events

Notification-backed event records are visible through the event surface whenever an incident transition created a `notification_records` row. This includes sent, failed, and policy-suppressed notification records; `notification_only=true` does not mean “Telegram/Feishu send succeeded”.

```bash
curl -fsS -b "$COOKIE_JAR" 'http://127.0.0.1:8080/api/events?object_type=target&object_id=<target_id>&notification_only=true&limit=10'
```

Expected:

- if the incident transition emitted a notification decision, the response is an object with an `items` array containing notification-backed event rows even when delivery was suppressed or failed;
- if `items` is empty, verify whether the transition produced no notification decision for the current policy/state before treating the result as expected.

## Step 9: UI verification checkpoints

Open `http://127.0.0.1:8080/` and check the current UI surfaces that are relevant to the smoke:

- Dashboard shows the current workbench and abnormal/asset decision summaries without pretending the smoke proves production health.
- VPS detail shows the smoke VPS identity, billing/observability evidence, and the scoped MonitoringInstance relationship.
- Monitoring page shows the smoke monitoring instance as observation evidence and visible onboarding/binding status.
- MonitoringInstance onboarding page shows generated-command metadata, token expiry, and bound/conflict state truthfully.
- MonitoringInstance detail shows latest host sample and runtime evidence once the agent has synced.
- Targets page shows the smoke target.
- Target detail shows ProbeItem, latest probe observation, incident context, and recent trend evidence.
- Events page filters by object, time, severity/type, notification-only, recovery-only, maintenance-only, and explicit backfilled-event opt-in.
- Settings page shows effective defaults and notification/retention behavior truthfully.

For broader frontend checks, use the local preview and browser-sanity workflow in `docs/operations/ui-preview-and-browser-sanity.md` rather than removed historical screenshot flows.

### Troubleshooting: `minisign is required to verify release checksums`

If the installer exits with `houfeng-agent install: minisign is required to verify release checksums`, first inspect the command you copied. A command generated by the stale `v0.55.0` center looks like this in the installer argument list:

```sh
sudo sh "$tmp_installer" --server-url 'https://center.example.com' --enrollment-token-stdin --version 'v0.55.0' --release-repo 'xiangnan0811/houfeng'
```

That command is missing `--install-missing-deps`, and the `v0.55.0` script served from `/api/agent/install.sh` does not know how to bootstrap `minisign`. Debian 11 bullseye can also return `E: Unable to locate package minisign`, so `apt install minisign` is not a reliable smoke-run fix.

Use this recovery path:

1. Discard the old generated command. Do not paste the old enrollment token into a hand-edited command.
2. Upgrade the center to `v0.55.1` or newer, preferably the latest published patch release, and restart it.
3. Verify the center now serves a fixed installer:

   ```bash
   curl -fsSL https://center.example.com/api/agent/install.sh | grep -- '--install-missing-deps'
   ```

4. Regenerate the MonitoringInstance install/upgrade command from the fixed center.
5. Confirm the regenerated command contains `--install-missing-deps` and a non-stale `--version`.
6. Run the regenerated command on the target host.

Do not work around this by disabling signed checksum verification, editing `sha256sums.txt`, using unsigned binaries, or adding `--install-missing-deps` to a command that still downloads the old `v0.55.0` installer. The fixed installer keeps signed manifest verification enabled and only bootstraps the missing verifier after checking the pinned upstream tarball SHA256.

## Troubleshooting fallback: manual enrollment token

Use this only when debugging the installer or doing API-level local verification. It does not verify release artifact download or checksum behavior.

Issue a raw enrollment token:

```bash
curl -fsS -b "$COOKIE_JAR" -X POST http://127.0.0.1:8080/api/monitoring-instances/<monitoring_instance_id>/enrollment-token
```

The response key is `token`. Store it in a private token file:

```bash
printf '%s' '<enrollment_token>' > /tmp/houfeng-agent-token
chmod 0600 /tmp/houfeng-agent-token
```

Run a locally built agent:

```bash
export HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080
export HOUFENG_AGENT_TOKEN_FILE=/tmp/houfeng-agent-token
install -d -m 0700 /tmp/houfeng-agent
export HOUFENG_AGENT_BUFFER_FILE=/tmp/houfeng-agent/sync-buffer.json
export HOUFENG_AGENT_BUFFER_MAX_BYTES=67108864
make build-agent
./bin/houfeng-agent > /tmp/houfeng-agent.log 2>&1 &
AGENT_PID=$!
```

After the first successful enrollment, the agent replaces the enrollment token file with post-enrollment sync credentials for that MonitoringInstance. Do not reuse a consumed token for another host.

## Historical evidence snapshot

Earlier live PostgreSQL smoke evidence remains useful as history, but it should not be read as a current installer run:

- 2026-04-29: first full live path evidence with manual token enrollment; Telegram was intentionally disabled.
- 2026-05-02: auth-protected smoke path passed for node/target/probe/incident/event surfaces; local macOS host-sample collection was partial at the time.
- 2026-05-03 follow-up: Darwin hostsample collector was added and a short rerun produced a host sample.

For new evidence, record date, center version, database scope, onboarding path (`install-command` or manual fallback), data source, notification configuration, and any limitations. Do not record full enrollment tokens, sync tokens, passwords, cookies, webhook URLs, or real provider/customer secrets.
