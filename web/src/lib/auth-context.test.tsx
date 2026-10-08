import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import * as client from './auth-client'
import { capabilityFlagsForSession } from './auth-client'
import { AuthProvider, useAuth } from './auth-context'

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
  })
})
