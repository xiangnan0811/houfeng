import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../lib/apiRequest'
import * as recordsApi from '../lib/recordsApi'
import type { VPSOverview } from '../lib/types'
import { VPSDetailPage } from './VPSDetailPage'

vi.mock('./vps-detail/LegacyVPSDetail', () => ({
  LegacyVPSDetail: () => <div>Legacy VPS detail shell</div>,
}))

function overviewFixture(): VPSOverview {
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
      labels: [],
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
      items: [],
    },
    facts: [],
    relations: [],
    capabilities: ['records_v2_read'],
  }
}

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/vps/vps_001']}>
      <Routes>
        <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('VPSDetailPage gate', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('uses overview composition when records_v2_read is present', async () => {
    const get = vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())

    renderDetail()

    await waitFor(() => expect(screen.getByRole('heading', { name: '东京边缘' })).toBeInTheDocument())
    expect(screen.getByRole('link', { name: '新建记录' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '时间线' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '管理' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '概览' })).toHaveAttribute('aria-current', 'page')
    expect(screen.queryByText('动作：无')).not.toBeInTheDocument()
    // Gate probe seeds the overview route — no duplicate first-paint fetch.
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('shows overview not-found when identity is missing', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(404, 'vps not found', { code: 'resource_not_found' }),
    )

    renderDetail()

    await waitFor(() => expect(screen.getByText('未找到 VPS')).toBeInTheDocument())
    expect(screen.queryByText('Legacy VPS detail shell')).not.toBeInTheDocument()
  })

  it('falls back to legacy when overview capability is unavailable', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(503, 'overview unavailable', { code: 'overview_unavailable' }),
    )

    renderDetail()

    await waitFor(() => expect(screen.getByText('Legacy VPS detail shell')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: '管理' })).not.toBeInTheDocument()
  })

  it('does not silently fall back on overview server errors', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockRejectedValue(
      new ApiError(500, 'boom', { code: 'internal_error' }),
    )

    renderDetail()

    await waitFor(() => expect(screen.getByText('无法加载 VPS 概览')).toBeInTheDocument())
  })
})
