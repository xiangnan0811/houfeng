import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  ApiError,
  postJSON,
  postJSONBody,
  requestEmpty,
  requestJSON,
  setUnauthorizedHandler,
} from './apiRequest'

afterEach(() => {
  setUnauthorizedHandler(undefined)
  vi.restoreAllMocks()
})

describe('API request transport', () => {
  it('uses the shared JSON, cache and credential defaults', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    )

    await expect(requestJSON<{ ok: boolean }>('/api/example')).resolves.toEqual({ ok: true })
    expect(fetchMock).toHaveBeenCalledWith('/api/example', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
  })

  it('lets an explicit request init replace transport defaults', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), { status: 200 }),
    )

    await requestJSON('/api/example', {
      method: 'PUT',
      cache: 'reload',
      credentials: 'omit',
      headers: { 'X-Test': 'yes' },
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/example', {
      headers: { 'X-Test': 'yes' },
      cache: 'reload',
      credentials: 'omit',
      method: 'PUT',
    })
  })

  it('calls the current unauthorized handler before rejecting a 401', async () => {
    const unauthorized = vi.fn()
    setUnauthorizedHandler(unauthorized)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }))

    await expect(requestJSON('/api/private')).rejects.toEqual(
      expect.objectContaining({ name: 'ApiError', status: 401, message: 'unauthenticated' }),
    )
    expect(unauthorized).toHaveBeenCalledTimes(1)
  })

  it('rejects a 401 safely when no unauthorized handler is registered', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }))

    await expect(requestEmpty('/api/private')).rejects.toBeInstanceOf(ApiError)
  })

  it.each([
    [JSON.stringify({ error: 'explicit error' }), 'explicit error'],
    [JSON.stringify({ message: 'explicit message' }), 'explicit message'],
    ['upstream unavailable', 'upstream unavailable'],
  ])('surfaces a structured or plain error body', async (body, expectedMessage) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(body, { status: 502 }))

    await expect(requestJSON('/api/failure')).rejects.toMatchObject({
      name: 'ApiError',
      status: 502,
      message: expectedMessage,
    })
  })

  it('uses the status fallback for an empty error body', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 503 }))

    await expect(requestJSON('/api/failure')).rejects.toMatchObject({
      status: 503,
      message: 'Request failed: 503',
    })
  })

  it('keeps an unrecognized JSON error body as the diagnostic message', async () => {
    const body = JSON.stringify({ detail: 'unknown shape' })
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(body, { status: 400 }))

    await expect(requestJSON('/api/failure')).rejects.toMatchObject({ message: body })
  })

  it('propagates malformed successful JSON instead of inventing a value', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{', { status: 200 }))

    await expect(requestJSON('/api/malformed')).rejects.toBeInstanceOf(SyntaxError)
  })

  it('accepts a successful empty response', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }))

    await expect(requestEmpty('/api/empty')).resolves.toBeUndefined()
  })

  it('posts without a body through postJSON', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ id: 'created' }), { status: 200 }),
    )

    await postJSON('/api/create')

    expect(fetchMock).toHaveBeenCalledWith('/api/create', {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
      method: 'POST',
    })
  })

  it('posts a JSON body through postJSONBody', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ id: 'created' }), { status: 200 }),
    )

    await postJSONBody('/api/create', { name: 'candidate' })

    expect(fetchMock).toHaveBeenCalledWith('/api/create', {
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
      },
      cache: 'no-store',
      credentials: 'include',
      method: 'POST',
      body: JSON.stringify({ name: 'candidate' }),
    })
  })
})
