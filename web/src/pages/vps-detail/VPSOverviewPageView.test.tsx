import { fireEvent, render, screen } from '@testing-library/react'

import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { AssetDomainRecord, AssetServiceRecord, SubscriptionRecord, VPSOverview } from '../../lib/types'
import { VPSOverviewAnomalies } from './VPSOverviewAnomalies'
import { VPSOverviewPageView } from './VPSOverviewPageView'
import { useVPSDetailResources, type ResourceState, type VPSDetailResourceKind } from './hooks/useVPSDetailResources'
import type { VPSManagementController } from './hooks/useVPSManagementController'

vi.mock('./hooks/useVPSDetailResources', () => ({
  useVPSDetailResources: vi.fn(() => ({
    subscriptions: { status: 'ready', items: [], error: null },
    services: { status: 'ready', items: [], error: null },
    domains: { status: 'ready', items: [], error: null },
    retry: vi.fn(),
  })),
}))

function mockReadyResources(overrides: {
  subscriptions?: ResourceState<SubscriptionRecord>
  services?: ResourceState<AssetServiceRecord>
  domains?: ResourceState<AssetDomainRecord>
  retry?: (kind: VPSDetailResourceKind) => void
} = {}) {
  const retry = overrides.retry ?? vi.fn()
  vi.mocked(useVPSDetailResources).mockReturnValue({
    subscriptions: overrides.subscriptions ?? { status: 'ready', items: [], error: null },
    services: overrides.services ?? { status: 'ready', items: [], error: null },
    domains: overrides.domains ?? { status: 'ready', items: [], error: null },
    retry,
  })
  return retry
}

function healthyOverview(): VPSOverview {
  return {
    generated_at: '2026-08-20T00:00:00Z',
    identity: {
      vps_id: 'vps_001',
      display_name: '东京边缘',
      provider_name: 'Example',
      product_name: 'VPS',
      country: 'JP',
      region: 'Tokyo',
      city: 'Tokyo',
      datacenter: 'TK1',
      ipv4: '192.0.2.1',
      ipv6: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'high',
      labels: ['edge'],
      updated_at: '2026-08-20T00:00:00Z',
    },
    anomalies: [],
    summary: {
      overall: { status: 'healthy', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      monitoring: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      ip_quality: { status: 'low', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      renewal: { status: 'keep', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
    },
    recent_activity: {
      section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      items: [
        {
          activity_id: 'act_1',
          event_kind: 'record_created',
          event_at: '2026-08-19T10:00:00Z',
          recorded_at: '2026-08-19T10:00:01Z',
          source_kind: 'record_domain',
          backfilled: false,
          subjects: [],
          presentation: { version: 1, title: '最近一条' },
        },
        {
          activity_id: 'act_2',
          event_kind: 'command_executed',
          event_at: '2026-08-18T10:00:00Z',
          recorded_at: '2026-08-18T10:00:01Z',
          source_kind: 'command_audit',
          backfilled: false,
          subjects: [],
          presentation: { version: 1, title: '第二条' },
        },
        {
          activity_id: 'act_3',
          event_kind: 'evidence_captured',
          event_at: '2026-08-17T10:00:00Z',
          recorded_at: '2026-08-17T10:00:01Z',
          source_kind: 'evidence_snapshot',
          backfilled: false,
          subjects: [],
          presentation: { version: 1, title: '第三条' },
        },
        {
          activity_id: 'act_4',
          event_kind: 'asset_fact_changed',
          event_at: '2026-08-16T10:00:00Z',
          recorded_at: '2026-08-16T10:00:01Z',
          source_kind: 'asset_history',
          backfilled: false,
          subjects: [],
          presentation: { version: 1, title: '不应显示的第四条' },
        },
      ],
    },
    facts: [
      { key: 'ipv4', label: 'IPv4', value: '192.0.2.1' },
      { key: 'ssh', label: 'SSH', value: 'root@192.0.2.1:22' },
      { key: 'os_name', label: '系统', value: 'Debian' },
    ],

    relations: [{
      kind: 'monitoring_instances',
      count: 1,
      status: '正常',
      label: '监控实例',
      section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
    }],
    capabilities: ['records_v2_read'],
  }
}

function managementStub(overrides: Partial<VPSManagementController> = {}): VPSManagementController {
  return {
    panel: null,
    menuOpen: false,
    openMenu: vi.fn(),
    closeMenu: vi.fn(),
    openPanel: vi.fn(),
    closePanel: vi.fn(),
    ...overrides,
  }
}

describe('VPSOverviewPageView', () => {
  beforeEach(() => {
    mockReadyResources()
  })

  it('distinguishes local section navigation from activity routes and limits recent evidence', () => {
    const { container } = render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={healthyOverview()}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '新建记录' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '管理' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '概览' })).toHaveAttribute('href', '/vps/vps_001')

    const anomalies = container.querySelector('.vps-overview-anomalies')
    expect(anomalies).toBeNull()
    expect(screen.queryByText('需要关注')).not.toBeInTheDocument()

    expect(screen.getByRole('navigation', { name: '主体局部导航' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: '当前页分区' })).toBeInTheDocument()
    expect(screen.getByText('页面目录')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '页面目录' }))
    expect(screen.getByRole('link', { name: '最近活动' })).toHaveAttribute('href', expect.stringMatching(/#vps-section-activity$/))
    expect(screen.getByRole('link', { name: '活动' })).toHaveAttribute('href', '/vps/vps_001/activity')
    expect(screen.getByText('在用')).toBeInTheDocument()
    expect(screen.getByText('承载业务')).toBeInTheDocument()
    expect(screen.getByText('总体正常')).toBeInTheDocument()

    expect(screen.getByText('低风险')).toBeInTheDocument()
    expect(screen.getByText('人工记录')).toBeInTheDocument()
    expect(screen.getByText('系统事实')).toBeInTheDocument()
    expect(screen.getByText('不可变证据')).toBeInTheDocument()
    expect(screen.queryByText('不应显示的第四条')).not.toBeInTheDocument()
    expect(screen.getByText('最近一条')).toBeInTheDocument()
  })

  it('renders named resource rows from read records and does not treat loading as empty', () => {
    mockReadyResources({
      subscriptions: {
        status: 'loading',
        items: [{
          subscription_id: 'sub_001',
          vps_id: 'vps_001',
          price: 12,
          currency: 'USD',
          billing_cycle: 'monthly',
          billing_months: 1,
          monthly_price: 12,
          renew_at: '2026-09-01',
          auto_renew: true,
          auto_renew_cancelled: false,
          status: 'paused',
          payment_method: 'card',
          note: '',
          created_at: '2026-05-10T08:00:00Z',
          updated_at: '2026-05-10T08:00:00Z',
        }],
        error: null,
      },

      services: {
        status: 'loading',
        items: [],
        error: null,
      },
      domains: {
        status: 'ready',
        items: [{
          domain_id: 'dom_old',
          vps_id: 'vps_001',
          domain_name: 'retired.example.com',
          purpose: '旧站',
          status: 'retired',
          registrar: 'NameSilo',
          auto_renew: false,
          https_enabled: false,
          labels: [],
          note: '',
          created_at: '2026-05-10T08:00:00Z',
          updated_at: '2026-05-10T08:00:00Z',
        }],
        error: null,
      },
    })

    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={healthyOverview()}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )

    expect(screen.getByText('未命名订阅')).toBeInTheDocument()
    expect(screen.getByText('USD 12.00')).toBeInTheDocument()
    expect(screen.getByText('/月')).toBeInTheDocument()
    expect(screen.getByText('续费 2026-09-01')).toBeInTheDocument()
    expect(screen.queryByText('/月 · 续费 2026-09-01')).not.toBeInTheDocument()
    expect(screen.getByText('正在加载订阅…')).toBeInTheDocument()
    expect(screen.queryByText('已暂停')).not.toBeInTheDocument()
    expect(screen.getByText('正在加载服务…')).toBeInTheDocument()
    expect(screen.queryByText('暂无服务')).not.toBeInTheDocument()
    expect(screen.getByText('retired.example.com')).toBeInTheDocument()
    expect(screen.getByText('已退役')).toBeInTheDocument()
    expect(screen.getByText('旧站')).toBeInTheDocument()
    expect(screen.queryByText('已退役 · 旧站')).not.toBeInTheDocument()
    expect(screen.queryByText(/个服务/)).not.toBeInTheDocument()
    expect(screen.queryByText(/个域名/)).not.toBeInTheDocument()

  })

  it('owns degraded freshness and retry locally without rendering unavailable counts as zero', () => {
    const refresh = vi.fn()
    const retry = mockReadyResources({
      services: { status: 'error', items: [], error: '加载 VPS 服务失败' },
      domains: { status: 'ready', items: [], error: null },
    })
    const degraded: VPSOverview = {
      ...healthyOverview(),
      summary: {
        ...healthyOverview().summary,
        ip_quality: {
          status: '未知',
          section: {
            state: 'stale',
            observed_at: '2026-08-19T00:00:00Z',
            last_success_at: '2026-08-19T00:00:00Z',
            reason_code: 'ip_quality_stale',
          },
        },
        renewal: {
          status: '未知',
          section: {
            state: 'unavailable',
            observed_at: null,
            last_success_at: null,
            reason_code: 'subscription_unavailable',
          },
        },
      },
      recent_activity: {
        section: {
          state: 'unavailable',
          observed_at: null,
          last_success_at: null,
          reason_code: 'activity_projection_unavailable',
        },
        items: [],
      },
    }

    const { rerender } = render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={degraded}
          management={managementStub()}
          onRefresh={refresh}
          retrying={false}
        />
      </MemoryRouter>,
    )

    expect(screen.queryByText('暂无最近活动')).not.toBeInTheDocument()
    expect(screen.getByText('最近活动暂不可用，无法确认是否为空。')).toBeInTheDocument()
    expect(screen.queryByText('暂无服务')).not.toBeInTheDocument()
    expect(screen.getByText('加载 VPS 服务失败')).toBeInTheDocument()
    expect(screen.getByText('暂无域名')).toBeInTheDocument()
    expect(screen.queryByText('域名0')).not.toBeInTheDocument()
    const serviceRetry = screen.getByRole('button', { name: '重试 服务' })
    expect(serviceRetry.closest('a')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '刷新 概览 IP 质量' }))
    fireEvent.click(screen.getByRole('button', { name: '重试 概览 续费' }))
    fireEvent.click(screen.getByRole('button', { name: '重试 概览 最近活动' }))

    fireEvent.click(serviceRetry)
    expect(refresh).toHaveBeenCalledTimes(3)
    expect(retry).toHaveBeenCalledWith('services')
    expect(screen.queryByText('部分区段暂不可用。')).not.toBeInTheDocument()

    rerender(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={degraded}
          management={managementStub()}
          onRefresh={refresh}
          retrying
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('button', { name: '刷新 概览 IP 质量' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重试 概览 续费' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重试 概览 最近活动' })).toBeDisabled()

  })

  it('retains visible activity rows while the activity source is stale', () => {
    const stale = healthyOverview()
    stale.recent_activity.section = {
      state: 'stale',
      observed_at: '2026-08-19T00:00:00Z',
      last_success_at: '2026-08-19T00:00:00Z',
      reason_code: 'source_timestamp_invalid',
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={stale}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('最近一条')).toBeInTheDocument()
    expect(screen.getByText('活动源最新入库')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '刷新 概览 最近活动' })).toBeInTheDocument()

  })

  it('owns every overview command callback without changing routes', () => {
    const management = managementStub()
    const refresh = vi.fn()
    const overview = healthyOverview()
    overview.anomalies = [
      {
        rule_id: 'renewal.subscription.missing.v1',
        severity: 'warning',
        title: '缺少有效订阅',
        source: 'renewal',
        primary_action: { id: 'open_subscription', label: '管理订阅' },
        secondary_actions: [],
      },
      {
        rule_id: 'renewal.due.soon.v1',
        severity: 'notice',
        title: '续费临近',
        source: 'renewal',
        primary_action: { id: 'open_renewal_decision', label: '查看续费' },
        secondary_actions: [],
      },
      {
        rule_id: 'lifecycle.blocker.v1',
        severity: 'warning',
        title: '生命周期待处理',
        source: 'lifecycle',
        primary_action: { id: 'open_management', label: '打开管理' },
        secondary_actions: [],
      },
      {
        rule_id: 'source.unavailable.v1',
        severity: 'notice',
        title: '判断依据暂不可用',
        source: 'overview',
        primary_action: { id: 'retry_overview', label: '重试概览' },
        secondary_actions: [],
      },
      {
        rule_id: 'monitoring.unlinked.v1',
        severity: 'notice',
        title: '未关联监控实例',
        source: 'monitoring',
        primary_action: { id: 'open_monitoring_instances', label: '创建并接入 agent' },
        secondary_actions: [],
      },
    ]
    overview.relations = [
      ...overview.relations,
      {
        kind: 'services',
        count: 1,
        label: '服务',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      },
      {
        kind: 'domains',
        count: 1,
        label: '域名',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      },
    ]

    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <VPSOverviewPageView
          overview={overview}
          management={management}
          onRefresh={refresh}
          retrying={false}
        />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '管理订阅' }))
    fireEvent.click(screen.getByRole('button', { name: '查看续费' }))
    fireEvent.click(screen.getByRole('button', { name: '打开管理' }))
    fireEvent.click(screen.getByRole('button', { name: '重试概览' }))
    fireEvent.click(screen.getByRole('button', { name: '创建并接入 agent' }))
    fireEvent.click(screen.getByRole('button', { name: '查看实例' }))
    fireEvent.click(screen.getByRole('button', { name: '查看服务' }))
    fireEvent.click(screen.getByRole('button', { name: '查看域名' }))

    expect(management.openPanel).toHaveBeenCalledWith('subscription')
    expect(management.openPanel).toHaveBeenCalledWith('decision')
    expect(management.openPanel).toHaveBeenCalledWith('monitoring-instance-create')
    expect(management.openPanel).toHaveBeenCalledWith('monitoring-instance-evidence')
    expect(management.openPanel).toHaveBeenCalledWith('services-detail')
    expect(management.openPanel).toHaveBeenCalledWith('domains-detail')
    expect(management.openMenu).toHaveBeenCalledTimes(1)
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('shows a refresh failure without clearing last successful overview', () => {
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={healthyOverview()}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
          refreshError="VPS 概览请求或响应校验失败，请重试。"
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('status')).toHaveTextContent('本次刷新失败，当前仍展示上次成功数据')
    expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
  })

  it('hides cancellation and archive for an active VPS that is still keeping', () => {
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={healthyOverview()}
          management={managementStub({ menuOpen: true })}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.queryByRole('menuitem', { name: '取消 / 退役' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '归档' })).not.toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '编辑事实' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '管理' })).toHaveAttribute('aria-controls')
  })

  it('shows cancellation for an active VPS after a cancel renewal decision', () => {
    const overview = healthyOverview()
    overview.identity = { ...overview.identity, renewal_decision: 'cancel' }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub({ menuOpen: true })}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('menuitem', { name: '取消 / 退役' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '归档' })).not.toBeInTheDocument()
  })

  it('opens cancellation from the lifecycle blocker on to_cancel VPS', () => {
    const management = managementStub()
    const overview = healthyOverview()
    overview.identity = { ...overview.identity, lifecycle_status: 'to_cancel' }
    overview.anomalies = [{
      rule_id: 'lifecycle.blocker.v1',
      severity: 'warning',
      title: '生命周期待处理',
      source: 'lifecycle',
      primary_action: { id: 'open_management', label: '打开管理' },
      secondary_actions: [],
    }]
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={management}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: '打开管理' }))
    expect(management.openPanel).toHaveBeenCalledWith('cancellation')
  })

  it('opens cancellation from the lifecycle blocker on to_migrate VPS', () => {
    const management = managementStub()
    const overview = healthyOverview()
    overview.identity = { ...overview.identity, lifecycle_status: 'to_migrate' }
    overview.anomalies = [{
      rule_id: 'lifecycle.blocker.v1',
      severity: 'warning',
      title: '生命周期待处理',
      detail: 'to_migrate',
      source: 'lifecycle',
      primary_action: { id: 'open_management', label: '打开管理' },
      secondary_actions: [],
    }]
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={management}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: '打开管理' }))
    expect(management.openPanel).toHaveBeenCalledWith('cancellation')
    expect(management.openMenu).not.toHaveBeenCalled()
  })

  it('copies only raw IP and SSH facts', () => {
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={healthyOverview()}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('button', { name: '复制IPv4' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制SSH' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '复制系统' })).not.toBeInTheDocument()
  })

  it('does not present unlinked monitoring as healthy', () => {
    const overview = healthyOverview()
    overview.summary = {
      ...overview.summary,
      monitoring: {
        status: 'unlinked',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      },
      ip_quality: {
        status: 'failure',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      },
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const monitoring = screen.getByLabelText('监控关联')
    const ipQuality = screen.getByLabelText('IP 质量')
    const overall = screen.getByLabelText('总体')
    expect(monitoring).toHaveTextContent('未关联')
    expect(ipQuality).toHaveTextContent('采集失败')
    expect(overall).toHaveTextContent('观测不完整')
    expect(overall).not.toHaveTextContent('原始摘要')
    expect(overall.querySelector('.vps-overview-summary__status--ok')).toBeNull()
    expect(monitoring.querySelector('.vps-overview-summary__status--ok')).toBeNull()
    expect(ipQuality.querySelector('.vps-overview-summary__status--ok')).toBeNull()

  })

  it('keeps monitoring coverage visible with an independent issue detail', () => {
    const overview = healthyOverview()
    overview.summary = {
      ...overview.summary,
      monitoring: {
        status: '关注',
        detail: '心跳上报延迟',
        section: {
          state: 'stale',
          observed_at: '2026-08-19T00:00:00Z',
          last_success_at: '2026-08-19T00:00:00Z',
          reason_code: 'vps_stale',
        },
      },
    }
    overview.relations = [{
      kind: 'monitoring_instances',
      count: 1,
      status: '关注',
      label: '监控实例',
      section: {
        state: 'stale',
        observed_at: '2026-08-19T00:00:00Z',
        last_success_at: '2026-08-19T00:00:00Z',
        reason_code: 'vps_stale',
      },
    }]
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const monitoring = screen.getByLabelText('监控关联')
    expect(monitoring).toHaveTextContent('关注')
    expect(monitoring.querySelector('.vps-overview-summary__detail')).toHaveTextContent(/^1个实例 · 心跳上报延迟$/)
  })

  it('opens the existing resource panel from a named row without an edit action', () => {
    const management = managementStub()
    mockReadyResources({
      services: {
        status: 'ready',
        items: [{
          service_id: 'svc_001',
          vps_id: 'vps_001',
          name: 'Edge web',
          service_type: 'web',
          status: 'active',
          url: 'https://edge.example',
          labels: [],
          note: '',
          created_at: '2026-05-10T08:00:00Z',
          updated_at: '2026-05-10T08:00:00Z',
        }],
        error: null,
      },
    })
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={healthyOverview()}
          management={management}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('Web')).toBeInTheDocument()
    expect(screen.getByText('运行中')).toBeInTheDocument()
    expect(screen.getByText('https://edge.example')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /编辑/ })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '查看关联服务：Edge web' }))
    expect(management.openPanel).toHaveBeenCalledWith('services-detail')
    expect(management.openPanel).not.toHaveBeenCalledWith('service')
  })

  it('hides write actions for a cancelled overview without inventing sample detection', () => {
    const overview = healthyOverview()
    overview.identity = { ...overview.identity, lifecycle_status: 'cancelled' }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.queryByRole('button', { name: '管理' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '新建记录' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制IPv4' })).toBeInTheDocument()
  })

  it('renders identity product, importance, and labels without inventing a note', () => {
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={healthyOverview()}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('VPS')).toBeInTheDocument()
    expect(screen.getByText('重要性')).toBeInTheDocument()
    expect(screen.getByText('高')).toBeInTheDocument()
    expect(screen.getByText('标签')).toBeInTheDocument()
    expect(screen.getByText('edge')).toBeInTheDocument()
    expect(screen.queryByText('备注')).not.toBeInTheDocument()
  })

  it('keeps the last IP result when the source read fails', () => {
    const overview = healthyOverview()
    overview.summary.ip_quality = {
      status: 'low',
      section: {
        state: 'unavailable',
        observed_at: '2026-08-19T00:00:00Z',
        last_success_at: '2026-08-19T00:00:00Z',
        reason_code: 'ip_quality_timeout',
      },
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const ipQuality = screen.getByLabelText('IP 质量')
    expect(ipQuality).toHaveTextContent('低风险')
    expect(ipQuality).toHaveTextContent('仍展示上次可用结果')
    expect(ipQuality).toHaveTextContent('读取超时')
    expect(ipQuality).not.toHaveTextContent('IP 质量数据暂不可用，请稍后重试。')
  })

  it('does not duplicate an identical freshness badge on an unavailable conclusion', () => {
    const overview = healthyOverview()
    overview.summary.ip_quality = {
      status: '未知',
      section: {
        state: 'unavailable',
        observed_at: null,
        last_success_at: null,
        reason_code: 'ip_quality_unavailable',
      },
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const ipQuality = screen.getByLabelText('IP 质量')
    expect(ipQuality.querySelectorAll('.badge')).toHaveLength(1)
    expect(ipQuality).toHaveTextContent('暂不可用')
  })

  it('keeps supplied empty IP meaning without a failed-read retry', () => {
    const overview = healthyOverview()
    overview.summary.ip_quality = {
      status: 'unknown',
      detail: '暂未配置',
      section: {
        state: 'unavailable',
        observed_at: null,
        last_success_at: null,
        reason_code: 'future_non_read_note',
      },
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const ipQuality = screen.getByLabelText('IP 质量')
    expect(ipQuality).toHaveTextContent('暂未配置')
    expect(ipQuality).not.toHaveTextContent('暂不可用')
    expect(ipQuality).not.toHaveTextContent('请稍后重试')
    expect(screen.queryByRole('button', { name: /概览 IP 质量/ })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看 IP 质量结果' })).toBeInTheDocument()

  })

  it('does not hide a retained IP result when empty-config detail is also present', () => {
    const overview = healthyOverview()
    overview.summary.ip_quality = {
      status: 'low',
      detail: '暂未配置',
      section: {
        state: 'unavailable',
        observed_at: '2026-08-19T00:00:00Z',
        last_success_at: '2026-08-19T00:00:00Z',
        reason_code: 'future_non_read_note',
      },
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const ipQuality = screen.getByLabelText('IP 质量')
    expect(ipQuality).toHaveTextContent('低风险')
    expect(ipQuality).not.toHaveTextContent('暂未配置')
    expect(ipQuality).toHaveTextContent(/2026/)
    expect(screen.getByRole('link', { name: '查看历史报告' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /概览 IP 质量/ })).not.toBeInTheDocument()

  })

  it('groups each source freshness indicator with its own data time', () => {
    const overview = healthyOverview()
    overview.summary.overall.section = {
      state: 'stale',
      observed_at: '2026-08-19T01:00:00Z',
      last_success_at: '2026-08-19T01:00:00Z',
      reason_code: 'source_timestamp_invalid',
    }
    overview.summary.monitoring.section = {
      state: 'stale',
      observed_at: '2026-08-19T02:00:00Z',
      last_success_at: '2026-08-19T02:00:00Z',
      reason_code: 'source_timestamp_invalid',
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const overall = screen.getByLabelText('总体')
    const monitoring = screen.getByLabelText('监控关联')
    expect(overall.querySelector('.vps-observation__conclusion')).not.toHaveTextContent('数据陈旧')
    expect(overall.querySelector('.vps-observation__time')).toHaveTextContent('数据陈旧')
    expect(monitoring.querySelector('.vps-observation__conclusion')).toHaveTextContent('正常')
    expect(monitoring.querySelector('.vps-observation__conclusion')).not.toHaveTextContent('数据陈旧')
    expect(monitoring.querySelector('.vps-observation__time')).toHaveTextContent('数据陈旧')

  })

  it('uses the same planned-cancellation date context on primary and extra subscriptions', () => {
    const overview = healthyOverview()
    overview.identity.lifecycle_status = 'to_cancel'
    mockReadyResources({
      subscriptions: {
        status: 'ready',
        items: [
          {
            subscription_id: 'sub_primary',
            vps_id: 'vps_001',
            display_name: '主账单',
            price: 12,
            currency: 'USD',
            billing_cycle: 'monthly',
            billing_months: 1,
            monthly_price: 12,
            renew_at: '2026-10-15',
            trial_ends_at: '2026-09-15',
            auto_renew: true,
            auto_renew_cancelled: false,
            status: 'active',
            payment_method: 'card',
            note: '',
            created_at: '2026-05-10T08:00:00Z',
            updated_at: '2026-05-10T08:00:00Z',
          },
          {
            subscription_id: 'sub_extra',
            vps_id: 'vps_001',
            display_name: '附加账单',
            price: 8,
            currency: 'USD',
            billing_cycle: 'monthly',
            billing_months: 1,
            monthly_price: 8,
            renew_at: '2026-12-01',
            trial_ends_at: '2026-10-01',
            auto_renew: false,
            auto_renew_cancelled: false,
            status: 'active',
            payment_method: 'card',
            note: '',
            created_at: '2026-05-10T08:00:00Z',
            updated_at: '2026-05-10T08:00:00Z',
          },
        ],
        error: null,
      },
    })
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('登记续费日 2026-10-15 · 试用到期 2026-09-15')).toBeInTheDocument()
    expect(screen.getByText('登记续费日 2026-12-01 · 试用到期 2026-10-01')).toBeInTheDocument()
    expect(screen.queryByText(/试用账期到期/)).not.toBeInTheDocument()
  })


})

describe('VPSOverviewAnomalies', () => {
  it('renders anomaly actions between identity and summary when present', () => {
    render(
      <MemoryRouter>
        <VPSOverviewAnomalies
          vpsId="vps_001"
          anomalies={[{
            rule_id: 'renewal.due.soon.v1',
            severity: 'warning',
            title: '续费临期',
            detail: '7 天内到期',
            source: 'subscription',
            primary_action: { id: 'open_renewal_decision', label: '处理续费' },
            secondary_actions: [],
          }]}
          onCommand={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { name: '需要关注' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '处理续费' })).toBeInTheDocument()
  })

  it('does not leak classified machine tokens in summary or anomaly details', () => {
    const overview = healthyOverview()
    overview.summary.ip_quality = {
      ...overview.summary.ip_quality,
      status: 'high',
      detail: 'partial',
    }
    overview.identity = { ...overview.identity, lifecycle_status: 'to_cancel', importance: 'normal' }
    overview.anomalies = [
      {
        rule_id: 'lifecycle.blocker.v1',
        severity: 'warning',
        title: '生命周期待处理',
        detail: 'to_cancel',
        source: 'lifecycle',
        primary_action: { id: 'open_management', label: '打开管理' },
        secondary_actions: [],
      },
      {
        rule_id: 'source.unavailable.v1',
        severity: 'notice',
        title: '判断依据暂不可用',
        detail: 'ip_quality, monitoring, renewal',
        source: 'overview',
        primary_action: { id: 'retry_overview', label: '重试概览' },
        secondary_actions: [],
      },
    ]
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('高风险')).toBeInTheDocument()
    expect(screen.getByText('采集不完整')).toBeInTheDocument()
    expect(screen.getAllByText('待取消').length).toBeGreaterThan(0)
    expect(screen.getByText('IP 质量、监控、续费')).toBeInTheDocument()
    expect(screen.queryByText('partial')).not.toBeInTheDocument()
    expect(screen.queryByText('to_cancel')).not.toBeInTheDocument()
    expect(screen.queryByText('ip_quality, monitoring, renewal')).not.toBeInTheDocument()
    expect(screen.queryByText('high')).not.toBeInTheDocument()
  })

  it('shows disabled IP quality as not configured without a history footnote', () => {
    const overview = healthyOverview()
    overview.summary.ip_quality = {
      status: 'not_configured',
      detail: 'not_configured',
      section: {
        state: 'ready',
        observed_at: null,
        last_success_at: null,
        reason_code: '',
      },
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview}
          management={managementStub()}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )
    const cell = screen.getByLabelText('IP 质量')
    expect(cell).toHaveTextContent('未启用')
    expect(cell).not.toHaveTextContent('存在历史报告（当前未启用）')
    expect(cell).not.toHaveTextContent('高风险')
    expect(cell).not.toHaveTextContent('暂不可用')
    expect(screen.queryByText('ip_quality_disabled_has_history')).not.toBeInTheDocument()
  })

  it('returns null for healthy empty anomalies', () => {
    const { container } = render(
      <VPSOverviewAnomalies vpsId="vps_001" anomalies={[]} onCommand={vi.fn()} />,
    )
    expect(container.firstChild).toBeNull()
  })
})
