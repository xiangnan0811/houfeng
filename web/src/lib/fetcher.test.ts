import { describe, it, expect, vi, beforeEach } from 'vitest'
import { fetcher, setUnauthorizedHandler } from './fetcher'

describe('fetcher', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    setUnauthorizedHandler(undefined)
  })

  it('passes credentials: include', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await fetcher('/api/x')
    expect(fetchSpy).toHaveBeenCalledWith(
      '/api/x',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('returns parsed JSON on success', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ value: 7 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    expect(await fetcher<{ value: number }>('/api/x')).toEqual({ value: 7 })
  })

  it('returns undefined on 204', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }))
    expect(await fetcher('/api/x')).toBeUndefined()
  })

  it('throws AuthError on 401 and calls handler', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 401 }))
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    await expect(fetcher('/api/x')).rejects.toThrow(/unauthenticated/i)
    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('throws Error on other non-2xx', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: 'nope' }), {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await expect(fetcher('/api/x')).rejects.toThrow(/500/)
  })
})
