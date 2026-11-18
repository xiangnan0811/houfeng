import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, renderHook, screen, fireEvent, waitFor } from '@testing-library/react'
import * as client from './auth-client'
import { capabilityFlagsForSession } from './auth-client'
import { AuthProvider, useAuth } from './auth-context'
import type * as DraftBuffers from '../pages/records/draftBuffer'
import * as draftBuffers from '../pages/records/draftBuffer'
import { emptyRecordDraftPayload } from '../pages/records/recordPayload'
import { requestEmpty } from './apiRequest'
import * as cleanupConsumer from './recordClientCleanupConsumer'
import { readComparisonSaveAttempt, writeComparisonSaveAttempt, type ComparisonSaveAttemptV2 } from './comparisonSaveAttempts'

vi.mock('./recordClientCleanupConsumer', () => ({
  startRecordClientCleanup: () => ({ stop: () => {}, ready: Promise.resolve() }),
}))

vi.mock('../pages/records/draftBuffer', async (importOriginal) => {
  const actual = await importOriginal<typeof DraftBuffers>()
  const store = actual.memoryDraftBufferStore()
  return {
    ...actual,
    openIndexedDBDraftBuffer: () => store,
    discardUserDrafts: (userId: string) => actual.discardUserDrafts(userId, store),
  }
})

const logoutDraft = (userId: string) => ({
  key: `${userId}:rec_logout`,
  userId,
  recordId: 'rec_logout',
  payload: { ...emptyRecordDraftPayload(userId), title: `private ${userId}` },
  updatedAt: Date.now(),
})

const enabledUser = {
  user_id: 'u1',
  username: 'admin',
  role: 'admin',
  display_name: '',
  runtime_capabilities: { records: true, comparison: false, portability: true },
  management_capabilities: { access: false },
}

function Flags() {
  const flags = capabilityFlagsForSession(useAuth())
  return <span data-testid="flags">{`${flags.records}/${flags.comparison}/${flags.portability}`}</span>
}

function Probe() {
  const { user, loading, status, error, login, logout, refresh, retry } = useAuth()
  if (loading) return <div>loading</div>
  return (
    <div>
      <span data-testid="user">{user?.username ?? 'none'}</span>
      <span data-testid="status">{status}</span>
      <span data-testid="error">{error ?? ''}</span>
      <button onClick={() => { void login('admin', 'pw') }}>in</button>
      <button onClick={() => { void logout() }}>out</button>
      <button onClick={() => { void refresh() }}>refresh</button>
      <button onClick={() => { void retry() }}>retry</button>
    </div>
  )
}

describe('AuthProvider', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('rejects consumers outside AuthProvider', () => {
    function OutsideProbe() {
      useAuth()
      return null
    }
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    expect(() => render(<OutsideProbe />)).toThrow('useAuth must be inside <AuthProvider>')

    consoleError.mockRestore()
  })

  it('rejects capability readers outside AuthProvider', () => {
    function OutsideFlags() {
      capabilityFlagsForSession(useAuth())
      return null
    }
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    expect(() => render(<OutsideFlags />)).toThrow('useAuth must be inside <AuthProvider>')

    consoleError.mockRestore()
  })

  it('boots with /me result', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    expect(screen.getByTestId('status')).toHaveTextContent('ready')
  })

  it('keeps an unauthenticated boot failure retryable instead of signing out', async () => {
    const me = vi.spyOn(client, 'me').mockRejectedValue(new Error('auth service unavailable'))

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('error'))
    expect(screen.getByTestId('user')).toHaveTextContent('none')
    expect(screen.getByTestId('error')).toHaveTextContent('能力读取失败')
    expect(screen.getByTestId('error')).not.toHaveTextContent('auth service unavailable')
    me.mockResolvedValue(enabledUser)
    fireEvent.click(screen.getByText('retry'))
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    expect(screen.getByTestId('status')).toHaveTextContent('ready')
  })

  it('signs out only when /me reports that the session is gone', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(null)

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('anonymous'))
    expect(screen.getByTestId('user')).toHaveTextContent('none')
    expect(screen.getByTestId('error')).toHaveTextContent('')
  })

  it('keeps the signed-in identity but blocks capabilities when a later refresh fails', async () => {
    const me = vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    render(
      <AuthProvider>
        <>
          <Probe />
          <Flags />
        </>
      </AuthProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    expect(screen.getByTestId('flags')).toHaveTextContent('true/false/true')
    me.mockRejectedValue(new Error('auth service unavailable'))
    fireEvent.click(screen.getByText('refresh'))
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('error'))
    expect(screen.getByTestId('user')).toHaveTextContent('admin')
    expect(screen.getByTestId('error')).toHaveTextContent('能力读取失败')
    expect(screen.getByTestId('error')).not.toHaveTextContent('auth service unavailable')
    expect(screen.getByTestId('flags')).toHaveTextContent('false/false/false')
    me.mockResolvedValue(enabledUser)
    fireEvent.click(screen.getByText('retry'))
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('ready'))
    expect(screen.getByTestId('error')).toHaveTextContent('')
    expect(screen.getByTestId('flags')).toHaveTextContent('true/false/true')
  })

  it('sets user after login', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(null)
    vi.spyOn(client, 'login').mockResolvedValue(enabledUser)
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('none'))
    fireEvent.click(screen.getByText('in'))
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
  })

  it('logout without a session still completes and clears local record state', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(null)
    vi.spyOn(client, 'logout').mockResolvedValue()
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('none'))
    fireEvent.click(screen.getByText('out'))
    await waitFor(() => expect(client.logout).toHaveBeenCalled())
    expect(screen.getByTestId('user')).toHaveTextContent('none')
  })

  it('clears user after logout', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    vi.spyOn(client, 'logout').mockResolvedValue()
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    fireEvent.click(screen.getByText('out'))
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('none'))
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('anonymous'))
  })
})

describe('durable logout cleanup', () => {
  beforeEach(async () => {
    vi.restoreAllMocks()
    const store = draftBuffers.openIndexedDBDraftBuffer()
    await store.clearUser(enabledUser.user_id)
    await store.clearUser('usr_other')
  })

  it('keeps a failed cleanup explicit and retries it without losing another user or accepting a late write', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    vi.spyOn(client, 'logout').mockResolvedValue(undefined)
    const store = draftBuffers.openIndexedDBDraftBuffer()
    const mine = logoutDraft(enabledUser.user_id)
    const theirs = logoutDraft('usr_other')
    const oldScope = await store.captureScope(mine.userId, mine.recordId)
    await store.set(mine, oldScope)
    await store.set(theirs, await store.captureScope(theirs.userId, theirs.recordId))
    vi.spyOn(draftBuffers, 'discardUserDrafts').mockRejectedValueOnce(new Error('transaction aborted'))
    render(<AuthProvider><Probe /></AuthProvider>)
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('ready'))
    fireEvent.click(screen.getByText('out'))
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('error'))
    expect(screen.getByTestId('user')).toHaveTextContent('none')
    expect(screen.getByTestId('error')).toHaveTextContent('本地草稿清理失败')
    expect(await store.get(mine.key)).toEqual(mine)
    fireEvent.click(screen.getByText('retry'))
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('anonymous'))
    expect(await store.get(mine.key)).toBeUndefined()
    expect(await store.get(theirs.key)).toEqual(theirs)
    await expect(store.set(mine, oldScope)).rejects.toBeInstanceOf(draftBuffers.DraftWriteInvalidatedError)
  })

  it('drops a 401 session without deleting buffers or invalidating their durable generation', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    const store = draftBuffers.openIndexedDBDraftBuffer()
    const mine = logoutDraft(enabledUser.user_id)
    const scope = await store.captureScope(mine.userId, mine.recordId)
    await store.set(mine, scope)
    render(<AuthProvider><Probe /></AuthProvider>)
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('ready'))
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 401 }))
    await act(async () => {
      await expect(requestEmpty('/api/records/session-probe')).rejects.toMatchObject({ status: 401 })
    })
    expect(screen.getByTestId('status')).toHaveTextContent('anonymous')
    expect(await store.get(mine.key)).toEqual(mine)
    await store.set({ ...mine, payload: { ...mine.payload, title: 'still recoverable' } }, scope)
    expect((await store.get(mine.key))?.payload.title).toBe('still recoverable')
  })

  it('isolates cleanup connection failure from session capabilities and preserves drafts through retry', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    const store = draftBuffers.openIndexedDBDraftBuffer()
    const mine = logoutDraft(enabledUser.user_id)
    const scope = await store.captureScope(mine.userId, mine.recordId)
    await store.set(mine, scope)
    vi.spyOn(cleanupConsumer, 'startRecordClientCleanup').mockImplementationOnce((_userId, options) => {
      queueMicrotask(() => options.onError(new Error('socket closed')))
      return { stop: vi.fn(), ready: Promise.resolve() }
    })
    render(<AuthProvider><Probe /><Flags /></AuthProvider>)
    await waitFor(() => expect(screen.getByTestId('error')).toHaveTextContent('清理同步已暂停'))
    expect(screen.getByTestId('status')).toHaveTextContent('ready')
    expect(screen.getByTestId('flags')).toHaveTextContent('true/false/true')
    expect(await store.get(mine.key)).toEqual(mine)
    fireEvent.click(screen.getByText('retry'))
    await waitFor(() => expect(screen.getByTestId('error')).toBeEmptyDOMElement())
    await store.set({ ...mine, payload: { ...mine.payload, title: 'recoverable after retry' } }, scope)
    expect((await store.get(mine.key))?.payload.title).toBe('recoverable after retry')
  })

  it.each(['login', 'refresh'] as const)('does not revive a logged-out session when an earlier %s settles', async (operation) => {
    const me = vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    vi.spyOn(client, 'logout').mockResolvedValue(undefined)
    const { result } = renderHook(() => useAuth(), {
      wrapper: ({ children }) => <AuthProvider>{children}<Probe /><Flags /></AuthProvider>,
    })
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('ready'))
    let settle!: () => void
    const response = new Promise<typeof enabledUser>((resolve, reject) => {
      settle = operation === 'login'
        ? () => resolve(enabledUser)
        : () => reject(new Error('late refresh failure'))
    })
    if (operation === 'login') vi.spyOn(client, 'login').mockReturnValueOnce(response)
    else me.mockReturnValueOnce(response)
    let pending!: Promise<void>
    await act(async () => {
      pending = operation === 'login' ? result.current.login('admin', 'pw') : result.current.refresh()
    })
    await act(async () => { await result.current.logout() })
    expect(screen.getByTestId('status')).toHaveTextContent('anonymous')
    await act(async () => { settle(); await pending })
    expect(screen.getByTestId('status')).toHaveTextContent('anonymous')
    expect(screen.getByTestId('user')).toHaveTextContent('none')
    expect(screen.getByTestId('flags')).toHaveTextContent('false/false/false')
    expect(screen.getByTestId('error')).toBeEmptyDOMElement()
  })
})

describe('comparison retry identity on session replacement', () => {
  const nextUser = { ...enabledUser, user_id: 'usr_next', username: 'next user' }
  const previousAttempt: ComparisonSaveAttemptV2 = {
    version: 2, userId: enabledUser.user_id, digest: 'a'.repeat(64), generation: '1', sourceRecordIds: ['rec_previoussource'],
    payloadDigest: '1'.repeat(64), allocationKey: 'previous-allocation', requestDigest: '2'.repeat(64), idempotencyKey: 'previous-save',
  }
  const nextAttempt: ComparisonSaveAttemptV2 = {
    version: 2, userId: nextUser.user_id, digest: 'b'.repeat(64), generation: '1', sourceRecordIds: ['rec_nextsource'],
    payloadDigest: '3'.repeat(64), allocationKey: 'next-allocation', requestDigest: '4'.repeat(64), idempotencyKey: 'next-save',
  }
  beforeEach(() => {
    vi.restoreAllMocks()
    sessionStorage.clear()
    writeComparisonSaveAttempt(previousAttempt)
    writeComparisonSaveAttempt(nextAttempt)
  })
  afterEach(() => {
    vi.restoreAllMocks()
    sessionStorage.clear()
  })

  it('retains an uncertain save through 401 and same-user login, but clears it before another user enters', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    vi.spyOn(client, 'login').mockResolvedValueOnce(enabledUser).mockResolvedValueOnce(nextUser)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 401 }))
    const { result } = renderHook(() => useAuth(), {
      wrapper: ({ children }) => <AuthProvider>{children}<Probe /></AuthProvider>,
    })
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    await act(async () => { await expect(requestEmpty('/api/records')).rejects.toMatchObject({ status: 401 }) })
    expect(readComparisonSaveAttempt(previousAttempt)).toEqual(previousAttempt)
    await act(async () => { await result.current.login('admin', 'pw') })
    expect(readComparisonSaveAttempt(previousAttempt)).toEqual(previousAttempt)
    await act(async () => { await expect(requestEmpty('/api/records')).rejects.toMatchObject({ status: 401 }) })
    await act(async () => { await result.current.login('next', 'pw') })
    expect(readComparisonSaveAttempt(previousAttempt)).toBeNull()
    expect(readComparisonSaveAttempt(nextAttempt)).toEqual(nextAttempt)
    expect(screen.getByTestId('user')).toHaveTextContent('next user')
  })

  it.each(['refresh', 'login'] as const)('retires only the previous identity before applying %s', async (operation) => {
    const me = vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    vi.spyOn(client, 'login').mockResolvedValue(nextUser)
    render(<AuthProvider><Probe /></AuthProvider>)
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    me.mockResolvedValue(nextUser)
    fireEvent.click(screen.getByText(operation === 'refresh' ? 'refresh' : 'in'))
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('next user'))
    expect(screen.getByTestId('status')).toHaveTextContent('ready')
    expect(readComparisonSaveAttempt(previousAttempt)).toBeNull()
    expect(readComparisonSaveAttempt(nextAttempt)).toEqual(nextAttempt)
  })

  it.each(['refresh', 'login'] as const)('does not apply the replacement identity when %s cannot retire old metadata', async (operation) => {
    const me = vi.spyOn(client, 'me').mockResolvedValue(enabledUser)
    vi.spyOn(client, 'login').mockResolvedValue(nextUser)
    const { result } = renderHook(() => useAuth(), {
      wrapper: ({ children }) => <AuthProvider>{children}<Probe /><Flags /></AuthProvider>,
    })
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
    me.mockResolvedValue(nextUser)
    const remove = vi.spyOn(Storage.prototype, 'removeItem').mockImplementationOnce(() => {
      throw new Error('session storage unavailable')
    })
    await act(async () => {
      if (operation === 'login') {
        await expect(result.current.login('next user', 'test password')).rejects.toThrow('清理尚未完成')
      } else {
        await result.current.refresh()
      }
    })
    expect(screen.getByTestId('status')).toHaveTextContent('error')
    expect(screen.getByTestId('user')).toHaveTextContent('none')
    expect(screen.getByTestId('flags')).toHaveTextContent('false/false/false')
    expect(screen.getByTestId('error')).toHaveTextContent('比较重试状态清理失败')
    expect(readComparisonSaveAttempt(previousAttempt)).toEqual(previousAttempt)
    expect(readComparisonSaveAttempt(nextAttempt)).toEqual(nextAttempt)
    remove.mockRestore()
    await act(async () => { await result.current.retry() })
    expect(readComparisonSaveAttempt(previousAttempt)).toBeNull()
    expect(readComparisonSaveAttempt(nextAttempt)).toEqual(nextAttempt)
    expect(screen.getByTestId('status')).toHaveTextContent('anonymous')
    await act(async () => { await result.current.refresh() })
    expect(screen.getByTestId('user')).toHaveTextContent('next user')
    expect(screen.getByTestId('status')).toHaveTextContent('ready')
  })
})
