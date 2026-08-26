create table if not exists subscription_create_idempotency (
  idempotency_key text primary key,
  request_digest text not null,
  subscription_id text not null references subscriptions(subscription_id) on delete cascade,
  created_at timestamptz not null default now(),
  constraint subscription_create_idempotency_key_format
    check (
      char_length(idempotency_key) between 8 and 128
      and idempotency_key ~ '^[A-Za-z0-9._:-]+$'
    ),
  constraint subscription_create_idempotency_digest_sha256
    check (request_digest ~ '^[0-9a-f]{64}$')
);

create index if not exists idx_subscription_create_idempotency_subscription
  on subscription_create_idempotency (subscription_id);
