import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { VPSOverview } from '../../lib/types'
import { VPSOverviewPageView } from './VPSOverviewPageView'
import { useVPSDetailResources } from './hooks/useVPSDetailResources'
import type { VPSManagementController } from './hooks/useVPSManagementController'

vi.mock('../../lib/readOnlyPreview', () => ({
  READ_ONLY_PREVIEW: true,
}))

vi.mock('./hooks/useVPSDetailResources', () => ({
  useVPSDetailResources: vi.fn(() => ({
    subscriptions: { status: 'ready', items: [], error: null },
    services: { status: 'ready', items: [], error: null },
    domains: { status: 'ready', items: [], error: null },
    retry: vi.fn(),
  })),
}))

function overview(): VPSOverview {
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
      ipv4: '192.0.2.10',
      ipv6: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'high',
      labels: [],
      updated_at: '2026-08-20T00:00:00Z',
    },
    anomalies: [{
      rule_id: 'renewal.subscription.missing.v1',
      severity: 'warning',
      title: '缺少有效订阅',
      source: 'renewal',
      primary_action: { id: 'open_subscription', label: '管理订阅' },
      secondary_actions: [],
    }],
    summary: {
      overall: { status: 'healthy', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      monitoring: { status: '正常', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      ip_quality: { status: 'low', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
      renewal: { status: 'keep', section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' } },
    },
    recent_activity: {
      section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      items: [],
    },
    facts: [{ key: 'ipv4', label: 'IPv4', value: '192.0.2.10' }],
    relations: [{
      kind: 'monitoring_instances',
      count: 1,
      label: '监控实例',
      section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
    }],
    capabilities: ['records_v2_read'],
  }
}

describe('VPSOverviewPageView readonly preview', () => {
  beforeEach(() => {
    vi.mocked(useVPSDetailResources).mockReturnValue({
      subscriptions: { status: 'ready', items: [], error: null },
      services: { status: 'ready', items: [], error: null },
      domains: { status: 'ready', items: [], error: null },
      retry: vi.fn(),
    })
  })

  it('hides write affordances while keeping copy retry and navigation', () => {
    const management: VPSManagementController = {
      panel: null,
      menuOpen: false,
      openMenu: vi.fn(),
      closeMenu: vi.fn(),
      openPanel: vi.fn(),
      closePanel: vi.fn(),
    }
    render(
      <MemoryRouter>
        <VPSOverviewPageView
          overview={overview()}
          management={management}
          onRefresh={vi.fn()}
          retrying={false}
        />
      </MemoryRouter>,
    )

    expect(screen.getByText('只读预览')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '管理' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '新建记录' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制IPv4' })).toBeEnabled()
    expect(screen.getByRole('link', { name: '活动' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看全部' })).toBeInTheDocument()
    const writeAction = screen.getByRole('button', { name: '管理订阅' })
    expect(writeAction).toBeDisabled()
    fireEvent.click(writeAction)
    expect(management.openPanel).not.toHaveBeenCalled()
  })
})
