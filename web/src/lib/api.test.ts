import { afterEach, describe, expect, it, vi } from 'vitest'

import { listNodes } from './api'

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
})
