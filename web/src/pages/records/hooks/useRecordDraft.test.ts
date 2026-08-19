import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type { RecordDraft } from '../../../lib/types'
import { draftBufferKey, memoryDraftBufferStore, readUnsyncedDraft, writeUnsyncedDraft } from '../draftBuffer'
import { emptyRecordDraftPayload, recordDetailFixture, recordRevisionFixture } from '../testFixtures'
import { useRecordDraft } from './useRecordDraft'

const api = vi.hoisted(() => ({
  getRecord: vi.fn(),
  getRecordRevision: vi.fn(),
  listRecordDrafts: vi.fn(),
  getRecordDraft: vi.fn(),
  createRecordDraft: vi.fn(),
  patchRecordDraft: vi.fn(),
  createRecord: vi.fn(),
  createRecordRevision: vi.fn(),
  restoreRecordRevision: vi.fn(),
}))

vi.mock('../../../lib/recordsApi', () => api)

describe('useRecordDraft', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.useRealTimers()
    api.createRecordDraft.mockResolvedValue(draftFixture())
    api.patchRecordDraft.mockResolvedValue(draftFixture())
    api.listRecordDrafts.mockResolvedValue({ items: [] })
    api.getRecordDraft.mockResolvedValue(draftFixture())
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('opens a new workspace without fetching a record', () => {
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.payload.markdown_dialect_version).toBe(1)
    expect(api.getRecord).not.toHaveBeenCalled()
  })

  it('loads an existing record and maps it into the editor payload', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    expect(result.current.state.payload.title).toBe('Database outage')
    expect(result.current.state.record?.record_id).toBe('rec_001')
    expect(api.listRecordDrafts).toHaveBeenCalledWith({ limit: 100 })
  })

  it('overlays the unsynced buffer when reopening an edit workspace', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_001'),
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'local unsynced', body_markdown: 'keep local' },
      updatedAt: Date.now(),
    })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.payload.title).toBe('local unsynced'))
    expect(result.current.state.payload.body_markdown).toBe('keep local')
    expect(result.current.state.dirty).toBe(true)
  })

  it('resumes the server draft when reopening an edit workspace', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.listRecordDrafts.mockResolvedValue({
      items: [draftFixture({
        record_id: 'rec_001',
        payload: { ...emptyRecordDraftPayload('usr_1'), title: 'server draft', body_markdown: 'from server' },
      })],
    })
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.payload.title).toBe('server draft'))
    expect(result.current.state.draft?.draft_id).toBe('dft_001')
    expect(result.current.state.dirty).toBe(false)
  })

  it('renders an empty revoked shell after a closed authorization failure', async () => {
    api.getRecord.mockRejectedValue(new ApiError(403, 'forbidden'))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('revoked'))
    expect(result.current.state.payload.body_markdown).toBe('')
    expect(result.current.state.record).toBeNull()
  })

  it('loads the current record alongside a historical revision', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current_revision_id: 'rrv_002',
      current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'current' }),
    }))
    api.getRecordRevision.mockResolvedValue(recordRevisionFixture({
      revision_id: 'rrv_001',
      evidence_snapshot_ids: ['ev_hist'],
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'revision',
      recordId: 'rec_001',
      revisionId: 'rrv_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    expect(result.current.state.revision?.revision_id).toBe('rrv_001')
    expect(result.current.state.record?.current.revision_id).toBe('rrv_002')
    expect(result.current.state.revision?.evidence_snapshot_ids).toEqual(['ev_hist'])
  })

  it('marks the payload dirty when the operator edits metadata', () => {
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'Next' }))
    expect(result.current.state.dirty).toBe(true)
    expect(result.current.state.payload.title).toBe('Next')
  })

  it('creates a draft then publishes a new record', async () => {
    const draft = draftFixture()
    api.createRecordDraft.mockResolvedValue(draft)
    api.createRecord.mockResolvedValue({ record_id: 'rec_new' })
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(api.createRecordDraft).toHaveBeenCalled()
    expect(api.createRecord).toHaveBeenCalledWith({
      draft_id: draft.draft_id,
      draft_etag: draft.etag,
    }, expect.any(String))
    expect(result.current.state.dirty).toBe(false)
    expect(result.current.state.draft).toBeNull()
    expect(result.current.state.publishedRecordId).toBe('rec_new')
  })

  it('patches the latest payload before publishing an existing draft', async () => {
    const first = draftFixture({ etag: 'etag-1' })
    const second = draftFixture({ etag: 'etag-2' })
    api.createRecordDraft.mockResolvedValue(first)
    api.patchRecordDraft.mockResolvedValue(second)
    api.createRecord.mockResolvedValue({ record_id: 'rec_new' })
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'first' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    act(() => result.current.commands.patchPayload({ title: 'second', body_markdown: 'latest body' }))
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(api.patchRecordDraft).toHaveBeenCalledWith(
      first.draft_id,
      { payload: expect.objectContaining({ title: 'second', body_markdown: 'latest body' }) },
      first.etag,
    )
    expect(api.createRecord).toHaveBeenCalledWith({
      draft_id: second.draft_id,
      draft_etag: second.etag,
    }, expect.any(String))
  })

  it('loads the published revision on a read workspace instead of a draft', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current: recordRevisionFixture({
        title: 'published',
        attachment_ids: ['att_pub'],
      }),
    }))
    api.listRecordDrafts.mockResolvedValue({
      items: [draftFixture({
        record_id: 'rec_001',
        payload: {
          ...emptyRecordDraftPayload('usr_1'),
          title: 'unpublished draft',
          attachment_ids: ['att_draft'],
        },
      })],
    })
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    expect(result.current.state.payload.title).toBe('published')
    expect(result.current.state.payload.attachment_ids).toEqual(['att_pub'])
    expect(result.current.state.draft).toBeNull()
    expect(api.listRecordDrafts).not.toHaveBeenCalled()
  })

  // The local buffer has a 24-hour TTL, so a buffered fixture has to be anchored
  // to now. A fixed calendar date silently ages out of the window and then makes
  // the assertions pass for the wrong reason.
  const bufferedAt = Date.now() - 60 * 60 * 1000
  const serverDraftAt = new Date(bufferedAt + 30 * 60 * 1000).toISOString()

  it('lets a newer server draft win over a previously synced buffer', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.listRecordDrafts.mockResolvedValue({
      items: [draftFixture({
        record_id: 'rec_001',
        payload: { ...emptyRecordDraftPayload('usr_1'), title: 'newer server draft' },
        updated_at: serverDraftAt,
      })],
    })
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_001'),
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'stale local' },
      updatedAt: bufferedAt,
    })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.payload.title).toBe('newer server draft'))
    expect(result.current.state.dirty).toBe(false)
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1', 'rec_001'))).resolves.toBeUndefined()
  })

  it('fetches the open draft by id when the listed page omits it', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.listRecordDrafts.mockResolvedValue({
      items: [draftFixture({ record_id: 'rec_other' })],
    })
    api.getRecordDraft.mockResolvedValue(draftFixture({
      draft_id: 'dft_hidden',
      record_id: 'rec_001',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'fetched draft' },
      updated_at: serverDraftAt,
    }))
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_001'),
      userId: 'usr_1',
      recordId: 'rec_001',
      draftId: 'dft_hidden',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'stale local' },
      updatedAt: bufferedAt,
    })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.payload.title).toBe('fetched draft'))
    expect(api.getRecordDraft).toHaveBeenCalledWith('dft_hidden')
    expect(result.current.state.dirty).toBe(false)
  })

  it('discards a late persist after a later successful save', async () => {
    vi.useFakeTimers()
    let releaseWrite: (() => void) | undefined
    const inner = memoryDraftBufferStore()
    const store = {
      get: (key: string) => inner.get(key),
      list: () => inner.list(),
      delete: (key: string) => inner.delete(key),
      async set(value: Parameters<typeof inner.set>[0]) {
        await new Promise<void>((resolve) => {
          releaseWrite = resolve
        })
        await inner.set(value)
      },
    }
    api.createRecordDraft.mockResolvedValue(draftFixture())
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'old' }))
    act(() => {
      vi.advanceTimersByTime(2000)
    })
    await act(async () => {
      await Promise.resolve()
    })
    expect(releaseWrite).toEqual(expect.any(Function))
    act(() => result.current.commands.patchPayload({ title: 'new' }))
    await act(async () => {
      releaseWrite?.()
      await result.current.commands.saveDraft()
    })
    await expect(readUnsyncedDraft(inner, draftBufferKey('usr_1'))).resolves.toBeUndefined()
    vi.useRealTimers()
  })

  it('deletes the synced buffer after a successful draft save', async () => {
    api.createRecordDraft.mockResolvedValue(draftFixture())
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1'),
      userId: 'usr_1',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'buffered' },
      updatedAt: Date.now(),
    })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'saved' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1'))).resolves.toBeUndefined()
  })

  it('waits for an in-flight save then patches the latest payload before publish', async () => {
    let resolveSave: ((value: RecordDraft) => void) | undefined
    api.createRecordDraft.mockImplementationOnce(() => new Promise<RecordDraft>((resolve) => {
      resolveSave = resolve
    }))
    api.patchRecordDraft.mockResolvedValue(draftFixture({ etag: 'etag-2' }))
    api.createRecord.mockResolvedValue({ record_id: 'rec_new' })
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'one' }))
    act(() => {
      void result.current.commands.saveDraft()
    })
    await waitFor(() => expect(resolveSave).toEqual(expect.any(Function)))
    act(() => result.current.commands.patchPayload({ title: 'two', body_markdown: 'latest body' }))
    await act(async () => {
      resolveSave?.(draftFixture({ etag: 'etag-1' }))
    })
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(api.patchRecordDraft).toHaveBeenCalledWith(
      'dft_001',
      { payload: expect.objectContaining({ title: 'two', body_markdown: 'latest body' }) },
      'etag-1',
    )
    expect(api.createRecord).toHaveBeenCalledWith({
      draft_id: 'dft_001',
      draft_etag: 'etag-2',
    }, expect.any(String))
  })

  it('uses the server draft as the conflict server side and refreshes the etag', async () => {
    const serverDraft = draftFixture({
      etag: 'etag-server',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'server draft', body_markdown: 'theirs' },
    })
    api.createRecordDraft.mockRejectedValue(new ApiError(409, 'draft conflict', {
      recovery: { server_draft: serverDraft, local_payload: emptyRecordDraftPayload('usr_1') },
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'mine', body_markdown: 'keep me' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    expect(result.current.state.status).toBe('conflict')
    expect(result.current.state.conflictServer).toMatchObject({ title: 'server draft', body_markdown: 'theirs' })
    expect(result.current.state.draft?.etag).toBe('etag-server')
    expect(result.current.state.payload.body_markdown).toBe('keep me')
  })

  it('keeps the editor payload after an ordinary draft save error', async () => {
    api.createRecordDraft.mockRejectedValue(new ApiError(400, 'title required'))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'Draft title', body_markdown: 'keep me' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    expect(result.current.state.payload.body_markdown).toBe('keep me')
    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.message).toContain('title required')
    expect(result.current.state.dirty).toBe(true)
  })

  it('keeps local input when draft save reports a conflict during publish', async () => {
    api.createRecordDraft.mockRejectedValue(new ApiError(409, 'draft conflict', {
      recovery: { server_draft: draftFixture(), local_payload: emptyRecordDraftPayload('usr_1') },
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ body_markdown: 'keep me' }))
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(result.current.state.status).toBe('conflict')
    expect(result.current.state.payload.body_markdown).toBe('keep me')
  })

  it('keeps dirty true when the operator types during an in-flight draft save', async () => {
    let resolveSave: ((value: RecordDraft) => void) | undefined
    api.createRecordDraft.mockImplementationOnce(() => new Promise<RecordDraft>((resolve) => {
      resolveSave = resolve
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'one' }))
    let savePromise: Promise<void> = Promise.resolve()
    act(() => {
      savePromise = result.current.commands.saveDraft()
    })
    await waitFor(() => expect(resolveSave).toEqual(expect.any(Function)))
    act(() => result.current.commands.patchPayload({ title: 'two' }))
    await act(async () => {
      resolveSave?.(draftFixture())
      await savePromise
    })
    expect(result.current.state.payload.title).toBe('two')
    expect(result.current.state.dirty).toBe(true)
  })

  it('opens the conflict resolver when formal save reports a newer revision', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.createRecordDraft.mockResolvedValue(draftFixture({
      record_id: 'rec_001',
      base_revision_id: 'rrv_001',
    }))
    api.createRecordRevision.mockRejectedValue(new ApiError(409, 'revision advanced', {
      recovery: { server_revision_id: 'rrv_002' },
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(result.current.state.status).toBe('conflict')
    expect(result.current.state.conflictPayload).not.toBeNull()
  })

  it('loads the advanced server revision before opening the conflict resolver', async () => {
    const first = recordDetailFixture()
    const second = recordDetailFixture({
      current_revision_id: 'rrv_002',
      lock_version: 8,
      current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'server advanced', body_markdown: 'theirs' }),
    })
    api.getRecord.mockResolvedValueOnce(first).mockResolvedValueOnce(second)
    api.createRecordDraft.mockResolvedValue(draftFixture({
      record_id: 'rec_001',
      base_revision_id: 'rrv_001',
    }))
    api.createRecordRevision.mockRejectedValue(new ApiError(409, 'revision advanced', {
      recovery: {
        server_revision_id: 'rrv_002',
        server_lock_version: 8,
        server_authorization_epoch: 5,
      },
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(result.current.state.status).toBe('conflict')
    expect(result.current.state.record?.current.revision_id).toBe('rrv_002')
    expect(result.current.state.record?.lock_version).toBe(8)
    expect(result.current.state.conflictPayload).not.toBeNull()
  })

  it('clears the unsynced buffer when the record is revoked', async () => {
    api.getRecord.mockRejectedValue(new ApiError(403, 'forbidden'))
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_001'),
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: { ...emptyRecordDraftPayload('usr_1'), body_markdown: 'secret' },
      updatedAt: Date.now(),
    })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('revoked'))
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1', 'rec_001'))).resolves.toBeUndefined()
  })

  it('does not restore a late buffer after authorization is revoked', async () => {
    api.getRecord.mockRejectedValue(new ApiError(403, 'forbidden'))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('revoked'))
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_001'),
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'resurrected', body_markdown: 'secret' },
      updatedAt: Date.now(),
    })
    await act(async () => {
      window.dispatchEvent(new Event('pageshow'))
    })
    expect(result.current.state.status).toBe('revoked')
    expect(result.current.state.payload.body_markdown).toBe('')
    expect(result.current.state.payload.title).toBe('')
  })

  it('revokes when the tab becomes visible and the record is no longer authorized', async () => {
    api.getRecord
      .mockResolvedValueOnce(recordDetailFixture())
      .mockRejectedValueOnce(new ApiError(404, 'gone'))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
    })
    await waitFor(() => expect(result.current.state.status).toBe('revoked'))
    expect(result.current.state.payload.body_markdown).toBe('')
  })

  it('revokes on a persisted pageshow when the record is no longer authorized', async () => {
    api.getRecord
      .mockResolvedValueOnce(recordDetailFixture())
      .mockRejectedValueOnce(new ApiError(410, 'gone'))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    const event = new Event('pageshow')
    Object.defineProperty(event, 'persisted', { value: true })
    await act(async () => {
      window.dispatchEvent(event)
    })
    await waitFor(() => expect(result.current.state.status).toBe('revoked'))
    expect(result.current.state.payload.body_markdown).toBe('')
  })

  it('revokes when the network comes back and the record is no longer authorized', async () => {
    api.getRecord
      .mockResolvedValueOnce(recordDetailFixture())
      .mockRejectedValueOnce(new ApiError(403, 'gone'))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await act(async () => {
      window.dispatchEvent(new Event('online'))
    })
    await waitFor(() => expect(result.current.state.status).toBe('revoked'))
    expect(result.current.state.payload.body_markdown).toBe('')
  })

  it('lets the next record save again after a revoked one', async () => {
    api.getRecord
      .mockRejectedValueOnce(new ApiError(403, 'gone'))
      .mockResolvedValue(recordDetailFixture({ record_id: 'rec_002' }))
    const store = memoryDraftBufferStore()
    const { result, rerender } = renderHook((props: { recordId: string }) => useRecordDraft({
      mode: 'edit',
      recordId: props.recordId,
      userId: 'usr_1',
      store,
    }), { initialProps: { recordId: 'rec_001' } })
    await waitFor(() => expect(result.current.state.status).toBe('revoked'))

    rerender({ recordId: 'rec_002' })
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => {
      result.current.commands.patchPayload({ title: 'next record' })
    })
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    expect(api.createRecordDraft).toHaveBeenCalled()
  })

  it('does not apply an unsynced buffer on a read workspace', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'read',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_001'),
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'from pageshow' },
      updatedAt: Date.now(),
    })
    await act(async () => {
      window.dispatchEvent(new Event('pageshow'))
    })
    expect(result.current.state.payload.title).toBe('Database outage')
    expect(result.current.state.dirty).toBe(false)
  })

  it('applies the unsynced buffer on pageshow when the editor is not dirty', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'edit',
      recordId: 'rec_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_001'),
      userId: 'usr_1',
      recordId: 'rec_001',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'from pageshow' },
      updatedAt: Date.now(),
    })
    await act(async () => {
      window.dispatchEvent(new Event('pageshow'))
    })
    await waitFor(() => expect(result.current.state.payload.title).toBe('from pageshow'))
    expect(result.current.state.dirty).toBe(true)
  })

  it('registers a leave warning while the payload is dirty', () => {
    const add = vi.spyOn(window, 'addEventListener')
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    act(() => result.current.commands.patchPayload({ title: 'unsaved' }))
    expect(add).toHaveBeenCalledWith('beforeunload', expect.any(Function))
    add.mockRestore()
  })

  it('restores a historical revision as a new formal save', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current: recordRevisionFixture({ revision_id: 'rrv_002' }),
      current_revision_id: 'rrv_002',
    }))
    api.getRecordRevision.mockResolvedValue(recordDetailFixture().current)
    api.restoreRecordRevision.mockResolvedValue(recordDetailFixture({
      current: recordDetailFixture().current,
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({
      mode: 'revision',
      recordId: 'rec_001',
      revisionId: 'rrv_001',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await act(async () => {
      await result.current.commands.restore('restore known good')
    })
    expect(api.restoreRecordRevision).toHaveBeenCalledWith(
      'rec_001',
      'rrv_001',
      { save_reason: 'restore known good' },
      expect.any(String),
    )
    const firstKey = api.restoreRecordRevision.mock.calls[0]?.[3]
    await act(async () => {
      await result.current.commands.restore('restore known good')
    })
    expect(api.restoreRecordRevision.mock.calls[1]?.[3]).toBe(firstKey)
    expect(result.current.state.restoredToRecordId).toBe('rec_001')
    expect(result.current.state.record?.current.revision_id).toBe('rrv_002')
  })
})

function draftFixture(overrides: Partial<RecordDraft> = {}): RecordDraft {
  return {
    draft_id: 'dft_001',
    payload: emptyRecordDraftPayload('usr_1'),
    version: 1,
    etag: 'etag-1',
    warning_at: '2026-08-18T00:00:00Z',
    created_at: '2026-08-18T00:00:00Z',
    updated_at: '2026-08-18T00:00:00Z',
    expires_at: '2026-08-19T00:00:00Z',
    ...overrides,
  }
}
