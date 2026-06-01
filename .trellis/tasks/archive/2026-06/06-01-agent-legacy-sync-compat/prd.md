# Fix agent legacy sync compatibility

## Goal

Prevent v0.25.0+ agents from getting stuck after upgrading from pre-rename releases whose local persisted state still uses the legacy `node_id` JSON field.

## Requirements

- The agent must read legacy post-enrollment token files that contain `node_id` + `sync_token` as valid sync credentials for the renamed MonitoringInstance contract.
- The agent sync queue must read legacy buffered sync requests that contain `request.node_id` and translate them to `request.monitoring_instance_id` before retrying.
- New writes must continue to use the current `monitoring_instance_id` JSON field.
- The Linux systemd installer must preserve existing post-enrollment token files that still use `node_id` + `sync_token` during upgrade instead of overwriting them with a fresh enrollment token.
- The fix must remain scoped to local upgrade compatibility; do not reintroduce `node_id` to new public API payloads, database schema, or UI copy.
- The current single-center/single-agent test environment has already been manually recovered by clearing the old sync queue; this task prevents the same class of upgrade failure for future agents.

## Acceptance Criteria

- [x] `agent/token` tests cover reading legacy `node_id` token credentials and saving current `monitoring_instance_id` credentials.
- [x] `agent/syncqueue` tests cover loading a legacy queued request and exposing it with `MonitoringInstanceID` populated.
- [x] Installer tests or source-level coverage prove the preserve-token branch accepts both current `monitoring_instance_id` credentials and legacy `node_id` credentials.
- [x] Targeted Go tests for changed packages pass.
- [x] `make verify-go` passes, or any failure is documented with the exact blocker.

## Notes

- Evidence from production-like testing: after clearing `/var/lib/houfeng-agent/sync-buffer.json`, the only agent recovered heartbeat; `/etc/houfeng-agent/token` is already current-format `monitoring_instance_id` + `sync_token`.
