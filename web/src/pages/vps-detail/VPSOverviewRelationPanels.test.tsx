import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'

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
  monitoring_status: '启用',
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
    expect(await screen.findByRole('dialog', { name: '已关联监控实例' })).toBeInTheDocument()
    expect(screen.getByText('东京监控')).toBeInTheDocument()
    expect(screen.getByText('mi_001')).toBeInTheDocument()
    expect(screen.getByText('监控配置')).toBeInTheDocument()
    expect(screen.getByText('监控配置').tagName).toBe('DT')
    expect(screen.getByText('启用')).toBeInTheDocument()
    expect(screen.getByText('观测健康')).toBeInTheDocument()
    expect(screen.getByText('观测健康').tagName).not.toBe('DT')
    expect(screen.queryByRole('heading', { name: '监控观测' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看监控实例' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    expect(api.listVPSMonitoringInstances).toHaveBeenCalledWith('vps_001')
    expect(screen.queryByRole('button', { name: /接入\/升级 agent/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '解除关联' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '打开服务关系' }))
    expect(await screen.findByRole('dialog', { name: '已关联服务' })).toBeInTheDocument()
    expect(screen.getByText('Gateway')).toBeInTheDocument()
    expect(screen.getByText('入口探测')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '服务资产' })).not.toBeInTheDocument()
    expect(api.listVPSServices).toHaveBeenCalledWith('vps_001')
    expect(screen.queryByRole('button', { name: '新增服务' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))

    fireEvent.click(screen.getByRole('button', { name: '打开域名关系' }))
    expect(await screen.findByText('edge.example.com')).toBeInTheDocument()
    expect(api.listVPSDomains).toHaveBeenCalledWith('vps_001')
    expect(api.listVPSServices).toHaveBeenCalledTimes(2)
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
    expect(await screen.findByRole('dialog', { name: '已关联服务' })).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog', { name: '已关联服务' })).not.toBeInTheDocument())
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

  it('keeps complete service entries and valid actions for missing and non-HTTP endpoints', async () => {
    vi.spyOn(api, 'listVPSServices').mockResolvedValue([
      {
        ...SERVICE,
        port: 443,
        target_id: 'tg_001',
        labels: ['edge'],
        note: 'prod',
      },
      {
        ...SERVICE,
        service_id: 'svc_empty',
        name: 'Empty Gateway',
        url: '',
        port: null,
        labels: [],
        note: '',
      },
      {
        ...SERVICE,
        service_id: 'svc_grpc',
        name: 'Long Stream',
        url: 'grpc://stream.example.invalid/very/long/path:50051',
        port: 50051,
        labels: [],
        note: '',
      },
    ])
    renderHarness()
    fireEvent.click(screen.getByRole('button', { name: '打开服务关系' }))
    const dialog = await screen.findByRole('dialog', { name: '已关联服务' })
    const rows = within(dialog).getAllByRole('listitem')
    expect(rows).toHaveLength(3)

    const httpRow = within(rows[0] as HTMLElement)
    expect(httpRow.getByText('Web')).toBeInTheDocument()
    expect(httpRow.getByText('443')).toBeInTheDocument()
    expect(httpRow.getByText('https://example.invalid')).toBeInTheDocument()
    expect(httpRow.getByRole('link', { name: '打开入口' })).toHaveAttribute('href', 'https://example.invalid')
    expect(httpRow.getByRole('button', { name: '复制入口' })).toBeInTheDocument()
    expect(httpRow.getByRole('link', { name: 'tg_001' })).toHaveAttribute('href', '/targets/tg_001')

    const emptyRow = within(rows[1] as HTMLElement)
    expect(emptyRow.queryByRole('button', { name: '复制入口' })).not.toBeInTheDocument()

    const grpcRow = within(rows[2] as HTMLElement)
    expect(grpcRow.getByText('grpc://stream.example.invalid/very/long/path:50051')).toBeInTheDocument()
    expect(grpcRow.queryByRole('link', { name: /grpc:/ })).not.toBeInTheDocument()
    expect(grpcRow.getByRole('button', { name: '复制入口' })).toBeInTheDocument()
    expect(grpcRow.getByText('50051')).toBeInTheDocument()
  })

  it('shows domain records and IDs when optional service lookup fails', async () => {
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([{ ...DOMAIN, service_id: 'svc_001' }])
    vi.spyOn(api, 'listVPSServices').mockRejectedValue(new Error('service catalog offline'))
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([])
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开域名关系' }))
    const dialog = await screen.findByRole('dialog', { name: '已关联域名' })
    expect(await screen.findByText('edge.example.com')).toBeInTheDocument()
    expect(within(dialog).getByText('svc_001')).toBeInTheDocument()
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()
    expect(api.listVPSDomains).toHaveBeenCalledWith('vps_001')
    expect(api.listVPSServices).toHaveBeenCalledWith('vps_001')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重试加载域名' })).not.toBeInTheDocument()
  })

  it('fills a readable service name beside the domain association ID when metadata arrives', async () => {
    const pendingServices = deferred<AssetServiceRecord[]>()
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([{ ...DOMAIN, service_id: 'svc_001' }])
    vi.spyOn(api, 'listVPSServices').mockReturnValue(pendingServices.promise)
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([])
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开域名关系' }))
    expect(await screen.findByText('svc_001')).toBeInTheDocument()
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()

    await act(async () => pendingServices.resolve([SERVICE]))
    expect(screen.getByText('Gateway')).toBeInTheDocument()
    expect(screen.getByText('svc_001')).toBeInTheDocument()
  })

  it('keeps domain load errors and retry when service metadata would succeed', async () => {
    vi.spyOn(api, 'listVPSDomains')
      .mockRejectedValueOnce(new Error('domain catalog offline'))
      .mockResolvedValueOnce([{ ...DOMAIN, service_id: 'svc_001' }])
    vi.spyOn(api, 'listVPSServices').mockResolvedValue([SERVICE])
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([])
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开域名关系' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('domain catalog offline')
    expect(screen.queryByText('edge.example.com')).not.toBeInTheDocument()
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试加载域名' }))
    expect(await screen.findByText('edge.example.com')).toBeInTheDocument()
    expect(screen.getByText('Gateway')).toBeInTheDocument()
    expect(screen.getByText('svc_001')).toBeInTheDocument()
  })

  it('does not apply stale service metadata after VPS identity changes', async () => {
    const pendingServices = deferred<AssetServiceRecord[]>()
    const nextDomains = deferred<AssetDomainRecord[]>()
    vi.spyOn(api, 'listVPSDomains')
      .mockResolvedValueOnce([{ ...DOMAIN, service_id: 'svc_001' }])
      .mockReturnValueOnce(nextDomains.promise)
    vi.spyOn(api, 'listVPSServices')
      .mockReturnValueOnce(pendingServices.promise)
      .mockResolvedValueOnce([])
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([])
    renderHarness()

    fireEvent.click(screen.getByRole('button', { name: '打开域名关系' }))
    expect(await screen.findByText('edge.example.com')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '切换 VPS' }))
    expect(screen.queryByText('edge.example.com')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('正在加载域名')
    expect(api.listVPSDomains).toHaveBeenLastCalledWith('vps_002')

    await act(async () => pendingServices.resolve([SERVICE]))
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()
    expect(screen.queryByText('edge.example.com')).not.toBeInTheDocument()

    await act(async () => nextDomains.resolve([]))
    expect(await screen.findByText('尚未记录域名')).toBeInTheDocument()
    expect(screen.queryByText('Gateway')).not.toBeInTheDocument()
  })

})
