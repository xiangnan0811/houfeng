create index if not exists idx_state_change_events_created_at
  on state_change_events (created_at desc);

create index if not exists idx_notification_records_incident_object
  on notification_records (incident_id, object_type, object_id);

create index if not exists idx_nodes_labels_gin
  on nodes using gin (labels);

create index if not exists idx_targets_labels_gin
  on targets using gin (labels);
