import { waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { broadcastRecordSessionEnd, createRecordSecurityController } from './recordSecurity'

describe('recordSecurity', () => {
  it('aborts inflight work and broadcasts revoke once', () => {
    const onRevoke = vi.fn()
    const controller = createRecordSecurityController('rec_001', 'usr_1', 3, onRevoke)
    expect(controller.lease.revoked).toBe(false)
    controller.revoke('visibility')
    expect(controller.lease.revoked).toBe(true)
    expect(controller.abort.signal.aborted).toBe(true)
    expect(onRevoke).toHaveBeenCalledWith('visibility')
    controller.revoke('logout')
    expect(onRevoke).toHaveBeenCalledTimes(1)
  })

  it('disposes the local lease without broadcasting a logout', () => {
    const onRevoke = vi.fn()
    const other = vi.fn()
    const controller = createRecordSecurityController('rec_001', 'usr_1', 3, onRevoke)
    const sibling = createRecordSecurityController('rec_001', 'usr_1', 3, other)
    controller.dispose()
    expect(controller.lease.revoked).toBe(true)
    expect(controller.abort.signal.aborted).toBe(true)
    expect(onRevoke).not.toHaveBeenCalled()
    expect(other).not.toHaveBeenCalled()
    sibling.dispose()
  })

  it('revokes every open lease for the user on logout', async () => {
    const onRevoke = vi.fn()
    const controller = createRecordSecurityController('rec_001', 'usr_1', 3, onRevoke)
    broadcastRecordSessionEnd('usr_1', 'logout')
    await waitFor(() => expect(onRevoke).toHaveBeenCalledWith('logout'))
    controller.dispose()
  })

  it('ignores a revoke broadcast from an older authorization epoch', async () => {
    const stale = vi.fn()
    const current = vi.fn()
    const controller = createRecordSecurityController('rec_001', 'usr_1', 4, stale)
    const sibling = createRecordSecurityController('rec_001', 'usr_1', 2, current)
    sibling.revoke('revoke')
    await waitFor(() => expect(current).toHaveBeenCalledWith('revoke'))
    expect(stale).not.toHaveBeenCalled()
    expect(controller.lease.revoked).toBe(false)
    controller.dispose()
  })

  it('revokes when the broadcast carries the same or a newer epoch', async () => {
    const onRevoke = vi.fn()
    const controller = createRecordSecurityController('rec_001', 'usr_1', 2, onRevoke)
    const sibling = createRecordSecurityController('rec_001', 'usr_1', 5, vi.fn())
    sibling.revoke('revoke')
    await waitFor(() => expect(onRevoke).toHaveBeenCalledWith('revoke'))
    controller.dispose()
  })
})
