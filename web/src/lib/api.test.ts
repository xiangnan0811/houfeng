import { afterEach, describe, expect, it, vi } from 'vitest'
import { matchRoutes } from 'react-router-dom'

import type { ApplyCancellationInput } from './types'
import {
  applyVPSCancellation,
  archiveTarget,
  createAssetDomain,
  confirmMonitoringInstanceRebind,
  createAssetService,
  createProbeItem,
  createProvider,
  createSubscription,
  createTarget,
  createVPSAsset,
  createVPSExperienceLog,
  createVPSService,
  deleteProbeItem,
  enterMonitoringInstanceMaintenance,
  enterTargetMaintenance,
  exitMonitoringInstanceMaintenance,
  exitTargetMaintenance,
  getDashboard,
  getMonitoringInstanceOnboarding,
  getProvider,
  getSettings,
  getSubscription,
  getVPSAsset,
  getVPSCancellationPreview,
  getVPSTimeline,
  issueMonitoringInstanceInstallCommand,
  linkVPSMonitoringInstance,
  listAssetDomains,
  listAssetServices,
  listVPSDomains,
  listVPSExperienceLogs,
  listProviders,
  listMonitoringInstances,
  listEvents,
  listIncidents,
  listSubscriptions,
  listMonitoringInstanceAssetContexts,
  listTargetAssetContexts,
  pauseMonitoringInstanceMonitoring,
  pauseTarget,
  rejectPendingMonitoringInstanceBinding,
  restoreRetiredMonitoringInstanceToObserving,
  resetMonitoringInstanceBinding,
  retireMonitoringInstance,
  restoreTargetToPaused,
  resumeMonitoringInstanceMonitoring,
  resumeTarget,
  postMonitoringInstanceAction,
  unlinkVPSMonitoringInstance,
  updateMonitoringInstanceMetadata,
  updateProbeItem,
  updateProvider,
  updateSettings,
  updateSubscription,
  updateTargetMetadata,
  updateVPSAsset,
  listVPSAssets,
  listVPSForMonitoringInstance,
  listVPSMonitoringInstances,
  listVPSServices,
  createVPSDomain,
} from './api'
import type {
  AssetDomainListFilter,
  AssetDomainRecord,
  AssetServiceRecord,
  CreateAssetDomainInput,
  CreateAssetServiceInput,
  CreateProviderInput,
  CreateProbeItemInput,
  CreateSubscriptionInput,
  CreateTargetInput,
  CreateVPSAssetInput,
  CreateVPSExperienceLogInput,
  MonitoringInstanceRecord,
  ProbeItemRecord,
  ProviderRecord,
  SettingsRecord,
  SettingsUpdateInput,
  SubscriptionRecord,
  TargetRecord,
  AssetServiceListFilter,
  UpdateMonitoringInstanceMetadataInput,
  UpdateProbeItemInput,
  UpdateProviderInput,
  UpdateSubscriptionInput,
  UpdateTargetMetadataInput,
  VPSAssetRecord,
  VPSExperienceLogRecord,
  VPSMonitoringInstanceLinkRecord,
  VPSTimeline,
} from './types'
import { appRoutes } from '../app/router'

function mockResponse(status: number, body: string) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => body,
    json: async () => JSON.parse(body),
  } as Response
}

const settingsResponseBody = {
  telegram: {
    chat_id: 'chat-id',
    token_present: true,
    token_masked_summary: '****************oken',
    runtime_managed: false,
    runtime_apply_active: false,
  },
  feishu: {
    enabled: false,
    webhook_url: '',
  },
  host_sample_frequency_tier: '5s',
  probe_frequency_defaults: {
    tcp: '5s',
    http: '5s',
    tls: '6h',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 3,
    sweep_interval_seconds: 5,
    notify_on_started: true,
    notify_on_escalated: true,
    notify_on_recovered: true,
    cpu_warning_pct: 80,
    cpu_alert_pct: 90,
    cpu_critical_pct: 95,
    mem_warning_pct: 85,
    mem_alert_pct: 92,
    mem_critical_pct: 95,
    disk_warning_pct: 85,
    disk_alert_pct: 92,
    disk_critical_pct: 97,
    inode_warning_pct: 80,
    inode_alert_pct: 90,
    inode_critical_pct: 95,
    iowait_warning_pct: 20,
    iowait_critical_pct: 50,
    load5_warning: 4.0,
    load5_critical: 8.0,
  },
  override_rules: {
    monitoring_instance_labels: [
      {
        label: 'edge',
        overrides: {
          host_sample_frequency_tier: '1m',
          probe_frequency_defaults: { http: '1m' },
        },
      },
    ],
    target_types: [
      {
        target_type: 'http',
        overrides: {
          incident_defaults: { stale_threshold_intervals: 4 },
        },
      },
    ],
    target_labels: [
      {
        label: 'external',
        overrides: {
          probe_frequency_defaults: { tls: '30m' },
        },
      },
    ],
  },
  retention_policy: {
    raw_layer_days: 7,
    aggregate_layer_days: 30,
    event_layer_days: 90,
    notification_layer_days: 180,
  },
} satisfies SettingsRecord

const settingsUpdateBody = {
  telegram: {
    bot_token: 'bot-token',
    chat_id: 'chat-id',
  },
  feishu: {
    enabled: false,
    webhook_url: '',
  },
  host_sample_frequency_tier: '5s',
  probe_frequency_defaults: {
    tcp: '5s',
    http: '5s',
    tls: '6h',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 3,
    sweep_interval_seconds: 5,
    notify_on_started: true,
    notify_on_escalated: true,
    notify_on_recovered: true,
    cpu_warning_pct: 80,
    cpu_alert_pct: 90,
    cpu_critical_pct: 95,
    mem_warning_pct: 85,
    mem_alert_pct: 92,
    mem_critical_pct: 95,
    disk_warning_pct: 85,
    disk_alert_pct: 92,
    disk_critical_pct: 97,
    inode_warning_pct: 80,
    inode_alert_pct: 90,
    inode_critical_pct: 95,
    iowait_warning_pct: 20,
    iowait_critical_pct: 50,
    load5_warning: 4.0,
    load5_critical: 8.0,
  },
  override_rules: {
    monitoring_instance_labels: [
      {
        label: 'edge',
        overrides: {
          host_sample_frequency_tier: '1m',
          probe_frequency_defaults: { http: '1m' },
        },
      },
    ],
    target_types: [
      {
        target_type: 'http',
        overrides: {
          incident_defaults: { stale_threshold_intervals: 4 },
        },
      },
    ],
    target_labels: [
      {
        label: 'external',
        overrides: {
          probe_frequency_defaults: { tls: '30m' },
        },
      },
    ],
  },
  retention_policy: {
    raw_layer_days: 7,
    aggregate_layer_days: 30,
    event_layer_days: 90,
    notification_layer_days: 180,
  },
} satisfies SettingsUpdateInput

describe('api helpers', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('surfaces plain-text non-JSON error bodies as ApiError messages', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockResponse(502, 'upstream unavailable')))

    await expect(listMonitoringInstances()).rejects.toMatchObject({
      name: 'ApiError',
      status: 502,
      message: 'upstream unavailable',
    })
  })

  it('surfaces JSON error bodies as ApiError messages', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(mockResponse(404, JSON.stringify({ error: 'monitoring instance not found' }))),
    )

    await expect(listMonitoringInstances()).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
      message: 'monitoring instance not found',
    })
  })

  it('loads dashboard overview from /api/dashboard', async () => {
    const responseBody = {
      snapshot_generated_at: '2026-04-25T08:30:00Z',
      total_monitoring_instance_count: 5,
      total_target_count: 4,
      abnormal_monitoring_instance_count: 1,
      abnormal_target_count: 2,
      severe_monitoring_instance_count: 0,
      severe_target_count: 1,
      maintenance_monitoring_instance_count: 1,
      maintenance_target_count: 0,
      pending_onboarding_monitoring_instance_count: 1,
      paused_monitoring_instance_count: 1,
      retired_monitoring_instance_count: 0,
      paused_target_count: 1,
      archived_target_count: 0,
      recent_new_incident_count: 3,
      recent_recovery_count: 2,
      group_summaries: [
        {
          group: 'production',
          monitoring_instance_count: 3,
          target_count: 2,
          abnormal_monitoring_instance_count: 1,
          abnormal_target_count: 2,
          severe_monitoring_instance_count: 0,
          severe_target_count: 1,
          maintenance_monitoring_instance_count: 1,
          maintenance_target_count: 0,
        },
      ],
      notification_status: {
        telegram_configured: true,
        telegram_runtime_managed: true,
        telegram_runtime_apply_active: true,
        feishu_configured: false,
      },
      asset_summary: {
        renewal_due_30d_subscription_count: 3,
        renewal_due_30d_vps_count: 2,
        unreviewed_vps_count: 4,
        to_cancel_vps_count: 1,
        cancelled_vps_count: 2,
        cancellation_attention_vps_count: 3,
        running_cancelled_asset_count: 4,
        to_migrate_vps_count: 2,
        unlinked_vps_count: 5,
        abnormal_linked_vps_count: 1,
        cost_by_currency: [
          { currency: 'USD', monthly_total: 42.5, yearly_total: 510 },
        ],
      },
      recent_events: [],
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getDashboard()).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('loads, creates, and updates providers through /api/providers', async () => {
    const provider = {
      provider_id: 'pv_001',
      name: 'Hetzner',
      website: 'https://hetzner.com',
      panel_url: 'https://console.hetzner.cloud',
      account_hint: 'main',
      country: 'DE',
      note: '',
      rating: 5,
      labels: ['core'],
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    } satisfies ProviderRecord
    const input = {
      name: 'Hetzner',
      website: 'https://hetzner.com',
      panel_url: 'https://console.hetzner.cloud',
      account_hint: 'main',
      country: 'DE',
      note: '',
      rating: 5,
      labels: ['core'],
    } satisfies CreateProviderInput
    const patchBody = {
      name: 'Hetzner Cloud',
      rating: null,
      labels: ['core', 'backup'],
    } satisfies UpdateProviderInput
    const updatedProvider = {
      ...provider,
      name: 'Hetzner Cloud',
      rating: null,
      labels: ['core', 'backup'],
      updated_at: '2026-05-09T09:00:00Z',
    } satisfies ProviderRecord
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([provider])))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(provider)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(provider)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(updatedProvider)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listProviders()).resolves.toEqual([provider])
    await expect(getProvider('pv_001')).resolves.toEqual(provider)
    await expect(createProvider(input)).resolves.toEqual(provider)
    await expect(updateProvider('pv_001', patchBody)).resolves.toEqual(updatedProvider)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/providers', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/providers/pv_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/providers', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/providers/pv_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(patchBody),
    })
  })

  it('serializes VPS asset filters and creates VPS assets', async () => {
    const vps = {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_id: 'pv_001',
      provider_name: 'Hetzner',
      product_name: 'cx22',
      order_ref: '',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: '',
      ipv4: '',
      ipv6: '',
      ssh_host: '',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: '',
      virtualization: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'normal',
      labels: ['edge'],
      note: '',
      active_monitoring_instance_link_count: 1,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
      archived_at: null,
    } satisfies VPSAssetRecord
    const input = {
      display_name: 'Tokyo Edge',
      provider_id: 'pv_001',
      provider_name: 'Hetzner',
      product_name: 'cx22',
      order_ref: '',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: '',
      ipv4: '',
      ipv6: '',
      ssh_host: '',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: '',
      virtualization: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'keep',
      importance: 'normal',
      labels: ['edge'],
      note: '',
    } satisfies CreateVPSAssetInput
    const detail = { ...vps, monitoring_instance_links: [] }
    const timeline = {
      vps_id: 'vps_001',
      renewal_decisions: [],
      price_histories: [],
      ip_histories: [],
      spec_snapshots: [],
      experience_logs: [],
    } satisfies VPSTimeline
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([vps])))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(detail)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(timeline)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(vps)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listVPSAssets({ provider_id: 'pv_001', lifecycle_status: 'active', usage_status: 'in_use', renewal_decision: 'keep' })).resolves.toEqual([vps])
    await expect(getVPSAsset('vps_001')).resolves.toEqual(detail)
    await expect(getVPSTimeline('vps_001')).resolves.toEqual(timeline)
    await expect(createVPSAsset(input)).resolves.toEqual(vps)

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/vps?provider_id=pv_001&lifecycle_status=active&usage_status=in_use&renewal_decision=keep',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/vps/vps_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
  })

  it('serializes VPS experience log operations', async () => {
    const logRecord = {
      experience_log_id: 'elog_001',
      vps_id: 'vps_001',
      category: 'network',
      severity: 'warning',
      summary: 'packet loss',
      details: 'opened provider ticket',
      occurred_at: '2026-05-10T09:30:00Z',
      created_at: '2026-05-10T09:31:00Z',
    } satisfies VPSExperienceLogRecord
    const input = {
      category: 'network',
      severity: 'warning',
      summary: 'packet loss',
      details: 'opened provider ticket',
      occurred_at: '2026-05-10T09:30:00Z',
    } satisfies CreateVPSExperienceLogInput
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([logRecord])))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(logRecord)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listVPSExperienceLogs('vps_001')).resolves.toEqual([logRecord])
    await expect(createVPSExperienceLog('vps_001', input)).resolves.toEqual(logRecord)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_001/experience-logs', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/vps/vps_001/experience-logs', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
  })

  it('serializes asset service collection and VPS-scoped operations', async () => {
    const service = {
      service_id: 'svc_001',
      vps_id: 'vps_001',
      target_id: 'tg_001',
      name: 'Blog',
      service_type: 'web',
      status: 'active',
      url: 'https://example.com',
      port: 443,
      labels: ['prod'],
      note: 'primary',
      created_at: '2026-05-10T08:00:00Z',
      updated_at: '2026-05-10T08:00:00Z',
    } satisfies AssetServiceRecord
    const filter = {
      vps_id: 'vps_001',
      target_id: 'tg_001',
      service_type: 'web',
      status: 'active',
    } satisfies AssetServiceListFilter
    const collectionInput = {
      vps_id: 'vps_001',
      target_id: 'tg_001',
      name: 'Blog',
      service_type: 'web',
      status: 'active',
      url: 'https://example.com',
      port: 443,
      labels: ['prod'],
      note: 'primary',
    } satisfies CreateAssetServiceInput
    const vpsScopedInput = {
      vps_id: 'vps_body',
      target_id: null,
      name: 'Worker',
      service_type: 'worker',
      status: 'active',
      url: '',
      port: null,
      labels: ['jobs'],
      note: 'queue',
    } satisfies CreateAssetServiceInput
    const expectedVPSScopedBody = {
      target_id: null,
      name: 'Worker',
      service_type: 'worker',
      status: 'active',
      url: '',
      port: null,
      labels: ['jobs'],
      note: 'queue',
    } satisfies Omit<CreateAssetServiceInput, 'vps_id'>
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([service])))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(service)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([service])))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify({ ...service, name: 'Worker', service_type: 'worker' })))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listAssetServices(filter)).resolves.toEqual([service])
    await expect(createAssetService(collectionInput)).resolves.toEqual(service)
    await expect(listVPSServices('vps_001')).resolves.toEqual([service])
    await expect(createVPSService('vps_001', vpsScopedInput)).resolves.toMatchObject({
      service_id: 'svc_001',
      name: 'Worker',
      service_type: 'worker',
    })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/services?vps_id=vps_001&target_id=tg_001&service_type=web&status=active',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/services', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(collectionInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_001/services', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps/vps_001/services', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(expectedVPSScopedBody),
    })
  })

  it('serializes asset domain collection and VPS-scoped operations', async () => {
    const domain = {
      domain_id: 'dom_001',
      vps_id: 'vps_001',
      service_id: 'svc_001',
      target_id: 'tg_001',
      domain_name: 'www.example.com',
      purpose: 'site',
      status: 'active',
      registrar: 'NameSilo',
      expires_at: '2026-07-01',
      auto_renew: true,
      https_enabled: true,
      labels: ['prod'],
      note: 'primary',
      created_at: '2026-05-10T08:00:00Z',
      updated_at: '2026-05-10T08:00:00Z',
    } satisfies AssetDomainRecord
    const filter = {
      vps_id: 'vps_001',
      service_id: 'svc_001',
      target_id: 'tg_001',
      status: 'active',
    } satisfies AssetDomainListFilter
    const collectionInput = {
      vps_id: 'vps_001',
      service_id: 'svc_001',
      target_id: 'tg_001',
      domain_name: 'www.example.com',
      purpose: 'site',
      status: 'active',
      registrar: 'NameSilo',
      expires_at: '2026-07-01',
      auto_renew: true,
      https_enabled: true,
      labels: ['prod'],
      note: 'primary',
    } satisfies CreateAssetDomainInput
    const vpsScopedInput = {
      vps_id: 'vps_body',
      service_id: null,
      target_id: null,
      domain_name: 'api.example.com',
      purpose: 'api',
      status: 'active',
      registrar: '',
      expires_at: null,
      auto_renew: false,
      https_enabled: true,
      labels: ['api'],
      note: 'gateway',
    } satisfies CreateAssetDomainInput
    const expectedVPSScopedBody = {
      service_id: null,
      target_id: null,
      domain_name: 'api.example.com',
      purpose: 'api',
      status: 'active',
      registrar: '',
      expires_at: null,
      auto_renew: false,
      https_enabled: true,
      labels: ['api'],
      note: 'gateway',
    } satisfies Omit<CreateAssetDomainInput, 'vps_id'>
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([domain])))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(domain)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([domain])))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify({ ...domain, domain_name: 'api.example.com' })))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listAssetDomains(filter)).resolves.toEqual([domain])
    await expect(createAssetDomain(collectionInput)).resolves.toEqual(domain)
    await expect(listVPSDomains('vps_001')).resolves.toEqual([domain])
    await expect(createVPSDomain('vps_001', vpsScopedInput)).resolves.toMatchObject({
      domain_id: 'dom_001',
      domain_name: 'api.example.com',
    })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/domains?vps_id=vps_001&service_id=svc_001&target_id=tg_001&status=active',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/domains', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(collectionInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_001/domains', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps/vps_001/domains', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(expectedVPSScopedBody),
    })
  })

  it('loads VPS link summaries from asset and monitoring instance sides', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '[]'))
    vi.stubGlobal('fetch', fetchMock)

    await listVPSMonitoringInstances('vps_001')
    await listVPSForMonitoringInstance('mi_001')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_001/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001/vps', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('serializes VPS cancellation preview, confirmed action, and asset contexts', async () => {
    const preview = {
      vps: {
        vps_id: 'vps_001',
        display_name: 'Tokyo Edge',
        provider_name: 'Hetzner',
        lifecycle_status: 'active',
        usage_status: 'in_use',
        renewal_decision: 'cancel',
        active_monitoring_instance_link_count: 1,
        ssh_port: 22,
        labels: [],
        created_at: '2026-05-30T08:00:00Z',
        updated_at: '2026-05-30T08:00:00Z',
      },
      subscriptions: [],
      monitoring_instance_links: [],
      services: [],
      domains: [],
      target_links: [],
      recommended_steps: [],
      warnings: ['订阅已非活跃，但 VPS 尚未进入 to_cancel/cancelled，存在状态割裂。'],
      blockers: [],
    }
    const actionResult = {
      action: {
        action_id: 'ala_001',
        vps_id: 'vps_001',
        action_type: 'cancel_vps',
        status: 'completed',
        reason: 'expired',
        created_at: '2026-05-30T08:01:00Z',
      },
      steps: [],
    }
    const monitoringInstanceContexts = [{
      monitoring_instance_id: 'mi_001',
      linked_vps_count: 1,
      cancellation_attention: true,
      summaries: [{
        vps_id: 'vps_001',
        display_name: 'Tokyo Edge',
        lifecycle_status: 'cancelled',
        renewal_decision: 'cancel',
        subscription_state: 'expired',
        message: '关联 VPS 已取消，监控实例仍需确认状态。',
      }],
    }]
    const targetContexts = [{
      target_id: 'tg_001',
      linked_vps_count: 1,
      cancellation_attention: true,
      summaries: monitoringInstanceContexts[0].summaries,
      service_ids: ['svc_001'],
      domain_ids: ['dom_001'],
    }]
    const input = {
      reason: 'expired',
      effective_date: '2026-05-30',
      subscription_ids: ['sub_001'],
      vps_lifecycle_status: 'cancelled',
      monitoring_instance_actions: [{ monitoring_instance_id: 'mi_001', lifecycle_status: '已退役', monitoring_status: '暂停' }],
      target_actions: [{ target_id: 'tg_001', run_status: '已归档' }],
    } satisfies ApplyCancellationInput
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(preview)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(actionResult)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(monitoringInstanceContexts)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(targetContexts)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getVPSCancellationPreview('vps_001')).resolves.toEqual(preview)
    await expect(applyVPSCancellation('vps_001', input)).resolves.toEqual(actionResult)
    await expect(listMonitoringInstanceAssetContexts()).resolves.toEqual(monitoringInstanceContexts)
    await expect(listTargetAssetContexts()).resolves.toEqual(targetContexts)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_001/cancellation-preview', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/vps/vps_001/cancellation', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/asset-context/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/asset-context/targets', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('serializes VPS asset updates and link operations', async () => {
    const updatedVPS = {
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      provider_id: 'pv_001',
      provider_name: 'Hetzner',
      product_name: 'cx22',
      order_ref: '',
      country: 'JP',
      region: 'Kanto',
      city: 'Tokyo',
      datacenter: '',
      ipv4: '',
      ipv6: '',
      ssh_host: '',
      ssh_port: 22,
      ssh_user: 'root',
      os_name: '',
      virtualization: '',
      lifecycle_status: 'active',
      usage_status: 'in_use',
      renewal_decision: 'cancel',
      importance: 'normal',
      labels: ['edge'],
      note: '',
      active_monitoring_instance_link_count: 1,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T09:00:00Z',
      archived_at: null,
    } satisfies VPSAssetRecord
    const linkRecord = {
      link_id: 'vpn_001',
      vps_id: 'vps_001',
      monitoring_instance_id: 'mi_001',
      linked_at: '2026-05-09T09:01:00Z',
      unlinked_at: null,
      note: 'primary',
    } satisfies VPSMonitoringInstanceLinkRecord
    const unlinkedRecord = {
      ...linkRecord,
      unlinked_at: '2026-05-09T09:02:00Z',
      note: 'rotated',
    } satisfies VPSMonitoringInstanceLinkRecord
    const patchBody = { renewal_decision: 'cancel', renewal_reason: 'too expensive' } as const
    const linkBody = { monitoring_instance_id: 'mi_001', note: 'primary' } as const
    const unlinkBody = { monitoring_instance_id: 'mi_001', note: 'rotated' } as const
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(updatedVPS)))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(linkRecord)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(unlinkedRecord)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateVPSAsset('vps_001', patchBody)).resolves.toEqual(updatedVPS)
    await expect(linkVPSMonitoringInstance('vps_001', linkBody)).resolves.toEqual(linkRecord)
    await expect(unlinkVPSMonitoringInstance('vps_001', unlinkBody)).resolves.toEqual(unlinkedRecord)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(patchBody),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/vps/vps_001/link-monitoring-instance', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(linkBody),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_001/unlink-monitoring-instance', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(unlinkBody),
    })
  })

  it('serializes subscription filters and creates or updates subscriptions without monthly_price', async () => {
    const subscription = {
      subscription_id: 'sub_001',
      vps_id: 'vps_001',
      price: 12,
      currency: 'USD',
      billing_cycle: 'monthly',
      billing_months: 1,
      monthly_price: 12,
      started_at: '2026-05-01',
      renew_at: '2026-06-01',
      auto_renew: true,
      auto_renew_cancelled: false,
      status: 'active',
      payment_method: 'card',
      note: '',
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    } satisfies SubscriptionRecord
    const input = {
      vps_id: 'vps_001',
      price: 12,
      currency: 'USD',
      billing_cycle: 'monthly',
      billing_months: 1,
      started_at: '2026-05-01',
      renew_at: '2026-06-01',
      auto_renew: true,
      auto_renew_cancelled: false,
      status: 'active',
      payment_method: 'card',
      note: '',
    } satisfies CreateSubscriptionInput
    const patchBody = {
      price: 24,
      currency: 'USD',
      billing_cycle: 'quarterly',
      billing_months: 3,
      renew_at: '2026-08-01',
      auto_renew: false,
      auto_renew_cancelled: true,
      status: 'paused',
      payment_method: 'paypal',
      note: 'review',
    } satisfies UpdateSubscriptionInput
    const updatedSubscription = {
      ...subscription,
      price: 24,
      billing_cycle: 'quarterly',
      billing_months: 3,
      monthly_price: 8,
      renew_at: '2026-08-01',
      auto_renew: false,
      auto_renew_cancelled: true,
      status: 'paused',
      payment_method: 'paypal',
      note: 'review',
      updated_at: '2026-05-09T09:00:00Z',
    } satisfies SubscriptionRecord
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([subscription])))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(subscription)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(subscription)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(updatedSubscription)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listSubscriptions({ vps_id: 'vps_001', status: 'active', renew_within_days: 30, sort: 'renew_at', order: 'asc' })).resolves.toEqual([subscription])
    await expect(getSubscription('sub_001')).resolves.toEqual(subscription)
    await expect(createSubscription(input)).resolves.toEqual(subscription)
    await expect(updateSubscription('sub_001', patchBody)).resolves.toEqual(updatedSubscription)

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/subscriptions?vps_id=vps_001&status=active&renew_within_days=30&sort=renew_at&order=asc',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      },
    )
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/subscriptions/sub_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscriptions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/subscriptions/sub_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(patchBody),
    })
  })

  it('loads settings from /api/settings with truthful telegram metadata', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(mockResponse(200, JSON.stringify(settingsResponseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getSettings()).resolves.toEqual(settingsResponseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/settings', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('updates settings via PUT /api/settings and returns the redacted response shape', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(mockResponse(200, JSON.stringify(settingsResponseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateSettings(settingsUpdateBody)).resolves.toEqual(settingsResponseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/settings', {
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(settingsUpdateBody),
    })
  })

  it('passes settings update API errors through as ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockResponse(400, JSON.stringify({ error: 'invalid input' }))),
    )

    await expect(updateSettings(settingsUpdateBody)).rejects.toMatchObject({
      name: 'ApiError',
      status: 400,
      message: 'invalid input',
    })
  })

  it('updates monitoring instance metadata with PATCH /api/monitoring-instances/:monitoringInstanceId and returns the updated monitoring instance', async () => {
    const requestBody = {
      labels: ['edge', 'core'],
      note: 'updated note',
    } satisfies UpdateMonitoringInstanceMetadataInput
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      group: 'edge-group',
      region: 'ap-northeast-1',
      city: 'Tokyo',
      provider: 'aws',
      lifecycle_status: '在用',
      monitoring_status: '启用',
      binding_status: '已绑定',
      labels: ['edge', 'core'],
      note: 'updated note',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-27T09:00:00Z',
      updated_at: '2026-04-27T09:15:00Z',
    } satisfies MonitoringInstanceRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateMonitoringInstanceMetadata('mi_001', requestBody)).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(requestBody),
    })
  })

  it('sends monitoring instance metadata optimistic preconditions when provided', async () => {
    const requestBody = {
      labels: ['edge'],
      note: 'updated note',
    } satisfies UpdateMonitoringInstanceMetadataInput
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      group: 'edge-group',
      region: 'ap-northeast-1',
      city: 'Tokyo',
      provider: 'aws',
      lifecycle_status: '在用',
      monitoring_status: '启用',
      binding_status: '已绑定',
      labels: ['edge'],
      note: 'updated note',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-27T09:00:00Z',
      updated_at: '2026-04-27T09:15:00Z',
    } satisfies MonitoringInstanceRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await updateMonitoringInstanceMetadata('mi_001', requestBody, {
      expectedUpdatedAt: '2026-04-27T09:00:00Z',
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': '"2026-04-27T09:00:00Z"',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(requestBody),
    })
  })

  it('updates target metadata with PATCH /api/targets/:targetId and returns the updated target', async () => {
    const requestBody = {
      labels: ['public', 'external'],
      note: 'updated target note',
    } satisfies UpdateTargetMetadataInput
    const responseBody = {
      target_id: 'tg_001',
      name: 'Blog',
      target_type: 'service',
      host: 'blog.example.com',
      base_port: 443,
      execution_monitoring_instance_labels: ['edge'],
      run_status: '启用',
      group: 'prod-group',
      labels: ['public', 'external'],
      note: 'updated target note',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-27T09:00:00Z',
      updated_at: '2026-04-27T09:20:00Z',
    } satisfies TargetRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateTargetMetadata('tg_001', requestBody)).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(requestBody),
    })
  })

  it('sends target metadata optimistic preconditions when provided', async () => {
    const requestBody = {
      labels: ['public'],
      note: 'updated target note',
    } satisfies UpdateTargetMetadataInput
    const responseBody = {
      target_id: 'tg_001',
      name: 'Blog',
      target_type: 'service',
      host: 'blog.example.com',
      base_port: 443,
      execution_monitoring_instance_labels: ['edge'],
      run_status: '启用',
      group: 'prod-group',
      labels: ['public'],
      note: 'updated target note',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-27T09:00:00Z',
      updated_at: '2026-04-27T09:20:00Z',
    } satisfies TargetRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await updateTargetMetadata('tg_001', requestBody, {
      expectedUpdatedAt: '2026-04-27T09:00:00Z',
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': '"2026-04-27T09:00:00Z"',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(requestBody),
    })
  })

  it('creates targets with POST /api/targets and returns the created target', async () => {
    const requestBody = {
      name: 'Blog',
      target_type: 'service',
      host: 'blog.example.com',
      base_port: 443,
      execution_monitoring_instance_labels: ['edge', 'core'],
      run_status: '启用',
      group: 'prod-group',
      labels: ['public'],
      note: 'primary blog',
    } satisfies CreateTargetInput
    const responseBody = {
      target_id: 'tg_new',
      ...requestBody,
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-27T09:00:00Z',
      updated_at: '2026-04-27T09:00:00Z',
    } satisfies TargetRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(createTarget(requestBody)).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/targets', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(requestBody),
    })
  })

  it('updates probe items with PUT /api/targets/:targetId/probe-items/:probeItemId and returns the updated probe item', async () => {
    const requestBody = {
      probe_kind: 'http',
      enabled: false,
      frequency_tier: '5s',
      timeout_seconds: 8,
      config: {
        scheme: 'https',
        path: '/ready',
        method: 'HEAD',
        expected_status_range: [200, 204],
      },
    } satisfies UpdateProbeItemInput
    const responseBody = {
      probe_item_id: 'pb_001',
      target_id: 'tg_001',
      ...requestBody,
      created_at: '2026-04-27T09:05:00Z',
      updated_at: '2026-04-27T09:10:00Z',
    } satisfies ProbeItemRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateProbeItem('tg_001', 'pb_001', requestBody)).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_001/probe-items/pb_001', {
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(requestBody),
    })
  })

  it('deletes probe items with DELETE /api/targets/:targetId/probe-items/:probeItemId', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 204,
      text: async () => '',
      json: async () => { throw new Error('json should not be called for 204 responses') },
    } as unknown as Response)
    vi.stubGlobal('fetch', fetchMock)

    await expect(deleteProbeItem('tg_001', 'pb_001')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_001/probe-items/pb_001', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('creates probe items with POST /api/targets/:targetId/probe-items and returns the created probe item', async () => {
    const requestBody = {
      probe_kind: 'http',
      enabled: true,
      frequency_tier: '5s',
      timeout_seconds: 5,
      config: {
        scheme: 'https',
        path: '/healthz',
        method: 'GET',
        expected_status_range: [200, 299],
      },
    } satisfies CreateProbeItemInput
    const responseBody = {
      probe_item_id: 'pb_new',
      target_id: 'tg_new',
      ...requestBody,
      created_at: '2026-04-27T09:05:00Z',
      updated_at: '2026-04-27T09:05:00Z',
    } satisfies ProbeItemRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(createProbeItem('tg_new', requestBody)).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/targets/tg_new/probe-items', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(requestBody),
    })
  })

  it('serializes only non-empty event filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '{"items":[]}'))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listEvents({
      object_type: 'monitoring_instance',
      object_id: '',
      severity: '',
      event_type: 'incident_started',
      limit: 25,
    })).resolves.toEqual([])

    expect(fetchMock).toHaveBeenCalledWith('/api/events?object_type=monitoring_instance&event_type=incident_started&limit=25', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('serializes advanced event filters and omits false booleans', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '{"items":[]}'))
    vi.stubGlobal('fetch', fetchMock)

    await listEvents({
      object_type: 'monitoring_instance',
      created_from: '2026-04-25T00:00:00Z',
      created_to: '2026-04-26T00:00:00Z',
      label: 'edge',
      notification_only: true,
      recovery_only: true,
      maintenance_only: false,
      include_backfilled: true,
      limit: 25,
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/events?object_type=monitoring_instance&limit=25&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=true&recovery_only=true&include_backfilled=true',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      credentials: 'include',
      },
    )
  })

  it('unwraps event list envelopes', async () => {
    const event = {
      event_id: 'evt_001',
      incident_id: 'inc_001',
      incident_class: 'target_probe_failure',
      object_type: 'target',
      object_id: 'tg_001',
      event_type: 'incident_started',
      severity: '告警',
      summary: 'HTTPS 探测失败',
      created_at: '2026-04-25T08:00:00Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify({ items: [event] })))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listEvents()).resolves.toEqual([event])
    expect(fetchMock).toHaveBeenCalledWith('/api/events', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('serializes only non-empty incident filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '[]'))
    vi.stubGlobal('fetch', fetchMock)

    await listIncidents({
      object_type: 'target',
      object_id: 'tg_001',
      severity: '',
      limit: 10,
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/incidents?object_type=target&object_id=tg_001&limit=10', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('loads onboarding state from /api/monitoring-instances/:monitoringInstanceId/onboarding', async () => {
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      region: 'ap-northeast-1',
      city: 'Tokyo',
      provider: 'aws',
      lifecycle_status: '待接入',
      monitoring_status: '启用',
      binding_status: '未绑定',
      labels: [],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-26T09:00:00Z',
      updated_at: '2026-04-26T09:00:00Z',
      phase: '未开始接入',
      has_host_sample: false,
      has_accepted_observation: false,
      enrollment_token_issued_at: '2026-04-26T09:05:00Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getMonitoringInstanceOnboarding('mi_001')).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_001/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('issues one-command install commands with POST /api/monitoring-instances/:monitoringInstanceId/install-command', async () => {
    const responseBody = {
      command: 'curl -fsSL "https://center.example.com/api/agent/install.sh" | sudo sh -s -- --server-url "https://center.example.com" --enrollment-token "enroll_001" --version "v1.2.3" --release-repo "owner/repo"',
      issued_at: '2026-04-26T09:10:00Z',
      expires_at: '2026-04-26T09:40:00Z',
      installer_url: 'https://center.example.com/api/agent/install.sh',
      public_base_url: 'https://center.example.com',
      agent_version: 'v1.2.3',
      release_repo: 'owner/repo',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(issueMonitoringInstanceInstallCommand('mi_001')).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_001/install-command', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts binding actions and returns onboarding state', async () => {
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      region: 'ap-northeast-1',
      city: 'Tokyo',
      provider: 'aws',
      lifecycle_status: '待接入',
      monitoring_status: '启用',
      binding_status: '已绑定',
      labels: [],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-26T09:00:00Z',
      updated_at: '2026-04-26T09:12:00Z',
      phase: '已绑定，等待稳定观测',
      has_host_sample: false,
      has_accepted_observation: false,
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(confirmMonitoringInstanceRebind('mi_001')).resolves.toEqual(responseBody)
    await expect(rejectPendingMonitoringInstanceBinding('mi_001')).resolves.toEqual(responseBody)
    await expect(resetMonitoringInstanceBinding('mi_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances/mi_001/binding/confirm-rebind', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001/binding/reject-pending', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/monitoring-instances/mi_001/binding/reset', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts monitoring instance runtime control actions to the explicit endpoints', async () => {
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      region: 'ap-northeast-1',
      city: 'Tokyo',
      provider: 'aws',
      lifecycle_status: '在用',
      monitoring_status: '维护中',
      binding_status: '已绑定',
      labels: [],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-26T09:00:00Z',
      updated_at: '2026-04-26T09:15:00Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(enterMonitoringInstanceMaintenance('mi_001')).resolves.toEqual(responseBody)
    await expect(exitMonitoringInstanceMaintenance('mi_001')).resolves.toEqual(responseBody)
    await expect(pauseMonitoringInstanceMonitoring('mi_001')).resolves.toEqual(responseBody)
    await expect(resumeMonitoringInstanceMonitoring('mi_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances/mi_001/runtime/enter-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/monitoring-instances/mi_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/monitoring-instances/mi_001/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts monitoring instance command actions and preserves command identity', async () => {
    const responseBody = {
      action_id: 'act_001',
      command_id: 'uptime',
      status: 'pending',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(postMonitoringInstanceAction('mi_001', 'uptime')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_001/actions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ command_id: 'uptime' }),
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts monitoring instance lifecycle actions to the explicit endpoints', async () => {
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      region: 'ap-northeast-1',
      city: 'Tokyo',
      provider: 'aws',
      lifecycle_status: '已退役',
      monitoring_status: '暂停',
      binding_status: '已绑定',
      labels: [],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-26T09:00:00Z',
      updated_at: '2026-04-26T09:18:00Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(retireMonitoringInstance('mi_001')).resolves.toEqual(responseBody)
    await expect(restoreRetiredMonitoringInstanceToObserving('mi_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances/mi_001/lifecycle/retire', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/monitoring-instances/mi_001/lifecycle/restore-to-observing',
      {
        method: 'POST',
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      credentials: 'include',
      },
    )
  })

  it('posts target runtime control actions to the explicit endpoints', async () => {
    const responseBody = {
      target_id: 'tg_001',
      name: 'Blog',
      target_type: 'service',
      host: 'blog.example.com',
      base_port: 443,
      execution_monitoring_instance_labels: ['edge'],
      run_status: '暂停',
      labels: [],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-04-26T09:00:00Z',
      updated_at: '2026-04-26T09:20:00Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(enterTargetMaintenance('tg_001')).resolves.toEqual(responseBody)
    await expect(exitTargetMaintenance('tg_001')).resolves.toEqual(responseBody)
    await expect(pauseTarget('tg_001')).resolves.toEqual(responseBody)
    await expect(resumeTarget('tg_001')).resolves.toEqual(responseBody)
    await expect(archiveTarget('tg_001')).resolves.toEqual(responseBody)
    await expect(restoreTargetToPaused('tg_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/targets/tg_001/runtime/enter-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_001/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/targets/tg_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/targets/tg_001/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/targets/tg_001/runtime/archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/runtime/restore-to-paused', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })
})

describe('router onboarding route', () => {
  it('matches /asset-decisions', () => {
    const matches = matchRoutes(appRoutes, '/asset-decisions')

    expect(matches?.at(-1)?.route.path).toBe('asset-decisions')
  })

  it('no longer matches the removed onboarding route (falls through to catch-all)', () => {
    const matches = matchRoutes(appRoutes, '/monitoring/mi_001/onboarding')

    expect(matches?.at(-1)?.route.path).toBe('*')
  })
})
