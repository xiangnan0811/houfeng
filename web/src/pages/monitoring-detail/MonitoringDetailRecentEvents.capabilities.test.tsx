import { act, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import * as authClient from '../../lib/auth-client'
import { AuthProvider } from '../../lib/auth-context'
import { MonitoringDetailRecentEvents } from './MonitoringDetailRecentEvents'

describe('MonitoringDetailRecentEvents runtime capabilities', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('keeps history and hides record links when records are off', async () => {
    const me = vi.spyOn(authClient, 'me').mockResolvedValue({
      user_id: 'u1',
      username: 'admin',
      role: 'admin',
      display_name: '',
      runtime_capabilities: { records: false, comparison: false, portability: false },
      management_capabilities: { access: false },
    })

    render(
      <AuthProvider>
        <MemoryRouter>
          <MonitoringDetailRecentEvents
            subjectBase="/monitoring/mi_001"
            returnVPSId={null}
            events={[]}
            eventsError={null}
            eventsLoaded
            eventsRetrying={false}
            onRetryEvents={vi.fn()}
            onOpenHistory={vi.fn()}
          />
        </MemoryRouter>
      </AuthProvider>,
    )

    await waitFor(() => expect(me).toHaveBeenCalled())
    await act(async () => {
      await me.mock.results[0]?.value
    })

    expect(screen.getByRole('navigation', { name: '历史' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '历史' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '活动' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '记录' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '证据' })).not.toBeInTheDocument()
  })
})
