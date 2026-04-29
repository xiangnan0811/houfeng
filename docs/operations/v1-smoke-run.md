# Houfeng V1 Fresh-Install Smoke Run

## Purpose

This smoke run verifies the first V1 operating path for `候风 / Houfeng Fleet Control Plane`:

1. create a Node;
2. enroll an agent;
3. create a Target;
4. add a ProbeItem;
5. receive observations;
6. trigger and recover an incident;
7. verify events and notification records.

## Evidence levels

- **Automated:** can be verified by repository commands in any complete development environment.
- **Local PostgreSQL required:** requires a running PostgreSQL instance and live center process.
- **Manual / Telegram required:** requires optional Telegram credentials or an operator-captured UI screenshot.

## Prerequisites

```bash
go version
node --version
npm --version
psql --version
```

Required environment:

```bash
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
```

Run the center and agent in separate terminals, or use the background commands below from one shell. If using the background form, stop both processes at the end with `kill "$CENTER_PID" "$AGENT_PID"` after `AGENT_PID` has been set.

Build:

```bash
make build-center
make build-agent
cd web && npm ci && npm run build
```

Start center in the background:

```bash
./bin/houfeng-center > /tmp/houfeng-center.log 2>&1 &
CENTER_PID=$!
```

Health check:

```bash
curl -fsS http://127.0.0.1:8080/api/healthz
```

Expected: HTTP 200 JSON response containing service/version health metadata.

## Step 1: Create a Node

```bash
curl -fsS -X POST http://127.0.0.1:8080/api/nodes \
  -H 'Content-Type: application/json' \
  -d '{
    "display_name": "smoke-node-01",
    "provider": "local",
    "region": "local",
    "city": "local",
    "labels": ["smoke", "v1"],
    "note": "V1 smoke node"
  }'
```

Record the returned `node_id`.

## Step 2: Issue an enrollment token

```bash
curl -fsS -X POST http://127.0.0.1:8080/api/nodes/<node_id>/enrollment-token
```

Record the returned plaintext token once. Store it for the local agent:

```bash
printf '%s' '<enrollment_token>' > /tmp/houfeng-agent-token
chmod 0600 /tmp/houfeng-agent-token
```

## Step 3: Enroll and run an agent

```bash
export HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080
export HOUFENG_AGENT_TOKEN_FILE=/tmp/houfeng-agent-token
export HOUFENG_AGENT_BUFFER_FILE=/tmp/houfeng-agent-sync-buffer.json
./bin/houfeng-agent > /tmp/houfeng-agent.log 2>&1 &
AGENT_PID=$!
```

Expected:

- agent enrolls through `/api/agent/enroll`;
- subsequent syncs call `/api/agent/sync`;
- node onboarding state changes from unbound toward bound/running.

Check:

```bash
curl -fsS http://127.0.0.1:8080/api/nodes/<node_id>/onboarding
curl -fsS http://127.0.0.1:8080/api/nodes/<node_id>/runtime-facts
```

## Step 4: Create a Target

```bash
curl -fsS -X POST http://127.0.0.1:8080/api/targets \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "smoke-target-localhost",
    "target_type": "service",
    "host": "127.0.0.1",
    "base_port": 8080,
    "run_status": "启用",
    "labels": ["smoke", "v1"],
    "execution_node_labels": ["smoke"],
    "note": "V1 smoke target"
  }'
```

Record the returned `target_id`.

## Step 5: Add a ProbeItem

```bash
curl -fsS -X POST http://127.0.0.1:8080/api/targets/<target_id>/probe-items \
  -H 'Content-Type: application/json' \
  -d '{
    "probe_kind": "http",
    "enabled": true,
    "frequency_tier": "1m",
    "timeout_seconds": 3,
    "config": {
      "scheme": "http",
      "path": "/api/healthz",
      "method": "GET",
      "expected_status_range": [200, 299]
    }
  }'
```

Expected: the target detail page and runtime facts eventually show probe observations. The supported frequency tiers are `1m`, `5m`, `15m`, and `6h`.

Check:

```bash
curl -fsS http://127.0.0.1:8080/api/targets/<target_id>/runtime-facts
```

## Step 6: Trigger an incident

One safe local method is to change the ProbeItem to a failing path or stop the service it checks, then wait for the incident sweep interval.

Check active incidents:

```bash
curl -fsS 'http://127.0.0.1:8080/api/incidents?object_type=target&object_id=<target_id>&limit=10'
curl -fsS 'http://127.0.0.1:8080/api/events?object_type=target&object_id=<target_id>&event_type=incident_started&limit=10'
```

Expected:

- an active target incident appears after repeated failing observations;
- an `incident_started` event is recorded;
- Telegram notification is sent only if Telegram settings are configured and notification policy allows it.

## Step 7: Recover the incident

Restore the ProbeItem or checked service, wait for successful observations and the incident sweep interval, then check:

```bash
curl -fsS 'http://127.0.0.1:8080/api/incidents?object_type=target&object_id=<target_id>&limit=10'
curl -fsS 'http://127.0.0.1:8080/api/events?object_type=target&object_id=<target_id>&event_type=incident_recovered&limit=10'
```

Expected:

- active incident clears or moves out of the active list;
- an `incident_recovered` event is recorded;
- recovery notification follows current settings.

## Step 8: Verify notification record surface

If Telegram is configured and notification policy allows delivery, notification-backed event records should be visible through the event surface:

```bash
curl -fsS 'http://127.0.0.1:8080/api/events?object_type=target&object_id=<target_id>&notification_only=true&limit=10'
```

Expected:

- if Telegram settings are enabled and the incident transition emitted a notification, the response contains notification-backed event rows;
- if Telegram is disabled or policy suppressed delivery, record that intentional empty result in the evidence table instead of treating it as a failure.

## Step 9: UI verification checkpoints

Open `http://127.0.0.1:8080/` and check:

- Dashboard shows fleet health, abnormal summaries, and event stream.
- Nodes page shows the smoke node.
- Node onboarding page shows bound state or any binding conflict truthfully.
- Node detail shows latest host sample and recent trend summary.
- Targets page shows the smoke target.
- Target detail shows ProbeItem, latest probe observation, active/recovered incident context, and recent latency trend.
- Events page filters by object, time, label, notification-only, recovery-only, and maintenance-only controls.
- Settings page shows effective defaults and Telegram/retention behavior truthfully.

## Evidence table

| Check | Evidence level | Result field |
| --- | --- | --- |
| `go test ./...` | Automated | Fill with command output summary |
| `./scripts/verify.sh` | Automated | Fill with command output summary |
| center starts and `/api/healthz` returns 200 | Local PostgreSQL required | Fill after live run |
| Node created | Local PostgreSQL required | Fill returned `node_id` |
| enrollment token issued | Local PostgreSQL required | Fill timestamp only, not token |
| agent enrolls and syncs | Local PostgreSQL required | Fill log excerpt |
| Target created | Local PostgreSQL required | Fill returned `target_id` |
| ProbeItem created | Local PostgreSQL required | Fill returned probe id |
| observations received | Local PostgreSQL required | Fill runtime-facts summary |
| incident started | Local PostgreSQL required | Fill event id / incident id |
| incident recovered | Local PostgreSQL required | Fill event id |
| notification-backed event query checked | Local PostgreSQL / Telegram policy dependent | Fill `/api/events?...notification_only=true` result summary |
| Telegram notification sent or intentionally disabled | Manual / Telegram required | Fill delivery result or intentional disabled status |
| primary UI pages checked | Manual | Fill screenshots or notes |

## Current session status

This file is a reproducible smoke procedure. Unless a later commit fills the evidence table with actual command output, live PostgreSQL, agent, incident, notification, and screenshot evidence should be treated as pending manual evidence rather than completed proof.
