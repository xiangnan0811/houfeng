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
install -d -m 0700 /tmp/houfeng-agent
export HOUFENG_AGENT_BUFFER_FILE=/tmp/houfeng-agent/sync-buffer.json
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

Notification-backed event records are visible through the event surface whenever an incident transition created a `notification_records` row. This includes sent, failed, and policy-suppressed notification records; `notification_only=true` does not mean “Telegram send succeeded”.

```bash
curl -fsS 'http://127.0.0.1:8080/api/events?object_type=target&object_id=<target_id>&notification_only=true&limit=10'
```

Expected:

- if the incident transition emitted a notification decision, the response contains notification-backed event rows even when delivery was suppressed or failed;
- if the response is empty, verify whether the transition produced no notification decision for the current policy/state before treating the result as expected.

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
| `go test ./...` | Automated | Passed on 2026-04-29 after evidence update |
| `./scripts/verify.sh` | Automated | Passed on 2026-04-29: Go tests, `npm ci`, 14 Vitest files / 165 tests, and frontend build |
| center starts and `/api/healthz` returns 200 | Local PostgreSQL required | 2026-04-29 live run `smoke-20260429024908`: `{"name":"houfeng-center","version":"dev","status":"ok"}` |
| Node created | Local PostgreSQL required | 2026-04-29 live run `smoke-20260429024908`: `node_id=nd_1450995f5b3bdf38` |
| enrollment token issued | Local PostgreSQL required | Issued at `2026-04-29T10:49:09.129938+08:00`; plaintext token intentionally not recorded |
| agent enrolls and syncs | Local PostgreSQL required | `houfeng-agent` enrolled with `binding_status=已绑定`; latest host sample at `2026-04-29T10:50:09.151525+08:00` |
| Target created | Local PostgreSQL required | `target_id=tg_02d55cc117129e57`, host `127.0.0.1`, smoke center port `34923` |
| ProbeItem created | Local PostgreSQL required | `probe_item_id=pb_98a9b2826106bcb1`, `probe_kind=http`, `frequency_tier=1m` |
| observations received | Local PostgreSQL required | Runtime facts showed HTTP success observation, `http_status=200`, observed at `2026-04-29T10:50:09.151525+08:00` |
| incident started | Local PostgreSQL required | `incident_id=inc_target_tg_02d55cc117129e57_target_probe_failure`, `event_id=evt_813ad08f1029a282`, severity `关注` after two 404 probe failures |
| incident recovered | Local PostgreSQL required | Active incident count returned to `0`; recovered event `evt_b7416f1f3da1f506` |
| notification-backed event query checked | Local PostgreSQL / Telegram policy dependent | `notification_only=true` returned 2 event rows for the incident start/recovery transitions |
| Telegram notification sent or intentionally disabled | Manual / Telegram required | Telegram env vars were intentionally empty for this smoke; outbound Telegram delivery was not attempted |
| primary UI pages checked | Manual | Not checked in browser during this live PostgreSQL run; screenshot/visual evidence remains tracked in `docs/operations/v1-visual-verification.md` |

### 2026-05-02 Run Evidence

| Check | Evidence level | Result field |
| --- | --- | --- |
| `go test ./...` / `./scripts/verify.sh` | Automated | Not re-run in this smoke (black-box validation only); see 2026-04-29 row above |
| center starts and `/api/healthz` returns 200 | Local PostgreSQL required | Pre-existing center process at `localhost:8080`: `{"name":"houfeng-center","version":"dev","status":"ok"}` |
| Login (acquire session cookie) | Local PostgreSQL required | `POST /api/auth/login` admin/Houfeng@123*: HTTP 200, cookie `houfeng_session`, `user_id=usr_8ca5360bcc10195ccff02f58` |
| Node created | Local PostgreSQL required | `node_id=nd_cc1c47a6803a648c`, `display_name=smoke-node-20260502T152318Z`, created `2026-05-02T23:23:26+08:00` |
| enrollment token issued | Local PostgreSQL required | `POST /api/nodes/<id>/enrollment-token` returned `{"token":"enroll_c7a01127341509ae", ...}` (response key is `token`, not `plaintext_token`) |
| agent enrolls and syncs | Local PostgreSQL required (PARTIAL) | Agent PID `42347` enrolled at `2026-05-02T23:23:46+08:00`, `binding_status=已绑定`, sync heartbeat OK; host sample collection FAILED on macOS (`/proc/loadavg` not found) — Linux deploy target unaffected |
| Target created | Local PostgreSQL required | `target_id=tg_5742021c60d2cff1`, host `127.0.0.1:8080`, created `2026-05-02T23:25:25+08:00` |
| ProbeItem created | Local PostgreSQL required | `probe_item_id=pb_3bcefcd290adb5f8`, `probe_kind=http`, `frequency_tier=1m`, path `/api/healthz` |
| observations received | Local PostgreSQL required | First observation `2026-05-02T23:26:16+08:00`, `result_kind=success`, `http_status=200`, `latency_ms=1` |
| incident started | Local PostgreSQL required | `incident_id=inc_target_tg_5742021c60d2cff1_target_probe_failure`, started event `evt_18584acd91cc1236` (severity 关注), escalation event `evt_5f3dbc952fb7a055` (severity 告警); ~3m25s wait from probe-path mutation to detection |
| incident recovered | Local PostgreSQL required | Recovered event `evt_4a26540ab9b8afd0` at `2026-05-02T23:35:16+08:00`; ~2m05s wait from probe-path restore; active incident count back to 0 |
| notification-backed event query checked | Local PostgreSQL / Telegram policy dependent | `notification_only=true` returned 3 rows (started/escalated/recovered); Telegram intentionally not configured |
| Telegram notification sent or intentionally disabled | Manual / Telegram required | `telegram.token_present = false`; outbound delivery suppressed; notification records still persisted |
| primary UI pages checked | Manual | INCONCLUSIVE: pre-running center process serves `/` as HTTP 404 (no `HOUFENG_WEB_DIST_DIR` set); SPA dev shell at `http://localhost:5173/` returns 200; backing JSON endpoints (`/api/dashboard`, `/api/nodes`, `/api/targets`, `/api/events`, `/api/incidents`) verified directly |

## Current session status

Most recent live PostgreSQL smoke: **2026-05-02** against
`192.168.100.192:5432/houfeng` with the V1.x auth middleware in place
(admin login successful → cookie reused for protected endpoints).
End-to-end Step 1-2, 4-8 PASSED on first run; Step 3 (agent host sample)
PARTIAL because the local box is macOS and `agent/hostsample` requires
Linux `/proc/loadavg` (does not affect the systemd deploy target);
Step 9 INCONCLUSIVE because the running center process has no
`HOUFENG_WEB_DIST_DIR` set and serves SPA via the parallel vite dev
server instead.

Earlier 2026-04-29 run against `192.168.100.192:5432/user_82Xkx5`
remains tracked in the legacy evidence table (above) for historical
comparison.

## 2026-05-02 Live PostgreSQL Smoke Run

### Environment
- DB: `192.168.100.192:5432/houfeng`
- Center: pre-running `localhost:8080` (no `HOUFENG_WEB_DIST_DIR` set; SPA via vite :5173 instead)
- Auth: admin / Houfeng@123*
- Agent: built locally and run as background process (PID killed cleanly post-run)
- Effective center settings: `incident_defaults.sweep_interval_seconds = 60`, `heartbeat_interval_seconds = 30`, `stale_threshold_intervals = 3`, `probe_frequency_defaults.http = 5m` (smoke ProbeItem overrides to `1m`), `notify_on_started/escalated/recovered = true`, `telegram.token_present = false`

### Resource IDs
- node_id: `nd_cc1c47a6803a648c` (`smoke-node-20260502T152318Z`)
- target_id: `tg_5742021c60d2cff1` (`smoke-target-20260502T152318Z`, host `127.0.0.1:8080`)
- probe_item_id: `pb_3bcefcd290adb5f8` (http GET `/api/healthz`, freq `1m`, timeout `3s`)
- incident_id: `inc_target_tg_5742021c60d2cff1_target_probe_failure`
- events: started `evt_18584acd91cc1236` / escalated `evt_5f3dbc952fb7a055` / recovered `evt_4a26540ab9b8afd0`

### Timing
- incident started detection: 3 min 25 s after probe-path mutation (`23:27:21` → `23:30:46`), consistent with 1m probe cadence + 60s sweep + 2-failure threshold
- incident recovered detection: 2 min 5 s after probe-path restore (`23:33:11` → `23:35:16`)

### Per-step result summary

| Step | Status | Note |
| --- | --- | --- |
| 0. Login | PASS | session cookie reused for all protected calls |
| 1. Create Node | PASS | initial `binding_status=未绑定`, `lifecycle_status=待接入` |
| 2. Issue token | PASS | response key is `token`, not `plaintext_token` (see caveat below) |
| 3. Enroll + run agent | PARTIAL | enroll/sync/plan delivery OK; macOS host-sample fails (Darwin lacks `/proc/loadavg`) |
| 4. Create Target | PASS | self-probe target against running center |
| 5. Add ProbeItem | PASS | first observation ~50s after creation |
| 6. Trigger incident | PASS | `PUT` updates ProbeItem `config.path` to `/api/__nonexistent__`; full body required |
| 7. Recover incident | PASS | restore `config.path=/api/healthz`; recovered event observed within 2m05s |
| 8. Notification surface | PASS | 3 notification-backed events returned; delivery suppressed by missing Telegram config |
| 9. UI checkpoints | INCONCLUSIVE | no headless browser; backing JSON endpoints verified directly |

### Caveats / new findings (consider for v1-gap-checklist follow-ups)

1. **`POST /api/nodes/{id}/enrollment-token` response key is `token`** (not `plaintext_token` as the Step-2 doc text suggests). Anyone scripting strictly against the doc will break on the first parse — Step-2 should clarify the actual key.
2. **agent `agent/hostsample` requires Linux `/proc/loadavg`** — fails silently on macOS every 30s. The systemd deploy target is unaffected, but local-dev smoke on macOS cannot complete agent host-sample collection (`latest_host_sample` stays `null`, `has_host_sample=false`).
3. **Center `/` returns HTTP 404 when `HOUFENG_WEB_DIST_DIR` is unset** — production deploys must set it; the smoke prerequisite section already exports it, but operators reusing a long-lived dev center must verify the env var or the Step-9 visual checks cannot be performed against the center process itself.
4. **`GET /api/events` returns a bare JSON array, not `{items:[...]}`** — internal contract; if/when an envelope is introduced, all callers and any polling scripts written against the bare-array shape will break.

### Cleanup state

- Agent PID `42347` killed at `2026-05-02T23:35:51+08:00` (`agent runtime stopped` logged); `ps` confirms gone.
- Smoke resources retained in PostgreSQL (tagged `20260502T152318Z` for cleanup): node, target, probe item, incident, three events.
- Local files retained for forensics: `/tmp/houfeng-smoke-cookies.txt`, `/tmp/houfeng-smoke-vars.sh`, `/tmp/houfeng-agent-token-smoke`, `/tmp/houfeng-agent-smoke.log`, `/tmp/houfeng-agent-smoke/sync-buffer.json`.
- No DB rows from prior runs were modified or deleted.
