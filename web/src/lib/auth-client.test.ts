import { describe, it, expect, vi, beforeEach } from 'vitest'
import { login, logout, me, changePassword } from './auth-client'

beforeEach(() => {
  vi.restoreAllMocks()
})

describe('auth-client', () => {
  it('login posts JSON and returns the complete identity from /me', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ user_id: 'u1', username: '', role: '', display_name: '' }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '管理员' }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )
    const u = await login('admin', 'pw')
    expect(u).toEqual({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '管理员' })
    const loginCall = fetchSpy.mock.calls[0]
    if (!loginCall) throw new Error('login must call fetch')
    expect(loginCall[0]).toBe('/api/auth/login')
    const init = loginCall[1]
    if (!init) throw new Error('login must pass request options')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({ username: 'admin', password: 'pw' })
    expect(fetchSpy.mock.calls[1]?.[0]).toBe('/api/auth/me')
  })

  it.each([
    { name: 'service failure', me: new Response(JSON.stringify({ error: 'auth service unavailable' }), { status: 503 }), error: { status: 503 } },
    { name: 'incomplete identity', me: new Response(JSON.stringify({ user_id: 'u1' }), { status: 200, headers: { 'Content-Type': 'application/json' } }), error: { message: '登录成功但无法读取当前用户' } },
  ])('login revokes the new session when the identity read fails with $name', async ({ me: meResponse, error }) => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify({ user_id: 'u1' }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(meResponse)
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    await expect(login('admin', 'pw')).rejects.toMatchObject(error)
    expect(fetchSpy.mock.calls.map((call) => call[0])).toEqual(['/api/auth/login', '/api/auth/me', '/api/auth/logout'])
  })

  it('logout posts to /api/auth/logout', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 204 }))
    await logout()
    const logoutCall = fetchSpy.mock.calls[0]
    if (!logoutCall) throw new Error('logout must call fetch')
    expect(logoutCall[0]).toBe('/api/auth/logout')
  })

  it('me returns parsed user', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    const u = await me()
    expect(u?.user_id).toBe('u1')
  })

  it('me returns null on 401', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 401 }))
    expect(await me()).toBeNull()
  })

  it('me returns null when the response is not a complete user', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ user_id: 'u1' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    expect(await me()).toBeNull()
  })

  it('me preserves non-authentication request failures', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ error: 'auth service unavailable' }), { status: 503 }),
    )

    await expect(me()).rejects.toMatchObject({
      name: 'ApiError',
      status: 503,
      message: 'auth service unavailable',
    })
  })

  it('changePassword puts JSON', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 204 }))
    await changePassword('old', 'new-correct-horse-battery')
    const passwordCall = fetchSpy.mock.calls[0]
    if (!passwordCall) throw new Error('changePassword must call fetch')
    const init = passwordCall[1]
    if (!init) throw new Error('changePassword must pass request options')
    expect(init.method).toBe('PUT')
    expect(JSON.parse(String(init.body))).toEqual({
      old_password: 'old',
      new_password: 'new-correct-horse-battery',
    })
  })
})
