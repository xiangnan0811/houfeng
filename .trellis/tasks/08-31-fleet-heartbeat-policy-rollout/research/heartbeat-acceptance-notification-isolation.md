# Research: production heartbeat-policy acceptance and notification isolation

- Query: What evidence proves the v0.79.5 heartbeat policy in production, and can an end-to-end notification test be isolated from real fleet notifications?
- Scope: internal
- Date: 2026-08-31

## Findings

### Files found

- `db/migrations/0063_tune_heartbeat_incident_policy.sql` — global default/data conversion and live-heartbeat index.
- `internal/center/settings/types.go` — default `12`, persisted global/override schema, and validation.
- `internal/center/incidents/evaluator.go` and `types.go` — N/2N/4N transitions and recovery/event types.
- `internal/center/incidents/service.go` — shared persisted policy resolution, recovery query, notification routing, and sanitized identity text.
- `internal/center/http/handlers/settings.go` — masked Settings GET and secret-preserving Settings PUT merge.
- `internal/center/notify/telegram.go` and `feishu.go` — global provider destinations.
- `internal/center/store/incidents.go` — event and notification persistence.
- `.github/workflows/frontend-staging-smoke.yml` and `web/e2e/staging/staging-smoke.spec.ts` — non-production staging UI audit.
- `docs/operations/ui-preview-and-browser-sanity.md` — staging environment and evidence boundary.

### Policy evidence contract

- v0.79.5 defaults heartbeat interval to 5 seconds and the first missing threshold to 12 intervals (`internal/center/settings/types.go:186-217`).
- Migration `0063` updates only a persisted global threshold exactly equal to string value `3`; it does not traverse or rewrite explicit override rules, and any custom global value such as `20` remains unchanged (`db/migrations/0063_tune_heartbeat_incident_policy.sql:1-7`). It adds the live, received-time covering index used by recovery (`0063:9-12`).
- The implemented acceptance boundaries are `N` attention, `2N` alert, `4N` critical, with direct jumps producing only the actual reached transition. Event names are `incident_started`, `incident_escalated`, and `incident_recovered`; the heartbeat class is `monitoring_instance_heartbeat_missing` (`internal/center/incidents/types.go:251`, `276-278`).
- Recovery requires the latest three distinct post-incident live batches. The production query filters `received_at > started_at` and `is_backfilled=false`, bounds candidates before windowing, deduplicates by `sync_batch_id`, and returns three (`internal/center/incidents/service.go:1431-1467`, `1553-1571`). The policy also requires adjacent gaps no greater than twice the heartbeat interval.
- Heartbeat notifications are rewritten at delivery/persistence time to include a sanitized display name and stable MonitoringInstance ID. Control, whitespace, and bidi characters are normalized/removed, names are rune-bounded, and an empty sanitized value falls back to `未命名监控实例` (`internal/center/incidents/service.go:1065-1149`). The same delivery summary is stored in `notification_records` (`service.go:1071-1086`; `internal/center/store/incidents.go:313-340`).

### The repository staging smoke must not be aimed at production

`.github/workflows/frontend-staging-smoke.yml:26-35` binds the audit to a GitHub `staging` environment and a non-production base URL. The test verifies health version before login, but later performs a real Settings save/readback/restore (`web/e2e/staging/staging-smoke.spec.ts:434-443`, `483-534`). Project documentation explicitly requires a non-production staging origin and reversible staging data (`docs/operations/ui-preview-and-browser-sanity.md:239-256`). It is not a deployment workflow and is not safe to repoint at `fleet.yading.de`.

### Notification destinations are global, not per MonitoringInstance

- Telegram fallback credentials come from Center environment variables (`compose.yaml:197-198`; `cmd/houfeng-center/bootstrap.go:1249-1261`). Persisted Settings can take over only when the Telegram record exists and `runtime_managed=true`; otherwise the environment fallback remains active (`internal/center/incidents/service.go:169-229`, `300-305`).
- Feishu enablement/webhook and Telegram chat/token/runtime mode live in the singleton Center settings, not on a MonitoringInstance (`internal/center/settings/types.go:27-48`).
- Settings GET exposes Telegram chat ID and masked/presence metadata but never the bot token or full Feishu webhook. PUT preserves omitted Telegram token and omitted Feishu webhook (`internal/center/http/handlers/settings.go:17-64`, `130-195`). This permits disabling/re-enabling Feishu without reading its URL and, for already runtime-managed Telegram, changing/restoring only the chat ID.
- There is no per-instance notification destination, test-send lane, or routing filter. Temporarily changing a destination redirects all qualifying Center notifications; temporarily disabling a channel can drop real fleet alerts; enabling a test channel can still send the controlled heartbeat incident to the real channel. Thus a genuinely isolated end-to-end provider-message test cannot be performed safely on this shared production Center with the current repository contract.

The safe choices are:

1. run the forced N=20/19/20/recovery/provider-message acceptance on an isolated restored clone or separate non-production Center with a dedicated provider destination; or
2. limit production to migration readback, passive live-batch evidence, and passive verification of naturally occurring notification records/messages.

Any production forced-outage/provider test requires explicit owner acceptance of global notification redirection or real-channel noise plus a maintenance window. It is not "isolated" merely because the MonitoringInstance name contains a test prefix.

### Safe production readback after Center upgrade

Capture only values/hashes needed for comparison:

```bash
set -euo pipefail
DEPLOY_DIR=/absolute/owner-confirmed/fleet-deployment
DOCKER_HOST=unix:///run/docker.sock
DOCKER_CONFIG=/root/houfeng-rollout/docker-empty-config
compose_clean() {
  timeout --signal=TERM --kill-after=1s 10s env -i \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
    DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    docker compose --env-file "$DEPLOY_DIR/.env" \
    -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" "$@"
}
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -P pager=off -c \
  "select incident_defaults->>'heartbeat_interval_seconds' as heartbeat_seconds, incident_defaults->>'stale_threshold_intervals' as global_n, incident_defaults->>'notify_on_started' as notify_started, incident_defaults->>'notify_on_escalated' as notify_escalated, incident_defaults->>'notify_on_recovered' as notify_recovered, md5(override_rules::text) as override_fingerprint from center_settings where settings_id='center'"
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -P pager=off -c \
  "select name, checksum from public.schema_migrations where name='0063_tune_heartbeat_incident_policy.sql'"
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -P pager=off -c \
  "select indexdef from pg_indexes where schemaname='public' and indexname='idx_monitoring_instance_heartbeats_live_received'"
```

Compare the override fingerprint with the pre-upgrade snapshot. Read back Settings through the authenticated UI/API as a second layer. Do not store cookies, passwords, bot tokens, webhook URLs, or full Settings bodies in shared artifacts.

### Controlled acceptance on an isolated clone/dedicated Center

1. Restore the complete cold recovery point into an isolated network with no concurrent Records authority and replace notification destinations with a dedicated test-only provider.
2. Snapshot the full settings logically, including global N, notification toggles, override fingerprint, channel mode/presence, and destination identity; do not expose secret values.
3. Create a disposable MonitoringInstance with a sanitized unique name and stable ID. Ensure other incident classes remain normal.
4. Set global `N=20` through the authenticated Settings UI/API, read it back, and confirm the override fingerprint is unchanged.
5. Establish a live heartbeat baseline, then stop only the disposable Agent. At 19 missed intervals require no heartbeat active incident, state-change event, notification record, or provider message. At 20 require one attention incident/start event and one notification per enabled channel. Optional 40/80 checks must not fabricate intermediate events.
6. Restart the Agent. Require receipt 1 and 2 to retain the active incident. Receipt 3 must have a third distinct `sync_batch_id`, `is_backfilled=false`, `received_at` after incident start, and bounded gaps; only then require one recovery event/notification. Explicitly test duplicate batch, backfill, and oversized-gap negatives if the harness can inject them without touching production data.
7. Join evidence by stable MonitoringInstance ID, incident ID, event type/time, notification channel/status/summary, live batch ID/received time/version, and the actual provider message. The summary must contain the sanitized display name and exact ID.
8. Restore original settings and destinations, delete/retire disposable data through supported APIs, and read back all restoration invariants. Preserve a private, sanitized evidence receipt.

### Read-only joined evidence queries

Use a validated disposable/passive MonitoringInstance ID and a tight UTC window:

```bash
set -euo pipefail
MI_ID=mi_replace_with_validated_id
SINCE_UTC=2026-08-31T00:00:00Z
DEPLOY_DIR=/absolute/owner-confirmed/fleet-deployment
DOCKER_HOST=unix:///run/docker.sock
DOCKER_CONFIG=/root/houfeng-rollout/docker-empty-config
compose_clean() {
  timeout --signal=TERM --kill-after=1s 10s env -i \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
    DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    docker compose --env-file "$DEPLOY_DIR/.env" \
    -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" "$@"
}
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -v mi_id="$MI_ID" -v since_utc="$SINCE_UTC" -U postgres -d houfeng -P pager=off -c \
  "select event_id, event_type, severity, created_at from state_change_events where object_type='monitoring_instance' and object_id=:'mi_id' and created_at>=:'since_utc'::timestamptz order by created_at, event_id"
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -v mi_id="$MI_ID" -v since_utc="$SINCE_UTC" -U postgres -d houfeng -P pager=off -c \
  "select notification_id, incident_id, channel, delivery_status, summary, sent_at, created_at from notification_records where object_type='monitoring_instance' and object_id=:'mi_id' and created_at>=:'since_utc'::timestamptz order by created_at, notification_id"
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -v mi_id="$MI_ID" -v since_utc="$SINCE_UTC" -U postgres -d houfeng -P pager=off -c \
  "with recent_live as materialized (select sync_batch_id, received_at, id, agent_version from monitoring_instance_heartbeats where monitoring_instance_id=:'mi_id' and received_at>=:'since_utc'::timestamptz and is_backfilled=false order by received_at desc, id desc limit 768), ranked as (select sync_batch_id, received_at, id, agent_version, row_number() over (partition by sync_batch_id order by received_at desc, id desc) as batch_rank from recent_live) select sync_batch_id, received_at, agent_version from ranked where batch_rank=1 order by received_at desc, id desc limit 3"
```

Keep notification summaries private if VPS names are sensitive. Provider delivery status in the database is not proof that the intended human-visible isolated destination received the exact message; retain a separate sanitized provider-side receipt.

### Cleanup / stop conditions

- Stop before any forced production test if notification routing is not independently isolated, Settings restoration cannot be proven, or another real incident is active.
- Stop if the Center migration/manifest blocker remains, `/api/healthz.version` is not exactly the exact released next patch v0.79.6, `0063`/registered revision-2 successor is absent or wrong, the override fingerprint changes, or either real Agent is not stably live.
- If Settings restoration fails, freeze further mutations and escalate; do not guess a token/webhook from masked GET data.
- Do not delete event/notification/heartbeat rows directly. Use supported lifecycle cleanup for disposable objects and retain immutable acceptance evidence as required.

### Related specs

- `.trellis/spec/backend/database-guidelines.md` — N/2N/4N boundaries, three-live-batch recovery, `0063`, exact index, and strict PostgreSQL evidence.
- `.trellis/spec/backend/logging-guidelines.md` — provider credentials, notification content, session cookies, and Agent tokens must not leak into logs/artifacts.
- `.trellis/spec/web/state-and-data.md` — persisted Settings and default heartbeat threshold UI contract.

## Caveats / Not Found

- No repository-native production heartbeat acceptance harness was found.
- No per-MonitoringInstance notification routing or isolated test channel exists; full production provider acceptance is therefore unsafe without explicit global-impact authorization.
- The production PRD now fixes passive-only acceptance, host scope and rollback boundaries. Exact maintenance timing/final go-no-go, provider destination identities, stable MonitoringInstance IDs and backup retention/deletion authority still require private execution-time resolution; provider values and IDs must not be copied into task artifacts.
- Read-only inventory has confirmed current heartbeat/global threshold and override fingerprint, active-incident count and recent Agent-version/live-batch aggregate. Notification runtime mode/provider destinations and private stable instance IDs were not inspected; their presence/identity must be checked without exposing values before production cutover.
- The Center v0.79.4 to v0.79.5 migration-manifest blocker must be resolved in a new immutable patch before any production acceptance can start; production must skip the blocked release.
