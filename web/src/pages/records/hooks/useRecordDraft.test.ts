import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../../../lib/apiRequest'
import type { RecordDraft } from '../../../lib/types'
import { draftBufferKey, draftBufferRecordId, memoryDraftBufferStore, readUnsyncedDraft, writeUnsyncedDraft } from '../draftBuffer'
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

  it('keeps buffering locally but pauses server autosave while a conflict resolver is open', async () => {
    vi.useFakeTimers()
    api.patchRecordDraft.mockRejectedValueOnce(new ApiError(409, 'draft changed', {
      code: 'draft_conflict',
      recovery: { server_draft: draftFixture({ etag: 'etag-other' }), local_payload: emptyRecordDraftPayload('usr_1') },
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'new', userId: 'usr_1', store }))
    act(() => result.current.commands.patchPayload({ title: 'first' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    act(() => result.current.commands.patchPayload({ title: 'second' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    expect(result.current.state.status).toBe('conflict')
    expect(api.patchRecordDraft).toHaveBeenCalledTimes(1)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(api.patchRecordDraft).toHaveBeenCalledTimes(1)
    // 刷新或关闭页面不会走卸载补写：冲突中的本地修改必须已经落到缓冲。
    expect((await readUnsyncedDraft(store, draftBufferKey('usr_1')))?.payload.title).toBe('second')
    vi.useRealTimers()
  })

  it.each([
    ['dismissing', 'dismiss'],
    ['keeping the same local payload', 'resolve'],
] as const)('resumes server autosave after %s once the conflict timer ran out', async (_label, action) => {
    vi.useFakeTimers()
    api.patchRecordDraft.mockRejectedValueOnce(new ApiError(409, 'draft changed', {
      code: 'draft_conflict',
      recovery: { server_draft: draftFixture({ etag: 'etag-other' }), local_payload: emptyRecordDraftPayload('usr_1') },
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'new', userId: 'usr_1', store }))
    act(() => result.current.commands.patchPayload({ title: 'first' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    act(() => result.current.commands.patchPayload({ title: 'second' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000)
    })
    expect(api.patchRecordDraft).toHaveBeenCalledTimes(1)

    act(() => {
      if (action === 'dismiss') result.current.commands.dismissConflict()
      else result.current.commands.resolveConflict(result.current.state.payload)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500)
    })
    expect(api.patchRecordDraft).toHaveBeenCalledTimes(2)
    expect(api.patchRecordDraft).toHaveBeenLastCalledWith('dft_001', { payload: expect.objectContaining({ title: 'second' }) }, 'etag-other')
    vi.useRealTimers()
  })

  it('drops a queued autosave when the conflict lands after the workspace unmounted', async () => {
    vi.useFakeTimers()
    let rejectPatch: (error: unknown) => void = () => undefined
    api.patchRecordDraft.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectPatch = reject }))
    const store = memoryDraftBufferStore()
    const { result, unmount } = renderHook(() => useRecordDraft({ mode: 'new', userId: 'usr_1', store }))
    act(() => result.current.commands.patchPayload({ title: 'first' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    act(() => result.current.commands.patchPayload({ title: 'second' }))
    let saving: Promise<void> = Promise.resolve()
    act(() => {
      saving = result.current.commands.saveDraft()
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500)
    })
    unmount()
    await act(async () => {
      rejectPatch(new ApiError(409, 'draft changed', {
        code: 'draft_conflict',
        recovery: { server_draft: draftFixture({ etag: 'etag-other' }), local_payload: emptyRecordDraftPayload('usr_1') },
      }))
      await saving
      await vi.advanceTimersByTimeAsync(100)
    })
    // 卸载后不再有解决器，排队的自动保存不能用服务端新 ETag 把本地内容写回去。
    expect(api.patchRecordDraft).toHaveBeenCalledTimes(1)
    vi.useRealTimers()
  })

  it('drops an autosave already queued behind a save that lands a conflict', async () => {
    vi.useFakeTimers()
    let rejectPatch: (error: unknown) => void = () => undefined
    api.patchRecordDraft.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectPatch = reject }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'new', userId: 'usr_1', store }))
    act(() => result.current.commands.patchPayload({ title: 'first' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    act(() => result.current.commands.patchPayload({ title: 'second' }))
    let saving: Promise<void> = Promise.resolve()
    act(() => {
      saving = result.current.commands.saveDraft()
    })
    // 手动保存的响应超过自动保存间隔：定时器已触发，自动保存排在保存链上。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500)
    })
    await act(async () => {
      rejectPatch(new ApiError(409, 'draft changed', {
        code: 'draft_conflict',
        recovery: { server_draft: draftFixture({ etag: 'etag-other' }), local_payload: emptyRecordDraftPayload('usr_1') },
      }))
      await saving
      await vi.advanceTimersByTimeAsync(100)
    })
    expect(result.current.state.status).toBe('conflict')
    expect(api.patchRecordDraft).toHaveBeenCalledTimes(1)
    vi.useRealTimers()
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

  it.each([
    { name: 'keeps every existing evidence snapshot in order', ids: ['evs_b', 'evs_a', 'evs_c'] },
    { name: 'sends an explicit empty evidence list when the record has none', ids: [] as string[] },
  ])('formal revision save $name', async ({ ids }) => {
    // 修订的证据是请求里的有序全集；缺省会让后端把新修订的证据置空。
    api.getRecord.mockResolvedValue(recordDetailFixture({
      current: recordRevisionFixture({ evidence_snapshot_ids: ids }),
    }))
    api.createRecordDraft.mockResolvedValue(draftFixture({ record_id: 'rec_001', base_revision_id: 'rrv_001' }))
    api.createRecordRevision.mockResolvedValue({ record_id: 'rec_001' })
    // store 必须在渲染外创建：hook 的加载 effect 依赖其引用，每次渲染新建会无限重载。
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
    expect(api.createRecordRevision).toHaveBeenCalledWith('rec_001', expect.objectContaining({
      evidence_items: ids.map((id) => ({ existing_snapshot_id: id })),
    }), expect.any(String))
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

  describe('after a formal revision conflict', () => {
    const first = recordDetailFixture()
    const second = recordDetailFixture({
      current_revision_id: 'rrv_002',
      lock_version: 8,
      authorization_epoch: 5,
      current: recordRevisionFixture({ revision_id: 'rrv_002', title: 'server advanced', evidence_snapshot_ids: ['evs_server'] }),
    })
    const third = recordDetailFixture({
      current_revision_id: 'rrv_003',
      lock_version: 9,
      authorization_epoch: 6,
      current: recordRevisionFixture({ revision_id: 'rrv_003', title: 'advanced again', evidence_snapshot_ids: ['evs_third'] }),
    })
    const staleDraft = draftFixture({ draft_id: 'rdf_old', etag: 'etag-old', record_id: 'rec_001', base_revision_id: 'rrv_001' })
    const revisionConflict = (serverRevisionId: string, draft?: RecordDraft) => new ApiError(409, 'revision advanced', {
      recovery: { server_revision_id: serverRevisionId, server_lock_version: 8, server_authorization_epoch: 5, ...(draft ? { draft } : {}) },
    })

    async function openConflict() {
      api.getRecord.mockResolvedValueOnce(first).mockResolvedValue(second)
      api.createRecordDraft.mockResolvedValueOnce(staleDraft)
      api.createRecordRevision
        .mockRejectedValueOnce(revisionConflict('rrv_002', staleDraft))
        .mockResolvedValueOnce({ record_id: 'rec_001' })
      const store = memoryDraftBufferStore()
      const hook = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
      await waitFor(() => expect(hook.result.current.state.status).toBe('ready'))
      act(() => hook.result.current.commands.patchPayload({ title: 'mine' }))
      await act(async () => {
        await hook.result.current.commands.publish()
      })
      expect(hook.result.current.state.status).toBe('conflict')
      return hook
    }

    it('rebases the same draft onto the confirmed head, then publishes against that head', async () => {
      api.patchRecordDraft.mockResolvedValueOnce(draftFixture({ draft_id: 'rdf_old', etag: 'etag-rebased', record_id: 'rec_001', base_revision_id: 'rrv_002' }))
      const { result } = await openConflict()

      const merged = { ...result.current.state.payload, title: 'merged' }
      act(() => result.current.commands.resolveConflict(merged))
      await act(async () => {
        await result.current.commands.publish()
      })

      // 草稿 ID 不变（附件归属随之保留），由服务端在同一事务里核对 ETag 与当前头后改基准。
      expect(api.createRecordDraft).toHaveBeenCalledTimes(1)
      expect(api.patchRecordDraft).toHaveBeenCalledWith('rdf_old', {
        payload: expect.objectContaining({ title: 'merged' }),
        base_revision_id: 'rrv_002',
      }, 'etag-old')
      expect(api.createRecordRevision).toHaveBeenLastCalledWith('rec_001', expect.objectContaining({
        draft_id: 'rdf_old',
        draft_etag: 'etag-rebased',
        base_revision_id: 'rrv_002',
        lock_version: 8,
        authorization_epoch: 5,
        evidence_items: [{ existing_snapshot_id: 'evs_server' }],
      }), expect.any(String))
      expect(result.current.state.status).toBe('ready')
    })

    it('keeps the confirmed head when a background refresh sees a newer one', async () => {
      api.patchRecordDraft.mockResolvedValueOnce(draftFixture({ draft_id: 'rdf_old', etag: 'etag-rebased', record_id: 'rec_001', base_revision_id: 'rrv_002' }))
      const { result } = await openConflict()
      act(() => result.current.commands.resolveConflict({ ...result.current.state.payload, title: 'merged' }))
      api.getRecord.mockResolvedValue(third)
      await act(async () => {
        document.dispatchEvent(new Event('visibilitychange'))
      })
      await waitFor(() => expect(result.current.state.record?.current_revision_id).toBe('rrv_003'))

      await act(async () => {
        await result.current.commands.publish()
      })

      // 用户没看过 rrv_003，不能拿它的锁版本与证据替用户发布；服务端会因头已变再次拒绝。
      expect(api.patchRecordDraft).toHaveBeenCalledWith('rdf_old', expect.objectContaining({ base_revision_id: 'rrv_002' }), 'etag-old')
      expect(api.createRecordRevision).toHaveBeenLastCalledWith('rec_001', expect.objectContaining({
        base_revision_id: 'rrv_002',
        lock_version: 8,
        evidence_items: [{ existing_snapshot_id: 'evs_server' }],
      }), expect.any(String))
    })

    it('reopens the resolver on the newer head when it moves again before the rebase lands', async () => {
      const { result } = await openConflict()
      act(() => result.current.commands.resolveConflict({ ...result.current.state.payload, title: 'merged' }))
      api.getRecord.mockResolvedValue(third)
      api.patchRecordDraft
        .mockRejectedValueOnce(revisionConflict('rrv_003', staleDraft))
        .mockResolvedValueOnce(draftFixture({ draft_id: 'rdf_old', etag: 'etag-third', record_id: 'rec_001', base_revision_id: 'rrv_003' }))

      await act(async () => {
        await result.current.commands.publish()
      })
      expect(result.current.state.status).toBe('conflict')
      expect(result.current.state.record?.current_revision_id).toBe('rrv_003')
      expect(api.createRecordRevision).toHaveBeenCalledTimes(1)

      act(() => result.current.commands.resolveConflict({ ...result.current.state.payload, title: 'merged twice' }))
      await act(async () => {
        await result.current.commands.saveDraft()
      })
      expect(api.patchRecordDraft).toHaveBeenLastCalledWith('rdf_old', expect.objectContaining({ base_revision_id: 'rrv_003' }), 'etag-old')
    })

    it('leaves the draft base alone when the operator dismisses the conflict', async () => {
      const { result } = await openConflict()
      act(() => result.current.commands.dismissConflict())
      act(() => result.current.commands.patchPayload({ title: 'mine again' }))
      await act(async () => {
        await result.current.commands.saveDraft()
      })
      expect(api.patchRecordDraft).toHaveBeenCalledWith('rdf_old', { payload: expect.objectContaining({ title: 'mine again' }) }, 'etag-old')
    })

    it('keeps the pre-conflict base when the operator dismisses a create conflict', async () => {
      api.getRecord.mockResolvedValueOnce(first).mockResolvedValue(second)
      api.createRecordDraft
        .mockRejectedValueOnce(revisionConflict('rrv_002'))
        .mockRejectedValueOnce(revisionConflict('rrv_002'))
      const store = memoryDraftBufferStore()
      const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
      await waitFor(() => expect(result.current.state.status).toBe('ready'))
      act(() => result.current.commands.patchPayload({ title: 'mine' }))
      await act(async () => {
        await result.current.commands.saveDraft()
      })
      act(() => result.current.commands.dismissConflict())

      await act(async () => {
        await result.current.commands.publish()
      })

      // 用户没确认 rrv_002：仍按冲突前的 rrv_001 创建，服务端再次拒绝并重新打开解决器，不会按新头发布。
      expect(api.createRecordDraft).toHaveBeenLastCalledWith(expect.objectContaining({ base_revision_id: 'rrv_001' }))
      expect(api.createRecordRevision).not.toHaveBeenCalled()
      expect(result.current.state.status).toBe('conflict')
    })

    it('binds the resolver to the conflict snapshot even when an older background read lands later', async () => {
      let resolveStaleRead: (detail: typeof first) => void = () => undefined
      api.getRecord
        .mockResolvedValueOnce(first)
        .mockReturnValueOnce(new Promise((resolve) => { resolveStaleRead = resolve }))
        .mockResolvedValue(second)
      api.createRecordDraft.mockResolvedValueOnce(staleDraft)
      api.createRecordRevision
        .mockRejectedValueOnce(revisionConflict('rrv_002', staleDraft))
        .mockResolvedValueOnce({ record_id: 'rec_001' })
      api.patchRecordDraft.mockResolvedValueOnce(draftFixture({ draft_id: 'rdf_old', etag: 'etag-rebased', record_id: 'rec_001', base_revision_id: 'rrv_002' }))
      const store = memoryDraftBufferStore()
      const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
      await waitFor(() => expect(result.current.state.status).toBe('ready'))
      act(() => result.current.commands.patchPayload({ title: 'mine' }))
      await act(async () => {
        document.dispatchEvent(new Event('visibilitychange'))
      })
      await act(async () => {
        await result.current.commands.publish()
      })
      expect(result.current.state.conflictServer?.title).toBe('server advanced')

      await act(async () => {
        resolveStaleRead(first)
      })
      expect(result.current.state.record?.current_revision_id).toBe('rrv_002')
      expect(result.current.state.conflictServer?.title).toBe('server advanced')

      act(() => result.current.commands.resolveConflict({ ...result.current.state.payload, title: 'merged' }))
      await act(async () => {
        await result.current.commands.publish()
      })
      expect(api.patchRecordDraft).toHaveBeenCalledWith('rdf_old', expect.objectContaining({ base_revision_id: 'rrv_002' }), 'etag-old')
    })

    it('asks for confirmation when only the lock version or authorization epoch moved', async () => {
      const relocked = recordDetailFixture({ lock_version: 9, authorization_epoch: 6 })
      api.getRecord.mockResolvedValueOnce(first).mockResolvedValue(relocked)
      api.createRecordDraft.mockResolvedValueOnce(staleDraft)
      api.patchRecordDraft.mockResolvedValueOnce(draftFixture({ draft_id: 'rdf_old', etag: 'etag-saved', record_id: 'rec_001', base_revision_id: 'rrv_001' }))
      // 同一修订上的 CAS 失败只有错误码，没有 recovery。
      api.createRecordRevision
        .mockRejectedValueOnce(new ApiError(409, 'record revision changed', { code: 'record_revision_conflict' }))
        .mockResolvedValueOnce({ record_id: 'rec_001' })
      const store = memoryDraftBufferStore()
      const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
      await waitFor(() => expect(result.current.state.status).toBe('ready'))
      act(() => result.current.commands.patchPayload({ title: 'mine' }))
      await act(async () => {
        await result.current.commands.publish()
      })
      expect(result.current.state.status).toBe('conflict')

      act(() => result.current.commands.resolveConflict({ ...result.current.state.payload, title: 'merged' }))
      await act(async () => {
        await result.current.commands.publish()
      })
      expect(api.patchRecordDraft).toHaveBeenCalledWith('rdf_old', { payload: expect.objectContaining({ title: 'merged' }) }, 'etag-old')
      expect(api.createRecordRevision).toHaveBeenLastCalledWith('rec_001', expect.objectContaining({
        draft_etag: 'etag-saved',
        base_revision_id: 'rrv_001',
        lock_version: 9,
        authorization_epoch: 6,
      }), expect.any(String))
    })

    it('reports instead of opening an unconfirmable resolver when the new head cannot be read', async () => {
      api.getRecord.mockResolvedValueOnce(first).mockRejectedValue(new ApiError(503, 'record service unavailable'))
      api.createRecordDraft.mockResolvedValueOnce(staleDraft)
      api.createRecordRevision.mockRejectedValueOnce(revisionConflict('rrv_002', staleDraft))
      const store = memoryDraftBufferStore()
      const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
      await waitFor(() => expect(result.current.state.status).toBe('ready'))
      act(() => result.current.commands.patchPayload({ title: 'mine' }))
      await act(async () => {
        await result.current.commands.publish()
      })
      expect(result.current.state.status).toBe('ready')
      expect(result.current.state.conflictPayload).toBeNull()
      expect(result.current.state.message).toBe('记录已有新修订，暂时无法读取，请稍后重试')
    })

    it('holds server saves while the resolver is open', async () => {
      const { result } = await openConflict()
      const patches = api.patchRecordDraft.mock.calls.length
      await act(async () => {
        await result.current.commands.saveDraft()
      })
      expect(api.patchRecordDraft).toHaveBeenCalledTimes(patches)
    })

    it('does not confirm a hidden head when an in-flight save lands a draft conflict afterwards', async () => {
      api.getRecord.mockResolvedValueOnce(first).mockResolvedValue(second)
      api.createRecordDraft.mockResolvedValueOnce(staleDraft)
      let rejectRevision: (error: unknown) => void = () => undefined
      api.createRecordRevision.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectRevision = reject }))
      const store = memoryDraftBufferStore()
      const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
      await waitFor(() => expect(result.current.state.status).toBe('ready'))
      act(() => result.current.commands.patchPayload({ title: 'mine' }))
      let publishing: Promise<void> = Promise.resolve()
      await act(async () => {
        publishing = result.current.commands.publish()
        await waitFor(() => expect(api.createRecordRevision).toHaveBeenCalled())
      })

      // 发布请求在途时另一处保存已发出，随后才收到草稿冲突。
      const otherTab = draftFixture({
        draft_id: 'rdf_old',
        etag: 'etag-other',
        record_id: 'rec_001',
        base_revision_id: 'rrv_001',
        payload: { ...emptyRecordDraftPayload('usr_1'), title: 'other tab' },
      })
      let rejectPatch: (error: unknown) => void = () => undefined
      api.patchRecordDraft.mockReturnValueOnce(new Promise((_resolve, reject) => { rejectPatch = reject }))
      act(() => result.current.commands.patchPayload({ title: 'mine again' }))
      let saving: Promise<void> = Promise.resolve()
      await act(async () => {
        saving = result.current.commands.saveDraft()
        await waitFor(() => expect(api.patchRecordDraft).toHaveBeenCalled())
      })
      await act(async () => {
        rejectRevision(revisionConflict('rrv_002', staleDraft))
        await publishing
      })
      expect(result.current.state.conflictServer?.title).toBe('server advanced')
      await act(async () => {
        rejectPatch(new ApiError(409, 'draft changed', {
          code: 'draft_conflict',
          recovery: { server_draft: otherTab, local_payload: result.current.state.payload },
        }))
        await saving
      })
      expect(result.current.state.conflictServer?.title).toBe('other tab')

      act(() => result.current.commands.resolveConflict({ ...result.current.state.payload, title: 'merged with tab' }))
      await act(async () => {
        await result.current.commands.saveDraft()
      })
      // 用户合并的是草稿冲突，没看过 rrv_002：保存不得改基准。
      expect(api.patchRecordDraft).toHaveBeenLastCalledWith('rdf_old', { payload: expect.objectContaining({ title: 'merged with tab' }) }, 'etag-other')
    })

    it('creates the first draft on the confirmed head when the create itself hit a stale base', async () => {
      api.getRecord.mockResolvedValueOnce(first).mockResolvedValue(second)
      api.createRecordDraft
        .mockRejectedValueOnce(revisionConflict('rrv_002'))
        .mockResolvedValueOnce(draftFixture({ draft_id: 'rdf_new', record_id: 'rec_001', base_revision_id: 'rrv_002' }))
      const store = memoryDraftBufferStore()
      const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
      await waitFor(() => expect(result.current.state.status).toBe('ready'))
      act(() => result.current.commands.patchPayload({ title: 'mine' }))
      await act(async () => {
        await result.current.commands.saveDraft()
      })
      expect(result.current.state.status).toBe('conflict')

      act(() => result.current.commands.resolveConflict({ ...result.current.state.payload, title: 'merged' }))
      await act(async () => {
        await result.current.commands.saveDraft()
      })
      expect(api.createRecordDraft).toHaveBeenLastCalledWith({
        record_id: 'rec_001',
        base_revision_id: 'rrv_002',
        payload: expect.objectContaining({ title: 'merged' }),
      })
    })
  })

  it('adds an uploaded attachment to both the payload and an open conflict without dismissing it', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.patchRecordDraft.mockRejectedValueOnce(new ApiError(409, 'draft changed', {
      code: 'draft_conflict',
      recovery: { server_draft: draftFixture({ etag: 'etag-other' }), local_payload: emptyRecordDraftPayload('usr_1') },
    }))
    api.listRecordDrafts.mockResolvedValue({ items: [draftFixture({ record_id: 'rec_001', base_revision_id: 'rrv_001' })] })
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => result.current.commands.patchPayload({ title: 'mine' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    expect(result.current.state.status).toBe('conflict')

    act(() => result.current.commands.addAttachment('att_new'))
    act(() => result.current.commands.addAttachment('att_new'))

    expect(result.current.state.status).toBe('conflict')
    expect(result.current.state.payload.attachment_ids).toEqual(['att_new'])
    expect(result.current.state.conflictPayload?.attachment_ids).toEqual(['att_new'])
  })

  it('refuses to hand out the draft for uploads while a conflict is open', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.listRecordDrafts.mockResolvedValue({ items: [draftFixture({ record_id: 'rec_001', base_revision_id: 'rrv_001' })] })
    api.patchRecordDraft.mockRejectedValueOnce(new ApiError(409, 'draft changed', {
      code: 'draft_conflict',
      recovery: { server_draft: draftFixture({ etag: 'etag-other' }), local_payload: emptyRecordDraftPayload('usr_1') },
    }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    await expect(result.current.commands.ensureDraft()).resolves.toEqual(expect.objectContaining({ draft_id: 'dft_001' }))
    act(() => result.current.commands.patchPayload({ title: 'mine' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    expect(result.current.state.status).toBe('conflict')
    // 已有服务端草稿也不能交出：上传会绕过解决器，被确认的内容可能不引用它。
    await expect(result.current.commands.ensureDraft()).resolves.toBeUndefined()
  })

  it('refuses to hand out the draft for uploads while a publish is consuming it', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.createRecordDraft.mockResolvedValue(draftFixture({ record_id: 'rec_001', base_revision_id: 'rrv_001' }))
    let finishPublish: (value: unknown) => void = () => undefined
    api.createRecordRevision.mockReturnValueOnce(new Promise((resolve) => { finishPublish = resolve }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => result.current.commands.patchPayload({ title: 'mine' }))
    let publishing: Promise<void> = Promise.resolve()
    await act(async () => {
      publishing = result.current.commands.publish()
      await waitFor(() => expect(api.createRecordRevision).toHaveBeenCalled())
    })
    expect(result.current.commands.isPublishing()).toBe(true)
    await expect(result.current.commands.ensureDraft()).resolves.toBeUndefined()
    await act(async () => {
      finishPublish({ record_id: 'rec_001' })
      await publishing
    })
    expect(result.current.commands.isPublishing()).toBe(false)
  })

  it('lets go of the consumed draft as soon as publish succeeds even if the refresh read fails', async () => {
    api.getRecord.mockResolvedValueOnce(recordDetailFixture()).mockRejectedValue(new ApiError(500, 'refresh failed'))
    api.createRecordDraft.mockResolvedValue(draftFixture({ record_id: 'rec_001', base_revision_id: 'rrv_001' }))
    api.createRecordRevision.mockResolvedValueOnce({ record_id: 'rec_001' })
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => result.current.commands.patchPayload({ title: 'mine' }))
    await act(async () => {
      await result.current.commands.publish()
    })
    // 服务端已在发布事务中删除草稿：读取失败也不能再把它交给上传入口。
    expect(result.current.state.draft).toBeNull()
    api.createRecordDraft.mockClear()
    api.createRecordDraft.mockResolvedValueOnce(draftFixture({ draft_id: 'dft_next', record_id: 'rec_001', base_revision_id: 'rrv_001' }))
    await expect(result.current.commands.ensureDraft()).resolves.toEqual(expect.objectContaining({ draft_id: 'dft_next' }))
    expect(api.createRecordDraft).toHaveBeenCalledTimes(1)
  })

  it('explains a publish refused while draft attachments are still being checked', async () => {
    api.getRecord.mockResolvedValue(recordDetailFixture())
    api.createRecordDraft.mockResolvedValue(draftFixture({ record_id: 'rec_001', base_revision_id: 'rrv_001' }))
    api.createRecordRevision.mockRejectedValueOnce(new ApiError(409, 'draft attachments are still processing', { code: 'draft_attachments_busy' }))
    const store = memoryDraftBufferStore()
    const { result } = renderHook(() => useRecordDraft({ mode: 'edit', recordId: 'rec_001', userId: 'usr_1', store }))
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => result.current.commands.patchPayload({ title: 'mine' }))
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(result.current.state.status).toBe('ready')
    expect(result.current.state.message).toBe('附件仍在安全检查，请稍后再发布')
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

  it('keeps an unscoped new buffer when opening a canonical subject prefill', async () => {
    const store = memoryDraftBufferStore()
    const seedB = [{
      registry_version: 1 as const,
      kind: 'vps' as const,
      role: 'affected' as const,
      source_id: 'vps_001',
      primary: true,
    }]
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1'),
      userId: 'usr_1',
      payload: {
        ...emptyRecordDraftPayload('usr_1'),
        title: 'draft A',
        subjects: [{
          registry_version: 1,
          kind: 'vps',
          role: 'affected',
          source_id: 'vps_other',
          primary: true,
        }],
      },
      updatedAt: Date.now(),
    })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
      seedSubjects: seedB,
    }))
    await waitFor(() => expect(result.current.state.payload.subjects[0]?.source_id).toBe('vps_001'))
    expect(result.current.state.payload.title).not.toBe('draft A')
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1'))).resolves.toMatchObject({
      payload: { title: 'draft A' },
    })
  })

  it('still restores the unscoped new buffer without a subject prefill', async () => {
    const store = memoryDraftBufferStore()
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1'),
      userId: 'usr_1',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'draft A' },
      updatedAt: Date.now(),
    })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
    }))
    await waitFor(() => expect(result.current.state.payload.title).toBe('draft A'))
  })

  it('does not let a late scoped buffer replace edits on a seeded new record', async () => {
    const inner = memoryDraftBufferStore()
    const seedB = [{
      registry_version: 1 as const,
      kind: 'vps' as const,
      role: 'affected' as const,
      source_id: 'vps_001',
      primary: true,
    }]
    await writeUnsyncedDraft(inner, {
      key: draftBufferKey('usr_1', draftBufferRecordId(undefined, seedB)),
      userId: 'usr_1',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'old scoped', subjects: seedB },
      updatedAt: Date.now(),
    })
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const store = {
      async get(key: string) {
        await gate
        return inner.get(key)
      },
      set: (value: Parameters<typeof inner.set>[0]) => inner.set(value),
      delete: (key: string) => inner.delete(key),
      list: () => inner.list(),
    }
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
      seedSubjects: seedB,
    }))
    act(() => result.current.commands.patchPayload({ title: 'typed B' }))
    await act(async () => {
      release?.()
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(result.current.state.payload.title).toBe('typed B')
    expect(result.current.state.payload.subjects[0]?.source_id).toBe('vps_001')
  })

  it('publishes a seeded record without deleting the unscoped new buffer', async () => {
    const store = memoryDraftBufferStore()
    const seedB = [{
      registry_version: 1 as const,
      kind: 'vps' as const,
      role: 'affected' as const,
      source_id: 'vps_001',
      primary: true,
    }]
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1'),
      userId: 'usr_1',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'draft A' },
      updatedAt: Date.now(),
    })
    api.createRecordDraft.mockResolvedValue(draftFixture())
    api.createRecord.mockResolvedValue({ record_id: 'rec_new' })
    const { result } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
      seedSubjects: seedB,
    }))
    act(() => result.current.commands.patchPayload({ title: 'canonical B' }))
    await act(async () => {
      await result.current.commands.publish()
    })
    expect(result.current.state.publishedRecordId).toBe('rec_new')
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1'))).resolves.toMatchObject({
      payload: { title: 'draft A' },
    })
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1', draftBufferRecordId(undefined, seedB)))).resolves.toBeUndefined()
  })

  it('hands off a dirty seeded new draft when the canonical subject changes', async () => {
    const store = memoryDraftBufferStore()
    const seedA = [{
      registry_version: 1 as const,
      kind: 'vps' as const,
      role: 'affected' as const,
      source_id: 'vps_001',
      primary: true,
    }]
    const seedB = [{
      registry_version: 1 as const,
      kind: 'vps' as const,
      role: 'affected' as const,
      source_id: 'vps_002',
      primary: true,
    }]
    const first = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
      seedSubjects: seedA,
    }))
    act(() => first.result.current.commands.patchPayload({ title: 'draft A' }))
    first.unmount()
    const second = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
      seedSubjects: seedB,
    }))
    await waitFor(() => expect(second.result.current.state.payload.subjects[0]?.source_id).toBe('vps_002'))
    expect(second.result.current.state.payload.title).toBe('')
    await waitFor(async () => {
      await expect(readUnsyncedDraft(store, draftBufferKey('usr_1', draftBufferRecordId(undefined, seedA)))).resolves.toMatchObject({
        payload: { title: 'draft A' },
      })
    })
    await expect(readUnsyncedDraft(store, draftBufferKey('usr_1', draftBufferRecordId(undefined, seedB)))).resolves.toBeUndefined()
  })


  it('restores a scoped new buffer after unmount following a failed save', async () => {
    const store = memoryDraftBufferStore()
    const seed = [{
      registry_version: 1 as const,
      kind: 'vps' as const,
      role: 'affected' as const,
      source_id: 'vps_001',
      primary: true,
    }]
    api.createRecordDraft.mockRejectedValue(new Error('draft unavailable'))
    const { result, unmount } = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
      seedSubjects: seed,
    }))
    act(() => result.current.commands.patchPayload({ title: 'keep me' }))
    await act(async () => {
      await result.current.commands.saveDraft()
    })
    unmount()
    const again = renderHook(() => useRecordDraft({
      mode: 'new',
      userId: 'usr_1',
      store,
      seedSubjects: seed,
    }))
    await waitFor(() => expect(again.result.current.state.payload.title).toBe('keep me'))
  })

  it('applies the next record buffer when editing another record', async () => {
    const store = memoryDraftBufferStore()
    api.getRecord.mockImplementation(async (recordId: string) => recordDetailFixture({
      record_id: recordId,
      current: recordRevisionFixture({
        record_id: recordId,
        title: `server ${recordId}`,
      }),
    }))
    await writeUnsyncedDraft(store, {
      key: draftBufferKey('usr_1', 'rec_002'),
      userId: 'usr_1',
      recordId: 'rec_002',
      payload: { ...emptyRecordDraftPayload('usr_1'), title: 'buffered 002' },
      updatedAt: Date.now(),
    })
    const { result, rerender } = renderHook(
      ({ recordId }) => useRecordDraft({ mode: 'edit', recordId, userId: 'usr_1', store }),
      { initialProps: { recordId: 'rec_001' } },
    )
    await waitFor(() => expect(result.current.state.status).toBe('ready'))
    act(() => result.current.commands.patchPayload({ title: 'local 001' }))
    rerender({ recordId: 'rec_002' })
    await waitFor(() => expect(result.current.state.payload.title).toBe('buffered 002'))
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
