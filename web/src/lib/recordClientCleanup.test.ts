import { beforeEach, describe, expect, it, vi } from 'vitest'
import { memoryDraftBufferStore, DraftWriteInvalidatedError, type DraftCleanupEvent } from '../pages/records/draftBuffer'
import { emptyRecordDraftPayload } from '../pages/records/recordPayload'
import { recordAllocationFixture } from '../pages/records/testFixtures'
import { readComparisonSaveAttempt, writeComparisonSaveAttempt, type ComparisonSaveAttemptV2 } from './comparisonSaveAttempts'
import { applyRecordClientCleanup } from './recordClientCleanup'
import { createRecordSecurityController } from './recordSecurity'

const event: DraftCleanupEvent = { version: 1, cursor: '9007199254740993', operation_id: 'rpo_cleanup', record_id: 'rec_source', cleanup_epoch: '9007199254740995', scopes: ['markdown_draft_buffer', 'markdown_workspace', 'comparison_workbench', 'comparison_save_attempt'] }
const allocation = recordAllocationFixture('result', { purpose: 'comparison_save', idempotency_key: 'allocation-a', request_digest: 'e'.repeat(64) })
const attempt: ComparisonSaveAttemptV2 = {
  version: 2, userId: 'usr_a', digest: 'a'.repeat(64), generation: '0', sourceRecordIds: ['rec_source'],
  payloadDigest: 'f'.repeat(64), allocationKey: 'allocation-a', requestDigest: 'e'.repeat(64), idempotencyKey: 'request-a', allocation,
  request: { record_id: allocation.record_id, draft_id: 'rdf_sent', draft_etag: 'etag-sent', comparison_intent: 'cmp1.intent', allocation },
}
beforeEach(() => { vi.restoreAllMocks(); sessionStorage.clear() })

describe('durable record client cleanup', () => {
  it('closes work synchronously and clears retry metadata before committing an exact bigint-safe acknowledgement', async () => {
    const store = memoryDraftBufferStore()
    const oldScope = await store.captureScope('usr_a', 'rec_source')
    const buffer = { key: 'usr_a:rec_source', userId: 'usr_a', recordId: 'rec_source', payload: emptyRecordDraftPayload('usr_a'), updatedAt: Date.now() }
    await store.set(buffer, oldScope)
    writeComparisonSaveAttempt(attempt)
    const controller = createRecordSecurityController('rec_source', 'usr_a', Number.MAX_SAFE_INTEGER, vi.fn())
    const retained = createRecordSecurityController('rec_result', 'usr_a', 0, vi.fn())
    const otherUser = createRecordSecurityController('rec_source', 'usr_b', 0, vi.fn())
    const pending = applyRecordClientCleanup(store, 'usr_a', 'browser_a', event)
    expect(controller.signal.aborted).toBe(true)
    expect(retained.signal.aborted).toBe(false)
    expect(otherUser.signal.aborted).toBe(false)
    expect(readComparisonSaveAttempt(attempt)).toBeNull()
    expect(await pending).toEqual({ cursor: event.cursor, operation_id: event.operation_id, record_id: event.record_id, cleanup_epoch: event.cleanup_epoch })
    expect(await store.cleanupCursor('usr_a', 'browser_a')).toBe(event.cursor)
    expect(await store.get(buffer.key)).toBeUndefined()
    await expect(store.set(buffer, oldScope)).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
    retained.dispose()
    otherUser.dispose()
  })

  it('does not advance a cursor after metadata failure and can replay after recovery', async () => {
    const store = memoryDraftBufferStore()
    writeComparisonSaveAttempt(attempt)
    const remove = vi.spyOn(Storage.prototype, 'removeItem').mockImplementationOnce(() => { throw new Error('storage blocked') })
    await expect(applyRecordClientCleanup(store, 'usr_a', 'browser_a', event)).rejects.toThrow('storage blocked')
    expect(await store.cleanupCursor('usr_a', 'browser_a')).toBeUndefined()
    remove.mockRestore()
    expect(readComparisonSaveAttempt(attempt)).toEqual(attempt)
    await applyRecordClientCleanup(store, 'usr_a', 'browser_a', event)
    expect(await store.cleanupCursor('usr_a', 'browser_a')).toBe(event.cursor)
    expect(readComparisonSaveAttempt(attempt)).toBeNull()
  })

  it('rejects an aborted durable cleanup and replays already-cleared session metadata safely', async () => {
    const store = memoryDraftBufferStore()
    writeComparisonSaveAttempt(attempt)
    vi.spyOn(store, 'applyCleanup').mockRejectedValueOnce(new Error('transaction aborted'))
    await expect(applyRecordClientCleanup(store, 'usr_a', 'browser_a', event)).rejects.toThrow('transaction aborted')
    expect(readComparisonSaveAttempt(attempt)).toBeNull()
    expect(await store.cleanupCursor('usr_a', 'browser_a')).toBeUndefined()
    await applyRecordClientCleanup(store, 'usr_a', 'browser_a', event)
    expect(await store.cleanupCursor('usr_a', 'browser_a')).toBe(event.cursor)
  })
})
