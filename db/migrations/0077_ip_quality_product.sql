alter table ip_quality_reports
    add column if not exists product_json jsonb,
    add column if not exists product_summary_json jsonb;
alter table ip_quality_provider_results add column if not exists details_json jsonb;
alter table ip_quality_service_unlocks add column if not exists checks_json jsonb;

create table if not exists ip_quality_intelligence (
    report_id text primary key references ip_quality_reports(report_id) on delete cascade,
    state text not null check (state in ('pending', 'running', 'complete', 'partial', 'failed', 'skipped')),
    created_at timestamptz not null default now(),
    started_at timestamptz,
    completed_at timestamptz,
    deadline_at timestamptz not null,
    owner_id text,
    owner_generation bigint not null default 0,
    lease_until timestamptz,
    attempt_count integer not null default 0,
    data_json jsonb,
    summary_json jsonb,
    error_code text not null default ''
);
create index if not exists idx_ip_quality_intelligence_claim on ip_quality_intelligence(state, created_at);

create table if not exists ip_quality_source_cache (
    source_id text not null,
    resource text not null,
    credential_fingerprint text not null,
    fetched_at timestamptz not null,
    expires_at timestamptz not null,
    blocked_until timestamptz,
    payload_json jsonb,
    primary key (source_id, resource, credential_fingerprint)
);
