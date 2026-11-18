import { isRecordId } from '../../lib/recordIdentity'
import type { CreateRecordInput, RecordAllocation, RecordDraftPayload, RecordSubjectReference } from '../../lib/types'

export const DRAFT_BUFFER_TTL_MS = 24 * 60 * 60 * 1000
const DB_NAME = 'houfeng-record-drafts'
const STORE_NAME = 'unsynced'
const META_STORE = 'cleanup'
const scopeBrand = Symbol('draft-write-scope')

/**
 * A new-record publication that must survive reload. `reserving` fixes the
 * reservation key and digest before the allocation request; `bound` fixes the
 * exact create body and publication key before the first create request and
 * stays until that exact request returns success. No failure reply proves the
 * request did not commit, so a bound envelope is only replaced by an explicit
 * discard, logout, or record cleanup.
 */
export type PendingRecordPublication =
  | {
    version: 1
    phase: 'reserving'
    allocationKey: string
    requestDigest: string
    idempotencyKey: string
    request: { draft_id: string; draft_etag: string }
  }
  | {
    version: 1
    phase: 'bound'
    idempotencyKey: string
    publicationDigest: string
    request: CreateRecordInput & { record_id: string; allocation: RecordAllocation }
  }

export type UnsyncedDraft = {
  key: string
  userId: string
  recordId?: string
  draftId?: string
  etag?: string
  payload: RecordDraftPayload
  updatedAt: number
  publication?: PendingRecordPublication
  /** Evidence allocation is persisted separately from the editable draft. */
  evidenceReservation?: { allocationKey: string; requestDigest: string; allocation?: RecordAllocation }
}

export type DraftWriteScope = Readonly<{
  userId: string
  recordId: string | undefined
  generation: string
  recordGeneration: string
  [scopeBrand]: true
}>

export type DraftCleanupEvent = {
  version: 1
  cursor: string
  operation_id: string
  record_id: string
  cleanup_epoch: string
  scopes: readonly string[]
}

export class DraftWriteInvalidatedError extends Error {
  constructor() {
    super('Draft workspace has been invalidated')
    this.name = 'DraftWriteInvalidatedError'
  }
}

/** A bound publication is unresolved: ordinary buffer writes must not replace it. */
export class DraftPublicationPendingError extends Error {
  constructor() {
    super('上次发布结果尚未确认，确认前不能改写本地草稿')
    this.name = 'DraftPublicationPendingError'
  }
}

export function boundPublication(value: UnsyncedDraft | undefined) {
  return value?.publication?.phase === 'bound' ? value.publication : undefined
}

function publicationRecordId(value: UnsyncedDraft): string | undefined {
  return boundPublication(value)?.request.record_id ?? value.evidenceReservation?.allocation?.record_id
}

export type DraftBufferStore = {
  initialize(now?: number): Promise<void>
  captureScope(userId: string, recordId?: string): Promise<DraftWriteScope>
  validateScopes(scopes: readonly DraftWriteScope[]): Promise<void>
  get(key: string): Promise<UnsyncedDraft | undefined>
  readUnexpired(key: string, now: number): Promise<UnsyncedDraft | undefined>
  set(value: UnsyncedDraft, scope: DraftWriteScope): Promise<void>
  delete(key: string): Promise<void>
  /** Explicit user discard: drops only the matching envelope, keeping the local payload. */
  discardPublication(key: string, idempotencyKey: string): Promise<void>
  bindPublication(key: string, reservation: Extract<PendingRecordPublication, { phase: 'reserving' }>, publication: Extract<PendingRecordPublication, { phase: 'bound' }>, scope: DraftWriteScope): Promise<boolean>
  /** Finish only the exact published request; a late reply cannot delete a newer attempt. */
  completePublication(key: string, publication: Extract<PendingRecordPublication, { phase: 'bound' }>, scope: DraftWriteScope): Promise<boolean>
  bindEvidenceReservation(key: string, allocationKey: string, requestDigest: string, allocation: RecordAllocation, scope: DraftWriteScope): Promise<boolean>
  discardEvidenceReservation(key: string, allocationKey: string): Promise<void>
  list(): Promise<UnsyncedDraft[]>
  clearUser(userId: string): Promise<void>
  invalidateRecord(userId: string, recordId: string): Promise<void>
  applyCleanup(userId: string, browserId: string, event: DraftCleanupEvent): Promise<void>
  cleanupCursor(userId: string, browserId: string): Promise<string | undefined>
}

type CleanupMetadata = { key: string; generation?: string; cursor?: string; event?: DraftCleanupEvent }
type BufferTransaction = {
  get(key: string): Promise<UnsyncedDraft | undefined>
  put(value: UnsyncedDraft): Promise<void>
  delete(key: string): Promise<void>
  list(): Promise<UnsyncedDraft[]>
  metadata(key: string): Promise<CleanupMetadata | undefined>
  putMetadata(value: CleanupMetadata): Promise<void>
}
type RunTransaction = <T>(mode: IDBTransactionMode, work: (tx: BufferTransaction) => Promise<T>) => Promise<T>
const metadataKey = (...parts: string[]) => JSON.stringify(parts)
const generationKey = (userId: string) => metadataKey('generation', userId)
const tombstoneKey = (userId: string, recordId: string) => metadataKey('record', userId, recordId)
const cursorKey = (userId: string, browserId: string) => metadataKey('cursor', userId, browserId)

export function validateCleanupEvent(event: DraftCleanupEvent): void {
  const positive = (value: string) => typeof value === 'string' && /^[1-9][0-9]{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n
  const scopes: Record<string, true> = { markdown_draft_buffer: true, markdown_workspace: true, comparison_workbench: true, comparison_save_attempt: true }
  if (event.version !== 1 || !positive(event.cursor) || !positive(event.cleanup_epoch)
    || !/^rpo_[a-z0-9]{1,64}$/.test(event.operation_id) || typeof event.record_id !== 'string' || !isRecordId(event.record_id)
    || !Array.isArray(event.scopes) || event.scopes.length === 0 || event.scopes.length > 4
    || new Set(event.scopes).size !== event.scopes.length || event.scopes.some((scope) => !Object.hasOwn(scopes, scope))) {
    throw new Error('Invalid draft cleanup event')
  }
}

async function requireCurrentDraftScope(tx: BufferTransaction, scope: DraftWriteScope): Promise<void> {
  if (!scope || scope[scopeBrand] !== true) throw new DraftWriteInvalidatedError()
  const generation = (await tx.metadata(generationKey(scope.userId)))?.generation ?? '0'
  const record = scope.recordId ? await tx.metadata(tombstoneKey(scope.userId, scope.recordId)) : undefined
  if (scope.generation !== generation || record?.event || scope.recordGeneration !== (record?.generation ?? '0')) {
    throw new DraftWriteInvalidatedError()
  }
}

async function deleteRecordBuffers(tx: BufferTransaction, userId: string, recordId: string): Promise<void> {
  for (const value of await tx.list()) {
    if (value.userId !== userId || (value.recordId !== recordId && publicationRecordId(value) !== recordId
      && value.evidenceReservation?.allocation?.record_id !== recordId)) continue
    await tx.delete(value.key)
    // Evidence reservations have their own key, but the editable new-record
    // buffer belongs to that destination for as long as the reservation exists.
    const suffix = ':evidence-reservation'
    if (value.evidenceReservation?.allocation?.record_id === recordId
      && value.key.startsWith(`${userId}:`) && value.key.endsWith(suffix)) {
      const sibling = value.key.slice(0, -suffix.length)
      if (sibling !== `${userId}:new` && !sibling.startsWith(`${userId}:new:`)) continue
      const draft = await tx.get(sibling)
      const draftTarget = draft && publicationRecordId(draft)
      if (draft?.userId === userId && !draft.recordId && (!draftTarget || draftTarget === recordId)) await tx.delete(sibling)
    }
  }
}

function createDraftBufferStore(run: RunTransaction): DraftBufferStore {
  return {
    initialize(now = Date.now()) {
      return run('readwrite', async (tx) => {
        for (const value of await tx.list()) {
          if (isExpiredUnsyncedDraft(value, now)) await tx.delete(value.key)
        }
      })
    },
    captureScope(userId, recordId) {
      return run('readonly', async (tx) => {
        const record = recordId ? await tx.metadata(tombstoneKey(userId, recordId)) : undefined
        if (!userId || record?.event) throw new DraftWriteInvalidatedError()
        const generation = (await tx.metadata(generationKey(userId)))?.generation ?? '0'
        return Object.freeze({ userId, recordId, generation, recordGeneration: record?.generation ?? '0', [scopeBrand]: true as const })
      })
    },
    validateScopes(scopes) {
      const captured = [...scopes]
      return run('readonly', async (tx) => {
        if (captured.length === 0) throw new DraftWriteInvalidatedError()
        for (const scope of captured) await requireCurrentDraftScope(tx, scope)
      })
    },
    get: (key) => run('readonly', (tx) => tx.get(key)),
    readUnexpired(key, now) {
      return run('readwrite', async (tx) => {
        const value = await tx.get(key)
        if (value && isExpiredUnsyncedDraft(value, now)) {
          await tx.delete(key)
          return undefined
        }
        return value
      })
    },
    set(input, scope) {
      const value = structuredClone(input)
      return run('readwrite', async (tx) => {
        await requireCurrentDraftScope(tx, scope)
        if (scope.userId !== value.userId || scope.recordId !== value.recordId) throw new DraftWriteInvalidatedError()
        const previous = await tx.get(value.key)
        if (previous && (previous.userId !== value.userId || previous.recordId !== value.recordId)) throw new DraftWriteInvalidatedError()
        if (previous?.evidenceReservation) {
          const old = previous.evidenceReservation
          const next = value.evidenceReservation
          if (!next || old.allocationKey !== next.allocationKey || old.requestDigest !== next.requestDigest
            || (old.allocation && JSON.stringify(old.allocation) !== JSON.stringify(next.allocation))) throw new DraftPublicationPendingError()
        }
        const pending = previous?.publication
        if (pending) {
          const next = value.publication
          const samePayload = JSON.stringify(previous.payload) === JSON.stringify(value.payload)
          const sameIdentity = next?.idempotencyKey === pending.idempotencyKey
          const sameRequest = next && JSON.stringify(next) === JSON.stringify(pending)
          if (!samePayload || !sameIdentity || !sameRequest) throw new DraftPublicationPendingError()
        }
        const target = publicationRecordId(value)
        if (target && (await tx.metadata(tombstoneKey(value.userId, target)))?.event) throw new DraftWriteInvalidatedError()
        await tx.put(value)
      })
    },
    delete(key) {
      return run('readwrite', async (tx) => {
        if ((await tx.get(key))?.publication) throw new DraftPublicationPendingError()
        await tx.delete(key)
      })
    },
    discardPublication(key, idempotencyKey) {
      return run('readwrite', async (tx) => {
        const value = await tx.get(key)
        if (!value || value.publication?.idempotencyKey !== idempotencyKey) return
        const rest = { ...value }
        delete rest.publication
        await tx.put({ ...rest, updatedAt: Date.now() })
      })
    },
    bindPublication(key, reservation, publication, scope) {
      const expected = structuredClone(reservation)
      const next = structuredClone(publication)
      return run('readwrite', async (tx) => {
        await requireCurrentDraftScope(tx, scope)
        const value = await tx.get(key)
        if (!value || value.userId !== scope.userId || JSON.stringify(value.publication) !== JSON.stringify(expected)) return false
        if (next.idempotencyKey !== expected.idempotencyKey || next.request.draft_id !== expected.request.draft_id
          || next.request.draft_etag !== expected.request.draft_etag || next.request.record_id !== next.request.allocation.record_id
          || next.request.allocation.purpose !== 'record_create' || next.request.allocation.idempotency_key !== expected.allocationKey
          || next.request.allocation.request_digest !== expected.requestDigest) throw new DraftPublicationPendingError()
        if ((await tx.metadata(tombstoneKey(scope.userId, next.request.record_id)))?.event) throw new DraftWriteInvalidatedError()
        await tx.put({ ...value, publication: next })
        return true
      })
    },
    completePublication(key, publication, scope) {
      return run('readwrite', async (tx) => {
        await requireCurrentDraftScope(tx, scope)
        const value = await tx.get(key)
        if (!value || value.userId !== scope.userId || JSON.stringify(value.publication) !== JSON.stringify(publication)) return false
        const target = publication.request.record_id
        if ((await tx.metadata(tombstoneKey(scope.userId, target)))?.event) throw new DraftWriteInvalidatedError()
        await tx.delete(key)
        return true
      })
    },
    bindEvidenceReservation(key, allocationKey, requestDigest, allocation, scope) {
      const next = structuredClone(allocation)
      return run('readwrite', async (tx) => {
        await requireCurrentDraftScope(tx, scope)
        const value = await tx.get(key)
        const prior = value?.evidenceReservation
        if (!value || value.userId !== scope.userId || !prior || prior.allocationKey !== allocationKey || prior.requestDigest !== requestDigest) return false
        if (next.purpose !== 'evidence_preview' || next.idempotency_key !== allocationKey || next.request_digest !== requestDigest
          || (prior.allocation && JSON.stringify(prior.allocation) !== JSON.stringify(next))) throw new DraftPublicationPendingError()
        if ((await tx.metadata(tombstoneKey(scope.userId, next.record_id)))?.event) throw new DraftWriteInvalidatedError()
        await tx.put({ ...value, evidenceReservation: { ...prior, allocation: next } })
        return true
      })
    },
    discardEvidenceReservation(key, allocationKey) {
      return run('readwrite', async (tx) => {
        const value = await tx.get(key)
        if (value?.evidenceReservation?.allocationKey === allocationKey && !value.publication) await tx.delete(key)
      })
    },
    list: () => run('readonly', (tx) => tx.list()),
    clearUser(userId) {
      return run('readwrite', async (tx) => {
        const key = generationKey(userId)
        const generation = BigInt((await tx.metadata(key))?.generation ?? '0') + 1n
        await tx.putMetadata({ key, generation: generation.toString() })
        for (const value of await tx.list()) if (value.userId === userId) await tx.delete(value.key)
      })
    },
    invalidateRecord(userId, recordId) {
      return run('readwrite', async (tx) => {
        const key = tombstoneKey(userId, recordId)
        const previous = await tx.metadata(key)
        const generation = (BigInt(previous?.generation ?? '0') + 1n).toString()
        await tx.putMetadata({ ...previous, key, generation })
        await deleteRecordBuffers(tx, userId, recordId)
      })
    },
    applyCleanup(userId, browserId, input) {
      // Detach before the transaction: callers cannot mutate an event while storage is opening.
      const event = structuredClone(input)
      return run('readwrite', async (tx) => {
        validateCleanupEvent(event)
        if (!/^usr_[a-z0-9]{1,64}$/.test(userId) || !/^[a-z0-9_-]{1,128}$/.test(browserId)) throw new Error('Invalid draft cleanup identity')
        const key = tombstoneKey(userId, event.record_id)
        const previous = (await tx.metadata(key))?.event
        if (previous && (previous.operation_id !== event.operation_id || previous.cleanup_epoch !== event.cleanup_epoch
          || previous.cursor !== event.cursor || [...previous.scopes].sort().join(',') !== [...event.scopes].sort().join(','))) {
          throw new Error('Conflicting draft cleanup identity')
        }
        const eventKey = metadataKey('event', userId, event.cursor)
        const priorEvent = (await tx.metadata(eventKey))?.event
        if (priorEvent && (priorEvent.record_id !== event.record_id || priorEvent.operation_id !== event.operation_id
          || priorEvent.cleanup_epoch !== event.cleanup_epoch)) throw new Error('Conflicting draft cleanup cursor')
        await tx.putMetadata({ key, event })
        await tx.putMetadata({ key: eventKey, event })
        await deleteRecordBuffers(tx, userId, event.record_id)
        const progressKey = cursorKey(userId, browserId)
        const cursor = (await tx.metadata(progressKey))?.cursor
        if (!cursor || BigInt(event.cursor) > BigInt(cursor)) await tx.putMetadata({ key: progressKey, cursor: event.cursor })
      })
    },
    cleanupCursor: (userId, browserId) => run('readonly', async (tx) => (await tx.metadata(cursorKey(userId, browserId)))?.cursor),
  }
}

/** Explicit test storage; production never falls back to non-durable memory. */
export function memoryDraftBufferStore(initial: readonly UnsyncedDraft[] = []): DraftBufferStore {
  let values = new Map(initial.map((item) => [item.key, structuredClone(item)]))
  let metadata = new Map<string, CleanupMetadata>()
  let pending: Promise<unknown> = Promise.resolve()
  const run: RunTransaction = (mode, work) => {
    const result = pending.then(async () => {
      const nextValues = new Map(values)
      const nextMetadata = new Map(metadata)
      const result = await work({
        get: async (key) => structuredClone(nextValues.get(key)),
        put: async (value) => { nextValues.set(value.key, structuredClone(value)) },
        delete: async (key) => { nextValues.delete(key) },
        list: async () => structuredClone([...nextValues.values()]),
        metadata: async (key) => structuredClone(nextMetadata.get(key)),
        putMetadata: async (value) => { nextMetadata.set(value.key, structuredClone(value)) },
      })
      if (mode === 'readwrite') {
        values = nextValues
        metadata = nextMetadata
      }
      return result
    })
    pending = result.catch(() => undefined)
    return result
  }
  return createDraftBufferStore(run)
}

export function draftBufferKey(userId: string, recordId = 'new'): string {
  return `${userId}:${recordId}`
}

/** Scopes unsynced new-record buffers so a subject prefill cannot share `user:new`. */
export function draftBufferRecordId(
  recordId?: string,
  seedSubjects?: readonly Pick<RecordSubjectReference, 'kind' | 'source_id' | 'primary'>[],
): string {
  const trimmedRecord = recordId?.trim()
  if (trimmedRecord) return trimmedRecord
  const primary = seedSubjects?.find((subject) => subject.primary) ?? seedSubjects?.[0]
  const kind = primary?.kind?.trim()
  const sourceId = primary?.source_id?.trim()
  if (!kind || !sourceId) return 'new'
  return `new:${kind}:${sourceId}`
}

// A reservation or sent publication never expires locally: either may have
// committed despite a lost response and must be resumed using the same identity.
export function isExpiredUnsyncedDraft(value: UnsyncedDraft, now = Date.now()): boolean {
  return !value.publication && !value.evidenceReservation && now - value.updatedAt > DRAFT_BUFFER_TTL_MS
}

export async function readUnsyncedDraft(store: DraftBufferStore, key: string, now = Date.now()): Promise<UnsyncedDraft | undefined> {
  return store.readUnexpired(key, now)
}

export async function writeUnsyncedDraft(store: DraftBufferStore, value: UnsyncedDraft, scope: DraftWriteScope): Promise<void> {
  await store.set(value, scope)
}

export async function clearUnsyncedDraftsForUser(store: DraftBufferStore, userId: string): Promise<void> {
  if (userId) await store.clearUser(userId)
}

export async function discardUserDrafts(userId: string, store?: DraftBufferStore): Promise<void> {
  if (userId) await clearUnsyncedDraftsForUser(store ?? openIndexedDBDraftBuffer(), userId)
}

export function openIndexedDBDraftBuffer(): DraftBufferStore {
  return createDraftBufferStore(runIndexedDBTransaction)
}

function runIndexedDBTransaction<T>(mode: IDBTransactionMode, work: (tx: BufferTransaction) => Promise<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === 'undefined') {
      reject(new Error('Durable draft storage is unavailable'))
      return
    }
    const request = indexedDB.open(DB_NAME, 2)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(STORE_NAME)) request.result.createObjectStore(STORE_NAME, { keyPath: 'key' })
      if (!request.result.objectStoreNames.contains(META_STORE)) request.result.createObjectStore(META_STORE, { keyPath: 'key' })
    }
    request.onerror = () => reject(request.error)
    let abandoned = false
    request.onblocked = () => {
      abandoned = true
      reject(new Error('Draft storage upgrade is blocked by another tab'))
    }
    request.onsuccess = () => {
      const db = request.result
      if (abandoned) {
        db.close()
        return
      }
      db.onversionchange = () => db.close()
      let tx: IDBTransaction
      try {
        tx = db.transaction([STORE_NAME, META_STORE], mode)
      } catch (error) {
        db.close()
        reject(error)
        return
      }
      const values = tx.objectStore(STORE_NAME)
      const metadata = tx.objectStore(META_STORE)
      let result: T
      let failure: unknown
      tx.oncomplete = () => { db.close(); resolve(result) }
      tx.onabort = () => { db.close(); reject(failure ?? tx.error ?? new Error('Draft storage transaction aborted')) }
      tx.onerror = () => { failure ??= tx.error }
      void work({
        get: (key) => requestToPromise(values.get(key)),
        put: async (value) => { await requestToPromise(values.put(value)) },
        delete: async (key) => { await requestToPromise(values.delete(key)) },
        list: () => requestToPromise(values.getAll()),
        metadata: (key) => requestToPromise(metadata.get(key)),
        putMetadata: async (value) => { await requestToPromise(metadata.put(value)) },
      }).then((value) => { result = value }, (error: unknown) => {
        failure = error
        try {
          tx.abort()
        } catch {
          // A request error may already have aborted the transaction.
          db.close()
          reject(failure)
        }
      })
    }
  })
}

function requestToPromise<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onerror = () => reject(request.error)
    request.onsuccess = () => resolve(request.result)
  })
}
