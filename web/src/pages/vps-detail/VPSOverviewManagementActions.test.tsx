import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useRef, useState } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../lib/api'
import type { VPSAssetDetail } from '../../lib/types'
import { useVPSManagementController } from './hooks/useVPSManagementController'
import { VPSOverviewManagementActions } from './VPSOverviewManagementActions'

function detailFixture(vpsId: string, displayName: string): VPSAssetDetail {
  return {
    vps_id: vpsId,
    display_name: displayName,
    provider_id: null,
    provider_name: 'Example',
    product_name: 'VPS',
    order_ref: '',
    country: 'JP',
    region: 'Tokyo',
    city: 'Tokyo',
    datacenter: 'TK1',
    ipv4: '192.0.2.1',
    ipv6: '',
    ssh_host: '192.0.2.1',
    ssh_port: 22,
    ssh_user: 'root',
    os_name: 'Debian',
    virtualization: 'KVM',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    importance: 'high',
    labels: [],
    note: '',
    active_monitoring_instance_link_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    monitoring_instance_links: [],
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function Harness({ onRefresh }: { onRefresh: () => Promise<boolean> }) {
  const [vpsId, setVpsId] = useState('vps_a')
  const management = useVPSManagementController()
  const triggerRef = useRef<HTMLButtonElement>(null)

  return (
    <>
      <button ref={triggerRef} type="button" onClick={() => management.openPanel('facts')}>打开事实</button>
      <button type="button" onClick={() => setVpsId('vps_b')}>切换 VPS</button>
      <VPSOverviewManagementActions
        vpsId={vpsId}
        displayName={vpsId === 'vps_a' ? '东京边缘' : '大阪边缘'}
        management={management}
        managementTriggerRef={triggerRef}
        onOverviewRefresh={onRefresh}
      />
    </>
  )
}

describe('VPSOverviewManagementActions', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('ignores a completed mutation after the route switches to another VPS', async () => {
    const mutation = deferred<ReturnType<typeof detailFixture>>()
    vi.spyOn(api, 'getVPSAsset').mockImplementation(async (vpsId) => (
      vpsId === 'vps_a'
        ? detailFixture('vps_a', '东京边缘')
        : detailFixture('vps_b', '大阪边缘')
    ))
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    vi.spyOn(api, 'updateVPSAsset').mockReturnValue(mutation.promise)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    const nameInput = await screen.findByRole('textbox', { name: 'VPS 名称' })
    fireEvent.change(nameInput, { target: { value: '东京边缘已更新' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))

    expect(await screen.findByRole('textbox', { name: 'VPS 名称' })).toHaveValue('大阪边缘')
    await act(async () => mutation.resolve(detailFixture('vps_a', '东京边缘已更新')))

    await waitFor(() => expect(api.updateVPSAsset).toHaveBeenCalledTimes(1))
    expect(refresh).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: '编辑 VPS 事实' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'VPS 名称' })).toHaveValue('大阪边缘')
  })

  it('locks duplicate facts submissions until the active mutation settles', async () => {
    const mutation = deferred<ReturnType<typeof detailFixture>>()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a', '东京边缘'))
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    const update = vi.spyOn(api, 'updateVPSAsset').mockReturnValue(mutation.promise)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    const nameInput = await screen.findByRole('textbox', { name: 'VPS 名称' })
    fireEvent.change(nameInput, { target: { value: '东京边缘已更新' } })
    const submit = screen.getByRole('button', { name: '保存基础信息' })
    const form = submit.closest('form')
    expect(form).not.toBeNull()

    fireEvent.submit(form!)
    fireEvent.submit(form!)

    expect(update).toHaveBeenCalledTimes(1)
    await act(async () => mutation.resolve(detailFixture('vps_a', '东京边缘已更新')))
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
  })
})
