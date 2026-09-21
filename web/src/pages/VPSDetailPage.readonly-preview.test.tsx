import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import * as api from '../lib/api'
import * as recordsApi from '../lib/recordsApi'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  VPSAssetDetail,
  VPSMonitoringInstanceSummary,
  VPSOverview,
} from '../lib/types'
import { VPSDetailPage } from './VPSDetailPage'

vi.mock('../lib/readOnlyPreview', () => ({
  READ_ONLY_PREVIEW: true,
}))

vi.mock('./vps-detail/LegacyVPSDetail', () => ({
  LegacyVPSDetail: () => <div>Legacy VPS detail shell</div>,
}))

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
      ipv4: '192.0.2.10',
      ipv6: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
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
    facts: [{ key: 'ipv4', label: 'IPv4', value: '192.0.2.10' }],
    relations: [
      {
        kind: 'monitoring_instances',
        count: 1,
        label: '监控实例',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      },
      {
        kind: 'services',
        count: 1,
        label: '服务',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      },
      {
        kind: 'domains',
        count: 1,
        label: '域名',
        section: { state: 'ready', observed_at: null, last_success_at: null, reason_code: '' },
      },
    ],
    capabilities: ['records_v2_read'],
  }
}

function detailFixture(): VPSAssetDetail {
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
    ipv4: '192.0.2.10',
    ipv6: '',
    ssh_host: '192.0.2.10',
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
    active_monitoring_instance_link_count: 1,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-20T00:00:00Z',
    monitoring_instance_links: [],
  }
}

function serviceFixture(): AssetServiceRecord {
  return {
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
}

function domainFixture(): AssetDomainRecord {
  return {
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
}

function monitoringFixture(): VPSMonitoringInstanceSummary {
  return {
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
}

describe('VPSDetailPage readonly preview workbench', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  beforeEach(() => {
    vi.spyOn(recordsApi, 'getVPSOverview').mockResolvedValue(overviewFixture())
    vi.spyOn(api, 'listSubscriptions').mockResolvedValue([])
    vi.spyOn(api, 'listVPSServices').mockResolvedValue([serviceFixture()])
    vi.spyOn(api, 'listVPSDomains').mockResolvedValue([domainFixture()])
    vi.spyOn(api, 'listVPSMonitoringInstances').mockResolvedValue([monitoringFixture()])
    vi.spyOn(api, 'getVPSAsset').mockResolvedValue(detailFixture())
    vi.spyOn(api, 'createVPSService')
    vi.spyOn(api, 'createVPSDomain')
    vi.spyOn(api, 'unlinkVPSMonitoringInstance')
    vi.spyOn(api, 'issueMonitoringInstanceInstallCommand')
  })

  it.each(['subscription', 'monitoring', 'monitoring-instance-create', 'cancellation'] as const)(
    'consumes workbench=%s without opening an enabled write panel',
    async (workbench) => {
      render(
        <MemoryRouter initialEntries={[`/vps/vps_001?workbench=${workbench}`]}>
          <Routes>
            <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
          </Routes>
        </MemoryRouter>,
      )

      expect(await screen.findByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
      expect(screen.getByText('只读预览')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: '管理' })).not.toBeInTheDocument()
      expect(screen.queryByRole('link', { name: '新建记录' })).not.toBeInTheDocument()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
      expect(screen.queryByRole('textbox', { name: '原因' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: '复制IPv4' })).toBeEnabled()
      await waitFor(() => expect(api.getVPSAsset).not.toHaveBeenCalled())
    },
  )

  it.each([
    { button: '查看服务', dialog: '已关联服务', forbidden: '新增服务' },
    { button: '查看域名', dialog: '已关联域名', forbidden: '新增域名' },
    { button: '查看实例', dialog: '已关联监控实例', forbidden: /接入\/升级 agent/ },
  ] as const)('opens $button as a read-only relation panel', async ({ button, dialog, forbidden }) => {
    render(
      <MemoryRouter initialEntries={['/vps/vps_001']}>
        <Routes>
          <Route path="/vps/:vpsId" element={<VPSDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: '东京边缘' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: button }))
    expect(await screen.findByRole('dialog', { name: dialog })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: forbidden })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '解除关联' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '关联已有监控实例' })).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '新增服务' })).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: '新增域名' })).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '监控实例名称' })).not.toBeInTheDocument()
    expect(api.createVPSService).not.toHaveBeenCalled()
    expect(api.createVPSDomain).not.toHaveBeenCalled()
    expect(api.unlinkVPSMonitoringInstance).not.toHaveBeenCalled()
    expect(api.issueMonitoringInstanceInstallCommand).not.toHaveBeenCalled()
  })
})
