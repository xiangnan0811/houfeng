import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useRef, useState } from 'react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../lib/api'
import { ApiError } from '../../lib/apiRequest'
import type { CancellationPreview, VPSAssetDetail } from '../../lib/types'
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

function emptyPreview(vpsId: string, warning = ''): CancellationPreview {
  return {
    vps: detailFixture(vpsId, vpsId),
    subscriptions: [],
    monitoring_instance_links: [],
    services: [],
    domains: [],
    target_links: [],
    recommended_steps: [],
    warnings: warning ? [warning] : [],
    blockers: [],
    preview_digest: `digest-${vpsId}`,
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
        lifecycleStatus="active"
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

  it('does not write a stale cancellation preview onto a newly selected VPS', async () => {
    const stalePreview = deferred<CancellationPreview>()
    vi.spyOn(api, 'applyVPSCancellation').mockRejectedValue(new ApiError(409, 'cancellation preview stale'))
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview').mockImplementation(async (id) => {
      if (id === 'vps_a') {
        if (getPreview.mock.calls.filter((call) => call[0] === 'vps_a').length === 1) {
          return emptyPreview('vps_a')
        }
        return stalePreview.promise
      }
      return emptyPreview(id)
    })
    const refresh = vi.fn().mockResolvedValue(true)

    function CancellationHarness() {
      const [vpsId, setVpsId] = useState('vps_a')
      const management = useVPSManagementController()
      const triggerRef = useRef<HTMLButtonElement>(null)
      return (
        <>
          <button type="button" onClick={() => management.openPanel('cancellation')}>打开取消</button>
          <button type="button" onClick={() => setVpsId('vps_b')}>切换 VPS</button>
          <VPSOverviewManagementActions
            vpsId={vpsId}
            displayName={vpsId === 'vps_a' ? '东京边缘' : '大阪边缘'}
            lifecycleStatus="to_cancel"
            management={management}
            managementTriggerRef={triggerRef}
            onOverviewRefresh={refresh}
          />
        </>
      )
    }

    render(
      <MemoryRouter>
        <CancellationHarness />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开取消' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '原因' }), { target: { value: '退役' } })
    fireEvent.click(screen.getByRole('button', { name: '确认取消/退役' }))
    await waitFor(() => {
      expect(getPreview.mock.calls.filter((call) => call[0] === 'vps_a').length).toBeGreaterThanOrEqual(2)
    })
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))
    await act(async () => stalePreview.resolve(emptyPreview('vps_a', '旧 VPS 预览')))

    expect(screen.queryByText('旧 VPS 预览')).not.toBeInTheDocument()
    expect(screen.queryByText('影响范围已变化，请重新加载预览后再确认')).not.toBeInTheDocument()
  })

  it('rebuilds the production cancellation workbench when preview digest changes', async () => {
    vi.spyOn(api, 'applyVPSCancellation').mockRejectedValue(new ApiError(409, 'cancellation preview stale'))
    const getPreview = vi.spyOn(api, 'getVPSCancellationPreview')
      .mockResolvedValueOnce(emptyPreview('vps_a'))
      .mockResolvedValue({ ...emptyPreview('vps_a'), preview_digest: 'digest-vps_a-next' })
    const refresh = vi.fn().mockResolvedValue(true)

    function DigestHarness() {
      const management = useVPSManagementController()
      const triggerRef = useRef<HTMLButtonElement>(null)
      return (
        <>
          <button type="button" onClick={() => management.openPanel('cancellation')}>打开取消</button>
          <VPSOverviewManagementActions
            vpsId="vps_a"
            displayName="东京边缘"
            lifecycleStatus="to_cancel"
            management={management}
            managementTriggerRef={triggerRef}
            onOverviewRefresh={refresh}
          />
        </>
      )
    }

    render(
      <MemoryRouter>
        <DigestHarness />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开取消' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '原因' }), { target: { value: '旧确认' } })
    fireEvent.click(screen.getByRole('button', { name: '确认取消/退役' }))
    await waitFor(() => expect(getPreview).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('textbox', { name: '原因' })).toHaveValue('')
  })

  it('strips rejected archive workbench query without opening the panel', async () => {
    function LocationProbe() {
      const location = useLocation()
      return <div data-testid="location-search">{location.search}</div>
    }

    function ArchiveHarness() {
      const management = useVPSManagementController()
      const triggerRef = useRef<HTMLButtonElement>(null)
      return (
        <>
          <LocationProbe />
          <VPSOverviewManagementActions
            vpsId="vps_a"
            displayName="东京边缘"
            lifecycleStatus="active"
            management={management}
            managementTriggerRef={triggerRef}
            onOverviewRefresh={vi.fn().mockResolvedValue(true)}
          />
        </>
      )
    }

    render(
      <MemoryRouter initialEntries={['/vps/vps_a?workbench=archive']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<ArchiveHarness />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByTestId('location-search')).toHaveTextContent(''))
    expect(screen.queryByRole('dialog', { name: '确认归档 VPS' })).not.toBeInTheDocument()
  })
})
