import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useLayoutEffect, useRef, useState } from 'react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../lib/api'
import { ApiError } from '../../lib/apiRequest'
import type {
  CreateVPSMonitoringInstanceResponse,
  VPSAssetDetail,
  VPSMonitoringInstanceSummary,
} from '../../lib/types'
import { useVPSManagementController } from './hooks/useVPSManagementController'
import { VPSOverviewMonitoringOnboarding } from './VPSOverviewMonitoringOnboarding'
import { createVPSWriteOwnerStore, type VPSWriteOwnerStore } from './vpsWriteOwnerStore'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function monitoringLink(id: string): VPSMonitoringInstanceSummary {
  return {
    monitoring_instance_id: id,
    display_name: id,
    group: '',
    region: 'Tokyo',
    city: 'Tokyo',
    provider: 'Example',
    lifecycle_status: 'active',
    monitoring_status: '待接入',
    binding_status: 'unbound',
    current_health_status: '正常',
    last_heartbeat_at: null,
    last_sync_at: null,
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    linked_at: '2026-08-29T00:00:00Z',
    note: '',
  }
}

function detailFixture(vpsId: string, links: VPSMonitoringInstanceSummary[] = []): VPSAssetDetail {
  return {
    vps_id: vpsId,
    display_name: vpsId === 'vps_b' ? '大阪边缘' : '东京边缘',
    provider_id: null,
    provider_name: 'Example Cloud',
    product_name: 'VPS',
    order_ref: '',
    country: 'JP',
    region: 'Tokyo',
    city: 'Chiyoda',
    datacenter: 'TY1',
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
    labels: ['edge', 'prod'],
    note: 'asset note',
    active_monitoring_instance_link_count: links.length,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-29T00:00:00Z',
    monitoring_instance_links: links,
  }
}

function detailWithRuntimeMonitoringEvidence(
  links: unknown,
  activeLinkCount: unknown,
): VPSAssetDetail {
  return {
    ...detailFixture('vps_a'),
    monitoring_instance_links: links,
    active_monitoring_instance_link_count: activeLinkCount,
  } as unknown as VPSAssetDetail
}

function detailWithoutMonitoringLinks(): VPSAssetDetail {
  const detail = { ...detailFixture('vps_a') } as Partial<VPSAssetDetail>
  delete detail.monitoring_instance_links
  return detail as VPSAssetDetail
}

function createdFixture(id = 'mi_new'): CreateVPSMonitoringInstanceResponse {
  return {
    monitoring_instance_id: id,
    display_name: '东京边缘',
    group: '',
    region: 'Tokyo',
    city: 'Chiyoda',
    provider: 'Example Cloud',
    labels: ['edge', 'prod'],
    note: 'asset note',
    lifecycle_status: 'active',
    monitoring_status: '待接入',
    binding_status: 'unbound',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-08-29T00:00:00Z',
    updated_at: '2026-08-29T00:00:00Z',
    link: {
      link_id: 'link_new',
      vps_id: 'vps_a',
      monitoring_instance_id: id,
      linked_at: '2026-08-29T00:00:00Z',
      unlinked_at: null,
      note: 'created from vps detail',
    },
  }
}

type HarnessProps = {
  onRefresh?: () => Promise<boolean>
  writeOwnerStore?: VPSWriteOwnerStore
  initialVPSID?: string
  onAuthorityLayout?: (snapshot: { formVisible: boolean; submitEnabled: boolean }) => void
}

function Harness({
  onRefresh = async () => true,
  writeOwnerStore: providedWriteOwnerStore,
  initialVPSID = 'vps_a',
  onAuthorityLayout,
}: HarnessProps) {
  const [vpsId, setVpsId] = useState(initialVPSID)
  const [viewToken, setViewToken] = useState('overview-view')
  const [onboardingMounted, setOnboardingMounted] = useState(true)
  const [localWriteOwnerStore] = useState(createVPSWriteOwnerStore)
  const writeOwnerStore = providedWriteOwnerStore ?? localWriteOwnerStore
  const management = useVPSManagementController()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const location = useLocation()

  useLayoutEffect(() => {
    const form = document.querySelector<HTMLFormElement>('form.asset-operation-form')
    const submit = form?.querySelector<HTMLButtonElement>('button[type="submit"]') ?? null
    onAuthorityLayout?.({
      formVisible: form !== null,
      submitEnabled: submit !== null && !submit.disabled,
    })
  })

  return (
    <>
      <button ref={triggerRef} type="button" onClick={() => management.openPanel('monitoring-instance-create')}>
        打开接入
      </button>
      <button type="button" onClick={() => management.closePanel()}>外部关闭</button>
      <button type="button" onClick={() => setVpsId((current) => current === 'vps_a' ? 'vps_b' : 'vps_a')}>
        切换 VPS
      </button>
      <button type="button" onClick={() => setViewToken('replacement-view')}>更换视图权限</button>
      <button type="button" onClick={() => setOnboardingMounted(false)}>卸载接入组件</button>
      <div data-testid="active-panel">{management.panel ?? 'none'}</div>
      <div data-testid="location">{location.pathname}{location.search}</div>
      {onboardingMounted ? (
        <VPSOverviewMonitoringOnboarding
          vpsId={vpsId}
          management={management}
          managementTriggerRef={triggerRef}
          onOverviewRefresh={onRefresh}
          writeOwnerStore={writeOwnerStore}
          viewToken={viewToken}
        />
      ) : null}
    </>
  )
}

function renderHarness(props: HarnessProps = {}) {
  return render(
    <MemoryRouter initialEntries={[`/vps/${props.initialVPSID ?? 'vps_a'}`]}>
      <Harness {...props} />
    </MemoryRouter>,
  )
}

async function openZeroLinkForm() {
  fireEvent.click(screen.getByRole('button', { name: '打开接入' }))
  return screen.findByRole('textbox', { name: '监控实例名称' })
}

async function submitCreate() {
  const submit = screen.getByRole('button', { name: '接入/升级 agent' })
  const form = submit.closest('form')
  expect(form).not.toBeNull()
  fireEvent.submit(form!)
}

describe('VPSOverviewMonitoringOnboarding', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('waits for authoritative zero-link evidence before rendering the create form', async () => {
    const pending = deferred<VPSAssetDetail>()
    vi.spyOn(api, 'getVPSAsset').mockReturnValue(pending.promise)
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))

    expect(api.getVPSAsset).toHaveBeenCalledWith('vps_a')
    expect(screen.getByRole('status')).toHaveTextContent('正在检查监控关联')
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()

    await act(async () => pending.resolve(detailFixture('vps_a')))

    expect(await screen.findByRole('textbox', { name: '监控实例名称' })).toHaveValue('东京边缘')
    expect(screen.getByRole('textbox', { name: '服务商' })).toHaveValue('Example Cloud')
  })

  it.each([
    ['missing', detailWithoutMonitoringLinks()],
    ['null', detailWithRuntimeMonitoringEvidence(null, 0)],
  ])('fails closed when authoritative monitoring links are $0', async (_case, detail) => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detail)
    const create = vi.spyOn(api, 'createVPSMonitoringInstance')
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('监控关联证据无效')
    expect(screen.getByRole('button', { name: '重试加载' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
    expect(create).not.toHaveBeenCalled()
  })

  it.each([
    ['mismatched', 1],
    ['non-finite', Number.NaN],
    ['negative', -1],
    ['fractional', 0.5],
  ])('fails closed when the authoritative link count is $0', async (_case, activeLinkCount) => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(
      detailWithRuntimeMonitoringEvidence([], activeLinkCount),
    )
    const create = vi.spyOn(api, 'createVPSMonitoringInstance')
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('监控关联证据无效')
    expect(screen.getByRole('button', { name: '重试加载' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
    expect(create).not.toHaveBeenCalled()
  })

  it('reuses one authoritative active link without issuing a create request', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a', [monitoringLink('mi_existing')]))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance')
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(
      '/monitoring/mi_existing?onboarding=1&return_vps=vps_a',
    ))
    expect(create).not.toHaveBeenCalled()
    expect(screen.getByTestId('active-panel')).toHaveTextContent('none')
  })

  it('fails closed when the only authoritative active link has a malformed monitoring instance ID', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a', [monitoringLink('mi/existing')]))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance')
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('监控关联证据无效')
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_a')
    expect(create).not.toHaveBeenCalled()
  })

  it('fails closed on multiple authoritative links and opens evidence', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a', [
      monitoringLink('mi_1'),
      monitoringLink('mi_2'),
    ]))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance')
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))

    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent(
      '检测到多个 active 监控关联，请先人工复核',
    ))
    expect(screen.getByTestId('active-panel')).toHaveTextContent('monitoring-instance-evidence')
    expect(create).not.toHaveBeenCalled()
  })

  it('shows an authoritative load error and retries without exposing the form early', async () => {
    vi.spyOn(api, 'getVPSAsset')
      .mockRejectedValueOnce(new ApiError(503, 'detail unavailable'))
      .mockResolvedValueOnce(detailFixture('vps_a'))
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('detail unavailable')
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '重试加载' }))

    expect(await screen.findByRole('textbox', { name: '监控实例名称' })).toHaveValue('东京边缘')
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)
  })

  it('discards a late authoritative read after the panel closes and restores focus', async () => {
    const pending = deferred<VPSAssetDetail>()
    vi.spyOn(api, 'getVPSAsset').mockReturnValue(pending.promise)
    renderHarness()

    const trigger = screen.getByRole('button', { name: '打开接入' })
    fireEvent.click(trigger)
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(trigger).toHaveFocus())
    await act(async () => pending.resolve(detailFixture('vps_a')))

    expect(screen.queryByRole('dialog', { name: '接入/升级 agent' })).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
  })

  it('keeps a late VPS A read from mutating the VPS B panel', async () => {
    const pendingA = deferred<VPSAssetDetail>()
    vi.spyOn(api, 'getVPSAsset').mockImplementation((vpsId) => (
      vpsId === 'vps_a' ? pendingA.promise : Promise.resolve(detailFixture('vps_b'))
    ))
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))
    expect(await screen.findByRole('textbox', { name: '监控实例名称' })).toHaveValue('大阪边缘')

    await act(async () => pendingA.resolve(detailFixture('vps_a')))

    expect(screen.getByRole('textbox', { name: '监控实例名称' })).toHaveValue('大阪边缘')
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_a')
  })

  it('does not expose the old form during an external close and same-VPS reopen', async () => {
    const pendingSecondLoad = deferred<VPSAssetDetail>()
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockReturnValueOnce(pendingSecondLoad.promise)
    const authorityLayouts: Array<{ formVisible: boolean; submitEnabled: boolean }> = []
    renderHarness({ onAuthorityLayout: (snapshot) => authorityLayouts.push(snapshot) })

    await openZeroLinkForm()
    fireEvent.click(screen.getByRole('button', { name: '外部关闭' }))
    authorityLayouts.length = 0
    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))

    expect(authorityLayouts).not.toContainEqual({ formVisible: true, submitEnabled: true })
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)

    await act(async () => pendingSecondLoad.resolve(detailFixture('vps_a')))
    expect(await screen.findByRole('textbox', { name: '监控实例名称' })).toBeInTheDocument()
  })

  it('does not expose the old form when the same VPS receives a replacement view token', async () => {
    const pendingReplacementLoad = deferred<VPSAssetDetail>()
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockReturnValueOnce(pendingReplacementLoad.promise)
    const authorityLayouts: Array<{ formVisible: boolean; submitEnabled: boolean }> = []
    renderHarness({ onAuthorityLayout: (snapshot) => authorityLayouts.push(snapshot) })

    await openZeroLinkForm()
    authorityLayouts.length = 0
    fireEvent.click(screen.getByRole('button', { name: '更换视图权限' }))

    expect(authorityLayouts).not.toContainEqual({ formVisible: true, submitEnabled: true })
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)

    await act(async () => pendingReplacementLoad.resolve(detailFixture('vps_a')))
    expect(await screen.findByRole('textbox', { name: '监控实例名称' })).toBeInTheDocument()
  })

  it('posts the normalized body once and navigates after a successful refresh', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockResolvedValue(createdFixture('mi_new'))
    const refresh = vi.fn().mockResolvedValue(true)
    renderHarness({ onRefresh: refresh })

    await openZeroLinkForm()
    await submitCreate()

    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    expect(create).toHaveBeenCalledWith('vps_a', {
      display_name: '东京边缘',
      group: '',
      region: 'Tokyo',
      city: 'Chiyoda',
      provider: 'Example Cloud',
      labels: ['edge', 'prod'],
      note: 'asset note',
      link_note: 'created from vps detail',
    }, expect.any(String))
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(
      '/monitoring/mi_new?onboarding=1&return_vps=vps_a',
    ))
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('keeps a malformed confirmed create response confirmed without constructing an invalid route', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockResolvedValue(createdFixture('mi/invalid'))
    const refresh = vi.fn().mockResolvedValue(true)
    renderHarness({ onRefresh: refresh })

    await openZeroLinkForm()
    await submitCreate()

    expect(await screen.findByRole('status')).toHaveTextContent(
      '监控实例已创建并关联，但返回的监控实例标识无效',
    )
    expect(screen.getByRole('link', { name: '前往监控列表' })).toHaveAttribute('href', '/monitoring')
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_a')
    expect(create).toHaveBeenCalledTimes(1)
    expect(refresh).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '打开接入' }))
    await screen.findByRole('textbox', { name: '监控实例名称' })
    expect(create).toHaveBeenCalledTimes(1)
  })

  it.each([
    ['returns false', async () => false],
    ['rejects', async () => Promise.reject(new Error('refresh unavailable'))],
  ])('keeps a truthful continuation when overview refresh $0', async (_label, refresh) => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockResolvedValue(createdFixture())
    renderHarness({ onRefresh: refresh })

    await openZeroLinkForm()
    await submitCreate()

    expect(await screen.findByRole('status')).toHaveTextContent('监控实例已创建并关联，但概览刷新失败')
    expect(screen.getByRole('link', { name: '继续接入 agent' })).toHaveAttribute(
      'href',
      '/monitoring/mi_new?onboarding=1&return_vps=vps_a',
    )
    expect(screen.queryByText('创建监控实例失败')).not.toBeInTheDocument()
    expect(create).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('active-panel')).toHaveTextContent('none')
  })

  it('reuses the same idempotency key after a transport failure', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const keys: string[] = []
    vi.spyOn(api, 'createVPSMonitoringInstance')
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        throw new TypeError('Failed to fetch')
      })
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        return createdFixture()
      })
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()
    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to fetch')
    await submitCreate()

    await waitFor(() => expect(keys).toHaveLength(2))
    expect(keys[0]).toBe(keys[1])
  })

  it('rotates the idempotency key after the request body changes', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const keys: string[] = []
    vi.spyOn(api, 'createVPSMonitoringInstance')
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        throw new TypeError('Failed to fetch')
      })
      .mockImplementationOnce(async (_vpsId, _input, key) => {
        keys.push(key)
        return createdFixture()
      })
    renderHarness()

    const name = await openZeroLinkForm()
    await submitCreate()
    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to fetch')
    fireEvent.change(name, { target: { value: '东京边缘 B' } })
    await submitCreate()

    await waitFor(() => expect(keys).toHaveLength(2))
    expect(keys[0]).not.toBe(keys[1])
  })

  it('rotates a reused idempotency key without running active-link convergence', async () => {
    const get = vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const keys: string[] = []
    vi.spyOn(api, 'createVPSMonitoringInstance').mockImplementation(async (_vpsId, _input, key) => {
      keys.push(key)
      throw new ApiError(409, 'idempotency key reused', { code: 'idempotency_key_reused' })
    })
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()
    expect(await screen.findByRole('alert')).toHaveTextContent('幂等键')
    await submitCreate()

    await waitFor(() => expect(keys).toHaveLength(2))
    expect(keys[0]).not.toBe(keys[1])
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('converges a non-idempotency 409 to one active link without another POST', async () => {
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockResolvedValueOnce(detailFixture('vps_a', [monitoringLink('mi_raced')]))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockRejectedValue(
      new ApiError(409, 'vps active monitoring instance exists'),
    )
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(
      '/monitoring/mi_raced?onboarding=1&return_vps=vps_a',
    ))
    expect(create).toHaveBeenCalledTimes(1)
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)
  })

  it('converges a non-idempotency 409 with multiple links to evidence', async () => {
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockResolvedValueOnce(detailFixture('vps_a', [monitoringLink('mi_1'), monitoringLink('mi_2')]))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockRejectedValue(
      new ApiError(409, 'vps active monitoring instance exists'),
    )
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()

    expect(await screen.findByRole('status')).toHaveTextContent('检测到多个 active 监控关联，请先人工复核')
    expect(screen.getByTestId('active-panel')).toHaveTextContent('monitoring-instance-evidence')
    expect(create).toHaveBeenCalledTimes(1)
  })

  it('keeps the original create error when 409 convergence still has zero links', async () => {
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockResolvedValueOnce(detailFixture('vps_a'))
    vi.spyOn(api, 'createVPSMonitoringInstance').mockRejectedValue(
      new ApiError(409, 'vps active monitoring instance exists'),
    )
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()

    expect(await screen.findByRole('alert')).toHaveTextContent('vps active monitoring instance exists')
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)
    expect(screen.getByTestId('active-panel')).toHaveTextContent('monitoring-instance-create')
  })

  it('keeps the original create error when 409 convergence evidence has a count mismatch', async () => {
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockResolvedValueOnce(detailWithRuntimeMonitoringEvidence([monitoringLink('mi_raced')], 0))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockRejectedValue(
      new ApiError(409, 'vps active monitoring instance exists'),
    )
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()

    expect(await screen.findByRole('alert')).toHaveTextContent('vps active monitoring instance exists')
    expect(screen.getByTestId('active-panel')).toHaveTextContent('monitoring-instance-create')
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_a')
    expect(create).toHaveBeenCalledTimes(1)
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)
  })

  it('keeps the original create error when 409 convergence has one malformed link ID', async () => {
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockResolvedValueOnce(detailFixture('vps_a', [monitoringLink('mi/raced')]))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockRejectedValue(
      new ApiError(409, 'vps active monitoring instance exists'),
    )
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()

    expect(await screen.findByRole('alert')).toHaveTextContent('vps active monitoring instance exists')
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_a')
    expect(create).toHaveBeenCalledTimes(1)
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)
  })

  it('keeps the original create error when the 409 convergence read fails', async () => {
    vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockRejectedValueOnce(new ApiError(503, 'convergence unavailable'))
    vi.spyOn(api, 'createVPSMonitoringInstance').mockRejectedValue(
      new ApiError(409, 'vps active monitoring instance exists'),
    )
    renderHarness()

    await openZeroLinkForm()
    await submitCreate()

    expect(await screen.findByRole('alert')).toHaveTextContent('vps active monitoring instance exists')
    expect(screen.queryByText('convergence unavailable')).not.toBeInTheDocument()
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)
  })

  it('keeps the modal dismissible and reports a blocking external same-VPS write', async () => {
    const store = createVPSWriteOwnerStore()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance')
    renderHarness({ writeOwnerStore: store })

    const name = await openZeroLinkForm()
    const form = name.closest('form')
    expect(form).not.toBeNull()
    let otherOwner: ReturnType<VPSWriteOwnerStore['begin']> = null
    act(() => {
      otherOwner = store.begin({
        vpsId: 'vps_a',
        viewToken: 'legacy-view',
        generation: 1,
        operation: 'subscription',
      })
    })
    expect(otherOwner).not.toBeNull()

    expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: '创建中…' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消' })).toBeEnabled()
    fireEvent.submit(form!)

    expect(await screen.findByRole('alert')).toHaveTextContent('上一次保存仍在进行，请稍后再试')
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.queryByRole('dialog', { name: '接入/升级 agent' })).not.toBeInTheDocument()
    expect(create).not.toHaveBeenCalled()
    if (otherOwner) {
      const owner = otherOwner
      act(() => { store.finish(owner) })
    }
  })

  it('treats an old-view monitoring-create owner as external to a new component identity', async () => {
    const store = createVPSWriteOwnerStore()
    const oldViewOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'previous-view',
      generation: 1,
      operation: 'monitoring-create',
    })
    expect(oldViewOwner).not.toBeNull()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    renderHarness({ writeOwnerStore: store })

    await openZeroLinkForm()

    expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: '创建中…' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消' })).toBeEnabled()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: '接入/升级 agent' })).not.toBeInTheDocument()

    await openZeroLinkForm()
    expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(screen.queryByRole('dialog', { name: '接入/升级 agent' })).not.toBeInTheDocument()
    if (oldViewOwner) {
      act(() => { store.finish(oldViewOwner) })
    }
  })

  it('closes from the header during an exact-own POST and settles without stale success', async () => {
    const store = createVPSWriteOwnerStore()
    const pendingCreate = deferred<CreateVPSMonitoringInstanceResponse>()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    vi.spyOn(api, 'createVPSMonitoringInstance').mockReturnValue(pendingCreate.promise)
    const refresh = vi.fn().mockResolvedValue(true)
    renderHarness({ onRefresh: refresh, writeOwnerStore: store })

    await openZeroLinkForm()
    await submitCreate()
    await waitFor(() => expect(api.createVPSMonitoringInstance).toHaveBeenCalledTimes(1))

    expect(screen.getByRole('button', { name: '创建中…' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '取消' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(screen.queryByRole('dialog', { name: '接入/升级 agent' })).not.toBeInTheDocument()
    await act(async () => pendingCreate.resolve(createdFixture()))

    await waitFor(() => expect(store.getSnapshot().has('vps_a')).toBe(false))
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_a')
    expect(screen.queryByRole('link', { name: '继续接入 agent' })).not.toBeInTheDocument()
    expect(refresh).not.toHaveBeenCalled()
  })

  it('revalidates a reopened zero-link form after the observed owner settles and routes to the committed link', async () => {
    const store = createVPSWriteOwnerStore()
    const pendingCreate = deferred<CreateVPSMonitoringInstanceResponse>()
    const postSettleRead = deferred<VPSAssetDetail>()
    const get = vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockReturnValueOnce(postSettleRead.promise)
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockReturnValue(pendingCreate.promise)
    const authorityLayouts: Array<{ formVisible: boolean; submitEnabled: boolean }> = []
    renderHarness({
      writeOwnerStore: store,
      onAuthorityLayout: (snapshot) => authorityLayouts.push(snapshot),
    })

    await openZeroLinkForm()
    await submitCreate()
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await openZeroLinkForm()
    expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeDisabled()
    expect(get).toHaveBeenCalledTimes(2)

    authorityLayouts.length = 0
    await act(async () => pendingCreate.resolve(createdFixture('mi_committed')))

    await waitFor(() => expect(get).toHaveBeenCalledTimes(3))
    expect(authorityLayouts).not.toContainEqual({ formVisible: true, submitEnabled: true })
    expect(create).toHaveBeenCalledTimes(1)

    await act(async () => postSettleRead.resolve(detailFixture('vps_a', [monitoringLink('mi_committed')])))

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(
      '/monitoring/mi_committed?onboarding=1&return_vps=vps_a',
    ))
    expect(get).toHaveBeenCalledTimes(3)
    expect(create).toHaveBeenCalledTimes(1)
  })

  it('re-enables a reopened form only after post-settle authority proves zero links', async () => {
    const store = createVPSWriteOwnerStore()
    const pendingCreate = deferred<CreateVPSMonitoringInstanceResponse>()
    const postSettleRead = deferred<VPSAssetDetail>()
    const get = vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockReturnValueOnce(postSettleRead.promise)
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockReturnValue(pendingCreate.promise)
    const authorityLayouts: Array<{ formVisible: boolean; submitEnabled: boolean }> = []
    renderHarness({
      writeOwnerStore: store,
      onAuthorityLayout: (snapshot) => authorityLayouts.push(snapshot),
    })

    await openZeroLinkForm()
    await submitCreate()
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await openZeroLinkForm()
    expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeDisabled()

    authorityLayouts.length = 0
    await act(async () => pendingCreate.reject(new TypeError('Failed to fetch')))

    await waitFor(() => expect(get).toHaveBeenCalledTimes(3))
    expect(authorityLayouts).not.toContainEqual({ formVisible: true, submitEnabled: true })
    expect(create).toHaveBeenCalledTimes(1)

    await act(async () => postSettleRead.resolve(detailFixture('vps_a')))

    await waitFor(() => expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeEnabled())
    expect(get).toHaveBeenCalledTimes(3)
    expect(create).toHaveBeenCalledTimes(1)
  })

  it('discards an old post-settle probe when a successor owner takes authority', async () => {
    const store = createVPSWriteOwnerStore()
    const firstProbe = deferred<VPSAssetDetail>()
    const successorProbe = deferred<VPSAssetDetail>()
    const get = vi.spyOn(api, 'getVPSAsset')
      .mockResolvedValueOnce(detailFixture('vps_a'))
      .mockReturnValueOnce(firstProbe.promise)
      .mockReturnValueOnce(successorProbe.promise)
    const firstOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'legacy-view-a',
      generation: 1,
      operation: 'subscription',
    })
    expect(firstOwner).not.toBeNull()
    const authorityLayouts: Array<{ formVisible: boolean; submitEnabled: boolean }> = []
    renderHarness({
      writeOwnerStore: store,
      onAuthorityLayout: (snapshot) => authorityLayouts.push(snapshot),
    })

    await openZeroLinkForm()
    expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeDisabled()
    expect(get).toHaveBeenCalledTimes(1)

    act(() => {
      if (firstOwner) store.finish(firstOwner)
    })
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2))

    let successorOwner: ReturnType<VPSWriteOwnerStore['begin']> = null
    act(() => {
      successorOwner = store.begin({
        vpsId: 'vps_a',
        viewToken: 'legacy-view-b',
        generation: 2,
        operation: 'facts',
      })
    })
    expect(successorOwner).not.toBeNull()
    authorityLayouts.length = 0
    await act(async () => firstProbe.resolve(detailFixture('vps_a')))

    expect(authorityLayouts).not.toContainEqual({ formVisible: true, submitEnabled: true })
    const submitAfterStaleProbe = screen.queryByRole('button', { name: '接入/升级 agent' })
    if (submitAfterStaleProbe) expect(submitAfterStaleProbe).toBeDisabled()
    expect(get).toHaveBeenCalledTimes(2)

    act(() => {
      if (successorOwner) store.finish(successorOwner)
    })
    await waitFor(() => expect(get).toHaveBeenCalledTimes(3))
    expect(authorityLayouts).not.toContainEqual({ formVisible: true, submitEnabled: true })

    await act(async () => successorProbe.resolve(detailFixture('vps_a')))

    await waitFor(() => expect(screen.getByRole('button', { name: '接入/升级 agent' })).toBeEnabled())
    expect(get).toHaveBeenCalledTimes(3)
  })

  it('closes from the header while prepareCreate is pending and settles not_sent without POST', async () => {
    const store = createVPSWriteOwnerStore()
    const prepareGate = deferred<void>()
    const originalPrepareCreate = store.prepareCreate
    vi.spyOn(store, 'prepareCreate').mockImplementation(async (owner, wireBody) => {
      await prepareGate.promise
      return originalPrepareCreate(owner, wireBody)
    })
    const finishCreate = vi.spyOn(store, 'finishCreate')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockResolvedValue(createdFixture())
    renderHarness({ writeOwnerStore: store })

    await openZeroLinkForm()
    await submitCreate()
    await waitFor(() => expect(store.prepareCreate).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(screen.queryByRole('dialog', { name: '接入/升级 agent' })).not.toBeInTheDocument()
    await act(async () => prepareGate.resolve())

    await waitFor(() => expect(store.getSnapshot().has('vps_a')).toBe(false))
    expect(create).not.toHaveBeenCalled()
    expect(finishCreate).toHaveBeenCalledWith(expect.objectContaining({ vpsId: 'vps_a' }), 'not_sent')
  })

  it('does not POST when prepareCreate resolves after component unmount and settles not_sent', async () => {
    const store = createVPSWriteOwnerStore()
    const prepareGate = deferred<void>()
    const originalPrepareCreate = store.prepareCreate
    vi.spyOn(store, 'prepareCreate').mockImplementation(async (owner, wireBody) => {
      await prepareGate.promise
      return originalPrepareCreate(owner, wireBody)
    })
    const finishCreate = vi.spyOn(store, 'finishCreate')
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    const create = vi.spyOn(api, 'createVPSMonitoringInstance')
    renderHarness({ writeOwnerStore: store })

    await openZeroLinkForm()
    await submitCreate()
    await waitFor(() => expect(store.prepareCreate).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '卸载接入组件' }))
    await act(async () => prepareGate.resolve())

    await waitFor(() => expect(store.getSnapshot().has('vps_a')).toBe(false))
    expect(create).not.toHaveBeenCalled()
    expect(finishCreate).toHaveBeenCalledWith(expect.objectContaining({ vpsId: 'vps_a' }), 'not_sent')
  })

  it('settles an already-sent POST after unmount without refresh, navigation, or feedback', async () => {
    const store = createVPSWriteOwnerStore()
    const pendingCreate = deferred<CreateVPSMonitoringInstanceResponse>()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    vi.spyOn(api, 'createVPSMonitoringInstance').mockReturnValue(pendingCreate.promise)
    const refresh = vi.fn().mockResolvedValue(true)
    const finishCreate = vi.spyOn(store, 'finishCreate')
    renderHarness({ onRefresh: refresh, writeOwnerStore: store })

    await openZeroLinkForm()
    await submitCreate()
    await waitFor(() => expect(api.createVPSMonitoringInstance).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '卸载接入组件' }))
    await act(async () => pendingCreate.resolve(createdFixture()))

    await waitFor(() => expect(store.getSnapshot().has('vps_a')).toBe(false))
    expect(finishCreate).toHaveBeenCalledWith(expect.objectContaining({ vpsId: 'vps_a' }), 'confirmed')
    expect(refresh).not.toHaveBeenCalled()
    expect(screen.getByTestId('location')).toHaveTextContent('/vps/vps_a')
    expect(screen.queryByRole('link', { name: /接入 agent|监控列表/ })).not.toBeInTheDocument()
  })

  it('resets the draft after cancel and reopen', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture('vps_a'))
    renderHarness()

    const name = await openZeroLinkForm()
    fireEvent.change(name, { target: { value: '未保存名称' } })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '打开接入' })).toHaveFocus())
    const reopened = await openZeroLinkForm()

    expect(reopened).toHaveValue('东京边缘')
    expect(api.getVPSAsset).toHaveBeenCalledTimes(2)
  })

  it('does not leak post-close continuation feedback to another VPS', async () => {
    vi.spyOn(api, 'getVPSAsset').mockImplementation(async (vpsId) => detailFixture(vpsId))
    vi.spyOn(api, 'createVPSMonitoringInstance').mockResolvedValue(createdFixture())
    renderHarness({ onRefresh: async () => false })

    await openZeroLinkForm()
    await submitCreate()
    expect(await screen.findByRole('link', { name: '继续接入 agent' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))

    await waitFor(() => expect(screen.queryByRole('link', { name: '继续接入 agent' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))
    expect(screen.queryByRole('link', { name: '继续接入 agent' })).not.toBeInTheDocument()
  })
})
