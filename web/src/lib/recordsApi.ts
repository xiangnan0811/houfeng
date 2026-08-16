import allowlistedApiError from './apiError'
import {
  ApiError,
  jsonBodyInit,
  requestBlob as transportRequestBlob,
  requestEmpty as transportRequestEmpty,
  requestExternalEmpty,
  requestJSON as transportRequestJSON,
  withQuery,
} from './apiRequest'
import type {
  AttachmentContentVariant,
  AttachmentMetadata,
  AttachmentUploadCompletion,
  AttachmentUploadSession,
  CreateAttachmentUploadInput,
  CreateRecordDraftInput,
  EvidenceCapturePreview,
  EvidenceCapturePreviewInput,
  EvidenceSnapshotRead,
  PatchRecordDraftInput,
  PublishRecordInput,
  PublishRecordRevisionInput,
  RecordDeletionExecuteInput,
  RecordDeletionOperation,
  RecordDeletionPreview,
  RecordDetail,
  RecordDraft,
  RecordDraftListFilter,
  RecordDraftListResponse,
  RecordLifecycleResult,
  RecordListFilter,
  RecordListResponse,
  RecordMutationResult,
  RecordRevision,
  RecordRevisionListFilter,
  RecordRevisionListResponse,
  RestoreRecordRevisionInput,
} from './types'

function encoded(value: string): string {
  return encodeURIComponent(value)
}

function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  return transportRequestJSON<T>(path, init, allowlistedApiError)
}

function requestEmpty(path: string, init?: RequestInit): Promise<void> {
  return transportRequestEmpty(path, init, allowlistedApiError)
}

function requestBlob(path: string, init?: RequestInit): Promise<Blob> {
  return transportRequestBlob(path, init, allowlistedApiError)
}

function attachmentUploadHeaders(
  requiredHeaders: readonly string[],
  draftId: string,
  sha256: string,
  content: Blob,
): Record<string, string> {
  const headers: Record<string, string> = {}
  for (const header of requiredHeaders) {
    switch (header.toLowerCase()) {
      case 'x-houfeng-draft-id':
        headers[header] = draftId
        break
      case 'x-content-sha256':
        headers[header] = sha256
        break
      case 'content-type':
        headers[header] = content.type || 'application/octet-stream'
        break
      default:
        throw new ApiError(503, 'unsupported attachment upload instruction', {
          code: 'attachment_service_unavailable',
        })
    }
  }
  return headers
}

function postIdempotentJSON<T>(path: string, body: unknown, idempotencyKey: string): Promise<T> {
  return requestJSON<T>(path, jsonBodyInit('POST', body, {
    'Idempotency-Key': idempotencyKey,
  }))
}

function changeRecordLifecycle(
  recordId: string,
  action: 'archive' | 'restore',
  idempotencyKey: string,
): Promise<RecordLifecycleResult> {
  return requestJSON<RecordLifecycleResult>(`/api/records/${encoded(recordId)}/${action}`, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  })
}

export function listRecords(filter?: RecordListFilter): Promise<RecordListResponse> {
  return requestJSON<RecordListResponse>(withQuery('/api/records', filter
    ? {
        q: filter.q,
        lifecycle: filter.lifecycle,
        record_type: filter.record_type,
        sort: filter.sort,
        limit: filter.limit,
        cursor: filter.cursor,
      }
    : undefined))
}

export function captureEvidencePreview(
  input: EvidenceCapturePreviewInput,
  signal?: AbortSignal,
): Promise<EvidenceCapturePreview> {
  const body: EvidenceCapturePreviewInput = {
    kind: input.kind,
    schema_version: input.schema_version,
    source_type: input.source_type,
    source_id: input.source_id,
    requested_window: {
      start: input.requested_window.start,
      end: input.requested_window.end,
    },
    metrics: [...input.metrics],
    precision_seconds: input.precision_seconds,
    sensitive_topology_fields: [...input.sensitive_topology_fields],
  }
  if (input.record_id !== undefined) body.record_id = input.record_id
  const init = jsonBodyInit('POST', body)
  if (signal) init.signal = signal
  return requestJSON<EvidenceCapturePreview>('/api/evidence/capture-previews', init)
}

export function getEvidenceSnapshot(
  snapshotId: string,
  signal?: AbortSignal,
): Promise<EvidenceSnapshotRead> {
  const init: RequestInit = {}
  if (signal) init.signal = signal
  return requestJSON<EvidenceSnapshotRead>(`/api/evidence/${encoded(snapshotId)}`, init)
}

export function createAttachmentUpload(
  input: CreateAttachmentUploadInput,
  signal?: AbortSignal,
): Promise<AttachmentUploadSession> {
  const init = jsonBodyInit('POST', input)
  if (signal) init.signal = signal
  return requestJSON<AttachmentUploadSession>(
    '/api/attachment-uploads',
    init,
  )
}

export async function uploadAttachmentContent(
  session: AttachmentUploadSession,
  draftId: string,
  sha256: string,
  content: Blob,
  signal?: AbortSignal,
): Promise<void> {
  const headers = attachmentUploadHeaders(session.target.required_headers, draftId, sha256, content)
  if (session.target.transport === 'local') headers.Accept = 'application/json'
  const init: RequestInit = {
    method: session.target.method,
    headers,
    body: content,
  }
  if (signal) init.signal = signal

  if (session.target.transport === 's3') {
    await requestExternalEmpty(session.target.upload_url, init)
    return
  }
  await requestJSON<unknown>(session.target.upload_url, init)
}

export function completeAttachmentUpload(
  uploadId: string,
  draftId: string,
  signal?: AbortSignal,
): Promise<AttachmentUploadCompletion> {
  const init = jsonBodyInit('POST', { draft_id: draftId })
  if (signal) init.signal = signal
  return requestJSON<AttachmentUploadCompletion>(
    `/api/attachment-uploads/${encoded(uploadId)}/complete`,
    init,
  )
}

export function getAttachmentMetadata(
  attachmentId: string,
  signal?: AbortSignal,
): Promise<AttachmentMetadata> {
  const init: RequestInit = {}
  if (signal) init.signal = signal
  return requestJSON<AttachmentMetadata>(`/api/attachments/${encoded(attachmentId)}`, init)
}

export function getAttachmentContent(
  attachmentId: string,
  variant: AttachmentContentVariant = 'original',
  signal?: AbortSignal,
): Promise<Blob> {
  const init: RequestInit = { headers: { Accept: 'application/octet-stream' } }
  if (signal) init.signal = signal
  return requestBlob(withQuery(
    `/api/attachments/${encoded(attachmentId)}/content`,
    variant === 'original' ? undefined : { variant },
  ), init)
}

export function getRecord(recordId: string): Promise<RecordDetail> {
  return requestJSON<RecordDetail>(`/api/records/${encoded(recordId)}`)
}

export function createRecord(input: PublishRecordInput, idempotencyKey: string): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>('/api/records', input, idempotencyKey)
}

export function listRecordRevisions(
  recordId: string,
  filter?: RecordRevisionListFilter,
): Promise<RecordRevisionListResponse> {
  return requestJSON<RecordRevisionListResponse>(withQuery(
    `/api/records/${encoded(recordId)}/revisions`,
    filter ? { limit: filter.limit } : undefined,
  ))
}

export function getRecordRevision(recordId: string, revisionId: string): Promise<RecordRevision> {
  return requestJSON<RecordRevision>(
    `/api/records/${encoded(recordId)}/revisions/${encoded(revisionId)}`,
  )
}

export function createRecordRevision(
  recordId: string,
  input: PublishRecordRevisionInput,
  idempotencyKey: string,
): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>(
    `/api/records/${encoded(recordId)}/revisions`,
    input,
    idempotencyKey,
  )
}

export function restoreRecordRevision(
  recordId: string,
  revisionId: string,
  input: RestoreRecordRevisionInput,
  idempotencyKey: string,
): Promise<RecordMutationResult> {
  return postIdempotentJSON<RecordMutationResult>(
    `/api/records/${encoded(recordId)}/revisions/${encoded(revisionId)}/restore`,
    input,
    idempotencyKey,
  )
}

export function archiveRecord(recordId: string, idempotencyKey: string): Promise<RecordLifecycleResult> {
  return changeRecordLifecycle(recordId, 'archive', idempotencyKey)
}

export function restoreRecord(recordId: string, idempotencyKey: string): Promise<RecordLifecycleResult> {
  return changeRecordLifecycle(recordId, 'restore', idempotencyKey)
}

export function listRecordDrafts(filter?: RecordDraftListFilter): Promise<RecordDraftListResponse> {
  return requestJSON<RecordDraftListResponse>(withQuery(
    '/api/record-drafts',
    filter ? { limit: filter.limit } : undefined,
  ))
}

export function createRecordDraft(input: CreateRecordDraftInput): Promise<RecordDraft> {
  return requestJSON<RecordDraft>('/api/record-drafts', jsonBodyInit('POST', input))
}

export function getRecordDraft(draftId: string): Promise<RecordDraft> {
  return requestJSON<RecordDraft>(`/api/record-drafts/${encoded(draftId)}`)
}

export function patchRecordDraft(
  draftId: string,
  input: PatchRecordDraftInput,
  etag: string,
): Promise<RecordDraft> {
  return requestJSON<RecordDraft>(
    `/api/record-drafts/${encoded(draftId)}`,
    jsonBodyInit('PATCH', input, { 'If-Match': etag }),
  )
}

export function discardRecordDraft(draftId: string): Promise<void> {
  return requestEmpty(`/api/record-drafts/${encoded(draftId)}`, { method: 'DELETE' })
}

export function previewRecordPermanentDeletion(recordId: string): Promise<RecordDeletionPreview> {
  return requestJSON<RecordDeletionPreview>(
    `/api/records/${encoded(recordId)}/permanent-delete-preview`,
    { method: 'POST' },
  )
}

export function executeRecordPermanentDeletion(
  recordId: string,
  input: RecordDeletionExecuteInput,
): Promise<RecordDeletionOperation> {
  return requestJSON<RecordDeletionOperation>(
    `/api/records/${encoded(recordId)}/permanent-delete`,
    jsonBodyInit('POST', { reservation_id: input.reservation_id }, {
      'Idempotency-Key': input.deletion_request_token,
    }),
  )
}

export function getRecordDeletionOperation(operationId: string): Promise<RecordDeletionOperation> {
  return requestJSON<RecordDeletionOperation>(`/api/record-deletions/${encoded(operationId)}`)
}
