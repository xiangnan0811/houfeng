alter table if exists nodes
  add column if not exists enrollment_token_issued_at timestamptz,
  add column if not exists pending_binding_fingerprint text,
  add column if not exists pending_binding_first_seen_at timestamptz,
  add column if not exists pending_binding_last_seen_at timestamptz,
  add column if not exists pending_binding_attempt_count integer not null default 0;
