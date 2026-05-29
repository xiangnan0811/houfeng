import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    expect(screen.queryByRole('dialog', { name: '新建服务商表单' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /新建服务商/ }))
    const createDialog = screen.getByRole('dialog', { name: '新建服务商表单' })
    expect(createDialog).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('服务商名称'), { target: { value: 'Vultr' } })
    fireEvent.change(within(createDialog).getByLabelText('网站'), { target: { value: 'https://vultr.com' } })
    fireEvent.change(within(createDialog).getByLabelText('面板地址'), { target: { value: 'https://my.vultr.com' } })
    fireEvent.change(within(createDialog).getByLabelText('账号提示'), { target: { value: 'backup' } })
    fireEvent.change(within(createDialog).getByLabelText('国家 / 地区'), { target: { value: 'US' } })
    fireEvent.change(within(createDialog).getByLabelText('评分 (1-5)'), { target: { value: '4' } })
    fireEvent.change(within(createDialog).getByLabelText('标签'), { target: { value: 'edge, edge' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建' }))

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

  it('shows provider empty state and resets create draft/errors after drawer cancel', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<ProvidersPage />)

    await waitFor(() => expect(screen.getByText('尚未记录服务商')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务商' }))
    const createDialog = screen.getByRole('dialog', { name: '新建服务商表单' })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建' }))
    expect(screen.getByText('服务商名称不能为空。')).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('服务商名称'), { target: { value: 'Draft Provider' } })
    fireEvent.click(within(createDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '新建服务商表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务商' }))

    const reopened = screen.getByRole('dialog', { name: '新建服务商表单' })
    expect(within(reopened).queryByText('服务商名称不能为空。')).not.toBeInTheDocument()
    expect(within(reopened).getByLabelText('服务商名称')).toHaveValue('')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps invalid provider input local', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<ProvidersPage />)

    await waitFor(() => expect(screen.getByText('尚未记录服务商')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '创建第一个服务商' }))
    const createDialog = screen.getByRole('dialog', { name: '新建服务商表单' })
    fireEvent.click(within(createDialog).getByRole('button', { name: '创建' }))

    expect(screen.getByText('服务商名称不能为空。')).toBeInTheDocument()
    fireEvent.change(within(createDialog).getByLabelText('服务商名称'), { target: { value: 'Invalid Rating' } })
    fireEvent.change(within(createDialog).getByLabelText('评分 (1-5)'), { target: { value: '1.5' } })
    fireEvent.submit(within(createDialog).getByRole('button', { name: '创建' }).closest('form')!)

    expect(within(createDialog).getByText('评分必须为 1 到 5。')).toBeInTheDocument()
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
    expect(screen.queryByRole('dialog', { name: '编辑服务商表单' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    const editDialog = screen.getByRole('dialog', { name: '编辑服务商表单' })
    expect(editDialog).toBeInTheDocument()
    fireEvent.change(within(editDialog).getByLabelText('服务商名称'), { target: { value: 'Hetzner Cloud' } })
    fireEvent.change(within(editDialog).getByLabelText('评分 (1-5)'), { target: { value: '' } })
    fireEvent.change(within(editDialog).getByLabelText('标签'), { target: { value: 'core, backup, core' } })
    fireEvent.click(within(editDialog).getByRole('button', { name: '保存' }))

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

  it('shows provider error state with retry action', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse({ error: 'database unavailable' }, 500))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<ProvidersPage />)

    await waitFor(() => expect(screen.getByText('加载失败')).toBeInTheDocument())
    expect(screen.getByText('database unavailable')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.getByText('尚未记录服务商')).toBeInTheDocument())
  })

  it('resets provider edit draft and errors after drawer cancel', async () => {
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
    const fetchMock = vi.fn().mockResolvedValueOnce(mockJSONResponse([provider]))
    vi.stubGlobal('fetch', fetchMock)

    render(<ProvidersPage />)

    await waitFor(() => expect(screen.getByText('Hetzner')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    const firstEditDialog = screen.getByRole('dialog', { name: '编辑服务商表单' })
    fireEvent.change(within(firstEditDialog).getByLabelText('服务商名称'), { target: { value: '' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(within(firstEditDialog).getByText('服务商名称不能为空。')).toBeInTheDocument())
    fireEvent.change(within(firstEditDialog).getByLabelText('服务商名称'), { target: { value: 'Draft Hetzner' } })
    fireEvent.click(within(firstEditDialog).getByRole('button', { name: '取消' }))

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '编辑服务商表单' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))

    const editDialog = screen.getByRole('dialog', { name: '编辑服务商表单' })
    expect(within(editDialog).queryByText('服务商名称不能为空。')).not.toBeInTheDocument()
    expect(within(editDialog).getByLabelText('服务商名称')).toHaveValue('Hetzner')
    expect(within(editDialog).getByLabelText('评分 (1-5)')).toHaveValue(5)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})