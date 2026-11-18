import { describe, expect, it } from 'vitest'

import type { RecordDraftPayload } from '../../lib/types'
import {
  DRAFT_BUFFER_TTL_MS,
  draftBufferKey,
  draftBufferRecordId,
  memoryDraftBufferStore,
  readUnsyncedDraft,
  discardUserDrafts,
  writeUnsyncedDraft,
  DraftPublicationPendingError,
  DraftWriteInvalidatedError,
  type DraftCleanupEvent,
  type UnsyncedDraft,
} from './draftBuffer'
import { recordAllocationFixture } from './testFixtures'

function payload(title: string): RecordDraftPayload {
  return {
    title,
    body_markdown: 'body',
    markdown_dialect_version: 1,
    record_type: 'note',
    business_status: '',
    impact_level: 'medium',
    visibility: { kind: 'project', allowed_roles: [], allowed_group_ids: [] },
    subjects: [],
    tags: [],
    attachment_ids: ['att_1'],
    owner_id: 'usr_1',
    participant_ids: [],
    save_reason: 'edit',
  }
}

describe('draftBuffer', () => {
  it('keeps unsynced payload for 24h and drops expired copies', async () => {
    const store = memoryDraftBufferStore()
    const key = draftBufferKey('usr_1', 'rec_001')
    await writeUnsyncedDraft(store, {
      key,
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: payload('local'),
      updatedAt: 1,
    }, await store.captureScope('usr_1', 'rec_001'))
    await expect(readUnsyncedDraft(store, key, 2)).resolves.toMatchObject({ payload: { title: 'local' } })
    await expect(readUnsyncedDraft(store, key, 1 + DRAFT_BUFFER_TTL_MS + 1)).resolves.toBeUndefined()
  })

  it('scopes a new-record buffer by primary subject without changing unscoped keys', () => {
    expect(draftBufferRecordId()).toBe('new')
    expect(draftBufferRecordId('rec_001')).toBe('rec_001')
    expect(draftBufferRecordId(undefined, [{
      kind: 'vps',
      source_id: 'vps_001',
      primary: true,
    }])).toBe('new:vps:vps_001')
    expect(draftBufferKey('usr_1')).toBe('usr_1:new')
    expect(draftBufferKey('usr_1', draftBufferRecordId(undefined, [{
      kind: 'vps',
      source_id: 'vps_001',
      primary: true,
    }]))).toBe('usr_1:new:vps:vps_001')
  })

  it('clears only the current user on logout', async () => {
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1'),
      userId: 'usr_1',
      payload: payload('mine'),
      updatedAt: 1,
    }, await store.captureScope('usr_1'))
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_2'),
      userId: 'usr_2',
      payload: payload('theirs'),
      updatedAt: 1,
    }, await store.captureScope('usr_2'))
    await discardUserDrafts('usr_1', store)
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1'), 2)).resolves.toBeUndefined()
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_2'), 2)).resolves.toMatchObject({ payload: { title: 'theirs' } })
  })
  it('prevents late writes after logout while preserving another user and allowing a new session', async () => {
    const store = memoryDraftBufferStore()
    const oldScope = await store.captureScope('usr_1', 'rec_001')
    const otherScope = await store.captureScope('usr_2', 'rec_001')
    const mine = { key: draftBufferKey('usr_1', 'rec_001'), userId: 'usr_1', recordId: 'rec_001', payload: payload('mine'), updatedAt: 1 }
    const theirs = { ...mine, key: draftBufferKey('usr_2', 'rec_001'), userId: 'usr_2', payload: payload('theirs') }
    await store.set(mine, oldScope)
    await store.set(theirs, otherScope)
    const logout = store.clearUser('usr_1')
    const lateWrite = store.set(mine, oldScope)
    await logout
    await expect(lateWrite).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
    expect(await store.list()).toEqual([theirs])
    await store.set(mine, await store.captureScope('usr_1', 'rec_001'))
    expect(await store.get(mine.key)).toEqual(mine)
  })

  it('atomically tombstones every matching buffer and advances exact bigint cursors without resurrection', async () => {
    const store = memoryDraftBufferStore()
    const scope = await store.captureScope('usr_1', 'rec_001')
    const value = { key: draftBufferKey('usr_1', 'rec_001'), userId: 'usr_1', recordId: 'rec_001', payload: payload('deleted'), updatedAt: 1 }
    const standalone = { key: draftBufferKey('usr_1'), userId: 'usr_1', payload: payload('standalone'), updatedAt: 1 }
    await store.set(value, scope)
    await store.set({ ...value, key: 'second-buffer' }, scope)
    await store.set(standalone, await store.captureScope('usr_1'))
    const event: DraftCleanupEvent = { version: 1, cursor: '9007199254740993', operation_id: 'rpo_one', record_id: 'rec_001', cleanup_epoch: '9007199254740995', scopes: ['markdown_draft_buffer', 'markdown_workspace'] }
    await store.applyCleanup('usr_1', 'browser_one', event)
    await store.applyCleanup('usr_1', 'browser_one', event)
    expect(await store.list()).toEqual([standalone])
    await expect(store.set(value, scope)).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
    await expect(store.captureScope('usr_1', 'rec_001')).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
    await store.clearUser('usr_1')
    await expect(store.captureScope('usr_1', 'rec_001')).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
    await store.applyCleanup('usr_1', 'browser_one', { ...event, cursor: '9007199254740992', record_id: 'rec_002', operation_id: 'rpo_two' })
    expect(await store.cleanupCursor('usr_1', 'browser_one')).toBe('9007199254740993')
    await expect(store.applyCleanup('usr_1', 'browser_one', { ...event, cleanup_epoch: '9007199254740996' })).rejects.toThrow('Conflicting')
    expect(await store.cleanupCursor('usr_1', 'browser_one')).toBe('9007199254740993')
    expect(await store.cleanupCursor('usr_2', 'browser_one')).toBeUndefined()
  })

  it.each(['0', '01', '-1', '1.0', '1e3', '9223372036854775808'])('rejects noncanonical cleanup cursor %s without deleting data', async (cursor) => {
    const value = { key: draftBufferKey('usr_1', 'rec_001'), userId: 'usr_1', recordId: 'rec_001', payload: payload('keep'), updatedAt: 1 }
    const store = memoryDraftBufferStore([value])
    await expect(store.applyCleanup('usr_1', 'browser_one', { version: 1, cursor, operation_id: 'rpo_one', record_id: 'rec_001', cleanup_epoch: '1', scopes: ['markdown_draft_buffer'] })).rejects.toThrow('Invalid')
    expect(await store.get(value.key)).toEqual(value)
    expect(await store.cleanupCursor('usr_1', 'browser_one')).toBeUndefined()
  })

  it('tombstones a reserved-domain record buffer and leaves the legacy-named sibling intact', async () => {
    const reserved = `rec2_${'0'.repeat(31)}1`
    const target = { key: draftBufferKey('usr_1', reserved), userId: 'usr_1', recordId: reserved, payload: payload('reserved'), updatedAt: 1 }
    const legacy = { key: draftBufferKey('usr_1', 'rec_001'), userId: 'usr_1', recordId: 'rec_001', payload: payload('legacy'), updatedAt: 1 }
    const store = memoryDraftBufferStore([target, legacy])
    await store.applyCleanup('usr_1', 'browser_one', { version: 1, cursor: '1', operation_id: 'rpo_one', record_id: reserved, cleanup_epoch: '1', scopes: ['markdown_draft_buffer'] })
    expect(await store.list()).toEqual([legacy])
    await expect(store.captureScope('usr_1', reserved)).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
  })

  it.each([`rec2_${'0'.repeat(31)}`, `rec2_${'G'.repeat(32)}`, 'rec_UPPER', 'rrv_001', `rec_${'a'.repeat(65)}`])('rejects malformed cleanup record %s without deleting data', async (recordId) => {
    const value = { key: draftBufferKey('usr_1', 'rec_001'), userId: 'usr_1', recordId: 'rec_001', payload: payload('keep'), updatedAt: 1 }
    const store = memoryDraftBufferStore([value])
    await expect(store.applyCleanup('usr_1', 'browser_one', { version: 1, cursor: '1', operation_id: 'rpo_one', record_id: recordId, cleanup_epoch: '1', scopes: ['markdown_draft_buffer'] })).rejects.toThrow('Invalid')
    expect(await store.get(value.key)).toEqual(value)
    expect(await store.cleanupCursor('usr_1', 'browser_one')).toBeUndefined()
  })

  it('sweeps expired buffers for all users before restoring any workspace', async () => {
    const expired = { key: 'usr_2:rec_old', userId: 'usr_2', recordId: 'rec_old', payload: payload('expired'), updatedAt: 1 }
    const fresh = { ...expired, key: 'usr_1:rec_fresh', userId: 'usr_1', recordId: 'rec_fresh', updatedAt: DRAFT_BUFFER_TTL_MS }
    const store = memoryDraftBufferStore([expired, fresh])
    await store.initialize(DRAFT_BUFFER_TTL_MS + 2)
    expect(await store.list()).toEqual([fresh])
  })

  it('invalidates a reconciled copy without permanently banning later authorized work', async () => {
    const store = memoryDraftBufferStore()
    const oldScope = await store.captureScope('usr_1', 'rec_001')
    const value = { key: 'usr_1:rec_001', userId: 'usr_1', recordId: 'rec_001', payload: payload('old copy'), updatedAt: 1 }
    await store.set(value, oldScope)
    await store.invalidateRecord('usr_1', 'rec_001')
    expect(await store.get(value.key)).toBeUndefined()
    await expect(store.set(value, oldScope)).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
    const fresh = { ...value, payload: payload('new authorized copy') }
    await store.set(fresh, await store.captureScope('usr_1', 'rec_001'))
    expect(await store.get(value.key)).toEqual(fresh)
    await store.applyCleanup('usr_1', 'browser_one', { version: 1, cursor: '1', operation_id: 'rpo_one', record_id: 'rec_001', cleanup_epoch: '1', scopes: ['markdown_draft_buffer'] })
    await store.invalidateRecord('usr_1', 'rec_001')
    await expect(store.captureScope('usr_1', 'rec_001')).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
  })

  describe('unresolved new-record publication', () => {
    const allocation = recordAllocationFixture('buffered')
    const key = draftBufferKey('usr_1')
    function pending(overrides: Partial<UnsyncedDraft> = {}): UnsyncedDraft {
      return {
        key,
        userId: 'usr_1',
        draftId: 'rdf_sent',
        etag: 'etag-sent',
        payload: payload('sent'),
        updatedAt: 1,
        publication: {
          version: 1,
          phase: 'bound',
          idempotencyKey: 'publish-1',
          publicationDigest: 'f'.repeat(64),
          request: { draft_id: 'rdf_sent', draft_etag: 'etag-sent', record_id: allocation.record_id, allocation },
        },
        ...overrides,
      }
    }

    it('outlives the buffer TTL so a committed result can still be asked for', async () => {
      const store = memoryDraftBufferStore()
      await writeUnsyncedDraft(store, pending(), await store.captureScope('usr_1'))
      await store.initialize(1 + DRAFT_BUFFER_TTL_MS * 10)
      await expect(readUnsyncedDraft(store, key, 1 + DRAFT_BUFFER_TTL_MS * 10)).resolves.toEqual(pending())
    })

    it('refuses ordinary buffer writes that would replace the sent request', async () => {
      const store = memoryDraftBufferStore()
      const scope = await store.captureScope('usr_1')
      await writeUnsyncedDraft(store, pending(), scope)
      const edited = pending({ payload: payload('edited'), updatedAt: 2 })
      delete edited.publication
      await expect(writeUnsyncedDraft(store, edited, scope)).rejects.toBeInstanceOf(DraftPublicationPendingError)
      const other = pending()
      if (!other.publication) throw new Error('expected pending publication')
      other.publication.idempotencyKey = 'publish-2'
      await expect(writeUnsyncedDraft(store, other, scope)).rejects.toBeInstanceOf(DraftPublicationPendingError)
      const sameKeyEditedBody = pending()
      if (sameKeyEditedBody.publication?.phase !== 'bound') throw new Error('expected bound publication')
      sameKeyEditedBody.publication.request.draft_etag = 'changed-etag'
      await expect(writeUnsyncedDraft(store, sameKeyEditedBody, scope)).rejects.toBeInstanceOf(DraftPublicationPendingError)
      await expect(store.delete(key)).rejects.toBeInstanceOf(DraftPublicationPendingError)
      await expect(store.get(key)).resolves.toEqual(pending())
    })

    it('drops only the matching envelope on explicit discard and keeps the local body', async () => {
      const store = memoryDraftBufferStore()
      await writeUnsyncedDraft(store, pending(), await store.captureScope('usr_1'))
      await store.discardPublication(key, 'publish-other')
      await expect(store.get(key)).resolves.toEqual(pending())
      await store.discardPublication(key, 'publish-1')
      const kept = await store.get(key)
      expect(kept?.publication).toBeUndefined()
      expect(kept?.payload.title).toBe('sent')
    })

    it('keeps an unknown reservation past expiry and completes only its exact bound request', async () => {
      const store = memoryDraftBufferStore()
      const scope = await store.captureScope('usr_1')
      const reservation = pending({ publication: {
        version: 1, phase: 'reserving', allocationKey: allocation.idempotency_key,
        requestDigest: allocation.request_digest, idempotencyKey: 'publish-1',
        request: { draft_id: 'rdf_sent', draft_etag: 'etag-sent' },
      } })
      await store.set(reservation, scope)
      await store.initialize(DRAFT_BUFFER_TTL_MS * 10)
      expect(await store.get(key)).toEqual(reservation)
      const bound = pending()
      if (reservation.publication?.phase !== 'reserving' || bound.publication?.phase !== 'bound') throw new Error('expected reservation and publication')
      expect(await store.bindPublication(key, reservation.publication, bound.publication, scope)).toBe(true)
      const changed = { ...bound.publication, request: { ...bound.publication.request, draft_etag: 'other' } }
      expect(await store.completePublication(key, changed, scope)).toBe(false)
      expect(await store.get(key)).toEqual(bound)
      expect(await store.completePublication(key, bound.publication, scope)).toBe(true)
      expect(await store.get(key)).toBeUndefined()
    })

    it('cannot resurrect an allocation after the user discards its recovery state', async () => {
      const store = memoryDraftBufferStore()
      const scope = await store.captureScope('usr_1')
      const reservation = pending({ publication: {
        version: 1, phase: 'reserving', allocationKey: allocation.idempotency_key,
        requestDigest: allocation.request_digest, idempotencyKey: 'publish-1',
        request: { draft_id: 'rdf_sent', draft_etag: 'etag-sent' },
      } })
      const bound = pending()
      if (reservation.publication?.phase !== 'reserving' || bound.publication?.phase !== 'bound') throw new Error('expected reservation and publication')
      await store.set(reservation, scope)
      await store.discardPublication(key, 'publish-1')
      expect(await store.bindPublication(key, reservation.publication, bound.publication, scope)).toBe(false)
      expect((await store.get(key))?.publication).toBeUndefined()

      const evidence = recordAllocationFixture('evidence', { purpose: 'evidence_preview' })
      const previewKey = 'usr_1:preview'
      await store.set({ key: previewKey, userId: 'usr_1', payload: payload(''), updatedAt: 1,
        evidenceReservation: { allocationKey: evidence.idempotency_key, requestDigest: evidence.request_digest } }, scope)
      await store.discardEvidenceReservation(previewKey, evidence.idempotency_key)
      expect(await store.bindEvidenceReservation(previewKey, evidence.idempotency_key, evidence.request_digest, evidence, scope)).toBe(false)
      expect(await store.get(previewKey)).toBeUndefined()
    })

    it('is purged by cleanup of its reserved target and cannot be re-bound to a tombstoned target', async () => {
      const store = memoryDraftBufferStore()
      const scope = await store.captureScope('usr_1')
      await writeUnsyncedDraft(store, pending(), scope)
      const event: DraftCleanupEvent = {
        version: 1, cursor: '1', operation_id: 'rpo_purge', record_id: allocation.record_id,
        cleanup_epoch: '1', scopes: ['markdown_draft_buffer'],
      }
      await store.applyCleanup('usr_1', 'browser_1', event)
      await expect(store.get(key)).resolves.toBeUndefined()
      await expect(writeUnsyncedDraft(store, pending(), await store.captureScope('usr_1'))).rejects.toBeInstanceOf(DraftWriteInvalidatedError)
    })

    it('is cleared with the rest of the user on logout', async () => {
      const store = memoryDraftBufferStore()
      await writeUnsyncedDraft(store, pending(), await store.captureScope('usr_1'))
      await discardUserDrafts('usr_1', store)
      await expect(store.get(key)).resolves.toBeUndefined()
    })
  })

  it.each(['cleanup', 'invalidation'] as const)('removes only the draft coupled to an evidence target on %s', async (kind) => {
    const store = memoryDraftBufferStore()
    const userId = 'usr_linked'
    const allocation = recordAllocationFixture('linked', { purpose: 'evidence_preview' })
    const key = draftBufferKey(userId, 'new:vps:vps_a')
    const previewKey = `${key}:evidence-reservation`
    const otherKey = draftBufferKey(userId, 'new:vps:vps_b')
    const otherUserKey = draftBufferKey('usr_other', 'new:vps:vps_a')
    const scope = await store.captureScope(userId)
    await store.set({ key, userId, payload: payload('linked draft'), updatedAt: Date.now() }, scope)
    await store.set({ key: previewKey, userId, payload: payload(''), updatedAt: Date.now(), evidenceReservation: {
      allocationKey: allocation.idempotency_key, requestDigest: allocation.request_digest, allocation,
    } }, scope)
    await store.set({ key: otherKey, userId, payload: payload('other draft'), updatedAt: Date.now() }, scope)
    await store.set({ key: otherUserKey, userId: 'usr_other', payload: payload('other user'), updatedAt: Date.now() }, await store.captureScope('usr_other'))
    if (kind === 'cleanup') {
      await store.applyCleanup(userId, 'browser1', {
        version: 1, cursor: '1', operation_id: 'rpo_linked', record_id: allocation.record_id,
        cleanup_epoch: '1', scopes: ['markdown_draft_buffer', 'markdown_workspace'],
      })
    } else {
      await store.invalidateRecord(userId, allocation.record_id)
    }
    expect(await store.get(key)).toBeUndefined()
    expect(await store.get(previewKey)).toBeUndefined()
    expect((await store.get(otherKey))?.payload.title).toBe('other draft')
    expect((await store.get(otherUserKey))?.payload.title).toBe('other user')
  })
})
