import { afterEach, describe, expect, it, vi } from 'vitest'
import { matchRoutes } from 'react-router-dom'

import {
  archiveTarget,
  confirmNodeRebind,
  createProbeItem,
  createProvider,
  createSubscription,
  createTarget,
  createVPSAsset,
  createVPSExperienceLog,
  deleteProbeItem,
  enterNodeMaintenance,
  enterTargetMaintenance,
  exitNodeMaintenance,
  exitTargetMaintenance,
  getDashboard,
  getNodeOnboarding,
  getProvider,
  getSettings,
  getSubscription,
  getVPSAsset,
  getVPSTimeline,
  issueNodeEnrollmentToken,
  linkVPSNode,
  listVPSExperienceLogs,
  listProviders,
  listNodes,
  listEvents,
  listIncidents,
  listSubscriptions,
  pauseNodeMonitoring,
  pauseTarget,
  rejectPendingNodeBinding,
  restoreRetiredNodeToObserving,
  resetNodeBinding,
  retireNode,
  restoreTargetToPaused,
  resumeNodeMonitoring,
  resumeTarget,
  unlinkVPSNode,
  updateNodeMetadata,
  updateProbeItem,
  updateProvider,
  updateSettings,
  updateSubscription,
  updateTargetMetadata,
  updateVPSAsset,
  listVPSAssets,
  listVPSForNode,
  listVPSNodes,
} from './api'
import type {
  CreateProviderInput,
  CreateProbeItemInput,
  CreateSubscriptionInput,
  CreateTargetInput,
  CreateVPSAssetInput,
  CreateVPSExperienceLogInput,
  NodeRecord,
  ProbeItemRecord,
  ProviderRecord,
  SettingsRecord,
  SettingsUpdateInput,
  SubscriptionRecord,
  TargetRecord,
  UpdateNodeMetadataInput,
  UpdateProbeItemInput,
  UpdateProviderInput,
  UpdateSubscriptionInput,
  UpdateTargetMetadataInput,
  VPSAssetRecord,
  VPSExperienceLogRecord,
  VPSNodeLinkRecord,
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
  host_sample_frequency_tier: '5m',
  probe_frequency_defaults: {
    tcp: '5m',
    http: '1m',
    tls: '15m',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 30,
    stale_threshold_intervals: 3,
    sweep_interval_seconds: 60,
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
    node_labels: [
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
  host_sample_frequency_tier: '1m',
  probe_frequency_defaults: {
    tcp: '5m',
    http: '1m',
    tls: '15m',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 30,
    stale_threshold_intervals: 3,
    sweep_interval_seconds: 60,
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
    node_labels: [
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

    await expect(listNodes()).rejects.toMatchObject({
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
        .mockResolvedValue(mockResponse(404, JSON.stringify({ error: 'node not found' }))),
    )

    await expect(listNodes()).rejects.toMatchObject({
      name: 'ApiError',
      status: 404,
      message: 'node not found',
    })
  })

  it('loads dashboard overview from /api/dashboard', async () => {
    const responseBody = {
      snapshot_generated_at: '2026-04-25T08:30:00Z',
      total_node_count: 5,
      total_target_count: 4,
      abnormal_node_count: 1,
      abnormal_target_count: 2,
      severe_node_count: 0,
      severe_target_count: 1,
      maintenance_node_count: 1,
      maintenance_target_count: 0,
      pending_onboarding_node_count: 1,
      paused_node_count: 1,
      retired_node_count: 0,
      paused_target_count: 1,
      archived_target_count: 0,
      recent_new_incident_count: 3,
      recent_recovery_count: 2,
      group_summaries: [
        {
          group: 'production',
          node_count: 3,
          target_count: 2,
          abnormal_node_count: 1,
          abnormal_target_count: 2,
          severe_node_count: 0,
          severe_target_count: 1,
          maintenance_node_count: 1,
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
      active_node_link_count: 1,
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
    const detail = { ...vps, node_links: [] }
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

  it('loads VPS link summaries from asset and node sides', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '[]'))
    vi.stubGlobal('fetch', fetchMock)

    await listVPSNodes('vps_001')
    await listVPSForNode('nd_001')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_001/nodes', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/vps', {
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
      active_node_link_count: 1,
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T09:00:00Z',
      archived_at: null,
    } satisfies VPSAssetRecord
    const linkRecord = {
      link_id: 'vpn_001',
      vps_id: 'vps_001',
      node_id: 'nd_001',
      linked_at: '2026-05-09T09:01:00Z',
      unlinked_at: null,
      note: 'primary',
    } satisfies VPSNodeLinkRecord
    const unlinkedRecord = {
      ...linkRecord,
      unlinked_at: '2026-05-09T09:02:00Z',
      note: 'rotated',
    } satisfies VPSNodeLinkRecord
    const patchBody = { renewal_decision: 'cancel', renewal_reason: 'too expensive' } as const
    const linkBody = { node_id: 'nd_001', note: 'primary' } as const
    const unlinkBody = { node_id: 'nd_001', note: 'rotated' } as const
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(updatedVPS)))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(linkRecord)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(unlinkedRecord)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateVPSAsset('vps_001', patchBody)).resolves.toEqual(updatedVPS)
    await expect(linkVPSNode('vps_001', linkBody)).resolves.toEqual(linkRecord)
    await expect(unlinkVPSNode('vps_001', unlinkBody)).resolves.toEqual(unlinkedRecord)

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
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/vps/vps_001/link-node', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(linkBody),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_001/unlink-node', {
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

  it('updates node metadata with PATCH /api/nodes/:nodeId and returns the updated node', async () => {
    const requestBody = {
      labels: ['edge', 'core'],
      note: 'updated note',
    } satisfies UpdateNodeMetadataInput
    const responseBody = {
      node_id: 'nd_001',
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
    } satisfies NodeRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateNodeMetadata('nd_001', requestBody)).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_001', {
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

  it('sends node metadata optimistic preconditions when provided', async () => {
    const requestBody = {
      labels: ['edge'],
      note: 'updated note',
    } satisfies UpdateNodeMetadataInput
    const responseBody = {
      node_id: 'nd_001',
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
    } satisfies NodeRecord
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await updateNodeMetadata('nd_001', requestBody, {
      expectedUpdatedAt: '2026-04-27T09:00:00Z',
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_001', {
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
      execution_node_labels: ['edge'],
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
      execution_node_labels: ['edge'],
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
      execution_node_labels: ['edge', 'core'],
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
      frequency_tier: '5m',
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
      frequency_tier: '1m',
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
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '[]'))
    vi.stubGlobal('fetch', fetchMock)

    await listEvents({
      object_type: 'node',
      object_id: '',
      severity: '',
      event_type: 'incident_started',
      limit: 25,
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/events?object_type=node&event_type=incident_started&limit=25', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('serializes advanced event filters and omits false booleans', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '[]'))
    vi.stubGlobal('fetch', fetchMock)

    await listEvents({
      object_type: 'node',
      created_from: '2026-04-25T00:00:00Z',
      created_to: '2026-04-26T00:00:00Z',
      label: 'edge',
      notification_only: true,
      recovery_only: true,
      maintenance_only: false,
      limit: 25,
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/events?object_type=node&limit=25&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=true&recovery_only=true',
      {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      credentials: 'include',
      },
    )
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

  it('loads onboarding state from /api/nodes/:nodeId/onboarding', async () => {
    const responseBody = {
      node_id: 'nd_001',
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

    await expect(getNodeOnboarding('nd_001')).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_001/onboarding', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('issues enrollment tokens with POST /api/nodes/:nodeId/enrollment-token', async () => {
    const responseBody = {
      token: 'enroll_001',
      issued_at: '2026-04-26T09:10:00Z',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(issueNodeEnrollmentToken('nd_001')).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/nodes/nd_001/enrollment-token', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts binding actions and returns onboarding state', async () => {
    const responseBody = {
      node_id: 'nd_001',
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

    await expect(confirmNodeRebind('nd_001')).resolves.toEqual(responseBody)
    await expect(rejectPendingNodeBinding('nd_001')).resolves.toEqual(responseBody)
    await expect(resetNodeBinding('nd_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_001/binding/confirm-rebind', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/binding/reject-pending', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/binding/reset', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts node runtime control actions to the explicit endpoints', async () => {
    const responseBody = {
      node_id: 'nd_001',
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

    await expect(enterNodeMaintenance('nd_001')).resolves.toEqual(responseBody)
    await expect(exitNodeMaintenance('nd_001')).resolves.toEqual(responseBody)
    await expect(pauseNodeMonitoring('nd_001')).resolves.toEqual(responseBody)
    await expect(resumeNodeMonitoring('nd_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_001/runtime/enter-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/nodes/nd_001/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts node lifecycle actions to the explicit endpoints', async () => {
    const responseBody = {
      node_id: 'nd_001',
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

    await expect(retireNode('nd_001')).resolves.toEqual(responseBody)
    await expect(restoreRetiredNodeToObserving('nd_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/nodes/nd_001/lifecycle/retire', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/nodes/nd_001/lifecycle/restore-to-observing',
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
      execution_node_labels: ['edge'],
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

  it('matches /nodes/:nodeId/onboarding', () => {
    const matches = matchRoutes(appRoutes, '/nodes/nd_001/onboarding')

    expect(matches?.at(-1)?.route.path).toBe('nodes/:nodeId/onboarding')
  })
})
