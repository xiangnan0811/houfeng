import http from 'node:http'
import { URL } from 'node:url'

const PORT = Number(process.env.HOUFENG_VISUAL_MOCK_PORT || 58080)

function isoDate(daysFromToday) {
  const date = new Date()
  date.setUTCDate(date.getUTCDate() + daysFromToday)
  return date.toISOString().slice(0, 10)
}

function isoTimestamp(daysFromToday, hour = 8) {
  const date = new Date()
  date.setUTCDate(date.getUTCDate() + daysFromToday)
  date.setUTCHours(hour, 0, 0, 0)
  return date.toISOString()
}

function isoTimestampHoursAgo(hours) {
  const date = new Date(Date.now() - hours * 60 * 60 * 1000)
  date.setMilliseconds(0)
  return date.toISOString()
}

const providers = [
  {
    provider_id: 'provider_hetzner',
    name: 'Hetzner',
    website: 'https://www.hetzner.com',
    panel_url: 'https://console.hetzner.cloud',
    account_hint: 'ops-main',
    country: 'DE',
    note: 'Primary EU compute account.',
    rating: 5,
    labels: ['core', 'eu'],
    created_at: isoTimestamp(-120),
    updated_at: isoTimestamp(-2),
  },
  {
    provider_id: 'provider_vultr',
    name: 'Vultr',
    website: 'https://www.vultr.com',
    panel_url: 'https://my.vultr.com',
    account_hint: 'edge-lab',
    country: 'US',
    note: 'Edge and migration candidates.',
    rating: 4,
    labels: ['edge', 'backup'],
    created_at: isoTimestamp(-90),
    updated_at: isoTimestamp(-3),
  },
  {
    provider_id: 'provider_netcup',
    name: 'Netcup',
    website: 'https://www.netcup.de',
    panel_url: 'https://www.servercontrolpanel.de',
    account_hint: 'legacy-eu',
    country: 'DE',
    note: 'Legacy low-cost instances under review.',
    rating: 3,
    labels: ['legacy'],
    created_at: isoTimestamp(-300),
    updated_at: isoTimestamp(-7),
  },
]

const vpsAssets = [
  {
    vps_id: 'vps_ams_core',
    display_name: 'ams-core-01',
    provider_id: 'provider_hetzner',
    provider_name: 'Hetzner',
    product_name: 'CPX31',
    order_ref: 'HZ-2026-001',
    country: 'NL',
    region: 'EU-West',
    city: 'Amsterdam',
    datacenter: 'AMS1',
    ipv4: '192.0.2.10',
    ipv6: '2001:db8:10::1',
    ssh_host: 'ams-core-01.example.test',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian 12',
    virtualization: 'kvm',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'unreviewed',
    importance: 'critical',
    labels: ['prod', 'web'],
    note: 'Primary asset workflow fixture with complete facts.',
    active_node_link_count: 2,
    created_at: isoTimestamp(-100),
    updated_at: isoTimestamp(-1),
    archived_at: null,
  },
  {
    vps_id: 'vps_sjc_edge',
    display_name: 'sjc-edge-02',
    provider_id: 'provider_vultr',
    provider_name: 'Vultr',
    product_name: 'High Frequency 2 vCPU',
    order_ref: 'VU-2026-EDGE',
    country: 'US',
    region: 'US-West',
    city: 'San Jose',
    datacenter: 'SJC',
    ipv4: '198.51.100.24',
    ipv6: '',
    ssh_host: 'sjc-edge-02.example.test',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Ubuntu 24.04',
    virtualization: 'kvm',
    lifecycle_status: 'to_migrate',
    usage_status: 'standby',
    renewal_decision: 'migrate',
    importance: 'normal',
    labels: ['edge', 'migration'],
    note: 'Migration candidate with active subscription evidence.',
    active_node_link_count: 1,
    created_at: isoTimestamp(-80),
    updated_at: isoTimestamp(-1),
    archived_at: null,
  },
  {
    vps_id: 'vps_tokyo_lab',
    display_name: 'tokyo-lab-unlinked',
    provider_id: null,
    provider_name: '',
    product_name: '',
    order_ref: '',
    country: '',
    region: '',
    city: '',
    datacenter: '',
    ipv4: '',
    ipv6: '',
    ssh_host: '',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: '',
    virtualization: '',
    lifecycle_status: 'testing',
    usage_status: 'unknown',
    renewal_decision: 'unreviewed',
    importance: 'low',
    labels: ['needs-facts'],
    note: 'Fixture row intentionally missing subscription, provider, location, and access facts.',
    active_node_link_count: 0,
    created_at: isoTimestamp(-20),
    updated_at: isoTimestamp(-1),
    archived_at: null,
  },
  {
    vps_id: 'vps_fra_legacy',
    display_name: 'fra-legacy-cancel',
    provider_id: 'provider_netcup',
    provider_name: 'Netcup',
    product_name: 'RS 1000',
    order_ref: 'NC-LEGACY',
    country: 'DE',
    region: 'EU-Central',
    city: 'Frankfurt',
    datacenter: 'FRA',
    ipv4: '203.0.113.7',
    ipv6: '',
    ssh_host: '',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian 11',
    virtualization: 'kvm',
    lifecycle_status: 'to_cancel',
    usage_status: 'idle',
    renewal_decision: 'cancel',
    importance: 'low',
    labels: ['legacy', 'cost-review'],
    note: 'Cancel queue fixture with auto-renew cancelled.',
    active_node_link_count: 0,
    created_at: isoTimestamp(-240),
    updated_at: isoTimestamp(-4),
    archived_at: null,
  },
  {
    vps_id: 'vps_archive_old',
    display_name: 'archive-old-2019',
    provider_id: 'provider_netcup',
    provider_name: 'Netcup',
    product_name: 'Legacy VPS',
    order_ref: 'NC-2019',
    country: 'DE',
    region: 'EU-Central',
    city: 'Nuremberg',
    datacenter: 'NBG',
    ipv4: '203.0.113.40',
    ipv6: '',
    ssh_host: '',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian 10',
    virtualization: 'kvm',
    lifecycle_status: 'archived',
    usage_status: 'idle',
    renewal_decision: 'keep',
    importance: 'low',
    labels: ['archived'],
    note: 'Archived fixture row for quick-view coverage.',
    active_node_link_count: 0,
    created_at: isoTimestamp(-900),
    updated_at: isoTimestamp(-60),
    archived_at: isoTimestamp(-30),
  },
]

const subscriptions = [
  {
    subscription_id: 'sub_ams_core',
    vps_id: 'vps_ams_core',
    price: 10.5,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    monthly_price: 10.5,
    started_at: isoDate(-100),
    renew_at: isoDate(8),
    auto_renew: true,
    auto_renew_cancelled: false,
    status: 'active',
    payment_method: 'card-main',
    note: 'Renewal window fixture.',
    created_at: isoTimestamp(-100),
    updated_at: isoTimestamp(-1),
  },
  {
    subscription_id: 'sub_sjc_edge',
    vps_id: 'vps_sjc_edge',
    price: 24,
    currency: 'USD',
    billing_cycle: 'quarterly',
    billing_months: 3,
    monthly_price: 8,
    started_at: isoDate(-80),
    renew_at: isoDate(21),
    auto_renew: false,
    auto_renew_cancelled: false,
    status: 'active',
    payment_method: 'paypal-edge',
    note: 'Migration candidate subscription.',
    created_at: isoTimestamp(-80),
    updated_at: isoTimestamp(-2),
  },
  {
    subscription_id: 'sub_fra_legacy',
    vps_id: 'vps_fra_legacy',
    price: 5,
    currency: 'EUR',
    billing_cycle: 'monthly',
    billing_months: 1,
    monthly_price: 5,
    started_at: isoDate(-240),
    renew_at: isoDate(5),
    auto_renew: true,
    auto_renew_cancelled: true,
    status: 'active',
    payment_method: 'sepa',
    note: 'Cancel queue subscription with auto-renew cancelled.',
    created_at: isoTimestamp(-240),
    updated_at: isoTimestamp(-4),
  },
  {
    subscription_id: 'sub_archive_old',
    vps_id: 'vps_archive_old',
    price: 3,
    currency: 'EUR',
    billing_cycle: 'monthly',
    billing_months: 1,
    monthly_price: 3,
    started_at: isoDate(-900),
    renew_at: isoDate(-20),
    auto_renew: false,
    auto_renew_cancelled: true,
    status: 'cancelled',
    payment_method: 'legacy-card',
    note: 'Archived subscription fixture.',
    created_at: isoTimestamp(-900),
    updated_at: isoTimestamp(-30),
  },
]

const nodes = [
  {
    node_id: 'node_hkg_edge_01',
    display_name: 'hkg-edge-01',
    group: 'asset-prod',
    region: 'APAC',
    city: 'Hong Kong',
    provider: 'Hetzner',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: ['prod', 'edge', 'vps-linked'],
    note: 'Severe node fixture for UX-5 abnormal evidence.',
    current_health_status: '严重',
    last_heartbeat_at: isoTimestampHoursAgo(1),
    last_sync_at: isoTimestampHoursAgo(1),
    current_active_incident_count: 3,
    current_primary_issue_summary: 'CPU 持续高位且心跳延迟，需要先核对 VPS 负载。',
    created_at: isoTimestamp(-80),
    updated_at: isoTimestampHoursAgo(1),
  },
  {
    node_id: 'node_pending_sfo_02',
    display_name: 'sfo-pending-onboarding',
    group: 'asset-intake',
    region: 'US-West',
    city: 'San Francisco',
    provider: 'Vultr',
    lifecycle_status: '待接入',
    monitoring_status: '启用',
    binding_status: '未绑定',
    labels: ['onboarding', 'needs-agent'],
    note: 'Pending onboarding fixture.',
    current_health_status: '正常',
    last_heartbeat_at: null,
    last_sync_at: null,
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-6),
    updated_at: isoTimestampHoursAgo(6),
  },
  {
    node_id: 'node_ams_conflict_03',
    display_name: 'ams-conflict-03',
    group: 'asset-prod',
    region: 'EU-West',
    city: 'Amsterdam',
    provider: 'Hetzner',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '指纹变更待确认',
    labels: ['prod', 'binding-review'],
    note: 'Fingerprint conflict fixture.',
    current_health_status: '关注',
    last_heartbeat_at: isoTimestampHoursAgo(5),
    last_sync_at: isoTimestampHoursAgo(5),
    current_active_incident_count: 1,
    current_primary_issue_summary: '等待确认新的主机指纹。',
    created_at: isoTimestamp(-60),
    updated_at: isoTimestampHoursAgo(5),
  },
  {
    node_id: 'node_fra_maint_04',
    display_name: 'fra-maintenance-04',
    group: 'asset-ops',
    region: 'EU-Central',
    city: 'Frankfurt',
    provider: 'Netcup',
    lifecycle_status: '在用',
    monitoring_status: '维护中',
    binding_status: '已绑定',
    labels: ['maintenance', 'db'],
    note: 'Maintenance window fixture.',
    current_health_status: '正常',
    last_heartbeat_at: isoTimestampHoursAgo(2),
    last_sync_at: isoTimestampHoursAgo(2),
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-140),
    updated_at: isoTimestampHoursAgo(2),
  },
  {
    node_id: 'node_sin_paused_05',
    display_name: 'sin-paused-05',
    group: 'asset-observe',
    region: 'APAC',
    city: 'Singapore',
    provider: 'Manual',
    lifecycle_status: '观察中',
    monitoring_status: '暂停',
    binding_status: '已绑定',
    labels: ['paused', 'cost-review'],
    note: 'Paused monitoring fixture.',
    current_health_status: '正常',
    last_heartbeat_at: isoTimestampHoursAgo(30),
    last_sync_at: isoTimestampHoursAgo(30),
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-220),
    updated_at: isoTimestampHoursAgo(30),
  },
  {
    node_id: 'node_old_retired_06',
    display_name: 'old-retired-06',
    group: 'archive',
    region: 'EU-Central',
    city: 'Nuremberg',
    provider: 'Netcup',
    lifecycle_status: '已退役',
    monitoring_status: '暂停',
    binding_status: '已绑定',
    labels: ['archived', 'legacy'],
    note: 'Retired node fixture for inventory completeness.',
    current_health_status: '正常',
    last_heartbeat_at: isoTimestamp(-45),
    last_sync_at: isoTimestamp(-45),
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-600),
    updated_at: isoTimestamp(-45),
  },
]

const targets = [
  {
    target_id: 'target_api_core',
    name: 'api-core.example.test',
    target_type: 'service',
    host: 'api-core.example.test',
    base_port: 443,
    execution_node_labels: ['prod', 'edge'],
    run_status: '启用',
    group: 'asset-prod',
    labels: ['prod', 'api', 'vps-linked'],
    note: 'Abnormal API target fixture.',
    current_health_status: '告警',
    current_active_incident_count: 2,
    last_success_at: isoTimestampHoursAgo(7),
    last_failure_at: isoTimestampHoursAgo(1),
    current_primary_issue_summary: 'HTTP 5xx 持续出现，需结合 Node 与资产决策核对。',
    created_at: isoTimestamp(-90),
    updated_at: isoTimestampHoursAgo(1),
  },
  {
    target_id: 'target_china_ref',
    name: 'china-reference-latency',
    target_type: 'china_reference',
    host: 'www.baidu.com',
    base_port: 443,
    execution_node_labels: ['cn-probe'],
    run_status: '启用',
    group: 'network-reference',
    labels: ['reference', 'china', 'notification'],
    note: 'Reference target with notification event coverage.',
    current_health_status: '关注',
    current_active_incident_count: 1,
    last_success_at: isoTimestampHoursAgo(4),
    last_failure_at: isoTimestampHoursAgo(2),
    current_primary_issue_summary: '跨境参考延迟超过关注阈值。',
    created_at: isoTimestamp(-70),
    updated_at: isoTimestampHoursAgo(2),
  },
  {
    target_id: 'target_www_maint',
    name: 'www-maintenance.example.test',
    target_type: 'service',
    host: 'www-maintenance.example.test',
    base_port: 443,
    execution_node_labels: ['prod'],
    run_status: '维护中',
    group: 'asset-ops',
    labels: ['maintenance', 'web'],
    note: 'Maintenance target fixture.',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: isoTimestampHoursAgo(3),
    last_failure_at: null,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-120),
    updated_at: isoTimestampHoursAgo(3),
  },
  {
    target_id: 'target_docs_paused',
    name: 'docs-paused.example.test',
    target_type: 'service',
    host: 'docs-paused.example.test',
    base_port: 443,
    execution_node_labels: ['docs'],
    run_status: '暂停',
    group: 'docs',
    labels: ['paused', 'docs'],
    note: 'Paused target fixture.',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: isoTimestampHoursAgo(48),
    last_failure_at: null,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-160),
    updated_at: isoTimestampHoursAgo(48),
  },
  {
    target_id: 'target_legacy_archived',
    name: 'legacy-archived.example.test',
    target_type: 'service',
    host: 'legacy-archived.example.test',
    base_port: 80,
    execution_node_labels: ['legacy'],
    run_status: '已归档',
    group: 'archive',
    labels: ['archived', 'legacy'],
    note: 'Archived target fixture.',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: isoTimestamp(-30),
    last_failure_at: null,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-500),
    updated_at: isoTimestamp(-30),
  },
  {
    target_id: 'target_no_exec_labels',
    name: 'no-execution-label.example.test',
    target_type: 'service',
    host: 'no-execution-label.example.test',
    base_port: 8443,
    execution_node_labels: [],
    run_status: '启用',
    group: 'asset-intake',
    labels: ['needs-coverage'],
    note: 'Coverage-gap target fixture.',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: null,
    last_failure_at: null,
    current_primary_issue_summary: '',
    created_at: isoTimestamp(-12),
    updated_at: isoTimestampHoursAgo(12),
  },
]

const events = [
  {
    event_id: 'event_node_severe_started',
    incident_id: 'inc_node_hkg_cpu',
    incident_class: 'node_resource_pressure',
    object_type: 'node',
    object_id: 'node_hkg_edge_01',
    event_type: 'incident_started',
    severity: '严重',
    summary: 'hkg-edge-01 CPU 与 load5 同时进入严重区间。',
    created_at: isoTimestampHoursAgo(1),
    _labels: ['prod', 'edge', 'vps-linked'],
    _notification_sent: true,
    _is_backfilled: false,
  },
  {
    event_id: 'event_target_api_escalated',
    incident_id: 'inc_target_api_5xx',
    incident_class: 'target_probe_failure',
    object_type: 'target',
    object_id: 'target_api_core',
    event_type: 'incident_escalated',
    severity: '告警',
    summary: 'api-core.example.test HTTP 探测失败率升高。',
    created_at: isoTimestampHoursAgo(2),
    _labels: ['prod', 'api', 'vps-linked'],
    _notification_sent: true,
    _is_backfilled: false,
  },
  {
    event_id: 'event_target_china_notice',
    incident_id: 'inc_target_china_latency',
    incident_class: 'target_probe_failure',
    object_type: 'target',
    object_id: 'target_china_ref',
    event_type: 'incident_started',
    severity: '关注',
    summary: '中国参考入口延迟超过关注阈值，已发送通知样例。',
    created_at: isoTimestampHoursAgo(4),
    _labels: ['reference', 'china', 'notification'],
    _notification_sent: true,
    _is_backfilled: false,
  },
  {
    event_id: 'event_target_recovered',
    incident_id: 'inc_target_api_5xx',
    incident_class: 'target_probe_failure',
    object_type: 'target',
    object_id: 'target_api_core',
    event_type: 'incident_recovered',
    severity: '正常',
    summary: 'api-core.example.test 探测恢复，保留用于 recovery filter。',
    created_at: isoTimestampHoursAgo(5),
    _labels: ['prod', 'api', 'vps-linked'],
    _notification_sent: false,
    _is_backfilled: false,
  },
  {
    event_id: 'event_node_maintenance_entered',
    incident_id: 'runtime_node_fra_maint',
    incident_class: '',
    object_type: 'node',
    object_id: 'node_fra_maint_04',
    event_type: 'node_monitoring_maintenance_entered',
    severity: '',
    summary: 'fra-maintenance-04 进入维护窗口。',
    created_at: isoTimestampHoursAgo(6),
    _labels: ['maintenance', 'db'],
    _notification_sent: false,
    _is_backfilled: false,
  },
  {
    event_id: 'event_target_maintenance_exited',
    incident_id: 'runtime_target_www_maint',
    incident_class: '',
    object_type: 'target',
    object_id: 'target_www_maint',
    event_type: 'target_maintenance_exited',
    severity: '',
    summary: 'www-maintenance.example.test 退出维护窗口。',
    created_at: isoTimestampHoursAgo(7),
    _labels: ['maintenance', 'web'],
    _notification_sent: false,
    _is_backfilled: false,
  },
  {
    event_id: 'event_backfilled_node',
    incident_id: 'inc_backfilled_node_disk',
    incident_class: 'node_disk_pressure',
    object_type: 'node',
    object_id: 'node_hkg_edge_01',
    event_type: 'incident_started',
    severity: '告警',
    summary: '补传观测触发的磁盘压力事件，默认应被事件流排除。',
    created_at: isoTimestampHoursAgo(8),
    _labels: ['prod', 'edge', 'backfilled'],
    _notification_sent: false,
    _is_backfilled: true,
  },
  {
    event_id: 'event_node_binding_confirmed',
    incident_id: 'binding_node_ams_conflict',
    incident_class: '',
    object_type: 'node',
    object_id: 'node_ams_conflict_03',
    event_type: 'node_binding_rebind_confirmed',
    severity: '关注',
    summary: 'ams-conflict-03 新指纹确认重新绑定。',
    created_at: isoTimestampHoursAgo(10),
    _labels: ['prod', 'binding-review'],
    _notification_sent: false,
    _is_backfilled: false,
  },
  {
    event_id: 'event_target_paused',
    incident_id: 'runtime_target_docs_paused',
    incident_class: '',
    object_type: 'target',
    object_id: 'target_docs_paused',
    event_type: 'target_paused',
    severity: '',
    summary: 'docs-paused.example.test 已暂停探测。',
    created_at: isoTimestampHoursAgo(12),
    _labels: ['paused', 'docs'],
    _notification_sent: false,
    _is_backfilled: false,
  },
  {
    event_id: 'event_target_archived',
    incident_id: 'runtime_target_legacy_archived',
    incident_class: '',
    object_type: 'target',
    object_id: 'target_legacy_archived',
    event_type: 'target_archived',
    severity: '',
    summary: 'legacy-archived.example.test 已归档。',
    created_at: isoTimestampHoursAgo(36),
    _labels: ['archived', 'legacy'],
    _notification_sent: false,
    _is_backfilled: false,
  },
]

function queryBool(url, key) {
  const value = url.searchParams.get(key)
  if (value == null) return false
  return ['1', 'true', 't', 'yes', 'y', 'on'].includes(value.toLowerCase())
}

function filterSubscriptions(url) {
  let rows = [...subscriptions]
  const vpsId = url.searchParams.get('vps_id')
  if (vpsId) rows = rows.filter((row) => row.vps_id === vpsId)
  const status = url.searchParams.get('status')
  if (status) rows = rows.filter((row) => row.status === status)
  const renewWithinDays = url.searchParams.get('renew_within_days')
  if (renewWithinDays) {
    const windowDays = Number.parseInt(renewWithinDays, 10)
    if (Number.isFinite(windowDays)) {
      const today = new Date()
      today.setUTCHours(0, 0, 0, 0)
      rows = rows.filter((row) => {
        if (!row.renew_at) return false
        const renewAt = new Date(`${row.renew_at}T00:00:00Z`)
        return Math.round((renewAt.getTime() - today.getTime()) / 86400000) <= windowDays
      })
    }
  }
  if (url.searchParams.get('sort') === 'renew_at') {
    rows.sort((a, b) => String(a.renew_at || '9999-12-31').localeCompare(String(b.renew_at || '9999-12-31')))
    if (url.searchParams.get('order') === 'desc') rows.reverse()
  }
  return rows
}

function filterVPS(url) {
  let rows = [...vpsAssets]
  for (const key of ['provider_id', 'lifecycle_status', 'usage_status', 'renewal_decision']) {
    const value = url.searchParams.get(key)
    if (value) rows = rows.filter((row) => String(row[key] || '') === value)
  }
  return rows
}

function filterEvents(url) {
  let rows = [...events]
  if (!queryBool(url, 'include_backfilled')) rows = rows.filter((row) => !row._is_backfilled)
  for (const key of ['object_type', 'object_id', 'severity', 'event_type']) {
    const value = url.searchParams.get(key)
    if (value) rows = rows.filter((row) => String(row[key] || '') === value)
  }
  const label = url.searchParams.get('label')
  if (label) rows = rows.filter((row) => row._labels.includes(label))
  if (queryBool(url, 'notification_only')) rows = rows.filter((row) => row._notification_sent)
  if (queryBool(url, 'recovery_only')) rows = rows.filter((row) => row.event_type === 'incident_recovered')
  if (queryBool(url, 'maintenance_only')) {
    const maintenanceTypes = new Set([
      'node_monitoring_maintenance_entered',
      'node_monitoring_maintenance_exited',
      'target_maintenance_entered',
      'target_maintenance_exited',
    ])
    rows = rows.filter((row) => maintenanceTypes.has(row.event_type))
  }
  rows.sort((a, b) => String(b.created_at || '').localeCompare(String(a.created_at || '')))
  const limit = Number.parseInt(url.searchParams.get('limit') || '50', 10)
  return rows.slice(0, Number.isFinite(limit) && limit > 0 ? limit : 50).map((row) => {
    const clean = { ...row }
    delete clean._labels
    delete clean._notification_sent
    delete clean._is_backfilled
    return clean
  })
}

function dashboardFromAssetWorkflow() {
  return {
    snapshot_generated_at: isoTimestamp(0),
    total_node_count: 3,
    total_target_count: 5,
    abnormal_node_count: 1,
    abnormal_target_count: 1,
    severe_node_count: 0,
    severe_target_count: 0,
    maintenance_node_count: 0,
    maintenance_target_count: 1,
    pending_onboarding_node_count: 1,
    paused_node_count: 0,
    retired_node_count: 0,
    paused_target_count: 1,
    archived_target_count: 0,
    recent_new_incident_count: 2,
    recent_recovery_count: 1,
    group_summaries: [
      {
        group: 'asset-fixture',
        node_count: 3,
        target_count: 5,
        abnormal_node_count: 1,
        abnormal_target_count: 1,
        severe_node_count: 0,
        severe_target_count: 0,
        maintenance_node_count: 0,
        maintenance_target_count: 1,
      },
    ],
    notification_status: {
      telegram_configured: true,
      telegram_runtime_managed: true,
      telegram_runtime_apply_active: true,
      feishu_configured: false,
    },
    asset_summary: {
      renewal_due_30d_subscription_count: 3,
      renewal_due_30d_vps_count: 3,
      unreviewed_vps_count: 2,
      to_cancel_vps_count: 1,
      to_migrate_vps_count: 1,
      unlinked_vps_count: 3,
      abnormal_linked_vps_count: 1,
      cost_by_currency: [
        { currency: 'USD', monthly_total: 18.5, yearly_total: 222 },
        { currency: 'EUR', monthly_total: 8, yearly_total: 96 },
      ],
    },
    abnormal_nodes: [],
    abnormal_targets: [],
    recent_events: [],
    new_incident_trend_24h: [0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0],
    recovery_trend_24h: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0],
  }
}

function dashboardFromObservability() {
  const abnormalNodes = nodes.filter((node) => node.current_health_status !== '正常')
  const abnormalTargets = targets.filter((target) => target.current_health_status !== '正常')
  const groups = [...new Set([...nodes, ...targets].map((item) => item.group || '未分组'))].sort()
  return {
    snapshot_generated_at: isoTimestampHoursAgo(0),
    total_node_count: nodes.length,
    total_target_count: targets.length,
    abnormal_node_count: abnormalNodes.length,
    abnormal_target_count: abnormalTargets.length,
    severe_node_count: nodes.filter((node) => node.current_health_status === '严重').length,
    severe_target_count: targets.filter((target) => target.current_health_status === '严重').length,
    maintenance_node_count: nodes.filter((node) => node.monitoring_status === '维护中').length,
    maintenance_target_count: targets.filter((target) => target.run_status === '维护中').length,
    pending_onboarding_node_count: nodes.filter((node) => node.lifecycle_status === '待接入' || ['未绑定', '指纹变更待确认'].includes(node.binding_status)).length,
    paused_node_count: nodes.filter((node) => node.monitoring_status === '暂停').length,
    retired_node_count: nodes.filter((node) => node.lifecycle_status === '已退役').length,
    paused_target_count: targets.filter((target) => target.run_status === '暂停').length,
    archived_target_count: targets.filter((target) => target.run_status === '已归档').length,
    recent_new_incident_count: events.filter((event) => event.event_type === 'incident_started').length,
    recent_recovery_count: events.filter((event) => event.event_type === 'incident_recovered').length,
    group_summaries: groups.map((group) => {
      const groupNodes = nodes.filter((node) => (node.group || '未分组') === group)
      const groupTargets = targets.filter((target) => (target.group || '未分组') === group)
      return {
        group,
        node_count: groupNodes.length,
        target_count: groupTargets.length,
        abnormal_node_count: groupNodes.filter((node) => node.current_health_status !== '正常').length,
        abnormal_target_count: groupTargets.filter((target) => target.current_health_status !== '正常').length,
        severe_node_count: groupNodes.filter((node) => node.current_health_status === '严重').length,
        severe_target_count: groupTargets.filter((target) => target.current_health_status === '严重').length,
        maintenance_node_count: groupNodes.filter((node) => node.monitoring_status === '维护中').length,
        maintenance_target_count: groupTargets.filter((target) => target.run_status === '维护中').length,
      }
    }),
    notification_status: {
      telegram_configured: true,
      telegram_runtime_managed: false,
      telegram_runtime_apply_active: false,
      feishu_configured: false,
    },
    asset_summary: {
      renewal_due_30d_subscription_count: 0,
      renewal_due_30d_vps_count: 0,
      unreviewed_vps_count: 0,
      to_cancel_vps_count: 0,
      to_migrate_vps_count: 0,
      unlinked_vps_count: 0,
      abnormal_linked_vps_count: 0,
      cost_by_currency: [],
    },
    abnormal_nodes: abnormalNodes.slice(0, 4).map((node) => ({
      node_id: node.node_id,
      display_name: node.display_name,
      group: node.group,
      region: node.region,
      city: node.city,
      provider: node.provider,
      lifecycle_status: node.lifecycle_status,
      monitoring_status: node.monitoring_status,
      current_health_status: node.current_health_status,
      last_heartbeat_at: node.last_heartbeat_at,
      current_active_incident_count: node.current_active_incident_count,
      current_primary_issue_summary: node.current_primary_issue_summary,
    })),
    abnormal_targets: abnormalTargets.slice(0, 4).map((target) => ({
      target_id: target.target_id,
      name: target.name,
      target_type: target.target_type,
      host: target.host,
      base_port: target.base_port,
      run_status: target.run_status,
      group: target.group,
      current_health_status: target.current_health_status,
      last_success_at: target.last_success_at,
      last_failure_at: target.last_failure_at,
      current_active_incident_count: target.current_active_incident_count,
      current_primary_issue_summary: target.current_primary_issue_summary,
    })),
    recent_events: filterEvents(new URL('http://mock/api/events?limit=6')),
    new_incident_trend_24h: [0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 1],
    recovery_trend_24h: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0],
  }
}

function respond(res, status, body) {
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Credentials': 'true',
    'Access-Control-Allow-Headers': 'content-type, accept',
    'Access-Control-Allow-Methods': 'GET,POST,PATCH,PUT,DELETE,OPTIONS',
  })
  res.end(JSON.stringify(body))
}

function handler(req, res) {
  const url = new URL(req.url || '/', `http://${req.headers.host || '127.0.0.1'}`)
  const method = req.method || 'GET'

  if (method === 'OPTIONS') {
    respond(res, 200, {})
    return
  }

  if (method === 'GET' && url.pathname === '/api/auth/me') {
    respond(res, 200, { user_id: 'user_visual_evidence', username: 'visual-evidence', role: 'admin', display_name: 'Visual Evidence' })
    return
  }
  if (method === 'POST' && url.pathname === '/api/auth/logout') {
    respond(res, 200, {})
    return
  }
  if (method === 'GET' && url.pathname === '/api/dashboard') {
    const profile = url.searchParams.get('profile') || ''
    respond(res, 200, profile === 'observability' ? dashboardFromObservability() : dashboardFromAssetWorkflow())
    return
  }
  if (method === 'GET' && url.pathname === '/api/providers') {
    respond(res, 200, providers)
    return
  }
  if (method === 'GET' && url.pathname === '/api/vps') {
    respond(res, 200, filterVPS(url))
    return
  }
  if (method === 'GET' && url.pathname === '/api/subscriptions') {
    respond(res, 200, filterSubscriptions(url))
    return
  }
  if (method === 'GET' && url.pathname === '/api/nodes') {
    respond(res, 200, nodes)
    return
  }
  if (method === 'GET' && url.pathname === '/api/nodes/sparklines') {
    respond(res, 200, {
      nodes: Object.fromEntries(nodes.map((node) => [node.node_id, {
        cpu_usage_pct: [42, 45, 48, 52, 57, 63, 72, 81, 88, 93, 96, 94],
        mem_used_pct: [61, 62, 64, 63, 66, 68, 70, 73, 76, 78, 80, 79],
        disk_used_pct: [50, 50, 51, 51, 52, 52, 53, 54, 54, 55, 56, 56],
      }]))
    })
    return
  }
  if (method === 'GET' && url.pathname === '/api/targets') {
    respond(res, 200, targets)
    return
  }
  if (method === 'GET' && url.pathname === '/api/targets/sparklines') {
    respond(res, 200, { targets: Object.fromEntries(targets.map((target) => [target.target_id, { latency: [42, 45, 55, 66, 80, 92, 120, 130, 110, 90, 70, 65] }])) })
    return
  }
  if (method === 'GET' && url.pathname === '/api/events') {
    respond(res, 200, { items: filterEvents(url) })
    return
  }

  respond(res, 404, { error: 'mock visual API has no fixture for this request', method, path: url.pathname })
}

const server = http.createServer(handler)
server.listen(PORT, '127.0.0.1', () => {
  console.log(`mock visual API listening on http://127.0.0.1:${PORT}`)
})
