alter table host_samples
  add column if not exists network_rates_valid boolean;
