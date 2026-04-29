# Houfeng V1 Delivery Verification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the missing delivery, smoke-run, visual-verification, and final V1 gap artifacts needed to operate and close Houfeng V1 without changing frozen product behavior.

**Architecture:** Keep Phase 6 as a documentation/evidence closure slice. Use the existing Go center, Go agent, React/Vite frontend, PostgreSQL, systemd-agent architecture, and verification scripts. Add deployment examples and reproducible evidence documents that reference implemented commands, environment variables, API routes, and Unified / Baseline visual assets.

**Tech Stack:** Go binaries, PostgreSQL, systemd unit examples, React/Vite static assets, existing shell verification scripts, Markdown delivery documentation

---

## Scope Guard

This plan must not:

- redesign V1 product, interaction, or visual behavior;
- add Docker as the primary deployment path;
- add dependencies, services, queues, TSDBs, or rule engines;
- claim live PostgreSQL smoke, Telegram delivery, or screenshot comparison was executed unless the implementing agent actually runs and records evidence.

If implementation discovers a runtime blocker, record it in the gap checklist and stop before changing product behavior.

## Planned File Structure

### Deployment artifacts

- Modify: `.env.example` — document all implemented center/agent environment variables used by examples.
- Create: `docs/deploy/local-and-systemd.md` — local build/run and systemd deployment guide.
- Create: `docs/deploy/systemd/houfeng-center.service` — center service example.
- Create: `docs/deploy/systemd/houfeng-agent.service` — agent service example.

### Smoke, visual, and release evidence

- Create: `docs/operations/v1-smoke-run.md` — fresh-install smoke procedure and evidence table.
- Create: `docs/operations/v1-visual-verification.md` — visual baseline reference, capture commands, and current evidence status.
- Create: `docs/release/v1-gap-checklist.md` — final V1 gap checklist with closed/partial/deferred classifications.
- Modify: `README.md` — link the new delivery/operation artifacts from the repository entry point.

---

### Task 1: Add deployment guide, env contract, and systemd examples

**Files:**
- Modify: `.env.example`
- Create: `docs/deploy/local-and-systemd.md`
- Create: `docs/deploy/systemd/houfeng-center.service`
- Create: `docs/deploy/systemd/houfeng-agent.service`

- [ ] **Step 1: Inspect implemented env variables and binary names**

Run:

```bash
sed -n '1,220p' internal/center/config/config.go
sed -n '1,180p' agent/config/config.go
sed -n '1,120p' Makefile
sed -n '1,120p' cmd/houfeng-center/main.go
sed -n '1,120p' cmd/houfeng-agent/main.go
```

Expected:

- center binary target is `./bin/houfeng-center`;
- agent binary target is `./bin/houfeng-agent`;
- center requires `HOUFENG_DATABASE_URL`;
- center supports `HOUFENG_HTTP_ADDR`, `HOUFENG_WEB_DIST_DIR`, `HOUFENG_INCIDENT_SWEEP_INTERVAL`, `HOUFENG_TELEGRAM_BOT_TOKEN`, and `HOUFENG_TELEGRAM_CHAT_ID`;
- agent requires `HOUFENG_AGENT_SERVER_URL` and `HOUFENG_AGENT_TOKEN_FILE`;
- agent supports `HOUFENG_AGENT_BUFFER_FILE`, `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`, and `HOUFENG_AGENT_BUFFER_MAX_AGE`.

- [ ] **Step 2: Update `.env.example`**

Replace the file with this complete example:

```dotenv
# Local development example; replace these values before deployment.

# Center
HOUFENG_HTTP_ADDR=:8080
HOUFENG_WEB_DIST_DIR=web/dist
HOUFENG_DATABASE_URL=postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable
HOUFENG_INCIDENT_SWEEP_INTERVAL=1m

# Optional Telegram delivery. Leave both empty to disable outbound Telegram sends.
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=

# Agent
HOUFENG_AGENT_SERVER_URL=http://localhost:8080
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=2048
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
```

- [ ] **Step 3: Add center systemd unit example**

Create `docs/deploy/systemd/houfeng-center.service`:

```ini
[Unit]
Description=Houfeng Fleet Control Plane center
Documentation=file:/opt/houfeng/docs/deploy/local-and-systemd.md
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=houfeng
Group=houfeng
WorkingDirectory=/opt/houfeng
EnvironmentFile=/etc/houfeng/center.env
ExecStart=/usr/local/bin/houfeng-center
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full
ReadWritePaths=/opt/houfeng

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 4: Add agent systemd unit example**

Create `docs/deploy/systemd/houfeng-agent.service`:

```ini
[Unit]
Description=Houfeng Fleet Control Plane agent
Documentation=file:/opt/houfeng/docs/deploy/local-and-systemd.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=houfeng-agent
Group=houfeng-agent
EnvironmentFile=/etc/houfeng-agent/agent.env
ExecStart=/usr/local/bin/houfeng-agent
Restart=always
RestartSec=10s
StateDirectory=houfeng-agent
ReadWritePaths=/var/lib/houfeng-agent
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 5: Add deployment guide**

Create `docs/deploy/local-and-systemd.md` with these sections and concrete content:

```markdown
# Houfeng V1 Local and systemd Deployment Guide

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
export HOUFENG_AGENT_BUFFER_FILE=/tmp/houfeng-agent-sync-buffer.json
make build-agent
./bin/houfeng-agent
```

## systemd installation example

```bash
sudo install -o root -g root -m 0755 bin/houfeng-center /usr/local/bin/houfeng-center
sudo install -o root -g root -m 0755 bin/houfeng-agent /usr/local/bin/houfeng-agent
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-center.service /etc/systemd/system/houfeng-center.service
sudo install -o root -g root -m 0644 docs/deploy/systemd/houfeng-agent.service /etc/systemd/system/houfeng-agent.service
sudo systemctl daemon-reload
sudo systemctl enable --now houfeng-center
sudo systemctl enable --now houfeng-agent
```

Adjust users, paths, PostgreSQL URL, TLS/reverse-proxy setup, and token file ownership for the target host.

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
```

- [ ] **Step 6: Validate references**

Run:

```bash
grep -R "HOUFENG_" .env.example docs/deploy/local-and-systemd.md docs/deploy/systemd
grep -R "houfeng-center\\|houfeng-agent" docs/deploy/local-and-systemd.md docs/deploy/systemd
```

Expected: only implemented env vars and current binary names appear.

- [ ] **Step 7: Commit Task 1**

```bash
git add .env.example docs/deploy/local-and-systemd.md docs/deploy/systemd/houfeng-center.service docs/deploy/systemd/houfeng-agent.service
git commit -m "Document deployable V1 systemd path" -m "Houfeng V1 needs operator-facing deployment examples before final delivery verification. This adds the implemented env contract, local run guidance, and systemd fixtures for the Go center and Go agent without changing runtime behavior.

Constraint: V1 deployment is Go center plus PostgreSQL plus systemd agents; Docker is not the primary path.
Rejected: Add generated installer scripts | premature for V1 and harder to verify safely than static examples.
Confidence: high
Scope-risk: narrow
Tested: Grep validation for documented env vars and binary names.
Not-tested: Unit files were not installed on a live host."
```

---

### Task 2: Add fresh-install smoke-run documentation

**Files:**
- Create: `docs/operations/v1-smoke-run.md`

- [ ] **Step 1: Inspect API helper paths used by the frontend**

Run:

```bash
sed -n '1,360p' web/src/lib/api.ts
sed -n '1,240p' internal/contracts/agentapi/routes.go
sed -n '1,260p' internal/contracts/agentapi/types.go
```

Expected:

- admin Node path: `/api/nodes`;
- node enrollment token path: `/api/nodes/:nodeId/enrollment-token`;
- agent paths: `/api/agent/enroll` and `/api/agent/sync`;
- target path: `/api/targets`;
- probe path: `/api/targets/:targetId/probe-items`;
- event and incident paths: `/api/events` and `/api/incidents`.

- [ ] **Step 2: Create smoke-run guide**

Create `docs/operations/v1-smoke-run.md`:

```markdown
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

Build:

```bash
make build-center
make build-agent
cd web && npm ci && npm run build
```

Start center:

```bash
./bin/houfeng-center
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
./bin/houfeng-agent
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
    "name": "smoke-http-health",
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

## Step 8: UI verification checkpoints

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
| Telegram notification sent or intentionally disabled | Manual / Telegram required | Fill delivery result |
| primary UI pages checked | Manual | Fill screenshots or notes |

## Current session status

This file is a reproducible smoke procedure. Unless a later commit fills the evidence table with actual command output, live PostgreSQL, agent, incident, notification, and screenshot evidence should be treated as pending manual evidence rather than completed proof.
```

- [ ] **Step 3: Commit Task 2**

```bash
git add docs/operations/v1-smoke-run.md
git commit -m "Make the V1 fresh-install smoke path reproducible" -m "Final delivery verification needs a concrete operator path from a blank install through node enrollment, target probing, incident transition, events, and optional notification delivery. This documents the path against implemented API routes and distinguishes automated proof from environment-dependent evidence.

Constraint: Live smoke requires PostgreSQL and optional Telegram credentials that may not exist in every agent session.
Rejected: Mark smoke as complete from unit tests alone | V1 delivery needs an executable operator procedure.
Confidence: high
Scope-risk: narrow
Tested: Smoke paths cross-checked against frontend API helpers and agent route constants.
Not-tested: Live PostgreSQL/agent/Telegram smoke execution is recorded as manual evidence until run."
```

---

### Task 3: Add visual verification and V1 gap checklist

**Files:**
- Create: `docs/operations/v1-visual-verification.md`
- Create: `docs/release/v1-gap-checklist.md`

- [ ] **Step 1: Inspect visual baseline registry and current frontend routes**

Run:

```bash
sed -n '1,220p' docs/design/v1-baseline/baseline-screens.md
sed -n '1,120p' web/src/app/router.tsx
find docs/design/v1-baseline/stitch -maxdepth 2 -name screen.png | sort
```

Expected:

- primary baseline screens include Global App Shell, Global Control Center, Fleet Nodes List, Node Detail Center, and Node Onboarding & Binding Conflict;
- supporting screens include Security Audit & Events, Global Logs Explorer, System Configuration;
- current routes include dashboard, nodes, node onboarding/detail, targets, target detail, events, and settings.

- [ ] **Step 2: Add visual verification document**

Create `docs/operations/v1-visual-verification.md`:

```markdown
# Houfeng V1 Visual Verification

## Authority

The only visual authority for V1 is the Unified / Baseline Stitch export under `docs/design/v1-baseline/`:

- `docs/design/v1-baseline/handoff.md`
- `docs/design/v1-baseline/ui-ux-spec.md`
- `docs/design/v1-baseline/visual-review-round2.md`
- `docs/design/v1-baseline/baseline-screens.md`
- `docs/design/v1-baseline/stitch/**/screen.png`

Legacy concept screens are retained for history and must not override the current baseline.

## Primary baseline coverage

| Baseline screen | Reference image | Implementation route | V1 status |
| --- | --- | --- | --- |
| Global App Shell Baseline | `docs/design/v1-baseline/stitch/global_app_shell_baseline_obsidian_core/screen.png` | all app routes through `web/src/app/layout/AppShell.tsx` | Requires screenshot comparison |
| Global Control Center (Unified) | `docs/design/v1-baseline/stitch/global_control_center_unified/screen.png` | `/` | Requires screenshot comparison |
| Fleet Nodes List | `docs/design/v1-baseline/stitch/fleet_nodes_list/screen.png` | `/nodes` | Requires screenshot comparison |
| Node Detail Center (Unified) | `docs/design/v1-baseline/stitch/node_detail_center_unified/screen.png` | `/nodes/:nodeId` | Requires screenshot comparison with seeded data |
| Node Onboarding & Binding Conflict (Unified) | `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/screen.png` | `/nodes/:nodeId/onboarding` | Requires screenshot comparison with seeded conflict state |

## Supporting coverage

| Baseline screen | Reference image | Implementation route | V1 status |
| --- | --- | --- | --- |
| Security Audit & Events | `docs/design/v1-baseline/stitch/security_audit_events/screen.png` | `/events` | Requires screenshot comparison |
| Global Logs Explorer | `docs/design/v1-baseline/stitch/global_logs_explorer/screen.png` | `/events` as event explorer surface | Supporting reference only |
| System Configuration | `docs/design/v1-baseline/stitch/system_configuration/screen.png` | `/settings` | Requires screenshot comparison |
| Target Detail | `docs/design/v1-baseline/stitch/target_details_blog.example.com/screen.png` | `/targets/:targetId` | Legacy-but-usable reference until a unified target detail baseline exists |

## Reproducible capture path

Build and run the app:

```bash
cd web && npm ci && npm run build
cd ..
export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
./bin/houfeng-center
```

Capture screenshots with a browser automation tool at viewport `1440x1024` after seeding smoke data:

```bash
mkdir -p docs/operations/visual-evidence
# Capture route screenshots for:
# /, /nodes, /nodes/<node_id>, /nodes/<node_id>/onboarding,
# /targets/<target_id>, /events, /settings
```

Compare each captured screenshot with the matching reference PNG. Record results in this table:

| Route | Captured screenshot | Reference screenshot | Verdict | Notes |
| --- | --- | --- | --- | --- |
| `/` | `docs/operations/visual-evidence/dashboard.png` | `docs/design/v1-baseline/stitch/global_control_center_unified/screen.png` | Pending live capture | Requires seeded data |
| `/nodes` | `docs/operations/visual-evidence/nodes.png` | `docs/design/v1-baseline/stitch/fleet_nodes_list/screen.png` | Pending live capture | Requires seeded node |
| `/nodes/<node_id>` | `docs/operations/visual-evidence/node-detail.png` | `docs/design/v1-baseline/stitch/node_detail_center_unified/screen.png` | Pending live capture | Requires runtime facts/incidents |
| `/nodes/<node_id>/onboarding` | `docs/operations/visual-evidence/node-onboarding.png` | `docs/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/screen.png` | Pending live capture | Requires onboarding or conflict state |
| `/events` | `docs/operations/visual-evidence/events.png` | `docs/design/v1-baseline/stitch/security_audit_events/screen.png` | Pending live capture | Requires event stream |
| `/settings` | `docs/operations/visual-evidence/settings.png` | `docs/design/v1-baseline/stitch/system_configuration/screen.png` | Pending live capture | Requires settings page |
| `/targets/<target_id>` | `docs/operations/visual-evidence/target-detail.png` | `docs/design/v1-baseline/stitch/target_details_blog.example.com/screen.png` | Pending live capture | Legacy reference |

## Current evidence status

This document records the authoritative comparison set and reproducible capture path. If no `docs/operations/visual-evidence/*.png` files are committed, visual verification remains a tracked evidence gap rather than completed visual proof.
```

- [ ] **Step 3: Add final V1 gap checklist**

Create `docs/release/v1-gap-checklist.md`:

```markdown
# Houfeng V1 Gap Checklist

## Scope

This checklist compares the implementation repository against the frozen V1 baseline. It does not revise the baseline.

Status values:

- **Closed:** implemented and covered by automated or documented evidence.
- **Partial:** implemented or documented, but final live evidence is still required.
- **Deferred outside V1:** intentionally not part of frozen V1 delivery.

## Product and architecture baseline

| Area | Status | Evidence |
| --- | --- | --- |
| Product naming is `候风 / Houfeng Fleet Control Plane` | Closed | `README.md`, binary names, design handoff |
| Go center + Go agent + React/Vite + PostgreSQL | Closed | `go.mod`, `cmd/houfeng-center`, `cmd/houfeng-agent`, `web/package.json`, `db/migrations` |
| Single center process owns API/UI/background workers/notifications | Closed | `cmd/houfeng-center/bootstrap.go` |
| systemd agent direction documented | Closed | `docs/deploy/systemd/houfeng-agent.service` |
| Docker-first deployment | Deferred outside V1 | Frozen tech selection excludes Docker as required runtime |

## Core object model

| Area | Status | Evidence |
| --- | --- | --- |
| Node persistence and UI | Closed | `internal/center/store/nodes.go`, `web/src/pages/NodesPage.tsx` |
| Target persistence and UI | Closed | `internal/center/store/targets.go`, `web/src/pages/TargetsPage.tsx` |
| ProbeItem persistence and UI | Closed | `internal/center/store/targets.go`, `web/src/pages/TargetDetailPage.tsx` |
| HostSample and ProbeObservation ingestion | Closed | `internal/center/observations`, `internal/center/syncing`, `agent/hostsample`, `agent/probe` |
| Incident and Event model | Closed | `internal/center/incidents`, `internal/center/store/dashboard.go`, `web/src/pages/EventsPage.tsx` |

## Runtime behavior

| Area | Status | Evidence |
| --- | --- | --- |
| Node enrollment and binding state | Closed | `internal/center/enrollment`, `web/src/pages/NodeOnboardingPage.tsx` |
| Agent durable sync buffer | Closed | `agent/syncqueue`, `agent/runtime/runtime.go` |
| Node pause/maintenance/retire sync semantics | Closed | `internal/center/store/agent_plan.go`, runtime control tests |
| Target pause/maintenance/archive semantics | Closed | `internal/center/http/handlers/runtime_controls.go`, target page tests |
| Retention and daily aggregation execution | Closed | `internal/center/retention`, `internal/center/store/retention.go` |
| Trend degradation incident families | Closed | `internal/center/incidents/evaluator.go` |

## UI and interaction surfaces

| Area | Status | Evidence |
| --- | --- | --- |
| Frozen app shell and primary navigation | Closed | `web/src/app/layout/AppShell.tsx`, `web/src/app/router.tsx` |
| Dashboard abnormal summaries and event stream | Closed | `web/src/pages/DashboardPage.tsx` |
| Nodes list filters and onboarding entry | Closed | `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx` |
| Node detail operational summary and trends | Closed | `web/src/pages/NodeDetailPage.tsx` |
| Target list/detail and ProbeItem management | Closed | `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx` |
| Events advanced filters | Closed | `web/src/pages/EventsPage.tsx` |
| Settings runtime truthfulness | Closed | `web/src/pages/SettingsPage.tsx`, `internal/center/settings` |
| Visual screenshot comparison against baseline PNGs | Partial | `docs/operations/v1-visual-verification.md`; live screenshot evidence pending unless PNG evidence is later committed |

## Notifications

| Area | Status | Evidence |
| --- | --- | --- |
| Telegram notifier implementation | Closed | `internal/center/notify/telegram.go` |
| Settings-aware notification policy | Closed | `internal/center/incidents/service.go`, settings tests |
| Live Telegram delivery evidence | Partial | Requires operator credentials; smoke guide records evidence path |

## Delivery and operations

| Area | Status | Evidence |
| --- | --- | --- |
| Local build/test verification path | Closed | `Makefile`, `scripts/verify.sh` |
| systemd examples for center and agent | Closed | `docs/deploy/systemd/*.service` |
| Deployment guide | Closed | `docs/deploy/local-and-systemd.md` |
| Fresh-install smoke procedure | Closed | `docs/operations/v1-smoke-run.md` |
| Fresh-install smoke executed on live PostgreSQL | Partial | Requires live PostgreSQL and agent run; evidence table remains pending until filled |

## Final V1 release gate

Before tagging or declaring V1 fully release-ready, collect:

- passing `go test ./...`;
- passing `./scripts/verify.sh`;
- passing `cd web && npm run build`;
- completed live PostgreSQL smoke table in `docs/operations/v1-smoke-run.md`;
- visual screenshot comparison artifacts or an explicit accepted waiver for pending screenshot evidence;
- Telegram delivery proof or an explicit note that Telegram is disabled for the deployment.
```

- [ ] **Step 4: Validate baseline paths**

Run:

```bash
while read -r path; do test -e "$path" || { echo "missing $path"; exit 1; }; done <<'EOF'
docs/design/v1-baseline/stitch/global_app_shell_baseline_obsidian_core/screen.png
docs/design/v1-baseline/stitch/global_control_center_unified/screen.png
docs/design/v1-baseline/stitch/fleet_nodes_list/screen.png
docs/design/v1-baseline/stitch/node_detail_center_unified/screen.png
docs/design/v1-baseline/stitch/node_onboarding_binding_conflict_unified/screen.png
docs/design/v1-baseline/stitch/security_audit_events/screen.png
docs/design/v1-baseline/stitch/global_logs_explorer/screen.png
docs/design/v1-baseline/stitch/system_configuration/screen.png
docs/design/v1-baseline/stitch/target_details_blog.example.com/screen.png
EOF
```

Expected: no output and exit code `0`.

- [ ] **Step 5: Commit Task 3**

```bash
git add docs/operations/v1-visual-verification.md docs/release/v1-gap-checklist.md
git commit -m "Track V1 visual evidence and final gaps" -m "V1 closure needs an explicit visual comparison set and a release checklist that does not silently conflate implementation, documentation, and live evidence. This records the authoritative baseline screenshots, current capture path, and remaining partial evidence items.

Constraint: Unified / Baseline Stitch screens are the only visual authority for V1.
Rejected: Treat frontend tests as visual proof | tests verify behavior, not screenshot fidelity.
Confidence: high
Scope-risk: narrow
Tested: Baseline screenshot paths exist locally.
Not-tested: Live browser screenshot comparison remains pending until a seeded runtime is available."
```

---

### Task 4: Link delivery artifacts from the repository entry point and run final verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add delivery section to `README.md`**

Append this section after the existing implementation entry list:

```markdown

## Delivery and V1 verification artifacts

- Local/systemd deployment: `docs/deploy/local-and-systemd.md`
- Center systemd example: `docs/deploy/systemd/houfeng-center.service`
- Agent systemd example: `docs/deploy/systemd/houfeng-agent.service`
- Fresh-install smoke run: `docs/operations/v1-smoke-run.md`
- Visual verification record: `docs/operations/v1-visual-verification.md`
- Final V1 gap checklist: `docs/release/v1-gap-checklist.md`

Automated verification:

```bash
go test ./...
./scripts/verify.sh
cd web && npm run build
```

Live PostgreSQL smoke, Telegram delivery, and screenshot comparison evidence are tracked separately in the operation/release documents because they require environment-specific runtime setup.
```

- [ ] **Step 2: Run full automated verification**

Run:

```bash
go test ./...
./scripts/verify.sh
cd web && npm run build
```

Expected:

- all Go tests pass;
- `./scripts/verify.sh` passes, including Go fmt/vet/tests and web tests/build;
- standalone web build passes.

- [ ] **Step 3: Inspect final git state**

Run:

```bash
git status --short
git log --oneline -5
```

Expected:

- only `README.md` is uncommitted before the final commit;
- recent commits include the Phase 6 spec and the three task commits.

- [ ] **Step 4: Commit Task 4**

```bash
git add README.md
git commit -m "Expose V1 delivery verification artifacts" -m "The repository entry point should direct operators and future agents to the deployment, smoke, visual verification, and release gap documents created for V1 closure.

Constraint: README remains a delivery index, not a replacement for the frozen design baseline.
Confidence: high
Scope-risk: narrow
Tested: go test ./...; ./scripts/verify.sh; cd web && npm run build.
Not-tested: Live PostgreSQL smoke and visual screenshot capture remain tracked as evidence gaps."
```

---

## Final Review

After all tasks:

1. Read `docs/release/v1-gap-checklist.md`.
2. Confirm every V1 area has one of `Closed`, `Partial`, or `Deferred outside V1`.
3. Confirm no document claims live PostgreSQL smoke, Telegram delivery, or screenshot comparison was completed unless actual evidence files/output were added.
4. Run:

```bash
git status --short --branch
```

Expected: clean `main` branch.
