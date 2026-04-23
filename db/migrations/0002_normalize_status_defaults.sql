alter table if exists nodes
  alter column monitoring_status set default '启用';

alter table if exists nodes
  alter column binding_status set default '未绑定';

alter table if exists nodes
  alter column current_health_status set default '正常';

alter table if exists targets
  alter column run_status set default '启用';

alter table if exists targets
  alter column current_health_status set default '正常';

update nodes
set monitoring_status = '启用'
where monitoring_status = 'enabled';

update nodes
set binding_status = '未绑定'
where binding_status = 'unbound';

update nodes
set current_health_status = '正常'
where current_health_status = 'normal';

update targets
set run_status = '启用'
where run_status = 'enabled';

update targets
set current_health_status = '正常'
where current_health_status = 'normal';
