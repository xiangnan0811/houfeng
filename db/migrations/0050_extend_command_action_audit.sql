alter table monitoring_instance_command_action_audit
    add column if not exists monitoring_instance_name_snapshot text not null default '',
    add column if not exists actor_username_snapshot text not null default '',
    add column if not exists actor_display_name_snapshot text not null default '';

update monitoring_instance_command_action_audit audit
set monitoring_instance_name_snapshot = mi.display_name
from monitoring_instances mi
where audit.monitoring_instance_id = mi.monitoring_instance_id
    and audit.monitoring_instance_name_snapshot = '';

update monitoring_instance_command_action_audit audit
set actor_username_snapshot = case
        when audit.actor_username_snapshot = '' then u.username
        else audit.actor_username_snapshot
    end,
    actor_display_name_snapshot = case
        when audit.actor_display_name_snapshot = '' then u.display_name
        else audit.actor_display_name_snapshot
    end
from users u
where audit.actor_user_id = u.user_id
    and (
        audit.actor_username_snapshot = ''
        or audit.actor_display_name_snapshot = ''
    );

do $$
declare
    foreign_key_name text;
begin
    for foreign_key_name in
        select conname
        from pg_constraint
        where conrelid = 'monitoring_instance_command_action_audit'::regclass
            and contype = 'f'
    loop
        execute format(
            'alter table monitoring_instance_command_action_audit drop constraint %I',
            foreign_key_name
        );
    end loop;
end $$;

alter table monitoring_instance_command_action_audit
    alter column action_id drop not null,
    drop constraint if exists monitoring_instance_command_action_audit_event_type_check;

do $$
begin
    if not exists (
        select 1
        from pg_constraint
        where conrelid = 'monitoring_instance_command_action_audit'::regclass
            and conname = 'command_action_audit_event_type_allowed'
    ) then
        alter table monitoring_instance_command_action_audit
            add constraint command_action_audit_event_type_allowed
            check (event_type in ('queued', 'dispatched', 'completed', 'rejected'));
    end if;

    if not exists (
        select 1
        from pg_constraint
        where conrelid = 'monitoring_instance_command_action_audit'::regclass
            and conname = 'command_action_audit_action_identity_valid'
    ) then
        alter table monitoring_instance_command_action_audit
            add constraint command_action_audit_action_identity_valid
            check (
                (event_type = 'rejected' and action_id is null)
                or (event_type <> 'rejected' and action_id is not null)
            );
    end if;

    if not exists (
        select 1
        from pg_constraint
        where conrelid = 'monitoring_instance_command_action_audit'::regclass
            and conname = 'command_action_audit_rejected_source_valid'
    ) then
        alter table monitoring_instance_command_action_audit
            add constraint command_action_audit_rejected_source_valid
            check (event_type <> 'rejected' or source = 'web');
    end if;

    if not exists (
        select 1
        from pg_constraint
        where conrelid = 'monitoring_instance_command_action_audit'::regclass
            and conname = 'command_action_audit_rejected_reason_valid'
    ) then
        alter table monitoring_instance_command_action_audit
            add constraint command_action_audit_rejected_reason_valid
            check (
                event_type <> 'rejected'
                or details = '{"reason":"sensitive_confirmation_required"}'::jsonb
            );
    end if;

    if not exists (
        select 1
        from pg_constraint
        where conrelid = 'monitoring_instance_command_action_audit'::regclass
            and conname = 'command_action_audit_details_metadata_only'
    ) then
        alter table monitoring_instance_command_action_audit
            add constraint command_action_audit_details_metadata_only
            check (
                not jsonb_path_exists(
                    details,
                    'lax $.** ? (@.type() == "object" && (exists(@.stdout) || exists(@.stderr)))'
                )
            );
    end if;
end $$;

create index if not exists idx_monitoring_instance_command_action_audit_global_time
    on monitoring_instance_command_action_audit(occurred_at desc, audit_id desc);
