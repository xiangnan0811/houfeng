import type { RecordDraftPayload } from '../../lib/types'

export const DRAFT_BUFFER_TTL_MS = 24 * 60 * 60 * 1000
const DB_NAME = 'houfeng-record-drafts'
const STORE_NAME = 'unsynced'

export type UnsyncedDraft = {
  key: string
  userId: string
  recordId?: string
  draftId?: string
  etag?: string
  payload: RecordDraftPayload
  updatedAt: number
}

export type DraftBufferStore = {
  get(key: string): Promise<UnsyncedDraft | undefined>
  set(value: UnsyncedDraft): Promise<void>
  delete(key: string): Promise<void>
  list(): Promise<UnsyncedDraft[]>
}

export function memoryDraftBufferStore(initial: readonly UnsyncedDraft[] = []): DraftBufferStore {
  const values = new Map(initial.map((item) => [item.key, item]))
  return {
    async get(key) {
      return values.get(key)
    },
    async set(value) {
      values.set(value.key, value)
    },
    async delete(key) {
      values.delete(key)
    },
    async list() {
      return [...values.values()]
    },
  }
}

export function draftBufferKey(userId: string, recordId = 'new'): string {
  return `${userId}:${recordId}`
}

export function isExpiredUnsyncedDraft(value: UnsyncedDraft, now = Date.now()): boolean {
  return now - value.updatedAt > DRAFT_BUFFER_TTL_MS
}

export async function readUnsyncedDraft(store: DraftBufferStore, key: string, now = Date.now()): Promise<UnsyncedDraft | undefined> {
  const current = await store.get(key)
  if (!current) return undefined
  if (isExpiredUnsyncedDraft(current, now)) {
    await store.delete(key)
    return undefined
  }
  return current
}

export async function writeUnsyncedDraft(store: DraftBufferStore, value: UnsyncedDraft): Promise<void> {
  const { payload, ...meta } = value
  await store.set({
    ...meta,
    payload: {
      ...payload,
      attachment_ids: [...payload.attachment_ids],
    },
  })
}

export async function clearUnsyncedDraftsForUser(store: DraftBufferStore, userId: string): Promise<void> {
  if (!userId) return
  const items = await store.list()
  await Promise.all(items.filter((item) => item.userId === userId).map((item) => store.delete(item.key)))
}

export async function discardUserDrafts(userId: string, store?: DraftBufferStore): Promise<void> {
  if (!userId) return
  await clearUnsyncedDraftsForUser(store ?? openIndexedDBDraftBuffer(), userId)
}

export function openIndexedDBDraftBuffer(): DraftBufferStore {
  if (typeof indexedDB === 'undefined') return memoryDraftBufferStore()
  return {
    async get(key) {
      const store = await withStore('readonly')
      return requestToPromise<UnsyncedDraft | undefined>(store.get(key))
    },
    async set(value) {
      const store = await withStore('readwrite')
      await requestToPromise(store.put(value))
    },
    async delete(key) {
      const store = await withStore('readwrite')
      await requestToPromise(store.delete(key))
    },
    async list() {
      const store = await withStore('readonly')
      return requestToPromise<UnsyncedDraft[]>(store.getAll())
    },
  }
}

function withStore(mode: IDBTransactionMode): Promise<IDBObjectStore> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1)
    request.onupgradeneeded = () => {
      request.result.createObjectStore(STORE_NAME, { keyPath: 'key' })
    }
    request.onerror = () => reject(request.error)
    request.onsuccess = () => {
      const tx = request.result.transaction(STORE_NAME, mode)
      resolve(tx.objectStore(STORE_NAME))
    }
  })
}

function requestToPromise<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onerror = () => reject(request.error)
    request.onsuccess = () => resolve(request.result)
  })
}
