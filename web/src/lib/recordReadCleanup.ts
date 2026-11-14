import { openIndexedDBDraftBuffer, type DraftBufferStore } from '../pages/records/draftBuffer'
import { isRecordId } from './recordIdentity'
import { createRecordSecurityController, type RecordSecurityController } from './recordSecurity'

export type RecordReadCleanupScope = {
  signal: AbortSignal
  dispose: () => void
  include: (recordId: string) => Promise<void>
}

/** Owns the in-memory copies loaded by one readonly view or selection request. */
export function createRecordReadCleanupScope(
  userId: string,
  onInvalidate: () => void,
  store: DraftBufferStore = openIndexedDBDraftBuffer(),
): RecordReadCleanupScope {
  const abort = new AbortController()
  const controllers = new Map<string, RecordSecurityController>()
  // Capture before the request, including when snapshot ownership is not known
  // until its authorized response. Preserve storage failure for include(), even
  // if a failed network request means no response ever reaches it.
  const captured = store.captureScope(userId).then(
    (scope) => ({ scope, error: null }),
    (error: unknown) => ({ scope: null, error }),
  )
  function dispose() {
    abort.abort()
    for (const controller of controllers.values()) controller.dispose()
    controllers.clear()
  }
  return {
    signal: abort.signal,
    dispose,
    async include(recordId: string): Promise<void> {
      if (abort.signal.aborted) throw new DOMException('Read copy invalidated', 'AbortError')
      if (!isRecordId(recordId)) throw new Error('读取结果缺少记录归属')
      const original = await captured
      if (!original.scope) throw original.error
      const source = await store.captureScope(userId, recordId)
      await store.validateScopes([original.scope, source])
      if (abort.signal.aborted) throw new DOMException('Read copy invalidated', 'AbortError')
      if (!controllers.has(recordId)) {
        controllers.set(recordId, createRecordSecurityController(recordId, userId, 0, () => {
          if (abort.signal.aborted) return
          dispose()
          onInvalidate()
        }))
      }
    },
  }
}
