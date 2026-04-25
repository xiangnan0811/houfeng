import { afterEach, describe, expect, it, vi } from 'vitest'

import { getDashboard, listEvents, listIncidents, listNodes } from './api'

function mockResponse(status: number, body: string) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => body,
    json: async () => JSON.parse(body),
  } as Response
}

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
})
