import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { VPSPage } from './VPSPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

const provider = {
  provider_id: 'pv_001',
  name: 'Hetzner',
  website: '',
  panel_url: '',
  account_hint: '',
  country: 'DE',
  note: '',
  rating: null,
  labels: [],
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
}

const vps = {
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
  note: '',
  active_node_link_count: 1,
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
  archived_at: null,
}

describe('VPSPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders VPS assets, applies filters, and navigates to detail on row click', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse([vps]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
          <Route path="/vps/:vpsId" element={<div>vps detail route</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Tokyo Edge')).toBeInTheDocument())
    expect(screen.getAllByText('在用').length).toBeGreaterThan(0)
    expect(screen.getAllByText('承载业务').length).toBeGreaterThan(0)
    expect(screen.getAllByText('保留').length).toBeGreaterThan(0)

    fireEvent.change(screen.getByLabelText('生命周期'), { target: { value: 'testing' } })
    await waitFor(() =>
      expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps?lifecycle_status=testing', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    expect(screen.getByText('生命周期: 测试中')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Tokyo Edge'))
    await waitFor(() => expect(screen.getByText('vps detail route')).toBeInTheDocument())
  })

  it('creates a VPS and navigates to the created detail route', async () => {
    const created = { ...vps, vps_id: 'vps_new', display_name: 'Osaka Standby' }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse(created, 201))
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/vps']}>
        <Routes>
          <Route path="/vps" element={<VPSPage />} />
          <Route path="/vps/:vpsId" element={<div>created vps detail</div>} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '创建第一台 VPS' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一台 VPS' }))
    fireEvent.change(screen.getByLabelText('VPS 名称'), { target: { value: 'Osaka Standby' } })
    fireEvent.change(screen.getByLabelText('资产服务商'), { target: { value: 'pv_001' } })
    fireEvent.change(screen.getByLabelText('国家'), { target: { value: 'JP' } })
    fireEvent.change(screen.getByLabelText('区域'), { target: { value: 'Kansai' } })
    fireEvent.change(screen.getByLabelText('城市'), { target: { value: 'Osaka' } })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'standby, standby' } })
    fireEvent.click(screen.getByRole('button', { name: '创建 VPS' }))

    await waitFor(() => expect(screen.getByText('created vps detail')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        display_name: 'Osaka Standby',
        provider_id: 'pv_001',
        provider_name: 'Hetzner',
        product_name: '',
        order_ref: '',
        country: 'JP',
        region: 'Kansai',
        city: 'Osaka',
        datacenter: '',
        ipv4: '',
        ipv6: '',
        ssh_host: '',
        ssh_port: 22,
        ssh_user: 'root',
        os_name: '',
        virtualization: '',
        lifecycle_status: 'active',
        usage_status: 'unknown',
        renewal_decision: 'unreviewed',
        importance: 'normal',
        labels: ['standby'],
        note: '',
      }),
    })
  })
})
