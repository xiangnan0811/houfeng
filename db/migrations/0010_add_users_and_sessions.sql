create table users (
  user_id              text primary key,
  username             text not null unique,
  password_hash        text not null,
  display_name         text not null default '',
  role                 text not null default 'admin',
  created_at           timestamptz not null default now(),
  password_changed_at  timestamptz not null default now()
);

create table sessions (
  session_id    text primary key,
  user_id       text not null references users(user_id) on delete cascade,
  issued_at     timestamptz not null default now(),
  last_seen_at  timestamptz not null default now(),
  expires_at    timestamptz not null,
  user_agent    text not null default '',
  client_ip     text not null default ''
);

create index sessions_user_idx on sessions(user_id);
create index sessions_expires_idx on sessions(expires_at);
