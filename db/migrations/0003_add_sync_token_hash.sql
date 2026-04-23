alter table if exists nodes
  add column if not exists sync_token_hash text;
