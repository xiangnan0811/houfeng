import {
  completeAttachmentUpload,
  createAttachmentUpload,
  getAttachmentMetadata,
  uploadAttachmentContent,
} from '../../lib/recordsApi'
import type {
  AttachmentMetadata,
  AttachmentUploadCompletion,
  AttachmentUploadSession,
  CreateAttachmentUploadInput,
} from '../../lib/types'

export type RecordAttachmentQueueStatus =
  | 'queued'
  | 'hashing'
  | 'creating'
  | 'uploading'
  | 'processing'
  | 'available'
  | 'rejected'
  | 'expired'
  | 'failed'
  | 'cancelled'

export type RecordAttachmentQueueItem = Readonly<{
  client_id: string
  file: File
  display_name: string
  media_type: string
  size_bytes: number
  status: RecordAttachmentQueueStatus
  upload_id?: string
  attachment_id?: string
  error?: string
}>

export type RecordAttachmentQueueDependencies = Readonly<{
  newClientID: (file: File) => string
  digestFile: (file: File, signal: AbortSignal) => Promise<string>
  createUpload: (
    input: CreateAttachmentUploadInput,
    signal: AbortSignal,
  ) => Promise<AttachmentUploadSession>
  uploadContent: (
    session: AttachmentUploadSession,
    draftId: string,
    sha256: string,
    content: Blob,
    signal: AbortSignal,
  ) => Promise<void>
  completeUpload: (
    uploadId: string,
    draftId: string,
    signal: AbortSignal,
  ) => Promise<AttachmentUploadCompletion>
  getMetadata: (attachmentId: string, signal: AbortSignal) => Promise<AttachmentMetadata>
  waitForPoll: (signal: AbortSignal) => Promise<void>
}>

export type RecordAttachmentQueueController = Readonly<{
  enqueue: (files: readonly File[]) => string[]
  retry: (clientId: string) => boolean
  cancel: (clientId: string) => boolean
  remove: (clientId: string) => boolean
  getSnapshot: () => RecordAttachmentQueueItem[]
  dispose: () => void
}>

type QueueEntry = {
  item: RecordAttachmentQueueItem
  generation: number
  abortController: AbortController | null
}

const activeStatuses = new Set<RecordAttachmentQueueStatus>([
  'queued',
  'hashing',
  'creating',
  'uploading',
  'processing',
])

function abortReason(): DOMException {
  return new DOMException('Attachment upload cancelled', 'AbortError')
}

function assertNotAborted(signal: AbortSignal): void {
  if (signal.aborted) throw signal.reason ?? abortReason()
}

async function digestFile(file: File, signal: AbortSignal): Promise<string> {
  assertNotAborted(signal)
  const content = await file.arrayBuffer()
  assertNotAborted(signal)
  const digest = await crypto.subtle.digest('SHA-256', content)
  assertNotAborted(signal)
  return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, '0')).join('')
}

function waitForPoll(signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason ?? abortReason())
      return
    }
    const timer = window.setTimeout(() => {
      signal.removeEventListener('abort', onAbort)
      resolve()
    }, 1000)
    function onAbort() {
      window.clearTimeout(timer)
      reject(signal.reason ?? abortReason())
    }
    signal.addEventListener('abort', onAbort, { once: true })
  })
}

const defaultDependencies: RecordAttachmentQueueDependencies = {
  newClientID: () => `attachment_${crypto.randomUUID()}`,
  digestFile,
  createUpload: createAttachmentUpload,
  uploadContent: uploadAttachmentContent,
  completeUpload: completeAttachmentUpload,
  getMetadata: getAttachmentMetadata,
  waitForPoll,
}

function initialItem(clientId: string, file: File): RecordAttachmentQueueItem {
  return {
    client_id: clientId,
    file,
    display_name: file.name,
    media_type: file.type || 'application/octet-stream',
    size_bytes: file.size,
    status: 'queued',
  }
}

function errorMessage(reason: unknown): string {
  return reason instanceof Error && reason.message ? reason.message : '附件上传失败'
}

export function createRecordAttachmentQueueController(options: Readonly<{
  draftId: string
  onChange: (items: readonly RecordAttachmentQueueItem[]) => void
  dependencies?: RecordAttachmentQueueDependencies
  maxPollAttempts?: number
}>): RecordAttachmentQueueController {
  const dependencies = options.dependencies ?? defaultDependencies
  const maxPollAttempts = options.maxPollAttempts ?? 120
  const entries = new Map<string, QueueEntry>()
  let disposed = false

  function getSnapshot(): RecordAttachmentQueueItem[] {
    return Array.from(entries.values(), (entry) => ({ ...entry.item }))
  }

  function emit(): void {
    if (!disposed) options.onChange(getSnapshot())
  }

  function current(entry: QueueEntry, generation: number): boolean {
    return !disposed && entry.generation === generation &&
      entries.get(entry.item.client_id) === entry && !entry.abortController?.signal.aborted
  }

  function transition(
    entry: QueueEntry,
    generation: number,
    item: RecordAttachmentQueueItem,
  ): boolean {
    if (!current(entry, generation)) return false
    entry.item = item
    emit()
    return true
  }

  async function poll(
    entry: QueueEntry,
    generation: number,
    attachmentId: string,
    signal: AbortSignal,
  ): Promise<void> {
    for (let attempt = 0; attempt < maxPollAttempts; attempt += 1) {
      const result = await dependencies.getMetadata(attachmentId, signal)
      if (result.state === 'available' || result.state === 'rejected' || result.state === 'expired') {
        transition(entry, generation, {
          ...entry.item,
          status: result.state,
        })
        return
      }
      if (attempt + 1 < maxPollAttempts) await dependencies.waitForPoll(signal)
    }
    throw new Error('附件处理状态轮询超时')
  }

  async function run(entry: QueueEntry, generation: number): Promise<void> {
    const abortController = entry.abortController
    if (!abortController) return
    const { signal } = abortController
    try {
      if (!transition(entry, generation, { ...entry.item, status: 'hashing' })) return
      const sha256 = await dependencies.digestFile(entry.item.file, signal)
      if (!transition(entry, generation, { ...entry.item, status: 'creating' })) return
      const upload = await dependencies.createUpload({
        draft_id: options.draftId,
        display_name: entry.item.display_name,
        media_type: entry.item.media_type,
        declared_size_bytes: entry.item.size_bytes,
      }, signal)
      if (!transition(entry, generation, {
        ...entry.item,
        status: 'uploading',
        upload_id: upload.upload_id,
        attachment_id: upload.attachment_id,
      })) return
      await dependencies.uploadContent(upload, options.draftId, sha256, entry.item.file, signal)
      const completed = await dependencies.completeUpload(upload.upload_id, options.draftId, signal)
      if (completed.state === 'available') {
        transition(entry, generation, { ...entry.item, status: 'available' })
        return
      }
      if (!transition(entry, generation, { ...entry.item, status: 'processing' })) return
      await poll(entry, generation, upload.attachment_id, signal)
    } catch (reason: unknown) {
      if (!current(entry, generation)) return
      transition(entry, generation, {
        ...entry.item,
        status: signal.aborted ? 'cancelled' : 'failed',
        error: signal.aborted ? '上传已取消' : errorMessage(reason),
      })
    }
  }

  function start(entry: QueueEntry): void {
    entry.generation += 1
    entry.abortController = new AbortController()
    void run(entry, entry.generation)
  }

  function enqueue(files: readonly File[]): string[] {
    if (disposed) return []
    const clientIds: string[] = []
    for (const file of files) {
      const clientId = dependencies.newClientID(file)
      if (entries.has(clientId)) throw new Error(`duplicate attachment queue id: ${clientId}`)
      const entry: QueueEntry = {
        item: initialItem(clientId, file),
        generation: 0,
        abortController: null,
      }
      entries.set(clientId, entry)
      clientIds.push(clientId)
      emit()
      start(entry)
    }
    return clientIds
  }

  function cancel(clientId: string): boolean {
    const entry = entries.get(clientId)
    if (!entry || !activeStatuses.has(entry.item.status)) return false
    entry.generation += 1
    entry.abortController?.abort(abortReason())
    entry.abortController = null
    entry.item = { ...entry.item, status: 'cancelled', error: '上传已取消' }
    emit()
    return true
  }

  function retry(clientId: string): boolean {
    const entry = entries.get(clientId)
    if (!entry || !['failed', 'cancelled', 'expired'].includes(entry.item.status)) return false
    entry.abortController?.abort(abortReason())
    entry.item = initialItem(clientId, entry.item.file)
    emit()
    start(entry)
    return true
  }

  function remove(clientId: string): boolean {
    const entry = entries.get(clientId)
    if (!entry || activeStatuses.has(entry.item.status)) return false
    entry.abortController?.abort(abortReason())
    entries.delete(clientId)
    emit()
    return true
  }

  function dispose(): void {
    if (disposed) return
    disposed = true
    for (const entry of entries.values()) entry.abortController?.abort(abortReason())
    entries.clear()
  }

  return { enqueue, retry, cancel, remove, getSnapshot, dispose }
}
