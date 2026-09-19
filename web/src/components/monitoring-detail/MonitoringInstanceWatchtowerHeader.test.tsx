import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { MonitoringInstanceRecord, VPSSummary } from '../../lib/types'
import { MonitoringInstanceWatchtowerHeader } from './MonitoringInstanceWatchtowerHeader'
import { returnVPSIdFromNavigationState, validateReturnVPSId, withReturnVPSNavigationState, withReturnVPSQuery } from '../../pages/monitoring-detail/monitoringDetailHelpers'

function sampleMonitoringInstance(overrides: Partial<MonitoringInstanceRecord> = {}): MonitoringInstanceRecord {
  return {
    monitoring_instance_id: 'mi_001',
    display_name: 'Tokyo Edge',
    group: '',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: ['core'],
    note: '',
    current_health_status: '正常',
    last_heartbeat_at: '2026-04-24T09:00:00Z',
    last_sync_at: '2026-04-24T09:05:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-24T09:05:00Z',
    ...overrides,
  }
}

function vpsSummaryFixture(overrides: Partial<VPSSummary> = {}): VPSSummary {
  return {
    vps_id: 'vps_001',
    display_name: 'Tokyo Edge VPS',
    provider_id: 'pv_001',
    provider_name: 'Hetzner',
    country: 'JP',
    region: 'Kanto',
    city: 'Tokyo',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'normal',
    labels: [],
    linked_at: '2026-04-24T09:06:00Z',
    note: '',
    ...overrides,
  }
}

function renderHeader({
  monitoringInstance = sampleMonitoringInstance(),
  readOnly = false,
  linkedVPS = [],
  linkedVPSLoading = false,
  linkedVPSLoaded = true,
  linkedVPSError = null,
  onRetryLinkedVPS = vi.fn(),
  returnVPSId,
  initialSearch = '',
  navState,
  actions,
}: {
  monitoringInstance?: MonitoringInstanceRecord
  readOnly?: boolean
  linkedVPS?: VPSSummary[]
  linkedVPSLoading?: boolean
  linkedVPSLoaded?: boolean
  linkedVPSError?: string | null
  onRetryLinkedVPS?: () => void
  returnVPSId?: string | null
  initialSearch?: string
  navState?: unknown
  actions?: React.ReactNode
} = {}) {
  const props = {
    monitoringInstance,
    readOnly,
    linkedVPS,
    linkedVPSLoading,
    linkedVPSLoaded,
    linkedVPSError,
    onRetryLinkedVPS,
    ...(returnVPSId !== undefined ? { returnVPSId } : {}),
    ...(actions !== undefined ? { actions } : {}),
  }

  return render(
    <MemoryRouter initialEntries={[{ pathname: '/monitoring/mi_001', search: initialSearch, state: navState }]}>
      <Routes>
        <Route path="/monitoring/:id" element={<MonitoringInstanceWatchtowerHeader {...props} />} />
      </Routes>
    </MemoryRouter>,
  )
}

function identityLabels(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('.monitoring-detail-identity dt')).map(
    (dt) => dt.textContent ?? '',
  )
}

function identityValue(label: string): string {
  const dt = screen.getByText(label, { selector: 'dt' })
  return dt.parentElement?.querySelector('dd')?.textContent ?? ''
}

describe('validateReturnVPSId', () => {
  it('accepts safe identifiers and trims whitespace', () => {
    expect(validateReturnVPSId('vps_001')).toBe('vps_001')
    expect(validateReturnVPSId('  vps_tokyo_001  ')).toBe('vps_tokyo_001')
    expect(validateReturnVPSId('vps-001')).toBe('vps-001')
    expect(validateReturnVPSId('vps.001')).toBe('vps.001')
    expect(validateReturnVPSId('vps_abc_123')).toBe('vps_abc_123')
  })

  it('rejects empty, null, undefined, and non-string inputs', () => {
    expect(validateReturnVPSId(null)).toBeNull()
    expect(validateReturnVPSId(undefined)).toBeNull()
    expect(validateReturnVPSId('')).toBeNull()
    expect(validateReturnVPSId('   ')).toBeNull()
    expect(validateReturnVPSId(123 as unknown as string)).toBeNull()
  })

  it('rejects unsafe values and open redirect vectors', () => {
    expect(validateReturnVPSId('//evil.com')).toBeNull()
    expect(validateReturnVPSId('http://evil.com')).toBeNull()
    expect(validateReturnVPSId('https://evil.com')).toBeNull()
    expect(validateReturnVPSId('javascript:alert(1)')).toBeNull()
    expect(validateReturnVPSId('../vps_001')).toBeNull()
    expect(validateReturnVPSId('vps_001/child')).toBeNull()
    expect(validateReturnVPSId('vps_001\\escape')).toBeNull()
    expect(validateReturnVPSId('vps_001?foo=bar')).toBeNull()
    expect(validateReturnVPSId('vps_001#hash')).toBeNull()
  })
})

describe('withReturnVPSQuery', () => {
  it('appends only a validated return_vps and leaves other query keys untouched', () => {
    expect(withReturnVPSQuery('/monitoring/mi_001/activity', 'vps_tokyo_origin')).toBe(
      '/monitoring/mi_001/activity?return_vps=vps_tokyo_origin',
    )
    expect(withReturnVPSQuery('/monitoring/mi_001?window=7d', 'vps_001')).toBe(
      '/monitoring/mi_001?window=7d&return_vps=vps_001',
    )
    expect(withReturnVPSQuery('/monitoring/mi_001?return_vps=old', 'vps_new')).toBe(
      '/monitoring/mi_001?return_vps=vps_new',
    )
  })

  it('omits invalid values instead of copying them onto the path', () => {
    expect(withReturnVPSQuery('/monitoring/mi_001/activity', 'javascript:alert(1)')).toBe(
      '/monitoring/mi_001/activity',
    )
    expect(withReturnVPSQuery('/monitoring/mi_001', null)).toBe('/monitoring/mi_001')
  })
})

describe('return VPS navigation state', () => {
  it('stores only a validated return_vps and drops leftovers when absent', () => {
    expect(withReturnVPSNavigationState(
      { monitoringListHref: '/monitoring?selected=mi_001', return_vps: 'vps_SHOULD_NOT_USE' },
      'vps_tokyo_origin',
    )).toEqual({
      monitoringListHref: '/monitoring?selected=mi_001',
      return_vps: 'vps_tokyo_origin',
    })
    expect(withReturnVPSNavigationState(
      { monitoringListHref: '/monitoring?selected=mi_001', return_vps: 'vps_SHOULD_NOT_USE' },
      null,
    )).toEqual({
      monitoringListHref: '/monitoring?selected=mi_001',
    })
  })

  it('refuses invalid provenance from history state', () => {
    expect(returnVPSIdFromNavigationState({ return_vps: 'javascript:alert(1)' })).toBeNull()
    expect(returnVPSIdFromNavigationState({ return_vps: 'vps_tokyo_origin' })).toBe('vps_tokyo_origin')
    expect(returnVPSIdFromNavigationState({ monitoringListHref: '/monitoring' })).toBeNull()
  })
})

describe('MonitoringInstanceWatchtowerHeader identity', () => {
  it('leads with the object name and keeps health out of the header', () => {
    renderHeader()
    expect(screen.getByRole('heading', { level: 1, name: 'Tokyo Edge' })).toBeInTheDocument()
    expect(screen.queryByLabelText(/^健康:/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/^运行:/)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/^绑定:/)).not.toBeInTheDocument()
    expect(screen.queryByText(/心跳/)).not.toBeInTheDocument()
    expect(screen.queryByText(/^采样/)).not.toBeInTheDocument()
  })

  it('renders the identity items in the locked order', () => {
    const { container } = renderHeader({
      monitoringInstance: sampleMonitoringInstance({
        group: 'edge',
        labels: ['core', 'jp', 'prod', 'extra'],
        note: 'primary gateway',
      }),
    })

    expect(identityLabels(container)).toEqual(['关联', '服务商', '位置', '分组', '标签', '备注', 'ID'])
    expect(identityValue('服务商')).toBe('Vultr')
    expect(identityValue('位置')).toBe('Tokyo')
    expect(identityValue('分组')).toBe('edge')
    expect(identityValue('标签')).toBe('core · jp · prod 等 4 个')
    expect(identityValue('备注')).toBe('primary gateway')
    expect(identityValue('ID')).toBe('mi_001')
    // The full label list stays available on hover.
    expect(screen.getByTitle('core · jp · prod · extra')).toBeInTheDocument()
  })

  it('omits the cloud region code but keeps the ASCII city', () => {
    renderHeader({ monitoringInstance: sampleMonitoringInstance({ region: 'ap-northeast-1', city: 'Tokyo' }) })
    expect(identityValue('位置')).toBe('Tokyo')
    expect(screen.queryByText(/ap-northeast-1/)).not.toBeInTheDocument()
  })

  it('drops region codes in every documented shape while keeping real region names', () => {
    const cases: Array<[string, string]> = [
      ['ap-northeast-1', 'Tokyo'],
      ['us-east-1', 'Virginia'],
      ['cn-hangzhou', 'Hangzhou'],
      ['us-central1', 'Iowa'],
      ['sgp1', 'Singapore'],
      ['nyc3', 'New York'],
      ['fsn1', 'Falkenstein'],
      ['JP', 'Tokyo'],
      ['nrt', 'Tokyo'],
      ['Kanto', 'Tokyo'],
    ]
    for (const [region, city] of cases) {
      const { unmount } = renderHeader({ monitoringInstance: sampleMonitoringInstance({ region, city }) })
      const expected = region === 'Kanto' ? 'Kanto · Tokyo' : city
      expect(identityValue('位置')).toBe(expected)
      unmount()
    }
  })

  it('never writes an unconfirmed location sentence', () => {
    const { unmount } = renderHeader({ monitoringInstance: sampleMonitoringInstance({ region: '', city: '' }) })
    expect(screen.queryByText('位置')).not.toBeInTheDocument()
    expect(screen.queryByText(/位置未确认/)).not.toBeInTheDocument()
    unmount()

    renderHeader({ monitoringInstance: sampleMonitoringInstance({ region: '未确认', city: '未确认' }) })
    expect(screen.queryByText('位置')).not.toBeInTheDocument()
  })

  it('hides empty, unconfirmed and "Provider …" providers', () => {
    for (const provider of ['', '未确认', 'Provider 未确认']) {
      const { unmount } = renderHeader({ monitoringInstance: sampleMonitoringInstance({ provider }) })
      expect(screen.queryByText('服务商')).not.toBeInTheDocument()
      expect(screen.queryByText(/Provider 未确认/)).not.toBeInTheDocument()
      unmount()
    }
    renderHeader({ monitoringInstance: sampleMonitoringInstance({ provider: 'Example Cloud' }) })
    expect(identityValue('服务商')).toBe('Example Cloud')
  })

  it('omits optional identity rows that have no value', () => {
    const { container } = renderHeader({
      monitoringInstance: sampleMonitoringInstance({ group: '', labels: [], note: '' }),
    })
    expect(identityLabels(container)).toEqual(['关联', '服务商', '位置', 'ID'])
  })

  it('shows a read-only preview badge next to the title', () => {
    const { container, unmount } = renderHeader({ readOnly: true })
    const badge = container.querySelector('.monitoring-detail-readonly')
    expect(badge).toHaveTextContent('只读预览')
    expect(badge?.parentElement?.querySelector('h1')).toHaveTextContent('Tokyo Edge')
    unmount()

    const plain = renderHeader()
    expect(plain.container.querySelector('.monitoring-detail-readonly')).toBeNull()
  })

  it('renders the page actions it is given', () => {
    renderHeader({ actions: <button type="button">刷新</button> })
    expect(screen.getByRole('button', { name: '刷新' })).toBeInTheDocument()
  })
})

describe('MonitoringInstanceWatchtowerHeader linked VPS identity', () => {
  it('links an unlinked instance to the unlinked VPS view', () => {
    renderHeader({ linkedVPS: [] })
    expect(screen.getByRole('link', { name: '未关联' })).toHaveAttribute('href', '/vps?view=unlinked')
    expect(screen.queryByRole('link', { name: '返回来源 VPS' })).not.toBeInTheDocument()
  })

  it('shows the display name without the vps id for a single link', () => {
    renderHeader({ linkedVPS: [vpsSummaryFixture()] })
    const link = screen.getByRole('link', { name: 'Tokyo Edge VPS' })
    expect(link).toHaveAttribute('href', '/vps/vps_001')
    expect(identityValue('关联')).not.toContain('vps_001')
  })

  it('lists both names for two links', () => {
    renderHeader({
      linkedVPS: [
        vpsSummaryFixture({ vps_id: 'vps_001', display_name: 'VPS 1' }),
        vpsSummaryFixture({ vps_id: 'vps_002', display_name: 'VPS 2' }),
      ],
    })
    expect(screen.getByRole('link', { name: 'VPS 1' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.getByRole('link', { name: 'VPS 2' })).toHaveAttribute('href', '/vps/vps_002')
  })

  it('lists the first two names plus the total for three or more links', () => {
    renderHeader({
      linkedVPS: [
        vpsSummaryFixture({ vps_id: 'vps_001', display_name: 'VPS 1' }),
        vpsSummaryFixture({ vps_id: 'vps_002', display_name: 'VPS 2' }),
        vpsSummaryFixture({ vps_id: 'vps_003', display_name: 'VPS 3' }),
      ],
    })
    expect(screen.getByRole('link', { name: 'VPS 1' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.getByRole('link', { name: 'VPS 2' })).toHaveAttribute('href', '/vps/vps_002')
    expect(screen.queryByRole('link', { name: 'VPS 3' })).not.toBeInTheDocument()
    expect(identityValue('关联')).toContain('等 3 台')
  })

  it('reads as loading while the relation request is in flight', () => {
    renderHeader({ linkedVPSLoading: true, linkedVPSLoaded: false })
    expect(identityValue('关联')).toBe('加载中')
  })

  it('reads as unsynced with a retry when the relation request fails', () => {
    const onRetryLinkedVPS = vi.fn()
    renderHeader({ linkedVPSError: 'Network error', onRetryLinkedVPS })
    expect(identityValue('关联')).toBe('未同步重试')
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(onRetryLinkedVPS).toHaveBeenCalledTimes(1)
  })

  it('reads as loading before the relation is marked loaded', () => {
    renderHeader({ linkedVPSLoaded: false, linkedVPSLoading: false })
    expect(identityValue('关联')).toBe('加载中')
  })
})

describe('MonitoringInstanceWatchtowerHeader - VPS origin return action', () => {
  it('renders "返回来源 VPS" when unlinked and returnVPSId is present, preserving truthful association', () => {
    renderHeader({ linkedVPS: [], returnVPSId: 'vps_tokyo_001', navState: { from: 'workbench' } })
    expect(screen.getByRole('link', { name: '未关联' })).toHaveAttribute('href', '/vps?view=unlinked')
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('renders "返回来源 VPS" when relation is loading', () => {
    renderHeader({ linkedVPSLoading: true, linkedVPSLoaded: false, returnVPSId: 'vps_tokyo_001' })
    expect(identityValue('关联')).toContain('加载中')
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('renders "返回来源 VPS" when relation fetch fails', () => {
    renderHeader({ linkedVPSError: 'Network error', returnVPSId: 'vps_tokyo_001' })
    expect(identityValue('关联')).toContain('未同步')
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('renders "返回来源 VPS" when relation is not yet loaded', () => {
    renderHeader({ linkedVPSLoaded: false, linkedVPSLoading: false, returnVPSId: 'vps_tokyo_001' })
    expect(identityValue('关联')).toContain('加载中')
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('does NOT render redundant "返回来源 VPS" when the single linked VPS is the origin VPS', () => {
    renderHeader({
      linkedVPS: [vpsSummaryFixture({
        vps_id: 'vps_tokyo_001',
        display_name: 'Tokyo Edge VPS',
      })],
      returnVPSId: 'vps_tokyo_001',
    })
    expect(screen.getByRole('link', { name: 'Tokyo Edge VPS' })).toHaveAttribute('href', '/vps/vps_tokyo_001')
    expect(screen.queryByRole('link', { name: '返回来源 VPS' })).not.toBeInTheDocument()
  })

  it('renders "返回来源 VPS" when linked to a different VPS while keeping actual link', () => {
    renderHeader({
      linkedVPS: [vpsSummaryFixture({
        vps_id: 'vps_other_002',
        display_name: 'Other Edge VPS',
      })],
      returnVPSId: 'vps_tokyo_001',
    })
    expect(screen.getByRole('link', { name: 'Other Edge VPS' })).toHaveAttribute('href', '/vps/vps_other_002')
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('renders "返回来源 VPS" when multiple VPSs are linked', () => {
    renderHeader({
      linkedVPS: [
        vpsSummaryFixture({ vps_id: 'vps_001', display_name: 'VPS 1' }),
        vpsSummaryFixture({ vps_id: 'vps_002', display_name: 'VPS 2' }),
      ],
      returnVPSId: 'vps_tokyo_001',
    })
    expect(identityValue('关联')).toContain('等 2 台')
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('extracts validated return_vps from search params when returnVPSId prop is omitted', () => {
    renderHeader({
      linkedVPS: [],
      initialSearch: '?return_vps=vps_from_search',
    })
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_from_search')
  })
})
