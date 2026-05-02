# V1 Live PostgreSQL Smoke Run — 2026-05-02

> Live end-to-end smoke for `候风 / Houfeng Fleet Control Plane` against the user-provided PostgreSQL and a locally running center + agent. Follows the 9-step canonical flow in `docs/operations/v1-smoke-run.md`.

## Environment

| Item | Value |
| --- | --- |
| Smoke run timestamp tag | `20260502T152318Z` |
| PostgreSQL DSN | `postgres://houfeng:houfeng@192.168.100.192:5432/houfeng?sslmode=disable` |
| Center URL | `http://localhost:8080` (already running before smoke) |
| Web (vite dev) URL | `http://localhost:5173` (already running before smoke) |
| Center binary | `/Users/weibo/Code/houfeng/bin/houfeng-center` (pre-existing) |
| Agent binary | `/Users/weibo/Code/houfeng/bin/houfeng-agent` (built fresh via `make build-agent` at smoke start) |
| Auth user | `admin` / `Houfeng@123*` |
| OS for agent run | macOS Darwin 24.6.0 (host machine) |
| Cookie file | `/tmp/houfeng-smoke-cookies.txt` |

Pre-flight `/api/healthz` (no auth):

```
{"name":"houfeng-center","version":"dev","status":"ok"}
```

Effective center settings (from `GET /api/settings`):

- `incident_defaults.sweep_interval_seconds = 60`
- `incident_defaults.heartbeat_interval_seconds = 30`
- `incident_defaults.stale_threshold_intervals = 3`
- `probe_frequency_defaults.http = 5m` (smoke ProbeItem overrides to `1m`)
- `telegram.token_present = false` (Telegram delivery intentionally disabled)
- `notify_on_started / notify_on_escalated / notify_on_recovered = true`

## Step 0: Login (acquire session cookie)

- Command:
  ```bash
  curl -sS -c /tmp/houfeng-smoke-cookies.txt -X POST http://localhost:8080/api/auth/login \
    -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"Houfeng@123*"}'
  ```
- HTTP 200, response `{"user_id":"usr_8ca5360bcc10195ccff02f58", ...}`
- Cookie name: `houfeng_session` (matches `internal/center/auth/types.go:12`)
- Verification: `GET /api/auth/me` with `-b /tmp/houfeng-smoke-cookies.txt` → 200, `{"username":"admin","role":"admin","display_name":"管理员"}`
- Status: **PASS**

## Step 1: Create Node

- Command (POST `/api/nodes`):
  ```json
  {"display_name":"smoke-node-20260502T152318Z","provider":"local","region":"local",
   "city":"local","labels":["smoke","v1","20260502T152318Z"],"note":"V1 smoke node 20260502T152318Z"}
  ```
- HTTP 201
- `node_id = nd_cc1c47a6803a648c`
- Initial `binding_status = 未绑定`, `lifecycle_status = 待接入`, `current_health_status = 正常`
- `created_at = 2026-05-02T23:23:26.058898+08:00`
- Status: **PASS**

## Step 2: Issue enrollment token

- Command: `POST /api/nodes/nd_cc1c47a6803a648c/enrollment-token`
- HTTP 200
- Response shape: `{"token":"enroll_c7a01127341509ae","issued_at":"2026-05-02T23:23:34.241004+08:00"}`
- Note: V1 smoke doc names the field `plaintext_token`, but the actual handler returns it as `token`. Recorded here for the docs follow-up.
- Token persisted: `/tmp/houfeng-agent-token-smoke` (`chmod 0600`, 23 bytes)
- Status: **PASS**

## Step 3: Enroll & run agent

- ENV used to start agent:
  - `HOUFENG_AGENT_SERVER_URL=http://127.0.0.1:8080`
  - `HOUFENG_AGENT_TOKEN_FILE=/tmp/houfeng-agent-token-smoke`
  - `HOUFENG_AGENT_BUFFER_FILE=/tmp/houfeng-agent-smoke/sync-buffer.json`
- Command: `nohup ./bin/houfeng-agent > /tmp/houfeng-agent-smoke.log 2>&1 &`
- Agent PID: `42347`
- Agent log first lines:
  ```
  2026-05-02T23:23:46.768+08:00 INFO  agent runtime started server_url=http://127.0.0.1:8080
  2026-05-02T23:23:46.812+08:00 INFO  agent enrolled        node_id=nd_cc1c47a6803a648c status=accepted binding_status=已绑定
  ```
- Onboarding endpoint after enroll (`GET /api/nodes/<id>/onboarding`):
  - `binding_status = 已绑定`
  - `phase = 已绑定，等待稳定观测`
  - `current_binding_fingerprint_summary = 761c4e40…6dbd2f`
  - After 60s: `last_heartbeat_at = 2026-05-02T23:24:46.812289+08:00`, `last_sync_at = 2026-05-02T23:24:46.886218+08:00`
- **Caveat — host sample on macOS:** agent logs `ERROR collect host sample failed: read /proc/loadavg: open /proc/loadavg: no such file or directory` every 30s. macOS does not expose `/proc/loadavg`, so `latest_host_sample` stays `null` and `has_host_sample = false` for the entire smoke. This does not block enrollment, sync heartbeats, plan dispatch, or probe observation ingestion (all of which use the agent's HTTP probes, not the host-sample collector).
- Status: **PARTIAL**
  - PASS: enroll + sync heartbeat + plan delivery
  - FAIL (Darwin-only): host sample collection, hence `runtime-facts.latest_host_sample` stays empty

## Step 4: Create Target

- Command (POST `/api/targets`):
  ```json
  {"name":"smoke-target-20260502T152318Z","target_type":"service",
   "host":"127.0.0.1","base_port":8080,"run_status":"启用",
   "labels":["smoke","v1","20260502T152318Z"],"execution_node_labels":["smoke"],
   "note":"V1 smoke target 20260502T152318Z"}
  ```
- HTTP 201
- `target_id = tg_5742021c60d2cff1`
- `created_at = 2026-05-02T23:25:25.54028+08:00`
- Note: smoke node was created without the `smoke` label match against `execution_node_labels=["smoke"]` (it has `smoke` in its labels list, so plan-binding matched). Verified by subsequent probe observations flowing.
- Status: **PASS**

## Step 5: Add ProbeItem

- Command (POST `/api/targets/<id>/probe-items`):
  ```json
  {"probe_kind":"http","enabled":true,"frequency_tier":"1m","timeout_seconds":3,
   "config":{"scheme":"http","path":"/api/healthz","method":"GET",
             "expected_status_range":[200,299]}}
  ```
- HTTP 201
- `probe_item_id = pb_3bcefcd290adb5f8`
- After ~50s the agent picked up the plan and emitted observations:
  - `2026-05-02T23:26:16.811422+08:00`  result_kind=success  http_status=200  latency_ms=1
  - `2026-05-02T23:27:46.810539+08:00`  result_kind=success  http_status=200
- Status: **PASS**

## Step 6: Trigger incident

- Action: `PUT /api/targets/<target_id>/probe-items/<probe_id>` updating `config.path` to `/api/__nonexistent__` (full request body required by handler — see `internal/center/http/handlers/targets.go:126`; `UpdateProbeItemInput` is an alias of `CreateProbeItemInput` per `internal/center/targets/types.go:116`).
- Update applied at `2026-05-02T23:27:21+08:00` (HTTP 200).
- Subsequent observations (from `runtime-facts`):
  - `2026-05-02T23:29:16.809739+08:00`  result_kind=failure  http_status=404
  - `2026-05-02T23:30:46.808806+08:00`  result_kind=failure  http_status=404 → triggered incident
  - `2026-05-02T23:32:16.807919+08:00`  result_kind=failure  http_status=404 → triggered escalation
- `GET /api/incidents?object_type=target&object_id=<id>` returned an active incident:
  ```json
  {
    "incident_id": "inc_target_tg_5742021c60d2cff1_target_probe_failure",
    "incident_class": "target_probe_failure",
    "object_type": "target",
    "object_id": "tg_5742021c60d2cff1",
    "severity": "告警",
    "started_at": "2026-05-02T23:30:46.808806+08:00",
    "last_evaluated_at": "2026-05-02T23:32:16.807919+08:00",
    "source_summary": "http 探针连续失败 3 次（unexpected http status 404）"
  }
  ```
- `GET /api/events?object_type=target&object_id=<id>&event_type=incident_started`:
  ```json
  [{"event_id":"evt_18584acd91cc1236","event_type":"incident_started",
    "severity":"关注","summary":"http 探针连续失败 2 次（unexpected http status 404）",
    "created_at":"2026-05-02T23:30:46.808806+08:00"}]
  ```
- Escalation event also recorded: `evt_5f3dbc952fb7a055` at `2026-05-02T23:32:16.807919+08:00`, severity `告警`.
- **Wait time from trigger update to incident_started detection**: 23:27:21 → 23:30:46 ≈ **3 min 25 s**, consistent with the 1m probe cadence + 60s sweep + 2-failure threshold.
- Note on polling: my poll loop initially used 10s × 30 with a wrapper key parser (`items`/`events`); the events endpoint actually returns a bare JSON array, so my parser reported zero counts even after the event was created. I subsequently confirmed the data with a direct `python3 -m json.tool` dump. The records ARE present in the DB; this is a polling-script bug, not an absence of evidence.
- Status: **PASS**

## Step 7: Recover incident

- Action: `PUT /api/targets/<target_id>/probe-items/<probe_id>` restoring `config.path = /api/healthz` at `2026-05-02T23:33:11+08:00` (HTTP 200).
- Polling for `event_type=incident_recovered` (10s cadence):
  - First success at poll 12 (`2026-05-02T23:35:25+08:00`).
- Recovered event:
  ```json
  {"event_id":"evt_4a26540ab9b8afd0","event_type":"incident_recovered",
   "severity":"告警","summary":"探针已连续成功恢复",
   "created_at":"2026-05-02T23:35:16.806193+08:00"}
  ```
- `GET /api/incidents?object_type=target&object_id=<id>` returned `[]` (no active).
- **Wait time from recover update to incident_recovered detection**: 23:33:11 → 23:35:16 ≈ **2 min 5 s**.
- Status: **PASS**

## Step 8: Notification-backed event surface

- Command: `GET /api/events?object_type=target&object_id=<id>&notification_only=true&limit=10`
- Response (3 rows):
  - `evt_4a26540ab9b8afd0`  incident_recovered  2026-05-02T23:35:16
  - `evt_5f3dbc952fb7a055`  incident_escalated  2026-05-02T23:32:16
  - `evt_18584acd91cc1236`  incident_started    2026-05-02T23:30:46
- Telegram credentials were intentionally NOT configured (`telegram.token_present = false`); rows still appear because policy-suppressed deliveries also persist `notification_records` and surface through `notification_only=true`. This matches the spec note in `docs/operations/v1-smoke-run.md` Step 8 ("`notification_only=true` does not mean Telegram send succeeded").
- Status: **PASS**

## Step 9: UI verification checkpoints

- `GET http://localhost:8080/` (center root, expected to serve embedded SPA): **HTTP 404** (`Content-Length: 19`). Cause: the running `houfeng-center` process appears to have started without `HOUFENG_WEB_DIST_DIR` pointing to a built `web/dist`, so `internal/center/http/handlers/spa.go` returns 404. SPA is not served from the center process during this smoke.
- `GET http://localhost:5173/` (Vite dev server): **HTTP 200**, returns `<!doctype html>` with `<title>候风 · 服务器舰队控制面</title>` and the Vite refresh harness. The web dev server is functional but cannot be auto-driven without a headless browser.
- API endpoints behind the SPA verified directly (with session cookie):
  - `GET /api/dashboard` → returns counts `{total_node_count:2, total_target_count:1, abnormal_node_count:1, severe_node_count:1, recent_new_incident_count:3, recent_recovery_count:3}` and the smoke incident events appear in `recent_events`.
  - `GET /api/nodes?limit=20` → contains `nd_cc1c47a6803a648c` `smoke-node-20260502T152318Z` `binding_status=已绑定` `current_health_status=正常`.
  - Target / events / incidents data already verified in Steps 4–7.
- Visual / UI walkthrough across Dashboard, Nodes, Node detail, Targets, Target detail, Events, Settings: **NOT executed** (no headless browser is available in this run, per task scope).
- Status: **INCONCLUSIVE** for visual verification; **PASS** for SPA-served-by-vite-dev availability and for the JSON shapes that back each page.

## Summary

| Step | Status | Key IDs / Notes |
| --- | --- | --- |
| 0. Login | PASS | session cookie `houfeng_session`, user_id `usr_8ca5360bcc10195ccff02f58` |
| 1. Create Node | PASS | `node_id=nd_cc1c47a6803a648c`, `display_name=smoke-node-20260502T152318Z` |
| 2. Issue token | PASS | `token=enroll_c7a01127341509ae` (response key is `token`, not `plaintext_token`) |
| 3. Enroll + run agent | PARTIAL | enroll OK, sync OK, plan delivery OK; host sample fails on macOS (no `/proc/loadavg`) |
| 4. Create Target | PASS | `target_id=tg_5742021c60d2cff1`, host 127.0.0.1:8080 |
| 5. Add ProbeItem | PASS | `probe_item_id=pb_3bcefcd290adb5f8`, http /api/healthz, freq 1m |
| 6. Trigger incident | PASS | `incident_id=inc_target_tg_5742021c60d2cff1_target_probe_failure`, started `evt_18584acd91cc1236`, escalated `evt_5f3dbc952fb7a055` (~3m25s wait) |
| 7. Recover incident | PASS | recovered `evt_4a26540ab9b8afd0` (~2m05s wait), active count returned to 0 |
| 8. Notification surface | PASS | `notification_only=true` returns 3 rows; Telegram intentionally disabled |
| 9. UI checkpoints | INCONCLUSIVE | center root returns 404 (no `HOUFENG_WEB_DIST_DIR`); vite dev SPA index 200; backing JSON endpoints verified |

## Cleanup

- Agent process killed: PID `42347` at `2026-05-02T23:35:51+08:00`. Final log entry: `level=INFO msg="agent runtime stopped"`. `ps -p 42347` returns no row (TIME CMD header only) — process gone.
- Smoke resources retained in PostgreSQL (NOT auto-deleted):
  - `node_id = nd_cc1c47a6803a648c` (`smoke-node-20260502T152318Z`)
  - `target_id = tg_5742021c60d2cff1` (`smoke-target-20260502T152318Z`)
  - `probe_item_id = pb_3bcefcd290adb5f8`
  - `incident_id = inc_target_tg_5742021c60d2cff1_target_probe_failure`
  - events `evt_18584acd91cc1236` (started), `evt_5f3dbc952fb7a055` (escalated), `evt_4a26540ab9b8afd0` (recovered)
- Local files retained for forensics:
  - `/tmp/houfeng-smoke-cookies.txt` (session cookie)
  - `/tmp/houfeng-smoke-vars.sh` (run variables)
  - `/tmp/houfeng-agent-token-smoke` (consumed enrollment token)
  - `/tmp/houfeng-agent-smoke.log` (agent log)
  - `/tmp/houfeng-agent-smoke/sync-buffer.json` (agent sync buffer)
- No DB rows from prior runs were deleted; smoke resources are tagged with `20260502T152318Z` for easy cleanup.

## Notable findings (non-actionable, recorded for the docs follow-up)

1. `POST /api/nodes/<id>/enrollment-token` response field is `token`, but `docs/operations/v1-smoke-run.md` step 2 instructs the operator to read `plaintext_token`. Anyone scripting against the doc will hit this.
2. macOS-hosted agents emit `collect host sample failed` every 30s because `/proc/loadavg` is Linux-only. The agent is documented as "host samples + probes"; on Darwin only probes work. V1 systemd deploy target is Linux, so production is unaffected, but local smoke will always show empty `latest_host_sample` on macOS.
3. The pre-running center process serves `/api/*` correctly but does not serve the SPA at `/` (HTTP 404). Anyone running the smoke against this same center expecting Step 9 visual checks must either set `HOUFENG_WEB_DIST_DIR=web/dist` and restart, or use the Vite dev server at 5173.
4. Events list endpoint (`/api/events`) returns a bare JSON array, not `{items:[...]}`. Polling scripts that assume the wrapped shape will silently report zero.
