alter table nodes
  add column if not exists enrollment_token_consumed_at timestamptz;

create index if not exists idx_nodes_enrollment_token_active
  on nodes (enrollment_token_hash)
  where enrollment_token_hash is not null
    and enrollment_token_hash <> ''
    and enrollment_token_consumed_at is null;
