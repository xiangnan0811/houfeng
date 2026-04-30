import { describe, it, expect, vi, beforeEach } from 'vitest'
import { login, logout, me, changePassword } from './auth-client'

beforeEach(() => {
  vi.restoreAllMocks()
})

describe('auth-client', () => {
  it('login posts JSON and returns user', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '管理员' }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    const u = await login('admin', 'pw')
    expect(u.username).toBe('admin')
    const init = fetchSpy.mock.calls[0][1] as RequestInit
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({ username: 'admin', password: 'pw' })
  })

  it('logout posts to /api/auth/logout', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 204 }))
    await logout()
    expect(fetchSpy.mock.calls[0][0]).toBe('/api/auth/logout')
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

  it('changePassword puts JSON', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 204 }))
    await changePassword('old', 'new-correct-horse-battery')
    const init = fetchSpy.mock.calls[0][1] as RequestInit
    expect(init.method).toBe('PUT')
    expect(JSON.parse(String(init.body))).toEqual({
      old_password: 'old',
      new_password: 'new-correct-horse-battery',
    })
  })
})
