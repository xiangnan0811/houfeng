create table if not exists agent_sync_batches (
    monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade,
    sync_batch_id text not null,
    received_at timestamptz not null default now(),
    primary key (monitoring_instance_id, sync_batch_id)
);

create index if not exists idx_agent_sync_batches_received_at on agent_sync_batches(received_at);
