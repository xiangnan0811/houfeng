alter table host_samples
  add column if not exists mem_total_bytes bigint not null default 0,
  add column if not exists disk_total_bytes bigint not null default 0;
