import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { VPSOverview } from '../../lib/types'
import { VPSOverviewAnomalies } from './VPSOverviewAnomalies'
import { VPSOverviewPageView } from './VPSOverviewPageView'
import type { VPSManagementController } from './hooks/useVPSManagementController'

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
    facts: [{ key: 'ipv4', label: 'IPv4', value: '192.0.2.1' }],
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
  it('renders identity actions, local nav, and section order without anomaly chrome when healthy', () => {
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
    expect(screen.getByRole('link', { name: '时间线' })).toHaveAttribute('href', '/vps/vps_001/activity')
    expect(screen.getByRole('button', { name: '管理' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '概览' })).toHaveAttribute('href', '/vps/vps_001')

    const anomalies = container.querySelector('.vps-overview-anomalies')
    expect(anomalies).toBeNull()
    expect(screen.queryByText('需要关注')).not.toBeInTheDocument()
    expect(screen.queryByText('动作：无')).not.toBeInTheDocument()

    const sectionTitles = Array.from(container.querySelectorAll('h2')).map((node) => node.textContent)
    expect(sectionTitles).toEqual(['决策摘要', '最近活动', '稳定事实', '关联资源'])
    expect(screen.getByText('在用')).toBeInTheDocument()
    expect(screen.getByText('承载业务')).toBeInTheDocument()
    expect(screen.getByText('总体正常')).toBeInTheDocument()
    expect(screen.getByText('低风险')).toBeInTheDocument()
    expect(screen.getByText('保留')).toBeInTheDocument()
    expect(screen.getByText('人工记录')).toBeInTheDocument()
    expect(screen.getByText('系统事实')).toBeInTheDocument()
    expect(screen.getByText('不可变证据')).toBeInTheDocument()
    expect(screen.queryByText('不应显示的第四条')).not.toBeInTheDocument()
    expect(screen.getByText('最近一条')).toBeInTheDocument()
  })

  it('owns degraded freshness and retry locally without rendering unavailable counts as zero', () => {
    const refresh = vi.fn()
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
      relations: [{
        kind: 'services',
        count: 0,
        status: 'unavailable',
        label: '服务',
        section: {
          state: 'unavailable',
          observed_at: null,
          last_success_at: null,
          reason_code: 'relation_unavailable',
        },
      }, {
        kind: 'domains',
        count: 0,
        label: '域名',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      }],
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
    const serviceTrigger = screen.getByRole('button', { name: '服务—暂不可用' })
    const domainTrigger = screen.getByRole('button', { name: '域名0' })
    expect(within(serviceTrigger).getByText('—')).toBeInTheDocument()
    expect(within(domainTrigger).getByText('0')).toBeInTheDocument()
    const serviceRetry = screen.getByRole('button', { name: '重试 服务' })
    expect(serviceRetry.closest('a')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试 IP 质量' }))
    fireEvent.click(screen.getByRole('button', { name: '重试 续费' }))
    fireEvent.click(screen.getByRole('button', { name: '重试 最近活动' }))
    fireEvent.click(serviceRetry)
    expect(refresh).toHaveBeenCalledTimes(4)
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
    for (const button of screen.getAllByRole('button', { name: /^重试 / })) {
      expect(button).toBeDisabled()
    }
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
    expect(screen.getByRole('button', { name: '重试 最近活动' })).toBeInTheDocument()
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
    fireEvent.click(screen.getByRole('button', { name: /监控实例/ }))
    fireEvent.click(screen.getByRole('button', { name: /^服务/ }))
    fireEvent.click(screen.getByRole('button', { name: /^域名/ }))

    expect(management.openPanel).toHaveBeenCalledWith('subscription')
    expect(management.openPanel).toHaveBeenCalledWith('decision')
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

  it('hides cancellation and archive for an active VPS', () => {
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
    expect(management.openMenu).not.toHaveBeenCalled()
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

  it('returns null for healthy empty anomalies', () => {
    const { container } = render(
      <VPSOverviewAnomalies vpsId="vps_001" anomalies={[]} onCommand={vi.fn()} />,
    )
    expect(container.firstChild).toBeNull()
  })
})
