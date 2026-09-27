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
      lifecycle_status: 'active',
      usage_tags: ['闲置'],
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
    lifecycle_status: 'active',
    usage_tags: ['闲置'],
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
    lifecycle_status: '已接入',
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
    lifecycle_status: '已接入',
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
  afterEach(() => vi.restoreAllMocks())

  const handlers = { onEditUsage: vi.fn(), onEditDecision: vi.fn(), onCreateMonitoring: vi.fn() }

  it('refreshes ownership after a VPS-scoped monitoring creation', async () => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    let created = false
    let readsAfterCreate = 0
    vi.spyOn(api, 'getVPSAsset').mockImplementation(async () => {
      if (created) readsAfterCreate += 1
      return detailFixture(created ? [monitoringLink('Tokyo Mon 1')] : [])
    })
    vi.spyOn(api, 'listVPSMonitoringInstances').mockImplementation(async () => created ? [monitoringLink('Tokyo Mon 1')] : [])
    const create = vi.spyOn(api, 'createVPSMonitoringInstance').mockImplementation(async () => {
      created = true
      return { ...monitoringRecord(), link: { link_id: 'link_1', vps_id: 'vps_001', monitoring_instance_id: 'mon_1', linked_at: '2026-08-20T00:00:00Z', note: '' } }
    })
    render(<MemoryRouter initialEntries={['/vps/vps_001?reorganize=1']}><Routes>
      <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
      <Route path="/monitoring/:id" element={<p>接入会话</p>} />
    </Routes></MemoryRouter>)
    await screen.findByText('当前没有监控实例或接入历史。')
    fireEvent.click(screen.getByRole('button', { name: '创建并接入' }))
    await screen.findByRole('textbox', { name: '监控实例名称' })
    fireEvent.click(screen.getByRole('button', { name: '接入/升级 agent' }))
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(readsAfterCreate).toBeGreaterThan(0))
    expect(create.mock.calls[0]?.[0]).toBe('vps_001')
  })

  it('ignores old generation responses after a newer monitoring refresh', async () => {
    const old = deferred<VPSMonitoringInstanceSummary[]>()
    const latest = deferred<VPSMonitoringInstanceSummary[]>()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    const list = vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValueOnce([]).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const view = render(<MemoryRouter><RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={0} {...handlers} /></MemoryRouter>)
    await screen.findByText('当前没有监控实例或接入历史。')
    view.rerender(<MemoryRouter><RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={1} {...handlers} /></MemoryRouter>)
    view.rerender(<MemoryRouter><RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={2} {...handlers} /></MemoryRouter>)
    await act(async () => latest.resolve([monitoringLink('Fresh Agent', 'mi_fresh')]))
    await screen.findByRole('link', { name: 'Fresh Agent' })
    await act(async () => old.resolve([monitoringLink('Stale Agent', 'mi_stale')]))
    expect(screen.queryByRole('link', { name: 'Stale Agent' })).not.toBeInTheDocument()
    expect(list).toHaveBeenLastCalledWith('vps_001', 'all')
  })

  it('keeps history retired and offers explicit reenrollment without unlink or cross-owner binding', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([{ ...monitoringLink('Old Agent'), lifecycle_status: '已退役', is_current: false }])
    render(<MemoryRouter><RestoreReorganizationPanel vpsId="vps_001" {...handlers} /></MemoryRouter>)
    const link = await screen.findByRole('link', { name: '重新接入历史实例' })
    expect(link).toHaveAttribute('href', '/monitoring/mon_1?onboarding=1&return_vps=vps_001')
    expect(screen.queryByRole('button', { name: /关联已有|解除旧/ })).not.toBeInTheDocument()
    expect(screen.getByText(/恢复后进入管理中/)).toBeInTheDocument()
  })

  it('does not offer historical reenrollment when another current instance exists', async () => {
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([monitoringLink('Current', 'mi_current'), { ...monitoringLink('Retired', 'mi_old'), lifecycle_status: '已退役' }])
    render(<MemoryRouter><RestoreReorganizationPanel vpsId="vps_001" {...handlers} /></MemoryRouter>)
    await screen.findByRole('link', { name: 'Current' })
    expect(screen.getByRole('link', { name: '重新接入当前实例' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '重新接入历史实例' })).not.toBeInTheDocument()
  })

  it('keeps a new read failure visible when an older refresh later succeeds', async () => {
    const old = deferred<VPSMonitoringInstanceSummary[]>()
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    vi.spyOn(api, 'listVPSMonitoringInstances').mockReturnValueOnce(old.promise).mockRejectedValueOnce(new ApiError(503, 'history unavailable'))
    const view = render(<MemoryRouter><RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={0} {...handlers} /></MemoryRouter>)
    view.rerender(<MemoryRouter><RestoreReorganizationPanel vpsId="vps_001" refreshGeneration={1} {...handlers} /></MemoryRouter>)
    await screen.findByText('history unavailable')
    await act(async () => old.resolve([monitoringLink('Stale Agent')]))
    expect(screen.getByText('history unavailable')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Stale Agent' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '创建并接入' })).toBeDisabled()
  })
})
