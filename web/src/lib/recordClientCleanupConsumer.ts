import { boundPublication, openIndexedDBDraftBuffer, type DraftBufferStore, type DraftCleanupEvent } from '../pages/records/draftBuffer'
import { jsonBodyInit, requestJSON } from './apiRequest'
import { clearComparisonSaveAttempts, comparisonSaveAllocationReferences, comparisonSaveRecordIDs } from './comparisonSaveAttempts'
import { applyRecordClientCleanup } from './recordClientCleanup'
import { broadcastRecordInvalidation, observeRecordClientAllocations, observeRecordClientTargets, type RecordClientAllocationReference } from './recordSecurity'

export type RecordCleanupConsumer = { stop: () => void; ready: Promise<void> }
type ConsumerOptions = { onError: (error: Error) => void; store?: DraftBufferStore }
type ReconcileResponse = { allowed: string[]; invalidate: string[]; pending: string[] }
// Identity is per document: sessionStorage is copied into opened tabs and
// survives reloads, so it would let two documents share one cleanup cursor.
const browserIDs = new Map<string, string>()

export function startRecordClientCleanup(userId: string, options: ConsumerOptions): RecordCleanupConsumer {
  const store = options.store ?? openIndexedDBDraftBuffer()
  const abort = new AbortController()
  const targets = new Set<string>()
  const allocations = new Map<string, RecordClientAllocationReference>()
  let initializing = true
  let stopped = false
  let socket: WebSocket | null = null
  let socketGeneration = 0
  let browserId = ''
  let reconciledUnknownCursor = false
  let unsubscribe = () => {}
  let queue: Promise<void> = Promise.resolve()

  function stop() {
    if (stopped) return
    stopped = true
    abort.abort()
    unsubscribe()
    socketGeneration += 1
    socket?.close()
  }
  function fail(cause: unknown) {
    if (stopped) return
    stop()
    options.onError(cause instanceof Error ? cause : new Error('记录客户端清理同步失败'))
  }
  function enqueue(work: () => Promise<void>) {
    queue = queue.then(async () => { if (!stopped) await work() }).catch(fail)
  }
  function newBrowserID() {
    const id = `browser_${crypto.randomUUID()}`
    browserIDs.set(userId, id)
    return id
  }
  async function reconcile(recordIds: readonly string[], references: readonly RecordClientAllocationReference[]) {
    const pending = [...references].sort((left, right) => left.record_id.localeCompare(right.record_id))
    const allocationIDs = new Set(pending.map((reference) => reference.record_id))
    const unique = [...new Set(recordIds)].filter((recordId) => !allocationIDs.has(recordId)).sort()
    const entries = [
      ...unique.map((recordId) => ({ kind: 'record' as const, recordId })),
      ...pending.map((reference) => ({ kind: 'allocation' as const, recordId: reference.record_id, reference })),
    ]
    for (let offset = 0; offset < entries.length || (offset === 0 && entries.length === 0); offset += 100) {
      const batch = entries.slice(offset, offset + 100)
      const batchRecords = batch.filter((entry) => entry.kind === 'record').map((entry) => entry.recordId)
      const batchReferences = batch.flatMap((entry) => entry.kind === 'allocation' ? [entry.reference] : [])
      const expected = batch.map((entry) => entry.recordId)
      const result = await requestJSON<ReconcileResponse>('/api/records/client-cleanup/reconcile', {
        ...jsonBodyInit('POST', { browser_id: browserId, record_ids: batchRecords,
          ...(batchReferences.length ? { allocation_references: batchReferences } : {}) }), signal: abort.signal,
      })
      if (stopped) return
      if (!result || !Array.isArray(result.allowed) || !Array.isArray(result.invalidate) || !Array.isArray(result.pending)) throw new Error('记录清理协调响应无效')
      const received = [...result.allowed, ...result.invalidate, ...result.pending]
      if (received.length !== expected.length || new Set(received).size !== received.length
        || received.some((recordId) => !expected.includes(recordId))
        || result.pending.some((recordId) => !batchReferences.some((reference) => reference.record_id === recordId))) {
        throw new Error('记录清理协调身份不匹配')
      }
      for (const recordId of result.allowed) {
        if (allocations.has(recordId)) {
          allocations.delete(recordId)
          targets.add(recordId)
        }
      }
      for (const recordId of result.invalidate) {
        targets.delete(recordId)
        allocations.delete(recordId)
        broadcastRecordInvalidation(userId, recordId)
        clearComparisonSaveAttempts(userId, recordId)
        await store.invalidateRecord(userId, recordId)
        if (stopped) return
      }
      if (entries.length === 0) break
    }
  }
  async function connect() {
    if (stopped) return
    const cursor = await store.cleanupCursor(userId, browserId)
    if (stopped) return
    const url = new URL('/api/records/client-cleanup', window.location.href)
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
    url.searchParams.set('browser_id', browserId)
    if (cursor) url.searchParams.set('cursor', cursor)
    const generation = ++socketGeneration
    const connection = new WebSocket(url)
    socket = connection
    connection.onmessage = (message) => {
      enqueue(async () => {
        if (generation !== socketGeneration || stopped) return
        if (typeof message.data !== 'string' || message.data.length > 16 * 1024) throw new Error('记录清理消息无效')
        const value: unknown = JSON.parse(message.data)
        if (!value || typeof value !== 'object') throw new Error('记录清理消息无效')
        const frame = value as { type?: string; event?: DraftCleanupEvent }
        if (frame.type === 'reconcile_required') {
          if (reconciledUnknownCursor) throw new Error('记录清理游标无法协调')
          reconciledUnknownCursor = true
          // A fresh browser identity has no cursor to silently skip. Re-register
          // all locally known targets before replaying its unacknowledged events.
          socketGeneration += 1
          connection.close()
          browserId = newBrowserID()
          await reconcile([...targets], [...allocations.values()])
          await connect()
          return
        }
        if (frame.type !== 'cleanup' || !frame.event) throw new Error('记录清理消息无效')
        const ack = await applyRecordClientCleanup(store, userId, browserId, frame.event)
        targets.delete(frame.event.record_id)
        allocations.delete(frame.event.record_id)
        if (stopped || generation !== socketGeneration || connection.readyState !== WebSocket.OPEN) return
        connection.send(JSON.stringify({ type: 'ack', ack }))
      })
    }
    connection.onerror = () => { if (generation === socketGeneration) fail(new Error('记录客户端清理连接失败')) }
    connection.onclose = () => {
      // Queue behind a received reconcile_required frame, which deliberately
      // replaces this socket; ordinary closes stop sync without deleting drafts.
      enqueue(async () => { if (generation === socketGeneration) throw new Error('记录客户端清理连接已中断，请重试') })
    }
  }

  const ready = (async () => {
    if (!/^usr_[a-z0-9]{1,64}$/.test(userId)) throw new Error('记录清理用户身份无效')
    browserId = browserIDs.get(userId) ?? newBrowserID()
    const stopTargets = observeRecordClientTargets(userId, (recordIds) => {
      for (const recordId of recordIds) targets.add(recordId)
      if (!initializing && recordIds.length) {
        const references = recordIds.flatMap((recordId) => allocations.has(recordId) ? [allocations.get(recordId)!] : [])
        enqueue(() => reconcile(recordIds, references))
      }
    })
    const stopAllocations = observeRecordClientAllocations(userId, (reference) => {
      const previous = allocations.get(reference.record_id)
      if (previous && previous.allocation_id !== reference.allocation_id) {
        fail(new Error('记录预留清理身份不匹配'))
        return
      }
      allocations.set(reference.record_id, reference)
      if (!initializing) enqueue(() => reconcile([], [reference]))
    })
    unsubscribe = () => { stopTargets(); stopAllocations() }
    await store.initialize()
    if (stopped) return
    const drafts = await store.list()
    if (stopped) return
    for (const draft of drafts) {
      if (draft.userId !== userId) continue
      if (draft.recordId) targets.add(draft.recordId)
      const allocation = boundPublication(draft)?.request.allocation ?? draft.evidenceReservation?.allocation
      if (allocation) {
        const previous = allocations.get(allocation.record_id)
        if (previous && previous.allocation_id !== allocation.allocation_id) throw new Error('记录预留清理身份不匹配')
        allocations.set(allocation.record_id, { record_id: allocation.record_id, allocation_id: allocation.allocation_id })
      }
    }
    for (const recordId of comparisonSaveRecordIDs(userId)) targets.add(recordId)
    for (const reference of comparisonSaveAllocationReferences(userId)) {
      const previous = allocations.get(reference.record_id)
      if (previous && previous.allocation_id !== reference.allocation_id) throw new Error('记录预留清理身份不匹配')
      allocations.set(reference.record_id, reference)
    }
    initializing = false
    await reconcile([...targets], [...allocations.values()])
    await connect()
  })()
  queue = ready.catch(fail)
  return { stop, ready }
}
