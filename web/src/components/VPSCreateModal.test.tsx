import { useState } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import type { ProviderRecord, VPSAssetRecord } from '../lib/types'
import { VPSCreateModal } from './VPSCreateModal'

const provider: ProviderRecord = {
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

const created: VPSAssetRecord = {
  vps_id: 'vps_new',
  display_name: 'Osaka Standby',
  provider_id: 'pv_001',
  provider_name: 'Hetzner',
  product_name: '',
  order_ref: '',
  country: 'JP',
  region: '',
  city: '',
  datacenter: '',
  ipv4: '203.0.113.8',
  ipv6: '',
  ssh_host: '203.0.113.8',
  ssh_port: 22,
  ssh_user: 'root',
  os_name: '',
  virtualization: '',
  lifecycle_status: 'active',
  usage_status: 'unknown',
  renewal_decision: 'unreviewed',
  importance: 'normal',
  labels: [],
  note: '',
  active_monitoring_instance_link_count: 0,
  created_at: '2026-05-09T08:00:00Z',
  updated_at: '2026-05-09T08:00:00Z',
}

function renderModal(
  props: Partial<Parameters<typeof VPSCreateModal>[0]> = {},
) {
  const onCreated = props.onCreated ?? vi.fn()
  const onClose = props.onClose ?? vi.fn()
  const onProviderCreated = props.onProviderCreated ?? vi.fn()
  const view = render(
    <MemoryRouter>
      <VPSCreateModal
        open
        onClose={onClose}
        providers={props.providers ?? [provider]}
        providersLoading={props.providersLoading ?? false}
        providersError={props.providersError ?? null}
        onCreated={onCreated}
        onProviderCreated={onProviderCreated}
      />
    </MemoryRouter>,
  )
  return { ...view, onCreated, onClose, onProviderCreated }
}

function openOptionalSettings() {
  const optional = screen.getByText('可选设置').closest('details')
  expect(optional).toBeInstanceOf(HTMLDetailsElement)
  expect((optional as HTMLDetailsElement).open).toBe(false)
  fireEvent.click(within(optional as HTMLElement).getByText('可选设置'))
  expect((optional as HTMLDetailsElement).open).toBe(true)
  return optional as HTMLDetailsElement
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('VPSCreateModal', () => {
  it('submits authored facts including usage enum and constrained importance', async () => {
    const create = vi.spyOn(api, 'createVPSAsset').mockResolvedValue(created)
    renderModal()
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'Osaka Standby' } })
    fireEvent.change(within(modal).getByLabelText('资产服务商'), { target: { value: 'pv_001' } })
    fireEvent.change(within(modal).getByRole('combobox', { name: '国家 / 地区' }), { target: { value: 'JP' } })
    fireEvent.change(within(modal).getByLabelText('城市'), { target: { value: 'Osaka' } })
    fireEvent.change(within(modal).getByLabelText('IPv4'), { target: { value: '203.0.113.8' } })
    fireEvent.change(within(modal).getByRole('combobox', { name: '使用状态' }), { target: { value: 'standby' } })
    fireEvent.change(within(modal).getByRole('combobox', { name: '重要性' }), { target: { value: 'high' } })
    fireEvent.change(within(modal).getByPlaceholderText('prod, edge'), { target: { value: 'edge, prod' } })
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledWith({
      display_name: 'Osaka Standby',
      provider_id: 'pv_001',
      provider_name: 'Hetzner',
      product_name: '',
      order_ref: '',
      country: 'JP',
      region: '',
      city: 'Osaka',
      datacenter: '',
      ipv4: '203.0.113.8',
      ipv6: '',
      ssh_host: '203.0.113.8',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: '',
      virtualization: '',
      lifecycle_status: 'active',
      usage_status: 'standby',
      renewal_decision: 'unreviewed',
      importance: 'high',
      labels: ['edge', 'prod'],
      note: '',
    })
  })

  it('keeps a typed SSH host after hide and submits that host', async () => {
    const create = vi.spyOn(api, 'createVPSAsset').mockResolvedValue(created)
    renderModal()
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'Jump Host' } })
    fireEvent.change(within(modal).getByLabelText('IPv4'), { target: { value: '203.0.113.8' } })
    openOptionalSettings()
    fireEvent.click(within(modal).getByRole('checkbox', { name: '单独填写 SSH' }))
    fireEvent.change(within(modal).getByRole('textbox', { name: 'SSH Host' }), {
      target: { value: 'ssh.example.test' },
    })
    fireEvent.click(within(modal).getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(within(modal).queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
    fireEvent.click(within(modal).getByRole('checkbox', { name: '单独填写 SSH' }))
    expect(within(modal).getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')
    fireEvent.click(within(modal).getByRole('checkbox', { name: '单独填写 SSH' }))
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      ipv4: '203.0.113.8',
      ssh_host: 'ssh.example.test',
      ssh_port: 22,
      ssh_user: 'root',
    }))
  })

  it('keeps a hidden custom SSH host when IPv4 changes and still submits it', async () => {
    const create = vi.spyOn(api, 'createVPSAsset').mockResolvedValue(created)
    renderModal()
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'Jump Host' } })
    fireEvent.change(within(modal).getByLabelText('IPv4'), { target: { value: '203.0.113.8' } })
    openOptionalSettings()
    fireEvent.click(within(modal).getByRole('checkbox', { name: '单独填写 SSH' }))
    fireEvent.change(within(modal).getByRole('textbox', { name: 'SSH Host' }), {
      target: { value: 'ssh.example.test' },
    })
    fireEvent.click(within(modal).getByRole('checkbox', { name: '单独填写 SSH' }))
    fireEvent.change(within(modal).getByLabelText('IPv4'), { target: { value: '198.51.100.9' } })
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      ipv4: '198.51.100.9',
      ssh_host: 'ssh.example.test',
    }))
  })

  it('keeps a typed IPv6 address after hide and submits it', async () => {
    const create = vi.spyOn(api, 'createVPSAsset').mockResolvedValue(created)
    renderModal()
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'Dual Stack' } })
    fireEvent.change(within(modal).getByLabelText('IPv4'), { target: { value: '203.0.113.8' } })
    openOptionalSettings()
    fireEvent.click(within(modal).getByRole('checkbox', { name: '启用 IPv6' }))
    fireEvent.change(within(modal).getByRole('textbox', { name: 'IPv6 地址' }), {
      target: { value: '2001:db8::1' },
    })
    fireEvent.click(within(modal).getByRole('checkbox', { name: '启用 IPv6' }))
    expect(within(modal).queryByRole('textbox', { name: 'IPv6 地址' })).not.toBeInTheDocument()
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      ipv4: '203.0.113.8',
      ipv6: '2001:db8::1',
    }))
  })

  it('publishes a typed country into the create payload without selecting a list option', async () => {
    const create = vi.spyOn(api, 'createVPSAsset').mockResolvedValue(created)
    renderModal()
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'Andorra Edge' } })
    fireEvent.change(within(modal).getByLabelText('IPv4'), { target: { value: '203.0.113.8' } })
    fireEvent.change(within(modal).getByRole('combobox', { name: '国家 / 地区' }), {
      target: { value: 'Andorra' },
    })
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ country: 'AD' }))
  })

  it('keeps server-required create constraints', () => {
    renderModal()
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))
    const footer = modal.querySelector('.modal-footer')
    expect(footer).toBeInstanceOf(HTMLElement)
    expect(within(footer as HTMLElement).getByRole('alert')).toHaveTextContent('VPS 名称不能为空。')
    expect(modal.querySelector('.modal-body')).not.toHaveTextContent('VPS 名称不能为空。')
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'No Address' } })
    fireEvent.click(within(modal).getByRole('button', { name: '创建 VPS' }))
    expect(within(footer as HTMLElement).getByRole('alert')).toHaveTextContent('IPv4 或 SSH Host 至少需要填写一个。')
  })

  it('surfaces provider catalog unavailability without removing unassociated or snapshot fields', () => {
    renderModal({ providers: [], providersError: 'provider catalog down' })
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    expect(within(modal).getByText(/服务商不可用：provider catalog down/)).toBeInTheDocument()
    expect(within(modal).getByRole('combobox', { name: '资产服务商' })).toHaveValue('')
    expect(within(modal).getByRole('button', { name: '新建服务商' })).toBeEnabled()
    fireEvent.click(within(modal).getByText('可选设置'))
    expect(within(modal).getByLabelText('服务商名称快照')).toBeInTheDocument()
  })

  it('disables the provider select while the catalog is loading', () => {
    renderModal({ providersLoading: true, providers: [] })
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    expect(within(modal).getByRole('combobox', { name: '资产服务商' })).toBeDisabled()
    expect(within(modal).getByText('正在读取服务商…')).toBeInTheDocument()
  })

  it('creates a provider inline and selects it', async () => {
    const createdProvider: ProviderRecord = {
      ...provider,
      provider_id: 'pv_new',
      name: 'New Cloud',
    }
    vi.spyOn(api, 'createProvider').mockResolvedValue(createdProvider)
    function Harness() {
      const [providers, setProviders] = useState([provider])
      return (
        <MemoryRouter>
          <VPSCreateModal
            open
            onClose={vi.fn()}
            providers={providers}
            onCreated={vi.fn()}
            onProviderCreated={(next) => setProviders((current) => [...current, next])}
          />
        </MemoryRouter>
      )
    }
    render(<Harness />)
    const modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.click(within(modal).getByRole('button', { name: '新建服务商' }))
    fireEvent.change(within(modal).getByLabelText('服务商名称'), { target: { value: 'New Cloud' } })
    fireEvent.click(within(modal).getByRole('button', { name: '创建' }))
    await waitFor(() => expect(within(modal).getByLabelText('资产服务商')).toHaveValue('pv_new'))
    expect(api.createProvider).toHaveBeenCalledWith(expect.objectContaining({
      name: 'New Cloud',
      website: '',
    }))
  })

  it('resets draft and optional toggles when closed and reopened', () => {
    function Harness() {
      const [open, setOpen] = useState(true)
      return (
        <MemoryRouter>
          <button type="button" onClick={() => setOpen(true)}>打开创建</button>
          <VPSCreateModal
            open={open}
            onClose={() => setOpen(false)}
            providers={[provider]}
            onCreated={vi.fn()}
            onProviderCreated={vi.fn()}
          />
        </MemoryRouter>
      )
    }
    render(<Harness />)
    let modal = screen.getByRole('dialog', { name: '添加 VPS' })
    fireEvent.change(within(modal).getByLabelText('VPS 名称'), { target: { value: 'Draft' } })
    openOptionalSettings()
    fireEvent.click(within(modal).getByRole('checkbox', { name: '单独填写 SSH' }))
    fireEvent.change(within(modal).getByRole('textbox', { name: 'SSH Host' }), {
      target: { value: 'ssh.example.test' },
    })
    fireEvent.click(within(modal).getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '添加 VPS' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '打开创建' }))
    modal = screen.getByRole('dialog', { name: '添加 VPS' })
    expect(within(modal).getByLabelText('VPS 名称')).toHaveValue('')
    expect(within(modal).getByText('可选设置').closest('details')).toHaveProperty('open', false)
    fireEvent.click(within(modal).getByText('可选设置'))
    expect(within(modal).getByRole('checkbox', { name: '单独填写 SSH' })).not.toBeChecked()
    expect(within(modal).queryByRole('textbox', { name: 'SSH Host' })).not.toBeInTheDocument()
  })
})
