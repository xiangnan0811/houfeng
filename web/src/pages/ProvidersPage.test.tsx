import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ProvidersPage } from './ProvidersPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('ProvidersPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders providers and creates a new provider through the API helper path', async () => {
    const provider = {
      provider_id: 'pv_001',
      name: 'Hetzner',
      website: 'https://hetzner.com',
      panel_url: 'https://console.hetzner.cloud',
      account_hint: 'main',
      country: 'DE',
      note: '',
      rating: 5,
      labels: ['core'],
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    }
    const created = { ...provider, provider_id: 'pv_002', name: 'Vultr', rating: 4 }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse(created, 201))
    vi.stubGlobal('fetch', fetchMock)

    render(<ProvidersPage />)

    expect(screen.getByText('正在加载服务商…')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '新建服务商' }))
    fireEvent.change(screen.getByLabelText('服务商名称'), { target: { value: 'Vultr' } })
    fireEvent.change(screen.getByLabelText('网站'), { target: { value: 'https://vultr.com' } })
    fireEvent.change(screen.getByLabelText('面板地址'), { target: { value: 'https://my.vultr.com' } })
    fireEvent.change(screen.getByLabelText('账号提示'), { target: { value: 'backup' } })
    fireEvent.change(screen.getByLabelText('国家 / 地区'), { target: { value: 'US' } })
    fireEvent.change(screen.getByLabelText('评分'), { target: { value: '4' } })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'edge, edge' } })
    fireEvent.click(screen.getByRole('button', { name: '创建服务商' }))

    await waitFor(() => expect(screen.getByText('Vultr')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/providers', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        name: 'Vultr',
        website: 'https://vultr.com',
        panel_url: 'https://my.vultr.com',
        account_hint: 'backup',
        country: 'US',
        rating: 4,
        labels: ['edge'],
        note: '',
      }),
    })
  })

  it('keeps invalid provider input local', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<ProvidersPage />)

    await waitFor(() => expect(screen.getByRole('button', { name: '创建第一个服务商' })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务商' }))
    fireEvent.click(screen.getByRole('button', { name: '创建服务商' }))

    expect(screen.getByText('服务商名称不能为空。')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('updates an existing provider through PATCH and refreshes the row', async () => {
    const provider = {
      provider_id: 'pv_001',
      name: 'Hetzner',
      website: 'https://hetzner.com',
      panel_url: 'https://console.hetzner.cloud',
      account_hint: 'main',
      country: 'DE',
      note: '',
      rating: 5,
      labels: ['core'],
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    }
    const updated = {
      ...provider,
      name: 'Hetzner Cloud',
      rating: null,
      labels: ['core', 'backup'],
      updated_at: '2026-05-09T09:00:00Z',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([provider]))
      .mockResolvedValueOnce(mockJSONResponse(updated))
    vi.stubGlobal('fetch', fetchMock)

    render(<ProvidersPage />)

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑 Hetzner' }))
    fireEvent.change(screen.getByLabelText('服务商名称'), { target: { value: 'Hetzner Cloud' } })
    fireEvent.change(screen.getByLabelText('评分'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'core, backup, core' } })
    fireEvent.click(screen.getByRole('button', { name: '保存服务商' }))

    await waitFor(() => expect(screen.getByText('Hetzner Cloud')).toBeInTheDocument())
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/providers/pv_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({
        name: 'Hetzner Cloud',
        website: 'https://hetzner.com',
        panel_url: 'https://console.hetzner.cloud',
        account_hint: 'main',
        country: 'DE',
        rating: null,
        labels: ['core', 'backup'],
        note: '',
      }),
    })
  })
})
