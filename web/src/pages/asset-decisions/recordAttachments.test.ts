import { describe, expect, it, vi } from 'vitest'

import type {
  AttachmentMetadata,
  AttachmentUploadCompletion,
  AttachmentUploadSession,
} from '../../lib/types'
import {
  createRecordAttachmentQueueController,
  type RecordAttachmentQueueDependencies,
  type RecordAttachmentQueueItem,
} from './recordAttachments'

const quota = {
  logical_bytes: 0,
  reserved_bytes: 12,
  physical_bytes: 0,
  effective_record_bytes: 12,
  project_warning: false,
}

function session(transport: 'local' | 's3' = 'local'): AttachmentUploadSession {
  return {
    upload_id: 'aup_queue',
    attachment_id: 'att_queue',
    state: transport === 'local' ? 'created' : 'uploading',
    expires_at: '2026-08-09T20:00:00Z',
    quota,
    target: transport === 'local'
      ? {
          transport: 'local',
          upload_url: '/api/attachment-uploads/aup_queue/content',
          method: 'PUT',
          required_headers: ['X-Houfeng-Draft-ID', 'X-Content-SHA256'],
        }
      : {
          transport: 's3',
          upload_url: 'https://objects.example.test/private-upload',
          method: 'PUT',
          required_headers: [],
          temporary_object_key: 'temporary/' + 'b'.repeat(64),
        },
  }
}

function completion(state: 'quarantined' | 'available'): AttachmentUploadCompletion {
  return {
    upload_id: 'aup_queue',
    attachment_id: 'att_queue',
    state,
    quota,
  }
}

function metadata(state: AttachmentMetadata['state']): AttachmentMetadata {
  return {
    attachment_id: 'att_queue',
    state,
    display_name: 'incident.txt',
    media_type: 'text/plain',
    size_bytes: 12,
    preview_available: state === 'available',
  }
}

function dependencies(
  overrides: Partial<RecordAttachmentQueueDependencies> = {},
): RecordAttachmentQueueDependencies {
  return {
    newClientID: () => 'queue_item',
    digestFile: vi.fn().mockResolvedValue('a'.repeat(64)),
    createUpload: vi.fn().mockResolvedValue(session()),
    uploadContent: vi.fn().mockResolvedValue(undefined),
    completeUpload: vi.fn().mockResolvedValue(completion('available')),
    getMetadata: vi.fn().mockResolvedValue(metadata('available')),
    waitForPoll: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

function latestItem(snapshots: readonly RecordAttachmentQueueItem[][]): RecordAttachmentQueueItem {
  const snapshot = snapshots.at(-1)
  const item = snapshot?.[0]
  if (!item) throw new Error('missing queue item')
  return item
}

describe('record attachment queue controller', () => {
  it('moves an upload through hashing, transport, quarantine polling, and availability', async () => {
    const getMetadata = vi.fn()
      .mockResolvedValueOnce(metadata('quarantined'))
      .mockResolvedValueOnce(metadata('available'))
    const deps = dependencies({
      completeUpload: vi.fn().mockResolvedValue(completion('quarantined')),
      getMetadata,
    })
    const snapshots: RecordAttachmentQueueItem[][] = []
    const controller = createRecordAttachmentQueueController({
      draftId: 'rdf_queue',
      dependencies: deps,
      onChange: (items) => snapshots.push([...items]),
    })
    const file = new File(['safe content'], 'incident.txt', { type: 'text/plain' })

    expect(controller.enqueue([file])).toEqual(['queue_item'])
    await vi.waitFor(() => expect(latestItem(snapshots).status).toBe('available'))

    const statuses = snapshots.map((items) => items[0]?.status).filter(Boolean)
    expect(statuses).toEqual([
      'queued',
      'hashing',
      'creating',
      'uploading',
      'processing',
      'available',
    ])
    expect(latestItem(snapshots)).toMatchObject({
      client_id: 'queue_item',
      upload_id: 'aup_queue',
      attachment_id: 'att_queue',
      display_name: 'incident.txt',
    })
    expect(deps.uploadContent).toHaveBeenCalledWith(
      session(),
      'rdf_queue',
      'a'.repeat(64),
      file,
      expect.any(AbortSignal),
    )
    expect(getMetadata).toHaveBeenCalledTimes(2)
  })

  it('cancels an active upload, retries it, and removes the terminal item locally', async () => {
    let uploadAttempt = 0
    const signals: AbortSignal[] = []
    const uploadContent = vi.fn((
      _session: AttachmentUploadSession,
      _draftId: string,
      _sha256: string,
      _content: Blob,
      signal: AbortSignal,
    ) => {
      uploadAttempt += 1
      signals.push(signal)
      if (uploadAttempt > 1) return Promise.resolve()
      return new Promise<void>((_resolve, reject) => {
        signal.addEventListener('abort', () => reject(signal.reason), { once: true })
      })
    })
    const snapshots: RecordAttachmentQueueItem[][] = []
    const controller = createRecordAttachmentQueueController({
      draftId: 'rdf_queue',
      dependencies: dependencies({ uploadContent }),
      onChange: (items) => snapshots.push([...items]),
    })

    controller.enqueue([new File(['safe content'], 'incident.txt', { type: 'text/plain' })])
    await vi.waitFor(() => expect(latestItem(snapshots).status).toBe('uploading'))
    expect(controller.cancel('queue_item')).toBe(true)
    expect(latestItem(snapshots).status).toBe('cancelled')
    expect(signals[0]?.aborted).toBe(true)

    expect(controller.retry('queue_item')).toBe(true)
    await vi.waitFor(() => expect(latestItem(snapshots).status).toBe('available'))
    expect(uploadContent).toHaveBeenCalledTimes(2)
    expect(controller.remove('queue_item')).toBe(true)
    expect(controller.getSnapshot()).toEqual([])
  })

  it('aborts active work and suppresses later emissions after dispose', async () => {
    let resolveCreate: ((value: AttachmentUploadSession) => void) | undefined
    let createSignal: AbortSignal | undefined
    const createUpload = vi.fn((
      _input: Parameters<RecordAttachmentQueueDependencies['createUpload']>[0],
      signal: AbortSignal,
    ) => {
      createSignal = signal
      return new Promise<AttachmentUploadSession>((resolve) => {
        resolveCreate = resolve
      })
    })
    const snapshots: RecordAttachmentQueueItem[][] = []
    const controller = createRecordAttachmentQueueController({
      draftId: 'rdf_queue',
      dependencies: dependencies({ createUpload }),
      onChange: (items) => snapshots.push([...items]),
    })

    controller.enqueue([new File(['safe content'], 'incident.txt', { type: 'text/plain' })])
    await vi.waitFor(() => expect(latestItem(snapshots).status).toBe('creating'))
    const emissionsBeforeDispose = snapshots.length
    controller.dispose()
    expect(createSignal?.aborted).toBe(true)
    resolveCreate?.(session())
    await Promise.resolve()
    await Promise.resolve()

    expect(snapshots).toHaveLength(emissionsBeforeDispose)
  })
})
