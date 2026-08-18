import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AuthProvider, useAuth } from './auth-context'
import * as client from './auth-client'

function Probe() {
  const { user, loading, login, logout } = useAuth()
  if (loading) return <div>loading</div>
  return (
    <div>
      <span data-testid="user">{user?.username ?? 'none'}</span>
      <button onClick={() => { void login('admin', 'pw') }}>in</button>
      <button onClick={() => { void logout() }}>out</button>
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

  it('boots with /me result', async () => {
    vi.spyOn(client, 'me').mockResolvedValue({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '',
    })
    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )
    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('admin'))
  })

  it('finishes boot in the signed-out state when /me fails', async () => {
    vi.spyOn(client, 'me').mockRejectedValue(new Error('auth service unavailable'))

    render(
      <AuthProvider>
        <Probe />
      </AuthProvider>,
    )

    await waitFor(() => expect(screen.getByTestId('user')).toHaveTextContent('none'))
  })

  it('sets user after login', async () => {
    vi.spyOn(client, 'me').mockResolvedValue(null)
    vi.spyOn(client, 'login').mockResolvedValue({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '',
    })
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
    vi.spyOn(client, 'me').mockResolvedValue({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '',
    })
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
