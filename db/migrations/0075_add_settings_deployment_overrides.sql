alter table center_settings
  add column incident_sweep_runtime_managed boolean not null default true;
alter table center_settings
  alter column incident_sweep_runtime_managed set default false;
