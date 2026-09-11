import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import * as recordsApi from '../lib/recordsApi'
import type { VPSOverview } from '../lib/types'
import { VPSDetailPage } from './VPSDetailPage'

vi.mock('../lib/readOnlyPreview', () => ({
  READ_ONLY_PREVIEW: true,
}))

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
      ipv4: '192.0.2.10',
      ipv6: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'high',
      labels: [],
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
      items: [],
    },
    facts: [{ key: 'ipv4', label: 'IPv4', value: '192.0.2.10' }],
    relations: [],
    capabilities: ['records_v2_read'],
  }
}

describe('VPSDetailPage readonly preview workbench', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue([])
    vi.spyOn(api, 'listVPSServices').mockResolvedValue([])
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([])
    vi.spyOn(api, 'getVPSAsset')
  })

  it.each(['subscription', 'monitoring', 'monitoring-instance-create', 'cancellation'] as const)(
    'consumes workbench=%s without opening an enabled write panel',
    async (workbench) => {
      render(
        <MemoryRouter initialEntries={[`/vps/vps_001?workbench=${workbench}`]}>
          <Routes>
            <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
          </Routes>
        </MemoryRouter>,
      )

      expect(await screen.findByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
      expect(screen.getByText('只读预览')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '管理' })).not.toBeInTheDocument()
      expect(screen.queryByRole('link', { name: '新建记录' })).not.toBeInTheDocument()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
      expect(screen.queryByRole('textbox', { name: '原因' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: '复制IPv4' })).toBeEnabled()
      await waitFor(() => expect(api.getVPSAsset).not.toHaveBeenCalled())
    },
  )
})
