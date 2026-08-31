import { afterEach, describe, expect, it, vi } from 'vitest'
import { matchRoutes } from 'react-router-dom'

import type { ApplyArchiveInput, ApplyCancellationInput } from './types'
import {
  addAssetDecisionManualGroupMember,
  archiveMonitoringInstance,
  archiveVPS,
  applyVPSCancellation,
  archiveTarget,
  bulkUpsertSubscriptionMonthlyBudgets,
  createAssetDomain,
  createAssetDecisionManualGroup,
  confirmMonitoringInstanceRebind,
  createAssetService,
  createAssetDecisionRecord,
  createProbeItem,
  createProvider,
  createSubscription,
  createTarget,
  createVPSAsset,
  createVPSExperienceLog,
  createVPSService,
  deleteAssetDecisionManualGroupMember,
  deleteProbeItem,
  enterMonitoringInstanceMaintenance,
  enterTargetMaintenance,
  exitMonitoringInstanceMaintenance,
  exitTargetMaintenance,
  getAssetDecisionGroup,
  getAssetDecisionManualGroup,
  getAssetDecisionOverview,
  getAssetDecisionRecord,
  getDashboard,
  getMonitoringInstanceOnboarding,
  getMonitoringInstanceManagementReview,
  getProvider,
  getSettings,
  getSubscription,
  getVPSAsset,
  getVPSArchiveReview,
  getVPSCancellationPreview,
  getVPSIPQuality,
  getVPSIPQualityReport,
  getVPSTimeline,
  issueMonitoringInstanceInstallCommand,
  linkVPSMonitoringInstance,
  listAssetDecisionManualGroups,
  listAssetDecisionRecords,
  listSubscriptionMonthlyBudgets,
  listAssetDomains,
  listAssetServices,
  listVPSDomains,
  listVPSExperienceLogs,
  listProviders,
  listMonitoringInstances,
  listSubscriptions,
  listAssetDecisionGroups,
  listTargetAssetContexts,
  pauseMonitoringInstanceMonitoring,
  pauseTarget,
  permanentCleanupMonitoringInstance,
  rejectPendingMonitoringInstanceBinding,
  resetMonitoringInstanceBinding,
  restoreMonitoringInstanceFromArchive,
  restoreMonitoringInstanceLifecycle,
  restoreTargetToPaused,
  restoreVPSFromArchive,
  resumeMonitoringInstanceMonitoring,
  resumeTarget,
  retireMonitoringInstance,
  postMonitoringInstanceAction,
  patchAssetDecisionManualGroup,
  patchAssetDecisionManualGroupMember,
  patchAssetDecisionRecord,
  unlinkVPSMonitoringInstance,
  updateMonitoringInstanceMetadata,
  updateProbeItem,
  updateProvider,
  updateSettings,
  upsertSubscriptionMonthlyBudget,
  updateSubscription,
  updateTargetMetadata,
  updateVPSAsset,
  withQuery,
  listVPSAssets,
  listVPSForMonitoringInstance,
  listVPSMonitoringInstances,
  monitoringInstanceRuntimeStreamURL,
  createVPSMonitoringInstance,
  listVPSServices,
  createVPSDomain,
  createVPSSubscription,
} from './api'
import { buildSubscriptionInput, INITIAL_SUBSCRIPTION_DRAFT } from '../pages/vps-detail/vpsDetailHelpers'
import { withQuery as transportWithQuery } from './apiRequest'
import { listCommandAudits, listEvents, listIncidents } from './observabilityApi'
import type { CommandAuditListFilter } from './types'
import type {
  AssetDomainListFilter,
  AssetDomainRecord,
  AssetDecisionManualGroupDetail,
  AssetDecisionRecordDetail,
  CreateAssetDecisionManualGroupInput,
  CreateAssetDecisionManualGroupMemberInput,
  CreateAssetDecisionRecordInput,
  PatchAssetDecisionManualGroupInput,
  PatchAssetDecisionManualGroupMemberInput,
  PatchAssetDecisionRecordInput,
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
  SubscriptionMonthlyBudgetRecord,
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
    webhook_url_present: false,
    webhook_url_masked_summary: '',
  },
  host_sample_frequency_tier: '5s',
  probe_frequency_defaults: {
    tcp: '5s',
    http: '5s',
    tls: '6h',
  },
  incident_defaults: {
    heartbeat_interval_seconds: 5,
    stale_threshold_intervals: 12,
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
    raw_layer_days: 30,
    aggregate_layer_days: 30,
    event_layer_days: 90,
    notification_layer_days: 180,
  },
  ip_quality_settings: {
    enabled: true,
    frequency_seconds: 86400,
    stale_after_seconds: 604800,
    timeout_seconds: 15,
    raw_retention_days: 90,
    history_retention_days: 365,
    services: ['netflix', 'chatgpt'],
  },
  subscription_cost_settings: {
    base_currency: 'CNY',
    exchange_rate_provider: 'frankfurter',
    fixer_configured: false,
    default_reminder_offsets_days: [14, 7, 1],
    max_reminder_lead_days: 30,
    exchange_rate_stale_after_hours: 36,
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
    stale_threshold_intervals: 12,
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
    raw_layer_days: 30,
    aggregate_layer_days: 30,
    event_layer_days: 90,
    notification_layer_days: 180,
  },
  ip_quality_settings: {
    enabled: true,
    frequency_seconds: 86400,
    stale_after_seconds: 604800,
    timeout_seconds: 15,
    raw_retention_days: 90,
    history_retention_days: 365,
    services: ['netflix', 'chatgpt'],
  },
} satisfies SettingsUpdateInput

describe('api helpers', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('re-exports the transport-owned query helper for compatibility', () => {
    expect(withQuery).toBe(transportWithQuery)
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

  it('serializes monitoring instance list scope query parameters', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '[]'))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listMonitoringInstances()).resolves.toEqual([])
    await expect(listMonitoringInstances('archived')).resolves.toEqual([])
    await expect(listMonitoringInstances('all')).resolves.toEqual([])

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances?scope=archived', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/monitoring-instances?scope=all', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
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
    const ipQuality = {
      summary: {
        vps_id: 'vps_001',
        observed_at: '2026-06-08T12:00:00Z',
        ip_address: '192.0.2.1',
        ip_version: 4,
        status: 'success',
        risk_level: 'low',
        stale: false,
        ambiguous: false,
        assignment_mode: 'link',
        provider_count: 1,
        unlockable_count: 2,
      },
      latest_report: null,
      provider_results: [],
      service_unlocks: [],
      history: [],
    }
    const ipQualityDetail = {
      ...ipQuality,
      latest_report: {
        report_id: 'ipq_001',
        monitoring_instance_id: 'mi_001',
        observed_at: '2026-06-08T12:00:00Z',
        received_at: '2026-06-08T12:00:01Z',
        agent_version: 'dev',
        fingerprint: 'fp-001',
        sync_batch_id: 'sync_001',
        ip_address: '192.0.2.1',
        ip_version: 4,
        status: 'success',
        is_backfilled: false,
        created_at: '2026-06-08T12:00:02Z',
      },
    }
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
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(ipQuality)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(ipQualityDetail)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(timeline)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(vps)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listVPSAssets({ provider_id: 'pv_001', lifecycle_status: 'active', usage_status: 'in_use', renewal_decision: 'keep', asset_scope: 'historical' })).resolves.toEqual([vps])
    await expect(getVPSAsset('vps_001')).resolves.toEqual(detail)
    await expect(getVPSIPQuality('vps_001')).resolves.toEqual(ipQuality)
    await expect(getVPSIPQualityReport('vps_001', 'ipq_001')).resolves.toEqual(ipQualityDetail)
    await expect(getVPSTimeline('vps_001')).resolves.toEqual(timeline)
    await expect(createVPSAsset(input)).resolves.toEqual(vps)

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/vps?provider_id=pv_001&lifecycle_status=active&usage_status=in_use&renewal_decision=keep&asset_scope=historical',
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
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_001/ip-quality', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps/vps_001/ip-quality/reports/ipq_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/vps/vps_001/timeline', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/vps', {
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
    await expect(createVPSExperienceLog('vps_001', input, 'experience-attempt-key')).resolves.toEqual(logRecord)

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
        'Idempotency-Key': 'experience-attempt-key',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
  })

  it('loads asset decision overview, groups, and group detail', async () => {
    const overview = {
      snapshot_generated_at: '2026-06-04T09:00:00Z',
      renew_within_days: 60,
      group_count: 1,
      member_vps_count: 2,
      needs_decision_count: 1,
      renewal_group_count: 1,
      region_group_count: 0,
      provider_group_count: 0,
      cost_group_count: 0,
      evidence_group_count: 0,
      top_groups: [],
      type_counts: { renewal_attention: 1 },
      view_counts: { renewal: 1 },
      source_availability: {
        subscriptions: true,
        services: true,
        domains: true,
        monitoring: true,
        targets: true,
      },
    }
    const group = {
      group_id: 'adg_auto_001',
      group_type: 'renewal_attention',
      view: 'renewal',
      title: '60 天内续费取舍',
      scope_key: '60',
      scope_label: '60 天内续费取舍',
      priority: 90,
      member_count: 2,
      lifecycle_counts: { active: 2 },
      usage_counts: { in_use: 1, standby: 1 },
      renewal_decision_counts: { unreviewed: 2 },
      renewal_window_count: 2,
      unreviewed_count: 2,
      migrate_count: 0,
      cancel_count: 0,
      cancellation_attention_count: 0,
      idle_count: 0,
      standby_count: 1,
      in_use_count: 1,
      service_count: 1,
      domain_count: 1,
      target_count: 1,
      running_target_count: 1,
      monitoring_link_count: 2,
      abnormal_monitoring_count: 0,
      active_incident_count: 0,
      primary_issue_summary: '',
      monthly_cost_by_currency: [{ currency: 'USD', monthly_total: 20, yearly_total: 240 }],
      evidence_chips: [{ kind: 'renewal_due', label: '续费临近', tone: 'alert' }],
    }
    const detail = { ...group, members: [] }
    const record: AssetDecisionRecordDetail = {
      record_id: 'adr_001',
      title: '德国主备取舍',
      goal: '保留主力',
      status: 'draft',
      source_type: 'auto_group',
      source_group_id: 'adg_auto_001',
      source_group_type: 'renewal_attention',
      source_view: 'renewal',
      scope_key: '60',
      scope_label: '60 天内续费取舍',
      renew_within_days: 60,
      member_count: 2,
      followup_todo_count: 2,
      followup_in_progress_count: 0,
      followup_blocked_count: 0,
      followup_done_count: 0,
      followup_skipped_count: 0,
      evidence_snapshot: { group_id: 'adg_auto_001' },
      execution_readback: {
        status: 'aligned',
        summary: '当前事实与组合判断一致',
        open_count: 0,
        aligned_count: 2,
        drift_count: 0,
        blocked_count: 0,
        needs_evidence_count: 0,
      },
      execution_plan: {
        summary: '执行计划已对齐',
        lane_counts: [{ lane: 'keep_observe', count: 2 }],
        actionable_count: 0,
        blocked_count: 0,
      },
      created_at: '2026-06-05T09:00:00Z',
      updated_at: '2026-06-05T09:00:00Z',
      members: [],
    }
    const createInput: CreateAssetDecisionRecordInput = {
      source_group_id: 'adg_auto_001',
      renew_within_days: 60,
      title: '德国主备取舍',
      goal: '保留主力',
      status: 'draft',
      members: [{ vps_id: 'vps_001', decided_role: 'primary_candidate', decided_action: 'keep', reason: '主力' }],
    }
    const patchInput: PatchAssetDecisionRecordInput = {
      status: 'in_progress',
      goal: '开始迁移',
      members: [{ vps_id: 'vps_001', followup_status: 'blocked', followup_note: '等待迁移窗口' }],
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(overview)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([group])))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(detail)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([record])))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(record)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(record)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify({ ...record, status: 'in_progress', goal: '开始迁移' })))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getAssetDecisionOverview({ view: 'renewal', renew_within_days: 60 })).resolves.toEqual(overview)
    await expect(listAssetDecisionGroups({ view: 'renewal', renew_within_days: 60 })).resolves.toEqual([group])
    await expect(getAssetDecisionGroup('adg_auto_001', { renew_within_days: 60 })).resolves.toEqual(detail)
    await expect(listAssetDecisionRecords()).resolves.toEqual([record])
    await expect(createAssetDecisionRecord(createInput)).resolves.toEqual(record)
    await expect(getAssetDecisionRecord('adr_001')).resolves.toEqual(record)
    await expect(patchAssetDecisionRecord('adr_001', patchInput)).resolves.toEqual({ ...record, status: 'in_progress', goal: '开始迁移' })

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/asset-decisions/overview?view=renewal&renew_within_days=60', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/asset-decisions/groups?view=renewal&renew_within_days=60', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/asset-decisions/groups/adg_auto_001?renew_within_days=60', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/asset-decisions/records', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/asset-decisions/records', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(createInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/asset-decisions/records/adr_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/asset-decisions/records/adr_001', {
      method: 'PATCH',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(patchInput),
    })
  })

  it('serializes asset decision manual group operations', async () => {
    const evidenceAssessment = {
      confidence_score: 70,
      pressure_score: 45,
      readiness_score: 64,
      quality_tier: 'usable',
      decision_bias: 'observe',
      support_signal_count: 2,
      risk_signal_count: 1,
      gap_signal_count: 0,
      summary: '证据可用于组合比较',
    } as const
    const manualGroup: AssetDecisionManualGroupDetail = {
      manual_group_id: 'admg_001',
      status: 'active',
      scenario: 'primary_standby',
      title: '德国主备取舍',
      goal: '保留一台主力和一台备用',
      note: '从同区自动组生成',
      source_type: 'auto_group',
      source_group_id: 'adg_auto_001',
      source_group_type: 'region_portfolio',
      source_view: 'region',
      scope_key: 'DE/Berlin',
      scope_label: '德国 / Berlin',
      renew_within_days: 60,
      member_count: 2,
      lifecycle_counts: { active: 2 },
      usage_counts: { in_use: 1, standby: 1 },
      renewal_decision_counts: { unreviewed: 2 },
      renewal_window_count: 1,
      unreviewed_count: 2,
      migrate_count: 0,
      cancel_count: 0,
      cancellation_attention_count: 0,
      idle_count: 0,
      standby_count: 1,
      in_use_count: 1,
      service_count: 2,
      domain_count: 1,
      target_count: 2,
      running_target_count: 2,
      monitoring_link_count: 2,
      abnormal_monitoring_count: 0,
      active_incident_count: 0,
      primary_issue_summary: '',
      monthly_cost_by_currency: [{ currency: 'EUR', monthly_total: 18, yearly_total: 216 }],
      monthly_cost_base: 140,
      yearly_cost_base: 1680,
      base_currency: 'CNY',
      evidence_chips: [{ kind: 'carries_service', label: '承载服务', tone: 'notice' }],
      evidence_assessment: evidenceAssessment,
      source_availability: {
        subscriptions: true,
        services: true,
        domains: true,
        monitoring: true,
        targets: true,
      },
      created_at: '2026-06-06T09:00:00Z',
      updated_at: '2026-06-06T09:00:00Z',
      members: [],
    }
    const { members: manualMembers, ...manualSummary } = manualGroup
    expect(manualMembers).toEqual([])
    const createInput: CreateAssetDecisionManualGroupInput = {
      source_type: 'auto_group',
      source_group_id: 'adg_auto_001',
      renew_within_days: 60,
      scenario: 'primary_standby',
      title: '德国主备取舍',
      goal: '保留一台主力和一台备用',
      note: '从同区自动组生成',
    }
    const patchInput: PatchAssetDecisionManualGroupInput = {
      status: 'archived',
      note: '已完成判断',
    }
    const memberInput: CreateAssetDecisionManualGroupMemberInput = {
      vps_id: 'vps_002',
      intended_role: 'standby_candidate',
      intended_action: 'observe',
      reason: '保留容灾',
      note: '观察下月账单',
      sort_order: 20,
    }
    const memberPatchInput: PatchAssetDecisionManualGroupMemberInput = {
      intended_action: 'keep',
      reason: '备用价值明确',
      note: '',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([manualSummary])))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(manualGroup)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(manualGroup)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify({ ...manualGroup, status: 'archived', note: '已完成判断' })))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(manualGroup)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(manualGroup)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify({ ...manualGroup, member_count: 1 })))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listAssetDecisionManualGroups()).resolves.toEqual([manualSummary])
    await expect(createAssetDecisionManualGroup(createInput)).resolves.toEqual(manualGroup)
    await expect(getAssetDecisionManualGroup('admg_001')).resolves.toEqual(manualGroup)
    await expect(patchAssetDecisionManualGroup('admg_001', patchInput)).resolves.toMatchObject({
      status: 'archived',
      note: '已完成判断',
    })
    await expect(addAssetDecisionManualGroupMember('admg_001', memberInput)).resolves.toEqual(manualGroup)
    await expect(patchAssetDecisionManualGroupMember('admg_001', 'vps_002', memberPatchInput)).resolves.toEqual(manualGroup)
    await expect(deleteAssetDecisionManualGroupMember('admg_001', 'vps_002')).resolves.toMatchObject({ member_count: 1 })

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/asset-decisions/manual-groups', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/asset-decisions/manual-groups', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(createInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/asset-decisions/manual-groups/admg_001', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/asset-decisions/manual-groups/admg_001', {
      method: 'PATCH',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(patchInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/asset-decisions/manual-groups/admg_001/members', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(memberInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/asset-decisions/manual-groups/admg_001/members/vps_002', {
      method: 'PATCH',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(memberPatchInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(7, '/api/asset-decisions/manual-groups/admg_001/members/vps_002', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
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
    await expect(createVPSService('vps_001', vpsScopedInput, 'service-attempt-key')).resolves.toMatchObject({
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
        'Idempotency-Key': 'service-attempt-key',
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
    await expect(createVPSDomain('vps_001', vpsScopedInput, 'domain-attempt-key')).resolves.toMatchObject({
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
        'Idempotency-Key': 'domain-attempt-key',
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

  it('creates VPS-scoped monitoring instances from the VPS aggregate route', async () => {
    const created = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      group: '',
      region: 'Kanto',
      city: 'Tokyo',
      provider: 'Hetzner',
      lifecycle_status: '待接入',
      monitoring_status: '启用',
      binding_status: '未绑定',
      labels: ['edge'],
      note: '',
      current_health_status: '正常',
      current_active_incident_count: 0,
      current_primary_issue_summary: '',
      created_at: '2026-05-09T09:00:00Z',
      updated_at: '2026-05-09T09:00:00Z',
      link: {
        link_id: 'vnl_001',
        vps_id: 'vps_001',
        monitoring_instance_id: 'mi_001',
        linked_at: '2026-05-09T09:00:00Z',
        note: 'created from vps detail',
      },
    }
    const fetchMock = vi.fn().mockResolvedValueOnce(mockResponse(201, JSON.stringify(created)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(createVPSMonitoringInstance('vps_001', {}, 'monitoring-attempt-key')).resolves.toEqual(created)
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/monitoring-instances', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'monitoring-attempt-key',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({}),
    })
  })

  it('serializes VPS cancellation preview, confirmed action, and target asset contexts', async () => {
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
      warnings: ['订阅账单记录已无续费动作，但 VPS 尚未进入 to_cancel/cancelled，存在状态割裂。'],
      blockers: [],
      preview_digest: 'preview-digest-test',
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
    const linkedVPSSummaries = [{
      vps_id: 'vps_001',
      display_name: 'Tokyo Edge',
      lifecycle_status: 'cancelled',
      renewal_decision: 'cancel',
      subscription_state: 'expired',
      message: '关联 VPS 已取消，Target 仍需确认状态。',
    }]
    const targetContexts = [{
      target_id: 'tg_001',
      linked_vps_count: 1,
      cancellation_attention: true,
      summaries: linkedVPSSummaries,
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
      preview_digest: 'preview-digest-test',
    } satisfies ApplyCancellationInput
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(preview)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(actionResult)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(targetContexts)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getVPSCancellationPreview('vps_001')).resolves.toEqual(preview)
    await expect(applyVPSCancellation('vps_001', input)).resolves.toEqual(actionResult)
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
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/asset-context/targets', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).not.toHaveBeenCalledWith('/api/asset-context/monitoring-instances', expect.anything())
  })

  it('serializes VPS archive review, archive confirmation, and restore endpoints', async () => {
    const review = {
      vps: {
        vps_id: 'vps_001',
        display_name: 'Tokyo Edge',
        provider_name: 'Hetzner',
        product_name: 'cx22',
        lifecycle_status: 'active',
        usage_status: 'in_use',
        renewal_decision: 'cancel',
        active_monitoring_instance_link_count: 0,
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
      warnings: [],
      blockers: [],
      eligible: true,
    }
    const archivedReview = {
      ...review,
      vps: {
        ...review.vps,
        lifecycle_status: 'archived',
        archived_at: '2026-05-30T09:00:00Z',
      },
      blockers: ['VPS 已归档，只能在归档详情页只读查看或执行受控恢复。'],
      eligible: false,
    }
    const restored = {
      ...review.vps,
      lifecycle_status: 'idle',
      archived_at: null,
    }
    const input = { confirmation_name: 'Tokyo Edge' } satisfies ApplyArchiveInput
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(review)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(archivedReview)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(restored)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getVPSArchiveReview('vps_001')).resolves.toEqual(review)
    await expect(archiveVPS('vps_001', input)).resolves.toEqual(archivedReview)
    await expect(restoreVPSFromArchive('vps_001')).resolves.toEqual(restored)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_001/archive-review', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/vps/vps_001/archive', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/vps/vps_001/restore-from-archive', {
      method: 'POST',
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

    await expect(updateVPSAsset('vps_001', patchBody, { expectedUpdatedAt: '2026-05-09T09:00:00Z' })).resolves.toEqual(updatedVPS)
    await expect(linkVPSMonitoringInstance('vps_001', linkBody)).resolves.toEqual(linkRecord)
    await expect(unlinkVPSMonitoringInstance('vps_001', unlinkBody)).resolves.toEqual(unlinkedRecord)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/vps/vps_001', {
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': '"2026-05-09T09:00:00Z"',
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
    const vpsScopedInput = {
      price: 12,
      currency: 'USD',
      billing_cycle: 'monthly',
      billing_months: 1,
      started_at: '2026-05-01',
      renew_at: '2026-06-01',
      auto_renew: true,
      auto_renew_cancelled: false,
      payment_method: 'card',
      note: 'production',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([subscription])))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(subscription)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(subscription)))
      .mockResolvedValueOnce(mockResponse(201, JSON.stringify(subscription)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(updatedSubscription)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listSubscriptions({ vps_id: 'vps_001', status: 'active', renew_within_days: 30, sort: 'renew_at', order: 'asc', asset_scope: 'historical' })).resolves.toEqual([subscription])
    await expect(getSubscription('sub_001')).resolves.toEqual(subscription)
    await expect(createSubscription(input, 'create-sub-collection-001')).resolves.toEqual(subscription)
    await expect(createVPSSubscription('vps_001', vpsScopedInput, 'create-sub-vps-001')).resolves.toEqual(subscription)
    await expect(updateSubscription('sub_001', patchBody)).resolves.toEqual(updatedSubscription)

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/subscriptions?vps_id=vps_001&status=active&renew_within_days=30&sort=renew_at&order=asc&asset_scope=historical',
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
        'Idempotency-Key': 'create-sub-collection-001',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/vps/vps_001/subscriptions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'create-sub-vps-001',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(vpsScopedInput),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/subscriptions/sub_001', {
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

  it('posts the real VPS workbench subscription form including period and renewal_mode', async () => {
    const subscription = {
      subscription_id: 'sub_001',
      vps_id: 'vps_001',
      price: 12,
      currency: 'USD',
      billing_cycle: 'monthly',
      billing_months: 1,
      billing_period_unit: 'month',
      billing_period_length: 1,
      monthly_price: 12,
      started_at: '2026-05-01',
      renew_at: '2026-06-01',
      auto_renew: false,
      auto_renew_cancelled: false,
      renewal_mode: 'manual',
      status: 'active',
      payment_method: 'card',
      note: 'production',
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    } satisfies SubscriptionRecord
    const input = buildSubscriptionInput({
      ...INITIAL_SUBSCRIPTION_DRAFT,
      price: '12',
      currency: 'USD',
      billingPeriodUnit: 'month',
      billingPeriodLength: '1',
      startedAt: '2026-05-01',
      renewAt: '2026-06-01',
      renewalMode: 'manual',
      paymentMethod: 'card',
      note: 'production',
    })
    expect(input).toEqual(expect.objectContaining({
      billing_period_unit: 'month',
      billing_period_length: 1,
      renewal_mode: 'manual',
    }))
    const fetchMock = vi.fn().mockResolvedValueOnce(mockResponse(201, JSON.stringify(subscription)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(createVPSSubscription('vps_001', input, 'form-sub-vps-001')).resolves.toEqual(subscription)
    expect(fetchMock).toHaveBeenCalledWith('/api/vps/vps_001/subscriptions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'Idempotency-Key': 'form-sub-vps-001',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
    const posted = JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body)) as Record<string, unknown>
    expect(posted.billing_period_unit).toBe('month')
    expect(posted.billing_period_length).toBe(1)
    expect(posted.renewal_mode).toBe('manual')
  })

  it('uses monthly budget endpoints for subscription budget timeline', async () => {
    const monthlyBudget = {
      budget_month: '2026-06-01',
      base_currency: 'CNY',
      monthly_limit: 100,
      warning_pct: 80,
      note: 'baseline',
      created_at: '2026-05-09T08:00:00Z',
      updated_at: '2026-05-09T08:00:00Z',
    } satisfies SubscriptionMonthlyBudgetRecord
    const input = {
      base_currency: 'USD',
      monthly_limit: 120,
      warning_pct: 75,
      note: 'growth',
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify([monthlyBudget])))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify({ ...monthlyBudget, ...input, budget_month: '2026-07-01' })))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify({
        scope: 'recent_year',
        start_month: '2025-08-01',
        end_month: '2026-07-01',
        records: [{ ...monthlyBudget, ...input, budget_month: '2026-07-01' }],
      })))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listSubscriptionMonthlyBudgets()).resolves.toEqual([monthlyBudget])
    await expect(upsertSubscriptionMonthlyBudget('2026-07-15', input)).resolves.toEqual({
      ...monthlyBudget,
      ...input,
      budget_month: '2026-07-01',
    })
    await expect(bulkUpsertSubscriptionMonthlyBudgets({ ...input, scope: 'recent_year' })).resolves.toEqual({
      scope: 'recent_year',
      start_month: '2025-08-01',
      end_month: '2026-07-01',
      records: [{ ...monthlyBudget, ...input, budget_month: '2026-07-01' }],
    })

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/subscription-monthly-budgets', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/subscription-monthly-budgets/2026-07', {
      method: 'PUT',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify(input),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/subscription-monthly-budgets/bulk', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ ...input, scope: 'recent_year' }),
    })
  })

  it('loads settings from /api/settings with truthful telegram metadata', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(mockResponse(200, JSON.stringify(settingsResponseBody)))
    vi.stubGlobal('fetch', fetchMock)

    const settings = await getSettings()
    expect(settings).toEqual(settingsResponseBody)
    expect(settings.incident_defaults.stale_threshold_intervals).toBe(12)
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
    const requestBody = JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string) as SettingsUpdateInput
    expect(requestBody.incident_defaults.stale_threshold_intervals).toBe(12)
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

  it('builds same-origin monitoring runtime stream WebSocket URLs', () => {
    expect(monitoringInstanceRuntimeStreamURL('mi_001', 'http://center.example.com')).toBe(
      'ws://center.example.com/api/monitoring-instances/mi_001/runtime-stream',
    )
    expect(monitoringInstanceRuntimeStreamURL('mi/slash', 'https://center.example.com/base')).toBe(
      'wss://center.example.com/api/monitoring-instances/mi%2Fslash/runtime-stream',
    )
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
    })
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

  it('requests default command audits without query parameters', async () => {
    const response = { items: [], next_cursor: '' }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(response)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listCommandAudits()).resolves.toEqual(response)
    expect(fetchMock).toHaveBeenCalledWith('/api/command-audits', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('serializes normalized initial command audit filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '{"items":[]}'))
    vi.stubGlobal('fetch', fetchMock)
    const filter: CommandAuditListFilter = {
      window: 'custom',
      started_from: '2026-07-01T00:00:00Z',
      started_to: '2026-07-02T00:00:00Z',
      monitoring_instance: ' Tokyo ',
      command_id: 'uptime',
      sensitivity: 'standard',
      outcome: 'succeeded',
      actor: 'admin',
      action_id: 'act_001',
      limit: 100,
    }

    await listCommandAudits(filter)

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/command-audits?window=custom&started_from=2026-07-01T00%3A00%3A00Z&started_to=2026-07-02T00%3A00%3A00Z&monitoring_instance=Tokyo&command_id=uptime&sensitivity=standard&outcome=succeeded&actor=admin&action_id=act_001&limit=100',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('uses cursor alone for command audit continuation', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, '{"items":[]}'))
    vi.stubGlobal('fetch', fetchMock)

    await listCommandAudits({
      cursor: 'opaque_cursor',
      window: '7d',
      actor: 'must-not-leak',
    })

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/command-audits?cursor=opaque_cursor',
      expect.objectContaining({ credentials: 'include' }),
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

  it('loads onboarding state from /api/monitoring-instances/:monitoringInstanceId/onboarding', async () => {
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      group: '',
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
      command: 'tmp_installer="$(mktemp)" && curl -fsSL "https://center.example.com/api/agent/install.sh" -o "$tmp_installer" && sudo sh "$tmp_installer" --server-url "https://center.example.com" --enrollment-token-stdin --install-missing-deps --version "v1.2.3" --release-repo "owner/repo" <<\'HOUFENG_ENROLLMENT_TOKEN\'\nenroll_001\nHOUFENG_ENROLLMENT_TOKEN\nstatus=$?; rm -f "$tmp_installer"; test "$status" -eq 0',
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
      group: '',
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

  it('loads monitoring instance management review from the explicit endpoint', async () => {
    const record = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      group: '',
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
      archived_at: null,
      archived_reason: '',
      created_at: '2026-04-26T09:00:00Z',
      updated_at: '2026-04-26T09:15:00Z',
    } satisfies MonitoringInstanceRecord
    const responseBody = {
      record,
      active_vps_links: [
        {
          link_id: 'lnk_001',
          vps_id: 'vps_001',
          display_name: 'Tokyo VPS',
          lifecycle_status: 'active',
          usage_status: 'in_use',
          linked_at: '2026-04-26T09:00:00Z',
          note: 'primary',
        },
      ],
      counts: {
        heartbeat_count: 0,
        host_sample_count: 0,
        probe_observation_count: 0,
        host_sample_daily_aggregate_count: 0,
        ip_quality_report_count: 0,
        active_incident_count: 0,
        state_change_event_count: 0,
        notification_record_count: 0,
        asset_lifecycle_action_step_count: 0,
        active_vps_link_count: 1,
      },
      warnings: ['存在活跃 VPS 关联'],
      blockers: ['归档前需要先解除活跃 VPS 关联'],
      actions: {
        can_retire: false,
        can_restore_lifecycle: true,
        can_archive: false,
        can_restore_archive: false,
        can_permanent_cleanup: true,
      },
      empty_mistake_candidate: true,
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getMonitoringInstanceManagementReview('mi_001')).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_001/management-review', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('posts monitoring instance lifecycle and cleanup management actions', async () => {
    const responseBody = {
      monitoring_instance_id: 'mi_001',
      display_name: 'Tokyo Edge',
      group: '',
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
      updated_at: '2026-04-26T09:15:00Z',
    } satisfies MonitoringInstanceRecord
    const cleanupResult = {
      monitoring_instance_id: 'mi_001',
      counts: {
        heartbeat_count: 0,
        host_sample_count: 0,
        probe_observation_count: 0,
        host_sample_daily_aggregate_count: 0,
        ip_quality_report_count: 0,
        active_incident_count: 0,
        state_change_event_count: 0,
        notification_record_count: 0,
        asset_lifecycle_action_step_count: 0,
        active_vps_link_count: 0,
      },
      deleted_reference_count: 0,
      deleted: true,
    }
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(responseBody)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(responseBody)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify({ ...responseBody, archived_at: '2026-04-26T09:20:00Z', archived_reason: '重复创建' })))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(responseBody)))
      .mockResolvedValueOnce(mockResponse(200, JSON.stringify(cleanupResult)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(retireMonitoringInstance('mi_001', { reason: '停止观测' })).resolves.toEqual(responseBody)
    await expect(restoreMonitoringInstanceLifecycle('mi_001', { reason: '重新观察' })).resolves.toEqual(responseBody)
    await expect(archiveMonitoringInstance('mi_001', { reason: '重复创建', confirmation_name: 'Tokyo Edge' })).resolves.toMatchObject({
      archived_at: '2026-04-26T09:20:00Z',
    })
    await expect(restoreMonitoringInstanceFromArchive('mi_001')).resolves.toEqual(responseBody)
    await expect(permanentCleanupMonitoringInstance('mi_001', { reason: '误创建空实例', confirmation_name: 'Tokyo Edge' })).resolves.toEqual(cleanupResult)

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/monitoring-instances/mi_001/lifecycle/retire', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ reason: '停止观测' }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/monitoring-instances/mi_001/lifecycle/restore', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ reason: '重新观察' }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/monitoring-instances/mi_001/archive', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ reason: '重复创建', confirmation_name: 'Tokyo Edge' }),
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/monitoring-instances/mi_001/restore-from-archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/monitoring-instances/mi_001/permanent-cleanup', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      body: JSON.stringify({ reason: '误创建空实例', confirmation_name: 'Tokyo Edge' }),
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

  it('posts sensitive monitoring instance command confirmation only when requested', async () => {
    const responseBody = {
      action_id: 'act_001',
      command_id: 'systemctl_status',
      status: 'pending',
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(
      postMonitoringInstanceAction('mi_001', 'systemctl_status', { confirmedSensitive: true }),
    ).resolves.toEqual(responseBody)

    expect(fetchMock).toHaveBeenCalledWith('/api/monitoring-instances/mi_001/actions', {
      method: 'POST',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ command_id: 'systemctl_status', confirmed_sensitive: true }),
      cache: 'no-store',
      credentials: 'include',
    })
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

  it('matches /archive', () => {
    const matches = matchRoutes(appRoutes, '/archive')

    expect(matches?.at(-1)?.route.path).toBe('archive')
  })

  it('matches /archive/:vpsId', () => {
    const matches = matchRoutes(appRoutes, '/archive/vps_001')

    expect(matches?.at(-1)?.route.path).toBe('archive/:vpsId')
  })

  it('matches /vps/:vpsId/ip-quality', () => {
    const matches = matchRoutes(appRoutes, '/vps/vps_001/ip-quality')

    expect(matches?.at(-1)?.route.path).toBe('vps/:vpsId/ip-quality')
  })

  it('no longer matches the removed onboarding route (falls through to catch-all)', () => {
    const matches = matchRoutes(appRoutes, '/monitoring/mi_001/onboarding')

    expect(matches?.at(-1)?.route.path).toBe('*')
  })
})
