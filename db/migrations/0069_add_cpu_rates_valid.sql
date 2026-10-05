alter table host_samples add column cpu_rates_valid boolean;

alter table monitoring_instance_host_sample_daily_aggregates
    add column cpu_valid_sample_count integer,
    add column cpu_valid_backfilled_sample_count integer,
    add column cpu_valid_maintenance_sample_count integer,
    alter column avg_cpu_usage_pct drop not null,
    alter column max_cpu_usage_pct drop not null,
    alter column avg_cpu_iowait_pct drop not null,
    alter column max_cpu_iowait_pct drop not null,
    alter column avg_cpu_steal_pct drop not null,
    alter column max_cpu_steal_pct drop not null,
    add constraint host_daily_cpu_valid_count_check check (
        cpu_valid_sample_count >= 0 and cpu_valid_sample_count <= sample_count
    ),
    add constraint host_daily_cpu_valid_backfilled_count_check check (
        cpu_valid_backfilled_sample_count >= 0
        and cpu_valid_backfilled_sample_count <= cpu_valid_sample_count
    ),
    add constraint host_daily_cpu_valid_maintenance_count_check check (
        cpu_valid_maintenance_sample_count >= 0
        and cpu_valid_maintenance_sample_count <= cpu_valid_sample_count
    );
