import { afterEach, describe, expect, it, vi } from 'vitest'

import type {
  AssetDomainRecord,
  AssetServiceRecord,
  SubscriptionRecord,
  VPSAssetDetail,
  VPSIPQualityReport,
  VPSTimeline,
} from '../../lib/types'
import { buildVPSDetailOverviewModel, renewalDueLabel } from './vpsDetailOverviewModel'

const baseDetail: VPSAssetDetail = {
  vps_id: 'vps_001',
  display_name: 'Tokyo Edge',
  provider_id: 'pv_001',
  provider_name: 'Hetzner',
  product_name: 'cx22',
  order_ref: 'ord-1',
  country: 'JP',
  region: 'Kanto',
  city: 'Tokyo',
  datacenter: 'nrt',
  ipv4: '192.0.2.1',
  ipv6: '',
  ssh_host: '192.0.2.1',
  ssh_port: 22,
  ssh_user: 'root',
  os_name: 'Debian',
  virtualization: 'kvm',
  lifecycle_status: 'active',
  usage_status: 'in_use',
  renewal_decision: 'keep',
  importance: 'normal',
  labels: ['edge'],
  note: 'primary',
  active_monitoring_instance_link_count: 1,
  running_monitoring_instance_count: 1,
  running_target_count: 1,
  ip_quality_summary: {
    report_id: 'ipq_001',
    vps_id: 'vps_001',
    observed_at: '2026-06-08T12:00:00Z',
    ip_address: '192.0.2.1',
    ip_version: 4,
    status: 'success',
    risk_level: 'high',
    use_region_code: 'JP',
    use_region_name: 'Japan',
    stale: false,
    ambiguous: false,
    provider_count: 1,
    unlockable_count: 2,
  },
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: null,
  monitoring_instance_links: [{
    monitoring_instance_id: 'mi_001',
    display_name: 'Tokyo Monitoring Instance',
    group: 'edge',
    region: 'JP',
    city: 'Tokyo',
    provider: 'Monitoring Hint',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    current_health_status: '正常',
    last_heartbeat_at: '2026-05-09T08:10:00Z',
    last_sync_at: '2026-05-09T08:11:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    linked_at: '2026-05-09T08:00:00Z',
    note: 'primary',
  }],
}

const baseSubscription: SubscriptionRecord = {
  subscription_id: 'sub_001',
  vps_id: 'vps_001',
  price: 12,
  currency: 'USD',
  billing_cycle: 'monthly',
  billing_months: 1,
  billing_period_unit: 'month',
  billing_period_length: 1,
  monthly_price: 12,
  started_at: '2026-05-01',
  renew_at: '2026-08-15',
  auto_renew: true,
  auto_renew_cancelled: false,
  renewal_mode: 'auto',
  status: 'active',
  payment_method: 'card',
  note: 'primary subscription',
  monthly_price_base: 86,
  yearly_price_base: 1032,
  base_currency: 'CNY',
  created_at: '2026-05-10T08:00:00Z',
  updated_at: '2026-05-10T08:00:00Z',
}

const baseTimeline: VPSTimeline = {
  vps_id: 'vps_001',
  renewal_decisions: [{
    decision_id: 'rdec_001',
    vps_id: 'vps_001',
    from_decision: 'unreviewed',
    to_decision: 'keep',
    reason: '稳定承载边缘流量',
    decided_at: '2026-05-09T08:12:00Z',
    created_at: '2026-05-09T08:12:00Z',
  }],
  price_histories: [],
  ip_histories: [],
  spec_snapshots: [],
  experience_logs: [{
    experience_log_id: 'elog_001',
    vps_id: 'vps_001',
    category: 'network',
    severity: 'warning',
    summary: '晚高峰丢包',
    details: '已向服务商提交工单',
    occurred_at: '2026-05-09T08:16:00Z',
    created_at: '2026-05-09T08:16:30Z',
  }],
}

const baseService: AssetServiceRecord = {
  service_id: 'svc_001',
  vps_id: 'vps_001',
  target_id: 'tg_001',
  name: 'Blog',
  service_type: 'web',
  status: 'active',
  url: 'https://blog.example.com',
  port: 443,
  labels: ['prod'],
  note: 'primary service',
  created_at: '2026-05-10T08:00:00Z',
  updated_at: '2026-05-10T08:00:00Z',
}

const baseDomain: AssetDomainRecord = {
  domain_id: 'dom_001',
  vps_id: 'vps_001',
  service_id: 'svc_001',
  target_id: 'tg_001',
  domain_name: 'www.example.com',
  purpose: 'site',
  status: 'active',
  registrar: 'NameSilo',
  expires_at: '2026-07-01',
  auto_renew: true,
  https_enabled: true,
  labels: ['prod'],
  note: 'primary domain',
  created_at: '2026-05-10T08:00:00Z',
  updated_at: '2026-05-10T08:00:00Z',
}

const baseIPQuality: VPSIPQualityReport = {
  summary: baseDetail.ip_quality_summary ?? null,
  latest_report: null,
  provider_results: [{
    provider: 'ipinfo',
    risk_level: 'high',
    risk_score: '80',
    region_code: 'JP',
    region_name: 'Japan',
    is_proxy: false,
    is_tor: false,
    is_vpn: true,
    is_server: true,
    is_abuser: false,
    is_robot: false,
  }],
  service_unlocks: [
    { service: 'chatgpt', status: 'unlocked', region: 'JP', unlock_type: 'native' },
    { service: 'netflix', status: 'blocked', region: 'US', unlock_type: 'none' },
  ],
  history: [],
}

function baseMonitoringInstance() {
  const monitoringInstance = baseDetail.monitoring_instance_links[0]
  if (!monitoringInstance) throw new Error('fixture must include a monitoring instance')
  return monitoringInstance
}

function buildModel(overrides: Partial<Parameters<typeof buildVPSDetailOverviewModel>[0]> = {}) {
  return buildVPSDetailOverviewModel({
    detail: baseDetail,
    timeline: baseTimeline,
    primarySubscription: baseSubscription,
    activeSubscription: baseSubscription,
    subscriptionLoadFailed: false,
    subscriptionError: null,
    services: [baseService],
    domains: [baseDomain],
    ipQuality: baseIPQuality,
    ipQualityError: null,
    ...overrides,
  })
}

describe('vpsDetailOverviewModel', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('formats renewal timing using days first and months after 30 days', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-01T08:00:00Z'))

    expect(renewalDueLabel({ ...baseSubscription, renew_at: '2026-06-20' })).toBe('19 天后续费')
    expect(renewalDueLabel({ ...baseSubscription, renew_at: '2026-08-15' })).toBe('3 个月后续费')
    expect(renewalDueLabel({ ...baseSubscription, renew_at: '2027-08-01' })).toBe('15 个月后续费')
    expect(renewalDueLabel({
      ...baseSubscription,
      auto_renew_cancelled: true,
      renew_at: '2026-07-15',
      ends_at: '2026-07-20',
    })).toBe('已取消自动续费 · 2 个月后到期')
  })

  it('orders mixed timeline families globally by date and namespaces source IDs', () => {
    const baseDecision = baseTimeline.renewal_decisions[0]
    const baseExperience = baseTimeline.experience_logs[0]
    if (!baseDecision || !baseExperience) throw new Error('base timeline fixture is incomplete')

    const metadataPriceHistory = {
      price_history_id: 'price_new',
      subscription_id: 'sub_001',
      vps_id: 'vps_001',
      from_price: 12,
      to_price: 12,
      from_currency: 'USD',
      to_currency: 'USD',
      from_billing_cycle: 'monthly',
      to_billing_cycle: 'annual',
      from_billing_months: 1,
      to_billing_months: 12,
      from_billing_period_unit: 'month',
      to_billing_period_unit: 'year',
      from_billing_period_length: 1,
      to_billing_period_length: 1,
      from_monthly_price: 12,
      to_monthly_price: 12,
      from_renew_at: '2026-05-01',
      to_renew_at: '2026-06-01',
      from_auto_renew: true,
      to_auto_renew: false,
      from_auto_renew_cancelled: false,
      to_auto_renew_cancelled: false,
      from_renewal_mode: 'auto',
      to_renewal_mode: 'manual',
      from_status: 'active',
      to_status: 'active',
      changed_at: '2026-05-13T08:00:00Z',
      created_at: '2026-05-13T08:00:01Z',
    } satisfies VPSTimeline['price_histories'][number]
    const statusOnlyPriceHistory = {
      ...metadataPriceHistory,
      price_history_id: 'price_status_only',
      from_billing_cycle: 'monthly',
      to_billing_cycle: 'monthly',
      from_billing_months: 1,
      to_billing_months: 1,
      from_billing_period_unit: 'month',
      to_billing_period_unit: 'month',
      from_billing_period_length: 1,
      to_billing_period_length: 1,
      from_renew_at: '2026-05-01',
      to_renew_at: '2026-05-01',
      from_auto_renew: true,
      to_auto_renew: true,
      from_renewal_mode: 'auto',
      to_renewal_mode: 'auto',
      from_status: 'active',
      to_status: 'cancelled',
      changed_at: '2026-05-12T08:00:00Z',
      created_at: '2026-05-12T08:00:01Z',
    } satisfies VPSTimeline['price_histories'][number]

    const model = buildModel({
      timeline: {
        vps_id: 'vps_001',
        renewal_decisions: [{
          ...baseDecision,
          decision_id: 'decision_old',
          decided_at: '2026-05-11T08:00:00Z',
          reason: '历史续费判断',
        }],
        price_histories: [metadataPriceHistory, statusOnlyPriceHistory],
        ip_histories: [{
          ip_history_id: 'ip_old',
          vps_id: 'vps_001',
          from_ipv4: '192.0.2.1',
          to_ipv4: '192.0.2.2',
          from_ipv6: '',
          to_ipv6: '',
          changed_at: '2026-05-10T08:00:00Z',
          created_at: '2026-05-10T08:00:01Z',
        }],
        spec_snapshots: [{
          snapshot_id: 'snapshot_new',
          vps_id: 'vps_001',
          product_name: 'cx32',
          ssh_host: '192.0.2.1',
          ssh_port: 22,
          ssh_user: 'root',
          os_name: 'Debian',
          virtualization: 'kvm',
          captured_at: '2026-05-11T08:00:00Z',
          created_at: '2026-05-11T08:00:01Z',
        }],
        experience_logs: [{
          ...baseExperience,
          experience_log_id: 'experience_latest',
          occurred_at: '2026-05-14T08:00:00Z',
          summary: '最新网络经验',
        }],
      },
    })

    expect(model.recentActivity).toEqual([
      {
        key: 'experience:experience_latest',
        date: '2026-05-14T08:00:00Z',
        kind: '经验记录',
        summary: '最新网络经验',
      },
      {
        key: 'price:price_new',
        date: '2026-05-13T08:00:00Z',
        kind: '账单更新',
        summary: '计费周期 每 1 月 -> 每 1 年 · 续费日 2026-05-01 -> 2026-06-01 · 续费方式 自动续费 -> 手动续费',
      },
      {
        key: 'price:price_status_only',
        date: '2026-05-12T08:00:00Z',
        kind: '账单更新',
        summary: '账单更新',
      },
    ])
  })

  it('does not treat subscription load failure as a missing subscription', () => {
    const model = buildModel({
      primarySubscription: null,
      activeSubscription: null,
      subscriptionLoadFailed: true,
      subscriptionError: 'subscription backend down',
    })

    const subscriptionItem = model.relatedItems.find((item) => item.key === 'subscription')
    expect(subscriptionItem?.primary).toBe('订阅证据暂不可用')
    expect(subscriptionItem?.tone).toBe('notice')
    expect(subscriptionItem?.quickActions.map((action) => action.label)).not.toContain('新增订阅事实')
    expect(model.judgement.attentionItems.map((item) => item.title)).toContain('订阅证据暂不可用')
    expect(model.judgement.attentionItems[0]?.primaryAction).toEqual({ kind: 'link', label: '核对订阅', to: '/subscriptions?vps_id=vps_001&view=details' })
  })

  it('promotes true missing subscription only after a successful empty subscription response', () => {
    const model = buildModel({
      primarySubscription: null,
      activeSubscription: null,
      subscriptionLoadFailed: false,
      subscriptionError: null,
    })

    const subscriptionItem = model.relatedItems.find((item) => item.key === 'subscription')
    expect(subscriptionItem?.primary).toBe('未记录当前订阅')
    expect(subscriptionItem?.tone).toBe('critical')
    expect(subscriptionItem?.quickActions.map((action) => action.label)).toContain('新增订阅事实')
    expect(model.judgement.attentionItems.map((item) => item.title)).toContain('缺少当前订阅')
    expect(model.judgement.attentionItems[0]?.primaryAction).toEqual({ kind: 'modal', label: '新增订阅事实', mode: 'subscription' })
  })

  it('keeps monitoring attention in the monitoring section', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-01T08:00:00Z'))

    const model = buildModel({
      detail: {
        ...baseDetail,
        monitoring_instance_links: [{
          ...baseMonitoringInstance(),
          current_health_status: '告警',
          current_active_incident_count: 2,
          current_primary_issue_summary: 'packet loss',
        }],
      },
    })

    expect(model.judgement.attentionItems).toEqual([])
    expect(model.monitoringAttentionItems.map((item) => item.title)).toEqual(['运行观测需要核对'])
  })

  it('keeps cancellation, monitoring and renewal work together in the top judgement', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-01T08:00:00Z'))

    const model = buildModel({
      detail: {
        ...baseDetail,
        lifecycle_status: 'to_cancel',
        renewal_decision: 'cancel',
        monitoring_instance_links: [{
          ...baseMonitoringInstance(),
          current_health_status: '关注',
          current_active_incident_count: 1,
          current_primary_issue_summary: 'legacy service still responds',
        }],
      },
      primarySubscription: {
        ...baseSubscription,
        auto_renew_cancelled: true,
        renew_at: '2026-08-15',
        ends_at: '2026-08-15',
      },
      activeSubscription: {
        ...baseSubscription,
        auto_renew_cancelled: true,
        renew_at: '2026-08-15',
        ends_at: '2026-08-15',
      },
    })

    expect(model.judgement.primaryAction).toEqual({
      kind: 'modal',
      label: '处理取消/退役',
      mode: 'cancellation',
    })
    expect(model.judgement.attentionItems.map((item) => item.title)).toEqual([
          '取消/退役',
          '自动续费已取消',
        ])
    expect(model.judgement.attentionItems[0]?.primaryAction).toEqual({
      kind: 'modal',
      label: '处理取消/退役',
      mode: 'cancellation',
    })
  })
})
