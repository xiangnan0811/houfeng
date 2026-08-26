import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  ApiError,
  jsonBodyInit,
  postJSON,
  postJSONBody,
  requestEmpty,
  requestJSON,
  setUnauthorizedHandler,
  withQuery,
} from './apiRequest'
import allowlistedApiError from './apiError'

afterEach(() => {
  setUnauthorizedHandler(undefined)
  vi.restoreAllMocks()
})

describe('API request transport', () => {
  it('builds JSON request init with shared and caller-owned headers', () => {
    expect(jsonBodyInit('PATCH', { name: 'candidate' }, {
      'If-Match': 'draft-etag',
    })).toEqual({
      method: 'PATCH',
      headers: {
        Accept: 'application/json',
        'Content-Type': 'application/json',
        'If-Match': 'draft-etag',
      },
      body: JSON.stringify({ name: 'candidate' }),
    })
  })

  it('normalizes shared query values without changing insertion order', () => {
    expect(withQuery('/api/example', {
      q: '  record title  ',
      lifecycle: undefined,
      include_archived: false,
      include_related: true,
      limit: 0,
      cursor: '   ',
      owner_id: null,
    })).toBe('/api/example?q=record+title&include_related=true&limit=0')
    expect(withQuery('/api/example')).toBe('/api/example')
    expect(withQuery('/api/example', {})).toBe('/api/example')
  })

  it('repeats a key per array value and drops the empty ones', () => {
    expect(withQuery('/api/example', {
      type: ['troubleshooting', '  migration  '],
      tag: [],
      status: ['', '   '],
      q: 'term',
    })).toBe('/api/example?type=troubleshooting&type=migration&q=term')
  })

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

  it('keeps structured error metadata opt-in and allowlists it through a decoder', async () => {
    const recovery = {
      server_revision_id: 'rrv_server',
      server_lock_version: 7,
    }
    const response = JSON.stringify({
      code: 'record_revision_conflict',
      message: 'record revision changed',
      field_errors: [
        { field: 'draft_etag', message: 'draft changed' },
        { field: 42, message: 'ignored' },
      ],
      recovery,
      internal_debug: 'must not escape',
    })
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(response, { status: 409 }))
      .mockResolvedValueOnce(new Response(response, { status: 409 }))

    const defaultError = await requestJSON('/api/failure').catch((reason: unknown) => reason)
    expect(defaultError).toMatchObject({
      status: 409,
      message: 'record revision changed',
      field_errors: [],
      code: 'record_revision_conflict',
    })
    expect(defaultError).not.toHaveProperty('recovery')
    expect(defaultError).not.toHaveProperty('internal_debug')

    const error = await requestJSON(
      '/api/failure',
      undefined,
      allowlistedApiError,
    ).catch((reason: unknown) => reason)

    expect(error).toBeInstanceOf(ApiError)
    expect(error).toMatchObject({
      status: 409,
      message: 'record revision changed',
      code: 'record_revision_conflict',
      field_errors: [{ field: 'draft_etag', message: 'draft changed' }],
      recovery,
    })
    expect(error).not.toHaveProperty('internal_debug')

    const typed = error as ApiError<typeof recovery>
    expect(typed.recovery?.server_lock_version).toBe(7)
  })

  it('ignores malformed structured error metadata', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      code: 409,
      error: 'legacy message',
      field_errors: { field: 'title', message: 'invalid' },
    }), { status: 409 }))

    await expect(requestJSON('/api/failure', undefined, allowlistedApiError)).rejects.toMatchObject({
      status: 409,
      message: 'legacy message',
      code: undefined,
      field_errors: [],
      recovery: undefined,
    })
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
