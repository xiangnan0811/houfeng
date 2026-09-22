import { fireEvent, render, screen, within } from '@testing-library/react'

import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { SubscriptionRecord, VPSAssetDetail, VPSTimeline } from '../../lib/types'
import { VPSDetailOverviewPanel } from './VPSDetailOverviewPanel'
import { buildVPSDetailOverviewModel, type VPSDetailOverviewModelInput } from './vpsDetailOverviewModel'

const timeline: VPSTimeline = {
  vps_id: 'vps_001',
  renewal_decisions: [],
  price_histories: [],
  ip_histories: [],
  spec_snapshots: [],
  experience_logs: [],
}

const subscription: SubscriptionRecord = {
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
  renew_at: '2026-08-01',
  auto_renew: true,
  auto_renew_cancelled: false,
  renewal_mode: 'auto',
  status: 'active',
  payment_method: 'card',
  note: '',
  created_at: '2026-05-10T08:00:00Z',
  updated_at: '2026-05-10T08:00:00Z',
}

const detail: VPSAssetDetail = {
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
  active_monitoring_instance_link_count: 0,
  running_monitoring_instance_count: 0,
  running_target_count: 0,
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: null,
  monitoring_instance_links: [],
}

const noop = vi.fn()

function renderOverview(
  overrides: Partial<VPSDetailOverviewModelInput> = {},
  handlers: { onTimelineOpen?: () => void } = {},
) {
  const model = buildVPSDetailOverviewModel({
    detail,
    timeline,
    primarySubscription: subscription,
    activeSubscription: subscription,
    subscriptionLoadFailed: false,
    subscriptionError: null,
    services: [],
    domains: [],
    ipQuality: null,
    ipQualityError: 'ip quality backend down',
    ...overrides,
  })

  render(
    <MemoryRouter>
      <VPSDetailOverviewPanel
        model={model}
        vpsId="vps_001"
        isArchived={false}
        lifecycleSubmitting={false}
        subscriptions={overrides.primarySubscription === null ? [] : [subscription]}
        subscriptionsError={overrides.subscriptionLoadFailed ? overrides.subscriptionError ?? null : null}
        onDecisionEdit={noop}
        onTimelineOpen={handlers.onTimelineOpen ?? noop}

        onServicesOpen={noop}
        onDomainsOpen={noop}
        onCancellationOpen={noop}
        onFactEdit={noop}
        onFactsOpen={noop}
        onExperienceLog={noop}
        onMonitoringEvidence={noop}
        onMonitoringAgent={noop}
        onMonitoringLink={noop}
        onSubscriptionOpen={noop}
        onValidityExtend={noop}
        onServiceCreate={noop}
        onDomainCreate={noop}
        onArchiveStart={noop}
        onRestoreStart={noop}
      />
    </MemoryRouter>,
  )
}

describe('VPSDetailOverviewPanel', () => {
  it('keeps missing runtime and unavailable IP quality in monitoring, not business judgement', () => {
    renderOverview()

    const ops = screen.getByLabelText('当前判断')
    const monitoring = screen.getByLabelText('运行观测')

    const identity = screen.getByLabelText('VPS 综合基础信息')

    expect(within(ops).queryByText('缺少运行观测')).not.toBeInTheDocument()
    expect(within(ops).queryByText('IP 质量暂不可用')).not.toBeInTheDocument()
    expect(within(ops).queryByText('尚未关联监控实例')).not.toBeInTheDocument()
    expect(within(identity).queryByText('缺少运行观测')).not.toBeInTheDocument()
    expect(monitoring).toHaveTextContent('缺少运行观测')
    expect(within(monitoring).getByText('IP 质量暂不可用')).toBeInTheDocument()
    expect(within(ops).getByText('决策')).toBeInTheDocument()
    expect(within(ops).getByText(/续费 2026/)).toBeInTheDocument()
    expect(within(ops).queryByText('动作', { selector: 'dt' })).not.toBeInTheDocument()
    expect(within(ops).getByRole('link', { name: '订阅' })).toBeInTheDocument()
  })



  it('offers raw IPv4 and SSH copy on identity facts', () => {
    renderOverview()
    const identity = screen.getByLabelText('VPS 综合基础信息')
    expect(within(identity).getByText('192.0.2.1')).toBeInTheDocument()
    expect(within(identity).getByText('root@192.0.2.1:22')).toBeInTheDocument()
    expect(within(identity).getByRole('button', { name: '复制IPv4' })).toBeInTheDocument()
    expect(within(identity).getByRole('button', { name: '复制SSH' })).toBeInTheDocument()
  })


  it('shows old evidence dates without presenting an unreported sync as current', () => {
    renderOverview({
      ipQualityError: null,
      detail: {
        ...detail,
        updated_at: '2001-01-15T12:00:00Z',
        active_monitoring_instance_link_count: 1,
        monitoring_instance_links: [{
          monitoring_instance_id: 'mi_001',
          display_name: 'Tokyo Monitoring',
          group: 'edge',
          region: 'JP',
          city: 'Tokyo',
          provider: 'Runtime provider',
          lifecycle_status: '在用',
          monitoring_status: '启用',
          binding_status: '已绑定',
          current_health_status: '正常',
          last_heartbeat_at: '2002-02-15T12:00:00Z',
          last_sync_at: null,
          current_active_incident_count: 0,
          current_primary_issue_summary: '',
          linked_at: '2001-01-15T12:00:00Z',
          note: '',
        }],
        ip_quality_summary: {
          observed_at: '2003-03-15T12:00:00Z',
          ip_address: '192.0.2.1',
          ip_version: 4,
          status: 'ready',
          stale: true,
          ambiguous: false,
          provider_count: 1,
          unlockable_count: 0,
        },
      },
    })

    expect(screen.getByText('更新').closest('.vps-overview-identity__meta-item')).toHaveTextContent('2001/01')
    const freshness = screen.getByLabelText('观测时间')
    expect(within(freshness).getByText(/2002\/02/)).toBeVisible()
    expect(within(freshness).getByText(/2003\/03/)).toBeVisible()
    expect(within(freshness).getByText('—')).toBeVisible()
  })

  it('opens full history from 查看全部 without embedding the ledger', () => {
    const onTimelineOpen = vi.fn()
    renderOverview({}, { onTimelineOpen })
    expect(screen.queryByRole('region', { name: '单机台账' })).not.toBeInTheDocument()
    fireEvent.click(within(screen.getByRole('region', { name: '最近活动' })).getByRole('button', { name: '查看全部' }))
    expect(onTimelineOpen).toHaveBeenCalledTimes(1)
  })

})
