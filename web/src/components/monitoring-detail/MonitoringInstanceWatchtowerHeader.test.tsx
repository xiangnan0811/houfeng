import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { MonitoringInstanceRecord, VPSSummary } from '../../lib/types'
import { MonitoringInstanceWatchtowerHeader } from './MonitoringInstanceWatchtowerHeader'
import { validateReturnVPSId } from '../../pages/monitoring-detail/monitoringDetailHelpers'
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
  linkedVPS = [],
  linkedVPSLoading = false,
  linkedVPSLoaded = true,
  linkedVPSError = null,
  returnVPSId,
  initialSearch = '',
  navState,
}: {
  linkedVPS?: VPSSummary[]
  linkedVPSLoading?: boolean
  linkedVPSLoaded?: boolean
  linkedVPSError?: string | null
  returnVPSId?: string | null
  initialSearch?: string
  navState?: unknown
} = {}) {
  const props = {
    monitoringInstance: sampleMonitoringInstance(),
    latestSample: null,
    runtimeActions: [],
    runtimeSubmitting: false,
    onRuntimeAction: vi.fn(),
    registerActionRef: vi.fn(),
    onOpenHistory: vi.fn(),
    onOpenCommands: vi.fn(),
    onOpenOnboarding: vi.fn(),
    onboardingActionLabel: '接入 agent…',
    linkedVPS,
    linkedVPSLoading,
    linkedVPSLoaded,
    linkedVPSError,
    ...(returnVPSId !== undefined ? { returnVPSId } : {}),
  }

  return render(
    <MemoryRouter initialEntries={[{ pathname: '/monitoring/mi_001', search: initialSearch, state: navState }]}>
      <Routes>
        <Route path="/monitoring/:id" element={<MonitoringInstanceWatchtowerHeader {...props} />} />
      </Routes>
    </MemoryRouter>,
  )
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

describe('MonitoringInstanceWatchtowerHeader - VPS origin return action', () => {
  it('renders standard unlinked state without return action when returnVPSId is absent', () => {
    renderHeader({ linkedVPS: [] })
    expect(screen.getByRole('link', { name: '未关联' })).toHaveAttribute('href', '/vps?view=unlinked')
    expect(screen.queryByRole('link', { name: '返回来源 VPS' })).not.toBeInTheDocument()
  })

  it('renders "返回来源 VPS" when unlinked and returnVPSId is present, preserving truthful association', () => {
    renderHeader({ linkedVPS: [], returnVPSId: 'vps_tokyo_001', navState: { from: 'workbench' } })
    expect(screen.getByRole('link', { name: '未关联' })).toHaveAttribute('href', '/vps?view=unlinked')
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('renders "返回来源 VPS" when relation is loading', () => {
    renderHeader({ linkedVPSLoading: true, linkedVPSLoaded: false, returnVPSId: 'vps_tokyo_001' })
    expect(screen.getByText(/VPS 关联加载中/)).toBeInTheDocument()
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('renders "返回来源 VPS" when relation fetch fails', () => {
    renderHeader({ linkedVPSError: 'Network error', returnVPSId: 'vps_tokyo_001' })
    expect(screen.getByText(/VPS 关联未同步/)).toBeInTheDocument()
    const returnLink = screen.getByRole('link', { name: '返回来源 VPS' })
    expect(returnLink).toHaveAttribute('href', '/vps/vps_tokyo_001')
  })

  it('renders "返回来源 VPS" when relation is not yet loaded', () => {
    renderHeader({ linkedVPSLoaded: false, linkedVPSLoading: false, returnVPSId: 'vps_tokyo_001' })
    expect(screen.getByText(/VPS 关联待同步/)).toBeInTheDocument()
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

  it('renders "返回来源 VPS" when linked to a different VPS (multi-other) while keeping actual link', () => {
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

  it('renders "返回来源 VPS" when multiple VPSs are linked (multi-other)', () => {
    renderHeader({
      linkedVPS: [
        vpsSummaryFixture({ vps_id: 'vps_001', display_name: 'VPS 1' }),
        vpsSummaryFixture({ vps_id: 'vps_002', display_name: 'VPS 2' }),
      ],
      returnVPSId: 'vps_tokyo_001',
    })
    expect(screen.getByRole('link', { name: '2 台' })).toHaveAttribute('href', '/vps/vps_001')
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
