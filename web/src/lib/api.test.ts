import { afterEach, describe, expect, it, vi } from 'vitest'
import { matchRoutes } from 'react-router-dom'

import {
  archiveTarget,
  confirmNodeRebind,
  enterNodeMaintenance,
  enterTargetMaintenance,
  exitNodeMaintenance,
  exitTargetMaintenance,
  getDashboard,
  getNodeOnboarding,
  getSettings,
  issueNodeEnrollmentToken,
  listNodes,
  listEvents,
  listIncidents,
  pauseNodeMonitoring,
  pauseTarget,
  rejectPendingNodeBinding,
  resetNodeBinding,
  restoreTargetToPaused,
  resumeNodeMonitoring,
  resumeTarget,
  updateSettings,
} from './api'
import type { SettingsRecord, SettingsUpdateInput } from './types'
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
    runtime_apply_active: false,
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
      abnormal_node_count: 1,
      abnormal_target_count: 2,
      severe_node_count: 0,
      severe_target_count: 1,
      maintenance_node_count: 1,
      maintenance_target_count: 0,
      recent_new_incident_count: 3,
      recent_recovery_count: 2,
      recent_events: [],
    }
    const fetchMock = vi.fn().mockResolvedValue(mockResponse(200, JSON.stringify(responseBody)))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getDashboard()).resolves.toEqual(responseBody)
    expect(fetchMock).toHaveBeenCalledWith('/api/dashboard', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
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
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/binding/reject-pending', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/binding/reset', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
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
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/nodes/nd_001/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/nodes/nd_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/nodes/nd_001/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
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
    })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_001/runtime/exit-maintenance', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/targets/tg_001/runtime/pause', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(4, '/api/targets/tg_001/runtime/resume', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(5, '/api/targets/tg_001/runtime/archive', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(6, '/api/targets/tg_001/runtime/restore-to-paused', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
  })
})

describe('router onboarding route', () => {
  it('matches /nodes/:nodeId/onboarding', () => {
    const matches = matchRoutes(appRoutes, '/nodes/nd_001/onboarding')

    expect(matches?.at(-1)?.route.path).toBe('nodes/:nodeId/onboarding')
  })
})
