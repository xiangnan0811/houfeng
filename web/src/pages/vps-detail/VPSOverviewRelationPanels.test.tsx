import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as api from '../../lib/api'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  VPSMonitoringInstanceSummary,
} from '../../lib/types'
import { useVPSManagementController } from './hooks/useVPSManagementController'
import { VPSOverviewRelationPanels } from './VPSOverviewRelationPanels'

const MONITORING: VPSMonitoringInstanceSummary = {
  monitoring_instance_id: 'mi_001',
  display_name: '东京监控',
  group: 'edge',
  region: 'Tokyo',
  city: 'Tokyo',
  provider: 'Example',
  lifecycle_status: 'active',
  monitoring_status: 'active',
  binding_status: 'bound',
  current_health_status: '正常',
  last_heartbeat_at: '2026-08-24T00:00:00Z',
  current_active_incident_count: 0,
  current_primary_issue_summary: '',
  linked_at: '2026-08-01T00:00:00Z',
  note: '',
}

const SERVICE: AssetServiceRecord = {
  service_id: 'svc_001',
  vps_id: 'vps_001',
  name: 'Gateway',
  service_type: 'web',
  status: 'active',
  url: 'https://example.invalid',
  labels: [],
  note: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z',
}

const DOMAIN: AssetDomainRecord = {
  domain_id: 'domain_001',
  vps_id: 'vps_001',
  domain_name: 'edge.example.com',
  purpose: 'gateway',
  status: 'active',
  registrar: 'Example',
  auto_renew: true,
  https_enabled: true,
  labels: [],
  note: '',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z',
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function Harness() {
  const management = useVPSManagementController()
  const [vpsId, setVpsId] = useState('vps_001')
  const relationPanelOpen = management.panel === 'monitoring-instance-evidence'
    || management.panel === 'services-detail'
    || management.panel === 'domains-detail'
  return (
    <>
      <button type="button" onClick={() => management.openPanel('monitoring-instance-evidence')}>
        打开监控关系
      </button>
      <button type="button" onClick={() => management.openPanel('services-detail')}>
        打开服务关系
      </button>
      <button type="button" onClick={() => management.openPanel('domains-detail')}>
        打开域名关系
      </button>
      <button type="button" onClick={() => setVpsId('vps_002')}>切换 VPS</button>
      {relationPanelOpen ? (
        <VPSOverviewRelationPanels
          key={`${vpsId}:${management.panel}`}
          vpsId={vpsId}
          management={management}
        />
      ) : null}
    </>
  )
}

function renderHarness() {
  return render(
    <MemoryRouter>
      <Harness />
    </MemoryRouter>,
  )
}

describe('VPSOverviewRelationPanels', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads each VPS-scoped API only when its read-only panel opens', async () => {
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([MONITORING])
    vi.spyOn(api, 'listVPSServices').mockResolvedValue([SERVICE])
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([DOMAIN])
    renderHarness()

    expect(api.listVPSMonitoringInstances).not.toHaveBeenCalled()
    expect(api.listVPSServices).not.toHaveBeenCalled()
    expect(api.listVPSDomains).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '打开监控关系' }))
    expect(await screen.findByText('东京监控')).toBeInTheDocument()
    expect(api.listVPSMonitoringInstances).toHaveBeenCalledWith('vps_001')
    expect(screen.queryByRole('button', { name: /接入\/升级 agent/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '解除关联' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '打开服务关系' }))
    expect(await screen.findByText('Gateway')).toBeInTheDocument()
    expect(api.listVPSServices).toHaveBeenCalledWith('vps_001')
    expect(screen.queryByRole('button', { name: '新增服务' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '打开域名关系' }))
    expect(await screen.findByText('edge.example.com')).toBeInTheDocument()
    expect(api.listVPSDomains).toHaveBeenCalledWith('vps_001')
    expect(screen.queryByRole('button', { name: '新增域名' })).not.toBeInTheDocument()
  })

  it('bounds loading, exposes errors, and retries only the active relation', async () => {
    const pending = deferred<AssetServiceRecord[]>()
    const services = vi.spyOn(api, 'listVPSServices')
      .mockReturnValueOnce(pending.promise)
      .mockRejectedValueOnce(new Error('service catalog offline'))
      .mockResolvedValueOnce([])
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([])
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([])
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开服务关系' }))
    expect(screen.getByRole('status')).toHaveTextContent('正在加载服务')
    await act(async () => pending.resolve([]))
    expect(await screen.findByText('尚未记录服务')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    fireEvent.click(screen.getByRole('button', { name: '打开服务关系' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('service catalog offline')
    fireEvent.click(screen.getByRole('button', { name: '重试加载服务' }))
    expect(await screen.findByText('尚未记录服务')).toBeInTheDocument()
    expect(services).toHaveBeenCalledTimes(3)
    expect(api.listVPSMonitoringInstances).not.toHaveBeenCalled()
    expect(api.listVPSDomains).not.toHaveBeenCalled()
  })

  it('returns focus to the relation trigger after Escape closes the modal', async () => {
    vi.spyOn(api, 'listVPSServices').mockResolvedValue([])
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([])
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([])
    renderHarness()

    const trigger = screen.getByRole('button', { name: '打开服务关系' })
    trigger.focus()
    fireEvent.click(trigger)
    expect(await screen.findByRole('dialog', { name: '关联服务' })).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '关联服务' })).not.toBeInTheDocument())
    await waitFor(() => expect(trigger).toHaveFocus())
  })

  it('returns focus after the close button and never carries relation data across VPS identity', async () => {
    const nextServices = deferred<AssetServiceRecord[]>()
    vi.spyOn(api, 'listVPSServices')
      .mockResolvedValueOnce([SERVICE])
      .mockResolvedValueOnce([SERVICE])
      .mockReturnValueOnce(nextServices.promise)
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([])
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([])
    renderHarness()

    const trigger = screen.getByRole('button', { name: '打开服务关系' })
    trigger.focus()
    fireEvent.click(trigger)
    expect(await screen.findByText('Gateway')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    await waitFor(() => expect(trigger).toHaveFocus())

    fireEvent.click(trigger)
    expect(await screen.findByText('Gateway')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('正在加载服务')
    expect(api.listVPSServices).toHaveBeenLastCalledWith('vps_002')

    await act(async () => nextServices.resolve([]))
    expect(await screen.findByText('尚未记录服务')).toBeInTheDocument()
  })
})
