import { waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import {
  broadcastRecordCleanup,
  broadcastRecordInvalidation,
  broadcastRecordSessionEnd,
  createRecordSecurityController,
  observeRecordClientTargets,
  registerRecordClientTarget,
} from './recordSecurity'

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

  it('reconciles and invalidates reserved-domain records while refusing malformed identities', () => {
    const reserved = `rec2_c${'0'.repeat(31)}`
    const observed: string[][] = []
    const stop = observeRecordClientTargets('usr_reserved', (ids) => observed.push([...ids]))
    const onRevoke = vi.fn()
    const controller = createRecordSecurityController(reserved, 'usr_reserved', 0, onRevoke)
    const malformed = createRecordSecurityController(`rec2_${'0'.repeat(31)}`, 'usr_reserved', 0, vi.fn())
    expect(observed).toEqual([[], [reserved]])
    expect(() => broadcastRecordInvalidation('usr_reserved', `rec2_${'C'.repeat(32)}`)).toThrow('Invalid record identity')
    expect(onRevoke).not.toHaveBeenCalled()
    broadcastRecordInvalidation('usr_reserved', reserved)
    expect(onRevoke).toHaveBeenCalledWith('cleanup')
    expect(controller.signal.aborted).toBe(true)
    stop()
    malformed.dispose()
  })

  it('does not announce a provisional binding as an authorized record target', () => {
    const target = `rec2_8${'6'.repeat(31)}`
    const first: string[][] = []
    const stop = observeRecordClientTargets('usr_provisional', (ids) => first.push([...ids]))
    const controller = createRecordSecurityController('new', 'usr_provisional', 0, vi.fn())
    expect(controller.bindRecordId(target)).toBe(true)
    const later: string[][] = []
    const stopLater = observeRecordClientTargets('usr_provisional', (ids) => later.push([...ids]))
    expect(first).toEqual([[]])
    expect(later).toEqual([[]])
    registerRecordClientTarget('usr_provisional', target)
    expect(first).toEqual([[], [target]])
    expect(later).toEqual([[], [target]])
    const after: string[][] = []
    const stopAfter = observeRecordClientTargets('usr_provisional', (ids) => after.push([...ids]))
    expect(after).toEqual([[target]])
    stop()
    stopLater()
    stopAfter()
    controller.dispose()
  })

  it('binds a live destination, isolates users, and forgets an explicitly detached destination', () => {
    const first = `rec2_8${'1'.repeat(31)}`
    const second = `rec2_8${'2'.repeat(31)}`
    const revoked = vi.fn()
    const other = vi.fn()
    const controller = createRecordSecurityController('new', 'usr_binding', 0, revoked)
    const otherUser = createRecordSecurityController('new', 'usr_otherbinding', 0, other)
    expect(controller.bindRecordId(first)).toBe(true)
    expect(otherUser.bindRecordId(first)).toBe(true)
    expect(controller.bindRecordId('new')).toBe(true)
    expect(controller.bindRecordId(second)).toBe(true)
    broadcastRecordCleanup('usr_binding', {
      version: 1, cursor: '1', operation_id: 'rpo_oldbinding', record_id: first,
      cleanup_epoch: '1', scopes: ['markdown_workspace'],
    })
    expect(revoked).not.toHaveBeenCalled()
    expect(other).not.toHaveBeenCalled()
    broadcastRecordCleanup('usr_binding', {
      version: 1, cursor: '2', operation_id: 'rpo_newbinding', record_id: second,
      cleanup_epoch: '2', scopes: ['markdown_workspace'],
    })
    expect(revoked).toHaveBeenCalledWith('cleanup')
    expect(controller.signal.aborted).toBe(true)
    expect(other).not.toHaveBeenCalled()
    otherUser.dispose()
  })

  it('closes a destination bound after its cleanup was received during storage lookup', () => {
    const target = `rec2_8${'3'.repeat(31)}`
    const revoked = vi.fn()
    const controller = createRecordSecurityController('new', 'usr_latebinding', 0, revoked)
    broadcastRecordCleanup('usr_latebinding', {
      version: 1, cursor: '1', operation_id: 'rpo_latebinding', record_id: target,
      cleanup_epoch: '1', scopes: ['markdown_workspace'],
    })
    expect(controller.bindRecordId(target)).toBe(false)
    expect(revoked).toHaveBeenCalledWith('cleanup')
    expect(controller.signal.aborted).toBe(true)
  })

  it('remembers a cross-tab cleanup while another controller awaits its destination', async () => {
    const target = `rec2_8${'5'.repeat(31)}`
    const waitingRevoked = vi.fn()
    const received = vi.fn()
    const waiting = createRecordSecurityController('new', 'usr_remotelookup', 0, waitingRevoked)
    createRecordSecurityController(target, 'usr_remotelookup', 0, received)
    const remote = new BroadcastChannel('houfeng-record-security')
    remote.postMessage({ type: 'cleanup', userId: 'usr_remotelookup', event: {
      version: 1, cursor: '1', operation_id: 'rpo_remotelookup', record_id: target,
      cleanup_epoch: '1', scopes: ['markdown_workspace'],
    } })
    await waitFor(() => expect(received).toHaveBeenCalledWith('cleanup'))
    expect(waiting.bindRecordId(target)).toBe(false)
    expect(waitingRevoked).toHaveBeenCalledWith('cleanup')
    remote.close()
  })

  it('closes a rebound destination on a cleanup message from another tab', async () => {
    const target = `rec2_8${'4'.repeat(31)}`
    const revoked = vi.fn()
    const controller = createRecordSecurityController('new', 'usr_remotebinding', 0, revoked)
    expect(controller.bindRecordId(target)).toBe(true)
    const remote = new BroadcastChannel('houfeng-record-security')
    remote.postMessage({
      type: 'cleanup', userId: 'usr_remotebinding', event: {
        version: 1, cursor: '1', operation_id: 'rpo_remotebinding', record_id: target,
        cleanup_epoch: '1', scopes: ['markdown_workspace'],
      },
    })
    await waitFor(() => expect(revoked).toHaveBeenCalledWith('cleanup'))
    expect(controller.signal.aborted).toBe(true)
    remote.close()
  })
})
