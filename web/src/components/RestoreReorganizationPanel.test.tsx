import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../lib/apiRequest'

import * as api from '../lib/api'
import * as recordsApi from '../lib/recordsApi'
import type { MonitoringInstanceRecord, VPSAssetDetail, VPSMonitoringInstanceSummary, VPSOverview } from '../lib/types'
import { VPSDetailPage } from '../pages/VPSDetailPage'
import { RestoreReorganizationPanel } from './RestoreReorganizationPanel'

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
      ipv4: '192.0.2.1',
      ipv6: '',
      lifecycle_status: 'idle',
      usage_status: 'unused',
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
    facts: [],
    relations: [],
    capabilities: ['records_v2_read'],
  }
}

function detailFixture(links: VPSMonitoringInstanceSummary[] = []): VPSAssetDetail {
  return {
    vps_id: 'vps_001',
    display_name: '东京边缘',
    provider_id: 'provider_001',
    provider_name: 'Example',
    product_name: 'VPS',
    order_ref: 'order_001',
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
    lifecycle_status: 'idle',
    usage_status: 'unknown',
    renewal_decision: 'keep',
    importance: 'high',
    labels: ['edge'],
    note: '',
    active_monitoring_instance_link_count: links.length,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    monitoring_instance_links: links,
  }
}

function monitoringLink(displayName: string, id = 'mon_1'): VPSMonitoringInstanceSummary {
  return {
    monitoring_instance_id: id,
    display_name: displayName,
    group: '',
    region: 'Tokyo',
    city: 'Tokyo',
    provider: 'AWS',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    current_health_status: '正常',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    linked_at: '2026-08-20T00:00:00Z',
    note: '',
  }
}

function monitoringRecord(): MonitoringInstanceRecord {
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
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('RestoreReorganizationPanel', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('reloads the panel after a successful monitoring association write', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    let linked = false
    let readsAfterLink = 0
    const getDetail = vi.spyOn(api, 'getVPSAsset').mockImplementation(async () => {
      if (linked) readsAfterLink += 1
      return linked ? detailFixture([monitoringLink('Tokyo Mon 1')]) : detailFixture()
    })
    vi.spyOn(api, 'listMonitoringInstances').mockResolvedValue([monitoringRecord()])
    const link = vi.spyOn(api, 'linkVPSMonitoringInstance').mockImplementation(async () => {
      linked = true
      return {
        link_id: 'link_1',
        vps_id: 'vps_001',
        monitoring_instance_id: 'mon_1',
        linked_at: '2026-08-20T00:00:00Z',
        note: '',
      }
    })

    render(
      <MemoryRouter initialEntries={['/vps/vps_001?reorganize=1']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByText('当前没有未解除的监控关联。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '关联已有监控' }))
    const select = await screen.findByRole('combobox', { name: '选择监控实例' })
    fireEvent.change(select, { target: { value: 'mon_1' } })
    fireEvent.click(screen.getByRole('button', { name: '关联监控实例' }))

    await waitFor(() => expect(link).toHaveBeenCalledTimes(1))
    expect(await screen.findByRole('link', { name: 'Tokyo Mon 1' })).toBeInTheDocument()
    expect(screen.queryByText('当前没有未解除的监控关联。')).not.toBeInTheDocument()
    expect(readsAfterLink).toBeGreaterThan(0)
    expect(getDetail).toHaveBeenCalledWith('vps_001')

  })

  it('does not let a stale detail response overwrite a newer association refresh', async () => {
    const first = deferred<VPSAssetDetail>()
    const second = deferred<VPSAssetDetail>()
    const third = deferred<VPSAssetDetail>()
    const getDetail = vi.spyOn(api, 'getVPSAsset')
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
      .mockReturnValueOnce(third.promise)
    const handlers = {
      onEditUsage: vi.fn(),
      onEditDecision: vi.fn(),
      onRelink: vi.fn(),
      onCreateMonitoring: vi.fn(),
      onChanged: vi.fn(),
    }

    const view = render(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={0} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      first.resolve(detailFixture())
    })
    expect(await screen.findByText('当前没有未解除的监控关联。')).toBeInTheDocument()

    view.rerender(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={1} {...handlers} />
      </MemoryRouter>,
    )
    view.rerender(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={2} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      second.resolve(detailFixture([monitoringLink('Stale Agent', 'mon_stale')]))
    })
    expect(screen.queryByRole('link', { name: 'Stale Agent' })).not.toBeInTheDocument()

    await act(async () => {
      third.resolve(detailFixture([monitoringLink('Fresh Agent', 'mon_fresh')]))
    })
    expect(getDetail).toHaveBeenCalledTimes(3)
  })

  it('closes the unlink dialog when a newer association refresh lands before unlink success', async () => {
    const first = deferred<VPSAssetDetail>()
    const refreshed = deferred<VPSAssetDetail>()
    const afterUnlink = deferred<VPSAssetDetail>()
    const unlinkResult = deferred<Awaited<ReturnType<typeof api.unlinkVPSMonitoringInstance>>>()
    const getDetail = vi.spyOn(api, 'getVPSAsset')
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(refreshed.promise)
      .mockReturnValueOnce(afterUnlink.promise)
    const unlink = vi.spyOn(api, 'unlinkVPSMonitoringInstance').mockReturnValue(unlinkResult.promise)
    const onChanged = vi.fn()
    const handlers = {
      onEditUsage: vi.fn(),
      onEditDecision: vi.fn(),
      onRelink: vi.fn(),
      onCreateMonitoring: vi.fn(),
      onChanged,
    }
    const view = render(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={0} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      first.resolve(detailFixture([monitoringLink('Old Agent', 'mon_old')]))
    })
    fireEvent.click(await screen.findByRole('button', { name: '解除旧关联' }))
    fireEvent.click(screen.getByRole('button', { name: '确认解除关联' }))
    await waitFor(() => expect(unlink).toHaveBeenCalledTimes(1))

    view.rerender(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={1} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      refreshed.resolve(detailFixture([monitoringLink('Fresh Agent', 'mon_fresh')]))
    })
    expect(await screen.findByRole('link', { name: 'Fresh Agent' })).toBeInTheDocument()

    const readsAtUnlinkSettle = getDetail.mock.calls.length
    await act(async () => {
      unlinkResult.resolve({
        link_id: 'link_old',
        vps_id: 'vps_001',
        monitoring_instance_id: 'mon_old',
        linked_at: '2026-08-10T00:00:00Z',
        note: '',
      })
    })

    expect(screen.queryByRole('alertdialog', { name: '确认解除监控实例关联' })).not.toBeInTheDocument()
    expect(onChanged).toHaveBeenCalledTimes(1)
    expect(getDetail.mock.calls.length).toBeGreaterThan(readsAtUnlinkSettle)
    await act(async () => {
      afterUnlink.resolve(detailFixture([monitoringLink('Fresh Agent', 'mon_fresh')]))
    })
    expect(screen.getByRole('link', { name: 'Fresh Agent' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认解除关联' })).not.toBeInTheDocument()
  })

  it('shows a late unlink failure after a newer association refresh', async () => {
    const first = deferred<VPSAssetDetail>()
    const refreshed = deferred<VPSAssetDetail>()
    const unlinkResult = deferred<Awaited<ReturnType<typeof api.unlinkVPSMonitoringInstance>>>()
    vi.spyOn(api, 'getVPSAsset')
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(refreshed.promise)
    vi.spyOn(api, 'unlinkVPSMonitoringInstance').mockReturnValue(unlinkResult.promise)
    const onChanged = vi.fn()
    const handlers = {
      onEditUsage: vi.fn(),
      onEditDecision: vi.fn(),
      onRelink: vi.fn(),
      onCreateMonitoring: vi.fn(),
      onChanged,
    }
    const view = render(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={0} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      first.resolve(detailFixture([monitoringLink('Old Agent', 'mon_old')]))
    })
    fireEvent.click(await screen.findByRole('button', { name: '解除旧关联' }))
    fireEvent.click(screen.getByRole('button', { name: '确认解除关联' }))
    await waitFor(() => expect(api.unlinkVPSMonitoringInstance).toHaveBeenCalledTimes(1))

    view.rerender(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={1} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      refreshed.resolve(detailFixture([monitoringLink('Fresh Agent', 'mon_fresh')]))
    })
    await act(async () => {
      unlinkResult.reject(new ApiError(409, 'unlink conflict'))
    })

    expect(await screen.findByText('unlink conflict')).toBeInTheDocument()
    expect(onChanged).not.toHaveBeenCalled()
    expect(screen.getByRole('link', { name: 'Fresh Agent' })).toBeInTheDocument()
  })

  it('keeps a late unlink failure when an older in-flight association refresh resolves afterwards', async () => {
    const first = deferred<VPSAssetDetail>()
    const refreshed = deferred<VPSAssetDetail>()
    const unlinkResult = deferred<Awaited<ReturnType<typeof api.unlinkVPSMonitoringInstance>>>()
    vi.spyOn(api, 'getVPSAsset')
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(refreshed.promise)
    vi.spyOn(api, 'unlinkVPSMonitoringInstance').mockReturnValue(unlinkResult.promise)
    const onChanged = vi.fn()
    const handlers = {
      onEditUsage: vi.fn(),
      onEditDecision: vi.fn(),
      onRelink: vi.fn(),
      onCreateMonitoring: vi.fn(),
      onChanged,
    }
    const view = render(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={0} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      first.resolve(detailFixture([monitoringLink('Old Agent', 'mon_old')]))
    })
    fireEvent.click(await screen.findByRole('button', { name: '解除旧关联' }))
    fireEvent.click(screen.getByRole('button', { name: '确认解除关联' }))
    await waitFor(() => expect(api.unlinkVPSMonitoringInstance).toHaveBeenCalledTimes(1))

    view.rerender(
      <MemoryRouter>
        <RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={1} {...handlers} />
      </MemoryRouter>,
    )
    await act(async () => {
      unlinkResult.reject(new ApiError(409, 'unlink conflict'))
    })
    expect(await screen.findByText('unlink conflict')).toBeInTheDocument()
    await act(async () => {
      refreshed.resolve(detailFixture([monitoringLink('Old Agent', 'mon_old'), monitoringLink('Fresh Agent', 'mon_fresh')]))
    })

    expect(screen.getByRole('link', { name: 'Fresh Agent' })).toBeInTheDocument()
    expect(screen.getByText('unlink conflict')).toBeInTheDocument()
    expect(screen.getByRole('alertdialog', { name: '确认解除监控实例关联' })).toBeInTheDocument()
    expect(onChanged).not.toHaveBeenCalled()
  })
})
