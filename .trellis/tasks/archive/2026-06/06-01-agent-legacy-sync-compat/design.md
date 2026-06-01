# Design

## Scope

This task adds backward-compatible readers around the agent's local persisted state from the `node_id` to `monitoring_instance_id` rename. It does not change the current center API shape, database schema, or newly written agent state.

## Compatibility Points

1. Token file:
   - Current writer keeps producing `{"monitoring_instance_id":"...","sync_token":"..."}`.
   - Reader also accepts legacy `{"node_id":"...","sync_token":"..."}` and returns the legacy ID as the MonitoringInstance ID.
   - If both fields exist, `monitoring_instance_id` wins.

2. Sync queue file:
   - Current writer keeps producing `request.monitoring_instance_id`.
   - Reader accepts legacy `request.node_id` and maps it into `request.monitoring_instance_id` during JSON unmarshal.
   - Queue rewrites that happen after read/prune/mark/delete naturally persist the current structure.

3. Installer:
   - Preserve existing post-enrollment token files when they contain `sync_token` plus either current `monitoring_instance_id` or legacy `node_id`.
   - Do not parse or print token contents in the shell script.

## Non-Goals

- Do not support new outbound `node_id` API payloads.
- Do not add a center-side route alias or database fallback.
- Do not change sync retry policy in this task.

## Risks

- A malformed JSON object containing only `node_id` without `sync_token` should still fail as incomplete credentials.
- A legacy queue entry with missing heartbeat fields should still be rejected by center; this task only maps the renamed carrier ID.
