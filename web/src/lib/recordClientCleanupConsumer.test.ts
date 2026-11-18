import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { waitFor } from '@testing-library/react'
import { memoryDraftBufferStore, type DraftCleanupEvent } from '../pages/records/draftBuffer'
import { emptyRecordDraftPayload } from '../pages/records/recordPayload'
import { recordAllocationFixture } from '../pages/records/testFixtures'
import type { RecordCleanupConsumer, startRecordClientCleanup as startConsumerSignature } from './recordClientCleanupConsumer'
import { readComparisonSaveAttempt, type LegacyComparisonSaveAttempt } from './comparisonSaveAttempts'

class CleanupSocket {
  static OPEN = 1
  static instances: CleanupSocket[] = []
  readyState = 1
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  sent: string[] = []
  readonly url: URL
  constructor(url: URL) { this.url = url; CleanupSocket.instances.push(this) }
  send(value: string) { this.sent.push(value) }
  close() { this.readyState = 3; this.onclose?.() }
  receive(value: unknown) { this.onmessage?.({ data: JSON.stringify(value) }) }
}
const consumers: RecordCleanupConsumer[] = []
type StartConsumer = typeof startConsumerSignature
let startRecordClientCleanup: StartConsumer

// Each fresh module instance stands for one browsing document (tab, opened
// window, or reload); the jsdom sessionStorage persists across them exactly as
// an opener's copied or reloaded tab storage would. A static import cannot
// provide distinct module instances, hence the dynamic load after reset.
async function loadDocument(): Promise<StartConsumer> {
  vi.resetModules()
  return (await import('./recordClientCleanupConsumer')).startRecordClientCleanup
}
function copySessionStorage() {
  const copied = new Map<string, string>()
  for (let index = 0; index < sessionStorage.length; index += 1) {
    const key = sessionStorage.key(index)!
    copied.set(key, sessionStorage.getItem(key)!)
  }
  return () => { sessionStorage.clear(); for (const [key, item] of copied) sessionStorage.setItem(key, item) }
}
const event: DraftCleanupEvent = { version: 1, cursor: '9007199254740993', operation_id: 'rpo_cleanup', record_id: 'rec_source', cleanup_epoch: '9007199254740995', scopes: ['markdown_draft_buffer'] }
const value = { key: 'usr_a:rec_source', userId: 'usr_a', recordId: 'rec_source', payload: emptyRecordDraftPayload('usr_a'), updatedAt: Date.now() }

beforeEach(async () => {
  startRecordClientCleanup = await loadDocument()
  sessionStorage.clear()
  CleanupSocket.instances = []
  vi.stubGlobal('WebSocket', CleanupSocket)
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
    const body = JSON.parse(init?.body as string) as { record_ids: string[]; allocation_references?: { record_id: string }[] }
    return new Response(JSON.stringify({ allowed: body.record_ids, invalidate: [], pending: (body.allocation_references ?? []).map((item) => item.record_id) }), { status: 200 })
  })
})
afterEach(() => { for (const consumer of consumers.splice(0)) consumer.stop(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

async function start(store = memoryDraftBufferStore(), onError = vi.fn(), startConsumer = startRecordClientCleanup) {
  const consumer = startConsumer('usr_a', { store, onError })
  consumers.push(consumer)
  await consumer.ready
  return { consumer, store, onError, socket: CleanupSocket.instances.at(-1)! }
}

describe('authenticated cleanup consumer', () => {
  it('acknowledges only durable ordered application and preserves exact bigint identities', async () => {
    const store = memoryDraftBufferStore([value])
    let release!: () => void
    const barrier = new Promise<void>((resolve) => { release = resolve })
    const apply = store.applyCleanup.bind(store)
    vi.spyOn(store, 'applyCleanup').mockImplementationOnce(async (...args) => { await barrier; return apply(...args) })
    const { socket, onError } = await start(store)
    socket.receive({ type: 'cleanup', event })
    socket.receive({ type: 'cleanup', event: { ...event, cursor: '9007199254740994', operation_id: 'rpo_second', record_id: 'rec_second' } })
    await waitFor(() => expect(store.applyCleanup).toHaveBeenCalledTimes(1))
    expect(socket.sent).toEqual([])
    expect(await store.cleanupCursor('usr_a', socket.url.searchParams.get('browser_id')!)).toBeUndefined()
    release()
    await waitFor(() => expect(socket.sent).toHaveLength(2))
    expect(socket.sent.map((message) => JSON.parse(message).ack.cursor)).toEqual(['9007199254740993', '9007199254740994'])
    expect(JSON.parse(socket.sent[0]!).ack.cleanup_epoch).toBe('9007199254740995')
    expect(await store.get(value.key)).toBeUndefined()
    expect(onError).not.toHaveBeenCalled()
  })

  it('does not acknowledge transaction failure or turn disconnection into draft deletion', async () => {
    const store = memoryDraftBufferStore([value])
    vi.spyOn(store, 'applyCleanup').mockRejectedValueOnce(new Error('transaction aborted'))
    const { socket, onError } = await start(store)
    socket.receive({ type: 'cleanup', event })
    await waitFor(() => expect(onError).toHaveBeenCalled())
    expect(socket.sent).toEqual([])
    expect(await store.get(value.key)).toEqual(value)
    expect(await store.cleanupCursor('usr_a', socket.url.searchParams.get('browser_id')!)).toBeUndefined()
    const resumed = await start(store)
    resumed.socket.close()
    await waitFor(() => expect(resumed.onError).toHaveBeenCalled())
    expect(await store.get(value.key)).toEqual(value)
  })

  it('reconciles an unknown cursor under a new browser identity instead of silently skipping events', async () => {
    const store = memoryDraftBufferStore([value])
    const { socket, onError } = await start(store)
    socket.receive({ type: 'reconcile_required' })
    socket.close()
    await waitFor(() => expect(CleanupSocket.instances).toHaveLength(2))
    const successor = CleanupSocket.instances[1]!
    expect(successor.url.searchParams.get('browser_id')).not.toBe(socket.url.searchParams.get('browser_id'))
    expect(successor.url.searchParams.has('cursor')).toBe(false)
    expect(await store.get(value.key)).toEqual(value)
    successor.receive({ type: 'cleanup', event })
    await waitFor(() => expect(successor.sent).toHaveLength(1))
    expect(await store.get(value.key)).toBeUndefined()
    expect(onError).not.toHaveBeenCalled()
  })

  it('gives an opened tab with copied sessionStorage its own identity instead of the opener cursor', async () => {
    const store = memoryDraftBufferStore([value])
    const opener = await start(store)
    const openerID = opener.socket.url.searchParams.get('browser_id')!
    opener.socket.receive({ type: 'cleanup', event })
    await waitFor(() => expect(opener.socket.sent).toHaveLength(1))
    expect(await store.cleanupCursor('usr_a', openerID)).toBe(event.cursor)
    // Earlier builds persisted the identity here; an opener copy carries it.
    sessionStorage.setItem('houfeng.record-cleanup/browser/usr_a', openerID)
    const restoreCopy = copySessionStorage()

    restoreCopy()
    const opened = await start(store, vi.fn(), await loadDocument())
    const openedID = opened.socket.url.searchParams.get('browser_id')!
    expect(openedID).not.toBe(openerID)
    expect(openedID).toMatch(/^[a-z0-9_-]{1,128}$/)
    expect(opened.socket.url.searchParams.has('cursor')).toBe(false)

    restoreCopy()
    const sibling = await start(store, vi.fn(), await loadDocument())
    const siblingID = sibling.socket.url.searchParams.get('browser_id')!
    expect(new Set([openerID, openedID, siblingID]).size).toBe(3)
    expect(sibling.socket.url.searchParams.has('cursor')).toBe(false)
    expect(opener.onError).not.toHaveBeenCalled()
    expect(opened.onError).not.toHaveBeenCalled()
    expect(sibling.onError).not.toHaveBeenCalled()
  })

  it('starts a reloaded document under a fresh identity even though sessionStorage survived', async () => {
    const store = memoryDraftBufferStore([value])
    const before = await start(store)
    const beforeID = before.socket.url.searchParams.get('browser_id')!
    before.socket.receive({ type: 'cleanup', event })
    await waitFor(() => expect(before.socket.sent).toHaveLength(1))
    before.consumer.stop()

    const reloaded = await start(store, vi.fn(), await loadDocument())
    expect(reloaded.socket.url.searchParams.get('browser_id')).not.toBe(beforeID)
    expect(reloaded.socket.url.searchParams.has('cursor')).toBe(false)
    expect(reloaded.onError).not.toHaveBeenCalled()
  })

  it('keeps the same identity and durable cursor when restarted within one document', async () => {
    const store = memoryDraftBufferStore([value])
    const first = await start(store)
    const firstID = first.socket.url.searchParams.get('browser_id')!
    first.socket.receive({ type: 'cleanup', event })
    await waitFor(() => expect(first.socket.sent).toHaveLength(1))
    first.consumer.stop()

    const restarted = await start(store)
    expect(restarted.socket.url.searchParams.get('browser_id')).toBe(firstID)
    expect(restarted.socket.url.searchParams.get('cursor')).toBe(event.cursor)
    expect(first.onError).not.toHaveBeenCalled()
    expect(restarted.onError).not.toHaveBeenCalled()
  })

  it('keeps the rotated identity after an unknown-cursor reconcile when restarted in the same document', async () => {
    const store = memoryDraftBufferStore([value])
    const { consumer, socket, onError } = await start(store)
    const originalID = socket.url.searchParams.get('browser_id')!
    socket.receive({ type: 'reconcile_required' })
    socket.close()
    await waitFor(() => expect(CleanupSocket.instances).toHaveLength(2))
    const successor = CleanupSocket.instances[1]!
    const rotatedID = successor.url.searchParams.get('browser_id')!
    expect(rotatedID).not.toBe(originalID)
    successor.receive({ type: 'cleanup', event })
    await waitFor(() => expect(successor.sent).toHaveLength(1))
    consumer.stop()

    const restarted = await start(store)
    expect(restarted.socket.url.searchParams.get('browser_id')).toBe(rotatedID)
    expect(restarted.socket.url.searchParams.get('cursor')).toBe(event.cursor)
    expect(onError).not.toHaveBeenCalled()
    expect(restarted.onError).not.toHaveBeenCalled()
  })

  it('invalidates inaccessible startup copies without affecting another user or standalone drafts', async () => {
    const other = { ...value, key: 'usr_b:rec_source', userId: 'usr_b' }
    const standalone = { key: 'usr_a:new', userId: value.userId, payload: value.payload, updatedAt: value.updatedAt }
    const store = memoryDraftBufferStore([value, other, standalone])
    const oldScope = await store.captureScope('usr_a', 'rec_source')
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ allowed: [], invalidate: ['rec_source'], pending: [] }), { status: 200 }))
    await start(store)
    expect(await store.get(value.key)).toBeUndefined()
    expect(await store.get(other.key)).toEqual(other)
    expect(await store.get(standalone.key)).toEqual(standalone)
    await expect(store.set(value, oldScope)).rejects.toThrow('invalidated')
    await store.set(value, await store.captureScope('usr_a', 'rec_source'))
    expect(await store.get(value.key)).toEqual(value)
  })

  it('clears inaccessible legacy comparison retry state on startup without rewriting retained entries', async () => {
    const removed: LegacyComparisonSaveAttempt = {
      version: 1, userId: 'usr_a', digest: 'a'.repeat(64), generation: '1',
      recordId: 'rec_result', idempotencyKey: 'saved-response-lost', sourceRecordIds: ['rec_source'],
    }
    const retained: LegacyComparisonSaveAttempt = {
      ...removed, digest: 'b'.repeat(64), recordId: 'rec_retainedresult',
      idempotencyKey: 'retained-attempt', sourceRecordIds: ['rec_retainedsource'],
    }
    const retainedRaw = JSON.stringify(retained)
    sessionStorage.setItem(`houfeng.comparison-save/usr_a/${removed.digest}`, JSON.stringify(removed))
    sessionStorage.setItem(`houfeng.comparison-save/usr_a/${retained.digest}`, retainedRaw)
    const asked: string[][] = []
    vi.mocked(fetch).mockImplementationOnce(async (_input, init) => {
      const body = JSON.parse(init?.body as string) as { record_ids: string[] }
      asked.push(body.record_ids)
      return new Response(JSON.stringify({
        allowed: body.record_ids.filter((id) => id !== 'rec_source'), invalidate: ['rec_source'], pending: [],
      }), { status: 200 })
    })
    const { socket, onError } = await start()
    // A legacy target may never have committed; only existing sources are reconciled.
    expect(asked[0]).toEqual(['rec_retainedsource', 'rec_source'])
    expect(readComparisonSaveAttempt(removed)).toBeNull()
    expect(sessionStorage.getItem(`houfeng.comparison-save/usr_a/${retained.digest}`)).toBe(retainedRaw)
    expect(socket.readyState).toBe(CleanupSocket.OPEN)
    expect(onError).not.toHaveBeenCalled()
  })

  it('retains an unknown pre-root publication on startup and honors an actual cleanup event', async () => {
    const allocation = recordAllocationFixture('pending')
    const pending = {
      key: 'usr_a:new', userId: 'usr_a', payload: emptyRecordDraftPayload('usr_a'), updatedAt: Date.now(),
      draftId: 'rdf_sent', etag: 'etag-sent',
      publication: {
        version: 1 as const, phase: 'bound' as const, idempotencyKey: 'publish-1',
        publicationDigest: 'f'.repeat(64),
        request: { draft_id: 'rdf_sent', draft_etag: 'etag-sent', record_id: allocation.record_id, allocation },
      },
    }
    const store = memoryDraftBufferStore([pending])
    vi.mocked(fetch).mockImplementationOnce(async (_input, init) => {
      const body = JSON.parse(init?.body as string) as { record_ids: string[]; allocation_references: { record_id: string; allocation_id: string }[] }
      expect(body.record_ids).toEqual([])
      expect(body.allocation_references).toEqual([{ record_id: allocation.record_id, allocation_id: allocation.allocation_id }])
      return new Response(JSON.stringify({ allowed: [], invalidate: [], pending: [allocation.record_id] }), { status: 200 })
    })
    const { socket, onError } = await start(store)
    expect(await store.get(pending.key)).toEqual(pending)
    socket.receive({ type: 'cleanup', event: { ...event, record_id: allocation.record_id } })
    await waitFor(async () => expect(await store.get(pending.key)).toBeUndefined())
    expect(onError).not.toHaveBeenCalled()
  })

  it('registers a newly persisted allocation with the active browser without claiming read access', async () => {
    const { socket, onError } = await start()
    // Use the same freshly loaded module instance as the simulated document.
    const { registerRecordClientAllocation } = await import('./recordSecurity')
    const allocation = recordAllocationFixture('live')
    registerRecordClientAllocation('usr_a', { record_id: allocation.record_id, allocation_id: allocation.allocation_id })
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2))
    const request = JSON.parse(String(vi.mocked(fetch).mock.calls[1]?.[1]?.body)) as { record_ids: string[]; allocation_references: unknown[] }
    expect(request.record_ids).toEqual([])
    expect(request.allocation_references).toEqual([{ record_id: allocation.record_id, allocation_id: allocation.allocation_id }])
    expect(socket.readyState).toBe(CleanupSocket.OPEN)
    expect(onError).not.toHaveBeenCalled()
  })

  it('retains pending state when allocation reconciliation is unavailable', async () => {
    const allocation = recordAllocationFixture('unavailable')
    const pending = {
      key: 'usr_a:new', userId: 'usr_a', payload: emptyRecordDraftPayload('usr_a'), updatedAt: Date.now(),
      publication: { version: 1 as const, phase: 'bound' as const, idempotencyKey: 'publish-1', publicationDigest: 'f'.repeat(64),
        request: { draft_id: 'rdf_sent', draft_etag: 'etag-sent', record_id: allocation.record_id, allocation } },
    }
    const store = memoryDraftBufferStore([pending])
    vi.mocked(fetch).mockResolvedValueOnce(new Response('{}', { status: 503 }))
    const onError = vi.fn()
    const consumer = startRecordClientCleanup('usr_a', { store, onError })
    consumers.push(consumer)
    await expect(consumer.ready).rejects.toMatchObject({ status: 503 })
    expect(await store.get(pending.key)).toEqual(pending)
    await waitFor(() => expect(onError).toHaveBeenCalled())
  })

  it('batches allocation references without also treating them as record reads', async () => {
    const reservations = Array.from({ length: 101 }, (_, index) => {
      const allocation = recordAllocationFixture(`batch-${index}`, { purpose: 'evidence_preview' })
      return {
        key: `usr_a:preview-${index}`, userId: 'usr_a', payload: emptyRecordDraftPayload('usr_a'), updatedAt: Date.now(),
        evidenceReservation: { allocationKey: allocation.idempotency_key, requestDigest: allocation.request_digest, allocation },
      }
    })
    const store = memoryDraftBufferStore(reservations)
    await start(store)
    const bodies = vi.mocked(fetch).mock.calls.map((call) => JSON.parse(String(call[1]?.body)) as {
      record_ids: string[]; allocation_references: { record_id: string; allocation_id: string }[]
    })
    expect(bodies.map((body) => body.record_ids.length + body.allocation_references.length)).toEqual([100, 1])
    expect(bodies.flatMap((body) => body.record_ids)).toEqual([])
    expect(new Set(bodies.flatMap((body) => body.allocation_references.map((reference) => reference.record_id))).size).toBe(101)
    expect(await store.list()).toHaveLength(101)
  })

  it('clears only the pending target explicitly invalidated by allocation reconciliation', async () => {
    const first = recordAllocationFixture('first', { purpose: 'evidence_preview' })
    const second = recordAllocationFixture('second', { purpose: 'evidence_preview' })
    const draft = (allocation: typeof first) => ({
      key: `usr_a:${allocation.record_id}`, userId: 'usr_a', payload: emptyRecordDraftPayload('usr_a'), updatedAt: Date.now(),
      evidenceReservation: { allocationKey: allocation.idempotency_key, requestDigest: allocation.request_digest, allocation },
    })
    const retained = draft(second)
    const store = memoryDraftBufferStore([draft(first), retained])
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ allowed: [], invalidate: [first.record_id], pending: [second.record_id] }), { status: 200 }))
    await start(store)
    expect(await store.get(`usr_a:${first.record_id}`)).toBeUndefined()
    expect(await store.get(`usr_a:${second.record_id}`)).toEqual(retained)
  })

  it('uses the allocation reference when the same target is also being observed as a record', async () => {
    const allocation = recordAllocationFixture('duplicate', { purpose: 'evidence_preview' })
    const stored = { key: 'usr_a:preview', userId: 'usr_a', payload: emptyRecordDraftPayload('usr_a'), updatedAt: Date.now(),
      evidenceReservation: { allocationKey: allocation.idempotency_key, requestDigest: allocation.request_digest, allocation } }
    // Match the simulated document's module instance after resetModules.
    const { createRecordSecurityController } = await import('./recordSecurity')
    const controller = createRecordSecurityController(allocation.record_id, 'usr_a', 0, vi.fn())
    try {
      await start(memoryDraftBufferStore([stored]))
      const body = JSON.parse(String(vi.mocked(fetch).mock.calls[0]?.[1]?.body)) as { record_ids: string[]; allocation_references: unknown[] }
      expect(body.record_ids).toEqual([])
      expect(body.allocation_references).toEqual([{ record_id: allocation.record_id, allocation_id: allocation.allocation_id }])
    } finally {
      controller.dispose()
    }
  })
})
