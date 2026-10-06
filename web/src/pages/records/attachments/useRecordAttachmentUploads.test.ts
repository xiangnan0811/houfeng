import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { RecordDraft } from '../../../lib/types'
import { emptyRecordDraftPayload } from '../testFixtures'
import { useRecordAttachmentUploads } from './useRecordAttachmentUploads'

const api = vi.hoisted(() => ({
  createAttachmentUpload: vi.fn(),
  uploadAttachmentContent: vi.fn(),
  completeAttachmentUpload: vi.fn(),
  getAttachmentMetadata: vi.fn(),
}))

vi.mock('../../../lib/recordsApi', () => api)

const draft: RecordDraft = {
  draft_id: 'rdf_up', payload: emptyRecordDraftPayload('usr_1'), version: 1, etag: 'etag-1',
  warning_at: '2026-08-18T00:00:00Z', created_at: '2026-08-18T00:00:00Z',
  updated_at: '2026-08-18T00:00:00Z', expires_at: '2026-08-19T00:00:00Z',
}

describe('useRecordAttachmentUploads', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('refuses unsupported files locally without creating a draft or reserving quota', async () => {
    const ensureDraft = vi.fn()
    const { result } = renderHook(() => useRecordAttachmentUploads({ ensureDraft, isPublishing: () => false, onAvailable: vi.fn() }))
    await act(async () => {
      await result.current.addFiles([new File(['x'], 'capture.pcap')])
    })
    expect(result.current.rows).toEqual([expect.objectContaining({ display_name: 'capture.pcap', status: 'rejected', error: '不支持的文件类型' })])
    expect(ensureDraft).not.toHaveBeenCalled()
    expect(api.createAttachmentUpload).not.toHaveBeenCalled()
    act(() => result.current.remove(result.current.rows[0]!.client_id))
    expect(result.current.rows).toEqual([])
  })

  it('explains when no draft can be saved to own the upload', async () => {
    const { result } = renderHook(() => useRecordAttachmentUploads({ ensureDraft: vi.fn().mockResolvedValue(undefined), isPublishing: () => false, onAvailable: vi.fn() }))
    await act(async () => {
      await result.current.addFiles([new File(['x'], 'trace.log')])
    })
    expect(result.current.notice).toBe('草稿暂不可保存，附件未上传')
    expect(api.createAttachmentUpload).not.toHaveBeenCalled()
  })

  it('uploads under the draft with the backend media type and hands available attachments to the draft', async () => {
    api.createAttachmentUpload.mockResolvedValue({
      upload_id: 'aup_1', attachment_id: 'att_1', state: 'created', expires_at: '2026-08-18T00:15:00Z',
      quota: { logical_bytes: 0, reserved_bytes: 0, physical_bytes: 0, effective_record_bytes: 0, project_warning: false },
      target: { transport: 'local', upload_url: '/api/attachment-uploads/aup_1/content', method: 'PUT', required_headers: [] },
    })
    api.uploadAttachmentContent.mockResolvedValue(undefined)
    api.completeAttachmentUpload.mockResolvedValue({ upload_id: 'aup_1', attachment_id: 'att_1', state: 'available', quota: {} })
    const onAvailable = vi.fn()
    const { result } = renderHook(() => useRecordAttachmentUploads({ ensureDraft: vi.fn().mockResolvedValue(draft), isPublishing: () => false, onAvailable }))
    await act(async () => {
      await result.current.addFiles([new File(['hello'], 'trace.log', { type: '' })])
    })
    await waitFor(() => expect(onAvailable).toHaveBeenCalledWith('att_1'))
    expect(api.createAttachmentUpload).toHaveBeenCalledWith({
      draft_id: 'rdf_up', display_name: 'trace.log', media_type: 'text/plain', declared_size_bytes: 5,
    }, expect.any(AbortSignal))
    await waitFor(() => expect(result.current.rows).toEqual([]))
    expect(result.current.active).toBe(false)
  })

  it('counts accepted files as busy while the draft is still being saved', async () => {
    let resolveDraft: (value: RecordDraft | undefined) => void = () => undefined
    const ensureDraft = vi.fn(() => new Promise<RecordDraft | undefined>((resolve) => { resolveDraft = resolve }))
    const { result } = renderHook(() => useRecordAttachmentUploads({ ensureDraft, isPublishing: () => false, onAvailable: vi.fn() }))
    let adding: Promise<void> = Promise.resolve()
    act(() => {
      adding = result.current.addFiles([new File(['x'], 'trace.log')])
    })
    // 还没入队，但发布入口必须已经被挡住。
    expect(result.current.active).toBe(true)
    expect(result.current.isBusy()).toBe(true)
    await act(async () => {
      resolveDraft(undefined)
      await adding
    })
    expect(result.current.active).toBe(false)
    expect(result.current.isBusy()).toBe(false)
  })

  it('does not start uploads when the workspace unmounts while the draft is being saved', async () => {
    let resolveDraft: (value: RecordDraft | undefined) => void = () => undefined
    const ensureDraft = vi.fn(() => new Promise<RecordDraft | undefined>((resolve) => { resolveDraft = resolve }))
    const { result, unmount } = renderHook(() => useRecordAttachmentUploads({ ensureDraft, isPublishing: () => false, onAvailable: vi.fn() }))
    let adding: Promise<void> = Promise.resolve()
    act(() => {
      adding = result.current.addFiles([new File(['x'], 'trace.log')])
    })
    unmount()
    resolveDraft(draft)
    await adding
    // 建队列后要先算摘要才发请求：多等一会儿，确认确实没有启动上传。
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(api.createAttachmentUpload).not.toHaveBeenCalled()
  })

  it('drops a consumed draft queue on reset so failed rows cannot retry against it', async () => {
    api.createAttachmentUpload.mockRejectedValueOnce(new Error('网络中断'))
    const { result } = renderHook(() => useRecordAttachmentUploads({
      ensureDraft: vi.fn().mockResolvedValue(draft), isPublishing: () => false, onAvailable: vi.fn(),
    }))
    await act(async () => {
      await result.current.addFiles([new File(['hello'], 'trace.log')])
    })
    await waitFor(() => expect(result.current.rows).toEqual([expect.objectContaining({ status: 'failed' })]))
    const clientId = result.current.rows[0]!.client_id

    act(() => result.current.reset())
    expect(result.current.rows).toEqual([])
    act(() => result.current.retry(clientId))
    expect(api.createAttachmentUpload).toHaveBeenCalledTimes(1)
  })

  it('starts no upload or retry while a publish is consuming the draft', async () => {
    let publishing = false
    api.createAttachmentUpload.mockRejectedValueOnce(new Error('网络中断'))
    const ensureDraft = vi.fn().mockResolvedValue(draft)
    const { result } = renderHook(() => useRecordAttachmentUploads({ ensureDraft, isPublishing: () => publishing, onAvailable: vi.fn() }))
    await act(async () => {
      await result.current.addFiles([new File(['hello'], 'trace.log')])
    })
    await waitFor(() => expect(result.current.rows).toEqual([expect.objectContaining({ status: 'failed' })]))

    publishing = true
    act(() => result.current.retry(result.current.rows[0]!.client_id))
    await act(async () => {
      await result.current.addFiles([new File(['more'], 'more.log')])
    })
    expect(result.current.notice).toBe('正在发布，完成后再上传附件')
    expect(ensureDraft).toHaveBeenCalledTimes(1)
    expect(api.createAttachmentUpload).toHaveBeenCalledTimes(1)
    expect(result.current.rows).toEqual([expect.objectContaining({ status: 'failed' })])
  })
})
