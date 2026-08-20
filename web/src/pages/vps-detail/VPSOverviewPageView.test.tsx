import { render, screen } from '@testing-library/react'
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
      lifecycle_status: '在用',
      usage_status: '生产',
      renewal_decision: '续费',
      importance: '高',
      labels: ['edge'],
      updated_at: '2026-08-20T00:00:00Z',
    },
    anomalies: [],
    summary: {
      overall: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      monitoring: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      ip_quality: { status: '低风险', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      renewal: { status: '续费', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
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
      kind: 'monitoring_instance',
      count: 1,
      status: '正常',
      route: '/monitoring/mi_001',
      label: '监控实例',
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
          onManagePanel={vi.fn()}
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
    expect(sectionTitles).toEqual(['最近活动', '稳定事实', '关联资源'])
    expect(screen.queryByText('不应显示的第四条')).not.toBeInTheDocument()
    expect(screen.getByText('最近一条')).toBeInTheDocument()
  })
})

describe('VPSOverviewAnomalies', () => {
  it('renders anomaly actions between identity and summary when present', () => {
    render(
      <MemoryRouter>
        <VPSOverviewAnomalies
          anomalies={[{
            rule_id: 'renewal.due_soon',
            severity: 'warning',
            title: '续费临期',
            detail: '7 天内到期',
            source: 'subscription',
            primary_action: { id: 'open_renewal', label: '处理续费', route: '/vps/vps_001' },
            secondary_actions: [],
          }]}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { name: '需要关注' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '处理续费' })).toBeInTheDocument()
  })

  it('returns null for healthy empty anomalies', () => {
    const { container } = render(<VPSOverviewAnomalies anomalies={[]} />)
    expect(container.firstChild).toBeNull()
  })
})
