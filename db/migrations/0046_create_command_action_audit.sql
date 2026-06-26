create table if not exists monitoring_instance_command_action_audit (
    audit_id text primary key,
    action_id text not null,
    monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade,
    command_id text not null,
    sensitivity text not null check (sensitivity in ('standard', 'sensitive')),
    event_type text not null check (event_type in ('queued', 'dispatched', 'completed')),
    actor_user_id text references users(user_id) on delete set null,
    source text not null check (source in ('web', 'agent_sync')),
    exit_code integer,
    occurred_at timestamptz not null default now(),
    details jsonb not null default '{}'::jsonb
);

create index if not exists idx_monitoring_instance_command_action_audit_instance_time
    on monitoring_instance_command_action_audit(monitoring_instance_id, occurred_at desc, audit_id desc);

create index if not exists idx_monitoring_instance_command_action_audit_action_time
    on monitoring_instance_command_action_audit(action_id, occurred_at asc, audit_id asc);
