import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useRef, useState } from 'react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../lib/api'
import { ApiError } from '../../lib/apiRequest'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  CancellationPreview,
  LifecycleActionResult,
  MonitoringInstanceRecord,
  SubscriptionRecord,
  TargetRecord,
  VPSAssetDetail,
  VPSMonitoringInstanceLinkRecord,
  VPSMonitoringInstanceSummary,
} from '../../lib/types'
import { useVPSManagementController } from './hooks/useVPSManagementController'
import { VPSOverviewManagementActions } from './VPSOverviewManagementActions'
import { createVPSWriteOwnerStore, type VPSWriteOwnerStore } from './vpsWriteOwnerStore'

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
    dependency_impacts: [],
    evaluated_on: '2026-09-24',
  }
}

function targetRecord(overrides: Partial<TargetRecord> = {}): TargetRecord {
  return {
    target_id: 'tgt_1',
    name: 'Web Target',
    target_type: 'service',
    host: '192.0.2.1',
    base_port: 80,
    execution_monitoring_instance_labels: [],
    run_status: '启用',
    group: '',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    ...overrides,
  }
}

function serviceRecord(overrides: Partial<AssetServiceRecord> = {}): AssetServiceRecord {
  return {
    service_id: 'svc_1',
    vps_id: 'vps_a',
    name: 'Blog Service',
    service_type: 'web',
    status: 'active',
    url: '',
    labels: [],
    note: '',
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    ...overrides,
  }
}

function subscriptionRecord(overrides: Partial<SubscriptionRecord> = {}): SubscriptionRecord {
  return {
    subscription_id: 'sub_1',
    vps_id: 'vps_a',
    price: 10,
    currency: 'USD',
    billing_cycle: 'monthly',
    billing_months: 1,
    monthly_price: 10,
    auto_renew: true,
    auto_renew_cancelled: false,
    status: 'active',
    payment_method: '',
    note: '',
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    ...overrides,
  }
}

function monitoringRecord(overrides: Partial<MonitoringInstanceRecord> = {}): MonitoringInstanceRecord {
  return {
    monitoring_instance_id: 'mon_1',
    display_name: 'Tokyo Mon 1',
    group: '',
    region: 'Tokyo',
    city: 'Tokyo',
    provider: 'AWS',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    ...overrides,
  }
}

function linkedMonitoring(overrides: Partial<VPSMonitoringInstanceSummary> = {}): VPSMonitoringInstanceSummary {
  return {
    monitoring_instance_id: 'mon_linked',
    display_name: 'Tokyo Mon Linked',
    group: '',
    region: 'Tokyo',
    city: 'Tokyo',
    provider: 'AWS',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    current_health_status: '正常',
    last_heartbeat_at: '2026-08-20T00:00:00Z',
    last_sync_at: '2026-08-20T00:00:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    linked_at: '2026-08-10T00:00:00Z',
    note: 'primary monitoring',
    ...overrides,
  }
}

function validityResult(): LifecycleActionResult {
  return {
    action: {
      action_id: 'act_1',
      vps_id: 'vps_a',
      action_type: 'extend_validity',
      status: 'completed',
      reason: '机房故障补偿 7 天',
      created_at: '2026-08-20T00:00:00Z',
    },
    steps: [{
      step_id: 'step_1',
      action_id: 'act_1',
      object_type: 'subscription',
      object_id: 'sub_active',
      step_type: 'subscription_renew_at',
      status: 'completed',
      before_state: {},
      after_state: {},
      message: 'subscription_updated',
      created_at: '2026-08-20T00:00:00Z',
    }],
  }
}

function domainRecord(overrides: Partial<AssetDomainRecord> = {}): AssetDomainRecord {
  return {
    domain_id: 'dom_1',
    vps_id: 'vps_a',
    domain_name: 'app.example.com',
    purpose: '',
    status: 'active',
    registrar: '',
    auto_renew: false,
    https_enabled: false,
    labels: [],
    note: '',
    created_at: '2026-08-20T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    ...overrides,
  }
}

function linkRecord(overrides: Partial<VPSMonitoringInstanceLinkRecord> = {}): VPSMonitoringInstanceLinkRecord {
  return {
    link_id: 'link_1',
    vps_id: 'vps_a',
    monitoring_instance_id: 'mon_1',
    linked_at: '2026-08-20T00:00:00Z',
    note: '',
    ...overrides,
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

function Harness({
  onRefresh,
  writeOwnerStore,
  viewToken,
}: {
  onRefresh: () => Promise<boolean>
  writeOwnerStore?: VPSWriteOwnerStore
  viewToken?: string
}) {
  const [vpsId, setVpsId] = useState('vps_a')
  const management = useVPSManagementController()
  const triggerRef = useRef<HTMLButtonElement>(null)

  return (
    <>
      <button ref={triggerRef} type="button" onClick={() => management.openPanel('facts')}>打开事实</button>
      <button type="button" onClick={() => management.openPanel('decision')}>打开续费</button>
      <button type="button" onClick={() => management.openPanel('subscription')}>打开订阅</button>
      <button type="button" onClick={() => management.openPanel('service')}>打开服务</button>
      <button type="button" onClick={() => management.openPanel('domain')}>打开域名</button>
      <button type="button" onClick={() => management.openPanel('validity-extension')}>打开延长有效期</button>
      <button type="button" onClick={() => management.openPanel('monitoring-instance-link')}>打开关联监控</button>
      <button type="button" onClick={() => management.openPanel('monitoring-instance-evidence')}>打开已关联监控</button>
      <button type="button" onClick={() => management.openPanel('archive')}>打开归档</button>
      <button type="button" onClick={() => setVpsId('vps_b')}>切换 VPS</button>
      <VPSOverviewManagementActions
        vpsId={vpsId}
        displayName={vpsId === 'vps_a' ? '东京边缘' : '大阪边缘'}
        management={management}
        managementTriggerRef={triggerRef}
        onOverviewRefresh={onRefresh}
        {...(writeOwnerStore ? { writeOwnerStore } : {})}
        {...(viewToken ? { viewToken } : {})}
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
    const form = (nameInput as HTMLInputElement).form

    fireEvent.submit(form!)
    fireEvent.submit(form!)

    expect(update).toHaveBeenCalledTimes(1)
    await act(async () => mutation.resolve(detailFixture('vps_a', '东京边缘已更新')))
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
  })

  it('blocks Overview while a Legacy view owns the same VPS and unblocks only after that exact owner settles', async () => {
    const registry = createVPSWriteOwnerStore()
    const legacyOwner = registry.begin({
      vpsId: 'vps_a',
      viewToken: 'legacy-view',
      generation: 1,
      operation: 'subscription',
    })
    expect(legacyOwner).not.toBeNull()
    if (!legacyOwner) return

    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a', '东京边缘'))
    const create = vi.spyOn(api, 'createVPSSubscription').mockResolvedValue({} as never)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness
          onRefresh={refresh}
          writeOwnerStore={registry}
          viewToken="overview-view"
        />
      </MemoryRouter>,
    )

    expect(screen.getByText('操作处理中，请等待当前写入完成。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '打开订阅' }))
    await screen.findByLabelText('价格')
    const blockedSave = screen.getByRole('button', { name: '保存中…' })
    expect(blockedSave).toBeDisabled()
    fireEvent.click(blockedSave)
    expect(create).not.toHaveBeenCalled()

    act(() => {
      registry.finish(legacyOwner)
    })
    const enabledSave = await screen.findByRole('button', { name: '新增订阅' })
    expect(enabledSave).toBeEnabled()
  })

  it('keeps an Overview write owner while the view leaves and returns', async () => {
    const registry = createVPSWriteOwnerStore()
    const pendingCreate = deferred<Awaited<ReturnType<typeof api.createVPSSubscription>>>()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a', '东京边缘'))
    const create = vi.spyOn(api, 'createVPSSubscription').mockReturnValue(pendingCreate.promise)
    const refresh = vi.fn().mockResolvedValue(true)

    const firstView = render(
      <MemoryRouter>
        <Harness onRefresh={refresh} writeOwnerStore={registry} viewToken="overview-old-view" />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: '打开订阅' }))
    fireEvent.change(await screen.findByLabelText('价格'), { target: { value: '12' } })
    fireEvent.click(screen.getByRole('button', { name: '新增订阅' }))
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))

    firstView.unmount()
    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} writeOwnerStore={registry} viewToken="overview-returned-view" />
      </MemoryRouter>,
    )
    expect(screen.getByText('操作处理中，请等待当前写入完成。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '打开订阅' }))
    await screen.findByLabelText('价格')
    expect(screen.getByRole('button', { name: '保存中…' })).toBeDisabled()
    expect(create).toHaveBeenCalledTimes(1)

    await act(async () => {
      pendingCreate.resolve({} as Awaited<ReturnType<typeof api.createVPSSubscription>>)
    })
    expect(await screen.findByRole('button', { name: '新增订阅' })).toBeEnabled()
    expect(create).toHaveBeenCalledTimes(1)
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
    expect(screen.getByRole('textbox', { name: '原因' })).toHaveValue('旧确认')
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
    expect(screen.getByRole('combobox', { name: '国家 / 地区' })).toHaveValue('美国')

    fireEvent.change(screen.getByRole('textbox', { name: 'IPv4' }), {
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

  it('saves a typed unique ISO country name without selecting a list option', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    const update = vi.spyOn(api, 'updateVPSAsset').mockResolvedValue(detail)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    const combo = await screen.findByRole('combobox', { name: '国家 / 地区' })
    fireEvent.change(combo, { target: { value: 'Andorra' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1))
    expect(update).toHaveBeenCalledWith('vps_a', expect.objectContaining({ country: 'AD' }), {
      expectedUpdatedAt: detail.updated_at,
    })
  })

  it('saves a typed custom country without selecting a list option', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    const update = vi.spyOn(api, 'updateVPSAsset').mockResolvedValue(detail)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    const combo = await screen.findByRole('combobox', { name: '国家 / 地区' })
    fireEvent.change(combo, { target: { value: '北境观测' } })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    await waitFor(() => expect(update).toHaveBeenCalledTimes(1))
    expect(update).toHaveBeenCalledWith('vps_a', expect.objectContaining({ country: '北境观测' }), {
      expectedUpdatedAt: detail.updated_at,
    })
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
    fireEvent.click(screen.getByRole('button', { name: '新增订阅' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('同一幂等键已用于不同的订阅内容')
    fireEvent.click(screen.getByRole('button', { name: '新增订阅' }))
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
    fireEvent.click(screen.getByRole('button', { name: '新增订阅' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to fetch')
    fireEvent.click(screen.getByRole('button', { name: '新增订阅' }))
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

    expect(await screen.findByRole('link', { name: '继续取消 / 退役' })).toHaveAttribute(
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

    expect(await screen.findByRole('link', { name: '继续取消 / 退役' })).toHaveAttribute(
      'href',
      '/vps/vps_a?workbench=cancellation',
    )
  })

  it('localizes renewal compare rows and treats an already-satisfied latest decision as done', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const latest = {
      ...stale,
      renewal_decision: 'cancel' as const,
      updated_at: '2026-08-21T00:00:00Z',
    }
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(stale)
      .mockResolvedValueOnce(latest)
    const update = vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps updated', { code: 'vps_asset_conflict' }))
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
    expect(await screen.findByRole('button', { name: '加载最新版本' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '加载最新版本' }))
    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent('该决策已由其他操作完成')
    })
    expect(screen.queryByRole('dialog', { name: '续费决策' })).not.toBeInTheDocument()
    expect(update).toHaveBeenCalledTimes(1)
    expect(refresh).toHaveBeenCalled()
  })

  it('shows localized renewal values when the latest decision still differs', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const latest = {
      ...stale,
      renewal_decision: 'observe' as const,
      updated_at: '2026-08-21T00:00:00Z',
    }
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(stale)
      .mockResolvedValueOnce(latest)
    vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps updated', { code: 'vps_asset_conflict' }))
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
    fireEvent.click(screen.getByRole('button', { name: '保存续费决策' }))
    expect(await screen.findByRole('button', { name: '加载最新版本' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '加载最新版本' }))
    expect(await screen.findByRole('combobox', { name: '续费决策' })).toHaveValue('cancel')
  })

  it('routes to archive after a readonly race on a cancelled VPS', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const archived = {
      ...stale,
      lifecycle_status: 'cancelled' as const,
      updated_at: '2026-08-21T00:00:00Z',
    }
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(stale)
      .mockResolvedValueOnce(archived)
    vi.spyOn(api, 'updateVPSAsset')
      .mockRejectedValueOnce(new ApiError(409, 'vps asset readonly', { code: 'vps_asset_readonly' }))
    const refresh = vi.fn().mockResolvedValue(true)
    const inventoryState = {
      vpsInventoryHref: '/vps?workspace=ledger&q=Tokyo&selected=vps_a',
      extraProvenance: 'keep-me',
    }

    function LocationProbe() {
      const location = useLocation()
      return (
        <div data-testid="location-path" data-state={JSON.stringify(location.state ?? null)}>
          {location.pathname}
        </div>
      )
    }

    render(
      <MemoryRouter initialEntries={[{ pathname: '/vps/vps_a', state: inventoryState }]}>
        <Routes>
          <Route path="/vps/:vpsId" element={<Harness onRefresh={refresh} />} />
          <Route path="/archive/:vpsId" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'VPS 名称' }), {
      target: { value: '我的草稿' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    await waitFor(() => expect(screen.getByTestId('location-path')).toHaveTextContent('/archive/vps_a'))
    expect(screen.getByTestId('location-path')).toHaveAttribute('data-state', JSON.stringify(inventoryState))
  })

  it('does not write a stale readonly error onto the next VPS after a delayed identity GET', async () => {
    const stale = detailFixture('vps_a', '东京边缘')
    const nextVPS = detailFixture('vps_b', '大阪边缘')
    const identity = deferred<VPSAssetDetail>()
    let aGets = 0
    vi.spyOn(api, 'getVPSAsset').mockImplementation(async (id) => {
      if (id === 'vps_b') return nextVPS
      aGets += 1
      if (aGets === 1) return stale
      return identity.promise
    })
    vi.spyOn(api, 'listProviders').mockResolvedValue([])
    vi.spyOn(api, 'updateVPSAsset').mockRejectedValue(
      new ApiError(409, 'vps asset readonly', { code: 'vps_asset_readonly' }),
    )
    const refresh = vi.fn().mockResolvedValue(true)

    function LocationProbe() {
      const location = useLocation()
      return <div data-testid="location-path">{location.pathname}</div>
    }

    render(
      <MemoryRouter initialEntries={['/vps/vps_a']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<Harness onRefresh={refresh} />} />
          <Route path="/archive/:vpsId" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开事实' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'VPS 名称' }), {
      target: { value: '我的草稿' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存基础信息' }))
    await waitFor(() => expect(aGets).toBe(2))
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))
    expect(await screen.findByRole('textbox', { name: 'VPS 名称' })).toHaveValue('大阪边缘')

    await act(async () => {
      identity.resolve({ ...stale, lifecycle_status: 'cancelled' })
    })

    expect(screen.getByRole('textbox', { name: 'VPS 名称' })).toHaveValue('大阪边缘')
    expect(screen.queryByText('当前状态不允许修改')).not.toBeInTheDocument()
    expect(screen.queryByTestId('location-path')).not.toBeInTheDocument()
  })

  it('creates a service record with idempotency key and refreshes overview', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listTargets').mockResolvedValue([targetRecord()])
    const createService = vi.spyOn(api, 'createVPSService').mockResolvedValue(serviceRecord({
      target_id: 'tgt_1',
      port: 80,
    }))
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开服务' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '服务名称' }), {
      target: { value: 'Blog Service' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))

    await waitFor(() => expect(createService).toHaveBeenCalledTimes(1))
    expect(createService).toHaveBeenCalledWith(
      'vps_a',
      expect.objectContaining({ name: 'Blog Service' }),
      expect.any(String),
    )
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('dialog', { name: '新增服务' })).not.toBeInTheDocument()
    expect(screen.getByText('服务记录已创建，概览已刷新。')).toBeInTheDocument()
  })

  it('creates a domain record with lazy-loaded targets/services and refreshes overview', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listTargets').mockResolvedValue([])
    vi.spyOn(api, 'listVPSServices').mockResolvedValue([])
    const createDomain = vi.spyOn(api, 'createVPSDomain').mockResolvedValue(domainRecord())
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开域名' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '域名' }), {
      target: { value: 'app.example.com' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建域名记录' }))

    await waitFor(() => expect(createDomain).toHaveBeenCalledTimes(1))
    expect(createDomain).toHaveBeenCalledWith(
      'vps_a',
      expect.objectContaining({ domain_name: 'app.example.com' }),
      expect.any(String),
    )
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('dialog', { name: '新增域名' })).not.toBeInTheDocument()
    expect(screen.getByText('域名记录已创建，概览已刷新。')).toBeInTheDocument()
  })

  it('rejects validity extension when no active subscription exists', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue([
      subscriptionRecord({ status: 'cancelled', renew_at: '2026-09-01' }),
    ])
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开延长有效期' }))
    expect(await screen.findByText('当前 VPS 没有生效中订阅，无法直接延长有效期。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存延长记录' })).toBeDisabled()
  })

  it('rejects validity extension when multiple active subscriptions exist', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue([
      subscriptionRecord({ subscription_id: 'sub_1', status: 'active', renew_at: '2026-09-01' }),
      subscriptionRecord({ subscription_id: 'sub_2', status: 'active', renew_at: '2026-10-01' }),
    ])
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开延长有效期' }))
    expect(await screen.findByText('当前 VPS 存在多个生效中订阅，无法直接延长有效期。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存延长记录' })).toBeDisabled()
  })

  it('rejects backwards extend_to date and extends validity when valid', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue([
      subscriptionRecord({ subscription_id: 'sub_active', renew_at: '2026-09-15' }),
    ])
    const extendValidity = vi.spyOn(api, 'extendVPSValidity').mockResolvedValue(validityResult())
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开延长有效期' }))
    const extendInput = await screen.findByLabelText(/延长至日期/)
    const reasonInput = screen.getByLabelText(/延长原因/)
    expect(screen.getByRole('button', { name: '保存延长记录' })).not.toBeDisabled()
    fireEvent.change(reasonInput, { target: { value: '机房故障补偿 7 天' } })
    fireEvent.change(extendInput, { target: { value: '2026-09-10' } })
    fireEvent.click(screen.getByRole('button', { name: '保存延长记录' }))

    expect(await screen.findByText('延长至日期不能早于当前生效中订阅续费日。')).toBeInTheDocument()
    expect(extendValidity).not.toHaveBeenCalled()

    fireEvent.change(extendInput, { target: { value: '2026-10-15' } })
    fireEvent.click(screen.getByRole('button', { name: '保存延长记录' }))

    await waitFor(() => expect(extendValidity).toHaveBeenCalledTimes(1))
    expect(extendValidity).toHaveBeenCalledWith('vps_a', expect.objectContaining({
      extend_to: '2026-10-15',
      reason: '机房故障补偿 7 天',
    }))
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('dialog', { name: '延长有效期' })).not.toBeInTheDocument()
    expect(screen.getByText(/有效期已延长/)).toBeInTheDocument()
  })

  it('links an existing unlinked monitoring instance and refreshes overview', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listMonitoringInstances').mockResolvedValue([monitoringRecord()])
    const linkInstance = vi.spyOn(api, 'linkVPSMonitoringInstance').mockResolvedValue(linkRecord())
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开关联监控' }))
    const select = await screen.findByRole('combobox', { name: '选择监控实例' })
    fireEvent.change(select, { target: { value: 'mon_1' } })
    fireEvent.click(screen.getByRole('button', { name: '关联监控实例' }))

    await waitFor(() => expect(linkInstance).toHaveBeenCalledTimes(1))
    expect(linkInstance).toHaveBeenCalledWith('vps_a', {
      monitoring_instance_id: 'mon_1',
      note: '',
    })
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    expect(screen.queryByRole('dialog', { name: '关联监控实例' })).not.toBeInTheDocument()
    expect(screen.getByText('监控实例关联已更新，概览已刷新。')).toBeInTheDocument()
  })

  it('confirms unlinking a monitoring instance from relation panel and refreshes overview', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([linkedMonitoring()])
    const unlinkInstance = vi.spyOn(api, 'unlinkVPSMonitoringInstance').mockResolvedValue(linkRecord({
      monitoring_instance_id: 'mon_linked',
      note: 'primary monitoring',
    }))
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开已关联监控' }))
    expect(await screen.findByRole('dialog', { name: '已关联监控实例' })).toBeInTheDocument()

    // Request unlink
    fireEvent.click(await screen.findByRole('button', { name: '解除关联' }))
    expect(screen.getByRole('alertdialog', { name: '确认解除监控实例关联' })).toBeInTheDocument()

    // Confirm unlink
    fireEvent.click(screen.getByRole('button', { name: '确认解除关联' }))

    await waitFor(() => expect(unlinkInstance).toHaveBeenCalledTimes(1))
    expect(unlinkInstance).toHaveBeenCalledWith('vps_a', {
      monitoring_instance_id: 'mon_linked',
      note: 'primary monitoring',
    })
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
    expect(screen.getByText('监控实例关联已解除')).toBeInTheDocument()
  })

  it('keeps the same service idempotency key after a transport failure and rotates on 409', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listTargets').mockResolvedValue([])
    const keys: string[] = []
    vi.spyOn(api, 'createVPSService')
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        throw new TypeError('Failed to fetch')
      })
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        throw new ApiError(409, 'idempotency key reused', { code: 'idempotency_key_reused' })
      })
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        return serviceRecord({ service_id: 'svc_retry', name: 'Retry Service', target_id: null })
      })
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开服务' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '服务名称' }), {
      target: { value: 'Retry Service' },
    })

    // First attempt: transport failure
    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))
    expect(await screen.findByRole('alert')).toBeInTheDocument()

    // Second attempt: retries with SAME key
    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))
    await waitFor(() => expect(keys).toHaveLength(2))
    expect(keys[0]).toBe(keys[1])

    // Third attempt: after 409, rotates key
    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))
    await waitFor(() => expect(keys).toHaveLength(3))
    expect(keys[2]).not.toBe(keys[0])
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1))
  })

  it('ignores completed service creation after route switches to another VPS', async () => {
    const mutation = deferred<AssetServiceRecord>()
    vi.spyOn(api, 'getVPSAsset').mockImplementation(async (id) => (
      id === 'vps_a' ? detailFixture('vps_a', '东京边缘') : detailFixture('vps_b', '大阪边缘')
    ))
    vi.spyOn(api, 'listTargets').mockResolvedValue([])
    vi.spyOn(api, 'createVPSService').mockReturnValue(mutation.promise)
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开服务' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '服务名称' }), {
      target: { value: 'Tokyo Web' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))

    await act(async () => {
      mutation.resolve(serviceRecord({ service_id: 'svc_late', name: 'Tokyo Web', target_id: null }))
    })

    expect(refresh).not.toHaveBeenCalled()
  })

  it('does not POST a service when authoritative GET reports a terminal VPS', async () => {
    const inventoryState = { vpsInventoryHref: '/vps?workspace=ledger&q=Tokyo&selected=vps_a' }
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a', '东京边缘'))
    vi.spyOn(api, 'listTargets').mockResolvedValue([])
    const createService = vi.spyOn(api, 'createVPSService')
    const refresh = vi.fn().mockResolvedValue(true)

    function ArchiveProbe() {
      const location = useLocation()
      return (
        <output data-testid="archive-location" data-state={JSON.stringify(location.state)}>
          {location.pathname}
        </output>
      )
    }

    render(
      <MemoryRouter initialEntries={[{ pathname: '/', state: inventoryState }]}>
        <Routes>
          <Route path="/" element={<Harness onRefresh={refresh} />} />
          <Route path="/archive/:vpsId" element={<ArchiveProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开服务' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '服务名称' }), {
      target: { value: 'Should Not Create' },
    })

    vi.mocked(api.getVPSAsset).mockResolvedValue({
      ...detailFixture('vps_a', '东京边缘'),
      lifecycle_status: 'cancelled',
    })
    fireEvent.click(screen.getByRole('button', { name: '创建服务记录' }))

    await waitFor(() => expect(screen.getByTestId('archive-location')).toHaveTextContent('/archive/vps_a'))
    expect(createService).not.toHaveBeenCalled()
    expect(screen.getByTestId('archive-location')).toHaveAttribute('data-state', JSON.stringify(inventoryState))
  })

  it('keeps relation reads visible but blocks unlink until non-terminal authority is ready', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue({
      ...detailFixture('vps_a', '东京边缘'),
      lifecycle_status: 'cancelled',
    })
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([linkedMonitoring()])
    const unlinkInstance = vi.spyOn(api, 'unlinkVPSMonitoringInstance')
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Routes>
          <Route path="/" element={<Harness onRefresh={refresh} />} />
          <Route path="/archive/:vpsId" element={<p>archive</p>} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开已关联监控' }))
    expect(await screen.findByText('Tokyo Mon Linked')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '解除关联' })).not.toBeInTheDocument()
    expect(unlinkInstance).not.toHaveBeenCalled()
  })

  it('does not treat a failed service catalog as an empty association list', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    vi.spyOn(api, 'listTargets').mockResolvedValue([])
    const listServices = vi.spyOn(api, 'listVPSServices')
      .mockRejectedValueOnce(new Error('services unavailable'))
      .mockResolvedValueOnce([serviceRecord({ name: 'Restored Gateway' })])
    const createDomain = vi.spyOn(api, 'createVPSDomain')
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开域名' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('services unavailable')
    expect(screen.queryByText('当前 VPS 还没有服务记录，可先创建服务或保留为空。')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建域名记录' })).toBeDisabled()
    expect(createDomain).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    expect(await screen.findByRole('option', { name: /Restored Gateway/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建域名记录' })).toBeEnabled()
    expect(listServices).toHaveBeenCalledTimes(2)
  })

  it('keeps a service draft when retrying a failed target catalog', async () => {
    const detail = detailFixture('vps_a', '东京边缘')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    const listTargets = vi.spyOn(api, 'listTargets')
      .mockRejectedValueOnce(new Error('targets unavailable'))
      .mockResolvedValueOnce([targetRecord({ name: 'Restored Probe' })])
    const refresh = vi.fn().mockResolvedValue(true)

    render(
      <MemoryRouter>
        <Harness onRefresh={refresh} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开服务' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '服务名称' }), {
      target: { value: 'Kept Service' },
    })
    expect(await screen.findByRole('alert')).toHaveTextContent('targets unavailable')
    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    expect(await screen.findByRole('option', { name: /Restored Probe/ })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '服务名称' })).toHaveValue('Kept Service')
    expect(listTargets).toHaveBeenCalledTimes(2)
  })

  it('archives through the confirmation dialog and preserves inventory location state', async () => {
    const inventoryState = {
      vpsInventoryHref: '/vps?workspace=ledger&q=Tokyo&selected=vps_a',
      extraProvenance: 'keep-me',
    }
    vi.spyOn(api, 'getVPSArchiveReview').mockResolvedValue({
      vps: detailFixture('vps_a', '东京边缘'),
      subscriptions: [],
      monitoring_instance_links: [],
      services: [],
      domains: [],
      target_links: [],
      warnings: [],
      blockers: [],
      eligible: true,
      blocker_details: [],
    })
    const archive = vi.spyOn(api, 'archiveVPS').mockResolvedValue({
      vps: { ...detailFixture('vps_a', '东京边缘'), lifecycle_status: 'archived' },
      subscriptions: [],
      monitoring_instance_links: [],
      services: [],
      domains: [],
      target_links: [],
      warnings: [],
      blockers: ['VPS 已归档，只能在归档详情页只读查看或执行受控恢复。'],
      eligible: false,
      blocker_details: [],
    })
    const refresh = vi.fn().mockResolvedValue(true)

    function ArchiveProbe() {
      const location = useLocation()
      return (
        <output data-testid="archive-location" data-state={JSON.stringify(location.state)}>
          {location.pathname}
        </output>
      )
    }

    render(
      <MemoryRouter initialEntries={[{ pathname: '/', state: inventoryState }]}>
        <Routes>
          <Route path="/" element={<Harness onRefresh={refresh} />} />
          <Route path="/archive/:vpsId" element={<ArchiveProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开归档' }))
    const dialog = await screen.findByRole('alertdialog', { name: '确认归档 VPS' })
    fireEvent.change(within(dialog).getByRole('textbox', { name: '归档原因' }), {
      target: { value: '订阅已结束' },
    })
    fireEvent.change(within(dialog).getByRole('textbox', { name: '输入 VPS 名称确认归档' }), {
      target: { value: '东京边缘' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认归档' }))

    await waitFor(() => expect(archive).toHaveBeenCalledWith('vps_a', { confirmation_name: '东京边缘', reason: '订阅已结束' }))
    expect(screen.getByTestId('archive-location')).toHaveTextContent('/archive/vps_a')
    expect(screen.getByTestId('archive-location')).toHaveAttribute('data-state', JSON.stringify(inventoryState))
  })

  it('keeps inventory state and the archive dialog when archive write fails', async () => {
    const inventoryState = {
      vpsInventoryHref: '/vps?workspace=ledger&q=Tokyo&selected=vps_a',
      extraProvenance: 'keep-me',
    }
    vi.spyOn(api, 'getVPSArchiveReview').mockResolvedValue({
      vps: detailFixture('vps_a', '东京边缘'),
      subscriptions: [],
      monitoring_instance_links: [],
      services: [],
      domains: [],
      target_links: [],
      warnings: [],
      blockers: [],
      eligible: true,
      blocker_details: [],
    })
    vi.spyOn(api, 'archiveVPS').mockRejectedValue(new ApiError(409, 'archive conflict'))
    const refresh = vi.fn().mockResolvedValue(true)

    function LocationProbe() {
      const location = useLocation()
      return (
        <div data-testid="location-path" data-state={JSON.stringify(location.state ?? null)}>
          {location.pathname}
        </div>
      )
    }

    render(
      <MemoryRouter initialEntries={[{ pathname: '/', state: inventoryState }]}>
        <LocationProbe />
        <Routes>
          <Route path="/" element={<Harness onRefresh={refresh} />} />
          <Route path="/archive/:vpsId" element={<div>archived</div>} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开归档' }))
    const dialog = await screen.findByRole('alertdialog', { name: '确认归档 VPS' })
    fireEvent.change(within(dialog).getByRole('textbox', { name: '归档原因' }), {
      target: { value: '订阅已结束' },
    })
    fireEvent.change(within(dialog).getByRole('textbox', { name: '输入 VPS 名称确认归档' }), {
      target: { value: '东京边缘' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认归档' }))

    expect(await within(dialog).findByText('archive conflict')).toBeInTheDocument()
    expect(screen.getByTestId('location-path')).toHaveTextContent('/')
    expect(screen.getByTestId('location-path')).toHaveAttribute('data-state', JSON.stringify(inventoryState))
    expect(screen.queryByText('archived')).not.toBeInTheDocument()
  })

  it('wires inline blocker action in archive modal to scoped dependency status correction', async () => {
    vi.spyOn(api, 'getVPSArchiveReview').mockResolvedValue({
      vps: detailFixture('vps_a', '东京边缘'),
      subscriptions: [],
      monitoring_instance_links: [],
      services: [],
      domains: [],
      target_links: [],
      warnings: [],
      blockers: ['服务状态待确认'],
      eligible: false,
      blocker_details: [
        {
          code: 'service_status_needs_confirmation',
          object_type: 'service',
          object_id: 'svc_blocker_01',
          display_name: 'Auth API',
          current_state: 'unknown',
          blocked_action: 'archive_vps',
          resolution_action: 'correct_dependency_status',
        },
      ],
    })

    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<Harness onRefresh={vi.fn().mockResolvedValue(true)} />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开归档' }))
    const dialog = await screen.findByRole('alertdialog', { name: '确认归档 VPS' })
    expect(dialog).toBeInTheDocument()

    const correctBtn = within(dialog).getByRole('button', { name: '纠正服务状态' })
    expect(correctBtn).toBeInTheDocument()

    fireEvent.click(correctBtn)

    // DependencyStatusCorrection modal must open
    expect(await screen.findByRole('dialog', { name: '纠正服务状态' })).toBeInTheDocument()
  })

})
