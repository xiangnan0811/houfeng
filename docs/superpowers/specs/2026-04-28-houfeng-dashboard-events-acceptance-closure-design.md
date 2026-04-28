# Houfeng Dashboard and Events Acceptance Closure Design

## Context

Phase 1, Phase 2, and Phase 3 V1 closure work have made runtime controls truthful, agent sync durable, and retention executable. The remaining Phase 4 gap is now narrower than the original observability-surface plan:

1. Dashboard sections for “当前异常节点摘要” and “当前异常目标摘要” still show counts only.
2. Events page supports only object type, severity, event type, and limit; the frozen V1 baseline also calls for time range, labels, notification-only, recovery-only, and maintenance-only filters.

This design closes those acceptance-surface gaps without reopening product design and without adding trend charts, new incident classes, custom dashboards, or a generic rule engine.

## Chosen Approach

Use the existing SQL-first read model and extend the current endpoints:

- `/api/dashboard` remains the single Dashboard read endpoint, but gains bounded `abnormal_nodes` and `abnormal_targets` arrays.
- `/api/events` keeps its current event stream contract and gains explicit query filters.
- Existing `nodes`, `targets`, `state_change_events`, and `notification_records` tables remain the source of truth.
- Add only small supporting indexes for the new filters; do not denormalize event labels or notifications into a new read table.

Rejected alternatives:

- Query `/api/incidents` separately from the Dashboard page: this would show incidents, not the object summary fields required by the baseline.
- Add a new dashboard read-model table: unnecessary for V1 and premature before scale data exists.
- Implement trend/detail chart work in this phase: that belongs to Phase 5.

## Dashboard Contract

Extend the backend `DashboardOverview` with two bounded arrays:

- `abnormal_nodes`
- `abnormal_targets`

Node summary fields:

- `node_id`
- `display_name`
- `region`
- `city`
- `provider`
- `lifecycle_status`
- `monitoring_status`
- `current_health_status`
- `current_active_incident_count`
- `last_heartbeat_at`
- `current_primary_issue_summary`

Target summary fields:

- `target_id`
- `name`
- `target_type`
- `host`
- `base_port`
- `run_status`
- `current_health_status`
- `current_active_incident_count`
- `last_success_at`
- `last_failure_at`
- `current_primary_issue_summary`

Ordering should prioritize operational urgency:

1. severity rank: `严重`, `告警`, `关注`, other;
2. active incident count descending;
3. recently updated rows first;
4. stable id ordering.

The existing dashboard `limit` parameter controls recent events and the abnormal summary list size. Default remains `10`.

## Dashboard UI

Keep the current page hierarchy and card vocabulary. Replace count-only node/target sections with short object summary cards:

- node cards link to `/nodes/:nodeId`;
- target cards link to `/targets/:targetId`;
- each card shows identity, status badges, active incident count, latest timestamp, and primary issue summary;
- if a summary list is empty, show a clear empty state.

This is not a visual redesign. It makes the existing frozen Dashboard sections truthful and useful.

## Events Contract

Extend `EventsFilter` and `/api/events` query parsing with:

- `created_from`: RFC3339 timestamp, inclusive lower bound;
- `created_to`: RFC3339 timestamp, inclusive upper bound;
- `label`: one Node or Target label to match against the current object labels;
- `notification_only`: boolean; events whose payload `incident_id` has a matching notification record;
- `recovery_only`: boolean; compiles to `event_type = incident_recovered`;
- `maintenance_only`: boolean; compiles to Node/Target maintenance enter/exit event types.

Existing filters stay unchanged:

- `object_type`
- `object_id`
- `severity`
- `event_type`
- `limit`

Invalid timestamps, booleans, or limits return `400`. Empty query values are ignored.

Notification-only semantics use current persisted facts: a state-change event is notification-related when `notification_records` contains a row with the same payload incident id, object type, and object id. This includes sent, suppressed, and failed notification records because all are relevant to “通知相关事件”.

## Events UI

Extend the Events page filter panel with:

- start time text field;
- end time text field;
- label text field;
- notification-only checkbox;
- recovery-only checkbox;
- maintenance-only checkbox;
- reset button.

Keep the existing object type, severity, event type, and limit controls. The page continues to render the same `EventList`.

## Indexes

Add a small migration for query support:

- `state_change_events(created_at desc)`
- `notification_records(incident_id, object_type, object_id)`
- GIN indexes for `nodes.labels` and `targets.labels`

No new tables are introduced.

## Testing

Backend:

- migration order test for the new indexes migration;
- dashboard repository tests for abnormal node/target summary SQL and response population;
- dashboard handler test for snake_case summary arrays;
- events repository tests for time range, label, notification-only, recovery-only, and maintenance-only SQL predicates;
- events handler tests for parsing valid filters and rejecting invalid timestamps/booleans.

Frontend:

- API helper tests for boolean and time/label query serialization;
- Dashboard page tests that render abnormal node and target summaries;
- Events page tests for advanced filters and reset behavior.

Full verification remains:

- `go test ./...`
- `cd web && npm test -- --run`
- `cd web && npm run build`
- `./scripts/verify.sh`

## Scope Boundaries

In scope:

- Dashboard current abnormal object summaries.
- Events advanced filters listed above.
- Small query indexes.

Out of scope:

- trend degradation rules;
- trend charts or aggregate trend display;
- custom saved filters/workbench views;
- notification delivery UI;
- generic search DSL;
- multi-user or permission behavior.

