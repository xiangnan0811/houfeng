import { describe, it, expect, vi, beforeEach } from 'vitest'
import { authSnapshotError, capabilityFlagsForSession, isAuthSnapshotError, login, logout, managementAccessForSession, me, changePassword, type RuntimeCapabilities, type User } from './auth-client'

const enabledCapabilities: RuntimeCapabilities = {
  records: true,
  comparison: false,
  portability: true,
}

const closedManagement = { access: false }

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
          JSON.stringify({
            user_id: 'u1',
            username: 'admin',
            role: 'admin',
            display_name: '管理员',
            runtime_capabilities: enabledCapabilities,
            management_capabilities: closedManagement,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )
    const u = await login('admin', 'pw')
    expect(u).toEqual({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '管理员',
      runtime_capabilities: enabledCapabilities,
      management_capabilities: closedManagement,
    })
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
    { name: 'incomplete identity', me: new Response(JSON.stringify({ user_id: 'u1' }), { status: 200, headers: { 'Content-Type': 'application/json' } }), error: { name: 'AuthSnapshotError', message: '能力读取失败' } },
    { name: 'missing capabilities', me: new Response(JSON.stringify({ user_id: 'u1', username: 'admin', role: 'admin', display_name: '管理员' }), { status: 200, headers: { 'Content-Type': 'application/json' } }), error: { name: 'AuthSnapshotError', message: '能力读取失败' } },
    { name: 'missing user', me: new Response('', { status: 401 }), error: { message: '登录成功但无法读取当前用户' } },
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
        JSON.stringify({
          user_id: 'u1',
          username: 'admin',
          role: 'admin',
          display_name: '',
          runtime_capabilities: { records: false, comparison: true, portability: true },
          management_capabilities: closedManagement,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )
    const u = await me()
    expect(u).toEqual({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '',
      runtime_capabilities: { records: false, comparison: false, portability: false },
      management_capabilities: closedManagement,
    })
  })

  it('me returns null on 401', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 401 }))
    expect(await me()).toBeNull()
  })

  it('me rejects a 200 snapshot that omits runtime capabilities', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ user_id: 'u1', username: 'admin' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )

    await expect(me()).rejects.toMatchObject({ name: 'AuthSnapshotError', message: '能力读取失败' })
  })

  it.each([
    { name: 'missing user', body: 'null' },
    {
      name: 'non-boolean capability flags',
      body: JSON.stringify({
        user_id: 'u1',
        username: 'admin',
        runtime_capabilities: { records: true, comparison: 'yes', portability: true },
      }),
    },
  ])('me rejects a 200 snapshot with $name', async ({ body }) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(body, { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )

    await expect(me()).rejects.toMatchObject({ name: 'AuthSnapshotError', message: '能力读取失败' })
  })

  it('me defaults absent role and display name, and keeps each optional cap only when records are open', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          user_id: 'u1',
          username: 'admin',
          runtime_capabilities: { records: true, comparison: true, portability: false },
          management_capabilities: { access: true },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    expect(await me()).toEqual({
      user_id: 'u1',
      username: 'admin',
      role: '',
      display_name: '',
      runtime_capabilities: { records: true, comparison: true, portability: false },
      management_capabilities: { access: true },
    })
  })

  it.each([
    {
      name: 'omitted management capabilities',
      management: undefined,
    },
    {
      name: 'non-boolean management access',
      management: { access: 'yes' },
    },
  ])('me rejects a snapshot with $name instead of granting access', async ({ management }) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({
        user_id: 'u1',
        username: 'admin',
        role: 'admin',
        display_name: '管理员',
        runtime_capabilities: enabledCapabilities,
        ...(management ? { management_capabilities: management } : {}),
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )

    await expect(me()).rejects.toMatchObject({ name: 'AuthSnapshotError', message: '能力读取失败' })
  })

  it('does not infer management access from the admin role and keeps it when records are closed', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    fetchSpy.mockResolvedValueOnce(new Response(JSON.stringify({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '管理员',
      runtime_capabilities: { records: false, comparison: true, portability: true },
      management_capabilities: { access: true },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    expect(await me()).toEqual({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '管理员',
      runtime_capabilities: { records: false, comparison: false, portability: false },
      management_capabilities: { access: true },
    })

    fetchSpy.mockResolvedValueOnce(new Response(JSON.stringify({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '管理员',
      runtime_capabilities: enabledCapabilities,
      management_capabilities: { access: false },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

    expect(await me()).toMatchObject({ role: 'admin', management_capabilities: { access: false } })
  })

  it('recognizes a capability read failure and ignores other errors', () => {
    expect(isAuthSnapshotError(authSnapshotError())).toBe(true)
    expect(isAuthSnapshotError(new Error('能力读取失败'))).toBe(false)
    expect(isAuthSnapshotError(null)).toBe(false)
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

const retainedUser: User = {
  user_id: 'u1',
  username: 'admin',
  role: 'admin',
  display_name: '',
  runtime_capabilities: enabledCapabilities,
  management_capabilities: closedManagement,
}

describe('capabilityFlagsForSession', () => {
  it('reads a ready snapshot and closes comparison and portability when records are closed', () => {
    expect(capabilityFlagsForSession({ loading: false, status: 'ready', user: retainedUser })).toEqual(enabledCapabilities)
    expect(capabilityFlagsForSession({
      loading: false,
      status: 'ready',
      user: {
        ...retainedUser,
        runtime_capabilities: { records: true, comparison: true, portability: false },
      },
    })).toEqual({ records: true, comparison: true, portability: false })
    expect(capabilityFlagsForSession({
      loading: false,
      status: 'ready',
      user: {
        ...retainedUser,
        runtime_capabilities: { records: false, comparison: true, portability: true },
      },
    })).toEqual({ records: false, comparison: false, portability: false })
  })

  it('stays closed for loading, anonymous, and failed reads instead of reusing a retained user', () => {
    const closed = { records: false, comparison: false, portability: false }
    expect(capabilityFlagsForSession({ loading: true, status: 'loading', user: retainedUser })).toEqual(closed)
    expect(capabilityFlagsForSession({ loading: false, status: 'anonymous', user: null })).toEqual(closed)
    expect(capabilityFlagsForSession({ loading: false, status: 'ready', user: null })).toEqual(closed)
    expect(capabilityFlagsForSession({ loading: false, status: 'error', user: retainedUser })).toEqual(closed)
  })
})

describe('managementAccessForSession', () => {
  const supervisor = { ...retainedUser, management_capabilities: { access: true } }

  it('allows only a ready snapshot with access explicitly true', () => {
    expect(managementAccessForSession({ loading: false, status: 'ready', user: supervisor })).toBe(true)
    expect(managementAccessForSession({ loading: false, status: 'ready', user: retainedUser })).toBe(false)
    expect(managementAccessForSession({
      loading: false,
      status: 'ready',
      user: { ...supervisor, role: 'admin', management_capabilities: { access: false } },
    })).toBe(false)
  })

  it('stays closed while loading, signed out, or after a failed read', () => {
    expect(managementAccessForSession({ loading: true, status: 'loading', user: supervisor })).toBe(false)
    expect(managementAccessForSession({ loading: false, status: 'anonymous', user: null })).toBe(false)
    expect(managementAccessForSession({ loading: false, status: 'error', user: supervisor })).toBe(false)
    expect(managementAccessForSession({ loading: false, status: 'ready', user: null })).toBe(false)
  })
})
