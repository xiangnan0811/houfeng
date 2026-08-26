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
      <button type="button" onClick={() => management.openPanel('decision')}>打开续费</button>
      <button type="button" onClick={() => management.openPanel('subscription')}>打开订阅</button>
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
    vi.spyOn(api, 'applyVPSCancellation').mockRejectedValue(new ApiError(409, 'cancellation preview stale', {
      code: 'cancellation_preview_stale',
    }))
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
    vi.spyOn(api, 'applyVPSCancellation').mockRejectedValue(new ApiError(409, 'cancellation preview stale', {
      code: 'cancellation_preview_stale',
    }))
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

  it('requires loading the latest VPS version after a CAS conflict before another write', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const latest = { ...detailFixture('vps_a', '东京边缘最新'), updated_at: '2026-08-21T00:00:00Z' }
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(stale)
      .mockResolvedValueOnce(latest)
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    const update = vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps updated', { code: 'vps_asset_conflict' }))
      .mockResolvedValueOnce(latest)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    const nameInput = await screen.findByRole('textbox', { name: 'VPS 名称' })
    fireEvent.change(nameInput, { target: { value: '我的草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))

    expect(await screen.findByRole('status')).toHaveTextContent('请先加载最新版本')
    expect(update).toHaveBeenCalledWith('vps_a', expect.anything(), { expectedUpdatedAt: stale.updated_at })

    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    expect(update).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '加载最新版本' }))
    expect(await screen.findByRole('status')).toHaveTextContent('已加载最新版本')
    expect(screen.getByRole('textbox', { name: 'VPS 名称' })).toHaveValue('我的草稿')

    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2))
    expect(update).toHaveBeenLastCalledWith('vps_a', expect.anything(), { expectedUpdatedAt: latest.updated_at })
  })

  it('clears a facts CAS conflict when switching to the renewal panel', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(stale)
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    const update = vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps updated', { code: 'vps_asset_conflict' }))
      .mockResolvedValueOnce({ ...stale, renewal_decision: 'cancel' })
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'VPS 名称' }), { target: { value: '我的草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    expect(await screen.findByRole('status')).toHaveTextContent('请先加载最新版本')

    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    fireEvent.click(screen.getByRole('button', { name: '打开续费' }))
    const reason = await screen.findByLabelText('决策理由')
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'cancel' } })
    fireEvent.change(reason, { target: { value: '准备取消' } })
    fireEvent.click(screen.getByRole('button', { name: '保存续费决策' }))

    await waitFor(() => expect(update).toHaveBeenCalledTimes(2))
    expect(update).toHaveBeenLastCalledWith('vps_a', expect.objectContaining({
      renewal_decision: 'cancel',
    }), { expectedUpdatedAt: stale.updated_at })
    expect(screen.queryByText('请先加载最新版本后再保存')).not.toBeInTheDocument()
  })

  it('keeps only local fact edits after loading latest and does not overwrite concurrent product or location changes', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const latest = {
      ...stale,
      display_name: '东京边缘最新',
      product_name: 'edge-large',
      region: 'Osaka',
      updated_at: '2026-08-21T00:00:00Z',
    }
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(stale)
      .mockResolvedValueOnce(latest)
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    const update = vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps updated', { code: 'vps_asset_conflict' }))
      .mockResolvedValueOnce(latest)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'VPS 名称' }), { target: { value: '我的草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    expect(await screen.findByRole('status')).toHaveTextContent('请先加载最新版本')

    fireEvent.click(screen.getByRole('button', { name: '加载最新版本' }))
    expect(await screen.findByRole('status')).toHaveTextContent('已加载最新版本')
    expect(screen.getByRole('textbox', { name: 'VPS 名称' })).toHaveValue('我的草稿')
    expect(screen.getByRole('textbox', { name: '产品名' })).toHaveValue('edge-large')
    expect(screen.getByRole('textbox', { name: '区域' })).toHaveValue('Osaka')
    expect(screen.getByRole('status')).toHaveTextContent('名称')
    expect(screen.getByRole('status')).not.toHaveTextContent('产品名')
    expect(screen.getByRole('status')).not.toHaveTextContent('区域')

    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2))
    expect(update).toHaveBeenLastCalledWith('vps_a', expect.objectContaining({
      display_name: '我的草稿',
      product_name: 'edge-large',
      region: 'Osaka',
    }), { expectedUpdatedAt: latest.updated_at })
  })

  it('after loading latest, editing IPv4 keeps a concurrent independent SSH host', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const latest = {
      ...stale,
      ssh_host: 'ssh.example.test',
      ipv6: '2001:db8::1',
      country: 'US',
      updated_at: '2026-08-21T00:00:00Z',
    }
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(stale)
      .mockResolvedValueOnce(latest)
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    const update = vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps updated', { code: 'vps_asset_conflict' }))
      .mockResolvedValueOnce(latest)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'VPS 名称' }), { target: { value: '我的草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    expect(await screen.findByRole('status')).toHaveTextContent('请先加载最新版本')

    fireEvent.click(screen.getByRole('button', { name: '加载最新版本' }))
    expect(await screen.findByRole('status')).toHaveTextContent('已加载最新版本')
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toBeEnabled()
    expect(screen.getByRole('textbox', { name: 'IPv6 地址' })).toHaveValue('2001:db8::1')
    expect(screen.getByRole('combobox', { name: '国家 / 地区' })).toHaveValue('US')

    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4 / 主入口' }), {
      target: { value: '198.51.100.9' },
    })
    expect(screen.getByRole('textbox', { name: 'SSH Host' })).toHaveValue('ssh.example.test')

    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    await waitFor(() => expect(update).toHaveBeenCalledTimes(2))
    expect(update).toHaveBeenLastCalledWith('vps_a', expect.objectContaining({
      ipv4: '198.51.100.9',
      ssh_host: 'ssh.example.test',
      ipv6: '2001:db8::1',
      country: 'US',
    }), { expectedUpdatedAt: latest.updated_at })
  })

  it('keeps edits typed while the latest VPS GET is still in flight', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const latest = { ...stale, display_name: '东京边缘最新', updated_at: '2026-08-21T00:00:00Z' }
    const pending = deferred<VPSAssetDetail>()
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(stale)
      .mockReturnValueOnce(pending.promise)
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps updated', { code: 'vps_asset_conflict' }))
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'VPS 名称' }), { target: { value: '我的草稿' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    expect(await screen.findByRole('status')).toHaveTextContent('请先加载最新版本')

    fireEvent.click(screen.getByRole('button', { name: '加载最新版本' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'VPS 名称' }), { target: { value: '加载中又改了' } })
    await act(async () => pending.resolve(latest))

    expect(await screen.findByRole('textbox', { name: 'VPS 名称' })).toHaveValue('加载中又改了')
  })

  it('rotates the subscription idempotency key only after a reused-key 409', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    const keys: string[] = []
    vi.spyOn(api, 'createVPSSubscription').mockImplementation(async (_vpsId, _input, key) => {
      keys.push(key)
      throw new ApiError(409, 'idempotency key reused', { code: 'idempotency_key_reused' })
    })
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开订阅' }))
    fireEvent.change(await screen.findByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(screen.getByRole('button', { name: '创建/更新订阅' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('同一幂等键已用于不同的订阅内容')
    fireEvent.click(screen.getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(keys).toHaveLength(2))
    expect(keys[0]).not.toBe(keys[1])
  })

  it('keeps the same subscription idempotency key after a transport failure', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    const created = {
      subscription_id: 'sub_001',
      vps_id: 'vps_a',
      price: 12,
      currency: 'USD',
      billing_cycle: 'monthly',
      billing_months: 1,
      monthly_price: 12,
      started_at: null,
      renew_at: null,
      auto_renew: false,
      auto_renew_cancelled: false,
      status: 'active' as const,
      payment_method: '',
      note: '',
      created_at: '2026-08-20T00:00:00Z',
      updated_at: '2026-08-20T00:00:00Z',
    }
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    const keys: string[] = []
    vi.spyOn(api, 'createVPSSubscription')
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        throw new TypeError('Failed to fetch')
      })
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        return created
      })
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开订阅' }))
    fireEvent.change(await screen.findByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(screen.getByRole('button', { name: '创建/更新订阅' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to fetch')
    fireEvent.click(screen.getByRole('button', { name: '创建/更新订阅' }))
    await waitFor(() => expect(refresh).toHaveBeenCalled())
    expect(keys).toHaveLength(2)
    expect(keys[0]).toBe(keys[1])
  })

  it('keeps the continue-cancel action after a cancel renewal write even when overview refresh fails', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'updateVPSAsset').mockResolvedValue({
      ...detail,
      renewal_decision: 'cancel',
      renewal_subscription_linkage: {
        status: 'subscription_updated',
        message: '已关联订阅',
        subscription_id: 'sub_001',
        candidate_count: 1,
        updated: true,
      },
    })
    const refresh = vi.fn().mockResolvedValue(false)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开续费' }))
    fireEvent.change(await screen.findByRole('combobox', { name: '续费决策' }), {
      target: { value: 'cancel' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '决策理由' }), {
      target: { value: '准备取消' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存续费决策' }))

    expect(await screen.findByRole('status')).toHaveTextContent('续费决策已更新，但概览刷新失败')
    expect(screen.getByRole('link', { name: '继续取消 / 退役' })).toHaveAttribute(
      'href',
      '/vps/vps_a?workbench=cancellation',
    )
  })

  it('offers continue-cancel after a cancel renewal write when overview refresh succeeds', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'updateVPSAsset').mockResolvedValue({
      ...detail,
      renewal_decision: 'cancel',
      renewal_subscription_linkage: {
        status: 'subscription_updated',
        message: '已关联订阅',
        subscription_id: 'sub_001',
        candidate_count: 1,
        updated: true,
      },
    })
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开续费' }))
    fireEvent.change(await screen.findByRole('combobox', { name: '续费决策' }), {
      target: { value: 'cancel' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '决策理由' }), {
      target: { value: '准备取消' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存续费决策' }))

    expect(await screen.findByRole('status')).toHaveTextContent('续费决策已更新，概览已刷新')
    expect(screen.getByRole('link', { name: '继续取消 / 退役' })).toHaveAttribute(
      'href',
      '/vps/vps_a?workbench=cancellation',
    )
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
