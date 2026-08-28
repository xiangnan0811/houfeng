import { afterEach, describe, expect, it, vi } from 'vitest'

import { createVPSWriteOwnerStore } from './vpsWriteOwnerStore'

describe('VPS write owner store', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('captures an ISO startedAt at provisional begin and preserves it through create preparation', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-28T08:09:10.123Z'))
    const store = createVPSWriteOwnerStore()

    const owner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'legacy-view',
      generation: 1,
      operation: 'subscription',
    })

    expect(owner).toHaveProperty('startedAt', '2026-08-28T08:09:10.123Z')
    if (!owner) return
    const preparedOwner = await store.prepareCreate(owner, { provider_id: 'provider_a' })
    expect(preparedOwner).toHaveProperty('startedAt', '2026-08-28T08:09:10.123Z')
  })

  it('isolates registry instances and only releases the exact VPS/token owner', () => {
    const store = createVPSWriteOwnerStore()
    const otherRegistry = createVPSWriteOwnerStore()
    const listener = vi.fn()
    const unsubscribe = store.subscribe(listener)

    const ownerA = store.begin({
      vpsId: 'vps_a',
      viewToken: 'legacy-view',
      generation: 1,
      operation: 'subscription',
    })
    const ownerB = store.begin({
      vpsId: 'vps_b',
      viewToken: 'legacy-view',
      generation: 2,
      operation: 'facts',
    })

    expect(ownerA).not.toBeNull()
    expect(ownerB).not.toBeNull()
    expect(store.begin({ vpsId: 'vps_a', viewToken: 'overview-view', generation: 3, operation: 'domain' })).toBeNull()
    expect(otherRegistry.begin({
      vpsId: 'vps_a',
      viewToken: 'overview-view',
      generation: 1,
      operation: 'domain',
    })).not.toBeNull()
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

  it('keeps canonical create attempts without retaining raw wire bodies', async () => {
    const store = createVPSWriteOwnerStore()
    const owner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'legacy-view',
      generation: 1,
      operation: 'experience',
    })
    expect(owner).not.toBeNull()
    if (!owner) return

    const pendingAttempt = store.prepareCreate(owner, {
      note: 'raw-secret-note',
      occurred_at: '2026-08-28T01:02:03Z',
    })
    expect(store.begin({
      vpsId: 'vps_a',
      viewToken: 'overview-view',
      generation: 2,
      operation: 'subscription',
    })).toBeNull()

    const firstAttempt = await pendingAttempt
    const canonicalRequest = JSON.stringify({
      body: {
        note: 'raw-secret-note',
        occurred_at: '2026-08-28T01:02:03Z',
      },
      operation: 'experience',
      vpsId: 'vps_a',
    })
    const expectedDigestBuffer = await crypto.subtle.digest(
      'SHA-256',
      new TextEncoder().encode(canonicalRequest),
    )
    const expectedDigest = Array.from(
      new Uint8Array(expectedDigestBuffer),
      (byte) => byte.toString(16).padStart(2, '0'),
    ).join('')
    expect(firstAttempt).toMatchObject({
      token: owner.token,
      requestDigest: expectedDigest,
      idempotencyKey: expect.any(String),
    })
    const serializedSnapshot = JSON.stringify(Array.from(store.getSnapshot().values()))
    expect(serializedSnapshot).not.toContain('raw-secret-note')
    expect(serializedSnapshot).not.toContain(expectedDigest)
    expect(serializedSnapshot).not.toContain(firstAttempt?.idempotencyKey ?? 'unreachable-attempt-key')
    expect(store.getSnapshot().get('vps_a')).not.toHaveProperty('requestDigest')
    expect(store.getSnapshot().get('vps_a')).not.toHaveProperty('idempotencyKey')
    expect(firstAttempt).not.toHaveProperty('wireBody')
    expect(firstAttempt).not.toHaveProperty('body')

    expect(store.finishCreate(firstAttempt!, 'unknown')).toBe(true)

    const retryOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'overview-view',
      generation: 2,
      operation: 'experience',
    })
    expect(retryOwner).not.toBeNull()
    if (!retryOwner || !firstAttempt) return

    const retryAttempt = await store.prepareCreate(retryOwner, {
      occurred_at: '2026-08-28T01:02:03Z',
      note: 'raw-secret-note',
    })
    expect(retryAttempt?.requestDigest).toBe(firstAttempt.requestDigest)
    expect(retryAttempt?.idempotencyKey).toBe(firstAttempt.idempotencyKey)
    expect(store.finishCreate(retryAttempt!, 'unknown')).toBe(true)

    const changedOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'overview-view',
      generation: 3,
      operation: 'experience',
    })
    expect(changedOwner).not.toBeNull()
    if (!changedOwner) return

    const changedAttempt = await store.prepareCreate(changedOwner, {
      note: 'changed-note',
      occurred_at: '2026-08-28T01:02:03Z',
    })
    expect(changedAttempt?.requestDigest).not.toBe(firstAttempt.requestDigest)
    expect(changedAttempt?.idempotencyKey).not.toBe(firstAttempt.idempotencyKey)
    expect(store.finishCreate(changedAttempt!, 'idempotency_key_reused')).toBe(true)

    const rotatedOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'overview-view',
      generation: 4,
      operation: 'experience',
    })
    expect(rotatedOwner).not.toBeNull()
    if (!rotatedOwner || !changedAttempt) return

    const rotatedAttempt = await store.prepareCreate(rotatedOwner, {
      occurred_at: '2026-08-28T01:02:03Z',
      note: 'changed-note',
    })
    expect(rotatedAttempt?.requestDigest).toBe(changedAttempt.requestDigest)
    expect(rotatedAttempt?.idempotencyKey).not.toBe(changedAttempt.idempotencyKey)
    expect(store.finishCreate(rotatedAttempt!, 'confirmed')).toBe(true)

    const freshOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'overview-view',
      generation: 5,
      operation: 'experience',
    })
    expect(freshOwner).not.toBeNull()
    if (!freshOwner || !rotatedAttempt) return

    const freshAttempt = await store.prepareCreate(freshOwner, {
      note: 'changed-note',
      occurred_at: '2026-08-28T01:02:03Z',
    })
    expect(freshAttempt?.idempotencyKey).not.toBe(rotatedAttempt.idempotencyKey)
  })

  it('does not publish prepared create secrets when an enriched owner begins a later write', async () => {
    const store = createVPSWriteOwnerStore()
    const owner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'legacy-view',
      generation: 1,
      operation: 'service',
    })
    expect(owner).not.toBeNull()
    if (!owner) return

    const preparedOwner = await store.prepareCreate(owner, { service_name: 'ssh' })
    expect(preparedOwner).not.toBeNull()
    if (!preparedOwner) return
    expect(store.finishCreate(preparedOwner, 'unknown')).toBe(true)

    const laterOwner = store.begin(preparedOwner)
    expect(laterOwner).not.toBeNull()
    expect(laterOwner).not.toHaveProperty('requestDigest')
    expect(laterOwner).not.toHaveProperty('idempotencyKey')
    expect(store.getSnapshot().get('vps_a')).not.toHaveProperty('requestDigest')
    expect(store.getSnapshot().get('vps_a')).not.toHaveProperty('idempotencyKey')
  })

  it('does not let an exact-token stale create settle release or clear a newer owner', async () => {
    const store = createVPSWriteOwnerStore()
    const oldOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'old-view',
      generation: 1,
      operation: 'service',
    })
    expect(oldOwner).not.toBeNull()
    if (!oldOwner) return

    const oldAttempt = await store.prepareCreate(oldOwner, { service_name: 'ssh' })
    expect(store.finishCreate(oldAttempt!, 'unknown')).toBe(true)

    const newerOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'new-view',
      generation: 2,
      operation: 'service',
    })
    expect(newerOwner).not.toBeNull()
    if (!newerOwner || !oldAttempt) return

    const newerAttempt = await store.prepareCreate(newerOwner, { service_name: 'ssh' })
    expect(store.finishCreate(oldAttempt, 'confirmed')).toBe(false)
    expect(store.getSnapshot().get('vps_a')).toMatchObject({ token: newerAttempt?.token })
    expect(store.getSnapshot().get('vps_a')).not.toHaveProperty('requestDigest')
    expect(store.getSnapshot().get('vps_a')).not.toHaveProperty('idempotencyKey')
    expect(store.finishCreate(newerAttempt!, 'unknown')).toBe(true)

    const retryOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'newer-view',
      generation: 3,
      operation: 'service',
    })
    expect(retryOwner).not.toBeNull()
    if (!retryOwner || !newerAttempt) return
    const retryAttempt = await store.prepareCreate(retryOwner, { service_name: 'ssh' })
    expect(retryAttempt?.idempotencyKey).toBe(newerAttempt.idempotencyKey)
  })

  it('keeps an unknown attempt key when its identical retry becomes not-sent after preparation', async () => {
    const store = createVPSWriteOwnerStore()
    const wireBody = { service_name: 'ssh' }
    const firstOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'old-view',
      generation: 1,
      operation: 'service',
    })
    expect(firstOwner).not.toBeNull()
    if (!firstOwner) return
    const unknownAttempt = await store.prepareCreate(firstOwner, wireBody)
    expect(store.finishCreate(unknownAttempt!, 'unknown')).toBe(true)

    const staleRetryOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'stale-view',
      generation: 2,
      operation: 'service',
    })
    expect(staleRetryOwner).not.toBeNull()
    if (!staleRetryOwner || !unknownAttempt) return
    const staleRetryAttempt = await store.prepareCreate(staleRetryOwner, wireBody)
    expect(staleRetryAttempt?.idempotencyKey).toBe(unknownAttempt.idempotencyKey)
    expect(store.finishCreate(staleRetryAttempt!, 'not_sent')).toBe(true)

    const nextRetryOwner = store.begin({
      vpsId: 'vps_a',
      viewToken: 'current-view',
      generation: 3,
      operation: 'service',
    })
    expect(nextRetryOwner).not.toBeNull()
    if (!nextRetryOwner) return
    const nextRetryAttempt = await store.prepareCreate(nextRetryOwner, wireBody)
    expect(nextRetryAttempt?.idempotencyKey).toBe(unknownAttempt.idempotencyKey)
  })
})
