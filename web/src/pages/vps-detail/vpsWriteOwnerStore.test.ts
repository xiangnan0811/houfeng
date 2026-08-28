import { describe, expect, it, vi } from 'vitest'

import { createVPSWriteOwnerStore } from './vpsWriteOwnerStore'

describe('VPS write owner store', () => {
  it('isolates page instances and only releases the exact VPS/token owner', () => {
    const store = createVPSWriteOwnerStore()
    const otherPageStore = createVPSWriteOwnerStore()
    const listener = vi.fn()
    const unsubscribe = store.subscribe(listener)

    const ownerA = store.begin({
      vpsId: 'vps_a',
      generation: 1,
      operation: 'subscription',
    })
    const ownerB = store.begin({
      vpsId: 'vps_b',
      generation: 2,
      operation: 'facts',
    })

    expect(ownerA).not.toBeNull()
    expect(ownerB).not.toBeNull()
    expect(store.begin({ vpsId: 'vps_a', generation: 3, operation: 'domain' })).toBeNull()
    expect(otherPageStore.begin({ vpsId: 'vps_a', generation: 1, operation: 'domain' })).not.toBeNull()
    expect(listener).toHaveBeenCalledTimes(2)

    if (!ownerA || !ownerB) throw new Error('owners must be acquired')
    expect(store.finish({ ...ownerA, token: 'stale-token' })).toBe(false)
    expect(store.getSnapshot().get('vps_a')).toBe(ownerA)

    expect(store.finish(ownerA)).toBe(true)
    expect(store.getSnapshot().has('vps_a')).toBe(false)
    expect(store.getSnapshot().get('vps_b')).toBe(ownerB)
    expect(listener).toHaveBeenCalledTimes(3)

    unsubscribe()
    expect(store.finish(ownerB)).toBe(true)
    expect(listener).toHaveBeenCalledTimes(3)
  })
})
